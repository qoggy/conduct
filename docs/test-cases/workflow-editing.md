# workflow 编辑管理 测试用例

覆盖 conduct 中**不运行、不烧 token** 的一族命令：`version` 与工作流**整份生命周期**的增删改查 —— `workflow create / edit / rename / delete / list / show`，以及触发这些命令时执行的**整份 DAG 落盘校验规则**。粒度编辑（`node add/rm/set/set-prompt/show`、`edge`、`copy`）见 [workflow-node-editing.md](./workflow-node-editing.md)；运行工作流见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/cli-authoring.md](../specs/cli-authoring.md)。

> **模型基线**：工作流定义是**节点 + 边的有向无环图（DAG）**——`nodes[]` 恒含两个保留标记节点 `START`（唯一源）、`END`（唯一汇），其余为 agent 节点；`edges[]` 表达执行依赖。`create --definition` / `edit` 的导入体须**自带** `START` / `END` 节点与连边（store 不代为注入）。本文全部用例零 token：只做创建 / 编辑 / 改名 / 删除 / 查询 / 校验，均不调用 AI 引擎。

> **隔离机制（关键）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。为不污染真实家目录，**每个用例把 `HOME` 重定向到临时目录**（`export HOME="$WORK"`），store 随之落在 `$WORK/.conduct/`，用例结束连同临时目录一并删除。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
REAL_HOME="$HOME"            # 一次性记下真实家目录，供各用例清理时恢复（见下注）
test -r "$PWD/docs/test-cases/atomic-conduct-test.sh"
```

> TC-033、TC-034 各自在独立脚本块里 `source docs/test-cases/atomic-conduct-test.sh` 并调用 `conduct_test_setup`：单一 shell 原子边界、trap 托底清理、真实 `~/.conduct/workflows`/`runs` 前后零差异快照（见 [atomic-conduct-test.sh](./atomic-conduct-test.sh)），不依赖下面「建隔离环境」的手工模式。

各用例〈前置〉统一用这段建立隔离环境（下文简称「建隔离环境」）：

```bash
WORK=$(mktemp -d)
OLD_HOME="$HOME"; export HOME="$WORK"   # store 落到 $WORK/.conduct，隔离全局
```

> **弃跑保护**：`OLD_HOME` 在每个用例〈前置〉里现取现存，正常顺序执行无碍。但若某用例中途失败、未跑〈清理〉就直接开下一个用例的〈前置〉，`OLD_HOME` 会把上一个临时 HOME 存进去、恢复即错。此时改用环境准备一次性记下的 `export HOME="$REAL_HOME"` 找回真实家目录。

对应〈清理〉统一为：

```bash
export HOME="$OLD_HOME"; rm -rf "$WORK"
```

多个用例复用的**最小 workflow 定义**（单 agent 节点，含 START/END 与连边——`create --definition` 的导入体须自带二者）：

```bash
cat > "$WORK/min.json" <<'JSON'
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
```

---

## version

### TC-001 version 打印版本号

- **目的**：验证 `conduct version` 输出版本号且退出码为 0。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" version; echo "exit=$?"`
- **预期**：
  - 退出码 `0`（末行 `exit=0`）。
  - stdout 形如 `conduct <VERSION>`（`<VERSION>` 为构建注入的版本串，如 SemVer / git 描述 / `dev`，不逐字比对）。
- **清理**：无。

### TC-002 --version 全局标志等价于 version

- **目的**：验证全局 `--version` 打印版本号并退出 0（spec 全局约定）。
- **前置**：无（只读）。
- **步骤**：
  1. `"$CONDUCT" --version; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout 打印版本号，与 TC-001 同格式。
- **清理**：无。

---

## workflow create

### TC-003 create 脚手架一份最小骨架（START→node-1→END）

- **目的**：验证 `workflow create <name>` 生成一份最小骨架并入库——单 agent 节点 `node-1`（引擎 `codex`），自动带 `START`/`END` 与两条连边。
- **前置**：建隔离环境。
- **步骤**：
  1. `"$CONDUCT" workflow create my-flow; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `test -f "$WORK/.conduct/workflows/my-flow.json"; echo "exists=$?"`
  4. `"$CONDUCT" workflow show my-flow --json | python3 -c 'import sys,json;d=json.load(sys.stdin)["definition"];ids=[n["id"] for n in d["nodes"]];edges=[(e["from"],e["to"]) for e in d["edges"]];print("ids=",ids);print("edges=",edges)'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已创建 my-flow`。
  - 步骤 2 列表含 `my-flow` 一行，`NODES` 列为 `node-1`（不含 START/END）。
  - 步骤 3 打印 `exists=0`（store 内已落盘 `my-flow.json`）。
  - 步骤 4 打印 `ids= ['START', 'node-1', 'END']`、`edges= [('START', 'node-1'), ('node-1', 'END')]`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-004 create --definition 从 stdin 导入定义（须自带 START/END）

