# workflow 运行 测试用例

覆盖 conduct 中**按 DAG 并行跑工作流、查运行记录、终止运行**的一族命令：`workflow run`、`run list`、`run show`、`run stop`。工作流定义的增删改查见 [workflow-editing.md](./workflow-editing.md) / [workflow-node-editing.md](./workflow-node-editing.md)；聚合视图 `ui` 的 CLI 冒烟见本文 TC-012，其服务端与 `/api/*` 端点的黑盒覆盖见 [ui-server.md](./ui-server.md)；中断恢复 `run resume` 见 [run-resume.md](./run-resume.md)。对应 spec：[docs/specs/cli-runtime.md](../specs/cli-runtime.md)（`ui` 见 [cli-tooling.md](../specs/cli-tooling.md)〈ui〉）。

> **模型基线**：一次运行按**节点 + 边的 DAG 依赖**并行调度——`START` 同时扇出的节点在 t0 一起开跑，某节点的全部前驱成功后它才就绪；无并发上限。落盘的 `trace.jsonl` 以 `nodeId` 为主键（无 `stepIndex`），`run.json` 无 `steps` 字段——进度分母由快照按 agent 节点数（排除 START/END）算得，`run list` 展示为 `NODES` 列（JSON `nodeCount`）。

> **💸 费钱标记**：带 💸 的用例会真实调用 AI 引擎、消耗 token。除个别（TC-015 需 agent 执行一条 `sleep`）外，其 workflow 都是「回一个词」的极简节点、单步秒级完成，仅作打通验证，**请勿改成大重构 / 写复杂页面 / 长耗时任务**。不带 💸 的用例（缺参报错、空列表、引擎坏掉、drain 失败、不存在 id 等）不产生真实引擎调用、零 token。

> **隔离机制（关键，两套）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。
> - **非 💸 用例**：把 `HOME` 重定向到临时目录（`export HOME="$WORK"`），store 落在 `$WORK/.conduct/`，用例结束连目录一并删除。
> - **💸 用例**：要调真实引擎，而**引擎登录态搬不进临时 HOME**——macOS 上 `claude` 的 OAuth 凭据存在系统 **Keychain**、不随 `HOME` 走，把 `~/.claude*` 拷进临时 HOME 只会得到「假性未登录」。故 💸 用例**在真实家目录（引擎已登录）里运行**，靠「唯一 workflow 名 + 用后精确删除」隔离，不残留（见〈环境准备〉的 `cleanup_run`）。引擎的文件读写另用一个临时 `--cwd` 目录隔离。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
```

💸 用例额外需要：本机**真实家目录**已安装并登录被测引擎的无头 CLI（默认 `claude-code` → `claude`，登录态在 Keychain + `~/.claude.json`）。真机确实未装 / 未登录时这些用例才应报引擎调用失败（退出 `1`），属环境未就绪、非用例本身问题。

**💸 用例的精确清理助手**（在真实 HOME 里跑，用后删除本用例造的 workflow 及其全部 run 记录，避免污染真实 store）：

```bash
cleanup_run() {   # 用法：cleanup_run <workflow 名>
  rm -f  "$HOME/.conduct/workflows/$1.json"
  rm -rf "$HOME/.conduct/runs/$1-"*
}
```

💸 用例复用的**最小 workflow 定义**（单 agent 节点、只回一个词，含 START/END）：

```bash
# 各 💸 用例前置里写入 $PROJ/min.json（$PROJ 为该用例的临时 --cwd 目录）
write_min() {   # 用法：write_min   —— 把最小定义写到 $PROJ/min.json
  cat > "$PROJ/min.json" <<'JSON'
{
  "nodes": [
    { "id": "START" },
    {
      "id": "say",
      "displayName": "打招呼",
      "engine": "claude-code",
      "promptTemplate": "只回复一个词：hello。不要读写任何文件、不要做别的事。需求：{{sys.userPrompt}}"
    },
    { "id": "END" }
  ],
  "edges": [
    { "from": "START", "to": "say" },
    { "from": "say", "to": "END" }
  ]
}
JSON
}
```

---

## workflow run

### TC-001 💸 run 端到端跑通最小工作流

- **目的**：验证 `workflow run <name> "<需求>"` 按 DAG 调度、驱动引擎、落盘 trace 并给出完成提示。
- **前置**：
  1. 真实家目录的 `claude` CLI 已装并登录。
  2. `PROJ=$(mktemp -d)`（引擎工作目录）；`write_min`（写 `$PROJ/min.json`）。
  3. `cat "$PROJ/min.json" | "$CONDUCT" workflow create hello --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow run hello "打个招呼" --cwd "$PROJ"; echo "exit=$?"`
  2. `ls "$HOME/.conduct/runs/" | command grep '^hello-'`
  3. `python3 -c 'import json,glob,os; f=glob.glob(os.path.expanduser("~/.conduct/runs/hello-*/run.json"))[0]; d=json.load(open(f)); print(d["status"], d["workflow"])'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 先打印 `▶ 调度 1 个节点（START 扇出：say 同刻开跑）`，随后 `▶ say [打招呼] 开跑 · engine=claude-code`、`✓ say 完成 · ...`，末行 `✅ 完成，阅读 <.../run-summary.md> 获取运行详情。`。
  - 步骤 2 出现一个 run 目录，名形如 `hello-YYYYMMDD-HHMMSS`；其中含 `run.json`、`run-summary.md`、`trace.jsonl` 三文件。
  - 步骤 3 打印 `completed hello`（`run.json` 无 `steps` 字段，进度分母改由快照按 agent 节点数算）。
  - 归一化说明：耗时 / token 数 / 时间戳 / run id 中的时间后缀每次都变，只校验存在与格式，不逐字比对。
