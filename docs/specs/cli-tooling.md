# conduct CLI 工具层命令规格

> 本文规定 conduct CLI 的**工具层命令**——不针对 workflow / run 任一资源族、而关乎**工具自身**的顶层命令：打印版本（`version`）、可视化界面（`ui`）、安装 Agent Skill（`init`）、更新二进制与已登记 Skill（`update`）、跨命令长文档（`help`）。它们是 noun-first 命令风格里的显式例外（见 [cli-authoring.md](./cli-authoring.md)〈设计前提〉）。
>
> 工作流**定义**的编辑见 [docs/specs/cli-authoring.md](./cli-authoring.md)；**运行**工作流与运行记录见 [docs/specs/cli-runtime.md](./cli-runtime.md)。
>
> 这是**设计规格（面向评审与实现对齐）**，不是「已实现功能说明」；逐条实现状态见文末〈实现状态〉。

## 设计前提（可推翻，动手实现前请确认）

本文承接 [cli-authoring.md](./cli-authoring.md)〈设计前提〉的地基。工具层命令的共同特征：

- **不属于任何资源族**：`workflow` / `run` 是资源名词，其下挂动词（`workflow edit` / `run list`）；而 `version` / `ui` / `init` / `update` / `help` 谈的是**工具本身**（版本号、GUI 外壳、Skill 安装、二进制与 Skill 更新、跨命令文档），不针对单一 workflow / run 对象，故做成顶层命令而非某名词的动词，对标 `go version` / `go help` / `gh` 的顶层辅助命令。
- **`ui` 是横切的人类外壳，无独占能力（北极星不变量）**：`ui` 覆盖 store 内全部工作流与运行，是 CLI 动词层的人类对等物——它做的每件事都有对应 CLI 命令（编辑 ↔ [cli-authoring.md](./cli-authoring.md)〈workflow edit〉/〈workflow node set〉、看状态 ↔ [cli-runtime.md](./cli-runtime.md)〈run list〉/〈run show〉、启动 ↔ [cli-runtime.md](./cli-runtime.md)〈workflow run〉、终止 ↔〈run stop〉），绝不新增「只有界面能做」的功能。
- **`update` 镜像分发机制**：conduct 以预编译 Release 分发（见 `AGENTS.md`〈发布〉的 GoReleaser 流水线），故自更新是「下二进制、按 `checksums.txt` 校验 checksum、原地替换」，**不做数字签名验证、不重新编译、不需要本机装 Go**。
- **文档分层**：各命令的 `--help` 只做精简速查；教程 / 概念 / 最佳实践这类**跨命令的长文档**不塞进 `--help`，改由 `conduct help <主题>` 输出（对标 `go help <topic>`）。

## 命令总览

| 命令 | 作用 |
| --- | --- |
| `conduct version` | 打印版本号 |
| `conduct ui` | 可视化界面：编辑工作流 / 监控运行 / 启动，conduct 的整体 GUI |
| `conduct init` | 为 AI 编程工具安装 conduct Skill，登记安装范围与目标 |
| `conduct update [版本]` | 自更新到最新 / 指定版本的预编译二进制，并同步已登记 Skill |
| `conduct help <主题>` | 输出跨命令的长文档（教程 / 概念 / 最佳实践） |

## 全局约定

**全局选项**（`-h, --help` 所有命令通用；`--version` 仅根命令）：

| 选项 | 说明 |
| --- | --- |
| `-h, --help` | 打印该命令的用法与选项后退出 `0` |
| `--version` | 仅根命令 `conduct --version`：打印版本号后退出 `0`（等价 `conduct version`；子命令不挂此旗标，与 gh / kubectl 惯例一致） |

**界面语言**：CLI 面向人的产品文案（包括 help、用法/领域错误、校验、确认、成功/警告、空态和人读输出）统一按 `~/.conduct/settings.json` 的 `language` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 英文解析。设置文件或属性缺失时才继续读取环境变量；设置无法读取、JSON 损坏或值非法时以固定英文技术诊断报错退出。高优先级环境变量非空但无法识别时直接使用英文，不再读取低优先级变量。不提供应用专属语言环境变量或 `--lang` 参数。底层技术诊断固定英文，机器协议与用户/引擎原文不翻译；完整规则见 [i18n.md](./i18n.md)。

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

