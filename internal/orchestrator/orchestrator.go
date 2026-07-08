// Package orchestrator 是 conduct 的解释器内核：把一份 workflow 定义展开成确定性步骤，
// 逐步驱动 AI 引擎执行，串联上游产物与评测反馈，并把运行记录落盘。
//
// 移植自 Python 原型 paw_workflow.py:run_workflow（其祖本是 x-one-web 的 TS orchestrator）。
// 呈现（人类进度 / --json 事件）经 Observer 外置，内核只管编排与落盘。
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// StepInfo 是一步开跑前已知的信息，供 Observer 打印进度（不含产物 / 耗时，那些在 OnStepDone 的 trace 里）。
type StepInfo struct {
	StepIndex   int
	Type        string // agent | evaluator
	NodeID      string
	DisplayName string
	Iteration   int
	Engine      string
	Model       string // 解析后的模型；空串＝用引擎默认
}

// Observer 接收编排过程中的事件，由调用方（CLI）装成人类进度或 --json 逐步事件。
type Observer interface {
	// OnExpand 报告展开后的总步数与清单；startIndex 为本次实际开跑的起始下标（Run 恒为 0，Resume 为
	// trace 推断出的重入点），供人类进度区分「整趟展开」与「从中断处恢复、共剩几步」，避免把 resume
	// 误显为整趟 N 步。
	OnExpand(steps []workflow.ExecutionStep, startIndex int)
	OnStepStart(info StepInfo)       // 某步开跑前
	OnStepDone(entry run.TraceEntry) // 某步落定（成功或失败），trace 已落盘
}

// Orchestrator 编排一次运行。Engines / Now 可注入以便测试（默认解析真实引擎与真实时钟）。
type Orchestrator struct {
	Store   *store.Store
	Engines func(name string) (engine.Engine, error)
	Now     func() time.Time
}

// New 构造一个用真实引擎注册表与真实时钟的 Orchestrator。
func New(st *store.Store) *Orchestrator {
	return &Orchestrator{Store: st, Engines: engine.Lookup, Now: time.Now}
}

// Run 解释运行一份（已通过校验的）定义，返回 run id。任一步引擎失败：写失败 trace、把 run 收尾为
// failed 并生成 summary，然后返回该错误（调用方据此退 1）；已完成步骤的 trace 保留。
func (o *Orchestrator) Run(ctx context.Context, def *workflow.Definition, userPrompt, cwd string, obs Observer) (string, error) {
	nodeByID, validNodeID := indexNodes(def.Nodes)
	steps := workflow.Expand(def.Nodes)

	runID := def.Name + "-" + o.Now().Format("20060102-150405")
	startedAt := o.Now().Format(time.RFC3339)
	artifacts := map[string]string{} // nodeId → 最近成功 agent 产物（覆盖写）
	feedback := map[string]string{}  // nodeId → 最近 evaluator 反馈（喂该节点下一次 agent）
	sysVars := map[string]string{"userPrompt": userPrompt, "cwd": cwd}

	pid := os.Getpid()
	startToken, _ := run.ProcessStartToken(pid) // 落盘进程启动时刻，供读时校验，防 pid 复用误判/误杀
	record := &run.Record{
		ID:               runID,
		Workflow:         def.Name,
		WorkflowSnapshot: def,
		UserPrompt:       userPrompt,
		Cwd:              cwd,
		Status:           run.StatusRunning,
		Pid:              pid,
		PidStartTime:     startToken,
		Steps:            len(steps),
		StartedAt:        startedAt,
		Artifacts:        artifacts, // 与循环内 artifacts 共享同一 map：agent 步覆盖写即对 record 可见
	}
	if err := o.Store.CreateRun(record); err != nil {
		return "", err
	}
	obs.OnExpand(steps, 0)

	return runID, o.runSteps(ctx, obs, record, steps, nodeByID, validNodeID, sysVars, artifacts, feedback, 0, cwd)
}

