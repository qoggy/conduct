# help 命令与 prompts 主题测试用例

覆盖 `conduct help` 的根帮助、命令路径分派、内嵌 `prompts` 主题、语言选择与未知目标错误。对应 spec：见 [docs/specs/cli-tooling.md](../specs/cli-tooling.md)〈help〉、[docs/specs/cli-authoring.md](../specs/cli-authoring.md)〈模板变量〉。

## 覆盖规划

| 行为空间 | 行为项 | 用例 |
| --- | --- | --- |
| 正常路径 | 无参数时打印完整根帮助，同时列出顶层命令与帮助主题 | TC-001 |
| 正常路径 | 一到多级命令路径分派到对应命令帮助 | TC-002 |
| 正常路径 | `prompts` 主题随二进制离线输出 | TC-003 |
| 正常路径 | `--help`、命令路径帮助与内嵌主题按环境输出中文或英文 | TC-005 |
| 边界与错误 | 未知主题或命令路径 fail-loud，退出 `2` | TC-004 |
| 边界与错误 | `C`、`POSIX`、未设置或无法识别的语言值使用英文 | TC-006 |
| 数据流转 | `help workflow node set` 与目标命令 `--help` 输出一致 | TC-002 |
| 数据流转 | prompts 文档带出 DAG 模板变量、祖先引用与并行写盘冲突约束 | TC-003 |
| 特性叠加 | `LC_ALL` > `LC_MESSAGES` > `LANG`，高优先级值无法识别时不读取低优先级值 | TC-006 |
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
  1. `LC_ALL=zh_CN.UTF-8 "$CONDUCT" help prompts > "$WORK/prompts.txt"; echo "exit=$?"`
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
  1. `LC_ALL=zh_CN.UTF-8 "$CONDUCT" help does-not-exist 2>&1; echo "exit=$?"`
  2. `LC_ALL=zh_CN.UTF-8 "$CONDUCT" help workflow does-not-exist 2>&1; echo "exit=$?"`
- **预期**：
  - 两步退出码均为 `2`。
  - stderr 均含 `未知帮助主题`、`可用主题：prompts`，并提示 `conduct --help` 或 `conduct help <命令>`；不打印成功主题正文。
- **清理**：无。

### TC-005 中文与英文 help 完整切换

