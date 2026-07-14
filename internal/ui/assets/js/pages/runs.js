// 运行列表页（#/runs）：run list 的表格镜像 + 过滤。running 项置顶分组，其余按开始时间倒序。

import { h, mount } from "../dom.js";
import { api } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTime } from "../format.js";
import { confirmModal } from "../modal.js";
import { loadInto } from "./common.js";

const STATUSES = ["running", "completed", "failed", "interrupted"];

// query 可带 workflow / status 过滤（等价 run list --json 的 jq 筛选）。
export function renderRunsPage(outlet, query) {
  const filter = { workflow: query.workflow || "", status: query.status || "" };
  return loadInto(
    outlet,
    () => api.listRuns(filter),
    (data) => view(outlet, data, filter),
  );
}

function reload(outlet, filter) {
  renderRunsPage(outlet, filter);
}

function view(outlet, data, filter) {
  const runs = data.runs || [];
  const warnings = data.warnings || [];

  // 过滤器候选：工作流取自当前结果集里出现过的名字（叠加当前筛选值，避免筛掉后自己消失）。
  const workflowOptions = [...new Set(runs.map((r) => r.workflow).concat(filter.workflow ? [filter.workflow] : []))].sort();

  const head = h(
    "div",
    { class: "pagehead" },
    h("h1", {}, i18n.runTitle),
    h("span", { class: "count" }, i18n.runSubtitleTpl(runs.length)),
    h("span", { class: "grow" }),
    filterSelect(i18n.filterWorkflow, workflowOptions, filter.workflow, (v) =>
      navigate(runsHash({ ...filter, workflow: v })),
    ),
    filterSelect(i18n.filterStatus, STATUSES, filter.status, (v) => navigate(runsHash({ ...filter, status: v }))),
    h("button", { class: "btn", onClick: () => reload(outlet, filter) }, i18n.refresh),
  );

  if (runs.length === 0) {
    return h(
      "div",
      { class: "page" },
      ...warnings.map((w) => h("div", { class: "warnbar" }, h("span", {}, "⚠"), h("span", {}, w))),
      head,
      h(
        "div",
        { class: "empty" },
        h("h2", {}, i18n.runEmptyTitle),
        h("p", {}, i18n.runEmptyHint),
      ),
    );
  }

  // running 置顶分组，其余按开始时间倒序（在跑的是监控核心，不混进时间倒序）。
  const runningRuns = runs.filter((r) => r.status === "running");
  const historyRuns = runs
    .filter((r) => r.status !== "running")
    .sort((a, b) => (a.startedAt < b.startedAt ? 1 : -1));

  const sections = [];
  if (runningRuns.length) {
    sections.push(h("div", { class: "gtitle" }, i18n.groupRunningTpl(runningRuns.length)));
    sections.push(runTable(outlet, runningRuns, filter));
  }
  if (historyRuns.length) {
    sections.push(h("div", { class: "gtitle" }, i18n.groupHistoryTpl(historyRuns.length)));
    sections.push(runTable(outlet, historyRuns, filter));
  }

  return h(
    "div",
    { class: "page" },
    ...warnings.map((w) => h("div", { class: "warnbar" }, h("span", {}, "⚠"), h("span", {}, w))),
    head,
    ...sections,
  );
}

function runTable(outlet, runs, filter) {
  const header = h(
    "div",
    { class: "trow-run thead" },
    h("div", {}, i18n.colRunID),
    h("div", {}, i18n.colWorkflow),
    h("div", {}, i18n.colStatus),
    h("div", {}, i18n.colProgress),
    h("div", {}, i18n.colStartedAt),
    h("div", {}, i18n.colUserPrompt),
    h("div", {}), // 操作列（删除按钮），表头留空
  );
  const rows = runs.map((r, i) => runRow(outlet, filter, r, i === runs.length - 1));
  return h("div", { class: "tbl" }, header, ...rows);
}

function runRow(outlet, filter, r, isLast) {
  const goto = () => navigate(`/runs/${encodeURIComponent(r.id)}`);
  return h(
    "div",
    { class: "trow-run trow-b" + (isLast ? " rowlast" : ""), onClick: goto },
    h("div", {}, h("span", { class: "rid" }, r.id)),
    h("div", { class: "mono", style: { fontSize: "12px" } }, r.workflow),
    h("div", {}, statusBadge(r.status)),
    h("div", {}, progressCell(r)),
    h("div", { class: "muted" }, fmtTime(r.startedAt)),
    h("div", { class: "muted", style: { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } }, r.userPrompt),
    h("div", { class: "actcell" }, runActions(outlet, filter, r)),
  );
}

// runActions：终态记录给一个删除按钮（running 不给——须先 stop，与 conduct run rm 语义一致）。
// 按钮 stopPropagation，以免点删除触发整行的进入详情。
function runActions(outlet, filter, r) {
  if (r.status === "running") return h("span", {});
  return h(
    "button",
    {
      class: "ghost",
      onClick: (e) => {
        e.stopPropagation();
        openRunDelete(outlet, filter, r.id);
      },
    },
    i18n.delete,
  );
}

// openRunDelete：删除运行记录的二次确认弹窗，充分参考工作流删除（openDelete）。确认后
// DELETE /api/runs/{id} 再刷新当前视图；删 running 会被服务端 409 拦下并在弹窗内提示。
function openRunDelete(outlet, filter, id) {
  confirmModal({
    title: i18n.dlgRunDeleteTitleTpl(id),
    danger: true,
    confirmLabel: i18n.delete,
    body: [h("p", { style: { margin: "0" } }, i18n.runDeleteBodyTpl(id))],
    onConfirm: async () => {
      await api.deleteRun(id);
      reload(outlet, filter);
    },
  });
}

export function statusBadge(status) {
  if (status === "running") {
    return h("span", { class: "badge b-running" }, h("span", { class: "pulse" }), "running");
  }
  return h("span", { class: "badge b-" + status }, status);
}

// progressCell：running / interrupted 显示 k/N 进度条（分母 = agent 节点数）；终态显示节点数。
function progressCell(r) {
  const isPartial = r.status === "running" || r.status === "interrupted";
  if (isPartial && r.nodeCount > 0) {
    const pct = Math.min(100, (r.progress / r.nodeCount) * 100);
    const color = r.status === "running" ? "#3E63DD" : "#C99A2E";
    return h(
      "span",
      { class: "pbar" },
      h("span", { class: "ptrack" }, h("span", { class: "pfill", style: { width: pct.toFixed(1) + "%", background: color } })),
      h("span", { class: "mono", style: { fontSize: "12px" } }, `${r.progress}/${r.nodeCount}`),
    );
  }
  return h("span", { class: "mono", style: { fontSize: "12px" } }, i18n.nodesCountTpl(r.nodeCount));
}

// filterSelect 组一个原生 <select> 过滤器；值为空表示「全部」。
function filterSelect(label, options, value, onPick) {
  const sel = h(
    "select",
    { class: "inp", style: { width: "auto", minHeight: "0" }, onChange: (e) => onPick(e.target.value) },
    h("option", { value: "" }, `${label}：${i18n.filterAll}`),
    ...options.map((o) => h("option", { value: o, selected: o === value }, `${label}：${o}`)),
  );
  return sel;
}

function runsHash(filter) {
  const params = new URLSearchParams();
  if (filter.workflow) params.set("workflow", filter.workflow);
  if (filter.status) params.set("status", filter.status);
  const qs = params.toString();
  return "/runs" + (qs ? "?" + qs : "");
}
