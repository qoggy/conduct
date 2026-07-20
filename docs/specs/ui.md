# conduct ui 设计方案

> 本文是 `conduct ui` 的**设计方案（面向评审）**。事实基线：[cli-authoring.md](./cli-authoring.md)（〈设计前提〉〈数据模型〉〈workflows/ 落盘结构〉〈落盘校验规则〉）、[cli-runtime.md](./cli-runtime.md)（〈数据模型〉〈runs/ 落盘结构〉〈workflow run〉〈run list/show/stop/wait/resume/rm〉）、[cli-tooling.md](./cli-tooling.md)〈ui〉。工作流模型为**节点 + 边的并行 DAG**（含保留标记节点 `START` / `END`）。文中每个界面能力都标注了 CLI 等价物；UI 之外的配套能力集中列在〈配套实现状态〉。配套的可视化分镜见 [../proposals/parallel-dag-workflow-ui.html](../proposals/parallel-dag-workflow-ui.html)。

## 设计取向

**UI 是 CLI 动词的直白镜像。** 页面与 noun 一一对应、表格与 CLI 表格同列、编辑聚合 `node add` / `node rm` / `node set` / `edge` / `edit` 诸命令、监控刷新等价于手动执行一次 `run show`。宁可朴素，不可花哨——目标用户是程序员，可预测性本身就是体验。

五条铁律，贯穿所有页面：

1. **UI 无独占能力**（规格不变量）：每个界面动作都有对应 CLI 命令；CLI 没有的，UI 也没有。反向不要求：CLI 有而 UI 不做是允许的（如 `create --definition` 导入）。
2. **该看的全文可见**：提示词、每节点 input/output 一律**全文**呈现；性能手段只允许「默认折叠、点开才渲染」，**不允许缩略内容**。
3. **不该看的不出现**：`~/.conduct`、`trace.jsonl`、`run-summary.md` 等内部文件路径字样全站不出现（`cwd` 是用户自己传的运行参数，属「该看的」，照常展示）。
4. **fail-loud 同源**：校验、错误全部复用内核同一套逻辑与文案，UI 不复刻第二套规则（前端为即时反馈另有一份**镜像**的结构校验 / 环检测，须与 Go 版同步，见〈工作流编辑器〉）。
5. **文案克制**：界面不写教学式解释——引擎差异由表单结构自己表达（该引擎没有的字段就不渲染）；不加冗余修饰后缀（「输入」就是输入，不写「输入（完整）」）。

## 命令形态（`conduct ui` 自身）

