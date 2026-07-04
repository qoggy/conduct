---
name: coding-help-topic-command
description: 长文档（教程/用法/最佳实践）用 help topic 命令承载，不塞进 --help；入口叫 help+主题，按概念组织
type: coding
---

`--help` 遵循社区标准 = **每命令的速查参考**（一句话描述 + usage + 选项 + 最小示例，精简）。教程、概念讲解、最佳实践这类**长文档不进 `--help`**，用一个独立命令承载。

**入口命令用 `help` + 主题（topic），不要发明 `guide` 之类的动词。** 对标 `go help <topic>`（modules / gopath / buildmode 等纯概念主题）、`git help <guide>`（everyday / tutorial）。conduct 基于 Cobra，用其原生 "additional help topics"（注册一个只有 `Long`、无 `Run` 的 `cobra.Command`，自动出现在 "Additional help topics:" 区），`conduct help <主题>` 调出。

**按「主题」组织，不按「命令」组织，也不是每命令一份文档。** 判据：内容若横跨多个命令（如「promptTemplate 怎么写好」同时关乎 create/edit/run），它是概念、不属于任一命令 → 放主题文档。per-command 复制这类内容会重复 + 漂移。

**发现性**靠两条补：在相关命令 `--help` 末尾加**一行指针**（只写「详见 `conduct help <主题>`」，不搬内容）；Cobra 的 "Additional help topics:" 区自动列出主题。

**实现形态**：一份 `.md` 用 `go:embed` 进二进制（conduct 走 `go install`，`docs/` 不随二进制发布，用户/沙箱 AI 看不到源码仓库的 `docs/`，故长文档必须 embed）→ 注册成 help topic。一个入口、可扩展到多份主题文档、单一数据源、随二进制发布。

**文档落地目录：新建独立包 `internal/help/`**，`.md` 主题文档 + `help.go`（`//go:embed *.md` 成 `embed.FS` + 主题注册表）都放这里，`internal/cli` import 它、只负责接上 `conduct help <主题>` 命令。内容与命令布线分离，贴合 conduct「一个关注点一个 internal 包」的布局；主题从 1 涨到 N 只需往 `internal/help/` 丢 `.md` + 注册一行。
**不能放仓库根 `docs/`**：`go:embed` 路径不允许 `..`、不能跨目录树，`.md` 必须与写 `//go:embed` 的 `.go` 同目录或其子目录；conduct 根不是 Go 包，无从嵌 `docs/`。放 `internal/help/` 不损失 GitHub 渲染/diff。

**Why:** conduct 的使用者是沙箱 AI，只有 PATH 上一个二进制、没有 man 生态也够不到 `docs/`；深度知识必须在二进制内且不污染精简的 `--help`。
**How to apply:** 需要给 conduct 增加教程 / 概念 / 最佳实践类文档时。与 coding-help-for-llm 配套：`--help` 管「本命令怎么用」，help topic 管「跨命令的craft/教程」。
