// Independent controlled review of type-aware model capabilities. No real backend or provider.
// node capability-form-visual.cjs --base-url http://127.0.0.1:5281 --owned capability-form-visual --input <revision> --run <label> [--only compact|four-types|create-four|narrow] [--check]
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
assert.equal(arg("--owned"), "capability-form-visual");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
  __dirname,
  "../../output/ai/capability-form-visual",
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
      name: {
        chat: "团队对话模型",
        embedding: "项目嵌入模型",
        reranker: "文档重排模型",
        image_generation: "图像生成模型",
      }[type],
      provider_model_id: modelId,
      type,
      enabled: true,
      parameters: {},
      request_overwrite: {},
      header_overwrite: {},
      capabilities: {
        tool_calls: false,
        parallel_tool_calls: false,
        streaming: type === "chat",
        reasoning: false,
        input_modalities: ["text"],
        output_modalities: {
          chat: ["text"],
          embedding: ["vector"],
          reranker: [],
          image_generation: ["image"],
        }[type],
        reasoning_efforts: [],
        structured_output_modes: type === "chat" ? ["text"] : [],
        context_length: ["chat", "embedding"].includes(type) ? "8192" : null,
        max_output: type === "chat" ? "1024" : null,
        embedding:
          type === "embedding"
            ? { dimensions: "1536", dimensions_mode: "fixed" }
            : null,
      },
    },
  };
}
async function setup(c, variant) {
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
    sampleModel(12, "reranker", "sample-reranker"),
    sampleModel(13, "image_generation", "sample-image"),
  ];
  if (["narrow", "compact"].includes(variant)) {
    models[0].input.capabilities.input_modalities = ["text", "image", "file"];
    models[0].input.capabilities.output_modalities = ["text"];
    models[0].input.capabilities.reasoning = true;
    models[0].input.capabilities.reasoning_efforts = [
      { name: "轻度思考", value: "low" },
      { name: "深入思考", value: "high" },
    ];
  }
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
    timeline: [],
  };
  const started = Date.now();
  const mark = (event, extra = {}) =>
    s.timeline.push({ event, ms: Date.now() - started, ...extra });
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
    mark("request", { method, ep });
    const reply = async (value, status = 200) => {
      await route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        body: JSON.stringify(value),
      });
      mark("fulfilled", { method, ep, status });
    };
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
    if (method === "GET" && ep.startsWith("/api/v1/system/models/")) {
      const found = models.find((model) => ep.endsWith("/" + model.id));
      if (found) return reply(found);
    }
    if (method === "GET" && ep === "/api/v1/system/model-capabilities")
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
    if (
      (method === "PUT" &&
        models.some((model) => ep === "/api/v1/system/models/" + model.id)) ||
      (method === "POST" && ep === "/api/v1/system/models")
    ) {
      const body = JSON.parse(req.postData()),
        key = req.headers()["idempotency-key"];
      assert(key);
      s.requests.push({ method, ep, key, body });
      if (s.holdWrite) await s.holdWrite.promise;
      let model;
      if (method === "POST") {
        assert.deepEqual(Object.keys(body).sort(), ["input", "provider_id"]);
        assert.equal(body.provider_id, provider.id);
        model = {
          id: id(30),
          provider_id: provider.id,
          version: "1",
          created_at: at,
          updated_at: at,
          input: structuredClone(body.input),
        };
        models.push(model);
      } else {
        const index = models.findIndex((model) => ep.endsWith("/" + model.id));
        assert.deepEqual(Object.keys(body).sort(), [
          "expected_version",
          "input",
        ]);
        assert.equal(body.expected_version, models[index].version);
        model = {
          ...models[index],
          version: String(BigInt(models[index].version) + 1n),
          input: structuredClone(body.input),
        };
        models[index] = model;
      }
      return reply({
        kind: method === "POST" ? "model.create" : "model.update",
        resource_id: model.id,
        version: model.version,
        affected_references: "0",
      });
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const p = await c.newPage();
  p.setDefaultTimeout(10000);
  p.on("pageerror", (e) =>
    s.errors.push({ name: e.name, message: e.message.slice(0, 250) }),
  );
  mark("navigation-start");
  await p.goto(origin + "/system/models/" + id(1), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  mark("domcontentloaded");
  await expect(table(p)).toBeVisible();
  mark("table-visible");
  return { p, s };
}

// Capture the actual scrolling dialog in overlapping viewport images. No layout resizing or CSS injection.
async function captureDialog(p, d, name) {
  const body = d.locator(".ui-dialog-body");
  const size = await body.evaluate((e) => ({
    height: e.clientHeight,
    total: e.scrollHeight,
    width: e.clientWidth,
    scrollWidth: e.scrollWidth,
  }));
  assert(size.scrollWidth <= size.width, "dialog body horizontal overflow");
  const last = Math.max(0, size.total - size.height),
    positions = [];
  for (
    let position = 0;
    position < last;
    position += Math.max(1, size.height - 100)
  )
    positions.push(position);
  positions.push(last);
  assert(positions.length <= 6, "unexpectedly unbounded screenshot state");
  const ranges = [];
  for (let index = 0; index < positions.length; index++) {
    await body.evaluate((e, y) => {
      e.scrollTop = y;
    }, positions[index]);
    await shot(p, name + (positions.length > 1 ? "-" + (index + 1) : ""));
    ranges.push(
      await body.evaluate((e) => ({
        top: e.scrollTop,
        bottom: e.scrollTop + e.clientHeight,
        total: e.scrollHeight,
      })),
    );
  }
  assert(
    ranges[0].top === 0 && ranges.at(-1).bottom >= ranges.at(-1).total - 1,
    "incomplete dialog capture",
  );
  await body.evaluate((e) => {
    e.scrollTop = 0;
  });
  return ranges;
}
const schemaLabel = "支持结构化输出（JSON Schema）";
const check = (d, label) =>
  d.getByRole("checkbox", { name: label, exact: true });
const field = (d, label) => d.getByLabel(label, { exact: true });
const captures = [];
async function capture(p, d, name) {
  const ranges = await captureDialog(p, d, name);
  captures.push({ name, ranges });
}
async function editor(p, modelID = "sample-chat") {
  await btn(row(p, modelID), "编辑").click();
  const d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  await expect(d).toBeVisible();
  await expect(
    d.getByRole("combobox", { name: "模型类型", exact: true }),
  ).toBeDisabled();
  for (const label of [
    "参数（JSON）",
    "请求体覆盖（JSON）",
    "请求头覆盖（JSON）",
  ])
    await expect(field(d, label)).toHaveCount(0);
  return d;
}
async function advanced(d) {
  const details = d.locator("details").filter({ hasText: "高级选项" });
  if (!(await details.evaluate((e) => e.open)))
    await details.locator("summary").click();
}
async function fixedChat(d) {
  const input = d.getByRole("group", { name: "输入模态", exact: true });
  await expect(check(input, "文本")).toBeChecked();
  await expect(check(input, "文本")).toBeDisabled();
  await expect(check(input, "向量")).toHaveCount(0);
  await expect(input.getByRole("checkbox")).toHaveCount(3);
  await expect(
    d.getByText("图像和文件仅记录模型能力，当前调用仅支持文本。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    d
      .locator(".fixed-modality")
      .filter({ hasText: "输出模态" })
      .locator(".fixed-value"),
  ).toHaveText("文本");
}
async function finish(p, d, modelID) {
  await expect(d).toContainText("模型已保存。");
  await btn(d, "完成").click();
  await expect(d).toHaveCount(0);
  await expect(btn(row(p, modelID), "编辑")).toBeFocused();
}
async function selectType(p, d, label) {
  await d.getByRole("combobox", { name: "模型类型", exact: true }).click();
  await p.getByRole("option", { name: label, exact: true }).click();
}
async function narrow({ p, s }) {
  stage = "narrow-all-fields";
  const d = await editor(p);
  await fixedChat(d);
  await advanced(d);
  await expect(field(d, "等级名称 1")).toHaveValue("轻度思考");
  await expect(field(d, "等级值 2")).toHaveValue("high");
  await expect(
    check(d.getByRole("group", { name: "输入模态", exact: true }), "图像"),
  ).toBeChecked();
  await expect(
    check(d.getByRole("group", { name: "输入模态", exact: true }), "文件"),
  ).toBeChecked();
  const geometry = await compactRows(p, d);
  await capture(p, d, "14-narrow-compact-reasoning");
  stage = "narrow-remove-and-cancel";
  await btn(d, "删除等级 2").click();
  await expect(field(d, "等级名称 1")).toBeFocused();
  await btn(d, "删除等级 1").click();
  await expect(btn(d, "添加思考等级")).toBeFocused();
  await btn(d, "添加思考等级").click();
  await expect(field(d, "等级名称 1")).toBeFocused();
  await p.keyboard.press("Escape");
  const discard = p.getByRole("dialog", {
    name: "放弃尚未保存的编辑？",
    exact: true,
  });
  await expect(btn(discard, "继续编辑")).toBeFocused();
  await btn(discard, "放弃修改").click();
  await expect(discard).toHaveCount(0);
  await expect(btn(row(p), "编辑")).toBeFocused();
  assert.equal(s.requests.length, 0);
  return {
    geometry,
    completeScrollingCapture: true,
    metadataChoicesVisible: true,
    levelRemoveFocus: true,
    cancelZeroWrites: true,
    reducedMotion: await p.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  };
}

async function compactRows(p, d) {
  const rows = d.locator(".reasoning-row");
  const count = await rows.count();
  assert(count > 0);
  for (let i = 1; i <= count; i++) {
    await expect(field(d, `等级名称 ${i}`)).toBeVisible();
    await expect(field(d, `等级值 ${i}`)).toBeVisible();
    await expect(field(d, `等级名称 ${i}`)).toHaveAttribute(
      "placeholder",
      "名称",
    );
    await expect(field(d, `等级值 ${i}`)).toHaveAttribute("placeholder", "值");
    await expect(btn(d, `删除等级 ${i}`)).toBeVisible();
  }
  const geometry = await rows.evaluateAll((nodes) =>
    nodes.map((e) => {
      const controls = [...e.querySelectorAll("input,button")].map((n) => {
        const r = n.getBoundingClientRect();
        return {
          x: r.x,
          y: r.y,
          width: r.width,
          height: r.height,
          center: r.y + r.height / 2,
        };
      });
      const labels = [...e.querySelectorAll(".ui-field-label")].map((n) => {
        const r = n.getBoundingClientRect(),
          c = getComputedStyle(n);
        return {
          width: r.width,
          height: r.height,
          clip: c.clip,
          position: c.position,
        };
      });
      return { controls, labels };
    }),
  );
  for (const item of geometry) {
    assert.equal(item.controls.length, 3);
    assert(
      Math.max(...item.controls.map((r) => r.center)) -
        Math.min(...item.controls.map((r) => r.center)) <=
        1,
      "name/value/delete must share one row",
    );
    assert(
      item.controls[0].x + item.controls[0].width <= item.controls[1].x,
      "name/value overlap",
    );
    assert(
      item.controls[1].x + item.controls[1].width <= item.controls[2].x,
      "value/delete overlap",
    );
    assert(
      item.labels.every(
        (r) =>
          r.width <= 1 &&
          r.height <= 1 &&
          r.clip === "rect(0px, 0px, 0px, 0px)",
      ),
      "visible level numbers remain",
    );
  }
  const last = await rows.last().boundingBox(),
    add = await btn(d, "添加思考等级").boundingBox();
  assert(add.y >= last.y + last.height, "add action must stay below rows");
  await dimensions(p);
  return geometry;
}
async function compact({ p, s }) {
  stage = "desktop-compact-levels";
  const d = await editor(p);
  await fixedChat(d);
  await advanced(d);
  const geometry = await compactRows(p, d);
  await field(d, "等级名称 1").focus();
  await p.keyboard.press("Tab");
  await expect(field(d, "等级值 1")).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(btn(d, "删除等级 1")).toBeFocused();
  await p.keyboard.press("Tab");
  await expect(field(d, "等级名称 2")).toBeFocused();
  await capture(p, d, "10-chat-compact-dark");
  await btn(d, "取消").click();
  await expect(d).toHaveCount(0);
  await expect(btn(row(p), "编辑")).toBeFocused();
  assert.equal(s.requests.length, 0);
  return {
    geometry,
    accessibleIndexedLabels: true,
    noVisibleNumbering: true,
    keyboardOrder: true,
    cleanCancelZeroWrites: true,
  };
}
async function nonChat(d, type) {
  const expected =
    type === "embedding"
      ? ["文本", "向量"]
      : type === "reranker"
        ? ["查询与文档", "排序结果"]
        : ["文本", "图像"];
  await expect(d.locator(".fixed-value")).toHaveText(expected);
  for (const label of [
    schemaLabel,
    "支持思考",
    "工具调用",
    "并行工具调用",
    "流式输出",
  ])
    await expect(check(d, label)).toHaveCount(0);
  await expect(field(d, "最大输出（可选）")).toHaveCount(0);
  if (type === "embedding") await advanced(d);
  else {
    await expect(d.locator("details")).toHaveCount(0);
    await expect(field(d, "上下文长度（可选）")).toHaveCount(0);
  }
}
async function fourTypes({ p, s }) {
  stage = "four-types-list";
  await expect(
    row(p, "sample-embedding").getByText("嵌入", { exact: true }),
  ).toBeVisible();
  await expect(
    row(p, "sample-reranker").getByText("重排", { exact: true }),
  ).toBeVisible();
  await expect(
    row(p, "sample-image").getByText("图像生成", { exact: true }),
  ).toBeVisible();
  for (const [index, type, modelID, label, image] of [
    [1, "embedding", "sample-embedding", "嵌入", "11-embedding-light"],
    [2, "reranker", "sample-reranker", "重排", "12-reranker-light"],
    [
      3,
      "image_generation",
      "sample-image",
      "图像生成",
      "13-image-generation-light",
    ],
  ]) {
    stage = "edit-" + type;
    const d = await editor(p, modelID);
    await expect(
      d.getByRole("combobox", { name: "模型类型", exact: true }),
    ).toContainText(label);
    await nonChat(d, type);
    await capture(p, d, image);
    const before = structuredClone(s.models[index].input);
    await field(d, "模型名称").fill(before.name + "示例");
    if (type === "embedding")
      await field(d, "上下文长度（可选）").fill("16384");
    await btn(d, "保存").click();
    await finish(p, d, modelID);
    const expected = structuredClone(before);
    expected.name += "示例";
    if (type === "embedding") expected.capabilities.context_length = "16384";
    assert.deepEqual(s.requests.at(-1).body, {
      expected_version: "1",
      input: expected,
    });
  }
  assert.equal(s.requests.length, 3);
  return {
    labelsEmbeddingRenamed: true,
    allExistingTypesLocked: true,
    exactNonChatWrites: 3,
    rerankerOutputEmpty: true,
    imageOutputImage: true,
    newTypesNoAdvanced: true,
  };
}
async function createFour({ p, s }) {
  stage = "create-four-options";
  await btn(p, "添加模型").click();
  const d = p.getByRole("dialog", { name: "添加模型", exact: true });
  await field(d, "模型名称").fill("图像生成配置示例");
  await field(d, "模型 ID").fill("sample-new-image");
  const type = d.getByRole("combobox", { name: "模型类型", exact: true });
  await expect(type).toBeEnabled();
  await type.click();
  await expect(p.getByRole("option")).toHaveText([
    "对话",
    "嵌入",
    "重排",
    "图像生成",
  ]);
  await p.getByRole("option", { name: "对话", exact: true }).click();
  await check(d, schemaLabel).check();
  await check(d, "支持思考").check();
  await btn(d, "添加思考等级").click();
  await field(d, "等级名称 1").fill("深入思考");
  await field(d, "等级值 1").fill("high");
  await advanced(d);
  await field(d, "上下文长度（可选）").fill("8192");
  await field(d, "最大输出（可选）").fill("1024");
  for (const [type, label] of [
    ["embedding", "嵌入"],
    ["reranker", "重排"],
    ["image_generation", "图像生成"],
  ]) {
    await selectType(p, d, label);
    await nonChat(d, type);
  }
  await capture(p, d, "15-create-image-generation-dark");
  await btn(d, "保存").click();
  await expect(d).toContainText("模型已保存。");
  assert.equal(s.requests.length, 1);
  assert.equal(s.requests[0].method, "POST");
  assert.deepEqual(s.requests[0].body, {
    provider_id: s.provider.id,
    input: {
      name: "图像生成配置示例",
      provider_model_id: "sample-new-image",
      type: "image_generation",
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
        output_modalities: ["image"],
        reasoning_efforts: [],
        structured_output_modes: [],
        context_length: null,
        max_output: null,
        embedding: null,
      },
    },
  });
  await btn(d, "完成").click();
  await expect(d).toHaveCount(0);
  await expect(row(p, "sample-new-image")).toBeVisible();
  return {
    fourSelectableTypes: true,
    hiddenChatFieldsExcluded: true,
    imageCreateExactPayload: true,
  };
}

async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(
    !only || ["compact", "four-types", "create-four", "narrow"].includes(only),
  );
  const results = [],
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
  try {
    for (const [name, work] of [
      ["compact", compact],
      ["four-types", fourTypes],
      ["create-four", createFour],
      ["narrow", narrow],
    ]) {
      if (only && only !== name) continue;
      const c = await browser.newContext({
        viewport:
          name === "narrow"
            ? { width: 390, height: 844 }
            : { width: 1440, height: 1000 },
        colorScheme: ["narrow", "four-types"].includes(name) ? "light" : "dark",
        reducedMotion: name === "narrow" ? "reduce" : "no-preference",
        serviceWorkers: "block",
      });
      try {
        stage = "load";
        const data = await setup(c, name);
        const detail = await work(data);
        assert.deepEqual(data.s.errors, []);
        assert.deepEqual(data.s.unexpected, []);
        results.push({
          name,
          outcome: "observed",
          detail,
          timeline: data.s.timeline,
        });
      } catch (e) {
        const p = c.pages().at(-1);
        const diagnostic =
          p && !p.isClosed()
            ? {
                locatorCount: await table(p).count(),
                locatorVisible: await table(p).isVisible(),
                dom: await p.evaluate(() => ({
                  readyState: document.readyState,
                  tables: [...document.querySelectorAll("table")].map((e) => {
                    const r = e.getBoundingClientRect();
                    const c = getComputedStyle(e);
                    return {
                      ariaLabel: e.getAttribute("aria-label"),
                      role: e.getAttribute("role"),
                      hidden: e.hidden,
                      width: r.width,
                      height: r.height,
                      display: c.display,
                      visibility: c.visibility,
                      hiddenAncestor:
                        e.closest('[hidden],[aria-hidden="true"]')?.tagName ??
                        null,
                      rows: [...e.rows].map((row) =>
                        row.cells[0]?.textContent?.trim(),
                      ),
                    };
                  }),
                })),
              }
            : null;
        if (p && !p.isClosed())
          await shot(p, "failure-" + name).catch(() => {});
        results.push({
          name,
          outcome: "failed",
          stage,
          check: String(e.message),
          diagnostic,
          timeline: c.review?.timeline,
          location: String(e.stack)
            .split("\n")
            .filter((x) => x.includes("capability-form-visual.cjs:"))
            .slice(0, 2),
          api: c.review?.api,
          unexpected: c.review?.unexpected,
          errors: c.review?.errors,
          focus:
            p && !p.isClosed()
              ? await p.evaluate(() => ({
                  tag: document.activeElement?.tagName,
                  label: document.activeElement?.getAttribute("aria-label"),
                  inOpen: !!document.activeElement?.closest("dialog[open]"),
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
        boundary: "Synthetic API only; no provider or real backend requests",
        results,
        captures,
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
