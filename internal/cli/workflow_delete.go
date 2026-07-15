package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowDeleteCommand() *cobra.Command {
	var yes, asJSON bool
	cmd := &cobra.Command{
		Use:   "delete <name>...",
		Short: localizedHelpText("删除一个 / 多个工作流", "Delete one or more workflows"),
		Long: localizedHelpText(
			"删除一个或多个工作流。默认在交互终端下二次确认；\n"+
				"非交互环境必须显式 --yes，避免脚本误删。",
			"Delete one or more workflows. By default, ask for confirmation in an interactive terminal;\n"+
				"non-interactive environments must pass --yes explicitly to prevent accidental deletion by scripts.",
		),
		Args: minimumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range args {
				if err := workflow.ValidateName(name); err != nil {
					return &usageError{err: err}
				}
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			if !yes {
				if !stdinIsTerminal() {
					return localizedUsageErrorf("拒绝在非交互环境删除，请加 --yes", "refusing to delete in a non-interactive environment; add --yes")
				}
				confirmed, confirmErr := confirmDeletion(cmd, args)
				if confirmErr != nil {
					return confirmErr
				}
				if !confirmed {
					// 取消是"未执行操作"的诊断，走 stderr——保 stdout 只承载数据
					// （成功回执 / --json 的 {"deleted":…}），--json 下取消不再污染 JSON。
					fmt.Fprintln(cmd.ErrOrStderr(), localizedHelpText("已取消", "cancelled"))
					return nil
				}
			}

			var deleted, missing []string
			for _, name := range args {
				if err := st.Delete(name); err != nil {
					if errors.Is(err, store.ErrNotExist) {
						missing = append(missing, name)
						continue
					}
					return err
				}
				deleted = append(deleted, name)
				if !asJSON {
					fmt.Fprintf(cmd.OutOrStdout(), localizedHelpText("✓ 已删除 %s\n", "✓ Deleted %s\n"), name)
				}
			}
			if asJSON {
				if err := printJSON(cmd, map[string][]string{"deleted": deleted}); err != nil {
					return err
				}
			}
			if len(missing) > 0 {
				// 存在的已删除；有缺失项则以非 0 退出，逐条汇总到 stderr。
				return apperror.New(apperror.CodeWorkflowNotFound, apperror.Params{"name": strings.Join(missing, ", ")})
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, localizedHelpText("跳过确认直接删除", "Skip confirmation and delete directly"))
	cmd.Flags().BoolVar(&asJSON, "json", false, localizedHelpText(`以 JSON 输出 {"deleted":[...]}`, `Output {"deleted":[...]} as JSON`))
	return cmd
}
