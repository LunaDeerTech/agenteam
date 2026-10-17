// Independent controlled review of ModelPicker reasoning choices.
// All backend responses are synthetic; unknown origins and routes are rejected.
// node picker-reasoning-visual.cjs --base-url http://127.0.0.1:5281
//   --owned model-picker-reasoning-visual --input <revision> --run <label> --check
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
const origin = arg("--base-url");
assert.equal(origin, "http://127.0.0.1:5281");
assert.equal(arg("--owned"), "model-picker-reasoning-visual");
assert(/^[a-z0-9-]+$/.test(arg("--run")));
const out = path.resolve(
  __dirname,
  "../../output/ai/picker-reasoning-visual",
  arg("--run"),
);
const btn = (p, name) => p.getByRole("button", { name, exact: true });
const field = (p, name) => p.getByLabel(name, { exact: true });

// The API callback must explicitly handle each allowed synthetic route. Neither
// fallthrough nor arbitrary same-origin fetches can reach an existing backend.
async function routeSynthetic(context, api) {
  const audit = { requests: [], unexpected: [], errors: [] };
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin !== origin) {
      audit.unexpected.push("outside-origin");
      return route.abort("blockedbyclient");
    }
    if (!url.pathname.startsWith("/api/")) {
      if (
        request.method() === "GET" &&
        !["xhr", "fetch"].includes(request.resourceType())
      )
        return route.continue();
      audit.unexpected.push("non-static");
      return route.abort("blockedbyclient");
    }
    audit.requests.push(request.method() + " " + url.pathname);
    if (await api(route, request, url)) return;
    audit.unexpected.push(request.method() + " " + url.pathname);
    return route.abort("blockedbyclient");
  });
  return audit;
}

async function screenshot(page, name) {
  assert(
    await page
      .locator('input[type="password"]')
      .evaluateAll((nodes) => nodes.every((node) => !node.value)),
    "private inputs must be empty for screenshots",
  );
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    );
  });
  await page.screenshot({ path: path.join(out, name + ".png") });
}

async function visibleBounds(locator, page) {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();
  const viewport = await page.evaluate(() => ({
    width: visualViewport?.width ?? innerWidth,
    height: visualViewport?.height ?? innerHeight,
    left: visualViewport?.offsetLeft ?? 0,
    top: visualViewport?.offsetTop ?? 0,
  }));
  assert(box && box.width > 0 && box.height > 0);
  assert(box.x >= viewport.left - 1 && box.y >= viewport.top - 1);
  assert(box.x + box.width <= viewport.left + viewport.width + 1);
  assert(box.y + box.height <= viewport.top + viewport.height + 1);
  return box;
}

