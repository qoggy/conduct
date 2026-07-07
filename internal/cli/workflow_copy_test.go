package cli

import (
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

// srcWithPointers 构造一份带全部指针字段（EngineConfig / Evaluator / LoopCount）的源定义，用于验证深拷。
// intPtr 复用同包 workflow_node_set_test.go 里的定义。
func srcWithPointers() *workflow.Definition {
	return &workflow.Definition{
		Name:      "src",
		CreatedAt: "2026-07-03T10:00:00Z",
		UpdatedAt: "2026-07-03T10:05:00Z",
		Nodes: []workflow.Node{{
			ID:             "gen",
			DisplayName:    "生成",
			Engine:         "claude-code",
			PromptTemplate: "{{sys.userPrompt}}",
			EngineConfig:   &workflow.EngineConfig{Model: "sonnet", Effort: "high"},
			Evaluator: &workflow.Evaluator{
				Engine:         "claude-code",
				EngineConfig:   &workflow.EngineConfig{Model: "opus"},
				PromptTemplate: "审阅",
			},
			LoopCount: intPtr(3),
		}},
	}
}

func TestBuildCopiedDefinitionRenamesAndDropsTimestamps(t *testing.T) {
	src := srcWithPointers()
	copied := buildCopiedDefinition(src, "dst")

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

func TestBuildCopiedDefinitionDeepCopiesPointerFields(t *testing.T) {
	src := srcWithPointers()
	copied := buildCopiedDefinition(src, "dst")

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

func TestBuildCopiedDefinitionHandlesNilPointers(t *testing.T) {
	// 最小节点（无任何指针字段）不应 panic，指针字段应保持 nil。
	src := &workflow.Definition{
		Name: "src",
		Nodes: []workflow.Node{{
			ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}",
		}},
	}
	copied := buildCopiedDefinition(src, "dst")
	node := copied.Nodes[0]
	if node.EngineConfig != nil || node.Evaluator != nil || node.LoopCount != nil {
		t.Errorf("nil 指针字段应保持 nil，得到 %+v", node)
	}
}
