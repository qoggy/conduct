// 引擎品牌 icon、节点色板、引擎能力表缓存。icon 和能力表来自 GET /api/engines。

import { h } from "./dom.js";
import { api } from "./api.js";

// engineIconEl 构建一个 .eicon 徽标元素：已知引擎渲染官方图标 <img>，未知引擎回退首字母。
export function engineIconEl(name) {
  const entry = (cache || []).find((engine) => engine.name === name);
  if (entry && entry.iconPath) {
    return h("img", { class: "eicon", src: entry.iconPath, alt: name, title: name });
  }
  return h("span", { class: "eicon eicon--fallback" }, (name || "?")[0]);
}

// 节点 id chip 的 4 色轮转板，按节点序号取色。
const PALETTE = [
  { fg: "var(--node-1-fg)", bg: "var(--node-1-bg)" },
  { fg: "var(--node-2-fg)", bg: "var(--node-2-bg)" },
  { fg: "var(--node-3-fg)", bg: "var(--node-3-bg)" },
  { fg: "var(--node-4-fg)", bg: "var(--node-4-bg)" },
];

export function paletteFor(index) {
  return PALETTE[((index % 4) + 4) % 4];
}

export function chipStyle(index) {
  const p = paletteFor(index);
  return { background: p.bg, color: p.fg };
}

// ---- 引擎能力表缓存 ----
// engines: [{name, capability: {allowsModel, modelSuggestions, allowsEffort, effortValues}, iconPath}]

let cache = null;

// loadEngines 拉取并缓存引擎能力表（检查器引擎 / effort 下拉的数据源）。只拉一次。
export async function loadEngines() {
  if (cache) return cache;
  cache = await api.engines();
  return cache;
}

export function engineNames() {
  return (cache || []).map((e) => e.name);
}

// capabilityOf 返回某引擎的能力表；未加载 / 未登记返回 null。
export function capabilityOf(name) {
  const entry = (cache || []).find((e) => e.name === name);
  return entry ? entry.capability : null;
}
