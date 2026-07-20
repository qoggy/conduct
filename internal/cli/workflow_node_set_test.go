package cli

import (
	"reflect"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

// —— 测试夹具 helper ——

func strPtr(s string) *string { return &s }

// plainNode 返回一个 claude-code agent 节点。
func plainNode(id string) workflow.Node {
	return workflow.Node{ID: id, DisplayName: id, Engine: "claude-code", PromptTemplate: "做事"}
}

// defWith 把给定 agent 节点串成一条合法 DAG：START → n1 → n2 → … → END，供 node set / show 测试。
func defWith(nodes ...workflow.Node) *workflow.Definition {
	all := make([]workflow.Node, 0, len(nodes)+2)
	all = append(all, workflow.Node{ID: "START"})
	all = append(all, nodes...)
	all = append(all, workflow.Node{ID: "END"})

	edges := make([]workflow.Edge, 0, len(nodes)+1)
	prev := "START"
	for _, n := range nodes {
		edges = append(edges, workflow.Edge{From: prev, To: n.ID})
		prev = n.ID
	}
	edges = append(edges, workflow.Edge{From: prev, To: "END"})
	return &workflow.Definition{Nodes: all, Edges: edges}
}

// nodeByID 在 def 里按 id 取节点（测试断言用）。
func nodeByID(t *testing.T, def *workflow.Definition, id string) *workflow.Node {
	t.Helper()
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i]
		}
	}
	t.Fatalf("定义里找不到节点 %s", id)
	return nil
}

// —— applyNodeSet：空串清除 ——

func TestApplyNodeSetClearScalarByEmptyString(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Model: "claude-opus-4-8", Effort: "high"}
	def := defWith(node)

	if err := applyNodeSet(def, "gen", nodeSetOptions{Effort: strPtr("")}); err != nil {
		t.Fatalf("清除 effort 不应报错: %v", err)
	}
	got := nodeByID(t, def, "gen").EngineConfig
	if got == nil || got.Model != "claude-opus-4-8" || got.Effort != "" {
		t.Fatalf("effort 应被清除、model 保留，得到 %+v", got)
	}
}

func TestApplyNodeSetClearCollapsesConfigToNil(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Model: "claude-opus-4-8"}
	def := defWith(node)

	if err := applyNodeSet(def, "gen", nodeSetOptions{Model: strPtr("")}); err != nil {
		t.Fatalf("清除 model 不应报错: %v", err)
	}
	if nodeByID(t, def, "gen").EngineConfig != nil {
		t.Fatalf("清空后 EngineConfig 应塌缩为 nil，得到 %+v", nodeByID(t, def, "gen").EngineConfig)
	}
}

// —— applyNodeSet：改结构化字段 ——

func TestApplyNodeSetUpdatesEngineAndDisplayName(t *testing.T) {
	def := defWith(plainNode("gen"))
	if err := applyNodeSet(def, "gen", nodeSetOptions{Engine: strPtr("codex"), DisplayName: strPtr("生成器")}); err != nil {
		t.Fatalf("改引擎 + 显示名不应报错: %v", err)
	}
	node := nodeByID(t, def, "gen")
	if node.Engine != "codex" || node.DisplayName != "生成器" {
		t.Fatalf("引擎 / 显示名应更新，得到 engine=%q display=%q", node.Engine, node.DisplayName)
	}
}

func TestApplyNodeSetEmptyDisplayNameRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if err := applyNodeSet(def, "gen", nodeSetOptions{DisplayName: strPtr("")}); err == nil {
		t.Fatal("空 displayName 应报错")
	}
}

// —— applyNodeSet：目标非 agent 节点 ——

func TestApplyNodeSetNodeNotFound(t *testing.T) {
	def := defWith(plainNode("gen"))
	if err := applyNodeSet(def, "ghost", nodeSetOptions{Model: strPtr("x")}); err == nil {
		t.Fatal("节点不存在应报错")
	}
}

func TestApplyNodeSetMarkerNodeRejected(t *testing.T) {
	def := defWith(plainNode("gen"))
	if err := applyNodeSet(def, "START", nodeSetOptions{Model: strPtr("x")}); err == nil {
		t.Fatal("目标为保留标记节点应报错")
	}
}

// —— applyNodeSet：改 id 级联改名（接线 workflow.RenameNodeID）——

