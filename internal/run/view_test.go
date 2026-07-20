package run

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/engine"
)

func TestNewTraceViewReplayDerivation(t *testing.T) {
	sessionID := "two words'quoted"
	view := NewTraceView(TraceEntry{Engine: "codex", SessionID: &sessionID})
	if view.SessionReplayCommand == nil || *view.SessionReplayCommand != `codex resume 'two words'"'"'quoted'` {
		t.Fatalf("replay command 未 shell quote: %v", view.SessionReplayCommand)
	}

	for _, entry := range []TraceEntry{
		{Engine: "codex"},
		{Engine: "codex", SessionID: stringPointer("")},
		{Engine: "unknown", SessionID: stringPointer("sid")},
		{Engine: "kiro", SessionID: stringPointer("sid")},
	} {
		if got := NewTraceView(entry).SessionReplayCommand; got != nil {
			t.Errorf("entry=%+v replay 应为 nil，得到 %q", entry, *got)
		}
	}
}

func TestNewTraceViewDoesNotCallReplayForEmptySession(t *testing.T) {
	for _, sessionID := range []*string{nil, stringPointer("")} {
		newTraceView(TraceEntry{Engine: "unused", SessionID: sessionID}, func(string) (engine.EngineDescriptor, bool) {
			t.Fatal("nil/空 session id 不应查询 descriptor")
			return engine.EngineDescriptor{}, false
		})
	}
}

func TestNewTraceViewTreatsEmptyReplayAsUnavailable(t *testing.T) {
	describe := func(string) (engine.EngineDescriptor, bool) {
		return engine.EngineDescriptor{SessionReplayCommand: func(string) string { return "" }}, true
	}
	if got := newTraceView(TraceEntry{Engine: "empty-replay", SessionID: stringPointer("sid")}, describe).SessionReplayCommand; got != nil {
		t.Fatalf("空 replay 应归一化为 nil，得到 %q", *got)
	}
}

func TestSessionReplayCommandIsViewOnly(t *testing.T) {
	sessionID := "sid"
	entry := TraceEntry{Engine: "codex", SessionID: &sessionID}
	stored, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "sessionReplayCommand") {
		t.Fatalf("持久化 TraceEntry 不应含派生字段: %s", stored)
	}
	view, err := json.Marshal(NewTraceView(entry))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(view), `"sessionReplayCommand":"codex resume sid"`) {
		t.Fatalf("TraceView 应含派生字段: %s", view)
	}
}

func stringPointer(value string) *string { return &value }
