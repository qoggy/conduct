# workflow 局部编辑 测试用例

覆盖 conduct 中**局部编辑与派生**的一族命令：`workflow copy`（造变体）、`workflow node add / rm / set / set-prompt / show`（节点级增删改查）、`workflow edge`（连边，无 --add/--rm 即列出）。整份工作流的生命周期（create/edit/rename/delete/list/show）与整份 DAG 落盘校验规则见 [workflow-editing.md](./workflow-editing.md)；运行工作流见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/cli-authoring.md](../specs/cli-authoring.md)〈workflow copy〉〈workflow node add/rm/set/set-prompt/show〉〈workflow edge〉。

> **模型基线**：工作流定义是节点 + 边的 DAG，含两个保留标记节点 `START`/`END`。本文用例都基于**已存在**的工作流做局部改动——不重发整份定义，验证「只改一处」的编辑算符。

> **本文全部用例零 token**：只做复制 / 局部编辑 / 查询 / 校验，均不调用 AI 引擎。

> **隔离机制（关键，与 [workflow-editing.md](./workflow-editing.md) 一致）**：每个用例把 `HOME` 重定向到临时目录（`export HOME="$WORK"`），store 落在 `$WORK/.conduct/`，用例结束连同临时目录一并删除。

## 环境准备（每篇跑一次）

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
REAL_HOME="$HOME"            # 一次性记下真实家目录，供中途失败时找回
```

各用例〈前置〉统一用这段建立隔离环境（下文简称「建隔离环境」）：

```bash
WORK=$(mktemp -d)
OLD_HOME="$HOME"; export HOME="$WORK"   # store 落到 $WORK/.conduct，隔离全局
```

> **弃跑保护**：若某用例中途失败、未跑〈清理〉就直接开下一个用例的〈前置〉，`OLD_HOME` 会把上一个临时 HOME 存进去、恢复即错——此时改用 `export HOME="$REAL_HOME"` 找回真实家目录。

对应〈清理〉统一为：

```bash
export HOME="$OLD_HOME"; rm -rf "$WORK"
```

多个用例复用的**双节点定义**（`START → gen → review → END`，`review` 引用 `gen` 产物）。凡〈前置〉写「建库 flow」即指建隔离环境后跑下面这段：

```bash
cat > "$WORK/base.json" <<'JSON'
{
  "nodes": [
    { "id": "START" },
    {"id":"gen","displayName":"生成","engine":"claude-code","promptTemplate":"生成：{{sys.userPrompt}}"},
    {"id":"review","displayName":"评审","engine":"claude-code","promptTemplate":"评审 {{gen}}"},
    { "id": "END" }
  ],
  "edges": [
    {"from":"START","to":"gen"},{"from":"gen","to":"review"},{"from":"review","to":"END"}
  ]
}
JSON
cat "$WORK/base.json" | "$CONDUCT" workflow create flow --definition
```

---

## 功能覆盖清单（动笔前规划）

- **copy**：正常派生 / 深拷 engineConfig 且与源独立（数据流转 + 隔离）/ dst 已存在拒绝 / src 不存在 / dst 名非法 / runs 不动。
- **node add**：缺省自动接 `START→id→END` / 连加多个得并行扇出 / `--from`/`--to` 显式指定 / `--model`/`--reasoning-effort` 等调优选项 / id 为保留名拒绝 / id 已存在拒绝 / id 格式非法 / 缺 `--engine`/`--display-name` 用法错误 / `--from` 指向不存在节点 / 目标工作流不存在。
- **node rm**：级联删边 / 结果孤立时拒绝并保留原文件 / `START`/`END` 拒删。
- **edge**：`--add`/`--rm` 原子批量、给一对即单改 / `--add` 已存在的边报错 / `--rm` 不存在的边报错 / 同边同时 `--add`+`--rm` 视为保留 / reorder 三步配方（`set-prompt` → `edge` 批量 → `set-prompt`）。
- **edge（列出）**：无 `--add`/`--rm` 时列出边，人类可读 / `--json`。
- **node set**：`--engine`/`--model`/`--effort`/`--reasoning-effort`/`--display-name` 逐项生效 / 空串清除标量 / `--engine` 级联不兼容拒绝、同命令一并修好 / 未给任何字段用法错误 / `--display-name` 空拒绝 / 目标节点不存在 / 工作流名非法。
- **node set-prompt**：原始文本免转义写入 / round-trip 字节稳定 / 空输入拒绝 / stdin 是终端拒绝不挂起。
- **node show**：三态（人类可读 / `--prompt` / `--json`）/ `--prompt` 与 `--json` 互斥 / 与 set-prompt 的 round-trip 配对 / `START`/`END` 无可查看定义。
- **文案**：`create`/`edit` 的 `--help` 含 `engineConfig` 示例、不含 evaluator/redoTarget/loopCount 残留提及；`workflow`/`node` 未知子命令列出全部动词（含新命令 `copy`/`node`/`edge`/`add`/`rm`）。

---

## workflow copy — 造变体

### TC-001 copy 从既有工作流派生新变体

- **目的**：验证 `workflow copy <src> <dst>` 复制定义主体（含 START/END）、生成全新托管对象，且 `<src>` 不受影响。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow copy flow flow-heavy; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `"$CONDUCT" workflow show flow-heavy --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print("name=",d["name"]);print("ids=",[n["id"] for n in d["definition"]["nodes"]]);print("has_ts=", bool(d.get("createdAt")) and bool(d.get("updatedAt")))'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已复制 flow → flow-heavy`。
  - 步骤 2 列表同时含 `flow` 与 `flow-heavy`。
  - 步骤 3 打印 `name= flow-heavy`、`ids= ['START', 'gen', 'review', 'END']`（含 START/END 的定义主体已复制）、`has_ts= True`（`createdAt`/`updatedAt` 由 store 重戳，非空）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-002 copy 深拷 engineConfig，dst 与 src 互不串改

