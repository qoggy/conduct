package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

const testPort = 7420
const testHost = "127.0.0.1:7420"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServer(store.New(t.TempDir()), "test-1.2.3")
	if err != nil {
		t.Fatalf("构造测试 Server 失败: %v", err)
	}
	return server
}

// do 发一个带合法 Host 的请求穿过完整路由（含守卫中间件）。body 非空默认置 application/json。
func do(t *testing.T, s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = testHost
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	s.routes(testPort).ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("解析响应体失败: %v（body=%s）", err, rec.Body.String())
	}
}

func TestVersionEndpoint(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/api/version", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", rec.Code)
	}
	var resp versionResponse
	decodeBody(t, rec, &resp)
	if resp.Version != "test-1.2.3" {
		t.Fatalf("版本串不符: %q", resp.Version)
	}
}

func TestEnginesEndpoint(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/api/engines", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", rec.Code)
	}
	var infos []engineInfo
	decodeBody(t, rec, &infos)
	byName := map[string]engineInfo{}
	for _, info := range infos {
		byName[info.Name] = info
	}
	cc, ok := byName["claude-code"]
	if !ok || cc.Capability == nil {
		t.Fatalf("claude-code 应有能力表: %+v", cc)
	}
	if cc.Capability.EffortField != "effort" {
		t.Fatalf("claude-code effortField 应为 effort，得到 %q", cc.Capability.EffortField)
	}
	agy, ok := byName["antigravity"]
	if !ok || agy.Capability == nil {
		t.Fatalf("antigravity 应有能力表: %+v", agy)
	}
	if agy.Capability.EffortField != "" {
		t.Fatalf("antigravity 应无独立 effort 字段，得到 %q", agy.Capability.EffortField)
	}
}

func TestCreateListWorkflow(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create 期望 201，得到 %d（%s）", rec.Code, rec.Body.String())
	}

	// 同名 → 409
	dup := do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	if dup.Code != http.StatusConflict {
		t.Fatalf("重名 create 期望 409，得到 %d", dup.Code)
	}

	// 非法名 → 400
	bad := do(t, s, http.MethodPost, "/api/workflows", `{"name":"bad name"}`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("非法名 create 期望 400，得到 %d", bad.Code)
	}

	list := do(t, s, http.MethodGet, "/api/workflows", "", nil)
	var resp workflowsResponse
	decodeBody(t, list, &resp)
	if len(resp.Workflows) != 1 || resp.Workflows[0].Name != "demo" {
		t.Fatalf("列表应含 demo，得到 %+v", resp.Workflows)
	}
	if len(resp.Workflows[0].NodeIDs) != 1 || resp.Workflows[0].NodeIDs[0] != "node-1" {
		t.Fatalf("节点 id 流不符: %+v", resp.Workflows[0].NodeIDs)
	}
}

func TestGetWorkflowNotFound(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/api/workflows/ghost", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404，得到 %d", rec.Code)
	}
}

func TestPutInvalidDefinitionReturnsStructuredProblems(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)

	// 语义非法：节点 engine 为未知引擎 → ValidateStructured 报字段级错误。
	badDef := `{"nodes":[{"id":"n1","displayName":"步骤","engine":"nope","promptTemplate":"{{sys.userPrompt}}"}]}`
	rec := do(t, s, http.MethodPut, "/api/workflows/demo", badDef, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("语义非法应 422，得到 %d（%s）", rec.Code, rec.Body.String())
	}
	var resp errorResponse
	decodeBody(t, rec, &resp)
	if len(resp.Problems) == 0 {
		t.Fatalf("422 应带字段级 problems，得到空")
	}
	// Path 应指向具体字段（不含 ": " 前缀污染），且非空。
	for _, p := range resp.Problems {
		if p.Path == "" {
			t.Fatalf("problem.Path 不应为空: %+v", resp.Problems)
		}
		if strings.Contains(p.Path, ": ") {
			t.Fatalf("problem.Path 不应含 ': ' 分隔符: %q", p.Path)
		}
	}
}

func TestPutValidDefinition(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	goodDef := `{"nodes":[{"id":"n1","displayName":"步骤","engine":"claude-code","promptTemplate":"{{sys.userPrompt}}"}]}`
	rec := do(t, s, http.MethodPut, "/api/workflows/demo", goodDef, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("合法 PUT 应 200，得到 %d（%s）", rec.Code, rec.Body.String())
	}
	var def workflow.Definition
	decodeBody(t, rec, &def)
	if len(def.Nodes) != 1 || def.Nodes[0].ID != "n1" {
		t.Fatalf("返回定义不符: %+v", def)
	}
}

