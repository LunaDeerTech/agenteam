// Independent controlled review of ModelPicker reasoning choices.
// This preparation loads the locked runtime without opening a browser. The final
// DTO and frozen candidate must be wired before executable scenarios are added.
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

async function main() {
  // Deliberate preparation boundary: no guessed DTO or stale business scenario.
  throw new Error("FROZEN_CANDIDATE_AND_REASONING_DTO_REQUIRED");
}
if (!args.includes("--check"))
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
