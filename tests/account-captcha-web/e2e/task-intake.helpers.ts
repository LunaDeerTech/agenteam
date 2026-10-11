import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type IntakeMaterial = {
  mode: "create-and-ready" | "lost-create-confirmation-and-ready";
  cookie: string;
  project_id: string;
  sprint_id: string;
  worker_id: string;
  route: string;
  draft: {
    title: string;
    description: string;
    plan: string;
    type: "task";
    priority: "medium";
  };
};
export function intakeMaterial(): IntakeMaterial {
  return JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-intake-material.json"),
      "utf8",
    ),
  );
}

const uuid7 =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export async function observeIntake(page: Page, data: IntakeMaterial) {
  let taskID: string | undefined;
  const prefix = `/api/v1/projects/${data.project_id}`;
  const classify = (request: Request): string | undefined => {
    const { pathname } = new URL(request.url());
    if (!pathname.startsWith("/api/")) return undefined;
    const method = request.method();
    const global: Record<string, string> = {
      "GET /api/v1/session": "session",
      "GET /api/v1/me": "profile",
      "GET /api/v1/me/preferences": "preferences",
      "GET /api/v1/auth/bootstrap": "bootstrap",
      "GET /api/v1/projects/resolve": "resolve",
      "GET /api/v1/projects": "projects",
    };
    if (global[`${method} ${pathname}`]) return global[`${method} ${pathname}`];
    if (method === "GET" && pathname === prefix) return "project";
    if (!pathname.startsWith(prefix + "/"))
      throw Error("UNEXPECTED_INTAKE_SCOPE");
    const path = pathname.slice(prefix.length);
    if (method === "POST" && path === "/tasks") {
      // Observe the UUID/key already frozen by the real Session before send.
      // The fixture never supplies an ID to the product or constructs its command.
      const body = request.postDataJSON();
      expect(Object.keys(body)).toEqual(["request"]);
      expect(body.request.task_id).toMatch(uuid7);
      expect(body.request).toEqual({
        task_id: body.request.task_id,
        sprint_id: data.sprint_id,
        ...data.draft,
      });
      if (taskID) throw Error("CREATE_WAS_RETRANSMITTED");
      taskID = body.request.task_id;
      return "create";
    }
    if (method === "POST" && path === "/task-commands/lookup") return "lookup";
    if (method === "POST" && taskID && path === `/tasks/${taskID}/transfer`)
      return "ready";
    if (method === "GET") {
      const simple: Record<string, string> = {
        "/milestones": "milestones",
        "/sprints": "sprints",
        "/tasks": "tasks",
        "/agents": "agents",
      };
      if (simple[path]) return simple[path];
      const parts = path.split("/");
      if (parts.length === 3 && uuid7.test(parts[2]!)) {
        if (parts[1] === "milestones") return "milestone";
        if (parts[1] === "sprints" && parts[2] === data.sprint_id)
          return "sprint";
        if (parts[1] === "agents" && parts[2] === data.worker_id)
          return "agent";
        if (parts[1] === "tasks" && parts[2] === taskID) return "task";
      }
      if (taskID && path === `/tasks/${taskID}/blockers`) return "blockers";
    }
    throw Error("UNOBSERVED_TASK_INTAKE_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_TASK_INTAKE_WEB_DIST!,
    evidence: process.env.AGENTEAM_TASK_INTAKE_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_TASK_INTAKE_WEB_INPUT_HASH!,
    prefix: "task-intake",
    endpoint: classify,
    cut:
      data.mode === "lost-create-confirmation-and-ready" ? "create" : undefined,
    lookupCommand: "work.task.create",
  });
  return {
    ...seen,
    taskID() {
      expect(taskID).toMatch(uuid7);
      return taskID!;
    },
    requireSeparateReady() {
      const creates = seen.requests("create"),
        ready = seen.requests("ready");
      expect(creates).toHaveLength(1);
      expect(ready).toHaveLength(1);
      const createKey = creates[0]!.headers()["idempotency-key"],
        readyKey = ready[0]!.headers()["idempotency-key"];
      expect(createKey).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(readyKey).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(readyKey).not.toBe(createKey);
      expect(ready[0]!.postDataJSON()).toEqual({
        expected_version: "1",
        request: { target_state: "todo", assignee_agent_id: data.worker_id },
      });
      expect(seen.requests("lookup")).toHaveLength(
        data.mode === "create-and-ready" ? 0 : 1,
      );
    },
  };
}

export async function intakeScreenshot(
  page: Page,
  data: IntakeMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const directory = process.env.AGENTEAM_TASK_INTAKE_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `task-intake-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
