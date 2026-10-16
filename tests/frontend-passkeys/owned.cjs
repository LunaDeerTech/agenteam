// Dedicated real App journey: private task-owned runtime only, never public SMTP.
const fs = require("node:fs");
const path = require("node:path");
const https = require("node:https");
const { createHash, X509Certificate } = require("node:crypto");
const { chromium, expect } = require("@playwright/test");
const { readLastMail } = require("../frontend-account-rebuild/mail-reader.cjs");
const {
  observeOwnedCertificate,
} = require("../frontend-account-rebuild/fixed-runtime.cjs");

const scope = "account-passkeys-email-change";
const origin = "https://localhost:5270";
function requireFact(value, failure) {
  if (!value) throw new Error(failure);
}
function privateJSON(filename) {
  const info = fs.lstatSync(filename);
  requireFact(
    info.isFile() &&
      !info.isSymbolicLink() &&
      info.uid === process.getuid() &&
      (info.mode & 0o777) === 0o600,
    "OWNED_PRIVATE_FILE_REQUIRED",
  );
  return JSON.parse(fs.readFileSync(filename, "utf8"));
}
function loadRuntime() {
  const filename = process.env.PASSKEY_OWNED_RUNTIME;
  requireFact(filename && path.isAbsolute(filename), "OWNED_RUNTIME_REQUIRED");
  const folder = fs.lstatSync(path.dirname(filename));
  requireFact(
    folder.isDirectory() &&
      !folder.isSymbolicLink() &&
      folder.uid === process.getuid() &&
      (folder.mode & 0o777) === 0o700,
    "OWNED_PRIVATE_DIRECTORY_REQUIRED",
  );
  const runtime = privateJSON(filename);
  requireFact(
    process.env.NODE_EXTRA_CA_CERTS === runtime.certificate_ca_file,
    "OWNED_NODE_CA_REQUIRED",
  );
  const sidecar = privateJSON(
    path.join(path.dirname(filename), "passkey-owned.json"),
  );
  requireFact(
    runtime.origin === origin &&
      runtime.test_scope === scope &&
      sidecar.test_scope === scope &&
      sidecar.runtime_file === filename &&
      sidecar.nonce === runtime.nonce &&
      /^[a-f0-9]{32}$/.test(runtime.nonce || "") &&
      Date.parse(runtime.expires_at) > Date.now() + 120000,
    "OWNED_SCOPE_OR_EXPIRY_MISMATCH",
  );
  requireFact(
    runtime.regular_credentials?.email?.endsWith("@example.test") &&
      typeof runtime.regular_credentials.password === "string" &&
      runtime.regular_credentials.password.length > 0,
    "OWNED_REGULAR_ACCOUNT_REQUIRED",
  );
  const leaf = new X509Certificate(fs.readFileSync(runtime.certificate_file));
  const ca = new X509Certificate(fs.readFileSync(runtime.certificate_ca_file));
  const spki = createHash("sha256")
    .update(leaf.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
  requireFact(
    leaf.checkHost("localhost") &&
      ca.ca &&
      leaf.checkIssued(ca) &&
      leaf.verify(ca.publicKey) &&
      spki === runtime.certificate_spki &&
      Date.parse(leaf.validFrom) <= Date.now() &&
      Date.parse(leaf.validTo) > Date.now() &&
      Date.parse(ca.validFrom) <= Date.now() &&
      Date.parse(ca.validTo) > Date.now(),
    "OWNED_CERTIFICATE_MISMATCH",
  );
  return runtime;
}
async function identity(runtime) {
  return new Promise((resolve, reject) => {
    const request = https.get(
      {
        hostname: "127.0.0.1",
        port: 5270,
        servername: "localhost",
        path: "/__fixture__/identity",
        ca: fs.readFileSync(runtime.certificate_ca_file),
        headers: { Host: "localhost:5270" },
        timeout: 5000,
      },
      (response) => {
        const peerRaw = response.socket.getPeerCertificate().raw;
        let raw = "";
        response.on("data", (chunk) => {
          raw += chunk;
          if (raw.length > 4096) request.destroy();
        });
        response.on("end", () => {
          try {
            requireFact(
              response.statusCode === 200 &&
                JSON.parse(raw).nonce === runtime.nonce,
              "OWNED_IDENTITY_MISMATCH",
            );
            const peer = new X509Certificate(peerRaw);
            requireFact(
              createHash("sha256")
                .update(peer.publicKey.export({ type: "spki", format: "der" }))
                .digest("base64") === runtime.certificate_spki,
              "OWNED_LIVE_CERTIFICATE_MISMATCH",
            );
            resolve();
          } catch {
            reject(new Error("OWNED_TLS_IDENTITY_FAILED"));
          }
        });
      },
    );
    request.on("timeout", () => request.destroy());
    request.on("error", () => reject(new Error("OWNED_TLS_IDENTITY_FAILED")));
  });
}
function extractCode(raw, recipient, purpose) {
  if (!raw) return undefined;
  const split = raw.indexOf("\r\n\r\n");
  requireFact(split >= 0, "OWNED_MAIL_INVALID");
  const headers = raw.slice(0, split).replace(/\r\n[ \t]+/g, " ");
  const to = headers.match(/^To:\s*(.+)$/im)?.[1];
  if (!(to === recipient || to?.endsWith(`<${recipient}>`))) return undefined;
  let body = raw.slice(split + 4);
  if (/^Content-Transfer-Encoding:\s*base64\s*$/im.test(headers))
    body = Buffer.from(body.replace(/\s/g, ""), "base64").toString("utf8");
  else if (/^Content-Transfer-Encoding:\s*quoted-printable\s*$/im.test(headers))
    body = body
      .replace(/=\r\n/g, "")
      .replace(/=([a-f0-9]{2})/gi, (_, hex) =>
        String.fromCharCode(parseInt(hex, 16)),
      );
  return body.match(
    new RegExp(
      `Agenteam ${purpose} verification code: ([0-9]{6})(?:\\r?\\n|$)`,
    ),
  )?.[1];
}
async function mailCode(runtime, recipient, purpose, previous) {
  let code;
  await expect
    .poll(
      async () => {
        const raw = await readLastMail(runtime);
        if (raw && raw !== previous)
          code = extractCode(raw, recipient, purpose);
        return !!code;
      },
      { timeout: 20000, message: "OWNED_MATCHING_CODE_MAIL_REQUIRED" },
    )
    .toBe(true);
  return code;
}
async function fillPrivate(locator, value) {
  try {
    await locator.fill(value);
  } catch {
    throw new Error("PRIVATE_FIELD_INTERACTION_FAILED");
  }
}
async function logout(page) {
  return page.evaluate(async () => {
    const current = await fetch("/api/v1/session", { cache: "no-store" });
    if (current.status === 401) return 401;
    if (current.status !== 200) return current.status;
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
    return response.status;
  });
}
async function signatureBudget(page) {
  return page.evaluate(async () => {
    const {
      authenticatePasskey,
      decodeCredentialBytes,
      encodeCredentialBytes,
    } = await import("/src/account/webauthn.ts");
    const sessionResponse = await fetch("/api/v1/session", {
      cache: "no-store",
      signal: AbortSignal.timeout(10000),
    });
    if (sessionResponse.status !== 200)
      throw new Error("BUDGET_SESSION_REQUIRED");
    const session = await sessionResponse.json();
    const post = async (route, body, key) => {
      const response = await fetch(`/api/v1${route}`, {
        method: "POST",
        signal: AbortSignal.timeout(10000),
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": session.csrf_token,
          ...(key ? { "Idempotency-Key": key } : {}),
        },
        body: JSON.stringify(body),
      });
      return { status: response.status, value: await response.json() };
    };
    const prepare = async () => {
      const options = await post("/me/passkeys/reauth/options", {
        purpose: "change_email",
      });
      if (options.status !== 200) throw new Error("BUDGET_OPTIONS_REJECTED");
      const credential = await authenticatePasskey(
        options.value.public_key,
        AbortSignal.timeout(15000),
      );
      const bad = structuredClone(credential);
      const signature = new Uint8Array(
        decodeCredentialBytes(bad.response.signature),
      );
      signature[signature.length - 1] ^= 1;
      bad.response.signature = encodeCredentialBytes(signature.buffer);
      return {
        good: { operation_id: options.value.operation_id, credential },
        bad: { operation_id: options.value.operation_id, credential: bad },
      };
    };
    const complete = (body, key = crypto.randomUUID()) =>
      post("/me/passkeys/reauth/complete", body, key);
    const rejected = (response) =>
      response.status === 400 &&
      response.value.code === "INVALID_ARGUMENT" &&
      response.value.field_errors?.some(
        (field) => field.path === "/credential" && field.code === "INVALID",
      );
    const replay = await prepare();
    const key = crypto.randomUUID();
    if (
      !rejected(await complete(replay.bad, key)) ||
      !rejected(await complete(replay.bad, key))
    )
      throw new Error("BUDGET_ORIGINAL_REJECTION_REPLAY_FAILED");
    for (let index = 0; index < 3; index++)
      if (!rejected(await complete(replay.bad)))
        throw new Error("BUDGET_DISTINCT_REJECTION_FAILED");
    if ((await complete(replay.good)).status !== 200)
      throw new Error("BUDGET_ORIGINAL_RETRY_DOUBLE_COUNTED");
    const exhausted = await prepare();
    for (let index = 0; index < 5; index++)
      if (!rejected(await complete(exhausted.bad)))
        throw new Error("BUDGET_FIVE_REJECTIONS_FAILED");
    const locked = await complete(exhausted.good);
    if (locked.status !== 409 || locked.value.code !== "INVALID_STATE")
      throw new Error("BUDGET_EXHAUSTION_NOT_ENFORCED");
    return {
      pass: true,
      native_assertions: 2,
      repeated_original_key_counted_once: true,
      correct_after_four_accepted: true,
      correct_after_five_rejected: true,
    };
  });
}

