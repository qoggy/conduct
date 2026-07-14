// DAG 画布：把 {nodes, edges} 摆成「START 顶 → agent 分层 → END 底」的纵向布局，并提供编辑页画布与
// 运行详情页进度画布共用的 SVG 构件（锚点 pill / 节点卡片 / 贝塞尔连线 / 箭头 marker）。
// 见 ui.md〈画布展示约定〉：两页画布展示一致，唯一差异是运行详情页节点有状态填充色、编辑页无填充。
//
// 布局是 Sugiyama 分层法（参考 mermaid 所用的 dagre）：分层 → 层内排序减交叉 → x 坐标向邻层中位数
// 对齐 → 跨层长边经「虚拟节点」逐层绕行。虚拟节点是让长边绕开中间层真实节点的关键——否则长边直连
// 起终点会径直穿过中间节点、被节点盖住（这正是之前"节点挡住边"的根因）。布局纯算坐标，构件用 svg.js。

import { buildAdjacency, isMarker, edgeKey } from "./graph.js";
import { svg, foreignHTML } from "./svg.js";
import { engineIconEl } from "./engines.js";

export const NODE_W = 140;
export const NODE_H = 46;
export const ANCHOR_W = 88;
export const ANCHOR_H = 28;

const H_GAP = 28; // 同层相邻节点最小水平间距（越大越拉宽画布 → 超列宽时整体缩得越小，故只微调）
// 相邻层垂直间距（= dagre 的 ranksep）：须 ≥ 边的横向位移，否则「横向跑得比竖向多」会把竖向优先的
// 贝塞尔压成折角。节点宽 140、同层相距一个节点宽时横向位移约 70，故取 72 让边以竖向为主、走线平滑。
const V_GAP = 72;
const H_PAD = 18; // 画布左右内边距
const TOP_PAD = 16;
const BOTTOM_PAD = 16;
const VNODE_HALF = 7; // 虚拟节点半宽：占一点位以分隔并行长边，又不过度挤占真实节点空间
const ORDER_SWEEPS = 6; // 层内排序（中位数启发式）上下来回扫的轮数
const XCOORD_SWEEPS = 8; // x 坐标向邻层对齐迭代轮数

