import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  observeTaskRequests,
  type TaskObservationProfile,
} from "./task-review.helpers";

const id =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type HumanBlockerMaterial = {
  mode: "human-blocker-wide" | "human-blocker-narrow";
  cookie: string;
  project_id: string;
  sprint_id: string;
  task_id: string;
  worker_id: string;
  other_project_id: string;
  foreign_id: string;
  route: string;
  input_hash: string;
  description: string;
  resolution: string;
  initial: Record<string, any>;
};
export function humanBlockerMaterial(): HumanBlockerMaterial {
  const value: HumanBlockerMaterial = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "task-human-blocker-material.json",
      ),
      "utf8",
    ),
  );
  expect(["human-blocker-wide", "human-blocker-narrow"]).toContain(value.mode);
  for (const valueID of [
    value.project_id,
    value.sprint_id,
    value.task_id,
    value.worker_id,
    value.other_project_id,
    value.foreign_id,
    value.initial.milestone_id,
  ])
    expect(valueID).toMatch(id);
  expect(value.other_project_id).not.toBe(value.project_id);
  expect(value.foreign_id).not.toBe(value.task_id);
  expect(value.initial).toMatchObject({
    id: value.task_id,
    project_id: value.project_id,
    sprint_id: value.sprint_id,
    state: "backlog",
    assignee_agent_id: null,
    version: "1",
  });
  expect(value.route).toMatch(
    new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${value.task_id}$`),
  );
  expect(value.input_hash).toBe(
    process.env.AGENTEAM_TASK_HUMAN_BLOCKER_WEB_INPUT_HASH,
  );
  expect(value.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(value.description).toBe("等待人工确认。\n保留 原始说明。");
  expect(value.resolution).toBe("已由原 Owner 核对。\n允许继续规划。");
  return value;
}

function query(url: URL, values: Record<string, string>) {
  expect([...url.searchParams.entries()].sort()).toEqual(
    Object.entries(values).sort(),
  );
}

export async function observeHumanBlocker(
  page: Page,
  data: HumanBlockerMaterial,
) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  const target = `/tasks/${data.task_id}`;
  const keys = new Set<string>();
  let blockerID = "";
  let writes = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) return undefined;
    const method = request.method();
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (method === "GET" && globals[url.pathname]) return globals[url.pathname];
    if (method === "GET" && url.pathname === prefix) {
      query(url, {});
      return "project";
    }
    if (!url.pathname.startsWith(prefix + "/"))
      throw Error("HUMAN_BLOCKER_FOREIGN_HTTP_SCOPE");
    const path = url.pathname.slice(prefix.length);
    if (
      method === "POST" &&
      [target + "/blockers", target + "/blockers/resolve"].includes(path)
    ) {
      query(url, {});
      const key = request.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      const body = request.postDataJSON();
      if (path === target + "/blockers") {
        expect(writes).toBe(0);
        blockerID = body.request.blocker_id;
        expect(blockerID).toMatch(id);
        expect(body).toEqual({
          expected_version: "1",
          request: {
            blocker_id: blockerID,
            type: "waiting_for_human",
            description: data.description,
            metadata: {},
          },
        });
      } else {
        expect(writes).toBe(1);
        expect(body).toEqual({
          expected_version: "2",
          request: {
            blocker_id: blockerID,
            resolution_comment:
              data.mode === "human-blocker-wide" ? data.resolution : null,
          },
        });
      }
      keys.add(key);
      writes++;
      return writes === 1 ? "add" : "resolve-blocker";
    }
    if (method !== "GET")
      throw Error("HUMAN_BLOCKER_UNDECLARED_WRITE_LOOKUP_OR_TRANSFER");
    if (["/milestones", "/agents"].includes(path)) {
      query(url, { limit: "50" });
      return path.slice(1);
    }
    if (path === "/sprints") {
      query(url, { limit: "50", milestone_id: data.initial.milestone_id });
      return "sprints";
    }
    if (path === "/tasks") {
      const state = url.searchParams.get("state")!;
      expect([
        "backlog",
        "todo",
        "in_progress",
        "in_review",
        "blocked",
        "done",
        "cancelled",
      ]).toContain(state);
      query(url, { limit: "50", sprint_id: data.sprint_id, state });
      return "tasks";
    }
    const singles: Record<string, string> = {
      [`/milestones/${data.initial.milestone_id}`]: "milestone",
      [`/sprints/${data.sprint_id}`]: "sprint",
      [`/agents/${data.worker_id}`]: "agent",
      [target]: "task",
    };
    if (singles[path]) {
      query(url, {});
      return singles[path];
    }
    if (path === target + "/blockers") {
      query(url, { limit: "50", status: "all" });
      return "blockers";
    }
    if (path === target + "/events") {
      query(url, { limit: "50", order: "desc" });
      return "events";
    }
    throw Error("HUMAN_BLOCKER_UNOBSERVED_HTTP_ENDPOINT");
  };
  const profile: TaskObservationProfile = {
    dist: process.env.AGENTEAM_TASK_HUMAN_BLOCKER_WEB_DIST!,
    evidence: process.env.AGENTEAM_TASK_HUMAN_BLOCKER_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_TASK_HUMAN_BLOCKER_WEB_INPUT_HASH!,
    prefix: "task-human-blocker",
    endpoint: classify,
    lookupCommand: null,
    humanBlockerWrites: true,
  };
  // Opt-in must not enable other writes, cuts or query lanes. These fail before
  // any browser instrumentation is installed and do not issue HTTP requests.
  for (const invalid of [
    { ...profile, humanBlockerWrites: undefined },
    { ...profile, prefix: "task-edit" },
    { ...profile, cut: "update" },
    { ...profile, lookupCommand: "work.task.transfer" },
    { ...profile, timelineReads: true },
  ])
    await expect(
      observeTaskRequests(page, data, invalid as TaskObservationProfile),
    ).rejects.toThrow("TASK_HUMAN_BLOCKER_PROFILE_MISMATCH");
  const seen = await observeTaskRequests(page, data, profile);
  let added: Record<string, any> | undefined;
  let resolved: Record<string, any> | undefined;
  function checkReceipt(stage: "added" | "resolved") {
    const result = seen.receipt(
      stage === "added" ? "add" : "resolve-blocker",
      0,
    );
    const version = stage === "added" ? "2" : "3";
    expect(result.task).toEqual({
      ...data.initial,
      version,
      updated_at: result.task.updated_at,
    });
    expect(result.task.updated_at >= data.initial.updated_at).toBe(true);
    expect(result.task_event_id).toMatch(id);
    expect(result.event_ids).toHaveLength(1);
    expect(result.event_ids[0]).toMatch(id);
    if (stage === "added") {
      expect(result.blocker).toEqual({
        id: blockerID,
        project_id: data.project_id,
        task_id: data.task_id,
        type: "waiting_for_human",
        description: data.description,
        related_task_id: null,
        created_at: result.task.updated_at,
        resolved_at: null,
        resolution_comment: null,
      });
      added = result;
    } else {
      expect(added).toBeTruthy();
      expect(result.blocker).toEqual({
        ...added!.blocker,
        resolved_at: result.task.updated_at,
        resolution_comment:
          data.mode === "human-blocker-wide" ? data.resolution : null,
      });
      expect(result.task.updated_at >= added!.task.updated_at).toBe(true);
      expect(result.task_event_id).not.toBe(added!.task_event_id);
      expect(result.event_ids[0]).not.toBe(added!.event_ids[0]);
      resolved = result;
    }
    return result;
  }
  return {
    ...seen,
    checkReceipt,
    checkCurrent(stage: "initial" | "added" | "resolved") {
      const result =
        stage === "initial" ? undefined : stage === "added" ? added : resolved;
      if (stage !== "initial") expect(result).toBeTruthy();
      expect(seen.humanBlockerRead("task")).toEqual(
        result?.task ?? data.initial,
      );
      expect(seen.humanBlockerRead("blockers")).toEqual({
        items: result ? [result.blocker] : [],
      });
      const events = seen.humanBlockerRead("events");
      expect(events.next_cursor).toBeUndefined();
      expect(events.items.map((event: any) => event.type)).toEqual(
        stage === "initial"
          ? ["task_created"]
          : stage === "added"
            ? ["blocker_added", "task_created"]
            : ["blocker_resolved", "blocker_added", "task_created"],
      );
      for (const event of events.items)
        expect(event).toMatchObject({
          project_id: data.project_id,
          task_id: data.task_id,
        });
      if (added) {
        const event = events.items.find(
          (event: any) => event.type === "blocker_added",
        );
        expect(event).toMatchObject({
          id: added.task_event_id,
          task_version: "2",
          created_at: added.task.updated_at,
          payload: { blocker_id: blockerID, blocker_type: "waiting_for_human" },
        });
      }
      if (resolved)
        expect(events.items[0]).toMatchObject({
          id: resolved.task_event_id,
          task_version: "3",
          created_at: resolved.task.updated_at,
          payload: {
            blocker_id: blockerID,
            blocker_type: "waiting_for_human",
            resolution_comment: resolved.blocker.resolution_comment,
          },
        });
    },
    checkWrites(count: 0 | 1 | 2) {
      expect(writes).toBe(count);
      expect(seen.requests("add")).toHaveLength(count ? 1 : 0);
      expect(seen.requests("resolve-blocker")).toHaveLength(
        count === 2 ? 1 : 0,
      );
      expect(seen.requests("lookup")).toHaveLength(0);
      expect(seen.requests("ready")).toHaveLength(0);
      expect(seen.requests("transfer")).toHaveLength(0);
    },
  };
}

export async function humanBlockerScreenshot(
  page: Page,
  data: HumanBlockerMaterial,
  stage: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  const directory = process.env.AGENTEAM_TASK_HUMAN_BLOCKER_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `task-human-blocker-${data.mode}-${stage}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
