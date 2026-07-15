package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// codexEngine 通过 OpenAI Codex 无头 CLI（codex exec）执行。与其它三引擎不同，codex 的 --json
// 输出是 JSON Lines 事件流（每行一个事件对象），需逐行扫描按 type 归一化，而非解析单个 JSON 对象。
// prompt 走 stdin（codex exec … - 的 - 哨兵强制从 stdin 读取）；用法见 docs/references/codex.md。
type codexEngine struct{}

func (codexEngine) Name() string { return "codex" }

// codexEvent 是 codex --json 事件流的一行（只取用到的字段；未识别的 type 忽略）。
type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"` // thread.started
	Item     struct {
		Type string `json:"type"` // item.completed；agent_message 才取 text
		Text string `json:"text"`
	} `json:"item"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"` // turn.completed
	Message string `json:"message"` // error 事件的错误文本
}

func (codexEngine) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	// 末尾 "-" 在 PROMPT 位，强制从 stdin 读取 prompt（规避 argv 的 ARG_MAX 上限，与 claude/qoder 同族）。
	args := []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "-"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	// codex 无专用调优标志，-c key=value 覆盖配置项（value 按 TOML 解析失败即当字面字符串）。
	if request.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+request.Effort)
	}

	out, err := runCommand(ctx, commandSpec{binary: "codex", args: args, stdin: request.Prompt, dir: request.WorkingDirectory})
	if err != nil {
		return RunResult{}, commandError("codex", out, err)
	}
	result, err := parseCodexStream(out.stdout)
	if err != nil {
		return RunResult{}, err
	}
	result.DurationMilliseconds = out.durationMs
	return result, nil
}

// parseCodexStream 逐行扫描 codex --json 事件流并归一化为 RunResult（不含耗时，由调用方补）。
// 逐行读法同 store.LoadTrace：用 bufio.Reader.ReadBytes('\n') 而非 Scanner，避开单行 MB 级产物撑爆
// Scanner 的 token 上限。失败事件（turn.failed / error）优先返回错误——即便进程退 0；无法解析的行
// 显式报错（附该行前 200 字），不静默跳过；既无失败事件也无 agent_message 报错，不假装成功。
func parseCodexStream(stdout string) (RunResult, error) {
	var (
		result     RunResult
		sawMessage bool // 是否见过 agent_message（取最后一条）
	)
	reader := bufio.NewReader(strings.NewReader(stdout))
	for lineNumber := 1; ; lineNumber++ {
		chunk, readErr := reader.ReadBytes('\n')
		line := bytes.TrimSpace(chunk)
		if len(line) > 0 {
			var event codexEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return RunResult{}, fmt.Errorf("codex returned unexpected JSON: failed to parse line %d: %w (first 200 characters of line: %s)",
					lineNumber, err, truncate(string(line), 200))
			}
			switch event.Type {
			case "thread.started":
				result.SessionID = event.ThreadID
			case "item.completed":
				if event.Item.Type == "agent_message" {
					result.Text = event.Item.Text // 取最后一条 agent_message
					sawMessage = true
				}
			case "turn.completed":
				result.Tokens = event.Usage.InputTokens + event.Usage.OutputTokens // 取最后一个 turn
			case "turn.failed", "error":
				return RunResult{}, fmt.Errorf("codex error: %s", codexFailureMessage(event))
			default:
				// turn.started / item.started / 其它 item.* 等事件忽略。
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return RunResult{}, fmt.Errorf("failed to read codex output: %w", readErr)
		}
	}
	if !sawMessage {
		return RunResult{}, fmt.Errorf("codex did not produce a final agent_message")
	}
	return result, nil
}

// codexFailureMessage 从失败事件里取可读的错误文本：error 事件带 message，turn.failed 的错误体
// 结构随版本而异，无 message 时回退给出原始事件的类型占位，绝不静默丢失失败信号。
func codexFailureMessage(event codexEvent) string {
	if event.Message != "" {
		return event.Message
	}
	return event.Type
}

func init() { Register(codexEngine{}) }
