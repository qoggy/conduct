# 引擎失败态错误信息解析 测试用例

覆盖 `claude-code` / `qoder` / `antigravity` 三引擎在**失败态**下如何把各自 CLI 的输出解析成可读错误信息：qoder 的 `errors` 数组优先级、claude-code 非零退出时的 stdout JSON 兜底解析、antigravity 的 `error` 字段优先于 `response` 摘要。跑工作流与查运行记录的通用面见 [workflow-running.md](./workflow-running.md)（其 TC-010 已覆盖「引擎二进制整体不可用」这一最外层故障，及非零退出+空 stdout 的 fallback 路径，本文不重复，只补三引擎**输出内容本身报告业务失败**时的解析细节）。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈逐引擎详述〉〈错误与退出行为〉。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的目标行为，用来验证实现对不对。截至编写时三处修复均**已实装**，预期可直接对照验证；命令若偏离本文〈预期〉，即为实现未达标。

> **💸 费钱标记**：带 💸 的用例会真实调用 AI 引擎，可能消耗 token。本文仅 TC-001 / TC-006 / TC-009 真调引擎，因为这些失败形态已用真实 CLI 验证可控复现；请勿把这些用例改成更大任务或反复重试。其余用例测的是精确 fallback 边界（字段恰好为空、stdout 恰好非法 JSON、stderr/response 恰好 600 字等），真实引擎无法稳定按需产出，故逐条在〈前置〉说明为何使用测试替身。

> **用户视角、不伪造内部数据**：所有用例都通过 `conduct workflow run` 触发被测代码，断言 `run.json` 的 `error` 字段等对外可见结果；绝不手写 `run.json` / `trace.jsonl` 去「摆拍」记录。假二进制仅用于外部引擎 CLI 的不可控精确边界，run 记录仍由 conduct 自己的真实运行链路产出。TC-009 因 conduct 当前不暴露 `agy --add-dir`，使用一个只注入 `--add-dir` 参数的 wrapper 调真实 `agy`；该 wrapper 不合成 stdout/stderr，不属于伪造引擎输出。

