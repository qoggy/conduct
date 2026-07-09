# workflow 运行 测试用例

覆盖 conduct 中**跑工作流、查运行记录、终止运行**的一族命令：`workflow run`、`run list`、`run show`、`run stop`。工作流定义的增删改查见 [workflow-editing.md](./workflow-editing.md)；聚合视图 `ui` 的 CLI 冒烟见本文 TC-012，其服务端与 `/api/*` 端点的黑盒覆盖见 [ui-server.md](./ui-server.md)。对应 spec：[docs/specs/cli-runtime.md](../specs/cli-runtime.md)（`ui` 见 [cli-tooling.md](../specs/cli-tooling.md)〈ui〉）。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**（命令该怎样表现），用来验证实现对不对——不是照现有代码反推。当前实现状态（见 spec〈实现状态〉）：`workflow run`、`run list`、`run show`（含 `--trace`/`--json`）、`run stop` 与 `ui` 服务端**均已实装**，预期可直接对照验证。两点行为变更需注意：① `run show` 默认视图（不加 `--trace`）现打印 `run-summary.md` 全文（终态）/ 状态摘要（未收尾），不再是旧版「概要 + 每步 80 字预览」（见 TC-007 / TC-008）；② `ui` 已从占位骨架变为可用服务端（TC-012 现可通过），内嵌前端 SPA 代码已落地、待浏览器走查验收。命令若有偏离本文〈预期〉，即为实现未达标。

> **用户视角、不伪造内部数据（关键）**：查询类命令（`run list`/`run show`）要「有一条运行记录」才能查。本文**一律先用真实的 `workflow run` 把记录跑出来，再查**——绝不手写 `run.json`/`trace.jsonl` 去「摆拍」一条记录（那是与 conduct 内部存储格式死耦合的伪造，见 test-case-writing skill 的 MUST〈用户视角〉）。代价是这些用例也要真调引擎、算 💸；这是保真度换来的，值得。

> **💸 费钱标记**：带 💸 的用例会真实调用 AI 引擎、消耗 token。除个别（TC-015 需 agent 执行一条 `sleep`）外，其 workflow 都是「回一个词」的极简节点、单步秒级完成，仅作打通验证，**请勿改成大重构 / 写复杂页面 / 长耗时任务**。不带 💸 的用例（缺参报错 TC-004、空列表 TC-005、引擎坏掉 TC-010、不存在 id TC-011）不产生真实引擎调用、零 token。

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

💸 用例复用的**最小 workflow 定义**（单节点、只回一个词，导入体仅需 `nodes`）：

```bash
# 各 💸 用例前置里写入 $PROJ/min.json（$PROJ 为该用例的临时 --cwd 目录）
write_min() {   # 用法：write_min   —— 把最小定义写到 $PROJ/min.json
  cat > "$PROJ/min.json" <<'JSON'
{
  "nodes": [
    {
      "id": "say",
      "displayName": "打招呼",
      "engine": "claude-code",
      "promptTemplate": "只回复一个词：hello。不要读写任何文件、不要做别的事。需求：{{sys.userPrompt}}"
    }
  ]
}
JSON
}
```

---

## workflow run

### TC-001 💸 run 端到端跑通最小工作流

