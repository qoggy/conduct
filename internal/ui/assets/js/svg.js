// SVG 元素构建助手：dom.js 的 h() 用 document.createElement，无法正确创建 SVG 元素（缺少
// SVG 命名空间，浏览器不按图形元素解释）。DAG 画布（编辑器 / 运行详情共用）改用本文件的 svg()。
//
// svg(tag, attrs, ...children) 用法与 h() 一致：attrs 里 on* 挂事件、style 接对象、其余按
// setAttribute 设置（SVG 元素没有可写的 .className，故一律走 attribute）。

const SVG_NS = "http://www.w3.org/2000/svg";

export function svg(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) {
      if (value === null || value === undefined || value === false) continue;
      if (key.startsWith("on") && typeof value === "function") {
        el.addEventListener(key.slice(2).toLowerCase(), value);
      } else if (key === "style" && typeof value === "object") {
        Object.assign(el.style, value);
      } else {
        el.setAttribute(key, value === true ? "" : String(value));
      }
    }
  }
  appendChildren(el, children);
  return el;
}

function appendChildren(el, children) {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    if (Array.isArray(child)) {
      appendChildren(el, child);
    } else if (child instanceof Node) {
      el.appendChild(child);
    } else {
      el.appendChild(document.createTextNode(String(child)));
    }
  }
}

// foreignHTML 把一个真实 HTML 节点（如 engineIconEl 返回的 <img>/<span>）嵌进 SVG 画布：
// 引擎图标复用现成的 HTML 组件，不必在 SVG 里重新手刻一套图标绘制逻辑。
export function foreignHTML(x, y, w, h, htmlNode) {
  const fo = document.createElementNS(SVG_NS, "foreignObject");
  fo.setAttribute("x", x);
  fo.setAttribute("y", y);
  fo.setAttribute("width", w);
  fo.setAttribute("height", h);
  const div = document.createElement("div");
  div.style.width = "100%";
  div.style.height = "100%";
  div.style.display = "flex";
  div.style.alignItems = "center";
  div.style.justifyContent = "center";
  div.appendChild(htmlNode);
  fo.appendChild(div);
  return fo;
}

// clientToLocal 把一个 pointer 事件的 client 坐标换算成某个 <svg> 内部的用户坐标系坐标——
// 画布超列宽时会被 CSS max-width:100% 等比缩小，viewBox 用户单位与屏幕像素不再 1:1，必须走 CTM
// 逆变换，否则拖拽连边在非 1:1 缩放下会跟手错位。
export function clientToLocal(svgRoot, clientX, clientY) {
  const ctm = svgRoot.getScreenCTM();
  if (!ctm) return { x: clientX, y: clientY };
  const pt = svgRoot.createSVGPoint();
  pt.x = clientX;
  pt.y = clientY;
  const local = pt.matrixTransform(ctm.inverse());
  return { x: local.x, y: local.y };
}
