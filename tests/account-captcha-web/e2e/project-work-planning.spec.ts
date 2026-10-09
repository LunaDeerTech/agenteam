import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { join, isAbsolute } from "node:path";
import {
  material,
  button,
  details,
  recovery,
  path,
  enter,
  ready,
  login,
  choose,
  save,
  confirmed,
  ipc,
  complete,
  observe,
  originalBody,
} from "./project-work-planning.helpers";

const editor = (page: Page) =>
  page.getByRole("form", { name: /^(规划结构编辑|Task 规划编辑)$/ });
const title = (page: Page) =>
  editor(page).getByRole("textbox", { name: "标题", exact: true });
const tree = (page: Page) => page.locator(".desktop-tree");
async function current(page: Page, expected: string) {
  await expect(details(page)).toContainText(expected);
  await expect(button(page, "新建 Milestone")).toBeEnabled();
}
async function go(page: Page, destination: string) {
  await page.goto(destination);
  await ready(page);
}
async function discard(page: Page) {
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await button(dialog, "放弃本地修改").click();
  await expect(dialog).toHaveCount(0);
}
async function realOrdering(
  page: Page,
  peer: string,
  kind: "milestone" | "sprint" | "task",
) {
  const selected = new URL(page.url()).pathname.split("/").at(-1)!;
  const listPath = `/api/v1/projects/${material().work.main!.project_id}/${kind}s`;
  async function open(expected: string[]) {
    const pending = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === listPath &&
        r.request().method() === "GET",
    );
    await button(page, "调整同组顺序").click();
    const body = await originalBody(await pending);
    expect(body.items.map((item: { id: string }) => item.id)).toEqual(expected);
    expect(body.next_cursor === undefined).toBe(true);
  }
  async function move(tail: boolean) {
    const pending = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === `${listPath}/${selected}/reorder` &&
        r.request().method() === "POST",
    );
    if (!tail) {
      await button(page, "移到哪个对象之前").click();
      await page.getByRole("option").filter({ hasText: peer }).click();
    }
    await button(page, tail ? "移到组尾" : "移到所选对象之前").click();
    const response = await pending;
    expect(response.status()).toBe(200);
    const body = await originalBody(response);
    const request = response.request().postDataJSON();
    expect(
      body.changed === true &&
        BigInt(body[kind].version) === BigInt(request.expected_version) + 1n,
    ).toBe(true);
    await confirmed(page);
    await expect(button(page, "关闭排序")).toHaveCount(0);
  }
  await open([peer, selected]);
  await move(false);
  await open([selected, peer]);
  await move(true);
  await open([peer, selected]);
  await button(page, "关闭排序").click();
}
async function addHuman(page: Page, description: string) {
  await choose(page, "阻塞类型", "等待人工处理");
  await page
    .getByRole("textbox", { name: "阻塞说明", exact: true })
    .fill(description);
  await button(
    page.getByRole("form", { name: "添加阻塞", exact: true }),
    "添加阻塞",
  ).click();
  await confirmed(page);
}
async function addDependency(page: Page, target: string) {
  await choose(page, "阻塞类型", "依赖其它任务");
  await button(page, "读取依赖候选首页").click();
  await expect(button(page, "依赖任务")).toBeEnabled();
  await button(page, "依赖任务").click();
  await page.getByRole("option").filter({ hasText: target }).click();
  await button(
    page.getByRole("form", { name: "添加阻塞", exact: true }),
    "添加阻塞",
  ).click();
}

