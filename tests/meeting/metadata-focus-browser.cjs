// Historical component fixture; dependencies and source belong to web-crape.
const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const pwRoot = process.env.AGENTEAM_MEETING_WEB_PLAYWRIGHT;
if (!pwRoot || require(path.join(pwRoot, "package.json")).version !== "1.56.1")
  throw Error("LOCKED_PLAYWRIGHT_REQUIRED");
const { chromium } = require(pwRoot);
const { expect } = require(path.join(pwRoot, "test.js"));
if (process.argv.includes("--list")) {
  console.log(
    "Metadata original component focus: idle / read-before-close / close-before-read",
  );
} else {
  void (async () => {
    const repo = path.resolve(__dirname, "../..");
    const web = path.join(repo, "web-crape");
    const output = process.env.AGENTEAM_MEETING_FOCUS_EVIDENCE;
    if (!output || !path.isAbsolute(output))
      throw Error("PRIVATE_EVIDENCE_REQUIRED");
    fs.mkdirSync(output, { recursive: true, mode: 0o700 });
    const { createServer } = await import(
      pathToFileURL(require.resolve("vite", { paths: [web] })).href
    );
    const vue = (
      await import(
        pathToFileURL(require.resolve("@vitejs/plugin-vue", { paths: [web] }))
          .href
      )
    ).default;
    const entry = path.join(__dirname, "metadata-focus-app.ts");
    const server = await createServer({
      configFile: false,
      root: web,
      cacheDir: path.join(output, "vite-cache"),
      resolve: {
        alias: {
          vue: fs.realpathSync(
            path.join(web, "node_modules/vue/dist/vue.runtime.esm-bundler.js"),
          ),
        },
        dedupe: ["vue"],
      },
      plugins: [
        vue(),
        {
          name: "owned-metadata-focus-entry",
          configureServer(server) {
            server.middlewares.use((request, response, next) => {
              if (request.url !== "/__metadata_focus") return next();
              response.setHeader("Content-Type", "text/html; charset=utf-8");
              response.end(
                '<!doctype html><html><body><div id="app"></div><script type="module" src="/@fs/' +
                  entry +
                  '"></script></body></html>',
              );
            });
          },
        },
      ],
      server: {
        host: "127.0.0.1",
        port: 0,
        strictPort: false,
        fs: { allow: [repo] },
      },
    });
    const observations = [];
    let browser;
    try {
      await server.listen();
      const address = server.httpServer.address();
      if (
        !address ||
        typeof address === "string" ||
        address.address !== "127.0.0.1"
      )
        throw Error("OWNED_LOOPBACK_REQUIRED");
      const origin = "http://127.0.0.1:" + address.port;
      browser = await chromium.launch({
        executablePath: process.env.AGENTEAM_AUTH_WEB_CHROMIUM,
        args: ["--no-sandbox"],
      });
      for (const order of ["idle", "read-before-close", "close-before-read"]) {
        const context = await browser.newContext();
        const page = await context.newPage();
        page.setDefaultTimeout(8000);
        const observe = async (phase) => {
          observations.push({
            order,
            phase,
            ...(await page.evaluate(() =>
              window.metadataFocusFixture.observe(),
            )),
          });
          fs.writeFileSync(
            path.join(output, "observations.json"),
            JSON.stringify(observations),
            { mode: 0o600 },
          );
        };
        try {
          await page.goto(origin + "/__metadata_focus");
          const edit = page.getByRole("button", {
            name: "编辑成员",
            exact: true,
          });
          await edit.waitFor();
          if (order === "idle")
            await page.evaluate(() =>
              window.metadataFocusFixture.setReadMode("idle"),
            );
          await edit.click();
          const dialog = page.getByRole("dialog", {
            name: "编辑参会成员",
            exact: true,
          });
          await expect(dialog.locator(".ordered-items li > span")).toHaveText([
            "Agent 6",
            "Agent 5",
          ]);
          await observe("opened");
          if (order === "read-before-close") {
            await page.evaluate(() =>
              window.metadataFocusFixture.releaseRead(),
            );
            await expect(
              page.locator(".metadata-actions > button").first(),
            ).toBeEnabled();
          }
          await page.keyboard.press("Escape");
          await expect(dialog).toHaveCount(0);
          await observe("closed");
          if (order === "close-before-read") {
            await page.evaluate(() =>
              window.metadataFocusFixture.releaseRead(),
            );
            await expect(edit).toBeEnabled();
            await observe("read-completed");
          }
          await expect(edit).toBeFocused({ timeout: 1000 });
          await observe("restored");
        } finally {
          await context.close();
        }
      }
    } finally {
      if (browser) await browser.close();
      await server.close();
    }
  })().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
