// 启动运行 / 改名 两个弹窗：工作流列表页与编辑器顶栏共用同一份实现，避免两处抄写各自漂移。
// 二者交互完全一致，仅「成功后去哪」由调用方经回调注入（列表页留原页重载、编辑器跳到新名）。

import { h } from "./dom.js";
import { api } from "./api.js";
import { openModal } from "./modal.js";
import { createPromptEditor } from "./prompt-editor.js";
import { cwdRow } from "./folder-picker.js";
import { navigate } from "./router.js";
import { i18n } from "./i18n.js";

// openLaunchDialog 启动某工作流的一次运行：填需求 + 工作目录，发射后进运行详情；
// 超时未确认 run id（子进程仍在跑）则进运行列表，不误报失败。
export function openLaunchDialog(name) {
  const editor = createPromptEditor({ value: "", fieldName: i18n.fPrompt, placeholders: [], plain: true });
  const promptErr = h("div", { class: "ferr", style: { display: "none" } });
  const cwdInput = h("input", { class: "inp inp-mono", placeholder: i18n.cwdPlaceholder });
  const cwdErr = h("div", { class: "ferr", style: { display: "none" } });
  const runBtn = h("button", { class: "btn btn-ink" }, i18n.launchBtn);
  runBtn.addEventListener("click", async () => {
    promptErr.style.display = cwdErr.style.display = "none";
    const userPrompt = editor.getValue().trim();
    // 前端即时把守，防误发空需求烧 token（服务端仍会二次把守）。
    if (!userPrompt) {
      promptErr.textContent = i18n.launchNeedPrompt;
      promptErr.style.display = "block";
      return;
    }
    runBtn.disabled = true;
    try {
      const res = await api.launchRun(name, userPrompt, cwdInput.value.trim());
      ctl.close();
      navigate(res.runId ? `/runs/${encodeURIComponent(res.runId)}` : "/runs");
    } catch (e) {
      // 目录不存在等就地报错（服务端原文）；cwd 相关归工作目录字段，其余归需求。
      if (/工作目录|目录|cwd/.test(e.message)) {
        cwdInput.classList.add("inp-red");
        cwdErr.textContent = e.message;
        cwdErr.style.display = "block";
      } else {
        promptErr.textContent = e.message;
        promptErr.style.display = "block";
      }
      runBtn.disabled = false;
    }
  });
  const ctl = openModal({
    title: i18n.dlgLaunchTitleTpl(name),
    width: "560px",
    body: h(
      "div",
      {},
      h("div", { style: { marginBottom: "14px" } }, editor.element, promptErr),
      h("div", {}, h("label", { class: "flabel" }, i18n.fCwd), cwdRow(cwdInput, cwdErr), cwdErr),
    ),
    footer: [h("button", { class: "btn", onClick: () => ctl.close() }, i18n.cancel), runBtn],
  });
}

// openRenameDialog 改名；成功后调用 onSuccess(newName)——由调用方决定去向。
export function openRenameDialog(name, onSuccess) {
  const input = h("input", { class: "inp inp-mono" });
  input.value = name;
  const err = h("div", { class: "ferr", style: { display: "none" } });
  const okBtn = h("button", { class: "btn btn-ink" }, i18n.rename);
  okBtn.addEventListener("click", async () => {
    const newName = input.value.trim();
    okBtn.disabled = true;
    err.style.display = "none";
    try {
      await api.renameWorkflow(name, newName);
      ctl.close();
      onSuccess(newName);
    } catch (e) {
      err.textContent = e.message;
      err.style.display = "block";
      okBtn.disabled = false;
    }
  });
  const ctl = openModal({
    title: i18n.dlgRenameTitleTpl(name),
    body: h(
      "div",
      {},
      h("label", { class: "flabel" }, i18n.fNewName, h("span", { class: "info" }, "i", h("span", { class: "tip" }, i18n.renameNote))),
      input,
      err,
    ),
    footer: [h("button", { class: "btn", onClick: () => ctl.close() }, i18n.cancel), okBtn],
  });
}
