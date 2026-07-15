package run

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/qoggy/conduct/internal/locale"
)

// RenderSummary 把一次运行渲染成 run-summary.md（给人 / AI 阅读的报告，机器读 run.json）。
// 结构见 spec〈runs/ 落盘结构〉：头部 + 需求 + 状态耗时 + 工作目录 + 节点表（按 startedAt 排序）+
// XML 包裹的逐 agent 节点产物。trace 提供逐节点结果，record.Artifacts 提供各节点最终产物。
func RenderSummary(record *Record, trace []TraceEntry) string {
	var b strings.Builder
	language := record.Language

	fmt.Fprintf(&b, "# %s\n\n", record.ID)

	nodeCount := 0
	if record.WorkflowSnapshot != nil {
		nodeCount = record.WorkflowSnapshot.Definition.AgentNodeCount()
	}
	fmt.Fprintf(&b, language.Select("**工作流** %s · %d 节点\n", "**Workflow** %s · %d nodes\n"), record.Workflow, nodeCount)
	fmt.Fprintf(&b, language.Select("**需求** %s\n", "**Request** %s\n"), summarizePrompt(record.UserPrompt, language))
	fmt.Fprintf(&b, language.Select("**状态** %s\n", "**Status** %s\n"), statusLine(record, language))
	fmt.Fprintf(&b, language.Select("**工作目录** %s\n", "**Working directory** %s\n"), record.Cwd)
	if record.Status == StatusFailed {
		if record.FailedNodeID != nil {
			fmt.Fprintf(&b, language.Select("**失败节点** %s\n", "**Failed node** %s\n"), *record.FailedNodeID)
		}
		if record.Error != nil {
			fmt.Fprintf(&b, language.Select("**错误** %s\n", "**Error** %s\n"), *record.Error)
		}
	}

	// 节点表：按 NodeID 去重取末条（resume 保留旧失败行 + 补跑行时收敛为每节点一行），再按 startedAt 排序
	// 还原时间线（并行下 trace 追加序 = 完成序、不定）。
	nodes := lastPerNode(trace)
	b.WriteString(language.Select("\n## 节点\n\n", "\n## Nodes\n\n"))
	b.WriteString(language.Select("| 节点 | 引擎 | 起 → 止 | 耗时 |\n", "| Node | Engine | Start → End | Duration |\n"))
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, entry := range nodes {
		fmt.Fprintf(&b, "| %s | %s | %s → %s | %s |\n",
			entry.DisplayName, entry.Engine, formatSecond(entry.StartedAt), formatSecond(entry.EndedAt),
			formatDurationMs(entry.DurationMs))
	}
	b.WriteString(language.Select("\n## 产物\n\n", "\n## Artifacts\n\n"))
	// 按快照节点顺序输出，稳定且与定义一致；仅输出有产物的 agent 节点。
	if record.WorkflowSnapshot != nil {
		for _, node := range record.WorkflowSnapshot.Definition.Nodes {
			if !node.IsAgent() {
				continue
			}
			output, ok := record.Artifacts[node.ID]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "<output node=%q name=%q>\n%s\n</output>\n\n", node.ID, node.DisplayName, output)
		}
	}
	return b.String()
}

// lastPerNode 按 NodeID 去重取末条（同一节点以最后一次记录为准，语义同 ProgressCount），再按 startedAt 升序
// 返回（并列时按 NodeID 兜底稳定）。收敛 resume 后同一 NodeID 的多条记录为每节点一行；非 resume 运行每个
// NodeID 本就只出现一次。
func lastPerNode(trace []TraceEntry) []TraceEntry {
	last := make(map[string]TraceEntry, len(trace))
	for _, entry := range trace {
		last[entry.NodeID] = entry
	}
	result := make([]TraceEntry, 0, len(last))
	for _, entry := range last {
		result = append(result, entry)
	}
	sort.SliceStable(result, func(i, j int) bool { return TraceOrderLess(result[i], result[j]) })
	return result
}

// TraceOrderLess 是 trace 时间线排序的统一比较：先按 StartedAt 的真实时刻（解析 RFC3339 再比，避免裸字典序
// 在混合时区偏移下失真——resume 跨时区 / 夏令时可能让同一次运行的时间戳混入不同偏移），同刻再按 NodeID 兜底
// 稳定。run-summary 节点表与 run show --trace 共用它，排序口径一致。
func TraceOrderLess(a, b TraceEntry) bool {
	ta, tb := parseRFC3339(a.StartedAt), parseRFC3339(b.StartedAt)
	if !ta.Equal(tb) {
		return ta.Before(tb)
	}
	return a.NodeID < b.NodeID
}

// parseRFC3339 解析 RFC3339 时刻；时间戳恒由编排器以 RFC3339 写出，万一解析失败退零值时刻（排最前）、不崩。
func parseRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// summarizePrompt 把用户需求压成头部一行摘要：取首行、按字数截断，超出 / 多行则以 … 收尾并注明全文在 run.json。
// run-summary.md 是「给人读的那副面孔」，需求可能是整份 PRD（数十 KB），整段塞进头部会淹掉步骤表与产物；
// 故此处只留一行摘要，全文由 run.json 的 userPrompt 保留——与 run list 人读截断、机读留全文的同一分工。
func summarizePrompt(prompt string, language locale.Language) string {
	const maxRunes = 80
	full := strings.TrimSpace(prompt)
	line := full
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if runes := []rune(line); len(runes) > maxRunes {
		line = string(runes[:maxRunes])
	}
	if line == full {
		return line // 需求本就是未超长的单行，原样呈现
	}
	return line + language.Select("…（完整需求见 run.json）", "… (see run.json for the complete request)")
}

// statusLine 渲染「状态」行：图标 + 状态词 +（终结时）耗时与起止时刻。
func statusLine(record *Record, language locale.Language) string {
	icon := map[Status]string{
		StatusRunning: "⏳", StatusCompleted: "✅", StatusFailed: "❌", StatusInterrupted: "⚠️",
	}[record.Status]
	if record.EndedAt == nil || *record.EndedAt == "" {
		return fmt.Sprintf("%s %s", icon, record.Status)
	}
	format := language.Select("%s %s · %s（%s → %s）", "%s %s · %s (%s → %s)")
	return fmt.Sprintf(format, icon, record.Status,
		formatElapsed(record.StartedAt, *record.EndedAt),
		formatSecond(record.StartedAt), formatSecond(*record.EndedAt))
}

// formatSecond 把 RFC3339 时间戳格式化为可读形态；解析失败则原样返回（不崩）。
func formatSecond(rfc3339 string) string { return reformat(rfc3339, "2006-01-02 15:04:05") }

func reformat(rfc3339, layout string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Format(layout)
}

// formatElapsed 返回 start→end 的耗时（如 "18.3s"）；任一解析失败则返回空占位。
func formatElapsed(start, end string) string {
	startTime, err1 := time.Parse(time.RFC3339, start)
	endTime, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return "?"
	}
	return formatDurationMs(endTime.Sub(startTime).Milliseconds())
}

// formatDurationMs 把毫秒渲染成一位小数的秒（如 8021 → "8.0s"）。
func formatDurationMs(ms int64) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
