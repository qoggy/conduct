# conduct CLI 命令规格

> 本文规定 conduct CLI 的**完整命令面**——每条命令的用途、入参（位置参数 + 选项）、出参（stdout / stderr / 退出码 / 落盘副作用）、示例，覆盖 workflow 的**创建 / 编辑 / 删除 / 查询 / 运行**，以及**运行记录的查询**。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」。当前代码仅有可编译骨架、`run` 是 stub；逐条实现状态见文末〈实现状态〉。工作流的数据模型与展开语义见〈workflow 定义 schema〉。

## 设计前提（可推翻，动手实现前请确认）

下面几条是整份规格的地基。它们决定了每条命令的形态，若你不认同，先改这里、其余随之调整：

- **工作流是「有名字的托管对象」，不是散落文件。** 存储在一个 *store* 里，`create / edit / rename / delete / show / run` 一律按**名字**定位工作流（`list` 作用于整个 store，无需名字）；不接受直接传文件路径——要跑手头的一份 JSON，先 `create --definition` 入库拿到名字，再按名字 `run`。
  - 理由：只有工作流是托管对象时，「删除 / 查询」才值得做成子命令；若只是文件，它们就退化成 `rm` / `ls`。
- **store 位置（固定）**：工作流统一存放在全局 `~/.conduct/workflows/`（每份一个 `<name>.json`）；运行记录存放在 `~/.conduct/runs/<id>/`（一次运行一个目录）。所有命令固定读写此 store，**不支持自定义存储位置**（完整落盘布局见〈落盘存储结构〉）。
- **命令风格 noun-first（统一）**：资源操作命令形如 `conduct <noun> <verb>`（对标 `gh` / `kubectl`），无顶层动词命令。当前两个 noun：`workflow`（工作流定义，create/edit/rename/delete/list/show/run）与 `run`（运行记录，list/show）——如 `gh` 里 `gh workflow run` 触发、`gh run list` 查历史，二者并存不冲突。便于未来再扩展 `engine` 等资源族。**例外**是不针对单一资源的顶层工具命令 `conduct version`（版本）与 `conduct ui`（可视化界面，横跨 workflow 与 run 两族）。
- **对 AI-bash 与人类双友好（北极星）**：每条能力都以非交互、可脚本化的 CLI 命令提供（查询类带 `--json` 机读，变更类以退出码表达成败）；同时以顶层 `conduct ui` 提供一个 x-one-web 式可视化界面（人类层），聚合「编辑全部工作流 + 监控运行状态 + 启动运行」。**关键不变量：UI 无独占能力**——它能做的每件事都有对应的、可单独完成的 CLI 命令（编辑 ↔ `workflow edit` 喂 stdin、看状态 ↔ `run list` / `run show`、启动 ↔ `workflow run`），UI 只把它们聚合成人看的视图，绝不新增「只有界面能做」的功能。
- **名称即文件名**：工作流以 `<name>.json` 落盘。`<name>` 限定 `[A-Za-z0-9._-]+`，不含路径分隔符。

## 数据模型（两个实体）

conduct 只有两个模型，像数据库的两张表：

| 实体 | 是什么 | 主键 | 可变性 | 落盘 |
| --- | --- | --- | --- | --- |
| **workflow**（工作流） | 一份定义 | `name` | 可变（`edit` 改定义、`rename` 改名） | `workflows/<name>.json`（完整记录 `{ name, createdAt, updatedAt, nodes }`） |
| **run**（运行记录） | 一次执行 | run id ＝ `<workflow>-<时间戳>` | 不可变（历史） | `runs/<id>/`（run.json + run-summary.md + trace.jsonl） |

- **run id 内嵌 workflow**：目录名形如 `autopilot-20260703-152233`，id 自身即体现所属 workflow（时间戳后缀固定 `YYYYMMDD-HHMMSS`，故 workflow 前缀可无歧义还原）；权威归属另存于 run.json 的 `workflow` 字段。
- **关系是「快照」，不是「活外键」**：run 除 `workflow` 名外，还 copy 一份 `workflowSnapshot`（开始运行那一刻冻结的完整 workflow 记录）。类比订单行 copy 下单时的商品价格、而非外键到会变的现价——因为 run 是**不可变历史**，必须永远可复现。由此两条推论：
  1. **`edit` / `delete` 一个 workflow，不影响它已有的 run**：run 靠快照自解释，workflow 改了、删了，历史照样能看、能复现（`delete` 只删 `workflows/<name>.json`，`runs/` 分毫不动）。
  2. **跨表查询就是 join**：「某 workflow 的全部运行」＝按 `workflow` 过滤 run 表；「哪些 workflow 在跑」＝ run 表 filter `status:"running"`（据 `pid` 判活）再并回 workflow 表——运行态已持久化在 run.json，见〈落盘存储结构〉。

## 命令总览

