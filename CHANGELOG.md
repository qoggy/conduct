# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.2] - 2026-07-20

### Added

- Added Kiro as a supported AI coding engine, using the user's existing Kiro profile and project configuration and supporting per-node model and effort settings.

### Changed

- Engines that support reasoning-effort configuration now use `engineConfig.effort` and the `--effort` CLI option.
- Run details now use JSON `null` when token usage or session information is unavailable; human-readable CLI and UI views omit unavailable values.
- Running a Kiro node sets `chat.disableMarkdownRendering=true` in the active Kiro profile. The setting affects Kiro's classic UI, persists after the run, and is not restored automatically.
- The workflow editor now suggests Kiro and Codex model names while continuing to accept custom model values.

### Removed

- Removed `reasoningEffort` and `--reasoning-effort`. Existing workflows, run records, and scripts using the old names are not migrated automatically and must be updated to use `effort` and `--effort`.

## [0.1.1] - 2026-07-16

### Added

- Chinese and English localization for CLI help, user-facing commands and errors, generated run summaries, and the web UI. Language resolution uses `~/.conduct/settings.json` first, then `LC_ALL`, `LC_MESSAGES`, and `LANG`, with English as the fallback.
- A global UI settings page for choosing Chinese, English, or the environment language, and for choosing light, dark, or the system theme. Both preferences are stored in `~/.conduct/settings.json`, so language changes also apply to subsequently started CLI commands.
- Independently designed light and dark UI themes, including system-theme following and accessible dark-theme contrast, without changing page layouts.

### Changed

- conduct's own technical diagnostics, engine-adapter errors, logs, and browser console diagnostics now use stable English text, while user input, AI output, and original third-party error text remain unchanged.
- New run records persist the resolved language and use that snapshot for summaries and resume. Run records without a valid `language` field are treated as corrupt rather than silently falling back to a current or legacy language.

Run records created before v0.1.1 do not contain the required language snapshot and are not compatible with this release; they must be recreated before they can be read or resumed.

## [0.1.0] - 2026-07-15

### Added

- Workflows are now a **parallel DAG of nodes and edges** instead of a linear node sequence: every definition carries two reserved marker nodes, `START` (the single source) and `END` (the single sink), and `START` can fan out to multiple nodes so a workflow can run several nodes in parallel from the very first moment. Execution is scheduled by edge dependencies with no concurrency cap — a node starts as soon as all of its predecessors have succeeded.
- `conduct workflow node add <name> <id>`: add a node and wire its edges in one atomic step. With no `--from`/`--to` it auto-connects `START → <id> → END` (so repeated bare `node add` calls produce nodes that all fan out from `START` in parallel); `--from`/`--to` accept comma-separated predecessor/successor ids (including `START`/`END`).
- `conduct workflow node rm <name> <id>`: remove a node and cascade-delete its edges, re-validating the resulting graph; `START`/`END` and removals that would orphan another node are rejected.
- `conduct workflow edge <name> --add <from:to> --rm <from:to>`: atomically batch-add/remove edges (target edge set = current − rm ∪ add), validated as a whole before saving — this makes topology changes with no valid single-edge intermediate state (e.g. reordering a chain) possible in one command.
- `conduct workflow edge list <name>`: list every edge of a workflow.
- `conduct workflow node set <name> <id> --id <new-id>`: rename an agent node while automatically updating every connected edge and live `{{<nodeId>}}` prompt reference; escaped literal references remain unchanged.
- `conduct workflow show --expand` now prints a **topological-level preview** (nodes that can run in parallel grouped per level) instead of a flat expanded step list; `--json --expand` adds a `levels` field.
- Prompt templates may only reference an **upstream ancestor** agent node via `{{<nodeId>}}` (validated at save time); referencing a non-ancestor, a nonexistent node, or the marker nodes `{{START}}`/`{{END}}` is rejected before it reaches disk.
- `conduct help prompts` gains a section on avoiding write conflicts between parallel branches (design them as non-overlapping tasks, or have each branch use its own `git worktree`), since conduct itself does not isolate worktrees or cap concurrency.
- The UI workflow editor now provides a DAG canvas for adding, removing, renaming, and connecting nodes, including cycle prevention and removal of redundant transitive edges; run detail shows the frozen DAG and its current execution frontier.

