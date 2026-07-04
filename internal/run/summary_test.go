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
		"**工作流** autopilot · 2 节点（冻结于 updatedAt 2026-07-03 15:40）",
		"**需求** 给购物车加一个清空按钮",
		"✅ completed · 18.0s（2026-07-03 15:22:33 → 2026-07-03 15:22:51）",
		"**工作目录** /Users/me/proj",
		"| 0 | 规划 | claude-code · claude-opus-4-8 | ✅ | 1.2s |",
		"| 1 | 编码 | claude-code · (默认) | ✅ | 8.0s |",
		`<output node="plan" name="规划">`,
		"# 方案\n加按钮",
		`<output node="code" name="编码">`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("summary 缺少片段:\n%q\n\n完整:\n%s", want, md)
		}
	}
}

func TestRenderSummaryFailedShowsStepAndError(t *testing.T) {
	r := sampleRecord()
	r.Status = StatusFailed
	failed := 1
	errMsg := "claude 退出码 1: boom"
	r.FailedStep = &failed
	r.Error = &errMsg
	delete(r.Artifacts, "code") // 失败步没有产物
	trace := []TraceEntry{
		{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Success: true, DurationMs: 1000},
		{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, DurationMs: 500},
	}
	md := RenderSummary(r, trace)
	for _, want := range []string{"**失败步** step 1", "**错误** claude 退出码 1: boom", "| 1 | 编码 | claude-code · (默认) | ❌ | 0.5s |"} {
		if !strings.Contains(md, want) {
			t.Errorf("failed summary 缺少 %q\n完整:\n%s", want, md)
		}
	}
	if strings.Contains(md, `node="code"`) {
		t.Error("失败步无产物，不应出现 code 的 output 块")
	}
}
