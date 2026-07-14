package workflow

import (
	"reflect"
	"strings"
	"testing"
)

// renameFixture 造 START→a→b→END：b 的模板引用 {{a}}，并含一处转义 \{{a}} 作对照（改名不得动它）。
func renameFixture() *Definition {
	return &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: `看 {{a}} 与 \{{a}}`},
			{ID: "END"},
		},
		Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "b"}, {From: "b", To: "END"}},
	}
}

func TestRenameNodeIDCascades(t *testing.T) {
	def := renameFixture()
	if err := RenameNodeID(def, "a", "plan"); err != nil {
		t.Fatalf("改名不应报错: %v", err)
	}
	if def.Nodes[1].ID != "plan" {
		t.Errorf("节点 id 应为 plan，得到 %q", def.Nodes[1].ID)
	}
	wantEdges := []Edge{{From: "START", To: "plan"}, {From: "plan", To: "b"}, {From: "b", To: "END"}}
	if !reflect.DeepEqual(def.Edges, wantEdges) {
		t.Errorf("边未级联改名，得到 %+v", def.Edges)
	}
	// 活引用 {{a}}→{{plan}}；转义 \{{a}} 保持字面量不动。
	if got := def.Nodes[2].PromptTemplate; got != `看 {{plan}} 与 \{{a}}` {
		t.Errorf("模板引用级联错误，得到 %q", got)
	}
}

func TestRenameNodeIDNoopSameID(t *testing.T) {
	def := renameFixture()
	if err := RenameNodeID(def, "a", "a"); err != nil {
		t.Fatalf("同名 id 不应报错: %v", err)
	}
	if !reflect.DeepEqual(def, renameFixture()) {
		t.Error("newID == oldID 应为空操作、定义不变")
	}
}

func TestRenameNodeIDRejects(t *testing.T) {
	cases := []struct {
		name, oldID, newID, wantSub string
	}{
		{"保留名 END", "a", "END", "保留名"},
		{"非法 id", "a", "1x", "非法"},
		{"重名", "a", "b", "已存在同名"},
		{"改标记节点", "START", "s", "标记节点"},
		{"节点不存在", "ghost", "x", "无节点"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := renameFixture()
			err := RenameNodeID(def, c.oldID, c.newID)
			if err == nil {
				t.Fatalf("应拒绝 %s→%s", c.oldID, c.newID)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("错误信息应含 %q，得到 %q", c.wantSub, err.Error())
			}
			if !reflect.DeepEqual(def, renameFixture()) {
				t.Error("拒绝时定义不得被改动")
			}
		})
	}
}
