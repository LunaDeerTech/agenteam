const { test, expect, loginSystem } = require("./system-owned.cjs");
const { api } = require("./journey-support.cjs");

function safeRoute(handler) {
  return async (route) => {
    try {
      await handler(route);
    } catch {
      await route.abort("failed").catch(() => {});
      // Playwright's raw route.fetch failure includes private request headers.
      throw new Error("OWNED_SYSTEM_ROUTE_FORWARDING_FAILED");
    }
  };
}

function securityInput(value, changes = {}) {
  return {
    version: value.version,
    session_idle_seconds: value.session_idle_seconds,
    session_absolute_seconds: value.session_absolute_seconds,
    password_reset_seconds: value.password_reset_seconds,
    challenge_after_failures: value.challenge_after_failures,
    ...changes,
  };
}

test("real account-security save preserves conflict drafts and replays only its original uncertain write", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  if (process.env.NODE_EXTRA_CA_CERTS !== owned.certificate_ca_file)
    throw new Error("OWNED_SYSTEM_NODE_CA_REQUIRED");
  await loginSystem(page, owned, "/system/account-security");
  const challenge = page.getByLabel("登录失败挑战阈值（次）", { exact: true });
  const reset = page.getByLabel("密码重置链接期限（秒）", { exact: true });
  const save = page.getByRole("button", { name: "保存配置", exact: true });
  await expect(challenge).toBeEnabled();
  const initial = await api(page, "/system/account-settings");
  expect(initial.status).toBe(200);
  await expect(challenge).toHaveValue(initial.value.challenge_after_failures);
  const nextThreshold =
    initial.value.challenge_after_failures === "5" ? "6" : "5";
  await challenge.fill(nextThreshold);
  await save.click();
  await expect(
    page.getByRole("status").filter({ hasText: "账号安全配置已保存。" }),
  ).toBeVisible();
  await expect(challenge).toBeEnabled();
  const saved = await api(page, "/system/account-settings");
  expect(saved.value.version).toBe(String(BigInt(initial.value.version) + 1n));
  expect(saved.value.challenge_after_failures).toBe(nextThreshold);

  // An independently accepted admin write advances the singleton. The stale
  // UI must keep its own values until the user explicitly adopts the new view.
  const draftReset =
    saved.value.password_reset_seconds === "900" ? "1200" : "900";
  const remoteReset = draftReset === "1200" ? "1500" : "1800";
  await reset.fill(draftReset);
  const remote = await api(
    page,
    "/system/account-settings",
    "PUT",
    securityInput(saved.value, { password_reset_seconds: remoteReset }),
  );
  expect(remote.status).toBe(200);
  await save.click();
  await expect(page.getByRole("alert")).toContainText("你的草稿已保留");
  await expect(reset).toHaveValue(draftReset);
  await page.getByRole("button", { name: "读取最新配置", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "采用最新配置", exact: true }),
  ).toBeEnabled();
  await expect(reset).toHaveValue(draftReset);
  await page.getByRole("button", { name: "采用最新配置", exact: true }).click();
  await page
    .getByRole("dialog", { name: "放弃当前修改", exact: true })
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(reset).toHaveValue(remoteReset);

  // Forward a genuine accepted write and drop only its response. This proves
  // committed persistence separately from what the UI can know at that point.
  const attempts = [];
  let committedVersion;
  let loseNextRead = false;
  await page.route(
    "**/api/v1/system/account-settings",
    safeRoute(async (route) => {
      if (route.request().method() === "PUT") {
        attempts.push({
          key: route.request().headers()["idempotency-key"],
          body: route.request().postData(),
        });
        const response = await route.fetch();
        if (response.status() !== 200) throw new Error();
        const value = await response.json();
        if (attempts.length === 1) {
          committedVersion = value.version;
          await route.abort("connectionfailed");
        } else {
          loseNextRead = true;
          await route.fulfill({ response });
        }
      } else if (loseNextRead && route.request().method() === "GET") {
        loseNextRead = false;
        await route.abort("connectionfailed");
      } else await route.continue();
    }),
  );
  const recoveredReset = remoteReset === "1500" ? "1800" : "2100";
  await reset.fill(recoveredReset);
  await save.click();
  await expect(page.getByRole("alert")).toContainText("原保存结果尚未确认");
  await expect(reset).toBeDisabled();
  expect(attempts).toHaveLength(1);
  await page.getByRole("button", { name: "检查当前状态", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  expect(attempts).toHaveLength(1);
  await page.getByRole("button", { name: "重试原请求", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "保存已确认，当前配置读取失败",
  );
  expect(attempts).toHaveLength(2);
  expect(
    attempts[0].key === attempts[1].key &&
      attempts[0].body === attempts[1].body,
  ).toBe(true);
  await page.getByRole("button", { name: "重新读取配置", exact: true }).click();
  await expect(reset).toBeEnabled();
  await expect(reset).toHaveValue(recoveredReset);
  expect(attempts).toHaveLength(2);
  await page.unroute("**/api/v1/system/account-settings");
  const final = await api(page, "/system/account-settings");
  expect(final.value.version).toBe(committedVersion);
  expect(final.value.password_reset_seconds).toBe(recoveredReset);
});