- stdout 打印 `conduct <版本号>`；退出 `0`。
- 等价形式：根命令 `conduct --version`（子命令不挂 `--version`，见〈全局约定〉）。

**示例**：

```bash
conduct version      # → conduct 0.0.1
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
| `--port <n>` | 整数 | `7420` | 监听端口；固定非零端口被占则 stderr 报错退出 `1`（不自动递增）；传 `0` 时由操作系统分配空闲端口，stdout 与安全白名单均使用实际端口，适合隔离测试 |
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

工作流的可视化编辑统一由本命令承担，非交互的 CLI 编辑走 [cli-authoring.md](./cli-authoring.md) 的 `workflow edit`（全量）/ `workflow node …`（局部）。前端（内嵌 SPA）通过顶栏设置入口进入独立设置页，在其中选择语言（跟随环境 / 中文 / English）与主题（跟随系统 / light / dark）；选择写入 `~/.conduct/settings.json`，浏览器不使用 `localStorage` 另存偏好。完整行为见 [ui.md](./ui.md)〈设置〉与〈前端技术栈〉，持久化 schema 和语言解析规则见 [i18n.md](./i18n.md)，浏览器验收用例见 [ui-frontend.md](../test-cases/ui-frontend.md)。

---

## init — 安装 Agent Skill

**用途**：将当前 CLI 内嵌的同版本完整 conduct Skill 安装到所选 Agent 的 Skill 目录。单二进制即可离线安装，不依赖源码 checkout、下载、Node.js 或 npm；不执行 Agent 二进制，也不声称 Agent 已安装或成功加载 Skill。`install.sh` 与 `go install` 不隐式安装 Skill，首次安装必须显式执行本命令。

**用法**：

```text
conduct init [--agent <id,...>] [--project | --global] [--copy] [--yes | -y] [--json]
```

**参数**：无位置参数。

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--agent <id,...>` | 可重复字符串列表 | 交互选择；`--yes` 时为 Universal + 已检测目标 | 可重复或逗号分隔；显式指定时只安装列出的 Agent，不额外添加 Universal；合法 ID 见下表 |
| `--project` | 布尔 | 交互默认 Project；`--yes` 默认 Project | 使用当前物理 cwd 作为项目根，不查找 git root，不向父目录搜索；与 `--global` 互斥 |
| `--global` | 布尔 | `false` | 使用用户主目录及 Agent 的用户级目录映射；与 `--project` 互斥 |
| `--copy` | 布尔 | `false` | 强制每个不同实际目标目录完整复制；不创建 Agent 目标软链 |
| `-y, --yes` | 布尔 | `false` | 跳过选择与确认，授权计划中的完整覆盖；多个不同目录默认 Symlink |
| `--json` | 布尔 | `false` | stdout 输出 JSON，不混入人读计划或交互文字；必须同时指定 `--yes` |

**交互与选择**：

- 普通 TTY 使用终端表单，顺序为 AI 编程工具多选 → 当前目录（默认）/当前用户 → 必要时选择方式（默认符号链接，可选独立副本）→ 路径与覆盖汇总 → 一次确认。显式 flags 固定对应选择，不重复询问。交互标题为「选择 AI 编程工具」，默认包含的工具在「默认包含」提示框列出；目录统一留到安装计划展示。工具列表与标题、说明分别计算高度：终端空间足够时展示全部选项，空间不足时可滚动选择；窗口缩放后重新计算，放大后恢复显示。
- 交互采用连贯的步骤呈现：以 `conduct Skill` 开始，默认工具与安装汇总以独立提示框展示；当前步骤以 `◆` 标记，每步完成后以 `◇` 回显已选内容。多选使用 `◼`/`◻` 区分选中与未选，单选与确认明确标示当前选择。完成、取消或安装失败均有收尾提示。框体与文字按终端宽度换行；以上人读提示均支持中英文。`--yes` 不显示交互装饰，`--json --yes` 仍只输出原有 JSON。
- 未显式指定 `--agent` 时，共享 `.agents/skills` 的 Universal 组固定选中；其他已检测目标预选。检测仅检查路径存在性，不能证明安装或加载 Agent 成功；不存在或中间路径不是目录均视为未检测到，其他读取错误显式上抛。
- 所选 Agent 的实际目标目录去重后只有一个目录时，直接完整复制，跳过方式选择；多个目录才需要选择 Symlink/Copy。 选择方式前只解析所选目标以判断是否需要询问，不先按默认 Symlink 校验中央目录或生成完整计划；方式确定后才生成、校验并展示最终计划。Copy 不受未使用的中央目录异常影响；所选目标自身异常仍报错。
- 非终端或 `--json` 模式必须有 `--yes`，缺失时在写入前报用法错误，绝不提示或等待输入。未知 Agent、矛盾选项也在写入前报错。
- 在确认前按 Ctrl+C / Ctrl+D 取消或拒绝最终确认：退出 `0`，不写安装文件和安装记录；`TERM=dumb` 也沿用相同的表单、双语提示与取消语义。

