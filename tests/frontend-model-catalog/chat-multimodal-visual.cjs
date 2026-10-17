// Independent real Chromium visual/interaction review. Every API and input file is synthetic.
// node tests/frontend-model-catalog/chat-multimodal-visual.cjs --owned chat-multimodal-visual --input <revision> --run <label> [--only desktop|narrow|capabilities|zoom|removal] [--tmp-output]
// Boundaries: no real backend, model provider, native OS picker or physical mobile keyboard.
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const args = process.argv.slice(2),
  arg = (key) => args[args.indexOf(key) + 1];
assert.equal(arg("--owned"), "chat-multimodal-visual");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const run = arg("--run"),
  input = arg("--input"),
  only = args.includes("--only") ? arg("--only") : null;
assert(/^[a-z0-9-]+$/.test(run));
assert(input && !input.startsWith("--"));
assert(
  !only ||
    ["desktop", "narrow", "capabilities", "zoom", "removal"].includes(only),
);
const origin = "http://127.0.0.1:5281";
const out = args.includes("--tmp-output")
  ? path.join("/tmp/agenteam-chat-multimodal-visual", run)
  : path.resolve(__dirname, "../../output/ai/chat-multimodal-visual", run);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jF9kAAAAASUVORK5CYII=",
  "base64",
);
const imageFile = {
  name: "合成示意图.png",
  mimeType: "image/png",
  buffer: png,
};
const textFile = {
  name: "附带说明-测试中的中文和English-long-name.txt",
  mimeType: "text/plain",
  buffer: Buffer.from("第一行：测试文字。\n第二行：Synthetic only."),
};
const button = (p, name) =>
  name === "关闭"
    ? p.getByRole("button", { name, exact: true }).filter({ hasText: /^关闭$/ })
    : p.getByRole("button", { name, exact: true });
