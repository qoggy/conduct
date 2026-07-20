package run

import "github.com/qoggy/conduct/internal/engine"

// TraceView adds read-time derived presentation data without changing trace.jsonl.
type TraceView struct {
	TraceEntry
	SessionReplayCommand *string `json:"sessionReplayCommand"`
}

// NewTraceView is the single derivation point shared by CLI JSON and HTTP responses.
func NewTraceView(entry TraceEntry) TraceView {
	return newTraceView(entry, engine.Describe)
}

func newTraceView(entry TraceEntry, describe func(string) (engine.EngineDescriptor, bool)) TraceView {
	view := TraceView{TraceEntry: entry}
	if entry.SessionID == nil || *entry.SessionID == "" {
		return view
	}
	descriptor, ok := describe(entry.Engine)
	if !ok || descriptor.SessionReplayCommand == nil {
		return view
	}
	command := descriptor.SessionReplayCommand(*entry.SessionID)
	if command != "" {
		view.SessionReplayCommand = &command
	}
	return view
}

// NewTraceViews derives presentation fields for a trace slice while preserving order.
func NewTraceViews(entries []TraceEntry) []TraceView {
	views := make([]TraceView, len(entries))
	for index, entry := range entries {
		views[index] = NewTraceView(entry)
	}
	return views
}
