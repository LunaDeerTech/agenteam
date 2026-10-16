/**
 * Real browser cookie regression for a task-owned HTTPS account fixture.
 * Input: FRONTEND_ACCOUNT_RUNTIME=/absolute/private/runtime.json (0600/0700).
 * This uses a single-target loopback CONNECT relay to localhost:5254. The
 * browser URL/Host/Origin remain the manifest public IP:18443; TLS is opaque
 * to the relay. It does NOT verify public frp routing or bypass its proxy.
 * Real backend GET/PATCH 401 responses are captured with strict CA/IP TLS,
 * then delivered to tab A after tab B establishes a new real login session.
 * Only status, cookie attributes/counts, equality booleans and expiry print.
 * No trace, screenshot, request headers/body, cookie values or credentials.
 */
const fs = require("node:fs"),
  path = require("node:path"),
  https = require("node:https"),
  tls = require("node:tls");
const { X509Certificate, createHash } = require("node:crypto");
const origin = "https://154.12.86.233:18443";
function loadPublicRuntime() {
  const file = process.env.FRONTEND_ACCOUNT_RUNTIME;
  if (!file || !path.isAbsolute(file))
    throw new Error("PUBLIC_RUNTIME_PATH_REQUIRED");
  const a = fs.lstatSync(file),
    b = fs.lstatSync(path.dirname(file));
  if (
    !a.isFile() ||
    a.isSymbolicLink() ||
    (a.mode & 0o777) !== 0o600 ||
    a.uid !== process.getuid() ||
    b.uid !== process.getuid() ||
    !b.isDirectory() ||
    b.isSymbolicLink() ||
    (b.mode & 0o777) !== 0o700
  )
    throw new Error("PUBLIC_RUNTIME_PERMISSIONS");
  const r = JSON.parse(fs.readFileSync(file, "utf8"));
  if (
    !Number.isFinite(Date.parse(r.expires_at)) ||
    Date.parse(r.expires_at) <= Date.now() ||
    typeof r.credentials?.email !== "string" ||
    typeof r.credentials?.password !== "string" ||
    !r.credentials.password
  )
    throw new Error("PUBLIC_RUNTIME_EXPIRED_OR_INVALID");
  if (r.origin !== origin || !/^[a-f0-9]{32}$/.test(r.nonce || ""))
    throw new Error("PUBLIC_RUNTIME_ORIGIN");
  const leaf = fs.readFileSync(r.certificate_file),
    cert = new X509Certificate(leaf),
    ca = fs.readFileSync(r.certificate_ca_file);
  const spki = createHash("sha256")
    .update(cert.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
  if (
    !cert.checkIP("154.12.86.233") ||
    spki !== r.certificate_spki ||
    Date.parse(cert.validTo) <= Date.now() ||
    Date.parse(cert.validFrom) > Date.now()
  )
    throw new Error("PUBLIC_OWNED_CERTIFICATE");
  return { ...r, ca };
}
function localDial(runtime, pathname, options = {}) {
  if (!pathname.startsWith("/api/v1/") && pathname !== "/__fixture__/identity")
    throw new Error("PUBLIC_LOCAL_PATH");
  const agent = new https.Agent({ keepAlive: false });
  agent.createConnection = () =>
    tls.connect({
      host: "127.0.0.1",
      port: 5254,
      ca: runtime.ca,
      rejectUnauthorized: true,
      servername: "",
      checkServerIdentity: (_host, cert) =>
        tls.checkServerIdentity("154.12.86.233", cert),
    });
  return new Promise((resolve, reject) => {
    const body = options.body;
    const req = https.request(
      new URL(pathname, runtime.origin),
      {
        agent,
        method: options.method || "GET",
        headers: {
          Host: new URL(runtime.origin).host,
          ...options.headers,
          ...(body === undefined
            ? {}
            : { "Content-Length": Buffer.byteLength(body) }),
        },
      },
      (res) => {
        const chunks = [];
        let size = 0;
        res.on("data", (x) => {
          size += x.length;
          if (size > 2 * 1024 * 1024) req.destroy();
          else chunks.push(x);
        });
        res.on("end", () => {
          agent.destroy();
          resolve({
            status: res.statusCode,
            headers: res.headers,
            body: Buffer.concat(chunks),
          });
        });
      },
    );
    req.on("error", () => {
      agent.destroy();
      reject(new Error("PUBLIC_LOCAL_HTTPS_FAILED"));
    });
    req.setTimeout(20000, () => req.destroy());
    req.end(body);
  });
}
const { chromium } = require("../account-captcha-web/node_modules/playwright");
const spki = (cert) =>
  createHash("sha256")
    .update(cert.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
(async () => {
  const runtime = loadPublicRuntime();
  let ownedSPKI = runtime.certificate_spki,
    proxy,
    relay;
  const relaySockets = new Set();
  {
    const http = require("node:http"),
      net = require("node:net");
    relay = http.createServer((req, res) => {
      res.writeHead(403);
      res.end();
    });
    relay.on("connect", (req, client, head) => {
      if (req.url !== "154.12.86.233:18443") {
        client.end("HTTP/1.1 403 Forbidden\r\n\r\n");
        return;
      }
      const upstream = net.connect({ host: "127.0.0.1", port: 5254 });
      for (const socket of [client, upstream]) {
        relaySockets.add(socket);
        socket.on("close", () => relaySockets.delete(socket));
        socket.on("error", () => {
          client.destroy();
          upstream.destroy();
        });
      }
      upstream.once("connect", () => {
        client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
        if (head.length) upstream.write(head);
        client.pipe(upstream);
        upstream.pipe(client);
      });
    });
    await new Promise((resolve) => relay.listen(0, "127.0.0.1", resolve));
    proxy = { server: "http://127.0.0.1:" + relay.address().port };
  }
  const closeRelay = async () => {
    for (const socket of relaySockets) socket.destroy();
    await new Promise((resolve) => relay.close(resolve));
  };
  let browser;
  try {
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      headless: true,
      proxy,
      args: [
        "--no-sandbox",
        `--ignore-certificate-errors-spki-list=${ownedSPKI}`,
      ],
    });
  } catch {
    await closeRelay();
    throw new Error("BROWSER_START_FAILED");
  }
  const results = [];
  let stage = "browser-start";
  const stop = () => {
    void browser.close().catch(() => {});
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  const deadline = setTimeout(stop, 90000);
  deadline.unref();
  try {
    for (const unsafe of [false, true]) {
      stage = unsafe ? "patch-scenario" : "get-scenario";
      const context = await browser.newContext({
        viewport: { width: 1440, height: 960 },
      });
      context.setDefaultTimeout(20000);
      context.setDefaultNavigationTimeout(20000);
      let release;
      try {
        const older = await context.newPage(),
          newer = await context.newPage();
        const cdp = await context.newCDPSession(older);
        let observedCertificates;
        cdp.on("Security.visibleSecurityStateChanged", (e) => {
          observedCertificates =
            e.visibleSecurityState?.certificateSecurityState?.certificate;
        });
        await cdp.send("Network.enable");
        await cdp.send("Security.enable");
        stage = "owned-navigation";
        await older.goto(runtime.origin + "/__fixture__/identity", {
          waitUntil: "domcontentloaded",
        });
        stage = "nonce-check";
        const identity = JSON.parse(await older.locator("body").innerText());
        if (identity.nonce !== runtime.nonce)
          throw new Error("PUBLIC_NONCE_MISMATCH");
        stage = "browser-certificate";
        const observedChain = observedCertificates;
        const browserObservedLeafSPKIMatched = observedChain?.length
          ? spki(
              new X509Certificate(Buffer.from(observedChain[0], "base64")),
            ) === ownedSPKI
          : null;
        if (browserObservedLeafSPKIMatched !== true)
          throw new Error("BROWSER_OWNED_CERTIFICATE_MISMATCH");
        await cdp.detach();
        stage = "capture-real-401";
        const pathname = unsafe ? "/api/v1/me" : "/api/v1/session";
        const actual = await localDial(
          runtime,
          pathname,
          unsafe
            ? {
                method: "PATCH",
                headers: {
                  Origin: runtime.origin,
                  "Sec-Fetch-Site": "same-origin",
                  "Content-Type": "application/json",
                  "X-CSRF-Token": "review-expired-context",
                  "Idempotency-Key": require("node:crypto").randomUUID(),
                },
                body: JSON.stringify({
                  version: "1",
                  display_name: "Synthetic boundary review",
                }),
              }
            : { headers: { "Sec-Fetch-Site": "same-origin" } },
        );
        const oldStatus = actual.status,
          actualHeaders = actual.headers,
          actualBody = actual.body;
        if (oldStatus !== 401)
          throw new Error("PUBLIC_ORDINARY_ERROR_COOKIE_CONTRACT");
        let ready;
        const readyPromise = new Promise((r) => (ready = r)),
          releasePromise = new Promise((r) => (release = r));
        let routeFailed = false;
        await older.route("**" + pathname, async (route) => {
          try {
            if (route.request().method() !== (unsafe ? "PATCH" : "GET"))
              throw new Error("PUBLIC_ROUTE_METHOD_MISMATCH");
            ready();
            await releasePromise;
            const headers = {};
            for (const [k, v] of Object.entries(actualHeaders))
              if (
                ![
                  "connection",
                  "transfer-encoding",
                  "content-length",
                  "content-encoding",
                ].includes(k)
              )
                headers[k] = Array.isArray(v) ? v.join("\n") : String(v);
            await route.fulfill({
              status: oldStatus,
              headers,
              body: actualBody,
            });
          } catch {
            routeFailed = true;
            await route.abort().catch(() => {});
          }
        });
        const lateRead = older.evaluate(
          async (unsafe) =>
            (
              await fetch(unsafe ? "/api/v1/me" : "/api/v1/session", {
                cache: "no-store",
                credentials: "same-origin",
                ...(unsafe
                  ? {
                      method: "PATCH",
                      headers: {
                        "Content-Type": "application/json",
                        "X-CSRF-Token": "review-expired-context",
                        "Idempotency-Key": crypto.randomUUID(),
                      },
                      body: JSON.stringify({
                        version: "1",
                        display_name: "Synthetic boundary review",
                      }),
                    }
                  : {}),
              })
            ).status,
          unsafe,
        );
        void lateRead.catch(() => {});
        let readyTimeout;
        try {
          await Promise.race([
            readyPromise,
            new Promise((_, reject) => {
              readyTimeout = setTimeout(
                () => reject(new Error("PUBLIC_INTERCEPT_TIMEOUT")),
                15000,
              );
            }),
          ]);
        } finally {
          clearTimeout(readyTimeout);
        }
        stage = "new-browser-api-login";
        await newer.goto(runtime.origin + "/__fixture__/identity", {
          waitUntil: "domcontentloaded",
        });
        if (
          JSON.parse(await newer.locator("body").innerText()).nonce !==
          runtime.nonce
        )
          throw new Error("PUBLIC_NEW_TAB_NONCE_MISMATCH");
        const login = await newer.evaluate(async (credentials) => {
          const boot = await fetch("/api/v1/auth/bootstrap", {
            cache: "no-store",
            credentials: "same-origin",
            signal: AbortSignal.timeout(15000),
          });
          if (boot.status !== 200) return { bootstrap: boot.status };
          const b = await boot.json();
          const r = await fetch("/api/v1/sessions/login", {
            method: "POST",
            credentials: "same-origin",
            signal: AbortSignal.timeout(15000),
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": b.csrf_token,
              "Idempotency-Key": crypto.randomUUID(),
            },
            body: JSON.stringify(credentials),
          });
          await r.arrayBuffer();
          const s = await fetch("/api/v1/session", {
            cache: "no-store",
            credentials: "same-origin",
            signal: AbortSignal.timeout(15000),
          });
          await s.arrayBuffer();
          return { login: r.status, session: s.status };
        }, runtime.credentials);
        if (login.login !== 200 || login.session !== 200)
          throw new Error("PUBLIC_BROWSER_LOGIN_FAILED");
        stage = "release-late-response";
        const before = (await context.cookies()).filter(
          (c) => c.name === "__Host-agenteam_session",
        );
        release();
        const lateStatus = await lateRead;
        const after = (await context.cookies()).filter(
          (c) => c.name === "__Host-agenteam_session",
        );
        stage = "logout-control";
        const control = await newer.evaluate(async () => {
          const s = await fetch("/api/v1/session", {
            cache: "no-store",
            credentials: "same-origin",
          });
          if (s.status !== 200) return { sessionAfterLate: s.status };
          const data = await s.json();
          const out = await fetch("/api/v1/sessions/logout", {
            method: "POST",
            credentials: "same-origin",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": data.csrf_token,
              "Idempotency-Key": crypto.randomUUID(),
            },
            body: "{}",
          });
          await out.arrayBuffer();
          const post = await fetch("/api/v1/session", {
            cache: "no-store",
            credentials: "same-origin",
          });
          await post.arrayBuffer();
          return {
            sessionAfterLate: s.status,
            logout: out.status,
            sessionAfterLogout: post.status,
          };
        });
        const finalCookies = (await context.cookies()).filter(
          (c) => c.name === "__Host-agenteam_session",
        );
        const result = {
          kind: "local-fixed-route-browser-public-origin-real-response-controlled-delivery",
          method: unsafe ? "PATCH" : "GET",
          nonceMatched: true,
          browserObservedLeafSPKIMatched,
          ownedServerLeafVisible: browserObservedLeafSPKIMatched === true,
          routeFailed,
          ordinaryError: oldStatus,
          ordinaryErrorSetCookie: Boolean(actualHeaders["set-cookie"]),
          lateStatus,
          sessionCookieBefore: before.length,
          sessionCookieAfter: after.length,
          cookieUnchanged:
            before.length === 1 &&
            after.length === 1 &&
            before[0].value === after[0].value,
          secureHttpOnlyHostOnly:
            before.length === 1 &&
            before[0].secure &&
            before[0].httpOnly &&
            before[0].domain === "154.12.86.233" &&
            before[0].path === "/",
          ...control,
          sessionCookieAfterLogout: finalCookies.length,
        };
        result.pass =
          !result.routeFailed &&
          !result.ordinaryErrorSetCookie &&
          result.secureHttpOnlyHostOnly &&
          lateStatus === 401 &&
          result.cookieUnchanged &&
          control.sessionAfterLate === 200 &&
          control.logout === 204 &&
          control.sessionAfterLogout === 401 &&
          finalCookies.length === 0;
        results.push(result);
        console.log(JSON.stringify(result));
        if (!result.pass) process.exitCode = 1;
      } catch (e) {
        console.error("SCENARIO_FAILED_STAGE " + stage);
        throw e;
      } finally {
        release?.();
        await context.close();
      }
    }
    console.log(
      JSON.stringify({
        kind: "probe-complete",
        origin: runtime.origin,
        runtimeExpires: runtime.expires_at,
        scenarios: results.length,
        pass: results.length === 2 && results.every((r) => r.pass),
        scope:
          "A transient loopback CONNECT relay permits only public IP:18443 and fixed-dials localhost:5254 without terminating TLS. This is not public frp connectivity verification.",
      }),
    );
  } catch (e) {
    console.error(
      "SESSION_COOKIE_RACE_PROBE_FAILED " +
        stage +
        " " +
        (e.message?.startsWith("PUBLIC_") || e.message?.startsWith("BROWSER_")
          ? e.message
          : e.name),
    );
    process.exitCode = 1;
  } finally {
    clearTimeout(deadline);
    process.removeListener("SIGINT", stop);
    process.removeListener("SIGTERM", stop);
    await browser.close();
    await closeRelay();
  }
})().catch(() => {
  console.error("SESSION_COOKIE_RACE_PROBE_INITIALIZATION_FAILED");
  process.exitCode = 1;
});
