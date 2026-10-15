import { basePages } from "./base-pages.js";
import { contentPages } from "./content-pages.js";
import { collaborationPages } from "./collaboration-pages.js";
import { agentPages } from "./agent-pages.js";
import {
  settingsPages,
  settingsShell,
  bindSettingsShell,
} from "./settings-pages.js";
import {
  esc,
  icon,
  showDialog,
  dialogHeader,
  dialogFooter,
  closeDialog,
  enhanceSelects,
  closeMenu,
} from "./core.js";
const pages = {
  ...basePages,
  ...contentPages,
  ...collaborationPages,
  ...settingsPages,
  ...agentPages,
};
const labels = {
  dashboard: "系统首页",
  projects: "项目列表",
  project: "项目概览",
  tasks: "任务工作台",
  providers: "系统 Provider",
  settings: "项目设置",
  agents: "Agent",
  knowledge: "知识库",
  artifacts: "产物",
  meetings: "会议",
  inbox: "项目待办",
  mcp: "MCP 连接",
  oauth: "OAuth 回调",
};
let activeHistoryIndex = Number.isFinite(history.state?.f0Index)
  ? history.state.f0Index
  : 0;
let activeContext = null;
let restoringTraversal = null;
let approvedTraversal = null;
const entryScroll = new Map();
history.replaceState({ ...history.state, f0Index: activeHistoryIndex }, "");
history.scrollRestoration = "manual";

function locationFor(page, extra = {}) {
  const p = new URLSearchParams(location.search);
  p.set("page", page);
  ["detail", "tab", "format", "section", "panel", "agent", "view"].forEach(
    (k) => p.delete(k),
  );
  for (const [k, v] of Object.entries(extra)) p.set(k, v);
  return `?${p}`;
}
const link = (page, label, current) =>
  `<a href="${locationFor(page)}" data-page="${page}" ${current ? 'aria-current="page"' : ""}>${label}</a>`;
