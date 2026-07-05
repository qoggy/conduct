# conduct ui 服务端与 /api 测试用例

覆盖 `conduct ui` 的**服务端**：启动行为（入口地址 / 驻留 / 端口 / store 探测）、`/api/*` 全端点（工作流 CRUD、运行查询、引擎能力表、目录浏览、启动 / 终止运行）、以及三层安全防护（Host / Origin 白名单、变更类强制 JSON）。对应 spec：[docs/specs/ui.md](../specs/ui.md)〈API 设计〉与 [docs/specs/cli-commands.md](../specs/cli-commands.md)〈ui〉。CLI 层的 `conduct ui` 冒烟（启动、打印地址、驻留）与 `run stop` 命令见 [workflow-running.md](./workflow-running.md)；工作流定义增删改查的 CLI 侧见 [workflow-editing.md](./workflow-editing.md)。

> **预期以 spec 为准。** 本文描述 spec 规定的**目标行为**，用来验证实现对不对。当前实现状态（见 cli-commands.md〈实现状态〉）：`ui` 服务端 + `/api/*` 全端点 + self-exec 发射器**已实装**，预期可直接对照；**内嵌前端 SPA 代码已落地、待浏览器走查验收**（见 [ui.md](../specs/ui.md)），本文只测 HTTP API（黑盒），页面交互待走查通过后另补前端手工用例。

> **全程零 💸（零 token）**：本文所有用例都在**隔离临时 HOME**（`export HOME="$WORK"`，store 落在 `$WORK/.conduct/`）里跑，用后连目录一并删除，不碰真实 store。**唯一会真起子进程的启动运行端点（`POST …/runs`）也用一个「一运行就失败」的假引擎**（见〈环境准备〉`broke_engine`）——self-exec 出的子进程继承这个坏 PATH，秒级失败、不触真实引擎、零 token，却仍产出一条**真实**的 run 记录（`status:"failed"`），足以验证发射链路与 run id 回传。真实引擎的端到端跑通（`completed` + 终止**运行中**的 run）交给 [workflow-running.md](./workflow-running.md) 的 💸 用例，本文不重复烧钱。

> **用户视角、不伪造内部数据**：需要「有一条运行记录 / 有一个工作流」时，一律经**对外接口**真实造出来（`POST /api/workflows` 建工作流、`POST …/runs` 真发射一次 run），绝不手写 `~/.conduct` 下的 `run.json` / `<name>.json` 去摆拍（那是与内部存储格式死耦合的伪造，见 test-case-writing skill 的 MUST〈用户视角〉）。

## 环境准备（每篇跑一次）

在仓库根执行，构建被测二进制并固定绝对路径供各用例引用：

```bash
make build
CONDUCT="$PWD/bin/conduct"   # 用绝对路径，改 HOME 后仍可用
```

各用例复用的**辅助函数**（在每个用例的〈前置〉里 `source` 或直接粘贴定义；隔离 HOME 已在各用例内建好）：

```bash
# 装一个「一运行就报错退出」的假 claude，遮蔽真 claude，使 self-exec 出的 run 秒级失败、零 token。
broke_engine() {   # 用法：broke_engine   —— 需已 export HOME="$WORK"
  mkdir -p "$WORK/brokenbin"
  cat > "$WORK/brokenbin/claude" <<'SH'
#!/usr/bin/env bash
echo "claude: 引擎不可用（模拟故障）" >&2
exit 1
SH
  chmod +x "$WORK/brokenbin/claude"
  export PATH="$WORK/brokenbin:$PATH"
}

# 后台起服务（--port 0 让系统随机分配端口，避免固定端口撞车），等就绪后把入口地址写进 $B。
start_ui() {   # 用法：start_ui   —— 设置全局 UIPID 与 B（形如 http://127.0.0.1:<port>）
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

> **归一化说明（全篇通用）**：版本号写 `<VERSION>`（形如 `0.0.1` / `dev` / `<hash>-dirty`，不逐字比对）；端口号每次随机，只校验 `http://127.0.0.1:` 前缀与「能连上」；run id 的时间后缀、`createdAt`/`updatedAt` 时间戳、临时目录路径均忽略，只校验格式或子串。断言 HTTP 状态码用 `curl -o /dev/null -w '%{http_code}'`；断言 JSON 字段用 `python3` 解析取值，不比对键序。

