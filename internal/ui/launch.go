package ui

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// self-exec 发射器：UI 以 os.Executable() 自呼 `conduct workflow run <name> --cwd <dir>` 起子进程，
// 而非进程内跑 orchestrator。这样 pid 判活 / interrupted 语义与终端启动逐字节一致，且关掉 UI
// 不连累在跑的 run。发射细节每条都有踩坑背书，见 docs/specs/ui.md〈启动运行机制〉。

const (
	launchMatchTimeout = 10 * time.Second       // run id 组合匹配的轮询上限
	launchPollInterval = 100 * time.Millisecond // 轮询间隔（run.json 开跑即写，通常亚秒命中）
	launchClockMargin  = 2 * time.Second        // startedAt 与 spawn 时刻的时钟余量（秒精度 + 时钟抖动）
)

// launchError 是发射失败的分类错误：status 决定 HTTP 码，problems 非空时走 422 字段级错误。
type launchError struct {
	status   int
	message  string
	problems []problem
}

func (e *launchError) Error() string { return e.message }

func newLaunchError(status int, format string, a ...any) *launchError {
	return &launchError{status: status, message: fmt.Sprintf(format, a...)}
}

// launchedProcess 关联一个已 Start 的子进程与它的会话私有 stderr 临时文件路径。
type launchedProcess struct {
	cmd        *exec.Cmd
	stderrPath string
}

// launchRun 发射一次运行并返回其 run id。流程：预检（毫秒级，只读）→ spawn 分离子进程 →
// 组合条件轮询匹配 run id。返回的 note 非空表示「已发射但超时未确认（子进程仍在跑）」，非失败。
func (s *Server) launchRun(name, userPrompt, cwd string) (runID, note string, err error) {
	absCwd, err := s.preflight(name, userPrompt, cwd)
	if err != nil {
		return "", "", err
	}
	spawnedAt := s.now()
	launched, err := s.spawn(name, userPrompt, absCwd)
	if err != nil {
		return "", "", newLaunchError(http.StatusInternalServerError, "启动运行子进程失败: %v", err)
	}
	// 读后即弃：无论成败，最终清掉这份会话私有 stderr（路径绝不进任何响应）。
	defer func() { _ = os.Remove(launched.stderrPath) }()
	childPid := launched.cmd.Process.Pid
	// 必须回收子进程：Setsid 不改父子关系，不 reap 就是僵尸；僵尸对 kill(pid,0) 探活返回 nil →
	// EffectiveStatus 永报 running → interrupted 永不派生（假活）。退出码由 run.json 终态承载。
	go func() { _ = launched.cmd.Wait() }()

	deadline := spawnedAt.Add(launchMatchTimeout)
	for {
		if records, _, listErr := s.store.ListRuns(); listErr == nil {
			if id, ok := matchRunID(records, name, childPid, spawnedAt); ok {
				return id, "", nil
			}
		}
		if s.now().After(deadline) {
			break
		}
		time.Sleep(launchPollInterval)
	}
	// 超时未命中：子进程仍在跑 → 不误报失败，引导去运行列表核对；已退出 → 读 stderr 回传原因。
	if run.ProcessAlive(childPid) {
		return "", "已发射运行，但未能在超时内确认 run id（子进程仍在运行）。请到运行列表核对。", nil
	}
	return "", "", newLaunchError(http.StatusInternalServerError, "运行启动失败：%s", s.readLaunchStderr(launched.stderrPath))
}

// preflight 在起子进程前进程内做只读校验，把「workflow 不存在 / 定义损坏 / 需求空 / 目录不存在」
// 从秒级子进程失败缩到毫秒级 400/404/422。真正的权威闸门仍是子进程 workflow run 自身的
// resolveCwd（跑同一份 run.ValidateWorkingDir）。返回绝对化后的工作目录。
func (s *Server) preflight(name, userPrompt, cwd string) (string, error) {
	def, err := s.store.Load(name)
	if err != nil {
		if errors.Is(err, store.ErrNotExist) {
			return "", newLaunchError(http.StatusNotFound, "%s", err.Error())
		}
		return "", newLaunchError(http.StatusBadRequest, "%s", err.Error())
	}
	if problems := workflow.ValidateStructured(def); len(problems) > 0 {
		return "", &launchError{
			status:   http.StatusUnprocessableEntity,
			message:  "工作流定义校验未通过，无法运行",
			problems: problemsFrom(problems),
		}
	}
	if strings.TrimSpace(userPrompt) == "" {
		return "", newLaunchError(http.StatusBadRequest, "缺少用户需求：不能为空")
	}
	// UI 无 shell，不做 ~ 展开、也不把相对路径拼到进程启动目录（那是用户看不见的隐藏基准）。
	// 显式要求绝对路径：非空却不以 / 开头 → 就地报错。留空则用进程启动目录（默认）。
	if cwd != "" && !filepath.IsAbs(cwd) {
		return "", newLaunchError(http.StatusBadRequest, "工作目录必须是绝对路径（以 / 开头）：%s", cwd)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", newLaunchError(http.StatusBadRequest, "解析工作目录失败: %v", err)
	}
	if err := run.ValidateWorkingDir(abs); err != nil {
		return "", newLaunchError(http.StatusBadRequest, "%s", err.Error())
	}
	return abs, nil
}

