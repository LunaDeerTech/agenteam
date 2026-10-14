import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

const directory = fileURLToPath(new URL(".", import.meta.url));
const output = fileURLToPath(
  new URL("../../output/ai/project-list-polish-ui", import.meta.url),
);
export default defineConfig({
  testDir: ".",
  testMatch: "project-list-polish.spec.ts",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${output}/results`,
  use: {
    baseURL: "http://127.0.0.1:5175",
    browserName: "chromium",
    launchOptions: {
      executablePath: "/usr/bin/chromium",
      args: ["--no-sandbox"],
    },
    screenshot: "only-on-failure",
    trace: "off",
    video: "off",
  },
  webServer: {
    command: "node project-list-polish.server.mjs",
    cwd: directory,
    url: "http://127.0.0.1:5175",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
