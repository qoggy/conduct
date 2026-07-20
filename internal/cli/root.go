// Package cli 组装 conduct 的命令行界面（基于 Cobra）。
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newRootCommand 构造根命令并挂载全部子命令与帮助主题。
func newRootCommand() (*cobra.Command, error) {
	language, err := locale.Resolve()
	if err != nil {
		return nil, err
	}
	selectedLanguage = language
	rootCommand := &cobra.Command{
		Use:   "conduct",
		Short: localizedHelpText("编排并运行多引擎 AI workflow", "Orchestrate and run multi-engine AI workflows"),
		Long: fmt.Sprintf(localizedHelpText(`conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。

它按 DAG 依赖确定性调度节点，并驱动 AI 编程引擎完成任务；
支持 %s 引擎，均以无头 CLI 方式调用。

工作流的增删改查与解释运行已就位（conduct workflow ...）；运行记录查询见 conduct run ...。`, `conduct — a CLI that interprets and runs workflow definitions (JSON).

It schedules nodes deterministically according to DAG dependencies and drives AI coding engines to complete tasks;
it supports the %s engines, all invoked through headless CLIs.

Workflow creation, deletion, editing, querying, and interpreted execution are available under conduct workflow ...; see conduct run ... to query run records.`), engineNamesSentence()),
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
			return localizedUsageErrorf("未知命令 %q", "unknown command %q", args[0])
		},
		// 根命令 --version：Cobra 在 Version 非空时自动挂载 --version 旗标（仅根命令、非持久化，
		// 与 gh / kubectl 惯例一致），打印后退 0。模板对齐 `conduct version` 子命令输出（"conduct <版本>"）。
		Version: version,
	}
	rootCommand.SetVersionTemplate("conduct {{.Version}}\n")
	// Cobra 的旗标解析错误统一包成 usageError（→ 退出码 2）。
	rootCommand.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: localizedFlagError(err)}
	})
	rootCommand.AddCommand(newWorkflowCommand())
	rootCommand.AddCommand(newRunCommand())
	rootCommand.AddCommand(newUICommand())
	rootCommand.AddCommand(newUpdateCommand())
	rootCommand.AddCommand(newVersionCommand())
	if err := addHelpTopics(rootCommand); err != nil {
		return nil, err
	}
	// 替换 Cobra 默认 help 命令：未知主题 fail-loud 退 2（见 newHelpCommand）。
	rootCommand.SetHelpCommand(newHelpCommand(rootCommand))
	rootCommand.InitDefaultHelpCmd()
	rootCommand.InitDefaultCompletionCmd()
	localizeCobraGeneratedHelp(rootCommand)
	return rootCommand, nil
}

func localizeCobraGeneratedHelp(root *cobra.Command) {
	var initializeFlags func(*cobra.Command)
	initializeFlags = func(command *cobra.Command) {
		command.InitDefaultHelpFlag()
		if helpFlag := command.Flags().Lookup("help"); helpFlag != nil {
			helpFlag.Usage = localizedHelpText("显示该命令的帮助", "Show help for this command")
		}
		for _, child := range command.Commands() {
			initializeFlags(child)
		}
	}
	initializeFlags(root)
	root.InitDefaultVersionFlag()
	if versionFlag := root.Flags().Lookup("version"); versionFlag != nil {
		versionFlag.Usage = localizedHelpText("显示 conduct 版本", "Show the conduct version")
	}
	if completion, _, err := root.Find([]string{"completion"}); err == nil && completion != root {
		localizeCompletionCommand(completion)
	}
}

