// 工作流编辑器页（#/workflows/:name）：show（载入）+ edit（整体替换保存）的聚合工作台。
// 两栏：左侧 DAG 画布（节点 + 边的图，拖拽连边 / 增删节点 / 选中），右侧检查器（选中节点的完整配置）。
// 另有 JSON 视图（定义主体 {nodes, edges} 源码编辑，与画布双向同步）。保存走 PUT 整体替换，
// 校验不过 422 逐条锚定字段；乐观并发 409 弹「覆盖 / 重载」。

import { h, mount, toast } from "../dom.js";
import { api, ApiError } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTime } from "../format.js";
import { capabilityOf, loadEngines } from "../engines.js";
import { createPromptEditor } from "../prompt-editor.js";
import { createCodeEditor } from "../code-editor.js";
import { engineSelect, listSelect, closeOnOutsideClick } from "../custom-select.js";
import { openModal } from "../modal.js";
import { openLaunchDialog, openRenameDialog } from "../dialogs.js";
import { loadingView, errorView } from "./common.js";
import { NODE_ID_START, NODE_ID_END, isAgent, isMarker, ancestors, wouldCreateCycle, edgeKey, redundantEdges } from "../graph.js";
import { localProblems, nodeIdsWithProblems, NODE_ID_RE, renameTemplateRef } from "../validate.js";
import { svg, clientToLocal } from "../svg.js";
import {
  layoutPositions,
  edgePath,
  edgePathThrough,
  curveD,
  arrowMarkers,
  anchorEl,
  nodeEl,
  nodeAt,
  NODE_W,
  NODE_H,
  ANCHOR_H,
  ARROW_ID,
  ARROW_BAD_ID,
} from "../dag-layout.js";

export async function renderEditorPage(outlet, name) {
  mount(outlet, loadingView());
  let record;
  try {
    // 引擎能力表与定义并行拉；定义即使语义非法也要能载入去修（编辑器不做载入期语义校验）。
    [, record] = await Promise.all([loadEngines(), api.getWorkflow(name)]);
  } catch (err) {
    mount(outlet, errorView(err));
    return;
  }
  new Editor(outlet, name, record).mount();
}

class Editor {
  constructor(outlet, name, record) {
    this.outlet = outlet;
    this.name = name;
    this.record = record; // 完整记录 {name, createdAt, updatedAt, definition:{nodes,edges}}
    this.def = record.definition; // 编辑对象 = 定义主体 {nodes, edges}
    this.baseUpdatedAt = record.updatedAt; // 乐观并发基线（顶层 updatedAt）
    this.selId = this.firstAgentId(); // 选中的节点 id（仅 agent 可选）
    this.view = "form";
    this.dirty = false;
    this.errors = []; // 上次保存的字段级校验错误 [{path, message}]
  }

  mount() {
    this.root = h("div", { class: "page page-wide" });
    mount(this.outlet, this.root);
    this.renderAll();
  }

  // ---- 选中 / 节点辅助 ----
  firstAgentId() {
    const n = (this.def.nodes || []).find((n) => isAgent(n.id));
    return n ? n.id : null;
  }
  agentCount() {
    return (this.def.nodes || []).filter((n) => isAgent(n.id)).length;
  }
  selectedNode() {
    return (this.def.nodes || []).find((n) => n.id === this.selId) || null;
  }
  selectedIndex() {
    return (this.def.nodes || []).findIndex((n) => n.id === this.selId);
  }

  markDirty() {
    this.dirty = true;
    if (this.saveDot) {
      this.saveDot.className = "savedot unsaved";
      this.saveDot.textContent = i18n.edUnsaved;
    }
  }

  // 带校验错误的节点 id 集合：镜像本地校验 + 上次保存的服务端 422（供画布节点红描边）。
  errorNodeIds() {
    const ids = nodeIdsWithProblems(this.def, this.localProblems || []);
    for (const e of this.errors) {
      const m = /^nodes\[(\d+)\]/.exec(e.path);
      if (m && this.def.nodes[Number(m[1])]) ids.add(this.def.nodes[Number(m[1])].id);
    }
    return ids;
  }

  // refreshRunnable 原地更新顶栏「可运行」徽标（类名 + 文案），不重建顶栏——供 renderFlow 在结构变化后
  // 同步徽标，规避 commitRename 刻意不走 renderAll（否则重建保存按钮会吞掉紧随的保存点击）导致的徽标滞后。
  refreshRunnable() {
    if (!this.runnableBadge) return;
    const runnable = this.localProblems.length === 0;
    this.runnableBadge.className = runnable ? "runnable ok" : "runnable bad";
    this.runnableBadge.textContent = runnable ? i18n.edRunnable : i18n.edNotRunnable;
  }

