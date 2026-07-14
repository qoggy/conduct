package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// fakeEngine 记录每次收到的请求并按注入的 reply 返回，用于验证编排接线，不触碰真引擎。
// 并行调度下多个 worker goroutine 并发调 Run，故 calls 追加加锁、reply 只依赖请求本身（不依赖调用序）。
type fakeEngine struct {
	mu    *sync.Mutex
	calls *[]engine.RunRequest
	reply func(request engine.RunRequest) (engine.RunResult, error)
}

func (f fakeEngine) Name() string { return "claude-code" }
func (f fakeEngine) Run(_ context.Context, request engine.RunRequest) (engine.RunResult, error) {
	f.mu.Lock()
	*f.calls = append(*f.calls, request)
	f.mu.Unlock()
	return f.reply(request)
}

// captureObserver 记录调度概述与逐节点 trace，供断言。OnNodeDone 只由调度 goroutine 调用，无需加锁。
type captureObserver struct {
	agentNodeCount  int
	initialReady    []NodeBrief
	resumeDoneCount int
	done            []run.TraceEntry
}

func (c *captureObserver) OnSchedule(info ScheduleInfo) {
	c.agentNodeCount = info.AgentNodeCount
	c.initialReady = info.InitialReady
	c.resumeDoneCount = info.ResumeDoneCount
}
func (c *captureObserver) OnNodeStart(NodeInfo)            {}
func (c *captureObserver) OnNodeDone(entry run.TraceEntry) { c.done = append(c.done, entry) }

func fixedClock() func() time.Time {
	instant := time.Date(2026, 7, 3, 15, 22, 33, 0, time.FixedZone("CST", 8*3600))
	return func() time.Time { return instant }
}

func newOrchestrator(t *testing.T, reply func(engine.RunRequest) (engine.RunResult, error)) (*Orchestrator, *[]engine.RunRequest, *store.Store) {
	t.Helper()
	calls := &[]engine.RunRequest{}
	fe := fakeEngine{mu: &sync.Mutex{}, calls: calls, reply: reply}
	st := store.New(t.TempDir())
	o := &Orchestrator{
		Store:   st,
		Engines: func(string) (engine.Engine, error) { return fe, nil },
		Now:     fixedClock(),
	}
	return o, calls, st
}

// echoReply 返回 Text = "out:"+prompt 的成功结果，使下游输入可回溯上游产物、且与调用序无关。
func echoReply(request engine.RunRequest) (engine.RunResult, error) {
	return engine.RunResult{Text: "out:" + request.Prompt, Tokens: 5, DurationMilliseconds: 3}, nil
}

// wf 把定义主体包成完整记录（Run/Resume 的入参单元）。
func wf(name string, def workflow.Definition) *workflow.Workflow {
	return &workflow.Workflow{Name: name, Definition: def}
}

// chainDef 返回 START→plan→code→review→END 的线性 DAG（用于串联 / resume 测试）。
func chainDef() workflow.Definition {
	return workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "PLAN:{{plan}}"},
			{ID: "review", DisplayName: "评审", Engine: "claude-code", PromptTemplate: "REVIEW:{{code}}"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "plan"}, {From: "plan", To: "code"},
			{From: "code", To: "review"}, {From: "review", To: "END"},
		},
	}
}

// findCall 返回首个 Prompt 命中的请求；未命中 t.Fatal。
func findCall(t *testing.T, calls []engine.RunRequest, prompt string) engine.RunRequest {
	t.Helper()
	for _, c := range calls {
		if c.Prompt == prompt {
			return c
		}
	}
	t.Fatalf("未找到 Prompt=%q 的引擎调用，实际=%v", prompt, calls)
	return engine.RunRequest{}
}

