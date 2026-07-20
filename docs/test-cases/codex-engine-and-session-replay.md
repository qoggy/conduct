# codex 引擎 + 会话回放 id 测试用例

覆盖 **codex 引擎接入**（`codex exec` 的 JSONL 事件流解析、engineConfig 校验、默认写死参数）与四个结构化引擎的 **`RunResult.SessionID`（会话回放 id）**链路。Kiro 的 nullable metadata 与冒烟另见 [kiro-engine.md](./kiro-engine.md)。工作流定义的增删改查见 [workflow-editing.md](./workflow-editing.md)，跑工作流与查运行记录的通用面见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈codex〉〈引擎抽象〉〈schema 字段映射〉〈conduct 默认写死的参数〉〈引擎能力表〉、[docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈run show〉〈runs/ 落盘结构〉。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**，用来验证实现对不对——不是照现有代码反推。codex 引擎、四个结构化引擎的 session 回放与全部引擎的 nullable metadata 链路均已实装，预期可直接对照验证。

> **本文用例全部零 token**：校验类用例只做定义校验、不触引擎；端到端类用例**用确定性假引擎**（fake binary）顶替真 `codex` / `claude` / `qodercli` / `agy`。**引擎是 conduct 的外部依赖**，用假二进制顶替是复现「本机装了某引擎、它这样输出」的真实场景、也规避真实调用的登录态 / 网络 / 不确定产物 / 💸——这属外部依赖的测试替身（同 [workflow-running.md](./workflow-running.md) TC-007/TC-010 的做法），**不是伪造内部数据**：run.json / trace.jsonl 仍全由 conduct 真实的 `workflow run` 代码路径产出，本文只在最外层遮蔽引擎子进程。绝不手写 run.json / trace.jsonl 去「摆拍」记录。

> **隔离机制（关键）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。下面每个 TC 都是一个完整的 `bash` heredoc：它在同一 shell 内捕获真实 `HOME`，对真实 `~/.conduct/workflows` 与 `~/.conduct/runs` 做内容及元数据前快照，注册覆盖成功 / 失败 / 中断的 trap，再重定向 `HOME`、安装假引擎、执行与断言；trap 最后恢复环境、清理临时目录并做真实 store 后快照。任何真实 store 差异都会打印 diff 并令该 TC 失败。临时 store 残留数或执行器自报不能替代该比较。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
test -x "$PWD/bin/conduct"
test -r "$PWD/docs/test-cases/atomic-conduct-test.sh"
```

> **装机版不可用于验证本功能**：全局 `conduct`（0.0.1）尚无 codex，务必用上面 `make build` 产出的 `./bin/conduct`。

每条用例都在自身脚本块里 `source docs/test-cases/atomic-conduct-test.sh` 并调用 `conduct_test_setup`；不依赖本节或上一条 TC 留下的变量、函数、当前目录或后台进程。

---

## 功能覆盖清单（动笔前规划）

对着 spec 的行为空间逐项映射到用例，避免只测 happy path：

**codex 引擎**
- **校验（边界）**：codex 接受 `model` + 统一 `effort`（合法值 low/medium/high/xhigh）→ TC-001；`effort` 越界（含 `max`——qoder 认、codex 不认，专为区分两引擎的允许集）被拒 → TC-002；旧 `reasoningEffort` 的普通未知字段行为由 [workflow-editing.md](./workflow-editing.md) TC-034 覆盖。
- **默认写死参数 + 可变参数映射（数据流转）**：`exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -` 恒附加、`Model→--model`、`effort→-c model_reasoning_effort=<v>` → TC-003（断言 conduct 实际传给二进制的 argv）。
- **JSONL 正常数据流转**：thread.started→SessionID、agent_message→Text、turn.completed→Tokens → TC-003。取最后一条 agent_message、失败事件、坏 JSON、缺失 agent_message 与空文本等解析边界交由 `internal/engine/codex_test.go` 的确定性单测覆盖。

**RunResult.SessionID（会话回放 id）**
- **数据流转（引擎输出 → trace → TraceView）**：四个结构化引擎各自的会话 id 字段流到持久化 trace 的 `sessionId`，读取时再派生 `sessionReplayCommand`，供 `run show --trace` 与 `--json --trace` 展示——codex（`thread_id`）→ TC-003；claude-code（`session_id`）→ TC-009；qoder（`session_id`）→ TC-010；antigravity（`conversation_id`）→ TC-011。
- **回放命令逐引擎正确**：`claude -r`（TC-009）/ `codex resume`（TC-003）/ `qodercli -r`（TC-010）/ `agy --conversation`（TC-011）。
- **负路径**：引擎未回报会话 id 时 trace 明确写 `"sessionId":null`、`run show --trace` 不显会话行 → TC-012。

**交给单测 / 不可黑盒触发**（见〈交给单测的行为〉）：descriptor replay 函数 nil/空串、未知引擎、空 session id 不调用函数、shell quote、trace.jsonl 不含派生字段，以及失败节点的 `SessionID` 不入 trace。

---

## codex 引擎校验（编辑态，零 token）

### TC-001 codex 接受 model + effort（合法档位）

- **目的**：验证 codex 引擎的 engineConfig 判别联合接受 `model`（任意非空串）与 `effort`（合法档位），入库成功。
- **前置**：无；下方脚本自行建立隔离、注册 trap 并造 fixture。
- **步骤**：完整复制执行一个脚本块（定义须自带保留标记节点 START/END）：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  for pair in k1:low k2:medium k3:xhigh; do
    name=${pair%%:*}; effort=${pair#*:}
    printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"model":"gpt-5-codex","effort":"%s"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' "$effort" \
      | "$CONDUCT" workflow create "$name" --definition
    echo "exit=0"
  done
  "$CONDUCT" workflow show k1 --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["definition"]["nodes"][1]["engineConfig"];print("model=",n["model"],"re=",n["effort"])'
  BASH
  ```
- **预期**：
  - 步骤 1、2、3 退出码均 `0`（`effort ∈ {low,medium,high,xhigh}`，`low` / `medium` / `xhigh` 逐档均在集内；`high` 由 TC-003 覆盖；model 不做白名单，任意非空串放行）。
  - 步骤 4 打印 `model= gpt-5-codex re= low`（engineConfig 两字段原样入库；节点下标 `[1]` 因 `[0]` 是 START 标记节点）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

### TC-002 codex 校验：effort 越界被拒

- **目的**：验证 codex 的统一 `effort` 取值须落在 `{low,medium,high,xhigh}`，越界（含 qoder 才认的 `max`）被拒。
- **前置**：无；下方脚本自行建立隔离、注册 trap 并造 fixture。
- **步骤**：完整复制执行一个脚本块：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  expect_rejected() {
    local name="$1" field="$2" value="$3"
    set +e
    printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"%s":"%s"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' "$field" "$value" \
      | "$CONDUCT" workflow create "$name" --definition
    rc=$?
    set -e
    echo "exit=$rc"
    test "$rc" -eq 1
  }
  expect_rejected b1 effort insane
  expect_rejected b2 effort max
  "$CONDUCT" workflow list | grep -q 'store 为空'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[1].engineConfig.effort: "insane" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`（节点下标 `[1]` 因 `[0]` 是 START）。
  - 步骤 2 退出码 `1`，stderr 含 `"max" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`（`max` 是 qoder 的档位，codex 不认——两引擎允许集不同）。
  - 列表为空（`b1`/`b2` 均未落盘）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

---

## codex 端到端解析（确定性假 codex，零 token）

### TC-003 codex 正常路径：解析 Text/Tokens/SessionID + 默认与可变参数映射 + 回放展示

- **目的**：一条用例串起 codex 的核心正常链路——① JSONL 三事件正常解析（`thread.started`→SessionID、`agent_message`→Text、`turn.completed`→Tokens）；② conduct 传给 `codex` 二进制的 argv 含**默认写死参数**与 `Model`/`effort` 的**可变参数映射**；③ SessionID 数据流转：`thread_id` → trace 的 `sessionId` → `run show --trace` 附 codex 回放命令、`--json --trace` 经 trace 带出。
- **前置**：无；下方脚本自行建立隔离、注册 trap、安装假 codex 并创建 workflow。
- **步骤**：完整复制执行一个脚本块：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export CODEX_ARGS_FILE="$WORK/codex-args.txt" CODEX_STDIN_FILE="$WORK/codex-stdin.txt"
  cat > "$WORK/fakebin/codex.jsonl" <<'JSONL'
  {"type":"thread.started","thread_id":"th-CODEX-001"}
  {"type":"item.completed","item":{"type":"agent_message","text":"CODEX-ARTIFACT"}}
  {"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}
  JSONL
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat > "$CODEX_STDIN_FILE"
  printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
  cat "$WORK/fakebin/codex.jsonl"
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}","engineConfig":{"model":"gpt-5-codex","effort":"high"}},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' \
    | "$CONDUCT" workflow create cx --definition >/dev/null
  "$CONDUCT" workflow run cx "打个招呼" --cwd "$WORK"
  echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="cx"][0])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"]; print("output=",t[0]["output"],"tokens=",t[0]["tokens"],"sessionId=",t[0].get("sessionId"))'
  grep -q -- 'exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -' "$CODEX_ARGS_FILE" && echo "default_args_ok"
  grep -q -- '--model gpt-5-codex' "$CODEX_ARGS_FILE" && echo "model_ok"
  grep -q -- '-c model_reasoning_effort=high' "$CODEX_ARGS_FILE" && echo "effort_ok"
  grep -q '打个招呼' "$CODEX_STDIN_FILE" && echo "stdin_prompt_ok"
  "$CONDUCT" run show "$RID" --trace | grep '回放'
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"][0]; print("json_sessionId=",t.get("sessionId"),"replay=",t.get("sessionReplayCommand"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`；stdout 含 `▶ 调度 1 个节点`、`▶ say [打招呼] 开跑 · engine=codex` 与 `✓ say 完成`，完成行含 `tokens=24885`、`产物 14 字符：CODEX-ARTIFACT`（tokens = 24763+122；耗时不逐字比对）。
  - 步骤 3 打印 `output= CODEX-ARTIFACT tokens= 24885 sessionId= th-CODEX-001`（Text 取 agent_message、Tokens 取 turn.completed 的 input+output、SessionID 取 thread_id）。
  - 步骤 4 依次打印 `default_args_ok`、`model_ok`、`effort_ok`（三段 argv 均如实下传；`-` 哨兵在 PROMPT 位）。
  - 步骤 5 打印 `stdin_prompt_ok`（渲染后的 prompt「回复：hi。需求：打个招呼」经 stdin 下传，含用户输入 `打个招呼`——证明 prompt 走 stdin 而非 argv）。
  - 步骤 6 输出 `会话 th-CODEX-001 · 回放：codex resume th-CODEX-001`（codex 的回放命令）。
  - 步骤 7 打印 `json_sessionId= th-CODEX-001 replay= codex resume th-CODEX-001`（CLI JSON 与 HTTP 共用 `TraceView` 派生字段）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

---

## 四个结构化引擎 SessionID → run show 回放（确定性假引擎，零 token）

TC-003 已覆盖 codex（`thread_id`）的会话 id 数据流转与回放命令；本节补齐另三引擎，各自从自身输出的会话 id 字段填充 `RunResult.SessionID`，验证 `run show --trace` 附**该引擎专属**的回放命令。

### TC-009 claude-code：session_id → `claude -r <id>`

- **目的**：验证 claude-code 从 stdout 单对象的 `session_id` 取会话 id，`run show --trace` 附 `claude -r <id>` 回放命令、`--json` 经 trace 带出。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/claude" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  echo '{"result":"OUT","is_error":false,"session_id":"SID-CLAUDE","usage":{"input_tokens":1,"output_tokens":1}}'
  SH
  chmod +x "$WORK/fakebin/claude"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create cc --definition >/dev/null
  "$CONDUCT" workflow run cc "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="cc"][0])')
  "$CONDUCT" run show "$RID" --trace | grep '回放'
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-CLAUDE · 回放：claude -r SID-CLAUDE`。
  - 步骤 4 打印 `SID-CLAUDE`。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-010 qoder：session_id → `qodercli -r <id>`

- **目的**：验证 qoder 从 stdout 单对象的 `session_id` 取会话 id，`run show --trace` 附 `qodercli -r <id>` 回放命令。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/qodercli" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  echo '{"result":"OUT","is_error":false,"session_id":"SID-QODER","usage":{"input_tokens":1,"output_tokens":1}}'
  SH
  chmod +x "$WORK/fakebin/qodercli"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"qoder","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create qo --definition >/dev/null
  "$CONDUCT" workflow run qo "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="qo"][0])')
  "$CONDUCT" run show "$RID" --trace | grep '回放'
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-QODER · 回放：qodercli -r SID-QODER`。
  - 步骤 4 打印 `SID-QODER`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-011 antigravity：conversation_id → `agy --conversation <id>`

- **目的**：验证 antigravity 从 stdout 单对象的 `conversation_id`（字段名与另三引擎不同）取会话 id，`run show --trace` 附 `agy --conversation <id>` 回放命令。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/agy" <<'SH'
  #!/usr/bin/env bash
  echo '{"response":"OUT","status":"SUCCESS","conversation_id":"SID-AGY","usage":{"total_tokens":2}}'
  SH
  chmod +x "$WORK/fakebin/agy"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"antigravity","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create ag --definition >/dev/null
  "$CONDUCT" workflow run ag "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="ag"][0])')
  "$CONDUCT" run show "$RID" --trace | grep '回放'
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-AGY · 回放：agy --conversation SID-AGY`（用 `--conversation` 而非 `-r`——antigravity 的回放旗标与另三引擎不同）。
  - 步骤 4 打印 `SID-AGY`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-012 引擎未回报会话 id：trace 的 sessionId 为 null、run show 不显会话行

- **目的**：验证负路径——引擎输出**不含**会话 id 字段时，`RunResult.SessionID=nil`、trace 明确写 `"sessionId":null`、`run show --trace` **不显示会话行**（不臆造 id / 命令）。同时验证缺失 usage 不冒充 `tokens=0`。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/claude" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  echo '{"result":"OUT","is_error":false}'
  SH
  chmod +x "$WORK/fakebin/claude"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create ns --definition >/dev/null
  "$CONDUCT" workflow run ns "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="ns"][0])')
  set +e; count=$("$CONDUCT" run show "$RID" --trace | grep -c '会话'); rc=$?; set -e
  echo "$count"; echo "grep_exit=$rc"; test "$count" -eq 0; test "$rc" -eq 1
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"][0]; print("tokens=",t["tokens"],"sessionId=",t["sessionId"],"keys=",("tokens" in t and "sessionId" in t))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 打印两行：`0` 与 `grep_exit=1`（`--trace` 输出无任何「会话」行；`grep -c` 把计数 `0` 打到 stdout，但无匹配时退出码为 `1`，故 `grep_exit=$?` 记到 `1`——两者都要出现，别只对 `0` 打卡）。
  - 步骤 4 打印 `tokens= None sessionId= None keys= True`（两字段始终存在，未知值明确为 JSON `null`）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

---

## 交给单测的行为

以下行为在手工黑盒层难以稳定触发或不可触发，交由单测覆盖，本文不设手工用例：

- **codex JSONL 逐行解析的内部精确性**：`internal/engine/codex_test.go` 覆盖 thread.started / agent_message（含取最后一条和空文本）/ turn.completed / turn.failed（含无顶层 message 的事件类型回退）/ error / 无法解析行（含行号与前 200 字截断）/ 无 agent_message 各路径的返回值精确断言；本文只用 TC-003 保留正常路径的完整黑盒数据流转。
- **四个结构化引擎 SessionID 字段解析**：`internal/engine/session_test.go` 用假二进制断言各引擎从 `session_id` / `conversation_id` / `thread_id` 填充非空 `RunResult.SessionID`，并由解析器单测覆盖缺失/空值归一化为 `nil`；Kiro 固定 `nil` 由 `internal/engine/kiro_test.go` 覆盖。
- **TraceView replay 边界**：`internal/run/view_test.go` 覆盖未知引擎、replay 函数 nil/返回空串、session id nil/空时不调用函数，以及带空格/单引号 id 必须经 `engine.ShellQuote`；`internal/engine/descriptor_test.go` 单独覆盖 quoting 与 descriptor 深拷贝。黑盒不手写 trace.jsonl 伪造不可达状态。
- **codex 失败节点的 SessionID 不入 trace**：编排器在引擎返回错误时提前返回、不设 `entry.SessionID`（`internal/orchestrator/orchestrator.go`）；该单节点逻辑交单测，本文不专门断言。
- **空文本成功分支**：`internal/engine/codex_test.go` 覆盖 codex，`internal/engine/exec_test.go` 的 `TestSingleObjectEnginesAllowEmptyText` 覆盖 claude-code / qoder / antigravity。
