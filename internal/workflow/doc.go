// Package workflow 承载 workflow 定义的数据模型与解释器内核。
//
// 规划中的职责（尚未移植）：
//   - 定义 workflow 定义 schema（节点、评测官、循环 / 回跳等）；
//   - expand：把节点图展开成确定性的线性执行步骤序列；
//   - render：模板变量（{{sys.x}} / {{nodeId}}）替换；
//   - 主循环：串联各步产物与反馈，逐步驱动 engine 包中的引擎执行。
package workflow
