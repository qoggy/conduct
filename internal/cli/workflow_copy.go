package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowCopyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copy <src> <dst>",
		Short: localizedHelpText("从既有工作流复制出一份新名字的工作流（造变体）", "Copy an existing workflow under a new name (create a variant)"),
		Long:  localizedHelpText("从 <src> 复制出一份名为 <dst> 的新工作流。", "Copy <src> into a new workflow named <dst>."),
		Args:  exactArgs(2),
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
				return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": src})
			}
			if st.Exists(dst) {
				return apperror.New(apperror.CodeWorkflowAlreadyExists, apperror.Params{"name": dst})
			}
			wf, err := st.Load(src)
			if err != nil {
				return err
			}
			copied := wf.CopyAs(dst)
			// 防御式校验：<src> 已在库应已合法，仍校验一遍；不过即拒、不写盘。
			if err = workflow.Validate(&copied.Definition); err != nil {
				return err
			}
			if err = st.Create(copied); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已复制 %s → %s\n", "✓ Copied %s → %s\n"), src, dst)
			return nil
		},
	}
	return cmd
}
