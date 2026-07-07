package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeShowCommand 构造 `conduct workflow node show <name> <id>`：
// 查看单个节点（或其 evaluator）的定义详情，或把其 promptTemplate 单独取为纯文本 / 规范化 JSON。
// 与 node set-prompt 构成 round-trip：--prompt 补恰好一个尾随换行，与 set-prompt「剥恰好一个」配对，字节稳定。
func newWorkflowNodeShowCommand() *cobra.Command {
	var prompt, asJSON, evaluator bool
	cmd := &cobra.Command{
		Use:   "show <name> <id>",
		Short: "查看单个节点 / 评测官详情，或导出其提示词纯文本 / 规范化 JSON",
		Long: "查看单个节点（或其 evaluator）的定义详情；--prompt 只取 promptTemplate 纯文本原文（补恰好一个尾随换行，供 > file），\n" +
			"与 node set-prompt「剥恰好一个尾随换行」配对，round-trip 字节稳定；--json 输出规范化的单个对象。\n" +
			"--prompt 与 --json 互斥。--evaluator 作用于该节点的评测官（节点无 evaluator 时报错退 1）。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			// 名字非法属用法错误退 2，与 node set / copy / workflow show 同族对齐（不落到 Load 抛普通 error 退 1）。
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}

			// --prompt 已是纯文本、--json 是结构化对象，二者互斥，同给报用法错误退 2。
			if prompt && asJSON {
				return usageErrorf("--prompt 与 --json 互斥：--prompt 输出纯文本原文，--json 输出结构化对象，请只择其一")
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			def, err := st.Load(name)
			if err != nil {
				return err
			}
			node, err := findNode(def, id)
			if err != nil {
				return err
			}

			// --evaluator：节点无评测循环则无评测官可查，退 1。
			if evaluator && node.Evaluator == nil {
				return fmt.Errorf("节点 %s 无评测循环，无评测官可查（先用 conduct workflow node set %s %s --evaluator --engine <e> 挂载）", id, name, id)
			}

			out := cmd.OutOrStdout()

			switch {
			case prompt:
				// 纯文本原文 + 恰好一个尾随换行，与 set-prompt 剥一个配对，round-trip 字节稳定。
				tmpl := node.PromptTemplate
				if evaluator {
					tmpl = node.Evaluator.PromptTemplate
				}
				fmt.Fprint(out, appendOneTrailingNewline(tmpl))
				return nil
			case asJSON:
				// 规范化后输出单个对象。Normalize 作用于 def.Nodes，findNode 返回的指针指向其元素，故补齐值已就位。
				def.Normalize()
				if evaluator {
					return printJSON(cmd, node.Evaluator)
				}
				return printJSON(cmd, node)
			default:
				return printNodeShowHuman(cmd, node, evaluator)
			}
		},
	}
	cmd.Flags().BoolVar(&prompt, "prompt", false, "只输出该节点 / 评测官的 promptTemplate 纯文本原文（补恰好一个尾随换行，供重定向到文件）")
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出该节点 / 评测官的规范化定义 JSON（单个对象）")
	cmd.Flags().BoolVar(&evaluator, "evaluator", false, "作用于该节点的评测官而非节点主体（节点无评测官时报错退 1）")
	return cmd
}

// appendOneTrailingNewline 在字符串尾补恰好一个 \n。
// 与 stripOneTrailingNewline「剥恰好一个尾随换行」互逆，是 node show --prompt / node set-prompt round-trip 字节稳定的一半。
func appendOneTrailingNewline(s string) string {
	return s + "\n"
}

// printNodeShowHuman 打印单节点（或其 evaluator）的人类可读详情：一行摘要 + 空行 + 提示词全文（不截断）。
func printNodeShowHuman(cmd *cobra.Command, node *workflow.Node, evaluator bool) error {
	out := cmd.OutOrStdout()
	if evaluator {
		// evaluator 无 displayName、无自己的循环模式（见 design D8），只打印父节点 id·evaluator · engine · model。
		fmt.Fprintf(out, "%s·evaluator · %s · %s\n",
			node.ID, node.Evaluator.Engine, modelDisplay(node.Evaluator.EngineConfig))
		fmt.Fprintln(out)
		fmt.Fprintln(out, node.Evaluator.PromptTemplate)
		return nil
	}
	fmt.Fprintf(out, "%s · %s · %s · %s · %s\n",
		node.ID, node.DisplayName, node.Engine, modelDisplay(node.EngineConfig), loopModeDisplay(*node))
	fmt.Fprintln(out)
	fmt.Fprintln(out, node.PromptTemplate)
	return nil
}
