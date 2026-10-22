package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
)

func TestUpdateOrchestration(t *testing.T) {
	for _, test := range []struct {
		name                               string
		current                            string
		check                              bool
		scope                              skillinstall.Scope
		installError                       bool
		syncError                          bool
		wantInstall, wantSync, wantHandoff int
	}{
		{name: "latest still syncs", current: "9.9.9", wantSync: 1},
		{name: "latest project", current: "9.9.9", scope: skillinstall.Project, wantSync: 1},
		{name: "latest global", current: "9.9.9", scope: skillinstall.Global, wantSync: 1},
		{name: "check latest", current: "9.9.9", check: true},
		{name: "check update", current: "1.0.0", check: true},
		{name: "upgrade handoff", current: "1.0.0", wantInstall: 1, wantHandoff: 1},
		{name: "upgrade project handoff", current: "1.0.0", scope: skillinstall.Project, wantInstall: 1, wantHandoff: 1},
		{name: "upgrade global handoff", current: "1.0.0", scope: skillinstall.Global, wantInstall: 1, wantHandoff: 1},
		{name: "install failure", current: "1.0.0", installError: true, wantInstall: 1},
		{name: "sync failure", current: "9.9.9", syncError: true, wantSync: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, _ := isolatedSkillCLI(t)
			previousVersion := version
			version = test.current
			t.Cleanup(func() { version = previousVersion })
			var output bytes.Buffer
			command := &cobra.Command{}
			command.SetOut(&output)
			queries, installs, syncs, handoffs := 0, 0, 0, 0
			sentinel := errors.New("fixture failed")
			dependencies := updateDependencies{
				executable: func() (string, error) { return "/temporary/conduct", nil },
				query: func(ctx context.Context, target string, pre bool) (*updateCandidate, error) {
					queries++
					if target != "v9.9.9" || !pre {
						t.Errorf("query lost target/pre: %q %v", target, pre)
					}
					return &updateCandidate{Version: "9.9.9", Install: func(ctx context.Context, path string) error {
						installs++
						if test.installError {
							return sentinel
						}
						return nil
					}}, nil
				},
				synchronize: func(command *cobra.Command, scope skillinstall.Scope) error {
					syncs++
					if scope != test.scope {
						t.Errorf("lost scope %q", scope)
					}
					if test.syncError {
						return sentinel
					}
					return nil
				},
				handoff: func(command *cobra.Command, path, newVersion string, scope skillinstall.Scope) error {
					handoffs++
					if scope != test.scope || path != "/temporary/conduct" || newVersion != "9.9.9" {
						t.Errorf("bad handoff %s %s %s", path, newVersion, scope)
					}
					return nil
				},
			}
			err := runUpdateWithDependencies(command, "v9.9.9", test.check, true, test.scope, dependencies)
			if (test.installError || test.syncError) != errors.Is(err, sentinel) {
				t.Fatalf("error = %v", err)
			}
			if queries != 1 || installs != test.wantInstall || syncs != test.wantSync || handoffs != test.wantHandoff {
				t.Fatalf("calls query/install/sync/handoff = %d/%d/%d/%d", queries, installs, syncs, handoffs)
			}
			if _, err := os.Lstat(filepath.Join(home, ".conduct")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("orchestration wrote metadata: %v", err)
			}
		})
	}
}

func TestUpdateCheckDoesNotReadInvalidSkillMetadata(t *testing.T) {
	home, _ := isolatedSkillCLI(t)
	if err := os.Mkdir(filepath.Join(home, ".conduct"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(home, ".conduct", "skill-installations.json")
	original := []byte("invalid JSON\n")
	if err := os.WriteFile(record, original, 0o600); err != nil {
		t.Fatal(err)
	}
	dependencies := defaultUpdateDependencies()
	dependencies.executable = func() (string, error) { return "/fixture/conduct", nil }
	dependencies.query = func(context.Context, string, bool) (*updateCandidate, error) {
		return &updateCandidate{Version: "99.0.0"}, nil
	}
	command := &cobra.Command{}
	command.SetOut(&bytes.Buffer{})
	if err := runUpdateWithDependencies(command, "", true, false, "", dependencies); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("check changed records")
	}
	entries, err := os.ReadDir(filepath.Dir(record))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("check created lock or files: %v", entries)
	}
}

