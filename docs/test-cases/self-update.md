# 自更新 测试用例

覆盖 `conduct update`（从 GitHub Releases 下载预编译二进制、校验 checksum、原地替换）与一行安装脚本 `install.sh`。对应 spec：[docs/specs/cli-commands.md](../specs/cli-commands.md)〈update — 自更新〉；发布流水线见 [AGENTS.md](../../AGENTS.md)〈发布〉。

> **预期以 spec 为准。** 本文描述 spec 规定的目标行为，用来验证实现对不对。当前实现（见 spec〈实现状态〉）：`update` 已实装（`internal/cli/update.go`，经 `creativeprojects/go-selfupdate`）。

> **隔离机制（关键，与其它篇不同）**：`conduct update` 会**原地替换正在运行的可执行文件**。若直接对 `bin/conduct` 或已安装的全局 `conduct` 跑，成功更新就会**覆盖掉被测二进制本身**。因此本篇所有会真正触发下载替换的用例，一律先把二进制**拷成一次性副本**（`$UPD/conduct`）、只对副本跑 `update`——被替换的是副本，用后连目录删除，绝不动 `bin/` 或真实安装。不涉及 store（`~/.conduct/`），故无需重定向 `HOME`。

> **网络与发布前置（诚实标注）**：真正的下载/校验/替换需要项目 GitHub Releases 存在匹配本机 `GOOS`/`GOARCH` 的资产。据此把用例分两类：
> - **不依赖 Release**（TC-001 / TC-002 / TC-006）：纯本地或「查不到」路径，随时可跑、零网络依赖。
> - **依赖公开 Release**（TC-003 / TC-004 / TC-007）：前置要求 `qoggy/conduct` 已是公开仓且至少有一个正式 Release（首个为 `v0.0.1`）。首发上线后即可无人值守回归。
> 未认证访问 GitHub API 有 60 次/小时限流；跑依赖 Release 的用例前可 `export GITHUB_TOKEN=<token>` 提额，避免 403 假失败（403 与真错误都表现为退 `1` + stderr，见 TC-002 说明）。

> **brew 拒绝分支的覆盖边界（诚实标注）**：「经 Homebrew 安装则拒绝自更新」这条判断，其逻辑（可执行文件路径是否落在 `/opt/homebrew/` 等前缀下）由 Go 单测 `TestHomebrewPrefixOf` 覆盖。**不做黑盒 e2e**——它要求被测二进制真实位于 `/opt/homebrew/...` 这类系统前缀下，而测试不得往系统目录写入、也无法在临时目录伪造该绝对前缀。这是绝对路径判断的固有边界，非「偷懒不测」：逻辑已自动化，只是替换动作本身不在黑盒层复现。

## 环境准备（每篇跑一次）

在仓库根执行：

```bash
make build
CONDUCT="$PWD/bin/conduct"        # 只读用途：拷副本、看帮助；不对它本身跑会触发替换的 update
PKG="github.com/qoggy/conduct/internal/cli"

# 造一次性副本（会被 update 替换的就是它）
mkupd() {                          # 用法：mkupd [版本号]  —— 建 $UPD/conduct，可选注入旧版本号
  UPD="$(mktemp -d)"
  if [ -n "${1:-}" ]; then
    go build -ldflags "-X $PKG.version=$1" -o "$UPD/conduct" ./cmd/conduct
  else
    cp "$CONDUCT" "$UPD/conduct"
  fi
}
# 用后：rm -rf "$UPD"
```

---

## conduct update

### TC-001 update --help 打印用法（离线）

- **目的**：帮助文本自解释，覆盖参数与选项，退 `0`。
- **前置**：无（离线）。
- **步骤**：
  1. `"$CONDUCT" update --help; echo "exit=$?"`
- **预期**：exit=0；stdout 含 `conduct update`、`--check`、`--pre`，且提示「显式版本号可安装预发布」「Homebrew 改用包管理器」等要点。

### TC-002 请求不存在的版本 → fail-loud 不静默不挂起（不依赖 Release）

- **目的**：验证「查不到目标」时**显式报错退 `1`**、不静默退 `0`、不挂起等待。用一个**永不存在**的版本号，使结论与「当前是否已有 Release」无关，可确定性回归。
- **前置**：`mkupd`（拷副本）。
- **步骤**：
  1. `"$UPD/conduct" update v99.99.99 >/tmp/u2.out 2>&1; echo "exit=$?"`
  2. `cat /tmp/u2.out`
