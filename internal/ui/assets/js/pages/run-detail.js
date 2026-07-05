// 运行详情页（#/runs/:id）：run show / run show --trace 的镜像。三态 running / completed / failed
// （+ interrupted 派生），逐步可展开全文、运行总结 marked 渲染、冻结定义、running 可终止。
// 全页不显示、不提及任何内部文件路径（cwd 是用户自己传的运行参数，照常展示）。

import { h, mount, copyText, copyIcon } from "../dom.js";
import { api } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTime, fmtTimeSec, fmtDurationMs, durationBetween, elapsedSince, fmtTokens } from "../format.js";
import { engineIconEl, chipStyle } from "../engines.js";
import { highlightHTML } from "../highlight.js";
import { confirmModal } from "../modal.js";
import { loadInto } from "./common.js";

export function renderRunDetailPage(outlet, id) {
  return loadInto(
    outlet,
    () => api.getRun(id, true), // trace=1：逐步全文
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

  // 节点 id → 色板序号（按快照 nodes 顺序，与列表/编辑器同一套配色）。
  const colorIndex = {};
  const snapshot = d.workflowSnapshot;
  if (snapshot && snapshot.nodes) {
    snapshot.nodes.forEach((n, i) => {
      colorIndex[n.id] = i;
    });
  }

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
  );

  const wfName = h("span", { class: "mono link", style: { fontSize: "12.5px" } }, d.workflow);
  wfName.addEventListener("click", () => navigate(`/workflows/${encodeURIComponent(d.workflow)}`));

  const card = h(
    "div",
    { class: "card" },
    row1,
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailWorkflow), wfName),
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailCwd), h("span", { class: "mono", style: { fontSize: "12px" } }, d.cwd)),
    h("div", { class: "kv" }, h("span", { class: "kv-k" }, i18n.detailTime), h("span", { class: "muted" }, timeLine(d))),
    running ? runningProgress(d) : null,
    interrupted ? h("div", { class: "fnote" }, i18n.interruptedNote) : null,
  );
  page.appendChild(card);

  // 需求：与 trace 输入同等地位——可能是大 PRD，独立成块、Prism markdown 渲染（自带转义、无 XSS）、可复制，
  // 而非挤在 kv 小行里当纯文本。
  page.appendChild(
    h(
      "div",
      { class: "promptblk" },
      codeBar(i18n.detailPrompt, d.userPrompt),
      h("div", { class: "ed ed-att", html: highlightHTML(d.userPrompt, "markdown") }),
    ),
  );

  // ---- failed：error 全文置顶 ----
  if (failed && d.error) {
    const failedStepText =
      d.failedStep !== null && d.failedStep !== undefined ? i18n.failedAtStepTpl(d.failedStep) : i18n.failedPrefix;
    page.appendChild(
      h("div", { class: "errpanel" }, h("h5", {}, failedStepText), h("p", {}, d.error)),
    );
  }

  // ---- 逐步列表 ----
  page.appendChild(stepsView(d.trace || [], colorIndex));

  // ---- 运行总结（终态渲染，running 提示待生成） ----
  if (running) {
    page.appendChild(h("div", { class: "fnote" }, i18n.summaryPending));
  } else if (completed || interrupted) {
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

  // ---- 冻结定义折叠区 ----
  page.appendChild(frozenDefView(snapshot));

  return page;
}

function statusBadgeLarge(status, running) {
  if (running) return h("span", { class: "badge b-running" }, h("span", { class: "pulse" }), "running");
  return h("span", { class: "badge b-" + status }, status);
}

function timeLine(d) {
  const start = fmtTimeSec(d.startedAt);
  if (d.endedAt) {
    const dur = durationBetween(d.startedAt, d.endedAt);
    const durText = dur !== null ? ` · ${i18n.durationLabel} ` + fmtDurationMs(dur) : "";
    return `${i18n.stepsCountTpl(d.steps)}${durText} · ${start} → ${fmtTimeSec(d.endedAt)}`;
  }
  const el = elapsedSince(d.startedAt);
  return `${start} ${i18n.sinceLabel}` + (el !== null ? ` · ${i18n.elapsedLabel} ` + fmtDurationMs(el) : "");
}

function runningProgress(d) {
  const pct = d.steps > 0 ? Math.min(100, (d.progress / d.steps) * 100) : 0;
  return h(
    "div",
    {},
    h("div", { class: "prog" }, h("span", { class: "prog-i", style: { width: pct.toFixed(1) + "%" } })),
    h("div", { class: "muted", style: { fontSize: "12px" } }, `已完成 ${d.progress}/${d.steps} 步`),
  );
}

// ---- 逐步列表 + 展开详情 ----
function stepsView(trace, colorIndex) {
  const container = h("div", { class: "steps" });
  // 仅含循环（评测内循环 / 回跳，任一步 iteration>1）的运行才按轮次分组；线性运行全是第 1 轮，
  // 插「第 1 轮」头纯属噪音，不插。
  const grouped = trace.some((e) => e.iteration > 1);
  let lastIteration = null;
  trace.forEach((entry, i) => {
    if (grouped && entry.iteration !== lastIteration) {
      lastIteration = entry.iteration;
      container.appendChild(
        h(
          "div",
          { class: "grouphead" },
          i18n.iterationTpl(entry.iteration),
          h("span", { class: "ghint" }, `iter=${entry.iteration}`),
        ),
      );
    }
    container.appendChild(stepRow(entry, i, colorIndex, i === trace.length - 1));
  });
  return container;
}

