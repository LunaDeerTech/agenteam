const { test, expect } = require("@playwright/test");

test.beforeEach(async ({ page }) => {
  await page.goto("/__test__/");
  await expect(
    page.getByRole("heading", { name: "共享 UI 测试夹具" }),
  ).toBeVisible();
});

async function insideViewport(page, locator) {
  const bounds = await locator.boundingBox();
  expect(bounds).not.toBeNull();
  const viewport = await page.evaluate(() => ({
    width: innerWidth,
    height: innerHeight,
  }));
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.y).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(viewport.height + 1);
}

test("input descriptions, select choice, and tabs respond to real keyboard interaction", async ({
  page,
}) => {
  const input = page.getByRole("textbox", { name: "名称", exact: true });
  await expect(input).toHaveAttribute("aria-invalid", "true");
  await expect(input).toHaveAccessibleDescription("填写测试名称 名称不能为空");
  await input.fill("浏览器测试");
  await expect(page.getByRole("status", { name: "输入结果" })).toHaveText(
    "浏览器测试",
  );
  await expect(input).not.toHaveAttribute("aria-invalid");
  await expect(page.getByRole("textbox", { name: "禁用输入" })).toBeDisabled();
  await expect(page.getByRole("combobox", { name: "禁用选择" })).toBeDisabled();
  const select = page.getByRole("combobox", { name: "选择项", exact: true });
  await select.focus();
  await select.press("ArrowDown");
  await expect(select).toBeFocused();
  await select.press("ArrowDown");
  const active = await select.getAttribute("aria-activedescendant");
  await expect(page.locator(`[id="${active}"]`)).toHaveText("Charlie");
  await select.press("Enter");
  await expect(select).toHaveText("Charlie");
  await expect(page.getByRole("status", { name: "选择结果" })).toHaveText(
    "charlie",
  );
  await select.press("ArrowDown");
  await select.press("Home");
  await select.press("Escape");
  await expect(select).toHaveText("Charlie");
  await expect(select).toBeFocused();
  await select.press("a");
  await select.press("Enter");
  await expect(select).toHaveText("Alpha");
  await select.press("ArrowDown");
  await select.press("Tab");
  await expect(page.getByRole("listbox")).toHaveCount(0);
  await expect(
    page.getByRole("tab", { name: "第一项", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowRight");
  const last = page.getByRole("tab", { name: "最后一项", exact: true });
  await expect(last).toBeFocused();
  await expect(last).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tabpanel", { name: "最后一项" })).toHaveText(
    "最后一项内容",
  );
  await last.press("Home");
  await expect(
    page.getByRole("tab", { name: "第一项", exact: true }),
  ).toBeFocused();
});

test("menu skips disabled actions and dismisses without stealing outside focus", async ({
  page,
}) => {
  const trigger = page.getByRole("button", { name: "操作菜单", exact: true });
  await trigger.focus();
  await trigger.press("ArrowUp");
  await expect(
    page.getByRole("menuitem", { name: "Last action" }),
  ).toBeFocused();
  await page.keyboard.press("Home");
  await page.keyboard.press("ArrowDown");
  await expect(
    page.getByRole("menuitem", { name: "Last action" }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("status", { name: "菜单结果" })).toHaveText(
    "last",
  );
  await expect(trigger).toBeFocused();
  await trigger.click();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
  await trigger.click();
  const outside = page.getByRole("button", { name: "外部按钮", exact: true });
  await outside.click();
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(outside).toBeFocused();
});

test("native dialog traps focus, supports nested popup and restores each opener", async ({
  page,
}) => {
  const opener = page.getByRole("button", {
    name: "打开测试弹窗",
    exact: true,
  });
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "测试弹窗", exact: true });
  await expect(dialog).toBeVisible();
  const close = dialog.getByRole("button", { name: "关闭", exact: true });
  const finish = dialog.getByRole("button", { name: "完成", exact: true });
  await close.focus();
  await page.keyboard.press("Shift+Tab");
  await expect(finish).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(close).toBeFocused();
  await page
    .getByRole("button", { name: "外部按钮", exact: true })
    .evaluate((button) => button.focus());
  await expect(close).toBeFocused();
  const select = dialog.getByRole("combobox", { name: "弹窗选择" });
  await select.click();
  await expect(dialog.getByRole("listbox")).toBeVisible();
  await insideViewport(page, dialog.getByRole("listbox"));
  await select.press("Escape");
  await expect(dialog).toBeVisible();
  await expect(select).toBeFocused();
  const menu = dialog.getByRole("button", { name: "弹窗菜单" });
  await menu.click();
  await expect(dialog.getByRole("menu")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  const nestedOpener = dialog.getByRole("button", { name: "打开第二层" });
  await nestedOpener.click();
  await expect(
    page.getByRole("dialog", { name: "第二层弹窗", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(nestedOpener).toBeFocused();
  await expect(dialog).toBeVisible();
  expect(
    await page
      .locator("body")
      .evaluate((body) => getComputedStyle(body).overflow),
  ).toBe("hidden");
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(opener).toBeFocused();
  await expect(page.getByRole("status", { name: "关闭原因" })).toHaveText(
    "escape",
  );
  expect(
    await page
      .locator("body")
      .evaluate((body) => getComputedStyle(body).overflow),
  ).not.toBe("hidden");
});

test("narrow dark controls stay reachable and backdrop dismissal restores focus", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 680 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  const select = page.getByRole("combobox", { name: "选择项", exact: true });
  await select.click();
  await insideViewport(page, page.getByRole("listbox"));
  await page.getByRole("option", { name: /较长的选项/ }).click();
  await expect(select).toContainText("较长的选项");
  const opener = page.getByRole("button", {
    name: "打开测试弹窗",
    exact: true,
  });
  await opener.click();
  const dialog = page.getByRole("dialog", { name: "测试弹窗", exact: true });
  await insideViewport(page, dialog);
  expect(
    await dialog.evaluate((element) => getComputedStyle(element).animationName),
  ).toBe("none");
  await dialog.getByRole("button", { name: "完成" }).scrollIntoViewIfNeeded();
  await expect(dialog.getByRole("button", { name: "完成" })).toBeInViewport();
  // A drag beginning inside must not be mistaken for a backdrop click.
  const bounds = await dialog.boundingBox();
  await page.mouse.move(bounds.x + 30, bounds.y + 20);
  await page.mouse.down();
  await page.mouse.move(2, 2);
  await page.mouse.up();
  await expect(dialog).toBeVisible();
  await page.mouse.click(2, 2);
  await expect(dialog).not.toBeVisible();
  await expect(opener).toBeFocused();
  await expect(page.getByRole("status", { name: "关闭原因" })).toHaveText(
    "outside",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
