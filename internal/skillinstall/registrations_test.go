package skillinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReinstallRetainsUnselectedTargets(t *testing.T) {
	for _, scope := range []Scope{Project, Global} {
		t.Run(string(scope), func(t *testing.T) {
			e := testEnvironment(t)
			old := mustInstall(t, e, []string{"codex"}, scope, Copy)
			marker := filepath.Join(old.Targets[0].Path, "obsolete")
			writeFile(t, marker, "old copy")
			e.Version = "v2"
			installed := mustInstall(t, e, []string{"claude-code"}, scope, Copy)
			if len(installed.Targets) != 1 || len(installed.Agents) != 1 || installed.Agents[0] != "claude-code" {
				t.Fatal("reinstall expanded the explicit installation plan", installed)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("reinstall changed the unselected old copy", err)
			}
			records, err := readRecords(old.RecordPath, e)
			if err != nil {
				t.Fatal(err)
			}
			if len(records.Installations) != 2 || records.Installations[0].ConductVersion != "v1.2.3" || records.Installations[1].ConductVersion != "v2" {
				t.Fatal("old installation or version lost", records)
			}
			e.Version = "v3"
			report, err := Sync(e, scope)
			if err != nil || report.Synced != 2 {
				t.Fatal(report, err)
			}
			assertBundle(t, old.Targets[0].Path)
			assertBundle(t, installed.Targets[0].Path)
		})
	}
}

func TestReinstallRetainsMixedMethodsAndCentralDependencies(t *testing.T) {
	tests := []struct {
		name   string
		agents [][]string
		method []Method
		count  int
	}{
		{"partial copy", [][]string{{"codex", "claude-code"}, {"claude-code"}}, []Method{Copy, Copy}, 2},
		{"replace one link", [][]string{{"codex", "claude-code", "qwen-code"}, {"claude-code"}}, []Method{Symlink, Copy}, 2},
		{"retain dependent link", [][]string{{"codex", "claude-code"}, {"codex"}}, []Method{Symlink, Copy}, 2},
		{"complete copy replacement", [][]string{{"codex", "claude-code"}, {"codex", "claude-code"}}, []Method{Symlink, Copy}, 1},
		{"complete symlink replacement", [][]string{{"codex", "claude-code"}, {"codex", "claude-code"}}, []Method{Copy, Symlink}, 1},
		{"retain implicit central", [][]string{{"claude-code", "qwen-code"}, {"claude-code", "qwen-code"}}, []Method{Symlink, Copy}, 2},
		{"deduplicate retained central", [][]string{{"codex", "claude-code"}, {"codex", "qwen-code"}, {"claude-code", "qwen-code"}}, []Method{Symlink, Symlink, Copy}, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := testEnvironment(t)
			paths := map[string]bool{}
			var latest Plan
			for index, agents := range test.agents {
				e.Version = fmt.Sprintf("v%d", index+1)
				latest = mustInstall(t, e, agents, Project, test.method[index])
				for _, target := range latest.Targets {
					paths[target.Path] = true
				}
			}
			records, err := readRecords(latest.RecordPath, e)
			if err != nil || len(records.Installations) != test.count {
				t.Fatal(records, err)
			}
			before := records
			// Repeating the same request must not accumulate registrations.
			mustInstall(t, e, test.agents[len(test.agents)-1], Project, test.method[len(test.method)-1])
			records, err = readRecords(latest.RecordPath, e)
			if err != nil || !reflect.DeepEqual(before, records) {
				t.Fatal("repeated install changed registrations", records, err)
			}
			kinds := map[string]string{}
			for _, record := range records.Installations {
				for _, target := range record.Targets {
					kinds[filepath.Join(e.Cwd, target.Path)] = target.Kind
				}
			}
			for path := range paths {
				if kinds[path] == "" {
					t.Fatal("lost registration", path, records)
				}
				if kinds[path] == "directory" {
					writeFile(t, filepath.Join(path, "obsolete"), "remove on sync")
				}
			}
			e.Version = "v4"
			report, err := Sync(e, Project)
			if err != nil || report.Synced != test.count {
				t.Fatal(report, err)
			}
			for path, kind := range kinds {
				info, err := os.Lstat(path)
				if err != nil || (info.Mode()&os.ModeSymlink != 0) != (kind == "symlink") {
					t.Fatal("sync changed the recorded method", path, info, err)
				}
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil {
					t.Fatal(err)
				}
				assertBundle(t, resolved)
			}
		})
	}
}

