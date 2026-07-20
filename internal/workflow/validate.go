package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/engine"
)

// nodeIDPattern 限定 agent 节点 id：首字符字母 / 下划线，其余字母 / 数字 / 连字符 / 下划线，总长 1–64。
// 注意与工作流名规则不同（工作流名见 name.go，可含点、可数字开头）。
var nodeIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// templateVariablePattern 匹配模板里的变量引用 {{key}}，允许前置反斜杠转义 \{{key}}。
// 与运行时 render 阶段保持同一套语法。
var templateVariablePattern = regexp.MustCompile(`\\?\{\{([a-zA-Z_][\w.-]*)\}\}`)

// knownSystemVariables 是模板里允许的 {{sys.*}} 系统变量集。
var knownSystemVariables = map[string]bool{
	"sys.userPrompt": true,
	"sys.cwd":        true,
	"sys.runId":      true,
}

// IsValidNodeID 报告 id 是否符合 agent 节点命名规则（不含保留名判断，供 CLI node add 在建节点前速判 id 格式）。
func IsValidNodeID(id string) bool { return nodeIDPattern.MatchString(id) }

// Problem 是一条字段级校验错误：Path 为稳定字段路径，Code/Params 为可由 CLI/UI 分别本地化的机器信息。
type Problem = apperror.Problem

// ValidateStructured 对一份定义主体执行落盘校验，逐条收集字段级错误后一并返回（承 spec〈落盘校验规则〉：
// 恰好一个 START/END + ≥1 agent、保留名、标记节点必空、边合法、无环、单源单汇无悬空、模板引用祖先）。
// 不修改 def。返回空切片表示校验通过。Validate 把问题包装成共享结构化错误。
func ValidateStructured(def *Definition) []Problem {
	if len(def.Nodes) == 0 {
		return []Problem{{Path: "nodes", Code: apperror.CodeNodesRequired}}
	}

	var problems []Problem

	// 预扫描：node id → 首次位置（供唯一性与引用校验），并数 START / END / agent。
	indexByID := make(map[string]int, len(def.Nodes))
	startCount, endCount, agentCount := 0, 0, 0
	for position, node := range def.Nodes {
		switch {
		case node.IsStart():
			startCount++
		case node.IsEnd():
			endCount++
		default:
			agentCount++
		}
		if node.ID == "" {
			continue // 空 id 的必填错误在下方 agent 主循环报出
		}
		if _, duplicate := indexByID[node.ID]; duplicate {
			problems = append(problems, problem(fmt.Sprintf("nodes[%d].id", position), apperror.CodeDuplicateNodeID, "id", node.ID))
			continue
		}
		indexByID[node.ID] = position
	}
	validNodeID := func(id string) bool { _, ok := indexByID[id]; return ok }

	// —— 节点集：恰好一个 START、一个 END，另有 ≥1 agent 节点 ——
	if startCount != 1 {
		problems = append(problems, problem("nodes", apperror.CodeStartNodeCount, "count", startCount))
	}
	if endCount != 1 {
		problems = append(problems, problem("nodes", apperror.CodeEndNodeCount, "count", endCount))
	}
	if agentCount == 0 {
		problems = append(problems, Problem{Path: "nodes", Code: apperror.CodeAgentNodeRequired})
	}

	// —— 逐节点：标记节点必空 / agent 节点必填与能力表 ——
	for position, node := range def.Nodes {
		path := fmt.Sprintf("nodes[%d]", position)
		if node.IsMarker() {
			problems = append(problems, validateMarkerNode(path, node)...)
			continue
		}
		problems = append(problems, validateAgentNode(path, node)...)
	}

	// —— 边：引用存在、自环、重复、指向 START、源自 END、START→END 直连 ——
	problems = append(problems, validateEdges(def, validNodeID)...)

	// —— 单源单汇 / 无悬空：每个 agent 节点 ≥1 入边、≥1 出边 ——
	problems = append(problems, validateDegrees(def)...)

	// —— 无环 ——
	cycle := DetectCycle(def)
	if cycle != nil {
		problems = append(problems, problem("edges", apperror.CodeCycleDetected, "cycle", strings.Join(cycle, "→")))
	}

	// —— 模板引用祖先 —— 仅在无环时「祖先」有定义，故 DetectCycle 通过后再算。
	if cycle == nil {
		problems = append(problems, validateTemplateAncestry(def, validNodeID)...)
	}

	return problems
}

