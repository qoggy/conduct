package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexRunParsesAndPlumbs 端到端验证 codex 引擎的参数/stdin/cwd 接线与 JSONL 解析。
func TestCodexRunParsesAndPlumbs(t *testing.T) {
	dir := fakeBinary(t, "codex", `cat > "$FAKE_OUT/stdin"
echo "$@" > "$FAKE_OUT/args"
printf '%s\n' \
  '{"type":"thread.started","thread_id":"th-1"}' \
  '{"type":"turn.started"}' \
  '{"type":"item.started","item":{"id":"i1","type":"command_execution"}}' \
  '{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"最终产物"}}' \
  '{"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}'`)

	res, err := codexEngine{}.Run(context.Background(), RunRequest{
		Prompt: "做点事", Model: "gpt-5-codex", Effort: "high", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if res.Text != "最终产物" || res.Tokens != 24885 || res.SessionID != "th-1" {
		t.Errorf("解析错误：Text=%q Tokens=%d SessionID=%q", res.Text, res.Tokens, res.SessionID)
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "做点事" {
		t.Errorf("prompt 应经 stdin 传入，得到 %q", got)
	}
	args := read(t, filepath.Join(dir, "args"))
	for _, want := range []string{
		"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "--skip-git-repo-check", "-",
		"--model gpt-5-codex", "-c model_reasoning_effort=high",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("参数缺 %q，实际 %q", want, args)
		}
	}
}

func TestCodexRunTakesLastAgentMessage(t *testing.T) {
	res, err := parseCodexStream(strings.Join([]string{
		`{"type":"item.completed","item":{"type":"agent_message","text":"第一条"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"最后一条"}}`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatalf("解析报错: %v", err)
	}
	if res.Text != "最后一条" {
		t.Errorf("应取最后一条 agent_message，得到 %q", res.Text)
	}
}

func TestCodexRunTurnFailedIsError(t *testing.T) {
	// turn.failed 优先返回错误，即使后面还有 agent_message、且进程退 0。
	fakeBinary(t, "codex", `printf '%s\n' \
  '{"type":"thread.started","thread_id":"th-1"}' \
  '{"type":"turn.failed","message":"配额耗尽"}' \
  '{"type":"item.completed","item":{"type":"agent_message","text":"不该被采信"}}'`)
	_, err := codexEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "codex 报错") || !strings.Contains(err.Error(), "配额耗尽") {
		t.Errorf("turn.failed 应转译为错误，得到 %v", err)
	}
}

func TestCodexRunErrorEventIsError(t *testing.T) {
	_, err := parseCodexStream(`{"type":"error","message":"模型不可用"}` + "\n")
	if err == nil || !strings.Contains(err.Error(), "codex 报错") || !strings.Contains(err.Error(), "模型不可用") {
		t.Errorf("error 事件应转译为错误，得到 %v", err)
	}
}

func TestCodexRunUnparseableLineIsError(t *testing.T) {
	_, err := parseCodexStream(`{"type":"thread.started","thread_id":"th-1"}` + "\n这不是JSON\n")
	if err == nil || !strings.Contains(err.Error(), "无法解析") || !strings.Contains(err.Error(), "这不是JSON") {
		t.Errorf("无法解析的行应显式报错并附内容，得到 %v", err)
	}
}

func TestCodexRunNoAgentMessageIsError(t *testing.T) {
	// 既无失败事件也无 agent_message：不假装成功。
	_, err := parseCodexStream(strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`,
	}, "\n") + "\n")
	if err == nil || !strings.Contains(err.Error(), "未产出最终 agent_message") {
		t.Errorf("无 agent_message 应报错，得到 %v", err)
	}
}

func TestCodexRunEffortOmittedWhenEmpty(t *testing.T) {
	dir := fakeBinary(t, "codex", `echo "$@" > "$FAKE_OUT/args"
printf '%s\n' \
  '{"type":"item.completed","item":{"type":"agent_message","text":"x"}}'`)
	if _, err := (codexEngine{}).Run(context.Background(), RunRequest{Prompt: "p", WorkingDirectory: dir}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, filepath.Join(dir, "args")), "model_reasoning_effort") {
		t.Error("Effort 为空不应下传 -c model_reasoning_effort")
	}
}

func TestCodexRunNonZeroExit(t *testing.T) {
	fakeBinary(t, "codex", `echo "边界爆炸" >&2
exit 1`)
	_, err := codexEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "codex 退出码 1") || !strings.Contains(err.Error(), "边界爆炸") {
		t.Errorf("应转译退出码+stderr，得到 %v", err)
	}
}