- **目的**：验证 `workflow create <name> --definition` 从 stdin 读入完整定义主体入库；导入体必须自带 `START`/`END`（store 不代为注入）。
- **前置**：建隔离环境；准备最小定义 `min.json`（见〈环境准备〉）。
- **步骤**：
  1. `cat "$WORK/min.json" | "$CONDUCT" workflow create ported --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow show ported --json`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已创建 ported`。
  - 步骤 2 输出规范化 JSON：含系统补齐的 `"name":"ported"`、`createdAt`、`updatedAt`（RFC3339，值不逐字比对），`definition.nodes` 含 `START`/`say`/`END` 三项、`definition.edges` 含 `START→say`、`say→END`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-005 create 同名已存在时拒绝、不覆盖

- **目的**：验证重复创建同名工作流报错且不写坏原文件。
- **前置**：建隔离环境。
- **步骤**：
  1. `"$CONDUCT" workflow create dup`
  2. `cp "$WORK/.conduct/workflows/dup.json" "$WORK/before.json"`（留基线快照）
  3. `"$CONDUCT" workflow create dup; echo "exit=$?"`
  4. `diff "$WORK/before.json" "$WORK/.conduct/workflows/dup.json"; echo "diff=$?"`
- **预期**：
  - 步骤 3 退出码 `1`，stderr 含 `工作流 dup 已存在`。
  - 步骤 4 `diff=0`（原文件逐字节未变，重复 create 未覆盖）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-006 create --definition 内容非法时拒绝入库

- **目的**：验证非法定义（缺 START/END 与 agent 必填字段）被拒、不落盘。
- **前置**：
  1. 建隔离环境。
  2. 造一份**不含 START/END、也缺 engine** 的非法定义：
     ```bash
     printf '{"nodes":[{"id":"say","displayName":"x","promptTemplate":"hi"}]}' > "$WORK/bad.json"
     ```
- **步骤**：
  1. `cat "$WORK/bad.json" | "$CONDUCT" workflow create broken --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 同时含 `须恰好含一个 START 标记节点，得到 0 个`、`须恰好含一个 END 标记节点，得到 0 个`、`nodes[0].engine: 必填`（`ValidateStructured` 一次性收集多条字段级错误）。
  - 步骤 2 列表**不含** `broken`；`$WORK/.conduct/workflows/broken.json` 不存在。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-007 create --definition 导入体带整条记录时元数据被忽略（不因 name 不一致报错）

- **目的**：验证导入体若直接给整条记录（含 `name` / 时间戳外壳），解包 `definition`、**忽略**元数据——`name` 由命令参数 `<name>` 定，即便导入体的 `name` 与目标名不一致也**不报错**（spec〈workflow create〉：`create --definition` 导入体只需 `definition` 的值，元数据不一致不报错，这与旧版「不一致即拒绝」不同，是本次 DAG 改造的既定行为变更）。
- **前置**：
  1. 建隔离环境。
  2. 造一份 `name` 与目标不符的**整条记录**（含 `definition` 外壳）：
     ```bash
     printf '{"name":"WRONG","createdAt":"2020-01-01T00:00:00Z","updatedAt":"2020-01-01T00:00:00Z","definition":{"nodes":[{"id":"START"},{"id":"a","displayName":"x","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}}' > "$WORK/mismatch.json"
     ```
- **步骤**：
  1. `cat "$WORK/mismatch.json" | "$CONDUCT" workflow create rightname --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow show rightname --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["name"])'`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已创建 rightname`（不因 `name="WRONG"` 与目标不一致而拒绝）。
  - 步骤 2 打印 `rightname`（`name` 取自命令参数，不是导入体里的 `WRONG`；`WRONG` 未被当作独立工作流创建）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow show

### TC-008 show 打印单个工作流详情（邻接 + START/END 标注）

- **目的**：验证 `workflow show <name>` 打印名称 / agent 节点数 / 逐节点行，随后打印边邻接并标注 `START`/`END`。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create demo --definition`
- **步骤**：
  1. `"$CONDUCT" workflow show demo; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout 含 `demo · 1 节点`，有一行以节点 id `say` 开头（`say · 打招呼 · claude-code`），随后 `边：` 段含 `START → say`、`say → END`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-009 show --expand 预览拓扑分层（零成本，START 扇出并行）

- **目的**：验证 `--expand` 用 `workflow.TopoLevels` 打印拓扑分层（同层可并行），且**不调用引擎、不花 token**。用 `START` 同时扇出到两个节点、汇聚到第三个节点的菱形图验证多层输出。
- **前置**：
  1. 建隔离环境。
  2. 造一份 `START` 扇出到 `a`、`b`，两者汇聚到 `c` 的定义：
     ```bash
     cat > "$WORK/diamond.json" <<'JSON'
     {
       "nodes": [
         { "id": "START" },
         { "id": "a", "displayName": "调研", "engine": "claude-code", "promptTemplate": "{{sys.userPrompt}}" },
         { "id": "b", "displayName": "起草", "engine": "qoder", "promptTemplate": "{{sys.userPrompt}}" },
         { "id": "c", "displayName": "实现", "engine": "claude-code", "promptTemplate": "{{a}} {{b}}" },
         { "id": "END" }
       ],
       "edges": [
         { "from": "START", "to": "a" },
         { "from": "START", "to": "b" },
         { "from": "a", "to": "c" },
         { "from": "b", "to": "c" },
         { "from": "c", "to": "END" }
       ]
     }
     JSON
     cat "$WORK/diamond.json" | "$CONDUCT" workflow create diamond --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow show diamond --expand; echo "exit=$?"`
- **预期**：
  - 退出码 `0`（纯本地拓扑分层，不触发任何引擎调用，不产生 `~/.conduct/runs/` 记录）。
  - stdout 在详情之后追加 `拓扑分层（同层可并行；实际调度贪心，节点自身依赖就绪即开跑）：`，随后两行 `level 0: [a, b]`、`level 1: [c]`（`a`、`b` 以 `START` 为唯一前驱同落 level 0，`c` 依赖二者落 level 1；层清单不含 `START`/`END`）。
  - `$WORK/.conduct/runs/` 目录为空或不存在（`--expand` 不算一次运行）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 show --json 输出规范化定义（含 --expand 的 levels 字段）

- **目的**：验证 `--json` 输出规范化（camelCase、补默认值、含 START/END）的完整记录 JSON；`--json --expand` 额外挂 `"levels"` 字段。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create demo --definition`
- **步骤**：
  1. `"$CONDUCT" workflow show demo --json | python3 -m json.tool >/dev/null; echo "valid_json=$?"`
  2. `"$CONDUCT" workflow show demo --json | python3 -c 'import sys,json;d=json.load(sys.stdin);print("name=",d["name"]);ids=[n["id"] for n in d["definition"]["nodes"]];print("ids=",ids)'`
  3. `"$CONDUCT" workflow show demo --json --expand | python3 -c 'import sys,json;print(json.load(sys.stdin)["levels"])'`
