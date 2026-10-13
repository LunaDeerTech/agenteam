import { test, expect, type Page } from "@playwright/test";
import {
  stateFilterMaterial,
  observeStateFilter,
  stateFilterScreenshot,
  stateFilterFailure,
  type Material,
} from "./task-state-filter.helpers";

const observed = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeStateFilter>>
>();
const materials = new WeakMap<Page, Material>();
test.afterEach(async ({ page }, info) => {
  if (info.status !== "passed")
    await stateFilterFailure(page, materials.get(page));
  await observed.get(page)?.save(
    info.status ?? "unknown",
    info.errors.flatMap((error) =>
      [
        ...(error.stack ?? "").matchAll(
          /task-state-filter\.(?:spec|helpers)\.ts:\d+:\d+/g,
        ),
      ].map((match) => match[0]),
    ),
  );
});
const states = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
];
const labels = [
  "待规划",
  "待执行",
  "进行中",
  "评审中",
  "受阻",
  "已完成",
  "已取消",
];
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled();
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function select(page: Page, label: string, value: string) {
  await press(page, label);
  const option = page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: value, exact: true });
  await option.focus();
  await page.keyboard.press("Enter");
  await expect(button(page, label)).toBeFocused();
  await expect(button(page, label)).toContainText(value);
}

