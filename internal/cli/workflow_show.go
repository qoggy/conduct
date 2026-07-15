package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowShowCommand() *cobra.Command {
	var expand, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: localizedHelpText("查看单个工作流的 DAG 详情（可附拓扑分层预览）", "Show a workflow's DAG details (optionally with a topological-level preview)"),
		Long: localizedHelpText(
			"查看单个工作流的 DAG 详情——agent 节点清单 + 边邻接（标注 START / END）。\n"+
				"--expand 追加拓扑分层预览（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）。\n"+
				"--json 输出规范化的完整记录（含 name / 时间戳元数据与 definition），--json --expand 额外挂 levels 字段。",
			"Show a workflow's DAG details—an agent-node list plus edge adjacency (with START / END marked).\n"+
				"--expand appends a topological-level preview (nodes at the same level may run in parallel; actual scheduling is greedy, and each node starts as soon as its own dependencies are ready).\n"+
				"--json outputs the normalized complete record (including name / timestamp metadata and definition); --json --expand also adds a levels field.",
		),
		Args: exactArgs(1),
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
			if asJSON {
				return printShowJSON(cmd, wf, expand)
			}
			return printShowHuman(cmd, wf, expand)
		},
	}
	cmd.Flags().BoolVar(&expand, "expand", false, localizedHelpText("追加打印拓扑分层预览", "Append a topological-level preview"))
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText("以机器可读 JSON 输出规范化完整记录", "Output the normalized complete record as machine-readable JSON"))
	return cmd
}

func printShowHuman(cmd *cobra.Command, wf *workflow.Workflow, expand bool) error {
	out := cmd.OutOrStdout()
	def := &wf.Definition
	fmt.Fprintf(out, localizedHelpText("%s · %d 节点\n", "%s · %d nodes\n"), wf.Name, def.AgentNodeCount())
	for _, node := range def.Nodes {
		if node.IsAgent() {
			fmt.Fprintf(out, "%s · %s · %s · %s\n",
				node.ID, node.DisplayName, node.Engine, modelDisplay(node.EngineConfig))
		}
	}
	fmt.Fprintln(out, localizedHelpText("\n边：", "\nEdges:"))
	for _, edge := range def.Edges {
		fmt.Fprintf(out, "  %s → %s\n", edge.From, edge.To)
	}
	if expand {
		fmt.Fprintln(out)
		printTopoLevels(out, def)
	}
	return nil
}

// printTopoLevels 打印拓扑分层（逐层一行 level i: [a, b, …]）。
func printTopoLevels(out io.Writer, def *workflow.Definition) {
	fmt.Fprintln(out, localizedHelpText("拓扑分层（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）：", "Topological levels (nodes in one level may run in parallel; actual scheduling is greedy and starts each node as soon as its dependencies are ready):"))
	for i, level := range workflow.TopoLevels(def) {
		fmt.Fprintf(out, localizedHelpText("  第 %d 层：[%s]\n", "  level %d: [%s]\n"), i, strings.Join(level, ", "))
	}
}

// modelDisplay 返回节点 model 的展示串，未指定则标「(引擎默认)」。
func modelDisplay(config *workflow.EngineConfig) string {
	if config == nil || config.Model == "" {
		return localizedHelpText("(引擎默认)", "(engine default)")
	}
	return config.Model
}

func printShowJSON(cmd *cobra.Command, wf *workflow.Workflow, expand bool) error {
	if !expand {
		return printJSON(cmd, wf)
	}
	levels := workflow.TopoLevels(&wf.Definition)
	if levels == nil {
		levels = [][]string{}
	}
	payload := struct {
		*workflow.Workflow
		Levels [][]string `json:"levels"`
	}{Workflow: wf, Levels: levels}
	return printJSON(cmd, payload)
}