- **预期**：
  - 步骤 1 `valid_json=0`（是合法 JSON）。
  - 步骤 2 打印 `name= demo`、`ids= ['START', 'say', 'END']`。
  - 步骤 3 打印 `[['say']]`（单 agent 节点、单层）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 show 不存在的工作流报错

- **目的**：验证 show 对不存在工作流显式失败。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow show nope; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含错误信息（如 `工作流 nope 不存在` 或等价校验 / 缺失报错）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow edit

### TC-012 edit 从 stdin 整体替换定义（须自带 START/END）

- **目的**：验证 `workflow edit <name>` 用 stdin 新定义整体替换既有工作流；替换体须自带 `START`/`END`。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create f`（脚手架骨架）。
  3. 准备一份新定义：
     ```bash
     printf '{"nodes":[{"id":"START"},{"id":"only","displayName":"唯一节点","engine":"claude-code","promptTemplate":"回复 ok"},{"id":"END"}],"edges":[{"from":"START","to":"only"},{"from":"only","to":"END"}]}' > "$WORK/new.json"
     ```
- **步骤**：
  1. `cat "$WORK/new.json" | "$CONDUCT" workflow edit f; echo "exit=$?"`
  2. `"$CONDUCT" workflow show f --json`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 f`。
  - 步骤 2 JSON 中 `definition.nodes` 只含 `START`/`only`/`END`（原骨架 `node-1` 已被整体替换）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-013 edit 校验不过时拒绝写入、保留原定义

- **目的**：验证非法新定义（空 `nodes`）被拒且**原文件不变**（不写坏数据）。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create keep --definition`。
  3. 记录原文件：`cp "$WORK/.conduct/workflows/keep.json" "$WORK/before.json"`。
  4. 造非法新定义（空 nodes）：`printf '{"nodes":[]}' > "$WORK/empty.json"`。
- **步骤**：
  1. `cat "$WORK/empty.json" | "$CONDUCT" workflow edit keep; echo "exit=$?"`
  2. `diff "$WORK/before.json" "$WORK/.conduct/workflows/keep.json"; echo "diff=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含字段级校验错误 `nodes: 不能为空，至少需要一个节点`。
  - 步骤 2 `diff=0`（原文件逐字节不变）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-014 edit 不存在的工作流报错

- **目的**：验证 edit 目标不存在时失败。
- **前置**：建隔离环境（store 为空）；准备 `min.json`。
- **步骤**：
  1. `cat "$WORK/min.json" | "$CONDUCT" workflow edit ghost; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `工作流 ghost 不存在`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-015 edit 无有效 stdin 定义时报错、不挂起

- **目的**：验证 edit 缺有效定义时**不挂起等待、显式失败**。区分两条路径：stdin 是**终端** → 退出 `2`（报缺少定义、提示 `conduct ui`）；stdin 是**非 TTY 空输入**（如 `< /dev/null`）→ 按非法 JSON 处理、退出 `1`。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create t --definition`。
- **步骤**：
  1. （非 TTY 空输入分支）`"$CONDUCT" workflow edit t < /dev/null; echo "exit=$?"`
  2. （真 TTY 分支，pty 伪终端驱动，可无人值守自动化）把 stdin 接成真终端后运行 `edit`、不喂任何输入，用超时守卫验证它立即失败返回而非挂起等待：

     ```bash
     python3 - "$CONDUCT" <<'PY'
     import os, pty, subprocess, sys
     conduct = sys.argv[1]
     master, slave = pty.openpty()            # 分配伪终端；slave 端 os.isatty()=True
     p = subprocess.Popen([conduct, "workflow", "edit", "t"],
                          stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
     os.close(slave)
     try:
         out, err = p.communicate(timeout=5)  # 超时守卫：若停在等待输入即为 bug
         print(f"exit={p.returncode}"); print("stderr:", err.strip())
     except subprocess.TimeoutExpired:
         p.kill(); print("exit=HANG(FAIL)")
     os.close(master)
     PY
     ```

     （沿用〈前置〉里 `export HOME="$WORK"`，python 子进程继承同一隔离 store。）
