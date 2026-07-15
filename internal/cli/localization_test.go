package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/apperror"
	"github.com/qoggy/conduct/internal/locale"
)

func useTestLanguage(t *testing.T, language locale.Language) {
	t.Helper()
	previous := selectedLanguage
	selectedLanguage = language
	t.Cleanup(func() { selectedLanguage = previous })
}

func isolateTestSettings(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestCLIEnglishProductSurfacesContainNoChinese(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")

	outputs := []string{
		executeCommandForLocalization(t, nil, "workflow", "list"),
		executeCommandForLocalization(t, nil, "workflow", "create", "demo"),
		executeCommandForLocalization(t, nil, "workflow", "show", "missing"),
		executeCommandForLocalization(t, nil, "unknown"),
		executeCommandForLocalization(t, strings.NewReader(`{"nodes":[]}`), "workflow", "create", "invalid", "--definition"),
	}
	hanzi := regexp.MustCompile(`[\p{Han}]`)
	for _, output := range outputs {
		if hanzi.MatchString(output) {
			t.Errorf("English CLI output contains Chinese product text:\n%s", output)
		}
	}
}

func TestCLIChineseProductTextAndEnglishTechnicalDiagnostic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")

	for _, test := range []struct {
		arguments []string
		want      string
	}{
		{arguments: []string{"workflow", "list"}, want: "（store 为空）"},
		{arguments: []string{"workflow", "create", "demo"}, want: "已创建"},
		{arguments: []string{"workflow", "show", "missing"}, want: "工作流 missing 不存在"},
		{arguments: []string{"unknown"}, want: "未知命令"},
	} {
		output := executeCommandForLocalization(t, nil, test.arguments...)
		if !strings.Contains(output, test.want) {
			t.Errorf("output missing %q:\n%s", test.want, output)
		}
	}

	settingsPath := filepath.Join(home, ".conduct", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"language":`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := newRootCommand()
	if err == nil || !strings.Contains(err.Error(), "failed to parse ~/.conduct/settings.json") {
		t.Fatalf("invalid settings diagnostic = %v", err)
	}
}

func TestCLIUsageErrorsUseSelectedLanguage(t *testing.T) {
	for _, test := range []struct {
		name        string
		language    string
		arguments   []string
		want        string
		doesNotWant string
	}{
		{name: "Chinese unknown flag", language: "zh_CN.UTF-8", arguments: []string{"--unknown"}, want: "未知选项 --unknown", doesNotWant: "unknown flag"},
		{name: "Chinese invalid flag value", language: "zh_CN.UTF-8", arguments: []string{"ui", "--port", "nope"}, want: "选项 --port 的值 \"nope\" 无效", doesNotWant: "invalid argument"},
		{name: "Chinese too few arguments", language: "zh_CN.UTF-8", arguments: []string{"workflow", "show"}, want: "需要 1 个位置参数，收到 0 个", doesNotWant: "accepts 1 arg"},
		{name: "Chinese too many arguments", language: "zh_CN.UTF-8", arguments: []string{"workflow", "show", "one", "two"}, want: "需要 1 个位置参数，收到 2 个", doesNotWant: "accepts 1 arg"},
		{name: "English unknown flag", language: "C", arguments: []string{"--unknown"}, want: "unknown flag: --unknown"},
		{name: "English invalid flag value", language: "C", arguments: []string{"ui", "--port", "nope"}, want: "invalid argument \"nope\""},
		{name: "English too few arguments", language: "C", arguments: []string{"workflow", "show"}, want: "requires 1 positional argument; received 0"},
		{name: "English too many arguments", language: "C", arguments: []string{"workflow", "show", "one", "two"}, want: "requires 1 positional argument; received 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolateTestSettings(t)
			t.Setenv("LC_ALL", test.language)
			output := executeCommandForLocalization(t, nil, test.arguments...)
			if !strings.Contains(output, test.want) {
				t.Fatalf("output missing %q:\n%s", test.want, output)
			}
			if test.doesNotWant != "" && strings.Contains(output, test.doesNotWant) {
				t.Fatalf("output must not contain %q:\n%s", test.doesNotWant, output)
			}
		})
	}
}

func TestCLIInvalidResumeSnapshotKeepsEnglishTechnicalContextInChineseMode(t *testing.T) {
	useTestLanguage(t, locale.Chinese)
	err := apperror.Technicalf(
		apperror.Validation([]apperror.Problem{{Path: "nodes", Code: apperror.CodeNodesRequired}}),
		"workflowSnapshot for run demo failed validation and cannot be resumed",
	)
	got := formatCLIError(err)
	if got != "workflowSnapshot for run demo failed validation and cannot be resumed" {
		t.Fatalf("technical context was not preserved: %q", got)
	}
}

func executeCommandForLocalization(t *testing.T, input *strings.Reader, arguments ...string) string {
	t.Helper()
	command, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(arguments)
	if input != nil {
		command.SetIn(input)
	}
	err = command.Execute()
	if err != nil {
		stderr.WriteString(formatCLIError(err))
	}
	return stdout.String() + stderr.String()
}
