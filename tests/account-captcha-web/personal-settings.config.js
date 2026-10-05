import { defineConfig } from "@playwright/test";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const selected = process.env.AGENTEAM_PERSONAL_WEB_CASE;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
if (!origin || !directory || !executablePath)
  throw new Error("OWNED_PERSONAL_SETTINGS_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin
)
  throw new Error("EXACT_OWNED_ORIGIN_REQUIRED");
if (!["profile", "theme", "password", "authority"].includes(selected))
  throw new Error("EXACT_PERSONAL_SETTINGS_CASE_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "personal-settings.spec.ts",
  grep: new RegExp(`\\[${selected}\\]`),
  timeout: 45_000,
  workers: 1,
  retries: 0,
  reporter: "line",
  outputDir: `${directory}/personal-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