- **目的**：验证复制是**深拷**——含指针字段（`engineConfig`）的节点被逐份新建，事后改 `<dst>` 的节点不影响 `<src>`（数据流转 + 隔离）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow copy flow flow-heavy; echo "exit=$?"`
  2. 改 `flow-heavy` 的 `gen` 节点模型与显示名，再看 `flow` 是否被牵连：
     ```bash
     "$CONDUCT" workflow node set flow-heavy gen --model claude-sonnet-5 --display-name 改了 >/dev/null
     "$CONDUCT" workflow node show flow gen
     echo "---"
     "$CONDUCT" workflow node show flow-heavy gen
     ```
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2：`flow` 的 `gen` 仍是 `gen · 生成 · claude-code · (引擎默认)`（未被牵连）；`flow-heavy` 的 `gen` 已变为 `gen · 改了 · claude-code · claude-sonnet-5`——两者不共享底层 `engineConfig` 指针。
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
  - 步骤 1 退出码 `2`，stderr 含 `工作流名 "bad/name" 非法`。
  - 步骤 2 打印 `src_kept=0`（`flow` 仍在）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node add — 加节点

### TC-006 node add 缺省自动接 START→id→END，连加多个得并行扇出

- **目的**：验证不给 `--from`/`--to` 时自动接成 `START → <id> → END`；连加两个都不给 from/to，得到两个从 `START` 并行扇出的节点。
- **前置**：建隔离环境；`"$CONDUCT" workflow create f`（脚手架 `START→node-1→END`）。
- **步骤**：
  1. `"$CONDUCT" workflow node add f a --engine claude-code --display-name 调研; echo "exit=$?"`
  2. `"$CONDUCT" workflow node add f b --engine qoder --display-name 起草; echo "exit=$?"`
  3. `"$CONDUCT" workflow show f --expand`
- **预期**：
  - 步骤 1、2 均退出 `0`，stdout 分别含 `✓ 已加节点 f·a`、`✓ 已加节点 f·b`。
  - 步骤 3：`边：` 段含 `START → a`、`a → END`、`START → b`、`b → END`（`node-1` 原有边不变）；拓扑分层 `level 0: [node-1, a, b]`（三者都仅以 `START` 为前驱，同层并行）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-007 node add --from/--to 显式指定，构建菱形汇聚

- **目的**：验证 `--from`（逗号分隔多个前驱）、`--to`、以及 `--model`/`--reasoning-effort` 等调优选项在建节点时一并生效。
- **前置**：建隔离环境；建库 flow（`START→gen→review→END`）。
- **步骤**：
  1. `"$CONDUCT" workflow node add flow merged --engine qoder --display-name 合并 --reasoning-effort high --from gen,review --to END; echo "exit=$?"`
  2. `"$CONDUCT" workflow node show flow merged --json`
  3. `"$CONDUCT" workflow edge flow`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已加节点 flow·merged`。
  - 步骤 2 打印含 `"engine": "qoder"`、`"engineConfig": {"reasoningEffort": "high"}`。
  - 步骤 3 含 `gen → merged`、`review → merged`、`merged → END`（原 `review → END` 边仍在，因 `node add` 只加边、不动既有边——`review` 此时有两条出边）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 node add 各类拒绝：保留名 / 已存在 / id 非法 / 缺必填 / --from 指向不存在

