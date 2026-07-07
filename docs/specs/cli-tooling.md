# conduct CLI 工具层命令规格

> 本文规定 conduct CLI 的**工具层命令**——不针对 workflow / run 任一资源族、而关乎**工具自身**的顶层命令：打印版本（`version`）、可视化界面（`ui`）、自更新二进制（`update`）、跨命令长文档（`help`）。它们是 noun-first 命令风格里的显式例外（见 [cli-authoring.md](./cli-authoring.md)〈设计前提〉）。
>
> 工作流**定义**的编辑见 [docs/specs/cli-authoring.md](./cli-authoring.md)；**运行**工作流与运行记录见 [docs/specs/cli-runtime.md](./cli-runtime.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

本文承接 [cli-authoring.md](./cli-authoring.md)〈设计前提〉的地基。工具层命令的共同特征：

- **不属于任何资源族**：`workflow` / `run` 是资源名词，其下挂动词（`workflow edit` / `run list`）；而 `version` / `ui` / `update` / `help` 谈的是**工具本身**（版本号、GUI 外壳、二进制自更新、跨命令文档），不针对单一 workflow / run 对象，故做成顶层命令而非某名词的动词，对标 `go version` / `go help` / `gh` 的顶层辅助命令。
- **`ui` 是横切的人类外壳，无独占能力（北极星不变量）**：`ui` 覆盖 store 内全部工作流与运行，是 CLI 动词层的人类对等物——它做的每件事都有对应 CLI 命令（编辑 ↔ [cli-authoring.md](./cli-authoring.md)〈workflow edit〉/〈workflow node set〉、看状态 ↔ [cli-runtime.md](./cli-runtime.md)〈run list〉/〈run show〉、启动 ↔ [cli-runtime.md](./cli-runtime.md)〈workflow run〉、终止 ↔〈run stop〉），绝不新增「只有界面能做」的功能。
- **`update` 镜像分发机制**：conduct 以预编译 Release 分发（见 `AGENTS.md`〈发布〉的 GoReleaser 流水线），故自更新是「下二进制、验签、原地替换」，**不重新编译、不需要本机装 Go**。
- **文档分层**：各命令的 `--help` 只做精简速查；教程 / 概念 / 最佳实践这类**跨命令的长文档**不塞进 `--help`，改由 `conduct help <主题>` 输出（对标 `go help <topic>`）。

## 命令总览

| 命令 | 作用 |
| --- | --- |
| `conduct version` | 打印版本号 |
| `conduct ui` | 可视化界面：编辑工作流 / 监控运行 / 启动，conduct 的整体 GUI |
| `conduct update [版本]` | 自更新到最新 / 指定版本的预编译二进制 |
| `conduct help <主题>` | 输出跨命令的长文档（教程 / 概念 / 最佳实践） |

## 全局约定

**全局选项**（`-h, --help` 所有命令通用；`--version` 仅根命令）：

| 选项 | 说明 |
| --- | --- |
| `-h, --help` | 打印该命令的用法与选项后退出 `0` |
| `--version` | 仅根命令 `conduct --version`：打印版本号后退出 `0`（等价 `conduct version`；子命令不挂此旗标，与 gh / kubectl 惯例一致） |

**fail-loud 基线**：错误一律显式报出并以非 0 退出，绝不静默吞掉、绝不用空动作冒充成功（承自项目编码规范「错误不吞 / 不假装成功」）。

统一退出码见文末〈退出码约定〉。

---

## version — 打印版本

**用途**：打印当前 conduct 二进制的版本号。版本号在构建期由 GoReleaser 注入（`ldflags -X …internal/cli.version`，见 `AGENTS.md`〈发布〉）；从源码 `go install` / 本地 `make build` 未注入时为占位值（如 `dev`）。

**用法**：

```
conduct version
```

**参数**：无。

**输出**：

- stdout 打印版本号；退出 `0`。
- 等价形式：根命令 `conduct --version`（子命令不挂 `--version`，见〈全局约定〉）。

**示例**：

```bash
conduct version      # → 0.0.1
conduct --version    # 等价
```

---

## ui — 可视化界面（人类层）

**用途**：启动 conduct 的可视化界面（x-one-web 式的整体 GUI，覆盖 store 内全部工作流与运行），给人一个聚合视图：**编辑**全部工作流、**监控**各工作流的运行状态、**启动**运行。它是 CLI 动词层的人类对等物——见〈设计前提〉「无独占能力」不变量：它做的每件事都有对应 CLI 命令（编辑 ↔ [cli-authoring.md](./cli-authoring.md)〈workflow edit〉/〈workflow node set〉、看状态 ↔ [cli-runtime.md](./cli-runtime.md)〈run list〉/〈run show〉、启动 ↔〈workflow run〉、终止 ↔〈run stop〉）。

**用法**：

```
conduct ui [--port <n>] [--open]
```

**参数**：无位置参数——它是 conduct 的整体 GUI，覆盖 store 内全部工作流与运行，不针对单一对象（要按名字操作单个工作流，用 `workflow` 名词下的动词）。

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--port <n>` | 整数 | `7420` | 监听端口；被占则 stderr 报错退出 `1`（不自动递增——可预测、书签友好） |
| `--open` | 布尔 | `false` | 启动后自动打开浏览器；默认不开（照顾 SSH / 无头环境），仅打印地址 |

**输出**：

- 启动界面后 stdout 打印入口地址，进程驻留至界面关闭（`Ctrl-C` 退出）。
- store 不可读 / 端口被占等启动失败：stderr 打印原因；退出 `1`。

示意输出：

```
conduct ui — 可视化界面已启动
  ▶ http://127.0.0.1:7420
按 Ctrl-C 退出。
```

**主次用途**：编辑与监控是主用途；从界面**启动**运行是次要用途——启动主路径是 `conduct workflow run`（面向 AI / bash）。

**启动与安全边界（定案）**：

- 服务**只绑 `127.0.0.1`**，不监听 `0.0.0.0`。
- **启动时主动探测一次 store 可读性**（执行一次 `List`）：不可读 → stderr 报原因退出 `1`（不做「启动假成功、首个请求才报错」）。
- **v1 不做账号鉴权**，但所有 `/api/*` 校验 `Host` / `Origin` 白名单（仅 `127.0.0.1:<port>` / `localhost:<port>`）、变更类端点仅接受 `application/json`。诚实边界：这防的是**浏览器跨站**（恶意网页 fetch 本地端口）与 DNS rebinding，**不防本机进程**——单用户本机工具下可接受。
- **启动运行走 self-exec 子进程**：UI 服务端以 `os.Executable()` 自呼 `conduct workflow run <name> --cwd <dir>`（`Setsid` 独立成组、stdin 喂需求、stdout→`/dev/null`），使 pid 判活 / `interrupted` 语义与终端启动逐字节一致，且关掉 UI 不连累在跑的 run。这是「UI 无独占能力」不变量的最强证明——启动 ≡ 执行一条 CLI 命令。

工作流的可视化编辑统一由本命令承担，非交互的 CLI 编辑走 [cli-authoring.md](./cli-authoring.md) 的 `workflow edit`（全量）/ `workflow node …`（局部）。前端（内嵌 SPA）见 `docs/specs/ui.md`〈前端技术栈〉，代码已落地、待浏览器走查验收。

---

## update — 自更新（工具层）

**用途**：把当前 conduct 可执行文件自更新到最新（或指定）版本——从项目 GitHub Releases 下载匹配本机 `GOOS`/`GOARCH` 的预编译二进制，校验 `checksums.txt` 后**原地替换**正在运行的二进制。更新机制镜像分发机制（见〈设计前提〉），**不重新编译、不需要本机装 Go**。

**用法**：

```
conduct update [版本] [--check] [--pre]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<版本>` | 否 | 目标版本 tag（如 `v0.2.0`）；省略则取最新正式版。显式版本可命中预发布版本，是 opt-in beta 的入口 |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--check` | 布尔 | `false` | 只比对当前版本与最新发布并打印结论，**不下载、不安装** |
| `--pre` | 布尔 | `false` | 把预发布（`-beta` / `-rc` 等）版本纳入「最新」候选（不加则只认正式版） |

**输出**：

- 有更新且未加 `--check`：stdout 打印 `正在更新：<旧> → <新> …`，下载校验替换成功后打印 `已更新到 <新>（安装于 <路径>）` 及发布说明链接；退出 `0`。
- 已是最新：stdout 提示无需更新；退出 `0`。
- `--check`：只打印「已是最新 / 有新版本 / 当前版本非规范无法比较」三态之一；退出 `0`。
- 尚无任何 Release、或指定版本不存在 / 无匹配本机架构的资产：stderr 明确报错（fail-loud，不静默无动作）；退出 `1`。
- checksum 校验不过 / 下载失败 / 写入失败：stderr 转译原因；退出 `1`。
- 经 Homebrew 安装（可执行文件落在 brew 前缀下）：拒绝自替换，stderr 提示改用 `brew upgrade conduct`（不与包管理器打架）；退出 `1`。

**版本比较的诚实边界**：当前版本为 `dev` / git 伪版本 / dirty 等**非规范 semver** 时无法可靠比大小——此时 `--check` 如实说明「无法比较」并列出最新发布版本，绝不假装能算「落后几个版本」。有正式 tag 后比较即精确。

**示例**：

```bash
conduct update                 # 更新到最新正式版
conduct update --check         # 只看有没有新版
conduct update v0.2.0          # 装指定版本（显式版本号可 opt-in 预发布）
conduct update --pre           # 更新到最新版本，纳入 beta
```

---

## help — 跨命令长文档（支撑）

**用途**：输出**跨命令的长文档**——教程 / 概念 / 最佳实践，按概念组织（如 `prompts` 讲 `promptTemplate` 怎么写好）。这类内容不塞进各命令的 `--help`（`--help` 只做精简速查），改由本命令承载，对标 `go help <topic>`。文档 `go:embed` 进二进制随发布走（`docs/` 不随 `go install` 发布，故必须内嵌），相关命令的 `--help` 末尾留一行指针指向对应主题。

**用法**：

```
conduct help [主题]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<主题>` | 否 | 主题名（如 `prompts`）。省略时列出全部可用主题 |

**输出**：

- 给定已知主题：stdout 打印该主题的内嵌长文档；退出 `0`。
- 省略主题：stdout 列出全部可用主题及一句话简介；退出 `0`。
- 未知主题：stderr 报「未知主题 <x>」并列出可用主题；退出 `2`（用法错误）。

**已内置主题**：

| 主题 | 内容 |
| --- | --- |
| `prompts` | 如何写好节点 `promptTemplate`（模板变量、上游产物串联、评测官提示词） |

**示例**：

```bash
conduct help            # 列出全部主题
conduct help prompts    # 读「怎么写好提示词」
```

---

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 一般错误：无可用 Release、下载 / 校验 / 写入失败、端口被占、store 不可读、Homebrew 拒绝自替换等 |
| `2` | 用法错误（未知 help 主题、非法参数）——Cobra 默认 |

（编辑态 / 运行时命令的退出码见 [cli-authoring.md](./cli-authoring.md) 与 [cli-runtime.md](./cli-runtime.md)，同一张表。）

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `version` | **已实现**（构建期 `ldflags` 注入版本；根命令 `--version` 等价） |
| `ui` | **部分实现**（服务端 + `/api/*` 全端点 + self-exec 发射器就位：`internal/ui` 只绑 127.0.0.1、启动探测 store、Host/Origin 白名单、变更类强制 JSON；`conduct ui --port/--open` 已注册。handler / 预检 / run id 匹配 / 发射全链路经单测 + curl e2e。**内嵌前端 SPA 代码已落地、待浏览器走查验收**——`/` 服务 SPA 外壳（`index.html` + `js/` + `style.css`，随 go:embed 打进二进制）。self-exec 成组连带引擎子进程的组信号待真起引擎手工验） |
| `update` | **已实现**（`internal/cli/update.go`：经 `creativeprojects/go-selfupdate` 从 GitHub Releases 下载匹配架构资产、`checksums.txt` 校验、原地替换；`--check` / `--pre` / 显式版本；Homebrew 前缀拒绝自替换；非规范当前版本跳过比较。资产命名与 `.goreleaser.yaml` 对齐，改一处须同步另一处） |
| `help` | **部分实现**（命令 + `internal/help` 内嵌 `go:embed` 落地；当前仅 `prompts` 一个主题，按概念继续扩充） |

`ui` 内嵌前端的浏览器走查验收（服务端 + API + SPA 代码均已就位）尚未完成，是本工具层唯一的待验收项。
