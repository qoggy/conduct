# conduct ui 前端主题测试用例

覆盖内嵌 SPA 的 light / dark 主题选择、切换、持久化与主要视觉令牌。对应 spec：[docs/specs/ui.md](../specs/ui.md)〈前端技术栈〉。

## 行为空间

| 类型 | 行为 | 覆盖 |
| --- | --- | --- |
| 正常路径 | settings 无显式主题时分别跟随系统 light / dark | TC-001 |
| 正常路径 / 数据流转 | 设置页自定义下拉切换主题，DOM 与 `settings.json` 同步变化 | TC-002 |
| 特性叠加 | 显式主题优先于系统偏好，刷新后保持 | TC-002 |
| 边界 | 主题缺失、两个有效值与非法值；语言/主题严格部分更新 | `internal/locale/settings_test.go`、`internal/ui/handlers_test.go`（由 `make test` 执行） |
| 特性叠加 | 设置页语言与主题互不覆盖，分别支持跟随项 | TC-003 |
| 视觉覆盖 | 页面、表面、边框、DAG 节点与语法高亮均取当前主题令牌，切换不改变布局 | TC-004 |

内嵌资产存在、主题脚本先于样式表加载、dark 令牌块存在及关键小字号文字对比度，由 `internal/ui/assets_test.go` 覆盖。

## 环境准备

在仓库根执行：

```bash
make build
test -x "$PWD/bin/conduct"
test -r "$PWD/docs/test-cases/atomic-conduct-test.sh"
test -r "$PWD/docs/test-cases/ui-theme-browser.mjs"
node -e 'if (Number(process.versions.node.split(".")[0]) < 22) process.exit(1)'
```

浏览器脚本只使用 Node.js 22+ 内置 API，通过 Chrome DevTools Protocol 驱动无头 Chrome，不需要 npm 安装。macOS 默认使用 `/Applications/Google Chrome.app`；其他安装位置通过 `CHROME_BIN=/absolute/path/to/chrome` 指定。

每条用例都是可单独复制执行的 `bash` 原子边界：先捕获真实 `HOME` 下 `.conduct/workflows`、`.conduct/runs` 的内容及元数据快照，再注册覆盖正常退出与 `HUP` / `INT` / `TERM` 的 `trap`，之后才重定向 `HOME`、启动随机端口 UI 和浏览器。任何断言失败也会停止进程、恢复环境、删除临时目录并比较真实 store 前后快照。

## 用例

### TC-001 首次访问跟随系统 light / dark

- **目的**：验证 settings 没有显式主题时，首屏直接使用系统主题且不闪烁。
- **前置**：已完成〈环境准备〉；脚本自行建立隔离环境。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  "$CONDUCT" ui --port 0 >"$WORK/ui.log" 2>&1 &
  UIPID=$!
  conduct_test_register_pid "$UIPID"
  B=""
  for _ in $(seq 1 100); do
    B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1 || true)
    [ -n "$B" ] && curl -fsS "$B/api/version" >/dev/null && break
    sleep 0.05
  done
  test -n "$B" || { cat "$WORK/ui.log"; exit 1; }
  node docs/test-cases/ui-theme-browser.mjs \
    --scenario follow-system --url "$B" --profile "$WORK/chrome"
  BASH
  ```

- **预期**：退出码 `0`并打印 `PASS: follow-system`；light 首屏断言根主题 `light`、body 背景 `rgb(247, 247, 245)`；dark 首屏对应 `dark`、`rgb(11, 16, 24)`；两页 API 均返回 `theme:null`，且无浏览器错误日志。
- **清理**：脚本 `trap` 自动关闭 UI 与 Chrome、恢复 `HOME/PATH`、删除临时目录并确认真实 store 前后零差异。

### TC-002 设置页切换主题并在刷新后保持

- **目的**：验证设置页自定义下拉把 light 切到 dark、刷新后保持，再切回 light，并把选择完整流转到 DOM 与全局 settings。
- **前置**：已完成〈环境准备〉；脚本自行建立隔离环境。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  "$CONDUCT" ui --port 0 >"$WORK/ui.log" 2>&1 &
  UIPID=$!
  conduct_test_register_pid "$UIPID"
  B=""
  for _ in $(seq 1 100); do
    B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1 || true)
    [ -n "$B" ] && curl -fsS "$B/api/version" >/dev/null && break
    sleep 0.05
  done
  test -n "$B" || { cat "$WORK/ui.log"; exit 1; }
  node docs/test-cases/ui-theme-browser.mjs \
    --scenario toggle --url "$B" --profile "$WORK/chrome"
  BASH
  ```

