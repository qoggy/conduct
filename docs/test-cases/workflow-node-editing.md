# workflow copy / node 子族 测试用例

覆盖 conduct 中**局部编辑与派生**的一族命令：`workflow copy`（造变体）与 `workflow node set / set-prompt / show`（节点字段级编辑、提示词编辑、单节点查询 / 导出），外加 `create` / `edit` 的 `--help` 定义结构文案与 `workflow` / `node` 的未知子命令错误文案。整份工作流的增删改查见 [workflow-editing.md](./workflow-editing.md)，运行工作流见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/cli-authoring.md](../specs/cli-authoring.md)〈workflow copy〉〈workflow node set / set-prompt / show〉。

> **本文对应的领先项账单**：这批命令与文案的立项依据见 [docs/20260706/conduct-spec-领先实现清单.md](../../../docs/20260706/conduct-spec-领先实现清单.md)（领先项 1–5 与连带改动）。截至编写时，5 项**均已实现**，本文预期可直接对照验证。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**，用来验证实现对不对；命令若偏离本文〈预期〉即为实现未达标。

> **本文全部用例零 token**：只做复制 / 局部编辑 / 查询 / 校验，均不调用 AI 引擎。

> **隔离机制（关键，与 [workflow-editing.md](./workflow-editing.md) 一致）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。为不污染真实家目录，**每个用例把 `HOME` 重定向到临时目录**（`export HOME="$WORK"`），store 随之落在 `$WORK/.conduct/`，用例结束连同临时目录一并删除。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
REAL_HOME="$HOME"            # 一次性记下真实家目录，供中途失败时找回（见下注）
```

各用例〈前置〉统一用这段建立隔离环境（下文简称「建隔离环境」）：

```bash
WORK=$(mktemp -d)
OLD_HOME="$HOME"; export HOME="$WORK"   # store 落到 $WORK/.conduct，隔离全局
```

> **弃跑保护**：`OLD_HOME` 在每个用例〈前置〉里现取现存，正常顺序执行无碍。若某用例中途失败、未跑〈清理〉就直接开下一个用例的〈前置〉，`OLD_HOME` 会把上一个临时 HOME 存进去、恢复即错——此时改用 `export HOME="$REAL_HOME"` 找回真实家目录。

对应〈清理〉统一为：

```bash
export HOME="$OLD_HOME"; rm -rf "$WORK"
```

多个用例复用的**双节点定义**（`gen`→`review`，review 引用 gen 产物）。凡〈前置〉写「建库 flow」即指建隔离环境后跑下面这段：

```bash
cat > "$WORK/base.json" <<'JSON'
{
  "nodes": [
    {"id":"gen","displayName":"生成","engine":"claude-code","promptTemplate":"生成：{{sys.userPrompt}}"},
    {"id":"review","displayName":"评审","engine":"claude-code","promptTemplate":"评审 {{gen}}"}
  ]
}
JSON
cat "$WORK/base.json" | "$CONDUCT" workflow create flow --definition
```

---

## 功能覆盖清单（动笔前规划）

对着 spec 的行为空间逐项映射到用例，避免只测 happy path：

- **copy**：正常派生 / 深拷 evaluator·loopCount 且与源独立（数据流转 + 隔离）/ dst 已存在拒绝 / src 不存在 / dst 名非法 / runs 不动。
- **node set**：挂 evaluator（补默认提示词 + loopCount=1）/ `--evaluator` 作用域切换到评测官 / `--display-name` 恒节点级 / 空串清除标量 / 挂 redo（loopCount=1）/ 成功改 loop-count / 两循环互斥 / `--no-*` 成功拆除并回落单次 / engine 级联拒绝并可同命令修好 / `--effort`（claude-code）与 `--reasoning-effort`（qoder）对称 / loop-count 施于无循环节点拒绝 / 整份校验兜底（redoTarget 前向性）/ flag 组合四类用法错误 / 目标节点不存在 / 名非法 / 六种成功文案（挂 / 拆 evaluator·redo、更新节点、更新评测官）逐一断言（数据流转：文案随 outcome 变）。
- **node set-prompt**：原始文本免转义写入（含 `{{变量}}`·中文·多行）/ round-trip 字节稳定 / `--evaluator` 写评测官提示词 / 空输入拒绝且原文不变 / stdin 是终端拒绝不挂起。名非法退 2 与 `node show` 共用 `ValidateName` 机制，交同族 TC-005 / TC-015 覆盖。
- **node show**：三态（人类可读 / `--prompt` / `--json`）/ `--evaluator` / `--prompt` 与 `--json` 互斥 / 与 set-prompt 的 round-trip 配对。
- **文案**：`create` / `edit` 的 `--help` 补 evaluator 的 `engineConfig`、去自引用反模式；`workflow` / `node` 未知子命令列出全部动词。

深藏内部的展开 / 求值精确性交单测（`internal/workflow`）；本文只验对外可见的退出码 / 输出 / 产物。

---

## workflow copy — 造变体

### TC-001 copy 从既有工作流派生新变体

- **目的**：验证 `workflow copy <src> <dst>` 复制定义主体、生成全新托管对象，且 `<src>` 不受影响。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow copy flow flow-heavy; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `"$CONDUCT" workflow show flow-heavy --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print("name=",d["name"]);print("ids=",[n["id"] for n in d["nodes"]]);print("has_ts=", bool(d.get("createdAt")) and bool(d.get("updatedAt")))'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已复制 flow → flow-heavy`。
  - 步骤 2 列表同时含 `flow` 与 `flow-heavy`。
  - 步骤 3 打印 `name= flow-heavy`、`ids= ['gen', 'review']`（nodes 主体已复制）、`has_ts= True`（`createdAt`/`updatedAt` 由 store 重戳，非空）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-002 copy 深拷 evaluator / loopCount，dst 与 src 互不串改

