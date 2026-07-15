package workflow

import (
	"regexp"

	"github.com/qoggy/conduct/internal/apperror"
)

// workflowNamePattern 限定工作流名（= 文件名 <name>.json）：字母 / 数字 / 点 / 下划线 / 连字符，至少 1 个。
// 注意与 node id 规则不同（node id 见 Validate，不含点、不可数字开头）。
var workflowNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateName 校验工作流名是否合法。正则已禁掉路径分隔符（`/` `\`），额外拒绝
// `.` / `..` 这两个目录特殊名，杜绝按名寻址时越出 store 目录。
func ValidateName(name string) error {
	if !workflowNamePattern.MatchString(name) {
		return apperror.New(apperror.CodeWorkflowNameInvalidChars, apperror.Params{"name": name})
	}
	if name == "." || name == ".." {
		return apperror.New(apperror.CodeWorkflowNameReserved, apperror.Params{"name": name})
	}
	return nil
}
