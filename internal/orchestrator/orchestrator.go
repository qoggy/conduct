// Package orchestrator 是 conduct 的解释器内核：把一份 workflow 定义按边的依赖并行调度，
// 逐节点驱动 AI 引擎执行，串联上游产物，并把运行记录落盘。
//
// 调度算法（Kahn 拓扑 + 并发、单调度 goroutine 独占共享态、START 预置 done、END no-op、drain 失败语义）
// 见 schedule.go。呈现（人类进度 / --json 事件）经 Observer 外置，内核只管编排与落盘。
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// NodeBrief 是节点的最小标识（id + 展示名），供 Observer 概述初始就绪集。
type NodeBrief struct {
	NodeID      string
	DisplayName string
}

// ScheduleInfo 是一趟运行开跑前的调度概述，供 Observer 打印「调度 N 个节点 / START 扇出 / resume 已完成几个」。
type ScheduleInfo struct {
	AgentNodeCount  int         // 进度分母 N（agent 节点数）
	InitialReady    []NodeBrief // t0 就绪、同刻开跑的节点（START 扇出）
	ResumeDoneCount int         // resume 时已完成的 agent 节点数（fresh run 为 0）
}

// NodeInfo 是一个节点开跑前已知的信息，供 Observer 打印进度（不含产物 / 耗时，那些在 OnNodeDone 的 trace 里）。
type NodeInfo struct {
	NodeID      string
	DisplayName string
	Engine      string
	Model       string // 解析后的模型；空串＝用引擎默认
}

// Observer 接收编排过程中的节点生命周期事件，由调用方（CLI）装成人类进度或 --json 逐节点事件。
type Observer interface {
	OnSchedule(info ScheduleInfo)    // 开跑前的调度概述（含 resume 的已完成数）
	OnNodeStart(info NodeInfo)       // 某节点开跑前
	OnNodeDone(entry run.TraceEntry) // 某节点落定（成功或失败），trace 已落盘
}

// Orchestrator 编排一次运行。Engines / Now 可注入以便测试（默认解析真实引擎与真实时钟）。
type Orchestrator struct {
	Store    *store.Store
	Engines  func(name string) (engine.Engine, error)
	Now      func() time.Time
	Language locale.Language
}

// New 构造一个用真实引擎注册表与真实时钟的 Orchestrator。
func New(st *store.Store) *Orchestrator {
	return &Orchestrator{Store: st, Engines: engine.Lookup, Now: time.Now, Language: locale.English}
}

// Run 解释运行一份（已通过校验的）工作流，返回 run id。按 DAG 依赖并行调度节点（见 schedule.go）：
// 以 START 为唯一前驱者 t0 同刻开跑，其余待全部前驱成功后就绪。任一节点引擎失败走 drain 语义、整体收尾
// failed 并生成 summary，然后返回该错误（调用方据此退 1）；已完成节点的 trace 与产物保留。
func (o *Orchestrator) Run(ctx context.Context, wf *workflow.Workflow, userPrompt, cwd string, obs Observer) (string, error) {
	if !o.Language.Valid() {
		return "", fmt.Errorf("orchestrator has missing or invalid language %q", o.Language)
	}
	def := &wf.Definition
	runID := wf.Name + "-" + o.Now().Format("20060102-150405")
	startedAt := o.Now().Format(time.RFC3339)
	sysVars := map[string]string{"userPrompt": userPrompt, "cwd": cwd, "runId": runID}

	pid := os.Getpid()
	startToken, _ := run.ProcessStartToken(pid) // 落盘进程启动时刻，供读时校验，防 pid 复用误判/误杀
	record := &run.Record{
		ID:               runID,
		Workflow:         wf.Name,
		WorkflowSnapshot: wf,
		UserPrompt:       userPrompt,
		Cwd:              cwd,
		Status:           run.StatusRunning,
		Pid:              pid,
		PidStartTime:     startToken,
		StartedAt:        startedAt,
		Artifacts:        map[string]string{}, // 与调度循环共享：agent 节点成功即覆盖写、增量落盘
		Language:         o.Language,
	}
	if err := o.Store.CreateRun(record); err != nil {
		return "", err
	}

	// 初始 done 只含 START（t0 即"完成"、不执行）；其余节点由调度器按前驱解锁。
	done := map[string]string{workflow.NodeIDStart: ""}
	return runID, o.schedule(ctx, obs, record, def, sysVars, done, cwd)
}

