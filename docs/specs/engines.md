# 引擎层

conduct 把 workflow 定义里的 `engine` / `engineConfig` 落到具体 AI 编程 CLI 的一次无头（headless）子进程调用。本篇是引擎层的单一权威来源：**支持哪些引擎**、workflow **schema 字段分别映射到引擎 CLI 的哪些参数**、各字段的**枚举**、以及 conduct **默认写死了哪些 CLI 参数**。面向要新增 / 维护引擎、或要读懂 `engineConfig` 到底控制什么的实现者与评审者。

workflow 定义的整体 schema、`engineConfig` 的落盘校验入口在 [cli-authoring.md](./cli-authoring.md)；本篇只覆盖「定义 → CLI 参数」这一层。运行时如何逐节点驱动引擎、如何记录每节点的 `engine` / `engineConfig` / `tokens` 见 [cli-runtime.md](./cli-runtime.md)。

## 设计前提

- 每个引擎是**本机已安装的无头 CLI**，conduct 以子进程方式调用它，喂一段提示词、拿回一段产物文本。conduct 不内嵌任何模型调用，也不管引擎自身的登录 / 计费。
- workflow 是**无人值守**运行，故各引擎一律**跳过工具权限门**（bypass），由 conduct 在参数里写死。
- 所有引擎共用同一组入参（`RunRequest`）与出参（`RunResult`），差异只在「这组入参怎么翻译成各自的 CLI 参数」与「怎么从各自的输出里取回文本 / metadata」。
- `engine` 与 `engineConfig` 构成**判别联合**：`engine` 是判别式（tag），`engineConfig` 的合法字段由 `engine` 决定——把「引擎 / 模型 / 调优档位三者绑定」编进结构本身，不能各自独立填（见〈引擎能力表〉）。

## 支持的引擎

| engine（`Descriptor().Name`） | 本机可执行文件 | prompt 传递 | 状态 |
| --- | --- | --- | --- |
| `claude-code` | `claude` | stdin | 已实装 |
| `antigravity` | `agy` | 命令行参数（argv） | 已实装 |
| `qoder` | `qodercli` | stdin | 已实装 |
| `codex` | `codex` | stdin（`codex exec -`） | 已实装 |
| `kiro` | `kiro-cli` | stdin | 已实装 |

`engine` 字段的合法取值即上表已注册引擎的 `Descriptor().Name`。注册表见 `internal/engine/engine.go`（`Register` / `Lookup` / `Describe` / `RegisteredDescriptors` / `RegisteredNames`）；各引擎在自身文件的 `init()` 中用同一个 `Engine` 同时注册执行实现、能力、图标和 replay 函数。未登记的名字在校验期即被拒（错误附可用引擎清单）。

## 引擎抽象（conduct ↔ 引擎的统一接口）

所有引擎实现同一个 `Engine` 接口（`internal/engine/engine.go`）：`Descriptor() EngineDescriptor` 返回静态元数据，`Run(ctx, RunRequest) (RunResult, error)` 执行一次提示词。conduct 只经这两个方法与引擎交互。`EngineDescriptor` 同时包含 `Name`、`Capability`、`IconFilename` 和 `SessionReplayCommand`；注册时 fail-fast 校验名称唯一、能力开关与数组组合、数组重复值和图标文件名，并对切片做深拷贝。

**入参 `RunRequest`**——这是 conduct 能喂给任一引擎的全部参数，逐引擎翻译成 CLI 参数：

| 字段 | 类型 | 含义 | 为空时 |
| --- | --- | --- | --- |
| `Prompt` | string | 完整提示词（运行内核已把模板变量、上游产物渲染进来） | 必有 |
| `Model` | string | 模型；来自 `engineConfig.model` | 空 → 不传 `--model`，用引擎自身默认模型 |
| `WorkingDirectory` | string | 引擎读写文件的工作目录（= `workflow run --cwd`） | 空 → 继承 conduct 当前进程的工作目录 |
| `Effort` | string | 引擎特定的推理强度；来自统一的 `engineConfig.effort` | 空 → 不传调优标志，用引擎默认 |

**出参 `RunResult`**——conduct 从各引擎 JSON 输出里归一化出来的产物：

