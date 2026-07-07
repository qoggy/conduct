// Package launch 是 CLI 与 UI 共用的「后台发射器」：以独立会话（setsid）self-exec 出一个
// `conduct workflow run <name> --cwd <dir>` 前台子进程、经 stdin 喂入用户需求，再有界等待子进程
// 落下初始 run.json，把可用 run id 交回调用方。
//
// 被 `conduct ui` 的 HTTP 启动路径与 `conduct workflow run -d` 复用——两者由此得到逐字节同构的
// 后台 run（pid 判活 / interrupted 语义一致）。发射细节每条都有踩坑背书，见
// docs/specs/ui.md〈启动运行机制〉与 docs/specs/cli-runtime.md〈后台运行〉。
package launch

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/qoggy/conduct/internal/run"
)

const (
	matchTimeout = 10 * time.Second       // run id 组合匹配的轮询上限
	pollInterval = 100 * time.Millisecond // 轮询间隔（run.json 开跑即写，通常亚秒命中）
	clockMargin  = 2 * time.Second        // startedAt 与 spawn 时刻的时钟余量（秒精度 + 时钟抖动）
)

// RunLister 是发射器确认子进程 run.json 落定所需的最小 store 能力——列出全部运行记录。
// *store.Store 天然满足。
type RunLister interface {
	ListRuns() ([]*run.Record, []error, error)
}

// Launcher 持有一次后台发射所需的依赖：自身可执行文件路径（self-exec）、运行记录列表（确认落定）、
// 会话私有 stderr 临时目录（兜底子进程写 run.json 前就失败）、可注入时钟（便于测试）。
type Launcher struct {
	exePath   string
	store     RunLister
	stderrDir string
	now       func() time.Time
}

// NewLauncher 构造发射器；now 传 nil 时用 time.Now。
func NewLauncher(exePath string, store RunLister, stderrDir string, now func() time.Time) *Launcher {
	if now == nil {
		now = time.Now
	}
	return &Launcher{exePath: exePath, store: store, stderrDir: stderrDir, now: now}
}

// launchedProcess 关联一个已 Start 的子进程与它的会话私有 stderr 临时文件路径。
type launchedProcess struct {
	cmd        *exec.Cmd
	stderrPath string
}

// Launch 发射一次运行并确认其初始 run.json。三种结果互斥：
//   - runID != ""：已确认，run id 可被 run list / run show 查到；
//   - runID == "" 且 note != "" 且 err == nil：已发射但有界等待内未确认（子进程仍存活），
//     由调用方决定这算不算成功（UI 视作 202 回执，CLI 视作退 1）；
//   - err != nil：spawn 失败，或子进程在写 run.json 前就死了。
//
// absCwd 须为调用方已校验过的绝对工作目录（发射器只管发射，不重做预检）。
func (l *Launcher) Launch(name, userPrompt, absCwd string) (runID, note string, err error) {
	spawnedAt := l.now()
	launched, err := l.spawn(name, userPrompt, absCwd)
	if err != nil {
		return "", "", err
	}
	// 读后即弃：无论成败，最终清掉这份会话私有 stderr（路径绝不外泄）。
	defer func() { _ = os.Remove(launched.stderrPath) }()
	childPid := launched.cmd.Process.Pid
	// 必须回收子进程：Setsid 不改父子关系，不 reap 就是僵尸；僵尸对 kill(pid,0) 探活返回 nil →
	// EffectiveStatus 永报 running → interrupted 永不派生（假活）。退出码由 run.json 终态承载。
	go func() { _ = launched.cmd.Wait() }()

	deadline := spawnedAt.Add(matchTimeout)
	for {
		if records, _, listErr := l.store.ListRuns(); listErr == nil {
			if id, ok := matchRunID(records, name, childPid, spawnedAt); ok {
				return id, "", nil
			}
		}
		if l.now().After(deadline) {
			break
		}
		time.Sleep(pollInterval)
	}
	// 超时未命中：子进程仍在跑 → 不误报失败，引导去运行列表核对；已退出 → 读 stderr 回传原因。
	if run.ProcessAlive(childPid) {
		return "", "已发射运行，但未能在超时内确认 run id（子进程仍在运行）。请到运行列表核对。", nil
	}
	return "", "", fmt.Errorf("运行启动失败：%s", readLaunchStderr(launched.stderrPath))
}

// spawn 起一个分离的 workflow run 子进程。每个决策都对应一处踩坑（见 spec〈启动运行机制〉表）。
func (l *Launcher) spawn(name, userPrompt, absCwd string) (*launchedProcess, error) {
	// 绝不用 CommandContext 绑调用方 ctx——发起方返回即杀 run。用背景 Command。
	cmd := exec.Command(l.exePath, "workflow", "run", name, "--cwd", absCwd)
	// Setsid 起新会话：彻底脱离发起方的会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP 两条路径。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	// stdout → /dev/null：进度已逐步落盘 trace，无需管道。绝不 pipe——发起方先退出会令子进程写 stdout 时
	// EPIPE，Go 运行时对 fd 1/2 写失败重升 SIGPIPE 杀死 run，恰好击穿「发起方退出、run 继续跑」的承诺。
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("打开 /dev/null 失败: %w", err)
	}
	defer devNull.Close() // Start 后子进程已继承 dup，父侧 fd 可关
	cmd.Stdout = devNull
	// stderr → 会话私有临时文件：兜底「CreateRun 之前就失败」（store IO 等罕见路径）的原因回传。
	stderrFile, err := os.CreateTemp(l.stderrDir, "run-*.stderr")
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
	earliest := spawnedAt.Add(-clockMargin)
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
func readLaunchStderr(stderrPath string) string {
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
