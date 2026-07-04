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
