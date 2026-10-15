import {
  esc,
  icon,
  status,
  banner,
  action,
  stateGate,
  readonlyNotice,
  showDialog,
  closeDialog,
  dialogHeader,
  dialogFooter,
  inlineResult,
} from "./core.js";

// F0 fixtures only. Command names mirror the existing API/session owners; no API is called.
// Meetings: api/meetings.ts createMeetingsAPI.command / lookup; useProjectMeetings.
// Inbox: api/project-inbox.ts createProjectInboxAPI.list; actions remain in the source meeting.
// MCP: api/project-mcp.ts createProjectMCPAPI; mcp-session.ts createMCPSession.
// OAuth: mcp-oauth-session.ts createMCPOAuthSession.completeReturn / lookup.
// Memory: agent-memory-session.ts createAgentMemorySession.start / lookup.
const states = new Map();
const confirmedOAuthScenarios = new Set();
const main = () => document.querySelector("#main");
const ro = (ctx) => ctx.scenario === "readonly";
const btn = (label, name, options = {}) =>
  `<button type="button" class="button ${options.primary ? "primary" : ""} ${options.danger ? "danger" : ""} ${options.quiet ? "quiet" : ""}" data-collab="${name}" ${options.disabled ? "disabled" : ""}>${esc(label)}</button>`;
const rowButton = (name, label, body, selected = false) =>
  `<button type="button" class="co-directory-item ${selected ? "is-selected" : ""}" data-collab="${name}" ${selected ? 'aria-current="true"' : ""}><strong>${esc(label)}</strong>${body}</button>`;
const feedback = (text) =>
  text ? `<p class="co-feedback" role="status">${esc(text)}</p>` : "";
const stamp = (value) =>
  new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
  }).format(new Date(value));
