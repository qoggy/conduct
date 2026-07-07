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
- **命令风格 noun-first**：运行记录是 `run` 名词族（`list` / `show` / `stop` / `wait` / `rm`）；解释运行一份工作流的动作挂在 `workflow` 名词下（`workflow run`，语义是「运行这份定义」）——如 `gh` 里 `gh workflow run` 触发、`gh run list` 查历史，二者并存不冲突。**例外**是不针对单一资源的顶层工具命令（`conduct version` / `conduct ui` / `conduct update` / `conduct help`）——它们不属于 workflow / run 资源族，单列于 [cli-tooling.md](./cli-tooling.md)。
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
| `conduct workflow run <name> "<需求>" [-d]` | 解释运行一份工作流（`-d` 后台起、返回 run id 即脱手） | 运行 |
| `conduct run list [--status <state>]` | 列出历史运行记录（可按状态过滤） | 查询（运行） |
| `conduct run show <id>` | 查看某次运行的状态与详情 | 查询（运行） |
| `conduct run stop <id>` | 终止一次进行中的运行 | 终止（运行） |
| `conduct run wait <id>` | 阻塞等待一次运行到终态，退出码即成败 | 编排（运行） |
| `conduct run rm <id>` | 删除一条运行记录 | 清理（运行） |

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
conduct workflow run <name> ["<用户需求>"] [--cwd <dir>] [-d | --detach] [--json]
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
| `-d, --detach` | 布尔 | `false` | 后台起跑：预检通过后以独立会话（`setsid`）spawn 子进程跑工作流，父进程返回 run id 后退 `0`，不阻塞到运行结束（行为详见〈后台运行（`-d` / `--detach`）〉） |

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
- 任一步引擎调用失败：人读模式下该步在 stdout 打一行 `✗ <耗时>ms 失败：<错误摘要>` 进度标记（与成功的 `✓` 对称）；**权威错误另走 stderr**（含引擎名、退出码、报错摘要）；该步完整错误写入 trace 对应行的 `error` 字段，`run.json` 记 `status:"failed"` 与失败步号 `failedStep`（指针，错误详情看该步 trace）；已完成步骤的 trace 保留；退出 `1`。
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

### 后台运行（`-d` / `--detach`）

长工作流一次运行动辄数十分钟，前台阻塞会独占终端；对调度它的上层 agent 而言，要的是「提交即返回句柄」而非逐步直播。`-d` 让 `workflow run` 以独立会话在后台起跑，**父进程把 run id 立刻还回并退 `0`**，之后凭 id 用 `run show` / `run wait` / `run stop` 查、等、停。语义对标 `docker run -d`。

**行为约定**：

1. **该同步失败的先同步失败（fail-loud，不带病 detach）**：前台跑会做的全部预检——`<name>` 合法且定义存在、载入校验通过、`<用户需求>` 非空（位置参数或非 TTY stdin，皆无且 stdin 是终端则退 `2`）、`--cwd` 存在且是目录（退 `2`）、能成功展开——**都在父进程同步做完**；用法错误退 `2`、载入 / IO 错误退 `1`，**绝不 detach 之后再在后台静默失败**（对标 `docker run` 找不到镜像时先报错、不后台起）。
2. **stdin 需求在 fork 前读完**：`<用户需求>` 来自管道 stdin 时，父进程在 spawn 前 `ReadAll` 整个 stdin，再经子进程 stdin 管道喂入——子进程已脱离终端、读不到发起方的 stdin。
3. **以独立会话 spawn 一个前台子进程**：父进程 self-exec 出 `conduct workflow run <name> --cwd <dir>`（**普通前台子进程、不再带 `-d`**，故不递归），`SysProcAttr{Setsid: true}` 起新会话，彻底脱离发起方的终端会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP。子进程 stdout 重定向 `/dev/null`（进度已逐步落 `trace.jsonl`，且**绝不能用 pipe**——父进程先退会令子进程写 stdout 触发 EPIPE→SIGPIPE 反被杀），stderr 重定向会话私有临时文件、兜底「写 `run.json` 之前就失败」的罕见路径。此发射器即 `conduct ui` 已在用的 self-exec + setsid 发射路径（见 [ui.md](./ui.md)〈启动运行机制〉）；已把它从 `ui` 私有抽成 `internal/launch`，`-d` 与 UI 共用同一条路径。
4. **等初始 `run.json` 落定再交回 id**：为保证「打印了 id 就一定能被 `run list` / `run show` 查到」，父进程在**有界短等待**内轮询运行列表，按「workflow 名 + 子进程 pid + `startedAt` ≥ spawn−时钟余量」组合条件锁定刚起的这次运行，命中即打印 id 并退 `0`——这是一个亚秒级的短等待，不是等整个 run 跑完。子进程随后独立跑到终态，正常落 `trace.jsonl` 与 `run-summary.md`。

