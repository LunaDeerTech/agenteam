import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";
const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_PROJECT_OWNER_WEB_EVIDENCE;
const selected = process.env.AGENTEAM_PROJECT_VARIABLE_WEB_CASE;
if (!origin || !directory || !executablePath || !evidence)
  throw new Error("OWNED_VARIABLE_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin ||
  ![directory, executablePath, evidence].every(isAbsolute)
)
  throw new Error("EXACT_VARIABLE_PATHS_REQUIRED");
if (
  !selected ||
  !["read", "crud", "recovery", "identity", "authority", "layouts"].includes(
    selected,
  )
)
  throw new Error("EXACT_VARIABLE_CASE_REQUIRED");
export default defineConfig({
  testDir: "./e2e",
  testMatch: "project-variables.spec.ts",
  grep: new RegExp(`\\[${selected}\\]`),
  timeout: 45000,
  expect: { timeout: 5000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: `${directory}/project-variables-playwright-results`,
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
