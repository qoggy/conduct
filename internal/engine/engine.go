// Package engine 定义 AI 编程引擎的无头执行抽象，以及引擎注册表。
//
// 每种引擎（claude-code、codex、qoder、gemini）实现同一个 Engine 接口，
// 以子进程 / 无头 CLI 方式运行一个提示词并返回产物文本。workflow 节点通过
// engine 字段按名字选择引擎，由本包的注册表解析到具体实现。
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// ErrNotImplemented 表示引擎已登记但其无头执行尚未落地。
var ErrNotImplemented = errors.New("engine not yet implemented")

// RunRequest 是一次无头引擎执行的入参。
type RunRequest struct {
	// Prompt 是喂给引擎的完整提示词。
	Prompt string
	// Model 指定模型；为空则交由引擎自身的默认模型。
	Model string
	// WorkingDirectory 是引擎读写文件的工作目录。
	WorkingDirectory string
	// Effort 是引擎特定的推理强度；各引擎语义不同，未设时为空字符串。
	Effort string
}

// RunResult 是一次无头引擎执行的产物。
type RunResult struct {
	// Text 是本次运行的产物文本，作为 workflow 节点的输出。
	Text string
	// DurationMilliseconds 是本次运行的耗时。
	DurationMilliseconds int64
	// Tokens 是本次运行消耗的 token 数；引擎不提供时为 0。
	Tokens int
}

// Engine 抽象一个可被无头调用的 AI 编程引擎。
type Engine interface {
	// Name 返回引擎的稳定标识，即 workflow 定义里 engine 字段的取值（如 "claude-code"）。
	Name() string
	// Run 以无头方式执行一个提示词并返回产物。
	Run(ctx context.Context, request RunRequest) (RunResult, error)
}

// registry 保存已登记的引擎，键为 Engine.Name()。
var registry = map[string]Engine{}

// Register 登记一个引擎；名字重复即 panic——那是编译期就该发现的编程错误。
func Register(engine Engine) {
	name := engine.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("engine %q already registered", name))
	}
	registry[name] = engine
}

// Lookup 按名字取引擎；未登记时返回带可用引擎清单的错误。
func Lookup(name string) (Engine, error) {
	engine, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown engine %q; available: %v", name, RegisteredNames())
	}
	return engine, nil
}

// RegisteredNames 返回已登记引擎名字的有序列表。
func RegisteredNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
