package skillinstall

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Bundle is embedded directly by Go; installation never requires a checkout.
//
//go:embed assets/conduct
var bundle embed.FS

func copyBundle(destination string) error {
	return fs.WalkDir(bundle, "assets/conduct", func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return fmt.Errorf("read embedded skill: %w", walkError)
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
			return fs.SkipDir
		}
		if !entry.IsDir() && (strings.HasSuffix(path, ".pyc") || strings.HasSuffix(path, ".pyo")) {
			return nil
		}
		relative := strings.TrimPrefix(path, "assets/conduct/")
		if path == "assets/conduct" {
			relative = ""
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create skill directory: %w", err)
			}
			return nil
		}
		data, err := bundle.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded skill: %w", err)
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(path, ".py") {
			mode = 0755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write skill resource: %w", err)
		}
		return nil
	})
}

// sourceDirectory is only a protection for source checkouts represented in build
// debug information. Embedded resources remain the sole installation source;
// moved binaries and builds with trimmed paths do not depend on this directory.
func sourceDirectory() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(filename) {
		return "", nil
	}
	source := filepath.Join(filepath.Dir(filename), "assets", "conduct")
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect skill source protection: %w", err)
	}
	if !info.IsDir() {
		return "", nil
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return "", fmt.Errorf("resolve skill source protection: %w", err)
	}
	return source, nil
}
