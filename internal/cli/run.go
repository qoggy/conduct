package cli

import "github.com/spf13/cobra"

// newRunCommand 构造 `conduct run` 名词族——运行记录的查询与生命周期操作入口。
// 「跑一份工作流」是 `conduct workflow run`；本命令只读 ~/.conduct/runs/ 下的历史。
func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: localizedHelpText("查询 / 编排运行记录（列表 / 详情 / 终止 / 等待 / 恢复 / 删除）", "Query / orchestrate run records (list / show / stop / wait / resume / delete)"),
		Long: localizedHelpText(
			"conduct run —— 运行记录的查询、终止、等待、恢复与删除。run id 与冻结工作流快照不变，状态与执行记录会随运行和恢复更新。跑一份工作流用 conduct workflow run。",
			"conduct run — query, stop, wait for, resume, and delete run records. The run id and frozen workflow snapshot stay unchanged; status and execution records are updated as the run executes and resumes. Use conduct workflow run to run a workflow.",
		),
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return localizedUsageErrorf("未知子命令 %q（可用：list / show / stop / wait / resume / rm）", "unknown subcommand %q (available: list / show / stop / wait / resume / rm)", args[0])
		},
	}
	cmd.AddCommand(
		newRunListCommand(),
		newRunShowCommand(),
		newRunStopCommand(),
		newRunWaitCommand(),
		newRunResumeCommand(),
		newRunRmCommand(),
	)
	return cmd
}
