import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_SKILL_IMPORT_WEB_EVIDENCE;
const dist = process.env.AGENTEAM_SKILL_IMPORT_WEB_DIST;
const inputHash = process.env.AGENTEAM_SKILL_IMPORT_WEB_INPUT_HASH;
if (
  !origin ||
  !directory ||
  !executablePath ||
  !evidence ||
  !dist ||
  !inputHash ||
  process.env.AGENTEAM_SKILL_IMPORT_WEB_CASE !==
    "existing-file-import-and-fixed-revision-download"
)
  throw Error("OWNED_SKILL_IMPORT_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence, dist].every(isAbsolute) ||
  !/^[0-9a-f]{64}$/.test(inputHash)
)
  throw Error("EXACT_SKILL_IMPORT_PATHS_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "skill-package-import.spec.ts",
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/skill-import-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