- **清理**：`cleanup_run hello; rm -rf "$PROJ"`。

### TC-002 💸 run 需求从 stdin 读取

- **目的**：验证省略位置参数、需求改从 stdin（管道）读取。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create hi --definition`。
- **步骤**：
  1. `echo "从 stdin 来的需求" | "$CONDUCT" workflow run hi --cwd "$PROJ"; echo "exit=$?"`
  2. `python3 -c 'import json,glob,os; f=glob.glob(os.path.expanduser("~/.conduct/runs/hi-*/run.json"))[0]; print(json.load(open(f))["userPrompt"].strip())'`
- **预期**：
  - 步骤 1 退出码 `0`；正常调度并完成，末行 `✅ 完成，阅读 ...`。
  - 步骤 2 打印 `从 stdin 来的需求`（`run.json` 的 `userPrompt`，尾随换行已 strip）。
- **清理**：`cleanup_run hi; rm -rf "$PROJ"`。

### TC-003 💸 run --json 逐节点输出事件（TraceEntry，无 stepIndex）

- **目的**：验证 `--json` 每节点落定吐出一行事件 JSON（无进度装饰），每行即 `trace.jsonl` 的一条记录——主键为 `nodeId`，不再是 `stepIndex`。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create je --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow run je "hi" --cwd "$PROJ" --json > "$PROJ/out.jsonl"; echo "exit=$?"`
  2. `python3 -c 'import json;[json.loads(l) for l in open("'"$PROJ"'/out.jsonl") if l.strip()]; print("all_lines_json_ok")'`
  3. `python3 -c 'import json; d=json.loads(open("'"$PROJ"'/out.jsonl").readline()); print(d["nodeId"], d["success"], "stepIndex" not in d, "startedAt" in d, "endedAt" in d)'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 打印 `all_lines_json_ok`（每行都是合法 JSON，无 `▶`/`✓` 等人类装饰行）。
  - 步骤 3 打印 `say True True True True`（首行事件的 `nodeId`/`success`；确认无 `stepIndex` 字段、含 `startedAt`/`endedAt`）。
- **清理**：`cleanup_run je; rm -rf "$PROJ"`。

### TC-004 run 缺需求且 stdin 是终端时报错、不挂起（零成本，pty 伪终端驱动）

- **目的**：验证既无位置参数、stdin 又是**终端**时报参数缺失、退出 `2`，不静默挂起、不调用引擎。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. `PROJ="$WORK"; write_min`；`cat "$PROJ/min.json" | "$CONDUCT" workflow create np --definition`。
- **步骤**：
  1. （真 TTY 分支，pty 伪终端驱动，可无人值守自动化）把 stdin 接成真终端后运行 `run`、不喂任何输入，用超时守卫验证它立即报错返回而非挂起：

     ```bash
     python3 - "$CONDUCT" "$WORK" <<'PY'
     import os, pty, subprocess, sys
     conduct, work = sys.argv[1], sys.argv[2]
     master, slave = pty.openpty()            # 分配伪终端；slave 端 os.isatty()=True
     p = subprocess.Popen([conduct, "workflow", "run", "np"],
                          stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
     os.close(slave)
     try:
         out, err = p.communicate(timeout=5)  # 超时守卫：若停在等待输入即为 bug
         print(f"exit={p.returncode}"); print("stderr:", err.strip())
     except subprocess.TimeoutExpired:
         p.kill(); print("exit=HANG(FAIL)")
     os.close(master)
     runs = os.path.join(work, ".conduct", "runs")
     real = [x for x in (os.listdir(runs) if os.path.isdir(runs) else []) if not x.startswith(".")]
     print("runs=", real)                     # 应为空：未触发引擎、零 token
     PY
     ```
- **预期**：
  - 脚本**立即返回、不停在等待输入**（不打印 `HANG`）。
  - 打印 `exit=2`，stderr 含 `缺少用户需求`。
  - 打印 `runs= []`——未产生 run 记录（未触发引擎、零 token）。
  - **关键说明**：本用例断言的是 **stdin 是终端**这一路径，**切勿用 `< /dev/null` 代替**（那是非 TTY 空需求路径，行为不同）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow run 并行 DAG 调度（零成本，假引擎）

以下三例用假引擎（零 token）验证 spec〈执行模型：并行 DAG 调度器〉的核心行为：`START` 扇出并行开跑、菱形汇聚等前驱都完成才就绪、drain 失败语义。真实引擎的端到端打通已由 TC-001~003 覆盖，这里换假引擎是为了**可控地制造耗时差与失败**而不花钱——引擎是 conduct 的外部依赖，替身不影响 conduct 自身产出的 run 记录真实性。

### TC-005 START 扇出并行开跑，菱形汇聚等待全部前驱

- **目的**：验证 `START` 同时扇出到两个节点时二者在 t0 一起开跑（`startedAt` 相同），下游汇聚节点等两个前驱都成功才就绪开跑；数据流转断言汇聚节点的输入确实同时含两个分支的产物。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装一个「打印固定 JSON、零 token」的假 claude：
     ```bash
     mkdir -p "$WORK/fakebin"
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null
     echo '{"result":"HELLO-ARTIFACT","is_error":false,"usage":{"input_tokens":3,"output_tokens":2}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     export PATH="$WORK/fakebin:$PATH"
     ```
  3. 造 `START` 扇出到 `a`/`b`、汇聚到 `c` 的菱形定义并跑一条：
     ```bash
     cat > "$WORK/dag.json" <<'JSON'
     {
       "nodes": [
         { "id": "START" },
         { "id": "a", "displayName": "调研", "engine": "claude-code", "promptTemplate": "A: {{sys.userPrompt}}" },
         { "id": "b", "displayName": "起草", "engine": "claude-code", "promptTemplate": "B: {{sys.userPrompt}}" },
         { "id": "c", "displayName": "实现", "engine": "claude-code", "promptTemplate": "C 用 {{a}} 和 {{b}}" },
         { "id": "END" }
       ],
       "edges": [
         {"from":"START","to":"a"},{"from":"START","to":"b"},{"from":"a","to":"c"},{"from":"b","to":"c"},{"from":"c","to":"END"}
       ]
     }
     JSON
     cat "$WORK/dag.json" | "$CONDUCT" workflow create dag --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow run dag "需求X" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | command grep '^dag-')`
  3. 断言并行同刻开跑与汇聚数据流转：
     ```bash
     python3 -c '
     import json
     trace = [json.loads(l) for l in open("'"$WORK"'/.conduct/runs/'"$RID"'/trace.jsonl") if l.strip()]
     by_id = {e["nodeId"]: e for e in trace}
     print("a_started=", by_id["a"]["startedAt"])
     print("b_started=", by_id["b"]["startedAt"])
     print("a_b_same_start=", by_id["a"]["startedAt"] == by_id["b"]["startedAt"])
     print("c_input_has_both=", "HELLO-ARTIFACT" in by_id["c"]["input"])
     print("c_started_after_a_and_b=", by_id["c"]["startedAt"] >= by_id["a"]["endedAt"] and by_id["c"]["startedAt"] >= by_id["b"]["endedAt"])
     '
     ```
- **预期**：
  - 步骤 1 退出码 `0`；stdout 首行含 `▶ 调度 3 个节点（START 扇出：a、b 同刻开跑）`，随后 `a`、`b` 各自的 `▶ 开跑`/`✓ 完成` 交错出现，`c` 在二者之后才 `▶ 开跑`，末行 `✅ 完成，阅读 ...`。
  - 步骤 3 打印 `a_b_same_start= True`（`a`、`b` 的 `startedAt` 相同，证明二者由 `START` 同刻一起开跑）、`c_input_has_both= True`（`c` 的 `input` 里两次出现产物文本，来自 `{{a}}` 与 `{{b}}` 两次替换）、`c_started_after_a_and_b= True`（`c` 直到 `a`、`b` 都结束才开跑）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-006 drain 失败语义：一分支失败不影响另一分支跑完，下游不派发

- **目的**：验证失败置位后**不再调度新节点**，但**已在途的节点跑完**（drain）——用一个立即失败、一个耗时数秒成功的两个并行分支验证：失败发生在先，仍等成功分支跑完才整体收尾 `failed`；依赖两分支的下游节点因其中一支失败而**从未开跑**。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装一个按输入分流的假引擎：`NODE_A` 立即失败，`NODE_B` sleep 2 秒后成功：
     ```bash
     mkdir -p "$WORK/fakebin"
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     body=$(cat)
     if printf '%s' "$body" | grep -q 'NODE_A'; then
       echo "claude: 节点A故意失败" >&2
       exit 1
     fi
     if printf '%s' "$body" | grep -q 'NODE_B'; then
       sleep 2
       echo '{"result":"B-OK","is_error":false,"usage":{}}'
       exit 0
     fi
     echo '{"result":"OK","is_error":false,"usage":{}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     export PATH="$WORK/fakebin:$PATH"
     ```
  3. 造 `START` 扇出到 `a`/`b`、汇聚到 `c` 的定义：
     ```bash
     cat > "$WORK/dag.json" <<'JSON'
     {
       "nodes": [
         { "id": "START" },
         { "id": "a", "displayName": "A", "engine": "claude-code", "promptTemplate": "NODE_A" },
         { "id": "b", "displayName": "B", "engine": "claude-code", "promptTemplate": "NODE_B" },
         { "id": "c", "displayName": "C", "engine": "claude-code", "promptTemplate": "C 用 {{a}} {{b}}" },
         { "id": "END" }
       ],
       "edges": [
         {"from":"START","to":"a"},{"from":"START","to":"b"},{"from":"a","to":"c"},{"from":"b","to":"c"},{"from":"c","to":"END"}
       ]
     }
     JSON
     cat "$WORK/dag.json" | "$CONDUCT" workflow create dag --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow run dag "x" --cwd "$WORK"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | command grep '^dag-')`
  3. `python3 -c '
     import json
     trace = [json.loads(l) for l in open("'"$WORK"'/.conduct/runs/'"$RID"'/trace.jsonl") if l.strip()]
     ids = sorted(e["nodeId"] for e in trace)
     by_id = {e["nodeId"]: e for e in trace}
     print("node_ids_seen=", ids)                 # c 不应出现：其前驱之一失败，从未派发
     print("a_success=", by_id["a"]["success"])
     print("b_success=", by_id["b"]["success"])
     '`
  4. `python3 -c 'import json;d=json.load(open("'"$WORK"'/.conduct/runs/'"$RID"'/run.json"));print(d["status"], d["error"])'`
