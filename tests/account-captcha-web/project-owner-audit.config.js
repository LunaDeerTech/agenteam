import { defineConfig } from "@playwright/test";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const selected = process.env.AGENTEAM_PROJECT_AUDIT_WEB_CASE;
if (!origin || !directory || !executablePath)
  throw new Error("OWNED_PROJECT_AUDIT_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin
)
  throw new Error("EXACT_OWNED_ORIGIN_REQUIRED");
if (!selected || !["read", "authority", "navigation"].includes(selected))
  throw new Error("EXACT_PROJECT_AUDIT_CASE_REQUIRED");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "project-owner-audit.spec.ts",
  grep: new RegExp(`\\[${selected}\\]`),
  timeout: 45_000,
  workers: 1,
  retries: 0,
  reporter: "line",
  outputDir: `${directory}/project-audit-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
