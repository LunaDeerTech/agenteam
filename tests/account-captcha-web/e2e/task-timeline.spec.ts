import { test, expect, type Page } from "@playwright/test";
import { timelineMaterial, observeTimeline, timelineScreenshot } from "./task-timeline.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeTimeline>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
const drawer = (page: Page) => page.getByRole("dialog", { name: "任务详情", exact: true });
const history = (page: Page) => drawer(page).getByRole("region", { name: "任务历史", exact: true });
const items = (page: Page) => history(page).locator(".timeline-events > .timeline-event");
async function eventIDs(page: Page) {
  // Read the rendered disclosure text, not Vue state or a later API response.
  return items(page).evaluateAll((nodes) => nodes.map((node) => {
    const field = Array.from(node.querySelectorAll("details dt"))
      .find((dt) => dt.textContent === "事件 ID");
    return field?.nextElementSibling?.textContent ?? null;
  }));
}
async function fullComment(page: Page, body: string) {
  const comment = items(page).filter({
    has: page.getByRole("heading", { name: "评论", exact: true }),
  }).locator("p.event-text");
  await expect(comment).toHaveCount(1);
  await comment.scrollIntoViewIfNeeded();
  await expect(comment).toBeVisible();
  expect(await comment.textContent()).toBe(body);
  await expect(comment.locator("*")).toHaveCount(0);
  await expect(comment).toHaveCSS("white-space", "pre-wrap");
  await expect(comment).toHaveCSS("overflow-wrap", "anywhere");
  await expect.poll(() => comment.evaluate(
    (element) => element.scrollWidth <= element.clientWidth + 1,
  )).toBe(true);
}
async function pressHistory(page: Page, name: string) {
  const button = history(page).getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.focus();
  await page.keyboard.press("Enter");
}

test("real persisted history reads recent, older and refreshed pages [recent-older-and-refresh]", async ({ page }) => {
  const data = timelineMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{
    name: "agenteam_local_session",
    value: data.cookie,
    url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
    httpOnly: true,
    sameSite: "Lax",
    secure: false,
  }]);
  const seen = await observeTimeline(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(drawer(page).getByRole("heading", { name: data.title, exact: true })).toBeVisible();
  await expect(drawer(page).locator("dl.task-facts > div").filter({
    has: page.getByText("版本", { exact: true }),
  }).locator("dd")).toHaveText(data.version);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(items(page)).toHaveCount(50);
  await expect(history(page).getByRole("button", { name: "加载较早历史", exact: true })).toBeEnabled();
  expect(await eventIDs(page)).toEqual(data.event_ids.slice(1));
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(1);

  await pressHistory(page, "加载较早历史");
  await expect(items(page)).toHaveCount(51);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(history(page).getByRole("button", { name: "加载较早历史", exact: true })).toHaveCount(0);
  await expect(history(page).getByRole("heading", { name: "任务历史", exact: true })).toBeFocused();
  expect(await eventIDs(page)).toEqual(data.event_ids);
  await expect(items(page).first().getByRole("heading", { name: "创建任务", exact: true })).toBeVisible();
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(2);
  await timelineScreenshot(page, data, "1440-light");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await pressHistory(page, "重新读取历史");
  await expect(items(page)).toHaveCount(50);
  await expect(history(page)).toHaveAttribute("aria-busy", "false");
  await expect(history(page).getByRole("button", { name: "加载较早历史", exact: true })).toBeEnabled();
  await expect(history(page).getByRole("button", { name: "重新读取历史", exact: true })).toBeFocused();
  expect(await eventIDs(page)).toEqual(data.event_ids.slice(1));
  await fullComment(page, data.comment);
  await seen.verify();
  seen.requirePages(3);
  await timelineScreenshot(page, data, "390-dark");
});
