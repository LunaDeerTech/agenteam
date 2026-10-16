const { randomBytes } = require("node:crypto");
const {
  test,
  expect,
  loginSystem,
  closeSystemSession,
} = require("./system-owned.cjs");
const { fillPrivate, openPrivateLink } = require("./owned-fixture.cjs");
const { api, mailLink } = require("./journey-support.cjs");

async function invitation(page, email) {
  const result = await api(page, "/system/invitations?limit=10");
  expect(result.status).toBe(200);
  return result.value.items.find((item) => item.email === email);
}
async function create(page, email) {
  await page.getByRole("button", { name: "邀请用户", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "邀请用户", exact: true });
  await dialog.getByLabel("邮箱", { exact: true }).fill(email);
  await dialog.getByRole("button", { name: "发送邀请", exact: true }).click();
  await expect(dialog.getByRole("status")).toContainText("请求已受理");
  await expect(dialog.getByLabel("邮箱", { exact: true })).toHaveValue("");
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await expect(page.getByText(email, { exact: true })).toBeVisible();
  return invitation(page, email);
}
async function action(page, email, label) {
  await page
    .getByRole("button", { name: `${email} 的邀请操作`, exact: true })
    .click();
  await page.getByRole("menuitem", { name: label, exact: true }).click();
  return page.getByRole("dialog", {
    name: label === "查看详情" ? "邀请详情" : label,
    exact: true,
  });
}
async function delivered(page, email) {
  let current;
  await expect
    .poll(
      async () => {
        current = await invitation(page, email);
        return current?.latest_delivery.phase;
      },
      { timeout: 20000, intervals: [250, 500, 1000] },
    )
    .toBe("sent");
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(
    page.getByRole("button", { name: `${email} 的邀请操作`, exact: true }),
  ).toBeEnabled();
  return current;
}

test("real invitation creation, resend, retry and revocation expose only safe delivery facts and registration enters the directory", async ({
  page,
  owned,
}) => {
  test.setTimeout(150000);
  if (process.env.FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE !== "1")
    throw new Error("COORDINATE_OWNED_SMTP_MAILBOX_BEFORE_RUNNING");
  const suffix = randomBytes(6).toString("hex");
  const email = `system-invite-${suffix}@example.test`;
  const cancelledEmail = `system-revoke-${suffix}@example.test`;
  const password = `Aa7!${randomBytes(24).toString("base64url")}`;
  await loginSystem(page, owned, "/system/invitations");
  const first = await create(page, email);
  const creationFinished = Date.now();
  expect(Object.keys(first).sort()).toEqual([
    "created_at",
    "email",
    "expires_at",
    "id",
    "latest_delivery",
    "version",
  ]);
  const originalLink = await mailLink(owned, email, "invite");
  const firstDelivery = await delivered(page, email);
  expect(firstDelivery.latest_delivery.channel).toBe("smtp");
  expect(firstDelivery.latest_delivery.attempt_result).toBe("sent");
  expect(Object.keys(firstDelivery.latest_delivery).sort()).toEqual([
    "accepted_at",
    "attempt_result",
    "attempts",
    "channel",
    "job_id",
    "phase",
    "reason",
    "version",
  ]);
  const details = await action(page, email, "查看详情");
  await expect(details).toContainText("SMTP");
  await expect(details).toContainText("不保证邮件已到达收件箱");
  expect(
    await details.evaluate(
      (element, link) => element.textContent.includes(link),
      originalLink,
    ),
  ).toBe(false);
  await details.getByRole("button", { name: "完成", exact: true }).click();

  const retry = await action(page, email, "重试投递");
  await expect(retry).toContainText("可能导致重复邮件");
  await retry
    .getByRole("button", { name: "确认重试投递", exact: true })
    .click();
  await expect(retry.getByRole("status")).toContainText("请求已受理");
  await retry.getByRole("button", { name: "完成", exact: true }).click();
  const retried = await delivered(page, email);
  expect(retried.latest_delivery.job_id).not.toBe(
    firstDelivery.latest_delivery.job_id,
  );
  expect(retried.expires_at).toBe(first.expires_at);
  expect((await mailLink(owned, email, "invite")) === originalLink).toBe(true);

  await create(page, cancelledEmail);
  const cancelledLink = await mailLink(owned, cancelledEmail, "invite");
  let revoked = await action(page, cancelledEmail, "撤销邀请");
  await revoked.getByRole("button", { name: "取消", exact: true }).click();
  expect(!!(await invitation(page, cancelledEmail))).toBe(true);
  revoked = await action(page, cancelledEmail, "撤销邀请");
  await revoked.getByRole("button", { name: "确认撤销", exact: true }).click();
  await expect(revoked.getByRole("status")).toContainText("原链接已失效");
  await revoked.getByRole("button", { name: "完成", exact: true }).click();
  expect(await invitation(page, cancelledEmail)).toBeUndefined();

  // The service has a real 60-second resend interval. Wait for that explicit
  // elapsed-time condition; do not repeatedly submit new mutation keys.
  await expect
    .poll(() => Date.now() - creationFinished, {
      timeout: 65000,
      intervals: [1000],
      message: "owned invitation resend interval elapsed",
    })
    .toBeGreaterThanOrEqual(61000);
  const resend = await action(page, email, "重发邀请");
  await resend.getByRole("button", { name: "确认重发", exact: true }).click();
  await expect(resend.getByRole("status")).toContainText("请求已受理");
  await resend.getByRole("button", { name: "完成", exact: true }).click();
  const resent = await delivered(page, email);
  expect(resent.id).toBe(first.id);
  expect(resent.expires_at).toBe(first.expires_at);
  expect(resent.latest_delivery.job_id).not.toBe(
    retried.latest_delivery.job_id,
  );
  expect((await mailLink(owned, email, "invite")) === originalLink).toBe(true);

  await closeSystemSession(page);
  await openPrivateLink(page, cancelledLink);
  await expect(page.getByRole("alert")).toContainText("失效");
  await expect(page.getByLabel("密码", { exact: true })).toHaveCount(0);
  await openPrivateLink(page, originalLink);
  await expect(page.getByLabel("用户名", { exact: true })).toBeEnabled();
  await page.getByLabel("用户名", { exact: true }).fill(`system-${suffix}`);
  await page.getByLabel("显示名", { exact: true }).fill("系统邀请真实用户");
  await fillPrivate(page.getByLabel("密码", { exact: true }), password);
  await fillPrivate(page.getByLabel("确认密码", { exact: true }), password);
  await page.getByRole("button", { name: "接受邀请", exact: true }).click();
  await expect(page.getByRole("status")).toContainText(
    "账号已创建，请登录后继续。",
  );
  await loginSystem(page, owned);
  await expect(
    page
      .getByRole("list", { name: "用户目录", exact: true })
      .getByText(email, { exact: true }),
  ).toBeVisible();
});
