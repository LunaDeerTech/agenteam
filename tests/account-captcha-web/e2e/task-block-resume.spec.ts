import { test, expect, type Page } from "@playwright/test";
import {
  blockResumeMaterial,
  observeBlockResume,
  blockResumeScreenshot,
  blockResumeFailure,
  type Material,
} from "./task-block-resume.helpers";

const observed = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeBlockResume>>
>();
const materials = new WeakMap<Page, Material>();
test.afterEach(async ({ page }, info) => {
  if (info.status !== "passed")
    await blockResumeFailure(page, materials.get(page));
  await observed.get(page)?.save(
    info.status ?? "unknown",
    info.errors.flatMap((error) =>
      [
        ...(error.stack ?? "").matchAll(
          /task-block-resume\.(?:spec|helpers)\.ts:\d+:\d+/g,
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

test("Owner marks the assigned Todo blocked and atomically resolves its final human cause when resuming", async ({
  page,
}) => {
  const data = blockResumeMaterial();
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
  const seen = await observeBlockResume(page, data);
  observed.set(page, seen);
  await page.goto(data.route);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  const heading = drawer(page).getByRole("heading", {
    name: data.task.title,
    exact: true,
  });
  await expect(heading).toBeVisible();
  const assignee = await fact(page, "负责人").innerText();
  expect(assignee).not.toBe("未指派");
  async function current(writes: number, state: "todo" | "blocked") {
    await expect(fact(page, "状态")).toHaveText(
      state === "todo" ? "待执行" : "受阻",
    );
    await expect(fact(page, "负责人")).toHaveText(assignee);
    await expect(fact(page, "版本")).toHaveText(
      String(BigInt(data.task.version) + BigInt(writes)),
    );
    await seen.verify();
    expect(seen.latest("task")).toMatchObject({
      id: data.task.id,
      state,
      assignee_agent_id: data.task.assignee_agent_id,
      version: String(BigInt(data.task.version) + BigInt(writes)),
    });
  }
  await current(0, "todo");
  seen.stage("human-cause-draft");
  await press(page, "添加人工阻塞");
  const add = page.getByRole("form", { name: "添加人工阻塞", exact: true });
  const description = add.getByRole("textbox", {
    name: "阻塞说明",
    exact: true,
  });
  await expect(description).toBeFocused();
  await description.fill(data.description);
  await blockResumeScreenshot(page, "wide-light-human-cause-draft");
  await press(page, "确认添加人工阻塞");
  await expect(add).toHaveCount(0);
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  await current(1, "todo");
  expect(seen.latest("add").task).toEqual(seen.latest("task"));
  const originalBlocker = seen.latest("add").blocker as Record<string, unknown>;
  expect(originalBlocker).toMatchObject({
    type: "waiting_for_human",
    description: data.description,
    resolved_at: null,
  });
  const rows = drawer(page).locator("ul.blockers > li");
  await expect(rows).toHaveCount(1);
  await expect(rows.first().getByText("未解除", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(() => Reflect.has(window, "__blocker_executed")),
  ).toBe(false);

  seen.stage("mark-blocked");
  await press(page, "标记受阻");
  const mark = page.getByRole("form", { name: "标记受阻", exact: true });
  await expect(
    mark.getByRole("textbox", { name: "状态变更说明", exact: true }),
  ).toBeFocused();
  await press(page, "保留当前状态");
  await expect(mark).toHaveCount(0);
  await expect(button(page, "标记受阻")).toBeFocused();
  expect(seen.count("block")).toBe(0);
  await page.keyboard.press("Enter");
  await expect(
    mark.getByRole("textbox", { name: "状态变更说明", exact: true }),
  ).toHaveValue("");
  await press(page, "确认标记受阻");
  await expect(mark).toHaveCount(0);
  await expect(heading).toBeFocused();
  await current(2, "blocked");
  expect(seen.latest("block").task).toEqual(seen.latest("task"));
  expect(seen.latest("block").task_event_ids).toHaveLength(1);
  expect(seen.latest("blockers").items).toEqual([originalBlocker]);
  await expect(rows.first().getByText("未解除", { exact: true })).toBeVisible();
  await expect(button(page, "解决阻塞并恢复待执行")).toBeEnabled();
  await blockResumeScreenshot(page, "wide-light-original-task-blocked");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  seen.stage("atomic-resume-draft");
  await press(page, "解决阻塞并恢复待执行");
  const resume = page.getByRole("form", {
    name: "解决阻塞并恢复待执行",
    exact: true,
  });
  const reason = resume.getByRole("checkbox", {
    name: `等待人工处理：${data.description.replace(/\s+/g, " ")}`,
    exact: true,
  });
  await expect(resume.getByRole("checkbox")).toHaveCount(1);
  await expect(reason).toBeFocused();
  await expect(reason).not.toBeChecked();
  await expect(button(page, "确认解决阻塞并恢复待执行")).toBeDisabled();
  await press(page, "保留当前状态");
  await expect(resume).toHaveCount(0);
  await expect(button(page, "解决阻塞并恢复待执行")).toBeFocused();
  expect(seen.count("resume")).toBe(0);
  await page.keyboard.press("Enter");
  await expect(reason).toBeFocused();
  await page.keyboard.press("Space");
  await expect(reason).toBeChecked();
  await expect(button(page, "恢复后的执行 Agent")).toContainText(
    "保留当前负责人",
  );
  await expect(
    resume.getByRole("textbox", { name: "状态变更说明", exact: true }),
  ).toHaveValue("");
  await expect(button(page, "确认解决阻塞并恢复待执行")).toBeEnabled();
  await blockResumeScreenshot(page, "narrow-dark-final-cause-selected");
  await press(page, "确认解决阻塞并恢复待执行");
  await expect(resume).toHaveCount(0);
  await expect(heading).toBeFocused();
  await current(3, "todo");
  expect(seen.latest("resume").task).toEqual(seen.latest("task"));
  expect(seen.latest("resume").task_event_ids).toHaveLength(2);
  const resolved = (
    seen.latest("blockers").items as Record<string, unknown>[]
  )[0]!;
  expect(resolved).toEqual({
    ...originalBlocker,
    resolved_at: expect.any(String),
    resolution_comment: null,
  });
  await expect(rows).toHaveCount(1);
  await expect(rows.first().getByText("已解除", { exact: true })).toBeVisible();
  await expect(button(page, "解决阻塞并恢复待执行")).toHaveCount(0);
  await expect(
    drawer(page).getByRole("region", { name: "任务历史", exact: true }),
  ).toBeVisible();
  await blockResumeScreenshot(page, "narrow-dark-atomic-todo-restored");
  await seen.finish();
});
