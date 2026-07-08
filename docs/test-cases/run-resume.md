# 中断恢复（run resume）测试用例

覆盖 conduct 的**中断续跑**能力：CLI `conduct run resume <id>`（前台 / `-d` 后台）与 UI 重跑端点 `POST /api/runs/{id}/resume`。语义是「恢复一次 **failed 或 interrupted** 的运行——跳过前面已成功的步骤，从 trace 推断出的中断处整步重跑、续到终态，续写**同一** run（id 不变）、保留旧 trace 作审计」。对应 spec：[docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈run resume〉、[docs/specs/ui.md](../specs/ui.md)（运行详情「重跑」按钮与 `POST /api/runs/{id}/resume`）。前台 `workflow run`、`run list/show/stop`、`-d` 与 UI 其它端点的既有行为分别见 [workflow-running.md](./workflow-running.md)、[detached-run.md](./detached-run.md)、[ui-server.md](./ui-server.md)，本文不重复。

> **实现状态**：spec〈实现状态〉标 `run resume` **已实现**，各用例〈预期〉可直接对照验证，无「现状注」。重入点只从 `trace.jsonl` 推断；空 trace → 从 step 0 恢复、混合 trace 历史按每个 `stepIndex` 的末条记录去重。

> **执行策略：真实二进制、隔离 store、真实 run**：所有用例都使用仓库构建出的真实 `bin/conduct`，每条用例在 `mktemp -d` 创建的独立 `HOME` 中执行，run / workflow 记录都必须经 conduct 对外命令或 UI API 产生，禁止手写 `run.json` / `trace.jsonl` / 私有 store 文件摆状态。外部 AI 引擎是 conduct 的外部依赖；为保证用例幂等、无 token 成本、可稳定制造 failed / interrupted / running 边界，本文用 `$WORK/fakebin/claude` 作为外部引擎测试替身。使用替身时须在执行记录中注明“外部引擎由假 claude 代替；原因是真实 AI 引擎有成本、输出不确定，且难以稳定构造本例所需的 failed / interrupted / running 状态”，但被测二进制、run 创建、resume、show/list/stop、UI API 全部走真实 conduct。

> **覆盖清单**：本文必须覆盖并保持：① 重入下标 `R` 只由 trace 推断，含空 trace → `R=0`；② `failed` 与 `interrupted` 都可恢复；③ `completed` 与存活 `running` 被拒；④ resume 跳过已成功步，只重跑中断步及其后；⑤ 原地续写同一 `trace.jsonl`，run id 不变；⑥ 二次 resume 后按 `stepIndex` + 末条 `success` 去重回放与计进度，而非按物理行切片；⑦ `run.json` 不含 `failedStep`，失败步由 trace 末条 `success:false` 记录推断。

> **构造性兜底夹具：一次性失败引擎（toggle_engine）**。失败恢复要能稳定观察「首跑失败 → resume 重跑成功」且不消耗真实引擎额度时，可使用本夹具。`toggle_engine` 用磁盘标记文件实现：需求里带 `MARK_B` 标记的步骤，首次调用创建标记并退 `1`（失败），标记已存在时正常回话（成功）；带 `MARK_A` 的步骤恒成功。两类调用都把自己记进 `$WORK/calls.log`，供断言「resume 跳过了已成功步」（step-a 只被调 1 次、step-b 被调 2 次）。标记文件落在磁盘，`-d` / UI 的 self-exec 子进程也能读到，故后台恢复同样只失败一次。使用该夹具时须在执行记录中注明“构造性兜底，外部引擎由假 claude 代替”，且清理对应 workflow / run。

> **run id 是秒级粒度**：run id ＝ `<workflow>-<YYYYMMDD-HHMMSS>`。同一 workflow 同一秒起两次会撞 id。本文一个用例只起一条 run（resume 续写同一 id、不新建），无撞 id 之虞。

## 行为空间与用例映射（动笔前覆盖规划）

| 分类 | 行为项 | 覆盖用例 / 测试层 |
| --- | --- | --- |
| 正常路径 | failed run 前台 resume：从 trace 末条失败步恢复到 completed | TC-006 |
| 正常路径 | interrupted run resume：空 trace 推断 `R=0`，从 step 0 恢复 | TC-005 |
| 正常路径 | `run resume -d` 后台恢复：父进程打印原 run id 后退 0，子进程续到终态 | TC-007 |
| 正常路径 | `run resume -d --json` 后台恢复：stdout 单行句柄 `{"id","workflow"}`，id 为原 run id | 单测 `internal/cli/run_resume_test.go::TestRunResumeDetachedWithSuccessJSON` |
| 正常路径 | UI `POST /api/runs/{id}/resume` 对 failed run 返回 202 并续跑 | TC-008 |
| 正常路径 | UI `POST /api/runs/{id}/resume` 对 interrupted run 返回 202 并续跑 | TC-013 |
| 边界 / 错误 | 非法 run id：用法错误退 2 | TC-001 |
| 边界 / 错误 | 合法但不存在 run：CLI 退 1，UI 404 | TC-002、TC-009 |
| 边界 / 错误 | resume 不接受新用户需求、不接受 `--cwd` | TC-014 |
| 边界 / 错误 | completed run 被拒绝恢复 | TC-003、TC-009、TC-012 |
| 边界 / 错误 | 存活 running run 被拒绝恢复 | TC-004 |
| 边界 / 错误 | failed 与 interrupted 都被放行，且 interrupted 与 failed 走同一恢复入口 | TC-005、TC-006、TC-012、TC-013 |
| 数据流转 | 跳过已成功步，只重跑中断步及其后；用引擎调用计数证明不是从头跑 | TC-006、TC-010 |
| 数据流转 | 同一 run 原地续写，run id 不变，旧失败 trace 保留作审计 | TC-006、TC-007、TC-008、TC-011 |
| 数据流转 | `run.json` / `run show --json` 不再暴露 `failedStep`，失败步由 trace 末条 `success:false` 推断 | TC-006 |
| 数据流转 | `--json` resume 每补跑一步输出一行事件，且与最终 trace 尾部一致 | TC-011 |
| 特性叠加 | 原 workflow 删除后仍按 run 快照恢复，不回读活 workflow | TC-010 |
| 特性叠加 | 连续两次 resume 后按 `stepIndex` + 末条 `success` 去重回放，非物理行切片 | TC-010 |
| 特性叠加 | UI 前端门控与审计展示：failed / interrupted 有重跑按钮，completed 无，旧失败行显示「已重跑取代」 | TC-012 |
| 交给单测 | `resumeStartIndex` 对越界 `stepIndex`、空 trace、缺口 trace、全成功 trace 的纯算法分支 | `internal/orchestrator/orchestrator_test.go` |
| 交给单测 | evaluator feedback 回放、summary 步骤表去重、`ProgressCount` / `CountProgress` 细粒度算法 | `internal/orchestrator/orchestrator_test.go`、`internal/run/summary_test.go`、`internal/run/record_test.go`、`internal/store/runs_test.go` |

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径。每条用例都先执行 `new_env`，使 conduct store 落在 `$WORK/.conduct`；进入临时目录后仍通过绝对路径 `$CONDUCT` 调用同一个真实二进制：

```bash
make build
CONDUCT="$PWD/bin/conduct"          # 绝对路径，cd / 改 HOME 后仍可用

new_env() {                          # 建隔离环境：临时 HOME（store 落 $WORK/.conduct）+ 假引擎目录置于 PATH 前
  OLD_HOME="${OLD_HOME:-$HOME}"; OLD_PATH="${OLD_PATH:-$PATH}"
  WORK=$(mktemp -d)
  export HOME="$WORK"; export PATH="$WORK/fakebin:$PATH"
  mkdir -p "$WORK/fakebin"
}
del_env() {                          # 复原并清理（先收掉可能遗留的假引擎子进程，再删临时目录）
  pkill -f "$WORK/fakebin/claude" 2>/dev/null
  export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"
  unset OLD_HOME OLD_PATH
}

fast_engine() {                      # 快假引擎：吞 stdin、秒级回定值 JSON → 该步 completed，零 token
  cat > "$WORK/fakebin/claude" <<'SH'
#!/usr/bin/env bash
cat > /dev/null
echo '{"result":"HELLO","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
SH
  chmod +x "$WORK/fakebin/claude"
}
broken_engine() {                    # 坏假引擎：一运行就报错退 1 → 该步 failed
  cat > "$WORK/fakebin/claude" <<'SH'
#!/usr/bin/env bash
cat > /dev/null
echo "claude: 引擎不可用（模拟故障）" >&2
exit 1
SH
  chmod +x "$WORK/fakebin/claude"
}
slow_engine() {                      # 慢假引擎：sleep 后再回话，把该步长时间拖在 running；用法 slow_engine [秒]，默认 30
  # 有意用非引号 <<SH，把秒数烘进脚本；勿改 <<'SH'（那会写出字面 sleep ${1:-30}，运行时 $1 是引擎参数报错）。
  cat > "$WORK/fakebin/claude" <<SH
#!/usr/bin/env bash
cat > /dev/null
sleep ${1:-30}
echo '{"result":"DONE","is_error":false,"usage":{}}'
SH
  chmod +x "$WORK/fakebin/claude"
}
toggle_engine() {                    # 一次性失败引擎：MARK_B 步首跑失败（留标记）、再跑成功；MARK_A 步恒成功；两者都记 calls.log
  # 有意用非引号 <<SH，把 $WORK 路径烘进脚本；$body/$(cat) 转义留字面（运行时才展开）。
  cat > "$WORK/fakebin/claude" <<SH
#!/usr/bin/env bash
body=\$(cat)
if printf '%s' "\$body" | grep -q MARK_A; then echo A >> "$WORK/calls.log"; fi
if printf '%s' "\$body" | grep -q MARK_B; then
  echo B >> "$WORK/calls.log"
  if [ ! -f "$WORK/failonce.flag" ]; then
    touch "$WORK/failonce.flag"
    echo "claude: 第 2 步首跑故意失败（模拟故障）" >&2
    exit 1
  fi
fi
echo '{"result":"OK","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
SH
  chmod +x "$WORK/fakebin/claude"
}
toggle3_engine() {                   # 三步多次失败引擎：MARK_B 首跑失败/重跑成功、MARK_C 首跑失败/重跑成功、MARK_A 恒成功；各自独立标记文件；均记 calls.log
  # 供 TC-010「连续两次 resume + 混合 trace 历史」用：一次 resume 又踩到下一步失败，第二次 resume 才到底。
  cat > "$WORK/fakebin/claude" <<SH
#!/usr/bin/env bash
body=\$(cat)
mark_fail() { echo "\$1" >> "$WORK/calls.log"; if [ ! -f "$WORK/\$2" ]; then touch "$WORK/\$2"; echo "claude: \$1 首跑故意失败（模拟故障）" >&2; exit 1; fi; }
printf '%s' "\$body" | grep -q MARK_A && echo A >> "$WORK/calls.log"
printf '%s' "\$body" | grep -q MARK_B && mark_fail B failB.flag
printf '%s' "\$body" | grep -q MARK_C && mark_fail C failC.flag
echo '{"result":"OK","is_error":false,"usage":{"input_tokens":1,"output_tokens":1}}'
SH
  chmod +x "$WORK/fakebin/claude"
}

make_wf() {                          # 用法：make_wf <名> —— 造并入库单节点最小工作流（fast/broken/slow 引擎用）
  cat > "$WORK/$1.json" <<JSON
{"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"}]}
JSON
  cat "$WORK/$1.json" | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
make_wf2() {                         # 用法：make_wf2 <名> —— 造并入库 2 步工作流：step-a 恒成功、step-b 首跑失败/重跑成功（配 toggle_engine）
  cat > "$WORK/$1.json" <<JSON
{"nodes":[
  {"id":"step-a","displayName":"第一步","engine":"claude-code","promptTemplate":"标记 MARK_A。回复 A。需求：{{sys.userPrompt}}"},
  {"id":"step-b","displayName":"第二步","engine":"claude-code","promptTemplate":"标记 MARK_B。回复 B。需求：{{sys.userPrompt}}"}
]}
JSON
  cat "$WORK/$1.json" | "$CONDUCT" workflow create "$1" --definition >/dev/null
}
make_wf3() {                         # 用法：make_wf3 <名> —— 造并入库 3 步工作流：step-a 恒成功、step-b/step-c 各首跑失败/重跑成功（配 toggle3_engine）
  cat > "$WORK/$1.json" <<JSON
{"nodes":[
  {"id":"step-a","displayName":"第一步","engine":"claude-code","promptTemplate":"标记 MARK_A。回复 A。需求：{{sys.userPrompt}}"},
  {"id":"step-b","displayName":"第二步","engine":"claude-code","promptTemplate":"标记 MARK_B。回复 B。需求：{{sys.userPrompt}}"},
  {"id":"step-c","displayName":"第三步","engine":"claude-code","promptTemplate":"标记 MARK_C。回复 C。需求：{{sys.userPrompt}}"}
]}
JSON
  cat "$WORK/$1.json" | "$CONDUCT" workflow create "$1" --definition >/dev/null
}

rid_of() {                           # 用法：rid_of <workflow> —— 从 run list --json 取该 workflow 的 run id（本文每 workflow 仅一条 run）
  "$CONDUCT" run list --json | python3 -c "import sys,json;print(next(x['id'] for x in json.load(sys.stdin) if x['workflow']=='$1'))"
}
poll_terminal() {                    # 用法：poll_terminal <id> —— 经 run list 轮询到终态，打印最终 status（不碰内部文件）
  local s=""
  for _ in $(seq 1 100); do
    s=$("$CONDUCT" run list --json | python3 -c "import sys,json;print(next((x['status'] for x in json.load(sys.stdin) if x['id']=='$1'),''))")
    case "$s" in completed|failed) break;; esac
    sleep 0.2
  done
  echo "$s"
}
```

> **归一化说明（全篇通用）**：run id 的时间后缀（`-YYYYMMDD-HHMMSS`）、`startedAt`/`endedAt` 时间戳、`pid`、耗时、临时路径均忽略，只校验格式或子串。退出码用 `echo "exit=$?"` 显式打印。stderr 错误信息只校验 spec 规定的关键子串（实现可能带 `Error:` 前缀），不逐字比对整行。

---

## 前置校验（fail-loud，failed / interrupted 可恢复）

### TC-001 resume 非法 id → 退 2

- **目的**：验证非法 run id（`run.ValidateID` 不过）在发射前被拦，退**用法错误** `2`。
- **前置**：`new_env`（无需引擎 / 工作流，发射前即拦）。
- **步骤**：
  1. `"$CONDUCT" run resume "bad/../id"; echo "exit=$?"`
- **预期**：
  - 退出码 `2`（用法错误，非 `1`）；stderr 提示 id 非法。
- **清理**：`del_env`。

### TC-002 resume 不存在的 id → 退 1

- **目的**：验证 id 合法但 store 中无此 run 时，退 `1`（运行不存在）。
- **前置**：`new_env`（store 为空）。
- **步骤**：
  1. `"$CONDUCT" run resume ghost-20260101-000000; echo "exit=$?"`
- **预期**：
  - 退出码 `1`；stderr 含 `运行不存在`（或等价「不存在」提示）。
- **清理**：`del_env`。

### TC-014 resume 不接受新用户需求、不接受 --cwd → 退 2

- **目的**：验证 `resume` 沿用原 run 的 `userPrompt` / `cwd`，不接受新的用户需求位置参数，也不接受 `--cwd`；两类输入都在发射前按用法错误拒绝。
- **前置**：`new_env`（无需引擎 / 工作流，参数解析阶段即拦）。
- **步骤**：
  1. 传多余位置参数：
     `"$CONDUCT" run resume ghost-20260101-000000 "新需求"; echo "extra-exit=$?"`
  2. 传 `--cwd`：
     `"$CONDUCT" run resume ghost-20260101-000000 --cwd "$WORK"; echo "cwd-exit=$?"`
- **预期**：
  - 步骤 1 退出码 `2`；stderr 含用法错误提示（多余参数 / `accepts 1 arg` 等等价文案），且不会进入 store 查询。
  - 步骤 2 退出码 `2`；stderr 含 `unknown flag: --cwd`（或等价未知 flag 提示），且不会进入 store 查询。
- **清理**：`del_env`。

### TC-003 resume 已 completed 的 run → 退 1「已成功完成，无需恢复」

- **目的**：验证对**成功终态**的 run 恢复被 fail-loud 拒绝（无可恢复点）。
- **前置**：
  1. `new_env; fast_engine; make_wf ok`。
  2. 跑到 completed：`"$CONDUCT" workflow run ok "跑通" --cwd "$WORK" >/dev/null; RID=$(rid_of ok)`。
- **步骤**：
  1. `"$CONDUCT" run resume "$RID"; echo "exit=$?"`
- **预期**：
  - 退出码 `1`；stderr 含 `已成功完成，无需恢复`。
  - 该 run 仍为 completed（未被改动）：`"$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])'` → `completed`。
- **清理**：`del_env`。

### TC-004 resume 运行中（running）的 run → 退 1「仍在运行中，无法恢复」

- **目的**：验证对**进程存活的 running** run 恢复被拒绝（不并发续跑一个还在写盘的 run）。
- **前置**：
  1. `new_env; slow_engine; make_wf busy`（慢引擎把该步拖在 running）。
  2. 后台起跑并等它落盘进 running：
     ```bash
     "$CONDUCT" workflow run busy "慢慢跑" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$(rid_of busy)
     "$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json;print("mid=",json.load(sys.stdin)["status"])'
     ```
- **步骤**：
  1. `"$CONDUCT" run resume "$RID"; echo "exit=$?"`
- **预期**：
  - 前置打印 `mid= running`（run 确在跑）。
  - 步骤 1 退出码 `1`；stderr 含 `仍在运行中，无法恢复`。
- **清理**：`del_env`（`pkill` 兜底收掉假 sleep 子进程）。

### TC-005 resume 已中断（interrupted）的 run → 空 trace 按 R=0 恢复到 completed

- **目的**：验证对**读时派生为 interrupted**（running 但 pid 已死）的 run 可恢复；该 run 中断前没有完整 trace 行时，`resume` 按空 trace 规则推断 `R=0`，从 step 0 重跑。
- **前置**：
  1. `new_env; slow_engine; make_wf gone`。
  2. 后台起跑 → 确认 running → `run stop` 使其停写、派生 interrupted：
     ```bash
     "$CONDUCT" workflow run gone "会被中断" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$(rid_of gone)
     "$CONDUCT" run stop "$RID" >/dev/null
     for _ in $(seq 1 30); do
       S=$("$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
       [ "$S" = interrupted ] && break; sleep 0.3
     done
     echo "derived=$S"
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;print("trace_before=",len(json.load(sys.stdin)["trace"]))'
     ```
- **步骤**：
  1. 把假引擎换成快成功版本，再恢复该 interrupted run：
     ```bash
     fast_engine
     "$CONDUCT" run resume "$RID"; echo "exit=$?"
     "$CONDUCT" run show "$RID" --json --trace | python3 -c '
     import sys,json
     d=json.load(sys.stdin)
     print("status=", d["status"])
     print("trace_shape=", [(e["stepIndex"], e["success"]) for e in d["trace"]])
     '
     ```
- **预期**：
  - 前置打印 `derived=interrupted`（被 stop 后进程死、按 pid 判活派生为 interrupted）与 `trace_before= 0`——中断前没有完整 trace 行，说明重入下标只能由空 trace 推断为 `R=0`。
  - 步骤 1 退出码 `0`；最终打印 `status= completed`，`trace_shape= [(0, True)]`——中断前没有完整 trace 行，恢复从 step 0 重跑成功。
- **清理**：`del_env`。

---

## happy path（跳过已成功步 + 原地续写 + trace 审计保留）

### TC-006 前台 resume：失败步续跑到 completed，run id 不变、跳过已成功步、trace 审计保留、进度去重

- **目的**：一条用例把 spec 的 happy path 核心不变量一次验全：① 首跑第 2 步失败 → run `failed`，失败步从 trace 末条失败记录推断为 `stepIndex=1`；② `resume` 从中断处续跑到 `completed`、**run id 不变**、续写同一 `runs/<id>/`；③ **跳过已成功的第 1 步**（step-a 引擎只被调 1 次、step-b 被调 2 次——数据流转断言，非只看退出码）；④ 失败步旧 trace **保留**、补跑行续写在后（`stepIndex=1` 出现两条：一失败一成功，审计轨迹）；⑤ 进度按唯一 `stepIndex` 且 `success` 去重为 `2/2`（不因保留失败行而 `k>N`）；⑥ 恢复后 `endedAt` 重新落值。
- **前置**：
  1. `new_env; toggle_engine; make_wf2 two`。
- **步骤**：
  1. 首跑：第 2 步故意失败：
     ```bash
     "$CONDUCT" workflow run two "干活-XYZ" --cwd "$WORK"; echo "run-exit=$?"
     RID=$(rid_of two)
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;d=json.load(sys.stdin);tr=d["trace"];print("status1=",d["status"],"last_failed=",next(e["stepIndex"] for e in reversed(tr) if not e["success"]))'
     ```
  2. 恢复：从中断处续跑到底：
     ```bash
     "$CONDUCT" run resume "$RID"; echo "resume-exit=$?"
     ```
  3. 校验终态、run id 不变、进度与 trace 审计：
     ```bash
     "$CONDUCT" run show "$RID" --json --trace | python3 -c '
     import sys, json
     d = json.load(sys.stdin)
     tr = d["trace"]
     uniq_ok = {}
     for e in tr: uniq_ok[e["stepIndex"]] = e["success"]  # 同一步以最后一次为准
     k = sum(1 for v in uniq_ok.values() if v)
     print("status2=", d["status"])
     print("endedAt_set=", d["endedAt"] is not None)
     print("has_failedStep=", "failedStep" in d)
     print("trace_lines=", len(tr))
     print("step1_records=", sum(1 for e in tr if e["stepIndex"]==1))
     print("progress=", str(k) + "/" + str(d["steps"]))
     '
     echo "id_unchanged=$([ "$(rid_of two)" = "$RID" ] && echo yes || echo no)"
     echo "calls: A=$(grep -c '^A$' "$WORK/calls.log") B=$(grep -c '^B$' "$WORK/calls.log")"
     ```
- **预期**：
  - 步骤 1：`run-exit=1`（第 2 步失败，整趟退 1）；`status1= failed last_failed= 1`。
  - 步骤 2：`resume-exit=0`（续跑成功到底，退 0）。
  - 步骤 3：
    - `status2= completed`、`endedAt_set= True`（终态重新落值）。
    - `has_failedStep= False`——对外 JSON 不再暴露 `failedStep`，失败步由 trace 末条 `success:false` 记录推断。
    - `trace_lines= 3`、`step1_records= 2`——失败步旧 trace 保留 + 补跑行续写，`stepIndex=1` 两条（一失败一成功），是有意保留的审计轨迹。
    - `progress= 2/2`——本用例的 `k` 由脚本按唯一 `stepIndex` 且 `success` 从 trace **交叉核算**，用以印证「数物理行会得 `3/2`、去重后才 `2/2`」这一 trace 形态；conduct **对外**的进度实现（`run.ProgressCount` / `store.CountProgress`）另由 [TC-008](#tc-008-ui-重跑-failed-run--202-返回同一-runid续跑到-completedtrace-保留审计双行) 直接断言 `/api/runs/{id}?trace=1` 的 `progress` 字段（`1`→`2`）、并由单测 `internal/run/record_test.go`、`internal/store/runs_test.go` 覆盖，不以本用例的脚本重算冒充实现覆盖。
    - `id_unchanged=yes`——续写同一 run、id 不变（未新建 run）。
    - `calls: A=1 B=2`——**核心数据流转断言**：第 1 步（step-a）引擎只在首跑被调 1 次、resume 时被跳过；第 2 步（step-b）首跑失败 + 补跑成功共 2 次。证明 resume 真「跳过前面已成功步、只重跑失败步及其后」，非从头再来。
- **清理**：`del_env`。

### TC-010 恢复源＝落盘快照 + trace 回放：删原 workflow 后仍可续、连续两次 resume 依前次补跑产物按 stepIndex 续跑

- **目的**：一次验全 spec〈run resume〉的**恢复源关键语义**（TC-006 的干净单失败前缀盖不到的两点）：① 恢复只认 `run.json` 里的 `workflowSnapshot`——**首跑后把原 workflow 从 store 删除**，resume 仍能按快照还原步序续跑到底（不回读已删的活 workflow）；② trace 回放按每个 `stepIndex` 的末条记录去重，且仅回放 `stepIndex < R` 且末条 `success` 的记录，**非物理行切片**——用 3 步工作流（step-b、step-c 各首跑失败一次）制造「旧失败行 + 上次补跑成功行 + 再失败行」的**混合 trace 历史**，**连续两次 resume**：第一次补跑 step-b 成功但 step-c 又失败，第二次 resume 必须依据前次 step-b 的**补跑成功**产物（而非那条旧的 step-b 失败行）跳过 a、b 只重跑 c。数据流转断言 `A=1 B=2 C=2` 与 trace 五行形态共同证明去重回放正确。
- **前置**：
  1. `new_env; toggle3_engine; make_wf3 three`。
- **步骤**：
  1. 首跑：step-b 故意失败，拿 run id，删除原 workflow：
     ```bash
     "$CONDUCT" workflow run three "多步-XYZ" --cwd "$WORK" >/dev/null 2>&1; echo "run1-exit=$?"
     RID=$(rid_of three)
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;d=json.load(sys.stdin);tr=d["trace"];print("status1=",d["status"],"last_failed1=",next(e["stepIndex"] for e in reversed(tr) if not e["success"]))'
     "$CONDUCT" workflow delete three --yes >/dev/null; echo "wf_count=$("$CONDUCT" workflow list --json | python3 -c 'import sys,json;print(len(json.load(sys.stdin)))')"
     ```
  2. 第一次 resume：step-b 补跑成功、step-c 又失败（仍非终态）：
     ```bash
     "$CONDUCT" run resume "$RID" >/dev/null 2>&1; echo "resume1-exit=$?"
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;d=json.load(sys.stdin);tr=d["trace"];print("status2=",d["status"],"last_failed2=",next(e["stepIndex"] for e in reversed(tr) if not e["success"]))'
     ```
  3. 第二次 resume：依前次 step-b 补跑产物续跑，step-c 成功到底，校验 trace 形态与调用次数：
     ```bash
     "$CONDUCT" run resume "$RID" >/dev/null 2>&1; echo "resume2-exit=$?"
     "$CONDUCT" run show "$RID" --json --trace | python3 -c '
     import sys, json
     d = json.load(sys.stdin); tr = d["trace"]
     print("status3=", d["status"])
     print("trace_lines=", len(tr))
     print("shape=", [(e["stepIndex"], e["success"]) for e in tr])
     '
     echo "calls: A=$(grep -c '^A$' "$WORK/calls.log") B=$(grep -c '^B$' "$WORK/calls.log") C=$(grep -c '^C$' "$WORK/calls.log")"
     ```
- **预期**：
  - 步骤 1：`run1-exit=1`；`status1= failed last_failed1= 1`；`wf_count=0`——原 workflow 已从 store 删除。
  - 步骤 2：`resume1-exit=1`（虽删了 workflow 仍按快照续跑，只是这次踩到 step-c 失败）；`status2= failed last_failed2= 2`——失败点前移到第 3 步。**若实现回读活 workflow 而非快照，此处应因「工作流不存在」而报错、拿不到 `last_failed2= 2`**。
  - 步骤 3：
    - `resume2-exit=0`；`status3= completed`。
    - `trace_lines= 5`、`shape= [(0, True), (1, False), (1, True), (2, False), (2, True)]`——step-b、step-c 各留「一失败一成功」双行，是有意保留的混合审计历史。
    - `calls: A=1 B=2 C=2`——**核心断言**：step-a 全程只调 1 次（两次 resume 都跳过）；step-b 共 2 次（首跑失败 + 第一次 resume 补跑成功），**第二次 resume 未再调 step-b**（证明回放取的是 stepIndex=1 的**补跑成功**行、非那条旧失败行，且非物理切片重跑）；step-c 共 2 次（第一次 resume 失败 + 第二次 resume 成功）。
- **清理**：`del_env`。

---

## 前台逐步事件流（--json）

### TC-011 前台 resume --json：每步一行 JSON 事件，行数＝补跑步数，且与续写 trace 对齐

- **目的**：验证 `conduct run resume <id> --json` 的前台逐步事件流（spec〈run resume〉暴露的 `--json` 选项）——stdout **每补跑一步吐一行**机器可读 JSON（即 `trace.jsonl` 的一条记录），行数恰等于「从中断处到终态」的补跑步数，且事件内容与最终续写进 trace 的记录对齐（`stepIndex` / `success`）。避免 `--json` 退化成人读装饰或漏吐事件行而无从察觉。
- **前置**：
  1. `new_env; toggle_engine; make_wf2 j2`（2 步工作流，step-b 首跑失败：失败步 `stepIndex=1`，补跑只剩 1 步）。
  2. 首跑失败，拿到 failed run：
     ```bash
     "$CONDUCT" workflow run j2 "json-XYZ" --cwd "$WORK" >/dev/null 2>&1; RID=$(rid_of j2)
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;d=json.load(sys.stdin);print("last_failed=",next(e["stepIndex"] for e in reversed(d["trace"]) if not e["success"]))'
     ```
- **步骤**：
  1. 前台 `--json` 恢复，把 stdout 落文件后逐行校验为合法 JSON、统计事件行：
     ```bash
     "$CONDUCT" run resume "$RID" --json > "$WORK/events.ndjson"; echo "resume-exit=$?"
     python3 -c '
     import json
     lines = [l for l in open("'"$WORK"'/events.ndjson") if l.strip()]
     objs = [json.loads(l) for l in lines]          # 每行必须是合法 JSON，否则抛异常即 FAIL
     print("event_lines=", len(objs))
     print("event_steps=", [o["stepIndex"] for o in objs])
     print("event_success=", [o["success"] for o in objs])
     '
     ```
  2. 校验事件与最终续写进 trace 的记录对齐（补跑那条 `stepIndex=1` 成功记录同时出现在事件流与 trace 尾）：
     ```bash
     "$CONDUCT" run show "$RID" --json --trace | python3 -c '
     import sys, json
     d = json.load(sys.stdin); tr = d["trace"]
     print("final=", d["status"])
     print("trace_step1_records=", sum(1 for e in tr if e["stepIndex"]==1))
     print("trace_tail=", (tr[-1]["stepIndex"], tr[-1]["success"]))
     '
     ```
- **预期**：
  - 前置打印 `last_failed= 1`。
  - 步骤 1：`resume-exit=0`；`event_lines= 1`（补跑仅剩第 2 步一步）；`event_steps= [1]`；`event_success= [True]`——每补跑一步吐一行合法 JSON，行数＝补跑步数（1），非人读装饰。
  - 步骤 2：`final= completed`；`trace_step1_records= 2`（旧失败行 + 补跑成功行）；`trace_tail= (1, True)`——事件流吐出的 `stepIndex=1` 成功记录，正是续写进 trace 尾的那条（事件与落盘对齐）。
- **清理**：`del_env`。

---

## 后台恢复（-d / --detach）

### TC-007 resume -d：预检同步做完后打印 run id 退 0，后台续跑到 completed

- **目的**：验证 `-d` 后台恢复——预检（fail-loud）同步做完后 spawn 独立会话子进程续跑，父进程打印 run id 提示后退 `0`；run id 即入参（无需轮询匹配），后台子进程独立把该 run 续到 `completed`。
- **前置**：
  1. `new_env; toggle_engine; make_wf2 bg2`。
  2. 首跑失败，拿到 failed run：
     ```bash
     "$CONDUCT" workflow run bg2 "后台恢复-XYZ" --cwd "$WORK" >/dev/null 2>&1; RID=$(rid_of bg2)
     "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json;d=json.load(sys.stdin);print("last_failed=",next(e["stepIndex"] for e in reversed(d["trace"]) if not e["success"]))'
     ```
- **步骤**：
  1. `"$CONDUCT" run resume "$RID" -d; echo "exit=$?"`
  2. 等后台子进程收尾：`echo "final=$(poll_terminal "$RID")"`
- **预期**：
  - 前置打印 `last_failed= 1`（trace 可推断失败步、可恢复）。
  - 步骤 1 退出码 `0`；stdout 一行提示，含 `已在后台恢复`、`conduct run show`、`conduct run stop`，且其中的 run id 与 `$RID` 一致（时间后缀忽略）。
  - 步骤 2 打印 `final=completed`——后台子进程脱离父进程后独立把 run 续到了终态。
- **清理**：`del_env`。

---

## UI 重跑端点（POST /api/runs/{id}/resume）

> 复用 [ui-server.md](./ui-server.md) 的 `start_ui`（`--port 0` 随机端口、就绪后置 `$B`）与 `stop_ui`。**关键顺序**：必须先装假引擎（改 PATH）**再** `start_ui`——服务端 self-exec 出的子进程继承服务端启动时的环境，假引擎才生效。
>
> **分层**：TC-008/009 打的是**服务端 API**（重跑端点的状态码与续写语义），是 `run-detail.js` 重跑按钮点击后真正发起的调用；`run-detail.js` 的**前端渲染**行为（failed / interrupted 页出重跑按钮、completed 页无、重跑后逐步列表旧失败行出现「已重跑取代」标签）是可自动化的 **DOM 结构**断言，另立 [TC-012](#tc-012-ui-前端浏览器自动化failed--interrupted-出重跑按钮点击后续跑逐步列表出现已重跑取代) 用浏览器自动化覆盖。只有「取代行的具体样式配色」这类**像素级视觉**才留浏览器人工走查（同 ui-server.md 对 SPA 的处理）。

`start_ui` / `stop_ui` 定义（粘贴到各用例前置）：

```bash
start_ui() {   # 设置全局 UIPID 与 B（形如 http://127.0.0.1:<port>）
  "$CONDUCT" ui --port 0 > "$WORK/ui.log" 2>&1 &
  UIPID=$!
  local i
  for i in $(seq 1 50); do
    B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1)
    if [ -n "$B" ] && curl -s -o /dev/null "$B/api/version"; then return 0; fi
    sleep 0.1
  done
  echo "ui 未在预期时间内就绪"; cat "$WORK/ui.log"; return 1
}
stop_ui() { kill "$UIPID" 2>/dev/null; wait "$UIPID" 2>/dev/null; }
```

### TC-008 UI 重跑 failed run → 202 返回同一 runId，续跑到 completed，trace 保留审计双行

- **目的**：验证运行详情「重跑」的服务端等价物 `POST /api/runs/{id}/resume`：对 `failed` run 返回 `202` + `{runId}`（**即原 id**，同一 run 续写）；self-exec 子进程从 trace 推断出的失败步续到 `completed`；trace 保留失败步旧行 + 补跑行（`stepIndex=1` 两条，审计基础）。
- **前置**：隔离 HOME + toggle 引擎 + 服务端 + 一个 2 步工作流，先经 API 跑出一条 failed run：
  ```bash
  new_env                                   # 临时 HOME + fakebin 于 PATH 前
  # 粘贴 toggle_engine / make_wf2 / rid_of / poll_terminal / start_ui / stop_ui 定义
  toggle_engine                             # 装一次性失败假 claude（先于 start_ui）
  start_ui                                  # 服务端带着假引擎 PATH 起，子进程亦然
  make_wf2 uiwf                             # 经 CLI 入库 2 步工作流 uiwf（store 与 UI 同源）
  # 经 API 发射首跑（引擎令第 2 步失败）→ 取 run id
  RID=$(curl -s -X POST "$B/api/workflows/uiwf/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"UI-XYZ","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  echo "first=$(poll_terminal "$RID")"      # 期望 failed
  ```
- **步骤**：
  1. 对该 failed run 调重跑端点，取 HTTP 码与回传 runId：
     ```bash
     RESP=$(curl -s -w '\n%{http_code}' -X POST "$B/api/runs/$RID/resume" -H 'Content-Type: application/json' -d '{}')
     CODE=$(printf '%s' "$RESP" | tail -1)
     BACK=$(printf '%s' "$RESP" | sed '$d' | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
     echo "http=$CODE back=$BACK same=$([ "$BACK" = "$RID" ] && echo yes || echo no)"
     ```
  2. 等后台续跑收尾，校验终态与 trace 审计：
     ```bash
     echo "final=$(poll_terminal "$RID")"
     curl -s "$B/api/runs/$RID?trace=1" | python3 -c '
     import sys, json
     d = json.load(sys.stdin)
     tr = d["trace"]
     print("status=", d["status"])
     print("progress=", d["progress"], "steps=", d["steps"])   # conduct 对外进度：store.CountProgress 去重后的 k / N
     print("step1_records=", sum(1 for e in tr if e["stepIndex"]==1), "trace_lines=", len(tr))
     '
     ```
- **预期**：
  - 前置打印 `first=failed`（首跑第 2 步失败）。
  - 步骤 1 打印 `http=202 back=<RID> same=yes`——重跑返回 `202`，回传的 `runId` 即原 id（同一 run 续写，不新建）。
  - 步骤 2：`final=completed`；`status= completed`；`progress= 2 steps= 2`——**这是 conduct 自身进度实现的断言**：`/api/runs/{id}?trace=1` 的 `progress` 字段由 `store.CountProgress` 按唯一 `stepIndex` 且 `success` 去重得出（首跑失败时为 `1`、resume 后为 `2`），恒 `≤ steps`，数物理行会得 `3` 而实现去重为 `2`；`step1_records= 2 trace_lines= 3`——失败步旧 trace 保留 + 补跑成功行续写，`stepIndex=1` 两条（前端据此把被取代的失败行标「已重跑取代」）。
- **清理**：`stop_ui; del_env`。

### TC-009 UI 重跑 completed / 不存在的 run → 409 / 404

- **目的**：验证重跑端点的 fail-loud 边界：对**completed** run 返回 `409`（无需恢复）；对**不存在**的 run 返回 `404`。错误码映射与 CLI `run resume` 语义（completed→退 1）一致地收敛为 HTTP 409/404。
- **前置**：隔离 HOME + 快引擎 + 服务端 + 一条已 completed 的 run：
  ```bash
  new_env
  # 粘贴 fast_engine / make_wf / rid_of / poll_terminal / start_ui / stop_ui 定义
  fast_engine
  start_ui
  make_wf done                              # 经 CLI 入库单节点工作流
  RID=$(curl -s -X POST "$B/api/workflows/done/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"跑通","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  echo "state=$(poll_terminal "$RID")"      # 期望 completed
  ```
- **步骤**：
  1. 重跑已 completed 的 run：
     `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/runs/$RID/resume" -H 'Content-Type: application/json' -d '{}'`
  2. 重跑不存在的 run：
     `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/runs/ghost-20260101-000000/resume" -H 'Content-Type: application/json' -d '{}'`
- **预期**：
  - 前置打印 `state=completed`。
  - 步骤 1 打印 `409`——completed 不可恢复。
  - 步骤 2 打印 `404`——run 不存在。
- **清理**：`stop_ui; del_env`。

### TC-013 UI 重跑 interrupted run → 202 返回同一 runId，空 trace 从 step 0 续跑

- **目的**：验证 `POST /api/runs/{id}/resume` 对 **interrupted** run 的成功分支：返回 `202` + `{runId}`（原 id），由 self-exec 子进程按空 trace 推断 `R=0`，续写同一 run 到 `completed`。TC-005 覆盖 CLI interrupted；本用例专门覆盖 UI API interrupted → 202。
- **前置**：隔离 HOME + 慢引擎 + 服务端，经 API 发射后停止，造出空 trace 的 interrupted run：
  ```bash
  new_env
  # 粘贴 slow_engine / fast_engine / make_wf / poll_terminal / start_ui / stop_ui 定义
  slow_engine
  start_ui
  make_wf uiint
  RID=$(curl -s -X POST "$B/api/workflows/uiint/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"UI-INT","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  for _ in $(seq 1 30); do
    S=$(curl -s "$B/api/runs/$RID" | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
    [ "$S" = running ] && break; sleep 0.2
  done
  curl -s -o /dev/null -X POST "$B/api/runs/$RID/stop" -H 'Content-Type: application/json' -d '{}'
  for _ in $(seq 1 30); do
    S=$(curl -s "$B/api/runs/$RID" | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
    [ "$S" = interrupted ] && break; sleep 0.3
  done
  echo "derived=$S"
  curl -s "$B/api/runs/$RID?trace=1" | python3 -c 'import sys,json;print("trace_before=",len(json.load(sys.stdin)["trace"]))'
  ```
- **步骤**：
  1. 换成快引擎，对该 interrupted run 调重跑端点：
     ```bash
     fast_engine
     RESP=$(curl -s -w '\n%{http_code}' -X POST "$B/api/runs/$RID/resume" -H 'Content-Type: application/json' -d '{}')
     CODE=$(printf '%s' "$RESP" | tail -1)
     BACK=$(printf '%s' "$RESP" | sed '$d' | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
     echo "http=$CODE back=$BACK same=$([ "$BACK" = "$RID" ] && echo yes || echo no)"
     ```
  2. 等续跑收尾，校验终态与 trace：
     ```bash
     echo "final=$(poll_terminal "$RID")"
     curl -s "$B/api/runs/$RID?trace=1" | python3 -c '
     import sys, json
     d = json.load(sys.stdin)
     print("status=", d["status"])
     print("trace_shape=", [(e["stepIndex"], e["success"]) for e in d["trace"]])
     '
     ```
- **预期**：
  - 前置打印 `derived=interrupted` 与 `trace_before= 0`。
  - 步骤 1 打印 `http=202 back=<RID> same=yes`——UI API 对 interrupted 放行，且返回原 run id。
  - 步骤 2 打印 `final=completed`、`status= completed`、`trace_shape= [(0, True)]`——空 trace 按 `R=0` 从 step 0 续跑成功，续写同一 run。
- **清理**：`stop_ui; del_env`。

### TC-012 UI 前端（浏览器自动化）：failed / interrupted 出重跑按钮、点击后续跑、逐步列表出现「已重跑取代」

- **目的**：验证 `run-detail.js` 的**前端渲染与交互**（TC-008/009 只覆盖服务端 API，覆不到前端）：① 运行详情页 **failed / interrupted** 状态显示「重跑」按钮，completed 页**无**该按钮；② 在 failed 页点「重跑」→ 确认对话框 →发起 resume，run 续跑到 completed；③ 续跑后逐步列表里被取代的旧失败行出现「已重跑取代」标签（`.superseded-tag`）。这些是可自动化的 **DOM 结构**断言，用浏览器自动化工具（如 playwright MCP）驱动，非人工目视。
- **前置**：隔离 HOME + 假引擎 + 服务端，经 API 造出三条**已知终态**的 run（都走对外接口、不伪造内部状态），并安装临时 Playwright 浏览器依赖到 `$WORK`：
  ```bash
  new_env
  # 粘贴 toggle_engine / fast_engine / slow_engine / make_wf / make_wf2 / poll_terminal / start_ui / stop_ui 定义
  toggle_engine
  start_ui

  make_wf2 uifail
  RID_FAIL=$(curl -s -X POST "$B/api/workflows/uifail/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"UI-FAIL","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  echo "fail_state=$(poll_terminal "$RID_FAIL")"

  fast_engine
  make_wf uidone
  RID_DONE=$(curl -s -X POST "$B/api/workflows/uidone/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"UI-DONE","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  echo "done_state=$(poll_terminal "$RID_DONE")"

  slow_engine
  make_wf uibusy
  RID_INT=$(curl -s -X POST "$B/api/workflows/uibusy/runs" \
    -H 'Content-Type: application/json' -d '{"userPrompt":"UI-INT","cwd":"'"$WORK"'"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["runId"])')
  for _ in $(seq 1 30); do
    S=$(curl -s "$B/api/runs/$RID_INT" | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
    [ "$S" = running ] && break; sleep 0.2
  done
  curl -s -o /dev/null -X POST "$B/api/runs/$RID_INT/stop" -H 'Content-Type: application/json' -d '{}'
  for _ in $(seq 1 30); do
    S=$(curl -s "$B/api/runs/$RID_INT" | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
    [ "$S" = interrupted ] && break; sleep 0.3
  done
  echo "int_state=$S"

  fast_engine
  export B RID_FAIL RID_DONE RID_INT
  export PLAYWRIGHT_BROWSERS_PATH="$WORK/pw-browsers"
  npx -y @playwright/test@1.45.3 install chromium
  ```
- **步骤**：
  1. 生成可判定的 Playwright spec：
     ```bash
     cat > "$WORK/run-detail-resume.spec.js" <<'JS'
     const { test, expect } = require('@playwright/test');

     const B = process.env.B;
     const RID_FAIL = process.env.RID_FAIL;
     const RID_DONE = process.env.RID_DONE;
     const RID_INT = process.env.RID_INT;

     async function runStatus(request, id) {
       const r = await request.get(`${B}/api/runs/${id}`);
       expect(r.ok()).toBeTruthy();
       return (await r.json()).status;
     }

     test('run detail resume button and superseded row', async ({ page, request }) => {
       await page.goto(`${B}/#/runs/${RID_DONE}`);
       await expect(page.getByRole('button', { name: '重跑' })).toHaveCount(0);

       await page.goto(`${B}/#/runs/${RID_INT}`);
       await expect(page.getByRole('button', { name: '重跑' })).toHaveCount(1);

       await page.goto(`${B}/#/runs/${RID_FAIL}`);
       await expect(page.getByRole('button', { name: '重跑' })).toHaveCount(1);
       await page.getByRole('button', { name: '重跑' }).click();
       await expect(page.locator('.modal')).toContainText('恢复运行');
       await page.locator('.modal').getByRole('button', { name: '重跑' }).click();

       await expect.poll(() => runStatus(request, RID_FAIL), { timeout: 10000 }).toBe('completed');
       await page.goto(`${B}/#/runs/${RID_FAIL}`);
       await expect(page.locator('.superseded-tag')).toContainText('已重跑取代');
       await expect(page.locator('.step.superseded')).toHaveCount(1);
     });
     JS
     ```
  2. 执行浏览器自动化断言：
     ```bash
     npx -y @playwright/test@1.45.3 test "$WORK/run-detail-resume.spec.js" --reporter=line; echo "pw-exit=$?"
     ```
- **预期**：
  - 前置打印 `fail_state=failed`、`done_state=completed`、`int_state=interrupted`。
  - 步骤 2 退出码 `0`，打印 `pw-exit=0`。
  - Playwright 断言 completed 详情页无「重跑」按钮，interrupted / failed 详情页各有一个「重跑」按钮；点击 failed 页「重跑」并确认后，该 run 到达 `completed`；刷新后存在 `.superseded-tag` 文本「已重跑取代」，且存在一行 `.step.superseded`。
- **清理**：`stop_ui; del_env`。