// Resume 从一次未完成运行的中断处续跑到终态，续写同一 runs/<id>/、run id 不变（语义即「恢复这次运行」）。
// 恢复源全部来自落盘：workflowSnapshot（不回读 store 里可能已被 edit/delete 的活 workflow）经 Expand
// 确定性还原步骤序列、trace 末条记录推断重入下标；trace 中 stepIndex<R 且末条 success 的记录回放重建
// artifacts/feedback（见 replayState）。调用方（CLI）已做派生态的 fail-loud 前置校验，本方法仍对缺失
// 快照 / trace 与快照不一致做防御性报错，不带病续跑、不静默。
func (o *Orchestrator) Resume(ctx context.Context, record *run.Record, trace []run.TraceEntry, obs Observer) error {
	def := record.WorkflowSnapshot
	if def == nil {
		return fmt.Errorf("运行 %s 缺少 workflowSnapshot，无法恢复", record.ID)
	}
	nodeByID, validNodeID := indexNodes(def.Nodes)
	steps := workflow.Expand(def.Nodes)
	startIndex, err := resumeStartIndex(trace, len(steps))
	if err != nil {
		return fmt.Errorf("运行 %s 无法推断恢复点: %w", record.ID, err)
	}

	// 从落盘 trace 回放重建「进入重入点那一刻」的串联态（不按物理行切片，见 replayState）。
	artifacts, feedback := replayState(trace, startIndex)
	sysVars := map[string]string{"userPrompt": record.UserPrompt, "cwd": record.Cwd}

	// 状态由 failed/interrupted 改回 running、更新 pid/pidStartTime、清空 endedAt/error——endedAt 在失败
	// 收尾时可能已写入，续跑期间必须复归 null，守住「running 时 endedAt 为 null」的落盘不变量。
	pid := os.Getpid()
	startToken, _ := run.ProcessStartToken(pid)
	record.Status = run.StatusRunning
	record.Pid = pid
	record.PidStartTime = startToken
	record.EndedAt = nil
	record.Error = nil
	record.Artifacts = artifacts // 与循环内 artifacts 共享同一 map：agent 步覆盖写即对 record 可见
	if err := o.Store.WriteRun(record); err != nil {
		return err
	}
	if err := o.Store.RemoveSummary(record.ID); err != nil {
		return err
	}
	obs.OnExpand(steps, startIndex)

	return o.runSteps(ctx, obs, record, steps, nodeByID, validNodeID, sysVars, artifacts, feedback, startIndex, record.Cwd)
}

// runSteps 是 Run 与 Resume 共用的步骤主循环内核：从 startIndex 起逐步执行 steps，追加 trace 到同一
// run、增量落盘 artifacts，收尾为 completed 或 failed。record 须已落盘为 running（Run 经 CreateRun、
// Resume 经 WriteRun），且 record.Artifacts 与传入的 artifacts 是同一 map。
func (o *Orchestrator) runSteps(ctx context.Context, obs Observer, record *run.Record, steps []workflow.ExecutionStep,
	nodeByID map[string]workflow.Node, validNodeID func(string) bool, sysVars, artifacts, feedback map[string]string,
	startIndex int, cwd string) error {

	for stepIndex := startIndex; stepIndex < len(steps); stepIndex++ {
		step := steps[stepIndex]
		node := nodeByID[step.NodeID]
		entry, stepErr := o.executeStep(ctx, obs, stepIndex, step, node, sysVars, artifacts, feedback, validNodeID, cwd)
		appendErr := o.Store.AppendTrace(record.ID, entry)
		obs.OnStepDone(entry)
		// 引擎失败或 trace 落盘失败都终止本次运行——两者合并上抛，不让 IO 错误遮蔽引擎错误（承「错误不吞」）。
		if stepErr != nil || appendErr != nil {
			return o.finalizeFailed(record, stepIndex, errors.Join(stepErr, appendErr))
		}
		// 成功后按步类型更新串联态（record.Artifacts 与 artifacts 是同一个 map，此处只需增量落盘）。
		if step.Type == workflow.StepTypeAgent {
			artifacts[node.ID] = entry.Output
			if err := o.Store.WriteRun(record); err != nil { // 增量落盘 artifacts
				return err
			}
		} else {
			feedback[node.ID] = entry.Output
		}
	}

	return o.finalizeCompleted(record)
}

