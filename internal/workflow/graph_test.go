package workflow

import (
	"reflect"
	"strings"
	"testing"
)

// diamond 是菱形图 START→a→{b,c}→d→END，供图算法测试复用。
func diamond() *Definition {
	return &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "a", DisplayName: "a", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "b", DisplayName: "b", Engine: "claude-code", PromptTemplate: "{{a}}"},
			{ID: "c", DisplayName: "c", Engine: "claude-code", PromptTemplate: "{{a}}"},
			{ID: "d", DisplayName: "d", Engine: "claude-code", PromptTemplate: "{{b}}{{c}}"},
			{ID: "END"},
		},
		Edges: []Edge{
			{From: "START", To: "a"},
			{From: "a", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "d"},
			{From: "c", To: "d"},
			{From: "d", To: "END"},
		},
	}
}

func TestAgentNodeIDs(t *testing.T) {
	// 菱形 START→a→{b,c}→d→END：拓扑序 a（层0）、b/c（层1）、d（层2），不含 START/END。
	if got := AgentNodeIDs(diamond()); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("AgentNodeIDs = %v，期望 [a b c d]（确定性拓扑序）", got)
	}
	if got := AgentNodeIDs(&Definition{}); len(got) != 0 {
		t.Errorf("空定义应返回空切片，得到 %v", got)
	}
}

func TestDetectCycleAcyclic(t *testing.T) {
	if cycle := DetectCycle(diamond()); cycle != nil {
		t.Errorf("菱形图无环，却报 %v", cycle)
	}
}

func TestDetectCycleFindsCycle(t *testing.T) {
	def := &Definition{
		Nodes: []Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "c", To: "a"}},
	}
	cycle := DetectCycle(def)
	if cycle == nil {
		t.Fatal("应探测到环，却返回 nil")
	}
	// 环路径首尾同 id，便于打印 a→b→c→a。
	if cycle[0] != cycle[len(cycle)-1] {
		t.Errorf("环路径首尾应同 id，得到 %v", cycle)
	}
}

func TestAncestors(t *testing.T) {
	def := diamond()
	got := Ancestors(def, "d")
	// d 的祖先：b、c、a、START。
	for _, want := range []string{"a", "b", "c", "START"} {
		if !got[want] {
			t.Errorf("d 的祖先应含 %q，实际 %v", want, got)
		}
	}
	if got["d"] {
		t.Error("祖先集不应含节点自身")
	}
	if got["END"] {
		t.Error("END 不应是任何节点的祖先")
	}

	// b 的祖先只有 a、START，不含 c（并行分支互不为祖先）。
	bAnc := Ancestors(def, "b")
	if bAnc["c"] {
		t.Errorf("b 不应以并行分支 c 为祖先，实际 %v", bAnc)
	}
}

func TestTopoLevels(t *testing.T) {
	// START→plan→{code,test}→review→END。
	def := &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "{{plan}}"},
			{ID: "test", DisplayName: "测试", Engine: "claude-code", PromptTemplate: "{{plan}}"},
			{ID: "review", DisplayName: "评审", Engine: "claude-code", PromptTemplate: "{{code}}{{test}}"},
			{ID: "END"},
		},
		Edges: []Edge{
			{From: "START", To: "plan"},
			{From: "plan", To: "code"},
			{From: "plan", To: "test"},
			{From: "code", To: "review"},
			{From: "test", To: "review"},
			{From: "review", To: "END"},
		},
	}
	got := TopoLevels(def)
	want := [][]string{{"plan"}, {"code", "test"}, {"review"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopoLevels = %v，期望 %v", got, want)
	}
}

// TestTopoLevelsAllParallel 确认 START 扇出的多个节点落在同一层 level 0。
func TestTopoLevelsAllParallel(t *testing.T) {
	def := &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "a", DisplayName: "a", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "b", DisplayName: "b", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "END"},
		},
		Edges: []Edge{
			{From: "START", To: "a"},
			{From: "START", To: "b"},
			{From: "a", To: "END"},
			{From: "b", To: "END"},
		},
	}
	got := TopoLevels(def)
	want := [][]string{{"a", "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopoLevels = %v，期望 %v", got, want)
	}
}

// TestTopoLevelsOrphanAgentNoPanic 防御非法输入：agent "orphan" 无入边（落 −1 层），与正常 agent
// "b"（level 0）混在一张图里。TopoLevels 约定输入已过校验，但一旦被喂非法图也不得 levels[-1] 越界
// panic——无层号的 agent 跳过，仅返回有效层。
func TestTopoLevelsOrphanAgentNoPanic(t *testing.T) {
	def := &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "orphan", DisplayName: "无入边", Engine: "claude-code", PromptTemplate: "x"},
			{ID: "b", DisplayName: "b", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "END"},
		},
		Edges: []Edge{
			{From: "START", To: "b"},
			{From: "b", To: "END"},
			{From: "orphan", To: "END"}, // orphan 有出边无入边 → 被当作源、落 −1 层
		},
	}
	got := TopoLevels(def)
	want := [][]string{{"b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TopoLevels = %v，期望 %v（orphan 无层号应被跳过而非 panic）", got, want)
	}
}

// TestDetectCyclePrintable 确认环路径能拼成可读串。
func TestDetectCyclePrintable(t *testing.T) {
	def := &Definition{
		Nodes: []Node{{ID: "x"}, {ID: "y"}},
		Edges: []Edge{{From: "x", To: "y"}, {From: "y", To: "x"}},
	}
	cycle := DetectCycle(def)
	joined := strings.Join(cycle, "→")
	if !strings.Contains(joined, "→") {
		t.Errorf("环路径应可拼接为 x→y→x 形态，得到 %q", joined)
	}
}
