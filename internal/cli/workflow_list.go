package cli

import (
	"fmt"
	"sort"
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
			fmt.Fprintln(writer, "NAME\tNODES\tENGINES\tUPDATED")
			for _, def := range defs {
				fmt.Fprintf(writer, "%s\t%d\t%s\t%s\n",
					def.Name, len(def.Nodes), strings.Join(enginesOf(def), ", "), formatTimestamp(def.UpdatedAt))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以机器可读 JSON 输出")
	return cmd
}

type workflowListItem struct {
	Name      string   `json:"name"`
	Nodes     int      `json:"nodes"`
	Engines   []string `json:"engines"`
	UpdatedAt string   `json:"updatedAt"`
}

func listItems(defs []*workflow.Definition) []workflowListItem {
	items := make([]workflowListItem, 0, len(defs))
	for _, def := range defs {
		items = append(items, workflowListItem{
			Name:      def.Name,
			Nodes:     len(def.Nodes),
			Engines:   enginesOf(def),
			UpdatedAt: def.UpdatedAt,
		})
	}
	return items
}

// enginesOf 返回定义里去重排序后的引擎集合（含各节点及其 evaluator 用到的引擎）。
func enginesOf(def *workflow.Definition) []string {
	seen := make(map[string]bool)
	for _, node := range def.Nodes {
		if node.Engine != "" {
			seen[node.Engine] = true
		}
		if node.Evaluator != nil && node.Evaluator.Engine != "" {
			seen[node.Evaluator.Engine] = true
		}
	}
	engines := make([]string, 0, len(seen))
	for engineName := range seen {
		engines = append(engines, engineName)
	}
	sort.Strings(engines)
	return engines
}

// formatTimestamp 把 RFC3339 时间转成人类可读的 "2006-01-02 15:04"；解析失败则原样返回。
func formatTimestamp(rfc3339 string) string {
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return parsed.Format("2006-01-02 15:04")
}