| 命令 | 作用 | 对应诉求 |
| --- | --- | --- |
| `conduct workflow create <name>` | 脚手架/导入一份新工作流入库 | 创建 |
| `conduct workflow edit <name>` | 从 stdin 整体替换既有工作流（保存即校验） | 编辑 |
| `conduct workflow rename <old> <new>` | 给既有工作流改名（改标识，不动定义） | 改名 |
| `conduct workflow delete <name>...` | 从 store 删除一个/多个工作流 | 删除 |
| `conduct workflow list` | 列出 store 内全部工作流 | 查询 |
| `conduct workflow show <name>` | 查看单个工作流（可附展开预览） | 查询 |
| `conduct workflow run <name> "<需求>"` | 解释运行一份工作流 | 运行 |
| `conduct run list` | 列出历史运行记录 | 查询（运行） |
| `conduct run show <id>` | 查看某次运行的状态与详情 | 查询（运行） |
| `conduct run stop <id>` | 终止一次进行中的运行 | 终止（运行） |
| `conduct ui` | 可视化界面：编辑工作流 / 监控运行 / 启动，conduct 的整体 GUI | 人类界面 |
| `conduct help <主题>` | 输出跨命令的长文档（教程 / 概念 / 最佳实践） | 支撑 |
| `conduct version` | 打印版本号 | 支撑 |

> 没有独立的 `validate` 命令：定义校验已内化进 `create` / `edit` 的落盘环节（不合规即拒绝保存），规则见〈落盘校验规则〉。需要不运行、不花 token 地核对一份定义合不合法、并预览它会展开成什么步骤时，用 `show --expand`（载入即校验，再打印展开）。

> 文档分层：各命令的 `--help` 只做精简速查（本命令怎么用，遵循社区标准）；教程 / 概念 / 最佳实践这类**跨命令的长文档**不塞进 `--help`，改由 `conduct help <主题>` 输出（对标 `go help <topic>`）。主题按概念组织（如 `prompts` 讲 promptTemplate 怎么写好），文档 `go:embed` 进二进制随发布走（`docs/` 不随 `go install` 发布，故必须内嵌），相关命令的 `--help` 末尾留一行指针。

## 全局约定

**全局选项**（`-h, --help` 所有命令通用；`--version` 仅根命令）：

| 选项 | 说明 |
| --- | --- |
| `-h, --help` | 打印该命令的用法与选项后退出 0 |
| `--version` | 仅根命令 `conduct --version`：打印版本号后退出 0（等价 `conduct version`；子命令不挂此旗标，与 gh / kubectl 惯例一致） |

**通用选项**（涉及结构化输出的命令支持）：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（人类可读表格/装饰全部关闭），便于脚本消费 |

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

## workflow edit — 编辑

**用途**：编辑既有工作流——从 **stdin** 读入一份**完整**新定义整体替换（便于 AI / 脚本改写）。**保存即校验（规则见〈落盘校验规则〉），校验不过则拒绝写入、保留原定义不变**（不写坏数据）。可视化编辑请用 `conduct ui`，不在本命令内；**改名**用 `workflow rename`——`edit` 只换定义、不换名。

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

- 校验通过：stdout `✓ 已更新 <name>`；退出 `0`；落盘覆盖 `<name>.json`。
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

## workflow rename — 改名

**用途**：给一个既有工作流改名。改的是**标识（主键）**，不动定义（`nodes`）——与 `edit`（换定义、不换名）正交。新名须合法且未被占用。**已有运行记录不随之改名**：run id 与 run.json 里的 `workflow` 保留旧名，这是快照语义下的诚实历史（见〈数据模型〉）；往后的新运行才落到新名下。改名**不阻拦、也不等待**在跑的运行——进行中的 run 在旧名下自然收尾（其 run id 与 run.json 的 `workflow` 均保留旧名），与 `delete` 不拦在跑运行一致。

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
- 落盘副作用：删除对应 `<name>.json`。

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

> 设计说明：列由早期的 `NODES`（数量）`| ENGINES`（引擎集合）调整为节点 id 流并移除引擎列——节点名比引擎集合信息量更大，列表页保持克制；`conduct ui` 工作流列表与此同列。
- store 为空：stdout 提示 `（store 为空）`；退出 `0`（空不是错误）。

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

**用途**：查看单个工作流的定义详情；可选附带**展开预览**（复用运行时的展开算法，零成本核对节点图会被解释成怎样的线性步骤序列）。

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

- 人类可读（默认）：打印名称/节点数，随后逐节点一行 `id · displayName · engine · model · <循环模式>`（循环模式＝`evaluator 内循环` / `redoTarget→<目标> 回跳` / `单次`）；`--expand` 时附展开步骤清单；退出 `0`。
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

`--expand` 追加的展开清单，格式同〈workflow run〉输出里的展开段（`[i] type node=<id> iter=<n>`）。

---

## workflow run — 运行

**用途**：解释运行一份工作流——先把节点图展开成确定性线性步骤，再逐步驱动 AI 引擎（走无头 CLI）执行，串联上游产物与评测反馈，落盘 trace。

**用法**：

