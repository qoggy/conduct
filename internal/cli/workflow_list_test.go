package cli

import (
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

func TestNodeIDStreamUsesCommaSeparator(t *testing.T) {
	wf := &workflow.Workflow{Definition: workflow.Definition{
		Nodes: []workflow.Node{
			{ID: workflow.NodeIDStart},
			{ID: "plan"},
			{ID: "code"},
			{ID: "test"},
			{ID: workflow.NodeIDEnd},
		},
		Edges: []workflow.Edge{
			{From: workflow.NodeIDStart, To: "plan"},
			{From: "plan", To: "code"},
			{From: "code", To: "test"},
			{From: "test", To: workflow.NodeIDEnd},
		},
	}}

	if got, want := nodeIDStream(wf), "plan,code,test"; got != want {
		t.Fatalf("nodeIDStream() = %q, want %q", got, want)
	}
}

func TestNodeIDStreamTruncatesAfterSixNodes(t *testing.T) {
	wf := &workflow.Workflow{Definition: workflow.Definition{
		Nodes: []workflow.Node{
			{ID: workflow.NodeIDStart},
			{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
			{ID: "e"}, {ID: "f"}, {ID: "g"}, {ID: "h"},
			{ID: workflow.NodeIDEnd},
		},
		Edges: []workflow.Edge{
			{From: workflow.NodeIDStart, To: "a"},
			{From: "a", To: "b"}, {From: "b", To: "c"},
			{From: "c", To: "d"}, {From: "d", To: "e"},
			{From: "e", To: "f"}, {From: "f", To: "g"},
			{From: "g", To: "h"}, {From: "h", To: workflow.NodeIDEnd},
		},
	}}

	if got, want := nodeIDStream(wf), "a,b,c,d,e,f+2"; got != want {
		t.Fatalf("nodeIDStream() = %q, want %q", got, want)
	}
}
