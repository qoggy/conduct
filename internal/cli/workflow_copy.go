package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

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
			copied := def.CopyAs(dst)
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