  renderAll() {
    this.localProblems = localProblems(this.def); // 前端镜像校验（画布红点 / 可运行状态）
    mount(
      this.root,
      this.headBar(),
      this.errors.length ? this.errorPanel() : null,
      this.view === "form" ? this.formBody() : this.jsonBody(),
    );
  }

  // ---- 顶部条 ----
  headBar() {
    this.saveDot = h(
      "span",
      { class: this.dirty ? "savedot unsaved" : "savedot" },
      this.dirty ? i18n.edUnsaved : i18n.edSaved,
    );
    const runnable = this.localProblems.length === 0;
    // 持引用而非内联：renderFlow 在结构变化后经 refreshRunnable 原地更新徽标，无需重建整个顶栏。
    this.runnableBadge = h("span", { class: runnable ? "runnable ok" : "runnable bad" }, runnable ? i18n.edRunnable : i18n.edNotRunnable);
    return h(
      "div",
      { class: "edithead" },
      h("span", { class: "wfname" }, this.name),
      h("span", { class: "ghost", style: { padding: "2px 7px", fontSize: "12px" }, onClick: () => this.openRename() }, i18n.rename),
      h("span", { class: "meta" }, `${i18n.edNodesTpl(this.agentCount())} · ${i18n.edUpdatedTpl(fmtTime(this.record.updatedAt))}`),
      this.saveDot,
      this.runnableBadge,
      h("span", { class: "grow" }),
      h("span", { class: "ghost", onClick: () => navigate(`/runs?workflow=${encodeURIComponent(this.name)}`) }, i18n.edRunHistory),
      this.viewToggle(),
      h("button", { class: "btn", onClick: () => this.openLaunch() }, i18n.actRun),
      h("button", { class: "btn btn-ink", onClick: () => this.save() }, i18n.save),
    );
  }

  viewToggle() {
    return h(
      "div",
      { class: "seg" },
      h("span", { class: this.view === "form" ? "seg-i seg-on" : "seg-i", onClick: () => this.switchView("form") }, i18n.edViewForm),
      h("span", { class: this.view === "json" ? "seg-i seg-on" : "seg-i", onClick: () => this.switchView("json") }, i18n.edViewJSON),
    );
  }

  switchView(target) {
    if (target === this.view) return;
    if (this.view === "json" && target === "form") {
      // JSON → 表单：先 parse，语法错误则阻止切换并就地标错（DisallowUnknownFields 的终裁在服务端保存时）。
      const parsed = this.parseJsonEditor();
      if (!parsed) return;
      this.def = parsed.definition ? parsed.definition : parsed; // 容忍粘进整条记录：解包 definition
      // 选中节点可能已被 JSON 删除，收敛到一个仍存在的 agent（否则检查器空态）。
      if (!this.selectedNode()) this.selId = this.firstAgentId();
    }
    this.view = target;
    this.errors = [];
    this.renderAll();
  }

  // ---- 错误面板 ----
  errorPanel() {
    return h(
      "div",
      { class: "errpanel" },
      h("h5", {}, i18n.edErrTitle),
      ...this.errors.map((e) =>
        h(
          "div",
          {
            class: "errline",
            onClick: () => {
              const m = /^nodes\[(\d+)\]/.exec(e.path);
              if (m && this.def.nodes[Number(m[1])]) {
                this.selId = this.def.nodes[Number(m[1])].id;
                this.view = "form";
                this.renderAll();
              }
            },
          },
          `${e.path}: ${e.message}`,
        ),
      ),
    );
  }

  // ---- 表单视图：两栏（左画布 / 右检查器） ----
  formBody() {
    this.canvasHost = h("div", { class: "canvashost" }); // svg 挂载点：renderFlow 只重建它，不动工具条
    this.flowCol = h("div", { class: "canvascol" }, this.canvasTools(), this.canvasHost);
    this.inspCol = h("div", { class: "insp" });
    this.renderFlow();
    this.renderInspector();
    return h("div", { class: "body2" }, this.flowCol, this.inspCol);
  }

  // 画布工具条：紧贴画布上方，承载对图整体的操作——增节点 / 整理连线（传递归约删冗余边）。
  canvasTools() {
    return h(
      "div",
      { class: "canvastools" },
      h("button", { class: "btn", onClick: () => this.addNode() }, i18n.addNode),
      h("button", { class: "btn", onClick: () => this.optimizeEdges() }, i18n.edgeOpt),
    );
  }

