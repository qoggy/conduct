# conduct CLI 运行时命令规格

> 本文规定 conduct CLI 的**运行时命令面**——如何解释运行一份工作流、查询 / 终止运行记录，以及运行记录的数据模型与落盘结构。
>
> 工作流**定义**的创建 / 编辑 / 复制 / 改名 / 删除 / 查询与校验规则，见 [docs/specs/cli-authoring.md](./cli-authoring.md)；可视化界面 `ui`、自更新 `update`、`version` / `help` 等工具层命令，见 [docs/specs/cli-tooling.md](./cli-tooling.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

本文承接 [cli-authoring.md](./cli-authoring.md)〈设计前提〉的地基（工作流是托管对象、store 固定在 `~/.conduct/`、noun-first、双友好、UI 无独占能力）。运行时侧额外或需强调的：

- **run 是不可变历史**：一次运行落一条 run 记录，永不回改。它在**开跑那一刻冻结一份完整 workflow 快照**（`workflowSnapshot`），此后既不读、也不依赖 store 里那份活 workflow——故工作流被 `edit` / `rename` / `delete` 都不影响已有 run，历史永远可复现（详见〈数据模型〉）。
- **store 位置（固定）**：运行记录统一存放在全局 `~/.conduct/runs/<id>/`（一次运行一个目录）。所有命令固定读写此 store，**不支持自定义存储位置**（完整落盘布局见〈runs/ 落盘结构〉）。
- **命令风格 noun-first**：运行记录是 `run` 名词族（`list` / `show` / `stop`）；解释运行一份工作流的动作挂在 `workflow` 名词下（`workflow run`，语义是「运行这份定义」）——如 `gh` 里 `gh workflow run` 触发、`gh run list` 查历史，二者并存不冲突。**例外**是不针对单一资源的顶层工具命令（`conduct version` / `conduct ui` / `conduct update` / `conduct help`）——它们不属于 workflow / run 资源族，单列于 [cli-tooling.md](./cli-tooling.md)。
- **对 AI-bash 与人类双友好（北极星）**：查询类带 `--json` 机读、变更类以退出码表达成败；`conduct ui` 是 CLI 动词层的人类对等物，**无独占能力**——看状态 ↔ `run list` / `run show`、启动 ↔ `workflow run`、终止 ↔ `run stop`。

## 数据模型（运行记录实体）

conduct 有两个模型，像数据库的两张表；本文详述 **run** 表，工作流 **workflow** 表见 [cli-authoring.md](./cli-authoring.md)〈数据模型〉。

| 实体 | 是什么 | 主键 | 可变性 | 落盘 |
| --- | --- | --- | --- | --- |
| **workflow**（工作流） | 一份定义 | `name` | 可变 | `workflows/<name>.json`（详见 [cli-authoring.md](./cli-authoring.md)） |
| **run**（运行记录） | 一次执行 | run id ＝ `<workflow>-<时间戳>` | 不可变（历史） | `runs/<id>/`（run.json + run-summary.md + trace.jsonl） |

- **run id 内嵌 workflow**：目录名形如 `autopilot-20260703-152233`，id 自身即体现所属 workflow（时间戳后缀固定 `YYYYMMDD-HHMMSS`，故 workflow 前缀可无歧义还原）；权威归属另存于 run.json 的 `workflow` 字段。
- **关系是「快照」，不是「活外键」**：run 除 `workflow` 名外，还 copy 一份 `workflowSnapshot`（开始运行那一刻冻结的完整 workflow 记录）。类比订单行 copy 下单时的商品价格、而非外键到会变的现价——因为 run 是**不可变历史**，必须永远可复现。由此两条推论：
  1. **`edit` / `delete` 一个 workflow，不影响它已有的 run**：run 靠快照自解释，workflow 改了、删了，历史照样能看、能复现（`delete` 只删 `workflows/<name>.json`，`runs/` 分毫不动）。
  2. **跨表查询就是 join**：「某 workflow 的全部运行」＝按 `workflow` 过滤 run 表；「哪些 workflow 在跑」＝ run 表 filter `status:"running"`（据 `pid` 判活）再并回 workflow 表——运行态已持久化在 run.json，见〈runs/ 落盘结构〉。

## 命令总览

| 命令 | 作用 | 对应诉求 |
| --- | --- | --- |
| `conduct workflow run <name> "<需求>"` | 解释运行一份工作流 | 运行 |
| `conduct run list` | 列出历史运行记录 | 查询（运行） |
| `conduct run show <id>` | 查看某次运行的状态与详情 | 查询（运行） |
| `conduct run stop <id>` | 终止一次进行中的运行 | 终止（运行） |

> 工具层命令 `conduct version` / `conduct ui` / `conduct update` / `conduct help` 不属于 workflow / run 资源族，见 [cli-tooling.md](./cli-tooling.md)。

> 需要不运行、不花 token 地核对一份定义合不合法、并预览它会展开成什么步骤时，用 `workflow show --expand`（见 [cli-authoring.md](./cli-authoring.md)），无需真跑。

## 全局约定

**全局选项**（`-h, --help` 所有命令通用；`--version` 仅根命令）：

| 选项 | 说明 |
| --- | --- |
| `-h, --help` | 打印该命令的用法与选项后退出 `0` |
| `--version` | 仅根命令 `conduct --version`：打印版本号后退出 `0`（等价 `conduct version`；子命令不挂此旗标，与 gh / kubectl 惯例一致） |

**通用选项**（涉及结构化输出的命令支持）：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（人类可读表格 / 装饰全部关闭），便于脚本消费 |

**「出参」的含义**：本文每条命令的「输出」小节同时约定 **stdout**（正常结果）、**stderr**（错误信息）、**退出码**、以及**落盘副作用**（若有）。统一退出码见文末〈退出码约定〉。

**fail-loud 基线**：错误一律显式报出并以非 0 退出，绝不静默吞掉、绝不用空动作冒充成功（承自项目编码规范「错误不吞 / 不假装成功」）。

---

## workflow run — 运行

**用途**：解释运行一份工作流——先把节点图展开成确定性线性步骤，再逐步驱动 AI 引擎（走无头 CLI）执行，串联上游产物与评测反馈，落盘 trace。定义的展开 / 校验规则见 [cli-authoring.md](./cli-authoring.md)。

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
- `--json`：stdout **每步一行**事件 JSON，无进度装饰（无单独汇总事件，整体概要见落盘的 `run.json`）。每行即 `trace.jsonl` 的一条记录，完整字段见〈runs/ 落盘结构〉，下例仅列核心字段：

  ```json
  {"stepIndex":0,"type":"agent","nodeId":"node-1","iteration":1,"output":"...","success":true}
  ```

- 落盘副作用：在运行目录 `~/.conduct/runs/<id>/` 下——**开跑即写** `run.json`（`status:"running"`），`trace.jsonl` 逐步追加，**收尾**把 `run.json` 更新为终态并生成 `run-summary.md`；三文件结构见〈runs/ 落盘结构〉，供 `run list` / `run show` 查询。
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
✓ 1240ms tokens=3155 产物 512 字符：# 方案：购物车页头部新增"清空购物车"按钮，点击弹确认……
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

- 人类可读（默认）：打印 `run-summary.md` 全文（运行总结，见〈runs/ 落盘结构〉）；退出 `0`。运行**未收尾**（`running` / `interrupted`）时总结尚未生成，改打印状态摘要（run id / 状态 / 用户需求 / 步数 / 进度 `step k/N`）并提示用 `--trace` 查看已执行步骤。
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

- **已收尾**（completed / failed）：打印 `run-summary.md` 全文，格式见〈runs/ 落盘结构〉的 run-summary.md 示例。
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

## runs/ 落盘结构

运行记录落在固定的全局 store `~/.conduct/runs/` 下——无数据库、纯文件，便于人肉查看、备份与版本管理；首次使用时自动创建缺失目录。布局：

```
~/.conduct/
└── runs/                          # 运行记录（workflow run 写入，run list/show 读取）
    └── autopilot-20260703-152233/ # 一次运行一个目录，目录名即 run id（形如 <workflow>-<时间戳>）
        ├── run.json               # 运行概要（宏观信息，归档 / 排查用）
        ├── run-summary.md         # 运行总结（给人 / AI 阅读的报告）
        └── trace.jsonl            # 逐步 trace，每行一步（含 input/output，失败含 error），追加写
```

> 工作流定义 `~/.conduct/workflows/` 的布局见 [cli-authoring.md](./cli-authoring.md)〈workflows/ 落盘结构〉。

三类文件：

**`runs/<id>/run.json`** —— 运行概要（宏观信息，归档 / 排查用；`run` **开跑即写**（`status:"running"`）、收尾再更新为终态，`run list` / `run show` 的机读数据源）。含**开始运行那一刻冻结的 workflow 定义**——运行开始后工作流可能被编辑或删除，本记录一律以冻结快照为准，使这次运行永远可复现、可解释：

```jsonc
{
  "id": "autopilot-20260703-152233",  // run id，等于所在目录名（<workflow>-<时间戳>）
  "workflow": "autopilot",          // 所属工作流名
  "workflowSnapshot": {             // 冻结快照：开始运行时的完整 workflow 记录（含 name/createdAt/updatedAt），结构见 cli-authoring.md〈workflow 定义 schema〉
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

**`runs/<id>/run-summary.md`** —— 运行总结，给人 / AI 阅读的 Markdown 报告，由 `run.json` 渲染而来（人类读这份、机器读 `run.json`——同一份运行记录的两副面孔）。至少含：所属工作流、用户需求、状态、开始 / 结束时间与耗时、逐步结果（每步节点 / 引擎 / 耗时）、各节点最终产物（Markdown 原文、XML 标签包裹，见下例）。`run` 结束时 stdout 指向的就是这份文件。**用户需求只渲染为一行摘要**（取首行、超长截断并以 `…` 收尾）：需求可能是整份 PRD（数十 KB），整段塞进头部会淹掉步骤表与产物；被截断时附「（完整需求见 run.json）」，全文由 `run.json` 的 `userPrompt` 保留——与 `run list` 人读截断、机读留全文的同一分工。示例（`completed` 运行；`failed` 会额外渲染失败步与 `error`）：

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

购物车页头部新增"清空购物车"按钮，点击弹二次确认后清空 items。
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
  "engine": "claude-code",     // 该步实际调用的引擎（同定义的判别式；引擎层如何把它落到 CLI 调用见 engines.md）
  "engineConfig": {            // 该步生效的引擎配置，记节点/evaluator 的声明值，结构同定义（见 cli-authoring.md〈workflow 定义 schema〉、engines.md〈引擎能力表〉）
    "model": "claude-opus-4-8",// 声明的模型；定义省略则此字段缺省（引擎侧用其默认模型，本记录不额外探测该默认名）
    "effort": "high"           // claude-code 档位（antigravity 无、编码在 model 标签；qoder 用 reasoningEffort）
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

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：引擎调用失败、IO 失败、目标不存在等 |
| `2` | 用法错误（缺参、非法参数）——Cobra 默认 |

具体原因看 stderr 报错，按命令语境即可区分（`run` 的 `1` 多为引擎失败、`run show` 的 `1` 多为目标不存在）。（编辑态命令的退出码见 [cli-authoring.md](./cli-authoring.md)〈退出码约定〉，同一张表。）

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `workflow run` | **已实现**（`internal/orchestrator` 主循环：展开 → 渲染 → 逐步驱动引擎 → 串联产物/反馈 → 落盘 trace；`--cwd` / `--json` / stdin 需求就位。显式 `--cwd` 现做存在性 + 目录校验，无效即退 `2`。顶层 `run` 已改作运行记录 noun） |
| `run list` / `run show` | **已实现**（读 `~/.conduct/runs/`；`show` 支持 `--trace` / `--json` 四组合；`interrupted` 按 pid 存活读时派生） |
| `run stop` | **已实现**（`internal/run.StopProcess` 先按进程组发 SIGTERM、非组长 `ESRCH` 回退单进程；仅 `running` 可终止，不落新状态、进程停写后按 pid 判活派生 `interrupted`。组信号连带引擎子进程仅在 `ui` self-exec 成组路径下真实生效，单进程回退分支经单测） |
| 引擎 `claude-code` / `antigravity` / `qoder` | **已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，三者均经真实调用冒烟通过；单测用假二进制覆盖参数/stdin/cwd 接线与 JSON 解析） |
| 引擎 `codex` | **暂时下线**（账户欠费，下周恢复）；届时加回注册表（`internal/engine/codex.go`）与能力表 |
| 引擎 `gemini` | **已移除**：被 `antigravity` 取代（agy 取代 gemini cli） |

> 工具层命令 `version` / `ui` / `update` / `help` 的实现状态见 [cli-tooling.md](./cli-tooling.md)〈实现状态〉。

解释器内核（展开 `expand` + 渲染 `render` + 主循环 `orchestrator`）已全部落地。尚未做：崩溃续跑 / 超时重试 / 多模态附件（原型也刻意不做）。
