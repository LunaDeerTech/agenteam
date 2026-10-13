import { test, expect, type Page } from "@playwright/test";
import {
  dependencyMaterial,
  observeDependency,
  dependencyScreenshot,
} from "./task-dependency.helpers";

// Default public App with real owned PostgreSQL. Three original browser POSTs:
// Add and Resolve commit; the reverse edge returns the real cycle conflict.
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeDependency>>
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
const form = (page: Page, adding = true) =>
  page.getByRole("form", {
    name: adding ? "添加任务依赖" : "解除任务依赖",
    exact: true,
  });
const field = (page: Page, name: string) =>
  page.getByRole("textbox", { name, exact: true });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");
async function press(page: Page, name: string) {
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function taskFacts(
  page: Page,
  task: Record<string, any>,
  version: string,
) {
  await expect(fact(page, "任务 ID")).toHaveText(task.id);
  await expect(fact(page, "版本")).toHaveText(version);
  await expect(fact(page, "状态")).toHaveText("待规划");
  await expect(fact(page, "负责人")).toHaveText("未指派");
  await expect(button(page, "重新读取任务")).toBeEnabled();
}
async function choose(page: Page, task: Record<string, any>) {
  await press(page, "关联任务");
  const list = page.getByRole("listbox", { name: "关联任务", exact: true });
  await list
    .getByRole("option", { name: `${task.title} · ${task.id}`, exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(button(page, "关联任务")).toBeFocused();
  await expect(button(page, "关联任务")).toHaveText(
    `${task.title} · ${task.id}`,
  );
}
test("real cross-Sprint dependency, cycle refusal and retained history [dependency-current-read]", async ({
  page,
}) => {
  const data = dependencyMaterial();
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
  const seen = await observeDependency(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await taskFacts(page, data.initial, "1");
  await expect(
    drawer(page).getByText("暂无阻塞记录。", { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.current("source");
  seen.writes(0);
  await press(page, "添加任务依赖");
  await expect(form(page)).toBeVisible();
  await expect(field(page, "查找关联任务")).toBeFocused();
  await expect(button(page, "更多候选任务")).toBeEnabled();
  await seen.verify();
  seen.candidates(1);
  await press(page, "关联任务");
  const list = page.getByRole("listbox", { name: "关联任务", exact: true });
  await expect(
    list.getByRole("option", {
      name: `${data.initial.title} · ${data.initial.id}`,
      exact: true,
    }),
  ).toHaveCount(0);
  await expect(
    list.getByRole("option", {
      name: `${data.related.title} · ${data.related.id}`,
      exact: true,
    }),
  ).toHaveCount(0);
  await expect(
    list.getByRole("option", {
      name: `${data.foreign.title} · ${data.foreign.id}`,
      exact: true,
    }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  await press(page, "更多候选任务");
  await expect(button(page, "更多候选任务")).toHaveCount(0);
  await seen.verify();
  seen.candidates(2);
  seen.writes(0);
  await choose(page, data.related);
  await field(page, "依赖原因").fill(data.description);
  await press(page, "添加依赖");
  await expect(form(page)).toHaveCount(0);
  await taskFacts(page, data.initial, "2");
  await expect(button(page, "添加任务依赖")).toBeFocused();
  const row = drawer(page).locator("ul.blockers > li");
  await expect(row).toHaveCount(1);
  await expect(row.getByText("未解除", { exact: true })).toBeVisible();
  await expect(row.locator("p").first()).toHaveText(data.description);
  await expect(
    row.getByText(`关联任务：${data.related.id}`, { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.receipt("added");
  seen.current("source");
  seen.writes(1);
  await dependencyScreenshot(page, "wide-light-added");
  await press(page, "查看关联任务");
  await taskFacts(page, data.related, "1");
  await expect(fact(page, "Sprint")).toHaveText(data.sprints[1]!.title);
  await expect(
    drawer(page).getByText("暂无阻塞记录。", { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.current("related");
  seen.writes(1);
  await press(page, "添加任务依赖");
  await expect(field(page, "查找关联任务")).toBeEnabled();
  await seen.verify();
  seen.candidates(1);
  await field(page, "查找关联任务").fill(data.initial.title);
  await press(page, "查找任务");
  await expect(button(page, "更多候选任务")).toHaveCount(0);
  await seen.verify();
  seen.candidates("search");
  await choose(page, data.initial);
  await field(page, "依赖原因").fill(data.cycle_description);
  await press(page, "添加依赖");
  await expect(
    form(page).getByText("该依赖会形成循环，请选择其他关联任务。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(field(page, "依赖原因")).toHaveValue(data.cycle_description);
  await expect(field(page, "查找关联任务")).toHaveValue(data.initial.title);
  await expect(button(page, "关联任务")).toHaveText(
    `${data.initial.title} · ${data.initial.id}`,
  );
  await expect(button(page, "添加依赖")).toBeDisabled();
  await taskFacts(page, data.related, "1");
  await seen.verify();
  seen.cycle();
  seen.current("related");
  await dependencyScreenshot(page, "wide-light-cycle");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(field(page, "依赖原因")).toHaveValue(data.cycle_description);
  await dependencyScreenshot(page, "narrow-dark-cycle");
  await form(page).getByRole("button", { name: "取消", exact: true }).focus();
  await page.keyboard.press("Enter");
  const leave = page.getByRole("dialog", { name: "离开任务", exact: true });
  await expect(leave).toBeVisible();
  await leave.getByRole("button", { name: "离开", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(form(page)).toHaveCount(0);
  await expect(button(page, "添加任务依赖")).toBeDisabled();
  await expect(button(page, "重新读取任务")).toBeFocused();
  await seen.verify();
  seen.writes(2);
  await page.goBack();
  await taskFacts(page, data.initial, "2");
  await expect(button(page, "解除任务依赖")).toBeEnabled();
  await seen.verify();
  seen.current("source");
  seen.writes(2);
  await press(page, "解除任务依赖");
  await expect(field(page, "解除说明")).toBeFocused();
  await form(page, false)
    .getByRole("button", { name: "取消", exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(form(page, false)).toHaveCount(0);
  await expect(button(page, "解除任务依赖")).toBeFocused();
  await seen.verify();
  seen.writes(2);
  await page.keyboard.press("Enter");
  await expect(field(page, "解除说明")).toBeFocused();
  await field(page, "解除说明").fill(data.resolution);
  await press(page, "解除依赖");
  await expect(form(page, false)).toHaveCount(0);
  await taskFacts(page, data.initial, "3");
  await expect(button(page, "添加任务依赖")).toBeFocused();
  await expect(row).toHaveCount(1);
  await expect(row.getByText("已解除", { exact: true })).toBeVisible();
  await expect(button(page, "解除任务依赖")).toHaveCount(0);
  await expect(
    row.getByText(`关联任务：${data.related.id}`, { exact: true }),
  ).toBeVisible();
  await expect(row.getByText(data.resolution, { exact: true })).toBeVisible();
  await seen.verify();
  seen.receipt("resolved");
  seen.current("source");
  seen.writes(3);
  await press(page, "重新读取任务");
  await taskFacts(page, data.initial, "3");
  await expect(row.getByText("已解除", { exact: true })).toBeVisible();
  await seen.verify();
  seen.current("source");
  seen.writes(3);
  await dependencyScreenshot(page, "narrow-dark-resolved");
  await seen.finish();
});
