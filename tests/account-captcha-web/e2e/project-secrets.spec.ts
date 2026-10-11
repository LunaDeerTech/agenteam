import { test, expect, type Page, type Locator } from "@playwright/test";
import { secretsMaterial, observeSecrets, secretsScreenshot } from "./project-secrets.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSecrets>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { await seen?.retire(); } finally { seen?.save(info.status, info.errors); }
});
async function press(page: Page, button: Locator) {
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}
test("pages the safe Secrets catalog and reads current metadata twice [metadata-pagination-and-detail]", async ({ page }) => {
  const data = secretsMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeSecrets(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", { name: "项目 Secrets", exact: true });
  const catalog = region.getByRole("list", { name: "Secrets 目录", exact: true });
  await expect(region).toBeVisible();
  await expect(catalog.getByRole("listitem")).toHaveCount(50);
  expect(await catalog.locator('.button-label[aria-hidden="false"]').allTextContents()).toEqual(data.items.slice(0, 50).map(value => value.name));
  for (const value of data.items.slice(0, 50)) await expect(catalog.getByRole("button", { name: value.name, exact: true })).toBeVisible();
  await expect(catalog.getByRole("button", { name: data.selected.name, exact: true })).toHaveCount(0);
  seen.stage("first-page"); await seen.verify(); seen.firstPage();
  await press(page, region.getByRole("button", { name: "加载更多 Secrets", exact: true }));
  await expect(catalog.getByRole("listitem")).toHaveCount(51);
  expect(await catalog.locator('.button-label[aria-hidden="false"]').allTextContents()).toEqual(data.items.map(value => value.name));
  await expect(region.getByRole("button", { name: "加载更多 Secrets", exact: true })).toHaveCount(0);
  const choice = catalog.getByRole("button", { name: data.selected.name, exact: true });
  await expect(choice).toHaveAttribute("aria-pressed", "false");
  seen.stage("second-page"); await seen.verify(); seen.secondPage();
  seen.stage("select-current-detail"); await press(page, choice);
  const detail = region.locator(".secret-detail"), heading = detail.getByRole("heading", { name: data.selected.name, exact: true });
  await expect(heading).toBeVisible();
  await expect(heading).toBeFocused();
  await expect(choice).toHaveAttribute("aria-pressed", "true");
  await expect(detail.locator(".secret-description")).toHaveText(data.selected.description || "暂无描述");
  await expect(detail.locator("dd")).toHaveText([data.selected.version, data.selected.created_at, data.selected.updated_at, data.selected.id, data.project_id]);
  await seen.verify(); seen.selection();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await secretsScreenshot(page, "1440-light-detail");
  seen.stage("explicit-detail-refresh");
  await press(page, detail.getByRole("button", { name: "重新读取详情", exact: true }));
  await expect(heading).toBeVisible();
  await seen.verify(); seen.refresh();
  await expect(detail.locator("dd")).toHaveText([data.selected.version, data.selected.created_at, data.selected.updated_at, data.selected.id, data.project_id]);
  seen.stage("narrow-detail");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await heading.scrollIntoViewIfNeeded();
  await expect(heading).toBeVisible();
  await expect(choice).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
  await secretsScreenshot(page, "390-dark-detail");
  seen.stage("restore-selection-focus");
  await press(page, detail.getByRole("button", { name: "返回目录", exact: true }));
  await expect(detail).toHaveCount(0);
  await expect(choice).toBeFocused();
  await expect(choice).toHaveAttribute("aria-pressed", "false");
  await expect(region.getByRole("textbox")).toHaveCount(0);
  seen.stage("retire"); await seen.finish();
});
