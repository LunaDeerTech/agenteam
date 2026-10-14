import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";
const directory = fileURLToPath(new URL(".", import.meta.url));
const output = fileURLToPath(
  new URL("../../output/ai/interface-layout", import.meta.url),
);
export default defineConfig({
  testDir: ".",
  testMatch: "interface-layout.spec.ts",
  timeout: 35000,
  expect: { timeout: 5000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: [["line"], ["json", { outputFile: `${output}/results.json` }]],
  outputDir: `${output}/results`,
  use: {
    baseURL: "http://127.0.0.1:5177",
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
    command: "node interface-layout.server.mjs",
    cwd: directory,
    url: "http://127.0.0.1:5177",
    reuseExistingServer: false,
    timeout: 30000,
  },
});
