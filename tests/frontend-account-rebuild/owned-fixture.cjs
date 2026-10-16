const fs = require("node:fs");
const path = require("node:path");
const { createHash, X509Certificate } = require("node:crypto");
const { test: base, expect, chromium } = require("@playwright/test");

function loadOwnedRuntime() {
  const filename = process.env.FRONTEND_ACCOUNT_RUNTIME;
  if (!filename || !path.isAbsolute(filename))
    throw new Error("OWNED_ACCOUNT_RUNTIME_REQUIRED");
  const stat = fs.lstatSync(filename);
  const parent = fs.lstatSync(path.dirname(filename));
  if (
    !stat.isFile() ||
    stat.isSymbolicLink() ||
    (stat.mode & 0o777) !== 0o600 ||
    !parent.isDirectory() ||
    parent.isSymbolicLink() ||
    (parent.mode & 0o777) !== 0o700 ||
    stat.uid !== process.getuid() ||
    parent.uid !== process.getuid()
  )
    throw new Error("OWNED_ACCOUNT_RUNTIME_PERMISSIONS_REQUIRED");
  let runtime;
  try {
    runtime = JSON.parse(fs.readFileSync(filename, "utf8"));
  } catch {
    throw new Error("OWNED_ACCOUNT_RUNTIME_INVALID");
  }
  if (
    runtime.origin !== "https://localhost:5250" ||
    !/^[a-f0-9]{32}$/.test(runtime.nonce || "") ||
    !/^[A-Za-z0-9+/]{43}=$/.test(runtime.certificate_spki || "") ||
    typeof runtime.credentials?.email !== "string" ||
    typeof runtime.credentials?.password !== "string" ||
    !runtime.credentials.password ||
    typeof runtime.certificate_file !== "string"
  )
    throw new Error("OWNED_ACCOUNT_RUNTIME_CONTRACT_MISMATCH");
  const certificate = new X509Certificate(
    fs.readFileSync(runtime.certificate_file),
  );
  const actual = createHash("sha256")
    .update(certificate.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
  if (
    actual !== runtime.certificate_spki ||
    !certificate.checkHost("localhost") ||
    Date.parse(certificate.validFrom) > Date.now() ||
    Date.parse(certificate.validTo) <= Date.now()
  )
    throw new Error("OWNED_ACCOUNT_CERTIFICATE_MISMATCH");
  return runtime;
}

const test = base.extend({
  owned: [
    async ({}, use) => {
      await use(loadOwnedRuntime());
    },
    { scope: "worker" },
  ],
  browser: [
    async ({ owned }, use) => {
      const browser = await chromium.launch({
        executablePath:
          process.env.FRONTEND_ACCOUNT_CHROMIUM || "/usr/bin/chromium",
        headless: true,
        args: [
          "--no-sandbox",
          `--ignore-certificate-errors-spki-list=${owned.certificate_spki}`,
        ],
      });
      try {
        await use(browser);
      } finally {
        await browser.close();
      }
    },
    { scope: "worker" },
  ],
  baseURL: async ({ owned }, use) => {
    await use(owned.origin);
  },
  page: async ({ page, owned }, use) => {
    const response = await page.goto(`${owned.origin}/__fixture__/identity`);
    if (!response || response.status() !== 200)
      throw new Error("OWNED_ACCOUNT_IDENTITY_UNAVAILABLE");
    let identity;
    try {
      identity = await response.json();
    } catch {
      throw new Error("OWNED_ACCOUNT_IDENTITY_INVALID");
    }
    if (identity.nonce !== owned.nonce)
      throw new Error("OWNED_ACCOUNT_IDENTITY_MISMATCH");
    await use(page);
  },
});

// Only safe status and necessary non-secret facts leave the browser evaluation.
async function sessionStatus(page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    return response.status;
  });
}

async function fillPrivate(locator, value) {
  try {
    await locator.fill(value, { timeout: 5000 });
  } catch {
    throw new Error("PRIVATE_FIELD_INTERACTION_FAILED");
  }
}

async function openPrivateLink(page, url) {
  try {
    await page.goto(url, { timeout: 15000 });
  } catch {
    throw new Error("PRIVATE_LINK_NAVIGATION_FAILED");
  }
}

module.exports = { test, expect, sessionStatus, fillPrivate, openPrivateLink };
