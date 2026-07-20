# conduct 项目记忆索引

- [改代码同步 docs/specs](coding-spec-sync.md) — 代码与规格文档必须一致，编辑文档保持全局一致性
- [--help 面向 LLM 信息完备](coding-help-for-llm.md) — 删存储/校验废话，补 JSON 结构与最小示例，枚举从能力表动态生成
- [长文档用 help topic 命令承载](coding-help-topic-command.md) — 教程/最佳实践不塞 --help，用 conduct help &lt;主题&gt; + go:embed 落 internal/help 包，按概念组织
- [通用常量措辞统一](coding-help-shared-constants.md) — id 约束/engine 字段/模板变量/图约束跨命令 help 与文档用同一套规范描述，以 workflow edit --help 为权威
- [CLI help 中英文同步](coding-help-i18n.md) — help 双语逐项对应，并复用 conduct 的统一语言解析结果
- [conduct 语言边界](coding-i18n-boundaries.md) — 技术诊断固定英文，CLI/UI 产品文案统一按 settings 与 locale 选择
- [UI dark 主题独立设计](coding-ui-dark-theme.md) — dark 使用独立语义令牌和表面层级，禁止简单反转 light 或复用正文色作背景
- [引擎 metadata 缺失用 null](domain-nullable-engine-metadata.md) — token usage / session id 未提供时禁用零值、空字符串或字段缺省冒充
