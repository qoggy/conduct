package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture 读入 testdata 下的定义主体夹具（{nodes, edges}），供各测试复用。
func loadFixture(t *testing.T, name string) *Definition {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读取 fixture %s 失败: %v", name, err)
	}
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatalf("解析 fixture %s 失败: %v", name, err)
	}
	return def
}

func TestParseDefinitionTreatsReasoningEffortAsOrdinaryUnknownField(t *testing.T) {
	parse := func(field string) string {
		data := []byte(`{"nodes":[{"id":"a","engineConfig":{"` + field + `":"high"}}],"edges":[]}`)
		_, err := ParseDefinition(data)
		if err == nil {
			t.Fatalf("未知字段 %s 应被拒绝", field)
		}
		return strings.ReplaceAll(err.Error(), field, "<unknown>")
	}
	if reasoning, ordinary := parse("reasoningEffort"), parse("xxxabc"); reasoning != ordinary {
		t.Fatalf("reasoningEffort 应与普通未知字段走同一路径：\nreasoning=%s\nordinary=%s", reasoning, ordinary)
	}
}

func TestParseDefinitionRejectsUnknownField(t *testing.T) {
	// promptTemplate 拼错成 promtTemplate → 未知字段，应被 fail-loud 拒绝
	data := []byte(`{"nodes":[{"id":"a","displayName":"A","engine":"claude-code","promtTemplate":"hi"}]}`)
	if _, err := ParseDefinition(data); err == nil {
		t.Fatal("期望拒绝未知字段 promtTemplate，却通过了")
	}
}

func TestParseDefinitionRejectsTrailingContent(t *testing.T) {
	data := []byte(`{"nodes":[]}{"x":1}`)
	if _, err := ParseDefinition(data); err == nil {
		t.Fatal("期望拒绝多余尾随内容，却通过了")
	}
}

// TestParseDefinitionAcceptsBody 确认直接给定义主体 {nodes, edges} 原样解析。
func TestParseDefinitionAcceptsBody(t *testing.T) {
	data := []byte(`{"nodes":[{"id":"START"}],"edges":[{"from":"START","to":"a"}]}`)
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatalf("主体应可解析，却报错: %v", err)
	}
	if len(def.Nodes) != 1 || len(def.Edges) != 1 {
		t.Fatalf("解析结果不符: %+v", def)
	}
}

// TestParseDefinitionUnwrapsFullRecord 确认整条记录（show --json 输出）被解包为主体、元数据忽略。
func TestParseDefinitionUnwrapsFullRecord(t *testing.T) {
	data := []byte(`{"name":"x","createdAt":"t1","updatedAt":"t2","definition":{"nodes":[{"id":"START"}],"edges":[]}}`)
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatalf("整条记录应被解包，却报错: %v", err)
	}
	if len(def.Nodes) != 1 || def.Nodes[0].ID != "START" {
		t.Fatalf("解包结果不符: %+v", def)
	}
}

// TestParseDefinitionRejectsUnknownFieldInFullRecord 确认整条记录路径同样 fail-loud 拒绝未知外壳字段。
func TestParseDefinitionRejectsUnknownFieldInFullRecord(t *testing.T) {
	data := []byte(`{"name":"x","bogus":1,"definition":{"nodes":[],"edges":[]}}`)
	if _, err := ParseDefinition(data); err == nil {
		t.Fatal("期望拒绝整条记录里的未知字段 bogus，却通过了")
	}
}

// srcWithPointers 构造一份带 EngineConfig 指针字段的源记录，用于验证 CopyAs 深拷。
func srcWithPointers() *Workflow {
	return &Workflow{
		Name:      "src",
		CreatedAt: "2026-07-03T10:00:00Z",
		UpdatedAt: "2026-07-03T10:05:00Z",
		Definition: Definition{
			Nodes: []Node{
				{ID: "START"},
				{
					ID:             "gen",
					DisplayName:    "生成",
					Engine:         "claude-code",
					PromptTemplate: "{{sys.userPrompt}}",
					EngineConfig:   &EngineConfig{Model: "sonnet", Effort: "high"},
				},
				{ID: "END"},
			},
			Edges: []Edge{{From: "START", To: "gen"}, {From: "gen", To: "END"}},
		},
	}
}

func TestCopyAsRenamesAndDropsTimestamps(t *testing.T) {
	src := srcWithPointers()
	copied := src.CopyAs("dst")

	if copied.Name != "dst" {
		t.Errorf("name 应改为 dst，得到 %q", copied.Name)
	}
	if copied.CreatedAt != "" || copied.UpdatedAt != "" {
		t.Errorf("不应携带源时间戳，得到 createdAt=%q updatedAt=%q", copied.CreatedAt, copied.UpdatedAt)
	}
	if len(copied.Definition.Nodes) != 3 || len(copied.Definition.Edges) != 2 {
		t.Fatalf("定义主体未正确复制: %+v", copied.Definition)
	}
}

func TestCopyAsDeepCopiesPointerFields(t *testing.T) {
	src := srcWithPointers()
	copied := src.CopyAs("dst")

	cNode := &copied.Definition.Nodes[1]
	sNode := &src.Definition.Nodes[1]

	if cNode.EngineConfig == sNode.EngineConfig {
		t.Error("EngineConfig 指针被浅拷共享")
	}
	// 改动 copied 的指针指向值，src 不得受影响。
	cNode.EngineConfig.Model = "haiku"
	cNode.EngineConfig.Effort = "low"
	if sNode.EngineConfig.Model != "sonnet" || sNode.EngineConfig.Effort != "high" {
		t.Errorf("src.EngineConfig 被串改: %+v", sNode.EngineConfig)
	}
	// 改动 copied 的边不得影响 src（切片独立）。
	copied.Definition.Edges[0].To = "改了"
	if src.Definition.Edges[0].To != "gen" {
		t.Errorf("src.Edges 被串改: %+v", src.Definition.Edges[0])
	}
}

func TestCopyAsHandlesNilPointers(t *testing.T) {
	src := &Workflow{
		Name: "src",
		Definition: Definition{
			Nodes: []Node{
				{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
				{ID: "END"},
			},
			Edges: []Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
		},
	}
	copied := src.CopyAs("dst")
	if copied.Definition.Nodes[1].EngineConfig != nil {
		t.Errorf("nil 指针字段应保持 nil，得到 %+v", copied.Definition.Nodes[1])
	}
}

// TestScaffoldIsRunnable 确认脚手架骨架（START→node-1→END）本身校验通过。
func TestScaffoldIsRunnable(t *testing.T) {
	def := Scaffold()
	if err := Validate(&def); err != nil {
		t.Fatalf("脚手架骨架应校验通过，却报错:\n%v", err)
	}
	if def.AgentNodeCount() != 1 {
		t.Errorf("脚手架应有 1 个 agent 节点，得到 %d", def.AgentNodeCount())
	}
}

// TestNodeClassification 确认 START/END/agent 的判别方法。
func TestNodeClassification(t *testing.T) {
	start := Node{ID: NodeIDStart}
	end := Node{ID: NodeIDEnd}
	agent := Node{ID: "a"}
	if !start.IsStart() || !start.IsMarker() || start.IsAgent() {
		t.Errorf("START 判别错误: %+v", start)
	}
	if !end.IsEnd() || !end.IsMarker() || end.IsAgent() {
		t.Errorf("END 判别错误: %+v", end)
	}
	if agent.IsMarker() || !agent.IsAgent() {
		t.Errorf("agent 判别错误: %+v", agent)
	}
}