const date = (value) => `<time datetime="${value}">${stamp(value)}</time>`;
const link = (ctx, page, label, extra = {}) => {
  const q = new URLSearchParams({
    page,
    scenario: ctx.scenario,
    theme: ctx.theme,
    accent: ctx.accent,
    ...extra,
  });
  return `<a class="co-link" href="?${q}" data-collab-page="${page}" ${Object.entries(
    extra,
  )
    .map(([k, v]) => `data-nav-${k}="${esc(v)}"`)
    .join(" ")}>${esc(label)}</a>`;
};
function local(ctx, init) {
  const key = `${ctx.page}:${ctx.scenario}`;
  if (!states.has(key)) states.set(key, init());
  return states.get(key);
}
function paint(ctx, focus) {
  ctx.render();
  if (focus) requestAnimationFrame(() => main()?.querySelector(focus)?.focus());
}
function bindActions(ctx, callback) {
  main().addEventListener("click", (e) => {
    const a = e.target.closest("[data-collab-page]");
    if (a && !e.ctrlKey && !e.metaKey && !e.shiftKey && e.button === 0) {
      e.preventDefault();
      const extra = {};
      for (const name of a.getAttributeNames())
        if (name.startsWith("data-nav-"))
          extra[name.slice(9)] = a.getAttribute(name);
      ctx.navigate(a.dataset.collabPage, extra);
      return;
    }
    const b = e.target.closest("[data-collab]");
    if (b && !b.disabled) callback(b.dataset.collab, b, e);
  });
}
function bindDialog(d, handler) {
  // The dialog element is reused. Bind transient controls so old handlers cannot survive its next opening.
  d.querySelectorAll("[data-collab]").forEach((button) =>
    button.addEventListener("click", handler),
  );
}
function confirm(title, text, label, run, danger = false) {
  showDialog(
    `${dialogHeader(title)}<div class="dialog-body co-dialog"><p>${esc(text)}</p></div>${dialogFooter(`<button type="button" class="button" data-dialog-close>取消</button>${btn(label, "confirm", { primary: !danger, danger })}`)}`,
    (d) => {
      d.querySelector('[data-collab="confirm"]').onclick = () => {
        closeDialog();
        run();
      };
    },
  );
}
function unknown(title, body, command, name = "lookup") {
  return `<section class="co-recovery" aria-label="原操作恢复">${banner(title, body, "warning", btn("查询原操作结果", name))}<details class="co-diagnostic"><summary>原操作信息</summary><dl><dt>原请求</dt><dd>${esc(command)}</dd><dt>恢复方式</dt><dd>保留原对象、版本、请求身份与输入；查询未确认时继续等待。</dd></dl></details></section>`;
}
function pageGate(ctx, noun, heading) {
  const gate = stateGate(ctx, noun);
  return gate
    ? `<section class="co-page"><div class="page-heading"><h1>${heading}</h1></div>${gate}</section>`
    : "";
}
const initialMessages = [
  {
    id: "m1",
    author: "林亦",
    role: "你",
    time: "2026-10-15T09:32:00Z",
    text: "今天确定知识库增量同步的第一版范围。请先核对失败恢复路径，再讨论文件分段与检索。",
    refs: ["# 增量同步设计"],
  },
  {
    id: "m2",
    author: "Architecture",
    role: "Agent",
    time: "2026-10-15T09:34:00Z",
    text: "建议以已确认的文件版本为同步边界。网络中断后保留原请求，先查询结果；只有明确未提交时，才让用户修正并重新发起。\n\n第一版保留全文与分段读取，并让引用始终指向同一 Artifact。",
    refs: ["同步流程.md"],
    reply: "林亦：先核对失败恢复路径",
  },
  {
    id: "m3",
    author: "Review",
    role: "Agent",
    time: "2026-10-15T09:36:00Z",
    text: "我核对了读取与删除流程。待确认的问题是：发现远端文件被删除时，是否立即删除本地索引？建议先标记来源不可用，等待明确决定。",
    refs: [],
  },
];
const meetingNames = [
  "知识库增量同步方案",
  "Sprint 12 · 发布准备",
  "Runner 异常恢复复盘",
  "工具调用授权边界",
  "项目资料整理",
];
function meetingState(ctx) {
  const state = local(ctx, () => ({
    selected: new URLSearchParams(location.search)
      .get("source")
      ?.startsWith("proposal")
      ? 3
      : 0,
    list: false,
    messages: initialMessages.map((x) => ({ ...x })),
    draft:
      ctx.scenario === "unknown"
        ? "请保留来源记录，并补充同步失败后的恢复步骤。"
        : "",
    refs: [],
    reply: "",
    mode: "parallel",
    interrupt: false,
    attachment: null,
    uploadStage: "",
    pending: ctx.scenario === "unknown",
    lookupCount: 0,
    sent: false,
    notice: "",
    approval: "pending",
    approvalPending: false,
    approvalChoice: "approve",
    approvalLookup: 0,
    decision: "",
    decisionAnswer: "",
    proposal: "",
    archive: false,
    deleted: false,
    more: false,
    summary: false,
    summaryFailure: true,
    summaryVersion: 3,
    title: meetingNames[0],
    agents: ["Architecture", "Review"],
    turn: "waiting",
    view: "thread",
    versions: false,
    connection: true,
    source: null,
  }));
  const source = new URLSearchParams(location.search).get("source");
  if (source !== state.source) {
    state.source = source;
    if (source) {
      state.selected = source.startsWith("proposal") ? 3 : 0;
      state.list = false;
    }
  }
  return state;
}
function messageHTML(m, index, ctx) {
  return `<article class="co-message" id="message-${esc(m.id)}"><div class="co-avatar ${m.role === "你" ? "person" : ""}" aria-hidden="true">${m.role === "你" ? "林" : m.author.slice(0, 1)}</div><div class="co-message-body"><header><strong>${esc(m.author)}</strong><span class="muted small">${esc(m.role)}</span>${date(m.time)}</header>${m.reply ? `<div class="co-quote">${esc(m.reply)}</div>` : ""}<p>${esc(m.text).replace(/\n/g, "<br>")}</p>${(m.refs || []).map((r) => `<button type="button" class="co-reference" data-collab="preview:${esc(r)}">${icon("file")}${esc(r)}</button>`).join("")}<div class="co-message-actions">${btn("回复", `reply:${index}`, { quiet: true, disabled: ro(ctx) })}${m.role === "Agent" ? btn("版本与执行", `versions:${index}`, { quiet: true }) : ""}</div></div></article>`;
}
function approvalHTML(ctx, s) {
  const pending = s.approval === "pending";
  return `<section class="co-approval" aria-label="工具执行审批"><div class="co-inline-heading"><h3>Review 请求运行检查</h3>${status(pending ? "等待审批" : s.approval === "approved" ? "已批准" : "已拒绝", pending ? "warning" : "neutral")}</div><p>执行工具 <strong>run-command</strong>。本次审批只对当前操作有效。</p><dl class="co-facts"><dt>来源</dt><dd>知识库增量同步方案 · Review 执行</dd><dt>决策依据</dt><dd>操作、执行与版本已锁定；请核对执行来源再决定。</dd></dl><details class="co-diagnostic"><summary>审批身份与操作指纹</summary><dl><dt>操作</dt><dd><code>op-review-sync-01</code></dd><dt>指纹</dt><dd><code>fixture:run-command:sync-review</code></dd><dt>审批版本</dt><dd>1</dd></dl></details>${s.approvalPending ? unknown("审批结果尚未确认", `已保留“${s.approvalChoice === "approve" ? "批准" : "拒绝"}”的原始决定。查询前请勿重复提交。`, "executionApproval.decide · expected_version 1", "approval-lookup") : pending ? `<div class="co-action-row">${btn("批准本次操作", "approve", { primary: true, disabled: ro(ctx) || s.pending })}${btn("拒绝", "reject", { disabled: ro(ctx) || s.pending })}</div>` : feedback(`样板演示：本次操作${s.approval === "approved" ? "已批准，等待执行继续" : "已拒绝"}。`)}${ro(ctx) ? '<p class="muted small">项目已归档，审批不可修改。</p>' : ""}</section>`;
}
function renderMeetings(ctx) {
  const gate = pageGate(ctx, "会议", "会议");
  if (gate) return gate;
  const s = meetingState(ctx),
    empty = ctx.scenario === "empty" || s.deleted;
  if (empty)
    return `<section class="co-page"><div class="page-heading"><h1>会议</h1>${btn("发起会议", "new-meeting", { primary: true })}</div>${feedback(s.notice)}<div class="empty-state">${icon("message", 32)}<h2>从一个需要协作的问题开始</h2><p>选择参与 Agent，写下第一条消息。会议会保留讨论、引用与待决定的问题。</p></div></section>`;
  const source = new URLSearchParams(location.search).get("source");
  const names = ctx.long
    ? [
        ...meetingNames,
        "跨团队资料同步与超长中英文文件名称 Metadata Synchronization Review",
        "发布后观察与回归清单",
        "外部依赖升级审查",
      ]
    : meetingNames;
  return `<section class="co-page co-meetings"><div class="co-viewbar"><h1>会议</h1><span class="muted">讨论与决策</span>${btn("发起会议", "new-meeting", { disabled: ro(ctx) || s.pending })}</div>${readonlyNotice(ctx)}<div class="co-meeting-layout ${s.list ? "co-show-list" : "co-show-detail"}"><aside class="co-directory" aria-label="会议目录"><div class="co-directory-label">最近会议</div>${names.map((name, i) => rowButton(`select-meeting:${i}`, name, `<span class="co-row-meta">${i === 0 ? status(s.archive ? "已归档" : "进行中", s.archive ? "neutral" : "success") : i === 3 ? status("待审核提议", "warning") : status("进行中")}<span>${i < 2 ? "今天" : "10 月 14 日"}</span></span>`, s.selected === i)).join("")}</aside><section class="co-thread" aria-label="当前会议"><header class="co-object-heading"><div><button class="button quiet co-mobile-back" type="button" data-collab="meeting-list">${icon("back")}会议目录</button><h2 tabindex="-1" id="meeting-heading">${esc(s.selected === 0 ? (ctx.long ? "知识库增量同步、跨来源引用与异常恢复机制设计讨论 Knowledge Synchronization" : s.title) : names[s.selected])}</h2>${source ? `<p>${link(ctx, "inbox", "返回项目待办", { item: source })}</p>` : ""}<p class="muted small">${esc(s.agents.join("、"))} 参与讨论 <span aria-hidden="true">·</span> ${s.connection ? "已同步已提交内容" : "连接已中断"}</p></div>${btn("会议信息", "meeting-info")}</header>${feedback(s.notice)}${s.pending ? unknown("发送结果尚未确认", "原消息、回复目标与引用已保留。请先查询原发送结果；当前不能再次发送。", "meeting.send · expected_version 12", "meeting-lookup") : ""}${!s.connection ? banner("实时连接已中断", "当前显示最近一次已确认的内容。重新连接后读取权威快照。", "warning", btn("重新连接", "reconnect")) : ""}<div class="co-thread-content">${s.selected === 3 && !s.proposal ? `<section class="co-approval"><h3>审核会议提议</h3><p>Architecture 建议发起“工具调用授权边界”，用于确认本次工具执行的权限范围。</p><p class="muted small">来源：当前项目 · Sprint 12 发布准备</p><div class="co-action-row">${btn("接受提议", "proposal-approve", { primary: true, disabled: ro(ctx) })}${btn("拒绝提议", "proposal-reject", { disabled: ro(ctx) })}</div></section>` : ""}<details class="co-summary" ${s.summary ? "open" : ""}><summary>本轮摘要 <span class="muted small">版本 ${s.summaryVersion} · ${s.summaryFailure ? "更新失败，可展开重试" : "待决定索引处理方式"}</span></summary><dl><dt>目标</dt><dd>确认增量同步范围与失败恢复方式。</dd><dt>已决定</dt><dd>引用绑定稳定文件身份；保留已确认的分段读取。</dd><dt>待决定</dt><dd>远端文件删除后，本地索引如何保留。</dd></dl>${s.summaryFailure ? `<p class="muted small">最近一轮摘要更新失败，当前保留上次已确认的摘要。</p>${btn("重试摘要更新", "summary-retry", { quiet: true, disabled: ro(ctx) || s.pending })}` : ""}</details>${s.messages.map((m, i) => messageHTML(m, i, ctx)).join("")}${ctx.long ? Array.from({ length: 5 }, (_, i) => messageHTML({ id: `long${i}`, author: i % 2 ? "Review" : "Architecture", role: "Agent", time: "2026-10-15T09:38:00Z", text: "补充核对：网络恢复后先读取当前来源状态，再处理尚未确认的操作。长名称、连续的中英文描述以及来源信息都应在阅读区域自然换行。\n\n同步边界需要保留明确版本。用户回到会议时，仍然能找到原始问题、引用与决定。", refs: [] }, i % 3, ctx)).join("") : ""}${approvalHTML(ctx, s)}<section class="co-decision" aria-label="待决定的问题"><div class="co-inline-heading"><h3>远端文件删除后，如何处理本地索引？</h3>${status(s.decision ? "已回答" : "待决定", s.decision ? "neutral" : "warning")}</div><p class="muted">Review 需要你的决定才能继续。</p>${s.decision ? `<p>${esc(s.decision)}</p>` : `<form id="decision-form"><label class="co-choice"><input type="radio" name="answer" value="保留索引，标记来源不可用" checked ${ro(ctx) ? "disabled" : ""}>保留索引，标记来源不可用</label><label class="co-choice"><input type="radio" name="answer" value="删除索引，保留操作记录" ${ro(ctx) ? "disabled" : ""}>删除索引，保留操作记录</label><div class="co-action-row"><button class="button" type="submit" ${ro(ctx) || s.pending ? "disabled" : ""}>提交决定</button>${btn("跳过本次决定", "skip-decision", { quiet: true, disabled: ro(ctx) || s.pending })}</div></form>`}</section><div class="co-runtime"><span>${status(s.turn === "cancelled" ? "本轮已取消" : s.turn === "waiting" ? "等待你的输入" : "已加入队列", "neutral")}</span>${btn("执行详情", "execution", { quiet: true })}${btn("取消整轮", "cancel-turn", { quiet: true, disabled: ro(ctx) || s.turn === "cancelled" || s.pending })}</div></div><form class="co-composer" id="meeting-composer"><label class="co-composer-label" for="meeting-draft">回复会议</label>${s.reply ? `<div class="co-reply-target"><span>回复 ${esc(s.reply)}</span>${btn("取消回复", "cancel-reply", { quiet: true, disabled: s.pending })}</div>` : ""}<textarea id="meeting-draft" name="message" rows="3" placeholder="写下消息，使用 @ 提及 Agent 或 # 引用资料…" ${ro(ctx) || s.pending || s.archive ? "disabled" : ""}>${esc(s.draft)}</textarea>${s.refs.length ? `<div class="co-references">${s.refs.map((r, i) => `<span class="co-reference">${icon("file")}${esc(r)}<button type="button" aria-label="移除引用 ${esc(r)}" data-collab="remove-ref:${i}" ${s.pending ? "disabled" : ""}>×</button></span>`).join("")}</div>` : ""}${s.attachment ? `<div class="co-upload"><div>${icon("file")}<strong>${esc(s.attachment.name)}</strong><span class="muted">${esc(s.uploadStage)}</span></div><div class="co-action-row">${s.uploadStage === "等待创建" ? btn("创建上传", "upload-create") : s.uploadStage === "传输中" ? `${btn("完成传输", "upload-transfer")}${btn("停止传输", "upload-stop")}` : s.uploadStage === "等待完成" ? btn("确认完成上传", "upload-complete") : s.uploadStage === "上传完成" ? btn("插入附件引用", "upload-insert") : s.uploadStage === "结果待确认" ? btn("查询原上传结果", "upload-lookup") : btn("查询原上传状态", "upload-resume")}${btn("取消上传", "upload-cancel", { quiet: true })}</div></div>` : ""}<div class="co-composer-tools"><div class="co-action-row">${btn("@ 提及", "mention", { quiet: true, disabled: ro(ctx) || s.pending || s.archive })}${btn("# 引用", "reference", { quiet: true, disabled: ro(ctx) || s.pending || s.archive })}<label class="button quiet co-file-label ${ro(ctx) || s.pending || s.archive ? "is-disabled" : ""}">${icon("link")}附件<input type="file" id="meeting-file" ${ro(ctx) || s.pending || s.archive ? "disabled" : ""} aria-label="添加会议附件"></label></div><div class="co-send-options"><label><span class="co-visually-hidden">发送方式</span><select id="meeting-mode" name="mode" ${s.pending || ro(ctx) ? "disabled" : ""}><option value="parallel" ${s.mode === "parallel" ? "selected" : ""}>并行讨论</option><option value="sequential" ${s.mode === "sequential" ? "selected" : ""}>依次讨论</option></select></label><label class="co-interrupt"><input type="checkbox" id="meeting-interrupt" ${s.interrupt ? "checked" : ""} ${s.pending || ro(ctx) ? "disabled" : ""}>插话</label><button class="button primary" type="submit" ${ro(ctx) || s.pending || s.archive || !!s.attachment ? "disabled" : ""}>发送</button></div></div><p class="muted small co-composer-hint">${ro(ctx) ? "项目已归档，无法发送或修改会议。" : s.archive ? "会议已归档，可在会议信息中恢复。" : s.pending ? "发送结果待确认，原草稿已锁定保留。" : s.attachment ? "先完成或取消当前附件上传，再发送消息。" : "样板演示：发送只更新当前页面。消息和附件不会上传。"}</p><p id="composer-error" role="alert"></p></form></section></div></section>`;
}
function openReference(s, ctx, type) {
  const items =
    type === "mention"
      ? ["Architecture", "Review"]
      : ["# 增量同步设计", "# TASK-42 同步失败恢复", "同步流程.md"];
  showDialog(
    `${dialogHeader(type === "mention" ? "提及参与 Agent" : "引用项目内容")}<div class="dialog-body co-dialog"><p class="muted">${type === "mention" ? "提及会保留参与 Agent 身份。" : "选择当前项目资料，引用保留类型与稳定身份。"}</p>${items.map((r, i) => rowButton(`pick:${i}`, r, `<span class="muted small">${type === "mention" ? "会议参与者" : i === 0 ? "知识库" : i === 1 ? "任务" : "已上传文件"}</span>`)).join("")}</div>`,
    (d) =>
      bindDialog(d, (e) => {
        const b = e.target.closest("[data-collab]");
        if (!b) return;
        s.refs.push(
          type === "mention"
            ? `@ ${items[Number(b.dataset.collab.split(":")[1])]}`
            : items[Number(b.dataset.collab.split(":")[1])],
        );
        closeDialog();
        paint(ctx, "#meeting-draft");
      }),
  );
}
function meetingInfo(ctx, s) {
  showDialog(
    `${dialogHeader("会议信息")}<div class="dialog-body co-dialog"><dl class="co-facts"><dt>标题</dt><dd>${esc(s.title)}</dd><dt>状态</dt><dd>${s.archive ? "已归档" : "进行中"}</dd><dt>参与 Agent 顺序</dt><dd id="participant-order">${esc(s.agents.join(" → "))}</dd><dt>会议引用</dt><dd>增量同步设计 · 同步流程.md</dd></dl><div class="co-action-row">${btn("调整参与顺序", "reorder", { disabled: ro(ctx) || s.pending })}${btn(s.archive ? "恢复会议" : "归档会议", "archive", { disabled: ro(ctx) || s.pending })}</div><p class="muted small">${ro(ctx) ? "项目已归档，会议管理只读。" : "调整顺序影响后续的依次讨论；现有消息保留原参与者。"}</p><section class="co-danger-zone"><h3>删除会议</h3><p>删除后无法继续此会议。正在执行的任务会先进入取消流程。</p>${btn("删除此会议", "delete-meeting", { danger: true, disabled: ro(ctx) || s.pending })}</section></div>${dialogFooter('<button class="button" type="button" data-dialog-close>关闭</button>')}`,
    (d) =>
      bindDialog(d, (e) => {
        const b = e.target.closest("[data-collab]");
        if (!b || b.disabled) return;
        if (b.dataset.collab === "reorder") {
          s.agents.reverse();
          d.querySelector("#participant-order").textContent =
            s.agents.join(" → ");
          s.notice = "样板演示：后续依次讨论采用新的参与顺序。";
        }
        if (b.dataset.collab === "archive") {
          s.archive = !s.archive;
          closeDialog();
          s.notice = `样板演示：会议${s.archive ? "已归档" : "已恢复"}。`;
          paint(ctx);
        }
        if (b.dataset.collab === "delete-meeting") {
          closeDialog();
          confirm(
            "删除“知识库增量同步方案”？",
            "将先取消本轮执行，再删除会议。此演示只移除当前样板中的会议。",
            "确认删除",
            () => {
              s.deleted = true;
              s.notice = "样板演示：取消已确认，会议已删除。";
              paint(ctx);
            },
            true,
          );
        }
      }),
  );
}
function newMeeting(ctx, s) {
  showDialog(
    `${dialogHeader("发起会议")}<form id="new-meeting-form"><div class="dialog-body co-dialog"><p class="muted">先选择参与 Agent，再写下要一起解决的问题。</p><fieldset><legend>参与 Agent</legend><label class="co-choice"><input name="agents" type="checkbox" value="Architecture" checked>Architecture</label><label class="co-choice"><input name="agents" type="checkbox" value="Review" checked>Review</label></fieldset><label class="field">第一条消息<textarea name="first" rows="4" required placeholder="例如：一起核对本次发布的恢复方案…"></textarea></label><label class="field">讨论方式<select name="mode"><option value="parallel">并行讨论</option><option value="sequential">依次讨论</option></select></label><p id="new-meeting-error" role="alert"></p></div>${dialogFooter('<button class="button" type="button" data-dialog-close>取消</button><button class="button primary" type="submit">发起会议</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        const f = new FormData(e.target);
        if (!f.getAll("agents").length) {
          d.querySelector("#new-meeting-error").textContent =
            "至少选择一位参与 Agent。";
          return;
        }
        s.agents = f.getAll("agents");
        s.mode = f.get("mode");
        s.title = "新会议";
        s.messages = [
          {
            id: "new",
            author: "林亦",
            role: "你",
            time: new Date().toISOString(),
            text: f.get("first"),
            refs: [],
          },
        ];
        s.deleted = false;
        s.notice = "样板演示：已发起会议，等待 Agent 参与。";
        closeDialog();
        if (ctx.scenario === "empty") {
          states.set("meetings:normal", s);
          ctx.setScenario("normal");
        } else paint(ctx);
      };
    },
  );
}
function bindMeetings(ctx) {
  const s = meetingState(ctx);
  bindActions(ctx, (name, b) => {
    if (name === "new-meeting") return newMeeting(ctx, s);
    if (name.startsWith("select-meeting:")) {
      s.selected = Number(name.split(":")[1]);
      s.list = false;
      s.notice = "";
      paint(ctx, "#meeting-heading");
      return;
    }
    if (name === "meeting-list") {
      s.list = true;
      paint(ctx, `[data-collab="select-meeting:${s.selected}"]`);
      return;
    }
    if (name === "meeting-info") return meetingInfo(ctx, s);
    if (name.startsWith("reply:")) {
      const m = s.messages[Number(name.split(":")[1])];
      s.reply = `${m.author}：${m.text.slice(0, 40)}`;
      paint(ctx, "#meeting-draft");
      return;
    }
    if (name === "cancel-reply") {
      s.reply = "";
      paint(ctx, "#meeting-draft");
      return;
    }
    if (name === "mention" || name === "reference")
      return openReference(s, ctx, name);
    if (name.startsWith("remove-ref:")) {
      s.refs.splice(Number(name.split(":")[1]), 1);
      paint(ctx, "#meeting-draft");
      return;
    }
    if (name.startsWith("preview:")) {
      showDialog(
        `${dialogHeader("引用预览")}<div class="dialog-body co-dialog"><p class="muted small">当前项目 / 文件引用</p><h3>${esc(name.slice(8))}</h3><p>所有引用都绑定稳定的 Artifact 身份。对于文本文件，可以分段读取内容；非文本文件保留元信息和下载入口。</p><pre class="co-code">同步顺序\n1. 读取当前文件版本\n2. 确认原请求结果\n3. 更新索引</pre><p class="muted small">样板展示安全文本片段；未请求或下载实际文件。</p></div>${dialogFooter('<button class="button" type="button" data-dialog-close>关闭预览</button>')}`,
      );
      return;
    }
    if (name === "meeting-lookup") {
      s.lookupCount++;
      if (s.lookupCount === 1) {
        s.notice =
          "样板演示：尚未查到原发送结果。原请求与草稿继续保留，可再次查询。";
      } else {
        s.pending = false;
        s.messages.push({
          id: "recovered",
          author: "林亦",
          role: "你",
          time: "2026-10-15T09:42:00Z",
          text: s.draft,
          refs: [...s.refs],
          reply: s.reply,
        });
        s.draft = "";
        s.refs = [];
        s.reply = "";
        s.notice = "样板演示：原发送结果已确认，消息已加入本轮。";
      }
      paint(
        ctx,
        s.pending ? '[data-collab="meeting-lookup"]' : "#meeting-draft",
      );
      return;
    }
    if (name === "approve" || name === "reject") {
      s.approvalChoice = name;
      showDialog(
        `${dialogHeader(name === "approve" ? "批准本次工具操作？" : "拒绝本次工具操作？")}<form id="approval-form"><div class="dialog-body co-dialog"><p>Review · run-command。操作身份与审批版本 1 将随本次决定保留。</p><p>当前决定仅用于这一次操作。</p><label class="co-choice"><input type="checkbox" name="uncertain">演示提交后结果未知</label></div>${dialogFooter(`<button type="button" class="button" data-dialog-close>取消</button><button type="submit" class="button primary">${name === "approve" ? "确认批准" : "确认拒绝"}</button>`)}</form>`,
        (d) => {
          d.querySelector("form").onsubmit = (e) => {
            e.preventDefault();
            s.approvalPending = d.querySelector('[name="uncertain"]').checked;
            s.approvalLookup = 0;
            if (!s.approvalPending)
              s.approval = name === "approve" ? "approved" : "rejected";
            s.notice = s.approvalPending
              ? "样板演示：原审批决定已保留，结果待确认。"
              : "样板演示：审批决定已确认。";
            closeDialog();
            paint(ctx);
          };
        },
      );
      return;
    }
    if (name === "approval-lookup") {
      s.approvalLookup++;
      if (s.approvalLookup === 1)
        s.notice = "样板演示：尚未观察到审批结果。原决定保持不变。";
      else {
        s.approvalPending = false;
        s.approval = s.approvalChoice === "approve" ? "approved" : "rejected";
        s.notice = "样板演示：原审批结果已确认。";
      }
      paint(ctx);
      return;
    }
    if (name === "skip-decision") {
      s.decision = "已跳过本次决定";
      s.notice = "样板演示：已跳过，后续处理由原执行决定。";
      paint(ctx);
      return;
    }
    if (name === "proposal-approve" || name === "proposal-reject") {
      s.proposal = name === "proposal-approve" ? "已接受" : "已拒绝";
      s.notice = `样板演示：会议提议${s.proposal}。`;
      paint(ctx);
      return;
    }
    if (name === "cancel-turn") {
      confirm(
        "取消当前整轮讨论？",
        "本轮尚未完成的参与者将停止。已提交的消息继续保留。",
        "取消整轮",
        () => {
          s.turn = "cancelled";
          s.notice = "样板演示：取消请求已确认。";
          paint(ctx);
        },
      );
      return;
    }
    if (name === "summary-retry") {
      s.summaryFailure = false;
      s.summaryVersion = 4;
      s.notice = "样板演示：摘要重试已确认；摘要读取已更新为版本 4。";
      paint(ctx);
      return;
    }
    if (name === "reconnect") {
      s.connection = true;
      s.notice = "样板演示：已读取权威快照并恢复订阅。";
      paint(ctx);
      return;
    }
    if (name === "execution" || name.startsWith("versions:")) {
      showDialog(
        `${dialogHeader(name === "execution" ? "本轮执行" : "贡献版本与执行")}<div class="dialog-body co-dialog"><div class="co-inline-heading"><h3>Review</h3>${status(s.turn === "cancelled" ? "已取消" : "等待审批", "warning")}</div><p>公开文本、Tool 摘要与已提交状态按当前执行读取。</p><dl class="co-facts"><dt>当前版本</dt><dd>Generation 1 · Attempt 1</dd><dt>等待原因</dt><dd>本次工具审批尚未处理</dd><dt>此前版本</dt><dd>尚无历史贡献</dd></dl><div class="co-action-row">${btn("取消此 Agent", "cancel-agent", { disabled: ro(ctx) || s.pending })}${btn("跳过待发言 Agent", "skip-agent", { disabled: ro(ctx) || s.pending })}${btn("重新生成", "regenerate", { disabled: ro(ctx) || s.pending || name === "execution" || name.endsWith(":2") })}</div><p class="muted small">等待中的贡献可以取消；已完成的贡献可以重新生成。重新生成保留原贡献历史，已取消的执行不会自动重启。</p></div>${dialogFooter('<button class="button" type="button" data-dialog-close>返回会议</button>')}`,
        (d) =>
          bindDialog(d, (e) => {
            const b = e.target.closest("[data-collab]");
            if (!b || b.disabled) return;
            s.notice = `样板演示：${b.textContent}请求已确认，原贡献历史保留。`;
            closeDialog();
            paint(ctx);
          }),
      );
      return;
    }
    const stages = {
      "upload-create": "传输中",
      "upload-transfer": "等待完成",
      "upload-complete": "上传完成",
      "upload-stop": "已停止",
    };
    if (stages[name]) {
      s.uploadStage = stages[name];
      paint(ctx);
      return;
    }
    if (name === "upload-resume") {
      s.uploadStage = "结果待确认";
      paint(ctx);
      return;
    }
    if (name === "upload-lookup") {
      s.uploadStage = "上传完成";
      s.notice = "样板演示：原上传已完成，可插入已确认的附件引用。";
      paint(ctx);
      return;
    }
    if (name === "upload-insert") {
      s.refs.push(s.attachment.name);
      s.attachment = null;
      s.uploadStage = "";
      paint(ctx, "#meeting-draft");
      return;
    }
    if (name === "upload-cancel") {
      s.attachment = null;
      s.uploadStage = "";
      s.notice = "样板演示：取消已确认，当前附件已移除。";
      paint(ctx);
      return;
    }
    inlineResult(b, "样板演示：当前操作已处理。");
  });
  const summary = main().querySelector(".co-summary");
  if (summary)
    summary.ontoggle = () => {
      s.summary = summary.open;
    };
  const draft = main().querySelector("#meeting-draft");
  if (draft)
    draft.oninput = () => {
      s.draft = draft.value;
    };
  const mode = main().querySelector("#meeting-mode");
  if (mode)
    mode.onchange = () => {
      s.mode = mode.value;
    };
  const interrupt = main().querySelector("#meeting-interrupt");
  if (interrupt)
    interrupt.onchange = () => {
      s.interrupt = interrupt.checked;
    };
  const file = main().querySelector("#meeting-file");
  if (file)
    file.onchange = () => {
      if (file.files[0]) {
        s.attachment = { name: file.files[0].name };
        s.uploadStage = "等待创建";
        paint(ctx);
      }
    };
  const composer = main().querySelector("#meeting-composer");
  if (composer)
    composer.onsubmit = (e) => {
      e.preventDefault();
      if (ro(ctx) || s.pending || s.attachment || s.archive) return;
      if (!s.draft.trim() && !s.refs.length) {
        main().querySelector("#composer-error").textContent =
          "先写下一条消息或插入引用。";
        draft.focus();
        return;
      }
      s.messages.push({
        id: `sent${s.messages.length}`,
        author: "林亦",
        role: "你",
        time: new Date().toISOString(),
        text: s.draft,
        refs: [...s.refs],
        reply: s.reply,
      });
      s.draft = "";
      s.refs = [];
      s.reply = "";
      s.turn = "queued";
      s.notice = `样板演示：消息${s.interrupt ? "已插入当前讨论" : "已加入下一轮队列"}，${s.mode === "parallel" ? "并行" : "依次"}讨论。`;
      paint(ctx, "#meeting-draft");
    };
  const decision = main().querySelector("#decision-form");
  if (decision)
    decision.onsubmit = (e) => {
      e.preventDefault();
      if (ro(ctx) || s.pending) return;
      s.decision = new FormData(e.target).get("answer");
      s.notice = "样板演示：决定已提交，等待原执行继续。";
      paint(ctx);
    };
}

const inboxItems = [
  {
    id: "decision-01",
    type: "decision",
    title: "远端文件删除后，如何处理本地索引？",
    summary: "Review 建议先保留索引并标记来源不可用，等待你的决定。",
    source: "知识库增量同步方案",
    blocking: true,
    time: "2026-10-15T09:36:00Z",
  },
  {
    id: "proposal-02",
    type: "proposal",
    title: "讨论工具调用的授权边界",
    summary: "Architecture 提议召开会议，确认外部工具执行前的审批范围。",
    source: "Sprint 12 · 发布准备",
    blocking: false,
    time: "2026-10-15T08:20:00Z",
  },
  {
    id: "decision-03",
    type: "decision",
    title: "选择本次发布的回退策略",
    summary: "先回滚应用，还是同步回退配置？Release 需要一个明确的执行顺序。",
    source: "Sprint 12 · 发布准备",
    blocking: true,
    time: "2026-10-14T16:20:00Z",
  },
  {
    id: "proposal-04",
    type: "proposal",
    title: "复盘 Runner 断线后的任务恢复",
    summary: "将已完成、执行中和结果待确认的情况放入同一份复盘。",
    source: "Runner 异常恢复复盘",
    blocking: false,
    time: "2026-10-14T12:10:00Z",
  },
];
function inboxState(ctx) {
  return local(ctx, () => ({
    filter:
      new URLSearchParams(location.search).get("status") === "resolved"
        ? "resolved"
        : "open",
    selected: new URLSearchParams(location.search).get("item"),
    more: false,
    notice: "",
  }));
}
function renderInbox(ctx) {
  const gate = pageGate(ctx, "项目待办", "项目待办");
  if (gate) return gate;
  const s = inboxState(ctx),
    empty = ctx.scenario === "empty";
  const items =
    s.filter === "resolved"
      ? [
          {
            ...inboxItems[0],
            id: "resolved-01",
            title: "确认第一版保留分段读取",
            summary: "已回答：保留全文与分段读取，引用绑定稳定的文件身份。",
            blocking: false,
          },
        ]
      : s.more || ctx.long
        ? [
            ...inboxItems,
            ...inboxItems.map((x, i) => ({
              ...x,
              id: `extra-${i}`,
              title: `${x.title} · 跨来源同步与长名称的第二轮完整评审`,
            })),
          ]
        : inboxItems;
  const selected = items.find((x) => x.id === s.selected);
  return `<section class="co-page co-inbox"><div class="page-heading"><div><h1>项目待办</h1><p class="muted">当前项目中需要你参与的决定与会议提议。</p></div>${btn("刷新", "refresh-inbox")}</div>${readonlyNotice(ctx)}${feedback(s.notice)}<nav class="co-tabs" aria-label="待办状态"><a href="?page=inbox&scenario=${ctx.scenario}&theme=${ctx.theme}&accent=${ctx.accent}&status=open" data-collab="filter:open" ${s.filter === "open" ? 'aria-current="page"' : ""}>待处理</a><a href="?page=inbox&scenario=${ctx.scenario}&theme=${ctx.theme}&accent=${ctx.accent}&status=resolved" data-collab="filter:resolved" ${s.filter === "resolved" ? 'aria-current="page"' : ""}>已处理</a></nav>${empty ? '<div class="empty-state"><h2>当前没有待处理事项</h2><p>新的决定与会议提议会出现在这里。你可以回到会议继续讨论。</p></div>' : `<div class="co-inbox-layout ${selected ? "co-inbox-selected" : ""}"><section class="co-inbox-list" aria-label="待办列表"><div class="co-list-label">${s.filter === "open" ? "需要你的参与" : "已处理的事项"}</div>${items.map((x) => `<button type="button" class="co-inbox-row ${s.selected === x.id ? "is-selected" : ""}" data-collab="inbox-item:${x.id}"><span class="co-inbox-type">${icon(x.type === "decision" ? "message" : "plus")}</span><span class="co-inbox-text"><strong>${esc(x.title)}</strong><span class="muted small">${x.type === "decision" ? "决定" : "会议提议"} · ${esc(x.source)}</span><span class="co-inbox-summary">${esc(x.summary)}</span></span><span class="co-inbox-end">${status(s.filter === "resolved" ? "已处理" : x.blocking ? "等待决定" : "待审核", s.filter === "resolved" ? "neutral" : x.blocking ? "warning" : "neutral")}${date(x.time)}</span></button>`).join("")}${s.filter === "open" && !s.more ? `<div class="co-load-more">${btn("加载更多", "inbox-more")}</div>` : ""}</section>${selected ? `<aside class="co-inbox-detail" aria-label="待办来源详情"><button type="button" class="button quiet" data-collab="inbox-close">${icon("back")}返回待办列表</button><p class="muted small">${selected.type === "decision" ? "决定请求" : "会议提议"}</p><h2 tabindex="-1" id="inbox-detail-heading">${esc(selected.title)}</h2>${status(s.filter === "resolved" ? "已处理" : "待处理", s.filter === "resolved" ? "neutral" : "warning")}<p>${esc(selected.summary)}</p><dl class="co-facts"><dt>来源会议</dt><dd>${esc(selected.source)}</dd><dt>当前影响</dt><dd>${selected.blocking ? "来源执行正在等待这个决定。" : "审核提议后可进入新的讨论。"}</dd><dt>处理位置</dt><dd>进入当前项目的来源会议，核对上下文并处理。</dd></dl>${link(ctx, "meetings", "打开来源会议", { source: selected.id })}${ctx.scenario === "unknown" ? banner("来源结果需重新确认", "待办列表不直接提交审批。回到来源会议查看原操作状态并查询原结果。", "warning") : ""}</aside>` : ""}</div>`}<p class="co-scope-note muted small">这里仅展示当前项目。待办按来源处理，无法直接忽略或跨项目汇总。</p></section>`;
}
function bindInbox(ctx) {
  const s = inboxState(ctx);
  bindActions(ctx, (name, b, e) => {
    if (name.startsWith("filter:")) {
      e.preventDefault();
      s.filter = name.split(":")[1];
      s.selected = null;
      const u = new URL(location.href);
      u.searchParams.set("status", s.filter);
      history.replaceState(null, "", u);
      paint(ctx, `[data-collab="${name}"]`);
    } else if (name.startsWith("inbox-item:")) {
      s.selected = name.slice(11);
      paint(ctx, "#inbox-detail-heading");
    } else if (name === "inbox-close") {
      const id = s.selected;
      s.selected = null;
      paint(ctx, `[data-collab="inbox-item:${id}"]`);
    } else if (name === "inbox-more") {
      s.more = true;
      paint(ctx);
    } else if (name === "refresh-inbox") {
      s.notice = "样板演示：已重新读取当前项目的来源状态。";
      paint(ctx);
    }
  });
}

const serviceSeed = [
  {
    id: "docs",
    name: "Documentation",
    description: "检索项目设计资料与技术文档。",
    endpoint: "https://docs.example.com/mcp",
    scheme: "bearer",
    scope: "project",
    state: "connected",
    enabled: true,
    tools: "ready",
    resources: "ready",
    credential: true,
  },
  {
    id: "github",
    name: "GitHub",
    description: "查看仓库、Issue 与 Pull Request。",
    endpoint: "https://github.example.com/mcp",
    scheme: "oauth",
    scope: "project",
    state: "auth_required",
    enabled: false,
    tools: "never",
    resources: "never",
    credential: false,
  },
  {
    id: "system",
    name: "Shared research",
    description: "由系统提供的公共研究资料服务。",
    endpoint: "https://research.example.com/mcp",
    scheme: "none",
    scope: "system",
    state: "connected",
    enabled: true,
    tools: "ready",
    resources: "error",
    credential: false,
  },
];
function mcpState(ctx) {
  const state = local(ctx, () => ({
    services:
      ctx.scenario === "empty" ? [] : serviceSeed.map((x) => ({ ...x })),
    selected: new URLSearchParams(location.search).get("service") || "docs",
    tab: "tools",
    mobileList: false,
    pending: ctx.scenario === "unknown" ? "connection.refresh" : "",
    lookupCount: 0,
    stopped: false,
    notice: "",
    credentialPending: false,
    credentialLookup: 0,
    oauthPending: false,
    oauthLookup: 0,
    credentialVersion: 1,
    routeService: null,
  }));
  const selected = new URLSearchParams(location.search).get("service");
  if (selected !== state.routeService) {
    state.routeService = selected;
    if (selected && state.services.some((x) => x.id === selected)) {
      state.selected = selected;
      state.mobileList = false;
    }
  }
  if (confirmedOAuthScenarios.has(ctx.scenario) && !state.oauthApplied) {
    const service = state.services.find((x) => x.id === "github");
    if (service) {
      // Local callback confirmation unlocks connection; enablement and discovery remain separate.
      service.state = "unconnected";
      service.enabled = false;
      state.oauthApplied = true;
    }
  }
  return state;
}
const connectionLabel = {
  connected: "已连接",
  unconnected: "未连接",
  connecting: "连接中",
  auth_required: "需要认证",
  error: "连接错误",
  needs_reconnect: "需要重新连接",
};
function renderMCP(ctx) {
  const gate = pageGate(ctx, "MCP 服务", "MCP 服务");
  if (gate) return gate;
  const s = mcpState(ctx),
    service = s.services.find((x) => x.id === s.selected) || s.services[0],
    disabled =
      ro(ctx) ||
      !!s.pending ||
      s.stopped ||
      s.credentialPending ||
      s.oauthPending;
  return `<section class="co-page co-mcp"><div class="page-heading"><div><p class="muted small">项目设置</p><h1>MCP 服务</h1><p class="muted">连接外部工具和资料，让 Agent 在项目中使用。</p></div>${btn("添加服务", "add-service", { primary: true, disabled })}</div>${readonlyNotice(ctx)}${feedback(s.notice)}${
    !service
      ? '<div class="empty-state"><h2>还没有可用的 MCP 服务</h2><p>添加项目服务配置，随后连接并检查可发现的工具与资源。</p></div>'
      : `<div class="co-mcp-layout ${s.mobileList ? "co-show-list" : "co-show-detail"}"><aside class="co-directory" aria-label="MCP 服务目录"><div class="co-directory-label">服务目录</div>${s.services.map((x) => rowButton(`service:${x.id}`, ctx.long ? `${x.name} · 跨团队技术文档与知识服务集成` : x.name, `<span class="co-row-meta"><span>${x.scope === "system" ? "系统来源" : "项目配置"}</span>${status(connectionLabel[x.state], x.state === "connected" ? "success" : x.state === "auth_required" ? "warning" : "neutral")}</span>`, x.id === service.id)).join("")}</aside><section class="co-service-detail"><button type="button" class="button quiet co-mobile-back" data-collab="service-list">${icon("back")}服务目录</button><header class="co-object-heading"><div><h2 id="service-heading" tabindex="-1">${esc(service.name)}</h2><p class="muted">${esc(service.description)}</p></div>${btn("编辑配置", "edit-service", { disabled: disabled || service.scope === "system" })}</header>${service.scope === "system" ? banner("系统来源配置只读", "项目可以管理自己的连接。服务名称、地址与认证方式由系统配置提供。", "info") : ""}${s.pending ? unknown("连接操作结果尚未确认", "当前保留原服务、连接版本与请求。查询结果前，连接和配置操作暂停。", `${s.pending} · expected_version 7`, "mcp-lookup") : ""}${s.stopped ? banner("原刷新已经停止", "重新读取连接状态，再决定是否发起下一次刷新。配置仍然保留。", "warning", btn("重新读取连接", "mcp-reread")) : ""}<section class="co-settings-section"><h3>服务配置</h3><dl class="co-setting-rows"><div><dt>来源</dt><dd>${service.scope === "system" ? "系统配置" : "当前项目"}<span class="muted small">版本 3</span></dd></div><div><dt>服务地址</dt><dd><code>${esc(service.endpoint)}</code></dd></div><div><dt>认证方式</dt><dd>${esc(service.scheme === "bearer" ? "Bearer credential" : service.scheme === "oauth" ? "OAuth 2.0" : service.scheme === "none" ? "无需认证" : service.scheme)}</dd></div></dl></section><section class="co-settings-section"><div class="co-inline-heading"><h3>项目连接</h3>${status(connectionLabel[service.state], service.state === "connected" ? "success" : service.state === "auth_required" ? "warning" : "neutral")}</div><dl class="co-setting-rows"><div><dt>Agent 使用权限</dt><dd>${status(service.enabled ? "已启用" : "已停用")}<span class="muted small">连接状态与启用状态独立</span></dd></div><div><dt>认证</dt><dd>${service.scheme === "none" ? "此服务无需认证" : service.state === "auth_required" ? "尚未完成授权" : service.credential ? "已配置凭据 · 只显示元信息" : "授权已确认"}</dd></div></dl><div class="co-action-row">${service.state !== "connected" ? btn(service.state === "auth_required" ? "先完成认证" : "连接服务", "connect", { primary: service.state !== "auth_required", disabled: disabled || service.state === "auth_required" }) : btn("刷新发现", "refresh-discovery", { disabled })}${btn(service.enabled ? "停用连接" : "启用连接", "toggle-enabled", { disabled: disabled || service.state !== "connected" })}${btn("断开连接", "disconnect", { disabled: disabled || service.state === "unconnected" })}</div>${service.state === "auth_required" ? '<p class="muted small">请先完成下方认证，再连接服务。</p>' : ""}${service.state !== "connected" ? '<p class="muted small">连接成功后才能启用 Agent 使用和刷新发现。</p>' : ""}</section><section class="co-settings-section"><div class="co-inline-heading"><h3>${service.scheme === "oauth" ? "OAuth 授权" : "访问凭据"}</h3>${service.scheme !== "none" ? btn(service.scheme === "oauth" ? "开始授权" : service.credential ? "更换凭据" : "配置凭据", service.scheme === "oauth" ? "oauth-begin" : "credential", { disabled }) : ""}</div>${service.scheme === "none" ? '<p class="muted">此服务未要求认证，无需保存访问凭据。</p>' : service.scheme === "oauth" ? `<p>授权返回后，当前项目连接会使用确认后的授权结果。</p>${link(ctx, "oauth", "查看授权返回样板")}` : `<p>凭据材料仅在输入时使用。这里显示用途与版本，不读取或回显原始值。</p><dl class="co-facts"><dt>用途</dt><dd>MCP</dd><dt>凭据版本</dt><dd>${service.credential ? s.credentialVersion : "尚未配置"}</dd></dl>${service.credential ? btn("删除访问凭据", "delete-credential", { danger: true, disabled }) : ""}`}${s.credentialPending ? unknown("凭据操作结果尚未确认", "凭据输入已经清空。请查询原操作结果，不要重新输入并创建另一条操作。", "credential.update · expected_version 1", "credential-lookup") : ""}${s.oauthPending ? unknown("授权发起结果尚未确认", "保留原授权请求，确认后才继续授权。", "oauth.begin · expected_version 7", "oauth-lookup") : ""}</section><section class="co-settings-section"><div class="co-inline-heading"><h3>发现的能力</h3><span class="muted small">与连接状态分别确认</span></div><nav class="co-tabs" aria-label="能力类型"><button type="button" data-collab="tab:tools" ${s.tab === "tools" ? 'aria-current="page"' : ""}>Tools ${service.tools === "ready" ? "3" : ""}</button><button type="button" data-collab="tab:resources" ${s.tab === "resources" ? 'aria-current="page"' : ""}>Resources ${service.resources === "ready" ? "2" : ""}</button></nav>${
          service[s.tab] === "never"
            ? `<div class="co-discovery-empty"><h4>尚未发现${s.tab === "tools" ? "工具" : "资源"}</h4><p>完成认证与连接后，再读取这个服务提供的能力。</p></div>`
            : service[s.tab] === "error"
              ? banner(
                  "资源发现失败",
                  "连接配置与已发现的工具仍保留。可以刷新发现，重新读取资源目录。",
                  "error",
                  btn("重新发现资源", "refresh-discovery", { disabled }),
                )
              : s.tab === "tools"
                ? `<div class="co-capabilities">${[
                    ["search_documents", "按标题和内容检索文档", "只读"],
                    ["read_document", "读取文档正文与版本", "只读"],
                    ["list_collections", "列出可访问的资料集合", "只读"],
                  ]
                    .map(
                      ([n, d, t]) =>
                        `<details><summary><strong>${n}</strong><span class="muted small">${t}</span></summary><p>${d}</p><dl class="co-facts"><dt>默认启用</dt><dd>是</dd><dt>当前可用</dt><dd>${service.enabled ? "是" : "否，项目连接已停用"}</dd></dl><pre class="co-code">${esc(JSON.stringify({ type: "object", properties: { query: { type: "string" } } }, null, 2))}</pre></details>`,
                    )
                    .join("")}</div>`
                : `<div class="co-capabilities">${[
                    ["Project handbook", "项目协作与交付约定", "text/markdown"],
                    ["Architecture notes", "架构资料与接口说明", "text/plain"],
                  ]
                    .map(
                      ([n, d, t]) =>
                        `<details><summary><strong>${n}</strong><span class="muted small">${t}</span></summary><p>${d}</p><p class="muted small">资源元信息由该连接的发现结果提供。</p></details>`,
                    )
                    .join("")}</div>`
        }</section>${service.scope === "project" ? `<section class="co-danger-zone"><h3>删除服务配置</h3><p>请先断开当前项目连接，再删除这个项目服务。</p>${btn("删除服务", "delete-service", { danger: true, disabled: disabled || service.state !== "unconnected" })}${service.state !== "unconnected" ? '<p class="muted small">仍存在项目连接，当前无法删除配置。</p>' : ""}</section>` : ""}</section></div>`
  }</section>`;
}
function serviceForm(ctx, s, service) {
  const editing = !!service;
  showDialog(
    `${dialogHeader(editing ? "编辑服务配置" : "添加 MCP 服务")}<form id="mcp-config-form"><div class="dialog-body co-dialog"><label class="field">名称<input name="name" value="${esc(service?.name || "")}" required maxlength="120" autocomplete="off" placeholder="例如：Documentation…"></label><label class="field">说明<textarea name="description" rows="2">${esc(service?.description || "")}</textarea></label><label class="field">服务地址<input name="endpoint" type="url" value="${esc(service?.endpoint || "")}" required autocomplete="url" placeholder="https://service.example.com/mcp"></label><label class="field">认证方式<select name="scheme">${[
      ["none", "无需认证"],
      ["bearer", "Bearer"],
      ["basic", "Basic"],
      ["oauth", "OAuth 2.0"],
    ]
      .map(
        ([v, l]) =>
          `<option value="${v}" ${service?.scheme === v ? "selected" : ""}>${l}</option>`,
      )
      .join(
        "",
      )}</select></label><p class="muted small">凭据材料在独立步骤配置，不写入服务地址。</p><p id="mcp-config-error" role="alert"></p></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button class="button primary" type="submit">保存配置</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        const f = new FormData(e.target),
          u = new URL(f.get("endpoint"));
        if (u.protocol !== "https:" || u.username || u.password) {
          d.querySelector("#mcp-config-error").textContent =
            "请输入不包含用户名或密码的 HTTPS 服务地址。";
          d.querySelector('[name="endpoint"]').focus();
          return;
        }
        const updated = {
          name: f.get("name"),
          description: f.get("description"),
          endpoint: u.href,
          scheme: f.get("scheme"),
        };
        if (editing) Object.assign(service, updated);
        else {
          const id = `service-${s.services.length}`;
          s.services.push({
            ...updated,
            id,
            scope: "project",
            state: updated.scheme === "oauth" ? "auth_required" : "unconnected",
            enabled: false,
            tools: "never",
            resources: "never",
            credential: false,
          });
          s.selected = id;
        }
        s.notice = "样板演示：项目服务配置已保存。";
        closeDialog();
        paint(ctx, "#service-heading");
      };
    },
  );
}
function bindMCP(ctx) {
  const s = mcpState(ctx);
  bindActions(ctx, (name, b) => {
    const service =
      s.services.find((x) => x.id === s.selected) || s.services[0];
    if (name === "add-service") return serviceForm(ctx, s, null);
    if (name === "edit-service") return serviceForm(ctx, s, service);
    if (name.startsWith("service:")) {
      s.selected = name.slice(8);
      s.mobileList = false;
      s.notice = "";
      paint(ctx, "#service-heading");
      return;
    }
    if (name === "service-list") {
      s.mobileList = true;
      paint(ctx);
      return;
    }
    if (name.startsWith("tab:")) {
      s.tab = name.split(":")[1];
      paint(ctx, `[data-collab="${name}"]`);
      return;
    }
    if (name === "mcp-lookup") {
      s.lookupCount++;
      if (s.lookupCount === 1) {
        s.notice =
          "样板演示：原刷新已停止，连接与配置保留。可重新读取连接状态后，再发起新的刷新。";
        s.pending = "";
        s.stopped = true;
      }
      paint(ctx);
      return;
    }
    if (name === "mcp-reread") {
      s.stopped = false;
      s.notice = "样板演示：已重新读取连接状态，可以重新发起刷新。";
      paint(ctx);
      return;
    }
    if (name === "connect") {
      service.state = "connected";
      service.enabled = true;
      service.tools = "ready";
      service.resources = "ready";
      s.notice = "样板演示：连接已确认，工具与资源发现完成。";
      paint(ctx);
      return;
    }
    if (name === "toggle-enabled") {
      service.enabled = !service.enabled;
      s.notice = `样板演示：连接${service.enabled ? "已启用" : "已停用"}。`;
      paint(ctx);
      return;
    }
    if (name === "refresh-discovery") {
      service.tools = "ready";
      service.resources = "ready";
      s.notice = "样板演示：工具与资源发现已刷新；连接配置保留。";
      paint(ctx);
      return;
    }
    if (name === "disconnect") {
      confirm(
        "断开项目连接？",
        `${service.name} 的工具与资源将不再用于本项目。服务配置仍然保留。`,
        "断开连接",
        () => {
          service.state = "unconnected";
          service.enabled = false;
          s.notice = "样板演示：项目连接已断开。";
          paint(ctx);
        },
      );
      return;
    }
    if (name === "delete-service") {
      confirm(
        `删除“${service.name}”？`,
        "删除当前项目服务配置。系统提供的配置不受影响。",
        "确认删除",
        () => {
          s.services = s.services.filter((x) => x.id !== service.id);
          s.selected = s.services[0]?.id;
          s.notice = "样板演示：项目服务配置已删除。";
          paint(ctx);
        },
        true,
      );
      return;
    }
    if (name === "credential") {
      showDialog(
        `${dialogHeader(service.credential ? "更换访问凭据" : "配置访问凭据")}<form id="credential-form"><div class="dialog-body co-dialog"><p>${esc(service.name)} · ${esc(service.scheme)}</p><label class="field">凭据材料<input type="password" name="material" required autocomplete="new-password" spellcheck="false" placeholder="输入新的凭据材料…"></label><p class="muted small">保存后清空输入，仅保留凭据用途与版本。此样板不会联网。</p><label class="co-choice"><input type="checkbox" name="uncertain">演示提交后的连接中断</label></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button type="submit" class="button primary">保存凭据</button>')}</form>`,
        (d) => {
          d.querySelector("form").onsubmit = (e) => {
            e.preventDefault();
            const simulate = d.querySelector('[name="uncertain"]').checked;
            d.querySelector('[name="material"]').value = "";
            s.credentialPending = simulate;
            if (!simulate) {
              service.credential = true;
              if (service.state === "auth_required")
                service.state = "unconnected";
              s.credentialVersion++;
            }
            s.notice = simulate
              ? "样板演示：材料已清空，原凭据操作等待确认。"
              : "样板演示：凭据已确认，仅展示元信息。";
            closeDialog();
            paint(ctx);
          };
        },
        {
          beforeClose: () => {
            const input = document.querySelector(
              '#credential-form [name="material"]',
            );
            if (input) input.value = "";
            return true;
          },
        },
      );
      return;
    }
    if (name === "credential-lookup") {
      s.credentialLookup++;
      if (s.credentialLookup === 1)
        s.notice = "样板演示：尚未观察到原凭据结果。原操作继续保留。";
      else {
        s.credentialPending = false;
        service.credential = true;
        if (service.state === "auth_required") service.state = "unconnected";
        s.credentialVersion++;
        s.notice = "样板演示：原凭据结果已确认。";
      }
      paint(ctx);
      return;
    }
    if (name === "delete-credential") {
      confirm(
        "删除访问凭据？",
        `删除 ${service.name} 使用的凭据。连接需要新的凭据才能再次认证。`,
        "确认删除凭据",
        () => {
          service.credential = false;
          service.state = "auth_required";
          s.notice = "样板演示：凭据删除已确认。";
          paint(ctx);
        },
        true,
      );
      return;
    }
    if (name === "oauth-begin") {
      showDialog(
        `${dialogHeader(`授权 ${service.name}`)}<div class="dialog-body co-dialog"><p>将为当前项目发起 OAuth 授权。授权返回后确认结果，再更新连接状态。</p><p class="muted small">样板使用本地回调情境，不打开第三方授权或携带授权码。</p></div>${dialogFooter(`<button type="button" class="button" data-dialog-close>取消</button>${btn("模拟发起结果未知", "oauth-uncertain")}${btn("进入授权返回", "oauth-return", { primary: true })}`)}`,
        (d) =>
          bindDialog(d, (e) => {
            const t = e.target.closest("[data-collab]");
            if (!t) return;
            closeDialog();
            if (t.dataset.collab === "oauth-uncertain") {
              s.oauthPending = true;
              paint(ctx);
            } else ctx.navigate("oauth");
          }),
      );
      return;
    }
    if (name === "oauth-lookup") {
      s.oauthLookup++;
      if (s.oauthLookup === 1)
        s.notice = "样板演示：原授权发起仍在处理中，请稍后再次查询。";
      else {
        s.oauthPending = false;
        s.notice = "样板演示：原授权发起已确认，可以继续授权返回样板。";
      }
      paint(ctx);
      return;
    }
    inlineResult(b, "样板演示：当前操作已处理。");
  });
}

