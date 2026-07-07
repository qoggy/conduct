package engine

import (
	"context"
	"testing"
)

// TestEnginesParseSessionID 验证四引擎各从自身 JSON 输出取会话 id 填充 RunResult.SessionID：
// claude-code / qoder 的 session_id、antigravity 的 conversation_id、codex 的 thread_id。
func TestEnginesParseSessionID(t *testing.T) {
	t.Run("claude-code", func(t *testing.T) {
		fakeBinary(t, "claude", `echo '{"result":"x","is_error":false,"session_id":"claude-sess"}'`)
		res, err := claudeCodeEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if res.SessionID != "claude-sess" {
			t.Errorf("应取 session_id，得到 %q", res.SessionID)
		}
	})

	t.Run("qoder", func(t *testing.T) {
		fakeBinary(t, "qodercli", `echo '{"result":"x","is_error":false,"session_id":"qoder-sess"}'`)
		res, err := qoderEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if res.SessionID != "qoder-sess" {
			t.Errorf("应取 session_id，得到 %q", res.SessionID)
		}
	})

	t.Run("antigravity", func(t *testing.T) {
		fakeBinary(t, "agy", `echo '{"status":"SUCCESS","response":"x","conversation_id":"agy-conv"}'`)
		res, err := antigravityEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if res.SessionID != "agy-conv" {
			t.Errorf("应取 conversation_id，得到 %q", res.SessionID)
		}
	})

	t.Run("codex", func(t *testing.T) {
		fakeBinary(t, "codex", `printf '%s\n' \
  '{"type":"thread.started","thread_id":"codex-thread"}' \
  '{"type":"item.completed","item":{"type":"agent_message","text":"x"}}'`)
		res, err := codexEngine{}.Run(context.Background(), RunRequest{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		if res.SessionID != "codex-thread" {
			t.Errorf("应取 thread_id，得到 %q", res.SessionID)
		}
	})
}
