// conduct ui 前端入口：拉版本号填顶栏、登记四条路由、启动 hash 路由器。

import { register, rerender, setNotFound, start } from "./router.js";
import { api } from "./api.js";
import { loadEngines } from "./engines.js";
import { renderWorkflowsPage } from "./pages/workflows.js";
import { renderEditorPage } from "./pages/editor.js";
import { renderRunsPage } from "./pages/runs.js";
import { renderRunDetailPage } from "./pages/run-detail.js";
import { i18n, setLanguage } from "./i18n.js";

register("/workflows", (params, query, outlet) => renderWorkflowsPage(outlet));
register("/workflows/:name", (params, query, outlet) => renderEditorPage(outlet, params.name));
register("/runs", (params, query, outlet) => renderRunsPage(outlet, query));
register("/runs/:id", (params, query, outlet) => renderRunDetailPage(outlet, params.id));

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
  document.getElementById("language-label").textContent = i18n.language;
  const select = document.getElementById("language");
  select.options[0].textContent = i18n.followEnvironment;
  select.options[1].textContent = i18n.chinese;
  select.options[2].textContent = i18n.english;
  select.value = settings.language || "";
}

async function bootstrap() {
  let currentSettings = await api.settings();
  setLanguage(currentSettings.resolvedLanguage);
  syncChrome(currentSettings);

  const languageSelect = document.getElementById("language");
  languageSelect.addEventListener("change", async () => {
    const requestedLanguage = languageSelect.value || null;
    languageSelect.disabled = true;
    try {
      const updated = await api.updateLanguage(requestedLanguage);
      setLanguage(updated.resolvedLanguage);
      syncChrome(updated);
      currentSettings = updated;
      rerender();
    } catch (err) {
      languageSelect.value = currentSettings.language || "";
      window.alert(i18n.settingsSaveFail + err.message);
    } finally {
      languageSelect.disabled = false;
    }
  });

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
  syncChrome({ language: null, resolvedLanguage: "en" });
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