---

## conduct ui 启动

### TC-001 ui 启动打印入口地址、探测 store、驻留至被中断

- **目的**：验证 `conduct ui` 只绑 `127.0.0.1`、启动即探测 store、stdout 打印入口地址横幅，且进程驻留（不一闪而过）。
- **前置**：隔离 HOME：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. 后台启动，等待后先探活、抓横幅、再中断：
     ```bash
     "$CONDUCT" ui --port 0 > "$WORK/ui.log" 2>&1 &
     UIPID=$!
     sleep 1
     kill -0 "$UIPID" 2>/dev/null && echo "ui_alive"   # 中断前先确认进程仍驻留
     cat "$WORK/ui.log"
     kill "$UIPID" 2>/dev/null; wait "$UIPID" 2>/dev/null   # 回收，避免僵尸
     ```
- **预期**：
  - 打印 `ui_alive`——`sleep 1` 后进程仍活着，证明它驻留而非退出。
  - `ui.log` 含三行横幅：`conduct ui — 可视化界面已启动`、`  ▶ http://127.0.0.1:<port>`（只校验 `http://127.0.0.1:` 前缀，端口随机忽略）、`按 Ctrl-C 退出。`。
- **清理**：`kill "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-002 ui --port 指定端口

- **目的**：验证 `--port <n>` 让服务监听指定端口（横幅与实际可访问端口一致）。
- **前置**：隔离 HOME：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。取一个大概率空闲端口：`PORT=7742`。
- **步骤**：
  1. ```bash
     "$CONDUCT" ui --port "$PORT" > "$WORK/ui.log" 2>&1 &
     UIPID=$!; sleep 1
     grep -o "http://127.0.0.1:$PORT" "$WORK/ui.log"
     curl -s "http://127.0.0.1:$PORT/api/version"; echo
     kill "$UIPID" 2>/dev/null; wait "$UIPID" 2>/dev/null
     ```
- **预期**：
  - 横幅出现 `http://127.0.0.1:7742`；`curl` 该端口 `/api/version` 返回 `{"version":"<VERSION>"}`。
  - **归一化说明**：若 `7742` 恰被占用，横幅不会出现且 `curl` 失败——换一个空闲端口重跑（端口占用本身另见 TC-003）。
- **清理**：`kill "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-003 ui 端口被占用 → 报错退 1（不自动递增）

- **目的**：验证目标端口被占用时 `conduct ui` 报错退 `1`，不静默改用别的端口。
- **前置**：隔离 HOME：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。`PORT=7743`。
- **步骤**：
  1. 先占住端口（起第一个实例），再用同端口起第二个：
     ```bash
     "$CONDUCT" ui --port "$PORT" >/dev/null 2>&1 &
     UIPID=$!; sleep 1
     "$CONDUCT" ui --port "$PORT" 2>"$WORK/err.txt"; echo "exit=$?"
     cat "$WORK/err.txt"
     kill "$UIPID" 2>/dev/null; wait "$UIPID" 2>/dev/null
     ```
- **预期**：
  - 第二个实例退出码 `1`；stderr 含 `端口可能已被占用` 与 `address already in use` 摘要（第一个实例仍正常驻留）。
- **清理**：`kill "$UIPID" 2>/dev/null; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-004 ui store 不可读 → 启动即报错退 1（不做「启动假成功」）

- **目的**：验证启动时主动探测 store 可读性——store 损坏（`workflows/` 不是目录）时**启动即失败**退 `1`，而非「启动假成功、首个请求才报错」。
- **前置**：隔离 HOME 并把 store 的 `workflows` 弄成一个**普通文件**（`List()` 会因「不是目录」失败）：
  ```bash
  WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"
  mkdir -p "$WORK/.conduct"
  : > "$WORK/.conduct/workflows"   # 该占坑文件挡住目录，List() 读它即报「not a directory」
  ```
