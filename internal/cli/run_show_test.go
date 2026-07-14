package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
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
		Status: run.StatusCompleted, StartedAt: "2026-07-03T15:00:00+08:00"}
	trace := []run.TraceEntry{
		{NodeID: "code", DisplayName: "编码", Engine: "codex", StartedAt: "2026-07-03T15:00:01+08:00",
			Input: "IN0", Success: true, Output: "OUT0", SessionID: "th-9"},
		{NodeID: "review", DisplayName: "评审", Engine: "claude-code", StartedAt: "2026-07-03T15:00:02+08:00",
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

func TestShowRunSummaryIgnoresStaleSummaryForUnfinishedRun(t *testing.T) {
	st := store.New(t.TempDir())
	record := seedRun(t, st, "flow-20260703-150000", run.StatusRunning, os.Getpid())
	if err := st.WriteSummary(record.ID, "# 旧失败总结\n"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := showRunSummary(&buf, st, record.ID, record); err != nil {
		t.Fatalf("showRunSummary 失败: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "旧失败总结") {
		t.Fatalf("未收尾运行不应打印旧 summary，实际:\n%s", out)
	}
	if !strings.Contains(out, "运行 flow-20260703-150000 · running") || !strings.Contains(out, "运行总结尚未生成") {
		t.Fatalf("未收尾运行应退回状态摘要，实际:\n%s", out)
	}
}
