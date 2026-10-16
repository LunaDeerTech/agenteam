import {
  esc,
  icon,
  showDialog,
  closeDialog,
  dialogHeader,
  dialogFooter,
} from "./core.js";

// F0 uses synthetic records. These domains never write the Agent configuration
// or its draft/version; each command retains its own identity and revision.
const PAGE_SIZE = 3;
const HISTORY_SIZE = 2;
const copy = (value) => JSON.parse(JSON.stringify(value));
const nameOf = (agent) => agent.displayName || agent.name;
const types = { fact: "事实", experience: "经验", procedure: "流程" };
const importance = { low: "低", normal: "普通", high: "高" };
const sourceTypes = {
  execution: "执行", task: "任务", meeting: "会议",
  knowledge_document: "知识文档", memory: "记忆", tool_operation: "工具操作",
};
const action = (label, id, disabled = false, kind = "quiet") =>
  `<button type="button" class="button ${kind}" data-agent-domain-action="${esc(id)}" ${disabled ? "disabled" : ""}>${esc(label)}</button>`;
const stamp = (value) => {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "时间不可用" : new Intl.DateTimeFormat("zh-CN", {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
    hour12: false,
  }).format(date);
};
const time = (value) => `<time datetime="${esc(value)}" title="${esc(value)}">${stamp(value)}</time>`;
const status = (value) => `<span class="agent-domain-status ${value}">${({ active: "生效中", revoked: "已撤销", superseded: "已替代", deleted: "已删除" })[value] || esc(value)}</span>`;
const selected = (a, b) => a === b ? " selected" : "";
const filter = (id, title, value, options) => `<label class="agent-domain-filter" for="${id}"><span>${title}</span><select id="${id}" data-agent-domain-filter="${id}">${options.map(([key, label]) => `<option value="${esc(key)}"${selected(value, key)}>${esc(label)}</option>`).join("")}</select></label>`;
const locked = (ctx, state, bridge) => bridge.readOnly(ctx) || !!(state.pending || state.busy || state.domainPending || state.domainBusy);
const source = (record) => {
  const p = record.provenance;
  if (!p) return `<span class="agent-domain-source">来源信息未记录</span>`;
  const unavailable = p.availability === "deleted" ? "来源已删除，保留当时摘要" : p.availability === "unavailable" ? "来源暂不可访问" : "";
  return `<span class="agent-domain-source">${esc(sourceTypes[p.type] || "来源")}：${esc(p.label || "未记录标题")}${unavailable ? `<span class="agent-domain-source-note">${unavailable}</span>` : ""}</span>`;
};

function memorySeeds(current) {
  const records = (current.memories || []).map((item, index) => {
    if (item.history) return item;
    const p = { type: "execution", label: `${nameOf(current)} · ${index ? "交付复盘" : "项目约定整理"}`, id: item.source, availability: "available" };
    const date = item.updatedAt || item.date;
    const record = {
      ...item, type: item.type === "经验" ? "experience" : "fact",
      importance: index ? "normal" : "high", status: "active", updatedAt: date,
      provenance: p,
    };
    record.history = [{ revision: record.revision, text: record.text, status: "active", createdAt: date, provenance: copy(p) }];
    if (item.old) record.history.push({ revision: "1", text: item.old, status: "superseded", createdAt: "2026-10-12T09:20:00Z", provenance: { ...p, label: `${nameOf(current)} · 初始约定` } });
    return record;
  });
  // Newly created Agents have no runtime memory. Existing fixture Agents have
  // enough distinct records to exercise pagination and independently read history.
  if (records.length && /^agent-(architecture|researcher|reviewer)$/.test(current.id)) {
    const extra = [
      ["procedure", "交付前核对范围、来源和未验证事项，完成后记录下一步。", ["交付", "流程"], "normal", "task", "交付检查清单"],
      ["fact", "接口命名与版本边界以项目约定为准，历史决定仍保留其适用条件。", ["项目约定"], "low", "knowledge_document", "接口命名约定"],
      ["experience", "讨论中未达成一致的事项单独记录，并附上需要补充的依据。", ["协作", "复盘"], "normal", "meeting", "设计评审会议"],
    ];
    extra.forEach(([type, text, tags, weight, sourceType, title], index) => {
      const id = `${current.id}-memory-${index + 3}`;
      const createdAt = `2026-10-${13 - index}T10:25:00Z`;
      const p = { type: sourceType, label: title, id: `${id}-source`, availability: index === 1 ? "deleted" : index === 2 ? "unavailable" : "available" };
      records.push({ id, type, text, tags, importance: weight, revision: index === 0 ? "3" : "1", status: "active", updatedAt: createdAt, provenance: p,
        history: [{ revision: index === 0 ? "3" : "1", text, status: "active", createdAt, provenance: copy(p) }, ...(index === 0 ? [
          { revision: "2", text: "交付前核对范围和来源，完成后记录待验证事项。", status: "superseded", createdAt: "2026-10-12T11:30:00Z", provenance: { type: "execution", label: `${nameOf(current)} · 交付检查`, availability: "available" } },
          { revision: "1", text: "交付前核对范围和来源。", status: "superseded", createdAt: "2026-10-11T09:10:00Z", provenance: null },
        ] : [])] });
    });
  }
  return records;
}

