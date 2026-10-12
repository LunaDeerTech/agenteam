import { test, expect, type Page, type Locator } from "@playwright/test";
import { secretDeleteMaterial, observeSecretDelete, secretDeleteScreenshot } from "./project-secret-delete.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSecretDelete>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { await seen?.retire(); } finally { seen?.save(info.status, info.errors); }
});
async function press(page: Page, control: Locator) {
  await expect(control).toHaveCount(1); await expect(control).toBeEnabled();
  await control.focus(); await expect(control).toBeFocused(); await page.keyboard.press("Enter");
}
async function navigateSettings(page: Page, path: string, seen: Awaited<ReturnType<typeof observeSecretDelete>>) {
  const link = page.getByRole("navigation", { name: "项目设置", exact: true }).locator(`a[href="${path}"]`);
  await expect(link).toBeVisible();
  const href = await link.getAttribute("href");
  expect(href).toBe(path);
  const target = new URL(href!, page.url());
  expect(target.origin).toBe(new URL(page.url()).origin);
  // This case covers full-document navigation to the actual settings link.
  // Complete and retire the old document before navigation; do not turn a
  // retired SPA read into a successful current read or replay a failed request.
  await seen.verify();
  await seen.endDocument();
  await page.goto(target.href);
  await expect(page).toHaveURL(target.href);
}
test("referenced Secret is Busy until its Agent allowlist is cleared, then deletes safely", async ({ page }) => {
  const data = secretDeleteMaterial();
  await page.setViewportSize({ width: 1440, height: 960 }); await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeSecretDelete(page, data); observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", { name: "项目 Secrets", exact: true });
  const catalog = region.getByRole("list", { name: "Secrets 目录", exact: true }), detail = region.locator(".secret-detail");
  await expect(catalog.getByRole("listitem")).toHaveCount(2); seen.stage("initial"); await seen.initial();
  await press(page, catalog.getByRole("button", { name: data.target.name, exact: true }));
  await expect(detail.getByRole("heading", { name: data.target.name, exact: true })).toBeVisible();
  await expect(detail.locator("dd").first()).toHaveText("1"); await seen.selected();
  const remove = detail.getByRole("button", { name: "删除 Secret", exact: true });
  await press(page, remove);
  const dialog = page.getByRole("dialog", { name: "删除 Secret？", exact: true });
  await expect(dialog).toContainText(data.target.name); await expect(dialog).toContainText("版本 1");
  seen.stage("cancel"); await press(page, dialog.getByRole("button", { name: "取消", exact: true }));
  await expect(dialog).toHaveCount(0); await expect(remove).toBeFocused(); await seen.cancelled();
  await press(page, remove); await expect(dialog).toBeVisible();
  seen.stage("referenced-busy"); await press(page, dialog.getByRole("button", { name: "确认删除", exact: true }));
  await expect(dialog).toHaveCount(0);
  await expect(region.getByText("Secret 仍被引用，无法删除。请先解除引用后再重试。", { exact: true })).toBeVisible();
  await expect(detail.getByRole("heading", { name: data.target.name, exact: true })).toBeVisible();
  await expect(detail.locator("dd").first()).toHaveText("1"); await expect(remove).toBeEnabled(); await seen.busy();
  await secretDeleteScreenshot(page, "1440-light-busy");

  seen.stage("agent-original-configuration"); await navigateSettings(page, data.agent_route, seen);
  const agents = page.getByRole("region", { name: "Agent 目录", exact: true });
  const entry = agents.getByRole("button").filter({ has: page.locator("strong").filter({ hasText: new RegExp(`^${data.agent_name}$`) }) });
  await expect(entry).toHaveCount(1); await press(page, entry);
  const config = page.getByRole("region", { name: "Agent 配置", exact: true });
  const fact = (label: string) => config.locator("dt").filter({ hasText: new RegExp(`^${label}$`) }).locator("xpath=following-sibling::dd[1]");
  await expect(fact("版本")).toHaveText("1"); await expect(fact("Secret Variable 引用")).toContainText(data.target.id);
  await expect(page.getByRole("button", { name: "编辑配置", exact: true })).toBeEnabled(); await seen.originalAgent();
  await press(page, page.getByRole("button", { name: "编辑配置", exact: true }));
  const editor = page.getByRole("form", { name: "编辑 Agent 配置", exact: true });
  await expect(editor).toBeVisible();
  const reload = editor.getByRole("button", { name: "重新读取 Secret Variable 目录", exact: true });
  await press(page, reload); await expect(reload).toBeEnabled(); await seen.secretChoices();
  await press(page, editor.getByRole("button", { name: `移除 ${data.target.name}`, exact: true }));
  await expect(editor.getByRole("region", { name: "Secret Variable 白名单", exact: true }).getByRole("button", { name: /^移除 / })).toHaveCount(0);
  seen.stage("agent-clear-reference"); await press(page, editor.getByRole("button", { name: "保存配置", exact: true }));
  await expect(editor).toHaveCount(0); await expect(fact("版本")).toHaveText("2"); await expect(fact("Secret Variable 引用")).toHaveText("白名单为空");
  await expect(page.getByRole("button", { name: "编辑配置", exact: true })).toBeEnabled(); await seen.cleared();

  const priorDirectories = seen.requests("secrets").length;
  seen.stage("return-and-reread"); await navigateSettings(page, data.route, seen);
  await expect(catalog.getByRole("listitem")).toHaveCount(2);
  expect(seen.requests("secrets")).toHaveLength(priorDirectories + 1);
  await seen.returned();
  const priorDetails = seen.requests("secret").length;
  await press(page, catalog.getByRole("button", { name: data.target.name, exact: true }));
  await expect(detail.getByRole("heading", { name: data.target.name, exact: true })).toBeVisible();
  expect(seen.requests("secret")).toHaveLength(priorDetails + 1);
  await seen.selected();
  await press(page, remove); await expect(dialog).toBeVisible();
  seen.stage("delete"); await press(page, dialog.getByRole("button", { name: "确认删除", exact: true }));
  await expect(dialog).toHaveCount(0); await expect(detail).toHaveCount(0);
  await expect(catalog.getByRole("listitem")).toHaveCount(1);
  await expect(catalog.getByRole("button", { name: data.target.name, exact: true })).toHaveCount(0);
  await expect(catalog.getByRole("button", { name: data.remaining.name, exact: true })).toBeEnabled();
  expect(seen.requests("secrets")).toHaveLength(priorDirectories + 2);
  await seen.deleted();
  await press(page, catalog.getByRole("button", { name: data.remaining.name, exact: true }));
  await expect(detail.getByRole("heading", { name: data.remaining.name, exact: true })).toBeVisible();
  await expect(detail.locator("dd").first()).toHaveText("1");
  await page.setViewportSize({ width: 390, height: 844 }); await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
  await secretDeleteScreenshot(page, "390-dark-current");
  expect(seen.requests("secret")).toHaveLength(priorDetails + 2);
  seen.stage("complete"); await seen.finish();
});
