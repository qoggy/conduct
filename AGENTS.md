## 这是什么

conduct —— 把 workflow 定义（JSON）解释成确定性步骤、逐步驱动多种 AI 编程引擎（claude-code / codex / qoder / gemini）执行的命令行工具。

## 动手前必读

- **语言 / 布局**：Go，标准布局。入口在 `cmd/conduct/`，其余全在 `internal/`。
- **改完必须自检**：`make fmt && make vet && make test && make build` 全绿才算完成。
- **错误不吞**：`error` 一律处理——用 `fmt.Errorf("...: %w", err)` 包装上抛，或记录后处理；禁止 `_ = err` 静默丢弃。
- **命名求完整**：用完整单词表达意图（`WorkflowNodeDefinition` 而非 `NodeDef`）。
- **不假装成功**：未实现的路径显式返回错误（如 `engine.ErrNotImplemented`），不要用空实现冒充可用。

## 常用命令

```bash
make build     # 构建到 ./bin/conduct
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -s -w .
```

发布（beta / 正式）流程见 [README](./README.md#开发)。
