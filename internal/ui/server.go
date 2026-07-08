// Package ui 是 conduct 的可视化界面服务端：一个只绑 127.0.0.1 的本地 HTTP 服务，把 CLI 动词
// 镜像成人看的视图（工作流列表 / 编辑器 / 运行列表 / 运行详情）。关键不变量：UI 无独占能力——
// 每个 /api/* 端点的能力面都不超出其 CLI 等价物（见 docs/specs/ui.md〈API 设计〉）。
//
// 启动运行不在进程内跑 orchestrator，而是 self-exec 自呼 `conduct workflow run`（见 launch.go），
// 使 pid 判活 / interrupted 语义与终端启动逐字节一致，且关掉 UI 不连累在跑的 run。
package ui

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/qoggy/conduct/internal/store"
)

//go:embed all:assets
var assetsFS embed.FS

// Server 持有一次 conduct ui 会话的依赖。now 可注入以便测试；exePath 供 self-exec 发射器自呼；
// stderrDir 是会话私有临时目录，存子进程启动失败的 stderr 兜底（读后即弃，路径绝不进响应）。
type Server struct {
	store     *store.Store
	version   string
	exePath   string
	now       func() time.Time
	stderrDir string
	assets    fs.FS
}

// NewServer 构造服务端：解析自身可执行文件路径（发射器要），建会话私有 stderr 临时目录，
// 并从内嵌资源切出 assets 子树。
func NewServer(st *store.Store, version string) (*Server, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("解析自身可执行文件路径失败: %w", err)
	}
	stderrDir, err := os.MkdirTemp("", "conduct-ui-launch-")
	if err != nil {
		return nil, fmt.Errorf("创建启动日志临时目录失败: %w", err)
	}
	assets, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, fmt.Errorf("加载内嵌前端资源失败: %w", err)
	}
	return &Server{
		store:     st,
		version:   version,
		exePath:   exe,
		now:       time.Now,
		stderrDir: stderrDir,
		assets:    assets,
	}, nil
}

// ListenAndServe 启动并驻留服务，直到进程被 Ctrl-C 终止。
// 启动前主动探测 store 可读性、绑定端口，任一失败返回 error（命令层据此 stderr 退 1，不做启动假成功）。
func (s *Server) ListenAndServe(port int, open bool) error {
	// 启动即执行一次 List，确认 store 可读——不做「启动假成功、首个请求才报错」。
	if _, _, err := s.store.List(); err != nil {
		return fmt.Errorf("store 不可读: %w", err)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("监听 127.0.0.1:%d 失败（端口可能已被占用）: %w", port, err)
	}
	// 用监听器**实际**绑定的端口（--port 0 时系统随机分配）构建白名单与入口 URL，
	// 否则字面 :0 与浏览器带来的真实端口对不上，会导致每个请求都 403。
	actualPort := listener.Addr().(*net.TCPAddr).Port
	entryURL := fmt.Sprintf("http://127.0.0.1:%d", actualPort)
	fmt.Println("conduct ui — 可视化界面已启动")
	fmt.Printf("  ▶ %s\n", entryURL)
	fmt.Println("按 Ctrl-C 退出。")
	if open {
		s.openBrowser(entryURL)
	}
	return http.Serve(listener, s.routes(actualPort))
}

// routes 组装路由表 + 中间件。方法 + 通配模式由 net/http（go 1.22+）原生支持。
func (s *Server) routes(port int) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("GET /api/engines", s.handleEngines)

	mux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	mux.HandleFunc("POST /api/workflows", s.handleCreateWorkflow)
	mux.HandleFunc("GET /api/workflows/{name}", s.handleGetWorkflow)
	mux.HandleFunc("PUT /api/workflows/{name}", s.handlePutWorkflow)
	mux.HandleFunc("DELETE /api/workflows/{name}", s.handleDeleteWorkflow)
	mux.HandleFunc("POST /api/workflows/{name}/rename", s.handleRenameWorkflow)
	mux.HandleFunc("POST /api/workflows/{name}/copy", s.handleCopyWorkflow)
	mux.HandleFunc("POST /api/workflows/{name}/runs", s.handleLaunchRun)

	mux.HandleFunc("GET /api/fs", s.handleFS)

	mux.HandleFunc("GET /api/runs", s.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /api/runs/{id}/summary", s.handleGetSummary)
	mux.HandleFunc("POST /api/runs/{id}/stop", s.handleStopRun)
	mux.HandleFunc("POST /api/runs/{id}/resume", s.handleResumeRun)

	// 前端静态资源（内嵌）。hash 路由使浏览器只请求 / 与资源文件本身，无需 history fallback。
	mux.Handle("/", http.FileServer(http.FS(s.assets)))

	return s.withGuards(mux, port)
}

// withGuards 套三层防护：① 所有响应 no-store（防浏览器缓存让刷新失真）；② Host / Origin 白名单
// （防浏览器跨站与 DNS rebinding，诚实边界：不防本机进程）；③ 变更类 /api 端点强制 application/json
// （连带挡住表单式 CSRF——form 无法发 application/json）。
func (s *Server) withGuards(next http.Handler, port int) http.Handler {
	allowedHosts := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", port): true,
		fmt.Sprintf("localhost:%d", port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")           // 禁被 iframe 嵌入：Host 白名单挡不住嵌入，避免点击劫持诱导变更操作
		w.Header().Set("X-Content-Type-Options", "nosniff") // 半可信的 text/markdown 运行总结不被浏览器嗅探执行为 HTML

		if !allowedHosts[r.Host] {
			writeError(w, http.StatusForbidden, fmt.Sprintf("拒绝：Host %q 不在白名单（仅限本机访问）", r.Host))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !allowedHosts[parsed.Host] {
				writeError(w, http.StatusForbidden, fmt.Sprintf("拒绝：Origin %q 不在白名单", origin))
				return
			}
		}
		// 仅当请求带 body 时才要求 application/json：无 body 的变更请求（如 DELETE 工作流）本就不带
		// Content-Type，浏览器不会补。放行它们不削弱 CSRF 防护——跨站 form 发不出 DELETE/PUT，跨站
		// fetch 必带 Origin（已被上面白名单拦），带 body 的跨站 form POST 仍会因非 JSON 被这里拦下。
		if isMutatingMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") && r.ContentLength != 0 {
			if contentType := r.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "变更类请求必须为 application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

// openBrowser 尽力打开系统浏览器；失败只告警不致命（照顾 SSH / 无头环境）。
func (s *Server) openBrowser(entryURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", entryURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", entryURL)
	default:
		cmd = exec.Command("xdg-open", entryURL)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法自动打开浏览器（请手动访问 %s）: %v\n", entryURL, err)
	}
}
