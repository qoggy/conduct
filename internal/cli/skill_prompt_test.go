package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
)

func TestSkillNoteFitsTerminalWidth(t *testing.T) {
	command := &cobra.Command{}
	var output bytes.Buffer
	command.SetOut(&output)
	content := strings.Repeat("项目/", 40) + "conduct/SKILL.md"
	if err := writeSkillNote(command, "安装汇总", content); err != nil {
		t.Fatal(err)
	}
	var reconstructed strings.Builder
	for _, line := range strings.Split(ansi.Strip(output.String()), "\n") {
		if width := ansi.StringWidth(line); width > 80 {
			t.Fatalf("note exceeds 80-column terminal (%d): %q", width, line)
		}
		if value, found := strings.CutPrefix(line, "│  │ "); found {
			value = strings.TrimSuffix(value, "│")
			reconstructed.WriteString(strings.TrimRight(value, " "))
		}
	}
	if reconstructed.String() != content {
		t.Fatal("wrapped path lost content", output.String())
	}
}

func TestSkillPromptOutputPropagatesWriteErrors(t *testing.T) {
	failure := errors.New("terminal write failed")
	command := &cobra.Command{}
	command.SetOut(failingSkillJSONWriter{failure})
	for name, write := range map[string]func() error{
		"introduction": func() error { return writeSkillIntroduction(command) },
		"step":         func() error { return writeSkillStep(command, "Scope", "Current directory") },
		"note":         func() error { return writeSkillNote(command, "Summary", "target") },
		"conclusion":   func() error { return writeSkillConclusion(command, "Cancelled") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := write(); !errors.Is(err, failure) {
				t.Fatalf("lost terminal write error: %v", err)
			}
		})
	}
}
