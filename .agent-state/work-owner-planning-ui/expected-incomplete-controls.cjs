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
function ledger() {
  const c = {
    URL,
    Promise,
    Error,
    Set,
    Object,
    uuid7:
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  };
  vm.createContext(c);
  vm.runInContext(code + ";this.make=workIncompleteLedger;", c);
  return c.make();
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
    key,
    ...changes,
  };
  return {
    url: () => values.url,
    method: () => values.method,
    headers: () => ({ "idempotency-key": values.key }),
    postData: () => (values.body === null ? null : JSON.stringify(values.body)),
    postDataJSON: () => values.body,
  };
}
let unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
(async () => {
  const controls = [];
  async function check(name, fn) {
    await fn();
    controls.push(name);
  }
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
    { key: "wrong-key" },
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
  function observerEnv() {
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
      workDiagnosticTarget: () => ({}),
      safeWorkFailure: () => "controlled-failure",
      evidence: "/owned",
      repository: "/repo",
      schemaProgram: "unchanged-schema",
      join: require("path").join,
      expect: (value) => ({ toBe: (want) => assert.equal(value, want) }),
      spawnSync: () => ({
        status: 0,
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
          ["workIncompleteLedger", "observe"].includes(n.name?.text),
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
    const page = {
      on: (name, fn) => {
        (callbacks[name] ??= []).push(fn);
      },
    };
    const observed = c.make(page),
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
      return { calls: () => calls };
    }
    return {
      observed,
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
          allHeaders: async () => headers,
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
      assert.equal(r.calls(), 1);
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
      await assert.rejects(e.observed.verify(1));
    },
  );
  await check(
    "actual observer: unforwarded other Request failure never closes original",
    async () => {
      const { e, q } = ownedCase();
      e.ownedResponse(q, Promise.resolve(Error("original-error")));
      e.emit("requestfailed", req("unforwarded-milestone-update"));
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
      assert.equal(r.calls(), 1);
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
      e.response(q, new Promise(() => {}));
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
    "actual observer: original finished rejection before verify remains failure",
    async () => {
      const e = observerEnv();
      e.observed.declareIncomplete(spec());
      const q = req();
      e.emit("request", q);
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
    "originalBody/schema/decoder unchanged from accepted read02 input",
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
      for (const name of ["originalBody", "decodeOriginal"]) {
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
  await new Promise((r) => setImmediate(r));
  assert.equal(unhandled, 0);

  const result = {
    status: "actual-helper-pure-controls",
    controls,
    passed: controls.length,
    unhandled,
  };
  fs.writeFileSync(
    "output/ai/work-owner-planning-ui/implementation/expected-incomplete-503-controls.json",
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