- **目的**：验证 `node add` 的入参校验各自触发正确的退出码。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. id 为保留名：`"$CONDUCT" workflow node add flow START --engine claude-code --display-name x; echo "exit=$?"`
  2. id 已存在：`"$CONDUCT" workflow node add flow gen --engine claude-code --display-name dup; echo "exit=$?"`
  3. id 格式非法：`"$CONDUCT" workflow node add flow 1bad --engine claude-code --display-name x; echo "exit=$?"`
  4. 缺 `--engine`/`--display-name`：`"$CONDUCT" workflow node add flow c; echo "exit=$?"`
  5. `--from` 指向不存在节点：`"$CONDUCT" workflow node add flow c --engine claude-code --display-name x --from ghost; echo "exit=$?"`
  6. 目标工作流不存在：`"$CONDUCT" workflow node add nope c --engine claude-code --display-name x; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `节点 id 不得为保留名 START / END`。
  - 步骤 2 退出码 `1`，stderr 含 `节点 gen 已存在`。
  - 步骤 3 退出码 `2`，stderr 含 `节点 id "1bad" 非法`。
  - 步骤 4 退出码 `2`，stderr 含 `--engine 与 --display-name 必填`。
  - 步骤 5 退出码 `1`，stderr 含 `from 指向不存在的节点 "ghost"`。
  - 步骤 6 退出码 `1`，stderr 含 `工作流 nope 不存在`。
  - 全部用例均未改动 `flow.json`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node rm — 删节点

### TC-009 node rm 级联删边，结果合法即删除

- **目的**：验证 `node rm` 删除节点及其全部连边，结果校验通过即落盘。
- **前置**：建隔离环境；建库 flow；先加一个从 START 并行、汇入 review 的节点：`"$CONDUCT" workflow node add flow extra --engine claude-code --display-name 额外 --from START --to review >/dev/null`。
- **步骤**：
  1. `"$CONDUCT" workflow node rm flow extra; echo "exit=$?"`
  2. `"$CONDUCT" workflow edge flow`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已删节点 flow·extra`。
  - 步骤 2 不再出现任何含 `extra` 的边（级联删除）；`START → gen`、`gen → review`、`review → END` 仍在。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 node rm 结果孤立时拒绝、原文件不变

