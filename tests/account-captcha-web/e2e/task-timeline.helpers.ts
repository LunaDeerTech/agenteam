import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type TimelineMaterial = {
  mode: "recent-older-and-refresh";
  cookie: string;
  project_id: string;
  sprint_id: string;
  task_id: string;
  worker_id: string;
  route: string;
  title: string;
  comment: string;
  version: "49";
  event_ids: string[];
};
export function timelineMaterial(): TimelineMaterial {
  const value: TimelineMaterial = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-timeline-material.json"),
      "utf8",
    ),
  );
  expect(value.mode).toBe("recent-older-and-refresh");
  expect(value.version).toBe("49");
  expect(value.event_ids).toHaveLength(51);
  expect(new Set(value.event_ids).size).toBe(51);
  const id = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  for (const item of [
    value.project_id,
    value.task_id,
    value.sprint_id,
    value.worker_id,
    ...value.event_ids,
  ]) expect(item).toMatch(id);
  expect(value.route).toMatch(new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${value.task_id}$`));
  expect(value.title.length).toBeGreaterThan(0);
  expect(value.comment).toContain("\n");
  return value;
}

export async function observeTimeline(page: Page, data: TimelineMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const eventsPath = `/tasks/${data.task_id}/events`;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url());
    const pathname = url.pathname;
    if (!pathname.startsWith("/api/")) return undefined;
    if (request.method() !== "GET") throw Error("TASK_TIMELINE_IS_READ_ONLY");
    const global: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (global[pathname]) return global[pathname];
    if (pathname === prefix) return "project";
    if (!pathname.startsWith(prefix + "/")) throw Error("UNEXPECTED_TIMELINE_SCOPE");
    const path = pathname.slice(prefix.length);
    if (path === eventsPath) {
      expect(url.searchParams.getAll("limit")).toEqual(["50"]);
      expect(url.searchParams.getAll("order")).toEqual(["desc"]);
      expect(url.searchParams.getAll("cursor").length).toBeLessThanOrEqual(1);
      for (const key of url.searchParams.keys())
        expect(["limit", "order", "cursor"]).toContain(key);
      if (url.searchParams.has("cursor"))
        expect(url.searchParams.get("cursor")!.length).toBeGreaterThan(0);
      return "events";
    }
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
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(parts[2]!)
    ) {
      if (parts[1] === "milestones") return "milestone";
      if (parts[1] === "sprints" && parts[2] === data.sprint_id) return "sprint";
      if (parts[1] === "agents" && parts[2] === data.worker_id) return "agent";
      if (parts[1] === "tasks" && parts[2] === data.task_id) return "task";
    }
    if (path === `/tasks/${data.task_id}/blockers`) return "blockers";
    throw Error("UNOBSERVED_TASK_TIMELINE_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_TASK_TIMELINE_WEB_DIST!,
    evidence: process.env.AGENTEAM_TASK_TIMELINE_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_TASK_TIMELINE_WEB_INPUT_HASH!,
    prefix: "task-timeline",
    endpoint: classify,
    lookupCommand: null,
    timelineReads: true,
  });
  return {
    ...seen,
    requirePages(count: 1 | 2 | 3) {
      const requests = seen.requests("events");
      expect(requests).toHaveLength(count);
      const recent = [...data.event_ids.slice(1)].reverse();
      for (let index = 0; index < count; index++) {
        const body = seen.timelinePage(index);
        expect(Object.keys(body).sort()).toEqual(
          index === 1 ? ["items"] : ["items", "next_cursor"],
        );
        expect(body.items.map((item: any) => item.id)).toEqual(
          index === 1 ? [data.event_ids[0]] : recent,
        );
        for (const item of body.items) {
          expect(item.project_id).toBe(data.project_id);
          expect(item.task_id).toBe(data.task_id);
          expect(BigInt(item.task_version) <= BigInt(data.version)).toBe(true);
          expect(item.actor.type).toBe("human");
        }
        const query = new URL(requests[index]!.url()).searchParams;
        if (index === 1) {
          expect(query.get("cursor")).toBe(seen.timelinePage(0).next_cursor);
          expect(Object.hasOwn(body, "next_cursor")).toBe(false);
          expect(body.items[0].type).toBe("task_created");
          expect(body.items[0].task_version).toBe("1");
        } else {
          expect(query.has("cursor")).toBe(false);
          expect(typeof body.next_cursor).toBe("string");
          expect(body.next_cursor.length).toBeGreaterThan(0);
          const comments = body.items.filter((item: any) => item.type === "comment");
          expect(comments).toHaveLength(1);
          expect(comments[0].payload.body).toBe(data.comment);
          expect(comments[0].task_version).toBe(data.version);
          expect(body.items.filter((item: any) => item.type === "fields_updated")).toHaveLength(47);
          const state = body.items.filter((item: any) => item.type === "state_changed");
          const assignee = body.items.filter((item: any) => item.type === "assignee_changed");
          expect(state).toHaveLength(1);
          expect(state[0].payload).toEqual({
            from_state: "backlog", to_state: "todo", reason_code: null,
          });
          expect(assignee).toHaveLength(1);
          expect(assignee[0].payload).toEqual({
            from_agent_id: null, to_agent_id: data.worker_id,
          });
        }
      }
      expect(seen.requests("transfer")).toHaveLength(0);
      expect(seen.requests("lookup")).toHaveLength(0);
    },
  };
}

export async function timelineScreenshot(
  page: Page,
  data: TimelineMaterial,
  layout: "1440-light" | "390-dark",
) {
  const [width, theme] = layout.split("-");
  expect(page.viewportSize()?.width).toBe(Number(width));
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme!);
  expect(await page.evaluate(
    () => matchMedia("(prefers-reduced-motion: reduce)").matches,
  )).toBe(true);
  await expect.poll(() => page.evaluate(
    () => document.documentElement.scrollWidth <= innerWidth + 1,
  )).toBe(true);
  const directory = process.env.AGENTEAM_TASK_TIMELINE_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `task-timeline-${data.mode}-${layout}-history.png`),
    fullPage: true,
    animations: "disabled",
  });
}
