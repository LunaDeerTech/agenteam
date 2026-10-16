const fs = require("node:fs");
const path = require("node:path");
const { X509Certificate, createHash } = require("node:crypto");
const { test: base, expect, chromium } = require("@playwright/test");
const {
  observeOwnedCertificate,
} = require("../frontend-account-rebuild/fixed-runtime.cjs");

const origin = "https://localhost:5260";
function privateJSON(filename) {
  if (!path.isAbsolute(filename) || fs.realpathSync(filename) !== filename)
    throw new Error();
  const stat = fs.lstatSync(filename);
  const parent = fs.lstatSync(path.dirname(filename));
  if (
    !stat.isFile() ||
    stat.isSymbolicLink() ||
    (stat.mode & 0o777) !== 0o600 ||
    stat.uid !== process.getuid() ||
    !parent.isDirectory() ||
    parent.isSymbolicLink() ||
    (parent.mode & 0o777) !== 0o700 ||
    parent.uid !== process.getuid()
  )
    throw new Error();
  return JSON.parse(fs.readFileSync(filename, "utf8"));
}
function loadRuntime() {
  try {
    const filename = process.env.OUTBOUND_POLICY_RUNTIME;
    const runtime = privateJSON(filename);
    const sidecar = privateJSON(
      path.join(path.dirname(filename), "outbound-owned.json"),
    );
    const credentials = (value) =>
      typeof value?.email === "string" &&
      !!value.email &&
      typeof value?.password === "string" &&
      !!value.password;
    if (
      runtime.origin !== origin ||
      !/^[a-f0-9]{32}$/.test(runtime.nonce || "") ||
      !Number.isFinite(Date.parse(runtime.expires_at)) ||
      Date.parse(runtime.expires_at) <= Date.now() ||
      !credentials(runtime.credentials) ||
      !credentials(runtime.regular_credentials) ||
      Object.keys(sidecar).sort().join() !== "nonce,runtime_file,test_scope" ||
      sidecar.test_scope !== "system-outbound-policy-proxy" ||
      sidecar.runtime_file !== filename ||
      sidecar.nonce !== runtime.nonce
    )
      throw new Error();
    const certificates = [
      runtime.certificate_file,
      runtime.certificate_ca_file,
    ].map((file) => {
      if (
        !path.isAbsolute(file) ||
        fs.realpathSync(file) !== file ||
        path.dirname(file) !== path.dirname(filename)
      )
        throw new Error();
      const stat = fs.lstatSync(file);
      if (!stat.isFile() || stat.uid !== process.getuid()) throw new Error();
      return new X509Certificate(fs.readFileSync(file));
    });
    const [leaf, ca] = certificates;
    const spki = createHash("sha256")
      .update(leaf.publicKey.export({ type: "spki", format: "der" }))
      .digest("base64");
    if (
      !ca.ca ||
      !leaf.checkIssued(ca) ||
      !leaf.verify(ca.publicKey) ||
      !leaf.checkHost("localhost") ||
      spki !== runtime.certificate_spki ||
      certificates.some(
        (cert) =>
          Date.parse(cert.validFrom) > Date.now() ||
          Date.parse(cert.validTo) <= Date.now(),
      )
    )
      throw new Error();
    return runtime;
  } catch {
    throw new Error("OWNED_OUTBOUND_RUNTIME_OR_TLS_SCOPE_INVALID");
  }
}

