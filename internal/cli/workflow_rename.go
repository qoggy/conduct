package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowRenameCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "给既有工作流改名（改标识，不动定义）",
		Long: "给既有工作流改名。改的是标识。\n" +
			"已有运行记录不随之改名。",
		Args: requireArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName, newName := args[0], args[1]
			if err := workflow.ValidateName(oldName); err != nil {
				return &usageError{err: err}
			}
			if err := workflow.ValidateName(newName); err != nil {
				return &usageError{err: err}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			if !st.Exists(oldName) {
				return fmt.Errorf("工作流 %s 不存在", oldName)
			}
			if st.Exists(newName) {
				return fmt.Errorf("工作流 %s 已存在（先 delete 或换名）", newName)
			}
			if err = st.Rename(oldName, newName); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已重命名 %s → %s\n", oldName, newName)
			return nil
		},
	}
	return cmd
}