- **目的**：验证删除会使其它节点失去唯一入边或出边时，结果校验拦下、拒绝落盘。
- **前置**：建隔离环境；建库 flow（`START→gen→review→END`）；`cp "$WORK/.conduct/workflows/flow.json" "$WORK/before.json"`（留基线）。
- **步骤**：
  1. `"$CONDUCT" workflow node rm flow gen; echo "exit=$?"`（删 `gen` 会让 `review` 失去唯一入边）
  2. `diff "$WORK/before.json" "$WORK/.conduct/workflows/flow.json"; echo "diff=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `agent 节点 "review" 无入边`。
  - 步骤 2 `diff=0`（原文件逐字节不变）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 node rm 拒删 START/END，拒删导致零 agent 节点

- **目的**：验证保留节点不可删；删至零 agent 节点同样被拒。
- **前置**：建隔离环境；`"$CONDUCT" workflow create f`（脚手架，单 agent 节点 `node-1`）。
- **步骤**：
  1. `"$CONDUCT" workflow node rm f START; echo "exit=$?"`
  2. `"$CONDUCT" workflow node rm f END; echo "exit=$?"`
  3. `"$CONDUCT" workflow node rm f node-1; echo "exit=$?"`（唯一 agent 节点，删后零 agent）
- **预期**：
  - 步骤 1、2 均退出 `1`，stderr 含 `START / END 为保留节点，不可删除`。
  - 步骤 3 退出 `1`，stderr 含 `至少需要一个 agent 节点`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow edge — 列出 / 原子批量改边

### TC-012 edge --add/--rm 一次算清，给一对即单改

- **目的**：验证 `edge --add <from:to> --rm <from:to>` 原子生效——目标边集 = （当前 − rm）∪ add，末尾整份校验一次。
- **前置**：建隔离环境；建库 flow（`START→gen→review→END`）。
- **步骤**：
  1. 加一条从 `START` 直接扇出到 `review` 的边（让 `review` 也从头并行、多一个前驱）：`"$CONDUCT" workflow edge flow --add START:review; echo "exit=$?"`
  2. `"$CONDUCT" workflow edge flow`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow 边（+1 -0）`。
  - 步骤 2 含 `START → review`（新边）与原有四条边。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-013 edge --add 已存在的边报错；--rm 不存在的边报错

- **目的**：验证 `--add` 对「当前 − rm」判定加已存在边报错；`--rm` 对当前边集判定删不存在边报错（均退 `1`，不落盘）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow edge flow --add START:gen; echo "exit=$?"`（该边已存在）
  2. `"$CONDUCT" workflow edge flow --rm gen:END; echo "exit=$?"`（该边不存在，`gen` 实际连到 `review`）
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `加已存在的边 START→gen`。
  - 步骤 2 退出码 `1`，stderr 含 `删不存在的边 gen→END`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-014 edge 同一条边同时 --add 与 --rm，等价保留、不报重复

- **目的**：验证同一条边同时出现在 `--add` 与 `--rm` 时，等价「先删后加」、结果为保留该边，不因「已存在」报错。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow edge flow --add gen:review --rm gen:review; echo "exit=$?"`
  2. `"$CONDUCT" workflow edge flow`
- **预期**：
  - 步骤 1 退出码 `0`。
  - 步骤 2 仍含恰好一条 `gen → review`（未变化、未报重复）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-015 edge reorder 配方：a→b→c 改为 a→c→b（三步落盘皆合法）

- **目的**：验证 spec 给出的 reorder 配方——先 `set-prompt` 把数据流指向翻转所需的祖先关系铺垫好，再一次原子 `edge` 批量重连（含**终端出边一起迁移**），最后 `set-prompt` 补上新的引用方向；每步落盘都合法。
- **前置**：建隔离环境；造最小三节点链 `START→a→b→c→END`：
  ```bash
  cat > "$WORK/chain.json" <<'JSON'
  {"nodes":[{"id":"START"},
    {"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"a：{{sys.userPrompt}}"},
    {"id":"b","displayName":"乙","engine":"claude-code","promptTemplate":"b：{{a}}"},
    {"id":"c","displayName":"丙","engine":"claude-code","promptTemplate":"c：{{b}}"},
    {"id":"END"}],
   "edges":[{"from":"START","to":"a"},{"from":"a","to":"b"},{"from":"b","to":"c"},{"from":"c","to":"END"}]}
  JSON
  cat "$WORK/chain.json" | "$CONDUCT" workflow create chain --definition
  ```
- **步骤**：
  1. `c` 改引 `{{a}}`（此刻 `a` 仍是 `c` 的祖先，`START→a→b→c→END` 未变，合法）：
     `printf 'c 引用 a：{{a}}' | "$CONDUCT" workflow node set-prompt chain c; echo "exit=$?"`
  2. 一次原子重连（**连终端出边 `c:END`→`b:END` 一起迁移**，否则 `b` 会失去唯一出边被拒）：
     `"$CONDUCT" workflow edge chain --rm a:b --rm b:c --rm c:END --add a:c --add c:b --add b:END; echo "exit=$?"`
  3. `"$CONDUCT" workflow edge chain`
  4. `b` 改引 `{{c}}`（此刻 `c` 是 `b` 的祖先，合法）：
     `printf 'b 引用 c：{{c}}' | "$CONDUCT" workflow node set-prompt chain b; echo "exit=$?"`
  5. `"$CONDUCT" workflow show chain --expand`