- **目的**：验证 `workflow run <name> "<需求>"` 展开、驱动引擎、落盘 trace 并给出完成提示。
- **前置**：
  1. 真实家目录的 `claude` CLI 已装并登录。
  2. `PROJ=$(mktemp -d)`（引擎工作目录）；`write_min`（写 `$PROJ/min.json`）。
  3. `cat "$PROJ/min.json" | "$CONDUCT" workflow create hello --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow run hello "打个招呼" --cwd "$PROJ"; echo "exit=$?"`
  2. `ls "$HOME/.conduct/runs/" | grep '^hello-'`
  3. `python3 -c 'import json,glob,os; f=glob.glob(os.path.expanduser("~/.conduct/runs/hello-*/run.json"))[0]; d=json.load(open(f)); print(d["status"], d["workflow"], d["steps"])'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 先打印 `▶ 展开为 1 步：` 与该步清单，随后 `● step 0 [打招呼] agent ...`、`✓ ...ms tokens=... 产物 ... 字符：...`，末行 `✅ 完成，阅读 <.../run-summary.md> 获取运行详情`。
  - 步骤 2 出现一个 run 目录，名形如 `hello-YYYYMMDD-HHMMSS`；其中含 `run.json`、`run-summary.md`、`trace.jsonl` 三文件。
  - 步骤 3 打印 `completed hello 1`。
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
  - 步骤 1 退出码 `0`；正常展开并完成，末行 `✅ 完成，阅读 ...`。
  - 步骤 2 打印 `从 stdin 来的需求`（`run.json` 的 `userPrompt`，尾随换行已 strip）。
- **清理**：`cleanup_run hi; rm -rf "$PROJ"`。

### TC-003 💸 run --json 逐步输出事件

- **目的**：验证 `--json` 逐步吐出事件 JSON（无进度装饰），每行即一条 trace。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create je --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow run je "hi" --cwd "$PROJ" --json > "$PROJ/out.jsonl"; echo "exit=$?"`
  2. `python3 -c 'import json;[json.loads(l) for l in open("'"$PROJ"'/out.jsonl") if l.strip()]; print("all_lines_json_ok")'`
  3. `python3 -c 'import json; d=json.loads(open("'"$PROJ"'/out.jsonl").readline()); print(d["stepIndex"], d["type"], d["nodeId"], d["success"])'`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 打印 `all_lines_json_ok`（每行都是合法 JSON，无 `▶`/`●`/`✓` 等人类装饰行）。
  - 步骤 3 打印 `0 agent say True`（首行事件的 `stepIndex`/`type`/`nodeId`/`success`；用解析取值而非逐字匹配键序）。
- **清理**：`cleanup_run je; rm -rf "$PROJ"`。

### TC-004 run 缺需求且 stdin 是终端时报错、不挂起（零成本，pty 伪终端驱动）

- **目的**：验证既无位置参数、stdin 又是**终端**时报参数缺失、退出 `2`，不静默挂起、不调用引擎（spec〈用户需求的来源〉最后一句）。
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

     （沿用〈前置〉里 `export HOME="$WORK"`，python 子进程继承同一隔离 store。）
- **预期**：
  - 脚本**立即返回、不停在等待输入**（不打印 `HANG`）。
  - 打印 `exit=2`，stderr 含 `缺少用户需求`。
  - 打印 `runs= []`——未产生 run 记录（未触发引擎、零 token）。
- **关键说明（勿踩坑）**：本用例断言的是 **stdin 是终端**这一路径——用 pty 伪终端把 stdin 接成 tty 即可在 CI / 无人值守自动化里复现，**不必真人守终端**。**切勿用 `< /dev/null` 代替**——按 spec，`< /dev/null` 是重定向（非 TTY），走的是「读取整个 stdin 作需求」路径，会读到**空串**需求（其行为 spec 未定义，甚至可能拿空 prompt 触发引擎、意外烧钱），退出码也非 `2`。两条路径语义不同，不可混用。若要人工复核，也可在真实终端直接敲 `"$CONDUCT" workflow run np` 观察其立即退出 `2`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## run list

### TC-005 run list 无记录时提示（零成本）

- **目的**：验证无运行记录时 `run list` 给提示、退出 `0`。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run list; echo "exit=$?"`
- **预期**：
  - 退出码 `0`，stdout 含 `（暂无运行记录）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-006 💸 run list 列出真实跑出的运行记录

