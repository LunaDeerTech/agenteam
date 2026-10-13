import { test, expect, type Page } from "@playwright/test";
import {
  observeTaskCancel,
  taskCancelMaterial,
  taskCancelScreenshot,
} from "./task-cancel.helpers";

const seen = new WeakMap<Page, Awaited<ReturnType<typeof observeTaskCancel>>>();
test.afterEach(async ({ page }, info) => {
  const observer = seen.get(page);
  if (observer)
    await observer.save(
      info.status ?? "unknown",
      info.errors.flatMap((error) =>
        [
          ...(error.stack ?? "").matchAll(
            /task-cancel\.(?:spec|helpers)\.ts:\d+:\d+/g,
          ),
        ].map((match) => match[0]),
      ),
    );
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled();
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}

test("explicitly cancels both original tasks, preserves relations, and completes their Sprint", async ({
  page,
}) => {
  const data = taskCancelMaterial();
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
  const observer = await observeTaskCancel(page, data);
  seen.set(page, observer);
  await page.goto(data.route);
  const drawer = page.getByRole("dialog", { name: "任务详情", exact: true });
  await expect(
    drawer.getByRole("heading", { name: data.tasks[0].title, exact: true }),
  ).toBeVisible();
  await observer.verify();
  expect(observer.latest("task-0")).toMatchObject({
    id: data.tasks[0].id,
    state: "todo",
    assignee_agent_id: data.tasks[0].assignee_agent_id,
  });
  const assignee = await drawer
    .locator(".task-facts > div")
    .filter({ has: page.getByText("负责人", { exact: true }) })
    .locator("dd")
    .innerText();
  expect(assignee).not.toBe("未指派");
  observer.stage("assigned-confirmation");
  await press(page, "取消任务");
  await expect(button(page, "确认取消任务")).toBeFocused();
  await expect(
    page.getByRole("form", { name: "取消任务确认", exact: true }),
  ).toBeVisible();
  await page.getByLabel("取消说明", { exact: true }).fill(data.comment);
  await taskCancelScreenshot(page, "wide-light-confirmation");
  await press(page, "保留任务");
  await expect(button(page, "取消任务")).toBeFocused();
  expect(observer.count("cancel-0")).toBe(0);
  await press(page, "取消任务");
  await page.getByLabel("取消说明", { exact: true }).fill(data.comment);
  await press(page, "确认取消任务");
  await expect(drawer.getByText("任务已取消。", { exact: true })).toBeVisible();
  await expect(
    drawer.getByRole("heading", { name: data.tasks[0].title, exact: true }),
  ).toBeFocused();
  await expect(button(page, "取消任务")).toHaveCount(0);
  await observer.verify();
  expect(observer.latest("task-0")).toMatchObject({
    id: data.tasks[0].id,
    state: "cancelled",
    version: String(BigInt(data.tasks[0].version) + 1n),
    assignee_agent_id: data.tasks[0].assignee_agent_id,
  });
  await expect(
    drawer
      .locator(".task-facts > div")
      .filter({ has: page.getByText("负责人", { exact: true }) })
      .locator("dd"),
  ).toHaveText(assignee);
  const history = drawer.getByRole("region", { name: "任务历史", exact: true });
  await expect(history.getByText(data.comment, { exact: true })).toBeVisible();
  expect(
    await page.evaluate(() => Reflect.has(window, "__cancel_executed")),
  ).toBe(false);
  await taskCancelScreenshot(page, "wide-light-cancelled-history");
  await press(page, "关闭任务详情");
  await expect(drawer).toHaveCount(0);
  await press(page, data.tasks[1].title);
  await expect(
    drawer.getByRole("heading", { name: data.tasks[1].title, exact: true }),
  ).toBeVisible();
  await observer.verify();
  observer.stage("blocker-preserved");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(
    drawer.getByText(String(data.blocker.description), { exact: true }),
  ).toBeVisible();
  await press(page, "取消任务");
  await expect(button(page, "确认取消任务")).toBeFocused();
  await expect(page.getByLabel("取消说明", { exact: true })).toHaveValue("");
  await press(page, "确认取消任务");
  await expect(drawer.getByText("任务已取消。", { exact: true })).toBeVisible();
  await expect(
    drawer.getByRole("heading", { name: data.tasks[1].title, exact: true }),
  ).toBeFocused();
  await observer.verify();
  expect(observer.latest("task-1")).toMatchObject({
    id: data.tasks[1].id,
    state: "cancelled",
    assignee_agent_id: null,
    version: String(BigInt(data.tasks[1].version) + 1n),
  });
  expect(observer.latest("blockers-1").items).toEqual([
    expect.objectContaining({
      id: data.blocker.id,
      task_id: data.tasks[1].id,
      description: data.blocker.description,
      resolved_at: null,
      resolution_comment: null,
    }),
  ]);
  await expect(
    drawer.getByText(String(data.blocker.description), { exact: true }),
  ).toBeVisible();
  await expect(button(page, "解除人工阻塞")).toHaveCount(0);
  await taskCancelScreenshot(page, "narrow-dark-preserved-blocker");
  await press(page, "关闭任务详情");
  await expect(drawer).toHaveCount(0);
  await observer.verify();
  expect(observer.latest("cancelled").items).toEqual(
    expect.arrayContaining(
      data.tasks.map((task) =>
        expect.objectContaining({ id: task.id, state: "cancelled" }),
      ),
    ),
  );
  observer.stage("terminal-members-complete");
  await press(page, "完成 Sprint");
  await expect(button(page, "确认完成 Sprint")).toBeFocused();
  await press(page, "确认完成 Sprint");
  await expect(
    page.getByText("Sprint 已完成。可选择另一个待启动 Sprint，并明确启动。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: data.sprint.title, exact: true }),
  ).toBeFocused();
  await expect(button(page, "完成 Sprint")).toHaveCount(0);
  await observer.verify();
  expect(observer.latest("project").current_sprint_id).toBeNull();
  expect(observer.latest("sprint")).toMatchObject({
    id: data.sprint.id,
    state: "completed",
    version: String(BigInt(data.sprint.version) + 1n),
  });
  await taskCancelScreenshot(page, "narrow-dark-sprint-completed");
  await observer.finish();
});