function approvalSeeds(current, project) {
  if (!/^agent-(architecture|researcher|reviewer)$/.test(current.id)) return [];
  return [
    ["项目文档", "active", "2026-10-15T09:10:00Z"],
    ["接口说明", "active", "2026-10-14T14:30:00Z"],
    ["发布说明", "revoked", "2026-10-13T10:20:00Z"],
    ["研究资料", "active", "2026-10-12T08:45:00Z"],
  ].map(([group, state, createdAt], index) => ({
    id: `${current.id}-approval-${index + 1}`, agentID: current.id, project,
    // This is the architecture's illustrative Tool, not an invented production integration.
    toolID: "example.resource.update", toolName: "更新资源", toolSource: "示例工具",
    scope: { resource_group: group }, scopeSummary: `资源组「${group}」`, state, version: "1", createdAt,
    revokedAt: state === "revoked" ? "2026-10-14T16:05:00Z" : null,
    createdBy: "项目 Owner", revokedBy: state === "revoked" ? "项目 Owner" : null,
    sourceCallID: `${current.id}-call-${index + 1}`,
    sourceSummary: `更新「${group}」时批准后续同类调用`,
    executionSummary: `${nameOf(current)} · ${group}整理`,
    sourceAvailability: index === 2 ? "deleted" : "available",
  }));
}

function local(ctx, state, current, bridge) {
  state.domains ||= {};
  if (!state.domains[current.id]) {
    current.memories = memorySeeds(current);
    current.approvals ||= approvalSeeds(current, bridge.scopeFor(ctx));
    const read = (view) => ({ page: 0, loading: false, message: "", failure: null, readError: ctx.params.get(`${view}_read`) === "error" || (ctx.scenario === "error" && ctx.params.get("view") === view), readOnce: false });
    state.domains[current.id] = {
      memory: { ...read("memory"), type: "all", tag: "all", importance: "all", source: "all", status: "active", expanded: null, historyPages: {}, historyErrors: {}, historyLoaded: {} },
      audit: { ...read("audit"), state: "all", tool: "all", selected: null },
      initialUnknown: false,
    };
  }
  const domain = state.domains[current.id];
  const view = ctx.params.get("view");
  if (!domain.initialUnknown && ctx.scenario === "unknown" && ["memory", "audit"].includes(view) && !state.domainPending) {
    domain.initialUnknown = true;
    const target = view === "memory" ? current.memories.find((record) => record.status === "active") : current.approvals.find((record) => record.state === "active");
    if (target) state.domainPending = commandFor(ctx, current, view, target, bridge);
  }
  return domain;
}

