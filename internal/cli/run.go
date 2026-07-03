package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newRunCommand 构造 `conduct run`：解释运行一份 workflow 定义。
func newRunCommand() *cobra.Command {
	var expandOnly bool

	runCommand := &cobra.Command{
		Use:   "run <workflow.json>",
		Short: "解释运行一份 workflow 定义",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 解释器内核（类型建模 + expand 展开 + 主循环）尚未移植。
			// 显式报错而非静默假装成功，避免误导使用者。
			return errors.New("run 尚未实现：解释器内核待移植")
		},
	}
	runCommand.Flags().BoolVar(&expandOnly, "expand-only", false,
		"只打印展开后的执行步骤、不调用任何引擎")
	return runCommand
}
