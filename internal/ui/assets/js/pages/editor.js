// 工作流编辑器页（#/workflows/:name）：show（载入）+ edit（整体替换保存）的聚合工作台。
// 两栏：左侧流程概览（紧凑，忠实于线性 nodes 数组），右侧检查器（选中节点的完整配置）。
// 另有 JSON 视图（源码编辑，与表单双向同步）。保存走 PUT 整体替换，校验不过 422 逐条锚定字段。

import { h, mount, copyText } from "../dom.js";
import { api, ApiError } from "../api.js";
import { navigate } from "../router.js";
import { i18n } from "../i18n.js";
import { fmtTime } from "../format.js";
import { loadEngines, engineNames, capabilityOf, engineIconEl } from "../engines.js";
import { createPromptEditor } from "../prompt-editor.js";
import { createCodeEditor } from "../code-editor.js";
import { engineSelect, listSelect, closeOnOutsideClick } from "../custom-select.js";
import { openModal } from "../modal.js";
import { openLaunchDialog, openRenameDialog } from "../dialogs.js";
import { loadingView, errorView } from "./common.js";

export async function renderEditorPage(outlet, name) {
  mount(outlet, loadingView());
  let def, engines;
  try {
    // 引擎能力表与定义并行拉；定义即使语义非法也要能载入去修（编辑器不做载入期语义校验）。
    [engines, def] = await Promise.all([loadEngines(), api.getWorkflow(name)]);
  } catch (err) {
    mount(outlet, errorView(err));
    return;
  }
  new Editor(outlet, name, def, engines).mount();
}

class Editor {
  constructor(outlet, name, def, engines) {
    this.outlet = outlet;
    this.name = name;
    this.def = def;
    this.baseUpdatedAt = def.updatedAt; // 乐观并发基线
    this.sel = def.nodes.length ? 0 : -1; // 聚焦哪个节点（index）
    this.focus = "node"; // 检查器聚焦目标：node | evaluator（自循环）| loop（回跳线）
    this.view = "form";
    this.dirty = false;
    this.errors = []; // 上次保存的字段级校验错误 [{path, message}]
  }

  mount() {
    this.root = h("div", { class: "page page-wide" });
    mount(this.outlet, this.root);
    this.renderAll();
  }

  markDirty() {
    this.dirty = true;
    if (this.saveDot) {
      this.saveDot.className = "savedot unsaved";
      this.saveDot.textContent = i18n.edUnsaved;
    }
  }

  // 节点索引 → 该节点当前是否带校验错误（供左栏卡片红点、错误面板锚定）。
  nodeErrorIndices() {
    const set = new Set();
    for (const e of this.errors) {
      const m = /^nodes\[(\d+)\]/.exec(e.path);
      if (m) set.add(Number(m[1]));
    }
    return set;
  }

  // 由错误路径推断该字段所在的检查器面板：evaluator.* → 评测循环面板；redoTarget → 回跳面板；
  // loopCount 归属看节点当前是哪种循环；其余（含节点级 nodes[i]）→ 节点面板。
  focusForErrorPath(path, node) {
    if (path.includes(".evaluator.") || path.endsWith(".evaluator")) return "evaluator";
    if (path.includes(".redoTarget")) return "loop";
    if (path.includes(".loopCount")) return node && node.evaluator ? "evaluator" : "loop";
    return "node";
  }

