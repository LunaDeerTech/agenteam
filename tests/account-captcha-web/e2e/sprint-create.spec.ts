import { test, expect, type Page } from "@playwright/test";
import {
  sprintCreateMaterial,
  observeSprintCreate,
  sprintCreateScreenshot,
} from "./sprint-create.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeSprintCreate>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled();
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
test("plans a real sibling without replacing the current Sprint [plan-another-sprint-with-existing-current]", async ({
  page,
}) => {
  const data = sprintCreateMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value: data.cookie,
        url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
  const seen = await observeSprintCreate(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(page.locator(".sprint-heading h2")).toHaveText(
    data.current_sprint_title,
  );
  await expect(button(page, "刷新任务看板")).toBeEnabled();
  await expect(page.getByText(data.task_title, { exact: true })).toBeVisible();
  await seen.verify();
  const originalProject = seen.sprintCreateRead("project");
  const originalParent = seen.sprintCreateRead("milestone");
  const originalSprint = seen.sprintCreateRead("sprint");
  expect(originalProject).toMatchObject({
    id: data.project_id,
    current_sprint_id: data.current_sprint_id,
    version: data.project_version,
  });
  expect(originalSprint).toMatchObject({
    id: data.current_sprint_id,
    state: "current",
  });
  expect(originalParent).toMatchObject({
    id: data.milestone_id,
    title: data.milestone_title,
  });
  await press(page, "新建 Sprint");
  const form = page.getByRole("form", { name: "新建 Sprint", exact: true });
  await expect(form).toBeVisible();
  await expect(form.getByLabel("Sprint 标题", { exact: true })).toBeFocused();
  const milestone = form.getByRole("button", {
    name: "所属 Milestone",
    exact: true,
  });
  await milestone.focus();
  await page.keyboard.press("Enter");
  const option = page
    .getByRole("listbox", { name: "所属 Milestone", exact: true })
    .getByRole("option", { name: data.milestone_title, exact: true });
  await option.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".ui-popover")).toHaveCount(0);
  await expect(milestone).toBeFocused();
  await form.getByLabel("Sprint 标题", { exact: true }).fill(data.title);
  await form.getByLabel("Sprint 描述", { exact: true }).fill(data.description);
  expect(seen.requests("create")).toHaveLength(0);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await sprintCreateScreenshot(page, data, "1440-light-create-form");
  await press(page, "确认创建 Sprint");
  await expect(form).toHaveCount(0);
  await expect(page.locator(".sprint-heading h2")).toHaveText(data.title);
  await expect(button(page, "刷新任务看板")).toBeEnabled();
  await expect(page.locator(".sprint-heading h2")).toBeFocused();
  await expect(page.locator(".sprint-heading")).toContainText("未启动");
  await expect(button(page, "启动 Sprint")).toBeDisabled();
  await expect(
    page.locator('nav[aria-label="Sprint 列表"] button[aria-current="page"]'),
  ).toContainText(data.title);
  await expect(
    page.locator('.task-board[aria-label="任务看板"]'),
  ).toBeVisible();
  await expect(page.locator(".task-board .task-column")).toHaveCount(7);
  await expect(page.locator(".task-board .empty-column")).toHaveCount(7);
  await seen.verify();
  await expect(page).toHaveURL(
    new URL(
      `${data.route}/sprints/${seen.createdID()}`,
      process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
    ).href,
  );
  seen.requireResult(originalProject, originalParent, originalSprint);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  await expect(page.locator(".sprint-heading h2")).toBeVisible();
  await sprintCreateScreenshot(page, data, "390-dark-planned-sprint");
  await seen.endDocument();
});
