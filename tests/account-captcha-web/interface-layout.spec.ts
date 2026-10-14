import { test, expect, type Page, type TestInfo } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import {
  installFixture,
  routes,
  projectPath,
  id,
} from "./interface-layout.fixtures";
const screenshots = fileURLToPath(
  new URL("../../output/ai/interface-layout/screenshots", import.meta.url),
);
async function observe(page: Page, testInfo: TestInfo, name: string) {
  await page.evaluate(() => document.fonts.ready);
  const metrics = await page.evaluate(() => ({
    viewport: { width: innerWidth, height: innerHeight },
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
    headings: [...document.querySelectorAll("h1")]
      .filter((el) => el.getClientRects().length)
      .map((el) => el.textContent?.trim()),
    main: [...document.querySelectorAll("main")].map((el) => ({
      width: el.clientWidth,
      scroll: el.scrollWidth,
    })),
  }));
  await testInfo.attach(`${name}-layout`, {
    body: JSON.stringify(metrics, null, 2),
    contentType: "application/json",
  });
  expect(metrics.document).toBeLessThanOrEqual(metrics.viewport.width + 1);
  expect(metrics.body).toBeLessThanOrEqual(metrics.viewport.width + 1);
  expect(metrics.headings).toHaveLength(1);
  for (const main of metrics.main)
    expect(main.scroll).toBeLessThanOrEqual(main.width + 1);
  mkdirSync(screenshots, { recursive: true });
  await page.screenshot({
    path: `${screenshots}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
}
function clean(state: Awaited<ReturnType<typeof installFixture>>) {
  expect(state.unexpected).toEqual([]);
  expect(state.errors).toEqual([]);
  expect(state.consoleErrors).toEqual([]);
}
for (const route of routes)
  test(`route ${route.key}`, async ({ page }, testInfo) => {
    const state = await installFixture(page, route);
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
    await page.goto(route.path);
    await expect(
      page.getByRole("heading", { level: 1, name: route.heading, exact: true }),
    ).toBeVisible();
    if (route.key === "projects")
      await expect(
        page.getByRole("link", { name: "Atlas", exact: true }),
      ).toBeVisible();
    if (route.key === "profile")
      await expect(
        page.getByRole("textbox", { name: "显示名", exact: true }),
      ).toHaveValue(state.user.display_name);
    if (route.key === "knowledge-document")
      await expect(
        page.locator('pre[aria-label="当前文档正文"]'),
      ).toBeVisible();
    await expect(page.locator('.ui-state[aria-busy="true"]')).toHaveCount(0);
    clean(state);
    await expect(page.locator('.ui-state[role="alert"]')).toHaveCount(0);
    await observe(page, testInfo, `route-${route.key}-1440-light`);
    clean(state);
  });
test("list template widths themes and keyboard modal", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.goto("/projects");
  await expect(
    page.getByRole("link", { name: "Atlas", exact: true }),
  ).toBeVisible();
  for (const [width, colorScheme] of [
    [834, "light"],
    [1024, "dark"],
    [1920, "light"],
    [390, "dark"],
  ] as const) {
    await page.setViewportSize({ width, height: 960 });
    await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute(
      "data-theme",
      colorScheme,
    );
    if (width === 1920)
      expect((await page.locator(".project-list").boundingBox())!.width).toBe(
        1120,
      );
    await observe(page, testInfo, `list-${width}-${colorScheme}`);
  }
  const trigger = page.getByRole("button", { name: "新建项目", exact: true });
  await trigger.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("textbox", { name: "项目名称", exact: true }),
  ).toBeFocused();
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press("Tab");
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement)),
    ).toBe(true);
  }
  await observe(page, testInfo, "list-390-dark-dialog");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  expect(state.requests.filter((request) => request.method !== "GET")).toEqual(
    [],
  );
  clean(state);
});
test("knowledge reader long content and narrow directory", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${projectPath}/knowledge/${id(21)}`);
  await expect(page.locator('pre[aria-label="当前文档正文"]')).toBeVisible();
  await observe(page, testInfo, "knowledge-1440-light-reader");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await observe(page, testInfo, "knowledge-390-dark-reader");
  clean(state);
});