  renderAll() {
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
    return h(
      "div",
      { class: "edithead" },
      h("span", { class: "wfname" }, this.name),
      h("span", { class: "ghost", style: { padding: "2px 7px", fontSize: "12px" }, onClick: () => this.openRename() }, i18n.rename),
      h("span", { class: "meta" }, `${i18n.edNodesTpl(this.def.nodes.length)} · ${i18n.edUpdatedTpl(fmtTime(this.def.updatedAt))}`),
      this.saveDot,
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
      this.def = parsed;
      // JSON 里删了节点后 sel 可能越界，收敛到合法范围（否则检查器误显示「没有可编辑节点」空态）。
      if (this.sel >= this.def.nodes.length) this.sel = this.def.nodes.length - 1;
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
              if (m) {
                const idx = Number(m[1]);
                this.sel = idx;
                // 切到该错误字段所在面板，否则 evaluator./redoTarget/loopCount 的 data-field
                // 只存在于聚焦面板 DOM 里，停在节点面板会标不出红框（见 decorateFieldErrors）。
                this.focus = this.focusForErrorPath(e.path, this.def.nodes[idx]);
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

  // ---- 表单视图：两栏 ----
  formBody() {
    this.flowCol = h("div", { class: "flowcol" });
    this.inspCol = h("div", { class: "insp" });
    this.renderFlow();
    this.renderInspector();
    return h("div", { class: "body2" }, this.flowCol, this.inspCol);
  }

  renderFlow() {
    const rail = h("div", { class: "railwrap" });
    const errSet = this.nodeErrorIndices();
    rail.appendChild(h("div", { class: "anchor" }, i18n.anchorStart));
    this.def.nodes.forEach((node, i) => {
      rail.appendChild(h("div", { class: "conn" }));
      rail.appendChild(this.nodeCard(node, i, errSet.has(i)));
    });
    rail.appendChild(h("div", { class: "conn" }));
    rail.appendChild(h("div", { class: "addnode", onClick: () => this.addNode() }, i18n.addNode));
    rail.appendChild(h("div", { class: "conn" }));
    rail.appendChild(h("div", { class: "anchor" }, i18n.anchorEnd));
    mount(this.flowCol, rail);
    // 回跳弧线需 DOM 已布局才能测量定位；rAF 确保在 mount + paint 之后再叠加。
    requestAnimationFrame(() => this.layoutRedoArcs());
  }

  // 在 railwrap 左侧为每个带 redoTarget 的节点叠加一条紫色弧线（source 中心 → target 上沿，
  // 顶端箭头指向 target）。多条并存时递增 left 错开（参考 x-one-web 的 arc peak 偏移）。
  layoutRedoArcs() {
    const rail = this.flowCol && this.flowCol.querySelector(".railwrap");
    if (!rail) return;
    rail.querySelectorAll(".redoarc").forEach((el) => el.remove()); // 清旧弧再重画
    const cards = [...rail.querySelectorAll(".fnode")];
    if (!cards.length) return;
    const railTop = rail.getBoundingClientRect().top;
    const ids = this.def.nodes.map((n) => n.id);
    let lane = 0;
    this.def.nodes.forEach((node, i) => {
      if (!node.redoTarget) return;
      const srcRect = cards[i].getBoundingClientRect();
      const srcCenter = srcRect.top + srcRect.height / 2 - railTop;
      const targetIdx = ids.indexOf(node.redoTarget);
      // 失效回跳：目标不存在，或前向/自指（redoTarget 须严格在本节点之前，见 workflow.Validate）。
      // 例如把带 redoTarget 的节点拖到目标之前——回跳变前向。一律标红断桩，与保存时的 422 同调，
      // 不画方向错误的"正常"弧误导用户。
      const invalid = targetIdx < 0 || targetIdx >= i;
      let topY, height;
      if (invalid) {
        topY = srcRect.top - railTop - 14;
        height = srcCenter - topY;
      } else {
        const tgtRect = cards[targetIdx].getBoundingClientRect();
        const tgtCenter = tgtRect.top + tgtRect.height / 2 - railTop;
        topY = Math.min(tgtCenter, srcCenter);
        height = Math.abs(srcCenter - tgtCenter);
      }
      const arc = this.buildRedoArc(i, node, invalid, lane);
      arc.style.top = topY + "px";
      arc.style.height = height + "px";
      rail.appendChild(arc);
      lane += 1;
    });
  }

  buildRedoArc(i, node, invalid, lane) {
    const loopCount = node.loopCount || 1;
    const selected = i === this.sel && this.focus === "loop";
    const cls = "redoarc" + (invalid ? " redoarc-err" : "") + (selected ? " redoarc-sel" : "");
    const arc = h(
      "div",
      { class: cls, style: { left: 8 + lane * 7 + "px" }, title: i18n.loopLineTip },
      h("span", { class: "archead" }),
      h("span", { class: "arclabel" }, i18n.redoArcLabelTpl(loopCount)),
    );
    arc.addEventListener("click", (e) => {
      e.stopPropagation();
      this.focusLoop(i);
    });
    // 端点拖拽手柄：弧顶 = target 端（改回跳目标）、弧底 = source 端（改哪个节点回跳）。参考
    // x-one-web WorkflowDiagram 的 arc endpoint drag。
    const hTop = h("span", { class: "archandle archandle-top", title: i18n.arcDragTip });
    const hBot = h("span", { class: "archandle archandle-bot", title: i18n.arcDragTip });
    hTop.addEventListener("pointerdown", (e) => this.startArcDrag(e, i, "end"));
    hBot.addEventListener("pointerdown", (e) => this.startArcDrag(e, i, "start"));
    arc.appendChild(hTop);
    arc.appendChild(hBot);
    return arc;
  }

  // 拖动回跳弧端点改连接。end 端：落点须在 source 之前 → 改 source 的 redoTarget；
  // start 端：落点须在 target 之后 → 把回跳整体移到新 source 节点（target/loopCount 保留）。
  startArcDrag(e, sourceIdx, endpoint) {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation(); // 不冒泡到弧的 click(focusLoop)
    const rail = this.flowCol && this.flowCol.querySelector(".railwrap");
    if (!rail) return;
    const cards = [...rail.querySelectorAll(".fnode")];
    const railTop = rail.getBoundingClientRect().top;
    const centers = cards.map((c) => {
      const r = c.getBoundingClientRect();
      return r.top + r.height / 2 - railTop;
    });
    const targetIdx = this.def.nodes.findIndex((n) => n.id === this.def.nodes[sourceIdx].redoTarget);
    if (endpoint === "start" && targetIdx < 0) return; // 无合法 target 时 source 端不可拖
    const arc = e.target.closest(".redoarc");
    // 固定端锚在其节点中心：拖 target 端(end)时固定 source，拖 source 端(start)时固定 target。
    const fixedY = endpoint === "end" ? centers[sourceIdx] : centers[targetIdx];
    const railBottom = rail.getBoundingClientRect().height;
    const st = { sourceIdx, targetIdx, endpoint, activated: false, hover: -1, startY: e.clientY };
    const onMove = (ev) => {
      if (!st.activated) {
        if (Math.abs(ev.clientY - st.startY) < 4) return; // 阈值内不算拖拽（留给点击）
        st.activated = true;
        document.body.style.userSelect = "none";
        if (arc) arc.classList.add("redoarc-dragging");
      }
      // 被拖端实时跟随光标 Y（clamp 到画布内），弧线随手重画——对齐 x-one-web 的 drag.y 机制。
      const y = Math.max(0, Math.min(railBottom, ev.clientY - railTop));
      if (arc) {
        arc.style.top = Math.min(fixedY, y) + "px";
        arc.style.height = Math.abs(fixedY - y) + "px";
      }
      // 落点：离光标最近的合法节点（end：source 之前；start：target 之后），高亮它。
      let best = -1;
      let bestD = Infinity;
      for (let k = 0; k < centers.length; k++) {
        if (k === sourceIdx) continue;
        // end 端只改回跳目标（须为 source 之前的节点）；start 端把回跳移到新 source，新 source 必须在
        // target 之后，且不能带 evaluator（与 redoTarget 互斥）或已有回跳（否则静默覆盖），与 CTA 入口同守约束。
        const n = this.def.nodes[k];
        const ok = endpoint === "end" ? k < sourceIdx : k > targetIdx && !n.evaluator && !n.redoTarget;
        if (!ok) continue;
        const d = Math.abs(centers[k] - y);
        if (d < bestD) {
          bestD = d;
          best = k;
        }
      }
      st.hover = best;
      this.highlightArcDrop(cards, best);
    };
    const onUp = () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.userSelect = "";
      this.highlightArcDrop(cards, -1);
      if (!st.activated) {
        this.focusLoop(sourceIdx); // 未越阈值 = 点击 = 聚焦回跳线面板
        return;
      }
      if (st.hover >= 0) this.applyArcDrag(st);
      else this.renderFlow(); // 落到非法处：弧线复位到原连接
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  }

  highlightArcDrop(cards, idx) {
    cards.forEach((c, k) => c.classList.toggle("arc-drop", k === idx));
  }

  applyArcDrag(st) {
    const nodes = this.def.nodes;
    if (st.endpoint === "end") {
      nodes[st.sourceIdx].redoTarget = nodes[st.hover].id; // 改回跳目标
    } else {
      // 把回跳整体移到另一个 source 节点：清旧 source、在新 source 上重建（target/loopCount 保留）。
      const oldSrc = nodes[st.sourceIdx];
      const targetId = nodes[st.targetIdx].id;
      const loopCount = oldSrc.loopCount || 1;
      oldSrc.redoTarget = "";
      delete oldSrc.loopCount;
      const newSrc = nodes[st.hover];
      newSrc.redoTarget = targetId;
      newSrc.loopCount = loopCount;
      if (this.focus === "loop") this.sel = st.hover; // 聚焦跟随到新 source
    }
    this.markDirty();
    this.renderFlow();
    this.renderInspector();
  }

  focusEvaluator(i) {
    this.sel = i;
    this.focus = "evaluator";
    this.renderFlow();
    this.renderInspector();
  }

  focusLoop(i) {
    this.sel = i;
    this.focus = "loop";
    this.renderFlow();
    this.renderInspector();
  }

  nodeCard(node, i, hasErr) {
    const selected = i === this.sel;
    const badges = [];
    const loopCount = node.loopCount || 1;
    // 自循环：节点自身的「循环属性」，画成节点上的 ↻ ×N 徽标（可点，聚焦到评测循环面板）。
    if (node.evaluator) {
      const evalSel = selected && this.focus === "evaluator";
      const b = h("span", { class: "loopb" + (evalSel ? " loopb-sel" : ""), title: i18n.evalLoopTip }, `↻ ×${loopCount}`);
      b.addEventListener("click", (e) => {
        e.stopPropagation();
        this.focusEvaluator(i);
      });
      badges.push(b);
    }
    // 回跳（redoTarget）是跨节点跳回，不贴节点徽标——改由 layoutRedoArcs 在画布左侧画紫色弧线。

    // 删除按钮：hover 时右上角红色 icon（取代原 ✕；不再需要选中）。
    const delBtn = h("button", { class: "node-del", title: i18n.delNode }, "✕");
    delBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      this.deleteNode(i);
    });

    const card = h(
      "div",
      { class: "fnode" + (selected ? " fnode-sel" : ""), dataset: { index: String(i) } },
      hasErr ? h("div", { class: "errdot" }) : null,
      h("span", { class: "grip", title: i18n.dragSort }, "⠿"),
      delBtn,
      engineIconEl(node.engine),
      h("span", { class: "nm" }, node.displayName || node.id || i18n.unnamed),
      ...badges,
    );
    // pointer 拖拽（非 HTML5 DnD）：节点跟随光标、按指针 Y 位置算插入槽，不必精确落到目标上。
    // 移动超过阈值才算拖拽，否则视为点击选中。参考 x-one-web WorkflowDiagram 的 node drag-to-reorder。
    card.addEventListener("pointerdown", (e) => this.onNodePointerDown(e, card, i));
    return card;
  }

  onNodePointerDown(e, card, i) {
    if (e.button !== 0) return; // 仅左键
    // 删除按钮 / 自循环徽标有各自的 click 处理，不能触发节点拖拽——否则 pointerdown 的
    // preventDefault 会吃掉它们的 click，徽标点击就退化成 pointerup→selectNode（进错面板）。
    if (e.target.closest(".node-del") || e.target.closest(".loopb")) return;
    e.preventDefault();
    // 拖拽开始时快照所有卡片中心 Y（client 坐标）——落点用它算，稳定不受后续 transform 影响。
    const cards = [...this.flowCol.querySelectorAll(".fnode")];
    const centers = cards.map((c) => {
      const r = c.getBoundingClientRect();
      return r.top + r.height / 2;
    });
    this.drag = { i, card, startY: e.clientY, centers, height: card.getBoundingClientRect().height, activated: false, dropIndex: i };
    const onMove = (ev) => this.onNodePointerMove(ev);
    const onUp = (ev) => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      this.onNodePointerUp(ev);
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  }

  onNodePointerMove(e) {
    const d = this.drag;
    if (!d) return;
    const delta = e.clientY - d.startY;
    if (!d.activated) {
      if (Math.abs(delta) <= 5) return; // 阈值内不算拖拽（留给点击选中）
      d.activated = true;
      d.card.classList.add("dragging-live");
      document.body.style.userSelect = "none";
      this.ensureDropLine();
    }
    d.card.style.transform = `translateY(${delta}px)`;
    // 落点：被拖节点当前中心 <= 某个其它节点中心 → 插到它之前；都不满足 → 末尾。
    const currentCenter = d.centers[d.i] + delta;
    let dropIndex = d.centers.length;
    for (let k = 0; k < d.centers.length; k++) {
      if (k === d.i) continue;
      if (currentCenter <= d.centers[k]) {
        dropIndex = k;
        break;
      }
    }
    d.dropIndex = dropIndex;
    this.moveDropLine(dropIndex);
  }

  onNodePointerUp() {
    const d = this.drag;
    this.drag = null;
    if (!d) return;
    this.removeDropLine();
    document.body.style.userSelect = "";
    if (!d.activated) {
      this.selectNode(d.i); // 未越过阈值 = 点击选中
      return;
    }
    d.card.style.transform = "";
    d.card.classList.remove("dragging-live");
    const from = d.i;
    const to = d.dropIndex;
    // dropIndex===from / from+1 都是原地（插到自己前后），无需移动。
    if (to !== from && to !== from + 1) {
      this.reorderNode(from, to);
    } else {
      this.renderFlow(); // 复位 transform
    }
  }

  // reorderNode 把被拖节点插到「原始索引 dropIndex 之前」，保持选中被移动的那个节点。
  reorderNode(from, dropIndex) {
    const nodes = this.def.nodes;
    const [moved] = nodes.splice(from, 1);
    // 删除 from 后，落点若原在其后需左移一位。
    const target = from < dropIndex ? dropIndex - 1 : dropIndex;
    nodes.splice(target, 0, moved);
    this.sel = target;
    this.markDirty();
    this.renderFlow();
    this.renderInspector();
  }

  selectNode(i) {
    this.sel = i;
    this.focus = "node";
    this.renderFlow();
    this.renderInspector();
  }

  // ---- 拖拽插入指示线 ----
  ensureDropLine() {
    if (!this.dropLine) this.dropLine = h("div", { class: "drop-line" });
    const rail = this.flowCol.querySelector(".railwrap");
    if (rail) rail.appendChild(this.dropLine);
  }

  moveDropLine(dropIndex) {
    const d = this.drag;
    const rail = this.flowCol.querySelector(".railwrap");
    if (!this.dropLine || !rail) return;
    const railTop = rail.getBoundingClientRect().top;
    // 落点在 node[dropIndex] 上沿；末尾则落在最后一个节点下沿。
    const y =
      dropIndex >= d.centers.length
        ? d.centers[d.centers.length - 1] + d.height / 2
        : d.centers[dropIndex] - d.height / 2;
    this.dropLine.style.top = y - railTop + "px";
  }

  removeDropLine() {
    if (this.dropLine && this.dropLine.parentNode) this.dropLine.parentNode.removeChild(this.dropLine);
  }

  // ---- 检查器 ----
  renderInspector() {
    if (this.sel < 0 || this.sel >= this.def.nodes.length) {
      mount(this.inspCol, h("div", { class: "muted" }, i18n.noEditableNodes));
      return;
    }
    const node = this.def.nodes[this.sel];
    // focus 指向的循环若已不存在（如被清除/删除），回落到节点面板。
    if (this.focus === "evaluator" && node.evaluator) {
      mount(this.inspCol, this.evaluatorFocusPanel(node));
      this.decorateFieldErrors();
      return;
    }
    if (this.focus === "loop" && node.redoTarget) {
      mount(this.inspCol, this.loopFocusPanel(node));
      this.decorateFieldErrors();
      return;
    }
    this.focus = "node";
    mount(
      this.inspCol,
      this.fieldID(node),
      this.fieldText(i18n.fDisplayName, node.displayName || "", (v) => (node.displayName = v), { live: (v) => this.liveName(v), field: "displayName" }),
      this.fieldEngine(node, (eng) => {
        node.engine = eng;
        this.pruneEngineConfig(node);
        this.renderInspector();
        this.renderFlow();
      }),
      ...this.engineConfigFields(node.engine, node),
      this.promptField(node),
      this.loopEntry(node),
    );
    this.decorateFieldErrors();
  }

  // 保存校验失败后，把每条字段级错误标到检查器对应字段：整组套红框 + 组内补一行内核原文消息。
  // 字段定位靠各 fgroup 的 data-field（= 去掉 nodes[i]. 前缀的点路径，如 id / engineConfig.model /
  // evaluator.promptTemplate），与 validate.go 的 Problem.Path 同源。节点级错误（path 恰为 nodes[i]，
  // 如 evaluator/redoTarget 互斥）无对应单一字段，仍只在顶部错误面板呈现。每次 renderInspector 重挂
  // 检查器，红框 / 消息随之刷新，不累积。
  decorateFieldErrors() {
    if (!this.errors.length || !this.inspCol) return;
    const prefix = `nodes[${this.sel}].`;
    for (const e of this.errors) {
      if (!e.path.startsWith(prefix)) continue;
      const fg = this.inspCol.querySelector(`[data-field="${e.path.slice(prefix.length)}"]`);
      if (!fg) continue;
      fg.classList.add("field-err");
      fg.appendChild(h("div", { class: "ferr" }, e.message));
    }
  }

  // 聚焦面板头部：返回节点链接 + 面板标题 + 副标（node id 或 id→target）。复用 .insphead 布局。
  focusHead(title, subtitle) {
    return h(
      "div",
      { class: "insphead focushead" },
      h("span", { class: "backlink", onClick: () => this.selectNode(this.sel) }, "‹ " + i18n.backToNode),
      h("span", { class: "grow" }),
      h("span", { class: "focustag" }, title),
      h("span", { class: "idchip" }, subtitle),
    );
  }

  // 评测循环聚焦面板：evaluator 引擎/model/effort/prompt + loopCount + 清除。
  evaluatorFocusPanel(node) {
    const ev = node.evaluator;
    const cap = capabilityOf(ev.engine);
    const engSel = engineSelect(ev.engine, (name) => {
      ev.engine = name;
      this.pruneEngineConfig(ev);
      this.markDirty();
      this.renderInspector();
    });
    const fields = [h("div", { class: "fgroup", dataset: { field: "evaluator.engine" } }, h("label", { class: "flabel" }, i18n.fEngine), engSel)];
    if (!cap || cap.allowsModel) fields.push(this.modelField(cap, ev, "evaluator."));
    if (cap && cap.effortField) fields.push(this.effortField(cap, ev, "evaluator."));
    const evEditor = createPromptEditor({
      value: ev.promptTemplate || "",
      fieldName: "evaluator.promptTemplate",
      placeholders: this.placeholders(),
      ghostAppend: GHOST_EVAL,
      onChange: (v) => {
        ev.promptTemplate = v;
        this.markDirty();
      },
    });
    return h(
      "div",
      {},
      this.focusHead(i18n.evalLoopTitle, node.id || "?"),
      ...fields,
      h("div", { class: "fgroup", dataset: { field: "evaluator.promptTemplate" } }, evEditor.element), // fgroup 补下边距，别让 loopCount 贴住编辑器
      this.loopCountField(node),
      h("div", { class: "loopclear" }, h("button", { class: "btn btn-danger-ghost", onClick: () => this.clearLoop(node) }, i18n.clearEvaluator)),
    );
  }

  // 回跳线聚焦面板：跳回目标下拉 + loopCount + 清除。
  loopFocusPanel(node) {
    return h(
      "div",
      {},
      this.focusHead(i18n.loopLineTitle, `${node.id || "?"} → ${node.redoTarget || "?"}`),
      h("div", { class: "fgroup", dataset: { field: "redoTarget" } }, h("label", { class: "flabel" }, i18n.redoJumpTo), this.redoTargetControl(node)),
      this.loopCountField(node),
      h("div", { class: "loopclear" }, h("button", { class: "btn btn-danger-ghost", onClick: () => this.clearLoop(node) }, i18n.clearRedo)),
    );
  }

  clearLoop(node) {
    this.setLoopMode(node, "once"); // 删 evaluator / redoTarget / loopCount
    this.focus = "node";
    this.renderFlow();
    this.renderInspector();
  }

  fieldID(node) {
    const hint = h("div", { class: "ferr", style: { display: "none" } });
    // 即时轻提示：id 非法即显示规则，不阻断输入（保存时服务端 nodeIDPattern 终裁）。镜像 validate.go。
    const check = (v) => {
      const bad = v !== "" && !NODE_ID_RE.test(v);
      hint.textContent = bad ? i18n.idInvalidHint : "";
      hint.style.display = bad ? "block" : "none";
    };
    check(node.id || "");
    return h(
      "div",
      { class: "fgroup", dataset: { field: "id" } },
      h(
        "label",
        { class: "flabel" },
        i18n.fID,
        h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.idNoteTpl(node.id || "id"))),
      ),
      this.textInput(node.id || "", (v) => (node.id = v), { mono: true, live: check }),
      hint,
    );
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

