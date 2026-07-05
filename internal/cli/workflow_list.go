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
		Short: "列出 store 内全部工作流",
		Args:  requireArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defs, skipped, err := st.List()
			if err != nil {
				return err
			}
			for _, skipErr := range skipped {
				fmt.Fprintln(cmd.ErrOrStderr(), "警告: 跳过无法解析的工作流: "+skipErr.Error())
			}
			if asJSON {
				return printJSON(cmd, listItems(defs))
			}
			if len(defs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "（store 为空）")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "NAME\tNODES\tUPDATED")
			for _, def := range defs {
				fmt.Fprintf(writer, "%s\t%s\t%s\n",
					def.Name, nodeIDStream(def), formatTimestamp(def.UpdatedAt))
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

func listItems(defs []*workflow.Definition) []workflowListItem {
	items := make([]workflowListItem, 0, len(defs))
	for _, def := range defs {
		items = append(items, workflowListItem{
			Name:      def.Name,
			Nodes:     nodeIDs(def),
			UpdatedAt: def.UpdatedAt,
		})
	}
	return items
}

// nodeIDs 返回定义里各节点 id（按定义顺序）；空定义返回空切片（JSON 里为 []，不为 null）。
func nodeIDs(def *workflow.Definition) []string {
	ids := make([]string, len(def.Nodes))
	for i, node := range def.Nodes {
		ids[i] = node.ID
	}
	return ids
}

// nodeIDStream 把节点 id 按定义顺序以 `›` 连接，供人类表格的 NODES 列；
// 超过 6 个截断为前 6 个 + `+N`，避免长流程撑爆表格（节点全量见 show）。
func nodeIDStream(def *workflow.Definition) string {
	ids := nodeIDs(def)
	const maxShown = 6
	if len(ids) > maxShown {
		return strings.Join(ids[:maxShown], "›") + fmt.Sprintf("+%d", len(ids)-maxShown)
	}
	return strings.Join(ids, "›")
}

// formatTimestamp 把 RFC3339 时间转成人类可读的 "2006-01-02 15:04"；解析失败则原样返回。
func formatTimestamp(rfc3339 string) string {
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return parsed.Format("2006-01-02 15:04")
}
