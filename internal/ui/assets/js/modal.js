// 弹窗助手。openModal 铺一层遮罩 + 居中卡片，点遮罩 / 按 Esc 关闭；返回 { close }。
// 用于新建 / 启动 / 改名 / 删除 / 终止 / 保存冲突等所有二次确认。

import { h, mount } from "./dom.js";
import { i18n } from "./i18n.js";

// 浮层栈：openModal 与最大化编辑器（prompt-editor）共用，保证一次 Esc 只关**最上层**浮层。
// 否则各浮层各挂一个 document keydown，叠层时（如启动弹窗→浏览目录）一次 Esc 会连环关闭、
// 把下层已填内容一并丢弃。栈顶优先、stopPropagation 阻断继续冒泡到其它浮层。
const overlayStack = [];
function handleEscape(e) {
  if (e.key !== "Escape") return;
  const top = overlayStack[overlayStack.length - 1];
  if (top) {
    e.stopPropagation();
    top();
  }
}
export function pushOverlay(closeFn) {
  if (overlayStack.length === 0) document.addEventListener("keydown", handleEscape);
  overlayStack.push(closeFn);
}
export function popOverlay(closeFn) {
  const i = overlayStack.lastIndexOf(closeFn);
  if (i >= 0) overlayStack.splice(i, 1);
  if (overlayStack.length === 0) document.removeEventListener("keydown", handleEscape);
}

// openModal({ title, body, footer, width }):
//   body   —— Node 或 Node[]（弹窗主体）
//   footer —— Node[]（底部按钮，通常含一个「取消」+ 一个主操作）
//   width  —— 覆盖默认宽度（480px）
export function openModal({ title, body, footer, width }) {
  const card = h(
    "div",
    { class: "modal", style: width ? { width } : null, onClick: (e) => e.stopPropagation() },
    h("div", { class: "mhead" }, title),
    h("div", { class: "mbody" }, body),
    footer ? h("div", { class: "mfoot" }, footer) : null,
  );
  const dim = h("div", { class: "dim", onClick: () => close() }, card);

  function close() {
    popOverlay(close);
    if (dim.parentNode) dim.parentNode.removeChild(dim);
  }
  pushOverlay(close);
  document.body.appendChild(dim);

  // 若主体内有输入框，自动聚焦第一个，便于键盘操作。
  const firstInput = card.querySelector("input, textarea");
  if (firstInput) firstInput.focus();

  return { close, card };
}

// confirmModal 是「取消 / 危险确认」两键弹窗的快捷封装（删除 / 终止）。
// onConfirm 可返回 Promise；执行中禁用按钮，抛错则把错误文案显示在 body 下方、不关闭。
export function confirmModal({ title, body, confirmLabel, danger, onConfirm }) {
  const errLine = h("div", { class: "ferr", style: { display: "none" } });
  const cancelBtn = h("button", { class: "btn", onClick: () => ctl.close() }, i18n.cancel);
  const confirmBtn = h("button", { class: danger ? "btn btn-red" : "btn btn-ink" }, confirmLabel);
  confirmBtn.addEventListener("click", async () => {
    confirmBtn.disabled = cancelBtn.disabled = true;
    errLine.style.display = "none";
    try {
      await onConfirm();
      ctl.close();
    } catch (err) {
      errLine.textContent = err.message;
      errLine.style.display = "block";
      confirmBtn.disabled = cancelBtn.disabled = false;
    }
  });
  const bodyNodes = Array.isArray(body) ? [...body, errLine] : [body, errLine];
  const ctl = openModal({ title, body: bodyNodes, footer: [cancelBtn, confirmBtn] });
  return ctl;
}
