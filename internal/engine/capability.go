package engine

// EngineCapability 描述一个引擎接受的 engineConfig 形态（判别联合的「合法字段表」）。
type EngineCapability struct {
	// AllowsModel 指示是否接受 model 字段。
	AllowsModel bool
	// ModelSuggestions 是 model 字段的常见建议值，仅供 UI 展示，不是白名单。
	ModelSuggestions []string
	// AllowsEffort 指示是否接受统一的 effort 字段。
	AllowsEffort bool
	// EffortValues 是 effort 的合法取值集；AllowsEffort=false 时为空。
	EffortValues []string
}
