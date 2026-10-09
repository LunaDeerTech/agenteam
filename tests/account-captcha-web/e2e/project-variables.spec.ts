import { test, expect, type Page } from "@playwright/test";
import { join, isAbsolute } from "node:path";
import {
  button,
  checkpoint,
  complete,
  confirmed,
  editor,
  endTracking,
  enter,
  field,
  history,
  ipc,
  login,
  material,
  observe,
  path,
  protect,
  ready,
  select,
  table,
} from "./project-variables.helpers";
import { uuid7 } from "../../../web/src/api/client";

const projectLink = (page: Page) =>
  page
    .getByRole("navigation", { name: "系统导航", exact: true })
    .getByRole("link", { name: "项目", exact: true });
async function currentID(page: Page) {
  const text = await editor(page).locator(".meta").innerText();
  const id = text.match(
    /[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/,
  )?.[0];
  expect(uuid7.test(id ?? "")).toBe(true);
  return id!;
}
async function adopt(page: Page) {
  await button(page, "读取当前变量").click();
  await expect(button(page, "采用当前内容")).toBeEnabled();
  await button(page, "采用当前内容").click();
  await expect(button(page, "删除变量")).toBeEnabled();
}
async function remove(page: Page) {
  await button(page, "删除变量").click();
  const d = page.getByRole("dialog", { name: "删除普通变量？", exact: true });
  await expect(d).toContainText("历史操作回执仍可能保留旧值");
  await button(d, "删除变量").click();
}
async function create(
  page: Page,
  name: string,
  value: string,
  description = "",
) {
  await button(page, "新建变量").click();
  await field(page, "名称").fill(name);
  await field(page, "描述").fill(description);
  await field(page, "值").fill(value);
  const id = await currentID(page);
  await button(page, "创建变量").click();
  await confirmed(page);
  return id;
}
async function refreshSession(page: Page) {
  await page.evaluate(() => window.dispatchEvent(new Event("pageshow")));
}
async function sessionID(page: Page) {
  return page.evaluate(async () => {
    const r = await fetch("/api/v1/session", {
      cache: "no-store",
      credentials: "same-origin",
    });
    const v = await r.json();
    return typeof v.session?.id === "string" ? v.session.id : "";
  });
}
async function revoke(page: Page) {
  const status = await page.evaluate(async () => {
    const r = await fetch("/api/v1/session", {
        cache: "no-store",
        credentials: "same-origin",
      }),
      v = await r.json();
    const out = await fetch("/api/v1/sessions/logout", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": v.csrf_token,
        "Idempotency-Key": crypto.randomUUID(),
      },
      body: "{}",
    });
    await out.arrayBuffer();
    return out.status;
  });
  expect(status === 204).toBe(true);
  await refreshSession(page);
}
// Explicit protocol probes use the same actual browser Cookie context. Private
// request material stays inside the browser and the in-memory observation.
async function protocol(
  page: Page,
  method: string,
  url: string,
  body?: unknown,
) {
  return page.evaluate(
    async ({ method, url, body }) => {
      const session = await fetch("/api/v1/session", {
        cache: "no-store",
        credentials: "same-origin",
      });
      const current = await session.json();
      const response = await fetch(url, {
        method,
        credentials: "same-origin",
        cache: "no-store",
        headers:
          method === "GET"
            ? {}
            : {
                "Content-Type": "application/json",
                "X-CSRF-Token": current.csrf_token,
                "Idempotency-Key": crypto.randomUUID(),
              },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      const value = await response.json();
      return { status: response.status, value };
    },
    { method, url, body },
  );
}
async function noOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
}
async function focusInside(page: Page) {
  expect(
    await page
      .getByRole("dialog")
      .evaluate((node) => node.contains(document.activeElement)),
  ).toBe(true);
}

test("[read] dotted direct navigation, exact summaries and stale cursors", async ({
  page,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data, "dotted");
  await expect(table(page).getByRole("row")).toHaveCount(51);
  await expect(field(page, "值")).toHaveCount(0);
  expect(
    network.entries.filter(
      (e) => e.method === "GET" && e.url.pathname.split("/").length === 7,
    ).length === 0,
  ).toBe(true);
  await button(page, "下一页").click();
  await expect(
    page.getByText("第 2 页 · 每页50项", { exact: true }),
  ).toBeVisible();
  await expect(table(page).getByRole("row")).toHaveCount(3);
  await button(page, "上一页").click();
  await expect(
    page.getByText("第 1 页 · 每页50项", { exact: true }),
  ).toBeVisible();
  await select(page);
  await expect(field(page, "值")).toHaveValue(data.variables.dotted!.value);
  await button(page, "关闭详情").click();
  checkpoint();
  await button(page, "下一页").click();
  await ipc("update", {
    project: "dotted",
    target: data.targets.dotted!,
    name: "Z_CHANGED",
  });
  await button(page, "重读本页").click();
  await expect(
    page.getByText("分页位置已失效。已保留旧观察，请从第一页重新读取。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(table(page).getByRole("row")).toHaveCount(3);
  await expect(button(page, "下一页")).toBeDisabled();
  await button(page, "从第一页重新读取").click();
  await expect(
    page.getByText("第 1 页 · 每页50项", { exact: true }),
  ).toBeVisible();
  await expect(button(page, "下一页")).toBeEnabled();
  const reload = await page.reload();
  expect(reload?.status() === 200).toBe(true);
  await ready(page);
  await page.getByRole("link", { name: "基本信息", exact: true }).click();
  await page.getByRole("link", { name: "Variables", exact: true }).click();
  await ready(page);
  checkpoint();
  for (const suffix of ["?page=1", "#value", "/extra", "%2Fextra"]) {
    const before = network.entries.length;
    await page.goto(path(data, "dotted") + suffix);
    await expect(
      page.getByRole("heading", { name: "Variables", exact: true }),
    ).toHaveCount(0);
    expect(network.entries.length === before).toBe(true);
  }
  for (const missing of [
    "/api/v1/variables-missing",
    "/assets/variables-missing.js",
  ]) {
    const r = await page.request.get(missing);
    expect(r.status() === 404).toBe(true);
    expect(!(await r.text()).includes('<div id="app">')).toBe(true);
  }
  await errors.check();
  complete({
    paging: true,
    deep_link: true,
    refresh: true,
    summary: true,
    cursor_stale: true,
    invalid_route: true,
    network: await network.finish(),
  });
});

test("[crud] empty values, presence, no-op, deletion and same-name identity", async ({
  page,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data);
  const original = await create(page, "EMPTY_VALUE", "");
  await expect(history(page).locator("pre")).toHaveText("");
  await endTracking(page);
  await adopt(page);
  await expect(button(page, "保存修改")).toBeDisabled();
  await field(page, "描述").fill("description only");
  await button(page, "保存修改").click();
  await confirmed(page);
  const update = network.entries.filter((e) => e.method === "PATCH").at(-1)!;
  expect(
    JSON.stringify(Object.keys(JSON.parse(update.body!).request)) ===
      '["description"]',
  ).toBe(true);
  await endTracking(page);
  await adopt(page);
  const version = (await editor(page).locator(".meta").innerText()).match(
    /已读版本 ([1-9][0-9]*)/,
  )?.[1];
  expect(version !== undefined).toBe(true);
  checkpoint();
  const before = await ipc("observe");
  const noop = await protocol(
    page,
    "PATCH",
    `/api/v1/projects/${data.ids.main}/variables/${original}`,
    { expected_version: version, request: { value: "" } },
  );
  expect(
    noop.status === 200 &&
      noop.value.changed === false &&
      noop.value.audit_id === null &&
      noop.value.event_id === null,
  ).toBe(true);
  const after = await ipc("observe");
  expect(
    after.commands === before.commands + 1 &&
      after.history === before.history &&
      after.audits === before.audits &&
      after.events === before.events,
  ).toBe(true);
  await remove(page);
  await confirmed(page);
  await button(page, "查询原操作").click();
  await confirmed(page);
  await button(page, "读取原对象当前信息").click();
  await expect(editor(page)).toContainText("当前对象不存在或不可访问");
  await expect(history(page)).toContainText("原删除已确认");
  await button(page, "重放原操作").click();
  await confirmed(page);
  await endTracking(page);
  await button(page, "关闭详情").click();
  const replacement = await create(
    page,
    "EMPTY_VALUE",
    "replacement ordinary value",
  );
  expect(replacement !== original).toBe(true);
  await expect(history(page)).toContainText(replacement);
  await endTracking(page);
  await button(page, "关闭详情").click();
  await button(page, "从第一页重新读取").click();
  await select(page, "EMPTY_VALUE");
  await expect(field(page, "值")).toHaveValue("replacement ordinary value");
  expect((await currentID(page)) === replacement).toBe(true);
  await errors.check();
  complete({
    crud: true,
    empty: true,
    presence: true,
    noop: true,
    same_name_new_id: true,
    historical: true,
    network: await network.finish(),
  });
});

test("[recovery] three completed commands cut before EOF and replay original intent", async ({
  page,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data, "create");
  for (const kind of ["create", "update", "delete"] as const) {
    if (kind !== "create") {
      await page.goto(path(data, kind));
      await ready(page);
      await select(page);
    } else {
      await button(page, "新建变量").click();
      await field(page, "名称").fill("RECOVER_CREATE");
      await field(page, "值").fill("historical create value");
    }
    if (kind === "update")
      await field(page, "值").fill("historical update value");
    const target = await currentID(page),
      method =
        kind === "create" ? "POST" : kind === "update" ? "PATCH" : "DELETE";
    checkpoint();
    await ipc("arm-loss", { project: kind, kind, target });
    network.cut(method, data.ids[kind]!, target);
    if (kind === "delete") await remove(page);
    else
      await button(page, kind === "create" ? "创建变量" : "保存修改").click();
    await expect(history(page)).toContainText("结果尚未确认");
    await expect(button(page, "查询原操作")).toBeEnabled();
    const committed = await ipc("observe", { project: kind });
    await button(page, "读取原对象当前信息").click();
    await expect(history(page)).toContainText("结果尚未确认");
    if (kind !== "delete")
      await ipc("update", {
        project: kind,
        target,
        value: "later current value",
      });
    const later = await ipc("observe", { project: kind });
    await button(page, "查询原操作").click();
    await confirmed(page);
    if (kind !== "delete") {
      await expect(history(page).locator("pre")).toHaveText(
        `historical ${kind} value`,
      );
      await button(page, "读取原对象当前信息").click();
      await expect(editor(page)).toContainText("later current value");
    }
    await button(page, "重放原操作").click();
    await confirmed(page);
    await button(page, "查询原操作").click();
    await confirmed(page);
    const replayed = await ipc("observe", { project: kind });
    expect(
      ["commands", "history", "audits", "events"].every(
        (key) => later[key] === replayed[key],
      ),
    ).toBe(true);
    expect(committed.commands >= 1).toBe(true);
    await endTracking(page);
    if (await editor(page).count()) await button(page, "关闭详情").click();
  }
  await errors.check();
  complete({
    three_cuts: true,
    original_lookup: true,
    original_replay: true,
    historical: true,
    unique_facts: true,
    network: await network.finish(),
  });
});

test("[identity] checking preserves draft, real session replacement clears it, cancellation joins", async ({
  page,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data);
  await select(page);
  const original = await sessionID(page);
  await field(page, "值").fill("same session draft");
  await ipc("fail-session");
  await refreshSession(page);
  await expect(button(page, "检查当前会话")).toBeEnabled();
  await expect(editor(page)).toHaveCount(0);
  await button(page, "检查当前会话").click();
  await ready(page);
  await expect(field(page, "值")).toHaveValue("same session draft");
  expect((await sessionID(page)) === original).toBe(true);
  await button(page, "退出登录").click();
  await expect(
    page.getByRole("dialog", { name: "离开变量管理？", exact: true }),
  ).toBeVisible();
  await button(page.getByRole("dialog"), "取消").click();
  await expect(button(page, "退出登录")).toBeFocused();
  await expect(field(page, "值")).toHaveValue("same session draft");
  checkpoint();
  await revoke(page);
  await login(page, data.owner);
  await page.goto(path(data));
  await ready(page);
  expect((await sessionID(page)) !== original).toBe(true);
  await expect(history(page)).toHaveCount(0);
  await select(page);
  await expect(field(page, "值")).toHaveValue(data.variables.main!.value);
  await ipc("hold-read", { target: data.targets.main! });
  await button(page, "读取当前变量").click();
  await expect
    .poll(async () => (await ipc("hold-status")).started, {
      intervals: [20, 40],
    })
    .toBe(true);
  expect((await ipc("hold-status")).finished === false).toBe(true);
  network.cancel(data.ids.main!, data.targets.main!);
  await expect(button(page, "退出登录")).toBeDisabled();
  await projectLink(page).click();
  await expect(page).toHaveURL(/\/projects$/);
  await expect
    .poll(
      async () => {
        const status = await ipc("hold-status");
        return status.finished && status.canceled;
      },
      { intervals: [20, 40] },
    )
    .toBe(true);
  await ipc("release-read");
  await expect(editor(page)).toHaveCount(0);
  await errors.check();
  complete({
    checking: true,
    cancel_logout: true,
    new_session: true,
    held_cancel: true,
    joined: true,
    network: await network.finish(),
  });
});

test("[authority] current Human Owner, administrators and archived original operations", async ({
  page,
  browser,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data);
  await select(page);
  await field(page, "值").fill("prepared before archive");
  await ipc("archive");
  await button(page, "保存修改").click();
  await expect(history(page)).toContainText("本次请求被明确拒绝");
  await expect(field(page, "值")).toHaveValue("prepared before archive");
  await endTracking(page);
  await button(page, "关闭详情").click();
  const discard = page.getByRole("dialog", {
    name: "放弃变量修改？",
    exact: true,
  });
  if (await discard.isVisible()) await button(discard, "放弃本地材料").click();
  await page.reload();
  await ready(page);
  await expect(button(page, "新建变量")).toBeDisabled();
  await expect(page.getByText(/项目当前只读（archived）/)).toBeVisible();
  await page.goto(path(data, "update"));
  await ready(page);
  await select(page);
  await field(page, "值").fill("before terminal lifecycle");
  await button(page, "保存修改").click();
  await confirmed(page);
  await ipc("archive", { project: "update" });
  await refreshSession(page);
  await ready(page);
  await expect(button(page, "新建变量")).toBeDisabled();
  await button(page, "查询原操作").click();
  await confirmed(page);
  await button(page, "重放原操作").click();
  await confirmed(page);
  await expect(history(page).locator("pre")).toHaveText(
    "before terminal lifecycle",
  );
  checkpoint();
  for (const who of [data.other, data.admin]) {
    const context = await browser.newContext();
    try {
      const outsider = await context.newPage(),
        observed = observe(outsider);
      await outsider.goto(process.env.AGENTEAM_AUTH_WEB_ORIGIN! + path(data));
      await login(outsider, who);
      await expect(
        outsider.getByRole("heading", { name: "项目不可用", exact: true }),
      ).toBeVisible();
      expect(observed.entries.length === 0).toBe(true);
      const denied = await protocol(
        outsider,
        "GET",
        `/api/v1/projects/${data.ids.main}/variables/${data.targets.main}`,
      );
      expect([403, 404].includes(denied.status)).toBe(true);
      const crossed = await protocol(
        outsider,
        "GET",
        `/api/v1/projects/${data.ids.other}/variables/${data.targets.main}`,
      );
      expect([403, 404].includes(crossed.status)).toBe(true);
      await observed.finish({ allowNoSuccess: true });
    } finally {
      await context.close();
    }
  }
  await errors.check();
  complete({
    non_owner: true,
    administrator: true,
    archived: true,
    new_write_refused: true,
    history_replay: true,
    network: await network.finish(),
  });
});

test("[layouts] themes, 390px, keyboard dialogs, long text and reduced motion", async ({
  page,
}) => {
  const data = material(),
    network = observe(page),
    errors = protect(page);
  await errors.install();
  checkpoint();
  await enter(page, data, "dotted");
  await select(page);
  await field(page, "值").fill("long ordinary text ".repeat(1000));
  await button(page, "关闭详情").focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toHaveAccessibleName("放弃变量修改？");
  await focusInside(page);
  for (let n = 0; n < 5; n++) await page.keyboard.press("Tab");
  await focusInside(page);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(button(page, "关闭详情")).toBeFocused();
  const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
  expect(!!images && isAbsolute(images)).toBe(true);
  for (const colorScheme of ["light", "dark"] as const)
    for (const width of [1440, 390])
      for (const reducedMotion of ["no-preference", "reduce"] as const) {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ colorScheme, reducedMotion });
        await expect(page.locator("html")).toHaveAttribute(
          "data-theme",
          colorScheme,
        );
        await noOverflow(page);
        await field(page, "值").focus();
        await expect(field(page, "值")).toBeFocused();
        expect(
          await page.evaluate(
            (reduced) =>
              matchMedia("(prefers-reduced-motion: reduce)").matches ===
              reduced,
            reducedMotion === "reduce",
          ),
        ).toBe(true);
        await page.screenshot({
          path: join(
            images!,
            `variables-${colorScheme}-${width}-${reducedMotion}.png`,
          ),
          fullPage: true,
          animations: "disabled",
          mask: [
            page.locator(".account-actions"),
            editor(page),
            table(page),
            history(page),
          ],
        });
      }
  await page.setViewportSize({ width: 780, height: 900 });
  await page.evaluate(() => {
    document.documentElement.style.zoom = "2";
  });
  await noOverflow(page);
  await page.evaluate(() => {
    document.documentElement.style.zoom = "";
  });
  await button(page, "关闭详情").click();
  await button(page.getByRole("dialog"), "放弃本地材料").click();
  await page.goto(path(data, "archived"));
  await ready(page);
  await select(page);
  await expect(field(page, "值")).toBeDisabled();
  await expect(button(page, "保存修改")).toBeDisabled();
  await noOverflow(page);
  await errors.check();
  complete({
    themes: true,
    narrow: true,
    keyboard: true,
    focus: true,
    readonly: true,
    no_overflow: true,
    network: await network.finish(),
  });
});
