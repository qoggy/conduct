package cli

import (
	"strings"

	"github.com/qoggy/conduct/internal/help"
	"github.com/spf13/cobra"
)

// addHelpTopics 把 internal/help 的长文档主题注册为 Cobra「附加帮助主题」——
// 只有 Long、无 Run 的命令，出现在帮助的 "Additional help topics:" 区，经
// `conduct help <主题>` 输出。深度内容（教程 / 概念 / 最佳实践）单点维护在 internal/help，
// 各命令的 --help 只留一行指针、保持精简。
func addHelpTopics(root *cobra.Command) error {
	for _, topic := range help.Topics() {
		content, _, err := help.Content(topic.Name)
		if err != nil {
			return err // 内嵌文档读取失败（登记与 .md 漂移）——上抛不静默
		}
		root.AddCommand(&cobra.Command{
			Use:   topic.Name,
			Short: topic.Short,
			Long:  content,
		})
	}
	return nil
}

// newHelpCommand 替换 Cobra 默认 help 命令，只为改一处行为：未知主题 / 命令时
// fail-loud 报用法错误退 2（对标 go help 的 "unknown help topic. Run 'go help'."），
// 而非 Cobra 默认的「打印根帮助、退 0」——与 conduct 别处「拼错子命令退 2」的风格一致。
// 已知命令 / 主题仍打印其帮助（`conduct help workflow`、`conduct help prompts`）。
func newHelpCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [命令 | 主题]",
		Short: "查看某个命令的用法，或输出一个帮助主题（conduct help <主题>）",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return root.Help()
			}
			target, _, err := root.Find(args)
			if err != nil || target == nil || target == root {
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
