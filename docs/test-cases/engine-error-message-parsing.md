# 引擎失败态错误信息解析 测试用例

覆盖 `claude-code` / `qoder` / `antigravity` 三引擎在**失败态**下如何把各自 CLI 的输出解析成可读错误信息：qoder 的 `errors` 数组优先级、claude-code 非零退出时的 stdout JSON 兜底解析、antigravity 的 `error` 字段优先于 `response` 摘要。跑工作流与查运行记录的通用面见 [workflow-running.md](./workflow-running.md)（其 TC-010 已覆盖「引擎二进制整体不可用」这一最外层故障，及非零退出+空 stdout 的 fallback 路径，本文不重复，只补三引擎**输出内容本身报告业务失败**时的解析细节）。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈逐引擎详述〉〈错误与退出行为〉。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的目标行为，用来验证实现对不对。截至编写时三处修复均**已实装**，预期可直接对照验证；命令若偏离本文〈预期〉，即为实现未达标。

> **可选外部兼容性测试**：本文 TC-001 / TC-006 / TC-009 均会真实调用 AI 引擎、依赖当前登录态并可能消耗 token，不属于日常确定性回归。仅在需要确认当前第三方 CLI 版本仍保持既有失败输出协议时执行；请勿扩大 payload 或反复重试。

> **用户视角、不伪造内部数据**：所有用例都通过 `conduct workflow run` 触发被测代码，断言 `run.json` 的 `error` 字段等对外可见结果；绝不手写 `run.json` / `trace.jsonl` 去「摆拍」记录。TC-009 因 conduct 当前不暴露 `agy --add-dir`，使用一个只注入该参数的 wrapper 调真实 `agy`；wrapper 不合成 stdout/stderr。