- **步骤**：
  1. `"$CONDUCT" ui --port 0 2>"$WORK/err.txt"; echo "exit=$?"`
  2. `cat "$WORK/err.txt"`
- **预期**：
  - 命令**立即返回**（不驻留），退出码 `1`。
  - stderr 含 `store 不可读` 与 `not a directory` 摘要。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-005 ui 拒绝多余位置参数 → 退 2

- **目的**：验证 `conduct ui` 是无位置参数的整体 GUI，传多余参数属用法错误退 `2`。
- **前置**：隔离 HOME：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`。
- **步骤**：
  1. `"$CONDUCT" ui bogus 2>"$WORK/err.txt"; echo "exit=$?"; cat "$WORK/err.txt"`
- **预期**：
  - 退出码 `2`；stderr 含 `unknown command "bogus"`（Cobra 用法错误）。
- **清理**：`export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 安全防护（Host / Origin / JSON）

> 三层防护套在**所有**路由上（`/api/*` 与静态 `/`）：非白名单 `Host` → 403；带非白名单 `Origin` → 403；带 body 的变更类 `/api` 请求非 `application/json` → 415。诚实边界：这防的是浏览器跨站与 DNS rebinding，**不防本机进程**（curl 能带任意头，正好用来构造这些攻击面）。

### TC-006 非白名单 Host → 403

- **目的**：验证请求的 `Host` 不在 `127.0.0.1:<port>` / `localhost:<port>` 白名单时被拒 403。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s -o /dev/null -w "%{http_code}\n" -H 'Host: evil.example.com' "$B/api/version"`
  2. 对照：白名单 Host（curl 默认即 `127.0.0.1:<port>`）应放行：
     `curl -s -o /dev/null -w "%{http_code}\n" "$B/api/version"`
- **预期**：
  - 步骤 1 打印 `403`（Host 不在白名单）。
  - 步骤 2 打印 `200`（默认 Host 在白名单）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-007 跨站 Origin → 403

- **目的**：验证带跨站 `Origin` 头（模拟恶意网页 fetch 本地端口）被拒 403。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s -o /dev/null -w "%{http_code}\n" -H 'Origin: http://evil.example.com' "$B/api/version"`
- **预期**：
  - 打印 `403`（Origin 不在白名单）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-008 变更类请求非 application/json → 415

- **目的**：验证带 body 的变更类 `/api` 端点若 `Content-Type` 非 `application/json` 被拒 415（连带挡住表单式 CSRF）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/workflows" -H 'Content-Type: text/plain' -d 'x=1'`
  2. 对照：同一端点带正确 JSON 头应放行（此处校验止步于 415 与否，创建成功另见 TC-012）：
     `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"ctjson"}'`
- **预期**：
  - 步骤 1 打印 `415`。
  - 步骤 2 打印 `201`（放行并创建）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 只读信息端点

### TC-009 GET /api/version