function commandFor(ctx, current, view, target, bridge) {
  return {
    agent: current.id, view, kind: view === "memory" ? "memory-delete" : "approval-revoke",
    fixture: ctx.fixture, scenario: ctx.scenario, status: "unknown",
    original: { project: bridge.scopeFor(ctx), agent: current.id, targetID: target.id,
      expectedRevision: view === "memory" ? target.revision : undefined,
      expectedVersion: view === "audit" ? target.version : undefined,
      key: `f0-${view}-${crypto.randomUUID()}` },
  };
}

function inView(ctx, current, view, bridge) {
  const here = new URLSearchParams(location.search);
  return here.get("page") === "agents" && here.get("agent") === current.id &&
    here.get("view") === view && bridge.scopeFor({ ...ctx, params: here }) === bridge.scopeFor(ctx) &&
    (here.get("fixture") || "design") === ctx.fixture && (here.get("scenario") || "normal") === ctx.scenario;
}

function refresh(ctx, current, view, bridge, focus) {
  if (inView(ctx, current, view, bridge)) bridge.refresh(ctx, focus);
}

function pendingBanner(state, current, view) {
  const busy = state.domainBusy?.agent === current.id && state.domainBusy.view === view;
  const pending = state.domainPending?.agent === current.id && state.domainPending.view === view ? state.domainPending : null;
  if (busy) return `<div class="agent-domain-notice" role="status" aria-live="polite"><strong>${view === "memory" ? "正在删除记忆…" : "正在撤销审批…"}</strong><p>正在确认原操作，请稍候。</p></div>`;
  if (!pending) return "";
  const terminal = ["committed", "failed"].includes(pending.status);
  return `<div class="agent-domain-notice warning" role="status" aria-live="polite"><strong>${terminal ? pending.status === "committed" ? "原操作已确认完成，等待回读" : "原操作已确认失败，等待回读" : view === "memory" ? "删除结果待确认" : "撤销结果待确认"}</strong><p>${terminal ? "重新读取记录后恢复操作。" : "已保留原记录、版本和请求，确认前不能重复提交。"}${pending.observation === "not_observed" ? " 暂未查到终态，请稍后继续查询。" : pending.observation === "invalid_receipt" ? " 记录与原请求不一致，继续保留待确认状态。" : ""}</p>${action(terminal ? view === "memory" ? "重新读取记忆" : "重新读取审批" : view === "memory" ? "查询原删除结果" : "查询原撤销结果", terminal ? `${view}-reconcile` : `${view}-lookup`, !!pending.checking, "")}</div>`;
}

function feedback(domain, view) {
  const failure = domain.failure;
  return `${failure ? `<div class="agent-domain-notice warning" role="alert"><strong>${failure.kind === "conflict" ? "记录已变化" : view === "memory" ? "未能删除记忆" : "未能撤销审批"}</strong><p>${failure.kind === "conflict" ? "此操作未应用。重新读取最新记录后再决定。" : "原操作已明确失败，记录仍保留。可以重试此操作。"}</p>${action(failure.kind === "conflict" ? "重新读取记录" : "重试此操作", `${view}-${failure.kind === "conflict" ? "refresh" : "retry"}`, false, "")}</div>` : ""}<p class="agent-domain-feedback" role="status" aria-live="polite">${esc(domain.message)}</p>`;
}

function pager(page, count, size, prefix) {
  if (!count) return "";
  const pages = Math.max(1, Math.ceil(count / size));
  return `<nav class="agent-domain-pagination" aria-label="${prefix.startsWith("history") ? "修订历史分页" : prefix === "memory" ? "记忆分页" : "审批分页"}"><span>第 ${page + 1} / ${pages} 页 · ${count} 条</span><div>${action("上一页", `${prefix}-prev`, page === 0)}${action("下一页", `${prefix}-next`, page >= pages - 1)}</div></nav>`;
}

function readGate(domain, view) {
  if (domain.loading) return `<section class="agent-domain-empty" aria-busy="true" role="status"><p>正在读取${view === "memory" ? "记忆" : "审批记录"}…</p></section>`;
  if (domain.readError) return `<section class="agent-domain-empty"><h2>暂时无法读取${view === "memory" ? "记忆" : "审批记录"}</h2><p>已有筛选与操作状态仍保留。</p>${action("重新读取", `${view}-refresh`, false, "")}</section>`;
  return "";
}