  // ---- DAG 画布 ----
  renderFlow() {
    // 每次重排前重算本地校验：拖拽连边 / 删边 / 改名 / 改提示词都只走 renderFlow（不走 renderAll），须在此
    // 刷新画布红描边（errorNodeIds 依赖 localProblems）与顶栏「可运行」徽标，否则即时反馈会滞后一步交互。
    this.localProblems = localProblems(this.def);
    this.refreshRunnable();
    const layout = layoutPositions(this.def);
    this.positions = layout.positions;
    const routeOf = layout.routeOf;
    const errIds = this.errorNodeIds();

    const edgeEls = [];
    for (const edge of this.def.edges || []) {
      const pts = routeOf.get(edgeKey(edge));
      if (!pts) continue; // 端点指向不存在节点（手改 JSON 所致）：连线画不出，交错误面板呈现
      const d = edgePathThrough(pts);
      edgeEls.push(
        svg(
          "g",
          { class: "edgewrap" },
          svg("path", { class: "edge", d, "marker-end": `url(#${ARROW_ID})` }),
          svg("path", { class: "edge-hit", d, onClick: (e) => { e.stopPropagation(); this.removeEdge(edge); } }),
          svg("title", null, i18n.edgeClickDelete),
        ),
      );
    }

    const markerEls = [];
    if (this.positions.has(NODE_ID_START)) {
      const g = anchorEl(NODE_ID_START, this.positions.get(NODE_ID_START));
      g.classList.add("dsource"); // 可作为连边起点（扇出）
      g.addEventListener("pointerdown", (e) => this.onNodePointerDown(e, NODE_ID_START));
      markerEls.push(g);
    }
    if (this.positions.has(NODE_ID_END)) markerEls.push(anchorEl(NODE_ID_END, this.positions.get(NODE_ID_END)));

    const nodeEls = [];
    for (const node of this.def.nodes || []) {
      if (!isAgent(node.id)) continue;
      const pos = this.positions.get(node.id);
      if (!pos) continue;
      const g = nodeEl(node, pos, { selected: node.id === this.selId, stateClass: errIds.has(node.id) ? "nb-err" : "" });
      g.appendChild(this.deleteAffordance(node, pos));
      g.addEventListener("pointerdown", (e) => this.onNodePointerDown(e, node.id));
      nodeEls.push(g);
    }

    // width/height 用布局的自然像素尺寸——配合 CSS max-width:100% + height:auto，画布按真实尺寸
    // 呈现、仅在超出列宽时才等比缩小，不会因节点少而被拉伸放大（否则单节点图会被撑到两倍大）。
    this.svgRoot = svg(
      "svg",
      { class: "dagcanvas", width: layout.width, height: layout.height, viewBox: `0 0 ${layout.width} ${layout.height}`, preserveAspectRatio: "xMidYMid meet" },
      arrowMarkers(),
      ...edgeEls,
      ...markerEls,
      ...nodeEls,
    );
    mount(this.canvasHost, this.svgRoot);
  }

  // 节点右上角删除按钮（hover 现出，CSS 控制显隐）；pointerdown 自己吞掉，不触发连边拖拽。
  deleteAffordance(node, pos) {
    const cx = pos.x + NODE_W / 2;
    const cy = pos.y - NODE_H / 2;
    const g = svg(
      "g",
      { class: "ndel" },
      svg("circle", { class: "ndel-c", cx, cy, r: 8 }),
      svg("text", { class: "ndel-x", x: cx, y: cy + 3.2, "text-anchor": "middle" }, "✕"),
      svg("title", null, i18n.delNode),
    );
    g.addEventListener("pointerdown", (e) => e.stopPropagation());
    g.addEventListener("click", (e) => {
      e.stopPropagation();
      this.deleteNode(node.id);
    });
    return g;
  }