- **预期**：
  - 步骤 1、2、4 均退出 `0`；步骤 2 stdout 含 `✓ 已更新 chain 边（+3 -3）`。
  - 步骤 3 含 `START → a`、`a → c`、`c → b`、`b → END`（不再含 `a → b`、`b → c`、`c → END`）。
  - 步骤 5 拓扑分层为 `level 0: [a]`、`level 1: [c]`、`level 2: [b]`（顺序已对调为 `a→c→b`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-016 edge 列出（无 --add/--rm）人类可读与 --json

- **目的**：验证 `edge`（无 --add/--rm）默认人类可读逐行 `<from> → <to>`，`--json` 输出对象数组。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow edge flow; echo "exit=$?"`
  2. `"$CONDUCT" workflow edge flow --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d)'`
- **预期**：
  - 步骤 1 退出码 `0`，逐行含 `START → gen`、`gen → review`、`review → END`。
  - 步骤 2 打印 `[{'from': 'START', 'to': 'gen'}, {'from': 'gen', 'to': 'review'}, {'from': 'review', 'to': 'END'}]`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node set — 局部编辑（结构化字段）

### TC-017 node set 逐项生效：--model / --effort / --display-name

- **目的**：验证 `node set` 只改指定字段，不碰其余（如 `promptTemplate`）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --model claude-sonnet-5 --effort high --display-name 生成器; echo "exit=$?"`
  2. `"$CONDUCT" workflow node show flow gen --json`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`。
  - 步骤 2 打印含 `"displayName": "生成器"`、`"engineConfig": {"model": "claude-sonnet-5", "effort": "high"}`、`"promptTemplate": "生成：{{sys.userPrompt}}"`（提示词未被 `node set` 改动）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-018 node set 空串清除标量（回落引擎默认）

- **目的**：验证 `--model ""` 清除该字段（回落引擎默认），三字段全空后 `engineConfig` 整体消失（而非留空对象）。
- **前置**：建隔离环境；建库 flow；先设模型：`"$CONDUCT" workflow node set flow gen --model claude-sonnet-5`。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --model ""; echo "exit=$?"`
  2. `"$CONDUCT" workflow show flow --json | python3 -c 'import sys,json;n=json.load(sys.stdin)["definition"]["nodes"][1];print("id=",n["id"],"has_engineConfig=", "engineConfig" in n)'`
  3. `"$CONDUCT" workflow node show flow gen`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`。
  - 步骤 2 打印 `id= gen has_engineConfig= False`（model 清除后 engineConfig 整体消失）。
  - 步骤 3 输出含 `(引擎默认)`（model 已回落默认）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-019 node set --engine 改引擎的级联：不兼容整份拒绝，可同命令一并重设

- **目的**：验证改 engine 后旧引擎专属字段被新引擎拒收时**整份校验失败退 1、绝不静默丢弃**；且允许在**同一条命令**里清掉旧字段修好。
- **前置**：建隔离环境；建库 flow；先给 `gen` 配 claude-code 专属 effort：`"$CONDUCT" workflow node set flow gen --effort high`。
- **步骤**：
  1. 只换引擎、不清旧字段（应被拒）：`"$CONDUCT" workflow node set flow gen --engine qoder; echo "exit=$?"`
  2. 同命令换引擎 + 清旧 effort + 设新档位（应成功）：`"$CONDUCT" workflow node set flow gen --engine qoder --effort "" --reasoning-effort medium; echo "exit=$?"`
  3. `"$CONDUCT" workflow node show flow gen --json`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `engine="qoder" 不认 effort`（旧 effort 未被静默丢弃、整份校验拦下）；原定义不变（仍是 claude-code）。
  - 步骤 2 退出码 `0`，stdout 含 `✓ 已更新 flow·gen`。
  - 步骤 3 打印含 `"engine": "qoder"`、`"engineConfig": {"reasoningEffort": "medium"}`（旧 effort 已清、新档位落位）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-020 node set 未给任何字段选项报用法错误