const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
function fixture() {
  const provider = {
    id: id(1),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: "合成模型服务",
      protocol: "openai-compatible",
      base_url: "https://provider.example.invalid/v1",
      enabled: true,
      credential_ref: null,
      options: {},
    },
  };
  function model(
    n,
    modelID,
    type,
    notes,
    reasoning = false,
    mode = null,
    efforts = [],
  ) {
    const chat = type === "chat",
      embedding = type === "embedding";
    return {
      id: id(n),
      provider_id: provider.id,
      version: "1",
      created_at: at,
      updated_at: at,
      input: {
        provider_model_id: modelID,
        notes,
        type,
        enabled: true,
        parameters: {},
        request_overwrite: {},
        header_overwrite: {},
        capabilities: {
          tool_calls: false,
          parallel_tool_calls: false,
          streaming: false,
          reasoning,
          reasoning_toggle_mode: mode,
          input_modalities: ["text"],
          output_modalities: chat
            ? ["text"]
            : embedding
              ? ["vector"]
              : type === "image_generation"
                ? ["image"]
                : [],
          reasoning_efforts: efforts,
          structured_output_modes: chat ? ["text", "json_schema"] : [],
          context_length: chat || embedding ? "8192" : null,
          max_output: chat ? "1024" : null,
          embedding:
            type === "embedding"
              ? { dimensions: "1536", dimensions_mode: "fixed" }
              : null,
        },
      },
    };
  }
  const models = [
    model(10, "sample-chat-basic", "chat", "普通模型的合成备注"),
    model(11, "sample-thinking-toggle", "chat", "", true, "enable_thinking"),
    model(
      12,
      "sample-thinking-default-only",
      "chat",
      "未声明开关映射的合成模型",
      true,
    ),
    model(
      13,
      "sample-thinking-levels",
      "chat",
      "这是一段用于独立视觉审查的较长合成备注。用于确认二级菜单能完整呈现备注与思考等级，不读取任何真实配置或用户资料。",
      true,
      null,
      [
        { name: "轻度思考", value: "low" },
        { name: "深入思考", value: "high" },
      ],
    ),
    model(14, "sample-embedding", "embedding", ""),
    model(15, "sample-reranker", "reranker", "文档重排合成备注"),
    model(16, "sample-image", "image_generation", "图像生成合成备注"),
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
  const choice = (n) => ({ model_id: id(n), reasoning: { mode: "default" } });
  const state = {
    provider,
    models,
    user,
    selection: {
      id: id(70),
      version: "1",
      configured: {
        embedding: choice(14),
        summary: choice(10),
        reranker: null,
        image: null,
      },
    },
    modelWrites: [],
    writes: [],
    receipts: new Map(),
  };
  return state;
}
function assertChoice(state, value, purpose) {
  if (value === null) {
    assert(["reranker", "image"].includes(purpose));
    return;
  }
  assert.deepEqual(Object.keys(value).sort(), ["model_id", "reasoning"]);
  const model = state.models.find((item) => item.id === value.model_id);
  assert(model);
  assert.equal(
    model.input.type,
    {
      embedding: "embedding",
      summary: "chat",
      reranker: "reranker",
      image: "image_generation",
    }[purpose],
  );
  const reasoning = value.reasoning,
    c = model.input.capabilities;
  if (reasoning.mode === "effort") {
    assert.deepEqual(Object.keys(reasoning).sort(), ["mode", "value"]);
    assert(
      c.reasoning &&
        c.reasoning_efforts.some((effort) => effort.value === reasoning.value),
    );
  } else {
    assert.deepEqual(Object.keys(reasoning), ["mode"]);
    assert(["default", "on", "off"].includes(reasoning.mode));
    if (reasoning.mode !== "default")
      assert(
        model.input.type === "chat" &&
          c.reasoning &&
          !c.reasoning_efforts.length &&
          c.reasoning_toggle_mode !== null,
      );
  }
  if (model.input.type !== "chat")
    assert.deepEqual(reasoning, { mode: "default" });
}
async function serveFixture(state, route, request, url) {
  const method = request.method(),
    pathname = url.pathname;
  const reply = async (body) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
    return true;
  };
  if (method === "GET" && pathname === "/api/v1/session")
    return reply({
      user: state.user,
      session: {
        id: id(91),
        issued_at: at,
        absolute_expires_at: "2026-10-19T12:00:00.000000Z",
        idle_expires_at: "2026-10-19T12:00:00.000000Z",
      },
      csrf_token: "synthetic-picker-reasoning",
    });
  if (method === "GET" && pathname === "/api/v1/me")
    return reply({ user: state.user, avatar: null });
  if (method === "GET" && pathname === "/api/v1/system/model-selection")
    return reply(state.selection);
  if (
    method === "GET" &&
    pathname === "/api/v1/system/model-providers/" + state.provider.id
  )
    return reply(state.provider);
  if (method === "GET" && pathname === "/api/v1/system/model-capabilities")
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
  if (method === "GET" && pathname === "/api/v1/system/models") {
    const query = (url.searchParams.get("q") ?? "").trim().toLowerCase();
    const filtered = state.models.filter(
      (item) =>
        (!url.searchParams.has("provider_id") ||
          url.searchParams.get("provider_id") === item.provider_id) &&
        `${state.provider.input.name}\n${item.input.provider_model_id}\n${item.input.notes}`
          .toLowerCase()
          .includes(query),
    );
    return reply({
      items: filtered.map((item) => ({
        ...item,
        provider: {
          id: state.provider.id,
          name: state.provider.input.name,
          protocol: state.provider.input.protocol,
          enabled: state.provider.input.enabled,
          version: state.provider.version,
        },
      })),
      next_cursor: null,
    });
  }
  if (method === "GET" && pathname.startsWith("/api/v1/system/models/")) {
    const model = state.models.find(
      (item) => pathname === "/api/v1/system/models/" + item.id,
    );
    if (model) return reply(model);
  }
  if (method === "PUT" && pathname.startsWith("/api/v1/system/models/")) {
    const index = state.models.findIndex(
      (item) => pathname === "/api/v1/system/models/" + item.id,
    );
    if (index < 0) return false;
    const body = JSON.parse(request.postData());
    assert.deepEqual(Object.keys(body).sort(), ["expected_version", "input"]);
    assert.equal(body.expected_version, state.models[index].version);
    assert.equal(typeof body.input.notes, "string");
    assert(!Object.hasOwn(body.input, "name"));
    const version = String(BigInt(state.models[index].version) + 1n);
    state.models[index] = {
      ...state.models[index],
      input: structuredClone(body.input),
      version,
    };
    state.modelWrites.push({
      body: structuredClone(body),
      key: request.headers()["idempotency-key"],
    });
    assert(state.modelWrites.at(-1).key);
    return reply({
      kind: "model.update",
      resource_id: state.models[index].id,
      version,
      affected_references: "0",
    });
  }
  if (method === "PUT" && pathname === "/api/v1/system/model-selection") {
    const body = JSON.parse(request.postData()),
      key = request.headers()["idempotency-key"];
    assert(key);
    assert.deepEqual(Object.keys(body).sort(), [
      "embedding",
      "expected_version",
      "id",
      "image",
      "reranker",
      "summary",
    ]);
    for (const purpose of ["embedding", "summary", "reranker", "image"])
      assertChoice(state, body[purpose], purpose);
    assert.equal(body.id, state.selection.id);
    const previous = state.receipts.get(key);
    if (previous) {
      assert.deepEqual(body, previous.body);
      return reply(previous.receipt);
    }
    assert.equal(body.expected_version, state.selection.version);
    const version = String(BigInt(state.selection.version) + 1n);
    state.selection = {
      id: body.id,
      version,
      configured: {
        embedding: body.embedding,
        summary: body.summary,
        reranker: body.reranker,
        image: body.image,
      },
    };
    const receipt = {
      kind: "model.selection.update",
      resource_id: body.id,
      version,
      affected_references: "0",
    };
    state.writes.push({ key, body: structuredClone(body) });
    state.receipts.set(key, { body: structuredClone(body), receipt });
    return reply(receipt);
  }
  return false;
}