// Validate 是 ValidateStructured 的字符串化包装：把每条 Problem 拼成 "<path>: <message>"、换行连接返回。
// 各调用方（CLI / store 载入兜底）按整串比对 / 子串断言。无错误返回 nil。
func Validate(def *Definition) error {
	problems := ValidateStructured(def)
	if len(problems) == 0 {
		return nil
	}
	return apperror.Validation(problems)
}

// validateMarkerNode 校验 START / END 标记节点：engine / promptTemplate / engineConfig / displayName 必空
// （标记节点不承载配置与展示名，UI 直接渲染其 id）。
func validateMarkerNode(path string, node Node) []Problem {
	var problems []Problem
	if node.DisplayName != "" {
		problems = append(problems, markerFieldProblem(path+".displayName", node.ID, "displayName"))
	}
	if node.Engine != "" {
		problems = append(problems, markerFieldProblem(path+".engine", node.ID, "engine"))
	}
	if node.PromptTemplate != "" {
		problems = append(problems, markerFieldProblem(path+".promptTemplate", node.ID, "promptTemplate"))
	}
	if node.EngineConfig != nil {
		problems = append(problems, markerFieldProblem(path+".engineConfig", node.ID, "engineConfig"))
	}
	return problems
}

// validateAgentNode 校验 agent 节点的必填字段与格式、engine + engineConfig 能力表。
// 模板变量引用（须皆祖先）在 validateTemplateAncestry 里另行校验（依赖无环后的祖先集）。
func validateAgentNode(path string, node Node) []Problem {
	var problems []Problem
	if node.ID == "" {
		problems = append(problems, requiredProblem(path+".id", "id"))
	} else if !nodeIDPattern.MatchString(node.ID) {
		problems = append(problems, problem(path+".id", apperror.CodeInvalidNodeID, "id", node.ID))
	}
	if node.DisplayName == "" {
		problems = append(problems, requiredProblem(path+".displayName", "displayName"))
	}
	if node.PromptTemplate == "" {
		problems = append(problems, requiredProblem(path+".promptTemplate", "promptTemplate"))
	}
	problems = append(problems, validateEngine(path, node.Engine, node.EngineConfig)...)
	return problems
}

// validateEdges 校验每条边：端点存在（可含 START/END）、禁止自环 / 重复边 / 边指向 START / 边源自 END /
// START→END 直连。
func validateEdges(def *Definition, validNodeID func(string) bool) []Problem {
	var problems []Problem
	seen := make(map[string]bool, len(def.Edges))
	for i, edge := range def.Edges {
		path := fmt.Sprintf("edges[%d]", i)
		if edge.From == "" || edge.To == "" {
			problems = append(problems, Problem{Path: path, Code: apperror.CodeEdgeEndpointsRequired})
			continue
		}
		if !validNodeID(edge.From) {
			problems = append(problems, problem(path, apperror.CodeEdgeFromNodeNotFound, "id", edge.From))
		}
		if !validNodeID(edge.To) {
			problems = append(problems, problem(path, apperror.CodeEdgeToNodeNotFound, "id", edge.To))
		}
		if edge.From == edge.To {
			problems = append(problems, problem(path, apperror.CodeSelfEdge, "from", edge.From, "to", edge.To))
		}
		if edge.From == NodeIDStart && edge.To == NodeIDEnd {
			problems = append(problems, Problem{Path: path, Code: apperror.CodeStartEndDirectEdge})
		} else {
			if edge.To == NodeIDStart {
				problems = append(problems, Problem{Path: path, Code: apperror.CodeEdgeToStart})
			}
			if edge.From == NodeIDEnd {
				problems = append(problems, Problem{Path: path, Code: apperror.CodeEdgeFromEnd})
			}
		}
		key := edge.From + "\x00" + edge.To
		if seen[key] {
			problems = append(problems, problem(path, apperror.CodeDuplicateEdge, "from", edge.From, "to", edge.To))
		}
		seen[key] = true
	}
	return problems
}

// validateDegrees 校验每个 agent 节点 ≥1 入边、≥1 出边（单源单汇的落地：入度 0 只允许 START、出度 0 只允许
// END）。标记节点的度约束由 validateEdges 兜住（禁止指向 START / 源自 END）。
func validateDegrees(def *Definition) []Problem {
	inDegree := make(map[string]int, len(def.Nodes))
	outDegree := make(map[string]int, len(def.Nodes))
	for _, edge := range def.Edges {
		outDegree[edge.From]++
		inDegree[edge.To]++
	}
	var problems []Problem
	for position, node := range def.Nodes {
		if !node.IsAgent() {
			continue
		}
		path := fmt.Sprintf("nodes[%d]", position)
		if inDegree[node.ID] == 0 {
			problems = append(problems, problem(path, apperror.CodeNodeMissingIncomingEdge, "id", node.ID))
		}
		if outDegree[node.ID] == 0 {
			problems = append(problems, problem(path, apperror.CodeNodeMissingOutgoingEdge, "id", node.ID))
		}
	}
	return problems
}

