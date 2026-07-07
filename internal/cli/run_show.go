package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
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
		Long: "查看某次运行的详情。<id> 取自 conduct run list（形如 <workflow>-<YYYYMMDD-HHMMSS>）；不存在则报错退 1。\n" +
			"默认打印 run-summary.md（运行总结）；未收尾（running / interrupted）时尚无总结，改打印状态与进度。--trace 展开每步完整 input/output。",
		Args: requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := run.ValidateID(id); err != nil {
				return &usageError{err: err} // 非法 id → 退 2（与 run wait / run rm 对齐，遵全局退出码约定）
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			record, err := st.LoadRun(id)
			if err != nil {
				return err // 不存在 → ErrNotExist → 退 1
			}
			if asJSON {
				var trace []run.TraceEntry
				if withTrace { // --json 仅 --trace 时嵌入逐步数据
					if trace, err = st.LoadTrace(id); err != nil {
						return err
					}
				}
				return showRunJSON(cmd, record, trace, withTrace)
			}
			if withTrace {
				trace, err := st.LoadTrace(id)
				if err != nil {
					return err
				}
				showRunTrace(cmd.OutOrStdout(), record, trace)
				return nil
			}
			return showRunSummary(cmd.OutOrStdout(), st, id, record)
		},
	}
	cmd.Flags().BoolVar(&withTrace, "trace", false, "展开每步完整 input/output（--json 时嵌入 trace 数组）")
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 run.json 的规范化内容")
	return cmd
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

// showRunSummary 打印 run-summary.md 全文（默认视图）；未收尾（running / interrupted）时总结尚未生成，
// 退回状态 + 进度视图并指路 --trace 查看已执行步骤。
func showRunSummary(out io.Writer, st *store.Store, id string, record *run.Record) error {
	md, err := st.ReadSummary(id)
	if err == nil {
		fmt.Fprint(out, md)
		if !strings.HasSuffix(md, "\n") { // 保证行尾换行，避免与 shell 提示符黏在一起
			fmt.Fprintln(out)
		}
		return nil
	}
	if !errors.Is(err, store.ErrSummaryNotExist) {
		return err // 读文件真出错（权限等）：如实上抛，不静默
	}
	// 收尾节点还没写 summary：打印状态与进度，需逐步数据算进度。
	trace, terr := st.LoadTrace(id)
	if terr != nil {
		return terr
	}
	showRunStatus(out, record, trace)
	fmt.Fprintf(out, "运行总结尚未生成（运行未收尾）；用 conduct run show %s --trace 查看已执行步骤。\n", id)
	return nil
}

// showRunTrace 打印状态摘要 + 逐步完整 input/output（--trace）。
func showRunTrace(out io.Writer, record *run.Record, trace []run.TraceEntry) {
	showRunStatus(out, record, trace)
	for _, entry := range trace {
		result := "成功"
		if !entry.Success {
			result = "失败"
		}
		printTraceEntryFull(out, entry, result)
	}
}

// showRunStatus 打印一次运行的状态摘要（运行行 / 需求 / 步数进度或耗时 / 失败错误），不含逐步明细。
func showRunStatus(out io.Writer, record *run.Record, trace []run.TraceEntry) {
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
