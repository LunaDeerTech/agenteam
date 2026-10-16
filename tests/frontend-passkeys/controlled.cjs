"use strict";
// Controlled UI journey: real Chromium WebAuthn signatures, synthetic API state.
// This does not replace the owned App/PG/SMTP integration journey.
const { chromium, expect } = require("@playwright/test");
const crypto = require("node:crypto");
const origin = process.env.PASSKEY_CONTROLLED_ORIGIN;
if (origin !== "http://localhost:5274")
  throw new Error("CONTROLLED_ORIGIN_REQUIRED");
const stamp = () => new Date().toISOString();
const future = (seconds) => new Date(Date.now() + seconds * 1000).toISOString();
let sequence = 1;
const id = () =>
  `019a0000-0000-7000-8000-${String(sequence++).padStart(12, "0")}`;
const user = {
  id: id(),
  email: "owned@example.test",
  username: "owned-user",
  display_name: "Owned User",
  role: "user",
  theme: "light",
  version: "1",
  initial_password_suggestion: false,
};
const session = {
  id: id(),
  issued_at: "2026-10-16T12:00:00.000000Z",
  absolute_expires_at: "2026-10-18T12:00:00.000000Z",
  idle_expires_at: "2026-10-17T12:00:00.000000Z",
};
let authenticated = true,
  version = 1,
  dropRename = true,
  renameCommits = 0,
  nativeAssertions = 0;
const items = [],
  operations = new Map(),
  grants = new Map(),
  codes = new Map(),
  receipts = new Map();
const statuses = [],
  failures = [],
  pageErrors = [],
  failedResources = [];
let browser,
  context,
  page,
  cdp,
  authenticatorId,
  stage = "launch";
