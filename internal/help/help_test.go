package help

import (
	"strings"
	"testing"
)

// TestTopicsLoad 钉住「登记的每个主题都能加载到非空内嵌内容」这一构建期不变量：
// 往 topics 加了一行却漏放 / 写错 .md 文件名时，此测试立刻失败，而非留给用户在运行时踩。
func TestTopicsLoad(t *testing.T) {
	all := Topics()
	if len(all) == 0 {
		t.Fatal("未登记任何 help 主题")
	}
	for _, topic := range all {
		if topic.Name == "" || topic.Short == "" {
			t.Errorf("主题 %+v 的 Name / Short 不得为空", topic)
		}
		content, ok, err := Content(topic.Name)
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
	}
}

// TestContentUnknownTopic 未登记主题返回 ok=false、无错误（供上层报「未知主题」）。
func TestContentUnknownTopic(t *testing.T) {
	content, ok, err := Content("no-such-topic")
	if err != nil {
		t.Fatalf("未知主题不应返回错误，得到: %v", err)
	}
	if ok || content != "" {
		t.Fatalf("未知主题应返回 (\"\", false)，得到 (%q, %v)", content, ok)
	}
}
