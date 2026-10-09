import { defineConfig } from "@playwright/test";
import { lstatSync } from "node:fs";
import { isAbsolute, join } from "node:path";

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE;
const dist = process.env.AGENTEAM_PROJECT_MODELS_WEB_DIST;
const inputHash = process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH;
const selected = process.env.AGENTEAM_PROJECT_MODELS_WEB_CASE;
const modes = [
  "configuration",
  "credential",
  "recovery",
  "read",
  "authority",
  "navigation",
];
if (
  !origin ||
  ![directory, executablePath, evidence, dist].every(
    (value) => typeof value === "string" && isAbsolute(value),
  ) ||
  !/^[0-9a-f]{64}$/.test(inputHash ?? "") ||
  !modes.includes(selected)
)
  throw new Error("OWNED_PROJECT_MODELS_FIXTURE_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin
)
  throw new Error("EXACT_OWNED_PROJECT_MODELS_ORIGIN_REQUIRED");
for (const path of [directory, evidence, dist]) {
  const stat = lstatSync(path);
  if (stat.isSymbolicLink() || !stat.isDirectory())
    throw new Error("EXACT_OWNED_PROJECT_MODELS_DIRECTORY_REQUIRED");
}
if (!lstatSync(join(dist, "index.html")).isFile())
  throw new Error("PROJECT_MODELS_PRODUCTION_ASSETS_REQUIRED");
if (
  selected === "navigation" &&
  !isAbsolute(process.env.AGENTEAM_AUTH_WEB_IMAGES ?? "")
)
  throw new Error("PROJECT_MODELS_IMAGE_DIRECTORY_REQUIRED");

export default defineConfig({
  testDir: "./e2e",
  testMatch: "project-owner-models.spec.ts",
  grep: new RegExp(`\\[${selected}\\]`),
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: join(directory, "project-models-playwright-results"),
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