// layoutPositions(def) → { positions: Map<id,{x,y,level,marker}>, width, height, routeOf }。
//   positions 只含真实节点中心点；routeOf: Map<edgeKey, {x,y}[]> 是每条边的折线路点（起点 → 若干
//   虚拟路点 → 终点，端点已裁到节点边并按多边端口散开），供 edgePathThrough 画平滑样条。
// 图暂时非法（环 / 与 START 不连通）时相关节点兜底摆到末尾一层，仍能载入去修，不崩、不静默丢弃。
export function layoutPositions(def) {
  const nodes = def.nodes || [];
  const edges = def.edges || [];
  const { rank, maxRank } = computeRanks(def);

  // 1) 跨层边（rv−ru>1）插虚拟节点，拆成逐层相邻的段；记录每条边的路由脊（节点 id 序列）。
  const vRank = new Map(); // 虚拟节点 id → rank
  const spineOf = new Map(); // edgeKey → [fromId, v…, toId]
  let vseq = 0;
  for (const e of edges) {
    const ru = rank.get(e.from);
    const rv = rank.get(e.to);
    if (ru == null || rv == null || rv - ru <= 1) {
      spineOf.set(edgeKey(e), [e.from, e.to]); // 相邻层 / 非法（缺 rank / 非严格向下）：直连两点
      continue;
    }
    const spine = [e.from];
    for (let rr = ru + 1; rr < rv; rr++) {
      const vid = "\u0000v" + vseq++; // \u0000 前缀保证不与真实节点 id 相撞
      vRank.set(vid, rr);
      spine.push(vid);
    }
    spine.push(e.to);
    spineOf.set(edgeKey(e), spine);
  }

  // 2) 段图邻接（真实 + 虚拟）：脊里每对相邻节点连一段，供排序与 x 对齐把长边拉直。
  const succ = new Map();
  const pred = new Map();
  const addSeg = (a, b) => {
    if (!succ.has(a)) succ.set(a, []);
    succ.get(a).push(b);
    if (!pred.has(b)) pred.set(b, []);
    pred.get(b).push(a);
  };
  for (const spine of spineOf.values()) {
    for (let i = 0; i + 1 < spine.length; i++) addSeg(spine[i], spine[i + 1]);
  }

  // 3) 每层节点表：真实按 nodes[] 序，虚拟追加。
  const rankLists = [];
  for (let r0 = 0; r0 <= maxRank; r0++) rankLists.push([]);
  for (const n of nodes) {
    const r0 = rank.get(n.id);
    if (r0 != null && r0 >= 0 && r0 <= maxRank) rankLists[r0].push(n.id);
  }
  for (const [vid, r0] of vRank) rankLists[r0].push(vid);

  // 4) 层内排序（中位数启发式）减交叉。 5) x 坐标向邻层中位数对齐（PAVA 保序 + 保最小间距）。
  const halfW = (id) => (vRank.has(id) ? VNODE_HALF : isMarker(id) ? ANCHOR_W / 2 : NODE_W / 2);
  orderRanks(rankLists, succ, pred);
  const x = assignX(rankLists, succ, pred, halfW);

  // 6) 每层 y：层高取该层最高节点（marker 28 / agent 46 / 虚拟 0）。
  const rankCenterY = [];
  let y = TOP_PAD;
  for (let r0 = 0; r0 <= maxRank; r0++) {
    const h = rankLists[r0].reduce((m, id) => Math.max(m, nodeHeight(id, vRank)), 0) || NODE_H;
    rankCenterY[r0] = y + h / 2;
    y += h + V_GAP;
  }
  const height = y - V_GAP + BOTTOM_PAD;

  // 归一化到左内边距，据最宽处算画布宽。
  let minX = Infinity;
  let maxX = -Infinity;
  for (const [id, xx] of x) {
    minX = Math.min(minX, xx - halfW(id));
    maxX = Math.max(maxX, xx + halfW(id));
  }
  if (!isFinite(minX)) {
    minX = 0;
    maxX = NODE_W;
  }
  const shift = H_PAD - minX;
  const width = maxX - minX + 2 * H_PAD;

  // 7) 真实节点 positions + 全节点（含虚拟）中心坐标 coordOf（供路由取路点）。
  const positions = new Map();
  const coordOf = new Map();
  for (const n of nodes) {
    const r0 = rank.get(n.id);
    if (r0 == null || r0 < 0 || r0 > maxRank) continue;
    const p = { x: x.get(n.id) + shift, y: rankCenterY[r0], level: r0, marker: isMarker(n.id) };
    positions.set(n.id, p);
    coordOf.set(n.id, p);
  }
  for (const [vid, r0] of vRank) coordOf.set(vid, { x: x.get(vid) + shift, y: rankCenterY[r0], marker: false });

  // 8) routeOf：脊 → 坐标路点，端点裁到节点边并按多边端口散开。
  const routeOf = buildRoutes(edges, spineOf, coordOf);

  return { positions, width, height: Math.max(height, TOP_PAD + NODE_H), routeOf };
}

// computeRanks：longest-path 分层（START 无入边 → rank 0，节点 rank = max(前驱 rank)+1）。环 / 与
// START 不连通而未获 rank 者兜底到末尾一层，保证仍可渲染去修。返回 { rank: Map<id,int>, maxRank }。
function computeRanks(def) {
  const { succ, pred } = buildAdjacency(def);
  const nodes = def.nodes || [];
  const indeg = new Map();
  for (const n of nodes) indeg.set(n.id, (pred.get(n.id) || []).length);
  const rank = new Map();
  const queue = [];
  for (const n of nodes) {
    if (indeg.get(n.id) === 0) {
      rank.set(n.id, 0);
      queue.push(n.id);
    }
  }
  while (queue.length) {
    const cur = queue.shift();
    for (const nx of succ.get(cur) || []) {
      const cand = rank.get(cur) + 1;
      if (cand > (rank.has(nx) ? rank.get(nx) : -1)) rank.set(nx, cand);
      indeg.set(nx, indeg.get(nx) - 1);
      if (indeg.get(nx) === 0) queue.push(nx);
    }
  }
  let maxRank = 0;
  for (const v of rank.values()) maxRank = Math.max(maxRank, v);
  let hasUnranked = false;
  for (const n of nodes) {
    if (!rank.has(n.id)) {
      rank.set(n.id, maxRank + 1); // 环 / 不连通：兜底到末尾一层
      hasUnranked = true;
    }
  }
  if (hasUnranked) maxRank += 1;
  return { rank, maxRank };
}

