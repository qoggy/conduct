// Package run 定义运行记录实体：一次 workflow 执行的不可变历史。
//
// 落盘为三份文件（见 spec〈落盘存储结构〉）：run.json（Record）、trace.jsonl（每行一条
// TraceEntry）、run-summary.md（RenderSummary 渲染）。持久化由 internal/store 负责，本包只管类型
// 与纯逻辑（状态派生、总结渲染）。
package run

import (
	"fmt"
	"regexp"
	"syscall"

	"github.com/qoggy/conduct/internal/workflow"
)

// runIDPattern 限定 run id 的字符集（＝目录名，须无路径分隔符）：run id 形如
// <workflow>-<YYYYMMDD-HHMMSS>，两段都落在 [A-Za-z0-9._-] 内。
var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateID 校验 run id 合法（防路径穿越）：只允许 [A-Za-z0-9._-]，且不为 . / ..。
func ValidateID(id string) error {
	if !runIDPattern.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("run id %q 非法：只允许字母、数字、点、下划线、连字符，且不能是 . 或 ..", id)
	}
	return nil
}

// Status 是运行的持久化状态。interrupted 不落盘、是读时派生态（见 EffectiveStatus）。
type Status string

const (
	StatusRunning     Status = "running"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted" // 派生：status=running 但进程已死
)

// Record 是 run.json 的结构——运行概要 + 开始那一刻冻结的 workflow 快照，使这次运行永远可复现。
// endedAt / error 用指针，未终结时显式序列化为 null（对齐 spec 示例）。
type Record struct {
	ID               string               `json:"id"`
	Workflow         string               `json:"workflow"`
	WorkflowSnapshot *workflow.Definition `json:"workflowSnapshot"`
	UserPrompt       string               `json:"userPrompt"`
	Cwd              string               `json:"cwd"`
	Status           Status               `json:"status"`
	Pid              int                  `json:"pid"`
	PidStartTime     string               `json:"pidStartTime,omitempty"` // 进程启动时刻标识，防 pid 复用误判/误杀；旧记录/不支持平台为空
	Steps            int                  `json:"steps"`
	StartedAt        string               `json:"startedAt"`
	EndedAt          *string              `json:"endedAt"`
	Artifacts        map[string]string    `json:"artifacts"`
	Error            *string              `json:"error"`
}

// TraceEntry 是 trace.jsonl 的一行——单个执行步骤的完整记录（自解释，不依赖当时的定义）。
type TraceEntry struct {
	StepIndex    int                    `json:"stepIndex"`
	Type         string                 `json:"type"` // agent | evaluator
	NodeID       string                 `json:"nodeId"`
	DisplayName  string                 `json:"displayName"`
	Iteration    int                    `json:"iteration"`
	Engine       string                 `json:"engine"`
	EngineConfig *workflow.EngineConfig `json:"engineConfig,omitempty"`
	Input        string                 `json:"input"`
	Success      bool                   `json:"success"`
	Error        *string                `json:"error"`
	Output       string                 `json:"output"`
	Tokens       int                    `json:"tokens,omitempty"`
	SessionID    string                 `json:"sessionId,omitempty"` // 选填：该步引擎的会话/线程 id（引擎回报则记），凭它回放本步
	DurationMs   int64                  `json:"durationMs"`
}

// ProcessStartToken 暴露给编排器在创建运行时捕获**自身**进程的启动时刻标识，随 run.json 落盘；
// 与 processAlive 用同一来源，保证同一进程存续期内比对必然相等。不支持平台/读不到返回 ("", false)。
func ProcessStartToken(pid int) (string, bool) {
	return processStartToken(pid)
}

// ProcessAlive 报告某 pid 的进程是否存活（signal 0 探测：不投递信号、只做存在性/权限检查）。
// ESRCH → 已死；EPERM → 存在但属他人（视为存活）；nil → 存活。
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// EffectiveStatus 返回对外展示的状态：running 且进程已死 → interrupted，其余照 Status。
func (r *Record) EffectiveStatus() Status {
	return deriveStatus(r.Status, r.processAlive())
}

// processAlive 在 pid 存活基础上再校验进程启动时刻，防 pid 被 OS 复用后把无关进程误判为本运行。
// 无 PidStartTime（旧记录/不支持平台）或读不到目标进程启动时刻时，退回纯 pid 判断，不因此误判为死。
func (r *Record) processAlive() bool {
	if !ProcessAlive(r.Pid) {
		return false
	}
	if r.PidStartTime == "" {
		return true
	}
	token, ok := processStartToken(r.Pid)
	if !ok {
		return true
	}
	return token == r.PidStartTime
}

// deriveStatus 是状态派生的纯逻辑（便于单测）：仅当 running 且进程已死才降级为 interrupted。
func deriveStatus(status Status, alive bool) Status {
	if status == StatusRunning && !alive {
		return StatusInterrupted
	}
	return status
}

// ProgressCount 返回进度分子 k = trace 中「唯一 stepIndex 且（最后一次记录）success」的步数。
// 与「数物理行」不同：resume 会保留失败行 + 续写补跑行，同一 stepIndex 有多条，数行数会让 k 越过
// 分母 N（如 11/10）。按 stepIndex 去重、以每步最后一次记录（＝执行序最新）为准，保证 k ≤ N 恒成立。
// 审计视角要看全部历史记录仍走 run show --trace（不去重）。
func ProgressCount(trace []TraceEntry) int {
	lastSuccess := make(map[int]bool, len(trace))
	for _, entry := range trace {
		lastSuccess[entry.StepIndex] = entry.Success // 同一 stepIndex 后写覆盖前写，末条为准
	}
	count := 0
	for _, ok := range lastSuccess {
		if ok {
			count++
		}
	}
	return count
}

// LastUnsuccessfulStepIndex 返回 trace 中最后一条失败记录的 stepIndex。run.json 不保存失败步；需要展示失败位置时
// 从 trace 推断。没有失败记录时返回 nil。
func LastUnsuccessfulStepIndex(trace []TraceEntry) *int {
	for i := len(trace) - 1; i >= 0; i-- {
		if !trace[i].Success {
			stepIndex := trace[i].StepIndex
			return &stepIndex
		}
	}
	return nil
}

// StepLabel 返回一步在报告/列表里的展示名：evaluator 步加「· 评测」后缀，与 agent 步区分
// （否则带 evaluator 的节点会产出多行同名步骤，分不清写与评）。
func (e TraceEntry) StepLabel() string {
	if e.Type == workflow.StepTypeEvaluator {
		return e.DisplayName + " · 评测"
	}
	return e.DisplayName
}
