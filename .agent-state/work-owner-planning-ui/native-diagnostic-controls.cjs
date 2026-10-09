const fs = require("node:fs"),
  vm = require("node:vm"),
  assert = require("node:assert/strict"),
  path = require("node:path");
const ts = require(path.resolve("web/node_modules/typescript"));
const nativePath =
  "tests/account-captcha-web/e2e/project-work-planning.native.ts";
const publicationPath =
  "tests/account-captcha-web/e2e/project-work-planning.publication.ts";
const project = "01900000-0000-7000-8000-000000000001",
  target = "01900000-0000-7000-8000-000000000002",
  requestID = "01900000-0000-7000-8000-000000000003";
const endpoint = `/api/v1/projects/${project}/tasks/${target}`;
const tick = () => new Promise((resolve) => setImmediate(resolve));
let unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
function source(file, name) {
  const text = fs.readFileSync(file, "utf8"),
    ast = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true);
  const node = ast.statements.find(
    (n) => ts.isFunctionDeclaration(n) && n.name?.text === name,
  );
  assert(node);
  return ts.transpileModule(
    node.getText(ast).replace(/^export /, "") + `\nthis.subject=${name};`,
    {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.CommonJS,
      },
    },
  ).outputText;
}
function environment(extra = {}) {
  return vm.createContext({
    URL,
    Request,
    Response,
    Headers,
    ReadableStream,
    Uint8Array,
    Promise,
    Object,
    Reflect,
    Number,
    Error,
    Date,
    crypto: require("node:crypto").webcrypto,
    setTimeout,
    clearTimeout,
    performance,
    location: { origin: "http://127.0.0.1:1" },
    ...extra,
  });
}
function native(options = {}) {
  let count = 0,
    index = 0,
    releaseCalls = 0;
  const promises = [],
    original = {};
  const reader = {
    read() {
      const promise = options.read
        ? options.read(index++)
        : Promise.resolve(
            index++
              ? { done: true }
              : { done: false, value: Uint8Array.of(1, 2, 3) },
          );
      promises.push(promise);
      return promise;
    },
    cancel() {
      return options.cancel ? options.cancel() : Promise.resolve();
    },
    releaseLock() {
      releaseCalls++;
      if (options.releaseThrows) throw Error("original-release");
    },
  };
  const stream = {
    getReader() {
      if (options.getReaderThrows) throw Error("original-reader");
      return reader;
    },
    cancel() {
      return options.streamCancel ? options.streamCancel() : Promise.resolve();
    },
  };
  const response = {
    status: 200,
    headers: new Headers({
      "X-Request-ID": requestID,
      "Content-Length": "3",
      ...options.headers,
    }),
    body: stream,
  };
  const fetchPromise = Promise.resolve(response);
  const fetch = function (...args) {
    count++;
    original.args = args;
    return fetchPromise;
  };
  const window = { fetch };
  const context = environment({ window });
  vm.runInContext(source(nativePath, "installWorkNativeDiagnostic"), context);
  context.subject({ projects: [project], expiresAt: Date.now() + 45_000 });
  return {
    window,
    reader,
    stream,
    promises,
    response,
    fetch,
    fetchPromise,
    original,
    count: () => count,
    releaseCalls: () => releaseCalls,
    row: () => window.__workNativeDiagnostic.snapshot().requests[0],
    finish: () => window.__workNativeDiagnostic.finish(),
  };
}
async function consume(f) {
  const init = {
    method: "GET",
    body: "PRIVATE-MATERIAL-SENTINEL",
    headers: { "X-CSRF-Token": "PRIVATE-MATERIAL-SENTINEL" },
  };
  const promise = f.window.fetch(endpoint, init);
  assert.equal(promise, f.fetchPromise);
  assert.equal(await promise, f.response);
  assert.equal(f.original.args[1], init);
  const reader = f.stream.getReader();
  const first = reader.read();
  assert.equal(first, f.promises[0]);
  await first;
  const eof = reader.read();
  assert.equal(eof, f.promises[1]);
  await eof;
  const cancel = reader.cancel();
  await cancel;
  reader.releaseLock();
  await f.stream.cancel();
  await tick();
  return f.row();
}
(async () => {
  const passed = [];
  async function check(name, action) {
    await action();
    passed.push(name);
  }
  await check(
    "actual native wrapper retains fetch/Response/read identity and does not retain body/header values",
    async () => {
      const f = native(),
        read = f.reader.read,
        cancel = f.reader.cancel,
        release = f.reader.releaseLock,
        getReader = f.stream.getReader,
        streamCancel = f.stream.cancel;
      try {
        const row = await consume(f);
        assert.equal(f.count(), 1);
        assert.equal(row.bytes, 3);
        assert.equal(row.read_done, true);
        assert.equal(row.eof_before_interruption, true);
        assert.equal(row.length_matches_before_binding, true);
        assert(
          row.read_done_order < row.reader_cancel_order &&
            row.reader_cancel_order < row.release_order &&
            row.release_order < row.stream_cancel_order,
        );
        assert(
          !JSON.stringify(f.window.__workNativeDiagnostic.snapshot()).includes(
            "PRIVATE-MATERIAL",
          ),
        );
      } finally {
        f.finish();
      }
      assert.equal(f.window.fetch, f.fetch);
      assert.equal(f.reader.read, read);
      assert.equal(f.reader.cancel, cancel);
      assert.equal(f.reader.releaseLock, release);
      assert.equal(f.stream.getReader, getReader);
      assert.equal(f.stream.cancel, streamCancel);
    },
  );
  for (const [name, headers] of [
    ["mismatched", { "Content-Length": "4" }],
    ["invalid", { "Content-Length": "3x" }],
    ["compressed", { "Content-Encoding": "gzip" }],
  ])
    await check("length never upgrades " + name, async () => {
      const f = native({ headers });
      try {
        assert.equal((await consume(f)).length_matches_before_binding, false);
      } finally {
        f.finish();
      }
    });
  await check(
    "cancel-induced done cannot prove EOF even with exact byte count",
    async () => {
      const f = native();
      try {
        await f.window.fetch(endpoint);
        const reader = f.stream.getReader();
        await reader.read();
        await reader.cancel();
        await reader.read();
        await tick();
        assert.equal(f.row().read_done, true);
        assert.equal(f.row().cancel_before_eof, true);
        assert.equal(f.row().eof_before_interruption, false);
        assert.equal(f.row().length_matches_before_binding, false);
      } finally {
        f.finish();
      }
    },
  );
  await check(
    "original signal abort before EOF cannot establish comparable length",
    async () => {
      const f = native(),
        controller = new AbortController();
      try {
        await f.window.fetch(endpoint, { signal: controller.signal });
        const reader = f.stream.getReader();
        await reader.read();
        controller.abort();
        await reader.read();
        await tick();
        assert.equal(f.row().abort_events, 1);
        assert.equal(f.row().eof_before_interruption, false);
      } finally {
        f.finish();
      }
    },
  );
  for (const mode of ["read", "cancel", "streamCancel"])
    await check(
      "original rejected " + mode + " remains rejected and observed",
      async () => {
        const options = { [mode]: () => Promise.reject(Error("original")) },
          f = native(options);
        try {
          await f.window.fetch(endpoint);
          const reader = f.stream.getReader();
          await assert.rejects(
            mode === "read"
              ? reader.read()
              : mode === "cancel"
                ? reader.cancel()
                : f.stream.cancel(),
          );
          await tick();
          assert.notEqual(f.row().failure, "none");
        } finally {
          f.finish();
        }
      },
    );
  for (const mode of ["getReaderThrows", "releaseThrows"])
    await check("original synchronous " + mode + " is not masked", async () => {
      const f = native({ [mode]: true });
      try {
        await f.window.fetch(endpoint);
        assert.throws(
          () =>
            mode === "getReaderThrows"
              ? f.stream.getReader()
              : f.stream.getReader().releaseLock(),
          /original-/,
        );
        assert.notEqual(f.row().failure, "none");
      } finally {
        f.finish();
      }
    });
  await check(
    "retirement preserves explicit missing original tail and late completion cannot change snapshot",
    async () => {
      let done;
      const f = native({
        read: () =>
          new Promise((resolve) => {
            done = resolve;
          }),
      });
      await f.window.fetch(endpoint);
      await tick();
      const read = f.stream.getReader().read();
      const end = f.finish();
      assert.equal(end.retired, true);
      assert.equal(end.pending_observations, 1);
      done({ done: true });
      await read;
      await tick();
      assert.equal(end.requests[0].read_done, false);
      assert.equal(f.row().read_done, false);
    },
  );
  await check(
    "unexpected URL passes through once and observer ownership conflict is explicit",
    async () => {
      const f = native();
      try {
        assert.equal(f.window.fetch("/api/v1/session"), f.fetchPromise);
        await tick();
        assert.equal(f.count(), 1);
        assert.equal(
          f.window.__workNativeDiagnostic.snapshot().requests.length,
          0,
        );
        const other = () => Promise.resolve(f.response);
        f.window.fetch = other;
        const end = f.finish();
        assert.equal(end.observer_failed, true);
        assert.equal(f.window.fetch, other);
      } finally {
        f.finish();
      }
    },
  );
  const publicationCode = source(
    publicationPath,
    "installWorkPublicationDiagnostic",
  );
  async function publication(overrides = {}) {
    const identity = { userID: project, sessionID: target, epoch: 1 };
    let resolve,
      calls = 0,
      disconnected = false;
    const promise = new Promise((r) => {
      resolve = r;
    });
    const auth = {
      state: { phase: "authenticated", busy: false },
      personalContext: { identity },
      workPlanning: {
        progress: { domain: "task", projectID: project, targetID: target },
      },
    };
    for (const method of [
      "getMilestone",
      "getTask",
      "getSprint",
      "checkOriginal",
      "retryOriginal",
    ])
      auth.workPlanning[method] = function () {
        calls++;
        return promise;
      };
    const original = auth.workPlanning.getTask;
    const window = {};
    const context = environment({
      window,
      performance: { now: () => 1, getEntriesByName: () => [1] },
      document: { body: {}, querySelector: () => null },
      MutationObserver: class {
        observe() {}
        disconnect() {
          disconnected = true;
        }
      },
      Function: function () {
        return async () => ({ singleton: () => auth });
      },
      ...overrides,
    });
    vm.runInContext(publicationCode, context);
    const status = await context.subject({
      binding: {
        entry: "/assets/entry.js",
        asset: "/assets/controller.js",
        export_name: "singleton",
      },
      expiresAt: Date.now() + 45_000,
    });
    return {
      status,
      window,
      auth,
      promise,
      resolve,
      original,
      calls: () => calls,
      disconnected: () => disconnected,
    };
  }
  await check(
    "public facade preserves original Promise and uniquely binds the original target",
    async () => {
      const f = await publication();
      assert.equal(f.status, "installed");
      try {
        const p = f.auth.workPlanning.getTask(project, target);
        assert.equal(p, f.promise);
        assert.equal(f.calls(), 1);
        assert.equal(
          f.window.__workPublicationDiagnostic.bindNative("GET", endpoint, 1),
          1,
        );
        f.resolve({ id: target });
        await p;
        await tick();
        const row = f.window.__workPublicationDiagnostic.snapshot().calls[0];
        assert.equal(row.fulfilled, 1);
        assert.equal(row.native_requests, 1);
        assert.equal(row.result_kind, "typed-detail-returned");
      } finally {
        f.window.__workPublicationDiagnostic.finish();
      }
      assert.equal(f.auth.workPlanning.getTask, f.original);
      assert.equal(f.disconnected(), true);
    },
  );
  await check(
    "wrong target, changed identity and simultaneous public calls cannot bind",
    async () => {
      const f = await publication();
      try {
        f.auth.workPlanning.getTask(project, target);
        assert.equal(
          f.window.__workPublicationDiagnostic.bindNative("POST", endpoint, 1),
          null,
        );
        f.auth.personalContext.identity = {
          userID: target,
          sessionID: target,
          epoch: 2,
        };
        assert.equal(
          f.window.__workPublicationDiagnostic.bindNative("GET", endpoint, 1),
          null,
        );
        f.auth.personalContext.identity = {
          userID: project,
          sessionID: target,
          epoch: 1,
        };
        f.auth.workPlanning.getTask(project, target);
        assert.equal(
          f.window.__workPublicationDiagnostic.bindNative("GET", endpoint, 1),
          null,
        );
        f.resolve({ id: target });
        await tick();
      } finally {
        f.window.__workPublicationDiagnostic.finish();
      }
    },
  );
  await check(
    "assets not already loaded prevent dynamic import or hooks",
    async () => {
      let invoked = 0;
      const f = await publication({
        performance: { now: () => 1, getEntriesByName: () => [] },
        Function: function () {
          invoked++;
          throw Error("unexpected import");
        },
      });
      assert.equal(f.status, "assets-unobserved");
      assert.equal(invoked, 0);
      assert.equal(f.window.__workPublicationDiagnostic, undefined);
    },
  );
  const moduleCode = ts.transpileModule(
    fs.readFileSync(publicationPath, "utf8"),
    {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.CommonJS,
      },
    },
  ).outputText;
  const context = vm.createContext({ require, exports: {} });
  vm.runInContext(moduleCode, context);
  await check(
    "actual current private dist identifies one already-loaded Session singleton",
    async () => {
      const binding = await context.exports.workSessionBinding(process.cwd());
      assert(
        binding.asset.startsWith("/assets/") &&
          binding.entry.startsWith("/assets/") &&
          binding.export_name,
      );
    },
  );
  await tick();
  assert.equal(unhandled, 0);
  console.log(
    JSON.stringify({ passed: passed.length, unhandled, controls: passed }),
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
