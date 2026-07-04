package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// usageError 标记「用法错误」——由 Execute 映射为退出码 2（缺参 / 非法参数 / 非交互拒绝危险操作）。
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usageErrorf(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

// requireArgs 把 Cobra 的位置参数校验错误包成 usageError（→ 退出码 2）。
func requireArgs(validator cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validator(cmd, args); err != nil {
			return &usageError{err: err}
		}
		return nil
	}
}

// newWorkflowCommand 构造 `conduct workflow` 名词族及其动词子命令。
func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "管理工作流定义（创建 / 编辑 / 改名 / 删除 / 查询）",
		Long:  "conduct workflow —— 工作流定义的增删改查与解释运行，按名字寻址。",
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return usageErrorf("未知子命令 %q（可用：create / edit / rename / delete / list / show / run）", args[0])
		},
	}
	cmd.AddCommand(
		newWorkflowCreateCommand(),
		newWorkflowEditCommand(),
		newWorkflowRenameCommand(),
		newWorkflowDeleteCommand(),
		newWorkflowListCommand(),
		newWorkflowShowCommand(),
		newWorkflowRunCommand(),
	)
	return cmd
}

// openStore 打开生产 store（~/.conduct）。
func openStore() (*store.Store, error) {
	return store.Default()
}

// stdinIsTerminal 报告 stdin 是否为真实交互终端（用 x/term 的 ioctl 判定，
// 能正确区分真 TTY 与 /dev/null、管道、重定向文件——后者一律视为非终端）。
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readStdinDefinition 从 stdin 读入完整定义；stdin 是终端（无管道输入）时报用法错误退出 2，不挂起等待。
func readStdinDefinition() ([]byte, error) {
	if stdinIsTerminal() {
		return nil, usageErrorf("缺少定义：请通过 stdin 传入（如 cat def.json | conduct workflow ...）；可视化编辑用 conduct ui")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("读取 stdin 失败: %w", err)
	}
	return data, nil
}

// reconcileImportName 处理导入体里的 name：若出现且与目标名不一致则拒绝（绝不静默改名）。
// createdAt / updatedAt 等系统元数据由 store 写入，导入值忽略。
func reconcileImportName(def *workflow.Definition, target string) error {
	if def.Name != "" && def.Name != target {
		return fmt.Errorf("导入定义的 name=%q 与目标 %q 不一致（改名请用 conduct workflow rename）", def.Name, target)
	}
	return nil
}

// confirmDeletion 在交互终端下就删除做二次确认，回答 y / yes 才算确认。
func confirmDeletion(cmd *cobra.Command, names []string) (bool, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "将删除 %d 个工作流：%s。确认？[y/N] ", len(names), strings.Join(names, ", "))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("读取确认输入失败: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// workflowDefinitionHelp 返回「从 stdin 读入的工作流定义 JSON」的结构说明 + 最小示例，
// 供 create --definition / edit 的 --help 引用（让不熟悉 conduct 的调用方仅凭 --help 即可拼出合法定义）。
// 引擎名与各引擎 engineConfig 允许字段从 engine 能力表动态生成，避免静态文案与实现漂移。
func workflowDefinitionHelp() string {
	var b strings.Builder
	b.WriteString(`定义 JSON（stdin，单个对象）结构：
{
  "nodes": [                                 // 必填，≥1 个，按数组顺序执行
    {
      "id": "gen",                           // 必填，同一定义内唯一，须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$
      "displayName": "生成",                  // 必填，人类可读名
      "engine": "claude-code",               // 必填，见下方「引擎」
      "promptTemplate": "{{sys.userPrompt}}",// 必填，见下方「模板变量」
      "engineConfig": { "model": "" },       // 选填，字段随 engine 而定，见下方「引擎」；字段均选填
      "evaluator": {                         // 选填，节点内循环：本节点产出后由 evaluator 评测再重做本节点；与 redoTarget 互斥
        "engine": "claude-code",
        "promptTemplate": "评价并指出改进点：{{gen}}"
      },
      "redoTarget": "gen",                   // 选填，回跳到之前某节点 id、重跑该段；与 evaluator 互斥
      "loopCount": 1                         // 选填，仅配 evaluator/redoTarget 时生效，取值 1–20，默认 1
    }
  ]
}
引擎（engine 取值）与各自 engineConfig 允许字段：
`)
	for _, name := range engine.RegisteredNames() {
		capability, ok := engine.Capability(name)
		if !ok {
			fmt.Fprintf(&b, "  %s：暂不接受任何 engineConfig 字段\n", name)
			continue
		}
		var fields []string
		if capability.AllowsModel {
			fields = append(fields, "model")
		}
		if capability.EffortField != "" {
			fields = append(fields, fmt.Sprintf("%s(%s)", capability.EffortField, strings.Join(capability.EffortValues, "|")))
		}
		line := strings.Join(fields, " ")
		if capability.EffortField == "" {
			line += "（无独立 effort 字段，推理强度编码在 model 标签里）"
		}
		fmt.Fprintf(&b, "  %s：%s\n", name, line)
	}
	b.WriteString(`模板变量：{{sys.userPrompt}}=用户需求  {{sys.cwd}}=工作目录  {{<节点id>}}=引用该节点产物（未运行则空串）  \{{x}}=转义为字面量
name 若出现须与目标名一致（否则拒绝，改名用 conduct workflow rename）；createdAt/updatedAt 导入时忽略。
示例：echo '{"nodes":[{"id":"s1","displayName":"步骤1","engine":"claude-code","promptTemplate":"{{sys.userPrompt}}"}]}' | conduct workflow edit <name>
promptTemplate 怎么写好（模板变量、节点隔离、最佳实践）见 conduct help prompts。`)
	return b.String()
}

// printJSON 把值以缩进 JSON 打印到 stdout。
func printJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
