// Independent review of the frozen general-settings pilot, not a visual score.
// The caller owns the static server. This script closes only its browser.
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
const require = createRequire(import.meta.url);
const { chromium, expect } = require(
  process.env.F0_PLAYWRIGHT ||
    "/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules/@playwright/test",
);
const repository = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../..",
);
const origin = process.env.F0_REVIEW_ORIGIN || "http://127.0.0.1:5207";
const output =
  process.env.F0_REVIEW_OUTPUT ||
  path.join(
    repository,
    "output/ai/frontend-f0-refinement-review/general-second-interactions",
  );
const mode = process.argv[2] || "interactions";
if (mode === "--list") {
  console.log(
    JSON.stringify(
      {
        modes: ["views", "interactions"],
        input: "settings / fixture=baseline / normal / Atlas",
        viewports: ["1440×960 light/dark", "390×844 light/dark"],
        interactions: [
          "more menu keys and mid-transition close",
          "description cancel and local save",
          "rename confirmation cancel and confirm",
          "dirty navigation cancel",
          "mobile settings menu",
          "normal and reduced motion frames",
        ],
        boundary: "Synthetic page; no API persistence or overall F0 acceptance",
      },
      null,
      2,
    ),
  );
  process.exit(0);
}
assert(["views", "interactions"].includes(mode));
await mkdir(output, { recursive: true });
const report = {
  input:
    process.env.F0_REVIEW_INPUT ||
    "f0138630; author-frozen general second draft",
  origin,
  mode,
  cases: [],
  boundary:
    "Synthetic prototype only; no business writes; visual judgments are in README",
};
const browser = await chromium.launch({
  executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
  args: ["--no-sandbox"],
});
const baseline = "一个用于核对真实页面结构、长内容与操作布局的测试项目。";
const settle = (page) =>
  page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => {})),
    );
  });
const shot = (page, name, fullPage = false) =>
  page.screenshot({
    path: path.join(output, `${name}.png`),
    fullPage,
    animations: "allow",
  });
