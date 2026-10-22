package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
)

func buildSkillFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	source := filepath.Join(root, "internal/skillinstall/assets/conduct/SKILL.md")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(fixture, append(content, []byte("\nNEW_BINARY_ASSET_FIXTURE\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{source: fixture}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "conduct-new")
	build := exec.Command("go", "build", "-overlay", overlayPath, "-ldflags", "-X github.com/qoggy/conduct/internal/cli.version=v9.9.9", "-o", binary, "./cmd/conduct")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated new binary: %v\n%s", err, output)
	}
	return binary
}

func binarySkillEnvironment(home string) []string {
	result := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "XDG_CONFIG_HOME", "LC_ALL", "LC_MESSAGES", "LANG", skillSyncTokenEnvironment:
			continue
		}
		result = append(result, entry)
	}
	return append(result, "HOME="+home, "LC_ALL=en_US.UTF-8", "TERM=xterm-256color")
}

func TestSkillRealBinaryOfflinePTYAndNewBinaryHandoff(t *testing.T) {
	// Build before changing HOME so Go can reuse its module cache. Every executed
	// product command below gets an isolated HOME and a directory outside source.
	binary := buildSkillFixture(t)
	home, project := isolatedSkillCLI(t)
	environment := binarySkillEnvironment(home)
	runBinary := func(arguments ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, arguments...)
		command.Dir = project
		command.Env = environment
		output, err := command.CombinedOutput()
		return string(output), err
	}
	t.Run("offline complete bundle", func(t *testing.T) {
		output, err := runBinary("init", "--yes", "--json", "--agent", "codex")
		if err != nil {
			t.Fatalf("%v: %s", err, output)
		}
		var result skillinstall.Result
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"SKILL.md", "LICENSE", "scripts/workflow_help.py", "scripts/run_help.py"} {
			if _, err := os.Stat(filepath.Join(project, ".agents/skills/conduct", file)); err != nil {
				t.Fatal(err)
			}
		}
		content, err := os.ReadFile(filepath.Join(project, ".agents/skills/conduct/SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(content, []byte("NEW_BINARY_ASSET_FIXTURE")) {
			t.Fatal("overlay resource was not embedded in the binary")
		}
	})
	t.Run("private entry refuses direct call", func(t *testing.T) {
		output, err := runBinary(skillSyncCommand)
		if err == nil || !strings.Contains(output, "requires an update handoff") {
			t.Fatalf("%v: %s", err, output)
		}
	})
	t.Run("PTY cancellation writes nothing", func(t *testing.T) {
		cancelHome := t.TempDir()
		cancelProject := t.TempDir()
		output, err := driveSkillPTY(binary, cancelHome, cancelProject, []string{"init"}, []ptyAction{{"Select AI coding tools", []byte{3}}})
		if err != nil {
			t.Fatalf("PTY: %v\n%s", err, output)
		}
		if !strings.Contains(output, "Installation cancelled.") {
			t.Fatalf("missing cancel output: %s", output)
		}
		for _, path := range []string{filepath.Join(cancelHome, ".conduct"), filepath.Join(cancelProject, ".agents")} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cancel wrote %s: %v", path, err)
			}
		}
	})
	for _, language := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		for _, columns := range []uint16{80, 140} {
			t.Run(fmt.Sprintf("PTY all tool options visible/%s/columns=%d", language, columns), func(t *testing.T) {
				cancelHome, cancelProject := t.TempDir(), t.TempDir()
				output, err := driveSkillPTYWithSize(binary, cancelHome, cancelProject, []string{"init"}, []ptyAction{{"Roo Code", []byte{3}}}, &pty.Winsize{Rows: 20, Cols: columns}, "LC_ALL="+language)
				if err != nil {
					t.Fatalf("all options must be visible before navigation: %v\n%s", err, output)
				}
				for _, name := range []string{"Claude Code", "Windsurf", "Qwen Code", "Kiro CLI", "Roo Code"} {
					if !strings.Contains(output, name) {
						t.Fatalf("missing tool %s: %s", name, output)
					}
				}
				if !strings.Contains(output, "◻") || !strings.Contains(output, "◆") {
					t.Fatalf("missing checkbox or active-step indicator: %s", output)
				}
			})
		}
	}
	for _, language := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		t.Run("PTY tool list grows after resize/"+language, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "init")
			command.Dir = t.TempDir()
			command.Env = append(binarySkillEnvironment(t.TempDir()), "LC_ALL="+language)
			terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 6, Cols: 140})
			if err != nil {
				t.Fatal(err)
			}
			var output lockedPTYOutput
			copied := make(chan error, 1)
			go func() { _, err := io.Copy(&output, terminal); copied <- err }()
			defer func() {
				if t.Failed() {
					cancel()
				}
				waitError := command.Wait()
				closeError := terminal.Close()
				copyError := <-copied
				if closeError != nil {
					t.Error(closeError)
				}
				if copyError != nil && !errors.Is(copyError, os.ErrClosed) && !errors.Is(copyError, io.EOF) && !strings.Contains(copyError.Error(), "input/output error") {
					t.Error(copyError)
				}
				if !t.Failed() && waitError != nil {
					t.Error(waitError)
				}
			}()
			waitFor := func(value string, offset int) {
				t.Helper()
				for !strings.Contains(output.String()[offset:], value) {
					select {
					case <-ctx.Done():
						t.Fatalf("waiting for %q after resize: %v\n%s", value, ctx.Err(), output.String())
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			waitFor("Kiro CLI", 0)
			offset := len(output.String())
			if strings.Contains(output.String(), "Roo Code") {
				t.Fatal("short window unexpectedly displayed every option")
			}
			if err := pty.Setsize(terminal, &pty.Winsize{Rows: 20, Cols: 140}); err != nil {
				t.Fatal(err)
			}
			waitFor("Roo Code", offset)
			if _, err := terminal.Write([]byte{3}); err != nil {
				t.Fatal(err)
			}
			cancelled := "Installation cancelled."
			if language == "zh_CN.UTF-8" {
				cancelled = "安装已取消。"
			}
			waitFor(cancelled, offset)
		})
	}
	for _, language := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		t.Run("PTY step history and summary/"+language, func(t *testing.T) {
			home, project := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(home, ".claude"), 0755); err != nil {
				t.Fatal(err)
			}
			scopeTitle, methodTitle, confirmTitle := "Installation scope", "Installation method", "Install the conduct Skill"
			scopeValue, methodValue := "Current user", "Independent copies"
			toolsTitle, defaultTitle, summaryTitle, cancelled := "Select AI coding tools", "Included by default", "Installation Summary", "Installation cancelled."
			if language == "zh_CN.UTF-8" {
				scopeTitle, methodTitle, confirmTitle = "安装范围", "安装方式", "安装 conduct Skill"
				scopeValue, methodValue = "当前用户", "独立副本"
				toolsTitle, defaultTitle, summaryTitle, cancelled = "选择 AI 编程工具", "默认包含", "安装汇总", "安装已取消。"
			}
			output, err := driveSkillPTY(binary, home, project, []string{"init"}, []ptyAction{{"Roo Code", []byte("\r")}, {scopeTitle, []byte("j\r")}, {methodTitle, []byte("j\r")}, {confirmTitle, []byte("n\r")}}, "LC_ALL="+language)
			if err != nil {
				t.Fatalf("interactive flow: %v\n%s", err, output)
			}
			for _, expected := range []string{"┌  conduct Skill", "◼", "◻", "◇  " + defaultTitle, "◇  " + toolsTitle + "\r\n│  Claude Code", "◇  " + scopeTitle + "\r\n│  " + scopeValue, "◇  " + methodTitle + "\r\n│  " + methodValue, "◇  " + summaryTitle, "╭", "╯", "└  " + cancelled} {
				if !strings.Contains(output, expected) {
					t.Fatalf("missing transcript %q: %s", expected, output)
				}
			}
			for _, path := range []string{filepath.Join(home, ".conduct"), filepath.Join(home, ".agents"), filepath.Join(project, ".agents")} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("cancel wrote %s: %v", path, err)
				}
			}
		})
	}
	t.Run("PTY short window can select last tool", func(t *testing.T) {
		interactiveHome, interactiveProject := t.TempDir(), t.TempDir()
		output, err := driveSkillPTYWithSize(binary, interactiveHome, interactiveProject, []string{"init"}, []ptyAction{{"Claude Code", []byte("jjjj \r")}, {"Installation scope", []byte("\r")}, {"Installation method", []byte("\r")}, {"Install the conduct Skill", []byte("\r")}}, &pty.Winsize{Rows: 6, Cols: 140})
		if err != nil {
			t.Fatalf("short window navigation: %v\n%s", err, output)
		}
		data, err := os.ReadFile(filepath.Join(interactiveHome, ".conduct/skill-installations.json"))
		if err != nil {
			t.Fatal(err)
		}
		var records skillinstall.Records
		if err := json.Unmarshal(data, &records); err != nil {
			t.Fatal(err)
		}
		if len(records.Installations) != 1 || !slices.Contains(records.Installations[0].Agents, "roo") {
			t.Fatalf("last tool was not selected: %s\n%s", data, output)
		}
	})
	t.Run("PTY full default flow", func(t *testing.T) {
		interactiveHome := t.TempDir()
		interactiveProject := t.TempDir()
		output, err := driveSkillPTY(binary, interactiveHome, interactiveProject, []string{"init"}, []ptyAction{{"Select AI coding tools", []byte("\r")}, {"Installation scope", []byte("\r")}, {"Install the conduct Skill", []byte("\r")}})
		if err != nil {
			t.Fatalf("PTY: %v\n%s", err, output)
		}
		if !strings.Contains(output, "Skill installation complete.") {
			t.Fatalf("missing success output: %s", output)
		}
		if !strings.Contains(output, "└  Skill installation complete.") || !strings.Contains(output, "◇  Installation scope") {
			t.Fatalf("missing completion or step history: %s", output)
		}
		if _, err := os.Stat(filepath.Join(interactiveProject, ".agents/skills/conduct/SKILL.md")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("PTY global symlink method and final confirmation", func(t *testing.T) {
		interactiveHome := t.TempDir()
		interactiveProject := t.TempDir()
		output, err := driveSkillPTY(binary, interactiveHome, interactiveProject, []string{"init", "--agent", "codex,claude-code"}, []ptyAction{{"Installation scope", []byte("j\r")}, {"Installation method", []byte("\r")}, {"Install the conduct Skill", []byte("\r")}})
		if err != nil {
			t.Fatalf("PTY: %v\n%s", err, output)
		}
		target := filepath.Join(interactiveHome, ".claude/skills/conduct")
		info, err := os.Lstat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected global symlink: %s\n%s", target, output)
		}
		if _, err := os.Lstat(filepath.Join(interactiveProject, ".agents")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("global wrote project: %v", err)
		}
	})
	t.Run("PTY Chinese final cancellation", func(t *testing.T) {
		cancelHome := t.TempDir()
		cancelProject := t.TempDir()
		output, err := driveSkillPTY(binary, cancelHome, cancelProject, []string{"init", "--agent", "codex", "--project"}, []ptyAction{{"安装 conduct Skill", []byte("n\r")}}, "LC_ALL=zh_CN.UTF-8")
		if err != nil {
			t.Fatalf("PTY: %v\n%s", err, output)
		}
		if !strings.Contains(output, "安装已取消。") {
			t.Fatalf("missing Chinese cancel output: %s", output)
		}
		for _, path := range []string{filepath.Join(cancelHome, ".conduct"), filepath.Join(cancelProject, ".agents")} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cancel wrote %s: %v", path, err)
			}
		}
	})
	for _, language := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		for _, scope := range []string{"project", "global"} {
			for _, fault := range []string{"broken", "cyclic"} {
				for _, choice := range []string{"copy", "symlink", "cancel"} {
					t.Run("PTY choose method before central validation/"+language+"/"+scope+"/"+fault+"/"+choice, func(t *testing.T) {
						home, project := t.TempDir(), t.TempDir()
						root := project
						if scope == "global" {
							root = home
						}
						alias := filepath.Join(root, ".agents/skills")
						if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
							t.Fatal(err)
						}
						link := "missing"
						if fault == "cyclic" {
							link = "skills"
						}
						if err := os.Symlink(link, alias); err != nil {
							t.Fatal(err)
						}
						methodTitle, confirmTitle := "Installation method", "Install the conduct Skill"
						if language == "zh_CN.UTF-8" {
							methodTitle, confirmTitle = "安装方式", "安装 conduct Skill"
						}
						actions := []ptyAction{{methodTitle, []byte("\r")}}
						if choice == "copy" {
							actions = []ptyAction{{methodTitle, []byte("j\r")}, {confirmTitle, []byte("\r")}}
						} else if choice == "cancel" {
							actions = []ptyAction{{methodTitle, []byte{3}}}
						}
						output, err := driveSkillPTY(binary, home, project, []string{"init", "--agent", "claude-code,qwen-code", "--" + scope}, actions, "LC_ALL="+language)
						if choice == "symlink" {
							if err == nil || !strings.Contains(output, ".agents/skills") || strings.Contains(output, confirmTitle) {
								t.Fatalf("required central path must fail before confirmation: %v\n%s", err, output)
							}
							var exitError *exec.ExitError
							if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
								t.Fatalf("expected installation error exit 1: %v", err)
							}
						} else if err != nil {
							t.Fatalf("method choice: %v\n%s", err, output)
						}
						if actual, err := os.Readlink(alias); err != nil || actual != link {
							t.Fatal("unused central alias changed", actual, err)
						}
						for _, directory := range []string{".claude", ".qwen"} {
							target := filepath.Join(root, directory, "skills/conduct")
							if choice == "copy" {
								for _, resource := range []string{"SKILL.md", "LICENSE", "scripts/workflow_help.py", "scripts/run_help.py"} {
									if info, err := os.Lstat(filepath.Join(target, resource)); err != nil || !info.Mode().IsRegular() {
										t.Fatalf("missing copied resource %s: %v", resource, err)
									}
								}
							} else if _, err := os.Lstat(filepath.Join(root, directory)); !errors.Is(err, os.ErrNotExist) {
								t.Fatal("cancelled or invalid plan wrote agent directory", err)
							}
						}
						records, err := os.ReadFile(filepath.Join(home, ".conduct/skill-installations.json"))
						if choice == "copy" {
							var saved skillinstall.Records
							if err != nil {
								t.Fatal(err)
							}
							if err := json.Unmarshal(records, &saved); err != nil {
								t.Fatal(err)
							}
							if len(saved.Installations) != 1 || saved.Installations[0].Method != skillinstall.Copy || len(saved.Installations[0].Targets) != 2 {
								t.Fatalf("unexpected copy registration: %s", records)
							}
						} else if !errors.Is(err, os.ErrNotExist) {
							t.Fatal("cancelled or invalid plan wrote records", err)
						}
					})
				}
			}
		}
	}
	for _, language := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		for _, cancelKey := range []byte{3, 4} {
			t.Run(fmt.Sprintf("TERM=dumb/%s/cancel=%d", language, cancelKey), func(t *testing.T) {
				cancelHome, cancelProject := t.TempDir(), t.TempDir()
				marker := filepath.Join(cancelProject, ".agents/skills/conduct/local")
				if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				title, cancelled := "Install the conduct Skill", "Installation cancelled."
				if language == "zh_CN.UTF-8" {
					title, cancelled = "安装 conduct Skill", "安装已取消。"
				}
				output, err := driveSkillPTY(binary, cancelHome, cancelProject, []string{"init", "--agent", "codex", "--project"}, []ptyAction{{title, []byte{cancelKey}}}, "LC_ALL="+language, "TERM=dumb")
				if err != nil || !strings.Contains(output, cancelled) {
					t.Fatalf("PTY cancellation: %v\n%s", err, output)
				}
				if language == "zh_CN.UTF-8" && strings.Contains(output, "(y/N)") {
					t.Fatal("English accessible prompt leaked", output)
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "untouched" {
					t.Fatal("cancel overwrote existing target", err)
				}
				if _, err := os.Lstat(filepath.Join(cancelHome, ".conduct")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("cancel wrote metadata", err)
				}
			})
		}
	}
	t.Run("installed executable owns new resources and scope", func(t *testing.T) {
		previousVersion := version
		version = "v1.0.0"
		t.Cleanup(func() { version = previousVersion })
		// Register a second project with the old process; a project-scoped handoff
		// must leave it at the old version while replacing only the current one.
		second := t.TempDir()
		oldEnvironment, err := skillinstall.NewEnvironment(version)
		if err != nil {
			t.Fatal(err)
		}
		oldEnvironment.Cwd = second
		plan, err := skillinstall.CreatePlan(skillinstall.Request{Scope: skillinstall.Project, Agents: []string{"codex"}}, oldEnvironment)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := skillinstall.Execute(plan, oldEnvironment); err != nil {
			t.Fatal(err)
		}
		oldEnvironment.Cwd = project
		plan, err = skillinstall.CreatePlan(skillinstall.Request{Scope: skillinstall.Project, Agents: []string{"codex"}}, oldEnvironment)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := skillinstall.Execute(plan, oldEnvironment); err != nil {
			t.Fatal(err)
		}
		installed := filepath.Join(t.TempDir(), "conduct")
		if err := os.WriteFile(installed, []byte("old executable fixture"), 0o700); err != nil {
			t.Fatal(err)
		}
		fixtureBytes, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		command := &cobra.Command{}
		command.SetContext(context.Background())
		command.SetOut(&output)
		command.SetErr(&output)
		dependencies := defaultUpdateDependencies()
		dependencies.executable = func() (string, error) { return installed, nil }
		queries := 0
		dependencies.query = func(context.Context, string, bool) (*updateCandidate, error) {
			queries++
			return &updateCandidate{Version: "9.9.9", Install: func(context.Context, string) error {
				replacement := installed + ".new"
				if err := os.WriteFile(replacement, fixtureBytes, 0o700); err != nil {
					return err
				}
				return os.Rename(replacement, installed)
			}}, nil
		}
		if err := runUpdateWithDependencies(command, "", false, false, skillinstall.Project, dependencies); err != nil {
			t.Fatalf("handoff: %v\n%s", err, output.String())
		}
		if queries != 1 {
			t.Fatalf("update queried %d times", queries)
		}
		current, err := os.ReadFile(filepath.Join(project, ".agents/skills/conduct/SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		other, err := os.ReadFile(filepath.Join(second, ".agents/skills/conduct/SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(current, []byte("NEW_BINARY_ASSET_FIXTURE")) || bytes.Contains(other, []byte("NEW_BINARY_ASSET_FIXTURE")) {
			t.Fatal("new binary resources or project scope were not preserved")
		}
		recordsData, err := os.ReadFile(filepath.Join(home, ".conduct/skill-installations.json"))
		if err != nil {
			t.Fatal(err)
		}
		var records skillinstall.Records
		if err := json.Unmarshal(recordsData, &records); err != nil {
			t.Fatal(err)
		}
		for _, record := range records.Installations {
			expected := "v1.0.0"
			if record.ProjectPath == plan.ProjectPath {
				expected = "v9.9.9"
			}
			if record.ConductVersion != expected {
				t.Fatalf("record %s = %s; expected %s", record.ProjectPath, record.ConductVersion, expected)
			}
		}
	})
	t.Run("wrong new binary version cannot claim success", func(t *testing.T) {
		command := &cobra.Command{}
		command.SetContext(context.Background())
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		err := handoffSkillSync(command, binary, "8.0.0", skillinstall.Global)
		if err == nil {
			t.Fatal("wrong-version binary accepted handoff")
		}
	})
}

type ptyAction struct {
	waitFor string
	send    []byte
}
type lockedPTYOutput struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (output *lockedPTYOutput) Write(data []byte) (int, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	return output.buffer.Write(data)
}
func (output *lockedPTYOutput) String() string {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	return output.buffer.String()
}

func driveSkillPTY(binary, home, project string, arguments []string, actions []ptyAction, environmentOverrides ...string) (string, error) {
	return driveSkillPTYWithSize(binary, home, project, arguments, actions, &pty.Winsize{Rows: 40, Cols: 140}, environmentOverrides...)
}

func driveSkillPTYWithSize(binary, home, project string, arguments []string, actions []ptyAction, size *pty.Winsize, environmentOverrides ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = project
	command.Env = binarySkillEnvironment(home)
	command.Env = append(command.Env, environmentOverrides...)
	terminal, err := pty.StartWithSize(command, size)
	if err != nil {
		return "", err
	}
	var output lockedPTYOutput
	copied := make(chan error, 1)
	go func() { _, err := io.Copy(&output, terminal); copied <- err }()
	for _, action := range actions {
		for !strings.Contains(output.String(), action.waitFor) {
			select {
			case <-ctx.Done():
				closeError := terminal.Close()
				waitError := command.Wait()
				copyError := <-copied
				return output.String(), errors.Join(fmt.Errorf("waiting for %q: %w", action.waitFor, ctx.Err()), closeError, waitError, copyError)
			case <-time.After(20 * time.Millisecond):
			}
		}
		if _, err := terminal.Write(action.send); err != nil {
			closeError := terminal.Close()
			waitError := command.Wait()
			copyError := <-copied
			return output.String(), errors.Join(err, closeError, waitError, copyError)
		}
	}
	waitError := command.Wait()
	closeError := terminal.Close()
	copyError := <-copied
	// PTY EOF is EIO on Linux; closing the reader after process completion may
	// also return os.ErrClosed. Neither is a product command failure.
	if copyError != nil && !errors.Is(copyError, os.ErrClosed) && !errors.Is(copyError, io.EOF) && !strings.Contains(copyError.Error(), "input/output error") {
		return output.String(), errors.Join(waitError, closeError, copyError)
	}
	return output.String(), errors.Join(waitError, closeError)
}
