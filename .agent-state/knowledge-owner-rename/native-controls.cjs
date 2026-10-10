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

const { EventEmitter } = require("node:events");
const crypto = require("node:crypto");
const { createAccountAPI } = source(path.join(root, "web/src/api/account.ts"));
const { createKnowledgeCommandsAPI } = source(
  path.join(root, "web/src/api/knowledge-commands.ts"),
);
const { createSessionController } = source(
  path.join(root, "web/src/composables/useSession.ts"),
);
const esm = (file) =>
  ts.transpileModule(fs.readFileSync(file, "utf8"), {
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.ESNext,
    },
  }).outputText;
const dataURL = (code) =>
  "data:text/javascript;base64," + Buffer.from(code).toString("base64");
const readModule = dataURL(
  esm(
    path.join(
      root,
      "tests/account-captcha-web/e2e/knowledge-owner-read.native.ts",
    ),
  ),
);
const nativeCode = esm(
  path.join(
    root,
    "tests/account-captcha-web/e2e/knowledge-owner-rename.native.ts",
  ),
).replace('"./knowledge-owner-read.native"', JSON.stringify(readModule));
const id = (n) => `01970000-0000-7000-8000-${n.toString(16).padStart(12, "0")}`;
const project = id(10),
  target = id(20),
  user = id(1),
  requestID = id(99),
  title = "新标题：中文🙂",
  time = "2026-10-10T12:00:00.123456Z";