```
conduct workflow run <name> ["<用户需求>"] [--cwd <dir>] [--json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<name>` | 是 | 工作流名称 |
| `<用户需求>` | 条件必填 | 传入模板变量 `{{sys.userPrompt}}`；省略时改从 **stdin** 读取（见下「用户需求的来源」） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--cwd <dir>` | 路径 | 当前工作目录 | AI 引擎读写文件的工作目录，即模板变量 `{{sys.cwd}}`。显式传入时**必须是已存在的目录**（不存在 / 不是目录即报用法错误退 `2`，不带着错误目录去烧引擎）；省略时取当前工作目录 |

**用户需求的来源（按优先级）**：① 命令行位置参数 `<用户需求>`；② 省略它、且 stdin 是管道 / 重定向（非 TTY）时，读取**整个 stdin** 作为需求（如 `cat req.txt | conduct workflow run <name>`）。二者皆无、stdin 又是终端时，报参数缺失并退出 `2`，**不静默挂起等待输入**。

**输出**：

- 人类可读（默认）：
  1. 先打印 `▶ 展开为 N 步：` 及每步清单；
  2. 逐步打印进度 `● step i [displayName] agent|evaluator iter=<n> engine=<e> model=<m>`，每步完成打印 `✓ <耗时>ms tokens=<n> 产物 <len> 字符：<前 80 字预览>`；
  3. 结束打印 `✅ 完成，阅读 <run-summary.md 路径> 获取运行详情`；
  4. 退出 `0`。
- `--json`：stdout **每步一行**事件 JSON，无进度装饰（无单独汇总事件，整体概要见落盘的 `run.json`）。每行即 `trace.jsonl` 的一条记录，完整字段见〈落盘存储结构〉，下例仅列核心字段：

  ```json
  {"stepIndex":0,"type":"agent","nodeId":"node-1","iteration":1,"output":"...","success":true}
  ```

- 落盘副作用：在运行目录 `~/.conduct/runs/<id>/` 下——**开跑即写** `run.json`（`status:"running"`），`trace.jsonl` 逐步追加，**收尾**把 `run.json` 更新为终态并生成 `run-summary.md`；三文件结构见〈落盘存储结构〉，供 `run list` / `run show` 查询。
- 任一步引擎调用失败：stderr 打印错误（含引擎名、退出码、报错摘要）；该步完整错误写入 trace 对应行的 `error` 字段，`run.json` 记 `status:"failed"` 与失败步号 `failedStep`（指针，错误详情看该步 trace）；已完成步骤的 trace 保留；退出 `1`。
- 缺少 `<用户需求>` 且 stdin 是终端（无管道输入）：stderr 报参数缺失；退出 `2`。
- 显式 `--cwd` 指向不存在的路径 / 非目录：stderr 报用法错误；退出 `2`（发射前拦下，不烧引擎）。UI 启动弹窗与服务端 self-exec 预检复用同一校验（同源）。

**示例**：

```bash
# 端到端运行
conduct workflow run autopilot "给购物车加一个清空按钮"

# 长需求从 stdin 读取（文件或任意上游命令皆可）
cat requirement.txt | conduct workflow run autopilot

# 指定引擎工作目录
conduct workflow run demo "一个能解释运行 workflow 的引擎" --cwd ~/proj

# 机器可读逐步事件
conduct workflow run autopilot "..." --json | jq -c 'select(.type=="agent")'
```

示意输出（人类可读）：

```
▶ 展开为 4 步：
  [0] agent     node=plan   iter=1
  [1] agent     node=code   iter=1
  [2] evaluator node=code   iter=1
  [3] agent     node=review iter=1
