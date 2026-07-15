package cli

import (
	"testing"

	"github.com/qoggy/conduct/internal/locale"
	"github.com/qoggy/conduct/internal/workflow"
)

// —— --prompt 补换行与 set-prompt 剥换行成对：round-trip 字节稳定 ——

func TestNodeShowPromptRoundTripByteStable(t *testing.T) {
	samples := []string{
		"",                 // 空
		"单行无尾换行",           // 无尾换行
		"含 {{变量}} 与\n内部换行", // 含内部换行、模板变量
		"末尾已有换行\n",         // 尾部本就有一个换行
		"多行\n\n带空行\n",      // 多个换行
	}
	for _, p := range samples {
		rendered := appendOneTrailingNewline(p)
		if got := stripOneTrailingNewline([]byte(rendered)); got != p {
			t.Fatalf("round-trip 应字节稳定：原文 %q，补换行后剥回得到 %q", p, got)
		}
	}
}

// —— modelDisplay：无 config / 空 model 回落「(引擎默认)」，否则原样 ——

func TestModelDisplay(t *testing.T) {
	useTestLanguage(t, locale.Chinese)
	if got := modelDisplay(nil); got != "(引擎默认)" {
		t.Fatalf("nil config 应回落 (引擎默认)，得到 %q", got)
	}
	if got := modelDisplay(&workflow.EngineConfig{}); got != "(引擎默认)" {
		t.Fatalf("空 model 应回落 (引擎默认)，得到 %q", got)
	}
	if got := modelDisplay(&workflow.EngineConfig{Model: "claude-sonnet-5"}); got != "claude-sonnet-5" {
		t.Fatalf("有 model 应原样返回，得到 %q", got)
	}
}
