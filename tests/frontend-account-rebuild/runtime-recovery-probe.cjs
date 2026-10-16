// Independent, read-only account checks around one operator-owned Central restart.
// Credentials and cookies stay in memory. This fixed local TLS route is not frp.
const fs = require("node:fs");
const path = require("node:path");
const https = require("node:https");
const tls = require("node:tls");
const { X509Certificate, createHash, randomUUID } = require("node:crypto");

const origin = "https://154.12.86.233:18443";
const address = "127.0.0.1:5256";
const allowed = new Set([
  "GET /__instance__/identity",
  "GET /api/v1/auth/bootstrap",
  "GET /api/v1/session",
  "GET /api/v1/system/smtp",
  "POST /api/v1/sessions/login",
  "POST /api/v1/sessions/logout",
]);
class Failure extends Error {}
const requireFact = (condition, code) => {
  if (!condition) throw new Failure(code);
};
const emit = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const spki = (certificate) =>
  createHash("sha256")
    .update(certificate.publicKey.export({ type: "spki", format: "der" }))
    .digest("base64");
const validNow = (certificate) =>
  Date.parse(certificate.validFrom) <= Date.now() &&
  Date.parse(certificate.validTo) > Date.now();

function privateJSON(filename, base) {
  requireFact(
    typeof filename === "string" && path.isAbsolute(filename),
    "PRIVATE_PATH_REQUIRED",
  );
  const real = fs.realpathSync(filename);
  requireFact(
    real === filename && (!base || path.dirname(real) === base),
    "PRIVATE_PATH_MISMATCH",
  );
  const file = fs.lstatSync(real),
    parent = fs.lstatSync(path.dirname(real));
  requireFact(
    file.isFile() &&
      !file.isSymbolicLink() &&
      file.uid === process.getuid() &&
      (file.mode & 0o777) === 0o600 &&
      file.size <= 32768,
    "PRIVATE_FILE_REJECTED",
  );
  requireFact(
    parent.isDirectory() &&
      !parent.isSymbolicLink() &&
      parent.uid === process.getuid() &&
      (parent.mode & 0o777) === 0o700,
    "PRIVATE_DIRECTORY_REJECTED",
  );
  return JSON.parse(fs.readFileSync(real, "utf8"));
}
function certificateFile(filename, base) {
  requireFact(
    typeof filename === "string" &&
      path.isAbsolute(filename) &&
      fs.realpathSync(filename) === filename &&
      filename.startsWith(base + path.sep),
    "CERTIFICATE_PATH_REJECTED",
  );
  const file = fs.lstatSync(filename);
  requireFact(
    file.isFile() &&
      file.uid === process.getuid() &&
      !(file.mode & 0o022) &&
      file.size < 65536,
    "CERTIFICATE_FILE_REJECTED",
  );
  return fs.readFileSync(filename);
}
function runtimeInput() {
  const filename = process.env.FRONTEND_ACCOUNT_RUNTIME;
  const runtime = privateJSON(filename);
  const base = path.dirname(filename);
  requireFact(
    runtime.origin === origin &&
      runtime.local_tls_address === address &&
      runtime.central_address === "127.0.0.1:5257",
    "RUNTIME_TARGET_REJECTED",
  );
  requireFact(
    /^[a-f0-9]{32}$/.test(runtime.nonce || "") &&
      /^[a-f0-9]{40}$/.test(runtime.source_revision || ""),
    "INSTANCE_IDENTITY_INVALID",
  );
  if (runtime.expires_at !== undefined)
    requireFact(Date.parse(runtime.expires_at) > Date.now(), "RUNTIME_EXPIRED");
  const caBytes = certificateFile(runtime.certificate_ca_file, base);
  const ca = new X509Certificate(caBytes);
  const leaf = new X509Certificate(
    certificateFile(runtime.certificate_file, base),
  );
  requireFact(
    ca.ca &&
      validNow(ca) &&
      validNow(leaf) &&
      leaf.checkIP("154.12.86.233") &&
      leaf.checkIssued(ca) &&
      leaf.verify(ca.publicKey) &&
      spki(leaf) === runtime.certificate_spki,
    "CERTIFICATE_IDENTITY_REJECTED",
  );
  const credentials = privateJSON(runtime.credentials_file, base);
  requireFact(
    typeof credentials.email === "string" &&
      credentials.email.length > 0 &&
      credentials.email.length <= 254 &&
      typeof credentials.password === "string" &&
      credentials.password.length > 0 &&
      credentials.password.length <= 4096,
    "CREDENTIALS_INVALID",
  );
  return { runtime, base, caBytes, credentials };
}
function processIdentity(input) {
  const record = privateJSON(
    path.join(input.base, "central-process.json"),
    input.base,
  );
  requireFact(
    Number.isSafeInteger(record.pid) &&
      record.pid > 1 &&
      Number.isSafeInteger(record.start_ticks) &&
      record.start_ticks > 0 &&
      record.source_revision === input.runtime.source_revision &&
      record.binary_sha256 === input.runtime.central_binary_sha256,
    "CENTRAL_RECORD_REJECTED",
  );
  const fields = fs
    .readFileSync(`/proc/${record.pid}/stat`, "utf8")
    .split(") ")
    .at(-1)
    .trim()
    .split(/\s+/);
  requireFact(
    !["Z", "X"].includes(fields[0]) &&
      Number(fields[19]) === record.start_ticks &&
      fs.realpathSync(`/proc/${record.pid}/exe`) ===
        fs.realpathSync(input.runtime.central_binary),
    "CENTRAL_PROCESS_IDENTITY_REJECTED",
  );
  return { pid: record.pid, ticks: record.start_ticks };
}

