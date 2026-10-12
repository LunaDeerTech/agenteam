import {
  test,
  expect,
  type Page,
  type Request,
  type Locator,
} from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";
import { observeUsageWire } from "./project-usage.helpers";

const mode = "daily-server-aggregate";
const instant = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/;
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const fields = [
  "input",
  "output",
  "total",
  "cached_input",
  "cache_write",
  "reasoning",
] as const;
const counts = [
  "confirmed_invocations",
  "dispatch_unknown",
  "succeeded",
  "failed",
  "cancelled",
  "unknown",
] as const;
type Project = {
  id: string;
  route: string;
  rows: Record<string, any>[];
  expected: Record<string, any>[];
};
type Material = {
  mode: string;
  origin: string;
  cookie: string;
  input_hash: string;
  project_id: string;
  projects: Project[];
};
function material(): Material {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "project-usage-summary-material.json",
      ),
      "utf8",
    ),
  );
  expect(Object.keys(data).sort()).toEqual(
    ["mode", "origin", "cookie", "input_hash", "project_id", "projects"].sort(),
  );
  expect(data.mode).toBe(mode);
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_INPUT_HASH,
  );
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(data.cookie.length).toBeGreaterThan(0);
  expect(data.projects).toHaveLength(3);
  expect(new Set(data.projects.map((p: Project) => p.id)).size).toBe(3);
  expect(data.project_id).toBe(data.projects[0].id);
  for (const p of data.projects as Project[]) {
    expect(p.id).toMatch(uuid);
    expect(p.route).toMatch(
      /^\/[A-Za-z0-9-]+\/[A-Za-z0-9._-]+\/settings\/model-usage$/,
    );
    for (const row of p.rows) expect(row.project_id).toBe(p.id);
  }
  expect(data.projects.map((p: Project) => p.rows.length)).toEqual([52, 1, 0]);
  expect(data.projects.map((p: Project) => p.expected.length)).toEqual([
    2, 1, 0,
  ]);
  // Expectations are independent fixed seed facts, not sums of the first List page.
  expect(
    data.projects[0].expected.map((r: any) => r.summary.confirmed_invocations),
  ).toEqual(["26", "26"]);
  expect(data.projects[0].expected[0].summary.input).toEqual({
    sum: "175",
    known_count: "25",
    unknown_count: "1",
  });
  for (const name of fields)
    expect(data.projects[0].expected[1].summary[name]).toEqual({
      sum: null,
      known_count: "0",
      unknown_count: "26",
    });
  expect(data.projects[1].expected[0].summary.input).toEqual({
    sum: "99",
    known_count: "1",
    unknown_count: "0",
  });
  return data;
}
async function observe(page: Page, data: Material) {
  const evidence = process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE!;
  const listRequests: Request[] = [],
    summaryRequests: Request[] = [];
  const windows = new Map<string, { from: string; to: string }>();
  let verifiedLists = 0,
    verifiedSummaries = 0,
    completed = false,
    stage = "start";
  const classify = (request: Request): string | undefined => {
    const u = new URL(request.url()),
      path = u.pathname;
    if (!path.startsWith("/api/")) return undefined;
    expect(u.origin).toBe(data.origin);
    expect(request.method()).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined();
    expect(request.headers()["x-csrf-token"]).toBeUndefined();
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/projects": "projects",
      "/api/v1/projects/resolve": "resolve",
    };
    if (globals[path]) return globals[path];
    const prefix = `/api/v1/projects/${data.project_id}`;
    if (path === prefix && !u.search) return "project";
    const summary = path === prefix + "/model-usage/summary";
    if (!summary && path !== prefix + "/model-usage")
      throw Error("UNOBSERVED_USAGE_SUMMARY_ENDPOINT");
    expect([...u.searchParams.keys()].sort()).toEqual(
      summary ? ["from", "group_by", "limit", "to"] : ["from", "limit", "to"],
    );
    expect([...u.searchParams].length).toBe(summary ? 4 : 3);
    const window = {
      from: u.searchParams.get("from")!,
      to: u.searchParams.get("to")!,
    };
    expect(window.from).toMatch(instant);
    expect(window.to).toMatch(instant);
    expect(Date.parse(window.to) - Date.parse(window.from)).toBe(
      30 * 24 * 60 * 60 * 1000,
    );
    if (summary) {
      expect(u.searchParams.get("limit")).toBe("100");
      expect(u.searchParams.get("group_by")).toBe("day");
      expect(window).toEqual(windows.get(data.project_id));
      expect(summaryRequests.length).toBe(verifiedSummaries);
      expect(verifiedLists).toBe(verifiedSummaries + 1);
      summaryRequests.push(request);
      return "summary";
    }
    expect(u.searchParams.get("limit")).toBe("50");
    expect(windows.has(data.project_id)).toBe(false);
    expect(listRequests.length).toBe(verifiedLists);
    expect(verifiedLists).toBe(verifiedSummaries);
    windows.set(data.project_id, window);
    listRequests.push(request);
    return "usage";
  };
  const wire = await observeUsageWire(page, data.origin);
  let seen: Awaited<ReturnType<typeof observeTaskRequests>>;
  try {
    seen = await observeTaskRequests(page, data, {
      dist: process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_DIST!,
      evidence,
      inputHash: data.input_hash,
      prefix: "project-usage-summary",
      endpoint: classify,
      lookupCommand: null,
      usageReads: true,
      usageSummaryReads: true,
      usageWireHeaders: wire.headers,
    });
  } catch (error) {
    try {
      await wire.close();
    } finally {
      throw error;
    }
  }
  return {
    ...seen,
    stage(value: string) {
      stage = value;
    },
    list(project: Project) {
      expect(listRequests).toHaveLength(verifiedLists + 1);
      const body = seen.usageSummaryRead(listRequests[verifiedLists]!);
      expect(Object.keys(body).sort()).toEqual(["items", "next_cursor"]);
      expect(body.items).toEqual(project.rows.slice(0, 50));
      if (project.rows.length > 50) {
        expect(typeof body.next_cursor).toBe("string");
        expect(body.next_cursor.length).toBeGreaterThan(0);
      } else expect(body.next_cursor).toBeNull();
      verifiedLists++;
    },
    summary(project: Project) {
      expect(summaryRequests).toHaveLength(verifiedSummaries + 1);
      const body = seen.usageSummaryRead(summaryRequests[verifiedSummaries]!);
      expect(Object.keys(body).sort()).toEqual([
        "as_of",
        "items",
        "next_cursor",
      ]);
      expect(body.next_cursor).toBeNull();
      expect(body.as_of).toMatch(instant);
      expect(body.as_of >= windows.get(project.id)!.to).toBe(true);
      expect(body.items).toEqual(
        project.expected.map((item) => ({
          ...item,
          summary: { ...item.summary, as_of: body.as_of },
        })),
      );
      verifiedSummaries++;
      return body;
    },
    async finish() {
      expect(verifiedLists).toBe(3);
      expect(verifiedSummaries).toBe(3);
      expect(seen.requests("lookup")).toHaveLength(0);
      await seen.verify();
      await seen.endDocument();
      wire.complete();
      completed = true;
    },
    async retire() {
      try {
        await seen.retire();
        wire.complete();
      } finally {
        await wire.close();
      }
    },
    save(status: string | undefined, errors: readonly { stack?: string }[]) {
      seen.save(status!);
      writeFileSync(
        join(evidence, "project-usage-summary-wire.json"),
        JSON.stringify(wire.snapshot(), null, 2),
        { mode: 0o600 },
      );
      writeFileSync(
        join(evidence, "project-usage-summary-stage.json"),
        JSON.stringify({
          stage,
          status,
          verifiedLists,
          verifiedSummaries,
          error_locations: errors.map(
            (e) =>
              e.stack?.match(
                /(?:project-usage-summary\.spec|task-review\.helpers|project-usage\.helpers)\.ts:[0-9]+:[0-9]+/,
              )?.[0] ?? "unavailable",
          ),
        }),
        { mode: 0o600 },
      );
      if (completed && status === "passed") {
        const proof = JSON.parse(
          readFileSync(
            join(evidence, `project-usage-summary-${mode}.json`),
            "utf8",
          ),
        );
        writeFileSync(
          join(
            process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
            "project-usage-summary-result.json",
          ),
          JSON.stringify({
            completed,
            input_hash: data.input_hash,
            original_requests: proof.original_requests,
            all_observed: proof.all_observed,
            lists: verifiedLists,
            summaries: verifiedSummaries,
          }),
          { mode: 0o600 },
        );
      }
    },
  };
}
const observations = new WeakMap<Page, Awaited<ReturnType<typeof observe>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status, info.errors);
  }
});
async function press(page: Page, target: Locator) {
  await expect(target).toBeEnabled();
  await target.focus();
  await expect(target).toBeFocused();
  await page.keyboard.press("Enter");
}

