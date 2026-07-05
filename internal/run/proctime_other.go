//go:build !linux && !darwin

package run

// processStartToken 在未特化的平台上无法获取进程启动时刻：返回 ("", false)，
// 调用方（processAlive）据此退回纯 pid 判断，保持原有行为。
func processStartToken(pid int) (string, bool) {
	return "", false
}
