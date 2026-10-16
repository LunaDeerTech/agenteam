const { test, expect } = require("@playwright/test");

// This entry exercises the real Vue page against synthetic HTTP responses.
// It never consumes a private runtime, authenticates, or contacts a backend.
const origin = "http://127.0.0.1:5245";
test.use({
  launchOptions: {
    executablePath: "/usr/bin/chromium",
    args: ["--no-sandbox"],
  },
  serviceWorkers: "block",
});
const id = (index) => `019a2222-2222-7222-8222-${String(999999999999 - index)}`;
const instant = "2026-10-16T07:00:00.000000Z";
const user = {
  id: id(9000),
  email: "synthetic-admin@example.invalid",
  username: "synthetic-admin",
  display_name: "Synthetic administrator",
  role: "admin",
  theme: "system",
  version: "1",
  initial_password_suggestion: false,
};
const session = {
  user,
  session: {
    id: id(9001),
    issued_at: instant,
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-17T08:00:00.000000Z",
  },
  csrf_token: "synthetic-modal-context",
};
const smtp = {
  id: id(9002),
  version: "1",
  configured: true,
  host: "smtp.synthetic.example.invalid",
  port: 2525,
  encryption: "starttls",
  username: "",
  sender_email: "sender@example.invalid",
  sender_name: "Synthetic mail",
  credential_present: false,
  auto_retry_count: "0",
  retry_interval_seconds: "60",
};
const job = (index) => ({
  job_id: id(index),
  kind: "test",
  phase: "failed",
  attempts: "1",
  version: "2",
  channel: "smtp",
  created_at: instant,
  attempt_channel: "smtp",
  attempt_result: "failed",
  reason: "network_failed",
});

async function controlled(page, options = {}) {
  const jobs = Array.from({ length: 60 }, (_, index) => job(index));
  const state = {
    lists: [],
    configs: 0,
    details: [],
    tests: [],
    retries: [],
    unexpected: [],
    accepted: job(1000),
    retried: job(1001),
    failDetail: false,
  };
  await page.context().route("**/*", async (route) => {
    if (new URL(route.request().url()).origin === origin)
      await route.continue();
    else {
      state.unexpected.push("outside-origin");
      await route.abort("blockedbyclient");
    }
  });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    const respond = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    const intent = () => ({
      path,
      method,
      body: request.postData(),
      key: request.headers()["idempotency-key"],
    });
    if (method === "GET" && path === "/api/v1/session") return respond(session);
    if (method === "GET" && path === "/api/v1/me")
      return respond({ user, avatar: null });
    if (method === "GET" && path === "/api/v1/system/smtp") {
      state.configs++;
      return respond(smtp);
    }
    if (method === "GET" && path === "/api/v1/system/mail-jobs/management") {
      const limit = Number(url.searchParams.get("limit"));
      const cursor = url.searchParams.get("cursor");
      const offset = cursor
        ? Number(cursor.replace("synthetic-offset-", ""))
        : 0;
      state.lists.push({ limit, cursor });
      const items = jobs.slice(offset, offset + limit);
      return respond({
        items,
        ...(offset + limit < jobs.length
          ? { next_cursor: `synthetic-offset-${offset + limit}` }
          : {}),
      });
    }
    if (method === "POST" && path === "/api/v1/system/smtp/test") {
      state.tests.push(intent());
      state.failDetail = !!options.failDetailAfterTest;
      return respond({ job_id: state.accepted.job_id }, 202);
    }
    if (
      method === "POST" &&
      /^\/api\/v1\/system\/mail-jobs\/[^/]+\/retry$/.test(path)
    ) {
      state.retries.push(intent());
      if (options.failFirstRetry && state.retries.length === 1)
        return route.abort("connectionfailed");
      return respond({ job_id: state.retried.job_id, version: "1" }, 202);
    }
    if (
      method === "GET" &&
      /^\/api\/v1\/system\/mail-jobs\/[^/]+\/management$/.test(path)
    ) {
      state.details.push(path);
      if (state.failDetail) {
        state.failDetail = false;
        return route.abort("connectionfailed");
      }
      const found = [...jobs, state.accepted, state.retried].find(
        (value) =>
          path === `/api/v1/system/mail-jobs/${value.job_id}/management`,
      );
      if (found) return respond(found);
    }
    state.unexpected.push(`${method} ${path}`);
    await route.abort("blockedbyclient");
  });
  await page.goto(`${origin}/system/smtp`);
  await expect(page.getByLabel("端口", { exact: true })).toHaveValue("2525");
  await page.getByRole("tab", { name: "测试与任务", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "投递任务", exact: true }),
  ).toBeVisible();
  return state;
}

async function secondPage(page) {
  await page.getByRole("combobox", { name: "每页条数", exact: true }).click();
  await page.getByRole("option", { name: "20 条", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "第 1 页" }),
  ).toContainText("20 项");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "第 2 页" }),
  ).toContainText("20 项");
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
  ).toContainText("20 项");
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
