import { test, expect, type Page, type Locator } from "@playwright/test";
import {
  executionUsageMaterial,
  observeExecutionUsage,
  executionUsageScreenshot,
  summaryFields,
  summaryCounts,
} from "./usage-execution-filter.helpers";
import type { UsageRow } from "./project-usage.helpers";

// Actual default app.Run reads; persisted fixture Runtime Facts and owned wire
// preparation are not production Execution/LLM execution or a fault matrix.
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeExecutionUsage>>
>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
async function press(page: Page, button: Locator) {
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}

test("filters one Execution across pages, full daily summary, refresh and clear [execution-filter-current-read]", async ({
  page,
}) => {
  const data = executionUsageMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "__Host-agenteam_session",
        value: data.cookie,
        url: data.origin,
        httpOnly: true,
        sameSite: "Lax",
        secure: true,
      },
    ]);
  const seen = await observeExecutionUsage(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const region = page.getByRole("region", {
    name: "项目模型调用记录",
    exact: true,
  });
  const list = region.getByRole("list", { name: "调用记录列表", exact: true });
  const button = (name: string) =>
    region.getByRole("button", { name, exact: true });
  const previous = button("上一页"),
    next = button("下一页"),
    detail = region.locator(".usage-detail");
  const filter = region.getByRole("group", { name: "执行筛选", exact: true });
  const heading = (name: string) =>
    region.getByRole("heading", { name, exact: true });
  async function rows(expected: UsageRow[]) {
    await expect(list.getByRole("listitem")).toHaveCount(expected.length);
    const actual = await list
      .getByRole("listitem")
      .evaluateAll((items) =>
        items.map((item) => ({
          title: item
            .querySelector('.button-label[aria-hidden="false"]')
            ?.textContent?.replace(/\s+/g, " ")
            .trim(),
          time: item.querySelector("time")?.textContent?.trim(),
          tokens: Array.from(item.querySelectorAll(".usage-tokens dd")).map(
            (el) => el.textContent?.trim(),
          ),
        })),
      );
    expect(actual).toEqual(
      expected.map((row) => ({
        title: `${row.model_name} · ${row.purpose === "approval_auto" ? "自动审批" : "Agent 生成"} · 第 ${row.attempt_index} 次尝试`,
        time: row.started_at,
        tokens: [
          row.usage.input_tokens ?? "未知",
          row.usage.output_tokens ?? "未知",
          row.usage.total_tokens ?? "未知",
        ],
      })),
    );
  }
  async function read(index: number, expected?: UsageRow[]) {
    if (expected) await rows(expected);
    await seen.verify();
    return seen.read(index);
  }
  async function scope(active: boolean) {
    if (active) {
      await expect(filter).toBeVisible();
      await expect(filter.locator(":scope > p > span")).toHaveText(data.execution_id);
      await expect(button("清除执行筛选")).toBeEnabled();
    } else await expect(filter).toHaveCount(0);
  }
  async function daily(expected: Record<string, any>[], index: number) {
    const daily = region.getByRole("region", {
      name: "按日用量汇总",
      exact: true,
    });
    await expect(daily.getByRole("article")).toHaveCount(2);
    const body = await read(index);
    await expect(daily.locator(":scope > p time")).toHaveText(body.as_of);
    for (const [i, row] of expected.entries()) {
      const article = daily.getByRole("article").nth(i);
      await expect(article).toHaveAttribute(
        "aria-label",
        row.key.day + " 用量汇总",
      );
      await expect(article.getByRole("heading")).toHaveText(
        row.key.day + "（UTC）",
      );
      await expect(article.locator(".usage-day-counts dd")).toHaveText(
        summaryCounts.map((field) => row.summary[field]),
      );
      const actual = await article
        .locator("tbody tr")
        .evaluateAll((rows) =>
          rows.map((row) =>
            Array.from(row.querySelectorAll("td")).map((cell) =>
              cell.textContent?.trim(),
            ),
          ),
        );
      expect(actual).toEqual(
        summaryFields.map((field) => [
          row.summary[field].sum ?? "未知",
          row.summary[field].known_count,
          row.summary[field].unknown_count,
        ]),
      );
    }
    return daily;
  }
  await read(0, data.items.slice(0, 50));
  await scope(false);
  await expect(previous).toBeDisabled();
  await expect(next).toBeEnabled();
  await press(page, next);
  await read(1, data.items.slice(50));
  await expect(previous).toBeEnabled();
  await expect(next).toBeDisabled();
  await expect(heading("调用明细")).toBeFocused();
  // The unassociated Tool record must not manufacture an execution filter.
  const tail = data.items.slice(50),
    nullIndex = tail.findIndex((row) => row.execution_id === null);
  expect(nullIndex).toBeGreaterThanOrEqual(0);
  const nullChoice = list
    .getByRole("listitem")
    .nth(nullIndex)
    .getByRole("button");
  await press(page, nullChoice);
  await expect(detail.getByRole("heading")).toBeFocused();
  await expect(button("查看同一次执行的调用")).toHaveCount(0);
  await press(page, button("返回调用列表"));
  await expect(nullChoice).toBeFocused();
  const targetIndex = tail.findIndex(
    (row) => row.execution_id === data.execution_id,
  );
  expect(targetIndex).toBeGreaterThanOrEqual(0);
  await press(
    page,
    list.getByRole("listitem").nth(targetIndex).getByRole("button"),
  );
  await expect(detail.getByRole("heading")).toBeFocused();
  await expect(detail).toContainText(data.execution_id);
  await press(page, button("查看同一次执行的调用"));
  await expect(detail).toHaveCount(0);
  await expect(heading("调用明细")).toBeFocused();
  await read(2, data.target_items.slice(0, 50));
  await scope(true);
  await expect(previous).toBeDisabled();
  await expect(next).toBeEnabled();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await filter.scrollIntoViewIfNeeded();
  await executionUsageScreenshot(page, "1440-light-filtered");
  await press(page, next);
  await read(3, data.target_items.slice(50));
  await scope(true);
  await expect(next).toBeDisabled();
  await expect(previous).toBeEnabled();
  await press(page, list.getByRole("listitem").first().getByRole("button"));
  await expect(detail).toContainText(data.execution_id);
  await expect(button("查看同一次执行的调用")).toBeDisabled();
  await press(page, button("返回调用列表"));
  await expect(
    list.getByRole("listitem").first().getByRole("button"),
  ).toBeFocused();
  await press(page, button("按日汇总"));
  await expect(heading("按日汇总")).toBeFocused();
  const summary = await daily(data.target_summary, 4);
  await scope(true);
  // Day counts 25+26 prove all 51 records, including the second detail page.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await filter.scrollIntoViewIfNeeded();
  await executionUsageScreenshot(page, "390-dark-filtered-summary");
  const scroll = summary.locator(".usage-summary-scroll").first();
  await expect(scroll).toHaveAttribute("tabindex", "0");
  await scroll.scrollIntoViewIfNeeded();
  await scroll.focus();
  await expect(scroll).toBeFocused();
  const geometry = await scroll.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const separated = Array.from(element.querySelectorAll("tr")).every(
      (row) => {
        const cells = Array.from(row.querySelectorAll("th,td")).map((cell) => {
          const range = document.createRange();
          range.selectNodeContents(cell);
          return range.getBoundingClientRect();
        });
        return cells.every(
          (cell, i) =>
            cell.width > 0 && (i === 0 || cells[i - 1]!.right <= cell.left + 1),
        );
      },
    );
    return {
      contained: box.left >= -1 && box.right <= innerWidth + 1,
      overflow: element.scrollWidth > element.clientWidth,
      separated,
    };
  });
  expect(geometry).toEqual({
    contained: true,
    overflow: true,
    separated: true,
  });
  await page.keyboard.press("ArrowRight");
  await expect
    .poll(() => scroll.evaluate((element) => element.scrollLeft))
    .toBeGreaterThan(0);
  await executionUsageScreenshot(page, "390-dark-summary-scrolled");
  await press(page, button("调用明细"));
  await read(5, data.target_items.slice(0, 50));
  await expect(previous).toBeDisabled();
  await scope(true);
  await press(page, next);
  await read(6, data.target_items.slice(50));
  await press(page, previous);
  await read(7, data.target_items.slice(0, 50));
  await expect(heading("调用明细")).toBeFocused();
  await press(page, button("刷新调用记录"));
  await read(8, data.target_items.slice(0, 50));
  await scope(true);
  await expect(previous).toBeDisabled();
  await press(page, button("按日汇总"));
  await daily(data.target_summary, 9);
  await scope(true);
  await press(page, button("清除执行筛选"));
  await expect(heading("按日汇总")).toBeFocused();
  await daily(data.all_summary, 10);
  await scope(false);
  await press(page, button("调用明细"));
  await read(11, data.items.slice(0, 50));
  await expect(previous).toBeDisabled();
  await expect(next).toBeEnabled();
  await press(page, next);
  await read(12, data.items.slice(50));
  await scope(false);
  await expect(next).toBeDisabled();
  await heading("调用明细").scrollIntoViewIfNeeded();
  await executionUsageScreenshot(page, "390-dark-cleared-controls");
  await seen.finish();
});