const summary = "选择总结模型（Chat）";
const search = (p) =>
  p.getByRole("combobox", { name: "搜索供应商或模型", exact: true });
const title = (state, n) =>
  state.provider.input.name +
  "/" +
  state.models.find((m) => m.id === id(n)).input.provider_model_id;
const trigger = (p) => p.getByRole("button", { name: /^选择总结模型（Chat）/ });
const panel = (p) => p.getByRole("dialog", { name: summary, exact: true });
const row = (p, state, n) =>
  panel(p)
    .getByRole("row")
    .filter({ has: p.getByText(title(state, n), { exact: true }) });
const more = (p, state, n) => btn(panel(p), title(state, n) + "，更多");
const details = (p, state, n) =>
  p.getByRole("dialog", { name: title(state, n) + "，更多", exact: true });
const radio = (d, name) => d.getByRole("radio", { name, exact: true });
let stage = "prepare";
async function openPicker(p) {
  await trigger(p).click();
  await expect(panel(p).getByRole("grid")).toBeVisible();
  await expect(search(panel(p))).toBeFocused();
}
async function openDetails(p, state, n, touch = false) {
  const button = more(p, state, n);
  if (touch) await button.tap();
  else await button.click();
  const d = details(p, state, n);
  await expect(d).toBeVisible();
  await visibleBounds(d, p);
  return d;
}
async function closeDetails(p, state, n, key = "Escape") {
  await p.keyboard.press(key);
  await expect(details(p, state, n)).toHaveCount(0);
  await expect(more(p, state, n)).toBeFocused();
}
async function closePicker(p) {
  await p.keyboard.press("Escape");
  await expect(panel(p)).toHaveCount(0);
  await expect(trigger(p)).toBeFocused();
}
async function noOverflow(p) {
  const v = await p.evaluate(() => ({
    root: document.documentElement.scrollWidth,
    inner: innerWidth,
  }));
  assert(v.root <= v.inner + 1, JSON.stringify(v));
  return v;
}
async function setup(context, routePath = "/system/model-selection") {
  const state = fixture();
  const audit = await routeSynthetic(context, (...args) =>
    serveFixture(state, ...args),
  );
  const p = await context.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", (e) => audit.errors.push(e.message));
  await p.goto(origin + routePath, {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  if (routePath === "/system/model-selection")
    await expect(trigger(p)).toContainText(title(state, 10));
  else
    await expect(
      p.getByRole("table", { name: "供应商模型", exact: true }),
    ).toBeVisible();
  return { p, state, audit };
}
async function persistAndRead(p, state, n, choice) {
  const before = state.writes.length,
    version = state.selection.version;
  await btn(p, "保存").click();
  await expect(p.getByText("已保存。", { exact: true })).toBeVisible();
  assert.equal(state.writes.length, before + 1);
  assert.deepEqual(state.writes.at(-1).body, {
    id: id(70),
    expected_version: version,
    embedding: { model_id: id(14), reasoning: { mode: "default" } },
    summary: { model_id: id(n), reasoning: choice },
    reranker: null,
    image: null,
  });
  await p.reload({ waitUntil: "domcontentloaded", timeout: 30000 });
  await expect(trigger(p)).toContainText(title(state, n));
}
async function desktop({ p, state }) {
  stage = "desktop-primary-and-notes";
  await openPicker(p);
  await expect(row(p, state, 10)).toHaveAttribute("aria-selected", "true");
  await expect(row(p, state, 10)).not.toContainText(
    state.models[0].input.notes,
  );
  await expect(row(p, state, 14).getByRole("button").first()).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await screenshot(p, "01-picker-primary-dark");
  const dBasic = await openDetails(p, state, 10);
  await expect(dBasic).toHaveText(state.models[0].input.notes);
  await expect(dBasic.getByRole("radio")).toHaveCount(0);
  await closeDetails(p, state, 10);
  const unmapped = await openDetails(p, state, 12);
  await expect(unmapped.getByRole("radio")).toHaveCount(1);
  await expect(radio(unmapped, "供应商默认")).toBeChecked();
  await expect(unmapped).toContainText("此模型尚未配置思考开关格式");
  await closeDetails(p, state, 12, "ArrowLeft");
  stage = "desktop-levels-pointer-and-focus";
  const m = more(p, state, 13);
  await m.focus();
  await expect(m).toHaveCSS("opacity", "1");
  await p.keyboard.press("Enter");
  const d = details(p, state, 13);
  await expect(radio(d, "供应商默认")).toBeFocused();
  await expect(d.locator(".model-picker-notes")).toHaveText(
    state.models[3].input.notes,
  );
  await expect(d.getByText("备注", { exact: true })).toHaveCount(0);
  const bounds = await visibleBounds(d, p),
    mb = await m.boundingBox();
  assert(
    bounds.x + bounds.width <= mb.x || bounds.x >= mb.x + mb.width,
    "submenu must be beside trigger",
  );
  await p.mouse.move(
    bounds.x + bounds.width / 2,
    bounds.y + bounds.height / 2,
    { steps: 12 },
  );
  await expect(d).toBeVisible();
  await expect(panel(p)).toBeVisible();
  assert.equal(state.writes.length, 0);
  await screenshot(p, "02-levels-side-dark");
  await closeDetails(p, state, 13);
  await p.setViewportSize({ width: 1920, height: 1000 });
  const right = await openDetails(p, state, 13);
  const rb = await visibleBounds(right, p),
    mt = await more(p, state, 13).boundingBox();
  assert(rb.x >= mt.x + mt.width, "wide viewport side should open right");
  await closeDetails(p, state, 13);
  await p.setViewportSize({ width: 1440, height: 1000 });
  await closePicker(p);
  for (const [label, choice] of [
    ["开启思考", { mode: "on" }],
    ["关闭思考", { mode: "off" }],
    ["供应商默认", { mode: "default" }],
  ]) {
    stage = "draft-and-persist-" + choice.mode;
    await openPicker(p);
    const menu = await openDetails(p, state, 11);
    await expect(menu.locator(".model-picker-notes")).toHaveCount(0);
    await expect(menu.getByRole("radio")).toHaveCount(3);
    const count = state.writes.length;
    await radio(menu, label).check();
    assert.equal(state.writes.length, count);
    await expect(row(p, state, 11)).toHaveAttribute("aria-selected", "true");
    if (choice.mode === "on") await screenshot(p, "03-toggle-policy-dark");
    await closeDetails(p, state, 11);
    // Selecting the same model must retain the explicit draft reasoning choice.
    await row(p, state, 11).getByRole("button").first().click();
    await persistAndRead(p, state, 11, choice);
    await openPicker(p);
    const read = await openDetails(p, state, 11);
    await expect(radio(read, label)).toBeChecked();
    await closeDetails(p, state, 11);
    await closePicker(p);
  }
  stage = "effort-name-value-persistence";
  await openPicker(p);
  const efforts = await openDetails(p, state, 13);
  await radio(efforts, "深入思考").check();
  assert.equal(state.writes.length, 3);
  await closeDetails(p, state, 13);
  await closePicker(p);
  await persistAndRead(p, state, 13, { mode: "effort", value: "high" });
  await openPicker(p);
  await row(p, state, 11).getByRole("button").first().click();
  await openPicker(p);
  const reset = await openDetails(p, state, 11);
  await expect(radio(reset, "供应商默认")).toBeChecked();
  assert.equal(state.writes.length, 4);
  await closeDetails(p, state, 11);
  await closePicker(p);
  await btn(p, "放弃修改").click();
  await expect(trigger(p)).toContainText(title(state, 13));
  return {
    noNotesInOrdinaryRows: true,
    notesOnlySupported: true,
    unmappedDefaultOnly: true,
    sideWithinViewportAndWideRight: true,
    hoverPathRetained: true,
    sameModelRetains: true,
    newModelResets: true,
    defaultOnOffEffortPersisted: true,
    selectionWrites: 4,
    ...(await noOverflow(p)),
  };
}
async function narrow({ p, state }) {
  stage = "touch-more-entry";
  await trigger(p).tap();
  await expect(panel(p).getByRole("grid")).toBeVisible();
  await expect(more(p, state, 11)).toHaveCSS("opacity", "1");
  const menu = await openDetails(p, state, 11, true);
  await expect(btn(menu, "返回模型列表")).toBeVisible();
  await radio(menu, "开启思考").tap();
  assert.equal(state.writes.length, 0);
  await screenshot(p, "04-narrow-toggle-light");
  await btn(menu, "返回模型列表").tap();
  await expect(more(p, state, 11)).toBeFocused();
  await expect(row(p, state, 11)).toHaveAttribute("aria-selected", "true");
  stage = "narrow-long-notes-levels";
  const long = await openDetails(p, state, 13, true);
  await expect(long.locator(".model-picker-notes")).toHaveText(
    state.models[3].input.notes,
  );
  await radio(long, "深入思考").scrollIntoViewIfNeeded();
  await expect(radio(long, "深入思考")).toBeInViewport();
  await screenshot(p, "05-narrow-levels-light");
  await p.keyboard.press("Escape");
  await expect(more(p, state, 13)).toBeFocused();
  await p.keyboard.press("Escape");
  await expect(trigger(p)).toBeFocused();
  assert.equal(state.writes.length, 0);
  await btn(p, "放弃修改").click();
  return {
    touchEntry: true,
    returnToMore: true,
    longNotesReachable: true,
    escapeLevels: true,
    draftZeroWrites: true,
    ...(await noOverflow(p)),
  };
}
async function form({ p, state }) {
  stage = "form-labels-order-and-optional-notes";
  const t = p.getByRole("table", { name: "供应商模型", exact: true });
  const r = t.getByRole("row").filter({
    has: p.getByRole("cell", { name: "sample-chat-basic", exact: true }),
  });
  await btn(r, "编辑").click();
  const d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  const identity = field(d, "模型 ID"),
    notes = field(d, "备注（可选）");
  await expect(field(d, "模型名称")).toHaveCount(0);
  await expect(notes).not.toHaveAttribute("required", "");
  const ib = await identity.boundingBox(),
    nb = await notes.boundingBox();
  assert(ib.y < nb.y, "model identity must precede optional notes");
  await expect(
    d.getByRole("combobox", { name: "模型类型", exact: true }),
  ).toHaveText("对话（Chat）");
  await d.locator("summary").filter({ hasText: "高级选项" }).click();
  await expect(field(d, "上下文窗口（tokens，可选）")).toHaveValue("8192");
  await expect(field(d, "最大输出（tokens，可选）")).toHaveValue("1024");
  await screenshot(p, "06-model-id-notes-tokens-light");
  const original = structuredClone(state.models[0].input);
  await notes.fill("");
  await btn(d, "保存").click();
  await expect(d).toContainText("模型已保存。");
  assert.deepEqual(state.modelWrites[0].body, {
    expected_version: "1",
    input: { ...original, notes: "" },
  });
  await btn(d, "完成").click();
  await expect(btn(r, "编辑")).toBeFocused();
  await btn(p, "添加模型").click();
  const fresh = p.getByRole("dialog", { name: "添加模型", exact: true });
  await fresh.getByRole("combobox", { name: "模型类型", exact: true }).click();
  await expect(p.getByRole("option")).toHaveText([
    "对话（Chat）",
    "嵌入（Embedding）",
    "重排（ReRank）",
    "图像生成（Image Generation）",
  ]);
  await p.keyboard.press("Escape");
  await btn(fresh, "取消").click();
  await expect(fresh).toHaveCount(0);
  return {
    modelIDFirst: true,
    notesOptionalEmptySave: true,
    tokensExplicit: true,
    fourEnglishTypeLabels: true,
    modelWrites: 1,
    ...(await noOverflow(p)),
  };
}
async function zoomWork({ p, state }) {
  stage = "native-200-percent";
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
  await openPicker(p);
  const menu = await openDetails(p, state, 13);
  await visibleBounds(panel(p), p);
  await radio(menu, "深入思考").scrollIntoViewIfNeeded();
  await expect(radio(menu, "深入思考")).toBeInViewport();
  const cdp = await p.context().newCDPSession(p);
  try {
    const capture = await cdp.send("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
    });
    await fs.writeFile(
      path.join(out, "07-native-200-reduced-dark.png"),
      Buffer.from(capture.data, "base64"),
    );
  } finally {
    await cdp.detach();
  }
  await p.keyboard.press("ArrowLeft");
  await expect(more(p, state, 13)).toBeFocused();
  await closePicker(p);
  assert.equal(state.writes.length, 0);
  return {
    ...measured,
    sideReachable: true,
    focusReturned: true,
    ...(await noOverflow(p)),
  };
}
async function keyboard({ p, state }) {
  stage = "left-flip-and-keyboard-entry";
  await p.setViewportSize({ width: 1280, height: 1000 });
  await openPicker(p);
  const primary = row(p, state, 13).getByRole("button").first();
  await primary.focus();
  await p.keyboard.press("Tab");
  await expect(more(p, state, 13)).toBeFocused();
  await p.keyboard.press("Shift+Tab");
  await expect(primary).toBeFocused();
  await p.keyboard.press("ArrowRight");
  const menu = details(p, state, 13);
  await expect(radio(menu, "供应商默认")).toBeFocused();
  const child = await visibleBounds(menu, p);
  const anchor = await more(p, state, 13).boundingBox();
  assert(child.x + child.width <= anchor.x, "1280 side must flip left");
  await p.keyboard.press("ArrowDown");
  await expect(radio(menu, "轻度思考")).toBeChecked();
  await expect(radio(menu, "轻度思考")).toBeFocused();
  assert.equal(state.writes.length, 0);
  await screenshot(p, "08-left-keyboard-dark");
  const focus = async () =>
    p.evaluate(() => {
      const a = document.activeElement;
      const r = a?.getBoundingClientRect();
      return {
        tag: a?.tagName,
        label: a?.getAttribute("aria-label"),
        text: a?.textContent?.trim().slice(0, 100),
        width: r?.width,
        height: r?.height,
        documentHasFocus: document.hasFocus(),
        focusVisible: a?.matches(":focus-visible"),
      };
    });
  stage = "keyboard-tab-exit";
  if (args.includes("--focus-diagnostic")) {
    const observations = [];
    await p.evaluate(() => {
      window.__safeFocus = [];
      for (const type of ["focusin", "focusout", "blur", "focus"])
        window.addEventListener(
          type,
          (event) => {
            window.__safeFocus.push({
              type,
              tag: event.target?.tagName,
              label: event.target?.getAttribute?.("aria-label"),
              documentHasFocus: document.hasFocus(),
            });
          },
          true,
        );
    });
    for (const key of ["Tab", "Shift+Tab", "Tab", "Tab", "Shift+Tab"]) {
      await p.keyboard.press(key);
      await p.evaluate(
        () =>
          new Promise((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(resolve)),
          ),
      );
      observations.push({
        key,
        focus: await focus(),
        sideVisible: await menu.isVisible(),
        parentVisible: await panel(p).isVisible(),
      });
    }
    const events = await p.evaluate(() => window.__safeFocus);
    await fs.writeFile(
      path.join(out, "focus-diagnostic.json"),
      JSON.stringify({ observations, events }, null, 2),
    );
    return {
      diagnosticOnly: true,
      leftFlip: true,
      primaryMoreTabRoundTrip: true,
      arrowOpensAndSelects: true,
      observations,
      events,
      zeroWrites: state.writes.length === 0,
    };
  }
  await p.keyboard.press("Tab");
  const forward = await focus();
  // This last DOM control yields to Chromium's chrome. That is a native
  // nonmodal boundary, not a lost document focus, provided Shift+Tab returns.
  assert.equal(forward.documentHasFocus, false, JSON.stringify(forward));
  assert.equal(forward.tag, "BODY");
  await p.keyboard.press("Shift+Tab");
  await expect(radio(menu, "轻度思考")).toBeFocused();
  await expect(radio(menu, "轻度思考")).toBeChecked();
  const backward = await focus();
  assert(backward.documentHasFocus && backward.focusVisible);
  await p.keyboard.press("Tab");
  await p.keyboard.press("Tab");
  await expect(
    p.getByRole("link", { name: "跳到主要内容", exact: true }),
  ).toBeFocused();
  await expect(menu).toHaveCount(0);
  await expect(panel(p)).toHaveCount(0);
  await openPicker(p);
  const retained = await openDetails(p, state, 13);
  await expect(radio(retained, "轻度思考")).toBeChecked();
  await closeDetails(p, state, 13);
  await closePicker(p);
  await btn(p, "放弃修改").click();
  assert.equal(state.writes.length, 0);
  return {
    leftFlip: true,
    primaryMoreTabRoundTrip: true,
    arrowOpensAndSelects: true,
    tabExit: forward,
    shiftTabExit: backward,
    draftRetained: true,
    zeroWrites: true,
    ...(await noOverflow(p)),
  };
}
const results = [];
async function record(context, name, work) {
  let data;
  try {
    stage = "load";
    data = await setup(
      context,
      name === "form" ? "/system/models/" + id(1) : undefined,
    );
    const result = await work(data);
    assert.deepEqual(data.audit.unexpected, []);
    assert.deepEqual(data.audit.errors, []);
    results.push({ name, outcome: "observed", result });
  } catch (error) {
    const p = data?.p ?? context.pages().at(-1);
    if (p && !p.isClosed())
      await screenshot(p, "failure-" + name).catch(() => {});
    results.push({
      name,
      outcome: "failed",
      stage,
      error: error.message,
      stack: String(error.stack)
        .split("\n")
        .filter((s) => s.includes("picker-reasoning-visual.cjs:"))
        .slice(0, 2),
      audit: data?.audit,
      focus:
        p && !p.isClosed()
          ? await p.evaluate(() => ({
              tag: document.activeElement?.tagName,
              label: document.activeElement?.getAttribute("aria-label"),
              text: document.activeElement?.textContent?.slice(0, 100),
            }))
          : null,
    });
  } finally {
    await context.close();
    console.log(JSON.stringify(results.at(-1)));
  }
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(
    !only || ["desktop", "narrow", "form", "zoom", "keyboard"].includes(only),
  );
  let browser;
  try {
    if (only !== "zoom") {
      browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: ["--no-sandbox"],
      });
      for (const [name, work] of [
        ["desktop", desktop],
        ["narrow", narrow],
        ["form", form],
        ...(only === "keyboard" ? [["keyboard", keyboard]] : []),
      ]) {
        if (only && name !== only) continue;
        const context = await browser.newContext({
          viewport:
            name === "narrow"
              ? { width: 390, height: 844 }
              : { width: 1440, height: 1000 },
          colorScheme: ["desktop", "keyboard"].includes(name)
            ? "dark"
            : "light",
          hasTouch: name === "narrow",
          serviceWorkers: "block",
        });
        await record(context, name, work);
      }
      await browser.close();
      browser = undefined;
    }
    if (!only || only === "zoom") {
      const profile = await fs.mkdtemp(
        path.join(require("node:os").tmpdir(), "picker-reasoning-zoom-"),
      );
      let context;
      try {
        await fs.mkdir(path.join(profile, "Default"));
        await fs.writeFile(
          path.join(profile, "Default/Preferences"),
          JSON.stringify({
            partition: {
              default_zoom_level: { x: Math.log(2) / Math.log(1.2) },
            },
          }),
        );
        context = await chromium.launchPersistentContext(profile, {
          executablePath: "/usr/bin/chromium",
          args: ["--no-sandbox", "--window-size=1440,1000"],
          viewport: null,
          colorScheme: "dark",
          reducedMotion: "reduce",
          serviceWorkers: "block",
        });
        await record(context, "zoom", zoomWork);
        context = undefined;
      } finally {
        await context?.close();
        await fs.rm(profile, { recursive: true, force: true });
      }
    }
  } finally {
    await browser?.close();
  }
  await fs.writeFile(
    path.join(out, "report.json"),
    JSON.stringify(
      {
        input: arg("--input"),
        boundary: "Synthetic API only; no backend or provider calls",
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
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
