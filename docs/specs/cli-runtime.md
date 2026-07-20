# conduct CLI 运行时命令规格

> 本文规定 conduct CLI 的**运行时命令面**——如何按 DAG 依赖并行运行一份工作流、查询 / 终止运行记录，以及运行记录的数据模型与落盘结构。
>
> 工作流**定义**的创建 / 编辑 / 复制 / 改名 / 删除 / 查询与校验规则，见 [docs/specs/cli-authoring.md](./cli-authoring.md)；可视化界面 `ui`、自更新 `update`、`version` / `help` 等工具层命令，见 [docs/specs/cli-tooling.md](./cli-tooling.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

本文承接 [cli-authoring.md](./cli-authoring.md)〈设计前提〉的地基（工作流是节点 + 边的 DAG、是托管对象、store 固定在 `~/.conduct/`、noun-first、双友好、UI 无独占能力）。运行时侧额外或需强调的：

- **按 DAG 依赖并行调度**：运行一份工作流＝按边的依赖关系调度节点——`START` 在 t0 即「完成」，**以 `START` 为唯一前驱的节点**同刻开跑（一开始就能并行）；一个节点的全部前驱都成功后它才就绪开跑；`END` 的全部前驱完成即到达终点。无循环，每个 agent 节点在一趟成功运行里**至多执行一次**（失败 drain 下未派发者执行 0 次，跨 resume 补跑亦至多成功一次）。无并发上限（不设 `--max-parallel`，见〈workflow run〉）。
- **run 的身份与输入快照稳定，执行记录按生命周期更新**：一次运行的 run id、所属 workflow 名、用户需求、cwd 与开跑时冻结的完整 `workflowSnapshot` 不变；`status` / `pid` / `endedAt` / `artifacts` / `error` / `failedNodeId` 和 trace 会随执行推进更新，`resume` 还会把 `failed` / `interrupted` 重新置为 `running` 并续写同一记录。活 workflow 后续被 `edit` / `rename` / `delete` 都不影响冻结快照，历史仍可解释、可复现（详见〈数据模型〉）。
- **store 位置（固定）**：运行记录统一存放在全局 `~/.conduct/runs/<id>/`（一次运行一个目录）。所有命令固定读写此 store，**不支持自定义存储位置**（完整落盘布局见〈runs/ 落盘结构〉）。
- **命令风格 noun-first**：运行记录是 `run` 名词族（`list` / `show` / `stop` / `wait` / `rm` / `resume`）；解释运行一份工作流的动作挂在 `workflow` 名词下（`workflow run`）——如 `gh` 里 `gh workflow run` 触发、`gh run list` 查历史，二者并存不冲突。**例外**是不针对单一资源的顶层工具命令（`conduct version` / `conduct ui` / `conduct update` / `conduct help`），单列于 [cli-tooling.md](./cli-tooling.md)。
- **对 AI-bash 与人类双友好（北极星）**：查询类带 `--json` 机读、变更类以退出码表达成败；`conduct ui` 是 CLI 动词层的人类对等物，**无独占能力**——看状态 ↔ `run list` / `run show`、启动 ↔ `workflow run`、终止 ↔ `run stop`。

## 数据模型（运行记录实体）

conduct 有两个模型，像数据库的两张表；本文详述 **run** 表，工作流 **workflow** 表见 [cli-authoring.md](./cli-authoring.md)〈数据模型〉。

| 实体 | 是什么 | 主键 | 可变性 | 落盘 |
| --- | --- | --- | --- | --- |
| **workflow**（工作流） | 一份 DAG 定义 | `name` | 可变 | `workflows/<name>.json`（详见 [cli-authoring.md](./cli-authoring.md)） |
| **run**（运行记录） | 一次执行 | run id ＝ `<workflow>-<时间戳>` | id / 冻结输入不变；执行状态与记录可更新、可恢复 | `runs/<id>/`（run.json + run-summary.md + trace.jsonl） |

- **run id 内嵌 workflow**：目录名形如 `autopilot-20260703-152233`，id 自身即体现所属 workflow（时间戳后缀固定 `YYYYMMDD-HHMMSS`，故 workflow 前缀可无歧义还原）；权威归属另存于 run.json 的 `workflow` 字段。
- **关系是「快照」，不是「活外键」**：run 除 `workflow` 名外，还 copy 一份 `workflowSnapshot`（开始运行那一刻冻结的完整 workflow 记录，含 `name` / 时间戳与 `definition`（`{nodes, edges}`））。类比订单行 copy 下单时的商品价格、而非外键到会变的现价——run 的执行状态虽会推进，但冻结输入不能漂移，才能复现。由此两条推论：
  1. **`edit` / `delete` 一个 workflow，不影响它已有的 run**：run 靠快照自解释，workflow 改了、删了，历史照样能看、能复现（`delete` 只删 `workflows/<name>.json`，`runs/` 分毫不动）。
  2. **跨表查询就是 join**：「某 workflow 的全部运行」＝按 `workflow` 过滤 run 表；「哪些 workflow 在跑」＝ run 表 filter `status:"running"`（据 `pid` 判活）再并回 workflow 表——运行态已持久化在 run.json，见〈runs/ 落盘结构〉。

## 命令总览

| 命令 | 作用 | 对应诉求 |
| --- | --- | --- |
| `conduct workflow run <name> "<需求>" [-d]` | 按 DAG 并行运行一份工作流（`-d` 后台起、返回 run id 即脱手） | 运行 |
| `conduct run list [--status <state>]` | 列出历史运行记录（可按状态过滤） | 查询（运行） |
| `conduct run show <id>` | 查看某次运行的状态与详情 | 查询（运行） |
| `conduct run stop <id>` | 终止一次进行中的运行 | 终止（运行） |
| `conduct run wait <id>` | 阻塞等待一次运行到终态，退出码只表示是否等到终态（run 成败看 `status`） | 编排（运行） |
| `conduct run resume <id> [-d]` | 从中断处恢复一次未完成的运行（`failed` / `interrupted`，跳过已成功节点续跑；`-d` 后台起） | 恢复（运行） |
| `conduct run rm <id>` | 删除一条运行记录 | 清理（运行） |

> 工具层命令 `conduct version` / `conduct ui` / `conduct update` / `conduct help` 不属于 workflow / run 资源族，见 [cli-tooling.md](./cli-tooling.md)。

> 需要不运行、不花 token 地核对一份定义合不合法、并预览它的拓扑分层（同层可并行）时，用 `workflow show --expand`（见 [cli-authoring.md](./cli-authoring.md)），无需真跑。

## 全局约定

**全局选项**（`-h, --help` 所有命令通用；`--version` 仅根命令）：

| 选项 | 说明 |
| --- | --- |
| `-h, --help` | 打印该命令的用法与选项后退出 `0` |
| `--version` | 仅根命令 `conduct --version`：打印版本号后退出 `0`（等价 `conduct version`；子命令不挂此旗标，与 gh / kubectl 惯例一致） |

**界面语言**：CLI 面向人的产品文案（包括 help、用法/领域错误、校验、确认、成功/警告、空态和人读输出）统一按 `~/.conduct/settings.json` 的 `language` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 英文解析。设置文件或属性缺失时才继续读取环境变量；设置无法读取、JSON 损坏或值非法时以固定英文技术诊断报错退出。高优先级环境变量非空但无法识别时直接使用英文，不再读取低优先级变量。不提供应用专属语言环境变量或 `--lang` 参数。底层技术诊断固定英文，机器协议与用户/引擎原文不翻译。每次 run 在开始时快照解析后的语言，`run-summary.md` 与 resume 沿用该快照；完整规则见 [i18n.md](./i18n.md)。

**通用选项**（涉及结构化输出的命令支持）：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--json` | 布尔 | `false` | 以机器可读 JSON 输出（人类可读表格 / 装饰全部关闭），便于脚本消费 |

**「出参」的含义**：本文每条命令的「输出」小节同时约定 **stdout**（正常结果）、**stderr**（错误信息）、**退出码**、以及**落盘副作用**（若有）。统一退出码见文末〈退出码约定〉。

**fail-loud 基线**：错误一律显式报出并以非 0 退出，绝不静默吞掉、绝不用空动作冒充成功（承自项目编码规范「错误不吞 / 不假装成功」）。

**「节点」是运行的基本单位**：一次运行由若干 agent 节点组成（`START` / `END` 是标记节点、不执行、不产 trace）。进度记为 `节点 k/N`——分母 `N` = agent 节点数（`len(definition.nodes) - 2`），分子 `k` = 已成功的**唯一** agent 节点数（并行下按 nodeId 去重，见〈run resume〉进度去重说明）。

---

## workflow run — 运行

**用途**：按 DAG 依赖并行运行一份工作流——以 `START` 为唯一前驱的节点在 t0 同刻开跑，每个节点的全部前驱成功后它才就绪、随即驱动 AI 引擎（走无头 CLI）执行，产物按 `{{<nodeId>}}` 串给下游，逐节点落盘 trace。校验规则见 [cli-authoring.md](./cli-authoring.md)〈落盘校验规则〉，并行调度语义见本文〈设计前提〉。

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

> **无 `--max-parallel`**：并行度不设上限——就绪节点一律开跑，受 DAG 结构自然约束。并行节点共享同一个 `--cwd`，若多个会写盘的引擎同刻改同一份工作树会互相踩踏；conduct **不做工作树隔离**，这属提示词设计范畴（并行分支应是互不冲突的任务，或在提示词里让各分支 AI 各自 `git worktree`），详见 `conduct help prompts`。

**用户需求的来源（按优先级）**：① 命令行位置参数 `<用户需求>`；② 省略它、且 stdin 是管道 / 重定向（非 TTY）时，读取**整个 stdin** 作为需求（如 `cat req.txt | conduct workflow run <name>`）。二者皆无、stdin 又是终端时，报参数缺失并退出 `2`，**不静默挂起等待输入**。

**给引擎看图片**：需求文本里直接写图片的**本地绝对路径**即可——已验证支持该方式的引擎会用自身文件工具读取图片。conduct **不提供**专门的图片旗标、也不做 URL 下载；当前验证清单、细节与边界见 [engines.md](./engines.md)〈图片输入〉。

**输出**：

- 人类可读（默认）：**事件流**逐行滚动打印节点生命周期，并行节点的事件交错呈现——
  1. 先打印 `▶ 调度 N 个节点` 概述（`START` 扇出几个即几个同刻开跑）；
  2. 每个节点开跑打印 `▶ <id> [<displayName>] 开跑 · engine=<e>`；完成打印 `✓ <id> 完成 · <耗时> [· tokens=<n>] · 产物 <len> 字符：<前 80 字预览>`（引擎未提供 usage 时省略整个 token 片段；已知 `0` 显示 `tokens=0`）；失败打印 `✗ <id> 失败 · <耗时> · <错误摘要>`；
  3. 结束打印 `✅ 完成，阅读 <run-summary.md 路径> 获取运行详情`；
  4. 退出 `0`。
- `--json`：stdout **每节点落定一行**事件 JSON，无进度装饰（无单独汇总事件，整体概要见落盘的 `run.json`）。每行即 `trace.jsonl` 的一条记录（按**完成序**输出，非拓扑序），完整字段见〈runs/ 落盘结构〉，下例仅列核心字段：

  ```json
  {"nodeId":"a","displayName":"调研现状","engine":"kiro","success":true,"output":"...","tokens":null,"sessionId":null,"startedAt":"2026-07-13T16:00:00+08:00","endedAt":"2026-07-13T16:00:40+08:00","durationMs":40000}
  ```

- 落盘副作用：在运行目录 `~/.conduct/runs/<id>/` 下——**开跑即写** `run.json`（`status:"running"`），`trace.jsonl` 每节点落定即追加，**收尾**把 `run.json` 更新为终态并生成 `run-summary.md`；三文件结构见〈runs/ 落盘结构〉，供 `run list` / `run show` 查询。
- 某节点引擎调用失败（**drain 语义**）：失败置位后**不再调度新节点**，但让**在途节点跑完**（各自落 trace、成功产物照记进 `artifacts`），直到在途清空才整体收尾 `failed`。人读模式下失败节点打一行 `✗ <id> 失败 …`；**权威错误另走 stderr**（含引擎名、退出码、报错摘要）；该节点完整错误写入其 trace 记录的 `error` 字段，`run.json` 记 `status:"failed"` 并把首个失败节点的错误摘要落进 `error`；已完成节点的 trace 与产物保留（resume 无需重跑）；退出 `1`。
- `<name>` 不存在，或定义载入 / 校验不过：stderr 报错（校验类打印字段级错误）；退出 `1`（发射前拦下，不烧引擎；`-d` 预检同此）。
- 同一 workflow 在同一秒内再次启动：run id 仅精确到秒，第二次创建命中既有 `runs/<id>/` 时 stderr 报 `运行已存在`，退出 `1`，不覆盖已有 run。不同 workflow 同秒启动不会冲突。
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

# 机器可读逐节点事件
conduct workflow run autopilot "..." --json | jq -c 'select(.success)'
```

示意输出（人类可读，`START` 扇出 a、b 并行）：

```
▶ 调度 4 个节点（START 扇出：a、b 同刻开跑）
▶ a [调研现状] 开跑 · engine=claude-code
▶ b [起草方案] 开跑 · engine=qoder
✓ a 完成 · 40s · tokens=1100 · 产物 512 字符：# 现状调研：购物车页头部无清空入口……
✓ b 完成 · 90s · tokens=2880 · 产物 640 字符：# 交付方案：拆成接口/前端/验收三块……
▶ c [实现] 开跑 · engine=claude-code
✓ c 完成 · 8s · tokens=12040 · 产物 2048 字符：diff --git a/src/Cart.tsx b/src/Cart.tsx……
▶ d [验收测试] 开跑 · engine=claude-code
✓ d 完成 · 3s · tokens=4200 · 产物 900 字符：三条 e2e 全绿……
✅ 完成，阅读 ~/.conduct/runs/autopilot-20260703-152233/run-summary.md 获取运行详情。
```

### 后台运行（`-d` / `--detach`）

长工作流一次运行动辄数十分钟，前台阻塞会独占终端；对调度它的上层 agent 而言，要的是「提交即返回句柄」而非逐节点直播。`-d` 让 `workflow run` 以独立会话在后台起跑，**父进程把 run id 立刻还回并退 `0`**，之后凭 id 用 `run show` / `run wait` / `run stop` 查、等、停。语义对标 `docker run -d`。

**行为约定**：

1. **该同步失败的先同步失败（fail-loud，不带病 detach）**：前台跑会做的全部预检——`<name>` 合法且定义存在、载入校验通过、`<用户需求>` 非空（位置参数或非 TTY stdin，皆无且 stdin 是终端则退 `2`）、`--cwd` 存在且是目录（退 `2`）、能成功构建调度图——**都在父进程同步做完**；用法错误退 `2`、载入 / IO 错误退 `1`，**绝不 detach 之后再在后台静默失败**（对标 `docker run` 找不到镜像时先报错、不后台起）。
2. **stdin 需求在 fork 前读完**：`<用户需求>` 来自管道 stdin 时，父进程在 spawn 前 `ReadAll` 整个 stdin，再经子进程 stdin 管道喂入——子进程已脱离终端、读不到发起方的 stdin。
3. **以独立会话 spawn 一个前台子进程**：父进程 self-exec 出 `conduct workflow run <name> --cwd <dir>`（**普通前台子进程、不再带 `-d`**，故不递归），`SysProcAttr{Setsid: true}` 起新会话，彻底脱离发起方的终端会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP。子进程 stdout 重定向 `/dev/null`（进度已逐节点落 `trace.jsonl`，且**绝不能用 pipe**——父进程先退会令子进程写 stdout 触发 EPIPE→SIGPIPE 反被杀），stderr 重定向会话私有临时文件、兜底「写 `run.json` 之前就失败」的罕见路径。此发射器即 `conduct ui` 已在用的 self-exec + setsid 发射路径（见 [ui.md](./ui.md)〈启动运行机制〉）；已把它从 `ui` 私有抽成 `internal/launch`，`-d` 与 UI 共用同一条路径。
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
  **注意**：`-d --json` 吐的是这一行句柄、**不是**前台 `--json` 的逐节点事件流；逐节点事件在 `trace.jsonl`，用 `conduct run show <id> --json --trace` 取。
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

**诚实边界**：`-d` 只是启动模式——run 记录三件套、`status` 语义、`run list` / `run show` / `run stop` 一律不变，后台跑与前台跑落盘逐字节同构。**不提供** `--follow` / 逐节点实时直播（对标 `docker logs -f`）：那是给人看的诉求，交给 `conduct ui` 或 `run show` 轮询；`-d` 刻意不碰，以免范围蔓延。

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

- 人类可读（默认）：表格，列为 `RUN ID | WORKFLOW | STATUS | NODES | STARTED | PROMPT`——run id（目录名，形如 `<workflow>-<时间戳>`）、所属工作流名、状态（`running` / `completed` / `failed` / `interrupted`）、**agent 节点数**（快照里 `len(definition.nodes) - 2`）、开始时间、用户需求（`userPrompt` 截断至约 20 字、超出以 `…` 收尾）；按时间倒序；退出 `0`。
- `--json`：数组，每项 `{"id","workflow","status","nodeCount":<数>,"startedAt":"<RFC3339>","userPrompt":"<完整、不截断>"}`（`nodeCount` = agent 节点数，读时由快照算；机读给全文，截断只发生在人类表格）。
- `--status <state>`：仅保留派生态等于 `<state>` 的记录，其余列 / 格式 / 排序不变；未指定则列**全部**（含已完成 / 失败 / 中断）。
- 无运行记录（或过滤后为空）：stdout 提示 `（暂无运行记录）`；退出 `0`。
- 单个 `run.json` 无法解析：跳过该记录、继续列出其余运行并退出 `0`；每个跳过项向 stderr 打印 `警告: 跳过无法解析的运行记录: <原因>`。若所有记录都损坏，人读 stdout 仍输出 `（暂无运行记录）`，`--json` stdout 输出空数组。

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
RUN ID                     WORKFLOW   STATUS     NODES  STARTED           PROMPT
autopilot-20260703-171102  autopilot  running    4      2026-07-03 17:11  重构结算流程为状态机
autopilot-20260703-152233  autopilot  completed  4      2026-07-03 15:22  给购物车加一个清空按钮
demo-20260703-160140       demo       failed     4      2026-07-03 16:01  实现一个能解释运行 workflow 的引擎…
```

---

## run show — 运行详情 / 状态

**用途**：查看某次运行的状态与详情——汇总（状态、所属工作流、用户需求、节点数、耗时）与逐节点结果；可选附完整 trace。

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
| `--trace` | 布尔 | `false` | 追加打印逐节点 trace（`trace.jsonl` 的每条记录，按 `startedAt` 排序还原时间线） |

**输出**：

- 人类可读（默认）：打印 `run-summary.md` 全文（运行总结，见〈runs/ 落盘结构〉）；退出 `0`。运行**未收尾**（`running` / `interrupted`）时总结尚未生成，改打印状态摘要（run id / 状态 / 用户需求 / 节点数 / 进度 `节点 k/N`）并提示用 `--trace` 查看已执行节点。
- `--json`：输出 `run.json` 的规范化内容。
- **`--trace` 与 `--json` 正交**：`--json` 决定**格式**（机读 JSON ↔ 人类文本），`--trace` 决定**深度**（是否展开每节点**完整** input/output）。四种组合：

  | | 人类（默认） | `--json` |
  | --- | --- | --- |
  | **不加 `--trace`** | `run-summary.md` 全文（未收尾时退回状态摘要） | `run.json` 概要 |
  | **加 `--trace`** | 状态摘要 + 每节点完整 input/output | `run.json` + `"trace":[…]`（`trace.jsonl` 逐行） |

  人类 `--trace` 视图里节点按 `startedAt` 排序（并行下 trace 追加序＝完成序、不定，按开跑时刻还原时间线）；某节点若记有有效非空 `sessionId`（引擎回报的会话/线程 id，见〈runs/ 落盘结构〉），在该节点 input 前附一行会话信息，并按注册 descriptor 的 `SessionReplayCommand` 附回放命令。`sessionId` 为 `null`、旧记录缺字段或历史空字符串时整块省略；引擎未知、函数为 nil 或返回空串时只显示 id。`--json --trace` 使用共享 `run.TraceView`，每条增加读时派生的 `sessionReplayCommand`（无命令为 `null`）；`tokens` / `sessionId` 未知值也为 JSON `null`。

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
  节点 4 · 进度 节点 2/4 · 2026-07-03 15:22 起
  运行总结尚未生成（运行未收尾）；用 conduct run show autopilot-20260703-152233 --trace 查看已执行节点。
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

**诚实边界（进程组连带的适用范围）**：只有以独立会话（`setsid`）发射的 run——`conduct ui` 启动的、或 `conduct workflow run -d`（`--detach`）起的——才保证 pid 即组长、组信号能一并收割引擎子进程。终端里前台 `cat req | conduct workflow run`（未加 `-d`）这类**管道启动**的 run，conduct 不是组长，`kill(-pid)` 得 `ESRCH` → 回退单进程：只终止编排器本身，当前正在跑的引擎子进程会遗留到本节点自然结束（编排器已死、不再调度新节点）——可接受的降级。

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

## run resume — 从中断处恢复运行

**用途**：恢复一次**未正常完成**的运行——无论是引擎报错中止（`failed`）还是进程被杀 / 崩溃（`interrupted`），都跳过已成功的节点、把**未完成的节点**（中断在途、失败、从未开跑的）续跑到终态。DAG + 无循环下每个 agent 节点在整个 run（含本次 resume）里**至多成功一次**，故已成功节点的产物（真实引擎调用、往往很贵）已落盘、直接复用，`resume` 只补跑未完成的前沿及其下游。语义对标「从断点续跑」。`failed` 与 `interrupted` 对「从哪续」没有区别——续跑前沿一律由 `trace.jsonl` 推断（见下文「恢复来源」），二者只是**中断方式**不同（引擎拒绝 vs 进程被杀）。

**为什么是「整节点重跑」而非「引擎续跑」**：中断节点的引擎会话 id 拿不到——`failed` 时引擎返回空结果、不带会话 id，`interrupted` 时进程在记录会话 id 前就被杀——故无法用引擎自带工具接着那次会话续跑；`resume` 一律对未完成节点发起**一次全新调用**（输入由落盘数据重建、与首次一致）。整节点重跑对绝大多数节点无副作用——上游产物一致、prompt 一致。

**用法**：

```
conduct run resume <id> [-d | --detach] [--json]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<id>` | 是 | run id（`run list` 里的 `RUN ID`），须是一次 `failed` 或 `interrupted` 的运行 |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `-d, --detach` | 布尔 | `false` | 后台恢复：预检通过后以独立会话（`setsid`）spawn 子进程续跑，父进程打印 run id 后退 `0`，机制与语义同 `workflow run -d`（见〈后台运行（`-d` / `--detach`）〉），唯一差异是无需经 stdin 喂需求（沿用原 run 的 `userPrompt`） |
| `--json` | 布尔 | `false` | 逐节点输出机器可读事件 JSON（每节点一行，无进度装饰），同 `workflow run --json`；与 `-d` 并用时改为吐单行句柄 `{"id","workflow"}` |

resume **不接受新的用户需求，也不接受 `--cwd`**——二者都从原 run 记录（`run.json` 的 `userPrompt` / `cwd`）沿用，保证与首次运行同一上下文（`{{sys.userPrompt}}` / `{{sys.cwd}}` 不变，`{{sys.runId}}` 仍为原 run id）。

**恢复来源（从落盘数据重建「中断那一刻」的调度状态）**。**`trace.jsonl` 是唯一事实源**——续跑前沿、已完成节点的产物全从它推断，`run.json` 不存重入指针：

- **workflow 定义**：取 `run.json` 的 `workflowSnapshot`（**不**回读 store 里可能已被 `edit` / `delete` 的活 workflow），据 `definition.nodes` / `definition.edges` 确定性还原 DAG。
- **已完成集 `done`（从 trace 推断，`failed` / `interrupted` 同一规则）**：对每个 nodeId 取其 trace **末条记录**（去重、后写覆盖前写）；`done` = {末条记录 `success:true` 的 agent 节点} ∪ {`START`}。中断在途（无记录或末条 `success:false`）、从未开跑的节点都**不在** `done`。
- **续跑前沿与解锁**：对每个**不在 `done`** 的 agent 节点，`pending` = 其前驱中不在 `done` 的数目；`pending == 0` 者进 `ready`，用与 `workflow run` **同一套并行调度循环**续跑；其下游随依赖（前驱陆续完成）自然解锁。若快照里每个 agent 节点末条都 `success`（全成功却因进程被杀、未及把 `run.json` 写成 `completed` 而派生为 `interrupted`），则初始 `ready` 为空、`inflight` 归 0，调度循环空转一轮即收尾——直接 finalize 为 `completed`（补写终态、清 `endedAt` 后重置为收尾时刻），不再驱动任何引擎。
- **上游产物**：回放 `trace.jsonl` 中末条 `success:true` 的记录重建 `{{node-id}}` 引用源（`run.json` 的 `artifacts` 亦可，但以 trace 为权威——按 nodeId 去重、后写覆盖前写）。**注意不可按文件物理行切片**：一次 `resume` 又中断后 trace 已混入旧中断行与补跑行、不是干净前缀，只有按 nodeId + `success` 去重过滤才正确。**读 trace 按 `\n` 完整行解析、丢弃末尾未写完的半行**——崩溃中断最易在 `AppendTrace` 中途留下半行，半行丢弃恰等价「该节点未完成 → 重跑」，与 UI 刷新读 trace 同一条容错铁律（见 [ui.md](./ui.md)〈监控机制〉）。

**前置校验**（不满足即退 `1`，信息明确，fail-loud）。按**派生态**判定（`running` 且进程已死读时派生为 `interrupted`，判活逻辑见〈runs/ 落盘结构〉「运行态与中断判定」）——**派生态是 `failed` 或 `interrupted` 即可恢复**：

- `completed` → stderr `<id>: 已成功完成，无需恢复`。
- `running`（进程存活）→ stderr `<id>: 仍在运行中，无法恢复`（要恢复请先让它自然结束或崩溃；`run stop` 会把它变成 `interrupted`，那之后可 `resume`）。
- `failed` / `interrupted` → 放行。续跑前沿从 trace 推断，即便 trace 为空也能续（所有 agent 节点都未完成，`START` 的后继即初始 `ready`，等价从头跑）。

**行为**：

1. 前置校验通过后，从 trace 推断 `done` 与初始 `ready`、重建 `artifacts`，把 run 状态改回 `running`、更新 `pid` / `pidStartTime`、**清空 `endedAt`（置回 `null`）/ `error` / `failedNodeId`**——`endedAt` 在中断收尾时可能已被写入（`failed` 会写），续跑期间必须复归 `null`，守住「`running` 时 `endedAt` 为 `null`」的落盘不变量；`failedNodeId` 是失败态概要，不能在恢复期或成功终态残留（见〈runs/ 落盘结构〉示例）。**并发 resume 无原子互斥（v1 已知局限）**：抢占该 run（读到终态 → 写 `status:"running"` + 新 `pid`）不加锁；前置校验的 `running` 分支只能挡下「对方已抢占且 pid 存活」之后的第二次 `resume`，两个进程若在该窗口内同时 `resume` 同一 run，会各自续写同一 `trace.jsonl` / `run.json`——属单用户本机工具下可接受的取舍，与 `internal/orchestrator/orchestrator.go` 的实现注释一致。
2. 用同一并行调度循环从 `ready` 起执行：每个就绪节点渲染 → 驱动引擎 → **追加**到**同一** `trace.jsonl`、`artifacts` 增量写回**同一** `run.json`；失败仍走 drain 语义。
3. 终态：全部成功 → `completed`；再次失败 → `failed`（`error` 写新的失败信息，可再次 `resume`——下次续跑前沿仍由 trace 推断）。

**在原 run 记录上续写（不新建 run）**：`resume` 续写原 `runs/<id>/`、run id 不变——语义上「恢复这次运行」＝同一次 run 继续。中断节点的旧 trace 记录**保留**（`failed` 那节点留有一条 `success:false` 记录；`interrupted` 那节点本就没记录），新记录续写在其后，故 `run show --trace` 会同时看到中断那次与重跑那次的记录，是有意保留的审计轨迹。

> **进度 `k/N` 的计数按 nodeId 去重**：`run show` 的未完成态进度分子与 UI 的进度分子取「trace 中**唯一 nodeId 且 `success`** 的条目数」（`store.CountProgress` → `run.ProgressCount`，同一节点以最后一次记录为准），而非 `trace.jsonl` 的物理行数；分母 `N` = agent 节点数（`len(definition.nodes) - 2`）。`run list` 人类表格与 JSON 只输出 `NODES` / `nodeCount`（agent 节点数），不输出 `k/N` 进度分子（见〈run list〉输出）。因为 resume 保留旧失败行 + 续写补跑行会让**同一 nodeId 出现多条**，若数物理行，`k` 会越过分母 `N`；按 nodeId + `success` 去重使 `k ≤ N` 恒成立。审计视角要看全部历史记录仍走 `run show --trace`（不去重、按 `startedAt` 排序）。

**输出**：

- 前台（默认）：同 `workflow run` 前台——先打印已完成几个、待续几个，再逐节点打印进度事件流，收尾指向 `run-summary.md`；退出码表达**整趟恢复**的成败（全部成功退 `0`、有节点失败退 `1`）。
- `--json`（前台）：stdout 每节点一行事件 JSON，同 `workflow run --json`；逐节点事件即续写进 `trace.jsonl` 的记录。
- `-d`：同 `workflow run -d`——打印 run id（`--json` 吐单行句柄 `{"id","workflow"}`，`id` 即原 run id）后退 `0`；恢复的真实成败去 `run show <id>` / `run wait <id>` 查。
- 落盘副作用：原 `runs/<id>/` 的 `run.json` / `trace.jsonl` 续写更新，`run-summary.md` 收尾重生成。

**退出码**：

| 码 | 含义 |
| --- | --- |
| `0` | 前台：整趟恢复成功到 `completed`；`-d`：后台已发射、run id 已打印到 stdout |
| `1` | 前台：恢复中有节点失败；或前置校验不通过（`completed` / `running` 存活）、`<id>` 不存在、IO 失败；`-d`：fork / setsid 发射失败或有界等待内未取得句柄 |
| `2` | 用法错误（缺 `<id>`、`<id>` 非法） |

**示例**：

```bash
# 某次运行有节点失败，跳过已成功节点、只补跑未完成的前沿及其下游
conduct run resume autopilot-20260703-152233

# 后台恢复，拿到 id 立刻脱手，再等它收尾
id=$(conduct run resume autopilot-20260703-152233 -d --json | jq -r .id)
conduct run wait "$id" && conduct run show "$id"
```

**边界**：`resume` 恢复 `failed`（引擎报错中止）与 `interrupted`（进程被杀 / 崩溃）两类未完成的 run；续跑前沿统一由 trace 推断，二者无差别对待。`interrupted` 的中断节点可能在进程被杀前已产生**半途副作用**（引擎实际改了文件但没记进 trace），整节点重跑会重复执行该节点——与 `failed` 重跑同属「prompt 未必幂等」的已知取舍，绝大多数节点无副作用故可接受。另一叠加场景：`run stop` 对**非组长** run 的降级路径会遗留一个仍在写 `cwd` 的引擎子进程（见〈run stop〉诚实边界），此刻立即 `resume` 会对同一节点再起一个引擎、与遗留进程同写一份工作树——建议等遗留引擎自然结束再 `resume`。resume 不改变 run 记录三件套结构、`status` 语义，也不影响 `run list` / `run show` / `run stop` / `run wait`。

> **`error` 字段的定位（已定）**：trace 失败条目的 `error` 保存该次引擎调用的原始错误；`run.json.error` 是 run 级快速排查摘要，会在原始错误前补上固定英文 `node <id>:` 技术上下文，两者不保证字符串相同。**定案保留 run 级 `error`**——`run list` / `run show` / `run-summary.md` 免解析 trace 即可显示带节点上下文的头条；trace 仍是单次尝试的权威事实源。

---

## run rm — 删除运行记录

**用途**：删除一条历史运行记录（清掉 `runs/<id>/` 整个目录），供长期使用后清理（对标 `docker rm`）。本命令不修改记录内容，只**整条移除**。默认在交互终端下二次确认；非交互环境必须显式 `--yes`，避免脚本误删。

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

> **与 `docker rm` 的差异**：只接受**单个** id（不批量、不通配）、**拒删在跑的 run**（无 `-f` 强删，须先 `run stop`）、且默认二次确认（`--yes` 跳过）——比 `docker rm` 保守，因为 run 记录保存了烧过 token 的执行历史，误删不可逆。

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
        └── trace.jsonl            # 逐节点 trace，每行一次节点执行尝试（含 input/output，失败含 error），追加写
```

> 工作流定义 `~/.conduct/workflows/` 的布局见 [cli-authoring.md](./cli-authoring.md)〈workflows/ 落盘结构〉。

三类文件：

**`runs/<id>/run.json`** —— 运行概要（宏观信息，归档 / 排查用；`run` **开跑即写**（`status:"running"`）、收尾再更新为终态，`run list` / `run show` 的机读数据源）。含**开始运行那一刻冻结的 workflow 定义**——运行开始后工作流可能被编辑或删除，本记录一律以冻结快照为准，使这次运行永远可复现、可解释：

```jsonc
{
  "id": "autopilot-20260703-152233",  // 本次运行的 run id（{{sys.runId}}），等于所在目录名（<workflow>-<时间戳>）
  "workflow": "autopilot",          // 所属工作流名
  "workflowSnapshot": {             // 冻结快照：开始运行时的完整 workflow 记录（name/createdAt/updatedAt + definition{nodes（含 START/END）, edges}），结构见 cli-authoring.md〈workflow 定义 schema〉
    "name": "autopilot",
    "createdAt": "2026-07-03T09:12:00+08:00",
    "updatedAt": "2026-07-03T15:40:00+08:00",
    "definition": {
      "nodes": [ /* … 含 START / END … */ ],
      "edges": [ /* … */ ]
    }
  },
  "userPrompt": "给购物车加一个清空按钮",  // 本次用户需求（{{sys.userPrompt}}）
  "cwd": "/Users/me/proj",          // 引擎工作目录（--cwd）
  "status": "failed",               // running | completed | failed（interrupted 为派生态：status 仍 running 但 pid 已死）
  "language": "zh-CN",              // 必填：en | zh-CN；开跑时的语言快照，summary / resume 固定沿用
  "pid": 48213,                     // 运行进程 PID；据此判活——status=running 但进程已死 → interrupted
  "pidStartTime": "1783263565.442591",  // 进程启动时刻令牌，与 pid 联合校验以免 pid 被无关新进程复用时误判/误杀（旧记录或不支持的平台为空，omitempty）
  "startedAt": "2026-07-03T15:22:33+08:00",  // RFC3339，开跑即写
  "endedAt": "2026-07-03T15:24:14+08:00",    // RFC3339，收尾写；running 时为 null
  "artifacts": {                    // 各 agent 节点最终产物：nodeId → 该 node 最后一次成功的 output（随运行推进增量写）
    "a": "…", "b": "…", "c": "…", "d": "…"
  },
  "error": "node c: codex turn failed", // status=failed 时的 run 级失败摘要；conduct 技术上下文固定英文，否则 null
  "failedNodeId": "c"              // status=failed 且根因是节点失败时，首个失败节点 id；无节点级根因时省略
}
```

> **无 `steps` 字段**：进度分母 `N` = agent 节点数，读时由 `workflowSnapshot` 算（`len(definition.nodes) - 2`，排除 `START` / `END`），不落盘冗余；分子 = trace 中唯一 nodeId 且 `success` 的条目数（见〈run resume〉进度去重说明）。`failedNodeId` 仅供 summary / UI 快速定位首个失败节点，**不是**重入指针；恢复前沿仍一律由 trace 推断（见〈run resume〉恢复来源）。

> **`language` 不做旧数据兼容**：字段缺失或取值不是 `en` / `zh-CN` 时，`run.json` 视为损坏。读取、resume 与后续写回均固定英文 fail-loud，不使用旧版中文、当前全局设置或环境变量补值。

**`runs/<id>/run-summary.md`** —— 运行总结，给人 / AI 阅读的 Markdown 报告，由 `run.json` 与 `trace.jsonl` 渲染而来（人类读这份、机器读 `run.json`——同一份运行记录的两副面孔）。至少含：所属工作流、用户需求、状态、开始 / 结束时间与耗时、逐节点结果（每节点 / 引擎 / 起止 / 耗时，**按 `startedAt` 排序**）、各 agent 节点最终产物（Markdown 原文、XML 标签包裹，见下例）。`run` 结束时 stdout 指向的就是这份文件。**用户需求只渲染为一行摘要**（取首行、超长截断并以 `…` 收尾）：需求可能是整份 PRD（数十 KB），整段塞进头部会淹掉节点表与产物；被截断时附「（完整需求见 run.json）」，全文由 `run.json` 的 `userPrompt` 保留——与 `run list` 人读截断、机读留全文的同一分工。示例（`completed` 运行；`failed` 会额外渲染失败节点与 `error`）：

````markdown
# autopilot-20260703-152233

**工作流** autopilot · 4 节点
**需求** 给购物车加一个清空按钮
**状态** ✅ completed · 101s（2026-07-03 15:22:33 → 15:24:14）
**工作目录** /Users/me/proj

## 节点

| 节点 | 引擎 | 起 → 止 | 耗时 |
| --- | --- | --- | --- |
| 调研现状 | claude-code | 15:22:33 → 15:23:13 | 40s |
| 起草方案 | qoder | 15:22:33 → 15:24:03 | 90s |
| 实现 | claude-code | 15:24:03 → 15:24:11 | 8s |
| 验收测试 | claude-code | 15:24:11 → 15:24:14 | 3s |

## 产物

<output node="a" name="调研现状">
# 现状调研

购物车页头部无「清空」入口，逐项删除体验差。
</output>

<output node="b" name="起草方案">
# 交付方案

拆成接口 / 前端 / 验收三块推进。
</output>

<output node="c" name="实现">
```diff
--- a/src/Cart.tsx
+++ b/src/Cart.tsx
@@ export function Cart() {
+  <button onClick={() => setConfirming(true)}>清空购物车</button>
```
</output>

<output node="d" name="验收测试">
清空 / 取消 / 空车 三条 e2e 全绿。
</output>
````

**`runs/<id>/trace.jsonl`** —— 逐节点执行日志，[JSON Lines](https://jsonlines.org/) 格式：每行一个独立 JSON，追加写，**行序＝完成序**（并行下与拓扑序不同，跨运行不定；审计按 `startedAt` 还原时间线）。每次 agent 节点执行尝试一条（`START` / `END` 不产条目）；首次运行通常每节点一条，`resume` 会保留旧失败条目并为同一 `nodeId` 追加新条目。`sessionReplayCommand` 是读取时由 `TraceView` 派生的展示字段，**不写入**此文件。`run --json` 逐节点吐出的事件就是这些持久化记录：

```jsonc
// 每行一条（此处换行仅为可读，实际每条压成一行）
{
  "nodeId": "a",               // 所属节点 id（天然主键）
  "displayName": "调研现状",     // 冗余存节点名，使本文件自解释（不依赖当时的定义）
  "engine": "claude-code",     // 该节点实际调用的引擎（同定义的判别式；引擎层如何把它落到 CLI 调用见 engines.md）
  "engineConfig": {            // 该节点生效的引擎配置，记声明值，结构同定义（见 cli-authoring.md〈workflow 定义 schema〉、engines.md〈引擎能力表〉）
    "model": "claude-opus-4-8",// 声明的模型；定义省略则此字段缺省（引擎侧用其默认模型，本记录不额外探测该默认名）
    "effort": "high"           // 声明的推理档位；是否接受及合法值由引擎 descriptor capability 决定
  },
  "input": "…",                // 该节点喂给引擎的完整输入（渲染后的 promptTemplate，全文不截断）
  "success": true,             // 该节点是否成功
  "error": null,               // 失败（success=false）时的错误信息：引擎报错 / 退出码 / stderr 摘要，全文；成功为 null
  "output": "…",               // 该节点产物全文（不截断；进度显示里的 80 字预览只是展示截断）
  "tokens": 1100,              // 必有、可空：引擎明确回报的本节点 token 消耗；未知为 null，已知 0 写 0
  "sessionId": "0199a213-…",  // 必有、可空：引擎明确回报的非空会话/线程 id；未知为 null；凭非空 id 用引擎自带工具回放本节点
  "startedAt": "2026-07-13T16:00:00+08:00",  // 节点开跑时刻（RFC3339）——并行下据此还原时间线
  "endedAt": "2026-07-13T16:00:40+08:00",    // 节点落定时刻（RFC3339）
  "durationMs": 40000          // 该节点耗时（毫秒）
}
```

新写入的每条 trace 都显式包含 `tokens` / `sessionId`。旧 trace 缺少字段时读取为 `nil`，经 `run show --json --trace` 或 HTTP 重新输出会规范化为 JSON `null`，无需迁移历史文件。Kiro 两项固定为 `null`；这表示引擎未提供，不表示免费或实际消耗为零。

> **运行态与中断判定**：`run.json` 开跑即写（`status:"running"`）、`trace.jsonl` 每节点落定即追加、`run-summary.md` 收尾才生成（故 `running` 时尚无 summary）。`run show` / `run list` 读到 `status:"running"` 时按 `pid` 判活（并核对 `pidStartTime` 启动时刻，防 pid 被无关新进程复用时误判）：进程在＝真运行中；进程已死＝ `interrupted`（崩溃 / 被强杀），尽力展示已有 trace。终态 `completed` / `failed` 以 run.json 为准。

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：引擎调用失败、IO 失败、目标不存在等 |
| `2` | 用法错误（缺参、非法参数）——Cobra 默认 |

具体原因看 stderr 报错，按命令语境即可区分（`run` 的 `1` 多为引擎失败、`run show` 的 `1` 多为目标不存在）。（编辑态命令的退出码见 [cli-authoring.md](./cli-authoring.md)〈退出码约定〉，同一张表。）

**`-d` 与 `run wait` 的退出码都不表达 run 的成败**（各命令小节已详述）：`workflow run -d` 只表达**发射**成没成、`run wait` 只表达**有没有等到终态**；两者的 run 成败一律去 `run show` / `run wait --json` 的 `status` 看，不进退出码。这与 `docker run -d` / `docker wait` 一致——退出码说的是「命令这一步成没成」，不是「里头的 run 最终成没成」。

## 实现状态（诚实标注）

本规格描述的「节点 + 边的并行 DAG」运行时**已实现**：并行调度器、trace / run.json schema、resume 语义均已按本规格完成改造。

| 命令 / 主题 | 状态 |
| --- | --- |
| 并行 DAG 调度器 | **已实现**（`internal/workflow/graph.go` 图算法 + `internal/orchestrator/schedule.go` 并行调度器：Kahn 拓扑 + 并发、单调度 goroutine 独占共享态（`pending`/`done`/`ready`）、START 预置 done、END no-op、drain 失败语义） |
| `workflow run` 事件流输出 | **已实现**（节点生命周期事件流：`▶ 开跑` / `✓ 完成` / `✗ 失败`，无 iteration；`--json` 每节点落定吐一行 `TraceEntry`） |
| `workflow run` 无 `--max-parallel` | **已实现**（不引入并发上限，就绪节点一律开跑，受 DAG 结构自然约束） |
| trace.jsonl schema | **已实现**（`run.TraceEntry` 已删除 `Type` / `Iteration`，新增 `StartedAt` / `EndedAt`，主键为 `NodeID`；`Tokens` / `SessionID` 为无 `omitempty` 的可空指针，未知值显式写 `null`） |
| run.json schema | **已实现**（`run.Record` 无 `Steps int`；进度分母由 `WorkflowSnapshot` 读时算 agent 节点数；`workflowSnapshot` 嵌套 `definition{nodes, edges}`；`failedNodeId` 直接记录首个失败节点；`language` 必填且严格校验） |
| `run resume` DAG 语义 | **已实现**（`internal/cli/run_resume.go` 按 nodeId 推断 `done` 集 + 前驱解锁续跑，不再经过 evaluator，复用同一并行调度循环） |
| `run list` 进度列 | **已实现**（`NODES` 列 = agent 节点数，JSON 字段 `nodeCount`） |
| `run-summary.md` 渲染 | **已实现**（节点表按 `startedAt` 排序，无 evaluator 行） |
| `run list` / `run show` / `run stop` / `run wait` / `run rm` / `workflow run -d` | **已实现**（命令骨架与后台发射机制 `internal/launch` 已随 schema / 进度 / 术语（步→节点）同步调整读写与展示） |
| Descriptor 注册表中的引擎 | **已实装**（当前清单、无头 CLI 调用及可空 token/session metadata 契约见 [engines.md](./engines.md)） |

> **不考虑兼容**：旧线性 run 记录与新模型结构不同，**无法用新版 `run resume` 续跑**，未写迁移代码；旧记录仅作历史查看（字段对不上处尽力展示）——这是本次改造的既定取舍，非缺陷。

解释器内核（渲染 `render` + 引擎抽象）复用；`internal/workflow/expand.go`（`Expand` + `ExecutionStep`）已整体删除，代之以图算法 + 并行调度器。尚未做：超时重试。
