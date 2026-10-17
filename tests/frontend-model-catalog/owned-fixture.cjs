const fs = require("node:fs");
const path = require("node:path");
const { X509Certificate, createHash } = require("node:crypto");
const { test: base, expect, chromium } = require("@playwright/test");
const {
  observeOwnedCertificate,
} = require("../frontend-account-rebuild/fixed-runtime.cjs");

const origin = "https://localhost:5283";
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
    const filename = process.env.MODEL_CATALOG_RUNTIME;
    const runtime = privateJSON(filename);
    const sidecar = privateJSON(
      path.join(path.dirname(filename), "model-owned.json"),
    );
    const credentials = (value) =>
      typeof value?.email === "string" &&
      !!value.email &&
      typeof value?.password === "string" &&
      !!value.password;
    if (
      runtime.origin !== origin ||
      runtime.test_scope !== "system-model-catalog" ||
      !/^[a-f0-9]{32}$/.test(runtime.nonce || "") ||
      !Number.isFinite(Date.parse(runtime.expires_at)) ||
      Date.parse(runtime.expires_at) <= Date.now() ||
      !credentials(runtime.credentials) ||
      !credentials(runtime.regular_credentials) ||
      Object.keys(sidecar).sort().join() !== "nonce,runtime_file,test_scope" ||
      sidecar.test_scope !== "system-model-catalog" ||
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
    throw new Error("OWNED_MODEL_CATALOG_RUNTIME_OR_TLS_SCOPE_INVALID");
  }
}

async function strictIdentity(runtime) {
  const https = require("node:https");
  await new Promise((resolve, reject) => {
    const request = https.get(
      `${runtime.origin}/__fixture__/identity`,
      {
        ca: fs.readFileSync(runtime.certificate_ca_file),
        lookup: (_host, options, callback) =>
          options.all
            ? callback(null, [{ address: "127.0.0.1", family: 4 }])
            : callback(null, "127.0.0.1", 4),
        rejectUnauthorized: true,
        timeout: 5000,
      },
      (response) => {
        const peerCertificate = response.socket.getPeerCertificate(true).raw;
        let body = "";
        response.on("data", (chunk) => {
          body += chunk;
          if (body.length > 65536) request.destroy();
        });
        response.on("end", () => {
          try {
            const leaf = new X509Certificate(peerCertificate);
            const expected = new X509Certificate(
              fs.readFileSync(runtime.certificate_file),
            );
            if (
              response.statusCode !== 200 ||
              JSON.parse(body).nonce !== runtime.nonce ||
              leaf.fingerprint256 !== expected.fingerprint256
            )
              throw new Error();
            resolve();
          } catch {
            reject(new Error("OWNED_MODEL_STRICT_IDENTITY_FAILED"));
          }
        });
      },
    );
    request.on("timeout", () => request.destroy());
    request.on("error", () =>
      reject(new Error("OWNED_MODEL_STRICT_TLS_FAILED")),
    );
  });
}
const test = base.extend({
  owned: [
    async ({}, use) => {
      const runtime = loadRuntime();
      await strictIdentity(runtime);
      await use(runtime);
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
      throw new Error("OWNED_MODEL_CATALOG_INSTANCE_IDENTITY_REJECTED");
    } finally {
      await certificate.close();
    }
    const statuses = [];
    const started = Date.now();
    page.on("response", (response) => {
      const pathname = new URL(response.url()).pathname;
      if (
        /^\/api\/v1\/(?:auth\/bootstrap|session|sessions\/(?:login|logout)|system\/model(?:-providers|-credentials|-selection|s)(?:\/[^?]*)?)$/.test(
          pathname,
        )
      )
        statuses.push({
          path: pathname,
          method: response.request().method(),
          status: response.status(),
          elapsedMs: Date.now() - started,
        });
    });
    try {
      await use(page);
    } finally {
      statuses.push({
        event: "teardown-start",
        elapsedMs: Date.now() - started,
      });
      await page.unrouteAll({ behavior: "wait" });
      try {
        // Finish teardown on the owned inert document so application navigation
        // cannot destroy the execution context while revoking this session.
        const response = await page.goto(`${origin}/__fixture__/identity`);
        if (
          response?.status() !== 200 ||
          (await response.json()).nonce !== owned.nonce
        )
          throw new Error("OWNED_MODEL_CATALOG_CLEANUP_IDENTITY_REJECTED");
        await closeSession(page);
      } finally {
        if (testInfo.status !== testInfo.expectedStatus)
          await testInfo.attach("safe-model-status", {
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
// DTOs and secret request material stay in memory. Tests only assert booleans
// for private comparisons; this helper never attaches response bodies.
async function api(page, pathname, method = "GET", body, options = {}) {
  try {
    return await page.evaluate(
      async ({ pathname, method, body, options }) => {
        const headers = {};
        if (method !== "GET") {
          headers["Content-Type"] = "application/json";
          headers["Idempotency-Key"] = options.key || crypto.randomUUID();
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
    throw new Error("OWNED_MODEL_CATALOG_API_OPERATION_FAILED");
  }
}
async function loginHTTP(page, credentials) {
  try {
    const result = await page.evaluate(async (credentials) => {
      let phase = "bootstrap_fetch";
      let status;
      try {
        const bootstrap = await fetch("/api/v1/auth/bootstrap");
        status = bootstrap.status;
        if (status !== 200) return { phase: "bootstrap_status", status };
        phase = "bootstrap_body";
        const csrf = (await bootstrap.json()).csrf_token;
        phase = "login_fetch";
        status = undefined;
        const response = await fetch("/api/v1/sessions/login", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": csrf,
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: JSON.stringify(credentials),
        });
        status = response.status;
        phase = "login_body";
        await response.arrayBuffer();
        return { phase: "complete", status };
      } catch {
        return { phase, status };
      }
    }, credentials);
    if (result.phase !== "complete") {
      await base.info().attach("safe-login-phase", {
        body: Buffer.from(JSON.stringify(result)),
        contentType: "application/json",
      });
      throw new Error();
    }
    return result.status;
  } catch {
    throw new Error("OWNED_MODEL_CATALOG_HTTP_LOGIN_FAILED");
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
    throw new Error("OWNED_MODEL_CATALOG_SESSION_CLEANUP_FAILED");
}
async function providerStats(page, owned) {
  try {
    return await page.evaluate(async (nonce) => {
      const response = await fetch("/__fixture__/provider", {
        headers: { "X-Fixture-Nonce": nonce },
        cache: "no-store",
      });
      if (response.status !== 200) throw new Error();
      return response.json();
    }, owned.nonce);
  } catch {
    throw new Error("OWNED_MODEL_PROVIDER_OBSERVATION_FAILED");
  }
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
      throw new Error("OWNED_MODEL_CATALOG_ROUTE_FORWARDING_FAILED");
    }
  };
}
module.exports = {
  loadRuntime,
  strictIdentity,
  test,
  expect,
  api,
  loginHTTP,
  closeSession,
  providerStats,
  fillPrivate,
  select,
  safeForward,
};