// validateTemplateAncestry 校验每个 agent 节点 promptTemplate 里非转义的 {{X}}：
// {{sys.*}} 限已知系统变量；{{START}} / {{END}} 禁止（标记节点无产物）；其余须引用本节点的上游祖先 agent
// 节点（存在但非祖先、或根本不存在，均拒绝）。假定图无环（由调用方在 DetectCycle 通过后再调）。
func validateTemplateAncestry(def *Definition, validNodeID func(string) bool) []Problem {
	var problems []Problem
	for position, node := range def.Nodes {
		if !node.IsAgent() {
			continue
		}
		ancestors := Ancestors(def, node.ID)
		path := fmt.Sprintf("nodes[%d].promptTemplate", position)
		for _, match := range templateVariablePattern.FindAllStringSubmatch(node.PromptTemplate, -1) {
			full, key := match[0], match[1]
			if strings.HasPrefix(full, "\\") {
				continue // 转义 \{{x}} → 字面量，不校验
			}
			if strings.HasPrefix(key, "sys.") {
				if !knownSystemVariables[key] {
					problems = append(problems, problem(path, apperror.CodeUnknownSystemVariable, "key", key))
				}
				continue
			}
			if key == NodeIDStart || key == NodeIDEnd {
				problems = append(problems, problem(path, apperror.CodeMarkerNodeReference, "id", key))
				continue
			}
			if ancestors[key] {
				continue
			}
			if validNodeID(key) {
				problems = append(problems, problem(path, apperror.CodeNonAncestorNodeReference, "id", key))
			} else {
				problems = append(problems, problem(path, apperror.CodeNodeReferenceNotFound, "id", key))
			}
		}
	}
	return problems
}

// validateEngine 校验 engine 合法性及其 engineConfig（判别联合，依 engine 包能力表）。
func validateEngine(path, engineName string, config *EngineConfig) []Problem {
	if engineName == "" {
		return []Problem{requiredProblem(path+".engine", "engine")}
	}
	if !engine.Exists(engineName) {
		return []Problem{problem(path+".engine", apperror.CodeUnknownEngine,
			"engine", engineName, "available", strings.Join(engine.RegisteredNames(), ", "))}
	}
	if config == nil {
		return nil
	}

	var problems []Problem
	descriptor, _ := engine.Describe(engineName)
	capability := descriptor.Capability

	if config.Model != "" && !capability.AllowsModel {
		problems = append(problems, problem(path+".engineConfig.model", apperror.CodeEngineModelNotAllowed, "engine", engineName))
	}
	if config.Effort != "" {
		if !capability.AllowsEffort {
			problems = append(problems, effortFieldProblem(path+".engineConfig.effort", engineName))
		} else if !slices.Contains(capability.EffortValues, config.Effort) {
			problems = append(problems, effortValueProblem(path+".engineConfig.effort", engineName, config.Effort, capability.EffortValues))
		}
	}
	return problems
}

func problem(path string, code apperror.Code, pairs ...any) Problem {
	params := make(apperror.Params, len(pairs)/2)
	for index := 0; index+1 < len(pairs); index += 2 {
		params[pairs[index].(string)] = pairs[index+1]
	}
	if len(params) == 0 {
		params = nil
	}
	return Problem{Path: path, Code: code, Params: params}
}

func markerFieldProblem(path, id, field string) Problem {
	return problem(path, apperror.CodeMarkerFieldNotEmpty, "id", id, "field", field)
}

func requiredProblem(path, field string) Problem {
	return problem(path, apperror.CodeRequiredField, "field", field)
}

func effortFieldProblem(path, engineName string) Problem {
	return problem(path, apperror.CodeEngineEffortFieldNotAllowed, "engine", engineName)
}

func effortValueProblem(path, engineName, value string, allowed []string) Problem {
	return problem(path, apperror.CodeEngineEffortValueNotAllowed,
		"engine", engineName, "value", value, "allowed", strings.Join(allowed, ", "))
}
