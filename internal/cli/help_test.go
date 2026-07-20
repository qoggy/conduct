package cli

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/engine"
	"github.com/qoggy/conduct/internal/locale"
	"github.com/spf13/cobra"
)

func TestHelpRejectsUnknownCommandPathSuffix(t *testing.T) {
	isolateTestSettings(t)
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
	isolateTestSettings(t)
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

func TestHelpUsesSelectedLanguage(t *testing.T) {
	isolateTestSettings(t)
	tests := []struct {
		name        string
		language    string
		args        []string
		want        []string
		doesNotWant []string
	}{
		{
			name:     "chinese root help",
			language: "zh_CN.UTF-8",
			args:     []string{"help"},
			want: []string{
				"conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。",
				"支持 " + strings.Join(engine.RegisteredNames(), "、") + " 引擎",
				"怎么写好节点 promptTemplate：模板变量、节点隔离、最佳实践",
				"用法：",
				"可用命令：",
				"选项：",
				"其它帮助主题：",
			},
			doesNotWant: []string{
				"conduct — a CLI that interprets and runs workflow definitions (JSON).",
				"Usage:", "Available Commands:", "Flags:", "Additional help topics:",
				"Generate the autocompletion script for the specified shell",
			},
		},
		{
			name:     "chinese kiro node help",
			language: "zh_CN.UTF-8",
			args:     []string{"help", "workflow", "node", "set"},
			want: []string{
				"设引擎（" + engineNamesHelp() + "）",
				"设推理档位（" + effortValuesHelp() + "）；传空串清除",
			},
			doesNotWant: []string{"Set the engine", "Set the reasoning effort level", "--reasoning-effort"},
		},
		{
			name:     "english nested command help",
			language: "en_US.UTF-8",
			args:     []string{"help", "workflow", "node", "set"},
			want: []string{
				"Change only one agent node's fields",
				"Set the engine (" + engineNamesHelp() + ")",
				"Set the reasoning effort level (" + effortValuesHelp() + ")",
				"Change the node display name (must be nonempty)",
			},
			doesNotWant: []string{"只改一个 agent 节点的字段", "设引擎（", "--reasoning-effort"},
		},
		{
			name:     "english root help",
			language: "en_US.UTF-8",
			args:     []string{"help"},
			want: []string{
				"conduct — a CLI that interprets and runs workflow definitions (JSON).",
				"How to write a good node promptTemplate: template variables, node isolation, and best practices",
			},
			doesNotWant: []string{"conduct —— 一个把 workflow 定义（JSON）解释运行起来的 CLI。", "--lang"},
		},
		{
			name:     "english topic",
			language: "C",
			args:     []string{"help", "prompts"},
			want: []string{
				"# Writing a Good Node promptTemplate",
				"Artifacts from parallel branches are not merged automatically",
			},
			doesNotWant: []string{"# 写好节点 promptTemplate"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LC_ALL", test.language)
			t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
			t.Setenv("LANG", "zh_CN.UTF-8")
			root, err := newRootCommand()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(test.args)
			if err := root.Execute(); err != nil {
				t.Fatalf("help 执行失败: %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help 缺少 %q，得到：\n%s", want, output.String())
				}
			}
			for _, notWant := range test.doesNotWant {
				if strings.Contains(output.String(), notWant) {
					t.Errorf("help 不应包含 %q，得到：\n%s", notWant, output.String())
				}
			}
		})
	}
}

func TestEnglishRootHelpContainsNoChinese(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("英文根帮助执行失败: %v", err)
	}
	if regexp.MustCompile(`\p{Han}`).MatchString(output.String()) {
		t.Fatalf("英文根帮助不应包含汉字，得到：\n%s", output.String())
	}
	if !strings.Contains(output.String(), "conduct help <topic>") {
		t.Fatalf("英文根帮助应使用英文 topic 占位符，得到：\n%s", output.String())
	}
}

func TestReasoningEffortFlagIsOrdinaryUnknownFlag(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "C")
	execute := func(flag string) string {
		root, err := newRootCommand()
		if err != nil {
			t.Fatal(err)
		}
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"workflow", "node", "set", "flow", "node", "--" + flag, "high"})
		err = root.Execute()
		var usage *usageError
		if !errors.As(err, &usage) {
			t.Fatalf("未知 flag %s 应返回 usageError，得到 %T: %v", flag, err, err)
		}
		return strings.ReplaceAll(err.Error(), flag, "<unknown>")
	}
	if reasoning, ordinary := execute("reasoning-effort"), execute("xxxabc"); reasoning != ordinary {
		t.Fatalf("--reasoning-effort 应与普通未知 flag 走同一路径：\nreasoning=%s\nordinary=%s", reasoning, ordinary)
	}
}