  liveName(v) {
    // 名称改动即时反映到左栏当前卡片标题（不整树重渲，保住输入焦点）。
    const card = this.flowCol.querySelector(".fnode-sel .nm");
    if (card) card.textContent = v || this.def.nodes[this.sel].id || i18n.unnamed;
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
    // model：能力表允许（或能力表未登记时保守给出）时渲染，一律自由文本。
    if (!cap || cap.allowsModel) {
      fields.push(this.modelField(cap, holder));
    }
    // effort / reasoningEffort：仅当能力表声明了 effortField 才渲染下拉。
    if (cap && cap.effortField) {
      fields.push(this.effortField(cap, holder));
    }
    return fields;
  }

  // scope 为字段路径前缀（节点配置 ""、评测官配置 "evaluator."），供保存错误红框定位到对应字段。
  // cap.modelValues 非空时挂一个自定义建议下拉（与 engine 选择器同一套 .engsel 视觉），
  // 但控件本身仍是真实 <input>——保留自由打字/光标/输入法，建议值只是点击可填的便利提示
  // （不是白名单，model 本身是开放集合）。为空（如 antigravity/codex）时退化为普通输入框。
  modelField(cap, holder, scope = "") {
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

    return h("div", { class: "fgroup", dataset: { field: scope + "engineConfig.model" } }, h("label", { class: "flabel" }, i18n.fModel), control);
  }

