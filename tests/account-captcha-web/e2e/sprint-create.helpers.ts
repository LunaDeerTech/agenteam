import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type SprintCreateMaterial = {
  mode: "plan-another-sprint-with-existing-current";
  cookie: string;
  project_id: string;
  milestone_id: string;
  milestone_title: string;
  current_sprint_id: string;
  current_sprint_title: string;
  project_version: string;
  route: string;
  title: string;
  description: string;
  task_title: string;
};
export function sprintCreateMaterial(): SprintCreateMaterial {
  const value: SprintCreateMaterial = JSON.parse(readFileSync(join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "sprint-create-material.json"), "utf8"));
  expect(value.mode).toBe("plan-another-sprint-with-existing-current");
  for (const v of [value.project_id, value.milestone_id, value.current_sprint_id]) expect(v).toMatch(id);
  expect(value.project_version).toMatch(/^[1-9][0-9]*$/);
  expect(value.route).toMatch(/^\/[^/?#]+\/[^/?#]+\/tasks$/);
  for (const v of [value.milestone_title, value.current_sprint_title, value.title, value.description, value.task_title]) expect(v.length).toBeGreaterThan(0);
  return value;
}

export async function observeSprintCreate(page: Page, data: SprintCreateMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  let createdID: string | undefined;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()), path = url.pathname, method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    if (method === "POST" && path === `${prefix}/sprints` && !url.search) {
      expect(createdID, "only the first original Create may dispatch").toBeUndefined();
      const body = request.postDataJSON();
      expect(body?.request?.sprint_id).toMatch(id);
      expect(body.request.sprint_id).not.toBe(data.current_sprint_id);
      expect(body).toEqual({ request: { sprint_id: body.request.sprint_id, milestone_id: data.milestone_id, title: data.title, description: data.description } });
      createdID = body.request.sprint_id;
      return "create";
    }
    if (method !== "GET") throw Error("UNOBSERVED_SPRINT_CREATE_WRITE");
    const globals: Record<string, string> = {
      "/api/v1/session": "session", "/api/v1/me": "profile", "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap", "/api/v1/projects/resolve": "resolve", "/api/v1/projects": "projects",
    };
    if (globals[path]) return globals[path];
    if (path === prefix && !url.search) return "project";
    if (!path.startsWith(prefix + "/")) throw Error("UNEXPECTED_SPRINT_CREATE_SCOPE");
    const suffix = path.slice(prefix.length);
    if (suffix === "/milestones" || suffix === "/agents") {
      expect([...url.searchParams.keys()].sort()).toEqual(["limit"]);
      expect(url.searchParams.get("limit")).toBe("50");
      return suffix.slice(1);
    }
    if (suffix === "/sprints") {
      expect([...url.searchParams.keys()].sort()).toEqual(["limit", "milestone_id"]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("milestone_id")).toBe(data.milestone_id);
      return "sprints";
    }
    if (suffix === "/tasks") {
      expect([...url.searchParams.keys()].sort()).toEqual(["limit", "sprint_id", "state"]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect([data.current_sprint_id, createdID]).toContain(url.searchParams.get("sprint_id"));
      expect(["backlog", "todo", "in_progress", "blocked", "in_review", "done", "cancelled"]).toContain(url.searchParams.get("state"));
      return "tasks";
    }
    if ([`/sprints/${data.current_sprint_id}`, `/sprints/${createdID}`].includes(suffix) && !url.search) return "sprint";
    if (suffix === `/milestones/${data.milestone_id}` && !url.search) return "milestone";
    throw Error("UNOBSERVED_SPRINT_CREATE_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_SPRINT_CREATE_WEB_DIST!, evidence: process.env.AGENTEAM_SPRINT_CREATE_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_SPRINT_CREATE_WEB_INPUT_HASH!, prefix: "sprint-create", endpoint: classify,
    lookupCommand: "work.sprint.create", sprintCreateWrites: true,
  });
  return {
    ...seen,
    createdID() { expect(createdID).toMatch(id); return createdID!; },
    requireResult(originalProject: Record<string, any>, originalParent: Record<string, any>, originalSprint: Record<string, any>) {
      expect(seen.requests("create")).toHaveLength(1);
      expect(seen.requests("lookup")).toHaveLength(0);
      expect(seen.requests("start")).toHaveLength(0);
      const result = seen.receipt("create", 0);
      expect(Object.keys(result).sort()).toEqual(["changed", "command", "event_id", "milestone", "sprint"]);
      expect(result.command).toBe("work.sprint.create");
      expect(result.changed).toBe(true);
      expect(result.milestone).toBeNull();
      expect(result.event_id).toMatch(id);
      const sprint = result.sprint;
      expect(Object.keys(sprint).sort()).toEqual(["completed_at", "completed_by", "created_at", "description", "id", "manual_rank", "milestone_id", "project_id", "started_at", "started_by", "state", "title", "updated_at", "version"]);
      expect(sprint).toEqual({
        id: createdID, project_id: data.project_id, milestone_id: data.milestone_id, title: data.title, description: data.description,
        manual_rank: sprint.manual_rank, version: "1", state: "planned", created_at: sprint.created_at, updated_at: sprint.created_at,
        started_at: null, started_by: null, completed_at: null, completed_by: null,
      });
      expect(typeof sprint.manual_rank).toBe("string");
      expect(sprint.manual_rank).toMatch(/^[0-9a-f]{32}$/);
      expect(BigInt("0x" + sprint.manual_rank)).toBeGreaterThan(BigInt("0x" + originalSprint.manual_rank));
      expect(typeof sprint.created_at).toBe("string");
      // These later reads establish publication. The original public Create
      // Promise and same-request Go commit proof were already required by verify.
      const { started_by, completed_by, ...detail } = sprint;
      expect(seen.sprintCreateRead("sprint")).toEqual(detail);
      expect(seen.sprintCreateRead("project")).toEqual(originalProject);
      expect(seen.sprintCreateRead("milestone")).toEqual(originalParent);
      const summary = (s: Record<string, any>) => {
        const { description, started_at, completed_at, started_by, completed_by, ...value } = s;
        return value;
      };
      expect(seen.sprintCreateRead("sprints")).toEqual({ items: [summary(originalSprint), summary(sprint)] });
      const after = seen.requests("tasks").filter(r => new URL(r.url()).searchParams.get("sprint_id") === createdID);
      expect(new Set(after.map(r => new URL(r.url()).searchParams.get("state"))).size).toBe(7);
      return result;
    },
  };
}

export async function sprintCreateScreenshot(page: Page, data: SprintCreateMaterial, name: string) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  const directory = process.env.AGENTEAM_SPRINT_CREATE_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({ path: join(directory, `sprint-create-${data.mode}-${name}.png`), fullPage: true, animations: "disabled" });
}
