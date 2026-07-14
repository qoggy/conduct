// 运行详情页（#/runs/:id）：run show / run show --trace 的镜像。三态 running / completed / failed
// （+ interrupted 派生）。左栏 DAG 进度画布（节点颜色即状态），右栏节点详情（点开延迟渲染全文）；
// 下方运行总结 marked 渲染、冻结定义折叠、running 可终止 / failed·interrupted 可恢复。
// 全页不显示、不提及任何内部文件路径（cwd 是用户自己传的运行参数，照常展示）。

import { h, mount, copyText, copyIcon } from "../dom.js";
import { api } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTimeSec, fmtDurationMs, durationBetween, elapsedSince, fmtTokens } from "../format.js";
import { engineIconEl } from "../engines.js";
import { highlightHTML } from "../highlight.js";
import { confirmModal } from "../modal.js";
import { loadInto } from "./common.js";
import { isAgent, edgeKey, NODE_ID_START, NODE_ID_END } from "../graph.js";
import { svg } from "../svg.js";
import { layoutPositions, edgePathThrough, arrowMarkers, anchorEl, nodeEl, ARROW_ID } from "../dag-layout.js";

export function renderRunDetailPage(outlet, id) {
  return loadInto(
    outlet,
    () => api.getRun(id, true), // trace=1：节点 input/output 全文
    (detail) => view(outlet, detail),
  );
}

function reload(outlet, id) {
  renderRunDetailPage(outlet, id);
}