  // 自定义下拉（listSelect），与 engine 选择器同一套视觉/交互——原生 <select> 在 macOS 上会用
  // 系统级弹出样式（以当前选中项为中心展开），观感与 engine 选择器不统一。
  effortField(cap, holder, scope = "") {
    const field = cap.effortField;
    const cfg = () => holder.engineConfig || (holder.engineConfig = {});
    const current = (holder.engineConfig && holder.engineConfig[field]) || "";
    const items = [{ value: "", label: i18n.fEffortNotSet }, ...(cap.effortValues || []).map((v) => ({ value: v, label: v }))];
    const select = listSelect(current, items, (value) => {
      cfg()[field] = value;
      this.markDirty();
    });
    return h("div", { class: "fgroup", dataset: { field: scope + "engineConfig." + field } }, h("label", { class: "flabel" }, field), select);
  }

  // ---- 提示词编辑器字段 ----
  promptField(node) {
    const editor = createPromptEditor({
      value: node.promptTemplate || "",
      fieldName: "promptTemplate",
      placeholders: this.placeholders(),
      ghostAppend: node.evaluator ? GHOST_AGENT : null, // 带 evaluator 的节点才追加评测反馈
      onChange: (v) => {
        node.promptTemplate = v;
        this.markDirty();
      },
    });
    return h("div", { dataset: { field: "promptTemplate" } }, editor.element);
  }

