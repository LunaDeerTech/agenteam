import { test, expect, type Page, type Route } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

// These synthetic API responses prove UI behavior only, not backend integration.
const evidence = fileURLToPath(
  new URL(
    "../../output/ai/project-list-polish-ui/screenshots",
    import.meta.url,
  ),
);
const id = (n: number) =>
  `01990000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
const at = "2026-10-14T00:00:00.000001Z";
const projectName = "Project_" + "A".repeat(56);
const initialItems = [
  {
    id: id(10),
    name: "Atlas",
    description: "围绕真实需求，构建更顺畅的协作体验。",
    lifecycle: "active",
    version: "7",
  },
  {
    id: id(11),
    name: "Design.System",
    description: "组件、交互与视觉规范，共享一致的工作语言。",
    lifecycle: "active",
    version: "2",
  },
  {
    id: id(12),
    name: "Release-Archive",
    description: "已交付的版本记录。",
    lifecycle: "archived",
    version: "4",
  },
  {
    id: id(13),
    name: projectName,
    description: "LongDescription".repeat(24),
    lifecycle: "archiving",
    version: "3",
    operation_id: id(23),
  },
  {
    id: id(14),
    name: "Retired.Project",
    lifecycle: "deleting",
    version: "5",
    operation_id: id(24),
  },
];
type Item = (typeof initialItems)[number];
async function fixture(page: Page) {
  const state = {
    items: [...initialItems] as Item[],
    mode: "ready" as "ready" | "empty" | "error" | "hold",
    release: () => {},
    requests: [] as { method: string; path: string }[],
    writes: [] as Record<string, unknown>[],
    faults: [] as string[],
  };
  page.on("pageerror", (error) => state.faults.push(error.message));
  const json = (route: Route, body: unknown, status = 200) =>
    route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname;
    state.requests.push({ method: request.method(), path });
    if (path === "/api/v1/session" && request.method() === "GET") {
      return json(route, {
        user: {
          id: id(1),
          email: "owner@example.test",
          username: "owner",
          display_name: "林 / Workspace",
          role: "admin",
          theme: "system",
          version: "1",
          initial_password_suggestion: false,
        },
        session: {
          id: id(2),
          issued_at: at,
          absolute_expires_at: "2027-10-14T00:00:00.000001Z",
          idle_expires_at: "2027-10-14T00:00:00.000001Z",
        },
        csrf_token: "S".repeat(43),
      });
    }
    if (path === "/api/v1/projects" && request.method() === "GET") {
      if (state.mode === "hold")
        await new Promise<void>((resolve) => {
          state.release = resolve;
        });
      if (state.mode === "error") return route.abort("failed");
      const filter = url.searchParams.getAll("lifecycle");
      const items =
        state.mode === "empty"
          ? []
          : state.items.filter(
              (item) => !filter.length || filter.includes(item.lifecycle),
            );
      return json(route, { items, next_cursor: null });
    }
    if (path === "/api/v1/projects" && request.method() === "POST") {
      const body = request.postDataJSON();
      state.writes.push(body);
      state.items.push({
        id: body.project_id,
        name: body.name,
        description: body.description ?? "",
        lifecycle: "active",
        version: "1",
      });
      return json(
        route,
        {
          state: "ready",
          project: {
            id: body.project_id,
            owner_user_id: id(1),
            name: body.name,
            normalized_name: body.name.toLowerCase(),
            description: body.description ?? "",
            lifecycle: "active",
            version: "1",
            current_sprint_id: null,
            created_at: at,
            updated_at: at,
            archived_at: null,
          },
        },
        201,
      );
    }
    state.faults.push(`Unexpected API: ${request.method()} ${path}`);
    return route.abort("failed");
  });
  return state;
}
async function noOverflow(page: Page) {
  await expect
    .poll(() =>
      page.evaluate(() => ({
        document: document.documentElement.scrollWidth <= innerWidth + 1,
        body: document.body.scrollWidth <= innerWidth + 1,
        main: [...document.querySelectorAll("main")].every(
          (el) => el.scrollWidth <= el.clientWidth + 1,
        ),
      })),
    )
    .toEqual({ document: true, body: true, main: true });
}
async function screenshot(page: Page, name: string) {
  await noOverflow(page);
  await page.evaluate(() => document.fonts.ready);
  mkdirSync(evidence, { recursive: true });
  await page.screenshot({
    path: `${evidence}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
}
async function ready(page: Page) {
  await page.goto("/projects");
  await expect(
    page.getByRole("heading", { name: "项目", exact: true, level: 1 }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Atlas", exact: true }),
  ).toBeVisible();
}

test("light desktop keeps a bounded list and independent keyboard modal", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await ready(page);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.getByRole("separator")).toHaveCount(0);
  await screenshot(page, "1440-light-list");
  const list = page.getByRole("list", {
    name: "项目列表",
    exact: true,
    includeHidden: true,
  });
  const before = await list.boundingBox();
  const create = page.getByRole("button", { name: "新建项目", exact: true });
  await create.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveAttribute("aria-modal", "true");
  await expect
    .poll(() => dialog.evaluate((el) => el.contains(document.activeElement)))
    .toBe(true);
  expect(await list.boundingBox()).toEqual(before);
  await screenshot(page, "1440-light-dialog");
  const name = dialog.getByRole("textbox", { name: "项目名称", exact: true });
  await name.fill("DraftProject");
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press("Tab");
    expect(
      await dialog.evaluate((el) => el.contains(document.activeElement)),
    ).toBe(true);
  }
  await page.keyboard.press("Shift+Tab");
  expect(
    await dialog.evaluate((el) => el.contains(document.activeElement)),
  ).toBe(true);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeFocused();
  expect(state.writes).toHaveLength(0);
  await create.click();
  await expect(name).toHaveValue("");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeFocused();
  await page.setViewportSize({ width: 1920, height: 1080 });
  await noOverflow(page);
  const wide = await list.boundingBox();
  expect(wide!.width).toBeLessThan(1450);
  expect(state.faults).toEqual([]);
});

