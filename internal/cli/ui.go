package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/ui"
	"github.com/spf13/cobra"
)

// newUICommand 构造 `conduct ui` —— 启动可视化界面（横跨 workflow 与 run 两族的整体 GUI）。
// 它把 CLI 动词镜像成人看的视图，无独占能力（见 docs/specs/cli-tooling.md〈ui〉）。
func newUICommand() *cobra.Command {
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: localizedHelpText("启动可视化界面（编辑工作流 / 监控运行 / 启动运行）", "Start the visual interface (edit workflows / monitor runs / start runs)"),
		Long: localizedHelpText(
			"conduct ui —— 启动一个只绑 127.0.0.1 的本地 Web 界面，聚合工作流的编辑、运行监控与启动。\n"+
				"它是 CLI 动词层的人类对等物，无独占能力：做的每件事都有对应 CLI 命令。\n"+
				"进程驻留至 Ctrl-C；store 不可读 / 端口被占等启动失败报原因退 1。",
			"conduct ui — start a local web interface bound only to 127.0.0.1 that brings together workflow editing, run monitoring, and run launching.\n"+
				"It is the human-facing equivalent of the CLI verb layer and has no exclusive capabilities: everything it does has a corresponding CLI command.\n"+
				"The process remains running until Ctrl-C; startup failures such as an unreadable store or an occupied port report the reason and exit 1.",
		),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			server, err := ui.NewServer(st, version) // version 是 cli 包内未导出变量，直接传入
			if err != nil {
				return err
			}
			if err := server.ListenAndServe(port, open); err != nil {
				return fmt.Errorf("failed to start conduct ui: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 7420, localizedHelpText(
		"监听端口（被占则报错退 1，不自动递增——可预测、书签友好）",
		"Listening port (if occupied, report an error and exit 1; do not increment automatically—predictable and bookmark-friendly)",
	))
	cmd.Flags().BoolVar(&open, "open", false, localizedHelpText(
		"启动后自动打开浏览器（默认不开，照顾 SSH / 无头环境）",
		"Open a browser automatically after startup (off by default for SSH / headless environments)",
	))
	return cmd
}