test("artifact reader split and narrow return focus", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 1024, height: 960 });
  await page.goto(`${projectPath}/artifacts`);
  const trigger = page.getByRole("button", {
    name: "查看产物 布局评审记录.txt",
    exact: true,
  });
  await trigger.click();
  await expect(page.getByLabel("产物文本内容", { exact: true })).toBeVisible();
  await observe(page, testInfo, "artifacts-1024-light-detail");
  await page.setViewportSize({ width: 834, height: 960 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(trigger).toBeHidden();
  await observe(page, testInfo, "artifacts-834-dark-detail");
  await page.setViewportSize({ width: 390, height: 844 });
  await observe(page, testInfo, "artifacts-390-dark-detail");
  await page.getByRole("button", { name: "返回产物列表", exact: true }).click();
  await expect(trigger).toBeFocused();
  await observe(page, testInfo, "artifacts-390-dark-list");
  clean(state);
});
test("task workbench narrow scope and filter controls", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${projectPath}/tasks/sprints/${id(61)}`);
  await expect(
    page.getByRole("heading", { name: "当前迭代 / 布局与交互", exact: true }),
  ).toBeVisible();
  await expect(page.locator('.ui-state[aria-busy="true"]')).toHaveCount(0);
  clean(state);
  await expect(page.locator('.ui-state[role="alert"]')).toHaveCount(0);
  await observe(page, testInfo, "tasks-1440-light-board");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await observe(page, testInfo, "tasks-390-dark-board");
  const trigger = page.getByRole("button", {
    name: "选择 Sprint",
    exact: true,
  });
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "选择 Sprint", exact: true });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Tab");
  expect(
    await dialog.evaluate((el) => el.contains(document.activeElement)),
  ).toBe(true);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await page.getByText("筛选条件", { exact: true }).click();
  await expect(
    page.getByRole("button", { name: "筛选状态", exact: true }),
  ).toBeVisible();
  clean(state);
});

test("list loading empty error retry and one creation", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  state.mode = "hold";
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/projects");
  await expect(
    page.getByRole("heading", { name: "正在读取项目列表", exact: true }),
  ).toBeVisible();
  await observe(page, testInfo, "list-loading");
  state.mode = "empty";
  state.release();
  await expect(
    page.getByText("从第一个项目开始", { exact: true }),
  ).toBeVisible();
  await observe(page, testInfo, "list-empty");
  state.mode = "error";
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "项目列表读取失败", exact: true }),
  ).toBeVisible();
  await observe(page, testInfo, "list-error");
  state.mode = "ready";
  await page.getByRole("button", { name: /重读当前页|重试/ }).click();
  await expect(
    page.getByRole("link", { name: "Atlas", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "新建项目", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
  await dialog
    .getByRole("textbox", { name: "项目名称", exact: true })
    .fill("Browser.Project");
  await dialog
    .getByRole("button", { name: "确认创建项目", exact: true })
    .click();
  await expect(
    dialog.getByRole("link", { name: "进入项目", exact: true }),
  ).toHaveAttribute("href", "/owner/browser.project");
  expect(
    state.requests.filter((request) => request.method === "POST"),
  ).toHaveLength(1);
  clean(state);
});
test("task creation footer submits once after guarded escape", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${projectPath}/tasks/sprints/${id(61)}`);
  await page.getByRole("button", { name: "新建任务", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新建任务", exact: true });
  const title = dialog.getByRole("textbox", { name: "任务标题", exact: true });
  await expect(title).toBeFocused();
  await title.fill("浏览器确认创建任务");
  await dialog.getByRole("button", { name: "任务类型", exact: true }).click();
  await page.getByRole("option", { name: "任务", exact: true }).click();
  await dialog.getByRole("button", { name: "优先级", exact: true }).click();
  await page.getByRole("option", { name: "中", exact: true }).click();
  await page.keyboard.press("Escape");
  const guard = page.getByRole("dialog", { name: "离开任务", exact: true });
  await expect(guard).toBeVisible();
  await guard.getByRole("button", { name: "继续查看", exact: true }).click();
  await expect(title).toHaveValue("浏览器确认创建任务");
  await expect
    .poll(() => dialog.evaluate((el) => el.contains(document.activeElement)))
    .toBe(true);
  await observe(page, testInfo, "tasks-390-create-dialog");
  await dialog
    .getByRole("button", { name: "确认创建任务", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "浏览器确认创建任务", exact: true }),
  ).toBeVisible();
  expect(
    state.requests.filter(
      (request) => request.method === "POST" && request.path.endsWith("/tasks"),
    ),
  ).toHaveLength(1);
  clean(state);
});
for (const kind of ["Milestone", "Sprint"])
  test(`${kind} creation footer submits once`, async ({ page }) => {
    const state = await installFixture(page);
    await page.goto(`${projectPath}/tasks/sprints/${id(61)}`);
    await page.locator(".planning-controls > summary").click();
    await page
      .getByRole("button", { name: `新建 ${kind}`, exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: `新建 ${kind}`,
      exact: true,
    });
    const title = dialog.getByRole("textbox", {
      name: `${kind} 标题`,
      exact: true,
    });
    await expect(title).toBeFocused();
    await title.fill(`浏览器创建 ${kind}`);
    await dialog
      .getByRole("button", { name: `确认创建 ${kind}`, exact: true })
      .click();
    await expect(dialog).toHaveCount(0);
    await expect(
      page.getByRole("heading", { name: `浏览器创建 ${kind}`, exact: true }),
    ).toBeVisible();
    expect(
      state.requests.filter(
        (request) =>
          request.method === "POST" &&
          request.path.endsWith(
            kind === "Milestone" ? "/milestones" : "/sprints",
          ),
      ),
    ).toHaveLength(1);
    clean(state);
  });

