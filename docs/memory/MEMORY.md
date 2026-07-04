# conduct 项目记忆索引

- [改代码同步 docs/specs](coding-spec-sync.md) — 代码与规格文档必须一致，编辑文档保持全局一致性
- [--help 面向 LLM 信息完备](coding-help-for-llm.md) — 删存储/校验废话，补 JSON 结构与最小示例，枚举从能力表动态生成
- [长文档用 help topic 命令承载](coding-help-topic-command.md) — 教程/最佳实践不塞 --help，用 conduct help &lt;主题&gt; + go:embed 落 internal/help 包，按概念组织