  placeholders() {
    return ["{{sys.userPrompt}}", "{{sys.cwd}}", ...this.def.nodes.map((n) => `{{${n.id}}}`)];
  }

  // ---- 节点面板的「循环」入口区（替代原 radio 三选一）----
  // once：两个 CTA（设为评测循环 / 设为回跳循环）；已有循环：一行摘要，点击进对应聚焦面板。
  loopEntry(node) {
    const box = h("div", { class: "loopbox" }, h("span", { class: "radios-t" }, i18n.loopLabel));
    if (node.evaluator) {
      box.appendChild(this.loopSummaryRow(`↻ ${i18n.evalLoopTitle} ×${node.loopCount || 1}`, () => this.focusEvaluator(this.sel)));
    } else if (node.redoTarget) {
      box.appendChild(this.loopSummaryRow(`↺ ${i18n.redoJumpTo} ${node.redoTarget} ×${node.loopCount || 1}`, () => this.focusLoop(this.sel)));
    } else {
      // 回跳需至少一个前序节点；首节点（sel===0）无处可跳，禁用该 CTA。
      const canRedo = this.sel > 0;
      const evalBtn = h("button", { class: "btn btn-loop", onClick: () => this.startLoop(node, "eval") }, i18n.setEvalLoop);
      let redoEl;
      if (canRedo) {
        redoEl = h("button", { class: "btn btn-loop", onClick: () => this.startLoop(node, "redo") }, i18n.setRedoLoop);
      } else {
        // 禁用态：hover 弹自定义气泡说明原因（复用 .tip，与站内 info 气泡一致，不用原生 title）。
        const btn = h("button", { class: "btn btn-loop btn-loop-off" }, i18n.setRedoLoop);
        redoEl = h("span", { class: "btnhint" }, btn, h("span", { class: "tip" }, i18n.redoNeedsPrior));
      }
      box.appendChild(h("div", { class: "loopcta" }, evalBtn, redoEl));
    }
    return box;
  }

