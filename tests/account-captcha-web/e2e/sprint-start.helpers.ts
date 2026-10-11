import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type SprintStartMaterial = {
  mode: "paused-start-and-current-board" | "lost-start-confirmation-only-lookup";
  cookie: string;
  project_id: string;
  sprint_id: string;
  version: string;
  project_version: string;
  route: string;
  title: string;
  task_id: string;
  task_title: string;
};
export function sprintStartMaterial(): SprintStartMaterial {
  const value: SprintStartMaterial = JSON.parse(readFileSync(join(
    process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "sprint-start-material.json"), "utf8"));
  const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  for (const v of [value.project_id, value.sprint_id, value.task_id]) expect(v).toMatch(id);
  for (const v of [value.version, value.project_version]) expect(v).toMatch(/^[1-9][0-9]*$/);
  expect(value.route).toMatch(new RegExp(`^/[^/?#]+/[^/?#]+/tasks/sprints/${value.sprint_id}$`));
  expect(value.title.length).toBeGreaterThan(0);
  expect(value.task_title.length).toBeGreaterThan(0);
  return value;
}

export async function observeSprintStart(page: Page, data: SprintStartMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const original = { expected_version: data.version, request: {} };
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname, method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    if (method === "POST" && path === `${prefix}/sprints/${data.sprint_id}/start` && !url.search) {
      expect(request.postDataJSON()).toEqual(original);
      return "start";
    }
    if (method === "POST" && path === `${prefix}/sprint-lifecycle-commands/lookup` && !url.search) {
      expect(data.mode).toBe("lost-start-confirmation-only-lookup");
      expect(request.postDataJSON()).toEqual({ command: "work.sprint.start", target_id: data.sprint_id, ...original });
      return "lookup";
    }
    if (method !== "GET") throw Error("UNOBSERVED_SPRINT_START_WRITE");
    const globals: Record<string, string> = {
      "/api/v1/session": "session", "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences", "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects",
    };
    if (globals[path]) return globals[path];
    if (path === prefix && !url.search) return "project";
    if (!path.startsWith(prefix + "/")) throw Error("UNEXPECTED_SPRINT_START_SCOPE");
    const suffix = path.slice(prefix.length);
    const lists: Record<string, string> = { "/milestones": "milestones", "/sprints": "sprints", "/tasks": "tasks", "/agents": "agents" };
    if (lists[suffix]) {
      if (suffix === "/tasks") expect(url.searchParams.getAll("sprint_id")).toEqual([data.sprint_id]);
      return lists[suffix];
    }
    if (suffix === `/sprints/${data.sprint_id}` && !url.search) return "sprint";
    if (/^\/milestones\/[0-9a-f-]{36}$/.test(suffix) && !url.search) return "milestone";
    throw Error("UNOBSERVED_SPRINT_START_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_SPRINT_START_WEB_DIST!,
    evidence: process.env.AGENTEAM_SPRINT_START_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_SPRINT_START_WEB_INPUT_HASH!,
    prefix: "sprint-start", endpoint: classify,
    cut: data.mode === "lost-start-confirmation-only-lookup" ? "start" : undefined,
    lookupCommand: "work.sprint.start", sprintStartWrites: true,
  });
  return {
    ...seen,
    requireResult() {
      const lost = data.mode === "lost-start-confirmation-only-lookup";
      expect(seen.requests("start")).toHaveLength(1);
      expect(seen.requests("lookup")).toHaveLength(lost ? 1 : 0);
      if (lost) seen.originalLookup();
      const result = lost ? seen.receipt("lookup", 0).receipt : seen.receipt("start", 0);
      expect(Object.keys(result).sort()).toEqual(["event_id", "project", "sprint"]);
      expect(result.sprint).toMatchObject({ id: data.sprint_id, project_id: data.project_id, state: "current", version: String(BigInt(data.version) + 1n), completed_at: null, completed_by: null });
      expect(result.project).toMatchObject({ id: data.project_id, lifecycle: "active", current_sprint_id: data.sprint_id, version: String(BigInt(data.project_version) + 1n) });
      expect(result.sprint.started_by).toEqual({ kind: "human", user_id: result.project.owner_user_id });
      expect(result.sprint.updated_at).toBe(result.project.updated_at);
      // Later GETs establish current publication, never the original write proof.
      const { started_by, completed_by, ...detail } = result.sprint;
      expect(seen.sprintRead("sprint")).toEqual(detail);
      expect(seen.sprintRead("project")).toEqual({ ...result.project, archived_at: null });
      return result;
    },
  };
}
export async function sprintStartScreenshot(page: Page, data: SprintStartMaterial, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const directory = process.env.AGENTEAM_SPRINT_START_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `sprint-start-${data.mode}-${name}.png`), fullPage: true, animations: "disabled" });
}
