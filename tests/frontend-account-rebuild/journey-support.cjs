const { randomBytes } = require("node:crypto");
const { expect, fillPrivate } = require("./owned-fixture.cjs");
const { readLastMail, accountLinkFromMail } = require("./mail-reader.cjs");

async function mailLink(owned, recipient, purpose) {
  let link;
  await expect
    .poll(
      async () => {
        link = accountLinkFromMail(await readLastMail(owned), {
          origin: owned.origin,
          recipient,
          purpose,
        });
        return !!link;
      },
      {
        timeout: 20000,
        message: "owned SMTP accepted a matching account message",
      },
    )
    .toBe(true);
  return link;
}

async function login(page, email, password) {
  await page.goto("/settings/profile");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await fillPrivate(page.getByLabel("邮箱", { exact: true }), email);
  await fillPrivate(page.getByLabel("密码", { exact: true }), password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "个人资料", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("显示名称", { exact: true })).toBeEnabled();
}

async function api(page, path, method = "GET", body) {
  try {
    return await page.evaluate(
      async ({ path, method, body }) => {
        const session = await fetch("/api/v1/session", { cache: "no-store" });
        const current = await session.json();
        const response = await fetch(`/api/v1${path}`, {
          method,
          cache: "no-store",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": current.csrf_token,
            ...(method === "GET"
              ? {}
              : { "Idempotency-Key": crypto.randomUUID() }),
          },
          body: body === undefined ? undefined : JSON.stringify(body),
        });
        return {
          status: response.status,
          value: response.status === 204 ? undefined : await response.json(),
        };
      },
      { path, method, body },
    );
  } catch {
    throw new Error("OWNED_API_INTERACTION_FAILED");
  }
}

async function newOwnedUser(page, owned) {
  if (process.env.FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE !== "1")
    throw new Error("COORDINATE_OWNED_SMTP_MAILBOX_BEFORE_RUNNING");
  const suffix = randomBytes(6).toString("hex");
  const email = `vue-avatar-${suffix}@example.test`;
  const password = `Aa7!${randomBytes(24).toString("base64url")}`;
  const statuses = await page.evaluate(
    async ({ credentials, email }) => {
      const bootstrap = await (await fetch("/api/v1/auth/bootstrap")).json();
      const post = async (path, body, csrf) =>
        fetch(`/api/v1${path}`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": csrf,
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: JSON.stringify(body),
        });
      const login = await post(
        "/sessions/login",
        credentials,
        bootstrap.csrf_token,
      );
      await login.arrayBuffer();
      const session = await (await fetch("/api/v1/session")).json();
      const invite = await post(
        "/system/invitations",
        { email },
        session.csrf_token,
      );
      await invite.arrayBuffer();
      const logout = await post("/sessions/logout", {}, session.csrf_token);
      await logout.arrayBuffer();
      return [login.status, invite.status, logout.status];
    },
    { credentials: owned.credentials, email },
  );
  expect(statuses).toEqual([200, 201, 204]);
  const link = await mailLink(owned, email, "invite");
  const status = await page.evaluate(
    async ({ token, username, password }) => {
      const bootstrap = await (await fetch("/api/v1/auth/bootstrap")).json();
      const response = await fetch("/api/v1/invitations/redeem", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": bootstrap.csrf_token,
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: JSON.stringify({
          token,
          username,
          password,
          confirmation: password,
        }),
      });
      await response.arrayBuffer();
      return response.status;
    },
    {
      token: new URL(link).hash.slice(1),
      username: `avatar-${suffix}`,
      password,
    },
  );
  expect(status).toBe(201);
  return { email, password };
}

module.exports = { mailLink, login, api, newOwnedUser };