| 字段 | 类型 | 含义 | 引擎不提供时 |
| --- | --- | --- | --- |
| `Text` | string | 本次运行的产物文本，作为该 workflow 节点的输出；字段始终返回，成功但没有文本产物时允许为空字符串 | `""` |
| `DurationMilliseconds` | int64 | 本次子进程调用耗时（conduct 侧计时，非引擎回报） | 必有 |
| `Tokens` | `*int` | 引擎明确回报的本次 token 数；已知值 `0` 仍为非 `nil` 指针 | `nil` |
| `SessionID` | `*string` | 引擎明确回报的非空会话/线程 id（claude-code / qoder 的 `session_id`、antigravity 的 `conversation_id`、codex 的 `thread_id`）。conduct 记入该节点 trace，供凭引擎自带工具回放该节点（见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉） | `nil` |

token usage / session id 的缺失不能用 `0`、空字符串或字段缺省冒充。trace 始终写出 `tokens` / `sessionId`；未知值序列化为 JSON `null`。claude-code / qoder 只有 input/output token 字段都存在时才相加；antigravity 只有 `total_tokens` 存在时才返回；codex 只有收到完整 `turn.completed.usage` 时才返回；所有引擎都把空 session id 规范化为 `nil`。Kiro 普通无头输出不提供这两项，固定返回 `nil`。

`SessionReplayCommand` 接收原始 session id，返回供 CLI/UI 展示和复制的完整命令；它必须调用共享 `engine.ShellQuote`，且只能是确定性纯函数。claude-code、codex、qoder、antigravity 分别生成 `claude -r`、`codex resume`、`qodercli -r`、`agy --conversation` 命令；Kiro 为 `nil`。函数为 `nil` 或返回空串时只展示 id，不生成命令。该命令永不由 conduct 执行。

调度器传入的 `context.Context` 用于取消节点运行。适配器必须把它传入子进程调用，不保存或跨 `Run` 复用，也不自行添加 conduct 未配置的超时；取消后子进程终止并返回错误。

`RunRequest` 没有的旋钮（如系统提示词、工具白名单、上下文窗口），conduct **不下传**——一律走引擎自身默认。

## schema 字段映射

一次引擎调用的 CLI 参数由三部分拼成：conduct 写死的**默认参数**（见〈conduct 默认写死的参数〉）＋ 由 `RunRequest` 映射来的**可变参数**＋ prompt / cwd。下表是 `engineConfig` 字段与 `RunRequest` 各字段到每个引擎具体 CLI 参数的映射：

| 来源 | `RunRequest` 字段 | claude-code（`claude`） | antigravity（`agy`） | qoder（`qodercli`） | codex（`codex exec`） | kiro（`kiro-cli chat`） |
| --- | --- | --- | --- | --- | --- | --- |
| 渲染后的提示词 | `Prompt` | stdin | 命令行参数 `-p <prompt>` | stdin | stdin（`codex exec -`） | stdin |
| `engineConfig.model` | `Model` | `--model <m>` | `--model <m>` | `--model <m>`（亦接受档位名） | `--model <m>` | `--model <m>` |
| `engineConfig.effort` | `Effort` | `--effort <v>` | **拒绝**（强度编码在 model 标签后缀） | `--reasoning-effort <v>` | `-c model_reasoning_effort=<v>` | `--effort <v>` |
| `workflow run --cwd` | `WorkingDirectory` | `cmd.Dir` | `cmd.Dir`（agy 无 `--cwd`，靠切目录） | `cmd.Dir` | `cmd.Dir` | `cmd.Dir` |

要点：

- workflow、CLI、HTTP 和 UI 只使用统一字段 `effort`；适配器再把 `RunRequest.Effort` 翻译成供应商方言。Qoder 的 `--reasoning-effort` 与 Codex 的 `model_reasoning_effort` 只存在于对应适配器内部。
- **antigravity 没有独立调优字段**：推理强度作为后缀编码在 model 标签里（如 `Gemini 3.5 Flash (Medium)` / `Claude Opus 4.6 (Thinking)`），要改强度就换 model 标签。`engineConfig.effort` 在保存期被拒，因此正常调用不会向适配器传入该值。见 `docs/references/agy-print.md`。
- `Model` 为空则不传 `--model`；conduct **不探测**引擎默认模型名，运行记录里该字段留空（见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉）。

## 逐引擎详述

### claude-code（`claude`）

