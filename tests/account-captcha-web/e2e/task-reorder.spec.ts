import { test, expect, type Page } from "@playwright/test";
import {
  taskReorderMaterial,
  observeTaskReorder,
  taskReorderScreenshot,
  type ReorderMaterial,
} from "./task-reorder.helpers";
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeTaskReorder>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const form = (page: Page) =>
  page.getByRole("form", { name: "调整任务顺序", exact: true });
const drawer = (page: Page) =>
  page.getByRole("dialog", { name: "任务详情", exact: true });
const version = (page: Page) =>
  drawer(page)
    .locator("dl.task-facts > div")
    .filter({ has: page.getByText("版本", { exact: true }) })
    .locator("dd");
async function press(page: Page, name: string) {
  const target = button(page, name);
  await expect(target).toBeEnabled();
  await target.focus();
  await expect(target).toBeFocused();
  await page.keyboard.press("Enter");
}
async function choose(page: Page, label: string, option: string) {
  await press(page, label);
  const target = page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true });
  await target.focus();
  await page.keyboard.press("Enter");
  await expect(button(page, label)).toBeFocused();
}
async function open(page: Page, confirmed = false) {
  await press(page, confirmed ? "顺序已确认" : "调整任务顺序");
  await expect(form(page)).toBeVisible();
  await expect(
    form(page).getByRole("heading", { name: "调整任务顺序", exact: true }),
  ).toBeFocused();
  await expect(button(page, "目标任务")).toBeEnabled();
}
async function selectAnchor(page: Page, data: ReorderMaterial) {
  await press(page, "下一页候选");
  await expect(form(page).getByText("第 2 页", { exact: true })).toBeVisible();
  await choose(page, "目标任务", data.rows[51]!.title);
  await expect(button(page, "确认调整顺序")).toBeEnabled();
}
async function layout(
  page: Page,
  width: number,
  colorScheme: "light" | "dark",
) {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  const box = await drawer(page).boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
}
async function verifyCurrentOrder(
  page: Page,
  seen: Awaited<ReturnType<typeof observeTaskReorder>>,
  data: ReorderMaterial,
  tail: boolean,
  expectedVersion: string,
) {
  // The form reads the full original rank group independently of the board's
  // priority lanes, so two real pages establish where the server put this Task.
  await seen.verify();
  const first = seen.result("candidates").items;
  expect(first).toHaveLength(50);
  await press(page, "下一页候选");
  await seen.verify();
  const second = seen.result("candidates").items;
  expect(second).toHaveLength(2);
  const items = [...first, ...second],
    others = data.rows.filter((row) => row.id !== data.task_id);
  expect(items.map((row) => row.id)).toEqual(
    tail
      ? [...others.map((row) => row.id), data.task_id]
      : [
          ...others.slice(0, 50).map((row) => row.id),
          data.task_id,
          data.anchor_id,
        ],
  );
  expect(items.find((row) => row.id === data.task_id)?.version).toBe(
    expectedVersion,
  );
  expect(
    items.every(
      (row) =>
        row.sprint_id === data.sprint_id &&
        row.state === "backlog" &&
        row.priority === "medium",
    ),
  ).toBe(true);
}
async function begin(page: Page) {
  const data = taskReorderMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value: data.cookie,
        url: data.origin,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
  const seen = await observeTaskReorder(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(drawer(page)).toBeVisible();
  await expect(version(page)).toHaveText("1");
  await expect(button(page, "调整任务顺序")).toBeEnabled();
  await seen.verify();
  await open(page);
  await press(page, "取消调整顺序");
  await expect(form(page)).toHaveCount(0);
  await expect(button(page, "调整任务顺序")).toBeFocused();
  expect(seen.requests("reorder")).toHaveLength(0);
  await open(page);
  await press(page, "目标任务");
  let list = page.getByRole("listbox", { name: "目标任务", exact: true });
  await expect(list.getByRole("option")).toHaveText(
    data.rows.slice(1, 50).map((row) => row.title),
  );
  await page.keyboard.press("Escape");
  await choose(page, "目标任务", data.rows[1]!.title);
  await press(page, "下一页候选");
  await expect(button(page, "确认调整顺序")).toBeDisabled();
  await press(page, "目标任务");
  list = page.getByRole("listbox", { name: "目标任务", exact: true });
  await expect(list.getByRole("option")).toHaveText(
    data.rows.slice(50).map((row) => row.title),
  );
  await page.keyboard.press("Escape");
  await choose(page, "目标任务", data.rows[51]!.title);
  await seen.verify();
  await layout(page, 1440, "light");
  await taskReorderScreenshot(page, data, "1440-light-before");
  return { data, seen };
}
test("same-group before, tail and no-op through the original Work service [task-reorder-normal]", async ({
  page,
}) => {
  const { data, seen } = await begin(page);
  expect(data.mode).toBe("task-reorder-normal");
  await press(page, "确认调整顺序");
  await expect(form(page)).toHaveCount(0);
  await expect(version(page)).toHaveText("2");
  await expect(button(page, "顺序已确认")).toBeFocused();
  await seen.verify();
  const first = seen.result("reorder");
  expect(first.changed).toBe(true);
  expect(first.task.id).toBe(data.task_id);
  expect(first.event_ids).toHaveLength(1);
  await open(page, true);
  await verifyCurrentOrder(page, seen, data, false, "2");
  await choose(page, "排序位置", "移到组尾");
  await layout(page, 390, "dark");
  await taskReorderScreenshot(page, data, "390-dark-tail");
  await press(page, "确认调整顺序");
  await expect(form(page)).toHaveCount(0);
  await expect(version(page)).toHaveText("3");
  await seen.verify();
  const second = seen.result("reorder");
  expect(second.changed).toBe(true);
  expect(second.task.manual_rank).not.toBe(first.task.manual_rank);
  await open(page, true);
  await verifyCurrentOrder(page, seen, data, true, "3");
  await choose(page, "排序位置", "移到组尾");
  await press(page, "确认调整顺序");
  await expect(form(page)).toHaveCount(0);
  await expect(version(page)).toHaveText("3");
  await seen.verify();
  const noop = seen.result("reorder");
  expect(noop.changed).toBe(false);
  expect(noop.task_event_id).toBeNull();
  expect(noop.event_ids).toEqual([]);
  expect(noop.task).toEqual(second.task);
  await open(page, true);
  await verifyCurrentOrder(page, seen, data, true, "3");
  await press(page, "取消调整顺序");
  await expect(button(page, "顺序已确认")).toBeFocused();
  expect(seen.requests("reorder")).toHaveLength(3);
  expect(seen.requests("lookup")).toHaveLength(0);
  await taskReorderScreenshot(page, data, "390-dark-confirmed");
});
test("real version conflict, explicit adoption, lost receipt and original Lookup [task-reorder-recovery]", async ({
  page,
}) => {
  const { data, seen } = await begin(page);
  expect(data.mode).toBe("task-reorder-recovery");
  await press(page, "确认调整顺序");
  await expect(
    form(page).getByText(
      "任务已发生变化，原版本和排序选择已保留。请重新读取并核对，再明确采用当前版本。",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(version(page)).toHaveText("1");
  await expect(button(page, "确认调整顺序")).toBeDisabled();
  await seen.verify();
  expect(seen.requests("reorder")).toHaveLength(1);
  expect(seen.requests("lookup")).toHaveLength(0);
  await press(page, "重新读取排序对象");
  await expect(version(page)).toHaveText("2");
  await expect(button(page, "确认调整顺序")).toBeDisabled();
  await press(page, "使用当前版本继续排序");
  await selectAnchor(page, data);
  expect(seen.requests("reorder")).toHaveLength(1);
  await layout(page, 390, "dark");
  await taskReorderScreenshot(page, data, "390-dark-adopted");
  await press(page, "确认调整顺序");
  await expect(
    drawer(page).getByText("提交结果待确认。请查询原操作结果，不要重复提交。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(form(page)).toHaveCount(0);
  await expect(version(page)).toHaveText("2");
  await seen.verify();
  expect(seen.requests("reorder")).toHaveLength(2);
  expect(seen.requests("lookup")).toHaveLength(0);
  await taskReorderScreenshot(page, data, "390-dark-uncertain");
  await press(page, "查询原操作结果");
  await expect(version(page)).toHaveText("3");
  await expect(button(page, "顺序已确认")).toBeFocused();
  await seen.verify();
  expect(seen.result("lookup").status).toBe("committed");
  expect(seen.result("lookup").receipt.task.version).toBe("3");
  await open(page, true);
  await verifyCurrentOrder(page, seen, data, false, "3");
  await press(page, "取消调整顺序");
  expect(seen.requests("reorder")).toHaveLength(2);
  expect(seen.requests("lookup")).toHaveLength(1);
  await taskReorderScreenshot(page, data, "390-dark-confirmed");
});
