package engine

import "context"

// geminiEngine 通过 Google Gemini 无头 CLI（gemini）执行（待接入）。
type geminiEngine struct{}

func (geminiEngine) Name() string { return "gemini" }

func (geminiEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	return RunResult{}, ErrNotImplemented
}

func init() { Register(geminiEngine{}) }
