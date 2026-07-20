package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

func sampleRun(id string) *run.Record {
	return &run.Record{
		ID:       id,
		Workflow: "flow",
		WorkflowSnapshot: &workflow.Workflow{Name: "flow", Definition: workflow.Definition{
			Nodes: []workflow.Node{
				{ID: "START"},
				{ID: "a", DisplayName: "甲", Engine: "claude-code", PromptTemplate: "x"},
				{ID: "END"},
			},
			Edges: []workflow.Edge{{From: "START", To: "a"}, {From: "a", To: "END"}},
		}},
		UserPrompt: "需求",
		Cwd:        "/proj",
		Status:     run.StatusRunning,
		Language:   locale.English,
		Pid:        os.Getpid(),
		StartedAt:  "2026-07-03T15:00:00+08:00",
		Artifacts:  map[string]string{},
	}
}

func TestRunStoreRejectsMissingOrInvalidLanguage(t *testing.T) {
	for _, language := range []locale.Language{"", "fr"} {
		t.Run(string(language), func(t *testing.T) {
			s := New(t.TempDir())
			record := sampleRun("flow-20260703-150000")
			record.Language = language
			if err := s.CreateRun(record); err == nil {
				t.Fatalf("CreateRun language %q = nil, want error", language)
			}
		})
	}
}