test("[read] actual Owner route, bounded pages, deep links and stale cursor", async ({
  page,
  browser,
}) => {
  const data = material(),
    seen = observe(page);
  await enter(page, data);
  await expect(page.getByText("请选择 Sprint", { exact: false })).toBeVisible();
  await expect(page).toHaveURL(new RegExp("/tasks/explore$"));
  const pages = tree(page).locator('[aria-label="Milestone 分页"]');
  await expect(tree(page).getByRole("treeitem")).toHaveCount(50);
  await button(pages, "下一页").click();
  await expect(pages).toContainText("第 2 页");
  await expect(tree(page).getByRole("treeitem")).toHaveCount(1);
  await expect(button(pages, "下一页")).toBeDisabled();
  await button(pages, "上一页").click();
  await expect(tree(page).getByRole("treeitem")).toHaveCount(50);
  await ipc("update", { resource: "milestone", text: "页已变更" });
  await button(pages, "下一页").click();
  await expect(
    tree(page).getByText("分页已失效，请明确从首页重新读取。", { exact: true }),
  ).toBeVisible();
  await expect(button(pages, "下一页")).toBeDisabled();
  await button(pages, "从首页读取 Milestone").click();
  await expect(button(pages, "下一页")).toBeEnabled();
  for (const kind of ["milestone", "sprint", "task"] as const) {
    await go(page, path(data, "main", kind));
    await current(page, data.work.main![`${kind}_id`]);
    await page.reload();
    await ready(page, data.work.main![`${kind}_id`]);
  }
  await expect(
    page.getByRole("navigation", { name: "规划层级" }),
  ).toContainText("规划 Sprint");
  await expect(
    page.getByText(
      "所选对象不在已加载的树页中。这里显示直接读取的详情与父级；树中的位置尚未加载。",
    ),
  ).toBeVisible();
  for (const kind of [undefined, "milestone", "sprint", "task"] as const) {
    await go(page, path(data, "dotted", kind));
    if (kind) await current(page, data.work.dotted![`${kind}_id`]);
    await page.reload();
    await ready(page, kind ? data.work.dotted![`${kind}_id`] : undefined);
  }
  for (const suffix of [
    "/tasks",
    "/tasks/explore/tasks/not-an-id",
    "/tasks/explore?x=1",
    "/tasks/explore/tasks/" + data.work.main!.task_id + "/extra",
  ]) {
    const before = seen.requests.length;
    await page.goto(
      `/${data.owner.username}/${data.projects.main!.name}${suffix}`,
    );
    await expect(
      page.getByRole("heading", { name: "任务规划", exact: true }),
    ).toHaveCount(0);
    expect(seen.requests.length === before).toBe(true);
  }
  const missingAsset = await page.request.get("/assets/does-not-exist-work.js");
  expect(
    missingAsset.status() === 404 &&
      !(missingAsset.headers()["content-type"] ?? "").includes("text/html"),
  ).toBe(true);
  const missingAPI = await page.request.get("/api/v1/does-not-exist-work");
  expect(
    missingAPI.status() === 404 &&
      !(missingAPI.headers()["content-type"] ?? "").includes("text/html"),
  ).toBe(true);
  const deniedContext = await browser.newContext();
  try {
    const denied = await deniedContext.newPage(),
      watched = observe(denied);
    await denied.goto(path(data, "main", "task"));
    await login(denied, data.other);
    await expect(
      denied.getByRole("heading", { name: "任务规划", exact: true }),
    ).toHaveCount(0);
    await expect(
      denied.getByRole("heading", { name: "项目不可用", exact: true }),
    ).toBeVisible();
    expect(watched.requests.length === 0).toBe(true);
  } finally {
    await deniedContext.close();
  }
  complete({
    pagination: true,
    deep_links: true,
    refresh: true,
    raw_route: true,
    parent: true,
    permissions: true,
    cursor: true,
    bodies: await seen.verify(),
  });
});

