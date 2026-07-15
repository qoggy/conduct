// 全局设置页（#/settings）：语言与主题均写入 ~/.conduct/settings.json，通过严格 settings API 更新。

import { h, mount } from "../dom.js";
import { listSelect } from "../custom-select.js";
import { i18n } from "../i18n.js";

export function renderSettingsPage(outlet, settings, updateSettings, errorMessage = "") {
  if (!settings) return;

  const languageSelect = listSelect(
    settings.language || "",
    [
      { value: "", label: i18n.followEnvironment },
      { value: "zh-CN", label: i18n.chinese },
      { value: "en", label: i18n.english },
    ],
    (value) => save({ language: value || null }),
    { label: i18n.language },
  );
  languageSelect.dataset.setting = "language";

  const themeSelect = listSelect(
    settings.theme || "",
    [
      { value: "", label: i18n.followSystem },
      { value: "light", label: i18n.themeLight },
      { value: "dark", label: i18n.themeDark },
    ],
    (value) => save({ theme: value || null }),
    { label: i18n.theme },
  );
  themeSelect.dataset.setting = "theme";

  let saving = false;
  async function save(update) {
    if (saving) return;
    saving = true;
    page.classList.add("settings-saving");
    try {
      await updateSettings(update);
    } catch (err) {
      renderSettingsPage(outlet, settings, updateSettings, i18n.settingsSaveFail + err.message);
    }
  }

  const page = h(
    "div",
    { class: "page settings-page" },
    h("div", { class: "settings-head" }, h("h1", {}, i18n.settingsTitle), h("p", {}, i18n.settingsSubtitle)),
    errorMessage ? h("div", { class: "settings-error", role: "alert" }, errorMessage) : null,
    h(
      "div",
      { class: "settings-card" },
      settingRow(i18n.language, i18n.settingsLanguageHint, languageSelect),
      settingRow(i18n.theme, i18n.settingsThemeHint, themeSelect),
    ),
  );
  mount(outlet, page);
}

function settingRow(title, hint, control) {
  return h(
    "div",
    { class: "settings-row" },
    h("div", { class: "settings-copy" }, h("div", { class: "settings-label" }, title), h("div", { class: "settings-hint" }, hint)),
    h("div", { class: "settings-control" }, control),
  );
}