- **目的**：验证版本端点返回当前 conduct 版本（顶栏展示数据源，等价 `conduct version`）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s "$B/api/version"; echo`
- **预期**：
  - 返回 `{"version":"<VERSION>"}`（`<VERSION>` 不逐字比对）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-010 GET /api/engines 返回引擎能力表

- **目的**：验证引擎端点列出已注册引擎及其能力表（检查器引擎 / effort 下拉数据源）；能力表待实装的引擎以 `capability:null` 表达，不误报成 `allowsModel:false`。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. ```bash
     curl -s "$B/api/engines" | python3 -c '
     import sys, json
     d = json.load(sys.stdin)
     names = sorted(e["name"] for e in d)
     print("names=", names)
     cc = [e for e in d if e["name"]=="claude-code"][0]["capability"]
     print("cc_effortField=", cc["effortField"])
     print("cc_has_high=", "high" in cc["effortValues"])
     '
     ```
- **预期**：
  - `names=` 含 `claude-code`、`antigravity`、`qoder`（`codex` 已下线，不出现）。
  - `cc_effortField= effort`、`cc_has_high= True`（claude-code 的 effort 档位含 `high`）。
  - **说明**：这是唯一无 CLI 命令等价的只读信息性端点（「无独占能力」不变量的显式豁免）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-011 GET / 返回内嵌前端页面（SPA 外壳）

- **目的**：验证根路径由内嵌静态资源服务，返回 SPA 外壳 HTML（页面交互待浏览器走查验收，本用例只验证服务端把内嵌资源吐出来、返回 HTML）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s -o /dev/null -w "status=%{http_code} type=%{content_type}\n" "$B/"`
- **预期**：
  - `status=200`，`type=` 以 `text/html` 开头。
  - **说明**：`/` 服务内嵌 SPA 外壳（`index.html` + `js/` + `style.css`，随 go:embed 打进二进制）；SPA 页面交互待浏览器走查验收，本用例只断言「根路径由内嵌资源服务、返回 HTML」，不校验页面内容。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 工作流端点（CRUD，对齐 workflow 命令族）

### TC-012 GET /api/workflows 空 store；POST 创建骨架 → 201

- **目的**：验证列表端点在空 store 返回空数组；`POST /api/workflows`（body `{name}`）以最小骨架创建并返回 201 + 规范化定义（等价 `workflow create <name>`）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s "$B/api/workflows" | python3 -c 'import sys,json; print("workflows=", json.load(sys.stdin)["workflows"])'`
  2. ```bash
     curl -s -w "\n%{http_code}\n" -X POST "$B/api/workflows" \
       -H 'Content-Type: application/json' -d '{"name":"demo"}' \
       | python3 -c 'import sys; lines=sys.stdin.read().splitlines(); import json; d=json.loads(lines[0]); print("name=", d["name"], "nodes=", [n["id"] for n in d["nodes"]]); print("http=", lines[-1])'
     ```
  3. `curl -s "$B/api/workflows" | python3 -c 'import sys,json; print("names=", [w["name"] for w in json.load(sys.stdin)["workflows"]])'`
- **预期**：
  - 步骤 1 打印 `workflows= []`（空 store）。
  - 步骤 2 打印 `name= demo nodes= ['node-1']`（骨架含一个默认节点）与 `http= 201`。
  - 步骤 3 打印 `names= ['demo']`（列表现出新建工作流）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-013 POST 创建同名 → 409；GET 不存在 → 404

- **目的**：验证 `create` 不覆盖（同名 409）与查询不存在的工作流（404），错误码映射与 CLI 语义一致。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`；先建一个：`curl -s -o /dev/null -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"demo"}'`。
- **步骤**：
  1. `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"demo"}'`
  2. `curl -s -o /dev/null -w "%{http_code}\n" "$B/api/workflows/ghost"`
- **预期**：
  - 步骤 1 打印 `409`（同名已存在，不覆盖）。
  - 步骤 2 打印 `404`（工作流不存在）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-014 PUT 整体替换（合法定义）→ 200

- **目的**：验证 `PUT /api/workflows/{name}` 用完整新定义整体替换、校验通过后落盘（等价 `cat def.json | workflow edit <name>`）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`；先建 `demo`：`curl -s -o /dev/null -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"demo"}'`。
- **步骤**：
  1. 用一份两节点合法定义整体替换：
     ```bash
     curl -s -w "\n%{http_code}\n" -X PUT "$B/api/workflows/demo" \
       -H 'Content-Type: application/json' \
       -d '{"nodes":[{"id":"gen","displayName":"产出","engine":"claude-code","promptTemplate":"需求：{{sys.userPrompt}}"},{"id":"use","displayName":"引用","engine":"claude-code","promptTemplate":"上一步：{{gen}}"}]}' \
       | python3 -c 'import sys,json; lines=sys.stdin.read().splitlines(); d=json.loads(lines[0]); print("nodes=", [n["id"] for n in d["nodes"]]); print("http=", lines[-1])'
     ```
  2. 复查落盘生效：`curl -s "$B/api/workflows/demo" | python3 -c 'import sys,json; print("reloaded=", [n["id"] for n in json.load(sys.stdin)["nodes"]])'`
