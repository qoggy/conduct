# conduct ui 设计方案

> 本文是 `conduct ui` 的**设计方案（面向评审）**。事实基线：[cli-authoring.md](./cli-authoring.md)（〈设计前提〉〈数据模型〉〈workflows/ 落盘结构〉〈落盘校验规则〉）、[cli-runtime.md](./cli-runtime.md)（〈数据模型〉〈runs/ 落盘结构〉〈workflow run〉〈run …〉）、[cli-tooling.md](./cli-tooling.md)〈ui〉，与当前已实现的代码（workflow / run 命令族、`internal/orchestrator` / `internal/store` / `internal/engine`）。文中每个界面能力都标注了 CLI 等价物；确需在 UI 之外额外实现的，集中列在〈需要额外实现的功能〉。

## 设计取向

**UI 是 CLI 动词的直白镜像。** 页面与 noun 一一对应、表格与 CLI 表格同列、编辑是 `edit` 的整体替换语义、监控是「自动帮你反复执行 `run show`」。宁可朴素，不可花哨——目标用户是程序员，可预测性本身就是体验。

五条铁律，贯穿所有页面：

1. **UI 无独占能力**（规格不变量）：每个界面动作都有对应 CLI 命令；CLI 没有的（如 `run delete`），UI 也没有。反向不要求：CLI 有而 UI 不做是允许的（如 `create --definition` 导入）。
2. **该看的全文可见**：提示词、每步 input/output 一律**全文**呈现；性能手段只允许「默认折叠、点开才渲染」，**不允许缩略内容**。
3. **不该看的不出现**：`~/.conduct`、`trace.jsonl`、`run-summary.md` 等内部文件路径字样全站不出现（`cwd` 是用户自己传的运行参数，属「该看的」，照常展示）。
4. **fail-loud 同源**：校验、错误全部复用内核同一套逻辑与文案，UI 不复刻第二套规则。
5. **文案克制**：界面不写教学式解释——引擎差异由表单结构自己表达（该引擎没有的字段就不渲染，而不是渲染出来再配一段说明书）；不加冗余修饰后缀（「输入」就是输入，不写「输入（完整）」）。

## 命令形态（`conduct ui` 自身，含待定项定案）