- **目的**：验证复制是**深拷**——含指针字段（`evaluator` / `engineConfig` / `loopCount`）的节点被逐份新建，事后改 `<dst>` 不影响 `<src>`（数据流转 + 隔离，退化夹具测不出这条，故用带 evaluator 的节点）。
- **前置**：
  1. 建隔离环境。
  2. 造带 evaluator（`loopCount:3`、评测官配 qoder + reasoningEffort）的定义并入库：
     ```bash
     cat > "$WORK/rich.json" <<'JSON'
     {"nodes":[{"id":"g","displayName":"生","engine":"claude-code","promptTemplate":"p","loopCount":3,"evaluator":{"engine":"qoder","engineConfig":{"reasoningEffort":"high"},"promptTemplate":"评"}}]}
     JSON
     cat "$WORK/rich.json" | "$CONDUCT" workflow create src --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow copy src dst; echo "exit=$?"`
  2. `"$CONDUCT" workflow show dst --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];print("loopCount=",n["loopCount"],"evalEngine=",n["evaluator"]["engine"],"re=",n["evaluator"]["engineConfig"]["reasoningEffort"])'`
  3. 改 `dst` 的节点显示名，再看 `src` 是否被牵连：
     ```bash
     "$CONDUCT" workflow node set dst g --display-name 改了 >/dev/null
     "$CONDUCT" workflow node show src g | head -1
     ```
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已复制 src → dst`。
  - 步骤 2 打印 `loopCount= 3 evalEngine= qoder re= high`（指针字段全部深拷到位）。
  - 步骤 3 输出以 `g · 生 ·` 开头（`src` 的显示名仍是 `生`，未被改 `dst` 牵连——两者不共享底层指针）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-003 copy 目标已存在时拒绝、不覆盖

- **目的**：验证 `<dst>` 已存在时失败、不写坏原文件（与 create / rename 同族的不覆盖语义）。
- **前置**：建隔离环境；建库 flow；`cp "$WORK/.conduct/workflows/flow.json" "$WORK/before.json"`（留基线）。
- **步骤**：
  1. `"$CONDUCT" workflow copy flow flow; echo "exit=$?"`
  2. `diff "$WORK/before.json" "$WORK/.conduct/workflows/flow.json"; echo "diff=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `工作流 flow 已存在`。
  - 步骤 2 `diff=0`（原 `flow` 逐字节未变）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-004 copy 源不存在时报错

