package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

// nodeResult 是一个 worker goroutine 跑完一个节点后回传调度线程的结果。
// err 非 nil 表示引擎失败（此时 entry.Success=false、entry.Error 已填）。
type nodeResult struct {
	entry run.TraceEntry
	err   error
}

// schedule 是 Run 与 Resume 共用的并行 DAG 调度内核：从 done（含 START 与 resume 已完成节点）出发，
// 按边依赖并发驱动就绪节点，追加 trace、增量落盘 artifacts，收尾为 completed 或 failed。
//
// 并发模型：**只有调度 goroutine 读写共享态**（pending / done / ready / record / trace）；worker goroutine
// 只拿调度线程渲染好的 prompt 调引擎，结果经 channel 回传。节点启动时其入度已归 0 → 所有前驱都在 done，
// 而模板引用被校验限定为祖先，故上游产物齐备、无竞态（此「无竞态」只针对 conduct 内存态，不含文件系统——
// 并行节点共享同一 cwd 的写盘冲突属提示词设计范畴，见 conduct help prompts）。
//
// 失败语义（drain）：failed 置位后停止启动新节点，但继续收干在途节点（各自落 trace、成功产物照记进 done），
// 直到 inflight==0 才收尾 failed。record 须已落盘为 running，且 record.Artifacts 与调度共享同一 map。
func (o *Orchestrator) schedule(ctx context.Context, obs Observer, record *run.Record,
	def *workflow.Definition, sysVars, done map[string]string, cwd string) error {

	nodeByID := make(map[string]workflow.Node, len(def.Nodes))
	for _, node := range def.Nodes {
		nodeByID[node.ID] = node
	}
	validNodeID := func(id string) bool { _, ok := nodeByID[id]; return ok }
	succ, _ := workflow.BuildAdjacency(def)

	// pending[node] = 未完成前驱数（仅对未完成节点计算）：只有前驱与自身都未完成的入边才计数。
	pending := make(map[string]int, len(def.Nodes))
	for _, node := range def.Nodes {
		if _, isDone := done[node.ID]; !isDone {
			pending[node.ID] = 0
		}
	}
	for _, edge := range def.Edges {
		if _, toDone := done[edge.To]; toDone {
			continue
		}
		if _, fromDone := done[edge.From]; fromDone {
			continue
		}
		pending[edge.To]++
	}

	// 初始就绪：未完成 agent 节点里 pending==0 者，按 nodes[] 出现序（确定性）。END 即便 pending 归 0 也不入队
	// （不执行、只表到达终点）。
	var ready []string
	for _, node := range def.Nodes {
		if _, isDone := done[node.ID]; isDone {
			continue
		}
		if node.IsAgent() && pending[node.ID] == 0 {
			ready = append(ready, node.ID)
		}
	}

	obs.OnSchedule(o.scheduleInfo(def, nodeByID, done, ready))

	results := make(chan nodeResult)
	inflight := 0
	failed := false
	var firstEngineErr *string    // 首个失败节点的 error（run.json 快速排查缓存，已自带节点名）
	var firstFailedNodeID *string // 首个失败节点 id（根因），落进 record.FailedNodeID 供 summary / UI 复用
	var cause error               // 所有引擎 / IO 错误合并上抛，任何一项都不静默丢弃

	for {
		if !failed {
			for len(ready) > 0 {
				id := ready[0]
				ready = ready[1:]
				node := nodeByID[id]
				prompt := workflow.Render(node.PromptTemplate, sysVars, done, validNodeID)
				obs.OnNodeStart(NodeInfo{NodeID: id, DisplayName: node.DisplayName, Engine: node.Engine, Model: modelOf(node.EngineConfig)})
				startedAt := o.Now() // 调度线程内取时刻，避免 worker 并发调 o.Now（注入时钟未必并发安全）
				inflight++
				go func(n workflow.Node, p string, started time.Time) {
					entry, err := o.executeNode(ctx, n, p, cwd, started)
					results <- nodeResult{entry: entry, err: err}
				}(node, prompt, startedAt)
			}
		}
		if inflight == 0 {
			break // 无在途：全完成 或 已失败且在途清空
		}
		res := <-results
		inflight--
		// 失败节点：引擎错误路径常回零耗时（EndedAt==StartedAt），排障最需时间线处失真。在调度线程（o.Now
		// 安全、worker 不碰时钟）补真实结束时刻与耗时，不违反「时钟只在调度线程」的并发约束。
		if res.err != nil && res.entry.DurationMs == 0 {
			ended := o.Now()
			res.entry.EndedAt = ended.Format(time.RFC3339)
			if started, perr := time.Parse(time.RFC3339, res.entry.StartedAt); perr == nil {
				res.entry.DurationMs = ended.Sub(started).Milliseconds()
			}
		}
		appendErr := o.Store.AppendTrace(record.ID, res.entry)
		obs.OnNodeDone(res.entry)

		// 引擎失败或 trace 落盘失败都置 failed、进入 drain（不再启动新节点，收干在途）。
		if res.err != nil || appendErr != nil {
			failed = true
			if res.err != nil && firstEngineErr == nil {
				id := res.entry.NodeID
				firstFailedNodeID = &id
				// 首个失败节点即根因：错误文案自带节点名，令 run.json 与 summary 两处指向同一节点、不再张冠李戴。
				msg := fmt.Sprintf("节点 %s：%s", id, engineErrorText(res.entry))
				firstEngineErr = &msg
			}
			cause = errors.Join(cause, res.err, appendErr)
			continue
		}

		// 成功：记产物、增量落盘、解锁后继。
		done[res.entry.NodeID] = res.entry.Output
		record.Artifacts[res.entry.NodeID] = res.entry.Output
		if err := o.Store.WriteRun(record); err != nil {
			failed = true
			cause = errors.Join(cause, err)
			continue
		}
		for _, next := range succ[res.entry.NodeID] {
			if _, isDone := done[next]; isDone {
				continue
			}
			pending[next]--
			if pending[next] == 0 && nodeByID[next].IsAgent() {
				ready = append(ready, next)
			}
		}
	}

	if failed {
		return o.finalizeFailed(record, firstEngineErr, firstFailedNodeID, cause)
	}
	return o.finalizeCompleted(record)
}

