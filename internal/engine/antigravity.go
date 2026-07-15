package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// antigravityEngine 通过 Google Antigravity 无头 CLI（agy -p）执行。
// 与 claude 不同：prompt 走命令行参数（agy 无 stdin 形态）、无 --cwd（靠 cmd.Dir 切目录）、
// 无独立 effort 标志（推理强度编码在 model 标签后缀里）。用法见 docs/references/agy-print.md。
type antigravityEngine struct{}

func (antigravityEngine) Name() string { return "antigravity" }

// agyPromptLimitBytes 是经命令行参数下传 prompt 的保守上限。agy 无 stdin 形态，prompt 只能进 argv，
// 受 ARG_MAX 约束（macOS 约 1MB，含环境变量）。运行内核会把上游节点完整产物渲染进 prompt，长产物叠加
// 可能超限；超限时提前给可读错误，胜过 exec 抛出无指向性的 "argument list too long"。
const agyPromptLimitBytes = 256 * 1024

// antigravityResult 是 `agy -p ... --output-format json` 的 stdout 单对象（只取用到的字段）。
type antigravityResult struct {
	Status         string `json:"status"`
	Response       string `json:"response"`
	Error          string `json:"error"`
	ConversationID string `json:"conversation_id"`
	Usage          struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (antigravityEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	if len(request.Prompt) > agyPromptLimitBytes {
		return RunResult{}, fmt.Errorf("agy passes prompts as command-line arguments; prompt too long (%d bytes > %d-byte limit); use a stdin-based engine or reduce upstream output",
			len(request.Prompt), agyPromptLimitBytes)
	}
	// agy 默认会就工具权限征询；工作流是无人值守运行，故跳过权限门（对齐 claude/qoder 的 bypass）。
	// 注意：prompt 经 argv 传递，在多用户机器上对 `ps` 可见——这是 agy 无 stdin 形态的固有限制。
	args := []string{"-p", request.Prompt, "--output-format", "json", "--dangerously-skip-permissions"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	// request.Effort 对 agy 无独立标志（强度在 model 标签里），刻意忽略。

	out, err := runCommand(ctx, commandSpec{binary: "agy", args: args, dir: request.WorkingDirectory})
	if err != nil {
		return RunResult{}, commandError("agy", out, err)
	}
	var parsed antigravityResult
	if err := json.Unmarshal([]byte(out.stdout), &parsed); err != nil {
		return RunResult{}, fmt.Errorf("agy returned unexpected JSON: %w (first 200 characters of stdout: %s)", err, truncate(out.stdout, 200))
	}
	if parsed.Status != "SUCCESS" {
		reason := parsed.Error
		if reason == "" {
			reason = truncate(parsed.Response, 500)
		}
		return RunResult{}, fmt.Errorf("agy status %s: %s", parsed.Status, reason)
	}
	return RunResult{
		Text:                 parsed.Response,
		DurationMilliseconds: out.durationMs,
		Tokens:               parsed.Usage.TotalTokens,
		SessionID:            parsed.ConversationID,
	}, nil
}

func init() { Register(antigravityEngine{}) }
