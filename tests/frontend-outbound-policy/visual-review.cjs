// Independent review of the real Vue page using finite synthetic API responses.
// No backend, runtime, credentials, mail, or proxy connections are permitted.
const { chromium, expect } = require("@playwright/test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");

const origin = "http://127.0.0.1:5246";
const run = process.env.OUTBOUND_VISUAL_RUN || "candidate";
assert(/^[a-z0-9-]+$/.test(run));
const output = path.resolve(`output/ai/frontend-outbound-visual/${run}`);
const only = process.env.OUTBOUND_VISUAL_CASE;
const mode = process.argv[2] || "views";
const modes = ["rules", "views", "interactions", "errors", "zoom"];
const instant = "2026-10-16T07:00:00.000000Z";
const uuid = (n) => `019a2222-2222-7222-8222-${String(n).padStart(12, "0")}`;
const user = {
  id: uuid(1),
  email: "visual@example.invalid",
  username: "visual-review",
  display_name: "合成审查账号",
  role: "admin",
  theme: "system",
  version: "1",
  initial_password_suggestion: false,
};
const session = {
  user,
  session: {
    id: uuid(2),
    issued_at: instant,
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-17T08:00:00.000000Z",
  },
  csrf_token: "synthetic-visual-context",
};
const rule = (n) => ({
  target: `10.${n}.0.0/16`,
  ports: [443, 587],
});
const emptyDeny = "未添加规则，允许访问所有目标。";
const emptyAllow = "未添加规则，所有业务请求将被阻止。";
const btn = (page, name) => page.getByRole("button", { name, exact: true });
const tab = (page, name) => page.getByRole("tab", { name, exact: true });
const field = (page, name) => page.getByLabel(name, { exact: true });
const dialog = (page, name) => page.getByRole("dialog", { name, exact: true });
const results = [];

