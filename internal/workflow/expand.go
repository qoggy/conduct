package workflow

// 执行步骤的两种类型（ExecutionStep.Type / trace 的 type 字段取值）。集中定义避免魔法串拼错。
const (
	StepTypeAgent     = "agent"     // 节点主体
	StepTypeEvaluator = "evaluator" // 评测官
)

// ExecutionStep 是展开后的单个执行步骤。
type ExecutionStep struct {
	Type      string // StepTypeAgent | StepTypeEvaluator
	NodeID    string
	Iteration int
}

// Expand 把节点图展开成确定性的线性执行序列，含两种循环模式：
//   - evaluator（in-place 内循环）：同一节点「写 → 评 → 改」重复 loopCount 轮，末轮不再评；
//   - redoTarget（jump-back 段循环）：本节点跑完跳回 redoTarget，把二者之间的整段重跑 loopCount 轮。
//
// 从 Python 原型 paw_workflow.py:expand_workflow 移植，保持相同语义（含对前向 / 非法
// redoTarget 退化为单次的兜底）。假定节点结构已通过 Validate；对非法输入仍尽量优雅降级。
func Expand(nodes []Node) []ExecutionStep {
	steps := []ExecutionStep{}
	indexByID := make(map[string]int, len(nodes))
	for position, node := range nodes {
		indexByID[node.ID] = position
	}

	// 处于 jump-back 段内的子节点 id：由段循环代为展开，外层遍历跳过以免重复。
	redoRangeNodeIDs := make(map[string]bool)
	for position, node := range nodes {
		if node.RedoTarget == "" {
			continue
		}
		targetPosition, ok := indexByID[node.RedoTarget]
		if !ok || targetPosition >= position {
			continue
		}
		for segmentPosition := targetPosition; segmentPosition < position; segmentPosition++ {
			redoRangeNodeIDs[nodes[segmentPosition].ID] = true
		}
	}

	// pushNodeUnit 展开单个节点的完整语义（含其自身的 in-place 内循环）。
	// outerIteration==0 表示「未设」，用 runIndex+1 作迭代号；否则统一用 outerIteration（段循环轮号）。
	pushNodeUnit := func(node Node, outerIteration int) {
		isInPlace := node.Evaluator != nil
		agentRuns := 1
		if isInPlace {
			agentRuns = loopCountOf(node) + 1
		}
		for runIndex := 0; runIndex < agentRuns; runIndex++ {
			iteration := outerIteration
			if outerIteration == 0 {
				iteration = runIndex + 1
			}
			steps = append(steps, ExecutionStep{Type: StepTypeAgent, NodeID: node.ID, Iteration: iteration})
			// 末尾 agent 之后不再 eval（没有下一次 agent 消费反馈）。
			if isInPlace && runIndex < agentRuns-1 {
				steps = append(steps, ExecutionStep{Type: StepTypeEvaluator, NodeID: node.ID, Iteration: iteration})
			}
		}
	}

	for position, node := range nodes {
		if redoRangeNodeIDs[node.ID] {
			continue
		}
		if node.RedoTarget != "" {
			targetPosition, ok := indexByID[node.RedoTarget]
			if !ok || targetPosition >= position { // 前向 / 非法 redoTarget → 退化为单次
				steps = append(steps, ExecutionStep{Type: StepTypeAgent, NodeID: node.ID, Iteration: 1})
				continue
			}
			segmentRuns := loopCountOf(node) + 1
			for loop := 1; loop <= segmentRuns; loop++ {
				for segmentPosition := targetPosition; segmentPosition < position; segmentPosition++ {
					pushNodeUnit(nodes[segmentPosition], loop) // 段内子节点按自身完整语义展开
				}
				steps = append(steps, ExecutionStep{Type: StepTypeAgent, NodeID: node.ID, Iteration: loop}) // 段尾节点本身
			}
		} else {
			pushNodeUnit(node, 0)
		}
	}
	return steps
}

// loopCountOf 返回节点的循环轮数，未设时默认为 1。
func loopCountOf(node Node) int {
	if node.LoopCount != nil {
		return *node.LoopCount
	}
	return 1
}