**输出**：

- 人类可读（默认）：stdout 打印 run id 与一行提示，退 `0`：

  ```
  已在后台启动 autopilot-20260703-152233；conduct run show autopilot-20260703-152233 查看进度、conduct run stop autopilot-20260703-152233 终止。
  ```

- `--json`：打印**单行句柄 JSON**（机读 handle），退 `0`：

  ```json
  {"id":"autopilot-20260703-152233","workflow":"autopilot"}
  ```

  句柄只含 `id` 与 `workflow`——它是**可寻址句柄**、不是状态快照。句柄产出的那一刻 run 未必仍是 `running`（引擎可能已秒级失败 / 完成），故**不塞一个恒为 `running` 的误导字段**；run 的真实成败用 `conduct run wait <id>` / `conduct run show <id>` 查。
  **注意**：`-d --json` 吐的是这一行句柄、**不是**前台 `--json` 的逐步事件流；逐步事件在 `trace.jsonl`，用 `conduct run show <id> --json --trace` 取。
- 有界等待内未能确认 run id（无论子进程是否仍存活）：以退 `1` 报「未取得可用句柄」，stderr 提示子进程可能仍在跑、请用 `run list` 核对。**由此保证 `-d` 退 `0` ⟺ stdout 已打印 run id**——机读 `… -d --json | jq -r .id` 不会拿到空 id 却见「成功」。（这是相对 `conduct ui`「已发射未确认按成功回执」的有意收紧：CLI 退出码是机器契约，给不出句柄就不算发射成功。）
- fork / setsid 发射失败：stderr 回传子进程 stderr 摘要；退 `1`。

**退出码（语义分层）**：`-d` 的退出码只表达**发射成不成功**，不表达 run 本身跑得成不成功——正如 `docker run -d` 退 `0` 只代表容器起来了、不代表里面进程最终成功。run 的成败去 `run show <id>` 看 `status`（或 `run wait <id>` 等它跑完、再读 `status`，见〈run wait〉）。

| 码 | 含义 |
| --- | --- |
| `0` | 后台子进程已起、初始 `run.json` 已确认，**run id 已打印到 stdout** |
| `1` | 载入 / IO 失败，fork / setsid 发射失败，或有界等待内未能确认 run id（子进程可能仍在跑，去 `run list` 核对） |
| `2` | 用法错误（缺需求、`--cwd` 非法等）——与前台同一套预检 |

前台（不加 `-d`）跑的退出码仍表达**整趟运行**的成败，不变。

**示例**：

```bash
# 后台起跑，拿到 id 立刻脱手
conduct workflow run autopilot "重构结算流程为状态机" -d

# 机读句柄：抽出 run id 供后续编排
id=$(conduct workflow run autopilot "实现 X" -d --json | jq -r .id)
conduct run wait "$id" && conduct run show "$id"   # 等它收尾再读总结
```

**附带收益**：`-d` 让 CLI 起的 run 也 `setsid` 成进程组组长，`run stop` 的进程组连带信号对它**可靠生效**（终端里前台管道启动的 run 因非组长只能回退单进程，见〈run stop — 终止运行〉的诚实边界）；同时补平「UI 能后台起 run、CLI 不能」这处 UI 独占，兑现〈设计前提〉UI↔CLI 能力对等的北极星。

**诚实边界**：`-d` 只是启动模式——run 记录三件套、`status` 语义、`run list` / `run show` / `run stop` 一律不变，后台跑与前台跑落盘逐字节同构。**不提供** `--follow` / 逐步实时直播（对标 `docker logs -f`）：那是给人看的诉求，交给 `conduct ui` 或 `run show` 轮询；`-d` 刻意不碰，以免范围蔓延。

---

## run list — 运行历史（列表）

**用途**：列出历史运行记录（`~/.conduct/runs/` 下每个运行目录一条）。

**用法**：

```
conduct run list [--status <state>] [--json]
```