```
conduct ui [--port <n>] [--open]
```

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--port <n>` | 整数 | `7420` | 监听端口；固定非零端口被占则 stderr 报错退出 `1`（不自动递增）；传 `0` 时由操作系统分配空闲端口，stdout 与 Host / Origin 白名单使用实际端口 |
| `--open` | 布尔 | `false` | 启动后自动打开浏览器；默认不开（照顾 SSH / 无头环境），仅打印地址 |

- 服务**只绑定 `127.0.0.1`**，不监听 `0.0.0.0`。
- **启动时主动探测一次 store 可读性**（执行一次 List）：不可读 → stderr 报原因退出 `1`（承规格「store 不可读 → 退 1」，不做「启动假成功、首个请求才报错」）。
- 启动后 stdout 打印入口地址，进程驻留至 `Ctrl-C`（承规格既有约定）。
- **v1 不做账号鉴权**，但所有 `/api/*` 校验 `Host` / `Origin` 白名单、变更类端点仅接受 `application/json`，且对所有响应加 `X-Frame-Options: DENY` / `X-Content-Type-Options: nosniff`（防点击劫持与 MIME 嗅探）。诚实边界：这防的是**浏览器跨站**（恶意网页 fetch 本地端口），**不防本机进程**——单用户本机工具下可接受，定案时写进规格。
- `cli-tooling.md`〈ui〉已按本节同步：命令、启动机制与安全边界均已定案并落地。

## 信息架构

顶栏保留与两张业务表对应的两个主导航，右侧另放全局设置入口：

```
┌──────────────────────────────────────────────┐
│ conduct    [工作流] [运行]        v0.1.0 [设置]│
├──────────────────────────────────────────────┤
│                                              │
│   #/workflows        工作流列表（默认路由）   │
│   #/workflows/:name  工作流编辑器（图编辑器） │
│   #/runs             运行列表                 │
│   #/runs/:id         运行详情（DAG 进度）     │
│   #/settings         全局语言与主题设置       │
│                                              │
└──────────────────────────────────────────────┘
```

hash 路由（单 index.html，embed 静态服务无需 history fallback，刷新 / 书签稳定）。跳转关系：

- 工作流列表行 → 编辑器；列表行「运行」/ 编辑器顶栏「运行」→ 启动弹窗 → 成功后自动跳转该运行详情；编辑器「运行历史」→ `#/runs?workflow=<name>`。
- 运行列表行 → 运行详情；运行详情头部的工作流名 → 回编辑器（该 workflow 已被删除 / 改名时名字不可点——快照语义下的诚实状态，冻结定义仍可在本页查看）。

### 设置（`#/settings`）

顶栏右侧齿轮入口进入独立设置页，顶栏不再直接摆放语言下拉和主题切换按钮。页面只包含两个全局设置项：语言（跟随环境 / 中文 / English）与主题（跟随系统 / 浅色 / 深色）。两项均复用 `custom-select.js` 的自定义下拉，切换成功后立即应用；失败则保持原值并展示本地化产品层概要与固定英文技术详情。持久化 schema、API 与 fail-loud 规则见 [i18n.md](./i18n.md)〈Web UI 产品文案与设置〉。

## 页面设计

### 工作流列表（`#/workflows`）

`workflow list` 的表格镜像，兼 create / rename / copy / delete / run 五个动词的入口。

- 表格列 `名称 | 节点流 | 最近修改`，与 CLI 表格同列（随 cli-authoring.md〈workflow list〉：节点列展示 **agent 节点 id 流、不含 `START` / `END`**）；**行序按 `最近修改` 倒序**（`updatedAt` 降序，相同再按名字升序兜底——与 CLI `workflow list`、`run list` 的时间倒序同源同序）。节点流以中性 chip 呈现（`plan, code, test, review`），超过 6 个折叠为前 6 个 + `+N`。另加 `运行中` 徽标列（该 workflow 下 `status=running` 的 run 数，来自 run 表 join——规格〈数据模型〉明示支持此聚合，等价 `run list --json` 过滤）。行点击进入编辑器。
- **行内操作**：**运行**（打开启动弹窗，见下）· **改名**（弹窗，= `rename`，文案明示「已有运行记录保留旧名」）· **复制**（弹窗，= `copy`，新名输入预填 `<原名>-copy`，文案明示「复制定义主体（节点 + 边），生成一份新名称的工作流；原工作流与其运行记录不受影响」；目标重名 409、非法名 400）· **删除**（确认弹窗，= `delete --yes`；文案明示「运行记录不受影响」）。
- **新建工作流**：弹窗只有一个名称输入（前端即时校验 `[A-Za-z0-9._-]+`），确认即以最小骨架创建（= `create`，`START → node-1 → END`）并**直接进入编辑器**。不提供「导入定义 JSON」：那是 CLI 专属路径（`create --definition`）。
- store 为空 → 空态引导新建；store 里有解析失败的坏文件 → 页顶警告条如实列出（对齐 `store.List` 的 skipped 语义，不静默隐藏）。

**启动运行弹窗**（= `workflow run <name> --cwd <dir>`，需求经 stdin）：

- 字段两个：**需求**（多行 **markdown 编辑器**，与提示词编辑器同款外观、不含占位符高亮；非空校验，前端 + 服务端双重把守，防误发空需求烧 token）、**工作目录**（自由输入，留空即用 `workflow run` 的 `--cwd` 默认值——`conduct ui` 进程的启动目录；旁挂「浏览…」目录选择器辅助定位，见下；**必须是已存在的目录、且为绝对路径**（UI 无 shell，不做 `~` 展开与相对路径拼接），服务端校验、就地报错）。
- **目录选择器**（「浏览…」弹窗，数据源 `GET /api/fs`）：从某目录出发，列出其父目录与子目录（只列目录、含隐藏目录如 `.claude`），逐级点入选定工作目录。纯只读浏览，不产生任何副作用——同 `/api/engines` 属只读信息性端点，是「无独占能力」不变量的显式豁免。
- 启动成功自动跳转运行详情页。发射机制见〈启动运行机制〉。

### 工作流编辑器（`#/workflows/:name`）

`show`（载入）+ 增删节点 / 连边 / 改字段 / 保存的聚合工作台，UI 的主用途所在。**两栏布局：左侧 DAG 画布（图），右侧检查器（详情）**——整张图一眼可见，配置细节只展开选中的那一个节点。**改动集中在画布这半边**；右侧检查器沿用现有实现、基本不改（仅去掉循环相关控件）。

**顶部条**：工作流名（旁挂改名入口）· agent 节点数 · updatedAt · 未保存改动标记 · 可运行状态 ·「表单 | JSON」视图切换 ·「运行」（同列表页启动弹窗）·「保存」。

**左栏 · DAG 画布**（忠实于「节点 + 边」模型）：

- **`START` / `END` 以虚线锚点 pill 区分**（沿用现网 `.anchor` 样式）：`START` 是唯一源（可扇出到多个节点，让工作流一开始就并行）、`END` 是唯一汇；二者不可删、不承载配置。
- 每个 agent 节点一张**紧凑卡片**（`.fnode`）：**左侧引擎品牌 icon + displayName**；节点间以带箭头的连线表达边（`from → to`）。Kiro 使用 `kiro.dev` 官方 32×32 白色幽灵紫底 PNG（`internal/ui/assets/vendor/engine-icons/kiro.png`），不走字母 `K` 回退。
- **拖拽连边**：从一个节点拖到另一个节点即连一条边；**拖出会成环的边当场红闪拒绝、不落**。删节点 / 边采用「级联删边 + 本地镜像校验即时红点 + 保存时服务端整份校验终裁」（与 `node rm` 语义一致，**不硬禁用**——否则链式图里删不掉中间节点，因为删掉后其两侧暂时悬空、须再连一条边才合法，这正是"改多处后整份 `PUT` 统一存"要支持的编辑流），仅「删到不剩任何 agent 节点」硬拦并提示。
- **画布工具条**（紧贴画布上方，随画布仅在表单视图出现；JSON 视图直接改源码）承载两个对全图的操作：
  - **＋ 节点**：新增一个 agent 节点（缺省引擎 `codex`），缺省自动接 `START → 新节点 → END`（= `node add` 不给 `--from/--to`），随即在检查器填配置；悬浮卡片右上角出现**删除**（= `node rm`，级联删边、结果校验）。
  - **整理连线**：对当前图做 **DAG 传递归约**，一次删掉所有可被更长绕行路径替代的冗余边——边 `from→to` 冗余当且仅当 `from` 另有后继能到达 `to`（例：同时存在 `START→node1→node2` 与 `START→node2` 时，`START→node2` 冗余被删）。传递归约保持可达性（传递闭包）不变，故不破坏单源单汇、不触发校验拦截；无冗余边时仅提示不改图，有则删后标记未保存并提示删除条数。

**画布展示约定（编辑页 / 运行详情页一致）**：

- 节点标签**只显 `displayName`**、左侧带 engine 图标；`node id` 不进画布标签，收在右侧检查器（`.idchip`）与节点悬浮里。id 承载语义（prompt 引用 `{{id}}`、连边取 id），但让画布保持人类可读、id 随手可查即可。
- 选中态两页**统一为节点边缘黑描边**（把节点自身描边换成 ink 单层、排在状态色之后以胜出，不叠加、不外扩）。
- 唯一差异：运行详情页节点有状态填充色，编辑页节点无填充色——运行态 vs 编辑态的固有区别，其余一律统一。

**右栏 · 检查器**（选中节点的完整配置；沿用现有实现，仅去掉循环控件）：`id`（可编辑，即时正则校验；失焦 / 回车提交时**级联改名**——把所有边的 `from/to` 与各节点模板里的 `{{旧id}}` 引用一并换成新 id，不留悬空引用；重名 / 保留名 / 非法一律拒绝回滚。CLI 侧对等命令是 `workflow node set --id`，共用同一套 `workflow.RenameNodeID` 级联语义，符合「UI 无独占能力」不变量）、`displayName`、`engine` 下拉（数据源 `GET /api/engines`，图标直接读 `iconPath`，为空时回退首字母）、`engineConfig` 按 descriptor capability **条件渲染**（`allowsModel` / `allowsEffort` 决定字段是否渲染；该引擎没有的字段不渲染、不配解释文案；model 是可手输自由文本，`modelSuggestions` 非空时挂建议下拉、非白名单）、**提示词编辑器**（markdown 源码 + 语法高亮 + `{{占位符}}` 高亮，全文可见可编辑，超高内部滚动、附最大化覆盖层；占位符入口 hover 下拉：sys 变量 + 当前定义内**祖先** agent 节点 id，点击复制）。

- 删除节点 / 边前预警其它节点对它的 `{{id}}` 模板引用（省一轮「删完→保存→报错→回改」；最终裁决仍归服务端 Validate）。

**即时校验（前端镜像 + 服务端终裁）**：服务端 `ValidateStructured` / `DetectCycle` 为**终裁**（保存走整份 `PUT /api/workflows/{name}`，不合法则 422 逐字段标错、不落盘、原定义不变）。前端（原生 JS、无 WASM）另有一份**镜像的 JS 结构校验 / 环检测**提供本地即时反馈（拖成环的边红闪拒绝、删节点 / 边使「单源单汇 / 无悬空」破坏时对涉事节点即时红点提示——不硬禁用删除，最终仍由保存时的服务端整份校验终裁）；这份 JS 校验须与 Go 版规则保持同步（现网已是此镜像模式，如 `editor.js` 的 `NODE_ID_RE` 镜像内核 `nodeIDPattern`）。

**JSON 视图**——**定义主体** `{nodes, edges}`（含 START/END）的源码编辑区（同款高亮），与图画布双向同步；作为兜底编辑模式，面向想直接改 JSON / 从外部粘贴的程序员。JSON→图切换先 parse：语法错误 → 阻止切换、就地标错。真正未知的字段（拼写错）整体保留、保存时由服务端 `DisallowUnknownFields` 拒绝（不在前端复刻字段白名单，避免与内核 schema 漂移）。元数据 `name` / `createdAt` / `updatedAt` 不属主体：即便粘进带 `definition` 外壳与这三个键的整条记录（如 `show --json` 导出），保存时一律解包 `definition`、**忽略**元数据——`name` 由路由 `{name}` 定（改名走独立 rename 入口）、时间戳由服务端管理，不因不一致报错。已知限制：视图切换重建编辑区，浏览器原生撤销（⌘Z）历史随之清空，界面提示告知。

**保存**：PUT 完整定义整体替换（= `cat def.json | conduct workflow edit <name>`），服务端复用 `ParseDefinition + Validate + store.ReplaceDefinition`——校验不过 → 422 逐条返回字段级错误、**不落盘、原定义不变**；旧定义文件只要存在，即使已无法严格解码，也可由一份合法完整定义覆盖修复。错误面板置顶列出（与 CLI stderr 逐字一致），每条按路径**点击定位**（左栏对应节点标红、右栏切到该节点并红框该字段；边错误在画布高亮对应连线）。**乐观并发提示**：保存请求携带载入时的 updatedAt 基线，服务端发现已被外部改过（CLI `edit` 或另一标签页）→ 返回冲突，前端弹「已被外部修改：覆盖保存 / 放弃重载」——软提示不做硬锁，不超出 `edit` 的 last-write-wins 语义。

> **粒度端点 vs 整份保存**：图编辑器的主保存路径是整份 `PUT`（编辑器一次改多处后统一存）。UI 无独占能力要求每个界面动作都有 CLI 等价物——增删节点 ↔ `node add` / `node rm`、连边 ↔ `edge`——但 UI 可选择用整份 `PUT` 聚合实现这些动作，或调用对应的粒度端点（`POST /nodes` / `DELETE /nodes/{id}` / `POST /edges`，见〈API 设计〉）。两条路径都在服务端复用同一套整份校验。**当前实现**：粒度端点尚未实现（见〈API 设计〉与〈配套实现状态〉），编辑器唯一走整份 `PUT`；这不违反不变量，只是暂未提供比整份替换更细的编辑接口。

### 运行列表（`#/runs`）

`run list` 的表格镜像 + 过滤。

- **running 项置顶分组**（在跑的是监控核心，不混进时间倒序），其余按开始时间倒序。
- 表格列 `RUN ID | 工作流 | 状态 | 进度 | 开始时间 | 需求`：状态徽标四态 running / completed / failed / interrupted（interrupted 为服务端按 pid 判活的**读时派生态**，与 `run show` 同逻辑）；进度列 running / interrupted 显示 `k/N`（interrupted 即中断时进度）、终态显示 agent 节点数；需求列表格内截断（**全文在详情页**）。进度分子 `k` 按 trace 中**唯一 nodeId 且 `success`** 去重计（服务端 `store.CountProgress` → `run.ProgressCount`），分母 `N` = agent 节点数——`run resume` 续跑过的 run 其 trace 含保留的失败行 + 补跑行、同一 nodeId 有多条，去重保 `k ≤ N`（见 [cli-runtime.md](./cli-runtime.md)〈run resume〉进度说明）。CLI `run list` 只输出 `NODES`，不输出此 `k/N` 进度。
- `工作流` / `状态` 两个过滤器（等价 `run list --json | jq` 聚合，无新能力）。
- 顶部提供**刷新按钮**：running 项的状态 / 进度不自动更新，点刷新即全量重取（见〈监控机制〉）。
- **行内删除**：终态记录行尾提供**删除**（确认弹窗，= `run rm <id>`，充分参考工作流删除；文案明示不可撤销）；`running` 不提供删除入口，须先「终止」，与 CLI 拒删活运行一致。
- 空态「（暂无运行记录）」。

### 运行详情（`#/runs/:id`）

`run show` / `run show --trace` 的镜像；running 时点**刷新按钮**看推进。全页不显示、不提及任何文件路径。

- **概要头**：run id · 所属工作流（链接）· 状态徽标 · **userPrompt 全文**（独立成块的 markdown，**默认折叠**、点头条展开即全文不截断，复制按钮）· cwd · **pid** · 进度 `节点 k/N` 或 耗时 · startedAt → endedAt。running 时提供**「终止运行」**（= `run stop <id>`；确认弹窗后调用，终止后按 pid 判活语义转 interrupted）；interrupted 时如实标注「进程已不在」。
- **failed / interrupted（都可恢复）**：概要头提供**「恢复」**（= `run resume <id>`，见 [cli-runtime.md](./cli-runtime.md)〈run resume〉）——两态都可恢复，续跑前沿统一由 trace 推断；确认弹窗后调用，跳过已成功节点、只补跑未完成的前沿及其下游，**同一 run 续写、id 不变**，点击后转 `running`、刷新即见推进。两态展示略有差异：`failed` 把 `error` 全文置顶、失败节点画布标红；`interrupted` 如实标注「进程已不在，尽力展示已有 trace」。只有 `completed`（无可恢复）与 `running`（存活、仍在跑）不显示恢复按钮。
- **DAG 进度画布**（取代线性执行列表）：拓扑分层、`START` 下的并行节点并排为泳道。**节点颜色即状态**（绿成功 / 蓝运行 / 灰待运行 / 红失败），标签同编辑页只显 `displayName` + 左侧 engine 图标；节点上只对有 trace 的成功 / 失败节点显示耗时，蓝色运行与灰色待运行节点不显示耗时，不再赘述状态文字、不放图例——颜色已足够表达。`START` 越过即转**绿底**（t0 完成）、`END` 仅整体完成时转绿，一眼看出进度前沿。选中态与编辑页统一的节点边缘黑描边。
- **右栏节点详情（未选中节点时整列不显示、画布在整行内居中；点画布节点才展开，再次点该节点或点画布空白即取消选中、回到居中态）**：展开该节点的 **engine · model · [effort] · [tokens] · 耗时**（`engineConfig` 声明值在详情顶部，不占画布；`tokens=null` / 缺失时省略，已知 `0` 显示 `0 tok`）+ 仅当 `sessionId` 是有效非空字符串时附**会话 id + 回放命令**。回放命令直接使用 HTTP trace 的 `sessionReplayCommand`，前端不按品牌拼接；命令非空时与 id 分两行、各自可复制，为 `null` 时只显示 id。`sessionId=null`、字段缺失或空字符串时，会话标题、id、复制按钮与 replay 整块都不渲染。随后展示**输入 / 输出（产物）全文**（`--trace` 深度）。
- **运行总结**：终态后渲染 run-summary 内容（Markdown 富文本：概要、节点表按 `startedAt` 排序、逐节点产物块——产物区的 `<output node name>` 分隔标签渲染为**小标题**、内层产物按 markdown 呈现；产物内部残留的裸 XML 标签如 `<user_prompt>` 按**字面显示**、不当 HTML 解析——否则会被当标签透传再被 DOMPurify 剥掉、内容糊成一团）——给人读的报告；标题头条可点折叠（**默认展开**——结论落地即读，与需求 / 输入 / 输出的折叠交互一致）、旁附**「复制全文」**；running 时如实提示「运行结束后生成」。数据即 `run` 收尾落盘的总结（由 `run.RenderSummary` 从 run.json + trace 确定性生成，= `run show --json --trace` 的可读投影）。
- **冻结定义**折叠区：点开渲染 workflowSnapshot 的**只读高亮 JSON**（完整记录：name / 时间戳 + `definition`{nodes / edges / START/END}，可复制），「运行开始时冻结，与当前定义可能不同」的说明收进标题旁 ⓘ 气泡。
- **手动刷新推进**：status=running 时页面提供**刷新按钮**，点击全量重取 `?trace=1`，新节点着色、进度前移；每次刷新都重算派生态（running→interrupted 的降级刷新即可见）。不自动轮询（见〈监控机制〉）。

## 启动运行机制（核心决策：self-exec 子进程）

UI 服务端以 `os.Executable()` 自呼 `conduct workflow run <name> --cwd <dir>`，**不用进程内 goroutine 跑 orchestrator**。

**goroutine 方案的三处硬伤**（否决理由）：

1. pid 判活语义被稀释：`orchestrator.Run` 写 `Pid: os.Getpid()`，进程内跑则所有 run 记同一个 ui 进程 pid——某 run 异常后 ui 还活着 → 该 run 永远显示 running，interrupted 派生失效（假活，违背 fail-loud）。
2. run 生命期与 UI 绑死：Ctrl-C 关 UI 连带杀死所有在跑 run——「关仪表盘杀运行」违反监控工具直觉；run 烧的是真金白银的 token。
3. run id 获取需改内核：`orchestrator.Run` 跑完才返回 runID，即时拿到得拆 Prepare/Execute 或扩 Observer。

**self-exec 方案**：pid = 子进程自身 pid，判活 / interrupted 语义与终端启动**逐字节一致**；「UI 启动 ≡ 执行一条 CLI 命令」是无独占能力不变量的最强证明。对 orchestrator / store / CLI 零改动。实现细节（每条都有对应的踩坑背书）：

| 细节 | 决策 | 理由 |
| --- | --- | --- |
| 脱离方式 | `SysProcAttr{Setsid: true}` 起新会话 | 彻底脱离 UI 的会话与进程组，免疫 Ctrl-C（进程组信号）与终端 SIGHUP 两条路径；Setpgid 不够 |
| userPrompt 传递 | stdin 管道写入后关闭 | 规格支持 stdin 需求；规避 argv 长度限制与转义（整份 PRD 也无压力） |
| 子进程 stdout | 重定向 `/dev/null` | 进度已逐节点落盘 trace，无需管道；**绝不可用 pipe**——UI 先退出会令子进程写 stdout 时 EPIPE，Go 运行时对 fd 1/2 的写失败重升 SIGPIPE 杀死 run |
| 子进程 stderr | 重定向 UI 会话私有临时文件 | 兜底「CreateRun 之前就失败」（store IO 等罕见路径）的原因回传；读后即弃，绝不在界面出现该路径 |
| 发射前预检 | spawn 前服务端进程内跑 `store.Load + workflow.Validate`（只读）+ 需求非空 + **工作目录存在性**校验 | 把「workflow 不存在 / 定义损坏 / 目录不存在」在起子进程前以 400/422 拦下，失败反馈从秒级缩到毫秒级 |
| run id 获取 | spawn 后轮询 run 列表，**组合条件匹配**：`workflow == 目标名 && record.pid == 子进程 pid && startedAt >= spawn 时刻（留时钟余量）` | run.json 开跑即写（含 pid），通常亚秒命中。**不要求停留在 `running`**——子进程秒级完成 / 失败、状态已转终态时它仍是刚发射的这次，仍要交回 id。单靠 pid 不够——历史 interrupted 记录残留的 pid 可能被新进程复用，故加 workflow 名 + 时钟余量消歧 |
| 超时与撞车 | 约 10s 未命中且子进程已退出 → 读 stderr 临时文件回传启动失败原因；同秒并发启动同 workflow 的 run id 撞车由 `store.CreateRun` 拒绝、子进程报错退出 | 超时但子进程仍在跑时**文案不得误报失败**——run 可能正常在跑，提示去运行列表核对 |

> **此发射器与 CLI 共用（已落地）**：`conduct workflow run -d`（`--detach`，见 [cli-runtime.md](./cli-runtime.md)〈后台运行（`-d` / `--detach`）〉）复用同一条 self-exec + setsid 发射路径——它已从 `internal/ui` 私有抽成 `internal/launch`、CLI `-d` 与 UI 共用；「UI 后台起 run」不再是独占能力。

> **恢复（`run resume`）走同一发射器**：运行详情页「恢复」经 `POST /api/runs/{id}/resume` 同样 self-exec 分离子进程（`conduct run resume <id>`）；`internal/launch` 已泛化为可发射 `workflow run` 与 `run resume`（`LaunchResume`）。因续写原 run，run id 即入参 `<id>`、**无需**上表的轮询匹配一步（改以「run.json 的 pid 被改成子进程 pid」确认接管），其余脱离 / 落盘 / pid 判活逐字节一致。

## 监控机制（手动刷新，不自动轮询）

running 的 run 由**另一个进程**（self-exec 子进程，或用户在终端手起的 run）追加写 `trace.jsonl` 与原子重写 `run.json`。UI **不自动轮询**：运行列表页与运行详情页各提供一个**刷新按钮**，点击即全量重取当前视图（语义上就是「手动执行一次 `run show --json --trace`」）。取舍：单节点动辄数秒到分钟，自动轮询买不到多少体验、却要引入轮询节奏 / 可见性暂停 / 长连接等复杂度；手动刷新最简单、也最诚实——页面显示的永远是「上次刷新时刻」的真实状态，不假装实时。

刷新读取的两条工程铁律（评审阶段逐条核实过的坑）：

1. **进度 k 按 nodeId 去重、流式解析**：列表页为每个 running run 算进度分子 `k` 时用 `store.CountProgress`——逐行流式读、每行只解出 `nodeId` / `success` 两字段（**不** materialize MB 级 input/output），按唯一 nodeId 且末次 `success` 去重（防 resume 后 `k>N`，见〈run resume〉进度说明）；**禁止** `LoadTrace` 全量 JSON 解析只为算进度。数物理行数的 `store.CountTrace` 仅表达「已落盘多少条 trace」，不作进度分子。
2. **trace 全量读只认完整行**：详情页刷新走全量 `LoadTrace`，实现按 `\n` 逐行读、末尾无换行的半行（另一进程 `AppendTrace` 未写完）丢弃不解析，防解析假损坏；不设 16MB 行长上限（trace 单行可达 MB 级）。

每次刷新的响应都**重算派生态**（`running` 但 pid 已死 → `interrupted`）。响应带 `Cache-Control: no-store`；列表页刷新可裁掉 `workflowSnapshot` / `artifacts` 大字段（等价 `run list`）。

## 前端技术栈

**原生 ES Modules + 手写 DOM 渲染，零构建步骤、零 node 工具链**；按「基础能力用成熟开源库」原则，vendor 极少量 MIT 单文件库（进仓库、随 go:embed 走，不引 CDN）。

- `internal/ui/assets/` 下放 index.html、若干 ESM 源文件（router / api / 各页面模块 / 文案字典）、style.css 与 `vendor/`——**源文件即产物**，`//go:embed assets` 进二进制，`go install` 即得完整 UI。
- vendor 三个库：**marked**（运行总结 + 节点输入/输出的 Markdown 渲染）、**Prism + 自定义占位符 token**（提示词 md 源码高亮、JSON 高亮、`{{占位符}}` 着色）、**DOMPurify**（marked 输出注入 DOM 前消毒——含半可信引擎产物，防 XSS）。
- DAG 画布用手写 SVG 渲染（节点卡片 + 边连线 + START/END 锚点），原生 pointer 事件用于点击选中与拖拽连边；节点位置由定义结构确定性自动布局，数据模型不保存坐标，当前不支持自由拖动节点。环检测 / 结构校验是镜像 Go 规则的一份 JS 实现（须与内核同步）。若图交互复杂度失控，B 计划 vendor 单文件图库；默认不引。
- 严格自包含：无 CDN 引用（内网 / 离线可用）。
- **light / dark 主题（已实现）**：全部颜色经 `:root` CSS 变量，light 为基础值，`[data-theme=dark]` 覆盖同名颜色令牌；页面以 `<html data-theme="light|dark">` 作为唯一生效主题状态。dark 采用独立设计的深蓝灰工作台层级，不做 light 色值反转。持久化使用 `settings.json.theme`；属性缺失时跟随系统 `prefers-color-scheme`，显式值为 `light` / `dark`。服务端把偏好注入首页 `<html>` 数据属性，样式表前脚本同步解析，避免刷新闪烁。
- **语言切换（已实现）**：全部可见产品文案进入 key 集合一致的 `zh-CN` / `en` 字典；语言选择位于设置页并复用自定义下拉，仍按 `settings.language` > locale 环境变量 > 英文解析。服务端领域错误使用稳定错误码 + 参数，由页面本地化；底层技术详情仍固定英文。完整规则见 [i18n.md](./i18n.md)。

## API 设计（无独占能力对照表）

全部挂 `/api/` 下、JSON 收发。每个端点的能力面都不超出其 CLI 等价物：

| 方法 & 路径 | CLI 等价 | 说明 |
| --- | --- | --- |
| `GET /api/workflows` | `workflow list --json` | 每项含 agent 节点 id 流（不含 START/END）+ running 计数（run 表 join） |
| `POST /api/workflows` | `workflow create <name>` | body `{name}`，脚手架骨架（START→node-1→END）；同名 409 |
| `GET /api/workflows/{name}` | `workflow show <name> --json` | 规范化完整记录，含 `name` / 时间戳与 `definition`（`nodes`（含 START/END）+ `edges`） |
| `PUT /api/workflows/{name}` | `cat def.json \| workflow edit <name>` | 整体替换 `definition`（body 为 `{nodes, edges}`，给整条记录则解包 `definition`）；校验不过 422 + 错误数组、不落盘；携带 updatedAt 基线做乐观冲突提示；多带的 `name` / 时间戳元数据一律忽略、不因不一致报错 |
| `POST /api/workflows/{name}/nodes`（**未实现，延后**） | `workflow node add` | body `{id, engine, displayName, prompt?, from?[], to?[]}`（缺省 from/to 接 START/END）；末尾整份校验、不过 422；id 为保留名 / 已存在 409。当前编辑器增删节点走整份 `PUT` 完成，不影响 UI↔CLI 能力对等（UI 仍是 CLI 能力子集） |
| `DELETE /api/workflows/{name}/nodes/{id}`（**未实现，延后**） | `workflow node rm` | 级联删边、结果校验；START/END 拒删 409；结果孤立 422。当前编辑器删节点走整份 `PUT` 完成 |
| `POST /api/workflows/{name}/edges`（**未实现，延后**） | `workflow edge --add/--rm` | body `{add:[{from,to}...], rm:[{from,to}...]}`（原子批量，可含 START/END）；末尾整份校验、不过 422。当前编辑器改连边走整份 `PUT` 完成 |
| `POST /api/workflows/{name}/rename` | `workflow rename <old> <new>` | body `{newName}`；占用 409、非法名 400；runs 分毫不动 |
| `POST /api/workflows/{name}/copy` | `workflow copy <src> <dst>` | body `{newName}`；复制定义主体（nodes + edges）为新托管对象；源不存在 404、目标占用 409、非法名 400；runs 分毫不动 |
| `DELETE /api/workflows/{name}` | `workflow delete <name> --yes` | UI 确认弹窗承担交互确认职责 |
| `POST /api/workflows/{name}/runs` | `workflow run <name> --cwd <dir>`（stdin 喂需求） | self-exec 分离子进程；202 返回 `{runId}`；spawn 前预检（定义校验 / 需求非空 400 / 目录不存在 400） |
| `GET /api/runs?workflow=&status=` | `run list --json` | 过滤等价 jq 筛选；running 项附进度 k；每项含 `nodeCount`；interrupted 读时派生 |
| `GET /api/runs/{id}` | `run show <id> --json` | run.json 规范化内容（含 snapshot（nodes+edges）/ artifacts / error 全文与可选 `failedNodeId`；无 `steps` / `failedStep` 字段——进度分母按 agent 节点数，失败节点直接读 `failedNodeId`，恢复前沿仍由 trace 推断） |
| `GET /api/runs/{id}?trace=1` | `run show <id> --json --trace` | 每条含 input / output / error **全文** + `startedAt` / `endedAt`；`tokens` / `sessionId` 始终存在，未知值为 `null`；共享 `TraceView` 增加读时派生的 `sessionReplayCommand`（无命令为 `null`，不落盘） |
| `GET /api/runs/{id}/summary` | `run show --json --trace` 的可读投影 | 返回运行总结 Markdown 文本；running 时 404（尚未生成，如实） |
| `POST /api/runs/{id}/stop` | `conduct run stop <id>` | 先向进程组、非组长回退单进程发 SIGTERM；非 running / pid 已死 → 409 |
| `POST /api/runs/{id}/resume` | `conduct run resume <id>` | self-exec 分离子进程从中断处续跑（复用 `internal/launch`）；`failed` 或 `interrupted` 可恢复，`completed` / `running`（存活）→ 409；202 返回 `{runId}`（即原 id） |
| `DELETE /api/runs/{id}` | `conduct run rm <id>` | UI 确认弹窗承担交互确认职责；仅终态可删——`running`（存活）→ 409、不存在 → 404、非法 id → 400；成功 204 无 body |
| `GET /api/engines` | （无 CLI 命令：只读信息性端点豁免） | 按 name 排序返回 descriptor 的可序列化部分：`name`、必为对象的 `capability`（`allowsModel` / `modelSuggestions` / `allowsEffort` / `effortValues`）与 `iconPath`；两个列表无值时固定为 `[]`、不输出 `null`；函数不序列化 |
| `GET /api/fs?path=<dir>` | （无 CLI 命令：只读目录浏览豁免） | 启动弹窗「工作目录」选择器的数据源：列出某目录的父目录与子目录（只列目录、含隐藏目录）；纯只读、无副作用 |
| `GET /api/version` | `conduct version` | 顶栏展示 |
| `GET /api/settings` | （无 CLI 命令：全局配置基础设施豁免） | 返回显式 `language`、当前 `resolvedLanguage` 与显式 `theme`；损坏设置返回固定英文技术诊断，不降级 |
| `PATCH /api/settings` | （无 CLI 命令：全局配置基础设施豁免） | 严格部分更新 `language` / `theme`，`null` 删除对应属性并保留其它属性；完整 schema 见 [i18n.md](./i18n.md) |

## 配套实现状态

UI 自身（`internal/ui` 服务端 + 内嵌前端 + `internal/cli/ui.go` 注册命令）与运行时同为**并行 DAG 模型**；服务端主路径（列表 / 详情 / 保存 / 运行 / 终止 / 恢复）已按本文改造完成，唯一延后项是 node / edge 粒度端点（见第 2 条）。

1. **【已实现·服务端直读】`GET /api/engines`**——检查器引擎 / effort 下拉、model 建议和图标的数据源，由 UI 服务端直接映射 `engine.RegisteredDescriptors()`，**不新增 `conduct engine list` 命令**。只读信息性端点，作为「无独占能力」不变量的显式豁免。
2. **【未实现（延后）】node / edge 粒度端点**——`POST /api/workflows/{name}/nodes`、`DELETE …/nodes/{id}`、`POST …/edges`（镜像 `node add` / `node rm` / `edge`）**尚未实现**：`internal/ui/server.go` 当前未注册这三个路由。图编辑器的增删节点 / 连边动作走整份 `PUT /api/workflows/{name}` 主保存路径（= `workflow edit`）完成，服务端复用同一套整份校验；这不违反「UI 无独占能力」不变量（UI 能力仍是 CLI 能力的子集），只是尚未提供比整份 `PUT` 更细粒度的编辑接口。落地粒度端点时补 handler 并回收本条标注。
3. **【已实现】`workflow.Validate` 结构化错误 + `DetectCycle`**——`ValidateStructured`（`internal/workflow/validate.go`）已随新 DAG 规则重写，覆盖恰好一对 START/END、保留名、标记节点必空、边合法性、无环、度约束、祖先引用，返回 `[]Problem{Path, Code, Params}` 供 CLI/UI 按语言渲染并由编辑器定位字段；`internal/workflow/graph.go` 的 `DetectCycle` 供拖边即时拒环判定。前端另有镜像的 JS 结构校验 / 环检测（须与 Go 版同步维护）。
4. **【已实现】`internal/run` / `internal/store`：进度计数按 nodeId + 读总结 + 全量读健壮化**——(a) `run.ProgressCount` 按唯一 NodeID 去重（防 resume 后 `k>N`）；(b) `GET /api/runs/{id}/summary` 由 `run.RenderSummary` 支撑；(c) trace 读取按 `\n` 逐行解析，丢弃末尾未写完的半行，消除超长行崩溃与半行假损坏。**不做**增量 tail 读取。
5. **【已实现】`conduct run stop <id>`**——`internal/cli/run_stop.go`，向 run 的 pid 进程组发 SIGTERM，规格见 [cli-runtime.md](./cli-runtime.md)〈run stop〉。为运行详情页「终止运行」按钮提供 CLI 等价物。
6. **【已实现】`conduct run resume <id>`**——`internal/cli/run_resume.go` 已改为 DAG 续跑语义（trace 推断 `done` 集 + 前驱解锁，复用并行调度循环），规格见 [cli-runtime.md](./cli-runtime.md)〈run resume〉。为运行详情页「恢复」按钮提供 CLI 等价物；`internal/ui/launch.go` 的 `POST /api/runs/{id}/resume` 复用同一 `internal/launch` 发射器。
7. **【命令增强·可选，不阻塞】`workflow run` 开跑即输出 run id**——UI 的 pid 组合匹配可退化为兜底；对纯 CLI 用户独立有价值。主方案零改动可用，此条可延后，不受本次模型改造影响。
8. **【已同步】`cli-tooling.md`**——〈ui〉一节已与本文〈命令形态〉、随机端口和启动机制保持一致。
9. **【已实现】中英文界面与全局语言设置**——字典、动态 `<html lang>`、稳定领域错误码与前端本地化已实现；语言入口位于独立设置页并使用自定义下拉。
10. **【已实现】light / dark 主题**——两套颜色令牌、`settings.json.theme` 持久化、首页偏好注入和设置页控制均已实现。

## 明确不做（与理由）

- **节点自由移动 / 多选框选 / 小地图**：拖拽连边、确定性自动布局、增删节点已完整表达「节点 + 边」模型的编辑；定义没有位置字段，故不提供自由拖动与位置持久化。框选、缩略图等是超出模型的锦上添花，本阶段不做。
- **新建弹窗的「导入定义 JSON」**：导入是 CLI 专属路径（`create --definition`），UI 新建只留最短路径（名称 → 骨架 → 进编辑器改）。
- **trace / 提示词的富文本渲染**：提示词保持等宽源码呈现（高亮不改内容）；富文本 markdown 渲染用于「运行总结」和运行详情的**节点输入 / 输出**这些本就写给人读的内容。
- **字段级 PATCH 编辑节点结构化字段**：node 结构化字段（engine/model/…）UI 走整份 `PUT` 或粒度端点覆盖，不引入 JSON Patch 语义。
- **账号鉴权**：v1 只绑 127.0.0.1 + Host/Origin 校验（边界如〈命令形态〉所述）。

## 已知限制（诚实标注）

- **pid 判活的固有局限**：pid 被无关新进程复用时 interrupted 可能误判回 running——与 `run show` 同语义、照实展示，属内核已知局限。
- **并发编辑是 last-write-wins**：乐观基线提示能缓解、不能杜绝覆盖，与 `edit` 整体替换语义一致，交互文案讲清。
- **图画布 ↔ JSON 切换清空撤销历史**：浏览器原生 undo 栈随 DOM 重建清空，界面提示告知。
- **监控新鲜度取决于手动刷新**：页面显示的是上次刷新时刻的状态，不自动更新；对节点级（秒~分钟）进展，手动刷新足够。
- **前端镜像校验可能与内核漂移**：拖边即时拒环靠一份 JS 环检测，须与 Go 版规则同步维护；最终裁决始终归服务端 `Validate`（422），前端只做即时反馈。

## 实现落点（包结构）

```
internal/ui/            # HTTP 服务端
├── server.go           # 启动、绑定、store 探测、Host/Origin 校验
├── handlers.go         # 已注册的 /api/* handler（整体 workflow PUT；node / edge 粒度端点尚未实现）
├── launch.go           # UI 侧发射：HTTP 味 preflight（400/404/422）+ 调用共用发射器 + 错误映射
└── assets/             # 内嵌前端（index.html / *.js（含主题 theme.js、图编辑器 editor.js、DAG 进度 run-detail.js）/ style.css / 中英文文案字典 / vendor/）
internal/launch/        # CLI -d 与 UI 共用的发射器（Setsid / stdin / run id 组合匹配 / 有界等待）
internal/cli/ui.go      # 注册 conduct ui 命令（--port / --open）
```

自检口径不变：`make fmt && make vet && make test && make build` 全绿；服务端 handler 与发射器可注入 store / 时钟单测，前端逻辑按 [ui-frontend.md](../test-cases/ui-frontend.md) 的浏览器用例验收。
