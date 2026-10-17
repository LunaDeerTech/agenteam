// Independent display/write-semantic checks. All API traffic is synthetic and fail-closed.
// node configured-secret-visual.cjs --base-url http://127.0.0.1:5281 --owned configured-secret-visual --input <revision> --run <label> [--only provider|smtp|empty-provider|empty-smtp] [--check]
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const args = process.argv.slice(2),
  arg = (n) => args[args.indexOf(n) + 1];
const origin = arg("--base-url"),
  run = arg("--run");
assert.equal(origin, "http://127.0.0.1:5281");
assert.equal(arg("--owned"), "configured-secret-visual");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
  __dirname,
  "../../output/ai/configured-secret-visual",
  run,
);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const pp = `/api/v1/system/model-providers/${id(1)}`,
  cp = `/api/v1/system/model-credentials/${id(30)}`;
const at = "2026-10-17T09:00:00.000000Z",
  dots = "••••••••";
const replacement = "SYNTHETIC_ONLY_DISPLAY_CHECK_REPLACEMENT";
const btn = (p, n) => p.getByRole("button", { name: n, exact: true });
const input = (p, smtp) =>
  p.getByLabel(smtp ? "密码" : "API 凭据（可选）", { exact: true });
let stage = "prepare";
async function shot(p, name) {
  assert(
    await p
      .locator('input[type="password"]')
      .evaluateAll((nodes) => nodes.every((n) => !n.value)),
    "capture only empty real inputs",
  );
  await p.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) =>
      requestAnimationFrame(() => requestAnimationFrame(r)),
    );
  });
  await p.screenshot({ path: path.join(out, name + ".png"), fullPage: false });
}
async function bounds(p) {
  const v = await p.evaluate(() => ({
    inner: innerWidth,
    root: document.documentElement.scrollWidth,
    main: document.querySelector(".settings-main").scrollWidth,
    client: document.querySelector(".settings-main").clientWidth,
  }));
  assert(v.root <= v.inner && v.main <= v.client, "horizontal overflow");
  return v;
}
async function setup(c, options) {
  const provider = {
    id: id(1),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: "OpenAI",
      protocol: "openai-compatible",
      base_url: "https://provider.example.invalid/v1",
      enabled: true,
      credential_ref: options.empty ? null : id(30),
      options: {},
    },
  };
  const smtp = {
    id: id(40),
    version: "1",
    configured: true,
    host: "smtp.example.invalid",
    port: 587,
    encryption: "starttls",
    username: options.empty ? "" : "synthetic-account",
    sender_email: "sender@example.invalid",
    sender_name: "合成邮件服务",
    credential_present: !options.empty,
    auto_retry_count: "2",
    retry_interval_seconds: "30",
  };
  const user = {
    id: id(90),
    email: "visual@example.invalid",
    username: "visual-review",
    display_name: "合成视觉审查",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  };
  const s = {
    provider,
    smtp,
    secretVersion: "1",
    secretDeleted: false,
    writes: [],
    errors: [],
    unexpected: [],
    api: [],
  };
  c.review = s;
  await c.routeWebSocket("**/*", (ws) => ws.close());
  await c.route("**/*", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      ep = url.pathname,
      method = req.method();
    if (url.origin !== origin) {
      s.unexpected.push("outside-origin");
      return route.abort("blockedbyclient");
    }
    if (!ep.startsWith("/api/")) {
      if (method === "GET" && !["xhr", "fetch"].includes(req.resourceType()))
        return route.continue();
      s.unexpected.push("non-static");
      return route.abort("blockedbyclient");
    }
    s.api.push(method + " " + ep);
    const reply = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (method === "GET" && ep === "/api/v1/session")
      return reply({
        user,
        session: {
          id: id(91),
          issued_at: at,
          absolute_expires_at: "2026-10-19T12:00:00.000000Z",
          idle_expires_at: "2026-10-19T12:00:00.000000Z",
        },
        csrf_token: "synthetic-visual-context",
      });
    if (method === "GET" && ep === "/api/v1/me")
      return reply({ user, avatar: null });
    if (method === "GET" && ep === pp) return reply(provider);
    if (method === "GET" && ep === cp) {
      assert(!s.secretDeleted);
      return reply({
        credential_id: id(30),
        purpose: "model",
        version: s.secretVersion,
      });
    }
    if (method === "GET" && ep === "/api/v1/system/models")
      return reply({ items: [], next_cursor: null });
    if (method === "GET" && ep === "/api/v1/system/model-capabilities")
      return reply({
        protocols: [
          {
            protocol: "openai-compatible",
            model_types: ["chat", "embedding"],
            discovery: true,
            test: true,
          },
        ],
      });
    if (method === "GET" && ep === "/api/v1/system/smtp") return reply(smtp);
    if (
      (method === "PUT" && [pp, cp, "/api/v1/system/smtp"].includes(ep)) ||
      (method === "DELETE" && ep === cp)
    ) {
      const body = JSON.parse(req.postData());
      assert(req.headers()["idempotency-key"]);
      s.writes.push({ method, ep, body });
      assert(
        !JSON.stringify(body).includes(dots),
        "visual dots must never be submitted",
      );
      if (ep === pp) {
        assert.equal(body.expected_version, provider.version);
        provider.version = String(BigInt(provider.version) + 1n);
        provider.input = structuredClone(body.input);
        return reply({
          kind: "provider.update",
          resource_id: provider.id,
          version: provider.version,
          affected_references: "0",
        });
      }
      if (ep === cp) {
        assert.equal(body.expected_version, s.secretVersion);
        if (method === "PUT")
          assert(
            body.value === replacement,
            "only the explicit replacement may be sent",
          );
        else {
          assert.equal(provider.input.credential_ref, null);
          s.secretDeleted = true;
        }
        s.secretVersion = String(BigInt(s.secretVersion) + 1n);
        return reply({
          credential_id: id(30),
          purpose: "model",
          version: s.secretVersion,
          deleted: method === "DELETE",
        });
      }
      assert.equal(body.version, smtp.version);
      if (Object.hasOwn(body, "password"))
        assert(
          body.password === replacement,
          "only the explicit replacement may be sent",
        );
      for (const name of [
        "host",
        "port",
        "encryption",
        "username",
        "sender_email",
        "sender_name",
        "auto_retry_count",
        "retry_interval_seconds",
      ])
        smtp[name] = body[name];
      if (body.credential_action === "remove") smtp.credential_present = false;
      else if (body.password) smtp.credential_present = true;
      smtp.version = String(BigInt(smtp.version) + 1n);
      return reply({ applied_version: smtp.version, settings: smtp });
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", (e) =>
    s.errors.push({ name: e.name, message: e.message.slice(0, 250) }),
  );
  await p.goto(
    origin +
      (options.smtp ? "/system/smtp" : `/system/models/${id(1)}?tab=config`),
    { waitUntil: "domcontentloaded", timeout: 30000 },
  );
  await expect(input(p, options.smtp)).toBeVisible();
  return { p, s };
}
async function masked(control, configured) {
  await expect(control).toHaveValue("");
  if (configured) await expect(control).toHaveAttribute("placeholder", dots);
  else await expect(control).not.toHaveAttribute("placeholder", dots);
  if (configured) await expect(control).toHaveAccessibleDescription(/已配置/);
  assert.equal(await control.evaluate((e) => e.defaultValue), "");
}
async function editChecks(control) {
  await control.focus();
  await expect(control).toBeFocused();
  await masked(control, true);
  await control.press("Tab");
  await control.page().keyboard.press("Shift+Tab");
  await expect(control).toBeFocused();
  await control.fill(replacement);
  await expect(control).not.toHaveAttribute("placeholder", dots);
  await control.fill("");
  await masked(control, true);
}
async function provider({ p, s }) {
  stage = "provider-display";
  const password = input(p, false);
  await masked(password, true);
  await shot(p, "provider-configured-dark");
  await editChecks(password);
  stage = "provider-empty-save";
  await p
    .getByLabel("API 端点", { exact: true })
    .fill("https://provider.example.invalid/review");
  await btn(p, "保存").click();
  await expect(p.getByText("供应商已保存。", { exact: true })).toBeVisible();
  assert.equal(s.writes.filter((w) => w.ep === cp).length, 0);
  assert.equal(s.provider.input.credential_ref, id(30));
  await masked(password, true);
  stage = "provider-explicit-replacement";
  await password.fill(replacement);
  await btn(p, "保存").click();
  await expect(p.getByText("供应商已保存。", { exact: true })).toBeVisible();
  assert.equal(
    s.writes.filter((w) => w.ep === cp && w.method === "PUT").length,
    1,
  );
  await masked(password, true);
  stage = "provider-after-delete-display";
  await btn(p, "删除凭据").click();
  const d = p.getByRole("dialog", { name: "删除凭据", exact: true });
  await expect(btn(d, "确认删除")).toBeEnabled();
  await btn(d, "确认删除").click();
  await expect(btn(d, "完成")).toBeEnabled();
  await btn(d, "完成").click();
  await expect(d).toHaveCount(0);
  await masked(password, false);
  await expect(password).toBeFocused();
  await expect(btn(p, "删除凭据")).toBeDisabled();
  return {
    displayOnly: true,
    emptyKeep: true,
    explicitReplacement: true,
    deleteClearsDisplay: true,
    ...(await bounds(p)),
  };
}
async function smtp({ p, s }) {
  stage = "smtp-display";
  const password = input(p, true);
  await masked(password, true);
  await password.scrollIntoViewIfNeeded();
  await shot(p, "smtp-configured-light");
  await editChecks(password);
  stage = "smtp-empty-save";
  await p.getByLabel("发件人显示名", { exact: true }).fill("合成邮件验证");
  await btn(p, "保存配置").click();
  await expect(
    p.getByRole("status").filter({ hasText: "SMTP 配置已保存" }),
  ).toBeVisible();
  assert.equal(s.writes.length, 1);
  assert.equal(s.writes[0].body.credential_action, "keep");
  assert(!Object.hasOwn(s.writes[0].body, "password"));
  await masked(password, true);
  stage = "smtp-explicit-replacement";
  await password.fill(replacement);
  await btn(p, "保存配置").click();
  await expect(
    p.getByRole("status").filter({ hasText: "SMTP 配置已保存" }),
  ).toBeVisible();
  assert.equal(s.writes.length, 2);
  assert(s.writes[1].body.password === replacement);
  await masked(password, true);
  stage = "smtp-remove-display";
  await btn(p, "移除已保存的认证").click();
  await btn(p.getByRole("dialog"), "确认移除认证").click();
  await expect(
    p.getByRole("status").filter({ hasText: "已保存的认证账号与密码已移除" }),
  ).toBeVisible();
  await masked(password, false);
  await password.focus();
  await expect(password).toBeFocused();
  assert.equal(s.smtp.credential_present, false);
  assert.equal(s.writes[2].body.credential_action, "remove");
  assert(!Object.hasOwn(s.writes[2].body, "password"));
  return {
    displayOnly: true,
    emptyKeep: true,
    explicitReplacement: true,
    removalClearsDisplay: true,
    ...(await bounds(p)),
  };
}
async function empty({ p, s }, options) {
  stage = "initially-empty";
  const control = input(p, options.smtp);
  await masked(control, false);
  await control.focus();
  await expect(control).toBeFocused();
  assert.equal(s.writes.length, 0);
  return { emptyValueAndDisplay: true, focus: true, ...(await bounds(p)) };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(
    !only ||
      ["provider", "smtp", "empty-provider", "empty-smtp"].includes(only),
  );
  const results = [],
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
  try {
    for (const [name, options, work] of [
      ["provider", {}, provider],
      ["smtp", { smtp: true, narrow: true }, smtp],
      ["empty-provider", { empty: true, narrow: true }, empty],
      ["empty-smtp", { smtp: true, empty: true }, empty],
    ]) {
      if (only && only !== name) continue;
      const c = await browser.newContext({
        viewport: options.narrow
          ? { width: 390, height: 844 }
          : { width: 1440, height: 1000 },
        colorScheme: options.narrow ? "light" : "dark",
        serviceWorkers: "block",
      });
      try {
        stage = "load";
        const data = await setup(c, options),
          detail = await work(data, options);
        assert.deepEqual(data.s.unexpected, []);
        assert.deepEqual(data.s.errors, []);
        results.push({ name, outcome: "observed", detail });
      } catch (e) {
        const p = c.pages().at(-1);
        if (p && !p.isClosed())
          await shot(p, "failure-" + name).catch(() => {});
        results.push({
          name,
          outcome: "failed",
          stage,
          check: String(e.message).split("\n")[0],
          location: String(e.stack)
            .split("\n")
            .filter((x) => x.includes("configured-secret-visual.cjs:"))
            .slice(0, 2),
          api: c.review?.api,
          errors: c.review?.errors,
          unexpected: c.review?.unexpected,
        });
      } finally {
        await c.close();
        console.log(JSON.stringify(results.at(-1)));
      }
    }
  } finally {
    await browser.close();
  }
  await fs.writeFile(
    path.join(out, "report.json"),
    JSON.stringify(
      {
        input: arg("--input"),
        boundary:
          "Synthetic UI responses; no real provider/SMTP service or credentials",
        results,
        browserClosed: true,
      },
      null,
      2,
    ),
  );
  if (results.some((r) => r.outcome === "failed")) process.exitCode = 1;
}
if (!args.includes("--check"))
  main().catch((e) => {
    console.error(String(e.message).split("\n")[0]);
    process.exitCode = 1;
  });
