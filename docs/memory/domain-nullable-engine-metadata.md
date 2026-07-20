---
name: domain-nullable-engine-metadata
description: 引擎未提供 token usage 或 session id 时必须用 null，禁止用零值、空字符串或字段缺省冒充
type: domain
---

引擎运行结果与 trace 中的 token usage、session id 是可空 metadata：

- 引擎明确提供 token 数时保存该数值；已知的 `0` 是有效值。
- 引擎没有提供 token usage 时，Go 字段使用 `nil`，JSON 明确写 `"tokens": null`。
- 引擎明确提供非空 session id 时保存该字符串。
- 引擎没有提供可用 session id 时，Go 字段使用 `nil`，JSON 明确写 `"sessionId": null`。
- 禁止用 `0`、`""` 或 `omitempty` 造成的字段缺省表达“未知”。该规则适用于全部引擎，不为单个引擎特判。

**Why:** `0` 和空字符串看起来像真实返回值，字段缺省又让不同输出路径形态不一致；JSON `null` 才能明确、稳定地表达引擎没有提供数据。
**How to apply:** `RunResult`、`TraceEntry`、引擎输出解析、CLI JSON、HTTP trace 与 UI 展示统一保留可空语义；人读界面在值为 `nil` 时省略对应信息，非 `nil` 时如实显示，包括已知值 `0`。
