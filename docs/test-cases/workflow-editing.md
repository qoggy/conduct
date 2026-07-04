# workflow 编辑管理 测试用例

覆盖 conduct 中**不运行、不烧 token** 的一族命令：`version` 与 workflow 定义的增删改查 —— `workflow create / edit / rename / delete / list / show`。运行工作流与查看运行记录见 [workflow-running.md](./workflow-running.md)。对应 spec：[docs/specs/cli-commands.md](../specs/cli-commands.md)。

> **预期以 spec 为准，不以当前代码为准。** 本文描述 spec 规定的**目标行为**（命令该怎样表现），用来验证实现对不对——不是照现有代码反推。截至编写时，`version` 与 `workflow create / edit / rename / delete / list / show` **均已实现**（见 spec〈实现状态〉），预期可直接对照验证；唯一例外是全局 `--version` 标志（TC-002）尚未接线——当前跑它返回 `unknown flag: --version`、退出 `2`，本文按 spec 目标（退出 `0`）写预期，待其落地。命令若有偏离本文〈预期〉，即为实现未达标。

> **本文全部用例零 token**：只做创建 / 编辑 / 改名 / 删除 / 查询 / 校验 / 展开预览，均不调用 AI 引擎。

> **隔离机制（关键）**：conduct 的 store 固定在 `~/.conduct/`、不支持自定义位置。为不污染真实家目录，**每个用例把 `HOME` 重定向到临时目录**（`export HOME="$WORK"`），store 随之落在 `$WORK/.conduct/`，用例结束连同临时目录一并删除。若某命令的实现改用了别的路径解析方式（非 `$HOME`），按实际调整本机制。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，cd 进临时目录 / 改 HOME 后仍可用
REAL_HOME="$HOME"            # 一次性记下真实家目录，供各用例清理时恢复（见下注）
```

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

多个用例复用的**最小 workflow 定义**（单节点，导入体仅需 `nodes`）：

```bash
cat > "$WORK/min.json" <<'JSON'
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
  - **现状注**：全局 `--version` 尚未接线，当前实际返回 `unknown flag: --version`、退出 `2`。上述为 spec 目标行为，待落地后本用例即可通过。
- **清理**：无。

---

## workflow create

### TC-003 create 脚手架一份最小骨架

- **目的**：验证 `workflow create <name>` 生成一份最小骨架并入库。
- **前置**：建隔离环境。
- **步骤**：
  1. `"$CONDUCT" workflow create my-flow; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
  3. `test -f "$WORK/.conduct/workflows/my-flow.json"; echo "exists=$?"`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已创建 my-flow`。
  - 步骤 2 列表含 `my-flow` 一行。
  - 步骤 3 打印 `exists=0`（store 内已落盘 `my-flow.json`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-004 create --definition 从 stdin 导入定义

- **目的**：验证 `workflow create <name> --definition` 从 stdin 读入完整定义入库。
- **前置**：建隔离环境；准备最小定义 `min.json`（见〈环境准备〉）。
- **步骤**：
  1. `cat "$WORK/min.json" | "$CONDUCT" workflow create ported --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow show ported --json`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已创建 ported`。
  - 步骤 2 输出规范化 JSON：含系统补齐的 `"name":"ported"`、`createdAt`、`updatedAt`（RFC3339，值不逐字比对），`nodes[0].id` 为 `say`。
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

- **目的**：验证非法定义（校验不过）被拒、不落盘。
- **前置**：
  1. 建隔离环境。
  2. 造一份缺 `engine` 的非法定义：
     ```bash
     printf '{"nodes":[{"id":"say","displayName":"x","promptTemplate":"hi"}]}' > "$WORK/bad.json"
     ```
- **步骤**：
  1. `cat "$WORK/bad.json" | "$CONDUCT" workflow create broken --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 打印字段级校验错误（指出 `nodes[0]` 缺 `engine` 之类）。
  - 步骤 2 列表**不含** `broken`；`$WORK/.conduct/workflows/broken.json` 不存在。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow show

