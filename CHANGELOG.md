# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.3] - 2026-07-09

### Added

- The UI workflow editor now offers model suggestions for claude-code (`sonnet` / `opus` / `fable`) and qoder (`Auto` / `Ultimate` / `Performance` / `Efficient` / `Lite`) in the inspector's model field. The field remains free text, so full model names and custom backend model names still work; `GET /api/engines` exposes the suggestions as `modelValues`.

### Changed

- The UI workflow editor inspector now uses one custom dropdown interaction for engine, effort, and model suggestions, with a consistent fixed-downward menu and outside-click closing instead of browser-native select / datalist popups.

### Fixed

- qoder failures with `is_error:true` now report messages from the `errors` array when present instead of returning an empty or misleading `result`, and failed calls now keep their real `durationMs`.
- claude-code non-zero exits now try to read a JSON error message from stdout before falling back to the old exit-code / stderr summary, so application-level failures with empty stderr still show the concrete reason.
- antigravity failures now prefer the dedicated `error` field and only fall back to the truncated `response` text when no error field is available.

## [0.0.2] - 2026-07-08

### Added

- `conduct workflow run -d` / `--detach`: launch a run in the background (detached from your terminal) and print its run id immediately, like `docker run -d`. Preflight (name, requirement, `--cwd`) still fails loudly before returning, so exit `0` always means a usable run id was printed. `-d --json` prints a single-line handle `{"id","workflow"}` — a pure addressable handle with no status field (the run may already have left `running` by the time it prints; read real status via `run wait` / `run show`).
- `conduct run wait <id>`: block until a run reaches a terminal state, then exit — like `docker wait` / Unix `wait`, the exit code reflects whether the wait itself succeeded (0 once any terminal state is reached), not the run's outcome. The run's outcome (`completed` / `failed` / `interrupted`) is on stdout (summary / `--json` `status`); only the command's own errors (missing id / IO) exit non-zero (1), and a missing / malformed id is a usage error (2).
- `conduct run rm <id>`: delete a run record (`runs/<id>/`). Refuses to delete a still-running run, confirms interactively unless `-y` / `--yes` is given, and accepts only a single id (no batch, no force).
- `conduct run list --status <state>`: filter the run list by derived state (`running` / `completed` / `failed` / `interrupted`); the default still lists all runs.
- `conduct run resume <id>`: resume a `failed` or `interrupted` run from where it stopped — already-succeeded steps are skipped and the run continues in place (same run id), picking up everything the earlier steps produced. `-d` / `--detach` resumes in the background like `workflow run -d`.
- A fourth AI coding engine, **codex** (`codex exec`), invoked headlessly like the others, with a `reasoningEffort` tunable.
- Each step now records its engine session id (`sessionId`) in the run's trace, so a single step can be replayed with the engine's own tooling.
- `conduct workflow copy <src> <dst>`: copy an existing workflow into a new named variant (the copy starts fresh, with its own timestamps).
- Per-node, field-level workflow editing: `conduct workflow node show` / `set` / `set-prompt` — export a single node, edit a node's or its evaluator's structured fields and attach / detach the two loop kinds, or set a node's prompt from raw text — so a single node can be tweaked without rewriting the whole definition.
- The `conduct ui` workflow list can now copy a workflow (mirrors `workflow copy`; the UI still has no exclusive capability over the CLI).

### Changed

- `conduct workflow list` (and the UI workflow list) now orders workflows by most-recently-updated first, instead of alphabetically by name.
- Run progress (`k/N`) and the run summary's step table now count each step once, so resuming a run can no longer push progress past the total (e.g. `11/10`). The full per-record trace is still available via `run show --trace`.

### Removed

- `run.json` no longer includes the `failedStep` field; the failed step is now derived from the trace's last `success:false` record and shown identically in `run show` / the summary / the UI.

### Fixed

- `install.sh` now resolves the latest release tag via the `releases/latest` redirect instead of the GitHub API, avoiding unauthenticated rate-limit (403) failures during `curl | sh`.
- `conduct run show` / `run stop` now exit `2` (usage error) for a malformed run id (empty / path separators), consistent with `run wait` / `run rm` and the exit-code convention; previously they exited `1`.
- The UI run-detail API (`GET /api/runs/{id}?trace=1`) now always returns a `trace` array — empty (`[]`) when the run has no trace entries — instead of omitting the field for empty traces.

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

[unreleased]: https://github.com/qoggy/conduct/compare/v0.0.3...HEAD
[0.0.3]: https://github.com/qoggy/conduct/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/qoggy/conduct/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/qoggy/conduct/releases/tag/v0.0.1