- **预期**：
  - 步骤 1 退出码 `1`；stdout/stderr 含 `✗ a 失败 ...` 与 `✓ b 完成 ...`（drain：failed 置位后仍等 b 跑完才收尾，故两行都出现，`b` 的完成事件在 `a` 的失败事件之后打印）。
  - 步骤 3 打印 `node_ids_seen= ['a', 'b']`（`c` 从未被派发——其前驱之一 `a` 失败，永远等不到齐）、`a_success= False`、`b_success= True`（drain 期间跑完的产物照记）。
  - 步骤 4 打印 `failed claude exited with code 1: claude: 节点A故意失败`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## run list

### TC-007 run list 无记录时提示（零成本）

- **目的**：验证无运行记录时 `run list` 给提示、退出 `0`。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run list; echo "exit=$?"`
- **预期**：
  - 退出码 `0`，stdout 含 `（暂无运行记录）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 💸 run list 列出真实跑出的运行记录（NODES 列 = agent 节点数）

- **目的**：验证 `run list` 从 `runs/` 解析并按表格 / `--json` 列出——记录由真实 `workflow run` 产出；`NODES` 列 / JSON `nodeCount` 均为 agent 节点数（排除 START/END）。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create rl --definition`。
  3. `"$CONDUCT" workflow run rl "打个招呼" --cwd "$PROJ" >/dev/null`（真跑一条 completed run）。
- **步骤**：
  1. `"$CONDUCT" run list; echo "exit=$?"`
  2. `"$CONDUCT" run list --json | python3 -c 'import sys,json; d=[x for x in json.load(sys.stdin) if x["workflow"]=="rl"][0]; print(d["workflow"], d["status"], d["nodeCount"])'`
- **预期**：
  - 步骤 1 退出码 `0`；表格有表头 `RUN ID`/`WORKFLOW`/`STATUS`/`NODES`/`STARTED`/`PROMPT`，含一行同时出现 `rl`、`completed`、`1`、`打个招呼` 各字段；`RUN ID` 形如 `rl-YYYYMMDD-HHMMSS`。
  - 步骤 2 打印 `rl completed 1`（`nodeCount` = 1 个 agent 节点，不含 START/END）。
- **清理**：`cleanup_run rl; rm -rf "$PROJ"`。

---

## run show

### TC-009 run show 默认打印运行总结（run-summary.md 全文，节点表按 startedAt 排序）

- **目的**：验证 `run show <id>`（默认、不加 `--trace`）打印 `run-summary.md` 全文——概要头 + **节点**表（不再是「步骤」表、无 stepIndex 列）+ 逐节点完整产物。
- **前置**（**零 💸**：`run show` 只读回落盘数据、**不调引擎**，故用一个「打印固定 JSON、零 token」的确定性假引擎顶替真 claude）：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装假引擎：
     ```bash
     mkdir -p "$WORK/fakebin"
     cat > "$WORK/fakebin/claude" <<'SH'
     #!/usr/bin/env bash
     cat > /dev/null   # 吞掉 stdin（conduct 从 stdin 喂 prompt）
     echo '{"result":"HELLO-ARTIFACT","is_error":false,"usage":{"input_tokens":3,"output_tokens":2}}'
     SH
     chmod +x "$WORK/fakebin/claude"
     export PATH="$WORK/fakebin:$PATH"
     ```
  3. 造最小工作流并真跑一条（假引擎秒级成功 → `completed`）：
     ```bash
     cat > "$WORK/min.json" <<'JSON'
     {"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"},{"id":"END"}],
      "edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create ok --definition
     "$CONDUCT" workflow run ok "打个招呼" --cwd "$WORK" >/dev/null
     RID=$(ls "$WORK/.conduct/runs/" | command grep '^ok-' | head -1)
     ```
- **步骤**：
  1. `"$CONDUCT" run show "$RID"; echo "exit=$?"`
  2. `"$CONDUCT" run show "$RID" | command grep -c 'HELLO-ARTIFACT'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 是 `run-summary.md` 全文：首行 `# ok-<时间戳>`；含 `**工作流** ok · 1 节点`、`**需求** 打个招呼`、`**状态** ✅ completed …`；有 `## 节点` 表（表头 `| 节点 | 引擎 | 起 → 止 | 耗时 |`，一行以 `| 打招呼 | claude-code |` 打头——**不再有 stepIndex 列**）；有 `## 产物` 段，内含 `<output node="say" name="打招呼">` 包裹的**完整**产物。
  - 步骤 2 打印 `1`——默认视图即含完整产物 `HELLO-ARTIFACT`（总结给全文、不截断）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：`pkill -f "$WORK/fakebin/claude" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 run show --trace 打印状态摘要 + 每节点完整 input/output（按 startedAt 排序）

- **目的**：验证 `--trace` 改变**深度**：打印状态摘要后，逐节点展开完整 input 与 output（按 `startedAt` 排序还原时间线），节点标题行形如 `● <id> [<displayName>] <engine>  成功`（无 stepIndex / type 字段）。
- **前置**：同 TC-009 前置 1-3（临时 HOME + 确定性假引擎 + 跑一条 `completed` run），但 workflow 名改用 `tr`：`cat "$WORK/min.json" | "$CONDUCT" workflow create tr --definition`、`"$CONDUCT" workflow run tr "打个招呼" --cwd "$WORK" >/dev/null`、`RID=$(ls "$WORK/.conduct/runs/" | command grep '^tr-' | head -1)`。
- **步骤**：
  1. `"$CONDUCT" run show "$RID" --trace; echo "exit=$?"`
  2. `"$CONDUCT" run show "$RID" --trace | command grep -c 'HELLO-ARTIFACT'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 首为状态摘要（`运行 tr-<时间戳> · completed`、`需求：打个招呼`、`节点 1 · 耗时 …`），随后一行 `● say [打招呼] claude-code  成功`，其下 `  ── input ──` 段为该节点完整输入（含 `回复：hi。需求：打个招呼`）、`  ── output ──` 段为该节点完整产物。
  - 步骤 2 打印 `1`——`--trace` 的 output 段含完整产物 `HELLO-ARTIFACT`。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：`pkill -f "$WORK/fakebin/claude" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 💸 run show --json 输出规范化 run.json（无 steps / stepIndex）

- **目的**：验证 `--json` 输出 `run.json` 规范化内容（无 `steps` 字段）；`--json --trace` 组合额外含 `"trace":[…]`，每条记录以 `nodeId` 为主键。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create js --definition`。
  3. `"$CONDUCT" workflow run js "hi" --cwd "$PROJ" >/dev/null`；`RID=$(ls "$HOME/.conduct/runs/" | command grep '^js-' | head -1)`。
