# agy -p 用法

`agy` 是 Google Antigravity CLI（取代 gemini cli）。本文档记录 `agy -p` 非交互模式的用法。

- 版本：`agy --version` → `1.0.16`（记录时）
- 查看帮助：`agy --help`
- 本文档中「未在 `--help` 列出」的标志，依据可执行文件内的字符串与实测得出，已注明验证方式。

## -p 的作用

`-p` 是 `--print` 的短别名（`--prompt` 亦为其别名）。作用：运行单条 prompt，非交互地把模型响应打印到 stdout 后退出。

```bash
agy -p "hello"
```

默认以 Markdown 文本形式打印**最终响应**到 stdout，不打印中间步骤（工具调用、读文件、执行 bash）。

## 全局标志（`agy --help`）

| 标志 | 说明 |
| --- | --- |
| `-p` / `--print` / `--prompt` | 运行单条 prompt 并打印响应 |
| `--print-timeout` | print 模式等待超时，默认 `5m0s` |
| `-i` / `--prompt-interactive` | 运行初始 prompt 后进入交互会话（与 `-p` 用途互斥） |
| `--model` | 指定本次会话模型 |
| `-c` / `--continue` | 继续最近一次会话 |
| `--conversation` | 按 ID 恢复此前会话 |
| `--project` / `--new-project` | 指定 / 新建项目 |
| `--add-dir` | 向工作空间加入目录（可重复） |
| `--sandbox` | 在启用终端限制的沙箱中运行 |
| `--dangerously-skip-permissions` | 自动批准所有工具权限请求 |
| `--log-file` | 覆盖 CLI 日志文件路径 |

## 设置模型

`agy models` 列出可用模型，`--model` 传入完整标签：

```bash
agy models
agy -p "hello" --model "Gemini 3.5 Flash (Medium)"
```

记录时 `agy models` 输出：

```
Gemini 3.5 Flash (Medium)
Gemini 3.5 Flash (High)
Gemini 3.5 Flash (Low)
Gemini 3.1 Pro (Low)
Gemini 3.1 Pro (High)
Claude Sonnet 4.6 (Thinking)
Claude Opus 4.6 (Thinking)
GPT-OSS 120B (Medium)
```

## 设置 effort（推理强度）

没有独立的 effort 标志。effort 作为后缀编码在模型标签里（`Low` / `Medium` / `High`；Claude 系列为 `Thinking`）。要改 effort，就换对应后缀的模型标签：

```bash
agy -p "..." --model "Gemini 3.5 Flash (High)"
agy -p "..." --model "Gemini 3.1 Pro (Low)"
```

## 附加系统提示词

没有 CLI 标志用于追加系统提示词。附加规则通过上下文文件 `GEMINI.md` 注入：

- 全局：`~/.gemini/GEMINI.md`
- 项目级：工作目录下的 `GEMINI.md`（进入工作空间时自动发现）

验证：把规则写进 `~/.gemini/GEMINI.md` 后，`agy -p` 的响应会引用其中内容（实测响应中出现 `RULE[user_global]` 并按该文件规定的 prompt 结构作答）。

## 传递 skill / plugin

没有针对单次运行的 skill 标志。两种机制：

- 工作空间内放置 `skills.json` / `.agents/skills.json` / `plugins.json` 清单，进入工作空间时自动发现（依据可执行文件内字符串：`the agent will automatically discover .agents/skills.json`）。
- 用插件子命令管理：

```bash
agy plugin list
agy plugin import [gemini|claude]      # 从 gemini / claude 导入
agy plugin install <plugin@marketplace>
agy plugin enable|disable|uninstall <name>
```

## 设置工作目录

`agy` 在当前工作目录（cwd）中运行；没有 `--cwd` 标志。做法：先 `cd` 到目标目录，或用 `--add-dir` 追加额外目录（可重复）：

```bash
cd /path/to/project && agy -p "..."
agy -p "..." --add-dir /extra/dir1 --add-dir /extra/dir2
```

## JSON 输出（供解析）

用 `--output-format json` 让 stdout 输出单个 JSON 对象（该标志未在 `--help` 列出，实测有效）：

```bash
agy -p "reply with the single word hi" --output-format json
```

实测输出（单行，此处格式化展示）：

```json
{
  "conversation_id": "40bc5e43-1097-48c2-b369-375edf7f85dc",
  "status": "SUCCESS",
  "response": "hi\n",
  "duration_seconds": 3.82,
  "num_turns": 1,
  "usage": {
    "input_tokens": 17488,
    "output_tokens": 630,
    "thinking_tokens": 626,
    "total_tokens": 18118
  }
}
```

- 该 JSON 只含**最终结果**，不含逐步的工具调用/读文件/bash 明细。
- 可选 `--json-schema '<schema>'` 约束响应结构，仅当 `--output-format json` 时可用（依据可执行文件内字符串：`--json-schema can only be used when --output-format is 'json'`；未逐例实测）。

## 查看运行过程中的每一步

`-p` 模式的 stdout 不输出中间步骤。运行痕迹记录在这些位置：

- CLI 日志：`~/.gemini/antigravity-cli/log/cli-*.log`（含所调用的后端 URL、选定模型、流式请求等），可用 `--log-file` 改路径。
- 会话轨迹：`~/.gemini/antigravity-cli/conversations/<conversation_id>.db`（SQLite，逐轮记录）。
- 交互模式（不带 `-p`，或用 `-i`）会在 TUI 中实时展示工具调用等步骤。

（说明：未发现将逐步工具/bash/读文件事件流式打印到 `-p` stdout 的标志。日志文件的粒度是 HTTP/流事件，非直接的工具调用语义；如需从 `.db` 还原每一步，需另行解析该 SQLite。）

## 子命令

`changelog`、`help`、`install`、`models`、`plugin`（别名 `plugins`）、`update`。
