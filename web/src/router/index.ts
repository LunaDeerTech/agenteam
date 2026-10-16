import { createRouter, createWebHistory } from "vue-router";
import { routes, type SettingsScope } from "./routes";
import "../account/link";
import { accountEnabled } from "../account/transport";
import { ensureSession, useSession } from "../account/session";
import { confirmAccountLeave } from "../account/leave";

declare module "vue-router" {
  interface RouteMeta {
    title?: string;
    description?: string;
    layout?: "app" | "account";
    settingsScope?: SettingsScope;
    settingKey?: string;
    projectContext?: boolean;
    group?: string;
    order?: number;
    content?: "unimplemented";
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
});
const reservedOwners = new Set([
  "system",
  "settings",
  "mcp",
  "api",
  "assets",
  "login",
  "invite",
  "forgot-password",
  "reset-password",
  "projects",
]);
router.beforeEach(async (to, from) => {
  const session = useSession();
  if (
    from.matched.length &&
    from.path !== to.path &&
    !(from.meta.layout !== "account" && session.status.value === "anonymous") &&
    !(await confirmAccountLeave())
  )
    return false;
  if (
    to.meta.projectContext &&
    reservedOwners.has(String(to.params.owner).toLowerCase())
  )
    return {
      name: "not-found",
      params: { pathMatch: to.path.slice(1).split("/") },
      replace: true,
    };
  if (!accountEnabled) return;
  await ensureSession();
  if (to.meta.layout !== "account" || to.name === "mcp-oauth-callback") {
    if (session.status.value === "anonymous")
      return { name: "login", query: { return: to.path }, replace: true };
  }
});
router.afterEach((to) => {
  document.title = `${to.meta.title || "agenteam"} · agenteam`;
});
