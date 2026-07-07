# 后台运行（detach）及其配套命令 测试用例

覆盖 `workflow run` 的**后台起跑**旗标 `-d`/`--detach`，以及随它闭合 docker 式 run 生命周期的三条配套命令：`run wait`（阻塞到终态）、`run rm`（删除记录）、`run list --status`（按状态过滤）。前台 `workflow run` 与 `run list`/`run show`/`run stop` 的既有行为见 [workflow-running.md](./workflow-running.md)，本文不重复。对应 spec：[docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈后台运行（`-d` / `--detach`）〉〈run wait〉〈run rm〉〈run list〉；需求背景见 [docs/proposals/detached-run.md](../proposals/detached-run.md)。

> **实现状态**：本文四项功能（`-d` / `run wait` / `run rm` / `run list --status`）spec〈实现状态〉均标 **已实现**，各用例〈预期〉可直接对照验证，无「现状注」。
>
> **手工层不覆盖、交给单测的路径（诚实标注）**：以下 spec 行为难以在手工层**确定性**触发（子进程即 conduct 自身、亚秒级落盘），故不写手工用例、由单测兜底，不在本文「可直接对照验证」的承诺范围内：① `-d` 有界等待超时未确认 run id → 退 `1`、以及 fork/setsid 发射失败 → 退 `1`（spec〈后台运行〉退出码表；`internal/launch` 的 `matchRunID` 纯函数已单测，`runDetached` 的 `runID==""→退1` 分支建议补 `internal/cli` 单测）；② `-d --json` 句柄在 run 已**秒级终态**时仍正确交回 id（`matchRunID` 不要求停留 running，已单测）。

> **零成本、全隔离（关键，与 workflow-running.md 的 💸 用例不同）**：`-d` 的父进程 self-exec 出的**子进程会继承父进程的环境**（`HOME` / `PATH`），故后台 run 一样能用「临时 HOME + PATH 前置的假引擎」隔离——**本文全部用例都在临时 HOME 里跑、用假引擎顶替真 `claude`，零 token、无残留**，无需真实登录态。假引擎是 conduct 的外部依赖替身（同 workflow-running.md 的 TC-007 / TC-010），run 记录仍全部由 conduct 自己产出，非伪造内部数据。

> **run id 是秒级粒度（造多条 run 时务必注意）**：run id ＝ `<workflow>-<YYYYMMDD-HHMMSS>`（见 spec〈数据模型〉）。**同一 workflow 在同一秒内起两次会撞 id**（后一次报「运行已存在」失败）。故凡一个用例需要多条 run，一律用**不同的 workflow 名**（下文 TC-020 即用 `ok` + `bad` 两名），不靠同名连跑。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径，再定义各用例复用的隔离环境与假引擎助手（同一 shell 会话内持续有效）：

```bash
make build
CONDUCT="$PWD/bin/conduct"          # 绝对路径，cd / 改 HOME 后仍可用

new_env() {                          # 建隔离环境：临时 HOME（store 落 $WORK/.conduct）+ 假引擎目录置于 PATH 前
  # OLD_HOME/OLD_PATH 用 := 幂等保护：某用例中途失败没跑 del_env 就又开一个用例时，
  # 不把已被 rm 的上一个 $WORK 误记成「原始 HOME」（否则 del_env 会把 HOME 设到已删目录）。
  OLD_HOME="${OLD_HOME:-$HOME}"; OLD_PATH="${OLD_PATH:-$PATH}"
  WORK=$(mktemp -d)
  export HOME="$WORK"; export PATH="$WORK/fakebin:$PATH"
  mkdir -p "$WORK/fakebin"
}
del_env() {                          # 复原并清理（先收掉可能遗留的假引擎子进程，再删临时目录）
  pkill -f "$WORK/fakebin/claude" 2>/dev/null
  export HOME="$OLD_HOME"; export PATH="$OLD_PATH"; rm -rf "$WORK"
  unset OLD_HOME OLD_PATH            # 复位，令下一次 new_env 重新以当前真实 HOME 为基准
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
echo "claude: 引擎不可用（模拟故障）" >&2
exit 1
SH
  chmod +x "$WORK/fakebin/claude"
}
slow_engine() {                      # 慢假引擎：sleep 后再回话，把该步长时间拖在 running；用法 slow_engine [秒]，默认 30
  # 注意：此处 heredoc 用非引号 <<SH（与上面 fast/broken 的 <<'SH' 有意相反），
  # 为的是在函数定义期把秒数 ${1:-30} 烘进脚本。切勿为「统一风格」改成 <<'SH'——
  # 那会写出字面 sleep ${1:-30}，运行时假 claude 的 $1 是引擎参数（如 -p）→ sleep -p 报错。
  cat > "$WORK/fakebin/claude" <<SH
#!/usr/bin/env bash
sleep ${1:-30}
echo '{"result":"DONE","is_error":false,"usage":{}}'
SH
  chmod +x "$WORK/fakebin/claude"
}

make_wf() {                          # 用法：make_wf <名> —— 造并入库一个单节点最小工作流（只回一个词）
  cat > "$WORK/$1.json" <<JSON
{"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"}]}
JSON
  cat "$WORK/$1.json" | "$CONDUCT" workflow create "$1" --definition >/dev/null
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

---

## workflow run -d / --detach（后台起跑）

### TC-001 -d 后台起跑：打印 run id 退 0，run 落盘可查并跑到 completed

- **目的**：验证 `-d` 预检通过后以独立会话 spawn 子进程、父进程打印 run id 提示后退 `0`；该 run 能被 `run list` 查到，并由子进程独立跑到终态 `completed`。
- **前置**：
  1. `new_env; fast_engine; make_wf bg`。
- **步骤**：
  1. `"$CONDUCT" workflow run bg "后台跑一个" --cwd "$WORK" -d; echo "exit=$?"`
  2. 取回 run id，等它跑到终态：
     ```bash
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="bg"][0])')
     echo "final=$(poll_terminal "$RID")"
     ```
  3. `"$CONDUCT" run list --json | python3 -c 'import sys,json;print([(x["workflow"],x["status"]) for x in json.load(sys.stdin) if x["workflow"]=="bg"])'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 一行提示，形如 `已在后台启动 bg-<时间戳>；conduct run show bg-<时间戳> 查看进度、conduct run stop bg-<时间戳> 终止。`（run id 时间后缀每次不同，只校验 `已在后台启动 bg-`、`conduct run show`、`conduct run stop` 等子串，不逐字比对时间戳）。
  - 步骤 2 打印 `final=completed`——子进程脱离父进程后独立跑到了终态。
  - 步骤 3 打印 `[('bg', 'completed')]`——run 已落盘、可被 `run list` 查到（证明父进程等到了初始 `run.json` 才交回 id）。
