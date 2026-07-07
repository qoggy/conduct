package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

// buildCopiedDefinition 从 src 造一份新定义：只带 dstName 与深拷来的 nodes，
// 不携带时间戳（CreatedAt / UpdatedAt 留空，交给 store.Create 重戳当前时刻）。
// nodes 必须深拷——Node 内含指针字段（EngineConfig / Evaluator / LoopCount），
// 逐个 new 一份，避免 dst 与 src 共享底层指针后互相串改。
func buildCopiedDefinition(src *workflow.Definition, dstName string) *workflow.Definition {
	nodes := make([]workflow.Node, len(src.Nodes))
	for i := range src.Nodes {
		nodes[i] = copyNode(src.Nodes[i])
	}
	return &workflow.Definition{
		Name:  dstName,
		Nodes: nodes,
	}
}

// copyNode 深拷单个节点：值字段直接复制，三个指针字段各 new 一份新的底层值。
func copyNode(node workflow.Node) workflow.Node {
	copied := node // 先浅拷值字段（ID / DisplayName / Engine / PromptTemplate / RedoTarget）
	copied.EngineConfig = copyEngineConfig(node.EngineConfig)
	if node.Evaluator != nil {
		evaluator := *node.Evaluator
		evaluator.EngineConfig = copyEngineConfig(node.Evaluator.EngineConfig)
		copied.Evaluator = &evaluator
	}
	if node.LoopCount != nil {
		loopCount := *node.LoopCount
		copied.LoopCount = &loopCount
	}
	return copied
}

// copyEngineConfig 深拷 EngineConfig 指针（nil 原样返回 nil）。
func copyEngineConfig(config *workflow.EngineConfig) *workflow.EngineConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	return &cloned
}

func newWorkflowCopyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copy <src> <dst>",
		Short: "从既有工作流复制出一份新名字的工作流（造变体）",
		Long: "从 <src> 复制出一份名为 <dst> 的新工作流——一步造变体，替掉 show --json / 改文件 / create --definition 的多步拼装。\n" +
			"复制的是定义主体（nodes）；<dst> 是全新的托管对象，createdAt / updatedAt 重戳当前时刻、不继承 <src>。\n" +
			"语义同 create：<dst> 已存在则拒绝、不覆盖。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, dst := args[0], args[1]
			if err := workflow.ValidateName(dst); err != nil {
				return &usageError{err: err}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			if !st.Exists(src) {
				return fmt.Errorf("工作流 %s 不存在", src)
			}
			if st.Exists(dst) {
				return fmt.Errorf("工作流 %s 已存在（先 delete 或换名）", dst)
			}
			def, err := st.Load(src)
			if err != nil {
				return err
			}
			copied := buildCopiedDefinition(def, dst)
			// 防御式校验：<src> 已在库应已合法，仍校验一遍；不过即拒、不写盘。
			if err = workflow.Validate(copied); err != nil {
				return err
			}
			if err = st.Create(copied); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已复制 %s → %s\n", src, dst)
			return nil
		},
	}
	return cmd
}