**Agent 目录与检测**：Skill 目标注册表独立于 workflow 引擎注册表，Agent ID 不等同于可执行 workflow 的引擎 ID。表中目录均为 Skill 根；最终安装目标为其下的 `conduct`。`<project>` 为当前物理 cwd，`<home>` 为用户主目录。

| Agent ID | 名称 | Project Skill 根 | Global Skill 根 | 检测目录 |
| --- | --- | --- | --- | --- |
| `codex` | Codex | `<project>/.agents/skills` | `<home>/.agents/skills` | 解析后的 `CODEX_HOME`，或 `/etc/codex` |
| `cursor` | Cursor | `<project>/.agents/skills` | `<home>/.agents/skills` | `<home>/.cursor` |
| `claude-code` | Claude Code | `<project>/.claude/skills` | `<claude-config>/skills` | `<claude-config>` |
| `opencode` | OpenCode | `<project>/.agents/skills` | `<home>/.agents/skills` | `<config>/opencode` |
| `gemini-cli` | Gemini CLI | `<project>/.agents/skills` | `<home>/.agents/skills` | `<home>/.gemini` |
| `github-copilot` | GitHub Copilot | `<project>/.agents/skills` | `<home>/.agents/skills` | `<home>/.copilot` |
| `cline` | Cline | `<project>/.agents/skills` | `<home>/.agents/skills` | `<home>/.cline` |
| `windsurf` | Windsurf | `<project>/.windsurf/skills` | `<home>/.codeium/windsurf/skills` | `<home>/.codeium/windsurf` |
| `qwen-code` | Qwen Code | `<project>/.qwen/skills` | `<home>/.qwen/skills` | `<home>/.qwen` |
| `kiro-cli` | Kiro CLI | `<project>/.kiro/skills` | `<home>/.kiro/skills` | `<home>/.kiro` |
| `roo` | Roo Code | `<project>/.roo/skills` | `<home>/.roo/skills` | `<home>/.roo` |

环境变量先去除首尾空白：`<claude-config>` 使用非空 `CLAUDE_CONFIG_DIR`，默认 `<home>/.claude`；Codex 检测使用非空 `CODEX_HOME`，默认 `<home>/.codex`。这两个变量为相对路径时相对于当前 cwd 解析；`CODEX_HOME` 只影响检测，不改变 Universal 安装目录。`<config>` 仅在非空 `XDG_CONFIG_HOME` 为绝对路径时使用该值，否则为 `<home>/.config`。Universal 包含 `codex`、`cursor`、`opencode`、`gemini-cli`、`github-copilot`、`cline`，它们共享同一实际安装目录。Agent 映射与检测逻辑沿用 MIT 许可来源，来源归属及适用许可证保存在 `internal/skillinstall/UPSTREAM-LICENSE.md`。

**资源与覆盖**：

