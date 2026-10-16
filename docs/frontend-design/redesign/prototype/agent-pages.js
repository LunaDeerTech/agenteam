import {
  esc,
  icon,
  showMenu,
  showDialog,
  closeDialog,
  dialogHeader,
  dialogFooter,
} from "./core.js";

import { models, runnerChoices, secretChoices, skillChoices, toolChoices, coreTools, presets, dateText } from "./agent-catalog.js";
import { renderAgentMemory, renderAgentAudit, bindAgentMemoryAudit } from "./agent-memory-audit.js";

const basicFields = [
  "name",
  "displayName",
  "description",
  "tagColor",
  "instructions",
  "modelID",
  "reasoningEffort",
  "approvalPolicy",
  "approvalModelID",
  "injectAgentsMD",
];
const basicSnapshot = (agent) =>
  Object.fromEntries(basicFields.map((key) => [key, agent[key]]));
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const seeds = [
  [
    "architecture",
    "Architecture",
    "梳理系统边界、接口与关键设计决策。",
    "#87929b",
  ],
  [
    "researcher",
    "Researcher",
    "收集资料与核对来源，为团队提供研究依据。",
    "#8a9583",
  ],
  [
    "reviewer",
    "Reviewer",
    "审查实现与交付结果，记录风险与改进建议。",
    "#a09481",
  ],
];
const stores = new Map();
const copy = (value) => JSON.parse(JSON.stringify(value));
const label = (agent) => agent.displayName || agent.name;
const button = (text, action, kind = "", disabled = false) =>
  `<button type="button" class="button ${kind}" data-agent-action="${action}" ${disabled ? "disabled" : ""}>${esc(text)}</button>`;
const scopeFor = (ctx) =>
  `${ctx.params.get("owner") || "owner"}:${ctx.params.get("project") || ctx.params.get("project_id") || ctx.params.get("project_name") || "Atlas"}`;
const keyFor = (ctx) => `${scopeFor(ctx)}:${ctx.fixture}:${ctx.scenario}`;
const agentID = (ctx) => ctx.params.get("agent") || "";
const tabFor = (ctx) => {
  const view = ctx.params.get("view");
  if (view === "resources") return "resources";
  if (["memory", "audit"].includes(view) && agentID(ctx) !== "new") return view;
  return "basic";
};
const isMemory = (ctx) => tabFor(ctx) === "memory";
const readOnly = (ctx) =>
  ctx.scenario === "readonly" ||
  (ctx.params.get("project_lifecycle") || "active") !== "active";
const memoryCopy = {
  architecture: [
    "项目的接口变更先核对调用方与提供方，再记录版本与兼容边界。",
    "设计交付需同时说明已确定的边界与尚待验证的假设。",
    "接口变更需要记录影响范围。",
  ],
  researcher: [
    "研究资料保留原始来源与读取时间，引用应能回到对应文档位置。",
    "多份资料结论不一致时，分别记录证据与条件，避免提前合并为单一判断。",
    "研究资料保留原始来源。",
  ],
  reviewer: [
    "评审结论区分已验证问题、潜在风险与需要补充的证据。",
    "复验先重走失败路径，再检查变更影响范围；未验证项在交付说明中保留。",
    "评审结论记录问题与验证证据。",
  ],
};
function local(ctx) {
  const key = keyFor(ctx);
  if (!stores.has(key)) {
    const agents = seeds.map(
      ([name, displayName, description, tagColor], index) => ({
        id: `agent-${name}`,
        name,
        displayName,
        description,
        tagColor,
        instructions:
          index === 0
            ? "先确认任务目标和已有约束，再分析模块边界与调用关系。\n设计结论需说明依据、取舍与尚未确认的事项。"
            : "先阅读项目上下文与当前任务。保留资料来源，明确结论与待确认事项，并在完成后记录交付结果。",
        modelID: index === 2 ? models[1].id : models[0].id,
        reasoningEffort: index === 2 ? null : "medium",
        approvalPolicy: "default",
        approvalModelID: "",
        injectAgentsMD: "yes",
        runnerMounts:
          index === 0
            ? [{ name: "architecture", runner_id: runnerChoices[0].id, workspace: "architecture", description: "架构设计与接口文档。" }]
            : [],
        secretIDs: index === 1 ? [secretChoices[0].id] : [],
        allowedToolIDs: index === 0 ? ["read-file", "web-search"] : [],
        executions: index === 0 ? [{ id: "execution-architecture-active", status: "running", task: "AT-142 · 接口边界检查" }] : [],
        skills:
          index === 0
            ? [
                {
                  id: "assignment-review",
                  skillID: "skill-review",
                  enabled: true,
                },
              ]
            : [],
        version: "1",
        updatedAt: ["2026-10-15T08:30:00Z", "2026-10-14T15:10:00Z", "2026-10-13T12:25:00Z"][index],
        createdAt: "2026-10-12T09:00:00Z",
        memories:
          index < 3
            ? [
                {
                  id: `memory-${name}-1`,
                  type: "事实",
                  revision: "2",
                  text: memoryCopy[name][0],
                  old: memoryCopy[name][2],
                  tags: ["项目约定", "协作"],
                  source: `execution-${name}-1`,
                  date: "2026-10-15T08:20:00Z",
                },
                {
                  id: `memory-${name}-2`,
                  type: "经验",
                  revision: "1",
                  text: memoryCopy[name][1],
                  tags: ["交付"],
                  source: `execution-${name}-2`,
                  date: "2026-10-14T15:10:00Z",
                },
              ]
            : [],
      }),
    );
    if (ctx.params.get("list_fixture") === "paged") for(let i=1;i<=12;i++) agents.push({...copy(agents[i%3]),id:`agent-specialist-${i}`,name:`specialist-${i}`,displayName:`Specialist ${i}`,description:`负责项目第 ${i} 组协作任务与资料核对。`,executions:[],memories:[],createdAt:`2026-10-${String(1+i).padStart(2,"0")}T09:00:00Z`});
    if (ctx.params.get("resource_extra_secret") === "1")
      agents[2].secretIDs.push("secret-outside-directory");
    if (ctx.params.get("model_reference") === "invalid") agents[0].modelID = "model-retired-chat";
    if (ctx.long) {
      agents[0].displayName =
        "Architecture · 跨区域协作与系统架构设计负责人 / Distributed Systems Architecture";
      agents[0].description =
        "梳理跨区域项目协作的模块边界、接口契约与关键设计决策。".repeat(4);
    }
    stores.set(key, {
      agents,
      edits: {},
      deletedMemories: {},
      resources: {},
      busy: null,
      notice: "",
      listFilter: "",
      listStatus: "all",
      listPage: 0,
      outcomes: new Set(),
      listView: null,
      detailViews: {},
      returnFocus: "",
      skillsOpen: false,
      expandedHistory: "",
      pending:
        ctx.scenario === "unknown" && !["memory", "audit"].includes(tabFor(ctx))
          ? {
              kind: isMemory(ctx)
                ? "memory"
                : tabFor(ctx) === "resources" && agentID(ctx) !== "new"
                  ? "resource"
                  : agentID(ctx) === "new"
                    ? "creation"
                    : "configuration",
              agent: agentID(ctx) || "agent-architecture",
              original: null,
              observation: "",
            }
          : null,
      leaving: false,
    });
  }
  return stores.get(key);
}
function draftFor(ctx, state) {
  const id = agentID(ctx);
  if (!state.edits[id]) {
    const current = state.agents.find((a) => a.id === id);
    if (id !== "new" && !current) return null;
    state.edits[id] = {
      value: current
        ? copy(basicSnapshot(current))
        : {
            id: "new",
            name: "",
            displayName: "",
            description: "",
            tagColor: "",
            instructions: "",
            modelID: "",
            reasoningEffort: null,
            approvalPolicy: "default",
            approvalModelID: "",
            injectAgentsMD: "yes",
            runnerMounts: [],
            secretIDs: [],
            allowedToolIDs: [],
            skills: [],
            addSkills: true,
            installSkill: true,
            version: "1",
          },
      baseVersion: current?.version || "1",
      baseSnapshot: current ? copy(basicSnapshot(current)) : null,
      touched: new Set(),
      message: "",
      conflict: ctx.scenario === "conflict",
      sections: new Set(["runners"]),
      error: null,
    };
  }
  const edit = state.edits[id];
  if (
    state.pending?.agent === id &&
    !state.pending.original &&
    ["configuration", "creation"].includes(state.pending.kind)
  ) {
    if (id === "new")
      Object.assign(edit.value, {
        name: "new-agent",
        displayName: "New Agent",
        modelID: models[0].id,
        reasoningEffort: "medium",
      });
    edit.value.description += "\n补充当前 Agent 的协作范围。";
    edit.touched.add("description");
    state.pending.original = {
      agentID: id,
      version: edit.baseVersion,
      input: copy(edit.value),
    };
  }
  return edit;
}
function href(ctx, extra = {}) {
  const p = new URLSearchParams(ctx.params);
  ["agent", "view", "panel", "detail", "section"].forEach((k) => p.delete(k));
  p.set("page", "agents");
  Object.entries(extra).forEach(([k, v]) => p.set(k, v));
  return `?${p}`;
}
const notice = (title, body, actions = "", kind = "") =>
  `<section class="agent-notice ${kind}" role="status"><div><strong>${esc(title)}</strong><p>${esc(body)}</p></div>${actions}</section>`;
