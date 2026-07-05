// 引擎选择器：带品牌图标的自定义下拉（原生 <select>/<option> 渲染不了图片，故手写）。
// 选中态显示与展开列表项都带官方引擎图标；点击外部 / 选中即关闭。

import { h, mount } from "./dom.js";
import { engineNames, engineIconEl } from "./engines.js";

// engineSelect(current, onChange) → 一个 .engsel 元素。onChange(name) 在选中新引擎时触发。
export function engineSelect(current, onChange) {
  const display = h("div", { class: "selc engsel-display" });
  const menu = h("div", { class: "engsel-menu" });
  const wrap = h("div", { class: "engsel" }, display, menu);

  function renderDisplay(name) {
    mount(
      display,
      h("span", { class: "engsel-cur" }, engineIconEl(name), h("span", {}, name)),
      h("span", { class: "caret" }, "▾"),
    );
  }
  function pick(name) {
    renderDisplay(name);
    close();
    onChange(name);
  }
  function openMenu() {
    wrap.classList.add("open");
    // 延后挂载，避免本次点击立刻被 onDoc 关掉。
    setTimeout(() => document.addEventListener("click", onDoc), 0);
  }
  function close() {
    wrap.classList.remove("open");
    document.removeEventListener("click", onDoc);
  }
  function onDoc(e) {
    if (!wrap.contains(e.target)) close();
  }

  display.addEventListener("click", (e) => {
    e.stopPropagation();
    wrap.classList.contains("open") ? close() : openMenu();
  });
  mount(
    menu,
    ...engineNames().map((name) =>
      h(
        "div",
        {
          class: "engsel-item" + (name === current ? " engsel-item--on" : ""),
          onClick: (e) => {
            e.stopPropagation();
            pick(name);
          },
        },
        engineIconEl(name),
        h("span", {}, name),
      ),
    ),
  );
  renderDisplay(current);
  return wrap;
}
