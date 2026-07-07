package cli

import (
	"errors"
	"os"
	"testing"

	"github.com/qoggy/conduct/internal/run"
)

func TestParseStatusFilter(t *testing.T) {
	// 空串 → 不过滤。
	got, err := parseStatusFilter("")
	if err != nil || got != "" {
		t.Fatalf("空串应表示不过滤，得到 status=%q err=%v", got, err)
	}
	// 合法枚举原样返回。
	for _, s := range []string{"running", "completed", "failed", "interrupted"} {
		got, err := parseStatusFilter(s)
		if err != nil || string(got) != s {
			t.Fatalf("%q 应为合法取值，得到 status=%q err=%v", s, got, err)
		}
	}
	// 非法取值 → 用法错误（退 2）。
	_, err = parseStatusFilter("bogus")
	if err == nil {
		t.Fatalf("非法 --status 应报错")
	}
	var usage *usageError
	if !errors.As(err, &usage) {
		t.Fatalf("非法 --status 应为用法错误（退 2），得到 %T", err)
	}
}

func TestFilterRunsByStatus(t *testing.T) {
	alive := os.Getpid()
	const dead = 21474836 // 超出 pid 上限 → 判死
	records := []*run.Record{
		{ID: "flow-running", Workflow: "flow", Status: run.StatusRunning, Pid: alive},
		{ID: "flow-interrupted", Workflow: "flow", Status: run.StatusRunning, Pid: dead}, // 派生 interrupted
		{ID: "flow-completed", Workflow: "flow", Status: run.StatusCompleted, Pid: dead},
		{ID: "flow-failed", Workflow: "flow", Status: run.StatusFailed, Pid: dead},
	}

	// 空串不过滤：原样返回全部。
	if got := filterRunsByStatus(records, ""); len(got) != len(records) {
		t.Fatalf("空过滤应返回全部 %d 条，得到 %d 条", len(records), len(got))
	}

	// 按派生态过滤：running 只留 pid 真存活的那条。
	cases := map[run.Status]string{
		run.StatusRunning:     "flow-running",
		run.StatusInterrupted: "flow-interrupted",
		run.StatusCompleted:   "flow-completed",
		run.StatusFailed:      "flow-failed",
	}
	for want, wantID := range cases {
		got := filterRunsByStatus(records, want)
		if len(got) != 1 || got[0].ID != wantID {
			t.Fatalf("按 %s 过滤应只剩 %s，得到 %+v", want, wantID, got)
		}
	}
}
