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
  agents:
    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M16 3a4 4 0 0 1 0 8M22 21v-2a4 4 0 0 0-3-3.87M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0",
  person: "M20 21v-2a7 7 0 0 0-14 0v2M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0",
  variable: "M8 4H6v16h2M16 4h2v16h-2M10 9l4 6M14 9l-4 6",
  lock: "M5 10h14v11H5zM8 10V6a4 4 0 0 1 8 0v4M12 14v3",
  shield: "M12 3 3 7v5c0 5 9 9 9 9s9-4 9-9V7zM8 12l3 3 5-6",
  layers: "m12 3 10 5-10 5L2 8zm-10 9 10 5 10-5M2 16l10 5 10-5",
  chart: "M4 20h16M7 16V9M12 16V4M17 16v-5",
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
  closeMenu(false);
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
  enhanceSelects(d);
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

// The native select remains the form's value owner; the visible control has one
// shared listbox implementation in both pages and modal dialogs.
let activeMenu = null;
export function closeMenu(restore = true) {
  if (!activeMenu) return;
  const { panel, trigger, cleanup } = activeMenu;
  cleanup();
  panel.remove();
  trigger.setAttribute("aria-expanded", "false");
  trigger.removeAttribute("aria-controls");
  activeMenu = null;
  if (restore && trigger.isConnected) trigger.focus({ preventScroll: true });
}
export function showMenu(trigger, items, choose, options = {}) {
  if (activeMenu?.trigger === trigger) return closeMenu();
  closeMenu(false);
  const panel = document.createElement("div");
  panel.className = "floating-menu";
  panel.id = `menu-${Math.random().toString(36).slice(2)}`;
  panel.setAttribute("role", options.role || "listbox");
  panel.setAttribute(
    "aria-label",
    options.label || trigger.getAttribute("aria-label") || "选项",
  );
  if (options.label)
    panel.innerHTML = `<div class="menu-label">${esc(options.label)}</div>`;
  items.forEach((item, index) => {
    if (item.group) {
      const group = document.createElement("div");
      group.className = "menu-label menu-group";
      group.textContent = item.group;
      panel.append(group);
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "menu-option";
    button.tabIndex = -1;
    button.setAttribute(
      "role",
      options.role === "menu" ? "menuitem" : "option",
    );
    if (options.role !== "menu")
      button.setAttribute("aria-selected", String(!!item.selected));
    if (item.selected) button.dataset.selected = "true";
    button.disabled = !!item.disabled;
    button.innerHTML = `${item.icon ? icon(item.icon, 14) : ""}<span>${esc(item.label)}</span><span class="menu-check">${item.selected ? icon("check", 14) : ""}</span>`;
    button.onclick = () => {
      closeMenu();
      choose(item.value, index);
    };
    panel.append(button);
  });
  (trigger.closest("dialog[open]") || document.body).append(panel);
  const rect = trigger.getBoundingClientRect();
  const width = Math.min(
    Math.max(rect.width, options.width || 196),
    innerWidth - 24,
  );
  panel.style.width = `${width}px`;
  panel.style.left = `${Math.min(Math.max(12, options.align === "end" ? rect.right - width : rect.left), innerWidth - width - 12)}px`;
  panel.style.top = `${Math.min(rect.bottom + 6, innerHeight - panel.offsetHeight - 12)}px`;
  trigger.setAttribute("aria-expanded", "true");
  trigger.setAttribute("aria-controls", panel.id);
  const buttons = [...panel.querySelectorAll("button:not(:disabled)")];
  const current =
    buttons.find((b) => b.dataset.selected === "true") || buttons[0];
  current?.focus();
  let letters = "",
    letterTimer;
  const keydown = (e) => {
    const index = buttons.indexOf(document.activeElement);
    if (["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) {
      e.preventDefault();
      buttons[
        e.key === "Home"
          ? 0
          : e.key === "End"
            ? buttons.length - 1
            : (index + (e.key === "ArrowDown" ? 1 : -1) + buttons.length) %
              buttons.length
      ]?.focus();
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeMenu();
    } else if (e.key === "Tab") {
      closeMenu();
    } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
      letters += e.key.toLocaleLowerCase();
      clearTimeout(letterTimer);
      letterTimer = setTimeout(() => {
        letters = "";
      }, 500);
      buttons
        .find((b) =>
          b.textContent.trim().toLocaleLowerCase().startsWith(letters),
        )
        ?.focus();
    }
  };
  const pointer = (e) => {
    if (!panel.contains(e.target) && !trigger.contains(e.target))
      closeMenu(false);
  };
  const reposition = () => closeMenu(false);
  const scrolled = (e) => {
    if (panel.contains(e.target)) return;
    // Bringing the trigger into view may queue a scroll event before opening.
    // Close only when the anchor has moved since this menu was positioned.
    const currentRect = trigger.getBoundingClientRect();
    if (currentRect.top !== rect.top || currentRect.left !== rect.left)
      closeMenu(false);
  };
  document.addEventListener("keydown", keydown, true);
  document.addEventListener("pointerdown", pointer);
  window.addEventListener("resize", reposition);
  window.addEventListener("scroll", scrolled, true);
  activeMenu = {
    panel,
    trigger,
    cleanup() {
      clearTimeout(letterTimer);
      document.removeEventListener("keydown", keydown, true);
      document.removeEventListener("pointerdown", pointer);
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", scrolled, true);
    },
  };
}
export function enhanceSelects(root = document) {
  root.querySelectorAll("select:not([data-enhanced])").forEach((select) => {
    select.dataset.enhanced = "true";
    select.classList.add("native-value-owner");
    select.tabIndex = -1;
    select.setAttribute("aria-hidden", "true");
    const trigger = document.createElement("button");
    trigger.type = "button";
    trigger.className = "select-trigger";
    trigger.dataset.selectFor = select.id;
    const label =
      document.querySelector(`label[for="${select.id}"]`)?.textContent.trim() ||
      select.getAttribute("aria-label") ||
      "选择选项";
    trigger.setAttribute("aria-label", label);
    trigger.setAttribute("aria-haspopup", "listbox");
    trigger.setAttribute("aria-expanded", "false");
    function sync() {
      trigger.innerHTML = `<span>${esc(select.selectedOptions[0]?.textContent || "请选择")}</span>${icon("chevron", 12)}`;
      trigger.disabled = select.disabled;
    }
    sync();
    select.after(trigger);
    select.addEventListener("change", sync);
    trigger.onclick = () =>
      showMenu(
        trigger,
        [...select.options].map((o) => ({
          label: o.textContent,
          value: o.value,
          selected: o.selected,
          disabled: o.disabled,
        })),
        (value) => {
          select.value = value;
          select.dispatchEvent(new Event("change", { bubbles: true }));
          sync();
        },
        { label },
      );
    trigger.onkeydown = (e) => {
      if (["ArrowDown", "ArrowUp"].includes(e.key)) {
        e.preventDefault();
        trigger.click();
      }
    };
    select.labels &&
      [...select.labels].forEach((label) =>
        label.addEventListener("click", (e) => {
          e.preventDefault();
          trigger.focus();
        }),
      );
  });
}
