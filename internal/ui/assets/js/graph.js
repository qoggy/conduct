// DAG 图算法：镜像 internal/workflow/graph.go，供编辑器做即时校验 / 拖拽连边环检测 / 占位符祖先
// 建议 / 整理连线（传递归约）。运行详情页的进度画布另有自己的分层布局，见 dag-layout.js。
//
// 输入统一为 { nodes: [{id, ...}], edges: [{from, to}] } 形状（含 START/END），纯函数、不改入参。
// 前端这份镜像须与 Go 版规则保持同步（见 ui.md〈已知限制〉：前端镜像校验可能与内核漂移）。

export const NODE_ID_START = "START";
export const NODE_ID_END = "END";

export function isMarker(id) {
  return id === NODE_ID_START || id === NODE_ID_END;
}

export function isAgent(id) {
  return !isMarker(id);
}

// buildAdjacency 返回后继表 succ / 前驱表 pred（Map<id, id[]>），只收录 edges 里出现的端点键。
export function buildAdjacency(def) {
  const succ = new Map();
  const pred = new Map();
  for (const edge of def.edges || []) {
    pushInto(succ, edge.from, edge.to);
    pushInto(pred, edge.to, edge.from);
  }
  return { succ, pred };
}

function pushInto(map, key, value) {
  if (!map.has(key)) map.set(key, []);
  map.get(key).push(value);
}

// detectCycle：DFS 三色标记探测环，命中返回一条环路径（如 ["a","b","a"]，首尾同 id 便于打印
// "a→b→a"）；无环返回 null。按 nodes 顺序起 DFS，结果确定。
export function detectCycle(def) {
  const { succ } = buildAdjacency(def);
  const WHITE = 0,
    GRAY = 1,
    BLACK = 2;
  const color = new Map();
  const stack = [];

  function dfs(id) {
    color.set(id, GRAY);
    stack.push(id);
    for (const next of succ.get(id) || []) {
      const c = color.get(next) || WHITE;
      if (c === GRAY) {
        const i = stack.indexOf(next);
        return [...stack.slice(i), next];
      }
      if (c === WHITE) {
        const cycle = dfs(next);
        if (cycle) return cycle;
      }
    }
    stack.pop();
    color.set(id, BLACK);
    return null;
  }

  for (const node of def.nodes || []) {
    if ((color.get(node.id) || WHITE) === WHITE) {
      const cycle = dfs(node.id);
      if (cycle) return cycle;
    }
  }
  return null;
}

// wouldCreateCycle：模拟加入 edge 后是否成环（供拖拽连边即时判断），不修改 def。
export function wouldCreateCycle(def, edge) {
  const probe = { nodes: def.nodes, edges: [...(def.edges || []), edge] };
  return detectCycle(probe) !== null;
}

// ancestors：沿边反向可达的全部前驱 id 集合（不含自身）。与 Go 版不同：本实现自带 visited 防御，
// 图暂时有环（编辑中间态、尚未通过校验）也不会死循环——Go 版假定调用前已过 DetectCycle，但编辑器
// 需要在校验通过之前也能安全渲染画布 / 算占位符建议，故加此防御（前端比 Go 版更宽容，是刻意的
// UI 侧加固，不影响合法图的结果）。
export function ancestors(def, id) {
  const { pred } = buildAdjacency(def);
  const out = new Set();
  const visiting = new Set();
  const visit = (node) => {
    if (visiting.has(node)) return; // 防环：已在当前访问链上，不再下钻
    visiting.add(node);
    for (const parent of pred.get(node) || []) {
      if (!out.has(parent)) {
        out.add(parent);
        visit(parent);
      }
    }
  };
  visit(id);
  return out;
}

// edgeKey：边的去重键（node id 不含 \u0000，故安全）。供 dag-layout / run-detail / editor 复用。
export function edgeKey(edge) {
  return edge.from + "\u0000" + edge.to;
}

// redundantEdges：DAG 传递归约——找出全部"冗余边"。边 from→to 冗余，当且仅当 from 另有后继 w（w≠to）
// 能到达 to，即存在 from→w→…→to 的绕行路径；此时直连 from→to 不改变任何可达性，可安全删除
// （例：START→node2 因 START→node1→node2 而多余）。返回冗余边数组 [{from, to}]。传递归约保持传递
// 闭包不变，故不会破坏单源单汇 / 节点度约束，且多条冗余边可同时删除。要求无环（有环时传递归约无
// 定义）；reach 记忆化时先占位空集，编辑中间态即便暂时有环也只拿到部分结果而非无限递归——结果仅
// 在无环图上具传递归约语义，此防御只保证不死循环。
export function redundantEdges(def) {
  const { succ } = buildAdjacency(def);
  // reach(x)：x 沿边可达的节点集合（≥1 步，不含 x 自身）。
  const memo = new Map();
  const reach = (x) => {
    const cached = memo.get(x);
    if (cached) return cached;
    const out = new Set();
    memo.set(x, out); // 先登记空集占位：有环时回引拿到部分结果而非无限递归
    for (const next of succ.get(x) || []) {
      out.add(next);
      for (const far of reach(next)) out.add(far);
    }
    return out;
  };
  const redundant = [];
  for (const e of def.edges || []) {
    const outs = succ.get(e.from) || [];
    // 存在另一后继 w 能到达 e.to → e.from→e.to 有绕行路径，冗余。
    if (outs.some((w) => w !== e.to && reach(w).has(e.to))) redundant.push({ from: e.from, to: e.to });
  }
  return redundant;
}
