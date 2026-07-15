package cli

import (
	"fmt"
	"os"

	"github.com/qoggy/conduct/internal/apperror"
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
		Short: localizedHelpText("从中断处恢复一次未完成的运行", "Resume an incomplete run from its interruption point"),
		Long: localizedHelpText(
			"恢复一次 failed 或 interrupted 运行：跳过已成功的节点，补跑未完成的前沿及其下游、续到终态，续写同一 run（id 不变）。\n"+
				"failed / interrupted 可恢复；completed / running（进程存活）一律 fail-loud 退 1。\n"+
				"-d / --detach 后台恢复：以独立会话 spawn 子进程续跑，打印 run id 立刻退 0，用 run show / run wait / run stop 查等停。\n\n"+
				"示例：\n",
			"Resume a failed or interrupted run: skip nodes that already succeeded, run the unfinished frontier and its downstream nodes, continue to a terminal state, and append to the same run (id unchanged).\n"+
				"failed / interrupted runs may be resumed; completed / running (process alive) always fail loudly and exit 1.\n"+
				"-d / --detach resumes in the background: spawn a child process in an independent session to continue the run, print the run id, and immediately exit 0; use run show / run wait / run stop to inspect, wait for, or stop it.\n\n"+
				"Examples:\n",
		) +
			"  conduct run resume autopilot-20260703-152233\n" +
			"  conduct run resume autopilot-20260703-152233 -d",
		Args: exactArgs(1),
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
			// Resume 的 summary 使用 run.json 中开跑时快照的语言；这里的当前语言只影响 CLI 进度外壳。
			orch.Language = selectedLanguage
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
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✅ 完成，阅读 %s 获取运行详情。\n", "✅ Completed. Read %s for run details.\n"), summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText("逐节点输出机器可读事件 JSON（每节点一行），无进度装饰", "Output machine-readable event JSON for each node (one line per node), without progress decoration"))
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, localizedHelpText("后台恢复：打印 run id 后立刻退 0，不阻塞到运行结束", "Resume in the background: print the run id and immediately exit 0 without blocking until the run finishes"))
	return cmd
}

// checkResumable 按派生态做 resume 前置校验（fail-loud）：failed / interrupted 可恢复。不满足返回退 1
// 的普通 error（非 usageError），信息明确。重入点由 trace 推断，不依赖 run.json 指针字段。
func checkResumable(record *run.Record) error {
	switch status := record.EffectiveStatus(); status {
	case run.StatusCompleted:
		return apperror.New(apperror.CodeRunNotResumable, apperror.Params{"id": record.ID, "status": status})
	case run.StatusRunning:
		return apperror.New(apperror.CodeRunNotResumable, apperror.Params{"id": record.ID, "status": status})
	case run.StatusFailed, run.StatusInterrupted:
		return nil
	default:
		return apperror.New(apperror.CodeRunNotResumable, apperror.Params{"id": record.ID, "status": status})
	}
}

// runResumeDetached 后台恢复：预检已在调用方同步做完（fail-loud），此处只负责发射。以独立会话 spawn 一个
// 普通前台 `conduct run resume <id>` 子进程（不递归 -d），有界等待其接管后打印 run id 退 0。机制同
// workflow run -d，唯一差异是 run id 即入参、无需 matchRunID 轮询（复用 launch.LaunchResume）。
func runResumeDetached(cmd *cobra.Command, st *store.Store, record *run.Record, asJSON bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable path: %w", err)
	}
	stderrDir, err := os.MkdirTemp("", "conduct-detach-")
	if err != nil {
		return fmt.Errorf("failed to create launch log temporary directory: %w", err)
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
		return fmt.Sprintf(localizedHelpText("已在后台恢复 %s；conduct run show %s 查看进度、conduct run stop %s 终止。", "Resumed %s in the background; use conduct run show %s to inspect progress and conduct run stop %s to stop it."), id, id, id)
	})
}
