# codex 引擎 + 会话回放 id 测试用例

覆盖本次交付的两块功能：**codex 引擎接入**（`codex exec` 的 JSONL 事件流解析、engineConfig 校验、默认写死参数）与 **`RunResult.SessionID`（会话回放 id）**（四引擎从各自输出取会话/线程 id → 记入该节点 trace → `run show --trace` 附回放命令 / `--json` 经 trace 带出）。工作流定义的增删改查见 [workflow-editing.md](./workflow-editing.md)，跑工作流与查运行记录的通用面见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈codex〉〈引擎抽象〉〈schema 字段映射〉〈conduct 默认写死的参数〉〈引擎能力表〉、[docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈run show〉〈runs/ 落盘结构〉。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**，用来验证实现对不对——不是照现有代码反推。截至编写时，codex 引擎与 `RunResult.SessionID` 链路（四引擎 + `run show --trace` 回放行）**均已实装**（见 engines.md / cli-runtime.md〈实现状态〉），预期可直接对照验证。命令若偏离本文〈预期〉，即为实现未达标。

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
- **校验（边界）**：codex 接受 `model` + `reasoningEffort`（合法值 low/medium/high/xhigh）→ TC-001；`reasoningEffort` 越界（含 `max`——qoder 认、codex 不认，专为区分两引擎的允许集）被拒 → TC-002；codex 不认 `effort`（只认 reasoningEffort，给错字段被拒）→ TC-002；未知引擎名被拒 / codex 空 engineConfig 被接受，已由 [workflow-editing.md](./workflow-editing.md) TC-033 覆盖，本文不重复。
- **默认写死参数 + 可变参数映射（数据流转）**：`exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -` 恒附加、`Model→--model`、`reasoningEffort→-c model_reasoning_effort=<v>` → TC-003（断言 conduct 实际传给二进制的 argv）。
- **JSONL 逐行解析各分支（数据流转 / 错误路径）**：正常（thread.started→SessionID、agent_message→Text、turn.completed→Tokens）→ TC-003；取**最后一条** agent_message → TC-004；`turn.failed` 失败优先（即便进程退 0）→ TC-005；`error` 事件报错（附 message）→ TC-006；无法解析的行显式报错（附行号 + 前 200 字）→ TC-007；无 agent_message 报错（不假装成功）→ TC-008；agent_message 存在但文本为空仍是合法成功结果 → TC-013。

**RunResult.SessionID（会话回放 id）**
- **数据流转（引擎输出 → trace → run show 展示）**：四引擎各自的会话 id 字段流到 trace 的 `sessionId`，再由 `run show --trace` 附回放命令、`--json` 经 trace 带出——codex（`thread_id`）→ TC-003；claude-code（`session_id`）→ TC-009；qoder（`session_id`）→ TC-010；antigravity（`conversation_id`）→ TC-011。
- **回放命令逐引擎正确**：`claude -r`（TC-009）/ `codex resume`（TC-003）/ `qodercli -r`（TC-010）/ `agy --conversation`（TC-011）。
- **负路径**：引擎未回报会话 id 时 trace 不带 `sessionId`、`run show --trace` 不显会话行 → TC-012。

**交给单测 / 不可黑盒触发**（见〈交给单测的行为〉）：codex 失败节点的 `SessionID` 不入 trace（失败提前返回，不设 `entry.SessionID`）；`sessionReplayLine` 的**未知引擎**分支（`run show` 只显 id、不臆造命令）——现四引擎均在回放命令 switch 内，黑盒下无法产出「未知引擎」的 trace 记录。

---

## codex 引擎校验（编辑态，零 token）

### TC-001 codex 接受 model + reasoningEffort（合法档位）

- **目的**：验证 codex 引擎的 engineConfig 判别联合接受 `model`（任意非空串）与 `reasoningEffort`（合法档位），入库成功。
- **前置**：无；下方脚本自行建立隔离、注册 trap 并造 fixture。
- **步骤**：完整复制执行一个脚本块（定义须自带保留标记节点 START/END）：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  for pair in k1:low k2:medium k3:xhigh; do
    name=${pair%%:*}; effort=${pair#*:}
    printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"model":"gpt-5-codex","reasoningEffort":"%s"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' "$effort" \
      | "$CONDUCT" workflow create "$name" --definition
    echo "exit=0"
  done
  "$CONDUCT" workflow show k1 --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["definition"]["nodes"][1]["engineConfig"];print("model=",n["model"],"re=",n["reasoningEffort"])'
  BASH
  ```
- **预期**：
  - 步骤 1、2、3 退出码均 `0`（`reasoningEffort ∈ {low,medium,high,xhigh}`，`low` / `medium` / `xhigh` 逐档均在集内；`high` 由 TC-003 覆盖；model 不做白名单，任意非空串放行）。
  - 步骤 4 打印 `model= gpt-5-codex re= low`（engineConfig 两字段原样入库；节点下标 `[1]` 因 `[0]` 是 START 标记节点）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

### TC-002 codex 校验：reasoningEffort 越界被拒、不认 effort

- **目的**：验证 codex 的调优字段绑定与取值校验：① `reasoningEffort` 取值须落在 `{low,medium,high,xhigh}`，越界（含 qoder 才认的 `max`）被拒；② codex **只认 `reasoningEffort`**，给 `effort`（claude-code 专属字段）被拒。逐条触发，均不落盘。
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
  expect_rejected b1 reasoningEffort insane
  expect_rejected b2 reasoningEffort max
  expect_rejected b3 effort high
  "$CONDUCT" workflow list | grep -q 'store 为空'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[1].engineConfig.reasoningEffort: "insane" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`（节点下标 `[1]` 因 `[0]` 是 START）。
  - 步骤 2 退出码 `1`，stderr 含 `"max" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`（`max` 是 qoder 的档位，codex 不认——两引擎允许集不同）。
  - 步骤 3 退出码 `1`，stderr 含 `nodes[1].engineConfig.effort: engine="codex" 不认 effort（该引擎用 reasoningEffort）`。
  - 步骤 4 列表为空（`b1`/`b2`/`b3` 均未落盘）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

---

## codex 端到端解析（确定性假 codex，零 token）

### TC-003 codex 正常路径：解析 Text/Tokens/SessionID + 默认与可变参数映射 + 回放展示

- **目的**：一条用例串起 codex 的核心正常链路——① JSONL 三事件正常解析（`thread.started`→SessionID、`agent_message`→Text、`turn.completed`→Tokens）；② conduct 传给 `codex` 二进制的 argv 含**默认写死参数**与 `Model`/`reasoningEffort` 的**可变参数映射**；③ SessionID 数据流转：`thread_id` → trace 的 `sessionId` → `run show --trace` 附 codex 回放命令、`--json --trace` 经 trace 带出。
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
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}","engineConfig":{"model":"gpt-5-codex","reasoningEffort":"high"}},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' \
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
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print("json_sessionId=", json.load(sys.stdin)["trace"][0].get("sessionId"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`；stdout 含 `▶ 调度 1 个节点`、`▶ say [打招呼] 开跑 · engine=codex` 与 `✓ say 完成`，完成行含 `tokens=24885`、`产物 14 字符：CODEX-ARTIFACT`（tokens = 24763+122；耗时不逐字比对）。
  - 步骤 3 打印 `output= CODEX-ARTIFACT tokens= 24885 sessionId= th-CODEX-001`（Text 取 agent_message、Tokens 取 turn.completed 的 input+output、SessionID 取 thread_id）。
  - 步骤 4 依次打印 `default_args_ok`、`model_ok`、`effort_ok`（三段 argv 均如实下传；`-` 哨兵在 PROMPT 位）。
  - 步骤 5 打印 `stdin_prompt_ok`（渲染后的 prompt「回复：hi。需求：打个招呼」经 stdin 下传，含用户输入 `打个招呼`——证明 prompt 走 stdin 而非 argv）。
  - 步骤 6 输出 `会话 th-CODEX-001 · 回放：codex resume th-CODEX-001`（codex 的回放命令）。
  - 步骤 7 打印 `json_sessionId= th-CODEX-001`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

### TC-004 codex 取最后一条 agent_message

- **目的**：验证事件流含多条 `agent_message` 时，`Text` 取**最后一条**（spec：agent_message → Text 取最后）。退化夹具（只发一条）验不出这条，故发两条不同文本。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  cat <<'JSONL'
  {"type":"thread.started","thread_id":"th-LAST"}
  {"type":"item.completed","item":{"type":"agent_message","text":"FIRST"}}
  {"type":"item.completed","item":{"type":"agent_message","text":"LAST"}}
  {"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
  JSONL
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create last --definition >/dev/null
  "$CONDUCT" workflow run last "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="last"][0])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print("output=",json.load(sys.stdin)["trace"][0]["output"])'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 打印 `output= LAST`（取最后一条 agent_message，非 `FIRST`）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-005 codex turn.failed 失败优先（即便进程退 0）

- **目的**：验证事件流出现 `turn.failed` 时**失败优先**——即使 ① 前面已有 agent_message、② 进程退出码为 `0`，该节点仍判失败并落盘失败信息（不假装成功）。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  printf '%s\n' '{"type":"thread.started","thread_id":"th-TF"}' '{"type":"item.completed","item":{"type":"agent_message","text":"partial"}}' '{"type":"turn.failed","error":{"message":"boom"}}'
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create tf --definition >/dev/null
  set +e; "$CONDUCT" workflow run tf "go" --cwd "$WORK"; rc=$?; set -e
  echo "exit=$rc"; test "$rc" -eq 1
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="tf"][0])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; r=json.load(sys.stdin); tr=r["trace"]; print("status=",r["status"],"failedTrace=",[(e["nodeId"], e["success"]) for e in tr],"error=",r["error"])'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`（进程虽退 0，但事件流报失败 → 该节点失败 → 运行失败）。
  - 步骤 2 打印 `status= failed failedTrace= [('say', False)] error= codex 报错: turn.failed`（失败信息真实落盘；失败节点由 trace 的 `nodeId=say success=false` 记录体现；`turn.failed` 的错误体嵌在 `error.message`、非顶层 `message`，故回退给出事件类型 `turn.failed` 作占位，不静默丢失失败信号）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-006 codex error 事件报错（附 message）

- **目的**：验证 `error` 事件被判失败，且错误文本取事件的顶层 `message`（与 TC-005 的 `turn.failed` 无 message 回退分支互补，一并覆盖 `codexFailureMessage` 两条路径）。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  echo '{"type":"error","message":"配额耗尽"}'
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create er --definition >/dev/null
  set +e; "$CONDUCT" workflow run er "go" --cwd "$WORK"; rc=$?; set -e
  echo "exit=$rc"; test "$rc" -eq 1
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="er"][0])')
  "$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json; r=json.load(sys.stdin); print("status=",r["status"],"error=",r["error"])'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `status= failed error= codex 报错: 配额耗尽`（error 事件的顶层 `message` 被如实带出）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-007 codex 无法解析的行显式报错（附行号 + 前 200 字）

- **目的**：验证事件流里出现**无法解析为 JSON 的行**时显式报错、不静默跳过，且错误附**行号**与该行的内容；并验证内容按「**前 200 字**」契约截断——坏行超过 200 字符时只带出前 200 字（末尾 `…`），不整行糊进错误。用超长坏行才能验出这条截断边界（短坏行验不出）。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  BAD="BADHEAD$(printf 'x%.0s' $(seq 200))TAILMARK"
  printf '%s\n%s\n' '{"type":"thread.started","thread_id":"th-BAD"}' "$BAD" > "$WORK/codex.jsonl"
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  cat "$WORK/codex.jsonl"
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create up --definition >/dev/null
  set +e; "$CONDUCT" workflow run up "go" --cwd "$WORK"; rc=$?; set -e
  echo "exit=$rc"; test "$rc" -eq 1
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="up"][0])')
  "$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json; e=json.load(sys.stdin)["error"]; print("line2=", "第 2 行无法解析" in e); print("head=", "BADHEAD" in e); print("ellipsis=", "…" in e); print("tail_dropped=", "TAILMARK" not in e)'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `line2= True`（错误含 `codex 输出非预期 JSON: 第 2 行无法解析:`，行号为 2）、`head= True`（前 200 字保留可辨识头 `BADHEAD`）、`ellipsis= True`（超长被截断，末尾附 `…`）、`tail_dropped= True`（第 200 字之外的尾标 `TAILMARK` 未进错误——「前 200 字」截断契约生效）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-008 codex 无 agent_message 报错（不假装成功）

