package cli

import (
	"fmt"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowCreateCommand() *cobra.Command {
	var fromDefinition bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: localizedHelpText("新建工作流（默认最小骨架，或 --definition 从 stdin 导入）", "Create a workflow (minimal skeleton by default, or import from stdin with --definition)"),
		Long: localizedHelpText(
			"新建一份名为 <name> 的工作流（<name> 已存在则报错）。默认脚手架出最小骨架（单节点、透传用户需求）；\n"+
				"带 --definition 时改为从 stdin 读入一份完整定义导入。\n\n",
			"Create a workflow named <name> (report an error if <name> already exists). By default, scaffold a minimal skeleton (one node that passes through the user request);\n"+
				"with --definition, import a complete definition from stdin instead.\n\n",
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
			if st.Exists(name) {
				return apperror.New(apperror.CodeWorkflowAlreadyExists, apperror.Params{"name": name})
			}

			var definition workflow.Definition
			if fromDefinition {
				data, readErr := readStdinDefinition()
				if readErr != nil {
					return readErr
				}
				body, parseErr := workflow.ParseDefinition(data) // 导入体：主体或整条记录皆容忍（解包 definition、忽略元数据）
				if parseErr != nil {
					return parseErr
				}
				definition = *body
				if err = workflow.Validate(&definition); err != nil {
					return err
				}
			} else {
				definition = workflow.Scaffold()
			}
			wf := &workflow.Workflow{Name: name, Definition: definition}
			if err = st.Create(wf); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已创建 %s\n", "✓ Created %s\n"), name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromDefinition, "definition", false, localizedHelpText(
		"从 stdin 读入完整 workflow 定义导入（替代脚手架骨架）",
		"Import a complete workflow definition from stdin (instead of the scaffolded skeleton)",
	))
	return cmd
}
