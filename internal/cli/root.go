// Package cli 组装 conduct 的命令行界面（基于 Cobra）。
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newRootCommand 构造根命令并挂载全部子命令与帮助主题。
func newRootCommand() (*cobra.Command, error) {
	rootCommand := &cobra.Command{
		Use:   "conduct",
		Short: "编排并运行多引擎 AI workflow",
		Long: `conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。

它把一份工作流拆解成确定性的执行步骤，逐步驱动 AI 编程引擎完成任务；
支持 claude-code、antigravity、qoder 引擎，均以无头 CLI 方式调用。

工作流的增删改查与解释运行已就位（conduct workflow ...）；运行记录查询见 conduct run ...。`,
		// 子命令自行返回 error，由 Execute 统一打印，避免 Cobra 重复输出用法与错误。
		SilenceUsage:  true,
		SilenceErrors: true,
		// Args 用 ArbitraryArgs 覆盖 Cobra 默认的 legacyArgs——后者在根命令遇未知参数会抢先返回
		// 自带的 "unknown command"（退出码落到 1）、绕过下面的 RunE。改用 ArbitraryArgs 让未知顶层
		// 命令走 RunE，统一归为用法错误（退出码 2）。
		Args: cobra.ArbitraryArgs,
		// 无参裸命令打印帮助；拼错的顶层命令 fail-loud 报用法错误（退出码 2）。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageErrorf("未知命令 %q", args[0])
		},
		// 根命令 --version：Cobra 在 Version 非空时自动挂载 --version 旗标（仅根命令、非持久化，
		// 与 gh / kubectl 惯例一致），打印后退 0。模板对齐 `conduct version` 子命令输出（"conduct <版本>"）。
		Version: version,
	}
	rootCommand.SetVersionTemplate("conduct {{.Version}}\n")
	// Cobra 的旗标解析错误统一包成 usageError（→ 退出码 2）。
	rootCommand.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	rootCommand.AddCommand(newWorkflowCommand())
	rootCommand.AddCommand(newRunCommand())
	rootCommand.AddCommand(newUICommand())
	rootCommand.AddCommand(newVersionCommand())
	if err := addHelpTopics(rootCommand); err != nil {
		return nil, err
	}
	// 替换 Cobra 默认 help 命令：未知主题 fail-loud 退 2（见 newHelpCommand）。
	rootCommand.SetHelpCommand(newHelpCommand(rootCommand))
	return rootCommand, nil
}

// Execute 运行根命令；出错时打印到 stderr 并按错误类别选择退出码：
// usageError → 2（用法错误），其余 → 1（一般错误）。
func Execute() {
	rootCommand, err := newRootCommand()
	if err != nil {
		fmt.Fprintln(os.Stderr, "conduct: "+err.Error())
		os.Exit(1)
	}
	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "conduct: "+err.Error())
		var usage *usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
