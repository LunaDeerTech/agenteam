import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  observeTaskRequests,
  type TaskObservationProfile,
} from "./task-review.helpers";

const id =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type SprintEditMaterial = {
  mode: "sprint-edit-normal" | "sprint-edit-lost";
  cookie: string;
  project_id: string;
  project_version: string;
  sprint_id: string;
  milestone_id: string;
  version: string;
  route: string;
  title: string;
  description: string;
  updated_title: string;
  updated_description: string;
  sprint: Record<string, any>;
  milestone: Record<string, any>;
};
export function sprintEditMaterial(): SprintEditMaterial {
  const value = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "sprint-edit-material.json"),
      "utf8",
    ),
  );
  expect(value.mode).toBe(process.env.AGENTEAM_SPRINT_EDIT_WEB_CASE);
  expect(["sprint-edit-normal", "sprint-edit-lost"]).toContain(value.mode);
  expect(value.project_id).toMatch(id);
  expect(value.sprint_id).toMatch(id);
  expect(value.milestone_id).toMatch(id);
  expect(value.project_version).toMatch(/^[1-9][0-9]*$/);
  expect(value.version).toBe(value.mode === "sprint-edit-normal" ? "1" : "2");
  expect(value.route).toMatch(
    new RegExp(`^/[^/?#]+/[^/?#]+/tasks/sprints/${value.sprint_id}$`),
  );
  expect(value.title).toBe("First delivery sprint");
  expect(value.description).toBe(
    "Plan the first Sprint.\nKeep lifecycle explicit.",
  );
  expect(value.updated_title).toBe("  e\u0301 Delivery sprint  ");
  expect(value.updated_description).toBe(
    "Updated Sprint.\nKeep lifecycle explicit.",
  );
  expect(value.sprint).toEqual({
    id: value.sprint_id,
    project_id: value.project_id,
    milestone_id: value.milestone_id,
    title: value.title,
    description: value.description,
    manual_rank: value.sprint.manual_rank,
    version: value.version,
    created_at: value.sprint.created_at,
    updated_at: value.sprint.updated_at,
    state: value.mode === "sprint-edit-normal" ? "planned" : "current",
    started_at:
      value.mode === "sprint-edit-normal" ? null : value.sprint.started_at,
    started_by:
      value.mode === "sprint-edit-normal" ? null : value.sprint.started_by,
    completed_at: null,
    completed_by: null,
  });
  expect(value.sprint.manual_rank).toMatch(/^[0-9a-f]{32}$/);
  if (value.mode === "sprint-edit-lost") {
    expect(value.sprint.started_at).toBe(value.sprint.updated_at);
    expect(value.sprint.started_by).toEqual({
      kind: "human",
      user_id: value.sprint.started_by.user_id,
    });
    expect(value.sprint.started_by.user_id).toMatch(id);
  } else expect(value.sprint.updated_at).toBe(value.sprint.created_at);
  expect(value.milestone).toMatchObject({
    id: value.milestone_id,
    project_id: value.project_id,
  });
  return value;
}
export function sprintEditProfile(
  data: Pick<SprintEditMaterial, "mode">,
): Omit<TaskObservationProfile, "endpoint"> {
  return {
    dist: process.env.AGENTEAM_SPRINT_EDIT_WEB_DIST!,
    evidence: process.env.AGENTEAM_SPRINT_EDIT_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_SPRINT_EDIT_WEB_INPUT_HASH!,
    prefix: "sprint-edit",
    lookupCommand: "work.sprint.update",
    sprintUpdateWrites: true,
    cut: data.mode === "sprint-edit-lost" ? "update" : undefined,
  };
}
export async function observeSprintEdit(page: Page, data: SprintEditMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`,
    target = `${prefix}/sprints/${data.sprint_id}`;
  let updates = 0,
    lookups = 0;
  const first = {
    expected_version: data.version,
    request: {
      title: data.updated_title,
      description: data.updated_description,
    },
  };
  const patches = [
    first,
    { expected_version: "2", request: { title: data.updated_title } },
    { expected_version: "2", request: { description: "" } },
  ];
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    if (method === "PATCH" && path === target && !url.search) {
      expect(updates).toBeLessThan(data.mode === "sprint-edit-normal" ? 3 : 1);
      expect(request.postDataJSON()).toEqual(patches[updates++]);
      return "update";
    }
    if (
      method === "POST" &&
      path === prefix + "/structure-commands/lookup" &&
      !url.search
    ) {
      expect(data.mode).toBe("sprint-edit-lost");
      expect(updates).toBe(1);
      expect(lookups++).toBe(0);
      expect(request.postDataJSON()).toEqual({
        command: "work.sprint.update",
        target_id: data.sprint_id,
        ...first,
      });
      return "lookup";
    }
    if (method !== "GET") throw Error("UNOBSERVED_SPRINT_EDIT_WRITE");
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/me": "profile",
      "/api/v1/me/preferences": "preferences",
      "/api/v1/auth/bootstrap": "bootstrap",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
    };
    if (globals[path]) return globals[path];
    if (path === prefix && !url.search) return "project";
    if (path === target && !url.search) return "sprint";
    if (path === `${prefix}/milestones/${data.milestone_id}` && !url.search)
      return "milestone";
    for (const name of ["milestones", "agents"]) {
      if (path === `${prefix}/${name}`) {
        expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]);
        return name;
      }
    }
    if (path === prefix + "/sprints") {
      expect([...url.searchParams.keys()].sort()).toEqual([
        "limit",
        "milestone_id",
      ]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("milestone_id")).toBe(data.milestone_id);
      return "sprints";
    }
    if (path === prefix + "/tasks") {
      expect([...url.searchParams.keys()].sort()).toEqual([
        "limit",
        "sprint_id",
        "state",
      ]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("sprint_id")).toBe(data.sprint_id);
      expect([
        "backlog",
        "todo",
        "in_progress",
        "in_review",
        "blocked",
        "done",
        "cancelled",
      ]).toContain(url.searchParams.get("state"));
      return "tasks";
    }
    throw Error("UNOBSERVED_SPRINT_EDIT_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    ...sprintEditProfile(data),
    endpoint: classify,
  });
  return {
    ...seen,
    current(expected: Record<string, any>) {
      // The existing current-read DTO omits ActorHistory; the exact full
      // lifecycle/actor snapshot is checked in the update receipt.
      const {
        started_by: _startedActor,
        completed_by: _completedActor,
        ...current
      } = expected;
      expect(seen.sprintEditRead("sprint")).toEqual(current);
      const {
        description: _description,
        started_at: _sa,
        started_by: _sb,
        completed_at: _ca,
        completed_by: _cb,
        ...summary
      } = expected;
      expect(seen.sprintEditRead("sprints")).toEqual({ items: [summary] });
      expect(seen.sprintEditRead("milestone")).toEqual(data.milestone);
      const { description: _md, ...milestoneSummary } = data.milestone;
      expect(seen.sprintEditRead("milestones")).toEqual({
        items: [milestoneSummary],
      });
      expect(seen.sprintEditRead("agents")).toEqual({ items: [] });
      expect(seen.sprintEditRead("tasks")).toEqual({ items: [] });
      expect(seen.sprintEditRead("project")).toMatchObject({
        id: data.project_id,
        version: data.project_version,
        current_sprint_id:
          data.mode === "sprint-edit-lost" ? data.sprint_id : null,
        lifecycle: "active",
      });
    },
    result(index: number, before: Record<string, any>, lookup = false) {
      const wire = seen.receipt(
        lookup ? "lookup" : "update",
        lookup ? 0 : index,
      );
      if (lookup) {
        expect(Object.keys(wire).sort()).toEqual(["result", "state"]);
        expect(wire.state).toBe("committed");
      }
      const value = lookup ? wire.result : wire;
      expect(Object.keys(value).sort()).toEqual([
        "changed",
        "command",
        "event_id",
        "milestone",
        "sprint",
      ]);
      expect(value.command).toBe("work.sprint.update");
      expect(value.milestone).toBeNull();
      expect(value.changed).toBe(index !== 1);
      if (index === 1) expect(value.event_id).toBeNull();
      else expect(value.event_id).toMatch(id);
      const fields = patches[index]!.request;
      const expected = {
        ...before,
        ...fields,
        version: String(BigInt(before.version) + (index === 1 ? 0n : 1n)),
        updated_at: index === 1 ? before.updated_at : value.sprint.updated_at,
      };
      expect(value.sprint).toEqual(expected);
      expect(typeof value.sprint.updated_at).toBe("string");
      expect(value.sprint.updated_at >= before.updated_at).toBe(true);
      return value.sprint as Record<string, any>;
    },
  };
}
export async function sprintEditScreenshot(
  page: Page,
  data: SprintEditMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const evidence = process.env.AGENTEAM_SPRINT_EDIT_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(evidence, `sprint-edit-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
