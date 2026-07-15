package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/qoggy/conduct/internal/workflow"
)

// 各 /api/* handler：薄映射到 store / workflow / engine，能力面严格对齐 CLI 等价物。
// 错误文案与内核 / CLI stderr 同源，不复刻第二套规则（fail-loud 同源）。

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionResponse{Version: s.version})
}

// handleEngines 直读引擎能力表——检查器引擎 / effort 下拉的数据源。这是唯一无 CLI 命令等价的
// 只读信息性端点（无独占能力不变量的显式豁免，见 ui.md〈需要额外实现〉①）。
func (s *Server) handleEngines(w http.ResponseWriter, r *http.Request) {
	names := engine.RegisteredNames()
	infos := make([]engineInfo, 0, len(names))
	for _, name := range names {
		infos = append(infos, engineInfoOf(name))
	}
	writeJSON(w, http.StatusOK, infos)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := locale.Read(s.store.Root())
	if err != nil {
		writeTechnicalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeTechnicalError(w, http.StatusInternalServerError, fmt.Errorf("failed to read settings request body: %w", err))
		return
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed[0] != '{' {
		writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("body_object", ""))
		return
	}
	var body map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	if err := decoder.Decode(&body); err != nil {
		writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("body_object", ""))
		return
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("body_object", ""))
		return
	}
	for field := range body {
		if field != "language" {
			writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("unknown_field", field))
			return
		}
	}
	encoded, ok := body["language"]
	if !ok {
		writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("language_required", ""))
		return
	}
	var language *locale.Language
	if string(encoded) != "null" {
		var value string
		if err := json.Unmarshal(encoded, &value); err != nil {
			writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("language_type", ""))
			return
		}
		parsed := locale.Language(value)
		if !parsed.Valid() {
			writeApplicationError(w, http.StatusBadRequest, invalidSettingsRequest("language_type", ""))
			return
		}
		language = &parsed
	}
	settings, err := locale.UpdateLanguage(s.store.Root(), language)
	if err != nil {
		writeTechnicalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func invalidSettingsRequest(reason, field string) *apperror.Error {
	params := apperror.Params{"reason": reason}
	if field != "" {
		params["field"] = field
	}
	return apperror.New(apperror.CodeInvalidSettingsRequest, params)
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, skipped, err := s.store.List()
	if err != nil {
		writeErrorValue(w, http.StatusInternalServerError, err)
		return
	}
	runningByWorkflow, err := s.runningCounts()
	if err != nil {
		writeErrorValue(w, http.StatusInternalServerError, err)
		return
	}
	summaries := make([]workflowSummary, 0, len(workflows))
	for _, wf := range workflows {
		summaries = append(summaries, workflowSummary{
			Name:         wf.Name,
			NodeIDs:      workflow.AgentNodeIDs(&wf.Definition),
			UpdatedAt:    wf.UpdatedAt,
			RunningCount: runningByWorkflow[wf.Name],
		})
	}
	writeJSON(w, http.StatusOK, workflowsResponse{Workflows: summaries, Warnings: warningsFrom(skipped)})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := workflow.ValidateName(req.Name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	wf := &workflow.Workflow{Name: req.Name, Definition: workflow.Scaffold()}
	if err := s.store.Create(wf); err != nil { // Create 内部戳时间戳 + 落盘
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, wf)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := workflow.ValidateName(name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	wf, err := s.store.Load(name)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	// 刻意不做语义 Validate：编辑器须能载入语义非法的定义去修复（校验在保存时把关，见 handlePutWorkflow）。
	writeJSON(w, http.StatusOK, wf)
}

func (s *Server) handlePutWorkflow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := workflow.ValidateName(name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeTechnicalError(w, http.StatusBadRequest, fmt.Errorf("failed to read request body: %w", err))
		return
	}
	body2, err := workflow.ParseDefinition(body) // 主体或整条记录皆容忍（解包 definition、忽略元数据）；DisallowUnknownFields 拒拼写错误
	if err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	if problems := workflow.ValidateStructured(body2); len(problems) > 0 {
		writeProblems(w, problemsFrom(problems))
		return
	}
	// 乐观并发：客户端带载入时 updatedAt 基线；若已被外部（CLI edit / 另一标签页）改过 → 409 + 现记录，
	// 前端弹「覆盖 / 重载」。软提示不硬锁，不超出 edit 的 last-write-wins。
	if baseline := r.Header.Get("X-Conduct-Base-UpdatedAt"); baseline != "" {
		current, err := s.store.Load(name)
		if err != nil {
			writeErrorValue(w, statusForStoreError(err), err)
			return
		}
		if current.UpdatedAt != baseline {
			writeJSON(w, http.StatusConflict, conflictResponse{
				Error:   envelopeFrom(apperror.New(apperror.CodeWorkflowSaveConflict, nil)),
				Current: current,
			})
			return
		}
	}
	wf := &workflow.Workflow{Name: name, Definition: *body2}
	if err := s.store.ReplaceDefinition(wf); err != nil { // 整体替换兼作坏文件恢复通道；可读时保留 createdAt
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

func (s *Server) handleRenameWorkflow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := workflow.ValidateName(name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	var req renameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := workflow.ValidateName(req.NewName); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Rename(name, req.NewName); err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	wf, err := s.store.Load(req.NewName)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, wf)
}

// handleCopyWorkflow 从 {name} 复制出一份名为 newName 的新工作流（造变体），语义同 CLI `workflow copy`：
// 复制定义主体（nodes + edges，含 START / END）、newName 为全新托管对象（时间戳由 store 重戳）、newName 已存在则拒绝不覆盖。
func (s *Server) handleCopyWorkflow(w http.ResponseWriter, r *http.Request) {
	src := r.PathValue("name")
	if err := workflow.ValidateName(src); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	var req copyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := workflow.ValidateName(req.NewName); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	srcWorkflow, err := s.store.Load(src) // 源不存在 → ErrNotExist → 404
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	copied := srcWorkflow.CopyAs(req.NewName)
	// 防御式校验：源已在库应已合法，仍校验一遍；不过即拒、不写盘（与 CLI copy 同）。
	if err := workflow.Validate(&copied.Definition); err != nil {
		if validationError, ok := apperror.As(err); ok {
			writeApplicationError(w, http.StatusUnprocessableEntity, validationError)
		} else {
			writeTechnicalError(w, http.StatusUnprocessableEntity, err)
		}
		return
	}
	if err := s.store.Create(copied); err != nil { // 目标已存在 → ErrExists → 409；内部戳时间戳 + Normalize + 落盘
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, copied)
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := workflow.ValidateName(name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Delete(name); err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLaunchRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := workflow.ValidateName(name); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	var req launchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	runID, note, err := s.launchRun(name, req.UserPrompt, req.Cwd)
	if err != nil {
		var launchErr *launchError
		if errors.As(err, &launchErr) {
			writeApplicationError(w, launchErr.status, launchErr.applicationError)
			return
		}
		writeTechnicalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, launchResponse{RunID: runID, Note: note})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	records, skipped, err := s.store.ListRuns()
	if err != nil {
		writeTechnicalError(w, http.StatusInternalServerError, err)
		return
	}
	workflowFilter := r.URL.Query().Get("workflow")
	statusFilter := r.URL.Query().Get("status")
	summaries := make([]runSummary, 0, len(records))
	for _, record := range records {
		if workflowFilter != "" && record.Workflow != workflowFilter {
			continue
		}
		effective := record.EffectiveStatus()
		if statusFilter != "" && string(effective) != statusFilter {
			continue
		}
		// 进度分子按唯一 nodeId 且 success 去重（防 resume 后 k>N，见 cli-runtime.md〈run resume〉）。
		progress, err := s.store.CountProgress(record.ID)
		if err != nil {
			writeTechnicalError(w, http.StatusInternalServerError, err)
			return
		}
		summaries = append(summaries, runSummary{
			ID:         record.ID,
			Workflow:   record.Workflow,
			Status:     effective,
			NodeCount:  recordNodeCount(record),
			Progress:   progress,
			StartedAt:  record.StartedAt,
			EndedAt:    record.EndedAt,
			UserPrompt: record.UserPrompt,
		})
	}
	writeJSON(w, http.StatusOK, runsResponse{Runs: summaries, Warnings: warningsFrom(skipped)})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := run.ValidateID(id); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.store.LoadRun(id)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	record.Status = record.EffectiveStatus() // 对外一律展示派生态（running 但 pid 已死 → interrupted）
	detail := runDetail{Record: record}
	if r.URL.Query().Get("trace") == "1" {
		trace, err := s.store.LoadTrace(id)
		if err != nil {
			writeTechnicalError(w, http.StatusInternalServerError, err)
			return
		}
		detail.Trace = &trace // 恒非 nil（LoadTrace 空时返回 []），故 ?trace=1 恒有 trace 字段（空则为 []）
		// 进度按唯一 nodeId 且 success 去重（trace 已在手，直接用纯函数），防 resume 后 k>N。
		detail.Progress = run.ProgressCount(trace)
	} else {
		progress, err := s.store.CountProgress(id)
		if err != nil {
			writeTechnicalError(w, http.StatusInternalServerError, err)
			return
		}
		detail.Progress = progress
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleGetSummary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := run.ValidateID(id); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.store.LoadRun(id)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	if status := record.EffectiveStatus(); status == run.StatusRunning || status == run.StatusInterrupted {
		writeApplicationError(w, http.StatusNotFound, apperror.New(apperror.CodeRunSummaryNotFound, apperror.Params{"id": id}))
		return
	}
	markdown, err := s.store.ReadSummary(id)
	if err != nil {
		if errors.Is(err, store.ErrSummaryNotExist) {
			writeErrorValue(w, http.StatusNotFound, err) // running 期尚未生成，如实 404
			return
		}
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	writeText(w, http.StatusOK, "text/markdown; charset=utf-8", markdown)
}

func (s *Server) handleStopRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := run.ValidateID(id); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.store.LoadRun(id)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	// 用派生态判断：running 且 pid 已死会被折算为 interrupted，天然拦下「进程早没了」的重复终止。
	if status := record.EffectiveStatus(); status != run.StatusRunning {
		writeApplicationError(w, http.StatusConflict, apperror.New(apperror.CodeRunNotStoppable,
			apperror.Params{"id": id, "status": status}))
		return
	}
	if err := run.StopProcess(record.Pid); err != nil {
		writeTechnicalError(w, http.StatusInternalServerError,
			fmt.Errorf("failed to stop run %s (pid %d): %w", id, record.Pid, err))
		return
	}
	writeJSON(w, http.StatusOK, stopResponse{ID: id, Pid: record.Pid, Signal: "SIGTERM"})
}

// handleResumeRun 从中断处恢复一次运行（= conduct run resume <id>）：self-exec 分离子进程续跑，续写原
// run、run id 不变。派生态 failed / interrupted 可恢复，否则 409（对齐 CLI checkResumable 的 fail-loud）；
// 成功 202 返回 {runId}（即原 id）。发射机制复用泛化后的 internal/launch（LaunchResume）。
func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := run.ValidateID(id); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.store.LoadRun(id)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	if status := record.EffectiveStatus(); status != run.StatusFailed && status != run.StatusInterrupted {
		writeApplicationError(w, http.StatusConflict, apperror.New(apperror.CodeRunNotResumable,
			apperror.Params{"id": id, "status": status}))
		return
	}
	runID, note, err := s.resumeRun(id)
	if err != nil {
		var launchErr *launchError
		if errors.As(err, &launchErr) {
			writeApplicationError(w, launchErr.status, launchErr.applicationError)
			return
		}
		writeTechnicalError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, launchResponse{RunID: runID, Note: note})
}

// handleDeleteRun 删除一条运行记录（= conduct run rm <id>）：UI 确认弹窗承担交互确认职责，能力面对齐
// CLI。仅终态可删——running（派生态：pid 存活）→ 409，须先终止再删；不存在 → 404；非法 id → 400。
func (s *Server) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := run.ValidateID(id); err != nil {
		writeErrorValue(w, http.StatusBadRequest, err)
		return
	}
	record, err := s.store.LoadRun(id)
	if err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	// 派生态判断：running 但 pid 已死会折算为 interrupted（可删），与 run rm / run stop 同源。
	if status := record.EffectiveStatus(); status == run.StatusRunning {
		writeApplicationError(w, http.StatusConflict, apperror.New(apperror.CodeRunNotDeletable, apperror.Params{"id": id}))
		return
	}
	if err := s.store.RemoveRun(id); err != nil {
		writeErrorValue(w, statusForStoreError(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFS 为工作目录选择器列出某目录下的子目录（应用内目录浏览器的后端）。conduct ui 只绑
// 127.0.0.1、有 Host/Origin 白名单、单机单用户，浏览的是用户自己账号权限内的文件系统——无提权，
// 故不设根牢笼。只列目录（选工作目录不需要文件），保留隐藏目录（.claude 这类正是常见目标）。
func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		// 未指定则从用户主目录起步（选择器一打开就在 home，符合直觉）。
		home, err := os.UserHomeDir()
		if err != nil {
			writeTechnicalError(w, http.StatusInternalServerError, fmt.Errorf("failed to resolve user home directory: %w", err))
			return
		}
		dir = home
	}
	if !filepath.IsAbs(dir) {
		writeApplicationError(w, http.StatusBadRequest, apperror.New(apperror.CodeWorkingDirectoryMustBeAbs, apperror.Params{"path": dir}))
		return
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeApplicationError(w, http.StatusNotFound, apperror.New(apperror.CodeDirectoryNotFound, apperror.Params{"path": dir}))
			return
		}
		writeTechnicalError(w, http.StatusBadRequest, fmt.Errorf("failed to access directory %s: %w", dir, err))
		return
	}
	if !info.IsDir() {
		writeApplicationError(w, http.StatusBadRequest, apperror.New(apperror.CodePathNotDirectory, apperror.Params{"path": dir}))
		return
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		writeTechnicalError(w, http.StatusForbidden, fmt.Errorf("failed to read directory %s: %w", dir, err))
		return
	}
	entries := make([]fsEntry, 0, len(items))
	for _, it := range items {
		isDir := it.IsDir()
		// 目录符号链接也算：DirEntry.IsDir() 对 symlink 返回 false，需 follow 一次判定。
		if !isDir && it.Type()&os.ModeSymlink != 0 {
			if target, statErr := os.Stat(filepath.Join(dir, it.Name())); statErr == nil && target.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		entries = append(entries, fsEntry{Name: it.Name(), Path: filepath.Join(dir, it.Name())})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	parent := filepath.Dir(dir)
	if parent == dir { // 已到根（filepath.Dir("/")=="/"）
		parent = ""
	}
	writeJSON(w, http.StatusOK, fsListing{Path: dir, Parent: parent, Entries: entries})
}

// ---- 小工具 ----

// decodeJSON 解析请求体 JSON；失败即 400 并返回 false（调用方据此提前 return）。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeApplicationError(w, http.StatusBadRequest, apperror.New(apperror.CodeInvalidRequest, apperror.Params{"reason": "invalid_json"}))
		return false
	}
	return true
}

// runningCounts 统计每个工作流下 running（派生态）的 run 数——工作流列表的「运行中」徽标数据源。
func (s *Server) runningCounts() (map[string]int, error) {
	records, _, err := s.store.ListRuns()
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, record := range records {
		if record.EffectiveStatus() == run.StatusRunning {
			counts[record.Workflow]++
		}
	}
	return counts, nil
}

// recordNodeCount 返回进度分母 N = 快照里的 agent 节点数（排除 START / END）；缺快照返回 0。
func recordNodeCount(record *run.Record) int {
	if record.WorkflowSnapshot == nil {
		return 0
	}
	return record.WorkflowSnapshot.Definition.AgentNodeCount()
}

// warningsFrom 把 store.List / ListRuns 的 skipped 错误转成如实带回前端的告警串（不静默隐藏坏文件）。
func warningsFrom(skipped []error) []string {
	if len(skipped) == 0 {
		return nil
	}
	out := make([]string, len(skipped))
	for i, err := range skipped {
		out[i] = err.Error()
	}
	return out
}

// statusForStoreError 把 store 层的哨兵错误映射为 HTTP 状态码；未识别的一律 500。
func statusForStoreError(err error) int {
	switch {
	case errors.Is(err, store.ErrNotExist), errors.Is(err, store.ErrRunNotExist), errors.Is(err, store.ErrSummaryNotExist):
		return http.StatusNotFound
	case errors.Is(err, store.ErrExists), errors.Is(err, store.ErrRunExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
