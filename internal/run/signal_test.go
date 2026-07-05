package run

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestStopProcessFallbackTerminatesChild 覆盖回退分支：默认 exec 起的子进程不是组长，
// kill(-pid) 得 ESRCH，回退 kill(pid) 终止之。Wait 回收后 pid 不再存活。
func TestStopProcessFallbackTerminatesChild(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动子进程失败: %v", err)
	}
	pid := cmd.Process.Pid
	if !ProcessAlive(pid) {
		t.Fatalf("子进程刚启动应存活")
	}
	if err := StopProcess(pid); err != nil {
		t.Fatalf("StopProcess 失败: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Errorf("被 SIGTERM 终止的进程 Wait 应返回信号退出错误，却为 nil")
	}
	// 已回收（非僵尸）→ 探活为死。
	if ProcessAlive(pid) {
		t.Errorf("终止并回收后 pid %d 不应再存活", pid)
	}
}

// TestStopProcessGroupLeaderTerminates 覆盖组分支：子进程 Setpgid 自成组（pid==pgid），
// kill(-pid) 直接命中该组、无 ESRCH。这是 UI self-exec 成组路径的机械验证（非引擎子进程 e2e）。
func TestStopProcessGroupLeaderTerminates(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动子进程失败: %v", err)
	}
	pid := cmd.Process.Pid
	if err := StopProcess(pid); err != nil {
		t.Fatalf("StopProcess（组分支）失败: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Errorf("整组被 SIGTERM 应使子进程信号退出，Wait 却为 nil")
	}
	if ProcessAlive(pid) {
		t.Errorf("整组终止并回收后 pid %d 不应再存活", pid)
	}
}

// TestStopProcessDeadPid 确认对已死 pid 返回错误（两次 ESRCH，不静默成功）。
func TestStopProcessDeadPid(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动子进程失败: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Kill(); err != nil { // SIGKILL 强杀
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Errorf("被强杀的进程 Wait 应返回错误")
	}
	if err := StopProcess(pid); err == nil {
		t.Errorf("对已死 pid %d 的 StopProcess 应返回错误，却成功", pid)
	}
}

// TestStopProcessRejectsInvalidPid 确认非法 pid（<=0）被拒，避免 kill(-0) 误伤本组。
func TestStopProcessRejectsInvalidPid(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if err := StopProcess(pid); err == nil {
			t.Errorf("StopProcess(%d) 应报错（防 kill(-0) 误伤本进程组）", pid)
		}
	}
}
