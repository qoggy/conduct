package workflow

import (
	"strings"
	"testing"
)

// validDef 返回最小合法 DAG：START → a → END。各测试在其上做单点破坏。
func validDef() *Definition {
	return &Definition{
		Nodes: []Node{
			{ID: "START"},
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "做事"},
			{ID: "END"},
		},
		Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
	}
}

// withEngineConfig 在 validDef 的 agent 节点上换引擎与配置。
func withEngineConfig(engineName string, config *EngineConfig) *Definition {
	def := validDef()
	def.Nodes[1].Engine = engineName
	def.Nodes[1].EngineConfig = config
	return def
}

// TestValidateFixturesPass 确认自带 DAG fixtures 校验通过（避免回归时悄悄写坏样例）。
func TestValidateFixturesPass(t *testing.T) {
	for _, name := range []string{"wf_autopilot.json", "wf_demo.json"} {
		def := loadFixture(t, name)
		if err := Validate(def); err != nil {
			t.Errorf("%s 应校验通过，却报错:\n%v", name, err)
		}
	}
}

func TestValidateValidDefPasses(t *testing.T) {
	if err := Validate(validDef()); err != nil {
		t.Errorf("最小合法 DAG 应通过，却报错:\n%v", err)
	}
	if err := Validate(diamond()); err != nil {
		t.Errorf("菱形 DAG 应通过，却报错:\n%v", err)
	}
}

func TestValidateKnownSystemVariables(t *testing.T) {
	def := validDef()
	def.Nodes[1].PromptTemplate = "{{sys.userPrompt}} / {{sys.cwd}} / {{sys.runId}}"
	if err := Validate(def); err != nil {
		t.Errorf("已知系统变量应通过校验，却报错:\n%v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	// mutate 在 validDef 上做单点破坏并返回。
	mutate := func(f func(d *Definition)) *Definition {
		d := validDef()
		f(d)
		return d
	}
	cases := []struct {
		name   string
		def    *Definition
		substr string
	}{
		{"空 nodes", &Definition{}, "不能为空"},
		{"缺 START", &Definition{
			Nodes: []Node{{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}, {ID: "END"}},
			Edges: []Edge{{From: "a", To: "END"}},
		}, "一个 START"},
		{"缺 END", &Definition{
			Nodes: []Node{{ID: "START"}, {ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}},
			Edges: []Edge{{From: "START", To: "a"}},
		}, "一个 END"},
		{"无 agent 节点", &Definition{
			Nodes: []Node{{ID: "START"}, {ID: "END"}},
			Edges: []Edge{{From: "START", To: "END"}},
		}, "agent 节点"},
		{"id 重复", mutate(func(d *Definition) {
			d.Nodes = append(d.Nodes, Node{ID: "a", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"})
			d.Edges = append(d.Edges, Edge{From: "START", To: "a"})
		}), "重复"},
		{"id 数字开头非法", mutate(func(d *Definition) {
			d.Nodes[1].ID = "1x"
			d.Edges = []Edge{{From: "START", To: "1x"}, {From: "1x", To: "END"}}
		}), "非法"},
		{"缺 displayName", mutate(func(d *Definition) { d.Nodes[1].DisplayName = "" }), "displayName"},
		{"缺 promptTemplate", mutate(func(d *Definition) { d.Nodes[1].PromptTemplate = "" }), "promptTemplate"},
		{"未知引擎", mutate(func(d *Definition) { d.Nodes[1].Engine = "nope" }), "未知引擎"},
		{"codex reasoningEffort 非法值", withEngineConfig("codex", &EngineConfig{ReasoningEffort: "insane"}), "允许集"},
		{"codex 不认 effort", withEngineConfig("codex", &EngineConfig{Effort: "high"}), "不认 effort"},
		{"claude-code effort 非法值", withEngineConfig("claude-code", &EngineConfig{Effort: "insane"}), "允许集"},
		{"claude-code 不认 reasoningEffort", withEngineConfig("claude-code", &EngineConfig{ReasoningEffort: "high"}), "不认 reasoningEffort"},
		{"antigravity 不认 effort", withEngineConfig("antigravity", &EngineConfig{Effort: "high"}), "不认 effort"},
		{"qoder reasoningEffort 非法值", withEngineConfig("qoder", &EngineConfig{ReasoningEffort: "insane"}), "允许集"},
		{"qoder 不认 effort", withEngineConfig("qoder", &EngineConfig{Effort: "high"}), "不认 effort"},
		// —— 标记节点必空 ——
		{"START 带 engine", mutate(func(d *Definition) { d.Nodes[0].Engine = "claude-code" }), "必须为空"},
		{"END 带 displayName", mutate(func(d *Definition) { d.Nodes[2].DisplayName = "尾" }), "必须为空"},
		// —— 边规则 ——
		{"边指向 START", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "a", To: "START"}) }), "指向 START"},
		{"边源自 END", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "END", To: "a"}) }), "源自 END"},
		{"START→END 直连", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "START", To: "END"}) }), "直连"},
		{"自环", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "a", To: "a"}) }), "自环"},
		{"重复边", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "START", To: "a"}) }), "重复边"},
		{"边指向不存在节点", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "a", To: "ghost"}) }), "不存在的节点"},
		// —— 无环 ——
		{"成环", &Definition{
			Nodes: []Node{{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
				{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"}, {ID: "END"}},
			Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "b"}, {From: "b", To: "a"}, {From: "b", To: "END"}},
		}, "检测到环"},
		// —— 单源单汇 / 无悬空 ——
		{"agent 无入边", &Definition{
			Nodes: []Node{{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
				{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"}, {ID: "END"}},
			Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "END"}, {From: "b", To: "END"}},
		}, "无入边"},
		{"agent 无出边", &Definition{
			Nodes: []Node{{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
				{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"}, {ID: "END"}},
			Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "END"}, {From: "START", To: "b"}},
		}, "无出边"},
		// —— 模板引用祖先 ——
		{"引用非祖先并行分支", &Definition{
			Nodes: []Node{{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
				{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "看 {{a}}"}, {ID: "END"}},
			Edges: []Edge{{From: "START", To: "a"}, {From: "START", To: "b"}, {From: "a", To: "END"}, {From: "b", To: "END"}},
		}, "非上游祖先"},
		{"模板引用不存在节点", mutate(func(d *Definition) { d.Nodes[1].PromptTemplate = "看 {{ghost}}" }), "不存在的节点"},
		{"模板引用 START", mutate(func(d *Definition) { d.Nodes[1].PromptTemplate = "{{START}}" }), "标记节点"},
		{"未知系统变量", mutate(func(d *Definition) { d.Nodes[1].PromptTemplate = "{{sys.foo}}" }), "仅支持 sys.userPrompt / sys.cwd / sys.runId"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.def)
			if err == nil {
				t.Fatalf("期望校验失败（含 %q），却通过了", c.substr)
			}
			if !strings.Contains(err.Error(), c.substr) {
				t.Errorf("错误信息应含 %q，实际:\n%v", c.substr, err)
			}
		})
	}
}

