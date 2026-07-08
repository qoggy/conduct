package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// fakeEngine 记录每次收到的请求并按注入的 reply 返回，用于验证编排接线，不触碰真引擎。
type fakeEngine struct {
	calls *[]engine.RunRequest
	reply func(callIndex int, request engine.RunRequest) (engine.RunResult, error)
}

func (f fakeEngine) Name() string { return "claude-code" }
func (f fakeEngine) Run(_ context.Context, request engine.RunRequest) (engine.RunResult, error) {
	index := len(*f.calls)
	*f.calls = append(*f.calls, request)
	return f.reply(index, request)
}

// captureObserver 记录展开步数、恢复起始下标与逐步 trace，供断言。
type captureObserver struct {
	expandSteps      int
	expandStartIndex int
	done             []run.TraceEntry
}

func (c *captureObserver) OnExpand(steps []workflow.ExecutionStep, startIndex int) {
	c.expandSteps = len(steps)
	c.expandStartIndex = startIndex
}
func (c *captureObserver) OnStepStart(StepInfo)            {}
func (c *captureObserver) OnStepDone(entry run.TraceEntry) { c.done = append(c.done, entry) }

func fixedClock() func() time.Time {
	instant := time.Date(2026, 7, 3, 15, 22, 33, 0, time.FixedZone("CST", 8*3600))
	return func() time.Time { return instant }
}

func newOrchestrator(t *testing.T, reply func(int, engine.RunRequest) (engine.RunResult, error)) (*Orchestrator, *[]engine.RunRequest, *store.Store) {
	t.Helper()
	calls := &[]engine.RunRequest{}
	fe := fakeEngine{calls: calls, reply: reply}
	st := store.New(t.TempDir())
	o := &Orchestrator{
		Store:   st,
		Engines: func(string) (engine.Engine, error) { return fe, nil },
		Now:     fixedClock(),
	}
	return o, calls, st
}

func TestRunThreadsArtifactsAndCompletes(t *testing.T) {
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "PLAN:{{plan}}"},
	}}
	o, calls, st := newOrchestrator(t, func(i int, _ engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{Text: fmt.Sprintf("out-%d", i), Tokens: 10, DurationMilliseconds: 5}, nil
	})
	obs := &captureObserver{}

	runID, err := o.Run(context.Background(), def, "加个按钮", "/proj", obs)
	if err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if runID != "flow-20260703-152233" {
		t.Errorf("run id 应由固定钟决定，得到 %q", runID)
	}
	if obs.expandSteps != 2 || len(obs.done) != 2 {
		t.Fatalf("应展开 2 步、完成 2 步，得到 expand=%d done=%d", obs.expandSteps, len(obs.done))
	}
	// 串联：plan 的输入＝userPrompt；code 的输入＝上游 plan 产物注入模板。
	if (*calls)[0].Prompt != "加个按钮" {
		t.Errorf("plan 输入应为 userPrompt，得到 %q", (*calls)[0].Prompt)
	}
	if (*calls)[1].Prompt != "PLAN:out-0" {
		t.Errorf("code 输入应串联 plan 产物，得到 %q", (*calls)[1].Prompt)
	}
	// 终态：run.json completed + artifacts 落盘。
	rec, err := st.LoadRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != run.StatusCompleted || rec.EndedAt == nil {
		t.Errorf("应收尾为 completed，得到 %+v", rec)
	}
	if rec.Artifacts["plan"] != "out-0" || rec.Artifacts["code"] != "out-1" {
		t.Errorf("artifacts 未正确落盘: %+v", rec.Artifacts)
	}
	// summary 已生成。
	path, _ := st.SummaryPath(runID)
	if _, err := st.LoadTrace(runID); err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Error("summary 路径为空")
	}
}