- **预期**：
  - 步骤 1：命令立即返回、不挂起；退出码 `1`，stderr 含 `解析定义 JSON 失败`（空输入非合法 JSON，具体原因如 `unexpected end of JSON input`，不同 Go 版本 / 平台措辞可能略有差异，只校验前缀）；`t.json` 不变。
  - 步骤 2：脚本立即返回（不挂起，耗时 < 1s），打印 `exit=2`，stderr 含 `缺少定义` 与 `conduct ui`；`t.json` 不变。
  - 说明：步骤 2 验的是「stdin 是真终端」分支——用 pty 伪终端把 stdin 接成 tty 即可在 CI / 无人值守自动化里复现，**不必真人守终端**；`< /dev/null`（步骤 1）走的是非 TTY 空输入分支，两条分支预期不同，勿混用。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow rename

### TC-016 rename 改名成功

- **目的**：验证 `workflow rename <old> <new>` 改标识、不动定义。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create old-name --definition`。
- **步骤**：
  1. `"$CONDUCT" workflow rename old-name new-name; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `test -f "$WORK/.conduct/workflows/new-name.json" && ! test -e "$WORK/.conduct/workflows/old-name.json"; echo "renamed=$?"`
  4. `grep -c '"name": "new-name"' "$WORK/.conduct/workflows/new-name.json"`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已重命名 old-name → new-name`。
  - 步骤 2 列表含 `new-name`、不含 `old-name`。
  - 步骤 3 打印 `renamed=0`（新文件在、旧文件已消失）。
  - 步骤 4 打印 `1`（文件内 `name` 字段已改为 `new-name`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-017 rename 源不存在时报错

- **目的**：验证 `<old>` 不存在时失败、不改动。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow rename nope other; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `工作流 nope 不存在`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-018 rename 目标已存在时拒绝、不覆盖

- **目的**：验证 `<new>` 已被占用时失败、不覆盖。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create a`；`"$CONDUCT" workflow create b`。
- **步骤**：
  1. `"$CONDUCT" workflow rename a b; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `工作流 b 已存在`。
  - 步骤 2 列表仍同时含 `a` 与 `b`（均未被改）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-019 rename 新名非法时报用法错误

- **目的**：验证新名不匹配 `[A-Za-z0-9._-]+` 时退出 `2`。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create a`。
- **步骤**：
  1. `"$CONDUCT" workflow rename a 'bad/name'; echo "exit=$?"`
- **预期**：
  - 退出码 `2`，stderr 报名称非法。
  - `a` 未被改动（`$WORK/.conduct/workflows/a.json` 仍在）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow delete

### TC-020 delete 删除单个工作流

- **目的**：验证 `workflow delete <name> --yes` 删除并回显。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create gone`。
- **步骤**：
  1. `"$CONDUCT" workflow delete gone --yes; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `test -e "$WORK/.conduct/workflows/gone.json"; echo "still_exists=$?"`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已删除 gone`。
  - 步骤 2 列表不含 `gone`（store 空则提示 `（store 为空）`）。
  - 步骤 3 打印 `still_exists=1`（`gone.json` 已被删除）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-021 delete 批量删除多个

- **目的**：验证一次删除多个工作流。
- **前置**：
  1. 建隔离环境。
  2. `for n in a b c; do "$CONDUCT" workflow create "$n"; done`。
- **步骤**：
  1. `"$CONDUCT" workflow delete a b c --yes; echo "exit=$?"`
- **预期**：
  - 退出码 `0`，stdout 逐行含 `✓ 已删除 a`、`✓ 已删除 b`、`✓ 已删除 c`。
  - 三个 `.json` 均被删除。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-022 delete 含不存在名称时以失败退出

- **目的**：验证批量删除中存在项照删、有缺失项则整体非 0 退出。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create real`。
- **步骤**：
  1. `"$CONDUCT" workflow delete real ghost --yes; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 退出码 `1`（有失败项）；stderr 含 `工作流 ghost 不存在`；stdout 含 `✓ 已删除 real`。
  - 步骤 2 列表不含 `real`（存在项仍被删除）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-023 delete 非交互且未加 --yes 时拒绝

- **目的**：验证非 TTY 下缺 `--yes` 拒绝删除、退出 `2`、不删。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create safe`。
- **步骤**：
  1. `"$CONDUCT" workflow delete safe < /dev/null; echo "exit=$?"`
