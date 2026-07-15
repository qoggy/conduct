import { formatErrorEnvelope, formatProblem, i18n } from "./i18n.js";

// /api/* 客户端封装。统一：no-store（防浏览器缓存让刷新失真，与服务端 Cache-Control 呼应）、
// 变更类请求带 application/json、非 2xx 抛 ApiError。领域错误由稳定错误码在当前字典中渲染，
// technicalDetail 则保持服务端固定英文诊断或下游原文。

// ApiError 携带 HTTP 状态码、本地化 message、稳定错误信封、可选字段级 problems，以及非 JSON 原文。
// 409 乐观并发冲突时 body.current 带回当前定义（供编辑器弹「覆盖 / 重载」）。
export class ApiError extends Error {
  constructor(status, envelope, body, fallbackMessage = "") {
    super(envelope ? formatErrorEnvelope(envelope) : fallbackMessage || i18n.requestFailTpl(status));
    this.name = "ApiError";
    this.status = status;
    this.envelope = envelope;
    this.problems = ((envelope && envelope.problems) || []).map((problem) => ({
      ...problem,
      message: formatProblem(problem),
    }));
    this.current = body && body.current; // 409 冲突时的当前定义
    this.body = body;
  }
}

async function request(method, path, { body, headers } = {}) {
  const opts = { method, cache: "no-store", headers: { ...headers } };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  let resp;
  try {
    resp = await fetch(path, opts);
  } catch (err) {
    // 网络层失败（服务已停 / 断连）：转译成 ApiError，不静默。
    throw new ApiError(0, null, null, i18n.networkFailTpl(err.message));
  }
  return parse(resp);
}

async function parse(resp) {
  const contentType = resp.headers.get("Content-Type") || "";
  const isJSON = contentType.includes("application/json");
  const text = await resp.text();

  if (!resp.ok) {
    if (isJSON && text) {
      let parsed;
      try {
        parsed = JSON.parse(text);
      } catch {
        throw new ApiError(resp.status, null, null, text);
      }
      throw new ApiError(resp.status, parsed.error || null, parsed);
    }
    throw new ApiError(resp.status, null, null, text || i18n.requestFailTpl(resp.status));
  }

  if (resp.status === 204 || text === "") return null;
  if (isJSON) return JSON.parse(text);
  return text; // text/markdown 等（运行总结）
}

export const api = {
  // 只读
  version: () => request("GET", "/api/version"),
  engines: () => request("GET", "/api/engines"),
  settings: () => request("GET", "/api/settings"),
  listWorkflows: () => request("GET", "/api/workflows"),
  getWorkflow: (name) => request("GET", `/api/workflows/${encodeURIComponent(name)}`),
  listRuns: (query) => request("GET", "/api/runs" + queryString(query)),
  getRun: (id, withTrace) => request("GET", `/api/runs/${encodeURIComponent(id)}${withTrace ? "?trace=1" : ""}`),
  getSummary: (id) => request("GET", `/api/runs/${encodeURIComponent(id)}/summary`),
  // 目录浏览（工作目录选择器）：path 空则服务端从用户主目录起步。
  listDir: (path) => request("GET", "/api/fs" + (path ? `?path=${encodeURIComponent(path)}` : "")),

  // 变更
  createWorkflow: (name) => request("POST", "/api/workflows", { body: { name } }),
  // putWorkflow 携带载入时 updatedAt 基线做乐观冲突提示（见 handlePutWorkflow）。
  putWorkflow: (name, definition, baseUpdatedAt) =>
    request("PUT", `/api/workflows/${encodeURIComponent(name)}`, {
      body: definition,
      headers: baseUpdatedAt ? { "X-Conduct-Base-UpdatedAt": baseUpdatedAt } : {},
    }),
  renameWorkflow: (name, newName) =>
    request("POST", `/api/workflows/${encodeURIComponent(name)}/rename`, { body: { newName } }),
  copyWorkflow: (name, newName) =>
    request("POST", `/api/workflows/${encodeURIComponent(name)}/copy`, { body: { newName } }),
  deleteWorkflow: (name) => request("DELETE", `/api/workflows/${encodeURIComponent(name)}`),
  launchRun: (name, userPrompt, cwd) =>
    request("POST", `/api/workflows/${encodeURIComponent(name)}/runs`, { body: { userPrompt, cwd } }),
  stopRun: (id) => request("POST", `/api/runs/${encodeURIComponent(id)}/stop`, { body: {} }),
  resumeRun: (id) => request("POST", `/api/runs/${encodeURIComponent(id)}/resume`, { body: {} }),
  deleteRun: (id) => request("DELETE", `/api/runs/${encodeURIComponent(id)}`),
  updateLanguage: (language) => request("PATCH", "/api/settings", { body: { language } }),
};

function queryString(query) {
  if (!query) return "";
  const parts = [];
  for (const [key, value] of Object.entries(query)) {
    if (value) parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(value)}`);
  }
  return parts.length ? "?" + parts.join("&") : "";
}
