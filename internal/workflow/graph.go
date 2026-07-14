package workflow

// graph.go 集中放纯图算法，供 validate / scheduler / show / UI 校验复用。
// 所有函数以 Definition 的 nodes（含 START/END）与 edges 为输入，不改动入参。

// BuildAdjacency 返回后继表 succ（from → []to）与前驱表 pred（to → []from），
// 均按 edges 出现顺序追加、按 node id 索引。只收录 edges 里出现的端点键，孤立节点不建键。
func BuildAdjacency(def *Definition) (succ, pred map[string][]string) {
	succ = make(map[string][]string, len(def.Nodes))
	pred = make(map[string][]string, len(def.Nodes))
	for _, edge := range def.Edges {
		succ[edge.From] = append(succ[edge.From], edge.To)
		pred[edge.To] = append(pred[edge.To], edge.From)
	}
	return succ, pred
}

// DetectCycle 用 DFS 三色标记探测环：命中返回一条环路径（如 ["a","b","a"]，首尾同 id 便于打印
// "a→b→a"）；无环返回 nil。按 nodes 顺序起 DFS，结果确定。仅依赖 edges 拓扑，与节点是否为标记节点无关。
func DetectCycle(def *Definition) []string {
	succ, _ := BuildAdjacency(def)
	const (
		white = 0 // 未访问
		gray  = 1 // 在当前 DFS 栈上
		black = 2 // 已完成
	)
	color := make(map[string]int, len(def.Nodes))
	var stack []string

	var dfs func(id string) []string
	dfs = func(id string) []string {
		color[id] = gray
		stack = append(stack, id)
		for _, next := range succ[id] {
			switch color[next] {
			case gray:
				// 命中回边：截取栈上从 next 到当前的片段，补上 next 收尾成闭环路径。
				for i, node := range stack {
					if node == next {
						cycle := append([]string{}, stack[i:]...)
						return append(cycle, next)
					}
				}
			case white:
				if cycle := dfs(next); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		return nil
	}

	for _, node := range def.Nodes {
		if color[node.ID] == white {
			if cycle := dfs(node.ID); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// Ancestors 返回从 id 沿边反向可达的全部前驱节点 id 集合（不含 id 自身）。
// 供祖先引用校验：模板 {{X}} 的 X 须是本节点祖先。可能含 START（若可达），调用方按需过滤标记节点。
// 假定图无环（有环时"祖先"无定义），故调用方应在 DetectCycle 通过后再调用。
func Ancestors(def *Definition, id string) map[string]bool {
	_, pred := BuildAdjacency(def)
	ancestors := make(map[string]bool)
	var visit func(node string)
	visit = func(node string) {
		for _, parent := range pred[node] {
			if !ancestors[parent] {
				ancestors[parent] = true
				visit(parent)
			}
		}
	}
	visit(id)
	return ancestors
}

// TopoLevels 返回 agent 节点的拓扑分层（同层可并行、不含 START / END），供 show --expand 预览。
// 层号算法：node 层号 = max(各前驱层号) + 1，START 视作 −1 层（以 START 为唯一前驱者落 level 0）；
// END 不计入。同层节点按其在 nodes[] 的出现序排列，跨次运行稳定。假定图无环（调用前应 DetectCycle 通过）。
func TopoLevels(def *Definition) [][]string {
	succ, pred := BuildAdjacency(def)

	// Kahn 拓扑序：入度 0 起，逐层松弛出层号。
	indeg := make(map[string]int, len(def.Nodes))
	for _, node := range def.Nodes {
		indeg[node.ID] = len(pred[node.ID])
	}
	level := make(map[string]int, len(def.Nodes))
	queue := make([]string, 0, len(def.Nodes))
	for _, node := range def.Nodes {
		if indeg[node.ID] == 0 {
			level[node.ID] = -1 // 源（START）视作 −1 层，其后继落 level 0
			queue = append(queue, node.ID)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range succ[current] {
			if candidate := level[current] + 1; candidate > level[next] {
				level[next] = candidate
			}
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	// 按层号聚合 agent 节点，同层保持 nodes[] 出现序。
	maxLevel := -1
	for _, node := range def.Nodes {
		if node.IsAgent() && level[node.ID] > maxLevel {
			maxLevel = level[node.ID]
		}
	}
	if maxLevel < 0 {
		return nil
	}
	levels := make([][]string, maxLevel+1)
	for _, node := range def.Nodes {
		if node.IsAgent() {
			lvl := level[node.ID]
			// 无入边的 agent 落在 −1 层（合法图里只有 START 无入边，agent 无入边即非法输入）：
			// 跳过而非 levels[-1] 越界 panic。TopoLevels 约定输入已过校验，此处仅作防御。
			if lvl < 0 {
				continue
			}
			levels[lvl] = append(levels[lvl], node.ID)
		}
	}
	return levels
}

// AgentNodeIDs 返回 agent 节点 id 流（排除 START / END），按确定性拓扑序（先层号、同层再按 nodes[] 出现序）。
// 异常图（拓扑分层未覆盖全部 agent 节点，如手改成环）按 nodes[] 出现序兜底补上，不丢节点。CLI `workflow list`
// 与 UI 运行详情共用此单一实现——两端节点流保证一致、不漂移。空定义返回空切片（JSON 里为 []、不为 null）。
func AgentNodeIDs(def *Definition) []string {
	ids := make([]string, 0, len(def.Nodes))
	seen := make(map[string]bool, len(def.Nodes))
	for _, level := range TopoLevels(def) {
		for _, id := range level {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for _, node := range def.Nodes {
		if node.IsAgent() && !seen[node.ID] {
			ids = append(ids, node.ID)
		}
	}
	return ids
}