function getContext(search = location.search) {
  const p = new URLSearchParams(search);
  return {
    page: p.get("page") || "projects",
    theme: p.get("theme") || "light",
    scenario: p.get("scenario") || "normal",
    long: p.get("scenario") === "long" || p.get("long") === "1",
    accent: p.get("accent") || "neutral",
    fixture: p.get("fixture") || "design",
    params: p,
    navigate,
    render,
    setScenario(scenario) {
      const p = new URLSearchParams(location.search);
      p.set("scenario", scenario);
      history.replaceState(history.state, "", `?${p}`);
      render();
    },
  };
}
function rememberView(ctx) {
  entryScroll.set(activeHistoryIndex, { left: scrollX, top: scrollY });
  pages[ctx.page]?.captureView?.(ctx);
}
function navigate(page, extra = {}, skipGuard = false) {
  const current = activeContext || getContext();
  const url = locationFor(page, extra);
  const target = getContext(new URL(url, location.href).search);
  if (
    !skipGuard &&
    pages[current.page]?.beforeLeave?.(
      current,
      () => navigate(page, extra, true),
      target,
    )
  )
    return;
  rememberView(current);
  activeHistoryIndex += 1;
  history.pushState({ f0Index: activeHistoryIndex }, "", url);
  render({ reason: "navigate", previous: current });
}
function canonicalizeLegacyMemory() {
  const params = new URLSearchParams(location.search);
  if (params.get("page") !== "memory") return;
  params.set("page", "agents");
  params.set("agent", "agent-architecture");
  if (params.get("panel") !== "configuration") params.set("view", "memory");
  params.delete("panel");
  history.replaceState(history.state, "", `?${params}`);
}
function render(options = {}) {
  canonicalizeLegacyMemory();
  history.replaceState({ ...history.state, f0Index: activeHistoryIndex }, "");
  closeMenu(false);
  const ctx = getContext(),
    page = pages[ctx.page] || pages.projects;
  document.documentElement.dataset.theme = ctx.theme;
  document.documentElement.dataset.accent = ctx.accent;
  document.title = `${page.title} · agenteam F0`;
  document.body.dataset.page = ctx.page;
  document.body.dataset.scenario = ctx.scenario;
  document.getElementById("app").innerHTML =
    `<header class="system-header"><a class="brand" data-page="dashboard" href="${locationFor("dashboard")}"><span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i><i></i></span>agenteam</a><nav class="system-nav" aria-label="系统导航">${link("dashboard", "首页", ctx.page === "dashboard")}${link("projects", "项目", ctx.page === "projects" || page.project)}</nav><div class="system-end">${link("providers", "系统设置", ctx.page === "providers")}<button class="icon-button mobile-only" id="system-menu" aria-label="打开系统菜单">${icon("menu")}</button><span class="avatar" aria-label="当前账号 林 / Workspace">林</span></div></header>${
      page.project
        ? `<header class="project-header"><div class="project-identity"><a href="${locationFor("projects")}" data-page="projects" aria-label="返回项目列表">${icon("back")}</a><span class="muted">owner</span><span class="muted">/</span><a href="${locationFor("project")}" data-page="project">${esc(ctx.params.get("project_name") || "Atlas")}</a></div><nav class="project-tabs" aria-label="项目菜单">${[
            ["tasks", "任务"],
            ["meetings", "会议"],
            ["knowledge", "知识库"],
            ["artifacts", "产物"],
            ["inbox", "待办"],
            ["settings", "项目设置"],
          ]
            .map(([id, label]) =>
              link(
                id,
                label,
                id === "settings"
                  ? ["settings", "mcp", "agents"].includes(ctx.page)
                  : ctx.page === id,
              ),
            )
            .join("")}</nav></header>`
        : ""
    }<main id="main" tabindex="-1">${["settings", "mcp", "agents"].includes(ctx.page) ? settingsShell(ctx, page.render(ctx)) : page.render(ctx)}</main><aside class="lab-bar" aria-label="设计样板控制"><span class="lab-note"><span class="lab-dot"></span>F0 交互样板 · 全部为合成数据</span><button type="button" id="lab-open">${icon("settings", 13)} 样板设置</button></aside>`;
  page.bind?.(ctx);
  if (["settings", "mcp", "agents"].includes(ctx.page)) bindSettingsShell(ctx);
  enhanceSelects();
  document.getElementById("system-menu")?.addEventListener("click", () =>
    showDialog(
      `${dialogHeader("系统菜单")}<nav class="dialog-body lab-pages" aria-label="窄屏系统入口">${Object.entries(
        { dashboard: "首页", projects: "项目", providers: "系统设置" },
      )
        .map(
          ([id, label]) =>
            `<a class="button" data-system-page="${id}" href="${locationFor(id)}">${label}</a>`,
        )
        .join("")}</nav>`,
      (d) =>
        d.querySelectorAll("[data-system-page]").forEach(
          (a) =>
            (a.onclick = (e) => {
              e.preventDefault();
              closeDialog();
              navigate(a.dataset.systemPage);
            }),
        ),
    ),
  );
  document
    .getElementById("lab-open")
    .addEventListener("click", () => openLab(ctx));
  activeContext = ctx;
  if (["navigate", "popstate"].includes(options.reason)) {
    document.getElementById("main").focus({ preventScroll: true });
    const position =
      options.reason === "popstate"
        ? entryScroll.get(activeHistoryIndex)
        : null;
    window.scrollTo({
      ...(position || { left: 0, top: 0 }),
      behavior: "instant",
    });
  }
  page.restoreView?.(ctx, {
    reason: options.reason || "render",
    previous: options.previous,
  });
}
function openLab(ctx) {
  showDialog(
    `${dialogHeader("设计样板设置")}<div class="dialog-body"><p class="muted small" style="margin-bottom:18px">隔离演示，不连接服务。切换同一页面的主题、强调色与状态，用于 F0 评审。</p><nav class="lab-pages" aria-label="样板页面">${Object.entries(
      labels,
    )
      .map(
        ([id, label]) =>
          `<a class="button ${ctx.page === id ? "primary" : ""}" data-lab-page="${id}" href="${locationFor(id)}">${label}</a>`,
      )
      .join(
        "",
      )}</nav><form id="lab-form" class="lab-grid"><div class="field"><label for="lab-theme">主题</label><select id="lab-theme" name="theme">${[
      ["light", "浅色"],
      ["dark", "深色"],
    ]
      .map(
        ([v, l]) =>
          `<option value="${v}" ${ctx.theme === v ? "selected" : ""}>${l}</option>`,
      )
      .join(
        "",
      )}</select></div><div class="field"><label for="lab-accent">强调色比较</label><select id="lab-accent" name="accent">${[
      ["neutral", "中性色基线"],
      ["indigo", "鸢尾蓝 · 比较"],
      ["teal", "低饱和青蓝"],
    ]
      .map(
        ([v, l]) =>
          `<option value="${v}" ${ctx.accent === v ? "selected" : ""}>${l}</option>`,
      )
      .join(
        "",
      )}</select></div><div class="field full"><label for="lab-scenario">页面状态</label><select id="lab-scenario" name="scenario">${[
      ["normal", "正常"],
      ["long", "长名称与内容"],
      ["empty", "空集合"],
      ["loading", "加载中"],
      ["error", "读取失败"],
      ["readonly", "只读"],
      ["unknown", "结果未知 / 恢复"],
      ["conflict", "基本信息 · 版本冲突"],
    ]
      .map(
        ([v, l]) =>
          `<option value="${v}" ${ctx.scenario === v ? "selected" : ""}>${l}</option>`,
      )
      .join(
        "",
      )}</select></div></form><p class="muted small">本轮精修基本信息与 Agent 列表、详情及记忆。Agent 示例为依据真实字段构造的三个合成对象，地址参数用于演示定位与返回；其余设置栏目保留位置，MCP 沿用已有对象布局，其余目录文字不是演示链接。全部保存只在本页内模拟。<br><br>L1–L8 设计依据、已有命令映射与未实现边界见 <a href="./README.md">样板说明</a>。</p></div>${dialogFooter('<button class="button" data-dialog-close>取消</button><button class="button primary" type="submit" form="lab-form">应用并查看</button>')}`,
    (d) => {
      d.querySelector("#lab-form").onsubmit = (e) => {
        e.preventDefault();
        const p = new URLSearchParams(location.search);
        for (const [k, v] of new FormData(e.target)) p.set(k, v);
        closeDialog();
        history.replaceState(history.state, "", `?${p}`);
        render();
        document.getElementById("lab-open").focus();
      };
      d.querySelectorAll("[data-lab-page]").forEach(
        (a) =>
          (a.onclick = (e) => {
            e.preventDefault();
            closeDialog();
            navigate(a.dataset.labPage);
          }),
      );
    },
  );
}
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[data-page]");
  if (a && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
    e.preventDefault();
    navigate(
      a.dataset.page,
      a.dataset.projectName
        ? {
            project_name: a.dataset.projectName,
            project_lifecycle: a.dataset.projectLifecycle,
          }
        : {},
    );
  }
  if (e.target.closest("[data-action=retry]"))
    getContext().setScenario("normal");
});
window.addEventListener("popstate", (event) => {
  const targetIndex = Number.isFinite(event.state?.f0Index)
    ? event.state.f0Index
    : activeHistoryIndex - 1;
  if (restoringTraversal) {
    const request = restoringTraversal;
    restoringTraversal = null;
    request.restored = true;
    if (request.accepted) {
      approvedTraversal = request;
      history.go(request.delta);
    }
    return;
  }
  const current = activeContext || getContext();
  const target = getContext();
  if (approvedTraversal) {
    approvedTraversal = null;
  } else {
    const request = {
      delta: targetIndex - activeHistoryIndex,
      restored: false,
      accepted: false,
    };
    const proceed = () => {
      request.accepted = true;
      if (request.restored) {
        approvedTraversal = request;
        history.go(request.delta);
      }
    };
    if (
      request.delta &&
      pages[current.page]?.beforeLeave?.(current, proceed, target)
    ) {
      restoringTraversal = request;
      history.go(-request.delta);
      return;
    }
  }
  rememberView(current);
  activeHistoryIndex = targetIndex;
  render({ reason: "popstate", previous: current });
});
window.addEventListener("beforeunload", (event) => {
  const ctx = activeContext || getContext();
  if (pages[ctx.page]?.beforeUnload?.(ctx)) {
    event.preventDefault();
    event.returnValue = "";
  }
});
render();
