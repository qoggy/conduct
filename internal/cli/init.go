package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newInitCommand() *cobra.Command {
	var agents []string
	var project, global, copyFiles, yes, jsonOutput bool
	command := &cobra.Command{
		Use:   "init",
		Short: localizedHelpText("为 AI 编程工具安装 conduct Skill", "Install the conduct Skill for AI coding tools"),
		Long: localizedHelpText(`安装与本机 conduct 同版本的 Skill，支持离线安装。
默认安装到当前目录，也可选择当前用户。
默认包含 Codex、Cursor、OpenCode、Gemini CLI、GitHub Copilot 和 Cline，
并预选其他已检测工具；--agent 只安装指定工具。
多个安装位置默认使用符号链接，可用 --copy 改为独立副本。
已有 Skill 将被覆盖，本地修改和过期文件会被清除。
非终端或 --json 必须提供 --yes。

Agent ID：{{agentIDs}}
示例：conduct init --agent codex,claude-code --project --yes --json`, `Install the Skill matching this conduct version, without network access.
Install in the current directory by default, or choose a user-level installation.
Codex, Cursor, OpenCode, Gemini CLI, GitHub Copilot and Cline are included by default;
other detected tools are preselected. --agent installs only the specified tools.
Multiple installation locations use symlinks by default; use --copy for independent copies.
Existing Skills will be overwritten, removing local edits and obsolete files.
Non-terminal input or --json requires --yes.

Agent IDs: {{agentIDs}}
Example: conduct init --agent codex,claude-code --project --yes --json`),
		Args: noArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			if project && global {
				return scopeUsageError()
			}
			explicit := command.Flags().Changed("agent")
			environment, err := skillinstall.NewEnvironment(version)
			if err != nil {
				return err
			}
			available, err := skillinstall.ListAgents(environment)
			if err != nil {
				return err
			}
			if explicit {
				for _, id := range agents {
					known := false
					for _, agent := range available {
						if id == agent.ID {
							known = true
							break
						}
					}
					if !known {
						return localizedUsageErrorf("未知 Agent %q；请参阅 `conduct init --help`", "unknown agent %q; see `conduct init --help`", id)
					}
				}
				if len(agents) == 0 {
					return localizedUsageErrorf("--agent 至少需要一个 Agent ID", "--agent requires at least one agent ID")
				}
			}
			if !yes && (jsonOutput || !terminalStreams(command)) {
				return localizedUsageErrorf("非终端或 --json 模式必须提供 --yes", "non-terminal input or --json requires --yes")
			}
			if !yes {
				if err := writeSkillIntroduction(command); err != nil {
					return err
				}
			}
			cancelled := func() error {
				return writeSkillConclusion(command, localizedHelpText("安装已取消。", "Installation cancelled."))
			}
			if !explicit {
				agents = nil
				var universalNames []string
				var selected []string
				var options []huh.Option[string]
				for _, agent := range available {
					if agent.Universal {
						agents = append(agents, agent.ID)
						universalNames = append(universalNames, agent.DisplayName)
						continue
					}
					name := agent.DisplayName
					if agent.Detected {
						name += localizedHelpText("（已检测）", " (detected)")
						selected = append(selected, agent.ID)
					}
					options = append(options, huh.NewOption(name, agent.ID))
				}
				if !yes {
					if err := writeSkillNote(command, localizedHelpText("默认包含", "Included by default"), strings.Join(universalNames, ", ")); err != nil {
						return err
					}
					title := localizedHelpText("选择 AI 编程工具", "Select AI coding tools")
					description := localizedHelpText("↑/↓ 移动，空格选择，Enter 确认，Ctrl+C 取消。", "↑/↓ move, space selects, Enter confirms, Ctrl+C cancels.")
					selection := huh.NewMultiSelect[string]().Options(options...).Value(&selected).Filterable(false)
					if err := runSkillPrompt(command, title, description, selection); err != nil {
						if errors.Is(err, huh.ErrUserAborted) {
							return cancelled()
						}
						return err
					}
					value := skillToolNames(selected, available)
					if value == "" {
						value = localizedHelpText("仅默认工具", "Default tools only")
					}
					if err := writeSkillStep(command, title, value); err != nil {
						return err
					}
				}
				agents = append(agents, selected...)
			}
			scope := skillinstall.Project
			if global {
				scope = skillinstall.Global
			}
			if !yes && !project && !global {
				scopeValue := string(scope)
				title := localizedHelpText("安装范围", "Installation scope")
				err := runSkillPrompt(command, title, "", huh.NewSelect[string]().Options(huh.NewOption(skillScopeLabel(skillinstall.Project)+localizedHelpText("（默认）", " (default)"), "project"), huh.NewOption(skillScopeLabel(skillinstall.Global), "global")).Value(&scopeValue))
				if err != nil {
					if errors.Is(err, huh.ErrUserAborted) {
						return cancelled()
					}
					return err
				}
				scope = skillinstall.Scope(scopeValue)
				if err := writeSkillStep(command, title, skillScopeLabel(scope)); err != nil {
					return err
				}
			}
			method := skillinstall.Method("")
			if copyFiles {
				method = skillinstall.Copy
			}
			request := skillinstall.Request{Agents: agents, Scope: scope, Method: method}
			if !yes && !copyFiles {
				method, err = skillinstall.ResolveMethod(request, environment)
				if err != nil {
					return err
				}
			}
			if !yes && !copyFiles && method == skillinstall.Symlink {
				methodValue := string(method)
				title := localizedHelpText("安装方式", "Installation method")
				err := runSkillPrompt(command, title, "", huh.NewSelect[string]().Options(huh.NewOption(skillMethodLabel(skillinstall.Symlink)+localizedHelpText("（推荐）", " (recommended)"), "symlink"), huh.NewOption(skillMethodLabel(skillinstall.Copy)+localizedHelpText("（每个目标完整复制）", " (complete copy per target)"), "copy")).Value(&methodValue))
				if err != nil {
					if errors.Is(err, huh.ErrUserAborted) {
						return cancelled()
					}
					return err
				}
				request.Method = skillinstall.Method(methodValue)
				if err := writeSkillStep(command, title, skillMethodLabel(request.Method)); err != nil {
					return err
				}
			}
			plan, err := skillinstall.CreatePlan(request, environment)
			if err != nil {
				return err
			}
			if !yes {
				if err := writeInteractiveSkillPlan(command, plan); err != nil {
					return err
				}
			} else if !jsonOutput {
				if err := writeSkillPlan(command.OutOrStdout(), plan); err != nil {
					return err
				}
			}
			if !yes {
				confirmed := true
				title := localizedHelpText("安装 conduct Skill？", "Install the conduct Skill?")
				err := runSkillPrompt(command, title, "", huh.NewConfirm().Affirmative(localizedHelpText("安装", "Install")).Negative(localizedHelpText("取消", "Cancel")).Value(&confirmed))
				if err != nil {
					if errors.Is(err, huh.ErrUserAborted) {
						return cancelled()
					}
					return err
				}
				if !confirmed {
					return cancelled()
				}
				if err := writeSkillStep(command, title, localizedHelpText("安装", "Install")); err != nil {
					return err
				}
			}
			result, err := skillinstall.Execute(plan, environment)
			if err != nil {
				if !yes {
					return errors.Join(err, writeSkillConclusion(command, localizedHelpText("Skill 安装失败。", "Skill installation failed.")))
				}
				return err
			}
			if jsonOutput {
				return printJSON(command, result)
			}
			if !yes {
				return writeSkillConclusion(command, localizedHelpText("Skill 安装完成。", "Skill installation complete."))
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), localizedHelpText("Skill 安装完成。", "Skill installation complete."))
			return err
		},
	}
	command.Long = strings.ReplaceAll(command.Long, "{{agentIDs}}", strings.Join(skillinstall.AgentIDs(), ", "))
	command.Flags().StringSliceVar(&agents, "agent", nil, localizedHelpText("指定安装工具，可重复或以逗号分隔", "Choose tools to install for; repeat or separate with commas"))
	command.Flags().BoolVar(&project, "project", false, localizedHelpText("安装到当前目录（默认）；与 --global 互斥", "Install in the current directory (default); mutually exclusive with --global"))
	command.Flags().BoolVar(&global, "global", false, localizedHelpText("安装到当前用户目录；与 --project 互斥", "Install for the current user; mutually exclusive with --project"))
	command.Flags().BoolVar(&copyFiles, "copy", false, localizedHelpText("为每个安装位置创建独立副本", "Create an independent copy in each installation location"))
	command.Flags().BoolVarP(&yes, "yes", "y", false, localizedHelpText("跳过交互并确认覆盖", "Skip prompts and confirm overwrite"))
	command.Flags().BoolVar(&jsonOutput, "json", false, localizedHelpText("输出 JSON 安装结果；必须同时指定 --yes", "Output the installation result as JSON; requires --yes"))
	return command
}

