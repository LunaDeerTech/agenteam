// Independent browser review. All API data is synthetic; unknown origins/routes fail closed.
// node provider-credential-delete-visual.cjs --base-url http://127.0.0.1:5281 --owned credential-delete-visual --input <revision> --run <label> [--only layout|recovery|partial|unavailable] [--check]
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const args = process.argv.slice(2);
const arg = (key) => args[args.indexOf(key) + 1];
const origin = arg("--base-url"),
  run = arg("--run");
assert.equal(arg("--owned"), "credential-delete-visual");
assert.equal(origin, "http://127.0.0.1:5281");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
  __dirname,
  "../../output/ai/provider-credential-delete-visual",
  run,
);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const pp = `/api/v1/system/model-providers/${id(1)}`;
const cp = `/api/v1/system/model-credentials/${id(30)}`;
const at = "2026-10-17T09:00:00.000000Z";
const btn = (p, name) => p.getByRole("button", { name, exact: true });
const dialog = (p) => p.getByRole("dialog", { name: "删除凭据", exact: true });
const panel = (p) => p.getByRole("tabpanel", { name: "配置", exact: true });
const secretInput = (p) =>
  panel(p).getByLabel("API 凭据（可选）", { exact: true });
const trigger = (p) => btn(panel(p), "删除凭据");
const puts = (s) => s.writes.filter((x) => x.method === "PUT");
const deletes = (s) => s.writes.filter((x) => x.method === "DELETE");
const gate = () => {
  let release;
  const promise = new Promise((r) => {
    release = r;
  });
  return { promise, release };
};
let stage = "prepare";

