package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// nodeSetOptions 是 node set 的一次调用意图，用指针显式携带「是否给出」：
// *string 为 nil 表示该 flag 本次未出现（不动），非 nil 表示给出（含空串清除语义）。
type nodeSetOptions struct {
	NewID           *string
	Engine          *string
	Model           *string
	Effort          *string
	ReasoningEffort *string
	DisplayName     *string
}

// checkNodeSetFlagCombo 校验至少给一个字段选项（否则退 2）。不依赖 store / Cobra，可直测。
func checkNodeSetFlagCombo(opts nodeSetOptions) error {
	if opts.NewID == nil && opts.Engine == nil && opts.Model == nil && opts.Effort == nil &&
		opts.ReasoningEffort == nil && opts.DisplayName == nil {
		return localizedUsageErrorf("至少给一个字段选项（--id / --engine / --model / --effort / --reasoning-effort / --display-name）", "provide at least one field option (--id / --engine / --model / --effort / --reasoning-effort / --display-name)")
	}
	return nil
}

// applyEngineConfig 把 opts 里给出的 model / effort / reasoning-effort 施加到 *carrier 指向的 EngineConfig 指针：
// 空串清除对应字段；施加后若三字段全空则把指针置 nil（保持规范化形态干净）；三者皆未给出则原样不动。
func applyEngineConfig(carrier **workflow.EngineConfig, opts nodeSetOptions) {
	if opts.Model == nil && opts.Effort == nil && opts.ReasoningEffort == nil {
		return
	}
	config := *carrier
	if config == nil {
		config = &workflow.EngineConfig{}
	}
	if opts.Model != nil {
		config.Model = *opts.Model
	}
	if opts.Effort != nil {
		config.Effort = *opts.Effort
	}
	if opts.ReasoningEffort != nil {
		config.ReasoningEffort = *opts.ReasoningEffort
	}
	if config.Model == "" && config.Effort == "" && config.ReasoningEffort == "" {
		*carrier = nil
	} else {
		*carrier = config
	}
}

// applyNodeSet 在内存中把 opts 施加到 def 里 id 指定的 agent 节点（就地改）。
// 只做命令级字段级前置校验（displayName 非空，退 1）；引擎兼容性等交整份 workflow.Validate 兜底。
func applyNodeSet(def *workflow.Definition, id string, opts nodeSetOptions) error {
	node, err := requireAgentNode(def, id)
	if err != nil {
		return err
	}
	if opts.Engine != nil {
		node.Engine = *opts.Engine
	}
	applyEngineConfig(&node.EngineConfig, opts)
	if opts.DisplayName != nil {
		if *opts.DisplayName == "" {
			return apperror.New(apperror.CodeNodeDisplayNameRequired, apperror.Params{"id": id})
		}
		node.DisplayName = *opts.DisplayName
	}
	// 改 id 放最后：级联同步所有边端点与模板 {{id}} 引用（node 指针指向切片元素、原地改，上面对它的
	// 字段赋值不受影响）。校验不过（重名 / 保留名 / 非法）返回 error、def 已改的其它字段不落盘（RunE
	// 里整份 Validate 后才 Save，任一步 error 都不写文件）。
	if opts.NewID != nil {
		if err := workflow.RenameNodeID(def, id, *opts.NewID); err != nil {
			return err
		}
	}
	return nil
}

// newWorkflowNodeSetCommand 构造 `conduct workflow node set <name> <id>`：改 agent 节点的结构化字段（id / 引擎 /
// 模型 / 档位 / 显示名）。改 id 会级联同步所有边端点与模板 {{id}} 引用。提示词走 node set-prompt；增删节点走
// node add / node rm；改边走 edge。
func newWorkflowNodeSetCommand() *cobra.Command {
	var newIDFlag, engineFlag, modelFlag, effortFlag, reasoningEffortFlag, displayNameFlag string
	cmd := &cobra.Command{
		Use:   "set <name> <id>",
		Short: localizedHelpText("改某 agent 节点的字段（id / 引擎 / 模型 / 档位 / 显示名）", "Change fields on an agent node (id / engine / model / effort level / display name)"),
		Long: localizedHelpText(
			"只改一个 agent 节点的字段（id / 引擎 / 模型 / 档位 / 显示名），不重发整份定义。\n"+
				"用 --id 给这个节点改 id，指向它的连线、别处提示词里的 {{id}} 会自动跟着改。\n"+
				"提示词走 node set-prompt；增删节点走 node add / node rm；改边走 edge。",
			"Change only one agent node's fields (id / engine / model / effort level / display name) without resending the complete definition.\n"+
				"Use --id to change this node's id; edges pointing to it and {{id}} references in other prompts are updated automatically.\n"+
				"Use node set-prompt for prompts; node add / node rm to add or remove nodes; and edge to change edges.",
		),
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}

			flags := cmd.Flags()
			var opts nodeSetOptions
			if flags.Changed("id") {
				opts.NewID = &newIDFlag
			}
			if flags.Changed("engine") {
				opts.Engine = &engineFlag
			}
			if flags.Changed("model") {
				opts.Model = &modelFlag
			}
			if flags.Changed("effort") {
				opts.Effort = &effortFlag
			}
			if flags.Changed("reasoning-effort") {
				opts.ReasoningEffort = &reasoningEffortFlag
			}
			if flags.Changed("display-name") {
				opts.DisplayName = &displayNameFlag
			}
			if err := checkNodeSetFlagCombo(opts); err != nil {
				return err
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			wf, err := st.Load(name)
			if err != nil {
				return err
			}
			if err := applyNodeSet(&wf.Definition, id, opts); err != nil {
				return err
			}
			if err := workflow.Validate(&wf.Definition); err != nil {
				return err // 整份校验：改 engine 级联不兼容等在此退 1，原文件不变
			}
			if err := st.Save(wf); err != nil {
				return err
			}
			finalID := id
			if opts.NewID != nil {
				finalID = *opts.NewID // 改了 id 后按新 id 回显，别再打印已不存在的旧 id
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已更新 %s·%s\n", "✓ Updated %s·%s\n"), name, finalID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&newIDFlag, "id", "", localizedHelpText(
		"给这个节点改 id，指向它的连线、别处的 {{id}} 引用会自动跟着改；新 id 唯一、须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$、不得为 START/END",
		"Change this node's id; edges pointing to it and {{id}} references elsewhere are updated automatically; the new id must be unique, match ^[A-Za-z_][A-Za-z0-9_-]{0,63}$, and not be START/END",
	))
	f.StringVar(&engineFlag, "engine", "", localizedHelpText("设引擎（claude-code / antigravity / qoder / codex）", "Set the engine (claude-code / antigravity / qoder / codex)"))
	f.StringVar(&modelFlag, "model", "", localizedHelpText("设模型；传空串就是清除、回落引擎默认", "Set the model; pass an empty string to clear it and fall back to the engine default"))
	f.StringVar(&effortFlag, "effort", "", localizedHelpText("设 claude-code 档位；传空串清除", "Set the claude-code effort level; pass an empty string to clear it"))
	f.StringVar(&reasoningEffortFlag, "reasoning-effort", "", localizedHelpText("设 qoder / codex 推理档位；传空串清除", "Set the qoder / codex reasoning effort level; pass an empty string to clear it"))
	f.StringVar(&displayNameFlag, "display-name", "", localizedHelpText("改节点显示名（须非空）", "Change the node display name (must be nonempty)"))
	return cmd
}
