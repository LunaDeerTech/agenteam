import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_M2_WEB_EVIDENCE;
const mode = process.env.AGENTEAM_M2_WEB_CASE;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  !["accept", "return-and-accept", "material-check"].includes(mode)
)
  throw Error("OWNED_M2_REVIEW_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw Error("EXACT_M2_REVIEW_FIXTURE_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "m2-task-review.spec.ts",
  grep:
    mode === "material-check"
      ? /M2 live material/
      : new RegExp(`\\[${mode}\\]$`),
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/m2-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
