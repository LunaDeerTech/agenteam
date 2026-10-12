import { test, expect, type Request, type Response } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { observeTaskRequests } from "./task-review.helpers";

const states = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
] as const;
type Material = {
  mode: string;
  cookie: string;
  project_id: string;
  sprint_id: string;
  route: string;
  foreign_id: string;
  first_ids: string[];
  terms: string[];
  target: {
    id: string;
    title: string;
    description: string;
    plan: string;
    project_id: string;
    sprint_id: string;
  };
};

// This closed classifier admits only the original read-only navigation and
// current Project/Sprint Task reads. No retry, lookup or business write is used.
function classify(request: Request, data: Material): string | undefined {
  const { pathname } = new URL(request.url());
  if (!pathname.startsWith("/api/")) return undefined;
  if (request.method() !== "GET") throw Error("TASK_SEARCH_WRITE_FORBIDDEN");
  const global: Record<string, string> = {
    "/api/v1/session": "session",
    "/api/v1/me": "profile",
    "/api/v1/me/preferences": "preferences",
    "/api/v1/auth/bootstrap": "bootstrap",
    "/api/v1/projects/resolve": "resolve",
    "/api/v1/projects": "projects",
  };
  if (global[pathname]) return global[pathname];
  const prefix = `/api/v1/projects/${data.project_id}`;
  if (pathname === prefix) return "project";
  if (!pathname.startsWith(prefix + "/"))
    throw Error("TASK_SEARCH_PROJECT_SCOPE");
  const path = pathname.slice(prefix.length);
  const reads: Record<string, string> = {
    "/milestones": "milestones",
    "/sprints": "sprints",
    "/tasks": "tasks",
    "/agents": "agents",
    [`/sprints/${data.sprint_id}`]: "sprint",
    [`/tasks/${data.target.id}`]: "task",
    [`/tasks/${data.target.id}/blockers`]: "blockers",
    [`/tasks/${data.target.id}/events`]: "events",
  };
  if (reads[path]) return reads[path];
  if (
    /^\/milestones\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      path,
    )
  )
    return "milestone";
  throw Error("UNOBSERVED_TASK_SEARCH_ENDPOINT");
}