function renderHistory(ctx, domain, record) {
  if (domain.expanded !== record.id) return "";
  if (domain.historyErrors[record.id]) return `<section class="agent-domain-history"><h3>修订历史</h3><p>暂时无法读取此条记忆的历史，当前记录仍可阅读。</p>${action("重试读取历史", `history-retry:${record.id}`)}</section>`;
  const page = Math.min(domain.historyPages[record.id] || 0, Math.ceil(record.history.length / HISTORY_SIZE) - 1);
  domain.historyPages[record.id] = page;
  return `<section class="agent-domain-history" aria-label="修订历史"><h3>修订历史</h3>${record.history.slice(page * HISTORY_SIZE, (page + 1) * HISTORY_SIZE).map((revision) => `<article class="agent-domain-revision" data-memory-revision="${esc(revision.revision)}"><header><strong>修订 ${esc(revision.revision)}</strong>${revision.status === "active" ? `<span class="agent-domain-status">当前</span>` : status(revision.status)}${time(revision.createdAt)}</header><p>${esc(revision.text)}</p>${source(revision)}</article>`).join("")}${pager(page, record.history.length, HISTORY_SIZE, `history:${record.id}`)}</section>`;
}

export function renderAgentMemory(ctx, state, current, bridge) {
  const domain = local(ctx, state, current, bridge).memory;
  const all = ctx.scenario === "empty" ? [] : current.memories;
  const records = all.filter((record) => (domain.type === "all" || record.type === domain.type) && (domain.tag === "all" || record.tags.includes(domain.tag)) && (domain.importance === "all" || record.importance === domain.importance) && (domain.source === "all" || (record.provenance?.type || "missing") === domain.source) && (domain.status === "all" || record.status === domain.status));
  domain.page = Math.min(domain.page, Math.max(0, Math.ceil(records.length / PAGE_SIZE) - 1));
  const tags = [...new Set(all.flatMap((record) => record.tags))];
  return `<div class="agent-detail-content agent-domain" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-memory"><div class="agent-domain-heading"><div><h2>记忆</h2><p>此 Agent 私有的事实、经验与流程，执行时按需读取。</p></div>${action("刷新记忆", "memory-refresh", !!state.domainBusy, "")}</div><p class="agent-domain-context">仅属于当前项目的 ${esc(nameOf(current))}，不会自动共享给其他 Agent。</p>${pendingBanner(state, current, "memory")}${feedback(domain, "memory")}<div class="agent-domain-filters">${filter("memory-type", "类型", domain.type, [["all", "全部类型"], ...Object.entries(types)])}${filter("memory-tag", "标签", domain.tag, [["all", "全部标签"], ...tags.map((tag) => [tag, tag])])}${filter("memory-importance", "重要性", domain.importance, [["all", "全部重要性"], ...Object.entries(importance)])}${filter("memory-source", "来源", domain.source, [["all", "全部来源"], ...Object.entries(sourceTypes), ["missing", "来源未记录"]])}${filter("memory-status", "记录", domain.status, [["active", "当前记忆"], ["deleted", "已删除"], ["all", "全部记录"]])}</div>${readGate(domain, "memory") || (records.length ? `<div class="agent-domain-records">${records.slice(domain.page * PAGE_SIZE, (domain.page + 1) * PAGE_SIZE).map((record) => `<article class="agent-domain-record" data-memory-id="${esc(record.id)}"><header><div><strong>${types[record.type] || esc(record.type)}</strong><span>修订 ${esc(record.revision)}</span>${record.status === "deleted" ? status("deleted") : ""}</div>${time(record.updatedAt)}</header><p class="agent-domain-memory-text">${esc(record.text)}</p><div class="agent-domain-memory-meta"><span>重要性：${importance[record.importance] || esc(record.importance)}</span><span>${esc(record.tags.join(" · ") || "无标签")}</span></div>${source(record)}<div class="agent-domain-record-actions">${action(domain.expanded === record.id ? "收起历史" : "查看历史", `memory-history:${record.id}`)}${record.status === "active" ? action("删除记忆", `memory-delete:${record.id}`, locked(ctx, state, bridge)) : `<span>已停止参与记忆召回，历史仍保留。</span>`}</div>${renderHistory(ctx, domain, record)}</article>`).join("")}</div>${pager(domain.page, records.length, PAGE_SIZE, "memory")}` : `<section class="agent-domain-empty"><h2>${all.length ? "没有符合筛选的记忆" : "此 Agent 暂无记忆"}</h2><p>${all.length ? "调整筛选，或查看已删除记录的修订历史。" : "执行中明确保留的内容会出现在这里。"}</p>${all.length ? action("清除筛选", "memory-clear") : ""}</section>`)}<details class="agent-domain-explanation"><summary>记忆如何使用</summary><p>这些内容属于当前 Agent 的私有命名空间，执行中通过记忆工具按需检索。重要性表示保留优先级，不代表内容置信度。来源删除或不可访问不会清除已保留的记忆。</p></details></div>`;
}