test("[planning] all nine planning mutations, raw Plan, conflict and grouped ordering", async ({
  page,
}) => {
  const data = material(),
    seen = observe(page);
  await enter(page, data);
  const sessionResponse = await page.request.get("/api/v1/session");
  expect(sessionResponse.status()).toBe(200);
  const session = await sessionResponse.json();
  await ipc("age-activity", { target: session.session.id });
  await button(page, "新建 Milestone").click();
  await title(page).fill("浏览器 Milestone");
  await editor(page)
    .getByRole("textbox", { name: "描述", exact: true })
    .fill("真实待清空描述");
  await save(page, "structure");
  await current(page, "浏览器 Milestone");
  await title(page).fill("修改 Milestone");
  await editor(page)
    .getByRole("textbox", { name: "描述", exact: true })
    .fill("");
  await save(page);
  await current(page, "修改 Milestone");
  await expect(
    editor(page).getByRole("textbox", { name: "描述", exact: true }),
  ).toHaveValue("");
  await realOrdering(page, data.work.main!.milestone_id, "milestone");
  await go(page, path(data, "main", "milestone"));
  await button(page, "新建 Sprint").click();
  await title(page).fill("浏览器 Sprint");
  await save(page, "structure");
  await current(page, "浏览器 Sprint");
  await title(page).fill("修改 Sprint");
  await save(page);
  await realOrdering(page, data.work.main!.sprint_id, "sprint");
  await go(page, path(data, "main", "sprint"));
  await button(page, "新建 Task").click();
  await title(page).fill("浏览器 Task");
  const createTask = button(editor(page), "创建 Task");
  const taskPosts = () =>
    seen.requests.filter(
      (request) =>
        request.method === "POST" &&
        request.path === `/api/v1/projects/${data.work.main!.project_id}/tasks`,
    );
  // Required values are validated on explicit submission, before any command.
  await expect(createTask).toBeEnabled();
  await createTask.click();
  await expect(
    editor(page).getByText("请选择类型。", { exact: true }),
  ).toBeVisible();
  await expect(
    editor(page).getByText("请选择优先级。", { exact: true }),
  ).toBeVisible();
  expect(taskPosts()).toHaveLength(0);
  await choose(page, "Task 类型", "功能");
  await createTask.click();
  await expect(
    editor(page).getByText("请选择类型。", { exact: true }),
  ).toHaveCount(0);
  await expect(
    editor(page).getByText("请选择优先级。", { exact: true }),
  ).toBeVisible();
  expect(taskPosts()).toHaveLength(0);
  await choose(page, "Task 优先级", "中");
  const plan = "  <script>literal</script>\n第二行 🧭  ";
  await editor(page)
    .getByRole("textbox", { name: "Plan", exact: true })
    .fill(plan);
  await save(page, "task");
  await current(page, "浏览器 Task");
  await expect
    .poll(() => details(page).locator("p.raw").last().textContent())
    .toBe(plan);
  await choose(page, "Task 优先级", "高");
  await title(page).fill("修改 Task");
  await editor(page)
    .getByRole("textbox", { name: "Plan", exact: true })
    .fill("");
  await save(page);
  await current(page, "修改 Task");
  await expect(details(page)).toContainText("尚未填写 Plan");
  await realOrdering(page, data.work.main!.related_id, "task");
  await button(page, "从首页读取阻塞记录").click();
  await expect(
    page.getByText("本页没有符合状态的阻塞记录。", { exact: true }),
  ).toBeVisible();
  // A separate real command advances the selected version; the UI must retain
  // its original draft and require an explicit read/adopt, never auto-rebase.
  await go(page, path(data, "main", "task"));
  await current(page, data.work.main!.task_id);
  await title(page).fill("保留冲突草稿");
  await ipc("update", { resource: "task", text: "另一命令的当前 Plan" });
  await button(editor(page), "保存修改").click();
  await expect(
    editor(page).getByText("版本冲突，原草稿与版本已保留。请先读取当前值。"),
  ).toBeVisible();
  await expect(title(page)).toHaveValue("保留冲突草稿");
  await button(editor(page), "读取当前值").click();
  await current(page, "另一命令的当前 Plan");
  await expect(title(page)).toHaveValue("保留冲突草稿");
  await button(page, "按当前值重新编辑").click();
  await discard(page);
  await expect(title(page)).toHaveValue("规划任务");
  complete({
    structure: true,
    task: true,
    plan: true,
    ordering: true,
    conflict: true,
    current_receipt: true,
    bodies: await seen.verify(),
  });
});

test("[blockers] two kinds, real dependency cycle refusal, resolution and stale pages", async ({
  page,
}) => {
  const data = material(),
    seen = observe(page),
    seed = data.work.main!;
  await enter(page, data, "task");
  await button(page, "从首页读取阻塞记录").click();
  await addHuman(page, "需要人工确认");
  await addDependency(page, seed.related_id);
  await confirmed(page);
  await button(page, "从首页读取阻塞记录").click();
  const pages = page.locator('[aria-label="阻塞记录分页"]');
  await expect(button(pages, "下一页")).toBeEnabled();
  await button(pages, "下一页").click();
  await expect(pages).toContainText("第 2 页");
  const dependency = page
    .locator(".records li")
    .filter({ hasText: seed.related_id });
  await expect(dependency).toBeVisible();
  await button(dependency, "读取依赖任务当前信息").click();
  await expect(dependency).toContainText("规划任务 · 待规划");
  await go(page, path(data, "main", "task", seed.related_id));
  await current(page, seed.related_id);
  const rejected = page.waitForResponse(
    (r) =>
      r.url().endsWith(`/tasks/${seed.related_id}/blockers`) &&
      r.request().method() === "POST",
  );
  await addDependency(page, seed.task_id);
  expect((await rejected).status() === 409).toBe(true);
  await expect(
    page.getByRole("region", { name: "任务阻塞记录" }).getByRole("alert"),
  ).toBeVisible();
  await expect(details(page)).toContainText("待规划");
  await go(page, path(data, "main", "task"));
  await button(page, "从首页读取阻塞记录").click();
  await button(page.locator(".records li").first(), "解除这条阻塞").click();
  await page.getByRole("textbox", { name: "解除备注", exact: true }).fill(" ");
  await button(page, "确认解除").click();
  await expect(
    page.getByRole("region", { name: "任务阻塞记录" }).getByRole("alert"),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "解除备注", exact: true }).fill("");
  await button(page, "确认解除").click();
  await confirmed(page);
  await choose(page, "阻塞记录状态", "已解除");
  await expect(page.locator(".records li").first()).toContainText("无备注");
  await expect(details(page)).toContainText("待规划");
  await choose(page, "阻塞记录状态", "全部状态");
  await expect(button(pages, "下一页")).toBeEnabled();
  await ipc("update", { resource: "task", text: "使版本分页失效" });
  await button(pages, "下一页").click();
  await expect(
    page.getByText("分页已失效，请明确从首页重新读取。", { exact: true }),
  ).toBeVisible();
  await expect(button(pages, "下一页")).toBeDisabled();
  complete({
    two_types: true,
    resolve: true,
    cycle: true,
    status: true,
    cursor: true,
    state_unchanged: true,
    bodies: await seen.verify(),
  });
});