async function open(width, theme, reducedMotion = "no-preference") {
  const context = await browser.newContext({
    viewport: { width, height: width === 390 ? 844 : 960 },
    colorScheme: theme,
    reducedMotion,
    deviceScaleFactor: 1,
  });
  await context.route("**/*", (route) =>
    route
      .request()
      .url()
      .startsWith(origin + "/")
      ? route.continue()
      : route.abort(),
  );
  const page = await context.newPage();
  page.setDefaultTimeout(8000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(
    `${origin}/?page=settings&fixture=baseline&scenario=normal&theme=${theme}`,
  );
  await page.evaluate(() => document.fonts.ready);
  return { context, page, errors };
}
async function sampleOnNextClick(page, selector) {
  await page.evaluate((selector) => {
    window.__reviewFrames = [];
    document.addEventListener(
      "click",
      () => {
        const started = performance.now();
        function frame() {
          const el = document.querySelector(selector),
            s = el && getComputedStyle(el);
          window.__reviewFrames.push({
            elapsed: performance.now() - started,
            element: s && {
              opacity: s.opacity,
              transform: s.transform,
              animation: s.animationName,
              duration: s.animationDuration,
            },
            animations: document.getAnimations().map((a) => ({
              name: a.animationName || "transition",
              currentTime: a.currentTime,
              duration: a.effect.getComputedTiming().duration,
              playState: a.playState,
            })),
          });
          if (performance.now() - started < 260) requestAnimationFrame(frame);
        }
        requestAnimationFrame(frame);
      },
      { once: true, capture: true },
    );
  }, selector);
}
async function collectFrames(page) {
  await page.waitForFunction(
    () => window.__reviewFrames.at(-1)?.elapsed >= 260,
  );
  return page.evaluate(() => window.__reviewFrames);
}
try {
  if (mode === "views") {
    for (const width of [1440, 390])
      for (const theme of ["light", "dark"]) {
        const { context, page, errors } = await open(width, theme);
        try {
          await settle(page);
          await shot(page, `general-${width}-${theme}-viewport`);
          await shot(page, `general-${width}-${theme}-full`, true);
          await page.locator("#general-more").click();
          await settle(page);
          await shot(page, `general-${width}-${theme}-menu`);
          assert.equal(errors.length, 0);
          report.cases.push({
            name: `views-${width}-${theme}`,
            result: "captured; requires human visual review",
            errors,
          });
        } finally {
          await context.close();
        }
      }
  } else {
    for (const reducedMotion of ["no-preference", "reduce"]) {
      const { context, page, errors } = await open(1440, "dark", reducedMotion);
      const current = {
        name: `journey-${reducedMotion}`,
        motion: {},
        checks: [],
      };
      report.cases.push(current);
      try {
        const more = page.locator("#general-more");
        await expect(page.getByLabel("项目名称", { exact: true })).toHaveValue(
          "Atlas",
        );
        await expect(page.getByLabel("项目描述", { exact: true })).toHaveValue(
          baseline,
        );
        await expect(
          page.getByRole("button", { name: "保存修改", exact: true }),
        ).toBeDisabled();
        await sampleOnNextClick(page, ".floating-menu");
        await more.click();
        await shot(page, `more-${reducedMotion}-early`);
        current.motion.menu = await collectFrames(page);
        await shot(page, `more-${reducedMotion}-settled`);
        await page.keyboard.press("ArrowDown");
        await expect(
          page.getByRole("menuitem", { name: "查看项目信息与标识" }),
        ).toBeFocused();
        await shot(page, `more-${reducedMotion}-keyboard-focus`);
        await page.keyboard.press("Escape");
        await expect(more).toBeFocused();
        await expect(more).not.toHaveAttribute("aria-controls", /.+/);
        await more.click();
        await page.keyboard.press("Tab");
        await expect(
          page.getByLabel("项目名称", { exact: true }),
        ).toBeFocused();
        current.checks.push(
          "menu ArrowDown / Escape return / Tab continuation / aria-controls cleanup",
        );
        await more.click();
        current.motion.interruptedMenu = await page.evaluate(() =>
          document.getAnimations().map((a) => ({
            currentTime: a.currentTime,
            duration: a.effect.getComputedTiming().duration,
          })),
        );
        await page.keyboard.press("Escape");
        await expect(page.getByRole("menu")).toHaveCount(0);
        await expect(more).toBeFocused();
        current.checks.push("menu can close during its entry; focus returns");
        await page
          .getByLabel("项目描述", { exact: true })
          .fill(`${baseline}\n独立检查取消。`);
        await page
          .getByRole("button", { name: "取消修改", exact: true })
          .click();
        await expect(page.getByLabel("项目描述", { exact: true })).toHaveValue(
          baseline,
        );
        await expect(
          page.getByLabel("项目名称", { exact: true }),
        ).toBeFocused();
        await expect(page.getByRole("dialog")).not.toBeVisible();
        current.checks.push(
          "cancel directly restores complete local current value",
        );
        const savedDescription = `${baseline}\n独立检查本地保存。`;
        await page
          .getByLabel("项目描述", { exact: true })
          .fill(savedDescription);
        await page
          .getByRole("button", { name: "保存修改", exact: true })
          .click();
        await expect(
          page.getByRole("button", { name: "保存中…", exact: true }),
        ).toBeDisabled();
        await shot(page, `saving-${reducedMotion}`);
        await expect(page.locator(".general-feedback")).toContainText(
          "已保存到当前样板，未调用服务端。",
        );
        await expect(page.getByLabel("项目描述", { exact: true })).toHaveValue(
          savedDescription,
        );
        await expect(
          page.getByRole("button", { name: "保存修改", exact: true }),
        ).toBeDisabled();
        await expect(page.locator("#general-title")).toBeFocused();
        await shot(page, `saved-${reducedMotion}`);
        current.checks.push(
          "description save has waiting and explicitly local feedback; focus remains meaningful",
        );
        await page.getByLabel("项目名称", { exact: true }).fill("Atlas.Review");
        await sampleOnNextClick(page, "dialog[open]");
        await page
          .getByRole("button", { name: "保存修改", exact: true })
          .click();
        const dialog = page.getByRole("dialog");
        await expect(dialog).toContainText(
          "旧名称链接将失效，不提供别名或自动跳转",
        );
        await shot(page, `rename-${reducedMotion}-early`);
        current.motion.dialog = await collectFrames(page);
        await shot(page, `rename-${reducedMotion}-settled`);
        await dialog.getByRole("button", { name: "关闭", exact: true }).focus();
        await page.keyboard.press("Shift+Tab");
        await expect(
          dialog.getByRole("button", { name: "确认保存", exact: true }),
        ).toBeFocused();
        await page.keyboard.press("Tab");
        await expect(
          dialog.getByRole("button", { name: "关闭", exact: true }),
        ).toBeFocused();
        await page.keyboard.press("Escape");
        await expect(dialog).not.toBeVisible();
        await expect(page.getByLabel("项目名称", { exact: true })).toHaveValue(
          "Atlas.Review",
        );
        await expect(
          page.getByRole("button", { name: "保存修改", exact: true }),
        ).toBeFocused();
        current.checks.push(
          "rename warning visible; Tab loop; Escape preserves draft and returns focus",
        );
        await page
          .getByRole("button", { name: "保存修改", exact: true })
          .click();
        await dialog
          .getByRole("button", { name: "确认保存", exact: true })
          .click();
        await expect(page.locator(".general-feedback")).toContainText(
          "已保存到当前样板，未调用服务端。",
        );
        await expect(page.locator(".project-identity")).toContainText(
          "Atlas.Review",
        );
        await expect(page).toHaveURL(/project_name=Atlas.Review/);
        current.checks.push(
          "confirmed local rename updates project context; no backend claim",
        );
        await page
          .getByLabel("项目描述", { exact: true })
          .fill("继续编辑时应保留的草稿");
        await page
          .locator(".project-tabs")
          .getByRole("link", { name: "任务", exact: true })
          .click();
        await expect(dialog).toContainText("离开不会撤销服务器命令");
        await dialog
          .getByRole("button", { name: "继续编辑", exact: true })
          .click();
        await expect(page.getByLabel("项目描述", { exact: true })).toHaveValue(
          "继续编辑时应保留的草稿",
        );
        await expect(page).toHaveURL(/page=settings/);
        current.checks.push(
          "dirty navigation cancellation preserves page and draft",
        );
        assert.equal(errors.length, 0);
        current.result =
          "behavior observations passed; motion still requires visual interpretation";
        current.errors = errors;
      } catch (error) {
        current.failure = error.message;
        await shot(page, `failure-${reducedMotion}`);
        throw error;
      } finally {
        await context.close();
      }
    }
    const { context, page, errors } = await open(390, "dark");
    try {
      const trigger = page.getByRole("button", {
        name: "基本信息，切换项目设置栏目",
        exact: true,
      });
      await trigger.click();
      await settle(page);
      await shot(page, "mobile-settings-menu");
      await expect(page.getByRole("menuitem")).toHaveCount(11);
      await page.keyboard.press("End");
      await expect(
        page.getByRole("menuitem", { name: "MCP", exact: true }),
      ).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(trigger).toBeFocused();
      await page.locator(".general-records summary").click();
      await page
        .locator(".general-records time")
        .last()
        .scrollIntoViewIfNeeded();
      await shot(page, "mobile-records-bottom");
      await expect(
        page.locator(".general-records time").last(),
      ).toBeInViewport();
      assert.equal(errors.length, 0);
      report.cases.push({
        name: "mobile-directory-and-records",
        result: "passed",
        scope:
          "11 labels visible, only general/Agent/MCP are live in this pilot; bottom metadata reachable",
        errors,
      });
    } finally {
      await context.close();
    }
  }
} catch (error) {
  report.failure = error.message;
  process.exitCode = 1;
} finally {
  await browser.close();
  report.browserClosed = true;
  report.serverOwnership = "designer-owned server was not closed";
  await writeFile(
    path.join(output, "general-review.json"),
    JSON.stringify(report, null, 2),
  );
}
console.log(
  JSON.stringify({
    output,
    mode,
    cases: report.cases.map((c) => ({
      name: c.name,
      result: c.result,
      failure: c.failure,
    })),
    failure: report.failure,
    browserClosed: report.browserClosed,
  }),
);