function view(outlet, d) {
  const status = d.status;
  const running = status === "running";
  const failed = status === "failed";
  const completed = status === "completed";
  const interrupted = status === "interrupted";

  // 冻结快照的定义主体（含 START/END）；进度分母 N = agent 节点数（读时由快照算，对齐后端 recordNodeCount）。
  const snapDef = (d.workflowSnapshot && d.workflowSnapshot.definition) || { nodes: [], edges: [] };
  const nodeCount = (snapDef.nodes || []).filter((n) => isAgent(n.id)).length;
  const progress = d.progress || 0;

  // trace 按 nodeId 归并：同一 nodeId 可有多条（resume 保留失败行 + 补跑行），以最后一条为准。
  const trace = d.trace || [];
  const lastByNode = new Map();
  trace.forEach((e) => lastByNode.set(e.nodeId, e));
  const done = new Set();
  lastByNode.forEach((e, id) => {
    if (e.success) done.add(id);
  });

  const page = h("div", { class: "page" });

  // ---- 概要头 ----
  const row1 = h(
    "div",
    { class: "row1" },
    h("span", { class: "rid-lg" }, d.id),
    statusBadgeLarge(status, running),
    running ? h("button", { class: "btn", onClick: () => reload(outlet, d.id) }, i18n.refresh) : null,
    h("span", { class: "grow" }),
    h("span", { class: "pid" }, "pid " + d.pid),
    running ? h("button", { class: "btn btn-stop", onClick: () => openStop(outlet, d) }, i18n.stopBtn) : null,
    failed || interrupted ? h("button", { class: "btn btn-ink", onClick: () => openResume(outlet, d) }, i18n.resumeBtn) : null,
  );

  const wfName = h("span", { class: "mono link", style: { fontSize: "12.5px" } }, d.workflow);
  wfName.addEventListener("click", () => navigate(`/workflows/${encodeURIComponent(d.workflow)}`));

  const card = h(
    "div",
    { class: "card" },
    row1,
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailWorkflow), wfName),
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailCwd), h("span", { class: "mono", style: { fontSize: "12px" } }, d.cwd)),
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailProgress), h("span", { class: "mono", style: { fontSize: "12.5px" } }, i18n.progressNodesTpl(progress, nodeCount))),
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailTime), h("span", { class: "muted" }, timeLine(d))),
    running ? runningProgress(progress, nodeCount) : null,
    interrupted ? h("div", { class: "fnote" }, i18n.interruptedNote) : null,
  );
  page.appendChild(card);

  // 需求：可能是大 PRD，独立成块、Prism markdown 渲染（自带转义、无 XSS）、可复制、默认折叠。
  page.appendChild(h("div", { class: "promptblk" }, mdBlock(i18n.detailPrompt, d.userPrompt)));

  // ---- failed：error 全文置顶（失败节点取后端 record.failedNodeId＝首个失败节点/根因，与 error 文案一致） ----
  if (failed && d.error) {
    const failureTitle = d.failedNodeId ? i18n.failedAtNodeTpl(d.failedNodeId) : i18n.failedPrefix;
    page.appendChild(h("div", { class: "errpanel" }, h("h5", {}, failureTitle), h("p", {}, d.error)));
  }

  // ---- DAG 进度画布 + 右栏节点详情（未选中节点时详情列隐藏、画布在整行内居中） ----
  const canvasCol = h("div", { class: "canvascol" });
  const detailCol = h("div", { class: "insp insp-run" });
  const body = h("div", { class: "body2" }, canvasCol, detailCol);
  const state = { selId: null };
  const syncLayout = () => {
    const has = !!state.selId;
    detailCol.style.display = has ? "" : "none";
    body.classList.toggle("body2-solo", !has); // 无选中：隐藏详情、画布居中
  };
  const renderCanvas = () => {
    // 选中回调：点节点选中它；再次点同一节点、或点画布空白（id=null）→ 取消选中，回到居中无面板态。
    mount(canvasCol, buildRunCanvas(snapDef, lastByNode, done, running, completed, state.selId, (id) => {
      state.selId = state.selId === id ? null : id;
      renderCanvas();
      renderDetail();
    }));
  };
  const renderDetail = () => {
    syncLayout();
    if (!state.selId) return; // 未选中：详情列已隐藏，不渲染占位
    const node = (snapDef.nodes || []).find((n) => n.id === state.selId) || { id: state.selId };
    // 选中才构建节点详情 DOM；其中输入 / 输出体进一步延迟到展开对应折叠块才渲染（trace 单行可达 MB 级，
    // 延迟渲染是唯一允许的性能手段，展开时渲染完整全文、绝不截断）。
    mount(detailCol, nodeDetail(node, lastByNode.get(state.selId) || null));
  };
  renderCanvas();
  renderDetail();
  page.appendChild(body);

  // ---- 运行总结（终态渲染，running 提示待生成） ----
  if (running) {
    page.appendChild(h("div", { class: "fnote" }, i18n.summaryPending));
  } else if (completed || interrupted || failed) {
    const panel = summaryPanel();
    page.appendChild(panel.element);
    // 异步拉总结 markdown 并 marked 渲染；仅 404（未生成）时移除面板，其余错误就地显示、不静默。
    api
      .getSummary(d.id)
      .then((md) => panel.fill(md))
      .catch((err) => {
        if (err && err.status === 404) panel.remove();
        else panel.fillError(err.message);
      });
  }

  // ---- 冻结定义折叠区（完整记录 JSON） ----
  page.appendChild(frozenDefView(d.workflowSnapshot));

  return page;
}

