package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

func sampleRun(id string) *run.Record {
	return &run.Record{
		ID:               id,
		Workflow:         "flow",
		WorkflowSnapshot: &workflow.Definition{Name: "flow", Nodes: []workflow.Node{{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"}}},
		UserPrompt:       "需求",
		Cwd:              "/proj",
		Status:           run.StatusRunning,
		Pid:              os.Getpid(),
		Steps:            1,
		StartedAt:        "2026-07-03T15:00:00+08:00",
		Artifacts:        map[string]string{},
	}
}

func TestCreateLoadRunRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatalf("CreateRun 失败: %v", err)
	}
	got, err := s.LoadRun(rec.ID)
	if err != nil {
		t.Fatalf("LoadRun 失败: %v", err)
	}
	if got.Workflow != "flow" || got.Status != run.StatusRunning || got.WorkflowSnapshot == nil {
		t.Errorf("run.json 往返丢字段: %+v", got)
	}
}

func TestCreateRunRejectsDuplicate(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(rec); !errors.Is(err, ErrRunExists) {
		t.Errorf("重复 CreateRun 应 ErrRunExists（不覆盖历史），得到 %v", err)
	}
}

func TestAppendAndLoadTrace(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.AppendTrace(rec.ID, run.TraceEntry{StepIndex: i, Type: "agent", NodeID: "a", Output: "o", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.LoadTrace(rec.ID)
	if err != nil {
		t.Fatalf("LoadTrace 失败: %v", err)
	}
	if len(entries) != 3 || entries[0].StepIndex != 0 || entries[2].StepIndex != 2 {
		t.Errorf("trace 追加/读取顺序错: %+v", entries)
	}
}

func TestLoadRunNotExist(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.LoadRun("ghost-20260703-150000"); !errors.Is(err, ErrRunNotExist) {
		t.Errorf("不存在的 run 应 ErrRunNotExist，得到 %v", err)
	}
}

func TestListRunsSortedDescAndSkipsCorrupt(t *testing.T) {
	s := New(t.TempDir())
	// 空 store。
	records, skipped, err := s.ListRuns()
	if err != nil || len(records) != 0 || len(skipped) != 0 {
		t.Fatalf("空 store 应返回 0/0/nil，得到 %d/%d/%v", len(records), len(skipped), err)
	}
	older := sampleRun("flow-20260703-150000")
	newer := sampleRun("flow-20260703-160000")
	newer.StartedAt = "2026-07-03T16:00:00+08:00"
	if err := s.CreateRun(older); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(newer); err != nil {
		t.Fatal(err)
	}
	// 注入一个损坏 run.json。
	brokenDir := filepath.Join(s.runsDir(), "flow-20260703-170000")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "run.json"), []byte("{ bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	records, skipped, err = s.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns 不应因单个损坏整体失败: %v", err)
	}
	if len(records) != 2 || records[0].ID != newer.ID || records[1].ID != older.ID {
		t.Errorf("应按 startedAt 倒序 [newer older]，得到 %v", []string{records[0].ID, records[1].ID})
	}
	if len(skipped) != 1 {
		t.Errorf("应跳过 1 个损坏，得到 %d", len(skipped))
	}
}

func TestRunStoreRejectsUnsafeID(t *testing.T) {
	s := New(t.TempDir())
	for _, bad := range []string{"../evil", "a/b", ".", ".."} {
		if err := s.CreateRun(sampleRun(bad)); err == nil {
			t.Errorf("CreateRun(%q) 应因非法 id 被拒", bad)
		}
		if _, err := s.LoadRun(bad); err == nil {
			t.Errorf("LoadRun(%q) 应因非法 id 被拒", bad)
		}
	}
}

func TestWriteRunAndSummary(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	// 增量更新 + 收尾终态。
	rec.Status = run.StatusCompleted
	ended := "2026-07-03T15:00:10+08:00"
	rec.EndedAt = &ended
	rec.Artifacts = map[string]string{"a": "产物"}
	if err := s.WriteRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSummary(rec.ID, "# 报告\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadRun(rec.ID)
	if got.Status != run.StatusCompleted || got.Artifacts["a"] != "产物" {
		t.Errorf("WriteRun 未生效: %+v", got)
	}
	path, err := s.SummaryPath(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "# 报告\n" {
		t.Errorf("summary 内容错: %q", string(data))
	}
}
