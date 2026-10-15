// Independent browser scenarios. Does not import the author's assertions.
import { createRequire } from "node:module";
import { mkdir, writeFile, mkdtemp, rm } from "node:fs/promises";
import path from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
const here = path.dirname(fileURLToPath(import.meta.url));
const output =
  process.env.F0_REVIEW_OUTPUT ||
  path.resolve(here, "../../../../../output/ai/frontend-f0-review");
const origin = process.env.F0_REVIEW_ORIGIN || "http://127.0.0.1:5198";
const modes = ["projects", "base", "zoom"];
if (process.argv.includes("--list")) {
  console.log(JSON.stringify(modes));
  process.exit(0);
}
const mode = process.argv[2] || "projects";
if (!modes.includes(mode)) throw new Error("Unknown review mode");
const require = createRequire(import.meta.url);
const { chromium } = require(
  process.env.F0_PLAYWRIGHT ||
    "/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules/@playwright/test",
);
await mkdir(output, { recursive: true });
const results = [],
  errors = [],
  requests = [];
let browser, context, profile;
function check(condition, message, evidence = {}) {
  results.push({ result: condition ? "pass" : "fail", message, ...evidence });
}
async function open(page, name, scenario = "normal", extra = {}) {
  await page.goto(
    `${origin}/index.html?${new URLSearchParams({ page: name, scenario, theme: "light", ...extra })}`,
  );
  await page.locator("#main").waitFor();
  await page.evaluate(() => document.fonts.ready);
}
async function screenshot(page, name) {
  await page.screenshot({
    path: path.join(output, `interaction-${name}.png`),
    fullPage: true,
  });
}
try {
  if (mode === "zoom") {
    profile = await mkdtemp(path.join(tmpdir(), "f0-review-zoom-"));
    await mkdir(path.join(profile, "Default"));
    // Chromium's native per-partition default zoom. The empty default partition is x.
    await writeFile(
      path.join(profile, "Default/Preferences"),
      JSON.stringify({
        partition: { default_zoom_level: { x: Math.log(2) / Math.log(1.2) } },
      }),
    );
    context = await chromium.launchPersistentContext(profile, {
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox", "--window-size=1440,960"],
      viewport: null,
      reducedMotion: "reduce",
    });
  } else {
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    });
    context = await browser.newContext({
      viewport: { width: 1440, height: 960 },
      reducedMotion: "reduce",
    });
  }
  await context.route("**/*", (route) => {
    const request = route.request();
    if (
      request.url().startsWith(origin + "/") &&
      ["GET", "HEAD"].includes(request.method())
    )
      return route.continue();
    if (/^(data|blob):/.test(request.url())) return route.continue();
    requests.push({ method: request.method(), url: request.url() });
    return route.abort();
  });
  const page = context.pages()[0] || (await context.newPage());
  page.on("pageerror", (error) => errors.push(error.message));
  if (mode === "projects") {
    await open(page, "projects", "unknown");
    const createUnknown = page.getByRole("button", {
      name: "新建项目",
      exact: true,
    });
    check(
      await createUnknown.isDisabled(),
      "Unknown creation prevents another create intent",
    );
    if (await createUnknown.isEnabled()) {
      await createUnknown.click();
      check(
        await page.getByRole("dialog").isVisible(),
        "Reproduction: another creation dialog is reachable in Unknown",
      );
      await screenshot(page, "projects-unknown-new-intent");
      await page.keyboard.press("Escape");
    }
    await page.getByRole("button", { name: "查询原请求", exact: true }).click();
    check(
      (await page.locator("#main").innerText()).includes("原请求"),
      "Original lookup remains visible",
    );
    await open(page, "projects", "normal", { fixture: "baseline" });
    await page
      .getByRole("link", { name: "Design.System", exact: true })
      .click();
    const projectContext = await page.locator(".project-identity").innerText();
    check(
      projectContext.includes("Design.System"),
      "Project entry preserves selected object context",
      { observed: projectContext },
    );
    await screenshot(page, "project-entry-context");
    for (const viewport of [
      { width: 1440, height: 960 },
      { width: 390, height: 844 },
    ]) {
      await page.setViewportSize(viewport);
      await open(page, "projects", "normal", {
        theme: viewport.width === 390 ? "dark" : "light",
      });
      const trigger = page.getByRole("button", {
        name: "新建项目",
        exact: true,
      });
      await trigger.focus();
      await page.keyboard.press("Enter");
      const dialog = page.getByRole("dialog");
      let trapped = true;
      for (let i = 0; i < 14; i++) {
        await page.keyboard.press(i % 2 ? "Tab" : "Shift+Tab");
        trapped &&= await page.evaluate(() =>
          document.querySelector("dialog").contains(document.activeElement),
        );
      }
      const focused = await page.evaluate(() => {
        const s = getComputedStyle(document.activeElement),
          r = document.activeElement.getBoundingClientRect();
        return {
          outline: s.outlineStyle,
          outlineWidth: s.outlineWidth,
          outlineColor: s.outlineColor,
          inViewport: r.top >= 0 && r.bottom <= innerHeight,
        };
      });
      check(
        trapped,
        `Project modal traps forward/reverse Tab at ${viewport.width}`,
      );
      check(
        focused.outline !== "none" &&
          parseFloat(focused.outlineWidth) >= 2 &&
          focused.inViewport,
        `Keyboard focus visible and reachable at ${viewport.width}`,
        { focused },
      );
      await screenshot(page, `projects-keyboard-${viewport.width}`);
      await page.keyboard.press("Escape");
      check(
        await trigger.evaluate((el) => el === document.activeElement),
        `Escape restores project trigger at ${viewport.width}`,
      );
      check(
        (
          await page
            .getByRole("complementary", { name: "设计样板控制" })
            .innerText()
        ).includes("合成数据"),
        `Fixture boundary persists at ${viewport.width}`,
      );
    }
  } else if (mode === "base") {
    await open(page, "tasks", "unknown");
    const columnCreate = page.locator("[data-add-state]").first();
    check(
      await columnCreate.isDisabled(),
      "Task Unknown disables column creation as well as the primary create action",
    );
    if (await columnCreate.isEnabled()) {
      await columnCreate.click();
      await screenshot(page, "task-unknown-column-create");
      await page.keyboard.press("Escape");
    }
    for (const viewport of [
      { width: 1440, height: 960 },
      { width: 390, height: 844 },
    ]) {
      await page.setViewportSize(viewport);
      await open(page, "tasks", "normal", {
        theme: viewport.width === 390 ? "dark" : "light",
      });
      const first = page.getByTestId("task-card").first();
      await first.click();
      await screenshot(page, `task-detail-${viewport.width}`);
      await page
        .getByLabel("任务描述草稿")
        .fill(`Independent draft for first task at ${viewport.width}`);
      await page.keyboard.press("Escape");
      check(
        await page.getByRole("dialog").isVisible(),
        `Task draft close gate at ${viewport.width}`,
      );
      await page
        .getByRole("button", { name: "保留草稿并关闭", exact: true })
        .click();
      check(
        await first.evaluate((el) => el === document.activeElement),
        `Task close restores source at ${viewport.width}`,
      );
      await page.getByTestId("task-card").nth(1).click();
      const otherDraft = await page.getByLabel("任务描述草稿").inputValue();
      check(
        !otherDraft.includes("Independent draft for first task"),
        `Task draft is scoped to its task at ${viewport.width}`,
        { observed: otherDraft },
      );
      if (otherDraft)
        await screenshot(page, `task-other-draft-${viewport.width}`);
      await page.keyboard.press("Escape");
      await open(page, "providers", "normal", {
        theme: viewport.width === 390 ? "dark" : "light",
      });
      await page.getByRole("tab", { name: "模型", exact: true }).click();
      await screenshot(page, `provider-models-${viewport.width}`);
      await page
        .getByRole("button", { name: "编辑", exact: true })
        .first()
        .click();
      await screenshot(page, `model-editor-${viewport.width}`);
      await page.keyboard.press("Escape");
      await page.getByRole("tab", { name: "配置", exact: true }).click();
      await page.getByRole("tab", { name: "配置", exact: true }).focus();
      await page.keyboard.press("ArrowRight");
      check(
        await page
          .getByRole("tab", { name: "模型", exact: true })
          .evaluate((el) => el === document.activeElement),
        `Provider tablist supports arrow navigation at ${viewport.width}`,
      );
      await page.keyboard.press("Escape");
      for (const name of ["projects", "providers"]) {
        await open(page, name);
        await page.keyboard.press("Tab");
        const obscured = [];
        for (let i = 0; i < 32; i++) {
          await page.keyboard.press("Tab");
          const result = await page.evaluate(() => {
            const el = document.activeElement,
              r = el.getBoundingClientRect(),
              lab = document.querySelector(".lab-bar").getBoundingClientRect();
            if (!el.closest("#main") || !r.width || !r.height) return null;
            return r.bottom > lab.top && r.top < lab.bottom
              ? {
                  text:
                    el.getAttribute("aria-label") ||
                    el.textContent.trim().slice(0, 60),
                  top: r.top,
                  bottom: r.bottom,
                  barTop: lab.top,
                }
              : null;
          });
          if (result) obscured.push(result);
        }
        check(
          !obscured.length,
          `${name}: focused actions avoid fixed sample bar at ${viewport.width}`,
          { obscured },
        );
      }
    }
  } else {
    for (const name of (
      process.env.F0_REVIEW_PAGES ||
      "projects,tasks,providers,knowledge,meetings,inbox,mcp,oauth,memory"
    ).split(",")) {
      await open(page, name, "long");
      const measured = await page.evaluate(() => ({
        innerWidth,
        innerHeight,
        outerWidth,
        devicePixelRatio,
        scrollWidth: document.documentElement.scrollWidth,
        reduced: matchMedia("(prefers-reduced-motion: reduce)").matches,
        animations: [...document.querySelectorAll("*")].filter((el) => {
          const s = getComputedStyle(el);
          return (
            s.animationName !== "none" &&
            parseFloat(s.animationDuration) > 0.001
          );
        }).length,
      }));
      check(
        measured.outerWidth === 1440 &&
          measured.innerWidth === 720 &&
          measured.devicePixelRatio === 2,
        `${name}: native Chromium 200% zoom observed`,
        { measured },
      );
      check(
        measured.scrollWidth <= measured.innerWidth &&
          measured.reduced &&
          measured.animations === 0,
        `${name}: zoom reflow and reduced-motion`,
        { measured },
      );
      if (name === "knowledge") {
        const region = await page
          .locator(".knowledge-reader-scroll")
          .evaluate((el) => {
            const r = el.getBoundingClientRect(),
              s = getComputedStyle(el);
            return {
              height: r.height,
              contentHeight:
                el.clientHeight -
                parseFloat(s.paddingTop) -
                parseFloat(s.paddingBottom),
              overflow: s.overflowY,
              scrollHeight: el.scrollHeight,
            };
          });
        check(
          region.contentHeight >= 120,
          "Knowledge 200%: reader has useful space for several lines, beyond padding",
          { region },
        );
        const readMore = page.locator('[data-knowledge="read-more"]');
        await readMore.scrollIntoViewIfNeeded();
        await readMore.click();
        check(
          await page
            .getByRole("button", { name: "编辑原文", exact: true })
            .isEnabled(),
          "Knowledge 200%: read continuation is reachable",
        );
        await page
          .locator(".knowledge-prose")
          .evaluate((el) => el.scrollIntoView({ block: "start" }));
        await page.mouse.move(500, 280);
        await page.mouse.wheel(0, 280);
        const afterScroll = await page.evaluate(() => ({
          pageY: scrollY,
          readerY: document.querySelector(".knowledge-reader-scroll").scrollTop,
        }));
        check(
          afterScroll.pageY > 0 || afterScroll.readerY > 0,
          "Knowledge 200%: wheel moves the page or reader to later content",
          { afterScroll },
        );
      }
      // Native zoom affects Playwright's fullPage clip coordinates; capture the actual viewport via CDP.
      const cdp = await context.newCDPSession(page);
      const capture = await cdp.send("Page.captureScreenshot", {
        format: "png",
        fromSurface: true,
        captureBeyondViewport: false,
      });
      await writeFile(
        path.join(output, `interaction-${name}-native-200pct.png`),
        Buffer.from(capture.data, "base64"),
      );
      await cdp.detach();
    }
  }
  check(
    errors.length === 0 && requests.length === 0,
    "No script errors or non-fixture requests",
    { errors, requests },
  );
} finally {
  await context?.close();
  await browser?.close();
  if (profile) await rm(profile, { recursive: true, force: true });
  await writeFile(
    path.join(output, `interaction-${mode}.json`),
    JSON.stringify(
      {
        boundary: "F0 synthetic only; no production service",
        results,
        errors,
        requests,
      },
      null,
      2,
    ),
  );
}
console.log(JSON.stringify(results, null, 2));
if (results.some((result) => result.result === "fail")) process.exitCode = 1;
