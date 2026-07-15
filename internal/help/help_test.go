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
		name     string
		language locale.Language
		title    string
		heading  string
	}{
		{name: "chinese", language: locale.Chinese, title: "# 写好节点 promptTemplate", heading: "`## 标题`"},
		{name: "english", language: locale.English, title: "# Writing a Good Node promptTemplate", heading: "`## Heading`"},
	}
	for _, language := range languages {
		t.Run(language.name, func(t *testing.T) {
			all := Topics(language.language)
			if len(all) == 0 {
				t.Fatal("未登记任何 help 主题")
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
				if !strings.HasPrefix(content, language.title) {
					t.Errorf("主题 %q 未加载对应语言正文，首行得到 %q", topic.Name, strings.SplitN(content, "\n", 2)[0])
				}
				if topic.Name == "prompts" && strings.Count(content, language.heading) != 2 {
					t.Errorf("主题 %q 应原样保留两处 Markdown 标题示例 %s", topic.Name, language.heading)
				}
				if language.language == locale.English && regexp.MustCompile(`\p{Han}`).MatchString(content) {
					t.Errorf("英文主题 %q 不得包含汉字", topic.Name)
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
