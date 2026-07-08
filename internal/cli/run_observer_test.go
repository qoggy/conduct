package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

// TestHumanObserverExpandHeader 覆盖 OnExpand 两态渲染：整趟 workflow run（startIndex=0）打印「展开为 N 步」
// 全量清单；resume（startIndex>0）打印「从第几步恢复、共剩几步」的恢复头并只列剩余步（承 spec〈run resume〉）。
func TestHumanObserverExpandHeader(t *testing.T) {
	steps := []workflow.ExecutionStep{
		{Type: "agent", NodeID: "plan", Iteration: 1},
		{Type: "agent", NodeID: "code", Iteration: 1},
		{Type: "agent", NodeID: "review", Iteration: 1},
	}

	var full bytes.Buffer
	humanObserver{out: &full}.OnExpand(steps, 0)
	if !strings.Contains(full.String(), "展开为 3 步") {
		t.Errorf("整趟展开应打印「展开为 3 步」，实际:\n%s", full.String())
	}
	if !strings.Contains(full.String(), "[0]") {
		t.Errorf("整趟展开应列出全部步（含 [0]），实际:\n%s", full.String())
	}

	var resumed bytes.Buffer
	humanObserver{out: &resumed}.OnExpand(steps, 1)
	out := resumed.String()
	if !strings.Contains(out, "从第 1 步恢复") || !strings.Contains(out, "续跑剩余 2 步") {
		t.Errorf("resume 应打印恢复头「从第 1 步恢复…续跑剩余 2 步」，实际:\n%s", out)
	}
	if strings.Contains(out, "[0]") {
		t.Errorf("resume 不应列出已跳过的 [0] 步，实际:\n%s", out)
	}
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "[2]") {
		t.Errorf("resume 应列出将重跑的 [1]/[2] 步，实际:\n%s", out)
	}
}
