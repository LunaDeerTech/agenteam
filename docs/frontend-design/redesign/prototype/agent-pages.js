import {
  esc,
  icon,
  showMenu,
  showDialog,
  closeDialog,
  dialogHeader,
  dialogFooter,
} from "./core.js";

const models = [
  { id: "model-project-chat", label: "Project Chat" },
  { id: "model-review-chat", label: "Review Chat" },
];
const runnerChoices = [
  { id: "runner-atlas", label: "Atlas Runner" },
  { id: "runner-research", label: "Research Runner" },
];
const secretChoices = [
  { id: "secret-research", label: "RESEARCH_API_KEY" },
  { id: "secret-repository", label: "REPOSITORY_TOKEN" },
];
const skillChoices = [
  {
    id: "skill-research",
    name: "Research",
    description: "整理来源、核对事实与形成研究结论。",
  },
  {
    id: "skill-review",
    name: "Review",
    description: "审查变更与记录需要跟进的问题。",
  },
  {
    id: "skill-writing",
    name: "Writing",
    description: "组织项目文档与交付说明。",
  },
];
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
  if (view === "memory" && agentID(ctx) !== "new") return "memory";
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
        reasoningEffort: null,
        approvalPolicy: "default",
        approvalModelID: "",
        injectAgentsMD: "yes",
        runnerMounts:
          index === 0
            ? [{ runner_id: runnerChoices[0].id, workspace: "architecture" }]
            : [],
        secretIDs: index === 1 ? [secretChoices[0].id] : [],
        allowedToolIDs: [],
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
        updatedAt: "2026-10-15T08:30:00Z",
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
    if (ctx.params.get("resource_extra_secret") === "1")
      agents[2].secretIDs.push("secret-outside-directory");
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
      listView: null,
      detailViews: {},
      returnFocus: "",
      skillsOpen: false,
      expandedHistory: "",
      pending:
        ctx.scenario === "unknown"
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
      sections: new Set(["runners", "secrets", "tools", "skills"]),
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
  if (agentID(ctx) !== "new") items.push(["memory", "记忆"]);
  return `<div class="agent-tabs" role="tablist" aria-label="Agent 配置">${items.map(([key, title]) => `<a id="agent-tab-${key}" role="tab" class="agent-tab" data-agent-tab="${key}" href="${esc(href(ctx, { agent: agentID(ctx), ...(key === "basic" ? {} : { view: key }) }))}" aria-controls="agent-tab-panel" aria-selected="${tabFor(ctx) === key}" tabindex="${tabFor(ctx) === key ? "0" : "-1"}">${title}</a>`).join("")}</div>`;
}
function gate(ctx, noun) {
  if (ctx.scenario === "loading")
    return `<section class="agent-state" aria-busy="true" aria-label="正在读取${noun}"><div class="skeleton wide"></div><div class="skeleton"></div><div class="skeleton wide"></div><p>正在读取${noun}…</p></section>`;
  if (ctx.scenario === "error")
    return `<section class="agent-state">${icon("refresh", 25)}<h2>暂时无法读取${noun}</h2><p>重新读取后继续。</p>${button("重新读取", "retry")}</section>`;
  return "";
}
function directory(ctx, state) {
  const agents = ctx.scenario === "empty" ? [] : state.agents;
  return `<section class="agent-page">${heading("Agent", "管理项目的 Agent 配置与能力。", button("创建 Agent", "create", "primary", readOnly(ctx) || !!state.pending))}${readOnly(ctx) ? notice("当前项目为只读", "你可以查看 Agent 配置和记忆。") : ""}${state.pending ? notice("有一个操作结果待确认", "请回到原 Agent 查询结果后继续。", button("查看原 Agent", "original")) : ""}<div class="agent-list-toolbar"><span>已加载 ${agents.length} 个 Agent</span><span class="agent-date-heading">更新时间</span><button type="button" class="icon-button" data-agent-action="refresh" aria-label="刷新 Agent 目录">${icon("refresh", 13)}</button></div>${gate(ctx, "Agent 目录") || (agents.length ? `<div class="agent-directory" role="list" aria-label="Agent 目录">${agents.map((agent) => `<article class="agent-directory-row" role="listitem"><a class="agent-directory-link" data-agent-open="${agent.id}" href="${esc(href(ctx, { agent: agent.id }))}"><span class="agent-mark" style="--agent-color:${agent.tagColor || "#8a8e87"}">${icon("agents", 17)}</span><span class="agent-directory-copy"><span class="agent-directory-name">${esc(label(agent))}<span class="agent-technical-name">${esc(agent.name)}</span></span><span class="agent-directory-description">${esc(agent.description)}</span></span></a><time datetime="${agent.updatedAt}">10 月 15 日</time><a class="agent-row-arrow" data-agent-open="${agent.id}" href="${esc(href(ctx, { agent: agent.id }))}" aria-label="查看 ${esc(label(agent))}">${icon("arrow", 14)}</a></article>`).join("")}</div>` : `<section class="agent-state">${icon("agents", 26)}<h2>项目还没有 Agent</h2><p>创建一个 Agent，配置它的指令与模型。</p>${button("创建 Agent", "create", "primary", readOnly(ctx))}</section>`)}<p class="agent-feedback" id="agent-list-feedback" role="status">${esc(state.notice)}</p></section>`;
}
function inputField(key, title, value, options = {}) {
  const id = `agent-${key}`;
  const hint = options.hint
    ? `<p id="${id}-hint" class="agent-field-hint">${esc(options.hint)}</p>`
    : "";
  const attrs = `id="${id}" name="${key}" data-agent-field="${key}" ${options.disabled ? "disabled" : ""} ${options.required ? "required" : ""} ${hint ? `aria-describedby="${id}-hint"` : ""}`;
  const control = options.choices
    ? `<select ${attrs}>${options.choices.map((choice) => `<option value="${esc(choice.id)}" ${String(value) === choice.id ? "selected" : ""}>${esc(choice.label)}</option>`).join("")}</select>`
    : options.rows
      ? `<textarea ${attrs} rows="${options.rows}">${esc(value)}</textarea>`
      : `<input ${attrs} value="${esc(value)}" ${options.max ? `maxlength="${options.max}"` : ""} ${key === "name" ? 'autocomplete="off" autocapitalize="none" spellcheck="false"' : ""}>`;
  return `<div class="agent-field"><label for="${id}">${esc(title)}${options.required ? '<span aria-hidden="true"> *</span>' : ""}</label>${control}${hint}<p id="${id}-error" class="agent-field-error" hidden></p></div>`;
}
function disclosure(key, title, subtitle, content, edit) {
  return `<details class="agent-resource" data-agent-section="${key}" ${edit.sections.has(key) ? "open" : ""}><summary><span><strong>${title}</strong><span>${esc(subtitle)}</span></span>${icon("chevron", 13)}</summary><div class="agent-resource-content">${content}</div></details>`;
}
function configuration(ctx, state, current) {
  const edit = draftFor(ctx, state),
    value = edit.value,
    creating = agentID(ctx) === "new";
  const pending = state.pending?.agent === agentID(ctx),
    locked = readOnly(ctx) || !!state.pending || !!state.busy || edit.conflict;
  const opt = { disabled: locked };
  const pendingNotice = !pending
    ? ""
    : state.pending.kind === "resource"
      ? notice(
          "资源操作结果待确认",
          "请回到原资源操作查询结果，确认前不能继续修改配置。",
          button("查看原资源操作", "original"),
        )
      : state.pending.kind === "memory"
        ? notice(
            "删除结果待确认",
            "原记忆记录与修订仍保留，请回到原删除操作查询结果。",
            button("查看原删除操作", "original"),
          )
        : notice(
            state.pending.kind === "creation"
              ? "创建结果待确认"
              : "保存结果待确认",
            "原配置与请求已保留。确认前请勿重复提交。",
            button(
              state.pending.kind === "creation"
                ? "查询原创建结果"
                : "查询原保存结果",
              "lookup",
            ),
          );
  const resourceValue = creating ? value : current;
  const modelOpts = [{ id: "", label: "选择聊天模型" }, ...models];
  const mounts = resourceValue.runnerMounts
    .map(
      (mount, index) =>
        `<div class="agent-mount-row"><div class="agent-field"><label for="agent-runner-${index}">Runner</label><select id="agent-runner-${index}" data-mount-index="${index}" data-mount-key="runner_id" ${locked ? "disabled" : ""}><option value="">选择 Runner</option>${runnerChoices.map((choice) => `<option value="${choice.id}" ${choice.id === mount.runner_id ? "selected" : ""}>${choice.label}</option>`).join("")}</select></div><div class="agent-field"><label for="agent-workspace-${index}">资源目录名</label><input id="agent-workspace-${index}" data-mount-index="${index}" data-mount-key="workspace" value="${esc(mount.workspace)}" placeholder="例如 workspace-1" ${locked ? "disabled" : ""}></div>${button("移除", `remove-mount:${index}`, "quiet", locked)}</div>`,
    )
    .join("");
  const runnerContent = `<p class="agent-section-note">选择 Runner 与单段资源目录名。移除配置不会删除目录。</p>${mounts || '<p class="agent-muted-line">未配置 Runner。</p>'}${button("添加资源目录", "add-mount", "", locked || resourceValue.runnerMounts.length >= 128)}`;
  const allSecretChoices = [
    ...secretChoices,
    ...resourceValue.secretIDs
      .filter((id) => !secretChoices.some((choice) => choice.id === id))
      .map((id) => ({ id, label: `已配置 Secret Variable · ${id}` })),
  ];
  const secrets = `<p class="agent-section-note">仅选择此 Agent 可使用的 Secret Variable，不显示凭据内容。</p>${allSecretChoices.map((secret) => `<label class="agent-check-row"><input type="checkbox" data-secret="${secret.id}" ${resourceValue.secretIDs.includes(secret.id) ? "checked" : ""} ${locked ? "disabled" : ""}><span>${secret.label}</span></label>`).join("")}`;
  const skills = `<p class="agent-section-note">分配项目技能；禁用保留分配记录，撤销不会删除技能包。</p>${current?.skills.length ? current.skills.map((skill) => `<div class="agent-skill-row"><div><strong>${esc(skillChoices.find((choice) => choice.id === skill.skillID)?.name || skill.skillID)}</strong><span>${skill.enabled ? "已启用" : "已禁用"}</span></div>${button(skill.enabled ? "禁用" : "启用", `skill-${skill.enabled ? "disable" : "enable"}:${skill.id}`, "quiet", locked || edit.touched.size > 0)}${button("撤销分配", `skill-remove:${skill.id}`, "quiet", locked || edit.touched.size > 0)}</div>`).join("") : '<p class="agent-muted-line">尚未分配项目技能。</p>'}<div class="agent-capability-actions">${button("分配项目技能", "skill-assign", "", locked || edit.touched.size > 0)}${edit.touched.size ? "<span>保存或取消配置修改后管理技能。</span>" : ""}</div>`;
  const basicFields = `<section class="agent-form-section"><header><h2>基本信息</h2></header><div class="agent-field-pair">${inputField("name", "Agent 名称", value.name, { ...opt, required: true, max: 32, hint: "3–32 位字母、数字或连字符。" })}${inputField("displayName", "显示名称", value.displayName, { ...opt, max: 256, hint: "可选，未填写时使用 Agent 名称。" })}</div>${inputField("description", "描述", value.description, { ...opt, rows: 2 })}<details class="agent-appearance" data-agent-section="appearance" ${edit.sections.has("appearance") ? "open" : ""}><summary>标签颜色</summary>${inputField("tagColor", "标签颜色", value.tagColor, { ...opt, hint: "可选，使用小写 #rrggbb。清空后恢复默认。" })}</details></section><section class="agent-form-section"><header><h2>指令</h2><p>定义这个 Agent 处理任务时遵循的方式。</p></header>${inputField("instructions", "Agent 指令", value.instructions, { ...opt, rows: 5 })}<label class="agent-check-row"><input type="checkbox" data-agent-inject ${value.injectAgentsMD === "yes" ? "checked" : ""} ${locked ? "disabled" : ""}><span>注入项目 AGENTS.md</span></label></section><section class="agent-form-section"><header><h2>模型与审批</h2><p>保存后的配置用于后续执行。</p></header><div class="agent-field-pair">${inputField("modelID", "主模型", value.modelID, { disabled: locked || value.reasoningEffort !== null, required: true, choices: modelOpts })}${inputField(
    "approvalPolicy",
    "审批策略",
    value.approvalPolicy,
    {
      disabled: locked || value.reasoningEffort !== null,
      choices: [
        { id: "default", label: "默认审批" },
        { id: "auto", label: "模型自动审批" },
        { id: "allow", label: "允许工具调用" },
      ],
    },
  )}</div><div id="agent-approval-model" ${value.approvalPolicy !== "auto" ? "hidden" : ""}>${inputField("approvalModelID", "审批模型", value.approvalModelID, { ...opt, choices: modelOpts })}</div>${!creating ? `<div class="agent-readonly-property"><span>推理强度</span><span>${esc(value.reasoningEffort || "使用模型默认值")}</span></div>` : ""}${value.reasoningEffort !== null ? '<p class="agent-section-note">当前配置的推理强度不支持在此编辑，主模型与审批配置保持原值。</p>' : ""}</section>`;
  const resourceFields = `<section class="agent-form-section agent-resource-groups">${disclosure("runners", "Runner 与资源目录", resourceValue.runnerMounts.length ? `${resourceValue.runnerMounts.length} 项资源目录` : "未配置", runnerContent, edit)}${disclosure("secrets", "Secret Variable 白名单", resourceValue.secretIDs.length ? `${resourceValue.secretIDs.length} 项已选择` : "未选择", secrets, edit)}${!creating ? disclosure("tools", "Tool 引用", resourceValue.allowedToolIDs.length ? `${resourceValue.allowedToolIDs.length} 项引用` : "无额外工具引用", `<p class="agent-section-note">${resourceValue.allowedToolIDs.length ? resourceValue.allowedToolIDs.map(esc).join(" · ") : "当前配置没有额外 Tool 引用。"}</p>`, edit) : ""}${!creating ? disclosure("skills", "项目技能", "管理此 Agent 的技能分配", skills, edit) : `<div class="agent-initialization"><h3>内置初始化</h3><label class="agent-check-row"><input type="checkbox" data-agent-init="addSkills" ${value.addSkills ? "checked" : ""} ${locked ? "disabled" : ""}>添加默认 Skills</label><label class="agent-check-row"><input type="checkbox" data-agent-init="installSkill" ${value.installSkill ? "checked" : ""} ${locked ? "disabled" : ""}>启用 Install Skill 工具</label></div>`}</section>`;
  return `<div class="agent-detail-content" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-${tabFor(ctx)}">${readOnly(ctx) ? notice("当前项目为只读", "配置可以阅读，修改暂不可用。") : ""}${pendingNotice}${edit.conflict ? notice("配置已有新版本", "原草稿与编辑版本已保留。取消编辑并重新读取后再保存。", button("取消编辑并重读", "conflict-read", "", !!state.busy || !!state.pending)) : ""}${gate(ctx, "Agent 配置") || `<form id="agent-configuration-form" novalidate aria-label="${creating ? "创建 Agent" : "编辑 Agent 配置"}">${tabFor(ctx) === "resources" ? resourceFields : basicFields}<div class="agent-savebar"><span id="agent-dirty-state">${edit.touched.size ? "有未保存的修改" : ""}</span><div>${button(creating ? "取消创建" : "取消修改", "cancel", "quiet", pending || !!state.busy)}<button type="submit" class="button primary" id="agent-save" ${locked || !edit.touched.size ? "disabled" : ""}>${creating ? "创建 Agent" : "保存配置"}</button></div></div><p class="agent-feedback" id="agent-form-feedback" role="status">${esc(edit.message)}</p></form>${!creating ? `<details class="agent-records"><summary>Agent 标识与记录</summary><dl><dt>Agent ID</dt><dd>${esc(current.id)}</dd><dt>配置版本</dt><dd>${esc(current.version)}</dd><dt>创建时间</dt><dd>2026 年 10 月 12 日</dd></dl></details>` : ""}`}</div>`;
}
function resourceState(state, id) {
  return (state.resources[id] ||= {
    mount: null,
    index: -1,
    status: "",
    message: "",
    failed: null,
  });
}
function records(current) {
  return `<details class="agent-records"><summary>Agent 标识与记录</summary><dl><dt>Agent ID</dt><dd>${esc(current.id)}</dd><dt>配置版本</dt><dd>${esc(current.version)}</dd><dt>创建时间</dt><dd>2026 年 10 月 12 日</dd></dl></details>`;
}
function resources(ctx, state, current) {
  const edit = draftFor(ctx, state),
    resource = resourceState(state, current.id);
  const locked =
    readOnly(ctx) || !!state.pending || !!state.busy || edit.conflict;
  if (
    state.pending?.kind === "resource" &&
    state.pending.agent === current.id &&
    !state.pending.original
  ) {
    state.pending.original = {
      project: scopeFor(ctx),
      agent: current.id,
      field: "secretIDs",
      payload: [...new Set([...current.secretIDs, secretChoices[0].id])],
      expectedVersion: current.version,
      key: crypto.randomUUID(),
      label: "Secret 白名单",
    };
    resource.status = "unknown";
    resource.message = "Secret 白名单的保存结果待确认，原操作已保留。";
  }
  const mountRows = current.runnerMounts
    .map(
      (mount, index) =>
        `<div class="agent-saved-mount"><div><strong>${esc(runnerChoices.find((runner) => runner.id === mount.runner_id)?.label || mount.runner_id)}</strong><span>${esc(mount.workspace)}</span></div><div>${button("编辑", `edit-mount:${index}`, "quiet", locked)}${button("移除", `remove-mount:${index}`, "quiet", locked)}</div></div>`,
    )
    .join("");
  const mountEditor = resource.mount
    ? `<form id="agent-resource-editor" novalidate class="agent-resource-editor"><h3>${resource.index < 0 ? "添加资源目录" : "编辑资源目录"}</h3><div class="agent-field-pair"><div class="agent-field"><label for="agent-action-runner">Runner</label><select id="agent-action-runner" data-resource-field="runner_id" ${locked ? "disabled" : ""}><option value="">选择 Runner</option>${runnerChoices.map((runner) => `<option value="${runner.id}" ${runner.id === resource.mount.runner_id ? "selected" : ""}>${runner.label}</option>`).join("")}</select></div><div class="agent-field"><label for="agent-action-workspace">资源目录名</label><input id="agent-action-workspace" data-resource-field="workspace" value="${esc(resource.mount.workspace)}" ${locked ? "disabled" : ""} placeholder="例如 workspace-1"></div></div><div class="agent-resource-editor-actions">${button("取消", "cancel-mount", "quiet", !!state.busy || !!state.pending)}<button type="submit" class="button primary" data-agent-action="confirm-mount" ${locked ? "disabled" : ""}>确认保存</button></div></form>`
    : button(
        "添加资源目录",
        "add-mount",
        "",
        locked || current.runnerMounts.length >= 128,
      );
  const allSecrets = [
    ...secretChoices,
    ...current.secretIDs
      .filter((id) => !secretChoices.some((choice) => choice.id === id))
      .map((id) => ({ id, label: `已配置 Secret Variable · ${id}` })),
  ];
  const skills = `<p class="agent-section-note">分配项目技能；禁用保留分配记录，撤销不会删除技能包。</p>${current.skills.length ? current.skills.map((skill) => `<div class="agent-skill-row"><div><strong>${esc(skillChoices.find((choice) => choice.id === skill.skillID)?.name || skill.skillID)}</strong><span>${skill.enabled ? "已启用" : "已禁用"}</span></div>${button(skill.enabled ? "禁用" : "启用", `skill-${skill.enabled ? "disable" : "enable"}:${skill.id}`, "quiet", locked)}${button("撤销分配", `skill-remove:${skill.id}`, "quiet", locked)}</div>`).join("") : '<p class="agent-muted-line">尚未分配项目技能。</p>'}${button("分配项目技能", "skill-assign", "", locked)}`;
  const feedbackActions =
    resource.status === "unknown"
      ? button("查询原资源操作", "lookup")
      : resource.failed && !edit.conflict
        ? button("重试此操作", "resource-retry", "", locked)
        : "";
  return `<div class="agent-detail-content" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-resources">${readOnly(ctx) ? notice("当前项目为只读", "资源可以阅读，修改暂不可用。") : ""}${edit.conflict ? notice("配置已有新版本", "基本配置草稿已保留。请取消编辑并重新读取后再修改。", button("取消编辑并重读", "conflict-read", "", !!state.busy || !!state.pending)) : ""}<div id="agent-resource-feedback" class="agent-resource-feedback" role="status" ${resource.message ? "" : "hidden"} data-status="${resource.status}"><span>${esc(resource.message)}</span>${feedbackActions}</div>${gate(ctx, "Agent 资源") || `<section class="agent-resource-groups">${disclosure("runners", "Runner 与资源目录", current.runnerMounts.length ? `${current.runnerMounts.length} 项资源目录` : "未配置", `<p class="agent-section-note">每项资源目录确认后保存。移除配置不会删除目录。</p>${mountRows}${mountEditor}`, edit)}${disclosure("secrets", "Secret Variable 白名单", current.secretIDs.length ? `${current.secretIDs.length} 项已选择` : "未选择", `<p class="agent-section-note">选择后保存，仅授权此 Agent 使用，不显示凭据内容。</p>${allSecrets.map((secret) => `<label class="agent-check-row"><input type="checkbox" data-secret="${secret.id}" ${current.secretIDs.includes(secret.id) ? "checked" : ""} ${locked ? "disabled" : ""}><span>${esc(secret.label)}</span></label>`).join("")}`, edit)}${disclosure("tools", "Tool 引用", "只读配置引用", `<p class="agent-section-note">${current.allowedToolIDs.length ? current.allowedToolIDs.map(esc).join(" · ") : "当前配置没有额外 Tool 引用。"}</p>`, edit)}${disclosure("skills", "项目技能", "独立管理技能分配", skills, edit)}</section>${records(current)}`}</div>`;
}
function resourceAction(ctx, action) {
  const state = local(ctx),
    current = state.agents.find((agent) => agent.id === agentID(ctx));
  const resource = resourceState(state, current.id),
    edit = draftFor(ctx, state);
  if (readOnly(ctx) || state.pending || state.busy || edit.conflict) return;
  if (action === "resource-retry" && resource.failed) {
    const failed = resource.failed;
    return submitResource(ctx, failed.field, failed.payload, failed.label);
  }
  if (action === "cancel-mount") {
    resource.mount = null;
    return refresh(ctx, '[data-agent-action="add-mount"]');
  }
  if (action === "add-mount" || action.startsWith("edit-mount:")) {
    resource.index = action === "add-mount" ? -1 : Number(action.split(":")[1]);
    resource.mount =
      resource.index < 0
        ? { runner_id: "", workspace: "" }
        : copy(current.runnerMounts[resource.index]);
    return refresh(ctx, '[data-select-for="agent-action-runner"]');
  }
  if (action.startsWith("remove-mount:")) {
    const mounts = copy(current.runnerMounts);
    mounts.splice(Number(action.split(":")[1]), 1);
    return submitResource(ctx, "runnerMounts", mounts, "资源目录");
  }
  if (action === "confirm-mount" && resource.mount) {
    const mounts = copy(current.runnerMounts);
    if (resource.index < 0) mounts.push(copy(resource.mount));
    else mounts[resource.index] = copy(resource.mount);
    const error = validation({
      ...basicSnapshot(current),
      runnerMounts: mounts,
    });
    if (error) {
      resource.status = "error";
      resource.message = error[1];
      return refresh(
        ctx,
        resource.mount.runner_id
          ? "#agent-action-workspace"
          : '[data-select-for="agent-action-runner"]',
      );
    }
    return submitResource(ctx, "runnerMounts", mounts, "资源目录");
  }
}
function submitResource(ctx, field, payload, labelText) {
  const state = local(ctx),
    current = state.agents.find((agent) => agent.id === agentID(ctx));
  const edit = draftFor(ctx, state),
    resource = resourceState(state, current.id);
  if (readOnly(ctx) || state.pending || state.busy || edit.conflict) return;
  const command = {
    project: scopeFor(ctx),
    agent: current.id,
    field,
    payload: copy(payload),
    expectedVersion: current.version,
    key: crypto.randomUUID(),
    label: labelText,
  };
  state.busy = command;
  resource.failed = null;
  resource.status = "pending";
  resource.message = `正在保存${labelText}…`;
  refresh(ctx);
  setTimeout(() => {
    if (state.busy !== command) return;
    state.busy = null;
    const outcome = ctx.params.get("resource_result") || "success";
    if (outcome === "unknown") {
      state.pending = {
        kind: "resource",
        agent: current.id,
        original: command,
        observation: "",
      };
      resource.status = "unknown";
      resource.message = `${labelText}的保存结果待确认，原操作已保留。`;
    } else if (outcome === "error") {
      resource.status = "error";
      resource.message = `${labelText}未保存，请重试此操作。`;
      resource.failed = command;
    } else if (
      outcome === "conflict" ||
      current.version !== command.expectedVersion
    ) {
      if (outcome === "conflict") {
        current.description += "（已更新协作范围）";
        current.version = String(Number(current.version) + 1);
      }
      edit.conflict = true;
      resource.status = "error";
      resource.message =
        "配置已有新版本，此次资源操作未保存。基本配置草稿已保留。";
      resource.failed = command;
    } else {
      const changed = !same(current[field], command.payload);
      const next = copy(current);
      next[field] = copy(command.payload);
      next.version = String(
        Number(current.version) +
          (changed ? 1 : 0) +
          (outcome === "version_jump" ? 1 : 0),
      );
      // Validate the receipt before publishing any canonical value. Skills then uses the configuration reread to verify the basic baseline.
      const receiptVersion = next.version;
      const expectedReceiptVersion = String(
        Number(command.expectedVersion) + (changed ? 1 : 0),
      );
      if (receiptVersion !== expectedReceiptVersion) {
        state.pending = {
          kind: "resource",
          agent: current.id,
          original: command,
          observation: "invalid_receipt",
        };
        resource.status = "unknown";
        resource.message =
          "资源操作返回的版本需要核对。已保存配置和基本配置草稿均已保留，请查询原操作。";
      } else {
        const verified =
          edit.baseVersion === command.expectedVersion &&
          same(edit.baseSnapshot, basicSnapshot(next));
        Object.assign(current, next);
        if (verified) {
          edit.baseVersion = receiptVersion;
          edit.baseSnapshot = copy(basicSnapshot(current));
        } else edit.conflict = true;
        resource.status = verified ? "success" : "error";
        resource.message = verified
          ? changed
            ? `${labelText}已保存。`
            : `${labelText}未改变。`
          : "资源配置已更新，但基本配置已有其他变化。草稿已保留，请核对后再保存。";
        if (field === "runnerMounts") resource.mount = null;
      }
    }
    const here = new URLSearchParams(location.search);
    if (
      scopeFor({ params: here }) !== command.project ||
      (here.get("fixture") || "design") !== ctx.fixture ||
      (here.get("scenario") || "normal") !== ctx.scenario
    )
      return;
    if (
      here.get("page") === "agents" &&
      here.get("agent") === current.id &&
      here.get("view") === "resources"
    ) {
      const focus =
        field === "runnerMounts"
          ? resource.mount
            ? "#agent-action-workspace"
            : '[data-agent-action="add-mount"]'
          : field === "skills"
            ? '[data-agent-action="skill-assign"]'
            : `[data-secret="${command.payload.find((id) => !secretChoices.some((choice) => choice.id === id)) || secretChoices[0].id}"]`;
      refresh(ctx, focus);
    } else if (
      here.get("page") === "agents" &&
      (!here.get("agent") || here.get("agent") === current.id)
    )
      ctx.render();
  }, 400);
}
function memory(ctx, state, current) {
  const records = ctx.scenario === "empty" ? [] : current.memories;
  const pending =
    state.pending?.agent === current.id && state.pending.kind === "memory";
  if (pending && !state.pending.original && current.memories[0])
    state.pending.original = {
      agentID: current.id,
      recordID: current.memories[0].id,
      revision: current.memories[0].revision,
    };
  return `<div class="agent-detail-content" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-memory"><div class="agent-memory-toolbar"><p class="agent-memory-intro">此 Agent 保留的事实、经验与流程。</p>${button("刷新记忆", "memory-refresh")}</div>${pending ? notice("删除结果待确认", "原记录与修订仍保留，确认前不能再次删除。", button("查询原删除结果", "lookup")) : ""}${gate(ctx, "Agent 记忆") || (records.length ? `<div class="agent-memory-list">${records.map((record) => `<article class="agent-memory-record"><header><strong>${record.type}</strong><span>修订 ${record.revision}</span><time datetime="${record.date}">10 月 15 日</time></header><p>${esc(record.text)}</p><div class="agent-memory-tags">${esc(record.tags.join(" · "))}</div><div class="agent-memory-actions">${button(state.expandedHistory === record.id ? "收起历史" : "查看历史", `memory-history:${record.id}`, "quiet")}${button("删除记忆", `memory-delete:${record.id}`, "quiet", readOnly(ctx) || !!state.pending)}</div>${state.expandedHistory === record.id ? `<section class="agent-memory-history"><h2>修订历史</h2><strong>修订 ${record.revision} · 当前</strong><p>${esc(record.text)}</p>${record.old ? `<strong>修订 1 · 已替代</strong><p>${esc(record.old)}</p>` : ""}<details><summary>来源与时间</summary><dl><dt>来源执行</dt><dd>${esc(record.source)}</dd><dt>保留时间</dt><dd>${record.date}</dd></dl></details></section>` : ""}</article>`).join("")}</div>` : `<section class="agent-state">${icon("file", 24)}<h2>此 Agent 暂无当前记忆</h2><p>执行中明确保留的内容会出现在这里。</p></section>`)}<p class="agent-feedback" id="agent-memory-feedback" role="status">${esc(state.notice)}</p></div>`;
}
function render(ctx) {
  const state = local(ctx),
    id = agentID(ctx);
  if (!id) return directory(ctx, state);
  const current = state.agents.find((agent) => agent.id === id);
  if ((id !== "new" && !current) || ctx.scenario === "missing")
    return `<section class="agent-page">${heading("Agent 不可用", "此 Agent 不存在，或当前身份无法访问。", "", backLink(ctx, "Agent"))}<section class="agent-state">${button("返回 Agent 列表", "list")}</section></section>`;
  const creating = id === "new";
  return `<section class="agent-page agent-object-page">${heading(creating ? "创建 Agent" : label(current), creating ? "设置这个 Agent 的身份、指令与执行配置。" : current.description, "", backLink(ctx, "Agent"))}${tabs(ctx)}${isMemory(ctx) && current ? memory(ctx, state, current) : tabFor(ctx) === "resources" && current ? resources(ctx, state, current) : configuration(ctx, state, current)}</section>`;
}
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
  if (!models.some((model) => model.id === value.modelID))
    return ["modelID", "请选择主模型。"];
  if (
    value.approvalPolicy === "auto" &&
    !models.some((model) => model.id === value.approvalModelID)
  )
    return ["approvalModelID", "模型自动审批需要选择审批模型。"];
  if (
    value.runnerMounts &&
    (value.runnerMounts.length > 128 ||
      new Set(
        value.runnerMounts.map(
          (mount) => `${mount.runner_id}/${mount.workspace}`,
        ),
      ).size !== value.runnerMounts.length ||
      value.runnerMounts.some(
        (mount) =>
          !mount.runner_id ||
          !/^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$/.test(mount.workspace) ||
          mount.workspace.endsWith(".") ||
          /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$/i.test(
            mount.workspace.split(".")[0],
          ),
      ))
  )
    return [
      "runnerMounts",
      "请为每项资源配置选择 Runner，并填写单段英文目录名。",
    ];
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
        edit.value[field.dataset.agentField] = field.value;
        markDirty(ctx, field.dataset.agentField);
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
  root.querySelectorAll("[data-secret]").forEach(
    (field) =>
      (field.onchange = () => {
        if (id !== "new") {
          const selected = new Set(current.secretIDs);
          if (field.checked) selected.add(field.dataset.secret);
          else selected.delete(field.dataset.secret);
          return submitResource(
            ctx,
            "secretIDs",
            [...selected],
            "Secret 白名单",
          );
        }
        const edit = draftFor(ctx, state);
        edit.value.secretIDs = [
          ...root.querySelectorAll("[data-secret]:checked"),
        ].map((el) => el.dataset.secret);
        markDirty(ctx, "secretIDs");
      }),
  );
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
  root
    .querySelector("#agent-configuration-form")
    ?.addEventListener("submit", (event) => {
      event.preventDefault();
      const edit = draftFor(ctx, state);
      if (
        readOnly(ctx) ||
        state.pending ||
        state.busy ||
        edit.conflict ||
        !edit.touched.size
      )
        return;
      if (
        id !== "new" &&
        (current.version !== edit.baseVersion ||
          !same(basicSnapshot(current), edit.baseSnapshot))
      ) {
        edit.conflict = true;
        return refresh(ctx, "#agent-page-title");
      }
      clearFieldErrors();
      const error = validation(edit.value);
      if (error) {
        edit.message = error[1];
        edit.error = error;
        const targetTab = error[0] === "runnerMounts" ? "resources" : "basic";
        if (targetTab !== tabFor(ctx)) {
          ctx.navigate("agents", {
            agent: id,
            ...(targetTab === "basic" ? {} : { view: targetTab }),
          });
        } else showFieldError(ctx, error);
        return;
      }
      const value = copy(edit.value);
      value.version = String(Number(edit.baseVersion) + 1);
      value.updatedAt = new Date().toISOString();
      if (id === "new") {
        value.id = `agent-${crypto.randomUUID()}`;
        if (
          state.agents.some(
            (agent) => agent.name.toLowerCase() === value.name.toLowerCase(),
          )
        ) {
          root.querySelector("#agent-form-feedback").textContent =
            "此 Agent 名称已被使用。";
          return;
        }
        value.memories = [];
        value.createdAt = value.updatedAt;
        state.agents.push(value);
        delete state.edits.new;
        state.notice = "";
        ctx.navigate("agents", { agent: value.id });
      } else {
        if (
          state.agents.some(
            (agent) =>
              agent.id !== id &&
              agent.name.toLowerCase() === value.name.toLowerCase(),
          )
        ) {
          root.querySelector("#agent-form-feedback").textContent =
            "此 Agent 名称已被使用。";
          return;
        }
        for (const field of edit.touched) current[field] = copy(value[field]);
        current.version = value.version;
        current.updatedAt = value.updatedAt;
        edit.value = copy(basicSnapshot(current));
        edit.baseSnapshot = copy(basicSnapshot(current));
        edit.baseVersion = current.version;
        edit.touched.clear();
        edit.message = "配置已保存。";
        refresh(ctx, "#agent-save");
      }
    });
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
  if (action === "original")
    return ctx.navigate("agents", {
      agent: state.pending.agent,
      ...(state.pending.kind === "memory"
        ? { view: "memory" }
        : state.pending.kind === "resource"
          ? { view: "resources" }
          : {}),
    });
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
  if (action === "lookup") {
    if (state.pending) state.pending.observation = "not_observed";
    const resource = state.resources[state.pending?.agent];
    if (state.pending?.kind === "resource" && resource) {
      resource.message = "尚未观察到可确认结果，原资源操作继续保留。";
      return refresh(ctx, '[data-agent-action="lookup"]');
    }
    const node = document.querySelector(".agent-notice p");
    if (node) node.textContent = "尚未观察到可确认结果，原请求继续保留。";
    return;
  }
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
      else
        refresh(
          ctx,
          tabFor(ctx) === "resources" ? "#agent-tab-resources" : "#agent-name",
        );
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
    id !== "new" &&
    (action === "add-mount" ||
      action === "cancel-mount" ||
      action === "confirm-mount" ||
      action === "resource-retry" ||
      action.startsWith("edit-mount:") ||
      action.startsWith("remove-mount:"))
  ) {
    return resourceAction(ctx, action);
  }
  if (action === "add-mount") {
    edit.value.runnerMounts.push({ runner_id: "", workspace: "" });
    markDirty(ctx, "runnerMounts");
    return refresh(ctx, `#agent-runner-${edit.value.runnerMounts.length - 1}`);
  }
  if (action.startsWith("remove-mount:")) {
    edit.value.runnerMounts.splice(Number(action.split(":")[1]), 1);
    markDirty(ctx, "runnerMounts");
    return refresh(ctx, '[data-agent-action="add-mount"]');
  }
  if (action.startsWith("skill-")) return skillAction(ctx, action, trigger);
  if (action.startsWith("memory-history:")) {
    const memoryID = action.split(":")[1];
    state.expandedHistory = state.expandedHistory === memoryID ? "" : memoryID;
    return refresh(ctx, `[data-agent-action="${action}"]`);
  }
  if (action.startsWith("memory-delete:")) {
    const record = current.memories.find(
      (memory) => memory.id === action.split(":")[1],
    );
    if (!record || readOnly(ctx) || state.pending) return;
    showDialog(
      `${dialogHeader("删除这条记忆？")}<div class="dialog-body"><p>${esc(record.text)}</p><p class="muted small">后续检索不再返回这条记忆，修订历史保留。</p></div>${dialogFooter('<button class="button" data-dialog-close>保留记忆</button><button class="button danger" data-memory-confirm>确认删除</button>')}`,
      (dialog) =>
        (dialog.querySelector("[data-memory-confirm]").onclick = () => {
          state.deletedMemories[record.id] = {
            ...copy(record),
            status: "deleted",
          };
          current.memories = current.memories.filter(
            (memory) => memory.id !== record.id,
          );
          closeDialog();
          state.notice = "记忆已删除，修订历史保留。";
          refresh(ctx, "#agent-page-title");
        }),
    );
  }
}
function skillAction(ctx, action, trigger) {
  const state = local(ctx),
    current = state.agents.find((agent) => agent.id === agentID(ctx)),
    edit = draftFor(ctx, state);
  if (readOnly(ctx) || state.pending || state.busy || edit.conflict) return;
  if (action === "skill-assign")
    return showMenu(
      trigger,
      skillChoices
        .filter(
          (skill) => !current.skills.some((item) => item.skillID === skill.id),
        )
        .map((skill) => ({ value: skill.id, label: skill.name })),
      (value) => confirmSkill("assign", value),
      { label: "选择项目技能", width: 240 },
    );
  const [kind, item] = action.replace("skill-", "").split(":");
  confirmSkill(kind, item);
  function confirmSkill(kind, item) {
    const assignment = current.skills.find((skill) => skill.id === item),
      title =
        kind === "assign"
          ? "分配项目技能？"
          : kind === "remove"
            ? "撤销技能分配？"
            : kind === "disable"
              ? "禁用这个技能？"
              : "启用这个技能？";
    confirm(
      title,
      kind === "remove"
        ? "仅撤销此 Agent 的分配，项目技能包仍保留。"
        : "此操作修改当前 Agent 的技能分配，不会启动执行。",
      () => {
        const skills = copy(current.skills);
        if (kind === "assign")
          skills.push({
            id: `assignment-${item}`,
            skillID: item,
            enabled: true,
          });
        else if (kind === "remove")
          skills.splice(
            skills.findIndex((skill) => skill.id === item),
            1,
          );
        else
          skills.find((skill) => skill.id === item).enabled = kind === "enable";
        submitResource(ctx, "skills", skills, "项目技能");
      },
      "确认",
    );
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
  const pending = state.pending?.agent === id || state.busy?.agent === id;
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
    !!state.busy
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
