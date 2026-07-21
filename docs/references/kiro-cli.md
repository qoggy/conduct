# Kiro CLI v3 无头模式

本文只记录 Conduct 直接调用 `kiro-cli chat --v3 --no-interactive` 时必须关注的行为：模型、effort、session、开发权限、文本输出、图片与错误。结论来自官方文档和本机 `kiro-cli 2.13.0`、`--model auto` 实测。

官方仍把 v3 标为 Early Access，并把非 TUI v3 列为已知缺口；2.13.0 虽然可以运行本文命令，但这些行为还不是稳定兼容承诺。升级 Kiro 后必须重新预演。

官方资料：

- [CLI v3](https://kiro.dev/docs/cli/v3/)
- [v3 permissions](https://kiro.dev/docs/cli/v3/permissions/)
- [v3 agent config](https://kiro.dev/docs/cli/v3/agent-config/)
- [CLI commands](https://kiro.dev/docs/cli/reference/cli-commands/)

## Conduct 调用基线

```bash
kiro-cli chat \
  --v3 \
  --no-interactive \
  --wrap never \
  --model auto \
  --effort high \
  "完成任务并验证结果"
```

- 父进程设置工作目录；Kiro 的文件查找、相对路径、steering 和 session 归属都受它影响。
- prompt 可以作为位置参数，也可以从 stdin 提供。Conduct 应继续通过 stdin 传递，避免命令行长度和转义问题。
- `--wrap never` 禁止终端按显示宽度插入换行，避免程序捕获的文本随终端宽度变化。
- 不要追加 `--legacy-ui` 或 `--trust-all-tools`；它们与 v3 不兼容。

## 模型

查询当前账户可用模型：

```bash
kiro-cli chat --list-models --format json
```

2.13.0 返回 `default_model: "auto"`。本机主要建议值为：

| model | context window |
| --- | ---: |
| `auto` | 1,000,000 |
| `claude-sonnet-5` | 1,000,000 |
| `claude-opus-4.8` | 1,000,000 |
| `gpt-5.6-sol` | 272,000 |
| `gpt-5.6-terra` | 272,000 |
| `gpt-5.6-luna` | 272,000 |

这些值是 suggestion，不是 Conduct 的校验白名单。模型会随账户、地区和 CLI 版本变化；Conduct 只传递用户填写的非空字符串，由 Kiro 判断是否可用。

`--model auto` 实测可以正常完成文本、开发工具和图片任务，但普通 chat 输出不会说明 auto 最终选择了哪个底层模型。

非法模型的实际输出：

```text
stdout: The selected model is not available. Please select a different model and try again. (Request ID: <ID>)
stderr: [ERROR] ... "errorType":"InvalidModelError" ...
stderr: Error: The model '<MODEL>' is not available. Please select a different model and try again. (Request ID: <ID>)
exit:   1
```

Conduct 处理非零退出时应使用退出码和 stderr 生成错误。stdout 虽然也有面向用户的提示，但不能当作成功结果或错误诊断兜底。

## effort

帮助中列出的值是：

```text
low, medium, high, xhigh, max
```

2.13.0 配合 `--model auto` 的实测结果：

| effort | 结果 |
| --- | --- |
| `low` | 正常回答，退出 `0` |
| `medium` | 正常回答，退出 `0` |
| `high` | 正常回答，退出 `0` |
| `xhigh` | 正常回答，退出 `0` |
| `max` | 正常回答，退出 `0` |
| `impossible` | 也正常回答并退出 `0` |

因此 Kiro 2.13.0 不校验 effort 枚举，`impossible` 能运行也不代表参数生效。Conduct 必须只接受上述五个值。

缺少 effort 参数值会在命令行解析阶段失败：

```text
stdout: <empty>
stderr: error: a value is required for '--effort <EFFORT>' but none was supplied
exit:   2
```

## session 与恢复

### 创建和列出

每次成功的 v3 无头 chat 都会落盘一个 session。当前目录的列表命令为：

```bash
kiro-cli chat --list-sessions --format json
```

2.13.0 的结构是按目录分组的数组：

```json
[
  {
    "cwd": "/Users/example/project",
    "sessions": [
      {
        "sessionId": "sess_b377c333-2310-40c9-b49d-2f2f772fd94a",
        "source": "v3",
        "title": "Remember ...",
        "updatedAt": "2026-07-20T13:19:03.530Z",
        "executionTarget": "local"
      }
    ]
  }
]
```

一次 `chat --v3 --no-interactive` 的 stdout/stderr 不会输出当前 session ID。列表没有调用方 correlation ID，并发运行时不能用“更新时间最新”猜测。因此 Conduct 的 `sessionId` 必须为 `null`。

普通 chat 输出也没有本次调用的 token usage。模型 context window 和本地 session 文件里的 credits 不是 token 数；Conduct 的 token 字段必须为 `null`。

### `--resume` 和 `--resume-id` 实测失效

在隔离目录中完成一次 v3 chat 后，2.13.0 分别实测：

```bash
kiro-cli chat --v3 --no-interactive --resume "说出上一轮记住的代码"
kiro-cli chat --v3 --no-interactive --resume-id <VALID_V3_SESSION_ID> "说出上一轮记住的代码"
```

两种命令都接受参数并退出 `0`，但模型明确表示没有上一轮历史；Kiro 创建了新的 v3 session，原 session 的 `updatedAt` 没有变化。也就是说，当前组合不会报错，却没有真正恢复。

Conduct 不能实现 Kiro session replay，也不应向 UI 提供一个看似可用的恢复命令。升级到明确修复该行为的 Kiro 版本前，每个节点只能视为独立 session。

## 开发权限：工具无需审批

shell/bash 是 Conduct 接入 Kiro 的硬性要求。v3 提供 `Run Command`，但默认权限只允许 `pwd`、只读 Git 等低风险命令；写文件和普通开发命令通常进入 `ask`。无头模式无法显示审批界面，`ask` 会被拒绝。

v3 不使用 classic 的 trust 参数：

```text
$ kiro-cli chat --v3 --no-interactive --trust-all-tools "hello"
error: the following arguments are not supported with --agent-engine=v3: --trust-all-tools
error: ACP initialize failed
exit: 1
```

权限必须来自当前 profile 的 `settings/permissions.yaml`（设置 `KIRO_HOME` 时位于 `$KIRO_HOME/settings/permissions.yaml`，否则位于 `~/.kiro/settings/permissions.yaml`）、workspace permission 或 agent 的 `permissions`。官方给出的 CI 全权限规则是：

```yaml
rules:
  - capability: all
    effect: allow
```

Conduct 在每次 Kiro 运行前幂等确保用户级规则中存在 `all/allow`，但不覆盖整个权限文件；已有相同规则时不重写。规则优先级是 `deny > ask > allow`，所以用户已有的 deny/ask 仍然生效；Kiro 自身禁止修改权限文件等硬编码限制也不能被覆盖。

隔离实测使用的最小开发 agent 为：

```markdown
---
description: Headless development
tools: [read, write, shell]
permissions:
  rules:
    - capability: fs_read
      effect: allow
    - capability: fs_write
      effect: allow
      match: ["**"]
    - capability: shell
      effect: allow
      match: ["*"]
---
Complete the requested development task and verify the result.
```

实测过程调用 `File Search`、`Read File`、`Write File` 和 `Run Command`：

- 文件按要求创建，内容完全匹配。
- shell 验证真实通过。
- stderr 没有 `[denied]` 或 approval 提示。
- 工具状态只写 stderr，不污染 stdout。

这证明普通工作区内的读、写、shell 开发链路可以无需审批运行。是否“权限全开”仍以最终合并后的用户规则为准，不能仅凭 CLI 参数声明成功；Conduct 还必须验证真实文件、diff 和测试结果。

## chat 能否获取最后一条 text

不能可靠获取“最后一条 assistant text”。v3 普通 chat 没有 JSON/JSONL 输出；`--format json` 只适用于模型和 session 列表。

无头 stdout 的真实边界是：

- 不包含工具调用和工具输出。
- 包含本轮所有可见的 assistant `Say` 文本。
- 多个 `Say` 会按顺序直接拼接，没有结构化边界，不保证只有最后一段。

一次开发任务在 session 中产生了两条 assistant text：

```text
Say 1: 明白了，需要创建 output.txt ...
Say 2: 验证通过。... FINAL_TEXT_OK
```

stdout 返回了两段拼接后的完整文本，而不是只有 `Say 2`。工具事件则只出现在 stderr。

因此 Conduct 可以把成功进程的完整 stdout 作为 `RunResult.Text`，但不能声称它是最后一条 text，也不能靠换行、ANSI 或自然语言规则截取最后一段。读取 `~/.kiro/sessions/` 内部文件同样不可取：当前 session ID 不可可靠关联，而且该文件格式不是 chat CLI 契约。

另一个程序集成问题是进程收尾：2.13.0 在 stdout/stderr 接匿名 pipe 时，曾多次出现内容已写完但管道不 EOF。改用本次调用专属的普通临时文件后，顶层 `kiro-cli` 能正常退出并读取完整输出，但本轮正常完成的测试仍遗留了 8 个 PPID 为 1 的 `acp-server.js --transport=stdio`。

Conduct 因此使用创建后立即 unlink 的普通临时文件避免 pipe EOF 卡死，并让每次 Kiro 调用运行在独立进程组中；无论成功、失败、取消还是超时，顶层进程结束后都清理该组内仍存活的子进程。

## 图片

`chat --v3 --no-interactive` 没有图片附件参数。把 Kiro 可访问的本地绝对路径写进 prompt，并要求读取图片：

```bash
kiro-cli chat --v3 --no-interactive --model auto --effort low \
  "读取图片 /absolute/path/kiro.png，描述主体和背景颜色"
```

2.13.0 实测读取了 Conduct 的 Kiro 图标：

```text
stderr: [tool] Read File
stderr: [tool] status: InProgress
stderr: [tool] status: Completed
stdout: 背景是紫色，主体是白色幽灵形状……
exit:   0
```

图片依赖 `fs_read` 权限和模型的多模态能力。路径必须在 Kiro 进程可见的本地文件系统中；工作区外图片可能被用户权限规则拒绝。

## 异常输出

2.13.0 v3 的实际结果：

| 场景 | stdout | stderr 关键内容 | exit |
| --- | --- | --- | ---: |
| 正常文本回答 | assistant `Say` 拼接文本 | INFO/KRS 日志 | `0` |
| 开发工具成功 | assistant `Say` 拼接文本 | tool InProgress/Completed | `0` |
| 工具无权限 | 模型对拒绝的自然语言解释 | `[denied] ... approval is not supported in non-interactive mode` | `0` |
| shell 命令失败后模型结束 | 模型最终说明 | tool Failed | `0` |
| 非法 model | model 不可用提示 | InvalidModelError | `1` |
| 非法 effort 字符串 | 正常回答 | INFO/KRS 日志 | `0` |
| `--effort` 缺值 | 空 | 参数缺值 | `2` |
| 无头模式没有输入 | 空 | `Input must be supplied when running in non-interactive mode` | `1` |
| `--trust-all-tools` | 空 | v3 不支持参数、ACP initialize failed | `1` |
| resume 未恢复 | 模型表示没有历史 | 正常 INFO/KRS 日志 | `0` |

由此得到 Conduct 的解析规则：

1. 非零退出：使用退出码和 stderr 报错；清理 ANSI、限制诊断长度，不回退读取 stdout。
2. 退出 `0`：完整 stdout 是回答文本，但不代表工具或任务成功。
3. stderr 正常情况下也有 INFO 和 tool 事件，不能用“stderr 非空”判失败。
4. 不对 stdout/stderr 做自然语言字符串或正则分类；用户输入、代码和模型回答都可能包含相同文字。
5. 权限、命令和业务是否成功，要检查文件、Git diff、测试等真实产物。
6. v3 `auto` 的超长输入/上下文溢出本轮没有得到可复现输出，不能编造专用错误解析；由 Kiro 决定输入上限，Conduct 只提供通用超时、取消和进程组清理。

## Conduct 接入结论

Kiro v3 已具备 Conduct 必需的模型、effort、图片和无审批开发工具能力，但 2.13.0 仍有三个明确限制：

- 无头 session resume 实际失效，`sessionId` 只能为 `null`。
- chat stdout 会拼接多个 assistant text，不能只取最后一条。
- 匿名 pipe 可能不 EOF，ACP server 还可能在正常退出后成为孤儿进程；必须用普通临时文件捕获，并在每次调用结束后清理进程组。

Conduct 的 Kiro 适配器已按这些边界切换到 v3；不再调用 classic UI，也不再修改 `chat.disableMarkdownRendering`。
