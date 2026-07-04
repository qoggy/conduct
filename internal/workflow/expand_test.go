package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func loadFixture(t *testing.T, name string) *Definition {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读取 fixture %s 失败: %v", name, err)
	}
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatalf("解析 fixture %s 失败: %v", name, err)
	}
	return def
}

// TestExpandRegressionAnchors 锁定展开算法的回归锚点（Python 原型实测值）。
func TestExpandRegressionAnchors(t *testing.T) {
	cases := []struct {
		fixture string
		want    int
	}{
		{"wf_autopilot.json", 14},
		{"wf_demo.json", 4},
	}
	for _, c := range cases {
		def := loadFixture(t, c.fixture)
		if got := len(Expand(def.Nodes)); got != c.want {
			t.Errorf("%s: 展开步数 = %d, 期望 %d", c.fixture, got, c.want)
		}
	}
}

// TestExpandDemoSequence 校验 in-place 内循环的精确步序：agent→eval→agent（末轮不评）+ 下一节点。
func TestExpandDemoSequence(t *testing.T) {
	def := loadFixture(t, "wf_demo.json")
	want := []ExecutionStep{
		{Type: "agent", NodeID: "name", Iteration: 1},
		{Type: "evaluator", NodeID: "name", Iteration: 1},
		{Type: "agent", NodeID: "name", Iteration: 2},
		{Type: "agent", NodeID: "slogan", Iteration: 1},
	}
	if got := Expand(def.Nodes); !reflect.DeepEqual(got, want) {
		t.Errorf("展开序列不符:\n得到 %+v\n期望 %+v", got, want)
	}
}

// TestExpandRedoOverSelfLoopSequence 校验「自循环 + redo 叠加」下的精确步序（回归锚点只钉步数，
// 这里钉逐步顺序，防止段循环把内层自循环展开错位而步数恰好不变的 bug）。
// 结构：A → B(自循环 loopCount=1，带 evaluator) → C(redoTarget=A, loopCount=1)。
// 期望：A → B → B-Eval → B → C → A → B → B-Eval → B → C（redo 段把 [A,B] 连同 B 的内循环整段重跑一轮，
// C 为段尾）。段循环内各步迭代号统一取段循环轮号（见 expand.go 的 outerIteration 语义）。
func TestExpandRedoOverSelfLoopSequence(t *testing.T) {
	one := 1
	nodes := []Node{
		{ID: "A", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "a"},
		{ID: "B", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "b",
			LoopCount: &one, Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}},
		{ID: "C", DisplayName: "丙", Engine: "claude-code", PromptTemplate: "c",
			LoopCount: &one, RedoTarget: "A"},
	}
	want := []ExecutionStep{
		{Type: "agent", NodeID: "A", Iteration: 1},
		{Type: "agent", NodeID: "B", Iteration: 1},
		{Type: "evaluator", NodeID: "B", Iteration: 1},
		{Type: "agent", NodeID: "B", Iteration: 1},
		{Type: "agent", NodeID: "C", Iteration: 1},
		{Type: "agent", NodeID: "A", Iteration: 2},
		{Type: "agent", NodeID: "B", Iteration: 2},
		{Type: "evaluator", NodeID: "B", Iteration: 2},
		{Type: "agent", NodeID: "B", Iteration: 2},
		{Type: "agent", NodeID: "C", Iteration: 2},
	}
	if got := Expand(nodes); !reflect.DeepEqual(got, want) {
		t.Errorf("自循环+redo 叠加展开序列不符:\n得到 %+v\n期望 %+v", got, want)
	}
}