// orderRanks：中位数启发式减交叉——上下来回扫，每层按其节点在相邻（已定序）层里邻居的中位位置排序。
// 无邻居的节点用当前 index 兜底、不乱窜。原地改 rankLists。
function orderRanks(rankLists, succ, pred) {
  const posIn = new Map();
  const reindex = () => {
    for (const list of rankLists) list.forEach((id, i) => posIn.set(id, i));
  };
  reindex();
  for (let sweep = 0; sweep < ORDER_SWEEPS; sweep++) {
    const down = sweep % 2 === 0;
    const order = down ? range(1, rankLists.length) : range(rankLists.length - 2, -1, -1);
    for (const r0 of order) {
      const neigh = down ? pred : succ;
      const list = rankLists[r0];
      const key = new Map();
      list.forEach((id, i) => {
        const ns = (neigh.get(id) || []).map((n) => posIn.get(n)).filter((v) => v != null);
        key.set(id, ns.length ? median(ns) : i);
      });
      list.sort((a, b) => key.get(a) - key.get(b));
      reindex();
    }
  }
}

// assignX：等距打底后多轮向邻层中位数对齐（把链 / 长边拉直、汇聚对称），收尾按父对齐成列。
// 每次对齐用 placeWithSeparation 在「层内保序 + 最小间距」约束下尽量贴近期望位置。返回 Map<id,x>。
function assignX(rankLists, succ, pred, halfW) {
  const x = new Map();
  const sepList = (list) => list.map((id, i) => (i === 0 ? 0 : halfW(list[i - 1]) + H_GAP + halfW(id)));
  for (const list of rankLists) {
    let cx = 0;
    list.forEach((id, i) => {
      if (i > 0) cx += halfW(list[i - 1]) + H_GAP + halfW(id);
      x.set(id, cx);
    });
  }
  const alignTo = (list, neighOf) => {
    const desired = list.map((id) => {
      const ns = (neighOf.get(id) || []).map((n) => x.get(n)).filter((v) => v != null);
      return ns.length ? median(ns) : x.get(id);
    });
    const placed = placeWithSeparation(desired, sepList(list));
    list.forEach((id, i) => x.set(id, placed[i]));
  };
  for (let sweep = 0; sweep < XCOORD_SWEEPS; sweep++) {
    const down = sweep % 2 === 0;
    const order = down ? range(0, rankLists.length) : range(rankLists.length - 1, -1, -1);
    for (const r0 of order) {
      if (rankLists[r0].length) alignTo(rankLists[r0], down ? pred : succ);
    }
  }
  // 收尾成列：自顶向下把每个节点对齐到其父（单父节点即落到父那一列，避免被末端往中间拽偏、错列），
  // 再让源点（rank 0，无父）居中于其子。这一步是「列对齐」观感的关键（对齐 mermaid / dagre）。
  for (const r0 of range(1, rankLists.length)) {
    if (rankLists[r0].length) alignTo(rankLists[r0], pred);
  }
  if (rankLists[0] && rankLists[0].length) alignTo(rankLists[0], succ);
  return x;
}

// placeWithSeparation：给定各点期望坐标与相邻最小间距（sep[0]=0），在「保持顺序 + 满足间距」下求最小
// 二乘最优位置。做法：减去累计最小间距把「带间距约束」化为「单调非减约束」，PAVA（保序回归）求解。
function placeWithSeparation(desired, sep) {
  const n = desired.length;
  const cum = new Array(n);
  let acc = 0;
  for (let i = 0; i < n; i++) {
    acc += sep[i];
    cum[i] = acc;
  }
  const target = desired.map((d, i) => d - cum[i]);
  const iso = pava(target);
  return iso.map((v, i) => v + cum[i]);
}

