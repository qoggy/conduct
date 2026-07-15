package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowRenameCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: localizedHelpText("给既有工作流改名（改标识，不动定义）", "Rename an existing workflow (change its identifier, not its definition)"),
		Long: localizedHelpText(
			"给既有工作流改名。改的是标识。\n"+
				"已有运行记录不随之改名。",
			"Rename an existing workflow. This changes its identifier.\n"+
				"Existing run records are not renamed with it.",
		),
		Args: exactArgs(2),
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
				return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": oldName})
			}
			if st.Exists(newName) {
				return apperror.New(apperror.CodeWorkflowAlreadyExists, apperror.Params{"name": newName})
			}
			if err = st.Rename(oldName, newName); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已重命名 %s → %s\n", "✓ Renamed %s → %s\n"), oldName, newName)
			return nil
		},
	}
	return cmd
}
