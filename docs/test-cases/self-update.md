# 自更新测试用例

覆盖 CLI 自更新、已登记 Skill 同步与 `install.sh`。对应规格：[cli-tooling.md](../specs/cli-tooling.md)〈update — 更新 CLI 与已登记 Skill（工具层）〉。

## 执行环境

在仓库根运行下列命令，需要 Go 和 Python 3。执行器 [scripts/self-update.py](scripts/self-update.py) 为每次验收构建临时二进制，创建临时 HOME 和项目，并重定向 XDG、Codex、Claude 配置目录。执行器不改变调用 shell 的环境，成功、失败或中断后均清理临时目录。

`update` 会更新全部已登记 Skill，因此仅复制二进制不足以隔离。执行器在临时 HOME 中登记临时项目；执行前后核对真实 HOME 下 conduct workflows、runs、安装记录与 settings 的内容及元数据，发现差异即失败并列出路径。Go 构建仍使用原有工具链与缓存。

网络用例需要可访问 GitHub Releases；查询、认证、限流、下载或校验失败均按实际错误报告，不能算验收通过。输出中的版本、临时路径和耗时不逐字比对。

## conduct update

### TC-001 update 帮助（离线）

- **目的**：帮助包含 Skill 同步范围选项。
- **前置**：无网络要求。
- **步骤**：`python3 docs/test-cases/scripts/self-update.py help`
- **预期**：退出 `0`；帮助包含 `--check`、`--pre`、`--project`、`--global`；最后打印 `PASS: help; real conduct store unchanged`，临时 HOME 不产生 `.conduct`。
- **清理**：执行器自动清理临时目录。

### TC-002 请求不存在的版本

- **目的**：查询失败时明确报错，不替换二进制或同步 Skill。
- **前置**：需要访问 GitHub API，不要求已有 Release；`v99.99.99` 不存在。
- **步骤**：`python3 docs/test-cases/scripts/self-update.py missing`
- **预期**：被测 `update v99.99.99` 退出 `1` 且诊断非空，二进制校验和不变，临时 HOME 不产生 `.conduct`；执行器退出 `0` 并打印 `PASS: missing; real conduct store unchanged`。若只是网络失败，本用例仅证明失败无写入，不能证明已查明版本不存在。
- **清理**：执行器自动清理临时目录。

### TC-003 --check 完全只读

- **目的**：检查新版本时不替换二进制、不创建安装记录或锁。
- **前置**：已有匹配本机系统与架构的正式 Release，版本高于 `0.0.0`。
- **步骤**：`python3 docs/test-cases/scripts/self-update.py check`
- **预期**：执行器以 `0.0.0` 构建临时二进制；被测命令退出 `0` 并报告有新版本，二进制校验和不变，临时 HOME 不产生 `.conduct`；执行器退出 `0` 并打印 `PASS: check; real conduct store unchanged`。
- **清理**：执行器自动清理临时目录。

### TC-004 CLI 更新后同步临时 Skill

- **目的**：验证下载、checksum 校验、二进制替换及新版 Skill 同步。
- **前置**：最新正式 Release 匹配本机系统与架构，版本高于 `0.0.0`，且包含 `conduct init` 与升级后的 Skill 同步能力。旧版 Release 不满足此前置；不得把「CLI 已替换但内部同步入口不存在」当作成功。
- **步骤**：`python3 docs/test-cases/scripts/self-update.py upgrade`
- **预期**：执行器以 `0.0.0` 构建临时二进制，使用真实 `init --agent codex,claude-code --copy --yes` 在临时项目安装两个副本，再向两者添加过期标记；被测 `update` 退出 `0`，报告 CLI 更新和 Skill 同步成功。临时二进制版本不再是 `0.0.0`，两个副本的过期标记均删除，`SKILL.md`、`LICENSE` 和两份 helper 均存在；执行器退出 `0` 并打印 `PASS: upgrade; real conduct store unchanged`。
- **清理**：执行器自动清理临时二进制、HOME、项目及安装记录。

版本比较、Homebrew 拒绝、已最新仍同步、范围筛选、失败保留成功版本、新二进制资源交接及中英文交互由 `internal/cli/update_skills_test.go`、`internal/cli/skill_binary_test.go` 和 `internal/skillinstall/*_test.go` 覆盖。此处真实 Release 用例不替代这些确定性回归。

## install.sh

### TC-006 安装脚本语法（离线）

- **目的**：验证 POSIX shell 语法。
- **前置**：无网络要求。
- **步骤**：`sh -n install.sh`
- **预期**：退出 `0`，无 stderr。
- **清理**：无文件写入。

### TC-007 安装到临时目录

- **目的**：验证探测架构、下载、checksum 校验和指定安装目录。
- **前置**：已有匹配本机系统与架构的正式 Release。
- **步骤**：`python3 docs/test-cases/scripts/self-update.py install`
- **预期**：安装脚本退出 `0`，目标位于本次临时目录；安装后的 `conduct version` 退出 `0`；执行器退出 `0` 并打印 `PASS: install; real conduct store unchanged`。
- **清理**：执行器自动清理安装目录及临时 HOME，不修改真实 PATH 或全局安装。