func TestRunThreadsArtifactsAndCompletes(t *testing.T) {
	def := chainDef()
	o, calls, st := newOrchestrator(t, echoReply)
	obs := &captureObserver{}

	runID, err := o.Run(context.Background(), wf("flow", def), "加个按钮", "/proj", obs)
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if runID != "flow-20260703-152233" {
		t.Errorf("run id 应由固定钟决定，得到 %q", runID)
	}
	if obs.agentNodeCount != 3 || len(obs.done) != 3 {
		t.Fatalf("应 3 个 agent 节点、完成 3 个，得到 count=%d done=%d", obs.agentNodeCount, len(obs.done))
	}
	// 串联：plan 输入＝userPrompt；code 输入＝上游 plan 产物注入模板；review 串联 code 产物。
	findCall(t, *calls, "加个按钮")
	findCall(t, *calls, "PLAN:out:加个按钮")
	findCall(t, *calls, "REVIEW:out:PLAN:out:加个按钮")
	// 终态：run.json completed + artifacts 落盘（仅 agent 节点）。
	rec, err := st.LoadRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != run.StatusCompleted || rec.EndedAt == nil {
		t.Errorf("应收尾为 completed，得到 %+v", rec)
	}
	if rec.Artifacts["plan"] != "out:加个按钮" || rec.Artifacts["review"] != "out:REVIEW:out:PLAN:out:加个按钮" {
		t.Errorf("artifacts 未正确落盘: %+v", rec.Artifacts)
	}
	if _, hasStart := rec.Artifacts["START"]; hasStart {
		t.Error("artifacts 不应含 START 标记节点")
	}
}

func TestRunInjectsRunID(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "运行：{{sys.runId}}"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
	}
	o, calls, _ := newOrchestrator(t, echoReply)
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	findCall(t, *calls, "运行："+runID)
}

// TestRunFanoutParallel 覆盖 START 扇出：a、b 以 START 为唯一前驱，t0 同刻就绪并跑。
func TestRunFanoutParallel(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "A"},
			{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "B"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "a"}, {From: "START", To: "b"},
			{From: "a", To: "END"}, {From: "b", To: "END"},
		},
	}
	o, calls, st := newOrchestrator(t, echoReply)
	obs := &captureObserver{}
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", obs)
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if len(obs.initialReady) != 2 {
		t.Errorf("START 扇出应有 2 个初始就绪节点，得到 %d", len(obs.initialReady))
	}
	if len(*calls) != 2 {
		t.Errorf("a、b 各跑一次共 2 次，得到 %d", len(*calls))
	}
	rec, _ := st.LoadRun(runID)
	if rec.Status != run.StatusCompleted || rec.Artifacts["a"] != "out:A" || rec.Artifacts["b"] != "out:B" {
		t.Errorf("并行分支产物未落全: %+v", rec)
	}
}

// TestRunDiamondThreadsBothBranches 覆盖菱形收口：d 引用两条并行分支 {{b}}{{c}} 的产物。
func TestRunDiamondThreadsBothBranches(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "a", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
			{ID: "b", DisplayName: "b", Engine: "claude-code", PromptTemplate: "B:{{a}}"},
			{ID: "c", DisplayName: "c", Engine: "claude-code", PromptTemplate: "C:{{a}}"},
			{ID: "d", DisplayName: "d", Engine: "claude-code", PromptTemplate: "D:{{b}}|{{c}}"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "a"}, {From: "a", To: "b"}, {From: "a", To: "c"},
			{From: "b", To: "d"}, {From: "c", To: "d"}, {From: "d", To: "END"},
		},
	}
	o, calls, st := newOrchestrator(t, echoReply)
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	// d 的输入须同时串联 b、c 两条分支的产物。
	dPrompt := "D:out:B:out:需求|out:C:out:需求"
	findCall(t, *calls, dPrompt)
	rec, _ := st.LoadRun(runID)
	if rec.Status != run.StatusCompleted || len(rec.Artifacts) != 4 {
		t.Errorf("菱形应 4 个 agent 产物且 completed，得到 %+v", rec)
	}
}

