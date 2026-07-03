package engine

import "context"

// claudeCodeEngine 通过 Anthropic Claude Code 无头 CLI（claude -p）执行。
// 子进程调用与 JSON 输出解析待实现。
type claudeCodeEngine struct{}

func (claudeCodeEngine) Name() string { return "claude-code" }

func (claudeCodeEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	return RunResult{}, ErrNotImplemented
}

func init() { Register(claudeCodeEngine{}) }