// spawn 起一个分离的 workflow run 子进程。每个决策都对应一处踩坑（见 ui.md〈启动运行机制〉表）。
func (s *Server) spawn(name, userPrompt, absCwd string) (*launchedProcess, error) {
	// 绝不用 CommandContext 绑 HTTP 请求 ctx——handler 返回即杀 run。用背景 Command。
	cmd := exec.Command(s.exePath, "workflow", "run", name, "--cwd", absCwd)
	// Setsid 起新会话：彻底脱离 UI 的会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP 两条路径。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	// stdout → /dev/null：进度已逐步落盘 trace，无需管道。绝不 pipe——UI 先退出会令子进程写 stdout 时
	// EPIPE，Go 运行时对 fd 1/2 写失败重升 SIGPIPE 杀死 run，恰好击穿「UI 退出 run 继续跑」的承诺。
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("打开 /dev/null 失败: %w", err)
	}
	defer devNull.Close() // Start 后子进程已继承 dup，父侧 fd 可关
	cmd.Stdout = devNull
	// stderr → 会话私有临时文件：兜底「CreateRun 之前就失败」（store IO 等罕见路径）的原因回传。
	stderrFile, err := os.CreateTemp(s.stderrDir, "run-*.stderr")
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("创建 stderr 临时文件失败: %w", err)
	}
	defer stderrFile.Close() // 同上；保留 path 供超时读取
	cmd.Stderr = stderrFile
	stderrPath := stderrFile.Name()

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("启动子进程失败: %w", err)
	}
	// 写入需求并立即 close stdin：子进程 resolveUserPrompt 会 ReadAll 到 EOF，不 close 则它永远读不完
	// → 卡在 CreateRun 之前 → run.json 不写 → 匹配永远失败。stdout 已 /dev/null，写入不会死锁。
	if _, err := io.WriteString(stdin, userPrompt); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("向子进程写入需求失败: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return nil, fmt.Errorf("关闭子进程 stdin 失败: %w", err)
	}
	return &launchedProcess{cmd: cmd, stderrPath: stderrPath}, nil
}

// matchRunID 从运行列表里用组合条件锁定刚发射的那次运行（纯函数，便于单测）：
// workflow 名匹配 && pid == 子进程 pid && startedAt >= spawn−时钟余量。
// 不要求停留在 running——run 一旦 CreateRun（写 run.json、含 pid）就有 id，哪怕引擎秒级失败 / 秒级完成、
// 状态已转终态，它仍是「我们刚发射的这次」，仍要把 id 交回。单靠 pid 不够——历史记录里残留的 pid
// 可能被新进程复用；workflow 名 + 时钟余量（startedAt 须新于 spawn−余量）把这类旧记录滤掉。
func matchRunID(records []*run.Record, name string, childPid int, spawnedAt time.Time) (string, bool) {
	earliest := spawnedAt.Add(-launchClockMargin)
	for _, record := range records {
		if record.Workflow != name || record.Pid != childPid {
			continue
		}
		started, err := time.Parse(time.RFC3339, record.StartedAt)
		if err != nil || started.Before(earliest) {
			continue // 解析失败或早于 spawn−余量 → 旧记录，跳过
		}
		return record.ID, true
	}
	return "", false
}

// readLaunchStderr 读会话私有 stderr 临时文件内容（供超时且子进程已退出时回传失败原因）。
func (s *Server) readLaunchStderr(stderrPath string) string {
	data, err := os.ReadFile(stderrPath)
	if err != nil {
		return fmt.Sprintf("（无法读取启动日志: %v）", err)
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return "（子进程未输出错误信息）"
	}
	return message
}