let stage = "prepare";
async function shot(page, label, nativeZoom = false) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((r) =>
      requestAnimationFrame(() => requestAnimationFrame(r)),
    );
  });
  let pixels;
  if (nativeZoom) {
    // Playwright viewport clips use CSS dimensions; native browser zoom needs
    // the complete compositor surface to avoid cropping the physical image.
    const session = await page.context().newCDPSession(page);
    try {
      const result = await session.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
      });
      pixels = Buffer.from(result.data, "base64");
      await fs.writeFile(path.join(out, label + ".png"), pixels);
    } finally {
      await session.detach();
    }
  } else {
    pixels = await page.screenshot({
      path: path.join(out, label + ".png"),
      fullPage: false,
    });
  }
  return { width: pixels.readUInt32BE(16), height: pixels.readUInt32BE(20) };
}
async function geometry(page) {
  const value = await page.evaluate(() => {
    const d = document.querySelector("dialog[open]"),
      b = d.getBoundingClientRect();
    return {
      inner: innerWidth,
      root: document.documentElement.scrollWidth,
      dialog: { x: b.x, y: b.y, width: b.width, height: b.height },
      height: innerHeight,
      content: d.querySelector(".ui-dialog-body").scrollWidth,
      client: d.querySelector(".ui-dialog-body").clientWidth,
    };
  });
  assert(
    value.root <= value.inner && value.content <= value.client,
    "horizontal overflow",
  );
  assert(
    value.dialog.x >= 0 &&
      value.dialog.y >= 0 &&
      value.dialog.x + value.dialog.width <= value.inner + 1 &&
      value.dialog.y + value.dialog.height <= value.height + 1,
    "dialog clipped by viewport",
  );
  return value;
}
async function open(page) {
  await button(page, "测试").focus();
  await page.keyboard.press("Enter");
  const d = page.getByRole("dialog", { name: "测试模型", exact: true });
  await expect(d).toBeVisible();
  return d;
}
async function close(page, d) {
  await button(d, "关闭").click();
  await expect(d).not.toBeVisible();
  await expect(button(page, "测试")).toBeFocused();
}
async function setup(context, options = {}) {
  const provider = {
    id: id(1),
    version: "3",
    created_at: at,
    updated_at: at,
    input: {
      name: "示例供应商",
      protocol: "openai-compatible",
      base_url: "https://example.invalid/v1",
      enabled: true,
      credential_ref: null,
      options: {},
    },
  };
  const model = {
    id: id(10),
    provider_id: id(1),
    version: "2",
    created_at: at,
    updated_at: at,
    input: {
      notes: "支持图像与文件的合成测试模型",
      provider_model_id: "sample-multimodal",
      type: "chat",
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
        input_modalities: options.modalities ?? ["text", "image", "file"],
        output_modalities: ["text"],
        reasoning_efforts: [],
        structured_output_modes: ["text"],
        context_length: "8192",
        max_output: "1024",
        embedding: null,
      },
    },
  };
  const user = {
    id: id(90),
    email: "author@example.invalid",
    username: "author",
    display_name: "合成测试",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  };
  const s = { requests: [], errors: [], unexpected: [], api: [], accepted: 0 };
  context.review = s;
  s.answer =
    options.answer ?? "已收到合成图片与文件。\n<em>这段回答应按原文显示</em>。";
  await context.routeWebSocket("**/*", (ws) => ws.close());
  await context.route("**/*", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      ep = url.pathname,
      method = req.method();
    if (url.origin !== origin) {
      s.unexpected.push("outside");
      return route.abort("blockedbyclient");
    }
    if (!ep.startsWith("/api/")) {
      if (method === "GET" && !["fetch", "xhr"].includes(req.resourceType()))
        return route.continue();
      s.unexpected.push("non-static");
      return route.abort("blockedbyclient");
    }
    s.api.push(method + " " + ep);
    const reply = (data, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(data),
      });
    if (method === "GET" && ep === "/api/v1/session")
      return reply({
        user,
        session: {
          id: id(91),
          issued_at: at,
          absolute_expires_at: "2027-01-01T00:00:00.000000Z",
          idle_expires_at: "2027-01-01T00:00:00.000000Z",
        },
        csrf_token: "synthetic-csrf",
      });
    if (method === "GET" && ep === "/api/v1/me")
      return reply({ user, avatar: null });
    if (
      method === "GET" &&
      ep === `/api/v1/system/model-providers/${provider.id}`
    )
      return reply(provider);
    if (method === "GET" && ep === "/api/v1/system/models")
      return reply({
        items: [
          {
            ...model,
            provider: {
              id: provider.id,
              name: provider.input.name,
              protocol: provider.input.protocol,
              enabled: true,
              version: provider.version,
            },
          },
        ],
        next_cursor: null,
      });
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
    if (method === "POST" && ep === `/api/v1/system/models/${model.id}/test`) {
      const body = req.postData(),
        key = req.headers()["idempotency-key"];
      s.requests.push({ body, key });
      if (s.requests.length === 1) {
        s.accepted++;
        if (options.unknown) return route.abort("connectionfailed");
      } else {
        assert.equal(key, s.requests[0].key);
        assert.equal(body, s.requests[0].body);
      }
      return reply({
        operation_id: id(40),
        provider_id: provider.id,
        provider_version: provider.version,
        model_id: model.id,
        model_version: model.version,
        outcome: "succeeded",
        reason: null,
        duration_ms: options.duration ?? 21000,
        created_at: at,
        answer_text: s.answer,
        answer_truncated: !!options.truncated,
      });
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => s.errors.push(e.message));
  const ready = Promise.all(
    ["/api/v1/system/models", "/api/v1/system/model-capabilities"].map((ep) =>
      page
        .waitForResponse(
          (r) =>
            new URL(r.url()).pathname === ep && r.request().method() === "GET",
        )
        .then((r) => r.finished()),
    ),
  );
  await page.goto(`${origin}/system/models/${provider.id}`, {
    waitUntil: "domcontentloaded",
  });
  if (ready) await ready;
  page.setDefaultTimeout(5000);
  await expect(
    page.getByRole("table", { name: "供应商模型", exact: true }),
  ).toBeVisible();
  return { page, s };
}

