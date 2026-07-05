package run

import (
	"fmt"
	"syscall"
)

// StopProcess 终止一次运行的进程。先按进程组发 SIGTERM（kill(-pid) 连带引擎子进程一并收），
// 组不存在时回退为向单进程发 SIGTERM：
//   - ESRCH：pid 不是组长（例如终端管道 `cat req | conduct workflow run` 里 conduct 未 Setsid），
//     没有 id==pid 的进程组可投递 → 回退 kill(pid, SIGTERM) 只终止 conduct 本身；
//   - 其余错误（如 EPERM 属他人进程）：原样上抛，不吞。
//
// 只有 UI self-exec 路径以 Setsid 独立成组时，组信号才真正连带引擎子进程整组退出；终端/管道启动
// 退化为仅终止 conduct 进程本身——当前步的引擎子进程遗留到本步自然结束、编排器已死不再驱动下一步，
// 属可接受降级。进程停写后由 pid 判活派生 interrupted，不引入新落盘状态。
func StopProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("非法 pid %d：无法发送终止信号", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return syscall.Kill(pid, syscall.SIGTERM) // 非组长 → 回退单进程
		}
		return fmt.Errorf("向进程组 %d 发送 SIGTERM 失败: %w", pid, err)
	}
	return nil
}
