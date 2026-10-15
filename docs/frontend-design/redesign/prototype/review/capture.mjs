// Independent F0 visual review helper. Synthetic pages only; no business API calls.
import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const prototype = path.resolve(here, "..");
const repository = path.resolve(here, "../../../../..");
const output =
  process.env.F0_REVIEW_OUTPUT ||
  path.join(repository, "output/ai/frontend-f0-review");
const pages = [
  "dashboard",
  "projects",
  "project",
  "tasks",
  "providers",
  "knowledge",
  "meetings",
  "inbox",
  "mcp",
  "oauth",
  "memory",
];
const scenarios = [
  "normal",
  "long",
  "empty",
  "loading",
  "error",
  "readonly",
  "unknown",
];
const args = process.argv.slice(2);
if (args.includes("--list")) {
  console.log(
    JSON.stringify(
      {
        pages,
        scenarios,
        viewports: ["1440x960/light", "390x844/dark"],
        boundary: "isolated F0 synthetic prototype; no business service",
      },
      null,
      2,
    ),
  );
  process.exit(0);
}
const selected = args[0] ? args[0].split(",") : pages;
const scenario = args[1] || "normal";
const extras = new URLSearchParams(args[2] || "");
const extraLabel = extras.size
  ? "--" +
    [...extras]
      .flat()
      .join("-")
      .replace(/[^a-zA-Z0-9-]/g, "_")
  : "";
const layouts = process.env.F0_REVIEW_LAYOUTS
  ? process.env.F0_REVIEW_LAYOUTS.split(",").map((value) => {
      const [width, height, theme] = value.split(":");
      if (
        !Number(width) ||
        !Number(height) ||
        !["light", "dark"].includes(theme)
      )
        throw new Error("Invalid review layout");
      return { width: Number(width), height: Number(height), theme };
    })
  : [
      { width: 1440, height: 960, theme: "light" },
      { width: 390, height: 844, theme: "dark" },
    ];
if (selected.some((p) => !pages.includes(p)) || !scenarios.includes(scenario))
  throw new Error("Unknown review input");
const require = createRequire(import.meta.url);
const { chromium } = require(
  process.env.F0_PLAYWRIGHT ||
    "/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules/@playwright/test",
);
const mime = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".pdf": "application/pdf",
};
const server = createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(
      new URL(request.url, "http://localhost").pathname,
    );
    const file = path.resolve(
      prototype,
      "." + (pathname === "/" ? "/index.html" : pathname),
    );
    if (!file.startsWith(prototype + path.sep)) {
      response.writeHead(403);
      response.end();
      return;
    }
    response.writeHead(200, {
      "Content-Type": mime[path.extname(file)] || "application/octet-stream",
    });
    response.end(await readFile(file));
  } catch {
    response.writeHead(404);
    response.end();
  }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
