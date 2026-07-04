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

func sampleDef(name string) *workflow.Definition {
	return &workflow.Definition{
		Name: name,
		Nodes: []workflow.Node{
			{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "{{sys.userPrompt}}"},
		},
	}
}

func names(defs []*workflow.Definition) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
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
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "a" {
		t.Errorf("节点未正确往返: %+v", got.Nodes)
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
	edited.Nodes[0].DisplayName = "改了"
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
	if after.Nodes[0].DisplayName != "改了" {
		t.Errorf("定义未更新: %+v", after.Nodes[0])
	}
}

func TestStoreSaveNonexistent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save(sampleDef("ghost")); !errors.Is(err, ErrNotExist) {
		t.Errorf("Save 不存在的应返回 ErrNotExist，得到 %v", err)
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

func TestStoreListSortedAndEmpty(t *testing.T) {
	s := newTestStore(t)
	defs, skipped, err := s.List() // 目录尚未创建
	if err != nil {
		t.Fatalf("空 store List 报错: %v", err)
	}
	if len(defs) != 0 || len(skipped) != 0 {
		t.Errorf("空 store 应返回 0 条、0 跳过，得到 %d 条 / %d 跳过", len(defs), len(skipped))
	}
	_ = s.Create(sampleDef("banana"))
	_ = s.Create(sampleDef("apple"))
	defs, skipped, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("无损坏文件时不应有跳过项，得到 %d", len(skipped))
	}
	if len(defs) != 2 || defs[0].Name != "apple" || defs[1].Name != "banana" {
		t.Errorf("List 应按名排序 [apple banana]，得到 %v", names(defs))
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
