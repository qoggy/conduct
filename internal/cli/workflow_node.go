package cli

import (
	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeCommand 构造 `conduct workflow node` 子资源命令族（节点的增删与字段级编辑 / 查询）。
func newWorkflowNodeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: localizedHelpText("增删 / 编辑 / 查询工作流的单个节点", "Add, remove, edit, or query individual workflow nodes"),
		Long: localizedHelpText(
			"conduct workflow node —— 建 / 删一个节点并连边，或只改一个节点的一处结构化字段 / 提示词，或导出单节点定义。",
			"conduct workflow node — add or remove a node and connect edges, change only one structured field / prompt on one node, or export a single-node definition.",
		),
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return localizedUsageErrorf("未知子命令 %q（可用：add / rm / set / set-prompt / show）", "unknown subcommand %q (available: add / rm / set / set-prompt / show)", args[0])
		},
	}
	cmd.AddCommand(
		newWorkflowNodeAddCommand(),
		newWorkflowNodeRmCommand(),
		newWorkflowNodeSetCommand(),
		newWorkflowNodeSetPromptCommand(),
		newWorkflowNodeShowCommand(),
	)
	return cmd
}

// requireAgentNode 按 id 在定义里定位 agent 节点，返回指向 def.Nodes 切片元素的指针（可原地改）：
// 不存在退 1；命中 START / END 标记节点也退 1（标记节点无可查看 / 编辑的字段）。供 node set / set-prompt / show 复用。
func requireAgentNode(def *workflow.Definition, id string) (*workflow.Node, error) {
	for i := range def.Nodes {
		if def.Nodes[i].ID == id {
			if def.Nodes[i].IsMarker() {
				return nil, apperror.New(apperror.CodeReservedNodeID, apperror.Params{"id": id})
			}
			return &def.Nodes[i], nil
		}
	}
	return nil, apperror.New(apperror.CodeNodeNotFound, apperror.Params{"id": id})
}

// stripOneTrailingNewline 剥掉恰好一个尾随 \n（仅当存在），返回字符串。
// 与 node show --prompt「补恰好一个尾随换行」配对，使 round-trip 字节稳定。
func stripOneTrailingNewline(data []byte) string {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return string(data[:len(data)-1])
	}
	return string(data)
}
