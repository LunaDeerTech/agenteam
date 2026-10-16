const { randomBytes } = require("node:crypto");
const {
  test,
  expect,
  fillPrivate,
  openPrivateLink,
  sessionStatus,
} = require("./owned-fixture.cjs");
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

test("real SMTP invitation, personal settings, password replacement and SMTP reset form one account journey", async ({
  page,
  owned,
  browser,
}) => {
  test.setTimeout(120000);
  if (process.env.FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE !== "1")
    throw new Error("COORDINATE_OWNED_SMTP_MAILBOX_BEFORE_RUNNING");
  const suffix = randomBytes(6).toString("hex");
  const email = `vue-author-${suffix}@example.test`;
  const password = `Aa7!${randomBytes(24).toString("base64url")}`;
  const replacement = `Bb8!${randomBytes(24).toString("base64url")}`;
  const reset = `Cc9!${randomBytes(24).toString("base64url")}`;
  let observer;
  try {
    await test.step("create an owned invitation and redeem the actual accepted SMTP message in Vue", async () => {
      const creation = await page.evaluate(
        async ({ credentials, email }) => {
          async function request(path, body, csrf) {
            const response = await fetch(`/api/v1${path}`, {
              method: body === undefined ? "GET" : "POST",
              cache: "no-store",
              headers: {
                "Content-Type": "application/json",
                ...(csrf ? { "X-CSRF-Token": csrf } : {}),
                ...(body === undefined
                  ? {}
                  : { "Idempotency-Key": crypto.randomUUID() }),
              },
              body: body === undefined ? undefined : JSON.stringify(body),
            });
            return {
              status: response.status,
              value:
                response.status === 204 ? undefined : await response.json(),
            };
          }
          const bootstrap = await request("/auth/bootstrap");
          const login = await request(
            "/sessions/login",
            credentials,
            bootstrap.value.csrf_token,
          );
          if (login.status !== 200) return { login: login.status };
          const session = await request("/session");
          const invitation = await request(
            "/system/invitations",
            { email },
            session.value.csrf_token,
          );
          const logout = await request(
            "/sessions/logout",
            {},
            session.value.csrf_token,
          );
          return {
            login: login.status,
            invitation: invitation.status,
            logout: logout.status,
          };
        },
        { credentials: owned.credentials, email },
      );
      expect(creation).toEqual({ login: 200, invitation: 201, logout: 204 });
      await openPrivateLink(page, await mailLink(owned, email, "invite"));
      await expect(page.getByLabel("用户名", { exact: true })).toBeEnabled();
      expect(
        await page.evaluate(
          () => location.hash === "" && location.search === "",
        ),
      ).toBe(true);
      await page.getByLabel("用户名", { exact: true }).fill(`author-${suffix}`);
      await page.getByLabel("显示名", { exact: true }).fill("作者真实账号");
      await fillPrivate(page.getByLabel("密码", { exact: true }), password);
      await fillPrivate(page.getByLabel("确认密码", { exact: true }), password);
      await page.getByRole("button", { name: "接受邀请", exact: true }).click();
      await expect(page.getByRole("status")).toContainText(
        "账号已创建，请登录后继续。",
      );
      expect(await sessionStatus(page)).toBe(401);
      expect(await page.locator('input[type="password"]').count()).toBe(0);
      await login(page, email, password);
    });

    await test.step("save profile and preserve a conflicting draft until the user adopts the latest version", async () => {
      await page.getByLabel("显示名称", { exact: true }).fill("真实资料已保存");
      await page.getByRole("button", { name: "保存资料", exact: true }).click();
      await expect(page.getByRole("status")).toContainText("个人资料已保存。");
      const persisted = await api(page, "/me");
      expect(persisted.status).toBe(200);
      expect(persisted.value.user.display_name).toBe("真实资料已保存");
      await page
        .getByLabel("显示名称", { exact: true })
        .fill("应保留的冲突草稿");
      expect(
        (
          await api(page, "/me", "PATCH", {
            version: persisted.value.user.version,
            display_name: "并发已保存版本",
          })
        ).status,
      ).toBe(200);
      await page.getByRole("button", { name: "保存资料", exact: true }).click();
      await expect(
        page.getByRole("button", { name: "读取最新资料", exact: true }),
      ).toBeVisible();
      await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue(
        "应保留的冲突草稿",
      );
      await page
        .getByRole("button", { name: "读取最新资料", exact: true })
        .click();
      await page
        .getByRole("button", { name: "基于最新资料继续编辑", exact: true })
        .click();
      await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue(
        "应保留的冲突草稿",
      );
      await page.getByRole("button", { name: "保存资料", exact: true }).click();
      await expect(page.getByRole("status")).toContainText("个人资料已保存。");
      await page.reload();
      await expect(page.getByLabel("显示名称", { exact: true })).toHaveValue(
        "应保留的冲突草稿",
      );
    });

    await test.step("preview and cancel appearance, then persist to the server and survive reload", async () => {
      await page.goto("/settings/appearance");
      await expect(page.getByRole("radio", { name: /深色/ })).toBeEnabled();
      const initial = await page.locator("html").getAttribute("data-theme");
      await page.getByRole("radio", { name: /深色/ }).check();
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
      await page.getByRole("button", { name: "取消", exact: true }).click();
      await expect(page.locator("html")).toHaveAttribute("data-theme", initial);
      await page.getByRole("radio", { name: /深色/ }).check();
      await page.getByRole("button", { name: "保存偏好", exact: true }).click();
      await expect(page.getByRole("status")).toContainText("外观偏好已保存。");
      const preference = await api(page, "/me/preferences");
      expect(preference.status).toBe(200);
      expect(preference.value.theme).toBe("dark");
      await page.reload();
      await expect(page.getByRole("radio", { name: /深色/ })).toBeChecked();
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    });

    await test.step("replace the password through Vue and reject another old session", async () => {
      observer = await browser.newContext({ baseURL: owned.origin });
      const second = await observer.newPage();
      await login(second, email, password);
      await page.goto("/settings/password");
      await fillPrivate(page.getByLabel("当前密码", { exact: true }), password);
      await fillPrivate(
        page.getByLabel("新密码", { exact: true }),
        replacement,
      );
      await fillPrivate(
        page.getByLabel("确认新密码", { exact: true }),
        replacement,
      );
      await page.getByRole("button", { name: "修改密码", exact: true }).click();
      await expect(page.getByRole("status")).toContainText(
        "本次登录状态已确认，可以继续使用。",
      );
      expect(await page.locator('input[type="password"]').count()).toBe(0);
      expect(await sessionStatus(page)).toBe(200);
      expect(await sessionStatus(second)).toBe(401);
      await observer.close();
      observer = undefined;
    });

    await test.step("request reset through Vue, consume actual SMTP token and invalidate the target account's current session", async () => {
      await page.goto("/forgot-password");
      await fillPrivate(page.getByLabel("邮箱", { exact: true }), email);
      await page
        .getByRole("button", { name: "发送重置请求", exact: true })
        .click();
      await expect(page.getByRole("status")).toContainText(
        "若该邮箱可用，将通过邮件提供恢复方式，请查看邮箱。",
      );
      await openPrivateLink(
        page,
        await mailLink(owned, email, "reset-password"),
      );
      await expect(page.getByLabel("新密码", { exact: true })).toBeEnabled();
      expect(
        await page.evaluate(
          () => location.hash === "" && location.search === "",
        ),
      ).toBe(true);
      await fillPrivate(page.getByLabel("新密码", { exact: true }), reset);
      await fillPrivate(page.getByLabel("确认密码", { exact: true }), reset);
      await page.getByRole("button", { name: "重置密码", exact: true }).click();
      await expect(page.getByRole("status")).toContainText("密码已重置");
      expect(await sessionStatus(page)).toBe(401);
      expect(await page.locator('input[type="password"]').count()).toBe(0);
      await login(page, email, reset);
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    });
  } finally {
    await observer?.close();
  }
});
