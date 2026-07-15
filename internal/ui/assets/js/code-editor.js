// 通用代码编辑器：透明 textarea 叠在 Prism 高亮层之上（字符度量一致 → 光标/选区自然对齐），
// 输入即重算高亮、同步滚动。提示词编辑器与 JSON 视图都建在它之上。
//
// 高亮由 Prism 承担（真 markdown / json 语法），本组件只负责叠层、滚动同步与可选 ghost 页脚。

import { h } from "./dom.js";
import { i18n } from "./i18n.js";
import { highlightHTML } from "./highlight.js";

// createCodeEditor(opts) → { element, getValue, setValue, focus }
//   opts.value       初始文本
//   opts.lang        "markdown-conduct" | "markdown" | "json"
//   opts.ghostAppend 运行时自动追加段的只读预告（可空）
//   opts.ghostLabel  ghost 标签文案
//   opts.attached    true → 无上边框、仅下圆角（贴在深色头条下方，用于提示词编辑器）
//   opts.minHeight / opts.maxHeight  高亮层最小/最大高度（px 字符串），超出内部滚动
//   opts.onChange    文本变化回调 (value) => void
export function createCodeEditor(opts) {
  const layer = h("pre", { class: "pe-layer" });
  const textarea = h("textarea", { class: "pe-input", spellcheck: "false" });
  textarea.value = opts.value || "";
  if (opts.minHeight) layer.style.minHeight = opts.minHeight;
  if (opts.maxHeight) layer.style.maxHeight = opts.maxHeight;

  function refresh() {
    // 尾随换行：令 textarea 末行为空时高亮层也留出等高的一行，光标不越界。
    layer.innerHTML = highlightHTML(textarea.value, opts.lang) + "\n";
  }
  refresh();

  textarea.addEventListener("input", () => {
    refresh();
    if (opts.onChange) opts.onChange(textarea.value);
  });
  textarea.addEventListener("scroll", () => {
    layer.scrollTop = textarea.scrollTop;
    layer.scrollLeft = textarea.scrollLeft;
  });

  const scroll = h("div", { class: "pe-scroll" }, layer, textarea);
  const children = [scroll];
  if (opts.ghostAppend) {
    children.push(
      h("div", { class: "ghostblk" }, h("span", { class: "ghosttag" }, opts.ghostLabel || i18n.ghostAppend), opts.ghostAppend),
    );
  }
  const element = h("div", { class: "pe-wrap" + (opts.attached ? " pe-wrap--attached" : "") }, ...children);

  return {
    element,
    getValue: () => textarea.value,
    setValue: (v) => {
      textarea.value = v;
      refresh();
    },
    setInvalid: (on) => element.classList.toggle("red", !!on),
    focus: () => textarea.focus(),
    textarea,
  };
}
