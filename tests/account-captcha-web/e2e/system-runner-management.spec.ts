import { test, expect, type Page, type Locator } from "@playwright/test";
import { systemRunnerMaterial, observeSystemRunner, systemRunnerScreenshot } from "./system-runner-management.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeSystemRunner>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { await seen?.retire(); } finally { seen?.save(info.status); }
});
const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
const detail = (page: Page) => page.locator('[aria-label="Runner 详情"]');
const panel = (page: Page) => page.locator('[aria-label="一次性登记材料"]');
const fact = (page: Page, name: string) => detail(page).locator("dt").filter({ hasText: new RegExp(`^${name}$`) }).locator("xpath=following-sibling::dd[1]");
async function press(target: Locator) { await expect(target).toBeEnabled(); await target.focus(); await target.press("Enter"); }
async function layout(page: Page, width: 1440 | 390, colorScheme: "light" | "dark") {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
}
async function run(page: Page, lost: boolean) {
  const data = systemRunnerMaterial(); expect(data.mode).toBe(lost ? "system-runner-lost" : "system-runner-normal");
  await page.setViewportSize({ width: 1440, height: 960 }); await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeSystemRunner(page, data); observations.set(page, seen);
  await page.goto(data.route); await expect(page.getByRole("heading", { name: "Runner 设备", exact: true })).toBeVisible();
  await expect(button(page, "创建 Runner")).toBeEnabled(); await seen.verify();
  expect(seen.systemRunnerRead("session").user.role).toBe("admin"); expect(seen.systemRunnerRead("runner-list")).toEqual({ items: [], next: null });

  seen.stage("create"); await press(button(page, "创建 Runner"));
  let editor = page.getByRole("dialog", { name: "创建 Runner", exact: true });
  await expect(editor.getByRole("textbox", { name: "Runner 名称", exact: true })).toBeFocused();
  await press(editor.getByRole("button", { name: "取消", exact: true }));
  await expect(editor).toHaveCount(0); await expect(button(page, "创建 Runner")).toBeFocused();
  expect(seen.requests("create")).toHaveLength(0); await button(page, "创建 Runner").press("Enter");
  editor = page.getByRole("dialog", { name: "创建 Runner", exact: true });
  await editor.getByRole("textbox", { name: "Runner 名称", exact: true }).fill(data.draft.name);
  await editor.getByRole("textbox", { name: "描述", exact: true }).fill(data.draft.description);
  await editor.getByRole("textbox", { name: "标签", exact: true }).fill(data.draft.tags.join("\n"));
  await editor.getByRole("textbox", { name: "根目录", exact: true }).fill(data.draft.root_path);
  if (!lost) await systemRunnerScreenshot(page, data.mode, "1440-light-create");
  await press(editor.getByRole("button", { name: "创建 Runner", exact: true }));
  if (lost) {
    seen.stage("uncertain");
    await expect(editor.getByRole("region", { name: "原命令状态未确认", exact: true })).toBeVisible();
    await expect(editor.getByRole("button", { name: "创建 Runner", exact: true })).toBeDisabled();
    await expect(panel(page)).toHaveCount(0); await seen.verify();
    expect(seen.requests("create")).toHaveLength(1); expect(seen.requests("lookup")).toHaveLength(0); expect(seen.requests("enrollment")).toHaveLength(0);
    await systemRunnerScreenshot(page, data.mode, "1440-light-uncertain");
    await press(editor.getByRole("button", { name: "检查原请求", exact: true }));
  }
  await expect(editor).toHaveCount(0); await expect(fact(page, "配置版本")).toHaveText("1");
  await expect(button(page, "编辑 Runner")).toBeEnabled(); await seen.verify();
  const first = seen.current("1", "1");
  if (lost) {
    seen.originalLookup(); expect(seen.receipt("lookup", 0)).toEqual({ state: "committed", receipt: seen.receipt("lookup", 0).receipt, token_available: false });
    expect(seen.receipt("lookup", 0).receipt.runner).toEqual(first);
    await expect(panel(page)).toHaveCount(0); await expect(page.getByRole("status").filter({ hasText: "历史回执不含登记材料" })).toBeVisible();
    expect(seen.requests("enrollment")).toHaveLength(0); expect(seen.requests("create")).toHaveLength(1);
  } else {
    await verifyAndDismissMaterial(page, seen);
    const receipt = seen.receipt("create", 0); expect(receipt.receipt.runner).toEqual(first); expect(receipt.token_available).toBe(true);
    expect(Object.hasOwn(receipt, "enrollment_token")).toBe(false);
    seen.stage("edit"); await press(button(page, "编辑 Runner"));
    const edit = page.getByRole("dialog", { name: "编辑 Runner", exact: true });
    await expect(edit.getByRole("textbox", { name: "Runner 名称", exact: true })).toBeFocused();
    await expect(edit.getByRole("textbox", { name: "根目录", exact: true })).toHaveAttribute("readonly", "");
    await expect(edit.getByRole("textbox", { name: "根目录", exact: true })).toHaveValue(data.draft.root_path);
    await press(edit.getByRole("button", { name: "取消", exact: true })); await expect(edit).toHaveCount(0); await expect(button(page, "编辑 Runner")).toBeFocused();
    await button(page, "编辑 Runner").press("Enter");
    await edit.getByRole("textbox", { name: "Runner 名称", exact: true }).fill(data.patch.name);
    await edit.getByRole("textbox", { name: "描述", exact: true }).fill(data.patch.description);
    await edit.getByRole("textbox", { name: "标签", exact: true }).fill(data.patch.tags.join("\n"));
    await layout(page, 390, "dark"); await systemRunnerScreenshot(page, data.mode, "390-dark-edit");
    await press(edit.getByRole("button", { name: "保存修改", exact: true })); await expect(edit).toHaveCount(0);
    await expect(fact(page, "配置版本")).toHaveText("2"); await expect(button(page, "编辑 Runner")).toBeEnabled(); await seen.verify();
    expect(seen.receipt("update", 0).receipt.runner).toEqual(seen.current("2", "1")); await expect(panel(page)).toHaveCount(0);
  }

  seen.stage("explicit-enrollment"); await press(button(page, "重新签发登记材料"));
  const enrollment = page.getByRole("dialog", { name: "重新签发登记材料？", exact: true });
  await expect(enrollment).toContainText("旧密钥"); await expect(enrollment).toContainText("连接失效");
  expect(seen.requests("enrollment")).toHaveLength(0);
  await press(enrollment.getByRole("button", { name: "确认重新签发", exact: true })); await expect(enrollment).toHaveCount(0);
  await expect(fact(page, "配置版本")).toHaveText(lost ? "2" : "3"); await expect(button(page, "编辑 Runner")).toBeEnabled(); await seen.verify();
  expect(seen.receipt("enrollment", 0).receipt.runner).toEqual(seen.current(lost ? "2" : "3", "2"));
  await verifyAndDismissMaterial(page, seen);

  if (!lost) {
    seen.stage("revoke"); await press(button(page, "撤销 Runner 凭据"));
    const revoke = page.getByRole("dialog", { name: "撤销 Runner 凭据？", exact: true });
    await expect(revoke).toContainText("设备记录和项目 Mount"); expect(seen.requests("revoke")).toHaveLength(0);
    await press(revoke.getByRole("button", { name: "确认撤销凭据", exact: true })); await expect(revoke).toHaveCount(0);
    await expect(fact(page, "配置版本")).toHaveText("4"); await expect(button(page, "编辑 Runner")).toBeEnabled(); await seen.verify();
    expect(seen.receipt("revoke", 0).receipt.runner).toEqual(seen.current("4", "3")); await expect(panel(page)).toHaveCount(0);
  }
  seen.stage("explicit-current");
  const reads = seen.requests("runner-detail").length, lists = seen.requests("runner-list").length;
  await press(button(page, "重新读取详情")); await expect(button(page, "重新读取详情")).toBeEnabled();
  await expect.poll(() => seen.requests("runner-detail").length).toBe(reads + 1);
  await press(button(page, "重新读取目录")); await expect(button(page, "重新读取目录")).toBeEnabled();
  await expect.poll(() => seen.requests("runner-list").length).toBe(lists + 1); await seen.verify();
  const final = seen.current(lost ? "2" : "4", lost ? "2" : "3"); expect(seen.systemRunnerRead("runner-list")).toEqual({ items: [final], next: null });
  await expect(fact(page, "状态")).toHaveText("离线"); await expect(fact(page, "最后联系")).toHaveText("尚无报告");
  await layout(page, 390, "dark"); await detail(page).scrollIntoViewIfNeeded(); await systemRunnerScreenshot(page, data.mode, "390-dark-current");
  const menu = button(page, "系统设置栏目");
  await press(menu);
  const drawer = page.getByRole("dialog", { name: "系统设置栏目", exact: true });
  const link = drawer.getByRole("link", { name: "设备列表", exact: true });
  await expect(link).toHaveAttribute("href", "/system/runners"); await expect(link).toHaveAttribute("aria-current", "page");
  await page.keyboard.press("Escape"); await expect(drawer).toHaveCount(0); await expect(menu).toBeFocused();
  seen.stage("complete"); await seen.finish();
}

async function verifyAndDismissMaterial(page: Page, seen: Awaited<ReturnType<typeof observeSystemRunner>>) {
  await expect(panel(page)).toBeVisible(); await expect(panel(page)).toContainText("不要把它写入命令参数、环境变量或日志");
  const field = panel(page).getByLabel("登记 token", { exact: true });
  await expect(field).toHaveAttribute("type", "password"); await expect(field).toHaveAttribute("readonly", "");
  await seen.materialMatches(field);
  const old = await field.elementHandle();
  await press(panel(page).getByRole("button", { name: "关闭登记材料", exact: true })); await expect(panel(page)).toHaveCount(0);
  expect(await old!.evaluate(element => (element as HTMLInputElement).value === ""), "closed one-time field must be cleared").toBe(true);
  await old!.dispose();
}
test("Admin manages an offline Runner and explicit credentials [system-runner-normal]", async ({ page }) => run(page, false));
test("lost Create keeps original Lookup then explicitly issues new material [system-runner-lost]", async ({ page }) => run(page, true));
