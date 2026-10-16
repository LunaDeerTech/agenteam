const {
  test,
  expect,
  sessionStatus,
  fillPrivate,
} = require("./owned-fixture.cjs");

test("real login restores a protected destination and logout revokes the current session", async ({
  page,
  context,
  owned,
}) => {
  await page.goto("/settings/profile");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  expect(await sessionStatus(page)).toBe(401);
  await fillPrivate(
    page.getByLabel("邮箱", { exact: true }),
    owned.credentials.email,
  );
  await fillPrivate(
    page.getByLabel("密码", { exact: true }),
    owned.credentials.password,
  );
  const loginResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/sessions/login") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "登录", exact: true }).click();
  expect((await loginResponse).status()).toBe(200);
  await expect(page).toHaveURL(/\/settings\/profile$/);
  await expect(
    page.getByRole("heading", { name: "个人资料", exact: true }),
  ).toBeVisible();
  expect(await sessionStatus(page)).toBe(200);
  const cookies = await context.cookies(owned.origin);
  for (const name of ["__Host-agenteam_browser", "__Host-agenteam_session"]) {
    const cookie = cookies.find((item) => item.name === name);
    expect(Boolean(cookie), "expected secure cookie is present").toBe(true);
    // Assert properties individually so a failing assertion never prints its value.
    expect(cookie.secure).toBe(true);
    expect(cookie.httpOnly).toBe(true);
    expect(cookie.sameSite).toBe("Lax");
    expect(cookie.path).toBe("/");
    expect(cookie.domain).toBe("localhost");
  }
  expect(
    cookies.some((cookie) => cookie.name.startsWith("agenteam_local_")),
  ).toBe(false);
  expect(
    await page.evaluate(() => document.cookie.includes("agenteam_session")),
  ).toBe(false);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "个人资料", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "账号", exact: true }).click();
  await page.getByRole("menuitem", { name: "退出登录", exact: true }).click();
  const confirmation = page.getByRole("dialog", {
    name: "退出登录",
    exact: true,
  });
  await expect(confirmation).toBeVisible();
  const logoutResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/sessions/logout") &&
      response.request().method() === "POST",
  );
  await confirmation
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  expect((await logoutResponse).status()).toBe(204);
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(await sessionStatus(page)).toBe(401);
  expect(
    (await context.cookies(owned.origin)).some(
      (cookie) => cookie.name === "__Host-agenteam_session",
    ),
  ).toBe(false);
});
