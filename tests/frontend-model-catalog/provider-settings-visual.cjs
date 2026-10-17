// Independent controlled visual review. All API routes are synthetic and unknown origins fail closed.
// Usage: node provider-settings-visual.cjs --base-url http://127.0.0.1:5281 --owned provider-settings-visual --input <frozen-revision> --run <label> [--only layout|status|config|narrow]
// Output: ignored output/ai/provider-settings-visual/<label>. No real backend, external provider, credentials or public routes.
const { createRequire } = require("node:module");
const lockedRequire = createRequire(
  process.env.PROVIDER_VISUAL_PLAYWRIGHT_PACKAGE ||
    "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = lockedRequire("@playwright/test");
if (lockedRequire("@playwright/test/package.json").version !== "1.56.1")
  throw new Error("LOCKED_PLAYWRIGHT_VERSION_REQUIRED");
const fs = require("node:fs/promises"),
  path = require("node:path"),
  assert = require("node:assert/strict");
const args = process.argv.slice(2),
  arg = (n, d) => (args.includes(n) ? args[args.indexOf(n) + 1] : d);
const origin = arg("--base-url"),
  owned = arg("--owned"),
  run = arg("--run"),
  mode = arg("--mode", "views");
assert.equal(owned, "provider-settings-visual");
const ownedURL = new URL(origin);
assert.equal(ownedURL.protocol, "http:");
assert.equal(ownedURL.hostname, "127.0.0.1");
assert.equal(ownedURL.port, "5281");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
    __dirname,
    "../../output/ai/provider-settings-visual",
    run,
  ),
  results = [];
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`,
  at = "2026-10-17T09:00:00.000000Z";
const btn = (p, n) => p.getByRole("button", { name: n, exact: true }),
  field = (p, n) => p.getByLabel(n, { exact: true }),
  dlg = (p, n) => p.getByRole("dialog", { name: n, exact: true });
const provider = (n = 1, name = "合成 OpenAI 服务") => ({
  id: id(n),
  version: "1",
  created_at: at,
  updated_at: at,
  input: {
    name,
    protocol: "openai-compatible",
    base_url: "https://provider.example.invalid/v1",
    enabled: true,
    credential_ref: id(30),
    options: {},
  },
});
const model = (
  n = 10,
  type = "chat",
  name = type === "chat" ? "团队对话模型" : "项目向量模型",
) => ({
  id: id(n),
  provider_id: id(1),
  version: "1",
  created_at: at,
  updated_at: at,
  input: {
    name,
    provider_model_id:
      type === "chat"
        ? n === 12
          ? "sample-chat-backup"
          : "sample-chat"
        : n === 13
          ? "sample-embedding-backup"
          : "sample-embedding",
    type,
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      input_modalities: ["text"],
      output_modalities: [type === "chat" ? "text" : "vector"],
      reasoning_efforts: [],
      structured_output_modes: type === "chat" ? ["json_schema"] : [],
      context_length: null,
      max_output: null,
    },
  },
});
async function shot(p, n) {
  assert(
    await p
      .locator("input[type=password]")
      .evaluateAll((es) => es.every((e) => !e.value)),
    "private fields empty for capture",
  );
  await p.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) =>
      requestAnimationFrame(() => requestAnimationFrame(r)),
    );
  });
  if (
    await p.evaluate(
      () =>
        devicePixelRatio === 2 &&
        getComputedStyle(document.documentElement).zoom === "1",
    )
  ) {
    const cdp = await p.context().newCDPSession(p);
    try {
      const capture = await cdp.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      await fs.writeFile(
        path.join(out, n + ".png"),
        Buffer.from(capture.data, "base64"),
      );
    } finally {
      await cdp.detach();
    }
  } else
    await p.screenshot({ path: path.join(out, n + ".png"), fullPage: false });
}
async function overflow(p) {
  const v = await p.evaluate(() => ({
    inner: innerWidth,
    root: document.documentElement.scrollWidth,
  }));
  assert(
    v.root <= v.inner + 1,
    "root horizontal overflow " + JSON.stringify(v),
  );
  return v;
}
async function setup(c, o = {}) {
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
    user,
    providers: [
      provider(1, "OpenAI"),
      provider(2, "DeepSeek"),
      provider(3, "本地推理服务"),
    ],
    models: [
      model(),
      model(11, "embedding"),
      model(12, "chat", "合成备用对话模型"),
      model(13, "embedding", "合成备用向量模型"),
    ],
    platform: {
      id: id(40),
      version: "1",
      configured: {
        embedding: id(11),
        summary: id(10),
        reranker: null,
        image: null,
      },
    },

    api: [],
    writes: [],
    unexpected: [],
    errors: [],
    fail: undefined,
    receipts: new Map(),
  };
  s.models = s.models.slice(0, 2);
  s.providers[2].input.enabled = false;
  s.providers[2].input.credential_ref = null;
  s.secretVersion = "1";
  if (o.long)
    s.providers[0].input.name =
      "合成超长供应商名称用于检验中文英文排版 Synthetic Provider With A Long Name";
  if (o.legacy) s.providers[2].input.protocol = "anthropic-messages";
  c.review = s;
  await c.routeWebSocket("**/*", (ws) => ws.close());
  await c.route("**/*", async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      ep = u.pathname,
      m = req.method();
    if (u.origin !== origin) {
      s.unexpected.push("outside-origin");
      return route.abort("blockedbyclient");
    }
    if (!ep.startsWith("/api/")) {
      if (m === "GET" && !["xhr", "fetch"].includes(req.resourceType()))
        return route.continue();
      s.unexpected.push("non-static");
      return route.abort("blockedbyclient");
    }
    s.api.push(m + " " + ep + u.search);
    const reply = (v, status = 200) =>
        route.fulfill({
          status,
          contentType:
            status >= 400 ? "application/problem+json" : "application/json",
          body: JSON.stringify(v),
        }),
      error = (
        code = "INVALID_ARGUMENT",
        status = 400,
        commit = "not_committed",
      ) => reply({ code, commit_state: commit, field_errors: [] }, status);
    if (s.fail?.ep === ep && (!s.fail.method || s.fail.method === m)) {
      const f = s.fail;
      s.fail = undefined;
      if (f.unknown) return route.abort("connectionfailed");
      return error(f.code, f.status);
    }
    if (m === "GET" && ep === "/api/v1/session")
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
    if (m === "GET" && ep === "/api/v1/me")
      return reply({ user, avatar: null });
    if (m === "GET" && ep === "/api/v1/system/model-capabilities")
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
    if (m === "GET" && ep === "/api/v1/system/model-providers")
      return reply({
        items: s.providers.slice(0, Number(u.searchParams.get("limit") || 10)),
        next_cursor: null,
      });
    if (m === "GET" && ep.startsWith("/api/v1/system/model-providers/"))
      return reply(s.providers.find((x) => x.id === ep.split("/").at(-1)));
    if (m === "GET" && ep.endsWith("/deletion-impact"))
      return reply({
        model_id: id(10),
        version: "1",
        reference_count: "1",
        reference_groups: [
          { owner_kind: "platform_selector", role: "summary", count: "1" },
        ],
        replacement_requirement: "required",
        delete_blocker: null,
      });
    if (m === "GET" && ep === "/api/v1/system/models") {
      const providerID = u.searchParams.get("provider_id"),
        q = u.searchParams.get("q") || "",
        cursor = u.searchParams.get("cursor"),
        limit = Number(u.searchParams.get("limit") || 10);
      assert.equal(limit, 10);
      if (!providerID && s.failNextPage && cursor) {
        s.failNextPage = false;
        return error("INTERNAL_ERROR", 500, "not_started");
      }
      const projection = (item) => {
        const p = s.providers.find((p) => p.id === item.provider_id);
        return {
          ...item,
          provider: {
            id: p.id,
            name: p.input.name,
            protocol: p.input.protocol,
            enabled: p.input.enabled,
            version: p.version,
          },
        };
      };
      const items = s.models
        .map(projection)
        .filter(
          (item) =>
            (!providerID || item.provider_id === providerID) &&
            (!q ||
              [
                item.input.name,
                item.input.provider_model_id,
                item.provider.name,
              ].some((value) => value.toLowerCase().includes(q))),
        );
      const start = cursor === "synthetic-next-page" ? 10 : 0;
      return reply({
        items: items.slice(start, start + limit),
        next_cursor:
          items.length > start + limit ? "synthetic-next-page" : null,
      });
    }
    if (m === "GET" && /^\/api\/v1\/system\/models\/[0-9a-f-]+$/.test(ep))
      return reply(s.models.find((x) => x.id === ep.split("/").at(-1)));
    if (m === "GET" && ep.startsWith("/api/v1/system/model-credentials/"))
      return reply({
        credential_id: ep.split("/").at(-1),
        purpose: "model",
        version: s.secretVersion,
      });
    if (m === "GET" && ep === "/api/v1/system/model-selection")
      return reply(s.platform);

    if (m !== "GET") {
      const body = JSON.parse(req.postData() || "{}");
      s.writes.push({ ep, m, body, key: req.headers()["idempotency-key"] });
      return command({ s, ep, m, body, reply, error, route, req });
    }
    s.unexpected.push(m + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", () => s.errors.push("pageerror"));
  await p.goto(origin + (o.route || "/system/models"), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(p.getByRole("heading", { level: 1 })).toBeVisible();
  return { p, s };
}

let stage = "setup";
const providerPath = (n) => "/api/v1/system/model-providers/" + id(n);
const row = (p, name) =>
  p
    .getByRole("list", { name: "供应商目录", exact: true })
    .getByRole("listitem")
    .filter({ has: p.getByRole("link", { name, exact: true }) });
const tab = (p, name) => p.getByRole("tab", { name, exact: true });
const configPanel = (p) =>
  p.getByRole("tabpanel", { name: "配置", exact: true });
const switchFor = (p, name) =>
  p.getByRole("switch", { name: "启用" + name, exact: true });
const writes = (s, ep) => s.writes.filter((w) => w.ep === ep && w.m !== "GET");
async function command({ s, ep, m, body, reply, error, route, req }) {
  const key = req.headers()["idempotency-key"];
  if (m === "POST" && ep === "/api/v1/system/model-commands/lookup")
    return reply({
      found: !!s.receipts.get(key),
      receipt: s.receipts.get(key)?.receipt ?? null,
    });
  if (m === "POST" && ep === "/api/v1/system/model-credential-commands/lookup")
    return reply({
      observed: !!s.receipts.get(key),
      result: s.receipts.get(key)?.receipt ?? null,
    });
  if (
    m === "PUT" &&
    (ep.startsWith("/api/v1/system/model-providers/") ||
      ep.startsWith("/api/v1/system/model-credentials/"))
  ) {
    assert(key, "mutation key required");
    const previous = s.receipts.get(key);
    if (previous) {
      assert.equal(previous.ep, ep);
      assert.deepEqual(previous.body, body);
      return reply(previous.receipt);
    }
    const secret = ep.startsWith("/api/v1/system/model-credentials/");
    const behavior = secret ? s.nextSecret : s.nextProvider;
    if (secret) s.nextSecret = undefined;
    else s.nextProvider = undefined;
    if (behavior === "reject")
      return error("INVALID_ARGUMENT", 400, "not_started");
    if (behavior === "conflict")
      return error("VERSION_CONFLICT", 409, "not_started");
    let receipt;
    if (secret) {
      assert.equal(body.value, "TEST_ONLY_PROVIDER_CREDENTIAL_DO_NOT_USE");
      assert.equal(body.expected_version, s.secretVersion);
      s.secretVersion = String(BigInt(s.secretVersion) + 1n);
      receipt = {
        credential_id: id(30),
        purpose: "model",
        version: s.secretVersion,
        deleted: false,
      };
    } else {
      const provider = s.providers.find(
        (p) => ep === providerPath(Number(p.id.slice(-12))),
      );
      assert(provider);
      assert.equal(body.expected_version, provider.version);
      provider.input = structuredClone(body.input);
      provider.version = String(BigInt(provider.version) + 1n);
      receipt = {
        kind: "provider.update",
        resource_id: provider.id,
        version: provider.version,
        affected_references: "0",
      };
      if (s.failAfterReceipt) {
        s.failAfterReceipt = false;
        s.fail = { ep, method: "GET", code: "INTERNAL_ERROR", status: 500 };
      }
    }
    s.receipts.set(key, { ep, body: structuredClone(body), receipt });
    if (behavior === "unknown") return route.abort("connectionfailed");
    return reply(receipt);
  }
  s.unexpected.push(m + " " + ep);
  return route.abort("blockedbyclient");
}
async function layout({ p, s }) {
  stage = "desktop-list";
  await expect(
    p.getByRole("heading", { name: "供应商列表", exact: true }),
  ).toBeVisible();
  await expect(p.getByRole("switch")).toHaveCount(3);
  await expect(
    row(p, "OpenAI").getByRole("button", { name: "配置", exact: true }),
  ).toBeVisible();
  await expect(switchFor(p, "OpenAI")).toHaveAttribute("aria-checked", "true");
  await expect(switchFor(p, "本地推理服务")).toHaveAttribute(
    "aria-checked",
    "false",
  );
  await overflow(p);
  await shot(p, "01-provider-list-1440-dark");
  stage = "desktop-name-to-models";
  await p.getByRole("link", { name: "OpenAI", exact: true }).click();
  await expect(tab(p, "模型")).toHaveAttribute("aria-selected", "true");
  await expect(p.getByRole("table", { name: "供应商模型" })).toBeVisible();
  await expect(
    p.getByRole("cell", { name: "OpenAI/sample-chat", exact: true }),
  ).toBeVisible();
  await expect(
    p.getByRole("cell", { name: "OpenAI/sample-embedding", exact: true }),
  ).toBeVisible();
  await shot(p, "models-tab-1440-dark");
  await p.getByRole("link", { name: "模型接入", exact: true }).click();
  await expect(
    p.getByRole("link", { name: "OpenAI", exact: true }),
  ).toBeFocused();
  stage = "desktop-config-route-inline";
  await btn(row(p, "OpenAI"), "配置").click();
  await expect(p).toHaveURL(origin + "/system/models/" + id(1) + "?tab=config");
  await expect(tab(p, "配置")).toHaveAttribute("aria-selected", "true");
  const panel = configPanel(p);
  await expect(field(panel, "API 端点")).toHaveValue(
    "https://provider.example.invalid/v1",
  );
  await expect(field(panel, "API 凭据（可选）")).toHaveValue("");
  await expect(p.locator("dialog[open]")).toHaveCount(0);
  await shot(p, "02-provider-config-1440-dark");
  stage = "desktop-tabs-draft-and-empty-secret-save";
  await field(panel, "API 端点").fill(
    "https://provider.example.invalid/review-v2",
  );
  await tab(p, "配置").focus();
  await p.keyboard.press("ArrowLeft");
  await expect(tab(p, "模型")).toBeFocused();
  await expect(p.getByRole("table", { name: "供应商模型" })).toBeVisible();
  await p.keyboard.press("ArrowRight");
  await expect(tab(p, "配置")).toBeFocused();
  await expect(field(panel, "API 端点")).toHaveValue(
    "https://provider.example.invalid/review-v2",
  );
  await btn(panel, "保存").click();
  await expect(
    panel.getByText("供应商已保存。", { exact: true }),
  ).toBeVisible();
  assert.equal(writes(s, providerPath(1)).length, 1);
  assert(!s.writes.some((w) => w.ep.includes("model-credentials")));
  assert.equal(s.providers[0].input.credential_ref, id(30));
  await expect(field(panel, "API 端点")).toHaveValue(
    "https://provider.example.invalid/review-v2",
  );
  stage = "desktop-inline-cancel-focus";
  await field(panel, "供应商名称").fill("OpenAI 未保存编辑");
  await btn(panel, "取消").click();
  await expect(
    panel.getByText("放弃尚未保存的编辑？", { exact: true }),
  ).toBeVisible();
  await btn(panel, "放弃编辑").focus();
  await p.keyboard.press("Enter");
  await expect(field(panel, "供应商名称")).toHaveValue("OpenAI");
  assert(
    await panel.evaluate((e) => e.contains(document.activeElement)),
    "discarding an inline draft must retain focus in the configuration task",
  );
  return {
    listSwitches: true,
    configButtonRoute: true,
    modelsAssociated: true,
    inlineConfig: true,
    draftAcrossTabs: true,
    emptySecretPreserved: true,
    cancelFocus: true,
    ...(await overflow(p)),
  };
}
async function status({ p, s }) {
  stage = "switch-keyboard-save";
  const control = switchFor(p, "OpenAI"),
    own = row(p, "OpenAI"),
    focusIssues = [];
  await control.focus();
  await p.keyboard.press("Space");
  await expect(control).toHaveAttribute("aria-checked", "false");
  await expect(control).toBeEnabled();
  if (!(await control.evaluate((e) => e === document.activeElement)))
    focusIssues.push("normal-save");
  assert.equal(writes(s, providerPath(1)).length, 1);
  stage = "switch-rejected-and-cancel";
  s.nextProvider = "reject";
  await control.click();
  await expect(own.getByRole("alert")).toContainText("请检查填写内容");
  await expect(control).toHaveAttribute("aria-checked", "false");
  await btn(own, "取消更改").focus();
  await p.keyboard.press("Enter");
  await expect(control).toBeEnabled();
  if (!(await control.evaluate((e) => e === document.activeElement)))
    focusIssues.push("cancel-rejected");
  stage = "switch-unknown-original-request";
  s.nextProvider = "unknown";
  await control.click();
  await expect(btn(own, "查询原操作")).toBeVisible();
  await expect(control).toBeDisabled();
  await expect(btn(own, "配置")).toBeDisabled();
  await expect(control).toHaveAttribute("aria-checked", "false");
  await shot(p, "switch-unknown-1440-dark");
  await btn(own, "查询原操作").click();
  await expect(
    own.getByText("已找到历史回执；确认原请求后才能完成本次操作。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(control).toHaveAttribute("aria-checked", "false");
  await btn(own, "确认原请求").click();
  await expect(control).toHaveAttribute("aria-checked", "true");
  await expect(control).toBeEnabled();
  const put = writes(s, providerPath(1));
  assert.equal(put.length, 4);
  assert.equal(put[2].key, put[3].key);
  assert.deepEqual(put[2].body, put[3].body);
  if (!(await control.evaluate((e) => e === document.activeElement)))
    focusIssues.push("unknown-confirmed");
  stage = "switch-confirmed-read-failure";
  s.failAfterReceipt = true;
  await control.click();
  await expect(
    own.getByText("状态已保存，最新配置尚未读回。", { exact: true }),
  ).toBeVisible();
  const before = writes(s, providerPath(1)).length;
  await btn(own, "重新读取").click();
  await expect(control).toHaveAttribute("aria-checked", "false");
  await expect(control).toBeEnabled();
  assert.equal(writes(s, providerPath(1)).length, before);
  if (!(await control.evaluate((e) => e === document.activeElement)))
    focusIssues.push("confirmed-get-retry");
  assert.deepEqual(focusIssues, [], "switch focus handoffs");
  return {
    normalAndRejected: true,
    unknownSameKeyBody: true,
    confirmedFailureOnlyGET: true,
    focusRestored: true,
    ...(await overflow(p)),
  };
}
async function config({ p, s }) {
  stage = "config-secret-unknown";
  await btn(row(p, "OpenAI"), "配置").click();
  const panel = configPanel(p),
    secret = field(panel, "API 凭据（可选）");
  await expect(secret).toHaveValue("");
  await field(panel, "API 端点").fill(
    "https://provider.example.invalid/controlled-config",
  );
  await secret.fill("TEST_ONLY_PROVIDER_CREDENTIAL_DO_NOT_USE");
  s.nextSecret = "unknown";
  await btn(panel, "保存").click();
  await expect(btn(panel, "查询原操作")).toBeVisible();
  await expect(secret).toHaveValue("");
  await expect(tab(p, "模型")).toBeDisabled();
  assert.equal(writes(s, providerPath(1)).length, 0);
  await shot(p, "inline-unknown-secret-empty");
  await btn(panel, "查询原操作").click();
  await expect(
    panel.getByText("已找到历史回执；确认原请求后才能完成本次操作。", {
      exact: true,
    }),
  ).toBeVisible();
  assert.equal(writes(s, providerPath(1)).length, 0);
  await btn(panel, "确认原请求").click();
  await expect(
    panel.getByText("供应商已保存。", { exact: true }),
  ).toBeVisible();
  await expect(secret).toHaveValue("");
  await expect(tab(p, "模型")).toBeEnabled();
  const secretWrites = writes(s, "/api/v1/system/model-credentials/" + id(30));
  assert.equal(secretWrites.length, 2);
  assert.equal(secretWrites[0].key, secretWrites[1].key);
  assert.deepEqual(secretWrites[0].body, secretWrites[1].body);
  assert.equal(writes(s, providerPath(1)).length, 1);
  await tab(p, "模型").click();
  await expect(
    p.getByRole("cell", { name: "OpenAI/sample-chat", exact: true }),
  ).toBeVisible();
  return {
    secretInputCleared: true,
    unknownFreezesOtherTab: true,
    lookupNotCompletion: true,
    originalSecretRequestConfirmed: true,
    providerAssociatedOnce: true,
  };
}
async function narrow({ p, s }) {
  stage = "narrow-list";
  const name = s.providers[0].input.name;
  await expect(switchFor(p, name)).toBeVisible();
  await overflow(p);
  await shot(p, "list-390-light");
  await btn(row(p, name), "配置").click();
  const panel = configPanel(p);
  await expect(field(panel, "API 端点")).toBeVisible();
  await overflow(p);
  await shot(p, "config-390-light");
  stage = "narrow-keyboard-scroll-cancel";
  await tab(p, "配置").focus();
  await p.keyboard.press("ArrowLeft");
  await expect(tab(p, "模型")).toBeFocused();
  await expect(p.getByRole("table", { name: "供应商模型" })).toBeVisible();
  await overflow(p);
  await p.keyboard.press("ArrowRight");
  await expect(tab(p, "配置")).toBeFocused();
  await btn(panel, "保存").scrollIntoViewIfNeeded();
  await expect(btn(panel, "保存")).toBeInViewport();
  await expect(btn(panel, "取消")).toBeInViewport();
  stage = "legacy-provider-read-only";
  await p.getByRole("link", { name: "模型接入", exact: true }).click();
  await expect(switchFor(p, "本地推理服务")).toBeDisabled();
  await btn(row(p, "本地推理服务"), "配置").click();
  await expect(field(configPanel(p), "API 端点")).toBeDisabled();
  await expect(btn(configPanel(p), "保存")).toBeDisabled();
  assert.equal(s.writes.length, 0);
  return {
    longNames: true,
    tabsKeyboard: true,
    saveAndCancelReachable: true,
    legacyReadOnly: true,
    ...(await overflow(p)),
  };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = arg("--only");
  assert(!only || ["layout", "status", "config", "narrow"].includes(only));
  const browser = await chromium.launch({
    executablePath: "/usr/bin/chromium",
    args: ["--no-sandbox"],
  });
  try {
    for (const [name, options, work] of [
      ["layout", {}, layout],
      ["status", {}, status],
      ["config", {}, config],
      [
        "narrow",
        { width: 390, theme: "light", long: true, legacy: true },
        narrow,
      ],
    ]) {
      if (only && only !== name) continue;
      const c = await browser.newContext({
        viewport: {
          width: options.width || 1440,
          height: options.width === 390 ? 844 : 1000,
        },
        colorScheme: options.theme || "dark",
        serviceWorkers: "block",
      });
      try {
        stage = "setup";
        const data = await setup(c, options),
          detail = await work(data);
        assert.deepEqual(data.s.unexpected, []);
        assert.deepEqual(data.s.errors, []);
        results.push({ name, outcome: "observed", detail });
      } catch (e) {
        const p = c.pages().at(-1),
          s = c.review;
        if (p && !p.isClosed())
          await shot(p, "failure-" + name).catch(() => {});
        results.push({
          name,
          outcome: "failed",
          stage,
          check: String(e.message).split("\n")[0],
          location: String(e.stack)
            .split("\n")
            .filter((x) => x.includes("provider-settings-visual.cjs:"))
            .slice(0, 2),
          api: s?.api,
          unexpected: s?.unexpected,
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
        results,
        browserClosed: true,
        boundary:
          "Synthetic API only; no real backend, vendor or public configuration tested.",
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