// engineErrorText 取节点引擎失败信息；res.err != nil 时 entry.Error 必已填，兜底空串防 nil 解引用。
func engineErrorText(entry run.TraceEntry) string {
	if entry.Error != nil {
		return *entry.Error
	}
	return ""
}

// scheduleInfo 组装 Observer 的开跑概述：分母、初始就绪集、resume 已完成的 agent 节点数。
func (o *Orchestrator) scheduleInfo(def *workflow.Definition, nodeByID map[string]workflow.Node,
	done map[string]string, ready []string) ScheduleInfo {
	resumeDoneCount := 0
	for id := range done {
		if nodeByID[id].IsAgent() {
			resumeDoneCount++
		}
	}
	initialReady := make([]NodeBrief, 0, len(ready))
	for _, id := range ready {
		initialReady = append(initialReady, NodeBrief{NodeID: id, DisplayName: nodeByID[id].DisplayName})
	}
	return ScheduleInfo{
		AgentNodeCount:  def.AgentNodeCount(),
		InitialReady:    initialReady,
		ResumeDoneCount: resumeDoneCount,
	}
}

// executeNode 渲染好的 prompt 交引擎执行、组装该节点的 trace 条目。返回的 error 非 nil 表示引擎失败
// （此时 entry.Success=false、entry.Error 已填）。endedAt 由 startedAt + 引擎耗时推出，使所有 o.Now() 调用
// 都留在调度线程（worker 不碰时钟），既无时钟竞态、又对注入时钟确定。本函数在 worker goroutine 内跑，
// 只读入参、不碰共享态。
func (o *Orchestrator) executeNode(ctx context.Context, node workflow.Node, prompt, cwd string, startedAt time.Time) (run.TraceEntry, error) {
	entry := run.TraceEntry{
		NodeID:       node.ID,
		DisplayName:  node.DisplayName,
		Engine:       node.Engine,
		EngineConfig: node.EngineConfig,
		Input:        prompt,
		StartedAt:    startedAt.Format(time.RFC3339),
	}
	result, err := o.invokeEngine(ctx, node.Engine, node.EngineConfig, prompt, cwd)
	entry.DurationMs = result.DurationMilliseconds
	entry.EndedAt = startedAt.Add(time.Duration(result.DurationMilliseconds) * time.Millisecond).Format(time.RFC3339)
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

// modelOf 返回节点声明的模型（空串＝引擎默认）。
func modelOf(config *workflow.EngineConfig) string {
	if config == nil {
		return ""
	}
	return config.Model
}