async function shot(p, name) {
  assert(
    await p
      .locator('input[type="password"]')
      .evaluateAll((nodes) => nodes.every((n) => !n.value)),
  );
  await p.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) =>
      requestAnimationFrame(() => requestAnimationFrame(r)),
    );
  });
  await p.screenshot({ path: path.join(out, name + ".png"), fullPage: false });
}
async function dimensions(p) {
  const value = await p.evaluate(() => ({
    width: innerWidth,
    root: document.documentElement.scrollWidth,
    main: document.querySelector(".settings-main").scrollWidth,
    client: document.querySelector(".settings-main").clientWidth,
  }));
  assert(
    value.root <= value.width && value.main <= value.client,
    JSON.stringify(value),
  );
  return value;
}
async function setup(c, options = {}) {
  const provider = {
    id: id(1),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: "OpenAI",
      protocol: options.readonly ? "anthropic-messages" : "openai-compatible",
      base_url: "https://provider.example.invalid/v1",
      enabled: true,
      credential_ref: options.empty ? null : id(30),
      options: {},
    },
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
    secretVersion: "1",
    secretDeleted: false,
    writes: [],
    receipts: new Map(),
    unexpected: [],
    errors: [],
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
    const reply = (body, status = 200) =>
      route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        body: JSON.stringify(body),
      });
    const error = (code, status) =>
      reply({ code, commit_state: "not_started", field_errors: [] }, status);
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
    if (method === "GET" && ep === pp) {
      if (s.failRead) {
        s.failRead = false;
        return error("INTERNAL_ERROR", 500);
      }
      return reply(provider);
    }
    if (method === "GET" && ep === cp)
      return s.secretDeleted
        ? error("NOT_FOUND", 404)
        : reply({
            credential_id: id(30),
            purpose: "model",
            version: s.secretVersion,
          });
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
    if (
      method === "POST" &&
      [
        "/api/v1/system/model-commands/lookup",
        "/api/v1/system/model-credential-commands/lookup",
      ].includes(ep)
    ) {
      const key = req.headers()["idempotency-key"],
        found = s.receipts.get(key),
        body = JSON.parse(req.postData());
      if (ep.endsWith("model-commands/lookup")) {
        assert.deepEqual(body, { command: "provider.update" });
        return reply({ found: !!found, receipt: found?.receipt ?? null });
      }
      assert.deepEqual(body, {
        kind: "delete",
        credential_id: id(30),
        expected_version: "1",
      });
      return reply({ observed: !!found, result: found?.receipt ?? null });
    }
    if ((method === "PUT" && ep === pp) || (method === "DELETE" && ep === cp)) {
      const body = JSON.parse(req.postData()),
        key = req.headers()["idempotency-key"];
      assert(key);
      s.writes.push({ method, ep, body, key });
      const previous = s.receipts.get(key);
      if (previous) {
        assert.equal(previous.method, method);
        assert.deepEqual(previous.body, body);
        if (method === "DELETE" && s.failReadAfterDelete) {
          s.failReadAfterDelete = false;
          s.failRead = true;
        }
        return reply(previous.receipt);
      }
      if (method === "PUT" && s.holdUnlink) {
        await s.holdUnlink.promise;
      }
      const behavior = method === "PUT" ? s.nextUnlink : s.nextDelete;
      if (method === "PUT") s.nextUnlink = undefined;
      else s.nextDelete = undefined;
      if (behavior === "referenced")
        return route.fulfill({
          status: 409,
          contentType: "application/problem+json",
          headers: { "Cache-Control": "no-store", "X-Request-ID": id(95) },
          body: JSON.stringify({
            type: "urn:agenteam:problem:resource-busy",
            title: "Resource busy",
            status: 409,
            detail: "The resource is currently busy.",
            instance: "/api/v1",
            code: "RESOURCE_BUSY",
            request_id: id(95),
            commit_state: "not_committed",
          }),
        });
      let receipt;
      if (method === "PUT") {
        assert.deepEqual(body, {
          expected_version: provider.version,
          input: { ...provider.input, credential_ref: null },
        });
        provider.input = structuredClone(body.input);
        provider.version = String(BigInt(provider.version) + 1n);
        receipt = {
          kind: "provider.update",
          resource_id: provider.id,
          version: provider.version,
          affected_references: "0",
        };
      } else {
        assert.deepEqual(body, { expected_version: s.secretVersion });
        assert.equal(provider.input.credential_ref, null);
        s.secretDeleted = true;
        s.secretVersion = String(BigInt(s.secretVersion) + 1n);
        receipt = {
          credential_id: id(30),
          purpose: "model",
          version: s.secretVersion,
          deleted: true,
        };
        if (behavior !== "unknown" && s.failReadAfterDelete) {
          s.failReadAfterDelete = false;
          s.failRead = true;
        }
      }
      s.receipts.set(key, { method, body: structuredClone(body), receipt });
      if (behavior === "unknown") return route.abort("connectionfailed");
      return reply(receipt);
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", () => s.errors.push("pageerror"));
  await p.goto(origin + `/system/models/${id(1)}?tab=config`, {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(secretInput(p)).toBeVisible();
  return { p, s };
}
async function openDelete(p) {
  await trigger(p).click();
  await expect(btn(dialog(p), "确认删除")).toBeEnabled();
}
async function layout({ p, s }) {
  stage = "desktop-entry-and-cancel";
  await expect(
    p.getByRole("checkbox", { name: "解除凭据关联（保留凭据）", exact: true }),
  ).toHaveCount(0);
  await expect(trigger(p)).toBeEnabled();
  const geometry = await panel(p).evaluate((node) => {
    const a = node
        .querySelector('input[type="password"]')
        .getBoundingClientRect(),
      b = [...node.querySelectorAll("button")]
        .find((n) => n.textContent.trim() === "删除凭据")
        .getBoundingClientRect();
    return {
      inputRight: a.right,
      buttonLeft: b.left,
      delta: a.y + a.height / 2 - (b.y + b.height / 2),
    };
  });
  assert(
    geometry.buttonLeft >= geometry.inputRight && Math.abs(geometry.delta) <= 1,
    JSON.stringify(geometry),
  );
  await shot(p, "desktop-config");
  await btn(p, "更多").click();
  await expect(p.getByRole("menuitem", { name: /凭据/ })).toHaveCount(0);
  await p.keyboard.press("Escape");
  await openDelete(p);
  await shot(p, "desktop-confirm");
  await p.keyboard.press("Escape");
  await expect(dialog(p)).toHaveCount(0);
  await expect(trigger(p)).toBeFocused();
  assert.equal(s.writes.length, 0);
  return {
    cancelZeroWrites: true,
    cancelFocus: true,
    singleEntry: true,
    geometry,
    ...(await dimensions(p)),
  };
}
async function recovery({ p, s }) {
  stage = "unlink-pending";
  s.nextUnlink = "unknown";
  s.nextDelete = "unknown";
  s.holdUnlink = gate();
  await openDelete(p);
  await btn(dialog(p), "确认删除").click();
  try {
    await expect.poll(() => puts(s).length).toBe(1);
    await expect(btn(dialog(p), "确认删除")).toBeDisabled();
    await expect(secretInput(p)).toBeDisabled();
    await expect(
      p.getByRole("tab", { name: "模型", exact: true }),
    ).toBeDisabled();
    await p.keyboard.press("Escape");
    await expect(dialog(p)).toBeVisible();
    assert.equal(deletes(s).length, 0);
  } finally {
    s.holdUnlink.release();
  }
  stage = "unlink-unknown-does-not-delete";
  await expect(btn(dialog(p), "查询原操作")).toBeVisible();
  assert.equal(deletes(s).length, 0);
  await btn(dialog(p), "查询原操作").click();
  await expect(dialog(p)).toContainText("已找到历史回执");
  assert.equal(deletes(s).length, 0);
  await btn(dialog(p), "确认原请求").click();
  stage = "delete-unknown-same-command";
  await expect(dialog(p)).toContainText("关联已解除，凭据删除结果尚未确认。");
  assert.equal(puts(s).length, 2);
  assert.equal(deletes(s).length, 1);
  assert.equal(puts(s)[0].key, puts(s)[1].key);
  assert.deepEqual(puts(s)[0].body, puts(s)[1].body);
  await expect(
    dialog(p).getByText("当前供应商已不使用此凭据，凭据尚未删除。", {
      exact: true,
    }),
  ).toHaveCount(0);
  await shot(p, "delete-unknown-desktop");
  await p.keyboard.press("Escape");
  await expect(dialog(p)).toBeVisible();
  await btn(dialog(p), "查询原操作").click();
  await expect(dialog(p)).toContainText("已找到历史回执");
  assert.equal(deletes(s).length, 1);
  s.failReadAfterDelete = true;
  await btn(dialog(p), "确认原请求").click();
  stage = "confirmed-delete-read-failure";
  await expect(dialog(p)).toContainText("删除已完成，最新供应商配置尚未读回。");
  assert.equal(puts(s).length, 2);
  assert.equal(deletes(s).length, 2);
  assert.equal(deletes(s)[0].key, deletes(s)[1].key);
  assert.deepEqual(deletes(s)[0].body, deletes(s)[1].body);
  await expect(btn(dialog(p), "完成")).toBeDisabled();
  await btn(dialog(p), "重新读取").click();
  await expect(btn(dialog(p), "完成")).toBeEnabled();
  assert.equal(puts(s).length, 2);
  assert.equal(deletes(s).length, 2);
  await btn(dialog(p), "完成").click();
  await expect(dialog(p)).toHaveCount(0);
  await expect(secretInput(p)).toBeFocused();
  await expect(secretInput(p)).toHaveValue("");
  await expect(trigger(p)).toBeDisabled();
  return {
    pendingLocked: true,
    unlinkUnknownNoDelete: true,
    originalRequestsOnly: true,
    finalReadOnlyGET: true,
    completedFocus: true,
  };
}
async function partial({ p, s }) {
  stage = "narrow-layout";
  await shot(p, "narrow-config");
  await dimensions(p);
  stage = "partial-close-and-continue";
  s.nextDelete = "referenced";
  await openDelete(p);
  await btn(dialog(p), "确认删除").click();
  await expect(dialog(p)).toContainText(
    "当前供应商已不使用此凭据，凭据尚未删除。",
  );
  await expect(btn(dialog(p), "关闭")).toBeEnabled();
  assert.equal(puts(s).length, 1);
  assert.equal(deletes(s).length, 1);
  assert.equal(s.secretDeleted, false);
  await shot(p, "narrow-partial");
  await btn(dialog(p), "关闭").click();
  await expect(dialog(p)).toHaveCount(0);
  await expect(trigger(p)).toBeEnabled();
  await expect(trigger(p)).toBeFocused();
  await trigger(p).click();
  await expect(btn(dialog(p), "继续删除")).toBeEnabled();
  assert.equal(puts(s).length, 1);
  assert.equal(deletes(s).length, 1);
  stage = "partial-explicit-abandon";
  await btn(dialog(p), "结束删除…").click();
  await expect(btn(dialog(p), "结束删除")).toBeVisible();
  await btn(dialog(p), "结束删除").focus();
  await p.keyboard.press("Enter");
  await expect(dialog(p)).toHaveCount(0);
  await expect(secretInput(p)).toBeFocused();
  await expect(trigger(p)).toBeDisabled();
  assert.equal(puts(s).length, 1);
  assert.equal(deletes(s).length, 1);
  assert.equal(s.secretDeleted, false);
  assert.equal(s.provider.input.credential_ref, null);
  return {
    partialResumable: true,
    explicitAbandonNoRollback: true,
    focus: true,
    ...(await dimensions(p)),
  };
}
async function unavailable({ p, s }) {
  stage = "unavailable-delete";
  await expect(trigger(p)).toBeDisabled();
  assert.equal(s.writes.length, 0);
  return { deleteDisabled: true, ...(await dimensions(p)) };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(
    !only || ["layout", "recovery", "partial", "unavailable"].includes(only),
  );
  const results = [],
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
  try {
    for (const [name, options, work] of [
      ["layout", {}, layout],
      ["recovery", {}, recovery],
      ["partial", { narrow: true }, partial],
      ["unavailable", { narrow: true, empty: true }, unavailable],
      ["unavailable", { narrow: true, readonly: true }, unavailable],
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
          detail = await work(data);
        assert.deepEqual(data.s.unexpected, []);
        assert.deepEqual(data.s.errors, []);
        results.push({ name, options, outcome: "observed", detail });
      } catch (e) {
        const p = c.pages().at(-1);
        if (p && !p.isClosed())
          await shot(p, "failure-" + name).catch(() => {});
        results.push({
          name,
          options,
          outcome: "failed",
          stage,
          check: String(e.message).split("\n")[0],
          api: c.review?.api,
          unexpected: c.review?.unexpected,
          focus:
            p && !p.isClosed()
              ? await p.evaluate(() => ({
                  tag: document.activeElement?.tagName,
                  role: document.activeElement?.getAttribute("role"),
                  label: document.activeElement?.getAttribute("aria-label"),
                }))
              : undefined,
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
        boundary: "Synthetic API only, not a new real-backend acceptance",
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
