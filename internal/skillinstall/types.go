// Package skillinstall installs the CLI's embedded skill and tracks explicit installations.
package skillinstall

import (
	"os"
	"path/filepath"
)

type Scope string

const (
	Project Scope = "project"
	Global  Scope = "global"
)

type Method string

const (
	Copy    Method = "copy"
	Symlink Method = "symlink"
)

type Environment struct {
	Cwd     string
	Home    string
	Version string
	Env     map[string]string
}

func NewEnvironment(version string) (Environment, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Environment{}, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return Environment{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Environment{}, err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return Environment{}, err
	}
	environment := map[string]string{}
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "XDG_CONFIG_HOME"} {
		environment[key] = os.Getenv(key)
	}
	return Environment{Cwd: cwd, Home: home, Version: version, Env: environment}, nil
}

type Agent struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Universal   bool   `json:"universal"`
	Detected    bool   `json:"detected"`
}
type Request struct {
	Agents []string
	Scope  Scope
	Method Method
}
type Target struct {
	Path   string   `json:"path"`
	Kind   string   `json:"kind"`
	Target string   `json:"target,omitempty"`
	Agents []string `json:"agents,omitempty"`
	Exists bool     `json:"exists"`
}
type Plan struct {
	Scope          Scope    `json:"scope"`
	ProjectPath    string   `json:"projectPath,omitempty"`
	Method         Method   `json:"method"`
	Agents         []string `json:"agents"`
	ConductVersion string   `json:"conductVersion"`
	RecordPath     string   `json:"recordPath"`
	Targets        []Target `json:"targets"`
}
type Result = Plan
type StoredTarget struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}
type Installation struct {
	Scope          Scope          `json:"scope"`
	ProjectPath    string         `json:"projectPath,omitempty"`
	ConductVersion string         `json:"conductVersion"`
	Agents         []string       `json:"agents"`
	Method         Method         `json:"method"`
	Targets        []StoredTarget `json:"targets"`
}
type Records struct {
	SchemaVersion int            `json:"schemaVersion"`
	Installations []Installation `json:"installations"`
}
type SyncItem struct {
	Scope          Scope    `json:"scope"`
	ProjectPath    string   `json:"projectPath,omitempty"`
	Status         string   `json:"status"`
	Error          string   `json:"error,omitempty"`
	Targets        []Target `json:"targets,omitempty"`
	ConductVersion string   `json:"conductVersion,omitempty"`
}
type SyncReport struct {
	Items   []SyncItem `json:"items"`
	Synced  int        `json:"synced"`
	Skipped int        `json:"skipped"`
	Failed  int        `json:"failed"`
	Matched int        `json:"matched"`
}