const doc = {
  id: target,
  project_id: project,
  parent_document_id: null,
  title,
  content_version: "2",
  source_kind: "text",
  media_type: "text/plain",
  status: "active",
  indexing_status: "pending",
  created_by: { kind: "human", user_id: user },
  created_at: time,
  updated_at: time,
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
const body = { document: doc },
  bytes = new TextEncoder().encode(JSON.stringify(body));
const requestPath = `/api/v1/projects/${project}/knowledge/documents/${target}/rename`;
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
const wait = async (predicate) => {
  const end = Date.now() + 1500;
  while (Date.now() < end) {
    if (predicate()) return;
    await tick();
  }
  throw Error("RENAME_CONTROL_NOT_SETTLED");
};
global.window = global;
global.location = new URL("http://127.0.0.1:12345");
global.document = { documentElement: { dataset: {} } };
let checks = 0,
  unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
(async () => {
  const methods = await import(dataURL(nativeCode));
  const dist = path.join(root, "output/ai/knowledge-owner-rename/dist");
  const modes = [
    "normal",
    "failed-post",
    "wrong-body",
    "duplicate-response",
    "reader-held",
    "outer-held",
    "reader-reject",
    "outer-reject",
    "void-public",
    "held-first-retirement",
    "held-pw-first-retirement",
  ];
  for (const mode of modes) {
    const evidence = fs.mkdtempSync(
      path.join(require("node:os").tmpdir(), "kr-control-"),
    );
    const page = new EventEmitter(),
      context = new EventEmitter();
    const gate = deferred(),
      entered = deferred();
    let headerCalls = 0,
      finishedCalls = 0,
      init;
    const request = {
      url: () => location.origin + requestPath,
      method: () => "POST",
      postData: () => (mode === "wrong-body" ? "{}" : init.body),
      headers: () => ({
        ...Object.fromEntries(new Headers(init.headers)),
        origin: location.origin,
      }),
      failure: () => ({ errorText: "net::ERR_ABORTED" }),
    };
    const pwResponse = {
      request: () => request,
      status: () => 200,
      headerValue(name) {
        assert.equal(this, pwResponse);
        assert.equal(name, "x-request-id");
        headerCalls++;
        return Promise.resolve(requestID);
      },
      finished() {
        assert.equal(this, pwResponse);
        finishedCalls++;
        if (mode === "held-pw-first-retirement") {
          entered.resolve();
          return gate.promise.then(() => null);
        }
        return Promise.resolve(null);
      },
      body() {
        throw Error("NO_SECOND_BODY");
      },
    };
    global.fetch = (input, options) => {
      if (input === "/api/v1/session") return Promise.resolve(json(session));
      if (input === "/api/v1/auth/bootstrap")
        return Promise.resolve(
          json({
            csrf_token: "A".repeat(43),
            challenge_modes: ["rotate"],
            delivery_channel: "backend_log",
          }),
        );
      assert.equal(input, requestPath);
      init = options;
      page.emit("request", request);
      queueMicrotask(() => {
        page.emit("response", pwResponse);
        if (mode === "duplicate-response") page.emit("response", pwResponse);
        page.emit(
          mode === "failed-post" ? "requestfailed" : "requestfinished",
          request,
        );
      });
      const response = new Response(bytes, {
        headers: {
          "Content-Type": "application/json",
          "Content-Length": String(bytes.length),
          "X-Request-ID": requestID,
        },
      });
      const originalGet = response.body.getReader.bind(response.body);
      response.body.getReader = function (...args) {
        const reader = originalGet(...args),
          cancel = reader.cancel;
        reader.cancel = function (...args) {
          assert.equal(this, reader);
          const p = Reflect.apply(cancel, this, args);
          if (mode === "reader-reject")
            return p.then(() => {
              throw Error("PRIVATE_CANCEL");
            });
          if (mode === "reader-held") {
            entered.resolve();
            return p.then(() => gate.promise);
          }
          return p;
        };
        return reader;
      };
      const outer = response.body.cancel;
      response.body.cancel = function (...args) {
        assert.equal(this, response.body);
        const p = Reflect.apply(outer, this, args);
        if (mode === "outer-reject")
          return p.then(() => {
            throw Error("PRIVATE_CANCEL");
          });
        if (["outer-held", "held-first-retirement"].includes(mode)) {
          entered.resolve();
          return p.then(() => gate.promise);
        }
        return p;
      };
      return Promise.resolve(response);
    };
    const transport = (...args) => window.fetch(...args),
      args = [createAccountAPI(transport)];
    args[16] = { knowledgeCommands: createKnowledgeCommandsAPI(transport) };
    const auth = createSessionController(...args);
    await auth.restore();
    global.__renameControlAuth = auth;
    let actualPublic;
    const original = auth.knowledgeCommands.startRename;
    auth.knowledgeCommands.startRename = function (...args) {
      const p = Reflect.apply(original, this, args);
      actualPublic = mode === "void-public" ? p.then(() => undefined) : p;
      return actualPublic;
    };
    page.context = () => context;
    page.isClosed = () => false;
    page.evaluate = async (fn, value) =>
      fn === methods.installRenamePublication
        ? fn({
            ...value,
            binding: {
              ...value.binding,
              exportName: "singleton",
              asset:
                "data:text/javascript,export%20function%20singleton(){return%20globalThis.__renameControlAuth}",
            },
          })
        : fn(value);
    const diagnostic = methods.createRenameDiagnostic({
      evidence,
      inputHash: "d".repeat(64),
      project,
      documents: [target],
    });
    let observer, report;
    try {
      observer = await methods.observeRename(page, {
        root,
        dist,
        project,
        documents: [target],
        user,
        title,
        evidence,
        inputHash: "d".repeat(64),
        diagnostic,
      });
      const visible = auth.knowledgeCommands.startRename(project, target, {
        expected_version: "1",
        title,
      });
      assert.equal(visible, actualPublic);
      checks++;
      const joined = visible.then(
        (value) => ({ value }),
        (error) => ({ error }),
      );
      if (
        ["reader-held", "outer-held", "held-first-retirement"].includes(mode)
      ) {
        await entered.promise;
        assert.equal(auth.state.busy, true);
        assert.equal(
          __knowledgeRenamePublication.snapshot().rows[0].fulfilled,
          false,
        );
        assert.equal(await observer.idle(), false);
        checks += 3;
        if (mode === "held-first-retirement") {
          const finish = observer.finish();
          await wait(() => __knowledgeRenameNative.snapshot().retired);
          assert(__knowledgeRenameNative.snapshot().pending_at_retirement > 0);
          gate.resolve();
          report = await finish;
          checks++;
        } else gate.resolve();
      }
      const settled = await joined;
      if (mode.endsWith("-reject")) {
        assert(settled.error);
        assert.equal(auth.knowledgeCommands.progress.phase, "uncertain");
        checks += 2;
      }
      await wait(
        () =>
          __knowledgeRenameNative.snapshot().pending === 0 &&
          __knowledgeRenamePublication.snapshot().pending === 0,
      );
      if (mode === "held-pw-first-retirement") {
        await entered.promise;
        assert.equal(await observer.idle(), true);
        let completed = false;
        const finish = observer.finish();
        finish.then(
          () => (completed = true),
          () => (completed = true),
        );
        assert.equal(observer.finish(), finish);
        await wait(
          () =>
            __knowledgeRenameNative.snapshot().retired &&
            __knowledgeRenamePublication.snapshot().retired,
        );
        assert.equal(completed, false);
        assert.equal(
          __knowledgeRenameNative.snapshot().pending_at_retirement,
          0,
        );
        assert.equal(
          __knowledgeRenamePublication.snapshot().pending_at_retirement,
          0,
        );
        gate.resolve();
        report = await finish;
        assert.equal(report.first_node.pending, 1);
        assert.equal(report.first_node.pending_terminals, 0);
        assert.equal(report.first_node.ready, false);
        assert.equal(report.pw_pending, 0);
        assert.equal(report.pw_failed, true);
        checks += 10;
      }
      report ??= await observer.finish();
      assert.equal(headerCalls, 1);
      assert.equal(
        finishedCalls,
        ["failed-post", "wrong-body", "duplicate-response"].includes(mode)
          ? 0
          : 1,
      );
      checks += 2;
      const row = report.requests[0],
        sidecar = {
          method: "POST",
          request_id: requestID,
          path: requestPath,
          query: "",
          status: 200,
          content_length: bytes.length,
          body_sha256: crypto.createHash("sha256").update(bytes).digest("hex"),
        };
      const complete =
        methods.renameOriginalCompleted(
          report.native,
          report.publication,
          row,
          sidecar,
        ) &&
        !report.pw_failed &&
        report.pw_pending === 0;
      assert.equal(
        complete,
        ["normal", "reader-held", "outer-held"].includes(mode),
        mode,
      );
      checks++;
      if (mode === "normal") {
        assert.deepEqual(report.publication.rows[0].typed, body);
        assert.equal(report.publication.rows[0].same_receipt, true);
        assert.equal(report.publication.rows[0].confirmed, true);
        checks += 3;
      }
    } finally {
      gate.resolve();
      context.emit("close");
      if (observer && !report) {
        try {
          await observer.finish();
        } catch {}
      }
      auth.leave();
      await tick();
      delete global.__knowledgeRenameNative;
      delete global.__knowledgeRenamePublication;
      delete global.__renameControlAuth;
      fs.rmSync(evidence, { recursive: true, force: true });
    }
  }
  await tick();
  assert.equal(unhandled, 0);
  console.log(
    `Rename native controls PASS modes=${modes.length} checks=${checks} unhandled=${unhandled}; actual Session/API/observer; controlled Fetch/PW`,
  );
})().catch(() => {
  console.error("RENAME_NATIVE_CONTROL_FAILED");
  process.exitCode = 1;
});
