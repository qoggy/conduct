package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/run"
	"github.com/spf13/cobra"
)

func newRunStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: localizedHelpText("终止一次正在进行的运行", "Stop an in-progress run"),
		Long: localizedHelpText(
			"向正在进行的运行发送终止信号（SIGTERM）。<id> 取自 conduct run list；不存在则报错退 1。\n"+
				"仅 running 可终止：已 completed / failed / interrupted（进程已不在）报错退 1。\n"+
				"先按进程组发信号（连带引擎子进程），进程停写后由 pid 判活派生 interrupted，不落新状态。",
			"Send a termination signal (SIGTERM) to an in-progress run. Get <id> from conduct run list; if it does not exist, report an error and exit 1.\n"+
				"Only running runs may be stopped: completed / failed / interrupted runs (whose process is gone) report an error and exit 1.\n"+
				"First signal the process group (including engine child processes); after the process stops writing, derive interrupted from pid liveness without storing a new status.",
		),
		Args: exactArgs(1),
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
				return err // 不存在 → ErrRunNotExist → 退 1
			}
			// 用派生态判断：running 且 pid 已死会被折算为 interrupted，天然拦下「进程早没了」的重复终止。
			status := record.EffectiveStatus()
			if status != run.StatusRunning {
				return apperror.New(apperror.CodeRunNotStoppable, apperror.Params{"id": id, "status": status})
			}
			if err := run.StopProcess(record.Pid); err != nil {
				return fmt.Errorf("failed to stop run %s (pid %d): %w", id, record.Pid, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("已向运行 %s（pid %d）发送终止信号 SIGTERM。\n", "Sent SIGTERM to run %s (pid %d).\n"), id, record.Pid)
			return nil
		},
	}
	return cmd
}