  // 节点 / START 上的 pointer 交互：未越阈值 = 点击选中；越阈值 = 拖拽连边（跟手临时线 + 落点高亮）。
  onNodePointerDown(e, sourceId) {
    if (e.button !== 0) return;
    if (e.target.closest(".ndel")) return; // 删除按钮各管各的
    e.preventDefault();
    const srcPos = this.positions.get(sourceId);
    if (!srcPos) return;
    const svgRoot = this.svgRoot;
    const sy0 = srcPos.y + (srcPos.marker ? ANCHOR_H / 2 : NODE_H / 2);
    const start = { x: e.clientX, y: e.clientY };
    let activated = false;
    let tempPath = null;

    const onMove = (ev) => {
      if (!activated) {
        if (Math.abs(ev.clientX - start.x) < 5 && Math.abs(ev.clientY - start.y) < 5) return;
        activated = true;
        document.body.style.userSelect = "none";
        tempPath = svg("path", { class: "edge-drag", "marker-end": `url(#${ARROW_ID})` });
        svgRoot.appendChild(tempPath);
      }
      const pt = clientToLocal(svgRoot, ev.clientX, ev.clientY);
      tempPath.setAttribute("d", curveD(srcPos.x, sy0, pt.x, pt.y));
      this.highlightConnectTarget(nodeAt(this.positions, pt.x, pt.y), sourceId);
    };
    const onUp = (ev) => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.userSelect = "";
      if (tempPath && tempPath.parentNode) tempPath.parentNode.removeChild(tempPath);
      this.clearConnectHighlight();
      if (!activated) {
        this.selectNode(sourceId);
        return;
      }
      const pt = clientToLocal(svgRoot, ev.clientX, ev.clientY);
      const targetId = nodeAt(this.positions, pt.x, pt.y);
      if (targetId) this.tryConnect(sourceId, targetId);
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  }

  findNodeG(id) {
    for (const g of this.svgRoot.querySelectorAll("[data-node-id]")) {
      if (g.dataset.nodeId === id) return g;
    }
    return null;
  }

  highlightConnectTarget(targetId, sourceId) {
    this.clearConnectHighlight();
    if (!targetId || targetId === sourceId) return;
    const g = this.findNodeG(targetId);
    if (g) g.classList.add(this.canConnect(sourceId, targetId) ? "connect-ok" : "connect-bad");
  }
  clearConnectHighlight() {
    this.svgRoot.querySelectorAll(".connect-ok, .connect-bad").forEach((g) => g.classList.remove("connect-ok", "connect-bad"));
  }

  // canConnect：镜像内核边合法性 + 环检测，判断 from→to 这条边是否可加（供拖拽高亮 / 落点裁决）。
  canConnect(from, to) {
    if (!to || from === to) return false;
    if (to === NODE_ID_START) return false; // START 无入边
    if (from === NODE_ID_END) return false; // END 无出边
    if (from === NODE_ID_START && to === NODE_ID_END) return false; // 须过 ≥1 个 agent
    if ((this.def.edges || []).some((e) => e.from === from && e.to === to)) return false; // 重复边
    if (wouldCreateCycle(this.def, { from, to })) return false; // 成环
    return true;
  }

  tryConnect(from, to) {
    if (from === to) return;
    if (this.canConnect(from, to)) {
      (this.def.edges || (this.def.edges = [])).push({ from, to });
      this.markDirty();
      this.renderFlow();
      this.renderInspector(); // 祖先变化 → 占位符建议随之刷新
      return;
    }
    // 拒绝：成环当场红闪拒绝、不落；其它（重复 / 结构非法）toast 说明。
    if (wouldCreateCycle(this.def, { from, to })) {
      this.flashRejectEdge(from, to);
      toast(i18n.cycleRejected);
    } else {
      toast(i18n.connectRejected);
    }
  }

  flashRejectEdge(from, to) {
    const a = this.positions.get(from);
    const b = this.positions.get(to);
    if (!a || !b) return;
    const p = svg("path", { class: "edge-bad", d: edgePath(a, b), "marker-end": `url(#${ARROW_BAD_ID})` });
    this.svgRoot.appendChild(p);
    setTimeout(() => {
      if (p.parentNode) p.parentNode.removeChild(p);
    }, 600);
  }

  removeEdge(edge) {
    this.def.edges = (this.def.edges || []).filter((e) => !(e.from === edge.from && e.to === edge.to));
    this.markDirty();
    this.renderFlow();
    this.renderInspector();
  }

  // 整理连线：DAG 传递归约，一次删掉所有可被绕行路径替代的冗余边（如 START→node2 因 START→node1→node2
  // 而多余）。保持可达性不变、不破坏单源单汇，因此不涉及校验拦截；无冗余边时仅提示、不改图。
  optimizeEdges() {
    const redundant = redundantEdges(this.def);
    if (!redundant.length) {
      toast(i18n.edgeOptNone);
      return;
    }
    const removeSet = new Set(redundant.map(edgeKey));
    this.def.edges = (this.def.edges || []).filter((e) => !removeSet.has(edgeKey(e)));
    this.markDirty();
    this.renderAll();
    toast(i18n.edgeOptDoneTpl(redundant.length));
  }

