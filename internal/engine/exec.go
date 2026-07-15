package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// commandSpec 描述一次无头 CLI 子进程调用。
type commandSpec struct {
	binary string   // 可执行文件名（经 PATH 解析）
	args   []string // 命令行参数
	stdin  string   // 经 stdin 喂入的内容；空串表示不接 stdin
	dir    string   // 工作目录（cmd.Dir）；空串表示继承当前进程
}

// commandOutput 是子进程的产出与耗时。
type commandOutput struct {
	stdout     string
	stderr     string
	durationMs int64
}

// runCommand 执行一次子进程调用并计时。返回的 error 为原始 exec 错误（*exec.ExitError /
// 找不到可执行文件等），由各引擎经 commandError 转译为带引擎名的可读错误。
func runCommand(ctx context.Context, spec commandSpec) (commandOutput, error) {
	cmd := exec.CommandContext(ctx, spec.binary, spec.args...)
	if spec.dir != "" {
		cmd.Dir = spec.dir
	}
	if spec.stdin != "" {
		cmd.Stdin = strings.NewReader(spec.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := time.Now()
	err := cmd.Run()
	out := commandOutput{
		stdout:     stdout.String(),
		stderr:     stderr.String(),
		durationMs: time.Since(startedAt).Milliseconds(),
	}
	return out, err
}

// commandError 把子进程失败转译为带引擎名的可读错误：非零退出码附退出码 + stderr 摘要，
// 其余（如找不到可执行文件）原样包装上抛。绝不静默——承项目「错误不吞」。
func commandError(engineName string, out commandOutput, err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s exited with code %d: %s", engineName, exitErr.ExitCode(), truncate(strings.TrimSpace(out.stderr), 500))
	}
	return fmt.Errorf("failed to invoke %s: %w", engineName, err)
}

// truncate 把字符串按 rune 截断到至多 n 个字符（超出以 … 收尾），用于错误 / 预览摘要。
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
