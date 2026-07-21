package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKiroRunConfiguresV3AndReturnsCompleteStdout(t *testing.T) {
	profile := t.TempDir()
	dir := fakeBinary(t, "kiro-cli", `
printf '%s ' "$@" > "$FAKE_OUT/chat-args"
cat > "$FAKE_OUT/stdin"
pwd > "$FAKE_OUT/pwd"
printf '%s' "$KIRO_HOME" > "$FAKE_OUT/kiro-home"
printf '先说明任务。## Final\n\n> quote\n\n\140\140\140go\nfmt.Println("ok")\n\140\140\140\n'
printf '[tool] Run Command\n[tool] status: Completed\n' >&2`)
	t.Setenv("KIRO_HOME", profile)

	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{
		Prompt: "完整 prompt", Model: "auto", Effort: "high", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if result.Text != "先说明任务。## Final\n\n> quote\n\n```go\nfmt.Println(\"ok\")\n```" {
		t.Fatalf("v3 stdout 应完整保留并只删除结尾换行，得到 %q", result.Text)
	}
	if result.Tokens != nil || result.SessionID != nil {
		t.Fatalf("Kiro 不提供 metadata，应返回 nil: %+v", result)
	}

	args := read(t, filepath.Join(dir, "chat-args"))
	for _, want := range []string{"chat", "--v3", "--no-interactive", "--wrap never", "--model auto", "--effort high"} {
		if !strings.Contains(args, want) {
			t.Errorf("chat 参数缺 %q，实际 %q", want, args)
		}
	}
	for _, unwanted := range []string{"--legacy-ui", "--trust-all-tools", "--require-mcp-startup"} {
		if strings.Contains(args, unwanted) {
			t.Errorf("v3 参数不应含 %q，实际 %q", unwanted, args)
		}
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "完整 prompt" {
		t.Errorf("prompt 应完整经 stdin 传入，得到 %q", got)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(dir, "pwd"))); !strings.Contains(got, filepath.Base(dir)) {
		t.Errorf("cmd.Dir 未生效: %q", got)
	}
	if got := read(t, filepath.Join(dir, "kiro-home")); got != profile {
		t.Errorf("KIRO_HOME 被覆盖: %q", got)
	}

	permissions := read(t, filepath.Join(profile, "settings", "permissions.yaml"))
	if !strings.Contains(permissions, "capability: all") || !strings.Contains(permissions, "effect: allow") {
		t.Fatalf("未写入 v3 全权限规则: %s", permissions)
	}
	info, err := os.Stat(filepath.Join(profile, "settings", "permissions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("新权限文件模式 = %o，期望 600", info.Mode().Perm())
	}
}

func TestKiroRunOmitsEmptyModelAndEffortAndAllowsEmptyStdout(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	dir := fakeBinary(t, "kiro-cli", `printf '%s ' "$@" > "$FAKE_OUT/chat-args"`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p", WorkingDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Fatalf("空 stdout 应作为空回答成功，得到 %q", result.Text)
	}
	args := read(t, filepath.Join(dir, "chat-args"))
	if strings.Contains(args, "--model") || strings.Contains(args, "--effort") {
		t.Fatalf("空 model/effort 不应下传，实际 %q", args)
	}
}

func TestEnsureKiroFullPermissionsPreservesAndDeduplicatesRules(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("KIRO_HOME", profile)
	path := filepath.Join(profile, "settings", "permissions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# keep this comment\nversion: 1\nrules:\n  - capability: shell\n    effect: deny\n    match: [\"rm *\"]\n"
	if err := os.WriteFile(path, []byte(existing), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := ensureKiroFullPermissions(); err != nil {
		t.Fatal(err)
	}
	if err := ensureKiroFullPermissions(); err != nil {
		t.Fatal(err)
	}
	updated := read(t, path)
	if !strings.Contains(updated, "# keep this comment") || !strings.Contains(updated, "capability: shell") {
		t.Fatalf("已有配置未保留:\n%s", updated)
	}
	if strings.Count(updated, "capability: all") != 1 || strings.Count(updated, "effect: allow") != 1 {
		t.Fatalf("all/allow 应只存在一次:\n%s", updated)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("已有文件模式应保留，得到 %o", info.Mode().Perm())
	}
}

func TestEnsureKiroFullPermissionsLeavesExistingRuleByteIdentical(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("KIRO_HOME", profile)
	path := filepath.Join(profile, "settings", "permissions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "# exact bytes stay untouched\nrules:\n  - effect: allow\n    capability: all\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureKiroFullPermissions(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != existing {
		t.Fatalf("已有 all/allow 时不应重写文件，得到:\n%s", got)
	}
}

func TestEnsureKiroFullPermissionsTreatsNullRulesAsEmpty(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("KIRO_HOME", profile)
	path := filepath.Join(profile, "settings", "permissions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1\nrules:\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureKiroFullPermissions(); err != nil {
		t.Fatal(err)
	}
	updated := read(t, path)
	if !strings.Contains(updated, "version: 1") || !strings.Contains(updated, "capability: all") || !strings.Contains(updated, "effect: allow") {
		t.Fatalf("null rules 应作为空列表追加 all/allow:\n%s", updated)
	}
}

func TestKiroRunInvalidPermissionsStopsBeforeChat(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("KIRO_HOME", profile)
	path := filepath.Join(profile, "settings", "permissions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("rules: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := fakeBinary(t, "kiro-cli", `touch "$FAKE_OUT/called"`)

	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "failed to configure kiro v3 permissions") {
		t.Fatalf("权限 YAML 错误诊断不正确: %v", err)
	}
	if result.DurationMilliseconds < 0 {
		t.Fatalf("耗时不可为负: %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "called")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("权限写入失败后不应启动 chat: %v", statErr)
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
			t.Setenv("KIRO_HOME", t.TempDir())
			fakeBinary(t, "kiro-cli", test.chatScript)
			_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
			if err == nil || err.Error() != test.want {
				t.Fatalf("得到 %v，期望 %q", err, test.want)
			}
		})
	}
}

func TestKiroRunDoesNotClassifyExternalTextByKeywords(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	fakeBinary(t, "kiro-cli", `
printf 'context window has overflowed\nConversation too short to compact\nis rejected because\nnon-interactive mode\n'
printf 'Command shell is rejected because it matches non-interactive mode.\n' >&2`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err != nil {
		t.Fatalf("外部文本中的自然语言不得被分类为引擎错误: %v", err)
	}
	want := "context window has overflowed\nConversation too short to compact\nis rejected because\nnon-interactive mode"
	if result.Text != want {
		t.Fatalf("stdout 应原样返回，得到 %q，期望 %q", result.Text, want)
	}
}

func TestKiroRunCleansChildProcessGroup(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	dir := fakeBinary(t, "kiro-cli", `
sleep 30 &
printf '%s' "$!" > "$FAKE_OUT/child-pid"
printf 'ok\n'`)
	result, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err != nil || result.Text != "ok" {
		t.Fatalf("Run 失败: result=%+v err=%v", result, err)
	}
	processID, err := strconv.Atoi(read(t, filepath.Join(dir, "child-pid")))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(processID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if killErr := syscall.Kill(processID, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("测试清理残留子进程失败: %v", killErr)
	}
	t.Fatalf("Kiro 子进程 %d 未被清理: %v", processID, err)
}

func TestKiroRunCancellationCleansChildProcessGroup(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	if err := ensureKiroFullPermissions(); err != nil {
		t.Fatal(err)
	}
	dir := fakeBinary(t, "kiro-cli", `
sleep 30 &
printf '%s' "$!" > "$FAKE_OUT/child-pid"
wait`)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, runErr := (kiroEngine{}).Run(ctx, RunRequest{Prompt: "p"})
	if runErr == nil {
		t.Fatal("取消的 Kiro 调用应返回错误")
	}
	processID, err := strconv.Atoi(read(t, filepath.Join(dir, "child-pid")))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(processID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if killErr := syscall.Kill(processID, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("测试清理残留子进程失败: %v", killErr)
	}
	t.Fatalf("取消后 Kiro 子进程 %d 未被清理: %v", processID, err)
}

func TestKiroRunUnlinksTemporaryFiles(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	temporaryDirectory := t.TempDir()
	t.Setenv("TMPDIR", temporaryDirectory)
	fakeBinary(t, "kiro-cli", `printf 'ok\n'`)
	if _, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Kiro 临时文件不应保留目录项: %v", entries)
	}
}

func TestKiroRunMissingBinary(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "failed to invoke kiro-cli:") {
		t.Fatalf("找不到 kiro-cli 的诊断错误: %v", err)
	}
}

func TestKiroFailureDiagnosticIsCleanAndTruncatedByCharacters(t *testing.T) {
	t.Setenv("KIRO_HOME", t.TempDir())
	value := strings.Repeat("界", 501)
	fakeBinary(t, "kiro-cli", `printf '\033[31m`+value+`\033[0m\n' >&2; exit 1`)
	_, err := (kiroEngine{}).Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), strings.Repeat("界", 500)+"…") || strings.Contains(err.Error(), "\x1b[") {
		t.Fatalf("诊断应清理 ANSI 并按字符截断: %v", err)
	}
}