// TestRunFailStopsBeforeFanout 覆盖失败早于扇出：a 失败后不再调度下游 b、c。
func TestRunFailStopsBeforeFanout(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "a", Engine: "claude-code", PromptTemplate: "A"},
			{ID: "b", DisplayName: "b", Engine: "claude-code", PromptTemplate: "B:{{a}}"},
			{ID: "c", DisplayName: "c", Engine: "claude-code", PromptTemplate: "C:{{a}}"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "a"}, {From: "a", To: "b"}, {From: "a", To: "c"},
			{From: "b", To: "END"}, {From: "c", To: "END"},
		},
	}
	o, calls, st := newOrchestrator(t, func(req engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{DurationMilliseconds: 3}, fmt.Errorf("claude 退出码 1: boom")
	})
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("失败应把引擎错误上抛，得到 %v", err)
	}
	if len(*calls) != 1 {
		t.Errorf("a 失败后不应调度 b、c，应只 1 次引擎调用，得到 %d", len(*calls))
	}
	rec, _ := st.LoadRun(runID)
	if rec.Status != run.StatusFailed || rec.Error == nil || !strings.Contains(*rec.Error, "boom") {
		t.Errorf("应收尾 failed 且记失败信息，得到 %+v", rec)
	}
}

// TestRunFailedNodeAttributionAndTimestamp 覆盖失败节点归属 + 时间戳补偿：record.FailedNodeID 记根因节点、
// record.Error 自带节点名（节点 X：…），且失败节点引擎回零耗时时由调度线程补真实 EndedAt/DurationMs
// （不再 EndedAt==StartedAt）。用递增时钟使补偿可观测（fixedClock 恒定，补偿无从体现）。
func TestRunFailedNodeAttributionAndTimestamp(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "gen", DisplayName: "生成", Engine: "claude-code", PromptTemplate: "G"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{{From: "START", To: "gen"}, {From: "gen", To: "END"}},
	}
	fe := fakeEngine{mu: &sync.Mutex{}, calls: &[]engine.RunRequest{}, reply: func(engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{}, fmt.Errorf("boom") // 失败且回零耗时（EndedAt==StartedAt 的失真场景）
	}}
	base := time.Date(2026, 7, 3, 15, 22, 33, 0, time.FixedZone("CST", 8*3600))
	tick := 0
	o := &Orchestrator{
		Store:   store.New(t.TempDir()),
		Engines: func(string) (engine.Engine, error) { return fe, nil },
		Now: func() time.Time { // 每次 +1s（o.Now 只在调度线程调用，无需锁）
			instant := base.Add(time.Duration(tick) * time.Second)
			tick++
			return instant
		},
	}
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("失败应上抛引擎错误，得到 %v", err)
	}
	rec, _ := o.Store.LoadRun(runID)
	if rec.FailedNodeID == nil || *rec.FailedNodeID != "gen" {
		t.Errorf("FailedNodeID 应为 gen，得到 %v", rec.FailedNodeID)
	}
	if rec.Error == nil || !strings.Contains(*rec.Error, "节点 gen：") || !strings.Contains(*rec.Error, "boom") {
		t.Errorf("error 应自带节点名『节点 gen：…boom』，得到 %v", rec.Error)
	}
	trace, _ := o.Store.LoadTrace(runID)
	if len(trace) != 1 {
		t.Fatalf("应有 1 条 trace，得到 %d", len(trace))
	}
	if trace[0].DurationMs <= 0 || trace[0].EndedAt == trace[0].StartedAt {
		t.Errorf("失败节点应补真实耗时（EndedAt != StartedAt），得到 dur=%d start=%s end=%s",
			trace[0].DurationMs, trace[0].StartedAt, trace[0].EndedAt)
	}
}

// TestRunDrainPreservesInflightSuccess 覆盖 drain：并行分支里一个失败、另一个在途成功，其产物仍保留。
func TestRunDrainPreservesInflightSuccess(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "boom", DisplayName: "炸", Engine: "claude-code", PromptTemplate: "BOOM"},
			{ID: "ok", DisplayName: "好", Engine: "claude-code", PromptTemplate: "OK"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "boom"}, {From: "START", To: "ok"},
			{From: "boom", To: "END"}, {From: "ok", To: "END"},
		},
	}
	o, _, st := newOrchestrator(t, func(req engine.RunRequest) (engine.RunResult, error) {
		if req.Prompt == "BOOM" {
			return engine.RunResult{DurationMilliseconds: 3}, fmt.Errorf("kaboom")
		}
		return engine.RunResult{Text: "ok-out", DurationMilliseconds: 3}, nil
	})
	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("应上抛失败，得到 %v", err)
	}
	rec, _ := st.LoadRun(runID)
	if rec.Status != run.StatusFailed {
		t.Errorf("应收尾 failed，得到 %q", rec.Status)
	}
	// drain：ok 分支的成功产物保留（resume 无需重跑）。
	if rec.Artifacts["ok"] != "ok-out" {
		t.Errorf("drain 应保留在途成功产物，得到 %+v", rec.Artifacts)
	}
	trace, _ := st.LoadTrace(runID)
	if len(trace) != 2 {
		t.Fatalf("drain 应让两分支各落一条 trace，得到 %d", len(trace))
	}
}