● step 0 [规划] agent iter=1 engine=claude-code model=claude-opus-4-8
✓ 1240ms tokens=3155 产物 512 字符：# 方案：购物车页头部新增“清空购物车”按钮，点击弹确认……
● step 1 [编码] agent iter=1 engine=claude-code model=claude-opus-4-8
✓ 8021ms tokens=12040 产物 2048 字符：diff --git a/src/Cart.tsx b/src/Cart.tsx……
● step 2 [编码评审] evaluator iter=1 engine=claude-code model=claude-opus-4-8
✓ 2110ms tokens=1980 产物 320 字符：<verdict>PASS</verdict> 按钮实现完整、有确认弹窗……
● step 3 [评审] agent iter=1 engine=claude-code model=claude-opus-4-8
✓ 3050ms tokens=4200 产物 900 字符：整体符合需求；建议给空购物车禁用该按钮……
✅ 完成，阅读 ~/.conduct/runs/autopilot-20260703-152233/run-summary.md 获取运行详情。
```

---

## run list — 运行历史（列表）

**用途**：列出历史运行记录（`~/.conduct/runs/` 下每个运行目录一条）。

**用法**：

```
conduct run list [--json]
```

**参数**：无。

**输出**：

- 人类可读（默认）：表格，列为 `RUN ID | WORKFLOW | STATUS | STEPS | STARTED | PROMPT`——run id（目录名，形如 `<workflow>-<时间戳>`）、所属工作流名、状态（`running` / `completed` / `failed` / `interrupted`）、步数、开始时间、用户需求（`userPrompt` 截断至约 20 字、超出以 `…` 收尾——整份 PRD 这类长输入也不会撑爆表格）；按时间倒序；退出 `0`。
- `--json`：数组，每项 `{"id","workflow","status","steps":<数>,"startedAt":"<RFC3339>","userPrompt":"<完整、不截断>"}`（机读给全文，截断只发生在人类表格）。
- 无运行记录：stdout 提示 `（暂无运行记录）`；退出 `0`。

**示例**：

```bash
conduct run list
conduct run list --json | jq '.[] | select(.status=="failed")'
```

示意输出：

```
RUN ID                     WORKFLOW   STATUS     STEPS  STARTED           PROMPT
autopilot-20260703-171102  autopilot  running    14     2026-07-03 17:11  重构结算流程为状态机
autopilot-20260703-152233  autopilot  completed  14     2026-07-03 15:22  给购物车加一个清空按钮
demo-20260703-160140       demo       failed     4      2026-07-03 16:01  实现一个能解释运行 workflow 的引擎…
```

---

## run show — 运行详情 / 状态

**用途**：查看某次运行的状态与详情——汇总（状态、所属工作流、用户需求、步数、耗时）与逐步结果；可选附完整 trace。

**用法**：

```
conduct run show <id> [--trace] [--json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<id>` | 是 | run id（`run list` 里的 `RUN ID`，目录名，形如 `<workflow>-<时间戳>`） |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--trace` | 布尔 | `false` | 追加打印逐步 trace（`trace.jsonl` 的每步记录） |

**输出**：

- 人类可读（默认）：打印 `run-summary.md` 全文（运行总结，见〈落盘存储结构〉）；退出 `0`。运行**未收尾**（`running` / `interrupted`）时总结尚未生成，改打印状态摘要（run id / 状态 / 用户需求 / 步数 / 进度 `step k/N`）并提示用 `--trace` 查看已执行步骤。
- `--json`：输出 `run.json` 的规范化内容。
- **`--trace` 与 `--json` 正交**：`--json` 决定**格式**（机读 JSON ↔ 人类文本），`--trace` 决定**深度**（是否展开每步**完整** input/output）。四种组合：

  | | 人类（默认） | `--json` |
  | --- | --- | --- |
  | **不加 `--trace`** | `run-summary.md` 全文（未收尾时退回状态摘要） | `run.json` 概要 |
  | **加 `--trace`** | 状态摘要 + 每步完整 input/output | `run.json` + `"trace":[…]`（`trace.jsonl` 逐行） |

- 运行态：`status:"running"` 且 `pid` 存活 → 显示实时进度；`status:"running"` 但 `pid` 已死 → 标 `interrupted`、尽力展示已有 trace；退出 `0`。
- `<id>` 不存在：stderr `运行 <id> 不存在`；退出 `1`。

**示例**：

```bash
conduct run show autopilot-20260703-152233
conduct run show demo-20260703-160140 --trace
conduct run show autopilot-20260703-152233 --json | jq '.status'
```

示意输出（默认）：

- **已收尾**（completed / failed）：打印 `run-summary.md` 全文，格式见〈落盘存储结构〉的 run-summary.md 示例。
- **未收尾**（running / interrupted）：总结尚未生成，退回状态摘要——

  ```
  运行 autopilot-20260703-152233 · running
  需求：给购物车加一个清空按钮
  步数 4 · 进度 step 2/4 · 2026-07-03 15:22 起
  运行总结尚未生成（运行未收尾）；用 conduct run show autopilot-20260703-152233 --trace 查看已执行步骤。
  ```

---

## run stop — 终止运行

**用途**：终止一次进行中的运行——向该 run 记录的 pid 发送 SIGTERM。**先按进程组发**（`kill(-pid, SIGTERM)`），以连带终止该 run 派生的引擎子进程；若该进程不是组长（`ESRCH`）**回退为向单进程发**（`kill(pid, SIGTERM)`）。

**用法**：

```
conduct run stop <id>
```

**参数**：`<id>`（必填）——run id（`run list` 里的 `RUN ID`）。

**行为与输出**：

- run 存在、`status:"running"` 且 pid 存活：按上述「先组后单」发送 SIGTERM，stdout 提示已发送；退出 `0`。**不引入新的落盘状态**：被终止的进程不再写盘，此后 `run list` / `run show` 按既有 pid 判活语义显示为 `interrupted`。
- run 不存在：stderr `<id>: 运行不存在`；退出 `1`。
- run 已是终态、或 `status:"running"` 但 pid 已死（interrupted）：stderr 提示无可终止；退出 `1`。
- 信号发送失败（权限等）：stderr 转译原因；退出 `1`。

**诚实边界（进程组连带的适用范围）**：只有 `conduct ui` 以独立会话（`setsid`）发射的 run 才保证 pid 即组长、组信号能一并收割引擎子进程。终端里 `cat req | conduct workflow run` 这类**管道启动**的 run，conduct 不是组长，`kill(-pid)` 得 `ESRCH` → 回退单进程：只终止编排器本身，当前正在跑的引擎子进程会遗留到本步自然结束（编排器已死、不再驱动下一步）——可接受的降级。

---

## ui — 可视化界面（人类层）

**用途**：启动 conduct 的可视化界面（x-one-web 式的整体 GUI，覆盖 store 内全部工作流与运行），给人一个聚合视图：**编辑**全部工作流、**监控**各工作流的运行状态、**启动**运行。它是 CLI 动词层的人类对等物——见〈设计前提〉「双友好」不变量：**UI 不拥有任何独占能力**，它做的每件事都有对应 CLI 命令（编辑 ↔ `workflow edit`、看状态 ↔ `run list` / `run show`、启动 ↔ `workflow run`）。

**用法**：

```
conduct ui [--port <n>] [--open]
```

**参数**：无位置参数——它是 conduct 的整体 GUI，覆盖 store 内全部工作流与运行，不针对单一对象（要按名字操作单个工作流，用 `workflow` 名词下的动词）。

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--port <n>` | 整数 | `7420` | 监听端口；被占则 stderr 报错退出 `1`（不自动递增——可预测、书签友好） |
| `--open` | 布尔 | `false` | 启动后自动打开浏览器；默认不开（照顾 SSH / 无头环境），仅打印地址 |

**输出**：

- 启动界面后 stdout 打印入口地址，进程驻留至界面关闭（`Ctrl-C` 退出）。
- store 不可读 / 端口被占等启动失败：stderr 打印原因；退出 `1`。

示意输出：

```
conduct ui — 可视化界面已启动
  ▶ http://127.0.0.1:7420
按 Ctrl-C 退出。
```

**主次用途**：编辑与监控是主用途；从界面**启动**运行是次要用途——启动主路径是 `conduct workflow run`（面向 AI / bash）。

**启动与安全边界（定案）**：

- 服务**只绑 `127.0.0.1`**，不监听 `0.0.0.0`。
- **启动时主动探测一次 store 可读性**（执行一次 `List`）：不可读 → stderr 报原因退出 `1`（不做「启动假成功、首个请求才报错」）。
- **v1 不做账号鉴权**，但所有 `/api/*` 校验 `Host` / `Origin` 白名单（仅 `127.0.0.1:<port>` / `localhost:<port>`）、变更类端点仅接受 `application/json`。诚实边界：这防的是**浏览器跨站**（恶意网页 fetch 本地端口）与 DNS rebinding，**不防本机进程**——单用户本机工具下可接受。
- **启动运行走 self-exec 子进程**：UI 服务端以 `os.Executable()` 自呼 `conduct workflow run <name> --cwd <dir>`（`Setsid` 独立成组、stdin 喂需求、stdout→`/dev/null`），使 pid 判活 / `interrupted` 语义与终端启动逐字节一致，且关掉 UI 不连累在跑的 run。这是「UI 无独占能力」不变量的最强证明——启动 ≡ 执行一条 CLI 命令。

工作流的可视化编辑统一由本命令承担，`workflow edit` 只做非交互的 stdin 整体替换。前端（内嵌 SPA）见 `docs/specs/ui.md`〈前端技术栈〉，代码已落地、待浏览器走查验收。

---

## workflow 定义 schema（自包含参考）

落盘的一份 workflow 是完整记录 `{ name, createdAt, updatedAt, nodes }`——`name` / `createdAt` / `updatedAt` 是系统管理的元数据，`nodes` 是用户编写的定义主体。字段规格如下（`//` 为注释，实际 JSON 不含）：

```jsonc
{
  "name": "autopilot",            // 主键：工作流名（= 文件名 <name>.json，来自 create 参数；仅 rename 可改，edit / 导入体不改它）
  "createdAt": "2026-07-03T09:12:00+08:00",  // 创建时间（RFC3339；create 时写，此后不变）
  "updatedAt": "2026-07-03T15:40:00+08:00",  // 最近修改时间（RFC3339；create / edit / rename 时重戳，导入值忽略）
  "nodes": [{
    "id": "node-1",                 // 必填，唯一，命名受限（见〈落盘校验规则〉）；模板中以 {{node-1}} 引用其产物
    "displayName": "规划",           // 必填，进度展示用
    "engine": "claude-code",        // 必填（判别式）：claude-code | antigravity（codex 暂时下线，见〈实现状态〉）
    "engineConfig": {               // 选填；shape 由 engine 决定，合法字段见下「引擎载荷」
      "model": "claude-opus-4-8",   // 选填；取值受 engine 约束，省略则用引擎默认模型
      "effort": "high"              // claude-code 档位（antigravity 无、编码在 model 标签；codex 下线用 reasoningEffort）
    },
    "promptTemplate": "…{{sys.userPrompt}}…",  // 必填，见下「模板变量」
    "evaluator": {                  // 选填：带评测官 → in-place 内循环（写→评→改）；同构 engine + engineConfig
      "engine": "claude-code",
      "engineConfig": { "model": null, "effort": "medium" },
      "promptTemplate": "审阅：{{node-1}}"
    },
    "redoTarget": "<前序节点 id>",   // 选填：与 evaluator 互斥；指向更早的节点 → jump-back 段整体重跑
    "loopCount": 1                  // 选填：内循环 / 回跳次数，默认 1，取值 1–20（仅在有 evaluator 或 redoTarget 时生效）
  }]
}
```

> 上例是**字段目录**（把可选字段集中展示）：实际同一 node 里 `evaluator` 与 `redoTarget` 二选一（见〈落盘校验规则〉）。

**engine + engineConfig（判别联合）**：`engine` 是判别式（tag），`engineConfig` 是引擎专属载荷，其合法字段由 `engine` 决定——这把「engine / model / effort 三者绑定」编进结构本身，三者不能各自独立填。各引擎载荷：

| engine | engineConfig 合法字段 |
| --- | --- |
| `claude-code` | `model?`（Claude 系）、`effort?`（`low`/`medium`/`high`/`xhigh`/`max`/`ultracode`/`auto`，实际随模型） |
| `antigravity` | `model?`（完整模型标签，如 `Gemini 3.5 Flash (Medium)`）；**无独立 effort 字段**——推理强度编码在模型标签后缀里（见 `docs/references/agy-print.md`） |
| `qoder` | `model?`（Qoder 模型名或档位，如 `Auto`/`Performance`，见 `--list-models`）、`reasoningEffort?`（`disabled`/`off`/`none`/`low`/`medium`/`high`/`xhigh`/`max`，与模型解耦的独立标志，见 `docs/references/qodercli-print.md`） |
| `codex`（暂时下线） | 账户欠费暂下线、下周恢复；恢复后为 `model?`（GPT 系）、`reasoningEffort?`（`low`/`medium`/`high`/`xhigh`）。`gemini` 被 `antigravity` 取代、均已移除 |

`model` 省略则用该引擎默认模型；`evaluator` 用同一套 `engine` + `engineConfig` 结构。具体校验（严格、依能力表）见〈落盘校验规则〉。

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

**展开基准值**（Python 原型实测，作为展开算法的回归锚点）：`examples/wf_autopilot.json` → **14 步**；`examples/wf_demo.json` → **4 步**（导入 store 后 `show --expand` 应复现同样步数）。

## 落盘存储结构（store 布局）

所有状态都落在固定的全局 store `~/.conduct/` 下——无数据库、纯文件，便于人肉查看、备份与版本管理；首次使用时自动创建缺失目录。布局：

```
~/.conduct/
├── workflows/                     # 工作流定义（create/edit/rename/delete/list/show 操作此处）
│   ├── autopilot.json
│   └── my-flow.json
└── runs/                          # 运行记录（workflow run 写入，run list/show 读取）
    └── autopilot-20260703-152233/ # 一次运行一个目录，目录名即 run id（形如 <workflow>-<时间戳>）
        ├── run.json               # 运行概要（宏观信息，归档 / 排查用）
        ├── run-summary.md         # 运行总结（给人 / AI 阅读的报告）
        └── trace.jsonl            # 逐步 trace，每行一步（含 input/output，失败含 error），追加写
```

四类文件：

**`workflows/<name>.json`** —— 一份完整 workflow 记录 `{ name, createdAt, updatedAt, nodes }`。字段结构见〈workflow 定义 schema〉；落盘为**规范化形态**（camelCase、补齐默认值），故 `show --json` 打印的即此文件内容。文件名 `<name>` 与内部 `name` 一致（`[A-Za-z0-9._-]+`，作主键）。

**`runs/<id>/run.json`** —— 运行概要（宏观信息，归档 / 排查用；`run` **开跑即写**（`status:"running"`）、收尾再更新为终态，`run list` / `run show` 的机读数据源）。含**开始运行那一刻冻结的 workflow 定义**——运行开始后工作流可能被编辑或删除，本记录一律以冻结快照为准，使这次运行永远可复现、可解释：

```jsonc
{
  "id": "autopilot-20260703-152233",  // run id，等于所在目录名（<workflow>-<时间戳>）
  "workflow": "autopilot",          // 所属工作流名
  "workflowSnapshot": {             // 冻结快照：开始运行时的完整 workflow 记录（含 name/createdAt/updatedAt），结构见〈workflow 定义 schema〉
    "name": "autopilot",
    "createdAt": "2026-07-03T09:12:00+08:00",
    "updatedAt": "2026-07-03T15:40:00+08:00",
    "nodes": [ /* … */ ]
  },
  "userPrompt": "给购物车加一个清空按钮",  // 本次用户需求（{{sys.userPrompt}}）
  "cwd": "/Users/me/proj",          // 引擎工作目录（--cwd）
  "status": "completed",            // running | completed | failed（interrupted 为派生态：status 仍 running 但 pid 已死）
  "pid": 48213,                     // 运行进程 PID；据此判活——status=running 但进程已死 → interrupted
  "pidStartTime": "1783263565.442591",  // 进程启动时刻令牌，与 pid 联合校验以免 pid 被无关新进程复用时误判/误杀（旧记录或不支持的平台为空，omitempty）
  "steps": 14,                      // 展开后的总步数（开跑即定；进度 = trace.jsonl 行数 / steps）
  "startedAt": "2026-07-03T15:22:33+08:00",  // RFC3339，开跑即写
  "endedAt": "2026-07-03T15:29:10+08:00",    // RFC3339，收尾写；running 时为 null
  "artifacts": {                    // 各节点最终产物：nodeId → 该 node 最后一次成功的 output（随运行推进增量写）
    "node-1": "…",
    "node-2": "…"
  },
  "failedStep": null,               // status=failed 时的失败步序（stepIndex），否则 null
  "error": null                     // status=failed 时的失败信息（汇总自 failedStep 那步 trace 的 error，便于快速排查），否则 null
}
```

**`runs/<id>/run-summary.md`** —— 运行总结，给人 / AI 阅读的 Markdown 报告，由 `run.json` 渲染而来（人类读这份、机器读 `run.json`——同一份运行记录的两副面孔）。至少含：所属工作流、用户需求、状态、开始 / 结束时间与耗时、逐步结果（每步节点 / 引擎 / 耗时）、各节点最终产物（Markdown 原文、XML 标签包裹，见下例）。`run` 结束时 stdout 指向的就是这份文件。示例（`completed` 运行；`failed` 会额外渲染失败步与 `error`）：

````markdown
# autopilot-20260703-152233

**工作流** autopilot · 4 节点
**需求** 给购物车加一个清空按钮
**状态** ✅ completed · 18.3s（2026-07-03 15:22:33 → 15:22:51）
**工作目录** /Users/me/proj

## 步骤

| # | 节点 | 引擎 | 耗时 |
| --- | --- | --- | --- |
| 0 | 规划 | claude-code | 1.2s |
| 1 | 编码 | claude-code | 8.0s |
| 2 | 编码 · 评测 | claude-code | 2.1s |
| 3 | 评审 | claude-code | 3.0s |

## 产物

<output node="plan" name="规划">
# 方案

购物车页头部新增“清空购物车”按钮，点击弹二次确认后清空 items。
</output>

<output node="code" name="编码">
```diff
--- a/src/Cart.tsx
+++ b/src/Cart.tsx
@@ export function Cart() {
+  <button onClick={() => setConfirming(true)}>清空购物车</button>
```
</output>

<output node="review" name="评审">
整体符合需求；建议空购物车时禁用该按钮，避免无效点击。
</output>
````

**`runs/<id>/trace.jsonl`** —— 逐步执行日志，[JSON Lines](https://jsonlines.org/) 格式：每行一个独立 JSON，追加写，行序即执行序。每步一条；`run --json` 逐步吐出的事件就是这些记录：

```jsonc
// 每行一条（此处换行仅为可读，实际每条压成一行）
{
  "stepIndex": 0,               // 步序，从 0 起
  "type": "agent",             // agent（节点主体）| evaluator（评测官）
  "nodeId": "node-1",          // 所属节点 id
  "displayName": "规划",         // 冗余存节点名，使本文件自解释（不依赖当时的定义）
  "iteration": 1,              // 第几轮（内循环 / 回跳时 >1）
  "engine": "claude-code",     // 该步实际调用的引擎（同定义的判别式）
  "engineConfig": {            // 该步生效的引擎配置，记节点/evaluator 的声明值，结构同定义（见〈workflow 定义 schema〉）
    "model": "claude-opus-4-8",// 声明的模型；定义省略则此字段缺省（引擎侧用其默认模型，本记录不额外探测该默认名）
    "effort": "high"           // claude-code 档位（antigravity 无、编码在 model 标签；codex 下线用 reasoningEffort）
  },
  "input": "…",                // 该步喂给引擎的完整输入（渲染后的 promptTemplate，全文不截断）
  "success": true,             // 该步是否成功
  "error": null,               // 失败（success=false）时的错误信息：引擎报错 / 退出码 / stderr 摘要，全文；成功为 null
  "output": "…",               // 该步产物全文（不截断；进度显示里的 80 字预览只是展示截断）
  "tokens": 1234,              // 选填：本步 token 消耗（引擎回报则记）
  "durationMs": 8021           // 该步耗时（毫秒）
}
```

> **运行态与中断判定**：`run.json` 开跑即写（`status:"running"`）、`trace.jsonl` 边跑边追加、`run-summary.md` 收尾才生成（故 `running` 时尚无 summary）。`run show` / `run list` 读到 `status:"running"` 时按 `pid` 判活（并核对 `pidStartTime` 启动时刻，防 pid 被无关新进程复用时误判）：进程在＝真运行中；进程已死＝ `interrupted`（崩溃 / 被强杀），尽力展示已有 trace。终态 `completed` / `failed` 以 run.json 为准。

## 落盘校验规则

无独立 `validate` 命令：下列校验由 `create` / `edit` 在**入库前强制执行**，不过即拒绝、不写盘（`create --definition` 导入、`show` / `run` 载入时也复用同一套）。承自并强化 Python 原型的 fail-loud 语义：

- **结构与类型**：`nodes` 数组存在；每个 node 的 `id` / `displayName` / `engine` / `promptTemplate` 必填；`engineConfig` 结构合法；`loopCount` 为 `1`–`20` 的整数（仅当 node 带 `evaluator` / `redoTarget` 时校验）。
- **元数据字段系统管理**：`name` 若在导入定义中出现，须等于目标名（`create` 的 `<name>` 参数 / `edit` 的目标），否则拒绝；`createdAt`（`create` 时写、此后不可变）与 `updatedAt`（`create` / `edit` 每次重戳）由系统写入，导入值一律忽略。故导入体（`create --definition` / `edit` 的 stdin）给 `nodes` 即可。改名是独立操作、走 `workflow rename`——不能靠导入体里的 `name` 与目标名不一致来触发（那一律按错误拒绝，绝不做静默改名）。
- **engine + engineConfig 严格校验（判别联合）**：`create` / `edit` 先按 `engine` 选定该引擎的**能力表**（该引擎接受的调优字段及其枚举），再校验 `engineConfig`：`engine` 合法（已注册）、调优字段属于该 `engine`（`effort` ↔ claude-code；`reasoningEffort` ↔ qoder；`antigravity` 无独立调优字段、仅认 `model`）、其值在该字段允许集内、无该引擎不认的多余字段；任一不符即拒绝（node 与其 `evaluator` 各自独立校验）。`model` 当前**不做白名单**（接受任意非空串，待有权威模型表再收紧）；能力表随引擎演进维护。`codex` 暂时下线（下周恢复）；`gemini` 被 `antigravity` 取代（见〈实现状态〉）。
- **`evaluator` 与 `redoTarget` 互斥**：同一 node 二者不可并存。
- **node `id` 合法且唯一**：`id` 须匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`——首字符为字母或下划线，其余限字母 / 数字 / 连字符 / 下划线，总长 1–64；且同一份定义内不得重复。`redoTarget` 作为对 node 的引用同样须是合法 id。（注意：这套 id 规则与工作流名 `<name>` 的 `[A-Za-z0-9._-]+` 不同——后者是 store 文件名，可含点、可数字开头。）
- **`redoTarget` 合法回跳**：必须指向一个**存在且位于本 node 之前**的节点；指向不存在的、自身、或后续节点即拒绝。
- **模板变量引用存在**：`promptTemplate`（含 `evaluator` 的）里非转义的 `{{<nodeId>}}` 必须引用定义内存在的 node id；`{{sys.*}}` 仅限已知系统变量（`sys.userPrompt` / `sys.cwd`）。

校验失败时逐条打印字段级错误（如 `nodes[0].engineConfig.effort: engine="antigravity" 不认 effort`），退出码 `1`。

> 注：这三条（`id` 唯一、`redoTarget` 合法回跳、模板引用存在）比 Python 原型更严——原型对它们分别是静默降级 / 后者覆盖 / 渲染时保留字面量；conduct 提前到入库时 fail-loud 拒绝。

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：校验失败、引擎调用失败、IO 失败、目标不存在、命名冲突（已存在）等 |
| `2` | 用法错误（缺参、非法参数、非交互拒绝危险操作）——Cobra 默认 |

校验失败并入 `1`，不单列专用码——具体原因看 stderr 的字段级报错；按命令语境即可区分（`create` / `edit` 的 `1` 多为校验或命名冲突，`run` 的 `1` 多为引擎失败）。

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `workflow run` | **已实现**（`internal/orchestrator` 主循环：展开 → 渲染 → 逐步驱动引擎 → 串联产物/反馈 → 落盘 trace；`--cwd` / `--json` / stdin 需求就位。显式 `--cwd` 现做存在性 + 目录校验，无效即退 `2`。顶层 `run` 已改作运行记录 noun） |
| `workflow create/edit/rename/delete/list/show` | **已实现**（`workflow` 命令族 + `internal/store` 托管层 + `internal/workflow` 校验/展开/渲染；`show --expand` 复用展开算法。校验内核提供 `ValidateStructured` 返回字段级 `[]Problem`，供 UI 定位错误字段） |
| `run list` / `run show` | **已实现**（读 `~/.conduct/runs/`；`show` 支持 `--trace` / `--json` 四组合；`interrupted` 按 pid 存活读时派生） |
| `run stop` | **已实现**（`internal/run.StopProcess` 先按进程组发 SIGTERM、非组长 `ESRCH` 回退单进程；仅 `running` 可终止，不落新状态、进程停写后按 pid 判活派生 `interrupted`。组信号连带引擎子进程仅在 `ui` self-exec 成组路径下真实生效，单进程回退分支经单测） |
| `ui` | **部分实现**（服务端 + `/api/*` 全端点 + self-exec 发射器就位：`internal/ui` 只绑 127.0.0.1、启动探测 store、Host/Origin 白名单、变更类强制 JSON；`conduct ui --port/--open` 已注册。handler / 预检 / run id 匹配 / 发射全链路经单测 + curl e2e。**内嵌前端 SPA 代码已落地、待浏览器走查验收**——`/` 服务 SPA 外壳（`index.html` + `js/` + `style.css`，随 go:embed 打进二进制）。self-exec 成组连带引擎子进程的组信号待真起引擎手工验） |
| `version` | 已实现 |
| 引擎 `claude-code` / `antigravity` / `qoder` | **已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，三者均经真实调用冒烟通过；单测用假二进制覆盖参数/stdin/cwd 接线与 JSON 解析） |
| 引擎 `codex` | **暂时下线**（账户欠费，下周恢复）；届时加回注册表（`internal/engine/codex.go`）与能力表 |
| 引擎 `gemini` | **已移除**：被 `antigravity` 取代（agy 取代 gemini cli） |

解释器内核（展开 `expand` + 渲染 `render` + 主循环 `orchestrator`）已全部落地。尚未做：`ui` 内嵌前端的浏览器走查验收（服务端 + API + SPA 代码均已就位）、崩溃续跑 / 超时重试 / 多模态附件（原型也刻意不做）。