  loopSummaryRow(text, onClick) {
    return h("div", { class: "loopsum", onClick }, h("span", {}, text), h("span", { class: "loopsum-go" }, "›"));
  }

  // startLoop：建立循环后直接进入对应聚焦面板（评测循环 / 回跳线）。
  startLoop(node, mode) {
    this.setLoopMode(node, mode);
    if (mode === "eval") this.focusEvaluator(this.sel);
    else this.focusLoop(this.sel);
  }

  // setLoopMode 只改数据不触发渲染——渲染由调用方（startLoop / clearLoop）负责，避免重复重绘。
  setLoopMode(node, mode) {
    if (mode === "once") {
      delete node.evaluator;
      node.redoTarget = "";
      delete node.loopCount;
    } else if (mode === "eval") {
      node.redoTarget = "";
      if (!node.evaluator) node.evaluator = { engine: node.engine, engineConfig: {}, promptTemplate: DEFAULT_EVAL_PROMPT };
      if (!node.loopCount) node.loopCount = 1;
    } else if (mode === "redo") {
      // 默认回跳到前一个节点（若有）；无前序则空串（服务端校验兜底）。
      if (!node.redoTarget) node.redoTarget = this.sel > 0 ? this.def.nodes[this.sel - 1].id : "";
      if (!node.loopCount) node.loopCount = 1;
    }
    this.markDirty();
  }

