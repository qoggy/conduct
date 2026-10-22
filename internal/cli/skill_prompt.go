package cli

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/qoggy/conduct/internal/skillinstall"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func runSkillPrompt(command *cobra.Command, title, description string, field huh.Field) error {
	if description == "" {
		description = localizedHelpText("↑/↓ 选择，Enter 确认。", "↑/↓ select, Enter confirms.")
		if _, confirmation := field.(*huh.Confirm); confirmation {
			description = localizedHelpText("←/→ 选择，Enter 确认。", "←/→ select, Enter confirms.")
		}
	}
	group := huh.NewGroup(field).Title("◆  " + title).Description(description)
	// Keep Huh's cancellable branch under TERM=dumb; the accessible branch
	// treats EOF as the default answer and supplies its own English prompts.
	keymap := huh.NewDefaultKeyMap()
	keymap.Quit.SetKeys("ctrl+c", "ctrl+d")
	return huh.NewForm(group).
		WithTheme(skillPromptTheme(field)).
		WithAccessible(false).
		WithKeyMap(keymap).
		WithProgramOptions(tea.WithFilter(func(_ tea.Model, message tea.Msg) tea.Msg {
			if _, resized := message.(tea.WindowSizeMsg); resized {
				// Huh only shrinks field heights. Restore natural height before
				// it measures the group and fits it to the new terminal size.
				field.WithHeight(0)
			}
			return message
		})).
		WithInput(command.InOrStdin()).
		WithOutput(command.OutOrStdout()).
		WithShowHelp(false).
		RunWithContext(command.Context())
}

func skillPromptTheme(field huh.Field) huh.Theme {
	return huh.ThemeFunc(func(dark bool) *huh.Styles {
		styles := huh.ThemeCharm(dark)
		cyan, green := lipgloss.Color("6"), lipgloss.Color("2")
		styles.Focused.Base = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(lipgloss.Color("8")).PaddingLeft(1)
		styles.Focused.Title = lipgloss.NewStyle().Foreground(cyan).Bold(true)
		styles.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(cyan).SetString("› ")
		styles.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(green).SetString("◼ ")
		styles.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("◻ ")
		styles.Focused.SelectedOption = styles.Focused.SelectedOption.Foreground(green)
		styles.Focused.FocusedButton = lipgloss.NewStyle().Foreground(cyan).Bold(true).PaddingRight(2).Transform(func(text string) string { return "● " + text })
		styles.Focused.BlurredButton = lipgloss.NewStyle().PaddingRight(2).Transform(func(text string) string { return "○ " + text })
		if _, selectField := field.(*huh.Select[string]); selectField {
			styles.Focused.SelectSelector = lipgloss.NewStyle()
			styles.Focused.SelectedOption = styles.Focused.SelectedOption.Transform(func(text string) string { return "● " + text })
			styles.Focused.UnselectedOption = styles.Focused.UnselectedOption.Transform(func(text string) string { return "○ " + text })
		}
		styles.Blurred = styles.Focused
		styles.Group.Title = styles.Focused.Title
		styles.Group.Description = styles.Focused.Description
		return styles
	})
}

func skillTerminalWidth(command *cobra.Command) (int, error) {
	file, ok := command.OutOrStdout().(*os.File)
	if !ok {
		return 80, nil
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0, fmt.Errorf("read skill installation terminal size: %w", err)
	}
	return max(10, width), nil
}

func writeSkillIntroduction(command *cobra.Command) error {
	_, err := fmt.Fprintln(command.OutOrStdout(), "┌  conduct Skill\n│")
	return err
}

func writeSkillConclusion(command *cobra.Command, message string) error {
	_, err := fmt.Fprintf(command.OutOrStdout(), "└  %s\n\n", message)
	return err
}

func writeSkillStep(command *cobra.Command, title, value string) error {
	width, err := skillTerminalWidth(command)
	if err != nil {
		return err
	}
	value = ansi.Wrap(value, max(1, width-3), "")
	_, err = fmt.Fprintf(command.OutOrStdout(), "◇  %s\n│  %s\n│\n", title, strings.ReplaceAll(value, "\n", "\n│  "))
	return err
}

func writeSkillNote(command *cobra.Command, title, body string) error {
	width, err := skillTerminalWidth(command)
	if err != nil {
		return err
	}
	contentWidth := min(max(1, width-5), max(20, lipgloss.Width(body)+2))
	box := lipgloss.NewStyle().Width(contentWidth).Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(body)
	_, err = fmt.Fprintf(command.OutOrStdout(), "◇  %s\n│  %s\n│\n", title, strings.ReplaceAll(box, "\n", "\n│  "))
	return err
}

func writeInteractiveSkillPlan(command *cobra.Command, plan skillinstall.Plan) error {
	var body strings.Builder
	if err := writeSkillPlan(&body, plan); err != nil {
		return err
	}
	_, targets, _ := strings.Cut(body.String(), "\n")
	content := skillScopeLabel(plan.Scope) + " / " + skillMethodLabel(plan.Method) + "\n\n" + targets
	return writeSkillNote(command, localizedHelpText("安装汇总", "Installation Summary"), strings.TrimSuffix(content, "\n"))
}

func skillToolNames(ids []string, agents []skillinstall.Agent) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		for _, agent := range agents {
			if agent.ID == id {
				names = append(names, agent.DisplayName)
				break
			}
		}
	}
	return strings.Join(names, ", ")
}
