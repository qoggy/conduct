package cli

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/help"
	"github.com/spf13/cobra"
)

// addHelpTopics 预校验全部帮助主题的内嵌正文可读（登记与 .md 漂移则启动即 fail-loud），
// 并把「Additional help topics」这一区追加进根命令的用法模板——列成 `conduct help <主题>`。
// 主题不再注册成顶层命令：裸调 `conduct <主题>` 落到根命令的「未知命令」fail-loud（退 2），
// 只有 `conduct help <主题>` 能访问（与 git / go 惯例一致）。深度内容单点维护在 internal/help，
// 各命令的 --help 只留一行指针、保持精简。
func addHelpTopics(root *cobra.Command) error {
	for _, topic := range help.Topics() {
		if _, _, err := help.Content(topic.Name); err != nil {
			return err // 内嵌文档读取失败（登记与 .md 漂移）——上抛不静默
		}
	}
	cobra.AddTemplateFunc("conductHelpTopics", helpTopicsSection)
	// 仅根命令 --help 追加该区（子命令 HasParent 为真，跳过）。
	root.SetUsageTemplate(root.UsageTemplate() + "{{if not .HasParent}}{{conductHelpTopics}}{{end}}")
	return nil
}

// helpTopicsSection 渲染根命令 --help 末尾的帮助主题清单，每行列成可访问形式 `conduct help <主题>`
// （而非可被裸调的 `conduct <主题>`）。无主题时返回空串，连标题都不出。
func helpTopicsSection() string {
	topics := help.Topics()
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
	b.WriteString("\nAdditional help topics:\n")
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
		Use:   "help [命令 | 主题]",
		Short: "查看某个命令的用法，或输出一个帮助主题（conduct help <主题>）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return root.Help()
			}
			// 帮助主题：conduct help prompts → 输出内嵌长文档。
			if len(args) == 1 {
				content, ok, err := help.Content(args[0])
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
				return usageErrorf("未知帮助主题 %q（可用主题：%s；命令用法见 conduct --help 或 conduct help <命令>）",
					strings.Join(args, " "), strings.Join(topicNames(), " / "))
			}
			return target.Help()
		},
	}
}

// topicNames 返回全部帮助主题名，供未知主题时提示可用集。
func topicNames() []string {
	topics := help.Topics()
	names := make([]string, len(topics))
	for index, topic := range topics {
		names[index] = topic.Name
	}
	return names
}
