import { test, expect, type Page } from "@playwright/test";
import {
  humanBlockerMaterial,
  observeHumanBlocker,
  humanBlockerScreenshot,
} from "./task-human-blocker.helpers";

// Real Owner + Work HTTP shell. This is not default App/SPA lifecycle or an
// Unknown fault matrix. Ready coverage is the UI gate; Transfer is never sent.
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeHumanBlocker>>
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
const form = (page: Page, adding: boolean) =>
  page.getByRole("form", {
    name: adding ? "添加人工阻塞" : "解除人工阻塞",
    exact: true,
  });
const field = (page: Page, adding: boolean) =>
  form(page, adding).getByRole("textbox", {
    name: adding ? "阻塞说明" : "解除说明",
    exact: true,
  });
const fact = (page: Page, label: string) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText(label, { exact: true }) })
    .locator("dd");
async function press(page: Page, name: string) {
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function selectAgent(page: Page, option: string) {
  await press(page, "执行 Agent");
  await page
    .getByRole("listbox", { name: "执行 Agent", exact: true })
    .getByRole("option", { name: option, exact: true })
    .focus();
  await page.keyboard.press("Enter");
  await expect(button(page, "执行 Agent")).toBeFocused();
}
async function unchangedTask(page: Page, version: string) {
  await expect(fact(page, "状态")).toHaveText("待规划");
  await expect(fact(page, "负责人")).toHaveText("未指派");
  await expect(fact(page, "版本")).toHaveText(version);
  await expect(button(page, "重新读取任务")).toBeEnabled();
}

async function runHumanBlocker(page: Page, narrow: boolean) {
  const data = humanBlockerMaterial();
  expect(data.mode).toBe(
    narrow ? "human-blocker-narrow" : "human-blocker-wide",
  );
  const theme = narrow ? "dark" : "light";
  await page.setViewportSize(
    narrow ? { width: 390, height: 844 } : { width: 1440, height: 960 },
  );
  await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
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
  const seen = await observeHumanBlocker(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
  await unchangedTask(page, "1");
  await expect(
    drawer(page).getByText("暂无阻塞记录。", { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.checkCurrent("initial");
  seen.checkWrites(0);

  await press(page, "添加人工阻塞");
  await expect(field(page, true)).toBeFocused();
  await press(page, "取消人工阻塞操作");
  await expect(form(page, true)).toHaveCount(0);
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  await seen.verify();
  seen.checkWrites(0);
  await page.keyboard.press("Enter");
  await expect(field(page, true)).toBeFocused();
  await field(page, true).fill(data.description);
  await press(page, "确认添加人工阻塞");
  await expect(form(page, true)).toHaveCount(0);
  await unchangedTask(page, "2");
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  const row = drawer(page).locator("ul.blockers > li");
  await expect(row).toHaveCount(1);
  await expect(row.getByText("未解除", { exact: true })).toBeVisible();
  await expect(row.locator("p").first()).toHaveText(data.description);
  await seen.verify();
  seen.checkReceipt("added");
  seen.checkCurrent("added");
  seen.checkWrites(1);

  await press(page, "指派并加入待执行");
  await expect(button(page, "执行 Agent")).toBeEnabled();
  await seen.verify();
  await selectAgent(page, seen.agentOption(data.worker_id));
  await expect(button(page, "确认指派")).toBeDisabled();
  await expect(
    drawer(page).getByText("提交前需要读取并确认全部阻塞已解除。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(button(page, "解除人工阻塞")).toBeDisabled();
  await humanBlockerScreenshot(page, data, "ready-blocked");
  await seen.verify();
  seen.checkWrites(1);

  // Leave the original Ready draft through the existing explicit discard flow.
  await press(page, "关闭任务详情");
  const leave = page.getByRole("dialog", { name: "离开任务", exact: true });
  await expect(leave).toBeVisible();
  await leave.getByRole("button", { name: "离开", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(drawer(page)).toHaveCount(0);
  await expect(button(page, data.initial.title)).toBeEnabled();
  await press(page, data.initial.title);
  await unchangedTask(page, "2");
  await expect(button(page, "解除人工阻塞")).toBeEnabled();
  await expect(button(page, "确认指派")).toHaveCount(0);
  await seen.verify();
  seen.checkCurrent("added");

  await press(page, "解除人工阻塞");
  await expect(field(page, false)).toBeFocused();
  await press(page, "取消人工阻塞操作");
  await expect(form(page, false)).toHaveCount(0);
  await expect(button(page, "解除人工阻塞")).toBeFocused();
  await unchangedTask(page, "2");
  await seen.verify();
  seen.checkCurrent("added");
  seen.checkWrites(1);
  await page.keyboard.press("Enter");
  await expect(field(page, false)).toBeFocused();
  if (!narrow) await field(page, false).fill(data.resolution);
  await expect(field(page, false)).toHaveValue(narrow ? "" : data.resolution);
  await press(page, "确认解除人工阻塞");
  await expect(form(page, false)).toHaveCount(0);
  await unchangedTask(page, "3");
  await expect(button(page, "添加人工阻塞")).toBeFocused();
  await expect(row).toHaveCount(1);
  await expect(row.getByText("已解除", { exact: true })).toBeVisible();
  await expect(row.getByText("未解除", { exact: true })).toHaveCount(0);
  await expect(button(page, "解除人工阻塞")).toHaveCount(0);
  await expect(row.locator("p")).toHaveCount(narrow ? 1 : 2);
  if (!narrow)
    await expect(row.locator("p").nth(1)).toHaveText(data.resolution);
  await seen.verify();
  seen.checkReceipt("resolved");
  seen.checkCurrent("resolved");
  seen.checkWrites(2);

  // A fresh normal read must retain the resolved record before Ready opens.
  await press(page, "重新读取任务");
  await unchangedTask(page, "3");
  await expect(row.getByText("已解除", { exact: true })).toBeVisible();
  await seen.verify();
  seen.checkCurrent("resolved");
  await press(page, "指派并加入待执行");
  await expect(button(page, "执行 Agent")).toBeEnabled();
  await seen.verify();
  await selectAgent(page, seen.agentOption(data.worker_id));
  await expect(button(page, "确认指派")).toBeEnabled();
  await expect(
    drawer(page).getByText("提交前需要读取并确认全部阻塞已解除。", {
      exact: true,
    }),
  ).toHaveCount(0);
  await unchangedTask(page, "3");
  await humanBlockerScreenshot(page, data, "ready-available");
  await seen.verify();
  seen.checkCurrent("resolved");
  seen.checkWrites(2);
}

test("real human blocker gates Ready and resolves with exact comment [human-blocker-wide]", async ({
  page,
}) => runHumanBlocker(page, false));
test("real human blocker cancels then resolves with null comment [human-blocker-narrow]", async ({
  page,
}) => runHumanBlocker(page, true));
