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
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// usageError 标记「用法错误」——由 Execute 映射为退出码 2（缺参 / 非法参数 / 非交互拒绝危险操作）。
type usageError struct{ err error }

func (e *usageError) Error() string { return formatCLIError(e.err) }
func (e *usageError) Unwrap() error { return e.err }

func usageErrorf(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

func localizedUsageErrorf(chinese, english string, arguments ...any) error {
	return &usageError{err: localizedErrorf(chinese, english, arguments...)}
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return localizedUsageErrorf("命令 %q 不接受位置参数，收到 %d 个", "command %q accepts no positional arguments; received %d", cmd.CommandPath(), len(args))
	}
	return nil
}

func exactArgs(want int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != want {
			english := "command %q requires %d positional arguments; received %d"
			if want == 1 {
				english = "command %q requires %d positional argument; received %d"
			}
			return localizedUsageErrorf("命令 %q 需要 %d 个位置参数，收到 %d 个", english, cmd.CommandPath(), want, len(args))
		}
		return nil
	}
}

func minimumArgs(want int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < want {
			english := "command %q requires at least %d positional arguments; received %d"
			if want == 1 {
				english = "command %q requires at least %d positional argument; received %d"
			}
			return localizedUsageErrorf("命令 %q 至少需要 %d 个位置参数，收到 %d 个", english, cmd.CommandPath(), want, len(args))
		}
		return nil
	}
}

func maximumArgs(want int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > want {
			english := "command %q accepts at most %d positional arguments; received %d"
			if want == 1 {
				english = "command %q accepts at most %d positional argument; received %d"
			}
			return localizedUsageErrorf("命令 %q 最多接受 %d 个位置参数，收到 %d 个", english, cmd.CommandPath(), want, len(args))
		}
		return nil
	}
}

func rangeArgs(minimum, maximum int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minimum || len(args) > maximum {
			return localizedUsageErrorf("命令 %q 接受 %d 到 %d 个位置参数，收到 %d 个", "command %q accepts between %d and %d positional arguments; received %d", cmd.CommandPath(), minimum, maximum, len(args))
		}
		return nil
	}
}

// newWorkflowCommand 构造 `conduct workflow` 名词族及其动词子命令。
func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: localizedHelpText("管理工作流定义（创建 / 编辑 / 改名 / 删除 / 查询）", "Manage workflow definitions (create / edit / rename / delete / query)"),
		Long: localizedHelpText(
			"conduct workflow —— 工作流定义的增删改查与解释运行，按名字寻址。",
			"conduct workflow — create, delete, edit, and query workflow definitions, or interpret and run them; workflows are addressed by name.",
		),
		// 无参裸命令打印帮助；拼错的子命令 fail-loud 报用法错误（退出码 2），不静默当成功。
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return localizedUsageErrorf("未知子命令 %q（可用：create / copy / edit / node / edge / rename / delete / list / show / run）", "unknown subcommand %q (available: create / copy / edit / node / edge / rename / delete / list / show / run)", args[0])
		},
	}
	cmd.AddCommand(
		newWorkflowCreateCommand(),
		newWorkflowEditCommand(),
		newWorkflowRenameCommand(),
		newWorkflowCopyCommand(),
		newWorkflowNodeCommand(),
		newWorkflowEdgeCommand(),
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

// readStdin 从 stdin 读入原始字节；stdin 是终端（无管道输入）时以 missingMsg 报用法错误退出 2，不挂起等待。
func readStdin(missingMsg string) ([]byte, error) {
	if stdinIsTerminal() {
		return nil, usageErrorf("%s", missingMsg)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdin: %w", err)
	}
	return data, nil
}

// readStdinDefinition 从 stdin 读入完整定义；stdin 是终端（无管道输入）时报用法错误退出 2，不挂起等待。
func readStdinDefinition() ([]byte, error) {
	return readStdin(localizedHelpText(
		"缺少定义：请通过 stdin 传入（如 cat def.json | conduct workflow ...）；可视化编辑用 conduct ui",
		"missing definition: pass it through stdin (for example, cat def.json | conduct workflow ...); use conduct ui for visual editing",
	))
}

// confirmDeletion 在交互终端下就删除做二次确认，回答 y / yes 才算确认。
func confirmDeletion(cmd *cobra.Command, names []string) (bool, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), localizedHelpText("将删除 %d 个工作流：%s。确认？[y/N] ", "Delete %d workflows: %s. Confirm? [y/N] "), len(names), strings.Join(names, ", "))
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read confirmation input: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// 下列三段帮助文案描述「提示词模板变量 / 图约束 / promptTemplate 写法」，被 create --definition、edit、
// node add 等多处 --help 共用；集中一处，避免各命令文案漂移。
func templateVariablesHelp() string {
	return localizedHelpText(
		`模板变量：{{sys.userPrompt}}=用户需求  {{sys.cwd}}=工作目录  {{sys.runId}}=本次运行的 run id  {{<节点id>}}=引用该上游祖先节点产物（未运行则空串）  \{{x}}=转义为字面量`,
		`Template variables: {{sys.userPrompt}}=user request  {{sys.cwd}}=working directory  {{sys.runId}}=current run id  {{<node-id>}}=references that upstream ancestor node's artifact (empty string if not run)  \{{x}}=escapes to a literal`,
	)
}

