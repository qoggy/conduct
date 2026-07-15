// 前端镜像的结构校验：对齐 internal/workflow/validate.go 的 ValidateStructured，供编辑器做即时
// 反馈（画布红点 / 顶栏「可运行」状态 / 拖拽连边即时拒环）。这不是终裁——服务端 ValidateStructured
// 才是终裁（保存 422 逐字段标错）；本文件须与 Go 版规则同步维护（见 ui.md〈已知限制〉）。
//
// 返回的 Problem 形状与服务端一致：{path, code, params, message}。code/params 是稳定身份，message
// 由当前 UI 字典渲染；path 用于定位字段，故本地问题与服务端 422 问题能共用同一套渲染逻辑。

import { NODE_ID_START, NODE_ID_END, isAgent, isMarker, detectCycle, ancestors } from "./graph.js";
import { formatProblem } from "./i18n.js";

export const NODE_ID_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,63}$/;
const TEMPLATE_VAR_RE = /\\?\{\{([a-zA-Z_][\w.-]*)\}\}/g;
const KNOWN_SYS_VARS = new Set(["sys.userPrompt", "sys.cwd", "sys.runId"]);

// localProblems(def) → Problem[]。空定义 / 单条基础问题也走同一形状，调用方不必特判。
export function localProblems(def) {
  const nodes = def.nodes || [];
  const edges = def.edges || [];
  if (nodes.length === 0) {
    return [problem("nodes", "nodes_required")];
  }

  const problems = [];
  const indexByID = new Map();
  let startCount = 0,
    endCount = 0,
    agentCount = 0;
  nodes.forEach((node, i) => {
    if (node.id === NODE_ID_START) startCount++;
    else if (node.id === NODE_ID_END) endCount++;
    else agentCount++;
    if (!node.id) return; // 空 id 的必填错误在下方 agent 校验里报出
    if (indexByID.has(node.id)) {
      problems.push(problem(`nodes[${i}].id`, "duplicate_node_id", { id: node.id }));
      return;
    }
    indexByID.set(node.id, i);
  });
  const validNodeID = (id) => indexByID.has(id);

  if (startCount !== 1) problems.push(problem("nodes", "start_node_count", { count: startCount }));
  if (endCount !== 1) problems.push(problem("nodes", "end_node_count", { count: endCount }));
  if (agentCount === 0) problems.push(problem("nodes", "agent_node_required"));

  nodes.forEach((node, i) => {
    const path = `nodes[${i}]`;
    if (isMarker(node.id)) {
      if (node.displayName) problems.push(problem(path + ".displayName", "marker_field_not_empty", { id: node.id, field: "displayName" }));
      if (node.engine) problems.push(problem(path + ".engine", "marker_field_not_empty", { id: node.id, field: "engine" }));
      if (node.promptTemplate) problems.push(problem(path + ".promptTemplate", "marker_field_not_empty", { id: node.id, field: "promptTemplate" }));
      if (node.engineConfig) problems.push(problem(path + ".engineConfig", "marker_field_not_empty", { id: node.id, field: "engineConfig" }));
      return;
    }
    if (!node.id) problems.push(problem(path + ".id", "required_field", { field: "id" }));
    else if (!NODE_ID_RE.test(node.id)) problems.push(problem(path + ".id", "invalid_node_id", { id: node.id }));
    if (!node.displayName) problems.push(problem(path + ".displayName", "required_field", { field: "displayName" }));
    if (!node.engine) problems.push(problem(path + ".engine", "required_field", { field: "engine" }));
    if (!node.promptTemplate) problems.push(problem(path + ".promptTemplate", "required_field", { field: "promptTemplate" }));
    // engineConfig 的能力表校验（该引擎是否接受 model / effort 等）交服务端终裁：编辑器已按能力表
    // 条件渲染字段，正常操作路径不该产出非法组合，本地不重复这套判别联合。
  });

  const seen = new Set();
  edges.forEach((edge, i) => {
    const path = `edges[${i}]`;
    if (!edge.from || !edge.to) {
      problems.push(problem(path, "edge_endpoints_required"));
      return;
    }
    if (!validNodeID(edge.from)) problems.push(problem(path, "edge_from_node_not_found", { id: edge.from }));
    if (!validNodeID(edge.to)) problems.push(problem(path, "edge_to_node_not_found", { id: edge.to }));
    if (edge.from === edge.to) problems.push(problem(path, "self_edge", { from: edge.from, to: edge.to }));
    if (edge.from === NODE_ID_START && edge.to === NODE_ID_END) {
      problems.push(problem(path, "start_end_direct_edge"));
    } else {
      if (edge.to === NODE_ID_START) problems.push(problem(path, "edge_to_start"));
      if (edge.from === NODE_ID_END) problems.push(problem(path, "edge_from_end"));
    }
    const key = edge.from + "\u0000" + edge.to;
    if (seen.has(key)) problems.push(problem(path, "duplicate_edge", { from: edge.from, to: edge.to }));
    seen.add(key);
  });

  const inDeg = new Map(),
    outDeg = new Map();
  for (const e of edges) {
    outDeg.set(e.from, (outDeg.get(e.from) || 0) + 1);
    inDeg.set(e.to, (inDeg.get(e.to) || 0) + 1);
  }
  nodes.forEach((node, i) => {
    if (!isAgent(node.id)) return;
    const path = `nodes[${i}]`;
    if (!inDeg.get(node.id)) problems.push(problem(path, "node_missing_incoming_edge", { id: node.id }));
    if (!outDeg.get(node.id)) problems.push(problem(path, "node_missing_outgoing_edge", { id: node.id }));
  });

  const cycle = detectCycle(def);
  if (cycle) problems.push(problem("edges", "cycle_detected", { cycle: cycle.join("→") }));

  if (!cycle) {
    nodes.forEach((node, i) => {
      if (!isAgent(node.id)) return;
      const anc = ancestors(def, node.id);
      const path = `nodes[${i}].promptTemplate`;
      const text = node.promptTemplate || "";
      TEMPLATE_VAR_RE.lastIndex = 0;
      let m;
      while ((m = TEMPLATE_VAR_RE.exec(text))) {
        const [full, key] = m;
        if (full.startsWith("\\")) continue; // 转义 \{{x}} → 字面量，不校验
        if (key.startsWith("sys.")) {
          if (!KNOWN_SYS_VARS.has(key)) problems.push(problem(path, "unknown_system_variable", { key }));
          continue;
        }
        if (key === NODE_ID_START || key === NODE_ID_END) {
          problems.push(problem(path, "marker_node_reference", { id: key }));
          continue;
        }
        if (anc.has(key)) continue;
        if (validNodeID(key)) problems.push(problem(path, "non_ancestor_node_reference", { id: key }));
        else problems.push(problem(path, "node_reference_not_found", { id: key }));
      }
    });
  }

  return problems;
}

