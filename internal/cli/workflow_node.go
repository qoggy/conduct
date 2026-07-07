package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeCommand 构造 `conduct workflow node` 子资源命令族（节点的字段级编辑与查询）。
func newWorkflowNodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "编辑 / 查询工作流的单个节点（字段级，不重发整份定义）",
		Long:  "conduct workflow node —— 只改一个节点的一处结构化字段 / 提示词，或导出单节点定义，省 token、免误伤。",
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageErrorf("未知子命令 %q（可用：set / set-prompt / show）", args[0])
		},
	}
	cmd.AddCommand(
		newWorkflowNodeSetCommand(),
		newWorkflowNodeSetPromptCommand(),
		newWorkflowNodeShowCommand(),
	)
	return cmd
}

// findNode 按 id 在定义里定位节点，返回指向 def.Nodes 切片元素的指针（可原地改）；不存在时返回错误（退出码 1）。
func findNode(def *workflow.Definition, id string) (*workflow.Node, error) {
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			return &def.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("工作流 %s 无节点 %s", def.Name, id)
}

// stripOneTrailingNewline 剥掉恰好一个尾随 \n（仅当存在），返回字符串。
// 与 node show --prompt「补恰好一个尾随换行」配对，使 round-trip 字节稳定。
func stripOneTrailingNewline(data []byte) string {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return string(data[:len(data)-1])
	}
	return string(data)
}