function approvalDetail(ctx, state, current, domain, record, bridge) {
  return `<section class="agent-domain-detail" data-approval-id="${esc(record.id)}">${action("返回审批列表", "audit-back")}<header class="agent-domain-detail-heading"><div><h2>${esc(record.toolName)}</h2><p>${esc(record.scopeSummary)}</p></div>${status(record.state)}</header><dl class="agent-domain-properties"><dt>适用范围</dt><dd>${esc(record.scopeSummary)}<span>仅限当前项目与 ${esc(nameOf(current))}；不扩展工具权限，也不跳过安全策略。</span></dd><dt>工具来源</dt><dd>${esc(record.toolSource)}</dd><dt>创建时间</dt><dd>${time(record.createdAt)}</dd><dt>批准人</dt><dd>${esc(record.createdBy)}</dd>${record.revokedAt ? `<dt>撤销时间</dt><dd>${time(record.revokedAt)}</dd><dt>撤销人</dt><dd>${esc(record.revokedBy)}</dd>` : ""}</dl><section class="agent-domain-audit-source"><h3>来源与操作记录</h3><p>${esc(record.sourceSummary)}</p><p class="agent-domain-source">来源执行：${esc(record.executionSummary)}</p>${record.sourceAvailability !== "available" ? `<p class="agent-domain-context">来源已删除，审批时的范围与审计摘要仍保留。</p>` : ""}<ol class="agent-domain-events"><li><strong>批准后续同类调用</strong><span>${esc(record.createdBy)} · ${stamp(record.createdAt)}</span></li>${record.revokedAt ? `<li><strong>撤销可复用审批</strong><span>${esc(record.revokedBy)} · ${stamp(record.revokedAt)}</span></li>` : ""}</ol></section>${record.state === "active" ? `<div class="agent-domain-revoke"><p>撤销后，后续调用不再复用这条审批。已完成的调用与审批历史仍保留。</p>${action("撤销审批", `audit-revoke:${record.id}`, locked(ctx, state, bridge), "danger")}</div>` : `<p class="agent-domain-context">这条审批已撤销，不再用于后续调用。</p>`}<details class="agent-domain-explanation"><summary>记录标识</summary><dl class="agent-domain-properties"><dt>审批</dt><dd>${esc(record.id)}</dd><dt>Tool</dt><dd>${esc(record.toolID)}</dd><dt>来源 Tool Call</dt><dd>${esc(record.sourceCallID)}</dd><dt>记录版本</dt><dd>${esc(record.version)}</dd></dl></details></section>`;
}

