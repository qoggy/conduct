package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// usageError 标记「用法错误」——由 Execute 映射为退出码 2（缺参 / 非法参数 / 非交互拒绝危险操作）。
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usageErrorf(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

// requireArgs 把 Cobra 的位置参数校验错误包成 usageError（→ 退出码 2）。
func requireArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validator(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

// newWorkflowCommand 构造 `conduct workflow` 名词族及其动词子命令。
func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "管理工作流定义（创建 / 编辑 / 改名 / 删除 / 查询）",
		Long:  "conduct workflow —— 工作流定义的托管 store，按名字寻址（存于 ~/.conduct/workflows/）。",
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageErrorf("未知子命令 %q（可用：create / edit / rename / delete / list / show / run）", args[0])
		},
	}
	cmd.AddCommand(
		newWorkflowCreateCommand(),
		newWorkflowEditCommand(),
		newWorkflowRenameCommand(),
		newWorkflowDeleteCommand(),
		newWorkflowListCommand(),
		newWorkflowShowCommand(),
		newWorkflowRunCommand(),
	)
	return cmd
}

// openStore 打开生产 store（~/.conduct）。
func openStore() (*store.Store, error) {
	return store.Default()
}

// stdinIsTerminal 报告 stdin 是否为真实交互终端（用 x/term 的 ioctl 判定，
// 能正确区分真 TTY 与 /dev/null、管道、重定向文件——后者一律视为非终端）。
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readStdinDefinition 从 stdin 读入完整定义；stdin 是终端（无管道输入）时报用法错误退出 2，不挂起等待。
func readStdinDefinition() ([]byte, error) {
	if stdinIsTerminal() {
		return nil, usageErrorf("缺少定义：请通过 stdin 传入（如 cat def.json | conduct workflow ...）；可视化编辑用 conduct ui")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("读取 stdin 失败: %w", err)
	}
	return data, nil
}

// reconcileImportName 处理导入体里的 name：若出现且与目标名不一致则拒绝（绝不静默改名）。
// createdAt / updatedAt 等系统元数据由 store 写入，导入值忽略。
func reconcileImportName(def *workflow.Definition, target string) error {
	if def.Name != "" && def.Name != target {
		return fmt.Errorf("导入定义的 name=%q 与目标 %q 不一致（改名请用 conduct workflow rename）", def.Name, target)
	}
	return nil
}

// confirmDeletion 在交互终端下就删除做二次确认，回答 y / yes 才算确认。
func confirmDeletion(cmd *cobra.Command, names []string) (bool, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "将删除 %d 个工作流：%s。确认？[y/N] ", len(names), strings.Join(names, ", "))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("读取确认输入失败: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// printJSON 把值以缩进 JSON 打印到 stdout。
func printJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
