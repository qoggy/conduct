package ui

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/qoggy/conduct/internal/apperror"
)

func TestUIDictionariesHaveMatchingKeys(t *testing.T) {
	source := readUIAsset(t, "assets/js/i18n.js")
	chinese := dictionaryKeys(t, source, "zhCN")
	english := dictionaryKeys(t, source, "en")
	if !slices.Equal(chinese, english) {
		t.Fatalf("UI dictionary keys differ:\nzh-CN=%v\nen=%v", chinese, english)
	}
}

func TestUIRendersEveryApplicationErrorCode(t *testing.T) {
	source := readUIAsset(t, "assets/js/i18n.js")
	for _, code := range apperror.AllCodes() {
		if !strings.Contains(source, "    "+string(code)+":") {
			t.Errorf("UI error dictionary is missing code %q", code)
		}
	}
}

func TestUILanguageUsesServerSettingsOnly(t *testing.T) {
	application := readUIAsset(t, "assets/js/app.js")
	html := readUIAsset(t, "assets/index.html")
	settingsPage := readUIAsset(t, "assets/js/pages/settings.js")
	allJavaScript := application + readUIAsset(t, "assets/js/i18n.js") + readUIAsset(t, "assets/js/theme.js")
	for _, forbidden := range []string{"navigator.language", "localStorage"} {
		if strings.Contains(allJavaScript, forbidden) {
			t.Errorf("browser language implementation must not use %s", forbidden)
		}
	}
	for _, required := range []string{"api.settings()", "api.updateSettings", "document.documentElement.lang", "rerender()"} {
		if !strings.Contains(application, required) {
			t.Errorf("UI language bootstrap is missing %q", required)
		}
	}
	if strings.Contains(html, "<select") {
		t.Error("SPA 外壳不得继续使用原生语言下拉")
	}
	for _, required := range []string{`listSelect(`, `value: "zh-CN"`, `value: "en"`, `value: "light"`, `value: "dark"`} {
		if !strings.Contains(settingsPage, required) {
			t.Errorf("settings page custom selectors are missing %s", required)
		}
	}
}

func TestUILaunchWarningsUseLocalizedNotes(t *testing.T) {
	i18nSource := readUIAsset(t, "assets/js/i18n.js")
	dialogs := readUIAsset(t, "assets/js/dialogs.js")
	runDetail := readUIAsset(t, "assets/js/pages/run-detail.js")
	for _, code := range []string{"run_launch_unconfirmed", "resume_launch_unconfirmed"} {
		if strings.Count(i18nSource, code) < 2 {
			t.Errorf("UI dictionaries must both render launch note %q", code)
		}
	}
	if !strings.Contains(dialogs, "res.note") || !strings.Contains(dialogs, "toast(i18n[res.note])") {
		t.Error("workflow launch response note is not shown as a localized toast")
	}
	if !strings.Contains(runDetail, "response.note") || !strings.Contains(runDetail, "toast(i18n[response.note])") {
		t.Error("resume response note is not shown as a localized toast")
	}
}

func dictionaryKeys(t *testing.T, source, name string) []string {
	t.Helper()
	marker := "const " + name + " = {"
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("dictionary %s not found", name)
	}
	keyPattern := regexp.MustCompile(`^  ([A-Za-z][A-Za-z0-9]*):`)
	keys := []string{}
	scanner := bufio.NewScanner(strings.NewReader(source[start+len(marker):]))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "};" {
			break
		}
		if match := keyPattern.FindStringSubmatch(line); match != nil {
			keys = append(keys, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	return keys
}

func readUIAsset(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
