package locale

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSettings(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	tests := []struct {
		name         string
		contents     *string
		asDirectory  bool
		wantExplicit *Language
		wantResolved Language
		wantTheme    *Theme
		wantError    string
	}{
		{name: "missing", wantResolved: English},
		{name: "language missing", contents: stringPointer(`{"theme":"dark"}`), wantResolved: English, wantTheme: themePointer(ThemeDark)},
		{name: "light theme", contents: stringPointer(`{"theme":"light"}`), wantResolved: English, wantTheme: themePointer(ThemeLight)},
		{name: "english", contents: stringPointer(`{"language":"en"}`), wantExplicit: languagePointer(English), wantResolved: English},
		{name: "chinese", contents: stringPointer(`{"language":"zh-CN"}`), wantExplicit: languagePointer(Chinese), wantResolved: Chinese},
		{name: "unreadable", asDirectory: true, wantError: "failed to read"},
		{name: "damaged json", contents: stringPointer(`{"language":`), wantError: "failed to parse"},
		{name: "array", contents: stringPointer(`[]`), wantError: "top-level value must be an object"},
		{name: "null", contents: stringPointer(`null`), wantError: "top-level value must be an object"},
		{name: "non string", contents: stringPointer(`{"language":1}`), wantError: "language must be a string"},
		{name: "unsupported", contents: stringPointer(`{"language":"fr"}`), wantError: `unsupported language "fr"`},
		{name: "theme non string", contents: stringPointer(`{"theme":1}`), wantError: "theme must be a string"},
		{name: "unsupported theme", contents: stringPointer(`{"theme":"sepia"}`), wantError: `unsupported theme "sepia"`},
		{name: "trailing", contents: stringPointer(`{} {}`), wantError: "unexpected trailing JSON value"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, settingsFileName)
			if test.asDirectory {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if test.contents != nil {
				if err := os.WriteFile(path, []byte(*test.contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Read(root)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Read() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !sameLanguagePointer(got.Language, test.wantExplicit) || got.ResolvedLanguage != test.wantResolved || !sameThemePointer(got.Theme, test.wantTheme) {
				t.Fatalf("Read() = %+v, want language=%v resolved=%s theme=%v", got, test.wantExplicit, test.wantResolved, test.wantTheme)
			}
		})
	}
}

func TestReadSettingsPriority(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		all      string
		messages string
		lang     string
		want     Language
	}{
		{name: "setting", setting: `{"language":"en"}`, all: "zh_CN", want: English},
		{name: "LC_ALL", all: "zh_CN", messages: "en_US", lang: "en_US", want: Chinese},
		{name: "LC_MESSAGES", messages: "zh_Hans", lang: "en_US", want: Chinese},
		{name: "LANG", lang: "zh_TW.UTF-8@variant", want: Chinese},
		{name: "unrecognized high priority", all: "fr_FR", messages: "zh_CN", want: English},
		{name: "fallback", want: English},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LC_ALL", test.all)
			t.Setenv("LC_MESSAGES", test.messages)
			t.Setenv("LANG", test.lang)
			root := t.TempDir()
			if test.setting != "" {
				if err := os.WriteFile(filepath.Join(root, settingsFileName), []byte(test.setting), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Read(root)
			if err != nil {
				t.Fatal(err)
			}
			if got.ResolvedLanguage != test.want {
				t.Fatalf("resolved = %s, want %s", got.ResolvedLanguage, test.want)
			}
		})
	}
}

func TestUpdateLanguagePreservesUnknownProperties(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, settingsFileName)
	if err := os.WriteFile(path, []byte("{\n  \"theme\": \"dark\",\n  \"nested\": {\"n\": 1}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := UpdateLanguage(root, languagePointer(Chinese))
	if err != nil {
		t.Fatal(err)
	}
	if settings.ResolvedLanguage != Chinese || settings.Language == nil || *settings.Language != Chinese {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"language": "zh-CN"`, `"theme": "dark"`, `"nested"`} {
		if !strings.Contains(text, want) {
			t.Errorf("updated file missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(filepath.Base(path), ".tmp") {
		t.Fatal("final file must not be a temporary file")
	}

	t.Setenv("LC_ALL", "en_US.UTF-8")
	settings, err = UpdateLanguage(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Language != nil || settings.ResolvedLanguage != English {
		t.Fatalf("follow-environment settings = %+v", settings)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"language"`) || !strings.Contains(string(data), `"theme": "dark"`) {
		t.Fatalf("language was not removed independently:\n%s", data)
	}
}

func TestUpdateSettingsPartiallyUpdatesLanguageAndTheme(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, settingsFileName)
	if err := os.WriteFile(path, []byte(`{"language":"en","theme":"light","nested":{"n":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := UpdateSettings(root, SettingsUpdate{ThemePresent: true, Theme: themePointer(ThemeDark)})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme == nil || *settings.Theme != ThemeDark || settings.Language == nil || *settings.Language != English {
		t.Fatalf("theme update changed another setting: %+v", settings)
	}
	settings, err = UpdateSettings(root, SettingsUpdate{LanguagePresent: true, Language: languagePointer(Chinese), ThemePresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Theme != nil || settings.Language == nil || *settings.Language != Chinese {
		t.Fatalf("combined update = %+v", settings)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"theme"`) || !strings.Contains(string(data), `"nested"`) {
		t.Fatalf("theme deletion did not preserve unknown properties:\n%s", data)
	}
}

func TestUpdateSettingsRejectsExistingInvalidSupportedValue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, settingsFileName)
	if err := os.WriteFile(path, []byte(`{"theme":"sepia"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateLanguage(root, languagePointer(Chinese)); err == nil || !strings.Contains(err.Error(), `unsupported theme "sepia"`) {
		t.Fatalf("UpdateLanguage() error = %v", err)
	}
}

func TestUpdateLanguageMissingFollowEnvironmentDoesNotCreateFile(t *testing.T) {
	root := t.TempDir()
	if _, err := UpdateLanguage(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, settingsFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings file should remain absent, stat error = %v", err)
	}
}

func TestUpdateLanguageReplaceFailureLeavesOriginalUnchanged(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, settingsFileName)
	original := []byte(`{"language":"en","keep":true}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	previous := replaceFile
	replaceFile = func(_, _ string) error { return errors.New("injected rename failure") }
	t.Cleanup(func() { replaceFile = previous })
	if _, err := UpdateLanguage(root, languagePointer(Chinese)); err == nil || !strings.Contains(err.Error(), "failed to replace settings file") {
		t.Fatalf("UpdateLanguage() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("original changed after failed replace: %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".settings-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files leaked: %v", matches)
	}
}

func stringPointer(value string) *string       { return &value }
func languagePointer(value Language) *Language { return &value }
func themePointer(value Theme) *Theme          { return &value }

func sameLanguagePointer(left, right *Language) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func sameThemePointer(left, right *Theme) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