// ---- DAG 进度画布 ----
function buildRunCanvas(snapDef, lastByNode, done, running, completed, selId, onSelect) {
  const layout = layoutPositions(snapDef);
  const positions = layout.positions;

  const routeOf = layout.routeOf;
  const edgeEls = [];
  for (const edge of snapDef.edges || []) {
    const pts = routeOf.get(edgeKey(edge));
    if (!pts) continue;
    edgeEls.push(svg("path", { class: "edge", d: edgePathThrough(pts), "marker-end": `url(#${ARROW_ID})` }));
  }

  const markerEls = [];
  // START 越过即转绿（t0 完成）；END 仅整体完成时转绿。
  if (positions.has(NODE_ID_START)) markerEls.push(anchorEl(NODE_ID_START, positions.get(NODE_ID_START), { ok: true }));
  if (positions.has(NODE_ID_END)) markerEls.push(anchorEl(NODE_ID_END, positions.get(NODE_ID_END), { ok: completed }));

  const nodeEls = [];
  for (const node of snapDef.nodes || []) {
    if (!isAgent(node.id)) continue;
    const pos = positions.get(node.id);
    if (!pos) continue;
    const st = nodeStateOf(node.id, lastByNode, done, snapDef, running);
    const g = nodeEl(node, pos, {
      selected: node.id === selId,
      stateClass: st.cls,
      statusClass: st.statusClass,
      statusLabel: st.label,
      faint: st.faint,
    });
    // 节点点击自吞（stopPropagation），不冒泡到 svg 背景，否则会被背景的"取消选中"抵消。
    g.addEventListener("click", (e) => {
      e.stopPropagation();
      onSelect(node.id);
    });
    nodeEls.push(g);
  }

  // 自然像素尺寸 + CSS max-width:100%/height:auto：真实尺寸呈现、超宽才等比缩小（与编辑页画布一致）。
  const canvas = svg(
    "svg",
    { class: "dagcanvas runcanvas", width: layout.width, height: layout.height, viewBox: `0 0 ${layout.width} ${layout.height}`, preserveAspectRatio: "xMidYMid meet" },
    arrowMarkers(),
    ...edgeEls,
    ...markerEls,
    ...nodeEls,
  );
  // 点画布空白（含 START/END 锚点、边——它们不吞事件）→ 取消选中。
  canvas.addEventListener("click", () => onSelect(null));
  return canvas;
}

// nodeStateOf 推断某 agent 节点的展示态：成功=绿、失败=红、运行中=蓝、待运行=灰（颜色即状态）。
// 无 trace 记录时：run 存活且全部前驱已完成（前沿）→ 运行中；否则 → 待运行。耗时只在成功/失败标注。
function nodeStateOf(id, lastByNode, done, snapDef, running) {
  const last = lastByNode.get(id);
  if (last) {
    if (last.success) return { cls: "nb-ok", statusClass: "nst-ok", label: fmtDurationMs(last.durationMs) };
    return { cls: "nb-fail", statusClass: "nst-fail", label: fmtDurationMs(last.durationMs) };
  }
  if (running && predsSatisfied(id, snapDef, done)) return { cls: "nb-run", statusClass: "nst-run", label: "" };
  return { cls: "nb-wait", statusClass: "", label: "", faint: true };
}

// predsSatisfied：某节点的全部前驱是否都已就绪（START 恒就绪；agent 前驱须已成功）。
function predsSatisfied(id, snapDef, done) {
  const preds = (snapDef.edges || []).filter((e) => e.to === id).map((e) => e.from);
  return preds.every((p) => p === NODE_ID_START || done.has(p));
}

// ---- 右栏节点详情（点画布节点才展开，延迟渲染全文） ----
function nodeDetail(node, entry) {
  const displayName = entry ? entry.displayName : node.displayName || node.id;
  const engine = entry ? entry.engine : node.engine;
  const cfg = entry ? entry.engineConfig : node.engineConfig;

  const head = h("div", { class: "insphead" }, h("span", { class: "idchip" }, node.id), h("span", { class: "itt" }, displayName));

  // engine · model · [effort] · [tokens] · [耗时]
  const parts = [engine, cfg && cfg.model ? cfg.model : i18n.engineDefaultModel];
  if (cfg && cfg.effort) parts.push("effort " + cfg.effort);
  if (cfg && cfg.reasoningEffort) parts.push("reasoningEffort " + cfg.reasoningEffort);
  if (entry) {
    if (entry.tokens) parts.push(fmtTokens(entry.tokens) + " tok");
    parts.push(fmtDurationMs(entry.durationMs));
  }
  const stats = h("div", { class: "dkv" }, engineIconEl(engine), h("span", {}, parts.join(" · ")));

  if (!entry) {
    // 尚无执行记录（待运行 / 运行中）：只展示配置，如实标注。
    return h("div", {}, head, stats, h("div", { class: "fnote" }, i18n.nodeNoTrace));
  }

  const outLabel = entry.success ? i18n.detailOutput : i18n.detailError;
  const outText = entry.success ? entry.output : entry.error || "";
  const outCls = entry.success ? "ed ed-att" : "ed ed-att ed-err";
  return h(
    "div",
    {},
    head,
    stats,
    sessionRow(entry),
    mdBlock(i18n.detailInput, entry.input),
    mdBlock(outLabel, outText, { bodyCls: outCls }),
  );
}

