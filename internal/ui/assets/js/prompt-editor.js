// 提示词编辑器组件：markdown 源码模式 + Prism 语法高亮 + {{占位符}} 着色，全文可见可编辑、
// 在最小/最大高度内自动增高、超出内部滚动（内容全文可达，绝不缩略），附「最大化」整页覆盖层。
//
// 高亮与叠层交给通用 code-editor（Prism 真 markdown 语法）；本组件负责头部条、占位符复制、
// ghost 预告与最大化。见 ui.md〈工作流编辑器〉提示词编辑器段。

import { h, copyText } from "./dom.js";
import { createCodeEditor } from "./code-editor.js";
import { pushOverlay, popOverlay } from "./modal.js";
import { i18n } from "./i18n.js";

// createPromptEditor(opts) → { element, getValue, setValue, setInvalid, focus }
//   opts.value        初始文本
//   opts.fieldName    头部条字段名（如 promptTemplate）
//   opts.placeholders 占位符列表（点击复制），string[]；空则不渲染占位符下拉
//   opts.ghostAppend  运行时自动追加段的只读预告文本（可空）
//   opts.plain        true → 用纯 markdown 语法、不着色 {{占位符}}（启动弹窗的「需求」字段用：
//                     此处 {{...}} 是字面用户文本而非模板占位符，见 ui.md〈启动运行弹窗〉）
//   opts.onChange     文本变化回调 (value) => void
//   opts.invalid      初始是否标红
export function createPromptEditor(opts) {
  const placeholders = opts.placeholders || [];
  const lang = opts.plain ? "markdown" : "markdown-conduct";
  const meta = h("span", { class: "edmeta" });

  const editor = createCodeEditor({
    value: opts.value || "",
    lang,
    attached: true,
    ghostAppend: opts.ghostAppend,
    ghostLabel: i18n.ghostAppend,
    onChange: (v) => {
      updateMeta(v);
      if (opts.onChange) opts.onChange(v);
    },
  });

  function updateMeta(value) {
    meta.textContent = i18n.peMetaTpl(value.split("\n").length, value.length);
  }
  updateMeta(opts.value || "");

  const phMenu = buildPlaceholderMenu(placeholders, (text) => copyText(text, i18n.copied + " " + text));
  const maximizeBtn = h("span", { class: "edmax", onClick: () => openMaximized() }, i18n.maximize);

  const bar = h(
    "div",
    { class: "edbar" },
    h("span", { class: "edname" }, opts.fieldName),
    h("span", { class: "edtag" }, "md"),
    h("span", { class: "grow" }),
    meta,
    placeholders.length ? phMenu : null,
    maximizeBtn,
  );

  const element = h("div", {}, bar, editor.element);
  if (opts.invalid) editor.setInvalid(true);

  // 最大化：整页覆盖层，另起一个绑定同一份值的大编辑器；关闭时写回并触发 onChange。
  function openMaximized() {
    const big = createCodeEditor({
      value: editor.getValue(),
      lang,
      ghostAppend: opts.ghostAppend,
      ghostLabel: i18n.ghostAppend,
    });
    big.element.classList.add("pe-max"); // 撑满覆盖层高度（解除普通模式 max-height，见 style.css .pe-max）
    const closeBtn = h("span", { class: "edmax", onClick: () => close() }, i18n.collapse);
    const maxBar = h(
      "div",
      { class: "edbar", style: { borderRadius: "10px 10px 0 0" } },
      h("span", { class: "edname" }, opts.fieldName),
      h("span", { class: "edtag" }, "md"),
      h("span", { class: "grow" }),
      closeBtn,
    );
    const boxWrap = h("div", { class: "pe-max-box" }, maxBar, big.element);
    const dim = h("div", { class: "pe-max-dim" }, boxWrap);
    function close() {
      popOverlay(close);
      editor.setValue(big.getValue());
      updateMeta(big.getValue());
      if (opts.onChange) opts.onChange(big.getValue());
      if (dim.parentNode) dim.parentNode.removeChild(dim);
    }
    pushOverlay(close);
    document.body.appendChild(dim);
    big.focus();
  }

  return {
    element,
    getValue: () => editor.getValue(),
    setValue: (v) => {
      editor.setValue(v);
      updateMeta(v);
    },
    setInvalid: editor.setInvalid,
    focus: editor.focus,
  };
}

// buildPlaceholderMenu 组占位符复制下拉（hover 展开；点击项复制）。
function buildPlaceholderMenu(placeholders, onPick) {
  const items = placeholders.map((p) => h("div", { class: "phitem", onClick: () => onPick(p) }, p));
  return h(
    "div",
    { class: "phwrap" },
    h("span", { class: "edmax" }, i18n.phMenuLabel),
    h("div", { class: "phmenu" }, h("div", { class: "phbox" }, h("div", { class: "phhead" }, i18n.phMenuHead), ...items)),
  );
}
