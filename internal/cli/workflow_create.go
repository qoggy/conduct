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
		Short: "新建一份工作流入库",
		Long: "新建一份工作流并入库。默认脚手架出最小骨架；带 --definition 时从 stdin 读入完整定义导入。\n" +
			"入库前一律校验，不过则拒绝落盘。",
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