- **目的**：验证 `run list` 从 `runs/` 解析并按表格 / `--json` 列出——记录由真实 `workflow run` 产出（非伪造）。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create rl --definition`。
  3. `"$CONDUCT" workflow run rl "打个招呼" --cwd "$PROJ" >/dev/null`（真跑一条 completed run）。
- **步骤**：
  1. `"$CONDUCT" run list; echo "exit=$?"`
  2. `"$CONDUCT" run list --json | python3 -c 'import sys,json; d=[x for x in json.load(sys.stdin) if x["workflow"]=="rl"][0]; print(d["workflow"], d["status"], d["steps"])'`
- **预期**：
  - 步骤 1 退出码 `0`；表格有表头 `RUN ID`/`WORKFLOW`/`STATUS`/`STEPS`/`STARTED`/`PROMPT`，含一行同时出现 `rl`、`completed`、`1`、`打个招呼` 各字段（**只校验各字段子串出现，不比对列间空白与对齐宽度**——那是随数据变的实现细节）；`RUN ID` 形如 `rl-YYYYMMDD-HHMMSS`。
  - 步骤 2 打印 `rl completed 1`。
- **清理**：`cleanup_run rl; rm -rf "$PROJ"`。

---

## run show

### TC-007 run show 默认打印运行总结（run-summary.md 全文）

- **目的**：验证 `run show <id>`（默认、不加 `--trace`）打印 `run-summary.md` 全文——概要头 + 步骤表 + **逐节点完整产物**（**行为变更**：旧版是「概要 + 每步 80 字预览」，现改为总结全文、产物不截断）。
- **前置**（**零 💸**：`run show` 只读回落盘数据、**不调引擎**，故用一个**确定性假引擎**顶替真 claude——避免真实调用的不确定产物，run 记录仍全由 conduct 自己产出；与 TC-010 弄坏引擎同理，属外部依赖的测试替身，非伪造内部数据）：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装一个「打印固定 JSON、零 token」的假 claude（claude-code 引擎解析 stdout 的 `{"result":…}`）：
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
     {"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create ok --definition
     "$CONDUCT" workflow run ok "打个招呼" --cwd "$WORK" >/dev/null
     RID=$(ls "$WORK/.conduct/runs/" | grep '^ok-' | head -1)
     ```
- **步骤**：
  1. `"$CONDUCT" run show "$RID"; echo "exit=$?"`
  2. `"$CONDUCT" run show "$RID" | grep -c 'HELLO-ARTIFACT'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 是 `run-summary.md` 全文：首行 `# ok-<时间戳>`；含 `**工作流** ok · 1 节点`、`**需求** 打个招呼`、`**状态** ✅ completed …`；有 `## 步骤` 表（表头 `| # | 节点 | 引擎 | 耗时 |`，一行以 `| 0 | 打招呼 | claude-code |` 打头）；有 `## 产物` 段，内含 `<output node="say" name="打招呼">` 包裹的**完整**产物。
  - 步骤 2 打印 `1`——默认视图即含完整产物 `HELLO-ARTIFACT`（总结给全文、不截断；这正是与旧版「80 字预览」相反的新行为）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：`pkill -f "$WORK/fakebin/claude" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 run show --trace 打印状态摘要 + 每步完整 input/output

- **目的**：验证 `--trace` 改变**深度**：打印状态摘要（运行行 / 需求 / 步数 / 耗时）后，逐步展开每步**完整** input 与 output（区别于默认视图的 run-summary.md 报告形态）。
- **前置**：同 TC-007 前置 1-3（临时 HOME + 确定性假引擎 + 跑一条 `completed` run），但 workflow 名改用 `tr`：`cat "$WORK/min.json" | "$CONDUCT" workflow create tr --definition`、`"$CONDUCT" workflow run tr "打个招呼" --cwd "$WORK" >/dev/null`、`RID=$(ls "$WORK/.conduct/runs/" | grep '^tr-' | head -1)`。
- **步骤**：
  1. `"$CONDUCT" run show "$RID" --trace; echo "exit=$?"`
  2. `"$CONDUCT" run show "$RID" --trace | grep -c 'HELLO-ARTIFACT'`
- **预期**：
  - 步骤 1 退出码 `0`；stdout 首为状态摘要（`运行 tr-<时间戳> · completed`、`需求：打个招呼`、`步数 1 · 耗时 …`），随后一行 `● step 0 [打招呼] agent claude-code  成功`，其下 `  ── input ──` 段为该步完整输入（含 `回复：hi。需求：打个招呼`）、`  ── output ──` 段为该步完整产物。
  - 步骤 2 打印 `1`——`--trace` 的 output 段含完整产物 `HELLO-ARTIFACT`（与 TC-007 同为全文，区别在**呈现形态**：TC-007 是总结报告、本例是逐步原始 input/output）。
  - 归一化说明：run id 时间后缀、耗时忽略。
- **清理**：`pkill -f "$WORK/fakebin/claude" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-009 💸 run show --json 输出规范化 run.json

