// Independent review probe. Set SUMMARY_SECURITY_RUN=1 and MODEL_CATALOG_RUNTIME
// to an active owned manifest; strict TLS/nonce checks run before credentials.
// Requires an already configured selection and an exclusive API window.
// Restores original configured contents with a new version and removes its model.
const fs = require("node:fs");
const https = require("node:https");
const { randomUUID, X509Certificate } = require("node:crypto");
const {
  loadRuntime,
  strictIdentity,
} = require("./owned-fixture.cjs");

const output = {
  kind: "independent-summary-selection-cas",
  passed: false,
  browser_used: false,
  external_provider_used: false,
  smtp_actions: false,
  checks: [],
  cleanup: [],
};
let stage = "explicit-owned-run-gate";
function check(ok, name) {
  if (!ok) throw new Error(name);
}
function key() {
  return randomUUID();
}
function jar() {
  return new Map();
}
function cloneJar(value) {
  return new Map(value);
}
function cookie(value) {
  return [...value].map(([name, content]) => `${name}=${content}`).join("; ");
}
function applyCookies(value, headers) {
  for (const item of headers["set-cookie"] || []) {
    const [pair] = item.split(";");
    const index = pair.indexOf("=");
    if (index <= 0) throw new Error("COOKIE_FORMAT_INVALID");
    const name = pair.slice(0, index);
    const content = pair.slice(index + 1);
    if (!content || /(?:^|;)\s*max-age=0(?:;|$)/i.test(item)) value.delete(name);
    else value.set(name, content);
  }
}
function uuid(value) {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value);
}

