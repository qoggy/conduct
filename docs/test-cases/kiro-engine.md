# Kiro 引擎测试用例

覆盖 `engine=kiro` 的真实 CLI 冒烟、model/effort、工具与图片、session 可见性、配置复用，以及 trace 的 nullable metadata。对应规格：见 [docs/specs/engines.md](../specs/engines.md)〈kiro〉与 [docs/specs/cli-runtime.md](../specs/cli-runtime.md)〈runs/ 落盘结构〉。

## 行为空间与覆盖

- 可选外部兼容性：真实 Kiro 的文本回答、stdin/cwd、model、五个 effort、工具、图片与项目配置 → TC-001、TC-002、TC-003。
- 数据流转：Kiro 最终回答进入节点 output；未知 usage/session 显式写 `null`；同 cwd 的 Kiro session 列表增加 → TC-001。
- 工具与图片：shell/read/write 修改隔离项目，本地 PNG 经 read 工具读取 → TC-002。
- 配置复用：Kiro 子进程使用当前用户已登录的 profile，session 对当前用户可见；项目 `.kiro/steering` 同时生效 → TC-001、TC-002。
- 错误路径：非法 model 由真实 Kiro 显式失败 → TC-003；effort 枚举与未知字段的保存期校验交由 `internal/workflow/validate_test.go` 和 [workflow-editing.md](./workflow-editing.md) TC-034 覆盖。
- 内容碰撞：回答、工具日志与 stderr 包含旧权限/上下文诊断关键词时仍按 assistant 结构成功、不根据自然语言误报，由 `internal/engine/kiro_test.go` 的确定性单测覆盖。
- 解析器的设置顺序、精确 argv、最后一个 assistant 标记、ANSI 清理、设置失败、非零退出只取 stderr（空 stderr 时只报退出码）、输出标记缺失，以及回答/工具日志包含诊断关键词时不误报，由 `internal/engine/kiro_test.go` 的确定性假二进制单测覆盖；真实冒烟不重复制造不稳定故障。

## 环境准备（每篇跑一次）

在仓库根执行：

```bash
make build
CONDUCT="$PWD/bin/conduct"
command -v kiro-cli
kiro-cli --version
```

TC-001、TC-002、TC-003 是**可选外部兼容性测试**，不属于日常确定性回归。它们会真实调用 Kiro、消耗 credits、创建当前用户可见的 session，并把 `chat.disableMarkdownRendering=true` 永久写入当前用户的 Kiro settings；这是 Conduct 的既定行为。仅在需要确认当前 Kiro CLI 版本仍与 Conduct 兼容时执行。

执行这些可选用例前，Kiro CLI 必须已用当前用户登录，且下面两条命令不得打开登录页面。再从模型列表中选择一个支持全部五档 effort 的模型写入 `KIRO_TEST_MODEL`：

```bash
: "${KIRO_TEST_MODEL:?select a model supporting low/medium/high/xhigh/max}"
kiro-cli chat --list-sessions --format json >/dev/null
kiro-cli chat --list-models --format json \
  | KIRO_TEST_MODEL="$KIRO_TEST_MODEL" python3 -c 'import json,os,sys; names={m["model_name"] for m in json.load(sys.stdin)["models"]}; assert os.environ["KIRO_TEST_MODEL"] in names'
```

每个会写 Conduct store 的用例都引用 [atomic-conduct-test.sh](./atomic-conduct-test.sh)：它在单个 shell 内注册 trap、把 Conduct 的 `HOME` 重定向到临时目录，并比较真实 `~/.conduct/workflows` / `runs` 前后快照。由于 Kiro 的登录态也依赖 `HOME`，真实 Kiro 用例会在临时 `PATH` 中安装一个同名薄包装器；包装器只为 `kiro-cli` 子进程恢复真实 `HOME`，其余参数和环境原样转交真实二进制。这样只隔离 Conduct 的 `~/.conduct`，不会隔离或复制当前用户的 Kiro profile。

## 可选外部兼容性测试

### TC-001 简单文本、session 可见与 nullable metadata

