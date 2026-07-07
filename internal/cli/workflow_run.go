package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/qoggy/conduct/internal/orchestrator"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowRunCommand() *cobra.Command {
	var cwd string
	var asJSON bool
	var detach bool
	cmd := &cobra.Command{
		Use:   `run <name> ["<需求>"]`,
		Short: "解释运行一份工作流",
		Long: "解释运行名为 <name> 的工作流：按定义逐节点驱动 AI 引擎执行，前台同步阻塞并打印进度，结束后用 conduct run show <id> 看记录。\n" +
			"用户需求经第二个位置参数或 stdin 传入（二者其一必填；均缺且 stdin 是终端则报错退 2，不挂起）。\n" +
			"-d / --detach 后台起跑：预检通过后以独立会话 spawn 子进程，打印 run id 立刻退 0，用 run show / run wait / run stop 查等停。\n\n" +
			"示例：\n" +
			"  conduct workflow run myflow \"把 README 翻译成英文\"\n" +
			"  echo \"把 README 翻译成英文\" | conduct workflow run myflow\n" +
			"  conduct workflow run myflow \"重构结算流程\" -d",
		Args: requireArgs(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			userPrompt, err := resolveUserPrompt(args)
			if err != nil {
				return err
			}
			workingDir, err := resolveCwd(cwd)
			if err != nil {
				return err
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			def, err := st.Load(name)
			if err != nil {
				return err
			}
			if err := workflow.Validate(def); err != nil {
				return err // 载入即校验，防手改损坏
			}

			// 预检全部同步做完（fail-loud），到这里才分道：-d 后台发射，否则前台跑到底。
			if detach {
				return runDetached(cmd, st, name, userPrompt, workingDir, asJSON)
			}

			orch := orchestrator.New(st)
			if asJSON {
				return runWithJSON(cmd, orch, def, userPrompt, workingDir)
			}
			return runWithHuman(cmd, orch, def, userPrompt, workingDir, st)
		},
	}
	cmd.Flags().StringVar(&cwd, "cwd", "", "AI 引擎读写文件的工作目录（默认当前目录），即 {{sys.cwd}}")
	cmd.Flags().BoolVar(&asJSON, "json", false, "逐步输出机器可读事件 JSON（每步一行），无进度装饰")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "后台起跑：打印 run id 后立刻退 0，不阻塞到运行结束")
	return cmd
}

// runWithHuman 以人类可读进度运行，收尾指向 run-summary.md。
func runWithHuman(cmd *cobra.Command, orch *orchestrator.Orchestrator, def *workflow.Definition,
	userPrompt, workingDir string, st *store.Store) error {
	obs := humanObserver{out: cmd.OutOrStdout()}
	runID, err := orch.Run(cmd.Context(), def, userPrompt, workingDir, obs)
	if err != nil {
		return err // 编排已落盘 failed trace/summary；此处上抛 → Execute 退 1
	}
	summary, pathErr := st.SummaryPath(runID)
	if pathErr != nil {
		return pathErr
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✅ 完成，阅读 %s 获取运行详情。\n", summary)
	return nil
}

// runWithJSON 以逐步事件 JSON 运行（无汇总事件，整体概要见 run.json）。
func runWithJSON(cmd *cobra.Command, orch *orchestrator.Orchestrator, def *workflow.Definition,
	userPrompt, workingDir string) error {
	obs := &jsonObserver{out: cmd.OutOrStdout()}
	_, err := orch.Run(cmd.Context(), def, userPrompt, workingDir, obs)
	if err != nil {
		return err
	}
	return obs.err // 序列化事件时的错误不静默吞
}

// resolveUserPrompt 按优先级取用户需求：位置参数 > 非 TTY 的 stdin；都无且 stdin 是终端则用法错误（退 2），不挂起。
func resolveUserPrompt(args []string) (string, error) {
	if len(args) == 2 {
		// 与 stdin 路径同一标准：空白需求不放行，避免带着空需求去烧引擎。
		prompt := strings.TrimSpace(args[1])
		if prompt == "" {
			return "", usageErrorf("用户需求不能为空")
		}
		return prompt, nil
	}
	if stdinIsTerminal() {
		return "", usageErrorf("缺少用户需求：作为第二个参数传入，或经 stdin 管道输入（如 cat req.txt | conduct workflow run <name>）")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("读取 stdin 失败: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", usageErrorf("stdin 未提供用户需求（读到空内容）")
	}
	return prompt, nil
}

// resolveCwd 解析引擎工作目录：未指定用当前目录；一律转绝对路径，落 run.json 时无歧义。
func resolveCwd(cwd string) (string, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("获取当前目录失败: %w", err)
		}
		return current, nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("解析 --cwd 路径失败: %w", err)
	}
	// 显式传入时必须是已存在的目录：不存在 / 不是目录 / 无法 stat 都属用法错误（退 2），
	// 让 AI 引擎在无效目录上空跑没有意义。校验逻辑收敛在 run.ValidateWorkingDir（UI 启动预检
	// 与本命令同源复用），这里按哨兵类型还原各自的 --cwd 用法错误文案（退 2）。
	if err := run.ValidateWorkingDir(abs); err != nil {
		switch {
		case errors.Is(err, run.ErrWorkingDirNotExist):
			return "", usageErrorf("--cwd 指向的路径不存在：%s", abs)
		case errors.Is(err, run.ErrWorkingDirNotDir):
			return "", usageErrorf("--cwd 不是目录：%s", abs)
		default:
			return "", usageErrorf("--cwd 路径无法访问：%s（%v）", abs, err)
		}
	}
	return abs, nil
}
