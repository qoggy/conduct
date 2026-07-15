// Package help 承载 conduct 的长文档主题——教程 / 概念 / 最佳实践这类跨命令的深度内容。
// 各命令的 --help 只做精简速查（本命令怎么用）；不适合塞进 --help 的长文档放这里，
// 经 `conduct help <主题>` 输出。文档用 go:embed 打进二进制：conduct 走 go install，
// 源码仓库的 docs/ 不随二进制发布，长文档必须内嵌才能被已安装的用户 / 沙箱 AI 读到。
//
// 本包只管内容与注册，不依赖 cobra；命令布线在 internal/cli/help.go。
package help

import (
	"embed"
	"fmt"

	"github.com/qoggy/conduct/internal/locale"
)

//go:embed *.md
var topicFiles embed.FS

// Topic 是一个 help 主题：一份跨命令的长文档。
type Topic struct {
	Name  string // 调用名，如 "prompts"（→ conduct help prompts）
	Short string // 一行简介，列在主题清单里
}

type topicDefinition struct {
	name         string
	chineseShort string
	englishShort string
	chineseFile  string
	englishFile  string
}

// topicDefinitions 是全部已注册主题。新增主题：往本包放中英文 .md，并在此登记一行。
var topicDefinitions = []topicDefinition{
	{
		name:         "prompts",
		chineseShort: "怎么写好节点 promptTemplate：模板变量、节点隔离、最佳实践",
		englishShort: "How to write a good node promptTemplate: template variables, node isolation, and best practices",
		chineseFile:  "prompts.md",
		englishFile:  "prompts.en.md",
	},
}

// Topics 返回全部已注册主题的只读快照（按登记顺序）。
func Topics(language locale.Language) []Topic {
	out := make([]Topic, 0, len(topicDefinitions))
	for _, definition := range topicDefinitions {
		out = append(out, Topic{
			Name:  definition.name,
			Short: language.Select(definition.chineseShort, definition.englishShort),
		})
	}
	return out
}

// Content 返回指定主题的 markdown 正文；主题未登记时 ok=false（供调用方报「未知主题」）。
// 主题已登记却读不到内嵌文件属构建期不变量被破坏（.md 缺失或改名未同步登记），
// go:embed 会在编译期先行拦截；此处仍显式转译该错误上抛，不静默返回空串。
func Content(name string, language locale.Language) (string, bool, error) {
	for _, definition := range topicDefinitions {
		if definition.name != name {
			continue
		}
		file := language.Select(definition.chineseFile, definition.englishFile)
		data, err := topicFiles.ReadFile(file)
		if err != nil {
			return "", true, fmt.Errorf("failed to read embedded file %q for help topic %q: %w", file, name, err)
		}
		return string(data), true, nil
	}
	return "", false, nil
}
