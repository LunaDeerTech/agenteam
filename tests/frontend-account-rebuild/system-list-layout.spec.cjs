const { test, expect } = require("@playwright/test");
const { controlled } = require("./controlled-system.cjs");

test.use({
  launchOptions: {
    executablePath: "/usr/bin/chromium",
    args: ["--no-sandbox"],
  },
  serviceWorkers: "block",
});

async function aligned(page, populated) {
  const geometry = await page.evaluate((filled) => {
    const main = document.querySelector(".settings-main");
    const body = document.querySelector(".settings-page-body");
    const list = document.querySelector(".system-list");
    const status = list.querySelector('[role="status"]');
    const selector = list.querySelector('[role="combobox"]');
    const content = list.querySelector(filled ? "ul" : ".system-list-empty");
    const footer = list.querySelector("nav");
    const bounds = (node) => {
      const { left, right, top, bottom } = node.getBoundingClientRect();
      return { left, right, top, bottom };
    };
    const css = getComputedStyle(main);
    const edge =
      main.getBoundingClientRect().left + parseFloat(css.borderLeftWidth);
    return {
      effective: {
        left: edge + parseFloat(css.paddingLeft),
        right: edge + main.clientWidth - parseFloat(css.paddingRight),
      },
      body: bounds(body),
      list: bounds(list),
      header: bounds(status.parentElement),
      status: bounds(status),
      selector: bounds(selector),
      content: bounds(content),
      footer: bounds(footer),
      documentWidth: document.documentElement.scrollWidth,
      viewport: innerWidth,
      mainWidth: main.clientWidth,
      mainScrollWidth: main.scrollWidth,
    };
  }, populated);
  // Compare the actual container edges, not the natural width of its text.
  // Client width excludes the main scroller's scrollbar; padding is preserved.
  for (const name of ["body", "list", "header", "content", "footer"]) {
    expect(
      Math.abs(geometry[name].left - geometry.effective.left),
      `${name} left edge`,
    ).toBeLessThanOrEqual(1.5);
    expect(
      Math.abs(geometry[name].right - geometry.effective.right),
      `${name} right edge`,
    ).toBeLessThanOrEqual(1.5);
  }
  expect(
    Math.abs(geometry.status.left - geometry.effective.left),
  ).toBeLessThanOrEqual(1.5);
  expect(
    Math.abs(geometry.selector.right - geometry.effective.right),
  ).toBeLessThanOrEqual(1.5);
  expect(geometry.content.top).toBeGreaterThanOrEqual(
    geometry.header.bottom - 1,
  );
  expect(geometry.footer.top).toBeGreaterThanOrEqual(
    geometry.content.bottom - 1,
  );
  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewport);
  expect(geometry.mainScrollWidth).toBeLessThanOrEqual(geometry.mainWidth + 1);
}

for (const [width, colorScheme] of [
  [1440, "light"],
  [390, "dark"],
]) {
  test(`three lists align status, rows and pagination to Settings content at ${width}px`, async ({
    context,
  }) => {
    for (const [path, label, refresh, emptyText] of [
      ["/system/users", "用户目录", "刷新", "暂无已注册用户。"],
      [
        "/system/invitations",
        "待注册邀请",
        "刷新",
        "暂无待注册邀请。已接受、撤销或过期的邀请不会列在这里。",
      ],
      ["/system/smtp", "投递任务", "刷新任务", "暂无投递任务。"],
    ]) {
      const page = await context.newPage();
      try {
        await page.setViewportSize({ width, height: 960 });
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        const state = await controlled(page, { path });
        await expect(
          page
            .getByRole("list", { name: label, exact: true })
            .locator(":scope > li"),
        ).toHaveCount(10);
        await expect(
          page.getByRole("combobox", { name: "每页条数", exact: true }),
        ).toContainText("10 条");
        await aligned(page, true);
        state.empty = true;
        await page.getByRole("button", { name: refresh, exact: true }).click();
        await expect(
          page.getByRole("status").filter({ hasText: "第 1 页" }),
        ).toContainText("0 条");
        await expect(page.getByText(emptyText, { exact: true })).toBeVisible();
        await expect(
          page.getByRole("button", { name: "上一页", exact: true }),
        ).toBeDisabled();
        await expect(
          page.getByRole("button", { name: "下一页", exact: true }),
        ).toBeDisabled();
        await aligned(page, false);
        const reads = path === "/system/smtp" ? state.lists : state.directories;
        expect(reads).toHaveLength(2);
        expect(
          reads.every((value) => value.limit === 10 && value.cursor === null),
        ).toBe(true);
        expect(state.tests.length + state.retries.length).toBe(0);
        expect(state.unexpected).toEqual([]);
      } finally {
        await page.close();
      }
    }
  });
}
