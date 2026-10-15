// Independent, bounded diagnosis of the user-rejected F0. No product mutations.
import { createServer } from "node:http";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const repository = path.resolve(here, "../../../../..");
const sourcePrefix = "docs/frontend-design/redesign/prototype/";
const revision = process.env.F0_REVIEW_REV || "b6db5297";
const output =
  process.env.F0_REVIEW_OUTPUT ||
  path.join(repository, "output/ai/frontend-f0-refinement-review/diagnostic");
const references = [
  [
    "linear2026-borders",
    "https://linear.app/now/behind-the-latest-design-refresh",
    "https://webassets.linear.app/images/ornj730p/production/67561baa677fbc429d94edd080e95aecabb6bae2-3904x2720.png",
    "2026 official detail comparison; not a complete product viewport",
  ],
  [
    "linear2026-navigation",
    "https://linear.app/now/behind-the-latest-design-refresh",
    "https://webassets.linear.app/images/ornj730p/production/b6d6be14c96978b10553cfb9205be1065087e793-3904x2720.png",
    "2026 official navigation detail; not a complete product viewport",
  ],
  [
    "linear2024-after",
    "https://linear.app/now/how-we-redesigned-the-linear-ui",
    "https://webassets.linear.app/images/ornj730p/production/9b91020243984487b4e0cbe72278dd1acd7f9c57-2352x1380.png",
    "2024 official full product composition; historical reference",
  ],
  [
    "linear-preferences",
    "https://linear.app/docs/account-preferences",
    "https://webassets.linear.app/images/ornj730p/production/4f79d61790a704d7e47e00611e8615a30f932979-2084x1633.png",
    "Official settings illustration; capture date/version unspecified",
  ],
  [
    "linear-priority-menu",
    "https://linear.app/docs/issue-property",
    "https://webassets.linear.app/images/ornj730p/production/cf0e427cf3138f660c031595d5fdd4f95637e3c9-1752x1028.png",
    "Official open menu illustration; capture date/version unspecified",
  ],
];
if (process.argv.includes("--list")) {
  console.log(
    JSON.stringify(
      {
        revision,
        pages: ["projects", "tasks", "providers"],
        interactions: [
          "filter open",
          "row hover",
          "keyboard focus",
          "create dialog normal and reduced motion",
        ],
        references: references.map(([name, source, , limit]) => ({
          name,
          source,
          limit,
        })),
        boundary: "Synthetic F0; observation output is not a visual PASS",
      },
      null,
      2,
    ),
  );
  process.exit(0);
}
await mkdir(output, { recursive: true });
const require = createRequire(import.meta.url);
const { chromium } = require(
  process.env.F0_PLAYWRIGHT ||
    "/workspace/shared/node/account-captcha-ac60fa262f0c44ae-node24.19.0-linux-x64/node_modules/@playwright/test",
);
const cache = new Map();
const server = createServer(async (request, response) => {
  try {
    const name =
      decodeURIComponent(
        new URL(request.url, "http://localhost").pathname,
      ).replace(/^\//, "") || "index.html";
    if (name.includes("..")) throw new Error("Path outside owned source");
    const isReference = name.startsWith("_reference/");
    let data;
    if (isReference) data = await readFile(path.join(output, name.slice(11)));
    else if (revision === "working")
      data = await readFile(path.join(repository, sourcePrefix, name));
    else {
      if (!cache.has(name))
        cache.set(
          name,
          execFileSync("git", ["show", `${revision}:${sourcePrefix}${name}`], {
            cwd: repository,
          }),
        );
      data = cache.get(name);
    }
    response.writeHead(200, {
      "Content-Type":
        {
          ".html": "text/html",
          ".js": "text/javascript",
          ".css": "text/css",
          ".png": "image/png",
        }[path.extname(name)] || "application/octet-stream",
    });
    response.end(data);
  } catch {
    response.writeHead(404);
    response.end();
  }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const report = {
  revision,
  origin,
  viewport: { width: 1440, height: 960 },
  boundary: "Synthetic prototype, no live service or business writes",
  pages: [],
  interactions: [],
  references: [],
};
let browser;
try {
  browser = await chromium.launch({
    executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
    args: ["--no-sandbox"],
  });
  report.browser = browser.version();
  report.node = process.version;
  const context = await browser.newContext({
    viewport: report.viewport,
    colorScheme: "light",
    reducedMotion: "no-preference",
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
  const screenshot = (name, fullPage = false) =>
    page.screenshot({
      path: path.join(output, `${name}.png`),
      fullPage,
      animations: "allow",
    });
  for (const name of ["projects", "tasks", "providers"]) {
    await page.goto(
      `${origin}/index.html?page=${name}&theme=light&scenario=normal`,
    );
    await page.evaluate(() => document.fonts.ready);
    await screenshot(`${name}-1440-light-viewport`);
    await screenshot(`${name}-1440-light-full`, true);
    report.pages.push(
      await page.evaluate(() => {
        const read = (selector) =>
          [...document.querySelectorAll(selector)].map((el) => {
            const r = el.getBoundingClientRect(),
              s = getComputedStyle(el);
            return {
              text: el.textContent.trim().slice(0, 100),
              x: r.x,
              y: r.y,
              width: r.width,
              height: r.height,
              font: s.font,
              background: s.backgroundColor,
              border: s.borderColor,
              transition: s.transitionDuration,
              animation: s.animationDuration,
            };
          });
        return {
          page: document.body.dataset.page,
          viewport: { width: innerWidth, height: innerHeight },
          documentHeight: document.documentElement.scrollHeight,
          headings: read("h1,h2,h3"),
          rows: read(".project-row,.field-row"),
          surfaces: read(
            ".system-header,.project-header,.view-bar,.board,.task-card",
          ),
          controls: read("select,input,.button.primary"),
          fonts: [...document.fonts].map((f) => ({
            family: f.family,
            status: f.status,
          })),
        };
      }),
    );
  }
  await page.goto(
    `${origin}/index.html?page=projects&theme=light&scenario=normal`,
  );
  await page.locator(".project-row").first().hover();
  await screenshot("project-row-hover");
  report.interactions.push({
    name: "row hover",
    background: await page
      .locator(".project-row")
      .first()
      .evaluate((el) => getComputedStyle(el).backgroundColor),
  });
  await page.locator("#project-filter").click();
  await screenshot("project-filter-open");
  await page.keyboard.press("Escape");
  await page.keyboard.press("Tab");
  await screenshot("project-keyboard-focus");
  report.interactions.push({
    name: "keyboard focus",
    focus: await page.evaluate(() => ({
      text: document.activeElement.textContent.trim(),
      tag: document.activeElement.tagName,
      outline: getComputedStyle(document.activeElement).outline,
      focusVisible: document.activeElement.matches(":focus-visible"),
    })),
  });
  for (const reducedMotion of ["no-preference", "reduce"]) {
    await page.emulateMedia({ reducedMotion });
    await page.evaluate(() => {
      window.__motionFrames = [];
      document.addEventListener(
        "click",
        () => {
          const start = performance.now();
          function sample() {
            const dialog = document.querySelector(
              "dialog[open], [role=dialog]",
            );
            const s = dialog && getComputedStyle(dialog);
            window.__motionFrames.push({
              elapsed: performance.now() - start,
              animations: document.getAnimations().map((a) => ({
                duration: a.effect.getComputedTiming().duration,
                currentTime: a.currentTime,
                playState: a.playState,
              })),
              dialog: s && {
                opacity: s.opacity,
                transform: s.transform,
                transition: s.transitionDuration,
                animation: s.animationDuration,
              },
            });
            if (performance.now() - start < 300) requestAnimationFrame(sample);
          }
          requestAnimationFrame(sample);
        },
        { once: true, capture: true },
      );
    });
    await page.getByRole("button", { name: "新建项目", exact: true }).click();
    await screenshot(`dialog-${reducedMotion}-early`);
    await page.waitForFunction(
      () => window.__motionFrames.at(-1)?.elapsed >= 300,
    );
    await screenshot(`dialog-${reducedMotion}-settled`);
    report.interactions.push({
      name: "create dialog motion",
      reducedMotion,
      frames: await page.evaluate(() => window.__motionFrames),
    });
    await page.keyboard.press("Escape");
  }
  await context.close();
  if (process.argv.includes("--references")) {
    const referenceContext = await browser.newContext({
      viewport: report.viewport,
      deviceScaleFactor: 1,
    });
    const referencePage = await referenceContext.newPage();
    await referenceContext.route("**/*", (route) =>
      route
        .request()
        .url()
        .startsWith(origin + "/")
        ? route.continue()
        : route.abort(),
    );
    for (const [name, source, asset, limitation] of references) {
      const response = await fetch(asset);
      if (!response.ok)
        throw new Error(`Reference fetch failed: ${response.status}`);
      await writeFile(
        path.join(output, `${name}.png`),
        Buffer.from(await response.arrayBuffer()),
      );
      await referencePage.goto(origin);
      await referencePage.setContent(
        `<body style="margin:0;background:#eee;display:grid;place-items:center;height:960px"><img src="${origin}/_reference/${name}.png" style="max-width:1440px;max-height:900px;object-fit:contain"><p style="margin:0;font:12px sans-serif">${source} — ${limitation}</p></body>`,
      );
      await referencePage.locator("img").evaluate((img) => img.decode());
      await referencePage.screenshot({
        path: path.join(output, `${name}-1440x960-reference.png`),
      });
      report.references.push({
        name,
        source,
        asset,
        fetchedAt: new Date().toISOString(),
        limitation,
        image: await referencePage.locator("img").evaluate((img) => ({
          width: img.naturalWidth,
          height: img.naturalHeight,
        })),
      });
    }
    await referenceContext.close();
  }
} finally {
  await browser?.close();
  await new Promise((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  report.resourcesClosed = true;
  await writeFile(
    path.join(output, "diagnostic.json"),
    JSON.stringify(report, null, 2),
  );
}
console.log(
  JSON.stringify({
    output,
    revision,
    pages: report.pages.length,
    interactions: report.interactions.length,
    references: report.references.length,
    resourcesClosed: report.resourcesClosed,
  }),
);