// pava：保序回归（pool adjacent violators），返回最贴近 t 的非减序列。合并违序相邻块为其均值。
function pava(t) {
  const blocks = []; // { mean, size }
  for (const v of t) {
    let mean = v;
    let size = 1;
    while (blocks.length && blocks[blocks.length - 1].mean >= mean) {
      const b = blocks.pop();
      mean = (mean * size + b.mean * b.size) / (size + b.size);
      size += b.size;
    }
    blocks.push({ mean, size });
  }
  const out = [];
  for (const b of blocks) for (let i = 0; i < b.size; i++) out.push(b.mean);
  return out;
}

// buildRoutes：把每条边的脊映射成坐标路点。端点裁到节点上/下沿，且同一真实节点的多条出边沿下沿散开、
// 多条入边沿上沿散开（按脊相邻路点 x 排序 → 同端不交叉）。中间虚拟路点取其中心。返回 Map<edgeKey,points>。
function buildRoutes(edges, spineOf, coordOf) {
  const peerX = (spine, atStart) => {
    const c = coordOf.get(atStart ? spine[1] : spine[spine.length - 2]);
    return c ? c.x : 0;
  };
  const port = (groupKeyOf, atStart) => {
    const groups = new Map();
    for (const e of edges) {
      const spine = spineOf.get(edgeKey(e));
      if (!spine) continue;
      const gk = groupKeyOf(e);
      if (!groups.has(gk)) groups.set(gk, []);
      groups.get(gk).push({ key: edgeKey(e), spine });
    }
    const off = new Map();
    for (const [id, items] of groups) {
      const c = coordOf.get(id);
      if (!c) continue;
      items.sort((a, b) => peerX(a.spine, atStart) - peerX(b.spine, atStart));
      const offsets = spread(items.length, c.marker ? ANCHOR_W : NODE_W);
      items.forEach((it, i) => off.set(it.key, offsets[i]));
    }
    return off;
  };
  const outPort = port((e) => e.from, true);
  const inPort = port((e) => e.to, false);

  const routeOf = new Map();
  for (const e of edges) {
    const key = edgeKey(e);
    const spine = spineOf.get(key);
    if (!spine) continue;
    const from = coordOf.get(spine[0]);
    const to = coordOf.get(spine[spine.length - 1]);
    if (!from || !to) continue;
    const points = [{ x: from.x + (outPort.get(key) || 0), y: from.y + (from.marker ? ANCHOR_H / 2 : NODE_H / 2) }];
    for (let i = 1; i + 1 < spine.length; i++) {
      const c = coordOf.get(spine[i]);
      if (c) points.push({ x: c.x, y: c.y });
    }
    points.push({ x: to.x + (inPort.get(key) || 0), y: to.y - (to.marker ? ANCHOR_H / 2 : NODE_H / 2) });
    routeOf.set(key, points);
  }
  return routeOf;
}

function nodeHeight(id, vRank) {
  if (vRank.has(id)) return 0;
  return isMarker(id) ? ANCHOR_H : NODE_H;
}

function range(start, endExclusive, step = 1) {
  const out = [];
  if (step > 0) for (let i = start; i < endExclusive; i += step) out.push(i);
  else for (let i = start; i > endExclusive; i += step) out.push(i);
  return out;
}

