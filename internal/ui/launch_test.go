package ui

import (
	"errors"
	"net/http"
	"testing"

	"github.com/qoggy/conduct/internal/workflow"
)

// seedWorkflow 直接把一份定义落进 store（store.Create 只校验名字、不做语义校验，
// 故可落一份 nodes 为空的定义来驱动预检的 422 分支）。
func seedWorkflow(t *testing.T, s *Server, def *workflow.Definition) {
	t.Helper()
	if err := s.store.Create(def); err != nil {
		t.Fatalf("落工作流失败: %v", err)
	}
}

// scaffoldNamed 返回一份带名字的最小合法定义（单节点脚手架）。
func scaffoldNamed(name string) *workflow.Definition {
	def := workflow.Scaffold()
	def.Name = name
	return def
}

func asLaunchError(t *testing.T, err error) *launchError {
	t.Helper()
	var launchErr *launchError
	if !errors.As(err, &launchErr) {
		t.Fatalf("期望 *launchError，得到 %T: %v", err, err)
	}
	return launchErr
}

func TestPreflightWorkflowNotExist(t *testing.T) {
	s := newTestServer(t)
	_, err := s.preflight("ghost", "需求", t.TempDir())
	if asLaunchError(t, err).status != http.StatusNotFound {
		t.Fatalf("不存在工作流应 404，得到 %d", asLaunchError(t, err).status)
	}
}

func TestPreflightInvalidDefinition(t *testing.T) {
	s := newTestServer(t)
	seedWorkflow(t, s, &workflow.Definition{Name: "empty", Nodes: nil}) // 空 nodes → 语义非法
	_, err := s.preflight("empty", "需求", t.TempDir())
	le := asLaunchError(t, err)
	if le.status != http.StatusUnprocessableEntity {
		t.Fatalf("损坏定义应 422，得到 %d", le.status)
	}
	if len(le.problems) == 0 {
		t.Fatalf("422 应带字段级 problems")
	}
}

func TestPreflightEmptyPrompt(t *testing.T) {
	s := newTestServer(t)
	seedWorkflow(t, s, scaffoldNamed("demo"))
	_, err := s.preflight("demo", "   ", t.TempDir())
	if asLaunchError(t, err).status != http.StatusBadRequest {
		t.Fatalf("空需求应 400")
	}
}

func TestPreflightBadCwd(t *testing.T) {
	s := newTestServer(t)
	seedWorkflow(t, s, scaffoldNamed("demo"))
	_, err := s.preflight("demo", "需求", "/no/such/dir/xyz")
	if asLaunchError(t, err).status != http.StatusBadRequest {
		t.Fatalf("不存在的 cwd 应 400")
	}
}

func TestPreflightOK(t *testing.T) {
	s := newTestServer(t)
	seedWorkflow(t, s, scaffoldNamed("demo"))
	dir := t.TempDir()
	abs, err := s.preflight("demo", "需求", dir)
	if err != nil {
		t.Fatalf("合法预检不应报错: %v", err)
	}
	if abs == "" {
		t.Fatalf("应返回绝对化后的工作目录")
	}
}
