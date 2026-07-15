// 极简 hash 路由。路由表用 :param 占位；hash 可带 ?query（如 #/runs?workflow=autopilot）。
// 单 index.html + 内嵌静态服务，hash 路由使浏览器只请求 / 与资源本身，无需 history fallback。

import { i18n } from "./i18n.js";

const routes = [];
let outlet = null;
let notFound = null;
let epoch = 0; // 每次导航自增，供异步页面 loader 判断自己是否已被切走（防过期响应盖掉当前页）

// renderEpoch 返回当前导航序号；loadInto 等异步载入在挂载前比对它，变了说明已切走、应丢弃结果。
export function renderEpoch() {
  return epoch;
}

// register 登记一条路由：pattern 形如 "/workflows/:name"，handler(params, query, outlet)。
export function register(pattern, handler) {
  routes.push({ segments: pattern.split("/").filter(Boolean), handler });
}

export function setNotFound(handler) {
  notFound = handler;
}

// start 绑定路由到某个挂载点并立即渲染当前 hash。
export function start(mountEl) {
  outlet = mountEl;
  window.addEventListener("hashchange", render);
  render();
}

// navigate 跳到某个 hash 路径（供代码内跳转，如启动成功后进详情页）。
export function navigate(path) {
  if (location.hash === "#" + path) {
    render(); // 相同 hash 不触发 hashchange，手动重渲
  } else {
    location.hash = path;
  }
}

// currentPath 返回当前 hash 的路径部分（不含 # 与 ?query）。
export function currentPath() {
  return parseHash().path;
}

export function rerender() {
  render();
}

function parseHash() {
  let raw = location.hash.replace(/^#/, "");
  if (!raw) raw = "/workflows"; // 默认路由
  const qIndex = raw.indexOf("?");
  const path = qIndex >= 0 ? raw.slice(0, qIndex) : raw;
  const query = {};
  if (qIndex >= 0) {
    new URLSearchParams(raw.slice(qIndex + 1)).forEach((value, key) => {
      query[key] = value;
    });
  }
  return { path, query };
}

function match(pathSegments, routeSegments) {
  if (pathSegments.length !== routeSegments.length) return null;
  const params = {};
  for (let i = 0; i < routeSegments.length; i++) {
    const rs = routeSegments[i];
    if (rs.startsWith(":")) {
      params[rs.slice(1)] = decodeURIComponent(pathSegments[i]);
    } else if (rs !== pathSegments[i]) {
      return null;
    }
  }
  return params;
}

function render() {
  epoch++; // 新一次导航：先前页面的在途 loader 据此作废
  const { path, query } = parseHash();
  const pathSegments = path.split("/").filter(Boolean);
  syncActiveTab(path);
  for (const route of routes) {
    const params = match(pathSegments, route.segments);
    if (params) {
      Promise.resolve(route.handler(params, query, outlet)).catch((err) => {
        // 页面渲染的兜底：不静默，把错误展示出来。
        renderFatal(err);
      });
      return;
    }
  }
  if (notFound) notFound(outlet);
}

function renderFatal(err) {
  if (!outlet) return;
  outlet.innerHTML = "";
  const box = document.createElement("div");
  box.className = "page";
  box.innerHTML = `<div class="loaderr"><span class="render-fail"></span><span class="mono"></span></div>`;
  box.querySelector(".render-fail").textContent = i18n.renderFail;
  box.querySelector(".mono").textContent = err && err.message ? err.message : String(err);
  outlet.appendChild(box);
  // 同时打到控制台，便于排查。
  console.error("conduct ui: page rendering failed", err);
}

// syncActiveTab 让顶栏主导航和设置入口跟随当前路由高亮。
function syncActiveTab(path) {
  const active = path.startsWith("/settings") ? "settings" : path.startsWith("/runs") ? "runs" : "workflows";
  document.querySelectorAll("[data-tab]").forEach((el) => {
    const on = el.dataset.tab === active;
    el.classList.toggle("tab-on", on);
  });
}
