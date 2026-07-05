//go:build darwin

package run

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// processStartToken 返回 pid 的进程启动时刻标识：kern.proc.pid 的 p_starttime（进程创建时刻），
// 进程存续期内稳定、pid 被复用后必不同。读不到返回 ("", false)。
func processStartToken(pid int) (string, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", false
	}
	tv := kp.Proc.P_starttime
	return strconv.FormatInt(int64(tv.Sec), 10) + "." + strconv.FormatInt(int64(tv.Usec), 10), true
}
