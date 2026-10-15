// All business facts below are synthetic. These checks do not prove backend behavior.
const { test, expect } = require("@playwright/test");
const { mkdirSync, readFileSync } = require("node:fs");
const path = require("node:path");
const {
  installFixture,
  projectPath,
  sprintID,
  provider,
  project: baselineProject,
} = require("../../../../tests/account-captcha-web/interface-layout.fixtures.ts");
const output = path.resolve(
  process.env.F0_OUTPUT ||
    path.resolve(__dirname, "../../../../output/ai/frontend-f0"),
);
const screens = path.join(output, "screenshots");
const pages = [
  "dashboard",
  "projects",
  "project",
  "tasks",
  "providers",
  "knowledge",
  "meetings",
  "inbox",
  "mcp",
  "oauth",
  "memory",
];
const states = [
  "normal",
  "long",
  "empty",
  "loading",
  "error",
  "readonly",
  "unknown",
];
const titles = {
  settings: "基本信息",
  dashboard: "首页",
  projects: "项目",
  project: "项目概览",
  tasks: "任务",
  providers: "Provider / Model",
  knowledge: "知识库",
  meetings: "会议",
  inbox: "项目待办",
  mcp: "MCP 服务",
  oauth: "MCP 授权返回",
  memory: "Agent 记忆",
};
const layouts = [
  { width: 1440, height: 960, theme: "light" },
  { width: 1440, height: 960, theme: "dark" },
  { width: 390, height: 844, theme: "light" },
  { width: 390, height: 844, theme: "dark" },
];
async function setup(page, layout) {
  await page.setViewportSize({ width: layout.width, height: layout.height });
  await page.emulateMedia({
    colorScheme: layout.theme,
    reducedMotion: "reduce",
  });
  const faults = { errors: [], outbound: [] };
  page.on("pageerror", (error) => faults.errors.push(error.message));
  await page.route("**/*", (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.protocol === "data:" || url.protocol === "blob:")
      return route.continue();
    // Chromium's built-in PDF viewer assets are local browser resources, not outbound services.
    if (
      request.method() === "GET" &&
      ((url.protocol === "chrome-extension:" &&
        url.hostname === "mhjfbmdgcfjbbpaeojofohoefgiehjai") ||
        (url.protocol === "chrome:" && url.hostname === "resources"))
    )
      return route.continue();
    const ownedOrigins = [
      `http://127.0.0.1:${process.env.F0_PORT || 5188}`,
      `http://127.0.0.1:${process.env.F0_LEGACY_PORT || 5189}`,
    ];
    if (
      ownedOrigins.includes(url.origin) &&
      ["GET", "HEAD"].includes(request.method())
    )
      return route.continue();
    faults.outbound.push(
      `${request.method()} ${url.protocol}//${url.host}${url.pathname}`,
    );
    return route.abort("blockedbyclient");
  });
  return faults;
}
async function open(page, name, layout, scenario = "normal", extra = {}) {
  await page.goto(
    "/index.html?" +
      new URLSearchParams({
        page: name,
        theme: layout.theme,
        scenario,
        accent: "indigo",
        ...extra,
      }),
  );
  await expect(page.locator("#main")).toBeVisible();
  await expect(page).toHaveTitle(`${titles[name]} · agenteam F0`);
  await expect(page.locator("#main")).not.toContainText("正在装配");
  await page.evaluate(() => document.fonts.ready);
}

