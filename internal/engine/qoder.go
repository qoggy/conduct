package engine

import "context"

// qoderEngine 通过 Qoder 无头 CLI 执行（待接入）。
type qoderEngine struct{}

func (qoderEngine) Name() string { return "qoder" }

func (qoderEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	return RunResult{}, ErrNotImplemented
}

func init() { Register(qoderEngine{}) }
