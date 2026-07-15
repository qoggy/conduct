// conduct ui 前端入口：拉全局设置与版本号、登记五条路由、启动 hash 路由器。

import { register, rerender, setNotFound, start } from "./router.js";
import { api } from "./api.js";
import { loadEngines } from "./engines.js";
import { renderWorkflowsPage } from "./pages/workflows.js";
import { renderEditorPage } from "./pages/editor.js";
import { renderRunsPage } from "./pages/runs.js";
import { renderRunDetailPage } from "./pages/run-detail.js";
import { renderSettingsPage } from "./pages/settings.js";
import { i18n, setLanguage } from "./i18n.js";

const themeController = globalThis.conductTheme;
let currentSettings = null;

register("/workflows", (params, query, outlet) => renderWorkflowsPage(outlet));
register("/workflows/:name", (params, query, outlet) => renderEditorPage(outlet, params.name));
register("/runs", (params, query, outlet) => renderRunsPage(outlet, query));
register("/runs/:id", (params, query, outlet) => renderRunDetailPage(outlet, params.id));
register("/settings", (params, query, outlet) => renderSettingsPage(outlet, currentSettings, applySettings));

setNotFound((outlet) => {
  outlet.innerHTML = "";
  const box = document.createElement("div");
  box.className = "page";
  box.innerHTML = `<div class="empty"><h2></h2><p><a class="link" href="#/workflows"></a></p></div>`;
  box.querySelector("h2").textContent = i18n.notFoundTpl(i18n.page);
  box.querySelector("a").textContent = i18n.backToWorkflows;
  outlet.appendChild(box);
});

function syncChrome(settings) {
  document.documentElement.lang = settings.resolvedLanguage;
  const tabText = { workflows: i18n.navWorkflows, runs: i18n.navRuns };
  for (const tab of document.querySelectorAll(".tab[data-tab]")) {
    tab.textContent = tabText[tab.dataset.tab] || "";
  }
  const settingsLink = document.getElementById("settings-link");
  settingsLink.setAttribute("aria-label", i18n.settings);
  settingsLink.setAttribute("title", i18n.settings);
  if (themeController) themeController.setPreference(settings.theme || null);
}

async function applySettings(update) {
  const updated = await api.updateSettings(update);
  setLanguage(updated.resolvedLanguage);
  currentSettings = updated;
  syncChrome(updated);
  rerender();
}

async function bootstrap() {
  currentSettings = await api.settings();
  setLanguage(currentSettings.resolvedLanguage);
  syncChrome(currentSettings);

  start(document.getElementById("app"));

  // 顶栏版本号（= conduct version）。失败不致命，留空即可。
  api
    .version()
    .then((v) => {
      document.getElementById("version").textContent = v.version;
    })
    .catch(() => {});

  // 引擎能力表先行预热；失败时编辑器打开后会正常展示请求错误。
  loadEngines().catch(() => {});
}

bootstrap().catch((err) => {
  setLanguage("en");
  syncChrome({ language: null, resolvedLanguage: "en", theme: null });
  const app = document.getElementById("app");
  app.innerHTML = "";
  const box = document.createElement("div");
  box.className = "page";
  box.innerHTML = `<div class="loaderr"><span class="render-fail"></span><span class="mono"></span></div>`;
  box.querySelector(".render-fail").textContent = i18n.renderFail;
  box.querySelector(".mono").textContent = err && err.message ? err.message : String(err);
  app.appendChild(box);
  console.error("conduct ui: application initialization failed", err);
});
