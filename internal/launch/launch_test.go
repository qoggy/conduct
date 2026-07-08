package launch

import (
	"testing"
	"time"

	"github.com/qoggy/conduct/internal/run"
)

func TestMatchRunID(t *testing.T) {
	spawnedAt, _ := time.Parse(time.RFC3339, "2026-07-05T12:00:00+08:00")
	recent := spawnedAt.Format(time.RFC3339)
	stale := spawnedAt.Add(-time.Hour).Format(time.RFC3339)
	const childPid = 4242

	records := []*run.Record{
		// 干扰项：同 pid（复用）但 startedAt 很旧 → 被时钟余量滤掉
		{ID: "demo-old", Workflow: "demo", Pid: childPid, Status: run.StatusRunning, StartedAt: stale},
		// 干扰项：别的 workflow，同 pid 同时刻
		{ID: "other-new", Workflow: "other", Pid: childPid, Status: run.StatusRunning, StartedAt: recent},
		// 干扰项：pid 不同
		{ID: "demo-otherpid", Workflow: "demo", Pid: 9999, Status: run.StatusRunning, StartedAt: recent},
		// 目标：workflow 名 + pid + startedAt 新。状态特意用 failed——引擎秒级失败已转终态，
		// 但它仍是刚发射的这次，必须被匹配（不因非 running 而漏掉）。
		{ID: "demo-target", Workflow: "demo", Pid: childPid, Status: run.StatusFailed, StartedAt: recent},
	}
	id, ok := matchRunID(records, "demo", childPid, spawnedAt)
	if !ok || id != "demo-target" {
		t.Fatalf("应命中 demo-target，得到 id=%q ok=%v", id, ok)
	}
}

func TestMatchRunIDNoMatch(t *testing.T) {
	spawnedAt, _ := time.Parse(time.RFC3339, "2026-07-05T12:00:00+08:00")
	records := []*run.Record{
		{ID: "demo-x", Workflow: "demo", Pid: 1, Status: run.StatusRunning, StartedAt: spawnedAt.Format(time.RFC3339)},
	}
	if _, ok := matchRunID(records, "demo", 4242, spawnedAt); ok {
		t.Fatalf("pid 不匹配不应命中")
	}
}

// TestResumeTaken 覆盖 LaunchResume 的接管判据（run resume 路径）：run.json 的 pid 已是子进程 pid 即算接管，
// 且不因状态非 running 而漏判（子进程可能秒级再次失败已转终态）；nil / pid 未更新（仍是旧 pid）时不算接管。
func TestResumeTaken(t *testing.T) {
	const childPid = 4242
	cases := []struct {
		name   string
		record *run.Record
		want   bool
	}{
		// pid 已更新为子进程 pid：接管成立。状态特意用 failed——秒级再次失败已转终态，但 pid 已是它，仍算接管。
		{"pid 已更新_终态也算接管", &run.Record{Pid: childPid, Status: run.StatusFailed}, true},
		{"pid 已更新_running", &run.Record{Pid: childPid, Status: run.StatusRunning}, true},
		// pid 仍是原 run 的旧 pid（子进程尚未改写 run.json）：未接管。
		{"pid 未更新", &run.Record{Pid: 21474836, Status: run.StatusFailed}, false},
		{"record 为 nil", nil, false},
	}
	for _, c := range cases {
		if got := resumeTaken(c.record, childPid); got != c.want {
			t.Errorf("%s：resumeTaken 应为 %v，得到 %v", c.name, c.want, got)
		}
	}
}