func TestRunFeedsEvaluatorFeedbackIntoNextAgent(t *testing.T) {
	loop := 1
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "写代码",
			LoopCount: &loop,
			Evaluator: &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评一下"}},
	}}
	// 展开：agent(1) → evaluator(1) → agent(2)，共 3 步。
	o, calls, _ := newOrchestrator(t, func(i int, _ engine.RunRequest) (engine.RunResult, error) {
		if i == 1 { // evaluator 步
			return engine.RunResult{Text: "需要改进 X"}, nil
		}
		return engine.RunResult{Text: fmt.Sprintf("agent-%d", i)}, nil
	})
	obs := &captureObserver{}
	if _, err := o.Run(context.Background(), def, "需求", "/p", obs); err != nil {
		t.Fatalf("Run 报错: %v", err)
	}
	if obs.expandSteps != 3 {
		t.Fatalf("evaluator 内循环 loopCount=1 应展开 3 步，得到 %d", obs.expandSteps)
	}
	// evaluator 输入拼接待评产物；第二次 agent 输入拼接上轮反馈。
	if !strings.Contains((*calls)[1].Prompt, "<artifact_under_review>\nagent-0") {
		t.Errorf("evaluator 应看到待评产物，实际:\n%s", (*calls)[1].Prompt)
	}
	if !strings.Contains((*calls)[2].Prompt, "<previous_evaluator_feedback>\n需要改进 X") {
		t.Errorf("第二次 agent 应收到 evaluator 反馈，实际:\n%s", (*calls)[2].Prompt)
	}
}

// TestResumeReplaysStateAndCompletes 覆盖 Resume 核心：从 trace 末条失败记录推断重入点、回放前序成功步的
// 产物、续写同一 run 到 completed，并清空 endedAt/error、保留旧失败 trace（审计）。
func TestResumeReplaysStateAndCompletes(t *testing.T) {
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "PLAN:{{plan}}"},
		{ID: "review", DisplayName: "评审", Engine: "claude-code", PromptTemplate: "REVIEW:{{code}}"},
	}}
	o, calls, st := newOrchestrator(t, func(i int, _ engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{Text: fmt.Sprintf("resumed-%d", i), Tokens: 5, DurationMilliseconds: 3}, nil
	})

	// 预置一条 failed run：step1（code）失败，step0（plan）已成功产出 "planned"。
	errMsg := "boom"
	ended := "2026-07-03T15:23:00+08:00"
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: def,
		UserPrompt: "加按钮", Cwd: "/proj", Status: run.StatusFailed,
		Pid: 21474836, Steps: 3, StartedAt: "2026-07-03T15:22:33+08:00",
		EndedAt: &ended, Artifacts: map[string]string{"plan": "planned"},
		Error: &errMsg,
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, record.ID,
		run.TraceEntry{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Input: "加按钮", Success: true, Output: "planned"},
		run.TraceEntry{StepIndex: 1, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Input: "PLAN:planned", Success: false, Error: &errMsg},
	)

	// 照 CLI 的方式重载 record + trace 再 Resume。
	reloaded, err := st.LoadRun(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := st.LoadTrace(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), reloaded, trace, obs); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}

	// 恢复头信息经 OnExpand 上报：startIndex 由 trace 推断为 1（供人类进度打印「从第 1 步恢复、共剩 2 步」）。
	if obs.expandStartIndex != 1 {
		t.Errorf("OnExpand 起始下标应由 trace 推断为 1，得到 %d", obs.expandStartIndex)
	}

	// 只重跑 step1/step2 两步（step0 跳过）；step1 串联回放的 plan 产物、step2 串联本次重跑的 code 产物。
	if len(*calls) != 2 {
		t.Fatalf("应只重跑 2 步（step1/step2），得到 %d 次引擎调用", len(*calls))
	}
	if (*calls)[0].Prompt != "PLAN:planned" {
		t.Errorf("code 步应串联回放的 plan 产物，得到 %q", (*calls)[0].Prompt)
	}
	if (*calls)[1].Prompt != "REVIEW:resumed-0" {
		t.Errorf("review 步应串联重跑的 code 产物，得到 %q", (*calls)[1].Prompt)
	}

	// 终态 completed；error 清空、endedAt 重写、pid 更新为续跑进程。
	final, err := st.LoadRun(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != run.StatusCompleted || final.EndedAt == nil {
		t.Errorf("应收尾 completed，得到 %+v", final)
	}
	if final.Error != nil {
		t.Errorf("恢复成功后 error 应清空，得到 %v", final.Error)
	}
	if final.Pid != os.Getpid() {
		t.Errorf("pid 应更新为续跑进程 %d，得到 %d", os.Getpid(), final.Pid)
	}

	// 旧失败 trace 保留 + 续写补跑行：审计视角 4 条（step0, step1-fail, step1-redo, step2）。
	finalTrace, err := st.LoadTrace(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalTrace) != 4 {
		t.Fatalf("应保留旧失败行并续写补跑行共 4 条，得到 %d", len(finalTrace))
	}
	// 进度去重后 k = 唯一成功 stepIndex {0,1,2} = 3 = 总步数（k ≤ N）。
	if k := run.ProgressCount(finalTrace); k != 3 {
		t.Errorf("进度去重后 k 应为 3，得到 %d", k)
	}
}

