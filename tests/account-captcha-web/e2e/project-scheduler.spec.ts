import { test, expect, type Page } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  schedulerMaterial,
  observeScheduler,
  schedulerScreenshot,
  schedulerLimit,
} from "./project-scheduler.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeScheduler>>
>();
type Stage =
  | "material"
  | "settings"
  | "scheduler-link"
  | "initial-read"
  | "pause"
  | "paused-read"
  | "light-image"
  | "resume"
  | "resumed-read"
  | "unknown"
  | "lookup"
  | "confirmed-read"
  | "dark-image"
  | "complete";
const stages = new WeakMap<Page, Stage>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
    const mode = process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_CASE!;
    const locations = info.errors.flatMap((error) =>
      [
        ...(error.stack ?? "").matchAll(
          /\b((?:project-scheduler\.(?:spec|helpers)|task-review\.helpers)\.ts):(\d+):(\d+)/g,
        ),
      ].map((match) => ({
        file: match[1],
        line: Number(match[2]),
        column: Number(match[3]),
      })),
    );
    writeFileSync(
      join(
        process.env.AGENTEAM_PROJECT_SCHEDULER_WEB_EVIDENCE!,
        `project-scheduler-${mode}-diagnostic.json`,
      ),
      JSON.stringify(
        {
          stage: stages.get(page) ?? "material",
          test_status: info.status,
          locations,
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
});
async function press(page: Page, name: string) {
  const button = page.getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}
async function open(page: Page) {
  stages.set(page, "material");
  const data = schedulerMaterial();
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
  const seen = await observeScheduler(page, data);
  observations.set(page, seen);
  stages.set(page, "settings");
  await page.goto(data.route.slice(0, -"/scheduler".length));
  const group = page.getByRole("button", { name: "Scheduler", exact: true });
  await expect(group).toBeVisible();
  await expect(group).toHaveAttribute("aria-expanded", /^(true|false)$/);
  if ((await group.getAttribute("aria-expanded")) === "false")
    await press(page, "Scheduler");
  await expect(group).toHaveAttribute("aria-expanded", "true");
  stages.set(page, "scheduler-link");
  const link = page.getByRole("link", { name: "调度配置", exact: true });
  await expect(link).toBeVisible();
  await link.focus();
  await expect(link).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(
    new URL(data.route, process.env.AGENTEAM_AUTH_WEB_ORIGIN!).href,
  );
  const screen = page.getByRole("region", { name: "调度配置", exact: true });
  stages.set(page, "initial-read");
  await expect(
    screen.getByRole("heading", { name: "调度配置", exact: true }),
  ).toBeVisible();
  const form = screen.getByRole("form", { name: "调度配置", exact: true });
  await expect(
    form.getByRole("switch", { name: "启用自动调度", exact: true }),
  ).toBeChecked();
  await expect(
    form.getByRole("checkbox", { name: "不限制并发数量", exact: true }),
  ).toBeChecked();
  await expect(
    page.getByRole("button", { name: "保存调度配置", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  seen.requireCurrent("2");
  const fact = (label: string) =>
    screen
      .locator("dl > div")
      .filter({ has: page.getByText(label, { exact: true }) })
      .locator("dd");
  const pause = async () => {
    stages.set(page, "pause");
    await form
      .getByRole("switch", { name: "启用自动调度", exact: true })
      .uncheck();
    await form
      .getByRole("checkbox", { name: "不限制并发数量", exact: true })
      .uncheck();
    await form
      .getByRole("textbox", { name: "并发上限", exact: true })
      .fill(schedulerLimit);
    await press(page, "保存调度配置");
  };
  return { data, seen, form, screen, fact, pause };
}

test("pauses with an integer limit above Number precision then resumes without a limit [pause-limit-and-resume]", async ({
  page,
}) => {
  const { data, seen, form, fact, pause } = await open(page);
  await pause();
  stages.set(page, "paused-read");
  await expect(fact("当前自动调度")).toHaveText("已暂停");
  await expect(fact("当前并发上限")).toHaveText(schedulerLimit);
  await expect(
    page.getByText("操作已确认，并已读取当前调度配置。", { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.requireCurrent("3");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  stages.set(page, "light-image");
  await schedulerScreenshot(page, data, "1440-light-paused");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  stages.set(page, "resume");
  await form.getByRole("switch", { name: "启用自动调度", exact: true }).check();
  await form
    .getByRole("checkbox", { name: "不限制并发数量", exact: true })
    .check();
  await press(page, "保存调度配置");
  stages.set(page, "resumed-read");
  await expect(fact("当前自动调度")).toHaveText("已启用");
  await expect(fact("当前并发上限")).toHaveText("不限制");
  await expect(
    page.getByRole("button", { name: "保存调度配置", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  seen.requireCurrent("4");
  expect(seen.requests("scheduler")).toHaveLength(3);
  stages.set(page, "dark-image");
  await schedulerScreenshot(page, data, "390-dark-resumed");
  await seen.endDocument();
  stages.set(page, "complete");
});

test("queries a committed but physically truncated update by its original key then reads current configuration [lost-update-confirmation-only-lookup]", async ({
  page,
}) => {
  const { data, seen, form, screen, fact, pause } = await open(page);
  await pause();
  stages.set(page, "unknown");
  await expect(
    screen.getByRole("heading", { name: "保存结果尚未确认", exact: true }),
  ).toBeVisible();
  await expect(screen.locator("dl")).toHaveCount(0);
  await expect(
    page.getByText("操作已确认，并已读取当前调度配置。", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "保存调度配置", exact: true }),
  ).toBeDisabled();
  await expect(
    form.getByRole("textbox", { name: "并发上限", exact: true }),
  ).toHaveValue(schedulerLimit);
  await seen.verify();
  expect(seen.requests("update")).toHaveLength(1);
  expect(seen.requests("lookup")).toHaveLength(0);
  expect(seen.requests("scheduler")).toHaveLength(1);
  stages.set(page, "lookup");
  await press(page, "查证原操作");
  await expect(fact("当前自动调度")).toHaveText("已暂停");
  await expect(fact("当前并发上限")).toHaveText(schedulerLimit);
  await expect(
    screen.getByRole("heading", { name: "保存结果尚未确认", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "保存调度配置", exact: true }),
  ).toBeEnabled();
  stages.set(page, "confirmed-read");
  await seen.verify();
  seen.requireCurrent("3");
  expect(seen.requests("update")).toHaveLength(1);
  expect(seen.requests("lookup")).toHaveLength(1);
  expect(seen.requests("scheduler")).toHaveLength(2);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  stages.set(page, "dark-image");
  await schedulerScreenshot(page, data, "390-dark-confirmed");
  await seen.endDocument();
  stages.set(page, "complete");
});
