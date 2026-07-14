package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qoggy/conduct/internal/launch"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

// detachHandle 是 -d --json 吐的单行句柄（机读 handle）——纯粹的「可寻址句柄」，只给 id + workflow，
// 不带状态：句柄产出的那一刻 run 未必仍是 running（引擎可能已秒级失败 / 完成），塞一个恒为 running 的
// 字段只会误导；run 的真实成败用 conduct run wait <id> / run show <id> 查。它也不是前台 --json 的
// 逐节点事件流；节点事件在 trace.jsonl，用 conduct run show <id> --json --trace 取。
type detachHandle struct {
	ID       string `json:"id"`
	Workflow string `json:"workflow"`
}

// detachLauncher 是 runDetached / runResumeDetached 消费的最小发射器能力：发射一次后台运行 /
// 恢复并确认子进程落定，返回三态 (runID, note, err)。*launch.Launcher 满足它；抽成接口是为了让
// runDetachedWith 注入假实现、单测「发射失败 / 未确认 / 成功出句柄」三条路径，而不真 self-exec。
type detachLauncher interface {
	Launch(name, userPrompt, absCwd string) (runID, note string, err error)
	LaunchResume(id string) (runID, note string, err error)
}

// runDetached 后台起跑：预检已在调用方同步做完（fail-loud），此处只负责发射。以独立会话 spawn 一个
// 普通前台子 run（不递归 -d），有界等待其写下初始 run.json 后打印 run id 退 0。
//
// 强约定「退 0 ⟺ stdout 已打印可用 run id」：确认不了（有界等待超时，无论子进程是否仍存活）一律退 1，
// 并在 stderr 提示去 run list 核对——机读 `… -d --json | jq -r .id` 不会拿到空 id 却见「成功」。
// -d 的退出码只表达发射成败，不表达 run 跑得成不成功（run 成败去 run show / run wait 看）。
func runDetached(cmd *cobra.Command, st *store.Store, name, userPrompt, workingDir string, asJSON bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("解析自身可执行文件路径失败: %w", err)
	}
	// 会话私有 stderr 临时目录：兜底「子进程写 run.json 之前就失败」的罕见路径，读后即弃、整目录清掉。
	stderrDir, err := os.MkdirTemp("", "conduct-detach-")
	if err != nil {
		return fmt.Errorf("创建启动日志临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(stderrDir) }()

	launcher := launch.NewLauncher(exePath, st, stderrDir, nil)
	return runDetachedWith(cmd, launcher, name, userPrompt, workingDir, asJSON)
}

// runDetachedWith 消费发射器返回的三态 (runID, note, err) 并映射为退出码与输出。与外层 runDetached 的
// os.Executable / 临时目录准备分离，便于注入假发射器单测三条路径（发射失败 / 未确认 / 成功出句柄）。
func runDetachedWith(cmd *cobra.Command, launcher detachLauncher, name, userPrompt, workingDir string, asJSON bool) error {
	runID, note, err := launcher.Launch(name, userPrompt, workingDir)
	return emitDetach(cmd, runID, note, err, name, asJSON, func(id string) string {
		return fmt.Sprintf("已在后台启动 %s；conduct run show %s 查看进度、conduct run stop %s 终止。", id, id, id)
	})
}

// emitDetach 把发射器的三态 (runID, note, err) 映射为退出码与输出，供 workflow run -d 与 run resume -d
// 共用。强约定「退 0 ⟺ stdout 已打印可用 run id」：发射失败 / 未确认一律退 1，绝不退 0 却给不出句柄。
// -d 的退出码只表达发射成败，不表达 run 跑得成不成功（run 成败去 run show / run wait 看）。
// workflowName 供 --json 单行句柄；humanLine 生成人读成功提示（入参为已确认的 run id）。
func emitDetach(cmd *cobra.Command, runID, note string, err error, workflowName string, asJSON bool, humanLine func(id string) string) error {
	if err != nil {
		// 发射失败（spawn / setsid / 子进程夭折）→ 退 1。直接透传发射器的错误：它已自解释
		//（「启动子进程失败: …」/「运行启动失败：<stderr>」），再包一层只会叠出重复的「失败」。
		return err
	}
	if runID == "" {
		// 有界等待内未确认 run id：子进程可能仍在跑——不退 0（给不出句柄就不算发射成功），引导核对。
		return fmt.Errorf("%s", note)
	}
	if asJSON {
		// 单行句柄（机读 handle），非前台 --json 的逐节点事件流——故 compact 而非缩进。
		line, err := json.Marshal(detachHandle{ID: runID, Workflow: workflowName})
		if err != nil {
			return fmt.Errorf("序列化句柄 JSON 失败: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(line))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), humanLine(runID))
	return nil
}
