package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// —— --prompt 补换行与 set-prompt 剥换行成对：round-trip 字节稳定 ——

func TestNodeShowPromptRoundTripByteStable(t *testing.T) {
	samples := []string{
		"",                 // 空
		"单行无尾换行",           // 无尾换行
		"含 {{变量}} 与\n内部换行", // 含内部换行、模板变量
		"末尾已有换行\n",         // 尾部本就有一个换行
		"多行\n\n带空行\n",      // 多个换行
	}
	for _, p := range samples {
		rendered := appendOneTrailingNewline(p)
		if got := stripOneTrailingNewline([]byte(rendered)); got != p {
			t.Fatalf("round-trip 应字节稳定：原文 %q，补换行后剥回得到 %q", p, got)
		}
	}
}

// —— 单节点 --json 规范化：Normalize 后节点补齐 loopCount 默认值 ——

func TestNodeShowJSONNormalizesLoopCount(t *testing.T) {
	// evaluator 节点未写 loopCount，Normalize 后应补 1。
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评"}
	def := defWith(node)

	def.Normalize()
	got, err := findNode(def, "gen")
	if err != nil {
		t.Fatalf("定位节点不应报错: %v", err)
	}
	if got.LoopCount == nil || *got.LoopCount != 1 {
		t.Fatalf("Normalize 后 loopCount 应补默认 1，得到 %v", got.LoopCount)
	}
}

// —— 单节点 --evaluator --json：取到正确的评测官对象 ——

func TestNodeShowJSONEvaluatorObject(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{
		Engine:         "qoder",
		EngineConfig:   &workflow.EngineConfig{Model: "qwen"},
		PromptTemplate: "审阅",
	}
	def := defWith(node)

	def.Normalize()
	got, err := findNode(def, "gen")
	if err != nil {
		t.Fatalf("定位节点不应报错: %v", err)
	}
	if got.Evaluator == nil || got.Evaluator.Engine != "qoder" || got.Evaluator.PromptTemplate != "审阅" {
		t.Fatalf("应取到正确的评测官对象，得到 %+v", got.Evaluator)
	}
	if got.Evaluator.EngineConfig == nil || got.Evaluator.EngineConfig.Model != "qwen" {
		t.Fatalf("评测官 engineConfig 应保留，得到 %+v", got.Evaluator.EngineConfig)
	}
}

// —— printNodeShowHuman：锁定单节点 / 评测官人类可读摘要的契约串（spec〈输出〉+ design D8）——

func TestPrintNodeShowHumanNodeSummary(t *testing.T) {
	node := plainNode("gen")
	node.DisplayName = "生成器"
	node.EngineConfig = &workflow.EngineConfig{Model: "claude-sonnet-5"}
	node.PromptTemplate = "做事\n分两步"

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	if err := printNodeShowHuman(cmd, &node, false); err != nil {
		t.Fatalf("打印节点详情不应报错: %v", err)
	}
	got := buf.String()
	// 摘要行：id · displayName · engine · model · 循环模式；随后空行 + 提示词全文不截断。
	if want := "gen · 生成器 · claude-code · claude-sonnet-5 · 单次\n\n做事\n分两步\n"; got != want {
		t.Fatalf("节点摘要契约串应为 %q，得到 %q", want, got)
	}
}

func TestPrintNodeShowHumanEvaluatorSummary(t *testing.T) {
	node := plainNode("gen")
	node.Evaluator = &workflow.Evaluator{
		Engine:         "qoder",
		EngineConfig:   &workflow.EngineConfig{Model: "qwen"},
		PromptTemplate: "审阅",
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	if err := printNodeShowHuman(cmd, &node, true); err != nil {
		t.Fatalf("打印评测官详情不应报错: %v", err)
	}
	got := buf.String()
	// 评测官摘要：<父节点id>·evaluator · engine · model（无 displayName / 循环模式，见 design D8）。
	if want := "gen·evaluator · qoder · qwen\n\n审阅\n"; got != want {
		t.Fatalf("评测官摘要契约串应为 %q，得到 %q", want, got)
	}
	if strings.Contains(got, "单次") {
		t.Fatalf("评测官摘要不应含节点循环模式串，得到 %q", got)
	}
}
