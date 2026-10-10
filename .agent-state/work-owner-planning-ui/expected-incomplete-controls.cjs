const fs = require("fs"),
  vm = require("vm"),
  assert = require("node:assert/strict");
const ts = require(require("path").resolve("web/node_modules/typescript"));
const actual = fs.readFileSync(
  "tests/account-captcha-web/e2e/project-work-planning.helpers.ts",
  "utf8",
);
const tree = ts.createSourceFile(
  "helpers.ts",
  actual,
  ts.ScriptTarget.Latest,
  true,
);
const ledgerFunction = tree.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "workIncompleteLedger",
);
assert(ledgerFunction);
const code = ts.transpileModule(ledgerFunction.getText(tree), {
  compilerOptions: {
    target: ts.ScriptTarget.ES2024,
    module: ts.ModuleKind.CommonJS,
  },
}).outputText;
const project = "01900000-0000-7000-8000-000000000001",
  target = "01900000-0000-7000-8000-000000000002",
  key = "01900000-0000-7000-8000-000000000003";
function ledger(...owners) {
  const c = {
    URL,
    performance,
    Promise,
    Error,
    Set,
    Object,
    uuid7:
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  };
  vm.createContext(c);
  vm.runInContext(code + ";this.make=workIncompleteLedger;", c);
  return c.make(...owners);
}
const spec = (kind = "lost-milestone-update") => ({
  kind,
  projectID: project,
  targetID: target,
  ...(kind === "canceled-task-read"
    ? {}
    : { expectedVersion: "7", text: "specific original text" }),
});
function req(kind = "lost-milestone-update", changes = {}) {
  const entity = [
      "lost-milestone-update",
      "unforwarded-milestone-update",
    ].includes(kind)
      ? "milestones"
      : "tasks",
    isCancel = kind === "canceled-task-read",
    isBlocker = kind === "lost-blocker-add";
  let body = isCancel
    ? null
    : {
        expected_version: "7",
        request: isBlocker
          ? {
              blocker_id: key,
              description: "specific original text",
              type: "waiting_for_human",
              metadata: {},
            }
          : { title: "specific original text" },
      };
  const values = {
    url: `http://127.0.0.1:1/api/v1/projects/${project}/${entity}/${target}${isBlocker ? "/blockers" : ""}`,
    method: isCancel ? "GET" : isBlocker ? "POST" : "PATCH",
    body,
    key: require("node:crypto").randomUUID(),
    ...changes,
  };
  return {
    url: () => values.url,
    method: () => values.method,
    headers: () => ({
      "idempotency-key": values.key,
      "x-csrf-token": changes.csrf ?? "controlled-csrf",
      origin: changes.origin ?? new URL(values.url).origin,
    }),
    allHeaders: async () => ({
      "idempotency-key": values.key,
      "x-csrf-token": changes.csrf ?? "controlled-csrf",
      origin: changes.origin ?? new URL(values.url).origin,
    }),
    postData: () =>
      changes.rawBody ??
      (values.body === null ? null : JSON.stringify(values.body)),
    postDataJSON: () => values.body,
  };
}
// Use the locked Playwright methods themselves; only the original channel is
// controlled. This opens no browser, socket, HTTP server or substitute request.
const PWRequest = require(
  require("path").resolve(
    "tests/account-captcha-web/node_modules/playwright-core/lib/client/network.js",
  ),
).Request;
const drainHeaders = () => new Promise((resolve) => setImmediate(resolve));
function rawRequest(kind, changes = {}, options = {}) {
  const request = req(kind, changes);
  const complete = request.headers();
  const provisional = { ...complete, ...options.provisional };
  if (options.omitOrigin) delete provisional.origin;
  let release,
    reject,
    calls = 0;
  const channel = new Promise((yes, no) => {
    release = yes;
    reject = no;
  });
  Object.assign(request, {
    _fallbackOverrides: {},
    _provisionalHeaders: { headers: () => provisional },
    _wrapApiCall: (fn) => fn(),
    _channel: {
      rawRequestHeaders: () => {
        calls++;
        options.onCall?.();
        return channel;
      },
    },
    _actualHeaders: PWRequest.prototype._actualHeaders,
    headers: PWRequest.prototype.headers,
    allHeaders: PWRequest.prototype.allHeaders,
  });
  return {
    request,
    calls: () => calls,
    release(overrides = {}) {
      release({
        headers: Object.entries({ ...complete, ...overrides }).map(
          ([name, value]) => ({ name, value }),
        ),
      });
    },
    reject() {
      reject(Error("private-header-rejection-canary"));
    },
  };
}
let unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
(async () => {
  const controls = [];
  async function check(name, fn) {
    if (
      process.env.WORK_PLANNING_ONLY === "1" &&
      !/^(planning |default originalBody)/.test(name)
    )
      return;
    await fn();
    controls.push(name);
  }
  await check(
    "actual crypto.randomUUID intent keys bind every declared mutation",
    () => {
      for (const kind of [
        "unforwarded-milestone-update",
        "lost-milestone-update",
        "lost-task-update",
        "lost-blocker-add",
      ]) {
        const l = ledger(),
          originalKey = require("node:crypto").randomUUID(),
          q = req(kind, { key: originalKey });
        assert.equal(originalKey[14], "4");
        l.declare(spec(kind));
        l.request(q);
        l.failed(q);
        assert.equal(l.expectedFailure(q), true, kind + " must bind v4 key");
      }
    },
  );
  await check(
    "Foundation intent key bounds stay distinct from resource IDs",
    () => {
      for (const value of ["a", "A._:/-09", "x".repeat(128)]) {
        const l = ledger(),
          q = req(undefined, { key: value });
        l.declare(spec());
        l.request(q);
        l.failed(q);
        assert.equal(l.verify(1, new Set([q])), true);
      }
      for (const value of [
        "",
        "x".repeat(129),
        "invalid key",
        "a\n",
        "é",
        "%",
      ]) {
        const l = ledger(),
          q = req(undefined, { key: value });
        l.declare(spec());
        l.request(q);
        l.failed(q);
        assert.equal(l.verify(1, new Set([q])), false);
      }
      const resourceV4 = require("node:crypto").randomUUID();
      for (const field of ["projectID", "targetID"]) {
        const l = ledger();
        assert.throws(() => l.declare({ ...spec(), [field]: resourceV4 }));
      }
      const l = ledger(),
        q = req("lost-blocker-add", {
          body: {
            expected_version: "7",
            request: {
              blocker_id: resourceV4,
              description: "specific original text",
              type: "waiting_for_human",
              metadata: {},
            },
          },
        });
      l.declare(spec("lost-blocker-add"));
      l.request(q);
      l.failed(q);
      assert.equal(l.verify(1, new Set([q])), false);
    },
  );
  for (const kind of [
    "lost-milestone-update",
    "lost-task-update",
    "lost-blocker-add",
  ])
    await check(
      kind + " same original failed request releases only declaration",
      async () => {
        const l = ledger(),
          s = spec(kind);
        l.declare(s);
        const q = req(kind);
        l.request(q);
        const pending = new Promise(() => {});
        const terminal = l.terminal(q, pending);
        l.failed(q);
        assert.equal((await terminal).message, "WORK_DECLARED_INCOMPLETE");
        assert.equal(l.verify(1, new Set([q])), true);
        const replay = req(kind);
        l.request(replay);
        assert.equal(l.expectedFailure(replay), false);
        const untouched = Promise.resolve(null);
        assert.equal(l.terminal(replay, untouched), untouched);
      },
    );
  await check(
    "held cancel requires authorization before same Request failure",
    async () => {
      const l = ledger(),
        h = l.declare(spec("canceled-task-read")),
        q = req("canceled-task-read");
      l.request(q);
      h.authorizeCancellation();
      l.failed(q);
      assert.equal(l.verify(1, new Set([q])), true);
    },
  );
  for (const mutation of [
    {
      body: {
        expected_version: "8",
        request: { title: "specific original text" },
      },
    },
    { body: { expected_version: "7", request: { title: "changed meaning" } } },
    {
      body: {
        expected_version: "7",
        request: { title: "specific original text", description: "extra" },
      },
    },
    { key: "invalid key" },
    { method: "POST" },
    {
      url: `http://127.0.0.1:1/api/v1/projects/${target}/milestones/${target}`,
    },
  ])
    await check(
      "wrong request does not consume declaration " + JSON.stringify(mutation),
      () => {
        const l = ledger();
        l.declare(spec());
        const q = req(undefined, mutation);
        l.request(q);
        l.failed(q);
        assert.equal(l.verify(1, new Set([q])), false);
      },
    );
  await check(
    "same URL different Request identity cannot fulfill expectation",
    () => {
      const l = ledger();
      l.declare(spec());
      const q = req(),
        other = req();
      l.request(q);
      l.failed(other);
      assert.equal(l.verify(1, new Set([other])), false);
    },
  );
  await check("duplicate matching in-flight request is rejected", () => {
    const l = ledger();
    l.declare(spec());
    const q = req();
    l.request(q);
    l.request(req());
    l.failed(q);
    assert.equal(l.verify(1, new Set([q])), false);
  });
  await check("failed before real cancel authorization stays invalid", () => {
    const l = ledger();
    l.declare(spec("canceled-task-read"));
    const q = req("canceled-task-read");
    l.request(q);
    l.failed(q);
    assert.equal(l.verify(1, new Set([q])), false);
  });
  await check(
    "page close cannot turn a pending request into expected incomplete",
    () => {
      const l = ledger();
      l.declare(spec());
      const q = req();
      l.request(q);
      l.close();
      l.failed(q);
      assert.equal(l.verify(1, new Set([q])), false);
    },
  );
  await check("missing terminal and unrelated failed request reject", () => {
    const l = ledger();
    l.declare(spec());
    const q = req();
    l.request(q);
    assert.equal(l.verify(1, new Set()), false);
    l.failed(q);
    assert.equal(l.verify(1, new Set([q, req()])), false);
  });
  await check(
    "ordinary original finished Promise identity and errors unchanged",
    async () => {
      const l = ledger(),
        q = req(),
        ok = Promise.resolve(null),
        error = Promise.resolve(new Error("original"));
      assert.equal(l.terminal(q, ok), ok);
      assert.equal(l.terminal(q, error), error);
      assert.equal((await error).message, "original");
      assert.equal(l.verify(0, new Set()), true);
    },
  );
  await check(
    "late original finished rejection has rejection sink",
    async () => {
      const l = ledger();
      l.declare(spec());
      const q = req();
      l.request(q);
      let reject;
      const late = new Promise((_, r) => (reject = r));
      const terminal = l.terminal(q, late);
      l.failed(q);
      await terminal;
      reject(Error("late"));
      await new Promise((r) => setImmediate(r));
      assert.equal(unhandled, 0);
    },
  );
  await check("unfinished or fifth declarations rejected", () => {
    const l = ledger();
    l.declare(spec());
    assert.throws(() => l.declare(spec()));
    for (let i = 0; i < 4; i++) {
      if (i) l.declare(spec());
      const q = req();
      l.request(q);
      l.failed(q);
    }
    assert.throws(() => l.declare(spec()));
    assert.equal(l.verify(3, new Set()), false);
  });
  function observerEnv(options = {}) {
    const callbacks = {},
      files = new Map(),
      metaNames = [],
      writes = [],
      decoded = [];
    const hash = (raw) =>
      require("crypto").createHash("sha256").update(raw).digest("hex");
    let counter = 0,
      clock = 0;
    const c = {
      URL,
      Map,
      Set,
      WeakMap,
      Promise,
      Error,
      Object,
      Array,
      performance: { now: () => ++clock },
      uuid7:
        /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      workBodyAwaits: new WeakMap(),
      failureSnapshots: [],
      failureTailEvents: [],
      failureFirstSnapshot: null,
      failureTailOverflow: false,
      console,
      workDiagnosticTarget: () => ({}),
      safeWorkFailure: () => options.reason ?? "controlled-failure",
      evidence: "/owned",
      repository: "/repo",
      schemaProgram: "unchanged-schema",
      join: require("path").join,
      expect: (value) => ({
        toBe: (want) => assert.equal(value, want),
        toBeNull: () => assert.equal(value, null),
      }),
      spawnSync: () => ({
        status: options.schemaStatus ?? 0,
        stdout: String(metaNames.length),
        stderr: "",
        signal: null,
      }),
      writeFileSync: (name, data) =>
        writes.push({ name, body: JSON.parse(data) }),
      readdirSync: () => metaNames,
      readFileSync: (name, encoding) => {
        assert(files.has(name));
        const raw = files.get(name);
        return encoding ? raw.toString() : raw;
      },
      createHash: require("crypto").createHash,
      decodeOriginal: async (fact, meta, raw) => {
        options.duringDecode?.();
        if (options.decodeReject)
          throw Error("controlled strict client rejection");
        assert.equal(meta.endpoint, fact.url.pathname);
        assert.equal(meta.method, fact.request.method());
        assert.equal(hash(raw), meta.body_sha256);
        decoded.push(fact.request);
      },
    };
    vm.createContext(c);
    const functions = tree.statements
      .filter(
        (n) =>
          ts.isFunctionDeclaration(n) &&
          [
            "workIncompleteLedger",
            "workPlanningReorders",
            "capturedOriginalBody",
            "originalBody",
            "markWorkBodyAwait",
            "recordWorkFailureTail",
            "saveWorkFailureObservations",
            "workOrdinaryCompletionEvents",
            "observe",
          ].includes(n.name?.text),
      )
      .map((n) => n.getText(tree).replace(/^export /, ""));
    vm.runInContext(
      ts.transpileModule(functions.join("\n") + "\nthis.make=observe;", {
        compilerOptions: {
          target: ts.ScriptTarget.ES2024,
          module: ts.ModuleKind.CommonJS,
        },
      }).outputText,
      c,
    );
    const context = new (require("node:events").EventEmitter)();
    const page = {
      context: () => context,
      on: (name, fn) => {
        (callbacks[name] ??= []).push(fn);
      },
    };
    const observed = c.make(page, {
        ordinaryCompletion: options.ordinaryCompletion,
        planningReorders: options.planningReorders,
      }),
      emit = (name, item) => {
        for (const fn of callbacks[name] ?? []) fn(item);
      };
    function response(request, finished) {
      const id =
          "01900000-0000-7000-8000-" + String(++counter + 10).padStart(12, "0"),
        raw = Buffer.from('{"safe":"same original upstream bytes"}'),
        bodyFile = "body-" + hash(raw) + ".json";
      const meta = {
        request_id: id,
        endpoint: new URL(request.url()).pathname,
        method: request.method(),
        status: 200,
        body_file: bodyFile,
        body_sha256: hash(raw),
      };
      const name = "response-" + counter + ".json";
      metaNames.push(name);
      files.set("/owned/" + name, Buffer.from(JSON.stringify(meta)));
      files.set("/owned/" + bodyFile, raw);
      let calls = 0;
      const r = {
        url: request.url,
        request: () => request,
        status: () => 200,
        headerValue: async () => id,
        finished: () => {
          calls++;
          return finished;
        },
      };
      emit("response", r);
      return { calls: () => calls, original: r };
    }
    return {
      observed,
      originalBody: c.originalBody,
      saveFailure: (status = "timedOut") =>
        c.saveWorkFailureObservations(status),
      context,
      emit,
      response,
      ownedResponse(request, finished, changes = {}) {
        let calls = 0;
        const headers = {
          "content-type": "application/problem+json",
          "content-length": "4096",
          connection: "close",
          ...(changes.headers ?? {}),
        };
        const r = {
          url: request.url,
          request: () => request,
          status: () => changes.status ?? 503,
          headerValue: async () => headers["x-request-id"] ?? null,
          allHeaders: async () =>
            changes.headersPromise ? await changes.headersPromise : headers,
          finished: () => {
            calls++;
            return finished;
          },
        };
        emit("response", r);
        return { calls: () => calls };
      },
      writes,
      decoded,
      snapshot: () => c.failureSnapshots[0](),
    };
  }
  function ownedCase(kind = "unforwarded-milestone-update") {
    const e = observerEnv(),
      ordinary = req();
    e.emit("request", ordinary);
    e.response(ordinary, Promise.resolve(null));
    e.observed.declareIncomplete(spec(kind));
    const q = req(kind);
    e.emit("request", q);
    return { e, q, ordinary };
  }
  for (const kind of [
    "unforwarded-milestone-update",
    "lost-milestone-update",
    "lost-task-update",
    "lost-blocker-add",
  ])
    await check(
      "locked PW failed event leaves finished pending; declared observer never starts that operation: " +
        kind,
      async () => {
        const root = require("path").resolve(
          "tests/account-captcha-web/node_modules/playwright-core",
        );
        assert.equal(require(root + "/package.json").version, "1.56.1");
        const { BrowserContext } = require(
          root + "/lib/client/browserContext.js",
        );
        const { Response } = require(root + "/lib/client/network.js");
        const { ManualPromise, LongStandingScope } = require(
          root + "/lib/utils/isomorphic/manualPromise.js",
        );
        const { e, q } = ownedCase(kind);
        const closed = new LongStandingScope();
        q._targetClosedScope = () => closed;
        q._setResponseEndTiming = () => {};
        const native = {
          request: () => q,
          _finishedPromise: new ManualPromise(),
        };
        const original = Response.prototype.finished.call(native);
        const joined = original.then(
          () => "finished",
          () => "closed",
        );
        const response =
          kind === "unforwarded-milestone-update"
            ? e.ownedResponse(q, original)
            : e.response(q, original);
        try {
          await new Promise((r) => setImmediate(r));
          BrowserContext.prototype._onRequestFailed.call(
            { emit() {} },
            q,
            1,
            "net::ERR_CONTENT_LENGTH_MISMATCH",
            { emit: e.emit },
          );
          assert.equal(
            await Promise.race([
              joined,
              new Promise((r) => setImmediate(() => r("pending"))),
            ]),
            "pending",
          );
          await e.observed.verify(1);
          assert.equal(response.calls(), 0);
          assert.equal(e.snapshot().requests[1].response_finished_at, null);
        } finally {
          closed.close(Error("test page closed"));
          assert.equal(await joined, "closed");
        }
      },
    );
  await check(
    "actual observer: exact unforwarded 503 is separate from upstream schema and waits for real same-request failed",
    async () => {
      const { e, q, ordinary } = ownedCase();
      const r = e.ownedResponse(q, new Promise(() => {}));
      await new Promise((r) => setImmediate(r));
      const pending = e.observed.verify(1);
      assert.equal(
        await Promise.race([
          pending.then(() => true),
          new Promise((r) => setImmediate(() => r(false))),
        ]),
        false,
      );
      e.emit("requestfailed", q);
      await pending;
      assert.equal(r.calls(), 0);
      assert.deepEqual(e.decoded, [ordinary]);
      const result = e.writes.at(-1).body;
      assert.equal(result.original_bodies, 1);
      assert.equal(result.owned_unforwarded_truncations, 1);
      assert.equal(result.failed_without_headers, 0);
      assert.equal(result.expected_incomplete, 0);
      const diagnostic = e.snapshot().requests[1];
      assert.equal(diagnostic.request_id, null);
      assert.equal(diagnostic.response_finished_at, null);
      assert.notEqual(diagnostic.request_failed_at, null);
    },
  );
  for (const kind of [
    "unforwarded-milestone-update",
    "lost-milestone-update",
    "lost-task-update",
    "lost-blocker-add",
  ]) {
    await check(
      "actual observer: successful event then failed is rejected: " + kind,
      async () => {
        const { e, q } = ownedCase(kind);
        const r =
          kind === "unforwarded-milestone-update"
            ? e.ownedResponse(q, Promise.resolve(null))
            : e.response(q, Promise.resolve(null));
        e.emit("requestfinished", q);
        e.emit("requestfailed", q);
        await assert.rejects(e.observed.verify(1));
        assert.equal(r.calls(), 0);
      },
    );
    await check(
      "actual observer: close settles missing-failed observation as rejected: " +
        kind,
      async () => {
        const { e, q } = ownedCase(kind);
        const r =
          kind === "unforwarded-milestone-update"
            ? e.ownedResponse(q, new Promise(() => {}))
            : e.response(q, new Promise(() => {}));
        const pending = e.observed.verify(1);
        const joined = pending.then(
          () => "accepted",
          () => "rejected",
        );
        await new Promise((resolve) => setImmediate(resolve));
        assert.equal(
          await Promise.race([
            joined,
            new Promise((resolve) => setImmediate(() => resolve("pending"))),
          ]),
          "pending",
        );
        e.emit("close");
        assert.equal(await joined, "rejected");
        e.emit("requestfailed", q);
        await assert.rejects(e.observed.verify(1));
        assert.equal(r.calls(), 0);
      },
    );
  }
  await check(
    "actual observer: v4 unforwarded key retires its slot before the next declared loss",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, new Promise(() => {}));
      e.emit("requestfailed", q);
      await new Promise((r) => setImmediate(r));
      e.observed.declareIncomplete(spec("lost-milestone-update"));
      const next = req("lost-milestone-update");
      e.emit("request", next);
      e.response(next, new Promise(() => {}));
      e.emit("requestfailed", next);
      await e.observed.verify(2);
      assert.equal(e.writes.at(-1).body.owned_unforwarded_truncations, 1);
      assert.equal(e.writes.at(-1).body.expected_incomplete, 1);
    },
  );
  for (const [name, changes] of [
    ["success status", { status: 200 }],
    ["different error", { status: 502 }],
    ["wrong content type", { headers: { "content-type": "application/json" } }],
    ["different length", { headers: { "content-length": "1" } }],
    ["different connection", { headers: { connection: "keep-alive" } }],
    ["upstream identity", { headers: { "x-request-id": key } }],
    ["empty upstream identity", { headers: { "x-request-id": "" } }],
    ["transfer encoding", { headers: { "transfer-encoding": "chunked" } }],
  ])
    await check(
      "actual observer: unforwarded headers reject " + name,
      async () => {
        const { e, q } = ownedCase();
        e.ownedResponse(q, new Promise(() => {}), changes);
        e.emit("requestfailed", q);
        await assert.rejects(e.observed.verify(1));
      },
    );
  await check(
    "actual observer: ordinary lost response cannot borrow unforwarded 503 allowance",
    async () => {
      const { e, q } = ownedCase("lost-milestone-update");
      e.ownedResponse(q, new Promise(() => {}));
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: unforwarded missing response headers and real failed still reject",
    async () => {
      const { e, q } = ownedCase();
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: unforwarded success finished is not failed",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, Promise.resolve(null));
      e.emit("requestfinished", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: unforwarded other Request failure never closes original",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, Promise.resolve(Error("original-error")));
      e.emit("requestfailed", req("unforwarded-milestone-update"));
      e.emit("close");
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: duplicate unforwarded response rejects",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, new Promise(() => {}));
      e.ownedResponse(q, new Promise(() => {}));
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: page close never satisfies unforwarded failure",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, new Promise(() => {}));
      await new Promise((r) => setImmediate(r));
      e.emit("close");
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: declared failed settles incomplete, never finished",
    async () => {
      const e = observerEnv();
      e.observed.declareIncomplete(spec());
      const q = req();
      e.emit("request", q);
      const r = e.response(q, new Promise(() => {}));
      await new Promise((r) => setImmediate(r));
      e.emit("requestfailed", q);
      await e.observed.verify(1);
      assert.equal(r.calls(), 0);
      assert.equal(e.snapshot().requests[0].response_finished_at, null);
      assert.equal(e.decoded[0], q);
      assert.equal(e.writes.at(-1).body.expected_incomplete, 1);
    },
  );
  await check(
    "actual observer: undeclared failure cannot settle original finished",
    async () => {
      const e = observerEnv(),
        q = req();
      e.emit("request", q);
      let finish;
      e.response(
        q,
        new Promise((resolve) => {
          finish = resolve;
        }),
      );
      e.emit("requestfailed", q);
      const verify = e.observed.verify().then(
        () => true,
        () => true,
      );
      assert.equal(
        await Promise.race([
          verify,
          new Promise((r) => setImmediate(() => r(false))),
        ]),
        false,
      );
      finish(null);
      await verify;
    },
  );
  await check(
    "actual observer: missing failed terminal rejects even finished null",
    async () => {
      const e = observerEnv();
      e.observed.declareIncomplete(spec());
      const q = req();
      e.emit("request", q);
      e.response(q, Promise.resolve(null));
      e.emit("requestfinished", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: exact canceled read before headers plus ordinary success",
    async () => {
      const e = observerEnv(),
        ordinary = req();
      e.emit("request", ordinary);
      e.response(ordinary, Promise.resolve(null));
      const handle = e.observed.declareIncomplete(spec("canceled-task-read")),
        held = req("canceled-task-read");
      e.emit("request", held);
      handle.authorizeCancellation();
      e.emit("requestfailed", held);
      await e.observed.verify(1);
      assert.equal(e.decoded.length, 1);
      assert.equal(e.writes.at(-1).body.failed_without_headers, 1);
    },
  );
  await check(
    "actual observer: held read original finished rejection before verify remains failure",
    async () => {
      const e = observerEnv();
      const handle = e.observed.declareIncomplete(spec("canceled-task-read"));
      const q = req("canceled-task-read");
      e.emit("request", q);
      handle.authorizeCancellation();
      let reject;
      const late = new Promise((_, r) => (reject = r));
      e.response(q, late);
      await new Promise((r) => setImmediate(r));
      e.emit("requestfailed", q);
      reject(Error("late-original-rejection"));
      await new Promise((r) => setImmediate(r));
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: wrong body cannot count toward expected incomplete",
    async () => {
      const e = observerEnv();
      e.observed.declareIncomplete(spec());
      const q = req(undefined, {
        body: { expected_version: "7", request: { title: "different intent" } },
      });
      e.emit("request", q);
      e.response(q, Promise.resolve(Error("network error")));
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: default closed page after successful finished unchanged",
    async () => {
      const e = observerEnv(),
        q = req();
      e.emit("request", q);
      e.response(q, Promise.resolve(null));
      e.emit("close");
      await e.observed.verify();
      assert.equal(e.writes.at(-1).body.browser_complete, 1);
    },
  );
  await check(
    "actual observer: page close never supplies declared failed terminal",
    async () => {
      const e = observerEnv();
      e.observed.declareIncomplete(spec());
      const q = req();
      e.emit("request", q);
      e.response(q, Promise.resolve(null));
      e.emit("close");
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "schema and typed decoder unchanged from accepted read02 input",
    () => {
      const base = require("child_process").execFileSync(
        "git",
        [
          "show",
          "51b46c8f:tests/account-captcha-web/e2e/project-work-planning.helpers.ts",
        ],
        { encoding: "utf8" },
      );
      const ast = ts.createSourceFile(
        "before.ts",
        base,
        ts.ScriptTarget.Latest,
        true,
      );
      const print = ts.createPrinter();
      for (const name of ["decodeOriginal"]) {
        const old = ast.statements.find(
            (n) => ts.isFunctionDeclaration(n) && n.name?.text === name,
          ),
          now = tree.statements.find(
            (n) => ts.isFunctionDeclaration(n) && n.name?.text === name,
          );
        assert.equal(
          print.printNode(ts.EmitHint.Unspecified, old, ast),
          print.printNode(ts.EmitHint.Unspecified, now, tree),
        );
      }
      for (const name of ["schemaProgram"]) {
        const pick = (t) =>
          t.statements
            .find(
              (n) =>
                ts.isVariableStatement(n) &&
                n.declarationList.declarations.some(
                  (d) => d.name.getText(t) === name,
                ),
            )
            .getText(t);
        assert.equal(pick(ast), pick(tree));
      }
    },
  );
  const planningInput = (slot = 1) => ({
    slot,
    kind: ["milestone", "sprint", "task"][Math.floor((slot - 1) / 2)],
    tail: slot % 2 === 0,
    projectID: project,
    targetID: target,
    expectedVersion: slot % 2 === 0 ? "8" : "7",
    peerID: key,
    milestoneID: slot > 2 ? key : null,
    sprintID: slot > 4 ? project : null,
  });
  const planningRequest = (slot = 1, changes = {}) => {
    const input = planningInput(slot);
    return req("lost-milestone-update", {
      method: "POST",
      url: `http://127.0.0.1:1/api/v1/projects/${project}/${input.kind}s/${target}/reorder`,
      body: {
        expected_version: input.expectedVersion,
        request: {
          ...(input.kind === "sprint" ? { milestone_id: key } : {}),
          ...(!input.tail ? { before_id: key } : {}),
        },
      },
      ...changes,
    });
  };
  const planningEnvironment = (options = {}) =>
    observerEnv({
      reason: "aborted",
      ...options,
      planningReorders: {
        bind: async () => true,
        complete: () => true,
        ...options.planningReorders,
      },
    });
  async function planningSlot(e, slot, terminal = "failed", changes = {}) {
    const scope = e.observed.planningReorders,
      request = planningRequest(slot, changes);
    scope.arm(planningInput(slot));
    e.emit("request", request);
    const response = e.response(
      request,
      terminal === "finished" ? Promise.resolve(null) : new Promise(() => {}),
    );
    const body = scope.body(response.original);
    e.emit(
      terminal === "finished" ? "requestfinished" : "requestfailed",
      request,
    );
    await body;
    scope.endSlot(request);
    assert.equal(response.calls(), terminal === "finished" ? 1 : 0);
    return request;
  }
  for (const terminal of ["finished", "failed"])
    await check(
      "planning six originals share terminal and exactly one normal finished: " +
        terminal,
      async () => {
        let binds = 0;
        const e = planningEnvironment({
            planningReorders: {
              bind: async (q, input, headers) => {
                assert.equal(input.slot, ++binds);
                assert.equal(headers.origin, new URL(q.url()).origin);
                return true;
              },
            },
          }),
          scope = e.observed.planningReorders;
        for (let slot = 1; slot <= 6; slot++)
          await planningSlot(e, slot, terminal);
        scope.close();
        await e.observed.verify();
        assert.equal(binds, 6);
        assert.equal(scope.verify(), true);
      },
    );
  for (const [label, slot, change] of [
    [
      "version",
      1,
      { body: { expected_version: "8", request: { before_id: key } } },
    ],
    [
      "peer",
      1,
      { body: { expected_version: "7", request: { before_id: target } } },
    ],
    [
      "wrong target",
      1,
      {
        url: `http://127.0.0.1:1/api/v1/projects/${project}/milestones/${key}/reorder`,
      },
    ],
    ["method", 1, { method: "PATCH" }],
    [
      "tail null",
      2,
      { body: { expected_version: "8", request: { before_id: null } } },
    ],
    [
      "sprint parent",
      3,
      {
        body: {
          expected_version: "7",
          request: { milestone_id: target, before_id: key },
        },
      },
    ],
    [
      "sprint tail omitted parent",
      4,
      { body: { expected_version: "8", request: {} } },
    ],
    [
      "task tail null",
      6,
      { body: { expected_version: "8", request: { before_id: null } } },
    ],
  ])
    await check(
      "planning wrong first mutation occupies fixed slot: " + label,
      async () => {
        const e = planningEnvironment(),
          scope = e.observed.planningReorders;
        for (let i = 1; i < slot; i++) await planningSlot(e, i);
        scope.arm(planningInput(slot));
        const wrong = planningRequest(slot, change),
          later = planningRequest(slot);
        e.emit("request", wrong);
        e.emit("request", later);
        await drainHeaders();
        scope.close();
        assert.equal(scope.selected(wrong), true);
        assert.equal(scope.selected(later), false);
        assert.equal(scope.verify(), false);
      },
    );
  for (const failure of [
    "material",
    "duplicate",
    "late",
    "unconsumed",
    "non-aborted",
    "old-key",
  ])
    await check("planning whole conjunction rejects " + failure, async () => {
      let release;
      const held = new Promise((r) => (release = r));
      const e = planningEnvironment({
          reason: failure === "non-aborted" ? "connection-closed" : "aborted",
          planningReorders: {
            bind: async () =>
              failure === "late" ? await held : failure !== "material",
            complete: () => failure !== "unconsumed",
          },
        }),
        scope = e.observed.planningReorders;
      const request = planningRequest(1, { key: "same-key" });
      scope.arm(planningInput(1));
      e.emit("request", request);
      const response = e.response(request, new Promise(() => {})),
        body = scope.body(response.original);
      const rejected = ["unconsumed", "old-key"].includes(failure)
        ? null
        : assert.rejects(body);
      e.emit("requestfailed", request);
      if (failure === "duplicate") e.emit("request", planningRequest());
      if (failure === "late") {
        await drainHeaders();
        scope.close();
        release(true);
      }
      if (rejected) await rejected;
      else {
        await body;
        scope.endSlot(request);
      }
      if (failure === "old-key")
        await assert.rejects(planningSlot(e, 2, "failed", { key: "same-key" }));
      if (failure === "unconsumed")
        for (let i = 2; i <= 6; i++) await planningSlot(e, i);
      scope.close();
      await assert.rejects(e.observed.verify());
      assert.equal(response.calls(), 0);
    });
  for (const bad of [
    { slot: 2 },
    { kind: "task" },
    { tail: true },
    { peerID: target },
    { milestoneID: key },
  ])
    await check(
      "planning wrong arm permanently occupies sequence: " + Object.keys(bad),
      async () => {
        const e = planningEnvironment(),
          scope = e.observed.planningReorders;
        assert.throws(() => scope.arm({ ...planningInput(), ...bad }));
        assert.throws(() => scope.arm(planningInput()));
        scope.close();
        assert.equal(scope.verify(), false);
      },
    );
  await check("planning missing final slot cannot close complete", async () => {
    const e = planningEnvironment(),
      scope = e.observed.planningReorders;
    for (let i = 1; i <= 5; i++) await planningSlot(e, i);
    scope.close();
    await assert.rejects(e.observed.verify());
  });
  await check(
    "planning actual PW header Promise registers before start, joins after controlled close",
    async () => {
      let scope,
        owned = 0;
      const q = planningRequest(),
        r = rawRequest(
          "lost-milestone-update",
          { url: q.url(), method: q.method(), body: q.postDataJSON() },
          { omitOrigin: true, onCall: () => assert.equal(owned, 1) },
        );
      const f = tree.statements.find(
        (n) =>
          ts.isFunctionDeclaration(n) &&
          n.name?.text === "workPlanningReorders",
      );
      const c = vm.createContext({
        URL,
        Promise,
        Set,
        Map,
        Object,
        Date,
        Error,
        uuid7:
          /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      });
      vm.runInContext(
        ts.transpileModule(
          f.getText(tree) + ";this.make=workPlanningReorders;",
          { compilerOptions: { target: ts.ScriptTarget.ES2024 } },
        ).outputText,
        c,
      );
      const tails = [];
      scope = c.make(
        (t) => {
          tails.push(t);
          owned++;
        },
        async () => true,
      );
      scope.arm(planningInput());
      scope.request(r.request);
      scope.close();
      assert.equal(scope.evidence(r.request).joined, false);
      r.release();
      await Promise.all(tails);
      assert.equal(r.calls(), 1);
      assert.equal(scope.evidence(r.request).joined, true);
      assert.equal(scope.verify(), false);
    },
  );
  await check(
    "planning policy leaves default GET finished and rejects unarmed reorder",
    async () => {
      const e = planningEnvironment(),
        scope = e.observed.planningReorders,
        q = req("canceled-task-read"),
        r = e.response(q, Promise.resolve(null));
      e.emit("requestfinished", q);
      await drainHeaders();
      assert.equal(r.calls(), 1);
      assert.equal(scope.selected(q), false);
      const wrong = planningRequest();
      e.emit("request", wrong);
      scope.arm(planningInput());
      assert.equal(scope.selected(wrong), false);
      scope.close();
    },
  );
  await check(
    "planning verify seals original material admission including decoder-stage new reorder",
    async () => {
      let e;
      e = planningEnvironment({
        duringDecode: () => e.emit("request", planningRequest(1)),
      });
      for (let i = 1; i <= 6; i++) await planningSlot(e, i);
      e.observed.planningReorders.close();
      await assert.rejects(e.observed.verify());
      assert.equal(e.observed.planningReorders.verify(), false);
    },
  );
  await check(
    "planning untouched Recovery R13 ledger decoder and default body AST",
    () => {
      const print = ts.createPrinter(),
        oldText = require("node:child_process").execFileSync(
          "git",
          [
            "show",
            "68d146f9:tests/account-captcha-web/e2e/project-work-planning.helpers.ts",
          ],
          { encoding: "utf8" },
        );
      const old = ts.createSourceFile(
        "old.ts",
        oldText,
        ts.ScriptTarget.Latest,
        true,
      );
      const pick = (ast, name) =>
        ast.statements.find(
          (n) => ts.isFunctionDeclaration(n) && n.name?.text === name,
        );
      for (const name of [
        "workIncompleteLedger",
        "decodeOriginal",
        "originalBody",
        "capturedOriginalBody",
      ])
        assert.equal(
          print.printNode(ts.EmitHint.Unspecified, pick(old, name), old),
          print.printNode(ts.EmitHint.Unspecified, pick(tree, name), tree),
        );
      const specPath =
          "tests/account-captcha-web/e2e/project-work-planning.spec.ts",
        previous = require("node:child_process").execFileSync(
          "git",
          ["show", "68d146f9:" + specPath],
          { encoding: "utf8" },
        ),
        current = fs.readFileSync(specPath, "utf8");
      const recovery = (text) =>
        text.slice(
          text.indexOf('test("[recovery]'),
          text.indexOf('test("[identity]'),
        );
      assert(recovery(previous).length > 1000);
      assert.equal(recovery(previous), recovery(current));
    },
  );
  await check(
    "default originalBody retains original finished gate before captured body",
    async () => {
      const e = observerEnv(),
        q = req("canceled-task-read");
      let finish;
      e.emit("request", q);
      const r = e.response(
        q,
        new Promise((resolve) => {
          finish = resolve;
        }),
      );
      let returned = false;
      const body = e.originalBody(r.original).then(() => {
        returned = true;
      });
      await drainHeaders();
      assert.equal(returned, false);
      finish(null);
      await body;
      assert.equal(returned, true);
    },
  );
  const ownedHeaders = {
    "content-type": "application/problem+json",
    "content-length": "4096",
    connection: "close",
  };
  for (const late of [false, true])
    await check(
      "same locked replay verifies headers " +
        (late ? "after" : "before") +
        " request",
      async () => {
        const kind = "unforwarded-milestone-update",
          l = ledger(),
          original = req(kind, { key }),
          replay = req(kind, { key });
        l.declare(spec(kind));
        l.request(original);
        assert.throws(() => l.armOriginalReplay(kind));
        l.failed(original);
        if (!late)
          assert.equal(
            l.ownedUnforwardedResponse(original, 503, ownedHeaders),
            true,
          );
        await l.armOriginalReplay(kind);
        l.request(replay);
        assert.equal(l.replayEvidence(replay).at_request_verified, !late);
        if (late)
          assert.equal(
            l.ownedUnforwardedResponse(original, 503, ownedHeaders),
            true,
          );
        await new Promise((r) => setImmediate(r));
        const proof = l.replayEvidence(replay);
        assert.equal(proof.at_request_verified, !late);
        assert.equal(proof.later_verified, late);
        assert.equal(l.isOriginalReplay(replay), true);
        l.finishOriginalReplay();
        assert.equal(l.verify(1, new Set([original])), true);
      },
    );
  for (const changes of [
    { key: "wrong" },
    { csrf: "wrong" },
    { origin: "https://wrong.invalid" },
    {
      url: `http://127.0.0.1:2/api/v1/projects/${project}/milestones/${target}`,
    },
    {
      body: {
        expected_version: "8",
        request: { title: "specific original text" },
      },
    },
    { body: { expected_version: "7", request: { title: "different" } } },
    {
      rawBody: JSON.stringify(
        { expected_version: "7", request: { title: "specific original text" } },
        null,
        1,
      ),
    },
    { method: "POST" },
    { url: `http://127.0.0.1:1/api/v1/projects/${project}/milestones/${key}` },
  ])
    await check(
      "wrong first mutation occupies replay slot: " +
        Object.keys(changes).join(),
      async () => {
        const kind = "unforwarded-milestone-update",
          l = ledger(),
          original = req(kind, { key }),
          bad = req(kind, { key, ...changes }),
          later = req(kind, { key });
        l.declare(spec(kind));
        l.request(original);
        l.failed(original);
        l.ownedUnforwardedResponse(original, 503, ownedHeaders);
        await l.armOriginalReplay(kind);
        l.request(bad);
        l.request(later);
        l.finishOriginalReplay();
        assert.equal(l.replayCandidate(bad), true);
        assert.equal(l.replayCandidate(later), false);
        assert.equal(l.isOriginalReplay(bad), false);
        assert.equal(l.verify(1, new Set([original])), false);
      },
    );
  for (const ending of [
    "end",
    "close",
    "duplicate",
    "missing",
    "wrong-headers",
  ])
    await check("late original headers cannot upgrade " + ending, async () => {
      const kind = "unforwarded-milestone-update",
        l = ledger(),
        original = req(kind, { key }),
        replay = req(kind, { key });
      l.declare(spec(kind));
      l.request(original);
      l.failed(original);
      await l.armOriginalReplay(kind);
      l.request(replay);
      if (ending === "close") l.close();
      else if (ending === "duplicate") l.request(req(kind, { key }));
      else if (ending === "end") l.finishOriginalReplay();
      if (ending !== "missing")
        l.ownedUnforwardedResponse(
          original,
          503,
          ending === "wrong-headers"
            ? { ...ownedHeaders, "content-length": "1" }
            : ownedHeaders,
        );
      if (!["end", "close"].includes(ending)) l.finishOriginalReplay();
      assert.equal(l.replayEvidence(replay).at_request_verified, false);
      assert.equal(l.isOriginalReplay(replay), false);
      assert.equal(l.verify(1, new Set([original])), false);
    });
  await check(
    "Task replay freezes exact latest Lookup Request and rejects unready anchor",
    async () => {
      const kind = "lost-task-update",
        l = ledger(),
        original = req(kind, { key }),
        replay = req(kind, { key });
      l.declare(spec(kind));
      l.request(original);
      l.failed(original);
      assert.throws(() => l.armOriginalReplay(kind));
      const lookup = req(kind, {
        key,
        method: "POST",
        url: `http://127.0.0.1:1/api/v1/projects/${project}/task-commands/lookup`,
        body: {
          command: "work.task.update",
          target_id: target,
          expected_version: "7",
          request: { title: "specific original text" },
        },
      });
      l.request(lookup);
      const waiting = l.armOriginalReplay(kind);
      assert.equal(l.replayCandidate(replay), false);
      l.lookupResponse(lookup, key, true);
      await waiting;
      l.request(replay);
      await new Promise((r) => setImmediate(r));
      l.finishOriginalReplay();
      assert.equal(l.replayEvidence(replay).lookup_request_id, key);
      assert.equal(l.replayEvidence(replay).lookup_finished, true);
      assert.equal(l.replayEvidence(replay).policy, "historical-task");
      assert.equal(l.verify(1, new Set([original])), true);
    },
  );
  for (const late of [false, true])
    await check(
      "actual PW both raw sides preserve provisional fact; late=" + late,
      async () => {
        const kind = "unforwarded-milestone-update",
          tails = [];
        const l = ledger((tail) => tails.push(tail));
        const original = rawRequest(
          kind,
          { key },
          { omitOrigin: true, onCall: () => assert.equal(tails.length, 1) },
        );
        const replay = rawRequest(
          kind,
          { key },
          { omitOrigin: true, onCall: () => assert.equal(tails.length, 2) },
        );
        l.declare(spec(kind));
        l.request(original.request);
        l.failed(original.request);
        l.ownedUnforwardedResponse(original.request, 503, ownedHeaders);
        if (!late) original.release();
        const arm = l.armOriginalReplay(kind);
        assert.equal(l.replayCandidate(replay.request), false);
        if (late) {
          await drainHeaders();
          assert.equal(l.headerState(original.request).joined, false);
          original.release();
        }
        await arm;
        if (!late) replay.release();
        l.request(replay.request);
        assert.equal(
          l.replayEvidence(replay.request).at_request_verified,
          false,
        );
        assert.equal(l.isOriginalReplay(replay.request), false);
        if (late) replay.release();
        await Promise.all(tails);
        assert.equal(original.request.headers().origin, undefined);
        assert.equal(replay.request.headers().origin, undefined);
        assert.equal(
          l.replayEvidence(replay.request).at_request_verified,
          false,
        );
        assert.equal(l.replayEvidence(replay.request).actual_verified, true);
        assert.equal(l.replayEvidence(replay.request).later_verified, true);
        assert.equal(l.replayEvidence(replay.request).headers_joined, true);
        assert.equal(original.calls(), 1);
        assert.equal(replay.calls(), 1);
        l.finishOriginalReplay();
        l.sealHeaders();
        assert.equal(l.headersComplete(), true);
        assert.equal(l.verify(1, new Set([original.request])), true);
      },
    );
  for (const side of ["original", "replay"])
    for (const [field, value] of [
      ["origin", "https://wrong.invalid"],
      ["x-csrf-token", "wrong"],
      ["idempotency-key", "wrong"],
    ])
      await check(
        "actual PW conflict overrides matching provisional " +
          side +
          "/" +
          field,
        async () => {
          const kind = "unforwarded-milestone-update",
            tails = [],
            l = ledger((tail) => tails.push(tail));
          const original = rawRequest(kind, { key }),
            replay = rawRequest(kind, { key });
          l.declare(spec(kind));
          l.request(original.request);
          l.failed(original.request);
          l.ownedUnforwardedResponse(original.request, 503, ownedHeaders);
          original.release(side === "original" ? { [field]: value } : {});
          const arm = l.armOriginalReplay(kind);
          if (side === "original" && field !== "x-csrf-token")
            await assert.rejects(arm);
          else {
            await arm;
            l.request(replay.request);
            assert.equal(
              l.replayEvidence(replay.request).at_request_verified,
              true,
            );
            replay.release(side === "replay" ? { [field]: value } : {});
            await Promise.all(tails);
            assert.equal(
              l.replayEvidence(replay.request).at_request_verified,
              true,
            );
            assert.equal(
              l.replayEvidence(replay.request).actual_verified,
              false,
            );
            assert.equal(l.replayEvidence(replay.request).invalid, true);
            l.finishOriginalReplay();
          }
          await Promise.all(tails);
          l.sealHeaders();
          // An arm rejection is terminal to its caller; no later mutation becomes a replay.
          assert.equal(l.isOriginalReplay(replay.request), false);
        },
      );
  function taskLookup(overrides = {}, options = {}) {
    return rawRequest(
      "lost-task-update",
      {
        key,
        method: "POST",
        url: `http://127.0.0.1:1/api/v1/projects/${project}/task-commands/lookup`,
        body: {
          command: "work.task.update",
          target_id: target,
          expected_version: "7",
          request: { title: "specific original text" },
        },
        ...overrides,
      },
      options,
    );
  }
  for (const order of ["headers-first", "response-first"])
    await check(
      "actual PW Lookup retains original response before/after raw headers " +
        order,
      async () => {
        const kind = "lost-task-update",
          tails = [],
          l = ledger((tail) => tails.push(tail));
        const original = rawRequest(kind, { key }, { omitOrigin: true }),
          lookup = taskLookup({}, { omitOrigin: true });
        l.declare(spec(kind));
        l.request(original.request);
        l.failed(original.request);
        l.request(lookup.request);
        let armed = false;
        if (order === "headers-first") {
          original.release();
          lookup.release();
          await Promise.all(tails);
        } else l.lookupResponse(lookup.request, key, true);
        const arm = l.armOriginalReplay(kind).then(() => {
          armed = true;
        });
        await drainHeaders();
        assert.equal(armed, false);
        if (order === "headers-first")
          l.lookupResponse(lookup.request, key, true);
        else {
          original.release();
          lookup.release();
        }
        await arm;
        const replay = rawRequest(kind, { key }, { omitOrigin: true });
        replay.release();
        l.request(replay.request);
        await Promise.all(tails);
        const proof = l.replayEvidence(replay.request);
        assert.equal(proof.lookup_request_id, key);
        assert.equal(proof.lookup_finished, true);
        assert.equal(proof.actual_verified, true);
        l.finishOriginalReplay();
        l.sealHeaders();
        assert.equal(l.headersComplete(), true);
        assert.equal(l.verify(1, new Set([original.request])), true);
        assert.equal(original.calls(), 1);
        assert.equal(lookup.calls(), 1);
      },
    );
  for (const invalid of [
    "origin",
    "csrf",
    "key",
    "target",
    "version",
    "request",
  ])
    await check(
      "latest invalid Lookup occupies slot without fallback " + invalid,
      async () => {
        const kind = "lost-task-update",
          tails = [],
          l = ledger((tail) => tails.push(tail));
        const original = rawRequest(kind, { key }),
          good = taskLookup();
        l.declare(spec(kind));
        l.request(original.request);
        l.failed(original.request);
        original.release();
        good.release();
        l.request(good.request);
        l.lookupResponse(good.request, key, true);
        await Promise.all(tails);
        const body = {
          command: "work.task.update",
          target_id: target,
          expected_version: "7",
          request: { title: "specific original text" },
        };
        if (invalid === "target") body.target_id = key;
        if (invalid === "version") body.expected_version = "8";
        if (invalid === "request") body.request.title = "wrong";
        const bad = taskLookup({ body });
        l.request(bad.request);
        l.lookupResponse(bad.request, target, true);
        bad.release(
          invalid === "origin"
            ? { origin: "https://wrong.invalid" }
            : invalid === "csrf"
              ? { "x-csrf-token": "wrong" }
              : invalid === "key"
                ? { "idempotency-key": "wrong" }
                : {},
        );
        await assert.rejects(l.armOriginalReplay(kind));
        await Promise.all(tails);
        assert.equal(good.calls(), 1);
        assert.equal(bad.calls(), 1);
      },
    );
  for (const interruption of [
    "new-lookup",
    "mutation",
    "close",
    "seal",
    "reject",
  ])
    await check(
      "waiting latest Lookup cannot gain permission after " + interruption,
      async () => {
        const kind = "lost-task-update",
          tails = [],
          l = ledger((tail) => tails.push(tail));
        const original = rawRequest(kind, { key }),
          lookup = taskLookup();
        l.declare(spec(kind));
        l.request(original.request);
        l.failed(original.request);
        original.release();
        l.request(lookup.request);
        l.lookupResponse(lookup.request, key, true);
        const arm = l.armOriginalReplay(kind),
          rejected = assert.rejects(arm);
        let next;
        if (interruption === "new-lookup") {
          next = taskLookup();
          l.request(next.request);
          next.release();
          l.lookupResponse(next.request, target, true);
        } else if (interruption === "mutation") l.request(req(kind, { key }));
        else if (interruption === "close") l.close();
        else if (interruption === "seal") l.sealHeaders();
        if (interruption === "reject") lookup.reject();
        else lookup.release();
        await rejected;
        await Promise.all(tails);
        assert.equal(l.headerState(lookup.request).joined, true);
        assert.equal(l.replayCandidate(req(kind, { key })), false);
      },
    );
  for (const ending of ["end", "close", "seal"])
    await check(
      "pending actual replay header joins but cannot upgrade after " + ending,
      async () => {
        const kind = "unforwarded-milestone-update",
          tails = [],
          l = ledger((tail) => tails.push(tail));
        const original = rawRequest(kind, { key }),
          replay = rawRequest(kind, { key }, { omitOrigin: true });
        l.declare(spec(kind));
        l.request(original.request);
        l.failed(original.request);
        l.ownedUnforwardedResponse(original.request, 503, ownedHeaders);
        original.release();
        await l.armOriginalReplay(kind);
        l.request(replay.request);
        let joined = false;
        const join = Promise.all(tails).then(() => {
          joined = true;
        });
        if (ending === "end") l.finishOriginalReplay();
        else if (ending === "close") l.close();
        else l.sealHeaders();
        await drainHeaders();
        assert.equal(joined, false);
        assert.equal(l.headerState(replay.request).joined, false);
        replay.release();
        await join;
        assert.equal(joined, true);
        assert.equal(l.headerState(replay.request).joined, true);
        assert.equal(l.replayEvidence(replay.request).actual_verified, false);
        assert.equal(l.verify(1, new Set([original.request])), false);
      },
    );
  await check(
    "verify boundary seals admission; successor cannot escape original tail join",
    async () => {
      const kind = "lost-task-update",
        tails = [],
        l = ledger((tail) => tails.push(tail));
      const original = rawRequest(kind, { key }),
        lookup = taskLookup();
      l.declare(spec(kind));
      l.request(original.request);
      l.failed(original.request);
      l.request(lookup.request);
      l.lookupResponse(lookup.request, key, true);
      l.sealHeaders();
      const admitted = tails.length,
        join = Promise.all(tails);
      const successor = taskLookup();
      l.request(successor.request);
      successor.release();
      assert.equal(tails.length, admitted);
      assert.equal(successor.calls(), 0);
      assert.equal(l.headersComplete(), false);
      original.release();
      lookup.release();
      await join;
      assert.equal(l.headerState(original.request).joined, true);
      assert.equal(l.headerState(lookup.request).joined, true);
      assert.equal(l.headersComplete(), false);
    },
  );
  for (const settle of ["fulfill", "reject"])
    await check(
      "afterEach seals pending actual header and preserves first snapshot: " +
        settle,
      async () => {
        const kind = "unforwarded-milestone-update",
          e = observerEnv();
        const original = rawRequest(kind, { key }),
          replay = rawRequest(kind, { key }, { omitOrigin: true });
        e.observed.declareIncomplete(spec(kind));
        e.emit("request", original.request);
        e.ownedResponse(original.request, new Promise(() => {}));
        e.emit("requestfailed", original.request);
        original.release();
        await drainHeaders();
        await e.observed.armOriginalReplay(kind);
        e.emit("request", replay.request);
        await drainHeaders();
        e.saveFailure();
        const first = structuredClone(e.writes.at(-1).body);
        assert.equal(
          first.observers[0].requests.at(-1).original_request_headers.state,
          "pending",
        );
        assert.equal(
          first.observers[0].requests.at(-1).original_request_headers.joined,
          false,
        );
        if (settle === "fulfill") replay.release();
        else replay.reject();
        await drainHeaders();
        const last = e.writes.at(-1).body;
        assert.deepEqual(last.observers, first.observers);
        assert.equal(last.tail_events.length, 1);
        assert.equal(last.tail_events[0].stage, "request-headers");
        assert.equal(
          last.tail_events[0].reason,
          settle === "fulfill" ? "headers-after-end" : "headers-rejected",
        );
        assert.equal(last.tail_events[0].after_first_snapshot, true);
        assert.equal(
          e.observed.replayEvidence(replay.request).headers_joined,
          true,
        );
        assert.equal(
          e.observed.replayEvidence(replay.request).actual_verified,
          false,
        );
        assert(
          !JSON.stringify(last).includes("private-header-rejection-canary"),
        );
        await assert.rejects(e.observed.verify(1));
      },
    );
  await check(
    "actual observer rejects a new header candidate during decode after its original tail join",
    async () => {
      const kind = "lost-task-update",
        original = rawRequest(kind, { key });
      const lookup = taskLookup(),
        successor = taskLookup();
      const e = observerEnv({
        duringDecode: () => e.emit("request", successor.request),
      });
      e.observed.declareIncomplete(spec(kind));
      e.emit("request", original.request);
      e.emit("requestfailed", original.request);
      e.emit("request", lookup.request);
      original.release();
      lookup.release();
      e.response(lookup.request, Promise.resolve(null));
      e.emit("requestfinished", lookup.request);
      await drainHeaders();
      await assert.rejects(e.observed.verify(1));
      assert.equal(original.calls(), 1);
      assert.equal(lookup.calls(), 1);
      assert.equal(successor.calls(), 0);
      successor.release();
    },
  );
  await check(
    "afterEach first snapshot survives later original rejection with safe bounded event",
    async () => {
      const e = observerEnv(),
        q = req("canceled-task-read");
      let reject;
      const tail = new Promise((_, r) => {
        reject = r;
      });
      e.emit("request", q);
      e.response(q, tail);
      await new Promise((r) => setImmediate(r));
      e.saveFailure();
      const first = structuredClone(e.writes.at(-1).body);
      assert.equal(first.observers[0].requests[0].observer_rejected_at, null);
      reject(Error("private-rejection-material-canary"));
      await new Promise((r) => setImmediate(r));
      const last = e.writes.at(-1).body;
      assert.deepEqual(last.observers, first.observers);
      assert.equal(last.tail_events.length, 1);
      assert.equal(last.tail_events[0].after_first_snapshot, true);
      assert.equal(last.tail_events[0].stage, "finished");
      assert.equal(last.tail_events[0].reason, "operation-rejected");
      assert(
        !JSON.stringify(last).includes("private-rejection-material-canary"),
      );
      await assert.rejects(e.observed.verify());
    },
  );
  for (const ending of ["active", "end", "close", "context-close"])
    await check(
      "original observer late headers on same captured Request: " + ending,
      async () => {
        const kind = "unforwarded-milestone-update",
          original = req(kind, { key }),
          replay = req(kind, { key });
        let releaseHeaders;
        const headers = new Promise((r) => {
          releaseHeaders = r;
        });
        const e = observerEnv({
          reason: "aborted",
          ordinaryCompletion: (request) =>
            request === replay && e.observed.isOriginalReplay(request),
        });
        e.observed.declareIncomplete(spec(kind));
        e.emit("request", original);
        e.ownedResponse(original, new Promise(() => {}), {
          headersPromise: headers,
        });
        e.emit("requestfailed", original);
        await new Promise((r) => setImmediate(r));
        await e.observed.armOriginalReplay(kind);
        e.emit("request", replay);
        assert.equal(
          e.observed.replayEvidence(replay).at_request_verified,
          false,
        );
        const response = e.response(replay, new Promise(() => {}));
        e.emit("requestfailed", replay);
        if (ending === "end") e.observed.finishOriginalReplay();
        if (ending === "close") e.emit("close");
        if (ending === "context-close") e.context.emit("close");
        releaseHeaders(ownedHeaders);
        await new Promise((r) => setImmediate(r));
        assert.equal(
          e.observed.replayEvidence(replay).at_request_verified,
          false,
        );
        assert.equal(
          e.observed.replayEvidence(replay).later_verified,
          ending === "active",
        );
        if (ending === "active") {
          e.observed.finishOriginalReplay();
          await e.observed.verify(1);
        } else await assert.rejects(e.observed.verify(1));
        assert.equal(response.calls(), 0);
      },
    );
  await check(
    "arming after original Request event cannot rematch that Request",
    async () => {
      const kind = "unforwarded-milestone-update",
        l = ledger(),
        original = req(kind, { key }),
        replay = req(kind, { key });
      l.declare(spec(kind));
      l.request(original);
      l.failed(original);
      l.ownedUnforwardedResponse(original, 503, ownedHeaders);
      l.request(replay);
      await l.armOriginalReplay(kind);
      assert.equal(l.replayCandidate(replay), false);
      assert.throws(() => l.finishOriginalReplay());
      assert.equal(l.verify(1, new Set([original])), false);
    },
  );
  await check(
    "actual catch events distinguish pre/post first snapshot and cap late diagnostics",
    async () => {
      const e = observerEnv();
      let rejectBefore, rejectAfter;
      const before = new Promise((_, r) => {
          rejectBefore = r;
        }),
        after = new Promise((_, r) => {
          rejectAfter = r;
        });
      const first = req("canceled-task-read");
      e.emit("request", first);
      e.response(first, before);
      await new Promise((r) => setImmediate(r));
      rejectBefore(Error("private-before-canary"));
      await new Promise((r) => setImmediate(r));
      e.saveFailure();
      const snapshot = structuredClone(e.writes.at(-1).body);
      assert.equal(snapshot.tail_events[0].after_first_snapshot, false);
      assert.notEqual(
        snapshot.observers[0].requests[0].observer_rejected_at,
        null,
      );
      for (let i = 0; i < 256; i++) {
        const q = req("canceled-task-read");
        e.emit("request", q);
        e.response(q, after);
      }
      await new Promise((r) => setImmediate(r));
      rejectAfter(Error("private-after-canary"));
      await new Promise((r) => setImmediate(r));
      const last = e.writes.at(-1).body;
      assert.deepEqual(last.observers, snapshot.observers);
      assert.equal(last.tail_events.length, 256);
      assert.equal(last.tail_overflow, true);
      assert.equal(last.tail_events.at(-1).after_first_snapshot, true);
      assert(!JSON.stringify(last).includes("canary"));
      await assert.rejects(e.observed.verify());
    },
  );
  for (const witnessed of [true, false])
    await check(
      "exact original replay observer requires final typed witness: " +
        witnessed,
      async () => {
        const kind = "unforwarded-milestone-update",
          original = req(kind, { key }),
          replay = req(kind, { key });
        const e = observerEnv({
          reason: "aborted",
          ordinaryCompletion: (request) =>
            request === replay &&
            e.observed.isOriginalReplay(request) &&
            witnessed,
        });
        e.observed.declareIncomplete(spec(kind));
        e.emit("request", original);
        e.ownedResponse(original, new Promise(() => {}));
        e.emit("requestfailed", original);
        await new Promise((r) => setImmediate(r));
        await e.observed.armOriginalReplay(kind);
        e.emit("request", replay);
        await new Promise((r) => setImmediate(r));
        assert.equal(e.observed.isOriginalReplay(replay), true);
        const response = e.response(replay, new Promise(() => {}));
        e.emit("requestfailed", replay);
        e.observed.finishOriginalReplay();
        if (witnessed) await e.observed.verify(1);
        else await assert.rejects(e.observed.verify(1));
        assert.equal(response.calls(), 0);
      },
    );
  for (const [method, endpoint] of [
    ["GET", `milestones/${target}`],
    ["GET", `sprints/${target}`],
    ["GET", `tasks/${target}`],
    ["POST", "structure-commands/lookup"],
    ["POST", "task-commands/lookup"],
    ["POST", `tasks/${target}/blocker-commands/lookup`],
  ])
    await check(
      "ordinary original event with full witness is separately complete: " +
        endpoint,
      async () => {
        const q = req("canceled-task-read", {
          method,
          url: `http://127.0.0.1:1/api/v1/projects/${project}/${endpoint}`,
        });
        let witnessed = 0;
        const e = observerEnv({
          reason: "aborted",
          ordinaryCompletion: (request, id) => {
            assert.equal(request, q);
            assert.match(id, /^019/);
            witnessed++;
            return true;
          },
        });
        e.emit("request", q);
        const r = e.response(q, new Promise(() => {}));
        e.emit("requestfailed", q);
        await e.observed.verify(0);
        assert.equal(r.calls(), 0);
        assert(witnessed >= 2);
        assert.equal(
          e.writes.at(-1).body.original_native_complete_with_pw_failed,
          1,
        );
        assert.equal(e.writes.at(-1).body.expected_incomplete, 0);
        assert.equal(e.context.listenerCount("close"), 0);
      },
    );
  await check(
    "ordinary locked PW actual failed leaves finished pending without starting it in observer",
    async () => {
      const root = require("path").resolve(
        "tests/account-captcha-web/node_modules/playwright-core",
      );
      const { BrowserContext } = require(
          root + "/lib/client/browserContext.js",
        ),
        { Response } = require(root + "/lib/client/network.js"),
        { ManualPromise, LongStandingScope } = require(
          root + "/lib/utils/isomorphic/manualPromise.js",
        );
      assert.equal(require(root + "/package.json").version, "1.56.1");
      const q = req("canceled-task-read"),
        closed = new LongStandingScope();
      q._targetClosedScope = () => closed;
      q._setResponseEndTiming = () => {};
      const native = {
          request: () => q,
          _finishedPromise: new ManualPromise(),
        },
        original = Response.prototype.finished.call(native),
        joined = original.then(
          () => "finished",
          () => "closed",
        );
      const e = observerEnv({
        reason: "aborted",
        ordinaryCompletion: () => true,
      });
      e.emit("request", q);
      const r = e.response(q, original);
      try {
        BrowserContext.prototype._onRequestFailed.call(
          { emit() {} },
          q,
          1,
          "net::ERR_ABORTED",
          { emit: e.emit },
        );
        assert.equal(
          await Promise.race([
            joined,
            new Promise((r) => setImmediate(() => r("pending"))),
          ]),
          "pending",
        );
        await e.observed.verify(0);
        assert.equal(r.calls(), 0);
      } finally {
        closed.close(Error("owned test closure"));
        assert.equal(await joined, "closed");
      }
    },
  );
  await check(
    "ordinary normal event still requires actual original finished null",
    async () => {
      const e = observerEnv({
          ordinaryCompletion: () => {
            throw Error("native branch not expected");
          },
        }),
        q = req("canceled-task-read");
      e.emit("request", q);
      const r = e.response(q, Promise.resolve(null));
      await new Promise((r) => setImmediate(r));
      assert.equal(r.calls(), 0);
      e.emit("requestfinished", q);
      await e.observed.verify(0);
      assert.equal(r.calls(), 1);
    },
  );
  for (const condition of [
    "unproven",
    "wrong-reason",
    "no-event",
    "page-close",
    "context-close",
    "late-failed",
    "late-finished",
    "duplicate-failed",
    "schema",
    "client",
  ])
    await check("ordinary alternative rejects " + condition, async () => {
      const e = observerEnv({
          reason:
            condition === "wrong-reason" ? "connection-closed" : "aborted",
          ordinaryCompletion: () => condition !== "unproven",
          schemaStatus: condition === "schema" ? 1 : 0,
          decodeReject: condition === "client",
        }),
        q = req("canceled-task-read");
      e.emit("request", q);
      const r = e.response(q, new Promise(() => {}));
      if (condition === "late-failed") e.emit("close");
      if (condition !== "no-event") e.emit("requestfailed", q);
      if (condition === "page-close") e.emit("close");
      if (condition === "context-close") e.context.emit("close");
      if (condition === "late-finished") e.emit("requestfinished", q);
      if (condition === "duplicate-failed") e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(0));
      assert.equal(r.calls(), 0);
      assert.equal(e.context.listenerCount("close"), 0);
    });
  for (const [method, endpoint] of [
    ["PATCH", `milestones/${target}`],
    ["GET", `tasks/${target}?limit=1`],
    ["GET", "tasks"],
    ["GET", `tasks/${target}/blocker-commands/lookup`],
    ["POST", `tasks/${target}/blocker-commands/lookup?unexpected=1`],
    ["POST", `tasks/${target}/blocker-commands/lookup/extra`],
    ["POST", `tasks/not-a-task/blocker-commands/lookup`],
    ["POST", `tasks/${target}/blockers`],
  ])
    await check(
      "ordinary alternative cannot include other endpoint: " +
        method +
        endpoint,
      async () => {
        const e = observerEnv({
            reason: "aborted",
            ordinaryCompletion: () => {
              throw Error("not eligible");
            },
          }),
          q = req("canceled-task-read", {
            method,
            url: `http://127.0.0.1:1/api/v1/projects/${project}/${endpoint}`,
          });
        e.emit("request", q);
        const r = e.response(q, Promise.resolve(null));
        e.emit("requestfailed", q);
        await assert.rejects(e.observed.verify(0));
        assert.equal(r.calls(), 1);
      },
    );
  await check(
    "ordinary alternative never borrows held-read declaration",
    async () => {
      const e = observerEnv({
          reason: "aborted",
          ordinaryCompletion: () => {
            throw Error("not eligible");
          },
        }),
        q = req("canceled-task-read"),
        slot = e.observed.declareIncomplete(spec("canceled-task-read"));
      e.emit("request", q);
      slot.authorizeCancellation();
      const r = e.response(q, Promise.resolve(null));
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(1));
      assert.equal(r.calls(), 1);
    },
  );
  await check(
    "ordinary late counterevent during original client verification still rejects",
    async () => {
      const q = req("canceled-task-read"),
        e = observerEnv({
          reason: "aborted",
          ordinaryCompletion: () => true,
          duringDecode: () => e.emit("requestfinished", q),
        });
      e.emit("request", q);
      const r = e.response(q, new Promise(() => {}));
      e.emit("requestfailed", q);
      await assert.rejects(e.observed.verify(0));
      assert.equal(r.calls(), 0);
    },
  );
  await new Promise((r) => setImmediate(r));
  assert.equal(unhandled, 0);

  const result = {
    status: "actual-helper-pure-controls",
    controls,
    passed: controls.length,
    unhandled,
  };
  fs.writeFileSync(
    process.env.WORK_PLANNING_ONLY === "1"
      ? "output/ai/work-owner-planning-ui/implementation/planning-helper-controls.json"
      : "output/ai/work-owner-planning-ui/implementation/expected-incomplete-503-controls.json",
    JSON.stringify(result, null, 2) + "\n",
  );
  console.log(
    JSON.stringify({
      passed: controls.length,
      unhandled,
      status: result.status,
    }),
  );
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