// TestRunJoinsParallelEngineErrors 覆盖 drain 期间多个并行节点都失败的场景：调用方收到的 cause
// 必须包含全部引擎错误，不能只保留最先完成的一个。并行完成顺序不确定，因此只断言错误集合、不断言顺序。
func TestRunJoinsParallelEngineErrors(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "left", DisplayName: "左", Engine: "claude-code", PromptTemplate: "LEFT"},
			{ID: "right", DisplayName: "右", Engine: "claude-code", PromptTemplate: "RIGHT"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{
			{From: "START", To: "left"}, {From: "START", To: "right"},
			{From: "left", To: "END"}, {From: "right", To: "END"},
		},
	}
	o, _, st := newOrchestrator(t, func(request engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{DurationMilliseconds: 3}, fmt.Errorf("%s failed", request.Prompt)
	})

	runID, err := o.Run(context.Background(), wf("flow", def), "需求", "/p", &captureObserver{})
	if err == nil {
		t.Fatal("两个并行节点失败时应上抛聚合错误")
	}
	for _, message := range []string{"LEFT failed", "RIGHT failed"} {
		if !strings.Contains(err.Error(), message) {
			t.Errorf("聚合错误应包含 %q，得到 %v", message, err)
		}
	}
	record, loadErr := st.LoadRun(runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if record.Status != run.StatusFailed {
		t.Errorf("两个并行节点失败后应收尾 failed，得到 %q", record.Status)
	}
	trace, loadErr := st.LoadTrace(runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(trace) != 2 {
		t.Fatalf("drain 应保留两个失败节点的 trace，得到 %d", len(trace))
	}
}

// seedTrace 预置若干 trace 行（供 Resume 测试构造未完成运行的历史）。
func seedTrace(t *testing.T, st *store.Store, id string, entries ...run.TraceEntry) {
	t.Helper()
	for _, entry := range entries {
		if err := st.AppendTrace(id, entry); err != nil {
			t.Fatalf("预置 trace 失败: %v", err)
		}
	}
}

// seedFailedRun 预置一次 failed run（record + trace），返回其 id。
func seedFailedRun(t *testing.T, st *store.Store, def workflow.Definition, artifacts map[string]string, entries ...run.TraceEntry) string {
	t.Helper()
	const id = "flow-20260703-152233"
	errMsg := "boom"
	ended := "2026-07-03T15:23:00+08:00"
	record := &run.Record{
		ID: id, Workflow: "flow", WorkflowSnapshot: wf("flow", def),
		UserPrompt: "加按钮", Cwd: "/proj", Status: run.StatusFailed,
		Pid: 21474836, StartedAt: "2026-07-03T15:22:33+08:00",
		EndedAt: &ended, Artifacts: artifacts, Error: &errMsg,
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, id, entries...)
	return id
}

// TestResumeReplaysStateAndCompletes 覆盖 Resume 核心：从 trace 推断已完成集、回放前序成功产物、续跑未完成
// 前沿到 completed，并清空 endedAt/error、保留旧失败 trace（审计）。
func TestResumeReplaysStateAndCompletes(t *testing.T) {
	def := chainDef()
	o, calls, st := newOrchestrator(t, echoReply)
	errMsg := "boom"
	id := seedFailedRun(t, st, def, map[string]string{"plan": "planned"},
		run.TraceEntry{NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Input: "加按钮", Success: true, Output: "planned"},
		run.TraceEntry{NodeID: "code", DisplayName: "编码", Engine: "claude-code", Input: "PLAN:planned", Success: false, Error: &errMsg},
	)

	reloaded, err := st.LoadRun(id)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := st.LoadTrace(id)
	if err != nil {
		t.Fatal(err)
	}
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), reloaded, trace, obs); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}

	// resume 已完成 1 个（plan）；只重跑 code、review 两个未完成节点。
	if obs.resumeDoneCount != 1 {
		t.Errorf("已完成数应为 1，得到 %d", obs.resumeDoneCount)
	}
	if len(*calls) != 2 {
		t.Fatalf("应只重跑 code、review 两个节点，得到 %d 次引擎调用", len(*calls))
	}
	// code 串联回放的 plan 产物；review 串联本次重跑的 code 产物。
	findCall(t, *calls, "PLAN:planned")
	findCall(t, *calls, "REVIEW:out:PLAN:planned")

	final, err := st.LoadRun(id)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != run.StatusCompleted || final.EndedAt == nil {
		t.Errorf("应收尾 completed，得到 %+v", final)
	}
	if final.Error != nil {
		t.Errorf("恢复成功后 error 应清空，得到 %v", final.Error)
	}
	if final.FailedNodeID != nil {
		t.Errorf("恢复成功后 failedNodeId 应清空，得到 %v", *final.FailedNodeID)
	}
	if final.Pid != os.Getpid() {
		t.Errorf("pid 应更新为续跑进程 %d，得到 %d", os.Getpid(), final.Pid)
	}
	// 旧失败 trace 保留 + 续写补跑行：审计视角 4 条（plan, code-fail, code-redo, review）。
	finalTrace, _ := st.LoadTrace(id)
	if len(finalTrace) != 4 {
		t.Fatalf("应保留旧失败行并续写补跑行共 4 条，得到 %d", len(finalTrace))
	}
	// 进度去重后 k = 唯一成功 nodeId {plan,code,review} = 3。
	if k := run.ProgressCount(finalTrace); k != 3 {
		t.Errorf("进度去重后 k 应为 3，得到 %d", k)
	}
}

