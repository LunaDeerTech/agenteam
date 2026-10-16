const path = require("node:path");
const { defineConfig } = require("@playwright/test");

const output = path.resolve(
  process.env.FRONTEND_ACCOUNT_OUTPUT ||
    "output/ai/frontend-account-rebuild/browser",
);

module.exports = defineConfig({
  testDir: __dirname,
  testMatch: "*.spec.cjs",
  timeout: 45000,
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
    // Account requests and pages carry private credentials and one-time tokens.
    // Keep raw trace, video, screenshots and response bodies out of artifacts.
    screenshot: "off",
    trace: "off",
    video: "off",
    ignoreHTTPSErrors: false,
  },
});
