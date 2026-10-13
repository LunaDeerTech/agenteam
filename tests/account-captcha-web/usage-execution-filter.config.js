import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_EVIDENCE;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_CASE !==
    "execution-filter-current-read"
)
  throw Error("OWNED_USAGE_EXECUTION_FILTER_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "https:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw Error("EXACT_OWNED_USAGE_EXECUTION_FILTER_PATHS_REQUIRED");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "usage-execution-filter.spec.ts",
  grep: /\[execution-filter-current-read\]$/,
  timeout: 45000,
  expect: { timeout: 5000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/usage-execution-filter-playwright-results`,
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