// indexNodes 建立 nodeId → 节点 的索引与「id 是否有效」判定，供渲染引用校验与主循环取节点。
func indexNodes(nodes []workflow.Node) (map[string]workflow.Node, func(string) bool) {
	nodeByID := make(map[string]workflow.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	validNodeID := func(id string) bool { _, ok := nodeByID[id]; return ok }
	return nodeByID, validNodeID
}

// resumeStartIndex 依据 trace 推断重入下标 R：对每个 stepIndex 取末条记录，返回最小的「无记录或末条
// 非 success」下标；trace 为空即 0。若所有步骤末条均成功，说明落盘 trace 已完整成功，不应恢复。
func resumeStartIndex(trace []run.TraceEntry, stepCount int) (int, error) {
	lastByStep := make(map[int]run.TraceEntry, len(trace))
	for _, entry := range trace {
		if entry.StepIndex < 0 || entry.StepIndex >= stepCount {
			return 0, fmt.Errorf("trace stepIndex %d 超出展开步数 %d", entry.StepIndex, stepCount)
		}
		lastByStep[entry.StepIndex] = entry
	}
	for stepIndex := 0; stepIndex < stepCount; stepIndex++ {
		entry, ok := lastByStep[stepIndex]
		if !ok || !entry.Success {
			return stepIndex, nil
		}
	}
	return 0, fmt.Errorf("trace 中 %d 个步骤末条均为 success，无需恢复", stepCount)
}

// replayState 回放 trace 重建续跑所需的串联态：仅取 stepIndex < startIndex 且末条 success 的记录，按
// stepIndex 升序覆盖——agent 记录还原节点产物（artifacts，{{node-id}} 引用源），evaluator 记录还原
// 评测反馈（feedback，供带评测节点的下一轮 agent 消费）。run.json 的 artifacts 只存 agent 产物、不含
// 评测反馈，故以 trace 为权威回放源。**不按物理行切片**（「取前 startIndex 行」）：一次 resume 又失败后
// trace 已混入旧失败行与补跑行、不再是干净前缀，只有按 stepIndex + success 过滤、稳定排序后覆盖才正确
// （同一 stepIndex 以最后一次记录为准）。
func replayState(trace []run.TraceEntry, startIndex int) (artifacts, feedback map[string]string) {
	artifacts = map[string]string{}
	feedback = map[string]string{}
	lastByStep := make(map[int]run.TraceEntry, len(trace))
	for _, entry := range trace {
		lastByStep[entry.StepIndex] = entry
	}
	relevant := make([]run.TraceEntry, 0, len(trace))
	for _, entry := range lastByStep {
		if entry.StepIndex < startIndex && entry.Success {
			relevant = append(relevant, entry)
		}
	}
	sort.SliceStable(relevant, func(i, j int) bool { return relevant[i].StepIndex < relevant[j].StepIndex })
	for _, entry := range relevant {
		if entry.Type == workflow.StepTypeEvaluator {
			feedback[entry.NodeID] = entry.Output
		} else {
			artifacts[entry.NodeID] = entry.Output
		}
	}
	return artifacts, feedback
}

// executeStep 渲染输入、调用引擎、组装该步的 trace 条目。返回的 error 非 nil 表示引擎失败
// （此时 entry.Success=false、entry.Error 已填），由 Run 决定收尾。
func (o *Orchestrator) executeStep(ctx context.Context, obs Observer, stepIndex int, step workflow.ExecutionStep,
	node workflow.Node, sysVars, artifacts, feedback map[string]string, validNodeID func(string) bool, cwd string) (run.TraceEntry, error) {

	engineName, config, prompt := o.buildStepInput(step, node, sysVars, artifacts, feedback, validNodeID)

	notifyStart(obs, stepIndex, step, node, engineName, config)

	entry := run.TraceEntry{
		StepIndex:    stepIndex,
		Type:         step.Type,
		NodeID:       node.ID,
		DisplayName:  node.DisplayName,
		Iteration:    step.Iteration,
		Engine:       engineName,
		EngineConfig: config,
		Input:        prompt,
	}

	result, err := o.invokeEngine(ctx, engineName, config, prompt, cwd)
	entry.DurationMs = result.DurationMilliseconds
	if err != nil {
		message := err.Error()
		entry.Success = false
		entry.Error = &message
		return entry, err
	}
	entry.Success = true
	entry.Output = result.Text
	entry.Tokens = result.Tokens
	entry.SessionID = result.SessionID
	return entry, nil
}

// buildStepInput 解析该步的引擎 / 配置，并渲染其完整输入（含反馈 / 待评产物的拼接）。
func (o *Orchestrator) buildStepInput(step workflow.ExecutionStep, node workflow.Node,
	sysVars, artifacts, feedback map[string]string, validNodeID func(string) bool) (string, *workflow.EngineConfig, string) {

	if step.Type == workflow.StepTypeAgent {
		prompt := workflow.Render(node.PromptTemplate, sysVars, artifacts, validNodeID)
		if fb := feedback[node.ID]; fb != "" {
			prompt += "\n\n## Previous evaluator feedback\n\n" +
				"<previous_evaluator_feedback>\n" + fb + "\n</previous_evaluator_feedback>"
		}
		return node.Engine, node.EngineConfig, prompt
	}
	// evaluator：节点必带 evaluator（展开保证），缺失＝schema 与展开不一致，交由 invokeEngine 报错。
	if node.Evaluator == nil {
		return "", nil, ""
	}
	prompt := workflow.Render(node.Evaluator.PromptTemplate, sysVars, artifacts, validNodeID)
	prompt += "\n\n## Artifact under review\n\n" +
		"<artifact_under_review>\n" + artifacts[node.ID] + "\n</artifact_under_review>"
	return node.Evaluator.Engine, node.Evaluator.EngineConfig, prompt
}

// invokeEngine 解析引擎并执行一步；engine 名为空（evaluator 步却无 evaluator 配置）显式报错，不静默跳过。
func (o *Orchestrator) invokeEngine(ctx context.Context, engineName string, config *workflow.EngineConfig, prompt, cwd string) (engine.RunResult, error) {
	if engineName == "" {
		return engine.RunResult{}, fmt.Errorf("内部错误：evaluator 步缺少 evaluator 配置")
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

// notifyStart 组装 StepInfo 并通知 Observer（模型取声明值，空串＝引擎默认）。
func notifyStart(obs Observer, stepIndex int, step workflow.ExecutionStep, node workflow.Node, engineName string, config *workflow.EngineConfig) {
	model := ""
	if config != nil {
		model = config.Model
	}
	obs.OnStepStart(StepInfo{
		StepIndex: stepIndex, Type: step.Type, NodeID: node.ID, DisplayName: node.DisplayName,
		Iteration: step.Iteration, Engine: engineName, Model: model,
	})
}

// finalizeCompleted 把 run 收尾为 completed 并生成 summary。
func (o *Orchestrator) finalizeCompleted(record *run.Record) error {
	record.Status = run.StatusCompleted
	return o.finalize(record)
}

// finalizeFailed 把 run 收尾为 failed（错误摘要进 run.json，失败步由 trace 末条失败记录推断）并生成
// summary，返回原始引擎错误上抛。
func (o *Orchestrator) finalizeFailed(record *run.Record, _ int, cause error) error {
	record.Status = run.StatusFailed
	message := cause.Error()
	record.Error = &message
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
