# 引擎层

conduct 把 workflow 定义里的 `engine` / `engineConfig` 落到具体 AI 编程 CLI 的一次无头（headless）子进程调用。本篇是引擎层的单一权威来源：**支持哪些引擎**、workflow **schema 字段分别映射到引擎 CLI 的哪些参数**、各字段的**枚举**、以及 conduct **默认写死了哪些 CLI 参数**。面向要新增 / 维护引擎、或要读懂 `engineConfig` 到底控制什么的实现者与评审者。

workflow 定义的整体 schema、`engineConfig` 的落盘校验入口在 [cli-authoring.md](./cli-authoring.md)；本篇只覆盖「定义 → CLI 参数」这一层。运行时如何逐步驱动引擎、如何记录每步的 `engine` / `engineConfig` / `tokens` 见 [cli-runtime.md](./cli-runtime.md)。

## 设计前提

- 每个引擎是**本机已安装的无头 CLI**，conduct 以子进程方式调用它，喂一段提示词、拿回一段产物文本。conduct 不内嵌任何模型调用，也不管引擎自身的登录 / 计费。
- workflow 是**无人值守**运行，故各引擎一律**跳过工具权限门**（bypass），由 conduct 在参数里写死。
- 所有引擎共用同一组入参（`RunRequest`）与出参（`RunResult`），差异只在「这组入参怎么翻译成各自的 CLI 参数」与「怎么从各自的 JSON 输出里取回文本 / token」。
- `engine` 与 `engineConfig` 构成**判别联合**：`engine` 是判别式（tag），`engineConfig` 的合法字段由 `engine` 决定——把「引擎 / 模型 / 调优档位三者绑定」编进结构本身，不能各自独立填（见〈引擎能力表〉）。

## 支持的引擎

| engine（`Name()`） | 本机可执行文件 | prompt 传递 | 状态 |
| --- | --- | --- | --- |
| `claude-code` | `claude` | stdin | 已实装 |
| `antigravity` | `agy` | 命令行参数（argv） | 已实装 |
| `qoder` | `qodercli` | stdin | 已实装 |
| `codex` | `codex` | stdin（`codex exec -`） | 已实装 |

`engine` 字段的合法取值即上表已注册（`Name()` 在注册表内）引擎。注册表见 `internal/engine/engine.go`（`Register` / `Lookup` / `RegisteredNames`）；各引擎在各自 `*.go` 的 `init()` 里注册（`internal/engine/claudecode.go` / `antigravity.go` / `qoder.go` / `codex.go`）。未登记的名字在校验期即被拒（错误附可用引擎清单）。

## 引擎抽象（conduct ↔ 引擎的统一接口）

所有引擎实现同一个 `Engine` 接口（`internal/engine/engine.go`）：`Name() string` 返回稳定标识，`Run(ctx, RunRequest) (RunResult, error)` 执行一次提示词。conduct 只经这两个方法与引擎交互。

**入参 `RunRequest`**——这是 conduct 能喂给任一引擎的全部参数，逐引擎翻译成 CLI 参数：

| 字段 | 类型 | 含义 | 为空时 |
| --- | --- | --- | --- |
| `Prompt` | string | 完整提示词（运行内核已把模板变量、上游产物渲染进来） | 必有 |
| `Model` | string | 模型；来自 `engineConfig.model` | 空 → 不传 `--model`，用引擎自身默认模型 |
| `WorkingDirectory` | string | 引擎读写文件的工作目录（= `workflow run --cwd`） | 空 → 继承 conduct 当前进程的工作目录 |
| `Effort` | string | 引擎特定的推理强度；来自 `engineConfig.effort` 或 `reasoningEffort`（见〈schema 字段映射〉） | 空 → 不传调优标志，用引擎默认 |

**出参 `RunResult`**——conduct 从各引擎 JSON 输出里归一化出来的产物：

| 字段 | 类型 | 含义 | 引擎不提供时 |
| --- | --- | --- | --- |
| `Text` | string | 本次运行的产物文本，作为该 workflow 节点的输出 | 必有 |
| `DurationMilliseconds` | int64 | 本次子进程调用耗时（conduct 侧计时，非引擎回报） | 必有 |
| `Tokens` | int | 本次消耗的 token 数 | `0` |
| `SessionID` | string | 本次运行的引擎会话/线程 id（各引擎从自身 JSON 输出取：claude-code / qoder 的 `session_id`、antigravity 的 `conversation_id`、codex 的 `thread_id`）。conduct 记入该步 trace，供凭引擎自带工具回放本步（见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉） | 空串 |

`RunRequest` 没有的旋钮（如系统提示词、工具白名单、上下文窗口），conduct **不下传**——一律走引擎自身默认。

## schema 字段映射

一次引擎调用的 CLI 参数由三部分拼成：conduct 写死的**默认参数**（见〈conduct 默认写死的参数〉）＋ 由 `RunRequest` 映射来的**可变参数**＋ prompt / cwd。下表是 `engineConfig` 字段与 `RunRequest` 各字段到每个引擎具体 CLI 参数的映射：

| 来源 | `RunRequest` 字段 | claude-code（`claude`） | antigravity（`agy`） | qoder（`qodercli`） | codex（`codex exec`） |
| --- | --- | --- | --- | --- | --- |
| 渲染后的提示词 | `Prompt` | stdin | 命令行参数 `-p <prompt>` | stdin | stdin（`codex exec -`） |
| `engineConfig.model` | `Model` | `--model <m>` | `--model <m>` | `--model <m>`（亦接受档位名） | `--model <m>` |
| `engineConfig.effort` | `Effort` | `--effort <v>` | **忽略**（强度编码在 model 标签后缀） | —— | —— |
| `engineConfig.reasoningEffort` | `Effort` | —— | —— | `--reasoning-effort <v>` | `-c model_reasoning_effort=<v>` |
| `workflow run --cwd` | `WorkingDirectory` | `cmd.Dir` | `cmd.Dir`（agy 无 `--cwd`，靠切目录） | `cmd.Dir` | `cmd.Dir` |

要点：

- **`effort` 与 `reasoningEffort` 映射到同一个 `RunRequest.Effort`**，但一个 `engineConfig` 上二者互斥（判别联合决定：claude-code 只认 `effort`，qoder 与 codex 只认 `reasoningEffort`，见〈引擎能力表〉），故不会撞车。
- **antigravity 没有独立调优字段**：推理强度作为后缀编码在 model 标签里（如 `Gemini 3.5 Flash (Medium)` / `Claude Opus 4.6 (Thinking)`），要改强度就换 model 标签。故 `agy` 引擎刻意**忽略** `RunRequest.Effort`（`engineConfig` 上也不接受 effort 类字段）。见 `docs/references/agy-print.md`。
- `Model` 为空则不传 `--model`；conduct **不探测**引擎默认模型名，运行记录里该字段留空（见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉）。

## 逐引擎详述

### claude-code（`claude`）

- **提示词**：走 stdin；工作目录用 `cmd.Dir`。
- **默认参数**：`-p --output-format json --permission-mode bypassPermissions`。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` 非空**且非 `"auto"`** → `--effort <v>`（`"auto"` / 空让 CLI 自决，不传）。
- **输出解析**：`claude -p --output-format json` 的 stdout 是单个 JSON 对象，取 `result`（→ `Text`）、`is_error`、`usage.input_tokens` + `usage.output_tokens`（→ `Tokens`）。`is_error` 为真 → 返回错误（附 `result` 文本）。
- 实现见 `internal/engine/claudecode.go`；CLI 参考 `docs/references/claudecode.md`。

### antigravity（`agy`）

- **提示词**：走**命令行参数** `-p <prompt>`（agy 无 stdin 形态）；工作目录用 `cmd.Dir`（agy 无 `--cwd`）。
- **prompt 大小上限**：经 argv 传参受 `ARG_MAX` 约束（macOS 约 1MB，含环境变量）。conduct 设保守上限 **256 KiB**（`agyPromptLimitBytes`），超限**提前**返回可读错误，胜过 exec 抛无指向性的 `argument list too long`。长上游产物叠加可能触顶——此时改用 stdin 型引擎（claude-code / qoder）或精简上游产物。
- **安全提示**：prompt 经 argv 传递，在多用户机器上对 `ps` 可见——这是 agy 无 stdin 形态的固有限制。
- **默认参数**：`-p <prompt> --output-format json --dangerously-skip-permissions`。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` **忽略**（见〈schema 字段映射〉）。
- **输出解析**：stdout 单 JSON 对象，取 `response`（→ `Text`）、`status`、`usage.total_tokens`（→ `Tokens`）。`status != "SUCCESS"` → 返回错误（附 status 与 `response` 摘要）。
- 实现见 `internal/engine/antigravity.go`；CLI 参考 `docs/references/agy-print.md`。

