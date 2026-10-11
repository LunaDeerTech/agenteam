import { test, expect, type Page } from "@playwright/test";
import { sprintStartMaterial, observeSprintStart, sprintStartScreenshot } from "./sprint-start.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSprintStart>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { await seen?.retire(); } finally { seen?.save(info.status); }
});
const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled();
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function layout(page: Page, width: 1440 | 390, colorScheme: "light" | "dark") {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
}
async function runSprintStart(page: Page, lost: boolean) {
  const data = sprintStartMaterial();
  expect(data.mode).toBe(lost ? "lost-start-confirmation-only-lookup" : "paused-start-and-current-board");
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeSprintStart(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const heading = page.getByRole("heading", { name: data.title, exact: true });
  const confirmation = page.getByRole("form", { name: "启动 Sprint 确认", exact: true });
  await expect(heading).toBeVisible();
  await expect(button(page, "启动 Sprint")).toBeEnabled();
  await expect(button(page, "刷新任务看板")).toBeEnabled();
  await seen.verify();
  expect(seen.sprintRead("project").current_sprint_id).toBeNull();
  expect(seen.sprintRead("sprint")).toMatchObject({ id: data.sprint_id, state: "planned", version: data.version });
  await press(page, "启动 Sprint");
  await expect(button(page, "确认启动 Sprint")).toBeFocused();
  await expect(confirmation).toContainText("此操作不会改变 Scheduler 的启用状态");
  await press(page, "取消启动");
  await expect(confirmation).toHaveCount(0);
  await expect(button(page, "启动 Sprint")).toBeFocused();
  expect(seen.requests("start")).toHaveLength(0);
  await page.keyboard.press("Enter");
  await expect(button(page, "确认启动 Sprint")).toBeFocused();
  await layout(page, 1440, "light");
  await sprintStartScreenshot(page, data, "1440-light-start-confirmation");
  const projectsBefore = seen.requests("project").length;
  const sprintsBefore = seen.requests("sprint").length;
  await press(page, "确认启动 Sprint");
  if (lost) {
    const recovery = page.getByRole("region", { name: "启动结果恢复", exact: true });
    await expect(recovery.getByText("启动结果待确认。请查询原启动结果，不要重复启动。", { exact: true })).toBeVisible();
    await expect(confirmation).toHaveCount(0);
    await expect(button(page, "启动 Sprint")).toHaveCount(0);
    await seen.verify();
    expect(seen.requests("start")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    await layout(page, 390, "dark");
    await sprintStartScreenshot(page, data, "390-dark-start-uncertain");
    await press(page, "查询原启动结果");
  }
  await expect(button(page, "刷新任务看板")).toBeEnabled();
  await expect(heading).toBeFocused();
  await expect(page.locator(".sprint-heading")).toContainText("当前");
  await expect(button(page, "启动 Sprint")).toHaveCount(0);
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByText(data.task_title, { exact: true })).toBeVisible();
  await expect.poll(() => seen.requests("project").length).toBeGreaterThan(projectsBefore);
  await expect.poll(() => seen.requests("sprint").length).toBeGreaterThan(sprintsBefore);
  await seen.verify();
  seen.requireResult();
  await layout(page, 390, "dark");
  await sprintStartScreenshot(page, data, "390-dark-current-board");
  await expect(page.getByText("启动 Sprint 不会改变 Scheduler 的启用状态；已暂停的调度保持暂停。", { exact: true })).toBeVisible();
  await seen.endDocument();
}
test("starts the selected paused Project Sprint and reads its current board [paused-start-and-current-board]", async ({ page }) => runSprintStart(page, false));
test("recovers only the original committed Sprint Start [lost-start-confirmation-only-lookup]", async ({ page }) => runSprintStart(page, true));
