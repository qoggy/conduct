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
		Short: "查看单个工作流的 DAG 详情（可附拓扑分层预览）",
		Long: "查看单个工作流的 DAG 详情——agent 节点清单 + 边邻接（标注 START / END）。\n" +
			"--expand 追加拓扑分层预览（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）。\n" +
			"--json 输出规范化的完整记录（含 name / 时间戳元数据与 definition），--json --expand 额外挂 levels 字段。",
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
			if asJSON {
				return printShowJSON(cmd, wf, expand)
			}
			return printShowHuman(cmd, wf, expand)
		},
	}
	cmd.Flags().BoolVar(&expand, "expand", false, "追加打印拓扑分层预览")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以机器可读 JSON 输出规范化完整记录")
	return cmd
}

func printShowHuman(cmd *cobra.Command, wf *workflow.Workflow, expand bool) error {
	out := cmd.OutOrStdout()
	def := &wf.Definition
	fmt.Fprintf(out, "%s · %d 节点\n", wf.Name, def.AgentNodeCount())
	for _, node := range def.Nodes {
		if node.IsAgent() {
			fmt.Fprintf(out, "%s · %s · %s · %s\n",
				node.ID, node.DisplayName, node.Engine, modelDisplay(node.EngineConfig))
		}
	}
	fmt.Fprintln(out, "\n边：")
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
	fmt.Fprintln(out, "拓扑分层（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）：")
	for i, level := range workflow.TopoLevels(def) {
		fmt.Fprintf(out, "  level %d: [%s]\n", i, strings.Join(level, ", "))
	}
}

// modelDisplay 返回节点 model 的展示串，未指定则标「(引擎默认)」。
func modelDisplay(config *workflow.EngineConfig) string {
	if config == nil || config.Model == "" {
		return "(引擎默认)"
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
