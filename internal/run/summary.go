package run

import (
	"fmt"
	"strings"
	"time"
)

// RenderSummary 把一次运行渲染成 run-summary.md（给人 / AI 阅读的报告，机器读 run.json）。
// 结构见 spec〈落盘存储结构〉：头部 + 需求 + 状态耗时 + 工作目录 + 步骤表 + XML 包裹的逐节点产物。
// trace 提供逐步结果，record.Artifacts 提供各节点最终产物。
func RenderSummary(record *Record, trace []TraceEntry) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", record.ID)

	nodeCount := 0
	if record.WorkflowSnapshot != nil {
		nodeCount = len(record.WorkflowSnapshot.Nodes)
	}
	fmt.Fprintf(&b, "**工作流** %s · %d 节点\n", record.Workflow, nodeCount)
	fmt.Fprintf(&b, "**需求** %s\n", summarizePrompt(record.UserPrompt))
	fmt.Fprintf(&b, "**状态** %s\n", statusLine(record))
	fmt.Fprintf(&b, "**工作目录** %s\n", record.Cwd)
	if record.Status == StatusFailed {
		if record.FailedStep != nil {
			fmt.Fprintf(&b, "**失败步** step %d\n", *record.FailedStep)
		}
		if record.Error != nil {
			fmt.Fprintf(&b, "**错误** %s\n", *record.Error)
		}
	}

	b.WriteString("\n## 步骤\n\n")
	b.WriteString("| # | 节点 | 引擎 | 耗时 |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, entry := range trace {
		fmt.Fprintf(&b, "| %d | %s | %s | %s |\n",
			entry.StepIndex, entry.StepLabel(), entry.Engine, formatDurationMs(entry.DurationMs))
	}

	b.WriteString("\n## 产物\n\n")
	// 按快照节点顺序输出，稳定且与定义一致；仅输出有产物的节点。
	if record.WorkflowSnapshot != nil {
		for _, node := range record.WorkflowSnapshot.Nodes {
			output, ok := record.Artifacts[node.ID]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "<output node=%q name=%q>\n%s\n</output>\n\n", node.ID, node.DisplayName, output)
		}
	}
	return b.String()
}

// summarizePrompt 把用户需求压成头部一行摘要：取首行、按字数截断，超出 / 多行则以 … 收尾并注明全文在 run.json。
// run-summary.md 是「给人读的那副面孔」，需求可能是整份 PRD（数十 KB），整段塞进头部会淹掉步骤表与产物；
// 故此处只留一行摘要，全文由 run.json 的 userPrompt 保留——与 run list 人读截断、机读留全文的同一分工。
func summarizePrompt(prompt string) string {
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
	return line + "…（完整需求见 run.json）"
}

// statusLine 渲染「状态」行：图标 + 状态词 +（终结时）耗时与起止时刻。
func statusLine(record *Record) string {
	icon := map[Status]string{
		StatusRunning: "⏳", StatusCompleted: "✅", StatusFailed: "❌", StatusInterrupted: "⚠️",
	}[record.Status]
	if record.EndedAt == nil || *record.EndedAt == "" {
		return fmt.Sprintf("%s %s", icon, record.Status)
	}
	return fmt.Sprintf("%s %s · %s（%s → %s）", icon, record.Status,
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
