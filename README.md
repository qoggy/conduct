# conduct

把 workflow 定义（JSON）解释成确定性步骤、逐步驱动多种 AI 编程引擎（claude-code / antigravity / qoder）执行的命令行工具。

## 开发

发布遵循语义化版本（SemVer）+ [Keep a Changelog](https://keepachangelog.com/)。

### 本地构建

```bash
make build                                    # 产出 ./bin/conduct（构建时注入 tag 版本号）
./bin/conduct --help
```

### 本地测试

```bash
make test                                     # go test ./...
make vet                                      # go vet ./...
make fmt                                       # gofmt -s -w .
```

### 本地安装

把当前本地代码装成全局 `conduct` 命令：

```bash
make install                                  # = go install ./cmd/conduct，装到 ~/go/bin
conduct version
```

### 发布 beta 版本

打一个 SemVer 预发布 tag 并推送。`@latest` 不会选中预发布版本，须显式指定版本号安装——即 opt-in 的 beta 通道：

```bash
git tag v0.1.0-beta.1
git push origin v0.1.0-beta.1
go install github.com/qoggy/conduct/cmd/conduct@v0.1.0-beta.1
```

### 发布正式版本

先把 CHANGELOG 的 `[Unreleased]` 归入该版本号，再打正式 tag 并推送，`@latest` 随即解析到它：

```bash
git tag v0.1.0
git push origin v0.1.0
go install github.com/qoggy/conduct/cmd/conduct@latest
```

> 私有仓库：安装方需 `export GOPRIVATE=github.com/qoggy/*` 且具备 git 拉取凭证。