func TestPutNameMismatchRejected(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	body := `{"name":"other","nodes":[{"id":"n1","displayName":"步骤","engine":"claude-code","promptTemplate":"x"}]}`
	rec := do(t, s, http.MethodPut, "/api/workflows/demo", body, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("name 不一致应 409，得到 %d", rec.Code)
	}
}

func TestOptimisticConcurrencyConflict(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	goodDef := `{"nodes":[{"id":"n1","displayName":"步骤","engine":"claude-code","promptTemplate":"x"}]}`
	rec := do(t, s, http.MethodPut, "/api/workflows/demo", goodDef, map[string]string{
		"X-Conduct-Base-UpdatedAt": "1999-01-01T00:00:00+08:00", // 过期基线
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("过期基线应 409，得到 %d", rec.Code)
	}
	var resp conflictResponse
	decodeBody(t, rec, &resp)
	if resp.Current == nil {
		t.Fatalf("409 应带回当前定义供前端重载")
	}
}

func TestRenameWorkflow(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"taken"}`, nil)

	ok := do(t, s, http.MethodPost, "/api/workflows/demo/rename", `{"newName":"demo2"}`, nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("改名应 200，得到 %d（%s）", ok.Code, ok.Body.String())
	}
	if gone := do(t, s, http.MethodGet, "/api/workflows/demo", "", nil); gone.Code != http.StatusNotFound {
		t.Fatalf("旧名应 404，得到 %d", gone.Code)
	}
	if got := do(t, s, http.MethodGet, "/api/workflows/demo2", "", nil); got.Code != http.StatusOK {
		t.Fatalf("新名应 200，得到 %d", got.Code)
	}
	// 改名到占用 → 409
	occupied := do(t, s, http.MethodPost, "/api/workflows/demo2/rename", `{"newName":"taken"}`, nil)
	if occupied.Code != http.StatusConflict {
		t.Fatalf("改名到占用应 409，得到 %d", occupied.Code)
	}
}

func TestCopyWorkflow(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"taken"}`, nil)

	// 复制成功 → 201，源仍在、副本新建
	ok := do(t, s, http.MethodPost, "/api/workflows/demo/copy", `{"newName":"demo-copy"}`, nil)
	if ok.Code != http.StatusCreated {
		t.Fatalf("复制应 201，得到 %d（%s）", ok.Code, ok.Body.String())
	}
	// 副本时间戳由 store 重戳，非空（不继承源）
	var copied workflow.Definition
	decodeBody(t, ok, &copied)
	if copied.Name != "demo-copy" || copied.CreatedAt == "" || copied.UpdatedAt == "" {
		t.Fatalf("副本应重戳非空时间戳，得到 %+v", copied)
	}
	if src := do(t, s, http.MethodGet, "/api/workflows/demo", "", nil); src.Code != http.StatusOK {
		t.Fatalf("源应仍在 200，得到 %d", src.Code)
	}
	if dst := do(t, s, http.MethodGet, "/api/workflows/demo-copy", "", nil); dst.Code != http.StatusOK {
		t.Fatalf("副本应 200，得到 %d", dst.Code)
	}
	// 复制成同名（源即目标）→ 409（目标已存在，不覆盖）
	same := do(t, s, http.MethodPost, "/api/workflows/demo/copy", `{"newName":"demo"}`, nil)
	if same.Code != http.StatusConflict {
		t.Fatalf("复制成同名应 409，得到 %d", same.Code)
	}

	// 复制到占用名 → 409
	occupied := do(t, s, http.MethodPost, "/api/workflows/demo/copy", `{"newName":"taken"}`, nil)
	if occupied.Code != http.StatusConflict {
		t.Fatalf("复制到占用应 409，得到 %d", occupied.Code)
	}
	// 源不存在 → 404
	missing := do(t, s, http.MethodPost, "/api/workflows/nope/copy", `{"newName":"x"}`, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("源不存在应 404，得到 %d", missing.Code)
	}
	// 非法新名 → 400
	bad := do(t, s, http.MethodPost, "/api/workflows/demo/copy", `{"newName":"bad name"}`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("非法新名应 400，得到 %d", bad.Code)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/workflows", `{"name":"demo"}`, nil)
	del := do(t, s, http.MethodDelete, "/api/workflows/demo", `{}`, nil)
	if del.Code != http.StatusNoContent {
		t.Fatalf("删除应 204，得到 %d", del.Code)
	}
	if again := do(t, s, http.MethodDelete, "/api/workflows/ghost", `{}`, nil); again.Code != http.StatusNotFound {
		t.Fatalf("删除不存在应 404，得到 %d", again.Code)
	}
}

