import { createRouter, createWebHistory } from "vue-router";
import { routes, type SettingsScope } from "./routes";

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
router.beforeEach((to) => {
  if (
    to.meta.projectContext &&
    reservedOwners.has(String(to.params.owner).toLowerCase())
  )
    return {
      name: "not-found",
      params: { pathMatch: to.path.slice(1).split("/") },
      replace: true,
    };
});
router.afterEach((to) => {
  document.title = `${to.meta.title || "agenteam"} · agenteam`;
});