> **隔离机制（两套）**：conduct 的 store 固定在 `~/.conduct/`。
> - **非 💸 用例**：把 `HOME` 重定向到临时目录，假引擎装进 `$WORK/fakebin` 并前置到 `PATH`。用例结束连同临时目录一并删除、恢复 `HOME` 与 `PATH`。
> - **💸 用例**：要调真实引擎，而引擎登录态通常绑定真实家目录（macOS 上 `claude` 的 OAuth 凭据在系统 Keychain，不随临时 `HOME` 走），故在真实家目录运行；靠「唯一 workflow 名 + 用后精确删除」隔离，不残留。引擎读写文件另用临时 `--cwd` 目录隔离。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，改 HOME / PATH / cwd 后仍可用
```

💸 用例额外需要：真实家目录已安装并登录对应无头 CLI（`qodercli`、`claude`、`agy`）。真实 CLI 未装 / 未登录时，💸 用例应报引擎调用失败，属环境未就绪。

💸 用例的精确清理助手（zsh / bash 均可用；删除本用例 workflow 与该 workflow 的全部 run 记录）：

```bash
cleanup_run() {   # 用法：cleanup_run <workflow 名>
  rm -f "$HOME/.conduct/workflows/$1.json"
  if [ -d "$HOME/.conduct/runs" ]; then
    find "$HOME/.conduct/runs" -maxdepth 1 -type d -name "$1-*" -exec rm -rf {} +
  fi
}
```

非 💸 用例的隔离环境助手：

```bash
fake_env() {
  WORK=$(mktemp -d)
  OLD_HOME="$HOME"; export HOME="$WORK"
  OLD_PATH="$PATH"; mkdir -p "$WORK/fakebin"; export PATH="$WORK/fakebin:$PATH"
}
cleanup_fake() {
  export HOME="$OLD_HOME"
  export PATH="$OLD_PATH"
  rm -rf "$WORK"
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
- **边界（次优先级）**：`errors` 为空、`result` 非空 → 回退用 `result`（`TrimSpace` 后）→ TC-002（假 qodercli；真实 qoder 无法稳定按需产出这个精确字段组合）。
- **边界（兜底）**：`errors` 与 `result` 皆空 → 固定英文兜底提示 `qodercli returned no specific error information`，绝不吐空字符串；引擎技术诊断不随 locale 国际化 → TC-003（假 qodercli；真实 qoder 无法稳定按需产出空错误体）。
- **数据流转**：失败态的 `RunResult.DurationMilliseconds` 不再被空字面量 `RunResult{}` 丢弃，真实耗时如实进入 `trace.jsonl` 的 `durationMs` → TC-004（假 qodercli；用受控 `sleep` 锁定耗时边界）。

**claude-code（`claudeStdoutFailureMessage` 的非零退出 stdout 兜底 + 既有 `is_error` 分支）**
- **正常路径（is_error，退出码 0）**：`is_error=true` → 附 `result` 文本 → TC-005（假 claude；已调研但未找到稳定真实触发，详见用例前置）。
- **真实路径（非零退出）**：真实 `claude -p` 收到 10 MB 以下但超过模型上下文的 payload 时，退出码非 0，stdout 是合法 JSON 且 `result` 非空，stderr 为空；conduct 应优先取 stdout 的 `result`（`claude error: <result>`），不落到退出码摘要路径 → TC-006 💸。
- **边界（stdout 非法 JSON）**：退出码非 0，stdout 非空但不是合法 JSON → 回退退出码+stderr 摘要 → TC-007（假 claude；真实 claude 无法稳定输出非法 JSON）。
- **边界（stdout 合法但 result 为空）**：退出码非 0，stdout 是合法 JSON 但 `result` 为空串 → 同样回退退出码+stderr 摘要，不用空字符串冒充报错原因 → TC-008（假 claude；真实 claude 无法稳定输出空 result）。
- 退出码非 0、stdout **整个为空**的通用回退路径已由 [workflow-running.md](./workflow-running.md) TC-010 覆盖（引擎二进制整体故障、stderr 有内容），本文不重复。

**antigravity（`status != "SUCCESS"` 分支的字段优先级）**
- **真实路径**：真实 `agy` 在不存在的 `--add-dir` 下返回 `status:"ERROR"`，顶层 `error` 字段是简洁原因，`response` 同时含模型长文；conduct 应优先用 `error`，不采用 `response` 摘要 → TC-009 💸。
- **边界（回退）**：`error` 字段为空 → 回退用 `response`（本用例 `response` 较短、未触及截断边界）→ TC-010（假 agy；真实 agy 无法稳定按需省略 error 且给固定 response）。

**commandError / antigravity response 的 500 字截断边界**
- **边界**：`commandError` 对 stderr 摘要按 500 字截断（超出加 `…`）；非零退出+stderr 超长 → TC-011（假 claude；真实 stderr 无法稳定控制到 600 字）。
- **边界**：antigravity `error` 为空、回退用的 `response` 摘要同样按 500 字截断；`response` 超长 → TC-012（假 agy；真实 response 无法稳定控制到 600 字）。

**特性叠加**：三引擎的失败态解析各自独立、互不影响其它 workflow 字段（`Model`/`Effort` 映射等）——三者的参数映射已由 [exec_test.go](../../internal/engine/exec_test.go) 的 `TestClaudeCodeRunParsesAndPlumbs` / `TestQoderRunParsesAndPlumbs` / `TestAntigravityRunUsesArgAndDir` 覆盖，本文聚焦失败态解析本身，不重复参数接线断言。

---

## qoder：`is_error` 失败信息优先级

### TC-001 💸 真实 payload 超限时使用 errors 数组内容

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

### TC-002 errors 为空时回退用 result（trim 后）

- **目的**：验证 `errors` 数组为空（字段缺省）时回退取 `result`，且经 `TrimSpace`。
- **前置**：
  1. 使用假 `qodercli` 的具体理由：真实 qoder 无法稳定按需返回「`is_error=true`、`errors` 缺省、`result` 恰好为指定文本且带首尾空格」这个精确字段组合；本例只测 fallback 边界，不测真实 API 可达性。
  2. `fake_env`。
  3. 装假 `qodercli`：
     ```bash
     cat > "$WORK/fakebin/qodercli" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"is_error":true,"result":"  fallback result text  "}'
     SH
     chmod +x "$WORK/fakebin/qodercli"
     ```
  4. `make_qoder_flow qe2`。
- **步骤**：
  1. `"$CONDUCT" workflow run qe2 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error qe2`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | qodercli error: fallback result text`（`errors` 空时回退 `result`，且已去掉首尾空格）。
- **清理**：`cleanup_fake`。

### TC-003 errors 与 result 皆空时给兜底提示，不吐空字符串

- **目的**：验证 `errors`、`result` 都为空时，即使中文 locale 也使用固定英文非空兜底提示，而不是拼出一句没有内容的错误前缀。
- **前置**：
  1. 使用假 `qodercli` 的具体理由：真实 qoder 的失败通常会给 `errors` 或 `result`，无法稳定按需产出「错误体为空」这个精确兜底边界。
  2. `fake_env`。
  3. 装假 `qodercli`：
     ```bash
     cat > "$WORK/fakebin/qodercli" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"is_error":true,"session_id":"s1"}'
     SH
     chmod +x "$WORK/fakebin/qodercli"
     ```
  4. `make_qoder_flow qe3`。
- **步骤**：
  1. `LC_ALL=zh_CN.UTF-8 "$CONDUCT" workflow run qe3 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error qe3`
- **预期**：
  - 步骤 1 退出码为 `1`。
  - 步骤 2 打印 `failed | qodercli error: qodercli returned no specific error information`（中文 locale 不改变技术诊断，且兜底信息非空）。
- **清理**：`cleanup_fake`。

### TC-004 失败态真实耗时如实落盘（不再被清零）

- **目的**：验证 `is_error=true` 的失败路径不再用空字面量 `RunResult{}` 丢弃已算好的耗时——用一个可观测的 `sleep` 制造非零耗时，确认失败步的 `trace.jsonl` 仍带上它。
- **前置**：
  1. 使用假 `qodercli` 的具体理由：真实 qoder 的网络/API 耗时不可控，且难以把失败原因、耗时边界和成本都固定；本例只需一个可重复的失败态耗时信号。
  2. `fake_env`。
  3. 装假 `qodercli`（先 sleep 0.2s 再报失败）：
     ```bash
     cat > "$WORK/fakebin/qodercli" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     sleep 0.2
     echo '{"is_error":true,"errors":["boom"]}'
     SH
     chmod +x "$WORK/fakebin/qodercli"
     ```
  4. `make_qoder_flow qe4`。
- **步骤**：
  1. `"$CONDUCT" workflow run qe4 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; d=glob.glob(os.path.expanduser("~/.conduct/runs/qe4-*"))[0]; t=[json.loads(l) for l in open(d+"/trace.jsonl") if l.strip()]; print("success=", t[0]["success"], "durationMs_positive=", t[0]["durationMs"] > 0)'`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `success= False durationMs_positive= True`（失败步的 `durationMs` 反映真实耗时，非 0）。