**参数**：无。

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--status <state>` | 枚举 | 空（列全部） | 只列指定状态的运行；取值 `running` / `completed` / `failed` / `interrupted`。过滤按**派生态**计（`running` 只留 pid 真存活的、`interrupted` 只留已崩溃的，与 `run show` 判活同逻辑）。非法取值报用法错误退 `2` |

**输出**：

- 人类可读（默认）：表格，列为 `RUN ID | WORKFLOW | STATUS | STEPS | STARTED | PROMPT`——run id（目录名，形如 `<workflow>-<时间戳>`）、所属工作流名、状态（`running` / `completed` / `failed` / `interrupted`）、步数、开始时间、用户需求（`userPrompt` 截断至约 20 字、超出以 `…` 收尾——整份 PRD 这类长输入也不会撑爆表格）；按时间倒序；退出 `0`。
- `--json`：数组，每项 `{"id","workflow","status","steps":<数>,"startedAt":"<RFC3339>","userPrompt":"<完整、不截断>"}`（机读给全文，截断只发生在人类表格）。
- `--status <state>`：仅保留派生态等于 `<state>` 的记录，其余列 / 格式 / 排序不变；未指定则列**全部**（含已完成 / 失败 / 中断）。
- 无运行记录（或过滤后为空）：stdout 提示 `（暂无运行记录）`；退出 `0`。

> **与 `docker ps` 的默认差异**：`docker ps` 默认只列运行中、`-a` 才列全部；`conduct run list` 默认列**全部**——不因新增过滤而改既有默认，避免破坏已有脚本。要「只看在跑的」用 `run list --status running`，即 `docker ps` 的等价物。

**示例**：

```bash
conduct run list
conduct run list --status running               # 只看仍在跑的（docker ps 等价）
conduct run list --status failed --json | jq '.[].id'
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

  人类 `--trace` 视图里，某步若记有 `sessionId`（引擎回报的会话/线程 id，见〈runs/ 落盘结构〉），在该步 input 前附一行会话信息 + 该引擎的回放命令（`claude-code`→`claude -r <id>`、`codex`→`codex resume <id>`、`qoder`→`qodercli -r <id>`、`antigravity`→`agy --conversation <id>`；未知引擎只显示 id）；`--json` 视图经 `trace` 数组的 `sessionId` 字段带出。

- 运行态：`status:"running"` 且 `pid` 存活 → 显示实时进度；`status:"running"` 但 `pid` 已死 → 标 `interrupted`、尽力展示已有 trace；退出 `0`。
- `<id>` 不存在：stderr `<id>: 运行不存在`；退出 `1`。

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

**诚实边界（进程组连带的适用范围）**：只有以独立会话（`setsid`）发射的 run——`conduct ui` 启动的、或 `conduct workflow run -d`（`--detach`）起的——才保证 pid 即组长、组信号能一并收割引擎子进程。终端里前台 `cat req | conduct workflow run`（未加 `-d`）这类**管道启动**的 run，conduct 不是组长，`kill(-pid)` 得 `ESRCH` → 回退单进程：只终止编排器本身，当前正在跑的引擎子进程会遗留到本步自然结束（编排器已死、不再驱动下一步）——可接受的降级。

---

## run wait — 阻塞等待运行终态

**用途**：阻塞到指定运行到达终态即返回——供「detach 出去、但某一步仍想 await 它跑完再继续」的编排（对标 `docker wait` / Unix `wait`）。`wait` 的本职只是**等到运行结束**：等到了就算完成、退 `0`；至于结束时是 `completed` / `failed` / `interrupted`（run 的成败）**不进 `wait` 的退出码**，去 stdout 摘要 / `--json` 的 `status` 看。轮询 `run show` 看 `status` 已能覆盖，`wait` 把它收敛成一条命令。

**用法**：

```
conduct run wait <id> [--json]
```

**参数**：`<id>`（必填）——run id（`run list` 里的 `RUN ID`）。

**行为与输出**：

- 目标已是终态（`completed` / `failed`，或 `running` 但 pid 已死＝派生 `interrupted`）：立即返回，不空等。
- 目标仍在跑（`running` 且 pid 存活）：**周期性轮询** `run.json` 与 pid 存活，直到转终态才返回；**无墙钟超时**（run 跑多久就等多久，对标 `docker wait` 阻塞到底）。等待期间进程崩溃（pid 死）即派生 `interrupted`，也算「等到了终态」。
- 等到**任一终态**（`completed` / `failed` / `interrupted`）：这就是 `wait` 完成本职，退 `0`。人类可读（默认）stdout 打印一行终态摘要（run id · 状态 · 耗时）；`--json` 打印收尾时 `run.json` 的规范化内容（同 `run show <id> --json`）。run 的成败在这行 `status` 里，不进退出码、也不额外打 stderr。
- 命令自身没干成才非 `0`：`<id>` 不存在 / IO 失败 → stderr `<id>: 运行不存在`（或 IO 原因）、退 `1`；缺 `<id>` / `<id>` 非法 → 退 `2`。