> **隔离机制**：真实引擎登录态通常绑定真实家目录，因此这些可选用例使用唯一 workflow 名并在结束后精确删除对应 workflow/run；引擎工作目录另放在临时目录。执行前应确认当前真实 Conduct store 可写并已有备份。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，改 HOME / PATH / cwd 后仍可用
```

额外需要：真实家目录已安装并登录对应无头 CLI（`qodercli`、`claude`、`agy`）。真实 CLI 未装 / 未登录时，用例报引擎调用失败，属环境未就绪。

可选用例的精确清理助手（zsh / bash 均可用；删除本用例 workflow 与该 workflow 的全部 run 记录）：

```bash
cleanup_run() {   # 用法：cleanup_run <workflow 名>
  rm -f "$HOME/.conduct/workflows/$1.json"
  if [ -d "$HOME/.conduct/runs" ]; then
    find "$HOME/.conduct/runs" -maxdepth 1 -type d -name "$1-*" -exec rm -rf {} +
  fi
}
```

各用例复用的最小单 agent 节点工作流助手（均含保留标记节点 START/END——`create --definition` 的导入体须自带二者）：

```bash
make_qoder_flow() {   # 用法：make_qoder_flow <name>
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"qoder","promptTemplate":"{{sys.userPrompt}}"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' \
    | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
make_claude_flow() {  # 用法：make_claude_flow <name>
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"{{sys.userPrompt}}"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' \
    | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
make_agy_flow() {     # 用法：make_agy_flow <name>
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"antigravity","promptTemplate":"test"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' \
    | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
```

断言助手：用 `run.json` 的 `error` 字段核对最终报错信息（`record.Error` 与 `trace.jsonl` 该步 `error` 均为引擎返回的错误原文，未做二次包装）：

```bash
run_error() {   # 用法：run_error <workflow 名前缀>   —— 打印该 run 的 status 与 error
  python3 -c 'import json,glob,os,sys; p=glob.glob(os.path.expanduser("~/.conduct/runs/"+sys.argv[1]+"-*/run.json"))[0]; d=json.load(open(p)); print(d["status"], "|", d["error"])' "$1"
}
```

---

## 功能覆盖清单（动笔前规划）

对着 spec〈逐引擎详述〉〈错误与退出行为〉的失败态解析优先级逐项映射到用例，避免只测 happy path：

**qoder（`is_error=true` 分支，`qoderFailureMessage` 的三级优先级）**
- **真实路径**：真实 `qodercli` 在超大低重复 payload 下返回 `is_error=true`，`result` 字段不存在，失败原因在 `errors` 数组；conduct 应取 `errors` 内容而非空 `result` / 兜底提示 → TC-001 💸。
- **确定性边界**：`errors` 为空时回退 trim 后的 `result`、两者皆空时使用固定英文兜底、失败态保留真实耗时，均由 `internal/engine/exec_test.go` 覆盖。

**claude-code（`claudeStdoutFailureMessage` 的非零退出 stdout 兜底 + 既有 `is_error` 分支）**
- **真实路径（非零退出）**：真实 `claude -p` 收到 10 MB 以下但超过模型上下文的 payload 时，退出码非 0，stdout 是合法 JSON 且 `result` 非空，stderr 为空；conduct 应优先取 stdout 的 `result`（`claude error: <result>`），不落到退出码摘要路径 → TC-006 💸。
- **确定性边界**：退出码 0 的 `is_error`、非法 JSON、合法 JSON 但空 `result`、空 stdout，以及 stderr 500 字截断，由 `internal/engine/exec_test.go` 和 [workflow-running.md](./workflow-running.md) TC-010 覆盖。

**antigravity（`status != "SUCCESS"` 分支的字段优先级）**
- **真实路径**：真实 `agy` 在不存在的 `--add-dir` 下返回 `status:"ERROR"`，顶层 `error` 字段是简洁原因，`response` 同时含模型长文；conduct 应优先用 `error`，不采用 `response` 摘要 → TC-009 💸。
- **确定性边界**：`error` 为空时回退 `response`，以及 response 500 字截断，由 `internal/engine/exec_test.go` 覆盖。

**特性叠加**：三引擎的失败态解析各自独立、互不影响其它 workflow 字段（`Model`/`Effort` 映射等）——三者的参数映射已由 [exec_test.go](../../internal/engine/exec_test.go) 的 `TestClaudeCodeRunParsesAndPlumbs` / `TestQoderRunParsesAndPlumbs` / `TestAntigravityRunUsesArgAndDir` 覆盖，本文聚焦失败态解析本身，不重复参数接线断言。

---

## 可选外部兼容性测试

### TC-001 💸 qoder 真实 payload 超限时使用 errors 数组内容

- **目的**：验证真实 `qodercli` 返回 `is_error=true` 且失败原因位于 `errors` 数组时，conduct 的 `workflow run` 把 `errors` 内容写入 run 错误；不因 `result` 字段缺失而落到空字符串或兜底提示。
- **前置**：
  1. 真实家目录的 `qodercli` 已装并登录。
  2. `PROJ=$(mktemp -d)`；`NAME="tc001-qoder-errors-$(date +%Y%m%d%H%M%S)-$$"`；`cleanup_run "$NAME"`。
  3. `make_qoder_flow "$NAME"`。
  4. 造一个足够大且低重复度的 payload（重复单字符可能被真实 qoder 接受，不足以稳定触发超限）：
     ```bash
     python3 - <<'PY' > "$PROJ/large.txt"
     for i in range(120000):
         print(f'{i:09d} payload limit validation text with varied digits {i*37:018d} {i*7919:018d}')
     PY
     wc -c "$PROJ/large.txt"   # 约 11 MB
     ```
- **步骤**：
  1. `cat "$PROJ/large.txt" | LC_ALL=C "$CONDUCT" workflow run "$NAME" --cwd "$PROJ"; echo "exit=$?"`
  2. `run_error "$NAME"`
  3. ```bash
     python3 - "$NAME" <<'PY'
     import json, glob, os, sys
     p = glob.glob(os.path.expanduser("~/.conduct/runs/"+sys.argv[1]+"-*/run.json"))[0]
     err = json.load(open(p))["error"]
     uses_errors = any(s in err for s in ["PAYLOAD_TOO_LARGE", "prompt is too long", "Model context window exceeded"])
     print("uses_errors=", uses_errors)
     fallbacks = ["未返回具体错误信息", "returned no specific error information"]
     print("not_empty_fallback=", all(s not in err for s in fallbacks))
     PY
     ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | qodercli error: ...`；错误文本含真实 qoder 的 `errors` 内容关键字（如 `PAYLOAD_TOO_LARGE` / `prompt is too long` / `Model context window exceeded`，具体 request_id、token 数和供应商措辞不逐字比对）。
  - 步骤 3 打印 `uses_errors= True` 与 `not_empty_fallback= True`——证明没有把缺失的 `result` 当空报错，也没有落到中英文任一空错误兜底。
- **清理**：`cleanup_run "$NAME"; rm -rf "$PROJ"`。

### TC-006 💸 claude-code 真实非零退出 + stdout 含合法 result

- **目的**：验证真实 `claude -p` 应用层失败（prompt 过长）场景——退出码非 0，stdout 是合法 JSON 且 `result` 非空、stderr 为空——conduct 报错优先取 stdout 的 `result`；不落到退出码摘要路径。
- **前置**：
  1. 真实家目录的 `claude` CLI 已装并登录。
  2. `PROJ=$(mktemp -d)`；`NAME="tc006-claude-stdout-$(date +%Y%m%d%H%M%S)-$$"`；`cleanup_run "$NAME"`。
  3. `make_claude_flow "$NAME"`。
  4. 造一个低于 CLI stdin 10 MB 限制、但足以超过模型上下文的 payload：
     ```bash
     python3 - <<'PY' > "$PROJ/large.txt"
     for i in range(55000):
         print(f'{i:09d} claude model context validation text with varied digits {i*37:018d} {i*7919:018d}')
     PY
     wc -c "$PROJ/large.txt"   # 约 5.7 MB
     ```
- **步骤**：
  1. `cat "$PROJ/large.txt" | "$CONDUCT" workflow run "$NAME" --cwd "$PROJ"; echo "exit=$?"`
  2. `run_error "$NAME"`
  3. ```bash
     python3 - "$NAME" <<'PY'
     import json, glob, os, sys
     p = glob.glob(os.path.expanduser("~/.conduct/runs/"+sys.argv[1]+"-*/run.json"))[0]
     err = json.load(open(p))["error"]
     print("result_used=", "Prompt is too long" in err)
     print("no_exit_summary=", "exited with code" not in err)
     PY
     ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | claude error: Prompt is too long` 或同义真实文案；不含 `exited with code` 字样。
  - 步骤 3 打印 `result_used= True` 与 `no_exit_summary= True`——证明 stdout JSON 的 `result` 被采用，没有回退到 `claude exited with code 1: ...`。
- **清理**：`cleanup_run "$NAME"; rm -rf "$PROJ"`。

### TC-009 💸 antigravity 真实 error 字段优先于 response 长文摘要

- **目的**：验证真实 `agy` 返回 `status!="SUCCESS"` 且顶层 `error` 与长 `response` 同时存在时，conduct 优先用 `error` 字段（引擎给出的简洁失败原因），不采用 `response` 里的模型叙述。
- **前置**：
  1. 真实家目录的 `agy` CLI 已装并登录。
  2. `PROJ=$(mktemp -d)`；`NAME="tc009-agy-error-$(date +%Y%m%d%H%M%S)-$$"`；`cleanup_run "$NAME"`。
  3. `make_agy_flow "$NAME"`。
  4. conduct 当前不暴露 `agy --add-dir`，因此用一个参数注入 wrapper 触发真实 agy 的可控错误；wrapper 不生成任何 JSON，只把调用转交给真实 `agy`：
     ```bash
     REAL_AGY=$(command -v agy)
     OLD_PATH="$PATH"
     mkdir -p "$PROJ/agybin"
     cat > "$PROJ/agybin/agy" <<SH
     #!/usr/bin/env bash
     exec "$REAL_AGY" --add-dir /this/path/definitely/does/not/exist/xyz999 "\$@"
     SH
     chmod +x "$PROJ/agybin/agy"
     export PATH="$PROJ/agybin:$PATH"
     ```
- **步骤**：
  1. `"$CONDUCT" workflow run "$NAME" "test" --cwd "$PROJ"; echo "exit=$?"`
  2. `run_error "$NAME"`
  3. ```bash
     python3 - "$NAME" <<'PY'
     import json, glob, os, sys
     p = glob.glob(os.path.expanduser("~/.conduct/runs/"+sys.argv[1]+"-*/run.json"))[0]
     err = json.load(open(p))["error"]
     print("error_field_used=", "Cannot list directory file:///this/path/definitely/does/not/exist/xyz999" in err)
     print("response_not_used=", all(s not in err for s in ["Summary of Work", "Gemini Execution Protocol", "Analysis"]))
     PY
     ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | agy status ERROR: Cannot list directory file:///this/path/definitely/does/not/exist/xyz999 which does not exist.`（路径与 `does not exist` 是关键结构；不逐字比对 response 长文）。
  - 步骤 3 打印 `error_field_used= True` 与 `response_not_used= True`——证明顶层 `error` 被采用，未把 `response` 的长篇分析当报错信息。
- **清理**：`export PATH="$OLD_PATH"; cleanup_run "$NAME"; rm -rf "$PROJ"`。

## 交给单测的行为

以下无法通过真实 CLI 稳定制造的字段组合与精确边界交由单测覆盖，本文不再用假引擎重复执行完整 workflow：

- **qoder 优先级**：`TestQoderRunIsErrorEmptyResultUsesErrorsArray`、`TestQoderRunIsErrorEmptyErrorsUsesTrimmedResult`、`TestQoderEmptyFailureIsAlwaysEnglish` 覆盖 `errors`、`result`、固定兜底三级优先级；`TestQoderRunIsErrorPreservesDuration` 覆盖失败耗时。
- **claude-code 回退**：`TestClaudeCodeRunNonZeroExitStdoutResult`、`TestClaudeCodeRunNonZeroExitStdoutNotJSON`、`TestClaudeCodeRunNonZeroExitStdoutEmptyResultUsesStderr` 覆盖 stdout JSON 成功提取及两种回退。
- **antigravity 优先级**：`TestAntigravityRunErrorFieldPreferredOverResponse`、`TestAntigravityRunNonSuccessStatus`、`TestAntigravityRunTruncatesResponseFallback` 覆盖 `error` 优先、`response` 回退及截断。
- **通用 stderr 截断**：`TestCommandErrorTruncatesStderr` 精确覆盖前 500 个字符加省略号的契约。
