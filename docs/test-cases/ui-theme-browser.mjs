#!/usr/bin/env node

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import process from "node:process";

const argumentsByName = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  argumentsByName.set(process.argv[index], process.argv[index + 1]);
}
const scenario = argumentsByName.get("--scenario");
const uiURL = argumentsByName.get("--url");
const profileDirectory = argumentsByName.get("--profile");
const screenshotPath = argumentsByName.get("--screenshot");
let activeChromeChild;

if (!scenario || !uiURL || !profileDirectory) {
  throw new Error("用法: node ui-theme-browser.mjs --scenario <follow-system|toggle|settings|visual> --url <UI_URL> --profile <DIR> [--screenshot <PNG>]");
}

function chromeExecutable() {
  if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
  if (process.platform === "darwin") return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
  if (process.platform === "win32") return "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
  return "google-chrome";
}

class DevToolsClient {
  constructor(webSocketURL) {
    this.nextID = 1;
    this.pending = new Map();
    this.errors = [];
    this.socket = new WebSocket(webSocketURL);
    this.ready = new Promise((resolve, reject) => {
      this.socket.addEventListener("open", resolve, { once: true });
      this.socket.addEventListener("error", reject, { once: true });
    });
    this.socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id);
        if (message.error) pending.reject(new Error(`${pending.method}: ${message.error.message}`));
        else pending.resolve(message.result);
        return;
      }
      if (message.method === "Runtime.exceptionThrown") {
        this.errors.push(message.params.exceptionDetails.text);
      }
      if (message.method === "Log.entryAdded" && message.params.entry.level === "error") {
        this.errors.push(message.params.entry.text);
      }
    });
  }

  async send(method, params = {}) {
    await this.ready;
    const id = this.nextID++;
    const response = new Promise((resolve, reject) => this.pending.set(id, { method, resolve, reject }));
    this.socket.send(JSON.stringify({ id, method, params }));
    return response;
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
    });
    if (result.exceptionDetails) {
      throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
    }
    return result.result.value;
  }

  close() {
    this.socket.close();
  }
}

async function waitFor(check, message, timeoutMilliseconds = 5000) {
  const deadline = Date.now() + timeoutMilliseconds;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const value = await check();
      if (value) return value;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`${message}${lastError ? `: ${lastError.message}` : ""}`);
}