function statusBadgeLarge(status, running) {
  if (running) return h("span", { class: "badge b-running" }, h("span", { class: "pulse" }), "running");
  return h("span", { class: "badge b-" + status }, status);
}

function timeLine(d) {
  const start = fmtTimeSec(d.startedAt);
  if (d.endedAt) {
    const dur = durationBetween(d.startedAt, d.endedAt);
    const durText = dur !== null ? `${i18n.durationLabel} ${fmtDurationMs(dur)} · ` : "";
    return `${durText}${start} → ${fmtTimeSec(d.endedAt)}`;
  }
  const el = elapsedSince(d.startedAt);
  return `${start} ${i18n.sinceLabel}` + (el !== null ? ` · ${i18n.elapsedLabel} ${fmtDurationMs(el)}` : "");
}

function runningProgress(k, n) {
  const pct = n > 0 ? Math.min(100, (k / n) * 100) : 0;
  return h("div", { style: { marginTop: "10px" } }, h("div", { class: "prog" }, h("span", { class: "prog-i", style: { width: pct.toFixed(1) + "%" } })));
}

// sessionReplayCmd 镜像 CLI 的 sessionReplayLine（internal/cli/run_show.go）：按引擎给出「凭会话 id
// 回放本节点」的命令；未知引擎无对应命令，返回空串（调用方退化为只显 id）。
function sessionReplayCmd(engine, sessionId) {
  switch (engine) {
    case "claude-code":
      return "claude -r " + sessionId;
    case "codex":
      return "codex resume " + sessionId;
    case "qoder":
      return "qodercli -r " + sessionId;
    case "antigravity":
      return "agy --conversation " + sessionId;
    default:
      return "";
  }
}

// sessionRow 是节点详情里的会话信息：会话 id 一行、该引擎回放命令另起一行（各自可复制）。
// 引擎未回报会话 id 时整块不渲染；未知引擎无回放命令时只出会话 id 一行。
// 返回两行的数组（h/mount 会扁平化数组子节点）——末行仍是 .dkv，紧邻其后的输入 .edbar 照旧紧贴。
function sessionRow(entry) {
  if (!entry.sessionId) return null;
  const idLine = h(
    "div",
    { class: "dkv" },
    h("span", { class: "session" }, `${i18n.detailSession} ${entry.sessionId}`),
    copyBtn((e) => {
      e.stopPropagation();
      copyText(entry.sessionId);
    }, "cpd"),
  );
  const cmd = sessionReplayCmd(entry.engine, entry.sessionId);
  if (!cmd) return [idLine];
  const replayLine = h(
    "div",
    { class: "dkv" },
    h("span", { class: "session" }, `${i18n.detailReplay}：${cmd}`),
    copyBtn((e) => {
      e.stopPropagation();
      copyText(cmd);
    }, "cpd"),
  );
  return [idLine, replayLine];
}

// mdBlock 是「深色头条（折叠开关）+ markdown 体」的可折叠只读块：点头条展开 / 收起，caret 指示态。
// 默认折叠（collapsed=true）；展开才把全文渲染进 DOM——trace 单行可达 MB 级，延迟到展开是性能手段。
// 头条含 字段名 + md 标签 + 复制（复制自吞点击、不触发折叠）。返回 [头条, 体容器] 数组，由 h / mount
// 扁平化插入，省一层包裹 div，保持 .dkv + .edbar 等相邻间距规则不变。
function mdBlock(label, text, { bodyCls = "ed ed-att", collapsed = true } = {}) {
  const bodyHolder = h("div");
  let open = !collapsed;
  const caret = h("span", { class: "edcaret" }, open ? "▾" : "▸");
  const bar = h(
    "div",
    { class: "edbar edbar-fold" + (open ? " is-open" : "") },
    caret,
    h("span", { class: "edname" }, label),
    h("span", { class: "edtag" }, "md"),
    h("span", { class: "grow" }),
    copyBtn((e) => {
      e.stopPropagation();
      copyText(text);
    }, "cpd"),
  );
  const render = () => {
    if (open) mount(bodyHolder, h("div", { class: bodyCls, html: highlightHTML(text, "markdown") }));
    else mount(bodyHolder);
  };
  bar.addEventListener("click", () => {
    open = !open;
    caret.textContent = open ? "▾" : "▸";
    bar.classList.toggle("is-open", open);
    render();
  });
  render();
  return [bar, bodyHolder];
}

