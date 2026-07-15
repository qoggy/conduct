package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version 是构建版本号，由 Makefile 通过 -ldflags 注入；默认 dev。
var version = "dev"

// newVersionCommand 构造 `conduct version`。
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: localizedHelpText("打印 conduct 版本", "Print the conduct version"),
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "conduct "+version)
			return err
		},
	}
}
