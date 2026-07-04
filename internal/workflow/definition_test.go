package workflow

import "testing"

func TestParseDefinitionRejectsUnknownField(t *testing.T) {
	// promptTemplate 拼错成 promtTemplate → 未知字段，应被 fail-loud 拒绝
	data := []byte(`{"nodes":[{"id":"a","displayName":"A","engine":"claude-code","promtTemplate":"hi"}]}`)
	if _, err := ParseDefinition(data); err == nil {
		t.Fatal("期望拒绝未知字段 promtTemplate，却通过了")
	}
}

func TestParseDefinitionRejectsTrailingContent(t *testing.T) {
	data := []byte(`{"nodes":[]}{"x":1}`)
	if _, err := ParseDefinition(data); err == nil {
		t.Fatal("期望拒绝多余尾随内容，却通过了")
	}
}

func TestNormalizeFillsLoopCountOnlyWhereApplicable(t *testing.T) {
	def := &Definition{Nodes: []Node{
		{ID: "a", DisplayName: "A", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}},
		{ID: "b", DisplayName: "B", Engine: "claude-code", PromptTemplate: "y"},
	}}
	def.Normalize()
	if def.Nodes[0].LoopCount == nil || *def.Nodes[0].LoopCount != 1 {
		t.Errorf("带 evaluator 的节点 loopCount 应补为 1，得到 %v", def.Nodes[0].LoopCount)
	}
	if def.Nodes[1].LoopCount != nil {
		t.Errorf("无 evaluator/redoTarget 的节点不应补 loopCount，得到 %v", def.Nodes[1].LoopCount)
	}
}