- **步骤**：
  1. `"$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["workflow"], d["status"], "trace" in d, "steps" in d)'`
  2. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; d=json.load(sys.stdin); print("trace_len", len(d["trace"]), d["trace"][0]["nodeId"])'`
- **预期**：
  - 步骤 1 打印 `js completed False False`（不加 `--trace` 的 `--json` 只给 run.json 概要，无 `trace` 字段；也**无 `steps` 字段**）。
  - 步骤 2 打印 `trace_len 1 say`（`--json --trace` 附上 `trace.jsonl` 逐行，首条 `nodeId` 为 `say`）。
- **清理**：`cleanup_run js; rm -rf "$PROJ"`。

### TC-012 引擎坏掉时节点失败、失败信息落盘并可查（零成本）

- **目的**：验证被测引擎的二进制**不可用 / 报错退出**时，conduct 让该节点失败、把失败信息**真实落盘**（`run.json` 的 `status:"failed"`+`error`，`trace.jsonl` 该节点 `success:false`+`error`），且 `run show` 能呈现失败。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。引擎会立即失败、不真调 API，故无需登录、零 token。
  2. **弄坏引擎**：在 PATH 前置一个「一运行就报错退出」的假 `claude`：
     ```bash
     mkdir -p "$WORK/brokenbin"
     cat > "$WORK/brokenbin/claude" <<'SH'
     #!/usr/bin/env bash
     echo "claude: 引擎不可用（模拟故障）" >&2
     exit 1
     SH
     chmod +x "$WORK/brokenbin/claude"
     OLD_PATH="$PATH"; export PATH="$WORK/brokenbin:$PATH"
     ```
  3. `PROJ="$WORK"; write_min`；`cat "$PROJ/min.json" | "$CONDUCT" workflow create bf --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow run bf "会失败" --cwd "$PROJ"; echo "exit=$?"`
  2. `RID=$(ls "$WORK/.conduct/runs/" | command grep '^bf-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; d=json.load(sys.stdin); tr=d["trace"]; print(d["status"], "|", d["error"], "|", tr[0]["nodeId"], tr[0]["success"])'`
  4. `python3 -c 'import json,glob,os; p=glob.glob(os.path.expanduser("~/.conduct/runs/bf-*"))[0]; t=[json.loads(l) for l in open(p+"/trace.jsonl") if l.strip()]; print(t[0]["success"], "|", t[0]["error"])'`
  5. `"$CONDUCT" run show "$RID"; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`；stdout/stderr 报该节点失败。
  - 步骤 3 打印 `failed | claude exited with code 1: claude: 引擎不可用（模拟故障） | say False`（`status:"failed"`、`error` 以引擎二进制名 `claude` 打头；失败节点由 trace 的 `nodeId=say success=false` 记录体现）。
  - 步骤 4 打印 `False | claude exited with code 1: claude: 引擎不可用（模拟故障）`。
  - 步骤 5 退出码 `0`；`run show` 呈现状态 `failed`、失败节点 `say`、错误摘要。
