package run

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorkingDirExisting(t *testing.T) {
	dir := t.TempDir()
	if err := ValidateWorkingDir(dir); err != nil {
		t.Fatalf("已存在目录应通过校验，却报错: %v", err)
	}
}

func TestValidateWorkingDirNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	err := ValidateWorkingDir(missing)
	if !errors.Is(err, ErrWorkingDirNotExist) {
		t.Fatalf("不存在路径应返回 ErrWorkingDirNotExist，得到: %v", err)
	}
}

func TestValidateWorkingDirNotDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	err := ValidateWorkingDir(file)
	if !errors.Is(err, ErrWorkingDirNotDir) {
		t.Fatalf("文件（非目录）应返回 ErrWorkingDirNotDir，得到: %v", err)
	}
}