// localizeCompletionCommand 补齐 Cobra 自动生成的 completion 命令。Cobra 只提供英文文案，且 root、
// 四种 shell 子命令与 --no-descriptions 各自持有独立说明；这里只替换人读文案，不改命令名或脚本内容。
func localizeCompletionCommand(completion *cobra.Command) {
	completion.Short = localizedHelpText("为指定 shell 生成自动补全脚本", completion.Short)
	completion.Long = localizedHelpText("为 conduct 生成指定 shell 的自动补全脚本。\n有关如何使用生成脚本的详细信息，请参阅各子命令的帮助。\n", completion.Long)
	completion.Args = noArgs

	rootName := completion.Root().Name()
	longByShell := map[string]string{
		"bash": fmt.Sprintf(`为 bash shell 生成自动补全脚本。

此脚本依赖 'bash-completion' 包。
如果尚未安装，可以通过操作系统的包管理器安装。

在当前 shell 会话中加载补全：

	source <(%[1]s completion bash)

要为每个新会话加载补全，请执行一次：

#### Linux：

	%[1]s completion bash > /etc/bash_completion.d/%[1]s

#### macOS：

	%[1]s completion bash > $(brew --prefix)/etc/bash_completion.d/%[1]s

需要启动一个新的 shell，此设置才会生效。
`, rootName),
		"zsh": fmt.Sprintf(`为 zsh shell 生成自动补全脚本。

如果当前环境尚未启用 shell 补全，需要先启用。以下命令只需执行一次：

	echo "autoload -U compinit; compinit" >> ~/.zshrc

在当前 shell 会话中加载补全：

	source <(%[1]s completion zsh)

要为每个新会话加载补全，请执行一次：

#### Linux：

	%[1]s completion zsh > "${fpath[1]}/_%[1]s"

#### macOS：

	%[1]s completion zsh > $(brew --prefix)/share/zsh/site-functions/_%[1]s

需要启动一个新的 shell，此设置才会生效。
`, rootName),
		"fish": fmt.Sprintf(`为 fish shell 生成自动补全脚本。

在当前 shell 会话中加载补全：

	%[1]s completion fish | source

要为每个新会话加载补全，请执行一次：

	%[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

需要启动一个新的 shell，此设置才会生效。
`, rootName),
		"powershell": fmt.Sprintf(`为 powershell 生成自动补全脚本。

在当前 shell 会话中加载补全：

	%[1]s completion powershell | Out-String | Invoke-Expression

要为每个新会话加载补全，请将上述命令的输出添加到 powershell 配置文件。
`, rootName),
	}
	for _, child := range completion.Commands() {
		child.Short = localizedHelpText(fmt.Sprintf("为 %s 生成自动补全脚本", child.Name()), child.Short)
		if chineseLong, ok := longByShell[child.Name()]; ok {
			child.Long = localizedHelpText(chineseLong, child.Long)
		}
		child.Args = noArgs
		if flag := child.Flags().Lookup("no-descriptions"); flag != nil {
			flag.Usage = localizedHelpText("禁用补全说明", flag.Usage)
		}
	}
}

func localizedFlagError(err error) error {
	if selectedLanguage != locale.Chinese {
		return err
	}
	var notExist *pflag.NotExistError
	if errors.As(err, &notExist) {
		if shorthand := notExist.GetSpecifiedShortnames(); shorthand != "" {
			return fmt.Errorf("未知简写选项 %q（位于 -%s）", notExist.GetSpecifiedName(), shorthand)
		}
		return fmt.Errorf("未知选项 --%s", notExist.GetSpecifiedName())
	}
	var valueRequired *pflag.ValueRequiredError
	if errors.As(err, &valueRequired) {
		if shorthand := valueRequired.GetSpecifiedShortnames(); shorthand != "" {
			return fmt.Errorf("简写选项 %q（位于 -%s）需要一个值", valueRequired.GetSpecifiedName(), shorthand)
		}
		return fmt.Errorf("选项 --%s 需要一个值", valueRequired.GetSpecifiedName())
	}
	var invalidValue *pflag.InvalidValueError
	if errors.As(err, &invalidValue) {
		return fmt.Errorf("选项 --%s 的值 %q 无效：%v", invalidValue.GetFlag().Name, invalidValue.GetValue(), errors.Unwrap(invalidValue))
	}
	var invalidSyntax *pflag.InvalidSyntaxError
	if errors.As(err, &invalidSyntax) {
		return fmt.Errorf("选项语法无效：%s", invalidSyntax.GetSpecifiedFlag())
	}
	return fmt.Errorf("选项错误：%s", err)
}

// Execute 运行根命令；出错时打印到 stderr 并按错误类别选择退出码：
// usageError → 2（用法错误），其余 → 1（一般错误）。
func Execute() {
	rootCommand, err := newRootCommand()
	if err != nil {
		fmt.Fprintln(os.Stderr, "conduct: "+formatCLIError(err))
		os.Exit(1)
	}
	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "conduct: "+formatCLIError(err))
		var usage *usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
