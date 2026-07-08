# codex 引擎 + 会话回放 id 测试用例

覆盖本次交付的两块功能：**codex 引擎接入**（`codex exec` 的 JSONL 事件流解析、engineConfig 校验、默认写死参数）与 **`RunResult.SessionID`（会话回放 id）**（四引擎从各自输出取会话/线程 id → 记入该步 trace → `run show --trace` 附回放命令 / `--json` 经 trace 带出）。工作流定义的增删改查见 [workflow-editing.md](./workflow-editing.md)，跑工作流与查运行记录的通用面见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈codex〉〈引擎抽象〉〈schema 字段映射〉〈conduct 默认写死的参数〉〈引擎能力表〉、[docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈run show〉〈runs/ 落盘结构〉。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**，用来验证实现对不对——不是照现有代码反推。截至编写时，codex 引擎与 `RunResult.SessionID` 链路（四引擎 + `run show --trace` 回放行）**均已实装**（见 engines.md / cli-runtime.md〈实现状态〉），预期可直接对照验证。命令若偏离本文〈预期〉，即为实现未达标。

> **本文用例全部零 token**：校验类用例只做定义校验、不触引擎；端到端类用例**用确定性假引擎**（fake binary）顶替真 `codex` / `claude` / `qodercli` / `agy`。**引擎是 conduct 的外部依赖**，用假二进制顶替是复现「本机装了某引擎、它这样输出」的真实场景、也规避真实调用的登录态 / 网络 / 不确定产物 / 💸——这属外部依赖的测试替身（同 [workflow-running.md](./workflow-running.md) TC-007/TC-010 的做法），**不是伪造内部数据**：run.json / trace.jsonl 仍全由 conduct 真实的 `workflow run` 代码路径产出，本文只在最外层遮蔽引擎子进程。绝不手写 run.json / trace.jsonl 去「摆拍」记录。

> **隔离机制（关键）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。为不污染真实家目录，**每个用例把 `HOME` 重定向到临时目录**（`export HOME="$WORK"`），store 随之落在 `$WORK/.conduct/`；假引擎装进 `$WORK/fakebin` 并**前置到 `PATH`** 遮蔽真引擎。用例结束连同临时目录一并删除、恢复 `HOME` 与 `PATH`。因假引擎不读写真凭据、秒级返回，端到端用例同样零 token、可无人值守自动化。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
REAL_HOME="$HOME"            # 一次性记下真实家目录，供中途失败时找回
```

> **装机版不可用于验证本功能**：全局 `conduct`（0.0.1）尚无 codex，务必用上面 `make build` 产出的 `./bin/conduct`。

各用例〈前置〉统一用这段建立隔离环境 + 空的假引擎目录（下文简称「建隔离环境」）：

```bash
WORK=$(mktemp -d)
OLD_HOME="$HOME"; export HOME="$WORK"          # store 落到 $WORK/.conduct，隔离全局
OLD_PATH="$PATH"; mkdir -p "$WORK/fakebin"; export PATH="$WORK/fakebin:$PATH"
```

> **弃跑保护**：`OLD_HOME` / `OLD_PATH` 在每个用例〈前置〉里现取现存，正常顺序执行无碍。若某用例中途失败、未跑〈清理〉就直接开下一个用例，恢复即错——此时改用 `export HOME="$REAL_HOME"` 找回真实家目录。

对应〈清理〉统一为：

```bash
export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"
```

装一个「确定性假 codex」的助手（每个 codex 端到端用例在〈前置〉里调用，喂入所需 JSONL）——它把 stdin 的 prompt 落到 `$CODEX_STDIN_FILE`（`codex exec -` 从 stdin 读，供断言渲染后的 prompt 确实经 stdin 下传）、把 conduct 实际传入的 argv 落到 `$CODEX_ARGS_FILE`（供断言默认参数），再原样吐出给定的 JSONL 到 stdout：

```bash
# 用法：install_fake_codex <exit_code> <<'JSONL' … JSONL   —— JSONL 从 stdin 传入
install_fake_codex() {
  local rc="$1"
  export CODEX_ARGS_FILE="$WORK/codex-args.txt"
  export CODEX_STDIN_FILE="$WORK/codex-stdin.txt"
  cat > "$WORK/fakebin/codex.jsonl"          # 暂存要吐出的事件流
  cat > "$WORK/fakebin/codex" <<SH
#!/usr/bin/env bash
cat > "$CODEX_STDIN_FILE"                     # 落存 stdin 的 prompt（供断言 prompt 走 stdin）
printf '%s\n' "\$*" > "$CODEX_ARGS_FILE"     # 记录 conduct 传入的 argv
cat "$WORK/fakebin/codex.jsonl"              # 吐出事件流
exit $rc
SH
  chmod +x "$WORK/fakebin/codex"
}
```

多个用例复用的**最小 codex 工作流**（单节点、engine=codex）：

```bash
# 用法：make_codex_flow <name> [engineConfig-json]
make_codex_flow() {
  local ec="${2:-}"
  local cfg=""
  [ -n "$ec" ] && cfg=",\"engineConfig\":$ec"
  printf '{"nodes":[{"id":"say","displayName":"打招呼","engine":"codex","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"%s}]}' "$cfg" \
    | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
```

---

## 功能覆盖清单（动笔前规划）

对着 spec 的行为空间逐项映射到用例，避免只测 happy path：

**codex 引擎**
- **校验（边界）**：codex 接受 `model` + `reasoningEffort`（合法值 low/medium/high/xhigh）→ TC-001；`reasoningEffort` 越界（含 `max`——qoder 认、codex 不认，专为区分两引擎的允许集）被拒 → TC-002；codex 不认 `effort`（只认 reasoningEffort，给错字段被拒）→ TC-002；未知引擎名被拒 / codex 空 engineConfig 被接受，已由 [workflow-editing.md](./workflow-editing.md) TC-031 覆盖，本文不重复。
- **默认写死参数 + 可变参数映射（数据流转）**：`exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -` 恒附加、`Model→--model`、`reasoningEffort→-c model_reasoning_effort=<v>` → TC-003（断言 conduct 实际传给二进制的 argv）。
- **JSONL 逐行解析各分支（数据流转 / 错误路径）**：正常（thread.started→SessionID、agent_message→Text、turn.completed→Tokens）→ TC-003；取**最后一条** agent_message → TC-004；`turn.failed` 失败优先（即便进程退 0）→ TC-005；`error` 事件报错（附 message）→ TC-006；无法解析的行显式报错（附行号 + 前 200 字）→ TC-007；无 agent_message 报错（不假装成功）→ TC-008。

**RunResult.SessionID（会话回放 id）**
- **数据流转（引擎输出 → trace → run show 展示）**：四引擎各自的会话 id 字段流到 trace 的 `sessionId`，再由 `run show --trace` 附回放命令、`--json` 经 trace 带出——codex（`thread_id`）→ TC-003；claude-code（`session_id`）→ TC-009；qoder（`session_id`）→ TC-010；antigravity（`conversation_id`）→ TC-011。
- **回放命令逐引擎正确**：`claude -r`（TC-009）/ `codex resume`（TC-003）/ `qodercli -r`（TC-010）/ `agy --conversation`（TC-011）。
- **负路径**：引擎未回报会话 id 时 trace 不带 `sessionId`、`run show --trace` 不显会话行 → TC-012。

**交给单测 / 不可黑盒触发**（见〈交给单测的行为〉）：codex 失败步的 `SessionID` 不入 trace（失败提前返回，不设 `entry.SessionID`）；`sessionReplayLine` 的**未知引擎**分支（`run show` 只显 id、不臆造命令）——现四引擎均在回放命令 switch 内，黑盒下无法产出「未知引擎」的 trace 步。

---

## codex 引擎校验（编辑态，零 token）

### TC-001 codex 接受 model + reasoningEffort（合法档位）

- **目的**：验证 codex 引擎的 engineConfig 判别联合接受 `model`（任意非空串）与 `reasoningEffort`（合法档位），入库成功。
- **前置**：建隔离环境。
- **步骤**（逐条应成功）：
  1. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"model":"gpt-5-codex","reasoningEffort":"low"}}]}' | "$CONDUCT" workflow create k1 --definition; echo "exit=$?"`
  2. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"model":"gpt-5-codex","reasoningEffort":"medium"}}]}' | "$CONDUCT" workflow create k2 --definition; echo "exit=$?"`
  3. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"model":"gpt-5-codex","reasoningEffort":"xhigh"}}]}' | "$CONDUCT" workflow create k3 --definition; echo "exit=$?"`
  4. `"$CONDUCT" workflow show k1 --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0]["engineConfig"];print("model=",n["model"],"re=",n["reasoningEffort"])'`
