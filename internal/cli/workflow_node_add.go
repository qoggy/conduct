package cli

import (
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// newWorkflowNodeAddCommand 构造 `conduct workflow node add <name> <id>`：建一个 agent 节点并连边，一步落地。
// 不给 --from / --to 时自动接成 START → <id> → END（裸节点即合法、默认就是「一开始就并行」的一员）；
// --from / --to 各自给出则该侧按指定连、缺省的一侧仍自动接 START / END。内存建好后复用整份定义的同一套校验再落盘。
func newWorkflowNodeAddCommand() *cobra.Command {
	var engineFlag, displayNameFlag, promptFlag, fromFlag, toFlag string
	var modelFlag, effortFlag, reasoningEffortFlag string
	cmd := &cobra.Command{
		Use:   "add <name> <id>",
		Short: "建一个 agent 节点并连边（缺省自动接 START→<id>→END）",
		Long: "建一个 agent 节点并连边。不给 --from / --to 时自动接成 START → <id> → END；\n" +
			"--from a,b 让入边来自 a、b，--to c 让出边到 c。\n" +
			"<id> 不得为保留名 START / END；下列任一约束不满足即整条拒绝、原文件不变。\n" +
			graphConstraintsHelp + "\n" +
			templateVariablesHelp,
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			flags := cmd.Flags()
			if !flags.Changed("engine") || !flags.Changed("display-name") {
				return usageErrorf("--engine 与 --display-name 必填")
			}
			// id：保留名退 1、格式非法退 2（与 spec〈node add〉退出码约定一致）。
			if id == workflow.NodeIDStart || id == workflow.NodeIDEnd {
				return fmt.Errorf("节点 id 不得为保留名 %s / %s", workflow.NodeIDStart, workflow.NodeIDEnd)
			}
			if !workflow.IsValidNodeID(id) {
				return usageErrorf("节点 id %q 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）", id)
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
					return fmt.Errorf("节点 %s 已存在", id)
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
				EngineConfig:   buildEngineConfig(modelFlag, effortFlag, reasoningEffortFlag),
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
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已加节点 %s·%s\n", name, id)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&engineFlag, "engine", "", "引擎（claude-code / antigravity / qoder / codex），必填")
	f.StringVar(&displayNameFlag, "display-name", "", "节点显示名，必填、须非空")
	f.StringVar(&fromFlag, "from", "", "入边来源，逗号分隔的一个或多个已存在节点 id（可含 START）；给了就不再自动接 START")
	f.StringVar(&toFlag, "to", "", "出边去向，逗号分隔的一个或多个已存在节点 id（可含 END）；给了就不再自动接 END")
	f.StringVar(&promptFlag, "prompt", "", "提示词（默认 {{sys.userPrompt}}）；复杂 / 多行改用 node set-prompt 读 stdin")
	f.StringVar(&modelFlag, "model", "", "模型（受 engine 约束）")
	f.StringVar(&effortFlag, "effort", "", fmt.Sprintf("claude-code 档位（%s）", effortEnum("claude-code")))
	f.StringVar(&reasoningEffortFlag, "reasoning-effort", "", fmt.Sprintf("qoder / codex 推理档位（qoder：%s；codex：%s）", effortEnum("qoder"), effortEnum("codex")))
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

// buildEngineConfig 从三个调优字段组装 EngineConfig；三者皆空返回 nil（保持规范化形态干净）。
func buildEngineConfig(model, effort, reasoningEffort string) *workflow.EngineConfig {
	if model == "" && effort == "" && reasoningEffort == "" {
		return nil
	}
	return &workflow.EngineConfig{Model: model, Effort: effort, ReasoningEffort: reasoningEffort}
}