(async () => {
  const result = { kind: "owned-real-app-native-webauthn", pass: false };
  const statuses = [];
  const unexpected = [];
  const pageErrors = [];
  const failedResources = [];
  let browser, context, page, secondContext, secondPage, cdp, authenticatorId;
  let originalConfirmation;
  let stage = "private-runtime";
  let identityVerified = false;
  try {
    const runtime = loadRuntime();
    stage = "strict-tls-identity";
    await identity(runtime);
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: [
        "--no-sandbox",
        `--ignore-certificate-errors-spki-list=${runtime.certificate_spki}`,
      ],
    });
    context = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
      locale: "zh-CN",
    });
    page = await context.newPage();
    page.setDefaultTimeout(10000);
    page.on("pageerror", (error) => pageErrors.push(error.name));
    page.on("requestfailed", (request) =>
      failedResources.push({
        path: new URL(request.url()).pathname,
        reason: request.failure()?.errorText,
      }),
    );
    const handleRequest = async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin !== origin) {
        unexpected.push("UNEXPECTED_ORIGIN");
        return route.abort("blockedbyclient");
      }
      if (url.pathname === "/api/v1/me/email-change/confirm") {
        const frozen = {
          key: request.headers()["idempotency-key"],
          body: request.postData(),
        };
        if (!originalConfirmation) {
          const response = await route.fetch();
          if (response.status() === 200) {
            originalConfirmation = frozen;
            result.confirmation_response_deliberately_lost = true;
            await response.dispose();
            return route.abort("failed");
          }
          await route.fulfill({ response });
          await response.dispose();
          return;
        }
        if (
          !frozen.key ||
          frozen.key !== originalConfirmation.key ||
          frozen.body !== originalConfirmation.body
        ) {
          unexpected.push("CONFIRMATION_INTENT_CHANGED");
          return route.abort("blockedbyclient");
        }
        result.original_confirmation_replayed = true;
      }
      return route.continue();
    };
    await page.route("**/*", (route) =>
      handleRequest(route).catch(async () => {
        unexpected.push("OWNED_INTERCEPTION_FAILED");
        await route.abort("failed").catch(() => {});
      }),
    );
    page.on("response", (response) => {
      const url = new URL(response.url());
      if (url.pathname.startsWith("/api/v1/"))
        statuses.push({
          path: url.pathname,
          method: response.request().method(),
          status: response.status(),
        });
    });
    const certificate = await observeOwnedCertificate(page, runtime);
    try {
      const response = await page.goto(origin + "/__fixture__/identity");
      certificate.verify();
      requireFact(
        response?.status() === 200 &&
          (await response.json()).nonce === runtime.nonce,
        "OWNED_BROWSER_IDENTITY_MISMATCH",
      );
      identityVerified = true;
    } finally {
      await certificate.close();
    }
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
    stage = "password-login-navigation";
    await page.goto(origin + "/settings/password");
    stage = "password-login-form";
    await fillPrivate(
      page.getByLabel("邮箱", { exact: true }),
      runtime.regular_credentials.email,
    );
    await fillPrivate(
      page.getByLabel("密码", { exact: true }),
      runtime.regular_credentials.password,
    );
    stage = "password-login-submit";
    await page.getByRole("button", { name: "登录", exact: true }).click();
    stage = "security-ready";
    await expect(
      page.getByRole("heading", { name: "登录与安全", exact: true }),
    ).toBeVisible();
    stage = "first-registration-recovery";
    await page
      .getByRole("button", { name: "添加 Passkey", exact: true })
      .click();
    let dialog = page.getByRole("dialog", {
      name: "添加 Passkey",
      exact: true,
    });
    let previous = await readLastMail(runtime);
    await fillPrivate(
      dialog.getByLabel("当前密码", { exact: true }),
      runtime.regular_credentials.password,
    );
    await dialog
      .getByRole("button", { name: "发送验证码", exact: true })
      .click();
    let code = await mailCode(
      runtime,
      runtime.regular_credentials.email,
      "Passkey recovery",
      previous,
    );
    await fillPrivate(dialog.getByLabel("邮箱验证码", { exact: true }), code);
    code = undefined;
    previous = undefined;
    await dialog
      .getByRole("button", { name: "验证并继续", exact: true })
      .click();
    await dialog
      .getByLabel("Passkey 名称", { exact: true })
      .fill("本轮隔离凭据");
    stage = "native-registration";
    await dialog
      .getByRole("button", { name: "创建 Passkey", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toContainText("已添加");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    requireFact(
      (await cdp.send("WebAuthn.getCredentials", { authenticatorId }))
        .credentials.length === 1,
      "NATIVE_RESIDENT_CREDENTIAL_REQUIRED",
    );
    stage = "discoverable-login";
    requireFact((await logout(page)) === 204, "OWNED_LOGOUT_FAILED");
    await page.goto(origin + "/login");
    await expect(page.getByLabel("邮箱", { exact: true })).toHaveValue("");
    await page
      .getByRole("button", { name: "使用 Passkey 登录", exact: true })
      .click();
    await expect(page).toHaveURL(origin + "/");
    stage = "second-password-session";
    secondContext = await browser.newContext();
    secondPage = await secondContext.newPage();
    secondPage.setDefaultTimeout(10000);
    await secondPage.route("**/*", (route) =>
      new URL(route.request().url()).origin === origin
        ? route.continue()
        : route.abort("blockedbyclient"),
    );
    await secondPage.goto(origin + "/login");
    await fillPrivate(
      secondPage.getByLabel("邮箱", { exact: true }),
      runtime.regular_credentials.email,
    );
    await fillPrivate(
      secondPage.getByLabel("密码", { exact: true }),
      runtime.regular_credentials.password,
    );
    await secondPage.getByRole("button", { name: "登录", exact: true }).click();
    await expect(secondPage).toHaveURL(origin + "/");
    const strongSession = await page.evaluate(
      async () =>
        (await (await fetch("/api/v1/session", { cache: "no-store" })).json())
          .session.id,
    );
    const sessionCookies = (await context.cookies(origin))
      .filter((cookie) => cookie.httpOnly)
      .map((cookie) => ({ name: cookie.name, value: cookie.value }));
    requireFact(sessionCookies.length > 0, "OWNED_SESSION_COOKIE_REQUIRED");
    stage = "fresh-proof-email-change";
    await page.bringToFront();
    await page.goto(origin + "/settings/profile");
    await page.getByRole("button", { name: "修改邮箱", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "修改邮箱", exact: true });
    await dialog
      .getByRole("button", { name: "使用 Passkey 验证", exact: true })
      .click();
    const email = `passkey-updated-${runtime.nonce.slice(0, 12)}@example.test`;
    await fillPrivate(dialog.getByLabel("新邮箱", { exact: true }), email);
    previous = await readLastMail(runtime);
    await dialog
      .getByRole("button", { name: "发送验证码", exact: true })
      .click();
    code = await mailCode(runtime, email, "Email address change", previous);
    requireFact(
      (await page.getByLabel("邮箱", { exact: true }).inputValue()) ===
        runtime.regular_credentials.email,
      "EMAIL_CHANGED_BEFORE_CODE_CONFIRMATION",
    );
    await fillPrivate(dialog.getByLabel("新邮箱验证码", { exact: true }), code);
    code = undefined;
    previous = undefined;
    stage = "email-code-confirmation";
    await dialog
      .getByRole("button", { name: "验证并修改邮箱", exact: true })
      .click();
    await expect(
      dialog.getByText("原操作结果尚未确认。", { exact: false }),
    ).toBeVisible();
    await dialog
      .getByRole("button", { name: "重试原请求", exact: true })
      .click();
    await expect(dialog.getByRole("status")).toHaveText("邮箱已修改。");
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    requireFact(
      (await page.getByLabel("邮箱", { exact: true }).inputValue()) === email,
      "CONFIRMED_EMAIL_NOT_READ_BACK",
    );
    stage = "email-session-policy";
    const current = await page.evaluate(async () => {
      const response = await fetch("/api/v1/session", { cache: "no-store" });
      return {
        status: response.status,
        value: response.status === 200 ? await response.json() : undefined,
      };
    });
    requireFact(
      current.status === 200 &&
        current.value.session.id === strongSession &&
        current.value.user.email === email,
      "CURRENT_STRONG_SESSION_NOT_PRESERVED",
    );
    const afterCookies = await context.cookies(origin);
    requireFact(
      sessionCookies.every((before) =>
        afterCookies.some(
          (after) => after.name === before.name && after.value === before.value,
        ),
      ),
      "CURRENT_SESSION_COOKIE_ROTATED",
    );
    requireFact(
      (await secondPage.evaluate(
        async () =>
          (await fetch("/api/v1/session", { cache: "no-store" })).status,
      )) === 401,
      "SECOND_SESSION_NOT_REVOKED",
    );
    result.current_session_preserved_and_other_session_revoked = true;
    requireFact(
      result.original_confirmation_replayed === true,
      "ORIGINAL_RECEIPT_RECOVERY_NOT_OBSERVED",
    );
    requireFact(unexpected.length === 0, "UNEXPECTED_NETWORK");
    result.main_chain_pass = true;
    stage = "signature-failure-budget";
    await page.bringToFront();
    result.signature_budget = await signatureBudget(page);
    result.pass = true;
  } catch (error) {
    if (stage === "signature-failure-budget")
      result.signature_budget = { pass: false };
    result.failure_stage = stage;
    result.location = page?.url();
    result.failure =
      (error.message || "").match(/\bBUDGET_[A-Z0-9_]+\b/)?.[0] ??
      (/^[A-Z0-9_]+$/.test(error.message || "")
        ? error.message
        : "OWNED_ASSERTION_FAILED");
    result.error_type = error.name;
    process.exitCode = 1;
  } finally {
    if (page && identityVerified)
      result.logout_status = await logout(page).catch(() => "CLEANUP_FAILED");
    if (secondPage)
      result.secondary_logout_status = await logout(secondPage).catch(
        () => "CLEANUP_FAILED",
      );
    if (cdp && authenticatorId)
      await cdp
        .send("WebAuthn.removeVirtualAuthenticator", { authenticatorId })
        .catch(() => {});
    if (context) await context.close();
    if (secondContext) await secondContext.close();
    if (browser) await browser.close();
    result.browser_closed = true;
    result.statuses = statuses;
    result.unexpected = unexpected;
    result.page_errors = pageErrors;
    result.failed_resources = failedResources;
    if (identityVerified && ![204, 401].includes(result.logout_status)) {
      result.pass = false;
      process.exitCode = 1;
    }
    if (secondPage && ![204, 401].includes(result.secondary_logout_status)) {
      result.pass = false;
      process.exitCode = 1;
    }
    originalConfirmation = undefined;
    console.log(JSON.stringify(result));
  }
})();
