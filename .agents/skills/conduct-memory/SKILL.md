---
name: conduct-memory
description: 读写 conduct 项目跨开发者共享的长期记忆（存于 docs/memory/，含编码规范、业务术语、技术栈约定、领域模型等事实）。以下场景使用：(1) 开始处理涉及项目长期约定的任务前，先查阅已有记忆再动手；(2) 本轮形成了跨 Issue 长期有效的规范、术语或领域模型，需要沉淀；(3) 用户明确要求"记住这条""归纳项目规范""写入记忆"。
---

# Memory Skill

- 项目记忆：跨开发者共享，存放在仓库的 `docs/memory/` 目录。

## 项目记忆文件

- `docs/memory/MEMORY.md`：项目记忆索引。
- `docs/memory/**`：项目记忆详情文件，可按主题拆分为 Markdown 文件或子目录。

`MEMORY.md` 是纯索引，只放一行一条指向详情文件的链接，本身不写任何记忆内容。所有编码规范、领域模型、术语、技术栈规范等事实都写进各自的详情文件，再从 `MEMORY.md` 引用。

## 项目记忆内容示例

记忆采用两步结构（two-step）：`MEMORY.md` 是**索引**，每条事实是一个**独立文件**（one fact, one file）。

- **第一步**：把每条事实写成自己的文件，带 frontmatter（`name` / `description` / `type`）+ 正文。涉及"为什么/何处适用"的规则，正文用 `**Why:**` 与 `**How to apply:**` 两行收尾。
- **第二步**：在 `MEMORY.md` 里加一行指针 `- [标题](文件.md) — 一行描述`。**绝不**把记忆内容直接写进 `MEMORY.md`，它只是索引。

`type` 取值：`coding`（编码规范）/ `glossary`（业务术语）/ `tech`（技术栈）/ `domain`（领域模型）等，按主题语义命名即可。

`docs/memory/MEMORY.md`（索引，会被注入上下文，保持精简）：

```markdown
- [金额字段用 decimal(21,6)](coding-money-decimal.md) — 金额禁 float/double
- [软删除用 deleted_at](coding-soft-delete.md) — 禁止物理删除
- [术语：工单 WorkOrder](glossary-workorder.md) — 一次交付任务的最小单元
- [后端用 Hono 禁 Express](tech-backend-hono.md) — Node.js 20 + Hono
```

`docs/memory/coding-money-decimal.md`（编码规范，带 Why/How）：

```markdown
---
name: coding-money-decimal
description: 金额字段统一用 decimal(21,6)，禁止浮点类型
type: coding
---

金额字段必须用 `decimal(21,6)`，禁止 float/double。

**Why:** 浮点类型有精度丢失，财务对账会出错。
**How to apply:** 建表 / 加列涉及金额字段时统一用此类型。
```

`docs/memory/glossary-workorder.md`（业务术语，纯定义无需 Why/How）：

```markdown
---
name: glossary-workorder
description: 业务术语「工单 WorkOrder」的定义
type: glossary
---

工单（WorkOrder）：一次交付任务的最小单元，对应一个履约流程。
```

`docs/memory/tech-backend-hono.md`（技术栈要求，带 Why/How）：

```markdown
---
name: tech-backend-hono
description: 后端技术栈用 Node.js 20 + Hono，禁止 Express
type: tech
---

后端统一用 Node.js 20 + Hono，禁止引入 Express。

**Why:** 团队已标准化在 Hono，混用框架增加维护成本。
**How to apply:** 新增服务或路由时。
```

## 读取规则

处理和项目长期约定相关的任务时：

1. 先读取 `docs/memory/MEMORY.md`。
2. 再根据索引和当前任务读取 `docs/memory/` 下相关详情文件。
3. 不要盲读整个 `memory/` 目录。

## 什么时候更新产品记忆

以下场景适合更新产品记忆：

1. 用户明确说“记住这条产品规范”“写入产品记忆”。
2. 本轮形成了跨 Issue 长期有效的编码规范、业务术语或领域模型。
3. 发现了会反复影响开发的产品级坑点或约束。

以下场景不要更新：

- 一次性调试信息。
- 某个临时 Issue 的过程细节。
- 仅属于某个开发者个人偏好的内容。
- 敏感信息，例如密钥、密码、凭证。

## 更新产品记忆

1. 读取 `docs/memory/MEMORY.md`，确认已有结构和命名，避免写重复记忆。
2. 把每条事实写成独立详情文件（带 frontmatter + 正文）；必要时新建子目录。
3. 在 `docs/memory/MEMORY.md` 追加一行索引指针 `- [标题](文件.md) — 一行描述`。
