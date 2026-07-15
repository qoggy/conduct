package ui

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestThemeAssetsAreEmbeddedAndInitializedBeforeStyles(t *testing.T) {
	server := newTestServer(t)

	index := do(t, server, http.MethodGet, "/", "", nil)
	if index.Code != http.StatusOK {
		t.Fatalf("GET / 期望 200，得到 %d", index.Code)
	}
	indexBody := index.Body.String()
	themeScriptPosition := strings.Index(indexBody, `src="./js/theme.js"`)
	stylesheetPosition := strings.Index(indexBody, `href="./style.css"`)
	if themeScriptPosition < 0 || stylesheetPosition < 0 {
		t.Fatalf("SPA 外壳应同时引用主题脚本与样式表")
	}
	if themeScriptPosition > stylesheetPosition {
		t.Fatalf("主题脚本必须先于样式表执行，避免刷新时主题闪烁")
	}
	if !strings.Contains(indexBody, `id="settings-link"`) {
		t.Fatalf("SPA 外壳应包含设置入口")
	}
	if !strings.Contains(indexBody, `data-theme-setting=""`) {
		t.Fatalf("未显式设置主题时首页应注入跟随系统的空偏好")
	}

	themeScript := do(t, server, http.MethodGet, "/js/theme.js", "", nil)
	if themeScript.Code != http.StatusOK {
		t.Fatalf("GET /js/theme.js 期望 200，得到 %d", themeScript.Code)
	}
	themeScriptBody := themeScript.Body.String()
	for _, requiredSnippet := range []string{
		`matchMedia("(prefers-color-scheme: dark)")`,
		`root.dataset.themeSetting`,
		`function setPreference(value)`,
		`conductTheme = Object.freeze`,
	} {
		if !strings.Contains(themeScriptBody, requiredSnippet) {
			t.Errorf("主题脚本缺少关键状态机接线 %q", requiredSnippet)
		}
	}
	if strings.Contains(themeScriptBody, "localStorage") {
		t.Fatal("主题不得继续以 localStorage 为持久化事实源")
	}

	stylesheet := do(t, server, http.MethodGet, "/style.css", "", nil)
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("GET /style.css 期望 200，得到 %d", stylesheet.Code)
	}
	if !strings.Contains(stylesheet.Body.String(), `:root[data-theme="dark"]`) {
		t.Fatalf("样式表应包含 dark 主题令牌覆盖")
	}
}

func TestDarkThemeSmallTextContrast(t *testing.T) {
	server := newTestServer(t)
	stylesheet := do(t, server, http.MethodGet, "/style.css", "", nil)
	if stylesheet.Code != http.StatusOK {
		t.Fatalf("GET /style.css 期望 200，得到 %d", stylesheet.Code)
	}

	darkBlockPattern := regexp.MustCompile(`(?s):root\[data-theme="dark"\]\s*\{(.*?)\n\}`)
	darkBlockMatch := darkBlockPattern.FindStringSubmatch(stylesheet.Body.String())
	if len(darkBlockMatch) != 2 {
		t.Fatal("样式表应包含可解析的 dark 主题令牌块")
	}

	for _, check := range []struct {
		name       string
		foreground string
		background string
	}{
		{name: "编辑器栏弱文字", foreground: "--editor-bar-muted", background: "--editor-bar"},
		{name: "普通表面 faint 文字", foreground: "--faint", background: "--surface"},
		{name: "抬升表面 muted 文字", foreground: "--muted", background: "--surface-raised"},
		{name: "主按钮文字", foreground: "--on-primary", background: "--primary"},
	} {
		foreground := cssHexToken(t, darkBlockMatch[1], check.foreground)
		background := cssHexToken(t, darkBlockMatch[1], check.background)
		if ratio := contrastRatio(foreground, background); ratio < 4.5 {
			t.Errorf("dark %s对比度 %.3f:1，期望至少 4.5:1", check.name, ratio)
		}
	}
}

func cssHexToken(t *testing.T, css string, token string) string {
	t.Helper()
	pattern := regexp.MustCompile(regexp.QuoteMeta(token) + `:\s*(#[0-9a-fA-F]{6});`)
	match := pattern.FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatalf("dark 主题缺少十六进制颜色令牌 %s", token)
	}
	return match[1]
}

func contrastRatio(first string, second string) float64 {
	firstLuminance := relativeLuminance(first)
	secondLuminance := relativeLuminance(second)
	if firstLuminance < secondLuminance {
		firstLuminance, secondLuminance = secondLuminance, firstLuminance
	}
	return (firstLuminance + 0.05) / (secondLuminance + 0.05)
}

func relativeLuminance(color string) float64 {
	channels := make([]float64, 3)
	for index := range channels {
		value, err := strconv.ParseUint(color[1+index*2:3+index*2], 16, 8)
		if err != nil {
			panic(fmt.Sprintf("解析测试颜色 %q 失败: %v", color, err))
		}
		channel := float64(value) / 255
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}
