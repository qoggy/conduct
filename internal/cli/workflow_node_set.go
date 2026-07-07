package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// nodeSetOptions 是 node set 的一次调用意图，用指针 / bool 显式携带「是否给出」：
// *string / *int 为 nil 表示该 flag 本次未出现（不动），非 nil 表示给出（含空串清除语义）。
// Evaluator 是作用域修饰（把引擎类字段切到 evaluator）；NoEvaluator / NoRedo 是拆除循环的动作。
type nodeSetOptions struct {
	Evaluator       bool
	Engine          *string
	Model           *string
	Effort          *string
	ReasoningEffort *string
	DisplayName     *string
	RedoTarget      *string
	LoopCount       *int
	NoEvaluator     bool
	NoRedo          bool
}

// nodeSetOutcomeKind 枚举 node set 施加后发生了什么，决定打印哪句成功文案。
type nodeSetOutcomeKind int

const (
	outcomeUpdated            nodeSetOutcomeKind = iota // 普通更新节点主体
	outcomeUpdatedEvaluator                             // 作用于既有评测官
	outcomeMountedEvaluator                             // 首次挂载评测循环
	outcomeMountedRedo                                  // 首次挂载回跳
	outcomeUnmountedEvaluator                           // 拆评测循环
	outcomeUnmountedRedo                                // 拆回跳
)

// nodeSetOutcome 描述 applyNodeSet 的结果，供 RunE 选取成功文案；redoTarget 仅在 outcomeMountedRedo 时有值。
type nodeSetOutcome struct {
	kind       nodeSetOutcomeKind
	redoTarget string
}

