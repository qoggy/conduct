// 工作流列表页（#/workflows）：workflow list 的表格镜像，兼 create / rename / copy / delete / run 五动词入口。

import { h, mount } from "../dom.js";
import { api } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTime } from "../format.js";
import { openModal, confirmModal } from "../modal.js";
import { openLaunchDialog, openRenameDialog, openCopyDialog } from "../dialogs.js";
import { loadInto } from "./common.js";

const MAX_CHIPS = 6;

export function renderWorkflowsPage(outlet) {
  return loadInto(
    outlet,
    () => api.listWorkflows(),
    (data) => view(outlet, data),
  );
}

function reload(outlet) {
  renderWorkflowsPage(outlet);
}

function view(outlet, data) {
  const workflows = data.workflows || [];
  const warnings = data.warnings || [];

  if (workflows.length === 0 && warnings.length === 0) {
    return h(
      "div",
      { class: "page" },
      h(
        "div",
        { class: "empty" },
        h("h2", {}, i18n.wfEmptyTitle),
        h("p", {}, i18n.wfEmptyHint),
        h("button", { class: "btn btn-ink", onClick: () => openCreate(outlet) }, i18n.wfNewBtn),
      ),
    );
  }

  const head = h(
    "div",
    { class: "pagehead" },
    h("h1", {}, i18n.wfTitle),
    h("span", { class: "count" }, i18n.wfSubtitleTpl(workflows.length)),
    h("span", { class: "grow" }),
    h("button", { class: "btn btn-ink", onClick: () => openCreate(outlet) }, i18n.wfNewBtn),
  );

  const rows = [
    h(
      "div",
      { class: "trow-wf thead" },
      h("div", {}, i18n.colName),
      h("div", {}, i18n.colNodes),
      h("div", {}, i18n.colUpdated),
      h("div", {}, i18n.colRunning),
      h("div", {}),
    ),
    ...workflows.map((wf, i) => rowView(outlet, wf, i === workflows.length - 1)),
  ];

  return h(
    "div",
    { class: "page" },
    ...warnings.map((w) => h("div", { class: "warnbar" }, h("span", {}, "⚠"), h("span", {}, w))),
    head,
    h("div", { class: "tbl" }, ...rows),
  );
}

function rowView(outlet, wf, isLast) {
  const gotoEditor = () => navigate(`/workflows/${encodeURIComponent(wf.name)}`);
  const running = wf.runningCount > 0;
  return h(
    "div",
    { class: "trow-wf trow-b" + (isLast ? " rowlast" : "") },
    h("div", {}, h("span", { class: "name-link", onClick: gotoEditor }, wf.name)),
    h("div", { onClick: gotoEditor }, h("div", { class: "nchips" }, ...nodeChips(wf.nodeIds || []))),
    h("div", { class: "muted", onClick: gotoEditor }, fmtTime(wf.updatedAt)),
    h(
      "div",
      { onClick: gotoEditor },
      running
        ? h("span", { class: "chip" }, h("span", { class: "pulse" }), i18n.runningCountTpl(wf.runningCount))
        : h("span", { class: "faint" }, "—"),
    ),
    h(
      "div",
      {},
      h(
        "div",
        { class: "acts" },
        h("button", { class: "ghost", onClick: () => openLaunchDialog(wf.name) }, i18n.actRun),
        h("button", { class: "ghost", onClick: () => openRenameDialog(wf.name, () => reload(outlet)) }, i18n.rename),
        h("button", { class: "ghost", onClick: () => openCopyDialog(wf.name, () => reload(outlet)) }, i18n.copy),
        h("button", { class: "ghost", onClick: () => openDelete(outlet, wf.name) }, i18n.delete),
      ),
    ),
  );
}

// nodeChips 把节点 id 流渲染成中性 chip，超过 6 个折叠为前 6 + +N（与 CLI workflow list 同规则）。
export function nodeChips(ids) {
  const shown = ids.length > MAX_CHIPS ? ids.slice(0, MAX_CHIPS) : ids;
  const out = [];
  shown.forEach((id, i) => {
    if (i > 0) out.push(h("span", { class: "narr" }, "›"));
    out.push(h("span", { class: "idchip" }, id));
  });
  if (ids.length > MAX_CHIPS) {
    out.push(h("span", { class: "narr" }, "›"));
    out.push(h("span", { class: "idchip" }, "+" + (ids.length - MAX_CHIPS)));
  }
  return out;
}

// 工作流名即时校验用正则，镜像 internal/workflow/name.go 的 workflowNamePattern（服务端为终裁）。
const WF_NAME_RE = /^[A-Za-z0-9._-]+$/;

// ---- 新建工作流 ----
function openCreate(outlet) {
  const input = h("input", { class: "inp inp-mono", placeholder: "order-export" });
  const err = h("div", { class: "ferr", style: { display: "none" } });
  const body = h(
    "div",
    {},
    h(
      "label",
      { class: "flabel" },
      i18n.fName,
      h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.nameRule)),
    ),
    input,
    err,
  );
  const createBtn = h("button", { class: "btn btn-ink", disabled: true }, i18n.create);
  // 即时校验：空 = 禁用按钮但不报错（尚未开始输入）；非法 = 提示规则并禁用。
  const syncName = () => {
    const name = input.value.trim();
    const invalid = name !== "" && (!WF_NAME_RE.test(name) || name === "." || name === "..");
    err.textContent = invalid ? i18n.nameInvalidHint : "";
    err.style.display = invalid ? "block" : "none";
    createBtn.disabled = name === "" || invalid;
  };
  input.addEventListener("input", syncName);
  createBtn.addEventListener("click", async () => {
    const name = input.value.trim();
    if (createBtn.disabled) return;
    createBtn.disabled = true;
    err.style.display = "none";
    try {
      await api.createWorkflow(name);
      ctl.close();
      navigate(`/workflows/${encodeURIComponent(name)}`); // 最短路径：建完直接进编辑器
    } catch (e) {
      err.textContent = e.message; // 名字校验错误是内核原文
      err.style.display = "block";
      createBtn.disabled = false;
    }
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") createBtn.click();
  });
  const ctl = openModal({
    title: i18n.dlgCreateTitle,
    body,
    footer: [h("button", { class: "btn", onClick: () => ctl.close() }, i18n.cancel), createBtn],
  });
}

// ---- 删除 ----
function openDelete(outlet, name) {
  confirmModal({
    title: i18n.dlgDeleteTitleTpl(name),
    danger: true,
    confirmLabel: i18n.delete,
    body: [
      h("p", { style: { margin: "0" } }, i18n.deleteBodyTpl(name)),
      h("p", { class: "fnote", style: { margin: "6px 0 0" } }, i18n.deleteNote),
    ],
    onConfirm: async () => {
      await api.deleteWorkflow(name);
      reload(outlet);
    },
  });
}
