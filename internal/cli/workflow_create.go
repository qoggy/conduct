package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowCreateCommand() *cobra.Command {
	var fromDefinition bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "新建工作流（默认最小骨架，或 --definition 从 stdin 导入）",
		Long: "新建一份名为 <name> 的工作流（<name> 已存在则报错）。默认脚手架出最小骨架（单节点、claude-code、透传用户需求）；\n" +
			"带 --definition 时改为从 stdin 读入一份完整定义导入。\n\n" +
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
			if st.Exists(name) {
				return fmt.Errorf("工作流 %s 已存在（先 delete 或换名）", name)
			}

			var def *workflow.Definition
			if fromDefinition {
				data, readErr := readStdinDefinition()
				if readErr != nil {
					return readErr
				}
				def, err = workflow.ParseDefinition(data)
				if err != nil {
					return err
				}
				if err = reconcileImportName(def, name); err != nil {
					return err
				}
				if err = workflow.Validate(def); err != nil {
					return err
				}
			} else {
				def = workflow.Scaffold()
			}
			def.Name = name
			if err = st.Create(def); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已创建 %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromDefinition, "definition", false, "从 stdin 读入完整 workflow 定义导入（替代脚手架骨架）")
	return cmd
}
