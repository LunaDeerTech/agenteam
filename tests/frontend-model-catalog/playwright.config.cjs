const path = require("node:path");
const { defineConfig } = require("@playwright/test");

// Account and proxy forms can contain private credentials. Never let Playwright
// save its implicit failure DOM, trace, video or screenshot for these entries.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = "1";
const output = path.resolve(
  process.env.MODEL_CATALOG_OUTPUT ||
    "output/ai/frontend-model-catalog/browser",
);
module.exports = defineConfig({
  testDir: __dirname,
  testMatch: ["*.spec.cjs"],
  timeout: 60000,
  expect: { timeout: 5000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: [
    ["line"],
    ["json", { outputFile: path.join(output, "results.json") }],
  ],
  outputDir: path.join(output, "results"),
  use: {
    viewport: { width: 1440, height: 960 },
    screenshot: "off",
    trace: "off",
    video: "off",
    ignoreHTTPSErrors: false,
    serviceWorkers: "block",
  },
});