func TestResumeRemovesStaleSummaryBeforeNodes(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "x"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{{From: "START", To: "code"}, {From: "code", To: "END"}},
	}
	var storeForCheck *store.Store
	var summaryReadErr error
	o, _, st := newOrchestrator(t, func(engine.RunRequest) (engine.RunResult, error) {
		_, summaryReadErr = storeForCheck.ReadSummary("flow-20260703-152233")
		return engine.RunResult{Text: "resumed"}, nil
	})
	storeForCheck = st

	errMsg := "boom"
	id := seedFailedRun(t, st, def, map[string]string{},
		run.TraceEntry{NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, Error: &errMsg},
	)
	if err := st.WriteSummary(id, "# 旧失败总结\n"); err != nil {
		t.Fatal(err)
	}
	trace, _ := st.LoadTrace(id)
	if err := o.Resume(context.Background(), mustLoad(t, st, id), trace, &captureObserver{}); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}
	if !errors.Is(summaryReadErr, store.ErrSummaryNotExist) {
		t.Fatalf("resume 执行节点前应已移除旧 summary，读取错误=%v", summaryReadErr)
	}
}

// TestResumeAllSuccessInterruptedFinalizesCompleted 覆盖：全成功却因进程被杀未及写 completed（interrupted），
// resume 初始 ready 为空、不驱动任何引擎，直接收尾 completed。
func TestResumeAllSuccessInterruptedFinalizesCompleted(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
	}
	o, calls, st := newOrchestrator(t, echoReply)
	// status=running + pid 已死（interrupted）；trace 里 a 已成功。
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: wf("flow", def),
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusRunning, Pid: -1,
		StartedAt: "2026-07-03T15:22:33+08:00", Artifacts: map[string]string{"a": "done-a"},
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, record.ID,
		run.TraceEntry{NodeID: "a", DisplayName: "甲", Engine: "claude-code", Success: true, Output: "done-a"},
	)
	trace, _ := st.LoadTrace(record.ID)
	if err := o.Resume(context.Background(), mustLoad(t, st, record.ID), trace, &captureObserver{}); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("全成功的 resume 不应驱动引擎，得到 %d 次调用", len(*calls))
	}
	final, _ := st.LoadRun(record.ID)
	if final.Status != run.StatusCompleted || final.EndedAt == nil {
		t.Errorf("应直接收尾 completed，得到 %+v", final)
	}
}

