package cli

import (
	"fmt"
	"os"

	"github.com/qoggy/conduct/internal/launch"
	"github.com/qoggy/conduct/internal/orchestrator"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

func newRunResumeCommand() *cobra.Command {
	var asJSON bool
	var detach bool
	cmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "从中断处恢复一次未完成的运行",
		Long: "恢复一次 failed 或 interrupted 运行：跳过前面已成功的步骤，从中断处整步重跑、续到终态，续写同一 run（id 不变）。\n" +
			"恢复源全部取自落盘——workflowSnapshot 还原步骤、trace 推断重入点并回放重建上游产物与评测反馈；不接受新需求，也不接受 --cwd（沿用原 run 的 userPrompt / cwd）。\n" +
			"failed / interrupted 可恢复；completed / running（进程存活）一律 fail-loud 退 1。\n" +
			"-d / --detach 后台恢复：以独立会话 spawn 子进程续跑，打印 run id 立刻退 0，用 run show / run wait / run stop 查等停。\n\n" +
			"示例：\n" +
			"  conduct run resume autopilot-20260703-152233\n" +
			"  conduct run resume autopilot-20260703-152233 -d",
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
			record, err := st.LoadRun(id)
			if err != nil {
				return err // 不存在 / IO → 退 1
			}
			if err := checkResumable(record); err != nil {
				return err // 前置校验不通过 → 退 1（fail-loud）
			}

			// 预检通过后分道：-d 后台发射，否则前台续跑到底。
			if detach {
				return runResumeDetached(cmd, st, record, asJSON)
			}

			trace, err := st.LoadTrace(id)
			if err != nil {
				return err
			}
			orch := orchestrator.New(st)
			if asJSON {
				obs := &jsonObserver{out: cmd.OutOrStdout()}
				if err := orch.Resume(cmd.Context(), record, trace, obs); err != nil {
					return err // 编排已落盘 failed trace/summary；上抛 → 退 1
				}
				return obs.err // 序列化事件的错误不静默吞
			}
			obs := humanObserver{out: cmd.OutOrStdout()}
			if err := orch.Resume(cmd.Context(), record, trace, obs); err != nil {
				return err
			}
			summary, pathErr := st.SummaryPath(id)
			if pathErr != nil {
				return pathErr
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✅ 完成，阅读 %s 获取运行详情。\n", summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "逐步输出机器可读事件 JSON（每步一行），无进度装饰")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "后台恢复：打印 run id 后立刻退 0，不阻塞到运行结束")
	return cmd
}

// checkResumable 按派生态做 resume 前置校验（fail-loud）：failed / interrupted 可恢复。不满足返回退 1
// 的普通 error（非 usageError），信息明确。重入点由 trace 推断，不依赖 run.json 指针字段。
func checkResumable(record *run.Record) error {
	switch status := record.EffectiveStatus(); status {
	case run.StatusCompleted:
		return fmt.Errorf("%s: 已成功完成，无需恢复", record.ID)
	case run.StatusRunning:
		return fmt.Errorf("%s: 仍在运行中，无法恢复", record.ID)
	case run.StatusFailed, run.StatusInterrupted:
		return nil
	default:
		return fmt.Errorf("%s: 状态 %s 无法恢复", record.ID, status)
	}
}

// runResumeDetached 后台恢复：预检已在调用方同步做完（fail-loud），此处只负责发射。以独立会话 spawn 一个
// 普通前台 `conduct run resume <id>` 子进程（不递归 -d），有界等待其接管后打印 run id 退 0。机制同
// workflow run -d，唯一差异是 run id 即入参、无需 matchRunID 轮询（复用 launch.LaunchResume）。
func runResumeDetached(cmd *cobra.Command, st *store.Store, record *run.Record, asJSON bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("解析自身可执行文件路径失败: %w", err)
	}
	stderrDir, err := os.MkdirTemp("", "conduct-detach-")
	if err != nil {
		return fmt.Errorf("创建启动日志临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(stderrDir) }()

	launcher := launch.NewLauncher(exePath, st, stderrDir, nil)
	return runResumeDetachedWith(cmd, launcher, record, asJSON)
}

// runResumeDetachedWith 消费发射器返回的三态并映射为退出码与输出（同 runDetachedWith，句柄的 workflow
// 取自原 run 记录、人读提示为「已在后台恢复」）。与外层 runResumeDetached 分离，便于注入假发射器单测。
func runResumeDetachedWith(cmd *cobra.Command, launcher detachLauncher, record *run.Record, asJSON bool) error {
	runID, note, err := launcher.LaunchResume(record.ID)
	return emitDetach(cmd, runID, note, err, record.Workflow, asJSON, func(id string) string {
		return fmt.Sprintf("已在后台恢复 %s；conduct run show %s 查看进度、conduct run stop %s 终止。", id, id, id)
	})
}
