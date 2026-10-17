const { createRequire } = require("node:module");
const locked = createRequire(
  "/workspace/agenteam/tests/account-captcha-web/package.json",
);
const { chromium, expect } = locked("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
assert.equal(locked("@playwright/test/package.json").version, "1.56.1");
const origin = "http://127.0.0.1:5281";
const output = process.env.PROVIDER_SETTINGS_OUTPUT;
assert(
  output?.startsWith("/workspace/scratch/"),
  "Use a private task output directory",
);
const id = (n) => `019a5555-5555-7555-8555-${String(n).padStart(12, "0")}`;
const at = "2026-10-17T09:00:00.000000Z";
const makeProvider = (n, name, enabled, credential) => ({
  id: id(n),
  version: "1",
  created_at: at,
  updated_at: at,
  input: {
    name,
    protocol: "openai-compatible",
    base_url: "https://synthetic.example.invalid/v1",
    enabled,
    credential_ref: credential ? id(n + 30) : null,
    options: {},
  },
});
const capabilities = {
  tool_calls: true,
  parallel_tool_calls: false,
  streaming: true,
  reasoning: false,
  reasoning_toggle_mode: null,
  input_modalities: ["text"],
  output_modalities: ["text"],
  reasoning_efforts: [],
  structured_output_modes: ["json_schema"],
  context_length: "128000",
  max_output: "16384",
};
(async () => {
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({
    executablePath: "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  const results = [];
  try {
    for (const { width, height, theme } of [
      { width: 1440, height: 1000, theme: "dark" },
      { width: 390, height: 844, theme: "light" },
    ]) {
      let context,
        page,
        stage = "setup";
      const providers = [
        makeProvider(1, "OpenAI", true, true),
        makeProvider(2, "DeepSeek", true, true),
        makeProvider(3, "本地推理服务", false, false),
      ];
      const user = {
        id: id(90),
        email: "provider-ui@example.invalid",
        username: "provider-ui",
        display_name: "页面展示",
        role: "admin",
        theme,
        version: "1",
        initial_password_suggestion: false,
      };
      const writes = [],
        unexpected = [],
        errors = [];
      try {
        context = await browser.newContext({
          viewport: { width, height },
          colorScheme: theme,
          serviceWorkers: "block",
        });
        await context.routeWebSocket("**/*", (socket) => socket.close());
        await context.route("**/*", async (route) => {
          const request = route.request(),
            url = new URL(request.url()),
            method = request.method();
          const reply = (value) =>
            route.fulfill({
              status: 200,
              contentType: "application/json",
              body: JSON.stringify(value),
            });
          if (url.origin !== origin) {
            unexpected.push(method + " " + url.origin + url.pathname);
            return route.abort();
          }
          if (!url.pathname.startsWith("/api/")) return route.continue();
          const path = url.pathname.replace("/api/v1", "");
          if (method !== "GET")
            writes.push({ path, method, body: request.postDataJSON() });
          if (path === "/session")
            return reply({
              user,
              session: {
                id: id(91),
                issued_at: at,
                absolute_expires_at: "2026-10-20T12:00:00.000000Z",
                idle_expires_at: "2026-10-20T12:00:00.000000Z",
              },
              csrf_token: "synthetic-provider-context",
            });
          if (path === "/me") return reply({ user, avatar: null });
          if (path === "/system/model-providers" && method === "GET")
            return reply({ items: providers, next_cursor: null });
          const supplier = providers.find(
            (value) => path === `/system/model-providers/${value.id}`,
          );
          if (supplier && method === "GET") return reply(supplier);
          if (supplier && method === "PUT") {
            const body = request.postDataJSON();
            assert.equal(body.expected_version, supplier.version);
            supplier.input = body.input;
            supplier.version = String(Number(supplier.version) + 1);
            return reply({
              kind: "provider.update",
              resource_id: supplier.id,
              version: supplier.version,
              affected_references: "0",
            });
          }
          if (path === "/system/model-capabilities")
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
          if (path === "/system/models" && method === "GET") {
            const p = providers[0];
            return reply({
              items: [
                {
                  id: id(50),
                  provider_id: p.id,
                  version: "1",
                  created_at: at,
                  updated_at: at,
                  input: {
                    notes: "默认对话模型",
                    provider_model_id: "gpt-4.1",
                    type: "chat",
                    enabled: true,
                    parameters: {},
                    request_overwrite: {},
                    header_overwrite: {},
                    capabilities,
                  },
                  provider: {
                    id: p.id,
                    name: p.input.name,
                    protocol: p.input.protocol,
                    enabled: p.input.enabled,
                    version: p.version,
                  },
                },
              ],
              next_cursor: null,
            });
          }
          unexpected.push(method + " " + path);
          return route.abort();
        });
        page = await context.newPage();
        page.on("pageerror", (error) => errors.push(error.message));
        stage = "list";
        await page.goto(origin + "/system/models", {
          waitUntil: "domcontentloaded",
          timeout: 30000,
        });
        await expect(
          page.getByRole("heading", { name: "供应商列表", exact: true }),
        ).toBeVisible();
        await expect(page.getByRole("switch")).toHaveCount(3);
        await expect(
          page.getByRole("button", { name: "配置", exact: true }),
        ).toHaveCount(3);
        await page.screenshot({ path: `${output}/list-${width}-${theme}.png` });
        stage = "toggle";
        const toggle = page.getByRole("switch", {
          name: "启用OpenAI",
          exact: true,
        });
        await toggle.focus();
        await page.keyboard.press("Space");
        await expect(toggle).toHaveAttribute("aria-checked", "false");
        assert.equal(writes.length, 1);
        assert.equal(providers[0].input.enabled, false);
        stage = "config-navigation";
        await page
          .getByRole("button", { name: "配置", exact: true })
          .first()
          .click();
        await expect(
          page.getByRole("tab", { name: "配置", exact: true }),
        ).toHaveAttribute("aria-selected", "true");
        await expect(
          page.getByLabel("API 端点", { exact: true }),
        ).toBeVisible();
        assert.equal(await page.getByRole("dialog").count(), 0);
        stage = "tab-draft";
        await page
          .getByLabel("API 端点", { exact: true })
          .fill("https://updated.example.invalid/v1");
        await page.getByRole("tab", { name: "模型", exact: true }).click();
        await expect(
          page.getByRole("cell", { name: "OpenAI/gpt-4.1", exact: true }),
        ).toBeVisible();
        await page.getByRole("tab", { name: "配置", exact: true }).click();
        await expect(page.getByLabel("API 端点", { exact: true })).toHaveValue(
          "https://updated.example.invalid/v1",
        );
        stage = "save-empty-credential";
        await page.getByRole("button", { name: "保存", exact: true }).click();
        await expect(
          page.getByText("供应商已保存。", { exact: true }),
        ).toBeVisible();
        assert.equal(writes.length, 2);
        assert(
          writes.every(
            (value) =>
              value.path === `/system/model-providers/${id(1)}` &&
              value.method === "PUT",
          ),
        );
        assert.equal(providers[0].input.credential_ref, id(31));
        assert.equal(
          providers[0].input.base_url,
          "https://updated.example.invalid/v1",
        );
        await page.screenshot({
          path: `${output}/config-${width}-${theme}.png`,
        });
        stage = "models";
        await page.getByRole("tab", { name: "模型", exact: true }).click();
        await expect(
          page.getByRole("cell", { name: "OpenAI/gpt-4.1", exact: true }),
        ).toBeVisible();
        assert(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
        await page.screenshot({
          path: `${output}/models-${width}-${theme}.png`,
        });
        assert.deepEqual(unexpected, []);
        assert.deepEqual(errors, []);
        results.push({
          width,
          height,
          theme,
          synthetic: true,
          providerWrites: writes.length,
          secretWrites: 0,
          normalFlow: "passed",
        });
      } catch (error) {
        await fs.writeFile(
          `${output}/failure-${width}.json`,
          JSON.stringify(
            {
              stage,
              error: error.message,
              url: page?.url(),
              unexpected,
              errors,
              writeMethods: writes.map(
                (value) => value.method + " " + value.path,
              ),
            },
            null,
            2,
          ),
        );
        if (page)
          await page
            .screenshot({ path: `${output}/failure-${width}.png` })
            .catch(() => {});
        throw error;
      } finally {
        if (context) await context.close();
      }
    }
    await fs.writeFile(
      output + "/result.json",
      JSON.stringify(results, null, 2),
    );
    console.log(JSON.stringify(results));
  } finally {
    await browser.close();
    console.log(JSON.stringify({ browserClosed: true }));
  }
})().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