const test = base.extend({
  owned: [
    async ({}, use) => {
      await use(loadRuntime());
    },
    { scope: "worker" },
  ],
  browser: [
    async ({ owned }, use) => {
      const browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
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
  page: async ({ page, owned }, use, testInfo) => {
    const certificate = await observeOwnedCertificate(page, owned);
    try {
      const response = await page.goto(`${origin}/__fixture__/identity`);
      certificate.verify();
      if (
        response?.status() !== 200 ||
        (await response.json()).nonce !== owned.nonce
      )
        throw new Error();
    } catch {
      throw new Error("OWNED_OUTBOUND_INSTANCE_IDENTITY_REJECTED");
    } finally {
      await certificate.close();
    }
    const statuses = [];
    page.on("response", (response) => {
      const pathname = new URL(response.url()).pathname;
      if (
        /^\/api\/v1\/(?:session|sessions\/(?:login|logout)|system\/outbound-policy(?:\/proxy)?)$/.test(
          pathname,
        )
      )
        statuses.push({
          path: pathname,
          method: response.request().method(),
          status: response.status(),
        });
    });
    try {
      await use(page);
    } finally {
      await page.unrouteAll({ behavior: "wait" });
      try {
        await closeSession(page);
      } finally {
        if (testInfo.status !== testInfo.expectedStatus)
          await testInfo.attach("safe-outbound-status", {
            body: Buffer.from(JSON.stringify(statuses)),
            contentType: "application/json",
          });
      }
    }
  },
});

async function fillPrivate(locator, value) {
  try {
    await locator.fill(value, { timeout: 5000 });
  } catch {
    throw new Error("PRIVATE_FIELD_INTERACTION_FAILED");
  }
}
async function login(page, owned) {
  await page.goto("/system/outbound-policy");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await fillPrivate(
    page.getByLabel("邮箱", { exact: true }),
    owned.credentials.email,
  );
  await fillPrivate(
    page.getByLabel("密码", { exact: true }),
    owned.credentials.password,
  );
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(`${origin}/system/outbound-policy`);
  await expect(
    page.getByRole("button", { name: "添加规则", exact: true }),
  ).toBeEnabled();
}
// DTOs and secret request material stay in memory. Tests only assert booleans
// for private comparisons; this helper never attaches response bodies.
async function api(page, pathname, method = "GET", body, options = {}) {
  try {
    return await page.evaluate(
      async ({ pathname, method, body, options }) => {
        const headers = {};
        if (method !== "GET") {
          headers["Content-Type"] = "application/json";
          headers["Idempotency-Key"] = crypto.randomUUID();
          if (!options.omitCSRF) {
            const session = await fetch("/api/v1/session", {
              cache: "no-store",
            });
            if (session.status !== 200) throw new Error();
            headers["X-CSRF-Token"] = (await session.json()).csrf_token;
          }
        }
        const response = await fetch(`/api/v1${pathname}`, {
          method,
          headers,
          cache: "no-store",
          ...(body === undefined ? {} : { body: JSON.stringify(body) }),
        });
        const text = await response.text();
        return {
          status: response.status,
          value: text ? JSON.parse(text) : undefined,
        };
      },
      { pathname, method, body, options },
    );
  } catch {
    throw new Error("OWNED_OUTBOUND_API_OPERATION_FAILED");
  }
}
async function loginHTTP(page, credentials) {
  try {
    return await page.evaluate(async (credentials) => {
      const bootstrap = await fetch("/api/v1/auth/bootstrap");
      if (bootstrap.status !== 200) throw new Error();
      const csrf = (await bootstrap.json()).csrf_token;
      const response = await fetch("/api/v1/sessions/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": csrf,
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: JSON.stringify(credentials),
      });
      await response.arrayBuffer();
      return response.status;
    }, credentials);
  } catch {
    throw new Error("OWNED_OUTBOUND_HTTP_LOGIN_FAILED");
  }
}
async function closeSession(page) {
  const current = await api(page, "/session");
  if (current.status === 401) return;
  if (
    current.status !== 200 ||
    (await api(page, "/sessions/logout", "POST", {})).status !== 204 ||
    (await api(page, "/session")).status !== 401
  )
    throw new Error("OWNED_OUTBOUND_SESSION_CLEANUP_FAILED");
}
async function select(page, label, option) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}
function safeForward(handler) {
  return async (route) => {
    try {
      await handler(route);
    } catch {
      await route.abort("failed").catch(() => {});
      throw new Error("OWNED_OUTBOUND_ROUTE_FORWARDING_FAILED");
    }
  };
}
module.exports = {
  test,
  expect,
  api,
  login,
  loginHTTP,
  closeSession,
  fillPrivate,
  select,
  safeForward,
};
