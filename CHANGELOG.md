# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- CLI 骨架：`conduct` 根命令 + `version` 子命令
- 多引擎执行接口 `engine.Engine` 与注册表；claude-code / antigravity / qoder 三引擎**真实执行**（`claude -p` / `agy -p` / `qodercli -p`，均经真实调用冒烟）
- 各引擎 engineConfig 能力表（`engine.Capability`）：claude-code→`effort`、qoder→`reasoningEffort`、antigravity→仅 `model`
- `workflow` 命令族：`create` / `edit` / `rename` / `delete` / `list` / `show`（含 `show --expand` 展开）/ `run`（解释运行，`--cwd` / `--json` / stdin 需求）
- `run` 命令族：`list` / `show`（`--trace` / `--json` 四组合；`interrupted` 按 pid 存活读时派生）
- `conduct help <主题>` 帮助主题：跨命令的长文档（教程 / 概念 / 最佳实践）经 `go:embed` 内嵌、随二进制发布；首个主题 `prompts`（怎么写好节点 promptTemplate）
- `--help` 信息化改造：面向不熟悉 conduct 的调用方——`create` / `edit` 内嵌完整定义 JSON 结构 + 最小示例（引擎与 effort 取值从能力表动态生成）、`run` / `run show` 补齐用法与示例、删除存储位置等废话、相关命令末尾指向 `conduct help prompts`
- `internal/workflow` 领域层：定义类型 + 落盘校验（fail-loud）+ 展开 `expand` + 渲染 `render`（自 Python 原型移植）
- `internal/orchestrator` 解释器主循环：展开 → 渲染 → 逐步驱动引擎 → 串联产物/反馈 → 落盘 trace
- `internal/run` 运行记录实体（run.json / trace.jsonl / run-summary.md 类型与渲染，含 `interrupted` 派生）
- `internal/store` 托管层：`~/.conduct/workflows/` 与 `~/.conduct/runs/` 的原子读写与元数据管理
- `run stop <id>`：终止进行中的运行——`internal/run.StopProcess` 先按进程组发 SIGTERM、非组长（`ESRCH`）回退单进程；仅 `running` 可终止，不落新状态、进程停写后按 pid 判活派生 `interrupted`
- `workflow.ValidateStructured`：校验返回字段级 `[]Problem{Path, Message}`（供 UI「点错误 → 定位到 `nodes[i].字段`」）；`Validate` 退化为其字符串化包装，输出逐字不变
- `store.CountTrace`（流式数换行、零 JSON 解析，列表页进度 k/N 的 k）与 `store.ReadSummary`（读 run-summary.md，未生成返回哨兵 `ErrSummaryNotExist`）
- `workflow run --cwd` 显式路径存在性 + 目录校验：不存在 / 非目录即报用法错误退 `2`（发射前拦下，不烧引擎）
- `conduct ui`（服务端 + 命令，前端待下一子批）：只绑 `127.0.0.1` 的本地 Web GUI，把 CLI 动词镜像成人看的视图。`internal/ui` 提供 `/api/*` 全端点（workflows / runs 的增删改查、engines 能力表、version），启动即探测 store 可读性、Host/Origin 白名单 + 变更类强制 `application/json`；`--port`(7420)/`--open`。启动运行走 **self-exec 子进程**（`os.Executable()` 自呼 `workflow run`，`Setsid` 成组、stdin 喂需求、stdout→`/dev/null`、`go cmd.Wait()` 回收僵尸、run id 组合匹配），pid 判活 / interrupted 语义与终端启动逐字节一致，关掉 UI 不连累在跑的 run
- `run.ValidateWorkingDir`：工作目录存在性 + 目录校验的单一实现（类型化哨兵 `ErrWorkingDirNotExist` / `ErrWorkingDirNotDir`），由 CLI `workflow run` 与 UI 启动预检同源复用（`cli.resolveCwd` 重构为调用它，退 2 文案逐字不变）

### Changed

- `store.LoadTrace` 从 `bufio.Scanner`（16MB 单行上限）迁到 `bufio.Reader.ReadBytes('\n')`：消除超长产物行 `ErrTooLong` 崩溃，且只解析以换行结尾的完整行（末尾半行视为正在写入、丢弃不误报损坏）