- **清理**：`cleanup_fake`。

---

## claude-code：`is_error` 与非零退出的失败信息解析

### TC-005 is_error=true（退出码 0）附 result 文本

- **目的**：验证进程正常退出（退出码 0）但业务失败（`is_error=true`）时，报错附 `result` 文本。补齐完整覆盖矩阵（此分支非本轮改动，但与 TC-006~008 的非零退出分支共同构成 claude-code 完整的失败态解析行为）。
- **前置**：
  1. 使用假 `claude` 的具体理由：已调研真实 `claude` 的可控触发条件，未找到稳定复现「exit 0 + stdout JSON `is_error=true`」的方式。实测拒绝类 prompt 返回 `exit 0` 但 `is_error:false`；工具受限/禁用工具场景也返回 `is_error:false`；坏模型和超长 prompt 会返回 `is_error:true` 但进程退出码为 `1`，走 TC-006 的非零退出分支。继续真调会变成输出不确定且花费不可控。
  2. `fake_env`。
  3. 装假 `claude`：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"result":"model said no","is_error":true,"usage":{"input_tokens":1,"output_tokens":1}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  4. `make_claude_flow ce1`。
- **步骤**：
  1. `"$CONDUCT" workflow run ce1 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error ce1`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | claude error: model said no`。
- **清理**：`cleanup_fake`。

### TC-006 💸 真实非零退出 + stdout 含合法 result：优先于退出码摘要

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

### TC-007 非零退出 + stdout 非法 JSON：回退退出码+stderr 摘要

- **目的**：验证 stdout 非空但不是合法 JSON（杂散文本）时，无法从中取报错原因，回退到退出码+stderr 摘要路径。
- **前置**：
  1. 使用假 `claude` 的具体理由：真实 `claude -p --output-format json` 的 stdout 要么是合法 JSON，要么在 CLI 层失败时走 stderr；无法稳定按需输出「非零退出 + stdout 非法 JSON + 指定 stderr」。
  2. `fake_env`。
  3. 装假 `claude`：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo "this is not json at all, just garbage output"
     echo "real stderr message" >&2
     exit 1
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  4. `make_claude_flow ce3`。
- **步骤**：
  1. `"$CONDUCT" workflow run ce3 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error ce3`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | claude exited with code 1: real stderr message`（stdout 非法 JSON，回退到退出码+stderr 摘要；不含 `claude error:` 前缀）。