async function settle(page) {
  await page.evaluate(async () => {
    await document.fonts.ready;
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    );
  });
}
async function shot(page, name) {
  // This fixture never enters a password; require all password inputs empty.
  assert(
    await page
      .locator('input[type="password"]')
      .evaluateAll((nodes) => nodes.every((n) => !n.value)),
  );
  await settle(page);
  await page.screenshot({
    path: path.join(output, `${name}.png`),
    fullPage: true,
  });
}
async function overflow(page) {
  const size = await page.evaluate(() => ({
    inner: innerWidth,
    root: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  assert(
    size.root <= size.inner + 1 && size.body <= size.inner + 1,
    "document overflow",
  );
  return size;
}
async function select(page, name, option) {
  await page.getByRole("combobox", { name, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}
async function focusedControl(page) {
  await expect
    .poll(() =>
      page.evaluate(() => {
        const active = document.activeElement;
        return (
          active?.matches(
            'button:not(:disabled), input:not(:disabled), [role="tab"]',
          ) && !active.closest("dialog")
        );
      }),
    )
    .toBe(true);
}
async function fixture(context, options = {}) {
  const state = {
    rules: {
      version: "1",
      rule_mode: options.ruleMode || "denylist",
      rules: Array.from({ length: options.count ?? 2 }, (_, n) => rule(n + 1)),
    },
    proxy: {
      version: "1",
      mode: "proxy",
      gateway: {
        protocol: "https",
        host: "gateway.example.invalid",
        port: 8443,
        username: "",
        credential_present: false,
      },
    },
    writes: [],
    unexpected: [],
    errors: [],
    api: [],
    failRead: false,
    conflict: false,
    unknown: false,
    committed: false,
    failAfterWrite: false,
    pendingRead: undefined,
  };
  context.reviewState = state;
  if (options.credential) {
    state.proxy.gateway.username = "synthetic-user";
    state.proxy.gateway.credential_present = true;
  }
  if (options.direct)
    state.proxy = { version: "1", mode: "direct", gateway: null };
  await context.routeWebSocket("**/*", (socket) => {
    // Vite HMR is unnecessary for a frozen review; never establish a socket.
    socket.close();
  });
  await context.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    const method = request.method(),
      pathname = url.pathname;
    if (url.origin !== origin) {
      state.unexpected.push("outside-origin");
      return route.abort("blockedbyclient");
    }
    const respond = (value, status = 200) =>
      route.fulfill({
        status,
        contentType:
          status >= 400 ? "application/problem+json" : "application/json",
        body: JSON.stringify(value),
      });
    if (pathname.startsWith("/api/")) {
      state.api.push(`${method} ${pathname}`);
      if (url.search) {
        state.unexpected.push("api-query");
        return route.abort("blockedbyclient");
      }
      if (method === "GET" && pathname === "/api/v1/session")
        return respond(session);
      if (method === "GET" && pathname === "/api/v1/me")
        return respond({ user, avatar: null });
      const resource =
        pathname === "/api/v1/system/outbound-policy"
          ? "rules"
          : pathname === "/api/v1/system/outbound-policy/proxy"
            ? "proxy"
            : undefined;
      if (resource && method === "GET") {
        if (state.pendingRead) await state.pendingRead;
        if (state.failRead) {
          state.failRead = false;
          return route.abort("connectionfailed");
        }
        return respond(state[resource]);
      }
      if (resource && method === "PUT") {
        const body = request.postData();
        const intent = JSON.parse(body);
        // Keep comparisons private to this process; never serialize command bodies/keys.
        state.writes.push({
          resource,
          body,
          key: request.headers()["idempotency-key"],
        });
        if (state.conflict) {
          state.conflict = false;
          state[resource].version = "2";
          if (resource === "rules") state.rules.rules = [rule(12)];
          return respond(
            { code: "VERSION_CONFLICT", commit_state: "not_committed" },
            409,
          );
        }
        if (state.unknown) {
          state.unknown = false;
          if (state.committed)
            return respond(
              { code: "INTERNAL", commit_state: "committed" },
              500,
            );
          return route.abort("connectionfailed");
        }
        assert.equal(
          intent.expected_version,
          state[resource].version,
          "synthetic expected version",
        );
        const version = String(BigInt(intent.expected_version) + 1n);
        if (resource === "rules") {
          assert.deepEqual(Object.keys(intent).sort(), [
            "expected_version",
            "rule_mode",
            "rules",
          ]);
          assert(["allowlist", "denylist"].includes(intent.rule_mode));
          for (const item of intent.rules)
            assert.deepEqual(Object.keys(item).sort(), ["ports", "target"]);
          state.rules = {
            version,
            rule_mode: intent.rule_mode,
            rules: intent.rules,
          };
        } else
          state.proxy = {
            version,
            mode: intent.mode,
            gateway: intent.gateway && {
              ...intent.gateway,
              credential_present:
                (intent.credential_action === "keep" &&
                  state.proxy.gateway?.credential_present) ||
                false,
            },
          };
        if (state.failAfterWrite) {
          state.failAfterWrite = false;
          state.failRead = true;
        }
        return respond({
          version,
          ...(resource === "rules"
            ? { rule_count: String(intent.rules.length) }
            : {}),
          audit_id: uuid(8),
          created_at: instant,
        });
      }
      state.unexpected.push(`${method} ${pathname}`);
      return route.abort("blockedbyclient");
    }
    // Only frontend document/module/style/image requests can reach Vite.
    if (
      ["GET", "HEAD"].includes(method) &&
      !["fetch", "xhr"].includes(request.resourceType())
    )
      return route.continue();
    state.unexpected.push("non-static-request");
    return route.abort("blockedbyclient");
  });
  const page = context.pages()[0] || (await context.newPage());
  page.setDefaultTimeout(7000);
  page.on("pageerror", () => state.errors.push("pageerror"));
  await page.goto(`${origin}/system/outbound-policy`, {
    waitUntil: "domcontentloaded",
    timeout: 30000,
  });
  await expect(tab(page, "访问规则")).toBeVisible();
  await expect(btn(page, "添加规则")).toBeVisible();
  await settle(page);
  return { page, state };
}
async function checkCase(name, callback, browser, options = {}) {
  if (only && !only.split(",").includes(name)) return;
  const context = await browser.newContext({
    viewport: {
      width: options.width || 390,
      height: options.width === 1440 ? 960 : 844,
    },
    colorScheme: options.theme || "dark",
    reducedMotion: options.motion || "no-preference",
    serviceWorkers: "block",
  });
  let state;
  try {
    const current = await fixture(context, options);
    state = current.state;
    const detail = await callback(current.page, state);
    assert.deepEqual(state.unexpected, []);
    assert.deepEqual(state.errors, []);
    results.push({ name, outcome: "observed", detail });
  } catch (error) {
    // No implicit screenshot/DOM/trace and no raw request/error data.
    state ||= context.reviewState;
    const page = context.pages()[0];
    const feedback =
      page &&
      (await page
        .getByRole("alert")
        .allTextContents()
        .catch(() => []));
    results.push({
      name,
      outcome: "failed",
      check: String(error.message).split("\n")[0].slice(0, 240),
      location: String(error.stack)
        .split("\n")
        .filter((line) => /visual-review\.cjs:\d+/.test(line))
        .slice(0, 2),
      unexpected: state?.unexpected,
      pageErrors: state?.errors,
      api: state?.api,
      feedback,
    });
  } finally {
    await context.close();
    console.log(
      JSON.stringify({ case: name, outcome: results.at(-1)?.outcome }),
    );
  }
}
async function rules(browser) {
  for (const [width, theme] of [
    [1440, "light"],
    [390, "dark"],
  ]) {
    await checkCase(
      `rules-${width}-${theme}`,
      async (page, state) => {
        const selector = page.getByRole("combobox", {
          name: "规则模式",
          exact: true,
        });
        const table = page.getByRole("table", {
          name: "访问规则",
          exact: true,
        });
        await expect(selector).toHaveText("黑名单");
        await expect(page.getByText(emptyDeny, { exact: true })).toBeVisible();
        await expect(
          page.getByRole("checkbox", { name: "允许 HTTP" }),
        ).toHaveCount(0);
        await expect(
          page.getByText(/系统禁止地址除外|固定危险地址/),
        ).toHaveCount(0);
        await shot(page, `rules-empty-${width}-${theme}`);
        await selector.focus();
        await page.keyboard.press("Enter");
        await page.keyboard.press("Home");
        await page.keyboard.press("Enter");
        await expect(selector).toBeFocused();
        await expect(selector).toHaveText("白名单");
        await expect(page.getByText(emptyAllow, { exact: true })).toBeVisible();
        assert.equal(state.writes.length, 0);

        const targets = [
          "127.0.0.1",
          "192.168.40.0/24",
          "API.Example.TEST.",
          "*.LunaDeer.CN",
        ];
        for (const [index, target] of targets.entries()) {
          await btn(page, "添加规则").click();
          const row = table.getByRole("row", {
            name: `规则 ${index + 1}`,
            exact: true,
          });
          await expect(field(row, "目标")).toBeFocused();
          await field(row, "目标").fill(target);
          await field(row, "端口").fill(index === 0 ? "all" : "443, 8443");
        }
        await select(page, "规则模式", "黑名单");
        await expect(field(page, "目标").first()).toHaveValue(targets[0]);
        await select(page, "规则模式", "白名单");
        await expect(field(page, "目标").last()).toHaveValue(targets[3]);
        assert.equal(state.writes.length, 0);
        await shot(page, `rules-filled-${width}-${theme}`);
        const layout = await table.evaluate((node) => {
          const bounds = node.getBoundingClientRect();
          const parent = node.parentElement.getBoundingClientRect();
          return {
            tableWidth: bounds.width,
            available: parent.width,
            left: bounds.left,
            right: bounds.right,
          };
        });
        assert(
          Math.abs(layout.tableWidth - layout.available) <= 1,
          "table does not fill its available content width",
        );
        await overflow(page);

        // A loopback target is an ordinary editable/deletable row, not a hidden policy.
        const remove = btn(page, "删除规则 1");
        await remove.focus();
        await page.keyboard.press("Enter");
        await expect(dialog(page, "删除访问规则")).toBeVisible();
        await shot(page, `rules-delete-${width}-${theme}`);
        await page.keyboard.press("Escape");
        await expect(remove).toBeFocused();
        await remove.click();
        await dialog(page, "删除访问规则")
          .getByRole("button", { name: "删除规则", exact: true })
          .click();
        await expect(field(page, "目标")).toHaveCount(3);
        await focusedControl(page);
        await btn(page, "保存规则").click();
        await expect(
          page.getByText("访问规则已保存。", { exact: true }),
        ).toBeVisible();
        assert.equal(state.writes.length, 1);
        const saved = JSON.parse(state.writes[0].body);
        assert.equal(saved.rule_mode, "allowlist");
        assert.deepEqual(
          saved.rules.map((item) => item.target),
          ["192.168.40.0/24", "api.example.test", "*.lunadeer.cn"],
        );

        await select(page, "规则模式", "黑名单");
        for (let remaining = 3; remaining > 0; remaining--) {
          await btn(page, "删除规则 1").click();
          await dialog(page, "删除访问规则")
            .getByRole("button", { name: "删除规则", exact: true })
            .click();
          await expect(field(page, "目标")).toHaveCount(remaining - 1);
        }
        await focusedControl(page);
        await expect(page.getByText(emptyDeny, { exact: true })).toBeVisible();
        await btn(page, "保存规则").click();
        await expect(
          page.getByText("访问规则已保存。", { exact: true }),
        ).toBeVisible();
        assert.equal(state.writes.length, 2);
        assert.equal(state.rules.rule_mode, "denylist");
        assert.deepEqual(state.rules.rules, []);
        await overflow(page);
        return {
          width,
          theme,
          layout,
          keyboardMode: true,
          unsavedModePreservesRows: true,
          targetKinds: 4,
          ordinaryLoopbackDelete: true,
          deleteFocus: true,
          emptyDenySaved: true,
          writes: state.writes.length,
        };
      },
      browser,
      {
        width,
        theme,
        count: 0,
        motion: width === 390 ? "reduce" : "no-preference",
      },
    );
  }
}

async function views(browser) {
  for (const width of [1440, 390])
    for (const theme of ["light", "dark"]) {
      await checkCase(
        `views-${width}-${theme}`,
        async (page) => {
          await shot(page, `rules-${width}-${theme}`);
          await overflow(page);
          if (width === 390) {
            await btn(page, "保存规则").scrollIntoViewIfNeeded();
            await shot(page, `rules-${width}-${theme}-bottom`);
          }
          await tab(page, "代理设置").click();
          await expect(field(page, "代理主机")).toHaveValue(
            "gateway.example.invalid",
          );
          await shot(page, `proxy-${width}-${theme}`);
          if (width === 390) {
            await btn(page, "保存代理设置").scrollIntoViewIfNeeded();
            await shot(page, `proxy-${width}-${theme}-bottom`);
          }
          return overflow(page);
        },
        browser,
        { width, theme, credential: true },
      );
    }
}
async function interactions(browser) {
  await checkCase(
    "direct-partial-draft-and-focus-handoff",
    async (page) => {
      await tab(page, "代理设置").click();
      await select(page, "连接方式", "使用代理");
      await field(page, "代理主机").fill("partial.example.invalid");
      await select(page, "连接方式", "直连");
      await expect(field(page, "代理主机")).toHaveValue(
        "partial.example.invalid",
      );
      await btn(page, "保存代理设置").click();
      await expect(field(page, "代理端口")).toBeFocused();
      await expect(field(page, "代理端口")).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await btn(page, "取消").click();
      await dialog(page, "放弃当前修改")
        .getByRole("button", { name: "放弃修改", exact: true })
        .click();
      await expect(dialog(page, "放弃当前修改")).toHaveCount(0);
      const focus = await page.evaluate(() => ({
        tag: document.activeElement.tagName,
        text: document.activeElement.textContent?.trim().slice(0, 60),
        disabled: !!document.activeElement.disabled,
      }));
      assert(
        focus.tag !== "BODY" && !focus.disabled,
        "confirmed cancel leaves keyboard focus on document body",
      );
      return { partialDraftVisible: true, errorFocused: true, focus };
    },
    browser,
    { direct: true },
  );
  await checkCase(
    "ordinary-dialog-motion",
    async (page) => {
      await page.evaluate(() => {
        window.__visualFrames = [];
        document.addEventListener(
          "click",
          () => {
            const start = performance.now();
            const sample = () => {
              const dialog = document.querySelector("dialog[open]");
              const css = dialog && getComputedStyle(dialog);
              window.__visualFrames.push({
                elapsed: performance.now() - start,
                opacity: css?.opacity,
                transform: css?.transform,
                durations: document
                  .getAnimations()
                  .map((a) => a.effect.getComputedTiming().duration),
              });
              if (performance.now() - start < 240)
                requestAnimationFrame(sample);
            };
            requestAnimationFrame(sample);
          },
          { once: true, capture: true },
        );
      });
      await btn(page, "删除规则 1").click();
      await page.waitForFunction(
        () => window.__visualFrames.at(-1)?.elapsed >= 240,
      );
      const frames = await page.evaluate(() => window.__visualFrames);
      await page.keyboard.press("Escape");
      await expect(btn(page, "删除规则 1")).toBeFocused();
      assert(
        frames.some((frame) =>
          frame.durations.some((duration) => duration > 1),
        ),
        "ordinary dialog has no observed transition",
      );
      return {
        sampledFrames: frames.length,
        observedDurations: [
          ...new Set(frames.flatMap((frame) => frame.durations)),
        ],
        focusRestored: true,
      };
    },
    browser,
  );
  await checkCase(
    "rules-draft-tabs-delete-cancel-save",
    async (page, state) => {
      const rules = tab(page, "访问规则"),
        proxy = tab(page, "代理设置");
      await field(page, "端口").first().fill("443, 8443");
      await rules.focus();
      await page.keyboard.press("ArrowRight");
      await expect(proxy).toBeFocused();
      await expect(proxy).toHaveAttribute("aria-selected", "true");
      await field(page, "代理主机").fill("draft.example.invalid");
      await proxy.focus();
      await page.keyboard.press("ArrowLeft");
      await expect(rules).toBeFocused();
      await expect(field(page, "端口").first()).toHaveValue("443, 8443");
      const remove = btn(page, "删除规则 1");
      await remove.focus();
      await page.keyboard.press("Enter");
      await expect(dialog(page, "删除访问规则")).toBeVisible();
      await shot(page, "delete-dialog-390-dark");
      await page.keyboard.press("Escape");
      await expect(remove).toBeFocused();
      await remove.click();
      await dialog(page, "删除访问规则")
        .getByRole("button", { name: "删除规则", exact: true })
        .click();
      await expect(btn(page, "添加规则")).toBeFocused();
      await expect(field(page, "目标")).toHaveCount(1);
      await btn(page, "取消").click();
      await dialog(page, "放弃当前修改")
        .getByRole("button", { name: "放弃修改", exact: true })
        .click();
      await expect(field(page, "目标")).toHaveCount(2);
      await btn(page, "添加规则").click();
      await expect(field(page, "目标").last()).toBeFocused();
      await btn(page, "保存规则").click();
      await expect(field(page, "目标").last()).toBeFocused();
      await expect(field(page, "目标").last()).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await field(page, "目标").last().fill("192.168.30.0/24");
      await field(page, "端口").last().fill("443");
      await btn(page, "保存规则").click();
      await expect(
        page.getByText("访问规则已保存。", { exact: true }),
      ).toBeVisible();
      await proxy.click();
      await expect(field(page, "代理主机")).toHaveValue(
        "draft.example.invalid",
      );
      assert.equal(state.writes.length, 1);
      return {
        independentDrafts: true,
        deleteEscFocus: true,
        deleteConfirmFocus: true,
        validationFocus: true,
      };
    },
    browser,
  );
  await checkCase(
    "proxy-mode-credentials-protocol-and-cancel",
    async (page, state) => {
      await tab(page, "代理设置").click();
      await expect(field(page, "密码（可选）")).toHaveValue("");
      await expect(field(page, "密码（可选）")).toHaveAttribute(
        "placeholder",
        "留空保持现有密码",
      );
      await select(page, "连接方式", "直连");
      await expect(
        page.getByText("当前使用直连，以下代理配置会保留。", { exact: true }),
      ).toBeVisible();
      await btn(page, "保存代理设置").click();
      await expect(
        page.getByText("代理设置已保存。", { exact: true }),
      ).toBeVisible();
      const saved = JSON.parse(state.writes[0].body);
      assert(
        saved.gateway &&
          saved.credential_action === "keep" &&
          !Object.hasOwn(saved, "password"),
      );
      await btn(page, "移除身份认证").click();
      const modal = dialog(page, "移除代理身份认证");
      await expect(modal).toBeVisible();
      for (let index = 0; index < 5; index++) {
        await page.keyboard.press("Tab");
        assert(
          await modal.evaluate((node) => node.contains(document.activeElement)),
          "dialog focus trap",
        );
      }
      await page.keyboard.press("Escape");
      await expect(btn(page, "移除身份认证")).toBeFocused();
      await btn(page, "移除身份认证").click();
      await modal
        .getByRole("button", { name: "移除身份认证", exact: true })
        .click();
      await expect(field(page, "用户名（可选）")).toHaveValue("");
      await focusedControl(page);
      await btn(page, "取消").click();
      await dialog(page, "放弃当前修改")
        .getByRole("button", { name: "放弃修改", exact: true })
        .click();
      await expect(field(page, "用户名（可选）")).toHaveValue("synthetic-user");
      await focusedControl(page);
      await select(page, "连接方式", "使用代理");
      const protocol = page.getByRole("combobox", {
        name: "代理服务器类型",
        exact: true,
      });
      await protocol.focus();
      await page.keyboard.press("Enter");
      await page.keyboard.press("ArrowDown");
      await page.keyboard.press("Enter");
      await expect(protocol).toBeFocused();
      await expect(
        page.getByText(
          "到 HTTP / SOCKS5 代理网关的连接不加密，代理凭据可能明文传输。",
          { exact: true },
        ),
      ).toBeVisible();
      await shot(page, "proxy-http-warning-390-dark");
      return {
        keepWithoutPassword: true,
        credentialConfirmation: true,
        protocolKeyboard: true,
      };
    },
    browser,
    { credential: true },
  );
  await checkCase(
    "long-form-scroll-and-reduced-motion",
    async (page) => {
      const region = page.locator(".settings-main");
      const before = await region.evaluate((node) => node.scrollTop);
      await page.mouse.move(280, 650);
      await page.mouse.wheel(0, 6000);
      await expect
        .poll(() => region.evaluate((node) => node.scrollTop))
        .toBeGreaterThan(before);
      await field(page, "端口").last().fill("443, 8443");
      await btn(page, "保存规则").scrollIntoViewIfNeeded();
      await expect(btn(page, "保存规则")).toBeInViewport();
      await shot(page, "long-rules-end-390-dark");
      await page
        .getByRole("button", { name: "打开系统设置目录", exact: true })
        .click();
      await expect(dialog(page, "系统设置")).toBeVisible();
      const durations = await page.evaluate(() =>
        document
          .getAnimations()
          .map((animation) => animation.effect.getComputedTiming().duration),
      );
      await page.keyboard.press("Escape");
      await expect(btn(page, "打开系统设置目录")).toBeFocused();
      await overflow(page);
      return {
        wheelScrolled: true,
        saveReachable: true,
        reducedMotion: true,
        activeAnimationDurations: durations,
      };
    },
    browser,
    { count: 14, motion: "reduce" },
  );
}
async function errors(browser) {
  await checkCase(
    "empty-loading-read-failure",
    async (page, state) => {
      state.rules.rules = [];
      let release;
      state.pendingRead = new Promise((resolve) => {
        release = resolve;
      });
      await page.reload();
      await expect(
        page.getByText("正在读取访问规则…", { exact: true }),
      ).toBeVisible();
      await expect(page.getByText(emptyDeny, { exact: true })).toHaveCount(0);
      state.failRead = true;
      release();
      state.pendingRead = undefined;
      await expect(page.getByRole("alert")).toContainText("当前配置读取失败");
      await shot(page, "read-failure-390-dark");
      await expect(btn(page, "添加规则")).toHaveCount(0);
      await btn(page, "重新读取配置").click();
      await expect(page.getByText(emptyDeny, { exact: true })).toBeVisible();
      return {
        loadingNotEmpty: true,
        failedNotEmpty: true,
        emptyAfterRetry: true,
      };
    },
    browser,
  );
  await checkCase(
    "conflict-keeps-draft-and-explicit-adoption",
    async (page, state) => {
      await field(page, "目标").first().fill("10.99.0.0/16");
      state.conflict = true;
      await btn(page, "保存规则").click();
      await expect(page.getByRole("alert")).toContainText("你的草稿已保留");
      await expect(page.getByRole("alert")).toBeFocused();
      await expect(field(page, "目标").first()).toHaveValue("10.99.0.0/16");
      await btn(page, "读取最新配置").click();
      await expect(btn(page, "采用最新配置")).toBeVisible();
      await expect(field(page, "目标").first()).toHaveValue("10.99.0.0/16");
      await btn(page, "采用最新配置").scrollIntoViewIfNeeded();
      await shot(page, "conflict-adoption-390-dark");
      await btn(page, "采用最新配置").click();
      await dialog(page, "放弃当前修改")
        .getByRole("button", { name: "放弃修改", exact: true })
        .click();
      await expect(field(page, "目标").first()).toHaveValue("10.12.0.0/16");
      await focusedControl(page);
      assert.equal(state.writes.length, 1);
      return {
        draftPreserved: true,
        alertFocused: true,
        explicitAdoption: true,
      };
    },
    browser,
  );
  for (const committed of [false, true])
    await checkCase(
      `proxy-${committed ? "committed" : "unknown"}-recovery`,
      async (page, state) => {
        await tab(page, "代理设置").click();
        await field(page, "代理端口").fill("9443");
        state.unknown = true;
        state.committed = committed;
        await btn(page, "保存代理设置").click();
        await expect(page.getByRole("alert")).toContainText(
          committed ? "操作已提交" : "原保存结果尚未确认",
        );
        await expect(page.getByRole("alert")).toBeFocused();
        await expect(field(page, "代理端口")).toBeDisabled();
        await expect(tab(page, "访问规则")).toBeDisabled();
        await expect(btn(page, "读取最新配置")).toBeDisabled();
        await shot(
          page,
          `proxy-${committed ? "committed" : "unknown"}-390-dark`,
        );
        await btn(page, "检查当前状态").click();
        await expect(btn(page, "重试原请求")).toBeVisible();
        await btn(page, "重试原请求").click();
        await expect(
          page.getByText("代理设置已保存。", { exact: true }),
        ).toBeVisible();
        assert.equal(state.writes.length, 2);
        assert.equal(state.writes[0].body, state.writes[1].body);
        assert.equal(state.writes[0].key, state.writes[1].key);
        await expect(field(page, "代理端口")).toHaveValue("9443");
        return {
          frozen: true,
          alertFocused: true,
          samePrivateIntentReplayed: true,
        };
      },
      browser,
    );
  await checkCase(
    "confirmed-save-failed-current-read",
    async (page, state) => {
      await tab(page, "代理设置").click();
      await field(page, "代理端口").fill("9443");
      state.failAfterWrite = true;
      await btn(page, "保存代理设置").click();
      await expect(page.getByRole("alert")).toContainText(
        "保存已确认，当前配置读取失败",
      );
      await expect(
        page.getByRole("combobox", { name: "连接方式", exact: true }),
      ).toHaveCount(0);
      await expect(btn(page, "保存代理设置")).toHaveCount(0);
      await shot(page, "confirmed-read-failure-390-dark");
      await btn(page, "重新读取配置").click();
      await expect(field(page, "代理端口")).toHaveValue("9443");
      assert.equal(state.writes.length, 1);
      return { confirmedVisible: true, onlyReadRetried: true };
    },
    browser,
  );
  await checkCase(
    "unknown-abandon-focus",
    async (page, state) => {
      await tab(page, "代理设置").click();
      await field(page, "代理端口").fill("9443");
      state.unknown = true;
      await btn(page, "保存代理设置").click();
      await expect(page.getByRole("alert")).toContainText("原保存结果尚未确认");
      await btn(page, "放弃本次操作").click();
      const modal = dialog(page, "放弃追踪本次操作");
      await expect(modal).toContainText("不会撤销服务端操作");
      await modal
        .getByRole("button", { name: "放弃追踪", exact: true })
        .click();
      await expect(field(page, "代理端口")).toHaveValue("8443");
      await focusedControl(page);
      assert.equal(state.writes.length, 1);
      return { abandonDoesNotReplay: true, focusHandedOff: true };
    },
    browser,
  );
  await checkCase(
    "rules-confirmed-save-failed-current-read",
    async (page, state) => {
      await field(page, "端口").first().fill("443, 8443");
      state.failAfterWrite = true;
      await btn(page, "保存规则").click();
      await expect(page.getByRole("alert")).toContainText(
        "保存已确认，当前配置读取失败",
      );
      await expect(btn(page, "添加规则")).toHaveCount(0);
      await expect(page.getByText(emptyDeny, { exact: true })).toHaveCount(0);
      await expect(btn(page, "保存规则")).toHaveCount(0);
      await shot(page, "rules-confirmed-read-failure-390-dark");
      await btn(page, "重新读取配置").click();
      await expect(field(page, "目标")).toHaveCount(2);
      await expect(field(page, "端口").first()).toHaveValue("443, 8443");
      assert.equal(state.writes.length, 1);
      return {
        confirmedVisible: true,
        noInventedEmptyRules: true,
        onlyReadRetried: true,
      };
    },
    browser,
  );
}
async function zoom() {
  const profile = await fs.mkdtemp(
    path.join(os.tmpdir(), "outbound-visual-zoom-"),
  );
  let context;
  let phase = "launch";
  try {
    await fs.mkdir(path.join(profile, "Default"));
    await fs.writeFile(
      path.join(profile, "Default/Preferences"),
      JSON.stringify({
        partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
      }),
    );
    context = await chromium.launchPersistentContext(profile, {
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      reducedMotion: "reduce",
      colorScheme: "dark",
      serviceWorkers: "block",
    });
    phase = "fixture-ready";
    const { page, state } = await fixture(context, { count: 8 });
    phase = "measure-native-zoom";
    const measured = await page.evaluate(() => ({
      inner: innerWidth,
      outer: outerWidth,
      dpr: devicePixelRatio,
      cssZoom: getComputedStyle(document.documentElement).zoom,
    }));
    assert.deepEqual(measured, {
      inner: 720,
      outer: 1440,
      dpr: 2,
      cssZoom: "1",
    });
    phase = "scroll-and-save";
    const region = page.locator(".settings-main");
    await page.mouse.move(550, 280);
    await page.mouse.wheel(0, 5000);
    await expect
      .poll(() => region.evaluate((node) => node.scrollTop))
      .toBeGreaterThan(100);
    await btn(page, "保存规则").scrollIntoViewIfNeeded();
    await expect(btn(page, "保存规则")).toBeInViewport();
    await overflow(page);
    phase = "delete-dialog";
    const remove = btn(page, "删除规则 8");
    await remove.focus();
    await page.keyboard.press("Enter");
    await expect(dialog(page, "删除访问规则")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(remove).toBeFocused();
    await expect(remove).toBeInViewport();
    phase = "capture";
    await shot(page, "zoom-rules-full-observer-only");
    const cdp = await context.newCDPSession(page);
    const capture = await cdp.send("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
    });
    await fs.writeFile(
      path.join(output, "zoom-200-rules-viewport.png"),
      Buffer.from(capture.data, "base64"),
    );
    await cdp.detach();
    await overflow(page);
    assert.deepEqual(state.unexpected, []);
    assert.deepEqual(state.errors, []);
    results.push({
      name: "native-200-percent",
      outcome: "observed",
      detail: {
        ...measured,
        scrollAndSaveReachable: true,
        deleteEscFocusRestored: true,
        writes: state.writes.length,
      },
    });
  } catch (error) {
    const page = context?.pages()[0];
    results.push({
      name: "native-200-percent",
      outcome: "failed",
      phase,
      check: String(error.message).split("\n")[0].slice(0, 240),
      location: String(error.stack)
        .split("\n")
        .filter((line) => /visual-review\.cjs:\d+/.test(line))
        .slice(0, 2),
      api: context?.reviewState?.api,
      unexpected: context?.reviewState?.unexpected,
      pageErrors: context?.reviewState?.errors,
      ui:
        page &&
        (await page
          .evaluate(() => ({
            pathname: location.pathname,
            mounted: !!document.querySelector(".settings-main"),
            tabs: document.querySelectorAll('[role="tab"]').length,
            table: !!document.querySelector('table[aria-label="访问规则"]'),
            openDialog: !!document.querySelector("dialog[open]"),
          }))
          .catch(() => ({ unavailable: true }))),
    });
  } finally {
    if (context) await context.close();
    await fs.rm(profile, { recursive: true, force: true });
  }
}
async function main() {
  if (mode === "--list") {
    console.log(
      JSON.stringify({
        modes,
        origin,
        boundary:
          "synthetic API only; no backend; screenshots require visual inspection",
      }),
    );
    return;
  }
  assert(modes.includes(mode));
  await fs.mkdir(output, { recursive: true });
  let browser;
  try {
    if (mode === "zoom") await zoom();
    else {
      browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: ["--no-sandbox"],
      });
      if (mode === "rules") await rules(browser);
      if (mode === "views") await views(browser);
      if (mode === "interactions") await interactions(browser);
      if (mode === "errors") await errors(browser);
    }
  } finally {
    if (browser) await browser.close();
  }
  const report = {
    mode,
    input:
      process.env.OUTBOUND_VISUAL_INPUT ||
      "rule-mode candidate; set OUTBOUND_VISUAL_INPUT to the reviewed frozen input",
    results,
    browserClosed: true,
    boundary:
      "Synthetic responses prove visible UI behavior only; no real persistence or network routing.",
  };
  await fs.writeFile(
    path.join(output, `${mode}.json`),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report));
  if (results.some((item) => item.outcome === "failed")) process.exitCode = 1;
}
main().catch(() => {
  console.error(
    "Visual review failed before report; no sensitive diagnostics emitted.",
  );
  process.exitCode = 1;
});
