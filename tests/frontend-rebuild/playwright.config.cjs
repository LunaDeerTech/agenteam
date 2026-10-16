const path = require("node:path");
const { defineConfig } = require("@playwright/test");

const output = path.resolve(
  process.env.VUE_TEST_OUTPUT || "output/ai/vue-rebuild-test/browser",
);
const port = Number(process.env.VUE_TEST_PORT || 5242);
module.exports = defineConfig({
  testDir: __dirname,
  testMatch: "foundation.spec.cjs",
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
      executablePath: process.env.VUE_TEST_CHROMIUM || "/usr/bin/chromium",
      args: ["--no-sandbox"],
    },
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "off",
  },
  webServer: {
    command: "exec node tests/frontend-rebuild/preview-server.mjs",
    cwd: path.resolve(__dirname, "../.."),
    url: `http://127.0.0.1:${port}/system/users`,
    reuseExistingServer: false,
    gracefulShutdown: { signal: "SIGTERM", timeout: 5000 },
    timeout: 30000,
  },
});
