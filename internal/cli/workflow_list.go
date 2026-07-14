package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowListCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出全部工作流",
		Args:  requireArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			workflows, skipped, err := st.List()
			if err != nil {
				return err
			}
			for _, skipErr := range skipped {
				fmt.Fprintln(cmd.ErrOrStderr(), "警告: 跳过无法解析的工作流: "+skipErr.Error())
			}
			if asJSON {
				return printJSON(cmd, listItems(workflows))
			}
			if len(workflows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "（store 为空）")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "NAME\tNODES\tUPDATED")
			for _, wf := range workflows {
				fmt.Fprintf(writer, "%s\t%s\t%s\n",
					wf.Name, nodeIDStream(wf), formatTimestamp(wf.UpdatedAt))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以机器可读 JSON 输出")
	return cmd
}

type workflowListItem struct {
	Name      string   `json:"name"`
	Nodes     []string `json:"nodes"`
	UpdatedAt string   `json:"updatedAt"`
}

func listItems(workflows []*workflow.Workflow) []workflowListItem {
	items := make([]workflowListItem, 0, len(workflows))
	for _, wf := range workflows {
		items = append(items, workflowListItem{
			Name:      wf.Name,
			Nodes:     workflow.AgentNodeIDs(&wf.Definition),
			UpdatedAt: wf.UpdatedAt,
		})
	}
	return items
}

// nodeIDStream 把 agent 节点 id 按确定性拓扑序以 `,` 连接，供人类表格的 NODES 列；
// 超过 6 个截断为前 6 个 + `+N`，避免长流程撑爆表格（节点全量见 show）。节点流用 workflow.AgentNodeIDs
// 单一实现（与 UI 运行详情同源、不漂移）。
func nodeIDStream(wf *workflow.Workflow) string {
	ids := workflow.AgentNodeIDs(&wf.Definition)
	const maxShown = 6
	if len(ids) > maxShown {
		return strings.Join(ids[:maxShown], ",") + fmt.Sprintf("+%d", len(ids)-maxShown)
	}
	return strings.Join(ids, ",")
}

// formatTimestamp 把 RFC3339 时间转成人类可读的 "2006-01-02 15:04"；解析失败则原样返回。
func formatTimestamp(rfc3339 string) string {
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return parsed.Format("2006-01-02 15:04")
}