func TestEngineHelpFormattingUsesSyntheticDescriptors(t *testing.T) {
	descriptors := []engine.EngineDescriptor{
		{Name: "alpha", Capability: engine.EngineCapability{AllowsModel: true}},
		{Name: "zeta", Capability: engine.EngineCapability{AllowsEffort: true, EffortValues: []string{"small", "large"}}},
	}
	if got := engineNamesHelpFrom(descriptors); got != "alpha / zeta" {
		t.Fatalf("synthetic engine names = %q", got)
	}
	if got := effortValuesHelpFrom(descriptors); got != "zeta:small|large" {
		t.Fatalf("synthetic effort values = %q", got)
	}
	useTestLanguage(t, locale.English)
	if got := engineConfigFieldsHelp(engine.EngineCapability{}); got != "accepts no engineConfig fields" {
		t.Fatalf("no-field engineConfig help = %q", got)
	}
	if got := engineConfigFieldsHelp(engine.EngineCapability{AllowsModel: true}); got != "model" {
		t.Fatalf("model-only engineConfig help = %q", got)
	}
}

func TestAllEnglishCommandHelpContainsNoChinese(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	hasHan := regexp.MustCompile(`\p{Han}`)
	var check func(*cobra.Command)
	check = func(command *cobra.Command) {
		t.Helper()
		var output bytes.Buffer
		command.SetOut(&output)
		if err := command.Help(); err != nil {
			t.Errorf("%s 英文帮助执行失败: %v", command.CommandPath(), err)
			return
		}
		if hasHan.MatchString(output.String()) {
			t.Errorf("%s 英文帮助不应包含汉字，得到：\n%s", command.CommandPath(), output.String())
		}
		for _, child := range command.Commands() {
			check(child)
		}
	}
	check(root)
}

func TestChineseCompletionHelpIsFullyLocalized(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LC_MESSAGES", "C")
	t.Setenv("LANG", "C")
	tests := []struct {
		arguments []string
		want      string
	}{
		{[]string{"completion", "--help"}, "有关如何使用生成脚本的详细信息"},
		{[]string{"completion", "bash", "--help"}, "此脚本依赖 'bash-completion' 包"},
		{[]string{"completion", "zsh", "--help"}, "如果当前环境尚未启用 shell 补全"},
		{[]string{"completion", "fish", "--help"}, "为 fish shell 生成自动补全脚本"},
		{[]string{"completion", "powershell", "--help"}, "添加到 powershell 配置文件"},
	}
	englishProse := []string{
		"Generate the autocompletion script", "To load completions", "You will need to start a new shell",
		"disable completion descriptions",
	}
	for _, test := range tests {
		t.Run(strings.Join(test.arguments, " "), func(t *testing.T) {
			root, err := newRootCommand()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(test.arguments)
			if err := root.Execute(); err != nil {
				t.Fatalf("completion help 执行失败: %v", err)
			}
			if !strings.Contains(output.String(), test.want) {
				t.Errorf("completion help 缺少 %q，得到：\n%s", test.want, output.String())
			}
			for _, phrase := range englishProse {
				if strings.Contains(output.String(), phrase) {
					t.Errorf("completion help 泄漏英文文案 %q，得到：\n%s", phrase, output.String())
				}
			}
		})
	}
}

func TestChineseCompletionRejectsPositionalArgumentsInChinese(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"completion", "bash", "extra"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "不接受位置参数") {
		t.Fatalf("completion 多余参数应返回中文用法错误，得到 %v", err)
	}
}

func TestUnknownHelpTopicUsesSelectedLanguage(t *testing.T) {
	isolateTestSettings(t)
	t.Setenv("LC_ALL", "en_US.UTF-8")
	root, err := newRootCommand()
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"help", "does-not-exist"})

	err = root.Execute()
	var usage *usageError
	if !errors.As(err, &usage) {
		t.Fatalf("未知帮助主题应返回 usageError（退 2），得到 %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Unknown help topic") || regexp.MustCompile(`\p{Han}`).MatchString(err.Error()) {
		t.Fatalf("未知帮助主题错误应使用英文，得到 %v", err)
	}
}
