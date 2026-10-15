import { esc, icon, status, showMenu, showDialog, closeDialog, dialogHeader, dialogFooter } from "./core.js";

// A local presentation of ProjectGeneralSettings. All facts are synthetic; no API
// is called. The baseline values match interface-layout.fixtures.ts exactly.
const entries = [
  { id: "general", label: "基本信息", icon: "settings", page: "settings" },
  { id: "agents", label: "Agent", icon: "agents", page: "memory" },
  { id: "variables", label: "普通变量", icon: "variable" },
  { id: "secrets", label: "Secrets", icon: "lock" },
  { id: "skills", label: "Skills", icon: "file" },
  { id: "mcp", label: "MCP", icon: "link", page: "mcp" },
  { id: "scheduler", label: "Scheduler", icon: "clock" },
  { id: "audit", label: "项目审计", icon: "shield", group: "安全记录" },
  { id: "providers", label: "Providers", icon: "layers", group: "模型与 Provider" },
  { id: "models", label: "可用模型", icon: "grid" },
  { id: "usage", label: "调用记录", icon: "chart" },
];
const state = new Map();
const baselineDescription = "一个用于核对真实页面结构、长内容与操作布局的测试项目。";
function local(ctx) {
  const key = `${ctx.fixture}:${ctx.params.get("project") || "atlas"}:${ctx.scenario}`;
  if (!state.has(key)) {
    const name = ctx.params.get("project_name") || "Atlas";
    const description = ctx.scenario === "empty" ? "" : ctx.fixture === "baseline" ? baselineDescription : ctx.params.get("project_description") || "构建让 Agent 团队共同工作的项目空间。\n任务、项目资料与讨论在同一处保持上下文。";
    const current = { name, description, version: "1" };
    state.set(key, {
      key, current, baseline: { ...current }, draft: { name, description },
      phase: ctx.scenario === "unknown" ? "unknown" : ctx.scenario === "conflict" ? "conflict" : "idle",
      requiresRead: ctx.scenario === "conflict", conflictRead: false, observation: "",
      fields: {}, notice: "", recordsOpen: false, saving: false, leaving: false,
    });
    if (["unknown", "conflict"].includes(ctx.scenario)) state.get(key).draft.description += "\n补充研究与交付的范围。";
  }
  return state.get(key);
}
const changed = s => s.draft.name !== s.baseline.name || s.draft.description !== s.baseline.description;
const locked = (ctx, s) => ctx.scenario === "readonly" || ctx.params.get("project_lifecycle") === "archived" || ["unknown", "conflict", "needs-read"].includes(s.phase) || s.saving;
const dirty = s => changed(s) || ["unknown", "conflict"].includes(s.phase) || s.saving;
function locationFor(ctx, page, extra = {}) {
  const params = new URLSearchParams(ctx.params);
  ["section", "panel", "detail", "tab", "format"].forEach(k => params.delete(k));
  params.set("page", page);
  for (const [k, v] of Object.entries(extra)) params.set(k, v);
  return `?${params}`;
}
function settingLink(ctx, item, selected) {
  const content = `${icon(item.icon, 15)}<span>${esc(item.label)}</span>`;
  return item.page
    ? `<a class="project-settings-link" href="${esc(locationFor(ctx, item.page, item.id === "agents" ? { panel: "configuration" } : {}))}" data-setting-page="${item.page}" ${item.id === "agents" ? 'data-setting-panel="configuration"' : ""} ${selected ? 'aria-current="page"' : ""}>${content}</a>`
    : `<span class="project-settings-link project-settings-static">${content}</span>`;
}
export function settingsShell(ctx, content) {
  const selected = ctx.page === "mcp" ? "mcp" : ctx.page === "memory" ? "agents" : "general";
  const label = entries.find(e => e.id === selected).label;
  return `<div class="project-settings-shell"><aside class="project-settings-sidebar" aria-label="项目设置栏目"><h2>项目设置</h2><nav aria-label="设置栏目">${entries.map(item => `${item.group ? `<div class="project-settings-group">${esc(item.group)}</div>` : ""}${settingLink(ctx, item, selected === item.id)}`).join("")}</nav></aside><div class="project-settings-content ${ctx.page === "settings" ? "project-settings-general" : ""}"><div class="project-settings-mobilebar"><button type="button" id="${ctx.page === "settings" ? "settings-section-secondary" : "settings-section-menu"}" class="settings-section-trigger" aria-haspopup="menu" aria-expanded="false">${icon("settings",14)}<span>项目设置</span><span class="mobile-section-divider">/</span><strong>${esc(label)}</strong>${icon("chevron",12)}</button></div>${content}</div></div>`;
}
export function bindSettingsShell(ctx) {
  document.querySelectorAll("[data-setting-page]").forEach(link => {
    link.onclick = e => { if (e.ctrlKey || e.metaKey || e.shiftKey) return; e.preventDefault(); ctx.navigate(link.dataset.settingPage, link.dataset.settingPanel ? { panel: link.dataset.settingPanel } : {}); };
  });
  const trigger = document.querySelector("#settings-section-menu");
  if (trigger) trigger.onclick = () => showMenu(trigger, entries.map(item => ({ value: item.id, label: item.label, icon: item.icon, group: item.group, selected: item.id === (ctx.page === "mcp" ? "mcp" : ctx.page === "memory" ? "agents" : "general"), disabled: !item.page })), value => {
    const item = entries.find(e => e.id === value);
    ctx.navigate(item.page, item.id === "agents" ? { panel: "configuration" } : {});
  }, { role: "menu", label: "项目设置", width: 254 });
}
function recovery(ctx, s) {
  if (s.phase === "unknown") return `<section class="general-recovery" aria-labelledby="general-recovery-title"><div class="general-recovery-icon">${icon("clock",17)}</div><div><h2 id="general-recovery-title">保存结果尚未确认</h2><p>原输入与请求已保留。确认结果前，请勿重新提交。</p>${s.observation ? `<p class="general-recovery-result" role="status">${esc(s.observation === "in_progress" ? "原命令仍在处理中，请稍后明确查证。" : "此次未观察到原命令，不能据此判断它未提交或已回滚。")}</p>` : ""}<div class="general-recovery-actions"><button class="button" data-general="lookup">查证原命令</button><button class="button quiet" data-general="replay" ${s.observation !== "not_observed" ? "disabled" : ""}>按原请求重放</button><button class="button quiet" data-general="abandon">放弃本地追踪</button></div><p class="general-recovery-footnote">放弃追踪不会撤销服务器命令。</p></div></section>`;
  if (s.phase === "conflict") return `<section class="general-recovery" aria-labelledby="general-recovery-title"><div class="general-recovery-icon">${icon("refresh",17)}</div><div><h2 id="general-recovery-title">项目信息已有更新</h2><p>你的草稿与编辑版本仍保留。先重读当前值，再决定是否替换草稿。</p>${s.conflictRead ? `<dl class="general-comparison"><div><dt>当前名称</dt><dd>${esc(s.current.name)}</dd></div><div><dt>当前描述</dt><dd>${esc(s.current.description)}</dd></div><div><dt>当前版本</dt><dd>${esc(s.current.version)}</dd></div></dl><button class="button" data-general="adopt">使用当前值重新编辑</button>` : '<button class="button" data-general="read-current">重新读取当前值</button>'}</div></section>`;
  if (s.phase === "needs-read") return `<section class="general-recovery" aria-labelledby="general-recovery-title"><div class="general-recovery-icon">${icon("refresh",17)}</div><div><h2 id="general-recovery-title">需要重新读取当前项目</h2><p>本地追踪已放弃；原命令仍可能生效。读取当前信息后再继续编辑。</p><button class="button" data-general="read-current">重新读取当前值</button></div></section>`;
  return "";
}
function records(ctx, s) {
  return `<details class="general-records" ${s.recordsOpen ? "open" : ""}><summary><span>${icon("chevron",13)}项目信息与标识</span><span class="general-records-summary">Owner、版本与时间</span></summary><dl><div><dt>Owner</dt><dd><span class="general-owner">${icon("person",14)}owner</span><code>01990000-0000-7000-8000-000000000001</code></dd></div><div><dt>Project ID</dt><dd><code>01990000-0000-7000-8000-00000000000a</code></dd></div><div><dt>当前版本</dt><dd>${esc(s.current.version)}</dd></div><div><dt>创建时间</dt><dd><time datetime="2026-10-14T00:00:00.000001Z">2026年10月14日 00:00:00 UTC</time></dd></div><div><dt>更新时间</dt><dd><time datetime="2026-10-14T00:00:00.000001Z">2026年10月14日 00:00:00 UTC</time></dd></div>${ctx.scenario === "readonly" ? '<div><dt>归档时间</dt><dd><time datetime="2026-10-15T08:00:00Z">2026年10月15日 08:00:00 UTC</time></dd></div>' : ""}</dl></details>`;
}
function renderGeneral(ctx) {
  const s = local(ctx), readonly = locked(ctx, s);
  const header = `<header class="general-heading"><div><h1 id="general-title" tabindex="-1"><span class="general-desktop-title">基本信息</span><button type="button" id="settings-section-menu" class="general-title-menu" aria-label="基本信息，切换项目设置栏目" aria-haspopup="menu" aria-expanded="false">基本信息${icon("chevron",15)}</button></h1><p>管理项目名称与描述。</p></div><button class="button quiet general-more" id="general-more" aria-label="更多项目信息操作" aria-haspopup="menu" aria-expanded="false">${icon("more",16)}<span>更多</span></button></header>`;
  if (ctx.scenario === "loading") return `<section class="general-page">${header}<div class="general-loading" aria-busy="true" aria-label="正在读取项目信息"><div></div><div></div><div></div><p>正在读取项目信息…</p></div></section>`;
  if (ctx.scenario === "error") return `<section class="general-page">${header}<section class="general-read-error"><span>${icon("refresh",24)}</span><h2>暂时无法读取项目信息</h2><p>连接中断。重新读取后继续，尚未发起修改。</p><button class="button" data-general="retry">重新读取当前值</button></section></section>`;
  const nameHint = "1–64 位英文字母、数字、点、下划线或连字符；不能为 . 或 ..，名称不区分大小写。";
  return `<section class="general-page" aria-labelledby="general-title">${header}${ctx.scenario === "readonly" ? '<div class="general-readonly" role="status">' + icon("lock",14) + '<span>项目已归档，当前信息仅可查看。</span></div>' : ""}${recovery(ctx,s)}${s.phase !== "needs-read" ? `<form id="general-form" aria-label="项目基本信息" novalidate><div class="general-fields"><div class="general-field-row"><div class="general-field-label"><label for="general-name">项目名称</label></div><div class="general-field-control"><input id="general-name" name="name" value="${esc(s.draft.name)}" autocomplete="off" autocapitalize="none" spellcheck="false" maxlength="64" required aria-describedby="general-name-rule general-name-warning${s.fields.name ? " general-name-error" : ""}" ${s.fields.name ? 'aria-invalid="true"' : ""} ${readonly ? "readonly" : ""}><p id="general-name-rule" class="general-hint">${nameHint}</p><p id="general-name-warning" class="general-name-warning">更改名称后，旧链接将失效。</p>${s.fields.name ? `<p class="general-field-error" id="general-name-error" role="alert">${esc(s.fields.name)}</p>` : ""}</div></div><div class="general-field-row general-description-row"><div class="general-field-label"><label for="general-description">项目描述</label><span>可选</span></div><div class="general-field-control"><textarea id="general-description" name="description" rows="4" aria-describedby="general-description-hint${s.fields.description ? " general-description-error" : ""}" ${s.fields.description ? 'aria-invalid="true"' : ""} ${readonly ? "readonly" : ""} placeholder="简要说明这个项目的目标…">${esc(s.draft.description)}</textarea><p id="general-description-hint" class="general-hint">最多 8192 UTF-8 字节，保留空格与换行。留空会清空描述。</p>${s.fields.description ? `<p class="general-field-error" id="general-description-error" role="alert">${esc(s.fields.description)}</p>` : ""}</div></div></div><div class="general-form-footer"><div class="general-footer-actions"><button type="submit" class="button primary general-save" ${readonly || !changed(s) ? "disabled" : ""}>${s.saving ? `${icon("refresh",13)}保存中…` : "保存修改"}</button><button type="button" class="button quiet" data-general="cancel" ${readonly || !changed(s) ? "disabled" : ""}>取消修改</button></div><span class="general-edit-version">编辑版本 ${esc(s.baseline.version)}</span></div><div class="general-feedback" aria-live="polite">${s.notice ? `${icon(s.phase === "saved" ? "check" : "file",13)}<span>${esc(s.notice)}</span>` : changed(s) ? '<span>有未保存的修改</span>' : ""}</div></form>` : ""}${records(ctx,s)}</section>`;
}
function rerender(ctx, focus) {
  ctx.render();
  if (focus) document.querySelector(focus)?.focus({ preventScroll:true });
}
function confirm(title, body, label, done) {
  showDialog(`${dialogHeader(title)}<div class="dialog-body general-confirm-body"><p>${body}</p></div>${dialogFooter(`<button class="button" type="button" data-dialog-close>继续编辑</button><button class="button primary" type="button" id="general-confirm">${label}</button>`)}`, d => {
    d.querySelector("#general-confirm").onclick = () => { closeDialog(); done(); };
  });
}
function applyCurrent(s) {
  s.baseline = {...s.current};
  s.draft = {name:s.current.name,description:s.current.description};
  s.phase = "idle";s.fields = {};s.requiresRead = false;s.conflictRead = false;s.notice = "";
}
function readCurrent(ctx,s) {
  if (s.phase === "conflict") {
    s.current = {...s.current, description: `${s.baseline.description}\n当前已保存的信息包含最新协作范围。`,version:"2"};
    s.conflictRead = true;s.requiresRead = false;
    rerender(ctx,"[data-general=adopt]");
  } else {
    if (s.phase === "needs-read") applyCurrent(s);
    s.notice = "已重新读取样板中的当前值。";
    rerender(ctx,"#general-more");
  }
}
function performSave(ctx,s) {
  s.saving = true;s.notice = "";
  rerender(ctx,"#general-title");
  setTimeout(() => {
    s.saving = false;
    s.current = {...s.draft,version:String(Number(s.baseline.version)+1)};
    s.baseline = {...s.current};s.phase = "saved";s.notice = "已保存到当前样板，未调用服务端。";
    const params = new URLSearchParams(location.search);
    if (params.get("page") === "settings" && params.get("scenario") === ctx.params.get("scenario")) {
      params.set("project_name",s.current.name);
      history.replaceState({},"",`?${params}`);
      rerender(ctx,"#general-title");
    }
  },420);
}
function bindGeneral(ctx) {
  const s = local(ctx), root = document.querySelector("#main");
  const more = root.querySelector("#general-more");
  if (more) more.onclick = () => showMenu(more, [
    { label:"重新读取当前值",value:"read",icon:"refresh",disabled: s.saving || s.phase === "unknown" },
    { label:"查看项目信息与标识",value:"records",icon:"file",disabled:["loading","error"].includes(ctx.scenario) },
  ], value => {
    if (value === "read") { if (["error","loading"].includes(ctx.scenario)) ctx.setScenario("normal"); else readCurrent(ctx,s); }
    else { const details = root.querySelector(".general-records");details.open=true;s.recordsOpen=true;details.querySelector("summary").focus();details.scrollIntoView({block:"nearest",behavior:"instant"}); }
  },{role:"menu",label:"项目信息",width:224,align:"end"});
  root.querySelector(".general-records")?.addEventListener("toggle",e=>{s.recordsOpen=e.target.open;});
  const form = root.querySelector("#general-form");
  if (form) {
    form.oninput = e => {
      s.draft[e.target.name] = e.target.value;s.notice="";
      form.querySelector("button[type=submit]").disabled = locked(ctx,s) || !changed(s);
      form.querySelector("[data-general=cancel]").disabled = locked(ctx,s) || !changed(s);
      form.querySelector(".general-feedback").textContent = changed(s) ? "有未保存的修改" : "";
    };
    form.onsubmit = e => {
      e.preventDefault();if(locked(ctx,s)||!changed(s)) return;
      s.fields={};
      if (!/^[A-Za-z0-9._-]{1,64}$/.test(s.draft.name) || [".",".."].includes(s.draft.name)) s.fields.name="请输入 1–64 位合法项目名称，不能为 . 或 ..。";
      if (new TextEncoder().encode(s.draft.description).length>8192 || /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(s.draft.description)) s.fields.description="描述最多 8192 UTF-8 字节，不能包含换行与制表符之外的控制字符。";
      if(Object.keys(s.fields).length) return rerender(ctx,s.fields.name?"#general-name":"#general-description");
      if(s.draft.name!==s.baseline.name) confirm("确认更改项目名称？",`<span class="general-rename-comparison"><span>${esc(s.baseline.name)}</span>${icon("arrow",14)}<strong>${esc(s.draft.name)}</strong></span>旧名称链接将失效，不提供别名或自动跳转。保存后将使用更新的项目名称。`,"确认保存",()=>performSave(ctx,s));
      else performSave(ctx,s);
    };
  }
  root.querySelectorAll("[data-general]").forEach(button=>button.onclick=()=>{
    const command=button.dataset.general;
    if(command==="cancel"){applyCurrent(s);rerender(ctx,"#general-name");}
    if(command==="retry")ctx.setScenario("normal");
    if(command==="read-current")readCurrent(ctx,s);
    if(command==="adopt")confirm("采用当前项目值？","当前未保存的输入将被替换为刚才完整读取的值。不会重发或合并原命令。","采用当前值",()=>{applyCurrent(s);rerender(ctx,"#general-name");});
    if(command==="lookup"){s.observation=s.observation==="in_progress"?"not_observed":"in_progress";rerender(ctx,"[data-general=lookup]");}
    if(command==="replay" && s.observation==="not_observed"){s.observation="in_progress";rerender(ctx,"[data-general=lookup]");}
    if(command==="abandon")confirm("放弃本地追踪？","仅丢弃本地草稿和原命令追踪；此前请求仍可能生效，不会撤销服务器命令。","放弃本地追踪",()=>{s.phase="needs-read";s.draft={name:"",description:""};s.notice="";rerender(ctx,"[data-general=read-current]");});
  });
}
function beforeLeave(ctx, proceed) {
  const s=local(ctx);
  if(!dirty(s)) return false;
  if(s.leaving || document.querySelector("#dialog")?.open) return true;
  s.leaving=true;
  const dialog=showDialog(`${dialogHeader("放弃项目修改？")}<div class="dialog-body general-confirm-body"><p>将放弃本地草稿和原命令追踪。此前请求仍可能生效，离开不会撤销服务器命令。</p></div>${dialogFooter('<button class="button" type="button" data-dialog-close>继续编辑</button><button class="button primary" type="button" id="general-leave">放弃并离开</button>')}`,d=>{d.querySelector("#general-leave").onclick=()=>{applyCurrent(s);closeDialog();proceed();};});
  dialog.addEventListener("close",()=>{s.leaving=false;},{once:true});
  return true;
}
window.addEventListener("beforeunload",event=>{if(document.body.dataset.page==="settings" && [...state.values()].some(dirty)){event.preventDefault();event.returnValue="";}});
export const settingsPages={settings:{title:"基本信息",project:true,render:renderGeneral,bind:bindGeneral,beforeLeave}};
