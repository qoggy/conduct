package orchestrator

import (
	"context"
	"fmt"
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

// captureObserver 记录展开步数与逐步 trace，供断言。
type captureObserver struct {
	expandSteps int
	done        []run.TraceEntry
}

func (c *captureObserver) OnExpand(steps []workflow.ExecutionStep) { c.expandSteps = len(steps) }
func (c *captureObserver) OnStepStart(StepInfo)                    {}
func (c *captureObserver) OnStepDone(entry run.TraceEntry)         { c.done = append(c.done, entry) }

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
	if rec.Status != run.StatusFailed || rec.FailedStep == nil || *rec.FailedStep != 1 {
		t.Errorf("应收尾为 failed + failedStep=1，得到 status=%q failedStep=%v", rec.Status, rec.FailedStep)
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