test("server search reaches Tasks beyond the first page [server-search-beyond-first-page]", async ({
  page,
}, testInfo) => {
  const data: Material = JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-search-material.json"),
      "utf8",
    ),
  );
  expect(data.mode).toBe("server-search-beyond-first-page");
  const evidence = process.env.AGENTEAM_TASK_SEARCH_WEB_EVIDENCE!;
  const observations: { text: string; state: string; ids: string[] }[] = [];
  const faults: string[] = [];
  const onError = () => faults.push("page-error");
  page.on("pageerror", onError);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      sameSite: "Lax",
      secure: false,
    },
  ]);
  const seen = await observeTaskRequests(
    page,
    { ...data, task_id: data.target.id },
    {
      dist: process.env.AGENTEAM_TASK_SEARCH_WEB_DIST!,
      evidence,
      inputHash: process.env.AGENTEAM_TASK_SEARCH_WEB_INPUT_HASH!,
      prefix: "task-search",
      endpoint: (request) => classify(request, data),
      lookupCommand: null,
    },
  );
  const form = page.getByRole("search", {
    name: "Sprint 任务搜索",
    exact: true,
  });
  const input = form.getByRole("searchbox", { name: "搜索任务", exact: true });
  const submit = form.getByRole("button", { name: "搜索", exact: true });
  const clear = form.getByRole("button", { name: "清除搜索", exact: true });
  const target = page.getByRole("button", {
    name: data.target.title,
    exact: true,
  });
  const path = `/api/v1/projects/${data.project_id}/tasks`;

  async function columns(text: string, action: () => Promise<unknown>) {
    // Register all seven original response waits before the real UI action.
    const waits = states.map((state) =>
      page.waitForResponse((r) => {
        const u = new URL(r.url());
        return (
          r.request().method() === "GET" &&
          u.pathname === path &&
          u.searchParams.get("state") === state &&
          (u.searchParams.get("text") ?? "") === text
        );
      }),
    );
    const responses = await Promise.all([...waits, action()]);
    // Use the original request terminal plus native reader/public consumer and
    // Go EOF/Close proof. A failed terminal is never treated as requestfinished.
    await seen.verify();
    for (let n = 0; n < states.length; n++) {
      const response = responses[n] as Response;
      const query = new URL(response.url()).searchParams;
      expect(query.get("sprint_id")).toBe(data.sprint_id);
      expect(query.get("limit")).toBe("50");
      expect(query.has("cursor")).toBe(false);
      expect(query.has("text")).toBe(text !== "");
      const value = seen.taskSearchRead(response.request());
      const ids = value.items.map(
        (row: {
          id: string;
          project_id: string;
          sprint_id: string;
          state: string;
        }) => {
          expect(row.project_id).toBe(data.project_id);
          expect(row.sprint_id).toBe(data.sprint_id);
          expect(row.state).toBe(states[n]);
          expect(row.id).not.toBe(data.foreign_id);
          return row.id;
        },
      );
      const expected =
        states[n] !== "backlog" || text === data.terms[0]!.toLowerCase()
          ? []
          : text === ""
            ? data.first_ids
            : [data.target.id];
      expect(ids).toEqual(expected);
      expect(Boolean(value.next_cursor)).toBe(
        states[n] === "backlog" && text === "",
      );
      observations.push({ text, state: states[n]!, ids });
    }
    await expect(page.getByText("正在读取任务", { exact: true })).toHaveCount(
      0,
    );
  }
  async function search(text: string, keyboard = false) {
    await input.fill(text);
    await columns(text, () =>
      keyboard ? input.press("Enter") : submit.click(),
    );
    await expect(input).toHaveValue(text);
  }
  async function screenshot(name: string) {
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth + 1,
        ),
      )
      .toBe(true);
    mkdirSync(evidence, { recursive: true, mode: 0o700 });
    await page.screenshot({
      path: join(evidence, `task-search-${name}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
  try {
    await columns("", () => page.goto(data.route));
    await expect(target).toHaveCount(0);
    await expect(
      page
        .getByRole("region", { name: "待规划", exact: true })
        .getByRole("button", { name: "更多待规划任务", exact: true }),
    ).toBeVisible();

    await search(data.terms[0]!, true);
    await expect(target).toBeVisible();
    await screenshot("title-desktop-light");

    await search(data.terms[1]!);
    await expect(target).toBeVisible();
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await search(data.terms[2]!, true);
    await expect(target).toBeVisible();
    await screenshot("plan-narrow-dark");

    await search(data.terms[0]!.toLowerCase());
    await expect(target).toHaveCount(0);
    await expect(
      page.getByText("没有匹配的任务，可清除搜索。", { exact: false }),
    ).toBeVisible();
    await columns("", () => clear.click());
    await expect(input).toHaveValue("");
    await expect(target).toHaveCount(0);

    await search(data.terms[0]!, true);
    const detail = page.waitForResponse(
      (r) =>
        r.request().method() === "GET" &&
        new URL(r.url()).pathname === `${path}/${data.target.id}`,
    );
    const [response] = await Promise.all([
      detail,
      columns(data.terms[0]!, () => target.click()),
    ]);
    expect(seen.taskSearchRead(response.request())).toEqual(data.target);
    const drawer = page.getByRole("dialog", { name: "任务详情", exact: true });
    await expect(
      drawer.getByRole("heading", { name: data.target.title, exact: true }),
    ).toBeVisible();
    await expect(
      drawer.getByText(data.target.description, { exact: true }),
    ).toBeVisible();
    await expect(
      drawer.getByText(data.target.plan, { exact: true }),
    ).toBeVisible();
    await columns(data.terms[0]!, () =>
      drawer.getByRole("button", { name: "关闭任务详情", exact: true }).click(),
    );
    await expect(drawer).toHaveCount(0);
    // Both original route navigations have completed their seven real reads.
    await search(data.terms[2]!);
    await columns("", () => clear.click());
    await expect(input).toHaveValue("");
    await expect(target).toHaveCount(0);
    await screenshot("clear-narrow-dark");
    expect(faults).toEqual([]);
  } finally {
    try {
      await seen.retire();
    } finally {
      seen.save(testInfo.status);
      page.off("pageerror", onError);
      mkdirSync(evidence, { recursive: true, mode: 0o700 });
      writeFileSync(
        join(evidence, "task-search-browser-observations.json"),
        JSON.stringify(
          {
            input_hash: process.env.AGENTEAM_TASK_SEARCH_WEB_INPUT_HASH,
            observations,
            faults,
          },
          null,
          2,
        ) + "\n",
        { mode: 0o600 },
      );
    }
  }
});