// checkNodeSetFlagCombo 校验 flag 组合的四类用法错误（返回 *usageError → 退出码 2），不依赖 store / Cobra，可直测。
func checkNodeSetFlagCombo(opts nodeSetOptions) error {
	hasField := opts.Engine != nil || opts.Model != nil || opts.Effort != nil ||
		opts.ReasoningEffort != nil || opts.DisplayName != nil ||
		opts.RedoTarget != nil || opts.LoopCount != nil
	hasTeardown := opts.NoEvaluator || opts.NoRedo

	// 4. --redo-target ""（给了但为空串）：拆回跳请用 --no-redo，不用空串。
	if opts.RedoTarget != nil && *opts.RedoTarget == "" {
		return usageErrorf("--redo-target 不接受空串；拆除回跳请用 --no-redo")
	}
	// 3. 两个 --no-* 同给：一次只拆一种循环。
	if opts.NoEvaluator && opts.NoRedo {
		return usageErrorf("--no-evaluator 与 --no-redo 不能同用（一次只拆一种循环）")
	}
	// 2. --no-* 未单独给：与任何字段选项或 --evaluator 同用。
	if hasTeardown && (hasField || opts.Evaluator) {
		return usageErrorf("--no-evaluator / --no-redo 须单独使用，不能与其它字段选项或 --evaluator 同用")
	}
	// 1. 无任何字段选项且无拆除选项（--evaluator 不单独计作操作）。
	if !hasField && !hasTeardown {
		return usageErrorf("至少给一个字段选项或拆除选项（--evaluator 仅是作用域修饰，不单独计作操作）")
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

// applyNodeSet 在内存中把 opts 施加到 def 里 id 指定的节点（就地改），返回描述发生了什么的 outcome。
// 只做命令级语义判定与字段级前置校验（退出码 1）；引擎兼容性、redoTarget 前向性等交整份 workflow.Validate 兜底。
func applyNodeSet(def *workflow.Definition, id string, opts nodeSetOptions) (nodeSetOutcome, error) {
	node, err := findNode(def, id)
	if err != nil {
		return nodeSetOutcome{}, err
	}

	// —— 拆除分支（组合校验已保证 --no-* 单独给）——
	if opts.NoEvaluator {
		if node.Evaluator == nil {
			return nodeSetOutcome{}, fmt.Errorf("节点 %s 无评测循环，无可拆除", id)
		}
		node.Evaluator = nil
		node.LoopCount = nil // 回落单次
		return nodeSetOutcome{kind: outcomeUnmountedEvaluator}, nil
	}
	if opts.NoRedo {
		if node.RedoTarget == "" {
			return nodeSetOutcome{}, fmt.Errorf("节点 %s 无回跳，无可拆除", id)
		}
		node.RedoTarget = ""
		node.LoopCount = nil // 回落单次
		return nodeSetOutcome{kind: outcomeUnmountedRedo}, nil
	}

	mountedEvaluator := false
	mountedRedo := false

	// —— 引擎类字段（engine / model / effort / reasoning-effort）：--evaluator 决定作用于评测官还是节点主体 ——
	// --evaluator 仅是引擎类字段的作用域修饰；本次若无任何引擎类字段（如只改 display-name / loop-count），
	// 它无所修饰，整段跳过——否则会把「节点无评测官」误当挂载失败拦下，连累恒节点级的 display-name 改不动。
	hasEngineField := opts.Engine != nil || opts.Model != nil || opts.Effort != nil || opts.ReasoningEffort != nil
	if opts.Evaluator && hasEngineField {
		if node.Evaluator == nil {
			if opts.Engine == nil {
				return nodeSetOutcome{}, fmt.Errorf("节点 %s 无评测循环；用 --evaluator --engine <e> 先挂载", id)
			}
			if node.RedoTarget != "" {
				return nodeSetOutcome{}, fmt.Errorf("节点 %s 已配 redoTarget，与 evaluator 互斥；先拆回跳（--no-redo）再挂评测官", id)
			}
			evaluator := &workflow.Evaluator{
				Engine:         *opts.Engine,
				PromptTemplate: workflow.DefaultEvaluatorPrompt,
			}
			applyEngineConfig(&evaluator.EngineConfig, opts)
			node.Evaluator = evaluator
			mountedEvaluator = true
		} else {
			if opts.Engine != nil {
				node.Evaluator.Engine = *opts.Engine
			}
			applyEngineConfig(&node.Evaluator.EngineConfig, opts)
		}
	} else if !opts.Evaluator {
		if opts.Engine != nil {
			node.Engine = *opts.Engine
		}
		applyEngineConfig(&node.EngineConfig, opts)
	}

	// —— displayName：恒节点级，与 --evaluator 无关；须非空（字段级错误，退出码 1）——
	if opts.DisplayName != nil {
		if *opts.DisplayName == "" {
			return nodeSetOutcome{}, fmt.Errorf("节点 %s 的 displayName 不能为空", id)
		}
		node.DisplayName = *opts.DisplayName
	}

	// —— redoTarget：恒节点级；与 evaluator 互斥；首次挂载记为 mountedRedo ——
	if opts.RedoTarget != nil {
		if node.Evaluator != nil {
			return nodeSetOutcome{}, fmt.Errorf("节点 %s 已有 evaluator，与 redoTarget 互斥；先拆评测官（--no-evaluator）再挂回跳", id)
		}
		firstMount := node.RedoTarget == ""
		node.RedoTarget = *opts.RedoTarget
		if firstMount {
			mountedRedo = true
		}
	}

	// —— 首次挂载循环且未显式给 loop-count：LoopCount 默认 1 ——
	// 不看 node.LoopCount 现有值：导入定义可携带「无循环却有 loopCount」的休眠值（validate.go 故意放行），
	// 挂载时须以默认 1 覆盖它，否则休眠值会被静默激活成循环次数（用户与文档预期是默认 1 轮）。
	if (mountedEvaluator || mountedRedo) && opts.LoopCount == nil {
		one := 1
		node.LoopCount = &one
	}

	// —— 显式 loop-count：命令级判定「施于无循环节点」退出码 1（design D6，validate.go 故意放行故须在此拦）——
	if opts.LoopCount != nil {
		if node.Evaluator == nil && node.RedoTarget == "" {
			return nodeSetOutcome{}, fmt.Errorf("节点 %s 无评测循环 / 回跳，loopCount 无从设置（先用 --evaluator --engine 挂评测官或 --redo-target 挂回跳）", id)
		}
		value := *opts.LoopCount
		node.LoopCount = &value
	}

	// evaluatorFieldTouched：--evaluator 且确有引擎类字段落到既有评测官，才算「更新评测官」；
	// 仅 --evaluator --display-name（displayName 恒节点级、评测官分毫未动）应归为普通更新，避免文案误导。
	evaluatorFieldTouched := opts.Evaluator &&
		(opts.Engine != nil || opts.Model != nil || opts.Effort != nil || opts.ReasoningEffort != nil)

	// —— 归结 outcome（优先级：新挂评测官 > 新挂回跳 > 作用于既有评测官 > 普通更新）——
	switch {
	case mountedEvaluator:
		return nodeSetOutcome{kind: outcomeMountedEvaluator}, nil
	case mountedRedo:
		return nodeSetOutcome{kind: outcomeMountedRedo, redoTarget: node.RedoTarget}, nil
	case evaluatorFieldTouched:
		return nodeSetOutcome{kind: outcomeUpdatedEvaluator}, nil
	default:
		return nodeSetOutcome{kind: outcomeUpdated}, nil
	}
}

// printNodeSetOutcome 按 outcome 打印对应成功文案（spec〈workflow node set〉输出小节）。
func printNodeSetOutcome(cmd *cobra.Command, name, id string, outcome nodeSetOutcome) {
	out := cmd.OutOrStdout()
	switch outcome.kind {
	case outcomeMountedEvaluator:
		fmt.Fprintf(out, "✓ 已为 %s·%s 挂载评测循环\n", name, id)
	case outcomeMountedRedo:
		fmt.Fprintf(out, "✓ 已为 %s·%s 挂载回跳→%s\n", name, id, outcome.redoTarget)
	case outcomeUnmountedEvaluator:
		fmt.Fprintf(out, "✓ 已拆除 %s·%s 的评测循环\n", name, id)
	case outcomeUnmountedRedo:
		fmt.Fprintf(out, "✓ 已拆除 %s·%s 的回跳\n", name, id)
	case outcomeUpdatedEvaluator:
		fmt.Fprintf(out, "✓ 已更新 %s·%s 的评测官\n", name, id)
	default:
		fmt.Fprintf(out, "✓ 已更新 %s·%s\n", name, id)
	}
}

// newWorkflowNodeSetCommand 构造 `conduct workflow node set <name> <id>`：改节点 / 评测官的结构化字段、挂 / 拆两种循环。
func newWorkflowNodeSetCommand() *cobra.Command {
	var (
		evaluator, noEvaluator, noRedo                                                          bool
		engineFlag, modelFlag, effortFlag, reasoningEffortFlag, displayNameFlag, redoTargetFlag string
		loopCountFlag                                                                           int
	)
	cmd := &cobra.Command{
		Use:   "set <name> <id>",
		Short: "改某节点 / 评测官的结构化字段，挂 / 拆评测循环与回跳",
		Long: "只改一个节点的结构化字段（引擎 / 模型 / 档位 / 显示名 / 循环次数），并挂 / 拆两种循环——\n" +
			"evaluator 自循环（--evaluator --engine 挂、--no-evaluator 拆）、redoTarget 回跳（--redo-target 挂、--no-redo 拆）。\n" +
			"--evaluator 是作用域修饰：把 --engine/--model/--effort/--reasoning-effort 切到评测官；--display-name/--loop-count/--redo-target 恒节点级。\n" +
			"清除调优标量用空串（--model \"\"）；至少给一个字段选项或拆除选项。内存改完复用整份定义的同一套校验再落盘。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, id := args[0], args[1]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}

			flags := cmd.Flags()
			opts := nodeSetOptions{
				Evaluator:   evaluator,
				NoEvaluator: noEvaluator,
				NoRedo:      noRedo,
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
			if flags.Changed("redo-target") {
				opts.RedoTarget = &redoTargetFlag
			}
			if flags.Changed("loop-count") {
				opts.LoopCount = &loopCountFlag
			}

			if err := checkNodeSetFlagCombo(opts); err != nil {
				return err
			}

			st, err := openStore()
			if err != nil {
				return err
			}
			def, err := st.Load(name)
			if err != nil {
				return err
			}
			outcome, err := applyNodeSet(def, id, opts)
			if err != nil {
				return err
			}
			if err := workflow.Validate(def); err != nil {
				return err // 整份校验：改 engine 级联不兼容 / redoTarget 前向性等在此退 1
			}
			if err := st.Save(def); err != nil {
				return err
			}
			printNodeSetOutcome(cmd, name, id, outcome)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&evaluator, "evaluator", false, "把 --engine/--model/--effort/--reasoning-effort 切到该节点的评测官（不加时作用于节点主体）")
	f.StringVar(&engineFlag, "engine", "", "设引擎（claude-code / antigravity / qoder / codex）；配 --evaluator 且节点无评测官时以此新建评测官")
	f.StringVar(&modelFlag, "model", "", "设模型；传空串 --model \"\" 清除（回落引擎默认）")
	f.StringVar(&effortFlag, "effort", "", "设 claude-code 档位；传空串清除")
	f.StringVar(&reasoningEffortFlag, "reasoning-effort", "", "设 qoder / codex 推理档位；传空串清除")
	f.StringVar(&displayNameFlag, "display-name", "", "改节点显示名（须非空，不受 --evaluator 影响）")
	f.IntVar(&loopCountFlag, "loop-count", 0, "设循环 / 回跳次数（1–20，仅当节点带评测官或回跳时有效）")
	f.StringVar(&redoTargetFlag, "redo-target", "", "挂 / 改 redoTarget 回跳（目标须存在且更前，与 evaluator 互斥）；拆除用 --no-redo")
	f.BoolVar(&noEvaluator, "no-evaluator", false, "拆除评测循环（须单独给）")
	f.BoolVar(&noRedo, "no-redo", false, "拆除回跳（须单独给）")
	return cmd
}
