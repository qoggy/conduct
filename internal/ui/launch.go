package ui

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/qoggy/conduct/internal/launch"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// self-exec 发射器：UI 以 os.Executable() 自呼 `conduct workflow run <name> --cwd <dir>` 起子进程，
// 而非进程内跑 orchestrator。这样 pid 判活 / interrupted 语义与终端启动逐字节一致，且关掉 UI
// 不连累在跑的 run。spawn / 组合匹配 / 有界等待等发射细节已抽到 internal/launch 与 CLI `-d` 共用；
// 本文件只保留 UI 特有的 HTTP 味 preflight（400/404/422 分类）与错误映射。

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

// launchRun 发射一次运行并返回其 run id：先做 UI 的只读 preflight（毫秒级 400/404/422），
// 再交给共用发射器 spawn + 有界等待。返回的 note 非空表示「已发射但超时未确认（子进程仍在跑）」，非失败。
func (s *Server) launchRun(name, userPrompt, cwd string) (runID, note string, err error) {
	absCwd, err := s.preflight(name, userPrompt, cwd)
	if err != nil {
		return "", "", err
	}
	launcher := launch.NewLauncher(s.exePath, s.store, s.stderrDir, s.now)
	runID, note, err = launcher.Launch(name, userPrompt, absCwd)
	if err != nil {
		// 发射器的失败（spawn 失败 / 子进程写 run.json 前就死）对 UI 一律是 500。
		return "", "", newLaunchError(http.StatusInternalServerError, "%s", err.Error())
	}
	return runID, note, nil
}

// resumeRun 从中断处恢复一次运行并返回其 run id（即原 id，续写原 run）：调用方（handleResumeRun）已做
// 「派生态为 failed / interrupted」的 409 前置校验，此处只负责 spawn。复用共用发射器的 LaunchResume
// ——因续写原 run、run id 即入参，无需 workflow run 那套轮询匹配。返回的 note 非空表示「已发射但超时未
// 确认子进程接管（子进程仍在跑）」，非失败。发射器的失败对 UI 一律是 500。
func (s *Server) resumeRun(id string) (runID, note string, err error) {
	launcher := launch.NewLauncher(s.exePath, s.store, s.stderrDir, s.now)
	runID, note, err = launcher.LaunchResume(id)
	if err != nil {
		return "", "", newLaunchError(http.StatusInternalServerError, "%s", err.Error())
	}
	return runID, note, nil
}

// preflight 在起子进程前进程内做只读校验，把「workflow 不存在 / 定义损坏 / 需求空 / 目录不存在」
// 从秒级子进程失败缩到毫秒级 400/404/422。真正的权威闸门仍是子进程 workflow run 自身的
// resolveCwd（跑同一份 run.ValidateWorkingDir）。返回绝对化后的工作目录。
func (s *Server) preflight(name, userPrompt, cwd string) (string, error) {
	wf, err := s.store.Load(name)
	if err != nil {
		if errors.Is(err, store.ErrNotExist) {
			return "", newLaunchError(http.StatusNotFound, "%s", err.Error())
		}
		return "", newLaunchError(http.StatusBadRequest, "%s", err.Error())
	}
	if problems := workflow.ValidateStructured(&wf.Definition); len(problems) > 0 {
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
