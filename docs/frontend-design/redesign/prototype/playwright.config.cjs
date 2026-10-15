const path = require("node:path");
const { defineConfig } = require("@playwright/test");
const output = path.resolve(
  process.env.F0_OUTPUT ||
    path.resolve(__dirname, "../../../../output/ai/frontend-f0"),
);
const port = Number(process.env.F0_PORT || 5188);
module.exports = defineConfig({
  testDir: __dirname,
  testMatch: "playwright.spec.cjs",
  timeout: 30000,
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
    baseURL: `http://127.0.0.1:${port}`,
    browserName: "chromium",
    launchOptions: {
      executablePath: process.env.F0_CHROMIUM || "/usr/bin/chromium",
      args: ["--no-sandbox"],
    },
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "off",
  },
  webServer: {
    command: "exec node test-server.mjs",
    cwd: __dirname,
    url: `http://127.0.0.1:${port}/__f0_health`,
    reuseExistingServer: false,
    gracefulShutdown: { signal: "SIGTERM", timeout: 5000 },
    timeout: 30000,
  },
});
