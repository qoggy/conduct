// 引擎品牌 icon、节点色板、引擎能力表缓存。icon / 色板取自设计方案；能力表来自 GET /api/engines。

import { h } from "./dom.js";
import { api } from "./api.js";

// 引擎品牌 icon：各家官方图标（favicon，内嵌于 vendor/engine-icons/，取自 claude.com /
// antigravity.google / qoder.com / openai.com/codex）。未登记的引擎回退到中性首字母徽标。
const ICON_FILES = {
  "claude-code": "claude-code.png", // Claude 星芒（claude.com）
  antigravity: "antigravity.png", // Antigravity 彩虹峰（antigravity.google）
  qoder: "qoder.png", // Qoder app 图标（qoder.com）
  codex: "codex.png", // OpenAI Codex blossom + 终端提示符 >_（openai.com/codex，白 mark 黑底）
};

// engineIconEl 构建一个 .eicon 徽标元素：已知引擎渲染官方图标 <img>，未知引擎回退首字母。
export function engineIconEl(name) {
  const file = ICON_FILES[name];
  if (file) {
    return h("img", { class: "eicon", src: "/vendor/engine-icons/" + file, alt: name, title: name });
  }
  return h("span", { class: "eicon eicon--fallback" }, (name || "?")[0]);
}

// 节点 id chip 的 4 色轮转板，按节点序号取色。
const PALETTE = [
  { fg: "#6D4FC4", bg: "#F1EDFB" },
  { fg: "#0E7A6E", bg: "#E7F4F2" },
  { fg: "#3D6C9E", bg: "#EAF1F8" },
  { fg: "#A3459B", bg: "#F8EDF7" },
];

export function paletteFor(index) {
  return PALETTE[((index % 4) + 4) % 4];
}

export function chipStyle(index) {
  const p = paletteFor(index);
  return { background: p.bg, color: p.fg };
}

// ---- 引擎能力表缓存 ----
// engines: [{name, capability: {allowsModel, effortField, effortValues} | null}]

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

// capabilityOf 返回某引擎的能力表；未加载 / 未登记（capability 为 null）返回 null——
// 调用方据此不渲染该引擎没有的字段（不误报 allowsModel:false）。
export function capabilityOf(name) {
  const entry = (cache || []).find((e) => e.name === name);
  return entry ? entry.capability : null;
}
