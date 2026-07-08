package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Definition 是一份完整的工作流记录：系统管理的元数据 + 用户编写的节点定义。
// name / createdAt / updatedAt 由 store 写入，nodes 是定义主体。
type Definition struct {
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	Nodes     []Node `json:"nodes"`
}

// Node 是工作流的单个节点。evaluator 与 redoTarget 互斥（见 Validate）。
type Node struct {
	ID             string        `json:"id"`
	DisplayName    string        `json:"displayName"`
	Engine         string        `json:"engine"`
	EngineConfig   *EngineConfig `json:"engineConfig,omitempty"`
	PromptTemplate string        `json:"promptTemplate"`
	Evaluator      *Evaluator    `json:"evaluator,omitempty"`
	RedoTarget     string        `json:"redoTarget,omitempty"`
	LoopCount      *int          `json:"loopCount,omitempty"`
}

// EngineConfig 是引擎专属的调优载荷，其合法字段由所属 Node/Evaluator 的 engine 决定
// （判别联合，见 engine 包的能力表与 Validate）。所有字段选填。
type EngineConfig struct {
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`          // 仅 claude-code
	ReasoningEffort string `json:"reasoningEffort,omitempty"` // qoder / codex
}

// Evaluator 是节点内联的评测官，触发 in-place 内循环；engine + engineConfig 同构于 Node。
type Evaluator struct {
	Engine         string        `json:"engine"`
	EngineConfig   *EngineConfig `json:"engineConfig,omitempty"`
	PromptTemplate string        `json:"promptTemplate"`
}

// ParseDefinition 严格解析一份导入定义：拒绝未知字段（fail-loud，尽早暴露拼写错误）。
// 只做 JSON 结构解析，语义校验见 Validate。
func ParseDefinition(data []byte) (*Definition, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definition Definition
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("解析定义 JSON 失败: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("解析定义 JSON 失败: 检测到多余的尾随内容（应为单个 JSON 对象）")
	}
	return &definition, nil
}

// Normalize 补齐规范化默认值：带 evaluator / redoTarget 的节点若未写 loopCount，则补为 1。
func (d *Definition) Normalize() {
	for i := range d.Nodes {
		node := &d.Nodes[i]
		if (node.Evaluator != nil || node.RedoTarget != "") && node.LoopCount == nil {
			one := 1
			node.LoopCount = &one
		}
	}
}

// CopyAs 从本定义深拷出一份名为 name 的新定义（造变体）：只带 name 与深拷来的 nodes，
// 不携带时间戳（CreatedAt / UpdatedAt 留空，交给 store.Create 重戳当前时刻）。
// nodes 必须深拷——Node 内含指针字段（EngineConfig / Evaluator / LoopCount），逐个 new 一份，
// 避免新旧定义共享底层指针后互相串改。CLI `workflow copy` 与 UI 复制端点共用此方法。
func (d *Definition) CopyAs(name string) *Definition {
	nodes := make([]Node, len(d.Nodes))
	for i := range d.Nodes {
		nodes[i] = copyNode(d.Nodes[i])
	}
	return &Definition{
		Name:  name,
		Nodes: nodes,
	}
}

// copyNode 深拷单个节点：值字段直接复制，三个指针字段各 new 一份新的底层值。
func copyNode(node Node) Node {
	copied := node // 先浅拷值字段（ID / DisplayName / Engine / PromptTemplate / RedoTarget）
	copied.EngineConfig = copyEngineConfig(node.EngineConfig)
	if node.Evaluator != nil {
		evaluator := *node.Evaluator
		evaluator.EngineConfig = copyEngineConfig(node.Evaluator.EngineConfig)
		copied.Evaluator = &evaluator
	}
	if node.LoopCount != nil {
		loopCount := *node.LoopCount
		copied.LoopCount = &loopCount
	}
	return copied
}

// copyEngineConfig 深拷 EngineConfig 指针（nil 原样返回 nil）。
func copyEngineConfig(config *EngineConfig) *EngineConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	return &cloned
}

// DefaultEvaluatorPrompt 是首次挂载 evaluator 时自动补的默认评测提示词（自包含、不写 {{<节点id>}} 自引用）。
const DefaultEvaluatorPrompt = "你是独立质量评测官。审阅下面待评产物的正确性、完整性与质量，给出具体、可执行的改进反馈。"

// Scaffold 返回一份最小可用的骨架定义（单节点、claude-code、透传用户需求）。
// name 与时间戳由 store 写入。
func Scaffold() *Definition {
	return &Definition{
		Nodes: []Node{{
			ID:             "node-1",
			DisplayName:    "node-1",
			Engine:         "claude-code",
			PromptTemplate: "{{sys.userPrompt}}",
		}},
	}
}
