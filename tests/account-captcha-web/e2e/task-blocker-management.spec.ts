import { test, expect, type Page } from "@playwright/test";
import {
  blockerManagementMaterial,
  observeBlockerManagement,
  blockerManagementScreenshot,
  blockerManagementFailure,
  type Material,
} from "./task-blocker-management.helpers";

const observed = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeBlockerManagement>>
>();
const materials = new WeakMap<Page, Material>();
test.afterEach(async ({ page }, info) => {
  // Capture the failed UI before any observer or fixture retirement.
  if (info.status !== "passed")
    await blockerManagementFailure(page, materials.get(page));
  await observed.get(page)?.save(
    info.status ?? "unknown",
    info.errors.flatMap((error) =>
      [
        ...(error.stack ?? "").matchAll(
          /task-blocker-management\.(?:spec|helpers)\.ts:\d+:\d+/g,
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
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator(".task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");

test("Owner manages human and dependency blockers on the original assigned Todo task", async ({
  page,
}) => {
  const data = blockerManagementMaterial();
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
  const seen = await observeBlockerManagement(page, data);
  observed.set(page, seen);
  await page.goto(data.route);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(
    drawer(page).getByRole("heading", { name: data.task.title, exact: true }),
  ).toBeVisible();
  const assignee = await fact(page, "负责人").innerText();
  expect(assignee).not.toBe("未指派");
  async function unchanged(writes: number) {
    await expect(fact(page, "状态")).toHaveText("待执行");
    await expect(fact(page, "负责人")).toHaveText(assignee);
    await expect(fact(page, "版本")).toHaveText(
      String(BigInt(data.task.version) + BigInt(writes)),
    );
    await seen.verify();
    expect(seen.latest("task")).toMatchObject({
      id: data.task.id,
      state: "todo",
      assignee_agent_id: data.task.assignee_agent_id,
      manual_rank: data.task.manual_rank,
      version: String(BigInt(data.task.version) + BigInt(writes)),
    });
  }
  await unchanged(0);
  seen.stage("human-draft");
  await press(page, "添加人工阻塞");
  const humanAdd = page.getByRole("form", {
    name: "添加人工阻塞",
    exact: true,
  });
  await expect(
    humanAdd.getByRole("textbox", { name: "阻塞说明", exact: true }),
  ).toBeFocused();
  await press(page, "取消人工阻塞操作");
  await expect(humanAdd).toHaveCount(0);
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  expect(seen.count("add-0")).toBe(0);
  await page.keyboard.press("Enter");
  await humanAdd
    .getByRole("textbox", { name: "阻塞说明", exact: true })
    .fill(data.description);
  await blockerManagementScreenshot(page, "wide-light-human-draft");
  await press(page, "确认添加人工阻塞");
  await expect(humanAdd).toHaveCount(0);
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  await unchanged(1);
  const rows = drawer(page).locator("ul.blockers > li");
  await expect(rows).toHaveCount(1);
  await expect(rows.first().getByText("未解除", { exact: true })).toBeVisible();
  await expect(rows.first().locator("p").first()).toHaveText(data.description);
  expect(
    await page.evaluate(() => Reflect.has(window, "__blocker_executed")),
  ).toBe(false);
  expect(seen.latest("add-0").task).toEqual(seen.latest("task"));
  seen.stage("human-resolve");
  await press(page, "解除人工阻塞");
  const humanResolve = page.getByRole("form", {
    name: "解除人工阻塞",
    exact: true,
  });
  await expect(
    humanResolve.getByRole("textbox", { name: "解除说明", exact: true }),
  ).toBeFocused();
  await humanResolve
    .getByRole("textbox", { name: "解除说明", exact: true })
    .fill(data.resolution);
  await press(page, "确认解除人工阻塞");
  await expect(humanResolve).toHaveCount(0);
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  await unchanged(2);
  await expect(rows.first().getByText("已解除", { exact: true })).toBeVisible();
  await expect(
    rows.first().getByText(data.resolution, { exact: true }),
  ).toBeVisible();
  await blockerManagementScreenshot(page, "wide-light-human-resolved");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  seen.stage("dependency-draft");
  await press(page, "添加任务依赖");
  const addDependency = page.getByRole("form", {
    name: "添加任务依赖",
    exact: true,
  });
  await expect(
    addDependency.getByRole("textbox", { name: "查找关联任务", exact: true }),
  ).toBeFocused();
  await addDependency
    .getByRole("textbox", { name: "查找关联任务", exact: true })
    .fill(data.related.title);
  await page.keyboard.press("Enter");
  await expect(button(page, "关联任务")).toBeFocused();
  await page.keyboard.press("Enter");
  const option = page
    .getByRole("listbox", { name: "关联任务", exact: true })
    .getByRole("option", {
      name: `${data.related.title} · ${data.related.id}`,
      exact: true,
    });
  await option.focus();
  await page.keyboard.press("Enter");
  await expect(button(page, "关联任务")).toBeFocused();
  await addDependency
    .getByRole("textbox", { name: "依赖原因", exact: true })
    .fill(data.dependency);
  await press(page, "添加依赖");
  await expect(addDependency).toHaveCount(0);
  await expect(button(page, "添加任务依赖")).toBeFocused();
  await unchanged(3);
  await expect(rows).toHaveCount(2);
  const dependencyRow = rows.filter({
    has: page.getByText(data.dependency, { exact: true }),
  });
  await expect(
    dependencyRow.getByText("未解除", { exact: true }),
  ).toBeVisible();
  expect(seen.latest("add-1").blocker).toMatchObject({
    type: "rely_on",
    related_task_id: data.related.id,
    resolved_at: null,
  });
  await blockerManagementScreenshot(page, "narrow-dark-dependency-added");
  seen.stage("dependency-resolve");
  await press(page, "解除任务依赖");
  const resolveDependency = page.getByRole("form", {
    name: "解除任务依赖",
    exact: true,
  });
  await expect(
    resolveDependency.getByRole("textbox", { name: "解除说明", exact: true }),
  ).toBeFocused();
  await expect(
    resolveDependency.getByRole("textbox", { name: "解除说明", exact: true }),
  ).toHaveValue("");
  await press(page, "解除依赖");
  await expect(resolveDependency).toHaveCount(0);
  await expect(button(page, "添加任务依赖")).toBeFocused();
  await unchanged(4);
  await expect(
    dependencyRow.getByText("已解除", { exact: true }),
  ).toBeVisible();
  expect(seen.latest("resolve-1").blocker).toMatchObject({
    type: "rely_on",
    related_task_id: data.related.id,
    resolution_comment: null,
  });
  await expect(button(page, "解除任务依赖")).toHaveCount(0);
  await expect(
    drawer(page).getByRole("region", { name: "任务历史", exact: true }),
  ).toBeVisible();
  await blockerManagementScreenshot(page, "narrow-dark-both-resolved");
  await seen.finish();
});