// ---- 运行总结面板（marked 渲染） ----
function summaryPanel() {
  const bodyHolder = h("div");
  const copyBtnEl = h("button", { class: "ghost" }, copyIcon(), " " + i18n.copyAll);
  let rawMarkdown = "";
  // 复制自吞点击，避免冒泡触发头条折叠。
  copyBtnEl.addEventListener("click", (e) => {
    e.stopPropagation();
    copyText(rawMarkdown);
  });
  // 总结默认展开（结论落地即读）；头条可点折叠，与需求 / 输入 / 输出的折叠交互一致。
  let open = true;
  const caret = h("span", { class: "foldcaret" }, "▾");
  const headEl = h("div", { class: "panelhead panelhead-fold" }, caret, h("h5", {}, i18n.summaryTitle), h("span", { class: "grow" }), copyBtnEl);
  headEl.addEventListener("click", () => {
    open = !open;
    caret.textContent = open ? "▾" : "▸";
    bodyHolder.style.display = open ? "" : "none";
  });
  const element = h("div", { class: "panel" }, headEl, bodyHolder);
  return {
    element,
    fill(md) {
      rawMarkdown = md; // 复制全文取原始总结（含 <output> 分隔标签），非展示用的预处理版
      // marked 输出未消毒，总结含半可信引擎产物 → 注入 DOM 前必过 DOMPurify（strip script/on*/javascript:）。
      // 兜底 fail-safe：任一库缺失时退回纯文本转义，而非直接注入原始 HTML。
      // 展示前两步收拾总结里的 XML：preprocessSummary 把 conduct 自己的 <output> 分隔标签换成小标题、内层产物
      // 按 markdown 渲染；summaryRenderer 再把产物内部残留的裸标签（引擎输出里的 <user_prompt> 等）转义成字面
      // 文本并保留换行。breaks:true 让总结里的单换行（头部 **字段** 行、标签块内）呈现为换行而非塌成空格。
      const rendered = globalThis.marked ? globalThis.marked.parse(preprocessSummary(md), { renderer: summaryRenderer(), breaks: true }) : null;
      const html = rendered !== null && globalThis.DOMPurify ? globalThis.DOMPurify.sanitize(rendered) : escapeHTML(md);
      mount(bodyHolder, h("div", { class: "md-body", html }));
    },
    remove() {
      if (element.parentNode) element.parentNode.removeChild(element);
    },
    fillError(message) {
      mount(bodyHolder, h("div", { class: "ferr" }, message));
    },
  };
}

// ---- 冻结定义折叠区（完整记录：name / 时间戳 + definition{nodes,edges}） ----
function frozenDefView(snapshot) {
  const json = snapshot ? JSON.stringify(snapshot, null, 2) : "";
  const bodyHolder = h("div");
  let open = false;
  const caret = h("span", {}, "▸ " + i18n.frozenDef);
  const copyBtnEl = h("button", { class: "ghost", style: { display: "none" } }, copyIcon(), " " + i18n.copyJSON);
  copyBtnEl.addEventListener("click", (e) => {
    e.stopPropagation();
    copyText(json);
  });
  const fold = h(
    "div",
    { class: "fold" },
    caret,
    h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.frozenNote)),
    h("span", { class: "grow" }),
    copyBtnEl,
  );
  fold.addEventListener("click", () => {
    open = !open;
    caret.textContent = (open ? "▾ " : "▸ ") + i18n.frozenDef;
    copyBtnEl.style.display = open ? "inline-flex" : "none";
    if (open) {
      mount(
        bodyHolder,
        h(
          "div",
          { class: "jsonwrap", style: { marginTop: "8px" } },
          h("div", { class: "jsonbody" }, h("pre", { class: "pre", html: highlightHTML(json, "json") })),
        ),
      );
    } else {
      mount(bodyHolder);
    }
  });
  return h("div", {}, fold, bodyHolder);
}

