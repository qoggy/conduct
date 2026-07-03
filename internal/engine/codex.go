package engine

import "context"

// codexEngine 通过 OpenAI Codex 无头 CLI（codex exec）执行。
// 子进程调用与输出解析待实现。
type codexEngine struct{}

func (codexEngine) Name() string { return "codex" }

func (codexEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	return RunResult{}, ErrNotImplemented
}

func init() { Register(codexEngine{}) }
