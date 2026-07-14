// 极简 DOM 构建助手：手写渲染（零构建、零框架，见 ui.md〈前端技术栈〉）。
// h(tag, props, ...children) 返回一个真实 Element。props 里 on* 挂事件、style 接对象、
// 其余按属性/属性名设置；children 接受字符串 / Node / 数组 / null（null 跳过，便于条件渲染）。

import { i18n } from "./i18n.js";

export function h(tag, props, ...children) {
  const el = document.createElement(tag);
  if (props) {
    for (const [key, value] of Object.entries(props)) {
      if (value === null || value === undefined || value === false) continue;
      if (key === "class") {
        el.className = value;
      } else if (key === "style" && typeof value === "object") {
        Object.assign(el.style, value);
      } else if (key === "dataset" && typeof value === "object") {
        Object.assign(el.dataset, value);
      } else if (key.startsWith("on") && typeof value === "function") {
        el.addEventListener(key.slice(2).toLowerCase(), value);
      } else if (key === "html") {
        // 唯一 innerHTML 注入点：调用方须自证 value 已消毒/转义——Prism 输出自带转义；
        // marked 输出须先过 DOMPurify（见 run-detail 的总结与节点输入/输出渲染），不得直接注入原始 HTML。
        el.innerHTML = value;
      } else {
        el.setAttribute(key, value === true ? "" : String(value));
      }
    }
  }
  appendChildren(el, children);
  return el;
}

function appendChildren(el, children) {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    if (Array.isArray(child)) {
      appendChildren(el, child);
    } else if (child instanceof Node) {
      el.appendChild(child);
    } else {
      el.appendChild(document.createTextNode(String(child)));
    }
  }
}

// clear 清空一个容器的所有子节点。
export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

// mount 用新内容替换容器的全部子节点。
export function mount(el, ...children) {
  clear(el);
  appendChildren(el, children);
}

// 一个短暂的浮层提示（复制成功等），不打断操作。
let toastTimer = null;
export function toast(message) {
  let node = document.querySelector(".toast");
  if (!node) {
    node = h("div", { class: "toast" });
    document.body.appendChild(node);
  }
  node.textContent = message;
  node.classList.add("show");
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(() => node.classList.remove("show"), 1400);
}

// copyText 把文本写入剪贴板并提示；剪贴板不可用（非安全上下文等）时回退 execCommand。
export function copyText(text, okMessage = i18n.copied) {
  const done = () => toast(okMessage);
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(done, () => fallbackCopy(text, done));
  } else {
    fallbackCopy(text, done);
  }
}

function fallbackCopy(text, done) {
  const ta = h("textarea", { style: { position: "fixed", opacity: "0" } });
  ta.value = text;
  document.body.appendChild(ta);
  ta.select();
  try {
    // execCommand 失败通常返回 false 而非抛异常，必须查返回值，否则会假报「已复制」。
    if (document.execCommand("copy")) done();
    else toast(i18n.copyFailRejected);
  } catch (err) {
    toast(i18n.copyFailPrefix + err.message);
  } finally {
    document.body.removeChild(ta);
  }
}

// 复制图标（SVG）——方形叠加复制图标。
export function copyIcon() {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("width", "12");
  svg.setAttribute("height", "12");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  const rect = document.createElementNS(ns, "rect");
  rect.setAttribute("x", "9");
  rect.setAttribute("y", "9");
  rect.setAttribute("width", "12");
  rect.setAttribute("height", "12");
  rect.setAttribute("rx", "2");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1");
  svg.appendChild(rect);
  svg.appendChild(path);
  return svg;
}
