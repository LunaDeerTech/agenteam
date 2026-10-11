import { test, expect, type Page } from "@playwright/test";
import {
  agentMaterial, observeAgentConfiguration, agentScreenshot, directoryKeys,
} from "./agent-configuration.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeAgentConfiguration>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try {
    await seen?.retire();
  } finally {
    seen?.save(info.status);
  }
});
const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
const configuration = (page: Page) => page.getByRole("region", { name: "Agent 配置", exact: true });
const fact = (page: Page, label: string) => configuration(page)
  .locator("dt").filter({ hasText: new RegExp(`^${label}$`) }).locator("xpath=following-sibling::dd[1]");

async function selectWithKeyboard(page: Page, label: string, option: string) {
  const trigger = button(page, label);
  await trigger.focus();
  await page.keyboard.press("Enter");
  const selected = page.getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true });
  await selected.focus();
  await page.keyboard.press("Enter");
  await expect(trigger).toBeFocused();
}
async function layout(page: Page, width: number, colorScheme: "light" | "dark") {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  await expect.poll(() => page.evaluate(() => ({
    overflow: document.documentElement.scrollWidth > innerWidth + 1,
    reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
  }))).toEqual({ overflow: false, reduced: true });
  const region = page.getByRole("region", { name: "项目 Agent 设置", exact: true });
  await expect(region).toBeVisible();
  const box = await region.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
}