async function launchChrome() {
  await mkdir(profileDirectory, { recursive: true });
  const child = spawn(chromeExecutable(), [
    "--headless=new",
    "--remote-debugging-address=127.0.0.1",
    "--remote-debugging-port=0",
    `--user-data-dir=${profileDirectory}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    "about:blank",
  ], { stdio: ["ignore", "ignore", "pipe"] });
  activeChromeChild = child;

  let diagnostics = "";
  let webSocketURL;
  try {
    webSocketURL = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`Chrome DevTools 启动超时\n${diagnostics}`)), 10000);
      child.stderr.on("data", (chunk) => {
        diagnostics += chunk.toString();
        const match = diagnostics.match(/DevTools listening on (ws:\/\/[^\s]+)/);
        if (match) {
          clearTimeout(timer);
          resolve(match[1]);
        }
      });
      child.once("exit", (code) => {
        clearTimeout(timer);
        reject(new Error(`Chrome 在 DevTools 就绪前退出 ${code}\n${diagnostics}`));
      });
      child.once("error", reject);
    });
  } catch (error) {
    child.kill("SIGTERM");
    throw error;
  }
  const endpoint = webSocketURL.replace(/^ws:/, "http:").replace(/\/devtools\/browser\/.*$/, "");
  return { child, endpoint };
}

async function openPage(endpoint, colorScheme) {
  const response = await fetch(`${endpoint}/json/new?${encodeURIComponent("about:blank")}`, { method: "PUT" });
  assert.equal(response.ok, true, `创建浏览器标签页失败: HTTP ${response.status}`);
  const target = await response.json();
  const client = new DevToolsClient(target.webSocketDebuggerUrl);
  await Promise.all([
    client.send("Page.enable"),
    client.send("Runtime.enable"),
    client.send("Log.enable"),
  ]);
  await client.send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: colorScheme }],
  });
  await client.send("Page.navigate", { url: uiURL });
  await waitFor(
    () => client.evaluate(`document.readyState === "complete" && Boolean(document.querySelector("#settings-link[aria-label]"))`),
    "UI 页面未就绪",
  );
  return client;
}

async function pageState(client) {
  return client.evaluate(`(async () => {
    const settings = await fetch("/api/settings", { cache: "no-store" }).then((response) => response.json());
    return {
      theme: document.documentElement.dataset.theme,
      preference: document.documentElement.dataset.themeSetting,
      background: getComputedStyle(document.body).backgroundColor,
      language: document.documentElement.lang,
      settings,
    };
  })()`);
}

async function selectSetting(client, setting, value) {
  await client.evaluate(`location.hash = "#/settings"`);
  await waitFor(() => client.evaluate(`Boolean(document.querySelector('[data-setting="${setting}"] .engsel-display'))`), `设置项 ${setting} 未就绪`);
  await client.evaluate(`document.querySelector('[data-setting="${setting}"] .engsel-display').click()`);
  await client.evaluate(`document.querySelector('[data-setting="${setting}"] .engsel-item[data-value="${value}"]').click()`);
  const expected = value === "" ? null : value;
  await waitFor(
    () => client.evaluate(`fetch("/api/settings", { cache: "no-store" }).then((response) => response.json()).then((settings) => settings.${setting} === ${JSON.stringify(expected)})`),
    `设置项 ${setting} 未保存`,
  );
}

async function setSystemTheme(client, colorScheme) {
  await client.send("Emulation.setEmulatedMedia", {
    features: [{ name: "prefers-color-scheme", value: colorScheme }],
  });
}

async function runFollowSystem(endpoint, clients) {
  const light = await openPage(endpoint, "light");
  const dark = await openPage(endpoint, "dark");
  clients.push(light, dark);
  assert.deepEqual(await pageState(light), {
    theme: "light", preference: "", background: "rgb(247, 247, 245)", language: "en",
    settings: { language: null, resolvedLanguage: "en", theme: null },
  });
  assert.deepEqual(await pageState(dark), {
    theme: "dark", preference: "", background: "rgb(11, 16, 24)", language: "en",
    settings: { language: null, resolvedLanguage: "en", theme: null },
  });
}

async function runToggle(endpoint, clients) {
  const page = await openPage(endpoint, "light");
  clients.push(page);
  await selectSetting(page, "theme", "dark");
  await waitFor(() => page.evaluate(`document.documentElement.dataset.theme === "dark"`), "设置页未切换到 dark");
  assert.deepEqual(await pageState(page), {
    theme: "dark", preference: "dark", background: "rgb(11, 16, 24)", language: "en",
    settings: { language: null, resolvedLanguage: "en", theme: "dark" },
  });

  await page.send("Page.reload", { ignoreCache: true });
  await waitFor(() => page.evaluate(`document.readyState === "complete" && document.documentElement.dataset.theme === "dark"`), "刷新后未保持 dark");
  assert.equal((await pageState(page)).settings.theme, "dark");

  await selectSetting(page, "theme", "light");
  await waitFor(() => page.evaluate(`document.documentElement.dataset.theme === "light"`), "设置页未切回 light");
  assert.deepEqual(await pageState(page), {
    theme: "light", preference: "light", background: "rgb(247, 247, 245)", language: "en",
    settings: { language: null, resolvedLanguage: "en", theme: "light" },
  });
}

async function runSettings(endpoint, clients) {
  const page = await openPage(endpoint, "light");
  clients.push(page);
  await selectSetting(page, "language", "zh-CN");
  await waitFor(() => page.evaluate(`document.documentElement.lang === "zh-CN" && document.querySelector(".settings-head h1").textContent === "设置"`), "语言未切换到中文");
  await selectSetting(page, "theme", "dark");
  let state = await pageState(page);
  assert.equal(state.settings.language, "zh-CN");
  assert.equal(state.settings.theme, "dark");
  await selectSetting(page, "language", "en");
  state = await pageState(page);
  assert.equal(state.language, "en");
  assert.equal(state.settings.theme, "dark", "修改语言不应覆盖主题");
  await selectSetting(page, "theme", "");
  await setSystemTheme(page, "light");
  await waitFor(() => page.evaluate(`document.documentElement.dataset.theme === "light"`), "跟随系统未恢复 light");
}

async function runVisual(endpoint, clients) {
  assert.ok(screenshotPath, "visual 场景必须传 --screenshot");
  const page = await openPage(endpoint, "light");
  clients.push(page);
  const creationStatus = await page.evaluate(`fetch("/api/workflows", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: "theme-check" }),
  }).then((response) => response.status)`);
  assert.equal(creationStatus, 201);
  await page.evaluate(`location.hash = "#/workflows/theme-check"`);
  await waitFor(() => page.evaluate(`Boolean(document.querySelector(".insp") && document.querySelector(".nb") && document.querySelector(".edge"))`), "工作流编辑器未就绪");
  await page.evaluate(`(() => {
    const input = document.querySelector(".pe-input");
    input.value = "# 标题 {{sys.userPrompt}}";
    input.dispatchEvent(new Event("input", { bubbles: true }));
  })()`);
  await waitFor(() => page.evaluate(`Boolean(document.querySelector(".token.title") && document.querySelector(".token.placeholder"))`), "提示词语法高亮未生成");

  const lightLayout = await page.evaluate(`[".topbar", ".canvascol", ".insp", ".pe-wrap"].map((selector) => {
    const rectangle = document.querySelector(selector).getBoundingClientRect();
    return [rectangle.x, rectangle.y, rectangle.width, rectangle.height];
  })`);
  await page.evaluate(`fetch("/api/settings", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ theme: "dark" }),
  }).then((response) => {
    if (!response.ok) throw new Error("failed to persist dark theme");
    globalThis.conductTheme.setPreference("dark");
  })`);
  await waitFor(() => page.evaluate(`document.documentElement.dataset.theme === "dark"`), "视觉场景未切换到 dark");
  const darkLayout = await page.evaluate(`[".topbar", ".canvascol", ".insp", ".pe-wrap"].map((selector) => {
    const rectangle = document.querySelector(selector).getBoundingClientRect();
    return [rectangle.x, rectangle.y, rectangle.width, rectangle.height];
  })`);
  assert.deepEqual(darkLayout, lightLayout, "主题切换不应改变主要区域布局");

  const colors = await page.evaluate(`(() => {
    const style = (selector) => getComputedStyle(document.querySelector(selector));
    return {
      inspectorBackground: style(".insp").backgroundColor,
      inspectorBorder: style(".insp").borderColor,
      nodeFill: style(".nb").fill,
      edgeStroke: style(".edge").stroke,
      heading: style(".token.title").color,
      placeholder: style(".token.placeholder").color,
      placeholderBackground: style(".token.placeholder").backgroundColor,
    };
  })()`);
  assert.deepEqual(colors, {
    inspectorBackground: "rgb(25, 36, 52)",
    inspectorBorder: "rgb(39, 53, 72)",
    nodeFill: "rgb(20, 28, 39)",
    edgeStroke: "rgb(94, 113, 137)",
    heading: "rgb(138, 171, 255)",
    placeholder: "rgb(233, 167, 124)",
    placeholderBackground: "rgb(61, 47, 36)",
  });

  const screenshot = await page.send("Page.captureScreenshot", { format: "png", fromSurface: true });
  await writeFile(screenshotPath, Buffer.from(screenshot.data, "base64"));
}

const clients = [];
for (const signal of ["SIGHUP", "SIGINT", "SIGTERM"]) {
  process.once(signal, () => {
    activeChromeChild?.kill("SIGTERM");
    process.exit({ SIGHUP: 129, SIGINT: 130, SIGTERM: 143 }[signal]);
  });
}
const chrome = await launchChrome();
try {
  if (scenario === "follow-system") await runFollowSystem(chrome.endpoint, clients);
  else if (scenario === "toggle") await runToggle(chrome.endpoint, clients);
  else if (scenario === "settings") await runSettings(chrome.endpoint, clients);
  else if (scenario === "visual") await runVisual(chrome.endpoint, clients);
  else throw new Error(`未知场景: ${scenario}`);

  const browserErrors = clients.flatMap((client) => client.errors);
  assert.deepEqual(browserErrors, [], `浏览器控制台错误: ${browserErrors.join(" | ")}`);
  console.log(`PASS: ${scenario}`);
} finally {
  for (const client of clients) client.close();
  chrome.child.kill("SIGTERM");
  await new Promise((resolve) => {
    const timer = setTimeout(resolve, 2000);
    chrome.child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
  activeChromeChild = undefined;
}
