package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/qoggy/conduct/internal/run"
	"github.com/spf13/cobra"
)

func newRunListCommand() *cobra.Command {
	var asJSON bool
	var statusFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "列出历史运行记录",
		Long: "列出历史运行记录（~/.conduct/runs/ 下每个目录一条），按时间倒序。默认列全部（含已完成 / 失败 / 中断）。\n" +
			"--status 按派生态过滤：running（pid 真存活）/ completed / failed / interrupted（已崩溃）；非法取值退 2。",
		Args: requireArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			wantStatus, err := parseStatusFilter(statusFilter)
			if err != nil {
				return err // 非法 --status → usageError → 退 2
			}
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
			records = filterRunsByStatus(records, wantStatus)
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
	cmd.Flags().StringVar(&statusFilter, "status", "", "只列指定派生态的运行：running / completed / failed / interrupted")
	return cmd
}

// parseStatusFilter 解析 --status 取值：空串表示不过滤（列全部）；合法枚举返回对应 Status；
// 其余报用法错误（退 2）。取值须与 run 的派生态口径一致。
func parseStatusFilter(value string) (run.Status, error) {
	switch value {
	case "":
		return "", nil
	case string(run.StatusRunning), string(run.StatusCompleted), string(run.StatusFailed), string(run.StatusInterrupted):
		return run.Status(value), nil
	default:
		return "", usageErrorf("非法的 --status 取值 %q（可用：running / completed / failed / interrupted）", value)
	}
}

// filterRunsByStatus 按派生态过滤运行记录；want 为空串表示不过滤、原样返回。
func filterRunsByStatus(records []*run.Record, want run.Status) []*run.Record {
	if want == "" {
		return records
	}
	filtered := make([]*run.Record, 0, len(records))
	for _, record := range records {
		if record.EffectiveStatus() == want {
			filtered = append(filtered, record)
		}
	}
	return filtered
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
