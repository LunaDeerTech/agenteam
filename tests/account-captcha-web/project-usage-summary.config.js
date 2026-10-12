import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_EVIDENCE;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  process.env.AGENTEAM_PROJECT_USAGE_SUMMARY_WEB_CASE !==
    "daily-server-aggregate"
)
  throw Error("OWNED_PROJECT_USAGE_SUMMARY_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "https:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw Error("EXACT_OWNED_PROJECT_USAGE_SUMMARY_PATHS_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "project-usage-summary.spec.ts",
  grep: /\[daily-server-aggregate\]$/,
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/project-usage-summary-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    timezoneId: "America/Los_Angeles",
    ignoreHTTPSErrors: true,
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
