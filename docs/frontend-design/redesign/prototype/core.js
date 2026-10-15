export const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const paths = {
  plus: "M12 5v14M5 12h14",
  arrow: "M5 12h14m-5-5 5 5-5 5",
  back: "m14 6-6 6 6 6",
  chevron: "m7 10 5 5 5-5",
  close: "m6 6 12 12M18 6 6 18",
  search: "m21 21-5-5M19 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
  grid: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
  file: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6M8 13h8M8 17h6",
  check: "m5 12 4 4L19 6",
  menu: "M4 6h16M4 12h16M4 18h16",
  settings: "M4 7h16M4 17h16M8 4v6M16 14v6",
  clock: "M12 8v4l3 2M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
  refresh: "M20 7a9 9 0 1 0 1 9M20 3v5h-5",
  message: "M21 11a8 8 0 0 1-8 8H5l-3 3V11a9 9 0 0 1 19 0",
  link: "m10 13 4-4M9 16l-2 2a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0M13 8l2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  sun: "M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0",
};
export const icon = (name, size = 16) =>
  `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${(
    paths[name] || paths.file
  )
    .split("|")
    .map((d) => `<path d="${d}"/>`)
    .join("")}</svg>`;
export const status = (label, kind = "neutral") =>
  `<span class="status ${kind}"><span class="status-dot" aria-hidden="true"></span>${esc(label)}</span>`;
export const banner = (title, body, kind = "info", action = "") =>
  `<div class="notice ${kind}" role="${kind === "error" ? "alert" : "status"}"><div><strong>${esc(title)}</strong><p>${esc(body)}</p></div>${action}</div>`;
export const action = (label, id, primary = false, disabled = false) =>
  `<button type="button" class="button${primary ? " primary" : ""}" data-action="${id}" ${disabled ? "disabled" : ""}>${esc(label)}</button>`;
export function stateGate(ctx, noun = "内容") {
  if (ctx.scenario === "loading")
    return `<section class="state-panel" aria-busy="true" aria-label="正在加载${noun}"><div class="skeleton wide"></div><div class="skeleton"></div><div class="skeleton wide"></div><p>正在加载${esc(noun)}…</p></section>`;
  if (ctx.scenario === "error")
    return `<section class="state-panel">${icon("refresh", 28)}<h2>暂时无法读取${esc(noun)}</h2><p>连接中断，已保留当前页面。重新读取后继续。</p>${action("重新加载", "retry", true)}</section>`;
  return "";
}
export const readonlyNotice = (ctx) =>
  ctx.scenario === "readonly"
    ? banner(
        "当前为只读状态",
        "项目已归档。你仍可阅读现有内容，修改操作暂不可用。",
        "warning",
      )
    : "";
let dialogTrigger = null;
export function showDialog(markup, onOpen, options = {}) {
  const d = document.getElementById("dialog");
  dialogTrigger = document.activeElement;
  d.className = options.wide ? "wide-dialog" : "";
  d.innerHTML = markup;
  if (!d.open) d.showModal();
  d.oncancel = (e) => {
    e.preventDefault();
    if (options.beforeClose && !options.beforeClose()) {
      e.preventDefault();
      return;
    }
    closeDialog();
  };
  d.onclick = (e) => {
    const el = e.target.closest("[data-dialog-close]");
    if (el && (!options.beforeClose || options.beforeClose())) closeDialog();
  };
  d.onkeydown = (event) => {
    if (event.key !== "Tab") return;
    const focusable = [
      ...d.querySelectorAll(
        "a[href], button, input, select, textarea, summary, [tabindex]",
      ),
    ].filter(
      (el) =>
        !el.disabled && el.tabIndex >= 0 && el.getClientRects().length > 0,
    );
    if (!focusable.length) {
      event.preventDefault();
      d.focus();
      return;
    }
    const first = focusable[0],
      last = focusable[focusable.length - 1];
    if (
      event.shiftKey &&
      (document.activeElement === first || !d.contains(document.activeElement))
    ) {
      event.preventDefault();
      last.focus();
    } else if (
      !event.shiftKey &&
      (document.activeElement === last || !d.contains(document.activeElement))
    ) {
      event.preventDefault();
      first.focus();
    }
  };
  onOpen?.(d);
  return d;
}
export function closeDialog() {
  const d = document.getElementById("dialog");
  d.close();
  dialogTrigger?.focus();
}
export const dialogHeader = (title) =>
  `<header class="dialog-header"><h2 id="dialog-title">${esc(title)}</h2><button class="icon-button" type="button" aria-label="关闭" data-dialog-close>${icon("close")}</button></header>`;
export const dialogFooter = (body) =>
  `<footer class="dialog-footer">${body}</footer>`;
export function inlineResult(el, text) {
  let n = el.parentElement.querySelector(".inline-result");
  if (!n) {
    n = document.createElement("p");
    n.className = "inline-result";
    n.setAttribute("role", "status");
    el.parentElement.append(n);
  }
  n.textContent = text;
}
