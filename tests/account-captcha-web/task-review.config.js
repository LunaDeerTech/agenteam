import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE;
const selected = process.env.AGENTEAM_TASK_REVIEW_WEB_CASE;
const cases = [
  "review-complete",
  "lost-confirmation-lookup-and-session-revocation",
];
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  !selected ||
  !cases.includes(selected)
)
  throw new Error("OWNED_TASK_REVIEW_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw new Error("EXACT_OWNED_TASK_REVIEW_PATHS_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "task-review.spec.ts",
  grep: new RegExp(`^.*\\[${selected}\\]$`),
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/task-review-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
