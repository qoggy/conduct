package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

// resumableRecord 构造一条运行记录，供 resume 前置校验测试。
func resumableRecord(id string, status run.Status, pid int) *run.Record {
	return &run.Record{
		ID: id, Workflow: "flow",
		WorkflowSnapshot: &workflow.Workflow{Name: "flow", Definition: workflow.Definition{
			Nodes: []workflow.Node{{ID: "START"}, plainNode("a"), {ID: "END"}},
			Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
		}},
		UserPrompt: "需求", Cwd: "/proj", Status: status,
		Pid: pid, StartedAt: "2026-07-03T15:00:00+08:00",
		Artifacts: map[string]string{}, Language: locale.English,
	}
}

func TestCheckResumable(t *testing.T) {
	useTestLanguage(t, locale.Chinese)
	// 派生态 failed → 可恢复。
	if err := checkResumable(resumableRecord("flow-20260703-150000", run.StatusFailed, 21474836)); err != nil {
		t.Fatalf("failed 应可恢复，得到 %v", err)
	}
	// running 但 pid 已死 → 派生 interrupted → 可恢复。
	interrupted := resumableRecord("flow-20260703-150001", run.StatusRunning, -1)
	interrupted.Pid = -1
	if err := checkResumable(interrupted); err != nil {
		t.Fatalf("interrupted 应可恢复，得到 %v", err)
	}
	// completed → 拒绝。
	completed := resumableRecord("flow-20260703-150002", run.StatusCompleted, 21474836)
	if err := checkResumable(completed); err == nil || !strings.Contains(formatCLIError(err), "已成功完成") {
		t.Fatalf("completed 应拒绝，得到 %v", err)
	}
	// running（pid 存活）→ 拒绝「仍在运行中」。
	running := resumableRecord("flow-20260703-150003", run.StatusRunning, os.Getpid())
	running.Status = run.StatusRunning
	running.Pid = os.Getpid()
	if err := checkResumable(running); err == nil || !strings.Contains(formatCLIError(err), "仍在运行中") {
		t.Fatalf("running 应拒绝，得到 %v", err)
	}
}

// TestRunResumeDetachedWithSuccessJSON 验证 resume -d --json：单行句柄 {id, workflow}，id 即原 run id、
// workflow 取自原记录；且发射器收到的入参是原 run id。
func TestRunResumeDetachedWithSuccessJSON(t *testing.T) {
	cmd, out := newDetachTestCmd()
	fake := &fakeDetachLauncher{runID: "flow-20260707-120000"}
	record := resumableRecord("flow-20260707-120000", run.StatusFailed, 21474836)
	if err := runResumeDetachedWith(cmd, fake, record, true); err != nil {
		t.Fatalf("成功不应报错: %v", err)
	}
	if fake.gotResumeID != "flow-20260707-120000" {
		t.Fatalf("发射器应收到原 run id，得到 %q", fake.gotResumeID)
	}
	var handle map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &handle); err != nil {
		t.Fatalf("句柄非合法 JSON: %v（原文 %q）", err, out.String())
	}
	if handle["id"] != "flow-20260707-120000" || handle["workflow"] != "flow" || len(handle) != 2 {
		t.Fatalf("句柄应恰为 {id, workflow}，得到 %q", out.String())
	}
}

// TestRunResumeDetachedWithSuccessHuman 验证 resume -d 人读：打印含 run id 的「已在后台恢复」提示。
func TestRunResumeDetachedWithSuccessHuman(t *testing.T) {
	useTestLanguage(t, locale.Chinese)
	cmd, out := newDetachTestCmd()
	record := resumableRecord("flow-20260707-120000", run.StatusFailed, 21474836)
	if err := runResumeDetachedWith(cmd, &fakeDetachLauncher{runID: "flow-20260707-120000"}, record, false); err != nil {
		t.Fatalf("成功不应报错: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "flow-20260707-120000") || !strings.Contains(s, "已在后台恢复") {
		t.Fatalf("人读输出应含 run id 与恢复提示，得到 %q", s)
	}
}