- **提示词**：走 stdin；工作目录用 `cmd.Dir`。
- **默认参数**：`-p --output-format json --permission-mode bypassPermissions`。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` 非空**且非 `"auto"`** → `--effort <v>`（`"auto"` / 空让 CLI 自决，不传）。
- **输出解析**：`claude -p --output-format json` 的 stdout 是单个 JSON 对象，取 `result`（→ `Text`）、`is_error`、`usage.input_tokens` + `usage.output_tokens`（→ `Tokens`）。`is_error` 为真（进程仍以退出码 0 收尾）→ 返回错误（附 `result` 文本）。**退出码非 0**（如 prompt 过长等应用层失败，此时 stderr 常为空）→ 先尝试把 stdout 解析成同一 JSON 结构，`result` 非空则优先用它报错（`claude error: <result>`）；stdout 非法 JSON 或 `result` 为空才回退到退出码 + stderr 摘要（见〈错误与退出行为〉、`claudeStdoutFailureMessage`）。
- 实现见 `internal/engine/claudecode.go`；CLI 参考 `docs/references/claudecode.md`。

### antigravity（`agy`）

- **提示词**：走**命令行参数** `-p <prompt>`（agy 无 stdin 形态）；工作目录用 `cmd.Dir`（agy 无 `--cwd`）。
- **prompt 大小上限**：经 argv 传参受 `ARG_MAX` 约束（macOS 约 1MB，含环境变量）。conduct 设保守上限 **256 KiB**（`agyPromptLimitBytes`），超限**提前**返回可读错误，胜过 exec 抛无指向性的 `argument list too long`。长上游产物叠加可能触顶——此时改用 stdin 型引擎（claude-code / qoder / codex / Kiro）或精简上游产物。
- **安全提示**：prompt 经 argv 传递，在多用户机器上对 `ps` 可见——这是 agy 无 stdin 形态的固有限制。
- **默认参数**：`-p <prompt> --output-format json --dangerously-skip-permissions`。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` **忽略**（见〈schema 字段映射〉）。
- **输出解析**：stdout 单 JSON 对象，取 `response`（→ `Text`）、`status`、`error`、`usage.total_tokens`（→ `Tokens`）。`status != "SUCCESS"` → 返回错误：优先附 `error` 字段（引擎给出的简洁失败原因）；`error` 为空才回退到截断至 500 字的 `response` 摘要（避免把模型自己写的长篇叙述分析当报错信息）。
- 实现见 `internal/engine/antigravity.go`；CLI 参考 `docs/references/agy-print.md`。

### qoder（`qodercli`）

- **提示词**：走 stdin；工作目录用 `cmd.Dir`。与 claude-code 同族。
- **默认参数**：`-p --output-format json --permission-mode bypass_permissions`（注意 qoder 是下划线 `bypass_permissions`，claude 是驼峰 `bypassPermissions`）。
- **可变参数**：`Model` 非空 → `--model <m>`（模型名或档位名，如 `Auto` / `Performance`，见 `--list-models`）；`Effort` 非空 → `--reasoning-effort <v>`（与模型解耦的独立标志）。
- **输出解析**：stdout 单 JSON 对象，取 `result`（→ `Text`）、`is_error`、`errors`、`usage.input_tokens` + `usage.output_tokens`（→ `Tokens`）。`is_error` 为真 → 返回错误：优先用 `errors` 数组拼接的报错信息（`is_error` 为真时 `result` 可能整个不存在，反序列化为空串）；`errors` 为空才回退 `result`；两者皆空则给固定英文兜底提示 `qodercli returned no specific error information`。引擎适配器错误属于技术诊断，不随 language 设置或 locale 国际化；外层固定为 `qodercli error: ...`，非预期 JSON 固定为 `qodercli returned unexpected JSON: ...`，原始引擎错误内容保持不变。
- 实现见 `internal/engine/qoder.go`；CLI 参考 `docs/references/qodercli-print.md`。

### codex（`codex exec`）

