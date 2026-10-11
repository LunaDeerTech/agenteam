import { test, expect, type Page } from "@playwright/test";
import {
  packageMaterial,
  observePackage,
  packageScreenshot,
} from "./skill-package.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observePackage>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
test("downloads the real fixed Skill revision [download]", async ({ page }) => {
  const data = packageMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      secure: false,
      sameSite: "Lax",
    },
  ]);
  const seen = await observePackage(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", { name: "项目技能库", exact: true });
  const catalog = region.getByRole("heading", {
    name: "项目技能库",
    level: 1,
    exact: true,
  });
  await expect(catalog).toBeVisible();
  await expect(catalog).toBeFocused();
  const list = region.getByRole("list", { name: "技能目录", exact: true });
  await expect(list.getByRole("listitem")).toHaveCount(2);
  await expect(
    region.getByRole("button", { name: "下载当前修订", exact: true }),
  ).toHaveCount(0);
  await seen.verify();
  const link = list.getByRole("link", { name: data.skill.name, exact: true });
  await link.focus();
  await expect(link).toBeFocused();
  await page.keyboard.press("Enter");
  const heading = region.getByRole("heading", {
    name: "技能详情",
    level: 1,
    exact: true,
  });
  await expect(heading).toBeVisible();
  await expect(heading).toBeFocused();
  await expect(
    region.getByRole("heading", {
      name: data.skill.name,
      level: 2,
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    region
      .locator(".skill-facts > div")
      .filter({ has: page.getByText("当前修订", { exact: true }) })
      .locator("dd"),
  ).toHaveText(data.revision);
  await seen.verify();
  const button = region.getByRole("button", {
    name: "下载当前修订",
    exact: true,
  });
  await expect(button).toBeEnabled();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await packageScreenshot(page, "1440-light-detail");
  await button.focus();
  await expect(button).toBeFocused();
  const downloadPromise = page.waitForEvent("download");
  await page.keyboard.press("Enter");
  await seen.downloaded(await downloadPromise);
  await expect(region.getByRole("status")).toHaveText(
    `修订 ${data.revision} 已交给浏览器保存。`,
  );
  await expect(button).toBeEnabled();
  await seen.verify();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  await packageScreenshot(page, "390-dark-downloaded");
  await seen.retire();
  seen.complete();
});
