// All business facts below are synthetic. These checks do not prove backend behavior.
const { test, expect, chromium } = require("@playwright/test");
const {
  mkdirSync,
  readFileSync,
  writeFileSync,
  mkdtempSync,
  rmSync,
} = require("node:fs");
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
  agents: "Agent",
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
      await expect(more).not.toHaveAttribute("aria-controls", /.+/);
      await more.press("Enter");
      await page.keyboard.press("Tab");
      await expect(page.getByRole("menu")).toHaveCount(0);
      await expect(
        page.getByRole("textbox", { name: "项目名称", exact: true }),
      ).toBeFocused();
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
      const menu = page.getByRole("menu", { name: "项目设置", exact: true });
      await menu.hover();
      await page.mouse.wheel(0, 180);
      await expect
        .poll(() => menu.evaluate((node) => node.scrollTop))
        .toBeGreaterThan(0);
      await expect(menu).toBeVisible();
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
      page.getByRole("heading", { name: /^基本信息/, level: 1 }),
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

test("[general-refinement] four layouts and real scroll to bottom", async ({
  page,
}, info) => {
  const faults = await setup(page, layouts[0]);
  for (const layout of layouts) {
    await page.setViewportSize({ width: layout.width, height: layout.height });
    await openGeneral(page, layout);
    await noOverflow(page, `general second ${layout.width}/${layout.theme}`);
    await capture(
      page,
      info,
      `after-general-second-${layout.width}-${layout.theme}`,
    );
    await page.locator(".general-records summary").click();
    await page.mouse.move(layout.width / 2, layout.height - 100);
    await page.mouse.wheel(0, 1400);
    const lastRecord = page.locator(".general-records time").last();
    await expect(lastRecord).toBeInViewport();
    expect(
      await lastRecord.evaluate((node) => {
        const rect = node.getBoundingClientRect();
        return node.contains(
          document.elementFromPoint(
            rect.x + rect.width / 2,
            rect.y + rect.height / 2,
          ),
        );
      }),
    ).toBe(true);
    await noOverflow(
      page,
      `general second expanded ${layout.width}/${layout.theme}`,
    );
  }
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[general-refinement] shared projects filter Tab Escape and page scroll", async ({
  page,
}, info) => {
  // Six rows fit at 390x844. This short window creates actual page scrolling.
  const layout = { ...layouts[3], height: 600 };
  const faults = await setup(page, layout);
  await open(page, "projects", layout, "long", { accent: "neutral" });
  expect(
    await page.evaluate(() => document.documentElement.scrollHeight),
  ).toBeGreaterThan(layout.height);
  const trigger = page.getByRole("button", { name: "状态", exact: true });
  const menu = page.getByRole("listbox", { name: "状态", exact: true });
  await trigger.focus();
  await page.keyboard.press("ArrowDown");
  await expect(menu).toBeVisible();
  await expect(
    page.getByRole("option", { name: "全部状态", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(menu).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "刷新", exact: true }),
  ).toBeFocused();
  await expect(trigger).not.toHaveAttribute("aria-controls", /.+/);
  await trigger.click();
  await page.keyboard.press("End");
  await expect(
    page.getByRole("option", { name: "删除中", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  await trigger.click();
  mkdirSync(screens, { recursive: true });
  const screenshot = path.join(
    screens,
    "projects-filter-second-390x600-dark.png",
  );
  // A fullPage screenshot may resize the browser and correctly dismiss this menu.
  await page.screenshot({
    path: screenshot,
    fullPage: false,
    animations: "disabled",
  });
  await info.attach("projects-filter-second-390-dark", {
    path: screenshot,
    contentType: "image/png",
  });
  await expect(menu).toBeVisible();
  const initialScroll = await page.evaluate(() => scrollY);
  await page.mouse.move(layout.width - 15, layout.height - 100);
  await page.mouse.wheel(0, 600);
  await expect
    .poll(() => page.evaluate(() => scrollY))
    .toBeGreaterThan(initialScroll);
  await expect(menu).toHaveCount(0);
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  await expect(trigger).not.toHaveAttribute("aria-controls", /.+/);
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[general-native-zoom] real Chromium 200% edit save menu and records", async ({}, info) => {
  mkdirSync(output, { recursive: true });
  const profile = mkdtempSync(path.join(output, "native-zoom-profile-"));
  mkdirSync(path.join(profile, "Default"));
  // Reuse review/interact.mjs's native Chromium partition zoom, not CSS zoom or
  // device emulation. The assertions below verify the actual browser result.
  writeFileSync(
    path.join(profile, "Default/Preferences"),
    JSON.stringify({
      partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
    }),
  );
  const origin = `http://127.0.0.1:${process.env.F0_PORT || 5188}`;
  const faults = { errors: [], outbound: [] };
  let context;
  try {
    context = await chromium.launchPersistentContext(profile, {
      executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      baseURL: origin,
      reducedMotion: "reduce",
    });
    await context.route("**/*", (route) => {
      const request = route.request();
      if (
        new URL(request.url()).origin === origin &&
        ["GET", "HEAD"].includes(request.method())
      )
        return route.continue();
      faults.outbound.push(`${request.method()} ${request.url()}`);
      return route.abort("blockedbyclient");
    });
    const page = context.pages()[0] || (await context.newPage());
    page.on("pageerror", (error) => faults.errors.push(error.message));
    await openGeneral(page, { theme: "light" });
    await page.evaluate(() => {
      window.__generalZoomEvents = [];
      const record = (kind, target) =>
        window.__generalZoomEvents.push({
          kind,
          target,
          time: performance.now(),
          scrollY,
          more: document
            .querySelector("#general-more")
            ?.getBoundingClientRect()
            .toJSON(),
          menu: !!document.querySelector(".floating-menu"),
        });
      record("initial", "");
      for (const name of ["scroll", "pointerdown", "click"])
        document.addEventListener(
          name,
          (event) =>
            record(name, event.target.id || event.target.tagName || "document"),
          true,
        );
      new MutationObserver((records) => {
        for (const mutation of records) {
          for (const node of mutation.addedNodes)
            if (node.matches?.(".floating-menu")) record("menu-added", node.id);
          for (const node of mutation.removedNodes)
            if (node.matches?.(".floating-menu"))
              record("menu-removed", node.id);
        }
      }).observe(document.body, { childList: true });
    });
    const actual = await page.evaluate(() => ({
      innerWidth,
      innerHeight,
      outerWidth,
      outerHeight,
      dpr: devicePixelRatio,
      visualViewport: {
        width: visualViewport.width,
        height: visualViewport.height,
        scale: visualViewport.scale,
      },
      rootZoom: getComputedStyle(document.documentElement).zoom,
    }));
    await info.attach("native-200pct-actual-viewport", {
      body: JSON.stringify(actual, null, 2),
      contentType: "application/json",
    });
    expect(actual.outerWidth).toBe(1440);
    expect(actual.innerWidth).toBe(720);
    expect(actual.dpr).toBe(2);
    expect(actual.visualViewport.width).toBe(720);
    // visualViewport preserves fractional CSS pixels; innerHeight is integral.
    expect(Math.floor(actual.visualViewport.height)).toBe(actual.innerHeight);
    expect(actual.rootZoom).toBe("1");
    await noOverflow(page, "general native 200% initial");
    const description = page.getByRole("textbox", {
      name: "项目描述",
      exact: true,
    });
    await description.fill(
      baselineProject.description + "\n200% 缩放编辑检查。",
    );
    await page.getByRole("button", { name: "保存修改", exact: true }).click();
    await expect(page.locator(".general-feedback")).toContainText(
      "已保存到当前样板，未调用服务端。",
    );
    await expect(page.locator(".general-edit-version")).toHaveText(
      "编辑版本 2",
    );
    await expect(description).toHaveValue(
      baselineProject.description + "\n200% 缩放编辑检查。",
    );
    const more = page.getByRole("button", {
      name: "更多项目信息操作",
      exact: true,
    });
    await more.click();
    const menu = page.getByRole("menu", { name: "项目信息", exact: true });
    try {
      await expect(menu).toBeVisible();
    } catch (error) {
      const probe = await context.newCDPSession(page);
      const capture = await probe.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      mkdirSync(screens, { recursive: true });
      const file = path.join(screens, "general-native-200pct-menu-failure.png");
      writeFileSync(file, Buffer.from(capture.data, "base64"));
      await info.attach("native-menu-failure", {
        path: file,
        contentType: "image/png",
      });
      await probe.detach();
      throw error;
    } finally {
      await info.attach("native-menu-event-sequence", {
        body: JSON.stringify(
          await page.evaluate(() => window.__generalZoomEvents),
          null,
          2,
        ),
        contentType: "application/json",
      });
    }
    expect(
      await menu.evaluate((node) => {
        const r = node.getBoundingClientRect();
        return (
          r.left >= 0 &&
          r.top >= 0 &&
          r.right <= innerWidth &&
          r.bottom <= innerHeight
        );
      }),
    ).toBe(true);
    const cdp = await context.newCDPSession(page);
    async function captureNative(name) {
      const data = await cdp.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      mkdirSync(screens, { recursive: true });
      const file = path.join(screens, name + ".png");
      writeFileSync(file, Buffer.from(data.data, "base64"));
      await info.attach(name, { path: file, contentType: "image/png" });
    }
    await captureNative("general-native-200pct-menu");
    await page.keyboard.press("Escape");
    await expect(more).toBeFocused();
    await more.press("Enter");
    await page
      .getByRole("menuitem", { name: "查看项目信息与标识", exact: true })
      .click();
    await expect(page.locator(".general-records")).toHaveAttribute("open", "");
    await page.mouse.move(650, actual.innerHeight - 60);
    await page.mouse.wheel(0, 1200);
    const last = page.locator(".general-records time").last();
    await expect(last).toBeInViewport();
    expect(
      await last.evaluate((node) => {
        const r = node.getBoundingClientRect();
        return node.contains(
          document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2),
        );
      }),
    ).toBe(true);
    await noOverflow(page, "general native 200% expanded records");
    await captureNative("general-native-200pct-records");
    await cdp.detach();
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  } finally {
    await context?.close();
    rmSync(profile, { recursive: true, force: true });
    writeFileSync(
      path.join(output, "native-zoom-lifecycle.json"),
      JSON.stringify({ contextClosed: true, profileRemoved: true }, null, 2),
    );
  }
});

async function openAgents(page, layout, extra = {}, scenario = "normal") {
  await open(page, "agents", layout, scenario, {
    fixture: "design",
    accent: "neutral",
    ...extra,
  });
}
const agentExamples = {
  reviewer: {
    title: "Reviewer",
    description: "审查实现与交付结果，记录风险与改进建议。",
  },
  researcher: {
    title: "Researcher",
    description: "收集资料与核对来源，为团队提供研究依据。",
  },
};
for (const layout of [layouts[0], { ...layouts[3], height: 480 }]) {
  test(`[agents-first] explicit identity Memory return and direct URLs ${layout.width}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    await openAgents(page, layout);
    await expect(
      page.getByRole("heading", { name: "Agent", exact: true, level: 1 }),
    ).toBeVisible();
    await expect(page.locator(".agent-directory-row")).toHaveCount(3);
    await expect(
      page.locator("#agent-configuration-form, .agent-memory-list"),
    ).toHaveCount(0);
    for (const [id, expected] of Object.entries(agentExamples)) {
      const row = page.locator(
        `.agent-directory-link[data-agent-open="agent-${id}"]`,
      );
      await row.scrollIntoViewIfNeeded();
      const previousY = await page.evaluate(() => scrollY);
      if (layout.width === 390 && id === "reviewer")
        expect(previousY).toBeGreaterThan(0);
      await row.click();
      await expect(page).toHaveURL(new RegExp(`agent=agent-${id}(?:&|$)`));
      await expect(
        page.getByRole("heading", {
          name: expected.title,
          exact: true,
          level: 1,
        }),
      ).toBeVisible();
      await expect(page.locator(".agent-directory-row")).toHaveCount(0);
      await expect(
        page.getByRole("textbox", { name: "Agent 名称", exact: true }),
      ).toHaveValue(id);
      await expect(
        page.getByRole("textbox", { name: "描述", exact: true }),
      ).toHaveValue(expected.description);
      const memory = page.locator('[data-agent-tab="memory"]');
      await memory.click();
      await expect(page).toHaveURL(/view=memory/);
      await expect(page.locator(".settings-page-heading > div > p")).toHaveText(
        expected.description,
      );
      await page
        .locator(`[data-agent-action="memory-history:memory-${id}-1"]`)
        .click();
      await page.locator(".agent-memory-history summary").click();
      await expect(page.locator(".agent-memory-history")).toContainText(
        `execution-${id}-1`,
      );
      await expect(page.locator(".agent-memory-history")).not.toContainText(
        "execution-architecture-1",
      );
      await page.locator('[data-agent-tab="basic"]').click();
      await expect(page.locator('[data-agent-tab="basic"]')).toBeFocused();
      await expect(
        page.getByRole("textbox", { name: "Agent 名称", exact: true }),
      ).toHaveValue(id);
      await page.locator('[data-agent-back=""]').click();
      await expect(row).toBeFocused();
      await expect
        .poll(() => page.evaluate(() => scrollY))
        .toBeCloseTo(previousY, 0);
    }
    await page
      .locator('.agent-directory-link[data-agent-open="agent-reviewer"]')
      .click();
    await page.goBack({ waitUntil: "commit" });
    await expect(page.locator(".agent-directory-row")).toHaveCount(3);
    await expect(
      page.locator('.agent-directory-link[data-agent-open="agent-reviewer"]'),
    ).toBeFocused();
    for (const view of ["resources", "memory"]) {
      await openAgents(page, layout, { agent: "agent-researcher", view });
      await page.reload();
      await expect(page.locator("#agent-page-title")).toHaveText("Researcher");
      await expect(page.locator(".settings-page-heading > div > p")).toHaveText(
        agentExamples.researcher.description,
      );
      await expect(page.locator(`[data-agent-tab="${view}"]`)).toHaveAttribute(
        "aria-selected",
        "true",
      );
      if (view === "memory")
        await expect(
          page.locator(
            '[data-agent-action="memory-history:memory-researcher-1"]',
          ),
        ).toBeVisible();
      else
        await expect(
          page.locator('[data-agent-section="runners"]'),
        ).toBeVisible();
    }
    await openAgents(page, layout, { agent: "agent-missing" });
    await expect(
      page.getByRole("heading", { name: "Agent 不可用", exact: true }),
    ).toBeVisible();
    await expect(
      page.locator("#agent-configuration-form, .agent-memory-list"),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "返回 Agent 列表", exact: true })
      .click();
    await expect(page.locator(".agent-directory-row")).toHaveCount(3);
    await noOverflow(page, "Agent list restored");
    await capture(
      page,
      info,
      `agent-list-first-${layout.width}-${layout.theme}`,
    );
    expect(faults.errors).toEqual([]);
    expect(faults.outbound).toEqual([]);
  });
}

test("[agents-first] settings header geometry and left aligned forms", async ({
  page,
}, info) => {
  const faults = await setup(page, layouts[0]);
  const measured = [];
  const routes = [
    { page: "settings", body: "#general-form" },
    { page: "agents", body: ".agent-directory" },
    { page: "agents", agent: "agent-reviewer", body: ".agent-detail-content" },
    {
      page: "agents",
      agent: "agent-reviewer",
      view: "resources",
      body: ".agent-detail-content",
    },
    {
      page: "agents",
      agent: "agent-reviewer",
      view: "memory",
      body: ".agent-detail-content",
    },
    { page: "mcp", body: ".co-mcp-layout" },
  ];
  for (const layout of [layouts[0], layouts[3]]) {
    await page.setViewportSize({ width: layout.width, height: layout.height });
    let baseline;
    for (const route of routes) {
      const { body, ...extra } = route;
      await open(page, route.page, layout, "normal", {
        fixture: "design",
        accent: "neutral",
        ...extra,
      });
      await expect(page.locator(".settings-page-heading")).toHaveCount(1);
      const boxes = await page.evaluate((body) => {
        const box = (selector) => {
          const node = document.querySelector(selector);
          if (!node) return null;
          const r = node.getBoundingClientRect();
          return {
            x: r.x,
            y: r.y,
            bottom: r.bottom,
            width: r.width,
            right: r.right,
          };
        };
        return {
          header: box(".settings-page-heading"),
          title: box(".settings-page-heading h1"),
          description: box(".settings-page-heading > div > p"),
          body: box(body),
        };
      }, body);
      baseline ||= boxes;
      // The user requires shared horizontal alignment, not equal heading y.
      // Parent navigation and an optional object description occupy natural
      // vertical space; absence of description must not require a blank row.
      expect(Math.abs(boxes.header.x - baseline.header.x)).toBeLessThanOrEqual(
        1,
      );
      expect(
        Math.abs(boxes.header.width - baseline.header.width),
      ).toBeLessThanOrEqual(1);
      expect(Math.abs(boxes.title.x - baseline.title.x)).toBeLessThanOrEqual(1);
      if (boxes.description)
        expect(
          Math.abs(boxes.description.x - boxes.title.x),
        ).toBeLessThanOrEqual(1);
      if (route.agent) {
        const description = page.locator(".settings-page-heading > div > p");
        await expect(description).toHaveText(
          agentExamples.reviewer.description,
        );
        await expect(description).not.toHaveText("Reviewer");
        await expect(page.locator("#agent-page-title")).toHaveText("Reviewer");
      }
      if (route.agent) {
        const back = page.locator("[data-agent-back]");
        await expect(back).toHaveAccessibleName("Agent");
        const navigation = await back.evaluate((node) => {
          const title = document.querySelector(".settings-page-heading h1");
          const r = node.getBoundingClientRect();
          return {
            x: r.x,
            bottom: r.bottom,
            independentNavigation: !!node.closest('nav, [role="navigation"]'),
            outsideDescription: !node.closest("p"),
            beforeTitle: !!(
              node.compareDocumentPosition(title) &
              Node.DOCUMENT_POSITION_FOLLOWING
            ),
          };
        });
        expect(navigation.independentNavigation).toBe(true);
        expect(navigation.outsideDescription).toBe(true);
        expect(navigation.beforeTitle).toBe(true);
        expect(navigation.bottom).toBeLessThanOrEqual(boxes.title.y);
        expect(Math.abs(navigation.x - boxes.title.x)).toBeLessThanOrEqual(1);
        boxes.navigation = navigation;
      }
      expect(Math.abs(boxes.body.x - boxes.header.x)).toBeLessThanOrEqual(1);
      if (body === "#general-form" || body === ".agent-detail-content")
        expect(boxes.body.width).toBeLessThanOrEqual(760);
      measured.push({ route: extra, layout, boxes });
      await noOverflow(page, `settings heading ${JSON.stringify(extra)}`);
    }
  }
  await info.attach("settings-heading-geometry", {
    body: JSON.stringify(measured, null, 2),
    contentType: "application/json",
  });
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[agents-first] existing General dirty browser Back guard", async ({
  page,
}) => {
  const layout = layouts[0];
  const faults = await setup(page, layout);
  await openAgents(page, layout);
  await page
    .getByRole("navigation", { name: "项目菜单", exact: true })
    .getByRole("link", { name: "项目设置", exact: true })
    .click();
  await page
    .locator("#general-description")
    .fill("保留 General 草稿，直到明确选择放弃。");
  await page.goBack({ waitUntil: "commit" });
  const dialog = page.getByRole("dialog", {
    name: "放弃项目修改？",
    exact: true,
  });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page).toHaveURL(/page=settings/);
  await expect(page.locator("#general-description")).toHaveValue(
    "保留 General 草稿，直到明确选择放弃。",
  );
  await page.goBack({ waitUntil: "commit" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "放弃并离开", exact: true }).click();
  await expect(page).toHaveURL(/page=agents/);
  await expect(page.locator(".agent-directory-row")).toHaveCount(3);
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[agents-final] unknown Memory keeps its original command across return", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  await openAgents(
    page,
    layouts[3],
    { agent: "agent-reviewer", view: "memory" },
    "unknown",
  );
  await expect(
    page.getByRole("button", { name: "查询原删除结果", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "查询原删除结果", exact: true })
    .click();
  await expect(page.locator(".agent-notice")).toContainText(
    "尚未观察到可确认结果",
  );
  await expect(
    page.locator('[data-agent-action^="memory-delete:"]').first(),
  ).toBeDisabled();
  await page.locator('[data-agent-tab="basic"]').click();
  await expect(page.locator("#agent-save")).toBeDisabled();
  await expect(page.locator(".agent-notice")).not.toContainText(
    "保存结果待确认",
  );
  await page.locator('[data-agent-back=""]').click();
  await page.locator("[data-agent-leave]").click();
  await page.getByRole("button", { name: "查看原 Agent", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "查询原删除结果", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/view=memory/);
  await expect(
    page.locator('[data-agent-action="memory-history:memory-reviewer-1"]'),
  ).toBeVisible();
  expect(faults.errors).toEqual([]);
  expect(faults.outbound).toEqual([]);
});

test("[agents-final] dirty history and independent resources survive basic cancel and save", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  await openAgents(page, layouts[3], { resource_extra_secret: "1" });
  await page
    .locator('.agent-directory-link[data-agent-open="agent-reviewer"]')
    .click();
  const description = page.locator("#agent-description");
  const draft = "仅属于 Reviewer 的待保存配置。";
  await description.fill(draft);
  await page.goBack({ waitUntil: "commit" });
  await expect(page.getByRole("dialog")).toContainText("放弃尚未保存的修改");
  await page.keyboard.press("Escape");
  await expect(description).toHaveValue(draft);
  await expect(page).toHaveURL(/agent=agent-reviewer/);
  await page.locator('[data-agent-tab="resources"]').click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.locator("#agent-save, #agent-configuration-form"),
  ).toHaveCount(0);
  const version = page.locator(".agent-records dd").nth(1);
  const feedback = page.locator("#agent-resource-feedback");
  const outsideSecret = page.locator(
    '[data-secret="secret-outside-directory"]',
  );
  await expect(outsideSecret).toBeChecked();
  await page.locator('[data-agent-action="add-mount"]').click();
  await page.locator("#agent-action-workspace").fill("review-work");
  await page.locator('[data-agent-action="confirm-mount"]').click();
  await expect(page.locator("#agent-resource-editor")).toBeVisible();
  await expect(version).toHaveText("1");
  await page.locator('[data-select-for="agent-action-runner"]').click();
  await page.getByRole("option", { name: "Atlas Runner", exact: true }).click();
  await page.locator('[data-agent-action="confirm-mount"]').click();
  await expect(feedback).toHaveAttribute("data-status", "success");
  await expect(version).toHaveText("2");
  await expect(page.locator('[data-agent-section="runners"]')).toContainText(
    "review-work",
  );
  await page.locator('[data-agent-action="edit-mount:0"]').click();
  await page.locator('[data-agent-action="confirm-mount"]').click();
  await expect(feedback).toHaveText("资源目录未改变。");
  await expect(version).toHaveText("2");
  await page.locator('[data-secret="secret-research"]').click();
  await expect(feedback).toHaveAttribute("data-status", "success");
  await expect(version).toHaveText("3");
  await expect(outsideSecret).toBeChecked();
  await page.locator('[data-agent-action="skill-assign"]').click();
  await page.getByRole("option", { name: "Research", exact: true }).click();
  await page.locator("[data-agent-confirm]").click();
  await expect(feedback).toHaveAttribute("data-status", "success");
  await expect(version).toHaveText("4");
  await page.locator('[data-agent-tab="memory"]').click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(page.locator("#agent-page-title")).toHaveText("Reviewer");
  await page
    .locator('[data-agent-action="memory-delete:memory-reviewer-1"]')
    .click();
  await expect(page.getByRole("dialog")).toContainText(
    "评审结论区分已验证问题",
  );
  await page.locator("[data-memory-confirm]").click();
  await expect(page.locator(".agent-memory-record")).toHaveCount(1);
  await page.locator('[data-agent-tab="basic"]').click();
  await expect(page.locator('[data-agent-tab="basic"]')).toBeFocused();
  await expect(description).toHaveValue(draft);
  // Memory uses its own revision; successful deletion does not bump Agent version.
  await expect(version).toHaveText("4");
  await page.locator('[data-agent-action="cancel"]').click();
  await page.locator("[data-agent-confirm]").click();
  await expect(description).toHaveValue(agentExamples.reviewer.description);
  await page.locator('[data-agent-tab="resources"]').click();
  await expect(page.locator('[data-secret="secret-research"]')).toBeChecked();
  await expect(outsideSecret).toBeChecked();
  await expect(page.locator('[data-agent-section="runners"]')).toContainText(
    "review-work",
  );
  await expect(page.locator(".agent-skill-row")).toContainText("Research");
  await page.locator('[data-agent-tab="basic"]').click();
  await description.fill(draft);
  // Create the basic draft before another successful resource/Memory change.
  // Saving this older snapshot must retain those later independent results.
  await page.locator('[data-agent-tab="resources"]').click();
  await page.locator('[data-secret="secret-repository"]').click();
  await expect(feedback).toHaveAttribute("data-status", "success");
  await expect(version).toHaveText("5");
  await page.locator('[data-agent-tab="memory"]').click();
  await page
    .locator('[data-agent-action="memory-delete:memory-reviewer-2"]')
    .click();
  await page.locator("[data-memory-confirm]").click();
  await expect(page.locator(".agent-memory-record")).toHaveCount(0);
  await page.locator('[data-agent-tab="basic"]').click();
  await expect(description).toHaveValue(draft);
  await expect(version).toHaveText("5");
  await page.locator("#agent-save").click();
  await expect(page.locator("#agent-form-feedback")).toHaveText("配置已保存。");
  await expect(version).toHaveText("6");
  await page.locator('[data-agent-tab="resources"]').click();
  await expect(page.locator('[data-secret="secret-research"]')).toBeChecked();
  await expect(page.locator('[data-secret="secret-repository"]')).toBeChecked();
  await expect(outsideSecret).toBeChecked();
  await expect(page.locator('[data-agent-section="runners"]')).toContainText(
    "review-work",
  );
  await expect(page.locator(".agent-skill-row")).toContainText("Research");
  await page.locator('[data-agent-tab="memory"]').click();
  await expect(page.locator(".agent-memory-record")).toHaveCount(0);
  await expect(
    page.locator('[data-agent-action="memory-history:memory-reviewer-1"]'),
  ).toHaveCount(0);
  await page.locator('[data-agent-tab="basic"]').click();
  await description.fill("明确放弃的第二份草稿。");
  await page.locator('[data-agent-back=""]').click();
  await page.locator("[data-agent-leave]").click();
  await page
    .locator('.agent-directory-link[data-agent-open="agent-reviewer"]')
    .click();
  await expect(description).toHaveValue(draft);
  await page.locator('[data-agent-back=""]').click();
  await page
    .locator('.agent-directory-link[data-agent-open="agent-researcher"]')
    .click();
  await expect(description).toHaveValue(agentExamples.researcher.description);
  await page.locator('[data-agent-tab="memory"]').click();
  await expect(page.locator(".agent-memory-record")).toHaveCount(2);
  await expect(page.locator(".agent-memory-list")).toContainText(
    "研究资料保留原始来源",
  );
  expect(faults).toEqual({ errors: [], outbound: [] });
});

test("[agents-resource-recovery] errors Unknown and unverified versions preserve basic drafts", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  await openAgents(page, layouts[3], { agent: "agent-reviewer" });
  await page
    .locator("#agent-description")
    .fill("资源处理中仍保留的基本配置草稿。");
  await page.locator('[data-agent-tab="resources"]').click();
  // Hold only the synthetic resource timer. This makes the in-flight state
  // deterministic; it does not simulate or claim any real server behavior.
  await page.clock.install();
  await page.clock.pauseAt(new Date(Date.now() + 1000));
  await page.locator('[data-secret="secret-research"]').click();
  await expect(page.locator("#agent-resource-feedback")).toHaveAttribute(
    "data-status",
    "pending",
  );
  await page.locator('[data-agent-tab="basic"]').click();
  await expect(page.locator('[data-agent-action="cancel"]')).toBeDisabled();
  await expect(page.locator("#agent-save")).toBeDisabled();
  await expect(page.locator("#agent-description")).toHaveValue(
    "资源处理中仍保留的基本配置草稿。",
  );
  await page.clock.runFor(450);
  await page.clock.resume();
  await expect(page.locator('[data-agent-action="cancel"]')).toBeEnabled();
  await expect(page.locator("#agent-save")).toBeEnabled();
  await page.locator("#agent-save").click();
  await expect(page.locator("#agent-form-feedback")).toHaveText("配置已保存。");
  await page.locator('[data-agent-tab="resources"]').click();
  await expect(page.locator('[data-secret="secret-research"]')).toBeChecked();
  await expect(page.locator(".agent-records dd").nth(1)).toHaveText("3");
  for (const result of ["error", "unknown", "version_jump"]) {
    await openAgents(page, layouts[3], {
      agent: "agent-reviewer",
      resource_result: result,
      resource_extra_secret: "1",
    });
    const draft = `等待资源 ${result} 的基本配置草稿。`;
    await page.locator("#agent-description").fill(draft);
    await page.locator('[data-agent-tab="resources"]').click();
    const secret = page.locator('[data-secret="secret-research"]');
    const outsideSecret = page.locator(
      '[data-secret="secret-outside-directory"]',
    );
    const feedback = page.locator("#agent-resource-feedback");
    const version = page.locator(".agent-records dd").nth(1);
    await secret.click();
    await expect(feedback).toHaveAttribute(
      "data-status",
      result === "error" ? "error" : "unknown",
    );
    await expect(outsideSecret).toBeChecked();
    await expect(secret).not.toBeChecked();
    await expect(version).toHaveText("1");
    if (result === "version_jump") {
      // Validate the receipt before publishing its postimage. Neither canonical
      // resource values nor the basic baseline may accept an unverified version.
      await expect(feedback).toContainText("版本");
      await expect(
        page.locator('[data-agent-action="add-mount"]'),
      ).toBeDisabled();
    }
    if (result === "error") {
      await expect(feedback).toContainText("未保存");
      await page.locator('[data-agent-action="resource-retry"]').click();
      await expect(feedback).toHaveAttribute("data-status", "error");
      await expect(secret).not.toBeChecked();
      await expect(version).toHaveText("1");
    }
    if (result !== "error") {
      await expect(secret).toBeDisabled();
      await expect(
        page.locator('[data-agent-action="resource-retry"]'),
      ).toHaveCount(0);
      await page.locator('[data-agent-action="lookup"]').click();
      await expect(feedback).toContainText("尚未观察到");
      await expect(feedback).toHaveAttribute("data-status", "unknown");
      await expect(secret).toBeDisabled();
    }
    await page.locator('[data-agent-tab="basic"]').click();
    await expect(page.locator("#agent-description")).toHaveValue(draft);
    if (result !== "error")
      await expect(page.locator("#agent-save")).toBeDisabled();
    if (result !== "error") {
      await expect(page.locator('[data-agent-action="cancel"]')).toBeDisabled();
      await page.locator('[data-agent-back=""]').click();
      await page.locator("[data-agent-leave]").click();
      await page.locator('[data-agent-action="original"]').click();
      await expect(page).toHaveURL(/agent=agent-reviewer/);
      await expect(page).toHaveURL(/view=resources/);
      await expect(secret).not.toBeChecked();
      await expect(outsideSecret).toBeChecked();
      await page.locator('[data-agent-action="lookup"]').click();
      await expect(feedback).toHaveAttribute("data-status", "unknown");
      await expect(version).toHaveText("1");
    }
  }
  expect(faults).toEqual({ errors: [], outbound: [] });
});

test("[agents-final] create validates required fields and never reuses renamed identity", async ({
  page,
}, info) => {
  const faults = await setup(page, layouts[0]);
  await openAgents(page, layouts[0], { agent: "agent-architecture" });
  await page.locator("#agent-name").fill("architecture-old");
  await page.locator("#agent-save").click();
  await expect(page.locator("#agent-form-feedback")).toHaveText("配置已保存。");
  await page.locator('[data-agent-back=""]').click();
  await page.locator('[data-agent-action="create"]').click();
  await expect(page.getByRole("tab")).toHaveCount(2);
  await expect(page.locator('[data-agent-tab="memory"]')).toHaveCount(0);
  await page
    .locator("#agent-description")
    .fill("新对象，不继承原 Architecture 记忆。");
  await page.locator('[data-agent-tab="resources"]').click();
  await page.locator("#agent-save").click();
  await expect(page.locator('[data-agent-tab="basic"]')).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.locator("#agent-name")).toBeFocused();
  await expect(page.locator("#agent-name")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await page.locator("#agent-name").fill("architecture");
  await page.locator('[data-agent-tab="resources"]').click();
  await page.locator("#agent-save").click();
  await expect(page.locator('[data-agent-tab="basic"]')).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(page.locator("#agent-modelID-error")).toHaveText(
    "请选择主模型。",
  );
  const model = page.locator('[data-select-for="agent-modelID"]');
  await expect(model).toBeFocused();
  await expect(page).toHaveURL(/agent=new/);
  await model.click();
  await page.getByRole("option", { name: "Project Chat", exact: true }).click();
  await page.locator("#agent-save").click();
  await expect(page).not.toHaveURL(/agent=new/);
  const newID = new URL(page.url()).searchParams.get("agent");
  expect(newID).not.toBe("agent-architecture");
  expect(newID).toMatch(/^agent-.+/);
  await expect(page.locator("#agent-name")).toHaveValue("architecture");
  await page.locator('[data-agent-tab="memory"]').click();
  await expect(
    page.getByRole("heading", { name: "此 Agent 暂无当前记忆", exact: true }),
  ).toBeVisible();
  await page.locator('[data-agent-tab="basic"]').click();
  await page.locator('[data-agent-back=""]').click();
  await expect(page.locator(".agent-directory-row")).toHaveCount(4);
  await page
    .locator('.agent-directory-link[data-agent-open="agent-architecture"]')
    .click();
  await expect(page.locator("#agent-name")).toHaveValue("architecture-old");
  await page.locator('[data-agent-tab="memory"]').click();
  await expect(page.locator(".agent-memory-record")).toHaveCount(2);
  await expect(page.locator(".agent-memory-list")).toContainText(
    "项目的接口变更先核对调用方与提供方",
  );
  await info.attach("distinct-agent-identities", {
    body: JSON.stringify({ original: "agent-architecture", created: newID }),
    contentType: "application/json",
  });
  expect(faults).toEqual({ errors: [], outbound: [] });
});

test("[agents-final] non-active projects and readonly prevent all exposed writes", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  for (const state of ["archiving", "archived", "deleting", "readonly"]) {
    const scenario = state === "readonly" ? "readonly" : "normal";
    const extra = state === "readonly" ? {} : { project_lifecycle: state };
    await openAgents(page, layouts[3], extra, scenario);
    await expect(page.locator('[data-agent-action="create"]')).toBeDisabled();
    await page
      .locator('.agent-directory-link[data-agent-open="agent-reviewer"]')
      .click();
    await expect(page.locator("#agent-name")).toBeDisabled();
    await expect(page.locator("#agent-save")).toBeDisabled();
    await page.locator('[data-agent-tab="resources"]').click();
    const actions = page.locator('[data-agent-section="skills"] button');
    expect(await actions.count()).toBeGreaterThan(0);
    for (const action of await actions.all())
      await expect(action).toBeDisabled();
    await page.locator('[data-agent-tab="memory"]').click();
    for (const action of await page
      .locator('[data-agent-action^="memory-delete:"]')
      .all())
      await expect(action).toBeDisabled();
    await expect(page.locator(".agent-memory-record")).toHaveCount(2);
  }
  expect(faults).toEqual({ errors: [], outbound: [] });
});

test("[agents-final] bounded empty error conflict and configuration Unknown recovery", async ({
  page,
}) => {
  const faults = await setup(page, layouts[3]);
  await openAgents(page, layouts[3], {}, "empty");
  await expect(page.locator(".agent-directory-row")).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "项目还没有 Agent", exact: true }),
  ).toBeVisible();
  await openAgents(page, layouts[3], {}, "error");
  await expect(page.locator(".agent-directory-row")).toHaveCount(0);
  await page.locator('[data-agent-action="retry"]').click();
  await expect(page.locator(".agent-directory-row")).toHaveCount(3);
  await openAgents(page, layouts[3], { agent: "agent-reviewer" }, "conflict");
  await expect(page.locator("#agent-save")).toBeDisabled();
  await expect(page.locator(".agent-notice")).toContainText(
    "原草稿与编辑版本已保留",
  );
  await page.locator('[data-agent-action="conflict-read"]').click();
  await page.keyboard.press("Escape");
  await expect(page.locator("#agent-save")).toBeDisabled();
  await page.locator('[data-agent-action="conflict-read"]').click();
  await page.locator("[data-agent-confirm]").click();
  await expect(page.locator("#agent-name")).toBeEnabled();
  await expect(page.locator("#agent-form-feedback")).toHaveText(
    "已重新读取当前配置。",
  );
  for (const agent of ["agent-reviewer", "new"]) {
    await openAgents(page, layouts[3], { agent }, "unknown");
    const original = await page.locator("#agent-description").inputValue();
    await expect(page.locator("#agent-save")).toBeDisabled();
    await page.locator('[data-agent-action="lookup"]').click();
    await expect(page.locator(".agent-notice")).toContainText(
      "尚未观察到可确认结果",
    );
    await page.locator('[data-agent-back=""]').click();
    await page.locator("[data-agent-leave]").click();
    await expect(page.locator('[data-agent-action="create"]')).toBeDisabled();
    await page.locator('[data-agent-action="original"]').click();
    expect(new URL(page.url()).searchParams.get("agent")).toBe(agent);
    await expect(page.locator("#agent-description")).toHaveValue(original);
    await expect(page.locator("#agent-save")).toBeDisabled();
    await page.locator('[data-agent-action="lookup"]').click();
    await expect(page.locator(".agent-notice")).toContainText(
      "尚未观察到可确认结果",
    );
  }
  expect(faults).toEqual({ errors: [], outbound: [] });
});

for (const layout of [layouts[0], layouts[3]]) {
  test(`[agents-final] model menu and dirty dialog motion keyboard ${layout.width}`, async ({
    page,
  }, info) => {
    const faults = await setup(page, layout);
    await openAgents(page, layout, { agent: "agent-reviewer" });
    const basic = page.locator('[data-agent-tab="basic"]');
    const resources = page.locator('[data-agent-tab="resources"]');
    const memory = page.locator('[data-agent-tab="memory"]');
    const panel = page.getByRole("tabpanel");
    await expect(page.getByRole("tab")).toHaveCount(3);
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await expect(basic).toHaveAttribute("tabindex", "0");
    await expect(resources).toHaveAttribute("tabindex", "-1");
    await basic.focus();
    await page.keyboard.press("ArrowRight");
    await expect(resources).toBeFocused();
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");
    await expect(resources).toBeFocused();
    await expect(resources).toHaveAttribute("aria-selected", "true");
    await expect(panel).toHaveAttribute(
      "aria-labelledby",
      await resources.getAttribute("id"),
    );
    await page.keyboard.press("End");
    await expect(memory).toBeFocused();
    await expect(resources).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Space");
    await expect(memory).toBeFocused();
    await expect(memory).toHaveAttribute("aria-selected", "true");
    await expect(panel).toHaveAttribute(
      "aria-labelledby",
      await memory.getAttribute("id"),
    );
    await page.keyboard.press("Home");
    await expect(basic).toBeFocused();
    await expect(memory).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Enter");
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await expect(panel).toHaveAttribute(
      "aria-labelledby",
      await basic.getAttribute("id"),
    );
    await page.keyboard.press("ArrowLeft");
    await expect(memory).toBeFocused();
    await expect(basic).toHaveAttribute("aria-selected", "true");
    await page.keyboard.press("Home");
    for (const motion of ["no-preference", "reduce"]) {
      await page.emulateMedia({ reducedMotion: motion });
      const model = page.locator('[data-select-for="agent-modelID"]');
      await model.scrollIntoViewIfNeeded();
      await model.focus();
      await startMotionObservation(page, ".floating-menu");
      await page.keyboard.press("Enter");
      const menuSamples = await page.evaluate(() => window.__generalMotion);
      await page.keyboard.press("Escape");
      await expect(model).toBeFocused();
      await expect(page.locator(".floating-menu")).toHaveCount(0);
      await page.locator("#agent-description").fill("保留待确认的配置草稿。");
      const cancel = page.locator('[data-agent-action="cancel"]');
      await cancel.scrollIntoViewIfNeeded();
      await startMotionObservation(page, "#dialog[open]");
      await cancel.click();
      const dialogSamples = await page.evaluate(() => window.__generalMotion);
      for (const samples of [menuSamples, dialogSamples]) {
        if (motion === "reduce")
          expect(
            samples.every((s) => s.opacity === "1" && s.animations === 0),
          ).toBe(true);
        else
          expect(new Set(samples.map((s) => s.opacity)).size).toBeGreaterThan(
            1,
          );
      }
      await info.attach(`agent-motion-${motion}`, {
        body: JSON.stringify({ menuSamples, dialogSamples }),
        contentType: "application/json",
      });
      for (let i = 0; i < 4; i++) {
        await page.keyboard.press("Tab");
        expect(
          await page
            .getByRole("dialog")
            .evaluate((node) => node.contains(document.activeElement)),
        ).toBe(true);
      }
      await page.keyboard.press("Escape");
      await expect(cancel).toBeFocused();
      await expect(page.locator("#agent-description")).toHaveValue(
        "保留待确认的配置草稿。",
      );
      await noOverflow(page, `Agent motion ${motion}/${layout.width}`);
    }
    expect(faults).toEqual({ errors: [], outbound: [] });
  });
}

test("[agents-native-zoom] real 200% configuration menu records and Memory return", async ({}, info) => {
  mkdirSync(output, { recursive: true });
  const profile = mkdtempSync(path.join(output, "agent-native-profile-"));
  mkdirSync(path.join(profile, "Default"));
  writeFileSync(
    path.join(profile, "Default/Preferences"),
    JSON.stringify({
      partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
    }),
  );
  const origin = `http://127.0.0.1:${process.env.F0_PORT || 5188}`;
  const faults = { errors: [], outbound: [] };
  let context;
  try {
    context = await chromium.launchPersistentContext(profile, {
      executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      baseURL: origin,
      reducedMotion: "reduce",
    });
    await context.route("**/*", (route) => {
      const request = route.request();
      if (
        new URL(request.url()).origin === origin &&
        ["GET", "HEAD"].includes(request.method())
      )
        return route.continue();
      faults.outbound.push(`${request.method()} ${request.url()}`);
      return route.abort("blockedbyclient");
    });
    const page = context.pages()[0] || (await context.newPage());
    page.on("pageerror", (error) => faults.errors.push(error.message));
    await openAgents(page, { theme: "dark" });
    const actual = await page.evaluate(() => ({
      outerWidth,
      outerHeight,
      innerWidth,
      innerHeight,
      dpr: devicePixelRatio,
      visualViewport: {
        width: visualViewport.width,
        height: visualViewport.height,
        scale: visualViewport.scale,
      },
      rootZoom: getComputedStyle(document.documentElement).zoom,
    }));
    await info.attach("agent-native-actual-viewport", {
      body: JSON.stringify(actual),
      contentType: "application/json",
    });
    expect(actual.outerWidth).toBe(1440);
    expect(actual.outerHeight).toBe(960);
    expect(actual.innerWidth).toBe(720);
    expect(actual.dpr).toBe(2);
    expect(actual.visualViewport.width).toBe(720);
    expect(Math.floor(actual.visualViewport.height)).toBe(actual.innerHeight);
    expect(actual.visualViewport.scale).toBe(1);
    expect(actual.rootZoom).toBe("1");
    const row = page.locator(
      '.agent-directory-link[data-agent-open="agent-reviewer"]',
    );
    await row.scrollIntoViewIfNeeded();
    const listY = await page.evaluate(() => scrollY);
    await row.click();
    await page
      .locator("#agent-description")
      .fill("原生 200% 下编辑的 Reviewer 配置。");
    await page.locator("#agent-save").click();
    await expect(page.locator("#agent-form-feedback")).toHaveText(
      "配置已保存。",
    );
    const model = page.locator('[data-select-for="agent-modelID"]');
    await model.click();
    const menu = page.getByRole("listbox", { name: "主模型 *", exact: true });
    await expect(menu).toBeVisible();
    expect(
      await menu.evaluate((node) => {
        const r = node.getBoundingClientRect();
        return (
          r.top >= 0 &&
          r.left >= 0 &&
          r.bottom <= innerHeight + 1 &&
          r.right <= innerWidth
        );
      }),
    ).toBe(true);
    const cdp = await context.newCDPSession(page);
    async function nativeCapture(name) {
      const result = await cdp.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      mkdirSync(screens, { recursive: true });
      const file = path.join(screens, name + ".png");
      writeFileSync(file, Buffer.from(result.data, "base64"));
      await info.attach(name, { path: file, contentType: "image/png" });
    }
    await nativeCapture("agent-native-200pct-model-menu");
    await page.keyboard.press("Escape");
    await expect(model).toBeFocused();
    await page.locator(".agent-records summary").click();
    await page.mouse.move(680, actual.innerHeight - 40);
    await page.mouse.wheel(0, 1200);
    const last = page.locator(".agent-records dd").last();
    await expect(last).toBeInViewport();
    expect(
      await last.evaluate((node) => {
        const r = node.getBoundingClientRect();
        return node.contains(
          document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2),
        );
      }),
    ).toBe(true);
    await noOverflow(page, "Agent native 200% configuration records");
    await nativeCapture("agent-native-200pct-records");
    await page.locator('[data-agent-tab="resources"]').click();
    await expect(page.locator('[data-agent-tab="resources"]')).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await page
      .locator('[data-agent-action="skill-assign"]')
      .scrollIntoViewIfNeeded();
    await expect(
      page.locator('[data-agent-action="skill-assign"]'),
    ).toBeInViewport();
    await noOverflow(page, "Agent native 200% resources");
    await page.locator('[data-agent-tab="memory"]').click();
    await expect(page.locator(".agent-memory-list")).toContainText(
      "评审结论区分已验证问题",
    );
    await page
      .locator('[data-agent-action="memory-history:memory-reviewer-2"]')
      .click();
    await page.locator(".agent-memory-history summary").click();
    await page
      .locator(".agent-memory-history dd")
      .last()
      .scrollIntoViewIfNeeded();
    await expect(
      page.locator(".agent-memory-history dd").last(),
    ).toBeInViewport();
    await noOverflow(page, "Agent native 200% Memory");
    await nativeCapture("agent-native-200pct-memory");
    await page.locator('[data-agent-tab="basic"]').click();
    await expect(page.locator('[data-agent-tab="basic"]')).toBeFocused();
    await expect(page.locator("#agent-description")).toHaveValue(
      "原生 200% 下编辑的 Reviewer 配置。",
    );
    await page.locator('[data-agent-back=""]').click();
    await expect(row).toBeFocused();
    await expect.poll(() => page.evaluate(() => scrollY)).toBeCloseTo(listY, 0);
    await cdp.detach();
    expect(faults).toEqual({ errors: [], outbound: [] });
  } finally {
    await context?.close();
    rmSync(profile, { recursive: true, force: true });
    writeFileSync(
      path.join(output, "agent-native-lifecycle.json"),
      JSON.stringify({ contextClosed: true, profileRemoved: true }),
    );
  }
});

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