  // 回跳目标下拉：只列本节点之前的节点 id（结构上杜绝非法回跳）。改动同步弧线与聚焦面板头部。
  redoTargetControl(node) {
    const priorIds = this.def.nodes.slice(0, this.sel).map((n) => n.id);
    return h(
      "select",
      {
        class: "inp",
        onChange: (e) => {
          node.redoTarget = e.target.value;
          this.markDirty();
          this.renderFlow();
          this.renderInspector(); // 头部 id → target 同步
        },
      },
      ...priorIds.map((id) => h("option", { value: id, selected: id === node.redoTarget }, id)),
    );
  }

  // loopCount 字段：标准上下布局（label 上、控件下），与其它字段一致。
  loopCountField(node) {
    const input = h("input", { class: "lc", type: "number", min: "1", max: "20" });
    input.value = String(node.loopCount || 1);
    input.addEventListener("input", () => {
      const n = parseInt(input.value, 10);
      node.loopCount = isNaN(n) ? input.value : n; // 非数字原样留给服务端报错，不静默纠正
      this.markDirty();
      this.updateBadges();
    });
    return h("div", { class: "fgroup", dataset: { field: "loopCount" } }, h("label", { class: "flabel" }, i18n.loopCount), input);
  }

  updateBadges() {
    // loopCount 改动即时反映到左栏卡片徽标 / 回跳弧线：重建整个流程列（含弧线重测重画）。
    this.renderFlow();
  }