- **目的**：验证 `<src>` 不存在时失败、不产生 `<dst>`。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow copy nope dst; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `工作流 nope 不存在`。
  - 步骤 2 列表为空（`dst` 未被创建）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-005 copy 目标名非法时报用法错误

- **目的**：验证 `<dst>` 不匹配 `[A-Za-z0-9._-]+` 时退出 `2`（用法错误），`<src>` 不动。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow copy flow 'bad/name'; echo "exit=$?"`
  2. `test -f "$WORK/.conduct/workflows/flow.json"; echo "src_kept=$?"`
- **预期**：
  - 步骤 1 退出码 `2`，stderr 含 `工作流名 "bad/name" 非法`（提示只允许字母、数字、点、下划线、连字符）。
  - 步骤 2 打印 `src_kept=0`（`flow` 仍在）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node set — 局部编辑（结构化字段）

### TC-006 node set 挂载评测循环（补默认提示词 + loopCount 默认 1）

- **目的**：验证 `--evaluator --engine <e>` 在节点原无 evaluator 时首次挂载：自动补一份**自包含默认评测提示词**（纯文本、不自引用）与 `loopCount:1`（数据流转：断言默认提示词正文与 loopCount 真的落进定义）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --evaluator --engine claude-code; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];print("loopCount=",n["loopCount"]);print("prompt=",n["evaluator"]["promptTemplate"])'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已为 flow·gen 挂载评测循环`。
  - 步骤 2 打印 `loopCount= 1`，且 `prompt=` 后为 `你是独立质量评测官。审阅下面待评产物的正确性、完整性与质量，给出具体、可执行的改进反馈。`（默认提示词字面量，无 `{{gen}}` 自引用）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-007 --evaluator 把引擎类字段切到评测官，--display-name 恒节点级

- **目的**：验证 `--evaluator` 是**作用域修饰**——`--model` 落到评测官的 `engineConfig`、不碰节点主体；而 `--display-name` **不受 `--evaluator` 影响**（恒节点级，评测官无独立显示名），且此时成功文案归为普通「已更新」而非「评测官」。这是关键的特性叠加路径。
- **前置**：建隔离环境；建库 flow；先挂评测官：`"$CONDUCT" workflow node set flow gen --evaluator --engine claude-code`。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --evaluator --model claude-sonnet-5; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];print("node.engineConfig=",n.get("engineConfig"));print("eval.engineConfig=",n["evaluator"].get("engineConfig"))'`
  3. `"$CONDUCT" workflow node set flow gen --evaluator --display-name 新显示名; echo "exit=$?"`
  4. `"$CONDUCT" workflow node show flow gen | head -1`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen 的评测官`（引擎类字段落到既有评测官 → 文案为「评测官」）。
  - 步骤 2 打印 `node.engineConfig= None` 与 `eval.engineConfig= {'model': 'claude-sonnet-5'}`（model 只进评测官，节点主体分毫未动）。
  - 步骤 3 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`（**不**含「的评测官」——displayName 恒节点级、评测官未动，归普通更新）。
  - 步骤 4 输出以 `gen · 新显示名 · claude-code ·` 开头（节点显示名已改）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 空串清除调优标量（回落引擎默认）

