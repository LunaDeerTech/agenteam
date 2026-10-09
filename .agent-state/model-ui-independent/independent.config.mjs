import { createRequire } from "node:module";
import { lstatSync } from "node:fs";
import { dirname, isAbsolute, join } from "node:path";
import { fileURLToPath } from "node:url";

const source = dirname(fileURLToPath(import.meta.url));
const require = createRequire(
  join(source, "../../tests/account-captcha-web/package.json"),
);
const { defineConfig } = require("@playwright/test");
const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN;
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE;
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM;
const evidence = process.env.AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE;
const selected = process.env.MODELS_INDEPENDENT_CASE;
if (
  !origin ||
  !["a", "b"].includes(selected) ||
  !/^[0-9a-f]{64}$/.test(
    process.env.AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH ?? "",
  ) ||
  ![directory, evidence, executablePath].every(
    (value) => typeof value === "string" && isAbsolute(value),
  )
)
  throw new Error("INDEPENDENT_OWNED_INPUT_REQUIRED");
const url = new URL(origin);
if (
  url.protocol !== "http:" ||
  url.hostname !== "127.0.0.1" ||
  !url.port ||
  url.origin !== origin
)
  throw new Error("INDEPENDENT_OWNED_ORIGIN_REQUIRED");
for (const path of [directory, evidence]) {
  const stat = lstatSync(path);
  if (!stat.isDirectory() || stat.isSymbolicLink())
    throw new Error("INDEPENDENT_PRIVATE_DIRECTORY_REQUIRED");
}
export default defineConfig({
  testDir: source,
  testMatch: "independent.spec.ts",
  grep: new RegExp(`\\[independent-${selected}\\]`),
  timeout: 45_000,
  expect: { timeout: 5_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: "line",
  outputDir: join(directory, "independent-playwright"),
  use: {
    baseURL: origin,
    browserName: "chromium",
    launchOptions: { executablePath, args: ["--no-sandbox"] },
    screenshot: "off",
    video: "off",
    trace: "off",
  },
});
