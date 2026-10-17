import type { RouteRecordRaw } from "vue-router";

export type SettingsScope = "system" | "personal" | "project";
export interface SettingsPage {
  key: string;
  title: string;
  group: string;
  icon: string;
}

export const systemSettings: readonly SettingsPage[] = [
  { key: "users", title: "用户", group: "用户与邀请", icon: "users" },
  {
    key: "invitations",
    title: "待注册邀请",
    group: "用户与邀请",
    icon: "mail",
  },
  { key: "models", title: "模型目录", group: "模型与 Provider", icon: "grid" },
  {
    key: "model-selection",
    title: "平台模型用途",
    group: "模型与 Provider",
    icon: "sliders",
  },
  { key: "runners", title: "Runner", group: "执行环境", icon: "monitor" },
  {
    key: "account-security",
    title: "账号安全",
    group: "安全与审计",
    icon: "shield",
  },
  {
    key: "outbound-policy",
    title: "出站规则",
    group: "安全与审计",
    icon: "globe",
  },
  { key: "audit", title: "系统审计", group: "安全与审计", icon: "history" },
  { key: "smtp", title: "SMTP", group: "平台配置", icon: "mail" },
  {
    key: "runtime-information",
    title: "运行信息",
    group: "平台配置",
    icon: "activity",
  },
];

export const projectSettings: readonly SettingsPage[] = [
  { key: "general", title: "基本信息", group: "", icon: "sliders" },
  { key: "agents", title: "Agent", group: "", icon: "users" },
  { key: "variables", title: "普通变量", group: "", icon: "code" },
  { key: "secrets", title: "Secrets", group: "", icon: "lock" },
  { key: "skills", title: "Skills", group: "", icon: "file" },
  { key: "mcp", title: "MCP", group: "", icon: "link" },
  { key: "scheduler", title: "Scheduler", group: "", icon: "clock" },
  { key: "audit", title: "项目审计", group: "安全记录", icon: "shield" },
  {
    key: "model-providers",
    title: "Providers",
    group: "模型与 Provider",
    icon: "server",
  },
  {
    key: "available-models",
    title: "可用模型",
    group: "模型与 Provider",
    icon: "grid",
  },
  {
    key: "model-usage",
    title: "调用记录",
    group: "模型与 Provider",
    icon: "activity",
  },
];

export const personalSettings: readonly SettingsPage[] = [
  { key: "profile", title: "个人资料", group: "", icon: "user" },
  { key: "appearance", title: "外观", group: "", icon: "sun" },
  { key: "password", title: "登录与安全", group: "", icon: "lock" },
];

export const projectNavigation = [
  { name: "project-home", title: "概览" },
  { name: "project-meetings", title: "会议" },
  { name: "project-inbox", title: "待办" },
  { name: "project-tasks", title: "任务" },
  { name: "project-knowledge", title: "知识库" },
  { name: "project-artifacts", title: "产物" },
  { name: "project-settings-general", title: "项目设置" },
] as const;