let browser;
const results = [];
try {
  await mkdir(output, { recursive: true });
  browser = await chromium.launch({
    executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
    args: ["--no-sandbox"],
  });
  for (const name of selected) {
    for (const viewport of layouts) {
      const context = await browser.newContext({
        viewport,
        colorScheme: viewport.theme,
        reducedMotion: "reduce",
        deviceScaleFactor: 1,
      });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await context.route("**/*", (route) => {
        const url = route.request().url();
        return [
          origin + "/",
          `blob:${origin}/`,
          "data:",
          "chrome://resources/",
          "chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai/",
        ].some((prefix) => url.startsWith(prefix))
          ? route.continue()
          : route.abort();
      });
      const query = new URLSearchParams({
        page: name,
        theme: viewport.theme,
        scenario,
        accent: "indigo",
      });
      for (const [key, value] of extras) query.set(key, value);
      await page.goto(`${origin}/index.html?${query}`);
      await page.locator("#main").waitFor();
      await page.evaluate(() => document.fonts.ready);
      await page.evaluate(async () => {
        await new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        );
        await Promise.all(
          document
            .getAnimations()
            .filter(
              (animation) =>
                animation.effect?.getComputedTiming().iterations !== Infinity,
            )
            .map((animation) => animation.finished.catch(() => {})),
        );
      });
      if (extras.get("doc") === "pdf") await page.waitForTimeout(2000); // Native PDF viewer initialization; verify actual pixels separately.
      const label = `${name}-${scenario}-${viewport.width}-${viewport.theme}${extraLabel}`;
      // Force the compositor to finish painting the requested theme before measurement.
      await page.screenshot({
        path: path.join(output, `${label}.png`),
        fullPage: true,
      });
      const observations = await page.evaluate(() => {
        const rgb = (value) => {
          const values = value.match(/[\d.]+/g)?.map(Number) || [];
          return [
            values[0] || 0,
            values[1] || 0,
            values[2] || 0,
            values[3] ?? 1,
          ];
        };
        const blend = (front, back) => [
          ...front
            .slice(0, 3)
            .map((v, i) => v * front[3] + back[i] * (1 - front[3])),
          1,
        ];
        const background = (el) => {
          const chain = [];
          for (let current = el; current; current = current.parentElement)
            chain.unshift(current);
          return chain.reduce(
            (color, current) =>
              blend(rgb(getComputedStyle(current).backgroundColor), color),
            [255, 255, 255, 1],
          );
        };
        const luminance = (color) =>
          color
            .slice(0, 3)
            .map((c) => c / 255)
            .map((c) =>
              c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4,
            )
            .reduce((sum, c, i) => sum + c * [0.2126, 0.7152, 0.0722][i], 0);
        const ratio = (a, b) =>
          (Math.max(luminance(a), luminance(b)) + 0.05) /
          (Math.min(luminance(a), luminance(b)) + 0.05);
        const candidates = [];
        for (const el of document.querySelectorAll("#app *")) {
          const style = getComputedStyle(el),
            rect = el.getBoundingClientRect();
          if (
            !rect.width ||
            !rect.height ||
            style.visibility === "hidden" ||
            el.closest("[disabled],[inert]")
          )
            continue;
          const text = [...el.childNodes]
            .filter((n) => n.nodeType === Node.TEXT_NODE)
            .map((n) => n.textContent.trim())
            .join(" ")
            .trim();
          if (!text) continue;
          const bg = background(el),
            fg = blend(rgb(style.color), bg),
            contrast = ratio(fg, bg);
          const large =
            parseFloat(style.fontSize) >= 24 ||
            (parseFloat(style.fontSize) >= 18.66 &&
              Number(style.fontWeight) >= 700);
          if (contrast + 0.01 < (large ? 3 : 4.5))
            candidates.push({
              text: text.slice(0, 110),
              contrast: Number(contrast.toFixed(2)),
              color: style.color,
              background: bg,
              font: `${style.fontSize}/${style.fontWeight}`,
              location: { x: rect.x, y: rect.y },
            });
        }
        return {
          title: document.querySelector("h1")?.textContent,
          documentWidth: document.documentElement.clientWidth,
          scrollWidth: document.documentElement.scrollWidth,
          pageHeight: document.documentElement.scrollHeight,
          contrastCandidates: candidates,
          headings: [...document.querySelectorAll("h1,h2,h3")].map((el) => ({
            level: el.tagName,
            text: el.textContent,
          })),
        };
      });
      results.push({
        label,
        input: {
          page: name,
          scenario,
          viewport,
          zoom: 1,
          reducedMotion: "reduce",
          accent: "indigo",
        },
        ...observations,
        errors,
      });
      await context.close();
    }
  }
  await writeFile(
    path.join(
      output,
      `observations-${selected.join("-")}-${scenario}${extraLabel}.json`,
    ),
    JSON.stringify(
      {
        browser: await browser.version(),
        boundary: "F0 synthetic prototype only",
        results,
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify(
      results.map(
        ({
          label,
          scrollWidth,
          documentWidth,
          contrastCandidates,
          errors,
        }) => ({
          label,
          overflow: scrollWidth > documentWidth,
          contrastCandidates,
          errors,
        }),
      ),
      null,
      2,
    ),
  );
} finally {
  if (browser) await browser.close();
  await new Promise((resolve) => server.close(resolve));
}