- **预期**：
  - 退出码 `2`，stderr 含 `拒绝在非交互环境删除，请加 --yes`。
  - `safe.json` 仍在（未删除）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-024 delete --json 输出机读结果

- **目的**：验证 `workflow delete --yes --json` 输出 `{"deleted":[...]}`（spec delete 机读约定）。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create d1`。
- **步骤**：
  1. `"$CONDUCT" workflow delete d1 --yes --json | python3 -c 'import sys,json; print(json.load(sys.stdin)["deleted"])'; echo "exit=${PIPESTATUS[0]}"`
- **预期**：
  - stdout 打印 `['d1']`；`exit=0`。
  - `d1.json` 已删除。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow list

### TC-025 list 空 store 给出空提示

- **目的**：验证空 store 输出提示、退出 `0`（空不是错误）。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow list; echo "exit=$?"`
- **预期**：
  - 退出码 `0`，stdout 含 `（store 为空）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-026 list 表格与 --json 一致列出（NODES 列不含 START/END）

- **目的**：验证 list 默认表格与 `--json` 均列出全部工作流；`NODES` 列只展示 agent 节点 id 流，不含 `START`/`END`。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create one --definition`。
  3. `"$CONDUCT" workflow create two`。
- **步骤**：
  1. `"$CONDUCT" workflow list`
  2. `"$CONDUCT" workflow list --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(sorted(x["name"] for x in d)); print("nodes_ok=", all(isinstance(x.get("nodes"), list) and x["nodes"] and "START" not in x["nodes"] and "END" not in x["nodes"] for x in d))'`
- **预期**：
  - 步骤 1 表格有表头 `NAME`/`NODES`/`UPDATED`，含 `one`（`NODES` 列为 `say`）与 `two`（`NODES` 列为 `node-1`）两行，均不出现 `START`/`END`。
  - 步骤 2 打印 `['one', 'two']` 与 `nodes_ok= True`（`--json` 为数组、每项含 `name` 与 `nodes`——`nodes` 为非空、不含 START/END 的 agent 节点 id 数组）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-027 workflow list 按 updatedAt 倒序、name 升序兜底

- **目的**：验证 `workflow list` / `--json` 的行序遵守排序契约——**最近修改在最前**（`updatedAt` 倒序），同一 `updatedAt` 内按 `name` **升序兜底**；该序**不是**单纯按名字排（否则 `updatedAt` 维度未生效）；且 **CLI 与 UI `/api/workflows` 同源同序**（前端不二次排序）。此契约与本次 DAG 模型改造无关，随原实现保留、随本文一并核对。
- **前置**：
  1. 建隔离环境。
  2. 造出**跨两个修改时刻**的四个工作流：先建 `apple`，隔 >1s 再建 `banana`/`zebra`/`mango`（后三者力求同一秒、构成 `name` 兜底组；`apple` 更早、构成 `updatedAt` 更旧组）。因 run id / `updatedAt` 是**秒级**粒度，用 `sleep 1.1` 确保 `apple` 与后三者落在不同秒：
     ```bash
     "$CONDUCT" workflow create apple >/dev/null
     sleep 1.1
     "$CONDUCT" workflow create banana >/dev/null
     "$CONDUCT" workflow create zebra  >/dev/null
     "$CONDUCT" workflow create mango  >/dev/null
     ```
  3. 起 UI 服务端供步骤 3 校验 UI 同序（无需引擎，仅列表）：
     ```bash
     "$CONDUCT" ui --port 0 > "$WORK/ui.log" 2>&1 & UIPID=$!
     for i in $(seq 1 50); do
       B=$(grep -oE 'http://127\.0\.0\.1:[0-9]+' "$WORK/ui.log" | head -1)
       [ -n "$B" ] && curl -s -o /dev/null "$B/api/version" && break; sleep 0.1
     done
     ```
- **步骤**：
  1. 取表格首列的行序：
     `"$CONDUCT" workflow list | awk 'NR>1{print $1}'`
  2. 对 `--json` 校验行序恰是「`updatedAt` 倒序、`name` 升序兜底」、非纯字母序，**并显式确认 `name` 兜底组真的形成、组内升序**（否则同 `updatedAt` 下的 name 兜底其实没被触发到，须重跑前置）：
     ```bash
     "$CONDUCT" workflow list --json | python3 -c '
     import sys, json
     from collections import Counter
     d = json.load(sys.stdin)
     actual = [x["name"] for x in d]
     exp = sorted(d, key=lambda p: p["name"])                        # 先 name 升序
     exp = sorted(exp, key=lambda p: p["updatedAt"], reverse=True)   # 再 updatedAt 倒序（稳定排序保 name 兜底）
     expected = [x["name"] for x in exp]
     c = Counter(x["updatedAt"] for x in d)                          # 找出同 updatedAt（≥2 个）的兜底组
     groups = [[x["name"] for x in d if x["updatedAt"] == u] for u, n in c.items() if n > 1]
     print("actual  =", actual)
     print("expected=", expected)
     print("contract_match=", actual == expected)
     print("not_pure_alpha=", actual != sorted(actual))
     print("tie_group_found=", len(groups) > 0)                      # 兜底组是否真形成
     print("tie_group=", groups)
     print("tie_group_name_ascending=", all(g == sorted(g) for g in groups))
     '
     ```
  3. 校验 UI `/api/workflows` 行序与 CLI **逐位一致**（CLI 与 UI 同源同序）：
     ```bash
     CLI=$("$CONDUCT" workflow list --json); UI=$(curl -s "$B/api/workflows")
     CLI_JSON="$CLI" UI_JSON="$UI" python3 -c '
     import os, json
     cli = [x["name"] for x in json.loads(os.environ["CLI_JSON"])]
     ui  = [x["name"] for x in json.loads(os.environ["UI_JSON"])["workflows"]]
     print("cli_order=", cli)
     print("ui_order =", ui)
     print("cli_ui_same_order=", cli == ui)
     '
     ```
