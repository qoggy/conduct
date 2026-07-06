# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `install.sh` now resolves the latest release tag via the `releases/latest` redirect instead of the GitHub API, avoiding unauthenticated rate-limit (403) failures during `curl | sh`.

## [0.0.1] - 2026-07-06

Initial public release.

### Added

- `conduct` command-line tool that interprets a workflow definition (JSON) into deterministic steps and drives AI coding engines through them, step by step. `conduct version` and the root `--version` flag print the version.
- Support for three AI coding engines — claude-code, antigravity, and qoder — each invoked headlessly for real execution. Each engine exposes different tunables (claude-code / qoder support a reasoning effort, antigravity takes only a model); invalid configuration is rejected before execution.
- `conduct workflow` command family: `create` / `edit` / `rename` / `delete` / `list` / `show` to manage workflows; `show --expand` previews the fully expanded step sequence (including loops); `run` interprets and runs a workflow, reading the requirement from stdin, with `--cwd` to set the working directory and `--json` for machine-readable output. Definitions are stored under `~/.conduct/workflows/`.
- Per-node loops in workflows: an evaluator self-loop (retry in place based on evaluation feedback) and a redo jump-back (loop back to an earlier node). Definitions are validated field by field, and invalid ones are refused.
- `conduct run` command family: `list` past runs (with `k/N` progress), `show` a single run (`--trace` for the step-by-step trace, `--json` for machine-readable output), and `stop <id>` to terminate an in-progress run. Runs that are interrupted or terminated are automatically detected as `interrupted`. Run records are persisted as `run.json` / `trace.jsonl` / `run-summary.md` under `~/.conduct/runs/`, and large or still-being-written artifacts are read safely.
- `conduct help <topic>` with built-in offline docs shipped in the binary — no network needed; the first topic, `prompts`, covers how to write good node prompts. Each command's `--help` gives complete usage: `create` / `edit` embed the definition JSON structure with a minimal example, `run` includes usage examples, and each points to the relevant help topic.
- `conduct ui`, a local web GUI bound to `127.0.0.1` only: workflow list, a visual editor, run list, and run detail pages; the editor pins validation errors to specific fields. `--port` (default 7420) sets the port and `--open` opens the browser. Runs started from the UI behave identically to terminal-started ones, and closing the UI does not affect a run in progress.
- `conduct update` self-update: downloads the prebuilt binary matching the current OS and architecture from GitHub Releases, verifies its checksum, and replaces the running binary in place. `--check` checks without installing, `--pre` includes pre-releases, and an explicit version (e.g. `conduct update v0.2.0`) can be requested. When installed via Homebrew it refuses to self-update and points to `brew upgrade` instead.
- Cross-platform prebuilt binaries: every release cross-compiles macOS / Linux for amd64 / arm64 and publishes them to GitHub Releases with checksums for verification.
- One-line install script: `curl -sSL https://raw.githubusercontent.com/qoggy/conduct/main/install.sh | sh`, which detects the OS and architecture and installs the latest version.
- Released under the MIT license.

[unreleased]: https://github.com/qoggy/conduct/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/qoggy/conduct/releases/tag/v0.0.1