- **清理**：`export PATH="$OLD_PATH"; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-013 run show 不存在的 id 报错（零成本）

- **目的**：验证查询不存在 run 时失败。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run show no-such-000000; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `no-such-000000: 运行不存在`（实际形如 `conduct: no-such-000000: 运行不存在`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## ui

### TC-014 ui 启动并打印入口地址（交互 / 半自动）

- **目的**：验证 `conduct ui` 启动可视化界面、stdout 打印入口地址、进程驻留至被中断。
- **前置**：建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. 后台启动，等待后先探活、再抓取输出、后中断：
     ```bash
     "$CONDUCT" ui > "$WORK/ui.log" 2>&1 &
     UIPID=$!
     sleep 2
     kill -0 "$UIPID" 2>/dev/null && echo "ui_alive"   # 中断前先确认进程仍驻留
     kill "$UIPID" 2>/dev/null
     wait "$UIPID" 2>/dev/null                          # 回收，避免僵尸进程
     cat "$WORK/ui.log"
     ```
- **预期**：
  - 打印 `ui_alive`——`sleep 2` 后进程仍活着（未自行退出），证明它驻留而非一闪而过。
  - `ui.log` 含启动横幅与一个入口地址（形如 `http://127.0.0.1:<port>`；端口每次可能不同，只校验 `http://127.0.0.1:` 前缀，不比对端口号）。
  - **说明**：服务端启动的错误路径（端口占用 / store 不可读）与 `/api/*` 全端点的黑盒覆盖见 [ui-server.md](./ui-server.md)，本文不重复。
  - 归一化说明：端口号非确定，忽略；本用例含时序成分，非纯确定性，必要时人工在终端 `conduct ui` 目视确认并 `Ctrl-C` 退出。