func TestUpdateSynchronizesRetainedInstallations(t *testing.T) {
	home, project := isolatedSkillCLI(t)
	for _, agent := range []string{"codex", "claude-code"} {
		if _, err := executeInitForTest(t, "--yes", "--copy", "--agent", agent); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{filepath.Join(project, ".agents/skills/conduct/obsolete"), filepath.Join(project, ".claude/skills/conduct/obsolete")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("old copy"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previousVersion := version
	version = "v9.9.9"
	t.Cleanup(func() { version = previousVersion })
	dependencies := defaultUpdateDependencies()
	dependencies.executable = func() (string, error) { return "/temporary/conduct", nil }
	queries := 0
	dependencies.query = func(context.Context, string, bool) (*updateCandidate, error) {
		queries++
		return &updateCandidate{Version: "9.9.9"}, nil
	}
	command := &cobra.Command{}
	command.SetOut(&bytes.Buffer{})
	if err := runUpdateWithDependencies(command, "", false, false, skillinstall.Project, dependencies); err != nil {
		t.Fatal(err)
	}
	if queries != 1 {
		t.Fatal("update queried releases more than once", queries)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("update skipped an installed copy", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, ".conduct/skill-installations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records skillinstall.Records
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records.Installations) != 2 {
		t.Fatal("lost installation registrations", records)
	}
	for _, record := range records.Installations {
		if record.ConductVersion != version {
			t.Fatal("update did not record the successful version", record)
		}
	}
}

func TestUpdateHomebrewAndConflictingScopes(t *testing.T) {
	isolatedSkillCLI(t)
	dependencies := defaultUpdateDependencies()
	dependencies.executable = func() (string, error) { return "/opt/homebrew/Cellar/conduct/1/bin/conduct", nil }
	dependencies.query = func(context.Context, string, bool) (*updateCandidate, error) {
		t.Fatal("Homebrew queried releases")
		return nil, fmt.Errorf("unreachable")
	}
	command := &cobra.Command{}
	command.SetOut(&bytes.Buffer{})
	if err := runUpdateWithDependencies(command, "", false, false, "", dependencies); err == nil || !strings.Contains(err.Error(), "Homebrew") {
		t.Fatalf("error: %v", err)
	}
	command = newUpdateCommand()
	command.SetArgs([]string{"--project", "--global"})
	var usage *usageError
	if err := command.Execute(); !errors.As(err, &usage) {
		t.Fatalf("scope conflict: %v", err)
	}
}

func TestSyncRegisteredSkillOutputAndNoRecordWrites(t *testing.T) {
	home, project := isolatedSkillCLI(t)
	command := &cobra.Command{}
	var output bytes.Buffer
	command.SetOut(&output)
	if err := syncRegisteredSkills(command, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No Skills installed with `conduct init` were found; skipped.") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".conduct")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty sync wrote files: %v", err)
	}
	if _, err := executeInitForTest(t, "--yes", "--agent", "codex"); err != nil {
		t.Fatal(err)
	}
	environment, err := skillinstall.NewEnvironment(version)
	if err != nil {
		t.Fatal(err)
	}
	missing := t.TempDir()
	environment.Cwd = missing
	plan, err := skillinstall.CreatePlan(skillinstall.Request{Scope: skillinstall.Project, Agents: []string{"codex"}}, environment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skillinstall.Execute(plan, environment); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(missing); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := syncRegisteredSkills(command, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Skill updated: ") || !strings.Contains(output.String(), "Project directory is missing") {
		t.Fatal(output.String())
	}
	if !strings.Contains(output.String(), filepath.Base(project)) {
		t.Fatal("current project was not reported")
	}
	useTestLanguage(t, locale.Chinese)
	output.Reset()
	if err := syncRegisteredSkills(command, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "项目目录不存在，跳过 Skill 更新") || strings.Contains(output.String(), "registered project does not exist") {
		t.Fatal(output.String())
	}
	// Runtime diagnostics are returned for stderr, without repeating them in the
	// concise stdout status. No other record is prevented from synchronizing.
	parent := filepath.Join(project, ".agents/skills")
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "missing"), parent); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err = syncRegisteredSkills(command, "")
	if err == nil || !strings.Contains(err.Error(), "scope=project") || !strings.Contains(err.Error(), project) {
		t.Fatalf("missing contextual diagnostic: %v", err)
	}
	if !strings.Contains(output.String(), "Skill 更新失败") || strings.Contains(output.String(), "unsafe broken") {
		t.Fatal(output.String())
	}
}