func TestResumeRemovesStaleSummaryBeforeSteps(t *testing.T) {
	const runID = "flow-20260703-152233"
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "x"},
	}}
	var storeForCheck *store.Store
	var summaryReadErr error
	o, _, st := newOrchestrator(t, func(int, engine.RunRequest) (engine.RunResult, error) {
		_, summaryReadErr = storeForCheck.ReadSummary(runID)
		return engine.RunResult{Text: "resumed"}, nil
	})
	storeForCheck = st

	errMsg := "boom"
	ended := "2026-07-03T15:23:00+08:00"
	record := &run.Record{
		ID: runID, Workflow: "flow", WorkflowSnapshot: def,
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusFailed, Steps: 1,
		StartedAt: "2026-07-03T15:22:33+08:00", EndedAt: &ended,
		Artifacts: map[string]string{}, Error: &errMsg,
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteSummary(runID, "# 旧失败总结\n"); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, runID,
		run.TraceEntry{StepIndex: 0, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, Error: &errMsg},
	)
	trace, err := st.LoadTrace(runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Resume(context.Background(), record, trace, &captureObserver{}); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}
	if !errors.Is(summaryReadErr, store.ErrSummaryNotExist) {
		t.Fatalf("resume 执行步骤前应已移除旧 summary，读取错误=%v", summaryReadErr)
	}
}

// TestResumeReplaysEvaluatorFeedback 覆盖 replayState 的 evaluator 分支：失败步在 evaluator 之后时，resume
// 须回放前序 evaluator 记录的反馈、串联进重跑的 agent 步（锁住 feedback 回放，而非只回放 agent 产物）。
func TestResumeReplaysEvaluatorFeedback(t *testing.T) {
	loop := 1
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "写代码",
			LoopCount: &loop,
			Evaluator: &workflow.Evaluator{Engine: "claude-code", PromptTemplate: "评一下"}},
	}}
	// 展开：[0] agent(iter1) → [1] evaluator(iter1) → [2] agent(iter2)，共 3 步；step2 失败后 resume。
	o, calls, st := newOrchestrator(t, func(int, engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{Text: "resumed-agent", Tokens: 5, DurationMilliseconds: 3}, nil
	})

	errMsg := "boom"
	ended := "2026-07-03T15:23:00+08:00"
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: def,
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusFailed,
		Pid: 21474836, Steps: 3, StartedAt: "2026-07-03T15:22:33+08:00",
		EndedAt: &ended, Artifacts: map[string]string{"code": "agent-0"},
		Error: &errMsg,
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, record.ID,
		run.TraceEntry{StepIndex: 0, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: true, Output: "agent-0"},
		run.TraceEntry{StepIndex: 1, Type: "evaluator", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: true, Output: "需要改进 X"},
		run.TraceEntry{StepIndex: 2, Type: "agent", NodeID: "code", DisplayName: "编码", Engine: "claude-code", Success: false, Error: &errMsg},
	)

	reloaded, err := st.LoadRun(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := st.LoadTrace(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Resume(context.Background(), reloaded, trace, &captureObserver{}); err != nil {
		t.Fatalf("Resume 报错: %v", err)
	}

	// 只重跑 step2（agent iter2）一步，其输入须串联回放的 evaluator 反馈（不是只回放了 agent 产物）。
	if len(*calls) != 1 {
		t.Fatalf("应只重跑 1 步（step2），得到 %d 次引擎调用", len(*calls))
	}
	if !strings.Contains((*calls)[0].Prompt, "<previous_evaluator_feedback>\n需要改进 X") {
		t.Errorf("重跑的 agent 步应收到回放的 evaluator 反馈，实际:\n%s", (*calls)[0].Prompt)
	}
}

// TestResumeEmptyTraceStartsAtZero 覆盖新规格：trace 为空时重入点 R=0，等价从头跑。
func TestResumeEmptyTraceStartsAtZero(t *testing.T) {
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
	}}
	o, calls, st := newOrchestrator(t, func(int, engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{Text: "ok"}, nil
	})
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: def,
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusFailed, Steps: 1,
		StartedAt: "2026-07-03T15:22:33+08:00", Artifacts: map[string]string{},
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), record, nil, obs); err != nil {
		t.Fatalf("空 trace 应从头恢复成功，得到 %v", err)
	}
	if obs.expandStartIndex != 0 || len(*calls) != 1 {
		t.Fatalf("空 trace 应从 step0 起跑，start=%d calls=%d", obs.expandStartIndex, len(*calls))
	}
}

