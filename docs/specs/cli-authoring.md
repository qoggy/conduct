# conduct CLI 编辑态命令规格

> 本文规定 conduct CLI 的**工作流编辑态命令面**——如何脚手架、导入、增删节点、连边、局部编辑、复制、改名、删除、查询一份工作流**定义**，以及定义的数据模型（节点 + 边的 DAG）与入库校验规则。这些命令**不运行工作流、不烧 token**，纯粹读写 store 里的定义。
>
> **运行**一份工作流、查看运行记录，见 [docs/specs/cli-runtime.md](./cli-runtime.md)；可视化界面、工具自更新、版本 / 帮助等工具层命令，见 [docs/specs/cli-tooling.md](./cli-tooling.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

下面几条是整份规格的地基。它们决定了每条命令的形态，若你不认同，先改这里、其余随之调整：

- **工作流是「节点 + 边的有向无环图（DAG）」。** 节点表达一次 AI 引擎执行，边表达执行依赖（`from` 跑完才轮到 `to`）。图有且仅有一个虚拟入口 `START`、一个虚拟出口 `END`（保留标记节点，不执行、不产物）；`START` 可同时扇出到多个节点，让工作流**一开始就能并行**。无循环——图强制无环。数据流靠节点提示词里的 `{{<nodeId>}}` 引用上游祖先节点的产物。
- **工作流是「有名字的托管对象」，不是散落文件。** 存储在一个 *store* 里，`create / copy / edit / rename / delete / show / node … / edge …` 一律按**名字**定位工作流（`list` 作用于整个 store，无需名字）；不接受直接传文件路径——要跑手头的一份 JSON，先 `create --definition` 入库拿到名字，再按名字运行（见 [cli-runtime.md](./cli-runtime.md)〈workflow run〉）。
  - 理由：只有工作流是托管对象时，「删除 / 查询 / 局部编辑」才值得做成子命令；若只是文件，它们就退化成 `rm` / `ls` / 手改 JSON。
- **store 位置（固定）**：工作流统一存放在全局 `~/.conduct/workflows/`（每份一个 `<name>.json`）。所有命令固定读写此 store，**不支持自定义存储位置**（完整落盘布局见〈workflows/ 落盘结构〉；运行记录 `~/.conduct/runs/` 的布局见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉）。
- **命令风格 noun-first（统一）**：资源操作命令形如 `conduct <noun> <verb>`（对标 `gh` / `kubectl`），无顶层动词命令。工作流是 `workflow` 名词族；其下的**节点**是子资源，走 `conduct workflow node <verb>`（如 `conduct workflow node set`），**边**同样是子资源，走 `conduct workflow edge <verb>`。运行记录 `run` 名词族见 [cli-runtime.md](./cli-runtime.md)。**例外**是不针对单一资源的顶层工具命令 `conduct version` / `conduct ui` / `conduct update` / `conduct help`（见 [cli-tooling.md](./cli-tooling.md)）。
- **对 AI-bash 与人类双友好（北极星）**：每条能力都以非交互、可脚本化的 CLI 命令提供（查询类带 `--json` 机读，变更类以退出码表达成败）；同时以 `conduct ui` 提供可视化界面（人类层，见 [cli-tooling.md](./cli-tooling.md)〈ui〉）。**关键不变量：UI 无独占能力**——它做的每件编辑操作都有对应的、可单独完成的 CLI 命令（整体替换 ↔ `workflow edit`、增删节点 ↔ `workflow node add` / `workflow node rm`、改节点字段 ↔ `workflow node set`、改连边 ↔ `workflow edge`、改单份提示词 ↔ `workflow node set-prompt`），UI 只把它们聚合成人看的视图，绝不新增「只有界面能做」的编辑功能。
- **磁盘上的工作流永远完整可运行**——不存在草稿态。校验单层：每条变更命令在落盘前必须通过整份校验（规则见〈落盘校验规则〉），否则拒绝、原样保留。故每条编辑命令都是**合法图 → 合法图**的一步。
- **名称即文件名**：工作流以 `<name>.json` 落盘。`<name>` 限定 `[A-Za-z0-9._-]+`，不含路径分隔符。（注意：这套工作流名规则与 node `id` 的 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$` 不同，见〈落盘校验规则〉。）

## 数据模型（工作流实体）

conduct 有两个模型，像数据库的两张表；本文只详述编辑态相关的 **workflow** 表，**run** 表见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉。

| 实体 | 是什么 | 主键 | 可变性 | 落盘 |
| --- | --- | --- | --- | --- |
| **workflow**（工作流） | 一份 DAG 定义 | `name` | 可变（`edit` / `node add` / `node rm` / `node set` / `node set-prompt` / `edge` 改定义、`rename` 改名、`copy` 派生新对象） | `workflows/<name>.json`（完整记录 `{ name, createdAt, updatedAt, definition:{ nodes, edges } }`） |
| **run**（运行记录） | 一次执行 | run id | 身份与冻结快照不变；状态、产物、trace 随执行 / 恢复更新 | `runs/<id>/`（详见 [cli-runtime.md](./cli-runtime.md)） |

- **run 靠快照自解释，与编辑态解耦**：run 在开跑那一刻冻结一份完整 workflow 快照，此后 `edit` / `node add` / `node rm` / `node set` / `edge` / `delete` 一个 workflow **都不影响它已有的 run**（历史照样可看、可复现）。故本文所有变更命令只动 `workflows/<name>.json`，`runs/` 分毫不动。快照语义详见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉。

## 命令总览

| 命令 | 作用 | 对应诉求 |
| --- | --- | --- |
| `conduct workflow create <name>` | 脚手架 / 导入一份新工作流入库 | 创建 |
| `conduct workflow copy <src> <dst>` | 从既有工作流复制出一份新名字的变体 | 复制 |
| `conduct workflow edit <name>` | 从 stdin 整体替换既有工作流（保存即校验） | 编辑（全量） |
| `conduct workflow node add <name> <id>` | 建一个节点并连边（缺省自动接 `START→<id>→END`） | 编辑（加节点） |
| `conduct workflow node rm <name> <id>` | 删一个节点及其所有连边（结果校验） | 编辑（删节点） |
| `conduct workflow node set <name> <id>` | 改某节点的结构化字段（引擎 / 模型 / 档位 / 显示名） | 编辑（局部·结构化） |
| `conduct workflow node set-prompt <name> <id>` | 以原始文本设置某节点的提示词 | 编辑（局部·提示词） |
| `conduct workflow node show <name> <id>` | 导出单节点定义 / 单份提示词纯文本 | 查询（节点） |
| `conduct workflow edge <name>` | 列出边；带 `--add` / `--rm` 则原子批量改边（一次算清、末尾整份校验） | 连边（查询 / 编辑） |
| `conduct workflow rename <old> <new>` | 给既有工作流改名（改标识，不动定义） | 改名 |
| `conduct workflow delete <name>...` | 从 store 删除一个 / 多个工作流 | 删除 |
| `conduct workflow list` | 列出 store 内全部工作流 | 查询 |
| `conduct workflow show <name>` | 查看单个工作流（可附拓扑分层预览） | 查询 |

> 没有独立的 `validate` 命令：定义校验已内化进所有变更命令（`create` / `copy` / `edit` / `node add` / `node rm` / `node set` / `node set-prompt` / `edge`）的落盘环节——不合规即拒绝保存、原文件不变，规则见〈落盘校验规则〉。需要不运行、不花 token 地核对一份定义合不合法、并预览它的拓扑分层（同层可并行）时，用 `show --expand`（载入即校验，再打印分层）。

> **粒度编辑 vs 全量替换**：`edit` 从 stdin 读**整份**新定义替换，适合可视化编辑（`conduct ui`）或整体重写；`node add` / `node rm` / `node set` / `node set-prompt` / `edge` 只改**一处**（加/删一个节点、改一个节点的字段/提示词、改一批边），输入里只出现那一处改动——避免为微调一个字段、连一条边而复述整份含长提示词的 JSON（省 token、免误伤未改的提示词）。所有路径殊途同归，都在落盘前复用同一套整份校验。

> 文档分层：各命令的 `--help` 只做精简速查（本命令怎么用，遵循社区标准）；教程 / 概念 / 最佳实践这类**跨命令的长文档**不塞进 `--help`，改由 `conduct help <主题>` 输出（对标 `go help <topic>`）。主题按概念组织（如 `prompts` 讲 promptTemplate 怎么写好、并行分支如何避免写盘冲突），文档 `go:embed` 进二进制随发布走，相关命令的 `--help` 末尾留一行指针。

## 全局约定

**全局选项**（`-h, --help` 所有命令通用）：打印该命令的用法与选项后退出 `0`。（`--version` 仅根命令，见 [cli-tooling.md](./cli-tooling.md)〈version〉。）

**通用选项**（涉及结构化输出的命令支持）：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（人类可读表格 / 装饰全部关闭），便于脚本消费 |

**「出参」的含义**：本文每条命令的「输出」小节同时约定 **stdout**（正常结果）、**stderr**（错误信息）、**退出码**、以及**落盘副作用**（若有）。统一退出码见文末〈退出码约定〉。

**fail-loud 基线**：错误一律显式报出并以非 0 退出，绝不静默吞掉、绝不用空动作冒充成功（承自项目编码规范「错误不吞 / 不假装成功」）。

**保留节点 START / END**：每份定义的 `nodes[]` 恒含且仅含一个 `id == "START"`、一个 `id == "END"` 的**标记节点**——无 engine / prompt、不执行、不产 trace，`START` 是唯一源（无入边）、`END` 是唯一汇（无出边）。它们由 `create` 脚手架带出、`create --definition` / `edit` 的导入体自带；**用户 agent 节点的 id 不得为 `START` / `END`**（保留），`node rm START` / `node rm END` 一律拒绝。下文命令凡涉及 `<id>` 处，除特别说明外均指 agent 节点。

---

## workflow create — 创建

**用途**：新建一份工作流并入库。默认脚手架出一份最小骨架（`START → node-1 → END`，一个 agent 节点 `node-1`：引擎 `codex`、提示词 `{{sys.userPrompt}}`）；带 `--definition` 时改为从 **stdin** 读入**定义主体** `{nodes, edges}`（含 `START`/`END`）导入——`name` 由参数 `<name>` 定、`createdAt` / `updatedAt` 由 store 管理，导入体若额外带这三个元数据键（如 `show --json` 全记录）一律忽略。入库前一律校验（规则见〈落盘校验规则〉），校验不过不落盘。

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
- `<name>` 名称非法（不匹配 `[A-Za-z0-9._-]+`）：stderr 报错；退出 `2`（非法参数，与 `copy` / `rename` 一致）。
- `--definition` 内容非合法 JSON / 校验不过：stderr 打印字段级校验错误；退出 `1`（校验类，见〈退出码约定〉）；不写盘。
- `--definition` 导入体只需 `definition` 的值 `{nodes, edges}`；若直接给整条记录（`show --json` 输出，含 `name` / 时间戳外壳与 `definition` 包裹层），则解包 `definition`、**忽略**元数据——`name` 由参数 `<name>` 定、时间戳 `create` 时新戳当前时刻，**不因不一致报错**。
- 落盘副作用：成功时把该工作流写入 store。

**示例**：

```bash
conduct workflow create my-flow                            # 脚手架一份最小骨架（START→node-1→END）
cat wf.json | conduct workflow create ported --definition  # 从 stdin 导入一份定义
```

示意输出：

```
✓ 已创建 my-flow
```

---

## workflow copy — 复制（造变体）

**用途**：从一个既有工作流复制出一份新名字的工作流——一步造变体，替掉 `show --json > f`、改 `f`、`create --definition < f` 的多步外部拼装。复制的是**定义主体**（`nodes` + `edges`，含 `START`/`END`）；`<dst>` 是全新的托管对象，`createdAt` / `updatedAt` 重新戳当前时刻，不继承 `<src>` 的时间戳。语义同 `create`：`<dst>` 已存在则拒绝、**不覆盖**。

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

- 成功：stdout `✓ 已复制 <src> → <dst>`；退出 `0`；落盘写 `<dst>.json`（`nodes` / `edges` = `<src>` 的、`name` = `<dst>`、`createdAt` / `updatedAt` = 当前时刻）。
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

**用途**：编辑既有工作流——从 **stdin** 读入**定义主体** `{nodes, edges}`（含 `START`/`END`）整体替换（`name` / `createdAt` / `updatedAt` 等元数据不在主体内、由系统管理，导入体若带则忽略）。适合可视化编辑器导出后写回、或大批量重构；只改个别节点 / 边 / 提示词请优先用 `node add` / `node rm` / `node set` / `node set-prompt` / `edge`（省 token、免误伤）。**保存即校验（规则见〈落盘校验规则〉），校验不过则拒绝写入、保留原定义不变**（不写坏数据）。可视化编辑请用 `conduct ui`（见 [cli-tooling.md](./cli-tooling.md)〈ui〉），不在本命令内；**改名**用 `workflow rename`——`edit` 只换定义、不换名。

**用法**：

```
conduct workflow edit <name>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 待编辑的工作流名称 |

**输入**：定义主体（`{nodes, edges}`）从 **stdin** 读入（`cat new.json | conduct workflow edit <name>`）。stdin 是终端（无管道）时报错退出 `2`、**不挂起等待输入**；需要可视化编辑用 `conduct ui`。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>`；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。旧文件可解析出 `createdAt` 时保留；若旧 JSON 已损坏到元数据也无法读取，则以本次修复时刻重建 `createdAt`。
- 校验不过 / stdin 非合法 JSON：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- `<name>` 不存在：stderr `工作流 <name> 不存在`；退出 `1`。
- 导入体只需 `definition` 的值 `{nodes, edges}`；若直接给整条记录（`show --json` 输出，含 `name` / 时间戳外壳与 `definition` 包裹层），则解包 `definition`、**忽略输入中的元数据**——`name` 由参数 `<name>` 定（改名走 `rename`），时间戳按上一条的 store 恢复规则处理，**不因输入元数据不一致报错**。
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

## workflow node add — 加节点（原子建节点 + 连边）

**用途**：建一个 agent 节点并连边，一步落地。**不给 `--from` / `--to` 时自动接成 `START → <id> → END`**——裸节点即合法、且默认就是「一开始就并行」的一员（连加几个都不给 from/to，就得到多个从 `START` 扇出的并行节点）。`--from` / `--to` **各自**给出则该侧按指定连、缺省的一侧仍自动接 `START` / `END`（如只给 `--from a`，出边仍自动接 `END`）。在内存中建好节点与边后**复用整份定义的同一套校验**再落盘：结果不合法（成环、孤立、id 冲突等）即整条拒绝、原文件不变。

**用法**：

```
conduct workflow node add <name> <id> --engine <e> --display-name <dn> \
    [--from <id,...>] [--to <id,...>] [--prompt <text>] \
    [--model <m>] [--effort <v>] [--reasoning-effort <v>]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 新节点 id，须匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`、未被占用、且**不得为 `START` / `END`**（保留） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--engine <e>` | 字符串 | 必填 | 引擎，取值见 [engines.md](./engines.md)〈引擎能力表〉（`claude-code` / `antigravity` / `qoder` / `codex`） |
| `--display-name <dn>` | 字符串 | 必填 | 节点显示名（进度展示用），须非空 |
| `--from <id,...>` | 字符串 | `START` | 入边来源，逗号分隔的一个或多个已存在节点 id（可含 `START`）；给了就不再自动接 `START` |
| `--to <id,...>` | 字符串 | `END` | 出边去向，逗号分隔的一个或多个已存在节点 id（可含 `END`）；给了就不再自动接 `END` |
| `--prompt <text>` | 字符串 | `{{sys.userPrompt}}` | 提示词（简单场景内联）；复杂 / 多行 / 含大量 `{{变量}}` 的提示词改用 `node set-prompt` 读 stdin（免转义） |
| `--model <m>` | 字符串 | 引擎默认 | 模型，取值受 engine 约束（见能力表） |
| `--effort <v>` | 字符串 | — | claude-code 档位（仅 `engine=claude-code` 接受） |
| `--reasoning-effort <v>` | 字符串 | — | qoder / codex 推理档位（仅 `engine=qoder` / `codex` 接受） |

**输出**：

- 校验通过：stdout `✓ 已加节点 <name>·<id>`；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` 不存在：stderr 报错；退出 `1`。
- `<id>` 已存在 / 为保留名 `START`·`END` / 不匹配 id 规则：stderr 报错；退出 `1`（保留名 / 已存在）或 `2`（id 非法）。
- `--from` / `--to` 指向不存在的节点：stderr 报错；退出 `1`；不改。
- 结果不合法（成环、`START→END` 直连、孤立节点、引用非祖先等）：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- 缺 `--engine` / `--display-name`：stderr 用法错误；退出 `2`。
- 落盘副作用：仅校验通过时覆盖入库。

**示例**：

```bash
conduct workflow node add flow a --engine claude-code --display-name 调研现状      # 自动接 START→a→END
conduct workflow node add flow b --engine qoder       --display-name 起草方案      # 再来一个，从 START 并行
conduct workflow node add flow c --engine claude-code --display-name 实现 --from a,b --to END
```

示意输出：

```
✓ 已加节点 flow·a
```

---

## workflow node rm — 删节点

**用途**：删一个 agent 节点及其所有连边，再校验结果。合法则做；会制造孤立节点（某节点因此失去全部入边或全部出边）则拒绝并说明。`START` / `END` 是保留节点，`node rm START` / `node rm END` 直接拒绝。

**用法**：

```
conduct workflow node rm <name> <id>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 待删 agent 节点 id（须存在，且非 `START` / `END`） |

**输出**：

- 校验通过：stdout `✓ 已删节点 <name>·<id>`；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`，级联删除所有以该 id 为端点的边）。
- `<name>` / `<id>` 不存在：stderr 报错；退出 `1`。
- `<id>` 为 `START` / `END`：stderr `START / END 为保留节点，不可删除`；退出 `1`；不改。
- 删后结果不合法（孤立某节点、或该节点被他人 `{{<id>}}` 引用致悬空）：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。删除会破坏图时须先调整下游（改边 / 改引用）再删。
- 落盘副作用：仅校验通过时覆盖入库。

**示例**：

```bash
conduct workflow node rm flow c    # 删 c 及其连边；若因此孤立 a/b 则拒绝
```

---

## workflow node set — 局部编辑（结构化字段）

**用途**：只改**某个节点的结构化字段**，不重发整份定义——覆盖节点 id、引擎 / 模型 / 档位、节点显示名。改一处，输入里就只出现那一处：消灭「为把模型改一下而复述几百行含长提示词 JSON」的 token 成本与误伤风险。每个可调项一个**带类型的显式 flag**（`--help` 天然就是「这节点能调什么」的可发现文档）。在内存中就地改完后，**复用整份定义的同一套校验**再落盘（不写坏数据）。改 `id`（`--id`）会**级联改名**——把所有边端点与各节点模板里的 `{{<旧id>}}` 引用一并换成新 id，不留悬空引用（转义 `\{{<旧id>}}` 视作字面量、不动）。提示词正文走 `node set-prompt`（本命令不碰 `promptTemplate`）；增删节点走 `node add` / `node rm`；改连边走 `edge`。

**用法**：

```
conduct workflow node set <name> <id> \
    [--id <newid>] [--engine <e>] [--model <m>] [--effort <v>] [--reasoning-effort <v>] [--display-name <s>]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 目标 agent 节点 id（须存在于该工作流，且非 `START` / `END`） |

**选项**（**至少给一个**，否则退出 `2`）：

| 选项 | 类型 | 说明 |
| --- | --- | --- |
| `--id <newid>` | 字符串 | 改节点 id，并**级联**同步所有边端点与各节点模板里的 `{{<旧id>}}` 引用（转义 `\{{<旧id>}}` 不动）。`newid` 须合法（`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`）、非保留名 `START` / `END`、不与现有节点重名 → 见下「诚实边界」 |
| `--engine <e>` | 字符串 | 设引擎，取值见〈workflow 定义 schema〉能力表（`claude-code` / `antigravity` / `qoder` / `codex`）。改 engine 可能使原 `engineConfig` 字段被新 engine 拒收 → 见下「诚实边界」 |
| `--model <m>` | 字符串 | 设模型；传空串 `--model ""` **清除**该字段（回落引擎默认模型） |
| `--effort <v>` | 字符串 | 设 claude-code 档位（取值见能力表，随模型）；传空串清除。仅 `engine=claude-code` 的节点接受 |
| `--reasoning-effort <v>` | 字符串 | 设 qoder / codex 推理档位（取值见能力表）；传空串清除。仅 `engine=qoder` 或 `engine=codex` 的节点接受 |
| `--display-name <s>` | 字符串 | 改节点显示名（纯装饰标签、零引用负担）；须非空 |

> 给了该引擎不认的字段（如给 `antigravity` 节点设 `--effort`），按〈落盘校验规则〉的能力表**拒绝**，与走 JSON 编辑路径逐字节一致。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>·<最终id>`；退出 `0`；未改 id 时 `<最终id>` 是目标原 id，给 `--id` 改名时则回显新 id；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` / `<id>` 不存在，或 `<id>` 为 `START` / `END`：stderr 报错；退出 `1`。
- 字段值非法（engine 不认该调优字段 / 枚举外 / `--display-name` 为空 / `--engine` 改后 `engineConfig` 不兼容 / `--id` 的新 id 非法 / 为保留名 / 与现有节点重名）：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- 未给任何字段选项：stderr 提示用法错误；退出 `2`。
- 落盘副作用：仅校验通过时覆盖入库。

**诚实边界**：

- **本命令覆盖节点的结构化字段、id 与显示名，但不碰提示词、不增删节点、不改边**：提示词走 `node set-prompt`；增删节点走 `node add` / `node rm`；改边走 `edge`。`id` 有**引用完整性**（被 `{{<节点id>}}` 与边引用），故 `--id` 改名时**连带改引用**一次做完（级联所有边端点与模板 `{{<旧id>}}`，转义字面量不动），不留悬空引用——这与全量 `edit` 改 id 殊途同归，只是省去复述整份定义。`displayName` 是纯装饰、零引用负担，随 `--display-name`。
- **清除标量用空串**：`--model` / `--effort` / `--reasoning-effort` 用**空串**表达「清除、回落引擎默认」。空串本就是这些字段的非法值，语义无歧义。
- **`--engine` 改引擎的级联**：`engineConfig` 是随 `engine` 判别的载荷，改 engine 后旧的 `model` / `effort` / `reasoningEffort` 未必被新 engine 接受。此时若不同时重设兼容配置，整份定义校验会失败、命令拒绝退 `1`——可在**同一条命令**里一并给（如 `--engine qoder --effort ""` 清掉 claude-code 专属的 effort、或 `--engine qoder --reasoning-effort medium` 换成 qoder 的档位）。绝不静默丢弃不兼容字段。

**示例**：

```bash
conduct workflow node set flow gen  --model claude-sonnet-5        # 换模型
conduct workflow node set flow test --engine qoder --effort ""    # 换引擎，同时清掉旧引擎专属的 effort
conduct workflow node set flow gen  --display-name 生成            # 改显示名
conduct workflow node set flow gen  --id plan                     # 改 id 为 plan：级联改所有边端点与模板 {{gen}}→{{plan}}
```

示意输出：

```
✓ 已更新 flow·gen
```

---

## workflow node set-prompt — 局部编辑（提示词）

**用途**：把某节点的 `promptTemplate` 以**原始文本**从 **stdin** 读入，由 conduct 负责 JSON 编码——作者永远不必手工把含 `{{变量}}`、中文、markdown、多行的提示词转义进 JSON 字符串。与 `node show --prompt`（导出纯文本）构成 round-trip：导出到文件、编辑、写回。落盘前复用整份定义的同一套校验（含模板变量引用校验：引用须皆为本节点的上游祖先 agent 节点，见〈落盘校验规则〉）。

**用法**：

```
conduct workflow node set-prompt <name> <id>
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 目标 agent 节点 id（须存在，且非 `START` / `END`） |

**输入**：提示词正文从 **stdin** 原始读入整段（`cat prompt.md | conduct workflow node set-prompt flow gen`）。读入后**剥掉恰好一个尾随换行**（若存在）再存——兼容编辑器 / `echo` 自动补的尾换行，与 `node show --prompt`「补恰好一个尾换行」配对，使 round-trip **字节稳定、不会静默给提示词尾部加 `\n`**；诚实代价：无法存一个真的以换行结尾的提示词（提示词几乎不需要）。stdin 是终端（无管道）时报错退出 `2`、**不挂起等待输入**。

**输出**：

- 校验通过：stdout `✓ 已更新 <name>·<id> 提示词`；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` / `<id>` 不存在，或 `<id>` 为 `START` / `END`：stderr 报错；退出 `1`。
- 空输入（stdin 读到空、或剥掉尾换行后为空）/ 提示词内模板变量引用非法（`{{<nodeId>}}` 指向不存在节点、非本节点祖先、或 `{{START}}` / `{{END}}`；未知 `{{sys.*}}`）：stderr 字段级错误（`promptTemplate` 不可为空 / 引用错误）；**原文件不变**；退出 `1`（校验类）。
- stdin 是终端（无管道输入）：stderr 报缺少输入；退出 `2`。
- 落盘副作用：仅校验通过时覆盖入库。

**示例**：

```bash
# round-trip：导出 → 编辑 → 写回
conduct workflow node show flow gen --prompt > gen.md
$EDITOR gen.md
conduct workflow node set-prompt flow gen < gen.md

# 直接管道灌入
cat impl-prompt.md | conduct workflow node set-prompt flow c
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
conduct workflow node show <name> <id> [--prompt | --json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<id>` | 是 | 节点 id（须存在，且非 `START` / `END`——标记节点无可展示的定义） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--prompt` | 布尔 | `false` | 只输出该节点的 `promptTemplate` **纯文本原文**（不含 JSON 引号 / 转义），供重定向到文件 |
| `--json` | 布尔 | `false` | 输出该节点的**规范化定义 JSON**（单个对象） |

> `--prompt` 与 `--json` 互斥（`--prompt` 已是纯文本、`--json` 是结构化对象），同时给报用法错误退 `2`。

**输出**：

- 默认（无 `--prompt` / `--json`）：人类可读单节点详情——`id · displayName · engine · model`，随后提示词**全文**（单节点场景不截断——就一个节点、不怕长）；退出 `0`。
- `--prompt`：stdout 打印 `promptTemplate` 全文并补**恰好一个**尾随换行（便于 `> file` 与终端显示），不加其它任何修饰；退出 `0`。与 `node set-prompt`「剥掉恰好一个尾随换行」配对，round-trip 字节稳定。
- `--json`：stdout 打印规范化后的单 node 对象 JSON（camelCase、补齐默认值）；退出 `0`。
- `<name>` / `<id>` 不存在，或 `<id>` 为 `START` / `END`：stderr 报错；退出 `1`。
- `--prompt` 与 `--json` 同时给：stderr 用法错误；退出 `2`。

**示例**：

```bash
conduct workflow node show flow gen                    # 人类可读单节点详情
conduct workflow node show flow gen --prompt > gen.md  # 导出提示词纯文本，编辑后 node set-prompt 写回
conduct workflow node show flow gen --json             # 单节点规范化 JSON
```

---

## workflow edge — 连边（列出 / 原子批量改边）

**用途**：不带 `--add` / `--rm` 时列出该工作流的全部边；带 `--add` / `--rm` 则原子批量改边——各给若干条，一次算清目标边集再整份校验落盘。给一对即单改，给多对即原子批量；批量能一步完成「无合法单边中间态」的拓扑变换（如 reorder）。取代分立的 `edge add` / `edge rm`；列出也并入本命令（不设 `edge list` 子命令，免得名为 `list` 的工作流被 cobra 抢路由而无法改边）。

目标边集 =（当前边集 −（所有 `--rm`））∪（所有 `--add`），**末尾整份校验一次**、通过才落盘。存在性检查按此求值顺序：`--rm` 对**当前边集**判定（删不存在的边报错退 `1`）、`--add` 对**「当前 − rm」**后的集合判定（加已存在的边报错退 `1`）。故同一条边同时给 `--add` 与 `--rm` 等价「先删后加」——结果为**保留**该边、不报重复。

**用法**：

```
conduct workflow edge <name>                                        # 无 --add/--rm：列出全部边
conduct workflow edge <name> [--json]                               # 列出边（机器可读）
conduct workflow edge <name> --add <from:to> ... --rm <from:to> ...  # 原子批量改边
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |

**选项**（不带 `--add` / `--rm` 即列出边；改边时二者**至少给一条**、可各给多条）：

| 选项 | 类型 | 说明 |
| --- | --- | --- |
| `--add <from:to>` | 字符串（可重复） | 加一条边；`from` / `to` 为节点 id（可为 `START` / `END`），用 `:` 分隔（node id 不含 `:`）。如 `--add START:x` 让 x 从头并行、`--add x:END` 让 x 成为终端。**加一条已存在的边（对「当前 − rm」判定）报错退 `1`**（与 `--rm` 对称，不静默去重） |
| `--rm <from:to>` | 字符串（可重复） | 删一条边；语法同上。删已不存在的边报错退 `1` |
| `--json` | 布尔 | 仅列出边（无 `--add` / `--rm`）时有效：以 JSON 数组输出，每项 `{"from":"<id>","to":"<id>"}`；改边时无效 |

**输出**：

- 不带 `--add` / `--rm`（列出）：人类可读逐行 `<from> → <to>`，`--json` 则输出 `[{"from","to"}]`；退出 `0`。`<name>` 不存在 / 载入校验不过：stderr 报错、退出 `1`。
- 改边校验通过：stdout `✓ 已更新 <name> 边（+A -R）`（A / R 为增删条数）；退出 `0`；落盘覆盖 `<name>.json`（重戳 `updatedAt`）。
- `<name>` 不存在 / `from`·`to` 指向不存在节点 / `--rm` 删不存在的边：stderr 报错；退出 `1`；不改。
- 结果不合法（成环、自环、重复边、边指向 `START`、边源自 `END`、`START→END` 直连、孤立某节点、引用非祖先等）：stderr 打印字段级错误；**原文件不变**；退出 `1`（校验类）。
- `<from:to>` 格式非法（无 `:` / 空端点）：stderr 用法错误；退出 `2`。
- 落盘副作用：仅改边且校验通过时覆盖入库；列出无副作用。

> **纯 `--add` 也未必合法**：加边不新增孤立点，但仍可能触发自环 / 重复边 / 指向 `START` / 源自 `END` / `START→END` 直连 / 成环等——一律由末尾整份校验兜底，通过才落盘。

**reorder 示例（`a→b→c` 改为 `a→c→b`）**：拓扑对调需连**终端出边**一起迁移（`c:END` → `b:END`），否则 b 失去唯一出边被拒。以最小图 `START→a→b→c→END` 为例，配合 `set-prompt` 前后翻转数据流，每步落盘都合法：

```bash
conduct workflow node set-prompt flow c < c.md   # c 改引 {{a}}（a 仍是 c 祖先）
conduct workflow edge flow --rm a:b --rm b:c --rm c:END --add a:c --add c:b --add b:END
conduct workflow node set-prompt flow b < b.md   # b 改引 {{c}}（c 现是 b 祖先）
```

---

## workflow rename — 改名

**用途**：给一个既有工作流改名。改的是**标识（主键）**，不动定义（`nodes` / `edges`）——与 `edit`（换定义、不换名）正交。新名须合法且未被占用。**已有运行记录不随之改名**：run id 与 run.json 里的 `workflow` 保留旧名，这是快照语义下的诚实历史（见 [cli-runtime.md](./cli-runtime.md)〈数据模型〉）；往后的新运行才落到新名下。改名**不阻拦、也不等待**在跑的运行——进行中的 run 在旧名下自然收尾，与 `delete` 不拦在跑运行一致。

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
- 交互确认时拒绝删除：**stderr** `已取消`；退出 `0`；不删除任何文件（取消提示走 stderr，保 stdout 只承载数据、`--json` 不被污染，与 `run rm` 一致）。
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

- 人类可读（默认）：表格，列为 `NAME | NODES | UPDATED`——名称、**agent 节点 id 流**（按**确定性拓扑序**以 `,` 连接——先按层号、同层再按其在定义 `nodes[]` 中的出现序，使 `--json` 与人读列跨次运行稳定不抖；**不含 `START` / `END`**；超过 6 个截断为前 6 个 + `+N`）、最近修改时间；**按 `updatedAt` 倒序**（最近修改的在前，`updatedAt` 相同再按 `name` 升序兜底、免同刻并列抖动），与 `run list` 的时间倒序同一「最近优先」心智；退出 `0`。
- `--json`：数组，每项 `{"name","nodes":["<id>",...],"updatedAt":"<RFC3339>"}`（`nodes` 为 agent 节点 id 数组、不含 `START` / `END` 与存储路径）；顺序同上（`updatedAt` 倒序、`name` 升序兜底）。
- store 为空：stdout 提示 `（store 为空）`；退出 `0`（空不是错误）。
- 单个工作流文件无法解析：跳过该记录、继续列出其余工作流并退出 `0`；每个跳过项向 stderr 打印 `警告: 跳过无法解析的工作流: <原因>`。若所有记录都损坏，人读 stdout 仍按空 store 输出，stderr 保留逐项警告；`--json` stdout 输出空数组。

> 设计说明：节点列展示 agent 节点 id 流（排除 `START` / `END` 两个标记节点）；`conduct ui` 工作流列表与此同列同源。

**示例**：

```bash
conduct workflow list
conduct workflow list --json | jq '.[].name'
```

示意输出：

```
NAME       NODES              UPDATED
my-flow    main               2026-07-03 15:40
autopilot  plan,code,test,review  2026-07-03 10:22
```

---

## workflow show — 查询（详情）

**用途**：查看单个工作流的 DAG 详情——节点清单 + 边邻接；可选附带**拓扑分层预览**（同层可并行，零成本核对图会被怎样并行调度）。整份工作流用本命令，单个节点用 `workflow node show`。

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
| `--expand` | 布尔 | `false` | 追加打印**拓扑分层**（同层节点可并行）。注明实际调度是贪心的——节点自身依赖就绪即开跑，未必等整层 |

**输出**：

- 人类可读（默认）：打印名称 / agent 节点数，随后逐 agent 节点一行 `id · displayName · engine · model`，再打印边邻接（标注 `START` / `END`）；`--expand` 时附拓扑分层清单；退出 `0`。
- `--json`：输出**规范化后**的完整记录 JSON（camelCase、补齐默认值，含 `name` / `createdAt` / `updatedAt` 元数据与 `definition:{ nodes, edges }`、含 `START` / `END`）——比 `edit` 导入所需的 `definition` 主体多出元数据外壳，直接喂回 `edit` 时外壳被解包、元数据忽略；`--expand` 时在最外层额外含 `"levels":[["<id>",...],...]`（拓扑分层，不含 `START` / `END`）。
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
plan   · 规划 · claude-code · claude-opus-4-8
code   · 编码 · claude-code · claude-opus-4-8
test   · 测试 · antigravity · (引擎默认)
review · 评审 · claude-code · claude-opus-4-8

边：
  START → plan
  plan  → code
  plan  → test
  code  → review
  test  → review
  review → END
```

`--expand` 在上述默认输出末尾**追加**拓扑分层：逐层一行 `level i: [a, b, …]`（第 i 层的 agent 节点集合，同层可并行、不含 `START` / `END`）。**层号算法**：节点层号 = `max(各前驱层号) + 1`，以 `START` 为唯一前驱者落 `level 0`（`START` 视作 `−1` 层、t0 完成），`END` 不计入；由此每条边严格增层，故同层节点间无依赖路径、必可并行。上例 `plan → {code, test} → review` 的追加段：

```
拓扑分层（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）：
  level 0: [plan]
  level 1: [code, test]
  level 2: [review]
```

`--json --expand` 则在规范化定义 JSON 上额外挂 `"levels"` 字段：

```json
{ "name": "autopilot", "createdAt": "…", "updatedAt": "…",
  "definition": { "nodes": [ /* …含 START/END… */ ], "edges": [ /* … */ ] },
  "levels": [["plan"], ["code", "test"], ["review"]] }
```

---

## workflow 定义 schema（自包含参考）

落盘的一份 workflow 是完整记录 `{ name, createdAt, updatedAt, definition }`——最外层只有 4 个字段：`name` / `createdAt` / `updatedAt` 是系统管理的元数据，`definition` 是**定义主体**（对象 `{ nodes, edges }`，`nodes` 含两个保留标记节点 `START`、`END`）。**作者 / 导入只写主体**：`create --definition` / `edit` 的输入单元就是 `definition` 的值 `{ nodes, edges }`，`name` 来自命令参数、`createdAt` / `updatedAt` 由 store 管理；导入体若直接给整条记录（如 `show --json` 输出，含 `name` / 时间戳外壳与 `definition` 包裹层）则解包 `definition`、忽略元数据，故「导出→改→写回」往返成立。（实现上导入侧先探测有无 `definition` 外壳键：有则按整条记录解析、取其 `definition`；无则整体按主体解析——两条路径均 `DisallowUnknownFields` 严格校验，拼写错的未知字段仍拒绝。）字段规格如下（`//` 为注释，实际 JSON 不含）：

```jsonc
{
  "name": "autopilot",            // 主键：工作流名（= 文件名 <name>.json，来自 create 参数；仅 rename 可改）
  "createdAt": "2026-07-03T09:12:00+08:00",  // 创建时间（RFC3339；create / copy 时写，此后不变）
  "updatedAt": "2026-07-03T15:40:00+08:00",  // 最近修改时间（RFC3339；任一变更命令重戳，导入值忽略）
  "definition": {                 // 定义主体：edit / create --definition 的输入单元就是这个对象
    "nodes": [
      { "id": "START" },          // 保留标记节点：无 engine/prompt/engineConfig、不执行、不产 trace、无入边
      {
        "id": "plan",             // agent 节点 id：必填、唯一、命名受限（见〈落盘校验规则〉）、不得为 START/END；模板中以 {{plan}} 引用其产物
        "displayName": "规划",     // 必填，进度展示用
        "engine": "claude-code",  // 必填（判别式）：claude-code | antigravity | qoder | codex
        "engineConfig": {         // 选填；shape 由 engine 决定，合法字段见下「引擎载荷」
          "model": "claude-opus-4-8",  // 选填；取值受 engine 约束，省略则用引擎默认模型
          "effort": "high"        // claude-code 档位（antigravity 无；qoder / codex 用 reasoningEffort）
        },
        "promptTemplate": "…{{sys.userPrompt}}…"  // 必填，见下「模板变量」
      },
      { "id": "END" }             // 保留标记节点：无 engine/prompt/engineConfig、不执行、无出边
    ],
    "edges": [
      { "from": "START", "to": "plan" },   // from 跑完才轮到 to；from 可为 START，to 可为 END
      { "from": "plan",  "to": "END" }
    ]
  }
}
```

**START / END（保留标记节点）**：`nodes[]` 里 `id == "START"` / `id == "END"` 是标记节点——无 `engine` / `promptTemplate` / `engineConfig`、不执行、不产 trace。`START` 是唯一源（无入边）、`END` 是唯一汇（无出边）。二者恒各存在一个：`create`（脚手架）由 `Scaffold` 带出，`create --definition` / `edit` 则要求导入体自带（校验强制恰好各一个，store 不注入）；均不可删除。判别以 id 为准（`IsStart` / `IsEnd` / `IsMarker` / `IsAgent`），不散落裸字面串比较。

**engine + engineConfig（判别联合）**：`engine` 是判别式（tag），`engineConfig` 是引擎专属载荷，其合法字段由 `engine` 决定——这把「engine / model / effort 三者绑定」编进结构本身，三者不能各自独立填。支持的引擎（`claude-code` / `antigravity` / `qoder` / `codex`）、各引擎接受的 `engineConfig` 字段及枚举、schema 字段到 CLI 参数的映射，见 [engines.md](./engines.md)〈引擎能力表〉——那是引擎层的单一权威来源。`model` 省略则用该引擎默认模型。具体校验（严格、依能力表）见〈落盘校验规则〉。

**edge（边）**：`{ "from": "<id>", "to": "<id>" }`，表达执行依赖——`from` 跑完（成功）才轮到 `to`。`from` 可为 `START`（让 `to` 从头并行），`to` 可为 `END`（让 `from` 成为终端）。边只表依赖、不传数据；数据靠 `promptTemplate` 的 `{{<id>}}` 拉取。约束（自环 / 重复边 / 指向 START / 源自 END / START→END 直连 / 成环）见〈落盘校验规则〉。

**模板变量**（`promptTemplate` 内）：

| 写法 | 展开为 |
| --- | --- |
| `{{sys.userPrompt}}` | 用户需求 |
| `{{sys.cwd}}` | 工作目录 |
| `{{sys.runId}}` | 本次运行的 run id |
| `{{<nodeId>}}` | 该**上游祖先** agent 节点最近一次成功产物 |
| `\{{x}}` | 转义，输出字面量 `{{x}}` |

> **数据流限祖先引用**：`{{<nodeId>}}` 的 `<nodeId>` 必须是本节点的**上游祖先 agent 节点**（沿边可达的前驱），保证运行到本节点时其产物必已就绪。禁止 `{{START}}` / `{{END}}`（标记节点无产物）。并行分支的产物**不自动汇聚**——要收口就让下游节点在其 prompt 里逐个引用每条分支的 `{{id}}`。
>
> **fail-loud**：引用不存在 / 非祖先的 nodeId 或未知 `{{sys.*}}` 会在入库校验时即被拒绝（见〈落盘校验规则〉）；即便绕过校验，运行时也**保留字面量而非静默置空**作为兜底，让作者一眼看出写错。

## workflows/ 落盘结构

工作流定义落在固定的全局 store `~/.conduct/workflows/` 下——无数据库、纯文件，便于人肉查看、备份与版本管理；首次使用时自动创建缺失目录。

```
~/.conduct/
└── workflows/                     # 工作流定义（create/copy/edit/node …/edge …/rename/delete/list/show 操作此处）
    ├── autopilot.json
    └── my-flow.json
```

**`workflows/<name>.json`** —— 一份完整 workflow 记录 `{ name, createdAt, updatedAt, definition:{ nodes, edges } }`。字段结构见〈workflow 定义 schema〉；落盘为**规范化形态**（camelCase、补齐默认值、`definition` 含 `START` / `END` 节点与连边），故 `show --json` 打印的即此文件内容。文件名 `<name>` 与内部 `name` 一致（`[A-Za-z0-9._-]+`，作主键）。

> 运行记录 `~/.conduct/runs/` 的布局见 [cli-runtime.md](./cli-runtime.md)〈runs/ 落盘结构〉。

## 落盘校验规则

无独立 `validate` 命令：下列校验由所有变更命令（`create` / `copy` / `edit` / `node add` / `node rm` / `node set` / `node set-prompt` / `edge`）在**入库前强制执行**，不过即拒绝、不写盘（`show` / 运行时载入时也复用同一套）。

**校验的执行点**：不变量由每条写命令在 `store.Save` / `store.ReplaceDefinition` 前各自调用 `Validate` 强制；store 的写方法自身不做语义校验。`store.Load` 只做严格 JSON 解码（拒绝未知字段与尾随内容），不调用 `Validate`；需要可运行定义的 CLI 路径（如 `workflow run`、`show --expand`）会在载入后显式校验。UI 的读取路径刻意允许载入“结构可解码但语义非法”的定义，以便在编辑器中修复。新增写路径务必遵此约定。校验内核提供 `ValidateStructured` 返回字段级 `[]Problem`，供 CLI / UI 定位到具体字段。

**坏文件的处置**：手改导致 JSON 结构损坏、未知字段或语义非法时，读类与粒度编辑命令（`show` / `node …` / `edge`）会在严格解码或各自的显式校验点拒绝。此时 `delete`（只删文件、不依赖内容）与 `edit`（整份替换只要求目标文件存在，不要求旧内容可解码）可直接删除 / 覆盖修复，是从坏文件恢复的通道。`edit` 的新定义仍须通过完整语义校验；旧元数据可读时保留 `createdAt`，不可读时以修复时刻重建。

规则：

- **节点集**：`nodes` 非空，**恰好含一个 `id=="START"`、一个 `id=="END"`**，另有**至少一个 agent 节点**。
- **保留名**：agent 节点 id 匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`——首字符为字母或下划线，其余限字母 / 数字 / 连字符 / 下划线，总长 1–64；同一份定义内唯一；且**不得为 `START` / `END`**（保留）。（注意：这套 id 规则与工作流名 `<name>` 的 `[A-Za-z0-9._-]+` 不同——后者是 store 文件名，可含点、可数字开头。）
- **START / END 标记节点**：`engine` / `promptTemplate` / `engineConfig` / `displayName` 必空（标记节点不承载配置与展示名，UI 直接渲染其 id）；`START` 无入边；`END` 无出边。
- **agent 节点**：`displayName` / `engine` / `promptTemplate` 必填；`engineConfig` 结构合法。
- **engine + engineConfig 严格校验（判别联合）**：先按 `engine` 选定该引擎的**能力表**（见 [engines.md](./engines.md)〈引擎能力表〉），再校验 `engineConfig`：`engine` 合法（已注册）、调优字段属于该 `engine`（`effort` ↔ claude-code；`reasoningEffort` ↔ qoder / codex；`antigravity` 无独立调优字段、仅认 `model`）、其值在该字段允许集内、无该引擎不认的多余字段；任一不符即拒绝。`model` 当前**不做白名单**（接受任意非空串，待有权威模型表再收紧）；能力表里的 `ModelValues` 仅供 UI 下拉建议，不参与落盘校验。
- **边合法**：`from` / `to` 指向存在的节点（可含 `START` / `END`）；**禁止**自环（`from==to`）、重复边、边指向 `START`、边源自 `END`、`START → END` 直连（须过 ≥1 个 agent 节点）。
- **无环**：`DetectCycle` 命中即拒，报出环路径。这一条落地「删除循环」与「编辑期拒环」。
- **单源单汇 / 无悬空**：每个 agent 节点 ≥1 入边（可来自 `START`）、≥1 出边（可到 `END`）。由此推论：入度 0 的只有 `START`、出度 0 的只有 `END`，每个 agent 节点都落在某条 `START → … → END` 路径上，无游离子图。
- **模板变量引用祖先**：`promptTemplate` 里非转义的 `{{<nodeId>}}` 必须引用本节点的**上游祖先 agent 节点**（在无环校验通过后计算祖先集，不含 `START` / `END`）；`{{sys.*}}` 仅限已知系统变量（`sys.userPrompt` / `sys.cwd` / `sys.runId`）；禁止 `{{START}}` / `{{END}}`。引用不存在 / 非祖先 / 标记节点即拒绝。

校验失败时逐条打印字段级错误（如 `nodes[1].engineConfig.effort: engine="antigravity" 不认 effort`、`edges: 检测到环 a→b→a`），退出码 `1`。

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：校验失败、IO 失败、目标不存在、命名冲突（已存在）、保留节点操作等 |
| `2` | 用法错误（缺参、非法参数、非交互拒绝危险操作、stdin 是终端却需管道输入）——Cobra 默认 |

校验失败并入 `1`，不单列专用码——具体原因看 stderr 的字段级报错。（运行时命令的退出码见 [cli-runtime.md](./cli-runtime.md)〈退出码约定〉，同一张表。）

## 实现状态（诚实标注）

本规格描述的「节点 + 边的并行 DAG」模型**已实现**：领域模型、图算法、单层校验、CLI 命令族均已按本规格完成改造。

| 命令 / 主题 | 状态 |
| --- | --- |
| 领域模型：外层记录 `{name, createdAt, updatedAt, definition}` + 内层 `definition{nodes, edges}` + START/END 标记节点 | **已实现**（`internal/workflow/definition.go`：外层 `Workflow{Name, CreatedAt, UpdatedAt, Definition}` + 内层 `Definition{Nodes, Edges}`；`Node` 已删除 `evaluator`/`redoTarget`/`loopCount`，改用 `IsStart`/`IsEnd`/`IsMarker`/`IsAgent` 方法判别 START/END，不散落裸字面串比较） |
| `workflow node add` / `node rm` | **已实现**（`internal/cli/workflow_node_add.go` / `workflow_node_rm.go`：缺省自动接 `START→<id>→END`，落盘前复用整份校验） |
| `workflow edge` | **已实现**（`internal/cli/workflow_edge.go`：无 `--add`/`--rm` 列出全部边，带则 `--add`/`--rm` 原子批量改边） |
| `workflow node set` 瘦身 | **已实现**（`internal/cli/workflow_node_set.go` 已删除 `--evaluator` / `--no-evaluator` / `--redo-target` / `--no-redo` / `--loop-count`，留 `--id` / `--engine` / `--model` / `--effort` / `--reasoning-effort` / `--display-name`；`--id` 走 `workflow.RenameNodeID` 级联改名） |
| `workflow node set-prompt` 祖先引用 | **已实现**（引用校验已从「存在」升级为「祖先」：`internal/workflow/validate.go` 的 `validateTemplateAncestry` 基于 `Ancestors` 图算法判定，已去 `--evaluator`） |
| `workflow node show` 去循环模式 | **已实现**（输出与选项已随新模型调整，无循环模式 / evaluator 相关字段） |
| `workflow show` 图视图 + `--expand` 拓扑分层 | **已实现**（`internal/cli/workflow_show.go`：打印邻接并标注 START/END；`--expand` 改为 `workflow.TopoLevels` 输出的拓扑分层，`--json --expand` 在完整记录 JSON 上挂 `"levels"` 字段） |
| `workflow create / copy / edit / rename / delete / list` | **已实现**（脚手架骨架为 `START→node-1→END`；`list` 的 NODES 列按确定性拓扑序展示 agent 节点、排除 START/END；`edit` / `create --definition` 的导入单元收窄为主体 `{nodes, edges}`——导入体带整条记录时解包 `definition`、元数据不一致不再报错，一律忽略） |
| 落盘校验规则（单源单汇 / 无环 / 祖先引用 / 保留名） | **已实现**（`internal/workflow/validate.go` + `internal/workflow/graph.go`：`ValidateStructured` 覆盖恰好一对 START/END + ≥1 agent、保留名、标记节点必空、边合法性、`DetectCycle` 无环、每 agent 节点入度出度 ≥1、`validateTemplateAncestry` 祖先引用） |

> `internal/workflow/testdata/` 下的夹具（`wf_autopilot.json` / `wf_demo.json`）已按新 DAG 模型重建（含 START/END 节点与 edges）。旧的线性 + 循环模型定义与本模型不兼容，无迁移路径——这是本次改造的既定取舍，非缺陷。

## 提示词来源约定

`node set-prompt` 的提示词正文**只从 stdin 读入，不提供 `--file`**（与 `edit` / `create --definition` 一致）；文件场景用 shell 重定向 `< prompt.md` 覆盖。`node add --prompt` 则收内联简单文本（默认 `{{sys.userPrompt}}`），复杂 / 多行提示词回落 `set-prompt`。
