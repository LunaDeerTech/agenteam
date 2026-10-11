import { test, expect, type Page } from "@playwright/test";
import {
  modelPolicyMaterial,
  observeModelPolicy,
  modelPolicyScreenshot,
} from "./agent-model-policy.helpers";
const observations = new WeakMap<
  Page,
  Awaited<ReturnType<typeof observeModelPolicy>>
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
async function select(page: Page, label: string, option: string) {
  await press(page, label);
  const selected = page
    .getByRole("listbox", { name: label, exact: true })
    .getByRole("option", { name: option, exact: true });
  await selected.focus();
  await page.keyboard.press("Enter");
  await expect(button(page, label)).toBeFocused();
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
}
test("real model references switch atomically and clear approval [switch-model-auto-and-clear-approval]", async ({
  page,
}) => {
  const data = modelPolicyMaterial();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page
    .context()
    .addCookies([
      {
        name: "agenteam_local_session",
        value: data.cookie,
        url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
        httpOnly: true,
        sameSite: "Lax",
        secure: false,
      },
    ]);
  const seen = await observeModelPolicy(page, data);
  observations.set(page, seen);
  await page.goto(data.route);
  const directory = page.getByRole("region", {
    name: "Agent 目录",
    exact: true,
  });
  const entry = directory
    .getByRole("button")
    .filter({
      has: page.locator("strong").filter({ hasText: /^model-policy-agent$/ }),
    });
  await expect(entry).toHaveCount(1);
  await entry.focus();
  await page.keyboard.press("Enter");
  await expect(fact(page, "版本")).toHaveText("1");
  await expect(configuration(page).locator("dt")).toHaveCount(20);
  await seen.verify();
  const initial = seen.agentRead("agent-configuration");
  expect(initial).toMatchObject({
    id: data.agent_id,
    project_id: data.project_id,
    version: "1",
    ...data.initial,
    model_ref: data.old_model_id,
    reasoning_effort: null,
    inject_agents_md: true,
    approval_policy: "default",
    approval_model_ref: null,
  });
  seen.requireDirectory(initial);
  await press(page, "编辑配置");
  await expect(
    form(page).getByRole("textbox", { name: "Agent 名称", exact: true }),
  ).toBeFocused();
  await expect(button(page, "主模型")).toBeEnabled();
  await seen.verify();
  await select(page, "主模型", seen.modelOption(data.new_model_id));
  await select(page, "AGENTS.md 注入", "不注入 AGENTS.md");
  await select(page, "审批策略", "模型自动审批");
  await select(page, "审批模型", seen.modelOption(data.old_model_id));
  await layout(page, 1440, "light");
  await modelPolicyScreenshot(page, "1440-light-auto-form");
  await press(page, "保存配置");
  await expect(form(page)).toHaveCount(0);
  await expect(fact(page, "版本")).toHaveText("2");
  await button(page, "编辑配置").focus();
  await expect(button(page, "编辑配置")).toBeFocused();
  await seen.verify();
  const first = seen.receipt("update", 0);
  seen.requireReceipt(first, 0, initial);
  await expect(fact(page, "主模型 ID")).toHaveText(data.new_model_id);
  await expect(fact(page, "审批模型 ID")).toHaveText(data.old_model_id);
  await expect(fact(page, "AGENTS.md 注入")).toHaveText("停用");
  await press(page, "编辑配置");
  await expect(button(page, "审批策略")).toBeEnabled();
  await seen.verify();
  await select(page, "审批策略", "默认审批");
  await expect(
    form(page).getByRole("button", { name: "审批模型", exact: true }),
  ).toHaveCount(0);
  await press(page, "保存配置");
  await expect(form(page)).toHaveCount(0);
  await expect(fact(page, "版本")).toHaveText("3");
  await seen.verify();
  const final = seen.receipt("update", 1);
  seen.requireReceipt(final, 1, first.agent);
  expect(final.agent).toMatchObject({
    ...data.initial,
    model_ref: data.new_model_id,
    reasoning_effort: null,
    inject_agents_md: false,
    approval_policy: "default",
    approval_model_ref: null,
  });
  const dirs = seen.requests("agent-directory").length,
    configs = seen.requests("agent-configuration").length;
  await press(page, "刷新 Agent 目录");
  await expect
    .poll(() => seen.requests("agent-directory").length)
    .toBe(dirs + 1);
  await expect
    .poll(() => seen.requests("agent-configuration").length)
    .toBe(configs + 1);
  await expect(button(page, "刷新 Agent 目录")).toBeEnabled();
  await seen.verify();
  expect(seen.agentRead("agent-configuration")).toEqual(final.agent);
  seen.requireDirectory(final.agent);
  seen.requireCounts();
  expect(await fact(page, "指令").textContent()).toBe(
    data.initial.instructions,
  );
  await expect(directory).not.toContainText(data.initial.instructions);
  await layout(page, 390, "dark");
  await modelPolicyScreenshot(page, "390-dark-default-configuration");
});