- **目的**：验证 `--model ""` 清除该字段（回落引擎默认），三字段全空后 `engineConfig` 规范化为不出现（而非留空对象）。
- **前置**：建隔离环境；建库 flow；先设模型：`"$CONDUCT" workflow node set flow gen --model claude-sonnet-5`。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --model ""; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];print("has_engineConfig=", "engineConfig" in n)'`
  3. `"$CONDUCT" workflow node show flow gen | head -1`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`。
  - 步骤 2 打印 `has_engineConfig= False`（model 清除后 engineConfig 整体消失）。
  - 步骤 3 输出含 `(引擎默认)`（model 已回落默认）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-009 回跳的完整生命周期：挂载（loopCount 默认 1）/ 改次数 / 互斥 / 拆除

- **目的**：串起 redoTarget 回跳的一条完整生命周期——首次挂载补 `loopCount:1`；成功改循环次数（`node set` 的头号卖点：不复述整份 JSON 即调 `loopCount`）；与 evaluator 互斥（已配回跳的节点挂 evaluator 被拒）；`--no-redo` 成功拆除并回落单次。逐步断言各自的成功文案与产物。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. 挂回跳：`"$CONDUCT" workflow node set flow review --redo-target gen; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][1];print("redoTarget=",n.get("redoTarget"),"loopCount=",n.get("loopCount"))'`
  3. 只改循环次数：`"$CONDUCT" workflow node set flow review --loop-count 5; echo "exit=$?"`
  4. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;print("loopCount=",json.load(sys.stdin)["nodes"][1].get("loopCount"))'`
  5. 给已有回跳的节点再挂 evaluator（互斥）：`"$CONDUCT" workflow node set flow review --evaluator --engine claude-code; echo "exit=$?"`
  6. 拆回跳：`"$CONDUCT" workflow node set flow review --no-redo; echo "exit=$?"`
  7. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][1];print("has_redo=", bool(n.get("redoTarget")), "has_loop=", "loopCount" in n)'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已为 flow·review 挂载回跳→gen`。
  - 步骤 2 打印 `redoTarget= gen loopCount= 1`（首次挂载补默认次数）。
  - 步骤 3 退出码 `0`，stdout 含 `✓ 已更新 flow·review`（只改次数、不动回跳目标）。
  - 步骤 4 打印 `loopCount= 5`（次数已改，无需复述整份定义）。
  - 步骤 5 退出码 `1`，stderr 提示二者互斥、须先拆回跳（含 `互斥` 与 `--no-redo`）。
  - 步骤 6 退出码 `0`，stdout 含 `✓ 已拆除 flow·review 的回跳`。
  - 步骤 7 打印 `has_redo= False has_loop= False`（redoTarget 删除、loopCount 回落单次即清除）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 拆除两种循环（--no-evaluator / --no-redo，回落单次）

- **目的**：验证 `--no-evaluator` / `--no-redo` 拆除对应循环、`loopCount` 回落单次；拆一个本不存在的循环则退 `1`。
- **前置**：建隔离环境；建库 flow；给 `gen` 挂评测官：`"$CONDUCT" workflow node set flow gen --evaluator --engine claude-code`。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --no-evaluator; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];print("has_eval=", "evaluator" in n, "has_loop=", "loopCount" in n)'`
  3. 再拆一次（此时已无评测循环）：`"$CONDUCT" workflow node set flow gen --no-evaluator; echo "exit=$?"`
  4. 拆一个从未挂过的回跳：`"$CONDUCT" workflow node set flow gen --no-redo; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已拆除 flow·gen 的评测循环`。
  - 步骤 2 打印 `has_eval= False has_loop= False`（evaluator 删除、loopCount 回落单次即清除）。
  - 步骤 3 退出码 `1`，stderr 含 `节点 gen 无评测循环，无可拆除`。
  - 步骤 4 退出码 `1`，stderr 含 `节点 gen 无回跳，无可拆除`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 --engine 改引擎的级联：不兼容整份拒绝，可同命令一并重设

