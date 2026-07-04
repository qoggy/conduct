package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

func newRunShowCommand() *cobra.Command {
	var withTrace, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "查看某次运行的状态与详情",
		Long: "查看某次运行的状态与逐步结果。<id> 取自 conduct run list（形如 <workflow>-<YYYYMMDD-HHMMSS>）；不存在则报错退 1。\n" +
			"默认每步只显示产物前 80 字预览；--trace 展开每步完整 input/output。",
		Args: requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			st, err := openStore()
			if err != nil {
				return err
			}
			record, err := st.LoadRun(id)
			if err != nil {
				return err // 不存在 → ErrNotExist → 退 1
			}
			trace, err := loadTraceIfNeeded(st, id, withTrace, asJSON)
			if err != nil {
				return err
			}
			if asJSON {
				return showRunJSON(cmd, record, trace, withTrace)
			}
			showRunHuman(cmd.OutOrStdout(), record, trace, withTrace)
			return nil
		},
	}
	cmd.Flags().BoolVar(&withTrace, "trace", false, "追加打印逐步 trace（--json 时嵌入 trace 数组）")
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 run.json 的规范化内容")
	return cmd
}

// loadTraceIfNeeded 仅在需要逐步数据时读 trace：人类模式总要（逐步结果），--json 仅 --trace 时要。
func loadTraceIfNeeded(st *store.Store, id string, withTrace, asJSON bool) ([]run.TraceEntry, error) {
	if asJSON && !withTrace {
		return nil, nil
	}
	return st.LoadTrace(id)
}

// showRunJSON 输出 run.json 内容（status 用读时派生态，interrupted 也如实反映）；--trace 时嵌入 trace 数组。
func showRunJSON(cmd *cobra.Command, record *run.Record, trace []run.TraceEntry, withTrace bool) error {
	record.Status = record.EffectiveStatus() // 派生态覆盖存储态：running 但进程已死 → interrupted
	if !withTrace {
		return printJSON(cmd, record)
	}
	payload := struct {
		*run.Record
		Trace []run.TraceEntry `json:"trace"`
	}{Record: record, Trace: trace}
	return printJSON(cmd, payload)
}

// showRunHuman 打印概要 + 逐步结果；--trace 时每步展开完整 input/output。
func showRunHuman(out io.Writer, record *run.Record, trace []run.TraceEntry, withTrace bool) {
	status := record.EffectiveStatus()
	fmt.Fprintf(out, "运行 %s · %s\n", record.ID, status)
	fmt.Fprintf(out, "需求：%s\n", record.UserPrompt)
	// running 与 interrupted 都未正常收尾（无 endedAt），显示进度 step k/N 比「耗时 ?」更有意义。
	if status == run.StatusRunning || status == run.StatusInterrupted {
		fmt.Fprintf(out, "步数 %d · 进度 step %d/%d · %s 起\n",
			record.Steps, len(trace), record.Steps, formatTimestamp(record.StartedAt))
	} else {
		fmt.Fprintf(out, "步数 %d · 耗时 %s · %s → %s\n",
			record.Steps, elapsed(record), formatTimestamp(record.StartedAt), endedDisplay(record))
	}
	if status == run.StatusFailed && record.Error != nil {
		fmt.Fprintf(out, "错误：%s\n", *record.Error)
	}

	for _, entry := range trace {
		result := "成功"
		if !entry.Success {
			result = "失败"
		}
		if withTrace {
			printTraceEntryFull(out, entry, result)
		} else {
			fmt.Fprintf(out, "● step %d [%s] %s  %s  %s\n",
				entry.StepIndex, entry.StepLabel(), entry.Engine, result, preview(entry.Output, 80))
		}
	}
}

// printTraceEntryFull 打印单步的完整 input/output（--trace）。
func printTraceEntryFull(out io.Writer, entry run.TraceEntry, result string) {
	fmt.Fprintf(out, "● step %d [%s] %s %s  %s\n", entry.StepIndex, entry.DisplayName, entry.Type, entry.Engine, result)
	fmt.Fprintf(out, "  ── input ──\n%s\n", entry.Input)
	if entry.Success {
		fmt.Fprintf(out, "  ── output ──\n%s\n", entry.Output)
	} else if entry.Error != nil {
		fmt.Fprintf(out, "  ── error ──\n%s\n", *entry.Error)
	}
}

// elapsed / endedDisplay 处理终态时间展示；endedAt 缺失（异常）时给占位而非崩溃。
func elapsed(record *run.Record) string {
	if record.EndedAt == nil {
		return "?"
	}
	return formatElapsedTimestamps(record.StartedAt, *record.EndedAt)
}

func endedDisplay(record *run.Record) string {
	if record.EndedAt == nil {
		return "?"
	}
	return formatTimestamp(*record.EndedAt)
}

// formatElapsedTimestamps 返回 start→end 的耗时（如 "18.3s"）；任一解析失败给占位。
func formatElapsedTimestamps(start, end string) string {
	startTime, err1 := time.Parse(time.RFC3339, start)
	endTime, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return "?"
	}
	return fmt.Sprintf("%.1fs", endTime.Sub(startTime).Seconds())
}
