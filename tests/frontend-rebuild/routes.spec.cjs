const { test, expect } = require("@playwright/test");

// These expectations follow the agreed blueprint, independently of router metadata.
const settings = [
  {
    scope: "system",
    title: "系统设置",
    prefix: "/system",
    pages: [
      ["users", "用户"],
      ["invitations", "待注册邀请"],
      ["providers", "Providers"],
      ["models", "模型"],
      ["model-selection", "平台模型用途"],
      ["runners", "Runner"],
      ["account-security", "账号安全"],
      ["outbound-policy", "出站规则"],
      ["audit", "系统审计"],
      ["smtp", "SMTP"],
      ["runtime-information", "运行信息"],
    ],
  },
  {
    scope: "personal",
    title: "个人设置",
    prefix: "/settings",
    pages: [
      ["profile", "个人资料"],
      ["appearance", "外观"],
      ["password", "修改密码"],
    ],
  },
  {
    scope: "project",
    title: "项目设置",
    prefix: "/owner/project/settings",
    pages: [
      ["general", "基本信息"],
      ["agents", "Agent"],
      ["variables", "普通变量"],
      ["secrets", "Secrets"],
      ["skills", "Skills"],
      ["mcp", "MCP"],
      ["scheduler", "Scheduler"],
      ["audit", "项目审计"],
      ["model-providers", "Providers"],
      ["available-models", "可用模型"],
      ["model-usage", "调用记录"],
    ],
  },
];

const accountPreview = new Map([
  ["登录", "这是界面预览。安全账号入口就绪后可登录。"],
  ["接受邀请", "这是界面预览。安全账号入口就绪后可使用链接。"],
  ["找回密码", "这是界面预览。安全账号入口就绪后可提交请求。"],
  ["重置密码", "这是界面预览。安全账号入口就绪后可使用链接。"],
  ["个人资料", "当前为界面预览。账号服务连接后可编辑个人资料。"],
  ["外观", "当前为界面预览。账号服务连接后可保存外观偏好。"],
  ["修改密码", "当前为界面预览。账号服务连接后可修改密码。"],
]);

const systemPreview = new Map([
  ["用户", "当前为界面预览。连接账号服务后可查看用户。"],
  ["待注册邀请", "当前为界面预览。连接账号服务后可管理邀请。"],
  ["账号安全", "当前为界面预览，连接账号服务后可读取和修改设置。"],
  ["SMTP", "当前为界面预览，连接账号服务后可管理 SMTP。"],
]);

async function agreedPage(page, title) {
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(title);
  await expect(page).toHaveTitle(`${title} · agenteam`);
  if (systemPreview.has(title)) {
    await expect(
      page.getByText(systemPreview.get(title), { exact: true }),
    ).toBeVisible();
    await expect(page.locator("[data-page-content]")).toHaveCount(0);
    await expect(
      page.locator(
        'input:not(:disabled), button[type="submit"]:not(:disabled)',
      ),
    ).toHaveCount(0);
    if (title === "用户" || title === "待注册邀请")
      await expect(
        page.getByRole("button", { name: "刷新", exact: true }),
      ).toBeDisabled();
    if (title === "待注册邀请")
      await expect(
        page.getByRole("button", { name: "邀请用户", exact: true }),
      ).toBeDisabled();
    return;
  }
  if (accountPreview.has(title)) {
    await expect(
      page.getByText(accountPreview.get(title), { exact: true }),
    ).toBeVisible();
    await expect(page.locator("[data-page-content]")).toHaveCount(0);
    // Public link pages have no form before inspection; every other account
    // preview retains its real form with submission disabled.
    if (title !== "接受邀请" && title !== "重置密码")
      await expect(page.locator('button[type="submit"]')).toBeDisabled();
    return;
  }
  await expect(page.locator("[data-page-content]")).toHaveCount(1);
  await expect(page.locator("[data-page-content]")).toBeEmpty();
}

for (const group of settings) {
  test(`${group.scope} settings leaves use one settings shell and agreed content`, async ({
    page,
  }) => {
    const apiRequests = [];
    page.on("request", (request) => {
      if (["xhr", "fetch"].includes(request.resourceType()))
        apiRequests.push(request.url());
    });
    for (const [key, title] of group.pages) {
      await test.step(`${group.prefix}/${key}`, async () => {
        await page.goto(`${group.prefix}/${key}`);
        await agreedPage(page, title);
        await expect(page.locator("[data-settings-scope]")).toHaveCount(1);
        await expect(page.locator("[data-settings-scope]")).toHaveAttribute(
          "data-settings-scope",
          group.scope,
        );
        const navigation = page.getByRole("navigation", {
          name: group.title,
          exact: true,
        });
        await expect(navigation.getByRole("link")).toHaveCount(
          group.pages.length,
        );
        await expect(
          navigation.getByRole("link", { name: title, exact: true }),
        ).toHaveAttribute("aria-current", "page");
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        ).toBe(true);
      });
    }
    expect(apiRequests).toEqual([]);
  });
}