test("[interaction] project states and accepted-to-ready fixture flow", async ({
  page,
}) => {
  const layout = layouts[0];
  const faults = await setup(page, layout);
  await open(page, "projects", layout, "loading");
  await expect(page.locator('#main [aria-busy="true"]')).toBeVisible();
  await expect(page.getByTestId("project-row")).toHaveCount(0);
  await open(page, "projects", layout, "empty");
  await expect(page.getByRole("heading", { name: "还没有项目" })).toBeVisible();
  await expect(page.getByTestId("project-row")).toHaveCount(0);
  await open(page, "projects", layout, "error");
  await page.getByRole("button", { name: "重新加载", exact: true }).click();
  await expect(page.getByTestId("project-row").first()).toBeVisible();
  await open(page, "projects", layout, "readonly");
  await expect(
    page.getByRole("button", { name: "新建项目", exact: true }),
  ).toBeDisabled();
  await open(page, "projects", layout, "unknown");
  const before = await page.getByTestId("project-row").count();
  await page.getByRole("button", { name: "查询原请求", exact: true }).click();
  await expect(page.locator("#main")).toContainText("继续保留原请求");
  await expect(page.getByTestId("project-row")).toHaveCount(before);
  await open(page, "projects", layout);
  await page.getByRole("button", { name: "新建项目", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("项目名称", { exact: true }).fill("Fixture.Keyboard");
  await dialog.getByRole("button", { name: "创建项目", exact: true }).click();
  await expect(dialog).toContainText("已接受创建请求");
  await expect(
    dialog.getByRole("button", { name: "进入项目", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "创建项目", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("button", { name: "检查准备结果", exact: true })
    .click();
  await expect(dialog).toContainText("项目已就绪");
  await expect(dialog).toContainText("样板演示");
  await expect(
    dialog.getByRole("button", { name: "进入项目", exact: true }),
  ).toBeEnabled();
  expect(faults.outbound).toEqual([]);
  expect(faults.errors).toEqual([]);
});

test("[recovery] Bearer credential replacement permits reconnect", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  await open(page, "mcp", layouts[3]);
  await page.getByRole("button", { name: "删除访问凭据", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确认删除凭据", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "先完成认证", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "配置凭据", exact: true }).click();
  await page
    .getByLabel("凭据材料", { exact: true })
    .fill("f0-synthetic-material-only");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存凭据", exact: true })
    .click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByRole("button", { name: "连接服务", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "连接服务", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "刷新发现", exact: true }),
  ).toBeEnabled();
  await expect(page.locator("#main")).toContainText("连接已确认");
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[recovery] direct OAuth confirmation selects the authorized service", async ({
  page,
}) => {
  const faults = await setup(page, layouts[0]);
  await open(page, "oauth", layouts[0]);
  await page.getByRole("button", { name: "确认授权返回", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "授权结果已确认",
  );
  await page
    .getByRole("link", { name: "返回项目 MCP 服务", exact: true })
    .click();
  await expect(page.locator("#service-heading")).toHaveText("GitHub");
  await expect(
    page.getByRole("button", { name: "连接服务", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "连接服务", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "刷新发现", exact: true }),
  ).toBeEnabled();
  await expect(page.locator("#main")).toContainText("连接已确认");
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

for (const layout of [layouts[0], layouts[3]]) {
  test(`[detail] task and model keyboard return ${layout.width}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    await open(page, "tasks", layout);
    const task = page.getByTestId("task-card").first();
    await task.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toBeVisible();
    await capture(page, info, `task-detail-${layout.width}-${layout.theme}`);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(task).toBeFocused();
    await open(page, "providers", layout);
    await page.getByRole("tab", { name: "模型", exact: true }).click();
    await expect(page.locator(".model-row")).toHaveCount(2);
    await capture(
      page,
      info,
      `provider-models-${layout.width}-${layout.theme}`,
    );
    const edit = page.locator("[data-edit-model]").first();
    await edit.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog", { name: "编辑模型" })).toBeVisible();
    await expect(page.getByLabel("服务方模型标识")).toHaveValue(
      "research-chat",
    );
    await page.keyboard.press("Escape");
    await expect(edit).toBeFocused();
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });

  test(`[knowledge] four formats and versioned downloads ${layout.width}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    for (const doc of ["md", "txt", "pdf", "docx"]) {
      await open(page, "knowledge", layout, "normal", { doc });
      // A narrow root entry starts in its directory; selecting the real link opens the reader.
      const entry = page.locator(`[data-doc="${doc}"]`);
      if (await entry.isVisible()) await entry.click();
      await expect(page.locator(".knowledge-reader")).toBeVisible();
      await noOverflow(page, `knowledge/${doc}/${layout.width}`);
      if (doc === "md" || doc === "txt") {
        await expect(page.locator(".knowledge-prose")).toBeVisible();
        if (layout.width === 390 && doc === "md")
          expect(
            (await page.locator(".knowledge-prose").boundingBox()).y,
          ).toBeLessThanOrEqual(380);
      } else if (doc === "pdf") {
        await expect(
          page.locator('object[type="application/pdf"]'),
        ).toBeVisible();
        expect(
          await page
            .locator('object[type="application/pdf"]')
            .evaluate(async (object) => {
              const response = await fetch(object.data.split("#")[0]);
              return (await response.text()).startsWith("%PDF-");
            }),
        ).toBe(true);
        await expect
          .poll(
            () =>
              page
                .frames()
                .filter((frame) =>
                  frame
                    .url()
                    .startsWith(
                      "chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai/",
                    ),
                ).length,
          )
          .toBe(1);
        const pdfFrame = page
          .frames()
          .find((frame) =>
            frame
              .url()
              .startsWith(
                "chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai/",
              ),
          );
        await expect
          .poll(() =>
            pdfFrame.evaluate(() => Boolean(customElements.get("pdf-viewer"))),
          )
          .toBe(true);
        await expect
          .poll(() =>
            pdfFrame.evaluate(
              () => document.querySelector("pdf-viewer")?.loadProgress_,
            ),
          )
          .toBe(100);
      } else {
        await expect(page.locator(".knowledge-reader")).toContainText(
          "正在解析文档正文",
        );
        await page.getByText("演示解析失败", { exact: true }).click();
        await page
          .getByRole("button", { name: "切换为解析失败", exact: true })
          .click();
        await expect(page.locator(".knowledge-reader")).toContainText(
          "正文解析失败",
        );
      }
      await capture(
        page,
        info,
        `knowledge-${doc}-${layout.width}-${layout.theme}`,
      );
      await page
        .getByRole("button", { name: "打开详细信息", exact: true })
        .click();
      const pending = page.waitForEvent("download");
      await page
        .locator(".knowledge-info")
        .getByRole("button", { name: "下载原版本", exact: true })
        .click();
      const download = await pending;
      expect(await download.failure()).toBeNull();
      expect(download.suggestedFilename()).toMatch(
        new RegExp(`-v[0-9]+\\.${doc}$`),
      );
      const bytes = readFileSync(await download.path());
      expect(bytes.length).toBeGreaterThan(20);
      if (doc === "pdf") expect(bytes.subarray(0, 5).toString()).toBe("%PDF-");
      if (doc === "docx") expect(bytes.subarray(0, 2).toString()).toBe("PK");
    }
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

for (const layout of [layouts[0], layouts[3]]) {
  test(`[keyboard] project dialog trap and focus return ${layout.width}`, async ({
    page,
  }) => {
    const faults = await setup(page, layout);
    await open(page, "projects", layout);
    const trigger = page.getByRole("button", { name: "新建项目", exact: true });
    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog")).toBeVisible();
    for (let index = 0; index < 12; index++) {
      await page.keyboard.press(index % 3 === 0 ? "Shift+Tab" : "Tab");
      expect(
        await page.evaluate(() =>
          document.getElementById("dialog").contains(document.activeElement),
        ),
      ).toBe(true);
    }
    const focus = await page.evaluate(() => {
      const style = getComputedStyle(document.activeElement);
      return {
        outline: style.outlineStyle,
        width: parseFloat(style.outlineWidth),
      };
    });
    expect(focus.outline).not.toBe("none");
    expect(focus.width).toBeGreaterThanOrEqual(2);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(trigger).toBeFocused();
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

test("[touch] 390px create and close controls have 44px hit areas", async ({
  browser,
}) => {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  try {
    const page = await context.newPage();
    const layout = layouts[3];
    await setup(page, layout);
    await page.goto(
      `http://127.0.0.1:${process.env.F0_PORT || 5188}/index.html?page=projects&theme=dark`,
    );
    const trigger = page.getByRole("button", { name: "新建项目", exact: true });
    const bounds = await trigger.boundingBox();
    expect(bounds.height).toBeGreaterThanOrEqual(44);
    expect(bounds.width).toBeGreaterThanOrEqual(44);
    await trigger.tap();
    const close = page
      .getByRole("dialog")
      .getByRole("button", { name: "关闭", exact: true });
    const closeBounds = await close.boundingBox();
    expect(closeBounds.height).toBeGreaterThanOrEqual(44);
    expect(closeBounds.width).toBeGreaterThanOrEqual(44);
    await close.tap();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    await expect(trigger).toBeFocused();
  } finally {
    await context.close();
  }
});

test("[accessibility] final indigo computed token contrast in both themes", async ({
  page,
}, info) => {
  const samples = [];
  for (const theme of ["light", "dark"]) {
    await open(page, "projects", { ...layouts[0], theme });
    const colors = await page.evaluate(() => {
      const tokens = [
        "text",
        "muted",
        "accent",
        "accent-hover",
        "accent-text",
        "success",
        "warning",
        "danger",
        "content",
        "canvas",
        "subtle",
        "selected",
        "strong-border",
      ];
      const probe = document.createElement("span");
      document.body.append(probe);
      const values = Object.fromEntries(
        tokens.map((token) => {
          probe.style.color = `var(--${token})`;
          return [
            token,
            getComputedStyle(probe)
              .color.match(/[\d.]+/g)
              .slice(0, 3)
              .map(Number),
          ];
        }),
      );
      probe.remove();
      return values;
    });
    const luminance = (rgb) =>
      rgb
        .map((value) => value / 255)
        .map((value) =>
          value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4,
        )
        .reduce(
          (sum, value, index) => sum + value * [0.2126, 0.7152, 0.0722][index],
          0,
        );
    const contrast = (a, b) =>
      (Math.max(luminance(colors[a]), luminance(colors[b])) + 0.05) /
      (Math.min(luminance(colors[a]), luminance(colors[b])) + 0.05);
    const pairs = [];
    for (const background of ["content", "canvas", "subtle", "selected"])
      for (const foreground of ["text", "muted"])
        pairs.push([foreground, background, 4.5]);
    for (const foreground of ["success", "warning", "danger"])
      pairs.push([foreground, "content", 4.5]);
    for (const background of ["accent", "accent-hover"])
      pairs.push(["accent-text", background, 4.5]);
    for (const background of ["content", "canvas", "subtle", "selected"])
      pairs.push(["accent", background, 3]);
    for (const background of ["content", "canvas", "subtle"])
      pairs.push(["strong-border", background, 3]);
    for (const [foreground, background, target] of pairs) {
      const ratio = contrast(foreground, background);
      samples.push({ theme, foreground, background, ratio, target });
      expect
        .soft(ratio, `${theme} --${foreground}/--${background}`)
        .toBeGreaterThanOrEqual(target);
    }
  }
  await info.attach("computed-token-contrast", {
    body: JSON.stringify(samples, null, 2),
    contentType: "application/json",
  });
});

test("[accessibility] 200% equivalent reflow and reduced motion", async ({
  browser,
}, info) => {
  // 1440x960 at 200% gives a 720x480 CSS viewport. DPR=2 preserves the output size.
  // This checks browser reflow, not native browser chrome zoom or an on-screen keyboard.
  const context = await browser.newContext({
    viewport: { width: 720, height: 480 },
    deviceScaleFactor: 2,
    reducedMotion: "reduce",
  });
  try {
    const page = await context.newPage();
    for (const name of [
      "projects",
      "tasks",
      "providers",
      "knowledge",
      "meetings",
      "mcp",
      "memory",
    ]) {
      await page.goto(
        `http://127.0.0.1:${process.env.F0_PORT || 5188}/index.html?` +
          new URLSearchParams({ page: name, theme: "light", scenario: "long" }),
      );
      await expect(page.locator("#main")).toBeVisible();
      await noOverflow(page, `200%-reflow/${name}`);
      const motion = await page.evaluate(() => ({
        reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
        dpr: devicePixelRatio,
        active: [...document.querySelectorAll("*")].filter((node) => {
          const style = getComputedStyle(node);
          return (
            style.animationName !== "none" &&
            style.animationDuration
              .split(",")
              .some((value) => parseFloat(value) > 0.001)
          );
        }).length,
      }));
      expect(motion).toEqual({ reduced: true, dpr: 2, active: 0 });
    }
    await capture(page, info, "memory-200pct-equivalent-reflow");
  } finally {
    await context.close();
  }
});
async function metrics(page) {
  return page.evaluate(() => ({
    viewport: { width: innerWidth, height: innerHeight },
    documentWidth: document.documentElement.scrollWidth,
    bodyWidth: document.body.scrollWidth,
    headings: [...document.querySelectorAll("main h1")]
      .filter((node) => node.getClientRects().length)
      .map((node) => node.textContent.trim()),
    firstTaskY: document
      .querySelector('[data-testid="task-card"]')
      ?.getBoundingClientRect().top,
  }));
}
async function capture(page, info, name) {
  mkdirSync(screens, { recursive: true });
  const file = path.join(screens, name + ".png");
  await page.screenshot({ path: file, fullPage: true, animations: "disabled" });
  await info.attach(name, { path: file, contentType: "image/png" });
  await info.attach(name + "-metrics", {
    body: JSON.stringify(await metrics(page), null, 2),
    contentType: "application/json",
  });
}
async function noOverflow(page, context) {
  const value = await metrics(page);
  expect(
    value.documentWidth,
    context + " document overflow",
  ).toBeLessThanOrEqual(value.viewport.width + 1);
  expect(value.bodyWidth, context + " body overflow").toBeLessThanOrEqual(
    value.viewport.width + 1,
  );
  expect(value.headings, context + " single page heading").toHaveLength(1);
}
for (const name of pages)
  for (const layout of layouts) {
    test(`[matrix] ${name} ${layout.width} ${layout.theme}`, async ({
      page,
    }, info) => {
      const faults = await setup(page, layout);
      for (const scenario of states) {
        await test.step(scenario, async () => {
          await open(page, name, layout, scenario);
          await noOverflow(
            page,
            `${name}/${scenario}/${layout.width}/${layout.theme}`,
          );
          if (
            scenario === "normal" &&
            ((layout.width === 1440 && layout.theme === "light") ||
              (layout.width === 390 && layout.theme === "dark"))
          ) {
            await capture(
              page,
              info,
              `${name}-${layout.width}-${layout.theme}`,
            );
          }
          if (name === "tasks" && scenario === "normal") {
            const y = (await metrics(page)).firstTaskY;
            expect(y, "normal board has a visible first task").toBeDefined();
            expect(y).toBeLessThanOrEqual(layout.width === 390 ? 420 : 340);
          }
        });
      }
      expect(faults.errors).toEqual([]);
      expect(faults.outbound).toEqual([]);
    });
  }

// The earlier F0 prototype has no general-settings page. This baseline is the
// current production Vue route, rendered with the existing synthetic HTTP fixture.
for (const layout of layouts) {
  test(`[general-baseline] formal Vue ${layout.width} ${layout.theme}`, async ({
    page,
  }, info) => {
    if (process.env.F0_LEGACY !== "1")
      throw new Error(
        "General baseline requires F0_LEGACY=1 and external web dependencies",
      );
    const faults = await setup(page, layout);
    const fixture = await installFixture(page);
    await page.goto(
      `http://127.0.0.1:${process.env.F0_LEGACY_PORT || 5189}${projectPath}/settings/general`,
    );
    await expect(
      page.getByRole("heading", { name: "基本信息", exact: true, level: 1 }),
    ).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute(
      "data-theme",
      layout.theme,
    );
    await expect(
      page.getByRole("textbox", { name: "项目名称", exact: true }),
    ).toHaveValue(baselineProject.name);
    await expect(
      page.getByRole("textbox", { name: "项目描述", exact: true }),
    ).toHaveValue(baselineProject.description);
    await expect(
      page.getByRole("button", { name: "保存修改", exact: true }),
    ).toBeDisabled();
    await expect(page.locator(".project-records")).not.toHaveAttribute(
      "open",
      "",
    );
    await expect(page.locator('.ui-state[aria-busy="true"]')).toHaveCount(0);
    await page.evaluate(() => document.fonts.ready);
    await noOverflow(page, `formal-general/${layout.width}/${layout.theme}`);
    await capture(page, info, `before-general-${layout.width}-${layout.theme}`);
    if (layout.width === 390) {
      // The formal app scrolls inside main. Keep the actual shell unchanged and
      // supplement the initial viewport with a real wheel journey to its end.
      await page.locator("main").hover();
      await page.mouse.wheel(0, 1200);
      await expect(page.locator(".project-records summary")).toBeInViewport();
      await capture(
        page,
        info,
        `before-general-${layout.width}-${layout.theme}-bottom`,
      );
    }
    await info.attach("formal-general-fixture", {
      body: JSON.stringify(
        {
          implementation: "web/src/views/projects/ProjectGeneralSettings.vue",
          route: `${projectPath}/settings/general`,
          viewport: layout,
          project: baselineProject,
          projectRecordsExpanded: false,
          requests: fixture.requests,
        },
        null,
        2,
      ),
      contentType: "application/json",
    });
    expect(fixture.requests.every((request) => request.method === "GET")).toBe(
      true,
    );
    expect(fixture.unexpected).toEqual([]);
    expect(fixture.errors).toEqual([]);
    expect(fixture.consoleErrors).toEqual([]);
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

async function openGeneral(page, layout, scenario = "normal") {
  await open(page, "settings", layout, scenario, {
    fixture: "baseline",
    accent: "neutral",
  });
}

for (const layout of layouts) {
  test(`[general] baseline edit cancel save rename ${layout.width} ${layout.theme}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    await openGeneral(page, layout);
    const name = page.getByRole("textbox", { name: "项目名称", exact: true });
    const description = page.getByRole("textbox", {
      name: "项目描述",
      exact: true,
    });
    const save = page.getByRole("button", { name: "保存修改", exact: true });
    await expect(name).toHaveValue(baselineProject.name);
    await expect(description).toHaveValue(baselineProject.description);
    await expect(save).toBeDisabled();
    await expect(page.locator(".general-records")).not.toHaveAttribute(
      "open",
      "",
    );
    await expect(page.locator("html")).toHaveAttribute(
      "data-theme",
      layout.theme,
    );
    await noOverflow(page, "new general initial");
    await capture(page, info, `after-general-${layout.width}-${layout.theme}`);
    await description.fill("临时草稿，不应保留。");
    await expect(save).toBeEnabled();
    await page.getByRole("button", { name: "取消修改", exact: true }).click();
    await expect(description).toHaveValue(baselineProject.description);
    await expect(name).toBeFocused();
    const savedDescription =
      baselineProject.description + "\n确认本页演示保存。";
    await description.fill(savedDescription);
    await save.click();
    await expect(page.locator(".general-feedback")).toContainText(
      "已保存到当前样板，未调用服务端。",
    );
    await expect(description).toHaveValue(savedDescription);
    await expect(page.locator(".general-edit-version")).toHaveText(
      "编辑版本 2",
    );
    await expect(save).toBeDisabled();
    await name.fill("Atlas.Refined");
    await save.click();
    const dialog = page.getByRole("dialog", { name: "确认更改项目名称？" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("旧名称链接将失效");
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(save).toBeFocused();
    await expect(name).toHaveValue("Atlas.Refined");
    await expect(page.locator(".general-edit-version")).toHaveText(
      "编辑版本 2",
    );
    await save.press("Enter");
    await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(page.locator(".general-edit-version")).toHaveText(
      "编辑版本 3",
    );
    await expect(page.locator(".project-identity")).toContainText(
      "Atlas.Refined",
    );
    await expect(save).toBeDisabled();
    if (layout.width === 390) {
      await page.locator(".general-records summary").scrollIntoViewIfNeeded();
      await expect(page.locator(".general-records summary")).toBeInViewport();
    }
    await noOverflow(page, "new general after editing");
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

test("[general] readonly read failure conflict and unknown recovery", async ({
  page,
}) => {
  const layout = layouts[3];
  const faults = await setup(page, layout);
  await openGeneral(page, layout, "readonly");
  await expect(
    page.getByRole("textbox", { name: "项目名称", exact: true }),
  ).toHaveAttribute("readonly", "");
  await expect(
    page.getByRole("textbox", { name: "项目描述", exact: true }),
  ).toHaveAttribute("readonly", "");
  await expect(
    page.getByRole("button", { name: "保存修改", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "取消修改", exact: true }),
  ).toBeDisabled();
  await openGeneral(page, layout, "error");
  await expect(
    page.getByRole("heading", { name: "暂时无法读取项目信息" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "重新读取当前值", exact: true })
    .click();
  await expect(
    page.getByRole("textbox", { name: "项目名称", exact: true }),
  ).toHaveValue("Atlas");
  await openGeneral(page, layout, "conflict");
  const description = page.getByRole("textbox", {
    name: "项目描述",
    exact: true,
  });
  const oldDraft = await description.inputValue();
  await expect(
    page.getByRole("button", { name: "保存修改", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "重新读取当前值", exact: true })
    .click();
  await expect(description).toHaveValue(oldDraft);
  await expect(page.locator(".general-comparison")).toContainText(
    "当前已保存的信息包含最新协作范围。",
  );
  await page
    .getByRole("button", { name: "使用当前值重新编辑", exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(description).toHaveValue(oldDraft);
  await page
    .getByRole("button", { name: "使用当前值重新编辑", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "采用当前值", exact: true })
    .click();
  await expect(description).toHaveValue(
    baselineProject.description + "\n当前已保存的信息包含最新协作范围。",
  );
  await expect(page.locator(".general-edit-version")).toHaveText("编辑版本 2");
  await openGeneral(page, layout, "unknown");
  const unknownDraft = await description.inputValue();
  await expect(
    page.getByRole("button", { name: "保存修改", exact: true }),
  ).toBeDisabled();
  const replay = page.getByRole("button", {
    name: "按原请求重放",
    exact: true,
  });
  await expect(replay).toBeDisabled();
  await page.getByRole("button", { name: "查证原命令", exact: true }).click();
  await expect(page.locator(".general-recovery-result")).toContainText(
    "原命令仍在处理中",
  );
  await page.getByRole("button", { name: "查证原命令", exact: true }).click();
  await expect(page.locator(".general-recovery-result")).toContainText(
    "不能据此判断它未提交或已回滚",
  );
  await replay.click();
  await expect(replay).toBeDisabled();
  await expect(description).toHaveValue(unknownDraft);
  await page.getByRole("button", { name: "放弃本地追踪", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "放弃本地追踪", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "需要重新读取当前项目" }),
  ).toBeVisible();
  await expect(page.locator("#general-form")).toHaveCount(0);
  await page
    .getByRole("button", { name: "重新读取当前值", exact: true })
    .click();
  await expect(description).toHaveValue(baselineProject.description);
  await noOverflow(page, "general recovery narrow");
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

async function startMotionObservation(page, selector) {
  await page.evaluate((selector) => {
    window.__generalMotion = new Promise((resolve) => {
      const observer = new MutationObserver(() => {
        const target = document.querySelector(selector);
        if (!target) return;
        observer.disconnect();
        const samples = [];
        function frame() {
          const style = getComputedStyle(target);
          samples.push({
            opacity: style.opacity,
            transform: style.transform,
            animations: target.getAnimations().length,
          });
          if (samples.length < 18) requestAnimationFrame(frame);
          else resolve(samples);
        }
        requestAnimationFrame(frame);
      });
      observer.observe(document.body, {
        childList: true,
        attributes: true,
        subtree: true,
      });
    });
  }, selector);
}
for (const layout of [layouts[0], layouts[3]]) {
  test(`[general] menus keyboard motion and settings routes ${layout.width}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    await openGeneral(page, layout);
    for (const motion of ["no-preference", "reduce"]) {
      await page.emulateMedia({ reducedMotion: motion });
      const more = page.getByRole("button", {
        name: "更多项目信息操作",
        exact: true,
      });
      await more.focus();
      await startMotionObservation(page, ".floating-menu");
      await page.keyboard.press("Enter");
      const samples = await page.evaluate(() => window.__generalMotion);
      await info.attach(`menu-${motion}-frames`, {
        body: JSON.stringify(samples),
        contentType: "application/json",
      });
      if (motion === "no-preference")
        expect(
          new Set(samples.map((sample) => sample.opacity)).size,
        ).toBeGreaterThan(1);
      else
        expect(
          samples.every(
            (sample) => sample.opacity === "1" && sample.animations === 0,
          ),
        ).toBe(true);
      await expect(
        page.getByRole("menuitem", { name: "重新读取当前值", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("End");
      await expect(
        page.getByRole("menuitem", { name: "查看项目信息与标识", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(page.getByRole("menu")).toHaveCount(0);
      await expect(more).toBeFocused();
      await page
        .getByRole("textbox", { name: "项目名称", exact: true })
        .fill("Atlas.Keyboard");
      const save = page.getByRole("button", { name: "保存修改", exact: true });
      await save.focus();
      await startMotionObservation(page, "#dialog[open]");
      await page.keyboard.press("Enter");
      const dialogSamples = await page.evaluate(() => window.__generalMotion);
      await info.attach(`dialog-${motion}-frames`, {
        body: JSON.stringify(dialogSamples),
        contentType: "application/json",
      });
      if (motion === "no-preference")
        expect(
          new Set(dialogSamples.map((sample) => sample.opacity)).size,
        ).toBeGreaterThan(1);
      else
        expect(
          dialogSamples.every(
            (sample) => sample.opacity === "1" && sample.animations === 0,
          ),
        ).toBe(true);
      for (let count = 0; count < 4; count++) {
        await page.keyboard.press("Tab");
        expect(
          await page
            .locator("#dialog")
            .evaluate((dialog) => dialog.contains(document.activeElement)),
        ).toBe(true);
      }
      await page.keyboard.press("Escape");
      await expect(save).toBeFocused();
      await page.getByRole("button", { name: "取消修改", exact: true }).click();
    }
    if (layout.width === 390) {
      const trigger = page.locator("#settings-section-menu");
      await trigger.focus();
      await page.keyboard.press("Enter");
      await expect(
        page.getByRole("menuitem", { name: "基本信息", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(trigger).toBeFocused();
      await trigger.click();
      await page.getByRole("menuitem", { name: "MCP", exact: true }).click();
    } else
      await page
        .getByRole("navigation", { name: "设置栏目", exact: true })
        .getByRole("link", { name: "MCP", exact: true })
        .click();
    await expect(page).toHaveURL(/page=mcp/);
    await page
      .getByRole("navigation", { name: "项目菜单", exact: true })
      .getByRole("link", { name: "项目设置", exact: true })
      .click();
    await expect(page).toHaveURL(/page=settings/);
    await expect(
      page.getByRole("heading", { name: "基本信息", exact: true, level: 1 }),
    ).toBeVisible();
    await page.locator('.project-identity [data-page="project"]').click();
    await page
      .getByRole("button", { name: "查看基本信息", exact: true })
      .click();
    await expect(page).toHaveURL(/page=settings/);
    await expect(
      page.getByRole("textbox", { name: "项目名称", exact: true }),
    ).toHaveValue("Atlas");
    await noOverflow(page, "general routes");
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

const comparison = [
  {
    name: "projects",
    route: "/projects",
    ready: (page) => page.getByRole("link", { name: "Atlas", exact: true }),
  },
  {
    name: "tasks",
    route: `${projectPath}/tasks/sprints/${sprintID}`,
    ready: (page) => page.getByText("布局检查 1", { exact: true }).first(),
  },
  {
    name: "providers",
    route: "/system/providers",
    ready: (page) =>
      page.getByText("Research Provider / 共享服务", { exact: true }).first(),
  },
];
for (const entry of comparison)
  for (const layout of [layouts[0], layouts[3]]) {
    test(`[comparison] ${entry.name} ${layout.width} ${layout.theme}`, async ({
      page,
    }, info) => {
      if (process.env.F0_LEGACY !== "1")
        throw new Error(
          "Comparison requires F0_LEGACY=1 and the locked external web dependency install",
        );
      const faults = await setup(page, layout);
      const fixture = await installFixture(page);
      await page.goto(
        `http://127.0.0.1:${process.env.F0_LEGACY_PORT || 5189}${entry.route}`,
      );
      await expect(entry.ready(page)).toBeVisible();
      if (entry.name === "providers") {
        await page
          .getByRole("button", {
            name: `查看 ${provider.input.name}`,
            exact: true,
          })
          .click();
        await expect(
          page.getByRole("region", { name: "Provider详情", exact: true }),
        ).toBeVisible();
      }
      await expect(page.locator('.ui-state[aria-busy="true"]')).toHaveCount(0);
      await page.evaluate(() => document.fonts.ready);
      await capture(
        page,
        info,
        `before-${entry.name}-${layout.width}-${layout.theme}`,
      );
      expect(fixture.unexpected).toEqual([]);
      expect(fixture.errors).toEqual([]);
      expect(fixture.consoleErrors).toEqual([]);
      await open(page, entry.name, layout, "normal", { fixture: "baseline" });
      if (entry.name === "projects") {
        expect(
          await page
            .locator('[data-testid="project-row"] .project-name')
            .allTextContents(),
        ).toEqual(fixture.items.map((item) => item.name));
        const rows = await page.getByTestId("project-row").allTextContents();
        for (const [index, item] of fixture.items.entries())
          if (item.description) expect(rows[index]).toContain(item.description);
      }
      if (entry.name === "tasks")
        expect(
          (
            await page.locator('[data-testid="task-card"] h3').allTextContents()
          ).sort(),
        ).toEqual(fixture.tasks.map((item) => item.title).sort());
      if (entry.name === "providers") {
        await expect(page.getByLabel("名称", { exact: true })).toHaveValue(
          provider.input.name,
        );
        await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue(
          provider.input.base_url,
        );
        await expect(
          page.getByLabel("启用 Provider", { exact: true }),
        ).toBeChecked();
        await expect(page.locator("#provider-form")).toContainText("未绑定");
      }
      await noOverflow(page, `after/${entry.name}`);
      await capture(
        page,
        info,
        `after-${entry.name}-${layout.width}-${layout.theme}`,
      );
      expect(faults.errors).toEqual([]);
      expect(faults.outbound).toEqual([]);
    });
  }