- **清理**：`cleanup_fake`。

### TC-008 非零退出 + stdout 合法 JSON 但 result 为空：同样回退，不冒充报错原因

- **目的**：验证 stdout 是合法 JSON、但 `result` 字段为空串这一边界——`claudeStdoutFailureMessage` 不应把空字符串当作有效报错原因，同样回退到退出码+stderr 摘要。
- **前置**：
  1. 使用假 `claude` 的具体理由：真实 `claude` 无法稳定按需输出「非零退出 + 合法 JSON + 空 `result` + 指定 stderr」这个精确字段组合。
  2. `fake_env`。
  3. 装假 `claude`：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"is_error":true,"result":"","session_id":"s1"}'
     echo "stderr fallback message" >&2
     exit 1
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  4. `make_claude_flow ce4`。
- **步骤**：
  1. `"$CONDUCT" workflow run ce4 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error ce4`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | claude exited with code 1: stderr fallback message`（`result` 为空串不被采用，回退到退出码+stderr 摘要；不含 `claude error:` 前缀）。
- **清理**：`cleanup_fake`。

---

## antigravity：`status != "SUCCESS"` 的字段优先级

### TC-009 💸 真实 error 字段优先于 response 长文摘要

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

### TC-010 error 字段为空时回退用 response 摘要

- **目的**：验证 `error` 字段为空（缺省）时，回退用 `response` 作报错摘要（本用例 `response` 较短、未触及 500 字截断边界——截断边界另见 TC-012）。
- **前置**：
  1. 使用假 `agy` 的具体理由：真实 agy 的失败通常会填顶层 `error`，无法稳定按需产出「`status` 失败、`error` 缺省、`response` 恰好为指定短文本」这个精确 fallback 组合。
  2. `fake_env`。
  3. 装假 `agy`：
     ```bash
     cat > "$WORK/fakebin/agy" <<'SH'
     #!/usr/bin/env bash
     echo '{"status":"FAILED","response":"quota exceeded for this session"}'
     SH
     chmod +x "$WORK/fakebin/agy"
     ```
  4. `make_agy_flow ae2`。
- **步骤**：
  1. `"$CONDUCT" workflow run ae2 "go" --cwd "$WORK"; echo "exit=$?"`
  2. `run_error ae2`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `failed | agy status FAILED: quota exceeded for this session`（`error` 为空，回退用 `response`）。
- **清理**：`cleanup_fake`。

---

## commandError / antigravity response 的 500 字截断边界

### TC-011 非零退出 + stderr 超 500 字：commandError 按 500 字截断并加省略号

- **目的**：验证 `commandError` 对 stderr 摘要按 rune 截断到 500 字、超出部分以 `…` 收尾（spec [engines.md](../specs/engines.md)〈错误与退出行为〉「非零退出码」条）。用 claude-code 触发（stdout 为空，直接落到 `commandError` 路径，与三引擎共用同一截断逻辑）。
- **前置**：
  1. 使用假 `claude` 的具体理由：真实 CLI 的 stderr 长度和内容不可稳定控制到恰好 600 个字符；本例只测 conduct 的 500 字截断边界。
  2. `fake_env`。
  3. 装假 `claude`：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     python3 -c "print('A'*600, end='')" >&2
     exit 1
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  4. `make_claude_flow ce5`。
- **步骤**：
  1. `"$CONDUCT" workflow run ce5 "go" --cwd "$WORK"; echo "exit=$?"`
  2. ```bash
     python3 -c '
     import json, glob, os
     p = glob.glob(os.path.expanduser("~/.conduct/runs/ce5-*/run.json"))[0]
     d = json.load(open(p))
     expected = "claude exited with code 1: " + ("A" * 500) + "…"
     print("match=", d["error"] == expected, "| len=", len(d["error"]))
     '
     ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `match= True`（stderr 恰好截断到前 500 个 `A`，末尾加 `…`，多出的 100 个 `A` 未出现）。
