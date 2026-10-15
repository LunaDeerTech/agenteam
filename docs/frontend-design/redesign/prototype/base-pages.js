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
const baselineProjects = [
  {
    name: "Atlas",
    description: "一个用于核对真实页面结构、长内容与操作布局的测试项目。",
    state: "active",
  },
  {
    name: "Design.System",
    description: "组件、交互与视觉规范，共享一致的工作语言。",
    state: "active",
  },
  {
    name: "Release-Archive",
    description: "已交付的版本记录。",
    state: "archived",
  },
  {
    name: "Project_" + "A".repeat(56),
    description: "LongDescription".repeat(24),
    state: "archiving",
  },
  { name: "Retired.Project", description: "", state: "deleting" },
];
const designProjects = [
  {
    name: "Atlas",
    description: "让研究、规划与交付在同一个空间内有序发生。",
    state: "active",
  },
  {
    name: "Design.System",
    description: "组件、交互与视觉规范，共享一致的工作语言。",
    state: "active",
  },
  {
    name: "Release-Archive",
    description: "已交付的版本记录与复盘材料。",
    state: "archived",
  },
  {
    name: "Research.Workbench",
    description: "研究问题、资料与可复用的结论。",
    state: "active",
  },
  {
    name: "Documentation",
    description: "面向团队的产品文档与开发指南。",
    state: "active",
  },
  {
    name: "Prototype.Next",
    description: "下一阶段探索，项目归档处理中。",
    state: "archiving",
  },
];
const lifecycle = {
  active: ["活跃", "success"],
  archived: ["已归档", "neutral"],
  archiving: ["归档中", "warning"],
  deleting: ["删除中", "warning"],
};
let addedProjects = [];
function projectRows(ctx) {
  let rows = (
    ctx.fixture === "baseline" ? baselineProjects : designProjects
  ).concat(addedProjects);
  if (ctx.long && ctx.fixture !== "baseline")
    rows = rows.map((r, i) =>
      i === 1
        ? {
            ...r,
            name:
              "面向跨区域研究协作的知识组织与设计系统 / Design.System." +
              "Architecture.".repeat(5),
            description:
              "在中文、English 与长名称混排中，保留项目状态和真实入口。".repeat(
                5,
              ),
          }
        : r,
    );
  return rows;
}
function projects(ctx) {
  const gate = stateGate(ctx, "项目列表");
  return `<div class="page-wrap"><div class="page-heading"><div><h1>项目</h1><p>你的工作空间，集中管理研究与交付。</p></div>${action("新建项目", "create-project", true, ["readonly", "unknown"].includes(ctx.scenario))}</div>${ctx.scenario === "readonly" ? banner("当前账号只能查看项目", "当前权限不允许创建或修改项目。现有合法项目仍可进入。", "warning") : ""}${ctx.scenario === "unknown" ? banner("项目创建结果未知", "原创建请求与名称 Atlas.Research 已保留。在确认结果前不能再次提交。", "warning", action("查询原请求", "project-lookup")) : ""}<div class="toolbar"><label class="small muted" for="project-filter">状态</label><select class="filter-select" id="project-filter"><option value="all">全部状态</option><option value="active">活跃</option><option value="archived">已归档</option><option value="archiving">归档中</option><option value="deleting">删除中</option></select><span class="spacer"></span><button type="button" class="button quiet" data-action="refresh-projects">${icon("refresh", 14)}刷新</button></div>${
    gate ||
    (ctx.scenario === "empty"
      ? `<section class="empty-state">${icon("grid", 30)}<h2>还没有项目</h2><p>创建一个项目，为任务、知识与协作准备空间。</p><p class="small">使用页面右上方的“新建项目”开始。</p></section>`
      : `<div class="project-list"><div class="project-list-head"><span>项目名称</span><span>状态</span><span></span></div><div id="project-rows">${projectRows(
          ctx,
        )
          .map(
            (r) =>
              `<article class="project-row" data-testid="project-row" data-state="${r.state}"><span class="project-icon" aria-hidden="true">${esc(r.name[0])}</span><div>${r.state === "deleting" ? `<span class="project-name">${esc(r.name)}</span>` : `<a class="project-name" href="?page=project" data-page="project" data-project-name="${esc(r.name)}" data-project-lifecycle="${r.state}">${esc(r.name)}</a>`}${r.description ? `<p class="project-description">${esc(r.description)}</p>` : ""}${r.state === "deleting" ? "<p>删除尚未完成，暂无可进入的工作空间。</p>" : ""}</div>${status(...lifecycle[r.state])}${r.state === "deleting" ? "<span></span>" : `<a class="row-arrow" data-page="project" data-project-name="${esc(r.name)}" data-project-lifecycle="${r.state}" href="?page=project" aria-label="进入 ${esc(r.name)}">${icon("arrow", 16)}</a>`}</article>`,
          )
          .join(
            "",
          )}</div><div id="project-no-match" class="empty-state" hidden><h2>没有符合条件的项目</h2><p>尝试选择其他状态。</p><button class="button" data-action="clear-project-filter">清除筛选</button></div></div>`)
  }<footer class="pagination"><div><label for="project-limit">每页</label><select id="project-limit" class="filter-select"><option>20</option><option>50</option></select><span>项</span></div><div><button class="button quiet" disabled>上一页</button><button class="button quiet" disabled>下一页</button></div></footer><p class="muted small" id="project-feedback" role="status"></p></div>`;
}
function createProject(ctx) {
  showDialog(
    `${dialogHeader("新建项目")}<form id="project-create"><div class="dialog-body"><p class="muted small" style="margin-bottom:20px">项目将包含自己的任务、知识库和协作空间。</p><div class="field"><label for="project-name">项目名称</label><input id="project-name" name="name" autocomplete="off" required maxlength="120" placeholder="例如 Atlas.Research"><span class="help">使用字母、数字、点、连字符或下划线。</span></div><div class="field"><label for="project-description">描述 <span class="muted">可选</span></label><textarea id="project-description" name="description" rows="3" placeholder="这个项目要完成什么…"></textarea></div><div id="creation-status" aria-live="polite"></div></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button class="button primary" type="submit">创建项目</button>')}</form>`,
    (d) => {
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        const name = d.querySelector("#project-name").value.trim();
        if (!/^[a-zA-Z0-9._-]+$/.test(name)) {
          d.querySelector("#creation-status").innerHTML = banner(
            "请检查项目名称",
            "名称仅支持字母、数字、点、连字符和下划线。",
            "error",
          );
          d.querySelector("#project-name").focus();
          return;
        }
        const description = d.querySelector("textarea").value;
        d.querySelectorAll("input,textarea,button[type=submit]").forEach(
          (el) => (el.disabled = true),
        );
        d.querySelector("#creation-status").innerHTML = banner(
          "已接受创建请求 · 样板演示",
          "项目正在准备；此时还不能进入工作空间。",
          "info",
          action("检查准备结果", "creation-check"),
        );
        d.querySelector("[data-action=creation-check]").onclick = () => {
          addedProjects.push({ name, description, state: "active" });
          d.querySelector("#creation-status").innerHTML = banner(
            "项目已就绪 · 样板演示",
            "此结果仅保存在当前浏览器页面。",
            "info",
            action("进入项目", "creation-enter", true),
          );
          d.querySelector("[data-action=creation-enter]").onclick = () => {
            closeDialog();
            ctx.navigate("project");
          };
        };
      };
    },
  );
}
function bindProjects(ctx) {
  const root = document.getElementById("main");
  root.querySelector("[data-action=create-project]").onclick = () =>
    createProject(ctx);
  root.querySelector("#project-filter").onchange = (e) => {
    let count = 0;
    root.querySelectorAll("[data-testid=project-row]").forEach((row) => {
      row.hidden =
        e.target.value !== "all" && row.dataset.state !== e.target.value;
      if (!row.hidden) count++;
    });
    const none = root.querySelector("#project-no-match");
    if (none) none.hidden = count > 0;
  };
  root
    .querySelector("[data-action=clear-project-filter]")
    ?.addEventListener("click", () => {
      const s = root.querySelector("#project-filter");
      s.value = "all";
      s.dispatchEvent(new Event("change"));
    });
  root.querySelector("[data-action=refresh-projects]").onclick = () => {
    root.querySelector("#project-feedback").textContent =
      "已重新读取当前合成项目列表。";
  };
  root
    .querySelector("[data-action=project-lookup]")
    ?.addEventListener("click", (e) =>
      inlineResult(
        e.target,
        "查询原请求：尚未观察到结果，继续保留原请求。样板未重新创建项目。",
      ),
    );
  root.querySelector("#project-limit").onchange = () => {
    root.querySelector("#project-feedback").textContent =
      "当前合成目录未超过一页，无下一页数据。";
  };
}
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
function tasks(ctx) {
  const gate = stateGate(ctx, "当前 Sprint 任务");
  return `<div class="view-bar"><h1>任务</h1><label class="sr-only" for="sprint-select">任务范围</label><select id="sprint-select" class="filter-select"><option>${ctx.fixture === "baseline" ? "当前迭代 / 布局与交互" : "当前迭代 / 知识与协作"}</option></select><span class="small muted">${icon("grid", 14)} 看板</span><span class="spacer"></span><button class="button quiet" data-action="task-plan">规划</button>${action("新建任务", "create-task", true, ["readonly", "unknown"].includes(ctx.scenario))}</div>${ctx.scenario === "readonly" ? `<div style="padding:16px 28px 0">${readonlyNotice(ctx)}</div>` : ""}${ctx.scenario === "unknown" ? `<div style="padding:16px 28px 0">${banner("任务操作结果未知", "原请求、任务版本和修改意图均已保留。查询结果前不能重复提交。", "warning", action("查询原请求", "task-lookup"))}</div>` : ""}${
    gate ||
    (ctx.scenario === "empty"
      ? `<section class="empty-state">${icon("grid", 28)}<h2>当前 Sprint 还没有任务</h2><p>使用“新建任务”明确下一步工作，或通过规划管理 Sprint。</p></section>`
      : `<section class="board" aria-label="按七个状态分组的任务看板">${taskData(
          ctx,
        )
          .map(
            (t) =>
              `<section class="board-column" aria-label="${t.label}"><header class="board-column-header">${status(t.label, t.kind)}<span class="count">1</span><button class="icon-button" data-add-state="${t.state}" aria-label="在${t.label}列规划任务" ${["readonly", "unknown"].includes(ctx.scenario) ? "disabled" : ""}>${icon("plus", 14)}</button></header><button type="button" class="task-card" data-testid="task-card" data-task="${t.index}"><span class="task-id">${t.id}</span><h3>${esc(t.title)}</h3><span class="task-card-footer"><span>↑ 高优先级</span>${t.state === "blocked" ? "<span>等待人工</span>" : ""}<span class="avatar" aria-label="${["todo", "in_progress", "in_review"].includes(t.state) ? (ctx.fixture === "baseline" ? "Agent · 评审与实现" : "Agent · Researcher") : "未指派"}">${["todo", "in_progress", "in_review"].includes(t.state) ? "R" : "—"}</span></span></button></section>`,
          )
          .join("")}</section>`)
  }<div id="task-feedback" role="status" class="small muted" style="padding:0 28px"></div>`;
}
function openTask(ctx, index) {
  const t = taskData(ctx)[index];
  const query = new URLSearchParams(location.search);
  query.set("detail", t.id);
  history.replaceState({}, "", `?${query}`);
  let dirty = false;
  const d = showDialog(
    `${dialogHeader(`${t.id} · 任务详情`)}<div class="task-detail-body"><article class="task-detail-content"><div class="eyebrow">${ctx.fixture === "baseline" ? "当前迭代 / 布局与交互" : "当前迭代 / 知识与协作"}</div><h2 style="margin-top:10px">${esc(t.title)}</h2><div class="task-context-summary">${status(t.label, t.kind)}<span>↑ 高优先级</span><span>${["todo", "in_progress", "in_review"].includes(t.state) ? (ctx.fixture === "baseline" ? "评审与实现" : "Researcher") : "未指派"}</span></div><p>${ctx.fixture === "baseline" ? "保留描述与任务上下文。" : "让任务正文、当前状态和下一步动作在同一视野中。打开详情后仍可辨认来源，并能回到原来的任务位置。"}</p><h3>工作计划</h3><ol><li>核对对象归属、当前状态与可执行操作。</li><li>检查长内容、只读和结果未知时的恢复入口。</li><li>记录设计结论，交由独立审查者复核。</li></ol>${t.state === "blocked" ? banner("等待人工补充连接信息", "当前任务受阻；补充事实后通过现有恢复操作继续。", "warning") : ""}${ctx.scenario === "unknown" ? banner("上次操作结果未知", "保留当前任务版本和原请求。查询不会创建新的操作。", "warning", action("查询原请求", "detail-lookup")) : ""}<h3>讨论与补充</h3><label class="field"><span>任务描述草稿</span><textarea id="task-draft" rows="4" ${ctx.scenario === "readonly" ? "readonly" : ""} placeholder="补充下一步需要的上下文…">${esc(taskDrafts.get(t.id) || "")}</textarea></label><div class="save-row">${action("保存草稿演示", "save-task-draft", true, ctx.scenario === "readonly")}<span>仅保留在当前样板会话</span></div><p id="task-detail-feedback" class="inline-result" role="status"></p><h3>执行与历史</h3><div class="timeline"><p>林 创建了任务 <span>· 10 月 14 日</span></p><p>任务已加入当前 Sprint，等待后续操作。</p></div><details><summary>依赖、执行与审批说明</summary><p class="muted small">此任务样板未连接执行服务。真实实现保留 Blocker、成果、执行取消、run-command 与 MCP 审批的原请求恢复；项目待办样板提供审批布局。</p><a class="button" href="?page=inbox" data-detail-inbox>查看项目待办样板</a></details></article><aside class="task-properties"><h3>属性</h3><div class="property"><span>状态</span>${status(t.label, t.kind)}</div><div class="property"><span>优先级</span><span>↑ 高</span></div><div class="property"><span>类型</span><span>任务</span></div><div class="property"><span>执行 Agent</span><span>${["todo", "in_progress", "in_review"].includes(t.state) ? (ctx.fixture === "baseline" ? "评审与实现" : "Researcher") : "未指派"}</span></div><div class="property"><span>Sprint</span><span>当前迭代</span></div><div class="divider"></div><p class="muted small">修改受任务生命周期与当前权限约束。</p>${ctx.scenario === "readonly" ? '<p class="small muted" style="margin-top:12px">项目已归档，只能阅读。</p>' : ""}</aside></div>${dialogFooter('<button class="button" data-dialog-close>返回看板</button>')}`,
    (dialog) => {
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
  root.querySelector("[data-action=create-task]").onclick = () =>
    taskCreate(ctx);
  root
    .querySelectorAll("[data-add-state]")
    .forEach((b) => (b.onclick = () => taskCreate(ctx)));
  root.querySelector("[data-action=task-plan]").onclick = () =>
    showDialog(
      `${dialogHeader("当前 Sprint 规划")}<div class="dialog-body"><p>当前迭代 / 知识与协作</p><p class="muted small" style="margin-top:12px">按现有 Milestone → Sprint → Task 层级规划。新建任务起始为待规划；此样板不新增拖拽或 Explore 能力。</p><div class="divider"></div>${action("在当前 Sprint 新建任务", "plan-create", true, ["readonly", "unknown"].includes(ctx.scenario))}</div>${dialogFooter('<button class="button" data-dialog-close>返回看板</button>')}`,
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
let providerTab = "configuration";
let providerSaved = false;
function providerName(ctx) {
  return ctx.fixture === "baseline"
    ? "Research Provider / 共享服务"
    : ctx.long
      ? "Research Provider / 跨区域研究与协作共享服务 · " +
        "Long provider name ".repeat(4)
      : "Research Provider";
}
function providerConfig(ctx) {
  const readonly = ctx.scenario === "readonly";
  return `<form id="provider-form" data-testid="provider-editor"><section><h3>连接配置</h3><p class="muted small" style="margin-top:6px;margin-bottom:20px">设置服务名称与 API 地址。协议在创建后不可修改。</p><div class="field-row"><div><label for="provider-name">名称</label><p>显示在模型与用途配置中。</p></div><input id="provider-name" name="name" value="${esc(providerName(ctx))}" required ${readonly ? "readonly" : ""}></div><div class="field-row"><div><span>协议</span><p>当前 Provider 的固定通信协议。</p></div><span class="small" style="padding-top:4px">OpenAI Chat Completions</span></div><div class="field-row"><div><label for="provider-url">API 地址</label><p>使用服务提供方给出的基础地址。</p></div><input id="provider-url" name="url" type="url" value="https://provider.example.test/v1" required ${readonly ? "readonly" : ""}></div><div class="field-row"><div><label for="provider-enabled">启用 Provider</label><p>停用后不能用于新的模型选择。</p></div><input id="provider-enabled" name="enabled" type="checkbox" checked ${readonly ? "disabled" : ""}></div></section><section style="margin-top:30px"><h3>访问凭据</h3><div class="field-row"><div><span>凭据绑定</span><p>仅展示绑定状态；保存后不回显 Secret。</p></div><div>${status(ctx.fixture === "baseline" ? "未绑定" : "已绑定", ctx.fixture === "baseline" ? "neutral" : "success")}<button type="button" class="button quiet" data-action="credential-edit" ${readonly ? "disabled" : ""}>${ctx.fixture === "baseline" ? "添加凭据" : "更换凭据"}</button></div></div></section><div class="save-row"><button class="button primary" type="submit" ${readonly || ctx.scenario === "unknown" ? "disabled" : ""}>保存配置</button><span id="provider-save-result" role="status">${providerSaved ? "已保存演示输入；未调用服务端。" : "修改仅影响当前 Provider。"}</span></div></form><section class="danger-zone"><div><div><h3>删除 Provider</h3><p>删除前需解除已有模型与用途引用。</p></div><button class="button danger" data-action="delete-provider" ${readonly ? "disabled" : ""}>删除 Provider</button></div></section>`;
}
function providerModels(ctx) {
  return `<div><div class="section-heading" style="margin-top:0"><div><h3>此 Provider 的模型</h3><p>按模型类型配置能力与调用标识。</p></div>${action("添加模型", "create-model", true, ctx.scenario === "readonly")}</div>${
    ctx.fixture === "baseline"
      ? '<div class="empty-state"><h2>还没有模型</h2><p>添加模型后，再为用途与 Agent 分配可用模型。</p></div>'
      : [
          ["Research Chat", "research-chat", "LLM", "工具调用 · 推理 · 流式"],
          ["Research Embedding", "research-embedding", "Embedding", "文本嵌入"],
        ]
          .map(
            ([name, id, type, caps]) =>
              `<div class="model-row"><div><h3>${name}</h3><p>${id} · ${type}</p><p>${caps}</p></div><button class="button" data-edit-model="${id}" ${ctx.scenario === "readonly" ? "disabled" : ""}>编辑</button></div>`,
          )
          .join("")
  }<p class="muted small" style="margin-top:20px">模型能力来自保存的模型配置；本样板不进行远端探测。</p></div>`;
}
function providers(ctx) {
  const gate = stateGate(ctx, "Provider 目录");
  return `<div class="settings-layout"><aside class="settings-nav" aria-label="系统设置栏目"><h2>系统设置</h2><div class="nav-group">工作空间</div><span class="nav-label">用户与邀请</span><span class="nav-label">Runner</span><div class="nav-group">模型与服务</div><a href="?page=providers" aria-current="page">Provider 与模型</a><span class="nav-label">模型用途</span><div class="nav-group">平台</div><span class="nav-label">账号安全</span><span class="nav-label">邮件服务</span><span class="nav-label">出站策略</span><span class="nav-label">审计与运行信息</span></aside><div class="provider-workspace"><div class="page-heading"><div><h1>Provider 与模型</h1><p>管理模型服务、可用模型与访问凭据。</p></div>${action("添加 Provider", "create-provider", true, ctx.scenario === "readonly")}</div>${ctx.scenario === "readonly" ? banner("仅可查看系统配置", "当前账号没有系统配置写入权限。", "warning") : ""}${ctx.scenario === "unknown" ? banner("保存结果未知", "原 provider.update 请求、版本和输入已保留。查询确认前禁止再次保存。", "warning", action("查询原请求", "provider-lookup")) : ""}${gate || (ctx.scenario === "empty" ? '<div class="empty-state"><h2>还没有 Provider</h2><p>从页面顶部添加第一个模型服务。</p></div>' : `<div class="provider-split"><aside class="provider-directory" aria-label="Provider 目录"><h3>PROVIDERS</h3><button class="provider-choice" aria-pressed="true"><span class="project-icon">R</span><div><strong>${esc(providerName(ctx))}</strong><p>OpenAI 兼容</p></div></button>${ctx.fixture === "baseline" ? "" : `<div class="provider-choice" aria-pressed="false"><span class="project-icon">E</span><div><strong>Embedding Service</strong><p>仅作目录结构示例</p></div></div>`}<p class="small muted" style="margin:18px 12px">先选择服务，再编辑配置与模型。</p></aside><section class="provider-editor"><div class="provider-editor-heading"><div><h2>${esc(providerName(ctx))}</h2><p>系统级服务 · OpenAI Chat Completions</p></div>${status("已启用", "success")}</div><div class="tabs" role="tablist" aria-label="Provider 内容"><button role="tab" id="provider-tab-configuration" aria-controls="provider-tab-content" tabindex="${providerTab === "configuration" ? "0" : "-1"}" data-provider-tab="configuration" aria-selected="${providerTab === "configuration"}">配置</button><button role="tab" id="provider-tab-models" aria-controls="provider-tab-content" tabindex="${providerTab === "models" ? "0" : "-1"}" data-provider-tab="models" aria-selected="${providerTab === "models"}">模型</button></div><div id="provider-tab-content" role="tabpanel" aria-labelledby="provider-tab-${providerTab}">${providerTab === "configuration" ? providerConfig(ctx) : providerModels(ctx)}</div></section></div>`)}</div></div>`;
}
function credentialDialog() {
  showDialog(
    `${dialogHeader("保存访问凭据")}<form id="credential-form"><div class="dialog-body"><p class="muted small" style="margin-bottom:20px">输入新的凭据值。正式服务只返回凭据元信息，已有值不可读取。</p><div class="field"><label for="credential-value">新凭据值</label><input id="credential-value" type="password" autocomplete="new-password" required placeholder="输入新凭据…"></div><div id="credential-result" role="status"></div></div>${dialogFooter('<button class="button" type="button" data-dialog-close>取消</button><button class="button primary" type="submit">保存凭据</button>')}</form>`,
    (d) =>
      (d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        d.querySelector("input").value = "";
        d.querySelector("#credential-result").innerHTML = banner(
          "凭据输入已清除 · 样板演示",
          "未发送到服务端，也未存储到本地或 URL。",
        );
        d.querySelector("button[type=submit]").disabled = true;
      }),
  );
}
function modelDialog(edit = false) {
  showDialog(
    `${dialogHeader(edit ? "编辑模型" : "添加模型")}<form id="model-form"><div class="dialog-body"><div class="field"><label for="model-name">显示名称</label><input id="model-name" value="${edit ? "Research Chat" : ""}" required></div><div class="field"><label for="model-id">服务方模型标识</label><input id="model-id" value="${edit ? "research-chat" : ""}" required><span class="help">实际请求使用的模型名称。</span></div><div class="field"><label for="model-type">模型类型</label><select id="model-type"><option>LLM</option><option>Embedding</option></select></div><div class="field"><label><input type="checkbox" checked> 支持工具调用</label><label><input type="checkbox" checked> 支持流式输出</label></div><div id="model-result" role="status"></div></div>${dialogFooter('<button type="button" class="button" data-dialog-close>取消</button><button class="button primary" type="submit">保存模型</button>')}</form>`,
    (d) =>
      (d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        d.querySelector("#model-result").innerHTML = banner(
          "模型配置已演示",
          "正式接入保留 Provider 协议、模型类型与原版本；样板未写入模型。",
        );
        d.querySelector("button[type=submit]").disabled = true;
      }),
  );
}
function bindProviders(ctx) {
  const root = document.getElementById("main");
  root
    .querySelector('[role="tablist"]')
    ?.addEventListener("keydown", (event) => {
      if (!["ArrowRight", "ArrowLeft", "Home", "End"].includes(event.key))
        return;
      event.preventDefault();
      providerTab =
        event.key === "Home"
          ? "configuration"
          : event.key === "End"
            ? "models"
            : providerTab === "configuration"
              ? "models"
              : "configuration";
      ctx.render();
      document.getElementById(`provider-tab-${providerTab}`).focus();
    });
  root.querySelectorAll("[data-provider-tab]").forEach(
    (b) =>
      (b.onclick = () => {
        providerTab = b.dataset.providerTab;
        ctx.render();
        document.querySelector(`[data-provider-tab="${providerTab}"]`).focus();
      }),
  );
  root.querySelector("#provider-form")?.addEventListener("submit", (e) => {
    e.preventDefault();
    providerSaved = true;
    root.querySelector("#provider-save-result").textContent =
      "已保存演示输入；未调用服务端。";
  });
  root
    .querySelector("[data-action=credential-edit]")
    ?.addEventListener("click", credentialDialog);
  root
    .querySelector("[data-action=provider-lookup]")
    ?.addEventListener("click", (e) =>
      inlineResult(
        e.target,
        "原 provider.update 请求尚未观察到结果，继续保留输入与版本。",
      ),
    );
  root
    .querySelector("[data-action=delete-provider]")
    ?.addEventListener("click", () =>
      showDialog(
        `${dialogHeader("删除 Research Provider")}<div class="dialog-body">${banner("请先处理引用", "此 Provider 的模型仍被使用。解除或替换引用后才能删除。", "warning")}<p class="small muted">本样板演示删除门禁，不执行真实删除。</p></div>${dialogFooter('<button class="button" data-dialog-close>返回配置</button><button class="button danger" disabled>删除 Provider</button>')}`,
      ),
    );
  root.querySelector("[data-action=create-provider]").onclick = () =>
    showDialog(
      `${dialogHeader("添加 Provider")}<form id="add-provider-form"><div class="dialog-body"><div class="field"><label for="new-provider-name">名称</label><input id="new-provider-name" required></div><div class="field"><label for="new-provider-protocol">协议</label><select id="new-provider-protocol"><option>OpenAI Chat Completions</option><option>OpenAI Responses</option><option>Anthropic Messages</option></select></div><div class="field"><label for="new-provider-url">API 地址</label><input id="new-provider-url" type="url" required placeholder="https://provider.example.test/v1"></div><div id="new-provider-result" role="status"></div></div>${dialogFooter('<button class="button" type="button" data-dialog-close>取消</button><button class="button primary" type="submit">添加 Provider</button>')}</form>`,
      (d) =>
        (d.querySelector("form").onsubmit = (e) => {
          e.preventDefault();
          d.querySelector("#new-provider-result").innerHTML = banner(
            "Provider 创建输入已演示",
            "正式接入使用 provider.create 原请求恢复；样板未写入系统配置。",
          );
          d.querySelector("button[type=submit]").disabled = true;
        }),
    );
  root
    .querySelector("[data-action=create-model]")
    ?.addEventListener("click", () => modelDialog());
  root
    .querySelectorAll("[data-edit-model]")
    .forEach((b) => (b.onclick = () => modelDialog(true)));
}
export const basePages = {
  projects: {
    title: "项目",
    project: false,
    render: projects,
    bind: bindProjects,
  },
  tasks: { title: "任务", project: true, render: tasks, bind: bindTasks },
  providers: {
    title: "Provider / Model",
    project: false,
    render: providers,
    bind: bindProviders,
  },
};
