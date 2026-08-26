# conduct

A command-line tool that interprets workflow definitions (JSON) into deterministic steps and drives multiple AI coding engines (claude-code / antigravity / qoder / codex / kiro) through them, step by step.

## Install

macOS / Linux — install the latest release with one line:

```bash
curl -sSL https://raw.githubusercontent.com/qoggy/conduct/main/install.sh | sh
```

Or, with a Go toolchain, install from source:

```bash
go install github.com/qoggy/conduct/cmd/conduct@latest
```

## Update

Once installed, conduct updates itself in place:

```bash
conduct update            # update to the latest release
conduct update --check    # check for a newer version without installing
conduct update v0.2.0     # install a specific version (a version tag opts into pre-releases)
```

## Uninstall

conduct is a single binary. Remove it, and optionally its data:

```bash
rm "$(command -v conduct)"   # prefix with sudo if it lives in a system dir like /usr/local/bin
rm -rf ~/.conduct            # optional: workflows and run records
```

## Usage

```bash
conduct --help
```

## Agent Skill

The repository includes a conduct Skill at `.agents/skills/conduct`. Copy it into a Skill root supported by your coding agent.

For example, after cloning this repository:

```bash
# Choose a directory supported by your agent, for example a user-level
# ~/.agents/skills or ~/.claude/skills directory, or the matching project directory.
SKILL_ROOT="$HOME/.agents/skills"
mkdir -p "$SKILL_ROOT/conduct"
cp -R .agents/skills/conduct/. "$SKILL_ROOT/conduct/"
```
