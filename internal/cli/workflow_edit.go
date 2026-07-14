package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "从 stdin 读 JSON 整体替换既有工作流",
		Long: "从 stdin 读入一份完整定义，原子替换名为 <name> 的既有工作流。\n\n" +
			workflowDefinitionHelp(),
		Args: requireArgs(cobra.ExactArgs(1)),
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
				return fmt.Errorf("工作流 %s 不存在", name)
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
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已更新 %s\n", name)
			return nil
		},
	}
	return cmd
}
