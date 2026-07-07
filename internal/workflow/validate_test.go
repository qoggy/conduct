package workflow

import (
	"strings"
	"testing"
)

func baseNode() Node {
	return Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "做事"}
}

func oneNode(n Node) *Definition { return &Definition{Nodes: []Node{n}} }

func withEngineConfig(engineName string, config *EngineConfig) *Definition {
	n := baseNode()
	n.Engine = engineName
	n.EngineConfig = config
	return oneNode(n)
}

func loopCount(n int) *int { return &n }

// TestValidateFixturesPass 确认自带 fixtures 校验通过（避免回归时悄悄写坏样例）。
func TestValidateFixturesPass(t *testing.T) {
	for _, name := range []string{"wf_autopilot.json", "wf_demo.json"} {
		def := loadFixture(t, name)
		if err := Validate(def); err != nil {
			t.Errorf("%s 应校验通过，却报错:\n%v", name, err)
		}
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name   string
		def    *Definition
		substr string
	}{
		{"空 nodes", &Definition{}, "不能为空"},
		{"id 重复", &Definition{Nodes: []Node{baseNode(), baseNode()}}, "重复"},
		{"id 数字开头非法", oneNode(Node{ID: "1x", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}), "非法"},
		{"缺 displayName", oneNode(Node{ID: "a", Engine: "claude-code", PromptTemplate: "x"}), "displayName"},
		{"缺 promptTemplate", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code"}), "promptTemplate"},
		{"未知引擎", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "nope", PromptTemplate: "x"}), "未知引擎"},
		{"codex reasoningEffort 非法值", withEngineConfig("codex", &EngineConfig{ReasoningEffort: "insane"}), "允许集"},
		{"codex 不认 effort", withEngineConfig("codex", &EngineConfig{Effort: "high"}), "不认 effort"},
		{"claude-code effort 非法值", withEngineConfig("claude-code", &EngineConfig{Effort: "insane"}), "允许集"},
		{"claude-code 不认 reasoningEffort", withEngineConfig("claude-code", &EngineConfig{ReasoningEffort: "high"}), "不认 reasoningEffort"},
		{"antigravity 不认 effort", withEngineConfig("antigravity", &EngineConfig{Effort: "high"}), "不认 effort"},
		{"qoder reasoningEffort 非法值", withEngineConfig("qoder", &EngineConfig{ReasoningEffort: "insane"}), "允许集"},
		{"qoder 不认 effort", withEngineConfig("qoder", &EngineConfig{Effort: "high"}), "不认 effort"},
		{"evaluator 与 redoTarget 并存", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}, RedoTarget: "a"}), "互斥"},
		{"redoTarget 指向后节点", &Definition{Nodes: []Node{
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x", RedoTarget: "b"},
			{ID: "b", DisplayName: "乙", Engine: "claude-code", PromptTemplate: "y"},
		}}, "在其后或即本身"},
		{"redoTarget 不存在", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x", RedoTarget: "ghost"}), "不存在"},
		{"模板引用不存在节点", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "看 {{ghost}}"}), "不存在的节点"},
		{"未知系统变量", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.foo}}"}), "未知系统变量"},
		{"loopCount 越界", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}, LoopCount: loopCount(0)}), "1–20"},
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
	def := oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: `字面量 \{{ghost}}`})
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
	cases := []struct {
		name        string
		def         *Definition
		wantPath    string
		wantMsgPart string
	}{
		{"空 nodes 落在 nodes", &Definition{}, "nodes", "不能为空"},
		{"缺 id 落在 nodes[0].id", oneNode(Node{DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}), "nodes[0].id", "必填"},
		{"id 非法落在 nodes[0].id", oneNode(Node{ID: "1x", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}), "nodes[0].id", "非法"},
		{"id 重复落在 nodes[1].id", &Definition{Nodes: []Node{baseNode(), baseNode()}}, "nodes[1].id", "重复"},
		{"缺 displayName 落在 nodes[0].displayName", oneNode(Node{ID: "a", Engine: "claude-code", PromptTemplate: "x"}), "nodes[0].displayName", "必填"},
		{"缺 engine 落在 nodes[0].engine", oneNode(Node{ID: "a", DisplayName: "甲", PromptTemplate: "x"}), "nodes[0].engine", "必填"},
		{"未知引擎落在 nodes[0].engine", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "nope", PromptTemplate: "x"}), "nodes[0].engine", "未知引擎"},
		{"effort 非法落在 engineConfig.effort", withEngineConfig("claude-code", &EngineConfig{Effort: "insane"}), "nodes[0].engineConfig.effort", "允许集"},
		{"互斥落在 nodes[0]", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}, RedoTarget: "a"}), "nodes[0]", "互斥"},
		{"redoTarget 不存在落在 nodes[0].redoTarget", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x", RedoTarget: "ghost"}), "nodes[0].redoTarget", "不存在"},
		{"模板引用落在 nodes[0].promptTemplate", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "看 {{ghost}}"}), "nodes[0].promptTemplate", "不存在的节点"},
		{"loopCount 落在 nodes[0].loopCount", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code", PromptTemplate: "e"}, LoopCount: loopCount(0)}), "nodes[0].loopCount", "1–20"},
		{"evaluator promptTemplate 落在 nodes[0].evaluator.promptTemplate", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x",
			Evaluator: &Evaluator{Engine: "claude-code"}}), "nodes[0].evaluator.promptTemplate", "必填"},
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
