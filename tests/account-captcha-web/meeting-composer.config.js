import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_MEETING_WEB_EVIDENCE;
if (!origin || !directory || !executablePath || !evidence)
  throw new Error("OWNED_MEETING_COMPOSER_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin
)
  throw new Error("EXACT_OWNED_ORIGIN_REQUIRED");
if (
  ![directory, executablePath, evidence].every(isAbsolute) ||
  process.env.AGENTEAM_MEETING_COMPOSER_CASE !== "inline-original"
)
  throw new Error("EXACT_MEETING_COMPOSER_CASE_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "meeting-composer.spec.ts",
  timeout: 45_000,
  expect: { timeout: 8_000 },
  workers: 1,
  retries: 0,
  reporter: "line",
  outputDir: `${directory}/composer-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    viewport: { width: 1280, height: 900 },
    colorScheme: "light",
    reducedMotion: "reduce",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
