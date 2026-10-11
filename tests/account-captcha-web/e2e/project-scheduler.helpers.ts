import { expect, type Page, type Request } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

export type SchedulerMaterial = {
  mode: "pause-limit-and-resume" | "lost-update-confirmation-only-lookup";
  cookie: string;
  project_id: string;
  route: string;
  version: "2";
  scheduler_enabled: true;
  scheduler_max_concurrency: null;
};
export const schedulerLimit = "9007199254740993";
export function schedulerMaterial(): SchedulerMaterial {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "project-scheduler-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe(process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_CASE);
  expect([
    "pause-limit-and-resume",
    "lost-update-confirmation-only-lookup",
  ]).toContain(data.mode);
  expect(data.project_id).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  );
  expect(data.route).toMatch(/^\/[a-z0-9-]+\/[a-z0-9-]+\/settings\/scheduler$/);
  expect(typeof data.cookie).toBe("string");
  expect(data.cookie.length).toBeGreaterThan(0);
  // The real default is paused. Go performs the declared seed command through
  // the original HTTP adapter; no fixture rewrites the default initializer.
  expect(data.version).toBe("2");
  expect(data.scheduler_enabled).toBe(true);
  expect(data.scheduler_max_concurrency).toBeNull();
  return data;
}
export async function observeScheduler(page: Page, data: SchedulerMaterial) {
  const prefix = `/api/v1/projects/${data.project_id}`;
  let updates = 0;
  const classify = (request: Request): string | undefined => {
    const url = new URL(request.url()),
      method = request.method(),
      path = url.pathname;
    if (!path.startsWith("/api/")) return;
    expect(url.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
    if (method === "PATCH" && path === prefix + "/scheduler" && !url.search) {
      expect(updates).toBeLessThan(
        data.mode === "pause-limit-and-resume" ? 2 : 1,
      );
      expect(request.postDataJSON()).toEqual(
        updates === 0
          ? {
              expected_version: "2",
              scheduler_enabled: false,
              scheduler_max_concurrency: schedulerLimit,
            }
          : {
              expected_version: "3",
              scheduler_enabled: true,
              scheduler_max_concurrency: null,
            },
      );
      updates++;
      return "update";
    }
    if (
      method === "POST" &&
      path === prefix + "/commands/lookup" &&
      !url.search
    ) {
      expect(data.mode).toBe("lost-update-confirmation-only-lookup");
      expect(request.postDataJSON()).toEqual({ command: "update" });
      return "lookup";
    }
    if (method !== "GET") throw Error("UNOBSERVED_SCHEDULER_WRITE");
    expect(request.postDataBuffer()).toBeNull();
    if (path === "/api/v1/projects/resolve") {
      const [, username, project] = data.route.split("/");
      expect([...url.searchParams.entries()]).toEqual([
        ["username", username],
        ["project_name", project],
      ]);
      return "resolve";
    }
    if (path === "/api/v1/projects") {
      expect([...url.searchParams.entries()]).toEqual([["limit", "25"]]);
      return "projects";
    }
    expect(url.search).toBe("");
    if (path === "/api/v1/session") return "session";
    if (path === prefix) return "project";
    if (path === prefix + "/scheduler") return "scheduler";
    throw Error("UNOBSERVED_SCHEDULER_ENDPOINT");
  };
  const seen = await observeTaskRequests(page, data, {
    dist: process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_DIST!,
    evidence: process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE!,
    inputHash: process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_INPUT_HASH!,
    prefix: "project-scheduler",
    endpoint: classify,
    lookupCommand: "update",
    schedulerWrites: true,
    cut:
      data.mode === "lost-update-confirmation-only-lookup"
        ? "update"
        : undefined,
  });
  return {
    ...seen,
    requireCurrent(version: "2" | "3" | "4") {
      expect(seen.schedulerRead()).toEqual({
        project_id: data.project_id,
        version,
        scheduler_enabled: version !== "3",
        scheduler_max_concurrency: version === "3" ? schedulerLimit : null,
      });
      const writes = seen.requests("update");
      if (data.mode === "pause-limit-and-resume") {
        expect(writes).toHaveLength(Number(version) - 2);
        expect(seen.requests("lookup")).toHaveLength(0);
        for (let i = 0; i < writes.length; i++)
          expect(seen.receipt("update", i)).toEqual({
            project_id: data.project_id,
            version: String(i + 3),
          });
        expect(
          new Set(writes.map((r) => r.headers()["idempotency-key"])).size,
        ).toBe(writes.length);
      } else if (version === "3") {
        seen.originalLookup();
        expect(seen.receipt("lookup", 0)).toEqual({
          state: "committed",
          receipt: { project_id: data.project_id, version: "3" },
        });
      }
    },
  };
}
export async function schedulerScreenshot(
  page: Page,
  data: SchedulerMaterial,
  name: string,
) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect
    .poll(() =>
      brand.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  const directory = process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(directory, `project-scheduler-${data.mode}-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
