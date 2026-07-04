package cli

import (
	"fmt"
	"io"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowShowCommand() *cobra.Command {
	var expand, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "查看单个工作流（可附展开预览）",
		Args:  requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			def, err := st.Load(name)
			if err != nil {
				return err
			}
			if err := workflow.Validate(def); err != nil {
				return err // 载入即校验，防手改损坏
			}
			if asJSON {
				return printShowJSON(cmd, def, expand)
			}
			return printShowHuman(cmd, def, expand)
		},
	}
	cmd.Flags().BoolVar(&expand, "expand", false, "追加打印展开后的执行步骤")
	cmd.Flags().BoolVar(&asJSON, "json", false, "以机器可读 JSON 输出规范化定义")
	return cmd
}

func printShowHuman(cmd *cobra.Command, def *workflow.Definition, expand bool) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s · %d 节点\n", def.Name, len(def.Nodes))
	for _, node := range def.Nodes {
		fmt.Fprintf(out, "%s · %s · %s · %s · %s\n",
			node.ID, node.DisplayName, node.Engine, modelDisplay(node.EngineConfig), loopModeDisplay(node))
	}
	if expand {
		fmt.Fprintln(out)
		printExpansion(out, def)
	}
	return nil
}

// modelDisplay 返回节点 model 的展示串，未指定则标「(引擎默认)」。
func modelDisplay(config *workflow.EngineConfig) string {
	if config == nil || config.Model == "" {
		return "(引擎默认)"
	}
	return config.Model
}

// loopModeDisplay 返回节点的循环模式展示串。
func loopModeDisplay(node workflow.Node) string {
	switch {
	case node.Evaluator != nil:
		return "evaluator 内循环"
	case node.RedoTarget != "":
		return "redoTarget→" + node.RedoTarget + " 回跳"
	default:
		return "单次"
	}
}

func printExpansion(out io.Writer, def *workflow.Definition) {
	steps := workflow.Expand(def.Nodes)
	fmt.Fprintf(out, "▶ 展开为 %d 步：\n", len(steps))
	for index, step := range steps {
		fmt.Fprintf(out, "  [%d] %-9s node=%-10s iter=%d\n", index, step.Type, step.NodeID, step.Iteration)
	}
}

// expandedStep 是 show --json 里附带的展开步骤条目。
type expandedStep struct {
	StepIndex int    `json:"stepIndex"`
	Type      string `json:"type"`
	NodeID    string `json:"nodeId"`
	Iteration int    `json:"iteration"`
}

func printShowJSON(cmd *cobra.Command, def *workflow.Definition, expand bool) error {
	def.Normalize() // 输出规范化形态（补默认值）
	if !expand {
		return printJSON(cmd, def)
	}
	steps := workflow.Expand(def.Nodes)
	expanded := make([]expandedStep, len(steps))
	for index, step := range steps {
		expanded[index] = expandedStep{StepIndex: index, Type: step.Type, NodeID: step.NodeID, Iteration: step.Iteration}
	}
	payload := struct {
		*workflow.Definition
		Expanded []expandedStep `json:"expanded"`
	}{Definition: def, Expanded: expanded}
	return printJSON(cmd, payload)
}
