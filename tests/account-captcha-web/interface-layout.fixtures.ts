import type { Page } from "@playwright/test";
// Synthetic HTTP facts exercise production codecs, controllers and Vue pages.
// This harness proves frontend behavior only; it is not backend integration.
export const id = (n: number) =>
  `01990000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
export const at = "2026-10-14T00:00:00.000001Z";
export const projectPath = "/owner/atlas";
export const prefix = `/api/v1/projects/${id(10)}`;
const user = {
  id: id(1),
  email: "owner@example.test",
  username: "owner",
  display_name: "林 / Workspace",
  role: "admin",
  theme: "system",
  version: "1",
  initial_password_suggestion: false,
};
export const project = {
  id: id(10),
  owner_user_id: id(1),
  name: "Atlas",
  normalized_name: "atlas",
  description: "一个用于核对真实页面结构、长内容与操作布局的测试项目。",
  lifecycle: "active",
  version: "1",
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
};
const items = [
  {
    id: id(10),
    name: "Atlas",
    description: project.description,
    lifecycle: "active",
    version: "1",
  },
  {
    id: id(11),
    name: "Design.System",
    description: "组件、交互与视觉规范，共享一致的工作语言。",
    lifecycle: "active",
    version: "2",
  },
  {
    id: id(12),
    name: "Release-Archive",
    description: "已交付的版本记录。",
    lifecycle: "archived",
    version: "4",
  },
  {
    id: id(13),
    name: "Project_" + "A".repeat(56),
    description: "LongDescription".repeat(24),
    lifecycle: "archiving",
    version: "3",
    operation_id: id(23),
  },
  {
    id: id(14),
    name: "Retired.Project",
    lifecycle: "deleting",
    version: "5",
    operation_id: id(24),
  },
];
export const documentRow = (n = 20) => ({
  id: id(n),
  project_id: id(10),
  parent_document_id: n === 21 ? id(20) : null,
  title: n === 21 ? "长正文 / 阅读与滚动边界" : "统一界面设计与实施记录",
  content_version: "1",
  source_kind: "text",
  media_type: "text/markdown",
  status: "active",
  indexing_status: "failed",
  created_by: { kind: "human", user_id: id(1) },
  created_at: at,
  updated_at: at,
});
export const documentBody = [
  "# 统一界面设计",
  "正文仅作为文本展示：<img src=x onerror=alert(1)>",
  "A".repeat(180),
  ...Array.from(
    { length: 36 },
    (_, n) => `${n + 1}. 明确宽度与滚动职责，保留真实的业务状态与恢复入口。`,
  ),
].join("\n\n");
export const sprintID = id(61);
const workBase = (n: number, title: string) => ({
  id: id(n),
  project_id: id(10),
  title,
  manual_rank: "8" + "0".repeat(31),
  version: "1",
  created_at: at,
  updated_at: at,
});
const milestone = {
  ...workBase(60, "发布里程碑"),
  description: "跨页面规范与布局一致性",
};
const sprint = {
  ...workBase(61, "当前迭代 / 布局与交互"),
  milestone_id: id(60),
  state: "current",
  description: "保持真实工作流程与恢复契约。",
  started_at: at,
  started_by: { kind: "human", user_id: id(1) },
  completed_at: null,
  completed_by: null,
};
const tasks = [
  "backlog",
  "todo",
  "in_progress",
  "blocked",
  "in_review",
  "done",
  "cancelled",
].map((state, index) => ({
  ...workBase(
    70 + index,
    index === 2
      ? "长任务名称与正文验证 / " + "Task boundary ".repeat(8)
      : `布局检查 ${index + 1}`,
  ),
  milestone_id: id(60),
  sprint_id: sprintID,
  description: "保留描述与任务上下文。",
  plan: "按布局规范逐页核验。",
  type: "task",
  priority: "high",
  state,
  assignee_agent_id: ["todo", "in_progress", "in_review"].includes(state)
    ? id(90)
    : null,
}));
export const artifactRecord = {
  artifact_ref: { project_id: id(10), artifact_id: id(100), file_id: id(1100) },
  kind: "generated",
  name: "布局评审记录.txt",
  description: "长正文阅读与返回路径",
  object: {
    media_type: "text/plain",
    byte_size: String(new TextEncoder().encode(documentBody).length),
    sha256: "sha256:" + "a".repeat(64),
  },
  created_by_user_id: id(1),
  created_at: at,
  version: "1",
};
export const provider = {
  id: id(200),
  input: {
    name: "Research Provider / 共享服务",
    protocol: "openai-chat-completions",
    base_url: "https://provider.example.test/v1",
    enabled: true,
    credential_ref: null,
    options: {},
  },
  version: "1",
  created_at: at,
  updated_at: at,
};
const skill = {
  id: id(400),
  project_id: id(10),
  name: "Interface Review",
  normalized_name: "interface review",
  description: "核对界面布局、状态与交互。",
  protected: false,
  current_revision: "1",
  version: "1",
};
const variable = {
  id: id(401),
  project_id: id(10),
  type: "variable",
  name: "ENVIRONMENT",
  description: "普通配置，可在明确编辑后修改。",
  version: "1",
  created_at: at,
  updated_at: at,
};
const secret = {
  id: id(402),
  project_id: id(10),
  type: "secret",
  name: "SERVICE_TOKEN",
  description: "仅元数据，不含值。",
  version: "1",
  created_at: at,
  updated_at: at,
};
export async function installFixture(
  page: Page,
  options: { anonymous?: boolean; knowledgeText?: string } = {},
) {
  const state = {
    user: { ...user },
    items: [...items],
    tasks: [...tasks],
    milestones: [milestone],
    sprints: [sprint],
    mode: "ready" as "ready" | "empty" | "error" | "hold",
    release: () => {},
    requests: [] as { method: string; path: string; body?: unknown }[],
    unexpected: [] as string[],
    errors: [] as string[],
    consoleErrors: [] as string[],
  };
  page.on("pageerror", (error) => state.errors.push(error.message));
  page.on("console", (entry) => {
    if (
      entry.type() === "error" &&
      !/Failed to load resource|net::ERR_FAILED/.test(entry.text())
    )
      state.consoleErrors.push(entry.text());
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    let body: any;
    try {
      body = request.postDataJSON();
    } catch {
      body = undefined;
    }
    state.requests.push({ method, path, body });
    const json = (value: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        headers: { "X-Request-ID": id(99) },
        body: JSON.stringify(value),
      });
    const problem = (status: number) =>
      json(
        {
          type: "urn:agenteam:problem:fixture",
          title: "Fixture response",
          status,
          detail: "",
          instance: path,
          code: status === 401 ? "UNAUTHENTICATED" : "DEPENDENCY_UNAVAILABLE",
          request_id: id(99),
          commit_state: "not_started",
        },
        status,
      );
    if (path === "/api/v1/session" && method === "GET")
      return options.anonymous
        ? problem(401)
        : json({
            user: state.user,
            session: {
              id: id(2),
              issued_at: at,
              absolute_expires_at: "2027-10-14T00:00:00.000001Z",
              idle_expires_at: "2027-10-14T00:00:00.000001Z",
            },
            csrf_token: "S".repeat(43),
          });
    if (path === "/api/v1/auth/bootstrap" && method === "GET")
      return json({
        csrf_token: "A".repeat(43),
        challenge_modes: ["rotate"],
        delivery_channel: "backend_log",
      });
    if (path === "/api/v1/me" && method === "GET")
      return json({ user: state.user, avatar: null });
    if (path === "/api/v1/me/preferences" && method === "GET")
      return json({ theme: state.user.theme, version: state.user.version });
    if (path === "/api/v1/projects" && method === "GET") {
      if (state.mode === "hold")
        await new Promise<void>((resolve) => {
          state.release = resolve;
        });
      if (state.mode === "error") return problem(503);
      const filter = url.searchParams.getAll("lifecycle");
      return json({
        items:
          state.mode === "empty"
            ? []
            : state.items.filter(
                (item) => !filter.length || filter.includes(item.lifecycle),
              ),
        next_cursor: null,
      });
    }
    if (
      (path === "/api/v1/projects/resolve" || path === prefix) &&
      method === "GET"
    )
      return json(project);
    if (path === "/api/v1/system/users" && method === "GET")
      return json({
        items: [
          {
            ...user,
            id: id(31),
            username: "research",
            display_name: "Research Collaboration / 长名称与角色",
            email: "research@example.test",
            created_at: at,
          },
          { ...user, id: id(30), created_at: at },
        ],
      });
    if (method === "GET" && path === `${prefix}/milestones`)
      return json({
        items: state.milestones.map(({ description, ...row }) => row),
      });
    if (
      method === "GET" &&
      state.milestones.some((row) => path === `${prefix}/milestones/${row.id}`)
    )
      return json(
        state.milestones.find(
          (row) => path === `${prefix}/milestones/${row.id}`,
        ),
      );
    if (method === "GET" && path === `${prefix}/sprints`)
      return json({
        items: state.sprints
          .filter(
            (row) =>
              !url.searchParams.get("milestone_id") ||
              row.milestone_id === url.searchParams.get("milestone_id"),
          )
          .map(
            ({
              description,
              started_at,
              started_by,
              completed_at,
              completed_by,
              ...row
            }) => row,
          ),
      });
    if (
      method === "GET" &&
      state.sprints.some((row) => path === `${prefix}/sprints/${row.id}`)
    )
      return json(
        state.sprints.find((row) => path === `${prefix}/sprints/${row.id}`),
      );
    if (method === "GET" && path === `${prefix}/agents`)
      return json({
        items: [
          {
            id: id(90),
            project_id: id(10),
            name: "reviewer",
            display_name: "评审与实现",
            tag_color: null,
            description: "",
            version: "1",
            created_at: at,
            updated_at: at,
          },
        ],
      });
    if (method === "GET" && path === `${prefix}/agents/${id(90)}`)
      return json({
        id: id(90),
        project_id: id(10),
        name: "reviewer",
        display_name: "评审与实现",
        tag_color: null,
        description: "",
        version: "1",
        created_at: at,
        updated_at: at,
      });
    if (method === "GET" && path === `${prefix}/tasks`)
      return json({
        items: state.tasks
          .filter((task) => task.state === url.searchParams.get("state"))
          .map(({ description, plan, ...summary }) => summary),
      });
    const task = state.tasks.find(
      (task) => path === `${prefix}/tasks/${task.id}`,
    );
    if (method === "GET" && task) return json(task);
    if (
      method === "GET" &&
      state.tasks.some(
        (task) =>
          path === `${prefix}/tasks/${task.id}/blockers` ||
          path === `${prefix}/tasks/${task.id}/events`,
      )
    )
      return json({ items: [] });
    if (method === "GET" && path === `${prefix}/artifacts`)
      return json({ items: [artifactRecord] });
    if (
      method === "GET" &&
      path === `${prefix}/artifacts/${id(100)}/files/${id(1100)}/content`
    )
      return json({
        artifact: artifactRecord,
        projection: "text",
        text: documentBody,
        offset: "0",
        next_offset: String(new TextEncoder().encode(documentBody).length),
        truncated: false,
      });
    if (method === "GET" && path === "/api/v1/system/model-providers")
      return json({ items: [provider], next_cursor: null });
    if (
      method === "GET" &&
      path === `/api/v1/system/model-providers/${provider.id}`
    )
      return json(provider);
    if (method === "GET" && path === "/api/v1/system/models")
      return json({ items: [], next_cursor: null });
    if (method === "GET" && path === "/api/v1/system/runners")
      return json({ items: [], next: null });
    if (method === "GET" && path === "/api/v1/system/account-settings")
      return json({
        id: id(300),
        version: "1",
        session_idle_seconds: "604800",
        session_absolute_seconds: "2592000",
        password_reset_seconds: "1800",
        challenge_after_failures: "5",
        lifetime_changes_apply_to: "newly_issued_sessions_and_tokens",
      });
    if (method === "GET" && path === "/api/v1/system/smtp")
      return json({
        id: id(301),
        version: "1",
        configured: true,
        host: "mail.example.test",
        port: 587,
        encryption: "starttls",
        username: "",
        sender_email: "sender@example.test",
        sender_name: "Agenteam",
        credential_present: false,
        auto_retry_count: "3",
        retry_interval_seconds: "60",
      });
    if (method === "GET" && path === "/api/v1/system/outbound-policy")
      return json({
        version: "1",
        rules: [{ cidr: "10.0.0.0/8", ports: [443], allow_http: false }],
      });
    if (method === "GET" && path === "/api/v1/system/invitations")
      return json({ items: [] });
    if (method === "GET" && path === "/api/v1/system/audit")
      return json({ items: [], next_cursor: null });
    if (method === "GET" && path === "/api/v1/system/model-selection")
      return json({ id: id(302), version: "1", configured: null });
    if (
      method === "GET" &&
      path === "/api/v1/system/model-selection/meeting-summary"
    )
      return json({ id: id(303), version: "1", model: null });
    if (method === "GET" && path === "/api/v1/system/runtime-information")
      return json({
        observed_at: at,
        central: { version: null, safe_reason: "build_version_not_recorded" },
        database: {
          status: "available",
          safe_reason: null,
          last_success: {
            checked_at: at,
            received_at: at,
            postgresql_version: "PostgreSQL fixture",
            pgvector_version: "pgvector fixture",
          },
        },
        object_storage: {
          backend: "minio",
          assessment: "object_storage_aggregate",
          status: "available",
          safe_reason: null,
          last_success_received_at: at,
          details: "not_reported",
        },
        readiness: { ready: false, safe_reason: "DEPENDENCY_UNAVAILABLE" },
      });
    if (method === "GET" && path === `${prefix}/scheduler`)
      return json({
        project_id: id(10),
        version: "1",
        scheduler_enabled: true,
        scheduler_max_concurrency: null,
      });
    if (method === "GET" && path === `${prefix}/variables`)
      return json({ items: [variable] });
    if (method === "GET" && path === `${prefix}/variables/${variable.id}`)
      return json({ ...variable, value: "development" });
    if (method === "GET" && path === `${prefix}/secret-variables`)
      return json({ items: [secret] });
    if (method === "GET" && path === `${prefix}/secret-variables/${secret.id}`)
      return json(secret);
    if (method === "GET" && path === `${prefix}/skills/catalog`)
      return json({ items: [skill] });
    if (method === "GET" && path === `${prefix}/skills/${skill.id}`)
      return json(skill);
    if (method === "GET" && path === `${prefix}/model-providers`)
      return json({
        items: [
          { ...provider, scope: { kind: "project", project_id: id(10) } },
        ],
        next_cursor: null,
      });
    if (method === "GET" && path === `${prefix}/available-chat-models`)
      return json({ items: [], next_cursor: null });
    if (
      method === "GET" &&
      (path === `${prefix}/audit` || path === `${prefix}/model-usage`)
    )
      return json({ items: [], next_cursor: null });
    if (method === "POST" && path === "/api/v1/projects") {
      const created = {
        ...project,
        id: body.project_id,
        name: body.name,
        normalized_name: body.name.toLowerCase(),
        description: body.description ?? "",
      };
      state.items.push({
        id: created.id,
        name: created.name,
        description: created.description,
        lifecycle: "active",
        version: "1",
      });
      return json({ state: "ready", project: created }, 201);
    }
    if (method === "POST" && path === `${prefix}/tasks`) {
      const { task_id, ...fields } = body.request;
      const created = {
        ...tasks[0],
        ...fields,
        id: task_id,
        state: "backlog",
        assignee_agent_id: null,
        version: "1",
      };
      state.tasks.push(created);
      return json({
        task: created,
        changed: true,
        task_event_id: id(500),
        event_ids: [id(501)],
      });
    }
    if (method === "POST" && path === `${prefix}/milestones`) {
      const { milestone_id, ...fields } = body.request;
      const created = {
        ...milestone,
        ...fields,
        description: fields.description ?? "",
        id: milestone_id,
      };
      state.milestones.push(created);
      return json({
        command: "work.milestone.create",
        changed: true,
        event_id: id(502),
        milestone: created,
        sprint: null,
      });
    }
    if (method === "POST" && path === `${prefix}/sprints`) {
      const { sprint_id, ...fields } = body.request;
      const created = {
        ...sprint,
        ...fields,
        description: fields.description ?? "",
        id: sprint_id,
        state: "planned",
        started_at: null,
        started_by: null,
      };
      state.sprints.push(created);
      return json({
        command: "work.sprint.create",
        changed: true,
        event_id: id(503),
        milestone: null,
        sprint: created,
      });
    }
    if (method === "GET" && path === `${prefix}/available-runners`)
      return json({ items: [] });
    if (method === "POST" && path === "/api/v1/invitations/inspect")
      return json({
        email: "long.invited.workspace.member.with.detailed.name@example.test",
        expires_at: "2027-10-14T00:00:00.000001Z",
      });
    if (method === "POST" && path === "/api/v1/password-resets/inspect")
      return json({ valid: true, expires_at: "2027-10-14T00:00:00.000001Z" });
    if (method === "POST" && path === `${prefix}/secret-variables`)
      return json(
        {
          type: "urn:agenteam:problem:fixture",
          title: "Unknown",
          detail: "",
          status: 503,
          code: "COMMIT_UNKNOWN",
          instance: path,
          request_id: id(99),
          commit_state: "unknown",
        },
        503,
      );
    if (
      method === "POST" &&
      path === `${prefix}/secret-variables/commands/lookup`
    )
      return json({ status: "not_observed", receipt: null });
    const knowledge = `${prefix}/knowledge/documents`;
    if (path === `${knowledge}/children`)
      return json({
        items:
          url.searchParams.get("parent_document_id") === "null"
            ? [documentRow()]
            : [documentRow(21)],
      });
    const n = path.includes(id(21)) ? 21 : 20;
    if (path === `${knowledge}/${id(n)}/ancestors`)
      return json({ items: n === 21 ? [documentRow()] : [] });
    if (path === `${knowledge}/${id(n)}/content`)
      return json({
        document: documentRow(n),
        text: {
          text: options.knowledgeText ?? documentBody,
          next_byte_offset: String(
            new TextEncoder().encode(options.knowledgeText ?? documentBody)
              .length,
          ),
          truncated: false,
        },
      });
    if (path === `${knowledge}/${id(n)}`)
      return json({ active: documentRow(n) });
    state.unexpected.push(`${method} ${path}`);
    return problem(503);
  });
  return state;
}
export const routes = [
  {
    key: "project-general",
    path: `${projectPath}/settings/general`,
    heading: "基本信息",
  },
  {
    key: "project-skills",
    path: `${projectPath}/settings/skills`,
    heading: "项目技能库",
  },
  {
    key: "project-skill-detail",
    path: `${projectPath}/settings/skills/${id(400)}`,
    heading: "技能详情",
  },
  {
    key: "project-scheduler",
    path: `${projectPath}/settings/scheduler`,
    heading: "调度配置",
  },
  {
    key: "project-variables",
    path: `${projectPath}/settings/variables`,
    heading: "普通变量",
  },
  {
    key: "project-secrets",
    path: `${projectPath}/settings/secrets`,
    heading: "Secrets",
  },
  {
    key: "project-agents",
    path: `${projectPath}/settings/agents`,
    heading: "Agent",
  },
  {
    key: "project-audit",
    path: `${projectPath}/settings/audit`,
    heading: "项目审计",
  },
  {
    key: "project-providers",
    path: `${projectPath}/settings/model-providers`,
    heading: "Providers",
  },
  {
    key: "project-available-models",
    path: `${projectPath}/settings/available-models`,
    heading: "可用模型",
  },
  {
    key: "project-usage",
    path: `${projectPath}/settings/model-usage`,
    heading: "调用记录",
  },
  {
    key: "system-invitations",
    path: "/system/invitations",
    heading: "待注册邀请",
  },
  { key: "system-audit", path: "/system/audit", heading: "系统审计" },
  {
    key: "system-model-selection",
    path: "/system/model-selection",
    heading: "平台模型用途",
  },
  {
    key: "system-runtime-information",
    path: "/system/runtime-information",
    heading: "运行信息",
  },
  { key: "tasks", path: `${projectPath}/tasks`, heading: "任务" },
  {
    key: "task-sprint",
    path: `${projectPath}/tasks/sprints/${sprintID}`,
    heading: "任务",
  },
  {
    key: "task-detail",
    path: `${projectPath}/tasks/${id(70)}`,
    heading: "任务",
  },
  { key: "artifacts", path: `${projectPath}/artifacts`, heading: "产物" },
  { key: "system-providers", path: "/system/providers", heading: "Providers" },
  { key: "system-models", path: "/system/models", heading: "Models" },
  { key: "system-runners", path: "/system/runners", heading: "Runner 设备" },
  {
    key: "system-account-security",
    path: "/system/account-security",
    heading: "账号安全",
  },
  { key: "system-smtp", path: "/system/smtp", heading: "SMTP" },
  {
    key: "system-outbound-policy",
    path: "/system/outbound-policy",
    heading: "出站规则",
  },
  { key: "home", path: "/", heading: "首页" },
  { key: "projects", path: "/projects", heading: "项目" },
  { key: "project-home", path: projectPath, heading: "Atlas" },
  { key: "login", path: "/login", heading: "登录", anonymous: true },
  { key: "invite", path: "/invite", heading: "接受邀请", anonymous: true },
  {
    key: "forgot",
    path: "/forgot-password",
    heading: "找回密码",
    anonymous: true,
  },
  {
    key: "reset",
    path: "/reset-password",
    heading: "重置密码",
    anonymous: true,
  },
  {
    key: "not-found",
    path: "/missing/route/does-not-exist",
    heading: "未找到页面",
  },
  { key: "profile", path: "/settings/profile", heading: "基本资料" },
  { key: "appearance", path: "/settings/appearance", heading: "主题" },
  { key: "password", path: "/settings/password", heading: "修改密码" },
  { key: "system-users", path: "/system/users", heading: "用户" },
  { key: "knowledge", path: `${projectPath}/knowledge`, heading: "知识库" },
  {
    key: "knowledge-document",
    path: `${projectPath}/knowledge/${id(21)}`,
    heading: "知识库",
  },
];
