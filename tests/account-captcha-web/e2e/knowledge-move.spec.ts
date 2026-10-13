import { test, expect, type Page } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  moveMaterial,
  observeMove,
  moveScreenshot,
  moveFailure,
  type MoveMaterial,
} from "./knowledge-move.helpers";
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeMove>>
>();
type Stage =
  | "material"
  | "initial-read"
  | "choose-parent"
  | "move-parent"
  | "parent-read"
  | "light-image"
  | "move-root"
  | "root-read"
  | "unknown"
  | "lookup"
  | "dark-image"
  | "complete";
const stages = new WeakMap<Page, Stage>();
const materials = new WeakMap<Page, MoveMaterial>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    // Capture only the failed view before retiring the original consumers.
    if (info.status !== "passed") {
      try {
        await moveFailure(page, materials.get(page));
      } catch {
        // Observation failure cannot bypass original consumer/resource cleanup.
      }
    }
  } finally {
    try {
      await seen?.retire();
    } finally {
      seen?.save(info.status);
      const locations = info.errors.flatMap((error) =>
        [
          ...(error.stack ?? "").matchAll(
            /\b((?:knowledge-move\.(?:spec|helpers)|task-review\.helpers)\.ts):(\d+):(\d+)/g,
          ),
        ].map((match) => ({
          file: match[1],
          line: Number(match[2]),
          column: Number(match[3]),
        })),
      );
      writeFileSync(
        join(
          process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_EVIDENCE!,
          `knowledge-move-${process.env.AGENTEAM_KNOWLEDGE_MOVE_WEB_CASE}-diagnostic.json`,
        ),
        JSON.stringify(
          {
            stage: stages.get(page) ?? "material",
            test_status: info.status,
            locations,
          },
          null,
          2,
        ) + "\n",
        { mode: 0o600 },
      );
    }
  }
});
async function press(page: Page, name: string) {
  const scope =
    name === "关闭"
      ? page
          .getByRole("dialog", { name: "移动文档", exact: true })
          .locator("footer")
      : page;
  const button = scope.getByRole("button", { name, exact: true });
  await expect(button).toHaveCount(1);
  await expect(button).toBeEnabled();
  await button.focus();
  await expect(button).toBeFocused();
  await page.keyboard.press("Enter");
}
async function open(page: Page) {
  stages.set(page, "material");
  const data = moveMaterial();
  materials.set(page, data);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: data.origin,
      httpOnly: true,
      sameSite: "Lax",
      secure: false,
    },
  ]);
  const seen = await observeMove(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  stages.set(page, "initial-read");
  await expect(
    page.getByRole("heading", { name: data.document.title, exact: true }),
  ).toBeVisible();
  await expect(page.locator('[aria-label="当前文档正文"]')).toHaveText(
    data.text,
  );
  await expect(
    page.getByRole("button", { name: "移动", exact: true }),
  ).toBeEnabled();
  await seen.verify();
  seen.current(0);
  return { data, seen };
}
async function moveParent(page: Page) {
  stages.set(page, "choose-parent");
  await press(page, "移动");
  await expect(
    page.getByRole("dialog", { name: "移动文档", exact: true }),
  ).toBeVisible();
  await press(page, "选择 目标父文档 为父文档");
  await expect(
    page.getByRole("dialog", { name: "移动文档", exact: true }),
  ).toContainText("目标位置：目标父文档");
  stages.set(page, "move-parent");
  await press(page, "确认移动");
}
async function confirmed(page: Page) {
  await expect(
    page.getByText("移动已确认；已读取当前文档与位置。", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "采用当前位置", exact: true }),
  ).toBeEnabled();
}
async function dark(page: Page) {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
}
test("moves a document under another document and back to root [move-normal]", async ({
  page,
}) => {
  const { data, seen } = await open(page);
  await moveParent(page);
  stages.set(page, "parent-read");
  await confirmed(page);
  await seen.verify();
  seen.current(1);
  expect(seen.sourceReads()).toBe(2);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  stages.set(page, "light-image");
  await moveScreenshot(page, data, "1440-light-parent");
  await press(page, "关闭");
  await expect(
    page.getByRole("dialog", { name: "移动文档", exact: true }),
  ).toHaveCount(0);
  await dark(page);
  stages.set(page, "move-root");
  await press(page, "移动");
  await press(page, "移到根层");
  await press(page, "确认移动");
  stages.set(page, "root-read");
  await confirmed(page);
  await seen.verify();
  seen.current(2);
  expect(seen.sourceReads()).toBe(3);
  stages.set(page, "dark-image");
  await moveScreenshot(page, data, "390-dark-root");
  await press(page, "关闭");
  await seen.endDocument();
  stages.set(page, "complete");
});
test("looks up a physically lost Move receipt with its original complete intent then reads current position [move-lost]", async ({
  page,
}) => {
  const { data, seen } = await open(page);
  await moveParent(page);
  stages.set(page, "unknown");
  await expect(
    page.getByText("移动结果尚不确定，请查询原结果；不会自动重发。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认移动", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByText("移动已确认；已读取当前文档与位置。", { exact: true }),
  ).toHaveCount(0);
  await seen.verify();
  expect(seen.sourceReads()).toBe(1);
  expect(seen.requests("move")).toHaveLength(1);
  expect(seen.requests("lookup")).toHaveLength(0);
  stages.set(page, "lookup");
  await press(page, "查询原移动结果");
  await confirmed(page);
  await seen.verify();
  seen.current(1);
  expect(seen.sourceReads()).toBe(2);
  await dark(page);
  stages.set(page, "dark-image");
  await moveScreenshot(page, data, "390-dark-confirmed");
  await press(page, "关闭");
  await seen.endDocument();
  stages.set(page, "complete");
});
