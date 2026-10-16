// Finite synthetic responses for the three real Vue system-list pages.
// No runtime, private credentials, or backend passthrough.
const { expect } = require("@playwright/test");
const origin = "http://127.0.0.1:5245";

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
    empty: !!options.empty,
    directories: [],
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
    if (url.origin !== origin) {
      state.unexpected.push("outside-origin-api");
      return route.abort("blockedbyclient");
    }
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
    if (
      method === "GET" &&
      ["/api/v1/system/users", "/api/v1/system/invitations"].includes(path)
    ) {
      const limit = Number(url.searchParams.get("limit"));
      const cursor = url.searchParams.get("cursor");
      const offset = cursor
        ? Number(cursor.replace("synthetic-offset-", ""))
        : 0;
      state.directories.push({ path, limit, cursor });
      const all = state.empty
        ? []
        : Array.from({ length: 60 }, (_, index) =>
            path.endsWith("/users")
              ? {
                  ...user,
                  id: id(index),
                  email: `synthetic-directory-user-${index}@example.invalid`,
                  display_name: `Synthetic directory user ${index}`,
                  username: `synthetic-user-${index}`,
                  created_at: instant,
                }
              : {
                  id: id(index),
                  email: `synthetic-invitation-${index}@example.invalid`,
                  version: "1",
                  created_at: instant,
                  expires_at: "2026-10-17T07:00:00.000000Z",
                  latest_delivery: {
                    job_id: id(index + 2000),
                    accepted_at: instant,
                    phase: "failed",
                    attempts: "1",
                    version: "2",
                    channel: "smtp",
                    attempt_result: "failed",
                    reason: "network_failed",
                  },
                },
          );
      return respond({
        items: all.slice(offset, offset + limit),
        ...(offset + limit < all.length
          ? { next_cursor: `synthetic-offset-${offset + limit}` }
          : {}),
      });
    }
    if (method === "GET" && path === "/api/v1/system/mail-jobs/management") {
      const limit = Number(url.searchParams.get("limit"));
      const cursor = url.searchParams.get("cursor");
      const offset = cursor
        ? Number(cursor.replace("synthetic-offset-", ""))
        : 0;
      state.lists.push({ limit, cursor });
      const available = state.empty ? [] : jobs;
      const items = available.slice(offset, offset + limit);
      return respond({
        items,
        ...(offset + limit < available.length
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
  const path = options.path || "/system/smtp";
  if (!["/system/users", "/system/invitations", "/system/smtp"].includes(path))
    throw new Error("CONTROLLED_PATH_REJECTED");
  await page.goto(`${origin}${path}`);
  if (path === "/system/smtp") {
    await expect(page.getByLabel("端口", { exact: true })).toHaveValue("2525");
    await page.getByRole("tab", { name: "测试与任务", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "投递任务", exact: true }),
    ).toBeVisible();
  }
  await expect(
    page.getByRole("status").filter({ hasText: "第 1 页" }),
  ).toBeVisible();
  return state;
}

module.exports = { controlled, id };