export function renderAgentAudit(ctx, state, current, bridge) {
  const domain = local(ctx, state, current, bridge).audit;
  const all = ctx.scenario === "empty" ? [] : current.approvals;
  const detail = all.find((record) => record.id === domain.selected);
  const records = all.filter((record) => (domain.state === "all" || record.state === domain.state) && (domain.tool === "all" || record.toolID === domain.tool));
  domain.page = Math.min(domain.page, Math.max(0, Math.ceil(records.length / PAGE_SIZE) - 1));
  const tools = [...new Map(all.map((record) => [record.toolID, record.toolName])).entries()];
  return `<div class="agent-detail-content agent-domain" id="agent-tab-panel" role="tabpanel" aria-labelledby="agent-tab-audit"><div class="agent-domain-heading"><div><h2>可复用审批</h2><p>查看工具操作授权及其撤销历史。</p></div>${action("刷新审批", "audit-refresh", !!state.domainBusy, "")}</div><p class="agent-domain-context">在实际工具调用中批准后续同类操作后，记录会出现在这里。</p>${pendingBanner(state, current, "audit")}${feedback(domain, "audit")}${readGate(domain, "audit") || (detail ? approvalDetail(ctx, state, current, domain, detail, bridge) : `<div class="agent-domain-filters audit-filters">${filter("audit-state", "状态", domain.state, [["all", "全部状态"], ["active", "生效中"], ["revoked", "已撤销"]])}${filter("audit-tool", "工具", domain.tool, [["all", "全部工具"], ...tools])}</div>${records.length ? `<div class="agent-domain-approval-list"><div class="agent-domain-approval-head" aria-hidden="true"><span>工具与授权范围</span><span>状态</span><span>创建时间</span></div>${records.slice(domain.page * PAGE_SIZE, (domain.page + 1) * PAGE_SIZE).map((record) => `<button type="button" class="agent-domain-approval-row" data-agent-domain-action="audit-detail:${esc(record.id)}"><span class="agent-domain-approval-copy"><strong>${esc(record.toolName)}</strong><span>${esc(record.scopeSummary)}</span></span>${status(record.state)}${time(record.createdAt)}<span class="agent-domain-open" aria-hidden="true">${icon("arrow", 15)}</span></button>`).join("")}</div>${pager(domain.page, records.length, PAGE_SIZE, "audit")}` : `<section class="agent-domain-empty"><h2>${all.length ? "没有符合筛选的审批" : "暂无可复用审批"}</h2><p>${all.length ? "调整状态或工具筛选后重试。" : "这里只记录实际工具调用中产生的可复用审批。"}</p>${all.length ? action("清除筛选", "audit-clear") : ""}</section>`}`)}<p class="agent-domain-footnote">审批固定属于当前项目、Agent 与工具。范围不可在此编辑；撤销后保留记录。</p></div>`;
}

function applyCommand(current, command) {
  const target = (command.view === "memory" ? current.memories : current.approvals).find((record) => record.id === command.original.targetID);
  if (!target || command.original.agent !== current.id) return false;
  if (command.applied) return true;
  const now = new Date().toISOString();
  if (command.view === "memory") {
    if (target.revision !== command.original.expectedRevision || target.status !== "active") return false;
    target.history.forEach((revision) => { if (revision.status === "active") revision.status = "superseded"; });
    target.revision = String(Number(target.revision) + 1);
    target.status = "deleted";
    target.updatedAt = now;
    target.history.unshift({ revision: target.revision, text: target.text, status: "deleted", createdAt: now, provenance: { type: "tool_operation", label: "项目 Owner 删除记忆", availability: "available" } });
  } else {
    if (target.version !== command.original.expectedVersion || target.state !== "active") return false;
    target.state = "revoked";
    target.version = String(Number(target.version) + 1);
    target.revokedAt = now;
    target.revokedBy = "项目 Owner";
  }
  command.applied = true;
  return true;
}

function finish(ctx, state, current, domain, command, bridge) {
  if (!applyCommand(current, command)) {
    command.status = "unknown";
    command.observation = "invalid_receipt";
    state.domainPending = command;
    return;
  }
  domain.failure = null;
  domain.message = command.view === "memory" ? "记忆已删除。可在「已删除」记录中查看完整修订历史。" : "审批已撤销，记录与来源仍保留。";
}

