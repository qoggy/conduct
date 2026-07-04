package engine

// EngineCapability 描述一个引擎接受的 engineConfig 形态（判别联合的「合法字段表」）。
type EngineCapability struct {
	// AllowsModel 指示是否接受 model 字段。
	AllowsModel bool
	// EffortField 是该引擎接受的调优字段名：
	//   "effort"          —— claude-code
	//   "reasoningEffort" —— qoder（codex 恢复后同用此字段，但各自的允许集不同）
	//   ""                —— 无独立调优字段（如 antigravity，其推理强度编码在 model 标签后缀里）
	EffortField string
	// EffortValues 是 EffortField 的合法取值集；EffortField 为空时为 nil。
	EffortValues []string
}

// engineCapabilities 是各引擎的 engineConfig 能力表。已登记引擎（见各 *.go 的 init 注册）中
// 未在此列出者（能力表待实装），一律不接受任何 engineConfig 字段（见 workflow.Validate）。
var engineCapabilities = map[string]EngineCapability{
	"claude-code": {
		AllowsModel:  true,
		EffortField:  "effort",
		EffortValues: []string{"low", "medium", "high", "xhigh", "max", "ultracode", "auto"},
	},
	"antigravity": {
		// antigravity（agy）没有独立 effort 字段：推理强度编码在 model 标签后缀里
		// （如 "Gemini 3.5 Flash (Medium)" / "Claude Opus 4.6 (Thinking)"），故仅接受 model。
		// 见 docs/references/agy-print.md。
		AllowsModel: true,
		EffortField: "",
	},
	"qoder": {
		// Qoder CLI（qodercli）：-m/--model 接受模型名或档位（Auto/Ultimate/Performance/…，
		// 见 --list-models），--reasoning-effort 是与模型解耦的独立标志。
		// 见 docs/references/qodercli-print.md。
		AllowsModel:  true,
		EffortField:  "reasoningEffort",
		EffortValues: []string{"disabled", "off", "none", "low", "medium", "high", "xhigh", "max"},
	},
	// codex 暂时下线（账户欠费，下周恢复）：恢复时把它加回注册表（codex.go）与此处能力表——
	//   "codex": {AllowsModel: true, EffortField: "reasoningEffort", EffortValues: []string{"low", "medium", "high", "xhigh"}},
}

// Capability 返回某引擎的 engineConfig 能力表；未登记能力表时 ok=false。
func Capability(name string) (EngineCapability, bool) {
	capability, ok := engineCapabilities[name]
	return capability, ok
}