- **目的**：验证事件流**既无失败事件、也无任何 agent_message** 时（如只有 thread.started + turn.completed），显式报「未产出最终 agent_message」而非假装成功给空产物。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  printf '%s\n' '{"type":"thread.started","thread_id":"th-NA"}' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create na --definition >/dev/null
  set +e; "$CONDUCT" workflow run na "go" --cwd "$WORK"; rc=$?; set -e
  echo "exit=$rc"; test "$rc" -eq 1
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="na"][0])')
  "$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json; r=json.load(sys.stdin); print("status=",r["status"],"error=",r["error"])'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `status= failed error= codex 未产出最终 agent_message`（无产物即失败，不假装成功）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

---

## 四引擎 SessionID → run show 回放（确定性假引擎，零 token）

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

### TC-012 引擎未回报会话 id：trace 不带 sessionId、run show 不显会话行

- **目的**：验证负路径——引擎输出**不含**会话 id 字段时，`RunResult.SessionID` 为空、trace 的 `sessionId` 因 `omitempty` 不落盘、`run show --trace` **不显示会话行**（不臆造 id / 命令）。
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
  echo '{"result":"OUT","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
  SH
  chmod +x "$WORK/fakebin/claude"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create ns --definition >/dev/null
  "$CONDUCT" workflow run ns "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="ns"][0])')
  set +e; count=$("$CONDUCT" run show "$RID" --trace | grep -c '会话'); rc=$?; set -e
  echo "$count"; echo "grep_exit=$rc"; test "$count" -eq 0; test "$rc" -eq 1
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"]; print("has_sessionId=", "sessionId" in t[0])'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 打印两行：`0` 与 `grep_exit=1`（`--trace` 输出无任何「会话」行；`grep -c` 把计数 `0` 打到 stdout，但无匹配时退出码为 `1`，故 `grep_exit=$?` 记到 `1`——两者都要出现，别只对 `0` 打卡）。
  - 步骤 4 打印 `has_sessionId= False`（trace 记录无 `sessionId` 键，`omitempty` 生效）。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