class Client {
  constructor(input) {
    this.input = input;
    this.cookies = new Map();
    this.csrf = undefined;
  }
  acceptCookies(values = []) {
    for (const value of values) {
      const [pair, ...attributes] = value.split(";").map((part) => part.trim());
      const separator = pair.indexOf("=");
      const name = pair.slice(0, separator),
        content = pair.slice(separator + 1);
      const flags = attributes.map((part) => part.toLowerCase());
      requireFact(
        ["__Host-agenteam_session", "__Host-agenteam_browser"].includes(name) &&
          flags.includes("secure") &&
          flags.includes("httponly") &&
          flags.includes("path=/") &&
          flags.includes("samesite=lax") &&
          !flags.some((part) => part.startsWith("domain=")),
        "COOKIE_ATTRIBUTES_REJECTED",
      );
      if (!content || flags.includes("max-age=0")) this.cookies.delete(name);
      else this.cookies.set(name, content);
    }
  }
  async request(method, pathname, body, csrf) {
    requireFact(allowed.has(`${method} ${pathname}`), "REQUEST_SCOPE_REJECTED");
    const agent = new https.Agent({ keepAlive: false });
    agent.createConnection = () =>
      tls.connect({
        host: "127.0.0.1",
        port: 5256,
        ca: this.input.caBytes,
        rejectUnauthorized: true,
        servername: "",
        checkServerIdentity: (_host, certificate) => {
          const hostnameError = tls.checkServerIdentity(
            "154.12.86.233",
            certificate,
          );
          if (hostnameError) return new Error("TLS_HOST_REJECTED");
          try {
            if (
              spki(new X509Certificate(certificate.raw)) !==
              this.input.runtime.certificate_spki
            )
              return new Error("TLS_PIN_REJECTED");
          } catch {
            return new Error("TLS_PIN_REJECTED");
          }
        },
      });
    const encoded = body === undefined ? undefined : JSON.stringify(body);
    const headers = {
      Host: new URL(origin).host,
      Accept: "application/json",
      Origin: origin,
      "Sec-Fetch-Site": "same-origin",
    };
    if (this.cookies.size)
      headers.Cookie = [...this.cookies]
        .map(([name, value]) => `${name}=${value}`)
        .join("; ");
    if (encoded !== undefined) {
      headers["Content-Type"] = "application/json";
      headers["Content-Length"] = Buffer.byteLength(encoded);
      headers["Idempotency-Key"] = randomUUID();
      headers["X-CSRF-Token"] = csrf || this.csrf;
    }
    try {
      const result = await new Promise((resolve, reject) => {
        const request = https.request(
          new URL(pathname, origin),
          { agent, method, headers },
          (response) => {
            const chunks = [];
            let size = 0;
            response.on("error", () =>
              reject(new Failure("HTTPS_RESPONSE_FAILED")),
            );
            response.on("aborted", () =>
              reject(new Failure("HTTPS_RESPONSE_INCOMPLETE")),
            );
            response.on("data", (chunk) => {
              size += chunk.length;
              if (size > 1024 * 1024) request.destroy();
              else chunks.push(chunk);
            });
            response.on("end", () =>
              resolve({
                status: response.statusCode,
                headers: response.headers,
                raw: Buffer.concat(chunks),
              }),
            );
          },
        );
        const deadline = setTimeout(() => request.destroy(), 20000);
        request.once("close", () => clearTimeout(deadline));
        request.on("error", () => reject(new Failure("HTTPS_REQUEST_FAILED")));
        request.end(encoded);
      });
      this.acceptCookies(result.headers["set-cookie"]);
      if (result.status === 204) {
        requireFact(result.raw.length === 0, "EMPTY_RECEIPT_REQUIRED");
        return { status: result.status };
      }
      const media = String(result.headers["content-type"] || "").split(
        ";",
        1,
      )[0];
      requireFact(
        ["application/json", "application/problem+json"].includes(media),
        "JSON_MEDIA_REQUIRED",
      );
      return {
        status: result.status,
        value: JSON.parse(result.raw.toString("utf8")),
      };
    } finally {
      agent.destroy();
    }
  }
}
function sessionValue(response) {
  const value = response.value;
  requireFact(
    response.status === 200 &&
      value &&
      typeof value.csrf_token === "string" &&
      value.csrf_token.length > 0 &&
      value.user?.role === "admin" &&
      typeof value.user.id === "string" &&
      typeof value.session?.id === "string",
    "CURRENT_ADMIN_SESSION_REQUIRED",
  );
  return value;
}
function smtpValue(response) {
  const value = response.value;
  const keys = [
    "id",
    "version",
    "configured",
    "host",
    "port",
    "encryption",
    "username",
    "sender_email",
    "sender_name",
    "credential_present",
    "auto_retry_count",
    "retry_interval_seconds",
  ];
  requireFact(
    response.status === 200 &&
      value &&
      typeof value === "object" &&
      keys.length === Object.keys(value).length &&
      keys.every((key) => Object.hasOwn(value, key)) &&
      typeof value.configured === "boolean" &&
      typeof value.credential_present === "boolean",
    "SMTP_SAFE_PROJECTION_REQUIRED",
  );
  return JSON.stringify(keys.map((key) => [key, value[key]]));
}
async function checkInstance(client) {
  const response = await client.request("GET", "/__instance__/identity");
  requireFact(
    response.status === 200 &&
      response.value?.nonce === client.input.runtime.nonce &&
      response.value.source_revision === client.input.runtime.source_revision,
    "INSTANCE_MARKER_MISMATCH",
  );
}
async function resumeSignal() {
  await new Promise((resolve, reject) => {
    let pending = "";
    const timer = setTimeout(
      () => finish(new Failure("RESTART_SIGNAL_TIMEOUT")),
      10 * 60 * 1000,
    );
    const data = (part) => {
      pending += part.toString();
      if (pending.length > 16)
        return finish(new Failure("RESTART_SIGNAL_INVALID"));
      if (pending.includes("\n"))
        finish(
          pending.trim() === "resume"
            ? undefined
            : new Failure("RESTART_SIGNAL_INVALID"),
        );
    };
    const end = () => finish(new Failure("RESTART_SIGNAL_CLOSED"));
    function finish(error) {
      clearTimeout(timer);
      process.stdin.off("data", data);
      process.stdin.off("end", end);
      process.stdin.pause();
      error ? reject(error) : resolve();
    }
    process.stdin.on("data", data);
    process.stdin.once("end", end);
    process.stdin.resume();
  });
}

