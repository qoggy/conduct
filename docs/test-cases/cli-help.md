# help 命令与 prompts 主题测试用例

覆盖 `conduct help` 的根帮助、命令路径分派、内嵌 `prompts` 主题与未知目标错误。对应 spec：见 [docs/specs/cli-tooling.md](../specs/cli-tooling.md)〈help〉、[docs/specs/cli-authoring.md](../specs/cli-authoring.md)〈模板变量〉。

## 覆盖规划

| 行为空间 | 行为项 | 用例 |
| --- | --- | --- |
| 正常路径 | 无参数时打印完整根帮助，同时列出顶层命令与帮助主题 | TC-001 |
| 正常路径 | 一到多级命令路径分派到对应命令帮助 | TC-002 |
| 正常路径 | `prompts` 主题随二进制离线输出 | TC-003 |
| 边界与错误 | 未知主题或命令路径 fail-loud，退出 `2` | TC-004 |
| 数据流转 | `help workflow node set` 与目标命令 `--help` 输出一致 | TC-002 |
| 数据流转 | prompts 文档带出 DAG 模板变量、祖先引用与并行写盘冲突约束 | TC-003 |
| 特性叠加 | DAG + resume + 模板变量：每节点至多成功一次且 `{{sys.runId}}` 可用 | TC-003 |

帮助分派的 Cobra 内部查找过程交给 CLI 单测；手工层只断言对外的退出码与文本。主题通过 `go:embed` 随二进制发布，本文不访问仓库中的 `internal/help/prompts.md` 来代替验证。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径：

```bash
make build
CONDUCT="$PWD/bin/conduct"
```

本文全部是只读命令，不创建 store 或其它状态，无需临时 HOME。需要保存输出时，每个用例自行创建并删除独立临时目录。

## 用例

### TC-001 无参数打印完整根帮助

- **目的**：验证 `conduct help` 不带参数时打印完整根帮助，命令与帮助主题都可发现。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" help; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout 含 `Usage:`、`Available Commands:`，并列出 `workflow`、`run`、`ui`、`update`、`version`。
  - stdout 含 `Additional help topics:` 与 `conduct help prompts`。
- **清理**：无。

### TC-002 多级命令路径输出与 --help 一致

- **目的**：验证 `conduct help <命令路径>` 支持多级路径，并把请求分派给目标命令的帮助输出。
- **前置**：`WORK=$(mktemp -d)`。
- **步骤**：
  1. `"$CONDUCT" help workflow node set > "$WORK/by-help.txt"; echo "help_exit=$?"`
  2. `"$CONDUCT" workflow node set --help > "$WORK/by-flag.txt"; echo "flag_exit=$?"`
  3. `command grep -v 'help for set' "$WORK/by-flag.txt" > "$WORK/by-flag.norm"; diff -u "$WORK/by-flag.norm" "$WORK/by-help.txt"; echo "diff_exit=$?"`
- **预期**：
  - 步骤 1、2 分别打印 `help_exit=0`、`flag_exit=0`。
  - 两份输出都含 `conduct workflow node set <name> <id> [flags]`，并列出 `--id`、`--engine`、`--model`、`--effort`、`--reasoning-effort`、`--display-name`。
  - 步骤 3 打印 `diff_exit=0`，证明两条公开入口得到同一命令说明与业务选项。归一化只排除 Cobra 在 `--help` 入口额外注入的 `-h, --help  help for set` 自身帮助行。
- **清理**：`rm -rf "$WORK"`。

### TC-003 prompts 主题覆盖 DAG 模板与并行冲突约束

- **目的**：验证内嵌 `prompts` 主题已从旧循环模型更新为 DAG 语义，并完整说明 0.1.0 的模板变量、祖先产物串联和并行分支写盘冲突。
- **前置**：`WORK=$(mktemp -d)`。
- **步骤**：
  1. `"$CONDUCT" help prompts > "$WORK/prompts.txt"; echo "exit=$?"`
  2. `command grep -F '{{sys.userPrompt}}' "$WORK/prompts.txt"; command grep -F '{{sys.cwd}}' "$WORK/prompts.txt"; command grep -F '{{sys.runId}}' "$WORK/prompts.txt"`
  3. `command grep -F '上游祖先 agent 节点' "$WORK/prompts.txt"; command grep -F '并行分支的产物不自动汇聚' "$WORK/prompts.txt"`
  4. `command grep -F '并行分支避免写盘冲突' "$WORK/prompts.txt"; command grep -F 'git worktree add' "$WORK/prompts.txt"`
  5. `command grep -c 'evaluator' "$WORK/prompts.txt"; command grep -c 'redoTarget' "$WORK/prompts.txt"; command grep -c 'loopCount' "$WORK/prompts.txt"`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 首行含 `# 写好节点 promptTemplate`。
  - 步骤 2 三个变量均被找到；其中 `{{sys.runId}}` 明确映射为本次运行的 run id。
  - 步骤 3 两条均被找到：节点产物引用限上游祖先，多个并行分支须由下游逐个引用才会汇聚。
  - 步骤 4 两条均被找到：共享 `cwd` 的并行节点需避免写盘冲突，独立工作区可使用 `git worktree`。
  - 步骤 5 依次打印 `0`、`0`、`0`；主题中没有已删除循环模型的残留术语。
- **清理**：`rm -rf "$WORK"`。

### TC-004 未知帮助目标返回用法错误

- **目的**：验证未知主题或命令路径显式报错、列出可用主题与命令帮助入口，不静默回退到根帮助。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" help does-not-exist 2>&1; echo "exit=$?"`
  2. `"$CONDUCT" help workflow does-not-exist 2>&1; echo "exit=$?"`
- **预期**：
  - 两步退出码均为 `2`。
  - stderr 均含 `未知帮助主题`、`可用主题：prompts`，并提示 `conduct --help` 或 `conduct help <命令>`；不打印成功主题正文。
- **清理**：无。
