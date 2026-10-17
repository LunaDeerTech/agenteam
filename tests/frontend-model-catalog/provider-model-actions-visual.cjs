// Independent controlled review of supplier-detail model actions. No real backend or provider.
// node provider-model-actions-visual.cjs --base-url http://127.0.0.1:5281 --owned provider-model-actions-visual --input <revision> --run <label> [--only layout|toggle|edit] [--check]
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict"),
  fs = require("node:fs/promises"),
  path = require("node:path");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const args = process.argv.slice(2),
  arg = (key) => args[args.indexOf(key) + 1];
const origin = arg("--base-url"),
  run = arg("--run");
assert.equal(origin, "http://127.0.0.1:5281");
assert.equal(arg("--owned"), "provider-model-actions-visual");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
  __dirname,
  "../../output/ai/provider-model-actions-visual",
  run,
);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`,
  at = "2026-10-17T09:00:00.000000Z";
const pp = `/api/v1/system/model-providers/${id(1)}`,
  mp = `/api/v1/system/models/${id(10)}`;
const btn = (p, n) => p.getByRole("button", { name: n, exact: true });
const table = (p) => p.getByRole("table", { name: "供应商模型", exact: true });
const row = (p, modelId = "sample-chat") =>
  table(p)
    .getByRole("row")
    .filter({ has: p.getByRole("cell", { name: modelId, exact: true }) });
const sw = (p) => row(p).getByRole("switch");
const writes = (s) => s.requests.filter((r) => r.method === "PUT");
const tests = (s) => s.requests.filter((r) => r.ep.endsWith("/test"));
const gate = () => {
  let release;
  const promise = new Promise((resolve) => {
    release = resolve;
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
  const v = await p.evaluate(() => ({
    inner: innerWidth,
    root: document.documentElement.scrollWidth,
    main: document.querySelector(".settings-main").scrollWidth,
    client: document.querySelector(".settings-main").clientWidth,
  }));
  assert(v.root <= v.inner && v.main <= v.client, "page horizontal overflow");
  return v;
}
function sampleModel(n, type, modelId) {
  return {
    id: id(n),
    provider_id: id(1),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: type === "chat" ? "团队对话模型" : "项目向量模型",
      provider_model_id: modelId,
      type,
      enabled: true,
      parameters: { temperature: 0.2 },
      request_overwrite: { seed: 7 },
      header_overwrite: { "X-Synthetic": "model-review" },
      capabilities: {
        tool_calls: false,
        parallel_tool_calls: false,
        streaming: true,
        reasoning: false,
        input_modalities: ["text"],
        output_modalities: [type === "chat" ? "text" : "vector"],
        reasoning_efforts: [],
        structured_output_modes: type === "chat" ? ["json_schema"] : [],
        context_length: "8192",
        max_output: "1024",
        embedding:
          type === "embedding"
            ? { dimensions: "1536", dimensions_mode: "fixed" }
            : null,
      },
    },
  };
}
async function setup(c) {
  const provider = {
    id: id(1),
    version: "3",
    created_at: at,
    updated_at: at,
    input: {
      name: "OpenAI",
      protocol: "openai-compatible",
      base_url: "https://provider.example.invalid/v1",
      enabled: true,
      credential_ref: null,
      options: {},
    },
  };
  const models = [
    sampleModel(10, "chat", "sample-chat"),
    sampleModel(11, "embedding", "sample-embedding"),
  ];
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
    models,
    requests: [],
    api: [],
    errors: [],
    unexpected: [],
    receipts: new Map(),
    testRuns: 0,
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
    const reply = (value, status = 200) =>
      route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        body: JSON.stringify(value),
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
    if (method === "GET" && ep === "/api/v1/system/models") {
      if (s.failListRead) return route.abort("connectionfailed");
      return reply({
        items: models.map((model) => ({
          ...model,
          provider: {
            id: provider.id,
            name: provider.input.name,
            protocol: provider.input.protocol,
            enabled: provider.input.enabled,
            version: provider.version,
          },
        })),
        next_cursor: "synthetic-model-page-2",
      });
    }
    if (method === "GET" && ep === mp + "/deletion-impact")
      return reply({
        model_id: models[0].id,
        version: models[0].version,
        reference_count: "0",
        reference_groups: [],
        replacement_requirement: "none",
        delete_blocker: null,
      });
    if (method === "GET" && ep === mp) {
      if (s.failModelRead) return route.abort("connectionfailed");
      return reply(models[0]);
    }
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
    if (method === "POST" && ep === "/api/v1/system/model-commands/lookup") {
      assert.deepEqual(JSON.parse(req.postData()), { command: "model.update" });
      const found = s.receipts.get(req.headers()["idempotency-key"]);
      return reply({ found: !!found, receipt: found?.receipt ?? null });
    }
    if (
      (method === "PUT" && ep === mp) ||
      (method === "POST" && ep === mp + "/test")
    ) {
      const key = req.headers()["idempotency-key"],
        body = JSON.parse(req.postData());
      assert(key);
      s.requests.push({ key, method, ep, body });
      const previous = s.receipts.get(key);
      if (previous) {
        assert.equal(previous.ep, ep);
        assert.deepEqual(previous.body, body);
        return reply(previous.receipt);
      }
      if (method === "PUT" && s.holdWrite) await s.holdWrite.promise;
      const behavior = method === "PUT" ? s.nextWrite : s.nextTest;
      if (method === "PUT") s.nextWrite = undefined;
      else s.nextTest = undefined;
      if (behavior === "reject")
        return reply(
          {
            type: "urn:agenteam:problem:invalid-argument",
            title: "Invalid argument",
            status: 400,
            detail: "The request is invalid.",
            instance: "/api/v1",
            code: "INVALID_ARGUMENT",
            request_id: id(95),
            commit_state: "not_started",
          },
          400,
        );
      let receipt;
      if (method === "PUT") {
        assert.equal(body.expected_version, models[0].version);
        assert.deepEqual(Object.keys(body).sort(), [
          "expected_version",
          "input",
        ]);
        if (s.toggleIntent)
          assert.deepEqual(body.input, {
            ...models[0].input,
            enabled: !models[0].input.enabled,
          });
        models[0] = {
          ...models[0],
          version: String(BigInt(models[0].version) + 1n),
          input: structuredClone(body.input),
        };
        receipt = {
          kind: "model.update",
          resource_id: id(10),
          version: models[0].version,
          affected_references: "0",
        };
      } else {
        assert.deepEqual(body, {
          expected_version: models[0].version,
          provider_version: provider.version,
          input: { text: "请简要说明收到的内容。", attachments: [] },
        });
        s.testRuns++;
        receipt = {
          operation_id: id(60),
          provider_id: provider.id,
          provider_version: provider.version,
          model_id: models[0].id,
          model_version: models[0].version,
          outcome: "succeeded",
          reason: null,
          duration_ms: 57,
          created_at: at,
          answer_text: "这是合成的文本测试回答。",
          answer_truncated: false,
        };
      }
      s.receipts.set(key, { ep, body: structuredClone(body), receipt });
      if (behavior === "unknown") return route.abort("connectionfailed");
      return reply(receipt);
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", (e) =>
    s.errors.push({ name: e.name, message: e.message.slice(0, 250) }),
  );
  await p.goto(origin + "/system/models/" + id(1), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(table(p)).toBeVisible();
  return { p, s };
}
async function layout({ p, s }) {
  stage = "desktop-list";
  await expect(
    table(p).getByRole("columnheader", { name: "名称", exact: true }),
  ).toHaveCount(0);
  await expect(row(p)).toBeVisible();
  await expect(row(p, "sample-embedding")).toBeVisible();
  await expect(
    table(p).getByText("OpenAI/sample-chat", { exact: true }),
  ).toHaveCount(0);
  await expect(row(p).getByText("对话", { exact: true })).toBeVisible();
  await expect(
    row(p, "sample-embedding").getByText("向量", { exact: true }),
  ).toBeVisible();
  await expect(sw(p)).toHaveAttribute("aria-checked", "true");
  await expect(btn(row(p), "编辑")).toBeVisible();
  await expect(btn(row(p), "测试")).toBeVisible();
  const centers = [];
  for (const modelId of ["sample-chat", "sample-embedding"]) {
    const r = row(p, modelId);
    const boxes = await Promise.all([
      r.getByRole("switch").boundingBox(),
      btn(r, "编辑").boundingBox(),
      btn(r, "测试").boundingBox(),
    ]);
    const ys = boxes.map((b) => b.y + b.height / 2);
    const delta = Math.max(...ys) - Math.min(...ys);
    assert(delta <= 0.5, `switch/action center mismatch: ${delta}px`);
    centers.push({ modelId, delta });
  }
  await shot(p, "models-desktop-dark");
  assert.equal(s.requests.length, 0);
  return {
    centers,
    bareIDs: true,
    typeLabels: true,
    directActions: true,
    noAutomaticWritesOrTests: true,
    ...(await dimensions(p)),
  };
}
async function toggle({ p, s }) {
  stage = "narrow-list";
  await shot(p, "models-narrow-light");
  await dimensions(p);
  await expect(btn(p, "下一页")).toBeEnabled();
  await expect(p.getByLabel("每页条数", { exact: true })).toBeEnabled();
  s.toggleIntent = true;
  s.holdWrite = gate();
  stage = "toggle-pending";
  await sw(p).focus();
  await p.keyboard.press("Space");
  try {
    await expect.poll(() => writes(s).length).toBe(1);
    await expect(btn(row(p), "编辑")).toBeDisabled();
    await expect(btn(row(p), "测试")).toBeDisabled();
    await expect(sw(p)).toHaveAttribute("aria-checked", "true");
  } finally {
    s.holdWrite.release();
  }
  await expect(sw(p)).toHaveAttribute("aria-checked", "false");
  await expect(sw(p)).toBeEnabled();
  await expect(sw(p)).toBeFocused();
  assert.equal(s.models[0].input.enabled, false);
  stage = "toggle-known-failure";
  s.nextWrite = "reject";
  await sw(p).click();
  await expect(row(p).getByRole("alert")).toContainText("请检查填写内容");
  await expect(sw(p)).toHaveAttribute("aria-checked", "false");
  await btn(row(p), "取消更改").click();
  await expect(sw(p)).toBeEnabled();
  await expect(sw(p)).toBeFocused();
  stage = "toggle-unknown";
  s.nextWrite = "unknown";
  await sw(p).click();
  await expect(btn(row(p), "查询原操作")).toBeVisible();
  await expect(sw(p)).toHaveAttribute("aria-checked", "false");
  await expect(btn(row(p), "编辑")).toBeDisabled();
  await expect(
    p.getByRole("tab", { name: "配置", exact: true }),
  ).toBeDisabled();
  await expect(btn(p, "下一页")).toBeDisabled();
  await expect(p.getByLabel("每页条数", { exact: true })).toBeDisabled();
  await expect(btn(row(p, "sample-embedding"), "编辑")).toBeDisabled();
  await expect(row(p, "sample-embedding").getByRole("switch")).toBeDisabled();
  await shot(p, "toggle-unknown-narrow");
  await dimensions(p);
  await btn(row(p), "查询原操作").click();
  await expect(row(p)).toContainText("已找到历史回执");
  assert.equal(writes(s).length, 3);
  s.failModelRead = true;
  await btn(row(p), "确认原请求").click();
  stage = "toggle-confirmed-read-failure";
  await expect(row(p)).toContainText("状态已保存，最新配置尚未读回。");
  await expect(sw(p)).toBeDisabled();
  await expect(
    p.getByRole("tab", { name: "配置", exact: true }),
  ).toBeDisabled();
  assert.equal(writes(s).length, 4);
  s.failModelRead = false;
  await btn(row(p), "重新读取").click();
  await expect(sw(p)).toHaveAttribute("aria-checked", "true");
  await expect(p.getByRole("tab", { name: "配置", exact: true })).toBeEnabled();
  await expect(btn(p, "下一页")).toBeEnabled();
  await expect(sw(p)).toBeFocused();
  assert.equal(writes(s).length, 4);
  assert.equal(writes(s)[2].key, writes(s)[3].key);
  assert.deepEqual(writes(s)[2].body, writes(s)[3].body);
  assert.equal(tests(s).length, 0);
  return {
    completeInputPreserved: true,
    pendingLocked: true,
    rejectedCancelable: true,
    unknownOriginalRequest: true,
    ownerSurvivesPaginationTabLock: true,
    confirmedReadFailureGETOnly: true,
    focus: true,
    ...(await dimensions(p)),
  };
}
async function edit({ p, s }) {
  stage = "edit-open";
  await btn(row(p), "编辑").click();
  let d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  await expect(d.getByLabel("模型 ID", { exact: true })).toHaveValue(
    "sample-chat",
  );
  await shot(p, "edit-model-desktop");
  stage = "edit-delete-cancel";
  await btn(d, "删除模型").click();
  const deletion = p.getByRole("dialog", { name: "删除模型", exact: true });
  await expect(deletion).toContainText("当前共有 0 处引用。");
  await btn(deletion, "取消").click();
  await expect(deletion).toHaveCount(0);
  await expect(btn(d, "删除模型")).toBeFocused();
  assert.equal(s.requests.length, 0);
  stage = "edit-cancel";
  await d.getByLabel("模型名称", { exact: true }).fill("尚未保存的合成名称");
  await btn(d, "取消").click();
  d = p.getByRole("dialog", { name: "放弃尚未保存的编辑？", exact: true });
  await expect(d).toBeVisible();
  await expect
    .poll(() => d.evaluate((e) => e.contains(document.activeElement)))
    .toBe(true);
  await btn(d, "放弃修改").focus();
  await p.keyboard.press("Enter");
  await expect(d).toHaveCount(0);
  await expect(btn(row(p), "编辑")).toBeFocused();
  assert.equal(writes(s).length, 0);
  stage = "edit-save";
  const before = structuredClone(s.models[0].input);
  await btn(row(p), "编辑").click();
  d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  await d.getByLabel("模型 ID", { exact: true }).fill("sample-chat-edited");
  s.failListRead = true;
  await btn(d, "保存").click();
  await expect(d).toContainText("模型已保存。");
  assert.equal(writes(s).length, 1);
  assert.deepEqual(s.models[0].input, {
    ...before,
    provider_model_id: "sample-chat-edited",
  });
  await btn(d, "完成").click();
  await expect(d).toHaveCount(0);
  await expect(btn(row(p, "sample-chat-edited"), "编辑")).toBeFocused();
  assert.equal(tests(s).length, 0);
  const testDetail = await test({ p, s }, "sample-chat-edited");
  return {
    ...testDetail,
    cancelZeroWrites: true,
    nestedDeleteCancelFocus: true,
    editedFullInput: true,
    listUpdatedDespiteRefreshFailure: true,
    focus: true,
    noImplicitTest: true,
  };
}
async function test({ p, s }, modelId) {
  stage = "test-explicit-entry";
  assert.equal(tests(s).length, 0);
  await btn(row(p, modelId), "测试").click();
  const d = p.getByRole("dialog", { name: "测试模型", exact: true });
  await expect(d).toBeVisible();
  assert.equal(tests(s).length, 0);
  await expect(d.getByText("OpenAI/" + modelId, { exact: true })).toBeVisible();
  await btn(d, "开始测试").click();
  await expect(d).toContainText("本次测试成功。");
  await expect(d).toContainText("耗时 57 ms");
  assert.equal(tests(s).length, 1);
  assert.equal(s.testRuns, 1);
  assert.equal(tests(s)[0].body.expected_version, "2");
  assert.equal(tests(s)[0].body.provider_version, "3");
  assert.equal(writes(s).length, 1);
  await shot(p, "test-result-desktop");
  await btn(d, "关闭")
    .filter({ hasText: /^关闭$/ })
    .click();
  await expect(d).toHaveCount(0);
  await expect(btn(row(p, modelId), "测试")).toBeFocused();
  return {
    explicitTestOnly: true,
    exactSavedVersions: true,
    anonymousProviderAllowed: true,
    resultVisible: true,
    globalReferenceRetained: true,
    focus: true,
  };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(!only || ["layout", "toggle", "edit"].includes(only));
  const results = [],
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
  try {
    for (const [name, work] of [
      ["layout", layout],
      ["toggle", toggle],
      ["edit", edit],
    ]) {
      if (only && only !== name) continue;
      const c = await browser.newContext({
        viewport:
          name === "toggle"
            ? { width: 390, height: 844 }
            : { width: 1440, height: 1000 },
        colorScheme: name === "toggle" ? "light" : "dark",
        serviceWorkers: "block",
      });
      try {
        stage = "load";
        const data = await setup(c),
          detail = await work(data);
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
            .filter((x) => x.includes("provider-model-actions-visual.cjs:"))
            .slice(0, 2),
          api: c.review?.api,
          unexpected: c.review?.unexpected,
          errors: c.review?.errors,
          focus:
            p && !p.isClosed()
              ? await p.evaluate(() => ({
                  tag: document.activeElement?.tagName,
                  role: document.activeElement?.getAttribute("role"),
                  label: document.activeElement?.getAttribute("aria-label"),
                }))
              : null,
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
        boundary: "Synthetic API only; no real model requests or credentials",
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