- 唯一资源源码为 `internal/skillinstall/assets/conduct/`，包含 `SKILL.md`、MIT 许可证 `LICENSE`、`scripts/workflow_help.py` 与 `scripts/run_help.py`。`internal/skillinstall/bundle.go` 用 `go:embed` 内嵌完整发布资源，过滤 `__pycache__` / `.pyc` 等非发布文件。直接 `go build` / `go install` 即包含资源，不依赖 make 预生成；`.agents/skills/conduct` 是指向唯一源码的仓库相对软链。
- 先生成只读安装计划，汇总实际目标、方式。范围与方式显示本地化名称（当前目录/当前用户、符号链接/独立副本），JSON 枚举仍保持原值。每个目标路径仅列一次，已有目标在同一行标注「已存在」；仅存在已有目标时显示一次覆盖风险提示。最后以「安装 conduct Skill？」及「安装」/「取消」确认，或由 `--yes` 授权执行。正确软链保留。父目录软链与路径别名按实际路径去重；源/目标或目标之间的危险重叠在清理前拒绝，安装记录不能成为任意目录删除指令。检查涵盖其他已登记范围的实际目标，祖先/后代嵌套或同一路径的目录/链接规则冲突在任何安装写入前拒绝。
- Symlink：先把完整资源复制到该范围稳定的 `.agents/skills/conduct`，再为非通用目标创建指向它的相对软链；不链接到可执行文件、源码构建目录或临时目录。Copy：每个不同实际目标目录分别完整复制；仅解析所选目标，不因未使用的中央目录异常而失败。所选目标自身异常时仍报错。
- 已有普通 `conduct` 目录清理重建，覆盖本地修改并清除旧版残留文件。正确指向中央副本的软链保留；错误/失效软链只替换链接本身，不递归删除旧指向内容。清理仅限计划中的 conduct Skill 目标，不删除 skills 父目录或其他 Skill。
- 失败返回非零并如实报告已发生的部分更改；不隐式降级为 Copy，也不声称回滚。具体文件系统技术诊断固定英文，产品提示按统一 locale 规则中英双语。

**安装记录**：集中保存在 `~/.conduct/skill-installations.json`，不向项目写 `.conduct` 元数据。外层结构：

```json
{
  "schemaVersion": 1,
  "installations": [
    {
      "scope": "project",
      "projectPath": "/projects/example",
      "conductVersion": "0.2.0",
      "agents": ["codex", "claude-code"],
      "method": "symlink",
      "targets": [
        {"path": ".agents/skills/conduct", "kind": "directory"},
        {"path": ".claude/skills/conduct", "kind": "symlink", "target": ".agents/skills/conduct"}
      ]
    }
  ]
}
```

| 字段 | 类型 / 必填 | 约束 |
| --- | --- | --- |
| `schemaVersion` | 整数，必填 | 当前仅支持 `1` |
| `installations` | 数组，必填 | 同一 scope/projectPath 可保留多组安装，以实际目标集合区分；相同目标集合不重复登记 |
| `scope` | 字符串，必填 | `project` 或 `global` |
| `projectPath` | 字符串，仅 `project` 必填 | 项目绝对物理路径；`global` 不保存此字段 |
| `conductVersion` | 字符串，必填 | 成功同步完整 Skill 所使用的 CLI 版本 |
| `agents` | 字符串数组，必填 | 此组安装原先选择的合法 Agent ID；目标部分被重装覆盖时保留原选择，实际更新范围以 `targets` 为准 |
| `method` | 字符串，必填 | `copy` 或 `symlink`；单实际目标使用 `copy` |
| `targets` | 数组，必填 | 去重后的实际目标；Symlink 模式包含中央完整副本 |
| `targets[].path` | 字符串，必填 | 相对于 `projectPath` 或 `<home>` 的路径 |
| `targets[].kind` | 字符串，必填 | `directory` 或 `symlink` |
| `targets[].target` | 字符串，仅 `symlink` 必填 | 中央完整副本相对于同一根的路径，不是传给 `symlink` 的原始相对字符串 |

同一 scope/projectPath 重装只替换本次实际覆盖目标的登记；未覆盖目标保留原路径、安装方式与成功版本，并继续参与后续 `update`。本次安装计划仍只包含本次选择的目标，不额外改写旧副本。旧软链仍依赖的中央副本保留登记；旧组只剩普通目录时记为 Copy。保留目标与新目标发生嵌套或目录/链接冲突时，在写入前拒绝。相同目标集合合并登记，重复执行相同安装不累积条目；其他范围的记录保留。只有对应安装同步成功后才更新成功版本。记录原子写回，并保护并发 read-modify-write，避免条目丢失。非法 JSON、不支持 schema 或非法记录在破坏性写入前失败，不静默当作空记录覆盖。