- **清理**：`del_env`。

### TC-002 -d --json 吐单行句柄（只含 id + workflow，无 status）

- **目的**：验证 `-d --json` 打印**单行句柄 JSON**：只含 `id` 与 `workflow` 两键、**不含** `status`（句柄是可寻址句柄、非状态快照），且不是前台 `--json` 的逐步事件流。
- **前置**：
  1. `new_env; fast_engine; make_wf hj`。
- **步骤**：
  1. `"$CONDUCT" workflow run hj "拿句柄" --cwd "$WORK" -d --json > "$WORK/handle.json"; echo "exit=$?"`
  2. `wc -l < "$WORK/handle.json"`
  3. `python3 -c 'import json; d=json.load(open("'"$WORK"'/handle.json")); print(sorted(d.keys()), d["workflow"], "status" in d)'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 打印 `1`——句柄是**单行** JSON（非逐步事件流的多行输出）。
  - 步骤 3 打印 `['id', 'workflow'] hj False`——两键恰为 `id`/`workflow`，`status` 字段不存在。
- **清理**：`del_env`。

### TC-003 -d stdin 需求在 fork 前读完（数据流转：管道内容进 run.json）

- **目的**：验证 `<需求>` 来自管道 stdin 时，父进程在 spawn 前 `ReadAll` 整个 stdin，再喂给已脱离终端的子进程——断言管道里的**可辨识定值**确实流进了后台 run 的 `userPrompt`，而非只看退出码。
- **前置**：
  1. `new_env; fast_engine; make_wf si`。
- **步骤**：
  1. `echo "背景需求-ABC123" | "$CONDUCT" workflow run si --cwd "$WORK" -d; echo "exit=$?"`
  2. 等落盘后读回 `userPrompt`：
     ```bash
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="si"][0])')
     poll_terminal "$RID" >/dev/null
     "$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["userPrompt"] for x in json.load(sys.stdin) if x["workflow"]=="si"][0])'
     ```
- **预期**：
  - 步骤 1 退出码 `0`（省略位置参数、需求从管道读取，正常后台起跑）。
  - 步骤 2 打印 `背景需求-ABC123`——**这是本用例核心断言**：管道 stdin 的内容被父进程读完并传给了脱离终端的子进程，落进了后台 run 的 `userPrompt`。
- **清理**：`del_env`。

### TC-004 -d 起的 run 被 run stop 可靠终止（setsid 成组，附带收益）

- **目的**：验证 `-d` 起的 run 因 `setsid` 成为进程组组长，`run stop` 对它可靠生效——命令退 `0`、发出 SIGTERM，此后该 run 按 pid 判活派生为 `interrupted`。这是 spec〈后台运行〉「附带收益」的黑盒可观察结果（组信号连带引擎子进程的精确回收由单测覆盖）。
- **前置**：
  1. `new_env; slow_engine; make_wf sp`（慢引擎把该步拖在 running，留出终止窗口）。
- **步骤**：
  1. 后台起跑，等 `run.json` 落盘、确认在 running：
     ```bash
     "$CONDUCT" workflow run sp "慢慢跑" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="sp"][0])')
     "$CONDUCT" run list --json | python3 -c 'import sys,json;print("mid=",[x["status"] for x in json.load(sys.stdin) if x["workflow"]=="sp"][0])'
     ```
  2. 终止并查派生态（**短轮询**到 interrupted，避免单点 sleep 偶发读到僵尸期的 running）：
     ```bash
     "$CONDUCT" run stop "$RID"; echo "stop-exit=$?"
     for _ in $(seq 1 30); do
       AFT=$("$CONDUCT" run list --json | python3 -c "import sys,json;print(next((x['status'] for x in json.load(sys.stdin) if x['id']=='$RID'),''))")
       [ "$AFT" = interrupted ] && break; sleep 0.3
     done
     echo "after=$AFT"
     ```
- **预期**：
  - 步骤 1 打印 `mid= running`——后台 run 已落盘且在跑。
  - 步骤 2：`run stop` 退出码 `0`，stdout 形如 `已向运行 sp-<时间戳>（pid <n>）发送终止信号 SIGTERM。`（pid、时间戳忽略）；随后 `after=interrupted`——被终止的进程停写、按 pid 判活派生为 `interrupted`。
  - 归一化说明：`run stop` 后编排器成孤儿进程、需 init 回收后 `kill(pid,0)` 才返回 ESRCH，僵尸期内仍报存活；故用短轮询等派生态落定，不用单点 `sleep`。
- **清理**：`del_env`（其中 `pkill` 兜底收掉可能遗留的假 sleep 子进程）。

### TC-005 -d 缺需求且 stdin 是终端 → 退 2、不 fork（pty 伪终端驱动）

- **目的**：验证 `-d` 与前台同一套预检（fail-loud）：既无位置参数、stdin 又是**终端**时，父进程同步报参数缺失、退 `2`，**绝不 detach 之后再后台静默失败**，且不产生任何 run 记录。
- **前置**：
  1. `new_env; fast_engine; make_wf np`。
- **步骤**：
  1. 用 pty 把 stdin 接成真终端后运行 `-d`、不喂输入，超时守卫验证它立即报错而非挂起：
     ```bash
     python3 - "$CONDUCT" "$WORK" <<'PY'
     import os, pty, subprocess, sys
     conduct, work = sys.argv[1], sys.argv[2]
     master, slave = pty.openpty()            # slave 端 os.isatty()=True
     p = subprocess.Popen([conduct, "workflow", "run", "np", "-d"],
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
     print("runs=", real)                     # 应为空：预检就拦下、未 fork
     PY
     ```
- **预期**：
  - 脚本**立即返回、不停在等待输入**（不打印 `HANG`）。
  - 打印 `exit=2`，stderr 含 `缺少用户需求`。
  - 打印 `runs= []`——未 fork 出后台子进程、未产生任何 run 记录。
- **关键说明（勿踩坑）**：本用例断言的是 **stdin 是终端**这条路径，用 pty 伪终端复现，可无人值守自动化。**切勿用 `< /dev/null` 代替**——那是重定向（非 TTY），走「读整个 stdin 作需求」路径、读到空串，语义不同（详见 workflow-running.md TC-004 的同款说明）。
- **清理**：`del_env`。

### TC-006 -d --cwd 指向不存在的路径 → 退 2、不 fork

- **目的**：验证 `-d` 的 `--cwd` 预检与前台同源：显式 `--cwd` 指向不存在的路径时报用法错误退 `2`，发射前拦下、不 fork。
- **前置**：
  1. `new_env`（无需造工作流：`--cwd` 校验在载入工作流前触发）。
- **步骤**：
  1. `"$CONDUCT" workflow run any "hi" --cwd "$WORK/no-such-dir" -d 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
  2. `ls "$WORK/.conduct/runs/" 2>/dev/null | grep -c . || echo 0`
- **预期**：
  - 步骤 1 退出码 `2`；stderr 含 `--cwd 指向的路径不存在：` 且带该绝对路径（路径子串归一化，不逐字比对）。
  - 步骤 2 打印 `0`——未产生任何 run 记录。
- **清理**：`del_env`。

### TC-007 -d 工作流不存在 → 退 1、不 fork（不带病 detach）

- **目的**：验证载入错误也在父进程同步 fail-loud：`-d` 指定一个不存在的工作流时，载入失败退 `1`，绝不 detach 后再在后台静默失败。
- **前置**：
  1. `new_env`（不造该工作流）。
- **步骤**：
  1. `"$CONDUCT" workflow run nope "hi" --cwd "$WORK" -d 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
  2. `ls "$WORK/.conduct/runs/" 2>/dev/null | grep -c . || echo 0`
- **预期**：
  - 步骤 1 退出码 `1`；stderr 含 `nope: 工作流不存在`（实际形如 `conduct: nope: 工作流不存在`）。
  - 步骤 2 打印 `0`——载入阶段就失败，未 fork、无 run 记录。
- **清理**：`del_env`。

### TC-023 -d 缺需求且 stdin 是空管道（非 TTY 读到空内容）→ 退 2

- **目的**：验证与 TC-005（stdin 是 TTY）**对称的另一条空需求边界**：stdin 是管道 / 重定向（非 TTY）但内容为空时，`resolveUserPrompt` 读到空串、报用法错误退 `2`（区别于 TTY 路径的「缺少用户需求」，此路径文案为「读到空内容」）。这条预检同样在 fork 前同步做完。
- **前置**：
  1. `new_env; fast_engine; make_wf ev`。
- **步骤**：
  1. `"$CONDUCT" workflow run ev --cwd "$WORK" -d </dev/null 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
  2. `ls "$WORK/.conduct/runs/" 2>/dev/null | grep -c . || echo 0`
- **预期**：
  - 步骤 1 退出码 `2`；stderr 含 `stdin 未提供用户需求（读到空内容）`。
  - 步骤 2 打印 `0`——预检拦下、未 fork、无 run 记录。
  - **说明**：此处 `</dev/null` 是**有意**的（要的正是「非 TTY 且空」这条路径），与 TC-005 刻意用 pty 造 TTY 路径互补——两条空需求边界文案与触发条件不同，都要覆盖。
- **清理**：`del_env`。

---

## run wait（阻塞等待终态）

### TC-008 wait 已终态的 run 立即返回、退 0

- **目的**：验证对**已达终态**的 run 调 `run wait`：立即返回、不空等，退 `0`，stdout 打印终态摘要行。
- **前置**：
  1. `new_env; fast_engine; make_wf wt`。
  2. 前台跑一条 completed run 并取 id：
     ```bash
     "$CONDUCT" workflow run wt "跑完" --cwd "$WORK" >/dev/null
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
     ```
- **步骤**：
  1. `"$CONDUCT" run wait "$RID"; echo "exit=$?"`
- **预期**：
  - 退出码 `0`；stdout 形如 `运行 wt-<时间戳> · completed · 耗时 <t>s`（run id 时间后缀、耗时归一化，只校验含 `· completed ·`）。
- **清理**：`del_env`。

### TC-009 wait 阻塞到 running 的 run 跑完再转终态、退 0

- **目的**：验证对**仍在跑**的 run 调 `run wait`：阻塞轮询直到它转终态才返回（不提前返回），退 `0`、末态 `completed`。这是 `wait` 区别于「查一次 `run list`」的核心行为。
- **前置**：
  1. `new_env; slow_engine 3; make_wf sw`（慢引擎 sleep 3 秒，制造一段 running 窗口）。
  2. 后台起跑、确认在 running：
     ```bash
     "$CONDUCT" workflow run sw "慢答" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="sw"][0])')
     "$CONDUCT" run list --json | python3 -c 'import sys,json;print("before=",[x["status"] for x in json.load(sys.stdin) if x["workflow"]=="sw"][0])'
     ```
- **步骤**：
  1. `"$CONDUCT" run wait "$RID"; echo "exit=$?"`
  2. `"$CONDUCT" run list --json | python3 -c 'import sys,json;print("after=",[x["status"] for x in json.load(sys.stdin) if x["workflow"]=="sw"][0])'`
- **预期**：
  - 前置打印 `before= running`——wait 开始前 run 确实在跑。
  - 步骤 1：`run wait` **阻塞约数秒后**才返回（在 `sleep 3` 跑完之后），退出码 `0`，stdout 形如 `运行 sw-<时间戳> · completed · 耗时 <t>s`。
  - 步骤 2 打印 `after= completed`——wait 返回时 run 已到终态。
  - 归一化说明：本用例含时序，依赖慢引擎真 sleep；`wait` 返回时机紧跟 run 收尾，断言重点是「返回时已 completed」而非精确秒数。
  - **覆盖说明**：spec 另有一条「等待期间进程崩溃（pid 死）即派生 `interrupted`，也算等到终态」——它与本用例共用同一段派生逻辑（`waitForTerminal` 每轮调 `EffectiveStatus`，pid 死即返回 `interrupted`），无论 pid 是第 1 轮死还是途中死，代码路径相同，由单测 `TestWaitForTerminalDerivedInterrupted` 覆盖，本文不另设时序脆弱的手工用例。
- **清理**：`del_env`。

### TC-010 wait 对 failed 的 run 仍退 0（run 成败不进退出码）

- **目的**：验证 `run wait` 的退出码只表达「有没有等到终态」，**不表达 run 的成败**：等到一条 `failed` run，`wait` 照样退 `0`，run 的失败在 stdout 的 `status` 里体现（对标 docker wait / Unix wait）。
- **前置**：
  1. `new_env; broken_engine; make_wf wf`（坏引擎 → 该 run failed）。
  2. 前台跑一条 failed run（`workflow run` 自身因该步失败退 1，属正常）并取 id：
     ```bash
     "$CONDUCT" workflow run wf "会失败" --cwd "$WORK" >/dev/null 2>&1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
     ```
- **步骤**：
  1. `"$CONDUCT" run wait "$RID"; echo "wait-exit=$?"`
- **预期**：
  - **退出码 `0`**（不是 1）——`wait` 等到了终态即算完成本职；stdout 形如 `运行 wf-<时间戳> · failed · 耗时 <t>s`，run 的失败体现在 `· failed ·` 而非退出码。
  - **这是本用例核心断言**：别拿 `run wait` 的退出码判 run 成败，run 的成败要读 `status`。
- **清理**：`del_env`。

### TC-011 wait --json 输出规范化 run.json（含派生 status）

- **目的**：验证 `run wait --json` 打印收尾时 `run.json` 的规范化内容（同 `run show <id> --json`），含 `status` 字段——供脚本按 `status` 分支。
- **前置**：
  1. `new_env; broken_engine; make_wf wj`。
  2. 跑一条 failed run 并取 id：
     ```bash
     "$CONDUCT" workflow run wj "失败" --cwd "$WORK" >/dev/null 2>&1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
     ```
- **步骤**：
  1. `"$CONDUCT" run wait "$RID" --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["id"]==sys.argv[1], d["workflow"], d["status"])' "$RID"`
- **预期**：
  - 打印 `True wj failed`——输出是该 run 的规范化 run.json（`id` 与查询一致、`workflow` 为 `wj`、`status` 为 `failed`）。
- **清理**：`del_env`。

### TC-012 wait 不存在的 id → 退 1

- **目的**：验证 `run wait` 对不存在的 run 报错退 `1`（命令自身出错，非 `wait` 语义的 0）。
- **前置**：
  1. `new_env`（`runs/` 为空）。
- **步骤**：
  1. `"$CONDUCT" run wait no-such-000000 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `no-such-000000: 运行不存在`（实际形如 `conduct: no-such-000000: 运行不存在`）。
- **清理**：`del_env`。

### TC-013 wait 非法 id → 退 2

- **目的**：验证 `run wait` 对非法 id（含路径分隔符等，`run.ValidateID` 不过）报用法错误退 `2`，发射前拦下。
- **前置**：
  1. `new_env`。
- **步骤**：
  1. `"$CONDUCT" run wait "bad/id" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `run id "bad/id" 非法`。
- **清理**：`del_env`。

---

## run rm（删除运行记录）

### TC-014 rm --yes 删除终态 run（human 回执与 --json 两副面孔）

- **目的**：验证 `run rm <id> --yes` 删除终态 run 的整个 `runs/<id>/` 目录：human 回执 `✓ 已删除 <id>`、`--json` 回执 `{"deleted":["<id>"]}`，且目录确实被移除。用两条不同名 run 分别验两种输出（避免 run id 同秒撞车）。
- **前置**：
  1. `new_env; fast_engine; make_wf ra; make_wf rb`。
  2. 各跑一条 completed run 并取 id：
     ```bash
     "$CONDUCT" workflow run ra "甲" --cwd "$WORK" >/dev/null
     "$CONDUCT" workflow run rb "乙" --cwd "$WORK" >/dev/null
     RA=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="ra"][0])')
     RB=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="rb"][0])')
     ```
- **步骤**：
  1. `"$CONDUCT" run rm "$RA" --yes; echo "exit=$?"`
  2. `ls "$WORK/.conduct/runs/" | grep -c "^$RA$" || true`
  3. `"$CONDUCT" run rm "$RB" --yes --json > "$WORK/rm.json"; echo "exit=$?"; python3 -c 'import json;print(json.load(open("'"$WORK"'/rm.json")))'`
  4. `ls "$WORK/.conduct/runs/" | grep -c "^$RB$" || true`
- **预期**：
  - 步骤 1 退出码 `0`；stdout `✓ 已删除 <RA>`。
  - 步骤 2 打印 `0`——`runs/<RA>/` 目录已被移除。
  - 步骤 3 退出码 `0`；`--json` 回执是**缩进多行** JSON（`printJSON` 缩进输出），解析后恰为 `{'deleted': ['<RB>']}`（`deleted` 数组只含 RB 一个 id）。
  - 步骤 4 打印 `0`——`runs/<RB>/` 也已移除。
- **清理**：`del_env`。

### TC-015 rm 交互确认：空回车 / N 取消保留、y 才删除（pty 伪终端驱动）

- **目的**：验证交互终端（TTY）下未加 `--yes` 时的二次确认：回答非 `y`/`yes`（**含直接回车的空输入**、以及 `n`）视为取消——**stderr** `已取消`、退 `0`、**不删**；回答 `y` 才删除。用 pty 在自动化里复现真终端。spec〈run rm〉明确「其余（含直接回车）视为取消」，故空回车这条取值边界须专门覆盖。
- **前置**：
  1. `new_env; fast_engine; make_wf rc`。
  2. 跑一条 completed run 并取 id：
     ```bash
     "$CONDUCT" workflow run rc "丙" --cwd "$WORK" >/dev/null
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
     ```
- **步骤**：
  1. 依次答**空回车**（取消）、`n`（取消）、`y`（删除），同一 run id 连续三次（前两次不删，故 `y` 时仍在）：
     ```bash
     python3 - "$CONDUCT" "$RID" <<'PY'
     import os, pty, subprocess, sys
     conduct, rid = sys.argv[1], sys.argv[2]
     for ans in ("", "n", "y"):                   # "" 即直接回车（空输入）
         master, slave = pty.openpty()            # stdin 接 pty → os.isatty()=True，走交互确认分支
         p = subprocess.Popen([conduct, "run", "rm", rid],
                              stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
         os.write(master, (ans + "\n").encode()); os.close(slave)
         out, err = p.communicate(timeout=5)
         print(f"ans={ans!r} exit={p.returncode} stdout={out.strip()!r} stderr={err.strip()!r}")
         os.close(master)
     PY
     ```
  2. 确认最终已删：`ls "$WORK/.conduct/runs/" | grep -c "^$RID$" || true`
- **预期**：
  - 步骤 1 三行：
    - `ans=''`（空回车）：退出码 `0`，stdout 为空 `''`，stderr 含确认提示 `确认删除运行 <id>？[y/N]` 与 `已取消`——直接回车即取消。
    - `ans='n'`：退出码 `0`，stdout 为空 `''`，stderr 含 `已取消`（取消走 stderr，保 stdout 纯净）。
    - `ans='y'`：退出码 `0`，stdout 含 `✓ 已删除 <id>`。
  - 步骤 2 打印 `0`——空回车与 `n` 两次均未删（否则 `y` 那次会因不存在退 1）、`y` 那次删掉，最终目录不存在。**这是本用例的关系断言**：取消确实保留、确认确实删除。
- **清理**：`del_env`。

### TC-016 rm 非交互且无 --yes → 退 2、不删

- **目的**：验证非交互（非 TTY stdin）且未加 `--yes` 时，`run rm` 拒绝删除、报用法错误退 `2`，避免脚本误删。
- **前置**：
  1. `new_env; fast_engine; make_wf rn`。
  2. 跑一条 completed run 并取 id：
     ```bash
     "$CONDUCT" workflow run rn "丁" --cwd "$WORK" >/dev/null
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
     ```
- **步骤**：
  1. `"$CONDUCT" run rm "$RID" </dev/null 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
  2. `ls "$WORK/.conduct/runs/" | grep -c "^$RID$" || true`
- **预期**：
  - 步骤 1 退出码 `2`；stderr 含 `拒绝在非交互环境删除，请加 --yes`（`</dev/null` 使 stdin 非 TTY，触发该守卫）。
  - 步骤 2 打印 `1`——run 仍在、未被删。
- **清理**：`del_env`。

### TC-017 rm 仍在跑的 run → 退 1、不删

- **目的**：验证 `run rm` 拒删**仍在跑**（running 且 pid 存活）的 run，退 `1` 并提示先 `run stop`——不删正在写盘的活运行、以免留孤儿进程与半截目录。
- **前置**：
  1. `new_env; slow_engine; make_wf rr`。
  2. 后台起一条慢 run、确认在 running：
     ```bash
     "$CONDUCT" workflow run rr "拖住" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="rr"][0])')
     ```
- **步骤**：
  1. `"$CONDUCT" run rm "$RID" --yes 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
  2. `ls "$WORK/.conduct/runs/" | grep -c "^$RID$" || true`