// Resume 从一次未完成运行的中断处续跑到终态，续写同一 runs/<id>/、run id 不变（语义即「恢复这次运行」）。
// 恢复源全部来自落盘：workflowSnapshot（不回读 store 里可能已被 edit/delete 的活 workflow）确定性还原 DAG；
// trace 末条 success 的记录推断已完成集 done、并回放重建 artifacts（{{node-id}} 引用源）。调用方（CLI）已做
// 派生态的 fail-loud 前置校验；本方法再对快照做「非空 + 校验通过」防御——缺失或结构非法（空定义 / 旧格式 /
// 成环）直接报错，绝不喂空 DAG 让调度器空跑一圈就冒充 completed。（注：并发 resume 目前无互斥锁。）
func (o *Orchestrator) Resume(ctx context.Context, record *run.Record, trace []run.TraceEntry, obs Observer) error {
	if err := record.ValidateLanguage(); err != nil {
		return err
	}
	wf := record.WorkflowSnapshot
	if wf == nil {
		return fmt.Errorf("run %s is missing workflowSnapshot and cannot be resumed", record.ID)
	}
	def := &wf.Definition
	// 快照本应在创建时已校验；此处再校一遍，挡住旧格式 / 损坏 / 成环快照——否则空或断连的 DAG 会让调度器
	// 一个节点都不跑就 inflight==0 收尾成 completed，把 failed 运行静默改写成成功（违反「不假装成功」）。
	if err := workflow.Validate(def); err != nil {
		return apperror.Technicalf(err, "workflowSnapshot for run %s failed validation and cannot be resumed: %v", record.ID, err)
	}
	sysVars := map[string]string{"userPrompt": record.UserPrompt, "cwd": record.Cwd, "runId": record.ID}

	// 从 trace 推断已完成集 done（末条 success 的 agent 节点）+ START；artifacts 为 done 去掉 START 的产物。
	done := replayDone(trace)
	done[workflow.NodeIDStart] = ""
	artifacts := make(map[string]string, len(done))
	for id, output := range done {
		if id != workflow.NodeIDStart {
			artifacts[id] = output
		}
	}

	// 状态由 failed/interrupted 改回 running、更新 pid/pidStartTime、清空 endedAt/error/failedNodeId——
	// endedAt 在失败收尾时可能已写入，续跑期间必须复归 null，守住「running 时 endedAt 为 null」的落盘
	// 不变量；failedNodeId 是失败态概要，不能在成功恢复后残留。
	pid := os.Getpid()
	startToken, _ := run.ProcessStartToken(pid)
	record.Status = run.StatusRunning
	record.Pid = pid
	record.PidStartTime = startToken
	record.EndedAt = nil
	record.Error = nil
	record.FailedNodeID = nil
	record.Artifacts = artifacts // 与调度循环共享同一 map
	if err := o.Store.WriteRun(record); err != nil {
		return err
	}
	if err := o.Store.RemoveSummary(record.ID); err != nil {
		return err
	}

	return o.schedule(ctx, obs, record, def, sysVars, done, record.Cwd)
}

// replayDone 回放 trace 重建已完成集：对每个 nodeId 取末条记录（去重、后写覆盖前写），末条 success 者
// 收入 done（nodeId → 产物）。run.json 的 artifacts 亦可，但以 trace 为权威。不含 START（由调用方补）。
func replayDone(trace []run.TraceEntry) map[string]string {
	last := make(map[string]run.TraceEntry, len(trace))
	for _, entry := range trace {
		last[entry.NodeID] = entry
	}
	done := make(map[string]string, len(last))
	for id, entry := range last {
		if entry.Success {
			done[id] = entry.Output
		}
	}
	return done
}

// invokeEngine 解析引擎并执行一个节点；引擎名为空显式报错，不静默跳过。
func (o *Orchestrator) invokeEngine(ctx context.Context, engineName string, config *workflow.EngineConfig, prompt, cwd string) (engine.RunResult, error) {
	if engineName == "" {
		return engine.RunResult{}, fmt.Errorf("internal error: agent node is missing engine")
	}
	eng, err := o.Engines(engineName)
	if err != nil {
		return engine.RunResult{}, err
	}
	request := engine.RunRequest{Prompt: prompt, WorkingDirectory: cwd}
	if config != nil {
		request.Model = config.Model
		// effort / reasoningEffort 二者按引擎互斥，取非空那个交给引擎自解释。
		request.Effort = config.Effort
		if request.Effort == "" {
			request.Effort = config.ReasoningEffort
		}
	}
	return eng.Run(ctx, request)
}

// finalizeCompleted 把 run 收尾为 completed 并生成 summary。
func (o *Orchestrator) finalizeCompleted(record *run.Record) error {
	record.Status = run.StatusCompleted
	return o.finalize(record)
}

// finalizeFailed 把 run 收尾为 failed 并生成 summary，返回原始 cause 上抛（调用方据此退 1）。
// record.Error（失败摘要，run.json 的快速排查缓存）取首个失败节点的 error（firstEngineErr，已自带节点名）、
// record.FailedNodeID 记其节点 id 供 summary / UI 复用；若失败纯因 IO（无节点失败）则 error 退回 cause 文案、
// FailedNodeID 为 nil。
func (o *Orchestrator) finalizeFailed(record *run.Record, firstEngineErr, firstFailedNodeID *string, cause error) error {
	record.Status = run.StatusFailed
	record.FailedNodeID = firstFailedNodeID
	if firstEngineErr != nil {
		record.Error = firstEngineErr
	} else if cause != nil {
		message := cause.Error()
		record.Error = &message
	}
	if err := o.finalize(record); err != nil {
		return errors.Join(cause, err) // 收尾落盘也失败：两个错误都不丢
	}
	return cause
}

// finalize 写终态 run.json + run-summary.md（endedAt 重戳）。
func (o *Orchestrator) finalize(record *run.Record) error {
	ended := o.Now().Format(time.RFC3339)
	record.EndedAt = &ended
	if err := o.Store.WriteRun(record); err != nil {
		return err
	}
	trace, err := o.Store.LoadTrace(record.ID)
	if err != nil {
		return err
	}
	return o.Store.WriteSummary(record.ID, run.RenderSummary(record, trace))
}
