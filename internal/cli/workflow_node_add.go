package cli

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeAddCommand 构造 `conduct workflow node add <name> <id>`：建一个 agent 节点并连边，一步落地。
// 不给 --from / --to 时自动接成 START → <id> → END（裸节点即合法、默认就是「一开始就并行」的一员）；
// --from / --to 各自给出则该侧按指定连、缺省的一侧仍自动接 START / END。内存建好后复用整份定义的同一套校验再落盘。
func newWorkflowNodeAddCommand() *cobra.Command {
	var engineFlag, displayNameFlag, promptFlag, fromFlag, toFlag string
	var modelFlag, effortFlag string
	cmd := &cobra.Command{
		Use:   "add <name> <id>",
		Short: localizedHelpText("建一个 agent 节点并连边（缺省自动接 START→<id>→END）", "Create an agent node and connect edges (defaults to START→<id>→END)"),
		Long: localizedHelpText(
			"建一个 agent 节点并连边。不给 --from / --to 时自动接成 START → <id> → END；\n"+
				"--from a,b 让入边来自 a、b，--to c 让出边到 c。\n"+
				"<id> 不得为保留名 START / END；下列任一约束不满足即整条拒绝、原文件不变。\n",
			"Create an agent node and connect edges. Without --from / --to, connect it automatically as START → <id> → END;\n"+
				"--from a,b makes incoming edges come from a and b, while --to c makes an outgoing edge go to c.\n"+
				"<id> must not be the reserved name START / END; if any constraint below is unsatisfied, reject the entire operation and leave the original file unchanged.\n",
		) +
			graphConstraintsHelp() + "\n" +
			templateVariablesHelp(),
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			flags := cmd.Flags()
			if !flags.Changed("engine") || !flags.Changed("display-name") {
				return localizedUsageErrorf("--engine 与 --display-name 必填", "--engine and --display-name are required")
			}
			// id：保留名退 1、格式非法退 2（与 spec〈node add〉退出码约定一致）。
			if id == workflow.NodeIDStart || id == workflow.NodeIDEnd {
				return apperror.New(apperror.CodeReservedNodeID, apperror.Params{"id": id})
			}
			if !workflow.IsValidNodeID(id) {
				return &usageError{err: apperror.New(apperror.CodeInvalidNodeID, apperror.Params{"id": id})}
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			wf, err := st.Load(name)
			if err != nil {
				return err
			}
			for _, node := range wf.Definition.Nodes {
				if node.ID == id {
					return apperror.New(apperror.CodeNodeAlreadyExists, apperror.Params{"id": id})
				}
			}

			prompt := promptFlag
			if !flags.Changed("prompt") {
				prompt = "{{sys.userPrompt}}"
			}
			wf.Definition.Nodes = append(wf.Definition.Nodes, workflow.Node{
				ID:             id,
				DisplayName:    displayNameFlag,
				Engine:         engineFlag,
				PromptTemplate: prompt,
				EngineConfig:   buildEngineConfig(modelFlag, effortFlag),
			})
			// --from / --to 各自缺省接 START / END；给了就按指定连（可含 START / END）。
			froms := splitEndpoints(fromFlag, workflow.NodeIDStart, flags.Changed("from"))
			tos := splitEndpoints(toFlag, workflow.NodeIDEnd, flags.Changed("to"))
			for _, from := range froms {
				wf.Definition.Edges = append(wf.Definition.Edges, workflow.Edge{From: from, To: id})
			}
			for _, to := range tos {
				wf.Definition.Edges = append(wf.Definition.Edges, workflow.Edge{From: id, To: to})
			}

			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 整份校验：成环 / 孤立 / 引用非祖先 / from·to 不存在等在此退 1，原文件不变
			}
			if err := st.Save(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已加节点 %s·%s\n", "✓ Added node %s·%s\n"), name, id)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&engineFlag, "engine", "", fmt.Sprintf(localizedHelpText("引擎（%s），必填", "Engine (%s), required"), engineNamesHelp()))
	f.StringVar(&displayNameFlag, "display-name", "", localizedHelpText("节点显示名，必填、须非空", "Node display name, required and nonempty"))
	f.StringVar(&fromFlag, "from", "", localizedHelpText(
		"入边来源，逗号分隔的一个或多个已存在节点 id（可含 START）；给了就不再自动接 START",
		"Incoming-edge sources: one or more existing node ids separated by commas (may include START); when supplied, do not connect START automatically",
	))
	f.StringVar(&toFlag, "to", "", localizedHelpText(
		"出边去向，逗号分隔的一个或多个已存在节点 id（可含 END）；给了就不再自动接 END",
		"Outgoing-edge destinations: one or more existing node ids separated by commas (may include END); when supplied, do not connect END automatically",
	))
	f.StringVar(&promptFlag, "prompt", "", localizedHelpText(
		"提示词（默认 {{sys.userPrompt}}）；复杂 / 多行改用 node set-prompt 读 stdin",
		"Prompt (default {{sys.userPrompt}}); for complex / multiline content, use node set-prompt to read stdin",
	))
	f.StringVar(&modelFlag, "model", "", localizedHelpText("模型（受 engine 约束）", "Model (constrained by engine)"))
	f.StringVar(&effortFlag, "effort", "", fmt.Sprintf(localizedHelpText("推理档位（%s）", "Reasoning effort level (%s)"), effortValuesHelp()))
	return cmd
}

// splitEndpoints 解析 --from / --to 的逗号分隔端点列表；未给出（changed=false）时回落 fallback（START / END）。
// 给出时按逗号切分、逐项 trim、丢空项。
func splitEndpoints(value, fallback string, changed bool) []string {
	if !changed {
		return []string{fallback}
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// buildEngineConfig 从调优字段组装 EngineConfig；字段皆空返回 nil（保持规范化形态干净）。
func buildEngineConfig(model, effort string) *workflow.EngineConfig {
	if model == "" && effort == "" {
		return nil
	}
	return &workflow.EngineConfig{Model: model, Effort: effort}
}
