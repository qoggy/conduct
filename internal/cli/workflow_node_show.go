package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeShowCommand 构造 `conduct workflow node show <name> <id>`：
// 查看单个 agent 节点的定义详情，或把其 promptTemplate 单独取为纯文本 / 规范化 JSON。
// 与 node set-prompt 构成 round-trip：--prompt 补恰好一个尾随换行，与 set-prompt「剥恰好一个」配对，字节稳定。
func newWorkflowNodeShowCommand() *cobra.Command {
	var prompt, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name> <id>",
		Short: localizedHelpText("查看单个 agent 节点详情", "Show details for one agent node"),
		Long: localizedHelpText(
			"查看单个 agent 节点的定义详情；--prompt 只取 promptTemplate 纯文本原文（补恰好一个尾随换行，供 > file），\n"+
				"与 node set-prompt「剥恰好一个尾随换行」配对，round-trip 字节稳定；--json 输出规范化的单个对象。\n"+
				"--prompt 与 --json 互斥。<id> 须是 agent 节点（START / END 标记节点无可展示的定义）。",
			"Show definition details for one agent node; --prompt outputs only the original plain text of promptTemplate (adding exactly one trailing newline for > file),\n"+
				"paired with node set-prompt removing exactly one trailing newline so round-trip bytes remain stable; --json outputs one normalized object.\n"+
				"--prompt and --json are mutually exclusive. <id> must be an agent node (the START / END marker nodes have no displayable definition).",
		),
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			// 名字非法属用法错误退 2，与 node set / copy / workflow show 同族对齐（不落到 Load 抛普通 error 退 1）。
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			// --prompt 已是纯文本、--json 是结构化对象，二者互斥，同给报用法错误退 2。
			if prompt && asJSON {
				return localizedUsageErrorf("--prompt 与 --json 互斥：--prompt 输出纯文本原文，--json 输出结构化对象，请只择其一", "--prompt and --json are mutually exclusive: --prompt outputs the original plain text and --json outputs a structured object; choose one")
			}

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

			out := cmd.OutOrStdout()
			switch {
			case prompt:
				// 纯文本原文 + 恰好一个尾随换行，与 set-prompt 剥一个配对，round-trip 字节稳定。
				fmt.Fprint(out, appendOneTrailingNewline(node.PromptTemplate))
				return nil
			case asJSON:
				return printJSON(cmd, node)
			default:
				fmt.Fprintf(out, "%s · %s · %s · %s\n",
					node.ID, node.DisplayName, node.Engine, modelDisplay(node.EngineConfig))
				fmt.Fprintln(out)
				fmt.Fprintln(out, node.PromptTemplate)
				return nil
			}
		},
	}
	cmd.Flags().BoolVar(&prompt, "prompt", false, localizedHelpText(
		"只输出该节点的 promptTemplate 纯文本原文（补恰好一个尾随换行，供重定向到文件）",
		"Output only the node's original promptTemplate plain text (add exactly one trailing newline for redirection to a file)",
	))
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText("输出该节点的规范化定义 JSON（单个对象）", "Output the node's normalized definition JSON (one object)"))
	return cmd
}

// appendOneTrailingNewline 在字符串尾补恰好一个 \n。
// 与 stripOneTrailingNewline「剥恰好一个尾随换行」互逆，是 node show --prompt / node set-prompt round-trip 字节稳定的一半。
func appendOneTrailingNewline(s string) string {
	return s + "\n"
}