  // ---- JSON 视图 ----
  jsonBody() {
    const json = JSON.stringify(this.def, null, 2);
    // 可编辑的 JSON 源码编辑器：基于 JSON 语法高亮（Prism，纯语法着色、不与 conduct 结构关联）。
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
      body = parsed;
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
      // 其它错误（名字不符等）：就地提示，不静默。
      this.showSaveError(err.message);
    }
  }

  onSaved(saved) {
    this.def = saved;
    this.baseUpdatedAt = saved.updatedAt;
    this.dirty = false;
    this.errors = [];
    if (this.sel >= this.def.nodes.length) this.sel = this.def.nodes.length - 1;
    this.renderAll();
  }

  showSaveError(message) {
    this.errors = [{ path: "", message }];
    this.renderAll();
  }

  // 乐观并发冲突：弹「覆盖保存 / 放弃重载」。
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
    const prune = (holder) => {
      const c = holder.engineConfig;
      // 所有字段皆空才删（与字段清单解耦：将来 engineConfig 加字段也不会把只填了新字段的配置误删）。
      if (c && Object.values(c).every((v) => !v)) delete holder.engineConfig;
    };
    for (const node of def.nodes || []) {
      prune(node);
      if (node.evaluator) prune(node.evaluator);
    }
  }

  // ---- 结构操作 ----
  addNode() {
    const id = this.uniqueId("node");
    // 默认名与 id 一致（不造「新节点」这类占位名），用户按需在名称字段改。
    this.def.nodes.push({ id, displayName: id, engine: engineNames()[0] || "claude-code", promptTemplate: DEFAULT_NODE_PROMPT });
    this.sel = this.def.nodes.length - 1;
    this.focus = "node";
    this.markDirty();
    this.renderFlow();
    this.renderInspector();
  }

  uniqueId(base) {
    const existing = new Set(this.def.nodes.map((n) => n.id));
    let i = this.def.nodes.length + 1;
    let id = `${base}-${i}`;
    while (existing.has(id)) id = `${base}-${++i}`;
    return id;
  }

  deleteNode(i) {
    this.def.nodes.splice(i, 1);
    if (this.sel >= this.def.nodes.length) this.sel = this.def.nodes.length - 1;
    this.focus = "node"; // 删节点后回节点面板，避免 focus 悬指到已变化的循环
    this.markDirty();
    this.renderFlow();
    this.renderInspector();
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

// 节点 id 即时校验用正则，镜像 internal/workflow/validate.go 的 nodeIDPattern（服务端为终裁）。
const NODE_ID_RE = /^[A-Za-z_][A-Za-z0-9_-]{0,63}$/;

// 运行时自动追加段的只读预告：展示真实追加的 ## 标题 + 一个尖括号占位描述即可（参考 x-one-web
// 的 appendedSuffix 做法），不必把 <previous_evaluator_feedback> 这类 XML 边界标签铺出来——
// 标题是 orchestrator buildStepInput 真实拼接的，占位描述让用户一眼看懂追加的是什么。
const GHOST_AGENT = "## Previous evaluator feedback\n\n<上一轮 evaluator 输出>";
const GHOST_EVAL = "## Artifact under review\n\n<本轮节点产物>";

// 新建节点 / 开启 evaluator 内循环时填充的默认提示词（与 x-one-web 的 DEFAULT_NODE_PROMPT /
// DEFAULT_EVAL_PROMPT 保持一致）。被评 artifact 由编排层自动 append，evaluator 默认模板不重复声明。
const DEFAULT_NODE_PROMPT =
  "## User request\n\n{{sys.userPrompt}}\n\n## Task\n\nComplete the task according to the user's request above.";
const DEFAULT_EVAL_PROMPT =
  "You are an independent quality reviewer.\n\n## Original user request\n\n{{sys.userPrompt}}\n\n## Your task\n\nProvide specific, actionable feedback on quality, correctness and completeness.";