function requireFact(value, code) {
  if (!value) throw new Error(code);
}
async function verifyAssertion(body) {
  const operation = operations.get(body.operation_id);
  requireFact(operation && !operation.done, "OPERATION_REJECTED");
  const credential = body.credential;
  const data = JSON.parse(
    Buffer.from(credential.response.clientDataJSON, "base64url").toString(),
  );
  requireFact(
    data.type === "webauthn.get" &&
      data.challenge === operation.challenge &&
      data.origin === origin,
    "CLIENT_DATA_REJECTED",
  );
  const authenticators = await cdp.send("WebAuthn.getCredentials", {
    authenticatorId,
  });
  const stored = authenticators.credentials.find(
    (value) =>
      Buffer.from(value.credentialId, "base64").toString("base64url") ===
      credential.id,
  );
  requireFact(!!stored, "AUTHENTICATOR_CREDENTIAL_MISSING");
  const authData = Buffer.from(
    credential.response.authenticatorData,
    "base64url",
  );
  requireFact(
    authData
      .subarray(0, 32)
      .equals(crypto.createHash("sha256").update("localhost").digest()) &&
      (authData[32] & 5) === 5,
    "RP_OR_USER_VERIFICATION_REJECTED",
  );
  const signed = Buffer.concat([
    authData,
    crypto
      .createHash("sha256")
      .update(Buffer.from(credential.response.clientDataJSON, "base64url"))
      .digest(),
  ]);
  const key = crypto.createPrivateKey({
    key: Buffer.from(stored.privateKey, "base64"),
    type: "pkcs8",
    format: "der",
  });
  requireFact(
    crypto.verify(
      "sha256",
      signed,
      crypto.createPublicKey(key),
      Buffer.from(credential.response.signature, "base64url"),
    ),
    "REAL_SIGNATURE_REJECTED",
  );
  operation.done = true;
  nativeAssertions++;
  return operation;
}
async function api(route) {
  const request = route.request();
  const url = new URL(request.url());
  if (url.origin !== origin) {
    failures.push("UNEXPECTED_ORIGIN");
    return route.abort("blockedbyclient");
  }
  if (!url.pathname.startsWith("/api/")) return route.continue();
  const path = url.pathname.slice("/api/v1".length),
    method = request.method();
  let body;
  try {
    body = request.postData() ? JSON.parse(request.postData()) : {};
  } catch {
    failures.push("MALFORMED_REQUEST");
    return route.abort();
  }
  const key = request.headers()["idempotency-key"];
  const frozen = `${method} ${path} ${request.postData() || ""}`;
  const respond = async (value, status = 200, persist = false) => {
    if (persist) {
      requireFact(!!key, "MUTATION_KEY_MISSING");
      receipts.set(key, { frozen, value, status });
    }
    statuses.push({ path, method, status });
    return route.fulfill({
      status,
      contentType:
        status >= 400 ? "application/problem+json" : "application/json",
      body: JSON.stringify(value),
    });
  };
  if (key && receipts.has(key)) {
    const old = receipts.get(key);
    requireFact(old.frozen === frozen, "ORIGINAL_INTENT_CHANGED");
    return respond(old.value, old.status);
  }
  if (path === "/session")
    return authenticated
      ? respond({ user, session, csrf_token: "owned-session" })
      : respond({ code: "UNAUTHENTICATED", commit_state: "not_started" }, 401);
  if (path === "/auth/bootstrap")
    return respond({
      csrf_token: "owned-browser",
      challenge_modes: [],
      delivery_channel: "smtp",
    });
  if (path === "/auth/passkeys/capabilities")
    return respond({
      available: true,
      reason: "available",
      rp_id: "localhost",
    });
  if (path === "/me") return respond({ user, avatar: null });
  if (path === "/me/passkeys" && method === "GET")
    return respond({
      version: String(version),
      email_verified: true,
      email_initialization_required: false,
      items: items.map(({ credential, ...item }) => item),
    });
  if (path === "/me/passkeys/recovery/request") {
    requireFact(
      body.password === "owned-only-current-password",
      "PASSWORD_NOT_FORWARDED",
    );
    const request_id = id();
    codes.set(request_id, { kind: "recovery" });
    return respond(
      { request_id, expires_at: future(600), resend_after: future(60) },
      202,
      true,
    );
  }
  if (path === "/me/passkeys/recovery/confirm") {
    requireFact(
      codes.get(body.request_id)?.kind === "recovery" && body.code === "123456",
      "RECOVERY_PROOF_REJECTED",
    );
    const grant_id = id();
    grants.set(grant_id, { action: "register" });
    return respond(
      { grant_id, purpose: "manage_passkeys", expires_at: future(300) },
      200,
      true,
    );
  }
  if (
    path === "/me/passkeys/reauth/options" ||
    path === "/auth/passkeys/login/options"
  ) {
    const operation_id = id(),
      challenge = crypto.randomBytes(32).toString("base64url");
    operations.set(operation_id, { ...body, challenge });
    return respond({
      operation_id,
      expires_at: future(300),
      public_key: {
        challenge,
        rpId: "localhost",
        timeout: 15000,
        userVerification: "required",
        ...(path.startsWith("/me/")
          ? {
              allowCredentials: items.map((item) => ({
                id: item.credential,
                type: "public-key",
              })),
            }
          : {}),
      },
    });
  }
  if (path === "/me/passkeys/reauth/complete") {
    const operation = await verifyAssertion(body),
      grant_id = id();
    grants.set(grant_id, operation);
    return respond(
      { grant_id, purpose: operation.purpose, expires_at: future(300) },
      200,
      true,
    );
  }
  if (path === "/auth/passkeys/login/complete") {
    await verifyAssertion(body);
    authenticated = true;
    return respond({ user, session, next_path: "/" }, 200, true);
  }
  if (path === "/me/passkeys/registration/options") {
    const proof = grants.get(body.grant_id);
    requireFact(
      proof?.action === "register" && body.expected_version === String(version),
      "REGISTRATION_GRANT_REJECTED",
    );
    grants.delete(body.grant_id);
    const operation_id = id(),
      challenge = crypto.randomBytes(32).toString("base64url");
    operations.set(operation_id, { challenge, label: body.label });
    return respond({
      operation_id,
      expires_at: future(300),
      public_key: {
        challenge,
        rp: { id: "localhost", name: "Owned agenteam" },
        user: {
          id: Buffer.from("owned-user-handle").toString("base64url"),
          name: user.email,
          displayName: user.display_name,
        },
        pubKeyCredParams: [{ type: "public-key", alg: -7 }],
        timeout: 15000,
        authenticatorSelection: {
          residentKey: "required",
          requireResidentKey: true,
          userVerification: "required",
        },
        attestation: "none",
      },
    });
  }
  if (path === "/me/passkeys/registration/complete") {
    const operation = operations.get(body.operation_id);
    requireFact(
      operation && !operation.done,
      "REGISTRATION_OPERATION_REJECTED",
    );
    const data = JSON.parse(
      Buffer.from(
        body.credential.response.clientDataJSON,
        "base64url",
      ).toString(),
    );
    requireFact(
      data.type === "webauthn.create" &&
        data.origin === origin &&
        data.challenge === operation.challenge,
      "CREATE_CLIENT_DATA_REJECTED",
    );
    const native = await cdp.send("WebAuthn.getCredentials", {
      authenticatorId,
    });
    requireFact(
      native.credentials.some(
        (value) =>
          Buffer.from(value.credentialId, "base64").toString("base64url") ===
          body.credential.id,
      ),
      "NATIVE_CREATION_MISSING",
    );
    operation.done = true;
    const item = {
      id: id(),
      label: operation.label,
      created_at: stamp(),
      last_used_at: null,
      credential: body.credential.id,
    };
    items.push(item);
    version++;
    return respond(
      { version: String(version), id: item.id, created_at: stamp() },
      200,
      true,
    );
  }
  if (
    /^\/me\/passkeys\/[0-9a-f-]+$/.test(path) &&
    ["PATCH", "DELETE"].includes(method)
  ) {
    const item = items.find((item) => path.endsWith(item.id)),
      grant = grants.get(body.grant_id);
    requireFact(
      item &&
        grant?.passkey_id === item.id &&
        grant.action === (method === "PATCH" ? "rename" : "delete") &&
        body.expected_version === String(version),
      "MANAGE_PROOF_REJECTED",
    );
    grants.delete(body.grant_id);
    if (method === "PATCH") {
      item.label = body.label;
      renameCommits++;
    } else items.splice(items.indexOf(item), 1);
    version++;
    const receipt = {
      version: String(version),
      id: item.id,
      created_at: stamp(),
    };
    if (method === "PATCH" && dropRename) {
      dropRename = false;
      requireFact(!!key, "MUTATION_KEY_MISSING");
      receipts.set(key, { frozen, value: receipt, status: 200 });
      return route.abort("failed");
    }
    return respond(receipt, 200, true);
  }
  if (path === "/me/email-change/request") {
    const proof = grants.get(body.grant_id);
    requireFact(
      proof?.purpose === "change_email" &&
        body.expected_version === user.version,
      "EMAIL_GRANT_REJECTED",
    );
    grants.delete(body.grant_id);
    const request_id = id();
    codes.set(request_id, { kind: "email", email: body.email });
    return respond(
      { request_id, expires_at: future(600), resend_after: future(60) },
      202,
      true,
    );
  }
  if (path === "/me/email-change/confirm") {
    const pending = codes.get(body.request_id);
    requireFact(pending?.kind === "email", "EMAIL_REQUEST_REJECTED");
    if (body.code !== "123456")
      return respond(
        {
          code: "INVALID_ARGUMENT",
          commit_state: "not_committed",
          field_errors: [{ path: "/code", code: "INVALID_OR_EXPIRED" }],
        },
        400,
      );
    user.email = pending.email;
    user.version = String(Number(user.version) + 1);
    codes.delete(body.request_id);
    return respond(
      { version: user.version, email: user.email, created_at: stamp() },
      200,
      true,
    );
  }
  failures.push(`${method} ${path}`);
  return respond({ code: "NOT_FOUND", commit_state: "not_started" }, 404);
}
(async () => {
  const result = {
    kind: "synthetic-api-real-browser-webauthn",
    real_app_verified: false,
  };
  try {
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
    context = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
      locale: "zh-CN",
    });
    page = await context.newPage();
    page.setDefaultTimeout(10000);
    page.on("pageerror", (error) =>
      pageErrors.push(error.message.slice(0, 240)),
    );
    page.on("requestfailed", (request) =>
      failedResources.push({
        path: new URL(request.url()).pathname,
        reason: request.failure()?.errorText,
      }),
    );
    await page.route("**/*", (route) =>
      api(route).catch(() => {
        failures.push("CONTROLLED_HANDLER_FAILED");
        return route.abort();
      }),
    );
    cdp = await context.newCDPSession(page);
    await cdp.send("WebAuthn.enable");
    ({ authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    }));
    stage = "first-navigation";
    await page.goto(origin + "/settings/password");
    stage = "security-heading";
    await expect(
      page.getByRole("heading", { name: "登录与安全", exact: true }),
    ).toBeVisible();
    stage = "registration-open";
    await page
      .getByRole("button", { name: "添加 Passkey", exact: true })
      .click();
    let dialog = page.getByRole("dialog", {
      name: "添加 Passkey",
      exact: true,
    });
    await dialog
      .getByLabel("当前密码", { exact: true })
      .fill("owned-only-current-password");
    await dialog
      .getByRole("button", { name: "发送验证码", exact: true })
      .click();
    await dialog.getByLabel("邮箱验证码", { exact: true }).fill("123456");
    await dialog
      .getByRole("button", { name: "验证并继续", exact: true })
      .click();
    await dialog.getByLabel("Passkey 名称", { exact: true }).fill("测试电脑");
    await dialog
      .getByRole("button", { name: "创建 Passkey", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toContainText("已添加");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    stage = "rename-unknown";
    await page
      .getByRole("button", { name: "重命名 测试电脑", exact: true })
      .click();
    dialog = page.getByRole("dialog", { name: "重命名 Passkey", exact: true });
    await dialog
      .getByRole("button", { name: "使用 Passkey 验证", exact: true })
      .click();
    await dialog.getByLabel("Passkey 名称", { exact: true }).fill("日常电脑");
    await dialog.getByRole("button", { name: "保存名称", exact: true }).click();
    await expect(
      dialog.getByText("原操作结果尚未确认。", { exact: false }),
    ).toBeVisible();
    await dialog
      .getByRole("button", { name: "重试原请求", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toContainText("名称已更新");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    requireFact(renameCommits === 1, "RENAME_DUPLICATED");
    stage = "email-change";
    await page.goto(origin + "/settings/profile");
    await page.getByRole("button", { name: "修改邮箱", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "修改邮箱", exact: true });
    await dialog
      .getByRole("button", { name: "使用 Passkey 验证", exact: true })
      .click();
    await dialog
      .getByLabel("新邮箱", { exact: true })
      .fill("updated@example.test");
    await dialog
      .getByRole("button", { name: "发送验证码", exact: true })
      .click();
    requireFact(
      user.email === "owned@example.test",
      "EMAIL_CHANGED_BEFORE_CONFIRMATION",
    );
    await dialog.getByLabel("新邮箱验证码", { exact: true }).fill("000000");
    await dialog
      .getByRole("button", { name: "验证并修改邮箱", exact: true })
      .click();
    await expect(
      dialog.getByText("验证码错误或已过期，请重新获取。", { exact: true }),
    ).toBeVisible();
    requireFact(authenticated, "OTP_ERROR_LOGGED_OUT");
    await dialog.getByLabel("新邮箱验证码", { exact: true }).fill("123456");
    await dialog
      .getByRole("button", { name: "验证并修改邮箱", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toHaveText("邮箱已修改。");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    await expect(page.getByLabel("邮箱", { exact: true })).toHaveValue(
      "updated@example.test",
    );
    stage = "discoverable-login";
    authenticated = false;
    await page.goto(origin + "/login");
    await expect(page.getByLabel("邮箱", { exact: true })).toHaveValue("");
    await page
      .getByRole("button", { name: "使用 Passkey 登录", exact: true })
      .click();
    await expect(page).toHaveURL(origin + "/");
    stage = "native-cancellation-navigation";
    await page.goto(origin + "/settings/password");
    const trigger = page.getByRole("button", {
      name: "重命名 日常电脑",
      exact: true,
    });
    stage = "native-cancellation-open";
    await trigger.click();
    dialog = page.getByRole("dialog", { name: "重命名 Passkey", exact: true });
    await cdp.send("WebAuthn.setAutomaticPresenceSimulation", {
      authenticatorId,
      enabled: false,
    });
    stage = "native-cancellation-start";
    await dialog
      .getByRole("button", { name: "使用 Passkey 验证", exact: true })
      .click();
    stage = "native-cancellation-abort";
    await dialog.getByRole("button", { name: "取消验证", exact: true }).click();
    stage = "native-cancellation-close";
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    stage = "native-cancellation-focus";
    await expect(trigger).toBeFocused();
    await cdp.send("WebAuthn.setAutomaticPresenceSimulation", {
      authenticatorId,
      enabled: true,
    });
    stage = "delete-last-key";
    await page
      .getByRole("button", { name: "删除 日常电脑", exact: true })
      .click();
    dialog = page.getByRole("dialog", { name: "删除 Passkey", exact: true });
    await dialog
      .getByRole("button", { name: "使用 Passkey 验证", exact: true })
      .click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await expect(dialog.getByRole("status")).toContainText("已删除");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    await expect(
      page.getByText("尚未添加 Passkey。", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "添加 Passkey", exact: true }),
    ).toBeFocused();
    requireFact(failures.length === 0, "UNEXPECTED_API");
    requireFact(nativeAssertions >= 4, "SIGNATURE_COVERAGE_MISSING");
    result.pass = true;
  } catch (error) {
    result.pass = false;
    result.failure_stage = stage;
    result.location = page?.url();
    result.assertion = error.name;
    result.failure = /^[A-Z0-9_]+$/.test(error.message || "")
      ? error.message
      : "CONTROLLED_ASSERTION_FAILED";
    result.focus = await page
      ?.evaluate(() => ({
        tag: document.activeElement?.tagName,
        role: document.activeElement?.getAttribute("role"),
        label: document.activeElement?.getAttribute("aria-label"),
        disabled: document.activeElement?.matches(":disabled"),
      }))
      .catch(() => undefined);
    process.exitCode = 1;
  } finally {
    if (cdp && authenticatorId)
      await cdp
        .send("WebAuthn.removeVirtualAuthenticator", { authenticatorId })
        .catch(() => {});
    if (context) await context.close();
    if (browser) await browser.close();
    result.browser_closed = true;
    result.native_assertions_verified = nativeAssertions;
    result.rename_commits = renameCommits;
    result.unexpected = failures;
    result.statuses = statuses;
    result.page_errors = pageErrors;
    result.failed_resources = failedResources;
    console.log(JSON.stringify(result));
  }
})();