test("settings width narrow navigation and guarded variable dialog", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto(`${projectPath}/settings/variables`);
  await expect(page.getByText("ENVIRONMENT", { exact: true })).toBeVisible();
  expect(
    (await page.locator(".settings-shell").boundingBox())!.width,
  ).toBeLessThanOrEqual(1200);
  expect((await page.locator(".settings-sidebar").boundingBox())!.width).toBe(
    216,
  );
  await observe(page, testInfo, "settings-1920-light");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await observe(page, testInfo, "settings-390-dark");
  const menu = page.getByRole("button", { name: "项目设置栏目", exact: true });
  await menu.click();
  const navigation = page.getByRole("dialog", {
    name: "项目设置栏目",
    exact: true,
  });
  await expect(navigation).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(menu).toBeFocused();
  const create = page.getByRole("button", { name: "新建变量", exact: true });
  await create.click();
  const dialog = page.getByRole("dialog", {
    name: "新建普通变量",
    exact: true,
  });
  const name = dialog.getByRole("textbox", { name: "变量名称", exact: true });
  await expect(name).toBeFocused();
  await name.fill("BROWSER_DRAFT");
  await dialog
    .getByRole("textbox", { name: "变量值", exact: true })
    .fill("long value ".repeat(100));
  await observe(page, testInfo, "settings-390-dark-variable-dialog");
  await page.keyboard.press("Escape");
  const guard = page.getByRole("dialog", {
    name: "放弃变量输入？",
    exact: true,
  });
  await expect(guard).toBeVisible();
  await guard.getByRole("button", { name: "取消", exact: true }).click();
  await expect(name).toHaveValue("BROWSER_DRAFT");
  await expect
    .poll(() => dialog.evaluate((el) => el.contains(document.activeElement)))
    .toBe(true);
  await page.keyboard.press("Escape");
  await guard.getByRole("button", { name: "放弃输入", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeFocused();
  expect(state.requests.filter((request) => request.method !== "GET")).toEqual(
    [],
  );
  clean(state);
});
test("knowledge narrow directory and reader scroll", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${projectPath}/knowledge/${id(21)}`);
  await expect(page.getByLabel("当前文档正文", { exact: true })).toBeVisible();
  const tree = page.getByRole("button", { name: "文档树", exact: true });
  await tree.click();
  const drawer = page.getByRole("dialog", { name: "文档树", exact: true });
  await expect(drawer).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(tree).toBeFocused();
  await tree.click();
  await drawer.locator(`[data-tree-id="${id(20)}"]`).click();
  await expect(drawer).toHaveCount(0);
  await expect(
    page.getByRole("heading", {
      level: 2,
      name: "统一界面设计与实施记录",
      exact: true,
    }),
  ).toBeVisible();
  await expect(tree).toBeFocused();
  await observe(page, testInfo, "knowledge-390-directory-selected");
  clean(state);
});
test("200 percent reflow equivalent keeps collection and form usable", async ({
  browser,
}, testInfo) => {
  // 1440x960 at 200% yields 720x480 CSS pixels. This tests reflow, not OS chrome or mobile keyboards.
  const context = await browser.newContext({
    viewport: { width: 720, height: 480 },
    deviceScaleFactor: 2,
    baseURL: "http://127.0.0.1:5177",
    colorScheme: "light",
    reducedMotion: "reduce",
  });
  const page = await context.newPage();
  const state = await installFixture(page);
  try {
    await page.goto("/projects");
    await expect(
      page.getByRole("link", { name: "Atlas", exact: true }),
    ).toBeVisible();
    await observe(page, testInfo, "reflow-200-projects");
    await page.getByRole("button", { name: "新建项目", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
    await expect(
      dialog.getByRole("textbox", { name: "项目名称", exact: true }),
    ).toBeFocused();
    await expect(
      dialog.getByRole("button", { name: "确认创建项目", exact: true }),
    ).toBeInViewport();
    await observe(page, testInfo, "reflow-200-dialog");
    clean(state);
  } finally {
    await context.close();
  }
});

test("agent complex creation guard preserves input and focus", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${projectPath}/settings/agents`);
  const trigger = page.getByRole("button", {
    name: "创建 Agent",
    exact: true,
  });
  await trigger.click();
  const dialog = page.getByRole("dialog", {
    name: "创建空白 Agent",
    exact: true,
  });
  const name = dialog.getByRole("textbox", { name: "Agent 名称", exact: true });
  await expect(name).toBeFocused();
  await name.fill("Browser-Draft");
  await observe(page, testInfo, "agent-390-create-dialog");
  await page.keyboard.press("Escape");
  const guard = page.getByRole("dialog", {
    name: "离开 Agent 配置？",
    exact: true,
  });
  await expect(guard).toBeVisible();
  await guard.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(name).toHaveValue("Browser-Draft");
  await expect(name).toBeEnabled();
  await expect
    .poll(() => dialog.evaluate((el) => el.contains(document.activeElement)))
    .toBe(true);
  await page.keyboard.press("Escape");
  await guard.getByRole("button", { name: "确认离开", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  clean(state);
});
test("secret unknown lookup remains reachable within modal", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${projectPath}/settings/secrets`);
  await page.getByRole("button", { name: "创建 Secret", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "创建 Secret", exact: true });
  await dialog
    .getByRole("textbox", { name: "Secret 名称", exact: true })
    .fill("BROWSER_SYNTHETIC");
  const material = dialog.getByRole("textbox", {
    name: "Secret 值",
    exact: true,
  });
  await material.fill("synthetic-interface-test-material");
  await dialog
    .getByRole("button", { name: "创建 Secret", exact: true })
    .click();
  const lookup = dialog.getByRole("button", {
    name: "检查原请求",
    exact: true,
  });
  await expect(lookup).toBeEnabled();
  await expect(material).toHaveValue("");
  await observe(page, testInfo, "secret-390-unknown");
  await lookup.click();
  await expect
    .poll(
      () =>
        state.requests.filter((request) =>
          request.path.endsWith("/secret-variables/commands/lookup"),
        ).length,
    )
    .toBe(1);
  await expect(lookup).toBeEnabled();
  expect(
    state.requests.filter(
      (request) =>
        request.method === "POST" && request.path.endsWith("/secret-variables"),
    ),
  ).toHaveLength(1);
  clean(state);
});
test("provider editor and account entry retain narrow form structure", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/system/providers");
  await page
    .getByRole("button", { name: "创建 Provider", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "创建 Provider",
    exact: true,
  });
  await expect(
    dialog.getByRole("textbox", { name: "名称", exact: true }),
  ).toBeFocused();
  await observe(page, testInfo, "provider-390-dark-dialog");
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  clean(state);
});
test("valid invitation and reset account forms at narrow width", async ({
  page,
}, testInfo) => {
  const state = await installFixture(page, { anonymous: true });
  await page.setViewportSize({ width: 390, height: 844 });
  const token = `${id(600)}.${"T".repeat(43)}`;
  await page.goto(`/invite#${token}`);
  await expect(
    page.getByRole("textbox", { name: "用户名", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/invite$/);
  await observe(page, testInfo, "account-invite-390-light");
  await page.goto(`/reset-password#${token}`);
  await expect(page.locator("#reset-password")).toBeVisible();
  await observe(page, testInfo, "account-reset-390-light");
  clean(state);
});

test("knowledge normal-content preview", async ({ page }, testInfo) => {
  const knowledgeText =
    "界面设计与实施记录\n\n设计目标\n让团队成员清楚知道当前范围、正在处理的对象和下一步动作。页面沿用共同组件与主题，减少不必要的视觉装饰。\n\n页面结构\n集合列表保持稳定的名称起点与状态列；设置页面使用固定菜单和受控内容宽度；工作台分别管理导航、目录与正文的滚动。\n\n交互原则\n创建任务在独立弹窗中完成。关闭、切换和结果恢复遵循原业务规则，键盘用户可以回到原来的位置继续工作。";
  const state = await installFixture(page, { knowledgeText });
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${projectPath}/knowledge/${id(20)}`);
  await expect(page.getByLabel("当前文档正文", { exact: true })).toHaveText(
    knowledgeText,
  );
  await observe(page, testInfo, "preview-knowledge-1440-light");
  clean(state);
});