async function desktop({ page, s }) {
  let d = await open(page);
  assert.equal(s.requests.length, 0);
  stage = "desktop-keyboard-picker";
  const text = d.getByLabel("测试文本", { exact: true });
  await text.focus();
  await page.keyboard.press("Tab");
  await expect(button(d, "选择附件")).toBeFocused();
  const chooser = page.waitForEvent("filechooser");
  await page.keyboard.press("Enter");
  await (await chooser).setFiles([imageFile, textFile]);
  await expect(
    d.getByRole("list", { name: "已选附件" }).getByRole("listitem"),
  ).toHaveCount(2);
  await text.fill("请描述图片，并概括附件文字。");
  stage = "desktop-format-rejection";
  await d.getByLabel("添加附件", { exact: true }).setInputFiles({
    name: "unsupported.zip",
    mimeType: "application/zip",
    buffer: Buffer.from("synthetic"),
  });
  await expect(d.getByRole("alert")).toContainText("不支持此文件格式。");
  assert.equal(s.requests.length, 0);
  await expect(d.getByRole("listitem")).toHaveCount(2);
  await shot(page, "desktop-format-rejection");
  stage = "desktop-removal";
  await button(d, "移除附件 " + textFile.name).click();
  await expect(d.getByRole("listitem")).toHaveCount(1);
  await d.getByLabel("添加附件", { exact: true }).setInputFiles(textFile);
  await expect(d.getByRole("listitem")).toHaveCount(2);
  await expect(d.getByRole("alert")).toHaveCount(0);
  await shot(page, "desktop-input");
  const size = await geometry(page);
  stage = "desktop-send-answer";
  await button(d, "开始测试").click();
  await expect(d.getByText("本次测试成功。", { exact: true })).toBeVisible();
  await expect(d.getByText(s.answer, { exact: true })).toBeVisible();
  await expect(d.locator("em")).toHaveCount(0);
  assert.equal(s.requests.length, 1);
  assert.deepEqual(JSON.parse(s.requests[0].body), {
    expected_version: "2",
    provider_version: "3",
    input: {
      text: "请描述图片，并概括附件文字。",
      attachments: [imageFile, textFile].map((f) => ({
        name: f.name,
        media_type: f.mimeType,
        data_base64: f.buffer.toString("base64"),
      })),
    },
  });
  await shot(page, "desktop-result");
  stage = "desktop-focus-close";
  const header = d.getByRole("button", { name: "关闭", exact: true }).first();
  await button(d, "关闭").focus();
  await page.keyboard.press("Tab");
  await expect(header).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(button(d, "关闭")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(d).not.toBeVisible();
  await expect(button(page, "测试")).toBeFocused();
  d = await open(page);
  await expect(d.getByRole("list", { name: "已选附件" })).toHaveCount(0);
  await expect(d.getByLabel("测试文本", { exact: true })).toHaveValue(
    "请简要说明收到的内容。",
  );
  await close(page, d);
  return {
    size,
    filesExact: true,
    formatRejected: true,
    keyboardPicker: true,
    plainAnswer: true,
    focusRestored: true,
  };
}
async function narrow({ page, s }) {
  const d = await open(page);
  stage = "narrow-attachment-only";
  await d.getByLabel("测试文本", { exact: true }).fill("");
  await d
    .getByLabel("添加附件", { exact: true })
    .setInputFiles([imageFile, textFile]);
  await expect(d.getByRole("listitem")).toHaveCount(2);
  await shot(page, "narrow-input");
  stage = "narrow-unknown";
  await button(d, "开始测试").click();
  await expect(button(d, "恢复原操作")).toBeVisible();
  await expect(d.getByLabel("测试文本", { exact: true })).toBeDisabled();
  await expect(button(d, "选择附件")).toBeDisabled();
  await expect(button(d, "移除附件 " + imageFile.name)).toBeDisabled();
  await expect(button(d, "关闭")).toBeDisabled();
  assert.equal(s.requests.length, 1);
  await page.keyboard.press("Escape");
  await expect(d).toBeVisible();
  await d.getByRole("button", { name: "关闭", exact: true }).first().click();
  await expect(d).toBeVisible();
  await shot(page, "narrow-unknown");
  await geometry(page);
  stage = "narrow-recovery";
  await button(d, "恢复原操作").focus();
  await page.keyboard.press("Enter");
  await expect(d.getByText("本次测试成功。", { exact: true })).toBeVisible();
  assert.equal(s.requests.length, 2);
  assert.equal(s.accepted, 1);
  assert.equal(s.requests[0].key, s.requests[1].key);
  assert.equal(s.requests[0].body, s.requests[1].body);
  assert.equal(JSON.parse(s.requests[0].body).input.text, "");
  await expect(
    d.getByText("回答较长，仅显示前 8 KiB 内容。", { exact: true }),
  ).toBeVisible();
  await expect(d.getByText(s.answer, { exact: true })).toHaveText(s.answer);
  stage = "narrow-long-answer-scroll";
  await button(d, "关闭").scrollIntoViewIfNeeded();
  await expect(button(d, "关闭")).toBeInViewport();
  const size = await geometry(page);
  await shot(page, "narrow-result-bottom");
  const scroll = await d.locator(".ui-dialog-body").evaluate((e) => ({
    top: e.scrollTop,
    height: e.clientHeight,
    total: e.scrollHeight,
  }));
  assert(
    scroll.top > 0 && scroll.total > scroll.height,
    "long response should scroll locally",
  );
  await d.locator(".ui-dialog-body").evaluate((e) => (e.scrollTop = 0));
  await expect(
    d.getByRole("heading", { name: "测试模型", exact: true }),
  ).toBeInViewport();
  await shot(page, "narrow-result-top");
  await close(page, d);
  return {
    size,
    scroll,
    attachmentOnly: true,
    originalRequestRecovered: true,
    unknownCloseBlocked: true,
  };
}
async function capabilities({ page, s }) {
  const d = await open(page);
  stage = "capability-file-rejection";
  await expect(d.getByLabel("添加附件", { exact: true })).toHaveAttribute(
    "accept",
    /image\/png/,
  );
  assert(
    !(
      await d.getByLabel("添加附件", { exact: true }).getAttribute("accept")
    ).includes("text/plain"),
  );
  await d.getByLabel("添加附件", { exact: true }).setInputFiles(textFile);
  await expect(d.getByRole("alert")).toContainText(
    "此模型未声明文件输入能力。",
  );
  await expect(d.getByRole("list", { name: "已选附件" })).toHaveCount(0);
  assert.equal(s.requests.length, 0);
  await shot(page, "capability-rejection");
  stage = "empty-text-rejection";
  await d.getByLabel("测试文本", { exact: true }).fill("   ");
  await button(d, "开始测试").click();
  await expect(d.getByRole("alert")).toContainText(
    "请输入测试文本或添加附件。",
  );
  assert.equal(s.requests.length, 0);
  stage = "text-only-success";
  await d.getByLabel("测试文本", { exact: true }).fill("只发送这一段文字。");
  await button(d, "开始测试").click();
  await expect(d.getByText("本次测试成功。", { exact: true })).toBeVisible();
  assert.deepEqual(JSON.parse(s.requests[0].body).input, {
    text: "只发送这一段文字。",
    attachments: [],
  });
  await close(page, d);
  return {
    unsupportedCapabilityRejected: true,
    emptyRejected: true,
    textOnly: true,
  };
}
async function zoomWork({ page, s }) {
  stage = "native-200-reduced-motion";
  const measured = await page.evaluate(() => ({
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
  const d = await open(page);
  await d
    .getByLabel("添加附件", { exact: true })
    .setInputFiles([imageFile, textFile]);
  await expect(d.getByRole("listitem")).toHaveCount(2);
  await button(d, "开始测试").click();
  await expect(d.getByText("本次测试成功。", { exact: true })).toBeVisible();
  await button(d, "关闭").scrollIntoViewIfNeeded();
  await expect(button(d, "关闭")).toBeInViewport();
  const size = await geometry(page);
  const capture = await shot(page, "native-200-reduced-result", true);
  assert.equal(
    capture.width,
    measured.outer,
    "Native screenshot must show the full physical viewport",
  );
  await close(page, d);
  return { measured, size, capture, requests: s.requests.length };
}
async function removal({ page, s }) {
  const d = await open(page);
  stage = "keyboard-last-attachment-removal";
  await d.getByLabel("添加附件", { exact: true }).setInputFiles(imageFile);
  await expect(d.getByRole("listitem")).toHaveCount(1);
  await button(d, "移除附件 " + imageFile.name).focus();
  await page.keyboard.press("Enter");
  await expect(d.getByRole("list", { name: "已选附件" })).toHaveCount(0);
  s.observations = await d.evaluate((node) => ({
    withinDialog: node.contains(document.activeElement),
    activeTag: document.activeElement?.tagName,
    activeLabel: document.activeElement?.getAttribute("aria-label"),
  }));
  await shot(page, "keyboard-attachment-removed");
  assert(
    s.observations.withinDialog,
    "Keyboard removal moved focus outside the modal",
  );
  assert.equal(s.requests.length, 0);
  await close(page, d);
  return s.observations;
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const results = [];
  let browser;
  async function record(context, name, options, work) {
    let page, s;
    stage = "load";
    const budget = setTimeout(() => context.close().catch(() => {}), 45000);
    try {
      ({ page, s } = await setup(context, options));
      const detail = await work({ page, s });
      assert.deepEqual(s.errors, []);
      assert.deepEqual(s.unexpected, []);
      results.push({
        name,
        outcome: "passed",
        detail,
        requests: s.requests.length,
        externalRequests: 0,
      });
    } catch (e) {
      page ??= context.pages().at(-1);
      s ??= context.review;
      if (page && !page.isClosed())
        await shot(page, `${name}-failure`).catch(() => {});
      results.push({
        name,
        outcome: "failed",
        stage,
        error: e.message,
        errors: s?.errors,
        unexpected: s?.unexpected,
        api: s?.api,
        observations: s?.observations,
      });
    } finally {
      clearTimeout(budget);
      await context.close();
    }
    console.log(JSON.stringify(results.at(-1)));
  }
  try {
    if (only !== "zoom") {
      browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        headless: true,
        args: ["--no-sandbox"],
      });
      for (const name of [
        "desktop",
        "narrow",
        "capabilities",
        "removal",
      ].filter((x) => !only || x === only)) {
        const context = await browser.newContext({
          viewport:
            name === "narrow"
              ? { width: 390, height: 844 }
              : { width: 1440, height: 1000 },
          colorScheme: name === "narrow" ? "light" : "dark",
          serviceWorkers: "block",
        });
        const options =
          name === "narrow"
            ? {
                unknown: true,
                truncated: true,
                answer: Array.from(
                  { length: 26 },
                  (_, i) =>
                    `第 ${i + 1} 行：这是合成的多行回答，用于检查长文本、局部滚动与末尾关闭入口。`,
                ).join("\n"),
              }
            : name === "capabilities"
              ? { modalities: ["text", "image"], duration: 57 }
              : {};
        await record(
          context,
          name,
          options,
          { desktop, narrow, capabilities, removal }[name],
        );
      }
      await browser.close();
      browser = undefined;
    }
    if (!only || only === "zoom") {
      const profile = await fs.mkdtemp(
        path.join(require("node:os").tmpdir(), "chat-multimodal-zoom-"),
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
          headless: true,
          args: ["--no-sandbox", "--window-size=1440,960"],
          viewport: null,
          colorScheme: "dark",
          reducedMotion: "reduce",
          serviceWorkers: "block",
        });
        await record(context, "zoom", {}, zoomWork);
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
        input,
        boundary:
          "Real Chromium; synthetic API and files; no backend/provider verification",
        browserClosed: true,
        results,
      },
      null,
      2,
    ),
  );
  if (results.some((x) => x.outcome !== "passed")) process.exitCode = 1;
}
main().catch((e) => {
  console.error(e.message);
  process.exitCode = 1;
});
