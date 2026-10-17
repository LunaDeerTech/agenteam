"use strict";
const fs = require("node:fs"),
  path = require("node:path"),
  https = require("node:https"),
  tls = require("node:tls"),
  http = require("node:http"),
  net = require("node:net");
const { X509Certificate, createHash } = require("node:crypto");
const {
  chromium,
  expect,
} = require("../tests/account-captcha-web/node_modules/@playwright/test");
const {
  observeOwnedCertificate,
} = require("../tests/frontend-account-rebuild/fixed-runtime.cjs");
// Authorized empty-catalog preview: one login, read-only UI, then logout.
// Checks bundled multimodal controls; model dialogs need saved models and are
// explicitly reported as unexecuted here. Unexpected nonempty catalogs fail.
// FINAL_RUNTIME, FINAL_BACKEND_REVISION, FINAL_FRONTEND_DIRECTORY and
// FINAL_RESULT_FILE are required. Credentials remain in the private runtime.
const base = path.dirname(process.env.FINAL_RUNTIME || "/invalid/input");
const output = process.env.FINAL_RESULT_FILE;
const frontend = process.env.FINAL_FRONTEND_DIRECTORY;
const origin = "https://154.12.86.233:18443";
const result = {
  phase: "input",
  route: "local_tls_5256_only_not_public_frp",
  public_identity_newly_verified: false,
  read_only_module: true,
  statuses: [],
  blocked_mutations: 0,
};
const requireFact = (value, code) => {
  if (!value) throw new Error(code);
};
const privateJSON = (file) => {
  const st = fs.lstatSync(file);
  requireFact(
    fs.realpathSync(file) === file &&
      st.isFile() &&
      st.uid === process.getuid() &&
      (st.mode & 0o777) === 0o600,
    "PRIVATE_INPUT_REJECTED",
  );
  return JSON.parse(fs.readFileSync(file, "utf8"));
};
const pin = (cert) =>
  createHash("sha256")
    .update(cert.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
const safePaths = new Set([
  "/api/v1/auth/bootstrap",
  "/api/v1/session",
  "/api/v1/sessions/login",
  "/api/v1/sessions/logout",
  "/api/v1/system/outbound-policy",
  "/api/v1/system/outbound-policy/proxy",
  "/api/v1/system/model-providers",
  "/api/v1/system/model-selection",
  "/api/v1/system/models",
]);
let runtime, credentials, relay, browser, server, context, page, deadline;
let outputValidated = false;
const reads = new Map();
const sockets = new Set();
async function strictIdentity(ca, leaf) {
  return new Promise((resolve, reject) => {
    const req = https.request(
      {
        hostname: "127.0.0.1",
        port: 5256,
        path: "/__instance__/identity",
        method: "GET",
        ca,
        checkServerIdentity: (_host, cert) =>
          tls.checkServerIdentity("154.12.86.233", cert),
        headers: { Host: "154.12.86.233:18443" },
        timeout: 8000,
      },
      (res) => {
        let body = "";
        let raw;
        try {
          raw = res.socket.getPeerCertificate().raw;
          requireFact(
            res.socket.authorized && raw && Buffer.compare(raw, leaf.raw) === 0,
            "STRICT_CERT_MISMATCH",
          );
        } catch {
          res.destroy();
          reject(new Error("STRICT_CERT_MISMATCH"));
          return;
        }
        res.setEncoding("utf8");
        res.on("data", (chunk) => {
          body += chunk;
          if (body.length > 8192) res.destroy();
        });
        res.on("error", () => reject(new Error("STRICT_IDENTITY_BODY_FAILED")));
        res.on("end", () => {
          try {
            const data = JSON.parse(body);
            requireFact(
              res.statusCode === 200 &&
                data.nonce === runtime.nonce &&
                data.source_revision ===
                  "62ad504378fcab4e65722cbb7e5d2fffa8bc60a1",
              "STRICT_IDENTITY_MISMATCH",
            );
            resolve();
          } catch {
            reject(new Error("STRICT_IDENTITY_MISMATCH"));
          }
        });
      },
    );
    req.on("timeout", () => req.destroy());
    req.on("error", () => reject(new Error("STRICT_TLS_FAILED")));
    req.end();
  });
}
(async () => {
  try {
    requireFact(
      typeof output === "string" &&
        path.isAbsolute(output) &&
        !fs.existsSync(output),
      "PRIVATE_OUTPUT_REQUIRED",
    );
    const outputDir = fs.lstatSync(path.dirname(output));
    requireFact(
      outputDir.isDirectory() &&
        (outputDir.mode & 0o777) === 0o700 &&
        outputDir.uid === process.getuid() &&
        fs.realpathSync(path.dirname(output)) === path.dirname(output),
      "PRIVATE_OUTPUT_DIRECTORY_REQUIRED",
    );
    outputValidated = true;
    requireFact(
      typeof frontend === "string" && path.dirname(frontend) === base,
      "FRONTEND_INPUT_REQUIRED",
    );
    requireFact(
      typeof process.env.FINAL_RUNTIME === "string" &&
        path.dirname(process.env.FINAL_RUNTIME) === base &&
        /^[0-9a-f]{40}$/.test(process.env.FINAL_BACKEND_REVISION || ""),
      "FINAL_INPUT_REQUIRED",
    );
    runtime = privateJSON(process.env.FINAL_RUNTIME);
    requireFact(
      runtime.source_revision === process.env.FINAL_BACKEND_REVISION,
      "FINAL_REVISION_REJECTED",
    );
    result.backend_revision = runtime.source_revision;
    const frontendRecord = privateJSON(
      path.join(frontend, "preview-record.json"),
    );
    requireFact(
      frontendRecord.source_revision === runtime.source_revision &&
        frontendRecord.production_build === true,
      "FRONTEND_REVISION_REJECTED",
    );
    const assetsDir = path.join(frontend, "dist/assets");
    const bundle = fs
      .readdirSync(assetsDir)
      .filter((name) => name.endsWith(".js"))
      .map((name) => fs.readFileSync(path.join(assetsDir, name), "utf8"))
      .join("\n");
    result.old_hint_absent =
      !bundle.includes("图像和文件仅记录能力") &&
      !bundle.includes("当前调用仅支持文本");
    result.current_hint_present = bundle.includes(
      "图像支持 PNG、JPEG、WebP、静态 GIF；文件支持 PDF，或提取 TXT、Markdown、DOCX 的文字。",
    );
    result.test_attachment_controls_in_bundle =
      bundle.includes("选择附件") &&
      bundle.includes("测试文本") &&
      bundle.includes("最多 4 个附件，共 8 MiB");
    requireFact(
      result.old_hint_absent &&
        result.current_hint_present &&
        result.test_attachment_controls_in_bundle,
      "DEPLOYED_MULTIMODAL_COPY_MISMATCH",
    );
    requireFact(
      runtime.origin === origin &&
        runtime.local_tls_address === "127.0.0.1:5256",
      "TARGET_REJECTED",
    );
    const caBytes = fs.readFileSync(runtime.certificate_ca_file),
      ca = new X509Certificate(caBytes),
      leaf = new X509Certificate(fs.readFileSync(runtime.certificate_file));
    requireFact(
      ca.ca &&
        leaf.checkIP("154.12.86.233") &&
        leaf.checkIssued(ca) &&
        leaf.verify(ca.publicKey) &&
        pin(leaf) === runtime.certificate_spki &&
        [ca, leaf].every(
          (c) =>
            Date.parse(c.validFrom) <= Date.now() &&
            Date.parse(c.validTo) > Date.now(),
        ),
      "CERT_REJECTED",
    );
    await strictIdentity(caBytes, leaf);
    result.strict_tls_nonce_infra_identity = true;
    requireFact(
      path.dirname(runtime.credentials_file) === base,
      "CREDENTIAL_PATH_REJECTED",
    );
    credentials = privateJSON(runtime.credentials_file);
    requireFact(
      typeof credentials.email === "string" &&
        typeof credentials.password === "string",
      "CREDENTIAL_INPUT_REJECTED",
    );
    result.playwright_version =
      require("../tests/account-captcha-web/node_modules/@playwright/test/package.json").version;
    requireFact(
      result.playwright_version === "1.56.1",
      "PLAYWRIGHT_VERSION_REJECTED",
    );
    relay = http.createServer((_q, res) => {
      res.writeHead(403);
      res.end();
    });
    const track = (s) => {
      sockets.add(s);
      s.on("close", () => sockets.delete(s));
      s.on("error", () => s.destroy());
    };
    relay.on("connection", (s) => {
      track(s);
      s.setTimeout(30000, () => s.destroy());
    });
    relay.on("connect", (req, client, head) => {
      if (req.url !== "154.12.86.233:18443") {
        client.end("HTTP/1.1 403 Forbidden\r\n\r\n");
        return;
      }
      const upstream = net.connect({ host: "127.0.0.1", port: 5256 });
      track(upstream);
      upstream.on("close", () => client.destroy());
      client.on("close", () => upstream.destroy());
      upstream.once("connect", () => {
        client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        if (head.length) upstream.write(head);
        client.pipe(upstream);
        upstream.pipe(client);
      });
    });
    await new Promise((resolve, reject) => {
      relay.once("error", () => reject(new Error("RELAY_START_FAILED")));
      relay.listen(0, "127.0.0.1", resolve);
    });
    result.relay_port = relay.address().port;
    deadline = setTimeout(() => {
      result.deadline_expired = true;
      if (page) page.close().catch(() => {});
      else if (server) server.close().catch(() => {});
    }, 90000);
    result.phase = "browser_launch";
    server = await chromium.launchServer({
      executablePath: "/usr/bin/chromium",
      headless: true,
      proxy: { server: `http://127.0.0.1:${result.relay_port}` },
      args: [
        "--no-sandbox",
        `--ignore-certificate-errors-spki-list=${runtime.certificate_spki}`,
      ],
    });
    const child = server.process();
    result.browser_pid = child.pid;
    child.once("exit", (code, signal) => {
      result.browser_exit = { code, signal };
    });
    browser = await chromium.connect(server.wsEndpoint());
    result.chromium_version = browser.version();
    context = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
      locale: "zh-CN",
    });
    page = await context.newPage();
    page.setDefaultTimeout(15000);
    await page.route("**/*", async (route) => {
      const req = route.request(),
        url = new URL(req.url());
      if (
        url.origin === origin &&
        url.pathname.startsWith("/api/") &&
        !["GET", "HEAD", "OPTIONS"].includes(req.method()) &&
        !(
          req.method() === "POST" &&
          ["/api/v1/sessions/login", "/api/v1/sessions/logout"].includes(
            url.pathname,
          )
        )
      ) {
        result.blocked_mutations++;
        return route.abort("blockedbyclient");
      }
      return route.continue();
    });
    page.on("response", (res) => {
      const pathname = new URL(res.url()).pathname;
      if (
        res.request().method() === "GET" &&
        res.status() === 200 &&
        [
          "/api/v1/system/outbound-policy",
          "/api/v1/system/outbound-policy/proxy",
        ].includes(pathname)
      )
        reads.set(pathname, res);
      if (safePaths.has(pathname))
        result.statuses.push({
          path: pathname,
          method: res.request().method(),
          status: res.status(),
        });
    });
    result.phase = "browser_identity";
    const cert = await observeOwnedCertificate(page, runtime);
    const identity = await page.goto(origin + "/__instance__/identity");
    cert.verify();
    requireFact(
      identity.status() === 200 &&
        (await identity.json()).nonce === runtime.nonce,
      "BROWSER_NONCE_MISMATCH",
    );
    await cert.close();
    result.browser_certificate_nonce = true;
    result.phase = "login_page";
    await page.goto(origin + "/system/models");
    await expect(
      page.getByRole("heading", { name: "登录", exact: true }),
    ).toBeVisible();
    result.login_heading_visible = true;
    try {
      await page.getByLabel("邮箱", { exact: true }).fill(credentials.email);
      await page.getByLabel("密码", { exact: true }).fill(credentials.password);
    } catch {
      throw new Error("PRIVATE_FIELD_INTERACTION_FAILED");
    }
    result.phase = "login_once";
    const loginResponse = page.waitForResponse(
      (res) =>
        new URL(res.url()).pathname === "/api/v1/sessions/login" &&
        res.request().method() === "POST",
      { timeout: 20000 },
    );
    await page.getByRole("button", { name: "登录", exact: true }).click();
    const login = await loginResponse;
    result.login_status = login.status();
    requireFact(login.status() === 200, "SINGLE_LOGIN_REJECTED");
    credentials = undefined;
    result.phase = "catalog_page";
    await expect(page).toHaveURL(origin + "/system/models");
    await expect(
      page.getByRole("heading", { name: "供应商列表", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("尚未添加供应商。", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "添加供应商", exact: true }),
    ).toBeEnabled();
    result.catalog_empty_page = true;
    result.model_editor_dialog = {
      executed: false,
      reason: "no_saved_providers_or_models",
    };
    result.model_test_dialog = { executed: false, reason: "no_saved_models" };
    result.phase = "new_provider_dialog";
    await page.getByRole("button", { name: "添加供应商", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    result.provider_dialog_open_cancel = true;
    result.phase = "model_selection";
    await page.getByRole("link", { name: "平台模型用途", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "平台模型用途", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "选择总结模型（Chat）", exact: true }),
    ).toBeEnabled();
    result.model_selection_loaded = true;
    const summaryPicker = page.getByRole("button", {
      name: "选择总结模型（Chat）",
      exact: true,
    });
    await summaryPicker.click();
    await expect(page.getByRole("combobox")).toBeVisible();
    await page.keyboard.press("Escape");
    result.model_picker_open_cancel = true;
    result.phase = "read_only_api";
    result.api_reads = await page.evaluate(async () => {
      const out = [];
      for (const path of [
        "/api/v1/system/outbound-policy",
        "/api/v1/system/outbound-policy/proxy",
        "/api/v1/system/model-providers",
        "/api/v1/system/model-selection",
        "/api/v1/system/models",
      ]) {
        const r = await fetch(path, { cache: "no-store" });
        await r.arrayBuffer();
        out.push({ path, status: r.status });
      }
      return out;
    });
    requireFact(
      result.api_reads.every((r) => r.status === 200),
      "API_READ_FAILED",
    );
    result.phase = "release_match";
    const releaseResponse = await page.goto(
      origin + "/__instance__/release.json",
    );
    const release = await releaseResponse.json();
    requireFact(
      releaseResponse.status() === 200 &&
        release.source_revision === runtime.source_revision &&
        release.nonce === runtime.nonce &&
        release.backend_sha256 === runtime.central_binary_sha256 &&
        release.frontend_index_sha256 ===
          createHash("sha256")
            .update(fs.readFileSync(path.join(frontend, "dist/index.html")))
            .digest("hex"),
      "RELEASE_MISMATCH",
    );
    result.same_release_and_nonce = true;
    requireFact(result.blocked_mutations === 0, "UNEXPECTED_MUTATION");
    result.pass = true;
    result.phase = "complete";
  } catch (error) {
    if (page) {
      try {
        result.failure_diagnostics = await page.evaluate(() => ({
          mode_controls: document.querySelectorAll("[role=combobox]").length,
          proxy_reading:
            document.body.textContent.includes("正在读取代理设置…"),
          load_failure:
            document.body.textContent.includes("当前配置读取失败。"),
          permission_failure:
            document.body.textContent.includes("你目前没有系统管理权限。"),
        }));
      } catch {
        result.failure_diagnostics = { snapshot_unavailable: true };
      }
    }
    result.pass = false;
    result.failure_stage = result.phase;
    result.failure =
      error.message && /^[A-Z0-9_]+$/.test(error.message)
        ? error.message
        : "BROWSER_STAGE_ASSERTION_FAILED";
    process.exitCode = 1;
  } finally {
    credentials = undefined;
    if (deadline) clearTimeout(deadline);
    if (page) {
      try {
        const cleanupIdentity = await page.goto(
          origin + "/__instance__/identity",
        );
        requireFact(
          cleanupIdentity.status() === 200 &&
            (await cleanupIdentity.json()).nonce === runtime.nonce,
          "CLEANUP_IDENTITY_REJECTED",
        );
        result.session_cleanup = await page.evaluate(async () => {
          const session = await fetch("/api/v1/session", { cache: "no-store" });
          if (session.status === 401)
            return { session_status: 401, logout_required: false };
          if (session.status !== 200)
            return { session_status: session.status, pass: false };
          const token = (await session.json()).csrf_token;
          const logout = await fetch("/api/v1/sessions/logout", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": token,
              "Idempotency-Key": crypto.randomUUID(),
            },
            body: "{}",
          });
          return {
            logout_status: logout.status,
            session_status: (
              await fetch("/api/v1/session", { cache: "no-store" })
            ).status,
          };
        });
        requireFact(
          result.session_cleanup.session_status === 401 &&
            (result.session_cleanup.logout_required === false ||
              result.session_cleanup.logout_status === 204),
          "CLEANUP_REJECTED",
        );
      } catch {
        result.session_cleanup = { error: "CLEANUP_REQUEST_FAILED" };
        result.pass = false;
        process.exitCode = 1;
      }
    }
    try {
      if (context) await context.close();
      if (browser) await browser.close();
      if (server) await server.close();
      result.browser_closed = true;
    } catch {
      result.browser_closed = false;
      result.pass = false;
      process.exitCode = 1;
    }
    if (relay) {
      for (const socket of sockets) socket.destroy();
      await new Promise((resolve) => relay.close(resolve));
      result.relay_closed = true;
      result.relay_sockets_remaining = sockets.size;
    }
    result.completed_at = new Date().toISOString();
    if (outputValidated)
      fs.writeFileSync(output, JSON.stringify(result, null, 2) + "\n", {
        mode: 0o600,
        flag: "wx",
      });
    console.log(JSON.stringify(result));
  }
})();