**退出码（表达 `wait` 有没有等成功，不表达 run 的成败）**：

| 码 | 含义 |
| --- | --- |
| `0` | 成功等到终态（`completed` / `failed` / `interrupted` 一视同仁）——`wait` 完成本职 |
| `1` | 命令自身出错：`<id>` 不存在 / IO 失败 |
| `2` | 用法错误（缺 `<id>`、`<id>` 非法） |

> **与 `docker wait` 同理**：`docker wait` 把容器退出码写 **stdout**、命令自身等待成功即退 `0`；conduct `run wait` 一样——run 的成败放在 stdout 摘要 / `--json` 的 `status`，命令退出码只表达「有没有成功等到终态」。所以别用退出码判 run 成败，那要读 `status`（`run wait <id> --json | jq -r .status`，取值 `completed` / `failed` / `interrupted`）。

**示例**：

```bash
id=$(conduct workflow run propose-apply "实现 X" -d --json | jq -r .id)
# ……去干别的……
conduct run wait "$id" && conduct run show "$id"   # 等它跑完再读总结（wait 等到即退 0，&& 继续）
# 要按成败分支，读 status、别看退出码：
[ "$(conduct run wait "$id" --json | jq -r .status)" = completed ] && echo 成功 || echo 没成功
```

---

## run rm — 删除运行记录

**用途**：删除一条历史运行记录（清掉 `runs/<id>/` 整个目录），供长期使用后清理（对标 `docker rm`）。run 是不可变历史，本命令不改写、只**整条移除**。默认在交互终端下二次确认；非交互环境必须显式 `--yes`，避免脚本误删。

**用法**：

```
conduct run rm <id> [-y | --yes] [--json]
```

**参数**：`<id>`（必填）——run id（`run list` 里的 `RUN ID`）。

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `-y, --yes` | 布尔 | `false` | 跳过确认直接删除 |

**行为与输出**：

- 目标是终态（`completed` / `failed`）或已 `interrupted`：删除 `runs/<id>/`；stdout `✓ 已删除 <id>`，退出 `0`；`--json` 输出 `{"deleted":["<id>"]}`。
- 目标仍在跑（`running` 且 pid 存活）：**拒绝删除**（不删正在写盘的活运行的记录，以免留下孤儿进程与半截目录）；stderr 提示先 `conduct run stop <id>` 终止再删；退出 `1`。
- 交互终端（TTY）且未加 `--yes`：提示 `确认删除运行 <id>？[y/N] `，仅输入 `y` / `yes`（大小写不敏感）才删除；其余（含直接回车）视为取消——**stderr** `已取消`、不删除、退出 `0`（取消提示走 stderr，保 stdout 只承载数据、`--json` 不被污染）。
- 非交互（非 TTY）且未加 `--yes`：stderr `拒绝在非交互环境删除，请加 --yes`；退出 `2`（用法错误）；不删除。
- `<id>` 不存在：stderr `<id>: 运行不存在`；退出 `1`。
- `<id>` 非法（空 / 含路径分隔符等，`run.ValidateID` 不过）：stderr 报用法错误；退出 `2`（发射前拦下）。
- 落盘副作用：删除 `runs/<id>/`（该条运行记录三件套连同目录一并移除；其它运行分毫不动——各 run 靠自身快照自解释，互不依赖）。

> **与 `docker rm` 的差异**：只接受**单个** id（不批量、不通配）、**拒删在跑的 run**（无 `-f` 强删，须先 `run stop`）、且默认二次确认（`--yes` 跳过）——比 `docker rm` 保守，因为 run 是烧过 token 的不可变历史，误删不可逆。

**示例**：

