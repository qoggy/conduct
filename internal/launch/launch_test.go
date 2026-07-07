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
