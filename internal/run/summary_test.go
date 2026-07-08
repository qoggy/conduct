package run

import (
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

func sampleRecord() *Record {
	ended := "2026-07-03T15:22:51+08:00"
	return &Record{
		ID:       "autopilot-20260703-152233",
		Workflow: "autopilot",
		WorkflowSnapshot: &workflow.Definition{
			Name:      "autopilot",
			UpdatedAt: "2026-07-03T15:40:00+08:00",
			Nodes: []workflow.Node{
				{ID: "plan", DisplayName: "规划"},
				{ID: "code", DisplayName: "编码"},
			},
		},
		UserPrompt: "给购物车加一个清空按钮",
		Cwd:        "/Users/me/proj",
		Status:     StatusCompleted,
		Steps:      2,
		StartedAt:  "2026-07-03T15:22:33+08:00",
		EndedAt:    &ended,
		Artifacts:  map[string]string{"plan": "# 方案\n加按钮", "code": "diff --git ..."},
	}
}

func TestRenderSummaryCompleted(t *testing.T) {
	trace := []TraceEntry{
		{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code",
			EngineConfig: &workflow.EngineConfig{Model: "claude-opus-4-8"}, Success: true, DurationMs: 1240},
		{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code",
			Success: true, DurationMs: 8021},
	}
	md := RenderSummary(sampleRecord(), trace)

	for _, want := range []string{
		"# autopilot-20260703-152233",
		"**工作流** autopilot · 2 节点",
		"**需求** 给购物车加一个清空按钮",
		"✅ completed · 18.0s（2026-07-03 15:22:33 → 2026-07-03 15:22:51）",
		"**工作目录** /Users/me/proj",
		"| 0 | 规划 | claude-code | 1.2s |",
		"| 1 | 编码 | claude-code | 8.0s |",
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
			if got := summarizePrompt(c.prompt); got != c.want {
				t.Errorf("summarizePrompt(%q) = %q，期望 %q", c.prompt, got, c.want)
			}
		})
	}
}

func TestRenderSummaryFailedShowsStepAndError(t *testing.T) {
	r := sampleRecord()
	r.Status = StatusFailed
	errMsg := "claude 退出码 1: boom"
	r.Error = &errMsg
	delete(r.Artifacts, "code") // 失败步没有产物
	trace := []TraceEntry{
		{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Success: true, DurationMs: 1000},
		{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, DurationMs: 500},
	}
	md := RenderSummary(r, trace)
	for _, want := range []string{"**失败步** step 1", "**错误** claude 退出码 1: boom", "| 1 | 编码 | claude-code | 0.5s |"} {
		if !strings.Contains(md, want) {
			t.Errorf("failed summary 缺少 %q\n完整:\n%s", want, md)
		}
	}
	if strings.Contains(md, `node="code"`) {
		t.Error("失败步无产物，不应出现 code 的 output 块")
	}
}

// TestRenderSummaryDedupsResumedSteps 覆盖 resume 后的步骤表去重：同一 stepIndex 的旧失败行 + 补跑行只渲染
// 末条（成功那次），不出现重复的「step 1」；完整审计仍走 run show --trace。
func TestRenderSummaryDedupsResumedSteps(t *testing.T) {
	r := sampleRecord()
	// 模拟一次 resume 后的 trace：step1（编码）先失败、后补跑成功——同一 stepIndex 两条。
	trace := []TraceEntry{
		{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Success: true, DurationMs: 1000},
		{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, DurationMs: 500},
		{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: true, DurationMs: 8000},
	}
	md := RenderSummary(r, trace)
	// 步骤表 step1 只渲染末条（补跑成功、8.0s），不出现失败那次的 0.5s。
	if !strings.Contains(md, "| 1 | 编码 | claude-code | 8.0s |") {
		t.Errorf("步骤表 step1 应取补跑末条（8.0s），完整:\n%s", md)
	}
	if strings.Contains(md, "| 1 | 编码 | claude-code | 0.5s |") {
		t.Errorf("步骤表不应出现失败那次的重复 step1（0.5s），完整:\n%s", md)
	}
	// 步骤表整体应为每步一行：数 "| 1 |" 出现次数应恰为 1。
	if n := strings.Count(md, "| 1 | 编码"); n != 1 {
		t.Errorf("步骤表 step1 应去重为 1 行，得到 %d 行\n完整:\n%s", n, md)
	}
}
