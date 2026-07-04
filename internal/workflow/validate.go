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

// Validate 对一份定义执行落盘校验，逐条收集字段级错误后一并返回（承 spec〈落盘校验规则〉，
// 比 Python 原型更严：额外强制 id 唯一、redoTarget 存在且前向、模板引用存在）。不修改 def。
func Validate(def *Definition) error {
	if len(def.Nodes) == 0 {
		return fmt.Errorf("nodes: 不能为空，至少需要一个节点")
	}

	var problems []string

	// 预扫描：node id → 位置，供唯一性与 redoTarget 前向性校验。
	indexByID := make(map[string]int, len(def.Nodes))
	for position, node := range def.Nodes {
		if node.ID == "" {
			continue // 空 id 的必填错误在下方主循环报出
		}
		if _, duplicate := indexByID[node.ID]; duplicate {
			problems = append(problems, fmt.Sprintf("nodes[%d].id: 与前面的节点重复 %q", position, node.ID))
			continue
		}
		indexByID[node.ID] = position
	}
	validNodeID := func(id string) bool { _, ok := indexByID[id]; return ok }

	for position, node := range def.Nodes {
		path := fmt.Sprintf("nodes[%d]", position)

		// —— 必填与格式 ——
		if node.ID == "" {
			problems = append(problems, path+".id: 必填")
		} else if !nodeIDPattern.MatchString(node.ID) {
			problems = append(problems, fmt.Sprintf("%s.id: %q 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）", path, node.ID))
		}
		if node.DisplayName == "" {
			problems = append(problems, path+".displayName: 必填")
		}
		if node.PromptTemplate == "" {
			problems = append(problems, path+".promptTemplate: 必填")
		}

		// —— engine + engineConfig（判别联合，依能力表）——
		problems = append(problems, validateEngine(path, node.Engine, node.EngineConfig)...)

		// —— evaluator 与 redoTarget 互斥 ——
		if node.Evaluator != nil && node.RedoTarget != "" {
			problems = append(problems, path+": evaluator 与 redoTarget 互斥，不能并存")
		}

		// —— evaluator 自身 ——
		if node.Evaluator != nil {
			evaluatorPath := path + ".evaluator"
			if node.Evaluator.PromptTemplate == "" {
				problems = append(problems, evaluatorPath+".promptTemplate: 必填")
			}
			problems = append(problems, validateEngine(evaluatorPath, node.Evaluator.Engine, node.Evaluator.EngineConfig)...)
			problems = append(problems, validateTemplateReferences(evaluatorPath+".promptTemplate", node.Evaluator.PromptTemplate, validNodeID)...)
		}

		// —— redoTarget 合法回跳：存在且位于本节点之前 ——
		if node.RedoTarget != "" {
			targetPosition, ok := indexByID[node.RedoTarget]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s.redoTarget: 指向不存在的节点 %q", path, node.RedoTarget))
			case targetPosition >= position:
				problems = append(problems, fmt.Sprintf("%s.redoTarget: 必须指向本节点之前的节点，%q 在其后或即本身", path, node.RedoTarget))
			}
		}

		// —— loopCount 仅在有 evaluator / redoTarget 时校验，范围 1–20 ——
		if (node.Evaluator != nil || node.RedoTarget != "") && node.LoopCount != nil {
			if *node.LoopCount < 1 || *node.LoopCount > 20 {
				problems = append(problems, fmt.Sprintf("%s.loopCount: 须为 1–20 的整数，得到 %d", path, *node.LoopCount))
			}
		}

		// —— 模板变量引用存在 ——
		problems = append(problems, validateTemplateReferences(path+".promptTemplate", node.PromptTemplate, validNodeID)...)
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

// validateEngine 校验 engine 合法性及其 engineConfig（判别联合，依 engine 包能力表）。
func validateEngine(path, engineName string, config *EngineConfig) []string {
	if engineName == "" {
		return []string{path + ".engine: 必填"}
	}
	if !engine.Exists(engineName) {
		return []string{fmt.Sprintf("%s.engine: 未知引擎 %q（可用：%s）",
			path, engineName, strings.Join(engine.RegisteredNames(), ", "))}
	}
	if config == nil {
		return nil
	}

	var problems []string
	capability, ok := engine.Capability(engineName)
	if !ok {
		// 引擎已登记但能力表待实装 → 暂不接受任何 engineConfig 字段
		if config.Model != "" || config.Effort != "" || config.ReasoningEffort != "" {
			problems = append(problems, fmt.Sprintf("%s.engineConfig: engine=%q 的能力表待实装，暂不接受配置字段", path, engineName))
		}
		return problems
	}

	if config.Model != "" && !capability.AllowsModel {
		problems = append(problems, fmt.Sprintf("%s.engineConfig.model: engine=%q 不接受 model", path, engineName))
	}
	// effort / reasoningEffort：只接受该引擎声明的那一个，另一个必须为空。
	if config.Effort != "" {
		if capability.EffortField != "effort" {
			problems = append(problems, fmt.Sprintf("%s.engineConfig.effort: engine=%q 不认 effort%s", path, engineName, otherEffortHint(capability, "effort")))
		} else if !slices.Contains(capability.EffortValues, config.Effort) {
			problems = append(problems, fmt.Sprintf("%s.engineConfig.effort: %q 不在 engine=%q 允许集 [%s] 内", path, config.Effort, engineName, strings.Join(capability.EffortValues, ", ")))
		}
	}
	if config.ReasoningEffort != "" {
		if capability.EffortField != "reasoningEffort" {
			problems = append(problems, fmt.Sprintf("%s.engineConfig.reasoningEffort: engine=%q 不认 reasoningEffort%s", path, engineName, otherEffortHint(capability, "reasoningEffort")))
		} else if !slices.Contains(capability.EffortValues, config.ReasoningEffort) {
			problems = append(problems, fmt.Sprintf("%s.engineConfig.reasoningEffort: %q 不在 engine=%q 允许集 [%s] 内", path, config.ReasoningEffort, engineName, strings.Join(capability.EffortValues, ", ")))
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
// {{nodeId}} 须引用定义内存在的节点。
func validateTemplateReferences(path, template string, validNodeID func(string) bool) []string {
	var problems []string
	for _, match := range templateVariablePattern.FindAllStringSubmatch(template, -1) {
		full, key := match[0], match[1]
		if strings.HasPrefix(full, "\\") {
			continue // 转义 \{{x}} → 字面量，不校验
		}
		if strings.HasPrefix(key, "sys.") {
			if !knownSystemVariables[key] {
				problems = append(problems, fmt.Sprintf("%s: 引用未知系统变量 {{%s}}（仅支持 sys.userPrompt / sys.cwd）", path, key))
			}
			continue
		}
		if !validNodeID(key) {
			problems = append(problems, fmt.Sprintf("%s: 引用不存在的节点 {{%s}}", path, key))
		}
	}
	return problems
}
