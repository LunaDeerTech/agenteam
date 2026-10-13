import { test, expect, type Page } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  agentSkillsMaterial,
  observeAgentSkills,
} from "./agent-skills.helpers";
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const panel = (page: Page) =>
  page.getByRole("region", { name: "Agent Skills", exact: true });
const confirmation = (page: Page) =>
  page.getByRole("form", { name: "确认技能分配操作", exact: true });
async function press(page: Page, name: string) {
  const target = button(page, name);
  await expect(target).toBeEnabled();
  await target.focus();
  await expect(target).toBeFocused();
  await page.keyboard.press("Enter");
}
async function screenshot(
  page: Page,
  name: string,
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
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!,
      `agent-skills-${name}.png`,
    ),
    fullPage: true,
    animations: "disabled",
  });
}
test("explicitly assigns, disables, enables and removes a Skill [assign-disable-enable-remove]", async ({
  page,
  context,
}) => {
  const data = agentSkillsMaterial();
  await context.addCookies([
    {
      name: "agenteam_local_session",
      value: data.cookie,
      url: process.env.AGENTEAM_AUTH_WEB_ORIGIN!,
      httpOnly: true,
      secure: false,
      sameSite: "Lax",
    },
  ]);
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  const seen = await observeAgentSkills(page, data);
  let completed = false,
    stage = "navigation";
  try {
    await page.goto(data.route);
    const directory = page.getByRole("region", {
      name: "Agent 目录",
      exact: true,
    });
    const entry = directory
      .getByRole("button")
      .filter({
        has: page.locator("strong").filter({ hasText: /^assignment-agent$/ }),
      });
    await expect(entry).toHaveCount(1);
    await entry.focus();
    await page.keyboard.press("Enter");
    const configuration = page.getByRole("region", {
      name: "Agent 配置",
      exact: true,
    });
    const version = configuration
      .locator("dt")
      .filter({ hasText: /^版本$/ })
      .locator("xpath=following-sibling::dd[1]");
    await expect(version).toHaveText("1");
    await press(page, "管理 Skills");
    await expect(panel(page)).toBeVisible();
    await expect(panel(page).locator(".skill-list > li")).toHaveCount(1);
    await expect(button(page, "选择项目技能")).toBeEnabled();
    await seen.verify();
    stage = "select-candidate";
    await press(page, "选择项目技能");
    await page
      .getByRole("option", { name: "assignment-browser", exact: true })
      .click();
    await press(page, "分配技能");
    await expect(confirmation(page).getByRole("heading")).toBeFocused();
    await press(page, "保留当前分配");
    await expect(confirmation(page)).toHaveCount(0);
    await expect(button(page, "分配技能")).toBeFocused();
    expect(seen.requests("assign")).toHaveLength(0);
    await press(page, "分配技能");
    await screenshot(page, "1440-light-confirm", 1440, "light");
    const heading = panel(page).getByRole("heading", {
      name: "Agent Skills",
      exact: true,
    });
    const row = () =>
      panel(page)
        .locator(".skill-list > li")
        .filter({
          has: page
            .locator("strong")
            .filter({ hasText: /^assignment-browser$/ }),
        });
    async function confirm(expected: string, feedback: string) {
      await expect(confirmation(page)).toContainText(
        `基于 Agent 版本 ${Number(expected) - 1}`,
      );
      await press(page, "确认技能操作");
      await expect(confirmation(page)).toHaveCount(0);
      await expect(version).toHaveText(expected);
      await expect(
        panel(page).getByText(feedback, { exact: true }),
      ).toBeVisible();
      await expect(heading).toBeFocused();
      await seen.verify();
    }
    stage = "assign";
    await confirm("2", "技能分配已保存。");
    await expect(row().getByText("已启用", { exact: true })).toBeVisible();
    stage = "disable";
    await press(page, "禁用技能 assignment-browser");
    await confirm("3", "技能已禁用，分配记录保留。");
    await expect(row().getByText("已禁用", { exact: true })).toBeVisible();
    await screenshot(page, "1440-dark-disabled", 1440, "dark");
    stage = "enable";
    await press(page, "启用技能 assignment-browser");
    await confirm("4", "技能分配已保存。");
    await expect(row().getByText("已启用", { exact: true })).toBeVisible();
    await screenshot(page, "390-light-enabled", 390, "light");
    stage = "remove";
    await press(page, "撤销分配 assignment-browser");
    await confirm("5", "已撤销分配，技能包保留。");
    await expect(row()).toHaveCount(0);
    await expect(panel(page).locator(".skill-list > li")).toHaveCount(1);
    await expect(
      panel(page).getByText("Add Skills", { exact: true }),
    ).toBeVisible();
    await screenshot(page, "390-dark-removed", 390, "dark");
    stage = "close-focus";
    await press(page, "关闭技能管理");
    await expect(panel(page)).toHaveCount(0);
    await expect(button(page, "管理 Skills")).toBeFocused();
    await seen.verify();
    stage = "retire";
    await seen.retire();
    seen.complete();
    completed = true;
  } finally {
    if (!completed && !page.isClosed()) {
      // Bounded fixed-stage diagnostics contain only the synthetic fixture UI.
      const text = await page
        .locator("body")
        .innerText({ timeout: 1000 })
        .catch(() => "unavailable");
      writeFileSync(
        join(
          process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!,
          "agent-skills-failure.json",
        ),
        JSON.stringify({ stage, visible: text.slice(0, 8192) }),
        { mode: 0o600 },
      );
      await page
        .screenshot({
          path: join(
            process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!,
            "agent-skills-failure.png",
          ),
          timeout: 1500,
          animations: "disabled",
        })
        .catch(() => {});
    }
    try {
      await seen.retire();
    } finally {
      seen.save(completed ? "passed" : "failed");
      await page.close();
    }
  }
});
