// Package engine 定义 AI 编程引擎的无头执行抽象，以及引擎注册表。
//
// 每种引擎（如 claude-code、antigravity）实现同一个 Engine 接口，
// 以子进程 / 无头 CLI 方式运行一个提示词并返回产物文本。workflow 节点通过
// engine 字段按名字选择引擎，由本包的注册表解析到具体实现。
package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrNotImplemented 是「引擎已登记但无头执行尚未落地」的约定返回值（承 AGENTS.md「不假装成功」）。
// 现有已注册引擎均已实装，暂无使用者；若将来先登记某引擎 stub，即用它显式占位而非空实现冒充可用。
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
	// Tokens 是引擎明确回报的 token 数；引擎不提供时为 nil。已知值 0 仍以非 nil 指针表示。
	Tokens *int
	// SessionID 是本次运行的引擎会话/线程 id，从各引擎自身 JSON 输出取
	//（claude-code / qoder 的 session_id、antigravity 的 conversation_id、codex 的 thread_id）；
	// conduct 记入该步 trace，供凭引擎自带工具回放本步。引擎不回报或回报空串时为 nil。
	SessionID *string
}

func tokenTotal(inputTokens, outputTokens *int) *int {
	if inputTokens == nil || outputTokens == nil {
		return nil
	}
	total := *inputTokens + *outputTokens
	return &total
}

func nonEmptyString(value *string) *string {
	if value == nil || *value == "" {
		return nil
	}
	return value
}

// Engine 抽象一个可被无头调用的 AI 编程引擎。
type Engine interface {
	// Descriptor 返回注册、校验和展示所需的静态元数据。
	Descriptor() EngineDescriptor
	// Run 以无头方式执行一个提示词并返回产物。
	Run(ctx context.Context, request RunRequest) (RunResult, error)
}

// EngineDescriptor 把引擎实现、配置能力和展示元数据聚拢为一份注册事实源。
type EngineDescriptor struct {
	Name                 string
	Capability           EngineCapability
	IconFilename         string
	SessionReplayCommand func(sessionID string) string
}

type registryEntry struct {
	implementation Engine
	descriptor     EngineDescriptor
}

// registry 保存已登记的引擎及其 descriptor，键为 descriptor.Name。
var registry = map[string]registryEntry{}

// Register 登记一个引擎；名字重复即 panic——那是编译期就该发现的编程错误。
func Register(engine Engine) {
	if engine == nil {
		panic("cannot register nil engine")
	}
	descriptor := cloneDescriptor(engine.Descriptor())
	validateDescriptor(descriptor)
	name := descriptor.Name
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("engine %q already registered", name))
	}
	registry[name] = registryEntry{implementation: engine, descriptor: descriptor}
}

// Lookup 按名字取引擎；未登记时返回带可用引擎清单的错误。
func Lookup(name string) (Engine, error) {
	entry, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown engine %q; available: %v", name, RegisteredNames())
	}
	return entry.implementation, nil
}

// Describe 返回指定引擎 descriptor 的深拷贝。
func Describe(name string) (EngineDescriptor, bool) {
	entry, ok := registry[name]
	if !ok {
		return EngineDescriptor{}, false
	}
	return cloneDescriptor(entry.descriptor), true
}

// Exists 报告某名字的引擎是否已登记（即可作为 workflow 定义里的合法 engine 值）。
func Exists(name string) bool {
	_, ok := registry[name]
	return ok
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

// RegisteredDescriptors 返回按 name 排序的 descriptor 深拷贝。
func RegisteredDescriptors() []EngineDescriptor {
	descriptors := make([]EngineDescriptor, 0, len(registry))
	for _, entry := range registry {
		descriptors = append(descriptors, cloneDescriptor(entry.descriptor))
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Name < descriptors[j].Name })
	return descriptors
}

func validateDescriptor(descriptor EngineDescriptor) {
	if descriptor.Name == "" {
		panic("engine descriptor name must not be empty")
	}
	if !descriptor.Capability.AllowsModel && len(descriptor.Capability.ModelSuggestions) != 0 {
		panic(fmt.Sprintf("engine %q disallows model but has model suggestions", descriptor.Name))
	}
	if descriptor.Capability.AllowsEffort != (len(descriptor.Capability.EffortValues) > 0) {
		panic(fmt.Sprintf("engine %q has inconsistent effort capability", descriptor.Name))
	}
	validateUniqueValues(descriptor.Name, "model suggestion", descriptor.Capability.ModelSuggestions)
	validateUniqueValues(descriptor.Name, "effort value", descriptor.Capability.EffortValues)
	if strings.ContainsAny(descriptor.IconFilename, `/\\`) {
		panic(fmt.Sprintf("engine %q icon filename must not contain a path", descriptor.Name))
	}
}

func validateUniqueValues(engineName, kind string, values []string) {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" {
			panic(fmt.Sprintf("engine %q has an empty %s", engineName, kind))
		}
		if seen[value] {
			panic(fmt.Sprintf("engine %q has duplicate %s %q", engineName, kind, value))
		}
		seen[value] = true
	}
}

func cloneDescriptor(descriptor EngineDescriptor) EngineDescriptor {
	// Descriptor 是注册后对外消费的稳定 schema：两个列表即使为空也保持非 nil，
	// 这样 API 序列化为 []，不会让调用方区分 nil/null 与空集合。
	descriptor.Capability.ModelSuggestions = append([]string{}, descriptor.Capability.ModelSuggestions...)
	descriptor.Capability.EffortValues = append([]string{}, descriptor.Capability.EffortValues...)
	return descriptor
}

var shellSafeValue = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ShellQuote quotes one shell argument for display and copy. It does not execute the result.
func ShellQuote(value string) string {
	if value != "" && shellSafeValue.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