function oauthState(ctx) {
  return local(ctx, () => ({
    phase:
      ctx.scenario === "unknown"
        ? "unknown"
        : ctx.scenario === "empty"
          ? "missing"
          : ctx.scenario === "readonly"
            ? "revoked"
            : "ready",
    queries: 0,
    notice: "",
  }));
}
function renderOAuth(ctx) {
  const s = oauthState(ctx),
    gate = stateGate(ctx, "授权返回状态");
  return `<section class="co-oauth co-page"><div class="co-oauth-context">${icon("link", 22)}<span>MCP 授权返回</span></div><h1>${s.phase === "complete" ? "授权结果已确认" : "完成项目服务授权"}</h1><p class="muted">GitHub <span aria-hidden="true">·</span> 当前项目</p>${gate || `${s.phase === "missing" ? banner("找不到可恢复的授权记录", "请回到原项目服务重新检查授权入口。此页没有可继续的原请求。", "warning") : s.phase === "revoked" ? banner("当前会话无法继续此授权", "会话身份已经改变，旧授权材料已撤下。返回项目服务后重新确认可用权限。", "warning") : s.phase === "unknown" ? unknown("授权结果尚未确认", "授权返回已经处理过。保留原兑换请求，只查询原结果，不重复兑换授权码。", "oauth.complete · 同一会话 / 原返回目标", "oauth-query") : s.phase === "complete" ? banner("原授权已确认", "此样板已恢复原请求结果。回到项目服务查看连接、启用与发现状态。", "success") : banner("等待确认授权返回", "授权过程已经回到当前页面。确认后显示本次授权结果。", "info")}${feedback(s.notice)}<ol class="co-oauth-steps"><li><span class="co-step">1</span><div><strong>确认原项目与服务</strong><p>返回位置保留为项目设置中的 GitHub 服务。</p></div></li><li><span class="co-step">2</span><div><strong>处理授权返回</strong><p>授权码不展示、不持久化，地址中的返回材料及时清理。</p></div></li><li><span class="co-step">3</span><div><strong>读取确认结果</strong><p>${ctx.long ? "如果网络在兑换响应返回前中断，页面保留原请求身份，并通过同一个项目与会话查询结果。查询尚未找到结果时继续等待，不通过自动创建新请求重复兑换。" : "网络中断时查询原请求，确认后返回服务。"}</p></div></li></ol><div class="co-action-row">${s.phase === "ready" ? btn("确认授权返回", "oauth-complete", { primary: true }) : ""}${link(ctx, "mcp", "返回项目 MCP 服务", { service: "github" })}</div><p class="muted small co-oauth-footnote">本地样板演示。未接收真实授权码，未发起外部认证。</p>`}</section>`;
}
function bindOAuth(ctx) {
  const s = oauthState(ctx);
  bindActions(ctx, (name) => {
    if (name === "oauth-complete") {
      s.phase = "complete";
      confirmedOAuthScenarios.add(ctx.scenario);
      s.notice =
        "样板演示：授权结果已确认。连接与发现状态需在项目服务中分别查看。";
      paint(ctx);
    }
    if (name === "oauth-query") {
      s.queries++;
      if (s.queries === 1)
        s.notice =
          "样板演示：查询仍为 pending，保留原兑换请求。请稍后再次查询。";
      else {
        s.phase = "complete";
        confirmedOAuthScenarios.add(ctx.scenario);
        s.notice = "样板演示：原请求已完成，未重新兑换授权码。";
      }
      paint(ctx, s.phase === "unknown" ? '[data-collab="oauth-query"]' : null);
    }
  });
}

