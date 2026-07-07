package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/run"
	"github.com/qoggy/conduct/internal/store"
	"github.com/spf13/cobra"
)

func TestEnsureRunDeletableTerminal(t *testing.T) {
	st := store.New(t.TempDir())
	seedRun(t, st, "flow-20260703-150000", run.StatusCompleted, os.Getpid())
	if err := ensureRunDeletable(st, "flow-20260703-150000"); err != nil {
		t.Fatalf("终态运行应可删，得到 %v", err)
	}
}

func TestEnsureRunDeletableRefusesRunning(t *testing.T) {
	st := store.New(t.TempDir())
	// status=running 且 pid 存活（本测试进程 pid）→ 派生仍 running → 拒删。
	seedRun(t, st, "flow-20260703-150000", run.StatusRunning, os.Getpid())
	err := ensureRunDeletable(st, "flow-20260703-150000")
	if err == nil || !strings.Contains(err.Error(), "仍在进行中") {
		t.Fatalf("活运行应被拒删，得到 %v", err)
	}
}

func TestEnsureRunDeletableNotExist(t *testing.T) {
	st := store.New(t.TempDir())
	err := ensureRunDeletable(st, "ghost-20260101-000000")
	if !errors.Is(err, store.ErrRunNotExist) {
		t.Fatalf("不存在应返回 ErrRunNotExist（退 1），得到 %v", err)
	}
}

func TestConfirmRunDeletion(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false},
		{"nope\n", false},
	}
	for _, c := range cases {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader(c.input))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		got, err := confirmRunDeletion(cmd, "flow-x")
		if err != nil {
			t.Fatalf("输入 %q 读取确认不应报错: %v", c.input, err)
		}
		if got != c.want {
			t.Fatalf("输入 %q 期望 confirmed=%v，得到 %v", c.input, c.want, got)
		}
	}
}