**输出与退出**：人读模式显示计划与覆盖路径，确认成功后报告安装结果；取消明确报告取消并退出 `0`。`--json --yes` 成功时仅向 stdout 输出安装结果 JSON；用法错误退出 `2`，安装、路径安全、记录或输出写入错误退出 `1`。安装成功后 JSON 输出失败仍返回错误，已发生的安装不回滚。JSON 字段与枚举不翻译，产品文字遵循〈全局约定〉。

JSON 安装结果字段：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `scope` | 字符串 | `project` 或 `global` |
| `projectPath` | 字符串，仅项目级输出 | 绝对物理项目根 |
| `method` | 字符串 | `copy` 或 `symlink` |
| `agents` | 字符串数组 | 本次安装的 Agent ID |
| `conductVersion` | 字符串 | 提供内嵌资源的 CLI 版本 |
| `recordPath` | 字符串 | 集中安装记录的绝对路径 |
| `targets` | 数组 | 本次安装目标列表 |
| `targets[].path` | 字符串 | 目标的绝对路径 |
| `targets[].kind` | 字符串 | `directory` 或 `symlink` |
| `targets[].target` | 字符串，仅软链输出 | 中央完整副本的绝对路径 |
| `targets[].agents` | 字符串数组，可省略 | 共用该目标的 Agent ID |
| `targets[].exists` | 布尔 | 生成计划时目标是否已经存在 |

JSON 结果的绝对路径便于调用者直接定位文件；集中安装记录的 `targets` 则按前述 schema 保存相对路径。

**示例**：

```bash
conduct init                                      # TTY 交互安装
conduct init --agent codex,claude-code --project -y # 只安装显式目标
conduct init --agent claude-code --global --yes    # 用户级安装
conduct init --agent codex --agent qwen-code --copy --yes
conduct init --yes --json                          # 非交互调用
```

---

## update — 更新 CLI 与已登记 Skill（工具层）

**用途**：检查/更新 CLI 一次，再同步所选范围内已登记的 conduct Skill。把当前 conduct 可执行文件自更新到最新（或指定）版本——从项目 GitHub Releases 下载匹配本机 `GOOS`/`GOARCH` 的预编译二进制，校验 `checksums.txt` 后**原地替换**正在运行的二进制。更新机制镜像分发机制（见〈设计前提〉），**不重新编译、不需要本机装 Go**。

**用法**：

```
conduct update [版本] [--check] [--pre] [--project | --global]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<版本>` | 否 | 目标版本 tag（如 `v0.2.0`）；省略则取最新正式版。显式版本可命中预发布版本，是 opt-in beta 的入口 |

**选项**：

