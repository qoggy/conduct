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

// srcWithPointers 构造一份带全部指针字段（EngineConfig / Evaluator / LoopCount）的源定义，用于验证 CopyAs 深拷。
func srcWithPointers() *Definition {
	three := 3
	return &Definition{
		Name:      "src",
		CreatedAt: "2026-07-03T10:00:00Z",
		UpdatedAt: "2026-07-03T10:05:00Z",
		Nodes: []Node{{
			ID:             "gen",
			DisplayName:    "生成",
			Engine:         "claude-code",
			PromptTemplate: "{{sys.userPrompt}}",
			EngineConfig:   &EngineConfig{Model: "sonnet", Effort: "high"},
			Evaluator: &Evaluator{
				Engine:         "claude-code",
				EngineConfig:   &EngineConfig{Model: "opus"},
				PromptTemplate: "审阅",
			},
			LoopCount: &three,
		}},
	}
}

func TestCopyAsRenamesAndDropsTimestamps(t *testing.T) {
	src := srcWithPointers()
	copied := src.CopyAs("dst")

	if copied.Name != "dst" {
		t.Errorf("name 应改为 dst，得到 %q", copied.Name)
	}
	if copied.CreatedAt != "" || copied.UpdatedAt != "" {
		t.Errorf("不应携带源时间戳，得到 createdAt=%q updatedAt=%q", copied.CreatedAt, copied.UpdatedAt)
	}
	if len(copied.Nodes) != 1 || copied.Nodes[0].ID != "gen" {
		t.Fatalf("nodes 未正确复制: %+v", copied.Nodes)
	}
}

func TestCopyAsDeepCopiesPointerFields(t *testing.T) {
	src := srcWithPointers()
	copied := src.CopyAs("dst")

	cNode := &copied.Nodes[0]
	sNode := &src.Nodes[0]

	// 指针不得共享底层。
	if cNode.EngineConfig == sNode.EngineConfig {
		t.Error("EngineConfig 指针被浅拷共享")
	}
	if cNode.Evaluator == sNode.Evaluator {
		t.Error("Evaluator 指针被浅拷共享")
	}
	if cNode.Evaluator.EngineConfig == sNode.Evaluator.EngineConfig {
		t.Error("Evaluator.EngineConfig 指针被浅拷共享")
	}
	if cNode.LoopCount == sNode.LoopCount {
		t.Error("LoopCount 指针被浅拷共享")
	}

	// 改动 copied 的指针指向值，src 不得受影响。
	cNode.EngineConfig.Model = "haiku"
	cNode.EngineConfig.Effort = "low"
	cNode.Evaluator.EngineConfig.Model = "改了"
	cNode.Evaluator.PromptTemplate = "改了"
	*cNode.LoopCount = 20

	if sNode.EngineConfig.Model != "sonnet" || sNode.EngineConfig.Effort != "high" {
		t.Errorf("src.EngineConfig 被串改: %+v", sNode.EngineConfig)
	}
	if sNode.Evaluator.EngineConfig.Model != "opus" {
		t.Errorf("src.Evaluator.EngineConfig 被串改: %+v", sNode.Evaluator.EngineConfig)
	}
	if sNode.Evaluator.PromptTemplate != "审阅" {
		t.Errorf("src.Evaluator.PromptTemplate 被串改: %q", sNode.Evaluator.PromptTemplate)
	}
	if *sNode.LoopCount != 3 {
		t.Errorf("src.LoopCount 被串改: %d", *sNode.LoopCount)
	}
}

func TestCopyAsHandlesNilPointers(t *testing.T) {
	// 最小节点（无任何指针字段）不应 panic，指针字段应保持 nil。
	src := &Definition{
		Name: "src",
		Nodes: []Node{{
			ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}",
		}},
	}
	copied := src.CopyAs("dst")
	node := copied.Nodes[0]
	if node.EngineConfig != nil || node.Evaluator != nil || node.LoopCount != nil {
		t.Errorf("nil 指针字段应保持 nil，得到 %+v", node)
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
