package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinary 在临时目录写一个 shell 假二进制并置于 PATH 首位，返回该目录（脚本可用 $FAKE_OUT 写旁证文件）。
// 用假二进制验证「参数/stdin/cwd 的接线 + JSON 解析 + 错误转译」，不触碰真引擎、不花钱。
func fakeBinary(t *testing.T, name, scriptBody string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + scriptBody + "\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("写假二进制失败: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_OUT", dir)
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读旁证文件 %s 失败: %v", path, err)
	}
	return string(data)
}

func TestClaudeCodeRunParsesAndPlumbs(t *testing.T) {
	dir := fakeBinary(t, "claude", `cat > "$FAKE_OUT/stdin"
echo "$@" > "$FAKE_OUT/args"
echo '{"result":"HELLO","is_error":false,"usage":{"input_tokens":3,"output_tokens":7},"session_id":"s1"}'`)

	res, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{
		Prompt: "做点事", Model: "claude-opus-4-8", Effort: "high", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if res.Text != "HELLO" || res.Tokens != 10 {
		t.Errorf("解析错误：Text=%q Tokens=%d", res.Text, res.Tokens)
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "做点事" {
		t.Errorf("prompt 应经 stdin 传入，得到 %q", got)
	}
	args := read(t, filepath.Join(dir, "args"))
	for _, want := range []string{"-p", "--output-format json", "--permission-mode bypassPermissions", "--model claude-opus-4-8", "--effort high"} {
		if !strings.Contains(args, want) {
			t.Errorf("参数缺 %q，实际 %q", want, args)
		}
	}
}

func TestClaudeCodeRunEffortAutoOmitted(t *testing.T) {
	dir := fakeBinary(t, "claude", `echo "$@" > "$FAKE_OUT/args"
echo '{"result":"x","is_error":false}'`)
	if _, err := (claudeCodeEngine{}).Run(context.Background(), RunRequest{Prompt: "p", Effort: "auto", WorkingDirectory: dir}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, filepath.Join(dir, "args")), "--effort") {
		t.Error("effort=auto 不应下传 --effort")
	}
}

func TestClaudeCodeRunNonZeroExit(t *testing.T) {
	fakeBinary(t, "claude", `echo "边界爆炸" >&2
exit 1`)
	_, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "claude 退出码 1") || !strings.Contains(err.Error(), "边界爆炸") {
		t.Errorf("应转译退出码+stderr，得到 %v", err)
	}
}

func TestClaudeCodeRunNonZeroExitStdoutResult(t *testing.T) {
	// 复现 claude -p 应用层失败（如 prompt 过长）：exit 非 0，stderr 为空，真正原因在 stdout 的 JSON 里。
	fakeBinary(t, "claude", `echo '{"type":"result","subtype":"success","is_error":true,"result":"Prompt is too long","session_id":"s1"}'
exit 1`)
	_, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "claude 报错: Prompt is too long") {
		t.Errorf("应从 stdout JSON 取出 result 作为错误信息，得到 %v", err)
	}
	if strings.Contains(err.Error(), "退出码") {
		t.Errorf("stdout 有有效 result 时不应回退到退出码摘要路径，得到 %v", err)
	}
}

func TestClaudeCodeRunNonZeroExitStdoutNotJSON(t *testing.T) {
	fakeBinary(t, "claude", `echo "不是JSON的杂散输出"
echo "边界爆炸" >&2
exit 1`)
	_, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "claude 退出码 1") || !strings.Contains(err.Error(), "边界爆炸") {
		t.Errorf("stdout 非合法 JSON 时应回退到退出码+stderr 报错路径，得到 %v", err)
	}
}

func TestClaudeCodeRunIsError(t *testing.T) {
	fakeBinary(t, "claude", `echo '{"result":"model said no","is_error":true}'`)
	_, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "claude 报错: model said no") {
		t.Errorf("is_error 应转译，得到 %v", err)
	}
}

func TestAntigravityRunUsesArgAndDir(t *testing.T) {
	dir := fakeBinary(t, "agy", `echo "$@" > "$FAKE_OUT/args"
pwd > "$FAKE_OUT/pwd"
echo '{"status":"SUCCESS","response":"hey","usage":{"total_tokens":42}}'`)
	res, err := antigravityEngine{}.Run(context.Background(), RunRequest{
		Prompt: "问候一下", Model: "Gemini 3.5 Flash (Medium)", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if res.Text != "hey" || res.Tokens != 42 {
		t.Errorf("解析错误：Text=%q Tokens=%d", res.Text, res.Tokens)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "args")), "问候一下") {
		t.Error("agy 的 prompt 应经命令行参数传入")
	}
	// 用临时目录 basename 判定 cmd.Dir 生效（回避 macOS /private 符号链接导致的路径前缀差异）。
	if got := strings.TrimSpace(read(t, filepath.Join(dir, "pwd"))); !strings.Contains(got, filepath.Base(dir)) {
		t.Errorf("cmd.Dir 未生效：pwd=%q 期望含 %q", got, filepath.Base(dir))
	}
}