test("[recovery] three committed lost responses retain original intent and historical receipts", async ({
  page,
}) => {
  const data = material(),
    seen = observe(page);
  for (const [index, domain] of ["structure", "task", "blocker"].entries()) {
    const kind = domain === "structure" ? "milestone" : "task";
    if (!index) await enter(page, data, kind, domain);
    else await go(page, path(data, domain, kind));
    const seed = data.work[domain]!;
    await current(page, seed[`${kind}_id`]);
    await ipc("arm-loss", { project: domain, domain });
    if (domain === "blocker") {
      await choose(page, "阻塞类型", "等待人工处理");
      await page
        .getByRole("textbox", { name: "阻塞说明", exact: true })
        .fill("历史阻塞原命令");
      await button(
        page.getByRole("form", { name: "添加阻塞", exact: true }),
        "添加阻塞",
      ).click();
    } else {
      await title(page).fill("历史原命令 " + domain);
      await button(editor(page), "保存修改").click();
    }
    await expect(
      recovery(page).getByRole("heading", {
        name: "原命令结果不确定",
        exact: true,
      }),
    ).toBeVisible();
    await button(editor(page), "读取当前值").click();
    await expect(
      recovery(page).getByRole("heading", {
        name: "原命令结果不确定",
        exact: true,
      }),
    ).toBeVisible();
    await ipc("update", {
      project: domain,
      resource: kind,
      text: "独立更新后的当前值",
    });
    await button(recovery(page), "查证原命令").click();
    await confirmed(page);
    if (domain !== "blocker")
      await expect(recovery(page)).toContainText("历史原命令 " + domain);
    if (domain === "blocker") {
      const id = await recovery(page)
        .locator("dt")
        .filter({ hasText: /^历史阻塞记录 ID$/ })
        .locator("+ dd")
        .textContent();
      await ipc("resolve", { project: domain, target: id, text: "后继解除" });
    }
    await ipc("archive", { project: domain });
    // Requalification through the existing App event re-reads Session/Owner,
    // retaining the same identity's immutable original intent.
    await page.evaluate(() =>
      window.dispatchEvent(new PageTransitionEvent("pageshow")),
    );
    await expect(
      page.getByText("项目已归档，当前内容只读。", { exact: true }),
    ).toBeVisible();
    await button(recovery(page), "查证原命令").click();
    await confirmed(page);
    await button(recovery(page), "按原请求重放").click();
    await confirmed(page);
    await expect(button(page, "新建 Milestone")).toBeDisabled();
    await button(recovery(page), "放弃本地追踪").click();
    if (await page.getByRole("dialog").count()) await discard(page);
    await expect(recovery(page)).toHaveCount(0);
  }
  complete({
    three_domains: true,
    lookup_original: true,
    same_replay: true,
    history: true,
    archive: true,
    unique_facts: true,
    bodies: await seen.verify(3),
  });
});

