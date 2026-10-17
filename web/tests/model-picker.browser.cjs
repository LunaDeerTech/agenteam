// Author smoke: synthetic APIs only. Run with the locked Playwright NODE_PATH.
const { chromium, expect } = require("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
if (process.argv.includes("--list")) {
  console.log("model-picker: desktop dark / narrow light anchored selection");
  process.exit(0);
}
assert(process.argv.includes("--owned-model-picker"));
const origin = "http://127.0.0.1:5281";
const output = process.env.MODEL_PICKER_OUTPUT;
assert(output?.startsWith("/tmp/agenteam-model-dimensions-"));
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const choice = (n) => ({ model_id: id(n), reasoning: { mode: "default" } });
const at = "2026-10-17T09:00:00.000000Z";
const provider = (n, name) => ({
  id: id(n),
  version: "1",
  created_at: at,
  updated_at: at,
  input: {
    name,
    protocol: "openai-compatible",
    base_url: "https://synthetic.example.invalid",
    enabled: true,
    credential_ref: null,
    options: {},
  },
});
const providers = [
  provider(1, "合成 OpenAI 服务"),
  provider(2, "备用模型服务"),
];
const model = (n, p, type, modelID) => ({
  id: id(n),
  provider_id: p.id,
  version: "1",
  created_at: at,
  updated_at: at,
  input: {
    notes: modelID,
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
      input_modalities: type === "chat" ? ["text", "image", "file"] : ["text"],
      output_modalities:
        type === "chat"
          ? ["text"]
          : type === "embedding"
            ? ["vector"]
            : type === "image_generation"
              ? ["image"]
              : [],
      reasoning_efforts: [],
      structured_output_modes: type === "chat" ? ["text", "json_schema"] : [],
      context_length:
        type === "chat" ? "128000" : type === "embedding" ? "8192" : null,
      max_output: null,
      embedding:
        type === "embedding"
          ? { dimensions: "1536", dimensions_mode: "fixed" }
          : null,
    },
  },
});
const sampleModels = [
  model(10, providers[0], "chat", "sample-chat"),
  model(11, providers[0], "embedding", "sample-embedding"),
  model(12, providers[1], "chat", "backup-summary"),
  model(13, providers[0], "reranker", "sample-rerank"),
  model(14, providers[0], "image_generation", "sample-image"),
];
async function run(browser, width, theme) {
  const models = structuredClone(sampleModels);
  const modelWrites = [];
  const context = await browser.newContext({
    viewport: { width, height: 960 },
    colorScheme: theme,
    serviceWorkers: "block",
  });
  const faults = [],
    api = [],
    writes = [];
  let stage = "prepare";
  const selection = {
    id: id(40),
    version: "1",
    configured: {
      embedding: choice(11),
      summary: choice(10),
      reranker: null,
      image: null,
    },
  };
  const user = {
    id: id(90),
    email: "author@example.invalid",
    username: "author",
    display_name: "合成作者检查",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  };
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      method = request.method();
    if (url.origin !== origin) {
      faults.push("outside-origin");
      return route.abort();
    }
    if (!url.pathname.startsWith("/api/"))
      return method === "GET" ? route.continue() : route.abort();
    const respond = (data) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(data),
      });
    const pathname = url.pathname.replace("/api/v1", "");
    api.push(`${method} ${pathname}`);
    if (pathname === "/session" && method === "GET")
      return respond({
        user,
        session: {
          id: id(91),
          issued_at: at,
          absolute_expires_at: "2026-10-20T12:00:00.000000Z",
          idle_expires_at: "2026-10-20T12:00:00.000000Z",
        },
        csrf_token: "synthetic-author-csrf",
      });
    if (pathname === "/me" && method === "GET")
      return respond({ user, avatar: null });
    if (pathname === "/system/model-capabilities" && method === "GET")
      return respond({
        protocols: [
          {
            protocol: "openai-compatible",
            model_types: ["chat", "embedding", "reranker", "image_generation"],
            discovery: true,
            test: true,
          },
        ],
      });
    if (pathname === `/system/models/${id(11)}` && method === "PUT") {
      const item = models.find((item) => item.id === id(11));
      const body = request.postDataJSON();
      modelWrites.push(body);
      assert.equal(body.expected_version, item.version);
      item.input = body.input;
      item.version = String(Number(item.version) + 1);
      return respond({
        kind: "model.update",
        resource_id: item.id,
        version: item.version,
        affected_references: "0",
      });
    }
    if (pathname === "/system/model-selection" && method === "GET")
      return respond(selection);
    if (pathname === "/system/model-selection" && method === "PUT") {
      const body = request.postDataJSON();
      writes.push(body);
      assert.equal(body.expected_version, selection.version);
      selection.version = String(Number(selection.version) + 1);
      selection.configured = {
        embedding: body.embedding,
        summary: body.summary,
        reranker: body.reranker,
        image: body.image,
      };
      return respond({
        kind: "model.selection.update",
        resource_id: selection.id,
        version: selection.version,
        affected_references: "0",
      });
    }
    if (pathname === "/system/models" && method === "GET") {
      const q = (url.searchParams.get("q") || "").toLowerCase();
      const items = models
        .filter(
          (item) =>
            !url.searchParams.get("provider_id") ||
            item.provider_id === url.searchParams.get("provider_id"),
        )
        .map((item) => {
          const p = providers.find((p) => p.id === item.provider_id);
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
        })
        .filter((item) =>
          `${item.provider.name}/${item.input.provider_model_id}`
            .toLowerCase()
            .includes(q),
        );
      return respond({ items, next_cursor: null });
    }
    if (method === "GET") {
      const found = pathname.startsWith("/system/models/")
        ? models.find((m) => pathname.endsWith(m.id))
        : providers.find((p) => pathname === `/system/model-providers/${p.id}`);
      if (found) return respond(found);
    }
    faults.push(`${method} ${pathname}`);
    return route.abort();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.on("pageerror", (e) => faults.push(e.message));
  try {
    if (!process.argv.includes("--only-editor")) {
      stage = "selection-load";
      await page.goto(origin + "/system/model-selection", {
        waitUntil: "domcontentloaded",
        timeout: 30000,
      });
      const trigger = page.getByRole("button", { name: /^选择总结模型/ });
      await expect(trigger).toContainText("合成 OpenAI 服务/sample-chat", {
        timeout: 30000,
      });
      await expect(
        page.getByRole("heading", { name: /^图像生成模型/ }),
      ).toBeVisible();
      await page.screenshot({
        path: path.join(output, `page-${width}-${theme}.png`),
      });
      await trigger.click();
      const panel = page.getByRole("dialog", {
        name: "选择总结模型（Chat）",
        exact: true,
      });
      await expect(panel).toBeVisible();
      await expect(
        panel
          .getByRole("row")
          .filter({ hasText: "sample-embedding" })
          .getByRole("gridcell")
          .first()
          .getByRole("button"),
      ).toHaveAttribute("aria-disabled", "true");
      const chat = panel
        .getByRole("row")
        .filter({ hasText: "合成 OpenAI 服务/sample-chat" });
      await expect(chat.locator(".ui-search-select-tag")).toHaveText([
        "对话（Chat）",
        "128K",
        "图片",
        "文件",
      ]);
      await expect(
        panel
          .getByRole("row")
          .filter({ hasText: "sample-embedding" })
          .locator(".ui-search-select-tag"),
      ).toHaveText(["嵌入（Embedding）", "1536 维", "8.1K"]);
      await expect(
        panel
          .getByRole("row")
          .filter({ hasText: "sample-rerank" })
          .locator(".ui-search-select-tag"),
      ).toHaveText(["重排（ReRank）"]);
      await expect(
        panel
          .getByRole("row")
          .filter({ hasText: "sample-image" })
          .locator(".ui-search-select-tag"),
      ).toHaveText(["图像生成（Image Generation）", "文生图"]);
      const geometry = await panel.boundingBox();
      assert(geometry.x >= 7 && geometry.x + geometry.width <= width - 7);
      await page.screenshot({
        path: path.join(output, `picker-${width}-${theme}.png`),
      });
      await page.keyboard.press("Escape");
      await expect(trigger).toBeFocused();
      await trigger.click();
      await panel
        .getByRole("combobox", { name: "搜索供应商或模型" })
        .fill("backup");
      await expect(panel.getByRole("row")).toHaveCount(1);
      await panel
        .getByRole("row")
        .filter({ hasText: "backup-summary" })
        .getByRole("gridcell")
        .first()
        .getByRole("button")
        .click();
      await expect(trigger).toContainText("备用模型服务/backup-summary");
      await expect(trigger).toBeFocused();
      assert.equal(writes.length, 0);
      await page.getByRole("button", { name: "保存", exact: true }).click();
      await expect(page.getByText("已保存。", { exact: true })).toBeVisible();
      assert.equal(writes.length, 1);
      assert.deepEqual(writes[0].summary, choice(12));
      const overflow = await page.evaluate(() => ({
        width: innerWidth,
        root: document.documentElement.scrollWidth,
      }));
      assert(overflow.root <= overflow.width);
    }
    stage = "provider-load";
    await page.goto(origin + `/system/models/${id(1)}`, {
      waitUntil: "domcontentloaded",
    });
    const row = page.getByRole("row").filter({ hasText: "sample-embedding" });
    await expect(row).toBeVisible();
    stage = "open-editor";
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    const editor = page.getByRole("dialog", { name: "编辑模型", exact: true });
    await expect(editor.getByLabel("向量维度", { exact: true })).toHaveValue(
      "1536",
    );
    await expect(
      editor.getByRole("switch", { name: "请求指定维度" }),
    ).toHaveAttribute("aria-checked", "false");
    await editor.getByLabel("向量维度", { exact: true }).fill("1024");
    await editor.getByRole("switch", { name: "请求指定维度" }).focus();
    await page.keyboard.press("Space");
    await expect(
      editor.getByRole("switch", { name: "请求指定维度" }),
    ).toHaveAttribute("aria-checked", "true");
    stage = "embedding-screenshot";
    await page.screenshot({
      path: path.join(output, `embedding-${width}-${theme}.png`),
    });
    stage = "embedding-save";
    await editor.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("模型已保存。", { exact: true })).toBeVisible();
    assert.equal(modelWrites.length, 1);
    assert.deepEqual(modelWrites[0].input.capabilities.embedding, {
      dimensions: "1024",
      dimensions_mode: "request",
    });
    await page.getByRole("button", { name: "完成", exact: true }).click();
    await row.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(editor.getByLabel("向量维度", { exact: true })).toHaveValue(
      "1024",
    );
    await expect(
      editor.getByRole("switch", { name: "请求指定维度" }),
    ).toHaveAttribute("aria-checked", "true");
    await editor.getByRole("button", { name: "取消", exact: true }).click();
    await expect(editor).not.toBeVisible();
    assert.equal(modelWrites.length, 1);
    assert.deepEqual(faults, []);
    return {
      width,
      theme,
      selectionWrites: writes.length,
      modelWrites: modelWrites.length,
      noExternalRequests: true,
    };
  } catch (error) {
    const state = await page.evaluate(() => ({
      title: document.title,
      text: document.body.innerText.slice(0, 1500),
      active: document.activeElement?.tagName,
    }));
    await fs.writeFile(
      path.join(output, `failure-${width}-${theme}.json`),
      JSON.stringify(
        { stage, api, faults, state, error: error.message },
        null,
        2,
      ),
    );
    try {
      await page.screenshot({
        path: path.join(output, `failure-${width}-${theme}.png`),
      });
    } catch (captureError) {
      await fs.writeFile(
        path.join(output, `failure-capture-${width}-${theme}.txt`),
        String(captureError.message),
      );
    }
    throw error;
  } finally {
    await context.close();
  }
}
(async () => {
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({
    executablePath: "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  const results = [];
  try {
    results.push(await run(browser, 1440, "dark"));
    results.push(await run(browser, 390, "light"));
    console.log(JSON.stringify({ passed: true, results }));
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