### TC-007 show 打印单个工作流详情

- **目的**：验证 `workflow show <name>` 打印名称 / 节点数 / 逐节点行。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create demo --definition`
- **步骤**：
  1. `"$CONDUCT" workflow show demo; echo "exit=$?"`
- **预期**：
  - 退出码 `0`。
  - stdout 含工作流名 `demo` 与节点数（`1 节点`），并有一行以节点 id `say` 开头、含 `claude-code` 与循环模式 `单次`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 show --expand 预览展开步骤（零成本、含循环）

- **目的**：验证 `--expand` 复用运行时展开算法、把节点图展开成线性步骤序列，且**不调用引擎、不花 token**。用带 `evaluator` 内循环的定义验证多步展开。
- **前置**：
  1. 建隔离环境。
  2. 造一份含 evaluator、`loopCount:2` 的定义（写 → 评 → 改，循环 2 轮）：
     ```bash
     cat > "$WORK/loop.json" <<'JSON'
     {
       "nodes": [
         {
           "id": "code",
           "displayName": "编码",
           "engine": "claude-code",
           "promptTemplate": "只回复一个词：done。需求：{{sys.userPrompt}}",
           "loopCount": 2,
           "evaluator": {
             "engine": "claude-code",
             "promptTemplate": "只回复：<verdict>PASS</verdict>。审阅：{{code}}"
           }
         }
       ]
     }
     JSON
     cat "$WORK/loop.json" | "$CONDUCT" workflow create looped --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow show looped --expand; echo "exit=$?"`
- **预期**：
  - 退出码 `0`（纯本地展开，不触发任何引擎调用，不产生 `~/.conduct/runs/` 记录）。
  - stdout 在详情之后追加展开清单，每行形如 `[i] type node=<id> iter=<n>`；因 `loopCount:2` 的内循环，出现多行 `agent` / `evaluator` 交替、`iter` 递增（step 数 > 1）。
  - `$WORK/.conduct/runs/` 目录为空或不存在（`--expand` 不算一次运行）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-009 show --json 输出规范化定义

- **目的**：验证 `--json` 输出规范化（camelCase、补默认值）定义 JSON。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create demo --definition`
- **步骤**：
  1. `"$CONDUCT" workflow show demo --json | python3 -m json.tool >/dev/null; echo "valid_json=$?"`
  2. `"$CONDUCT" workflow show demo --json`
- **预期**：
  - 步骤 1 `valid_json=0`（是合法 JSON）。
  - 步骤 2 输出对象含 `"name": "demo"`、`"nodes"` 数组，`nodes[0]` 含 `"id": "say"`、`"engine": "claude-code"`、`"promptTemplate"`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 show 不存在的工作流报错

- **目的**：验证 show 对不存在工作流显式失败。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow show nope; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含错误信息（如 `工作流 nope 不存在` 或等价校验 / 缺失报错）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow edit

### TC-011 edit 从 stdin 整体替换定义

- **目的**：验证 `workflow edit <name>` 用 stdin 新定义整体替换既有工作流。
- **前置**：
  1. 建隔离环境。
  2. `"$CONDUCT" workflow create f`（脚手架骨架）。
  3. 准备一份新定义：
     ```bash
     printf '{"nodes":[{"id":"only","displayName":"唯一节点","engine":"claude-code","promptTemplate":"回复 ok"}]}' > "$WORK/new.json"
     ```
- **步骤**：
  1. `cat "$WORK/new.json" | "$CONDUCT" workflow edit f; echo "exit=$?"`
  2. `"$CONDUCT" workflow show f --json`
- **预期**：
  - 步骤 1 退出码 `0`，stdout 含 `✓ 已更新 f`。
  - 步骤 2 JSON 中 `nodes` 只有一个节点、`id` 为 `only`（原骨架已被整体替换）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-012 edit 校验不过时拒绝写入、保留原定义