- **预期**：
  - 步骤 1 打印 `nodes= ['gen', 'use']` 与 `http= 200`。
  - 步骤 2 打印 `reloaded= ['gen', 'use']`（替换已落盘）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-015 PUT 校验不过 → 422 + 字段级 problems（不落盘）

- **目的**：验证保存时复用内核校验，不过则 422 返回逐条字段级错误（供编辑器点击定位），且**原定义不变**。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`；先建 `demo` 并 PUT 成合法两节点（同 TC-014 步骤 1，使原定义为 `gen›use`）。
- **步骤**：
  1. 提交一份引用不存在节点的非法定义：
     ```bash
     curl -s -w "\n%{http_code}\n" -X PUT "$B/api/workflows/demo" \
       -H 'Content-Type: application/json' \
       -d '{"nodes":[{"id":"a","displayName":"A","engine":"claude-code","promptTemplate":"引用 {{ghost}}"}]}' \
       | python3 -c 'import sys,json; lines=sys.stdin.read().splitlines(); d=json.loads(lines[0]); print("http=", lines[-1]); print("paths=", [p["path"] for p in d.get("problems",[])])'
     ```
  2. 复查原定义未被非法内容覆盖：`curl -s "$B/api/workflows/demo" | python3 -c 'import sys,json; print("still=", [n["id"] for n in json.load(sys.stdin)["nodes"]])'`
- **预期**：
  - 步骤 1 打印 `http= 422` 与 `paths= ['nodes[0].promptTemplate']`（引用不存在的节点 `{{ghost}}`）。
  - 步骤 2 打印 `still= ['gen', 'use']`——校验不过**不落盘**，原定义原样保留。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-016 POST rename 改名 → 200；DELETE 删除 → 204

- **目的**：验证改名端点（body `{newName}`，等价 `workflow rename`）与删除端点（等价 `workflow delete --yes`，UI 弹窗承担二次确认）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`；先建 `demo`：`curl -s -o /dev/null -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"demo"}'`。
- **步骤**：
  1. 改名 `demo` → `demo2`：
     `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/workflows/demo/rename" -H 'Content-Type: application/json' -d '{"newName":"demo2"}'`
  2. 旧名已不存在、新名存在：
     ```bash
     curl -s -o /dev/null -w "old=%{http_code} " "$B/api/workflows/demo"
     curl -s -o /dev/null -w "new=%{http_code}\n" "$B/api/workflows/demo2"
     ```
  3. 删除 `demo2`：`curl -s -o /dev/null -w "%{http_code}\n" -X DELETE "$B/api/workflows/demo2"`
  4. 删后不存在：`curl -s -o /dev/null -w "%{http_code}\n" "$B/api/workflows/demo2"`
- **预期**：
  - 步骤 1 打印 `200`（改名成功）。
  - 步骤 2 打印 `old=404 new=200`（旧名释放、新名就位）。
  - 步骤 3 打印 `204`（删除成功，无响应体）。
  - 步骤 4 打印 `404`（已删除）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 目录浏览端点（工作目录选择器 GET /api/fs）

### TC-017 GET /api/fs 列出某目录下的子目录

- **目的**：验证目录浏览端点列出**绝对路径**目录下的子目录（只列目录、含隐藏目录），并返回其父目录，供启动弹窗的工作目录选择器使用。
- **前置**：隔离 HOME 并造几个子目录 / 一个文件（验证「只列目录」）：
  ```bash
  WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"
  mkdir -p "$WORK/adir" "$WORK/.hidden"; : > "$WORK/afile"
  ```
  粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. ```bash
     curl -s "$B/api/fs?path=$WORK" | python3 -c '
     import sys, json
     d = json.load(sys.stdin)
     names = sorted(e["name"] for e in d["entries"])
     print("entries=", names)
     print("parent_nonempty=", bool(d["parent"]))
     '
     ```
