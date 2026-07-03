---
name: coding-spec-sync
description: 改代码必须同步 docs/specs，保持代码与规格文档一致
type: coding
---

修改代码时必须保持代码与 `docs/specs/` 下的规格文档一致：改动实现的同时，同步更新对应的规格文档，二者不得脱节。

编辑规格文档时保持**全局一致性**：先通读全文识别所有同类改动点，再从头到尾统一修正，不要局部打补丁、只改后面不改前面。

**Why:** 规格文档是 conduct 行为的事实来源（如 `cli-commands.md`）；代码与文档脱节会让后续开发者以过时规格为准，产生错误实现。
**How to apply:** 每次改动 CLI 命令、workflow 解释逻辑、引擎行为等有规格文档覆盖的功能时，同一次改动内更新 `docs/specs/` 对应文档。
