---
name: coding-help-for-llm
description: --help 面向不懂 conduct 的 LLM，信息完备优先于人类友好：删实现细节废话，补参数结构与最小示例
type: coding
---

`--help`（各命令的 Short / Long / 示例）的首要读者是一个**不了解 conduct 的 LLM**（通过 bash 调用）。目标：读完某命令的 `--help` 就能正确使用它，无需外部文档。

判据：信息量必须**完备**，语言可以**精简**——追求高信息密度，不追求 human friendly。该说的说全，不该说的删掉。

**删「废话」**——对「怎么用」无贡献的实现细节：
- 存储位置 / 方式（如 `存于 ~/.conduct/...`）。使用者不需要知道存哪、怎么存就能用。
- 「保存即校验」这类预告。校验失败自然会报错并给出信息，无需提前声明。
- 指向未实现命令（如当前的 `conduct ui`）。

**补「缺失的关键信息」**——对不熟悉者真正必要的：
- 参数结构，尤其经 stdin 传入的 JSON schema（字段、必填/选填、取值约束）。
- 一个可复制粘贴的最小示例。

反例：`conduct workflow edit <name>` 从 stdin 读 JSON，只写「整体替换」不够——LLM 不知道 JSON 长什么样、有哪些字段，必须给出结构说明 + 最小示例才可用。

**边界**：「信息完备」指**把本命令用起来**所需（参数结构 + 最小示例），不是把教程 / 概念 / 最佳实践也塞进来。后者是跨命令的深度内容，放独立的 help topic 命令（见 coding-help-topic-command），`--help` 末尾只留一行指针。`--help` 仍遵循社区标准的精简速查定位。

**Why:** conduct 的核心使用者是沙箱里的 AI，`--help` 是它现学现用的唯一入口；缺参数结构 / 示例它就没法用，塞存储细节 / 套话则挤占 context 又无用。
**How to apply:** 新增或修改任何命令的 Short / Long / flag 描述时。取值枚举（引擎名、effort 集等）从 `internal/engine` 的 descriptor 注册表动态生成，避免静态文案与实现漂移。
