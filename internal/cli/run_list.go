package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/qoggy/conduct/internal/run"
	"github.com/spf13/cobra"
)

func newRunListCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出历史运行记录",
		Args:  requireArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			records, skipped, err := st.ListRuns()
			if err != nil {
				return err
			}
			for _, skipErr := range skipped {
				fmt.Fprintln(cmd.ErrOrStderr(), "警告: 跳过无法解析的运行记录: "+skipErr.Error())
			}
			if asJSON {
				return printJSON(cmd, runListItems(records))
			}
			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "（暂无运行记录）")
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, "RUN ID\tWORKFLOW\tSTATUS\tSTEPS\tSTARTED\tPROMPT")
			for _, record := range records {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\t%s\n",
					record.ID, record.Workflow, record.EffectiveStatus(), record.Steps,
					formatTimestamp(record.StartedAt), preview(record.UserPrompt, 20))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "以机器可读 JSON 输出（userPrompt 全文不截断）")
	return cmd
}

// runListItem 是 run list --json 的条目（userPrompt 全文，截断只发生在人类表格）。
type runListItem struct {
	ID         string     `json:"id"`
	Workflow   string     `json:"workflow"`
	Status     run.Status `json:"status"`
	Steps      int        `json:"steps"`
	StartedAt  string     `json:"startedAt"`
	UserPrompt string     `json:"userPrompt"`
}

func runListItems(records []*run.Record) []runListItem {
	items := make([]runListItem, 0, len(records))
	for _, record := range records {
		items = append(items, runListItem{
			ID:         record.ID,
			Workflow:   record.Workflow,
			Status:     record.EffectiveStatus(),
			Steps:      record.Steps,
			StartedAt:  record.StartedAt,
			UserPrompt: record.UserPrompt,
		})
	}
	return items
}
