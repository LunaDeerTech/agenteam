// Author-controlled Chat attachment test. All API responses are synthetic.
// node web/tests/model-test-input.browser.cjs --owned chat-multimodal-vue --phased
const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
assert.equal(
  process.argv[process.argv.indexOf("--owned") + 1],
  "chat-multimodal-vue",
);
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const origin = "http://127.0.0.1:5281";
const out = path.resolve(
  __dirname,
  "../../output/ai/chat-multimodal-author",
  process.argv.includes("--diagnostic")
    ? "diagnostic"
    : process.argv.includes("--phased")
      ? "phased"
      : "first",
);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jF9kAAAAASUVORK5CYII=",
  "base64",
);
const button = (p, name) =>
  name === "关闭"
    ? p.getByRole("button", { name, exact: true }).filter({ hasText: /^关闭$/ })
    : p.getByRole("button", { name, exact: true });
async function setup(context, unknown) {
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
        input_modalities: ["text", "image", "file"],
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
        if (unknown) return route.abort("connectionfailed");
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
        duration_ms: 21000,
        created_at: at,
        answer_text:
          "已收到一张图像和一个文本文件。\n这是合成回答，用于验证显示与请求恢复。",
        answer_truncated: false,
      });
    }
    s.unexpected.push(method + " " + ep);
    return route.abort("blockedbyclient");
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => s.errors.push(e.message));
  const ready = process.argv.includes("--phased")
    ? Promise.all(
        ["/api/v1/system/models", "/api/v1/system/model-capabilities"].map(
          (ep) =>
            page
              .waitForResponse(
                (r) =>
                  new URL(r.url()).pathname === ep &&
                  r.request().method() === "GET",
              )
              .then((r) => r.finished()),
        ),
      )
    : undefined;
  await page.goto(`${origin}/system/models/${provider.id}`, {
    waitUntil: "domcontentloaded",
  });
  if (ready) await ready;
  await expect(
    page.getByRole("table", { name: "供应商模型", exact: true }),
  ).toBeVisible();
  return { page, s };
}
async function main() {
  await fs.mkdir(out, { recursive: true });
  const browser = await chromium.launch({
    executablePath: "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  const results = [];
  try {
    for (const narrow of process.argv.includes("--diagnostic")
      ? [false]
      : [false, true]) {
      const context = await browser.newContext({
        viewport: narrow
          ? { width: 390, height: 844 }
          : { width: 1440, height: 1000 },
        colorScheme: narrow ? "light" : "dark",
        serviceWorkers: "block",
      });
      let page,
        s,
        stage = "load";
      try {
        ({ page, s } = await setup(context, narrow));
        await button(page, "测试").click();
        const dialog = page.getByRole("dialog", {
          name: "测试模型",
          exact: true,
        });
        await expect(dialog).toBeVisible();
        assert.equal(s.requests.length, 0);
        stage = "draft";
        await dialog
          .getByLabel("测试文本", { exact: true })
          .fill("请描述图片并概括附带文本。");
        await dialog.getByLabel("添加附件", { exact: true }).setInputFiles([
          { name: "pixel.png", mimeType: "image/png", buffer: png },
          {
            name: "说明.txt",
            mimeType: "text/plain",
            buffer: Buffer.from("附件中的文字。"),
          },
        ]);
        await expect(
          dialog.getByRole("list", { name: "已选附件" }).getByRole("listitem"),
        ).toHaveCount(2);
        assert.equal(s.requests.length, 0);
        await page.screenshot({
          path: path.join(
            out,
            narrow ? "narrow-input.png" : "desktop-input.png",
          ),
        });
        stage = "execute";
        await button(dialog, "开始测试").click();
        if (narrow) {
          await expect(
            dialog.getByText("结果尚未确认。恢复原操作不会再次发送测试请求。"),
          ).toBeVisible();
          await expect(
            dialog.getByLabel("测试文本", { exact: true }),
          ).toBeDisabled();
          await expect(button(dialog, "移除附件 pixel.png")).toBeDisabled();
          await expect(button(dialog, "关闭")).toBeDisabled();
          await button(dialog, "恢复原操作").click();
        }
        await expect(
          dialog.getByText("本次测试成功。", { exact: true }),
        ).toBeVisible();
        await expect(
          dialog.getByRole("heading", { name: "模型回答" }),
        ).toBeVisible();
        const body = JSON.parse(s.requests[0].body);
        assert.deepEqual(Object.keys(body).sort(), [
          "expected_version",
          "input",
          "provider_version",
        ]);
        assert.equal(body.input.text, "请描述图片并概括附带文本。");
        assert.equal(
          body.input.attachments[0].data_base64,
          png.toString("base64"),
        );
        assert.equal(
          Buffer.from(
            body.input.attachments[1].data_base64,
            "base64",
          ).toString(),
          "附件中的文字。",
        );
        assert.equal(s.accepted, 1);
        assert.equal(s.requests.length, narrow ? 2 : 1);
        stage = "result";
        await page.screenshot({
          path: path.join(
            out,
            narrow ? "narrow-result.png" : "desktop-result.png",
          ),
        });
        const size = await page.evaluate(() => ({
          root: document.documentElement.scrollWidth,
          inner: innerWidth,
          dialog: document.querySelector("dialog[open]").getBoundingClientRect()
            .width,
        }));
        assert(size.root <= size.inner && size.dialog <= size.inner);
        await button(dialog, "关闭").click();
        await expect(dialog).not.toBeVisible();
        await expect(button(page, "测试")).toBeFocused();
        await button(page, "测试").click();
        await expect(page.getByRole("list", { name: "已选附件" })).toHaveCount(
          0,
        );
        await button(page.getByRole("dialog"), "关闭").click();
        assert.deepEqual(s.errors, []);
        assert.deepEqual(s.unexpected, []);
        results.push({
          narrow,
          outcome: "passed",
          requests: s.requests.length,
          accepted: s.accepted,
          size,
        });
      } catch (e) {
        page ??= context.pages().at(-1);
        s ??= context.review;
        if (page)
          await page
            .screenshot({ path: path.join(out, `failure-${narrow}.png`) })
            .catch(() => {});
        results.push({
          narrow,
          outcome: "failed",
          stage,
          error: String(e.message),
          errors: s?.errors,
          unexpected: s?.unexpected,
          api: s?.api,
          body: page ? await page.locator("body").innerText() : null,
        });
      } finally {
        await context.close();
      }
      console.log(JSON.stringify(results.at(-1)));
    }
  } finally {
    await browser.close();
  }
  await fs.writeFile(
    path.join(out, "report.json"),
    JSON.stringify(
      {
        boundary: "Synthetic APIs; no external model call",
        browserClosed: true,
        results,
      },
      null,
      2,
    ),
  );
  if (results.some((r) => r.outcome === "failed")) process.exitCode = 1;
}
main().catch((e) => {
  console.error(e.message);
  process.exitCode = 1;
});