- **目的**：验证非法新定义被拒且**原文件不变**（不写坏数据）。
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

### TC-013 edit 不存在的工作流报错

- **目的**：验证 edit 目标不存在时失败。
- **前置**：建隔离环境（store 为空）；准备 `min.json`。
- **步骤**：
  1. `cat "$WORK/min.json" | "$CONDUCT" workflow edit ghost; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `工作流 ghost 不存在`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-014 edit 无有效 stdin 定义时报错、不挂起

- **目的**：验证 edit 缺有效定义时**不挂起等待、显式失败**。区分 spec 的两条路径（见 cli-commands.md）：stdin 是**终端** → 退出 `2`（报缺少定义、提示 `conduct ui`）；stdin 是**非 TTY 空输入**（如 `< /dev/null`）→ 按非法 JSON 处理、退出 `1`。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create t --definition`。
- **步骤**：
  1. （自动化，非 TTY 空输入）`"$CONDUCT" workflow edit t < /dev/null; echo "exit=$?"`
  2. （手工，真 TTY）在**真实终端**直接敲 `"$CONDUCT" workflow edit t`（不接管道、不重定向），观察其是否立即返回。
- **预期**：
  - 步骤 1：命令立即返回、不挂起；退出码 `1`，stderr 含 `解析定义 JSON 失败: EOF`（空输入非合法 JSON）；`t.json` 不变。
  - 步骤 2：命令**立即报错返回、不停在等待输入**；退出码 `2`，stderr 报缺少定义并提示改用 `conduct ui`。
  - 说明：步骤 2 依赖真 TTY，无法在管道 / CI 中自动复现，故列为手工项；`< /dev/null` 走的是非 TTY 分支，与 TTY 分支预期不同，勿混用。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## workflow rename

### TC-015 rename 改名成功

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

### TC-016 rename 源不存在时报错

- **目的**：验证 `<old>` 不存在时失败、不改动。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow rename nope other; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `工作流 nope 不存在`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-017 rename 目标已存在时拒绝、不覆盖

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

### TC-018 rename 新名非法时报用法错误

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

### TC-019 delete 删除单个工作流

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

### TC-020 delete 批量删除多个

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

### TC-021 delete 含不存在名称时以失败退出

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

### TC-022 delete 非交互且未加 --yes 时拒绝

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

---

## workflow list

### TC-023 list 空 store 给出空提示

- **目的**：验证空 store 输出提示、退出 `0`（空不是错误）。
- **前置**：建隔离环境（store 为空）。
- **步骤**：
  1. `"$CONDUCT" workflow list; echo "exit=$?"`
- **预期**：
  - 退出码 `0`，stdout 含 `（store 为空）`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-024 list 表格与 --json 一致列出

- **目的**：验证 list 默认表格与 `--json` 均列出全部工作流。
- **前置**：
  1. 建隔离环境；准备 `min.json`。
  2. `cat "$WORK/min.json" | "$CONDUCT" workflow create one --definition`。
  3. `"$CONDUCT" workflow create two`。
- **步骤**：
  1. `"$CONDUCT" workflow list`
  2. `"$CONDUCT" workflow list --json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(sorted(x["name"] for x in d))'`
- **预期**：
  - 步骤 1 表格有表头 `NAME`/`NODES`/`ENGINES`/`UPDATED`，含 `one` 与 `two` 两行；`one` 的 `ENGINES` 含 `claude-code`。
  - 步骤 2 打印 `['one', 'two']`（`--json` 为数组、每项含 `name`）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 补充：落盘校验 fail-loud 与机读输出

以下用例覆盖 spec〈落盘校验规则〉里强调的 fail-loud 强化点（比原型更严），及机读输出，均零 token。

### TC-025 导入体 name 与目标名不一致时拒绝（不静默改名）

