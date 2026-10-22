package skillinstall

// Agent mappings and detection are adapted from Vercel Skills (MIT), through
// Paneon. See UPSTREAM-LICENSE.md for source attribution and the full license.
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type agentDefinition struct {
	Agent
	project   string
	global    string
	detection []string
}

func registry(e Environment) []agentDefinition {
	resolve := func(value, fallback string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return fallback
		}
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return filepath.Join(e.Cwd, value)
	}
	codex := resolve(e.Env["CODEX_HOME"], filepath.Join(e.Home, ".codex"))
	claude := resolve(e.Env["CLAUDE_CONFIG_DIR"], filepath.Join(e.Home, ".claude"))
	claudeSkills := filepath.Join(claude, "skills")
	config := strings.TrimSpace(e.Env["XDG_CONFIG_HOME"])
	if !filepath.IsAbs(config) {
		config = filepath.Join(e.Home, ".config")
	}
	universal := ".agents/skills"
	makeAgent := func(id, name, project, global string, detection ...string) agentDefinition {
		return agentDefinition{Agent: Agent{ID: id, DisplayName: name, Universal: project == universal}, project: project, global: global, detection: detection}
	}
	return []agentDefinition{
		makeAgent("codex", "Codex", universal, filepath.Join(e.Home, universal), codex, "/etc/codex"),
		makeAgent("cursor", "Cursor", universal, filepath.Join(e.Home, universal), filepath.Join(e.Home, ".cursor")),
		makeAgent("claude-code", "Claude Code", ".claude/skills", claudeSkills, claude),
		makeAgent("opencode", "OpenCode", universal, filepath.Join(e.Home, universal), filepath.Join(config, "opencode")),
		makeAgent("gemini-cli", "Gemini CLI", universal, filepath.Join(e.Home, universal), filepath.Join(e.Home, ".gemini")),
		makeAgent("github-copilot", "GitHub Copilot", universal, filepath.Join(e.Home, universal), filepath.Join(e.Home, ".copilot")),
		makeAgent("cline", "Cline", universal, filepath.Join(e.Home, universal), filepath.Join(e.Home, ".cline")),
		makeAgent("windsurf", "Windsurf", ".windsurf/skills", filepath.Join(e.Home, ".codeium/windsurf/skills"), filepath.Join(e.Home, ".codeium/windsurf")),
		makeAgent("qwen-code", "Qwen Code", ".qwen/skills", filepath.Join(e.Home, ".qwen/skills"), filepath.Join(e.Home, ".qwen")),
		makeAgent("kiro-cli", "Kiro CLI", ".kiro/skills", filepath.Join(e.Home, ".kiro/skills"), filepath.Join(e.Home, ".kiro")),
		makeAgent("roo", "Roo Code", ".roo/skills", filepath.Join(e.Home, ".roo/skills"), filepath.Join(e.Home, ".roo")),
	}
}
func ListAgents(e Environment) ([]Agent, error) {
	result := []Agent{}
	for _, definition := range registry(e) {
		agent := definition.Agent
		for _, path := range definition.detection {
			_, err := os.Stat(path)
			if err == nil {
				agent.Detected = true
			} else if !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) {
				return nil, fmt.Errorf("inspect agent directory %s: %w", path, err)
			}
		}
		result = append(result, agent)
	}
	return result, nil
}

// AgentIDs lists the registry's supported IDs without inspecting the filesystem.
func AgentIDs() []string {
	ids := []string{}
	for _, agent := range registry(Environment{Env: map[string]string{}}) {
		ids = append(ids, agent.ID)
	}
	return ids
}
