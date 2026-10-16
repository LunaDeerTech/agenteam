const { randomBytes } = require("node:crypto");
const { test, expect, closeSystemSession } = require("./system-owned.cjs");
const { api, mailLink } = require("./journey-support.cjs");

async function browserPost(page, path, body) {
  try {
    return await page.evaluate(
      async ({ path, body }) => {
        const bootstrap = await (await fetch("/api/v1/auth/bootstrap")).json();
        const response = await fetch(`/api/v1${path}`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": bootstrap.csrf_token,
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: JSON.stringify(body),
        });
        await response.arrayBuffer();
        return response.status;
      },
      { path, body },
    );
  } catch {
    throw new Error("OWNED_SYSTEM_PERMISSION_BROWSER_POST_FAILED");
  }
}

test("real ordinary seeded user receives system 403 while its personal session remains authenticated", async ({
  page,
  owned,
}) => {
  test.setTimeout(45000);
  if (process.env.FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE !== "1")
    throw new Error("COORDINATE_OWNED_SMTP_MAILBOX_BEFORE_RUNNING");
  // Stay on the fixture identity document: this HTTP permission probe does
  // not consume Vue or depend on unrelated page hot reloads.
  expect(await browserPost(page, "/sessions/login", owned.credentials)).toBe(
    200,
  );
  const directory = await api(page, "/system/users?limit=100");
  expect(directory.status).toBe(200);
  // The scope-gated runtime started with only its bootstrap administrator.
  // These exact addresses were created by system-users' owned API seed.
  const user = directory.value.items.find(
    (item) =>
      item.role === "user" &&
      /^vue-avatar-[a-f0-9]{12}@example\.test$/.test(item.email),
  );
  if (!user) throw new Error("OWNED_SYSTEM_SEEDED_USER_REQUIRED");
  await closeSystemSession(page);
  const requested = await browserPost(page, "/password-resets/request", {
    email: user.email,
  });
  expect(requested).toBe(202);
  const link = await mailLink(owned, user.email, "reset-password");
  const password = `Owned7!${randomBytes(24).toString("base64url")}`;
  const completed = await browserPost(page, "/password-resets/complete", {
    token: new URL(link).hash.slice(1),
    new_password: password,
    confirmation: password,
  });
  expect(completed).toBe(204);
  expect(
    await browserPost(page, "/sessions/login", { email: user.email, password }),
  ).toBe(200);
  const denied = await api(page, "/system/users?limit=10");
  expect(denied.status).toBe(403);
  const current = await api(page, "/session");
  expect(current.status).toBe(200);
  expect(
    current.value.user.id === user.id && current.value.user.role === "user",
  ).toBe(true);
});
