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
const lockedFunctions = new Map();
function source(file, name) {
  if (name.startsWith("installWork")) {
    if (!lockedFunctions.has(file)) {
      const root = path.resolve(
        "tests/account-captcha-web/node_modules/playwright",
      );
      assert.equal(require(root + "/package.json").version, "1.56.1");
      const { transformHook } = require(root + "/lib/transform/transform.js");
      const code = transformHook(
        fs.readFileSync(file, "utf8"),
        path.resolve(file),
      ).code;
      const scope = vm.createContext({
        exports: {},
        require: (id) => (id.startsWith("./") ? {} : require(id)),
      });
      vm.runInContext(code, scope);
      lockedFunctions.set(file, scope.exports);
    }
    // This is exactly the function Playwright serializes for addInitScript/evaluate.
    return "this.subject=" + lockedFunctions.get(file)[name].toString() + ";";
  }
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
  const context = environment({ window, ...options.environment });
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
      reject,
      calls = 0,
      disconnected = false;
    const promise = new Promise((r, j) => {
      resolve = r;
      reject = j;
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
        if (overrides.originalThrows) throw overrides.originalThrows;
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
      reject,
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

  await check(
    "public rejection and synchronous throw remain original and never become typed success",
    async () => {
      const f = await publication();
      const p = f.auth.workPlanning.getTask(project, target),
        failure = Error("PRIVATE-MATERIAL-SENTINEL");
      assert.equal(p, f.promise);
      f.reject(failure);
      await assert.rejects(p, (error) => error === failure);
      await tick();
      const end = f.window.__workPublicationDiagnostic.finish();
      assert.equal(end.calls[0].rejected, 1);
      assert.equal(end.calls[0].fulfilled, 0);
      assert(!JSON.stringify(end).includes("PRIVATE-MATERIAL-SENTINEL"));
      const g = await publication({ originalThrows: failure });
      assert.throws(
        () => g.auth.workPlanning.getTask(project, target),
        (error) => error === failure,
      );
      const thrown = g.window.__workPublicationDiagnostic.finish();
      assert.equal(thrown.calls[0].synchronous_throws, 1);
      assert.equal(thrown.pending_observations, 0);
    },
  );
  await check(
    "pre-existing DOM is recorded as observation rather than attributed publication",
    async () => {
      const f = await publication({
        document: {
          body: {},
          querySelector: (selector) =>
            selector.includes("当前读取内容")
              ? { textContent: target + "PRIVATE-MATERIAL-SENTINEL" }
              : { querySelector: () => ({ textContent: "原命令已确认" }) },
        },
      });
      const p = f.auth.workPlanning.getTask(project, target);
      f.resolve({ id: target });
      await p;
      await tick();
      const end = f.window.__workPublicationDiagnostic.finish(),
        row = end.calls[0];
      assert.equal(row.entry_detail_target_present, true);
      assert.equal(row.entry_recovery_confirmed, true);
      assert.equal(row.detail_target_present, true);
      assert(!JSON.stringify(end).includes("PRIVATE-MATERIAL-SENTINEL"));
      assert.equal(row.first_detail_after_fulfilled, undefined);
    },
  );
  await check(
    "publication setup failure restores prior hooks and held original Promise stays explicit on retirement",
    async () => {
      const f = await publication({
        MutationObserver: class {
          observe() {
            throw Error("setup");
          }
          disconnect() {}
        },
      });
      assert.equal(f.status, "observer-unavailable");
      assert.equal(f.auth.workPlanning.getTask, f.original);
      const g = await publication();
      const p = g.auth.workPlanning.getTask(project, target);
      const end = g.window.__workPublicationDiagnostic.finish();
      assert.equal(end.pending_observations, 1);
      assert.equal(end.calls[0].fulfilled, 0);
      g.resolve({ id: target });
      await p;
      await tick();
      assert.equal(
        g.window.__workPublicationDiagnostic.snapshot().calls[0].fulfilled,
        0,
      );
    },
  );
  await check(
    "absolute expiry restores hooks without canceling or consuming the original reader",
    async () => {
      let expire;
      const f = native({
        environment: {
          setTimeout: (fn) => {
            expire = fn;
            return 1;
          },
          clearTimeout() {},
        },
      });
      await f.window.fetch(endpoint);
      const reader = f.stream.getReader();
      expire();
      const end = f.finish();
      assert.equal(end.retired, true);
      assert.equal(end.requests[0].read_calls, 0);
      assert.equal(end.requests[0].reader_cancel_calls, 0);
      assert.equal(f.window.fetch, f.fetch);
      reader.releaseLock();
    },
  );
  await check(
    "read-only reader methods do not turn successful getReader into observer failure thrown to caller",
    async () => {
      const f = native();
      Object.defineProperty(f.reader, "read", {
        value: f.reader.read,
        writable: false,
        configurable: false,
      });
      await f.window.fetch(endpoint);
      assert.equal(f.stream.getReader(), f.reader);
      assert.equal(f.finish().observer_failed, true);
    },
  );

  for (const layer of ["native", "publication"]) {
    for (const trigger of [
      "expiry",
      "overdue-before-timer",
      "pending-before-late-settlement",
    ]) {
      await check(
        layer + " first retirement cannot be upgraded: " + trigger,
        async () => {
          let expiry,
            now = Date.now();
          class Clock extends Date {
            static now() {
              return now;
            }
          }
          const timers = {
            Date: Clock,
            setTimeout(fn) {
              expiry = fn;
              return 1;
            },
            clearTimeout() {},
          };
          const held = deferred();
          let finish, snapshot, settle;
          if (layer === "native") {
            const f = native({ environment: timers, read: () => held.promise });
            await f.window.fetch(endpoint);
            await tick();
            if (trigger === "pending-before-late-settlement")
              f.stream.getReader().read();
            finish = f.finish;
            snapshot = () => f.window.__workNativeDiagnostic.snapshot();
            settle = () => held.resolve({ done: true });
          } else {
            const f = await publication(timers);
            if (trigger === "pending-before-late-settlement")
              f.auth.workPlanning.getTask(project, target);
            finish = () => f.window.__workPublicationDiagnostic.finish();
            snapshot = () => f.window.__workPublicationDiagnostic.snapshot();
            settle = () => f.resolve({ id: target });
          }
          if (trigger === "expiry") expiry();
          if (trigger === "overdue-before-timer") now += 60_000;
          const first = finish();
          assert.equal(
            first.retirement_reason,
            trigger === "pending-before-late-settlement"
              ? "explicit"
              : "expired",
          );
          if (trigger === "pending-before-late-settlement")
            assert(first.pending_at_retirement > 0);
          settle();
          await tick();
          const second = finish();
          assert.equal(second.retirement_reason, first.retirement_reason);
          assert.equal(
            second.pending_at_retirement,
            first.pending_at_retirement,
          );
          assert.equal(snapshot().pending_observations, 0);
        },
      );
    }
  }
  await check(
    "Structure Lookup binds its original public call and refuses another returned domain",
    async () => {
      for (const returnedDomain of ["structure", "task"]) {
        const f = await publication();
        f.auth.workPlanning.progress = {
          domain: "structure",
          projectID: project,
          targetID: target,
        };
        const promise = f.auth.workPlanning.checkOriginal();
        assert.equal(promise, f.promise);
        assert.equal(
          f.window.__workPublicationDiagnostic.bindNative(
            "POST",
            `/api/v1/projects/${project}/structure-commands/lookup`,
            1,
          ),
          1,
        );
        f.resolve({
          domain: returnedDomain,
          value:
            returnedDomain === "structure"
              ? { state: "in_progress", result: null }
              : { status: "in_progress", receipt: null },
        });
        await promise;
        await tick();
        const end = f.window.__workPublicationDiagnostic.finish();
        assert.equal(
          end.calls[0].result_kind,
          returnedDomain === "structure" ? "in_progress" : "other-returned",
        );
        assert.equal(end.pending_at_retirement, 0);
        assert.equal(end.retirement_reason, "explicit");
        assert.equal(f.calls(), 1);
      }
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

  // Exercise the actual Node event/sampler implementation without a browser or socket.
  const { EventEmitter } = require("node:events");
  function deferred() {
    let resolve, reject;
    const promise = new Promise((a, b) => {
      resolve = a;
      reject = b;
    });
    return { promise, resolve, reject };
  }
  async function nodeDiagnostic(options = {}) {
    const page = new EventEmitter(),
      contextEvents = new EventEmitter();
    let writes = [],
      evaluations = 0,
      active = 0,
      maximum = 0,
      handlers = [];
    const f = native();
    await consume(f);
    const nativeSnapshot = f.finish();
    let snapshot = { native: nativeSnapshot, publication: null };
    const timers = new Map();
    let timerID = 0;
    const set = (fn, delay) => {
      const id = ++timerID;
      timers.set(id, { fn, delay });
      return id;
    };
    const clear = (id) => timers.delete(id);
    page.context = () => contextEvents;
    page.addInitScript = async () => {};
    page.evaluate = async (fn) => {
      evaluations++;
      active++;
      maximum = Math.max(maximum, active);
      try {
        if (handlers.length) return await handlers.shift()(fn);
        return snapshot;
      } finally {
        active--;
      }
    };
    if (options.hold) handlers.push(() => options.hold.promise);
    const env = environment({
      Buffer,
      workSessionBinding: async () => ({}),
      installWorkNativeDiagnostic() {},
      installWorkPublicationDiagnostic() {},
      writeFileSync: (_file, value) => writes.push(value),
      join: path.join,
      setTimeout: set,
      clearTimeout: clear,
    });
    vm.runInContext(source(nativePath, "startWorkNativeDiagnostic"), env);
    const diagnostic = await env.subject(page, {
      projects: [project],
      evidence: "unused",
      repository: process.cwd(),
      classify: () => null,
    });
    await tick();
    const request = (id = requestID, route = endpoint) => {
      const req = {
        url: () => "http://127.0.0.1:1" + route,
        method: () => "GET",
      };
      page.emit("request", req);
      page.emit("response", {
        request: () => req,
        headers: () => ({ "x-request-id": id }),
        status: () => 200,
      });
      return req;
    };
    return {
      page,
      contextEvents,
      diagnostic,
      request,
      handlers,
      snapshot,
      replace(value) {
        snapshot = value;
      },
      evaluations: () => evaluations,
      maximum: () => maximum,
      data: () => JSON.parse(writes.at(-1)),
      writes: () => writes,
      async fire() {
        const next = timers.entries().next().value;
        assert(next, "owned timer exists");
        timers.delete(next[0]);
        next[1].fn();
        await tick();
      },
      timers: () => timers.size,
      assertRetired() {
        for (const name of [
          "request",
          "response",
          "requestfailed",
          "requestfinished",
          "close",
        ])
          assert.equal(page.listenerCount(name), 0);
        assert.equal(contextEvents.listenerCount("close"), 0);
        assert.equal(timers.size, 0);
      },
    };
  }
  await check(
    "same original Request binding and samples continue without waiting for ordinary finished",
    async () => {
      const f = await nodeDiagnostic();
      const req = f.request();
      f.page.emit("requestfailed", req);
      await f.fire();
      await f.fire();
      assert(f.evaluations() >= 3);
      await f.diagnostic.finish();
      f.assertRetired();
      const row = f.data().documents[0].native.requests[0];
      assert.equal(row.bound_original_request, true);
      assert.equal(row.content_length_matches_eof, true);
      assert.equal(f.data().requests[0].finished_event_at, null);
      assert.notEqual(f.data().requests[0].failed_at, null);
      assert.equal(f.data().ordinary_finished_gate_unchanged, true);
      assert.equal(f.maximum(), 1);
    },
  );
  for (const variant of [
    "missing-id",
    "duplicate-pw-id",
    "duplicate-native-id",
    "wrong-target",
    "duplicate-document-id",
  ])
    await check("native binding rejects " + variant, async () => {
      const f = await nodeDiagnostic();
      f.request(
        variant === "missing-id" ? null : requestID,
        variant === "wrong-target"
          ? endpoint.replace("tasks", "sprints")
          : endpoint,
      );
      if (variant === "duplicate-pw-id") f.request();
      if (variant === "duplicate-native-id")
        f.snapshot.native.requests.push({
          ...f.snapshot.native.requests[0],
          sequence: 2,
        });
      if (variant === "duplicate-document-id") {
        await f.diagnostic.flush();
        f.replace({
          native: {
            ...f.snapshot.native,
            document_id: require("node:crypto").randomUUID(),
          },
          publication: null,
        });
      }
      await f.diagnostic.finish();
      f.assertRetired();
      for (const doc of f.data().documents)
        for (const row of doc.native.requests)
          assert.equal(row.bound_original_request, false);
    });
  await check(
    "hung evaluate never overlaps and late post-close completion cannot upgrade saved evidence",
    async () => {
      const held = deferred(),
        f = await nodeDiagnostic({ hold: held });
      assert.equal(f.evaluations(), 1);
      assert.equal(f.timers(), 0);
      const finish = f.diagnostic.finish();
      await tick();
      await f.fire();
      await finish;
      f.assertRetired();
      assert.equal(f.data().sample_joined, false);
      assert.equal(f.data().end_snapshot_observed, false);
      const saved = f.writes().at(-1);
      f.page.emit("close");
      held.resolve(f.snapshot);
      await tick();
      assert.equal(f.maximum(), 1);
      assert.equal(f.writes().at(-1), saved);
      assert.equal(f.data().documents.length, 0);
    },
  );
  await check(
    "late end evaluate cannot upgrade missing end and page close precludes new end evaluation",
    async () => {
      const f = await nodeDiagnostic(),
        held = deferred();
      f.handlers.push(() => held.promise);
      const finish = f.diagnostic.finish();
      await tick();
      await f.fire();
      await finish;
      f.assertRetired();
      const saved = f.writes().at(-1);
      assert.equal(f.data().end_snapshot_observed, false);
      f.page.emit("close");
      held.resolve(f.snapshot);
      await tick();
      assert.equal(f.writes().at(-1), saved);
      const g = await nodeDiagnostic();
      g.page.emit("close");
      const before = g.evaluations();
      await g.diagnostic.finish();
      g.assertRetired();
      assert.equal(g.evaluations(), before);
      assert.equal(g.data().end_snapshot_observed, false);
      assert.equal(g.data().page_closed, true);
    },
  );
  await check(
    "navigation obtains actual document hook retirement and keeps distinct document evidence",
    async () => {
      const f = await nodeDiagnostic();
      f.request();
      await f.diagnostic.flush();
      assert.equal(f.data().documents[0].end_snapshot_observed, true);
      f.replace({
        native: {
          ...f.snapshot.native,
          document_id: require("node:crypto").randomUUID(),
          requests: [],
        },
        publication: null,
      });
      await f.diagnostic.installPublication();
      await f.fire();
      await f.diagnostic.finish();
      f.assertRetired();
      assert.equal(f.data().documents.length, 2);
      assert.equal(f.maximum(), 1);
    },
  );
  await check(
    "public call candidates bind only one native request in the same document",
    async () => {
      for (const count of [1, 2]) {
        const f = await nodeDiagnostic();
        f.request();
        const row = f.snapshot.native.requests[0];
        row.call_id = 1;
        f.snapshot.publication = {
          retired: true,
          retirement_reason: "explicit",
          pending_at_retirement: 0,
          observer_failed: false,
          overflow: false,
          pending_observations: 0,
          calls: [
            {
              call_id: 1,
              native_requests: count,
              native_sequence: count === 1 ? row.sequence : null,
              method: "GET",
              path: endpoint,
              target_id: target,
              operation: "getTask",
              result_kind: "typed-detail-returned",
              entry_identity_matches: true,
            },
          ],
        };
        await f.diagnostic.finish();
        f.assertRetired();
        assert.equal(
          f.data().documents[0].native.requests[0].bound_public_call,
          count === 1,
        );
      }
    },
  );
  await check(
    "closed projection drops secret extras and rejects unknown paths or string-valued counters",
    async () => {
      const f = await nodeDiagnostic();
      f.snapshot.native.secret = "PRIVATE-MATERIAL-SENTINEL";
      f.snapshot.native.requests[0].body = "PRIVATE-MATERIAL-SENTINEL";
      await f.diagnostic.finish();
      f.assertRetired();
      assert(!f.writes().at(-1).includes("PRIVATE-MATERIAL-SENTINEL"));
      for (const field of ["path", "bytes"]) {
        const g = await nodeDiagnostic();
        g.snapshot.native.requests[0][field] = "PRIVATE-MATERIAL-SENTINEL";
        await g.diagnostic.finish();
        g.assertRetired();
        assert.equal(g.data().projection_rejected, 1);
        assert(!g.writes().at(-1).includes("PRIVATE-MATERIAL-SENTINEL"));
      }
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
