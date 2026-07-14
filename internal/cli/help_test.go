package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestHelpRejectsUnknownCommandPathSuffix(t *testing.T) {
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"help", "workflow", "does-not-exist"})

	err = root.Execute()
	var usage *usageError
	if !errors.As(err, &usage) {
		t.Fatalf("未知命令路径应返回 usageError（退 2），得到 %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "workflow does-not-exist") {
		t.Fatalf("错误应包含完整未知路径，得到 %v", err)
	}
}

func TestHelpAcceptsKnownNestedCommandPath(t *testing.T) {
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"help", "workflow", "node", "set"})

	if err := root.Execute(); err != nil {
		t.Fatalf("已知多级命令路径应成功，得到 %v", err)
	}
	if !strings.Contains(output.String(), "conduct workflow node set <name> <id>") {
		t.Fatalf("应打印目标命令帮助，得到：\n%s", output.String())
	}
}
