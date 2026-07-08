package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/workflow"
)

// 本文件集中 /api/* 的请求 / 响应 JSON 结构与响应写出辅助。所有能力面都不超出其 CLI 等价物
// （见 docs/specs/ui.md〈API 设计〉），错误文案与内核 / CLI stderr 同源。

// ---- 错误信封 ----

// errorResponse 是所有非 2xx 的统一响应体。Problems 仅在 422 校验失败时出现，
// 逐条对应 workflow.ValidateStructured 的字段级错误（供编辑器点错误定位到字段）。
type errorResponse struct {
	Error    string    `json:"error"`
	Problems []problem `json:"problems,omitempty"`
}

// problem 是一条字段级校验错误，镜像 workflow.Problem（不复用内核类型是为了独立控制 JSON 标签）。
type problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func problemsFrom(structured []workflow.Problem) []problem {
	out := make([]problem, len(structured))
	for i, p := range structured {
		out[i] = problem{Path: p.Path, Message: p.Message}
	}
	return out
}

// ---- 请求体 ----

type createRequest struct {
	Name string `json:"name"`
}

type renameRequest struct {
	NewName string `json:"newName"`
}

type copyRequest struct {
	NewName string `json:"newName"`
}

type launchRequest struct {
	UserPrompt string `json:"userPrompt"`
	Cwd        string `json:"cwd"`
}

// ---- 响应体 ----

type versionResponse struct {
	Version string `json:"version"`
}

// engineInfo 是 GET /api/engines 的单个条目。Capability 为 nil（JSON null）表示引擎已注册但
// 能力表尚未实装——不得误报成 allowsModel:false（见 ui.md〈需要额外实现〉①）。
type engineInfo struct {
	Name       string            `json:"name"`
	Capability *engineCapability `json:"capability"`
}

type engineCapability struct {
	AllowsModel  bool     `json:"allowsModel"`
	EffortField  string   `json:"effortField"`
	EffortValues []string `json:"effortValues"`
}

func engineInfoOf(name string) engineInfo {
	capability, ok := engine.Capability(name)
	if !ok {
		return engineInfo{Name: name, Capability: nil}
	}
	return engineInfo{
		Name: name,
		Capability: &engineCapability{
			AllowsModel:  capability.AllowsModel,
			EffortField:  capability.EffortField,
			EffortValues: capability.EffortValues,
		},
	}
}

// workflowSummary 是工作流列表的单项：节点 id 流（对齐 ui.md 的节点流列）+ 该工作流下 running 计数。
// 一切字段都可由 CLI 等价能力得出（节点 id 见 show、running 计数 = run list 过滤聚合）。
type workflowSummary struct {
	Name         string   `json:"name"`
	NodeIDs      []string `json:"nodeIds"`
	UpdatedAt    string   `json:"updatedAt"`
	RunningCount int      `json:"runningCount"`
}

type workflowsResponse struct {
	Workflows []workflowSummary `json:"workflows"`
	// Warnings 如实带回 store 里解析失败的坏文件（对齐 store.List 的 skipped 语义，不静默隐藏）。
	Warnings []string `json:"warnings,omitempty"`
}

// runSummary 是运行列表的精简单项：裁掉 workflowSnapshot / artifacts 大字段（等价 run list）。
// Status 是读时派生的 EffectiveStatus；Progress 是 k/N 的 k——按唯一 stepIndex 且 success 去重的
// 进度分子（store.CountProgress，非物理行数，防 resume 后 k>N）。
type runSummary struct {
	ID         string     `json:"id"`
	Workflow   string     `json:"workflow"`
	Status     run.Status `json:"status"`
	Steps      int        `json:"steps"`
	Progress   int        `json:"progress"`
	StartedAt  string     `json:"startedAt"`
	EndedAt    *string    `json:"endedAt"`
	UserPrompt string     `json:"userPrompt"`
}

type runsResponse struct {
	Runs     []runSummary `json:"runs"`
	Warnings []string     `json:"warnings,omitempty"`
}

// runDetail 是运行详情：完整 run.Record（Status 已覆写为派生态）+ 进度 + 可选 trace 全文。
// 内嵌 *run.Record 使其字段提升到顶层，与 run show --json 的形态一致。
type runDetail struct {
	*run.Record
	Progress int `json:"progress"`
	// Trace 用指针区分「未请求」与「请求了但为空」：未带 ?trace=1 → nil → omitempty 省略；
	// 带 ?trace=1 → 非 nil（空则为 []），恒有 trace 字段（数组语义，与 run show --json --trace 一致）。
	Trace *[]run.TraceEntry `json:"trace,omitempty"`
}

type launchResponse struct {
	RunID string `json:"runId"`
	// Note 仅在「已发射但未能在超时内确认 run id（子进程仍在跑）」时出现，引导用户去运行列表核对，
	// 不误报失败（见 ui.md〈启动运行机制〉超时行）。
	Note string `json:"note,omitempty"`
}

// conflictResponse 是乐观并发 409 的响应：带回当前定义，供前端弹「覆盖 / 重载」。
type conflictResponse struct {
	Error   string               `json:"error"`
	Current *workflow.Definition `json:"current"`
}

// stopResponse 是 POST /api/runs/{id}/stop 的成功回执。
type stopResponse struct {
	ID     string `json:"id"`
	Pid    int    `json:"pid"`
	Signal string `json:"signal"`
}

// ---- 目录浏览（工作目录选择器 GET /api/fs）----

// fsListing 是某个目录的浏览结果：当前目录、其父目录（到根则空）、以及其下的子目录列表。
// 只列目录（工作目录选择器无需文件），含隐藏目录（.claude 这类正是常见目标）。
type fsListing struct {
	Path    string    `json:"path"`
	Parent  string    `json:"parent"` // 到达根目录时为空串
	Entries []fsEntry `json:"entries"`
}

type fsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ---- 响应写出辅助 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应体已开始写、状态码无法再改，只能记录不静默（承「错误不吞」）。
		fmt.Fprintf(os.Stderr, "conduct ui: 序列化响应失败: %v\n", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeProblems(w http.ResponseWriter, message string, problems []problem) {
	writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: message, Problems: problems})
}

func writeText(w http.ResponseWriter, status int, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body); err != nil {
		fmt.Fprintf(os.Stderr, "conduct ui: 写响应失败: %v\n", err)
	}
}
