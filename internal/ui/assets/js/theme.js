// 主题首屏初始化与切换。服务端把 settings.theme 注入 <html data-theme-setting>，本脚本在
// 样式表之前同步解析；空值表示跟随系统。页面交互通过 globalThis.conductTheme 绑定。

(() => {
  "use strict";

  const root = document.documentElement;
  const darkMedia = globalThis.matchMedia ? globalThis.matchMedia("(prefers-color-scheme: dark)") : null;
  const listeners = new Set();

  const validTheme = (value) => value === "light" || value === "dark";

  function systemTheme() {
    return darkMedia && darkMedia.matches ? "dark" : "light";
  }

  let preference = validTheme(root.dataset.themeSetting) ? root.dataset.themeSetting : null;
  let currentTheme = null;

  function applyTheme(theme) {
    currentTheme = theme;
    root.dataset.theme = theme;
    root.style.colorScheme = theme;
    for (const listener of listeners) listener(theme);
  }

  function setPreference(value) {
    if (value !== null && !validTheme(value)) throw new Error(`unsupported theme preference ${value}`);
    preference = value;
    root.dataset.themeSetting = value || "";
    applyTheme(preference || systemTheme());
  }

  function subscribe(listener) {
    listeners.add(listener);
    listener(currentTheme);
    return () => listeners.delete(listener);
  }

  applyTheme(preference || systemTheme());

  if (darkMedia && darkMedia.addEventListener) {
    darkMedia.addEventListener("change", () => {
      if (preference === null) applyTheme(systemTheme());
    });
  }

  globalThis.conductTheme = Object.freeze({
    get current() {
      return currentTheme;
    },
    get preference() {
      return preference;
    },
    setPreference,
    subscribe,
  });
})();
