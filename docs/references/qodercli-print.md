# qodercli -p 用法

`qodercli` 是 Qoder CLI，交互式为默认，`-p/--print` 为非交互模式。本文档记录 `-p` 模式及相关标志。

- 版本：`qodercli --version` → `1.0.37`（记录时）
- 查看帮助：`qodercli --help`
- 标志的合法取值来自 `--help`、可执行文件内字符串，以及实测，已在各节注明验证方式。

## -p 的作用

`-p` / `--print`：运行 prompt，非交互地打印响应后退出。

```bash
qodercli -p '你好'
```

默认（`-o text`）只打印**最终响应**到 stdout。

## 设置模型

`-m, --model <model>`；`--list-models` 列出可用模型。

```bash
qodercli --list-models
qodercli -p '...' -m Performance
```

记录时 `--list-models` 输出：

```
Auto  Ultimate  Performance  Efficient  Lite
Qwen3.7-Max-DogFooding  Qwen3.7-Max  Qwen3.7-Plus
GLM-5.2  Kimi-K2.7-Code  DeepSeek-V4-Pro  DeepSeek-V4-Flash  MiniMax-M3
```

（帮助说明：Default / New Models 用模型名，Custom 用 modelID。）

## 设置 effort（推理强度）

`--reasoning-effort <level>`，独立标志，与模型解耦。

```bash
qodercli -p '...' --reasoning-effort high
```

支持的等级（来自可执行文件内 `reasoningEffort` 枚举定义）：
`disabled`、`off`、`none`、`low`、`medium`、`high`、`xhigh`、`max`。

注：该标志未做 `choices` 强校验（实测传入非法值不报错，会照常运行），请使用上列合法值。相关标志 `--context-window <size>`、`--max-output-tokens <size>`。

## 附加系统提示词

两个独立标志（来自 `--help`）：

- `--append-system-prompt <text>`：在默认系统提示词后追加。
- `--system-prompt <text>`：整体替换本次会话的系统提示词。

```bash
qodercli -p '...' --append-system-prompt '始终用中文回答'
qodercli -p '...' --system-prompt '你是一个只输出 JSON 的助手'
```

## 传递 skill

没有针对单次运行的 skill 标志；skill 通过子命令管理并被自动发现：

```bash
qodercli skills list
qodercli skills install <git-url | 本地路径>
qodercli skills link <本地路径>          # 软链，源改动即时生效
qodercli skills enable|disable|uninstall <name>
```

相关：`--plugin-dir <dir>` 加载插件目录；`--agent <name>` / `--agents <json>` 指定/定义 agent。`-o stream-json` 的 `system/init` 事件里会列出本次生效的 `skills` 列表（见下文）。

## 设置工作目录

- `-w, --cwd <dir>`：启动前切换工作目录。
- `--add-dir <dir>`：把额外目录纳入工作空间。
- `--worktree [name]`：在新的 git worktree 中启动。

```bash
qodercli -p '...' -w /path/to/project --add-dir /extra/dir
```

## JSON 输出（供解析）

`-o, --output-format <format>`，合法取值 `text` / `json` / `stream-json`（来自可执行文件内校验：`Choices: "text", "json", "stream-json"`）。

`-o json`：输出**单个** `result` JSON 对象。实测（`qodercli -p 'reply with the single word hi' -o json`）：

```json
{
  "type": "result",
  "subtype": "success",
  "duration_ms": 4420,
  "duration_api_ms": 4228,
  "is_error": false,
  "num_turns": 1,
  "result": "hi",
  "stop_reason": "end_turn",
  "total_cost_usd": 0,
  "usage": { "...": "..." },
  "modelUsage": { "...": "..." },
  "permission_denials": [],
  "session_id": "b3a9f4bc-..."
}
```

最终文本在 `result` 字段。

## 查看运行过程中的每一步

`-o stream-json`：把每一步作为一行 JSON 事件流式打印（thinking、工具调用、工具结果、最终结果）。这是查看「调用了哪些工具 / 读了什么文件 / 执行了哪些 bash」的直接方式。

```bash
qodercli -p '...' -o stream-json --permission-mode bypass_permissions
```

实测事件类型（`qodercli -p 'run the shell command: echo qtest_ok' -o stream-json`）：

- `{"type":"system","subtype":"init", ...}`：会话初始化，含 `tools`、`model`、`cwd`、`permissionMode`、`slash_commands`、`agents`、`skills`、`plugins`。
- `{"type":"assistant", ...}`：模型输出。`content` 里可为 `thinking`（思考）或 `tool_use`（工具调用，含 `name` 与 `input`，如 `Bash` 的 `command`/`description`）。
- `{"type":"user", ...}`：`content` 为 `tool_result`（含工具 `stdout`、`is_error`）。
- `{"type":"result", ...}`：终止事件，字段同上文 `-o json`。
- 另有 `{"type":"system","subtype":"hook_started"|"hook_response", ...}`：钩子执行事件。

节选（工具调用与结果）：

```json
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"...","name":"Bash","input":{"command":"echo qtest_ok","description":"Echo test string"}}], ...}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"...","content":"qtest_ok","is_error":false}]}, ...}
```

配套：`--input-format <format>`（支持 `stream-json`，用于流式 JSON 输入）；权限相关 `--permission-mode <default|accept_edits|bypass_permissions|dont_ask|auto>`、`--dangerously-skip-permissions`；工具白/黑名单 `--tools`、`--allowed-tools`、`--disallowed-tools`。

## 其他常用标志（`qodercli --help`）

| 标志 | 说明 |
| --- | --- |
| `-i, --prompt-interactive <text>` | 执行 prompt 后进入交互模式 |
| `-c, --continue` | 继续最近一次会话 |
| `-r, --resume [id]` / `--fork-session` | 恢复 / 从已有会话分叉 |
| `--attachment <file>` | 给初始 prompt 附加文件 |
| `--mcp-config <config>` / `--strict-mcp-config` | 加载 / 仅用指定的 MCP 服务 |
| `--config-dir <dir>` | 使用自定义用户级配置根目录 |
| `--output-style <style>` | 输出风格 |
| `--remote [task]` / `--remote-session <id>` | 云端远程会话 |

## 子命令

`mcp`、`plugins`（别名 `plugin`）、`skills`（别名 `skill`）、`hooks`、`agents`、`login`、`commit`。
