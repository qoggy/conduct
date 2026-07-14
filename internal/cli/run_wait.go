package cli

import (
	"fmt"
	"time"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

// waitPollInterval 是 run wait 轮询 run.json + pid 判活的间隔（无墙钟超时，run 跑多久就等多久）。
const waitPollInterval = 500 * time.Millisecond

func newRunWaitCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "wait <id>",
		Short: "阻塞等待一次运行到终态（等到即退 0）",
		Long: "阻塞到 <id> 运行到达任一终态即返回并退 0，退出码只表达「有没有等到终态」，run 的成败不进退出码。\n" +
			"<id> 取自 conduct run list。命令自身出错才非 0：不存在 / IO 失败 → 1，缺 / 非法 id → 2。\n" +
			"run 的成败（completed / failed / interrupted）读 stdout 摘要或 --json 的 status，别用退出码判。",
		Args: requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := run.ValidateID(id); err != nil {
				return &usageError{err: err} // 非法 id → 退 2
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			record, err := waitForTerminal(st, id, waitPollInterval)
			if err != nil {
				return err // 不存在 / IO → 退 1（非 2）
			}
			status := record.EffectiveStatus()
			if asJSON {
				record.Status = status // 派生态覆盖存储态：running 但进程已死 → interrupted
				return printJSON(cmd, record)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "运行 %s · %s · 耗时 %s\n", record.ID, status, elapsed(record))
			// 等到终态即完成本职 → 退 0；completed / failed / interrupted 一视同仁，run 的成败在
			// status（stdout 摘要 / --json）里看，不进退出码——对标 docker wait / Unix wait。
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出收尾时 run.json 的规范化内容（同 run show <id> --json）")
	return cmd
}

// waitForTerminal 轮询到指定运行达终态并返回其记录：已终态立即返回、不空等；仍在跑则周期性重读
// run.json 与 pid 存活直到转终态（含等待期间进程崩溃派生 interrupted）。目标不存在 / IO 失败上抛。
func waitForTerminal(st *store.Store, id string, poll time.Duration) (*run.Record, error) {
	for {
		record, err := st.LoadRun(id)
		if err != nil {
			return nil, err // ErrRunNotExist / IO：调用方据此退 1
		}
		if isTerminalStatus(record.EffectiveStatus()) {
			return record, nil
		}
		time.Sleep(poll)
	}
}

// isTerminalStatus 报告一个派生态是否为终态（不再变化）：running 之外皆终态。
func isTerminalStatus(status run.Status) bool {
	return status != run.StatusRunning
}
