package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
		// claude -p 应用层失败（如 prompt 过长）时退出码非 0 但 stderr 为空，真正原因在
		// stdout 的 JSON 里；能解析出具体原因就优先用它，否则回退退出码+stderr 摘要。
		if msg, ok := claudeStdoutFailureMessage(out.stdout); ok {
			return RunResult{}, fmt.Errorf("claude error: %s", msg)
		}
		return RunResult{}, commandError("claude", out, err)
	}
	var parsed claudeResult
	if err := json.Unmarshal([]byte(out.stdout), &parsed); err != nil {
		return RunResult{}, fmt.Errorf("claude returned unexpected JSON: %w (first 200 characters of stdout: %s)", err, truncate(out.stdout, 200))
	}
	if parsed.IsError {
		return RunResult{}, fmt.Errorf("claude error: %s", parsed.Result)
	}
	return RunResult{
		Text:                 parsed.Result,
		DurationMilliseconds: out.durationMs,
		Tokens:               parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		SessionID:            parsed.SessionID,
	}, nil
}

// claudeStdoutFailureMessage 尝试从非零退出的 claude -p 的 stdout 里取出应用层失败原因：
// claude 在 prompt 过长等场景下退出码非 0 但 stderr 为空，具体原因只在 stdout 的 JSON
// result 字段里。stdout 为空、非合法 JSON、或 result 为空时返回 false，交由调用方回退到
// 退出码+stderr 摘要，绝不用空字符串冒充有效错误信息。
func claudeStdoutFailureMessage(stdout string) (string, bool) {
	if strings.TrimSpace(stdout) == "" {
		return "", false
	}
	var parsed claudeResult
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return "", false
	}
	if result := strings.TrimSpace(parsed.Result); result != "" {
		return result, true
	}
	return "", false
}

func init() { Register(claudeCodeEngine{}) }
