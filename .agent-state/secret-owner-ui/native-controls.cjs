// No browser or backend. Actual observer/API/Session with controlled transport
// and PW events. Held native/PW tails exercise first-explicit retirement.
const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  path = require("node:path"),
  vm = require("node:vm"),
  Module = require("node:module");
const { EventEmitter } = require("node:events"),
  { webcrypto } = require("node:crypto");
const root = path.resolve(__dirname, "../.."),
  ts = require(path.join(root, "web/node_modules/typescript"));
const cache = new Map();
function source(file) {
  file = path.resolve(file);
  if (cache.has(file)) return cache.get(file).exports;
  const m = new Module(file);
  m.filename = file;
  m.paths = Module._nodeModulePaths(path.dirname(file));
  cache.set(file, m);
  const original = m.require.bind(m);
  m.require = (name) => {
    const p = path.resolve(path.dirname(file), name) + ".ts";
    return name.startsWith(".") && fs.existsSync(p)
      ? source(p)
      : original(name);
  };
  m._compile(
    ts.transpileModule(fs.readFileSync(file, "utf8"), {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.CommonJS,
      },
    }).outputText,
    file,
  );
  return m.exports;
}
const code = ts.transpileModule(
  fs.readFileSync(
    path.join(
      root,
      "tests/account-captcha-web/e2e/project-secret-owner.native.ts",
    ),
    "utf8",
  ),
  {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
    },
  },
).outputText;
const { createAccountAPI } = source(path.join(root, "web/src/api/account.ts")),
  { createProjectSecretsAPI } = source(
    path.join(root, "web/src/api/project-secrets.ts"),
  ),
  { createSessionController } = source(
    path.join(root, "web/src/composables/useSession.ts"),
  );
