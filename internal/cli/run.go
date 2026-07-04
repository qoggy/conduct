package cli

import "github.com/spf13/cobra"

// newRunCommand 构造 `conduct run` 名词族——运行记录（不可变历史）的查询入口。
// 「跑一份工作流」是 `conduct workflow run`；本命令只读 ~/.conduct/runs/ 下的历史。
func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "查询运行记录（列表 / 详情）",
		Long:  "conduct run —— 查询运行记录（不可变历史）。跑一份工作流用 conduct workflow run。",
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageErrorf("未知子命令 %q（可用：list / show）", args[0])
		},
	}
	cmd.AddCommand(
		newRunListCommand(),
		newRunShowCommand(),
	)
	return cmd
}
