package help

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/locale"
)

// TestTopicsLoad 钉住「登记的每个主题都能加载到非空内嵌内容」这一构建期不变量：
// 往 topics 加了一行却漏放 / 写错 .md 文件名时，此测试立刻失败，而非留给用户在运行时踩。
func TestTopicsLoad(t *testing.T) {
	languages := []struct {
		name            string
		language        locale.Language
		titles          map[string]string
		markdownHeading string
	}{
		{
			name:     "chinese",
			language: locale.Chinese,
			titles: map[string]string{
				"prompts":     "# 写好节点 promptTemplate",
				"run-prompts": "# workflow run 的任务提示词",
			},
			markdownHeading: "`## 标题`",
		},
		{
			name:     "english",
			language: locale.English,
			titles: map[string]string{
				"prompts":     "# Writing a Good Node promptTemplate",
				"run-prompts": "# Task Prompts for workflow run",
			},
			markdownHeading: "`## Heading`",
		},
	}
	for _, language := range languages {
		t.Run(language.name, func(t *testing.T) {
			all := Topics(language.language)
			if len(all) != len(language.titles) {
				t.Fatalf("help 主题数 = %d，期望 %d", len(all), len(language.titles))
			}
			for _, topic := range all {
				if topic.Name == "" || topic.Short == "" {
					t.Errorf("主题 %+v 的 Name / Short 不得为空", topic)
				}
				content, ok, err := Content(topic.Name, language.language)
				if err != nil {
					t.Errorf("加载主题 %q 失败: %v", topic.Name, err)
					continue
				}
				if !ok {
					t.Errorf("主题 %q 已登记却 Content 返回 ok=false", topic.Name)
					continue
				}
				if strings.TrimSpace(content) == "" {
					t.Errorf("主题 %q 的内嵌内容为空", topic.Name)
				}
				title, exists := language.titles[topic.Name]
				if !exists {
					t.Errorf("主题 %q 没有登记预期标题", topic.Name)
					continue
				}
				if !strings.HasPrefix(content, title) {
					t.Errorf("主题 %q 未加载对应语言正文，首行得到 %q", topic.Name, strings.SplitN(content, "\n", 2)[0])
				}
				if topic.Name == "prompts" && strings.Count(content, language.markdownHeading) != 2 {
					t.Errorf("主题 %q 应原样保留两处 Markdown 标题示例 %s", topic.Name, language.markdownHeading)
				}
				if language.language == locale.English && regexp.MustCompile(`\p{Han}`).MatchString(content) {
					t.Errorf("英文主题 %q 不得包含汉字", topic.Name)
				}
			}
		})
	}
}

func TestRunPromptsTopicFollowsTaskPromptWritingPrinciples(t *testing.T) {
	tests := []struct {
		name     string
		language locale.Language
		want     []string
	}{
		{
			name:     "chinese",
			language: locale.Chinese,
			want:     []string{"刚走进房间的聪明同事", "已经了解或排除了什么", "查询任务：给出准确命令", "不要委派理解"},
		},
		{
			name:     "english",
			language: locale.English,
			want:     []string{"smart colleague who just walked into the room", "already learned or ruled out", "For a lookup, provide the exact command", "Never delegate understanding"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content, ok, err := Content("run-prompts", test.language)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatal("run-prompts 主题未登记")
			}
			for _, want := range test.want {
				if !strings.Contains(content, want) {
					t.Errorf("run-prompts 主题缺少 %q", want)
				}
			}
		})
	}
}

// TestContentUnknownTopic 未登记主题返回 ok=false、无错误（供上层报「未知主题」）。
func TestContentUnknownTopic(t *testing.T) {
	content, ok, err := Content("no-such-topic", locale.English)
	if err != nil {
		t.Fatalf("未知主题不应返回错误，得到: %v", err)
	}
	if ok || content != "" {
		t.Fatalf("未知主题应返回 (\"\", false)，得到 (%q, %v)", content, ok)
	}
}