func TestApplyNodeSetRenamesIDWithCascade(t *testing.T) {
	a := plainNode("a")
	b := workflow.Node{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "看 {{a}}"}
	def := defWith(a, b) // START→a→b→END

	if err := applyNodeSet(def, "a", nodeSetOptions{NewID: strPtr("plan")}); err != nil {
		t.Fatalf("改 id 不应报错: %v", err)
	}
	nodeByID(t, def, "plan") // 新 id 存在（找不到会 Fatal）
	if got := nodeByID(t, def, "b").PromptTemplate; got != "看 {{plan}}" {
		t.Errorf("下游模板引用应级联为 {{plan}}，得到 %q", got)
	}
	// 边 START→plan、plan→b 级联更新。
	wantEdges := []workflow.Edge{{From: "START", To: "plan"}, {From: "plan", To: "b"}, {From: "b", To: "END"}}
	if !reflect.DeepEqual(def.Edges, wantEdges) {
		t.Errorf("边未级联改名，得到 %+v", def.Edges)
	}
}

func TestApplyNodeSetRenameWithOtherFields(t *testing.T) {
	def := defWith(plainNode("a"))
	if err := applyNodeSet(def, "a", nodeSetOptions{NewID: strPtr("plan"), DisplayName: strPtr("规划")}); err != nil {
		t.Fatalf("同时改 id + 显示名不应报错: %v", err)
	}
	node := nodeByID(t, def, "plan")
	if node.DisplayName != "规划" {
		t.Errorf("显示名应同批更新，得到 %q", node.DisplayName)
	}
}

func TestApplyNodeSetRenameDuplicateRejected(t *testing.T) {
	def := defWith(plainNode("a"), plainNode("b"))
	if err := applyNodeSet(def, "a", nodeSetOptions{NewID: strPtr("b")}); err == nil {
		t.Fatal("改成已存在的 id 应报错")
	}
}

// —— applyNodeSet + Validate：改 engine 级联不兼容由整份校验捕获 ——

func TestApplyNodeSetEngineSwitchCaughtByValidate(t *testing.T) {
	node := plainNode("gen")
	node.EngineConfig = &workflow.EngineConfig{Effort: "high"} // claude-code 专属
	def := defWith(node)

	// 只改 engine 为 antigravity，不清 effort → applyNodeSet 不预判，交整份校验。
	if err := applyNodeSet(def, "gen", nodeSetOptions{Engine: strPtr("antigravity")}); err != nil {
		t.Fatalf("applyNodeSet 不应预判引擎兼容性: %v", err)
	}
	if err := workflow.Validate(def); err == nil {
		t.Fatal("antigravity 不认 effort，整份校验应报错")
	}
}

// —— applyEngineConfig：三字段全空塌缩为 nil ——

func TestApplyEngineConfigCollapsesToNil(t *testing.T) {
	var carrier *workflow.EngineConfig = &workflow.EngineConfig{Model: "m"}
	applyEngineConfig(&carrier, nodeSetOptions{Model: strPtr("")})
	if carrier != nil {
		t.Fatalf("清空唯一字段后应塌缩为 nil，得到 %+v", carrier)
	}
}

func TestApplyEngineConfigUntouchedWhenNoScalar(t *testing.T) {
	original := &workflow.EngineConfig{Model: "m"}
	carrier := original
	applyEngineConfig(&carrier, nodeSetOptions{DisplayName: strPtr("x")}) // 无 model/effort
	if carrier != original {
		t.Fatalf("未给调优标量时不应改动 EngineConfig 指针")
	}
}

// —— checkNodeSetFlagCombo：无字段选项退 2；合法通过 ——

func TestCheckNodeSetFlagComboNoOperation(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{}); err == nil {
		t.Fatal("无任何字段选项应报用法错误")
	} else if _, ok := err.(*usageError); !ok {
		t.Fatalf("应为 usageError（退 2），得到 %T", err)
	}
}

func TestCheckNodeSetFlagComboValidPasses(t *testing.T) {
	if err := checkNodeSetFlagCombo(nodeSetOptions{Model: strPtr("x")}); err != nil {
		t.Fatalf("合法组合不应报错: %v", err)
	}
	if err := checkNodeSetFlagCombo(nodeSetOptions{DisplayName: strPtr("名")}); err != nil {
		t.Fatalf("单独 --display-name 不应报错: %v", err)
	}
	if err := checkNodeSetFlagCombo(nodeSetOptions{NewID: strPtr("x")}); err != nil {
		t.Fatalf("单独 --id 不应报错: %v", err)
	}
}
