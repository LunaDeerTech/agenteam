const { test, expect } = require("@playwright/test");

test("system settings navigation and appearance persist across a reload", async ({
  page,
}) => {
  const errors = [];
  const apiRequests = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (["xhr", "fetch"].includes(request.resourceType()))
      apiRequests.push(request.url());
  });
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/system/users");
  await expect(
    page.getByRole("heading", { name: "用户", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("当前为界面预览。连接账号服务后可查看用户。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeDisabled();
  const settings = page.getByRole("navigation", {
    name: "系统设置",
    exact: true,
  });
  await expect(settings.getByRole("link")).toHaveCount(10);
  await settings.getByRole("link", { name: "模型目录", exact: true }).click();
  await expect(page).toHaveURL(/\/system\/models$/);
  await expect(
    page.getByRole("heading", { name: "模型目录", exact: true }),
  ).toBeFocused();
  await expect(
    settings.getByRole("link", { name: "模型目录", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "用户", exact: true }),
  ).toBeVisible();
  const appearance = page.getByRole("button", { name: "外观", exact: true });
  await appearance.focus();
  await appearance.press("ArrowDown");
  await expect(
    page.getByRole("menuitemradio", { name: "浅色", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(
    page.getByRole("menuitemradio", { name: "深色", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(appearance).toBeFocused();
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await appearance.click();
  await page
    .getByRole("menuitemradio", { name: "跟随系统", exact: true })
    .click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(errors).toEqual([]);
  expect(apiRequests).toEqual([]);
});
