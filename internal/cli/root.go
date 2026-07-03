// Package cli 组装 conduct 的命令行界面（基于 Cobra）。
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newRootCommand 构造根命令并挂载全部子命令。
func newRootCommand() *cobra.Command {
	rootCommand := &cobra.Command{
		Use:   "conduct",
		Short: "编排并运行多引擎 AI workflow",
		Long: `conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。

它把一份工作流拆解成确定性的执行步骤，逐步驱动 AI 编程引擎完成任务；
支持多种引擎（claude-code、codex、qoder、gemini），全部以无头 CLI 方式调用。

当前为骨架版本：命令与引擎接口已就位，解释器内核尚在移植中。`,
		// 子命令自行返回 error，由 Execute 统一打印，避免 Cobra 重复输出用法与错误。
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCommand.AddCommand(newRunCommand())
	rootCommand.AddCommand(newVersionCommand())
	return rootCommand
}

// Execute 运行根命令；出错时打印到 stderr 并以非零码退出。
func Execute() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "conduct: "+err.Error())
		os.Exit(1)
	}
}
