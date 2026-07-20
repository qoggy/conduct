---
name: coding-help-shared-constants
description: 通用常量（id 约束 / engine 及 engineConfig 字段 / 模板变量 / 图约束）在各命令 help 与文档里必须用统一的规范措辞，不各写各的
type: coding
---

一批**通用常量**会在多个命令的 `--help` 与文档里反复出现，同一个东西必须用**同一套规范描述**，禁止各命令各写各的。这类常量包括：

- **节点 id 约束**：`唯一、须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$、不得为 START/END`
- **engine 取值与各自 engineConfig 允许字段**：从 `internal/engine` 的 descriptor 注册表动态生成；只描述 `AllowsModel` / `AllowsEffort` 和 `EffortValues` 能表达的通用事实，引擎私有约定留在 `docs/specs/engines.md`
- **模板变量**：`{{sys.userPrompt}}`=用户需求、`{{sys.cwd}}`=工作目录、`{{sys.runId}}`=本次运行的 run id、`{{<节点id>}}`=引用该上游祖先节点产物（未运行则空串）、`\{{x}}`=转义为字面量
- **图约束**：恰好一个 START、一个 END；无环；每个 agent 节点有入有出；`{{<id>}}` 只能引用上游祖先 agent 节点（禁 `{{START}}`/`{{END}}`）

以现有最完整的那份为**权威来源**照抄（当前 `conduct workflow edit --help` 的这几段即权威版本），别自造变体——如把 id 约束写成「须合法、不叫 START 或 END」就是漂移。

**Why:** 同一事物在不同命令 help 里描述不一致，会让读者（尤其沙箱里靠 `--help` 现学现用的 AI）困惑，也让维护时改一处漏一处、越漂越远。
**How to apply:** 新增 / 改任何命令的 Short / Long / flag 描述、或文档里提到上述通用常量时，照权威来源的措辞抄。engine 名与 effort 枚举从 `internal/engine` 的 descriptor 注册表动态生成，避免静态文案漂移（见 [[coding-help-for-llm]]）；改动波及 docs/specs 时同步（见 [[coding-spec-sync]]）。
