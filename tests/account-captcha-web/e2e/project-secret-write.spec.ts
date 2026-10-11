import { test, expect, type Page, type Locator } from "@playwright/test";
import { secretWriteMaterial, observeSecretWrite, pasteSecretMaterial, secretInputsCleared, secretWriteScreenshot } from "./project-secret-write.helpers";
const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSecretWrite>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { if (!page.isClosed()) await secretInputsCleared(page, true); await seen?.retire(); } finally { seen?.save(info.status, info.errors); }
});
async function press(page: Page, button: Locator) {
  await expect(button).toHaveCount(1); await expect(button).toBeEnabled();
  await button.focus(); await expect(button).toBeFocused(); await page.keyboard.press("Enter");
}
for (const mode of ["secret-write-normal", "secret-write-lost"] as const) test(`writes Secret material and independently reads safe current metadata [${mode}]`, async ({ page }) => {
  const data = secretWriteMaterial(); expect(data.mode).toBe(mode);
  await page.setViewportSize({ width: 1440, height: 960 }); await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeSecretWrite(page, data); observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", { name: "项目 Secrets", exact: true }), catalog = region.getByRole("list", { name: "Secrets 目录", exact: true }), detail = region.locator(".secret-detail");
  await expect(region).toBeVisible(); await expect(catalog).toHaveCount(1);
  if (mode === "secret-write-normal") await expect(region.getByText("项目 Secrets 目录暂无条目。", { exact: true })).toBeVisible();
  else await expect(catalog).toBeVisible();
  await expect(catalog.getByRole("listitem")).toHaveCount(mode === "secret-write-normal" ? 0 : 1);
  seen.stage("initial-current"); await seen.initial();
  if (mode === "secret-write-normal") {
    await press(page, region.getByRole("button", { name: "创建 Secret", exact: true }));
    const form = region.getByRole("form", { name: "创建 Secret", exact: true });
    await expect(form.getByRole("textbox", { name: "Secret 名称", exact: true })).toBeFocused();
    await form.getByRole("textbox", { name: "Secret 名称", exact: true }).fill("BROWSER_SECRET"); await form.getByLabel("描述", { exact: true }).fill("创建说明");
    await pasteSecretMaterial(page); seen.stage("create");
    await press(page, form.getByRole("button", { name: "创建 Secret", exact: true }));
    await expect(detail.getByRole("heading", { name: "BROWSER_SECRET", exact: true })).toBeVisible(); await expect(detail.locator("dd").first()).toHaveText("1");
    await secretInputsCleared(page); await seen.current("1", "BROWSER_SECRET", "创建说明");
    await press(page, detail.getByRole("button", { name: "编辑名称与描述", exact: true }));
    const metadata = region.getByRole("form", { name: "编辑名称与描述", exact: true });
    await expect(metadata.getByRole("textbox", { name: "Secret 值", exact: true })).toHaveCount(0);
    await metadata.getByRole("textbox", { name: "Secret 名称", exact: true }).fill("RENAMED_BROWSER_SECRET"); await metadata.getByLabel("描述", { exact: true }).fill("更新后的安全说明");
    seen.stage("metadata-only"); await press(page, metadata.getByRole("button", { name: "保存修改", exact: true }));
    await expect(detail.getByRole("heading", { name: "RENAMED_BROWSER_SECRET", exact: true })).toBeVisible(); await expect(detail.locator("dd").first()).toHaveText("2");
    await secretInputsCleared(page); await seen.current("2", "RENAMED_BROWSER_SECRET", "更新后的安全说明");
  } else {
    await press(page, catalog.getByRole("button", { name: "BROWSER_SECRET", exact: true }));
    await expect(detail.getByRole("heading", { name: "BROWSER_SECRET", exact: true })).toBeVisible();
    await seen.seedSelected();
  }
  await press(page, detail.getByRole("button", { name: "替换 Secret 值", exact: true }));
  const replacement = region.getByRole("form", { name: "替换 Secret 值", exact: true });
  await expect(replacement.getByRole("textbox", { name: "Secret 值", exact: true })).toBeFocused();
  expect(await replacement.getByRole("textbox", { name: "Secret 值", exact: true }).evaluate((field: HTMLTextAreaElement) => field.value === "")).toBe(true);
  await pasteSecretMaterial(page); seen.stage("explicit-replace");
  await press(page, replacement.getByRole("button", { name: "确认替换", exact: true }));
  if (mode === "secret-write-lost") {
    const lookup = region.getByRole("button", { name: "检查原请求", exact: true });
    await expect(lookup).toBeVisible(); await secretInputsCleared(page);
    await expect(replacement.getByRole("button", { name: "确认替换", exact: true })).toBeDisabled();
    seen.stage("committed-response-lost"); await seen.uncertain();
    seen.stage("original-identity-lookup"); await press(page, lookup);
  }
  const version = mode === "secret-write-normal" ? "3" : "2", name = mode === "secret-write-normal" ? "RENAMED_BROWSER_SECRET" : "BROWSER_SECRET", description = mode === "secret-write-normal" ? "更新后的安全说明" : "现有安全元数据";
  await expect(detail.getByRole("heading", { name, exact: true })).toBeVisible(); await expect(detail.locator("dd").first()).toHaveText(version);
  await expect(region.getByRole("form")).toHaveCount(0); await secretInputsCleared(page);
  seen.stage("current-after-replace"); await seen.current(version, name, description);
  await secretWriteScreenshot(page, "1440-light-current");
  await page.setViewportSize({ width: 390, height: 844 }); await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
  await secretWriteScreenshot(page, "390-dark-current");
  seen.stage("retire"); await seen.finish();
});