- **预期**：
  - 步骤 1 退出码 `1`；stderr 含 `仍在进行中，无法删除` 且提示 `请先 conduct run stop`（即便加了 `--yes` 也拒删活运行）。
  - 步骤 2 打印 `1`——run 仍在、未被删。
- **清理**：`del_env`（`pkill` 收掉遗留的假 sleep 子进程）。

### TC-018 rm 不存在的 id → 退 1

- **目的**：验证 `run rm` 删一个不存在的 run 时报错退 `1`。
- **前置**：
  1. `new_env`（`runs/` 为空）。
- **步骤**：
  1. `"$CONDUCT" run rm no-such-000000 --yes 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `no-such-000000: 运行不存在`（加 `--yes` 跳过确认守卫，直抵「存在性」检查后失败）。
- **清理**：`del_env`。

### TC-019 rm 非法 id → 退 2

- **目的**：验证 `run rm` 对非法 id（`run.ValidateID` 不过）报用法错误退 `2`，发射前拦下。
- **前置**：
  1. `new_env`。
- **步骤**：
  1. `"$CONDUCT" run rm "bad/id" --yes 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `run id "bad/id" 非法`。
- **清理**：`del_env`。

---

## run list --status（按状态过滤）

### TC-020 --status 过滤 completed / failed（两条 run，验证真过滤而非只跑通）

