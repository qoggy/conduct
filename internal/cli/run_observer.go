package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/qoggy/conduct/internal/orchestrator"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

// humanObserver 把编排事件渲染成人类可读进度（▶ 展开 / ● 步骤 / ✓✗ 结果），承 spec〈workflow run〉。
type humanObserver struct {
	out io.Writer
}

func (h humanObserver) OnExpand(steps []workflow.ExecutionStep, startIndex int) {
	// startIndex>0 即 resume：打印「从第几步恢复、共剩几步」的恢复头，且只列将实际重跑的剩余步（不列已跳过
	// 的前序步），避免把 resume 误显为整趟 N 步都会跑（承 spec〈run resume〉〈输出〉）。startIndex==0 是整趟
	// workflow run，照旧打印「展开为 N 步」全量清单。
	if startIndex > 0 {
		fmt.Fprintf(h.out, "▶ 从第 %d 步恢复：共 %d 步、跳过前 %d 步，续跑剩余 %d 步：\n",
			startIndex, len(steps), startIndex, len(steps)-startIndex)
	} else {
		fmt.Fprintf(h.out, "▶ 展开为 %d 步：\n", len(steps))
	}
	for index := startIndex; index < len(steps); index++ {
		step := steps[index]
		fmt.Fprintf(h.out, "  [%d] %-9s node=%-10s iter=%d\n", index, step.Type, step.NodeID, step.Iteration)
	}
}

func (h humanObserver) OnStepStart(info orchestrator.StepInfo) {
	model := info.Model
	if model == "" {
		model = "(引擎默认)"
	}
	fmt.Fprintf(h.out, "● step %d [%s] %s iter=%d engine=%s model=%s\n",
		info.StepIndex, info.DisplayName, info.Type, info.Iteration, info.Engine, model)
}

func (h humanObserver) OnStepDone(entry run.TraceEntry) {
	if entry.Success {
		fmt.Fprintf(h.out, "  ✓ %dms tokens=%d 产物 %d 字符：%s\n",
			entry.DurationMs, entry.Tokens, len([]rune(entry.Output)), preview(entry.Output, 80))
		return
	}
	errText := ""
	if entry.Error != nil {
		errText = *entry.Error
	}
	fmt.Fprintf(h.out, "  ✗ %dms 失败：%s\n", entry.DurationMs, preview(errText, 200))
}

// jsonObserver 以 --json 模式逐步吐出事件：每步一行 trace 记录（即 trace.jsonl 的一条）。
// 展开 / 开跑不产生事件（无装饰）。序列化错误暂存，供命令收尾检查（不静默）。
type jsonObserver struct {
	out io.Writer
	err error
}

func (j *jsonObserver) OnExpand([]workflow.ExecutionStep, int) {}
func (j *jsonObserver) OnStepStart(orchestrator.StepInfo)      {}
func (j *jsonObserver) OnStepDone(entry run.TraceEntry) {
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

// preview 把可能多行的文本压成不超过 n 个字符的单行预览（换行折成空格，超出以 … 收尾）。
func preview(text string, n int) string {
	oneLine := strings.Join(strings.Fields(text), " ")
	runes := []rune(oneLine)
	if len(runes) <= n {
		return oneLine
	}
	return string(runes[:n]) + "…"
}
