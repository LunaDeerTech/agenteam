// Independent negative cases against the task-owned real App and SMTP sink.
// No connection is made without PASSKEY_SECURITY_RUN=1. Run only after the
// author releases the shared browser/SMTP window. No SQL state or API mocks.
// NODE_PATH=/workspace/agenteam/tests/account-captcha-web/node_modules
// NODE_EXTRA_CA_CERTS=<owned CA> PASSKEY_OWNED_RUNTIME=<private runtime.json>
// PASSKEY_SECURITY_RUN=1 node tests/account-preview/passkey-security-independent.cjs
const fs = require("node:fs");
const path = require("node:path");
const https = require("node:https");
const {
  createHash,
  randomBytes,
  randomUUID,
  X509Certificate,
} = require("node:crypto");

const origin = "https://localhost:5270";
const scope = "account-passkeys-email-change";
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
function fact(condition, code) {
  if (!condition) throw new Error(code);
}
function privateFile(filename) {
  const stat = fs.lstatSync(filename);
  fact(
    stat.isFile() &&
      !stat.isSymbolicLink() &&
      stat.uid === process.getuid() &&
      (stat.mode & 0o777) === 0o600,
    "PRIVATE_FILE_REQUIRED",
  );
  return fs.readFileSync(filename);
}
function spki(certificate) {
  return createHash("sha256")
    .update(
      certificate.publicKey.export({
        type: "spki",
        format: "der",
      }),
    )
    .digest("base64");
}
function loadRuntime() {
  const filename = process.env.PASSKEY_OWNED_RUNTIME;
  fact(filename && path.isAbsolute(filename), "OWNED_RUNTIME_REQUIRED");
  const directory = fs.lstatSync(path.dirname(filename));
  fact(
    directory.isDirectory() &&
      !directory.isSymbolicLink() &&
      directory.uid === process.getuid() &&
      (directory.mode & 0o777) === 0o700,
    "PRIVATE_DIRECTORY_REQUIRED",
  );
  const runtime = JSON.parse(privateFile(filename));
  const sidecar = JSON.parse(
    privateFile(path.join(path.dirname(filename), "passkey-owned.json")),
  );
  fact(
    runtime.origin === origin &&
      runtime.test_scope === scope &&
      sidecar.test_scope === scope &&
      sidecar.runtime_file === filename &&
      sidecar.nonce === runtime.nonce &&
      /^[a-f0-9]{32}$/.test(runtime.nonce || "") &&
      Date.parse(runtime.expires_at) > Date.now() + 120000,
    "OWNED_SCOPE_OR_EXPIRY_MISMATCH",
  );
  fact(
    process.env.NODE_EXTRA_CA_CERTS === runtime.certificate_ca_file,
    "OWNED_NODE_CA_REQUIRED",
  );
  fact(
    typeof runtime.credentials?.email === "string" &&
      /^[!-~]+@[!-~]+$/.test(runtime.credentials.email) &&
      runtime.credentials.email.length <= 254 &&
      typeof runtime.credentials.password === "string" &&
      runtime.credentials.password.length > 0,
    "OWNED_ADMIN_CREDENTIALS_REQUIRED",
  );
  const leaf = new X509Certificate(privateFile(runtime.certificate_file));
  const ca = new X509Certificate(privateFile(runtime.certificate_ca_file));
  fact(
    leaf.checkHost("localhost") &&
      ca.ca &&
      leaf.checkIssued(ca) &&
      leaf.verify(ca.publicKey) &&
      spki(leaf) === runtime.certificate_spki &&
      Date.parse(leaf.validFrom) <= Date.now() &&
      Date.parse(leaf.validTo) > Date.now() &&
      Date.parse(ca.validFrom) <= Date.now() &&
      Date.parse(ca.validTo) > Date.now(),
    "OWNED_CERTIFICATE_MISMATCH",
  );
  return runtime;
}
async function verifyLiveIdentity(runtime) {
  await new Promise((resolve, reject) => {
    const request = https.get(
      {
        hostname: "127.0.0.1",
        port: 5270,
        servername: "localhost",
        path: "/__fixture__/identity",
        ca: privateFile(runtime.certificate_ca_file),
        headers: { Host: "localhost:5270" },
        timeout: 5000,
      },
      (response) => {
        const peer = response.socket.getPeerCertificate().raw;
        let raw = "";
        response.on("data", (chunk) => {
          raw += chunk;
          if (raw.length > 4096) request.destroy();
        });
        response.on("end", () => {
          try {
            fact(
              response.statusCode === 200 &&
                JSON.parse(raw).nonce === runtime.nonce &&
                spki(new X509Certificate(peer)) === runtime.certificate_spki,
              "OWNED_IDENTITY_MISMATCH",
            );
            resolve();
          } catch {
            reject(new Error("OWNED_TLS_IDENTITY_FAILED"));
          }
        });
        response.on("error", () =>
          reject(new Error("OWNED_TLS_IDENTITY_FAILED")),
        );
      },
    );
    request.on("timeout", () => request.destroy());
    request.on("error", () => reject(new Error("OWNED_TLS_IDENTITY_FAILED")));
  });
}
function mailCode(raw, recipient, purpose) {
  if (!raw) return undefined;
  const split = raw.indexOf("\r\n\r\n");
  if (split < 0) return undefined;
  const headers = raw.slice(0, split).replace(/\r\n[ \t]+/g, " ");
  const to = headers.match(/^To:\s*(.+)$/im)?.[1];
  if (!(to === recipient || to?.endsWith(`<${recipient}>`))) return undefined;
  let body = raw.slice(split + 4);
  if (/^Content-Transfer-Encoding:\s*base64\s*$/im.test(headers))
    body = Buffer.from(body.replace(/\s/g, ""), "base64").toString("utf8");
  else if (/^Content-Transfer-Encoding:\s*quoted-printable\s*$/im.test(headers))
    body = body
      .replace(/=\r\n/g, "")
      .replace(/=([0-9a-f]{2})/gi, (_, hex) =>
        String.fromCharCode(parseInt(hex, 16)),
      );
  return body.match(
    new RegExp(
      `Agenteam ${purpose} verification code: ([0-9]{6})(?:\\r?\\n|$)`,
    ),
  )?.[1];
}

