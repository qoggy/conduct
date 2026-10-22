package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
)

func isolatedSkillCLI(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Chdir(project)
	useTestLanguage(t, locale.English)
	return home, project
}

func executeInitForTest(t *testing.T, arguments ...string) (string, error) {
	t.Helper()
	command := newInitCommand()
	var output bytes.Buffer
	command.SetIn(strings.NewReader(""))
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(arguments)
	err := command.Execute()
	return output.String(), err
}

func TestInitNoninteractiveAndUsageErrorsDoNotWrite(t *testing.T) {
	home, project := isolatedSkillCLI(t)
	for _, arguments := range [][]string{nil, {"--json"}, {"--yes", "--agent", "unknown"}, {"--yes", "--agent", ""}, {"--yes", "--project", "--global"}, {"extra"}} {
		_, err := executeInitForTest(t, arguments...)
		var usage *usageError
		if !errors.As(err, &usage) {
			t.Errorf("%v: expected usage error, got %v", arguments, err)
		}
	}
	for _, path := range []string{filepath.Join(home, ".conduct"), filepath.Join(project, ".agents")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected write %s: %v", path, err)
		}
	}
}

func TestInitExplicitJSONAndLocalizedSuccess(t *testing.T) {
	home, project := isolatedSkillCLI(t)
	output, err := executeInitForTest(t, "--yes", "--json", "--agent", "claude-code", "--agent", "qwen-code", "--copy")
	if err != nil {
		t.Fatal(err)
	}
	var result skillinstall.Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid pure JSON: %v: %s", err, output)
	}
	if result.Method != skillinstall.Copy || len(result.Agents) != 2 || len(result.Targets) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(project, ".agents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit selection added Universal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".conduct", "skill-installations.json")); err != nil {
		t.Fatal(err)
	}
	useTestLanguage(t, locale.Chinese)
	output, err = executeInitForTest(t, "-y", "--agent", "codex")
	if err != nil || !strings.Contains(output, "Skill 安装完成") || !strings.Contains(output, "安装计划") {
		t.Fatalf("Chinese output: %s; %v", output, err)
	}
}

func TestInitDefaultSelectsUniversalAndDetectedOnly(t *testing.T) {
	home, _ := isolatedSkillCLI(t)
	if err := os.Mkdir(filepath.Join(home, ".qwen"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := executeInitForTest(t, "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result skillinstall.Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{}
	for _, id := range result.Agents {
		selected[id] = true
	}
	if !selected["codex"] || !selected["cline"] || !selected["qwen-code"] || selected["roo"] || selected["windsurf"] {
		t.Fatalf("default selection: %v", result.Agents)
	}
	if result.Method != skillinstall.Symlink {
		t.Fatalf("method = %s", result.Method)
	}
}

type failingSkillJSONWriter struct{ failure error }

func (writer failingSkillJSONWriter) Write([]byte) (int, error) { return 0, writer.failure }

func TestInitJSONWriteFailureReturnsErrorAfterInstallation(t *testing.T) {
	home, project := isolatedSkillCLI(t)
	failure := errors.New("output device failed")
	command := newInitCommand()
	command.SetArgs([]string{"--yes", "--json", "--agent", "codex"})
	command.SetOut(failingSkillJSONWriter{failure})
	command.SetErr(io.Discard)
	if err := command.Execute(); !errors.Is(err, failure) || !strings.Contains(err.Error(), "failed to write JSON") {
		t.Fatalf("JSON output error was not propagated: %v", err)
	}
	// Output failure does not claim to undo the successful installation.
	for _, path := range []string{filepath.Join(project, ".agents/skills/conduct/SKILL.md"), filepath.Join(home, ".conduct/skill-installations.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	jsonCommand := &cobra.Command{}
	jsonCommand.SetOut(failingSkillJSONWriter{failure})
	if err := printJSON(jsonCommand, map[string]string{"result": "ok"}); !errors.Is(err, failure) {
		t.Fatalf("helper lost write error: %v", err)
	}
}

func TestSkillPlanMarksOnlyExistingTargetsInBothLanguages(t *testing.T) {
	for _, language := range []locale.Language{locale.English, locale.Chinese} {
		useTestLanguage(t, language)
		var output bytes.Buffer
		plan := skillinstall.Plan{Scope: skillinstall.Project, Method: skillinstall.Copy, Targets: []skillinstall.Target{{Path: "/new/conduct", Kind: "directory"}}}
		if err := writeSkillPlan(&output, plan); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "Overwrite") || strings.Contains(output.String(), "覆盖") {
			t.Fatal("new targets are described as overwrites", output.String())
		}
		plan.Targets = append(plan.Targets, skillinstall.Target{Path: "/existing/conduct", Kind: "symlink", Target: "/central/conduct", Exists: true})
		output.Reset()
		if err := writeSkillPlan(&output, plan); err != nil {
			t.Fatal(err)
		}
		label := "/existing/conduct -> /central/conduct (exists)"
		heading := "Installation plan: Current directory / Independent copies"
		if language == locale.Chinese {
			label = "/existing/conduct -> /central/conduct（已存在）"
			heading = "安装计划：当前目录 / 独立副本"
		}
		if !strings.Contains(output.String(), label) || !strings.Contains(output.String(), heading) || strings.Count(output.String(), "/existing/conduct") != 1 {
			t.Fatal(output.String())
		}
	}
}

func TestInitHelpUsesSkillAgentRegistry(t *testing.T) {
	useTestLanguage(t, locale.English)
	if !strings.Contains(newInitCommand().Long, strings.Join(skillinstall.AgentIDs(), ", ")) {
		t.Fatal("help does not list registry agent IDs")
	}
}

func TestInitExplicitAgentWithNonDirectoryDetectionPath(t *testing.T) {
	home, _ := isolatedSkillCLI(t)
	file := filepath.Join(home, "xdg-file")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", file)
	if _, err := executeInitForTest(t, "--yes", "--json", "--agent", "codex"); err != nil {
		t.Fatal(err)
	}
}
