import { test, expect, type Page } from "@playwright/test";
import {
  timelineMaterial,
  observeTimeline,
  timelineScreenshot,
} from "./task-timeline.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeTimeline>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const history = (page: Page) =>
  drawer(page).getByRole("region", { name: "任务历史", exact: true });
const items = (page: Page) =>
  history(page).locator(".timeline-events > .timeline-event");
async function eventIDs(page: Page) {
  // Read the rendered disclosure text, not Vue state or a later API response.
  return items(page).evaluateAll((nodes) =>
    nodes.map((node) => {
      const field = Array.from(node.querySelectorAll("details dt")).find(
        (dt) => dt.textContent === "事件 ID",
      );
      return field?.nextElementSibling?.textContent ?? null;
    }),
  );
}
async function fullComment(page: Page, body: string) {
  const comment = items(page)
    .filter({
      has: page.getByRole("heading", { name: "评论", exact: true }),
    })
    .locator("p.event-text");
  await expect(comment).toHaveCount(1);
  await comment.scrollIntoViewIfNeeded();
  await expect(comment).toBeVisible();
  expect(await comment.textContent()).toBe(body);
  await expect(comment.locator("*")).toHaveCount(0);
  await expect(comment).toHaveCSS("white-space", "pre-wrap");
  await expect(comment).toHaveCSS("overflow-wrap", "anywhere");
  await expect
    .poll(() =>
      comment.evaluate(
        (element) => element.scrollWidth <= element.clientWidth + 1,
      ),
    )
    .toBe(true);
}
async function pressHistory(page: Page, name: string) {
  const button = history(page).getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.focus();
  await page.keyboard.press("Enter");
}
async function selectHistory(page: Page, label: string, value: string) {
  const trigger = history(page).getByRole("button", {
    name: label,
    exact: true,
  });
  await expect(trigger).toBeEnabled();
  await trigger.focus();
  await page.keyboard.press("ArrowDown");
  const list = page.getByRole("listbox", { name: label, exact: true });
  const option = list.getByRole("option", { name: value, exact: true });
  await expect(option).toBeVisible();
  await option.focus();
  await page.keyboard.press("Enter");
  await expect(list).toHaveCount(0);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(trigger).toContainText(value);
  await expect(trigger).toBeFocused();
}

test("real persisted history reads recent, older and refreshed pages [recent-older-and-refresh]", async ({
  page,
}) => {
  const data = timelineMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      sameSite: "Lax",
      secure: false,
    },
  ]);
  const seen = await observeTimeline(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(
    drawer(page).getByRole("heading", { name: data.title, exact: true }),
  ).toBeVisible();
  await expect(
    drawer(page)
      .locator("dl.task-facts > div")
      .filter({
        has: page.getByText("版本", { exact: true }),
      })
      .locator("dd"),
  ).toHaveText(data.version);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(items(page)).toHaveCount(50);
  await expect(
    history(page).getByRole("button", { name: "加载较早历史", exact: true }),
  ).toBeEnabled();
  expect(await eventIDs(page)).toEqual([...data.event_ids.slice(1)].reverse());
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(1);

  await pressHistory(page, "加载较早历史");
  await expect(items(page)).toHaveCount(51);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(
    history(page).getByRole("button", { name: "加载较早历史", exact: true }),
  ).toHaveCount(0);
  await expect(
    history(page).getByRole("heading", { name: "任务历史", exact: true }),
  ).toBeFocused();
  expect(await eventIDs(page)).toEqual([...data.event_ids].reverse());
  await expect(
    items(page).last().getByRole("heading", { name: "创建任务", exact: true }),
  ).toBeVisible();
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(2);
  await timelineScreenshot(page, data, "1440-light");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await pressHistory(page, "重新读取历史");
  await expect(items(page)).toHaveCount(50);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(
    history(page).getByRole("button", { name: "加载较早历史", exact: true }),
  ).toBeEnabled();
  await expect(
    history(page).getByRole("button", { name: "重新读取历史", exact: true }),
  ).toBeFocused();
  expect(await eventIDs(page)).toEqual([...data.event_ids.slice(1)].reverse());
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(3);

  await selectHistory(page, "历史事件类型", "评论");
  await expect(items(page)).toHaveCount(1);
  await expect(
    history(page).getByRole("button", { name: "加载较早历史", exact: true }),
  ).toHaveCount(0);
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(4);

  await selectHistory(page, "历史事件类型", "添加阻塞");
  await expect(items(page)).toHaveCount(0);
  await expect(
    history(page).getByText("没有此类型的任务历史。", { exact: true }),
  ).toBeVisible();
  await seen.verify();
  seen.requirePages(5);

  await pressHistory(page, "清除事件筛选");
  await expect(items(page)).toHaveCount(50);
  await expect(
    history(page).getByRole("button", { name: "清除事件筛选", exact: true }),
  ).toHaveCount(0);
  await expect(
    history(page).getByRole("heading", { name: "任务历史", exact: true }),
  ).toBeFocused();
  expect(await eventIDs(page)).toEqual([...data.event_ids.slice(1)].reverse());
  await seen.verify();
  seen.requirePages(6);

  await selectHistory(page, "历史时间顺序", "由旧到新");
  await expect(items(page)).toHaveCount(50);
  expect(await eventIDs(page)).toEqual(data.event_ids.slice(0, 50));
  await expect(
    history(page).getByRole("button", { name: "加载较早历史", exact: true }),
  ).toHaveCount(0);
  await seen.verify();
  seen.requirePages(7);
  await pressHistory(page, "加载较新历史");
  await expect(items(page)).toHaveCount(51);
  expect(await eventIDs(page)).toEqual(data.event_ids);
  await expect(
    history(page).getByRole("button", { name: "加载较新历史", exact: true }),
  ).toHaveCount(0);
  await expect(
    history(page).getByRole("heading", { name: "任务历史", exact: true }),
  ).toBeFocused();
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(8);
  await timelineScreenshot(page, data, "390-dark");
});