- **目的**：验证 `create --definition` / `edit` 的导入体里若带 `name` 且不等于目标名，须显式拒绝——改名只能走 `rename`，绝不靠导入体静默改名（spec〈落盘校验规则〉「元数据字段系统管理」）。
- **前置**：
  1. 建隔离环境。
  2. 造一份 `name` 与目标不符的定义：
     ```bash
     printf '{"name":"WRONG","nodes":[{"id":"a","displayName":"x","engine":"claude-code","promptTemplate":"hi"}]}' > "$WORK/mismatch.json"
     ```
- **步骤**：
  1. `cat "$WORK/mismatch.json" | "$CONDUCT" workflow create rightname --definition; echo "exit=$?"`
  2. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `导入定义的 name="WRONG" 与目标 "rightname" 不一致`（并提示改名用 `conduct workflow rename`）。
  - 步骤 2 列表**不含** `rightname` 与 `WRONG`（未落盘、未静默改名）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-026 evaluator 与 redoTarget 互斥被拒

- **目的**：验证同一 node 同时带 `evaluator` 与 `redoTarget` 时入库被拒（spec 互斥规则）。
- **前置**：
  1. 建隔离环境。
  2. 造一份互斥违规定义（节点 `b` 同时有 `redoTarget` 与 `evaluator`）：
     ```bash
     printf '{"nodes":[{"id":"a","displayName":"x","engine":"claude-code","promptTemplate":"hi"},{"id":"b","displayName":"y","engine":"claude-code","promptTemplate":"{{a}}","redoTarget":"a","evaluator":{"engine":"claude-code","promptTemplate":"chk"}}]}' > "$WORK/mutex.json"
     ```
- **步骤**：
  1. `cat "$WORK/mutex.json" | "$CONDUCT" workflow create mx --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `nodes[1]: evaluator 与 redoTarget 互斥，不能并存`。
  - store 内无 `mx.json`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-027 模板引用不存在的节点被拒

- **目的**：验证 `promptTemplate` 里 `{{<nodeId>}}` 引用了定义内不存在的节点时，入库即 fail-loud 拒绝（spec「模板变量引用存在」）。
- **前置**：
  1. 建隔离环境。
  2. 造一份引用幽灵节点的定义：
     ```bash
     printf '{"nodes":[{"id":"a","displayName":"x","engine":"claude-code","promptTemplate":"{{ghost}}"}]}' > "$WORK/badref.json"
     ```
- **步骤**：
  1. `cat "$WORK/badref.json" | "$CONDUCT" workflow create tr --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `nodes[0].promptTemplate: 引用不存在的节点 {{ghost}}`。
  - store 内无 `tr.json`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-028 delete --json 输出机读结果

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

## 补充：节点定义逐条校验（fail-loud，零 token）

覆盖 spec〈落盘校验规则〉与 `Validate` 的字段级拒绝——**每条规则一个触发用例**，验证非法定义在入库前即被具体报错拦下、不落盘。TC-006（缺 engine）、TC-026（evaluator/redoTarget 互斥）、TC-027（模板引用幽灵节点）已覆盖其中三条，以下补齐其余。错误信息为当前实现实测值，只匹配关键子串。多个变体的用例逐条 create 到临时名，每条都应被拒。

### TC-029 节点必填字段缺失被拒（displayName / promptTemplate）

- **目的**：验证节点缺 `displayName` 或 `promptTemplate` 时字段级报错、不落盘（补 TC-006 的「缺 engine」到必填字段全覆盖）。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"a","engine":"claude-code","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create m1 --definition; echo "exit=$?"`
  2. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code"}]}' | "$CONDUCT" workflow create m2 --definition; echo "exit=$?"`
  3. `"$CONDUCT" workflow list`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].displayName: 必填`。
  - 步骤 2 退出码 `1`，stderr 含 `nodes[0].promptTemplate: 必填`。
  - 步骤 3 列表为空（`m1`/`m2` 均未落盘）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-030 节点 id 命名不规范被拒（非法字符 / 重复）

- **目的**：验证节点 id 不匹配命名规则、或同一定义内 id 重复时被拒（节点 id 命名规范，区别于 TC-018 的工作流名）。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"1x","displayName":"甲","engine":"claude-code","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create n1 --definition; echo "exit=$?"`
  2. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi"},{"id":"a","displayName":"乙","engine":"claude-code","promptTemplate":"ho"}]}' | "$CONDUCT" workflow create n2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].id: "1x" 非法`（须匹配 `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`，数字开头非法）。
  - 步骤 2 退出码 `1`，stderr 含 `nodes[1].id: 与前面的节点重复 "a"`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-031 未知 / 已下线引擎被拒