### TC-013 codex 空文本 agent_message 仍成功

- **目的**：验证 `agent_message` 事件已经存在但 `text` 是空字符串时，`RunResult.Text=""` 是合法成功结果；与 TC-008“整个事件流没有 agent_message”必须失败严格区分。
- **前置**：无；脚本自行完成隔离、trap、假引擎与 workflow fixture。
- **步骤**：完整复制执行：
  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  cat > "$WORK/fakebin/codex" <<'SH'
  #!/usr/bin/env bash
  cat >/dev/null
  printf '%s\n' '{"type":"thread.started","thread_id":"th-EMPTY"}' '{"type":"item.completed","item":{"type":"agent_message","text":""}}' '{"type":"turn.completed","usage":{"input_tokens":3,"output_tokens":0}}'
  SH
  chmod +x "$WORK/fakebin/codex"
  printf '{"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}' | "$CONDUCT" workflow create empty-text --definition >/dev/null
  "$CONDUCT" workflow run empty-text "go" --cwd "$WORK"; echo "exit=0"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print(json.load(sys.stdin)[0]["id"])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; d=json.load(sys.stdin); t=d["trace"][0]; print("status=",d["status"],"success=",t["success"],"output_repr=",repr(t["output"]),"tokens=",t["tokens"],"session=",t.get("sessionId"))'
  BASH
  ```
- **预期**：
  - 步骤 1 退出码 `0`；节点完成而非报 `codex 未产出最终 agent_message`。
  - 步骤 3 打印 `status= completed success= True output_repr= '' tokens= 3 session= th-EMPTY`。
  - 该用例以“事件存在性”区分空产物和缺产物，不用 `output != ""` 作为成功判据。
- **清理**：由脚本 trap 自动完成，并比较真实 store 前后快照。

---

## 交给单测的行为

以下行为在手工黑盒层难以稳定触发或不可触发，交由单测覆盖，本文不设手工用例：

- **codex JSONL 逐行解析的内部精确性**：`internal/engine/codex_test.go` 覆盖 thread.started / agent_message（含取最后一条）/ turn.completed / turn.failed / error / 无法解析行 / 无 agent_message 各路径的**返回值精确断言**（本文 TC-003~008 从外部验证其可观察后果，二者互补）。
- **四引擎 SessionID 字段解析**：`internal/engine/session_test.go` 用假二进制断言各引擎从 `session_id` / `conversation_id` / `thread_id` 填充 `RunResult.SessionID`。
- **`sessionReplayLine` 未知引擎分支**（`run show` 只显 id、不臆造命令）：现四个已注册引擎（claude-code / codex / qoder / antigravity）均在回放命令 switch 内，黑盒下**无法**产出「引擎名不在 switch」的 trace 记录——除非手写 trace.jsonl 伪造（本文禁止）。该防御分支交单测（`internal/cli/run_show_test.go`）覆盖。
- **codex 失败节点的 SessionID 不入 trace**：编排器在引擎返回错误时提前返回、不设 `entry.SessionID`（`internal/orchestrator/orchestrator.go`），故 TC-005～TC-008 的失败节点即便事件流带 `thread_id` 也不落 `sessionId`——此为实现取舍（失败节点无回放价值），其单节点逻辑交单测，本文不专门断言。
- **另外三个单对象引擎的空文本成功分支**：`internal/engine/exec_test.go` 的 `TestSingleObjectEnginesAllowEmptyText` 覆盖 claude-code / qoder / antigravity；TC-013 从黑盒覆盖同一 `RunResult.Text` 契约在 codex JSONL 路径上的结果。
