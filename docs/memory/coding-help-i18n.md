---
name: coding-help-i18n
description: CLI help 同时维护中文原文与忠实英文翻译，并复用 conduct 的统一语言解析结果
type: coding
---

conduct CLI 的 help 文案同时维护中文与英文。修改 Short / Long / flag 描述或内嵌 help 主题时，必须同步修改两种语言，英文忠实对应中文，不扩写、删减或改写原意；命令名、参数名和代码示例的机器语法在两种语言里保持原样。`<主题>`、`<命令>` 这类自然语言占位符、代码注释和示例值必须随文案语言翻译，英文 help 不得泄漏汉字。引擎适配器错误与底层技术诊断固定使用英文、原始引擎错误原样保留，不纳入 help 国际化。

help 复用 conduct 的统一语言解析结果：`~/.conduct/settings.json` 的 `language` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 英文。不得增加应用专属语言环境变量或 `--lang` 参数；完整解析、损坏设置 fail-loud 和文案分类规则见 `docs/specs/i18n.md`。

**Why:** 单语新增或两种文案漂移会让不同 locale 下的调用方获得不一致的命令能力说明。
**How to apply:** 新增或修改任何 CLI help 文案及 `internal/help` 内嵌主题时，同时提供中英文并补语言选择测试；对整棵 CLI 命令树的英文 help 和全部英文 help topic 做无汉字检查。不要把 `internal/locale` 接入引擎适配器或底层技术诊断。
