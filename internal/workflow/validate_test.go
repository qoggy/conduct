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
		{"codex 已下线", oneNode(Node{ID: "a", DisplayName: "甲", Engine: "codex", PromptTemplate: "x"}), "未知引擎"},
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

// TestValidateEscapedTemplateNotChecked 确认转义 \{{x}} 不参与引用校验。
func TestValidateEscapedTemplateNotChecked(t *testing.T) {
	def := oneNode(Node{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: `字面量 \{{ghost}}`})
	if err := Validate(def); err != nil {
		t.Errorf("转义模板不应校验引用，却报错: %v", err)
	}
}