  selectNode(id) {
    if (isMarker(id)) return; // START / END 不承载配置、不可选
    this.selId = id;
    this.renderFlow();
    this.renderInspector();
  }

  // ---- 结构操作 ----
  // addNode / optimizeEdges 的按钮都在画布工具条上，仅表单视图可点，故无需 JSON 视图回同步分支。
  addNode() {
    const id = this.uniqueId("node");
    const node = { id, displayName: id, engine: "codex", promptTemplate: DEFAULT_NODE_PROMPT };
    const endIdx = this.def.nodes.findIndex((n) => n.id === NODE_ID_END);
    if (endIdx >= 0) this.def.nodes.splice(endIdx, 0, node);
    else this.def.nodes.push(node);
    // 缺省接 START → 新节点 → END（= node add 不给 --from/--to）。
    this.addEdgeIfAbsent(NODE_ID_START, id);
    this.addEdgeIfAbsent(id, NODE_ID_END);
    this.selId = id;
    this.markDirty();
    this.renderAll();
  }

  addEdgeIfAbsent(from, to) {
    if (!(this.def.edges || []).some((e) => e.from === from && e.to === to)) {
      (this.def.edges || (this.def.edges = [])).push({ from, to });
    }
  }

  uniqueId(base) {
    const existing = new Set(this.def.nodes.map((n) => n.id));
    let i = this.agentCount() + 1;
    let id = `${base}-${i}`;
    while (existing.has(id)) id = `${base}-${++i}`;
    return id;
  }

  deleteNode(id) {
    if (this.agentCount() <= 1) {
      toast(i18n.lastAgentKeep); // 至少保留一个 agent 节点，不删空
      return;
    }
    // 级联删边（= node rm）：结果是否悬空 / 断桥交本地校验红点即时呈现、服务端保存终裁。
    this.def.nodes = this.def.nodes.filter((n) => n.id !== id);
    this.def.edges = (this.def.edges || []).filter((e) => e.from !== id && e.to !== id);
    if (this.selId === id) this.selId = this.firstAgentId();
    this.markDirty();
    this.renderAll();
  }

  // ---- 检查器 ----
  renderInspector() {
    const node = this.selectedNode();
    if (!node) {
      mount(this.inspCol, h("div", { class: "muted insp-empty" }, i18n.noEditableNodes));
      return;
    }
    mount(
      this.inspCol,
      this.inspHead(node),
      this.fieldId(node),
      this.fieldText(i18n.fDisplayName, node.displayName || "", (v) => (node.displayName = v), { live: (v) => this.liveName(v), field: "displayName" }),
      this.fieldEngine(node, (eng) => {
        node.engine = eng;
        this.pruneEngineConfig(node);
        this.renderInspector();
        this.renderFlow();
      }),
      ...this.engineConfigFields(node.engine, node),
      this.promptField(node),
    );
    this.decorateFieldErrors();
  }

