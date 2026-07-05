// 时间 / 耗时 / 进度的展示格式化。全部纯函数，不引入依赖。

// fmtTime 把 RFC3339 时间串渲染成「2026-07-03 15:40」（本地时区）。空串 / 非法值原样返回。
export function fmtTime(rfc3339) {
  if (!rfc3339) return "";
  const d = new Date(rfc3339);
  if (isNaN(d.getTime())) return rfc3339;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// fmtTimeSec 同 fmtTime 但精确到秒（详情页时间线用）。
export function fmtTimeSec(rfc3339) {
  if (!rfc3339) return "";
  const d = new Date(rfc3339);
  if (isNaN(d.getTime())) return rfc3339;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function pad(n) {
  return String(n).padStart(2, "0");
}

// fmtDurationMs 把毫秒渲染成「6m37s」/「31.2s」/「820ms」。
export function fmtDurationMs(ms) {
  if (ms === null || ms === undefined) return "";
  if (ms < 1000) return `${ms}ms`;
  const totalSec = ms / 1000;
  if (totalSec < 60) return `${totalSec.toFixed(1)}s`;
  // 先取整再拆分，避免 round(sec%60) 进位到 60 而显示「1m60s」（如 119.6s）。
  const total = Math.round(totalSec);
  return `${Math.floor(total / 60)}m${pad(total % 60)}s`;
}

// durationBetween 计算两个 RFC3339 时刻之间的毫秒差；任一非法返回 null。
export function durationBetween(startRfc, endRfc) {
  const a = new Date(startRfc).getTime();
  const b = new Date(endRfc).getTime();
  if (isNaN(a) || isNaN(b)) return null;
  return b - a;
}

// elapsedSince 计算从某时刻至今的毫秒（running 已运行时长）；非法返回 null。
export function elapsedSince(startRfc) {
  const a = new Date(startRfc).getTime();
  if (isNaN(a)) return null;
  return Date.now() - a;
}

// fmtTokens 千分位化 token 数。
export function fmtTokens(n) {
  return n.toLocaleString("en-US");
}
