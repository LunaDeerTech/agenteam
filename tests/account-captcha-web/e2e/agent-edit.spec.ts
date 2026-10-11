import { test, expect, type Page } from "@playwright/test";
import {
  agentEditMaterial,
  agentEditInputs,
  observeAgentEdit,
  agentEditScreenshot,
} from "./agent-edit.helpers";

const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeAgentEdit>>
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
const configuration = (page: Page) =>
  page.getByRole("region", { name: "Agent 配置", exact: true });
const form = (page: Page) =>
  page.getByRole("form", { name: "编辑 Agent 配置", exact: true });
const field = (page: Page, name: string) =>
  form(page).getByRole("textbox", { name, exact: true });
const fact = (page: Page, label: string) =>
  configuration(page)
    .locator("dt")
    .filter({ hasText: new RegExp(`^${label}$`) })
    .locator("xpath=following-sibling::dd[1]");
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled();
  await button(page, name).focus();
  await page.keyboard.press("Enter");
}
async function layout(
  page: Page,
  width: 1440 | 390,
  colorScheme: "light" | "dark",
) {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
}

async function runAgentEdit(page: Page, lost: boolean) {
  const data = agentEditMaterial();
  expect(data.mode).toBe(
    lost ? "lost-update-confirmation-only-lookup" : "changed-noop-and-clear",
  );
  const inputs = agentEditInputs(data);
  const name = inputs[0]!.request.name!;
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
  const seen = await observeAgentEdit(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const directory = page.getByRole("region", {
    name: "Agent 目录",
    exact: true,
  });
  const entry = directory.getByRole("button").filter({
    has: page
      .locator("strong")
      .filter({ hasText: new RegExp(`^${data.initial.name}$`) }),
  });
  await expect(entry).toHaveCount(1);
  await expect(entry).toBeEnabled();
  await entry.focus();
  await page.keyboard.press("Enter");
  await expect(fact(page, "版本")).toHaveText(data.version);
  await expect(fact(page, "Agent ID")).toHaveText(data.agent_id);
  await expect(configuration(page).locator("dt")).toHaveCount(20);
  await expect(button(page, "编辑配置")).toBeEnabled();
  await seen.verify();
  const initial = seen.agentRead("agent-configuration");
  expect(initial).toMatchObject({
    id: data.agent_id,
    project_id: data.project_id,
    version: data.version,
    ...data.initial,
  });
  seen.requireDirectory(initial);

  // Cancel the original untouched form; wait for the real parent to restore focus.
  await press(page, "编辑配置");
  await expect(field(page, "Agent 名称")).toBeFocused();
  await expect(button(page, "保存配置")).toBeDisabled();
  await press(page, "取消编辑");
  await expect(form(page)).toHaveCount(0);
  await expect(button(page, "编辑配置")).toBeFocused();
  expect(seen.requests("update")).toHaveLength(0);
  await page.keyboard.press("Enter");
  await expect(field(page, "Agent 名称")).toBeFocused();
  await field(page, "Agent 名称").fill(name);
  await field(page, "Agent 描述").fill(inputs[0]!.request.description!);
  if (!lost) await field(page, "指令").fill(inputs[0]!.request.instructions!);
  await expect(button(page, "保存配置")).toBeEnabled();
  await layout(page, 1440, "light");
  await agentEditScreenshot(page, data, "1440-light-edit-form");
  await press(page, "保存配置");
  if (lost) {
    const recovery = page.getByRole("region", {
      name: "Agent 保存结果恢复",
      exact: true,
    });
    await expect(
      recovery.getByText("保存结果待确认。请查询原操作结果，不要重复保存。", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(form(page)).toHaveCount(0);
    await expect(button(page, "保存配置")).toHaveCount(0);
    await expect(button(page, "创建 Agent")).toHaveCount(0);
    await expect(button(page, "编辑配置")).toBeDisabled();
    await seen.verify();
    expect(seen.requests("update")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    await layout(page, 390, "dark");
    await agentEditScreenshot(page, data, "390-dark-update-uncertain");
    await press(page, "查询原保存结果");
  }
  await expect(form(page)).toHaveCount(0);
  await expect(fact(page, "版本")).toHaveText("2");
  await expect(fact(page, "名称")).toHaveText(name);
  await expect(button(page, "编辑配置")).toBeEnabled();
  await seen.verify();
  if (lost) seen.originalLookup();
  const first = lost
    ? seen.receipt("lookup", 0).receipt
    : seen.receipt("update", 0);
  seen.requireReceipt(first, 0, initial);
  let final = first;
  if (!lost) {
    await press(page, "编辑配置");
    await field(page, "Agent 名称").fill(name + "x");
    await field(page, "Agent 名称").fill(name);
    await press(page, "保存配置");
    await expect(form(page)).toHaveCount(0);
    await expect(
      page.getByText("配置未变化，版本保持不变。", { exact: true }),
    ).toBeVisible();
    await expect(fact(page, "版本")).toHaveText("2");
    await seen.verify();
    const noop = seen.receipt("update", 1);
    seen.requireReceipt(noop, 1, first.agent);
    expect(noop.agent).toEqual(first.agent);
    await press(page, "编辑配置");
    await field(page, "Agent 描述").fill("");
    await field(page, "指令").fill("");
    await layout(page, 390, "dark");
    await agentEditScreenshot(page, data, "390-dark-explicit-clear");
    await press(page, "保存配置");
    await expect(form(page)).toHaveCount(0);
    await expect(fact(page, "版本")).toHaveText("3");
    await expect(fact(page, "描述")).toHaveText("未设置");
    await expect(fact(page, "指令")).toHaveText("未设置");
    await seen.verify();
    final = seen.receipt("update", 2);
    seen.requireReceipt(final, 2, noop.agent);
  } else {
    await expect(fact(page, "描述")).toHaveText("未设置");
    expect(await fact(page, "指令").textContent()).toBe(
      data.initial.instructions,
    );
  }

  // Later reads check publication only; original write success was already
  // established from its own public Promise, bytes and committed Go receipt.
  const directories = seen.requests("agent-directory").length;
  const configurations = seen.requests("agent-configuration").length;
  await press(page, "刷新 Agent 目录");
  await expect
    .poll(() => seen.requests("agent-directory").length)
    .toBe(directories + 1);
  await expect
    .poll(() => seen.requests("agent-configuration").length)
    .toBe(configurations + 1);
  await expect(button(page, "刷新 Agent 目录")).toBeEnabled();
  await expect(fact(page, "版本")).toHaveText(lost ? "2" : "3");
  await seen.verify();
  expect(seen.agentRead("agent-configuration")).toEqual(final.agent);
  seen.requireDirectory(final.agent);
  seen.requireCounts();
  await expect(directory).not.toContainText(initial.instructions);
  await layout(page, 390, "dark");
  await agentEditScreenshot(page, data, "390-dark-saved-configuration");
}

test("changed, no-op and explicit clear preserve the original Agent configuration [changed-noop-and-clear]", async ({
  page,
}) => runAgentEdit(page, false));
test("lost Agent update confirmation only looks up its original intent [lost-update-confirmation-only-lookup]", async ({
  page,
}) => runAgentEdit(page, true));