- **预期**：exit=1；stderr 有明确错误（「未找到版本 …」或查询失败原因），**非空**；命令**秒回**不挂起；`$UPD/conduct` 未被替换（`"$UPD/conduct" version` 仍打印副本原版本）。
- **清理**：`rm -rf "$UPD" /tmp/u2.out`

### TC-003 update --check 对真实 Release 只报告不安装（依赖公开 Release）

- **目的**：`--check` 只比对并打印结论，**不下载、不替换**。
- **前置**：`qoggy/conduct` 公开且已有正式 Release（≥ `v0.0.1`）；`mkupd 0.0.0`（副本盖成旧版 `0.0.0`，确保「有新版本」分支）。
- **步骤**：
  1. `sum_before=$(shasum -a 256 "$UPD/conduct" | awk '{print $1}')`
  2. `"$UPD/conduct" update --check >/tmp/u3.out 2>&1; echo "exit=$?"`
  3. `cat /tmp/u3.out`
  4. `sum_after=$(shasum -a 256 "$UPD/conduct" | awk '{print $1}'); [ "$sum_before" = "$sum_after" ] && echo "UNCHANGED" || echo "CHANGED"`
- **预期**：exit=0；stdout 说明「有新版本：当前 0.0.0 → 可更新到 <最新>」；步骤 4 打印 `UNCHANGED`（`--check` 绝不动二进制）。
- **清理**：`rm -rf "$UPD" /tmp/u3.out`

### TC-004 端到端更新替换一次性副本（依赖公开 Release）

- **目的**：验证真链路——下载匹配架构资产、checksum 校验、**原地替换**，替换后副本报告新版本。用 ldflags 把副本盖成 `0.0.0`（旧于任何真实 Release），强制真正触发一次更新。
- **前置**：同 TC-003 的公开 Release 前置；`mkupd 0.0.0`。
- **步骤**：
  1. `"$UPD/conduct" version`   # 预期 `conduct 0.0.0`
  2. `"$UPD/conduct" update >/tmp/u4.out 2>&1; echo "exit=$?"`
  3. `cat /tmp/u4.out`
  4. `"$UPD/conduct" version`
- **预期**：步骤 1 打印 `conduct 0.0.0`；步骤 2 exit=0，stdout 有「正在更新：0.0.0 → <新> …」「已更新到 <新>（安装于 $UPD/conduct）」；步骤 4 打印的版本 = 最新 Release 版本（不再是 0.0.0），证明二进制已被替换。`bin/conduct` 与真实安装**分毫未动**（只动了 `$UPD` 里的副本）。
- **清理**：`rm -rf "$UPD" /tmp/u4.out`

---

## install.sh（一行安装脚本）

### TC-006 install.sh 语法自检（离线）

- **目的**：脚本 POSIX 语法合法，可被 `sh` 解析。
- **前置**：无。
- **步骤**：
  1. `sh -n install.sh; echo "exit=$?"`
- **预期**：exit=0，无 stderr。

### TC-007 install.sh 端到端装入隔离目录（依赖公开 Release）

- **目的**：脚本能探测系统/架构、下载匹配资产、校验 checksum、把 `conduct` 装进指定目录，装出的二进制可跑。
- **前置**：公开 Release（≥ `v0.0.1`）；`DEST="$(mktemp -d)"`。
- **步骤**：
  1. `CONDUCT_INSTALL_DIR="$DEST" sh install.sh >/tmp/u7.out 2>&1; echo "exit=$?"`
  2. `cat /tmp/u7.out`
  3. `"$DEST/conduct" version`
- **预期**：exit=0；stdout 报「已安装 conduct <tag> → $DEST/conduct」；步骤 3 打印对应版本号。校验失败/下载失败时脚本 fail-loud 退非 `0`（不静默装出坏二进制）。
- **清理**：`rm -rf "$DEST" /tmp/u7.out`
- **说明**：脚本默认装到 `/usr/local/bin`（不可写则 `~/.local/bin`）；本用例用 `CONDUCT_INSTALL_DIR` 定向到临时目录隔离，不污染真实 PATH。