async function main() {
  check(process.env.SUMMARY_SECURITY_RUN === "1", "EXPLICIT_OWNED_RUN_REQUIRED");
  const runtime = loadRuntime();
  await strictIdentity(runtime);
  const ca = fs.readFileSync(runtime.certificate_ca_file);
  const expectedLeaf = new X509Certificate(fs.readFileSync(runtime.certificate_file));
  const admin = jar();
  const contender = jar();
  let modelID;
  let selectionBefore;
  let failure;
  const secretMarkers = [runtime.provider_credential, runtime.credentials.password, runtime.regular_credentials.password];

  function request(session, pathname, method = "GET", body, options = {}) {
    check(pathname.startsWith("/") && !pathname.startsWith("//"), "FIXED_PATH_REQUIRED");
    const bytes = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    const headers = {
      Accept: "application/json",
      Origin: runtime.origin,
      "Sec-Fetch-Site": "same-origin",
      ...(session.size ? { Cookie: cookie(session) } : {}),
      ...(bytes ? { "Content-Type": "application/json", "Content-Length": bytes.length } : {}),
      ...(options.csrf ? { "X-CSRF-Token": options.csrf } : {}),
      ...(options.key ? { "Idempotency-Key": options.key } : {}),
      ...(options.fixture ? { "X-Fixture-Nonce": runtime.nonce } : {}),
    };
    return new Promise((resolve, reject) => {
      const req = https.request(`${runtime.origin}${pathname}`, {
        method,
        headers,
        ca,
        rejectUnauthorized: true,
        agent: false,
        lookup: (_host, options, callback) => options.all
          ? callback(null, [{ address: "127.0.0.1", family: 4 }])
          : callback(null, "127.0.0.1", 4),
      }, (res) => {
        const parts = [];
        let length = 0;
        const peer = res.socket.getPeerCertificate(true).raw;
        if (!peer || new X509Certificate(peer).fingerprint256 !== expectedLeaf.fingerprint256) {
          req.destroy();
          reject(new Error("OWNED_LEAF_CHANGED"));
          return;
        }
        res.on("data", (part) => {
          length += part.length;
          if (length > 1 << 20) req.destroy();
          else parts.push(part);
        });
        res.on("aborted", () => reject(new Error("RESPONSE_ABORTED")));
        res.on("error", () => reject(new Error("RESPONSE_IO_FAILED")));
        res.on("end", () => {
          try {
            clearTimeout(timer);
            const raw = Buffer.concat(parts).toString("utf8");
            check(!secretMarkers.some((marker) => raw.includes(marker)), "SECRET_IN_HTTP_RESPONSE");
            const value = raw ? JSON.parse(raw) : undefined;
            applyCookies(session, res.headers);
            resolve({ status: res.statusCode, value });
          } catch (error) {
            reject(error.message === "SECRET_IN_HTTP_RESPONSE" ? error : new Error("SAFE_RESPONSE_DECODE_FAILED"));
          }
        });
      });
      const timer = setTimeout(() => req.destroy(), 12000);
      req.on("close", () => clearTimeout(timer));
      req.on("error", () => reject(new Error("OWNED_TLS_REQUEST_FAILED")));
      req.end(bytes);
    });
  }
  async function login(session, credentials) {
    const bootstrap = await request(session, "/api/v1/auth/bootstrap");
    check(bootstrap.status === 200 && typeof bootstrap.value.csrf_token === "string", "BOOTSTRAP_FAILED");
    const result = await request(session, "/api/v1/sessions/login", "POST", credentials, { csrf: bootstrap.value.csrf_token, key: key() });
    check(result.status === 200, "LOGIN_FAILED");
    const current = await request(session, "/api/v1/session");
    check(current.status === 200 && typeof current.value.csrf_token === "string", "SESSION_FAILED");
    return current.value.csrf_token;
  }
  async function csrf(session) {
    const current = await request(session, "/api/v1/session");
    check(current.status === 200 && typeof current.value.csrf_token === "string", "SESSION_MISSING");
    return current.value.csrf_token;
  }
  async function mutate(session, pathname, method, body, originalKey = key()) {
    return request(session, `/api/v1${pathname}`, method, body, { csrf: await csrf(session), key: originalKey });
  }
  async function logout(session, name) {
    if (!session.size) return;
    const current = await request(session, "/api/v1/session");
    if (current.status === 401) {
      session.clear();
      output.cleanup.push({ name, already_inactive: true });
      return;
    }
    check(current.status === 200, "CLEANUP_SESSION_READ_FAILED");
    const result = await request(session, "/api/v1/sessions/logout", "POST", {}, { csrf: current.value.csrf_token, key: key() });
    check(result.status === 204, "LOGOUT_FAILED");
    check((await request(session, "/api/v1/session")).status === 401, "LOGOUT_NOT_EFFECTIVE");
    output.cleanup.push({ name, logout: 204, after: 401 });
    session.clear();
  }
  const fields = ["embedding", "summary", "reranker", "image"];
  const selectionPath = "/api/v1/system/model-selection";
  function sameConfig(a, b) {
    return !!a && !!b && fields.every((name) => a[name] === b[name]);
  }
  function validSelection(value) {
    return uuid(value?.id) && /^[1-9][0-9]*$/.test(value?.version || "") &&
      value.configured && Object.keys(value.configured).sort().join() === [...fields].sort().join() &&
      uuid(value.configured.embedding) && uuid(value.configured.summary) &&
      ["reranker", "image"].every((name) => value.configured[name] === null || uuid(value.configured[name]));
  }
  async function readSelection(session) {
    const result = await request(session, selectionPath);
    check(result.status === 200 && validSelection(result.value), "CONFIGURED_SELECTION_REQUIRED");
    return result.value;
  }
  try {
    stage = "two-owned-admin-sessions";
    await login(admin, runtime.credentials);
    await login(contender, runtime.credentials);
    selectionBefore = await readSelection(admin);
    stage = "isolated-summary-model-copy";
    const model = await request(admin, `/api/v1/system/models/${selectionBefore.configured.summary}`);
    check(model.status === 200 && uuid(model.value?.provider_id) && model.value.input?.type === "chat" &&
      model.value.input?.enabled === true && model.value.input.capabilities?.structured_output_modes?.includes("json_schema"), "CURRENT_SUMMARY_MODEL_INVALID");
    const copy = await mutate(admin, "/system/models", "POST", {
      provider_id: model.value.provider_id,
      input: { ...model.value.input, name: `Independent summary ${key()}`, provider_model_id: `independent-summary-${key()}` },
    });
    check(copy.status === 200 && copy.value?.kind === "model.create" && uuid(copy.value.resource_id) && copy.value.version === "1", "SUMMARY_COPY_FAILED");
    modelID = copy.value.resource_id;
    const inputA = { id: selectionBefore.id, expected_version: selectionBefore.version, ...selectionBefore.configured };
    const inputB = { ...inputA, summary: modelID };
    const keyA = key(), keyB = key();
    const csrfA = await csrf(admin), csrfB = await csrf(contender);
    stage = "parallel-same-version-distinct-summary-cas";
    const results = await Promise.all([
      request(admin, selectionPath, "PUT", inputA, { csrf: csrfA, key: keyA }),
      request(contender, selectionPath, "PUT", inputB, { csrf: csrfB, key: keyB }),
    ]);
    check(results.filter((result) => result.status === 200).length === 1, "CAS_EXPECTED_ONE_SUCCESS");
    const winner = results[0].status === 200 ? 0 : 1;
    const lost = results[1 - winner];
    check(lost.status === 409 && ["VERSION_CONFLICT", "RESOURCE_BUSY"].includes(lost.value?.code), "CAS_LOSER_NOT_REFUSED");
    const receipt = results[winner].value;
    const winningBody = winner ? inputB : inputA;
    const winningKey = winner ? keyB : keyA;
    const winningJar = winner ? contender : admin;
    const survivingJar = winner ? admin : contender;
    check(receipt?.kind === "model.selection.update" && receipt.resource_id === selectionBefore.id &&
      receipt.version === String(BigInt(selectionBefore.version) + 1n) && receipt.affected_references === "0", "CAS_RECEIPT_MISMATCH");
    const committed = await readSelection(survivingJar);
    check(committed.id === selectionBefore.id && committed.version === receipt.version && sameConfig(committed.configured, winningBody), "CAS_STATE_TORN_OR_REPEATED");
    output.checks.push({ name: stage, success_count: 1, loser_status: lost.status, loser_code: lost.value.code, version_delta: 1 });
    stage = "same-key-changed-body-refused";
    const changed = { ...winningBody, summary: winner ? selectionBefore.configured.summary : modelID };
    const reused = await request(winningJar, selectionPath, "PUT", changed, { csrf: await csrf(winningJar), key: winningKey });
    check(reused.status === 409 && reused.value?.code === "IDEMPOTENCY_KEY_REUSED", "BODY_CHANGE_NOT_REFUSED");
    const afterChanged = await readSelection(survivingJar);
    check(afterChanged.version === committed.version && sameConfig(afterChanged.configured, committed.configured), "BODY_CHANGE_MUTATED_SELECTION");
    output.checks.push({ name: stage, status: 409, code: "IDEMPOTENCY_KEY_REUSED", version_delta: 0 });
    stage = "revoked-session-original-command-refused";
    const stale = cloneJar(winningJar);
    const staleCSRF = await csrf(winningJar);
    await logout(winningJar, "winning-session");
    const revoked = await request(stale, selectionPath, "PUT", winningBody, { csrf: staleCSRF, key: winningKey });
    stale.clear();
    check(revoked.status === 401 && revoked.value?.code === "SESSION_REVOKED", "REVOKED_COMMAND_NOT_REFUSED");
    const afterRevoked = await readSelection(survivingJar);
    check(afterRevoked.version === committed.version && sameConfig(afterRevoked.configured, committed.configured), "REVOKED_COMMAND_MUTATED_SELECTION");
    output.checks.push({ name: stage, status: 401, code: "SESSION_REVOKED", version_delta: 0 });
  } catch (error) {
    failure = { stage, reason: /^[A-Z_]+$/.test(error?.message || "") ? error.message : "INDEPENDENT_PROBE_FAILED" };
  } finally {
    stage = "restore-owned-selection-and-remove-copy";
    try {
      if (selectionBefore) {
        const session = admin.size ? admin : contender;
        if (!session.size || (await request(session, "/api/v1/session")).status !== 200) await login(session, runtime.credentials);
        const current = await readSelection(session);
        check(current.id === selectionBefore.id, "CLEANUP_SINGLETON_CHANGED");
        if (!sameConfig(current.configured, selectionBefore.configured)) {
          const restored = await mutate(session, "/system/model-selection", "PUT", {
            id: current.id, expected_version: current.version, ...selectionBefore.configured,
          });
          check(restored.status === 200 && restored.value?.kind === "model.selection.update" &&
            restored.value.resource_id === current.id && restored.value.version === String(BigInt(current.version) + 1n), "RESTORE_SELECTION_FAILED");
        }
        const restoredState = await readSelection(session);
        check(sameConfig(restoredState.configured, selectionBefore.configured), "SELECTION_CONTENT_NOT_RESTORED");
        output.cleanup.push({ resource: "selection", original_content_restored: true, version_rolled_back: false });
        if (modelID) {
          const deleted = await mutate(session, `/system/models/${modelID}`, "DELETE", { expected_version: "1", replacement: null });
          check(deleted.status === 200 && deleted.value?.kind === "model.delete" && deleted.value.resource_id === modelID, "SUMMARY_COPY_CLEANUP_FAILED");
          check((await request(session, `/api/v1/system/models/${modelID}`)).status === 404, "SUMMARY_COPY_STILL_EXISTS");
          output.cleanup.push({ resource: "independent-summary-copy", deleted: true });
        }
      }
    } catch (error) {
      failure ||= { stage, reason: /^[A-Z_]+$/.test(error?.message || "") ? error.message : "OWNED_RESOURCE_CLEANUP_FAILED" };
    }
    for (const [session, name] of [[admin, "admin-session"], [contender, "contender-session"]]) {
      try { await logout(session, name); }
      catch { failure ||= { stage, reason: "OWNED_SESSION_CLEANUP_FAILED" }; }
    }
  }
  output.passed = !failure;
  if (failure) output.failure = failure;
  console.log(JSON.stringify(output));
  process.exitCode = failure ? 1 : 0;
}
main().catch(() => {
  output.failure = { stage, reason: "OWNED_SETUP_OR_PROBE_FAILED" };
  console.log(JSON.stringify(output));
  process.exitCode = 1;
});
