package run

import (
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/workflow"
)

func sampleRecord() *Record {
	ended := "2026-07-03T15:22:51+08:00"
	return &Record{
		ID:       "autopilot-20260703-152233",
		Workflow: "autopilot",
		WorkflowSnapshot: &workflow.Workflow{
			Name:      "autopilot",
			UpdatedAt: "2026-07-03T15:40:00+08:00",
			Definition: workflow.Definition{
				Nodes: []workflow.Node{
					{ID: "START"},
					{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "x"},
					{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "{{plan}}"},
					{ID: "END"},
				},
				Edges: []workflow.Edge{
					{From: "START", To: "plan"}, {From: "plan", To: "code"}, {From: "code", To: "END"},
				},
			},
		},
		UserPrompt: "给购物车加一个清空按钮",
		Cwd:        "/Users/me/proj",
		Status:     StatusCompleted,
		Language:   locale.Chinese,
		StartedAt:  "2026-07-03T15:22:33+08:00",
		EndedAt:    &ended,
		Artifacts:  map[string]string{"plan": "# 方案\n加按钮", "code": "diff --git ..."},
	}
}

func TestRenderSummaryCompleted(t *testing.T) {
	trace := []TraceEntry{
		{NodeID: "plan", DisplayName: "规划", Engine: "claude-code",
			EngineConfig: &workflow.EngineConfig{Model: "claude-opus-4-8"}, Success: true, DurationMs: 1240,
			StartedAt: "2026-07-03T15:22:33+08:00", EndedAt: "2026-07-03T15:22:34+08:00"},
		{NodeID: "code", DisplayName: "编码", Engine: "claude-code",
			Success: true, DurationMs: 8021,
			StartedAt: "2026-07-03T15:22:43+08:00", EndedAt: "2026-07-03T15:22:51+08:00"},
	}
	md := RenderSummary(sampleRecord(), trace)

	for _, want := range []string{
		"# autopilot-20260703-152233",
		"**工作流** autopilot · 2 节点", // AgentNodeCount 排除 START / END
		"**需求** 给购物车加一个清空按钮",
		"✅ completed · 18.0s（2026-07-03 15:22:33 → 2026-07-03 15:22:51）",
		"**工作目录** /Users/me/proj",
		"## 节点",
		"| 规划 | claude-code | 2026-07-03 15:22:33 → 2026-07-03 15:22:34 | 1.2s |",
		"| 编码 | claude-code | 2026-07-03 15:22:43 → 2026-07-03 15:22:51 | 8.0s |",
		`<output node="plan" name="规划">`,
		"# 方案\n加按钮",
		`<output node="code" name="编码">`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("summary 缺少片段:\n%q\n\n完整:\n%s", want, md)
		}
	}
}

func TestSummarizePrompt(t *testing.T) {
	long := strings.Repeat("长", 200) // 200 字，远超 80 上限
	cases := []struct {
		name   string
		prompt string
		want   string
	}{
		{"短单行原样", "给购物车加一个清空按钮", "给购物车加一个清空按钮"},
		{"多行取首行加指针", "实现 5 项命令。\n\n1. copy\n2. node set", "实现 5 项命令。…（完整需求见 run.json）"},
		{"超长单行截断加指针", long, strings.Repeat("长", 80) + "…（完整需求见 run.json）"},
		{"首尾空白裁去", "  加按钮  ", "加按钮"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summarizePrompt(c.prompt, locale.Chinese); got != c.want {
				t.Errorf("summarizePrompt(%q) = %q，期望 %q", c.prompt, got, c.want)
			}
		})
	}
}