async function main() {
  if (process.argv[2] === "--help") {
    process.stdout.write(
      "FRONTEND_ACCOUNT_RUNTIME=/absolute/private/runtime.json node tests/frontend-account-rebuild/runtime-recovery-probe.cjs\nAt ready_for_restart, only the backend operator restarts Central; then enter: resume\n",
    );
    return;
  }
  requireFact(process.argv.length === 2, "ARGUMENTS_REJECTED");
  let stage = "private-input",
    client,
    loggedIn = false;
  try {
    const input = runtimeInput();
    client = new Client(input);
    stage = "tls-instance";
    await checkInstance(client);
    const beforeProcess = processIdentity(input);
    stage = "anonymous-protected";
    requireFact(
      (await client.request("GET", "/api/v1/system/smtp")).status === 401,
      "ANONYMOUS_PROTECTED_401_REQUIRED",
    );
    stage = "login";
    const bootstrap = await client.request("GET", "/api/v1/auth/bootstrap");
    requireFact(
      bootstrap.status === 200 &&
        typeof bootstrap.value?.csrf_token === "string",
      "BOOTSTRAP_REQUIRED",
    );
    const login = await client.request(
      "POST",
      "/api/v1/sessions/login",
      { email: input.credentials.email, password: input.credentials.password },
      bootstrap.value.csrf_token,
    );
    requireFact(login.status === 200, "LOGIN_200_REQUIRED");
    loggedIn = true;
    input.credentials = undefined;
    stage = "before-restart";
    const before = sessionValue(await client.request("GET", "/api/v1/session"));
    client.csrf = before.csrf_token;
    const cookie = client.cookies.get("__Host-agenteam_session");
    requireFact(!!cookie, "SESSION_COOKIE_REQUIRED");
    const smtp = smtpValue(await client.request("GET", "/api/v1/system/smtp"));
    emit({
      stage: "ready_for_restart",
      tlsAndInstanceMatched: true,
      anonymousProtected: 401,
      login: 200,
      currentSession: 200,
      smtpRead: 200,
    });
    stage = "restart-barrier";
    await resumeSignal();
    stage = "after-restart";
    await checkInstance(client);
    const afterProcess = processIdentity(input);
    requireFact(
      beforeProcess.pid !== afterProcess.pid ||
        beforeProcess.ticks !== afterProcess.ticks,
      "CENTRAL_PROCESS_NOT_REPLACED",
    );
    const after = sessionValue(await client.request("GET", "/api/v1/session"));
    client.csrf = after.csrf_token;
    requireFact(
      before.user.id === after.user.id &&
        before.session.id === after.session.id &&
        JSON.stringify(before.user) === JSON.stringify(after.user) &&
        client.cookies.get("__Host-agenteam_session") === cookie,
      "SESSION_OR_IDENTITY_NOT_PRESERVED",
    );
    requireFact(
      smtpValue(await client.request("GET", "/api/v1/system/smtp")) === smtp,
      "SMTP_PROJECTION_NOT_PRESERVED",
    );
    stage = "logout";
    requireFact(
      (await client.request("POST", "/api/v1/sessions/logout", {})).status ===
        204,
      "LOGOUT_204_REQUIRED",
    );
    loggedIn = false;
    requireFact(
      !client.cookies.has("__Host-agenteam_session") &&
        (await client.request("GET", "/api/v1/session")).status === 401,
      "LOGOUT_NOT_CONFIRMED",
    );
    emit({
      stage: "complete",
      processChanged: true,
      tlsAndInstanceMatched: true,
      sameUserAndSession: true,
      sameSessionCookie: true,
      sameSMTPProjection: true,
      logout: 204,
      afterLogout: 401,
    });
  } catch (error) {
    let cleanup = "not-needed";
    if (loggedIn && client) {
      try {
        const current = sessionValue(
          await client.request("GET", "/api/v1/session"),
        );
        client.csrf = current.csrf_token;
        cleanup =
          (await client.request("POST", "/api/v1/sessions/logout", {}))
            .status === 204
            ? "logout-204"
            : "unconfirmed";
      } catch {
        cleanup = "unconfirmed";
      }
    }
    emit({
      stage,
      failed:
        error instanceof Failure ? error.message : "PROBE_OPERATION_FAILED",
      cleanup,
    });
    process.exitCode = 1;
  }
}
main().catch(() => {
  emit({ failed: "PROBE_SETUP_FAILED" });
  process.exitCode = 1;
});