```
conduct ui [--port <n>] [--open]
```

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--port <n>` | 整数 | `7420` | 监听端口；被占则 stderr 报错退出 `1`（不自动递增——可预测、书签友好） |
| `--open` | 布尔 | `false` | 启动后自动打开浏览器；默认不开（照顾 SSH / 无头环境），仅打印地址 |

- 服务**只绑定 `127.0.0.1`**，不监听 `0.0.0.0`。
- **启动时主动探测一次 store 可读性**（执行一次 List）：不可读 → stderr 报原因退出 `1`（承规格「store 不可读 → 退 1」，不做「启动假成功、首个请求才报错」）。
- 启动后 stdout 打印入口地址，进程驻留至 `Ctrl-C`（承规格既有约定）。
- **v1 不做账号鉴权**，但所有 `/api/*` 校验 `Host` / `Origin` 白名单、变更类端点仅接受 `application/json`，且对所有响应加 `X-Frame-Options: DENY` / `X-Content-Type-Options: nosniff`（防点击劫持与 MIME 嗅探）。诚实边界：这防的是**浏览器跨站**（恶意网页 fetch 本地端口），**不防本机进程**——单用户本机工具下可接受，定案时写进规格。
- 落地时同步修订 `cli-tooling.md`〈ui〉一节（该节已预留「启动机制由用户另行规定」，本节即定案提议）。

## 信息架构

顶栏两个导航项，与数据模型的两张表一一对应，不发明第三个概念：

```
┌──────────────────────────────────────────────┐
│ conduct    [工作流] [运行]          v0.1.0   │
├──────────────────────────────────────────────┤
│                                              │
│   #/workflows        工作流列表（默认路由）   │
│   #/workflows/:name  工作流编辑器             │
│   #/runs             运行列表                 │
│   #/runs/:id         运行详情                 │
│                                              │
└──────────────────────────────────────────────┘
```

hash 路由（单 index.html，embed 静态服务无需 history fallback，刷新 / 书签稳定）。跳转关系：

- 工作流列表行 → 编辑器；列表行「运行」/ 编辑器顶栏「运行」→ 启动弹窗 → 成功后自动跳转该运行详情；编辑器「运行历史」→ `#/runs?workflow=<name>`。
- 运行列表行 → 运行详情；运行详情头部的工作流名 → 回编辑器（该 workflow 已被删除 / 改名时名字不可点——快照语义下的诚实状态，冻结定义仍可在本页查看）。

## 页面设计

### 工作流列表（`#/workflows`）

`workflow list` 的表格镜像，兼 create / rename / delete / run 四个动词的入口。

- 表格列 `名称 | 节点流 | 最近修改`，与 CLI 表格同列（随 cli-authoring.md〈workflow list〉修订：节点列展示节点 id 流、移除引擎列）；节点流以中性 chip 呈现（`plan › code › test › review`），超过 6 个折叠为前 6 个 + `+N`。另加 `运行中` 徽标列（该 workflow 下 `status=running` 的 run 数，来自 run 表 join——规格〈数据模型〉明示支持此聚合，等价 `run list --json` 过滤）。行点击进入编辑器。
- **行内操作**：**运行**（打开启动弹窗，见下）· **改名**（弹窗，= `rename`，文案明示「已有运行记录保留旧名」）· **删除**（确认弹窗，= `delete --yes`——UI 弹窗承担 CLI 交互式二次确认的职责；文案明示「运行记录不受影响」）。
- **新建工作流**：弹窗只有一个名称输入（前端即时校验 `[A-Za-z0-9._-]+`），确认即以最小骨架创建（= `create`）并**直接进入编辑器**——最短路径。不提供「导入定义 JSON」：那是 CLI 专属路径（`create --definition`），不变量是单向的，UI 不必背。
- store 为空 → 空态引导新建；store 里有解析失败的坏文件 → 页顶警告条如实列出（对齐 `store.List` 的 skipped 语义，不静默隐藏）。

**启动运行弹窗**（= `workflow run <name> --cwd <dir>`，需求经 stdin）：

- 字段两个：**需求**（多行 **markdown 编辑器**，与提示词编辑器同款外观、不含占位符高亮；非空校验，前端 + 服务端双重把守，防误发空需求烧 token）、**工作目录**（自由输入，留空即用 `workflow run` 的 `--cwd` 默认值——`conduct ui` 进程的启动目录；旁挂「浏览…」目录选择器辅助定位，见下；**必须是已存在的目录、且为绝对路径**（UI 无 shell，不做 `~` 展开与相对路径拼接），服务端校验、就地报错）。
- **目录选择器**（「浏览…」弹窗，数据源 `GET /api/fs`）：从某目录出发，列出其父目录与子目录（只列目录、含隐藏目录如 `.claude`），逐级点入选定工作目录。纯只读浏览，不产生任何副作用——同 `/api/engines` 属只读信息性端点，是「无独占能力」不变量的显式豁免。
- 启动成功自动跳转运行详情页。发射机制见〈启动运行机制〉。

### 工作流编辑器（`#/workflows/:name`）

`show`（载入）+ `edit`（整体替换保存）的聚合工作台，UI 的主用途所在。**两栏布局：左侧流程概览（紧凑），右侧检查器（详情）**——整个流程一眼可见，配置细节只展开选中的那一个节点，页面不会被撑爆。

**顶部条**：工作流名（旁挂改名入口）· 节点数 · updatedAt · 未保存改动标记 ·「表单 | JSON」视图切换 ·「运行」（同列表页启动弹窗）·「保存」。

**左栏 · 流程概览**（约 300px，忠实于数据模型——nodes 是线性数组，不做自由画布 / 拖拽连线）：

- 首尾虚线锚点「▶ 输入需求」「● 完成」（纯视觉定界，不是节点）；节点间箭头连线。
- 每个节点一张**紧凑卡片**：引擎品牌 icon · displayName · **evaluator 自循环徽标**（`↻ ×N`，贴在卡片上，点击进入评测循环聚焦面板）——`id` 与引擎全名不上卡片（检查器里看），保持画布干净。点击卡片本体选中，右栏检查器随之切换。
- **redoTarget 回跳**不贴卡片徽标，而画成画布左侧一条**紫色弧线**（顶端箭头指向回跳目标节点、竖排 `回跳 ×N` label，点击 label 进入回跳线聚焦面板；redoTarget 指向不存在节点时弧线转红，与校验错误态一致）——回跳是跨节点跳转，与自循环本质不同，故画布上区分呈现。弧线两端可拖拽改连接：拖目标端改 `redoTarget`（落点须在源节点之前）、拖源端把整条回跳移到新源节点（`target` / `loopCount` 保留）。
- 卡片支持**拖拽排序**（pointer 拖拽换序，数组顺序影响 redoTarget 前向性，必须可调序）；悬浮卡片右上角出现**删除**；列尾「＋ 新增节点」。
- 删除节点时预警其它节点对它的 `{{id}}` 模板引用与 redoTarget 指向（省一轮「删完→保存→报错→回改」；最终裁决仍归服务端 Validate）。

**右栏 · 检查器**（选中节点的完整配置；**单列表单**，一行一字段、自上而下填写；**标签置于控件上方**，必填项以红色 `*` 标注（`*` 置于标签文字左侧）；字段的辅助说明一律收进标签旁 ⓘ 悬浮气泡，不占版面）：

- `id`（正则 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$` 即时轻提示）、`displayName`；
- `engine` 下拉（数据源 `GET /api/engines` 能力表：当前 claude-code / antigravity / qoder，codex 下线不出现）；选项与左栏节点卡片均带**引擎品牌 icon**（icon 资产随前端内嵌，不引外链）；
- `engineConfig` 按判别联合**条件渲染**——claude-code → model 输入 + effort 下拉（low/medium/high/xhigh/max/ultracode/auto）；qoder → model 输入 + reasoningEffort 下拉（disabled/off/none/low/medium/high/xhigh/max）；antigravity → 仅 model 输入。**该引擎没有的字段就不渲染，不配解释文案**。model 一律自由文本、placeholder「留空使用引擎默认模型」——忠实于「model 不做白名单」，不捏造模型下拉清单；
- **提示词编辑器**：markdown **源码模式 + 语法高亮**（标题 / 代码围栏 / 加粗等 md 语法着色），**模板占位符高亮**（`{{sys.userPrompt}}` / `{{<nodeId>}}` 独立着色）；全文可见可编辑、在最小/最大高度约束内自动增高，超出最大高度**编辑器内部滚动**（内容仍全文可达，绝不缩略）；附「最大化」整页覆盖层。编辑器头部条展示字段名（`promptTemplate`）、md 标记与实时「行 · 字符」统计，尾部灰显**运行时自动追加段的只读预告**（agent 追加 `## Previous evaluator feedback` 段、evaluator 追加 `## Artifact under review` 段——展示编排器真实拼接的 Markdown 小标题 + 尖括号占位描述，不铺陈 `<previous_evaluator_feedback>` 这类 XML 边界标签）。**占位符入口**在编辑器头部条：hover 展开下拉（仅列占位符本体，不配描述文字），点击即复制——sys 变量（`{{sys.userPrompt}}` / `{{sys.cwd}}`）+ 当前定义内**全部**节点 id（引用后序节点合法、未产出时展开为空串，与校验语义一致）；
- **循环配置**（单次 / evaluator 内循环 / redoTarget 回跳，三态结构互斥）：节点默认单次，检查器给两个入口 CTA「设为评测循环」「设为回跳循环」（回跳在首节点禁用——无前序节点可回跳）。设置后画布出现对应徽标 / 弧线，配置改到**独立聚焦面板**（点画布徽标 / 弧线或检查器摘要行进入，与节点面板同栏切换）：
  - **评测循环聚焦面板**：同构子表单（engine + engineConfig 复用同一组件 + 评测提示词，同款高亮编辑器）+ `loopCount` 数字输入（1–20）+「清除评测官」回单次。
  - **回跳线聚焦面板**：redoTarget 下拉**只列本节点之前的节点 id**（结构上杜绝非法回跳）+ `loopCount`（1–20）+「清除回跳」回单次。

**JSON 视图**——完整定义原始 JSON 的源码编辑区（同款高亮），与表单双向同步；作为兜底编辑模式（typora 式双模式），面向想直接改 JSON / 从外部粘贴的程序员。JSON→表单切换先 parse：语法错误 → 阻止切换、就地标错。未知字段不在切换时拦，而是整体保留进定义、保存时由服务端 `DisallowUnknownFields` 拒绝（绝不静默丢字段——不在前端复刻一份字段白名单，避免与内核 schema 漂移）。`name` / `createdAt` / `updatedAt` 属系统元数据：JSON 视图整体可编辑（这三个字段也在编辑区里显示），但保存时按规格处理，不靠置灰硬拦——改动 `name` 与目标名不一致 → 拒绝保存（绝不静默改名，改名走独立的 rename 入口）；`createdAt` / `updatedAt` 由服务端管理，用户改动被忽略。已知限制：视图切换会重建编辑区，浏览器原生撤销（⌘Z）历史随之清空，界面提示告知。

**保存**：PUT 完整定义整体替换（= `cat def.json | conduct workflow edit <name>`），服务端复用 `ParseDefinition + Validate + store.Save`——校验不过 → 422 逐条返回字段级错误、**不落盘、原定义不变**。错误面板置顶列出（与 CLI stderr 逐字一致），每条按 `nodes[i].<字段>` 路径**点击定位**：左栏对应卡片标红点、右栏切到该节点并红框该字段。**乐观并发提示**：保存请求携带载入时的 updatedAt 基线，服务端发现已被外部改过（CLI `edit` 或另一标签页）→ 返回冲突，前端弹「已被外部修改：覆盖保存 / 放弃重载」——软提示不做硬锁，不超出 `edit` 的 last-write-wins 语义。

### 运行列表（`#/runs`）

`run list` 的表格镜像 + 过滤。

- **running 项置顶分组**（在跑的是监控核心，不混进时间倒序），其余按开始时间倒序。
- 表格列 `RUN ID | 工作流 | 状态 | 进度 | 开始时间 | 需求`：状态徽标四态 running / completed / failed / interrupted（interrupted 为服务端按 pid 判活的**读时派生态**，与 `run show` 同逻辑）；进度列 running / interrupted 显示 `k/N`（k = 已落盘 trace 行数；interrupted 即中断时进度）、终态显示总步数；需求列表格内截断（与 CLI 人类表格同策略，**全文在详情页**）。
- `工作流` / `状态` 两个过滤器（等价 `run list --json | jq` 聚合，无新能力）。
- 顶部提供**刷新按钮**：running 项的状态 / 进度不自动更新，点刷新即全量重取（见〈监控机制〉）。
- 空态「（暂无运行记录）」。

### 运行详情（`#/runs/:id`）

`run show` / `run show --trace` 的镜像；running 时点**刷新按钮**看推进。全页不显示、不提及任何文件路径。

- **概要头**：run id · 所属工作流（链接）· 状态徽标 · **userPrompt 全文**（不截断，超长可折叠但点开即全文，标题旁**复制按钮**）· cwd · **pid**（`run show --json` 可见数据，照常展示）· 进度 `k/N` 或 耗时 · startedAt → endedAt。running 时提供**「终止运行」**按钮（= `run stop <id>`，新增命令；确认弹窗后调用，终止后按 pid 判活语义转 interrupted）；interrupted 时如实标注「进程已不在」。
- **failed**：`error` 全文置顶展示，failedStep 步高亮标红。interrupted：如实标注「进程已不在，尽力展示已有 trace」。
- **逐步列表**：含循环（评测内循环 / 回跳）的运行按「第 N 轮」分组头聚合各轮迭代步，一眼看出循环了几轮；每步一行——step 序号 · 节点 id chip · 步骤名（复用 StepLabel：evaluator 步带「· 评测」后缀）· 引擎（icon + 引擎名）· 成功/失败 · 耗时 · tokens · 产物预览（单行截断）；**engineConfig 声明值（model / effort）不占行内列，只在展开详情顶部展示**。**每步均可点击展开完整「输入 / 产物 / 错误」全文**（`--trace` 深度；以 **md 源码高亮**只读呈现，与提示词编辑器同款外观、不含占位符高亮），各段标题旁**复制按钮**一键复制全文。展开才构建 DOM（trace 单行可达 MB 级，全量直灌会卡死页面）——**延迟渲染是唯一允许的性能手段，点开必须完整全文**。
- **运行总结**：终态后渲染 run-summary 内容（Markdown 富文本：概要、步骤表、逐节点产物块）——给人读的报告，替代生硬的原始数据堆砌；标题旁**「复制全文」**（总结的 Markdown 原文）；running 时如实提示「运行结束后生成」。数据即 `run` 收尾落盘的总结（由 `run.RenderSummary` 从 run.json + trace 确定性生成，= `run show --json --trace` 的可读投影，无新增信息面）。
- **冻结定义**折叠区：点开渲染 workflowSnapshot 的**只读高亮 JSON**（可复制），「运行开始时冻结，与当前定义可能不同」的说明收进标题旁 ⓘ 气泡——快照语义的诚实呈现。
- **手动刷新推进**：status=running 时页面提供**刷新按钮**，点击全量重取 `?trace=1`，新步骤追加、进度前移；每次刷新都重算派生态（running→interrupted 的降级刷新即可见，绝不假装还在跑）。不自动轮询（见〈监控机制〉）。

## 启动运行机制（核心决策：self-exec 子进程）

UI 服务端以 `os.Executable()` 自呼 `conduct workflow run <name> --cwd <dir>`，**不用进程内 goroutine 跑 orchestrator**。

**goroutine 方案的三处硬伤**（否决理由）：

1. pid 判活语义被稀释：`orchestrator.Run` 写 `Pid: os.Getpid()`（orchestrator.go:75），进程内跑则所有 run 记同一个 ui 进程 pid——某 run 的 goroutine 异常后 ui 还活着 → 该 run 永远显示 running，interrupted 派生失效（假活，违背 fail-loud）。
2. run 生命期与 UI 绑死：Ctrl-C 关 UI 连带杀死所有在跑 run——「关仪表盘杀运行」违反监控工具直觉，且与终端启动的 run 行为不对称；run 烧的是真金白银的 token。
3. run id 获取需改内核：`orchestrator.Run` 跑完才返回 runID，即时拿到得拆 Prepare/Execute 或扩 Observer。

**self-exec 方案**：pid = 子进程自身 pid，判活 / interrupted 语义与终端启动**逐字节一致**；「UI 启动 ≡ 执行一条 CLI 命令」是无独占能力不变量的最强证明。对 orchestrator / store / CLI 零改动。实现细节（每条都有对应的踩坑背书）：

| 细节 | 决策 | 理由 |
| --- | --- | --- |
| 脱离方式 | `SysProcAttr{Setsid: true}` 起新会话 | 彻底脱离 UI 的会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP 两条路径；Setpgid 不够（同会话仍可能收到 shell 转发的 SIGHUP） |
| userPrompt 传递 | stdin 管道写入后关闭 | 规格支持 stdin 需求；规避 argv 长度限制与转义（整份 PRD 也无压力） |
| 子进程 stdout | 重定向 `/dev/null` | 进度已逐步落盘 trace，无需管道；**绝不可用 pipe**——UI 先退出会令子进程写 stdout 时 EPIPE，Go 运行时对 fd 1/2 的写失败重升 SIGPIPE 杀死 run，恰好击穿「UI 退出 run 继续跑」的承诺 |
| 子进程 stderr | 重定向 UI 会话私有临时文件 | 兜底「CreateRun 之前就失败」（store IO 等罕见路径）的原因回传；读后即弃，绝不在界面出现该路径 |
| 发射前预检 | spawn 前服务端进程内跑 `store.Load + workflow.Validate`（只读）+ 需求非空 + **工作目录存在性**校验 | 把「workflow 不存在 / 定义损坏 / 目录不存在」在起子进程前以 400/422 拦下，失败反馈从秒级缩到毫秒级；stderr 临时文件收窄为罕见路径兜底 |
| run id 获取 | spawn 后轮询 run 列表，**组合条件匹配**：`workflow == 目标名 && status == running && startedAt >= spawn 时刻（留时钟余量） && record.pid == 子进程 pid` | run.json 开跑即写（含 pid），通常亚秒命中。单靠 pid 不够——历史 interrupted 记录里残留的 pid 可能被新进程复用，组合条件消歧 |
| 超时与撞车 | 约 10s 未命中且子进程已退出 → 读 stderr 临时文件回传启动失败原因；同秒并发启动同 workflow 的 run id 撞车由 `store.CreateRun` 拒绝、子进程报错退出 | 超时但子进程仍在跑时**文案不得误报失败**——run 可能正常在跑，提示去运行列表核对 |

## 监控机制（手动刷新，不自动轮询）

running 的 run 由**另一个进程**（self-exec 子进程，或用户在终端手起的 run）追加写 `trace.jsonl` 与原子重写 `run.json`。UI **不自动轮询**：运行列表页与运行详情页各提供一个**刷新按钮**，点击即全量重取当前视图（语义上就是「手动执行一次 `run show --json --trace`」）。取舍：单步动辄数秒到分钟，自动轮询买不到多少体验、却要引入轮询节奏 / 可见性暂停 / 长连接等复杂度；手动刷新最简单、也最诚实——页面显示的永远是「上次刷新时刻」的真实状态，不假装实时。

刷新读取的两条工程铁律（评审阶段逐条核实过的坑）：

1. **进度 k 用流式换行计数**：列表页为每个 running run 数 trace 行数时，按换行符流式扫描（`store.CountTrace`），**禁止** `LoadTrace` 全量 JSON 解析只为数个行数（单行可达 MB 级）。
2. **trace 全量读只认完整行**：详情页刷新走全量 `LoadTrace`，实现按 `\n` 逐行读、末尾无换行的半行（另一进程 `AppendTrace` 未写完）丢弃不解析，防解析假损坏；不设 16MB 行长上限（trace 单行可达 MB 级）。

每次刷新的响应都**重算派生态**（`running` 但 pid 已死 → `interrupted`，与 `run show` 同逻辑），刷新即可见状态降级，不假装还在跑。响应带 `Cache-Control: no-store`（防浏览器缓存让刷新失真）；列表页刷新可裁掉 `workflowSnapshot` / `artifacts` 大字段（等价 `run list`）。

## 前端技术栈

**原生 ES Modules + 手写 DOM 渲染，零构建步骤、零 node 工具链**；按「基础能力用成熟开源库」原则，vendor 极少量 MIT 单文件库（进仓库、随 go:embed 走，不引 CDN）。

- `internal/ui/assets/` 下放 index.html、若干 ESM 源文件（router / api / 各页面模块 / 文案字典）、style.css 与 `vendor/`——**源文件即产物**，`//go:embed assets` 进二进制（与 help 文档内嵌同一分发策略），`go install` 即得完整 UI。
- vendor 三个库（不手搓基础能力）：**marked**（运行总结的 Markdown 渲染）、**Prism + 自定义占位符 token**（提示词 md 源码高亮、JSON 高亮、`{{占位符}}` 着色）、**DOMPurify**（marked 输出注入 DOM 前消毒——运行总结含半可信引擎产物，防 XSS）。
- 提示词编辑器实现：等宽 textarea + 背后同步滚动的高亮层（Prism 着色），源码模式；若编辑体验不够，B 计划升级 vendor CodeMirror。页面框架若复杂度失控，B 计划 vendor 单文件 petite-vue；默认都不引。
- 严格自包含：无 CDN 引用（内网 / 离线可用）。
- **主题预留**：全部颜色经 `:root` CSS 变量（色板 / 间距 / 字号设计令牌），当前只发 light 值；未来 dark 仅加一段 `[data-theme=dark]` 变量覆盖。
- **语言预留**：全部界面文案收敛进单个 zh-CN 字典模块（key 引用），未来中英切换即换字典。诚实边界：服务端下发的校验错误、store 错误是 CLI 语义**原文**（与 CLI stderr 逐字一致本就是本设计的卖点），显式声明**不进翻译范围**——否则「预留」是自欺。

## API 设计（无独占能力对照表）

全部挂 `/api/` 下、JSON 收发。每个端点的能力面都不超出其 CLI 等价物：

| 方法 & 路径 | CLI 等价 | 说明 |
| --- | --- | --- |
| `GET /api/workflows` | `workflow list --json` | 另附每项 running 计数（run 表 join，等价 `run list --json` 过滤聚合） |
| `POST /api/workflows` | `workflow create <name>` | body `{name}`，脚手架骨架；同名 409。UI 不做 `--definition` 导入（CLI 专属路径） |
| `GET /api/workflows/{name}` | `workflow show <name> --json` | 规范化定义 |
| `PUT /api/workflows/{name}` | `cat def.json \| workflow edit <name>` | 整体替换；校验不过 422 + 错误数组、不落盘；携带 updatedAt 基线做乐观冲突提示；导入体 `name` 与目标不一致按规格拒绝 |
| `POST /api/workflows/{name}/rename` | `workflow rename <old> <new>` | body `{newName}`；占用 409、非法名 400；runs 分毫不动 |
| `DELETE /api/workflows/{name}` | `workflow delete <name> --yes` | UI 确认弹窗承担交互确认职责 |
| `POST /api/workflows/{name}/runs` | `workflow run <name> --cwd <dir>`（stdin 喂需求） | self-exec 分离子进程；202 返回 `{runId}`；spawn 前预检（定义校验 / 需求非空 400 / 目录不存在 400） |
| `GET /api/runs?workflow=&status=` | `run list --json` | 过滤等价 jq 筛选；running 项附进度 k；interrupted 读时派生 |
| `GET /api/runs/{id}` | `run show <id> --json` | run.json 规范化内容（含 snapshot / artifacts / failedStep / error 全文） |
| `GET /api/runs/{id}?trace=1` | `run show <id> --json --trace` | 每条含 input / output / error **全文**；刷新按钮全量重取（无 `since` 增量，见〈监控机制〉） |
| `GET /api/runs/{id}/summary` | `run show --json --trace` 的可读投影 | 返回运行总结 Markdown 文本（`run` 收尾由 `run.RenderSummary` 从 run.json + trace 确定性生成的同一份内容）；running 时 404（尚未生成，如实） |
| `POST /api/runs/{id}/stop` | `conduct run stop <id>`（新增命令） | 先向进程组、非组长回退单进程发 SIGTERM；非 running / pid 已死 → 409 |
| `GET /api/engines` | （无 CLI 命令：服务端直读能力表，只读信息性端点豁免） | 注册引擎 + 能力表（AllowsModel / EffortField / EffortValues），检查器下拉数据源；`ok=false`（能力表待实装，codex 恢复初期）以 `capability:null` 表达 |
| `GET /api/fs?path=<dir>` | （无 CLI 命令：只读目录浏览，同 `/api/engines` 属信息性端点豁免） | 启动弹窗「工作目录」选择器的数据源：列出某目录的父目录与子目录（只列目录、含隐藏目录），供图形化选目录；纯只读、无副作用 |
| `GET /api/version` | `conduct version` | 顶栏展示 |

## 需要额外实现的功能

UI 自身（`internal/ui` 服务端 + 内嵌前端 + `internal/cli/ui.go` 注册命令）之外，还需以下配套实现：

1. **【服务端直读·无需新命令】`GET /api/engines` 直接读引擎能力表**——检查器引擎 / effort 下拉的数据源，由 UI 服务端 handler 进程内组合既有导出 `engine.RegisteredNames()`（engine.go:77）+ `engine.Capability()` 生成，**不新增 `conduct engine list` 命令**（评审决策）。它是只读信息性端点，不产生任何 CLI 没有的副作用或独占能力，作为「无独占能力」不变量的显式豁免。`ok=false`（已注册但能力表待实装，如 codex 恢复初期）用 `capability: null` 表达，不得误报成 `allowsModel:false`。
2. **【命令增强·必须】`workflow run` 校验 `--cwd` 为已存在的目录**——不存在 / 不是目录即报用法错误退 `2`，不带着错误目录去烧引擎。UI 启动弹窗与 CLI 同源校验（UI 服务端预检 + CLI 自身都拦）；随实现回填规格〈workflow run〉。
3. **【内核内部增强·必须】`workflow.Validate` 结构化错误**——返回 `[]{Path, Message}` 而非拼接字符串（CLI 端 Join 后 stderr 文案**逐字不变**）。理由：编辑器「点击错误定位到字段」若靠解析 `nodes[i].字段:` 文案前缀，是建立在非契约的字符串格式上（且 `nodes 为空` 这类错误无前缀、evaluator 路径更深），内核文案微调即静默漂移。升格为 UI 动工前的硬前置。
4. **【内核内部增强·必须】`internal/store`：进度计数 + 读总结 + 全量读健壮化**——(a) 新增 `CountTrace`（流式数 `\n`、零 JSON 解析），供列表页为每个 running run 算进度 `k`，避免全量 `LoadTrace` 只为数行数（单行可达 MB 级）；(b) 新增 `ReadSummary`（现仅有 `WriteSummary`），供 `GET /api/runs/{id}/summary`，running 期尚未生成时返回哨兵 → 404；(c) 把 `LoadTrace`（runs.go:135）从 `bufio.Scanner`（16MB 行长上限、且会把末尾半行当完整行）迁到按 `\n` 逐行读，消除超长行崩溃与半行假损坏——详情页刷新走的正是全量 `LoadTrace`。**不做**增量 tail 读取（改手动刷新后无 `since` 游标，见〈监控机制〉）。
5. **【新增命令·必须】`conduct run stop <id>`**——向 run 的 pid 进程组发 SIGTERM，规格见 cli-runtime.md〈run stop〉。理由：为运行详情页「终止运行」按钮提供 CLI 等价物（无独占能力）；对纯 CLI 用户同样是刚需（此前只能手动 kill pid）。
6. **【命令增强·可选，不阻塞】`workflow run` 开跑即输出 run id**（人类模式首行 / `--json` 首行 start 事件）——UI 的 pid 组合匹配可退化为兜底；对纯 CLI 用户独立有价值（不必等运行结束就能另开终端 `run show` 跟进）。主方案零改动可用，此条可延后。
7. **【规格修订·随实现同步】`cli-tooling.md` / `cli-runtime.md`**——`cli-tooling.md`〈ui〉一节按本文〈命令形态〉定案待定项；`cli-runtime.md`〈workflow run〉回填 `--cwd` 存在性校验与「管道喂入空需求」的边界行为（当前只规定了 TTY 无参数退 2；UI 侧已双重拦截，CLI 侧行为值得明确）。

## 明确不做（与理由）

- **run 的删除按钮**：CLI 无 `run delete`，UI 不发明（无独占能力）。（终止已随新增命令 `run stop` 进入 UI，见〈需要额外实现的功能〉。）
- **自由画布 / 拖拽连线**：数据模型是线性 nodes 数组 + 两种受限循环，紧凑概览列 + 回跳弧线已完整表达；自由连线是超出模型的装饰性工程。但**节点卡片支持拖拽排序**（pointer 拖拽，只是数组换序），不属于自由连线。
- **新建弹窗的「导入定义 JSON」**：导入是 CLI 专属路径（`create --definition`），UI 新建只留最短路径（名称 → 骨架 → 进编辑器改）。
- **trace / 提示词的富文本渲染**：input / output / 提示词保持等宽源码呈现（高亮不改内容）；富文本渲染仅用于「运行总结」这份本来就写给人读的报告。
- **字段级 PATCH 编辑**：CLI 只有整体替换语义，UI 保存同构为 PUT 完整定义。
- **账号鉴权**：v1 只绑 127.0.0.1 + Host/Origin 校验（边界如〈命令形态〉所述）。
- **dark 主题 / 英文界面**：本阶段不做，预留机制见〈前端技术栈〉。

## 已知限制（诚实标注）

- **pid 判活的固有局限**：pid 被无关新进程复用时 interrupted 可能误判回 running——与 `run show` 同语义、照实展示，属内核已知局限，UI 不遮掩也不越权修复。
- **并发编辑是 last-write-wins**：乐观基线提示能缓解、不能杜绝覆盖，与 `edit` 整体替换语义一致，交互文案讲清。
- **表单 ↔ JSON 切换清空撤销历史**：浏览器原生 undo 栈随 DOM 重建清空，界面提示告知。
- **监控新鲜度取决于手动刷新**：页面显示的是上次刷新时刻的状态，不自动更新；对步级（秒~分钟）进展，手动刷新足够。

## 实现落点（包结构）

```
internal/ui/            # 新包：HTTP 服务端
├── server.go           # 启动、绑定、store 探测、Host/Origin 校验
├── handlers.go         # /api/* 路由（薄映射到 store / workflow / engine）
├── launch.go           # self-exec 发射器（Setsid / stdin / 预检 / run id 匹配）
└── assets/             # 内嵌前端（index.html / *.js / style.css / zh-CN 字典 / vendor/）
internal/cli/ui.go      # 注册 conduct ui 命令（--port / --open）
```

自检口径不变：`make fmt && make vet && make test && make build` 全绿；服务端 handler 与发射器可注入 store / 时钟单测，前端逻辑以浏览器手工用例（docs/test-cases/ 补 ui 篇）验收。