- **目的**：验证 `engine` 取未注册值（含已下线的 `codex`）时被拒，并回显可用引擎清单。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"nope","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create e1 --definition; echo "exit=$?"`
  2. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"codex","promptTemplate":"hi"}]}' | "$CONDUCT" workflow create e2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].engine: 未知引擎 "nope"`，并列出 `可用：antigravity, claude-code, qoder`。
  - 步骤 2 退出码 `1`，stderr 含 `未知引擎 "codex"`（codex 已下线，按未知引擎处理）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-032 engineConfig 引擎-字段匹配与取值被校验

- **目的**：验证 `engineConfig` 的调优字段与引擎绑定：claude-code 用 `effort`、qoder 用 `reasoningEffort`、antigravity 二者都不认；且取值须在各自允许集内。逐条触发。
- **前置**：建隔离环境。
- **步骤**（每条都应退出 `1`）：
  1. claude effort 非法值：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","engineConfig":{"effort":"insane"}}]}' | "$CONDUCT" workflow create c1 --definition; echo "exit=$?"`
  2. claude 不认 reasoningEffort：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","engineConfig":{"reasoningEffort":"high"}}]}' | "$CONDUCT" workflow create c2 --definition; echo "exit=$?"`
  3. antigravity 不认 effort：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"antigravity","promptTemplate":"hi","engineConfig":{"effort":"high"}}]}' | "$CONDUCT" workflow create c3 --definition; echo "exit=$?"`
  4. qoder 不认 effort：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"qoder","promptTemplate":"hi","engineConfig":{"effort":"high"}}]}' | "$CONDUCT" workflow create c4 --definition; echo "exit=$?"`
  5. qoder reasoningEffort 非法值：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"qoder","promptTemplate":"hi","engineConfig":{"reasoningEffort":"insane"}}]}' | "$CONDUCT" workflow create c5 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 stderr 含 `engineConfig.effort: "insane" 不在 engine="claude-code" 允许集`。
  - 步骤 2 stderr 含 `engine="claude-code" 不认 reasoningEffort`。
  - 步骤 3 stderr 含 `engine="antigravity" 不认 effort`。
  - 步骤 4 stderr 含 `engine="qoder" 不认 effort`。
  - 步骤 5 stderr 含 `不在 engine="qoder" 允许集`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-033 redoTarget 合法性被校验（指向后节点 / 不存在）

- **目的**：验证 `redoTarget` 只能指向本节点之前的节点，指向后节点或不存在的节点均被拒（回跳循环的合法性）。
- **前置**：建隔离环境。
- **步骤**：
  1. 指向后节点：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","redoTarget":"b"},{"id":"b","displayName":"乙","engine":"claude-code","promptTemplate":"ho"}]}' | "$CONDUCT" workflow create r1 --definition; echo "exit=$?"`
  2. 指向不存在：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","redoTarget":"ghost"}]}' | "$CONDUCT" workflow create r2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].redoTarget: 必须指向本节点之前的节点，"b" 在其后或即本身`。
  - 步骤 2 退出码 `1`，stderr 含 `nodes[0].redoTarget: 指向不存在的节点 "ghost"`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-034 模板引用未知系统变量被拒