test("uses full server aggregates across UTC days and isolates project scope [daily-server-aggregate]", async ({
  page,
}) => {
  const data = material();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "__Host-agenteam_session",
        value: data.cookie,
        url: data.origin,
        httpOnly: true,
        secure: true,
        sameSite: "Lax",
      },
    ]);
  const seen = await observe(page, data);
  observations.set(page, seen);
  for (const [index, project] of data.projects.entries()) {
    if (index) {
      await seen.endDocument();
      data.project_id = project.id;
    }
    seen.stage(`project-${index}-list`);
    await page.goto(project.route);
    const region = page.getByRole("region", {
      name: "项目模型调用记录",
      exact: true,
    });
    if (project.rows.length)
      await expect(
        region
          .getByRole("list", { name: "调用记录列表", exact: true })
          .getByRole("listitem"),
      ).toHaveCount(Math.min(50, project.rows.length));
    else
      await expect(
        region.getByText("本次时间范围内没有调用记录。", { exact: true }),
      ).toBeVisible();
    await seen.verify();
    seen.list(project);
    seen.stage(`project-${index}-summary`);
    await press(
      page,
      region.getByRole("button", { name: "按日汇总", exact: true }),
    );
    await expect(
      region.getByRole("heading", { name: "按日汇总", exact: true }),
    ).toBeFocused();
    const daily = region.getByRole("region", {
      name: "按日用量汇总",
      exact: true,
    });
    await expect(daily).toBeVisible();
    await expect(daily.getByRole("article")).toHaveCount(
      project.expected.length,
    );
    if (!project.expected.length)
      await expect(
        daily.getByText("本次时间范围内没有用量汇总。", { exact: true }),
      ).toBeVisible();
    await seen.verify();
    const body = seen.summary(project);
    await expect(daily.locator(":scope > p time")).toHaveText(body.as_of);
    for (const [i, row] of project.expected.entries()) {
      const article = daily.getByRole("article").nth(i);
      await expect(article).toHaveAttribute(
        "aria-label",
        row.key.day + " 用量汇总",
      );
      await expect(article.getByRole("heading")).toHaveText(
        row.key.day + "（UTC）",
      );
      await expect(article.locator(".usage-day-counts dd")).toHaveText(
        counts.map((name) => row.summary[name]),
      );
      const tableRows = article.locator("tbody tr");
      await expect(tableRows).toHaveCount(6);
      for (const [n, name] of fields.entries())
        await expect(tableRows.nth(n).locator("td")).toHaveText([
          row.summary[name].sum ?? "未知",
          row.summary[name].known_count,
          row.summary[name].unknown_count,
        ]);
    }
    if (index === 0) {
      await page.screenshot({
        path: join(
          process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE!,
          "summary-1440-light.png",
        ),
        animations: "disabled",
      });
      await page.setViewportSize({ width: 390, height: 844 });
      await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1,
          ),
        )
        .toBe(true);
      await daily.scrollIntoViewIfNeeded();
      await page.screenshot({
        path: join(
          process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE!,
          "summary-390-dark.png",
        ),
        animations: "disabled",
      });
    }
  }
  seen.stage("retire");
  await seen.finish();
});
