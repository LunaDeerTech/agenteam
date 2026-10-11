import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_EVIDENCE;
const selected = process.env.AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB_CASE;
if (
  process.env.PLAYWRIGHT_NO_COPY_PROMPT !== "1" ||
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  !selected ||
  !["system-runner-normal", "system-runner-lost"].includes(
    selected,
  )
)
  throw new Error("OWNED_SYSTEM_RUNNER_MANAGEMENT_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw new Error("EXACT_OWNED_SYSTEM_RUNNER_MANAGEMENT_PATHS_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "system-runner-management.spec.ts",
  grep: new RegExp(`^.*\\[${selected}\\]$`),
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/system-runner-management-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
