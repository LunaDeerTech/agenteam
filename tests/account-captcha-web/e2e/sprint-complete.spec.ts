import { test, expect, type Page } from "@playwright/test";
import {
  observeSprintComplete,
  sprintCompleteMaterial,
  sprintCompleteScreenshot,
} from "./sprint-complete.helpers";
const seen = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeSprintComplete>>
>();
test.afterEach(async ({ page }, info) => {
  const observer = seen.get(page);
  if (observer)
    await observer.save(
      info.status ?? "unknown",
      info.errors.flatMap((e) =>
        [
          ...(e.stack ?? "").matchAll(
            /sprint-complete\.(?:spec|helpers)\.ts:\d+:\d+/g,
          ),
        ].map((m) => m[0]),
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
test("completes, explicitly starts next, and recovers only original completion", async ({
  page,
}) => {
  const data = sprintCompleteMaterial();
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
  const observer = await observeSprintComplete(page, data);
  seen.set(page, observer);
  await page.goto(data.route);
  const heading = () =>
    page.getByRole("heading", { name: data.first.title, exact: true });
  await expect(heading()).toBeVisible();
  await expect(button(page, "完成 Sprint")).toBeEnabled();
  await observer.verify();
  observer.stage("first-confirmation");
  await press(page, "完成 Sprint");
  await expect(button(page, "确认完成 Sprint")).toBeFocused();
  await expect(
    page.getByRole("form", { name: "完成 Sprint 确认", exact: true }),
  ).toBeVisible();
  await sprintCompleteScreenshot(page, "wide-light-confirmation");
  await press(page, "取消完成");
  await expect(button(page, "完成 Sprint")).toBeFocused();
  expect(observer.count("complete-first")).toBe(0);
  await press(page, "完成 Sprint");
  await press(page, "确认完成 Sprint");
  await expect(
    page.getByText("Sprint 已完成。可选择另一个待启动 Sprint，并明确启动。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(heading()).toBeFocused();
  await expect(button(page, "完成 Sprint")).toHaveCount(0);
  await observer.verify();
  expect(observer.latest("project").current_sprint_id).toBeNull();
  expect(observer.latest("first")).toMatchObject({
    id: data.first.id,
    state: "completed",
    version: String(BigInt(data.first.version) + 1n),
  });
  expect(observer.count("start-next")).toBe(0);
  await sprintCompleteScreenshot(page, "wide-light-completed");
  observer.stage("explicit-next");
  await page
    .getByRole("navigation", { name: "Sprint 列表", exact: true })
    .getByRole("button", { name: new RegExp(data.next.title) })
    .click();
  await expect(
    page.getByRole("heading", { name: data.next.title, exact: true }),
  ).toBeVisible();
  await expect(button(page, "启动 Sprint")).toBeEnabled();
  await observer.verify();
  expect(observer.latest("next")).toMatchObject({
    id: data.next.id,
    state: "planned",
    version: data.next.version,
  });
  await press(page, "启动 Sprint");
  await expect(button(page, "确认启动 Sprint")).toBeFocused();
  await press(page, "确认启动 Sprint");
  await expect(button(page, "完成 Sprint")).toBeEnabled();
  await observer.verify();
  expect(observer.latest("project").current_sprint_id).toBe(data.next.id);
  observer.stage("lost-completion");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await press(page, "完成 Sprint");
  await press(page, "确认完成 Sprint");
  const recovery = page.getByRole("region", {
    name: "完成结果恢复",
    exact: true,
  });
  await expect(recovery).toBeVisible();
  await expect(button(page, "完成 Sprint")).toHaveCount(0);
  await expect(button(page, "启动 Sprint")).toHaveCount(0);
  await observer.verify();
  expect(observer.count("complete-next")).toBe(1);
  expect(observer.count("lookup")).toBe(0);
  await sprintCompleteScreenshot(page, "narrow-dark-recovery");
  await press(page, "查询原完成结果");
  await expect(recovery).toHaveCount(0);
  await expect(
    page.getByText("Sprint 已完成。可选择另一个待启动 Sprint，并明确启动。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: data.next.title, exact: true }),
  ).toBeFocused();
  await observer.verify();
  expect(observer.latest("project").current_sprint_id).toBeNull();
  expect(observer.latest("next")).toMatchObject({
    id: data.next.id,
    state: "completed",
    version: String(BigInt(data.next.version) + 2n),
  });
  expect(observer.count("complete-next")).toBe(1);
  await sprintCompleteScreenshot(page, "narrow-dark-completed");
  await observer.finish();
});