- **目的**：验证 `run list --status <state>` 按**派生态**过滤：造一条 `completed` 与一条 `failed`（不同 workflow 名，避免同秒撞 id），断言 `--status completed` 只出前者、`--status failed` 只出后者、无过滤时两者都在。用两条异态记录触发过滤关系，避免退化夹具（单条记录验不出「过滤掉了谁」）。
- **前置**：
  1. `new_env`。
  2. 先用快引擎跑一条 completed，再换坏引擎跑一条 failed：
     ```bash
     fast_engine;   make_wf ok;  "$CONDUCT" workflow run ok  "好" --cwd "$WORK" >/dev/null 2>&1
     broken_engine; make_wf bad; "$CONDUCT" workflow run bad "坏" --cwd "$WORK" >/dev/null 2>&1
     ```
- **步骤**：
  1. `"$CONDUCT" run list --status completed --json | python3 -c 'import sys,json;print(sorted((x["workflow"],x["status"]) for x in json.load(sys.stdin)))'`
  2. `"$CONDUCT" run list --status failed --json | python3 -c 'import sys,json;print(sorted((x["workflow"],x["status"]) for x in json.load(sys.stdin)))'`
  3. `"$CONDUCT" run list --json | python3 -c 'import sys,json;print(len(json.load(sys.stdin)))'`