function heading(title, description, actions = "", back = "") {
  return `${back ? `<nav class="agent-back-navigation" aria-label="上级页面">${back}</nav>` : ""}<header class="settings-page-heading agent-heading"><div><h1 id="agent-page-title" tabindex="-1">${esc(title)}</h1>${description ? `<p>${esc(description)}</p>` : ""}</div>${actions ? `<div class="agent-heading-actions">${actions}</div>` : ""}</header>`;
}
function backLink(ctx, text, extra = {}) {
  return `<a href="${esc(href(ctx, extra))}" class="agent-back" data-agent-back="${extra.agent || ""}">${icon("back", 13)}${esc(text)}</a>`;
}
function tabs(ctx) {
  const items = [
    ["basic", "基本配置"],
    ["resources", "能力与资源"],
  ];
  if (agentID(ctx) !== "new") items.push(["memory", "记忆"], ["audit", "审计"]);
  return `<div class="agent-tabs" role="tablist" aria-label="Agent 配置">${items.map(([key, title]) => `<a id="agent-tab-${key}" role="tab" class="agent-tab" data-agent-tab="${key}" href="${esc(href(ctx, { agent: agentID(ctx), ...(key === "basic" ? {} : { view: key }) }))}" aria-controls="agent-tab-panel" aria-selected="${tabFor(ctx) === key}" tabindex="${tabFor(ctx) === key ? "0" : "-1"}">${title}</a>`).join("")}</div>`;
}
function gate(ctx, noun) {
  if (ctx.scenario === "loading")
    return `<section class="agent-state" aria-busy="true" aria-label="正在读取${noun}"><div class="skeleton wide"></div><div class="skeleton"></div><div class="skeleton wide"></div><p>正在读取${noun}…</p></section>`;
  if (ctx.scenario === "error")
    return `<section class="agent-state">${icon("refresh", 25)}<h2>暂时无法读取${noun}</h2><p>重新读取后继续。</p>${button("重新读取", "retry")}</section>`;
  return "";
}
const busyExecution = (agent) => (agent.executions || []).find((execution) => !["completed", "failed", "cancelled"].includes(execution.status));
function directory(ctx, state) {
  const all = ctx.scenario === "empty" ? [] : state.agents;
  const filtered = all.filter((agent) => `${label(agent)} ${agent.name} ${agent.description}`.toLowerCase().includes(state.listFilter.toLowerCase()) && (state.listStatus === "all" || Boolean(busyExecution(agent)) === (state.listStatus === "busy")));
  const size = 10, pages = Math.max(1, Math.ceil(filtered.length / size));
  state.listPage = Math.min(state.listPage, pages - 1);
  const agents = filtered.slice(state.listPage * size, (state.listPage + 1) * size);
  const pending = state.pending || state.domainPending;
  return `<section class="agent-page">${heading("Agent", "管理项目中的 AI 协作成员、模型与工具权限。", button("创建 Agent", "create", "primary", readOnly(ctx) || !!pending))}${readOnly(ctx) ? notice("当前项目为只读", "你可以查看 Agent 配置、记忆与审计。") : ""}${pending ? notice("有一个操作结果待确认", "回到原 Agent 查询结果后可继续操作。", button("查看原 Agent", "original")) : ""}<div class="agent-filterbar"><label class="agent-search"><span class="sr-only">筛选 Agent</span><input id="agent-list-search" type="search" placeholder="筛选名称或职责…" value="${esc(state.listFilter)}" data-list-filter></label><label class="agent-filter-select"><span class="sr-only">执行状态</span><select id="agent-list-status" data-list-status><option value="all" ${state.listStatus === "all" ? "selected" : ""}>全部执行状态</option><option value="busy" ${state.listStatus === "busy" ? "selected" : ""}>正在执行</option><option value="idle" ${state.listStatus === "idle" ? "selected" : ""}>无进行中执行</option></select></label><button type="button" class="icon-button" data-agent-action="refresh" aria-label="刷新 Agent 目录">${icon("refresh", 13)}</button></div>${gate(ctx, "Agent 目录") || (agents.length ? `<div class="agent-directory" role="list" aria-label="Agent 目录">${agents.map((agent) => {const model = models.find((m) => m.id === agent.modelID), execution = busyExecution(agent); return `<article class="agent-directory-row" role="listitem"><a class="agent-directory-link" data-agent-open="${agent.id}" href="${esc(href(ctx, { agent: agent.id }))}"><span class="agent-mark" style="--agent-color:${agent.tagColor || "#8a8e87"}">${icon("agents", 17)}</span><span class="agent-directory-copy"><span class="agent-directory-name">${esc(label(agent))}<span class="agent-technical-name">${esc(agent.name)}</span></span><span class="agent-directory-description">${esc(agent.description)}</span><span class="agent-directory-metadata"><span>${esc(model?.label || "模型引用失效")}</span><span>${execution ? `正在执行 · ${esc(execution.task)}` : "无进行中执行"}</span></span></span></a><time datetime="${agent.updatedAt}" title="${esc(dateText(agent.updatedAt,true))}">${esc(dateText(agent.updatedAt))}</time><a class="agent-row-arrow" data-agent-open="${agent.id}" href="${esc(href(ctx, { agent: agent.id }))}" aria-label="查看 ${esc(label(agent))}">${icon("arrow", 14)}</a></article>`;}).join("")}</div>` : `<section class="agent-state">${icon("agents", 26)}<h2>${all.length ? "没有符合筛选的 Agent" : "项目还没有 Agent"}</h2><p>${all.length ? "调整名称或执行状态筛选。" : "从空白配置或系统 Preset 创建。"}</p>${all.length ? button("清除筛选", "list-clear") : button("创建 Agent", "create", "primary", readOnly(ctx))}</section>`)}<div class="agent-pagination"><span>共 ${filtered.length} 个 Agent · 第 ${state.listPage + 1} / ${pages} 页</span><div>${button("上一页", "list-prev", "quiet", state.listPage === 0)}${button("下一页", "list-next", "quiet", state.listPage >= pages - 1)}</div></div><p class="agent-feedback" id="agent-list-feedback" role="status">${esc(state.notice)}</p></section>`;
}
function inputField(key, title, value, options = {}) {
  const id = `agent-${key}`;
  const hint = options.hint
    ? `<p id="${id}-hint" class="agent-field-hint">${esc(options.hint)}</p>`
    : "";
  const attrs = `id="${id}" name="${key}" data-agent-field="${key}" ${options.disabled ? "disabled" : ""} ${options.required ? "required" : ""} ${hint ? `aria-describedby="${id}-hint"` : ""}`;
  const control = options.choices
    ? `<select ${attrs}>${options.choices.map((choice) => `<option value="${esc(choice.id)}" ${String(value) === choice.id ? "selected" : ""} ${choice.disabled ? "disabled" : ""}>${esc(choice.label)}</option>`).join("")}</select>`
    : options.rows
      ? `<textarea ${attrs} rows="${options.rows}">${esc(value)}</textarea>`
      : `<input ${attrs} value="${esc(value)}" ${options.max ? `maxlength="${options.max}"` : ""} ${key === "name" ? 'autocomplete="off" autocapitalize="none" spellcheck="false"' : ""}>`;
  return `<div class="agent-field"><label for="${id}">${esc(title)}${options.required ? '<span aria-hidden="true"> *</span>' : ""}</label>${control}${hint}<p id="${id}-error" class="agent-field-error" hidden></p></div>`;
}
function disclosure(key, title, subtitle, content, edit) {
  return `<details class="agent-resource" data-agent-section="${key}" ${edit.sections.has(key) ? "open" : ""}><summary><span><strong>${title}</strong><span>${esc(subtitle)}</span></span>${icon("chevron", 13)}</summary><div class="agent-resource-content">${content}</div></details>`;
}
function configuration(ctx, state, current) {
  const edit = draftFor(ctx, state), value = edit.value, creating = agentID(ctx) === "new";
  const locked = readOnly(ctx) || !!state.pending || !!state.busy || !!state.domainPending || !!state.domainBusy || edit.conflict;
  const opt = {disabled: locked};
  const model = models.find((item) => item.id === value.modelID);
  const modelOpts = [{id:"",label:"选择聊天模型"}, ...models.map((item) => ({id:item.id,label:`${item.label} · ${item.source} / ${item.provider}${item.available ? "" : "（不可用）"}`,disabled: !item.available}))];
  if(value.modelID && !model) modelOpts.push({id:value.modelID,label:`失效引用 · ${value.modelID}`,disabled:true});
  const pending = state.pending?.agent === agentID(ctx);
  const pendingNotice = pending ? notice(state.pending.phase === "resolved" ? "原操作已有明确结果" : "操作结果待确认", "原输入与操作已保留，查询原操作并重新读取后继续。", button(state.pending.phase === "resolved" ? "重新读取配置" : "查询原操作结果", state.pending.kind === "resource" ? "original" : state.pending.phase === "resolved" ? "basic-readback" : "lookup")) : state.domainPending ? notice("有一个操作结果待确认", "请回到原操作查询结果。", button("查看原操作", "original")) : "";
  const preset = creating ? `<section class="agent-preset-start"><div><strong>${edit.preset ? `已复制 ${esc(edit.preset)}` : "从空白配置开始"}</strong><p>也可复制系统 Preset 的角色配置，再选择本项目资源。</p></div>${button("选择系统 Preset", "preset-select", "", locked)}</section>${edit.presetMessage ? notice("复制说明", edit.presetMessage) : ""}` : "";
  return `<div class="agent-detail-content" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-basic">${readOnly(ctx) ? notice("当前项目为只读", "配置可以阅读，修改暂不可用。") : ""}${pendingNotice}${edit.conflict ? notice("配置已有新版本", "草稿已保留。重新读取当前配置后，仍可核对并保存你的输入。", button("重读并保留草稿", "basic-rebase", "", !!state.pending || !!state.busy)) : ""}${gate(ctx,"Agent 配置") || `<form id="agent-configuration-form" novalidate aria-label="${creating ? "创建 Agent" : "编辑 Agent 配置"}">${preset}<section class="agent-form-section"><header><h2>基本信息</h2></header><div class="agent-field-pair">${inputField("name","Agent 名称",value.name,{...opt,required:true,max:32,hint:"3–32 位字母、数字或连字符。"})}${inputField("displayName","显示名称",value.displayName,{...opt,max:256,hint:"可选，未填写时使用 Agent 名称。"})}</div>${inputField("description","描述",value.description,{...opt,rows:2})}<details class="agent-appearance" data-agent-section="appearance" ${edit.sections.has("appearance") ? "open" : ""}><summary>标签颜色</summary>${inputField("tagColor","标签颜色",value.tagColor,{...opt,hint:"使用小写 #rrggbb，清空后恢复默认。"})}</details></section><section class="agent-form-section"><header><h2>指令</h2><p>定义 Agent 的职责、工作方式与约束。</p></header>${inputField("instructions","Agent 指令",value.instructions,{...opt,rows:5})}<label class="agent-check-row"><input type="checkbox" data-agent-inject ${value.injectAgentsMD === "yes" ? "checked" : ""} ${locked ? "disabled" : ""}><span>注入项目 AGENTS.md</span></label></section><section class="agent-form-section"><header><h2>模型与审批</h2><p>保存后用于后续执行。审批策略不会扩大工具或项目资源的授权。</p></header>${inputField("modelID","主模型",value.modelID,{...opt,required:true,choices:modelOpts})}${model ? `<p class="agent-section-note">${esc(model.source)}模型 · ${esc(model.provider)} · ${model.available ? "当前可用" : "当前不可用，请重新选择"}</p>` : value.modelID ? notice("模型引用失效","请选择当前可用的聊天模型。") : ""}${model?.efforts.length ? inputField("reasoningEffort","推理强度",value.reasoningEffort || "",{...opt,required:true,choices:[{id:"",label:"选择推理强度"},...model.efforts.map((effort)=>({id:effort,label:({low:"低",medium:"中",high:"高",xhigh:"最高"})[effort]}))],hint:"可选等级由当前模型能力决定。"}) : ""}${inputField("approvalPolicy","审批策略",value.approvalPolicy,{...opt,choices:[{id:"default",label:"默认审批"},{id:"auto",label:"模型自动审批"},{id:"allow",label:"允许工具调用"}]})}<div id="agent-approval-model" ${value.approvalPolicy !== "auto" ? "hidden" : ""}>${inputField("approvalModelID","审批模型",value.approvalModelID,{...opt,choices:modelOpts,hint:"只决定已获授权工具是否通过审批，不授予新权限。"})}</div></section>${savebar(ctx,state,edit)}</form>${!creating ? records(current) : ""}`}</div>`;
}
function savebar(ctx,state,edit) {
  const pending = !!state.pending || !!state.busy || !!state.domainPending || !!state.domainBusy;
  return `<div class="agent-savebar"><span id="agent-dirty-state">${state.busy?.kind === "configuration" || state.busy?.kind === "creation" ? "正在保存…" : edit.touched.size ? "有未保存的修改" : ""}</span><div>${button(agentID(ctx) === "new" ? "取消创建" : "取消修改","cancel","quiet",pending)}<button type="submit" class="button primary" id="agent-save" ${readOnly(ctx) || pending || edit.conflict || !edit.touched.size ? "disabled" : ""}>${agentID(ctx) === "new" ? "创建 Agent" : "保存配置"}</button></div></div><p class="agent-feedback" id="agent-form-feedback" role="status">${esc(edit.message)}</p>${edit.failed ? button("重试保存","basic-retry","",pending) : ""}`;
}
function resourceState(state, id) {
  return (state.resources[id] ||= {
    mount: null,
    index: -1,
    status: "",
    message: "",
    failed: null,
    toolSource: "all",
    toolAccess: "all",
    toolRisk: "all",
    toolQuery: "",
    secretQuery: "",
    secretLimit: 3,
  });
}
function records(current) {
  return `<details class="agent-records"><summary>Agent 标识与记录</summary><dl><dt>Agent ID</dt><dd>${esc(current.id)}</dd><dt>配置版本</dt><dd>${esc(current.version)}</dd><dt>创建时间</dt><dd>${esc(dateText(current.createdAt,true))}</dd><dt>更新时间</dt><dd>${esc(dateText(current.updatedAt,true))}</dd></dl></details>`;
}
function resources(ctx, state, current) {
  const edit = draftFor(ctx,state), creating = agentID(ctx) === "new";
  current = creating ? edit.value : current;
  const resource = resourceState(state,agentID(ctx));
  const locked = readOnly(ctx) || !!state.pending || !!state.busy || !!state.domainPending || !!state.domainBusy || edit.conflict;
  if(state.pending?.kind === "resource" && !state.pending.original) {
    state.pending.original = {project:scopeFor(ctx),agent:current.id,field:"secretIDs",payload:[...new Set([...current.secretIDs,secretChoices[0].id])],expectedVersion:current.version,key:crypto.randomUUID(),label:"Secret 白名单"};
    resource.status="unknown";resource.message="Secret 白名单结果待确认。原操作已保留。";
  }
  const mountRows = current.runnerMounts.map((mount,index) => `<div class="agent-saved-mount"><div><strong>${esc(mount.name || mount.workspace)}</strong><span>${esc(runnerChoices.find((runner)=>runner.id===mount.runner_id)?.label || mount.runner_id)} / ${esc(mount.workspace)}</span><span>${esc(mount.description || "未填写说明")}</span></div><div>${button("编辑",`edit-mount:${index}`,"quiet",locked)}${button("移除",`remove-mount:${index}`,"quiet",locked)}</div></div>`).join("");
  const mountField = (key,title,hint="") => `<div class="agent-field"><label for="agent-action-${key}">${title}</label><input id="agent-action-${key}" data-resource-field="${key}" value="${esc(resource.mount[key] || "")}" ${locked ? "disabled" : ""}>${hint ? `<p class="agent-field-hint">${hint}</p>` : ""}</div>`;
  const mountEditor = resource.mount ? `<form id="agent-resource-editor" novalidate class="agent-resource-editor"><h3>${resource.index < 0 ? "添加资源目录" : "编辑资源目录"}</h3><div class="agent-field-pair">${mountField("name","挂载名称","Agent 看到的名称，与实际目录名分别设置。")}<div class="agent-field"><label for="agent-action-runner">Runner</label><select id="agent-action-runner" data-resource-field="runner_id" ${locked ? "disabled" : ""}><option value="">选择 Runner</option>${runnerChoices.map((runner)=>`<option value="${runner.id}" ${runner.id===resource.mount.runner_id ? "selected" : ""}>${runner.label}</option>`).join("")}</select></div></div>${mountField("workspace","资源目录名","单段逻辑目录名，例如 workspace-1；不能填写绝对路径。")}${mountField("description","说明")}<div class="agent-resource-editor-actions">${button("取消","cancel-mount","quiet",!!state.busy || !!state.pending)}<button type="submit" class="button primary" data-agent-action="confirm-mount" ${locked ? "disabled" : ""}>${creating ? "确认添加" : "确认保存"}</button></div></form>` : button("添加资源目录","add-mount","",locked);
  const allSecrets = [...secretChoices,...current.secretIDs.filter((id)=>!secretChoices.some((item)=>item.id===id)).map((id)=>({id,label:`已授权 Secret · ${id}`,description:"目录暂未返回此引用；保留授权，可单独撤销。"}))];
  const secrets = allSecrets.filter((item)=>`${item.label} ${item.description}`.toLowerCase().includes(resource.secretQuery.toLowerCase()));
  const tools = toolChoices.filter((tool)=>(resource.toolSource==="all" || tool.source===resource.toolSource) && (resource.toolAccess==="all" || tool.access===resource.toolAccess) && (resource.toolRisk==="all" || tool.risk===resource.toolRisk) && `${tool.name} ${tool.purpose}`.toLowerCase().includes(resource.toolQuery.toLowerCase()));
  const filterSelect = (key,title,choices) => `<label><span class="sr-only">${title}</span><select id="agent-filter-${key}" data-resource-filter="${key}">${choices.map(([id,name])=>`<option value="${id}" ${resource[key]===id ? "selected" : ""}>${name}</option>`).join("")}</select></label>`;
  const toolContent = `<p class="agent-section-note">逐项授予普通工具；MCP 按具体 Tool 授权。审批不会扩大这些权限。</p><div class="agent-filterbar agent-tool-filters"><input aria-label="筛选工具" type="search" placeholder="筛选工具…" data-resource-filter="toolQuery" value="${esc(resource.toolQuery)}">${filterSelect("toolSource","工具来源",[["all","全部来源"],["Builtin","Builtin"],["Runner","Runner"],["MCP","MCP"]])}${filterSelect("toolAccess","读写类型",[["all","读写类型"],["read","只读"],["write","写入"]])}${filterSelect("toolRisk","工具风险",[["all","全部风险"],["low","低风险"],["medium","中风险"],["high","高风险"]])}</div><div class="agent-catalog-list">${tools.map((tool)=>`<label class="agent-choice-row"><input type="checkbox" data-tool="${tool.id}" ${current.allowedToolIDs.includes(tool.id) ? "checked" : ""} ${locked || !tool.available ? "disabled" : ""}><span><strong>${esc(tool.name)}${!tool.available ? " · 已停用" : ""}</strong><span>${esc(tool.purpose)}</span><small>${tool.source} · ${tool.access==="read" ? "只读" : "写入"} · ${({low:"低",medium:"中",high:"高"})[tool.risk]}风险</small></span></label>`).join("") || '<p class="agent-muted-line">没有符合筛选的工具。</p>'}</div><details class="agent-core-tools"><summary>Core Tools · 始终可见（${coreTools.length}）</summary><p>平台核心能力只读，不可禁用；仍受项目范围和服务端授权限制。</p><div>${coreTools.map((tool)=>`<span>${esc(tool)}</span>`).join("")}</div></details>`;
  const skillRows = current.skills.map((assignment)=>{const skill=skillChoices.find((item)=>item.id===assignment.skillID); return `<div class="agent-skill-row"><div><strong>${esc(skill?.name || assignment.skillID)}${skill?.protected ? '<span class="agent-protected">受保护</span>' : ""}</strong><span>${assignment.enabled ? "已启用" : "已禁用"} · 修订 ${esc(assignment.revision || skill?.revision || "1")}</span><p>${esc(skill?.description || "技能目录暂不可用，分配记录保留。")}</p></div>${button(assignment.enabled ? "禁用" : "启用",`skill-${assignment.enabled ? "disable" : "enable"}:${assignment.id}`,"quiet",locked)}${button("撤销分配",`skill-remove:${assignment.id}`,"quiet",locked)}</div>`;}).join("");
  const feedbackActions = resource.status==="unknown" ? button("查询原资源操作","lookup") : resource.status==="resolved" ? button("重新读取资源","resource-readback") : resource.failed && !edit.conflict ? button("重试此操作","resource-retry","",locked) : "";
  return `<div class="agent-detail-content" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-resources">${readOnly(ctx) ? notice("当前项目为只读","资源可以阅读，修改暂不可用。") : ""}${creating ? '<p class="agent-section-note">这些资源在创建 Agent 时一并保存。</p>' : ""}${edit.conflict ? notice("配置已有新版本","基本配置草稿和未提交资源操作已保留。",button("重读并保留草稿","basic-rebase","",!!state.pending || !!state.busy)) : ""}${state.domainPending ? notice("操作结果待确认","请回到原操作查询结果。",button("查看原操作","original")) : ""}<div id="agent-resource-feedback" class="agent-resource-feedback" role="status" ${resource.message ? "" : "hidden"} data-status="${resource.status}"><span>${esc(resource.message)}</span>${feedbackActions}</div>${gate(ctx,"Agent 资源") || `<section class="agent-resource-groups">${disclosure("tools","工具权限",`${current.allowedToolIDs.length} 项普通工具已授权`,toolContent,edit)}${disclosure("runners","Runner 与资源目录",`${current.runnerMounts.length} 项资源目录`,`<p class="agent-section-note">填写完整挂载信息后确认。移除配置不会删除目录。</p>${mountRows}${mountEditor}`,edit)}${disclosure("secrets","Secret Variable 白名单",`${current.secretIDs.length} 项已授权`,`<p class="agent-section-note">只展示名称与用途，不显示凭据值。未加载的已有授权会保留。</p><input class="agent-catalog-search" type="search" aria-label="筛选 Secret" placeholder="筛选名称或用途…" data-resource-filter="secretQuery" value="${esc(resource.secretQuery)}">${secrets.slice(0,resource.secretLimit).map((secret)=>`<label class="agent-choice-row"><input type="checkbox" data-secret="${secret.id}" ${current.secretIDs.includes(secret.id) ? "checked" : ""} ${locked ? "disabled" : ""}><span><strong>${esc(secret.label)}</strong><span>${esc(secret.description)}</span></span></label>`).join("")}${secrets.length>resource.secretLimit ? button(`显示其余 ${secrets.length-resource.secretLimit} 项`,"secret-more","quiet") : `<p class="agent-field-hint">已展示全部 ${secrets.length} 项候选</p>`}`,edit)}${disclosure("skills","项目技能",`${current.skills.length} 项分配`,`<p class="agent-section-note">保存分配后，新分配可在后续模型轮输入确定时生效。禁用或撤销不改写已运行执行的固定版本。</p>${skillRows || '<p class="agent-muted-line">尚未分配项目技能。</p>'}${button("分配项目技能","skill-assign","",locked)}`,edit)}${creating ? `<section class="agent-initialization"><h3>创建时初始化</h3><label class="agent-check-row"><input type="checkbox" data-agent-init="addSkills" ${current.addSkills ? "checked" : ""} ${locked ? "disabled" : ""}>添加默认 Skills（Add Skills）</label><label class="agent-check-row"><input type="checkbox" data-agent-init="installSkill" ${current.installSkill ? "checked" : ""} ${locked ? "disabled" : ""}>启用 Install Skill 工具</label><p class="agent-field-hint">仅创建时应用；已明确禁用的技能不会被初始化重新启用。</p></section>` : ""}</section>${creating ? `<form id="agent-configuration-form" novalidate aria-label="创建 Agent">${savebar(ctx,state,edit)}</form>` : records(current)}`}</div>`;
}
function outcomeFor(ctx, state, domain) {
  const outcome = ctx.params.get(`${domain}_result`) || "success";
  const key = `${agentID(ctx)}:${domain}:${outcome}`;
  if(state.outcomes.has(key)) return "success";
  state.outcomes.add(key);
  return outcome;
}
function mountError(mounts) {
  if(mounts.length > 128 || new Set(mounts.map((mount)=>mount.name)).size !== mounts.length) return "挂载名称必须唯一，最多 128 项。";
  if(mounts.some((mount)=>!mount.name?.trim() || !mount.runner_id || !/^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$/.test(mount.workspace) || mount.workspace.endsWith(".") || /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$/i.test(mount.workspace.split(".")[0]))) return "请填写挂载名称、选择 Runner，并使用单段英文资源目录名。";
  return "";
}
function resourceAction(ctx, action) {
  const state=local(ctx), id=agentID(ctx), edit=draftFor(ctx,state), current=id==="new" ? edit.value : state.agents.find((agent)=>agent.id===id), resource=resourceState(state,id);
  if(readOnly(ctx) || state.pending || state.busy || state.domainPending || state.domainBusy || edit.conflict) return;
  if(action==="resource-retry" && resource.failed) return submitResource(ctx,resource.failed.field,resource.failed.payload,resource.failed.label);
  if(action==="cancel-mount") {resource.mount=null;resource.message="";return refresh(ctx,'[data-agent-action="add-mount"]');}
  if(action==="add-mount" || action.startsWith("edit-mount:")) {
    resource.index=action==="add-mount" ? -1 : Number(action.split(":")[1]);
    resource.mount=resource.index<0 ? {name:"",runner_id:"",workspace:"",description:""} : copy(current.runnerMounts[resource.index]);
    return refresh(ctx,"#agent-action-name");
  }
  if(action.startsWith("remove-mount:")) {
    const index=Number(action.split(":")[1]), mount=current.runnerMounts[index];
    return confirm(`移除 ${mount.name}？`,`从 ${label(current) || "新 Agent"} 撤销 ${runnerChoices.find((item)=>item.id===mount.runner_id)?.label || mount.runner_id} / ${mount.workspace} 的挂载。目录内容仍保留。`,()=>submitResource(ctx,"runnerMounts",current.runnerMounts.filter((_,i)=>i!==index),"资源目录"),"确认移除");
  }
  if(action==="confirm-mount" && resource.mount) {
    const mounts=copy(current.runnerMounts);
    if(resource.index<0) mounts.push(copy(resource.mount)); else mounts[resource.index]=copy(resource.mount);
    const error=mountError(mounts);
    if(error) {resource.status="error";resource.message=error;return refresh(ctx,!resource.mount.name ? "#agent-action-name" : "#agent-action-workspace");}
    return submitResource(ctx,"runnerMounts",mounts,"资源目录");
  }
}
function publishResource(state,current,edit,command) {
  const sameBasic=same(basicSnapshot(current),edit.baseSnapshot);
  const changed=!same(current[command.field],command.payload);
  current[command.field]=copy(command.payload);
  if(changed) {current.version=String(Number(current.version)+1);current.updatedAt=new Date().toISOString();}
  if(sameBasic) {edit.baseVersion=current.version;edit.baseSnapshot=copy(basicSnapshot(current));} else edit.conflict=true;
  const resource=resourceState(state,current.id);
  resource.status="success";resource.failed=null;
  resource.message=command.field==="skills" ? "技能分配已保存。新分配将在后续模型轮输入确定时生效；已运行版本保持原状。" : `${command.label}已保存。`;
  if(command.field==="runnerMounts") resource.mount=null;
}
function submitResource(ctx,field,payload,labelText) {
  const state=local(ctx),id=agentID(ctx),edit=draftFor(ctx,state),resource=resourceState(state,id);
  if(readOnly(ctx) || state.pending || state.busy || state.domainPending || state.domainBusy || edit.conflict) return;
  if(id==="new") {
    edit.value[field]=copy(payload);markDirty(ctx,field);
    if(field==="runnerMounts") resource.mount=null;
    resource.message=`${labelText}已加入创建草稿。`;resource.status="success";
    return refresh(ctx);
  }
  const current=state.agents.find((agent)=>agent.id===id);
  const command={project:scopeFor(ctx),agent:id,field,payload:copy(payload),expectedVersion:current.version,key:crypto.randomUUID(),label:labelText};
  state.busy=command;resource.failed=null;resource.status="pending";resource.message=`正在保存${labelText}…`;refresh(ctx);
  setTimeout(()=>{
    if(state.busy!==command)return;
    state.busy=null;
    const outcome=outcomeFor(ctx,state,"resource");
    if(outcome==="unknown" || outcome==="version_jump") {
      state.pending={kind:"resource",agent:id,original:command,observation:"",phase:"unknown"};
      resource.status="unknown";resource.message=outcome==="version_jump" ? "操作回执版本需要核对，原操作和草稿已保留。" : `${labelText}结果待确认，原操作已保留。`;
    } else if(outcome==="error") {
      resource.status="error";resource.message=`${labelText}未保存。输入已保留，可重试此操作。`;resource.failed=command;
    } else if(outcome==="conflict" || current.version!==command.expectedVersion) {
      current.version=String(Number(current.version)+1);current.description+="（协作范围已更新）";
      edit.conflict=true;resource.status="error";resource.message="资源操作遇到版本冲突，输入与基本配置草稿已保留。";resource.failed=command;
    } else publishResource(state,current,edit,command);
    if(new URLSearchParams(location.search).get("agent")===id) refresh(ctx);
  },350);
}
function creationValue(value) {
  const next=copy(value);
  next.id=`agent-${crypto.randomUUID()}`;next.version="1";next.createdAt=new Date().toISOString();next.updatedAt=next.createdAt;next.memories=[];next.approvals=[];next.executions=[];
  if(next.addSkills) for(const skill of skillChoices.filter((item)=>item.default)) if(!next.skills.some((item)=>item.skillID===skill.id)) next.skills.push({id:`assignment-${skill.id}`,skillID:skill.id,revision:skill.revision,enabled:true});
  next.allowedToolIDs=next.allowedToolIDs.filter((id)=>id!=="install-skill");
  if(next.installSkill) next.allowedToolIDs.push("install-skill");
  delete next.addSkills;delete next.installSkill;
  return next;
}
function publishConfiguration(ctx,state,command) {
  const edit=state.edits[command.agent];
  if(command.agent==="new") {
    const value=creationValue(command.input);state.agents.push(value);delete state.edits.new;command.createdID=value.id;
    return value;
  }
  const current=state.agents.find((agent)=>agent.id===command.agent);
  for(const field of command.fields) if(basicFields.includes(field)) current[field]=copy(command.input[field]);
  current.version=String(Number(current.version)+1);current.updatedAt=new Date().toISOString();
  edit.value=copy(basicSnapshot(current));edit.baseSnapshot=copy(basicSnapshot(current));edit.baseVersion=current.version;edit.touched.clear();edit.failed=null;edit.message="配置已保存。";
  return current;
}
function submitConfiguration(ctx) {
  const state=local(ctx),id=agentID(ctx),edit=draftFor(ctx,state),current=state.agents.find((agent)=>agent.id===id);
  if(readOnly(ctx) || state.pending || state.busy || state.domainPending || state.domainBusy || edit.conflict || !edit.touched.size)return;
  if(current && (current.version!==edit.baseVersion || !same(basicSnapshot(current),edit.baseSnapshot))) {edit.conflict=true;return refresh(ctx);}
  const error=validation(edit.value);
  if(error) {
    edit.message=error[1];edit.error=error;
    const tab=error[0]==="runnerMounts" ? "resources" : "basic";
    if(tab!==tabFor(ctx)) ctx.navigate("agents",{agent:id,...(tab==="basic" ? {} : {view:tab})}); else {refresh(ctx);requestAnimationFrame(()=>showFieldError(ctx,error));}
    return;
  }
  if(state.agents.some((agent)=>agent.id!==id && agent.name.toLowerCase()===edit.value.name.toLowerCase())) {edit.message="此 Agent 名称已被使用。";return refresh(ctx,"#agent-name");}
  const command={kind:id==="new" ? "creation" : "configuration",project:scopeFor(ctx),agent:id,input:copy(edit.value),fields:[...edit.touched],expectedVersion:edit.baseVersion,key:crypto.randomUUID()};
  state.busy=command;edit.failed=null;edit.message=id==="new" ? "正在创建 Agent…" : "正在保存配置…";refresh(ctx);
  setTimeout(()=>{
    if(state.busy!==command)return;state.busy=null;
    const outcome=outcomeFor(ctx,state,id==="new" ? "creation" : "basic");
    if(outcome==="unknown") {state.pending={kind:command.kind,agent:id,original:command,phase:"unknown",observation:""};edit.message="操作结果待确认。原输入和请求已保留。";}
    else if(outcome==="error") {edit.message="保存失败，输入已保留。可以重试保存。";edit.failed=command;}
    else if(outcome==="conflict") {if(current){current.version=String(Number(current.version)+1);current.description+="（协作范围已更新）";}edit.conflict=true;edit.message="配置已有新版本，输入已保留。";}
    else {const result=publishConfiguration(ctx,state,command);if(id==="new")return ctx.navigate("agents",{agent:result.id});}
    refresh(ctx,"#agent-form-feedback");
  },350);
}
function lookupOperation(ctx) {
  const state=local(ctx),pending=state.pending;if(!pending || state.busy)return;
  const command=pending.original, domain=pending.kind==="resource" ? "resource" : pending.kind==="creation" ? "creation" : "basic";
  pending.lookups=(pending.lookups || 0)+1;
  const result=ctx.params.get(`${domain}_lookup`) || "committed";
  // The normal demonstration progresses to a terminal receipt. not_observed is an explicit fixture only.
  if(result==="not_observed" && pending.lookups===1) {pending.observation="not_observed";if(domain==="resource")resourceState(state,pending.agent).message="暂未观察到结果，原请求仍保留。再次查询可核对终态。";else state.edits[pending.agent].message="暂未观察到结果，请再次查询原操作。";return refresh(ctx);}
  const current=state.agents.find((agent)=>agent.id===pending.agent),edit=state.edits[pending.agent];
  pending.phase="resolved";pending.observation=result==="failed" ? "failed" : "committed";
  if(pending.observation==="committed" && !pending.applied) {
    if(domain==="resource")publishResource(state,current,edit,command);else publishConfiguration(ctx,state,{...command,agent:pending.agent,fields:command.fields || basicFields,input:command.input || command.value});
    pending.applied=true;
  }
  if(domain==="resource") {const resource=resourceState(state,pending.agent);resource.status="resolved";resource.message=pending.observation==="committed" ? "原操作已确认成功。重新读取资源后恢复修改。" : "原操作已确认失败。重新读取后可重试保留的操作。";if(pending.observation==="failed")resource.failed=command;}
  else {const next=state.edits[pending.agent];if(next)next.message=pending.observation==="committed" ? "原操作已确认成功。重新读取配置后继续。" : "原操作已确认失败。重新读取后可重试，输入保留。";}
  refresh(ctx);
}
function readbackOperation(ctx) {
  const state=local(ctx),pending=state.pending;if(!pending || pending.phase!=="resolved")return;
  state.pending=null;
  if(pending.agent==="new" && pending.observation==="committed") {const created=state.agents.at(-1);return ctx.navigate("agents",{agent:created.id});}
  const edit=draftFor(ctx,state);
  if(pending.kind==="resource") {const resource=resourceState(state,pending.agent);resource.status=pending.observation==="committed" ? "success" : "error";resource.message=pending.observation==="committed" ? "已重新读取资源，操作已恢复。" : "已重新读取。原操作失败，可重试保留的输入。";}
  else {edit.message=pending.observation==="committed" ? "已重新读取配置，操作已恢复。" : "已重新读取。原输入已保留，可重试保存。";if(pending.observation==="failed")edit.failed=pending.original;}
  refresh(ctx);
}
function presetDialog(ctx) {
  const state=local(ctx),edit=draftFor(ctx,state);
  showDialog(`${dialogHeader("选择系统 Preset")}<div class="dialog-body"><p class="muted small">复制角色配置到新 Agent；后续模板变化不会同步。</p><div class="agent-preset-list">${presets.map((preset)=>`<button type="button" class="agent-preset-option" data-preset-preview="${preset.id}"><strong>${esc(preset.name)}</strong><span>${esc(preset.description)}</span><small>预览复制内容</small></button>`).join("")}</div></div>${dialogFooter('<button class="button" data-dialog-close>继续空白配置</button>')}`,(dialog)=>{
    dialog.querySelectorAll("[data-preset-preview]").forEach((control)=>control.onclick=()=>preview(presets.find((preset)=>preset.id===control.dataset.presetPreview)));
  });
  function preview(preset) {
    const model=models.find((item)=>item.id===preset.modelID),validTools=preset.allowedToolIDs.filter((id)=>toolChoices.some((tool)=>tool.id===id && tool.available && tool.source==="Builtin")),skipped=preset.allowedToolIDs.filter((id)=>!validTools.includes(id));
    showDialog(`${dialogHeader(`复制 ${preset.name}`)}<div class="dialog-body"><dl class="agent-preview-details"><dt>职责</dt><dd>${esc(preset.description)}</dd><dt>主模型</dt><dd>${esc(model?.label || "未指定模型")}${!model?.available ? " · 复制后必须重新选择" : ` · ${esc(model.source)} / ${esc(model.provider)}`}</dd><dt>推理强度</dt><dd>${esc(preset.reasoningEffort || "由所选模型决定")}</dd><dt>指令</dt><dd>${esc(preset.instructions)}</dd><dt>内置工具推荐</dt><dd>${validTools.map((id)=>esc(toolChoices.find((tool)=>tool.id===id).name)).join("、") || "无"}</dd></dl>${skipped.length ? `<p class="agent-copy-warning">将跳过不可用推荐：${skipped.map(esc).join("、")}。无需修改模板。</p>` : ""}<p class="muted small">不复制项目 Secret、挂载、Skills、记忆、审批或 MCP 身份。</p>${edit.touched.size ? '<p class="agent-copy-warning">已有草稿：确认后只覆盖描述、颜色、指令、模型、推理强度、AGENTS.md 策略及内置工具推荐。保留名称、显示名称、项目资源、MCP 授权和初始化开关。</p>' : ""}</div>${dialogFooter('<button class="button" data-dialog-close>保留当前草稿</button><button class="button primary" data-preset-copy>确认复制配置</button>')}`,(dialog)=>dialog.querySelector("[data-preset-copy]").onclick=()=>{
      for(const key of ["description","tagColor","instructions","modelID","reasoningEffort","injectAgentsMD"]){edit.value[key]=copy(preset[key]);edit.touched.add(key);}
      if(!edit.value.name){edit.value.name=preset.name.toLowerCase().replaceAll(" ","-");edit.touched.add("name");}
      if(!edit.value.displayName){edit.value.displayName=preset.name;edit.touched.add("displayName");}
      edit.value.allowedToolIDs=[...new Set([...edit.value.allowedToolIDs.filter((id)=>toolChoices.find((tool)=>tool.id===id)?.source!=="Builtin" || id==="install-skill"),...validTools])];edit.touched.add("allowedToolIDs");
      edit.preset=preset.name;edit.presetMessage=[!model?.available ? "模型未指定或已失效，请重新选择当前可用模型。" : "已复制角色配置。",skipped.length ? `已跳过不可用工具推荐：${skipped.join("、")}。` : "", "已有名称与项目资源保持不变。"].filter(Boolean).join(" ");
      closeDialog();refresh(ctx,"#agent-name");
    });
  }
}

