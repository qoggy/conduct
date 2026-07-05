// conduct ui 前端入口：拉版本号填顶栏、登记四条路由、启动 hash 路由器。

import { register, setNotFound, start } from "./router.js";
import { api } from "./api.js";
import { loadEngines } from "./engines.js";
import { renderWorkflowsPage } from "./pages/workflows.js";
import { renderEditorPage } from "./pages/editor.js";
import { renderRunsPage } from "./pages/runs.js";
import { renderRunDetailPage } from "./pages/run-detail.js";
import { i18n } from "./i18n.js";

// 顶栏导航文案从字典回填（index.html 里是占位，i18n 才是唯一事实源）。
const tabText = { workflows: i18n.navWorkflows, runs: i18n.navRuns };
for (const tab of document.querySelectorAll(".tab[data-tab]")) {
  const t = tabText[tab.dataset.tab];
  if (t) tab.textContent = t;
}

// 顶栏版本号（= conduct version）。失败不致命，留空即可。
api
  .version()
  .then((v) => {
    document.getElementById("version").textContent = v.version;
  })
  .catch(() => {});

// 引擎能力表先行预热（检查器 / 启动无关，但编辑器要用）；失败静默，编辑器打开时会再拉一次报错。
loadEngines().catch(() => {});

register("/workflows", (params, query, outlet) => renderWorkflowsPage(outlet));
register("/workflows/:name", (params, query, outlet) => renderEditorPage(outlet, params.name));
register("/runs", (params, query, outlet) => renderRunsPage(outlet, query));
register("/runs/:id", (params, query, outlet) => renderRunDetailPage(outlet, params.id));

setNotFound((outlet) => {
  outlet.innerHTML = "";
  const box = document.createElement("div");
  box.className = "page";
  box.innerHTML = `<div class="empty"><h2>${i18n.notFoundTpl("页面")}</h2><p><a class="link" href="#/workflows">回到工作流列表</a></p></div>`;
  outlet.appendChild(box);
});

start(document.getElementById("app"));
