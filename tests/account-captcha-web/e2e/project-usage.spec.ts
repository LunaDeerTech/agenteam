import { test, expect, type Page, type Locator } from "@playwright/test";
import { tokens, usageMaterial, observeUsage, usageScreenshot, type UsageRow } from "./project-usage.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeUsage>>>();
test.afterEach(async ({ page }, info) => { const seen = observations.get(page); try { await seen?.retire(); } finally { seen?.save(info.status, info.errors); } });
async function press(page: Page, button: Locator) {
  await expect(button).toBeEnabled(); await button.focus(); await expect(button).toBeFocused(); await page.keyboard.press("Enter");
}
function detailText(row: UsageRow): string[] {
  return [row.provider_name, row.model_name, row.provider_model_id, `${row.protocol} / ${row.model_type}`,
    "Agent / Agent 生成", row.attempt_index, "已派发", "成功", row.started_at, row.dispatched_at,
    row.final.finished_at, "无错误类别记录", row.usage.source === "provider" ? "Provider 返回" : "未知",
    ...tokens.map(field => row.usage[field] ?? "未知"), row.id, row.call_id, row.project_id,
    row.agent_id ?? "无关联", row.execution_id ?? "无关联", row.meeting_id ?? "无关联",
    row.provider_id, row.model_id, row.live_provider_id ?? "当前不可引用", row.live_model_id ?? "当前不可引用",
    row.provider_request_id ?? "无记录"];
}
test("reads exact usage pages, returns and refreshes the current window [paginated-current-read]", async ({ page }) => {
  const data = usageMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "__Host-agenteam_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: true }]);
  const seen = await observeUsage(page, data); observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", { name: "项目模型调用记录", exact: true }), list = region.getByRole("list", { name: "调用记录列表", exact: true });
  const previous = region.getByRole("button", { name: "上一页", exact: true }), next = region.getByRole("button", { name: "下一页", exact: true });
  const heading = region.getByRole("heading", { name: "调用明细", exact: true });
  async function assertRows(rows: UsageRow[]) {
    await expect(list.getByRole("listitem")).toHaveCount(rows.length);
    for (const [index, row] of rows.entries()) {
      const item = list.getByRole("listitem").nth(index);
      await expect(item.locator('.button-label[aria-hidden="false"]')).toHaveText(`${row.model_name} · Agent 生成 · 第 ${row.attempt_index} 次尝试`);
      await expect(item.locator("time")).toHaveText(row.started_at);
      await expect(item.locator(".usage-tokens dd")).toHaveText([row.usage.input_tokens ?? "未知", row.usage.output_tokens ?? "未知", row.usage.total_tokens ?? "未知"]);
      await expect(item).toContainText(row.provider_name);
    }
  }
  seen.stage("first-page"); await assertRows(data.items.slice(0, 50)); await seen.verify(); seen.page(0);
  await expect(previous).toBeDisabled(); await expect(next).toBeEnabled();
  // All four kinds come from formal Writer records. Details are already on this page.
  for (const value of [null, "0", "9007199254740993", "9223372036854775807"]) {
    const index = data.items.slice(0, 50).findIndex(row => row.usage.input_tokens === value), row = data.items[index]!;
    const choice = list.getByRole("listitem").nth(index).getByRole("button");
    await press(page, choice);
    const detail = region.locator(".usage-detail");
    await expect(detail.getByRole("heading")).toBeFocused(); await expect(choice).toHaveAttribute("aria-pressed", "true");
    await expect(detail.locator("dd")).toHaveText(detailText(row));
    await press(page, detail.getByRole("button", { name: "返回调用列表", exact: true }));
    await expect(detail).toHaveCount(0); await expect(choice).toBeFocused();
  }
  const choice = list.getByRole("listitem").first().getByRole("button");
  await press(page, choice);
  const detail = region.locator(".usage-detail");
  await expect(detail.locator("dd")).toHaveText(detailText(data.items[0]!));
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await detail.getByRole("heading").scrollIntoViewIfNeeded(); await usageScreenshot(page, "1440-light-detail");
  seen.stage("narrow-detail"); await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await detail.getByRole("heading").scrollIntoViewIfNeeded(); await usageScreenshot(page, "390-dark-detail");
  await press(page, detail.getByRole("button", { name: "返回调用列表", exact: true }));
  await expect(choice).toBeFocused(); await expect(detail).toHaveCount(0); await usageScreenshot(page, "390-dark-returned");
  seen.stage("next-page"); await press(page, next); await assertRows(data.items.slice(50));
  await expect(heading).toBeFocused(); await expect(next).toBeDisabled(); await expect(previous).toBeEnabled();
  await seen.verify(); seen.page(1);
  seen.stage("previous-page"); await press(page, previous); await assertRows(data.items.slice(0, 50));
  await expect(heading).toBeFocused(); await expect(previous).toBeDisabled(); await seen.verify(); seen.page(2);
  seen.stage("new-forward-page"); await press(page, next); await assertRows(data.items.slice(50)); await seen.verify(); seen.page(3);
  seen.stage("explicit-refresh"); await press(page, region.getByRole("button", { name: "刷新调用记录", exact: true }));
  await assertRows(data.items.slice(0, 50)); await expect(previous).toBeDisabled(); await seen.verify(); seen.page(4);
  await expect(region.getByRole("textbox")).toHaveCount(0);
  seen.stage("narrow-settings-menu");
  const menu = page.getByRole("button", { name: "项目设置栏目", exact: true });
  await press(page, menu);
  const drawer = page.getByRole("dialog", { name: "项目设置栏目", exact: true });
  await expect(drawer).toBeVisible();
  const usageLink = drawer.getByRole("link", { name: "调用记录", exact: true });
  await expect(usageLink).toHaveAttribute("href", data.route);
  await expect(usageLink).toHaveAttribute("aria-current", "page");
  await page.keyboard.press("Escape");
  await expect(drawer).toBeHidden();
  await expect(menu).toBeFocused();
  seen.stage("retire"); await seen.finish();
});