func graphConstraintsHelp() string {
	return localizedHelpText(
		`图约束：恰好一个 START、一个 END；无环；每个 agent 节点有入有出；{{<id>}} 只能引用上游祖先 agent 节点（禁 {{START}}/{{END}}）。`,
		`Graph constraints: exactly one START and one END; no cycles; every agent node has incoming and outgoing edges; {{<id>}} may reference only upstream ancestor agent nodes ({{START}}/{{END}} forbidden).`,
	)
}

func promptTemplateHint() string {
	return localizedHelpText(
		`promptTemplate 怎么写好（模板变量、节点隔离、并行分支避免写盘冲突）见 conduct help prompts。`,
		`For how to write a good promptTemplate (template variables, node isolation, and avoiding disk-write conflicts between parallel branches), see conduct help prompts.`,
	)
}

// effortEnum 返回某引擎 effort / reasoningEffort 字段合法取值的 "a|b|c" 串（无该字段或引擎未登记能力表则空串），
// 供 node add 的 --effort / --reasoning-effort 说明从能力表动态取值，不在文案里硬编码枚举。
func effortEnum(engineName string) string {
	capability, ok := engine.Capability(engineName)
	if !ok || capability.EffortField == "" {
		return ""
	}
	return strings.Join(capability.EffortValues, "|")
}

// workflowDefinitionHelp 返回「从 stdin 读入的工作流定义主体 JSON」的结构说明 + 最小示例，
// 供 create --definition / edit 的 --help 引用（让不熟悉 conduct 的调用方仅凭 --help 即可拼出合法定义）。
// 引擎名与各引擎 engineConfig 允许字段从 engine 能力表动态生成，避免静态文案与实现漂移。
func workflowDefinitionHelp() string {
	var b strings.Builder
	b.WriteString(localizedHelpText(
		"定义主体 JSON（stdin，单个对象 {nodes, edges}）：\n",
		"Workflow definition body JSON (stdin, one {nodes, edges} object):\n",
	))
	b.WriteString(localizedHelpText(`{
  "nodes": [                                 // 必填，含两个保留标记节点 START、END + ≥1 个 agent 节点
    { "id": "START" },                       // 保留标记：无 engine/prompt、不执行、唯一源（无入边）
    {
      "id": "gen",                           // agent 节点 id：必填、唯一、须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$、不得为 START/END
      "displayName": "生成",                  // 必填，人类可读名
      "engine": "claude-code",               // 必填，见下方「引擎」
      "promptTemplate": "{{sys.userPrompt}}",// 必填，见下方「模板变量」
      "engineConfig": { "model": "" }        // 选填，字段随 engine 而定，见下方「引擎」；字段均选填
    },
    { "id": "END" }                          // 保留标记：无 engine/prompt、不执行、唯一汇（无出边）
  ],
  "edges": [                                 // 必填，表达执行依赖（from 跑完才轮到 to）；from 可为 START、to 可为 END
    { "from": "START", "to": "gen" },
    { "from": "gen",   "to": "END" }
  ]
}
`, `{
  "nodes": [                                 // required; includes reserved marker nodes START and END plus at least one agent node
    { "id": "START" },                       // reserved marker: no engine or prompt; not executed; sole source with no incoming edge
    {
      "id": "gen",                           // agent node id: required, unique, must match ^[A-Za-z_][A-Za-z0-9_-]{0,63}$, and cannot be START or END
      "displayName": "Generate",              // required, human-readable name
      "engine": "claude-code",               // required; see "Engines" below
      "promptTemplate": "{{sys.userPrompt}}",// required; see "Template variables" below
      "engineConfig": { "model": "" }        // optional; fields depend on engine as listed below, and every field is optional
    },
    { "id": "END" }                          // reserved marker: no engine or prompt; not executed; sole sink with no outgoing edge
  ],
  "edges": [                                 // required execution dependencies: from completes before to; from may be START and to may be END
    { "from": "START", "to": "gen" },
    { "from": "gen",   "to": "END" }
  ]
}
`))
	b.WriteString(localizedHelpText(
		"引擎（engine 取值）与各自 engineConfig 允许字段：\n",
		"Engines (valid engine values) and the engineConfig fields each allows:\n",
	))
	for _, name := range engine.RegisteredNames() {
		capability, ok := engine.Capability(name)
		if !ok {
			fmt.Fprintf(&b, localizedHelpText(
				"  %s：暂不接受任何 engineConfig 字段\n",
				"  %s: currently accepts no engineConfig fields\n",
			), name)
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
			line += localizedHelpText(
				"（无独立 effort 字段，推理强度编码在 model 标签里）",
				" (no separate effort field; reasoning intensity is encoded in the model tag)",
			)
		}
		fmt.Fprintf(&b, localizedHelpText("  %s：%s\n", "  %s: %s\n"), name, line)
	}
	b.WriteString(templateVariablesHelp() + "\n" + graphConstraintsHelp() + "\n" + promptTemplateHint())
	return b.String()
}

// printJSON 把值以缩进 JSON 打印到 stdout。
func printJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
