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
		Short: localizedHelpText("列出历史运行记录", "List historical run records"),
		Long: localizedHelpText(
			"列出历史运行记录，按时间倒序。默认列全部（含已完成 / 失败 / 中断）。\n"+
				"--status 按派生态过滤：running（pid 真存活）/ completed / failed / interrupted（已崩溃）；非法取值退 2。",
			"List historical run records in reverse chronological order. By default, list all records (including completed / failed / interrupted).\n"+
				"--status filters by derived status: running (pid is actually alive) / completed / failed / interrupted (process has crashed); an invalid value exits 2.",
		),
		Args: noArgs,
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
				fmt.Fprintln(cmd.ErrOrStderr(), localizedHelpText("警告: 跳过无法解析的运行记录: ", "warning: skipped an unreadable run record: ")+skipErr.Error())
			}
			records = filterRunsByStatus(records, wantStatus)
			if asJSON {
				return printJSON(cmd, runListItems(records))
			}
			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), localizedHelpText("（暂无运行记录）", "(no run records)"))
				return nil
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(writer, localizedHelpText("RUN ID\t工作流\t状态\t节点\t开始时间\t需求", "RUN ID\tWORKFLOW\tSTATUS\tNODES\tSTARTED\tPROMPT"))
			for _, record := range records {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%s\t%s\n",
					record.ID, record.Workflow, record.EffectiveStatus(), recordNodeCount(record),
					formatTimestamp(record.StartedAt), preview(record.UserPrompt, 20))
			}
			return writer.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText("以机器可读 JSON 输出（userPrompt 全文不截断）", "Output machine-readable JSON (without truncating userPrompt)"))
	cmd.Flags().StringVar(&statusFilter, "status", "", localizedHelpText(
		"只列指定派生态的运行：running / completed / failed / interrupted",
		"List only runs with the specified derived status: running / completed / failed / interrupted",
	))
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
		return "", localizedUsageErrorf("非法的 --status 取值 %q（可用：running / completed / failed / interrupted）", "invalid --status value %q (available: running / completed / failed / interrupted)", value)
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
// nodeCount = agent 节点数（读时由快照算），替代旧的 steps。
type runListItem struct {
	ID         string     `json:"id"`
	Workflow   string     `json:"workflow"`
	Status     run.Status `json:"status"`
	NodeCount  int        `json:"nodeCount"`
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
			NodeCount:  recordNodeCount(record),
			StartedAt:  record.StartedAt,
			UserPrompt: record.UserPrompt,
		})
	}
	return items
}