- **目的**：验证改 engine 后旧引擎专属字段被新引擎拒收时**整份校验失败退 1、绝不静默丢弃**；且允许在**同一条命令**里清掉旧字段修好。
- **前置**：建隔离环境；建库 flow；先给 `gen` 配 claude-code 专属 effort：`"$CONDUCT" workflow node set flow gen --effort high`。
- **步骤**：
  1. 只换引擎、不清旧字段（应被拒）：`"$CONDUCT" workflow node set flow gen --engine qoder; echo "exit=$?"`
  2. 同命令换引擎 + 清旧 effort（应成功）：`"$CONDUCT" workflow node set flow gen --engine qoder --effort ""; echo "exit=$?"`
  3. `"$CONDUCT" workflow node show flow gen | head -1`
  4. 换引擎后设 qoder 专属档位 `--reasoning-effort`（与 claude-code 的 `--effort` 对称）：`"$CONDUCT" workflow node set flow gen --reasoning-effort medium; echo "exit=$?"`
  5. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;print("engineConfig=",json.load(sys.stdin)["nodes"][0].get("engineConfig"))'`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `engineConfig.effort: engine="qoder" 不认 effort`（旧 effort 未被静默丢弃、整份校验拦下）；原定义不变（仍是 claude-code）。
  - 步骤 2 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`。
  - 步骤 3 输出以 `gen · 生成 · qoder ·` 开头（引擎已切到 qoder、旧 effort 已清）。
  - 步骤 4 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`（qoder 接受 `reasoningEffort`）。
  - 步骤 5 打印 `engineConfig= {'reasoningEffort': 'medium'}`（qoder 专属档位字段落位，与 claude-code 的 `effort` 对称）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-012 --loop-count 施于无循环节点被拒（命令级）

- **目的**：验证给一个既无 evaluator 也无 redoTarget 的节点设 `--loop-count` 时命令级退 `1`（`loopCount` 无从设置）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --loop-count 3; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `节点 gen 无评测循环 / 回跳，loopCount 无从设置`（提示先挂评测官或回跳）；`gen` 不变。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-013 内存改完复用整份校验：redoTarget 前向性兜底