// seedRun 直接在 store 落一条运行记录（绕过发射器），供列表 / 详情 / 停止 / 总结的 handler 测试。
// TestResumeRunPrecheck 覆盖 POST /api/runs/{id}/resume 的 409 前置校验（不触发 self-exec spawn）：
// completed / running（进程存活）→ 409。成功续跑路径会真 spawn 子进程，交由 CLI/launch 层单测与
// 端到端验证，此处只锁 handler 的拒绝分支。
func TestResumeRunPrecheck(t *testing.T) {
	s := newTestServer(t)
	// completed → 409。
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusCompleted, 1)
	rec := do(t, s, http.MethodPost, "/api/runs/demo-20260101-000000/resume", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("completed 恢复应 409，得到 %d", rec.Code)
	}
	// running 且进程存活 → 409。
	seedRun(t, s, "demo-20260101-000001", "demo", run.StatusRunning, os.Getpid())
	rec = do(t, s, http.MethodPost, "/api/runs/demo-20260101-000001/resume", "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running 恢复应 409，得到 %d", rec.Code)
	}
	// 不存在 → 404。
	rec = do(t, s, http.MethodPost, "/api/runs/ghost-20260101-000000/resume", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的 run 恢复应 404，得到 %d", rec.Code)
	}
}

func seedRun(t *testing.T, s *Server, id, workflowName string, status run.Status, pid int) {
	t.Helper()
	record := &run.Record{
		ID: id, Workflow: workflowName, Status: status, Pid: pid, Steps: 3,
		StartedAt: "2026-07-05T10:00:00+08:00", Artifacts: map[string]string{},
	}
	if err := s.store.CreateRun(record); err != nil {
		t.Fatalf("落运行记录失败: %v", err)
	}
}

func TestListRunsWithProgressAndFilter(t *testing.T) {
	s := newTestServer(t)
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusCompleted, 1)
	seedRun(t, s, "other-20260101-000001", "other", run.StatusCompleted, 1)
	// 给 demo 追两条成功 trace（进度按唯一 stepIndex 且 success 去重计），进度应为 2。
	for i := 0; i < 2; i++ {
		if err := s.store.AppendTrace("demo-20260101-000000", run.TraceEntry{StepIndex: i, Success: true}); err != nil {
			t.Fatalf("追加 trace 失败: %v", err)
		}
	}

	rec := do(t, s, http.MethodGet, "/api/runs?workflow=demo", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应 200，得到 %d", rec.Code)
	}
	var resp runsResponse
	decodeBody(t, rec, &resp)
	if len(resp.Runs) != 1 || resp.Runs[0].Workflow != "demo" {
		t.Fatalf("workflow 过滤应只剩 demo，得到 %+v", resp.Runs)
	}
	if resp.Runs[0].Progress != 2 {
		t.Fatalf("进度应为 2，得到 %d", resp.Runs[0].Progress)
	}
}

func TestGetRunWithTrace(t *testing.T) {
	s := newTestServer(t)
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusCompleted, 1)
	if err := s.store.AppendTrace("demo-20260101-000000", run.TraceEntry{StepIndex: 0, Output: "hi"}); err != nil {
		t.Fatalf("追加 trace 失败: %v", err)
	}
	rec := do(t, s, http.MethodGet, "/api/runs/demo-20260101-000000?trace=1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("详情应 200，得到 %d", rec.Code)
	}
	var detail runDetail
	decodeBody(t, rec, &detail)
	if detail.Record == nil || detail.Record.ID != "demo-20260101-000000" {
		t.Fatalf("详情缺 record")
	}
	if detail.Trace == nil || len(*detail.Trace) != 1 || (*detail.Trace)[0].Output != "hi" {
		t.Fatalf("trace 全文缺失: %+v", detail.Trace)
	}
}

