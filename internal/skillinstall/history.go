package skillinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// safeHistoricalSkillTarget is only needed when a recorded physical alias can
// no longer be derived from a permitted agent configuration. Arbitrary ordinary
// directories never become deletion capabilities merely by appearing in JSON.
func safeHistoricalSkillTarget(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect historical skill target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	} // Replacement only unlinks it.
	if !info.IsDir() {
		return false, nil
	}
	for _, relative := range []string{"SKILL.md", "scripts/workflow_help.py", "scripts/run_help.py"} {
		resource, err := os.Lstat(filepath.Join(path, relative))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect historical skill resource: %w", err)
		}
		if !resource.Mode().IsRegular() {
			return false, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return false, fmt.Errorf("read historical skill identity: %w", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return false, nil
	}
	header, _, found := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---")
	if !found {
		return false, nil
	}
	var identity struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(header), &identity); err != nil {
		return false, nil
	} // Invalid frontmatter cannot prove identity.
	return identity.Name == "conduct", nil
}