```bash
conduct run rm demo-20260703-160140
conduct run rm demo-20260703-160140 --yes
```

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
    "effort": "high"           // claude-code 档位（antigravity 无、编码在 model 标签；qoder / codex 用 reasoningEffort）
  },
  "input": "…",                // 该步喂给引擎的完整输入（渲染后的 promptTemplate，全文不截断）
  "success": true,             // 该步是否成功
  "error": null,               // 失败（success=false）时的错误信息：引擎报错 / 退出码 / stderr 摘要，全文；成功为 null
  "output": "…",               // 该步产物全文（不截断；进度显示里的 80 字预览只是展示截断）
  "tokens": 1234,              // 选填：本步 token 消耗（引擎回报则记）
  "sessionId": "0199a213-…",  // 选填：该步引擎的会话/线程 id（引擎回报则记）；凭它用引擎自带工具回放本步（见 engines.md〈引擎抽象〉RunResult.SessionID）
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

**`-d` 与 `run wait` 的退出码都不表达 run 的成败**（各命令小节已详述）：`workflow run -d` 只表达**发射**成没成、`run wait` 只表达**有没有等到终态**；两者的 run 成败一律去 `run show` / `run wait --json` 的 `status` 看，不进退出码。这与 `docker run -d` / `docker wait` 一致——退出码说的是「命令这一步成没成」，不是「里头的 run 最终成没成」。

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `workflow run` | **已实现**（`internal/orchestrator` 主循环：展开 → 渲染 → 逐步驱动引擎 → 串联产物/反馈 → 落盘 trace；`--cwd` / `--json` / stdin 需求就位。显式 `--cwd` 现做存在性 + 目录校验，无效即退 `2`。顶层 `run` 已改作运行记录 noun） |
| `run list` / `run show` | **已实现**（读 `~/.conduct/runs/`；`show` 支持 `--trace` / `--json` 四组合；`interrupted` 按 pid 存活读时派生） |
| `run stop` | **已实现**（`internal/run.StopProcess` 先按进程组发 SIGTERM、非组长 `ESRCH` 回退单进程；仅 `running` 可终止，不落新状态、进程停写后按 pid 判活派生 `interrupted`。组信号连带引擎子进程仅在 `setsid` 成组路径下真实生效——`ui` 与 `workflow run -d` 皆经共用 `internal/launch` 成组，单进程回退分支经单测） |
| `workflow run -d`（`--detach`） | **已实现**（self-exec + setsid 发射器已从 `ui` 私有抽为 CLI `-d` 与 UI 共用的 `internal/launch`；父进程同步预检复用前台的 `resolveUserPrompt` / `resolveCwd` / 载入校验，非 UI 那套 HTTP 味 preflight。有界等待确认初始 `run.json` 后打印 run id 退 `0`，确认不了退 `1`；`--json` 吐单行句柄） |
| `run list --status` | **已实现**（在既有 `run list` 上加状态过滤，按派生态计，非法取值退 `2`；默认仍列全部，不改既有默认） |
| `run wait` | **已实现**（`internal/cli` 轮询 `run.json` + pid 判活到终态；等到任一终态即退 `0`（对标 docker wait），run 成败在 stdout / `--json` 的 `status`，命令自身出错（不存在 / IO）→`1`、用法错→`2`） |
| `run rm` | **已实现**（删 `runs/<id>/` 整个目录，`-y/--yes` 守卫、拒删在跑的 run、非法 id 退 `2`、不存在退 `1`；确认约定同 `workflow delete`，见 [cli-authoring.md](./cli-authoring.md)〈workflow delete〉） |
| 引擎 `claude-code` / `antigravity` / `qoder` | **已实装**（无头 CLI `claude -p` / `agy -p` / `qodercli -p`，三者均经真实调用冒烟通过；单测用假二进制覆盖参数/stdin/cwd 接线与 JSON 解析） |
| 引擎 `codex` | **已实装**（`internal/engine/codex.go` 注册、能力表含 `codex` 行；契约见 [engines.md](./engines.md)〈codex〉。codex 输出为 JSONL 事件流，逐行扫描按 type 归一化，单测覆盖各路径） |
| 每步 `sessionId` | **已实装**（四引擎从各自 JSON 输出的会话 id 填充 `RunResult.SessionID`，编排器写入该步 `trace.jsonl` 的 `sessionId`；`run show --trace` 在该步 input 前附会话行 + 回放命令，`--json` 经 trace 数组带出。见 [engines.md](./engines.md)〈引擎抽象〉） |

> 工具层命令 `version` / `ui` / `update` / `help` 的实现状态见 [cli-tooling.md](./cli-tooling.md)〈实现状态〉。

解释器内核（展开 `expand` + 渲染 `render` + 主循环 `orchestrator`）已全部落地。尚未做：崩溃续跑 / 超时重试 / 多模态附件（原型也刻意不做）。
