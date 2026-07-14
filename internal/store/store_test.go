package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoggy/conduct/internal/workflow"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := New(t.TempDir())
	// 递增时钟：每次调用 +1 分钟，便于观察 updatedAt 是否重戳。
	base := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	tick := 0
	s.now = func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Minute)
	}
	return s
}

func sampleDef(name string) *workflow.Workflow {
	return &workflow.Workflow{
		Name: name,
		Definition: workflow.Definition{
			Nodes: []workflow.Node{
				{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
				{ID: "END"},
			},
			Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
		},
	}
}

func names(workflows []*workflow.Workflow) []string {
	out := make([]string, len(workflows))
	for i, w := range workflows {
		out[i] = w.Name
	}
	return out
}

func TestStoreCreateLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("flow")); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	got, err := s.Load("flow")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if got.Name != "flow" || got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("加载结果元数据不全: %+v", got)
	}
	if len(got.Definition.Nodes) != 3 || got.Definition.Nodes[1].ID != "a" {
		t.Errorf("节点未正确往返: %+v", got.Definition.Nodes)
	}
}

func TestStoreCreateRejectsDuplicate(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("flow")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(sampleDef("flow")); !errors.Is(err, ErrExists) {
		t.Errorf("重复 Create 应返回 ErrExists，得到 %v", err)
	}
}

func TestStoreSavePreservesCreatedAtRestampsUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("flow")); err != nil {
		t.Fatal(err)
	}
	created, _ := s.Load("flow")

	edited := sampleDef("flow")
	edited.Definition.Nodes[1].DisplayName = "改了"
	if err := s.Save(edited); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Load("flow")
	if after.CreatedAt != created.CreatedAt {
		t.Errorf("createdAt 应保留 %q，得到 %q", created.CreatedAt, after.CreatedAt)
	}
	if after.UpdatedAt == created.UpdatedAt {
		t.Errorf("updatedAt 应重戳，仍为 %q", after.UpdatedAt)
	}
	if after.Definition.Nodes[1].DisplayName != "改了" {
		t.Errorf("定义未更新: %+v", after.Definition.Nodes[1])
	}
}

func TestStoreSaveNonexistent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save(sampleDef("ghost")); !errors.Is(err, ErrNotExist) {
		t.Errorf("Save 不存在的应返回 ErrNotExist，得到 %v", err)
	}
}

func TestStoreReplaceDefinitionRepairsCorruptFile(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("flow")); err != nil {
		t.Fatal(err)
	}
	corrupt := s.path("flow")
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	replacement := sampleDef("flow")
	replacement.Definition.Nodes[1].DisplayName = "已修复"
	if err := s.ReplaceDefinition(replacement); err != nil {
		t.Fatalf("ReplaceDefinition 应能修复结构损坏的旧文件: %v", err)
	}
	loaded, err := s.Load("flow")
	if err != nil {
		t.Fatalf("替换后应能严格载入: %v", err)
	}
	if loaded.Definition.Nodes[1].DisplayName != "已修复" {
		t.Fatalf("替换定义未落盘: %+v", loaded.Definition.Nodes[1])
	}
	if loaded.CreatedAt == "" || loaded.UpdatedAt == "" {
		t.Fatalf("损坏文件恢复后应重建系统时间戳: %+v", loaded)
	}
}

func TestStoreReplaceDefinitionPreservesReadableCreatedAt(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("flow")); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load("flow")
	if err != nil {
		t.Fatal(err)
	}
	// 未知字段令严格 Load 失败，但 JSON 元数据仍可读取；整体替换应修复结构并保留 createdAt。
	data := []byte(`{"name":"flow","createdAt":"` + before.CreatedAt + `","unexpected":true}`)
	if err := os.WriteFile(s.path("flow"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDefinition(sampleDef("flow")); err != nil {
		t.Fatal(err)
	}
	after, err := s.Load("flow")
	if err != nil {
		t.Fatal(err)
	}
	if after.CreatedAt != before.CreatedAt {
		t.Fatalf("可读取的 createdAt 应保留 %q，得到 %q", before.CreatedAt, after.CreatedAt)
	}
}

func TestStoreRename(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("old")); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("old", "new"); err != nil {
		t.Fatalf("Rename 失败: %v", err)
	}
	if s.Exists("old") {
		t.Error("旧名仍存在")
	}
	got, err := s.Load("new")
	if err != nil {
		t.Fatalf("新名 Load 失败: %v", err)
	}
	if got.Name != "new" {
		t.Errorf("内部 name 未改为 new，得到 %q", got.Name)
	}
}