- **预期**：
  - 步骤 1、2、3 退出码均 `0`（`reasoningEffort ∈ {low,medium,high,xhigh}`，`low` / `medium` / `xhigh` 逐档均在集内；`high` 由 TC-003 覆盖；model 不做白名单，任意非空串放行）。
  - 步骤 4 打印 `model= gpt-5-codex re= low`（engineConfig 两字段原样入库）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-002 codex 校验：reasoningEffort 越界被拒、不认 effort

- **目的**：验证 codex 的调优字段绑定与取值校验：① `reasoningEffort` 取值须落在 `{low,medium,high,xhigh}`，越界（含 qoder 才认的 `max`）被拒；② codex **只认 `reasoningEffort`**，给 `effort`（claude-code 专属字段）被拒。逐条触发，均不落盘。
- **前置**：建隔离环境。
- **步骤**（逐条应退出 `1`）：
  1. reasoningEffort 越界：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"reasoningEffort":"insane"}}]}' | "$CONDUCT" workflow create b1 --definition; echo "exit=$?"`
  2. reasoningEffort 用 qoder 才认的 `max`（codex 允许集无 `max`）：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"reasoningEffort":"max"}}]}' | "$CONDUCT" workflow create b2 --definition; echo "exit=$?"`
  3. codex 不认 effort：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"effort":"high"}}]}' | "$CONDUCT" workflow create b3 --definition; echo "exit=$?"`
  4. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].engineConfig.reasoningEffort: "insane" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`。
  - 步骤 2 退出码 `1`，stderr 含 `"max" 不在 engine="codex" 允许集 [low, medium, high, xhigh] 内`（`max` 是 qoder 的档位，codex 不认——两引擎允许集不同）。
  - 步骤 3 退出码 `1`，stderr 含 `nodes[0].engineConfig.effort: engine="codex" 不认 effort（该引擎用 reasoningEffort）`。
  - 步骤 4 列表为空（`b1`/`b2`/`b3` 均未落盘）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

---

## codex 端到端解析（确定性假 codex，零 token）

### TC-003 codex 正常路径：解析 Text/Tokens/SessionID + 默认与可变参数映射 + 回放展示

- **目的**：一条用例串起 codex 的核心正常链路——① JSONL 三事件正常解析（`thread.started`→SessionID、`agent_message`→Text、`turn.completed`→Tokens）；② conduct 传给 `codex` 二进制的 argv 含**默认写死参数**与 `Model`/`reasoningEffort` 的**可变参数映射**；③ SessionID 数据流转：`thread_id` → trace 的 `sessionId` → `run show --trace` 附 codex 回放命令、`--json --trace` 经 trace 带出。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（正常三事件、退 0）：
     ```bash
     install_fake_codex 0 <<'JSONL'
     {"type":"thread.started","thread_id":"th-CODEX-001"}
     {"type":"item.completed","item":{"type":"agent_message","text":"CODEX-ARTIFACT"}}
     {"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}
     JSONL
     ```
  3. 造带 model + reasoningEffort 的 codex 工作流：`make_codex_flow cx '{"model":"gpt-5-codex","reasoningEffort":"high"}'`。
- **步骤**：
  1. `"$CONDUCT" workflow run cx "打个招呼" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^cx-' | head -1)`
  3. 断言 trace 解析结果：
     ```bash
     python3 -c 'import json,glob,os; d=glob.glob(os.path.expanduser("~/.conduct/runs/cx-*"))[0]; t=[json.loads(l) for l in open(d+"/trace.jsonl") if l.strip()]; print("output=",t[0]["output"],"tokens=",t[0]["tokens"],"sessionId=",t[0].get("sessionId"))'
     ```
  4. 断言默认参数 + 可变参数映射（conduct 实际传给二进制的 argv）：
     ```bash
     grep -q -- 'exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -' "$CODEX_ARGS_FILE" && echo "default_args_ok"
     grep -q -- '--model gpt-5-codex' "$CODEX_ARGS_FILE" && echo "model_ok"
     grep -q -- '-c model_reasoning_effort=high' "$CODEX_ARGS_FILE" && echo "effort_ok"
     ```
  5. 断言渲染后的 prompt 确实经 stdin 下传（`-` 哨兵在 PROMPT 位、prompt 不走 argv）：
     ```bash
     grep -q '打个招呼' "$CODEX_STDIN_FILE" && echo "stdin_prompt_ok"
     ```
  6. `"$CONDUCT" run show "$RID" --trace | grep '回放'`
  7. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print("json_sessionId=", json.load(sys.stdin)["trace"][0].get("sessionId"))'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 展开 1 步并完成，步行含 `engine=codex model=gpt-5-codex`，`✓ …ms tokens=24885 产物 14 字符：CODEX-ARTIFACT`（tokens = 24763+122）。
  - 步骤 3 打印 `output= CODEX-ARTIFACT tokens= 24885 sessionId= th-CODEX-001`（Text 取 agent_message、Tokens 取 turn.completed 的 input+output、SessionID 取 thread_id）。
  - 步骤 4 依次打印 `default_args_ok`、`model_ok`、`effort_ok`（三段 argv 均如实下传；`-` 哨兵在 PROMPT 位）。
  - 步骤 5 打印 `stdin_prompt_ok`（渲染后的 prompt「回复：hi。需求：打个招呼」经 stdin 下传，含用户输入 `打个招呼`——证明 prompt 走 stdin 而非 argv）。
  - 步骤 6 输出 `会话 th-CODEX-001 · 回放：codex resume th-CODEX-001`（codex 的回放命令）。
  - 步骤 7 打印 `json_sessionId= th-CODEX-001`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-004 codex 取最后一条 agent_message

- **目的**：验证事件流含多条 `agent_message` 时，`Text` 取**最后一条**（spec：agent_message → Text 取最后）。退化夹具（只发一条）验不出这条，故发两条不同文本。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（两条 agent_message，文本不同）：
     ```bash
     install_fake_codex 0 <<'JSONL'
     {"type":"thread.started","thread_id":"th-LAST"}
     {"type":"item.completed","item":{"type":"agent_message","text":"FIRST"}}
     {"type":"item.completed","item":{"type":"agent_message","text":"LAST"}}
     {"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
     JSONL
     ```
  3. `make_codex_flow last`。
- **步骤**：
  1. `"$CONDUCT" workflow run last "go" --cwd "$WORK"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; d=glob.glob(os.path.expanduser("~/.conduct/runs/last-*"))[0]; t=[json.loads(l) for l in open(d+"/trace.jsonl") if l.strip()]; print("output=",t[0]["output"])'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 打印 `output= LAST`（取最后一条 agent_message，非 `FIRST`）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-005 codex turn.failed 失败优先（即便进程退 0）

- **目的**：验证事件流出现 `turn.failed` 时**失败优先**——即使 ① 前面已有 agent_message、② 进程退出码为 `0`，该步仍判失败并落盘失败信息（不假装成功）。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（先 agent_message 再 turn.failed，**退 0**）：
     ```bash
     install_fake_codex 0 <<'JSONL'
     {"type":"thread.started","thread_id":"th-TF"}
     {"type":"item.completed","item":{"type":"agent_message","text":"partial"}}
     {"type":"turn.failed","error":{"message":"boom"}}
     JSONL
     ```
  3. `make_codex_flow tf`。
- **步骤**：
  1. `"$CONDUCT" workflow run tf "go" --cwd "$WORK"; echo "exit=$?"`
  2. `"$CONDUCT" run show "$(ls "$HOME/.conduct/runs/" | grep '^tf-' | head -1)" --json --trace | python3 -c 'import sys,json; r=json.load(sys.stdin); tr=r["trace"]; print("status=",r["status"],"failedTrace=",[(e["stepIndex"], e["success"]) for e in tr],"error=",r["error"])'`
- **预期**：
  - 步骤 1 退出码 `1`（进程虽退 0，但事件流报失败 → 该步失败 → 运行失败）。
  - 步骤 2 打印 `status= failed failedTrace= [(0, False)] error= codex 报错: turn.failed`（失败信息真实落盘；失败步由 trace 的 `stepIndex=0 success=false` 记录体现；`turn.failed` 的错误体嵌在 `error.message`、非顶层 `message`，故回退给出事件类型 `turn.failed` 作占位，不静默丢失失败信号）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-006 codex error 事件报错（附 message）

- **目的**：验证 `error` 事件被判失败，且错误文本取事件的顶层 `message`（与 TC-005 的 `turn.failed` 无 message 回退分支互补，一并覆盖 `codexFailureMessage` 两条路径）。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（单个 error 事件）：
     ```bash
     install_fake_codex 0 <<'JSONL'
     {"type":"error","message":"配额耗尽"}
     JSONL
     ```
  3. `make_codex_flow er`。
- **步骤**：
  1. `"$CONDUCT" workflow run er "go" --cwd "$WORK"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; r=json.load(open(glob.glob(os.path.expanduser("~/.conduct/runs/er-*/run.json"))[0])); print("status=",r["status"],"error=",r["error"])'`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `status= failed error= codex 报错: 配额耗尽`（error 事件的顶层 `message` 被如实带出）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-007 codex 无法解析的行显式报错（附行号 + 前 200 字）

- **目的**：验证事件流里出现**无法解析为 JSON 的行**时显式报错、不静默跳过，且错误附**行号**与该行的内容；并验证内容按「**前 200 字**」契约截断——坏行超过 200 字符时只带出前 200 字（末尾 `…`），不整行糊进错误。用超长坏行才能验出这条截断边界（短坏行验不出）。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（第 2 行是超 200 字的坏行：可辨识头 `BADHEAD` + 200 个 `x` + 尾标 `TAILMARK`，共 215 字符，尾标落在 200 字截断线之外）：
     ```bash
     BAD="BADHEAD$(printf 'x%.0s' $(seq 200))TAILMARK"   # 215 字符：头在截断线内、尾标在线外
     { echo '{"type":"thread.started","thread_id":"th-BAD"}'; echo "$BAD"; } | install_fake_codex 0
     ```
  3. `make_codex_flow up`。
- **步骤**：
  1. `"$CONDUCT" workflow run up "go" --cwd "$WORK"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; e=json.load(open(glob.glob(os.path.expanduser("~/.conduct/runs/up-*/run.json"))[0]))["error"]; print("line2=", "第 2 行无法解析" in e); print("head=", "BADHEAD" in e); print("ellipsis=", "…" in e); print("tail_dropped=", "TAILMARK" not in e)'`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `line2= True`（错误含 `codex 输出非预期 JSON: 第 2 行无法解析:`，行号为 2）、`head= True`（前 200 字保留可辨识头 `BADHEAD`）、`ellipsis= True`（超长被截断，末尾附 `…`）、`tail_dropped= True`（第 200 字之外的尾标 `TAILMARK` 未进错误——「前 200 字」截断契约生效）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-008 codex 无 agent_message 报错（不假装成功）

- **目的**：验证事件流**既无失败事件、也无任何 agent_message** 时（如只有 thread.started + turn.completed），显式报「未产出最终 agent_message」而非假装成功给空产物。
- **前置**：
  1. 建隔离环境。
  2. 装假 codex（无 agent_message）：
     ```bash
     install_fake_codex 0 <<'JSONL'
     {"type":"thread.started","thread_id":"th-NA"}
     {"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
     JSONL
     ```
  3. `make_codex_flow na`。
- **步骤**：
  1. `"$CONDUCT" workflow run na "go" --cwd "$WORK"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; r=json.load(open(glob.glob(os.path.expanduser("~/.conduct/runs/na-*/run.json"))[0])); print("status=",r["status"],"error=",r["error"])'`
- **预期**：
  - 步骤 1 退出码 `1`。
  - 步骤 2 打印 `status= failed error= codex 未产出最终 agent_message`（无产物即失败，不假装成功）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

---

## 四引擎 SessionID → run show 回放（确定性假引擎，零 token）

TC-003 已覆盖 codex（`thread_id`）的会话 id 数据流转与回放命令；本节补齐另三引擎，各自从自身输出的会话 id 字段填充 `RunResult.SessionID`，验证 `run show --trace` 附**该引擎专属**的回放命令。

### TC-009 claude-code：session_id → `claude -r <id>`

- **目的**：验证 claude-code 从 stdout 单对象的 `session_id` 取会话 id，`run show --trace` 附 `claude -r <id>` 回放命令、`--json` 经 trace 带出。
- **前置**：
  1. 建隔离环境。
  2. 装假 claude（输出带 `session_id` 的单 JSON 对象）：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"result":"OUT","is_error":false,"session_id":"SID-CLAUDE","usage":{"input_tokens":1,"output_tokens":1}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  3. `printf '{"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create cc --definition >/dev/null`。
- **步骤**：
  1. `"$CONDUCT" workflow run cc "go" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^cc-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --trace | grep '回放'`
  4. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-CLAUDE · 回放：claude -r SID-CLAUDE`。
  - 步骤 4 打印 `SID-CLAUDE`。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-010 qoder：session_id → `qodercli -r <id>`

- **目的**：验证 qoder 从 stdout 单对象的 `session_id` 取会话 id，`run show --trace` 附 `qodercli -r <id>` 回放命令。
- **前置**：
  1. 建隔离环境。
  2. 装假 qodercli（输出带 `session_id`）：
     ```bash
     cat > "$WORK/fakebin/qodercli" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"result":"OUT","is_error":false,"session_id":"SID-QODER","usage":{"input_tokens":1,"output_tokens":1}}'
     SH
     chmod +x "$WORK/fakebin/qodercli"
     ```
  3. `printf '{"nodes":[{"id":"say","displayName":"打招呼","engine":"qoder","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create qo --definition >/dev/null`。
