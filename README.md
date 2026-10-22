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

Update the CLI and all Skills installed with `conduct init`:

```bash
conduct update            # update the CLI and all installed Skills
conduct update --check    # check only
conduct update v0.2.0     # install a specific version
```

## Uninstall

conduct is a single binary. Remove it, and optionally its data:

```bash
rm "$(command -v conduct)"   # prefix with sudo if it lives in a system dir like /usr/local/bin
rm -rf ~/.conduct            # optional: workflows, run records, and Skill installation metadata
```

## Usage

```bash
conduct --help
```

## Agent Skill

Install the conduct Skill for your AI coding tools:

```bash
conduct init                 # interactive setup
```
