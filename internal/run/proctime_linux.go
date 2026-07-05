//go:build linux

package run

import (
	"os"
	"strconv"
	"strings"
)

// processStartToken 返回 pid 的进程启动时刻标识：/proc/<pid>/stat 第 22 字段 starttime
// （自开机以来的时钟滴答数），进程存续期内稳定、pid 被复用后必不同。读不到返回 ("", false)。
func processStartToken(pid int) (string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	s := string(data)
	// 第 2 字段 comm 用括号包裹且可能含空格/括号，须从最后一个 ')' 之后再按空白切分。
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return "", false
	}
	fields := strings.Fields(s[i+2:])  // 从第 3 字段（state）起
	const startTimeIndexAfterComm = 19 // 第 22 字段 − 3
	if len(fields) <= startTimeIndexAfterComm {
		return "", false
	}
	return fields[startTimeIndexAfterComm], true
}
