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
