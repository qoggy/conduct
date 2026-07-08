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

// RunStore 是发射器确认子进程落定所需的最小 store 能力：workflow run 靠 ListRuns 组合匹配刚发射的
// run（matchRunID）；run resume 续写原 run、run id 即入参，靠 LoadRun 确认子进程已接管（pid 更新）。
// *store.Store 天然满足。
type RunStore interface {
	ListRuns() ([]*run.Record, []error, error)
	LoadRun(id string) (*run.Record, error)
}

// Launcher 持有一次后台发射所需的依赖：自身可执行文件路径（self-exec）、运行记录 store（确认落定）、
// 会话私有 stderr 临时目录（兜底子进程写 run.json 前就失败）、可注入时钟（便于测试）。
type Launcher struct {
	exePath   string
	store     RunStore
	stderrDir string
	now       func() time.Time
}

// NewLauncher 构造发射器；now 传 nil 时用 time.Now。
func NewLauncher(exePath string, store RunStore, stderrDir string, now func() time.Time) *Launcher {
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
	// 位置参数 name 用 `--` 与旗标隔离：workflow 名字符集允许前导连字符（见 workflow.ValidateName），`-` 开头的
	// 名字若不隔离会被子进程 cobra 误当旗标解析。旗标（--cwd）须在 `--` 前，其后一律按位置参数解析。
	launched, err := l.spawn([]string{"workflow", "run", "--cwd", absCwd, "--", name}, &userPrompt)
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

// LaunchResume 后台恢复一次 failed / interrupted 运行：spawn 一个前台 `conduct run resume <id>` 子进程（不递归 -d），
// 有界等待其接管——因续写原 run，run id 即入参 <id>、无需 matchRunID 轮询，改以「run.json 的 pid 被
// 改成子进程 pid」判定子进程已接管（resume 重建串联态后把 status 改回 running、更新 pid 即落此签名）。
// 三态返回与 Launch 一致（见其文档）。调用方须已做「派生态为 failed / interrupted」的前置校验。
func (l *Launcher) LaunchResume(id string) (runID, note string, err error) {
	spawnedAt := l.now()
	// `--` 隔离位置参数 id：run id 形如 <workflow>-<时间戳>，workflow 名允许前导连字符，`-` 开头的 id 若不
	// 隔离会被子进程 cobra 误当旗标解析（与上面 workflow run 发射器同理）。resume 不读 stdin（需求沿用 run.json）。
	launched, err := l.spawn([]string{"run", "resume", "--", id}, nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = os.Remove(launched.stderrPath) }()
	childPid := launched.cmd.Process.Pid
	go func() { _ = launched.cmd.Wait() }() // 同 Launch：必须 reap，否则僵尸假活

	deadline := spawnedAt.Add(matchTimeout)
	for {
		if record, loadErr := l.store.LoadRun(id); loadErr == nil && resumeTaken(record, childPid) {
			return id, "", nil
		}
		if l.now().After(deadline) {
			break
		}
		time.Sleep(pollInterval)
	}
	if run.ProcessAlive(childPid) {
		return "", "已发射恢复，但未能在超时内确认子进程接管（子进程仍在运行）。请到运行列表核对。", nil
	}
	return "", "", fmt.Errorf("恢复启动失败：%s", readLaunchStderr(launched.stderrPath))
}

// spawn 起一个分离的 conduct 子进程执行 args（跟在 exe 之后）。stdinData 非 nil 时经 stdin 管道写入后
// 关闭（workflow run 喂需求）；nil 时不设 cmd.Stdin，Go 默认把子进程 stdin 接 /dev/null（run resume 不
// 读 stdin，需求沿用 run.json）。每个决策都对应一处踩坑（见 spec〈启动运行机制〉表）。
func (l *Launcher) spawn(args []string, stdinData *string) (*launchedProcess, error) {
	// 绝不用 CommandContext 绑调用方 ctx——发起方返回即杀 run。用背景 Command。
	cmd := exec.Command(l.exePath, args...)
	// Setsid 起新会话：彻底脱离发起方的会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP 两条路径。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	var stdin io.WriteCloser
	if stdinData != nil {
		pipe, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("创建 stdin 管道失败: %w", err)
		}
		stdin = pipe
	}
	closeStdin := func() {
		if stdin != nil {
			_ = stdin.Close()
		}
	}
	// stdout → /dev/null：进度已逐步落盘 trace，无需管道。绝不 pipe——发起方先退出会令子进程写 stdout 时
	// EPIPE，Go 运行时对 fd 1/2 写失败重升 SIGPIPE 杀死 run，恰好击穿「发起方退出、run 继续跑」的承诺。
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		closeStdin()
		return nil, fmt.Errorf("打开 /dev/null 失败: %w", err)
	}
	defer devNull.Close() // Start 后子进程已继承 dup，父侧 fd 可关
	cmd.Stdout = devNull
	// stderr → 会话私有临时文件：兜底「CreateRun 之前就失败」（store IO 等罕见路径）的原因回传。
	stderrFile, err := os.CreateTemp(l.stderrDir, "run-*.stderr")
	if err != nil {
		closeStdin()
		return nil, fmt.Errorf("创建 stderr 临时文件失败: %w", err)
	}
	defer stderrFile.Close() // 同上；保留 path 供超时读取
	cmd.Stderr = stderrFile
	stderrPath := stderrFile.Name()

	if err := cmd.Start(); err != nil {
		closeStdin()
		return nil, fmt.Errorf("启动子进程失败: %w", err)
	}
	if stdinData != nil {
		// 写入需求并立即 close stdin：子进程 resolveUserPrompt 会 ReadAll 到 EOF，不 close 则它永远读不完
		// → 卡在 CreateRun 之前 → run.json 不写 → 匹配永远失败。stdout 已 /dev/null，写入不会死锁。
		if _, err := io.WriteString(stdin, *stdinData); err != nil {
			closeStdin()
			return nil, fmt.Errorf("向子进程写入需求失败: %w", err)
		}
		if err := stdin.Close(); err != nil {
			return nil, fmt.Errorf("关闭子进程 stdin 失败: %w", err)
		}
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

// resumeTaken 判定子进程是否已接管这次续跑（纯函数，便于单测）：run.json 的 pid 已被改成子进程 pid——原 run
// 的旧 pid 早已死、必不等于 childPid。不要求停留在 running（子进程可能秒级再次失败已转终态，但只要 pid 已是
// 它，就是它接管了这次续跑），口径同 matchRunID 不强求 running。
func resumeTaken(record *run.Record, childPid int) bool {
	return record != nil && record.Pid == childPid
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
