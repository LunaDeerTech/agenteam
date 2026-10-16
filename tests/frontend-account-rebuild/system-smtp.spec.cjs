const { randomBytes } = require("node:crypto");
const { test, expect, loginSystem } = require("./system-owned.cjs");
const { fillPrivate } = require("./owned-fixture.cjs");
const { api } = require("./journey-support.cjs");
const { readLastMail } = require("./mail-reader.cjs");

function safeRoute(handler) {
  return async (route) => {
    try {
      await handler(route);
    } catch {
      await route.abort("failed").catch(() => {});
      throw new Error("OWNED_SYSTEM_SMTP_FORWARDING_FAILED");
    }
  };
}
async function settings(page) {
  const response = await api(page, "/system/smtp");
  expect(response.status).toBe(200);
  expect(Object.keys(response.value).sort()).toEqual([
    "auto_retry_count",
    "configured",
    "credential_present",
    "encryption",
    "host",
    "id",
    "port",
    "retry_interval_seconds",
    "sender_email",
    "sender_name",
    "username",
    "version",
  ]);
  return response.value;
}
async function save(page) {
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "SMTP 配置已保存" }),
  ).toBeVisible();
  await expect(page.getByLabel("端口", { exact: true })).toBeEnabled();
}
async function jobPhase(page, id, phase) {
  let job;
  await expect
    .poll(
      async () => {
        const response = await api(page, `/system/mail-jobs/${id}/management`);
        if (response.status !== 200) return `http-${response.status}`;
        job = response.value;
        return job.phase;
      },
      { timeout: 20000, intervals: [250, 500, 1000] },
    )
    .toBe(phase);
  expect(Object.keys(job).sort()).toEqual([
    "attempt_channel",
    "attempt_result",
    "attempts",
    "channel",
    "created_at",
    "job_id",
    "kind",
    "phase",
    "reason",
    "version",
  ]);
  return job;
}

