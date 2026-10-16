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

module.exports = { mailLink, login, api };