function stepRow(entry, index, colorIndex, isLast) {
  const ci = colorIndex[entry.nodeId] ?? 0;
  const label = entry.type === "evaluator" ? entry.displayName + " " + i18n.evalSuffix : entry.displayName;
  const meta = entry.success
    ? fmtDurationMs(entry.durationMs) + (entry.tokens ? " · " + fmtTokens(entry.tokens) + " tok" : "")
    : fmtDurationMs(entry.durationMs);
  const preview = entry.success ? entry.output : entry.error || "";

  const detailHolder = h("div");
  let open = false;

  const row = h(
    "div",
    { class: "step clickable" + (entry.success ? "" : " failrow") + (isLast ? " steplast" : "") },
    h("span", { class: "no" }, "step " + entry.stepIndex),
    h("span", { class: "idchip", style: chipStyle(ci) }, entry.nodeId),
    h("span", { class: "lbl" }, label),
    h("span", { class: "engc" }, engineIconEl(entry.engine), entry.engine),
    entry.success ? h("span", { class: "ok" }, "✓") : h("span", { class: "bad" }, "✗"),
    h("span", { class: "meta" }, meta),
    h("span", { class: "pv" }, previewLine(preview)),
  );
  row.addEventListener("click", () => {
    open = !open;
    if (open) {
      row.classList.remove("steplast");
      mount(detailHolder, stepDetail(entry, ci));
    } else {
      mount(detailHolder);
      if (isLast) row.classList.add("steplast");
    }
  });

  return h("div", {}, row, detailHolder);
}

function previewLine(text) {
  const firstLine = (text || "").split("\n").find((l) => l.trim() !== "") || "";
  return firstLine;
}

// stepDetail 展开时才构建 DOM（trace 单行可达 MB 级，延迟渲染是唯一允许的性能手段，点开必须完整全文）。
function stepDetail(entry, ci) {
  const cfg = engineConfigLine(entry.engine, entry.engineConfig);
  const outLabel = entry.success ? i18n.detailOutput : i18n.detailError;
  const outText = entry.success ? entry.output : entry.error || "";
  const outCls = entry.success ? "ed ed-att" : "ed ed-att ed-err";
  return h(
    "div",
    { class: "sdetail" },
    h("div", { class: "dkv" }, engineIconEl(entry.engine), h("span", {}, cfg)),
    codeBar(i18n.detailInput, entry.input),
    h("div", { class: "ed ed-att", html: highlightHTML(entry.input, "markdown") }),
    codeBar(outLabel, outText),
    h("div", { class: outCls, html: highlightHTML(outText, "markdown") }),
  );
}

// codeBar 是只读代码块的深色头条（字段名 + md + 复制）。
function codeBar(label, text) {
  return h(
    "div",
    { class: "edbar" },
    h("span", { class: "edname" }, label),
    h("span", { class: "edtag" }, "md"),
    h("span", { class: "grow" }),
    copyBtn((e) => {
      e.stopPropagation();
      copyText(text);
    }, "cpd"),
  );
}

// engineConfigLine 渲染「引擎 · 模型 · effort」；模型缺省显式标注「（引擎默认模型）」。
function engineConfigLine(engine, cfg) {
  const parts = [engine];
  const model = cfg && cfg.model ? cfg.model : i18n.engineDefaultModel;
  parts.push(model);
  if (cfg) {
    if (cfg.effort) parts.push("effort " + cfg.effort);
    if (cfg.reasoningEffort) parts.push("reasoningEffort " + cfg.reasoningEffort);
  }
  return parts.join(" · ");
}

// ---- 运行总结面板（marked 渲染） ----
function summaryPanel() {
  const bodyHolder = h("div");
  const copyBtnEl = h("button", { class: "ghost" }, copyIcon(), " " + i18n.copyAll);
  let rawMarkdown = "";
  copyBtnEl.addEventListener("click", () => copyText(rawMarkdown));
  const element = h(
    "div",
    { class: "panel" },
    h("div", { class: "panelhead" }, h("h5", {}, i18n.summaryTitle), h("span", { class: "grow" }), copyBtnEl),
    bodyHolder,
  );
  return {
    element,
    fill(md) {
      rawMarkdown = md;
      // marked 输出未消毒，总结含半可信引擎产物 → 注入 DOM 前必过 DOMPurify（strip script/on*/javascript:）。
      // 兜底 fail-safe：任一库缺失时退回纯文本转义，而非直接注入原始 HTML。
      const rendered = globalThis.marked ? globalThis.marked.parse(md) : null;
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

// ---- 冻结定义折叠区 ----
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
      // 与编辑器 JSON 视图一致：pre 外包 .jsonbody 补内间距（否则文字左边顶住边框）。
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

// ---- 小工具 ----
function copyBtn(handler, cls) {
  const btn = h("button", { class: cls }, copyIcon());
  btn.addEventListener("click", handler);
  return btn;
}

function escapeHTML(s) {
  return s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" })[c]);
}
