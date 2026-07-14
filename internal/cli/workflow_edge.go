package cli

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// edgeItem 是 edge 列出（--json）时的条目。
type edgeItem struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// newWorkflowEdgeCommand 构造 `conduct workflow edge <name>`：不带 --add/--rm 时列出全部边，带则原子批量改边。
// 列出走无 flag 的默认路径（而非独立 list 子命令），避免名为 "list" 的工作流被 cobra 抢路由；--add/--rm 作为
// 一次原子事务生效，支持 a→b→c 改 a→c→b 这类需同时删旧加新的调序变更。
func newWorkflowEdgeCommand() *cobra.Command {
	var adds, rms []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "edge <name>",
		Short: "列出或增删工作流的边（--add / --rm，多改动原子生效）",
		Long: "不带 --add / --rm 时列出该工作流的全部边（含 START / END 连边）。\n" +
			"用 --add / --rm 增删边时，多个改动作为一次原子事务生效——适合 a→b→c 改 a→c→b 这类需同时删旧加新的调序。\n" +
			"删不存在的边、加已存在的边都会报错退出；同一条边同时 --add 与 --rm 视为先删后加、保留该边、不报重复。",
		Example: "  # 列出全部边\n" +
			"  conduct workflow edge myflow\n" +
			"  # 把 s1→s2→END 调序成 s1→s3→END\n" +
			"  conduct workflow edge myflow --rm s1:s2 --rm s2:END --add s1:s3 --add s3:END",
		Args: requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			wf, err := st.Load(name)
			if err != nil {
				return err
			}
			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 载入即校验，防手改损坏
			}

			// 无 --add/--rm：列出当前全部边。
			if len(adds) == 0 && len(rms) == 0 {
				return printEdges(cmd, wf.Definition.Edges, asJSON)
			}

			addEdges, err := parseEdgeSpecs(adds)
			if err != nil {
				return err
			}
			rmEdges, err := parseEdgeSpecs(rms)
			if err != nil {
				return err
			}
			target, err := applyEdgeChanges(wf.Definition.Edges, addEdges, rmEdges)
			if err != nil {
				return err // 删不存在 / 加已存在 → 退 1
			}
			wf.Definition.Edges = target

			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 整份校验：成环 / 自环 / 指向 START / 源自 END / 孤立 / 引用非祖先等在此退 1，原文件不变
			}
			if err := st.Save(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已更新 %s 边（+%d -%d）\n", name, len(addEdges), len(rmEdges))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&adds, "add", nil, "加一条边 <from:to>（可重复；from / to 可为 START / END）")
	cmd.Flags().StringArrayVar(&rms, "rm", nil, "删一条边 <from:to>（可重复）")
	cmd.Flags().BoolVar(&asJSON, "json", false, "列出边时以机器可读 JSON 输出（每项 {from,to}）；改边时无效")
	return cmd
}

// printEdges 列出一份工作流的全部边（含 START / END 连边）：--json 输出 [{from,to}]，否则逐行 from → to。
func printEdges(cmd *cobra.Command, edges []workflow.Edge, asJSON bool) error {
	if asJSON {
		items := make([]edgeItem, 0, len(edges))
		for _, edge := range edges {
			items = append(items, edgeItem{From: edge.From, To: edge.To})
		}
		return printJSON(cmd, items)
	}
	out := cmd.OutOrStdout()
	for _, edge := range edges {
		fmt.Fprintf(out, "%s → %s\n", edge.From, edge.To)
	}
	return nil
}

// edgeKey 是边的去重键（node id 不含 \x00，故安全）。
func edgeKey(e workflow.Edge) string { return e.From + "\x00" + e.To }

// parseEdgeSpecs 把 "from:to" 列表解析为边；格式非法（无 : / 空端点）报用法错误退 2。
func parseEdgeSpecs(specs []string) ([]workflow.Edge, error) {
	edges := make([]workflow.Edge, 0, len(specs))
	for _, spec := range specs {
		from, to, found := strings.Cut(spec, ":")
		if !found || from == "" || to == "" {
			return nil, usageErrorf("边格式非法 %q（应为 from:to，两端非空）", spec)
		}
		edges = append(edges, workflow.Edge{From: from, To: to})
	}
	return edges, nil
}

// applyEdgeChanges 算出目标边集：--rm 对当前边集判定（删不存在退 1）、--add 对「当前 − rm」判定（加已存在退 1）。
// 同一条边同时 add + rm 等价先删后加、结果保留、不报重复。目标顺序 = 保留的当前边（原序）后接新增边（--add 序），确定性。
func applyEdgeChanges(current, adds, rms []workflow.Edge) ([]workflow.Edge, error) {
	currentSet := make(map[string]bool, len(current))
	for _, edge := range current {
		currentSet[edgeKey(edge)] = true
	}
	rmSet := make(map[string]bool, len(rms))
	for _, edge := range rms {
		key := edgeKey(edge)
		if !currentSet[key] {
			return nil, fmt.Errorf("删不存在的边 %s→%s", edge.From, edge.To)
		}
		rmSet[key] = true
	}
	// afterRm：保留当前集里未被 --rm 删除的边（原序）。
	afterRm := make([]workflow.Edge, 0, len(current))
	afterRmSet := make(map[string]bool, len(current))
	for _, edge := range current {
		if rmSet[edgeKey(edge)] {
			continue
		}
		afterRm = append(afterRm, edge)
		afterRmSet[edgeKey(edge)] = true
	}
	// --add 对「当前 − rm」判定：已存在则报重复（与 --rm 对称，不静默去重）。
	for _, edge := range adds {
		if afterRmSet[edgeKey(edge)] {
			return nil, fmt.Errorf("加已存在的边 %s→%s", edge.From, edge.To)
		}
		afterRm = append(afterRm, edge)
		afterRmSet[edgeKey(edge)] = true // 拦住同批 --add 内的重复（否则落到整份校验的重复边）
	}
	return afterRm, nil
}