func TestLoadRunRejectsMissingLanguage(t *testing.T) {
	s := New(t.TempDir())
	record := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.runsDir(), record.ID, "run.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), ",\n  \"language\": \"en\"", "", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRun(record.ID); err == nil || !strings.Contains(err.Error(), "missing or invalid language") {
		t.Fatalf("LoadRun missing language error = %v", err)
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
	empty, err := s.LoadTrace(rec.ID)
	if err != nil {
		t.Fatalf("空 trace LoadTrace 失败: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("空 trace 应返回非 nil 空切片，得到 %#v", empty)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := s.AppendTrace(rec.ID, run.TraceEntry{NodeID: id, Output: "o", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.LoadTrace(rec.ID)
	if err != nil {
		t.Fatalf("LoadTrace 失败: %v", err)
	}
	if len(entries) != 3 || entries[0].NodeID != "a" || entries[2].NodeID != "c" {
		t.Errorf("trace 追加/读取顺序错: %+v", entries)
	}
}

func TestTraceNullableMetadataIsExplicitAndBackwardCompatible(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendTrace(rec.ID, run.TraceEntry{NodeID: "new", Success: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.runsDir(), rec.ID, "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tokens":null`) || !strings.Contains(string(data), `"sessionId":null`) {
		t.Fatalf("新 trace 应显式序列化 null metadata: %s", data)
	}

	writeRawTrace(t, s, rec.ID, `{"nodeId":"old","success":true}`+"\n")
	entries, err := s.LoadTrace(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Tokens != nil || entries[0].SessionID != nil {
		t.Fatalf("旧 trace 缺失字段应读取为 nil: %+v", entries)
	}
	normalized, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"tokens":null`) || !strings.Contains(string(normalized), `"sessionId":null`) {
		t.Fatalf("旧 trace 重新输出应规范化为 null: %s", normalized)
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

// writeRawTrace 直接向某 run 的 trace.jsonl 落原始字节（绕过 AppendTrace，用于构造末尾半行等边界）。
func writeRawTrace(t *testing.T, s *Store, id, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.runsDir(), id, "trace.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCountTrace 覆盖：缺文件=0、完整行计数、末尾半行不计。
func TestCountTrace(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}

	// 刚建好：空 trace.jsonl → 0。
	if n, err := s.CountTrace(rec.ID); err != nil || n != 0 {
		t.Fatalf("空 trace 应为 0，得到 %d/%v", n, err)
	}

	// 3 条完整行 + 1 条末尾无换行的半行 → 只数 3。
	writeRawTrace(t, s, rec.ID,
		`{"nodeId":"a"}`+"\n"+`{"nodeId":"b"}`+"\n"+`{"nodeId":"c"}`+"\n"+`{"nodeId":"d"`)
	if n, err := s.CountTrace(rec.ID); err != nil || n != 3 {
		t.Fatalf("3 完整行 + 半行应为 3，得到 %d/%v", n, err)
	}

	// 文件整个不存在（另一个从未写过 trace 的 id）→ 0。
	missing := New(t.TempDir())
	if n, err := missing.CountTrace("ghost-20260703-150000"); err != nil || n != 0 {
		t.Fatalf("缺 trace 文件应为 0，得到 %d/%v", n, err)
	}
}

// TestCountProgress 覆盖去重语义：唯一 nodeId 且（最后一次记录）success 才计数，防 resume 后 k>N。
func TestCountProgress(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	// 缺 / 空 trace → 0。
	if n, err := s.CountProgress(rec.ID); err != nil || n != 0 {
		t.Fatalf("空 trace 应为 0，得到 %d/%v", n, err)
	}
	// 模拟一次 resume 后的 trace：节点 c 先失败、后被补跑成功——同一 nodeId 两条，只应算 1 次。
	// 物理 5 行、但唯一成功 nodeId 为 {a,b,c,d} 共 4 个（c 末条 success 覆盖失败）。
	writeRawTrace(t, s, rec.ID,
		`{"nodeId":"a","success":true}`+"\n"+
			`{"nodeId":"b","success":true}`+"\n"+
			`{"nodeId":"c","success":false}`+"\n"+ // 首次失败行（保留）
			`{"nodeId":"c","success":true}`+"\n"+ // resume 补跑成功
			`{"nodeId":"d","success":true}`+"\n") // 真实 trace 每行以 \n 结尾（AppendTrace 保证）
	if n, err := s.CountProgress(rec.ID); err != nil || n != 4 {
		t.Fatalf("去重后应为 4（唯一成功 nodeId a/b/c/d），得到 %d/%v", n, err)
	}
	// 缺文件的 id → 0。
	missing := New(t.TempDir())
	if n, err := missing.CountProgress("ghost-20260703-150000"); err != nil || n != 0 {
		t.Fatalf("缺 trace 文件应为 0，得到 %d/%v", n, err)
	}
}

// TestLoadTraceIgnoresTrailingHalfLine 确认末尾半行不被当成完整行解析（不报「行损坏」）。
func TestLoadTraceIgnoresTrailingHalfLine(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	// 2 条完整合法行 + 末尾一条写了一半的非法 JSON（无换行）。
	writeRawTrace(t, s, rec.ID,
		`{"nodeId":"a"}`+"\n"+`{"nodeId":"b"}`+"\n"+`{"nodeId":"c`)
	entries, err := s.LoadTrace(rec.ID)
	if err != nil {
		t.Fatalf("末尾半行不应导致 LoadTrace 报错: %v", err)
	}
	if len(entries) != 2 || entries[0].NodeID != "a" || entries[1].NodeID != "b" {
		t.Errorf("应只读到 2 条完整行，得到 %+v", entries)
	}
}

// TestReadSummary 覆盖两态：已写返回内容、未写返回 ErrSummaryNotExist。
func TestReadSummary(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	// running 期还没收尾：未生成 → 哨兵。
	if _, err := s.ReadSummary(rec.ID); !errors.Is(err, ErrSummaryNotExist) {
		t.Errorf("未生成总结应 ErrSummaryNotExist，得到 %v", err)
	}
	// 收尾写入后可读回原文。
	if err := s.WriteSummary(rec.ID, "# 报告\n完成\n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadSummary(rec.ID)
	if err != nil {
		t.Fatalf("ReadSummary 失败: %v", err)
	}
	if got != "# 报告\n完成\n" {
		t.Errorf("总结内容错: %q", got)
	}
}

func TestRemoveSummary(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSummary(rec.ID, "# 旧报告\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveSummary(rec.ID); err != nil {
		t.Fatalf("RemoveSummary 失败: %v", err)
	}
	if _, err := s.ReadSummary(rec.ID); !errors.Is(err, ErrSummaryNotExist) {
		t.Fatalf("删除后应读不到 summary，得到 %v", err)
	}
	if err := s.RemoveSummary(rec.ID); err != nil {
		t.Fatalf("RemoveSummary 应幂等，得到 %v", err)
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
	got, err := s.LoadRun(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != run.StatusCompleted || got.Artifacts["a"] != "产物" {
		t.Errorf("WriteRun 未生效: %+v", got)
	}
	path, err := s.SummaryPath(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# 报告\n" {
		t.Errorf("summary 内容错: %q", string(data))
	}
}

func TestRemoveRun(t *testing.T) {
	s := New(t.TempDir())
	rec := sampleRun("flow-20260703-150000")
	if err := s.CreateRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveRun(rec.ID); err != nil {
		t.Fatalf("RemoveRun 失败: %v", err)
	}
	// 目录连同三件套一并移除：再读即不存在。
	if _, err := s.LoadRun(rec.ID); !errors.Is(err, ErrRunNotExist) {
		t.Fatalf("删除后应读不到运行记录，得到 %v", err)
	}
}

func TestRemoveRunNotExist(t *testing.T) {
	s := New(t.TempDir())
	if err := s.RemoveRun("ghost-20260101-000000"); !errors.Is(err, ErrRunNotExist) {
		t.Fatalf("删除不存在的运行应返回 ErrRunNotExist，得到 %v", err)
	}
}

func TestRemoveRunInvalidID(t *testing.T) {
	s := New(t.TempDir())
	if err := s.RemoveRun("../escape"); err == nil {
		t.Fatalf("非法 id 应报错（防路径穿越）")
	}
}