func TestResumeInterruptedGapStartsAtMissingStep(t *testing.T) {
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "plan", DisplayName: "规划", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
		{ID: "code", DisplayName: "编码", Engine: "claude-code", PromptTemplate: "PLAN:{{plan}}"},
		{ID: "review", DisplayName: "评审", Engine: "claude-code", PromptTemplate: "REVIEW:{{code}}"},
	}}
	o, calls, st := newOrchestrator(t, func(i int, _ engine.RunRequest) (engine.RunResult, error) {
		return engine.RunResult{Text: fmt.Sprintf("resumed-%d", i), Tokens: 5, DurationMilliseconds: 3}, nil
	})
	record := &run.Record{
		ID: "flow-20260703-152233", Workflow: "flow", WorkflowSnapshot: def,
		UserPrompt: "需求", Cwd: "/p", Status: run.StatusRunning, Pid: -1, Steps: 3,
		StartedAt: "2026-07-03T15:22:33+08:00", Artifacts: map[string]string{"plan": "planned"},
	}
	if err := st.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	seedTrace(t, st, record.ID,
		run.TraceEntry{StepIndex: 0, Type: "agent", NodeID: "plan", DisplayName: "规划", Engine: "claude-code", Input: "需求", Success: true, Output: "planned"},
	)
	trace, err := st.LoadTrace(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	obs := &captureObserver{}
	if err := o.Resume(context.Background(), record, trace, obs); err != nil {
		t.Fatalf("interrupted 缺口应可恢复，得到 %v", err)
	}
	if obs.expandStartIndex != 1 {
		t.Fatalf("缺失 step1 时应从 1 恢复，得到 %d", obs.expandStartIndex)
	}
	if len(*calls) != 2 || (*calls)[0].Prompt != "PLAN:planned" || (*calls)[1].Prompt != "REVIEW:resumed-0" {
		t.Fatalf("应跳过 step0 并续跑 step1/2，calls=%+v", *calls)
	}
}

// seedTrace 预置若干 trace 行（供 Resume 测试构造失败运行的历史）。
func seedTrace(t *testing.T, st *store.Store, id string, entries ...run.TraceEntry) {
	t.Helper()
	for _, entry := range entries {
		if err := st.AppendTrace(id, entry); err != nil {
			t.Fatalf("预置 trace 失败: %v", err)
		}
	}
}

func TestRunFailsLoudAndPreservesTrace(t *testing.T) {
	def := &workflow.Definition{Name: "flow", Nodes: []workflow.Node{
		{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
		{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"},
		{ID: "c", DisplayName: "丙", Engine: "claude-code", PromptTemplate: "z"},
	}}
	o, _, st := newOrchestrator(t, func(i int, _ engine.RunRequest) (engine.RunResult, error) {
		if i == 1 { // 第二步引擎失败
			return engine.RunResult{DurationMilliseconds: 3}, fmt.Errorf("claude 退出码 1: boom")
		}
		return engine.RunResult{Text: "ok"}, nil
	})
	obs := &captureObserver{}

	runID, err := o.Run(context.Background(), def, "需求", "/p", obs)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("失败步应把引擎错误上抛，得到 %v", err)
	}
	rec, loadErr := st.LoadRun(runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if rec.Status != run.StatusFailed {
		t.Errorf("应收尾为 failed，得到 status=%q", rec.Status)
	}
	if rec.Error == nil || !strings.Contains(*rec.Error, "boom") {
		t.Errorf("run.json 应记失败信息，得到 %v", rec.Error)
	}
	// 已完成步骤 + 失败步骤的 trace 都保留（第 3 步不应执行）。
	trace, _ := st.LoadTrace(runID)
	if len(trace) != 2 {
		t.Fatalf("应保留 2 条 trace（1 成功 + 1 失败），得到 %d", len(trace))
	}
	if trace[0].Success != true || trace[1].Success != false || trace[1].Error == nil {
		t.Errorf("trace 成败标记错: %+v", trace)
	}
}
