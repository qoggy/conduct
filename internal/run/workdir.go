package run

import (
	"errors"
	"fmt"
	"os"
)

// 引擎工作目录（run 的 --cwd / {{sys.cwd}}）的存在性校验：一次运行只应落在已存在的目录上，
// 带着不存在 / 非目录的路径去烧引擎没有意义。此校验有两个同源调用方——CLI 的 workflow run
// （见 cli.resolveCwd）与 UI 启动弹窗的发射前预检（见 internal/ui launch）——故收敛在此单一实现，
// 用类型化哨兵让两方各自组织自己的错误文案（CLI 退 2、UI 映射 400）。

var (
	// ErrWorkingDirNotExist 表示目标工作目录不存在。
	ErrWorkingDirNotExist = errors.New("工作目录不存在")
	// ErrWorkingDirNotDir 表示目标路径存在但不是目录。
	ErrWorkingDirNotDir = errors.New("工作目录不是目录")
)

// ValidateWorkingDir 校验一个（应为绝对路径的）工作目录已存在且确为目录。
// 不存在 → ErrWorkingDirNotExist；存在但非目录 → ErrWorkingDirNotDir；其余 stat 错误如实带出。
// 校验通过返回 nil。调用方负责先把用户输入转为绝对路径（filepath.Abs）。
func ValidateWorkingDir(absDir string) error {
	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", absDir, ErrWorkingDirNotExist)
		}
		return fmt.Errorf("无法访问工作目录 %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: %w", absDir, ErrWorkingDirNotDir)
	}
	return nil
}
