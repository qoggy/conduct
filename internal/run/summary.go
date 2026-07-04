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
	updated := ""
	if record.WorkflowSnapshot != nil {
		nodeCount = len(record.WorkflowSnapshot.Nodes)
		updated = formatMinute(record.WorkflowSnapshot.UpdatedAt)
	}
	fmt.Fprintf(&b, "**工作流** %s · %d 节点（冻结于 updatedAt %s）\n", record.Workflow, nodeCount, updated)
	fmt.Fprintf(&b, "**需求** %s\n", record.UserPrompt)
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
	b.WriteString("| # | 节点 | 引擎 · 模型 | 结果 | 耗时 |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, entry := range trace {
		result := "✅"
		if !entry.Success {
			result = "❌"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n",
			entry.StepIndex, entry.StepLabel(), engineModel(entry), result, formatDurationMs(entry.DurationMs))
	}

	b.WriteString("\n## 产物\n\n")
	b.WriteString("> 各节点产物多为 Markdown（含标题、代码块），故逐节点用 XML 标签包裹其**完整**产物：" +
		"产物内的标题不会污染本报告大纲、代码围栏也不与外层冲突（产物含字面 `</output>` 时边界才会破，极少见）。" +
		"字节级权威仍是 `trace.jsonl` 的 `output` 字段。\n\n")
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

// engineModel 返回步骤的「引擎 · 模型」展示串；未声明 model 时标「(默认)」。
func engineModel(entry TraceEntry) string {
	model := "(默认)"
	if entry.EngineConfig != nil && entry.EngineConfig.Model != "" {
		model = entry.EngineConfig.Model
	}
	return entry.Engine + " · " + model
}

// formatMinute / formatSecond 把 RFC3339 时间戳格式化为可读形态；解析失败则原样返回（不崩）。
func formatMinute(rfc3339 string) string { return reformat(rfc3339, "2006-01-02 15:04") }
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