async function main() {
  const result = {
    kind: "independent-real-app-native-webauthn-security",
    pass: false,
    checks: {},
    native_creations: 0,
    native_assertions: 0,
  };
  const clients = [];
  let browser,
    runtime,
    stage = "private-runtime",
    cdp,
    authenticatorId;
  let unexpected = 0;
  try {
    runtime = loadRuntime();
    stage = "strict-tls-identity";
    await verifyLiveIdentity(runtime);
    const { chromium } = require("@playwright/test");
    const {
      readLastMail,
      accountLinkFromMail,
    } = require("../frontend-account-rebuild/mail-reader.cjs");
    const {
      observeOwnedCertificate,
    } = require("../frontend-account-rebuild/fixed-runtime.cjs");
    const deadline = Date.now() + 120000;
    async function lastMailMatching(extract) {
      const until = Math.min(Date.now() + 20000, deadline);
      while (Date.now() < until) {
        const value = extract(await readLastMail(runtime));
        if (value) return value;
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      throw new Error("OWNED_MATCHING_MAIL_REQUIRED");
    }
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: [
        "--no-sandbox",
        `--ignore-certificate-errors-spki-list=${runtime.certificate_spki}`,
      ],
    });
    async function client(name) {
      const context = await browser.newContext({ serviceWorkers: "block" });
      const value = {
        name,
        context,
        page: await context.newPage(),
        trusted: false,
        csrf: "",
      };
      clients.push(value);
      value.page.setDefaultTimeout(10000);
      await value.page.route("**/*", (route) => {
        if (new URL(route.request().url()).origin === origin)
          return route.continue();
        unexpected++;
        return route.abort("blockedbyclient");
      });
      const certificate = await observeOwnedCertificate(value.page, runtime);
      try {
        const response = await value.page.goto(
          origin + "/__fixture__/identity",
        );
        certificate.verify();
        fact(
          response?.status() === 200 &&
            (await response.json()).nonce === runtime.nonce,
          "OWNED_BROWSER_IDENTITY_MISMATCH",
        );
        value.trusted = true;
      } finally {
        await certificate.close();
      }
      return value;
    }
    async function api(
      client,
      route,
      body,
      { method = "POST", key = randomUUID() } = {},
    ) {
      fact(
        client.trusted &&
          route.startsWith("/") &&
          !route.includes("?") &&
          !route.includes("#"),
        "OWNED_REQUEST_REQUIRED",
      );
      return client.page.evaluate(
        async ({ route, body, method, key, csrf }) => {
          const response = await fetch(`/api/v1${route}`, {
            method,
            credentials: "same-origin",
            cache: "no-store",
            redirect: "error",
            referrerPolicy: "no-referrer",
            signal: AbortSignal.timeout(10000),
            headers: {
              Accept: "application/json",
              ...(body === undefined
                ? {}
                : { "Content-Type": "application/json" }),
              ...(csrf ? { "X-CSRF-Token": csrf } : {}),
              ...(key ? { "Idempotency-Key": key } : {}),
            },
            ...(body === undefined ? {} : { body: JSON.stringify(body) }),
          });
          return {
            status: response.status,
            value: response.status === 204 ? null : await response.json(),
          };
        },
        { route, body, method, key, csrf: client.csrf },
      );
    }
    function accepted(response, status, code) {
      fact(response.status === status, code);
      return response.value;
    }
    const read = async (client, route) =>
      accepted(
        await api(client, route, undefined, { method: "GET", key: null }),
        200,
        "OWNED_READ_FAILED",
      );
    async function bootstrap(client) {
      client.csrf = (await read(client, "/auth/bootstrap")).csrf_token;
      fact(
        typeof client.csrf === "string" && client.csrf.length > 0,
        "BROWSER_CSRF_REQUIRED",
      );
    }
    async function login(client, credential) {
      await bootstrap(client);
      accepted(
        await api(client, "/sessions/login", credential),
        200,
        "OWNED_PASSWORD_LOGIN_FAILED",
      );
      const value = await read(client, "/session");
      client.csrf = value.csrf_token;
      client.session = value.session.id;
      client.user = value.user.id;
      fact(
        uuid.test(client.session) && uuid.test(client.user) && client.csrf,
        "STRONG_SESSION_REQUIRED",
      );
      return value;
    }
    function invalidState(response, name) {
      fact(
        response.status === 409 && response.value?.code === "INVALID_STATE",
        name,
      );
    }
    function invalidCode(response, name) {
      fact(
        response.status === 400 &&
          response.value?.code === "INVALID_ARGUMENT" &&
          response.value.field_errors?.some(
            (field) =>
              field.path === "/code" && field.code === "INVALID_OR_EXPIRED",
          ),
        name,
      );
    }
    async function native(client, options, create, selected) {
      await client.page.bringToFront();
      const value = await client.page.evaluate(
        async ({ options, create, selected }) => {
          if (!isSecureContext || document.visibilityState !== "visible")
            throw new Error("NATIVE_VISIBLE_SECURE_CONTEXT_REQUIRED");
          const decode = (text) =>
            Uint8Array.from(
              atob(text.replace(/-/g, "+").replace(/_/g, "/")),
              (v) => v.charCodeAt(0),
            ).buffer;
          const encode = (raw) =>
            btoa(String.fromCharCode(...new Uint8Array(raw)))
              .replace(/\+/g, "-")
              .replace(/\//g, "_")
              .replace(/=+$/, "");
          const descriptor = (value) => ({ ...value, id: decode(value.id) });
          const publicKey = {
            ...options,
            challenge: decode(options.challenge),
          };
          if (create) {
            if (
              options.rp?.id !== "localhost" ||
              options.authenticatorSelection?.userVerification !== "required" ||
              options.authenticatorSelection?.residentKey !== "required"
            )
              throw new Error("NATIVE_CREATION_POLICY_REQUIRED");
            publicKey.user = { ...options.user, id: decode(options.user.id) };
            publicKey.excludeCredentials =
              options.excludeCredentials?.map(descriptor);
          } else {
            if (
              options.rpId !== "localhost" ||
              options.userVerification !== "required"
            )
              throw new Error("NATIVE_ASSERTION_POLICY_REQUIRED");
            const allowed = options.allowCredentials?.filter(
              (item) => item.id === selected,
            );
            if (!allowed || allowed.length !== 1)
              throw new Error("SELECTED_REGISTERED_KEY_REQUIRED");
            publicKey.allowCredentials = allowed.map(descriptor);
          }
          const request = { publicKey, signal: AbortSignal.timeout(15000) };
          const credential = create
            ? await navigator.credentials.create(request)
            : await navigator.credentials.get(request);
          if (!credential || credential.type !== "public-key")
            throw new Error("NATIVE_CREDENTIAL_REQUIRED");
          const response = credential.response;
          const common = {
            id: credential.id,
            rawId: encode(credential.rawId),
            type: credential.type,
            authenticatorAttachment: credential.authenticatorAttachment,
            clientExtensionResults: credential.getClientExtensionResults(),
          };
          return {
            ...common,
            response: create
              ? {
                  clientDataJSON: encode(response.clientDataJSON),
                  attestationObject: encode(response.attestationObject),
                  transports: response.getTransports(),
                }
              : {
                  clientDataJSON: encode(response.clientDataJSON),
                  authenticatorData: encode(response.authenticatorData),
                  signature: encode(response.signature),
                  userHandle: response.userHandle
                    ? encode(response.userHandle)
                    : null,
                },
          };
        },
        { options, create, selected },
      );
      if (create) result.native_creations++;
      else {
        fact(value.rawId === selected, "UNEXPECTED_AUTHENTICATING_KEY");
        result.native_assertions++;
      }
      return value;
    }
    async function proof(client, intent, selected) {
      const options = accepted(
        await api(client, "/me/passkeys/reauth/options", intent, { key: null }),
        200,
        "REAUTH_OPTIONS_FAILED",
      );
      return {
        operation_id: options.operation_id,
        credential: await native(client, options.public_key, false, selected),
      };
    }
    async function grant(client, intent, selected) {
      return accepted(
        await api(
          client,
          "/me/passkeys/reauth/complete",
          await proof(client, intent, selected),
        ),
        200,
        "FRESH_REAUTH_FAILED",
      ).grant_id;
    }
    async function register(client, grantID, label, assertReuse) {
      const before = await read(client, "/me/passkeys");
      const body = {
        grant_id: grantID,
        label,
        expected_version: before.version,
      };
      const options = accepted(
        await api(client, "/me/passkeys/registration/options", body, {
          key: null,
        }),
        200,
        "REGISTRATION_OPTIONS_FAILED",
      );
      if (assertReuse) {
        invalidState(
          await api(client, "/me/passkeys/registration/options", body, {
            key: null,
          }),
          "GRANT_REUSE_ACCEPTED",
        );
        const still = await read(client, "/me/passkeys");
        fact(
          still.version === before.version &&
            still.items.length === before.items.length,
          "GRANT_REUSE_CHANGED_KEYS",
        );
        result.checks.registration_grant_single_consumer = true;
      }
      const credential = await native(client, options.public_key, true);
      const receipt = accepted(
        await api(client, "/me/passkeys/registration/complete", {
          operation_id: options.operation_id,
          credential,
        }),
        200,
        "NATIVE_REGISTRATION_FAILED",
      );
      fact(uuid.test(receipt.id), "REGISTERED_KEY_ID_REQUIRED");
      return { id: receipt.id, rawId: credential.rawId };
    }

    stage = "isolated-account-via-real-invitation";
    const admin = await client("setup-admin");
    fact(
      (await login(admin, runtime.credentials)).user.role === "admin",
      "OWNED_SETUP_ADMIN_REQUIRED",
    );
    const suffix = randomBytes(6).toString("hex");
    const account = {
      email: `passkey-security-${suffix}@example.test`,
      password: randomBytes(24).toString("hex"),
    };
    const invitation = accepted(
      await api(admin, "/system/invitations", { email: account.email }),
      201,
      "OWNED_INVITATION_FAILED",
    );
    fact(uuid.test(invitation.id), "INVITATION_ID_REQUIRED");
    let link = await lastMailMatching((raw) => {
      const value = accountLinkFromMail(raw, {
        origin,
        recipient: account.email,
        purpose: "invite",
      });
      return value && new URL(value).hash.startsWith(`#${invitation.id}.`)
        ? value
        : undefined;
    });
    const first = await client("independent-primary");
    await bootstrap(first);
    accepted(
      await api(first, "/invitations/redeem", {
        token: new URL(link).hash.slice(1),
        username: `security-${suffix}`,
        display_name: "Independent security fixture",
        password: account.password,
        confirmation: account.password,
      }),
      201,
      "OWNED_REDEEM_FAILED",
    );
    link = undefined;
    await login(first, account);
    fact(first.user !== admin.user, "INDEPENDENT_USER_REQUIRED");
    cdp = await first.context.newCDPSession(first.page);
    await cdp.send("WebAuthn.enable");
    ({ authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
      options: {
        protocol: "ctap2",
        transport: "internal",
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    }));
    stage = "two-real-native-keys";
    const recovery = accepted(
      await api(first, "/me/passkeys/recovery/request", {
        password: account.password,
      }),
      202,
      "FIRST_KEY_RECOVERY_FAILED",
    );
    let code = await lastMailMatching((raw) =>
      mailCode(raw, account.email, "Passkey recovery"),
    );
    const firstGrant = accepted(
      await api(first, "/me/passkeys/recovery/confirm", {
        request_id: recovery.request_id,
        code,
      }),
      200,
      "FIRST_KEY_EMAIL_PROOF_FAILED",
    );
    code = undefined;
    const keyOne = await register(
      first,
      firstGrant.grant_id,
      "Independent key one",
      false,
    );
    const addGrant = await grant(
      first,
      { purpose: "manage_passkeys", action: "register" },
      keyOne.rawId,
    );
    // Model a second device for creation: excludeCredentials must remain real.
    // Preserve only this test's native-generated key in process memory, create
    // on an empty virtual device, then restore it for deterministic selection.
    // Both keys and every assertion still come from Chromium's authenticator.
    let savedVirtualKey = (
      await cdp.send("WebAuthn.getCredentials", { authenticatorId })
    ).credentials[0];
    fact(
      savedVirtualKey &&
        Buffer.from(savedVirtualKey.credentialId, "base64").toString(
          "base64url",
        ) === keyOne.rawId,
      "NATIVE_KEY_EXPORT_MISMATCH",
    );
    await cdp.send("WebAuthn.clearCredentials", { authenticatorId });
    const keyTwo = await register(first, addGrant, "Independent key two", true);
    await cdp.send("WebAuthn.addCredential", {
      authenticatorId,
      credential: savedVirtualKey,
    });
    savedVirtualKey = undefined;
    fact(
      keyOne.id !== keyTwo.id &&
        keyOne.rawId !== keyTwo.rawId &&
        (await cdp.send("WebAuthn.getCredentials", { authenticatorId }))
          .credentials.length === 2,
      "TWO_REAL_KEYS_REQUIRED",
    );
    const second = await client("independent-secondary");
    await login(second, account);
    fact(
      second.user === first.user && second.session !== first.session,
      "TWO_INDEPENDENT_SESSIONS_REQUIRED",
    );

    stage = "operation-and-grant-session-action-target";
    const before = await read(first, "/me/passkeys");
    const signed = await proof(
      first,
      { purpose: "manage_passkeys", action: "rename", passkey_id: keyTwo.id },
      keyOne.rawId,
    );
    invalidState(
      await api(second, "/me/passkeys/reauth/complete", signed),
      "CROSS_SESSION_OPERATION_ACCEPTED",
    );
    const renameGrant = accepted(
      await api(first, "/me/passkeys/reauth/complete", signed),
      200,
      "ORIGINAL_OPERATION_REJECTED",
    ).grant_id;
    invalidState(
      await api(first, "/me/passkeys/reauth/complete", signed),
      "CONSUMED_OPERATION_REPLAY_ACCEPTED",
    );
    const rename = {
      expected_version: before.version,
      grant_id: renameGrant,
      label: "Independent renamed key",
    };
    invalidState(
      await api(second, `/me/passkeys/${keyTwo.id}`, rename, {
        method: "PATCH",
      }),
      "CROSS_SESSION_GRANT_ACCEPTED",
    );
    invalidState(
      await api(
        first,
        `/me/passkeys/${keyTwo.id}`,
        {
          expected_version: before.version,
          grant_id: renameGrant,
        },
        { method: "DELETE" },
      ),
      "CROSS_ACTION_GRANT_ACCEPTED",
    );
    invalidState(
      await api(first, `/me/passkeys/${keyOne.id}`, rename, {
        method: "PATCH",
      }),
      "CROSS_TARGET_GRANT_ACCEPTED",
    );
    const unchanged = await read(first, "/me/passkeys");
    fact(
      unchanged.version === before.version &&
        JSON.stringify(
          unchanged.items.map(({ id, label }) => ({ id, label })),
        ) ===
          JSON.stringify(before.items.map(({ id, label }) => ({ id, label }))),
      "DENIED_GRANTS_CHANGED_KEYS",
    );
    accepted(
      await api(first, `/me/passkeys/${keyTwo.id}`, rename, {
        method: "PATCH",
      }),
      200,
      "CORRECT_BOUND_GRANT_REJECTED",
    );
    fact(
      (await read(first, "/me/passkeys")).items.find(
        (item) => item.id === keyTwo.id,
      )?.label === rename.label,
      "CORRECT_RENAME_NOT_PERSISTED",
    );
    result.checks.operation_session_and_replay = true;
    result.checks.grant_session_action_target = true;
    result.checks.rejected_misuse_preserves_original_authority = true;

    stage = "email-request-with-fresh-key-proof";
    const profile = await read(first, "/me");
    const emailGrant = await grant(
      first,
      { purpose: "change_email" },
      keyOne.rawId,
    );
    const targetEmail = `passkey-security-target-${suffix}@example.test`;
    const request = accepted(
      await api(first, "/me/email-change/request", {
        grant_id: emailGrant,
        email: targetEmail,
        expected_version: profile.user.version,
      }),
      202,
      "EMAIL_REQUEST_FAILED",
    );
    code = await lastMailMatching((raw) =>
      mailCode(raw, targetEmail, "Email address change"),
    );
    const confirmation = { request_id: request.request_id, code };
    code = undefined;
    invalidCode(
      await api(second, "/me/email-change/confirm", confirmation),
      "CROSS_SESSION_EMAIL_CONFIRM_ACCEPTED",
    );
    fact(
      (await read(first, "/me")).user.email === account.email,
      "EMAIL_CHANGED_BEFORE_AUTHORIZED_CONFIRMATION",
    );
    result.checks.email_confirmation_session_bound = true;

    stage = "revoke-authenticating-key-before-email-confirmation";
    const deleteGrant = await grant(
      first,
      { purpose: "manage_passkeys", action: "delete", passkey_id: keyOne.id },
      keyTwo.rawId,
    );
    const currentKeys = await read(first, "/me/passkeys");
    accepted(
      await api(
        first,
        `/me/passkeys/${keyOne.id}`,
        {
          expected_version: currentKeys.version,
          grant_id: deleteGrant,
        },
        { method: "DELETE" },
      ),
      200,
      "AUTHENTICATING_KEY_REVOKE_FAILED",
    );
    const remaining = await read(first, "/me/passkeys");
    fact(
      remaining.items.length === 1 && remaining.items[0].id === keyTwo.id,
      "KEY_REVOCATION_NOT_PERSISTED",
    );
    invalidCode(
      await api(first, "/me/email-change/confirm", confirmation),
      "REVOKED_KEY_EMAIL_CONFIRM_ACCEPTED",
    );
    const finalProfile = await read(first, "/me");
    const finalSession = await read(first, "/session");
    fact(
      finalProfile.user.email === account.email &&
        finalProfile.user.version === profile.user.version &&
        finalSession.session.id === first.session,
      "REJECTED_CONFIRM_CHANGED_ACCOUNT_OR_SESSION",
    );
    result.checks.revoked_authenticating_key_invalidates_pending_email = true;
    result.checks.rejected_confirm_preserves_email_version_and_session = true;
    fact(unexpected === 0, "UNEXPECTED_NETWORK_REQUEST");
    result.pass = true;
  } catch (error) {
    result.failure_stage = stage;
    result.failure = /^[A-Z0-9_]+$/.test(error.message || "")
      ? error.message
      : "INDEPENDENT_ASSERTION_FAILED";
    result.error_type = error.name;
    process.exitCode = 1;
  } finally {
    result.cleanup = [];
    for (const client of clients) {
      let status = "NOT_TRUSTED";
      if (client.trusted) {
        status = await client.page
          .evaluate(async () => {
            const current = await fetch("/api/v1/session", {
              cache: "no-store",
              signal: AbortSignal.timeout(10000),
            });
            if (current.status === 401) return 401;
            if (current.status !== 200) return current.status;
            const session = await current.json();
            return (
              await fetch("/api/v1/sessions/logout", {
                method: "POST",
                signal: AbortSignal.timeout(10000),
                headers: {
                  "Content-Type": "application/json",
                  "X-CSRF-Token": session.csrf_token,
                  "Idempotency-Key": crypto.randomUUID(),
                },
                body: "{}",
              })
            ).status;
          })
          .catch(() => "CLEANUP_FAILED");
      }
      result.cleanup.push({ client: client.name, logout_status: status });
      if (client.trusted && ![204, 401].includes(status)) {
        result.pass = false;
        process.exitCode = 1;
      }
    }
    if (cdp && authenticatorId)
      await cdp
        .send("WebAuthn.removeVirtualAuthenticator", { authenticatorId })
        .catch(() => {});
    for (const client of clients)
      await client.context.close().catch(() => {
        result.pass = false;
        process.exitCode = 1;
      });
    if (browser)
      await browser.close().catch(() => {
        result.pass = false;
        process.exitCode = 1;
      });
    result.browser_closed = true;
    result.unexpected_network_requests = unexpected;
    console.log(JSON.stringify(result));
  }
}

if (process.env.PASSKEY_SECURITY_RUN !== "1") {
  console.log(
    JSON.stringify({
      kind: "independent-passkey-security",
      run: false,
      reason: "explicit-owned-run-required",
    }),
  );
} else {
  main().catch(() => {
    console.log(
      JSON.stringify({ pass: false, failure: "INDEPENDENT_HARNESS_FAILED" }),
    );
    process.exitCode = 1;
  });
}
