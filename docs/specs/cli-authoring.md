# conduct CLI 编辑态命令规格

> 本文规定 conduct CLI 的**工作流编辑态命令面**——如何脚手架、导入、局部编辑、复制、改名、删除、查询一份工作流**定义**，以及定义的数据模型与入库校验规则。这些命令**不运行工作流、不烧 token**，纯粹读写 store 里的定义。
>
> **运行**一份工作流、查看运行记录，见 [docs/specs/cli-runtime.md](./cli-runtime.md)；可视化界面、工具自更新、版本 / 帮助等工具层命令，见 [docs/specs/cli-tooling.md](./cli-tooling.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

下面几条是整份规格的地基。它们决定了每条命令的形态，若你不认同，先改这里、其余随之调整：

- **工作流是「有名字的托管对象」，不是散落文件。** 存储在一个 *store* 里，`create / copy / edit / rename / delete / show / node …` 一律按**名字**定位工作流（`list` 作用于整个 store，无需名字）；不接受直接传文件路径——要跑手头的一份 JSON，先 `create --definition` 入库拿到名字，再按名字运行（见 [cli-runtime.md](./cli-runtime.md)〈workflow run〉）。
  - 理由：只有工作流是托管对象时，「删除 / 查询 / 局部编辑」才值得做成子命令；若只是文件，它们就退化成 `rm` / `ls` / 手改 JSON。
- **store 位置（固定）**：工作流统一存放在全局 `~/.conduct/workflows/`（每份一个 `<name>.json`）。所有命令固定读写此 store，**不支持自定义存储位置**（完整落盘布局见〈workflows/ 落盘结构〉；运行记录 `~/.conduct/runs/` 的布局见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉）。
- **命令风格 noun-first（统一）**：资源操作命令形如 `conduct <noun> <verb>`（对标 `gh` / `kubectl`），无顶层动词命令。工作流是 `workflow` 名词族；其下的**节点**是子资源，字段级编辑走 `conduct workflow node <verb>`（如 `conduct workflow node set`），与 `gh` 里 `gh workflow …` 子命令层叠一致。运行记录 `run` 名词族见 [cli-runtime.md](./cli-runtime.md)。**例外**是不针对单一资源的顶层工具命令 `conduct version` / `conduct ui` / `conduct update` / `conduct help`（见 [cli-tooling.md](./cli-tooling.md)）。
- **对 AI-bash 与人类双友好（北极星）**：每条能力都以非交互、可脚本化的 CLI 命令提供（查询类带 `--json` 机读，变更类以退出码表达成败）；同时以 `conduct ui` 提供可视化界面（人类层，见 [cli-tooling.md](./cli-tooling.md)〈ui〉）。**关键不变量：UI 无独占能力**——它做的每件编辑操作都有对应的、可单独完成的 CLI 命令（整体替换 ↔ `workflow edit`、改字段 / 挂拆循环 ↔ `workflow node set`、改单份提示词 ↔ `workflow node set-prompt`），UI 只把它们聚合成人看的视图，绝不新增「只有界面能做」的编辑功能。
- **名称即文件名**：工作流以 `<name>.json` 落盘。`<name>` 限定 `[A-Za-z0-9._-]+`，不含路径分隔符。（注意：这套工作流名规则与 node `id` 的 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$` 不同，见〈落盘校验规则〉。）

## 数据模型（工作流实体）

conduct 有两个模型，像数据库的两张表；本文只详述编辑态相关的 **workflow** 表，**run** 表见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉。

| 实体 | 是什么 | 主键 | 可变性 | 落盘 |
| --- | --- | --- | --- | --- |
| **workflow**（工作流） | 一份定义 | `name` | 可变（`edit` / `node set` / `node set-prompt` 改定义、`rename` 改名、`copy` 派生新对象） | `workflows/<name>.json`（完整记录 `{ name, createdAt, updatedAt, nodes }`） |
| **run**（运行记录） | 一次执行 | run id | 不可变（历史） | `runs/<id>/`（详见 [cli-runtime.md](./cli-runtime.md)） |

- **run 靠快照自解释，与编辑态解耦**：run 在开跑那一刻冻结一份完整 workflow 快照，此后 `edit` / `node set` / `delete` 一个 workflow **都不影响它已有的 run**（历史照样可看、可复现）。故本文所有变更命令只动 `workflows/<name>.json`，`runs/` 分毫不动。快照语义详见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉。

## 命令总览

| 命令 | 作用 | 对应诉求 |
| --- | --- | --- |
| `conduct workflow create <name>` | 脚手架 / 导入一份新工作流入库 | 创建 |
| `conduct workflow copy <src> <dst>` | 从既有工作流复制出一份新名字的变体 | 复制 |
| `conduct workflow edit <name>` | 从 stdin 整体替换既有工作流（保存即校验） | 编辑（全量） |
| `conduct workflow node set <name> <id>` | 改某节点 / 评测官的结构化字段、挂 / 拆两种循环 | 编辑（局部·结构化） |
| `conduct workflow node set-prompt <name> <id>` | 以原始文本设置某节点（或其 evaluator）的提示词 | 编辑（局部·提示词） |
| `conduct workflow node show <name> <id>` | 导出单节点定义 / 单份提示词纯文本 | 查询（节点） |
| `conduct workflow rename <old> <new>` | 给既有工作流改名（改标识，不动定义） | 改名 |
| `conduct workflow delete <name>...` | 从 store 删除一个 / 多个工作流 | 删除 |
| `conduct workflow list` | 列出 store 内全部工作流 | 查询 |
| `conduct workflow show <name>` | 查看单个工作流（可附展开预览） | 查询 |

> 没有独立的 `validate` 命令：定义校验已内化进所有变更命令（`create` / `copy` / `edit` / `node set` / `node set-prompt`）的落盘环节——不合规即拒绝保存、原文件不变，规则见〈落盘校验规则〉。需要不运行、不花 token 地核对一份定义合不合法、并预览它会展开成什么步骤时，用 `show --expand`（载入即校验，再打印展开）。

> **局部编辑 vs 全量替换**：`edit` 从 stdin 读**整份**新定义替换，适合可视化编辑（`conduct ui`）或整体重写；`node set` / `node set-prompt` 只改**一个节点的一处结构化字段（含挂 / 拆循环）/ 一份提示词**，输入里只出现那一处改动——避免为微调一个 `loopCount`、加一个自循环而复述整份含长提示词的 JSON（省 token、免误伤未改的提示词）。两条路径殊途同归，都在落盘前复用同一套校验。

> 文档分层：各命令的 `--help` 只做精简速查（本命令怎么用，遵循社区标准）；教程 / 概念 / 最佳实践这类**跨命令的长文档**不塞进 `--help`，改由 `conduct help <主题>` 输出（对标 `go help <topic>`）。主题按概念组织（如 `prompts` 讲 promptTemplate 怎么写好），文档 `go:embed` 进二进制随发布走，相关命令的 `--help` 末尾留一行指针。

## 全局约定

**全局选项**（`-h, --help` 所有命令通用）：打印该命令的用法与选项后退出 `0`。（`--version` 仅根命令，见 [cli-tooling.md](./cli-tooling.md)〈version〉。）

**通用选项**（涉及结构化输出的命令支持）：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（人类可读表格 / 装饰全部关闭），便于脚本消费 |

**「出参」的含义**：本文每条命令的「输出」小节同时约定 **stdout**（正常结果）、**stderr**（错误信息）、**退出码**、以及**落盘副作用**（若有）。统一退出码见文末〈退出码约定〉。

**fail-loud 基线**：错误一律显式报出并以非 0 退出，绝不静默吞掉、绝不用空动作冒充成功（承自项目编码规范「错误不吞 / 不假装成功」）。

---

## workflow create — 创建

**用途**：新建一份工作流并入库。默认脚手架出一份最小骨架；带 `--definition` 时改为从 **stdin** 读入一份完整 workflow 定义导入。入库前一律校验（规则见〈落盘校验规则〉），校验不过不落盘。

**用法**：

```
conduct workflow create <name> [--definition]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称，兼作 store 内的 key 与文件名（`<name>.json`） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--definition` | 布尔 | `false` | 从 **stdin** 读入一份完整 workflow 定义导入（替代脚手架骨架）；导入前完整校验 |

**输出**：

- 成功：stdout 打印 `✓ 已创建 <name>`；退出 `0`（不回显存储位置——工作流按名字寻址，落在哪是实现细节）。
- 同名已存在：stderr `工作流 <name> 已存在（先 delete 或换名）`；退出 `1`；不写盘（`create` 只创建、不覆盖）。
- `--definition` 内容非合法 JSON / 校验不过：stderr 打印字段级校验错误；退出 `1`（校验类，见〈退出码约定〉）；不写盘。
- 落盘副作用：成功时把该工作流写入 store。

**示例**：

```bash
conduct workflow create my-flow                            # 脚手架一份最小骨架
cat wf.json | conduct workflow create ported --definition  # 从 stdin 导入一份定义
```

示意输出：

```
✓ 已创建 my-flow
```

---

## workflow copy — 复制（造变体）

**用途**：从一个既有工作流复制出一份新名字的工作流——一步造变体，替掉 `show --json > f`、改 `f`、`create --definition < f` 的多步外部拼装。复制的是**定义主体**（`nodes`）；`<dst>` 是全新的托管对象，`createdAt` / `updatedAt` 重新戳当前时刻，不继承 `<src>` 的时间戳。语义同 `create`：`<dst>` 已存在则拒绝、**不覆盖**。

**用法**：

```
conduct workflow copy <src> <dst>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<src>` | 是 | 源工作流名称（须存在） |
| `<dst>` | 是 | 目标新名称，须匹配 `[A-Za-z0-9._-]+` 且未被占用 |

**输出**：

- 成功：stdout `✓ 已复制 <src> → <dst>`；退出 `0`；落盘写 `<dst>.json`（`nodes` = `<src>` 的 nodes、`name` = `<dst>`、`createdAt` / `updatedAt` = 当前时刻）。
- `<src>` 不存在：stderr `工作流 <src> 不存在`；退出 `1`；不写盘。
- `<dst>` 已存在：stderr `工作流 <dst> 已存在（先 delete 或换名）`；退出 `1`；不写盘（不覆盖，语义同 `create` / `rename`）。
- `<dst>` 名称非法（不匹配 `[A-Za-z0-9._-]+`）：stderr 报错；退出 `2`（非法参数）。
- 落盘前复用同一套校验（`<src>` 已在库应已合法，仍防御式校验一遍；不过即拒、不写盘）。
- 落盘副作用：仅成功时新增 `workflows/<dst>.json`；`<src>` 与 `runs/` 分毫不动。

**示例**：

```bash
conduct workflow copy autopilot autopilot-heavy   # 基于 autopilot 造一个重型变体，再 node set 调档
```

示意输出：

```
✓ 已复制 autopilot → autopilot-heavy
```

---

## workflow edit — 编辑（全量替换）

**用途**：编辑既有工作流——从 **stdin** 读入一份**完整**新定义整体替换。适合可视化编辑器导出后写回、或整体重写；只改个别字段 / 提示词请优先用 `node set` / `node set-prompt`（省 token、免误伤）。**保存即校验（规则见〈落盘校验规则〉），校验不过则拒绝写入、保留原定义不变**（不写坏数据）。可视化编辑请用 `conduct ui`（见 [cli-tooling.md](./cli-tooling.md)〈ui〉），不在本命令内；**改名**用 `workflow rename`——`edit` 只换定义、不换名。

**用法**：

```
conduct workflow edit <name>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 待编辑的工作流名称 |

**输入**：完整新定义从 **stdin** 读入（`cat new.json | conduct workflow edit <name>`）。stdin 是终端（无管道）时报错退出 `2`、**不挂起等待输入**；需要可视化编辑用 `conduct ui`。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>`；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- 校验不过 / stdin 非合法 JSON：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- `<name>` 不存在：stderr `工作流 <name> 不存在`；退出 `1`。
- stdin 是终端（无管道输入）：stderr 报缺少定义并提示 `conduct ui`；退出 `2`。
- 落盘副作用：仅在校验通过时覆盖入库。

**示例**：

```bash
cat new.json | conduct workflow edit my-flow          # 用 new.json 整体替换
conduct workflow show my-flow --json > w.json         # 导出 → 改 w.json → cat w.json | edit 写回
```

示意输出：

```
✓ 已更新 my-flow
```

---

## workflow node set — 局部编辑（结构化字段）

**用途**：只改**某个节点的结构化字段**，不重发整份定义——覆盖节点主体与其 evaluator 的引擎 / 模型 / 档位、节点显示名、循环次数，以及**挂载 / 拆除**两种循环（evaluator 自循环、redoTarget 回跳）。改一处，输入里就只出现那一处：既消灭「为把 `loopCount` 从 5 改成 1 而复述几百行含长提示词 JSON」的 token 成本与误伤风险，也让 agent 最高频的「给节点加 / 去一个自循环」不必回退全量 `edit`。每个可调项一个**带类型的显式 flag**（`--help` 天然就是「这节点能调什么」的可发现文档），而非不透明路径或对象字面量。在内存中就地改完后，**复用整份定义的同一套校验**再落盘（不写坏数据）。提示词正文走 `node set-prompt`（本命令不碰 `promptTemplate`）。

**用法**：

```
conduct workflow node set <name> <id> [--evaluator] \
    [--engine <e>] [--model <m>] [--effort <v>] [--reasoning-effort <v>] \
    [--display-name <s>] [--loop-count <n>] [--redo-target <id>] \
    [--no-evaluator] [--no-redo]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 目标节点 id（须存在于该工作流） |

**选项**（**至少给一个**字段选项或拆除选项，否则退出 `2`；`--evaluator` 是作用域修饰，不单独计作操作）：

| 选项 | 类型 | 说明 |
| --- | --- | --- |
| `--evaluator` | 布尔 | 把 `--engine` / `--model` / `--effort` / `--reasoning-effort` 的作用域切到该节点的 **evaluator**（评测官）而非节点主体；不加时作用于节点主体。`--loop-count` / `--redo-target` 始终作用于节点级，与本旗标无关。拆除评测官走 `--no-evaluator`（不经本旗标） |
| `--engine <e>` | 字符串 | 设引擎，取值见〈workflow 定义 schema〉能力表（`claude-code` / `antigravity` / `qoder`）。改 engine 可能使原 `engineConfig` 字段被新 engine 拒收 → 见下「诚实边界」。**配 `--evaluator` 时**：节点无 evaluator 则以此引擎**新建**评测官（补默认提示词 + `loopCount`，见下）；已有则改其引擎 |
| `--model <m>` | 字符串 | 设模型；传空串 `--model ""` **清除**该字段（回落引擎默认模型） |
| `--effort <v>` | 字符串 | 设 claude-code 档位（取值见〈workflow 定义 schema〉能力表，随模型）；传空串清除。仅 `engine=claude-code` 的节点 / 评测官接受 |
| `--reasoning-effort <v>` | 字符串 | 设 qoder 推理档位（取值见〈workflow 定义 schema〉能力表）；传空串清除。仅 `engine=qoder` 的节点 / 评测官接受 |
| `--display-name <s>` | 字符串 | 改节点显示名（纯装饰标签、零引用负担）；须非空。**不受 `--evaluator` 影响**——evaluator 无独立显示名 |
| `--loop-count <n>` | 整数 | 设循环 / 回跳次数，`1`–`20`（节点级，两种循环共用）。仅当该节点带 `evaluator` 或 `redoTarget` 时有效（否则校验拒绝退 `1`） |
| `--redo-target <id>` | 字符串 | **挂载 / 改**该节点的 redoTarget 回跳（目标须是**存在且更前**的节点，见〈落盘校验规则〉；与 evaluator 互斥；首次挂载时若未同时给 `--loop-count`，`loopCount` 默认 `1`）。`<id>` 须非空——**拆除**回跳用 `--no-redo`，不是空串 |
| `--no-evaluator` | 布尔 | **拆除**该节点的评测循环（删 `evaluator`、回落单次、`loopCount` 清除）。须单独给（不与任何其它字段选项或 `--evaluator` 同用）；节点无评测官时报错退 `1` |
| `--no-redo` | 布尔 | **拆除**该节点的 redoTarget 回跳（删 `redoTarget`、回落单次、`loopCount` 清除）。须单独给；节点无回跳时报错退 `1` |

> 给了该引擎不认的字段（如给 `antigravity` 节点设 `--effort`），按〈落盘校验规则〉的能力表**拒绝**，与走 JSON 编辑路径逐字节一致。

**新建 evaluator 时的默认值**：`--evaluator --engine <e>` 在节点原无 evaluator 时**首次挂载**，除引擎（+ 可选 model / effort）外，conduct 自动补一份**默认评测提示词**与 `loopCount`（首次挂载时若未同时给 `--loop-count` 则默认 `1`），避开「评测官必需的 `promptTemplate` 为空」这个鸡生蛋校验死结；与 `conduct ui` 编辑器「设为评测循环」的脚手架逻辑一致。默认提示词是一句**自包含的评测官指令**，存进 `promptTemplate` 的**纯文本字面量**（非模板变量），随后可用 `node set-prompt --evaluator` 改写，例如：

```
你是独立质量评测官。审阅下面待评产物的正确性、完整性与质量，给出具体、可执行的改进反馈。
```

它**刻意不写 `{{<节点id>}}` 自引用**——按 `conduct help prompts`：evaluator 的输入末尾已由系统自动追加被评产物段（故无需自己引用），且内循环首轮的自引用会渲染成空串。这与内置示例 `internal/workflow/testdata/wf_demo.json` 的评测官（「下面 artifact 是一个候选产品名……」）同风格。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>·<id>`；作用于评测官时为 `… 的评测官`；首次挂载评测官 / 回跳时为 `✓ 已为 <name>·<id> 挂载评测循环` / `… 挂载回跳→<目标>`；拆除时为 `✓ 已拆除 <name>·<id> 的评测循环` / `… 的回跳`。退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` / `<id>` 不存在：stderr 报错；退出 `1`。
- `--evaluator` 改字段但该节点无 evaluator、又未在同命令用 `--engine` 新建：stderr 提示「该节点无评测循环；用 `--evaluator --engine <e>` 先挂载」；退出 `1`；不改。
- `--no-evaluator` 用于无评测官的节点、或 `--no-redo` 用于无回跳的节点（拆一个本就不存在的循环）：stderr 提示「该节点无评测循环 / 回跳，无可拆除」；退出 `1`；不改。
- 挂 evaluator 但节点已配 redoTarget、或挂 redoTarget 但节点已有 evaluator：stderr 提示二者互斥、须先拆另一个；退出 `1`；不改。
- 字段值非法（engine 不认该调优字段 / 枚举外 / `--loop-count` 越界或节点无循环 / `--redo-target` 指向不存在·自身·后续节点 / `--display-name` 为空 / `--engine` 改后 `engineConfig` 不兼容）：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- 未给任何字段选项或拆除选项、或 `--no-evaluator` / `--no-redo` 未单独给（与其它字段选项或 `--evaluator` 同用）、或两个拆除旗标同给：stderr 提示用法错误；退出 `2`。
- 落盘副作用：仅校验通过时覆盖入库。

**诚实边界**：

- **本命令覆盖节点的全部结构化字段与显示名，但不碰提示词、不增删节点、不改 `id`**：提示词走 `node set-prompt`；增删节点与改 `id` 走 `edit`（全量）。`id` 有**引用完整性**（被 `redoTarget` / `{{<节点id>}}` 引用），改它须连带改引用、故留给全量 `edit`；`displayName` 是纯装饰、零引用负担，随 `--display-name` 放进本命令（免为改个显示名而复述整份定义）。两种循环的挂 / 拆也都在本命令内（evaluator 走 `--evaluator --engine` 挂、`--no-evaluator` 拆，redoTarget 走 `--redo-target <id>` 挂、`--no-redo` 拆），故 node 层无需再分出 `set-evaluator` / `set-redo` 动词——一个 `node set` + 带类型 flag 即对称覆盖。
- **两种循环互斥、不在一条命令里互换**：节点同时只能有 evaluator 或 redoTarget 之一。要从一种换到另一种，先拆后挂（两条命令）或走 `edit`；一条命令里同时挂两种即拒绝。
- **拆除惯例按「概念」对齐、不按字段类型**：本命令有两套清除语义——**调优标量**（`--model` / `--effort` / `--reasoning-effort`）用**空串**表达「清除、回落引擎默认」；**循环特性**（evaluator / redoTarget）用 **`--no-<循环>`**（`--no-evaluator` / `--no-redo`）表达「拆掉这种循环」。`redoTarget` 虽底层是个标量指针，但它**概念上是一种循环**而非调优字段，故拆除走 `--no-redo` 而非 `--redo-target ""`——与拆 evaluator 对称、且是 CLI 通用的布尔取反惯例（调用方第一直觉即 `--no-X`）。空串仅用于调优标量、`--redo-target ""` 按用法错误拒绝并提示 `--no-redo`，避免同一操作出现两种写法。
- **`--engine` 改引擎的级联**：`engineConfig` 是随 `engine` 判别的载荷，改 engine 后旧的 `model` / `effort` / `reasoningEffort` 未必被新 engine 接受。此时若不同时重设兼容配置，整份定义校验会失败、命令拒绝退 `1`——可在**同一条命令**里一并给（如 `--engine qoder --effort ""` 清掉 claude-code 专属的 effort、或 `--engine qoder --reasoning-effort medium` 换成 qoder 的档位）。绝不静默丢弃不兼容字段。

**示例**：

```bash
conduct workflow node set flow apply  --loop-count 1                 # 只把循环次数改成 1
conduct workflow node set flow gen    --model claude-sonnet-5        # 换模型
conduct workflow node set flow gen    --evaluator --effort high      # 调评测官档位
conduct workflow node set flow test   --engine qoder --effort ""     # 换引擎，同时清掉旧引擎专属的 effort
conduct workflow node set flow propose --evaluator --engine claude-code  # 无则建（补默认提示词），有则改引擎
conduct workflow node set flow propose --no-evaluator                # 拆评测循环
conduct workflow node set flow review --redo-target gen              # 挂回跳
conduct workflow node set flow review --no-redo                      # 拆回跳
```

示意输出：

```
✓ 已更新 flow·apply
```

---

## workflow node set-prompt — 局部编辑（提示词）

**用途**：把某节点（或其 evaluator）的 `promptTemplate` 以**原始文本**从 **stdin** 读入，由 conduct 负责 JSON 编码——作者永远不必手工把含 `{{变量}}`、中文、markdown、多行的提示词转义进 JSON 字符串。与 `node show --prompt`（导出纯文本）构成 round-trip：导出到文件、编辑、写回。落盘前复用整份定义的同一套校验（含模板变量引用校验）。

**用法**：

```
conduct workflow node set-prompt <name> <id> [--evaluator]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 目标节点 id（须存在） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--evaluator` | 布尔 | `false` | 设该节点 **evaluator** 的提示词而非节点主体的（节点无 evaluator 时报错退 `1`） |

**输入**：提示词正文从 **stdin** 原始读入整段（`cat prompt.md | conduct workflow node set-prompt flow gen`）。读入后**剥掉恰好一个尾随换行**（若存在）再存——兼容编辑器 / `echo` 自动补的尾换行，与 `node show --prompt`「补恰好一个尾换行」配对，使 round-trip **字节稳定、不会静默给提示词尾部加 `\n`**（这正是本提案「免误伤」要守的）；诚实代价：无法存一个真的以换行结尾的提示词（提示词几乎不需要）。stdin 是终端（无管道）时报错退出 `2`、**不挂起等待输入**。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>·<id> 提示词`（`--evaluator` 时为 `… 评测官提示词`）；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` / `<id>` 不存在：stderr 报错；退出 `1`。
- `--evaluator` 但该节点无 evaluator：stderr 提示；退出 `1`；不改。
- 空输入（stdin 读到空、或剥掉尾换行后为空）/ 提示词内模板变量引用非法（`{{<nodeId>}}` 指向不存在节点、未知 `{{sys.*}}`）：stderr 字段级错误（`promptTemplate` 不可为空 / 引用错误）；**原文件不变**；退出 `1`（校验类）。
- stdin 是终端（无管道输入）：stderr 报缺少输入；退出 `2`。
- 落盘副作用：仅校验通过时覆盖入库。

**示例**：

```bash
# round-trip：导出 → 编辑 → 写回
conduct workflow node show flow gen --prompt > gen.md
$EDITOR gen.md
conduct workflow node set-prompt flow gen < gen.md

# 直接管道灌入
cat review-prompt.md | conduct workflow node set-prompt flow review --evaluator
```

示意输出：

```
✓ 已更新 flow·gen 提示词
```

---

## workflow node show — 查询 / 导出（单节点）

**用途**：查看单个节点的定义详情，或把它的**提示词单独取出为纯文本**（便于落文件编辑再 `node set-prompt` 写回）。与 `node set-prompt` 构成 round-trip。

**用法**：

```
conduct workflow node show <name> <id> [--evaluator] [--prompt | --json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 节点 id（须存在） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--evaluator` | 布尔 | `false` | 作用于该节点的 evaluator 而非节点主体（节点无 evaluator 时报错退 `1`） |
| `--prompt` | 布尔 | `false` | 只输出该节点（或其 evaluator）的 `promptTemplate` **纯文本原文**（不含 JSON 引号 / 转义），供重定向到文件 |
| `--json` | 布尔 | `false` | 输出该节点（或其 evaluator）的**规范化定义 JSON**（单个对象） |

> `--prompt` 与 `--json` 互斥（`--prompt` 已是纯文本、`--json` 是结构化对象），同时给报用法错误退 `2`。

**输出**：

- 默认（无 `--prompt` / `--json`）：人类可读单节点详情——`id · displayName · engine · model · <循环模式>`，随后提示词**全文**（单节点场景不截断——就一个节点、不怕长；截断预览留给 `workflow show`）；退出 `0`。
- `--prompt`：stdout 打印 `promptTemplate` 全文并补**恰好一个**尾随换行（便于 `> file` 与终端显示），不加其它任何修饰；退出 `0`。与 `node set-prompt`「剥掉恰好一个尾随换行」配对，round-trip 字节稳定（见〈workflow node set-prompt〉的〈输入〉）。
- `--json`：stdout 打印规范化后的单 node 对象 JSON（camelCase、补齐默认值）；退出 `0`。
- `<name>` / `<id>` 不存在：stderr 报错；退出 `1`。
- `--evaluator` 但该节点无 evaluator：stderr 提示；退出 `1`。
- `--prompt` 与 `--json` 同时给：stderr 用法错误；退出 `2`。

**示例**：

```bash
conduct workflow node show flow gen                    # 人类可读单节点详情
conduct workflow node show flow gen --prompt > gen.md  # 导出提示词纯文本，编辑后 node set-prompt 写回
conduct workflow node show flow gen --evaluator --json # 看该节点评测官的规范化 JSON
```

---

## workflow rename — 改名

**用途**：给一个既有工作流改名。改的是**标识（主键）**，不动定义（`nodes`）——与 `edit`（换定义、不换名）正交。新名须合法且未被占用。**已有运行记录不随之改名**：run id 与 run.json 里的 `workflow` 保留旧名，这是快照语义下的诚实历史（见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉）；往后的新运行才落到新名下。改名**不阻拦、也不等待**在跑的运行——进行中的 run 在旧名下自然收尾，与 `delete` 不拦在跑运行一致。

**用法**：

```
conduct workflow rename <old> <new>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<old>` | 是 | 现有工作流名称 |
| `<new>` | 是 | 新名称，须匹配 `[A-Za-z0-9._-]+` 且未被占用 |

**输出**：

- 成功：stdout `✓ 已重命名 <old> → <new>`；退出 `0`；落盘把 `<old>.json` 改名为 `<new>.json`、内部 `name` 改为 `<new>`、重戳 `updatedAt`。
- `<old>` 不存在：stderr `工作流 <old> 不存在`；退出 `1`；不改。
- `<new>` 已存在：stderr `工作流 <new> 已存在（先 delete 或换名）`；退出 `1`；不改（`rename` 不覆盖，语义同 `create`）。
- `<new>` 名称非法（不匹配 `[A-Za-z0-9._-]+`）：stderr 报错；退出 `2`（非法参数）。
- 落盘副作用：仅成功时改 `workflows/` 下那一个文件；`runs/` 分毫不动。

> **改名与在跑运行重叠是安全的**：run 在**开跑那一刻已冻结 `workflowSnapshot`**，此后既不读、也不依赖 store 里那份活 workflow，改名（乃至 `delete`）都动不了这个在跑的 run——它照旧在旧名下收尾。唯一过渡态：从改名到该 run 结束的窗口内，「哪些 workflow 在跑」会把它算在**旧名**下（新名此刻尚无在跑 run），窗口结束即消失，属预期、非 bug。

**示例**：

```bash
conduct workflow rename my-flow autopilot
```

示意输出：

```
✓ 已重命名 my-flow → autopilot
```

---

## workflow delete — 删除

**用途**：从 store 删除一个或多个工作流。默认在交互终端下二次确认；非交互环境必须显式 `--yes`，避免脚本误删。

**用法**：

```
conduct workflow delete <name>... [--yes] [--json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>...` | 是 | 一个或多个工作流名称 |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `-y, --yes` | 布尔 | `false` | 跳过确认直接删除 |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（`{"deleted":[...]}`） |

**输出**：

- 成功：逐个 stdout `✓ 已删除 <name>`；退出 `0`。`--json` 输出 `{"deleted":[...]}`。
- 某名称不存在：stderr `工作流 <name> 不存在`；退出 `1`（存在的仍会删除，最终以是否有失败项决定退出码）。
- 非交互（非 TTY）且未加 `--yes`：stderr `拒绝在非交互环境删除，请加 --yes`；退出 `2`（用法错误）；不删除。
- 落盘副作用：删除对应 `<name>.json`（`runs/` 分毫不动——已有运行记录靠快照自解释）。

**示例**：

```bash
conduct workflow delete my-flow
conduct workflow delete a b c --yes
```

示意输出：

```
✓ 已删除 a
✓ 已删除 b
✓ 已删除 c
```

---

## workflow list — 查询（列表）

**用途**：列出 store 内全部工作流。

**用法**：

```
conduct workflow list [--json]
```

**参数**：无。

**输出**：

- 人类可读（默认）：表格，列为 `NAME | NODES | UPDATED`——名称、节点 id 流（按定义顺序以 `›` 连接；超过 6 个截断为前 6 个 + `+N`）、最近修改时间；退出 `0`。
- `--json`：数组，每项 `{"name","nodes":["<id>",...],"updatedAt":"<RFC3339>"}`（`nodes` 为节点 id 数组，不含存储路径——按名字寻址）。
- store 为空：stdout 提示 `（store 为空）`；退出 `0`（空不是错误）。

> 设计说明：列由早期的 `NODES`（数量）`| ENGINES`（引擎集合）调整为节点 id 流并移除引擎列——节点名比引擎集合信息量更大，列表页保持克制；`conduct ui` 工作流列表与此同列。

**示例**：

```bash
conduct workflow list
conduct workflow list --json | jq '.[].name'
```

示意输出：

```
NAME       NODES                  UPDATED
autopilot  plan›code›test›review  2026-07-03 10:22
my-flow    main                   2026-07-03 15:40
```

---

## workflow show — 查询（详情）

**用途**：查看单个工作流的定义详情；可选附带**展开预览**（复用运行时的展开算法，零成本核对节点图会被解释成怎样的线性步骤序列）。整份工作流用本命令，单个节点用 `workflow node show`。

**用法**：

```
conduct workflow show <name> [--expand] [--json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--expand` | 布尔 | `false` | 追加打印展开后的执行步骤（`[i] type node=<id> iter=<n>`） |

**输出**：

- 人类可读（默认）：打印名称 / 节点数，随后逐节点一行 `id · displayName · engine · model · <循环模式>`（循环模式＝`evaluator 内循环` / `redoTarget→<目标> 回跳` / `单次`）；`--expand` 时附展开步骤清单；退出 `0`。
- `--json`：输出**规范化后**的定义 JSON（camelCase、补齐默认值）；`--expand` 时额外含 `"expanded":[{"stepIndex","type","nodeId","iteration"}]`。
- 不存在 / 校验不过：stderr 报错；退出 `1`。

**示例**：

```bash
conduct workflow show autopilot
conduct workflow show autopilot --expand
conduct workflow show demo --json
```

示意输出（默认）：

```
autopilot · 4 节点
plan   · 规划 · claude-code · claude-opus-4-8 · 单次
code   · 编码 · claude-code · claude-opus-4-8 · evaluator 内循环
test   · 测试 · antigravity · (引擎默认) · 单次
review · 评审 · claude-code · claude-opus-4-8 · redoTarget→code 回跳
```

`--expand` 追加的展开清单，格式同 [cli-runtime.md](./cli-runtime.md)〈workflow run〉输出里的展开段（`[i] type node=<id> iter=<n>`）。

---

## workflow 定义 schema（自包含参考）

落盘的一份 workflow 是完整记录 `{ name, createdAt, updatedAt, nodes }`——`name` / `createdAt` / `updatedAt` 是系统管理的元数据，`nodes` 是用户编写的定义主体。字段规格如下（`//` 为注释，实际 JSON 不含）：

```jsonc
{
  "name": "autopilot",            // 主键：工作流名（= 文件名 <name>.json，来自 create 参数；仅 rename 可改，edit / 导入体不改它）
  "createdAt": "2026-07-03T09:12:00+08:00",  // 创建时间（RFC3339；create / copy 时写，此后不变）
  "updatedAt": "2026-07-03T15:40:00+08:00",  // 最近修改时间（RFC3339；create / copy / edit / node set / node set-prompt / rename 时重戳，导入值忽略）
  "nodes": [{
    "id": "node-1",                 // 必填，唯一，命名受限（见〈落盘校验规则〉）；模板中以 {{node-1}} 引用其产物
    "displayName": "规划",           // 必填，进度展示用
    "engine": "claude-code",        // 必填（判别式）：claude-code | antigravity | qoder（codex 暂时下线，见〈实现状态〉）
    "engineConfig": {               // 选填；shape 由 engine 决定，合法字段见下「引擎载荷」
      "model": "claude-opus-4-8",   // 选填；取值受 engine 约束，省略则用引擎默认模型
      "effort": "high"              // claude-code 档位（antigravity 无、编码在 model 标签；qoder 用 reasoningEffort）
    },
    "promptTemplate": "…{{sys.userPrompt}}…",  // 必填，见下「模板变量」
    "evaluator": {                  // 选填：带评测官 → in-place 内循环（写→评→改）；同构 engine + engineConfig
      "engine": "claude-code",
      "engineConfig": { "model": null, "effort": "medium" },
      "promptTemplate": "审阅下面待评产物，指出改进点"  // 自包含：不写 {{被评节点id}}，产物由系统追加在末尾（见「模板变量」注）
    },
    "redoTarget": "<前序节点 id>",   // 选填：与 evaluator 互斥；指向更早的节点 → jump-back 段整体重跑
    "loopCount": 1                  // 选填：内循环 / 回跳次数，默认 1，取值 1–20（仅在有 evaluator 或 redoTarget 时生效）
  }]
}
```

> 上例是**字段目录**（把可选字段集中展示）：实际同一 node 里 `evaluator` 与 `redoTarget` 二选一（见〈落盘校验规则〉）。

**engine + engineConfig（判别联合）**：`engine` 是判别式（tag），`engineConfig` 是引擎专属载荷，其合法字段由 `engine` 决定——这把「engine / model / effort 三者绑定」编进结构本身，三者不能各自独立填。支持的引擎（`claude-code` / `antigravity` / `qoder`；`codex` 暂时下线）、各引擎接受的 `engineConfig` 字段及枚举、schema 字段到 CLI 参数的映射，见 [engines.md](./engines.md)〈引擎能力表〉——那是引擎层的单一权威来源。

`model` 省略则用该引擎默认模型；`evaluator` 用**同一套** `engine` + `engineConfig` 结构（含 `engineConfig`——`node set --evaluator` / `node set-prompt --evaluator` 即作用于此）。具体校验（严格、依能力表）见〈落盘校验规则〉。

> **`--help` 与本节须一致**：`create` / `edit` 的 `--help` 里内嵌的定义结构说明由能力表动态生成，须与本节 schema 对齐——**尤其 `evaluator` 同构支持 `engineConfig`**，不可只写成 `{engine, promptTemplate}`（否则调用方仅凭 `--help` 会误以为评测官不能单独配模型 / 档位）。当前差距见〈实现状态〉。

**模板变量**（`promptTemplate` 内）：

| 写法 | 展开为 |
| --- | --- |
| `{{sys.userPrompt}}` | 运行时传入的用户需求 |
| `{{sys.cwd}}` | 引擎工作目录（`--cwd`） |
| `{{<nodeId>}}` | 该节点最近一次成功产物（未跑则空串） |
| `\{{x}}` | 转义，输出字面量 `{{x}}` |

> **fail-loud**：引用不存在的 nodeId 或未知 `{{sys.*}}` 会在入库校验时即被拒绝（见〈落盘校验规则〉）；即便绕过校验，运行时也**保留字面量而非静默置空**作为兜底，让作者一眼看出写错。

**两种循环模式（互斥）**：

- `evaluator`（in-place 内循环）：同一节点「写 → 评测 → 带反馈改写」，重复 `loopCount` 轮。
- `redoTarget`（jump-back 段循环）：本节点跑完后跳回 `redoTarget`，把二者之间的整段重跑，重复 `loopCount` 轮。

**展开基准值**（作为展开算法的回归锚点）：`internal/workflow/testdata/wf_autopilot.json` → **14 步**；`internal/workflow/testdata/wf_demo.json` → **4 步**（导入 store 后 `show --expand` 应复现同样步数）。

## workflows/ 落盘结构

工作流定义落在固定的全局 store `~/.conduct/workflows/` 下——无数据库、纯文件，便于人肉查看、备份与版本管理；首次使用时自动创建缺失目录。

```
~/.conduct/
└── workflows/                     # 工作流定义（create/copy/edit/node …/rename/delete/list/show 操作此处）
    ├── autopilot.json
    └── my-flow.json
```

**`workflows/<name>.json`** —— 一份完整 workflow 记录 `{ name, createdAt, updatedAt, nodes }`。字段结构见〈workflow 定义 schema〉；落盘为**规范化形态**（camelCase、补齐默认值），故 `show --json` 打印的即此文件内容。文件名 `<name>` 与内部 `name` 一致（`[A-Za-z0-9._-]+`，作主键）。

> 运行记录 `~/.conduct/runs/` 的布局见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉。

## 落盘校验规则

无独立 `validate` 命令：下列校验由所有变更命令（`create` / `copy` / `edit` / `node set` / `node set-prompt`）在**入库前强制执行**，不过即拒绝、不写盘（`show` / 运行时载入时也复用同一套）。承自并强化 Python 原型的 fail-loud 语义：

- **结构与类型**：`nodes` 数组存在；每个 node 的 `id` / `displayName` / `engine` / `promptTemplate` 必填；`engineConfig` 结构合法；`loopCount` 为 `1`–`20` 的整数（仅当 node 带 `evaluator` / `redoTarget` 时校验）。
- **元数据字段系统管理**：`name` 若在导入定义中出现，须等于目标名（`create` 的 `<name>` 参数 / `edit` 的目标），否则拒绝；`createdAt`（`create` / `copy` 时写、此后不可变）与 `updatedAt`（每次变更重戳）由系统写入，导入值一律忽略。故导入体（`create --definition` / `edit` 的 stdin）给 `nodes` 即可。改名是独立操作、走 `workflow rename`——不能靠导入体里的 `name` 与目标名不一致来触发（那一律按错误拒绝，绝不做静默改名）。
- **engine + engineConfig 严格校验（判别联合）**：先按 `engine` 选定该引擎的**能力表**（该引擎接受的调优字段及其枚举，见 [engines.md](./engines.md)〈引擎能力表〉），再校验 `engineConfig`：`engine` 合法（已注册）、调优字段属于该 `engine`（`effort` ↔ claude-code；`reasoningEffort` ↔ qoder；`antigravity` 无独立调优字段、仅认 `model`）、其值在该字段允许集内、无该引擎不认的多余字段；任一不符即拒绝（node 与其 `evaluator` 各自独立校验）。`model` 当前**不做白名单**（接受任意非空串，待有权威模型表再收紧）；能力表随引擎演进维护。`codex` 暂时下线（下周恢复）；`gemini` 被 `antigravity` 取代（见〈实现状态〉）。
- **`evaluator` 与 `redoTarget` 互斥**：同一 node 二者不可并存。
- **node `id` 合法且唯一**：`id` 须匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`——首字符为字母或下划线，其余限字母 / 数字 / 连字符 / 下划线，总长 1–64；且同一份定义内不得重复。`redoTarget` 作为对 node 的引用同样须是合法 id。（注意：这套 id 规则与工作流名 `<name>` 的 `[A-Za-z0-9._-]+` 不同——后者是 store 文件名，可含点、可数字开头。）
- **`redoTarget` 合法回跳**：必须指向一个**存在且位于本 node 之前**的节点；指向不存在的、自身、或后续节点即拒绝。
- **模板变量引用存在**：`promptTemplate`（含 `evaluator` 的）里非转义的 `{{<nodeId>}}` 必须引用定义内存在的 node id；`{{sys.*}}` 仅限已知系统变量（`sys.userPrompt` / `sys.cwd`）。

校验失败时逐条打印字段级错误（如 `nodes[0].engineConfig.effort: engine="antigravity" 不认 effort`），退出码 `1`。校验内核提供 `ValidateStructured` 返回字段级 `[]Problem`，供 `node set` 定位改的那个字段、也供 UI 定位错误字段。

> 注：这三条（`id` 唯一、`redoTarget` 合法回跳、模板引用存在）比 Python 原型更严——原型对它们分别是静默降级 / 后者覆盖 / 渲染时保留字面量；conduct 提前到入库时 fail-loud 拒绝。

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：校验失败、IO 失败、目标不存在、命名冲突（已存在）等 |
| `2` | 用法错误（缺参、非法参数、非交互拒绝危险操作、stdin 是终端却需管道输入）——Cobra 默认 |

校验失败并入 `1`，不单列专用码——具体原因看 stderr 的字段级报错。（运行时命令的退出码见 [cli-runtime.md](./cli-runtime.md)〈退出码约定〉，同一张表。）

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `workflow create / edit / rename / delete / list / show` | **已实现**（`workflow` 命令族 + `internal/store` 托管层 + `internal/workflow` 校验/展开/渲染；`show --expand` 复用展开算法。校验内核提供 `ValidateStructured` 返回字段级 `[]Problem`） |
| `workflow copy` | **已实现**（`internal/cli/workflow_copy.go`；语义同 `create`，深拷 `nodes`、时间戳由 `store.Create` 重戳，落盘前复用整份校验） |
| `workflow node set` | **已实现**（`internal/cli/workflow_node_set.go`；带类型 flag 改节点 / 评测官引擎·模型·档位、节点显示名、循环次数，并挂 / 拆 evaluator 自循环（`--evaluator --engine` / `--no-evaluator`）与 redoTarget 回跳（`--redo-target` / `--no-redo`），故 node 层无需 `set-evaluator` / `set-redo` 独立动词；空串清除标量、`--loop-count` 无循环命令级退 1、engine 级联走整份校验，改完复用整份校验再落盘） |
| `workflow node set-prompt` | **已实现**（`internal/cli/workflow_node_set_prompt.go`；提示词原始文本经 stdin 入、conduct 负责 JSON 编码，剥恰好一个尾换行与 `node show --prompt` 配对） |
| `workflow node show` | **已实现**（`internal/cli/workflow_node_show.go`；导出单节点定义 / 提示词纯文本，`--prompt` 补恰好一个尾换行，与 `node set-prompt` round-trip 字节稳定） |
| `create` / `edit` 的 `--help` 定义结构说明 | **已实现**（`internal/cli/workflow.go` 的 `workflowDefinitionHelp()` 的 `evaluator` 示例已补 `engineConfig`、去自引用反模式，与〈workflow 定义 schema〉对齐） |
| 引擎 `claude-code` / `antigravity` / `qoder` | **已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，均经真实调用冒烟通过；单测用假二进制覆盖参数/stdin/cwd 接线与 JSON 解析） |
| 引擎 `codex` | **暂时下线**（账户欠费，下周恢复）；届时加回注册表（`internal/engine/codex.go`）与能力表 |
| 引擎 `gemini` | **已移除**：被 `antigravity` 取代（agy 取代 gemini cli） |

## 待确认

以下是本次新增命令里已按下述方案拟定、但实现前可复议的开放项（诚实标注，非既成事实）：

- **`node set` 清除标量字段的写法**：拟用**空串**表达「清除、回落引擎默认」（`--model ""` / `--effort ""`），不引入专用 `--clear-*` 旗标。理由：省一组旗标、脚本友好；代价是无法区分「设为空串」与「清除」——但 `model` / `effort` 空串本就非法值，语义无歧义。若日后有字段合法值可为空串，再引专用清除旗标。
- **提示词来源仅 stdin，不加 `--file`**：`node set-prompt` 只认 stdin（与 `edit` / `create --definition` 一致），文件场景用 shell 重定向 `< prompt.md` 覆盖。理由：不新增文件路径入参、命令面最小；`--file` 便利旗标按需再加。
