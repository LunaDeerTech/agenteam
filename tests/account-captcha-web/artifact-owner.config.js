import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";
const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_ARTIFACT_OWNER_WEB_EVIDENCE;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  process.env.AGENTEAM_ARTIFACT_OWNER_WEB_CASE !== "catalog-and-bounded-read"
)
  throw Error("OWNED_ARTIFACT_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw Error("EXACT_ARTIFACT_FIXTURE_REQUIRED");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "artifact-owner.spec.ts",
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/artifact-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
