package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// qoderEngine 通过 Qoder 无头 CLI（qodercli -p）执行。qodercli 与 claude 同族：prompt 走 stdin、
// --output-format json 输出单个 result 对象；调优字段用 --reasoning-effort（与模型解耦）。
// 用法见 docs/references/qodercli-print.md。
type qoderEngine struct{}

func (qoderEngine) Name() string { return "qoder" }

// qoderResult 是 `qodercli -p --output-format json` 的 stdout 单对象（只取用到的字段）。
// is_error=true 时 result 可能整个不存在（反序列化为空串），真正的失败原因在 errors 数组里。
type qoderResult struct {
	Result    string   `json:"result"`
	IsError   bool     `json:"is_error"`
	Errors    []string `json:"errors"`
	SessionID string   `json:"session_id"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// qoderFailureMessage 从失败态取可读报错：is_error=true 时优先用 errors 数组（真正的失败原因，
// result 这时甚至整个不存在）；errors 为空退而求其 result；都为空才用兜底提示，绝不让报错信息
// 是空字符串。
func qoderFailureMessage(parsed qoderResult) string {
	if len(parsed.Errors) > 0 {
		trimmed := make([]string, len(parsed.Errors))
		for i, e := range parsed.Errors {
			trimmed[i] = strings.TrimSpace(e)
		}
		return strings.Join(trimmed, "; ")
	}
	if result := strings.TrimSpace(parsed.Result); result != "" {
		return result
	}
	return "qodercli returned no specific error information"
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
		return RunResult{}, fmt.Errorf("qodercli returned unexpected JSON: %w (first 200 characters of stdout: %s)", err, truncate(out.stdout, 200))
	}
	if parsed.IsError {
		return RunResult{DurationMilliseconds: out.durationMs}, fmt.Errorf("qodercli error: %s", qoderFailureMessage(parsed))
	}
	return RunResult{
		Text:                 parsed.Result,
		DurationMilliseconds: out.durationMs,
		Tokens:               parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		SessionID:            parsed.SessionID,
	}, nil
}

func init() { Register(qoderEngine{}) }
