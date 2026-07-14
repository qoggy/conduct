// 前端镜像的结构校验：对齐 internal/workflow/validate.go 的 ValidateStructured，供编辑器做即时
// 反馈（画布红点 / 顶栏「可运行」状态 / 拖拽连边即时拒环）。这不是终裁——服务端 ValidateStructured
// 才是终裁（保存 422 逐字段标错）；本文件须与 Go 版规则同步维护（见 ui.md〈已知限制〉）。
//
// 返回的 Problem 形状与服务端一致：{path, message}，path 为 "nodes[i].xxx" / "edges[i]" /
// "nodes" / "edges"，故本地问题与服务端 422 问题能共用同一套「点击定位到字段」的渲染逻辑。

import { NODE_ID_START, NODE_ID_END, isAgent, isMarker, detectCycle, ancestors } from "./graph.js";

export const NODE_ID_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,63}$/;
const TEMPLATE_VAR_RE = /\\?\{\{([a-zA-Z_][\w.-]*)\}\}/g;
const KNOWN_SYS_VARS = new Set(["sys.userPrompt", "sys.cwd", "sys.runId"]);

// localProblems(def) → Problem[]。空定义 / 单条基础问题也走同一形状，调用方不必特判。
export function localProblems(def) {
  const nodes = def.nodes || [];
  const edges = def.edges || [];
  if (nodes.length === 0) {
    return [{ path: "nodes", message: "不能为空，至少需要一个节点" }];
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
      problems.push({ path: `nodes[${i}].id`, message: `与前面的节点重复 "${node.id}"` });
      return;
    }
    indexByID.set(node.id, i);
  });
  const validNodeID = (id) => indexByID.has(id);

  if (startCount !== 1) problems.push({ path: "nodes", message: `须恰好含一个 START 标记节点，得到 ${startCount} 个` });
  if (endCount !== 1) problems.push({ path: "nodes", message: `须恰好含一个 END 标记节点，得到 ${endCount} 个` });
  if (agentCount === 0) problems.push({ path: "nodes", message: "至少需要一个 agent 节点（START / END 之外）" });

  nodes.forEach((node, i) => {
    const path = `nodes[${i}]`;
    if (isMarker(node.id)) {
      if (node.displayName) problems.push({ path: path + ".displayName", message: `标记节点 ${node.id} 的 displayName 必须为空` });
      if (node.engine) problems.push({ path: path + ".engine", message: `标记节点 ${node.id} 的 engine 必须为空` });
      if (node.promptTemplate) problems.push({ path: path + ".promptTemplate", message: `标记节点 ${node.id} 的 promptTemplate 必须为空` });
      if (node.engineConfig) problems.push({ path: path + ".engineConfig", message: `标记节点 ${node.id} 的 engineConfig 必须为空` });
      return;
    }
    if (!node.id) problems.push({ path: path + ".id", message: "必填" });
    else if (!NODE_ID_RE.test(node.id)) problems.push({ path: path + ".id", message: `"${node.id}" 非法（须匹配 ^[A-Za-z_][A-Za-z0-9_-]{0,63}$）` });
    if (!node.displayName) problems.push({ path: path + ".displayName", message: "必填" });
    if (!node.engine) problems.push({ path: path + ".engine", message: "必填" });
    if (!node.promptTemplate) problems.push({ path: path + ".promptTemplate", message: "必填" });
    // engineConfig 的能力表校验（该引擎是否接受 model / effort 等）交服务端终裁：编辑器已按能力表
    // 条件渲染字段，正常操作路径不该产出非法组合，本地不重复这套判别联合。
  });

  const seen = new Set();
  edges.forEach((edge, i) => {
    const path = `edges[${i}]`;
    if (!edge.from || !edge.to) {
      problems.push({ path, message: "from / to 不能为空" });
      return;
    }
    if (!validNodeID(edge.from)) problems.push({ path, message: `from 指向不存在的节点 "${edge.from}"` });
    if (!validNodeID(edge.to)) problems.push({ path, message: `to 指向不存在的节点 "${edge.to}"` });
    if (edge.from === edge.to) problems.push({ path, message: `禁止自环 ${edge.from}→${edge.to}` });
    if (edge.from === NODE_ID_START && edge.to === NODE_ID_END) {
      problems.push({ path, message: "禁止 START→END 直连（须过 ≥1 个 agent 节点）" });
    } else {
      if (edge.to === NODE_ID_START) problems.push({ path, message: "禁止边指向 START（START 无入边）" });
      if (edge.from === NODE_ID_END) problems.push({ path, message: "禁止边源自 END（END 无出边）" });
    }
    const key = edge.from + "\u0000" + edge.to;
    if (seen.has(key)) problems.push({ path, message: `重复边 ${edge.from}→${edge.to}` });
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
    if (!inDeg.get(node.id)) problems.push({ path, message: `agent 节点 "${node.id}" 无入边（须 ≥1 条，可来自 START）` });
    if (!outDeg.get(node.id)) problems.push({ path, message: `agent 节点 "${node.id}" 无出边（须 ≥1 条，可到 END）` });
  });

  const cycle = detectCycle(def);
  if (cycle) problems.push({ path: "edges", message: "检测到环 " + cycle.join("→") });

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
          if (!KNOWN_SYS_VARS.has(key)) problems.push({ path, message: `引用未知系统变量 {{${key}}}（仅支持 sys.userPrompt / sys.cwd / sys.runId）` });
          continue;
        }
        if (key === NODE_ID_START || key === NODE_ID_END) {
          problems.push({ path, message: `禁止引用标记节点 {{${key}}}（无产物）` });
          continue;
        }
        if (anc.has(key)) continue;
        if (validNodeID(key)) problems.push({ path, message: `引用非上游祖先节点 {{${key}}}（数据流须来自沿边可达的前驱）` });
        else problems.push({ path, message: `引用不存在的节点 {{${key}}}` });
      }
    });
  }

  return problems;
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