func TestRetainedInstallationFailureDoesNotBlockNewInstallation(t *testing.T) {
	e := testEnvironment(t)
	old := mustInstall(t, e, []string{"claude-code"}, Project, Copy)
	parent := filepath.Dir(old.Targets[0].Path)
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.Home, "missing"), parent); err != nil {
		t.Fatal(err)
	}
	e.Version = "v2"
	installed := mustInstall(t, e, []string{"codex"}, Project, Copy)
	writeFile(t, filepath.Join(installed.Targets[0].Path, "obsolete"), "remove on sync")
	e.Version = "v3"
	report, err := Sync(e, Project)
	if err == nil || report.Failed != 1 || report.Synced != 1 {
		t.Fatal(report, err)
	}
	records, err := readRecords(old.RecordPath, e)
	if err != nil || len(records.Installations) != 2 || records.Installations[0].ConductVersion != "v1.2.3" || records.Installations[1].ConductVersion != "v3" {
		t.Fatal(records, err)
	}
	assertBundle(t, installed.Targets[0].Path)
}

func TestReinstallRetainsHistoricalAgentDestination(t *testing.T) {
	e := testEnvironment(t)
	e.Env["CLAUDE_CONFIG_DIR"] = filepath.Join(e.Home, "first-claude")
	first := mustInstall(t, e, []string{"claude-code"}, Global, Copy)
	e.Env["CLAUDE_CONFIG_DIR"] = filepath.Join(e.Home, "second-claude")
	e.Version = "v2"
	second := mustInstall(t, e, []string{"claude-code"}, Global, Copy)
	for _, plan := range []Plan{first, second} {
		writeFile(t, filepath.Join(plan.Targets[0].Path, "obsolete"), "remove on sync")
	}
	report, err := Sync(e, Global)
	if err != nil || report.Synced != 2 {
		t.Fatal(report, err)
	}
	assertBundle(t, first.Targets[0].Path)
	assertBundle(t, second.Targets[0].Path)
}

func TestReinstallCannotNestInsideRetainedInstallation(t *testing.T) {
	e := testEnvironment(t)
	old := mustInstall(t, e, []string{"codex"}, Project, Copy)
	before, err := os.ReadFile(old.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(e.Cwd, ".claude/skills")
	if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old.Targets[0].Path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePlan(Request{Agents: []string{"claude-code"}, Scope: Project}, e); err == nil {
		t.Fatal("accepted a new target inside a retained installation")
	}
	after, err := os.ReadFile(old.RecordPath)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed plan changed records", err)
	}
	assertBundle(t, old.Targets[0].Path)
}

func TestReinstallCannotReplaceCentralCopyNeededByRetainedLink(t *testing.T) {
	e := testEnvironment(t)
	first := filepath.Join(e.Home, "first-shared")
	second := filepath.Join(e.Home, "second-shared")
	for _, directory := range []string{first, second, filepath.Join(e.Cwd, ".agents"), filepath.Join(e.Cwd, ".kiro")} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(e.Cwd, ".agents/skills")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	old := mustInstall(t, e, []string{"codex", "claude-code"}, Project, Symlink)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, filepath.Join(e.Cwd, ".kiro/skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePlan(Request{Agents: []string{"codex", "kiro-cli"}, Scope: Project}, e); err == nil {
		t.Fatal("accepted replacing a central copy still required by Claude")
	}
	if _, err := os.Lstat(filepath.Join(second, "conduct")); !os.IsNotExist(err) {
		t.Fatal("rejected plan wrote the new central copy", err)
	}
	assertBundle(t, filepath.Join(first, "conduct"))
	if _, err := readRecords(old.RecordPath, e); err != nil {
		t.Fatal(err)
	}
}
