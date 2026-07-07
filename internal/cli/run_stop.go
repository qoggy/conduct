package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/run"
	"github.com/spf13/cobra"
)

func newRunStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "终止一次正在进行的运行",
		Long: "向正在进行的运行发送终止信号（SIGTERM）。<id> 取自 conduct run list；不存在则报错退 1。\n" +
			"仅 running 可终止：已 completed / failed / interrupted（进程已不在）报错退 1。\n" +
			"先按进程组发信号（连带引擎子进程），进程停写后由 pid 判活派生 interrupted，不落新状态。",
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
				return err // 不存在 → ErrRunNotExist → 退 1
			}
			// 用派生态判断：running 且 pid 已死会被折算为 interrupted，天然拦下「进程早没了」的重复终止。
			status := record.EffectiveStatus()
			if status != run.StatusRunning {
				return fmt.Errorf("运行 %s 当前状态为 %s，无可终止（仅 running 可终止）", id, status)
			}
			if err := run.StopProcess(record.Pid); err != nil {
				return fmt.Errorf("终止运行 %s（pid %d）失败: %w", id, record.Pid, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "已向运行 %s（pid %d）发送终止信号 SIGTERM。\n", id, record.Pid)
			return nil
		},
	}
	return cmd
}
