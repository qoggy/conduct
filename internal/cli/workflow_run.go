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
		Use:   localizedHelpText(`run <name> ["<需求>"]`, `run <name> ["<request>"]`),
		Short: localizedHelpText("解释运行一份工作流", "Interpret and run a workflow"),
		Long: localizedHelpText(
			"解释运行名为 <name> 的工作流：按定义逐节点驱动 AI 引擎执行，前台同步阻塞并打印进度，结束后用 conduct run show <id> 看记录。\n"+
				"用户需求经第二个位置参数或 stdin 传入（二者其一必填；均缺且 stdin 是终端则报错退 2，不挂起）。\n"+
				"给引擎看图片：把图片的本地绝对路径直接写进需求文本即可（如「参考 /Users/me/shot.png 的布局」），各引擎自带的文件工具会自行读取该图。\n"+
				"-d / --detach 改为后台起跑，用 run show / run wait / run stop 查看、等待、停止。\n\n"+
				"示例：\n"+
				"  conduct workflow run myflow \"把 README 翻译成英文\"\n"+
				"  echo \"把 README 翻译成英文\" | conduct workflow run myflow\n"+
				"  conduct workflow run myflow \"照 /Users/me/mock.png 实现这个页面\"\n"+
				"  conduct workflow run myflow \"重构结算流程\" -d",
			"Interpret and run the workflow named <name>: drive the AI engine node by node according to the definition, block synchronously in the foreground while printing progress, and use conduct run show <id> afterward to view the record.\n"+
				"Pass the user request as the second positional argument or through stdin (one is required; if both are absent and stdin is a terminal, report an error and exit 2 without hanging).\n"+
				"To show an image to an engine, put the image's absolute local path directly in the request text (for example, \"match the layout in /Users/me/shot.png\"); each engine's built-in file tools will read the image.\n"+
				"-d / --detach starts the run in the background; use run show / run wait / run stop to inspect, wait for, or stop it.\n\n"+
				"Examples:\n"+
				"  conduct workflow run myflow \"Translate README into English\"\n"+
				"  echo \"Translate README into English\" | conduct workflow run myflow\n"+
				"  conduct workflow run myflow \"Implement this page to match /Users/me/mock.png\"\n"+
				"  conduct workflow run myflow \"Refactor the checkout flow\" -d",
		),
		Args: rangeArgs(1, 2),
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
			wf, err := st.Load(name)
			if err != nil {
				return err
			}
			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 载入即校验，防手改损坏
			}

			// 预检全部同步做完（fail-loud），到这里才分道：-d 后台发射，否则前台跑到底。
			if detach {
				return runDetached(cmd, st, name, userPrompt, workingDir, asJSON)
			}

			orch := orchestrator.New(st)
			orch.Language = selectedLanguage
			if asJSON {
				return runWithJSON(cmd, orch, wf, userPrompt, workingDir)
			}
			return runWithHuman(cmd, orch, wf, userPrompt, workingDir, st)
		},
	}
	cmd.Flags().StringVar(&cwd, "cwd", "", localizedHelpText("AI 引擎读写文件的工作目录（默认当前目录），即 {{sys.cwd}}", "Working directory where AI engines read and write files (default: current directory), namely {{sys.cwd}}"))
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText("逐节点输出机器可读事件 JSON（每节点一行），无进度装饰", "Output machine-readable event JSON for each node (one line per node), without progress decoration"))
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, localizedHelpText("后台起跑：打印 run id 后立刻退 0，不阻塞到运行结束", "Start in the background: print the run id and immediately exit 0 without blocking until the run finishes"))
	return cmd
}

// runWithHuman 以人类可读进度运行，收尾指向 run-summary.md。
func runWithHuman(cmd *cobra.Command, orch *orchestrator.Orchestrator, wf *workflow.Workflow,
	userPrompt, workingDir string, st *store.Store) error {
	obs := humanObserver{out: cmd.OutOrStdout()}
	runID, err := orch.Run(cmd.Context(), wf, userPrompt, workingDir, obs)
	if err != nil {
		return err // 编排已落盘 failed trace/summary；此处上抛 → Execute 退 1
	}
	summary, pathErr := st.SummaryPath(runID)
	if pathErr != nil {
		return pathErr
	}
	fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✅ 完成，阅读 %s 获取运行详情。\n", "✅ Completed. Read %s for run details.\n"), summary)
	return nil
}

// runWithJSON 以逐节点事件 JSON 运行（无汇总事件，整体概要见 run.json）。
func runWithJSON(cmd *cobra.Command, orch *orchestrator.Orchestrator, wf *workflow.Workflow,
	userPrompt, workingDir string) error {
	obs := &jsonObserver{out: cmd.OutOrStdout()}
	_, err := orch.Run(cmd.Context(), wf, userPrompt, workingDir, obs)
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
			return "", localizedUsageErrorf("用户需求不能为空", "user request cannot be empty")
		}
		return prompt, nil
	}
	if stdinIsTerminal() {
		return "", localizedUsageErrorf("缺少用户需求：作为第二个参数传入，或经 stdin 管道输入（如 cat req.txt | conduct workflow run <name>）", "missing user request: pass it as the second argument or through stdin (for example, cat req.txt | conduct workflow run <name>)")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read stdin: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", localizedUsageErrorf("stdin 未提供用户需求（读到空内容）", "stdin did not provide a user request (empty content was read)")
	}
	return prompt, nil
}

// resolveCwd 解析引擎工作目录：未指定用当前目录；一律转绝对路径，落 run.json 时无歧义。
func resolveCwd(cwd string) (string, error) {
	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
		return current, nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to resolve --cwd path: %w", err)
	}
	// 显式传入时必须是已存在的目录：不存在 / 不是目录 / 无法 stat 都属用法错误（退 2），
	// 让 AI 引擎在无效目录上空跑没有意义。校验逻辑收敛在 run.ValidateWorkingDir（UI 启动预检
	// 与本命令同源复用），这里按哨兵类型还原各自的 --cwd 用法错误文案（退 2）。
	if err := run.ValidateWorkingDir(abs); err != nil {
		switch {
		case errors.Is(err, run.ErrWorkingDirNotExist):
			return "", &usageError{err: err}
		case errors.Is(err, run.ErrWorkingDirNotDir):
			return "", &usageError{err: err}
		default:
			return "", &usageError{err: err}
		}
	}
	return abs, nil
}