- **提示词**：走 **stdin**，用 `codex exec … -` 的 `-` 哨兵强制从 stdin 读取 prompt（codex 语义：省略 prompt 位置参数或用 `-` 时从 stdin 读）。选 stdin 而非 argv，规避 agy 那种 `ARG_MAX` 上限，与 claude-code / qoder 同族。工作目录用 `cmd.Dir`。
- **默认参数**：`exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -`（`-` 在 PROMPT 位）。权限用 `--dangerously-bypass-approvals-and-sandbox`（无沙箱 + 全权限）与其它引擎的 bypass 对齐；`--skip-git-repo-check` 允许在非 git 仓库目录运行（workflow 的 cwd 未必是 git 仓库）。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` 非空 → `-c model_reasoning_effort=<v>`（codex 无专用调优标志，`-c key=value` 覆盖配置项；value 按 TOML 解析失败即当字面字符串，故 `-c model_reasoning_effort=high` 即字符串 `"high"`）。
- **输出解析**：codex `--json` 的 stdout 是 **JSON Lines 事件流**（每行一个事件对象），**非**单个 JSON 对象——与 claude-code、antigravity、qoder 三个单对象 JSON 引擎的输出结构不同，需逐行扫描按 `type` 归一化（逐行读取范式同 `internal/store/runs.go` 的 `LoadTrace`，避开 `bufio.Scanner` 的 token 上限）：
  - `thread.started` → `thread_id`（→ `RunResult.SessionID`）
  - `item.completed` 且 `item.type == "agent_message"` → `item.text`（→ `RunResult.Text`，取**最后一条**）
  - `turn.completed` → `usage.input_tokens` + `usage.output_tokens`（→ `RunResult.Tokens`，取最后一个 turn）
  - `turn.failed` / `error` → 返回错误（失败优先，即使进程退 0）
  - 其余事件（`turn.started` / `item.started` / 其它 `item.*`）忽略；无法解析的行显式报错（附该行前 200 字），不静默跳过；既无失败事件也无 `agent_message` → 报错 `codex did not produce a final agent_message`（不假装成功）。
- **样例 stdout**（每行一个对象，见 `docs/references/codex.md`）：
  ```jsonl
  {"type":"thread.started","thread_id":"0199a213-…"}
  {"type":"item.completed","item":{"type":"agent_message","text":"最终产物"}}
  {"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}
  ```
- 实现见 `internal/engine/codex.go`；CLI 参考 `docs/references/codex.md`。

### kiro（`kiro-cli chat`）

- **权限副作用**：每次运行前在当前 Kiro profile 的 `settings/permissions.yaml` 中幂等确保存在 `capability: all` + `effect: allow`。`KIRO_HOME` 非空时以它为 profile 根目录，否则使用 `~/.kiro`。已有规则、未知字段、注释和文件权限均保留；已有相同规则时不重写文件。缺失时原子创建，新增文件权限为 `0600`。YAML 无法解析或 `rules` 不是数组时不启动 chat，返回权限配置错误及已耗时长。这是经用户确认的全局持久副作用，也会影响用户手动启动的 Kiro；已有 `deny` / `ask` 和 Kiro 硬编码保护仍高于 `allow`。
- **提示词与目录**：完整 prompt 走 stdin，工作目录用 `cmd.Dir`；conduct 不做 Kiro 专属字节数或 token 数限制。图片继续把本地绝对路径写进 prompt。
- **默认参数**：`chat --v3 --no-interactive --wrap never`。不传 `--agent`、`--resume` 或 `--resume-id`，因此复用用户默认 agent、认证、全局与项目配置并创建新 session。
- **可变参数**：`Model` 非空 → `--model <m>`；`Effort` 非空 → `--effort <v>`。model 是开放集合；effort 在保存期限定为 `low` / `medium` / `high` / `xhigh` / `max`。
- **输出解析**：v3 普通 chat 没有 JSON 模式。stdout 不含工具事件，但会把本轮所有可见 assistant `Say` 文本无结构地直接拼接；适配器保留完整 stdout，只删除结尾的 `CR` / `LF`。它不能可靠提取最后一条 text，空 stdout 允许成功。stderr 承载 INFO、tool 状态与错误诊断，不拼入 `Text`。
- **失败判定**：chat 非零退出时保留退出码；stderr 非空时附清理 ANSI、截断至 500 字的 stderr，stderr 为空时只报告退出码，不读取 stdout 补充诊断。退出 `0` 时返回完整 stdout。Kiro 没有机器可读的业务失败字段，conduct 不用 `is rejected because`、`context window has overflowed` 等自然语言关键词猜测权限或上下文状态，因为相同文本可以合法出现在用户输入、模型回答、工具日志和被读取的源码中；工具和任务是否成功由 workflow 的真实产物约束判断。
- **进程收尾**：三条标准流连接到创建后立即 unlink、仅当前用户可访问的普通临时文件，避免 Kiro v3 的 ACP 子进程持有匿名 pipe 导致 EOF 卡死。每次调用使用独立进程组；顶层进程结束后，无论成功或失败，都向该组仍存活的子进程发送终止信号。临时文件不形成持久目录项。
- **metadata**：普通无头输出不提供本次 token usage 或当前 session id，`Tokens` / `SessionID` 固定为 `nil`。Kiro 仍在用户 profile 中持久化 session，可在相同工作目录用 `kiro-cli chat --list-sessions` 查看；conduct 不读取私有 session 文件或猜测 id。
- 实现见 `internal/engine/kiro.go`；CLI 调研见 `docs/references/kiro-cli.md`。

## 图片输入

需要引擎「看」一张图片时，把图片的**本地绝对路径**写进 prompt 文本即可——各引擎自带的文件读取工具会自行打开该路径的图片并理解其内容。conduct **不提供**专门的图片入参（`RunRequest` 无图片字段）、也不做任何 URL 下载或附件管道。

- **已验证行为**：`claude-code` / `codex` / `qoder` / `antigravity` / `kiro` 五引擎在无头模式下，仅凭 prompt 里给出的本地绝对路径，均能由自身文件工具读取图片；能否正确理解仍取决于所选模型。
- **为什么不接专门旗标**：五引擎里只有个别有图片附件旗标（如 `qoder` 的 `--attachment <file>`，见 `docs/references/qodercli-print.md`），`claude`、`agy` 与 Kiro 普通 chat 没有 conduct 可统一使用的附件旗标，且 `codex exec` 参考文档（`docs/references/codex.md`）也未列图片旗标；已知旗标都只吃**本地文件路径**、无一接受 URL。既然「路径写进 prompt」是五引擎的统一约定，就不新增一套不统一的图片管道。
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
| kiro | `settings/permissions.yaml` 中 `all/allow` | 持久允许 v3 开发工具无人值守执行；已有更严格规则仍优先 |
| kiro | `chat --v3 --no-interactive` | 使用具备 shell/bash 工具的 v3 无头运行一次 |
| kiro | `--wrap never` | 禁止主动插入终端宽度硬换行 |

除上表外，conduct **不注入**系统提示词、工具白名单、超时、上下文窗口等——这些走各引擎自身默认。claude-code / antigravity / qoder 依赖 `--output-format json`、codex 依赖 `--json`；Kiro v3 普通 chat 没有机器输出模式，成功时返回完整 stdout（见各引擎输出解析）。

## 引擎能力表

`engineConfig` 的合法字段是判别联合，由 `engine` 决定。每个引擎的 `Descriptor()` 内嵌必有的 `EngineCapability`：是否接受 `model`（`AllowsModel`）、非约束性模型建议（`ModelSuggestions`）、是否接受统一的 `effort`（`AllowsEffort`）及其强制枚举（`EffortValues`）。执行实现和 capability 通过同一个 `Engine` 同时注册，不存在“引擎已注册但 capability 缺失”的状态。

`EngineCapability` 的四个字段始终有明确值。`ModelSuggestions` / `EffortValues` 在无内容时是非 `nil` 空切片，HTTP JSON 固定输出 `[]` 而不是 `null`；布尔字段则如实输出 `true` / `false`。

| engine | `model` | model 建议值（`ModelSuggestions`，非白名单） | `effort` | `EffortValues` |
| --- | --- | --- | --- | --- |
| `claude-code` | 接受（Claude 系） | `sonnet` · `opus` · `fable` | `effort` | `low` · `medium` · `high` · `xhigh` · `max` · `ultracode` · `auto`（实际可用档位随模型） |
| `antigravity` | 接受（完整 model 标签） | 无 | 无 | ——（推理强度编码在 model 标签后缀） |
| `qoder` | 接受（模型名或档位） | `Auto` · `Ultimate` · `Performance` · `Efficient` · `Lite` | 接受 | `disabled` · `off` · `none` · `low` · `medium` · `high` · `xhigh` · `max` |
| `codex` | 接受（GPT 系） | `gpt-5.6-sol` · `gpt-5.6-terra` · `gpt-5.6-luna` · `gpt-5.5` · `gpt-5.3-codex-spark` | 接受 | `low` · `medium` · `high` · `xhigh` |
| `kiro` | 接受（开放模型名） | `auto` · `claude-sonnet-5` · `claude-opus-4.8` · `gpt-5.6-sol` · `gpt-5.6-terra` · `gpt-5.6-luna` | `effort` | `low` · `medium` · `high` · `xhigh` · `max` |

`engineConfig` 两个字段（`internal/workflow/definition.go` 的 `EngineConfig`）——`model` / `effort`——**均选填**，校验时逐字段核对：

- `effort` 由 claude-code、qoder、codex、kiro 接受；antigravity 拒绝。
- 调优字段的值须落在该字段枚举内，否则拒。
- `model` 当前**不做白名单**：接受任意非空串（待有权威模型表再收紧）；省略则用引擎默认模型。`ModelSuggestions` 只是 UI 下拉建议项，不参与 `workflow.Validate` 强校验；为空只表示该引擎未登记建议值，不表示不接受 `model`。
- 每个 agent 节点独立按上表校验其 `engine` + `engineConfig`；`START` / `END` 两个保留标记节点不承载 `engine`/`engineConfig`，不参与此表（见 [cli-authoring.md](./cli-authoring.md)〈落盘校验规则〉）。

具体校验流程与错误格式（如 `nodes[0].engineConfig.effort: engine="antigravity" 不接受 effort`）见 [cli-authoring.md](./cli-authoring.md)〈落盘校验规则〉。旧字段 `reasoningEffort` 不兼容：它和 `xxxabc` 一样由严格 JSON 解码作为普通未知字段拒绝，无别名、迁移或专门诊断。

> **descriptor 是活的**：随引擎演进在各适配器的 `Descriptor()` 内维护；CLI help、workflow 校验和 HTTP/UI 都动态消费这份事实源。

## 错误与退出行为

引擎层的错误一律**显式上抛、绝不静默**（承项目「错误不吞」）。这些适配器错误与底层技术诊断固定使用英文，不随 language 设置或 locale 切换；引擎原始返回的 stderr / `result` / `error` / `message` 内容原样保留。子进程失败经 `commandError`（`internal/engine/exec.go`）转译为带引擎名的可读错误。下列 `<engine>` 占位是错误前缀里的引擎名，取**CLI 二进制名**（`claude` / `agy` / `qodercli`），非注册名（`claude-code` / `qoder`）：

- **非零退出码**：`<engine> exited with code <code>: <stderr summary>`（stderr 截断至 500 字）。**claude-code 例外**：非零退出时先尝试把 stdout 解析成 JSON 取 `result`，非空则优先返回 `claude error: <result>`；只有 stdout 非法 JSON 或 `result` 为空才落到这条退出码+stderr 摘要（见〈claude-code〉小节）。
- **找不到可执行文件等**：`failed to invoke <engine>: <original error>`。
- **输出非预期 JSON**：claude-code / antigravity 使用 `<engine> returned unexpected JSON: <err> (first 200 characters of stdout: …)`；qoder 使用 `qodercli returned unexpected JSON: …`；codex 使用 `codex returned unexpected JSON: failed to parse line <line>: …`。
- **Kiro 输出失败**：非零退出时只把 stderr 作为第三方诊断来源；stderr 为空则错误仅含退出码，stdout 不作为错误信息兜底。权限 YAML 读取、解析或原子写入失败时不启动 chat。exit `0` 时完整 stdout（允许为空）作为回答，不依据回答或 stderr 中的自然语言分类错误。进程组清理失败显式上抛；若 chat 同时失败则两个错误都保留。
- **引擎自报失败**（进程退出码为 0、但引擎自身报告业务失败）：
  - claude-code：`is_error` 为真 → 附 `result` 文本。
  - qoder：`is_error` 为真 → 优先附 `errors` 数组拼接的报错信息（`result` 此时可能整个不存在）；`errors` 为空才回退 `result`；两者皆空给兜底提示。
  - antigravity：`status != "SUCCESS"` → 优先附 `error` 字段；为空才回退截断至 500 字的 `response` 摘要。
  - codex：JSONL 中出现 `turn.failed` 或 `error` 事件 → 返回该事件携带的错误信息；若事件没有可用消息则返回明确的 codex 失败兜底文案。
  - Kiro：普通 chat 没有结构化失败字段，不做自然语言错误分类；只有进程退出码参与最低层成功失败判定。
- **prompt 超限**（仅 antigravity）：超 256 KiB 时**在调用前**返回 `agy passes prompts as command-line arguments; prompt too long (…); use a stdin-based engine or reduce upstream output`。

这些错误如何冒泡到 `workflow run` 的退出码见 [cli-runtime.md](./cli-runtime.md)。

## 实现状态

- **Descriptor 注册表**：**已实装**。五个引擎各自在自身文件同时注册执行实现、capability、图标和 replay；workflow 校验、CLI help、CLI/HTTP TraceView 与 UI 均消费该注册表。通用测试覆盖 fail-fast 校验、排序和深拷贝。
- **引擎 `claude-code` / `antigravity` / `qoder`**：**已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，均经真实调用冒烟通过；单测 `internal/engine/exec_test.go` 用假二进制覆盖参数 / stdin / cwd 接线与 JSON 解析）。三者的 `RunResult.SessionID` 解析（从 `session_id` / `conversation_id` 取）**已实装**——在各自结果结构体补取已有字段，无新增 CLI 参数（单测 `internal/engine/session_test.go`）。
- **引擎 `codex`**：**已实装**。`internal/engine/codex.go` 同时注册执行实现与 descriptor（`model? + effort ∈ {low, medium, high, xhigh}`）。契约见本篇〈codex〉小节、〈schema 字段映射〉、〈conduct 默认写死的参数〉、〈引擎能力表〉——codex 输出为 JSONL 事件流，逐行扫描按 type 归一化（单测 `internal/engine/codex_test.go` 覆盖 thread.started / agent_message / turn.completed / turn.failed / 无法解析行 / 无 agent_message 各路径）。
- **引擎 `kiro`**：**已实装**。`internal/engine/kiro.go` 注册 Kiro，幂等合并全局 v3 权限、调用 `chat --v3 --no-interactive`、把完整 stdout 归一化为 `Text`，并以临时文件和独立进程组处理 ACP 子进程收尾；`internal/engine/kiro_test.go` 以 PATH 假二进制覆盖权限保留/去重/错误、环境继承、参数/stdin/cwd、完整文本、非零退出、自然语言碰撞、临时文件与子进程清理。
- **`RunResult.SessionID`**：**已实装可空语义**。claude-code / qoder 从 `session_id`、antigravity 从 `conversation_id`、codex 从 `thread_id` 填充非空指针；缺失、JSON `null` 或空字符串返回 `nil`。Kiro 不提供当前 id，固定 `nil`。conduct 不额外拷贝 transcript。
- **`RunResult.DurationMilliseconds`**：由 conduct 侧计时（`internal/engine/exec.go` 的 `runCommand`），非引擎回报。
- **`RunResult.Tokens`**：**已实装可空语义**。结构化引擎仅在所需 usage 字段完整存在时返回指针（已知 `0` 有效），否则返回 `nil`；Kiro 固定 `nil`。trace 对未知值明确写 JSON `null`。

## 待确认

- **`model` 白名单**：当前不校验模型名（任意非空串放行）。`ModelSuggestions` 只是 UI 建议值，不是权威模型表。是否随每引擎维护一张权威模型表并收紧为白名单，待定——收紧会更早暴露拼写错误，但增加维护面。
- **子进程超时**：conduct 当前不对引擎调用设超时（依赖 `ctx` 取消与引擎自身超时，如 agy 默认 `--print-timeout 5m`）。是否在引擎层统一加可配置超时，待定。
- **codex `effort` 枚举**：沿用 `{low, medium, high, xhigh}`。codex 另支持 `minimal` 档，是否纳入待定（纳入更全，但需确认当前 codex-cli 版本对所选模型确实接受该档）。
- **codex token 口径**：`Tokens = input_tokens + output_tokens`，与 claude-code / qoder 一致；codex 另回报 `reasoning_output_tokens`（推理 token）与 `cached_input_tokens`，本方案不计入以免与其它引擎口径不一 / 重复计数。是否单列推理 token，待定。
