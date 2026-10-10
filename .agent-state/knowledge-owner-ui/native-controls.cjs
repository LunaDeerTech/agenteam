#!/usr/bin/env node
// Offline controls: real strict client and Session, controlled Fetch/streams.
// PW event rows alone are controlled here; no browser/backend claim is made.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const Module = require("node:module");
const root = path.resolve(__dirname, "../..");
const ts = require(path.join(root, "web/node_modules/typescript"));
const cache = new Map();
function source(file) {
  file = path.resolve(file);
  if (cache.has(file)) return cache.get(file).exports;
  const m = new Module(file);
  m.filename = file;
  m.paths = Module._nodeModulePaths(path.dirname(file));
  cache.set(file, m);
  const normal = m.require.bind(m);
  m.require = (name) => {
    if (name.startsWith(".")) {
      const candidate = path.resolve(path.dirname(file), name);
      if (fs.existsSync(candidate + ".ts")) return source(candidate + ".ts");
    }
    return normal(name);
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
const nativePath = path.join(
  root,
  "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",
);
const nativeCode = ts.transpileModule(fs.readFileSync(nativePath, "utf8"), {
  compilerOptions: {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
  },
}).outputText;
const { createAccountAPI } = source(path.join(root, "web/src/api/account.ts"));
const { createKnowledgeOwnerAPI } = source(
  path.join(root, "web/src/api/knowledge-owner.ts"),
);
const { createSessionController } = source(
  path.join(root, "web/src/composables/useSession.ts"),
);
const id = (n) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
const time = "2026-10-10T12:00:00.123456Z";
const project = id(10),
  target = id(20),
  user = id(1),
  requestID = id(99);
const document = {
  id: target,
  project_id: project,
  parent_document_id: null,
  title: "Text",
  content_version: "1",
  source_kind: "text",
  media_type: "text/plain",
  status: "active",
  indexing_status: "pending",
  created_by: { kind: "human", user_id: user },
  created_at: time,
  updated_at: time,
};
const body = {
  document,
  text: { text: "中🙂", next_byte_offset: "7", truncated: false },
};
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
const base = `/api/v1/projects/${project}/knowledge/documents/${target}/content`;
const json = (value) =>
  new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
const tick = () => new Promise((r) => setImmediate(r));
async function wait(predicate) {
  const deadline = Date.now() + 1000;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await tick();
  }
  throw Error(
    "CONTROL_NOT_SETTLED " +
      JSON.stringify({
        native: global.__knowledgeNative?.snapshot().pending,
        publication: global.__knowledgePublication?.snapshot().pending,
      }),
  );
}

global.document = { documentElement: { dataset: {} } };
let checks = 0,
  unhandled = 0;
process.on("unhandledRejection", () => {
  unhandled++;
});
(async () => {
  const methods = await import(
    "data:text/javascript;base64," + Buffer.from(nativeCode).toString("base64")
  );
  const dist = path.join(root, "output/ai/knowledge-owner-ui/dist-read-01");
  const binding = await methods.knowledgeSessionBinding(root, dist);
  assert(
    binding.asset.startsWith("/assets/") &&
      binding.entry.startsWith("/assets/"),
  );
  checks++;
  const copy = fs.mkdtempSync(
    path.join(require("node:os").tmpdir(), "ku-ast-"),
  );
  try {
    fs.mkdirSync(path.join(copy, "assets"));
    fs.copyFileSync(
      path.join(dist, "index.html"),
      path.join(copy, "index.html"),
    );
    for (const name of new Set([binding.asset, binding.entry]))
      fs.copyFileSync(
        path.join(dist, name.slice(1)),
        path.join(copy, name.slice(1)),
      );
    fs.copyFileSync(
      path.join(dist, binding.asset.slice(1)),
      path.join(copy, "assets/duplicate.js"),
    );
    await assert.rejects(
      methods.knowledgeSessionBinding(root, copy),
      /KNOWLEDGE_SINGLETON_UNIQUE/,
    );
    checks++;
  } finally {
    fs.rmSync(copy, { recursive: true, force: true });
  }
  for (const mode of [
    "normal",
    "reader-held",
    "outer-held",
    "void",
    "identity-change",
    "early-retirement",
  ]) {
    global.window = global;
    global.location = new URL("http://127.0.0.1:12345");
    const bytes = new TextEncoder().encode(JSON.stringify(body)),
      gate = deferred(),
      entered = deferred();
    let readerPromise,
      outerPromise,
      originalFetchPromise,
      readerReceiver = true,
      outerReceiver = true;
    global.fetch = function (input) {
      if (input === "/api/v1/session") return Promise.resolve(json(session));
      if (input === "/api/v1/auth/bootstrap")
        return Promise.resolve(
          json({
            csrf_token: "A".repeat(43),
            challenge_modes: ["rotate"],
            delivery_channel: "backend_log",
          }),
        );
      assert.equal(new URL(input, location).pathname, base);
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(bytes);
          controller.close();
        },
      });
      const get = stream.getReader.bind(stream),
        outer = stream.cancel;
      stream.getReader = function (...args) {
        const reader = get(...args),
          cancel = reader.cancel;
        reader.cancel = function (...args) {
          readerReceiver &&= this === reader;
          const p = Reflect.apply(cancel, this, args);
          readerPromise =
            mode === "reader-held"
              ? p.then(() => {
                  entered.resolve();
                  return gate.promise;
                })
              : p;
          return readerPromise;
        };
        return reader;
      };
      stream.cancel = function (...args) {
        outerReceiver &&= this === stream;
        const p = Reflect.apply(outer, this, args);
        outerPromise =
          mode === "outer-held"
            ? p.then(() => {
                entered.resolve();
                return gate.promise;
              })
            : p;
        return outerPromise;
      };
      originalFetchPromise = Promise.resolve(
        new Response(stream, {
          headers: {
            "Content-Type": "application/json",
            "Content-Length": String(bytes.length),
            "X-Request-ID": requestID,
          },
        }),
      );
      return originalFetchPromise;
    };
    const transport = (...args) => window.fetch(...args),
      args = [createAccountAPI(transport)];
    args[15] = createKnowledgeOwnerAPI(transport);
    const auth = createSessionController(...args);
    await auth.restore();
    assert.equal(auth.state.user.role, "user");
    const original = auth.knowledge.readContent;
    let originalPublicPromise;
    auth.knowledge.readContent = function (...args) {
      originalPublicPromise =
        mode === "void"
          ? Promise.resolve(undefined)
          : Reflect.apply(original, this, args);
      return originalPublicPromise;
    };
    global.__knowledgeControlAuth = auth;
    const asset =
      "data:text/javascript,export%20function%20singleton(){return%20globalThis.__knowledgeControlAuth}";
    const config = {
      project,
      documents: [target],
      expiresAt: Date.now() + 5000,
    };
    methods.installKnowledgeNative(config);
    await methods.installKnowledgePublication({
      ...config,
      user,
      binding: { entry: "unused", asset, exportName: "singleton" },
    });
    const promise = auth.knowledge.readContent(project, target, {
      byte_offset: "0",
      max_bytes: 65536,
    });
    assert.equal(promise, originalPublicPromise);
    checks++;
    if (mode === "reader-held" || mode === "outer-held") {
      await entered.promise;
      assert.equal(auth.state.busy, true);
      assert.equal(__knowledgePublication.snapshot().rows[0].fulfilled, false);
      assert(__knowledgeNative.snapshot().pending > 0);
      checks += 3;
      gate.resolve();
    }
    if (mode === "early-retirement") {
      const early = __knowledgeNative.finish();
      await promise;
      await early;
    } else await promise;
    if (mode === "identity-change") {
      auth.leave();
      await tick();
    }
    await wait(
      () =>
        __knowledgeNative.snapshot().pending === 0 &&
        __knowledgePublication.snapshot().pending === 0,
    );
    const n = await __knowledgeNative.finish(),
      p = await __knowledgePublication.finish();
    assert.equal(readerReceiver, true);
    assert.equal(outerReceiver, true);
    checks += 2;
    const row = n.rows[0];
    const pw = {
      request_id: requestID,
      method: "GET",
      path: base,
      query: "byte_offset=0&max_bytes=65536",
      status: 200,
      finished: true,
      finished_null: true,
      failed: false,
    };
    const side = {
      ...pw,
      content_length: bytes.length,
      body_sha256: require("node:crypto")
        .createHash("sha256")
        .update(bytes)
        .digest("hex"),
    };
    const complete = methods.knowledgeOriginalCompleted(n, p, pw, side);
    assert.equal(
      complete,
      ["normal", "reader-held", "outer-held"].includes(mode),
      mode,
    );
    checks++;
    if (mode === "normal") {
      assert.deepEqual(p.rows[0].typed, body);
      assert.equal(row.bytes, bytes.length);
      checks += 2;
      for (const mutate of [
        (r) => (r[0].rows[0].reader_cancel_joined = 0),
        (r) => (r[0].rows[0].outer_cancel_joined = 0),
        (r) => (r[0].pending_at_retirement = 1),
        (r) => (r[0].reason = "expired"),
        (r) => (r[0].rows[0].digest = "0".repeat(64)),
        (r) => r[0].rows.push({ ...r[0].rows[0] }),
        (r) => (r[1].not_busy = false),
        (r) => (r[1].rows[0].typed = null),
        (r) => (r[1].rows[0].bound = 2),
        (r) => (r[2].request_id = id(98)),
        (r) => (r[2].finished_null = false),
        (r) => (r[2].failed = true),
        (r) => (r[3].query = "byte_offset=7&max_bytes=65536"),
        (r) => r[3].content_length--,
      ]) {
        const values = structuredClone([n, p, pw, side]);
        mutate(values);
        assert.equal(methods.knowledgeOriginalCompleted(...values), false);
        checks++;
      }
    }
    assert.equal(__knowledgeNative.snapshot().pending, 0);
    assert.equal(__knowledgePublication.snapshot().pending, 0);
    checks += 2;
    auth.leave();
    delete global.__knowledgeNative;
    delete global.__knowledgePublication;
    delete global.__knowledgeControlAuth;
  }
  // Directly distinguish identity-preserving wrappers from async replacements.
  global.__knowledgePublication = {
    bind() {
      return 1;
    },
  };
  const stream = new ReadableStream({
    start(c) {
      c.enqueue(new Uint8Array([65]));
      c.close();
    },
  });
  const get = stream.getReader.bind(stream),
    cancelOuter = stream.cancel;
  let actualReader, readPromise, cancelPromise, outerPromise;
  stream.getReader = function (...args) {
    const reader = get(...args),
      read = reader.read,
      cancel = reader.cancel;
    actualReader = reader;
    reader.read = function (...args) {
      assert.equal(this, reader);
      return (readPromise = Reflect.apply(read, this, args));
    };
    reader.cancel = function (...args) {
      assert.equal(this, reader);
      return (cancelPromise = Reflect.apply(cancel, this, args));
    };
    return reader;
  };
  stream.cancel = function (...args) {
    assert.equal(this, stream);
    return (outerPromise = Reflect.apply(cancelOuter, this, args));
  };
  const originalPromise = Promise.resolve(
    new Response(stream, {
      headers: {
        "Content-Type": "application/json",
        "Content-Length": "1",
        "X-Request-ID": requestID,
      },
    }),
  );
  global.fetch = function () {
    assert.equal(this, global);
    return originalPromise;
  };
  methods.installKnowledgeNative({
    project,
    documents: [target],
    expiresAt: Date.now() + 5000,
  });
  const returned = window.fetch(base + "?byte_offset=0&max_bytes=65536");
  assert.equal(returned, originalPromise);
  checks++;
  const response = await returned,
    reader = response.body.getReader();
  assert.equal(reader, actualReader);
  checks++;
  let read = reader.read();
  assert.equal(read, readPromise);
  await read;
  checks++;
  read = reader.read();
  assert.equal(read, readPromise);
  await read;
  checks++;
  const cancelled = reader.cancel();
  assert.equal(cancelled, cancelPromise);
  await cancelled;
  checks++;
  reader.releaseLock();
  const outer = response.body.cancel();
  assert.equal(outer, outerPromise);
  await outer;
  checks++;
  await wait(() => __knowledgeNative.snapshot().pending === 0);
  const last = await __knowledgeNative.finish();
  assert.equal(last.rows[0].release, 1);
  checks++;
  delete global.__knowledgeNative;
  delete global.__knowledgePublication;
  // Exercise the actual Node orchestration, not constructed proof rows. The
  // controlled Page runs the two actual browser programs; its module loader
  // maps the already-checked singleton export to this one real Session.
  const { EventEmitter } = require("node:events");
  const secretMarker = "PRIVATE_DIAGNOSTIC_SENTINEL";
  for (const mode of [
    "normal-node",
    "held-header",
    "held-finished",
    "finished-without-event",
    "page-close",
    "context-close",
    "evaluate-reject",
    "header-reject",
    "finished-error",
    "invalid-metadata",
  ]) {
    const evidence = fs.mkdtempSync(
      path.join(require("node:os").tmpdir(), "ku-node-"),
    );
    const diagnostic = methods.createKnowledgeReadDiagnostic({
      evidence,
      inputHash: "d".repeat(64),
      project,
      documents: [target],
    });
    const page = new EventEmitter(),
      context = new EventEmitter();
    let pageClosed = false,
      rejectEvaluate = false,
      headerCalls = 0,
      finishedCalls = 0;
    const headerGate = deferred(),
      finishedGate = deferred();
    const request = {
      url: () =>
        location.origin +
        base +
        "?" +
        (mode === "invalid-metadata"
          ? "token=" + secretMarker
          : "byte_offset=0&max_bytes=65536"),
      method: () => "GET",
      headers: () => {
        throw Error("must never inspect request headers");
      },
    };
    const pwResponse = {
      request: () => request,
      status: () => 200,
      headerValue(name) {
        assert.equal(this, pwResponse);
        assert.equal(name, "x-request-id");
        headerCalls++;
        if (mode === "header-reject")
          return Promise.reject(Error(secretMarker));
        return mode === "held-header"
          ? headerGate.promise
          : Promise.resolve(
              mode === "invalid-metadata" ? secretMarker : requestID,
            );
      },
      finished() {
        assert.equal(this, pwResponse);
        finishedCalls++;
        return ["held-finished", "page-close", "context-close"].includes(mode)
          ? finishedGate.promise
          : Promise.resolve(
              mode === "finished-error" ? Error(secretMarker) : null,
            );
      },
      body: () => {
        throw Error("must never read a PW body");
      },
    };
    const bytes = new TextEncoder().encode(JSON.stringify(body));
    global.fetch = function (input) {
      if (input === "/api/v1/session") return Promise.resolve(json(session));
      if (input === "/api/v1/auth/bootstrap")
        return Promise.resolve(
          json({
            csrf_token: "A".repeat(43),
            challenge_modes: ["rotate"],
            delivery_channel: "backend_log",
          }),
        );
      assert.equal(new URL(input, location).pathname, base);
      page.emit("request", request);
      queueMicrotask(() => {
        page.emit("response", pwResponse);
        if (mode !== "finished-without-event")
          page.emit("requestfinished", request);
      });
      return Promise.resolve(
        new Response(bytes, {
          headers: {
            "Content-Type": "application/json",
            "Content-Length": String(bytes.length),
            "X-Request-ID": requestID,
          },
        }),
      );
    };
    const args = [createAccountAPI((...args) => window.fetch(...args))];
    args[15] = createKnowledgeOwnerAPI((...args) => window.fetch(...args));
    const auth = createSessionController(...args);
    await auth.restore();
    global.__knowledgeNodeAuth = auth;
    page.context = () => context;
    page.isClosed = () => pageClosed;
    page.evaluate = async (fn, value) => {
      if (pageClosed || rejectEvaluate) throw Error(secretMarker);
      if (fn === methods.installKnowledgePublication) {
        assert.deepEqual(value.binding, binding);
        return fn({
          ...value,
          binding: {
            entry: binding.entry,
            exportName: "singleton",
            asset:
              "data:text/javascript,export%20function%20singleton(){return%20globalThis.__knowledgeNodeAuth}",
          },
        });
      }
      return fn(value);
    };
    let observer, report;
    try {
      observer = await methods.observeKnowledge(page, {
        root,
        dist,
        project,
        documents: [target],
        user,
        evidence,
        inputHash: "d".repeat(64),
        diagnostic,
      });
      const typed = await auth.knowledge.readContent(project, target, {
        byte_offset: "0",
        max_bytes: 65536,
      });
      assert.deepEqual(typed, body);
      checks++;
      await wait(
        () =>
          __knowledgeNative.snapshot().pending === 0 &&
          __knowledgePublication.snapshot().pending === 0,
      );
      diagnostic.enter("observer-idle");
      assert.equal(await observer.idle(), true);
      checks++;
      let sample = diagnostic.snapshot().last_observers;
      assert.equal(sample.observed_in_phase, "observer-idle");
      assert.equal(sample.current_at_failure, "not_observed");
      assert.equal(sample.value.native.pending, 0);
      assert.equal(sample.value.publication.pending, 0);
      assert.equal(JSON.stringify(sample).includes('"typed"'), false);
      checks += 5;
      diagnostic.enter("observer-finish");
      if (mode === "evaluate-reject") rejectEvaluate = true;
      let settled = false;
      const finishing = observer.finish().then(
        (value) => {
          settled = true;
          return { value };
        },
        () => {
          settled = true;
          return { rejected: true };
        },
      );
      if (
        [
          "held-header",
          "held-finished",
          "page-close",
          "context-close",
        ].includes(mode)
      ) {
        await tick();
        assert.equal(settled, false);
        const before = diagnostic.snapshot();
        assert.equal(before.phase, "pw-tails");
        assert.equal(before.pw_pending, 1);
        assert.equal(
          before.pw_rows[0].header,
          mode === "held-header" ? "entered" : "returned",
        );
        assert.equal(
          before.pw_rows[0].finished_await,
          mode === "held-header" ? "not_entered" : "entered",
        );
        assert.equal(finishedCalls, mode === "held-header" ? 0 : 1);
        checks += 6;
        diagnostic.firstFailure(false, false);
        const first = diagnostic.snapshot().first_failure;
        assert.deepEqual(
          JSON.parse(
            fs.readFileSync(
              path.join(evidence, "knowledge-first-failure.json"),
              "utf8",
            ),
          ).first_failure,
          first,
        );
        checks++;
        if (mode === "page-close") {
          pageClosed = true;
          page.emit("close");
        }
        if (mode === "context-close") {
          context.emit("close");
          pageClosed = true;
        }
        headerGate.resolve(requestID);
        finishedGate.resolve(null);
        const outcome = await finishing;
        report = outcome.value;
        diagnostic.event(
          outcome.rejected ? "cleanup-reject" : "cleanup-return",
        );
        diagnostic.firstFailure(true, !!report);
        const after = diagnostic.snapshot();
        assert.deepEqual(after.first_failure, first);
        assert.equal(after.first_failure.page_closed, false);
        assert(after.tail_events.length > 0);
        assert.equal(after.pw_pending, 0);
        assert.equal(diagnostic.healthy(), false);
        if (mode.endsWith("close")) assert.equal(outcome.rejected, true);
        else assert.equal(report.pw_failed, true);
        checks += 6;
      } else {
        const outcome = await finishing;
        report = outcome.value;
        if (mode === "evaluate-reject") {
          assert.equal(outcome.rejected, true);
          diagnostic.firstFailure(false, false);
          assert.equal(
            diagnostic.snapshot().first_failure.phase,
            "page-observers",
          );
          assert.equal(
            diagnostic.snapshot().first_failure.last_observers
              .current_at_failure,
            "not_observed",
          );
          checks += 3;
        } else {
          const side = {
            method: "GET",
            request_id: requestID,
            path: base,
            query: "byte_offset=0&max_bytes=65536",
            status: 200,
            content_length: bytes.length,
            body_sha256: require("node:crypto")
              .createHash("sha256")
              .update(bytes)
              .digest("hex"),
          };
          assert.equal(
            methods.knowledgeOriginalCompleted(
              report.native,
              report.publication,
              report.requests[0],
              side,
            ),
            mode === "normal-node",
          );
          if (mode === "normal-node") {
            assert.equal(report.pw_failed, false);
            assert.equal(report.pw_pending, 0);
            assert(diagnostic.healthy());
            assert.equal(report.native.reason, "explicit");
            assert.equal(report.publication.reason, "explicit");
            checks += 5;
          }
          checks++;
        }
      }
      assert.equal(headerCalls, 1);
      assert.equal(finishedCalls, mode === "header-reject" ? 0 : 1);
      checks += 2;
      const safe = fs.readFileSync(
        path.join(evidence, "knowledge-read-diagnostic.json"),
        "utf8",
      );
      assert.equal(safe.includes(secretMarker), false);
      assert.equal(safe.includes('"typed"'), false);
      checks += 2;
    } finally {
      headerGate.resolve(requestID);
      finishedGate.resolve(null);
      // Controlled Page closing rejects evaluate, so its own fixture releases
      // the actual installed browser programs directly; never a success proof.
      if (global.__knowledgeNative) await __knowledgeNative.finish();
      if (global.__knowledgePublication) await __knowledgePublication.finish();
      auth.leave();
      page.removeAllListeners();
      context.removeAllListeners();
      delete global.__knowledgeNative;
      delete global.__knowledgePublication;
      delete global.__knowledgeNodeAuth;
      fs.rmSync(evidence, { recursive: true, force: true });
    }
  }
  const diagnosticDirectory = fs.mkdtempSync(
    path.join(require("node:os").tmpdir(), "ku-diagnostic-"),
  );
  try {
    const d = methods.createKnowledgeReadDiagnostic({
      evidence: diagnosticDirectory,
      inputHash: "d".repeat(64),
      project,
      documents: [target],
    });
    d.enter("completion-button");
    d.firstFailure(false, false);
    const first = d.snapshot().first_failure;
    const mutation = d.snapshot();
    mutation.first_failure.phase = "forged";
    for (let i = 0; i < 260; i++) d.event("cleanup-return");
    const end = d.snapshot();
    assert.deepEqual(end.first_failure, first);
    assert(end.overflow);
    assert.equal(end.events.length + end.tail_events.length, 256);
    assert.equal(d.healthy(), false);
    checks += 4;
    const file = path.join(diagnosticDirectory, "not-a-directory");
    fs.writeFileSync(file, "owned");
    const blocked = methods.createKnowledgeReadDiagnostic({
      evidence: file,
      inputHash: "d".repeat(64),
      project,
      documents: [target],
    });
    assert.doesNotThrow(() => {
      blocked.enter("pw-tails");
      blocked.firstFailure(false, false);
      blocked.event("cleanup-return");
    });
    assert(blocked.snapshot().write_failed);
    assert.equal(blocked.healthy(), false);
    checks += 3;
  } finally {
    fs.rmSync(diagnosticDirectory, { recursive: true, force: true });
  }
  await tick();
  assert.equal(unhandled, 0);
  console.log(
    `Knowledge native controls PASS checks=${checks} unhandled=${unhandled}; real client/Session and Node observeKnowledge, browser/PW events controlled`,
  );
})().catch((error) => {
  console.error("CONTROL_FAIL", error.name, error.message);
  process.exitCode = 1;
});