- **目的**：验证 `node set` 在落盘前复用整份定义校验——`--redo-target` 指向**其后**的节点时被整份校验拦下退 `1`（命令级只查目标非空 / 互斥，前向性交 `Validate` 兜底）。
- **前置**：建隔离环境；建库 flow（`gen` 在前、`review` 在后）。
- **步骤**：
  1. 让在前的 `gen` 回跳到在后的 `review`（前向性违规）：`"$CONDUCT" workflow node set flow gen --redo-target review; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `nodes[0].redoTarget: 必须指向本节点之前的节点，"review" 在其后或即本身`；`flow` 不变。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-014 node set 的 flag 组合四类用法错误（退出 2）

- **目的**：验证 `checkNodeSetFlagCombo` 的四类用法错误各退 `2`（不落盘、不改动）：无任何字段 / 拆除选项；`--redo-target ""`；两个 `--no-*` 同给；`--no-*` 与字段选项同用。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. 无任何操作旗标：`"$CONDUCT" workflow node set flow gen; echo "exit=$?"`
  2. 空串拆回跳（应改用 --no-redo）：`"$CONDUCT" workflow node set flow gen --redo-target ""; echo "exit=$?"`
  3. 两个拆除同给：`"$CONDUCT" workflow node set flow gen --no-evaluator --no-redo; echo "exit=$?"`
  4. 拆除与字段同用：`"$CONDUCT" workflow node set flow gen --no-redo --model x; echo "exit=$?"`
- **预期**（四步退出码均为 `2`）：
  - 步骤 1 stderr 含 `至少给一个字段选项或拆除选项`。
  - 步骤 2 stderr 含 `--redo-target 不接受空串；拆除回跳请用 --no-redo`。
  - 步骤 3 stderr 含 `--no-evaluator 与 --no-redo 不能同用`。
  - 步骤 4 stderr 含 `--no-evaluator / --no-redo 须单独使用`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-015 node set 字段级错误：display-name 空、目标节点不存在、名非法

- **目的**：验证三类各归其位的错误码：`--display-name ""` 字段级退 `1`；节点 id 不存在退 `1`；工作流名非法退 `2`（用法错误）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --display-name ""; echo "exit=$?"`
  2. `"$CONDUCT" workflow node set flow ghost --model m; echo "exit=$?"`
  3. `"$CONDUCT" workflow node set 'bad/name' gen --model m; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `节点 gen 的 displayName 不能为空`。
  - 步骤 2 退出码 `1`，stderr 含 `工作流 flow 无节点 ghost`。
  - 步骤 3 退出码 `2`，stderr 含 `工作流名 "bad/name" 非法`（名校验先于 store 载入，走用法错误）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node set-prompt — 局部编辑（提示词）

### TC-016 set-prompt 原始文本免转义写入（含 {{变量}} / 中文 / 多行）

- **目的**：验证提示词正文以原始文本从 stdin 整段读入、由 conduct 负责 JSON 编码——作者无需手工转义（数据流转：断言含 `{{sys.userPrompt}}`、中文、多行的正文原样落进定义）。
- **前置**：
  1. 建隔离环境；建库 flow。
  2. 造多行含变量的提示词文件：
     ```bash
     cat > "$WORK/p.md" <<'MD'
     写一首诗，主题来自用户需求：
     {{sys.userPrompt}}
     要求：押韵、不超过 8 行
     MD
     ```
- **步骤**：
  1. `cat "$WORK/p.md" | "$CONDUCT" workflow node set-prompt flow gen; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["nodes"][0];p=n["promptTemplate"];print("has_var=", "{{sys.userPrompt}}" in p);print("lines=", len(p.splitlines()))'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen 提示词`。
  - 步骤 2 打印 `has_var= True` 与 `lines= 3`（三行正文原样入库；末尾恰好一个换行已剥掉，故 3 行而非 4）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-017 set-prompt ↔ show --prompt round-trip 字节稳定

- **目的**：验证 `show --prompt`（补恰好一个尾换行）与 `set-prompt`（剥恰好一个尾换行）配对，使「导出 → 写回 → 再导出」**逐字节稳定**、不会静默给提示词尾部累加 `\n`。这是一对必须成对验证的功能。
- **前置**：
  1. 建隔离环境。
  2. 造含多行、含变量、含中文的提示词入库：
     ```bash
     cat > "$WORK/def.json" <<'JSON'
     {"nodes":[{"id":"gen","displayName":"生","engine":"claude-code","promptTemplate":"多行\n带 {{sys.userPrompt}}\n结尾"}]}
     JSON
     cat "$WORK/def.json" | "$CONDUCT" workflow create f --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow node show f gen --prompt > "$WORK/a.md"`
  2. `cat "$WORK/a.md" | "$CONDUCT" workflow node set-prompt f gen`
  3. `"$CONDUCT" workflow node show f gen --prompt > "$WORK/b.md"`
  4. `diff "$WORK/a.md" "$WORK/b.md"; echo "roundtrip_diff=$?"`
  5. `tail -c 1 "$WORK/a.md" | xxd | grep -q '0a' && echo "ends_with_single_newline=yes"`
- **预期**：
  - 步骤 2 stdout 含 `✓ 已更新 f·gen 提示词`。
  - 步骤 4 打印 `roundtrip_diff=0`（导出→写回→再导出后逐字节不变）。
  - 步骤 5 打印 `ends_with_single_newline=yes`（`--prompt` 输出以恰好一个 `\n` 收尾）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-018 set-prompt --evaluator 设评测官提示词；无评测官则报错

