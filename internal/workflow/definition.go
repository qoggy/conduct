package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// 保留标记节点的 id（判别以 id 为准，不引入独立 type 字段）。
const (
	NodeIDStart = "START" // 唯一源：无入边、不执行、不产 trace
	NodeIDEnd   = "END"   // 唯一汇：无出边、不执行、不产 trace
)

// Workflow 是一份完整的工作流记录：系统管理的元数据 + 定义主体。
// name / createdAt / updatedAt 由 store 写入，definition 是用户编写的节点与边。
type Workflow struct {
	Name       string     `json:"name"`
	CreatedAt  string     `json:"createdAt"`
	UpdatedAt  string     `json:"updatedAt"`
	Definition Definition `json:"definition"`
}

// Definition 是定义主体：节点 + 边构成的有向无环图（DAG）。
// nodes 恒含两个保留标记节点 START、END；edges 表达执行依赖（from 跑完才轮到 to）。
// 它是 edit / create --definition 的导入单元（作者只写主体，不写元数据外壳）。
type Definition struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Node 是图的单个节点。id == START / END 为标记节点（无 engine/prompt/engineConfig、不执行）；
// 其余为 agent 节点，表达一次 AI 引擎执行。判别一律走 IsStart/IsEnd/IsMarker/IsAgent，不散落裸字面串比较。
type Node struct {
	ID             string        `json:"id"`
	DisplayName    string        `json:"displayName,omitempty"`    // agent 必填；标记节点必空
	Engine         string        `json:"engine,omitempty"`         // agent 必填；标记节点必空
	EngineConfig   *EngineConfig `json:"engineConfig,omitempty"`   // 选填；标记节点必空
	PromptTemplate string        `json:"promptTemplate,omitempty"` // agent 必填；标记节点必空
}

// Edge 是一条有向边：from 跑完（成功）才轮到 to。from 可为 START、to 可为 END。
// 边只表依赖、不传数据；数据靠 promptTemplate 的 {{<id>}} 拉取。
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// EngineConfig 是引擎专属的调优载荷，其合法字段由所属 Node 的 engine 决定
// （判别联合，见 engine 包的能力表与 Validate）。所有字段选填。
type EngineConfig struct {
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`          // 仅 claude-code
	ReasoningEffort string `json:"reasoningEffort,omitempty"` // qoder / codex
}

// IsStart 报告本节点是否为保留的 START 标记节点。
func (n Node) IsStart() bool { return n.ID == NodeIDStart }

// IsEnd 报告本节点是否为保留的 END 标记节点。
func (n Node) IsEnd() bool { return n.ID == NodeIDEnd }

// IsMarker 报告本节点是否为标记节点（START / END）：无 engine/prompt、不执行、不产 trace。
func (n Node) IsMarker() bool { return n.IsStart() || n.IsEnd() }

// IsAgent 报告本节点是否为 agent 节点（一次 AI 引擎执行；engine/prompt 必填）。
func (n Node) IsAgent() bool { return !n.IsMarker() }

// ParseDefinition 严格解析一份导入定义，返回定义主体 {nodes, edges}：
//   - 导入体直接给主体 {nodes, edges} → 原样解析；
//   - 导入体给整条记录（如 show --json 输出，含 name/时间戳外壳与 definition 包裹层）→ 解包 definition、
//     忽略元数据（name 由命令参数定、时间戳由 store 管理，不因不一致报错）。
//
// 两条路径均拒绝未知字段（DisallowUnknownFields，fail-loud 暴露拼写错误）与多余尾随内容。
// 只做 JSON 结构解析，语义校验见 Validate。
func ParseDefinition(data []byte) (*Definition, error) {
	// 先探测有无 definition 外壳键：有则按整条记录解析取其 definition，无则整体按主体解析。
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("解析定义 JSON 失败: %w", err)
	}
	if _, hasDefinition := probe["definition"]; hasDefinition {
		record, err := decodeStrict[Workflow](data)
		if err != nil {
			return nil, err
		}
		return &record.Definition, nil
	}
	return decodeStrict[Definition](data)
}

// decodeStrict 把 data 严格解码为 *T：拒绝未知字段、拒绝多余尾随内容（应为单个 JSON 对象）。
func decodeStrict[T any](data []byte) (*T, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("解析定义 JSON 失败: %w", err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("解析定义 JSON 失败: 检测到多余的尾随内容（应为单个 JSON 对象）")
	}
	return &value, nil
}

// CopyAs 从本记录深拷出一份名为 name 的新工作流（造变体）：只带 name 与深拷来的定义主体，
// 不携带时间戳（CreatedAt / UpdatedAt 留空，交给 store.Create 重戳当前时刻）。
// CLI `workflow copy` 与 UI 复制端点共用此方法。
func (w *Workflow) CopyAs(name string) *Workflow {
	return &Workflow{Name: name, Definition: w.Definition.deepCopy()}
}

// deepCopy 深拷定义主体：Node 内含 EngineConfig 指针，逐个 new 一份，避免新旧共享底层指针后互相串改。
func (d Definition) deepCopy() Definition {
	nodes := make([]Node, len(d.Nodes))
	for i := range d.Nodes {
		nodes[i] = copyNode(d.Nodes[i])
	}
	edges := make([]Edge, len(d.Edges))
	copy(edges, d.Edges) // Edge 是纯值字段，浅拷即深拷
	return Definition{Nodes: nodes, Edges: edges}
}

// copyNode 深拷单个节点：值字段直接复制，EngineConfig 指针 new 一份新的底层值。
func copyNode(node Node) Node {
	copied := node // 先浅拷值字段（ID / DisplayName / Engine / PromptTemplate）
	copied.EngineConfig = copyEngineConfig(node.EngineConfig)
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

// Scaffold 返回一份最小可用的骨架定义主体（START → node-1 → END，单 agent 节点、codex、透传用户需求）。
// 外层 Workflow 由 create / store 组装（填 name + 时间戳）。
func Scaffold() Definition {
	return Definition{
		Nodes: []Node{
			{ID: NodeIDStart},
			{ID: "node-1", DisplayName: "node-1", Engine: "codex", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: NodeIDEnd},
		},
		Edges: []Edge{
			{From: NodeIDStart, To: "node-1"},
			{From: "node-1", To: NodeIDEnd},
		},
	}
}

// AgentNodeCount 返回定义里的 agent 节点数（排除 START / END）——进度分母 N。
func (d Definition) AgentNodeCount() int {
	count := 0
	for _, node := range d.Nodes {
		if node.IsAgent() {
			count++
		}
	}
	return count
}
