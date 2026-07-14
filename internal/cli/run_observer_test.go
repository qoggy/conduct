package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/orchestrator"
	"github.com/qoggy/conduct/internal/run"
)

// TestHumanObserverScheduleHeader 覆盖 OnSchedule 两态渲染：整趟 workflow run（ResumeDoneCount=0）打印
// 「调度 N 个节点」+ START 扇出的就绪清单；resume（ResumeDoneCount>0）打印「从中断恢复、已完成几个」的恢复头。
func TestHumanObserverScheduleHeader(t *testing.T) {
	info := orchestrator.ScheduleInfo{
		AgentNodeCount: 3,
		InitialReady: []orchestrator.NodeBrief{
			{NodeID: "plan", DisplayName: "计划"},
			{NodeID: "seed", DisplayName: "播种"},
		},
	}

	var fresh bytes.Buffer
	humanObserver{out: &fresh}.OnSchedule(info)
	if !strings.Contains(fresh.String(), "调度 3 个节点") {
		t.Errorf("整趟调度应打印「调度 3 个节点」，实际:\n%s", fresh.String())
	}
	if !strings.Contains(fresh.String(), "plan") || !strings.Contains(fresh.String(), "seed") {
		t.Errorf("整趟调度应列出 t0 就绪节点，实际:\n%s", fresh.String())
	}

	resumeInfo := info
	resumeInfo.ResumeDoneCount = 2
	var resumed bytes.Buffer
	humanObserver{out: &resumed}.OnSchedule(resumeInfo)
	out := resumed.String()
	if !strings.Contains(out, "从中断恢复") || !strings.Contains(out, "已完成 2 个") {
		t.Errorf("resume 应打印恢复头「从中断恢复…已完成 2 个」，实际:\n%s", out)
	}
}

// TestHumanObserverNodeLifecycle 覆盖单节点的开跑 / 成功 / 失败三种事件行。
func TestHumanObserverNodeLifecycle(t *testing.T) {
	var start bytes.Buffer
	humanObserver{out: &start}.OnNodeStart(orchestrator.NodeInfo{
		NodeID: "plan", DisplayName: "计划", Engine: "claude-code",
	})
	if !strings.Contains(start.String(), "plan") || !strings.Contains(start.String(), "[计划]") ||
		!strings.Contains(start.String(), "开跑") || !strings.Contains(start.String(), "engine=claude-code") {
		t.Errorf("开跑事件行应含 id/显示名/引擎，实际:\n%s", start.String())
	}

	var done bytes.Buffer
	humanObserver{out: &done}.OnNodeDone(run.TraceEntry{
		NodeID: "plan", Success: true, Output: "结果", DurationMs: 8021, Tokens: 12,
	})
	if !strings.Contains(done.String(), "✓") || !strings.Contains(done.String(), "plan") ||
		!strings.Contains(done.String(), "完成") {
		t.Errorf("成功事件行应含 ✓/id/完成，实际:\n%s", done.String())
	}

	var fail bytes.Buffer
	failMsg := "引擎超时"
	humanObserver{out: &fail}.OnNodeDone(run.TraceEntry{
		NodeID: "plan", Success: false, Error: &failMsg, DurationMs: 1200,
	})
	if !strings.Contains(fail.String(), "✗") || !strings.Contains(fail.String(), "失败") ||
		!strings.Contains(fail.String(), "引擎超时") {
		t.Errorf("失败事件行应含 ✗/失败/错因，实际:\n%s", fail.String())
	}
}
