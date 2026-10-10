import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";
const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_SECRET_OWNER_WEB_EVIDENCE;
const dist = process.env.AGENTEAM_SECRET_OWNER_WEB_DIST;
const inputHash = process.env.AGENTEAM_SECRET_OWNER_WEB_INPUT_HASH;
const python = process.env.AGENTEAM_SECRET_OWNER_WEB_SCHEMA_PYTHON;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  !dist ||
  !python ||
  !inputHash
)
  throw Error("OWNED_SECRET_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence, dist, python].every(isAbsolute) ||
  !/^[0-9a-f]{64}$/.test(inputHash) ||
  process.env.AGENTEAM_SECRET_OWNER_WEB_CASE !== "owner"
)
  throw Error("EXACT_SECRET_FIXTURE_REQUIRED");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "project-secret-owner.spec.ts",
  grep: /(?:^| )project-secret-owner\.spec\.ts \[owner\] Project Secret lifecycle$/,
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/secret-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    viewport: { width: 1280, height: 900 },
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
