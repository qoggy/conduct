# Kiro CLI 无头模式

`kiro-cli` 是 Kiro 的命令行客户端。本文聚焦 `kiro-cli chat --no-interactive`：如何在脚本、CI/CD 和其他无人值守环境中运行一次任务，并说明权限、模型、推理强度、输出、工作目录、大输入异常和图片识别。

- 本文实测版本：`kiro-cli 2.13.0`
- 查看版本：`kiro-cli --version`
- 查看帮助：`kiro-cli chat --help`
- 官方入口：[Kiro CLI 文档](https://kiro.dev/docs/cli/)
- 本机没有 `kiro` 命令；官方二进制名和本文命令均为 `kiro-cli`。

官方文档与已安装版本可能不同。尤其是模型清单、模型上下文窗口、effort 支持范围和输出细节，应以目标运行环境中的 `--help`、`--list-models` 和小规模预演为准。

## 最小用法

把 prompt 作为位置参数传入：

```bash
kiro-cli chat --no-interactive "hello"
```

无头模式不启动交互式终端界面，处理完第一轮回答后退出。它适合 CI/CD、定时任务和由其他程序拉起的一次性任务。官方的完整说明见[无头模式](https://kiro.dev/docs/cli/headless/)和[命令参考](https://kiro.dev/docs/cli/reference/cli-commands/)。

也可以只从 stdin 传入 prompt：

```bash
printf '%s\n' 'hello' | kiro-cli chat --no-interactive
```

实测 2.13.0 中：

- 只给位置参数：位置参数是 prompt。
- 只给 stdin：stdin 是 prompt。
- 同时给位置参数和 stdin：模型没有收到 stdin 内容，只收到位置参数。官方无头模式页面展示过“管道内容 + 位置参数”的形式，但 2.13.0 实测并未合并两者。
- 位置参数和 stdin 都为空：错误写入 stderr，退出码为 1。

因此，要把说明和长文本一起传入，兼容 2.13.0 的写法是把二者都写入 stdin：

```bash
{
  printf '%s\n\n' '解释下面的构建失败，并给出最小修复建议：'
  cat build-error.log
} | kiro-cli chat --no-interactive
```

空输入的输出为：

```text
stderr: error: Input must be supplied when running in non-interactive mode
exit:   1
```

## 认证

CI/CD 应把 Kiro API key 放在 `KIRO_API_KEY` 环境变量中：

```bash
KIRO_API_KEY="$KIRO_API_KEY" \
  kiro-cli chat --no-interactive "检查当前项目的测试失败原因"
```

API key 需要在 Kiro 账户中创建，不能写入仓库、命令脚本或日志。官方[认证文档](https://kiro.dev/docs/cli/authentication/)规定的凭据优先级是：

1. `kiro-cli login` 建立的活跃浏览器登录会话。
2. `KIRO_API_KEY`。
3. 没有凭据时提示登录。

这意味着开发机即使没有设置 `KIRO_API_KEY`，无头命令也可能因为已有登录会话而成功；不要据此判断 CI 已正确注入密钥。可用 `kiro-cli whoami` 检查当前身份。

## 权限

无头模式中没有用户可以确认工具调用。需要在启动时预先授权，否则待确认的工具会被拒绝。

```bash
# 只授权读文件和搜索，适合代码审查
kiro-cli chat --no-interactive \
  --trust-tools=read,grep,glob \
  "检查当前项目中的错误处理"

# 允许写文件和运行命令
kiro-cli chat --no-interactive \
  --trust-tools=read,grep,glob,write,shell \
  "修复测试并运行测试套件"

# 信任所有工具；只应在隔离且可丢弃的环境中使用
kiro-cli chat --no-interactive --trust-all-tools \
  "完成任务并验证结果"
```

`--trust-tools` 接受逗号分隔的工具名。常见内置工具及默认行为如下，完整定义见官方[内置工具](https://kiro.dev/docs/cli/reference/built-in-tools/)和[权限](https://kiro.dev/docs/cli/chat/permissions/)文档。

| 工具 | 用途 | 当前工作目录内的默认行为 |
| --- | --- | --- |
| `read`（别名 `fs_read`） | 读取文件、目录和图片 | 信任 |
| `grep` | 搜索文件内容 | 信任 |
| `glob` | 查找文件 | 信任 |
| `write`（别名 `fs_write`） | 创建、修改文件 | 需要预授权 |
| `shell`（别名 `execute_bash`） | 执行 shell 命令 | 需要预授权 |
| `aws` | 执行 AWS 操作 | 需要预授权 |

MCP 工具使用 `@服务器名/工具名` 的形式授权。对于需要细粒度路径、命令或 AWS 操作控制的任务，应创建 custom agent，在 agent 配置中设置 `allowedPaths`、`deniedPaths`、`allowedCommands` 或 `deniedCommands`，然后用 `--agent <名称>` 启动。单次命令的 `--trust-tools=read` 信任的是整个 read 工具，不是一个特定路径。

### 工具被拒绝时

下面的命令要求执行 `pwd`，但没有授权 shell：

```bash
kiro-cli chat --no-interactive \
  "必须用 shell 工具执行 pwd，只返回工具 stdout，不要推断目录"
```

2.13.0 实测的关键输出如下；示例已移除 ANSI 控制字符：

```text
stderr: Command execute_bash is rejected because it matches one or more rules on the denied list:
stderr:   - non-interactive mode (no user to approve)
stdout: > /private/tmp
exit:   0
```

工具虽然被拒绝，进程仍返回 0，而且模型根据已有工作目录信息给出了看似正确的答案。因此：

- 退出码 0 只表示这一轮对话完成，不代表所有工具都成功执行。
- stdout 中出现预期值，也不能证明该值来自成功的工具调用。
- 自动化任务应保留 stderr 供诊断，并检查预期文件、Git diff、测试结果等外部事实；不要仅凭 stderr 中的自然语言关键词改变任务状态。
- 对必须执行的工具在启动时显式授权，不要依赖模型在工具被拒后自行解释。

## 指定工作目录

`kiro-cli chat` 没有 `--cwd` / `-C` 标志。工作目录就是启动进程时的当前目录：

```bash
(
  cd /path/to/project || exit 1
  exec kiro-cli chat --no-interactive \
    --trust-tools=read,grep,glob \
    "分析这个项目"
)
```

在 CI 系统中也可以使用步骤自身的 `working-directory` 配置。工作目录会影响：

- 相对路径和 `@./file` 的解析。
- `read`、`grep`、`glob` 默认信任的范围。
- `.kiro/settings/cli.json`、`.kiro/steering/` 和 workspace agent 的发现。
- 会话存储与恢复；`--resume` 恢复当前目录最近的会话，`--list-sessions` 也按当前目录列出。

不要把额外目录误当作当前项目目录。若必须访问工作区外的路径，应通过 agent 权限配置限定路径；在无头模式下临时信任整个 `read` 或 `write` 工具会扩大访问范围。

## 模型

### 查看可用模型

模型会因 CLI 版本、账户套餐、地区和组织策略而不同。不要在通用脚本里假定某个模型一定存在，先在目标环境查询：

```bash
kiro-cli chat --list-models
kiro-cli chat --list-models --format json
kiro-cli chat --list-models --format json-pretty
```

`--format json` 的模型列表结构如下：

```json
{
  "models": [
    {
      "model_name": "claude-sonnet-4.5",
      "description": "Claude Sonnet 4.5 model",
      "model_id": "claude-sonnet-4.5",
      "context_window_tokens": 200000,
      "rate_multiplier": 1.3,
      "rate_unit": "Credit"
    }
  ],
  "default_model": "auto"
}
```

2.13.0 实测账户可见 `auto`、Claude Sonnet 4.5、Claude Sonnet 4、Claude Haiku 4.5、DeepSeek 3.2、MiniMax M2.5、MiniMax M2.1、GLM-5 和 Qwen3 Coder Next。该清单只是版本快照；选型和最新能力说明见官方[模型文档](https://kiro.dev/docs/cli/models/)，脚本判断仍应以 `--list-models --format json` 为准。

### 为单次运行指定模型

```bash
kiro-cli chat --no-interactive \
  --model claude-sonnet-4.5 \
  "审查这次改动"
```

让 Kiro 自动选择模型：

```bash
kiro-cli chat --no-interactive --model auto "审查这次改动"
```

设置以后新会话使用的默认模型：

```bash
kiro-cli settings chat.defaultModel claude-sonnet-4.5
```

模型名不存在时，请求不会发给模型，错误写入 stderr，退出码为 1：

```text
error: Model 'does-not-exist' does not exist. Available models: auto, ...
```

## 设置 effort

effort 控制支持该能力的模型在回答中投入的推理强度。值越高，通常越慢、消耗越多，但更适合复杂重构、架构分析和困难调试。官方[Effort 文档](https://kiro.dev/docs/cli/chat/effort/)定义了这些等级：

| 值 | 用途 |
| --- | --- |
| `low` | 简单问题和快速查询 |
| `medium` | 大多数开发任务 |
| `high` | 复杂重构和架构决策 |
| `xhigh` | 多文件修改和细致分析 |
| `max` | 困难调试、安全分析和复杂逻辑 |

启动无头任务时设置：

```bash
kiro-cli chat --no-interactive \
  --model claude-opus-4.8 \
  --effort high \
  "分析这个跨服务迁移方案"
```

不是所有模型都支持所有等级。官方当前列出的组合是：

| 模型 | 支持的 effort |
| --- | --- |
| Claude Opus 4.8 | `low`、`medium`、`high`、`xhigh`、`max` |
| Claude Opus 4.7 | `low`、`medium`、`high`、`xhigh`、`max` |
| Claude Opus 4.6 | `low`、`medium`、`high`、`max` |
| Claude Sonnet 4.6 | `low`、`medium`、`high`、`max` |

模型不可用时，先解决模型选择问题；不要因为帮助文本列出了 effort 就假定当前模型支持它。持久化默认值可写入用户级 `~/.kiro/settings/cli.json` 或项目级 `.kiro/settings/cli.json`：

```json
{
  "chat.modelDefaults": {
    "claude-opus-4.8": {
      "output_config": {
        "effort": "high"
      }
    }
  }
}
```

优先级从高到低为：当前会话的 `--effort`、项目级模型默认值、用户级模型默认值、内置默认值。

### effort 参数异常

缺少值会在命令行解析阶段失败，退出码为 2：

```bash
kiro-cli chat --no-interactive --effort
```

```text
error: a value is required for '--effort <EFFORT>' but none was supplied

For more information, try '--help'.
```

但 2.13.0 没有校验任意字符串。下面的非法值没有报错，模型正常回答，退出码为 0：

```bash
kiro-cli chat --no-interactive --effort impossible "hello"
```

```text
stdout: > Hello! How can I help you today?
stderr: ▸ Credits: 0.02 • Time: 3s
exit:   0
```

这不能证明 `impossible` 已生效，更可能是该值或整个 effort 设置被忽略。自动化调用方应在启动 Kiro 之前用固定枚举校验 effort，并按所选模型进一步限制合法值。

## 输出与 JSON

### 普通无头输出

简单回答的逻辑结构如下：

```text
stdout: > Hello! How can I help you today?
stderr: ▸ Credits: 0.04 • Time: 3s
exit:   0
```

如果发生工具调用，stdout 不再只有最终回答，还会包含工具说明、工具结果、耗时和最终回答：

```text
stdout: I will run the following command: pwd (using tool: shell)
stdout: /private/tmp
stdout: - Completed in 0.68s
stdout: > /private/tmp
```

2.13.0 在非交互输出中仍会写 ANSI 控制字符；实测设置 `NO_COLOR=1` 也没有完全清除。`--wrap never` 只控制换行，不会把输出变成纯净的结构化数据。

### `--format json` 的边界

`-f/--format` 只用于 `--list-models` 和 `--list-sessions` 等列表命令：

```bash
kiro-cli chat --list-models --format json
kiro-cli chat --list-sessions --format json
```

它不能把普通 chat 回答变成 JSON。2.13.0 会接受下面的命令，但仍输出带 `>` 前缀的文本，退出码为 0：

```bash
kiro-cli chat --no-interactive --format json "hello"
```

让模型“只返回 JSON”也不是可靠的传输协议：Kiro 仍可能在 stdout 中加入工具过程、`>` 前缀和 ANSI 字符，而且没有稳定的成功、错误、usage 或工具事件外层结构。因此不要把 `chat --no-interactive` 当作 JSON/JSONL API。

需要标准化结构化事件时可以选择 `kiro-cli acp`。ACP 在 stdin/stdout 上使用 JSON-RPC，需要客户端先发送 `initialize`、创建 session，再发送 prompt；它会返回 agent 消息片段、工具调用、工具更新和 turn end 事件，也声明支持图片输入。它不是 `chat --no-interactive` 的一个输出开关，不能只加一个参数完成迁移。对于固定 CLI 版本的普通 chat 适配器，也可以固定 classic UI 并解析其原始 assistant 标记，但必须在标记变化时显式失败，不能把整个 stdout 当产物。协议与消息示例见官方[ACP 文档](https://kiro.dev/docs/cli/acp/)。

## Conduct 采用的 classic UI 解析方式

Conduct 不使用 ACP。每次 Kiro 节点运行先执行：

```bash
kiro-cli settings chat.disableMarkdownRendering true
```

该命令永久写入用户当前 Kiro profile 的全局 classic UI 设置，Conduct 不备份、不恢复，也不覆盖 `KIRO_HOME`。随后执行固定参数：

```bash
kiro-cli chat --legacy-ui --no-interactive --wrap never \
  --trust-all-tools --require-mcp-startup
```

prompt 只经 stdin 传入，cwd 由父进程设置；非空 model / effort 分别追加 `--model` / `--effort`。classic UI 的最终 assistant 回答以原始字节 `\x1b[m> \x1b[0m` 开头。Conduct 取最后一个完整标记之后的文本，再清理 ANSI CSI 序列并只删除结尾换行；找不到标记时明确报输出格式变化，不退化为使用工具日志。普通无头输出不提供本次 token usage 或当前 session id，因此两项都记录为 JSON `null`。

## 图片识别

`chat --no-interactive` 没有 `--attachment` 标志。要识别本地图片，把图片放在工作目录内，并在 prompt 中明确引用路径：

```bash
kiro-cli chat --no-interactive \
  "识别 @./screenshots/error.png，说明错误信息和界面状态"
```

2.13.0 对项目内 PNG 的实测过程如下：

```text
Reading images: /path/to/project/screenshots/error.png (using tool: read)
✓ Successfully read image
- Completed in 0.0s
> 图片内容的描述……
```

这里是模型调用 `read` 工具读取图片，不是把二进制文件作为普通文本展开。官方[内置工具文档](https://kiro.dev/docs/cli/reference/built-in-tools/)明确说明 `read` 可读取文件、目录和图片。PNG 已实测可用；官方交互式 `/paste` 还说明支持 PNG、JPEG 等常见图片格式，但没有为无头 read 工具给出完整格式和大小矩阵，因此重要流程应使用小型 PNG/JPEG 预演。

权限和路径规则同普通文件：

- 当前工作目录内的图片默认可由 `read` 读取。
- 工作目录外的图片可能需要批准；无头模式无法临时批准。
- 最小权限做法是把待识别图片复制到隔离的任务工作目录，而不是使用 `--trust-tools=read` 放开所有路径。
- 图片路径含空格时用 `@"./path with spaces/image.png"`。
- 若模型没有调用 read，可在 prompt 中明确要求“必须使用 read 工具读取图片，不要根据文件名猜测”。

注意：官方[文件引用文档](https://kiro.dev/docs/cli/chat/file-references/)仍写着二进制图片不能作为普通 `@path` 文本引用；本文实测成功的原因是 Kiro 识别了图片路径并改用 read 工具。不要依赖“图片被内联成文本”这一不存在的行为。

## 大输入与上下文溢出

输入是否超限取决于模型上下文窗口、系统提示、工具定义、steering、会话历史和文本的实际 token 数，不能只用字节数准确判断。可通过模型列表中的 `context_window_tokens` 了解标称窗口：

```bash
kiro-cli chat --list-models --format json
```

### 可观察到的溢出输出

向上下文窗口为 200,000 tokens 的模型传入约 2 MB 高熵文本时，2.13.0 实测输出为：

```text
stdout: The context window has overflowed, summarizing the history...
stderr: Conversation too short to compact.
exit:   0
```

Kiro 先尝试压缩历史，但单轮超大输入没有足够的既有会话可压缩。这里依然返回 0，没有稳定的 JSON 错误对象。

高度重复文本可能被 tokenizer 更高效地编码，同样字节数不一定触发相同结果。测试上下文边界时应使用能代表真实输入的信息密度，不要用大量单一字符推断生产上限。

### 300 MiB 输入实测

本文用 stdin 传入约 300 MiB（314,572,803 字节，短指令后接重复字符）的文本。2.13.0 没有及时返回“context too long”错误，而是出现以下行为：

- 输入被 CLI 接收，没有小尺寸的本地预检错误。
- 进程超过 7 分钟没有产生 stdout 或可解析的 stderr 错误。
- `kiro-cli-chat` 常驻内存约 1.6 GiB。
- 最终由测试者发送 SIGINT 终止，因此没有自然退出码可记录。

这说明不能依赖 Kiro 为极端大输入快速、稳定地返回上下文超长错误。调用方可根据自身产品边界考虑：

- 对日志、diff 和生成文件做筛选、摘要或分块，或按自身产品约束限制输入字节数；Conduct 本身不增加 Kiro prompt 上限，始终把完整输入交给 Kiro。
- 给进程设置墙钟超时和内存限制。
- 不要在完整 stdout/stderr 中搜索权限或上下文错误关键词来判定状态：classic UI 会混入用户内容、模型回答、工具日志和文件内容，相同文本可能只是任务数据。
- 不把退出码 0 单独视为成功；还应验证是否存在完整的最终 assistant 结构。没有机器可读失败字段时，无法可靠细分零退出码业务失败，只能返回通用格式错误或交由下游验证实际产物。
- 避免把密钥、凭据和无关大文件拼入 prompt。

Linux CI 中可以使用外部超时器：

```bash
timeout 10m kiro-cli chat --no-interactive < prompt.txt
```

macOS 默认没有 GNU `timeout`；应由调用 Kiro 的父进程实现超时和终止，或安装 coreutils 后使用 `gtimeout`。

## 退出码

官方[退出码文档](https://kiro.dev/docs/cli/reference/exit-codes/)定义：

| 退出码 | 含义 |
| --- | --- |
| `0` | 命令完成 |
| `1` | 通用失败，例如认证、无输入、非法模型或操作失败 |
| `3` | 使用 `--require-mcp-startup` 时，必需的 MCP server 启动失败 |

命令行参数自身解析失败还可能返回 2，例如 `--effort` 后缺少值。更重要的是，实测工具拒绝、非法 effort 字符串和部分上下文溢出都可能返回 0，所以业务层必须验证最终 assistant 结构和实际产物；诊断自然语言只能展示，不能作为可靠的状态协议。

依赖 MCP 的任务应显式要求启动成功：

```bash
kiro-cli chat --no-interactive \
  --require-mcp-startup \
  --trust-tools=read,@github/get_issue \
  "读取 issue 并审查当前实现"
```

默认情况下，MCP 启动失败只作为警告，不改变退出码；`--require-mcp-startup` 才会快速返回 3。

## 推荐的自动化调用方式

下面的模板适用于只读分析。它固定工作目录、通过 stdin 传完整 prompt、限制权限、固定模型和 effort，并要求 MCP 启动失败时退出：

```bash
(
  cd /path/to/project || exit 1

  {
    printf '%s\n\n' '审查下面的 diff，输出风险、证据和建议；不要修改文件。'
    git diff --no-ext-diff --unified=80
  } | kiro-cli chat \
      --no-interactive \
      --model claude-opus-4.8 \
      --effort high \
      --trust-tools=read,grep,glob \
      --require-mcp-startup \
      --wrap never
)
```

落地前还应由父进程完成以下检查：

- 可从 `--list-models --format json` 确认模型存在，并验证 effort 属于固定枚举；Conduct 只校验 effort 枚举，不维护易过期的模型白名单或模型-effort 组合表。
- 按调用方需要设置执行时间或输入策略；Conduct 不增加 Kiro 专属输入上限，子进程受调用方 context 控制。
- 分开捕获 stdout 和 stderr，并清理 ANSI 字符后保存诊断。
- 不对混合终端文本做工具拒绝或上下文溢出的关键词分类；若需要可靠错误类型，应改用提供结构化状态的协议或 CLI 模式。
- 检查预期文件、测试结果或其他真实产物，而不是只看自然语言回答。
- 如果下游需要标准化 JSON-RPC 事件，可使用 ACP；固定版本的普通 chat 适配也可解析原始 assistant 标记，但标记变化必须 fail-loud。