test("dark desktop and narrow layout retain readable list and usable modal", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await ready(page);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await screenshot(page, "1440-dark-list");
  await page.setViewportSize({ width: 390, height: 844 });
  await screenshot(page, "390-dark-list");
  const create = page.getByRole("button", { name: "新建项目", exact: true });
  await create.click();
  const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
  await expect(dialog).toBeVisible();
  const rect = await dialog.boundingBox();
  expect(rect!.x).toBeGreaterThanOrEqual(0);
  expect(rect!.x + rect!.width).toBeLessThanOrEqual(391);
  await screenshot(page, "390-dark-dialog");
  await dialog
    .getByRole("textbox", { name: "项目名称", exact: true })
    .fill("NarrowProject");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(create).toBeFocused();
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await screenshot(page, "390-light-list");
  expect(state.writes).toHaveLength(0);
  expect(state.faults).toEqual([]);
});

test("loading, empty and failed list states support retry", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  state.mode = "hold";
  await page.goto("/projects");
  await expect(
    page.getByText("正在读取项目列表", { exact: true }),
  ).toBeVisible();
  await screenshot(page, "1440-light-loading");
  state.mode = "empty";
  state.release();
  await expect(
    page.getByText("从第一个项目开始", { exact: true }),
  ).toBeVisible();
  await screenshot(page, "1440-light-empty");
  state.mode = "error";
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "项目列表读取失败", exact: true }),
  ).toBeVisible();
  await screenshot(page, "1440-light-error");
  state.mode = "ready";
  await page.getByRole("button", { name: /重读当前页|重试/ }).click();
  await expect(
    page.getByRole("link", { name: "Atlas", exact: true }),
  ).toBeVisible();
  expect(state.faults).toEqual([]);
});

test("modal submits one creation and presents the ready project link", async ({
  page,
}) => {
  const state = await fixture(page);
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await ready(page);
  await page.getByRole("button", { name: "新建项目", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新建项目", exact: true });
  await dialog
    .getByRole("textbox", { name: "项目名称", exact: true })
    .fill("Browser.Project");
  await dialog
    .getByRole("textbox", { name: "项目描述", exact: true })
    .fill("在独立窗口内创建项目。");
  await dialog
    .getByRole("button", { name: "确认创建项目", exact: true })
    .click();
  await expect(
    dialog.getByRole("link", { name: "进入项目", exact: true }),
  ).toHaveAttribute("href", "/owner/browser.project");
  await screenshot(page, "creation-ready");
  expect(state.writes).toHaveLength(1);
  expect(state.writes[0]).toMatchObject({
    name: "Browser.Project",
    description: "在独立窗口内创建项目。",
  });
  await dialog
    .getByRole("button", { name: "返回项目列表", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Browser.Project", exact: true }),
  ).toBeVisible();
  expect(state.faults).toEqual([]);
});