func scopeUsageError() error {
	return localizedUsageErrorf("--project 与 --global 不能同时使用", "--project and --global cannot be used together")
}

func terminalStreams(command *cobra.Command) bool {
	input, inputOK := command.InOrStdin().(*os.File)
	output, outputOK := command.OutOrStdout().(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func writeSkillPlan(output io.Writer, plan skillinstall.Plan) error {
	if _, err := fmt.Fprintf(output, localizedHelpText("安装计划：%s / %s\n", "Installation plan: %s / %s\n"), skillScopeLabel(plan.Scope), skillMethodLabel(plan.Method)); err != nil {
		return err
	}
	hasExisting := false
	for _, target := range plan.Targets {
		description := target.Path
		if target.Kind == "symlink" {
			description += " -> " + target.Target
		}
		if target.Exists {
			hasExisting = true
			description += localizedHelpText("（已存在）", " (exists)")
		}
		if _, err := fmt.Fprintln(output, "  "+description); err != nil {
			return err
		}
	}
	if hasExisting {
		_, err := fmt.Fprintln(output, localizedHelpText("已有目录将完整覆盖，清除本地修改和过期文件。", "Existing directories will be completely overwritten, removing local edits and obsolete files."))
		return err
	}
	return nil
}

func skillScopeLabel(scope skillinstall.Scope) string {
	switch scope {
	case skillinstall.Project:
		return localizedHelpText("当前目录", "Current directory")
	case skillinstall.Global:
		return localizedHelpText("当前用户", "Current user")
	default:
		return string(scope)
	}
}

func skillMethodLabel(method skillinstall.Method) string {
	switch method {
	case skillinstall.Symlink:
		return localizedHelpText("符号链接", "Symlinks")
	case skillinstall.Copy:
		return localizedHelpText("独立副本", "Independent copies")
	default:
		return string(method)
	}
}
