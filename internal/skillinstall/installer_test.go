package skillinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func testEnvironment(t *testing.T) Environment {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, path := range []string{home, project} {
		if err = os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	return Environment{Cwd: project, Home: home, Version: "v1.2.3", Env: map[string]string{}}
}
func mustPlan(t *testing.T, e Environment, agents []string, scope Scope, method Method) Plan {
	t.Helper()
	plan, err := CreatePlan(Request{Agents: agents, Scope: scope, Method: method}, e)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func mustInstall(t *testing.T, e Environment, agents []string, scope Scope, method Method) Plan {
	t.Helper()
	plan := mustPlan(t, e, agents, scope, method)
	if _, err := Execute(plan, e); err != nil {
		t.Fatal(err)
	}
	return plan
}
func assertBundle(t *testing.T, target string) {
	t.Helper()
	resources := []string{"SKILL.md", "LICENSE", "scripts/workflow_help.py", "scripts/run_help.py"}
	for _, path := range resources {
		info, err := os.Stat(filepath.Join(target, path))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing resource %s: %v", path, err)
		}
	}
	entries := 0
	err := filepath.WalkDir(target, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			entries++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries != len(resources) {
		t.Fatalf("got %d files", entries)
	}
}
func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}
func writeRecord(t *testing.T, e Environment, records Records) {
	t.Helper()
	path, err := metadataPath(e)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

func TestRegistryAndDefaults(t *testing.T) {
	e := testEnvironment(t)
	agents, err := ListAgents(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 11 {
		t.Fatal(len(agents))
	}
	universals := 0
	for _, agent := range agents {
		if agent.Universal {
			universals++
		}
	}
	if universals != 6 {
		t.Fatal(universals)
	}
	e.Env["CLAUDE_CONFIG_DIR"] = "relative-claude"
	e.Env["CODEX_HOME"] = "relative-codex"
	e.Env["XDG_CONFIG_HOME"] = "relative-invalid"
	if err = os.Mkdir(filepath.Join(e.Cwd, "relative-claude"), 0755); err != nil {
		t.Fatal(err)
	}
	agents, err = ListAgents(e)
	if err != nil {
		t.Fatal(err)
	}
	if !agents[2].Detected {
		t.Fatal("relative CLAUDE_CONFIG_DIR was not detected")
	}
	definitions := registry(e)
	if definitions[2].global != filepath.Join(e.Cwd, "relative-claude/skills") {
		t.Fatal(definitions[2].global)
	}
	if definitions[3].detection[0] != filepath.Join(e.Home, ".config/opencode") {
		t.Fatal(definitions[3])
	}
}

func TestCopyIgnoresUnusedCentralDirectory(t *testing.T) {
	for _, scope := range []Scope{Project, Global} {
		for _, fault := range []string{"broken", "cyclic"} {
			for _, test := range []struct {
				name   string
				agents []string
				method Method
			}{
				{"implicit single copy", []string{"claude-code"}, ""},
				{"explicit single copy", []string{"claude-code"}, Copy},
				{"multiple copies", []string{"claude-code", "qwen-code"}, Copy},
			} {
				t.Run(string(scope)+"/"+fault+"/"+test.name, func(t *testing.T) {
					e := testEnvironment(t)
					root := e.Cwd
					if scope == Global {
						root = e.Home
					}
					parent := filepath.Join(root, ".agents")
					if err := os.Mkdir(parent, 0755); err != nil {
						t.Fatal(err)
					}
					link := "missing-skills"
					if fault == "cyclic" {
						link = "skills"
					}
					alias := filepath.Join(parent, "skills")
					if err := os.Symlink(link, alias); err != nil {
						t.Fatal(err)
					}
					plan := mustInstall(t, e, test.agents, scope, test.method)
					if plan.Method != Copy || len(plan.Targets) != len(test.agents) {
						t.Fatal("copy added an unrelated target", plan)
					}
					for _, target := range plan.Targets {
						assertBundle(t, target.Path)
					}
					if actual, err := os.Readlink(alias); err != nil || actual != link {
						t.Fatal("copy changed the unused central directory", actual, err)
					}
					// Both symlink mode and an explicitly selected universal target
					// actually need this directory, so its errors must still fail.
					for _, request := range []Request{
						{Agents: []string{"claude-code", "qwen-code"}, Scope: scope, Method: Symlink},
						{Agents: []string{"codex"}, Scope: scope, Method: Copy},
					} {
						if _, err := CreatePlan(request, e); err == nil {
							t.Fatal("accepted an unusable directory required by the plan", request)
						}
					}
				})
			}
		}
	}
}

func TestResolveMethodUsesPhysicalSelectedTargets(t *testing.T) {
	for _, scope := range []Scope{Project, Global} {
		t.Run(string(scope), func(t *testing.T) {
			e := testEnvironment(t)
			root := e.Cwd
			if scope == Global {
				root = e.Home
			}
			claude := filepath.Join(root, ".claude/skills")
			qwen := filepath.Join(root, ".qwen/skills")
			if err := os.MkdirAll(claude, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(qwen), 0755); err != nil {
				t.Fatal(err)
			}
			request := Request{Agents: []string{"claude-code", "qwen-code", "claude-code"}, Scope: scope}
			method, err := ResolveMethod(request, e)
			if err != nil || method != Symlink {
				t.Fatal("distinct targets must offer a method choice", method, err)
			}
			if err := os.Symlink(claude, qwen); err != nil {
				t.Fatal(err)
			}
			method, err = ResolveMethod(request, e)
			if err != nil || method != Copy {
				t.Fatal("physical aliases must skip the method choice", method, err)
			}
			if _, err := os.Lstat(filepath.Join(e.Home, ".conduct")); !os.IsNotExist(err) {
				t.Fatal("method selection wrote metadata", err)
			}
		})
	}
}
func TestReadOnlyPlanAndCompleteCopy(t *testing.T) {
	e := testEnvironment(t)
	plan := mustPlan(t, e, []string{"codex", "cursor"}, Project, "")
	if plan.Method != Copy || len(plan.Targets) != 1 {
		t.Fatal(plan)
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".conduct")); !os.IsNotExist(err) {
		t.Fatalf("plan wrote metadata: %v", err)
	}
	target := plan.Targets[0].Path
	writeFile(t, filepath.Join(target, "obsolete"), "old")
	plan = mustPlan(t, e, []string{"codex"}, Project, "")
	if _, err := Execute(plan, e); err != nil {
		t.Fatal(err)
	}
	assertBundle(t, target)
	if _, err := os.Stat(filepath.Join(e.Cwd, ".conduct")); !os.IsNotExist(err) {
		t.Fatal("project metadata created")
	}
	records, err := readRecords(plan.RecordPath, e)
	if err != nil {
		t.Fatal(err)
	}
	if records.Installations[0].Targets[0].Path != ".agents/skills/conduct" {
		t.Fatal(records)
	}
}
func TestSymlinkReplacementAndPreservation(t *testing.T) {
	e := testEnvironment(t)
	plan := mustInstall(t, e, []string{"codex", "claude-code"}, Project, "")
	if plan.Method != Symlink {
		t.Fatal(plan)
	}
	link := filepath.Join(e.Cwd, ".claude/skills/conduct")
	initial, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(mustPlan(t, e, plan.Agents, Project, Symlink), e); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(initial, after) {
		t.Fatal("correct link replaced")
	}
	victim := filepath.Join(e.Home, "victim")
	writeFile(t, filepath.Join(victim, "keep"), "untouched")
	brokenParent := filepath.Join(victim, "broken-parent")
	if err = os.Symlink(filepath.Join(victim, "missing-parent"), brokenParent); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(victim, "loop")
	if err = os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{victim, filepath.Join(victim, "missing"), filepath.Join(brokenParent, "child"), loop, link} {
		if err = os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(wrong, link); err != nil {
			t.Fatal(err)
		}
		if _, err = Execute(mustPlan(t, e, plan.Agents, Project, Symlink), e); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(filepath.Join(victim, "keep")); err != nil || string(data) != "untouched" {
			t.Fatal("followed old final symlink")
		}
	}
}
func TestParentAliasesDeduplicateAndCannotDeleteRoot(t *testing.T) {
	e := testEnvironment(t)
	if err := os.MkdirAll(filepath.Join(e.Cwd, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".agents", filepath.Join(e.Cwd, ".claude")); err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, e, []string{"codex", "claude-code"}, Project, "")
	if len(plan.Targets) != 1 || plan.Method != Copy {
		t.Fatal(plan)
	}
	if _, err := Execute(plan, e); err != nil {
		t.Fatal(err)
	}
	dangerous := testEnvironment(t)
	dangerous.Cwd = filepath.Join(filepath.Dir(dangerous.Cwd), "conduct")
	if err := os.MkdirAll(filepath.Join(dangerous.Cwd, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(dangerous.Cwd), filepath.Join(dangerous.Cwd, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePlan(Request{Agents: []string{"codex"}, Scope: Project}, dangerous); err == nil {
		t.Fatal("target encompassing project was accepted")
	}
}
func TestGlobalExternalClaudeAndCopy(t *testing.T) {
	e := testEnvironment(t)
	e.Env["CLAUDE_CONFIG_DIR"] = filepath.Join(filepath.Dir(e.Home), "external-claude")
	plan := mustInstall(t, e, []string{"claude-code", "kiro-cli"}, Global, Copy)
	if len(plan.Targets) != 2 {
		t.Fatal(plan)
	}
	for _, target := range plan.Targets {
		assertBundle(t, target.Path)
	}
	records, err := readRecords(plan.RecordPath, e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(records.Installations[0].Targets[0].Path, "..") {
		t.Fatal(records)
	}
	changed := e
	changed.Env = map[string]string{}
	report, err := Sync(changed, Global)
	if err != nil || report.Synced != 1 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("created a new default Claude target")
	}
	for _, target := range plan.Targets {
		assertBundle(t, target.Path)
	}
}
func TestInvalidMetadataAndPathsNeverRemoveFiles(t *testing.T) {
	for _, raw := range []string{"{", "null", "{}", `{"schemaVersion":2,"installations":[]}`, `{"schemaVersion":1,"installations":null}`, `{"schemaVersion":1,"installations":[],"unknown":1}`} {
		t.Run(raw, func(t *testing.T) {
			e := testEnvironment(t)
			metadata, metadataError := metadataPath(e)
			if metadataError != nil {
				t.Fatal(metadataError)
			}
			writeFile(t, metadata, raw)
			victim := filepath.Join(e.Cwd, ".agents/skills/conduct/keep")
			writeFile(t, victim, "keep")
			if _, err := CreatePlan(Request{Agents: []string{"codex"}, Scope: Project}, e); err == nil {
				t.Fatal("invalid records accepted")
			}
			if _, err := os.Stat(victim); err != nil {
				t.Fatal("mutated before validation")
			}
		})
	}
	e := testEnvironment(t)
	plan := mustInstall(t, e, []string{"codex"}, Project, Copy)
	records, err := readRecords(plan.RecordPath, e)
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(e.Home, "unrelated/conduct")
	writeFile(t, filepath.Join(victim, "keep"), "safe")
	records.Installations[0].Targets[0].Path = "../home/unrelated/conduct"
	writeRecord(t, e, records)
	if _, err := Sync(e, ""); err == nil {
		t.Fatal("unrelated deletion accepted")
	}
	if _, err := os.Stat(filepath.Join(victim, "keep")); err != nil {
		t.Fatal("victim removed")
	}
}
func TestChangedPlanRejected(t *testing.T) {
	e := testEnvironment(t)
	plan := mustPlan(t, e, []string{"codex"}, Project, Copy)
	plan.Targets[0].Path = filepath.Join(e.Home, "victim/conduct")
	if _, err := Execute(plan, e); err == nil {
		t.Fatal("forged plan accepted")
	}
	plan = mustPlan(t, e, []string{"codex"}, Project, Copy)
	writeFile(t, filepath.Join(plan.Targets[0].Path, "local"), "changed")
	if _, err := Execute(plan, e); err == nil {
		t.Fatal("changed plan accepted")
	}
}
func TestConcurrentRecordsPreserveAllProjects(t *testing.T) {
	e := testEnvironment(t)
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	for index := 0; index < 8; index++ {
		project := filepath.Join(filepath.Dir(e.Cwd), string(rune('a'+index)))
		if err := os.Mkdir(project, 0755); err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func(project string) {
			defer workers.Done()
			environment := e
			environment.Cwd = project
			plan, err := CreatePlan(Request{Agents: []string{"codex"}, Scope: Project}, environment)
			if err == nil {
				_, err = Execute(plan, environment)
			}
			errors <- err
		}(project)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	metadata, metadataError := metadataPath(e)
	if metadataError != nil {
		t.Fatal(metadataError)
	}
	records, err := readRecords(metadata, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Installations) != 8 {
		t.Fatal(len(records.Installations))
	}
}
func TestSyncAllScopeMissingAndNoRecords(t *testing.T) {
	e := testEnvironment(t)
	report, err := Sync(e, "")
	if err != nil || report.Matched != 0 {
		t.Fatal(report, err)
	}
	if _, err = os.Stat(filepath.Join(e.Home, ".conduct")); !os.IsNotExist(err) {
		t.Fatal("empty sync wrote metadata")
	}
	mustInstall(t, e, []string{"codex"}, Project, Copy)
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other")
	if err = os.Mkdir(other.Cwd, 0755); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, other, []string{"claude-code"}, Project, Copy)
	mustInstall(t, e, []string{"roo"}, Global, Copy)
	e.Version = "v2.0.0"
	report, err = Sync(e, Project)
	if err != nil || report.Synced != 1 {
		t.Fatal(report, err)
	}
	metadata, metadataError := metadataPath(e)
	if metadataError != nil {
		t.Fatal(metadataError)
	}
	records, err := readRecords(metadata, e)
	if err != nil {
		t.Fatal(err)
	}
	versions := []string{}
	for _, record := range records.Installations {
		versions = append(versions, record.ConductVersion)
	}
	if !reflect.DeepEqual(versions, []string{"v2.0.0", "v1.2.3", "v1.2.3"}) {
		t.Fatal(versions)
	}
	report, err = Sync(e, Global)
	if err != nil || report.Synced != 1 {
		t.Fatal(report, err)
	}
	if err = os.RemoveAll(other.Cwd); err != nil {
		t.Fatal(err)
	}
	report, err = Sync(e, "")
	if err != nil || report.Synced != 2 || report.Skipped != 1 {
		t.Fatal(report, err)
	}
	if _, err = os.Stat(other.Cwd); !os.IsNotExist(err) {
		t.Fatal("missing project recreated")
	}
	nested := e
	nested.Cwd = filepath.Join(e.Cwd, "subdir")
	if err = os.Mkdir(nested.Cwd, 0755); err != nil {
		t.Fatal(err)
	}
	report, err = Sync(nested, Project)
	if err != nil || report.Matched != 0 {
		t.Fatal(report, err)
	}
}

func TestSourceAliasProtectedWithoutFollowingFinalLink(t *testing.T) {
	e := testEnvironment(t)
	source, err := sourceDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if source == "" {
		t.Skip("build has no absolute source location")
	}
	if err = os.MkdirAll(filepath.Join(e.Cwd, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Dir(source), filepath.Join(e.Cwd, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	if _, err = CreatePlan(Request{Agents: []string{"codex"}, Scope: Project}, e); err == nil {
		t.Fatal("source alias was accepted")
	}
	if err = os.Remove(filepath.Join(e.Cwd, ".agents/skills")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(e.Cwd, ".agents/skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(source, filepath.Join(e.Cwd, ".agents/skills/conduct")); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, e, []string{"codex"}, Project, Copy)
	assertBundle(t, source)
}
func TestSyncFailureKeepsOtherRecordsAndVersion(t *testing.T) {
	e := testEnvironment(t)
	mustInstall(t, e, []string{"codex"}, Project, Copy)
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other")
	if err := os.Mkdir(other.Cwd, 0755); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, other, []string{"codex"}, Project, Copy)
	if err := os.RemoveAll(e.Cwd); err != nil {
		t.Fatal(err)
	}
	writeFile(t, e.Cwd, "project became a file")
	e.Version = "v2"
	report, err := Sync(e, "")
	if err == nil || report.Failed != 1 || report.Synced != 1 {
		t.Fatal(report, err)
	}
	metadata, metadataError := metadataPath(e)
	if metadataError != nil {
		t.Fatal(metadataError)
	}
	records, err := readRecords(metadata, e)
	if err != nil {
		t.Fatal(err)
	}
	if records.Installations[0].ConductVersion != "v1.2.3" || records.Installations[1].ConductVersion != "v2" {
		t.Fatal(records)
	}
}
func TestChangedParentAliasCannotRedirectInstallation(t *testing.T) {
	e := testEnvironment(t)
	if err := os.MkdirAll(filepath.Join(e.Cwd, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(e.Home, "first")
	second := filepath.Join(e.Home, "second")
	if err := os.Mkdir(first, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(second, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(e.Cwd, ".agents/skills")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, e, []string{"codex"}, Project, Copy)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(plan, e); err == nil {
		t.Fatal("changed parent redirected writes")
	}
	if _, err := os.Stat(filepath.Join(second, "conduct")); !os.IsNotExist(err) {
		t.Fatal("wrote changed destination")
	}
}

func TestGlobalRelativeClaudeSyncIgnoresNewCwd(t *testing.T) {
	e := testEnvironment(t)
	e.Env["CLAUDE_CONFIG_DIR"] = "custom-claude"
	plan := mustInstall(t, e, []string{"codex", "claude-code"}, Global, Symlink)
	original := filepath.Join(e.Cwd, "custom-claude/skills/conduct")
	if _, err := os.Lstat(original); err != nil {
		t.Fatal(err)
	}
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "different-project")
	if err := os.Mkdir(other.Cwd, 0755); err != nil {
		t.Fatal(err)
	}
	other.Version = "v2"
	report, err := Sync(other, Global)
	if err != nil || report.Synced != 1 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(filepath.Join(other.Cwd, "custom-claude")); !os.IsNotExist(err) {
		t.Fatal("reinterpreted relative config against new cwd")
	}
	records, err := readRecords(plan.RecordPath, other)
	if err != nil {
		t.Fatal(err)
	}
	if records.Installations[0].ConductVersion != "v2" {
		t.Fatal(records)
	}
	for _, target := range records.Installations[0].Targets {
		if filepath.Join(other.Home, target.Path) == original {
			return
		}
	}
	t.Fatal("original physical Claude target was lost")
}

func TestGlobalClaudeParentAliasCanBeVerified(t *testing.T) {
	for _, name := range []string{"shared-target", "shared-target/skills"} {
		t.Run(name, func(t *testing.T) {
			e := testEnvironment(t)
			config := filepath.Join(e.Home, ".claude")
			physical := filepath.Join(e.Home, name)
			if err := os.Mkdir(config, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(physical, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(physical, filepath.Join(config, "skills")); err != nil {
				t.Fatal(err)
			}
			mustInstall(t, e, []string{"claude-code"}, Global, Copy)
			e.Version = "v2"
			report, err := Sync(e, Global)
			if err != nil || report.Synced != 1 {
				t.Fatal(report, err)
			}
			assertBundle(t, filepath.Join(physical, "conduct"))
		})
	}
}

func TestHistoricalExternalClaudeAliasSurvivesClearedEnvironment(t *testing.T) {
	for _, method := range []Method{Copy, Symlink} {
		t.Run(string(method), func(t *testing.T) {
			e := testEnvironment(t)
			config := filepath.Join(filepath.Dir(e.Home), "external-claude")
			shared := filepath.Join(filepath.Dir(e.Home), "shared-target")
			for _, directory := range []string{config, shared} {
				if err := os.Mkdir(directory, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(shared, filepath.Join(config, "skills")); err != nil {
				t.Fatal(err)
			}
			e.Env["CLAUDE_CONFIG_DIR"] = config
			plan := mustInstall(t, e, []string{"codex", "claude-code"}, Global, method)
			actual := filepath.Join(shared, "conduct")
			if method == Copy {
				writeFile(t, filepath.Join(actual, "obsolete"), "remove me")
				writeFile(t, filepath.Join(actual, "scripts/run_help.py"), "local edits")
			}
			changed := e
			changed.Env = map[string]string{}
			changed.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other-project")
			if err := os.Mkdir(changed.Cwd, 0755); err != nil {
				t.Fatal(err)
			}
			changed.Version = "v2"
			report, err := Sync(changed, Global)
			if err != nil || report.Synced != 1 {
				t.Fatal(report, err)
			}
			if _, err := os.Stat(filepath.Join(e.Home, ".claude")); !os.IsNotExist(err) {
				t.Fatal("created replacement default config")
			}
			if method == Copy {
				assertBundle(t, actual)
			} else {
				info, err := os.Lstat(actual)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("historical symlink was not preserved", err)
				}
			}
			records, err := readRecords(plan.RecordPath, changed)
			if err != nil {
				t.Fatal(err)
			}
			if records.Installations[0].ConductVersion != "v2" {
				t.Fatal(records)
			}
			found := false
			for _, target := range records.Installations[0].Targets {
				if filepath.Join(e.Home, target.Path) == actual {
					found = true
				}
			}
			if !found {
				t.Fatal("historical physical target was lost")
			}
		})
	}
}

func TestHistoricalAliasCannotAuthorizeUnrelatedDirectoryDeletion(t *testing.T) {
	for _, name := range []string{"victim/conduct", "victim/skills/conduct"} {
		t.Run(name, func(t *testing.T) {
			e := testEnvironment(t)
			plan := mustInstall(t, e, []string{"claude-code"}, Global, Copy)
			records, err := readRecords(plan.RecordPath, e)
			if err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(filepath.Dir(e.Home), name)
			writeFile(t, filepath.Join(victim, "keep"), "unrelated data")
			relative, err := filepath.Rel(e.Home, victim)
			if err != nil {
				t.Fatal(err)
			}
			records.Installations[0].Targets = []StoredTarget{{Path: relative, Kind: "directory"}}
			writeRecord(t, e, records)
			e.Version = "v2"
			if _, err := Sync(e, Global); err == nil {
				t.Fatal("unrelated directory accepted as historical Claude skill")
			}
			data, err := os.ReadFile(filepath.Join(victim, "keep"))
			if err != nil || string(data) != "unrelated data" {
				t.Fatal("unrelated data was removed", err)
			}
			records, err = readRecords(plan.RecordPath, e)
			if err != nil {
				t.Fatal(err)
			}
			if records.Installations[0].ConductVersion != "v1.2.3" {
				t.Fatal("failed record version advanced")
			}
		})
	}
}

func TestCrossRecordNestedAliasesRejectedBeforeWriting(t *testing.T) {
	e := testEnvironment(t)
	outer := mustInstall(t, e, []string{"codex"}, Project, Copy)
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other")
	alias := filepath.Join(other.Cwd, ".agents/skills")
	shared := filepath.Join(outer.Targets[0].Path, "nested")
	if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePlan(Request{Agents: []string{"codex"}, Scope: Project}, other); err == nil {
		t.Fatal("nested cross-project installation accepted")
	}
	// Simulate registrations written by the previous implementation. A scoped
	// update must check the unselected record before removing the outer target.
	nested, err := createPlan(Request{Agents: []string{"codex"}, Scope: Project}, other, outer.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyBundle(nested.Targets[0].Path); err != nil {
		t.Fatal(err)
	}
	records, err := readRecords(outer.RecordPath, e)
	if err != nil {
		t.Fatal(err)
	}
	record, err := storedInstallation(nested, other)
	if err != nil {
		t.Fatal(err)
	}
	records.Installations = append(records.Installations, record)
	writeRecord(t, e, records)
	marker := filepath.Join(nested.Targets[0].Path, "keep")
	writeFile(t, marker, "safe")
	if _, err := Sync(e, Project); err == nil {
		t.Fatal("nested registered targets accepted")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "safe" {
		t.Fatal("scoped sync removed another installation", err)
	}
}

func TestHistoricalPhysicalTargetsSurviveAliasChangesForAllAgents(t *testing.T) {
	for _, scope := range []Scope{Project, Global} {
		for _, method := range []Method{Copy, Symlink} {
			for _, definition := range registry(Environment{Home: "/unused", Env: map[string]string{}}) {
				t.Run(string(scope)+"/"+string(method)+"/"+definition.ID, func(t *testing.T) {
					e := testEnvironment(t)
					var alias string
					for _, agent := range registry(e) {
						if agent.ID == definition.ID {
							alias = filepath.Join(e.Cwd, agent.project)
							if scope == Global {
								alias = agent.global
							}
						}
					}
					shared := filepath.Join(filepath.Dir(e.Home), "shared")
					if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(shared, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(shared, alias); err != nil {
						t.Fatal(err)
					}
					agents := []string{definition.ID}
					if !definition.Universal {
						agents = append(agents, "codex")
					}
					plan := mustInstall(t, e, agents, scope, method)
					if err := os.Remove(alias); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(e.Home, "new-unused-target"), alias); err != nil {
						t.Fatal(err)
					}
					for _, target := range plan.Targets {
						if target.Kind == "directory" {
							writeFile(t, filepath.Join(target.Path, "obsolete"), "old")
						}
					}
					e.Version = "v2"
					report, err := Sync(e, scope)
					if err != nil || report.Synced != 1 {
						t.Fatal(report, err)
					}
					for _, target := range plan.Targets {
						resolved, err := filepath.EvalSymlinks(target.Path)
						if err != nil {
							t.Fatal(err)
						}
						assertBundle(t, resolved)
					}
					if _, err := os.Stat(filepath.Join(e.Home, "new-unused-target")); !os.IsNotExist(err) {
						t.Fatal("followed changed logical alias", err)
					}
				})
			}
		}
	}
}

func TestRuntimePathFailureDoesNotBlockUnrelatedRecords(t *testing.T) {
	e := testEnvironment(t)
	bad := mustInstall(t, e, []string{"codex"}, Project, Copy)
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other")
	if err := os.Mkdir(other.Cwd, 0755); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, other, []string{"codex"}, Project, Copy)
	parent := filepath.Dir(bad.Targets[0].Path)
	if err := os.Rename(parent, parent+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.Home, "missing"), parent); err != nil {
		t.Fatal(err)
	}
	// Explicit reinstallation and unrelated scope do not validate this runtime path.
	mustInstall(t, other, []string{"codex"}, Project, Copy)
	other.Version = "v2"
	report, err := Sync(other, Project)
	if err != nil || report.Synced != 1 {
		t.Fatal(report, err)
	}
	report, err = Sync(other, "")
	if err == nil || report.Failed != 1 || report.Synced != 1 || !strings.Contains(err.Error(), e.Cwd) {
		t.Fatal(report, err)
	}
	records, err := readRecords(bad.RecordPath, e)
	if err != nil {
		t.Fatal(err)
	}
	if records.Installations[0].ConductVersion != "v1.2.3" || records.Installations[1].ConductVersion != "v2" {
		t.Fatal(records)
	}
}

func TestAgentDetectionTreatsNonDirectoryAsMissing(t *testing.T) {
	e := testEnvironment(t)
	file := filepath.Join(e.Home, "config-file")
	writeFile(t, file, "not a directory")
	e.Env["XDG_CONFIG_HOME"] = file
	e.Env["CODEX_HOME"] = filepath.Join(file, "codex")
	agents, err := ListAgents(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		if agent.ID == "opencode" && agent.Detected {
			t.Fatal("detected a path below a file")
		}
	}
}

func TestCrossProjectSharedPhysicalTargetIsAllowed(t *testing.T) {
	e := testEnvironment(t)
	first := mustInstall(t, e, []string{"codex"}, Project, Copy)
	other := e
	other.Cwd = filepath.Join(filepath.Dir(e.Cwd), "other")
	alias := filepath.Join(other.Cwd, ".agents/skills")
	if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(first.Targets[0].Path), alias); err != nil {
		t.Fatal(err)
	}
	second := mustInstall(t, other, []string{"codex"}, Project, Copy)
	if first.Targets[0].Path != second.Targets[0].Path {
		t.Fatal("shared target was not canonicalized")
	}
	e.Version = "v2"
	report, err := Sync(e, "")
	if err != nil || report.Synced != 2 {
		t.Fatal(report, err)
	}
}
