const { test, expect } = require("@playwright/test");

// This entry exercises the real Vue page against synthetic HTTP responses.
// It never consumes a private runtime, authenticates, or contacts a backend.
test.use({
  launchOptions: {
    executablePath: "/usr/bin/chromium",
    args: ["--no-sandbox"],
  },
  serviceWorkers: "block",
});
const { controlled, id } = require("./controlled-system.cjs");

async function secondPage(page) {
  await page.getByRole("combobox", { name: "每页条数", exact: true }).click();
  await page.getByRole("option", { name: "20 条", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "第 1 页" }),
  ).toContainText("20 条");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "第 2 页" }),
  ).toContainText("20 条");
}
const detailDialog = (page) =>
  page.getByRole("dialog", { name: "投递任务详情", exact: true });
const recipient = (page) => page.getByLabel("测试收件邮箱", { exact: true });
async function retained(page, state, reads, draft) {
  await expect(detailDialog(page)).not.toBeVisible();
  await expect(
    page.getByRole("tab", { name: "测试与任务", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(
    page.getByRole("status").filter({ hasText: "第 2 页" }),
  ).toContainText("20 条");
  await expect(
    page.getByRole("combobox", { name: "每页条数", exact: true }),
  ).toContainText("20 条");
  await expect(recipient(page)).toHaveValue(draft);
  expect(state.lists.length).toBe(reads);
  expect(state.configs).toBe(1);
  expect(state.unexpected).toEqual([]);
}
function acceptedListReads(state, before) {
  // A confirmed command may refresh its current page once, without loading
  // the tab's initial page or changing the user's cursor and page size.
  expect(state.lists.length).toBeGreaterThanOrEqual(before);
  expect(state.lists.length).toBeLessThanOrEqual(before + 1);
  for (const read of state.lists.slice(before))
    expect(read).toEqual({ limit: 20, cursor: "synthetic-offset-20" });
  return state.lists.length;
}

test("SMTP detail closes back to the same list page, draft, focus and scroll without reloading the tab", async ({
  page,
}) => {
  const state = await controlled(page);
  const draft = "unsent@example.invalid";
  await recipient(page).fill(draft);
  await secondPage(page);
  const opener = page.getByRole("button", {
    name: `查看测试任务 ${id(39)}`,
    exact: true,
  });
  await opener.scrollIntoViewIfNeeded();
  await opener.focus();
  const listNode = await page
    .locator('section[aria-labelledby="smtp-job-list"]')
    .elementHandle();
  const triggerNode = await opener.elementHandle();
  const scroll = await page
    .locator(".settings-main")
    .evaluate((node) => node.scrollTop);
  expect(scroll).toBeGreaterThan(100);
  const reads = state.lists.length;
  await opener.press("Enter");
  await expect(detailDialog(page)).toBeVisible();
  await expect(
    page.locator('section[aria-labelledby="smtp-job-list"] li'),
  ).toHaveCount(20);
  await expect(detailDialog(page)).toContainText(id(39));
  expect(await listNode.evaluate((node) => node.isConnected)).toBe(true);
  expect(await triggerNode.evaluate((node) => node.isConnected)).toBe(true);
  await page.keyboard.press("Escape");
  await retained(page, state, reads, draft);
  await expect(opener).toBeFocused();
  expect(
    Math.abs(
      (await page
        .locator(".settings-main")
        .evaluate((node) => node.scrollTop)) - scroll,
    ),
  ).toBeLessThanOrEqual(2);
  expect(state.details.length).toBe(1);
  expect(state.tests.length + state.retries.length).toBe(0);
});

test("a single accepted SMTP test opens a narrow-screen modal and recovers only the failed detail read", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  const state = await controlled(page, { failDetailAfterTest: true });
  await secondPage(page);
  const draft = "delivery@example.invalid";
  await recipient(page).fill(draft);
  const reads = state.lists.length;
  await page.getByRole("button", { name: "请求测试发送", exact: true }).click();
  const modal = detailDialog(page);
  await expect(modal).toBeVisible();
  await expect(modal.getByRole("alert")).toContainText("任务读取失败");
  expect(state.tests.length).toBe(1);
  await modal
    .getByRole("button", { name: "重新读取任务", exact: true })
    .click();
  await expect(modal).toContainText(state.accepted.job_id);
  await expect(modal.getByRole("alert")).toHaveCount(0);
  expect(state.tests.length).toBe(1);
  expect(state.details.length).toBe(2);
  expect(
    await modal.evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBe(true);
  const readsAfterReceipt = acceptedListReads(state, reads);
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  await retained(page, state, readsAfterReceipt, draft);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("closing an uncertain SMTP retry preserves its original intent until explicit recovery", async ({
  page,
}) => {
  const state = await controlled(page, { failFirstRetry: true });
  await secondPage(page);
  const draft = "another-unsent@example.invalid";
  await recipient(page).fill(draft);
  const reads = state.lists.length;
  await page
    .getByRole("button", { name: `查看测试任务 ${id(20)}`, exact: true })
    .click();
  const modal = detailDialog(page);
  await expect(modal).toBeVisible();
  await modal
    .getByRole("button", { name: "申请重试投递", exact: true })
    .click();
  const confirmation = page.getByRole("dialog", {
    name: "申请新的投递重试",
    exact: true,
  });
  await confirmation
    .getByRole("button", { name: "申请重试", exact: true })
    .click();
  await expect(modal.getByRole("alert")).toContainText("原请求结果尚未确认");
  await expect(
    modal.getByRole("button", { name: "申请重试投递", exact: true }),
  ).toBeDisabled();
  expect(state.retries.length).toBe(1);
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  const leave = page.getByRole("dialog", {
    name: "关闭并放弃追踪",
    exact: true,
  });
  await expect(leave).toBeVisible();
  await leave.getByRole("button", { name: "继续追踪", exact: true }).click();
  await expect(modal).toBeVisible();
  expect(state.retries.length).toBe(1);
  await modal
    .getByRole("button", { name: "检查当前状态", exact: true })
    .click();
  await expect(
    modal.getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  await modal.getByRole("button", { name: "重试原请求", exact: true }).click();
  await expect(modal).toContainText(state.retried.job_id);
  await expect(modal.getByRole("alert")).toHaveCount(0);
  expect(state.retries.length).toBe(2);
  expect(
    state.retries[0].path === state.retries[1].path &&
      state.retries[0].method === state.retries[1].method &&
      state.retries[0].key === state.retries[1].key &&
      state.retries[0].body === state.retries[1].body,
  ).toBe(true);
  expect(JSON.parse(state.retries[0].body).version).toBe("2");
  const readsAfterReceipt = acceptedListReads(state, reads);
  await modal.getByRole("button", { name: "关闭详情", exact: true }).click();
  await retained(page, state, readsAfterReceipt, draft);
});