test("real SMTP keeps secrets private, replays its original write and separates accepted tests from delivery and retry", async ({
  page,
  owned,
}) => {
  test.setTimeout(120000);
  if (process.env.FRONTEND_ACCOUNT_MAILBOX_EXCLUSIVE !== "1")
    throw new Error("COORDINATE_OWNED_SMTP_MAILBOX_BEFORE_RUNNING");
  if (process.env.NODE_EXTRA_CA_CERTS !== owned.certificate_ca_file)
    throw new Error("OWNED_SYSTEM_NODE_CA_REQUIRED");
  await loginSystem(page, owned, "/system/smtp");
  const password = page.getByLabel("密码", { exact: true });
  const port = page.getByLabel("端口", { exact: true });
  await expect(port).toBeEnabled();
  const initial = await settings(page);
  expect(initial.configured && initial.credential_present).toBe(true);
  expect((await password.inputValue()) === "").toBe(true);
  let blankKeptSecret = false;
  const observeKeep = (request) => {
    if (
      new URL(request.url()).pathname === "/api/v1/system/smtp" &&
      request.method() === "PUT"
    ) {
      const value = request.postDataJSON();
      blankKeptSecret =
        value.credential_action === "keep" && !Object.hasOwn(value, "password");
    }
  };
  page.on("request", observeKeep);
  await page
    .getByLabel("发件人显示名", { exact: true })
    .fill(`系统邮件验证 ${randomBytes(4).toString("hex")}`);
  await save(page);
  page.off("request", observeKeep);
  expect(blankKeptSecret).toBe(true);
  const kept = await settings(page);
  expect(kept.credential_present).toBe(true);

  // Forward a real accepted secret replacement, then lose its response. Only
  // comparisons, never private bodies or headers, may reach the reporter.
  const replacement = `Owned-${randomBytes(24).toString("base64url")}`;
  const attempts = [];
  let appliedVersion;
  await page.route(
    "**/api/v1/system/smtp",
    safeRoute(async (route) => {
      if (route.request().method() !== "PUT") return route.continue();
      attempts.push({
        key: route.request().headers()["idempotency-key"],
        body: route.request().postData(),
      });
      const response = await route.fetch();
      if (response.status() !== 200) throw new Error();
      const value = await response.json();
      if (attempts.length === 1) {
        appliedVersion = value.applied_version;
        await route.abort("connectionfailed");
      } else await route.fulfill({ response });
    }),
  );
  await fillPrivate(password, replacement);
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("原配置操作结果尚未确认");
  expect((await password.inputValue()) === "").toBe(true);
  await expect(password).toBeDisabled();
  await page.getByRole("button", { name: "检查当前状态", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  expect(attempts.length).toBe(1);
  await page.getByRole("button", { name: "重试原请求", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "SMTP 配置已保存" }),
  ).toBeVisible();
  expect(attempts.length).toBe(2);
  expect(
    attempts[0].key === attempts[1].key &&
      attempts[0].body === attempts[1].body &&
      JSON.parse(attempts[0].body).password === replacement,
  ).toBe(true);
  await page.unroute("**/api/v1/system/smtp");
  const replaced = await settings(page);
  expect(replaced.version).toBe(appliedVersion);
  expect(replaced.credential_present).toBe(true);
  expect(
    await page
      .locator("body")
      .evaluate((node, value) => node.textContent.includes(value), replacement),
  ).toBe(false);
  attempts.length = 0;

  // This destination is the same private fixture host on an unallowlisted
  // port. Saving it must not send; the independent test job is refused by
  // the real outbound policy without adding network exceptions.
  await port.fill(initial.port === 65535 ? "65534" : "65535");
  await page.getByLabel("自动重试次数", { exact: true }).fill("0");
  await save(page);
  await page.getByRole("tab", { name: "测试与任务", exact: true }).click();
  const recipient = `system-smtp-${randomBytes(6).toString("hex")}@example.test`;
  let acceptedId;
  let testWrites = 0;
  let loseRead = false;
  await page.route(
    "**/api/v1/system/smtp/test",
    safeRoute(async (route) => {
      testWrites++;
      const response = await route.fetch();
      if (response.status() !== 202) throw new Error();
      acceptedId = (await response.json()).job_id;
      loseRead = true;
      await route.fulfill({ response });
    }),
  );
  await page.route(
    "**/api/v1/system/mail-jobs/*/management",
    safeRoute(async (route) => {
      if (loseRead) {
        loseRead = false;
        await route.abort("connectionfailed");
      } else await route.continue();
    }),
  );
  await page.getByLabel("测试收件邮箱", { exact: true }).fill(recipient);
  await page.getByRole("button", { name: "请求测试发送", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "测试请求已受理" }),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toContainText(
    "任务读取失败，已受理的请求不会重新发送",
  );
  expect(testWrites).toBe(1);
  const failed = await jobPhase(page, acceptedId, "failed");
  expect(failed.reason).toBe("policy_rejected");
  await page.getByRole("button", { name: "重新读取任务", exact: true }).click();
  await expect(page.getByText("出站规则拒绝", { exact: true })).toBeVisible();
  expect(testWrites).toBe(1);
  await page.unroute("**/api/v1/system/smtp/test");
  await page.unroute("**/api/v1/system/mail-jobs/*/management");

  await page.getByRole("tab", { name: "配置", exact: true }).click();
  await port.fill(String(initial.port));
  await save(page);
  await page.getByRole("tab", { name: "测试与任务", exact: true }).click();
  await page
    .getByRole("button", { name: `查看测试任务 ${acceptedId}`, exact: true })
    .click();
  await page.getByRole("button", { name: "申请重试投递", exact: true }).click();
  const retry = page.getByRole("dialog", {
    name: "申请新的投递重试",
    exact: true,
  });
  await expect(retry).toContainText("可能重复投递");
  await retry.getByRole("button", { name: "申请重试", exact: true }).click();
  const retryStatus = page
    .getByRole("status")
    .filter({ hasText: "新的投递重试已受理" });
  await expect(retryStatus).toBeVisible();
  const retryId = (
    await retryStatus.locator(".job-reference").first().textContent()
  )
    .replace(/^任务\s+/, "")
    .trim();
  expect(retryId).not.toBe(acceptedId);
  const sent = await jobPhase(page, retryId, "sent");
  expect(sent.attempt_channel).toBe("smtp");
  expect(sent.attempt_result).toBe("sent");
  await expect
    .poll(
      async () => {
        const raw = await readLastMail(owned);
        const to = raw
          ?.slice(0, raw.indexOf("\r\n\r\n"))
          .match(/^To:\s*(.+)$/im)?.[1];
        return to === recipient || !!to?.endsWith(`<${recipient}>`);
      },
      { timeout: 10000 },
    )
    .toBe(true);
  await page.getByRole("button", { name: "重新读取任务", exact: true }).click();
  await expect(
    page.getByText("邮件服务器已接收", { exact: true }),
  ).toBeVisible();

  // Explicit removal and stopping are separate, confirmed actions; an empty
  // ordinary password field above retained the saved credential.
  await page.getByRole("tab", { name: "配置", exact: true }).click();
  await page
    .getByRole("button", { name: "移除已保存的认证", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "移除已保存的认证", exact: true })
    .getByRole("button", { name: "确认移除认证", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "已保存的认证账号与密码已移除",
  );
  const removed = await settings(page);
  expect(
    removed.configured &&
      !removed.credential_present &&
      removed.username === "",
  ).toBe(true);
  await page.getByRole("button", { name: "停用 SMTP", exact: true }).click();
  await page
    .getByRole("dialog", { name: "停用 SMTP", exact: true })
    .getByRole("button", { name: "确认停用", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText("停用操作已确认");
  const stopped = await settings(page);
  expect(stopped.configured || stopped.credential_present).toBe(false);
  expect(stopped.auto_retry_count).toBe("0");
});
