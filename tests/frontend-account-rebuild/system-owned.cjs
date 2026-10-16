const fs = require("node:fs");
const path = require("node:path");
const {
  test: accountTest,
  expect,
  fillPrivate,
} = require("./owned-fixture.cjs");

// System settings are global. A valid account-preview manifest alone is not
// authority to modify them; require the backend owner's isolated-task sidecar.
const test = accountTest.extend({
  owned: [
    async ({ owned }, use) => {
      const runtimeFile = process.env.FRONTEND_ACCOUNT_RUNTIME;
      try {
        const filename = path.join(
          path.dirname(runtimeFile),
          "system-owned.json",
        );
        const stat = fs.lstatSync(filename);
        const scope = JSON.parse(fs.readFileSync(filename, "utf8"));
        if (
          owned.origin !== "https://localhost:5250" ||
          !stat.isFile() ||
          stat.isSymbolicLink() ||
          (stat.mode & 0o777) !== 0o600 ||
          stat.uid !== process.getuid() ||
          scope.test_scope !== "frontend-system-account-settings" ||
          scope.runtime_file !== runtimeFile ||
          scope.nonce !== owned.nonce ||
          !Number.isFinite(Date.parse(owned.expires_at)) ||
          Date.parse(owned.expires_at) <= Date.now()
        )
          throw new Error();
      } catch {
        throw new Error("ISOLATED_SYSTEM_ACCOUNT_SCOPE_REQUIRED");
      }
      await use(owned);
    },
    { scope: "worker" },
  ],
  page: async ({ page }, use) => {
    try {
      await use(page);
    } finally {
      await closeSystemSession(page);
    }
  },
});

async function closeSystemSession(page) {
  try {
    await page.evaluate(async () => {
      const current = await fetch("/api/v1/session", { cache: "no-store" });
      if (current.status === 401) return;
      if (current.status !== 200) throw new Error();
      const session = await current.json();
      const response = await fetch("/api/v1/sessions/logout", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: "{}",
      });
      await response.arrayBuffer();
      if (response.status !== 204) throw new Error();
      const after = await fetch("/api/v1/session", { cache: "no-store" });
      await after.arrayBuffer();
      if (after.status !== 401) throw new Error();
    });
  } catch {
    throw new Error("OWNED_SYSTEM_SESSION_CLEANUP_FAILED");
  }
}

async function loginSystem(page, owned, destination = "/system/users") {
  await page.goto(destination);
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await fillPrivate(
    page.getByLabel("邮箱", { exact: true }),
    owned.credentials.email,
  );
  await fillPrivate(
    page.getByLabel("密码", { exact: true }),
    owned.credentials.password,
  );
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(new URL(destination, owned.origin).href);
}

module.exports = { test, expect, loginSystem, closeSystemSession };
