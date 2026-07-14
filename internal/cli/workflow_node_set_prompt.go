package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeSetPromptCommand 构造 `conduct workflow node set-prompt <name> <id>`：
// 把某 agent 节点的 promptTemplate 以原始文本从 stdin 读入，由 conduct 负责 JSON 编码，
// 作者永不必手工把含 {{变量}} / 中文 / markdown / 多行的提示词转义进 JSON 字符串。
// 与 node show --prompt 构成 round-trip：读入后剥掉恰好一个尾随换行，与 show 的「补恰好一个」配对，字节稳定。
func newWorkflowNodeSetPromptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-prompt <name> <id>",
		Short: "从 stdin 读原始文本设某节点的提示词",
		Long: "把提示词正文以原始文本从 stdin 整段读入，由 conduct 负责 JSON 编码——含 {{变量}} / 中文 / markdown / 多行都无需转义。\n" +
			"读入后剥掉恰好一个尾随换行（若存在），与 node show --prompt「补恰好一个」配对，使 round-trip 字节稳定。\n" +
			"落盘前复用整份定义的同一套校验（含空模板、模板变量引用须皆祖先）；stdin 是终端（无管道）时报错退出，不挂起等待输入。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			// 名字非法属用法错误退 2，与 node set / copy / workflow show 同族对齐（不落到 Load 抛普通 error 退 1）。
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}

			// stdin 读原始文本；是终端（无管道输入）时报缺少输入的用法错误退出 2，不挂起等待。
			data, err := readStdin(fmt.Sprintf("缺少提示词：请通过 stdin 传入（如 cat prompt.md | conduct workflow node set-prompt %s %s）", name, id))
			if err != nil {
				return err
			}
			// 剥掉恰好一个尾随换行，得提示词模板；空 / 非法在整份校验时兜底退 1。
			promptTemplate := stripOneTrailingNewline(data)

			st, err := openStore()
			if err != nil {
				return err
			}
			wf, err := st.Load(name)
			if err != nil {
				return err
			}
			node, err := requireAgentNode(&wf.Definition, id)
			if err != nil {
				return err
			}
			node.PromptTemplate = promptTemplate

			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 整份校验：空模板 / 模板变量引用非祖先在此退 1，原文件不变
			}
			if err := st.Save(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已更新 %s·%s 提示词\n", name, id)
			return nil
		},
	}
	return cmd
}