  // 检查器头：id chip（随改名实时刷新）+ displayName 标题。
  inspHead(node) {
    return h(
      "div",
      { class: "insphead" },
      h("span", { class: "idchip" }, node.id),
      h("span", { class: "itt" }, node.displayName || node.id),
      h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.idNoteTpl(node.id || "id"))),
    );
  }

  // id 字段：可编辑，改名走级联（renameNode 同步所有边 from/to 与模板 {{id}} 引用）。input 事件仅即时
  // 正则提示不落地；change（失焦 / 回车）才提交——一次性级联并整页重渲，避免逐键改名的抖动与失焦。
  fieldId(node) {
    const hint = h("div", { class: "ferr", style: { display: "none" } });
    const input = h("input", { class: "inp inp-mono" });
    input.value = node.id;
    input.addEventListener("input", () => {
      const v = input.value.trim();
      const ok = v === "" || NODE_ID_RE.test(v);
      hint.style.display = ok ? "none" : "block";
      if (!ok) hint.textContent = i18n.idRuleHint;
    });
    input.addEventListener("change", () => this.commitRename(node, input));
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") input.blur();
    });
    return h("div", { class: "fgroup", dataset: { field: "id" } }, h("label", { class: "flabel" }, i18n.fNodeId), input, hint);
  }

  // commitRename 校验并提交改名：非法（空 / 不合正则）、保留名、重名一律拒绝并原样回滚（重渲还原输入框）；
  // 合法则级联改名、选中跟到新 id、整页重渲。
  commitRename(node, input) {
    const oldId = node.id;
    const newId = input.value.trim();
    if (newId === oldId) return;
    const reject = (msg) => {
      toast(msg);
      this.renderInspector(); // 回滚：按未变的 node.id 重建输入框，清掉残留提示
    };
    if (!NODE_ID_RE.test(newId)) return reject(i18n.idRuleHint);
    if (isMarker(newId)) return reject(i18n.idReserved);
    if ((this.def.nodes || []).some((n) => n.id === newId)) return reject(i18n.idDuplicateTpl(newId));
    this.renameNode(oldId, newId);
    this.selId = newId;
    this.markDirty();
    // 局部重渲（画布 + 检查器），不走 renderAll——否则会重建顶栏保存按钮，若此次提交由「点保存按钮」
    // 失焦触发，重建会吞掉紧随的保存点击（按钮已被替换、click 落空），导致改了却没存出去。
    this.renderFlow();
    this.renderInspector();
  }

  // renameNode 级联改名：节点自身 id + 所有边 from/to + 所有模板里的 {{oldId}} 引用一起换成 newId，
  // 不留悬空引用。这正是内核把「改 id」归为全量 edit（因 id 有引用完整性）的原因，UI 在此自动兜住。
  renameNode(oldId, newId) {
    for (const n of this.def.nodes || []) {
      if (n.id === oldId) n.id = newId;
      if (n.promptTemplate) n.promptTemplate = renameTemplateRef(n.promptTemplate, oldId, newId);
    }
    for (const e of this.def.edges || []) {
      if (e.from === oldId) e.from = newId;
      if (e.to === oldId) e.to = newId;
    }
  }

  liveName(v) {
    // 名称改动即时反映到检查器标题与画布标签（改 id 才是结构变更，displayName 仅重绘）。
    const title = this.inspCol.querySelector(".insphead .itt");
    if (title) title.textContent = v || this.selId;
    this.renderFlow();
  }

  // 保存校验失败后，把每条字段级错误标到检查器对应字段：整组套红框 + 组内补一行内核原文消息。
  // 字段定位靠各 fgroup 的 data-field（= 去掉 nodes[i]. 前缀的点路径，如 engineConfig.model），
  // 与 validate.go 的 Problem.Path 同源。节点级错误（path 恰为 nodes[i]）无对应单一字段，仅在错误面板呈现。
  decorateFieldErrors() {
    if (!this.errors.length || !this.inspCol) return;
    const idx = this.selectedIndex();
    if (idx < 0) return;
    const prefix = `nodes[${idx}].`;
    for (const e of this.errors) {
      if (!e.path.startsWith(prefix)) continue;
      const fg = this.inspCol.querySelector(`[data-field="${e.path.slice(prefix.length)}"]`);
      if (!fg) continue;
      fg.classList.add("field-err");
      fg.appendChild(h("div", { class: "ferr" }, e.message));
    }
  }

  // 通用文本字段（label 上、控件下）。opts.field 给出该字段的 data-field 键（供保存错误红框定位）。
  fieldText(label, value, setter, opts = {}) {
    return h(
      "div",
      { class: "fgroup", dataset: opts.field ? { field: opts.field } : null },
      h("label", { class: "flabel" }, label),
      this.textInput(value, setter, opts),
    );
  }

  textInput(value, setter, opts = {}) {
    const input = h("input", { class: "inp" + (opts.mono ? " inp-mono" : "") });
    input.value = value;
    input.addEventListener("input", () => {
      setter(input.value);
      this.markDirty();
      if (opts.live) opts.live(input.value);
    });
    return input;
  }

  fieldEngine(node, onChange) {
    return h(
      "div",
      { class: "fgroup", dataset: { field: "engine" } },
      h("label", { class: "flabel" }, i18n.fEngine),
      engineSelect(node.engine, onChange),
    );
  }

  // 切换引擎后清掉新引擎不接受的 engineConfig 字段：否则残值既不渲染（无处修改）又会在保存时 422，
  // 且该错误路径在检查器里无对应控件、红框都挂不上。cap 未登记（能力表待实装）时任何字段都不接受。
  pruneEngineConfig(holder) {
    const cfg = holder.engineConfig;
    if (!cfg) return;
    const cap = capabilityOf(holder.engine);
    if (!cap) {
      delete cfg.model;
      delete cfg.effort;
      delete cfg.reasoningEffort;
      return;
    }
    if (!cap.allowsModel) delete cfg.model;
    if (cap.effortField !== "effort") delete cfg.effort;
    if (cap.effortField !== "reasoningEffort") delete cfg.reasoningEffort;
  }

  // engineConfig 按引擎能力表条件渲染：该引擎没有的字段就不渲染（不配解释文案）。
  engineConfigFields(engine, holder) {
    const cap = capabilityOf(engine);
    const fields = [];
    if (!cap || cap.allowsModel) fields.push(this.modelField(cap, holder));
    if (cap && cap.effortField) fields.push(this.effortField(cap, holder));
    return fields;
  }

  // cap.modelValues 非空时挂一个自定义建议下拉（与 engine 选择器同一套 .engsel 视觉），但控件本身
  // 仍是真实 <input>——保留自由打字 / 光标 / 输入法，建议值只是点击可填的便利提示（非白名单）。
  modelField(cap, holder) {
    const cfg = () => holder.engineConfig || (holder.engineConfig = {});
    const input = h("input", { class: "inp inp-mono", placeholder: i18n.modelPlaceholder });
    input.value = (holder.engineConfig && holder.engineConfig.model) || "";
    let renderMenu = null;
    input.addEventListener("input", () => {
      cfg().model = input.value;
      this.markDirty();
      if (renderMenu) renderMenu();
    });

    const modelValues = (cap && cap.modelValues) || [];
    let control = input;
    if (modelValues.length) {
      const menu = h("div", { class: "engsel-menu" });
      const wrap = h("div", { class: "engsel" }, input, menu);
      let stopOutsideClick = null;
      const close = () => {
        wrap.classList.remove("open");
        if (stopOutsideClick) {
          stopOutsideClick();
          stopOutsideClick = null;
        }
      };
      const open = () => {
        wrap.classList.add("open");
        stopOutsideClick = closeOnOutsideClick(wrap, close);
      };
      const pick = (value) => {
        input.value = value;
        cfg().model = value;
        this.markDirty();
        close();
      };
      renderMenu = () => {
        mount(
          menu,
          ...modelValues.map((v) =>
            h(
              "div",
              {
                class: "engsel-item" + (v === input.value ? " engsel-item--on" : ""),
                onClick: (e) => {
                  e.stopPropagation();
                  pick(v);
                },
              },
              h("span", {}, v),
            ),
          ),
        );
      };
      input.addEventListener("focus", () => {
        renderMenu();
        open();
      });
      input.addEventListener("keydown", (e) => {
        if (e.key === "Escape") close();
      });
      renderMenu();
      control = wrap;
    }

    return h("div", { class: "fgroup", dataset: { field: "engineConfig.model" } }, h("label", { class: "flabel" }, i18n.fModel), control);
  }

  // 自定义下拉（listSelect），与 engine 选择器同一套视觉/交互——原生 <select> 在 macOS 上会用
  // 系统级弹出样式（以当前选中项为中心展开），观感与 engine 选择器不统一。
  effortField(cap, holder) {
    const field = cap.effortField;
    const cfg = () => holder.engineConfig || (holder.engineConfig = {});
    const current = (holder.engineConfig && holder.engineConfig[field]) || "";
    const items = [{ value: "", label: i18n.fEffortNotSet }, ...(cap.effortValues || []).map((v) => ({ value: v, label: v }))];
    const select = listSelect(current, items, (value) => {
      cfg()[field] = value;
      this.markDirty();
    });
    return h("div", { class: "fgroup", dataset: { field: "engineConfig." + field } }, h("label", { class: "flabel" }, field), select);
  }

  // ---- 提示词编辑器字段 ----
  promptField(node) {
    const editor = createPromptEditor({
      value: node.promptTemplate || "",
      fieldName: "promptTemplate",
      placeholders: this.placeholders(node),
      onChange: (v) => {
        node.promptTemplate = v;
        this.markDirty();
        this.renderFlow(); // 模板 {{引用}} 变化影响校验：刷新画布红点与「可运行」徽标（只重建画布，不动检查器里的提示词编辑器）
      },
    });
    return h("div", { dataset: { field: "promptTemplate" } }, editor.element);
  }

  // 占位符建议 = sys 变量 + 当前定义内**祖先** agent 节点 id（沿边可达的前驱，用 graph.js ancestors）。
  placeholders(node) {
    const anc = ancestors(this.def, node.id);
    const ancestorIds = this.def.nodes.filter((n) => isAgent(n.id) && anc.has(n.id)).map((n) => `{{${n.id}}}`);
    return ["{{sys.userPrompt}}", "{{sys.cwd}}", "{{sys.runId}}", ...ancestorIds];
  }

  // ---- JSON 视图 ----
  jsonBody() {
    const json = JSON.stringify(this.def, null, 2);
    this.jsonEditor = createCodeEditor({
      value: json,
      lang: "json",
      minHeight: "420px",
      maxHeight: "70vh",
      onChange: () => this.markDirty(),
    });
    this.jsonErr = h("div", { class: "ferr", style: { display: "none", padding: "0 18px 12px" } });
    return h(
      "div",
      { class: "jsonwrap" },
      h(
        "div",
        { class: "jsonbar" },
        i18n.jsonBarTitle,
        h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.jsonMetaNote)),
      ),
      h("div", { class: "jsonbody" }, this.jsonEditor.element),
      this.jsonErr,
    );
  }

  // parseJsonEditor 解析 JSON 编辑区；语法错误就地标错并返回 null。
  parseJsonEditor() {
    try {
      const parsed = JSON.parse(this.jsonEditor.getValue());
      if (this.jsonErr) this.jsonErr.style.display = "none";
      return parsed;
    } catch (e) {
      if (this.jsonErr) {
        this.jsonErr.textContent = i18n.jsonSyntaxErr + e.message;
        this.jsonErr.style.display = "block";
      }
      return null;
    }
  }

  // ---- 保存 ----
  async save() {
    let body;
    if (this.view === "json") {
      const parsed = this.parseJsonEditor();
      if (!parsed) return;
      body = parsed.definition ? parsed.definition : parsed; // 容忍整条记录：解包 definition 主体
    } else {
      body = this.def;
    }
    this.pruneEmptyConfigs(body);
    // 提前采纳（JSON 视图下即为 parse 结果）：任何保存失败路径重渲都基于用户当前编辑，绝不回滚。
    this.def = body;
    try {
      const saved = await api.putWorkflow(this.name, body, this.baseUpdatedAt);
      this.onSaved(saved);
    } catch (err) {
      if (err instanceof ApiError && err.problems.length) {
        this.errors = err.problems;
        if (this.view === "json") this.view = "form"; // 切回表单便于逐字段定位
        this.renderAll();
        return;
      }
      if (err instanceof ApiError && err.status === 409 && err.current) {
        this.openConflict(body, err.current);
        return;
      }
      this.showSaveError(err.message);
    }
  }

  onSaved(saved) {
    this.record = saved;
    this.def = saved.definition;
    this.baseUpdatedAt = saved.updatedAt;
    this.dirty = false;
    this.errors = [];
    if (!this.selectedNode()) this.selId = this.firstAgentId();
    this.renderAll();
  }

  showSaveError(message) {
    this.errors = [{ path: "", message }];
    this.renderAll();
  }

  // 乐观并发冲突：弹「覆盖保存 / 放弃重载」。current 是服务端当前完整记录。
  openConflict(myBody, current) {
    const overwrite = h("button", { class: "btn btn-ink" }, i18n.overwrite);
    overwrite.addEventListener("click", async () => {
      ctl.close();
      try {
        const saved = await api.putWorkflow(this.name, myBody, ""); // 不带基线 = 强制覆盖
        this.onSaved(saved);
      } catch (err) {
        this.showSaveError(err.message);
      }
    });
    const reload = h("button", { class: "btn" }, i18n.reload);
    reload.addEventListener("click", () => {
      ctl.close();
      renderEditorPage(this.outlet, this.name);
    });
    const ctl = openModal({
      title: i18n.saveConflictTitle,
      body: h("p", { style: { margin: "0" } }, i18n.saveConflictBody),
      footer: [reload, overwrite],
    });
  }

  // pruneEmptyConfigs 清掉空的 engineConfig（{} / 全空字段），避免落一堆无意义空对象。
  pruneEmptyConfigs(def) {
    for (const node of def.nodes || []) {
      const c = node.engineConfig;
      // 所有字段皆空才删（与字段清单解耦：将来 engineConfig 加字段也不会把只填了新字段的配置误删）。
      if (c && Object.values(c).every((v) => !v)) delete node.engineConfig;
    }
  }

  // ---- 顶栏改名 / 运行：与列表页共用弹窗（dialogs.js），仅成功去向不同 ----
  openRename() {
    // 改名成功后跳到新名的编辑器（列表页则是留在原页重载）。
    openRenameDialog(this.name, (newName) => navigate(`/workflows/${encodeURIComponent(newName)}`));
  }

  openLaunch() {
    openLaunchDialog(this.name);
  }
}

// 新建节点时填充的默认提示词：透传用户需求 + 一句任务说明，用户按需在检查器改。
const DEFAULT_NODE_PROMPT =
  "## User request\n\n{{sys.userPrompt}}\n\n## Task\n\nComplete the task according to the user's request above.";
