package cli

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/help"
	"github.com/qoggy/conduct/internal/locale"
	"github.com/spf13/cobra"
)

// addHelpTopics 预校验全部帮助主题的内嵌正文可读（登记与 .md 漂移则启动即 fail-loud），
// 并把「Additional help topics」这一区追加进根命令的用法模板——列成 `conduct help <主题>`。
// 主题不再注册成顶层命令：裸调 `conduct <主题>` 落到根命令的「未知命令」fail-loud（退 2），
// 只有 `conduct help <主题>` 能访问（与 git / go 惯例一致）。深度内容单点维护在 internal/help，
// 各命令的 --help 只留一行指针、保持精简。
func addHelpTopics(root *cobra.Command) error {
	for _, language := range []locale.Language{locale.Chinese, locale.English} {
		for _, topic := range help.Topics(language) {
			if _, _, err := help.Content(topic.Name, language); err != nil {
				return err // 内嵌文档读取失败（登记与 .md 漂移）——上抛不静默
			}
		}
	}
	cobra.AddTemplateFunc("conductHelpTopics", helpTopicsSection)
	// 两种语言都使用完整模板，避免中文 help 泄漏 Cobra 默认英文栏目标题。
	// 仅根命令 --help 追加帮助主题区（子命令 HasParent 为真，跳过）。
	root.SetUsageTemplate(localizedUsageTemplate() + "{{if not .HasParent}}{{conductHelpTopics}}{{end}}")
	return nil
}

func localizedUsageTemplate() string {
	return localizedHelpText(chineseUsageTemplate, englishUsageTemplate)
}

const chineseUsageTemplate = `用法：{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [命令]{{end}}{{if gt (len .Aliases) 0}}

别名：
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例：
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

可用命令：{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

其它命令：{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

选项：
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局选项：
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

其它帮助主题：{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

使用 "{{.CommandPath}} [命令] --help" 查看某个命令的更多信息。{{end}}
`

const englishUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// helpTopicsSection 渲染根命令 --help 末尾的帮助主题清单，每行列成可访问形式 `conduct help <主题>`
// （而非可被裸调的 `conduct <主题>`）。无主题时返回空串，连标题都不出。
func helpTopicsSection() string {
	topics := help.Topics(detectedHelpLanguage())
	if len(topics) == 0 {
		return ""
	}
	width := 0
	for _, topic := range topics {
		if labelLen := len("conduct help " + topic.Name); labelLen > width {
			width = labelLen
		}
	}
	var b strings.Builder
	b.WriteString(localizedHelpText("\n其它帮助主题：\n", "\nAdditional help topics:\n"))
	for _, topic := range topics {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, "conduct help "+topic.Name, topic.Short)
	}
	return b.String()
}

// newHelpCommand 替换 Cobra 默认 help 命令：
//   - `conduct help <主题>`  直接输出 internal/help 的内嵌长文档；
//   - `conduct help <命令>`  打印该命令的用法；
//   - 未知主题 / 命令        fail-loud 报用法错误退 2（对标 go help 的 "unknown help topic"），
//     而非 Cobra 默认的「打印根帮助、退 0」——与 conduct 别处「拼错子命令退 2」一致。
func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use: localizedHelpText(
			"help [命令 | 主题]",
			"help [command | topic]",
		),
		Short: localizedHelpText(
			"查看某个命令的用法，或输出一个帮助主题（conduct help <主题>）",
			"Show usage for a command or output a help topic (conduct help <topic>)",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return root.Help()
			}
			// 帮助主题：conduct help prompts → 输出内嵌长文档。
			if len(args) == 1 {
				content, ok, err := help.Content(args[0], detectedHelpLanguage())
				if err != nil {
					return err // 内嵌读取失败不静默
				}
				if ok {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.TrimRight(content, "\n"))
					return err
				}
			}
			// 真实命令：conduct help workflow → 打印其用法。Find 会把未匹配的尾部参数另行
			// 返回；只要还有剩余，就说明整条命令路径不存在，不能退化成已匹配父命令的帮助。
			target, remaining, err := root.Find(args)
			if err != nil || target == nil || target == root || len(remaining) > 0 {
				return usageErrorf(localizedHelpText(
					"未知帮助主题 %q（可用主题：%s；命令用法见 conduct --help 或 conduct help <命令>）",
					"Unknown help topic %q (available topics: %s; see conduct --help or conduct help <command> for command usage)",
				), strings.Join(args, " "), strings.Join(topicNames(), " / "))
			}
			return target.Help()
		},
	}
}

// topicNames 返回全部帮助主题名，供未知主题时提示可用集。
func topicNames() []string {
	topics := help.Topics(detectedHelpLanguage())
	names := make([]string, len(topics))
	for index, topic := range topics {
		names[index] = topic.Name
	}
	return names
}