- **清理**：`kill "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 补充：运行中（running）状态可见

### TC-015 💸 运行途中 run list / run show 显示 running，完成后转 completed

- **目的**：验证 workflow 运行**在途时**，另开查询能看到 `status:"running"`；进程结束后同一条记录转 `completed`。
- **前置**：
  1. 真实家目录 `claude` 已就绪（agent 需能执行 shell 命令）；`PROJ=$(mktemp -d)`。
  2. 造一个「让 agent 先 sleep 再回话」的 workflow（把该节点拖到约 8 秒，留出查询窗口）：
     ```bash
     cat > "$PROJ/slow.json" <<'JSON'
     {
       "nodes": [
         { "id": "START" },
         { "id": "say", "displayName": "慢答", "engine": "claude-code",
           "promptTemplate": "请先执行 shell 命令 `sleep 8`，等它执行完，再只回复一个词：DONE。不要读写任何文件、不要做别的事。" },
         { "id": "END" }
       ],
       "edges": [ { "from": "START", "to": "say" }, { "from": "say", "to": "END" } ]
     }
     JSON
     cat "$PROJ/slow.json" | "$CONDUCT" workflow create sl --definition
     ```
- **步骤**：
  1. 后台起跑，留查询窗口：
     ```bash
     "$CONDUCT" workflow run sl "慢慢来" --cwd "$PROJ" >/dev/null 2>&1 &
     RUNPID=$!
     sleep 3   # 等 run.json 落盘 running、且 agent 仍卡在 sleep
     ```
  2. 运行途中查列表与详情（取 status）：
     ```bash
     "$CONDUCT" run list | command grep '^sl-\|STATUS'
     python3 -c 'import json,glob,os; d=json.load(open(sorted(glob.glob(os.path.expanduser("~/.conduct/runs/sl-*/run.json")))[-1])); print("mid_status=", d["status"], "pid_present=", isinstance(d.get("pid"), int))'
     ```
  3. 等跑完再查终态：
     ```bash
     wait "$RUNPID"
     python3 -c 'import json,glob,os; d=json.load(open(sorted(glob.glob(os.path.expanduser("~/.conduct/runs/sl-*/run.json")))[-1])); print("final_status=", d["status"])'
     ```
- **预期**：
  - 步骤 2：`run list` 有一行 `sl-...` 且 `STATUS` 列为 `running`；python 打印 `mid_status= running pid_present= True`。
  - 步骤 3：`final_status= completed`——同一条记录在进程结束后转终态。
  - 归一化说明：本用例含时序，依赖 agent 真的执行了 `sleep 8`。若 agent 不听话（没 sleep、秒回）导致查询窗口错过 running，会看到 `completed`——此时调大 `sleep` 秒数与 prompt 里的时长后重跑。
- **清理**：`cleanup_run sl; rm -rf "$PROJ"`。

---

## 补充：workflow run 的 --cwd 与空需求校验（零成本）

显式 `--cwd` 做「已存在的目录」校验——不存在 / 不是目录即报用法错误退 `2`，发射前拦下、不烧引擎；位置参数需求为空白也与 stdin 路径同标准退 `2`。三者都在载入工作流、调引擎**之前**触发（甚至工作流不存在也照样先报这些），故全零成本、全隔离临时 HOME。此三条校验不受本次 DAG 模型改造影响。

### TC-016 workflow run --cwd 指向不存在的路径 → 退 2

- **目的**：验证显式 `--cwd` 指向不存在的路径时报用法错误退 `2`，不带着错误目录去烧引擎。
- **前置**：建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。（无需建工作流：该校验在载入工作流前触发。）
- **步骤**：
  1. `"$CONDUCT" workflow run any "hi" --cwd "$WORK/no-such-dir" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `--cwd 指向的路径不存在：` 且带该绝对路径（路径子串归一化，不逐字比对）。
  - 未产生任何 run 记录：`$WORK/.conduct/runs/` 为空或不存在。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-017 workflow run --cwd 指向非目录（文件）→ 退 2