- **预期**：
  - 步骤 1 打印 `[('ok', 'completed')]`——只出 completed 的那条，failed 被滤掉。
  - 步骤 2 打印 `[('bad', 'failed')]`——只出 failed 的那条，completed 被滤掉。
  - 步骤 3 打印 `2`——不加 `--status` 时默认列全部（不改既有默认）。
- **清理**：`del_env`。

### TC-021 --status running 只留 pid 真存活的运行（派生态过滤）

- **目的**：验证 `--status running` 按**派生态**（pid 真存活）过滤：一条在跑的 run 出现在 `--status running`、不出现在 `--status completed`。
- **前置**：
  1. `new_env; slow_engine; make_wf lr`。
  2. 后台起一条慢 run、确认落盘：
     ```bash
     "$CONDUCT" workflow run lr "在跑" --cwd "$WORK" -d >/dev/null; sleep 1
     ```
- **步骤**：
  1. `"$CONDUCT" run list --status running --json | python3 -c 'import sys,json;print(sorted((x["workflow"],x["status"]) for x in json.load(sys.stdin)))'`
  2. `"$CONDUCT" run list --status completed --json | python3 -c 'import sys,json;print([x["workflow"] for x in json.load(sys.stdin)])'`
- **预期**：
  - 步骤 1 打印 `[('lr', 'running')]`——在跑的 run 落入 running 派生态。
  - 步骤 2 打印 `[]`——它不在 completed 过滤结果里。
