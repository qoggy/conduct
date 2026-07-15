package locale

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const settingsFileName = "settings.json"

var replaceFile = os.Rename

// Theme 是 conduct UI 支持的显式主题。settings.json 不含 theme 时由浏览器跟随系统。
type Theme string

const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

func (theme Theme) Valid() bool {
	return theme == ThemeLight || theme == ThemeDark
}

// Settings 是 settings API 对外视图。Language=nil 表示跟随环境，Theme=nil 表示跟随系统。
type Settings struct {
	Language         *Language `json:"language"`
	ResolvedLanguage Language  `json:"resolvedLanguage"`
	Theme            *Theme    `json:"theme"`
}

// SettingsUpdate 表示一次严格的部分更新。Present 区分“未修改该字段”与“删除该字段（值为 nil）”。
type SettingsUpdate struct {
	LanguagePresent bool
	Language        *Language
	ThemePresent    bool
	Theme           *Theme
}

// DefaultRoot 返回生产设置目录 ~/.conduct。
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".conduct"), nil
}

// Resolve 按 settings.language > LC_ALL > LC_MESSAGES > LANG > en 解析一次进程语言。
func Resolve() (Language, error) {
	root, err := DefaultRoot()
	if err != nil {
		return English, err
	}
	settings, err := Read(root)
	if err != nil {
		return English, err
	}
	return settings.ResolvedLanguage, nil
}

// Read 严格读取 root/settings.json，并返回显式值与统一解析结果。文件缺失视为跟随环境。
func Read(root string) (Settings, error) {
	raw, exists, err := readObject(filepath.Join(root, settingsFileName))
	if err != nil {
		return Settings{}, err
	}
	explicit, err := languageFromObject(raw)
	if err != nil {
		return Settings{}, err
	}
	theme, err := themeFromObject(raw)
	if err != nil {
		return Settings{}, err
	}
	resolved := Detect()
	if explicit != nil {
		resolved = *explicit
	}
	if !exists {
		explicit = nil
		theme = nil
	}
	return Settings{Language: explicit, ResolvedLanguage: resolved, Theme: theme}, nil
}

// UpdateLanguage 只修改 root/settings.json 的 language 属性。nil 表示跟随环境。
// 未知属性原样保留，最终通过同目录临时文件 + rename 原子替换。
func UpdateLanguage(root string, language *Language) (Settings, error) {
	return UpdateSettings(root, SettingsUpdate{LanguagePresent: true, Language: language})
}

// UpdateSettings 严格部分更新 language / theme；nil 删除对应属性，未出现的字段保持不变。
func UpdateSettings(root string, update SettingsUpdate) (Settings, error) {
	if !update.LanguagePresent && !update.ThemePresent {
		return Settings{}, fmt.Errorf("settings update must contain language or theme")
	}
	if update.LanguagePresent && update.Language != nil && !update.Language.Valid() {
		return Settings{}, fmt.Errorf("unsupported language %q", *update.Language)
	}
	if update.ThemePresent && update.Theme != nil && !update.Theme.Valid() {
		return Settings{}, fmt.Errorf("unsupported theme %q", *update.Theme)
	}
	path := filepath.Join(root, settingsFileName)
	raw, exists, err := readObject(path)
	if err != nil {
		return Settings{}, err
	}
	if _, err := languageFromObject(raw); err != nil {
		return Settings{}, err
	}
	if _, err := themeFromObject(raw); err != nil {
		return Settings{}, err
	}
	deletingFromMissingFile := !exists && (!update.LanguagePresent || update.Language == nil) && (!update.ThemePresent || update.Theme == nil)
	if deletingFromMissingFile {
		return Settings{Language: nil, ResolvedLanguage: Detect(), Theme: nil}, nil
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	if update.LanguagePresent {
		if update.Language == nil {
			delete(raw, "language")
		} else {
			encoded, marshalErr := json.Marshal(*update.Language)
			if marshalErr != nil {
				return Settings{}, fmt.Errorf("failed to encode language setting: %w", marshalErr)
			}
			raw["language"] = encoded
		}
	}
	if update.ThemePresent {
		if update.Theme == nil {
			delete(raw, "theme")
		} else {
			encoded, marshalErr := json.Marshal(*update.Theme)
			if marshalErr != nil {
				return Settings{}, fmt.Errorf("failed to encode theme setting: %w", marshalErr)
			}
			raw["theme"] = encoded
		}
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return Settings{}, fmt.Errorf("failed to encode %s: %w", displayPath(path), err)
	}
	data = append(data, '\n')
	if err := atomicWrite(path, data); err != nil {
		return Settings{}, err
	}
	return Read(root)
}

func readObject(path string) (map[string]json.RawMessage, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read %s: %w", displayPath(path), err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var encoded json.RawMessage
	if err := decoder.Decode(&encoded); err != nil {
		return nil, true, fmt.Errorf("failed to parse %s: %w", displayPath(path), err)
	}
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, true, fmt.Errorf("failed to parse %s: top-level value must be an object", displayPath(path))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return nil, true, fmt.Errorf("failed to parse %s: %w", displayPath(path), err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, true, fmt.Errorf("failed to parse %s: %w", displayPath(path), err)
	}
	return raw, true, nil
}

func languageFromObject(raw map[string]json.RawMessage) (*Language, error) {
	encoded, ok := raw["language"]
	if !ok {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, fmt.Errorf("failed to parse language setting: language must be a string")
	}
	language := Language(value)
	if !language.Valid() {
		return nil, fmt.Errorf("unsupported language %q", value)
	}
	return &language, nil
}

func themeFromObject(raw map[string]json.RawMessage) (*Theme, error) {
	encoded, ok := raw["theme"]
	if !ok {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, fmt.Errorf("failed to parse theme setting: theme must be a string")
	}
	theme := Theme(value)
	if !theme.Valid() {
		return nil, fmt.Errorf("unsupported theme %q", value)
	}
	return &theme, nil
}

func atomicWrite(path string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary settings file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
			err = fmt.Errorf("failed to remove temporary settings file: %w", removeErr)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			return fmt.Errorf("failed to write settings file: %w (and failed to close temporary file: %v)", err, closeErr)
		}
		return fmt.Errorf("failed to write settings file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		if closeErr := temporary.Close(); closeErr != nil {
			return fmt.Errorf("failed to set settings file permissions: %w (and failed to close temporary file: %v)", err, closeErr)
		}
		return fmt.Errorf("failed to set settings file permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to close temporary settings file: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("failed to replace settings file: %w", err)
	}
	return nil
}

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if relative, relErr := filepath.Rel(home, path); relErr == nil && relative != "." && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return filepath.Join("~", relative)
		}
	}
	return path
}
