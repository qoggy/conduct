package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/qoggy/conduct/internal/orchestrator"
	"github.com/qoggy/conduct/internal/run"
)

// humanObserver 把编排事件渲染成人类可读的**事件流**（逐行滚动打印节点生命周期），承 spec〈workflow run〉。
// 并行节点的事件交错呈现——▶ 开跑 / ✓ 完成 / ✗ 失败。
type humanObserver struct {
	out io.Writer
}

func (h humanObserver) OnSchedule(info orchestrator.ScheduleInfo) {
	if info.ResumeDoneCount > 0 {
		// resume：先报已完成几个、待续几个，再列 t0 就绪节点。
		fmt.Fprintf(h.out, "▶ 从中断恢复：共 %d 个节点、已完成 %d 个，续跑 %s\n",
			info.AgentNodeCount, info.ResumeDoneCount, briefList(info.InitialReady))
		return
	}
	fmt.Fprintf(h.out, "▶ 调度 %d 个节点（START 扇出：%s 同刻开跑）\n",
		info.AgentNodeCount, briefList(info.InitialReady))
}

func (h humanObserver) OnNodeStart(info orchestrator.NodeInfo) {
	fmt.Fprintf(h.out, "▶ %s [%s] 开跑 · engine=%s\n", info.NodeID, info.DisplayName, info.Engine)
}

func (h humanObserver) OnNodeDone(entry run.TraceEntry) {
	if entry.Success {
		fmt.Fprintf(h.out, "✓ %s 完成 · %s · tokens=%d · 产物 %d 字符：%s\n",
			entry.NodeID, formatDurationMs(entry.DurationMs), entry.Tokens,
			len([]rune(entry.Output)), preview(entry.Output, 80))
		return
	}
	errText := ""
	if entry.Error != nil {
		errText = *entry.Error
	}
	fmt.Fprintf(h.out, "✗ %s 失败 · %s · %s\n", entry.NodeID, formatDurationMs(entry.DurationMs), preview(errText, 200))
}

// briefList 把初始就绪节点渲染为「id、id」串（无就绪节点时给占位）。
func briefList(briefs []orchestrator.NodeBrief) string {
	if len(briefs) == 0 {
		return "（无）"
	}
	ids := make([]string, len(briefs))
	for i, brief := range briefs {
		ids[i] = brief.NodeID
	}
	return strings.Join(ids, "、")
}

// jsonObserver 以 --json 模式逐节点吐出事件：每节点落定一行 trace 记录（即 trace.jsonl 的一条）。
// 调度概述 / 开跑不产生事件（无装饰）。序列化错误暂存，供命令收尾检查（不静默）。
type jsonObserver struct {
	out io.Writer
	err error
}

func (j *jsonObserver) OnSchedule(orchestrator.ScheduleInfo) {}
func (j *jsonObserver) OnNodeStart(orchestrator.NodeInfo)    {}
func (j *jsonObserver) OnNodeDone(entry run.TraceEntry) {
	if j.err != nil {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		j.err = fmt.Errorf("序列化 trace 事件失败: %w", err)
		return
	}
	fmt.Fprintln(j.out, string(line))
}

// formatDurationMs 把毫秒渲染成一位小数的秒（如 8021 → "8.0s"）。
func formatDurationMs(ms int64) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// preview 把可能多行的文本压成不超过 n 个字符的单行预览（换行折成空格，超出以 … 收尾）。
func preview(text string, n int) string {
	oneLine := strings.Join(strings.Fields(text), " ")
	runes := []rune(oneLine)
	if len(runes) <= n {
		return oneLine
	}
	return string(runes[:n]) + "…"
}