- **目的**：验证 `--evaluator` 把提示词写到该节点评测官的 `promptTemplate`；节点无评测循环时退 `1`。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. 节点无评测官时用 `--evaluator`（应拒）：`printf '审阅 {{gen}}' | "$CONDUCT" workflow node set-prompt flow gen --evaluator; echo "exit=$?"`
  2. 先挂评测官：`"$CONDUCT" workflow node set flow gen --evaluator --engine claude-code`
  3. 再设评测官提示词：`printf '审阅并给出改进意见：应关注 {{gen}}\n' | "$CONDUCT" workflow node set-prompt flow gen --evaluator; echo "exit=$?"`
  4. `"$CONDUCT" workflow node show flow gen --evaluator --prompt`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `节点 gen 无评测循环，无评测官提示词可设`。
  - 步骤 3 退出码 `0`，stdout 含 `✓ 已更新 flow·gen 评测官提示词`。
  - 步骤 4 输出 `审阅并给出改进意见：应关注 {{gen}}`（评测官提示词已被改写，覆盖了默认提示词）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-019 set-prompt 空输入 / 非法模板被拒，原文件不变

- **目的**：验证空输入（或模板变量引用非法）落盘前被整份校验拦下退 `1`、**原提示词不变**。
- **前置**：建隔离环境；建库 flow；`cp "$WORK/.conduct/workflows/flow.json" "$WORK/before.json"`（留基线）。
- **步骤**：
  1. 空输入：`printf '' | "$CONDUCT" workflow node set-prompt flow gen; echo "exit=$?"`
  2. 引用幽灵节点：`printf '{{ghost}}' | "$CONDUCT" workflow node set-prompt flow gen; echo "exit=$?"`
  3. `diff "$WORK/before.json" "$WORK/.conduct/workflows/flow.json"; echo "diff=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].promptTemplate: 必填`（剥掉尾换行后为空，校验拒绝）。
  - 步骤 2 退出码 `1`，stderr 含 `引用不存在的节点 {{ghost}}`。
  - 步骤 3 `diff=0`（两次失败后 `flow.json` 逐字节不变）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-020 set-prompt stdin 是终端时报错、不挂起

- **目的**：验证 stdin 是**真终端**（无管道输入）时立即报用法错误退 `2`、**不挂起等待输入**。用 pty 伪终端把 stdin 接成 tty，可无人值守自动化。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. 用伪终端驱动、超时守卫验证其立即失败返回而非挂起：
     ```bash
     python3 - "$CONDUCT" <<'PY'
     import os, pty, subprocess, sys
     conduct = sys.argv[1]
     master, slave = pty.openpty()            # slave 端 os.isatty()=True
     p = subprocess.Popen([conduct, "workflow", "node", "set-prompt", "flow", "gen"],
                          stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
     os.close(slave)
     try:
         out, err = p.communicate(timeout=5)  # 超时守卫：停在等待输入即为 bug
         print(f"exit={p.returncode}"); print("stderr:", err.strip())
     except subprocess.TimeoutExpired:
         p.kill(); print("exit=HANG(FAIL)")
     os.close(master)
     PY
     ```
     （沿用〈前置〉里 `export HOME="$WORK"`，python 子进程继承同一隔离 store。）
- **预期**：
  - 脚本立即返回（耗时 < 1s，非 `HANG(FAIL)`），打印 `exit=2`，stderr 含 `缺少提示词` 与 `cat prompt.md | conduct workflow node set-prompt`。
  - 说明：这验的是「stdin 是真终端」分支——用 pty 即可在无人值守自动化里复现，不必真人守终端。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node show — 查询 / 导出（单节点）

### TC-021 node show 默认输出人类可读单节点详情

- **目的**：验证无旗标时输出 `id · displayName · engine · model · <循环模式>` 一行摘要 + 空行 + 提示词全文（不截断）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - 首行为 `gen · 生成 · claude-code · (引擎默认) · 单次`（model 未设显示 `(引擎默认)`、无循环显示 `单次`）。
  - 随后空行，再输出提示词全文 `生成：{{sys.userPrompt}}`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-022 node show --json 输出规范化单节点对象