- **目的**：验证不给任何字段旗标时退出 `2`（用法错误），不落盘。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen; echo "exit=$?"`
- **预期**：
  - 退出码 `2`，stderr 含 `至少给一个字段选项`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-021 node set 字段级错误：display-name 空、目标节点不存在、名非法、目标为 START/END

- **目的**：验证四类各归其位的错误码：`--display-name ""` 字段级退 `1`；节点 id 不存在退 `1`；工作流名非法退 `2`（用法错误）；目标为保留节点退 `1`。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node set flow gen --display-name ""; echo "exit=$?"`
  2. `"$CONDUCT" workflow node set flow ghost --model m; echo "exit=$?"`
  3. `"$CONDUCT" workflow node set 'bad/name' gen --model m; echo "exit=$?"`
  4. `"$CONDUCT" workflow node set flow START --model m; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `节点 gen 的 displayName 不能为空`。
  - 步骤 2 退出码 `1`，stderr 含 `工作流无节点 ghost`。
  - 步骤 3 退出码 `2`，stderr 含 `工作流名 "bad/name" 非法`（名校验先于 store 载入，走用法错误）。
  - 步骤 4 退出码 `1`，stderr 含 `START 为保留标记节点，无可查看 / 编辑的定义`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node set-prompt — 局部编辑（提示词）

### TC-022 set-prompt 原始文本免转义写入（含 {{变量}} / 中文 / 多行）

- **目的**：验证提示词正文以原始文本从 stdin 整段读入、由 conduct 负责 JSON 编码——作者无需手工转义。
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
  2. `"$CONDUCT" workflow node show flow gen --json | python3 -c 'import sys,json;p=json.load(sys.stdin)["promptTemplate"];print("has_var=", "{{sys.userPrompt}}" in p);print("lines=", len(p.splitlines()))'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 flow·gen 提示词`。
  - 步骤 2 打印 `has_var= True` 与 `lines= 3`（三行正文原样入库；末尾恰好一个换行已剥掉）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-023 set-prompt ↔ show --prompt round-trip 字节稳定

- **目的**：验证 `show --prompt`（补恰好一个尾换行）与 `set-prompt`（剥恰好一个尾换行）配对，使「导出 → 写回 → 再导出」**逐字节稳定**。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen --prompt > "$WORK/a.md"`
  2. `cat "$WORK/a.md" | "$CONDUCT" workflow node set-prompt flow gen`
  3. `"$CONDUCT" workflow node show flow gen --prompt > "$WORK/b.md"`
  4. `diff "$WORK/a.md" "$WORK/b.md"; echo "roundtrip_diff=$?"`
  5. `tail -c 1 "$WORK/a.md" | od -An -tx1 | command grep -q '0a' && echo "ends_with_single_newline=yes"`
- **预期**：
  - 步骤 2 stdout 含 `✓ 已更新 flow·gen 提示词`。
  - 步骤 4 打印 `roundtrip_diff=0`（导出→写回→再导出后逐字节不变）。
  - 步骤 5 打印 `ends_with_single_newline=yes`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-024 set-prompt 空输入 / 非祖先引用被拒，原文件不变

- **目的**：验证空输入、或引用非祖先节点，落盘前被整份校验拦下退 `1`、**原提示词不变**。
- **前置**：建隔离环境；建库 flow；`cp "$WORK/.conduct/workflows/flow.json" "$WORK/before.json"`（留基线）。
- **步骤**：
  1. 空输入：`printf '' | "$CONDUCT" workflow node set-prompt flow gen; echo "exit=$?"`
  2. 引用不存在节点：`printf '{{ghost}}' | "$CONDUCT" workflow node set-prompt flow gen; echo "exit=$?"`
  3. `diff "$WORK/before.json" "$WORK/.conduct/workflows/flow.json"; echo "diff=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[1].promptTemplate: 必填`。
  - 步骤 2 退出码 `1`，stderr 含 `引用不存在的节点 {{ghost}}`。
  - 步骤 3 `diff=0`（两次失败后 `flow.json` 逐字节不变）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-025 set-prompt stdin 是终端时报错、不挂起

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
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow node show — 查询 / 导出（单节点）

### TC-026 node show 默认输出人类可读单节点详情