- **目的**：验证 Kiro 节点成功运行，最终回答进入 trace；Kiro session 在相同 cwd 可见；tokens/sessionId 明确为 JSON `null`。
- **前置**：已完成〈环境准备〉，当前用户的 Kiro CLI 已登录。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  REAL_KIRO_CLI=$(command -v kiro-cli)
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export REAL_KIRO_CLI
  cat > "$WORK/fakebin/kiro-cli" <<'SH'
  #!/usr/bin/env bash
  # Conduct 的 HOME 仅用于隔离 ~/.conduct；Kiro 仍复用当前用户的登录态、设置与 session。
  exec env HOME="$CONDUCT_TEST_REAL_HOME" "$REAL_KIRO_CLI" "$@"
  SH
  chmod +x "$WORK/fakebin/kiro-cli"
  PROJECT="$WORK/project"; mkdir -p "$PROJECT"
  before=$(cd "$PROJECT" && kiro-cli chat --list-sessions --format json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(sum(len(e.get("sessions", [])) for e in d))')
  printf '%s' '{"nodes":[{"id":"START"},{"id":"ask","displayName":"Kiro text","engine":"kiro","promptTemplate":"只输出字符串 KIRO-SMOKE-OK"},{"id":"END"}],"edges":[{"from":"START","to":"ask"},{"from":"ask","to":"END"}]}' | "$CONDUCT" workflow create kiro-text --definition >/dev/null
  "$CONDUCT" workflow run kiro-text "smoke" --cwd "$PROJECT"
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print(json.load(sys.stdin)[0]["id"])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"][0]; print("success=",t["success"],"has_output=","KIRO-SMOKE-OK" in t["output"],"tokens=",t["tokens"],"sessionId=",t["sessionId"],"keys=",("tokens" in t and "sessionId" in t))'
  after=$(cd "$PROJECT" && kiro-cli chat --list-sessions --format json | python3 -c 'import sys,json; d=json.load(sys.stdin); print(sum(len(e.get("sessions", [])) for e in d))')
  echo "sessions_before=$before sessions_after=$after"; test "$after" -gt "$before"
  BASH
  ```

- **预期**：workflow run 退出 `0`；trace 打印 `success= True has_output= True tokens= None sessionId= None keys= True`；`sessions_after` 大于 `sessions_before`。run 的人读完成事件不含 `tokens=`。
- **清理**：脚本 trap 恢复环境、删除临时 Conduct store 与项目，并验证真实 Conduct store 零差异；当前用户的 Kiro profile 保留新 session 和 `chat.disableMarkdownRendering=true`，这是被测副作用。

### TC-002 shell/read/write、图片与项目配置

- **目的**：验证 `--trust-all-tools` 允许 Kiro 在隔离 cwd 使用 shell/read/write；能识别 prompt 中本地 PNG 的绝对路径与可辨识文字；项目 `.kiro/steering` 生效，同时复用当前用户的 Kiro profile。
- **前置**：已完成〈环境准备〉，当前用户的 Kiro CLI 已登录。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  REAL_KIRO_CLI=$(command -v kiro-cli)
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export REAL_KIRO_CLI
  cat > "$WORK/fakebin/kiro-cli" <<'SH'
  #!/usr/bin/env bash
  # Conduct 的 HOME 仅用于隔离 ~/.conduct；Kiro 仍复用当前用户的登录态、设置与 session。
  exec env HOME="$CONDUCT_TEST_REAL_HOME" "$REAL_KIRO_CLI" "$@"
  SH
  chmod +x "$WORK/fakebin/kiro-cli"
  PROJECT="$WORK/project"; mkdir -p "$PROJECT/.kiro/steering"
  printf '%s\n' '回答末尾必须输出 PROJECT-KIRO-SENTINEL。' > "$PROJECT/.kiro/steering/conduct-smoke.md"
  IMAGE_PATH="$PROJECT/kiro-7319.png" python3 <<'PY'
  import os, struct, zlib
  glyphs = {"K":["10001","10010","10100","11000","10100","10010","10001"],"I":["11111","00100","00100","00100","00100","00100","11111"],"R":["11110","10001","10001","11110","10100","10010","10001"],"O":["01110","10001","10001","10001","10001","10001","01110"],"-":["00000","00000","00000","11111","00000","00000","00000"],"7":["11111","00001","00010","00100","01000","01000","01000"],"3":["11110","00001","00001","01110","00001","00001","11110"],"1":["00100","01100","00100","00100","00100","00100","01110"],"9":["01110","10001","10001","01111","00001","00010","11100"]}
  text, scale, margin = "KIRO-7319", 8, 12
  width, height = margin*2 + (len(text)*6-1)*scale, margin*2 + 7*scale
  pixels = [[255]*width for _ in range(height)]
  for index, char in enumerate(text):
      for row, bits in enumerate(glyphs[char]):
          for column, bit in enumerate(bits):
              if bit == "1":
                  for y in range(margin+row*scale, margin+(row+1)*scale):
                      for x in range(margin+(index*6+column)*scale, margin+(index*6+column+1)*scale): pixels[y][x] = 0
  raw = b"".join(b"\x00" + bytes(row) for row in pixels)
  chunk = lambda kind,data: struct.pack(">I",len(data))+kind+data+struct.pack(">I",zlib.crc32(kind+data)&0xffffffff)
  png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR",struct.pack(">IIBBBBB",width,height,8,0,0,0,0)) + chunk(b"IDAT",zlib.compress(raw)) + chunk(b"IEND",b"")
  open(os.environ["IMAGE_PATH"],"wb").write(png)
  PY
  IMAGE_PATH="$PROJECT/kiro-7319.png" python3 -c 'import json,os; p=os.environ["IMAGE_PATH"]; prompt=f"必须依次使用 shell 执行 pwd、read 读取本地绝对路径 {p} 的 PNG 并识别其中的文字、write 创建 result.txt（内容精确为 KIRO-WRITE-OK）；最后回答必须包含图片文字和项目配置 sentinel。"; print(json.dumps({"nodes":[{"id":"START"},{"id":"tools","displayName":"Kiro tools","engine":"kiro","promptTemplate":prompt},{"id":"END"}],"edges":[{"from":"START","to":"tools"},{"from":"tools","to":"END"}]}))' | "$CONDUCT" workflow create kiro-tools --definition >/dev/null
  "$CONDUCT" workflow run kiro-tools "smoke" --cwd "$PROJECT"
  test "$(cat "$PROJECT/result.txt")" = KIRO-WRITE-OK
  RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print(json.load(sys.stdin)[0]["id"])')
  "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; o=json.load(sys.stdin)["trace"][0]["output"]; print("image=", "KIRO-7319" in o, "project=", "PROJECT-KIRO-SENTINEL" in o)'
  BASH
  ```

- **预期**：退出码 `0`；`result.txt` 内容精确为 `KIRO-WRITE-OK`；最后打印 `image= True project= True`。图片文字证明 Kiro 识别了 prompt 中的本地绝对路径 PNG；项目 sentinel 证明项目 steering 生效。
- **清理**：脚本 trap 删除隔离 Conduct store 与项目并验证真实 Conduct store 零差异；当前用户的 Kiro profile 保留 session 与 `chat.disableMarkdownRendering=true` 设置副作用。

### TC-003 显式 model、五个 effort 与非法 model

- **目的**：验证显式 `model=$KIRO_TEST_MODEL` 和五个合法 effort 均能启动；非法 model 由 Kiro 显式失败。
- **前置**：同 TC-001。
- **步骤**：完整复制执行：

  ```bash
  bash <<'BASH'
  set -euo pipefail
  : "${KIRO_TEST_MODEL:?set KIRO_TEST_MODEL}"
  REAL_KIRO_CLI=$(command -v kiro-cli)
  source docs/test-cases/atomic-conduct-test.sh
  conduct_test_setup
  export REAL_KIRO_CLI
  cat > "$WORK/fakebin/kiro-cli" <<'SH'
  #!/usr/bin/env bash
  # Conduct 的 HOME 仅用于隔离 ~/.conduct；Kiro 仍复用当前用户的登录态、设置与 session。
  exec env HOME="$CONDUCT_TEST_REAL_HOME" "$REAL_KIRO_CLI" "$@"
  SH
  chmod +x "$WORK/fakebin/kiro-cli"
  PROJECT="$WORK/project"; mkdir -p "$PROJECT"
  for effort in low medium high xhigh max; do
    name="kiro-$effort"
    EFFORT="$effort" MODEL="$KIRO_TEST_MODEL" python3 -c 'import json,os; e=os.environ["EFFORT"]; print(json.dumps({"nodes":[{"id":"START"},{"id":"ask","displayName":e,"engine":"kiro","engineConfig":{"model":os.environ["MODEL"],"effort":e},"promptTemplate":"只输出 OK"},{"id":"END"}],"edges":[{"from":"START","to":"ask"},{"from":"ask","to":"END"}]}))' | "$CONDUCT" workflow create "$name" --definition >/dev/null
    "$CONDUCT" workflow run "$name" smoke --cwd "$PROJECT" >/dev/null
    RID=$("$CONDUCT" run list --json | python3 -c 'import sys,json; print(json.load(sys.stdin)[0]["id"])')
    "$CONDUCT" run show "$RID" --json --trace | python3 -c 'import sys,json; t=json.load(sys.stdin)["trace"][0]; assert t["success"] is True'
    echo "effort=$effort ok"
  done
  printf '%s' '{"nodes":[{"id":"START"},{"id":"ask","displayName":"bad model","engine":"kiro","engineConfig":{"model":"conduct-model-does-not-exist"},"promptTemplate":"hello"},{"id":"END"}],"edges":[{"from":"START","to":"ask"},{"from":"ask","to":"END"}]}' | "$CONDUCT" workflow create kiro-bad-model --definition >/dev/null
  set +e
  "$CONDUCT" workflow run kiro-bad-model smoke --cwd "$PROJECT" >"$WORK/bad.out" 2>"$WORK/bad.err"; rc=$?
  set -e
  echo "bad_model_exit=$rc"; test "$rc" -eq 1; grep -i 'model' "$WORK/bad.err"
  BASH
  ```
- **预期**：`KIRO_TEST_MODEL` 下五个合法 effort 各自退出 `0`；非法 model 的 workflow 保存成功（model 是开放集合），运行退出 `1`，stderr 含 Kiro 的 model 不存在诊断。若模型能力变化导致任一档被 Kiro 拒绝，本用例失败；重新查询模型列表并把 `KIRO_TEST_MODEL` 设置为支持五档的模型后从头执行。
- **清理**：atomic shell 的 trap 恢复环境、删除临时 Conduct store 并验证真实 Conduct store 零差异；当前用户的 Kiro profile 保留 session/设置。