function execute(ctx, state, current, view, target, bridge) {
  if (locked(ctx, state, bridge)) return;
  const domain = local(ctx, state, current, bridge)[view];
  const command = commandFor(ctx, current, view, target, bridge);
  state.domainBusy = command;
  domain.message = "";
  domain.failure = null;
  refresh(ctx, current, view, bridge);
  setTimeout(() => {
    if (state.domainBusy !== command) return;
    state.domainBusy = null;
    const outcome = domain.resultUsed ? "success" : ctx.params.get(`${view}_result`);
    domain.resultUsed = true;
    if (outcome === "unknown") state.domainPending = command;
    else if (outcome === "error" || outcome === "conflict") domain.failure = { kind: outcome, command };
    else finish(ctx, state, current, domain, command, bridge);
    refresh(ctx, current, view, bridge, state.domainPending ? `[data-agent-domain-action="${view}-lookup"]` : `[data-agent-domain-action="${view}-refresh"]`);
  }, 350);
}

function confirmAction(ctx, state, current, view, target, bridge) {
  if (locked(ctx, state, bridge)) return;
  const title = view === "memory" ? "删除这条记忆？" : "撤销这条审批？";
  const description = view === "memory" ? "删除后停止参与记忆召回。当前内容与修订历史仍可在已删除记录中查看。" : "撤销后，后续工具调用不再复用此审批。已有调用和审批历史不受影响。";
  showDialog(`${dialogHeader(title)}<div class="dialog-body"><p>${description}</p><blockquote class="agent-domain-confirm-copy">${esc(view === "memory" ? target.text : `${target.toolName} · ${target.scopeSummary}`)}</blockquote></div>${dialogFooter(`<button type="button" class="button" data-dialog-close>取消</button>${action(view === "memory" ? "确认删除" : "确认撤销", "confirm-domain", false, "danger")}`)}`, (dialog) => {
    dialog.querySelector('[data-agent-domain-action="confirm-domain"]').onclick = () => {
      closeDialog();
      execute(ctx, state, current, view, target, bridge);
    };
  });
}

function lookup(ctx, state, current, view, bridge) {
  const pending = state.domainPending;
  if (!pending || pending.agent !== current.id || pending.view !== view || pending.checking || ["committed", "failed"].includes(pending.status)) return;
  pending.checking = true;
  refresh(ctx, current, view, bridge);
  setTimeout(() => {
    if (state.domainPending !== pending) return;
    const result = ctx.params.get(`${view}_lookup`) || "committed";
    pending.checking = false;
    if (result === "not_observed") pending.observation = result;
    else pending.status = result === "failed" ? "failed" : "committed";
    refresh(ctx, current, view, bridge, `[data-agent-domain-action="${view}-${pending.status === "unknown" ? "lookup" : "reconcile"}"]`);
  }, 300);
}

function reread(ctx, state, current, view, bridge, reconcile = false) {
  const domain = local(ctx, state, current, bridge)[view];
  const pending = state.domainPending;
  if (state.domainBusy || domain.loading) return;
  if (pending && (pending.agent !== current.id || pending.view !== view || !reconcile || !["committed", "failed"].includes(pending.status))) {
    domain.message = "先查询原操作结果，确认终态后再重新读取。";
    refresh(ctx, current, view, bridge, `[data-agent-domain-action="${view}-lookup"]`);
    return;
  }
  domain.loading = true;
  domain.message = "";
  refresh(ctx, current, view, bridge);
  setTimeout(() => {
    domain.loading = false;
    domain.readError = false;
    domain.readOnce = true;
    if (pending && state.domainPending === pending) {
      if (pending.status === "committed") {
        finish(ctx, state, current, domain, pending, bridge);
        if (pending.applied) state.domainPending = null;
      } else {
        state.domainPending = null;
        domain.failure = { kind: "error", command: pending };
        domain.message = "已回读原记录。原操作未应用，可重新确认后重试。";
      }
    } else {
      domain.failure = null;
      domain.message = view === "memory" ? "记忆已刷新。" : "审批记录已刷新。";
    }
    refresh(ctx, current, view, bridge, `[data-agent-domain-action="${view}-refresh"]`);
  }, 300);
}

