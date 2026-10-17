// Independent controlled visual review. All API routes are synthetic and unknown origins fail closed.
// Usage: node picker-visual.cjs --base-url http://127.0.0.1:5281 --owned model-picker-visual --input <frozen-revision> --run <label> [--only desktop|narrow|nested|zoom]
// Output: ignored output/ai/picker-visual/<label>. No real backend, external provider, credentials or public routes.
const { createRequire } = require("node:module");
const lockedRequire = createRequire(
  process.env.PICKER_PLAYWRIGHT_PACKAGE ||
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
assert.equal(owned, "model-picker-visual");
const ownedURL = new URL(origin);
assert.equal(ownedURL.protocol, "http:");
assert.equal(ownedURL.hostname, "127.0.0.1");
assert.equal(ownedURL.port, "5281");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(__dirname, "../../output/ai/picker-visual", run),
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
    providers: o.empty ? [] : [provider(), provider(2, "备用模型服务")],
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
  s.models[2].provider_id = id(2);
  s.models[3].input.enabled = false;
  for (let n = 14; n < 23; n++) {
    const item = model(n, n % 2 ? "embedding" : "chat", "合成候选 " + n);
    item.provider_id = id(n % 3 === 0 ? 2 : 1);
    item.input.provider_model_id = "sample-model-" + n;
    s.models.push(item);
  }
  if (o.long) {
    s.providers[0].input.name =
      "合成超长供应商名称用于检验中文英文混合排版 Synthetic OpenAI Compatible Provider With A Long Name";
    s.models[0].input.name =
      "超长对话模型名称 Long Model Name For Narrow Layout";
    s.models[0].input.provider_model_id =
      "long-model-identifier-with-many-characters-and-separators-synthetic-chat-v1";
  }
  if (o.many)
    s.providers = Array.from({ length: 10 }, (_, i) =>
      provider(i + 1, "合成供应商 " + (i + 1)),
    );
  if (o.unconfigured) s.platform.configured = null;
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
        version: "1",
      });
    if (m === "GET" && ep === "/api/v1/system/model-selection")
      return reply(s.platform);

    if (m !== "GET") {
      const body = JSON.parse(req.postData() || "{}");
      s.writes.push({ ep, m, body, key: req.headers()["idempotency-key"] });
      if (o.command)
        return o.command({ s, ep, m, body, reply, error, route, req });
    }
    s.unexpected.push(m + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", () => s.errors.push("pageerror"));
  await p.goto(origin + (o.route || "/system/model-selection"), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(p.getByRole("heading", { level: 1 })).toBeVisible();
  return { p, s };
}

let stage = "setup";
const trigger = (p, purpose = "总结模型（Chat）") =>
  p.getByRole("button", { name: new RegExp("^选择" + purpose + "(?:，|$)") });
const popup = (p, purpose = "总结模型（Chat）") =>
  p.getByRole("dialog", { name: "选择" + purpose, exact: true });
const search = (p) =>
  p.getByRole("combobox", { name: "搜索供应商或模型", exact: true });
const label = (s, n) => {
  const m = s.models.find((m) => m.id === id(n));
  return (
    s.providers.find((p) => p.id === m.provider_id).input.name +
    "/" +
    m.input.provider_model_id
  );
};
const option = (panel, text) =>
  panel
    .getByRole("option")
    .filter({ has: panel.page().getByText(text, { exact: true }) });
async function geometry(p, anchor, panel, expected) {
  await panel.evaluate(async (e) => {
    await Promise.all(
      e.getAnimations().map((animation) => animation.finished.catch(() => {})),
    );
  });
  const a = await anchor.boundingBox(),
    r = await panel.boundingBox();
  assert(a && r, "anchor and popup geometry required");
  const viewport = await p.evaluate(() => ({ w: innerWidth, h: innerHeight }));
  assert(
    r.x >= 7 && r.x + r.width <= viewport.w - 7,
    "popup must stay horizontally within viewport",
  );
  assert(
    r.y >= 7 && r.y + r.height <= viewport.h - 7,
    "popup must stay vertically within viewport",
  );
  const side = r.y + r.height <= a.y + 1 ? "above" : "below";
  if (expected) assert.equal(side, expected);
  assert(
    side === "above"
      ? Math.abs(a.y - r.y - r.height - 6) < 3
      : Math.abs(r.y - a.y - a.height - 6) < 3,
    "popup must be attached to its trigger",
  );
  assert(
    await panel.evaluate((e) => e.matches(":popover-open")),
    "native top layer popup open",
  );
  return { side, anchor: a, panel: r };
}
async function expand(p, purpose = "总结模型（Chat）", method = "click") {
  const t = trigger(p, purpose);
  await t[method]();
  const panel = popup(p, purpose);
  await expect(panel).toBeVisible();
  await expect(search(panel)).toBeFocused();
  await expect(panel.getByRole("option").first()).toBeVisible();
  return { t, panel };
}
async function desktop({ p, s }) {
  stage = "desktop-page";
  await expect(trigger(p)).toHaveText(label(s, 10));
  await expect(
    p.getByRole("heading", {
      name: /^图像生成模型（Image Generation）\s*可选$/,
    }),
  ).toBeVisible();
  await expect(p.locator(".purpose-description")).toHaveCount(4);
  await expect(btn(p, "保存")).toHaveCount(1);
  await overflow(p);
  await shot(p, "01-purpose-selects-1440-dark");
  stage = "desktop-anchor-selected-disabled";
  let { t, panel } = await expand(p);
  const firstGeometry = await geometry(p, t, panel, "below");
  await expect(panel.getByRole("option")).toHaveCount(10);
  await expect(option(panel, label(s, 10))).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(option(panel, label(s, 11))).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await expect(option(panel, label(s, 12))).not.toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await shot(p, "02-anchored-global-picker-1440-dark");
  const disabledMouse = await option(panel, label(s, 11)).boundingBox();
  await p.mouse.click(disabledMouse.x + 12, disabledMouse.y + 12);
  await expect(panel).toBeVisible();
  await expect(t).toHaveText(label(s, 10));
  stage = "desktop-arrow-selection";
  await search(panel).press("ArrowDown");
  await search(panel).press("Enter");
  await expect(panel).toHaveCount(0);
  await expect(t).toHaveText(label(s, 12));
  await expect(t).toBeFocused();
  stage = "desktop-search-empty-invalid";
  ({ t, panel } = await expand(p));
  await search(panel).fill("备用");
  await expect
    .poll(() => s.api.filter((a) => a.includes("q=")).length)
    .toBeGreaterThan(0);
  // Four supplier-name matches plus the disabled model's custom name.
  await expect(panel.getByRole("option")).toHaveCount(5);
  assert(
    s.api.some(
      (a) =>
        a.includes("/system/models?") &&
        a.includes("q=") &&
        !a.includes("provider_id="),
    ),
  );
  await search(panel).fill("%_");
  await expect(
    panel.getByText("没有匹配的模型。", { exact: true }),
  ).toBeVisible();
  const beforeInvalid = s.api.length;
  await search(panel).fill("汉".repeat(43));
  await expect(panel.getByRole("alert")).toContainText("128 字节");
  assert.equal(s.api.length, beforeInvalid);
  stage = "desktop-page-error-retry";
  await search(panel).fill("");
  await expect(panel.getByRole("option")).toHaveCount(10);
  s.failNextPage = true;
  await btn(panel, "下一页").click();
  await expect(panel.getByRole("alert")).toBeVisible();
  await expect(panel.getByRole("option")).toHaveCount(0);
  await shot(p, "pagination-error-1440-dark");
  await btn(panel, "重新读取").click();
  await expect(panel.getByText("第 2 页", { exact: true })).toBeVisible();
  await expect(panel.getByRole("option")).toHaveCount(3);
  await btn(panel, "上一页").click();
  await expect(panel.getByText("第 1 页", { exact: true })).toBeVisible();
  await expect(search(panel)).toBeFocused();
  stage = "desktop-escape-outside-tab";
  await search(panel).press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(t).toBeFocused();
  ({ t, panel } = await expand(p));
  await p.getByRole("heading", { name: "平台模型用途", exact: true }).click();
  await expect(panel).toHaveCount(0);
  ({ t, panel } = await expand(p));
  await search(panel).press("Tab");
  // Chromium 151 makes the overflowing results region natively tabbable.
  if (
    await panel
      .locator(".ui-search-select-results")
      .evaluate((e) => e === document.activeElement)
  )
    await p.keyboard.press("Tab");
  await expect(btn(panel, "下一页")).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(panel).toHaveCount(0);
  await expect(trigger(p, "重排模型（Rerank）")).toBeFocused();
  assert.equal(s.writes.length, 0, "picker interactions are drafts only");
  return {
    firstGeometry,
    globalSearch: true,
    selectedDisabled: true,
    paginationRetry: true,
    keyboardAndMouse: true,
    ...(await overflow(p)),
  };
}
async function narrow({ p, s }) {
  stage = "narrow-long-page";
  await expect(trigger(p)).toHaveText(label(s, 10));
  await overflow(p);
  await shot(p, "long-page-390-light");
  stage = "narrow-touch-anchor";
  const { t, panel } = await expand(p, "总结模型（Chat）", "tap");
  const firstGeometry = await geometry(p, t, panel);
  await shot(p, "03-long-picker-390-light");
  await option(panel, label(s, 11)).scrollIntoViewIfNeeded();
  const disabledTouch = await option(panel, label(s, 11)).boundingBox();
  await p.touchscreen.tap(disabledTouch.x + 12, disabledTouch.y + 12);
  await expect(panel).toBeVisible();
  await expect(t).toHaveText(label(s, 10));
  stage = "narrow-popup-scroll";
  const list = panel.locator(".ui-search-select-results");
  const pos = await list.boundingBox();
  await p.mouse.move(pos.x + pos.width / 2, pos.y + pos.height / 2);
  await p.mouse.wheel(0, 350);
  await expect.poll(() => list.evaluate((e) => e.scrollTop)).toBeGreaterThan(0);
  await search(panel).fill("sample-chat-backup");
  await expect(panel.getByRole("option")).toHaveCount(1);
  await panel.getByRole("option").tap();
  await expect(panel).toHaveCount(0);
  await expect(t).toHaveText(label(s, 12));
  await expect(t).toBeFocused();
  stage = "narrow-bottom-flip-scroll";
  const bottom = trigger(p, "图像生成模型（Image Generation）");
  await bottom.scrollIntoViewIfNeeded();
  await bottom.tap();
  const lower = popup(p, "图像生成模型（Image Generation）");
  await expect(lower.getByRole("option").first()).toBeVisible();
  const flipGeometry = await geometry(p, bottom, lower, "above");
  const scroll = p.locator(".settings-main");
  const before = await scroll.evaluate((e) => e.scrollTop);
  await scroll.evaluate(
    (e) => (e.scrollTop = e.scrollTop > 0 ? Math.max(0, e.scrollTop - 50) : 50),
  );
  await expect.poll(() => scroll.evaluate((e) => e.scrollTop)).not.toBe(before);
  await expect
    .poll(async () => {
      try {
        await geometry(p, bottom, lower);
        return true;
      } catch {
        return false;
      }
    })
    .toBe(true);
  await search(lower).press("Escape");
  await expect(bottom).toBeFocused();
  return {
    touchSelection: true,
    internalScroll: true,
    flipGeometry,
    firstGeometry,
    anchorTracksPageScroll: true,
    ...(await overflow(p)),
  };
}
async function nested({ p, s }) {
  stage = "nested-delete-open";
  await p.goto(origin + "/system/models/" + id(1), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  const rowAction = btn(p, label(s, 10) + "操作");
  await rowAction.click();
  await p.getByRole("menuitem", { name: "删除", exact: true }).click();
  const parent = dlg(p, "删除模型"),
    t = parent.getByRole("button", { name: "选择替代模型", exact: true });
  await expect(t).toBeEnabled();
  stage = "nested-popover-layer-keyboard";
  await t.click();
  const panel = dlg(p, "选择替代模型");
  await expect(panel.getByRole("option").first()).toBeVisible();
  await geometry(p, t, panel);
  await expect(p.locator("dialog[open]")).toHaveCount(1);
  await expect(search(panel)).toBeFocused();
  await shot(p, "delete-popover-1440-dark");
  await search(panel).press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(parent).toBeVisible();
  await expect(t).toBeFocused();
  await t.click();
  await expect(panel.getByRole("option").first()).toBeVisible();
  await search(panel).fill("sample-chat-backup");
  await expect(panel.getByRole("option")).toHaveCount(1);
  await panel.getByRole("option").click();
  await expect(panel).toHaveCount(0);
  await expect(
    parent.getByRole("button", {
      name: "选择替代模型，当前 " + label(s, 12),
      exact: true,
    }),
  ).toBeFocused();
  await expect(btn(parent, "确认删除")).toBeEnabled();
  await btn(parent, "取消").click();
  await expect(rowAction).toBeFocused();
  assert.equal(s.writes.length, 0);
  return {
    topLayerWithinOneModal: true,
    escapeOnlyClosesPicker: true,
    replacementSelected: true,
    cancelRestoresRow: true,
  };
}
async function keyboardDetail({ p, s }) {
  stage = "keyboard-scroll-region-bidirectional";
  const { t, panel } = await expand(p);
  const region = panel.locator(".ui-search-select-results");
  await search(panel).press("Tab");
  await expect(region).toBeFocused();
  await shot(p, "scroll-region-visible-focus");
  await p.keyboard.press("Shift+Tab");
  await expect(search(panel)).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(region).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(btn(panel, "下一页")).toBeFocused();
  await p.keyboard.press("Shift+Tab");
  await expect(region).toBeFocused();
  await p.keyboard.press("Shift+Tab");
  await expect(search(panel)).toBeFocused();
  await p.keyboard.press("Shift+Tab");
  await expect(panel).toHaveCount(0);
  await expect(trigger(p, "向量模型（Embedding）")).toBeFocused();
  await expect(t).toHaveText(label(s, 10));
  stage = "keyboard-pagination-error-retry-focus";
  await t.click();
  await expect(panel.getByRole("option").first()).toBeVisible();
  s.failNextPage = true;
  await search(panel).press("Tab");
  await p.keyboard.press("Tab");
  await expect(btn(panel, "下一页")).toBeFocused();
  await p.keyboard.press("Enter");
  await expect(panel.getByRole("alert")).toBeVisible();
  await expect(search(panel)).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(btn(panel, "重新读取")).toBeFocused();
  await p.keyboard.press("Enter");
  await expect(panel.getByText("第 2 页", { exact: true })).toBeVisible();
  await shot(p, "keyboard-after-page-retry");
  assert(
    await panel.evaluate((e) => e.contains(document.activeElement)),
    "successful retry must leave keyboard focus in the open picker",
  );
  await expect(t).toHaveText(label(s, 10));
  await p.keyboard.press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(t).toBeFocused();
  return {
    nativeRegionVisibleFocus: true,
    forwardBackwardTab: true,
    selectionRetained: true,
    errorRetryFocusRetained: true,
  };
}
async function record(context, name, options, work) {
  let data;
  try {
    stage = "setup";
    data = await setup(context, options);
    const detail = await work(data);
    assert.deepEqual(data.s.unexpected, []);
    assert.deepEqual(data.s.errors, []);
    results.push({ name, outcome: "observed", detail });
  } catch (e) {
    const p = context.pages().at(-1),
      s = context.review;
    if (p && !p.isClosed()) await shot(p, "failure-" + name).catch(() => {});
    results.push({
      name,
      outcome: "failed",
      stage,
      check: String(e.message).split("\n")[0],
      location: String(e.stack)
        .split("\n")
        .filter((x) => x.includes("picker-visual.cjs:"))
        .slice(0, 2),
      api: s?.api,
      unexpected: s?.unexpected,
      errors: s?.errors,
      focus:
        p && !p.isClosed()
          ? await p.evaluate(() => ({
              tag: document.activeElement?.tagName,
              role: document.activeElement?.getAttribute("role"),
              label: document.activeElement?.getAttribute("aria-label"),
              className: document.activeElement?.className,
            }))
          : undefined,
    });
  } finally {
    await context.close();
    console.log(JSON.stringify(results.at(-1)));
  }
}
async function zoom() {
  const profile = await fs.mkdtemp(
    require("node:os").tmpdir() + "/picker-visual-zoom-",
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
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      colorScheme: "dark",
      reducedMotion: "reduce",
      serviceWorkers: "block",
    });
    await record(context, "zoom", {}, async ({ p }) => {
      stage = "native-200-reduced-geometry";
      const measured = await p.evaluate(() => ({
        outer: outerWidth,
        inner: innerWidth,
        dpr: devicePixelRatio,
        cssZoom: getComputedStyle(document.documentElement).zoom,
        reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
      }));
      assert.deepEqual(measured, {
        outer: 1440,
        inner: 720,
        dpr: 2,
        cssZoom: "1",
        reduced: true,
      });
      const { t, panel } = await expand(p);
      await geometry(p, t, panel);
      await expect(search(panel)).toBeInViewport();
      await expect(btn(panel, "下一页")).toBeInViewport();
      await shot(p, "picker-native-200-reduced-dark");
      if (args.includes("--capture-only"))
        return { ...measured, captureOnly: true, ...(await overflow(p)) };
      await search(panel).press("Escape");
      await expect(t).toBeFocused();
      return { ...measured, ...(await overflow(p)) };
    });
    context = undefined;
  } finally {
    await context?.close();
    await fs.rm(profile, { recursive: true, force: true });
  }
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = arg("--only");
  assert(
    !only || ["desktop", "narrow", "nested", "zoom", "keyboard"].includes(only),
  );
  let browser;
  try {
    if (only !== "zoom") {
      browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: ["--no-sandbox"],
      });
      for (const [name, options, work] of [
        ["desktop", { theme: "dark" }, desktop],
        [
          "narrow",
          { theme: "light", width: 390, long: true, touch: true },
          narrow,
        ],
        ["nested", { theme: "dark" }, nested],
        ["keyboard", { theme: "dark" }, keyboardDetail],
      ]) {
        if (only && only !== name) continue;
        const context = await browser.newContext({
          viewport: {
            width: options.width || 1440,
            height: options.width === 390 ? 844 : 960,
          },
          colorScheme: options.theme,
          hasTouch: !!options.touch,
          serviceWorkers: "block",
        });
        await record(context, name, options, work);
      }
      await browser.close();
      browser = undefined;
    }
    if (!only || only === "zoom") await zoom();
  } finally {
    await browser?.close();
  }
  await fs.writeFile(
    path.join(out, "report.json"),
    JSON.stringify(
      {
        input: arg("--input"),
        results,
        browserClosed: true,
        boundary:
          "All APIs synthetic. No real backend, vendor or user data. Independent UI evidence only.",
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
