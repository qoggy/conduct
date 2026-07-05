// 页面公共件：加载态、错误态、异步载入骨架。错误文案原样透传服务端原文（fail-loud 同源）。

import { h, mount } from "../dom.js";
import { i18n } from "../i18n.js";
import { renderEpoch } from "../router.js";

export function loadingView() {
  return h("div", { class: "page" }, h("div", { class: "loading" }, i18n.loadingText));
}

// errorView 展示一处载入错误：ApiError 的 message 是 CLI / 内核原文，原样呈现、不改写。
export function errorView(err) {
  const box = h("div", { class: "loaderr" }, i18n.loadFail, h("span", { class: "mono" }, err && err.message ? err.message : String(err)));
  return h("div", { class: "page" }, box);
}

// loadInto 通用异步载入：先铺加载态，loader() 成功后用 render(data) 结果替换，失败铺错误态。
export async function loadInto(outlet, loader, render) {
  const myEpoch = renderEpoch(); // 记下本次载入所属的导航序号
  mount(outlet, loadingView());
  let data;
  try {
    data = await loader();
  } catch (err) {
    if (renderEpoch() !== myEpoch) return; // 已切到别的页：丢弃过期错误，不盖当前页
    mount(outlet, errorView(err));
    return;
  }
  if (renderEpoch() !== myEpoch) return; // 已切到别的页：丢弃过期结果（大 trace 慢响应尤易触发）
  mount(outlet, render(data));
}
