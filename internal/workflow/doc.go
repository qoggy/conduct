// Package workflow 承载 workflow 定义的数据模型、落盘校验与展开算法。
//
// 已实现：
//   - definition.go：workflow 定义 schema（节点、评测官、engineConfig 判别联合）与严格解析 / 规范化；
//   - validate.go：落盘校验（结构、id、engineConfig 能力表、循环互斥、redoTarget 回跳、模板引用）；
//   - expand.go：把节点图展开成确定性的线性执行步骤序列（in-place 内循环 + jump-back 段循环）。
//
// 待移植（run 批）：
//   - render：模板变量（{{sys.x}} / {{nodeId}}）替换；
//   - 主循环：串联各步产物与反馈，逐步驱动 engine 包中的引擎执行。
package workflow