// TestResumeEmptyTraceRunsFromStart 覆盖 trace 为空：所有节点未完成，从 START 后继起跑。
func TestResumeEmptyTraceRunsFromStart(t *testing.T) {
	def := workflow.Definition{
		Nodes: []workflow.Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "运行：{{sys.runId}}"},
			{ID: "END"},
		},
		Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
	}
	o, calls, st := newOrchestrator(t, echoReply)
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: wf("flow", def),
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusFailed,
		StartedAt: "2026-07-03T15:22:33+08:00", Artifacts: map[string]string{},
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), record, nil, obs); err != nil {
		t.Fatalf("空 trace 应从头恢复成功，得到 %v", err)
	}
	if obs.resumeDoneCount != 0 || len(*calls) != 1 {
		t.Fatalf("空 trace 应从 START 后继起跑，doneCount=%d calls=%d", obs.resumeDoneCount, len(*calls))
	}
	findCall(t, *calls, "运行："+record.ID)
}

// TestResumeInterruptedGapRunsUnfinished 覆盖 interrupted：plan 成功、code/review 从未开跑，resume 补跑二者。
func TestResumeInterruptedGapRunsUnfinished(t *testing.T) {
	def := chainDef()
	o, calls, st := newOrchestrator(t, echoReply)
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: wf("flow", def),
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusRunning, Pid: -1,
		StartedAt: "2026-07-03T15:22:33+08:00", Artifacts: map[string]string{"plan": "planned"},
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, record.ID,
		run.TraceEntry{NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Input: "需求", Success: true, Output: "planned"},
	)
	trace, _ := st.LoadTrace(record.ID)
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), mustLoad(t, st, record.ID), trace, obs); err != nil {
		t.Fatalf("interrupted 缺口应可恢复，得到 %v", err)
	}
	if obs.resumeDoneCount != 1 || len(*calls) != 2 {
		t.Fatalf("应跳过 plan、续跑 code/review，doneCount=%d calls=%d", obs.resumeDoneCount, len(*calls))
	}
	findCall(t, *calls, "PLAN:planned")
	findCall(t, *calls, "REVIEW:out:PLAN:planned")
}

// TestResumeMissingSnapshotFailsLoud 覆盖缺快照的防御性报错。
func TestResumeMissingSnapshotFailsLoud(t *testing.T) {
	o, _, _ := newOrchestrator(t, echoReply)
	record := &run.Record{ID: "x-1", Status: run.StatusFailed}
	if err := o.Resume(context.Background(), record, nil, &captureObserver{}); err == nil {
		t.Fatal("缺 workflowSnapshot 应报错")
	}
}

// TestResumeInvalidSnapshotFailsLoud 覆盖旧格式 / 损坏快照的防御：快照非 nil 但 Definition 结构非法
// （此处空定义、无 agent 节点）时 Resume 必须报错——否则空 DAG 会让调度器一个节点都不跑就 inflight==0
// 收尾成 completed，把 failed 运行静默改写成成功。
func TestResumeInvalidSnapshotFailsLoud(t *testing.T) {
	o, _, _ := newOrchestrator(t, echoReply)
	record := &run.Record{ID: "x-2", Status: run.StatusFailed, WorkflowSnapshot: wf("flow", workflow.Definition{})}
	if err := o.Resume(context.Background(), record, nil, &captureObserver{}); err == nil {
		t.Fatal("空/非法 workflowSnapshot 应报错，不得冒充 completed")
	}
	if record.Status == run.StatusCompleted {
		t.Fatalf("失败运行被非法快照静默改写为 completed（status=%q）", record.Status)
	}
}

// mustLoad 重载一条 run 记录（照 CLI 的方式），失败即 Fatal。
func mustLoad(t *testing.T, st *store.Store, id string) *run.Record {
	t.Helper()
	record, err := st.LoadRun(id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
