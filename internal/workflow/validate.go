package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/qoggy/conduct/internal/engine"
)

// nodeIDPattern 限定 node id：首字符字母 / 下划线，其余字母 / 数字 / 连字符 / 下划线，总长 1–64。
// 注意与工作流名规则不同（工作流名见 name.go，可含点、可数字开头）。
var nodeIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// templateVariablePattern 匹配模板里的变量引用 {{key}}，允许前置反斜杠转义 \{{key}}。
// 与运行时 render 阶段保持同一套语法。
var templateVariablePattern = regexp.MustCompile(`\\?\{\{([a-zA-Z_][\w.-]*)\}\}`)

// knownSystemVariables 是模板里允许的 {{sys.*}} 系统变量集。
var knownSystemVariables = map[string]bool{
	"sys.userPrompt": true,
	"sys.cwd":        true,
}

// Problem 是一条字段级校验错误：Path 为出错字段的点路径（如 "nodes[0].id" / "nodes[0]" / "nodes"），
// Message 为不含路径前缀的纯消息。供编辑器把「点校验错误 → 定位到对应字段」建立在结构而非文案格式上。
type Problem struct {
	Path    string
	Message string
}

// ValidateStructured 对一份定义执行落盘校验，逐条收集字段级错误后一并返回（承 spec〈落盘校验规则〉，
// 比 Python 原型更严：额外强制 id 唯一、redoTarget 存在且前向、模板引用存在）。不修改 def。
// 返回空切片表示校验通过。Validate 是它的字符串化 thin wrapper。
func ValidateStructured(def *Definition) []Problem {
	if len(def.Nodes) == 0 {
		return []Problem{{Path: "nodes", Message: "不能为空，至少需要一个节点"}}
	}

	var problems []Problem

	// 预扫描：node id → 位置，供唯一性与 redoTarget 前向性校验。
	indexByID := make(map[string]int, len(def.Nodes))
	for position, node := range def.Nodes {
		if node.ID == "" {
			continue // 空 id 的必填错误在下方主循环报出
		}
		if _, duplicate := indexByID[node.ID]; duplicate {
			problems = append(problems, Problem{fmt.Sprintf("nodes[%d].id", position), fmt.Sprintf("与前面的节点重复 %q", node.ID)})
			continue
		}
		indexByID[node.ID] = position
	}
	validNodeID := func(id string) bool { _, ok := indexByID[id]; return ok }

	for position, node := range def.Nodes {
		path := fmt.Sprintf("nodes[%d]", position)

		// —— 必填与格式 ——
		if node.ID == "" {
			problems = append(problems, Problem{path + ".id", "必填"})
		} else if !nodeIDPattern.MatchString(node.ID) {
			problems = append(problems, Problem{path + ".id", fmt.Sprintf("%q 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）", node.ID)})
		}
		if node.DisplayName == "" {
			problems = append(problems, Problem{path + ".displayName", "必填"})
		}
		if node.PromptTemplate == "" {
			problems = append(problems, Problem{path + ".promptTemplate", "必填"})
		}

		// —— engine + engineConfig（判别联合，依能力表）——
		problems = append(problems, validateEngine(path, node.Engine, node.EngineConfig)...)

		// —— evaluator 与 redoTarget 互斥 ——
		if node.Evaluator != nil && node.RedoTarget != "" {
			problems = append(problems, Problem{path, "evaluator 与 redoTarget 互斥，不能并存"})
		}

		// —— evaluator 自身 ——
		if node.Evaluator != nil {
			evaluatorPath := path + ".evaluator"
			if node.Evaluator.PromptTemplate == "" {
				problems = append(problems, Problem{evaluatorPath + ".promptTemplate", "必填"})
			}
			problems = append(problems, validateEngine(evaluatorPath, node.Evaluator.Engine, node.Evaluator.EngineConfig)...)
			problems = append(problems, validateTemplateReferences(evaluatorPath+".promptTemplate", node.Evaluator.PromptTemplate, validNodeID)...)
		}

		// —— redoTarget 合法回跳：存在且位于本节点之前 ——
		if node.RedoTarget != "" {
			targetPosition, ok := indexByID[node.RedoTarget]
			switch {
			case !ok:
				problems = append(problems, Problem{path + ".redoTarget", fmt.Sprintf("指向不存在的节点 %q", node.RedoTarget)})
			case targetPosition >= position:
				problems = append(problems, Problem{path + ".redoTarget", fmt.Sprintf("必须指向本节点之前的节点，%q 在其后或即本身", node.RedoTarget)})
			}
		}

		// —— loopCount 仅在有 evaluator / redoTarget 时校验，范围 1–20 ——
		if (node.Evaluator != nil || node.RedoTarget != "") && node.LoopCount != nil {
			if *node.LoopCount < 1 || *node.LoopCount > 20 {
				problems = append(problems, Problem{path + ".loopCount", fmt.Sprintf("须为 1–20 的整数，得到 %d", *node.LoopCount)})
			}
		}

		// —— 模板变量引用存在 ——
		problems = append(problems, validateTemplateReferences(path+".promptTemplate", node.PromptTemplate, validNodeID)...)
	}

	return problems
}

