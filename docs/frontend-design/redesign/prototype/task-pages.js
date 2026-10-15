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
const taskStates = [
  ["backlog", "待规划", "neutral"],
  ["todo", "待执行", "neutral"],
  ["in_progress", "进行中", "progress"],
  ["in_review", "评审中", "progress"],
  ["blocked", "受阻", "warning"],
  ["done", "已完成", "success"],
  ["cancelled", "已取消", "neutral"],
];
const taskTitles = [
  "整理知识库的引用与来源",
  "补齐项目只读状态的操作说明",
  "为任务详情保留连续阅读与恢复上下文",
  "审阅 MCP 工具调用的确认文案",
  "等待 Provider 连接信息确认",
  "统一项目目录与状态语义",
  "旧方案的导航探索",
];
const taskDrafts = new Map();
function taskData(ctx) {
  return taskStates.map(([state, label, kind], i) => ({
    state,
    label,
    kind,
    index: i,
    id: `AT-${142 + i}`,
    title:
      ctx.fixture === "baseline"
        ? i === 2
          ? "长任务名称与正文验证 / " + "Task boundary ".repeat(8)
          : `布局检查 ${["backlog", "todo", "in_progress", "blocked", "in_review", "done", "cancelled"].indexOf(state) + 1}`
        : ctx.long && i === 2
          ? "跨多区域、多语言知识组织与协作任务的长名称 / " +
            "Task boundary ".repeat(8)
          : taskTitles[i],
  }));
}
function taskMark(state) {
  return `<span class="task-state-mark task-state-${state}" aria-hidden="true">${state === "done" ? icon("check", 9) : state === "cancelled" ? icon("close", 9) : ""}</span>`;
}
function tasks(ctx) {
  const gate = stateGate(ctx, "当前 Sprint 任务");
  const blocked = ["readonly", "unknown"].includes(ctx.scenario);
  return `<div class="task-canvas"><header class="task-viewbar"><h1>任务</h1><span class="task-view-divider"></span><label class="sr-only" for="sprint-select">任务范围</label><select id="sprint-select" class="filter-select"><option>${ctx.fixture === "baseline" ? "当前迭代 / 布局与交互" : "当前迭代 / 知识与协作"}</option></select><span class="task-view-mode">${icon("grid", 13)}看板</span><span class="spacer"></span><button class="button quiet" data-action="task-plan">规划</button>${action("新建任务", "create-task", true, blocked)}</header>${ctx.scenario === "readonly" ? `<div class="task-page-notice">${readonlyNotice(ctx)}</div>` : ""}${ctx.scenario === "unknown" ? `<div class="task-page-notice">${banner("任务操作结果未知", "原请求与修改意图已保留。查询确认后继续。", "warning", action("查询原请求", "task-lookup"))}</div>` : ""}${gate || (ctx.scenario === "empty" ? `<section class="empty-state">${icon("grid", 28)}<h2>当前 Sprint 还没有任务</h2><p>从一个明确的任务开始。</p>${action("新建任务", "empty-create-task", true, blocked)}</section>` : `<section class="task-board-scroll" aria-label="按七个状态分组的任务看板" tabindex="0"><div class="task-board-columns">${taskData(ctx).map(t => `<section class="task-lane" aria-label="${t.label}"><header class="task-lane-heading">${taskMark(t.state)}<h2>${t.label}</h2><span class="task-lane-count" aria-label="已读取 1 个任务">1</span><button class="icon-button" data-add-state="${t.state}" aria-label="在${t.label}列规划任务" ${blocked ? "disabled" : ""}>${icon("plus", 13)}</button></header><div class="task-lane-body"><button type="button" class="task-board-card" data-testid="task-card" data-task="${t.index}"><span class="task-card-topline"><span class="task-number">${t.id}</span><span class="task-card-type">${icon("file", 12)}</span></span><h3>${esc(t.title)}</h3>${t.state === "blocked" ? `<span class="task-blocked-note">等待人工补充连接信息</span>` : ""}<span class="task-card-meta"><span class="task-priority" aria-label="高优先级"><i></i><i></i><i></i></span><span>任务</span><span class="spacer"></span><span class="task-assignee ${["todo", "in_progress", "in_review"].includes(t.state) ? "assigned" : ""}" aria-label="${["todo", "in_progress", "in_review"].includes(t.state) ? "Agent · Researcher" : "未指派"}">${["todo", "in_progress", "in_review"].includes(t.state) ? "R" : "—"}</span></span></button></div></section>`).join("")}</div></section>`)}<div id="task-feedback" role="status" class="task-page-feedback"></div></div>`;
}
const taskCopy = [
  ["整理已有知识文档的引用与来源，让研究结论能够回到原始材料。", ["核对引用对应的文档与版本。", "补全缺失的来源，并保留原文位置。", "整理需要人工确认的引用。"]],
  ["补齐项目归档后的操作说明，让阅读与修改权限保持一致。", ["核对归档状态下的任务与文档入口。", "在不可修改的位置解释原因。", "检查恢复入口和只读反馈。"]],
  ["在任务详情中保留完整正文、执行计划和讨论上下文，让评审与后续工作能够连续进行。", ["核对任务描述与执行计划的阅读顺序。", "保留未保存的补充内容与返回位置。", "检查长内容、只读与结果未知的恢复路径。"]],
  ["审阅远端工具调用前的确认内容，明确本次审批的操作范围。", ["核对 MCP 工具名称、参数与目标。", "确认批准与拒绝均针对当前操作。", "保留原请求查询与审批恢复信息。"]],
  ["连接信息尚未确认，当前工作等待人工补充。", ["确认 Provider 地址与可用协议。", "补充连接信息后重新核对。", "解除阻塞并恢复后续执行。"]],
  ["统一项目目录中的状态名称与进入方式，保证各页面表达一致。", ["核对活跃、归档与处理中状态。", "统一项目名称和状态展示。", "完成目录与工作空间入口检查。"]],
  ["保留早期导航方案的讨论上下文。本任务已取消。", ["整理早期方案的取舍。", "将可复用结论保留到后续设计。"]],
];
function openTask(ctx, index) {
  const t = taskData(ctx)[index];
  const query = new URLSearchParams(location.search);
  query.set("detail", t.id);
  history.replaceState({}, "", `?${query}`);
  let dirty = false;
  const d = showDialog(
    `<header class="task-detail-header"><div class="task-detail-location">${icon("grid", 14)}<span>任务</span><span>/</span><strong>${t.id}</strong></div><button class="icon-button" type="button" aria-label="关闭" data-dialog-close>${icon("close", 16)}</button></header><div class="task-reading-layout"><article class="task-reading-content"><h2 id="dialog-title">${esc(t.title)}</h2><div class="task-detail-meta"><span class="task-detail-state">${taskMark(t.state)}${t.label}</span><span class="task-detail-meta-divider"></span><span>↑ 高优先级</span><span class="task-detail-meta-divider"></span><span>${["todo", "in_progress", "in_review"].includes(t.state) ? (ctx.fixture === "baseline" ? "评审与实现" : "Researcher") : "未指派"}</span></div>${ctx.scenario === "unknown" ? banner("上次操作结果未知", "原请求与任务版本已保留。查询确认前不能重复提交。", "warning", action("查询原请求", "detail-lookup")) : ""}${ctx.scenario === "readonly" ? `<p class="task-readonly-note">项目已归档，当前任务仅可阅读。</p>` : ""}<div class="task-prose"><p>${ctx.fixture === "baseline" ? "保留描述与任务上下文。" : taskCopy[index][0]}</p><h3>执行计划</h3><ol>${taskCopy[index][1].map(item => `<li>${item}</li>`).join("")}</ol></div>${t.state === "blocked" ? banner("等待人工补充连接信息", "补充后可通过现有恢复操作继续。", "warning") : ""}<section class="task-draft-section"><label for="task-draft">补充上下文</label><textarea id="task-draft" rows="3" ${ctx.scenario === "readonly" ? "readonly" : ""} placeholder="记录下一步需要的信息…">${esc(taskDrafts.get(t.id) || "")}</textarea><div class="task-draft-actions"><span>草稿仅保留在当前样板会话</span>${action("保存草稿演示", "save-task-draft", false, ctx.scenario === "readonly")}</div><p id="task-detail-feedback" class="inline-result" role="status"></p></section><section class="task-activity"><h3>活动</h3><div class="task-activity-entry"><span class="task-activity-dot"></span><p><strong>林</strong> 创建了任务 <time>10 月 14 日</time></p></div><div class="task-activity-entry"><span class="task-activity-dot"></span><p>加入了当前 Sprint</p></div></section></article><aside class="task-reading-context"><h3>任务上下文</h3><div class="task-context-row">${icon("clock", 14)}<div><span>Sprint</span><p>${ctx.fixture === "baseline" ? "布局与交互" : "知识与协作"}</p></div></div><div class="task-context-row">${icon("file", 14)}<div><span>类型</span><p>任务</p></div></div><section class="task-execution-context"><h3>执行与审批</h3><p>此样板未连接执行服务。</p><a class="task-context-link" href="?page=inbox" data-detail-inbox>查看项目待办${icon("arrow", 13)}</a><details><summary>恢复与依赖</summary><p>真实实现保留 Blocker、成果、执行取消，以及 run-command 和 MCP 审批的原请求恢复。</p></details></section></aside></div><footer class="task-detail-footer"><span>${t.id}</span><button class="button" data-dialog-close>返回看板</button></footer>`,
    (dialog) => {
      dialog.classList.add("task-dialog");
      dialog.querySelector("#task-draft").oninput = () => {
        dirty = true;
      };
      dialog.querySelector("[data-action=save-task-draft]").onclick = () => {
        taskDrafts.set(t.id, dialog.querySelector("#task-draft").value);
        dirty = false;
        dialog.querySelector("#task-detail-feedback").textContent =
          "草稿已保留 · 样板演示；未调用任务写入接口。";
      };
      dialog
        .querySelector("[data-action=detail-lookup]")
        ?.addEventListener("click", (e) =>
          inlineResult(
            e.target,
            "原请求尚无可确认结果，草稿与原请求继续保留。",
          ),
        );
      dialog.querySelector("[data-detail-inbox]").onclick = (e) => {
        e.preventDefault();
        if (dirty) {
          dialog.querySelector("#task-detail-feedback").textContent =
            "仍有未保存草稿，请保存或清空草稿后离开。";
          return;
        }
        closeDialog();
        ctx.navigate("inbox");
      };
    },
    {
      wide: true,
      beforeClose: () => {
        if (!dirty) return true;
        taskDrafts.set(t.id, d.querySelector("#task-draft").value);
        d.querySelector("#task-detail-feedback").innerHTML =
          '未保存的草稿已临时保留。<button type="button" class="button" id="confirm-task-close">保留草稿并关闭</button>';
        d.querySelector("#confirm-task-close").onclick = () => {
          dirty = false;
          closeDialog();
        };
        return false;
      },
    },
  );
  d.addEventListener(
    "close",
    () => {
      const p = new URLSearchParams(location.search);
      p.delete("detail");
      history.replaceState({}, "", `?${p}`);
    },
    { once: true },
  );
}
function taskCreate(ctx) {
  if (["readonly", "unknown"].includes(ctx.scenario)) return;
  showDialog(
    `${dialogHeader("新建任务")}<form id="task-create"><div class="dialog-body"><p class="muted small" style="margin-bottom:18px">任务将创建在当前 Sprint 的待规划状态。</p><div class="field"><label for="new-task-title">任务标题</label><input id="new-task-title" name="title" required maxlength="240" placeholder="需要完成什么…"></div><div class="field"><label for="new-task-description">描述</label><textarea id="new-task-description" rows="4"></textarea></div><div id="new-task-result" role="status"></div></div>${dialogFooter('<button class="button" type="button" data-dialog-close>取消</button><button class="button primary" type="submit">创建任务</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        d.querySelector("#new-task-result").innerHTML = banner(
          "已演示任务创建输入",
          "正式接入使用现有 Sprint、task_id 与幂等请求；本样板未写入任务。",
        );
        d.querySelector("button[type=submit]").disabled = true;
      };
    },
  );
}
function bindTasks(ctx) {
  const root = document.getElementById("main");
  root
    .querySelectorAll("[data-task]")
    .forEach((b) => (b.onclick = () => openTask(ctx, Number(b.dataset.task))));
  root.querySelector("[data-action=create-task]").onclick = () => taskCreate(ctx);
  root.querySelector("[data-action=empty-create-task]")?.addEventListener("click", () => taskCreate(ctx));
  root
    .querySelectorAll("[data-add-state]")
    .forEach((b) => (b.onclick = () => taskCreate(ctx)));
  root.querySelector("[data-action=task-plan]").onclick = () =>
    showDialog(
      `${dialogHeader("当前 Sprint 规划")}<div class="dialog-body"><p>当前迭代 / 知识与协作</p><p class="muted small" style="margin-top:12px">Milestone → Sprint → Task。新建任务将进入当前 Sprint 的待规划状态。</p><div class="divider"></div>${action("在当前 Sprint 新建任务", "plan-create", true, ["readonly", "unknown"].includes(ctx.scenario))}</div>${dialogFooter('<button class="button" data-dialog-close>返回看板</button>')}`,
      (d) =>
        (d.querySelector("[data-action=plan-create]").onclick = () => {
          closeDialog();
          taskCreate(ctx);
        }),
    );
  root
    .querySelector("[data-action=task-lookup]")
    ?.addEventListener("click", (e) =>
      inlineResult(
        e.target,
        "原请求尚未观察到结果；未重发命令，当前意图继续保留。",
      ),
    );
  if (ctx.params.get("detail")) {
    const idx = taskData(ctx).findIndex(
      (t) => t.id === ctx.params.get("detail"),
    );
    if (idx >= 0) openTask(ctx, idx);
  }
}

export const taskPages = { tasks: { title: "任务", project: true, render: tasks, bind: bindTasks } };