- **预期**：退出码 `0`并打印 `PASS: toggle`；第一次选择及刷新后均断言 `data-theme=dark`、dark 背景与 API `theme:"dark"`；第二次选择逐项断言 light 状态及 API `theme:"light"`，无浏览器错误日志。
- **清理**：脚本 `trap` 自动完成进程回收、环境恢复、临时目录删除与真实 store 前后比较。

### TC-003 语言与主题设置互不覆盖

- **目的**：验证设置页使用自定义下拉，语言与主题严格部分更新互不覆盖，并都能恢复跟随项。
- **前置**：已完成〈环境准备〉；脚本自行建立隔离环境。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  "$CONDUCT" ui --port 0 >"$WORK/ui.log" 2>&1 &
  UIPID=$!
  conduct_test_register_pid "$UIPID"
  B=""
  for _ in $(seq 1 100); do
    B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1 || true)
    [ -n "$B" ] && curl -fsS "$B/api/version" >/dev/null && break
    sleep 0.05
  done
  test -n "$B" || { cat "$WORK/ui.log"; exit 1; }
  node docs/test-cases/ui-theme-browser.mjs \
    --scenario settings --url "$B" --profile "$WORK/chrome"
  BASH
  ```

- **预期**：退出码 `0`并打印 `PASS: settings`；语言切到中文后设置页即时中文化，主题切到 dark 后两个字段同时保留；再切英文不覆盖 dark；主题恢复跟随系统后响应系统 light，且无浏览器错误日志。
- **清理**：脚本 `trap` 自动完成进程回收、环境恢复、临时目录删除与真实 store 前后比较。

### TC-004 dark 令牌覆盖编辑器、DAG 与代码高亮

- **目的**：验证主题不只改变页面背景，工作流编辑器中的表面、图节点、连边与代码高亮也使用 dark 令牌，且切换不引发布局位移。
- **前置**：已完成〈环境准备〉；脚本通过公开 API 在临时 store 创建工作流，不伪造内部存储。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  "$CONDUCT" ui --port 0 >"$WORK/ui.log" 2>&1 &
  UIPID=$!
  conduct_test_register_pid "$UIPID"
  B=""
  for _ in $(seq 1 100); do
    B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1 || true)
    [ -n "$B" ] && curl -fsS "$B/api/version" >/dev/null && break
    sleep 0.05
  done
  test -n "$B" || { cat "$WORK/ui.log"; exit 1; }
  node docs/test-cases/ui-theme-browser.mjs \
    --scenario visual --url "$B" --profile "$WORK/chrome" \
    --screenshot "$WORK/theme-dark.png"
  test -s "$WORK/theme-dark.png"
  BASH
  ```

- **预期**：退出码 `0`并打印 `PASS: visual`。脚本断言：公开 API 创建工作流返回 `201`；`.insp` 背景/边框为 `rgb(25, 36, 52)` / `rgb(39, 53, 72)`；`.nb` fill 为 `rgb(20, 28, 39)`；`.edge` stroke 为 `rgb(94, 113, 137)`；标题及占位符前景/背景分别为 `rgb(138, 171, 255)`、`rgb(233, 167, 124)`、`rgb(61, 47, 36)`；顶栏、画布、检查器和提示词编辑器的矩形在 light→dark 切换前后完全一致；截图文件存在且非空；无浏览器错误日志。
- **清理**：脚本 `trap` 先停止 UI/Chrome，再恢复环境、比较真实 store 前后快照并删除包含临时工作流、浏览器 profile 和截图的 `$WORK`。