| 选项 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--check` | 布尔 | `false` | 只比对当前版本与发布并打印结论；不升级 CLI、不写 Skill、不创建元数据文件或记录锁 |
| `--pre` | 布尔 | `false` | 把预发布（`-beta` / `-rc` 等）版本纳入「最新」候选（不加则只认正式版） |
| `--project` | 布尔 | `false` | 只同步与当前物理 cwd 精确匹配的项目记录，不向父级查找；与 `--global` 互斥，仅限定 Skill 同步 |
| `--global` | 布尔 | `false` | 只同步用户级记录；与 `--project` 互斥，仅限定 Skill 同步 |

**输出**：

- 有更新且未加 `--check`：stdout 打印 `正在更新 conduct：<旧> → <新> …`，下载校验替换成功后打印 `conduct 已更新至 <新>。` 及发布说明链接，然后报告 Skill 同步结果。CLI 更新与所选 Skill 同步均无错误才退出 `0`。
- CLI 已是最新：stdout 提示 `conduct 已是最新版本 <版本>。`，仍同步所选 Skill，不提前返回。
- `--check`：只打印「已是最新 / 有新版本 / 当前版本非规范无法比较」三态之一；退出 `0`。
- 尚无任何 Release、或指定版本不存在 / 无匹配本机架构的资产：stderr 明确报错（fail-loud，不静默无动作）；退出 `1`。
- checksum 校验不过 / 下载失败 / 写入失败：stderr 报原因；退出 `1`。底层技术诊断固定英文。
- CLI 与 Skill 分别报告成功/失败；Skill 同步失败返回 `1`，不把 CLI 成功描述成全部成功，也不更新失败项的成功版本。人读提示统一使用「更新」：成功为 `Skill 已更新：<范围或项目>（<版本>）`，失败为 `Skill 更新失败：<范围或项目>`；缺失项目为 `项目目录不存在，跳过 Skill 更新：<项目>`。
- 经 Homebrew 安装（可执行文件落在 brew 前缀下）：拒绝自替换，stderr 提示改用 `brew upgrade conduct`（不与包管理器打架）；退出 `1`。

**Skill 同步范围与动作**：

- 默认从 `~/.conduct/skill-installations.json` 读取全部项目级与用户级记录，包括重装时保留的旧目标，与执行命令的 cwd 无关；不扫描磁盘寻找项目。`--project` 才精确筛选当前物理 cwd 的全部安装组，`--global` 选全部用户级安装组；无论何种范围，CLI 都只检查/更新一次。
- 沿用记录中的 Agent、实际目标和 Copy/Symlink 方式，不重新检测或为其他 Agent 增装。使用〈init — 安装 Agent Skill〉的完整覆盖规则：普通目录清理重建，正确软链保留，错误/失效链接只替换本身。
- 全部 Agent、Project/Global 范围的配置或逻辑父目录别名变化时仍沿用记录中的物理目标，不用当前映射重选目的地；若当前配置已不能证明目标来源，已有普通目录必须能识别为 conduct Skill（`SKILL.md` 声明 `name: conduct`，且两份 helper 为普通文件），才允许覆盖。身份也已丢失的目录安全报错，不把记录解释为删除普通目录的授权；记录中的物理父路径本身发生重定向时拒绝该项。
- 项目根目录已不存在时跳过并报告，不根据记录重建项目。没有安装记录或范围无匹配时提示「未找到通过 `conduct init` 安装的 Skill，已跳过。」；不猜测旧手工副本，不自动登记。
- CLI 已是最新时用当前二进制内嵌资源同步。CLI 已替换时，由安装后的新二进制通过内部入口执行 Skill 同步，使用新版内嵌资源与真实版本；旧进程不使用旧资源安装，不再次检查更新，也不递归执行公开 `update` 命令。范围原样传递给新二进制。
- 每项成功后才更新该项记录版本；失败项及其他未选择条目保留。非法 JSON、schema 或记录结构使整次同步在写入前失败；跨记录危险重叠也在写入前拒绝。单项运行时路径验证或安装失败则记录该项失败并继续其他所选记录，不阻止无关范围的 init/update，最终返回错误；路径诊断标出 scope/projectPath。CLI 升级成功后 Skill 失败不会撤销 CLI 替换，输出明确区分两步结果；stdout 显示简短状态，错误详情由 stderr 报出。
- `--check` 完全只读，始终不执行 Skill 同步；与范围选项一起使用也不创建记录、目录或记录锁。

**版本比较的诚实边界**：当前版本为 `dev` / git 伪版本 / dirty 等**非规范 semver** 时无法可靠比大小——此时 `--check` 如实说明「无法比较」并列出最新发布版本，绝不假装能算「落后几个版本」。有正式 tag 后比较即精确。

**示例**：

```bash
conduct update                 # 更新到最新正式版并同步全部已登记 Skill
conduct update --project       # CLI 更新一次；只同步当前物理 cwd 的项目记录
conduct update --global        # CLI 更新一次；只同步用户级记录
conduct update --check         # 只看有没有新版
conduct update v0.2.0          # 装指定版本（显式版本号可 opt-in 预发布）
conduct update --pre           # 更新到最新版本，纳入 beta
```

---

## help — 跨命令长文档（支撑）

**用途**：输出**跨命令的长文档**——教程 / 概念 / 最佳实践，按概念组织（如 `prompts` 讲 `promptTemplate` 机制，`run-prompts` 讲任务提示词怎么写清楚）。这类内容不塞进各命令的 `--help`（`--help` 只做精简速查），改由本命令承载，对标 `go help <topic>`。文档 `go:embed` 进二进制随发布走（`docs/` 不随 `go install` 发布，故必须内嵌），相关命令的 `--help` 末尾留一行指针指向对应主题。

**用法**：

```
conduct help [命令路径 | 主题]
```

**参数**：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `<命令路径 | 主题>` | 否 | 主题名（如 `prompts` / `run-prompts`），或一到多级命令路径（如 `workflow node set`）。省略时打印完整根帮助（含命令列表与帮助主题） |

**输出**：

- 给定已知主题：stdout 打印该主题的内嵌长文档；退出 `0`。
- 给定命令路径：stdout 打印该命令的用法与选项；支持多级路径；退出 `0`。
- 不给参数：stdout 打印完整根帮助，内容含顶层命令列表与全部帮助主题；退出 `0`。
- 未知主题或命令路径：stderr 报「未知帮助主题 <x>」并列出可用主题、提示从根帮助查命令；退出 `2`（用法错误）。

**已内置主题**：

| 主题 | 内容 |
| --- | --- |
| `prompts` | 如何写好节点 `promptTemplate`（模板变量、祖先产物串联、并行分支写盘冲突） |
| `run-prompts` | 如何写任务提示词（目标与原因、已知与排除项、周边背景，以及查询与调查的不同交代方式） |

**示例**：

```bash
conduct help            # 打印完整根帮助（含命令与主题）
conduct help prompts    # 读「怎么写好提示词」
conduct help run-prompts  # 读「怎么写任务提示词」
conduct help workflow node set  # 查看多级命令用法
```

---

## 退出码约定

| 码 | 含义 |
| --- | --- |
| `0` | 成功；`init` 取消；`update` 缺失项目跳过或范围内无安装记录（明确报告） |
| `1` | 一般错误：无可用 Release、下载 / 校验 / 写入失败、端口被占、store 不可读、Homebrew 拒绝自替换、Skill 安装/同步失败、路径安全或安装记录无效等 |
| `2` | 用法错误（未知 help 主题、未知 Agent、互斥选项、非终端/JSON 安装未加 `--yes`、非法参数）——Cobra 默认 |

（编辑态 / 运行时命令的退出码见 [cli-authoring.md](./cli-authoring.md) 与 [cli-runtime.md](./cli-runtime.md)，同一张表。）

## 实现状态（诚实标注）

本规格是**目标命令面**，与当前代码差距如下：

| 命令 | 状态 |
| --- | --- |
| `version` | **已实现**（构建期 `ldflags` 注入版本；根命令 `--version` 等价） |
| `ui` | **已实现**（`conduct ui --port/--open`、服务端已注册 API、self-exec 发射器与内嵌 SPA、light / dark 主题均已落地；只绑 127.0.0.1、启动探测 store、Host/Origin 白名单、变更类强制 JSON。`ui.md` 明确延后的 node / edge 粒度端点不在当前已实现面，编辑器通过整体 `PUT` 保存；浏览器交互按 `docs/test-cases/ui-frontend.md` 验收） |
| `init` | **已实现**（`internal/cli/init.go` 与 `internal/skillinstall/`：终端表单、显式/默认 Agent、Project/Global、Copy/Symlink、完整覆盖与集中记录；单一资产目录直接嵌入二进制，仓库发现入口为相对软链） |
| `update` | **已实现**（`internal/cli/update.go`：经 `creativeprojects/go-selfupdate` 从 GitHub Releases 下载匹配架构资产、`checksums.txt` 校验、原地替换；`--check` / `--pre` / 显式版本；Homebrew 前缀拒绝自替换；非规范当前版本跳过比较；升级后由新二进制同步全部/限定范围内已登记 Skill，CLI 已最新也同步，`--check` 不写入。资产命名与 `.goreleaser.yaml` 对齐，改一处须同步另一处） |
| `help` | **已实现**（命令 + `internal/help` 内嵌 `go:embed` 落地；`--help`、命令路径帮助与 `prompts` / `run-prompts` 两个内嵌主题已按 `settings.json` > `LC_ALL` > `LC_MESSAGES` > `LANG` > 英文的统一规则支持中英文；主题可继续按概念扩充） |

`ui` 的实现边界与延后端点以 [ui.md](./ui.md)〈配套实现状态〉为准；浏览器验收步骤维护在 [ui-frontend.md](../test-cases/ui-frontend.md)，不在规格中记录某次测试执行状态。