- **目的**：验证 `--cwd` 指向一个**存在但不是目录**的路径时报用法错误退 `2`。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；造一个普通文件：`: > "$WORK/afile"`。
- **步骤**：
  1. `"$CONDUCT" workflow run any "hi" --cwd "$WORK/afile" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `--cwd 不是目录：` 且带该文件路径。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-018 workflow run 位置参数需求为空白 → 退 2

- **目的**：验证位置参数需求为纯空白（`TrimSpace` 后为空）时报用法错误退 `2`，不带空需求去烧引擎（与 stdin 空需求同标准）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" workflow run any "   " --cwd "$WORK" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `用户需求不能为空`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## run stop（终止运行）

`run stop <id>` 向运行的进程发 SIGTERM（先按进程组、非组长回退单进程）。**仅 `running` 可终止**：不存在 / 已终态 / `running` 但 pid 已死（interrupted）均报错退 `1`；终止后不落新状态，进程停写、pid 判活派生为 `interrupted`。以下三例全零成本：错误路径不调引擎；happy path 用一个「只 sleep、零 token」的**假慢引擎**把 run 拖在 `running`，供 stop 命中。此命令语义不受本次 DAG 模型改造影响。

### TC-019 run stop 不存在的 id → 退 1

- **目的**：验证终止一个不存在的 run 时报错退 `1`。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run stop no-such-000000 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `no-such-000000: 运行不存在`（实际形如 `conduct: no-such-000000: 运行不存在`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-020 run stop 已终结（failed）的运行 → 退 1（仅 running 可终止）

- **目的**：验证对**已终态**的 run 调 `run stop` 报「无可终止」退 `1`。用弄坏引擎造一条真实的 `failed` run（零 token，同 TC-012）。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. **弄坏引擎**：PATH 前置一个「一运行就报错退出」的假 `claude`：
     ```bash
     mkdir -p "$WORK/brokenbin"
     cat > "$WORK/brokenbin/claude" <<'SH'
     #!/usr/bin/env bash
     echo "claude: 引擎不可用（模拟故障）" >&2
     exit 1
     SH
     chmod +x "$WORK/brokenbin/claude"
     export PATH="$WORK/brokenbin:$PATH"
     ```
  3. 造最小工作流并真跑一条（引擎秒级失败 → `failed`）：
     ```bash
     cat > "$WORK/min.json" <<'JSON'
     {"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"},{"id":"END"}],
      "edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create bf --definition
     "$CONDUCT" workflow run bf "会失败" --cwd "$WORK" >/dev/null 2>&1
     RID=$(ls "$WORK/.conduct/runs/" | command grep '^bf-' | head -1)
     ```
- **步骤**：
  1. `"$CONDUCT" run stop "$RID" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `当前状态为 failed，无可终止（仅 running 可终止）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-021 run stop 终止运行中的运行 → 退 0，转 interrupted

- **目的**：验证对一条**运行中**的 run 调 `run stop`：命令退 `0` 并提示已发送 SIGTERM；被终止的进程停写、不落新状态，此后 `run list` / `run show` 按 pid 判活**派生**为 `interrupted`（run.json 存储态仍为 `running`）；且 `run show` 默认视图在未收尾时给状态摘要 + 「运行总结尚未生成」提示（进度写作 `节点 k/N`，不再是 `step k/N`）。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装一个「只 sleep、零 token」的**假慢引擎**（把该节点长时间拖在 `running`，留出终止窗口；conduct 在解析其输出前就会被终止，故 sleep 后随便回什么都行）：
     ```bash
     mkdir -p "$WORK/slowbin"
     cat > "$WORK/slowbin/claude" <<'SH'
     #!/usr/bin/env bash
     sleep 30
     echo '{"result":"DONE","is_error":false,"usage":{}}'
     SH
     chmod +x "$WORK/slowbin/claude"
     export PATH="$WORK/slowbin:$PATH"
     ```
  3. 造最小工作流：
     ```bash
     cat > "$WORK/min.json" <<'JSON'
     {"nodes":[{"id":"START"},{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"},{"id":"END"}],
      "edges":[{"from":"START","to":"say"},{"from":"say","to":"END"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create ss --definition
     ```
- **步骤**：
  1. 后台起跑，等 run.json 落盘 `running`：
     ```bash
     "$CONDUCT" workflow run ss "慢慢来" --cwd "$WORK" >/dev/null 2>&1 &
     RUNPID=$!
     sleep 2
     RID=$(ls "$WORK/.conduct/runs/" | command grep '^ss-' | head -1); echo "RID=$RID"
     ```
  2. 途中确认为 running，再终止：
     ```bash
     "$CONDUCT" run list | command grep -E 'ss-|STATUS'
     "$CONDUCT" run stop "$RID"; echo "exit=$?"
     ```
  3. 终止后查派生态：
     ```bash
     sleep 1
     python3 -c 'import json,glob,os; d=json.load(open(glob.glob(os.path.expanduser("~/.conduct/runs/ss-*/run.json"))[0])); print("stored_status=", d["status"])'
     "$CONDUCT" run list | command grep -E 'ss-'
     "$CONDUCT" run show "$RID"
     ```
- **预期**：
  - 步骤 1：`RID=ss-<时间戳>`（时间后缀忽略）。
  - 步骤 2：`run list` 有一行 `ss-...` 且 `STATUS` 列为 `running`；`run stop` 退出码 `0`，stdout 形如 `已向运行 ss-…（pid <n>）发送终止信号 SIGTERM。`（pid 值忽略）。
  - 步骤 3：`stored_status= running`（run.json 存储态未改，符合「不落新状态」）；但 `run list` 该行 `STATUS` 现派生为 `interrupted`；`run show` 打印状态摘要（`运行 ss-… · interrupted`、`需求：慢慢来`、`节点 1 · 进度 节点 0/1 · … 起`）并附一行 `运行总结尚未生成（运行未收尾）；用 conduct run show ss-… --trace 查看已执行步骤。`。
  - 归一化说明：本用例含时序，依赖 `run stop` 在 `sleep 30` 窗口内命中 running（`sleep 2` 已足够 run.json 落盘）。pid 值、run id 时间后缀忽略。
- **清理**（务必清掉遗留的假 sleep 子进程与后台 conduct）：
  ```bash
  kill "$RUNPID" 2>/dev/null; wait "$RUNPID" 2>/dev/null
  pkill -f "$WORK/slowbin/claude" 2>/dev/null
  export HOME="$OLD_HOME"; rm -rf "$WORK"
  ```

---

## 补充：图片输入的 help 文案（零 token，只读）

conduct **不提供图片旗标、也不做 URL 下载**：给引擎看图片的方式是把图片的**本地绝对路径**直接写进需求文本，各引擎自带的文件工具自行读取。此约定须在 `workflow run --help` 里向用户交代清楚；不受本次 DAG 模型改造影响。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈图片输入〉、[cli-runtime.md](../specs/cli-runtime.md)〈workflow run〉。

### TC-022 workflow run --help 说明「把图片本地绝对路径写进需求文本」

- **目的**：验证 `workflow run --help` 的说明文案覆盖图片输入的三个要点——① 把图片**本地绝对路径**写进需求文本；② conduct **不提供图片旗标**；③ **不做 URL 下载**。
- **前置**：无（只读，`--help` 不触碰 store、不调引擎、零 token）。
- **步骤**：
  1. `"$CONDUCT" workflow run --help; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout（help 文本）同时含以下关键子串（用 `command grep -q` 逐条校验，不比对整段排版）：
    - `本地绝对路径`（写进需求文本的方式）；
    - `不提供图片旗标`（conduct 无 `--image` 之类的旗标）；
    - `不做 URL 下载`（不替用户抓取网络图片）。
  - 一条命令校验：
    ```bash
    "$CONDUCT" workflow run --help | command grep -q 本地绝对路径 \
      && "$CONDUCT" workflow run --help | command grep -q 不提供图片旗标 \
      && "$CONDUCT" workflow run --help | command grep -q '不做 URL 下载' \
      && echo "help_image_ok=yes" || echo "help_image_ok=no"
    ```
    应打印 `help_image_ok=yes`。
- **清理**：无。