function render(ctx) {
  const state = local(ctx),
    id = agentID(ctx);
  if (!id) return directory(ctx, state);
  const current = state.agents.find((agent) => agent.id === id);
  if ((id !== "new" && !current) || ctx.scenario === "missing")
    return `<section class="agent-page">${heading("Agent 不可用", "此 Agent 不存在，或当前身份无法访问。", "", backLink(ctx, "Agent"))}<section class="agent-state">${button("返回 Agent 列表", "list")}</section></section>`;
  const creating = id === "new";
  return `<section class="agent-page agent-object-page">${heading(creating ? "创建 Agent" : label(current), creating ? "设置这个 Agent 的身份、指令与执行配置。" : current.description, "", backLink(ctx, "Agent"))}${tabs(ctx)}${isMemory(ctx) && current ? renderAgentMemory(ctx,state,current,domainBridge(ctx)) : tabFor(ctx) === "audit" && current ? renderAgentAudit(ctx,state,current,domainBridge(ctx)) : tabFor(ctx) === "resources" ? resources(ctx,state,current) : configuration(ctx,state,current)}</section>`;
}
function domainBridge(ctx) { return {esc,icon,button,notice,readOnly,scopeFor,refresh,records}; }
function confirm(title, description, accepted, action = "放弃修改") {
  showDialog(
    `${dialogHeader(title)}<div class="dialog-body"><p>${esc(description)}</p></div>${dialogFooter(`<button type="button" class="button" data-dialog-close>继续编辑</button><button type="button" class="button primary" data-agent-confirm>${esc(action)}</button>`)}`,
    (dialog) => {
      dialog.querySelector("[data-agent-confirm]").onclick = () => {
        closeDialog();
        accepted();
      };
    },
  );
}
function refresh(ctx, focus) {
  captureView(ctx);
  ctx.render();
  if (focus)
    requestAnimationFrame(() =>
      document.querySelector(focus)?.focus({ preventScroll: true }),
    );
}
function markDirty(ctx, key) {
  const state = local(ctx),
    edit = draftFor(ctx, state);
  edit.touched.add(key);
  edit.message = "";
  edit.error = null;
  clearFieldErrors();
  const save = document.getElementById("agent-save");
  if (save)
    save.disabled =
      readOnly(ctx) || !!state.pending || !!state.busy || edit.conflict;
  const note = document.getElementById("agent-dirty-state");
  if (note) note.textContent = "有未保存的修改";
  const feedback = document.getElementById("agent-form-feedback");
  if (feedback) feedback.textContent = "";
}
function clearFieldErrors() {
  const form = document.getElementById("agent-configuration-form");
  form?.querySelectorAll('[aria-invalid="true"]').forEach((field) => {
    field.removeAttribute("aria-invalid");
    const ids = (field.getAttribute("aria-describedby") || "")
      .split(" ")
      .filter((id) => id && !id.endsWith("-error"));
    if (ids.length) field.setAttribute("aria-describedby", ids.join(" "));
    else field.removeAttribute("aria-describedby");
  });
  form?.querySelectorAll(".agent-field-error").forEach((message) => {
    message.textContent = "";
    message.hidden = true;
  });
}
function showFieldError(ctx, error) {
  const root = document.getElementById("main");
  const edit = draftFor(ctx, local(ctx));
  let fieldID = `agent-${error[0]}`;
  if (error[0] === "runnerMounts") {
    const index = Math.max(
      0,
      edit.value.runnerMounts.findIndex(
        (mount) =>
          !mount.runner_id ||
          !/^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$/.test(mount.workspace),
      ),
    );
    fieldID = `agent-${edit.value.runnerMounts[index]?.runner_id ? "workspace" : "runner"}-${index}`;
  }
  const field = root.querySelector(`#${fieldID}`);
  if (!field) return;
  field.closest("details")?.setAttribute("open", "");
  const feedback = root.querySelector("#agent-form-feedback");
  if (feedback) feedback.textContent = error[1];
  const target =
    field.tagName === "SELECT"
      ? root.querySelector(`[data-select-for="${field.id}"]`) || field
      : field;
  const message = root.querySelector(`#agent-${error[0]}-error`);
  if (message) {
    message.textContent = error[1];
    message.hidden = false;
  }
  for (const control of new Set([field, target])) {
    control.setAttribute("aria-invalid", "true");
    if (message)
      control.setAttribute(
        "aria-describedby",
        [control.getAttribute("aria-describedby"), message.id]
          .filter(Boolean)
          .join(" "),
      );
  }
  target.focus();
}
function validation(value) {
  if (
    !/^[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]$/.test(value.name) ||
    [
      "api",
      "assets",
      "auth",
      "login",
      "logout",
      "invite",
      "reset",
      "settings",
      "system",
      "personal",
      "diagnostics",
      "livez",
      "readyz",
      "debug",
      "support",
      "root",
      "admin",
    ].includes(value.name.toLowerCase())
  )
    return [
      "name",
      "名称须为 3–32 位字母、数字或连字符，首尾为字母或数字，且不能使用系统保留名称。",
    ];
  if (
    value.displayName &&
    (!value.displayName.trim() || [...value.displayName].length > 256)
  )
    return ["displayName", "显示名称不能全为空白，最多 256 个字符。"];
  if (value.tagColor && !/^#[0-9a-f]{6}$/.test(value.tagColor))
    return ["tagColor", "标签颜色使用小写 #rrggbb，或留空。"];
  if (!models.some((model) => model.id === value.modelID && model.available))
    return ["modelID", "请选择主模型。"];
  if (
    value.approvalPolicy === "auto" &&
    !models.some((model) => model.id === value.approvalModelID && model.available)
  )
    return ["approvalModelID", "模型自动审批需要选择审批模型。"];
  const model=models.find((item)=>item.id===value.modelID);
  if(model?.efforts.length && !model.efforts.includes(value.reasoningEffort)) return ["reasoningEffort","请选择当前模型支持的推理强度。"];
  if(value.runnerMounts && mountError(value.runnerMounts)) return ["runnerMounts",mountError(value.runnerMounts)];
  if (new TextEncoder().encode(value.description).byteLength > 8192)
    return ["description", "描述最多 8192 UTF-8 字节。"];
  if (new TextEncoder().encode(value.instructions).byteLength > 32768)
    return ["instructions", "指令最多 32768 UTF-8 字节。"];
  return null;
}
function bind(ctx) {
  const root = document.getElementById("main"),
    state = local(ctx),
    id = agentID(ctx);
  const current = state.agents.find((agent) => agent.id === id);
  if(current && ["memory","audit"].includes(tabFor(ctx))) bindAgentMemoryAudit(ctx,state,current,domainBridge(ctx));
  root.querySelectorAll("[data-agent-open]").forEach(
    (link) =>
      (link.onclick = (event) => {
        if (event.metaKey || event.ctrlKey || event.shiftKey) return;
        event.preventDefault();
        state.returnFocus = link.dataset.agentOpen;
        ctx.navigate("agents", { agent: link.dataset.agentOpen });
      }),
  );
  root.querySelectorAll("[data-agent-back]").forEach(
    (link) =>
      (link.onclick = (event) => {
        if (event.metaKey || event.ctrlKey || event.shiftKey) return;
        event.preventDefault();
        ctx.navigate(
          "agents",
          link.dataset.agentBack ? { agent: link.dataset.agentBack } : {},
        );
      }),
  );
  const tabLinks = [...root.querySelectorAll("[data-agent-tab]")];
  const activateTab = (link) => {
    const tab = link.dataset.agentTab;
    if (tab === tabFor(ctx)) return;
    ctx.navigate("agents", {
      agent: id,
      ...(tab === "basic" ? {} : { view: tab }),
    });
  };
  tabLinks.forEach((link, index) => {
    link.onclick = (event) => {
      if (event.metaKey || event.ctrlKey || event.shiftKey) return;
      event.preventDefault();
      activateTab(link);
    };
    link.onkeydown = (event) => {
      let target;
      if (event.key === "ArrowRight") target = (index + 1) % tabLinks.length;
      if (event.key === "ArrowLeft")
        target = (index - 1 + tabLinks.length) % tabLinks.length;
      if (event.key === "Home") target = 0;
      if (event.key === "End") target = tabLinks.length - 1;
      if (target !== undefined) {
        event.preventDefault();
        tabLinks[target].focus();
      } else if (event.key === " ") {
        event.preventDefault();
        activateTab(link);
      }
    };
  });
  root.querySelectorAll("[data-agent-section]").forEach((section) =>
    section.addEventListener("toggle", () => {
      const edit = draftFor(ctx, state);
      if (section.open) edit.sections.add(section.dataset.agentSection);
      else edit.sections.delete(section.dataset.agentSection);
    }),
  );
  root.querySelectorAll("[data-agent-field]").forEach((field) =>
    field.addEventListener(
      field.tagName === "SELECT" ? "change" : "input",
      () => {
        const edit = draftFor(ctx, state);
        edit.value[field.dataset.agentField] = field.dataset.agentField === "reasoningEffort" ? field.value || null : field.value;
        markDirty(ctx, field.dataset.agentField);
        if (field.dataset.agentField === "modelID") {
          const model=models.find((item)=>item.id===field.value);
          if(!model?.efforts.includes(edit.value.reasoningEffort)) edit.value.reasoningEffort=null;
          edit.touched.add("reasoningEffort");
          return refresh(ctx,'[data-select-for="agent-modelID"]');
        }
        if (field.dataset.agentField === "approvalPolicy") {
          const auto = field.value === "auto";
          document.getElementById("agent-approval-model").hidden = !auto;
          if (!auto) {
            edit.value.approvalModelID = "";
            edit.touched.add("approvalModelID");
          }
        }
      },
    ),
  );
  root
    .querySelector("[data-agent-inject]")
    ?.addEventListener("change", (event) => {
      draftFor(ctx, state).value.injectAgentsMD = event.target.checked
        ? "yes"
        : "no";
      markDirty(ctx, "injectAgentsMD");
    });
  root.querySelectorAll("[data-agent-init]").forEach(
    (field) =>
      (field.onchange = () => {
        draftFor(ctx, state).value[field.dataset.agentInit] = field.checked;
        markDirty(ctx, field.dataset.agentInit);
      }),
  );
  root.querySelectorAll("[data-mount-index]").forEach((field) =>
    field.addEventListener(
      field.tagName === "SELECT" ? "change" : "input",
      () => {
        draftFor(ctx, state).value.runnerMounts[
          Number(field.dataset.mountIndex)
        ][field.dataset.mountKey] = field.value;
        markDirty(ctx, "runnerMounts");
      },
    ),
  );
  root.querySelector("[data-list-filter]")?.addEventListener("input",event=>{state.listFilter=event.target.value;state.listPage=0;refresh(ctx,"#agent-list-search");});
  root.querySelector("[data-list-status]")?.addEventListener("change",event=>{state.listStatus=event.target.value;state.listPage=0;refresh(ctx,'[data-select-for="agent-list-status"]');});
  root.querySelectorAll("[data-resource-filter]").forEach((control)=>control.addEventListener(control.tagName==="SELECT" ? "change" : "input",()=>{
    resourceState(state,id)[control.dataset.resourceFilter]=control.value;
    const selector=control.tagName==="SELECT" ? `[data-select-for="${control.id}"]` : `[data-resource-filter="${control.dataset.resourceFilter}"]`;
    refresh(ctx,selector);
  }));
  root.querySelectorAll("[data-secret],[data-tool]").forEach((field)=>field.onchange=()=>{
    const value=id==="new" ? draftFor(ctx,state).value : current;
    const isTool=field.hasAttribute("data-tool"),key=isTool ? "allowedToolIDs" : "secretIDs",target=isTool ? field.dataset.tool : field.dataset.secret;
    const selected=new Set(value[key]);if(field.checked)selected.add(target);else selected.delete(target);
    if(id==="new" && target==="install-skill") {value.installSkill=field.checked;markDirty(ctx,"installSkill");}
    submitResource(ctx,key,[...selected],isTool ? "工具权限" : "Secret 白名单");
  });
  root.querySelectorAll("[data-resource-field]").forEach((field) => {
    field.addEventListener(
      field.tagName === "SELECT" ? "change" : "input",
      () => {
        resourceState(state, id).mount[field.dataset.resourceField] =
          field.value;
      },
    );
  });
  root
    .querySelector("#agent-resource-editor")
    ?.addEventListener("submit", (event) => {
      event.preventDefault();
      handleAction(ctx, "confirm-mount", event.submitter);
    });
  root.querySelector("#agent-configuration-form")?.addEventListener("submit",(event)=>{event.preventDefault();submitConfiguration(ctx);});
  root.querySelectorAll("[data-agent-action]").forEach(
    (control) =>
      (control.onclick = (event) => {
        if (control.dataset.agentAction === "confirm-mount")
          event.preventDefault();
        handleAction(ctx, control.dataset.agentAction, control);
      }),
  );
}
function handleAction(ctx, action, trigger) {
  const state = local(ctx),
    id = agentID(ctx),
    current = state.agents.find((agent) => agent.id === id),
    edit = id && !isMemory(ctx) ? draftFor(ctx, state) : null;
  if (action === "create") return ctx.navigate("agents", { agent: "new" });
  if (action === "list") return ctx.navigate("agents", {});
  if(action==="original") {const pending=state.pending || state.domainPending;return ctx.navigate("agents",{agent:pending.agent,...(pending.view ? {view:pending.view} : pending.kind==="resource" ? {view:"resources"} : {})});}
  if(action==="preset-select") return presetDialog(ctx);
  if(action==="list-clear") {state.listFilter="";state.listStatus="all";state.listPage=0;return refresh(ctx,"#agent-list-search");}
  if(action==="list-prev" || action==="list-next") {state.listPage+=action==="list-next" ? 1 : -1;return refresh(ctx,"#agent-page-title");}
  if(action==="secret-more") {resourceState(state,id).secretLimit=1000;return refresh(ctx);}
  if(action==="basic-retry") return submitConfiguration(ctx);
  if(action==="basic-rebase") {edit.baseVersion=current.version;edit.baseSnapshot=copy(basicSnapshot(current));edit.conflict=false;edit.message="已重新读取当前版本，草稿保留。请核对后再保存。";return refresh(ctx);}
  if(action==="resource-readback" || action==="basic-readback") return readbackOperation(ctx);
  if (action === "retry") return ctx.setScenario("normal");
  if (action === "refresh") {
    state.notice = "Agent 目录已更新。";
    return refresh(ctx, '[data-agent-action="refresh"]');
  }
  if (action === "memory")
    return ctx.navigate("agents", { agent: id, view: "memory" });
  if (action === "memory-refresh") {
    state.notice = "记忆已更新。";
    return refresh(ctx, '[data-agent-action="memory-refresh"]');
  }
  if(action==="lookup") return lookupOperation(ctx);
  if (action === "cancel" || action === "conflict-read") {
    if (state.busy || state.pending) return;
    const reset = () => {
      if (state.busy || state.pending) return;
      delete state.edits[id];
      if (action === "conflict-read") {
        const next = draftFor(ctx, state);
        next.conflict = false;
        next.message = "已重新读取当前配置。";
      }
      if (id === "new") ctx.navigate("agents", {});
      else {
        refresh(
          ctx,
          tabFor(ctx) === "resources" ? "#agent-tab-resources" : "#agent-name",
        );
        if (tabFor(ctx) === "basic")
          requestAnimationFrame(() => {
            const field = document.getElementById("agent-name");
            const bounds = field?.getBoundingClientRect();
            if (bounds && (bounds.top < 8 || bounds.bottom > innerHeight - 8))
              field.scrollIntoView({ block: "center", behavior: "instant" });
          });
      }
    };
    if (edit.touched.size || edit.conflict)
      confirm(
        "放弃尚未保存的修改？",
        "当前 Agent 的未保存配置将被丢弃。",
        reset,
      );
    else reset();
    return;
  }
  if (
    (action === "add-mount" ||
      action === "cancel-mount" ||
      action === "confirm-mount" ||
      action === "resource-retry" ||
      action.startsWith("edit-mount:") ||
      action.startsWith("remove-mount:"))
  ) {
    return resourceAction(ctx, action);
  }
  if(action.startsWith("skill-")) return skillAction(ctx,action,trigger);
}
function skillAction(ctx,action) {
  const state=local(ctx),id=agentID(ctx),edit=draftFor(ctx,state),current=id==="new" ? edit.value : state.agents.find((agent)=>agent.id===id);
  if(readOnly(ctx) || state.pending || state.busy || state.domainPending || state.domainBusy || edit.conflict)return;
  if(action==="skill-assign") {
    return showDialog(`${dialogHeader("分配项目技能")}<div class="dialog-body"><input type="search" class="agent-catalog-search" aria-label="筛选项目技能" placeholder="筛选名称或用途…" data-skill-search><div class="agent-skill-candidates">${skillChoices.map((skill)=>`<div class="agent-skill-candidate" data-skill-row="${skill.id}"><div><strong>${esc(skill.name)}${skill.protected ? '<span class="agent-protected">受保护</span>' : ""}</strong><p>${esc(skill.description)}</p><small>${esc(skill.source)} · 修订 ${skill.revision}</small></div><button class="button quiet" data-skill-pick="${skill.id}" ${current.skills.some((item)=>item.skillID===skill.id) ? "disabled" : ""}>${current.skills.some((item)=>item.skillID===skill.id) ? "已分配" : "分配"}</button></div>`).join("")}</div><p class="agent-field-hint">完整项目候选 · 受保护表示项目内置包不可删除，仍可按 Agent 禁用或撤销分配。</p></div>${dialogFooter('<button class="button" data-dialog-close>关闭</button>')}`,(dialog)=>{
      dialog.querySelector("[data-skill-search]").oninput=(event)=>dialog.querySelectorAll("[data-skill-row]").forEach((row)=>row.hidden=!row.textContent.toLowerCase().includes(event.target.value.toLowerCase()));
      dialog.querySelectorAll("[data-skill-pick]").forEach((control)=>control.onclick=()=>confirmSkill("assign",control.dataset.skillPick));
    });
  }
  const [kind,item]=action.replace("skill-","").split(":");confirmSkill(kind,item);
  function confirmSkill(kind,item) {
    const assignment=current.skills.find((skill)=>skill.id===item),skill=skillChoices.find((skill)=>skill.id===(kind==="assign" ? item : assignment?.skillID));
    if(!skill)return;
    const verb=({assign:"分配",remove:"撤销分配",disable:"禁用",enable:"启用"})[kind];
    confirm(`${verb} ${skill.name}？`,`${label(current) || "新 Agent"} · ${skill.name} · 修订 ${assignment?.revision || skill.revision}。${kind==="remove" ? "仅撤销此 Agent 分配，项目技能包保留。" : "保存后用于后续模型轮输入；不会改写运行中的固定版本。"}`,()=>{
      const skills=copy(current.skills);
      if(kind==="assign")skills.push({id:`assignment-${crypto.randomUUID()}`,skillID:item,revision:skill.revision,enabled:true});
      else if(kind==="remove")skills.splice(skills.findIndex((row)=>row.id===item),1);
      else skills.find((row)=>row.id===item).enabled=kind==="enable";
      submitResource(ctx,"skills",skills,`${skill.name} 技能分配`);
    },`确认${verb}`);
  }
}
function scrollOwner() {
  const content = document.querySelector(".project-settings-content");
  return content && content.scrollHeight > content.clientHeight + 1
    ? content
    : document.scrollingElement;
}
function captureView(ctx) {
  const state = local(ctx),
    owner = scrollOwner(),
    active = document.activeElement;
  const position = {
    top: owner?.scrollTop || 0,
    windowTop: window.scrollY,
    focus:
      active?.closest("[data-agent-open]")?.dataset.agentOpen ||
      state.returnFocus,
  };
  if (!agentID(ctx)) state.listView = position;
  else state.detailViews[`${agentID(ctx)}:${tabFor(ctx)}`] = position;
}
function restoreView(ctx, info = {}) {
  const state = local(ctx),
    id = agentID(ctx),
    previous = info.previous;
  requestAnimationFrame(() => {
    const owner = scrollOwner();
    if (
      !id &&
      state.listView &&
      (info.reason !== "render" || state.returnFocus)
    ) {
      if (owner) owner.scrollTop = state.listView.top;
      window.scrollTo(0, state.listView.windowTop);
      const target = state.returnFocus || state.listView.focus;
      document
        .querySelector(`[data-agent-open="${target}"]`)
        ?.focus({ preventScroll: true });
      state.returnFocus = "";
    } else if (
      id &&
      previous?.page === "agents" &&
      agentID(previous) === id &&
      scopeFor(previous) === scopeFor(ctx)
    ) {
      const saved = state.detailViews[`${id}:${tabFor(previous)}`];
      if (owner && saved) owner.scrollTop = saved.top;
      if (saved) window.scrollTo(0, saved.windowTop);
      document
        .querySelector(`[data-agent-tab="${tabFor(ctx)}"]`)
        ?.focus({ preventScroll: true });
      state.returnFocus = "";
    } else if (info.reason === "navigate" || info.reason === "popstate") {
      if (owner) owner.scrollTop = 0;
      window.scrollTo(0, 0);
      document
        .getElementById("agent-page-title")
        ?.focus({ preventScroll: true });
    }
    const error = state.edits[id]?.error;
    if (error && !isMemory(ctx)) showFieldError(ctx, error);
  });
}
function beforeLeave(ctx, proceed, target) {
  const state = local(ctx),
    id = agentID(ctx),
    edit = state.edits[id];
  if (
    target?.page === "agents" &&
    scopeFor(target) === scopeFor(ctx) &&
    target.params.get("agent") === id &&
    id
  )
    return false;
  const pending = state.pending?.agent === id || state.busy?.agent === id || state.domainPending?.agent === id || state.domainBusy?.agent === id;
  if (!edit?.touched.size && !edit?.conflict && !pending) return false;
  if (state.leaving) return true;
  state.leaving = true;
  const dialog = showDialog(
    `${dialogHeader(pending ? "操作结果尚未确认" : "放弃尚未保存的修改？")}<div class="dialog-body"><p>${pending ? "离开后仍需回到原 Agent 查询原操作，不能重新提交。" : "离开会丢弃当前 Agent 尚未保存的配置。"}</p></div>${dialogFooter('<button class="button" data-dialog-close>继续编辑</button><button class="button primary" data-agent-leave>放弃并离开</button>')}`,
    (d) => {
      d.querySelector("[data-agent-leave]").onclick = () => {
        if (!pending) delete state.edits[id];
        state.leaving = false;
        closeDialog();
        proceed();
      };
    },
  );
  dialog.addEventListener(
    "close",
    () => {
      state.leaving = false;
    },
    { once: true },
  );
  return true;
}
function beforeUnload(ctx) {
  const state = local(ctx);
  return (
    !!state.edits[agentID(ctx)]?.touched.size ||
    !!state.edits[agentID(ctx)]?.conflict ||
    !!state.pending ||
    !!state.busy ||
    !!state.domainPending ||
    !!state.domainBusy
  );
}
export const agentPages = {
  agents: {
    title: "Agent",
    project: true,
    render,
    bind,
    beforeLeave,
    captureView,
    restoreView,
    beforeUnload,
  },
};