- **预期**：
  - 步骤 1 表格数据行按 `updatedAt` 倒序：`apple`（更旧）排在**最后**，`banana`/`zebra`/`mango`（更新的同秒组）排在前、组内按名升序（`banana` → `mango` → `zebra`）。典型输出：
    ```
    banana
    mango
    zebra
    apple
    ```
  - 步骤 2 打印 `contract_match= True`（`--json` 行序 == 由各项自身 `updatedAt`+`name` 按契约算出的期望序）、`not_pure_alpha= True`（行序不等于把名字直接字母排序的结果，证明 `updatedAt` 维度确实参与）、`tie_group_found= True` 且 `tie_group_name_ascending= True`（同 `updatedAt` 的兜底组**确实形成**且组内 name 升序——`name` 兜底被真触发，而非因四者各占一秒而空过）。**若 `tie_group_found= False`**：说明后三者未落在同一秒（机器过慢跨了秒），兜底维度没测到，须重跑前置直至形成兜底组。
  - 步骤 3 打印 `cli_ui_same_order= True`——UI `/api/workflows` 行序与 CLI 逐位相同（`handleListWorkflows` 直接沿用 `store.List`、前端不二次排序）。
  - **归一化说明**：`updatedAt` 时间戳本身忽略（每次不同），只校验**行序**符合契约；`contract_match` 由数据自身算出的期望序对照，验的是「CLI 按契约排」；`name` 兜底另由 `tie_group_*` 显式确认「同秒组存在且升序」，不再默许「跨秒时 `contract_match` 照样过」而漏测兜底。
- **清理**：`kill "$UIPID" 2>/dev/null; wait "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 补充：DAG 落盘校验规则逐条覆盖（fail-loud，零 token）

以下用例覆盖 spec〈落盘校验规则〉——都通过 `create --definition` 触发（校验对所有变更命令一致），**每条规则至少一个专门触发它失败的用例**，验证非法定义在入库前即被具体报错拦下、不落盘。

### TC-028 节点集：START/END 数量与 agent 节点数不满足要求

- **目的**：验证「恰好一个 START、一个 END，至少一个 agent 节点」的三条子规则各自触发。
- **前置**：建隔离环境。
- **步骤**：
  1. 缺 START/END（TC-006 已覆盖，此处补「只缺 END」）：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"}],"edges":[{"from":"START","to":"a"}]}' | "$CONDUCT" workflow create m1 --definition; echo "exit=$?"`
  2. 两个 START（重复）：见下方 TC-029（本条不重复）。
  3. 只有 START/END、无 agent 节点：`printf '{"nodes":[{"id":"START"},{"id":"END"}],"edges":[]}' | "$CONDUCT" workflow create m2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `须恰好含一个 END 标记节点，得到 0 个`。
  - 步骤 3 退出码 `1`，stderr 含 `至少需要一个 agent 节点（START / END 之外）`（同时因 `START→END` 直连、无边可能触发其它错误，只校验此关键子串出现）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-029 保留名：agent 节点不得为 START/END，重复 START 标记节点被拒

- **目的**：验证 agent 节点 id 撞保留名、以及 `nodes[]` 里出现两个 `id=="START"` 时被拒（`ValidateStructured` 会一次性列出多条关联错误，只校验关键子串）。
- **前置**：建隔离环境。
- **步骤**：
  1. 两个 `id=="START"`：`printf '{"nodes":[{"id":"START"},{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create ts --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `nodes[1].id: 与前面的节点重复 "START"` 与 `须恰好含一个 START 标记节点，得到 2 个`。
  - `ts.json` 未落盘。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-030 标记节点必空：START/END 携带 engine / displayName / promptTemplate / engineConfig 被拒

- **目的**：验证标记节点若携带任何 agent 专属字段（`displayName`/`engine`/`promptTemplate`/`engineConfig`）被逐条拒绝。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"START","engine":"claude-code"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create mk --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `nodes[0].engine: 标记节点 START 的 engine 必须为空`。
  - `mk.json` 未落盘。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-031 agent 节点必填字段缺失被拒（displayName / engine / promptTemplate）