// TestGetRunEmptyTraceIsEmptyArray 锁住数组语义：?trace=1 对空 trace 的 run（如中断在 step 0 之前的
// interrupted）必须返回 `"trace":[]`（字段存在、值为空数组），不能因 omitempty 把字段整个省略；
// 不带 ?trace=1 时则不应出现 trace 字段。回归 spec-test 发现的 TC-013。
func TestGetRunEmptyTraceIsEmptyArray(t *testing.T) {
	s := newTestServer(t)
	// 空 trace 的 run（状态与本断言无关，用 completed 作固定态；真实场景是中断在 step 0 之前的 interrupted）。
	seedRun(t, s, "empty-20260101-000000", "empty", run.StatusCompleted, 1) // 不追加任何 trace

	withTrace := do(t, s, http.MethodGet, "/api/runs/empty-20260101-000000?trace=1", "", nil)
	if withTrace.Code != http.StatusOK {
		t.Fatalf("详情应 200，得到 %d", withTrace.Code)
	}
	if body := withTrace.Body.String(); !strings.Contains(body, `"trace":[]`) {
		t.Fatalf("?trace=1 空 trace 应含 \"trace\":[]，得到 %s", body)
	}

	noTrace := do(t, s, http.MethodGet, "/api/runs/empty-20260101-000000", "", nil)
	if body := noTrace.Body.String(); strings.Contains(body, `"trace"`) {
		t.Fatalf("未请求 trace 不应出现 trace 字段，得到 %s", body)
	}
}

func TestSummaryNotGeneratedReturns404(t *testing.T) {
	s := newTestServer(t)
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusRunning, 1)
	rec := do(t, s, http.MethodGet, "/api/runs/demo-20260101-000000/summary", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("总结未生成应 404，得到 %d", rec.Code)
	}
}

func TestSummaryStaleForUnfinishedRunReturns404(t *testing.T) {
	s := newTestServer(t)
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusRunning, os.Getpid())
	if err := s.store.WriteSummary("demo-20260101-000000", "# 旧失败总结\n"); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s, http.MethodGet, "/api/runs/demo-20260101-000000/summary", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未收尾运行即使残留旧 summary 也应 404，得到 %d（%s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "旧失败总结") {
		t.Fatalf("响应不应包含旧 summary，实际: %s", rec.Body.String())
	}
}

func TestStopNonRunningReturns409(t *testing.T) {
	s := newTestServer(t)
	seedRun(t, s, "demo-20260101-000000", "demo", run.StatusCompleted, 1)
	rec := do(t, s, http.MethodPost, "/api/runs/demo-20260101-000000/stop", `{}`, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("终止终态运行应 409，得到 %d", rec.Code)
	}
}

func TestStopNotExistReturns404(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, http.MethodPost, "/api/runs/ghost-20260101-000000/stop", `{}`, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("终止不存在运行应 404，得到 %d", rec.Code)
	}
}

func TestGuardRejectsBadHost(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "evil.com"
	rec := httptest.NewRecorder()
	s.routes(testPort).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非白名单 Host 应 403，得到 %d", rec.Code)
	}
}

func TestGuardRejectsBadOrigin(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = testHost
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	s.routes(testPort).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非白名单 Origin 应 403，得到 %d", rec.Code)
	}
}

func TestGuardRejectsNonJSONMutation(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/workflows", strings.NewReader(`{"name":"x"}`))
	req.Host = testHost
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.routes(testPort).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("非 JSON 变更应 415，得到 %d", rec.Code)
	}
}

func TestFSListing(t *testing.T) {
	s := newTestServer(t)
	root := t.TempDir()
	for _, d := range []string{"beta", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("建子目录失败: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a-file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("建文件失败: %v", err)
	}

	rec := do(t, s, http.MethodGet, "/api/fs?path="+url.QueryEscape(root), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var got fsListing
	decodeBody(t, rec, &got)
	if got.Path != root {
		t.Errorf("path = %q，期望 %q", got.Path, root)
	}
	if got.Parent != filepath.Dir(root) {
		t.Errorf("parent = %q，期望 %q", got.Parent, filepath.Dir(root))
	}
	// 只列目录（不含文件），且按名排序。
	if len(got.Entries) != 2 || got.Entries[0].Name != "alpha" || got.Entries[1].Name != "beta" {
		t.Errorf("entries 期望 [alpha beta]（仅目录、已排序），得到 %+v", got.Entries)
	}
}

func TestFSListingRejectsRelative(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/api/fs?path="+url.QueryEscape("relative/dir"), "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("相对路径应 400，得到 %d", rec.Code)
	}
}

func TestFSListingNotFound(t *testing.T) {
	s := newTestServer(t)
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	rec := do(t, s, http.MethodGet, "/api/fs?path="+url.QueryEscape(missing), "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在目录应 404，得到 %d", rec.Code)
	}
}