- **目的**：验证无旗标时输出 `id · displayName · engine · model` 一行摘要 + 空行 + 提示词全文（不截断）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - 首行为 `gen · 生成 · claude-code · (引擎默认)`（model 未设显示 `(引擎默认)`）。
  - 随后空行，再输出提示词全文 `生成：{{sys.userPrompt}}`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-027 node show --json 输出规范化单节点对象

- **目的**：验证 `--json` 输出规范化后的**单个** node 对象（非工作流整体、非数组）。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print(type(d).__name__, d["id"], d["engine"], "{{sys.userPrompt}}" in d["promptTemplate"])'`
- **预期**：
  - stdout 打印 `dict gen claude-code True`（是单个 `dict` 对象，含 `id`/`engine`/`promptTemplate`；`gen` 的提示词 `生成：{{sys.userPrompt}}` 含该变量子串）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-028 node show --prompt 与 --json 互斥

- **目的**：验证 `--prompt`（纯文本）与 `--json`（结构化对象）同给时报用法错误退 `2`。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow gen --prompt --json; echo "exit=$?"`
- **预期**：
  - 退出码 `2`，stderr 含 `--prompt 与 --json 互斥`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-029 node show 目标为 START/END 时报错（标记节点无可展示定义）

- **目的**：验证 `<id>` 为 `START`/`END` 时拒绝——标记节点无 engine/prompt，没有可展示的「定义」。
- **前置**：建隔离环境；建库 flow。
- **步骤**：
  1. `"$CONDUCT" workflow node show flow START; echo "exit=$?"`
  2. `"$CONDUCT" workflow node show flow END; echo "exit=$?"`
- **预期**：
  - 两步均退出 `1`，stderr 含 `START 为保留标记节点，无可查看 / 编辑的定义`（或 `END` 对应文案）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## create / edit 的 --help 定义结构 + 未知子命令文案

### TC-030 create / edit 的 --help 含 engineConfig 示例、不含循环相关残留提及

- **目的**：验证 `create` / `edit` 的 `--help` 里内嵌的定义结构说明含 `engineConfig` 字段示例，且**不含**已删除的 `evaluator` / `redoTarget` / `loopCount` 概念残留提及（DAG 改造已整体移除这些概念）。
- **前置**：建隔离环境（本用例只读 `--help`、不落盘，但沿用统一的临时目录存放 help 输出文件，便于清理）。
- **步骤**：
  1. `"$CONDUCT" workflow create --help > "$WORK/help_create.txt" 2>&1`
  2. `command grep -c 'engineConfig' "$WORK/help_create.txt"`
  3. `command grep -c 'evaluator' "$WORK/help_create.txt"; command grep -c 'redoTarget' "$WORK/help_create.txt"; command grep -c 'loopCount' "$WORK/help_create.txt"`
  4. `"$CONDUCT" workflow edit --help > "$WORK/help_edit.txt" 2>&1; command grep -c 'engineConfig' "$WORK/help_edit.txt"`
- **预期**：
  - 步骤 2 打印 `≥ 1`（实测 `2`）——`--help` 的定义示例含 `engineConfig` 字段。
  - 步骤 3 三行均打印 `0`——不再提及已删除的 evaluator / redoTarget / loopCount。
  - 步骤 4 打印 `≥ 1`——`edit --help` 复用同一份定义结构说明。
  - **注意**：本机若把 `grep` 覆写为其它工具（如某些开发环境里的 ripgrep 包装函数），需显式用 `command grep` 调用系统原生 grep，避免包装版本对 `-c` 等旗标行为不一致（本文档全篇统一用 `command grep`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-031 workflow / node 未知子命令列出全部可用动词

- **目的**：验证拼错子命令时 fail-loud 退 `2`，且错误文案列出的可用动词**含新增的 `copy` / `node` / `edge`**（workflow 层）与 `add / rm / set / set-prompt / show`（node 层）——命令面不自相矛盾。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" workflow bogus 2>&1; echo "exit=$?"`
  2. `"$CONDUCT" workflow node bogus 2>&1; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `2`，stderr 含 `未知子命令 "bogus"（可用：create / copy / edit / node / edge / rename / delete / list / show / run）`。
  - 步骤 2 退出码 `2`，stderr 含 `未知子命令 "bogus"（可用：add / rm / set / set-prompt / show）`。
- **清理**：无。
