package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
)

const skillSyncTokenEnvironment = "CONDUCT_INTERNAL_SKILL_SYNC"
const skillSyncCommand = "__skill-sync"

type skillSyncRequest struct {
	Token   string             `json:"token"`
	Version string             `json:"version"`
	Cwd     string             `json:"cwd"`
	Scope   skillinstall.Scope `json:"scope"`
}

// The private entry point accepts a bounded, version-checked stdin protocol. It
// never checks releases or calls update, and is intentionally absent from help.
func newSkillSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use: skillSyncCommand, Hidden: true, Args: noArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			token := os.Getenv(skillSyncTokenEnvironment)
			if token == "" {
				return fmt.Errorf("internal skill sync requires an update handoff")
			}
			if err := os.Unsetenv(skillSyncTokenEnvironment); err != nil {
				return fmt.Errorf("clear skill sync handoff: %w", err)
			}
			decoder := json.NewDecoder(io.LimitReader(command.InOrStdin(), 16385))
			decoder.DisallowUnknownFields()
			var request skillSyncRequest
			if err := decoder.Decode(&request); err != nil {
				return fmt.Errorf("decode skill sync handoff: %w", err)
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				return fmt.Errorf("invalid trailing skill sync handoff data: %v", err)
			}
			if request.Token != token || len(token) != 64 || request.Cwd == "" || strings.TrimPrefix(request.Version, "v") != strings.TrimPrefix(version, "v") {
				return fmt.Errorf("skill sync handoff token, version or cwd mismatch")
			}
			if request.Scope != "" && request.Scope != skillinstall.Project && request.Scope != skillinstall.Global {
				return fmt.Errorf("invalid skill sync handoff scope")
			}
			environment, err := skillinstall.NewEnvironment(version)
			if err != nil {
				return err
			}
			if environment.Cwd != request.Cwd {
				return fmt.Errorf("skill sync handoff physical cwd mismatch")
			}
			return syncRegisteredSkills(command, request.Scope)
		},
	}
}

func handoffSkillSync(command *cobra.Command, executable, newVersion string, scope skillinstall.Scope) error {
	environment, err := skillinstall.NewEnvironment(version)
	if err != nil {
		return fmt.Errorf("CLI updated; prepare skill sync: %w", err)
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("CLI updated; create skill sync token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	payload, err := json.Marshal(skillSyncRequest{Token: token, Version: newVersion, Cwd: environment.Cwd, Scope: scope})
	if err != nil {
		return fmt.Errorf("CLI updated; encode skill sync handoff: %w", err)
	}
	child := exec.CommandContext(command.Context(), executable, skillSyncCommand)
	child.Dir = environment.Cwd
	child.Env = append(withoutSkillSyncToken(os.Environ()), skillSyncTokenEnvironment+"="+token)
	child.Stdin = bytes.NewReader(payload)
	child.Stdout = command.OutOrStdout()
	child.Stderr = command.ErrOrStderr()
	if err := child.Run(); err != nil {
		return fmt.Errorf("CLI updated, but new-binary Skill synchronization failed: %w", err)
	}
	return nil
}

func withoutSkillSyncToken(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, skillSyncTokenEnvironment+"=") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func syncRegisteredSkills(command *cobra.Command, scope skillinstall.Scope) error {
	environment, err := skillinstall.NewEnvironment(version)
	if err != nil {
		return err
	}
	report, syncError := skillinstall.Sync(environment, scope)
	for _, item := range report.Items {
		name := item.ProjectPath
		if name == "" {
			name = skillScopeLabel(item.Scope)
		}
		var message string
		switch item.Status {
		case "synced":
			message = localizedHelpText("Skill 已更新：%s（%s）\n", "Skill updated: %s (%s)\n")
		case "skipped":
			message = localizedHelpText("项目目录不存在，跳过 Skill 更新：%s\n", "Project directory is missing; skipped Skill update: %s\n")
		default:
			message = localizedHelpText("Skill 更新失败：%s\n", "Skill update failed: %s\n")
		}
		arguments := []any{name}
		if item.Status == "synced" {
			arguments = append(arguments, item.ConductVersion)
		}
		if _, err := fmt.Fprintf(command.OutOrStdout(), message, arguments...); err != nil {
			return errors.Join(syncError, err)
		}
	}
	if report.Matched == 0 && syncError == nil {
		_, err := fmt.Fprintln(command.OutOrStdout(), localizedHelpText("未找到通过 `conduct init` 安装的 Skill，已跳过。", "No Skills installed with `conduct init` were found; skipped."))
		return err
	}
	return syncError
}