async function runAgentCreation(page: Page, lost: boolean) {
  const data = agentMaterial();
  expect(data.mode).toBe(lost
    ? "lost-create-confirmation-only-lookup" : "default-create-config-and-directory");
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{
    name: "agenteam_local_session", value: data.cookie,
    url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!, httpOnly: true,
    sameSite: "Lax", secure: false,
  }]);
  const seen = await observeAgentConfiguration(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  await expect(button(page, "创建 Agent")).toBeEnabled();
  await seen.verify();
  const directory = page.getByRole("region", { name: "Agent 目录", exact: true });
  await expect(directory).toBeVisible();
  await layout(page, 1440, "light");
  await agentScreenshot(page, data, "1440-light-directory");

  // Cancel the actual empty form and wait for the parent's focus restoration.
  await button(page, "创建 Agent").focus();
  await page.keyboard.press("Enter");
  const form = page.getByRole("form", { name: "创建 Agent", exact: true });
  const name = form.getByRole("textbox", { name: "Agent 名称", exact: true });
  await expect(name).toBeFocused();
  await expect(button(page, "确认创建 Agent")).toBeDisabled();
  await expect(button(page, "取消创建")).toBeEnabled();
  await button(page, "取消创建").focus();
  await page.keyboard.press("Enter");
  await expect(form).toHaveCount(0);
  await expect(button(page, "创建 Agent")).toBeFocused();
  expect(seen.requests("create")).toHaveLength(0);
  await page.keyboard.press("Enter");
  await expect(name).toBeFocused();
  await name.fill(data.draft.name);
  await form.getByRole("textbox", { name: "Agent 描述", exact: true }).fill(data.draft.description);
  await form.getByRole("textbox", { name: "指令", exact: true }).fill(data.draft.instructions);
  await expect(button(page, "刷新可用模型")).toBeEnabled();
  await seen.verify();
  await selectWithKeyboard(page, "主模型", seen.modelOption());
  await expect(button(page, "确认创建 Agent")).toBeDisabled();
  await selectWithKeyboard(page, "AGENTS.md 注入", "不注入 AGENTS.md");
  await selectWithKeyboard(page, "审批策略", "默认审批");
  await expect(form.getByRole("checkbox", { name: "添加默认 Skills", exact: true })).toBeChecked();
  await expect(form.getByRole("checkbox", { name: "启用 Install Skill 工具", exact: true })).toBeChecked();
  await expect(button(page, "确认创建 Agent")).toBeEnabled();
  await agentScreenshot(page, data, "1440-light-create-form");
  await button(page, "确认创建 Agent").focus();
  await page.keyboard.press("Enter");

  if (lost) {
    const recovery = page.getByRole("region", { name: "Agent 创建结果恢复", exact: true });
    await expect(recovery.getByText("创建结果待确认。请查询原操作结果，不要重复创建。", { exact: true })).toBeVisible();
    await expect(form).toHaveCount(0);
    await expect(button(page, "创建 Agent")).toHaveCount(0);
    await expect(configuration(page).locator("dl")).toHaveCount(0);
    await seen.verify();
    expect(seen.requests("create")).toHaveLength(1);
    expect(seen.requests("lookup")).toHaveLength(0);
    await layout(page, 390, "dark");
    await agentScreenshot(page, data, "390-dark-create-uncertain");
    await expect(button(page, "查询原创建结果")).toBeEnabled();
    await button(page, "查询原创建结果").focus();
    await page.keyboard.press("Enter");
  }
  await expect(fact(page, "名称")).toHaveText(data.draft.name);
  await expect(fact(page, "版本")).toHaveText("1");
  await expect(button(page, "创建 Agent")).toBeEnabled();
  await seen.verify();
  if (lost) seen.originalLookup();
  const receipt = lost ? seen.receipt("lookup", 0).receipt : seen.receipt("create", 0);
  expect(Object.keys(receipt).sort()).toEqual(["agent", "changed", "event_ids"]);
  expect(receipt.changed).toBe(true);
  expect(receipt.event_ids.length).toBeGreaterThan(0);
  seen.requireConfiguration(receipt.agent);
  await expect(configuration(page).locator("dt")).toHaveCount(20);
  await expect(fact(page, "Agent ID")).toHaveText(seen.agentID());
  await expect(fact(page, "主模型 ID")).toHaveText(data.model_id);
  await expect(fact(page, "指令")).toHaveText(data.draft.instructions);
  await expect(fact(page, "AGENTS.md 注入")).toHaveText("停用");
  await expect(fact(page, "审批策略")).toHaveText("默认审批");
  await expect(fact(page, "Tool 引用")).toHaveText(receipt.agent.allowed_tool_ids[0]);
  await expect(directory).not.toContainText(data.draft.instructions);
  await expect(directory).not.toContainText(receipt.agent.allowed_tool_ids[0]);

  // A public refresh must read both actual Directory9 and Config20. Neither
  // this later GET nor the DOM is used as proof of the original Create Promise.
  const directoryCount = seen.requests("agent-directory").length;
  await button(page, "刷新 Agent 目录").focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => seen.requests("agent-directory").length).toBeGreaterThan(directoryCount);
  await expect.poll(() => seen.requests("agent-configuration").length).toBe(1);
  await expect(button(page, "刷新 Agent 目录")).toBeEnabled();
  await expect(fact(page, "版本")).toHaveText("1");
  await seen.verify();
  const read = seen.agentRead("agent-configuration");
  seen.requireConfiguration(read);
  expect(read).toEqual(receipt.agent);
  const rows = seen.agentRead("agent-directory").items.filter((item: any) => item.id === seen.agentID());
  expect(rows).toHaveLength(1);
  expect(Object.keys(rows[0]).sort()).toEqual(directoryKeys);
  for (const key of directoryKeys) expect(rows[0][key]).toEqual(receipt.agent[key]);
  await expect(directory.getByRole("button").filter({ hasText: data.draft.name })).toHaveCount(1);
  await layout(page, 390, "dark");
  await agentScreenshot(page, data, "390-dark-created-configuration");
  expect(seen.requests("create")).toHaveLength(1);
  expect(seen.requests("lookup")).toHaveLength(lost ? 1 : 0);
  await seen.endDocument();
  seen.save("passed-before-owned-retirement");
}

test("default Agent creation and real configuration [default-create-config-and-directory]", async ({ page }) => {
  await runAgentCreation(page, false);
});
test("lost Create receipt retains only original Lookup [lost-create-confirmation-only-lookup]", async ({ page }) => {
  await runAgentCreation(page, true);
});