function problem(path, code, params) {
  const item = { path, code };
  if (params) item.params = params;
  item.message = formatProblem(item);
  return item;
}

// renameTemplateRef 把模板里的活引用 {{oldKey}} 改名为 {{newKey}}；转义的 \{{oldKey}} 是字面量、不动。
// 复用 TEMPLATE_VAR_RE 单一来源（与 render.go / validate.go 的 templateVariablePattern 同源），供编辑器改
// 节点 id 时级联同步下游模板引用。TEMPLATE_VAR_RE 带 /g，String.replace 会自行从头扫描并重置 lastIndex，
// 与本文件其它 exec 用法共享同一正则对象也互不干扰。
export function renameTemplateRef(template, oldKey, newKey) {
  return template.replace(TEMPLATE_VAR_RE, (full, key) => (!full.startsWith("\\") && key === oldKey ? `{{${newKey}}}` : full));
}

// nodeIdsWithProblems 把 Problem[] 里形如 nodes[i].xxx 的路径映射回节点 id 集合（供画布红点）。
export function nodeIdsWithProblems(def, problems) {
  const ids = new Set();
  for (const p of problems) {
    const m = /^nodes\[(\d+)\]/.exec(p.path);
    if (m && def.nodes[Number(m[1])]) ids.add(def.nodes[Number(m[1])].id);
  }
  return ids;
}