func TestAntigravityRunNonSuccessStatus(t *testing.T) {
	fakeBinary(t, "agy", `echo '{"status":"ERROR","response":"quota exceeded"}'`)
	_, err := antigravityEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "agy 状态 ERROR") || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("非 SUCCESS 应转译，得到 %v", err)
	}
}

// TestAntigravityRunErrorFieldPreferredOverResponse 用真实复现的 agy 失败态 JSON（顶层 error 装简洁原因，
// response 是模型自己写的长篇叙述分析）验证失败分支报错信息取自 error 字段，而非把 response 长文当作报错原因。
func TestAntigravityRunErrorFieldPreferredOverResponse(t *testing.T) {
	fakeBinary(t, "agy", `printf '%s\n' '{"conversation_id":"2b8c49f2-0d3c-4082-b795-417fc5cadb7d","status":"ERROR","response":"# Analysis: 系统集成测试与环境检查\n\n## Summary\n本分析报告针对当前测试指令进行响应……（此处省略，是模型写的几千字长文分析）","error":"Cannot list directory file:///this/path/definitely/does/not/exist/xyz999 which does not exist.","duration_seconds":19.606553,"num_turns":1,"usage":{"input_tokens":67111,"output_tokens":5960,"thinking_tokens":4964,"total_tokens":73071}}'`)
	_, err := antigravityEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "Cannot list directory file:///this/path/definitely/does/not/exist/xyz999 which does not exist.") {
		t.Errorf("失败分支应优先采用 error 字段，得到 %v", err)
	}
	if strings.Contains(err.Error(), "Analysis") || strings.Contains(err.Error(), "Summary") {
		t.Errorf("失败分支不应把 response 里的长文分析当作报错信息，得到 %v", err)
	}
}

func TestQoderRunParsesAndPlumbs(t *testing.T) {
	dir := fakeBinary(t, "qodercli", `cat > "$FAKE_OUT/stdin"
echo "$@" > "$FAKE_OUT/args"
echo '{"result":"OK","is_error":false,"usage":{"input_tokens":5,"output_tokens":5}}'`)
	res, err := qoderEngine{}.Run(context.Background(), RunRequest{
		Prompt: "跑一下", Model: "Performance", Effort: "high", WorkingDirectory: dir,
	})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if res.Text != "OK" || res.Tokens != 10 {
		t.Errorf("解析错误：Text=%q Tokens=%d", res.Text, res.Tokens)
	}
	if got := read(t, filepath.Join(dir, "stdin")); got != "跑一下" {
		t.Errorf("prompt 应经 stdin 传入，得到 %q", got)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "args")), "--reasoning-effort high") {
		t.Error("qoder 应用 --reasoning-effort 下传 effort")
	}
}

// TestQoderRunIsErrorEmptyResultUsesErrorsArray 用真实复现的 qodercli 失败态 JSON（payload 超限：
// is_error=true，result 字段整个不存在故反序列化为空串，真正原因在 errors 数组里）验证报错信息
// 取自 errors，而不是把空 result 拼成一句没有内容的 "qodercli 报错: "。
func TestQoderRunIsErrorEmptyResultUsesErrorsArray(t *testing.T) {
	fakeBinary(t, "qodercli", `echo '{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["Qoder API error: PAYLOAD_TOO_LARGE - provider_error: prompt is too long: 1396788 tokens > 1000000 maximum"],"error_code":80411,"session_id":"s1"}'`)
	_, err := qoderEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "PAYLOAD_TOO_LARGE") {
		t.Errorf("result 为空时应从 errors 数组取报错信息，得到 %v", err)
	}
	if err != nil && strings.TrimSpace(err.Error()) == "qodercli 报错:" {
		t.Errorf("报错信息不应为空，得到 %v", err)
	}
}

// TestQoderRunIsErrorPreservesDuration 验证失败路径不再用 RunResult{} 空字面量丢弃已算好的
// out.durationMs：故意用 sleep 制造可观测的非零耗时，确认失败态返回的 RunResult 仍带上它。
func TestQoderRunIsErrorPreservesDuration(t *testing.T) {
	fakeBinary(t, "qodercli", `sleep 0.05
echo '{"is_error":true,"errors":["boom"]}'`)
	res, err := qoderEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
	if err == nil {
		t.Fatal("应报错")
	}
	if res.DurationMilliseconds <= 0 {
		t.Errorf("失败路径应保留真实耗时，得到 DurationMilliseconds=%d", res.DurationMilliseconds)
	}
}
