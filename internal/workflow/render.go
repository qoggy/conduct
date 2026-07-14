package workflow

import "strings"

// Render 把 promptTemplate 里的变量引用替换为运行时值，语义与 Validate 的
// templateVariablePattern 同源（移植自 Python 原型 render_template）：
//   - \{{key}}    → 字面量 {{key}}（转义，不替换）
//   - {{sys.x}}   → sysVars["x"]；未注入则保留字面量 {{sys.x}}（fail-loud，不静默成空）
//   - {{nodeId}}  → artifacts[nodeId]；合法节点但未跑 → 空串
//   - 其余         → 保留字面量（非法引用不静默吞）
//
// sysVars 以裸名为键（"userPrompt" / "cwd" / "runId"，不含 "sys." 前缀）。validNodeID 判定某 id
// 是否为定义内的合法节点（与 Validate 用同一判定，避免把非法引用当节点渲染成空串）。
func Render(template string, sysVars, artifacts map[string]string, validNodeID func(string) bool) string {
	return templateVariablePattern.ReplaceAllStringFunc(template, func(matched string) string {
		if strings.HasPrefix(matched, "\\") {
			return matched[1:] // \{{key}} → 字面量 {{key}}
		}
		key := templateVariablePattern.FindStringSubmatch(matched)[1]
		if strings.HasPrefix(key, "sys.") {
			if value, ok := sysVars[strings.TrimPrefix(key, "sys.")]; ok {
				return value
			}
			return matched // 未注入的系统变量 → 保留字面量
		}
		if validNodeID(key) {
			return artifacts[key] // 合法节点：未跑取零值空串
		}
		return matched // 非法引用 → 保留字面量
	})
}