### qoder（`qodercli`）

- **提示词**：走 stdin；工作目录用 `cmd.Dir`。与 claude-code 同族。
- **默认参数**：`-p --output-format json --permission-mode bypass_permissions`（注意 qoder 是下划线 `bypass_permissions`，claude 是驼峰 `bypassPermissions`）。
- **可变参数**：`Model` 非空 → `--model <m>`（模型名或档位名，如 `Auto` / `Performance`，见 `--list-models`）；`Effort` 非空 → `--reasoning-effort <v>`（与模型解耦的独立标志）。
- **输出解析**：stdout 单 JSON 对象，取 `result`（→ `Text`）、`is_error`、`usage.input_tokens` + `usage.output_tokens`（→ `Tokens`）。`is_error` 为真 → 返回错误。
- 实现见 `internal/engine/qoder.go`；CLI 参考 `docs/references/qodercli-print.md`。

### codex（`codex exec`）

- **提示词**：走 **stdin**，用 `codex exec … -` 的 `-` 哨兵强制从 stdin 读取 prompt（codex 语义：省略 prompt 位置参数或用 `-` 时从 stdin 读）。选 stdin 而非 argv，规避 agy 那种 `ARG_MAX` 上限，与 claude-code / qoder 同族。工作目录用 `cmd.Dir`。
- **默认参数**：`exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -`（`-` 在 PROMPT 位）。权限用 `--dangerously-bypass-approvals-and-sandbox`（无沙箱 + 全权限）与其它引擎的 bypass 对齐；`--skip-git-repo-check` 允许在非 git 仓库目录运行（workflow 的 cwd 未必是 git 仓库）。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` 非空 → `-c model_reasoning_effort=<v>`（codex 无专用调优标志，`-c key=value` 覆盖配置项；value 按 TOML 解析失败即当字面字符串，故 `-c model_reasoning_effort=high` 即字符串 `"high"`）。
- **输出解析**：codex `--json` 的 stdout 是 **JSON Lines 事件流**（每行一个事件对象），**非**单个 JSON 对象——与其它三引擎的单对象输出结构不同，需逐行扫描按 `type` 归一化（逐行读取范式同 `internal/store/runs.go` 的 `LoadTrace`，避开 `bufio.Scanner` 的 token 上限）：
  - `thread.started` → `thread_id`（→ `RunResult.SessionID`）
  - `item.completed` 且 `item.type == "agent_message"` → `item.text`（→ `RunResult.Text`，取**最后一条**）
  - `turn.completed` → `usage.input_tokens` + `usage.output_tokens`（→ `RunResult.Tokens`，取最后一个 turn）
  - `turn.failed` / `error` → 返回错误（失败优先，即使进程退 0）
  - 其余事件（`turn.started` / `item.started` / 其它 `item.*`）忽略；无法解析的行显式报错（附该行前 200 字），不静默跳过；既无失败事件也无 `agent_message` → 报错「codex 未产出最终 agent_message」（不假装成功）。
- **样例 stdout**（每行一个对象，见 `docs/references/codex.md`）：
  ```jsonl
  {"type":"thread.started","thread_id":"0199a213-…"}
  {"type":"item.completed","item":{"type":"agent_message","text":"最终产物"}}
  {"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}
  ```
- 实现见 `internal/engine/codex.go`；CLI 参考 `docs/references/codex.md`。

## 图片输入

需要引擎「看」一张图片时，把图片的**本地绝对路径**写进 prompt 文本即可——各引擎自带的文件读取工具会自行打开该路径的图片并理解其内容。conduct **不提供**专门的图片入参（`RunRequest` 无图片字段）、也不做任何 URL 下载或附件管道。

- **已验证行为**：`claude-code` / `codex` / `qoder` / `antigravity` 四引擎在无头模式下，仅凭 prompt 里给出的本地绝对路径，均能读出并识别图片内容（同一张图实测，四者都正确认出）。
- **为什么不接专门旗标**：四引擎里只有个别有图片附件旗标（如 `qoder` 的 `--attachment <file>`，见 `docs/references/qodercli-print.md`），`claude` 与 `agy` 根本没有，且 conduct 走的 `codex exec` 非交互子命令其参考文档（`docs/references/codex.md`）也未列图片旗标；已知的图片旗标都只吃**本地文件路径**、无一接受 URL。既然「路径写进 prompt」对四引擎全部生效，就没必要为少数引擎接一套不统一、还得先把 URL 下载成本地文件的图片管道——那是不必要的复杂度（承〈引擎抽象〉「`RunRequest` 没有的旋钮一律不下传、走引擎自身默认」）。
- **边界**：
  - 路径须是引擎进程**可访问**的本地文件；sandbox 场景下图片必须先存在于 sandbox 文件系统内（这是文件可达性问题，与有没有图片旗标无关）。
  - 只支持本地路径、**不支持 URL**——远端图片请调用方自行下载到本地，再把本地路径写进 prompt。
  - 能否正确理解图片取决于引擎 / 模型自身的多模态能力，conduct 不做保证、也无从代劳。

## conduct 默认写死的参数

无论 `engineConfig` 怎么填，下列参数由 conduct 对每次调用**无条件附加**（不暴露给 workflow 定义、不可覆盖）：

| engine | 恒定附加的参数 | 作用 |
| --- | --- | --- |
| claude-code | `-p` | 无头（print）单次运行模式 |
| claude-code | `--output-format json` | 输出机器可解析的单 JSON 对象 |
| claude-code | `--permission-mode bypassPermissions` | 无人值守，跳过工具权限门 |
| antigravity | `-p <prompt>` | 无头单次运行（prompt 即经此 `-p` 旗标的取值下传） |
| antigravity | `--output-format json` | 同上 |
| antigravity | `--dangerously-skip-permissions` | 无人值守，自动批准所有工具权限 |
| qoder | `-p` | 无头单次运行模式 |
| qoder | `--output-format json` | 同上 |
| qoder | `--permission-mode bypass_permissions` | 无人值守，跳过工具权限门 |
| codex | `exec` | 非交互无头运行子命令 |
| codex | `--json` | 输出 JSONL 事件流（硬依赖：解析 `Text` / `Tokens` / `SessionID`） |
| codex | `--dangerously-bypass-approvals-and-sandbox` | 无人值守，无沙箱 + 全权限（与其它引擎 bypass 对齐） |
| codex | `--skip-git-repo-check` | 允许在非 git 仓库目录运行 |
| codex | `-`（PROMPT 位） | 强制从 stdin 读取 prompt |

除上表外，conduct **不注入**系统提示词、工具白名单、超时、上下文窗口等——这些走各引擎自身默认。机器可解析的 JSON 输出是硬依赖：claude-code / antigravity / qoder 靠 `--output-format json`、codex 靠 `--json`，conduct 据此解析 `Text` / `Tokens` / `SessionID`（见各引擎输出解析）。

## 引擎能力表

`engineConfig` 的合法字段是判别联合，由 `engine` 决定。校验内核（`internal/engine/capability.go` 的 `engineCapabilities`）为每个引擎登记一张能力表：是否接受 `model`、调优字段名（`EffortField`）及其枚举（`EffortValues`）。**已注册但未在能力表列出的引擎，一律不接受任何 `engineConfig` 字段。**

| engine | `model` | 调优字段 | 调优字段枚举 |
| --- | --- | --- | --- |
| `claude-code` | 接受（Claude 系） | `effort` | `low` · `medium` · `high` · `xhigh` · `max` · `ultracode` · `auto`（实际可用档位随模型） |
| `antigravity` | 接受（完整 model 标签） | 无 | ——（推理强度编码在 model 标签后缀） |
| `qoder` | 接受（模型名或档位） | `reasoningEffort` | `disabled` · `off` · `none` · `low` · `medium` · `high` · `xhigh` · `max` |
| `codex` | 接受（GPT 系） | `reasoningEffort` | `low` · `medium` · `high` · `xhigh` |

`engineConfig` 三个字段（`internal/workflow/definition.go` 的 `EngineConfig`）——`model` / `effort` / `reasoningEffort`——**均选填**，校验时逐字段核对：

- `effort` 仅 `claude-code` 认；`reasoningEffort` 仅 `qoder` 与 `codex` 认；给错引擎即拒（如给 `antigravity` 设 `effort`）。
- 调优字段的值须落在该字段枚举内，否则拒。
- `model` 当前**不做白名单**：接受任意非空串（待有权威模型表再收紧）；省略则用引擎默认模型。
- node 与其 `evaluator` 各自独立按上表校验（`evaluator` 用同一套 `engine` + `engineConfig` 结构）。

具体校验流程与错误格式（如 `nodes[0].engineConfig.effort: engine="antigravity" 不认 effort`）见 [cli-authoring.md](./cli-authoring.md)〈落盘校验规则〉。

> **能力表是活的**：随引擎演进维护。改这张表须同步 `internal/engine/capability.go`、本节、以及 `create` / `edit` 的 `--help` 里由能力表动态生成的定义结构说明（见 [cli-authoring.md](./cli-authoring.md)）。

## 错误与退出行为

引擎层的错误一律**显式上抛、绝不静默**（承项目「错误不吞」）。子进程失败经 `commandError`（`internal/engine/exec.go`）转译为带引擎名的可读错误。下列 `<engine>` 占位是错误前缀里的引擎名，取**CLI 二进制名**（`claude` / `agy` / `qodercli`），非注册名（`claude-code` / `qoder`）：

- **非零退出码**：`<engine> 退出码 <code>: <stderr 摘要>`（stderr 截断至 500 字）。
- **找不到可执行文件等**：`<engine> 调用失败: <原始错误>`。
- **输出非预期 JSON**：`<engine> 输出非预期 JSON: <err>（stdout 前 200 字: …）`。
- **引擎自报失败**：claude-code / qoder 的 `is_error` 为真、antigravity 的 `status != "SUCCESS"` → 附引擎回报的错误文本上抛。
- **prompt 超限**（仅 antigravity）：超 256 KiB 时**在调用前**返回错误，提示改用 stdin 型引擎或精简上游产物。

这些错误如何冒泡到 `workflow run` 的退出码见 [cli-runtime.md](./cli-runtime.md)。

## 实现状态

- **引擎 `claude-code` / `antigravity` / `qoder`**：**已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，均经真实调用冒烟通过；单测 `internal/engine/exec_test.go` 用假二进制覆盖参数 / stdin / cwd 接线与 JSON 解析）。三者的 `RunResult.SessionID` 解析（从 `session_id` / `conversation_id` 取）**已实装**——在各自结果结构体补取已有字段，无新增 CLI 参数（单测 `internal/engine/session_test.go`）。
- **引擎 `codex`**：**已实装**。`internal/engine/codex.go` 注册 codex 引擎；能力表（`capability.go`）含 `codex` 行（`model?` + `reasoningEffort ∈ {low, medium, high, xhigh}`）。契约见本篇〈codex〉小节、〈schema 字段映射〉、〈conduct 默认写死的参数〉、〈引擎能力表〉——codex 输出为 JSONL 事件流，逐行扫描按 type 归一化（单测 `internal/engine/codex_test.go` 覆盖 thread.started / agent_message / turn.completed / turn.failed / 无法解析行 / 无 agent_message 各路径）。
- **`RunResult.SessionID`**：**已实装**。四引擎从各自 JSON 输出的会话 id 字段（claude-code / qoder 的 `session_id`、antigravity 的 `conversation_id`、codex 的 `thread_id`）填充；conduct 记入该步 trace 的 `sessionId`（见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉）。四引擎默认均持久化会话 transcript，故 id 指向可回放的真实会话；conduct 不额外拷贝 transcript。
- **`RunResult.DurationMilliseconds`**：由 conduct 侧计时（`internal/engine/exec.go` 的 `runCommand`），非引擎回报。
- **`RunResult.Tokens`**：各引擎均从自身 `usage` 字段取（codex 取 `input_tokens` + `output_tokens`，口径同其它引擎的 input+output）；引擎不回报时为 `0`。

## 待确认

- **`model` 白名单**：当前不校验模型名（任意非空串放行）。是否随每引擎维护一张权威模型表并收紧为白名单，待定——收紧会更早暴露拼写错误，但增加维护面。
- **子进程超时**：conduct 当前不对引擎调用设超时（依赖 `ctx` 取消与引擎自身超时，如 agy 默认 `--print-timeout 5m`）。是否在引擎层统一加可配置超时，待定。
- **codex `reasoningEffort` 枚举**：沿用 `{low, medium, high, xhigh}`。codex 另支持 `minimal` 档，是否纳入待定（纳入更全，但需确认当前 codex-cli 版本对所选模型确实接受该档）。
- **codex token 口径**：`Tokens = input_tokens + output_tokens`，与 claude-code / qoder 一致；codex 另回报 `reasoning_output_tokens`（推理 token）与 `cached_input_tokens`，本方案不计入以免与其它引擎口径不一 / 重复计数。是否单列推理 token，待定。