- **步骤**：
  1. `"$CONDUCT" workflow run qo "go" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^qo-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --trace | grep '回放'`
  4. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-QODER · 回放：qodercli -r SID-QODER`。
  - 步骤 4 打印 `SID-QODER`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-011 antigravity：conversation_id → `agy --conversation <id>`

- **目的**：验证 antigravity 从 stdout 单对象的 `conversation_id`（字段名与另三引擎不同）取会话 id，`run show --trace` 附 `agy --conversation <id>` 回放命令。
- **前置**：
  1. 建隔离环境。
  2. 装假 agy（prompt 经 argv、不读 stdin；输出带 `conversation_id` + `status:"SUCCESS"`）：
     ```bash
     cat > "$WORK/fakebin/agy" <<'SH'
     #!/usr/bin/env bash
     echo '{"response":"OUT","status":"SUCCESS","conversation_id":"SID-AGY","usage":{"total_tokens":2}}'
     SH
     chmod +x "$WORK/fakebin/agy"
     ```
  3. `printf '{"nodes":[{"id":"say","displayName":"打招呼","engine":"antigravity","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create ag --definition >/dev/null`。
- **步骤**：
  1. `"$CONDUCT" workflow run ag "go" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^ag-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --trace | grep '回放'`
  4. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; print(json.load(sys.stdin)["trace"][0].get("sessionId"))'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 输出 `会话 SID-AGY · 回放：agy --conversation SID-AGY`（用 `--conversation` 而非 `-r`——antigravity 的回放旗标与另三引擎不同）。
  - 步骤 4 打印 `SID-AGY`（`--json --trace` 经 trace 数组的 `sessionId` 带出）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

