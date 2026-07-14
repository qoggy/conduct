package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

func newRunRmCommand() *cobra.Command {
	var yes, asJSON bool
	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "删除一条运行记录",
		Long: "删除一条历史运行记录。\n" +
			"仅终态（completed / failed / interrupted）可删；仍在跑（running 且 pid 存活）拒绝删除，先 conduct run stop <id>。\n" +
			"默认在交互终端下二次确认；非交互环境必须显式 --yes，避免脚本误删。",
		Args: requireArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := run.ValidateID(id); err != nil {
				return &usageError{err: err} // 非法 id → 退 2（发射前拦下）
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			// 非交互且未 --yes：用法错误退 2，绝不静默删除。
			if !yes && !stdinIsTerminal() {
				return usageErrorf("拒绝在非交互环境删除，请加 --yes")
			}
			// 先确认可删（存在 + 非活运行），再决定是否二次确认——避免「先确认、后被拒」的坏体验。
			if err := ensureRunDeletable(st, id); err != nil {
				return err
			}
			if !yes { // 到这里必是交互终端（非交互已在上面拦下）
				confirmed, confirmErr := confirmRunDeletion(cmd, id)
				if confirmErr != nil {
					return confirmErr
				}
				if !confirmed {
					// 取消是"未执行操作"的诊断，走 stderr——保 stdout 只承载数据
					// （成功回执 / --json 的 {"deleted":…}），--json 下取消不再污染 JSON。
					fmt.Fprintln(cmd.ErrOrStderr(), "已取消")
					return nil
				}
			}
			if err := st.RemoveRun(id); err != nil {
				return err
			}
			if asJSON {
				return printJSON(cmd, map[string][]string{"deleted": {id}})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ 已删除 %s\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "跳过确认直接删除")
	cmd.Flags().BoolVar(&asJSON, "json", false, `以 JSON 输出 {"deleted":["<id>"]}`)
	return cmd
}

// ensureRunDeletable 校验一条运行记录可删：存在（否则 ErrRunNotExist → 退 1），且非仍在写盘的活运行
// （running 且 pid 存活 → 拒绝，退 1）。用派生态判断，running 但 pid 已死会被折算为 interrupted（可删）。
func ensureRunDeletable(st *store.Store, id string) error {
	record, err := st.LoadRun(id)
	if err != nil {
		return err // 不存在 → ErrRunNotExist → 退 1
	}
	if record.EffectiveStatus() == run.StatusRunning {
		return fmt.Errorf("运行 %s 仍在进行中，无法删除；请先 conduct run stop %s 终止再删", id, id)
	}
	return nil
}

// confirmRunDeletion 在交互终端下就删除某条运行记录做二次确认，回答 y / yes（大小写不敏感）才算确认。
func confirmRunDeletion(cmd *cobra.Command, id string) (bool, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "确认删除运行 %s？[y/N] ", id)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("读取确认输入失败: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