test("Owner filters the selected Sprint by state through original pages and detail routes", async ({
  page,
}) => {
  const data = stateFilterMaterial();
  materials.set(page, data);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value: data.cookie,
        url: data.origin,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
  const seen = await observeStateFilter(page, data);
  observed.set(page, seen);
  const form = page.getByRole("search", {
    name: "Sprint 任务搜索",
    exact: true,
  });
  const input = form.getByRole("searchbox", { name: "搜索任务", exact: true });
  const column = (state: string) =>
    page
      .locator(".task-column")
      .filter({
        has: page.getByRole("heading", {
          name: labels[states.indexOf(state)]!,
          exact: true,
        }),
      });
  const target = button(page, data.task.title);
  const status = page.locator(".task-search-status");
  async function settled(before: number, added: number) {
    await expect.poll(() => seen.count("tasks")).toBe(before + added);
    await seen.verify();
    expect(seen.lists()).toHaveLength(before + added);
    return seen.lists().slice(before);
  }
  function query(
    q: URLSearchParams,
    state: string,
    assignee?: string,
    cursor?: string,
  ) {
    expect(Object.fromEntries(q)).toEqual({
      sprint_id: data.task.sprint_id,
      state,
      limit: "50",
      ...(assignee
        ? {
            text: data.term,
            priority: "high",
            type: "bug",
            assignee_agent_id: assignee,
          }
        : {}),
      ...(cursor ? { cursor } : {}),
    });
  }
  async function included(state: string, loaded: number) {
    await expect(page.locator(".task-column")).toHaveCount(7);
    for (const candidate of states) {
      const col = column(candidate);
      if (candidate === state) {
        await expect(col.locator('[aria-label="已读取任务数"]')).toHaveText(
          String(loaded),
        );
        await expect(col.locator(".task-card")).toHaveCount(loaded);
        await expect(
          col.getByText("当前筛选未包含此状态", { exact: true }),
        ).toHaveCount(0);
      } else {
        await expect(
          col.getByText("当前筛选未包含此状态", { exact: true }),
        ).toBeVisible();
        await expect(
          col.locator('[aria-label="已读取任务数"],button'),
        ).toHaveCount(0);
      }
    }
  }
  await page.goto(data.route);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(form).toBeVisible();
  const initial = await settled(0, 7);
  expect(initial.map((row) => row.query.get("state"))).toEqual(states);
  initial.forEach((row) => query(row.query, row.query.get("state")!));
  const agents = seen.latest("agents").items as {
    id: string;
    name: string;
    display_name: string | null;
  }[];
  const agent = agents.find((row) => row.id === data.task.assignee_agent_id)!;
  expect(agent).toBeDefined();
  const agentLabel =
    agent.display_name && agent.display_name !== agent.name
      ? `${agent.display_name} (${agent.name})`
      : agent.name;

  seen.stage("backlog-combination");
  await input.fill(data.term);
  await select(page, "筛选状态", "待规划");
  await select(page, "筛选优先级", "高");
  await select(page, "筛选类型", "缺陷");
  await select(page, "筛选 Agent", "未指派");
  await seen.verify();
  expect(seen.count("tasks")).toBe(7);
  await press(page, "搜索");
  const backlog = (await settled(7, 1))[0]!;
  query(backlog.query, "backlog", "null");
  expect(backlog.value.items).toHaveLength(50);
  expect(backlog.value.next_cursor).toEqual(expect.any(String));
  const cursor = backlog.value.next_cursor as string;
  expect(cursor.length).toBeGreaterThan(0);
  await included("backlog", 50);
  await expect(status).toContainText("状态：待规划");
  await input.focus();
  await stateFilterScreenshot(page, "wide-light-backlog-first-page");

  seen.stage("draft-keeps-original-cursor");
  await select(page, "筛选状态", "待执行");
  await seen.verify();
  expect(seen.count("tasks")).toBe(8);
  await expect(status).toContainText("状态：待规划");
  await press(page, "更多待规划任务");
  const continuation = (await settled(8, 1))[0]!;
  query(continuation.query, "backlog", "null", cursor);
  expect(continuation.value.items).toHaveLength(1);
  expect(continuation.value.next_cursor).toBeNull();
  const backlogIDs = [
    ...(backlog.value.items as { id: string }[]),
    ...(continuation.value.items as { id: string }[]),
  ].map((row) => row.id);
  expect(new Set(backlogIDs).size).toBe(51);
  expect([...backlogIDs].sort()).toEqual(
    data.rows
      .filter((row) => row.state === "backlog")
      .map((row) => row.id)
      .sort(),
  );
  await included("backlog", 51);
  await expect(button(page, "更多待规划任务")).toHaveCount(0);
  await expect(button(page, "筛选状态")).toContainText("待执行");
  await input.focus();
  await stateFilterScreenshot(page, "wide-light-paged-applied-backlog");

  seen.stage("todo-clears-old-page");
  await select(page, "筛选 Agent", agentLabel);
  await input.focus();
  await page.keyboard.press("Enter");
  const todo = (await settled(9, 1))[0]!;
  query(todo.query, "todo", data.task.assignee_agent_id!);
  expect((todo.value.items as { id: string }[]).map((row) => row.id)).toEqual([
    data.task.id,
  ]);
  expect(todo.value.next_cursor).toBeNull();
  await included("todo", 1);
  await expect(status).toContainText("状态：待执行");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  seen.stage("detail-retains-state");
  await expect(target).toBeEnabled();
  await target.focus();
  await page.keyboard.press("Enter");
  const detailReads = await settled(10, 1);
  query(detailReads[0]!.query, "todo", data.task.assignee_agent_id!);
  expect(seen.latest("task")).toEqual(data.task);
  const drawer = page.getByRole("dialog", { name: "任务详情", exact: true });
  await expect(
    drawer.getByRole("heading", { name: data.task.title, exact: true }),
  ).toBeVisible();
  expect(
    await drawer.evaluate((element) =>
      element.contains(document.activeElement),
    ),
  ).toBe(true);
  await drawer
    .getByRole("button", { name: "关闭任务详情", exact: true })
    .focus();
  await expect(
    drawer.getByRole("button", { name: "关闭任务详情", exact: true }),
  ).toBeFocused();
  await stateFilterScreenshot(page, "narrow-dark-selected-task-detail");
  await page.keyboard.press("Enter");
  const returned = await settled(11, 1);
  query(returned[0]!.query, "todo", data.task.assignee_agent_id!);
  await expect(drawer).toHaveCount(0);
  await expect(page).toHaveURL(
    new URL(`${data.route}/sprints/${data.task.sprint_id}`, data.origin).href,
  );
  await expect(input).toHaveValue(data.term);
  await expect(button(page, "筛选状态")).toContainText("待执行");
  await expect(button(page, "筛选优先级")).toContainText("高");
  await expect(button(page, "筛选类型")).toContainText("缺陷");
  await expect(button(page, "筛选 Agent")).toContainText(agentLabel);
  await included("todo", 1);
  await press(page, "筛选状态");
  await page.keyboard.press("Escape");
  await expect(button(page, "筛选状态")).toBeFocused();
  await stateFilterScreenshot(page, "narrow-dark-retained-state-focus");

  seen.stage("clear-all-filters");
  await press(page, "清除筛选");
  const cleared = await settled(12, 7);
  expect(cleared.map((row) => row.query.get("state"))).toEqual(states);
  cleared.forEach((row) => query(row.query, row.query.get("state")!));
  await expect(input).toHaveValue("");
  for (const [label, value] of [
    ["筛选状态", "全部状态"],
    ["筛选优先级", "全部优先级"],
    ["筛选类型", "全部类型"],
    ["筛选 Agent", "全部 Agent"],
  ])
    await expect(button(page, label!)).toContainText(value!);
  await expect(status).toHaveCount(0);
  await expect(
    page.locator('.task-column [aria-label="已读取任务数"]'),
  ).toHaveCount(7);
  await expect(
    page.getByText("当前筛选未包含此状态", { exact: true }),
  ).toHaveCount(0);
  await expect(column("backlog").locator(".task-card")).toHaveCount(50);
  await expect(column("todo").locator(".task-card")).toHaveCount(1);
  await expect(button(page, "清除筛选")).toBeDisabled();
  await seen.finish();
});