- **目的**：验证 `--json` 输出 `run.json` 规范化内容；`--json --trace` 组合额外含 `"trace":[…]`（spec〈run show〉四组合表）。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`；`write_min`。
  2. `cat "$PROJ/min.json" | "$CONDUCT" workflow create js --definition`。
  3. `"$CONDUCT" workflow run js "hi" --cwd "$PROJ" >/dev/null`；`RID=$(ls "$HOME/.conduct/runs/" | grep '^js-' | head -1)`。
- **步骤**：
  1. `"$CONDUCT" run show "$RID" --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["workflow"], d["status"], "trace" in d)'`
  2. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; d=json.load(sys.stdin); print("trace_len", len(d["trace"]))'`
- **预期**：
  - 步骤 1 打印 `js completed False`（不加 `--trace` 的 `--json` 只给 run.json 概要，无 `trace` 字段）。
  - 步骤 2 打印 `trace_len 1`（`--json --trace` 附上 `trace.jsonl` 逐行）。
- **清理**：`cleanup_run js; rm -rf "$PROJ"`。

### TC-010 引擎坏掉时节点失败、失败信息落盘并可查（零成本）

- **目的**：验证被测引擎的二进制**不可用 / 报错退出**时，conduct 让该节点失败、把失败信息**真实落盘**（`run.json` 的 `status:"failed"`+`error`，`trace.jsonl` 的 `success:false`+`error`），且 `run show` 能呈现失败。**引擎是 conduct 的外部依赖**，弄坏它是复现「用户机器上引擎真坏了」的真实场景，不是伪造数据——run 记录仍全由 conduct 自己产出。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。引擎会立即失败、不真调 API，故无需登录、零 token。
  2. **弄坏引擎**：在 PATH 前置一个「一运行就报错退出」的假 `claude`，遮蔽真 `claude`：
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
  2. `RID=$(ls "$WORK/.conduct/runs/" | grep '^bf-' | head -1)`
  3. `"$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; d=json.load(sys.stdin); tr=d["trace"]; print(d["status"], "|", d["error"], "|", tr[0]["stepIndex"], tr[0]["success"])'`
  4. `python3 -c 'import json,glob,os; p=glob.glob(os.path.expanduser("~/.conduct/runs/bf-*"))[0]; t=[json.loads(l) for l in open(p+"/trace.jsonl") if l.strip()]; print(t[0]["success"], "|", t[0]["error"])'`
  5. `"$CONDUCT" run show "$RID"; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`；stdout/stderr 报该步失败。
  - 步骤 3 打印 `failed | claude 退出码 1: claude: 引擎不可用（模拟故障） | 0 False`（`status:"failed"`、`error` 以引擎二进制名 `claude` 打头，含退出码 / stderr 摘要；失败步由 trace 的 `stepIndex=0 success=false` 记录体现）。
  - 步骤 4 打印 `False | claude 退出码 1: claude: 引擎不可用（模拟故障）`（trace 首步 `success:false` 且带同一 error）。
  - 步骤 5 退出码 `0`；`run show` 呈现状态 `failed`、失败步 `step 0`、错误摘要。
  - **现状注**：本用例的假引擎把错误写在 stderr，故走 `commandError`（`internal/engine/exec.go`）的退出码+stderr 摘要路径。若引擎（仅 claude-code）把诊断写在 **stdout**（真 `claude` 的 `is_error` JSON 即在 stdout）、stderr 为空，`internal/engine/claudecode.go` 会先尝试从 stdout JSON 的 `result` 取报错原因（`claudeStdoutFailureMessage`），能取到就返回 `claude 报错: <result>`，不会落到退出码摘要；单测 `TestClaudeCodeRunNonZeroExitStdoutResult` / `TestClaudeCodeRunNonZeroExitStdoutNotJSON`（`internal/engine/exec_test.go`）覆盖了这两条分支。
