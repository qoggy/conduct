package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

const kiroAssistantMarker = "\x1b[m> \x1b[0m"

var ansiCSISequence = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// kiroEngine 通过 Kiro classic UI 的无头 chat 执行。Kiro 没有普通 chat 的机器输出模式，
// 因而先固定 classic UI 的 Markdown 渲染设置，再从原始 ANSI assistant 标记中提取最终回答。
type kiroEngine struct{}

func (kiroEngine) Descriptor() EngineDescriptor {
	return EngineDescriptor{
		Name: "kiro",
		Capability: EngineCapability{
			AllowsModel: true,
			ModelSuggestions: []string{
				"auto",
				"claude-sonnet-5",
				"claude-opus-4.8",
				"gpt-5.6-sol",
				"gpt-5.6-terra",
				"gpt-5.6-luna",
			},
			AllowsEffort: true,
			EffortValues: []string{"low", "medium", "high", "xhigh", "max"},
		},
		IconFilename: "kiro.png",
	}
}

func (kiroEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	// Kiro headless chat has no machine-output mode. We intentionally persist the
	// user's global classic-UI setting so stdout keeps the model's raw Markdown and
	// can be parsed without irreversible terminal rendering. Do not restore it:
	// conduct and the user must observe one stable setting, including across
	// concurrent Kiro sessions. This setting affects classic UI only.
	settingsOutput, err := runCommand(ctx, commandSpec{
		binary: "kiro-cli",
		args:   []string{"settings", "chat.disableMarkdownRendering", "true"},
	})
	if err != nil {
		return RunResult{DurationMilliseconds: settingsOutput.durationMs}, kiroCommandError("kiro-cli settings", settingsOutput, err)
	}

	args := []string{
		"chat",
		"--legacy-ui",
		"--no-interactive",
		"--wrap", "never",
		"--trust-all-tools",
		"--require-mcp-startup",
	}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort != "" {
		args = append(args, "--effort", request.Effort)
	}

	chatOutput, err := runCommand(ctx, commandSpec{
		binary: "kiro-cli",
		args:   args,
		stdin:  request.Prompt,
		dir:    request.WorkingDirectory,
	})
	durationMilliseconds := settingsOutput.durationMs + chatOutput.durationMs
	if err != nil {
		return RunResult{DurationMilliseconds: durationMilliseconds}, kiroCommandError("kiro-cli", chatOutput, err)
	}
	// stdout/stderr 是 classic UI 的混合终端记录，可能包含用户输入、回答、工具日志和源码。
	// 这里只解析 assistant 结构，禁止根据其中的自然语言关键词猜测权限或上下文等错误类型。
	text, err := parseKiroOutput(chatOutput.stdout, chatOutput.stderr)
	if err != nil {
		return RunResult{DurationMilliseconds: durationMilliseconds}, err
	}
	return RunResult{
		Text:                 text,
		DurationMilliseconds: durationMilliseconds,
		Tokens:               nil,
		SessionID:            nil,
	}, nil
}

func parseKiroOutput(stdout, stderr string) (string, error) {
	markerIndex := strings.LastIndex(stdout, kiroAssistantMarker)
	if markerIndex < 0 {
		return "", fmt.Errorf("kiro-cli returned unexpected output (first 500 characters of stdout: %s; first 500 characters of stderr: %s)",
			diagnosticSummary(stdout), diagnosticSummary(stderr))
	}
	answer := stdout[markerIndex+len(kiroAssistantMarker):]
	answer = ansiCSISequence.ReplaceAllString(answer, "")
	return strings.TrimRight(answer, "\r\n"), nil
}

func kiroCommandError(commandName string, output commandOutput, err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		diagnostic := strings.TrimSpace(cleanTerminalOutput(output.stderr))
		if diagnostic == "" {
			return fmt.Errorf("%s exited with code %d", commandName, exitError.ExitCode())
		}
		return fmt.Errorf("%s exited with code %d: %s", commandName, exitError.ExitCode(), truncate(diagnostic, 500))
	}
	return fmt.Errorf("failed to invoke kiro-cli: %w", err)
}

func diagnosticSummary(value string) string {
	cleaned := strings.TrimSpace(cleanTerminalOutput(value))
	if cleaned == "" {
		return "<empty>"
	}
	return truncate(cleaned, 500)
}

func cleanTerminalOutput(value string) string {
	return ansiCSISequence.ReplaceAllString(value, "")
}

func init() { Register(kiroEngine{}) }