func TestStoreRenameConflicts(t *testing.T) {
	s := newTestStore(t)
	_ = s.Create(sampleDef("a"))
	_ = s.Create(sampleDef("b"))
	if err := s.Rename("ghost", "x"); !errors.Is(err, ErrNotExist) {
		t.Errorf("改名不存在的源应 ErrNotExist，得到 %v", err)
	}
	if err := s.Rename("a", "b"); !errors.Is(err, ErrExists) {
		t.Errorf("改名到已占用应 ErrExists，得到 %v", err)
	}
}

func TestStoreDelete(t *testing.T) {
	s := newTestStore(t)
	_ = s.Create(sampleDef("flow"))
	if err := s.Delete("flow"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if s.Exists("flow") {
		t.Error("删除后仍存在")
	}
	if err := s.Delete("flow"); !errors.Is(err, ErrNotExist) {
		t.Errorf("删除不存在的应 ErrNotExist，得到 %v", err)
	}
}

func TestStoreListSortedByUpdatedAtAndEmpty(t *testing.T) {
	s := newTestStore(t)
	defs, skipped, err := s.List() // 目录尚未创建
	if err != nil {
		t.Fatalf("空 store List 报错: %v", err)
	}
	if len(defs) != 0 || len(skipped) != 0 {
		t.Errorf("空 store 应返回 0 条、0 跳过，得到 %d 条 / %d 跳过", len(defs), len(skipped))
	}
	// 递增时钟：apple 先建（updatedAt 更早）、banana 后建（updatedAt 更晚）。按 updatedAt 倒序应
	// [banana apple]——恰与按名升序相反，坐实排序键是 updatedAt 而非 name。
	_ = s.Create(sampleDef("apple"))
	_ = s.Create(sampleDef("banana"))
	defs, skipped, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("无损坏文件时不应有跳过项，得到 %d", len(skipped))
	}
	if len(defs) != 2 || defs[0].Name != "banana" || defs[1].Name != "apple" {
		t.Errorf("List 应按 updatedAt 倒序 [banana apple]，得到 %v", names(defs))
	}
}

// TestStoreListTieBreaksByName 覆盖 updatedAt 相同时按 name 升序兜底（免同刻并列抖动）。
func TestStoreListTieBreaksByName(t *testing.T) {
	s := New(t.TempDir())
	s.now = func() time.Time { return time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC) } // 恒定时钟 → 同刻
	for _, name := range []string{"gamma", "alpha", "beta"} {
		if err := s.Create(sampleDef(name)); err != nil {
			t.Fatal(err)
		}
	}
	defs, _, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := names(defs); len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Errorf("updatedAt 相同应按 name 升序 [alpha beta gamma]，得到 %v", got)
	}
}

func TestStoreListSkipsCorrupt(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("good")); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(s.workflowsDir(), "broken.json")
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	defs, skipped, err := s.List()
	if err != nil {
		t.Fatalf("List 不应因单个损坏文件整体失败: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "good" {
		t.Errorf("应仍列出好的工作流，得到 %v", names(defs))
	}
	if len(skipped) != 1 {
		t.Errorf("应有 1 个跳过项，得到 %d", len(skipped))
	}
}

func TestStoreRejectsUnsafeName(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create(sampleDef("real")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../evil", "a/b", ".", ".."} {
		if err := s.Delete(bad); err == nil {
			t.Errorf("Delete(%q) 应因非法名被拒", bad)
		}
		if _, err := s.Load(bad); err == nil {
			t.Errorf("Load(%q) 应因非法名被拒", bad)
		}
		if s.Exists(bad) {
			t.Errorf("Exists(%q) 非法名应返回 false", bad)
		}
	}
}
