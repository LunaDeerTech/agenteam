// Offline method controls. Browser transport/Session are explicitly controlled
// halves; the real-case observer source and native Stream operations are used.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { EventEmitter } = require("node:events");
const { webcrypto, createHash } = require("node:crypto");
const root = path.resolve(__dirname, "../..");
const ts = require(path.join(root, "web/node_modules/typescript"));
const file = path.join(
  root,
  "tests/account-captcha-web/e2e/skill-owner-read.native.ts",
);
const code = ts.transpileModule(fs.readFileSync(file, "utf8"), {
  compilerOptions: {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.CommonJS,
  },
}).outputText;
const project = "01999f20-1111-7000-8000-000000000001",
  skill = "01999f20-1111-7000-8000-000000000002";
const xid = "01999f20-1111-7000-8000-000000000003",
  prefix = `/api/v1/projects/${project}/skills`;
const value = Object.freeze({
  id: skill,
  project_id: project,
  name: "Add Skills",
  normalized_name: "add-skills",
  description: "Controlled fixture text",
  protected: true,
  current_revision: "1",
  version: "1",
});
const directory = Object.freeze({ items: Object.freeze([value]) });
const bytes = new TextEncoder().encode(JSON.stringify(directory));
const digest = createHash("sha256").update(bytes).digest("hex");
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
let cases = 0,
  unhandled = 0;
process.on("unhandledRejection", () => unhandled++);
async function scenario({
  terminal = "finished",
  duplicate = false,
  hold = "",
  wrongIdentity = false,
  voidResult = false,
} = {}) {
  const blocker = deferred();
  let nativeFetchPromise, originalPublicPromise;
  const stream = new ReadableStream({
    start(c) {
      c.enqueue(bytes);
      c.close();
    },
  });
  if (hold === "outer") {
    const original = stream.cancel;
    stream.cancel = function (...args) {
      const p = Reflect.apply(original, this, args);
      return p.then(() => blocker.promise);
    };
  }
  const response = new Response(stream, {
    status: 200,
    headers: { "content-length": String(bytes.length), "x-request-id": xid },
  });
  const auth = {
    state: {
      phase: "authenticated",
      user: { id: "owner", role: "user" },
      busy: false,
    },
    personalContext: {
      phase: "current",
      identity: { userID: "owner", sessionID: "session", epoch: 1 },
    },
    skills: {},
  };
  const box = {
    exports: {},
    URL,
    URLSearchParams,
    Request,
    Response,
    ReadableStream,
    Uint8Array,
    crypto: webcrypto,
    location: { origin: "http://127.0.0.1:31000" },
    setTimeout,
    clearTimeout,
    console,
    require(name) {
      if (name === "./knowledge-owner-read.native")
        return {
          knowledgeSessionBinding: async () => ({
            asset: "actual-loaded-singleton",
            exportName: "useSession",
            entry: "entry",
          }),
        };
      if (name === "actual-loaded-singleton") return { useSession: () => auth };
      throw Error("CONTROL_IMPORT");
    },
  };
  box.window = box;
  box.fetch = function () {
    nativeFetchPromise = Promise.resolve(response);
    return nativeFetchPromise;
  };
  vm.createContext(box);
  vm.runInContext(code, box, { filename: file });
  auth.skills.list = function () {
    auth.state.busy = true;
    originalPublicPromise = (async () => {
      const promise = box.fetch(prefix);
      assert.equal(promise, nativeFetchPromise);
      const r = await promise,
        reader = r.body.getReader();
      try {
        if (hold === "reader") await blocker.promise;
        for (;;) {
          const next = await reader.read();
          if (next.done) break;
        }
      } finally {
        await reader.cancel();
        reader.releaseLock();
        await r.body.cancel();
      }
      if (wrongIdentity)
        auth.personalContext.identity = {
          ...auth.personalContext.identity,
          epoch: 2,
        };
      return voidResult ? undefined : directory;
    })().finally(() => {
      auth.state.busy = false;
    });
    return originalPublicPromise;
  };
  auth.skills.get = function () {
    throw Error("NOT_SELECTED");
  };
  const page = new EventEmitter(),
    context = new EventEmitter();
  let closed = false;
  page.url = () => "http://127.0.0.1:31000/owner/project/settings/general";
  page.context = () => context;
  page.isClosed = () => closed;
  page.evaluate = async (fn, arg) => fn(arg);
  page.close = async () => {
    closed = true;
    page.emit("close");
  };
  const observer = await box.exports.observeSkills(page, {
    root,
    dist: "/unused",
    project,
    skill,
    user: "owner",
    expiresAt: Date.now() + 3000,
  });
  const request = {
    url: () => "http://127.0.0.1:31000" + prefix,
    method: () => "GET",
    postData: () => null,
  };
  let finishedCalls = 0;
  const pw = {
    request: () => request,
    status: () => 200,
    headerValue: async () => xid,
    finished: () => {
      finishedCalls++;
      return Promise.resolve(null);
    },
  };
  page.emit("request", request);
  const publicPromise = auth.skills.list(project);
  assert.equal(
    publicPromise,
    originalPublicPromise,
    "wrapper must return original public Promise",
  );
  page.emit("response", pw);
  page.emit("request" + terminal, request);
  if (duplicate) page.emit("request" + terminal, request);
  let terminalReport;
  if (hold) {
    // Wait for the actual controlled call to be pending. Retire it at that
    // point; a later release joins the operation but cannot rescue success.
    for (let i = 0; i < 20; i++) await Promise.resolve();
    const finish = observer.finish();
    for (let i = 0; i < 20; i++) await Promise.resolve();
    blocker.resolve();
    await publicPromise;
    terminalReport = await finish;
  } else {
    await publicPromise;
    // Digest is an actual async crypto operation; wait its original observer.
    while (!((await observer.snapshot()).native?.pending === 0))
      await new Promise(setImmediate);
    terminalReport = await observer.finish();
  }
  const report = terminalReport;
  const sidecar = {
    method: "GET",
    request_id: xid,
    path: prefix,
    query: "",
    status: 200,
    content_length: bytes.length,
    body_sha256: digest,
  };
  const accepted =
    box.exports.skillOriginalCompleted(
      report.native,
      report.publication,
      report.requests[0],
      sidecar,
    ) &&
    !report.pw_failed &&
    report.pw_pending === 0;
  assert.equal(
    accepted,
    terminal === "finished" &&
      !duplicate &&
      !hold &&
      !wrongIdentity &&
      !voidResult,
  );
  assert.equal(finishedCalls, terminal === "finished" && !duplicate ? 1 : 0);
  assert.equal(report.pw_pending, 0, "original Node tails actually joined");
  assert.equal(
    report.native.pending,
    0,
    "original native tails actually joined",
  );
  assert.equal(
    report.publication.pending,
    0,
    "original Session observation tail joined",
  );
  cases++;
}
(async () => {
  await scenario();
  await scenario({ terminal: "failed" });
  await scenario({ duplicate: true });
  await scenario({ hold: "reader" });
  await scenario({ hold: "outer" });
  await scenario({ wrongIdentity: true });
  await scenario({ voidResult: true });
  await new Promise(setImmediate);
  assert.equal(unhandled, 0);
  console.log(
    `Skill native controls: ${cases} PASS; 0 unhandled (controlled transport/Session, no browser/socket)`,
  );
})().catch(() => {
  console.error("SKILL_NATIVE_CONTROL_FAILED");
  process.exitCode = 1;
});
