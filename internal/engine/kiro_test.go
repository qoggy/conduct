package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKiroRunConfiguresAndPlumbsChat(t *testing.T) {
	dir := fakeBinary(t, "kiro-cli", `
printf '%s\n' "$1" >> "$FAKE_OUT/sequence"
if [ "$1" = settings ]; then
  printf '%s ' "$@" > "$FAKE_OUT/settings-args"
  printf '%s' "$KIRO_HOME" > "$FAKE_OUT/kiro-home"
  exit 0
fi
printf '%s ' "$@" > "$FAKE_OUT/chat-args"
cat > "$FAKE_OUT/stdin"
pwd > "$FAKE_OUT/pwd"
printf 'tool log\n\033[m> \033[0mintermediate\n\033[m> \033[0m## Final\n\n> quote\n\n\140\140\140go\nfmt.Println("ok")\n\140\140\140\n\033[1mtext\033[0m\n'
printf '▸ Credits: 0.02 • Time: 3s\n' >&2`)
	t.Setenv("KIRO_HOME", "/tmp/existing-kiro-profile")

	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{
		Prompt: "完整 prompt", Model: "auto", Effort: "high", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if result.Text != "## Final\n\n> quote\n\n```go\nfmt.Println(\"ok\")\n```\ntext" {
		t.Fatalf("最终回答解析错误: %q", result.Text)
	}
	if result.Tokens != nil || result.SessionID != nil {
		t.Fatalf("Kiro 不提供 metadata，应返回 nil: %+v", result)
	}
	if got := read(t, filepath.Join(dir, "sequence")); got != "settings\nchat\n" {
		t.Fatalf("设置必须先于 chat，得到 %q", got)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(dir, "settings-args"))); got != "settings chat.disableMarkdownRendering true" {
		t.Fatalf("设置参数错误: %q", got)
	}
	args := read(t, filepath.Join(dir, "chat-args"))
	for _, want := range []string{
		"chat", "--legacy-ui", "--no-interactive", "--wrap never", "--trust-all-tools",
		"--require-mcp-startup", "--model auto", "--effort high",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("chat 参数缺 %q，实际 %q", want, args)
		}
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "完整 prompt" {
		t.Errorf("prompt 应完整经 stdin 传入，得到 %q", got)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(dir, "pwd"))); !strings.Contains(got, filepath.Base(dir)) {
		t.Errorf("cmd.Dir 未生效: %q", got)
	}
	if got := read(t, filepath.Join(dir, "kiro-home")); got != "/tmp/existing-kiro-profile" {
		t.Errorf("KIRO_HOME 被覆盖: %q", got)
	}
}

func TestKiroRunOmitsEmptyModelAndEffort(t *testing.T) {
	dir := fakeBinary(t, "kiro-cli", `
if [ "$1" = settings ]; then exit 0; fi
printf '%s ' "$@" > "$FAKE_OUT/chat-args"
printf '\033[m> \033[0m'`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p", WorkingDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Fatalf("标记后空正文应成功，得到 %q", result.Text)
	}
	args := read(t, filepath.Join(dir, "chat-args"))
	if strings.Contains(args, "--model") || strings.Contains(args, "--effort") {
		t.Fatalf("空 model/effort 不应下传，实际 %q", args)
	}
}

func TestKiroRunSettingsFailureStopsBeforeChat(t *testing.T) {
	dir := fakeBinary(t, "kiro-cli", `
printf '%s\n' "$1" >> "$FAKE_OUT/sequence"
if [ "$1" = settings ]; then
  sleep 0.05
  echo 'settings denied' >&2
  exit 1
fi
exit 99`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "kiro-cli settings exited with code 1") || !strings.Contains(err.Error(), "settings denied") {
		t.Fatalf("设置失败诊断错误: %v", err)
	}
	if result.DurationMilliseconds <= 0 {
		t.Fatalf("设置失败应保留耗时: %+v", result)
	}
	if got := read(t, filepath.Join(dir, "sequence")); got != "settings\n" {
		t.Fatalf("设置失败后不应启动 chat: %q", got)
	}
}

func TestKiroRunFailureDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name       string
		chatScript string
		want       string
	}{
		{name: "nonzero stderr", chatScript: `echo 'bad model' >&2; exit 3`, want: "kiro-cli exited with code 3: bad model"},
		{name: "nonzero empty stderr", chatScript: `echo 'ordinary stdout must not become a diagnostic'; exit 1`, want: "kiro-cli exited with code 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeBinary(t, "kiro-cli", "if [ \"$1\" = settings ]; then exit 0; fi\n"+test.chatScript)
			_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
			if err == nil || err.Error() != test.want {
				t.Fatalf("得到 %v，期望 %q", err, test.want)
			}
		})
	}
}

func TestKiroRunDoesNotClassifyExternalTextByKeywords(t *testing.T) {
	fakeBinary(t, "kiro-cli", `
if [ "$1" = settings ]; then exit 0; fi
printf 'tool output: The context window has overflowed; Conversation too short to compact.\n'
printf 'Command shell is rejected because it matches non-interactive mode.\n' >&2
printf '\033[m> \033[0mcontext window has overflowed\nConversation too short to compact\nis rejected because\nnon-interactive mode\n'`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err != nil {
		t.Fatalf("外部文本中的自然语言不得被分类为引擎错误: %v", err)
	}
	want := "context window has overflowed\nConversation too short to compact\nis rejected because\nnon-interactive mode"
	if result.Text != want {
		t.Fatalf("最终回答应原样返回，得到 %q，期望 %q", result.Text, want)
	}
}

func TestKiroRunZeroExitWithoutAssistantMarkerIsGenericUnexpectedOutput(t *testing.T) {
	fakeBinary(t, "kiro-cli", `
if [ "$1" = settings ]; then exit 0; fi
printf 'The context window has overflowed\n'
printf 'Conversation too short to compact; Command shell is rejected because it matches non-interactive mode.\n' >&2`)
	_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "kiro-cli returned unexpected output") {
		t.Fatalf("缺少 assistant 结构时应返回通用 unexpected output，得到 %v", err)
	}
	if strings.Contains(err.Error(), "kiro-cli context window overflow") || strings.Contains(err.Error(), "kiro-cli tool permission denied") {
		t.Fatalf("不得根据非结构化文本猜测错误类型: %v", err)
	}
}

func TestKiroRunUnexpectedOutputIncludesCleanSummaries(t *testing.T) {
	fakeBinary(t, "kiro-cli", `
if [ "$1" = settings ]; then exit 0; fi
printf '\033[31mstdout body\033[0m\n'
printf '\033[32mstderr body\033[0m\n' >&2`)
	_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "kiro-cli returned unexpected output") ||
		!strings.Contains(err.Error(), "stdout body") || !strings.Contains(err.Error(), "stderr body") || strings.Contains(err.Error(), "\x1b[") {
		t.Fatalf("unexpected output 诊断错误: %v", err)
	}
}

func TestKiroRunMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "failed to invoke kiro-cli:") || strings.Contains(err.Error(), "kiro-cli settings") {
		t.Fatalf("找不到 kiro-cli 的诊断错误: %v", err)
	}
}

func TestKiroOutputSummaryIsTruncatedByCharacters(t *testing.T) {
	value := strings.Repeat("界", 501)
	_, err := parseKiroOutput(value, "")
	if err == nil || !strings.Contains(err.Error(), strings.Repeat("界", 500)+"…") {
		t.Fatalf("摘要应按字符截断: %v", err)
	}
}

func TestKiroRunDoesNotCreateProfileFilesDirectly(t *testing.T) {
	profile := t.TempDir()
	dir := fakeBinary(t, "kiro-cli", `
if [ "$1" = settings ]; then exit 0; fi
printf '\033[m> \033[0mok\n'`)
	t.Setenv("KIRO_HOME", profile)
	if _, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p", WorkingDirectory: dir}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("conduct 不应直接写 profile 文件: %v", entries)
	}
}
