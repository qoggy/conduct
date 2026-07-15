package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: localizedHelpText("从 stdin 读 JSON 整体替换既有工作流", "Read JSON from stdin to replace an existing workflow in full"),
		Long: localizedHelpText(
			"从 stdin 读入一份完整定义，原子替换名为 <name> 的既有工作流。\n\n",
			"Read a complete definition from stdin and atomically replace the existing workflow named <name>.\n\n",
		) +
			workflowDefinitionHelp(),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := workflow.ValidateName(name); err != nil {
				return &usageError{err: err}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			if !st.Exists(name) {
				return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": name})
			}
			data, err := readStdinDefinition()
			if err != nil {
				return err
			}
			body, err := workflow.ParseDefinition(data) // 导入体：主体或整条记录皆容忍（解包 definition、忽略元数据）
			if err != nil {
				return err
			}
			if err = workflow.Validate(body); err != nil {
				return err
			}
			wf := &workflow.Workflow{Name: name, Definition: *body}
			if err = st.ReplaceDefinition(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已更新 %s\n", "✓ Updated %s\n"), name)
			return nil
		},
	}
	return cmd
}
