package cli

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// seedRun 直接把一条运行记录落进 store（含初始 run.json + 空 trace.jsonl），供 run 名词族命令测试。
func seedRun(t *testing.T, st *store.Store, id string, status run.Status, pid int) *run.Record {
	t.Helper()
	rec := &run.Record{
		ID:               id,
		Workflow:         "flow",
		WorkflowSnapshot: &workflow.Definition{Name: "flow", Nodes: []workflow.Node{plainNode("a")}},
		UserPrompt:       "需求",
		Cwd:              "/proj",
		Status:           status,
		Pid:              pid,
		Steps:            1,
		StartedAt:        "2026-07-03T15:00:00+08:00",
		Artifacts:        map[string]string{},
	}
	if err := st.CreateRun(rec); err != nil {
		t.Fatalf("落运行记录失败: %v", err)
	}
	return rec
}

func TestIsTerminalStatus(t *testing.T) {
	if isTerminalStatus(run.StatusRunning) {
		t.Fatalf("running 不是终态")
	}
	for _, status := range []run.Status{run.StatusCompleted, run.StatusFailed, run.StatusInterrupted} {
		if !isTerminalStatus(status) {
			t.Fatalf("%s 应为终态", status)
		}
	}
}

func TestWaitForTerminalImmediate(t *testing.T) {
	st := store.New(t.TempDir())
	// completed：已终态，立即返回、不空等。
	seedRun(t, st, "flow-20260703-150000", run.StatusCompleted, os.Getpid())
	rec, err := waitForTerminal(st, "flow-20260703-150000", time.Millisecond)
	if err != nil {
		t.Fatalf("等待终态运行不应报错: %v", err)
	}
	if rec.EffectiveStatus() != run.StatusCompleted {
		t.Fatalf("应返回 completed，得到 %s", rec.EffectiveStatus())
	}
}

func TestWaitForTerminalDerivedInterrupted(t *testing.T) {
	st := store.New(t.TempDir())
	// status=running 但 pid 已死（用一个不可能存活的 pid）→ 派生 interrupted，属终态，立即返回。
	seedRun(t, st, "flow-20260703-150001", run.StatusRunning, 21474836)
	rec, err := waitForTerminal(st, "flow-20260703-150001", time.Millisecond)
	if err != nil {
		t.Fatalf("等待派生 interrupted 不应报错: %v", err)
	}
	if rec.EffectiveStatus() != run.StatusInterrupted {
		t.Fatalf("pid 已死应派生 interrupted，得到 %s", rec.EffectiveStatus())
	}
}

func TestWaitForTerminalNotExist(t *testing.T) {
	st := store.New(t.TempDir())
	_, err := waitForTerminal(st, "ghost-20260101-000000", time.Millisecond)
	if !errors.Is(err, store.ErrRunNotExist) {
		t.Fatalf("目标不存在应返回 ErrRunNotExist（退 1），得到 %v", err)
	}
}