- **目的**：验证 agent 节点缺 `displayName` / `engine` / `promptTemplate` 时字段级报错、不落盘。
- **前置**：建隔离环境。
- **步骤**：
  1. 缺 displayName：`printf '{"nodes":[{"id":"START"},{"id":"a","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create m1 --definition; echo "exit=$?"`
  2. 缺 engine：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create m2 --definition; echo "exit=$?"`
  3. 缺 promptTemplate：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create m3 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[1].displayName: 必填`。
  - 步骤 2 退出码 `1`，stderr 含 `nodes[1].engine: 必填`。
  - 步骤 3 退出码 `1`，stderr 含 `nodes[1].promptTemplate: 必填`。
  - 三者均未落盘（`"$CONDUCT" workflow list` 应为空）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-032 agent 节点 id 命名不规范被拒（非法字符 / 重复）

- **目的**：验证 agent 节点 id 不匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`、或同一定义内 id 重复时被拒。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"START"},{"id":"1x","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"1x"},{"from":"1x","to":"END"}]}' | "$CONDUCT" workflow create n1 --definition; echo "exit=$?"`
  2. `printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"a","displayName":"乙","engine":"claude-code","promptTemplate":"ho"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create n2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[1].id: "1x" 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）`（数字开头非法）。
  - 步骤 2 退出码 `1`，stderr 含 `nodes[2].id: 与前面的节点重复 "a"`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-033 未知引擎被拒 / 已注册引擎包含 Codex 与 Kiro

- **目的**：验证 `engine` 取未注册值时被拒并回显动态生成的可用引擎清单；代表性的已注册引擎 `codex` 与 `kiro` 在空 `engineConfig` 时被接受。
- **前置**：无；下方脚本自行建立隔离、注册 trap、固定中文 locale，并做真实 store 前后快照。
- **步骤**：完整复制执行一个脚本块：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export LC_ALL=zh_CN.UTF-8
  set +e
  printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"nope","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create e1 --definition
  echo "e1-exit=$?"
  set -e
  printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create e2 --definition; echo "e2-exit=$?"
  printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"kiro","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create e3 --definition; echo "e3-exit=$?"
  BASH
  ```
- **预期**：
  - `e1-exit=1`，stderr 含 `nodes[1].engine: 未知引擎 "nope"`，并在 `可用：` 后按 name 排序列出当前 descriptor 注册表中的全部引擎。
  - `e2-exit=0`，建流成功（codex 已注册，空 engineConfig 合法）。
  - `e3-exit=0`，建流成功（kiro 已注册，空 engineConfig 合法）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

### TC-034 engineConfig 引擎-字段匹配与取值被校验

- **目的**：验证统一 `effort` 对 claude-code/qoder/codex/kiro 生效、antigravity 拒绝，且旧 `reasoningEffort` 与普通未知字段 `xxxabc` 完全同路失败。
- **前置**：无；下方脚本自行建立隔离、注册 trap、固定中文 locale，并做真实 store 前后快照（agent 节点均包在 START→a→END 中）。
- **步骤**：完整复制执行一个脚本块：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export LC_ALL=zh_CN.UTF-8
  expect_rejected() {
    local name="$1" json="$2"
    set +e
    printf '%s' "$json" | "$CONDUCT" workflow create "$name" --definition
    rc=$?
    set -e
    echo "$name-exit=$rc"
  }
  # 1. claude effort 非法值
  expect_rejected c1 '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","engineConfig":{"effort":"insane"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' 2>"$WORK/c1.err"; cat "$WORK/c1.err"
  # 2. antigravity 不认 effort
  expect_rejected c2 '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"antigravity","promptTemplate":"hi","engineConfig":{"effort":"high"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' 2>"$WORK/c2.err"; cat "$WORK/c2.err"
  # 3. qoder effort 非法值
  expect_rejected c3 '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"qoder","promptTemplate":"hi","engineConfig":{"effort":"insane"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' 2>"$WORK/c3.err"; cat "$WORK/c3.err"
  # 4. 四引擎统一接受合法 effort
  for pair in 'claude-code high' 'qoder max' 'codex xhigh' 'kiro max'; do
    set -- $pair
    printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"%s","promptTemplate":"hi","engineConfig":{"effort":"%s"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' "$1" "$2" | "$CONDUCT" workflow create "ok-$1" --definition
  done
  echo "ok-exit=$?"
  # 5. reasoningEffort 与 xxxabc 同路失败
  for field in reasoningEffort xxxabc; do
    set +e
    printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi","engineConfig":{"%s":"high"}},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' "$field" | "$CONDUCT" workflow create "bad-$field" --definition 2>"$WORK/$field.err"
    echo "$field-exit=$?"
    set -e
  done
  sed 's/reasoningEffort/<unknown>/g' "$WORK/reasoningEffort.err" > "$WORK/reasoning.norm"
  sed 's/xxxabc/<unknown>/g' "$WORK/xxxabc.err" > "$WORK/ordinary.norm"
  diff -u "$WORK/reasoning.norm" "$WORK/ordinary.norm"; echo "diff_exit=$?"
  BASH
  ```
