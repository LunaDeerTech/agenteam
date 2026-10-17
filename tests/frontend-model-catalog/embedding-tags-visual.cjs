// Independent visual review; every API response below is synthetic.
// No real account, provider request, database, public preview or deployment.
// node embedding-tags-visual.cjs --owned embedding-tags-visual --input <revision>
//   --output /tmp/agenteam-embedding-tags-review.<unique> [--case desktop|narrow|zoom|tags]
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
if (process.argv.includes("--list")) {
  console.log(
    "embedding-tags-visual: synthetic desktop 1440 dark; narrow 390 light; native 200% reduced motion; picker tags, reasoning keyboard and embedding form; tags = desktop labels/titles and one screenshot only",
  );
  process.exit(0);
}
const arg = (name) => process.argv[process.argv.indexOf(name) + 1];
assert(
  process.argv.includes("--owned") &&
    arg("--owned") === "embedding-tags-visual",
);
assert(
  process.argv.includes("--input") && /^[a-f0-9]{7,40}$/.test(arg("--input")),
);
const out = path.resolve(arg("--output"));
assert(/^\/tmp\/agenteam-embedding-tags-review\.[\w-]+$/.test(out));
const only = process.argv.includes("--case") ? arg("--case") : null;
assert(!only || ["desktop", "narrow", "zoom", "tags"].includes(only));
const origin = "http://127.0.0.1:5281";
const id = (n) => `019a7777-7777-7777-8777-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
const summary = "选择总结模型（Chat）";
let stage = "prepare";
const results = [];
const lifecycle = [];

function fixture() {
  const provider = {
    id: id(1),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: "合成审查供应商",
      protocol: "openai-compatible",
      base_url: "https://synthetic.example.invalid",
      enabled: true,
      credential_ref: null,
      options: {},
    },
  };
  function model(n, modelID, type, overrides = {}) {
    const chat = type === "chat",
      embedding = type === "embedding";
    return {
      id: id(n),
      provider_id: provider.id,
      version: "1",
      created_at: at,
      updated_at: at,
      input: {
        notes: "仅供独立界面审查的合成模型。",
        provider_model_id: modelID,
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
          reasoning_toggle_mode: null,
          reasoning_efforts: [],
          input_modalities: chat ? ["text", "image", "file"] : ["text"],
          output_modalities: chat
            ? ["text"]
            : embedding
              ? ["vector"]
              : type === "image_generation"
                ? ["image"]
                : [],
          structured_output_modes: chat ? ["text", "json_schema"] : [],
          context_length: chat ? "128000" : embedding ? "8192" : null,
          max_output: null,
          embedding: embedding
            ? { dimensions: "1536", dimensions_mode: "fixed" }
            : null,
          ...overrides,
        },
      },
    };
  }
  const models = [
    model(10, "review-chat-vision-and-file", "chat", {
      reasoning: true,
      reasoning_efforts: [
        { name: "轻度思考", value: "low" },
        { name: "深入思考", value: "high" },
      ],
    }),
    model(11, "review-embedding-fixed", "embedding"),
    model(12, "review-embedding-request", "embedding", {
      context_length: null,
      embedding: { dimensions: "1024", dimensions_mode: "request" },
    }),
    model(13, "review-reranker", "reranker"),
    model(14, "review-image-generation", "image_generation"),
    model(
      15,
      "review-long-identity-" + "unbrokenmodelidentity".repeat(7),
      "chat",
      { context_length: null, input_modalities: ["text"] },
    ),
    model(16, "review-disabled-chat", "chat", {
      context_length: null,
      input_modalities: ["text"],
    }),
  ];
  models[0].input.notes = "合成备注：验证思考菜单的层级、键盘操作和返回焦点。";
  models[5].input.notes =
    "合成长备注，保留中文、标点与连续标识的换行。".repeat(12) +
    "sampleonly".repeat(16);
  models[6].input.enabled = false;
  const choice = (n) => ({ model_id: id(n), reasoning: { mode: "default" } });
  return {
    provider,
    models,
    writes: [],
    faults: [],
    api: [],
    selection: {
      id: id(40),
      version: "1",
      configured: {
        embedding: choice(11),
        summary: choice(10),
        reranker: null,
        image: null,
      },
    },
  };
}

async function intercept(context, state) {
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      method = request.method();
    if (url.origin !== origin) {
      state.faults.push("blocked-external-origin");
      return route.abort("blockedbyclient");
    }
    if (!url.pathname.startsWith("/api/")) {
      if (
        method === "GET" &&
        !["xhr", "fetch"].includes(request.resourceType())
      )
        return route.continue();
      state.faults.push("blocked-nonstatic-request");
      return route.abort("blockedbyclient");
    }
    const p = url.pathname.replace(/^\/api\/v1/, "");
    state.api.push(`${method} ${p}`);
    const reply = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    const user = {
      id: id(90),
      email: "independent-review@example.invalid",
      username: "synthetic-review",
      display_name: "合成独立审查",
      role: "admin",
      theme: "system",
      version: "1",
      initial_password_suggestion: false,
    };
    if (p === "/session" && method === "GET")
      return reply({
        user,
        session: {
          id: id(91),
          issued_at: at,
          absolute_expires_at: "2026-10-20T12:00:00.000000Z",
          idle_expires_at: "2026-10-20T12:00:00.000000Z",
        },
        csrf_token: "synthetic-review-only",
      });
    if (p === "/me" && method === "GET") return reply({ user, avatar: null });
    if (p === "/system/model-capabilities" && method === "GET")
      return reply({
        protocols: [
          {
            protocol: "openai-compatible",
            model_types: ["chat", "embedding", "reranker", "image_generation"],
            discovery: true,
            test: true,
          },
        ],
      });
    if (p === "/system/model-selection" && method === "GET")
      return reply(state.selection);
    if (p === "/system/model-providers" && method === "GET")
      return reply({ items: [state.provider], next_cursor: null });
    if (
      p === `/system/model-providers/${state.provider.id}` &&
      method === "GET"
    )
      return reply(state.provider);
    if (p === "/system/models" && method === "GET") {
      const q = (url.searchParams.get("q") || "").toLowerCase();
      const items = state.models
        .filter(
          (m) =>
            !url.searchParams.has("type") ||
            m.input.type === url.searchParams.get("type"),
        )
        .filter(
          (m) =>
            !url.searchParams.get("provider_id") ||
            m.provider_id === url.searchParams.get("provider_id"),
        )
        .filter((m) =>
          `${state.provider.input.name}/${m.input.provider_model_id} ${m.input.notes}`
            .toLowerCase()
            .includes(q),
        )
        .map((m) => ({
          ...m,
          provider: {
            id: state.provider.id,
            name: state.provider.input.name,
            protocol: state.provider.input.protocol,
            enabled: true,
            version: state.provider.version,
          },
        }));
      return reply({ items, next_cursor: null });
    }
    const m = state.models.find((item) => p === `/system/models/${item.id}`);
    if (m && method === "GET") return reply(m);
    if (m && [id(11), id(12)].includes(m.id) && method === "PUT") {
      const body = request.postDataJSON();
      assert.equal(body.expected_version, m.version);
      state.writes.push({ id: m.id, input: body.input });
      m.input = body.input;
      m.version = String(Number(m.version) + 1);
      return reply({
        kind: "model.update",
        resource_id: m.id,
        version: m.version,
        affected_references: "0",
      });
    }
    state.faults.push(`blocked-api:${method} ${p}`);
    return route.abort("blockedbyclient");
  });
}
const trigger = (page) =>
  page.getByRole("button", { name: /^选择总结模型（Chat）/ });
const panel = (page) =>
  page.getByRole("dialog", { name: summary, exact: true });
const name = (state, n) =>
  `${state.provider.input.name}/${state.models.find((m) => m.id === id(n)).input.provider_model_id}`;
const row = (page, state, n) =>
  panel(page)
    .getByRole("row")
    .filter({ has: page.getByText(name(state, n), { exact: true }) });
const more = (page, state, n) =>
  panel(page).getByRole("button", {
    name: name(state, n) + "，更多",
    exact: true,
  });
const details = (page, state, n) =>
  page.getByRole("dialog", { name: name(state, n) + "，更多", exact: true });
async function within(locator, page) {
  const b = await locator.boundingBox();
  const v = await page.evaluate(() => ({
    width: innerWidth,
    height: innerHeight,
    scroll: document.documentElement.scrollWidth,
  }));
  assert(
    b &&
      b.x >= -1 &&
      b.x + b.width <= v.width + 1 &&
      b.y >= -1 &&
      b.y + b.height <= v.height + 1,
    "visible bounds",
  );
  assert(v.scroll <= v.width + 1, "page horizontal overflow");
  return b;
}
async function shot(page, label, native = false) {
  assert(
    await page
      .locator('input[type="password"]')
      .evaluateAll((els) => els.every((e) => e.value === "")),
  );
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) =>
      requestAnimationFrame(() => requestAnimationFrame(r)),
    );
  });
  const file = path.join(out, `${label}.png`);
  if (native) {
    const cdp = await page.context().newCDPSession(page);
    try {
      const s = await cdp.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      await fs.writeFile(file, Buffer.from(s.data, "base64"));
    } finally {
      await cdp.detach();
    }
  } else await page.screenshot({ path: file });
}
async function open(page) {
  await trigger(page).click();
  await expect(panel(page).getByRole("row")).toHaveCount(3);
  await expect(panel(page).getByRole("combobox")).toBeFocused();
}
async function close(page) {
  await page.keyboard.press("Escape");
  await expect(panel(page)).toHaveCount(0);
  await expect(trigger(page)).toBeFocused();
}
async function picker(page, state, label, native) {
  stage = `${label}:picker-tags`;
  await open(page);
  const expected = {
    10: ["对话（Chat）", "128K", "图片", "文件"],
    15: ["对话（Chat）"],
    16: ["对话（Chat）"],
  };
  for (const [n, tags] of Object.entries(expected)) {
    const r = row(page, state, Number(n));
    await expect(r.locator(".ui-search-select-tag")).toHaveText(tags);
    assert.equal(
      await r
        .locator(
          ".ui-search-select-tag [tabindex], .ui-search-select-tag button",
        )
        .count(),
      0,
    );
    const geometry = await r.evaluate((el) => {
      const title = el.querySelector(".ui-search-select-name"),
        tags = el.querySelector(".ui-search-select-tags");
      const t = title.getBoundingClientRect(),
        g = tags.getBoundingClientRect();
      return {
        titleBottom: t.bottom,
        tagTop: g.top,
        titleFont: parseFloat(getComputedStyle(title).fontSize),
        tagFont: parseFloat(getComputedStyle(tags.firstElementChild).fontSize),
        overflow: el.scrollWidth > el.clientWidth + 1,
      };
    });
    assert(
      geometry.tagTop >= geometry.titleBottom,
      "tags must follow identity on second line",
    );
    assert(
      geometry.tagFont >= 12 && geometry.tagFont < geometry.titleFont,
      "secondary tag typography",
    );
    assert(!geometry.overflow, "row horizontal overflow");
  }
  for (const [n, label, title] of [
    [10, "128K", "上下文 128000 tokens"],
    [10, "图片", "图片输入"],
    [10, "文件", "文件输入"],
  ]) {
    await expect(
      row(page, state, n).getByText(label, { exact: true }),
    ).toHaveAttribute("title", title);
  }
  for (const n of [11, 12, 13, 14])
    await expect(row(page, state, n)).toHaveCount(0);
  const geometry = await within(panel(page), page);
  await shot(page, `${label}-picker-top`, native);
  if (label === "desktop-tags") {
    assert.equal(state.writes.length, 0);
    await close(page);
    return {
      geometry,
      matchingChatTags: true,
      compactLabels: true,
      exactCapacityAndInputTitles: true,
      writes: 0,
    };
  }
  const disabled = row(page, state, 16).getByRole("button").first();
  await expect(disabled).toHaveAttribute("aria-disabled", "true");
  await expect(
    row(page, state, 16).locator(".ui-search-select-detail"),
  ).toContainText("停用");
  await disabled.scrollIntoViewIfNeeded();
  await disabled.focus();
  await page.keyboard.press("Enter");
  await expect(panel(page)).toBeVisible();
  await shot(page, `${label}-picker-long-and-disabled`, native);
  stage = `${label}:long-notes`;
  await more(page, state, 15).click();
  await expect(
    details(page, state, 15).locator(".model-picker-notes"),
  ).toHaveText(state.models[5].input.notes);
  await within(details(page, state, 15), page);
  await shot(page, `${label}-long-notes`, native);
  await page.keyboard.press("Escape");
  await expect(more(page, state, 15)).toBeFocused();
  stage = `${label}:reasoning-keyboard`;
  const primary = row(page, state, 10).getByRole("button").first();
  await primary.focus();
  await page.keyboard.press("Tab");
  await expect(more(page, state, 10)).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(primary).toBeFocused();
  await page.keyboard.press("ArrowRight");
  const menu = details(page, state, 10);
  if (await page.evaluate(() => innerWidth <= 640)) {
    await expect(
      menu.getByRole("button", { name: "返回模型列表", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
  }
  await expect(
    menu.getByRole("radio", { name: "供应商默认", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(
    menu.getByRole("radio", { name: "轻度思考", exact: true }),
  ).toBeFocused();
  await expect(
    menu.getByRole("radio", { name: "轻度思考", exact: true }),
  ).toBeChecked();
  await within(menu, page);
  await shot(page, `${label}-reasoning-keyboard`, native);
  await page.keyboard.press("ArrowLeft");
  await expect(more(page, state, 10)).toBeFocused();
  assert.equal(state.writes.length, 0, "picker draft must not write");
  await close(page);
  // Restore the default draft locally before changing routes: no synthetic save.
  await open(page);
  await more(page, state, 10).click();
  await details(page, state, 10)
    .getByRole("radio", { name: "供应商默认", exact: true })
    .check();
  await page.keyboard.press("Escape");
  await close(page);
  return {
    geometry,
    matchingChatTags: true,
    nullCapacityNotInvented: true,
    disabledReason: true,
    reasoningFocusReturned: true,
    writes: 0,
  };
}
async function editor(page, state, label, native) {
  stage = `${label}:provider-navigation`;
  await page.goto(`${origin}/system/models/${state.provider.id}`, {
    waitUntil: "domcontentloaded",
  });
  const modelRow = (n) =>
    page.getByRole("row").filter({
      hasText: state.models.find((m) => m.id === id(n)).input.provider_model_id,
    });
  await expect(modelRow(11)).toBeVisible({ timeout: 30000 });
  const dialog = page.getByRole("dialog", { name: "编辑模型", exact: true });
  for (const [n, dimensions, checked] of [
    [11, "1536", "false"],
    [12, "1024", "true"],
  ]) {
    stage = `${label}:embedding-${n === 11 ? "fixed" : "request"}`;
    await modelRow(n)
      .getByRole("button", { name: "编辑", exact: true })
      .click();
    const input = dialog.getByLabel("向量维度", { exact: true });
    const toggle = dialog.getByRole("switch", {
      name: "请求指定维度",
      exact: true,
    });
    await expect(input).toHaveValue(dimensions);
    await expect(toggle).toHaveAttribute("aria-checked", checked);
    await toggle.scrollIntoViewIfNeeded();
    const alignment = await dialog
      .locator(".embedding-mode-label")
      .evaluate((el) => {
        const a = el.querySelector("span").getBoundingClientRect(),
          b = el.querySelector('[role="switch"]').getBoundingClientRect();
        return {
          centers: Math.abs(a.y + a.height / 2 - b.y - b.height / 2),
          gap: b.x - a.right,
        };
      });
    assert(
      alignment.centers <= 2 && alignment.gap >= 8,
      "dimension switch alignment",
    );
    await within(dialog, page);
    await shot(
      page,
      `${label}-embedding-${n === 11 ? "fixed" : "request"}`,
      native,
    );
    await toggle.focus();
    await page.keyboard.press("Space");
    await expect(toggle).toHaveAttribute(
      "aria-checked",
      checked === "true" ? "false" : "true",
    );
    await page.keyboard.press("Space");
    await expect(toggle).toHaveAttribute("aria-checked", checked);
    if (n === 11 && label !== "zoom") {
      await input.fill("768");
      await toggle.click();
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(
        page.getByText("模型已保存。", { exact: true }),
      ).toBeVisible();
      assert.equal(state.writes.length, 1);
      assert.deepEqual(state.writes[0].input.capabilities.embedding, {
        dimensions: "768",
        dimensions_mode: "request",
      });
      await dialog.getByRole("button", { name: "完成", exact: true }).click();
      await modelRow(n)
        .getByRole("button", { name: "编辑", exact: true })
        .click();
      await expect(dialog.getByLabel("向量维度", { exact: true })).toHaveValue(
        "768",
      );
      await expect(
        dialog.getByRole("switch", { name: "请求指定维度", exact: true }),
      ).toHaveAttribute("aria-checked", "true");
    }
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(
      modelRow(n).getByRole("button", { name: "编辑", exact: true }),
    ).toBeFocused();
  }
  return {
    fixedAndRequest: true,
    switchKeyboard: true,
    labelAligned: true,
    syntheticSaveAndReadback: label !== "zoom",
    writes: state.writes.length,
  };
}

async function run(label, width, theme, native = false) {
  const profile = path.join(out, `profile-${label}`);
  await fs.mkdir(path.join(profile, "Default"), { recursive: true });
  if (native)
    await fs.writeFile(
      path.join(profile, "Default/Preferences"),
      JSON.stringify({
        partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
      }),
    );
  let context, page;
  const state = fixture();
  const record = { case: label, browserClosed: false, profileRemoved: false };
  lifecycle.push(record);
  try {
    context = await chromium.launchPersistentContext(profile, {
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,1000"],
      viewport: native ? null : { width, height: 1000 },
      colorScheme: theme,
      reducedMotion: native ? "reduce" : "no-preference",
      serviceWorkers: "block",
      env: { ...process.env, TMPDIR: path.join(out, "cache") },
    });
    await intercept(context, state);
    page = context.pages()[0] || (await context.newPage());
    page.setDefaultTimeout(12000);
    page.setDefaultNavigationTimeout(30000);
    page.on("pageerror", () => state.faults.push("pageerror"));
    stage = `${label}:selection-navigation`;
    await page.goto(origin + "/system/model-selection", {
      waitUntil: "domcontentloaded",
    });
    await expect(trigger(page)).toContainText(
      state.models[0].input.provider_model_id,
      { timeout: 30000 },
    );
    const measured = await page.evaluate(() => ({
      outer: outerWidth,
      inner: innerWidth,
      dpr: devicePixelRatio,
      cssZoom: getComputedStyle(document.documentElement).zoom,
      reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
    }));
    if (native)
      assert.deepEqual(measured, {
        outer: 1440,
        inner: 720,
        dpr: 2,
        cssZoom: "1",
        reduced: true,
      });
    const pickerResult = await picker(page, state, label, native);
    const editorResult =
      label === "desktop-tags"
        ? null
        : await editor(page, state, label, native);
    assert.deepEqual(state.faults, []);
    results.push({
      case: label,
      theme,
      measured,
      picker: pickerResult,
      editor: editorResult,
      apiRequests: state.api,
      passed: true,
    });
  } catch (error) {
    results.push({
      case: label,
      passed: false,
      stage,
      error: String(error.message).slice(0, 3000),
      faults: state.faults,
      apiRequests: state.api,
    });
    await fs.writeFile(
      path.join(out, `${label}-failure.json`),
      JSON.stringify(results.at(-1), null, 2),
    );
    if (page && !page.isClosed())
      await shot(page, `${label}-failure`, native).catch(() => {});
    throw error;
  } finally {
    if (context) {
      await context.close();
      record.browserClosed = true;
    }
    await fs.rm(profile, { recursive: true, force: true });
    record.profileRemoved = true;
  }
}
(async () => {
  await fs.mkdir(path.join(out, "cache"), { recursive: true });
  try {
    if (!only || only === "desktop") await run("desktop", 1440, "dark");
    if (!only || only === "narrow") await run("narrow", 390, "light");
    if (!only || only === "zoom") await run("zoom", 1440, "dark", true);
    if (only === "tags") await run("desktop-tags", 1440, "dark");
  } finally {
    await fs.writeFile(
      path.join(out, "report.json"),
      JSON.stringify(
        {
          input: arg("--input"),
          boundary:
            "Independent synthetic Vue-only review; no real backend/provider/public preview",
          results,
          lifecycle,
        },
        null,
        2,
      ),
    );
  }
  console.log(
    JSON.stringify({
      passed: true,
      cases: results.map((r) => r.case),
      browserClosed: lifecycle.every((l) => l.browserClosed),
      profileRemoved: lifecycle.every((l) => l.profileRemoved),
    }),
  );
})().catch(() => {
  console.error(
    JSON.stringify({
      passed: false,
      stage,
      report: path.join(out, "report.json"),
    }),
  );
  process.exitCode = 1;
});
