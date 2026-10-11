import { test, expect, type Page } from "@playwright/test";
import {
  editMaterial,
  editInputs,
  observeEdit,
  editScreenshot,
} from "./task-edit.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeEdit>>
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
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const form = (page: Page) =>
  page.getByRole("form", { name: "编辑任务", exact: true });
const field = (page: Page, name: string) =>
  form(page).getByRole("textbox", { name, exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");
async function press(page: Page, name: string) {
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function select(page: Page, label: string, option: string) {
  await press(page, label);
  await page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(button(page, label)).toBeFocused();
}
async function layout(
  page: Page,
  width: number,
  colorScheme: "light" | "dark",
) {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
}

async function runEdit(page: Page, lost: boolean) {
  const data = editMaterial();
  expect(data.mode).toBe(
    lost ? "lost-edit-confirmation-and-ready" : "changed-noop-clear-and-ready",
  );
  const title = editInputs(data)[0]!.request.title!;
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
  const seen = await observeEdit(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(button(page, data.initial.title)).toBeEnabled();
  await press(page, data.initial.title);
  await expect(fact(page, "状态")).toHaveText("待规划");
  await expect(fact(page, "负责人")).toHaveText("未指派");
  await expect(fact(page, "版本")).toHaveText("1");
  await seen.verify();

  // Empty cancellation exercises keyboard focus without emitting an intent.
  await press(page, "编辑任务");
  await expect(field(page, "任务标题")).toBeFocused();
  await expect(button(page, "保存任务")).toBeDisabled();
  await press(page, "取消编辑");
  await expect(form(page)).toHaveCount(0);
  await expect(button(page, "编辑任务")).toBeFocused();
  await page.keyboard.press("Enter");
  await field(page, "任务标题").fill(title);
  await field(page, "任务描述").fill(lost ? "" : "Browser saved description.");
  if (!lost) {
    await select(page, "任务类型", "功能");
    await select(page, "优先级", "高");
    await field(page, "执行计划").fill("Browser saved plan.");
  }
  await layout(page, 1440, "light");
  await editScreenshot(page, data, "1440-light-edit-form");
  await press(page, "保存任务");
  if (lost) {
    await expect(
      drawer(page).getByText(
        "提交结果待确认。请查询原操作结果，不要重复提交。",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(form(page)).toHaveCount(0);
    await expect(button(page, "编辑任务")).toHaveCount(0);
    await expect(button(page, "指派并加入待执行")).toHaveCount(0);
    await seen.verify();
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    expect(seen.requests("ready")).toHaveLength(0);
    await layout(page, 390, "dark");
    await editScreenshot(page, data, "390-dark-edit-uncertain");
    await press(page, "查询原操作结果");
  }
  await expect(form(page)).toHaveCount(0);
  await expect(fact(page, "版本")).toHaveText("2");
  await expect(
    drawer(page).getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  await expect(fact(page, "状态")).toHaveText("待规划");
  await seen.verify();
  if (lost) seen.originalLookup();
  const first = lost
    ? seen.receipt("lookup", 0).receipt
    : seen.receipt("update", 0);
  expect(first.changed).toBe(true);
  expect(first.task.assignee_agent_id).toBeNull();
  expect(first.task_event_id).toBeTruthy();
  expect(first.event_ids).toHaveLength(1);
  if (lost) {
    // Omitted fields stay exactly as captured; explicit empty stays empty.
    expect(first.task.plan).toBe(data.initial.plan);
    expect(first.task.type).toBe("task");
    expect(first.task.priority).toBe("medium");
    expect(first.task.description).toBe("");
  } else {
    await press(page, "编辑任务");
    await field(page, "任务标题").fill(title + " temporary");
    await field(page, "任务标题").fill(title);
    await press(page, "保存任务");
    await expect(form(page)).toHaveCount(0);
    await expect(
      drawer(page).getByText("已确认保存，任务内容没有变化。", { exact: true }),
    ).toBeVisible();
    await expect(fact(page, "版本")).toHaveText("2");
    await seen.verify();
    const noop = seen.receipt("update", 1);
    expect(noop.changed).toBe(false);
    expect(noop.task_event_id).toBeNull();
    expect(noop.event_ids).toEqual([]);
    expect(noop.task).toEqual(first.task);
    await press(page, "编辑任务");
    await field(page, "任务描述").fill("");
    await field(page, "执行计划").fill("");
    await layout(page, 390, "dark");
    await editScreenshot(page, data, "390-dark-clear-form");
    await press(page, "保存任务");
    await expect(form(page)).toHaveCount(0);
    await expect(fact(page, "版本")).toHaveText("3");
    await expect(
      drawer(page).getByText("暂无描述", { exact: true }),
    ).toBeVisible();
    await expect(
      drawer(page).getByText("暂无计划", { exact: true }),
    ).toBeVisible();
    await seen.verify();
    const clear = seen.receipt("update", 2);
    expect(clear.changed).toBe(true);
    expect(clear.task.description).toBe("");
    expect(clear.task.plan).toBe("");
    expect(clear.task.title).toBe(title);
    expect(clear.task.type).toBe("feature");
    expect(clear.task.priority).toBe("high");
  }
  expect(seen.requests("ready")).toHaveLength(0);
  await expect(fact(page, "负责人")).toHaveText("未指派");
  await expect(button(page, "指派并加入待执行")).toBeEnabled();
  await editScreenshot(page, data, "390-dark-saved-backlog");

  await press(page, "指派并加入待执行");
  await expect(button(page, "执行 Agent")).toBeEnabled();
  await seen.verify();
  await select(page, "执行 Agent", seen.agentOption(data.worker_id));
  await expect(button(page, "确认指派")).toBeEnabled();
  await press(page, "确认指派");
  await expect(fact(page, "状态")).toHaveText("待执行");
  await expect(fact(page, "版本")).toHaveText(lost ? "3" : "4");
  await expect(button(page, "编辑任务")).toHaveCount(0);
  await seen.verify();
  seen.requireSeparateReady();
  const ready = seen.receipt("ready", 0);
  expect(ready.task.assignee_agent_id).toBe(data.worker_id);
  expect(ready.task.id).toBe(data.task_id);
  await editScreenshot(page, data, "390-dark-separate-ready");
  await button(page, "关闭任务详情").click();
  await button(page, "刷新任务看板").click();
  await expect(button(page, title)).toBeEnabled();
  await press(page, title);
  await expect(fact(page, "版本")).toHaveText(lost ? "3" : "4");
  await expect(fact(page, "状态")).toHaveText("待执行");
  await seen.verify();
  seen.requireSeparateReady();
}

test("changed, no-op and explicit clear precede separate assignment [changed-noop-clear-and-ready]", async ({
  page,
}) => runEdit(page, false));
test("lost update confirmation only looks up the original patch [lost-edit-confirmation-and-ready]", async ({
  page,
}) => runEdit(page, true));