- **预期**：
  - `c1-exit=1`，`$WORK/c1.err` 含 `"insane" 不在 engine="claude-code" 允许集`。
  - `c2-exit=1`，`$WORK/c2.err` 含 `engine="antigravity" 不接受 effort`。
  - `c3-exit=1`，`$WORK/c3.err` 含 `不在 engine="qoder" 允许集`。
  - `ok-exit=0`（四引擎循环创建均成功，证明统一接受 `effort` 且各自允许集仍独立）。
  - `reasoningEffort-exit=1`、`xxxabc-exit=1`；`diff_exit=0`（归一化后的 stderr 完全相同，只出现 JSON decoder 的普通 `unknown field`，没有迁移建议或字段专用诊断）。
- **清理**：脚本 trap 自动恢复 `HOME/PATH`、清理临时目录并比较真实 store 前后快照。

### TC-035 边合法性：自环 / 重复边 / 指向 START / 源自 END / START→END 直连

- **目的**：验证边规则的五条子规则各自触发（`create --definition` 一次性建出违规图）。
- **前置**：建隔离环境。
- **步骤**：
  1. 自环：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create s1 --definition; echo "exit=$?"`
  2. 重复边：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create s2 --definition; echo "exit=$?"`
  3. 边指向 START：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"START"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create s3 --definition; echo "exit=$?"`
  4. 边源自 END：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"},{"from":"END","to":"a"}]}' | "$CONDUCT" workflow create s4 --definition; echo "exit=$?"`
  5. START→END 直连：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"},{"from":"START","to":"END"}]}' | "$CONDUCT" workflow create s5 --definition; echo "exit=$?"`
- **预期**（均退出 `1`，均未落盘）：
  - 步骤 1 stderr 含 `禁止自环 a→a`。
  - 步骤 2 stderr 含 `重复边 START→a`。
  - 步骤 3 stderr 含 `禁止边指向 START（START 无入边）`。
  - 步骤 4 stderr 含 `禁止边源自 END（END 无出边）`。
  - 步骤 5 stderr 含 `禁止 START→END 直连（须过 ≥1 个 agent 节点）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-036 无环校验：环被检测并报出环路径

- **目的**：验证 `a→b→a` 成环时被 `DetectCycle` 拦下，报出环路径。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"b","displayName":"乙","engine":"claude-code","promptTemplate":"ho"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"b"},{"from":"b","to":"a"},{"from":"b","to":"END"}]}' | "$CONDUCT" workflow create cyc --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `edges: 检测到环 a→b→a`。
  - `cyc.json` 未落盘。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-037 单源单汇 / 无悬空：agent 节点缺入边或出边被拒（孤立节点）

- **目的**：验证一个完全脱离图（无任何连边）的 agent 节点触发「无入边」与「无出边」两条错误——`START`/`END` 保证了单源单汇，其余节点必须各至少一条入边和出边。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"orphan","displayName":"孤","engine":"claude-code","promptTemplate":"hi"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create orp --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 同时含 `agent 节点 "orphan" 无入边（须 ≥1 条，可来自 START）` 与 `agent 节点 "orphan" 无出边（须 ≥1 条，可到 END）`。
  - `orp.json` 未落盘。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-038 模板变量引用祖先：引用不存在节点 / 非祖先节点 / 标记节点 / 未知系统变量均被拒

- **目的**：验证 `promptTemplate` 里 `{{X}}` 的四类非法引用——不存在的节点、存在但非本节点祖先、标记节点 `{{START}}`/`{{END}}`（无产物）、未知 `{{sys.*}}`——均被 fail-loud 拒绝（此规则较旧版「仅校验存在」更严格，是本次 DAG 改造新增的祖先限定）。
- **前置**：建隔离环境。
- **步骤**：
  1. 引用不存在节点：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"{{ghost}}"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create tr1 --definition; echo "exit=$?"`
  2. 引用非祖先节点（`b` 与 `a` 是从 `START` 各自并行的兄弟节点，互不为祖先）：
     ```bash
     printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"b","displayName":"乙","engine":"claude-code","promptTemplate":"看 {{a}}"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"START","to":"b"},{"from":"a","to":"END"},{"from":"b","to":"END"}]}' | "$CONDUCT" workflow create tr2 --definition; echo "exit=$?"
     ```
  3. 引用标记节点：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"{{START}}"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create tr3 --definition; echo "exit=$?"`
  4. 未知系统变量：`printf '{"nodes":[{"id":"START"},{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"{{sys.foo}}"},{"id":"END"}],"edges":[{"from":"START","to":"a"},{"from":"a","to":"END"}]}' | "$CONDUCT" workflow create tr4 --definition; echo "exit=$?"`
- **预期**（均退出 `1`，均未落盘）：
  - 步骤 1 stderr 含 `nodes[1].promptTemplate: 引用不存在的节点 {{ghost}}`。
  - 步骤 2 stderr 含 `nodes[2].promptTemplate: 引用非上游祖先节点 {{a}}（数据流须来自沿边可达的前驱）`。
  - 步骤 3 stderr 含 `nodes[1].promptTemplate: 禁止引用标记节点 {{START}}（无产物）`。
  - 步骤 4 stderr 含 `nodes[1].promptTemplate: 引用未知系统变量 {{sys.foo}}（仅支持 sys.userPrompt / sys.cwd / sys.runId）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。