- **预期**：
  - `entries=` 含 `adir` 与 `.hidden`（隐藏目录保留），**不含** `afile`（只列目录）。
  - `parent_nonempty= True`（返回了父目录，供「上一级」导航）。
  - **归一化说明**：`$WORK` 下可能还有 `.conduct` 等目录，只断言 `adir`/`.hidden` 出现、`afile` 不出现，不要求 entries 精确相等。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-018 GET /api/fs 相对路径 → 400；不存在 → 404

- **目的**：验证目录浏览端点拒绝相对路径（400）、如实报告目录不存在（404）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. 相对路径：`curl -s -o /dev/null -w "%{http_code}\n" "$B/api/fs?path=rel/dir"`
  2. 不存在的绝对路径：`curl -s -o /dev/null -w "%{http_code}\n" "$B/api/fs?path=$WORK/no-such-dir"`
- **预期**：
  - 步骤 1 打印 `400`（`path` 必须是绝对路径）。
  - 步骤 2 打印 `404`（目录不存在）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 运行端点

### TC-019 GET /api/runs 空列表；GET /api/runs/{id}/summary 不存在 → 404

- **目的**：验证无运行记录时运行列表返回空数组；某 run 的运行总结在其目录不存在时返回 404（与 running 期尚未生成的 404 同码）。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`。
- **步骤**：
  1. `curl -s "$B/api/runs" | python3 -c 'import sys,json; print("runs=", json.load(sys.stdin)["runs"])'`
  2. `curl -s -o /dev/null -w "%{http_code}\n" "$B/api/runs/demo-20260101-000000/summary"`
- **预期**：
  - 步骤 1 打印 `runs= []`。
  - 步骤 2 打印 `404`（无此 run / 尚无总结）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

---

## 启动运行端点（self-exec 发射器 POST /api/workflows/{name}/runs）

> 发射前服务端做**只读预检**（毫秒级），把「workflow 不存在 / 定义损坏 / 需求空 / 目录非法」在起子进程前拦成 4xx。TC-020 覆盖这些零成本预检失败路径；TC-021 用**假引擎**真发射一次（零 token），验证 202 + run id 回传 + run 记录真实产出。

### TC-020 启动运行的预检失败路径（404 / 400，零成本）

- **目的**：验证发射前预检：workflow 不存在 → 404；需求为空 → 400；`cwd` 不存在 → 400；`cwd` 为相对路径 → 400。四者都在起子进程前拦下，不烧引擎。
- **前置**：`WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"`；粘贴 `start_ui`；`start_ui`；建一个合法工作流 `hello`：
  ```bash
  curl -s -o /dev/null -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"hello"}'
  ```
- **步骤**：
  1. 不存在的 workflow：
     `curl -s -o /dev/null -w "%{http_code}\n" -X POST "$B/api/workflows/nope/runs" -H 'Content-Type: application/json' -d '{"userPrompt":"hi","cwd":"'"$WORK"'"}'`
  2. 空需求：
     `curl -s -w " http=%{http_code}\n" -X POST "$B/api/workflows/hello/runs" -H 'Content-Type: application/json' -d '{"userPrompt":"   ","cwd":"'"$WORK"'"}'`
  3. cwd 不存在：
     `curl -s -w " http=%{http_code}\n" -X POST "$B/api/workflows/hello/runs" -H 'Content-Type: application/json' -d '{"userPrompt":"hi","cwd":"'"$WORK"'/no-such"}'`
  4. cwd 相对路径：
     `curl -s -w " http=%{http_code}\n" -X POST "$B/api/workflows/hello/runs" -H 'Content-Type: application/json' -d '{"userPrompt":"hi","cwd":"rel/dir"}'`