### TC-012 引擎未回报会话 id：trace 不带 sessionId、run show 不显会话行

- **目的**：验证负路径——引擎输出**不含**会话 id 字段时，`RunResult.SessionID` 为空、trace 的 `sessionId` 因 `omitempty` 不落盘、`run show --trace` **不显示会话行**（不臆造 id / 命令）。
- **前置**：
  1. 建隔离环境。
  2. 装假 claude（输出**不含** `session_id`）：
     ```bash
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"result":"OUT","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     ```
  3. `printf '{"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create ns --definition >/dev/null`。
- **步骤**：
  1. `"$CONDUCT" workflow run ns "go" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^ns-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --trace | grep -c '会话'; echo "grep_exit=$?"`
  4. `python3 -c 'import json,glob,os; t=[json.loads(l) for l in open(glob.glob(os.path.expanduser("~/.conduct/runs/ns-*"))[0]+"/trace.jsonl") if l.strip()]; print("has_sessionId=", "sessionId" in t[0])'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 3 打印两行：`0` 与 `grep_exit=1`（`--trace` 输出无任何「会话」行；`grep -c` 把计数 `0` 打到 stdout，但无匹配时退出码为 `1`，故 `grep_exit=$?` 记到 `1`——两者都要出现，别只对 `0` 打卡）。
  - 步骤 4 打印 `has_sessionId= False`（trace 步无 `sessionId` 键，`omitempty` 生效）。
- **清理**：`export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"`。

