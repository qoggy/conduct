// 语法高亮：基于 Prism（vendor/prism.min.js，含 markdown / json 语法），返回高亮后的 HTML 串。
// Prism 由 index.html 以非模块脚本先行加载，运行时以 globalThis.Prism 取用。
//
// 提示词用 markdown 语法 + 自定义 {{占位符}} token（conduct 模板占位符）；JSON 用标准 JSON 语法
// （纯语法着色，不与 conduct 定义结构关联）。着色纯装饰、不改内容。

const P = globalThis.Prism;

// conduct-markdown：在 markdown 语法之上加最高优先级的模板占位符 token。pattern 与 conduct 后端
// render.go / validate.go 的 templateVariablePattern（\\?\{\{([a-zA-Z_][\w.-]*)\}\}）严格同源：
//   - {{key}}    活占位符（运行时替换；key 须 [a-zA-Z_][\w.-]*）→ placeholder
//   - \{{key}}   转义：后端渲染成字面 {{key}} 不替换 → placeholder-escaped（着色区分，不当活占位符）
//   - {{ }} / {{123}} 等不匹配 pattern → 普通文本（与运行时"不解析"一致，不制造"高亮却不被解析"的错位）
// escaped 必须排在 placeholder 之前：否则 placeholder 的 \{\{ 会先吃掉 {{key}} 段、把反斜杠漏成文本。
let conductMarkdown = null;
if (P && P.languages && P.languages.markdown) {
  conductMarkdown = Object.assign(
    {
      "placeholder-escaped": { pattern: /\\\{\{[a-zA-Z_][\w.-]*\}\}/, greedy: true },
      placeholder: { pattern: /\{\{[a-zA-Z_][\w.-]*\}\}/, greedy: true },
    },
    P.languages.markdown,
  );
}

// highlightHTML 把源码高亮成 HTML 串。lang: "markdown-conduct" | "markdown" | "json"。
// Prism 不可用时回退到转义纯文本（不静默丢内容）。
export function highlightHTML(src, lang) {
  const text = src || "";
  if (!P || !P.highlight) return escapeHTML(text);
  if (lang === "json" && P.languages.json) return P.highlight(text, P.languages.json, "json");
  if (lang === "markdown-conduct" && conductMarkdown) return P.highlight(text, conductMarkdown, "markdown");
  if (P.languages.markdown) return P.highlight(text, P.languages.markdown, "markdown");
  return escapeHTML(text);
}

// highlightCodeHTML 给 marked 的 fenced code renderer 使用：按代码块声明语言交给 Prism；语言未打包或
// Prism 不可用时只做 HTML 转义，保证完整内容仍安全可见。
export function highlightCodeHTML(src, lang) {
  const text = src || "";
  const language = (lang || "").toLowerCase();
  if (!P || !P.highlight || !language || !P.languages[language]) return escapeHTML(text);
  return P.highlight(text, P.languages[language], language);
}

export function escapeHTML(s) {
  return s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" })[c]);
}