- **目的**：验证 `promptTemplate` 里 `{{sys.<x>}}` 引用了未支持的系统变量时被拒（补 TC-027 的「引用不存在节点」到系统变量维度）。
- **前置**：建隔离环境。
- **步骤**：
  1. `printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"{{sys.foo}}"}]}' | "$CONDUCT" workflow create sv --definition; echo "exit=$?"`
- **预期**：
  - 退出码 `1`，stderr 含 `引用未知系统变量 {{sys.foo}}（仅支持 sys.userPrompt / sys.cwd）`；`sv.json` 未落盘。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-035 loopCount 越界被拒

- **目的**：验证 `loopCount` 超出 `1–20` 时被拒（下界 0 与上界 21 各一）。
- **前置**：建隔离环境。
- **步骤**：
  1. 下界：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","loopCount":0,"evaluator":{"engine":"claude-code","promptTemplate":"e"}}]}' | "$CONDUCT" workflow create l1 --definition; echo "exit=$?"`
  2. 上界：`printf '{"nodes":[{"id":"a","displayName":"甲","engine":"claude-code","promptTemplate":"hi","loopCount":21,"evaluator":{"engine":"claude-code","promptTemplate":"e"}}]}' | "$CONDUCT" workflow create l2 --definition; echo "exit=$?"`
- **预期**：
  - 步骤 1 退出码 `1`，stderr 含 `nodes[0].loopCount: 须为 1–20 的整数，得到 0`。
  - 步骤 2 退出码 `1`，stderr 含 `须为 1–20 的整数，得到 21`。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 补充：展开预览覆盖「自循环 + redo 叠加」（零 token）

TC-008 已验单节点自循环的 `--expand`；本节验**自循环与 redo 段循环叠加**时的**精确步序**——只数总步数会放过「顺序错位但步数不变」的 bug，故逐步对照。纯本地展开，不调引擎。

### TC-036 show --expand 展开自循环+redo 叠加的精确步序

- **目的**：验证 `A → B(自循环) → C(redoTarget→A)` 叠加时，`--expand` 展开出的每一步节点与顺序正确：redo 段把 `[A,B]`（连同 B 的内循环）整段重跑，C 为段尾。
- **前置**：
  1. 建隔离环境。
  2. 造叠加定义并入库：
     ```bash
     cat > "$WORK/redoloop.json" <<'JSON'
     {
       "nodes": [
         {"id":"A","displayName":"甲","engine":"claude-code","promptTemplate":"a：{{sys.userPrompt}}"},
         {"id":"B","displayName":"乙","engine":"claude-code","promptTemplate":"b","loopCount":1,"evaluator":{"engine":"claude-code","promptTemplate":"评 {{B}}"}},
         {"id":"C","displayName":"丙","engine":"claude-code","promptTemplate":"c","loopCount":1,"redoTarget":"A"}
       ]
     }
     JSON
     cat "$WORK/redoloop.json" | "$CONDUCT" workflow create rl --definition
     ```
- **步骤**：
  1. `"$CONDUCT" workflow show rl --expand; echo "exit=$?"`
- **预期**：
  - 退出码 `0`（纯展开、不触发引擎，不产生 `~/.conduct/runs/` 记录）。
  - stdout 含 `▶ 展开为 10 步：`，随后展开清单的 10 行按此顺序出现（每行含 序号 / type / node / iter 四个字段；实际输出行首带缩进、列宽随内容对齐，**只对照四字段取值与行序，不比对缩进与列宽**）：
    ```
    [0] agent     node=A          iter=1
    [1] agent     node=B          iter=1
    [2] evaluator node=B          iter=1
    [3] agent     node=B          iter=1
    [4] agent     node=C          iter=1
    [5] agent     node=A          iter=2
    [6] agent     node=B          iter=2
    [7] evaluator node=B          iter=2
    [8] agent     node=B          iter=2
    [9] agent     node=C          iter=2
    ```
  - 语义对照：`A→B→B评→B→C→A→B→B评→B→C`——redo 段循环第 2 轮把 `A、B` 连同 B 的自循环重跑，段内各步 iter 取段循环轮号。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。