---

## 交给单测的行为

以下行为在手工黑盒层难以稳定触发或不可触发，交由单测覆盖，本文不设手工用例：

- **codex JSONL 逐行解析的内部精确性**：`internal/engine/codex_test.go` 覆盖 thread.started / agent_message（含取最后一条）/ turn.completed / turn.failed / error / 无法解析行 / 无 agent_message 各路径的**返回值精确断言**（本文 TC-003~008 从外部验证其可观察后果，二者互补）。
- **四引擎 SessionID 字段解析**：`internal/engine/session_test.go` 用假二进制断言各引擎从 `session_id` / `conversation_id` / `thread_id` 填充 `RunResult.SessionID`。
- **`sessionReplayLine` 未知引擎分支**（`run show` 只显 id、不臆造命令）：现四个已注册引擎（claude-code / codex / qoder / antigravity）均在回放命令 switch 内，黑盒下**无法**产出「引擎名不在 switch」的 trace 步——除非手写 trace.jsonl 伪造（本文禁止）。该防御分支交单测（`internal/cli/run_show_test.go`）覆盖。
- **codex 失败步的 SessionID 不入 trace**：编排器在引擎返回错误时提前返回、不设 `entry.SessionID`（`internal/orchestrator/orchestrator.go`），故 TC-005~008 的失败步即便事件流带 `thread_id` 也不落 `sessionId`——此为实现取舍（失败步无回放价值），其单步逻辑交单测，本文不专门断言。
