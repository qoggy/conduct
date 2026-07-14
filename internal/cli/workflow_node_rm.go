package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeRmCommand 构造 `conduct workflow node rm <name> <id>`：删一个 agent 节点及其所有连边，再校验结果。
// 合法则做；会制造孤立节点（某节点因此失去全部入边或全部出边）则拒绝并说明。START / END 是保留节点，拒删。
func newWorkflowNodeRmCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <name> <id>",
		Short: "删一个 agent 节点及其所有连边",
		Long: "删名为 <name> 的工作流里 id 为 <id> 的 agent 节点，并级联删除以它为端点的所有边，再校验结果。\n" +
			"删后若制造孤立节点（某节点失去全部入边或全部出边）、或该节点被他人 {{<id>}} 引用致悬空，则拒绝、原文件不变。\n" +
			"START / END 是保留节点，node rm START / node rm END 直接拒绝。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			if id == workflow.NodeIDStart || id == workflow.NodeIDEnd {
				return fmt.Errorf("START / END 为保留节点，不可删除")
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			wf, err := st.Load(name)
			if err != nil {
				return err
			}

			nodes := make([]workflow.Node, 0, len(wf.Definition.Nodes))
			found := false
			for _, node := range wf.Definition.Nodes {
				if node.ID == id {
					found = true
					continue
				}
				nodes = append(nodes, node)
			}
			if !found {
				return fmt.Errorf("工作流 %s 无节点 %s", name, id)
			}
			// 级联删除以该 id 为端点的所有边。
			edges := make([]workflow.Edge, 0, len(wf.Definition.Edges))
			for _, edge := range wf.Definition.Edges {
				if edge.From == id || edge.To == id {
					continue
				}
				edges = append(edges, edge)
			}
			wf.Definition.Nodes = nodes
			wf.Definition.Edges = edges

			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 整份校验：孤立节点 / 悬空引用等在此退 1，原文件不变
			}
			if err := st.Save(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已删节点 %s·%s\n", name, id)
			return nil
		},
	}
	return cmd
}