function median(arr) {
  if (!arr.length) return 0;
  const s = [...arr].sort((a, b) => a - b);
  const m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

// spread 返回 count 个连接点相对节点中心的水平偏移（居中、等距、升序）；count≤1 时为 [0]（居中）。
// 跨度按节点宽收敛，避免端口落到卡片圆角外。
function spread(count, width) {
  if (count <= 1) return [0];
  const span = Math.min(width * 0.55, (count - 1) * 30);
  const offsets = [];
  for (let i = 0; i < count; i++) offsets.push((i / (count - 1) - 0.5) * span);
  return offsets;
}

// nodeAt 命中测试：返回中心框覆盖 (x,y) 的节点 id（供拖拽连边时判断落在哪个节点上），无则 null。
export function nodeAt(positions, x, y) {
  for (const [id, p] of positions) {
    const halfW = p.marker ? ANCHOR_W / 2 : NODE_W / 2;
    const halfH = p.marker ? ANCHOR_H / 2 : NODE_H / 2;
    if (x >= p.x - halfW && x <= p.x + halfW && y >= p.y - halfH && y <= p.y + halfH) return id;
  }
  return null;
}

// curveD 生成一条竖向为主的三次贝塞尔路径 d（控制点沿垂直方向偏置，走线自然向下弯）。
// 供 edgePath（拖拽临时线）与两点直连边共用。
export function curveD(sx, sy, ex, ey) {
  const dy = ey - sy;
  if (Math.abs(dy) < 1) return `M${r(sx)},${r(sy)} L${r(ex)},${r(ey)}`;
  const c1y = sy + dy * 0.5;
  const c2y = ey - dy * 0.5;
  return `M${r(sx)},${r(sy)} C${r(sx)},${r(c1y)} ${r(ex)},${r(c2y)} ${r(ex)},${r(ey)}`;
}

// edgePathThrough 把一串路点画成平滑竖向样条（Catmull-Rom → 三次贝塞尔）。2 点退化为 curveD，
// ≥3 点（长边经虚拟路点）则顺次穿过所有路点、平滑绕行。
export function edgePathThrough(points) {
  if (!points || points.length < 2) return "";
  if (points.length === 2) return curveD(points[0].x, points[0].y, points[1].x, points[1].y);
  let d = `M${r(points[0].x)},${r(points[0].y)}`;
  for (let i = 0; i + 1 < points.length; i++) {
    const p0 = points[i - 1] || points[i];
    const p1 = points[i];
    const p2 = points[i + 1];
    const p3 = points[i + 2] || p2;
    const c1x = p1.x + (p2.x - p0.x) / 6;
    const c1y = p1.y + (p2.y - p0.y) / 6;
    const c2x = p2.x - (p3.x - p1.x) / 6;
    const c2y = p2.y - (p3.y - p1.y) / 6;
    d += ` C${r(c1x)},${r(c1y)} ${r(c2x)},${r(c2y)} ${r(p2.x)},${r(p2.y)}`;
  }
  return d;
}

// edgePath 生成两节点中点间的连线 d：仅供拖拽临时线 / 成环红闪等瞬时单边使用。
export function edgePath(fromPos, toPos) {
  const fromHalfH = fromPos.marker ? ANCHOR_H / 2 : NODE_H / 2;
  const toHalfH = toPos.marker ? ANCHOR_H / 2 : NODE_H / 2;
  return curveD(fromPos.x, fromPos.y + fromHalfH, toPos.x, toPos.y - toHalfH);
}

export const ARROW_ID = "dag-arrow";
export const ARROW_BAD_ID = "dag-arrow-bad";

// arrowMarkers 返回一个 <defs>，含正常边与拒绝边（成环）两个箭头 marker，供画布 <svg> 复用。
// marker 填色走 class（.dag-arrowhead / .dag-arrowhead-bad），便于集中在 style.css 调色。
// markerUnits=userSpaceOnUse：箭头尺寸固定 9px（不随边 stroke-width 放大成钝三角），仍随画布整体
// 缩放而缩放。refX=9 把参考点放在近尖端，让箭头尖正落在目标节点上沿、不戳进卡片内部。
export function arrowMarkers() {
  const mk = (id, cls) =>
    svg(
      "marker",
      { id, viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 9, markerHeight: 9, markerUnits: "userSpaceOnUse", orient: "auto-start-reverse" },
      svg("path", { d: "M1,1.5 L9,5 L1,8.5 Z", class: cls }),
    );
  return svg("defs", null, mk(ARROW_ID, "dag-arrowhead"), mk(ARROW_BAD_ID, "dag-arrowhead-bad"));
}

// anchorEl 构建 START / END 虚线锚点 pill（沿用现网 .anchor 观感，SVG 版类名 .an）。
// opts.ok=true → 绿底（START 越过 / END 整体完成，见运行详情页）。
export function anchorEl(id, pos, opts = {}) {
  return svg(
    "g",
    { class: "dag-anchor", "data-node-id": id },
    svg("rect", {
      class: "an" + (opts.ok ? " an-ok" : ""),
      x: pos.x - ANCHOR_W / 2,
      y: pos.y - ANCHOR_H / 2,
      width: ANCHOR_W,
      height: ANCHOR_H,
      rx: ANCHOR_H / 2,
    }),
    svg("text", { class: "an-t", x: pos.x, y: pos.y + 3.5, "text-anchor": "middle" }, id),
  );
}

// nodeEl 构建一张 agent 节点卡片 <g>（左侧 engine 图标 + displayName，只显 displayName、不显 id）。
//   opts.selected   选中态（节点边缘黑描边，排在状态色之后胜出）
//   opts.stateClass 状态填充类（运行详情页：nb-ok / nb-run / nb-wait / nb-fail；编辑页：""｜nb-err）
//   opts.faint      displayName 淡化（待运行节点）
//   opts.statusLabel 节点下方的小字（耗时等）；opts.statusClass 其配色类
// 返回的 <g> 带 data-node-id，页面据此绑定选中 / 拖拽连边 / 点开详情等交互。
export function nodeEl(node, pos, opts = {}) {
  const left = pos.x - NODE_W / 2;
  const ICON = 16; // engine 图标边长
  const GAP = 6; // 图标与名称间距
  // 「图标 + 名称」作为整体在节点框内水平居中：量出内容总宽，以节点中心 pos.x 对称摆放。
  const label = fitLabel(node.displayName || node.id);
  const contentW = ICON + GAP + label.width;
  const iconX = pos.x - contentW / 2;
  const labelX = iconX + ICON + GAP;
  // 有状态小字（运行详情两行：名称在上、时间在下）时，图标上移与名称同一行，而非竖直居中于两行之间；
  // 单行（编辑页）则图标竖直居中于节点。
  const nameY = opts.statusLabel ? pos.y - 3 : pos.y + 4;
  const iconY = opts.statusLabel ? pos.y - 15 : pos.y - 8;
  const rectClass = "nb" + (opts.stateClass ? " " + opts.stateClass : "") + (opts.selected ? " nb-sel" : "");
  const children = [
    svg("title", null, (node.displayName || node.id) + " (" + node.id + ")"),
    svg("rect", { class: rectClass, x: left, y: pos.y - NODE_H / 2, width: NODE_W, height: NODE_H, rx: 9 }),
    foreignHTML(iconX, iconY, 16, 16, engineIconEl(node.engine)),
    svg("text", { class: "nm" + (opts.faint ? " nm-faint" : ""), x: labelX, y: nameY }, label.text),
  ];
  if (opts.statusLabel) {
    // 耗时小字水平居中于节点（以节点中心 pos.x 为锚），不跟 displayName 的左起点 labelX 对齐。
    children.push(svg("text", { class: "nst" + (opts.statusClass ? " " + opts.statusClass : ""), x: pos.x, y: pos.y + 12, "text-anchor": "middle" }, opts.statusLabel));
  }
  return svg("g", { class: "dnode", "data-node-id": node.id }, ...children);
}

// fitLabel 按像素预算截断标签（CJK 记作约 12.5px、其余约 7px），超出补省略号；全名收在 <title>。
// 返回 { text, width }：text 是截断后的显示串，width 是其像素宽——供 nodeEl 把「图标 + 名称」整体
// 在节点框内水平居中定位（截断时补的省略号按 7px 记）。
function fitLabel(text) {
  const budget = 92;
  let w = 0;
  let out = "";
  for (const ch of text) {
    const cw = /[⺀-鿿　-ヿ＀-￯]/.test(ch) ? 12.5 : 7;
    if (w + cw > budget) return { text: out + "…", width: w + 7 };
    w += cw;
    out += ch;
  }
  return { text: out, width: w };
}

function r(n) {
  return Math.round(n * 10) / 10;
}
