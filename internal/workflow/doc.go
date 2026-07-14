// Package workflow 承载 workflow 定义的数据模型、图算法、落盘校验与模板渲染。
//
//   - definition.go：workflow 记录 schema（外层 Workflow 元数据 + 内层 Definition 的节点 + 边 DAG、
//     START/END 保留标记节点、engineConfig 判别联合）与严格解析（ParseDefinition 探测 definition 外壳）；
//   - graph.go：纯图算法（邻接表、环探测、祖先集、拓扑分层），供 validate / scheduler / show / UI 复用；
//   - validate.go：落盘校验（恰好一个 START/END + ≥1 agent、保留名、标记节点必空、边合法、无环、单源单汇、
//     模板引用祖先），供所有变更命令入库前强制、store 载入兜底；
//   - render.go：模板变量替换（{{sys.x}} / {{nodeId}}），运行时把节点产物串给下游；
//   - rename.go：节点改 id 的级联改名（同步边端点与模板 {{引用}}），CLI node set --id 与 UI 检查器共用。
//
// 执行编排（把 DAG 按依赖并行调度、逐节点驱动引擎、落盘运行记录）见 internal/orchestrator。
package workflow
