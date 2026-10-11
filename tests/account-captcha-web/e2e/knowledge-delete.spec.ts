import { test, expect, type Page, type Locator } from "@playwright/test";
import { deleteMaterial, observeDelete, deleteScreenshot } from "./knowledge-delete.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeDelete>>>();
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
for (const mode of ["delete-normal", "delete-lost"] as const) {
  test(`previews an exact subtree and confirms its deletion through current Owner reads [${mode}]`, async ({ page }) => {
    const data = deleteMaterial();
    expect(data.mode).toBe(mode);
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
    await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!, httpOnly: true, sameSite: "Lax", secure: false }]);
    const seen = await observeDelete(page, data);
    observations.set(page, seen);
    await page.goto(data.route);
    await expect(page.getByRole("region", { name: "项目知识库", exact: true })).toBeVisible();
    await expect(page.locator(".knowledge-document > h2")).toHaveText(data.root.title);
    await expect.poll(() => page.locator("pre.canonical-text").textContent()).toBe(data.root_text);
    await seen.verify(); seen.initial();
    seen.stage("preview");
    await press(page, page.getByRole("button", { name: "删除子树", exact: true }));
    const dialog = page.getByRole("dialog", { name: "删除文档及子文档", exact: true });
    await expect(dialog).toBeVisible();
    const scope = dialog.getByRole("list", { name: "完整删除范围", exact: true });
    await expect(scope.getByRole("listitem")).toHaveCount(2);
    await expect(scope.getByText(data.root.title, { exact: true })).toBeVisible();
    await expect(scope.getByText(data.child.title, { exact: true })).toBeVisible();
    await expect(scope.getByText(data.sibling.title, { exact: true })).toHaveCount(0);
    const confirmation = dialog.getByRole("button", { name: "确认删除", exact: true });
    await expect(confirmation).toBeDisabled();
    await seen.verify(); await seen.preview();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    await deleteScreenshot(page, data, "1440-light-preview");
    const consent = dialog.getByRole("checkbox", { name: "我已核对以上文档，确认删除整个子树", exact: true });
    await expect(consent).not.toBeChecked();
    await consent.focus();
    await expect(consent).toBeFocused();
    await page.keyboard.press("Space");
    await expect(consent).toBeChecked();
    seen.stage("delete");
    await press(page, confirmation);
    if (mode === "delete-lost") {
      seen.stage("uncertain");
      await expect(dialog.getByText("删除结果尚不确定，请查询原结果；不会自动重发。", { exact: true })).toBeVisible();
      await expect(dialog.getByRole("button", { name: "确认删除", exact: true })).toHaveCount(0);
      await expect(dialog.getByRole("button", { name: "重新预览", exact: true })).toHaveCount(0);
      await expect(dialog.getByRole("button", { name: "按原请求重放", exact: true })).toHaveCount(0);
      await expect(dialog.getByText(/删除已确认/)).toHaveCount(0);
      await seen.verify(); seen.uncertain();
      await page.setViewportSize({ width: 390, height: 844 });
      await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
      await deleteScreenshot(page, data, "390-dark-uncertain");
      seen.stage("lookup-original");
      await press(page, dialog.getByRole("button", { name: "查询原删除结果", exact: true }));
    }
    seen.stage("current-tombstone");
    await expect(dialog.getByText("删除已确认；已读取当前删除状态。后台清理待完成。", { exact: true })).toBeVisible();
    await expect(dialog.getByRole("list", { name: "完整删除范围", exact: true })).toHaveCount(0);
    await expect(dialog.getByRole("button", { name: "查询原删除结果", exact: true })).toHaveCount(0);
    await seen.verify(); seen.confirmed();
    const close = dialog.locator(".dialog-footer").getByRole("button", { name: "关闭", exact: true });
    await expect(close).toHaveCount(1);
    await press(page, close);
    await expect(dialog).toHaveCount(0);
    await expect(page.locator(".knowledge-document > h2")).toBeFocused();
    await expect(page.locator(".knowledge-document").getByRole("heading", { name: "文档已删除", exact: true })).toBeVisible();
    await expect(page.locator("pre.canonical-text")).toHaveCount(0);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
    seen.stage("retained-sibling");
    const trigger = page.getByRole("button", { name: "文档树", exact: true });
    await press(page, trigger);
    const drawer = page.getByRole("dialog", { name: "文档树", exact: true });
    const tree = drawer.getByRole("tree", { name: "知识库文档" });
    await expect(tree.locator(`[data-tree-id="${data.root_id}"]`)).toHaveCount(0);
    await expect(tree.locator(`[data-tree-id="${data.child_id}"]`)).toHaveCount(0);
    const sibling = tree.locator(`[data-tree-id="${data.sibling_id}"]`);
    await expect(sibling).toBeVisible();
    await deleteScreenshot(page, data, "390-dark-current-tree");
    await page.keyboard.press("Escape");
    await expect(drawer).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await press(page, trigger);
    await press(page, sibling);
    await expect(drawer).toHaveCount(0);
    await expect(page.locator(".knowledge-document > h2")).toHaveText(data.sibling.title);
    await expect.poll(() => page.locator("pre.canonical-text").textContent()).toBe(data.sibling_text);
    await deleteScreenshot(page, data, "390-dark-sibling-text");
    seen.stage("complete");
    await seen.finish();
  });
}