### Changed

- The on-disk workflow record is now `{name, createdAt, updatedAt, definition: {nodes, edges}}` — the definition body (what `create --definition` / `edit` read from stdin) is just `{nodes, edges}`, nested under `definition`, rather than a flat record.
- `conduct workflow node set` is now focused on `--id`/`--engine`/`--model`/`--effort`/`--reasoning-effort`/`--display-name`; loop-related options have been removed.
- `conduct workflow create --definition` / `edit` / the UI's `PUT /api/workflows/{name}` no longer reject an import whose embedded `name` differs from the target — metadata (`name`, timestamps) is always taken from the command argument / route and any embedded value is silently ignored (previously a mismatch was a hard error).
- Run execution now drives the parallel scheduler instead of a linear step runner: nodes with all predecessors satisfied run concurrently (no `--max-parallel` cap), and `workflow run` / `run resume` print a per-node event stream (`▶`/`✓`/`✗`) instead of numbered steps.
- On a node failure, the run now **drains**: no new nodes are scheduled, but nodes already in flight are allowed to finish (and their successful output is kept) before the run is finalized as `failed`.
- `trace.jsonl` entries are keyed by `nodeId` and carry `startedAt`/`endedAt` instead of `stepIndex`/`type`/`iteration`; `run.json` no longer has a `steps` field — progress is now `k` (successful, deduplicated-by-`nodeId` node count) out of `N` (agent node count, excluding `START`/`END`), computed from the frozen `workflowSnapshot`.
- `conduct run resume` now derives its resume point per `nodeId` instead of a linear step index: it replays the trace to find the set of already-succeeded agent nodes, works out which remaining nodes are unblocked (all predecessors done), and reruns only those through the same parallel scheduler — so a partially-failed parallel run only re-executes the branch(es) that didn't finish, not the whole run.
- `conduct run list` / the UI run list report `NODES` / `nodeCount` (agent node count) instead of `STEPS` / `steps`.
- `conduct workflow list` (and the UI workflow list) now show the agent-node id chain in topological order, excluding `START`/`END`.
- The UI run detail now renders the user requirement and node inputs/outputs as sanitized Markdown, with Prism syntax highlighting for fenced code blocks; copying still returns the original unmodified text.

### Removed

- Per-node loops are gone: the `evaluator` self-loop ("write → review → revise", fixed-count retries) and the `redoTarget` jump-back, along with `loopCount` and the `node set --evaluator/--no-evaluator/--redo-target/--no-redo/--loop-count` flags. A DAG cannot contain cycles, so this iterative-refinement capability has no direct replacement; express "review and fix" as two nodes instead (`write(a) → review-and-fix(b)`, with `b` reading `{{a}}` once).
- The old expansion step (`Expand` / `ExecutionStep`, a flattened linear step list including loop unrolling) is removed entirely, replaced by the graph algorithms and parallel scheduler above.

Existing workflow definitions (linear node sequence with loops) and run records (linear `stepIndex`-based trace) are **not compatible** with this release and are not migrated — they must be recreated under the new node-and-edge model; old run records can no longer be resumed with `run resume`.

### Fixed

- `conduct help <command-path>` now rejects unknown trailing path segments with exit code `2` instead of silently showing help for the nearest matching parent command.
- Parallel run failures now report all engine and persistence errors collected while draining in-flight nodes, instead of discarding every error after the first one.

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

[unreleased]: https://github.com/qoggy/conduct/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/qoggy/conduct/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/qoggy/conduct/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/qoggy/conduct/compare/v0.0.3...v0.1.0
[0.0.3]: https://github.com/qoggy/conduct/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/qoggy/conduct/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/qoggy/conduct/releases/tag/v0.0.1
