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
  let originalError: unknown;
  let completed = false,
    stage = "navigation";
  try {
    await page.goto(data.route);
    const directory = page.getByRole("region", {
      name: "Agent 目录",
      exact: true,
    });
    const entry = directory.getByRole("button").filter({
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
    expect(seen.requests("assign").length).toBe(0);
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
  } catch (error) {
    originalError = error;
    throw error;
  } finally {
    let cleanupError: unknown;
    const note = (error: unknown) => {
      cleanupError ??= error;
    };
    try {
      if (!completed && !page.isClosed()) {
        const observedAt = new Date().toISOString();
        const failurePath = join(
          process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!,
          "agent-skills-failure.json",
        );
        writeFileSync(
          failurePath,
          JSON.stringify({ stage, observed_at: observedAt, unavailable: true }),
          { mode: 0o600 },
        );
        const diagnostic = page
          .evaluate(() => {
            const safe = (element: Element | null) => {
              if (!element) return null;
              const label =
                element.getAttribute("aria-label") ??
                element.textContent?.trim() ??
                "";
              const fixed =
                /^(管理 Skills|关闭技能管理|选择项目技能|分配技能|确认技能操作|保留当前分配|重新读取分配|读取项目技能|更多已分配技能|更多项目技能|清除技能选择|Agent Skills)$/;
              const operation = /^(禁用技能|启用技能|撤销分配)(?: |$)/.exec(
                label,
              )?.[1];
              return {
                tag: element.tagName.toLowerCase(),
                label: fixed.test(label) ? label : (operation ?? "other"),
              };
            };
            const native = (window as any).__taskReviewProof?.snapshot();
            const consumer = (window as any).__agentSkillsOriginal?.snapshot();
            return {
              route: location.pathname,
              theme: ["light", "dark"].includes(
                document.documentElement.dataset.theme ?? "",
              )
                ? document.documentElement.dataset.theme
                : "other",
              focus: safe(document.activeElement),
              buttons: [...document.querySelectorAll("button")]
                .slice(0, 32)
                .map((b) => ({ ...safe(b), disabled: b.disabled })),
              pending: {
                native: native?.pending ?? null,
                public: consumer?.pending ?? null,
              },
              readers:
                native?.rows.slice(-8).map((r: any) => ({
                  request_id: r.request_id ?? null,
                  eof: r.eof,
                  closed: r.closed,
                  release: r.release,
                  reads: r.reads,
                  read_returns: r.read_returns,
                })) ?? [],
            };
          })
          .catch(() => ({ unavailable: true }));
        let diagnosticTimer: ReturnType<typeof setTimeout> | undefined;
        const facts = await Promise.race([
          diagnostic,
          new Promise<{ unavailable: true }>((resolve) => {
            diagnosticTimer = setTimeout(
              () => resolve({ unavailable: true }),
              750,
            );
          }),
        ]).finally(() => {
          if (diagnosticTimer !== undefined) clearTimeout(diagnosticTimer);
        });
        writeFileSync(
          failurePath,
          JSON.stringify({ stage, observed_at: observedAt, ...facts }),
          { mode: 0o600 },
        );
        await page.screenshot({
          path: join(
            process.env.AGENTEAM_AGENT_SKILLS_WEB_EVIDENCE!,
            "agent-skills-failure.png",
          ),
          timeout: 1500,
          animations: "disabled",
        });
      }
    } catch (error) {
      note(error);
    } finally {
      try {
        await seen.retire();
      } catch (error) {
        note(error);
      } finally {
        try {
          await page.close();
          seen.pageClosed();
        } catch (error) {
          note(error);
        } finally {
          try {
            seen.save(
              completed && cleanupError === undefined ? "passed" : "failed",
            );
          } catch (error) {
            note(error);
          }
        }
      }
    }
    // Cleanup is attempted on every path without replacing the original failed assertion.
    if (originalError === undefined && cleanupError !== undefined)
      throw cleanupError;
  }
});
