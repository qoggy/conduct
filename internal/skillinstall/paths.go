package skillinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func physicalPath(path string) (string, error) {
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve path %s: %w", path, err)
	}
	if _, entryError := os.Lstat(path); entryError == nil {
		return "", fmt.Errorf("unsafe broken parent symbolic link: %s", path)
	} else if !os.IsNotExist(entryError) {
		return "", fmt.Errorf("inspect path %s: %w", path, entryError)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = physicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

// Resolve parent aliases, preserving a final symlink so replacement only unlinks it.
func physicalParent(path string) (string, error) {
	parent, err := physicalPath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
func contains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}
func overlaps(a, b string) bool { return contains(a, b) || contains(b, a) }
func rootFor(e Environment, scope Scope) (string, error) {
	path := e.Cwd
	if scope == Global {
		path = e.Home
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("installation root must be absolute: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve installation root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("installation root must be a directory: %s", resolved)
	}
	return resolved, nil
}
func metadataPath(e Environment) (string, error) {
	home, err := rootFor(e, Global)
	if err != nil {
		return "", err
	}
	return physicalParent(filepath.Join(home, ".conduct", "skill-installations.json"))
}
func nonempty(value string) bool { return value != "" && !strings.ContainsRune(value, 0) }
func supportedAgents(agents []string, e Environment) error {
	if len(agents) == 0 {
		return fmt.Errorf("at least one skill agent is required")
	}
	seen := map[string]bool{}
	for _, id := range agents {
		known := false
		for _, agent := range registry(e) {
			if id == agent.ID {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown skill agent: %s", id)
		}
		if seen[id] {
			return fmt.Errorf("duplicate skill agent: %s", id)
		}
		seen[id] = true
	}
	return nil
}
