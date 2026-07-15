---
name: coding-i18n-boundaries
description: 技术诊断固定英文，CLI/UI 产品文案按 settings、locale 环境变量与英文兜底统一选择
type: coding
---

conduct 生成的底层技术诊断、服务端日志和浏览器 console 诊断固定英文；被包装的操作系统、第三方或引擎原文仍原样保留。

CLI 和 Web UI 的所有人类可读产品文案均支持中英文，语言统一按 `~/.conduct/settings.json` 的 `language` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 英文解析。

`settings.json` 的 `language` 只允许 `en` / `zh-CN`；属性缺失表示跟随环境。文件缺失或属性缺失可正常降级；文件无法读取、JSON 损坏或值非法时必须用固定英文技术诊断 fail-loud。更新语言时只修改/删除 `language`，保留其它属性并原子写回。

UI 提供“跟随环境 / 中文 / English”：跟随环境即删除 `language`，其它两项写入对应值。这是全局设置，会影响之后的 UI 和 CLI；已启动的 CLI 不动态切换。

共享层中需要本地化的领域错误必须用稳定错误码 + 结构化参数表达；CLI 直接渲染，HTTP API 把错误码 + 参数交给 UI 渲染。不得用某种语言的 `err.Error()` 文本作为程序分支依据。

`run-summary.md` 使用运行开始时快照的已解析语言，resume 与收尾沿用；生成后不因全局语言或 UI 切换而重写。命令/flag、JSON 字段、状态枚举、engine/id/错误码不翻译；用户输入、AI 产物和原始外部错误也不翻译。

**Why:** 把领域句子、技术诊断和页面文案混成一层，会导致英文 CLI 泄漏汉字、UI 无法切换错误文案，以及程序依赖翻译文本后无法安全演进。
**How to apply:** 新增文案前先判定是技术诊断还是产品文案；完整行为、settings/API schema、错误边界与测试要求见 `docs/specs/i18n.md`。