test("[identity] dirty guards, same Session checking, revocation and old read isolation", async ({
  page,
  browser,
}) => {
  const data = material(),
    seen = observe(page);
  await enter(page, data, "task");
  await title(page).fill("仅当前身份的草稿");
  await button(page, "退出登录").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await button(dialog, "继续编辑").click();
  await expect(title(page)).toHaveValue("仅当前身份的草稿");
  await page.evaluate(() =>
    window.dispatchEvent(new PageTransitionEvent("pageshow")),
  );
  await expect(title(page)).toHaveValue("仅当前身份的草稿");
  // Logout in this same genuine cookie jar revokes the original Session. The
  // public application still owns a draft until its next actual Session read.
  const sessionResponse = await page.request.get("/api/v1/session");
  expect(sessionResponse.status() === 200).toBe(true);
  const originalSession = await sessionResponse.json();
  const revoked = await page.request.post("/api/v1/sessions/logout", {
    headers: {
      "X-CSRF-Token": originalSession.csrf_token,
      Origin: new URL(page.url()).origin,
      "Sec-Fetch-Site": "same-origin",
    },
  });
  expect(revoked.status() === 204).toBe(true);
  await page.evaluate(() =>
    window.dispatchEvent(new PageTransitionEvent("pageshow")),
  );
  await expect(page.locator("#login-email")).toBeVisible();
  await expect(title(page)).toHaveCount(0);
  await login(page, data.owner);
  await go(page, path(data, "main", "task"));
  await expect(title(page)).toHaveValue("规划任务");
  const nextSession = await (await page.request.get("/api/v1/session")).json();
  expect(nextSession.session.id !== originalSession.session.id).toBe(true);
  await ipc("hold-read", { resource: "task" });
  await button(editor(page), "读取当前值").click();
  await expect.poll(async () => (await ipc("hold-status")).started).toBe(true);
  await page.goto(path(data, "duplicate", "task"));
  await ipc("release-read");
  await ready(page, data.work.duplicate!.task_id);
  await expect(details(page)).not.toContainText(data.work.main!.task_id);
  const deniedContext = await browser.newContext();
  try {
    const denied = await deniedContext.newPage(),
      observation = observe(denied);
    await denied.goto(path(data, "main", "task"));
    await login(denied, data.admin);
    await expect(
      denied.getByRole("heading", { name: "项目不可用", exact: true }),
    ).toBeVisible();
    expect(observation.requests.length === 0).toBe(true);
  } finally {
    await deniedContext.close();
  }
  await button(page, "退出登录").click();
  await expect(page.locator("#login-email")).toBeVisible();
  complete({
    logout: true,
    revocation: true,
    owner: true,
    checking: true,
    new_session: true,
    late_read: true,
    confirmations: true,
    bodies: await seen.verify(1),
  });
});

test("[layouts] eight actual layouts, keyboard, focus and production content", async ({
  page,
}) => {
  const data = material(),
    seen = observe(page);
  await enter(page, data, "task");
  const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
  expect(!!images && isAbsolute(images!)).toBe(true);
  mkdirSync(images!, { recursive: true });
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme });
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      if (width === 390) {
        const toggle = button(page, "打开规划树");
        await toggle.click();
        await expect(page.getByRole("dialog")).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(toggle).toBeFocused();
      } else {
        const item = tree(page).getByRole("treeitem").first();
        await item.focus();
        await page.keyboard.press("ArrowRight");
        await expect(
          tree(page)
            .getByRole("treeitem")
            .filter({ hasText: "规划 Sprint" })
            .first(),
        ).toBeVisible();
      }
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth + 1,
          ),
        )
        .toBe(true);
      await expect
        .poll(() =>
          page.evaluate(() =>
            document.getAnimations().every((a) => a.playState !== "running"),
          ),
        )
        .toBe(true);
      await page.screenshot({
        path: join(images!, `work-${theme}-${width}.png`),
        fullPage: true,
      });
    }
  }
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 390, height: 900 });
  await button(page, "打开规划树").click();
  await page.keyboard.press("Escape");
  await expect(button(page, "打开规划树")).toBeFocused();
  await ipc("archive");
  await page.evaluate(() =>
    window.dispatchEvent(new PageTransitionEvent("pageshow")),
  );
  await expect(
    page.getByText("项目已归档，当前内容只读。", { exact: true }),
  ).toBeVisible();
  await expect(button(page, "新建 Milestone")).toBeDisabled();
  await go(page, path(data, "dotted"));
  await expect(page.getByText("请选择 Sprint", { exact: false })).toBeVisible();
  await page.goto(
    path(data, "dotted", "task", "01900000-0000-7000-8000-000000000099"),
  );
  await expect(
    page.getByText("当前身份无法读取此对象，请重新确认项目访问权限。", {
      exact: true,
    }),
  ).toBeVisible();
  const debug = await page.request.get("/debug");
  expect(debug.status() === 404).toBe(true);
  complete({
    keyboard: true,
    focus: true,
    no_overflow: true,
    readonly: true,
    empty: true,
    error: true,
    no_debug: true,
    layouts: 8,
    bodies: await seen.verify(),
  });
});