const emptyPage = () => import("../views/SkeletonPage.vue");
const projectBase = "/:owner/:project";
function settingsRoutes(
  scope: SettingsScope,
  pages: readonly SettingsPage[],
  prefix: string,
): RouteRecordRaw[] {
  return pages.map((page, order) => ({
    path: `${prefix}/${page.key}${scope === "project" && page.key === "skills" ? "/:skillId?" : ""}`,
    name: `${scope}-settings-${page.key}`,
    component:
      scope === "system" && page.key === "users"
        ? () => import("../views/system/UsersPage.vue")
        : scope === "system" && page.key === "invitations"
          ? () => import("../views/system/InvitationsPage.vue")
          : scope === "system" && page.key === "account-security"
            ? () => import("../views/system/AccountSecurityPage.vue")
            : scope === "system" && page.key === "smtp"
              ? () => import("../views/system/SmtpSettingsPage.vue")
              : scope === "system" && page.key === "outbound-policy"
                ? () => import("../views/system/OutboundPolicyPage.vue")
                : scope === "system" && page.key === "models"
                  ? () => import("../views/system/ModelCatalogPage.vue")
                  : scope === "system" && page.key === "model-selection"
                    ? () => import("../views/system/ModelSelectionPage.vue")
                    : scope !== "personal"
                      ? emptyPage
                      : page.key === "profile"
                        ? () => import("../views/ProfilePage.vue")
                        : page.key === "appearance"
                          ? () => import("../views/AppearancePage.vue")
                          : () => import("../views/PasswordPage.vue"),
    meta: {
      title: page.title,
      settingsScope: scope,
      settingKey: page.key,
      group: page.group,
      order,
      projectContext: scope === "project",
      layout: "app",
      content:
        scope === "personal" ||
        (scope === "system" &&
          [
            "users",
            "invitations",
            "account-security",
            "smtp",
            "outbound-policy",
            "models",
            "model-selection",
          ].includes(page.key))
          ? undefined
          : "unimplemented",
    },
  }));
}

export const routes: RouteRecordRaw[] = [
  {
    path: "/",
    name: "home",
    component: emptyPage,
    meta: { title: "首页", layout: "app", content: "unimplemented" },
  },
  {
    path: "/projects",
    name: "projects",
    component: emptyPage,
    meta: { title: "项目", layout: "app", content: "unimplemented" },
  },
  ...[
    ["login", "登录"],
    ["invite", "接受邀请"],
    ["forgot-password", "找回密码"],
    ["reset-password", "重置密码"],
  ].map(([path, title]): RouteRecordRaw => ({
    path: `/${path}`,
    name: path,
    component:
      path === "login"
        ? () => import("../views/LoginPage.vue")
        : path === "forgot-password"
          ? () => import("../views/ForgotPasswordPage.vue")
          : () => import("../views/LinkAccountPage.vue"),
    meta: {
      title,
      layout: "account",
      content: undefined,
    },
  })),
  {
    path: "/mcp/oauth/callback",
    name: "mcp-oauth-callback",
    component: emptyPage,
    meta: { title: "MCP 授权", layout: "account", content: "unimplemented" },
  },
  { path: "/system", redirect: { name: "system-settings-users" } },
  ...settingsRoutes("system", systemSettings, "/system"),
  {
    path: "/system/models/:providerId",
    name: "system-provider-detail",
    component: () => import("../views/system/ProviderDetailPage.vue"),
    meta: {
      title: "供应商",
      settingsScope: "system",
      settingKey: "models",
      layout: "app",
    },
  },
  { path: "/settings", redirect: { name: "personal-settings-profile" } },
  ...settingsRoutes("personal", personalSettings, "/settings"),
  {
    path: `${projectBase}/settings`,
    redirect: (to) => ({ name: "project-settings-general", params: to.params }),
  },
  ...settingsRoutes("project", projectSettings, `${projectBase}/settings`),
  ...[
    ["", "home", "概览"],
    ["/tasks", "tasks", "任务"],
    ["/tasks/sprints/:sprintId", "sprint", "任务"],
    ["/tasks/:taskId", "task-detail", "任务"],
    ["/knowledge/:documentId?", "knowledge", "知识库"],
    ["/artifacts", "artifacts", "产物"],
    ["/meetings/:meetingId?", "meetings", "会议"],
    ["/inbox", "inbox", "待办"],
  ].map(([suffix, name, title]): RouteRecordRaw => ({
    path: `${projectBase}${suffix}`,
    name: `project-${name}`,
    component: emptyPage,
    meta: {
      title,
      projectContext: true,
      layout: "app",
      content: "unimplemented",
    },
  })),
  {
    path: "/:pathMatch(.*)*",
    name: "not-found",
    component: emptyPage,
    meta: { title: "页面不存在", layout: "app", content: "unimplemented" },
  },
];
