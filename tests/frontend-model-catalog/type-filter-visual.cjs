// Independent real-browser check of Vue with controlled API responses.
// The separate Go HTTP/PG test proves SQL filtering; this is not a full App E2E.
// node type-filter-visual.cjs --owned type-filter-visual --input <revision>
//   --output /tmp/agenteam-type-filter-review.<unique> [--case desktop|narrow] [--flow delete]
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createRequire } = require("node:module");
const locked = createRequire(
  process.env.PICKER_PLAYWRIGHT_PACKAGE ||
    "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
if (process.argv.includes("--list")) {
  console.log(
    "type-filter: four purposes, same-type deletion replacement, wrong-type defense, search/pagination, empty state, notes/thinking and focus; desktop dark + narrow light reduced motion; synthetic APIs",
  );
  process.exit(0);
}
const arg = (name) => process.argv[process.argv.indexOf(name) + 1];
assert.equal(arg("--owned"), "type-filter-visual");
assert(/^[a-f0-9]{7,40}$/.test(arg("--input")));
const out = path.resolve(arg("--output"));
assert(/^\/tmp\/agenteam-type-filter-review\.[\w-]+$/.test(out));
const only = process.argv.includes("--case") ? arg("--case") : null;
assert(!only || ["desktop", "narrow"].includes(only));
const flow = process.argv.includes("--flow") ? arg("--flow") : "all";
assert(["all", "delete"].includes(flow));
const origin = "http://127.0.0.1:5281";
const kinds = ["chat", "embedding", "reranker", "image_generation"];
const purposes = {
  chat: "总结模型（Chat）",
  embedding: "嵌入模型（Embedding）",
  reranker: "重排模型（ReRank）",
  image_generation: "图像生成模型（Image Generation）",
};
const id = (n) => `019a8888-8888-7888-8888-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
let stage = "prepare";
const results = [],
  lifecycle = [];
function fixture() {
  const providers = [1, 2].map((n) => ({
    id: id(n),
    version: "1",
    created_at: at,
    updated_at: at,
    input: {
      name: n === 1 ? "合成北方服务" : "合成南方服务",
      protocol: "openai-compatible",
      base_url: "https://synthetic.example.invalid",
      enabled: true,
      credential_ref: null,
      options: {},
    },
  }));
  const models = [];
  for (const [index, type] of kinds.entries()) {
    for (let n = 0; n < (type === "chat" ? 13 : 2); n++) {
      const chat = type === "chat",
        embedding = type === "embedding";
      models.push({
        id: id(100 + index * 100 + n),
        provider_id: providers[n % 2].id,
        version: "1",
        created_at: at,
        updated_at: at,
        input: {
          notes: "合成备注：过滤后仍可查看说明与思考设置。",
          provider_model_id: `remote-${type}-${n}`,
          type,
          enabled: !(chat && n === 2),
          parameters: {},
          request_overwrite: {},
          header_overwrite: {},
          capabilities: {
            tool_calls: false,
            parallel_tool_calls: false,
            streaming: false,
            reasoning: chat && n === 0,
            reasoning_toggle_mode: null,
            reasoning_efforts:
              chat && n === 0 ? [{ name: "深入思考", value: "high" }] : [],
            input_modalities: ["text"],
            output_modalities: chat
              ? ["text"]
              : embedding
                ? ["vector"]
                : type === "image_generation"
                  ? ["image"]
                  : [],
            structured_output_modes: chat ? ["text", "json_schema"] : [],
            context_length: null,
            max_output: null,
            embedding: embedding
              ? { dimensions: "1536", dimensions_mode: "fixed" }
              : null,
          },
        },
      });
    }
  }
  return {
    providers,
    models,
    api: [],
    faults: [],
    writes: [],
    selection: {
      id: id(50),
      version: "1",
      configured: {
        embedding: { model_id: id(200), reasoning: { mode: "default" } },
        summary: { model_id: id(100), reasoning: { mode: "default" } },
        reranker: null,
        image: null,
      },
    },
  };
}
const label = (state, model) =>
  `${state.providers.find((p) => p.id === model.provider_id).input.name}/${model.input.provider_model_id}`;
const trigger = (page, kind) =>
  page.getByRole("button", {
    name: new RegExp("^选择" + purposes[kind] + "(?:，|$)"),
  });
const panel = (page, kind) =>
  page.getByRole("dialog", { name: "选择" + purposes[kind], exact: true });
const button = (scope, name) =>
  scope.getByRole("button", { name, exact: true });
const rows = (scope) => scope.getByRole("row");
const search = (scope) =>
  scope.getByRole("combobox", { name: "搜索供应商或模型", exact: true });
const row = (scope, state, model) =>
  rows(scope).filter({
    has: scope.page().getByText(label(state, model), { exact: true }),
  });
async function intercept(context, state) {
  await context.routeWebSocket("**/*", (socket) => socket.close());
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      method = request.method();
    if (url.origin !== origin) {
      state.faults.push("outside-owned-origin");
      return route.abort("blockedbyclient");
    }
    if (!url.pathname.startsWith("/api/")) {
      if (
        method === "GET" &&
        !["xhr", "fetch"].includes(request.resourceType())
      )
        return route.continue();
      state.faults.push("unexpected-nonstatic");
      return route.abort("blockedbyclient");
    }
    const ep = url.pathname.replace(/^\/api\/v1/, "");
    state.api.push(`${method} ${ep}${url.search}`);
    const reply = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    const user = {
      id: id(90),
      email: "synthetic@example.invalid",
      username: "synthetic-review",
      display_name: "合成独立审查",
      role: "admin",
      theme: "system",
      version: "1",
      initial_password_suggestion: false,
    };
    if (method === "GET" && ep === "/session")
      return reply({
        user,
        csrf_token: "synthetic-only",
        session: {
          id: id(91),
          issued_at: at,
          absolute_expires_at: "2026-10-20T12:00:00.000000Z",
          idle_expires_at: "2026-10-20T12:00:00.000000Z",
        },
      });
    if (method === "GET" && ep === "/me") return reply({ user, avatar: null });
    if (method === "GET" && ep === "/system/model-capabilities")
      return reply({
        protocols: [
          {
            protocol: "openai-compatible",
            model_types: kinds,
            discovery: true,
            test: true,
          },
        ],
      });
    if (method === "GET" && ep === "/system/model-selection")
      return reply(state.selection);
    if (method === "GET" && ep === "/system/model-providers")
      return reply({ items: state.providers, next_cursor: null });
    const provider = state.providers.find(
      (p) => ep === `/system/model-providers/${p.id}`,
    );
    if (method === "GET" && provider) return reply(provider);
    if (method === "GET" && ep === "/system/models") {
      const kind = url.searchParams.get("type"),
        providerID = url.searchParams.get("provider_id"),
        q = (url.searchParams.get("q") || "").toLowerCase(),
        cursor = url.searchParams.get("cursor"),
        limit = Number(url.searchParams.get("limit") || 10);
      if (!providerID)
        assert(
          kinds.includes(kind),
          "every picker request must carry its required type",
        );
      if (cursor)
        assert.equal(
          cursor,
          `next:${kind}:${q}`,
          "pagination must preserve its type and query",
        );
      const matching = state.models.filter(
        (m) =>
          (!kind || m.input.type === kind) &&
          (!providerID || m.provider_id === providerID) &&
          `${label(state, m)} ${m.input.notes}`.toLowerCase().includes(q),
      );
      const start = cursor ? limit : 0,
        items = matching.slice(start, start + limit);
      // A malformed mixed response must not make the wrong type visible or selectable.
      if (kind && items.length < limit)
        items.push(state.models.find((m) => m.input.type !== kind));
      const projected = items.map((m) => {
        const p = state.providers.find((p) => p.id === m.provider_id);
        return {
          ...m,
          provider: {
            id: p.id,
            name: p.input.name,
            protocol: p.input.protocol,
            enabled: p.input.enabled,
            version: p.version,
          },
        };
      });
      return reply({
        items: projected,
        next_cursor:
          matching.length > start + limit ? `next:${kind}:${q}` : null,
      });
    }
    const model = state.models.find((m) => ep === `/system/models/${m.id}`);
    if (method === "GET" && model) return reply(model);
    const impacted = state.models.find(
      (m) => ep === `/system/models/${m.id}/deletion-impact`,
    );
    if (method === "GET" && impacted) {
      const role = {
        chat: "summary",
        embedding: "embedding",
        reranker: "reranker",
        image_generation: "image",
      }[impacted.input.type];
      return reply({
        model_id: impacted.id,
        version: "1",
        reference_count: "1",
        reference_groups: [
          { owner_kind: "platform_selector", role, count: "1" },
        ],
        replacement_requirement: ["chat", "embedding"].includes(
          impacted.input.type,
        )
          ? "required"
          : "optional",
        delete_blocker: null,
      });
    }
    if (method !== "GET") state.writes.push(`${method} ${ep}`);
    state.faults.push(`unexpected-api:${method} ${ep}`);
    return route.abort("blockedbyclient");
  });
}
async function within(locator, page) {
  await locator.evaluate(async (e) => {
    await Promise.all(e.getAnimations().map((a) => a.finished.catch(() => {})));
  });
  const box = await locator.boundingBox(),
    viewport = await page.evaluate(() => ({
      width: innerWidth,
      height: innerHeight,
      scroll: document.documentElement.scrollWidth,
    }));
  assert(
    box &&
      box.x >= -1 &&
      box.x + box.width <= viewport.width + 1 &&
      box.y >= -1 &&
      box.y + box.height <= viewport.height + 1,
    "popup within viewport",
  );
  assert(viewport.scroll <= viewport.width + 1, "no page horizontal overflow");
}
async function shot(page, name) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    );
  });
  await page.screenshot({ path: path.join(out, name + ".png") });
}
async function open(page, kind, expected) {
  await trigger(page, kind).click();
  const popup = panel(page, kind);
  await expect(popup).toBeVisible();
  await expect(search(popup)).toBeFocused();
  await expect(rows(popup)).toHaveCount(expected);
  return popup;
}
async function close(page, kind) {
  await page.keyboard.press("Escape");
  await expect(panel(page, kind)).toHaveCount(0);
  await expect(trigger(page, kind)).toBeFocused();
}
async function purposeFlow(page, state, mode) {
  for (const kind of kinds) {
    stage = `${mode}:purpose:${kind}`;
    const popup = await open(page, kind, kind === "chat" ? 10 : 2);
    const visible = await rows(popup).allTextContents();
    for (const text of visible)
      assert(
        text.includes(`remote-${kind}-`),
        "other types must be absent from matching rows",
      );
    await within(popup, page);
    if (kind === "chat" || kind === "embedding")
      await shot(page, `${mode}-${kind}`);
    await close(page, kind);
  }
  stage = `${mode}:same-type-disabled`;
  let popup = await open(page, "chat", 10);
  const disabled = state.models.find((m) => m.id === id(102));
  await expect(
    row(popup, state, disabled).getByRole("button").first(),
  ).toHaveAttribute("aria-disabled", "true");
  await expect(row(popup, state, disabled)).toContainText("停用");
  stage = `${mode}:pagination`;
  await button(popup, "下一页").click();
  await expect(rows(popup)).toHaveCount(3);
  await expect(popup.getByText("第 2 页", { exact: true })).toBeVisible();
  await expect(search(popup)).toBeFocused();
  await button(popup, "上一页").click();
  await expect(rows(popup)).toHaveCount(10);
  await expect(search(popup)).toBeFocused();
  stage = `${mode}:search-and-empty`;
  await search(popup).fill("remote-chat-12");
  await expect(rows(popup)).toHaveCount(1);
  await expect(rows(popup)).toContainText("remote-chat-12");
  await search(popup).fill("remote-embedding");
  await expect(rows(popup)).toHaveCount(0);
  await expect(
    popup.getByText("没有匹配的对话（Chat）模型。", { exact: true }),
  ).toBeVisible();
  await shot(page, `${mode}-empty`);
  await search(popup).fill("");
  await expect(rows(popup)).toHaveCount(10);
  stage = `${mode}:notes-and-thinking`;
  const chosen = state.models[0],
    more = button(popup, label(state, chosen) + "，更多");
  await more.click();
  const detail = page.getByRole("dialog", {
    name: label(state, chosen) + "，更多",
    exact: true,
  });
  await expect(detail.locator(".model-picker-notes")).toHaveText(
    chosen.input.notes,
  );
  await within(detail, page);
  const original = detail.getByRole("radio", {
    name: "供应商默认",
    exact: true,
  });
  await original.focus();
  await page.keyboard.press("ArrowDown");
  await expect(
    detail.getByRole("radio", { name: "深入思考", exact: true }),
  ).toBeChecked();
  await shot(page, `${mode}-thinking`);
  await page.keyboard.press("ArrowLeft");
  await expect(more).toBeFocused();
  await close(page, "chat");
  // Local drafts only. Restore the selection before changing routes.
  popup = await open(page, "chat", 10);
  await button(popup, label(state, chosen) + "，更多").click();
  await detail.getByRole("radio", { name: "供应商默认", exact: true }).check();
  await page.keyboard.press("Escape");
  await close(page, "chat");
  return {
    fourPurposes: true,
    wrongTypesHidden: true,
    sameTypeDisabled: true,
    searchPaginationEmpty: true,
    notesThinkingFocus: true,
  };
}
async function deleteFlow(page, state, mode) {
  stage = `${mode}:provider-navigation-preparation`;
  await page.goto(`${origin}/system/models/${id(1)}`, {
    waitUntil: "domcontentloaded",
  });
  await expect(
    page.getByRole("heading", {
      name: state.providers[0].input.name,
      exact: true,
    }),
  ).toBeVisible({ timeout: 30000 });
  for (const kind of mode === "desktop" ? kinds : ["embedding"]) {
    stage = `${mode}:delete:${kind}`;
    const model = state.models.find((m) => m.input.type === kind),
      name = label(state, model);
    const catalogRow = page
      .getByRole("row")
      .filter({
        has: page.getByText(model.input.provider_model_id, { exact: true }),
      });
    const action = button(catalogRow, "编辑");
    await action.click();
    const editor = page.getByRole("dialog", { name: "编辑模型", exact: true });
    const deleteButton = button(editor, "删除模型");
    await deleteButton.click();
    const parent = page.getByRole("dialog", { name: "删除模型", exact: true });
    const replacement = parent.getByRole("button", {
      name: /^选择替代模型(?:，|$)/,
    });
    await expect(replacement).toBeEnabled();
    await replacement.click();
    const popup = page.getByRole("dialog", {
      name: "选择替代模型",
      exact: true,
    });
    await expect(rows(popup)).toHaveCount(kind === "chat" ? 10 : 2);
    for (const text of await rows(popup).allTextContents())
      assert(text.includes(`remote-${kind}-`));
    await expect(
      row(popup, state, model).getByRole("button").first(),
    ).toHaveAttribute("aria-disabled", "true");
    await within(popup, page);
    if (kind === "embedding") await shot(page, `${mode}-delete-embedding`);
    const target = state.models.find(
      (m) => m.input.type === kind && m.id !== model.id && m.input.enabled,
    );
    await row(popup, state, target).getByRole("button").first().click();
    await expect(popup).toHaveCount(0);
    await expect(replacement).toBeFocused();
    await expect(button(parent, "确认删除")).toBeEnabled();
    await button(parent, "取消").click();
    await expect(deleteButton).toBeFocused();
    await button(editor, "取消").click();
    await expect(action).toBeFocused();
  }
  return {
    sameTypeReplacement: true,
    currentExcluded: true,
    selectedAndCancelledWithFocus: true,
  };
}
async function run(mode, width, colorScheme) {
  const profile = path.join(out, `profile-${mode}`),
    state = fixture();
  const record = { case: mode, browserClosed: false, profileRemoved: false };
  lifecycle.push(record);
  let context, page;
  try {
    context = await chromium.launchPersistentContext(profile, {
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
      viewport: { width, height: 1000 },
      colorScheme,
      reducedMotion: mode === "narrow" ? "reduce" : "no-preference",
      serviceWorkers: "block",
      env: { ...process.env, TMPDIR: path.join(out, "cache") },
    });
    await intercept(context, state);
    page = context.pages()[0] || (await context.newPage());
    page.setDefaultTimeout(12000);
    page.on("pageerror", () => state.faults.push("pageerror"));
    let purposes;
    if (flow !== "delete") {
      stage = `${mode}:navigation-preparation`;
      await page.goto(origin + "/system/model-selection", {
        waitUntil: "domcontentloaded",
      });
      await expect(trigger(page, "chat")).toContainText("remote-chat-0", {
        timeout: 30000,
      });
      purposes = await purposeFlow(page, state, mode);
    }
    const deletion = await deleteFlow(page, state, mode);
    assert.deepEqual(state.faults, []);
    assert.deepEqual(
      state.writes,
      [],
      "selection and cancellation must remain drafts",
    );
    results.push({
      case: mode,
      flow,
      colorScheme,
      width,
      passed: true,
      purposes,
      deletion,
      api: state.api,
    });
  } catch (error) {
    results.push({
      case: mode,
      passed: false,
      stage,
      error: String(error.message),
      faults: state.faults,
      api: state.api,
    });
    if (page && !page.isClosed())
      await shot(page, `${mode}-failure`).catch(() => {});
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
  } finally {
    await fs.writeFile(
      path.join(out, "report.json"),
      JSON.stringify(
        {
          input: arg("--input"),
          boundary:
            "Real Chromium + Vue; all API responses synthetic; separate real HTTP/PG test",
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
      passed: results.every((r) => r.passed),
      output: out,
      cases: results.map((r) => r.case),
    }),
  );
})().catch((error) => {
  console.error(`${stage}: ${String(error.message)}`);
  process.exitCode = 1;
});
