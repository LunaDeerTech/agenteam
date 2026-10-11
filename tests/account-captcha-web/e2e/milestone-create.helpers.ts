import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const id =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type MilestoneCreateMaterial = {
  mode: "first-milestone-and-sprint-entry";
  cookie: string;
  project_id: string;
  project_version: string;
  route: string;
  title: string;
  description: string;
};
export function milestoneCreateMaterial(): MilestoneCreateMaterial {
  const value = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "milestone-create-material.json",
      ),
      "utf8",
    ),
  );
  expect(value.mode).toBe("first-milestone-and-sprint-entry");
  expect(value.project_id).toMatch(id);
  expect(value.project_version).toMatch(/^[1-9][0-9]*$/);
  expect(value.route).toMatch(/^\/[^/?#]+\/[^/?#]+\/tasks$/);
  expect(value.title).toBe("First delivery milestone");
  expect(value.description).toBe(
    "Plan the first milestone.\nKeep Sprint creation explicit.",
  );
  return value;
}
export async function observeMilestoneCreate(
  page: Page,
  data: MilestoneCreateMaterial,
) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  let createdID: string | undefined;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    if (!path.startsWith("/api/")) return undefined;
    if (method === "POST" && path === prefix + "/milestones" && !url.search) {
      expect(
        createdID,
        "only the original Milestone creation may dispatch",
      ).toBeUndefined();
      const body = request.postDataJSON();
      expect(body?.request?.milestone_id).toMatch(id);
      expect(body).toEqual({
        request: {
          milestone_id: body.request.milestone_id,
          title: data.title,
          description: data.description,
        },
      });
      createdID = body.request.milestone_id;
      return "create";
    }
    if (method !== "GET") throw Error("UNOBSERVED_MILESTONE_CREATE_WRITE");
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
    if (path === prefix + "/milestones") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "50"]]);
      return "milestones";
    }
    if (
      createdID &&
      path === prefix + "/milestones/" + createdID &&
      !url.search
    )
      return "milestone";
    if (createdID && path === prefix + "/sprints") {
      expect([...url.searchParams.keys()].sort()).toEqual([
        "limit",
        "milestone_id",
      ]);
      expect(url.searchParams.get("limit")).toBe("50");
      expect(url.searchParams.get("milestone_id")).toBe(createdID);
      return "sprints";
    }
    throw Error("UNOBSERVED_MILESTONE_CREATE_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_MILESTONE_CREATE_WEB_DIST!,
    evidence: process.env.AGENTEAM_MILESTONE_CREATE_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_MILESTONE_CREATE_WEB_INPUT_HASH!,
    prefix: "milestone-create",
    endpoint: classify,
    lookupCommand: "work.milestone.create",
    milestoneCreateWrites: true,
  });
  return {
    ...seen,
    createdID() {
      expect(createdID).toMatch(id);
      return createdID!;
    },
    requireResult(originalProject: Record<string, any>) {
      expect(seen.requests("create")).toHaveLength(1);
      expect(seen.requests("lookup")).toHaveLength(0);
      expect(seen.requests("start")).toHaveLength(0);
      const result = seen.receipt("create", 0);
      expect(Object.keys(result).sort()).toEqual([
        "changed",
        "command",
        "event_id",
        "milestone",
        "sprint",
      ]);
      expect(result.command).toBe("work.milestone.create");
      expect(result.changed).toBe(true);
      expect(result.sprint).toBeNull();
      expect(result.event_id).toMatch(id);
      const milestone = result.milestone;
      expect(Object.keys(milestone).sort()).toEqual([
        "created_at",
        "description",
        "id",
        "manual_rank",
        "project_id",
        "title",
        "updated_at",
        "version",
      ]);
      expect(milestone).toEqual({
        id: createdID,
        project_id: data.project_id,
        title: data.title,
        description: data.description,
        manual_rank: milestone.manual_rank,
        version: "1",
        created_at: milestone.created_at,
        updated_at: milestone.created_at,
      });
      expect(milestone.manual_rank).toMatch(/^[0-9a-f]{32}$/);
      expect(typeof milestone.created_at).toBe("string");
      expect(seen.milestoneRead("milestone")).toEqual(milestone);
      const { description, ...summary } = milestone;
      expect(seen.milestoneRead("milestones")).toEqual({ items: [summary] });
      expect(seen.milestoneRead("sprints")).toEqual({ items: [] });
      expect(seen.milestoneRead("project")).toEqual(originalProject);
      return result;
    },
  };
}
export async function milestoneCreateScreenshot(
  page: Page,
  data: MilestoneCreateMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const evidence = process.env.AGENTEAM_MILESTONE_CREATE_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(evidence, `milestone-create-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
