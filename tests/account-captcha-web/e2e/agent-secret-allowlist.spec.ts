import { test, expect, type Page, type Locator } from "@playwright/test";
import { agentSecretMaterial, agentSecretInputs, observeAgentSecret, agentSecretScreenshot } from "./agent-secret-allowlist.helpers";

const observations = new WeakMap<Page, Awaited<ReturnType<typeof observeAgentSecret>>>();
test.afterEach(async ({ page }, info) => {
  const seen = observations.get(page);
  try { await seen?.retire(); } finally { seen?.save(info.status, info.errors); }
});
const button = (page: Page, name: string) => page.getByRole("button", { name, exact: true });
const configuration = (page: Page) => page.getByRole("region", { name: "Agent 配置", exact: true });
const editor = (page: Page) => page.getByRole("form", { name: "编辑 Agent 配置", exact: true });
const secrets = (form: Locator) => form.getByRole("region", { name: "Secret Variable 白名单", exact: true });
const fact = (page: Page, label: string) => configuration(page).locator("dt").filter({ hasText: new RegExp(`^${label}$`) }).locator("xpath=following-sibling::dd[1]");
async function press(page: Page, name: string) {
  await expect(button(page, name)).toBeEnabled(); await button(page, name).focus(); await page.keyboard.press("Enter");
}
async function select(page: Page, label: string, option: string) {
  const trigger = button(page, label); await expect(trigger).toBeEnabled(); await trigger.focus(); await page.keyboard.press("Enter");
  const choice = page.getByRole("listbox", { name: label, exact: true }).getByRole("option", { name: option, exact: true });
  await expect(choice).toBeVisible(); await choice.focus(); await page.keyboard.press("Enter"); await expect(trigger).toBeFocused();
}
async function layout(page: Page, width: 1440 | 390, colorScheme: "light" | "dark") {
  await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
  await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(true);
}
async function run(page: Page, lost: boolean) {
  const data = agentSecretMaterial(), input = agentSecretInputs(data), first = data.secrets[0]!, second = data.secrets[1]!;
  expect(data.mode).toBe(lost ? "secret-allowlist-lost" : "secret-allowlist-normal");
  await page.setViewportSize({ width: 1440, height: 960 }); await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.context().addCookies([{ name: "agenteam_local_session", value: data.cookie, url: data.origin, httpOnly: true, sameSite: "Lax", secure: false }]);
  const seen = await observeAgentSecret(page, data); observations.set(page, seen);
  await page.goto(data.route);
  const directory = page.getByRole("region", { name: "Agent 目录", exact: true });
  await expect(directory).toBeVisible(); await expect(button(page, "创建 Agent")).toBeEnabled();
  await seen.verify(); expect(seen.agentSecretRead("session").user.role).toBe("user");
  let before: Record<string, any>;
  if (!lost) {
    seen.stage("create"); await press(page, "创建 Agent");
    const form = page.getByRole("form", { name: "创建 Agent", exact: true });
    await expect(form.getByRole("textbox", { name: "Agent 名称", exact: true })).toBeFocused();
    await press(page, "取消创建"); await expect(form).toHaveCount(0); await expect(button(page, "创建 Agent")).toBeFocused();
    expect(seen.requests("create")).toHaveLength(0); await page.keyboard.press("Enter");
    await press(page, "重新读取 Secret Variable 目录"); await expect(button(page, "重新读取 Secret Variable 目录")).toBeEnabled(); await seen.verify(); seen.secrets();
    await form.getByRole("textbox", { name: "Agent 名称", exact: true }).fill(data.draft.name);
    await form.getByRole("textbox", { name: "Agent 描述", exact: true }).fill(data.draft.description);
    await form.getByRole("textbox", { name: "指令", exact: true }).fill(data.draft.instructions);
    await select(page, "主模型", seen.modelOption()); await select(page, "AGENTS.md 注入", "不注入 AGENTS.md"); await select(page, "审批策略", "默认审批");
    await expect(form.getByRole("checkbox", { name: "添加默认 Skills", exact: true })).toBeChecked();
    await expect(form.getByRole("checkbox", { name: "启用 Install Skill 工具", exact: true })).toBeChecked();
    await select(page, "添加 Secret Variable", first.name); await press(page, "添加到白名单");
    await expect(secrets(form)).toContainText(first.id); await expect(button(page, `移除 ${first.name}`)).toBeEnabled();
    await secrets(form).scrollIntoViewIfNeeded(); await agentSecretScreenshot(page, data.mode, "1440-light-create");
    await press(page, "确认创建 Agent"); await expect(form).toHaveCount(0);
    await expect(fact(page, "版本")).toHaveText("1"); await expect(fact(page, "Secret Variable 引用")).toContainText(first.id); await expect(fact(page, "Secret Variable 引用")).toContainText(first.name);
    await expect(button(page, "编辑配置")).toBeEnabled(); await seen.verify();
    const receipt = seen.receipt("create", 0); seen.requireReceipt(receipt, "1", input.create.allowed_secret_variable_ids);
    before = seen.current("1", [first.id]); expect(before).toEqual(receipt.agent);
  } else {
    seen.stage("initial-current");
    const entry = directory.getByRole("button").filter({ has: page.locator("strong").filter({ hasText: new RegExp(`^${data.draft.name}$`) }) });
    await expect(entry).toHaveCount(1); await entry.focus(); await page.keyboard.press("Enter");
    await expect(fact(page, "版本")).toHaveText("1"); await expect(fact(page, "Secret Variable 引用")).toHaveText(first.id);
    await expect(button(page, "编辑配置")).toBeEnabled(); await seen.verify();
    before = seen.current("1", [first.id]); expect(before).toEqual(data.initial);
  }
  seen.stage("edit"); await press(page, "编辑配置");
  await expect(editor(page).getByRole("textbox", { name: "Agent 名称", exact: true })).toBeFocused();
  await press(page, "重新读取 Secret Variable 目录"); await expect(button(page, "重新读取 Secret Variable 目录")).toBeEnabled(); await seen.verify(); seen.secrets();
  await expect(secrets(editor(page))).toContainText(first.id); await expect(button(page, `移除 ${first.name}`)).toBeEnabled();
  await expect(button(page, "保存配置")).toBeDisabled();
  await press(page, "取消编辑"); await expect(editor(page)).toHaveCount(0); await expect(button(page, "编辑配置")).toBeFocused();
  await page.keyboard.press("Enter"); await press(page, "重新读取 Secret Variable 目录"); await expect(button(page, "重新读取 Secret Variable 目录")).toBeEnabled(); await seen.verify(); seen.secrets();
  await press(page, `移除 ${first.name}`); await select(page, "添加 Secret Variable", second.name); await press(page, "添加到白名单");
  await expect(secrets(editor(page))).toContainText(second.id); await expect(button(page, `移除 ${first.name}`)).toHaveCount(0);
  await press(page, "保存配置");
  if (lost) {
    seen.stage("uncertain"); const recovery = page.getByRole("region", { name: "Agent 保存结果恢复", exact: true });
    await expect(recovery).toContainText("保存结果待确认。请查询原操作结果，不要重复保存。");
    await expect(editor(page)).toHaveCount(0); await expect(button(page, "保存配置")).toHaveCount(0);
    await expect(button(page, "创建 Agent")).toHaveCount(0); await expect(button(page, "编辑配置")).toBeDisabled();
    await seen.verify(); expect(seen.requests("update")).toHaveLength(1); expect(seen.requests("lookup")).toHaveLength(0);
    await recovery.scrollIntoViewIfNeeded(); await agentSecretScreenshot(page, data.mode, "1440-light-uncertain");
    await press(page, "查询原保存结果");
  }
  await expect(editor(page)).toHaveCount(0); await expect(fact(page, "版本")).toHaveText("2");
  await expect(fact(page, "Secret Variable 引用")).toContainText(second.id); await expect(fact(page, "Secret Variable 引用")).toContainText(second.name); await expect(button(page, "编辑配置")).toBeEnabled(); await seen.verify();
  if (lost) seen.originalLookup();
  const receipt = lost ? seen.receipt("lookup", 0).receipt : seen.receipt("update", 0);
  seen.requireReceipt(receipt, "2", [second.id], before);
  let final = seen.current("2", [second.id]); expect(final).toEqual(receipt.agent);
  if (!lost) {
    seen.stage("clear"); await press(page, "编辑配置"); await press(page, "重新读取 Secret Variable 目录"); await expect(button(page, "重新读取 Secret Variable 目录")).toBeEnabled(); await seen.verify(); seen.secrets();
    await expect(secrets(editor(page))).toContainText(second.id); await press(page, `移除 ${second.name}`);
    await expect(secrets(editor(page)).getByRole("button", { name: /^移除 / })).toHaveCount(0);
    await expect(button(page, "保存配置")).toBeEnabled(); await layout(page, 390, "dark");
    await secrets(editor(page)).scrollIntoViewIfNeeded(); await agentSecretScreenshot(page, data.mode, "390-dark-clear");
    await press(page, "保存配置"); await expect(editor(page)).toHaveCount(0); await expect(fact(page, "版本")).toHaveText("3");
    await expect(fact(page, "Secret Variable 引用")).toHaveText("白名单为空"); await expect(button(page, "编辑配置")).toBeEnabled(); await seen.verify();
    const cleared = seen.receipt("update", 1); seen.requireReceipt(cleared, "3", [], final); final = seen.current("3", []); expect(final).toEqual(cleared.agent);
  }
  seen.stage("explicit-current");
  const configurations = seen.requests("agent-configuration").length, directories = seen.requests("agent-directory").length;
  await press(page, "刷新 Agent 目录");
  await expect.poll(() => seen.requests("agent-directory").length).toBe(directories + 1);
  await expect.poll(() => seen.requests("agent-configuration").length).toBe(configurations + 1);
  await expect(button(page, "刷新 Agent 目录")).toBeEnabled(); await expect(button(page, "编辑配置")).toBeEnabled(); await seen.verify();
  expect(seen.current(final.version, lost ? [second.id] : [])).toEqual(final); seen.directory(final);
  await expect(directory).not.toContainText(data.draft.instructions);
  await layout(page, 390, "dark"); await fact(page, "Secret Variable 引用").scrollIntoViewIfNeeded(); await agentSecretScreenshot(page, data.mode, "390-dark-current");
  seen.stage("complete"); await seen.finish();
}
test("Owner creates, replaces and clears Secret allowlist [secret-allowlist-normal]", async ({ page }) => run(page, false));
test("lost Secret update looks up only its original intent [secret-allowlist-lost]", async ({ page }) => run(page, true));
