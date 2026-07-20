package engine

import (
	"context"
	"reflect"
	"testing"
)

type descriptorTestEngine struct{ descriptor EngineDescriptor }

func (engine descriptorTestEngine) Descriptor() EngineDescriptor { return engine.descriptor }
func (descriptorTestEngine) Run(context.Context, RunRequest) (RunResult, error) {
	return RunResult{}, nil
}

func withIsolatedRegistry(t *testing.T) {
	t.Helper()
	original := registry
	registry = map[string]registryEntry{}
	t.Cleanup(func() { registry = original })
}

func TestDescriptorRegistrySortsAndDeepCopies(t *testing.T) {
	withIsolatedRegistry(t)
	first := EngineDescriptor{
		Name: "zeta",
		Capability: EngineCapability{
			AllowsModel:      true,
			ModelSuggestions: []string{"m1"},
			AllowsEffort:     true,
			EffortValues:     []string{"low"},
		},
		IconFilename: "zeta.png",
	}
	Register(descriptorTestEngine{descriptor: first})
	Register(descriptorTestEngine{descriptor: EngineDescriptor{Name: "alpha"}})

	first.Capability.ModelSuggestions[0] = "mutated"
	first.Capability.EffortValues[0] = "mutated"
	descriptors := RegisteredDescriptors()
	if got := []string{descriptors[0].Name, descriptors[1].Name}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("descriptor 排序错误: %v", got)
	}
	if descriptors[0].Capability.ModelSuggestions == nil || descriptors[0].Capability.EffortValues == nil {
		t.Fatalf("空 capability 列表应规范化为非 nil 空切片: %+v", descriptors[0].Capability)
	}
	descriptors[1].Capability.ModelSuggestions[0] = "caller-mutated"
	descriptors[1].Capability.EffortValues[0] = "caller-mutated"
	stored, ok := Describe("zeta")
	if !ok || stored.Capability.ModelSuggestions[0] != "m1" || stored.Capability.EffortValues[0] != "low" {
		t.Fatalf("注册表未深拷贝 descriptor: %+v", stored)
	}
	if names := RegisteredNames(); !reflect.DeepEqual(names, []string{"alpha", "zeta"}) {
		t.Fatalf("name 排序错误: %v", names)
	}
	if _, err := Lookup("missing"); err == nil {
		t.Fatal("未知引擎 Lookup 应失败")
	}
}

func TestRegisterRejectsInvalidDescriptors(t *testing.T) {
	tests := []struct {
		name       string
		descriptor EngineDescriptor
	}{
		{name: "empty name", descriptor: EngineDescriptor{}},
		{name: "model suggestions while model disabled", descriptor: EngineDescriptor{Name: "x", Capability: EngineCapability{ModelSuggestions: []string{"m"}}}},
		{name: "effort allowed without values", descriptor: EngineDescriptor{Name: "x", Capability: EngineCapability{AllowsEffort: true}}},
		{name: "effort values while disabled", descriptor: EngineDescriptor{Name: "x", Capability: EngineCapability{EffortValues: []string{"low"}}}},
		{name: "duplicate model suggestion", descriptor: EngineDescriptor{Name: "x", Capability: EngineCapability{AllowsModel: true, ModelSuggestions: []string{"m", "m"}}}},
		{name: "duplicate effort value", descriptor: EngineDescriptor{Name: "x", Capability: EngineCapability{AllowsEffort: true, EffortValues: []string{"low", "low"}}}},
		{name: "icon path", descriptor: EngineDescriptor{Name: "x", IconFilename: "icons/x.png"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withIsolatedRegistry(t)
			defer func() {
				if recover() == nil {
					t.Fatal("无效 descriptor 应 panic")
				}
			}()
			Register(descriptorTestEngine{descriptor: test.descriptor})
		})
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	withIsolatedRegistry(t)
	Register(descriptorTestEngine{descriptor: EngineDescriptor{Name: "same"}})
	defer func() {
		if recover() == nil {
			t.Fatal("重复名称应 panic")
		}
	}()
	Register(descriptorTestEngine{descriptor: EngineDescriptor{Name: "same"}})
}

func TestShellQuote(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{input: "session-42", want: "session-42"},
		{input: "two words", want: "'two words'"},
		{input: "a'b", want: `'a'"'"'b'`},
		{input: "", want: "''"},
	} {
		if got := ShellQuote(test.input); got != test.want {
			t.Errorf("ShellQuote(%q)=%q, want %q", test.input, got, test.want)
		}
	}
}

func TestBuiltInModelSuggestions(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{
			name: "codex",
			want: []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.3-codex-spark"},
		},
		{
			name: "kiro",
			want: []string{"auto", "claude-sonnet-5", "claude-opus-4.8", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor, ok := Describe(test.name)
			if !ok {
				t.Fatalf("引擎 %q 未注册", test.name)
			}
			if !reflect.DeepEqual(descriptor.Capability.ModelSuggestions, test.want) {
				t.Fatalf("%s model suggestions=%v, want %v", test.name, descriptor.Capability.ModelSuggestions, test.want)
			}
		})
	}
}

func TestBuiltInCapabilitiesUseNonNilSlices(t *testing.T) {
	for _, descriptor := range RegisteredDescriptors() {
		if descriptor.Capability.ModelSuggestions == nil {
			t.Errorf("%s ModelSuggestions 不应为 nil", descriptor.Name)
		}
		if descriptor.Capability.EffortValues == nil {
			t.Errorf("%s EffortValues 不应为 nil", descriptor.Name)
		}
	}
}