const memories = [
  {
    id: "memory-01",
    type: "流程",
    revision: "3",
    importance: "高",
    text: "遇到写入结果未知时，先查询原请求结果。保留原对象、版本和请求身份；尚未观察到提交不等于操作失败。",
    tags: ["异常恢复", "幂等"],
    time: "2026-10-15T08:45:00Z",
    source: "execution-sync-review-03",
    operation: "memory-retain-01",
    old: "网络恢复后先确认已有操作，再决定后续步骤。",
  },
  {
    id: "memory-02",
    type: "事实",
    revision: "1",
    importance: "普通",
    text: "项目文档引用应绑定稳定的 Artifact 身份。文件显示名可以变化，引用身份不随名称改变。",
    tags: ["Artifact", "文档引用"],
    time: "2026-10-14T15:10:00Z",
    source: "execution-artifact-design-02",
    operation: "memory-retain-02",
    old: "",
  },
  {
    id: "memory-03",
    type: "经验",
    revision: "2",
    importance: "普通",
    text: "评审长任务时，先核对用户可观察的完成结果，再检查单个模块的技术实现。分阶段完成需要明确还未覆盖的依赖。",
    tags: ["评审", "交付"],
    time: "2026-10-14T11:30:00Z",
    source: "execution-review-04",
    operation: "memory-retain-03",
    old: "任务评审要核对最终结果。",
  },
  {
    id: "memory-04",
    type: "观察",
    revision: "1",
    importance: "低",
    text: "本项目中，外部资料缺失时保留来源状态，有助于区分读取失败与实际删除。",
    tags: ["来源", "知识库"],
    time: "2026-10-13T14:20:00Z",
    source: "execution-knowledge-01",
    operation: "memory-retain-04",
    old: "",
  },
];
function memoryState(ctx) {
  return local(ctx, () => ({
    records:
      ctx.scenario === "empty"
        ? []
        : [
            ...memories.map((x) => ({ ...x })),
            ...(ctx.long
              ? memories.slice(1).map((x) => ({
                  ...x,
                  id: `extra-${x.id}`,
                  text:
                    x.text +
                    " 同一条记录的来源、修订与当前状态必须能够关联核对，长内容在正文中完整显示。",
                }))
              : []),
          ],
    selected: "memory-01",
    detail: false,
    closed: ctx.params?.get("panel") === "configuration",
    more: ctx.long,
    pending: ctx.scenario === "unknown" ? "memory-01" : "",
    pendingRevision: "3",
    lookup: 0,
    notice: "",
  }));
}
function renderMemory(ctx) {
  const s = memoryState(ctx),
    gate = stateGate(ctx, "Agent 记忆");
  const records = s.records;
  const selected = records.find((x) => x.id === s.selected) || records[0];
  return `<section class="co-page co-memory"><div class="page-heading"><div><p class="muted small">项目设置 / Agent</p><h1>Architecture</h1><p class="muted">架构方案与接口边界评审</p></div>${status("可用", "success")}</div><nav class="co-tabs" aria-label="Agent 详情栏目"><button type="button" data-collab="memory-close" ${s.closed ? 'aria-current="page"' : ""}>基础配置</button><button type="button" data-collab="memory-open" ${!s.closed ? 'aria-current="page"' : ""}>记忆</button></nav>${readonlyNotice(ctx)}${s.closed ? `<section class="co-agent-overview"><h2>Agent 基础配置</h2><dl class="co-setting-rows"><div><dt>名称</dt><dd>Architecture</dd></div><div><dt>说明</dt><dd>负责架构方案与接口边界评审。</dd></div><div><dt>状态</dt><dd>可用</dd></div></dl>${btn("查看记忆", "memory-open", { primary: true })}<p class="muted small">记忆在当前 Agent 内查看，关闭后返回此处。</p></section>` : `<section class="co-memory-panel" aria-label="Architecture 的记忆"><header class="co-object-heading"><div><h2 id="memory-heading" tabindex="-1">记忆</h2><p class="muted">当前 Agent 保留的事实、经验与流程。</p></div><div class="co-action-row">${btn("刷新", "memory-refresh")}${btn("关闭记忆", "memory-close")}</div></header>${feedback(s.notice)}${s.pending ? unknown("删除结果尚未确认", "原记录继续保留。请查询原删除结果，确认前不能再次删除。", `agentMemory.delete · ${s.pending} · expected_revision ${s.pendingRevision}`, "memory-lookup") : ""}${gate || (!records.length ? '<div class="empty-state"><h3>此 Agent 暂无当前记忆</h3><p>Agent 在执行中明确保留的内容会出现在这里。</p></div>' : `<div class="co-memory-layout ${s.detail ? "co-memory-show-detail" : ""}"><section class="co-memory-records" aria-label="当前记忆"><div class="co-list-label">当前记录</div>${records.map((r) => `<article class="co-memory-row ${r.id === s.selected ? "is-selected" : ""}"><header><strong>${r.type}</strong><span class="muted small">修订 ${r.revision}</span>${date(r.time)}</header><p>${esc(r.text)}</p><div class="co-memory-row-footer"><span class="muted small">${esc(r.tags.join(" · "))}</span>${btn("来源与修订", `memory-select:${r.id}`, { quiet: true })}</div></article>`).join("")}${!s.more ? `<div class="co-load-more">${btn("加载更多记忆", "memory-more")}</div>` : ""}</section><aside class="co-memory-history" aria-label="记忆来源与修订"><button type="button" class="button quiet co-mobile-back" data-collab="memory-list">${icon("back")}当前记录</button><h3 id="memory-detail-heading" tabindex="-1">来源与修订</h3><p class="co-selected-excerpt">${esc(selected.text)}</p><dl class="co-facts"><dt>类型</dt><dd>${selected.type}</dd><dt>重要性</dt><dd>${selected.importance}</dd><dt>来源执行</dt><dd><code>${esc(selected.source)}</code></dd><dt>保留时间</dt><dd>${date(selected.time)}</dd></dl><details class="co-diagnostic"><summary>完整来源身份</summary><dl><dt>记忆</dt><dd><code>${selected.id}</code></dd><dt>操作</dt><dd><code>${selected.operation}</code></dd><dt>派生自</dt><dd>没有派生来源</dd></dl></details><div class="co-revision"><strong>修订 ${selected.revision}</strong>${status("当前")}<p>${esc(selected.text)}</p></div>${selected.old ? `<div class="co-revision"><strong>修订 ${Number(selected.revision) - 1}</strong>${status("已替代")}<p>${esc(selected.old)}</p></div>` : '<p class="muted small">这是此条记忆的首个修订。</p>'}<section class="co-danger-zone"><h4>删除当前记忆</h4><p>后续检索不再返回此条记录，修订历史仍保留。</p>${btn("删除记忆", "memory-delete", { danger: true, disabled: ro(ctx) || !!s.pending })}${ro(ctx) ? '<p class="muted small">项目已归档，无法删除记忆。</p>' : ""}</section></aside></div>`)}</section>`}</section>`;
}
function bindMemory(ctx) {
  const s = memoryState(ctx);
  bindActions(ctx, (name, b) => {
    if (name === "memory-close") {
      s.closed = true;
      paint(ctx, '[data-collab="memory-open"]');
      return;
    }
    if (name === "memory-open") {
      s.closed = false;
      paint(ctx, "#memory-heading");
      return;
    }
    if (name.startsWith("memory-select:")) {
      s.selected = name.slice(14);
      s.detail = true;
      paint(ctx, "#memory-detail-heading");
      return;
    }
    if (name === "memory-list") {
      s.detail = false;
      paint(ctx, `[data-collab="memory-select:${s.selected}"]`);
      return;
    }
    if (name === "memory-more") {
      s.more = true;
      s.records.push(
        ...memories.slice(1).map((x) => ({ ...x, id: `extra-${x.id}` })),
      );
      paint(ctx);
      return;
    }
    if (name === "memory-refresh") {
      s.notice = "样板演示：已重新读取当前 Agent 的记忆。";
      paint(ctx);
      return;
    }
    if (name === "memory-lookup") {
      s.lookup++;
      if (s.lookup === 1)
        s.notice =
          "样板演示：尚未查到原删除结果。记录继续保留，原删除请求不变。";
      else {
        s.records = s.records.filter((x) => x.id !== s.pending);
        s.pending = "";
        s.selected = s.records[0]?.id;
        s.notice = "样板演示：原删除已确认，当前记录已移除，修订历史保留。";
      }
      paint(
        ctx,
        s.pending ? '[data-collab="memory-lookup"]' : "#memory-heading",
      );
      return;
    }
    if (name === "memory-delete") {
      const record = s.records.find((x) => x.id === s.selected) || s.records[0];
      showDialog(
        `${dialogHeader("删除这条记忆？")}<form id="memory-delete-form"><div class="dialog-body co-dialog"><p><strong>Architecture · ${record.type} · 修订 ${record.revision}</strong></p><blockquote>${esc(record.text)}</blockquote><p>删除后，Agent 后续检索不再返回这条记忆。修订历史保留供查看。</p><label class="co-choice"><input type="checkbox" name="uncertain">演示提交后结果未知</label></div>${dialogFooter('<button class="button" type="button" data-dialog-close>保留记忆</button><button class="button danger" type="submit">确认删除</button>')}</form>`,
        (d) => {
          d.querySelector("form").onsubmit = (e) => {
            e.preventDefault();
            if (d.querySelector('[name="uncertain"]').checked) {
              s.pending = record.id;
              s.pendingRevision = record.revision;
              s.lookup = 0;
              s.notice = "样板演示：删除结果待确认，原记录保留。";
            } else {
              s.records = s.records.filter((x) => x.id !== record.id);
              s.selected = s.records[0]?.id;
              s.notice = "样板演示：记忆删除已确认，修订历史保留。";
            }
            closeDialog();
            paint(ctx, "#memory-heading");
          };
        },
      );
      return;
    }
    inlineResult(b, "样板演示：当前操作已处理。");
  });
}

export const collaborationPages = {
  meetings: {
    title: "会议",
    project: true,
    render: renderMeetings,
    bind: bindMeetings,
  },
  inbox: {
    title: "项目待办",
    project: true,
    render: renderInbox,
    bind: bindInbox,
  },
  mcp: { title: "MCP 服务", project: true, render: renderMCP, bind: bindMCP },
  oauth: {
    title: "MCP 授权返回",
    project: false,
    render: renderOAuth,
    bind: bindOAuth,
  },
  memory: {
    title: "Agent 记忆",
    project: true,
    render: renderMemory,
    bind: bindMemory,
  },
};