// ---- 终止运行 ----
function openStop(outlet, d) {
  confirmModal({
    title: i18n.dlgStopTitleTpl(d.id),
    danger: true,
    confirmLabel: i18n.stopConfirm,
    body: [
      h("p", { style: { margin: "0" } }, i18n.stopBodyTpl(d.pid, d.id)),
      h("p", { class: "muted", style: { margin: "6px 0 0" } }, i18n.stopNote),
    ],
    onConfirm: async () => {
      await api.stopRun(d.id);
      reload(outlet, d.id);
    },
  });
}

// ---- 恢复（从中断处续跑） ----
function openResume(outlet, d) {
  confirmModal({
    title: i18n.dlgResumeTitleTpl(d.id),
    confirmLabel: i18n.resumeConfirm,
    body: [
      h("p", { style: { margin: "0" } }, i18n.resumeBodyTpl(d.id)),
      h("p", { class: "muted", style: { margin: "6px 0 0" } }, i18n.resumeNote),
    ],
    onConfirm: async () => {
      await api.resumeRun(d.id); // 202 返回 {runId}（即原 id）；续跑后该 run 转 running，刷新即见推进
      reload(outlet, d.id);
    },
  });
}

// ---- 小工具 ----
function copyBtn(handler, cls) {
  const btn = h("button", { class: cls }, copyIcon());
  btn.addEventListener("click", handler);
  return btn;
}

function escapeHTML(s) {
  return s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" })[c]);
}

// preprocessSummary 把总结里 conduct 自己的 `<output node="X" name="Y">` … `</output>` 分隔标签
// （见 internal/run/summary.go：产物区每个 agent 节点产物的稳健分隔符）换成 markdown 小标题，内层产物即按
// 正常 markdown 渲染；闭合标签删除。产物内部残留的别的裸标签（引擎输出里的 <user_prompt> 等）不在此处理，
// 交给 summaryRenderer 转义。node id 受 ^[A-Za-z_][A-Za-z0-9_-]* 约束、name 为 Go %q 引号串，常规无内嵌
// 引号，故整行精确匹配即可；name 含引号等罕见情形匹配不上、原样落到 summaryRenderer 按字面显示（优雅降级）。
function preprocessSummary(md) {
  return md
    .replace(/^<output node="([^"]*)" name="([^"]*)">[ \t]*$/gm, (_, id, name) => `### ${name} · \`${id}\``)
    .replace(/^<\/output>[ \t]*$/gm, "");
}

// summaryRenderer 惰性构建并缓存 marked 渲染器（模块加载时 marked 未必就绪，故不在模块顶层建）：覆盖 html
// 方法把裸 HTML/XML 标签转义为字面文本（marked v12 以字符串传入；兼容 token 对象签名以防升级），并把标签块内
// 的换行转成 <br>——marked 会把 `<tag>` 起头的多行内容整体捕成一个 html token，其换行在 HTML 里本会塌成空格，
// 转 <br> 方能保留原有行结构（配合 breaks:true 一并解决段落内软换行与标签块内换行两种粘连）。
let cachedSummaryRenderer = null;
function summaryRenderer() {
  if (!cachedSummaryRenderer) {
    cachedSummaryRenderer = new globalThis.marked.Renderer();
    cachedSummaryRenderer.html = (arg) => {
      const s = typeof arg === "string" ? arg : (arg && (arg.text ?? arg.raw)) || "";
      return escapeHTML(s).replace(/\n/g, "<br>\n");
    };
  }
  return cachedSummaryRenderer;
}
