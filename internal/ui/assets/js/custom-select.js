// 自定义下拉基础设施：原生 <select>（尤其 macOS）弹出样式不可控（以当前选中项为中心展开，
// 而非固定向下），且渲染不了图标；<input list><datalist> 同样受制于浏览器原生弹出层。
// 这里手写一套「点击展开、点击外部关闭、固定向下弹出」的最小实现，供纯选择型的 listSelect()
// 与 pages/editor.js 的 model 组合框共用。

import { h, mount } from "./dom.js";
import { engineNames, engineIconEl } from "./engines.js";

// closeOnOutsideClick 挂一个「点击 wrap 外部即调用 close」的监听：延后挂载（避免触发展开的
// 那次点击被同一个监听立刻关掉），返回卸载函数，供关闭时调用避免监听泄漏。
export function closeOnOutsideClick(wrap, close) {
  function onDoc(e) {
    if (!wrap.contains(e.target)) close();
  }
  const timer = setTimeout(() => document.addEventListener("click", onDoc), 0);
  return () => {
    clearTimeout(timer);
    document.removeEventListener("click", onDoc);
  };
}

// listSelect(current, items, onChange, options?) → 一个 .engsel 元素。items: [{value, label, icon?}]。
// icon 是**工厂函数** `() => Element`（不是单个元素）：选中态显示与展开菜单项各需一份 icon，同一个
// DOM 节点不能同时挂两处，故每处各调 icon() 取一个新节点，避免选中项的图标被挪走后菜单里缺失。
// 纯选择型下拉（不可打字）：engineSelect() 与 effortField() 共用的展示层。
export function listSelect(current, items, onChange, options = {}) {
  const display = h("div", {
    class: "selc engsel-display",
    role: "button",
    tabindex: "0",
    "aria-haspopup": "listbox",
    "aria-expanded": "false",
    "aria-label": options.label || null,
  });
  const menu = h("div", { class: "engsel-menu", role: "listbox" });
  const wrap = h("div", { class: "engsel" }, display, menu);
  let stopOutsideClick = null;
  let selected = current;

  function itemFor(value) {
    return items.find((it) => it.value === value);
  }
  function renderDisplay(value) {
    const item = itemFor(value);
    mount(display, h("span", { class: "engsel-cur" }, item?.icon ? item.icon() : null, h("span", {}, item ? item.label : "")), h("span", { class: "caret" }, "▾"));
  }
  function pick(value) {
    selected = value;
    renderDisplay(value);
    for (const item of menu.querySelectorAll(".engsel-item")) {
      const on = item.dataset.value === value;
      item.classList.toggle("engsel-item--on", on);
      item.setAttribute("aria-selected", String(on));
    }
    close();
    onChange(value);
  }
  function open() {
    wrap.classList.add("open");
    display.setAttribute("aria-expanded", "true");
    stopOutsideClick = closeOnOutsideClick(wrap, close);
  }
  function close() {
    wrap.classList.remove("open");
    display.setAttribute("aria-expanded", "false");
    if (stopOutsideClick) {
      stopOutsideClick();
      stopOutsideClick = null;
    }
  }

  display.addEventListener("click", (e) => {
    e.stopPropagation();
    wrap.classList.contains("open") ? close() : open();
  });
  display.addEventListener("keydown", (e) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      wrap.classList.contains("open") ? close() : open();
    } else if (e.key === "Escape") {
      close();
    }
  });
  mount(
    menu,
    ...items.map((item) =>
      h(
        "div",
        {
          class: "engsel-item" + (item.value === current ? " engsel-item--on" : ""),
          role: "option",
          "aria-selected": String(item.value === selected),
          dataset: { value: item.value },
          onClick: (e) => {
            e.stopPropagation();
            pick(item.value);
          },
        },
        item.icon ? item.icon() : null,
        h("span", {}, item.label),
      ),
    ),
  );
  renderDisplay(current);
  return wrap;
}

// engineSelect(current, onChange) → 引擎选择器：listSelect 套上带官方图标的引擎列表。
// 选中态显示与展开列表项都带图标；点击外部 / 选中即关闭（继承自 listSelect）。
export function engineSelect(current, onChange) {
  const items = engineNames().map((name) => ({ value: name, label: name, icon: () => engineIconEl(name) }));
  return listSelect(current, items, onChange);
}
