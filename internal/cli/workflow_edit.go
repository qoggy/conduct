package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "从 stdin 整体替换既有工作流（保存即校验）",
		Long: "从 stdin 读入完整新定义整体替换既有工作流。保存即校验，不过则拒绝写入、保留原定义不变。\n" +
			"可视化编辑用 conduct ui；改名用 conduct workflow rename。",
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
			def, err := workflow.ParseDefinition(data)
			if err != nil {
				return err
			}
			if err = reconcileImportName(def, name); err != nil {
				return err
			}
			def.Name = name
			if err = workflow.Validate(def); err != nil {
				return err
			}
			if err = st.Save(def); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已更新 %s\n", name)
			return nil
		},
	}
	return cmd
}