export function bindAgentMemoryAudit(ctx, state, current, bridge) {
  if (!current) return;
  const view = ctx.params.get("view");
  if (!["memory", "audit"].includes(view)) return;
  const domains = local(ctx, state, current, bridge), domain = domains[view];
  const root = document.getElementById("agent-tab-panel");
  if (!root) return;
  root.querySelectorAll("[data-agent-domain-filter]").forEach((input) => {
    input.onchange = () => {
      domain[input.id.slice(view.length + 1)] = input.value;
      domain.page = 0;
      bridge.refresh(ctx, `[data-select-for="${input.id}"], #${input.id}`);
    };
  });
  root.querySelectorAll("[data-agent-domain-action]").forEach((button) => {
    button.onclick = () => {
      const id = button.dataset.agentDomainAction;
      if (id === `${view}-lookup`) return lookup(ctx, state, current, view, bridge);
      if (id === `${view}-reconcile`) return reread(ctx, state, current, view, bridge, true);
      if (id === `${view}-refresh`) return reread(ctx, state, current, view, bridge);
      if (id === `${view}-retry`) {
        const target = (view === "memory" ? current.memories : current.approvals).find((record) => record.id === domain.failure?.command.original.targetID);
        if (target) confirmAction(ctx, state, current, view, target, bridge);
        return;
      }
      if (id === `${view}-clear`) {
        if (view === "memory") Object.assign(domain, { type: "all", tag: "all", importance: "all", source: "all", status: "active" });
        else Object.assign(domain, { state: "all", tool: "all" });
        domain.page = 0;
      } else if (id === `${view}-next`) domain.page++;
      else if (id === `${view}-prev`) domain.page = Math.max(0, domain.page - 1);
      else if (id.startsWith("memory-history:")) {
        const recordID = id.slice("memory-history:".length);
        domain.expanded = domain.expanded === recordID ? null : recordID;
        if (!domain.historyLoaded[recordID]) {
          domain.historyLoaded[recordID] = true;
          domain.historyErrors[recordID] = ctx.params.get("memory_history") === "error";
        }
      } else if (id.startsWith("history-retry:")) domain.historyErrors[id.slice("history-retry:".length)] = false;
      else if (id.startsWith("history:")) {
        const direction = id.endsWith("-next") ? 1 : -1;
        const recordID = id.slice("history:".length, -5);
        domain.historyPages[recordID] = Math.max(0, (domain.historyPages[recordID] || 0) + direction);
      } else if (id.startsWith("memory-delete:")) {
        const target = current.memories.find((record) => record.id === id.slice("memory-delete:".length));
        if (target?.status === "active") confirmAction(ctx, state, current, view, target, bridge);
        return;
      } else if (id.startsWith("audit-detail:")) {
        domain.selected = id.slice("audit-detail:".length);
        return bridge.refresh(ctx, '[data-agent-domain-action="audit-back"]');
      } else if (id === "audit-back") {
        const original = domain.selected;
        domain.selected = null;
        bridge.refresh(ctx, `[data-agent-domain-action="audit-detail:${original}"]`);
        // A state filter can remove a revoked row. Restore focus to the filter.
        requestAnimationFrame(() => {
          if (!document.querySelector(`[data-agent-domain-action="audit-detail:${original}"]`)) document.querySelector('[data-select-for="audit-state"], #audit-state')?.focus();
        });
        return;
      } else if (id.startsWith("audit-revoke:")) {
        const target = current.approvals.find((record) => record.id === id.slice("audit-revoke:".length));
        if (target?.state === "active") confirmAction(ctx, state, current, view, target, bridge);
        return;
      }
      bridge.refresh(ctx, `[data-agent-domain-action="${id}"]`);
    };
  });
}
