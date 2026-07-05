// 工作目录选择器：应用内目录浏览弹窗。浏览器拿不到用户选文件夹的绝对路径（安全限制），
// 故由本地服务端（GET /api/fs）列目录、回传真实绝对路径，前端只做导航与回填。
//
// openFolderBrowser(startPath, onPick)：从 startPath（空则服务端用主目录）打开弹窗；
// 点子目录进入、「上级」返回、「选择此目录」把当前绝对路径交给 onPick(absPath)。

import { h } from "./dom.js";
import { api } from "./api.js";
import { openModal } from "./modal.js";
import { i18n } from "./i18n.js";

// cwdRow 把工作目录输入框与「浏览…」按钮并排成一行；选定目录回填输入框并清掉错误态。
export function cwdRow(cwdInput, cwdErr) {
  const browse = h(
    "button",
    {
      class: "ghost fsb-browse",
      type: "button",
      onClick: () =>
        openFolderBrowser(cwdInput.value.trim(), (p) => {
          cwdInput.value = p;
          cwdInput.classList.remove("inp-red");
          if (cwdErr) cwdErr.style.display = "none";
        }),
    },
    i18n.browseBtn,
  );
  return h("div", { class: "cwdrow" }, cwdInput, browse);
}

export function openFolderBrowser(startPath, onPick) {
  const crumb = h("span", { class: "fsb-path" });
  const listBox = h("div", { class: "fsb-list" });
  const errBox = h("div", { class: "ferr", style: { display: "none" } });
  const filterInput = h("input", { class: "inp fsb-filter", type: "text", placeholder: i18n.fsFilter });
  let current = "";
  let parent = "";
  let allEntries = []; // 当前目录全部子目录，过滤只在前端做，不再打服务端

  filterInput.addEventListener("input", () => {
    const q = filterInput.value.trim().toLowerCase();
    renderEntries(q ? allEntries.filter((e) => e.name.toLowerCase().includes(q)) : allEntries);
  });

  const upBtn = h("button", { class: "ghost", type: "button", onClick: () => parent && load(parent) }, i18n.upLevel);
  const selectBtn = h("button", { class: "btn btn-ink" }, i18n.selectThisDir);
  selectBtn.addEventListener("click", () => {
    if (!current) return;
    onPick(current);
    ctl.close();
  });

  async function load(path) {
    errBox.style.display = "none";
    try {
      const res = await api.listDir(path);
      current = res.path;
      parent = res.parent || "";
      crumb.textContent = res.path;
      upBtn.disabled = !parent;
      allEntries = res.entries || [];
      filterInput.value = ""; // 进入新目录清掉上一次的过滤词
      renderEntries(allEntries);
    } catch (e) {
      // 目录不存在 / 无权限等：就地显示服务端原文，不静默。
      errBox.textContent = e.message;
      errBox.style.display = "block";
    }
  }

  function renderEntries(entries) {
    listBox.innerHTML = "";
    if (!entries.length) {
      listBox.appendChild(h("div", { class: "fsb-empty" }, i18n.fsEmptyDir));
      return;
    }
    for (const ent of entries) {
      listBox.appendChild(
        h(
          "div",
          { class: "fsb-item", onClick: () => load(ent.path) },
          h("span", { class: "fsb-ic" }, "📁"),
          h("span", { class: "fsb-nm" }, ent.name),
          h("span", { class: "fsb-go" }, "›"),
        ),
      );
    }
  }

  const body = h("div", {}, h("div", { class: "fsb-bar" }, upBtn, crumb), filterInput, listBox, errBox);
  const ctl = openModal({
    title: i18n.pickFolderTitle,
    width: "520px",
    body,
    footer: [h("button", { class: "btn", onClick: () => ctl.close() }, i18n.cancel), selectBtn],
  });
  // 起步目录：输入框里已是绝对路径就从那儿开始，否则交服务端用主目录。
  load(startPath && startPath.startsWith("/") ? startPath : "");
}