- **目的**：验证 `--json` 输出规范化后的**单个** node 对象（非工作流整体、非数组）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print(type(d).__name__, d["id"], d["engine"], "{{sys.userPrompt}}" in d["promptTemplate"])'`
- **预期**：
  - stdout 打印 `dict gen claude-code True`（是单个对象、含 `id`/`engine`/`promptTemplate`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-023 node show --evaluator 查评测官；无评测官则报错

- **目的**：验证 `--evaluator` 作用于该节点评测官（人类可读为 `<id>·evaluator · engine · model`，无 displayName / 循环模式）；节点无评测官时退 `1`。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. 节点无评测官时（应拒）：`"$CONDUCT" workflow node show flow gen --evaluator; echo "exit=$?"`
  2. 挂评测官并配模型：`"$CONDUCT" workflow node set flow gen --evaluator --engine claude-code --model claude-sonnet-5`
  3. 再查：`"$CONDUCT" workflow node show flow gen --evaluator | head -1`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `节点 gen 无评测循环，无评测官可查`。
  - 步骤 3 首行为 `gen·evaluator · claude-code · claude-sonnet-5`（评测官视图：无 displayName、无循环模式）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-024 node show --prompt 与 --json 互斥

- **目的**：验证 `--prompt`（纯文本）与 `--json`（结构化对象）同给时报用法错误退 `2`。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen --prompt --json; echo "exit=$?"`
- **预期**：
  - 退出码 `2`，stderr 含 `--prompt 与 --json 互斥`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## create / edit 的 --help 定义结构 + 未知子命令文案

### TC-025 create / edit 的 --help 补齐 evaluator 的 engineConfig，且无自引用反模式

- **目的**：验证 `create` / `edit` 的 `--help` 里内嵌的定义结构说明：evaluator 段**演示了 `engineConfig`**（对齐 `node set --evaluator --model/--effort` 的作用对象），且**不含被禁的自引用反模式**（evaluator 提示词不写 `{{gen}}` 之类自引用——按 `conduct help prompts`，evaluator 输入末尾已由系统追加被评产物）。
- **前置**：无（只读，仅看 `--help`）。
- **步骤**：
  1. `"$CONDUCT" workflow create --help 2>&1 | grep -c 'engineConfig'`
  2. `"$CONDUCT" workflow create --help 2>&1 | grep -c '{{gen}}'`
  3. `"$CONDUCT" workflow edit --help 2>&1 | grep -c 'engineConfig'`
- **预期**：
  - 步骤 1 打印 `≥ 1`（实测 `3`）——`--help` 的定义示例含 `engineConfig`，且 evaluator 段亦演示同构支持 `engineConfig`。
  - 步骤 2 打印 `0`——示例里 evaluator 提示词不再自引用 `{{gen}}`（已改成自包含的「审阅下面待评产物」式文案）。
  - 步骤 3 打印 `≥ 1`（`edit --help` 复用同一份定义结构说明）。
- **清理**：无。

### TC-026 workflow / node 未知子命令列出全部可用动词

- **目的**：验证拼错子命令时 fail-loud 退 `2`，且错误文案列出的可用动词**含新增的 `copy` / `node`**（workflow 层）与 `set / set-prompt / show`（node 层）——命令面不自相矛盾。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" workflow bogus 2>&1; echo "exit=$?"`
  2. `"$CONDUCT" workflow node bogus 2>&1; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `2`，stderr 含 `未知子命令 "bogus"（可用：create / copy / edit / node / rename / delete / list / show / run）`。
  - 步骤 2 退出码 `2`，stderr 含 `未知子命令 "bogus"（可用：set / set-prompt / show）`。
- **清理**：无。