- **预期**：
  - 步骤 1 打印 `404`（workflow 不存在）。
  - 步骤 2 打印形如 `{"error":"缺少用户需求：不能为空"} http=400`。
  - 步骤 3 打印形如 `{"error":"…/no-such: 工作目录不存在"} http=400`（路径子串归一化，不逐字比对）。
  - 步骤 4 打印形如 `{"error":"工作目录必须是绝对路径（以 / 开头）：rel/dir"} http=400`。
  - 说明：预检失败均**未产生 run 记录**（未起子进程，零 token）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。

### TC-021 启动运行 → 202 + run id，run 记录真实产出；对终态 run 终止 → 409（假引擎，零 token）

- **目的**：验证发射成功链路：`POST …/runs` self-exec 起子进程、202 返回 run id，且该次运行确实产出一条 run 记录（出现在 `GET /api/runs`）。再验证对**已终结**的 run 调终止端点返回 409（仅 running 可终止）。用一运行就失败的假引擎，秒级失败、零 token，但 run 记录照样真实生成。
- **前置**：隔离 HOME + 假引擎 + 一个合法工作流：
  ```bash
  WORK=$(mktemp -d); OLD_HOME="$HOME"; export HOME="$WORK"
  # 粘贴 broke_engine 与 start_ui 定义
  broke_engine            # 装假 claude 到 PATH，self-exec 子进程继承此 PATH
  start_ui                # 服务端进程带着坏 PATH 起，子进程亦然
  # 建一个最小合法工作流（骨架即可跑，引擎会失败但 run 记录照样落盘）
  curl -s -o /dev/null -X POST "$B/api/workflows" -H 'Content-Type: application/json' -d '{"name":"hello"}'
  ```
  > **关键顺序**：必须先 `broke_engine`（改 PATH）**再** `start_ui`——服务端 self-exec 出的 `conduct workflow run` 子进程继承服务端启动时的环境，坏 PATH 才能生效、引擎才会秒级失败。
- **步骤**：
  1. 真发射一次，取 run id：
     ```bash
     RID=$(curl -s -X POST "$B/api/workflows/hello/runs" \
       -H 'Content-Type: application/json' -d '{"userPrompt":"hi","cwd":"'"$WORK"'"}' \
       | python3 -c 'import sys,json; print(json.load(sys.stdin)["runId"])')
     echo "RID=$RID"
     ```
  2. 等子进程收尾，确认该 run 出现在列表且已终结：
     ```bash
     sleep 1
     curl -s "$B/api/runs" | python3 -c '
     import sys, json
     runs = json.load(sys.stdin)["runs"]
     hit = [r for r in runs if r["id"]=="'"$RID"'"]
     print("found=", bool(hit), "status=", hit[0]["status"] if hit else None, "workflow=", hit[0]["workflow"] if hit else None)
     '
     ```
  3. 对已终结的 run 调终止端点：
     `curl -s -w " http=%{http_code}\n" -X POST "$B/api/runs/$RID/stop" -H 'Content-Type: application/json' -d '{}'`
- **预期**：
  - 步骤 1：`RID=hello-YYYYMMDD-HHMMSS`（真实 run id，时间后缀忽略）——发射返回 202 且回传了 id（curl 未报错即取到 id）。
  - 步骤 2 打印 `found= True status= failed workflow= hello`——self-exec 真的产出了一条 run 记录（引擎故障故 `failed`；发射链路本身成功）。
  - 步骤 3 打印形如 `{"error":"运行 hello-… 当前状态为 failed，无可终止（仅 running 可终止）"} http=409`。
  - **说明**：终止**运行中**（running）的 run（→ 200 + `interrupted`）需真起引擎、留出终止窗口，属 💸，见 [workflow-running.md](./workflow-running.md)〈run stop〉的运行中用例；本端点的 running→200 路径与 CLI `run stop` 同源（同一 `run.StopProcess`）。
- **清理**：`stop_ui; export HOME="$OLD_HOME"; rm -rf "$WORK"`。
