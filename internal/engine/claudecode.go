package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// claudeCodeEngine 通过 Anthropic Claude Code 无头 CLI（claude -p）执行。
// prompt 走 stdin，工作目录用 cmd.Dir；输出解析 --output-format json 的单对象。
type claudeCodeEngine struct{}

func (claudeCodeEngine) Name() string { return "claude-code" }

// claudeResult 是 `claude -p --output-format json` 的 stdout 单对象（只取用到的字段）。
type claudeResult struct {
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	SessionID string `json:"session_id"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (claudeCodeEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	args := []string{"-p", "--output-format", "json", "--permission-mode", "bypassPermissions"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	// effort 仅 claude-code 生效；"auto" / 空让 CLI 自决，不传。
	if request.Effort != "" && request.Effort != "auto" {
		args = append(args, "--effort", request.Effort)
	}

	out, err := runCommand(ctx, commandSpec{binary: "claude", args: args, stdin: request.Prompt, dir: request.WorkingDirectory})
	if err != nil {
		return RunResult{}, commandError("claude", out, err)
	}
	var parsed claudeResult
	if err := json.Unmarshal([]byte(out.stdout), &parsed); err != nil {
		return RunResult{}, fmt.Errorf("claude 输出非预期 JSON: %w（stdout 前 200 字: %s）", err, truncate(out.stdout, 200))
	}
	if parsed.IsError {
		return RunResult{}, fmt.Errorf("claude 报错: %s", parsed.Result)
	}
	return RunResult{
		Text:                 parsed.Result,
		DurationMilliseconds: out.durationMs,
		Tokens:               parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		SessionID:            parsed.SessionID,
	}, nil
}

func init() { Register(claudeCodeEngine{}) }
