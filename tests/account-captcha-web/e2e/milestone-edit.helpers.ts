import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  observeTaskRequests,
  type TaskObservationProfile,
} from "./task-review.helpers";

const id =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type MilestoneEditMaterial = {
  mode: "milestone-edit-normal" | "milestone-edit-lost";
  cookie: string;
  project_id: string;
  project_version: string;
  milestone_id: string;
  version: string;
  route: string;
  title: string;
  description: string;
  updated_title: string;
  updated_description: string;
  milestone: Record<string, any>;
};
export function milestoneEditMaterial(): MilestoneEditMaterial {
  const value = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "milestone-edit-material.json",
      ),
      "utf8",
    ),
  );
  expect(value.mode).toBe(process.env.AGENTEAM_MILESTONE_EDIT_WEB_CASE);
  expect(["milestone-edit-normal", "milestone-edit-lost"]).toContain(
    value.mode,
  );
  expect(value.project_id).toMatch(id);
  expect(value.milestone_id).toMatch(id);
  expect(value.project_version).toMatch(/^[1-9][0-9]*$/);
  expect(value.version).toBe("1");
  expect(value.route).toMatch(/^\/[^/?#]+\/[^/?#]+\/tasks$/);
  expect(value.title).toBe("First delivery milestone");
  expect(value.description).toBe(
    "Plan the first milestone.\nKeep Sprint creation explicit.",
  );
  expect(value.updated_title).toBe("  e\u0301 Delivery milestone  ");
  expect(value.updated_description).toBe(
    "Updated milestone.\nKeep Sprint planning explicit.",
  );
  expect(value.milestone).toEqual({
    id: value.milestone_id,
    project_id: value.project_id,
    title: value.title,
    description: value.description,
    manual_rank: value.milestone.manual_rank,
    version: "1",
    created_at: value.milestone.created_at,
    updated_at: value.milestone.created_at,
  });
  expect(value.milestone.manual_rank).toMatch(/^[0-9a-f]{32}$/);
  return value;
}
export function milestoneEditProfile(
  data: Pick<MilestoneEditMaterial, "mode">,
): Omit<TaskObservationProfile, "endpoint"> {
  return {
    dist: process.env.AGENTEAM_MILESTONE_EDIT_WEB_DIST!,
    evidence: process.env.AGENTEAM_MILESTONE_EDIT_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_MILESTONE_EDIT_WEB_INPUT_HASH!,
    prefix: "milestone-edit",
    lookupCommand: "work.milestone.update",
    milestoneUpdateWrites: true,
    cut: data.mode === "milestone-edit-lost" ? "update" : undefined,
  };
}
export async function observeMilestoneEdit(
  page: Page,
  data: MilestoneEditMaterial,
) {
  const prefix = `/api/v1/projects/${data.project_id}`,
    target = `${prefix}/milestones/${data.milestone_id}`;
  let updates = 0,
    lookups = 0;
  const first = {
    expected_version: "1",
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
      expect(updates).toBeLessThan(
        data.mode === "milestone-edit-normal" ? 3 : 1,
      );
      expect(request.postDataJSON()).toEqual(patches[updates++]);
      return "update";
    }
    if (
      method === "POST" &&
      path === prefix + "/structure-commands/lookup" &&
      !url.search
    ) {
      expect(data.mode).toBe("milestone-edit-lost");
      expect(updates).toBe(1);
      expect(lookups++).toBe(0);
      expect(request.postDataJSON()).toEqual({
        command: "work.milestone.update",
        target_id: data.milestone_id,
        ...first,
      });
      return "lookup";
    }
    if (method !== "GET") throw Error("UNOBSERVED_MILESTONE_EDIT_WRITE");
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
    if (path === target && !url.search) return "milestone";
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
    throw Error("UNOBSERVED_MILESTONE_EDIT_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    ...milestoneEditProfile(data),
    endpoint: classify,
  });
  return {
    ...seen,
    current(expected: Record<string, any>) {
      expect(seen.milestoneRead("milestone")).toEqual(expected);
      const { description: _description, ...summary } = expected;
      expect(seen.milestoneRead("milestones")).toEqual({ items: [summary] });
      expect(seen.milestoneRead("sprints")).toEqual({ items: [] });
      expect(seen.milestoneRead("agents")).toEqual({ items: [] });
      expect(seen.milestoneRead("project")).toMatchObject({
        id: data.project_id,
        version: data.project_version,
        current_sprint_id: null,
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
      expect(value.command).toBe("work.milestone.update");
      expect(value.sprint).toBeNull();
      expect(value.changed).toBe(index !== 1);
      if (index === 1) expect(value.event_id).toBeNull();
      else expect(value.event_id).toMatch(id);
      const fields = patches[index]!.request;
      const expected = {
        ...before,
        ...fields,
        version: String(BigInt(before.version) + (index === 1 ? 0n : 1n)),
        updated_at:
          index === 1 ? before.updated_at : value.milestone.updated_at,
      };
      expect(value.milestone).toEqual(expected);
      expect(typeof value.milestone.updated_at).toBe("string");
      expect(value.milestone.updated_at >= before.updated_at).toBe(true);
      return value.milestone as Record<string, any>;
    },
  };
}
export async function milestoneEditScreenshot(
  page: Page,
  data: MilestoneEditMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const evidence = process.env.AGENTEAM_MILESTONE_EDIT_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(evidence, `milestone-edit-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