- **清理**：`del_env`（`pkill` 收掉遗留的假 sleep 子进程）。

### TC-022 --status 非法取值 → 退 2

- **目的**：验证 `--status` 传入枚举外的取值时报用法错误退 `2`。
- **前置**：
  1. `new_env`（无需任何 run：取值解析在读 store 前）。
- **步骤**：
  1. `"$CONDUCT" run list --status bogus 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `非法的 --status 取值 "bogus"` 且列出可用值 `running / completed / failed / interrupted`。
- **清理**：`del_env`。

### TC-024 --status interrupted 过滤派生态，且过滤后为空时给 human 提示

- **目的**：补齐 `--status` 第四个派生态 `interrupted`（running 但 pid 已死）的过滤——它与 `running`（TC-021）对称，spec 列为四个合法取值之一。造一条被 `run stop` 打成 `interrupted` 的 run，断言 `--status interrupted` 命中、`--status running` 不命中；顺带验证**过滤后为空时**人类模式给 `（暂无运行记录）` 提示（spec〈run list〉「过滤后为空 → 提示、退 0」）。
- **前置**：
  1. `new_env; slow_engine; make_wf li`。
  2. 后台起一条慢 run、确认在 running，再 `run stop` 打成 interrupted：
     ```bash
     "$CONDUCT" workflow run li "在跑" --cwd "$WORK" -d >/dev/null; sleep 1
     RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json;print([x["id"] for x in json.load(sys.stdin) if x["workflow"]=="li"][0])')
     "$CONDUCT" run stop "$RID" >/dev/null
     for _ in $(seq 1 30); do                     # 短轮询到派生态落定（僵尸期内仍报存活，见 TC-004 说明）
       s=$("$CONDUCT" run list --json | python3 -c "import sys,json;print(next((x['status'] for x in json.load(sys.stdin) if x['id']=='$RID'),''))")
       [ "$s" = interrupted ] && break; sleep 0.3
     done; echo "derived=$s"
     ```
- **步骤**：
  1. `"$CONDUCT" run list --status interrupted --json | python3 -c 'import sys,json;print(sorted((x["workflow"],x["status"]) for x in json.load(sys.stdin)))'`
  2. `"$CONDUCT" run list --status running --json | python3 -c 'import sys,json;print([x["workflow"] for x in json.load(sys.stdin)])'`
  3. `"$CONDUCT" run list --status completed`
- **预期**：
  - 前置打印 `derived=interrupted`——run 已被打成派生 interrupted。
  - 步骤 1 打印 `[('li', 'interrupted')]`——interrupted 派生态被正确过滤命中。
  - 步骤 2 打印 `[]`——它不在 running 过滤结果里（pid 已死，不再算 running）。
  - 步骤 3（人类模式、无 completed 记录 → 过滤后为空）：stdout 打印 `（暂无运行记录）`、退出 `0`。
- **清理**：`del_env`（`pkill` 兜底收掉遗留的假 sleep 子进程）。
