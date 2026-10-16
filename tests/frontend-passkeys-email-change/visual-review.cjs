// Independent visual/interaction review. All API responses are synthetic.
// Native WebAuthn uses an ephemeral CDP authenticator; no backend signature,
// SMTP delivery, real device or account persistence is claimed by this harness.
const {
  chromium,
  expect,
} = require("../account-captcha-web/node_modules/@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const crypto = require("node:crypto");
const args = process.argv.slice(2);
const option = (name, fallback) =>
  args.includes(name) ? args[args.indexOf(name) + 1] : fallback;
const mode = option("--mode", "views");
const modes = ["views", "interactions", "errors", "zoom"];
const rawOrigin = option("--base-url");
const owned = option("--owned");
const run = option("--run", "candidate");
const input = option("--input", "d7c42da5");
const selection = option("--case");
assert(/^[a-z0-9-]+$/.test(run));
const output = path.resolve("output/ai/frontend-passkeys-visual", run);
const results = [];
const uuid = (number) =>
  `019a3333-3333-7333-8333-${String(number).padStart(12, "0")}`;
const instant = "2026-10-16T07:00:00.000000Z";
const iso = (offset = 0) => new Date(Date.now() + offset).toISOString();
const button = (root, name) => root.getByRole("button", { name, exact: true });
const field = (root, name) => root.getByLabel(name, { exact: true });
const modal = (page, name) => page.getByRole("dialog", { name, exact: true });
const keyLabel = "个人电脑";
const freshUser = () => ({
  id: uuid(1),
  email: "visual@example.invalid",
  username: "visual-review",
  display_name: "合成视觉审查",
  role: "admin",
  theme: "system",
  version: "1",
  initial_password_suggestion: false,
});
let origin;

async function settle(page) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    );
  });
}
async function shot(page, name) {
  assert(
    await page
      .locator('input[type="password"], input[autocomplete="one-time-code"]')
      .evaluateAll((nodes) => nodes.every((node) => !node.value)),
    "sensitive input must be empty before screenshot",
  );
  await settle(page);
  await page.screenshot({
    path: path.join(output, `${name}.png`),
    fullPage: true,
  });
}
async function noOverflow(page) {
  const result = await page.evaluate(() => ({
    inner: innerWidth,
    root: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  assert(
    result.root <= result.inner + 1 && result.body <= result.inner + 1,
    "page horizontal overflow",
  );
  return result;
}
async function activeControl(page) {
  await expect
    .poll(() =>
      page.evaluate(() =>
        document.activeElement?.matches(
          "button:not(:disabled), input:not(:disabled), a[href]",
        ),
      ),
    )
    .toBe(true);
}
async function privateFill(locator, value) {
  try {
    await locator.fill(value);
  } catch {
    throw new Error("synthetic private input could not be filled");
  }
}
async function authenticator(page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send(
    "WebAuthn.addVirtualAuthenticator",
    {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    },
  );
  const key = crypto.generateKeyPairSync("ec", { namedCurve: "prime256v1" });
  await cdp.send("WebAuthn.addCredential", {
    authenticatorId,
    credential: {
      credentialId: Buffer.from("visual-credential").toString("base64"),
      isResidentCredential: true,
      rpId: "localhost",
      privateKey: key.privateKey
        .export({ format: "der", type: "pkcs8" })
        .toString("base64"),
      userHandle: Buffer.from("visual-user").toString("base64"),
      signCount: 0,
    },
  });
  return { cdp, authenticatorId };
}
async function setup(context, options = {}) {
  const state = {
    user: freshUser(),
    authenticated: options.route !== "/login",
    keys: {
      version: "1",
      email_verified: options.verified !== false,
      items: options.empty
        ? []
        : [
            {
              id: uuid(3),
              label: keyLabel,
              created_at: instant,
              last_used_at: null,
            },
            {
              id: uuid(4),
              label: "备用安全密钥",
              created_at: instant,
              last_used_at: instant,
            },
          ],
    },
    api: [],
    commands: [],
    unexpected: [],
    pageErrors: [],
    failedResources: [],
    stage: "install-interception",
    failure: undefined,
    hold: undefined,
    purpose: "manage_passkeys",
    pendingEmail: "",
    listFails: false,
  };
  context.reviewState = state;
  if (options.unsupported)
    await context.addInitScript(() =>
      Object.defineProperty(window, "PublicKeyCredential", {
        value: undefined,
        configurable: true,
      }),
    );
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      method = request.method();
    if (url.origin !== origin) {
      state.unexpected.push("outside-origin");
      return route.abort("blockedbyclient");
    }
    const endpoint = url.pathname;
    if (!endpoint.startsWith("/api/")) {
      if (
        ["GET", "HEAD"].includes(method) &&
        !["fetch", "xhr"].includes(request.resourceType())
      )
        return route.continue();
      state.unexpected.push("non-static-request");
      return route.abort("blockedbyclient");
    }
    state.api.push(`${method} ${endpoint}`);
    const respond = (value, status = 200) =>
      route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        body: JSON.stringify(value),
      });
    const error = (code, status = 400, fields = []) =>
      respond(
        { code, commit_state: "not_committed", field_errors: fields },
        status,
      );
    if (url.search) {
      state.unexpected.push("api-query");
      return route.abort("blockedbyclient");
    }
    if (method === "GET" && endpoint === "/api/v1/session")
      return state.authenticated
        ? respond({
            user: state.user,
            session: {
              id: uuid(2),
              issued_at: instant,
              absolute_expires_at: "2026-10-17T22:00:00.000000Z",
              idle_expires_at: "2026-10-17T22:00:00.000000Z",
            },
            csrf_token: "synthetic-visual-context",
          })
        : error("UNAUTHORIZED", 401);
    if (method === "GET" && endpoint === "/api/v1/auth/bootstrap")
      return respond({
        csrf_token: "synthetic-browser-context",
        challenge_modes: [],
        delivery_channel: "smtp",
      });
    if (method === "GET" && endpoint === "/api/v1/me")
      return respond({ user: state.user, avatar: null });
    if (method === "GET" && endpoint === "/api/v1/auth/passkeys/capabilities") {
      if (options.capabilityError) return error("DEPENDENCY_UNAVAILABLE", 503);
      return respond(
        options.domainDisabled
          ? { available: false, reason: "domain_required", rp_id: null }
          : { available: true, reason: "available", rp_id: "localhost" },
      );
    }
    if (method === "GET" && endpoint === "/api/v1/me/passkeys") {
      if (state.listFails) {
        state.listFails = false;
        return route.abort("connectionfailed");
      }
      return respond(state.keys);
    }
    const command = method !== "GET";
    if (command) {
      state.commands.push({
        endpoint,
        method,
        body: request.postData(),
        key: request.headers()["idempotency-key"],
      });
      if (state.hold?.endpoint === endpoint) await state.hold.promise;
      if (state.failure?.endpoint === endpoint) {
        const failure = state.failure;
        state.failure = undefined;
        if (failure.kind === "unknown") return route.abort("connectionfailed");
        return error(
          failure.code || "INVALID_ARGUMENT",
          failure.status || 400,
          failure.fields || [],
        );
      }
    }
    const body = command ? JSON.parse(request.postData() || "{}") : {};
    if (
      method === "POST" &&
      [
        "/api/v1/auth/passkeys/login/options",
        "/api/v1/me/passkeys/reauth/options",
      ].includes(endpoint)
    ) {
      state.purpose = body.purpose || "manage_passkeys";
      return respond({
        operation_id: uuid(10),
        expires_at: iso(300000),
        public_key: {
          challenge: Buffer.from("synthetic-visual-challenge-32bytes").toString(
            "base64url",
          ),
          rpId: "localhost",
          timeout: 60000,
          userVerification: "required",
        },
      });
    }
    if (method === "POST" && endpoint === "/api/v1/me/passkeys/reauth/complete")
      return respond({
        grant_id: uuid(11),
        purpose: state.purpose,
        expires_at: iso(300000),
      });
    if (
      method === "POST" &&
      endpoint === "/api/v1/auth/passkeys/login/complete"
    ) {
      state.authenticated = true;
      return respond({ user: state.user });
    }
    if (
      method === "POST" &&
      [
        "/api/v1/me/passkeys/recovery/request",
        "/api/v1/me/passkeys/recovery/resend",
        "/api/v1/me/email-change/request",
        "/api/v1/me/email-change/resend",
      ].includes(endpoint)
    ) {
      if (body.email) state.pendingEmail = body.email;
      return respond(
        {
          request_id: uuid(12),
          expires_at: iso(600000),
          resend_after: iso(60000),
        },
        202,
      );
    }
    if (
      method === "POST" &&
      endpoint === "/api/v1/me/passkeys/recovery/confirm"
    )
      return respond({
        grant_id: uuid(11),
        purpose: "manage_passkeys",
        expires_at: iso(300000),
      });
    if (
      ["PATCH", "DELETE"].includes(method) &&
      /^\/api\/v1\/me\/passkeys\/[0-9a-f-]+$/.test(endpoint)
    ) {
      const id = endpoint.split("/").at(-1);
      if (method === "DELETE")
        state.keys.items = state.keys.items.filter((item) => item.id !== id);
      else state.keys.items.find((item) => item.id === id).label = body.label;
      state.keys.version = String(BigInt(state.keys.version) + 1n);
      return respond({ id, version: state.keys.version, created_at: instant });
    }
    if (method === "POST" && endpoint === "/api/v1/me/email-change/confirm") {
      state.user.email = state.pendingEmail;
      state.user.version = "2";
      return respond({
        version: "2",
        email: state.user.email,
        created_at: instant,
      });
    }
    state.unexpected.push(`${method} ${endpoint}`);
    return route.abort("blockedbyclient");
  });
  const page = context.pages()[0] || (await context.newPage());
  page.setDefaultTimeout(10000);
  page.on("pageerror", () => state.pageErrors.push("pageerror"));
  page.on("requestfailed", (request) => {
    const url = new URL(request.url());
    if (url.origin === origin && !url.pathname.startsWith("/api/"))
      state.failedResources.push({
        path: url.pathname,
        reason: request.failure()?.errorText,
      });
  });
  state.stage = "native-authenticator";
  const native = options.unsupported ? undefined : await authenticator(page);
  state.stage = "navigation-domcontentloaded";
  await page.goto(origin + (options.route || "/settings/password"), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  state.stage = "initial-visible-ui";
  if (options.route === "/login")
    await expect(button(page, "使用 Passkey 登录")).toBeVisible();
  else if (options.route === "/settings/profile")
    await expect(button(page, "修改邮箱")).toBeVisible();
  else
    await expect(
      page.getByRole("heading", { name: "登录与安全", exact: true }),
    ).toBeVisible();
  await settle(page);
  state.stage = "interaction";
  return { page, state, native };
}
async function runCase(browser, name, callback, options = {}) {
  if (selection && !selection.split(",").includes(name)) return;
  const context = await browser.newContext({
    viewport: {
      width: options.width || 390,
      height: options.width === 1440 ? 960 : 844,
    },
    colorScheme: options.theme || "dark",
    reducedMotion: options.motion || "no-preference",
    serviceWorkers: "block",
  });
  try {
    const data = await setup(context, options);
    const detail = await callback(data);
    assert.deepEqual(data.state.unexpected, []);
    assert.deepEqual(data.state.pageErrors, []);
    results.push({ name, outcome: "observed", detail });
  } catch (failure) {
    const state = context.reviewState;
    const page = context.pages()[0];
    const focus =
      page && !page.isClosed()
        ? await page
            .evaluate(() => ({
              tag: document.activeElement?.tagName,
              role: document.activeElement?.getAttribute("role"),
              ariaLabel: document.activeElement?.getAttribute("aria-label"),
              insideDialog: !!document.activeElement?.closest("dialog[open]"),
            }))
            .catch(() => undefined)
        : undefined;
    results.push({
      name,
      outcome: "failed",
      check: String(failure.message).split("\n")[0].slice(0, 200),
      location: String(failure.stack)
        .split("\n")
        .filter((line) => /visual-review\.cjs:\d+/.test(line))
        .slice(0, 2),
      api: state?.api,
      unexpected: state?.unexpected,
      pageErrors: state?.pageErrors,
      failedResources: state?.failedResources,
      stage: state?.stage,
      focus,
    });
  } finally {
    await context.close();
    console.log(
      JSON.stringify({ case: name, outcome: results.at(-1)?.outcome }),
    );
  }
}
async function views(browser) {
  for (const width of [1440, 390])
    for (const theme of ["light", "dark"]) {
      await runCase(
        browser,
        `security-${width}-${theme}`,
        async ({ page }) => {
          await expect(
            page.getByRole("list", { name: "我的 Passkey" }),
          ).toBeVisible();
          await shot(page, `security-${width}-${theme}`);
          await button(page, "修改密码").scrollIntoViewIfNeeded();
          await expect(button(page, "修改密码")).toBeInViewport();
          await shot(page, `security-bottom-${width}-${theme}`);
          return noOverflow(page);
        },
        { width, theme },
      );
      await runCase(
        browser,
        `login-${width}-${theme}`,
        async ({ page }) => {
          await expect(field(page, "邮箱")).toBeEnabled();
          await expect(field(page, "密码")).toBeEnabled();
          await expect(button(page, "登录")).toBeEnabled();
          await shot(page, `login-${width}-${theme}`);
          return noOverflow(page);
        },
        { width, theme, route: "/login" },
      );
    }
  await runCase(
    browser,
    "email-dialog-1440-light",
    async ({ page }) => {
      await button(page, "修改邮箱").click();
      await expect(
        button(modal(page, "修改邮箱"), "使用 Passkey 验证"),
      ).toBeVisible();
      await shot(page, "email-dialog-1440-light");
      await page.keyboard.press("Escape");
      await expect(button(page, "修改邮箱")).toBeFocused();
      return { escapeFocus: true };
    },
    { width: 1440, theme: "light", route: "/settings/profile" },
  );
}
async function interactions(browser) {
  await runCase(
    browser,
    "system-nav-excludes-passkey",
    async ({ page }) => {
      await page.goto(origin + "/system/providers", {
        waitUntil: "domcontentloaded",
        timeout: 30000,
      });
      const nav = page.getByRole("navigation", {
        name: "系统设置",
        exact: true,
      });
      await expect(nav).toBeVisible();
      await expect(
        nav.getByRole("link", { name: /Passkey|登录与安全/ }),
      ).toHaveCount(0);
      return { systemNavigationUnchanged: true };
    },
    { width: 1440, theme: "light" },
  );
  await runCase(browser, "rename-delete-focus", async ({ page }) => {
    const rename = button(page, `重命名 ${keyLabel}`);
    await rename.click();
    const dialog = modal(page, "重命名 Passkey");
    await button(dialog, "使用 Passkey 验证").click();
    await expect(field(dialog, "Passkey 名称")).toBeVisible();
    await activeControl(page);
    await field(dialog, "Passkey 名称").fill("工作电脑");
    await button(dialog, "保存名称").click();
    await expect(dialog.getByRole("status")).toContainText("名称已更新");
    await button(dialog, "完成").click();
    await activeControl(page);
    const remove = button(page, "删除 工作电脑");
    await remove.click();
    await button(modal(page, "删除 Passkey"), "使用 Passkey 验证").click();
    await expect(button(modal(page, "删除 Passkey"), "确认删除")).toBeVisible();
    await shot(page, "delete-confirm-390-dark");
    await button(modal(page, "删除 Passkey"), "取消").click();
    await expect(remove).toBeFocused();
    await remove.click();
    await button(modal(page, "删除 Passkey"), "使用 Passkey 验证").click();
    await button(modal(page, "删除 Passkey"), "确认删除").click();
    await expect(modal(page, "删除 Passkey").getByRole("status")).toContainText(
      "已删除",
    );
    await button(modal(page, "删除 Passkey"), "完成").click();
    await expect(button(page, "添加 Passkey")).toBeFocused();
    return {
      nativeToSyntheticGrant: true,
      renameAndDeleteCancel: true,
      deletedTriggerFallbackFocus: true,
    };
  });
  await runCase(
    browser,
    "native-pending-cancel",
    async ({ page, state, native }) => {
      await native.cdp.send("WebAuthn.setAutomaticPresenceSimulation", {
        authenticatorId: native.authenticatorId,
        enabled: false,
      });
      await button(page, "添加 Passkey").click();
      const dialog = modal(page, "添加 Passkey");
      await button(dialog, "使用 Passkey 验证").click();
      await expect(button(dialog, "取消验证")).toBeVisible();
      await shot(page, "native-pending-390-dark");
      await button(dialog, "取消验证").click();
      await expect(button(dialog, "使用 Passkey 验证")).toBeEnabled();
      await expect(dialog.getByRole("alert")).toHaveCount(0);
      assert(!state.api.includes("POST /api/v1/me/passkeys/reauth/complete"));
      await button(dialog, "取消").click();
      await expect(button(page, "添加 Passkey")).toBeFocused();
      return {
        abortBeforeComplete: true,
        notBackendError: true,
        focusRestored: true,
      };
    },
  );
  await runCase(
    browser,
    "email-steps-cancel",
    async ({ page }) => {
      await button(page, "修改邮箱").click();
      const dialog = modal(page, "修改邮箱");
      await button(dialog, "使用 Passkey 验证").click();
      await expect(field(dialog, "新邮箱")).toBeVisible();
      await activeControl(page);
      await field(dialog, "新邮箱").fill("new-visual@example.invalid");
      await button(dialog, "发送验证码").click();
      await expect(dialog).toContainText("验证请求已受理");
      await expect(field(dialog, "新邮箱验证码")).toBeVisible();
      await expect(
        dialog.getByRole("button", { name: /秒后可重发/ }),
      ).toBeDisabled();
      await shot(page, "email-code-390-dark");
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(button(page, "修改邮箱")).toBeFocused();
      return {
        requestNotDelivery: true,
        rateLimitVisible: true,
        escapeFocus: true,
      };
    },
    { route: "/settings/profile" },
  );
}
async function errors(browser) {
  await runCase(browser, "rename-unknown-recovery", async ({ page, state }) => {
    await button(page, `重命名 ${keyLabel}`).click();
    const dialog = modal(page, "重命名 Passkey");
    await button(dialog, "使用 Passkey 验证").click();
    await field(dialog, "Passkey 名称").fill("恢复后电脑");
    const endpoint = `/api/v1/me/passkeys/${uuid(3)}`;
    state.failure = { endpoint, kind: "unknown" };
    await button(dialog, "保存名称").click();
    await expect(dialog.getByRole("alert")).toContainText("原操作结果尚未确认");
    await expect(field(dialog, "Passkey 名称")).toBeDisabled();
    await expect(button(dialog, "取消")).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();
    await shot(page, "rename-unknown-390-dark");
    await button(dialog, "重试原请求").click();
    await expect(dialog.getByRole("status")).toContainText("名称已更新");
    const requests = state.commands.filter(
      (command) => command.endpoint === endpoint,
    );
    assert.equal(requests.length, 2);
    assert(
      requests[0].key && requests[0].key === requests[1].key,
      "original retry key retained",
    );
    assert.equal(
      requests[0].body,
      requests[1].body,
      "original retry body retained",
    );
    await button(dialog, "完成").click();
    await expect(button(page, "重命名 恢复后电脑")).toBeFocused();
    return { unknownVisible: true, inputFrozen: true, originalRetry: true };
  });
  await runCase(browser, "rename-conflict-reread", async ({ page, state }) => {
    await button(page, `重命名 ${keyLabel}`).click();
    const dialog = modal(page, "重命名 Passkey");
    await button(dialog, "使用 Passkey 验证").click();
    await field(dialog, "Passkey 名称").fill("并发后电脑");
    state.failure = {
      endpoint: `/api/v1/me/passkeys/${uuid(3)}`,
      code: "VERSION_CONFLICT",
      status: 409,
    };
    state.keys.version = "2";
    await button(dialog, "保存名称").click();
    await expect(dialog).toContainText("已保留名称");
    await button(dialog, "读取最新列表").click();
    await button(dialog, "采用当前版本并重新验证").click();
    await button(dialog, "使用 Passkey 验证").click();
    await expect(field(dialog, "Passkey 名称")).toHaveValue("并发后电脑");
    await shot(page, "rename-conflict-recovered-390-dark");
    await button(dialog, "取消").click();
    return { conflictExplicit: true, keepsNameAfterReauthentication: true };
  });
  for (const kind of ["domainDisabled", "unsupported", "capabilityError"])
    await runCase(
      browser,
      `login-${kind}`,
      async ({ page }) => {
        await expect(button(page, "使用 Passkey 登录")).toBeDisabled();
        await expect(button(page, "登录")).toBeEnabled();
        await expect(field(page, "邮箱")).toBeEnabled();
        await shot(page, `login-${kind}`);
        return { passwordStillAvailable: true };
      },
      { route: "/login", [kind]: true },
    );
  await runCase(
    browser,
    "no-key-recovery-and-email-route",
    async ({ page }) => {
      await expect(
        page.getByText("尚未添加 Passkey。", { exact: true }),
      ).toBeVisible();
      await button(page, "添加 Passkey").click();
      const dialog = modal(page, "添加 Passkey");
      await expect(field(dialog, "当前密码")).toBeVisible();
      await shot(page, "first-key-recovery-390-dark");
      await button(dialog, "取消").click();
      await expect(button(page, "添加 Passkey")).toBeFocused();
      await page.goto(origin + "/settings/profile", {
        waitUntil: "domcontentloaded",
        timeout: 30000,
      });
      await button(page, "修改邮箱").click();
      await expect(modal(page, "修改邮箱")).toContainText(
        "修改邮箱前，请先添加 Passkey",
      );
      await button(modal(page, "修改邮箱"), "前往登录与安全").click();
      await expect(
        page.getByRole("heading", { name: "登录与安全", exact: true }),
      ).toBeVisible();
      return { firstKeyRequiresPasswordAndMail: true, emailRequiresKey: true };
    },
    { empty: true },
  );
  await runCase(
    browser,
    "recovery-code-field-error",
    async ({ page, state }) => {
      await button(page, "添加 Passkey").click();
      const dialog = modal(page, "添加 Passkey");
      await privateFill(
        field(dialog, "当前密码"),
        crypto.randomBytes(24).toString("base64url"),
      );
      await button(dialog, "发送验证码").click();
      await expect(field(dialog, "邮箱验证码")).toBeVisible();
      state.failure = {
        endpoint: "/api/v1/me/passkeys/recovery/confirm",
        fields: [{ path: "/code", code: "INVALID_OR_EXPIRED" }],
      };
      await privateFill(field(dialog, "邮箱验证码"), "123456");
      await button(dialog, "验证并继续").click();
      await expect(dialog.getByRole("alert")).toContainText(
        "验证码错误或已过期",
      );
      await privateFill(field(dialog, "邮箱验证码"), "");
      await shot(page, "recovery-code-error-390-dark");
      await expect(field(page, "新密码")).toBeVisible();
      return { invalidCodeStaysAuthenticated: true };
    },
    { empty: true },
  );
}
async function zoom() {
  const profile = await fs.mkdtemp(
    path.join(os.tmpdir(), "passkey-visual-zoom-"),
  );
  let context;
  try {
    await fs.mkdir(path.join(profile, "Default"));
    await fs.writeFile(
      path.join(profile, "Default/Preferences"),
      JSON.stringify({
        partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
      }),
    );
    context = await chromium.launchPersistentContext(profile, {
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      colorScheme: "dark",
      reducedMotion: "reduce",
      serviceWorkers: "block",
    });
    const { page, state } = await setup(context, {
      route: "/settings/profile",
    });
    const measured = await page.evaluate(() => ({
      outer: outerWidth,
      inner: innerWidth,
      dpr: devicePixelRatio,
      cssZoom: getComputedStyle(document.documentElement).zoom,
    }));
    assert.deepEqual(measured, {
      outer: 1440,
      inner: 720,
      dpr: 2,
      cssZoom: "1",
    });
    await button(page, "修改邮箱").click();
    await button(modal(page, "修改邮箱"), "使用 Passkey 验证").click();
    await expect(field(modal(page, "修改邮箱"), "新邮箱")).toBeVisible();
    await field(modal(page, "修改邮箱"), "新邮箱").fill("zoom@example.invalid");
    await button(modal(page, "修改邮箱"), "发送验证码").click();
    await expect(field(modal(page, "修改邮箱"), "新邮箱验证码")).toBeVisible();
    await button(modal(page, "修改邮箱"), "取消").scrollIntoViewIfNeeded();
    await button(modal(page, "修改邮箱"), "取消").focus();
    await page.keyboard.press("Tab");
    await expect(button(modal(page, "修改邮箱"), "关闭")).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect(button(modal(page, "修改邮箱"), "取消")).toBeFocused();
    assert(
      await page.evaluate(
        () => matchMedia("(prefers-reduced-motion: reduce)").matches,
      ),
    );
    await noOverflow(page);
    const cdp = await context.newCDPSession(page);
    const capture = await cdp.send("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
    });
    await fs.writeFile(
      path.join(output, "email-native-200.png"),
      Buffer.from(capture.data, "base64"),
    );
    await cdp.detach();
    await page.keyboard.press("Escape");
    await expect(button(page, "修改邮箱")).toBeFocused();
    assert.deepEqual(state.unexpected, []);
    assert.deepEqual(state.pageErrors, []);
    results.push({
      name: "native-200-percent-email",
      outcome: "observed",
      detail: measured,
    });
  } catch (failure) {
    results.push({
      name: "native-200-percent-email",
      outcome: "failed",
      check: String(failure.message).split("\n")[0],
    });
  } finally {
    if (context) await context.close();
    await fs.rm(profile, { recursive: true, force: true });
  }
}
async function main() {
  if (args.includes("--list")) {
    console.log(
      JSON.stringify({
        modes,
        requires: ["--base-url", "--owned passkey-ui-review", "--input"],
        boundary:
          "All APIs synthetic; native localhost CDP ceremony is not backend verification.",
      }),
    );
    return;
  }
  assert(modes.includes(mode));
  assert.equal(
    owned,
    "passkey-ui-review",
    "explicit owned review acknowledgement required",
  );
  const url = new URL(rawOrigin);
  assert(
    ["http:", "https:"].includes(url.protocol) &&
      url.hostname === "localhost" &&
      url.port &&
      !["5245", "18443"].includes(url.port) &&
      !url.username &&
      !url.password &&
      url.pathname === "/" &&
      !url.search &&
      !url.hash,
    "owned localhost origin required",
  );
  origin = url.origin;
  await fs.mkdir(output, { recursive: true });
  let browser;
  try {
    if (mode === "zoom") await zoom();
    else {
      browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: ["--no-sandbox"],
      });
      if (mode === "views") await views(browser);
      if (mode === "interactions") await interactions(browser);
      if (mode === "errors") await errors(browser);
    }
  } finally {
    if (browser) await browser.close();
  }
  const report = {
    mode,
    input,
    origin,
    results,
    browserClosed: true,
    boundary:
      "Synthetic API and task-owned ephemeral virtual authenticator. No real backend, mail, credentials or device verification.",
  };
  await fs.writeFile(
    path.join(output, `${mode}.json`),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report));
  if (!results.length || results.some((result) => result.outcome === "failed"))
    process.exitCode = 1;
}
main().catch(() => {
  console.error(
    "Passkey visual review failed before report; private diagnostics suppressed.",
  );
  process.exitCode = 1;
});