test("remaining leaf routes and optional detail URLs retain agreed content and their scope", async ({
  page,
}) => {
  const pages = [
    ["/", "首页"],
    ["/projects", "项目"],
    ["/login", "登录", "account"],
    ["/invite", "接受邀请", "account"],
    ["/forgot-password", "找回密码", "account"],
    ["/reset-password", "重置密码", "account"],
    ["/mcp/oauth/callback", "MCP 授权", "account"],
    ["/owner/project", "概览", "project"],
    ["/owner/project/tasks", "任务", "project"],
    ["/owner/project/tasks/sprints/sprint-id", "任务", "project"],
    ["/owner/project/tasks/task-id", "任务", "project"],
    ["/owner/project/knowledge", "知识库", "project"],
    ["/owner/project/artifacts", "产物", "project"],
    ["/owner/project/meetings", "会议", "project"],
    ["/owner/project/inbox", "待办", "project"],
    ["/owner/project/knowledge/document-id", "知识库", "project"],
    ["/owner/project/meetings/meeting-id", "会议", "project"],
  ];
  for (const [url, title, scope] of pages) {
    await test.step(url, async () => {
      await page.goto(url);
      await agreedPage(page, title);
      await expect(page.locator("[data-settings-scope]")).toHaveCount(0);
      await expect(
        page.getByRole("navigation", { name: "系统导航", exact: true }),
      ).toHaveCount(scope === "account" ? 0 : 1);
      await expect(
        page.getByRole("navigation", { name: "项目导航", exact: true }),
      ).toHaveCount(scope === "project" ? 1 : 0);
    });
  }
  await page.goto("/owner/project/settings/skills/skill-id");
  await agreedPage(page, "Skills");
  await expect(page.locator("[data-settings-scope]")).toHaveAttribute(
    "data-settings-scope",
    "project",
  );
});

test("scope defaults and reserved unknown paths cannot turn into project pages", async ({
  page,
}) => {
  for (const [url, target] of [
    ["/system", "/system/users"],
    ["/settings", "/settings/profile"],
    ["/owner/project/settings", "/owner/project/settings/general"],
  ]) {
    await page.goto(url);
    await expect(page).toHaveURL(new RegExp(`${target}$`));
  }
  for (const url of [
    "/system/unknown",
    "/settings/unknown",
    "/mcp/unknown",
    "/unknown/deep/path",
  ]) {
    await page.goto(url);
    await agreedPage(page, "页面不存在");
    await expect(
      page.getByRole("navigation", { name: "项目导航", exact: true }),
    ).toHaveCount(0);
  }
});

test("narrow system and settings directories close on navigation and restore focus on cancellation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/system/users");
  const opener = page.getByRole("button", {
    name: "打开系统设置目录",
    exact: true,
  });
  await opener.click();
  const directory = page.getByRole("dialog", { name: "系统设置", exact: true });
  await expect(directory).toBeVisible();
  expect(
    await directory.evaluate(
      (element) => getComputedStyle(element).animationName,
    ),
  ).toBe("ui-sheet-enter");
  await directory.getByRole("link", { name: "SMTP", exact: true }).click();
  await agreedPage(page, "SMTP");
  await expect(directory).not.toBeVisible();
  await expect(page.getByRole("heading", { level: 1 })).toBeFocused();
  await opener.click();
  await page.keyboard.press("Escape");
  await expect(opener).toBeFocused();
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await opener.click();
  expect(
    await directory.evaluate(
      (element) => getComputedStyle(element).animationName,
    ),
  ).toBe("none");
  await page.keyboard.press("Escape");
  const systemOpener = page.getByRole("button", {
    name: "打开系统导航",
    exact: true,
  });
  await systemOpener.click();
  const system = page.getByRole("dialog", { name: "导航", exact: true });
  await system.getByRole("link", { name: "个人设置", exact: true }).click();
  await agreedPage(page, "个人资料");
  await expect(system).not.toBeVisible();
  await page
    .getByRole("button", { name: "打开个人设置目录", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "个人设置", exact: true })
    .getByRole("link", { name: "修改密码" })
    .click();
  await agreedPage(page, "修改密码");
  await page.goBack();
  await agreedPage(page, "个人资料");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("narrow project navigation scrolls locally and keeps the route identity", async ({
  page,
}) => {
  // At 390px all seven labels fit; 320px exercises actual local overflow.
  await page.setViewportSize({ width: 320, height: 680 });
  await page.goto("/long-owner-name/long-project-name/settings/general");
  const navigation = page.getByRole("navigation", {
    name: "项目导航",
    exact: true,
  });
  await expect(
    navigation.getByRole("link", { name: "项目设置", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await page
    .getByRole("button", { name: "打开项目设置目录", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "项目设置", exact: true })
    .getByRole("link", { name: "MCP", exact: true })
    .click();
  await agreedPage(page, "MCP");
  await expect(page).toHaveURL(
    /\/long-owner-name\/long-project-name\/settings\/mcp$/,
  );
  await navigation.getByRole("link", { name: "会议", exact: true }).click();
  await agreedPage(page, "会议");
  await expect(page).toHaveURL(
    /\/long-owner-name\/long-project-name\/meetings$/,
  );
  expect(
    await navigation.evaluate(
      (element) => element.scrollWidth > element.clientWidth,
    ),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
