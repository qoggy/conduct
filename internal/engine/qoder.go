package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// qoderEngine 通过 Qoder 无头 CLI（qodercli -p）执行。qodercli 与 claude 同族：prompt 走 stdin、
// --output-format json 输出单个 result 对象；调优字段用 --reasoning-effort（与模型解耦）。
// 用法见 docs/references/qodercli-print.md。
type qoderEngine struct{}

func (qoderEngine) Name() string { return "qoder" }

// qoderResult 是 `qodercli -p --output-format json` 的 stdout 单对象（只取用到的字段）。
type qoderResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (qoderEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	args := []string{"-p", "--output-format", "json", "--permission-mode", "bypass_permissions"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	// qoder 的调优字段是 --reasoning-effort（与模型解耦），空则不传、用 CLI 默认。
	if request.Effort != "" {
		args = append(args, "--reasoning-effort", request.Effort)
	}

	out, err := runCommand(ctx, commandSpec{binary: "qodercli", args: args, stdin: request.Prompt, dir: request.WorkingDirectory})
	if err != nil {
		return RunResult{}, commandError("qodercli", out, err)
	}
	var parsed qoderResult
	if err := json.Unmarshal([]byte(out.stdout), &parsed); err != nil {
		return RunResult{}, fmt.Errorf("qodercli 输出非预期 JSON: %w（stdout 前 200 字: %s）", err, truncate(out.stdout, 200))
	}
	if parsed.IsError {
		return RunResult{}, fmt.Errorf("qodercli 报错: %s", parsed.Result)
	}
	return RunResult{
		Text:                 parsed.Result,
		DurationMilliseconds: out.durationMs,
		Tokens:               parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}, nil
}

func init() { Register(qoderEngine{}) }