// TestValidateAntigravityModelOnly 确认 antigravity 只接受 model（effort 编码在标签里）。
func TestValidateAntigravityModelOnly(t *testing.T) {
	def := withEngineConfig("antigravity", &EngineConfig{Model: "Gemini 3.5 Flash (Medium)"})
	if err := Validate(def); err != nil {
		t.Errorf("antigravity + model-only 应通过，却报错: %v", err)
	}
}

// TestValidateQoderModelAndReasoningEffort 确认 qoder 接受 model + reasoningEffort。
func TestValidateQoderModelAndReasoningEffort(t *testing.T) {
	def := withEngineConfig("qoder", &EngineConfig{Model: "Performance", ReasoningEffort: "high"})
	if err := Validate(def); err != nil {
		t.Errorf("qoder + model + reasoningEffort 应通过，却报错: %v", err)
	}
}

// TestValidateCodexModelAndReasoningEffort 确认 codex 接受 model + reasoningEffort。
func TestValidateCodexModelAndReasoningEffort(t *testing.T) {
	def := withEngineConfig("codex", &EngineConfig{Model: "gpt-5-codex", ReasoningEffort: "high"})
	if err := Validate(def); err != nil {
		t.Errorf("codex + model + reasoningEffort 应通过，却报错: %v", err)
	}
}

// TestValidateEscapedTemplateNotChecked 确认转义 \{{x}} 不参与引用校验。
func TestValidateEscapedTemplateNotChecked(t *testing.T) {
	def := validDef()
	def.Nodes[1].PromptTemplate = `字面量 \{{ghost}}`
	if err := Validate(def); err != nil {
		t.Errorf("转义模板不应校验引用，却报错: %v", err)
	}
}

// findProblem 返回首条 Path 命中的 Problem；未命中返回零值与 false。
func findProblem(problems []Problem, path string) (Problem, bool) {
	for _, p := range problems {
		if p.Path == path {
			return p, true
		}
	}
	return Problem{}, false
}

// TestValidateStructuredPaths 锁死结构化契约：每类错误落在预期的字段点路径上（编辑器据此定位）。
func TestValidateStructuredPaths(t *testing.T) {
	mutate := func(f func(d *Definition)) *Definition {
		d := validDef()
		f(d)
		return d
	}
	cases := []struct {
		name        string
		def         *Definition
		wantPath    string
		wantMsgPart string
	}{
		{"空 nodes 落在 nodes", &Definition{}, "nodes", "不能为空"},
		{"缺 displayName 落在 nodes[1].displayName", mutate(func(d *Definition) { d.Nodes[1].DisplayName = "" }), "nodes[1].displayName", "必填"},
		{"缺 engine 落在 nodes[1].engine", mutate(func(d *Definition) { d.Nodes[1].Engine = "" }), "nodes[1].engine", "必填"},
		{"未知引擎落在 nodes[1].engine", mutate(func(d *Definition) { d.Nodes[1].Engine = "nope" }), "nodes[1].engine", "未知引擎"},
		{"effort 非法落在 engineConfig.effort", withEngineConfig("claude-code", &EngineConfig{Effort: "insane"}), "nodes[1].engineConfig.effort", "允许集"},
		{"START 带 engine 落在 nodes[0].engine", mutate(func(d *Definition) { d.Nodes[0].Engine = "claude-code" }), "nodes[0].engine", "必须为空"},
		{"边指向 START 落在 edges[2]", mutate(func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "a", To: "START"}) }), "edges[2]", "指向 START"},
		{"模板引用落在 nodes[1].promptTemplate", mutate(func(d *Definition) { d.Nodes[1].PromptTemplate = "看 {{ghost}}" }), "nodes[1].promptTemplate", "不存在的节点"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problems := ValidateStructured(c.def)
			problem, ok := findProblem(problems, c.wantPath)
			if !ok {
				t.Fatalf("期望有 Path=%q 的错误，实际 problems=%+v", c.wantPath, problems)
			}
			if !strings.Contains(problem.Message, c.wantMsgPart) {
				t.Errorf("Path=%q 的 Message 应含 %q，实际 %q", c.wantPath, c.wantMsgPart, problem.Message)
			}
			if strings.Contains(problem.Path, ": ") {
				t.Errorf("Path 不应含 %q 分隔符（会破坏字符串化重建）：%q", ": ", problem.Path)
			}
		})
	}
}