- **清理**：`cleanup_fake`。

### TC-012 antigravity response 超 500 字：回退摘要同样按 500 字截断

- **目的**：验证 `error` 字段为空、回退用的 `response` 摘要同样按 500 字截断加 `…`（spec [engines.md](../specs/engines.md)〈antigravity〉「输出解析」条）。
- **前置**：
  1. 使用假 `agy` 的具体理由：真实 agy 的模型 response 内容、长度与是否同时带 `error` 都不可稳定控制；本例只测 600 字 response 经 conduct 截断成 500 字加省略号的精确边界。
  2. `fake_env`。
  3. 装假 `agy`：
     ```bash
     cat > "$WORK/fakebin/agy" <<'SH'
     #!/usr/bin/env bash
     python3 -c "import json; print(json.dumps({'status':'FAILED','response':'B'*600}))"
     SH
     chmod +x "$WORK/fakebin/agy"
     ```
  4. `make_agy_flow ae3`。
- **步骤**：
  1. `"$CONDUCT" workflow run ae3 "go" --cwd "$WORK"; echo "exit=$?"`
  2. ```bash
     python3 -c '
     import json, glob, os
     p = glob.glob(os.path.expanduser("~/.conduct/runs/ae3-*/run.json"))[0]
     d = json.load(open(p))
     expected = "agy status FAILED: " + ("B" * 500) + "…"
     print("match=", d["error"] == expected, "| len=", len(d["error"]))
     '
     ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `match= True`（`response` 恰好截断到前 500 个 `B`，末尾加 `…`）。
- **清理**：`cleanup_fake`。

---

## 交给单测的行为

以下行为在手工黑盒层成本高或难以稳定精确断言，交由单测覆盖或在单测层补足：

- **qoder `errors` 多条拼接与逐条 trim**：真实 qoder 的 payload 超限通常只返回一条真实错误，无法稳定产出多条带首尾空格的 `errors`。本文 TC-001 用真实引擎覆盖「errors 数组优先于空 result / 兜底」；多条拼接与 trim 属纯解析格式边界，应放在引擎级单测。
- **`qoderFailureMessage` / `claudeStdoutFailureMessage` 的更细字段组合**：`internal/engine/exec_test.go` 的 `TestQoderRunIsErrorEmptyResultUsesErrorsArray`、`TestClaudeCodeRunNonZeroExitStdoutResult`、`TestClaudeCodeRunNonZeroExitStdoutNotJSON`、`TestAntigravityRunErrorFieldPreferredOverResponse` 用假二进制驱动 `Run()`（而非直接调用内部函数）断言其可观察结果；本文从 `workflow run` 外部验证 run.json 落盘后果，二者是互补覆盖。
- **`TestQoderRunIsErrorPreservesDuration`**：单测直接断言 `RunResult.DurationMilliseconds` 字段值；本文 TC-004 从外部验证其落盘后果（`trace.jsonl` 的 `durationMs`），二者互补。
- **`commandError` / antigravity response 的 500 字截断边界**：本文 TC-011（stderr）、TC-012（antigravity response）已在手工层用测试替身覆盖；截断阈值 500 与省略号收尾这一精确边界不要求真实引擎复现。
