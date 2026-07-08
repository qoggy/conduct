package run

import (
	"os"
	"testing"
)

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		status Status
		alive  bool
		want   Status
	}{
		{StatusRunning, true, StatusRunning},
		{StatusRunning, false, StatusInterrupted}, // 只有这一种降级
		{StatusCompleted, false, StatusCompleted}, // 终态不受进程存活影响
		{StatusCompleted, true, StatusCompleted},
		{StatusFailed, false, StatusFailed},
	}
	for _, c := range cases {
		if got := deriveStatus(c.status, c.alive); got != c.want {
			t.Errorf("deriveStatus(%q, alive=%v) = %q，期望 %q", c.status, c.alive, got, c.want)
		}
	}
}

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("当前进程应判为存活")
	}
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Error("非法 pid 应判为不存活")
	}
}

func TestStepLabel(t *testing.T) {
	agent := TraceEntry{Type: "agent", DisplayName: "编码"}
	evaluator := TraceEntry{Type: "evaluator", DisplayName: "编码"}
	if agent.StepLabel() != "编码" {
		t.Errorf("agent 步应用原节点名，得到 %q", agent.StepLabel())
	}
	if evaluator.StepLabel() != "编码 · 评测" {
		t.Errorf("evaluator 步应加「· 评测」后缀，得到 %q", evaluator.StepLabel())
	}
}

func TestProgressCount(t *testing.T) {
	// 空 trace → 0。
	if k := ProgressCount(nil); k != 0 {
		t.Errorf("空 trace 应为 0，得到 %d", k)
	}
	// resume 后的 trace：step2 先失败、后被补跑成功——同一 stepIndex 两条，末条 success 为准，只算 1 次。
	trace := []TraceEntry{
		{StepIndex: 0, Success: true},
		{StepIndex: 1, Success: true},
		{StepIndex: 2, Success: false}, // 首次失败（保留）
		{StepIndex: 2, Success: true},  // 补跑成功
		{StepIndex: 3, Success: true},
	}
	if k := ProgressCount(trace); k != 4 {
		t.Errorf("唯一成功 stepIndex {0,1,2,3} 应为 4，得到 %d", k)
	}
	// 末条为失败（尚未补跑成功）：该 stepIndex 不计。
	failing := []TraceEntry{{StepIndex: 0, Success: true}, {StepIndex: 1, Success: false}}
	if k := ProgressCount(failing); k != 1 {
		t.Errorf("末条失败的 step 不计，应为 1，得到 %d", k)
	}
}

func TestEffectiveStatusRunningSelfIsRunning(t *testing.T) {
	// 用当前进程 pid：running + 存活 → 仍 running。
	r := &Record{Status: StatusRunning, Pid: os.Getpid()}
	if got := r.EffectiveStatus(); got != StatusRunning {
		t.Errorf("running + 存活进程应为 running，得到 %q", got)
	}
}
