package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/run"
)

func TestSessionReplayLine(t *testing.T) {
	cases := []struct {
		engine string
		want   string
	}{
		{"claude-code", "会话 sid · 回放：claude -r sid"},
		{"codex", "会话 sid · 回放：codex resume sid"},
		{"qoder", "会话 sid · 回放：qodercli -r sid"},
		{"antigravity", "会话 sid · 回放：agy --conversation sid"},
		{"unknown", "会话 sid"}, // 未知引擎只显示 id，不臆造命令
	}
	for _, c := range cases {
		if got := sessionReplayLine(c.engine, "sid"); got != c.want {
			t.Errorf("engine=%s：得到 %q，期望 %q", c.engine, got, c.want)
		}
	}
}

// TestShowRunTraceSessionLine 验证 --trace 视图：记有 sessionId 的步在 input 前附会话/回放行；无则不附。
func TestShowRunTraceSessionLine(t *testing.T) {
	record := &run.Record{ID: "flow-20260703-150000", Workflow: "flow", UserPrompt: "需求",
		Status: run.StatusCompleted, Steps: 2, StartedAt: "2026-07-03T15:00:00+08:00"}
	trace := []run.TraceEntry{
		{StepIndex: 0, Type: "agent", DisplayName: "编码", Engine: "codex",
			Input: "IN0", Success: true, Output: "OUT0", SessionID: "th-9"},
		{StepIndex: 1, Type: "agent", DisplayName: "评审", Engine: "claude-code",
			Input: "IN1", Success: true, Output: "OUT1"}, // 无 sessionId
	}
	var buf bytes.Buffer
	showRunTrace(&buf, record, trace)
	out := buf.String()
	if !strings.Contains(out, "codex resume th-9") {
		t.Errorf("有 sessionId 的步应附回放命令，实际:\n%s", out)
	}
	// 无 sessionId 的步不应出现会话标题。
	if strings.Count(out, "── 会话 ──") != 1 {
		t.Errorf("只应有一步附会话行，实际:\n%s", out)
	}
}
