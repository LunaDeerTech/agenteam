// Independent controlled review of supplier-detail model actions. No real backend or provider.
// node model-capabilities-visual.cjs --base-url http://127.0.0.1:5281 --owned model-capabilities-visual --input <revision> --run <label> [--only desktop|narrow] [--check]
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
assert.equal(arg("--owned"), "model-capabilities-visual");
assert(/^[a-z0-9-]+$/.test(run));
const out = path.resolve(
  __dirname,
  "../../output/ai/model-capabilities-visual",
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
  ];
  if (variant === "narrow") {
    models[0].input.capabilities.input_modalities = ["text", "image", "file"];
    models[0].input.capabilities.output_modalities = ["text", "image"];
    models[0].input.capabilities.reasoning = true;
    models[0].input.capabilities.reasoning_efforts = ["vendor-depth"];
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
    if (method === "PUT" && ep === mp) {
      const body = JSON.parse(req.postData()),
        key = req.headers()["idempotency-key"];
      assert(key);
      s.requests.push({ method, ep, key, body });
      if (s.holdWrite) await s.holdWrite.promise;
      assert.equal(body.expected_version, models[0].version);
      assert.deepEqual(Object.keys(body).sort(), ["expected_version", "input"]);
      models[0] = {
        ...models[0],
        version: String(BigInt(models[0].version) + 1n),
        input: structuredClone(body.input),
      };
      return reply({
        kind: "model.update",
        resource_id: models[0].id,
        version: models[0].version,
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
  await p.goto(origin + "/system/models/" + id(1), {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(table(p)).toBeVisible();
  return { p, s };
}
const group = (d, name) => d.getByRole("group", { name, exact: true });
const choice = (d, name, label) =>
  group(d, name).getByRole("checkbox", { name: label, exact: true });
async function openEditor(p) {
  await btn(row(p), "编辑").click();
  const d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  await expect(d).toBeVisible();
  return d;
}
async function checkChoices(d, input, output) {
  for (const [name, selected] of [
    ["输入模态", input],
    ["输出模态", output],
  ]) {
    await expect(group(d, name).getByRole("checkbox")).toHaveCount(4);
    for (const label of ["文本", "图像", "文件", "向量"])
      await expect(choice(d, name, label)).toBeChecked({
        checked: selected.includes(label),
      });
  }
}
async function dialogDimensions(p, d) {
  const value = await d.evaluate((e) => ({
    width: e.getBoundingClientRect().width,
    left: e.getBoundingClientRect().left,
    right: e.getBoundingClientRect().right,
    scroll: e.scrollWidth,
    client: e.clientWidth,
    inner: innerWidth,
  }));
  assert(
    value.left >= 0 &&
      value.right <= value.inner &&
      value.scroll <= value.client,
    "dialog horizontal overflow",
  );
  return value;
}
async function desktop({ p, s }) {
  stage = "desktop-existing-selections";
  let d = await openEditor(p);
  await checkChoices(d, ["文本"], ["文本"]);
  const boxes = await Promise.all([
    group(d, "输入模态").boundingBox(),
    group(d, "输出模态").boundingBox(),
  ]);
  assert(
    Math.abs(boxes[0].y - boxes[1].y) <= 0.5,
    "modality group top misalignment",
  );
  assert(
    Math.abs(boxes[0].width - boxes[1].width) <= 0.5,
    "modality group width mismatch",
  );
  await shot(p, "capabilities-desktop-dark");
  await dialogDimensions(p, d);
  const before = structuredClone(s.models[0].input);
  stage = "keyboard-multi-select";
  await choice(d, "输入模态", "图像").focus();
  await p.keyboard.press("Space");
  await expect(choice(d, "输入模态", "图像")).toBeChecked();
  await p.keyboard.press("Tab");
  await expect(choice(d, "输入模态", "文件")).toBeFocused();
  await p.keyboard.press("Space");
  await p.keyboard.press("Shift+Tab");
  await expect(choice(d, "输入模态", "图像")).toBeFocused();
  await p.keyboard.press("Space");
  await choice(d, "输出模态", "图像").check();
  stage = "empty-group-validation";
  await choice(d, "输入模态", "文本").uncheck();
  await choice(d, "输入模态", "文件").uncheck();
  await btn(d, "保存").click();
  await expect(d.getByRole("alert")).toContainText("输入和输出模态");
  assert.equal(writes(s).length, 0);
  await choice(d, "输入模态", "文本").check();
  await choice(d, "输入模态", "文件").check();
  stage = "capability-payload-save";
  s.holdWrite = gate();
  await btn(d, "保存").click();
  try {
    await expect.poll(() => writes(s).length).toBe(1);
    for (const name of ["输入模态", "输出模态"])
      for (const c of await group(d, name).getByRole("checkbox").all())
        await expect(c).toBeDisabled();
  } finally {
    s.holdWrite.release();
  }
  await expect(d).toContainText("模型已保存。");
  assert.deepEqual(writes(s)[0].body, {
    expected_version: "1",
    input: {
      ...before,
      capabilities: {
        ...before.capabilities,
        input_modalities: ["text", "file"],
        output_modalities: ["text", "image"],
      },
    },
  });
  await btn(d, "完成").click();
  await expect(btn(row(p), "编辑")).toBeFocused();
  d = await openEditor(p);
  await checkChoices(d, ["文本", "文件"], ["文本", "图像"]);
  await btn(d, "取消").click();
  await expect(d).toHaveCount(0);
  await expect(btn(row(p), "编辑")).toBeFocused();
  assert.equal(writes(s).length, 1);
  return {
    topDelta: Math.abs(boxes[0].y - boxes[1].y),
    widthDelta: Math.abs(boxes[0].width - boxes[1].width),
    existingSelection: true,
    keyboardSpaceTabShiftTab: true,
    emptyGroupZeroWrites: true,
    pendingDisabled: true,
    exactCapabilityArrays: true,
    otherInputPreserved: true,
    readbackSelections: true,
    closeFocus: true,
  };
}
async function narrow({ p, s }) {
  stage = "narrow-existing-multi-selection";
  let d = await openEditor(p);
  await checkChoices(d, ["文本", "图像", "文件"], ["文本", "图像"]);
  await expect(d.getByLabel("推理档位", { exact: true })).toHaveValue(
    "vendor-depth",
  );
  await group(d, "输入模态").scrollIntoViewIfNeeded();
  const a = await group(d, "输入模态").boundingBox(),
    b = await group(d, "输出模态").boundingBox();
  assert(b.y >= a.y + a.height, "narrow modality groups overlap");
  await shot(p, "capabilities-narrow-light");
  const dimensions = await dialogDimensions(p, d);
  const before = structuredClone(s.models[0].input);
  stage = "narrow-cancel-focus";
  await choice(d, "输入模态", "文件").uncheck();
  await p.keyboard.press("Escape");
  const discard = p.getByRole("dialog", {
    name: "放弃尚未保存的编辑？",
    exact: true,
  });
  await expect(btn(discard, "继续编辑")).toBeFocused();
  await p.keyboard.press("Enter");
  d = p.getByRole("dialog", { name: "编辑模型", exact: true });
  await expect(d.getByLabel("模型名称", { exact: true })).toBeFocused();
  await p.keyboard.press("Escape");
  await btn(discard, "放弃修改").focus();
  await p.keyboard.press("Enter");
  await expect(discard).toHaveCount(0);
  await expect(btn(row(p), "编辑")).toBeFocused();
  assert.equal(writes(s).length, 0);
  assert.deepEqual(s.models[0].input, before);
  return {
    stacked: true,
    existingMultiSelection: true,
    customReasoningPreservedInDraft: true,
    cancelZeroWrites: true,
    keyboardCancelAndContinueFocus: true,
    reducedMotion: await p.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
    dimensions,
  };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const only = args.includes("--only") ? arg("--only") : undefined;
  assert(!only || ["desktop", "narrow"].includes(only));
  const results = [],
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
  try {
    for (const [name, work] of [
      ["desktop", desktop],
      ["narrow", narrow],
    ]) {
      if (only && only !== name) continue;
      const c = await browser.newContext({
        viewport:
          name === "narrow"
            ? { width: 390, height: 844 }
            : { width: 1440, height: 1000 },
        colorScheme: name === "narrow" ? "light" : "dark",
        serviceWorkers: "block",
        reducedMotion: name === "narrow" ? "reduce" : "no-preference",
      });
      try {
        stage = "load";
        const data = await setup(c, name),
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
            .filter((x) => x.includes("model-capabilities-visual.cjs:"))
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