const id = (n) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`,
  time = "2026-10-10T12:00:00.123456Z",
  project = id(10),
  target = id(20),
  user = id(1),
  prefix = `/api/v1/projects/${project}/secret-variables`;
const session = {
  user: {
    id: user,
    email: "owner@example.test",
    username: "owner",
    display_name: "Owner",
    role: "user",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(2),
    issued_at: time,
    idle_expires_at: time,
    absolute_expires_at: time,
  },
  csrf_token: "S".repeat(43),
};
const metadata = {
  id: target,
  project_id: project,
  type: "secret",
  name: "UI_SECRET",
  description: "Initial safe metadata",
  version: "1",
  created_at: time,
  updated_at: time,
};
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
const tick = () => new Promise((r) => setImmediate(r));
async function until(fn) {
  const end = Date.now() + 2000;
  while (!fn()) {
    if (Date.now() > end) throw Error("CONTROL_SETTLEMENT");
    await tick();
  }
}
global.document = { documentElement: { dataset: {} } };
let unhandled = 0,
  checks = 0;
process.on("unhandledRejection", () => unhandled++);
async function scenario(mode = "normal") {
  const gate = deferred(),
    entered = deferred(),
    page = new EventEmitter(),
    context = new EventEmitter();
  let auth,
    observer,
    count = 0,
    rows = [],
    last = null,
    patchKey = null,
    closed = false,
    fetchPromise,
    publicPromise,
    readerThis = true,
    cancelThis = true;
  const binding = {
    entry: "entry",
    asset: "controlled-singleton",
    exportName: "useSession",
  };
  const box = {
    exports: {},
    URL,
    URLSearchParams,
    Request,
    Response,
    Headers,
    ReadableStream,
    Uint8Array,
    TextDecoder,
    TextEncoder,
    Object,
    JSON,
    Date,
    Set,
    Map,
    Promise,
    crypto: webcrypto,
    location: { origin: "http://127.0.0.1:31000" },
    setTimeout,
    clearTimeout,
    console,
    require(name) {
      if (name === "./knowledge-owner-read.native")
        return { knowledgeSessionBinding: async () => binding };
      if (name === binding.asset) return { useSession: () => auth };
      throw Error("CONTROL_IMPORT");
    },
  };
  box.window = box;
  page.url = () => box.location.origin + "/owner/demo/settings/secrets";
  page.context = () => context;
  page.isClosed = () => closed;
  page.evaluate = async (fn, arg) => fn(arg);
  box.fetch = function (input, init) {
    if (input === "/api/v1/session")
      return Promise.resolve(
        new Response(JSON.stringify(session), {
          headers: { "content-type": "application/json" },
        }),
      );
    assert(input.startsWith(prefix));
    count++;
    const method = init.method,
      xid = id(100 + count),
      request = {
        url: () => box.location.origin + input,
        method: () => method,
      };
    let body,
      status = 200;
    if (input === prefix + "?limit=50") body = { items: rows };
    else if (input === prefix && method === "POST") {
      const p = JSON.parse(init.body);
      assert.equal(p.request.value, "CONTROL_SECRET_FIRST");
      rows = [{ ...metadata }];
      last = {
        command: "project.secret_variable.create",
        changed: true,
        event_id: id(40),
        audit_id: id(41),
        variable: rows[0],
      };
      body = last;
    } else if (method === "PATCH") {
      const p = JSON.parse(init.body);
      assert.equal(p.request.value, "CONTROL_SECRET_SECOND");
      rows = [
        {
          ...metadata,
          name: "UI_SECRET_UPDATED",
          description: "Updated safe metadata",
          version: "2",
        },
      ];
      last = {
        command: "project.secret_variable.update",
        changed: true,
        event_id: id(42),
        audit_id: id(43),
        variable: rows[0],
      };
      patchKey = new Headers(init.headers).get("idempotency-key");
      body = "Owned backend response unavailable\n";
      status = 502;
    } else if (input.endsWith("/commands/lookup")) {
      assert.equal(new Headers(init.headers).get("idempotency-key"), patchKey);
      assert.deepEqual(JSON.parse(init.body), {
        command: "project.secret_variable.update",
        target_id: target,
        expected_version: "1",
      });
      body = { status: "committed", receipt: last };
    } else if (method === "DELETE") {
      rows = [];
      body = {
        command: "project.secret_variable.delete",
        changed: true,
        event_id: id(44),
        audit_id: id(45),
        deleted: {
          id: target,
          project_id: project,
          type: "secret",
          version: "3",
          deleted_at: time,
        },
      };
    } else if (method === "GET") body = rows[0];
    else throw Error("CONTROL_ROUTE");
    if (mode === "leaked-body" && count === 1)
      body = { items: [{ ...metadata, description: "CONTROL_SECRET_FIRST" }] };
    const bytes = new TextEncoder().encode(
      status === 502 ? body : JSON.stringify(body),
    );
    const stream = new ReadableStream({
        start(c) {
          c.enqueue(bytes);
          c.close();
        },
      }),
      get = stream.getReader;
    stream.getReader = function (...args) {
      const reader = Reflect.apply(get, this, args),
        read = reader.read,
        cancel = reader.cancel;
      reader.read = function (...xs) {
        readerThis &&= this === reader;
        return Reflect.apply(read, this, xs);
      };
      reader.cancel = function (...xs) {
        cancelThis &&= this === reader;
        const original = Reflect.apply(cancel, this, xs);
        if (mode === "held-reader" && count === 10)
          return original.then(() => {
            entered.resolve();
            return gate.promise;
          });
        return original;
      };
      return reader;
    };
    const response = new Response(stream, {
      status,
      headers: {
        "content-type": status === 502 ? "text/plain" : "application/json",
        "content-length": String(bytes.length),
        "x-request-id": xid,
      },
    });
    const ordinal = count,
      pw = {
        request: () => request,
        status: () => status,
        headerValue: async () => xid,
        finished: () => {
          if (mode === "held-pw" && ordinal === 10) {
            entered.resolve();
            return gate.promise.then(() => null);
          }
          return Promise.resolve(null);
        },
      };
    page.emit("request", request);
    queueMicrotask(() => {
      page.emit("response", pw);
      if (mode === "request-failed" && ordinal === 1)
        page.emit("requestfailed", request);
      else page.emit("requestfinished", request);
    });
    fetchPromise = Promise.resolve(response);
    return fetchPromise;
  };
  vm.createContext(box);
  vm.runInContext(code, box);
  const transport = (...args) => {
    const p = box.fetch(...args);
    if (args[0].startsWith(prefix)) assert.equal(p, fetchPromise);
    return p;
  };
  const args = [createAccountAPI(transport)];
  args[16] = { secrets: createProjectSecretsAPI(transport) };
  auth = createSessionController(...args);
  await auth.restore();
  assert.equal(auth.state.user.id, user);
  for (const method of ["list", "get", "execute", "lookup"]) {
    const original = auth.secrets[method];
    auth.secrets[method] = function (...args) {
      assert.equal(this, auth.secrets);
      publicPromise = Reflect.apply(original, this, args);
      return publicPromise;
    };
  }
  observer = await box.exports.observeSecretOwner(page, {
    root,
    dist: "/unused",
    project,
    user,
    forbidden: ["CONTROL_SECRET_FIRST", "CONTROL_SECRET_SECOND"],
    expiresAt: Date.now() + 10000,
  });
  const call = async (method, ...args) => {
    const p = auth.secrets[method](...args);
    assert.equal(p, publicPromise);
    return p;
  };
  if (mode === "leaked-body") {
    await call("list", project, { limit: 50 });
    await until(() => box.__secretOwnerObservation.snapshot().pending === 0);
    const result = await observer.finish();
    assert.equal(result.browser.failed, true);
    assert.equal(box.exports.secretOriginalCompleted(result), false);
    checks += 2;
    return;
  }
  await call("list", project, { limit: 50 });
  await call("execute", {
    kind: "create",
    projectID: project,
    request: {
      variable_id: target,
      name: "UI_SECRET",
      description: "Initial safe metadata",
      value: "CONTROL_SECRET_FIRST",
    },
  });
  await call("get", project, target);
  await call("list", project, { limit: 50 });
  await assert.rejects(
    call("execute", {
      kind: "update",
      projectID: project,
      targetID: target,
      expectedVersion: "1",
      request: {
        name: "UI_SECRET_UPDATED",
        description: "Updated safe metadata",
        value: "CONTROL_SECRET_SECOND",
      },
    }),
  );
  assert.equal(auth.secrets.progress.phase, "uncertain");
  await call("lookup");
  await call("get", project, target);
  await call("list", project, { limit: 50 });
  await call("execute", {
    kind: "delete",
    projectID: project,
    targetID: target,
    expectedVersion: "2",
  });
  const final = call("list", project, { limit: 50 });
  let result;
  if (mode === "held-reader" || mode === "held-pw") {
    await entered.promise;
    const finish = observer.finish();
    assert.equal(observer.finish(), finish);
    assert.equal(box.__secretOwnerObservation.snapshot().retired, true);
    if (mode === "held-reader") {
      assert.equal(auth.state.busy, true);
      assert(box.__secretOwnerObservation.snapshot().first.pending > 0);
    }
    gate.resolve();
    await final;
    result = await finish;
    assert.equal(result.pw_pending, 0);
    assert.equal(result.browser.pending, 0);
    assert.equal(box.exports.secretOriginalCompleted(result), false);
    checks += 4;
  } else {
    await final;
    await until(() => box.__secretOwnerObservation.snapshot().pending === 0);
    await tick();
    if (process.argv.includes("--diagnostics-only")) {
      assert.equal(await observer.ready(), true);
      const sampled = observer.diagnostics();
      assert.equal(sampled.node_ready, true);
      assert.equal(sampled.browser_pending, 0);
      assert(Object.isFrozen(sampled));
      assert(Object.isFrozen(sampled.pw[0]));
    }
    if (mode === "identity-change") auth.leave();
    result = await observer.finish();
    assert.equal(readerThis, true);
    assert.equal(cancelThis, true);
    assert.equal(
      box.exports.secretOriginalCompleted(result),
      mode === "normal",
    );
    checks += 3;
  }
  if (mode === "normal") {
    if (process.argv.includes("--diagnostics-only")) {
      const marker = "SYNTHETIC_DIAGNOSTIC_VALUE";
      const copied = structuredClone(result);
      const original = box.exports.secretOwnerDiagnostic(copied);
      copied.rows[0].failed = 1;
      copied.rows[0].url = marker;
      copied.rows[0].xid = marker;
      copied.browser.reason = marker;
      copied.browser.rows[0].operation = marker;
      copied.browser.rows[0].body = { value: marker };
      copied.browser.rows[0].typed = { value: "other" };
      copied.browser.rows[0].outer_cancel_joined = 0;
      copied.browser.rows[0].current = false;
      const projected = box.exports.secretOwnerDiagnostic(copied);
      assert.equal(original.pw[0].failed_zero, true);
      assert.equal(projected.pw[0].failed_zero, false);
      assert.equal(projected.consumer[0].binding, false);
      assert.equal(projected.consumer[0].outer, false);
      assert.equal(projected.consumer[0].publication, false);
      assert.equal(projected.consumer[0].representation, false);
      assert.equal(projected.consumer[0].operation, "unknown");
      assert.equal(projected.reason, "unavailable");
      assert.equal(JSON.stringify(projected).includes(marker), false);
      assert.equal(box.exports.secretOriginalCompleted(copied), false);
      checks += 10;
    } else {
      const bad = [
        (r) => (r.rows[0].xid = id(900)),
        (r) => (r.browser.rows[0].reader_cancel_joined = 0),
        (r) => (r.browser.rows[0].release = 0),
        (r) => (r.browser.rows[0].outer_cancel_joined = 0),
        (r) => (r.browser.rows[0].current = false),
        (r) => (r.browser.rows[0].typed = { items: ["wrong"] }),
        (r) =>
          (r.browser.rows.find((v) => v.operation === "lookup").request_valid =
            false),
        (r) =>
          (r.browser.rows.find((v) => v.operation === "update").progress.phase =
            "confirmed"),
        (r) =>
          (r.browser.rows.find((v) => v.operation === "update").status = 200),
        (r) => (r.rows[0].finished_null = false),
        (r) => (r.first.ready = false),
      ];
      for (const mutate of bad) {
        const copy = structuredClone(result);
        mutate(copy);
        assert.equal(box.exports.secretOriginalCompleted(copy), false);
        checks++;
      }
    }
  }
  await observer.abort();
  closed = true;
  page.emit("close");
}
(async () => {
  const modes = process.argv.includes("--diagnostics-only")
    ? ["normal"]
    : [
        "normal",
        "held-reader",
        "held-pw",
        "request-failed",
        "identity-change",
        "leaked-body",
      ];
  for (const mode of modes) {
    await scenario(mode);
    console.log("PASS " + mode);
  }
  await tick();
  assert.equal(unhandled, 0);
  console.log(JSON.stringify({ modes: modes.length, checks, unhandled }));
})().catch(() => {
  console.error("SECRET_OBSERVER_CONTROL_FAILED");
  process.exitCode = 1;
});