// Validate 是 ValidateStructured 的字符串化包装：把每条 Problem 拼成 "<path>: <message>"、换行连接返回，
// 输出与历史逐字一致（各调用方仍按整串比对 / 子串断言）。无错误返回 nil。
func Validate(def *Definition) error {
	problems := ValidateStructured(def)
	if len(problems) == 0 {
		return nil
	}
	lines := make([]string, len(problems))
	for i, problem := range problems {
		lines[i] = problem.Path + ": " + problem.Message
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// validateEngine 校验 engine 合法性及其 engineConfig（判别联合，依 engine 包能力表）。
func validateEngine(path, engineName string, config *EngineConfig) []Problem {
	if engineName == "" {
		return []Problem{{path + ".engine", "必填"}}
	}
	if !engine.Exists(engineName) {
		return []Problem{{path + ".engine", fmt.Sprintf("未知引擎 %q（可用：%s）",
			engineName, strings.Join(engine.RegisteredNames(), ", "))}}
	}
	if config == nil {
		return nil
	}

	var problems []Problem
	capability, ok := engine.Capability(engineName)
	if !ok {
		// 引擎已登记但能力表待实装 → 暂不接受任何 engineConfig 字段
		if config.Model != "" || config.Effort != "" || config.ReasoningEffort != "" {
			problems = append(problems, Problem{path + ".engineConfig", fmt.Sprintf("engine=%q 的能力表待实装，暂不接受配置字段", engineName)})
		}
		return problems
	}

	if config.Model != "" && !capability.AllowsModel {
		problems = append(problems, Problem{path + ".engineConfig.model", fmt.Sprintf("engine=%q 不接受 model", engineName)})
	}
	// effort / reasoningEffort：只接受该引擎声明的那一个，另一个必须为空。
	if config.Effort != "" {
		if capability.EffortField != "effort" {
			problems = append(problems, Problem{path + ".engineConfig.effort", fmt.Sprintf("engine=%q 不认 effort%s", engineName, otherEffortHint(capability, "effort"))})
		} else if !slices.Contains(capability.EffortValues, config.Effort) {
			problems = append(problems, Problem{path + ".engineConfig.effort", fmt.Sprintf("%q 不在 engine=%q 允许集 [%s] 内", config.Effort, engineName, strings.Join(capability.EffortValues, ", "))})
		}
	}
	if config.ReasoningEffort != "" {
		if capability.EffortField != "reasoningEffort" {
			problems = append(problems, Problem{path + ".engineConfig.reasoningEffort", fmt.Sprintf("engine=%q 不认 reasoningEffort%s", engineName, otherEffortHint(capability, "reasoningEffort"))})
		} else if !slices.Contains(capability.EffortValues, config.ReasoningEffort) {
			problems = append(problems, Problem{path + ".engineConfig.reasoningEffort", fmt.Sprintf("%q 不在 engine=%q 允许集 [%s] 内", config.ReasoningEffort, engineName, strings.Join(capability.EffortValues, ", "))})
		}
	}
	return problems
}

// otherEffortHint 在用户填错调优字段时，提示该引擎实际接受的字段（若有）。
func otherEffortHint(capability engine.EngineCapability, wrongField string) string {
	if capability.EffortField == "" || capability.EffortField == wrongField {
		return ""
	}
	return fmt.Sprintf("（该引擎用 %s）", capability.EffortField)
}

// validateTemplateReferences 校验模板里非转义的 {{...}} 引用：{{sys.*}} 限已知系统变量，
// {{nodeId}} 须引用定义内存在的节点。path 为该模板字段的完整点路径，直接作为 Problem.Path。
func validateTemplateReferences(path, template string, validNodeID func(string) bool) []Problem {
	var problems []Problem
	for _, match := range templateVariablePattern.FindAllStringSubmatch(template, -1) {
		full, key := match[0], match[1]
		if strings.HasPrefix(full, "\\") {
			continue // 转义 \{{x}} → 字面量，不校验
		}
		if strings.HasPrefix(key, "sys.") {
			if !knownSystemVariables[key] {
				problems = append(problems, Problem{path, fmt.Sprintf("引用未知系统变量 {{%s}}（仅支持 sys.userPrompt / sys.cwd）", key)})
			}
			continue
		}
		if !validNodeID(key) {
			problems = append(problems, Problem{path, fmt.Sprintf("引用不存在的节点 {{%s}}", key)})
		}
	}
	return problems
}
