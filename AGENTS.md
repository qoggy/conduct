## 这是什么

conduct —— 把 workflow 定义（JSON）解释成确定性步骤、逐步驱动已注册 AI 编程引擎执行的命令行工具。当前引擎清单以 `conduct --help` 和 `docs/specs/engines.md` 为准。

## 动手前必读

- **读记忆**：读 `@docs/memory/MEMORY.md`（项目长期记忆索引），按需再读其中链接的记忆文件。
- **语言 / 布局**：Go，标准布局。入口在 `cmd/conduct/`，其余全在 `internal/`。
- **改完必须自检**：`make fmt && make vet && make test && make build` 全绿才算完成。
- **错误不吞**：`error` 一律处理——用 `fmt.Errorf("...: %w", err)` 包装上抛，或记录后处理；禁止 `_ = err` 静默丢弃。
- **命名求完整**：用完整单词表达意图（`WorkflowNodeDefinition` 而非 `NodeDef`）。
- **不假装成功**：未实现的路径显式返回错误（如 `engine.ErrNotImplemented`），不要用空实现冒充可用。

## 常用命令

```bash
make build      # 构建到 ./bin/conduct（注入 tag 版本号）
make test       # go test ./...
make vet        # go vet ./...
make fmt        # gofmt -s -w .
make install    # = go install ./cmd/conduct，把本地代码装成全局 conduct（落到 ~/go/bin）
make uninstall  # 删除已安装的全局 conduct
```

## 发布

遵循语义化版本（SemVer）+ [Keep a Changelog](https://keepachangelog.com/)。发布由 GoReleaser 自动化：push 一个 `v*` tag，GitHub Actions（`.github/workflows/release.yml`）即交叉编译 macOS / Linux 的 amd64+arm64、打包、生成带 `checksums.txt` 的 GitHub Release。`curl | sh` 安装脚本（`install.sh`）与 `conduct update` 自更新都从该 Release 取预编译二进制——资产命名 `conduct_<os>_<arch>.tar.gz` 与 `internal/cli/update.go` 所用自更新库的约定对齐，改 `.goreleaser.yaml` 的命名务必同步改 `update.go`。

本地验证打包（不发布，产物落 `dist/`，已被 gitignore）：

```bash
goreleaser release --snapshot --clean
```

### 正式版本

先把 CHANGELOG 的 `[Unreleased]` 归入该版本号并补日期，再打正式 tag 推送，Actions 随即出 Release：

```bash
git tag v0.0.1
git push origin v0.0.1
```

### beta 版本

打一个含预发布标记（`-beta` / `-rc` 等）的 SemVer tag 并推送；GoReleaser 自动将其标记为 GitHub 预发布，`conduct update` 默认不选中它，用户须显式指定版本号安装——即 opt-in 的 beta 通道：

```bash
git tag v0.1.0-beta.1
git push origin v0.1.0-beta.1
# 用户侧：conduct update v0.1.0-beta.1
```