- **目的**：验证根帮助、多级命令帮助和内嵌主题均按环境变量切换语言，且命令名、参数名、占位符和代码示例不变，也未增加 `--lang`。
- **前置**：无（只读）。
- **步骤**：
  1. 执行以下原子 shell 块；任一 conduct 调用或文本断言失败，整个步骤立即以非零状态退出：

     ```bash
     (
       set -euo pipefail
       WORK=$(mktemp -d)
       trap 'rm -rf "$WORK"' EXIT

       LC_ALL=zh_CN.UTF-8 "$CONDUCT" --help > "$WORK/root.zh.txt"
       LC_ALL=en_US.UTF-8 "$CONDUCT" --help > "$WORK/root.en.txt"
       LC_ALL=zh_CN.UTF-8 "$CONDUCT" help workflow node set > "$WORK/set.zh.txt"
       LC_ALL=en_US.UTF-8 "$CONDUCT" help workflow node set > "$WORK/set.en.txt"
       LC_ALL=zh_CN.UTF-8 "$CONDUCT" help prompts > "$WORK/prompts.zh.txt"
       LC_ALL=en_US.UTF-8 "$CONDUCT" help prompts > "$WORK/prompts.en.txt"

       command grep -F 'conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。' "$WORK/root.zh.txt"
       command grep -F 'conduct — a CLI that interprets and runs workflow definitions (JSON).' "$WORK/root.en.txt"
       command grep -F '只改一个 agent 节点的字段' "$WORK/set.zh.txt"
       command grep -F "Change only one agent node's fields" "$WORK/set.en.txt"
       command grep -F '# 写好节点 promptTemplate' "$WORK/prompts.zh.txt"
       command grep -F '# Writing a Good Node promptTemplate' "$WORK/prompts.en.txt"
       test "$(command grep -Fo '`## 标题`' "$WORK/prompts.zh.txt" | wc -l | tr -d ' ')" = 2
       test "$(command grep -Fo '`## 标题`' "$WORK/prompts.en.txt" | wc -l | tr -d ' ')" = 2
       command grep -F 'conduct workflow node set <name> <id>' "$WORK/set.zh.txt"
       command grep -F 'conduct workflow node set <name> <id>' "$WORK/set.en.txt"
       command grep -F 'git worktree add ../wt-<分支标识> -b <分支名>' "$WORK/prompts.zh.txt"
       command grep -F 'git worktree add ../wt-<分支标识> -b <分支名>' "$WORK/prompts.en.txt"
       if grep -Fq -- '--lang' "$WORK/root.zh.txt"; then
         exit 1
       fi
       if grep -Fq -- '--lang' "$WORK/root.en.txt"; then
         exit 1
       fi
     )
     ```
- **预期**：
  - 原子 shell 块退出码为 `0`，因此六条 conduct 命令均退出 `0`，所有文本断言均成功。
  - 中文文件含中文关键文案，英文文件含忠实对应的英文关键文案。
  - 命令路径、`<name>` / `<id>` 占位符、Markdown 标题示例与 `git worktree add` 代码示例在两种语言里逐字相同。
  - 两种语言的根帮助中均没有 `--lang` 参数。
- **清理**：原子 shell 块退出时由 trap 自动删除临时目录。

### TC-006 环境变量优先级与英文回退

- **目的**：验证语言变量优先级为 `LC_ALL` > `LC_MESSAGES` > `LANG`，并验证 `C`、`POSIX`、未设置和无法识别值的英文回退。
- **前置**：无（只读）。
- **步骤**：
  1. 执行以下原子 shell 块；任一 conduct 调用或文本断言失败，整个步骤立即以非零状态退出：

     ```bash
     (
       set -euo pipefail
       WORK=$(mktemp -d)
       trap 'rm -rf "$WORK"' EXIT

       LC_ALL=zh_CN LC_MESSAGES=C LANG=C "$CONDUCT" --help > "$WORK/lc-all.txt"
       env -u LC_ALL LC_MESSAGES=zh_CN LANG=C "$CONDUCT" --help > "$WORK/lc-messages.txt"
       env -u LC_ALL -u LC_MESSAGES LANG=zh_CN "$CONDUCT" --help > "$WORK/lang.txt"
       LC_ALL=fr_FR LC_MESSAGES=zh_CN LANG=zh_CN "$CONDUCT" --help > "$WORK/unknown.txt"
       LC_ALL=C LC_MESSAGES=zh_CN "$CONDUCT" --help > "$WORK/c.txt"
       LC_ALL=POSIX LC_MESSAGES=zh_CN "$CONDUCT" --help > "$WORK/posix.txt"
       env -u LC_ALL -u LC_MESSAGES -u LANG "$CONDUCT" --help > "$WORK/unset.txt"

       for file in lc-all lc-messages lang; do
         command grep -F 'conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。' "$WORK/$file.txt"
       done
       for file in unknown c posix unset; do
         command grep -F 'conduct — a CLI that interprets and runs workflow definitions (JSON).' "$WORK/$file.txt"
       done
     )
     ```
- **预期**：
  - 原子 shell 块退出码为 `0`，因此七条 conduct 命令均退出 `0`，七次文本匹配均成功。
  - 三个中文匹配证明每一级变量都能在更高优先级变量未设置时选择中文。
  - 四个英文匹配中，`fr_FR` 位于最高优先级，虽低优先级均为中文仍直接使用英文，证明不可识别值不会继续向下取值；`C`、`POSIX` 与全部未设置也使用英文。
- **清理**：原子 shell 块退出时由 trap 自动删除临时目录。
