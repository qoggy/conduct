// Package locale 负责解析 conduct 的界面语言以及读写全局语言设置。
package locale

import (
	"os"
	"strings"
)

// Language 是 conduct 支持的界面语言，也是 settings.json 与 run.json 使用的稳定值。
type Language string

const (
	English Language = "en"
	Chinese Language = "zh-CN"
)

var environmentVariables = [...]string{"LC_ALL", "LC_MESSAGES", "LANG"}

// Detect 按 LC_ALL > LC_MESSAGES > LANG 的优先级读取第一个非空值。
// 它只解析环境变量；需要应用 settings.json 优先级时使用 Resolve。
func Detect() Language {
	for _, name := range environmentVariables {
		if value := os.Getenv(name); value != "" {
			return Parse(value)
		}
	}
	return English
}

// Parse 把 locale / language 值归一为 conduct CLI help 支持的语言。
// 中文接受 zh 及常见的地域、文字和编码后缀；其余值（含 C / POSIX）均为英文。
func Parse(value string) Language {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if index := strings.IndexAny(normalized, ".@"); index >= 0 {
		normalized = normalized[:index]
	}
	primary, _, _ := strings.Cut(normalized, "_")
	primary, _, _ = strings.Cut(primary, "-")
	if primary == "zh" {
		return Chinese
	}
	return English
}

// Select 返回当前语言对应的文案。
func (language Language) Select(chinese, english string) string {
	if language == Chinese {
		return chinese
	}
	return english
}

// Valid 报告 language 是否为持久化 schema 允许的值。
func (language Language) Valid() bool {
	return language == English || language == Chinese
}
