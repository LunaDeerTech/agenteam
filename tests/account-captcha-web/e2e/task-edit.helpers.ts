import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type EditMaterial = {
  mode: "changed-noop-clear-and-ready" | "lost-edit-confirmation-and-ready";
  cookie: string;
  project_id: string;
  sprint_id: string;
  task_id: string;
  worker_id: string;
  route: string;
  initial: { title: string; description: string; plan: string };
};
export function editMaterial(): EditMaterial {
  return JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-edit-material.json"),
      "utf8",
    ),
  );
}
export function editInputs(data: EditMaterial) {
  const title = `${data.initial.title} — edited`;
  return data.mode === "lost-edit-confirmation-and-ready"
    ? [{ expected_version: "1", request: { title, description: "" } }]
    : [
        {
          expected_version: "1",
          request: {
            title,
            description: "Browser saved description.",
            type: "feature",
            priority: "high",
            plan: "Browser saved plan.",
          },
        },
        { expected_version: "2", request: { title } },
        { expected_version: "2", request: { description: "", plan: "" } },
      ];
}

export async function observeEdit(page: Page, data: EditMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const inputs = editInputs(data);
  let updates = 0;
  const keys = new Set<string>();
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
      throw Error("UNEXPECTED_EDIT_SCOPE");
    const path = pathname.slice(prefix.length);
    if (method === "PATCH" && path === `/tasks/${data.task_id}`) {
      // Observe the real Session's already-frozen original intent. The fixture
      // never supplies its key or repeats the patch after loss of confirmation.
      expect(updates).toBeLessThan(inputs.length);
      expect(request.postDataJSON()).toEqual(inputs[updates]);
      const key = request.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      keys.add(key);
      updates++;
      return "update";
    }
    if (method === "POST" && path === "/task-commands/lookup") return "lookup";
    if (method === "POST" && path === `/tasks/${data.task_id}/transfer`)
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
      if (
        parts.length === 3 &&
        /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          parts[2]!,
        )
      ) {
        if (parts[1] === "milestones") return "milestone";
        if (parts[1] === "sprints" && parts[2] === data.sprint_id)
          return "sprint";
        if (parts[1] === "agents" && parts[2] === data.worker_id)
          return "agent";
        if (parts[1] === "tasks" && parts[2] === data.task_id) return "task";
      }
      if (path === `/tasks/${data.task_id}/blockers`) return "blockers";
    }
    throw Error("UNOBSERVED_TASK_EDIT_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_TASK_EDIT_WEB_DIST!,
    evidence: process.env.AGENTEAM_TASK_EDIT_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_TASK_EDIT_WEB_INPUT_HASH!,
    prefix: "task-edit",
    endpoint: classify,
    cut:
      data.mode === "lost-edit-confirmation-and-ready" ? "update" : undefined,
    lookupCommand: "work.task.update",
  });
  return {
    ...seen,
    requireSeparateReady() {
      expect(seen.requests("update")).toHaveLength(inputs.length);
      const ready = seen.requests("ready");
      expect(ready).toHaveLength(1);
      const key = ready[0]!.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      expect(ready[0]!.postDataJSON()).toEqual({
        expected_version:
          data.mode === "lost-edit-confirmation-and-ready" ? "2" : "3",
        request: { target_state: "todo", assignee_agent_id: data.worker_id },
      });
      expect(seen.requests("lookup")).toHaveLength(
        data.mode === "lost-edit-confirmation-and-ready" ? 1 : 0,
      );
    },
  };
}

export async function editScreenshot(
  page: Page,
  data: EditMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const directory = process.env.AGENTEAM_TASK_EDIT_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `task-edit-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