- **清理**：`export PATH="$OLD_PATH"; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 run show 不存在的 id 报错（零成本）

- **目的**：验证查询不存在 run 时失败。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run show no-such-000000; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `no-such-000000: 运行不存在`（实际形如 `conduct: no-such-000000: 运行不存在`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## ui

### TC-012 ui 启动并打印入口地址（交互 / 半自动）

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
  - **说明**：`ui` 服务端已实装，本用例（CLI 层冒烟：启动、打印地址、驻留）现应通过。服务端启动的错误路径（端口占用 / store 不可读）与 `/api/*` 全端点的黑盒覆盖见 [ui-server.md](./ui-server.md)，本文不重复。
  - 归一化说明：端口号非确定，忽略；本用例含时序成分，非纯确定性，必要时人工在终端 `conduct ui` 目视确认并 `Ctrl-C` 退出。
- **清理**：`kill "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 补充：运行时数据流转（跨节点注入 / 自循环反馈拼接）

TC-001~003 只用**单节点**工作流，验不出「前置节点产物注入后续节点输入」与「自循环轮次间的反馈拼接」这两条运行时核心数据流。以下两条用**多节点 / 带循环**的工作流触发这些关系，并断言**数据确实流动了**（在下游 trace 的 `input` 里验证），而非只看退出码。均为 💸 用例（真调引擎），prompt 仍是「回一个词」的极简任务，在真实 HOME 里跑、用后 `cleanup_run`。

### TC-013 💸 前置节点产物注入后续节点输入（跨节点 `{{nodeId}}` render）

- **目的**：验证运行时 `{{<nodeId>}}` 把前置节点的产物 render 进后续节点的输入——这是多节点工作流的核心数据流，单节点用例覆盖不到。用「前节点产出可辨识定值、后节点引用它」来断言注入确实发生。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`。
  2. 造两节点工作流（`gen` 产出定值 `APPLE`，`use` 用 `{{gen}}` 引用它）并入库：
     ```bash
     cat > "$PROJ/inject.json" <<'JSON'
     {
       "nodes": [
         {"id":"gen","displayName":"产出","engine":"claude-code","promptTemplate":"只回复一个词：APPLE。不要读写任何文件。需求：{{sys.userPrompt}}"},
         {"id":"use","displayName":"引用","engine":"claude-code","promptTemplate":"上一步的产物是：{{gen}}。只回复一个词：OK。不要读写任何文件。"}
       ]
     }
     JSON
     cat "$PROJ/inject.json" | "$CONDUCT" workflow create inj --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow run inj "开始" --cwd "$PROJ"; echo "exit=$?"`
  2. 断言 `gen` 产物与「其产物被注入 `use` 输入」：
     ```bash
     python3 - <<PY
     import json, glob, os
     d = glob.glob(os.path.expanduser("~/.conduct/runs/inj-*"))[0]
     run = json.load(open(d + "/run.json"))
     trace = [json.loads(l) for l in open(d + "/trace.jsonl") if l.strip()]
     print("gen_artifact=", run["artifacts"]["gen"])
     print("use_input_has_APPLE=", "APPLE" in trace[1]["input"])
     PY
     ```
- **预期**：
  - 步骤 1 退出码 `0`，正常展开 2 步并完成。
  - 步骤 2 打印 `gen_artifact= APPLE`（前节点产出定值）与 `use_input_has_APPLE= True`——即 `gen` 的产物 `APPLE` 被 render 注入了 `use` 步的 `input`。**这是本用例的核心断言**：数据确实跨节点流动，而非只是命令退出 0。
  - 归一化说明：引擎偶发不严格「只回一个词」时，只要 `gen` 产物含 `APPLE` 且该串出现在 `use` 的 input 即算通过；断言用子串包含而非全等。
- **清理**：`cleanup_run inj; rm -rf "$PROJ"`。

### TC-014 💸 自循环轮次间的反馈拼接（agent 追评测、evaluator 追被评产物）

- **目的**：验证带 evaluator 的自循环节点，运行时会双向拼接：evaluator 的输入里含**被评 agent 的产物**（`<artifact_under_review>`），下一轮 agent 的输入里含**上一轮评测反馈**（`<previous_evaluator_feedback>`）。单次执行的用例覆盖不到，需 `loopCount≥1` 触发多轮。
- **前置**：
  1. 真实家目录 `claude` 已就绪；`PROJ=$(mktemp -d)`。
  2. 造单节点自循环工作流（`loopCount:1` + evaluator，展开 agent→eval→agent 三步）并入库：
     ```bash
     cat > "$PROJ/loopfeed.json" <<'JSON'
     {
       "nodes": [
         {"id":"work","displayName":"作答","engine":"claude-code","promptTemplate":"只回复一个词：DONE。不要读写任何文件。需求：{{sys.userPrompt}}","loopCount":1,
          "evaluator":{"engine":"claude-code","promptTemplate":"只回复：<verdict>PASS</verdict>。不要读写任何文件。"}}
       ]
     }
     JSON
     cat "$PROJ/loopfeed.json" | "$CONDUCT" workflow create lf --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow run lf "作答" --cwd "$PROJ"; echo "exit=$?"`
  2. 断言三步 trace 的双向拼接：
     ```bash
     python3 - <<PY
     import json, glob, os
     d = glob.glob(os.path.expanduser("~/.conduct/runs/lf-*"))[0]
     trace = [json.loads(l) for l in open(d + "/trace.jsonl") if l.strip()]
     print("steps=", len(trace))
     print("eval_sees_artifact=", trace[1]["type"]=="evaluator" and "<artifact_under_review>" in trace[1]["input"])
     print("agent2_sees_feedback=", trace[2]["type"]=="agent" and "<previous_evaluator_feedback>" in trace[2]["input"])
     PY
     ```
- **预期**：
  - 步骤 1 退出码 `0`，展开 3 步（agent→evaluator→agent）并完成。
  - 步骤 2 打印 `steps= 3`、`eval_sees_artifact= True`、`agent2_sees_feedback= True`——即 evaluator 的输入拼接了被评 agent 的产物、第二轮 agent 的输入拼接了上一轮评测反馈。**这是本用例的核心断言**：自循环的反馈确实在轮次间双向流动。
- **清理**：`cleanup_run lf; rm -rf "$PROJ"`。

---

## 补充：运行中（running）状态可见

TC-006~009 查的都是**已终结**的 run。本节验一条**仍在途**的 run：运行途中 `run list`/`run show` 应报 `running`（spec 的 `running`+pid 存活语义），跑完转 `completed`。做法是让 agent 真执行一条耗时命令把运行拖住，途中开查。

### TC-015 💸 运行途中 run list / run show 显示 running，完成后转 completed

- **目的**：验证 workflow 运行**在途时**，另开查询能看到 `status:"running"`；进程结束后同一条记录转 `completed`。这是「运行中检测」的核心，用已终结的 run 覆盖不到。
- **前置**：
  1. 真实家目录 `claude` 已就绪（agent 需能执行 shell 命令）；`PROJ=$(mktemp -d)`。
  2. 造一个「让 agent 先 sleep 再回话」的 workflow（把单步拖到约 8 秒，留出查询窗口）：
     ```bash
     cat > "$PROJ/slow.json" <<'JSON'
     {
       "nodes": [
         {"id":"say","displayName":"慢答","engine":"claude-code",
          "promptTemplate":"请先执行 shell 命令 `sleep 8`，等它执行完，再只回复一个词：DONE。不要读写任何文件、不要做别的事。"}
       ]
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
     "$CONDUCT" run list | grep '^sl-\|STATUS'
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
  - 归一化说明：本用例含时序，依赖 agent 真的执行了 `sleep 8`。若 agent 不听话（没 sleep、秒回）导致查询窗口错过 running，会看到 `completed`——此时调大 `sleep` 秒数与 prompt 里的时长后重跑。查询窗口内的 `running` 是断言重点，`pid` 为运行时判活字段（spec 语义：`running` 且 pid 已死 → `run show` 派生展示为 `interrupted`）。
- **清理**：`cleanup_run sl; rm -rf "$PROJ"`。

---

## 补充：workflow run 的 --cwd 与空需求校验（零成本）

**行为变更**：显式 `--cwd` 现做「已存在的目录」校验——不存在 / 不是目录即报用法错误退 `2`，发射前拦下、不烧引擎；位置参数需求为空白也与 stdin 路径同标准退 `2`。三者都在载入工作流、调引擎**之前**触发（甚至工作流不存在也照样先报这些），故全零成本、全隔离临时 HOME。

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

`run stop <id>` 向运行的进程发 SIGTERM（先按进程组、非组长回退单进程）。**仅 `running` 可终止**：不存在 / 已终态 / `running` 但 pid 已死（interrupted）均报错退 `1`；终止后不落新状态，进程停写、pid 判活派生为 `interrupted`。以下三例全零成本：错误路径不调引擎；happy path 用一个「只 sleep、零 token」的**假慢引擎**把 run 拖在 `running`，供 stop 命中（假引擎是 conduct 的外部依赖替身，run 记录仍由 conduct 自己产出，同 TC-010）。

### TC-019 run stop 不存在的 id → 退 1

- **目的**：验证终止一个不存在的 run 时报错退 `1`。
- **前置**：建隔离环境（临时 HOME、`runs/` 为空）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" run stop no-such-000000 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `no-such-000000: 运行不存在`（实际形如 `conduct: no-such-000000: 运行不存在`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-020 run stop 已终结（failed）的运行 → 退 1（仅 running 可终止）

- **目的**：验证对**已终态**的 run 调 `run stop` 报「无可终止」退 `1`。用弄坏引擎造一条真实的 `failed` run（零 token，同 TC-010）。
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
     {"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create bf --definition
     "$CONDUCT" workflow run bf "会失败" --cwd "$WORK" >/dev/null 2>&1
     RID=$(ls "$WORK/.conduct/runs/" | grep '^bf-' | head -1)
     ```
- **步骤**：
  1. `"$CONDUCT" run stop "$RID" 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `1`；stderr 含 `当前状态为 failed，无可终止（仅 running 可终止）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-021 run stop 终止运行中的运行 → 退 0，转 interrupted

- **目的**：验证对一条**运行中**的 run 调 `run stop`：命令退 `0` 并提示已发送 SIGTERM；被终止的进程停写、不落新状态，此后 `run list` / `run show` 按 pid 判活**派生**为 `interrupted`（run.json 存储态仍为 `running`）；且 `run show` 默认视图在未收尾时给状态摘要 + 「运行总结尚未生成」提示（**行为变更**：未收尾不再打印逐步预览，改打印状态并指路 `--trace`）。
- **前置**：
  1. 建隔离环境（临时 HOME）：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
  2. 装一个「只 sleep、零 token」的**假慢引擎**（把该步长时间拖在 `running`，留出终止窗口；conduct 在解析其输出前就会被终止，故 sleep 后随便回什么都行）：
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
     {"nodes":[{"id":"say","displayName":"打招呼","engine":"claude-code","promptTemplate":"回复：hi。需求：{{sys.userPrompt}}"}]}
     JSON
     cat "$WORK/min.json" | "$CONDUCT" workflow create ss --definition
     ```
- **步骤**：
  1. 后台起跑，等 run.json 落盘 `running`：
     ```bash
     "$CONDUCT" workflow run ss "慢慢来" --cwd "$WORK" >/dev/null 2>&1 &
     RUNPID=$!
     sleep 2
     RID=$(ls "$WORK/.conduct/runs/" | grep '^ss-' | head -1); echo "RID=$RID"
     ```
  2. 途中确认为 running，再终止：
     ```bash
     "$CONDUCT" run list | grep -E 'ss-|STATUS'
     "$CONDUCT" run stop "$RID"; echo "exit=$?"
     ```
  3. 终止后查派生态：
     ```bash
     sleep 1
     python3 -c 'import json,glob,os; d=json.load(open(glob.glob(os.path.expanduser("~/.conduct/runs/ss-*/run.json"))[0])); print("stored_status=", d["status"])'
     "$CONDUCT" run list | grep -E 'ss-'
     "$CONDUCT" run show "$RID"
     ```
- **预期**：
  - 步骤 1：`RID=ss-<时间戳>`（时间后缀忽略）。
  - 步骤 2：`run list` 有一行 `ss-...` 且 `STATUS` 列为 `running`；`run stop` 退出码 `0`，stdout 形如 `已向运行 ss-…（pid <n>）发送终止信号 SIGTERM。`（pid 值忽略）。
  - 步骤 3：`stored_status= running`（run.json 存储态未改，符合「不落新状态」）；但 `run list` 该行 `STATUS` 现派生为 `interrupted`；`run show` 打印状态摘要（`运行 ss-… · interrupted`、`需求：慢慢来`、`步数 1 · 进度 step 0/1 · … 起`）并附一行 `运行总结尚未生成（运行未收尾）；用 conduct run show ss-… --trace 查看已执行步骤。`。
  - 归一化说明：本用例含时序，依赖 `run stop` 在 `sleep 30` 窗口内命中 running（`sleep 2` 已足够 run.json 落盘）；窗口足够宽松，通常稳定。pid 值、run id 时间后缀忽略。
- **清理**（务必清掉遗留的假 sleep 子进程与后台 conduct）：
  ```bash
  kill "$RUNPID" 2>/dev/null; wait "$RUNPID" 2>/dev/null
  pkill -f "$WORK/slowbin/claude" 2>/dev/null
  export HOME="$OLD_HOME"; rm -rf "$WORK"
  ```

---

## 补充：图片输入的 help 文案（零 token，只读）

conduct **不提供图片旗标、也不做 URL 下载**：给引擎看图片的方式是把图片的**本地绝对路径**直接写进需求文本，各引擎自带的文件工具自行读取。此约定须在 `workflow run --help` 里向用户交代清楚。对应 spec：[docs/specs/engines.md](../specs/engines.md)〈图片输入〉、[cli-runtime.md](../specs/cli-runtime.md)〈workflow run〉。

### TC-022 workflow run --help 说明「把图片本地绝对路径写进需求文本」

- **目的**：验证 `workflow run --help` 的说明文案覆盖图片输入的三个要点——① 把图片**本地绝对路径**写进需求文本；② conduct **不提供图片旗标**；③ **不做 URL 下载**。防止「怎么给引擎传图」这一常见疑问在 help 里失载，也防未来误加图片旗标 / URL 下载而与本约定漂移。
- **前置**：无（只读，`--help` 不触碰 store、不调引擎、零 token）。
- **步骤**：
  1. `"$CONDUCT" workflow run --help; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout（help 文本）同时含以下关键子串（用 `grep -q` 逐条校验，不比对整段排版）：
    - `本地绝对路径`（写进需求文本的方式）；
    - `不提供图片旗标`（conduct 无 `--image` 之类的旗标）；
    - `不做 URL 下载`（不替用户抓取网络图片）。
  - 一条命令校验：
    ```bash
    "$CONDUCT" workflow run --help | grep -q 本地绝对路径 \
      && "$CONDUCT" workflow run --help | grep -q 不提供图片旗标 \
      && "$CONDUCT" workflow run --help | grep -q '不做 URL 下载' \
      && echo "help_image_ok=yes" || echo "help_image_ok=no"
    ```
    应打印 `help_image_ok=yes`。
- **清理**：无。