func TestRenderSummaryEnglishPreservesExternalContent(t *testing.T) {
	record := sampleRecord()
	record.Language = locale.English
	record.UserPrompt = "保留用户原文"
	errorText := "引擎原始错误"
	record.Error = &errorText
	record.Status = StatusFailed
	failedNodeID := "code"
	record.FailedNodeID = &failedNodeID
	markdown := RenderSummary(record, []TraceEntry{{
		NodeID: "code", DisplayName: "编码", Engine: "claude-code", StartedAt: record.StartedAt, EndedAt: *record.EndedAt,
	}})
	for _, want := range []string{
		"**Workflow** autopilot · 2 nodes",
		"**Request** 保留用户原文",
		"**Status** ❌ failed",
		"**Working directory** /Users/me/proj",
		"**Failed node** code",
		"**Error** 引擎原始错误",
		"## Nodes",
		"| Node | Engine | Start → End | Duration |",
		"| 编码 | claude-code |",
		"## Artifacts",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("English summary missing %q:\n%s", want, markdown)
		}
	}
	for _, unwanted := range []string{"**工作流**", "**需求**", "**状态**", "**工作目录**", "## 节点", "## 产物"} {
		if strings.Contains(markdown, unwanted) {
			t.Errorf("English summary contains Chinese product text %q:\n%s", unwanted, markdown)
		}
	}
}

func TestValidateLanguageRejectsMissingOrInvalidValue(t *testing.T) {
	for _, language := range []locale.Language{"", "fr"} {
		record := sampleRecord()
		record.Language = language
		if err := record.ValidateLanguage(); err == nil {
			t.Fatalf("ValidateLanguage(%q) = nil, want error", language)
		}
	}
}

func TestRenderSummaryFailedShowsNodeAndError(t *testing.T) {
	r := sampleRecord()
	r.Status = StatusFailed
	errMsg := "claude exited with code 1: boom"
	r.Error = &errMsg
	failedID := "code"
	r.FailedNodeID = &failedID  // 失败节点由 schedule 落进 record，summary 直接读（不再从 trace 猜）
	delete(r.Artifacts, "code") // 失败节点没有产物
	trace := []TraceEntry{
		{NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Success: true, DurationMs: 1000,
			StartedAt: "2026-07-03T15:22:33+08:00", EndedAt: "2026-07-03T15:22:34+08:00"},
		{NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, DurationMs: 500,
			StartedAt: "2026-07-03T15:22:34+08:00", EndedAt: "2026-07-03T15:22:34+08:00"},
	}
	md := RenderSummary(r, trace)
	for _, want := range []string{"**失败节点** code", "**错误** claude exited with code 1: boom", "| 编码 | claude-code |"} {
		if !strings.Contains(md, want) {
			t.Errorf("failed summary 缺少 %q\n完整:\n%s", want, md)
		}
	}
	if strings.Contains(md, `node="code"`) {
		t.Error("失败节点无产物，不应出现 code 的 output 块")
	}
}

// TestRenderSummaryDedupsResumedNodes 覆盖 resume 后的节点表去重：同一 NodeID 的旧失败行 + 补跑行只渲染
// 末条（成功那次），不出现重复行；完整审计仍走 run show --trace。
func TestRenderSummaryDedupsResumedNodes(t *testing.T) {
	r := sampleRecord()
	// 模拟一次 resume 后的 trace：code 先失败、后补跑成功——同一 NodeID 两条。
	trace := []TraceEntry{
		{NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Success: true, DurationMs: 1000,
			StartedAt: "2026-07-03T15:22:33+08:00", EndedAt: "2026-07-03T15:22:34+08:00"},
		{NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, DurationMs: 500,
			StartedAt: "2026-07-03T15:22:34+08:00", EndedAt: "2026-07-03T15:22:34+08:00"},
		{NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: true, DurationMs: 8000,
			StartedAt: "2026-07-03T15:23:00+08:00", EndedAt: "2026-07-03T15:23:08+08:00"},
	}
	md := RenderSummary(r, trace)
	// 节点表 code 只渲染末条（补跑成功、8.0s），不出现失败那次的 0.5s。
	if !strings.Contains(md, "| 编码 | claude-code | 2026-07-03 15:23:00 → 2026-07-03 15:23:08 | 8.0s |") {
		t.Errorf("节点表 code 应取补跑末条（8.0s），完整:\n%s", md)
	}
	// 节点表整体应为每节点一行：数 "| 编码 " 出现次数应恰为 1。
	if n := strings.Count(md, "| 编码 "); n != 1 {
		t.Errorf("节点表 code 应去重为 1 行，得到 %d 行\n完整:\n%s", n, md)
	}
}
