// New Project GET seam only. Actual Session/API/Workspace + production Vue;
// PW events below are controlled, never claimed as a browser or PG execution.
const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  path = require("node:path"),
  vm = require("node:vm"),
  crypto = require("node:crypto");
const root = path.resolve(__dirname, "../.."),
  { JSDOM } = require(root + "/web/node_modules/jsdom");
const pw = root + "/tests/account-captcha-web/node_modules/playwright";
assert.equal(require(pw + "/package.json").version, "1.56.1");
const { transformHook } = require(pw + "/lib/transform/transform.js");
function exportsOf(file) {
  const scope = vm.createContext({
    exports: {},
    require: (n) => (n.startsWith("./") ? {} : require(n)),
  });
  vm.runInContext(
    transformHook(fs.readFileSync(root + "/" + file, "utf8"), root + "/" + file)
      .code,
    scope,
  );
  return scope.exports;
}
const native = exportsOf(
    "tests/account-captcha-web/e2e/project-work-planning.native.ts",
  ),
  publication = exportsOf(
    "tests/account-captcha-web/e2e/project-work-planning.publication.ts",
  );
const id = (n) => `01900000-0000-7000-8000-${String(n).padStart(12, "0")}`,
  at = "2026-10-09T10:00:00.000000Z";
const session = {
  user: {
    id: id(1),
    email: "owner@example.test",
    username: "owner",
    display_name: "",
    role: "user",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: id(2),
    issued_at: at,
    absolute_expires_at: at,
    idle_expires_at: at,
  },
  csrf_token: "S".repeat(43),
};
const project = {
  id: id(10),
  owner_user_id: id(1),
  name: "owned",
  normalized_name: "owned",
  description: "PRIVATE_PROJECT_CANARY",
  lifecycle: "active",
  version: "1",
  current_sprint_id: null,
  created_at: at,
  updated_at: at,
  archived_at: null,
};
const defer = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
const drain = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise(setImmediate);
};
const unhandled = [];
process.on("unhandledRejection", (e) => unhandled.push(e));
(async () => {
  const { build } = await import(
    root + "/web/node_modules/vite/dist/node/index.js"
  );
  const entry =
    root + "/output/ai/work-owner-planning-ui/virtual-project-completion.js";
  const bundle = await build({
    configFile: false,
    root,
    logLevel: "silent",
    plugins: [
      {
        name: "actual-project-completion",
        enforce: "pre",
        resolveId: (n) => (n === entry ? "\0actual" : undefined),
        load: (n) =>
          n === "\0actual"
            ? `export {createSessionController} from '${root}/web/src/composables/useSession.ts';export {createAccountAPI} from '${root}/web/src/api/account.ts';export {createProjectOwnerAPI} from '${root}/web/src/api/project-owner.ts';export {createWorkPlanningAPI} from '${root}/web/src/api/work-planning.ts';export {createProjectWorkspace,projectWorkspaceKey} from '${root}/web/src/composables/useProjectWorkspace.ts';export {createProjectWorkPlanning} from '${root}/web/src/composables/useProjectWorkPlanning.ts';export {createApp,provide,h} from '${root}/web/node_modules/vue/dist/vue.runtime.esm-bundler.js';`
            : undefined,
      },
    ],
    build: {
      write: false,
      minify: false,
      lib: { entry, name: "Actual", formats: ["iife"] },
    },
  });
  const code = (Array.isArray(bundle) ? bundle : [bundle])
    .flatMap((x) => x.output)
    .find((x) => x.type === "chunk").code;
  let count = 0,
    positive;
  async function check(name, fn) {
    await fn();
    count++;
    console.log("PASS", name);
  }
  function report(w) {
    const n = w.__workNativeDiagnostic.finish(),
      p = w.__workPublicationDiagnostic.finish(),
      row = n.requests[0];
    return JSON.parse(
      JSON.stringify({
        observation_finished: true,
        ordinary_finished_gate_unchanged: false,
        sample_joined: true,
        sample_join_unavailable: false,
        end_snapshot_observed: true,
        page_closed: false,
        context_closed: false,
        overflow: false,
        projection_rejected: 0,
        requests: [
          {
            sequence: 1,
            request_id: row.request_id,
            method: row.method,
            path: row.path,
            has_query: false,
            status: 200,
            declaration: null,
            request_at: 0,
            response_at: 1,
            failed_at: 2,
            finished_event_at: null,
            project_terminal: "failed",
            project_failure_aborted: true,
            project_failed_count: 1,
            project_finished_count: 0,
          },
        ],
        documents: [
          {
            source: "end",
            end_snapshot_observed: true,
            before_page_close: true,
            native: {
              ...n,
              requests: [
                {
                  ...row,
                  bound_original_request: true,
                  bound_public_call: true,
                  pw_sequence: 1,
                  declaration: null,
                  content_length_comparable:
                    row.length_comparable_before_binding,
                  content_length_matches_eof: row.length_matches_before_binding,
                },
              ],
            },
            publication: p,
          },
        ],
      }),
    );
  }
  async function setup(options = {}) {
    const dom = new JSDOM('<div id="app"></div>', {
        url:
          "https://owned.invalid/owner/owned/tasks/explore" +
          (options.withWork ? "/milestones/" + id(11) : ""),
        runScripts: "outside-only",
      }),
      w = dom.window,
      ctx = dom.getInternalVMContext();
    Object.assign(w, {
      Request,
      Response,
      Headers,
      ReadableStream,
      TextEncoder,
      TextDecoder,
      Uint8Array,
      Blob,
      AbortController,
      process: { env: { NODE_ENV: "production" } },
    });
    Object.defineProperty(w, "crypto", { value: crypto.webcrypto });
    w.performance.getEntriesByName = () => [{}];
    let sequence = 0;
    const timers = new Map();
    w.setTimeout = (fn, ms) => {
      const key = ++sequence;
      timers.set(key, { fn, ms });
      return key;
    };
    w.clearTimeout = (key) => timers.delete(key);
    let prepared = false,
      requests = 0,
      lastPublic,
      lastWorkspace,
      fetchPromise,
      workspaceAtSettlement;
    const entered = defer(),
      release = defer(),
      workEntered = defer(),
      workRelease = defer();
    w.fetch = (url, init) => {
      if (
        options.withWork &&
        url.startsWith(`/api/v1/projects/${id(10)}/milestones`)
      ) {
        const milestone = {
          id: id(11),
          project_id: id(10),
          title: "Actual Work selection",
          description: "",
          manual_rank: "8".repeat(32),
          version: "1",
          created_at: at,
          updated_at: at,
        };
        const { description, ...summary } = milestone;
        const response = new Response(
          JSON.stringify(url.includes("?") ? { items: [summary] } : milestone),
          {
            headers: {
              "Content-Type": "application/json",
              "X-Request-ID": id(92),
            },
          },
        );
        if (prepared && options.holdWork) {
          workEntered.resolve();
          return workRelease.promise.then(() => response);
        }
        return Promise.resolve(response);
      }
      if (url === "/api/v1/session")
        return Promise.resolve(
          new Response(JSON.stringify(session), {
            headers: {
              "Content-Type": "application/json",
              "X-Request-ID": id(90),
            },
          }),
        );
      if (!prepared) {
        assert.ok(url.startsWith("/api/v1/projects"));
        return Promise.resolve(
          new Response(JSON.stringify(project), {
            headers: {
              "Content-Type": "application/json",
              "X-Request-ID": id(91),
            },
          }),
        );
      }
      assert.equal(url, `/api/v1/projects/${id(10)}`);
      assert.equal(init.method, "GET");
      requests++;
      const value = {
        ...project,
        lifecycle: "archived",
        version: "2",
        archived_at: at,
        ...(options.rename
          ? { name: "renamed", normalized_name: "renamed" }
          : {}),
        ...(options.badOwner ? { owner_user_id: id(3) } : {}),
      };
      const bytes = new TextEncoder().encode(JSON.stringify(value)),
        body = new ReadableStream({
          start(c) {
            c.enqueue(bytes);
            c.close();
          },
        }),
        get = body.getReader.bind(body),
        outer = body.cancel.bind(body);
      body.getReader = (...args) => {
        const reader = get(...args),
          cancel = reader.cancel.bind(reader);
        reader.cancel = (...a) => {
          if (options.hold === "reader") entered.resolve();
          return (
            options.hold === "reader" ? release.promise : Promise.resolve()
          ).then(() => cancel(...a));
        };
        return reader;
      };
      body.cancel = (...a) => {
        if (options.hold === "outer") entered.resolve();
        return (
          options.hold === "outer" ? release.promise : Promise.resolve()
        ).then(() => outer(...a));
      };
      fetchPromise = Promise.resolve(
        new Response(body, {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "Content-Length": String(bytes.length),
            "X-Request-ID": id(100),
          },
        }),
      );
      return fetchPromise;
    };
    vm.runInContext(code, ctx);
    const A = w.Actual,
      dep = Array(16).fill(undefined),
      fetcher = (...a) => w.fetch(...a);
    dep[0] = A.createAccountAPI(fetcher);
    dep[12] = A.createProjectOwnerAPI(fetcher);
    if (options.withWork) dep[15] = A.createWorkPlanningAPI(fetcher);
    const auth = A.createSessionController(...dep);
    await auth.restore();
    let workspace;
    workspace = A.createProjectWorkspace(auth, async (route) => {
      if (options.hold === "canonical") {
        entered.resolve();
        await release.promise;
      }
      if (options.canonicalReject) throw Error("controlled route rejection");
      w.history.replaceState({}, "", route);
      workspace.afterNavigation(route);
    });
    workspace.afterNavigation(w.location.pathname);
    await drain();
    assert.equal(workspace.detail.phase, "current");
    assert.ok(workspace.currentReadContext.value);
    const work = options.withWork
      ? A.createProjectWorkPlanning(auth, workspace)
      : null;
    if (work) {
      work.afterNavigation(w.location.pathname);
      await drain();
      assert.equal(work.detail.phase, "ready");
    }
    const app = A.createApp({
      setup() {
        A.provide(A.projectWorkspaceKey, workspace);
        return () => A.h("div", "actual workspace owner");
      },
    });
    app.mount(w.document.querySelector("#app"));
    const actualPublic = auth.projects.get,
      actualWorkspace = workspace.readCurrent;
    auth.projects.get = function (...args) {
      lastPublic = Reflect.apply(actualPublic, this, args);
      return lastPublic;
    };
    workspace.readCurrent = function (...args) {
      const entry = workspace.currentReadContext.value;
      lastWorkspace = Reflect.apply(actualWorkspace, this, args);
      if (options.withWork)
        void lastWorkspace.then(() => {
          workspaceAtSettlement = {
            busy: auth.state.busy,
            blocked: workspace.blocked.value,
            context: workspace.currentReadContext.value,
            entry,
            phase: workspace.detail.phase,
            project: { ...workspace.detail.project },
          };
        });
      return lastWorkspace;
    };
    const originalPublic = auth.projects.get,
      originalWorkspace = workspace.readCurrent;
    vm.runInContext(
      `this.installNative=(${native.installWorkNativeDiagnostic.toString()});this.installPublic=(${publication.installWorkPublicationDiagnostic.toString()});`,
      ctx,
    );
    w.installNative({
      projects: [id(10)],
      expiresAt: Date.now() + 45000,
      projectRefreshCompletion: true,
    });
    w.Function = function () {
      return async () => ({ singleton: () => auth });
    };
    const installed = await w.installPublic({
      binding: {
        entry: "/assets/main.js",
        asset: "/assets/session.js",
        export_name: "singleton",
        workspace_marker: "project-workspace",
      },
      expiresAt: Date.now() + 45000,
    });
    assert.equal(installed, "installed");
    prepared = true;
    const call = () => {
      const result = workspace.readCurrent();
      assert.equal(result, lastWorkspace);
      return result;
    };
    return {
      w,
      dom,
      auth,
      workspace,
      work,
      workspaceAtSettlement: () => workspaceAtSettlement,
      A,
      timers,
      entered,
      release,
      workEntered,
      workRelease,
      call,
      requests: () => requests,
      close() {
        w.__workPublicationDiagnostic.finish();
        w.__workNativeDiagnostic.finish();
        assert.equal(workspace.readCurrent, originalWorkspace);
        assert.equal(auth.projects.get, originalPublic);
        work?.dispose();
        workspace.dispose();
        app.unmount();
        dom.window.close();
      },
    };
  }
  await check(
    "actual Work automatic reread preserves completed Project publication",
    async () => {
      const f = await setup({ withWork: true });
      await f.call();
      await drain();
      const state = f.workspaceAtSettlement();
      const call = f.w.__workPublicationDiagnostic
        .snapshot()
        .calls.find((row) => row.operation === "getProject");
      assert.equal(state.busy, true);
      assert.equal(state.blocked, true);
      assert.equal(state.phase, "current");
      assert.equal(state.project.lifecycle, "archived");
      assert.equal(state.context.generation, state.entry.generation);
      assert.equal(
        state.context.readGeneration,
        state.entry.readGeneration + 1,
      );
      try {
        assert.equal(call.workspace_published, true);
        assert.equal(
          native.workOrdinaryConsumption(report(f.w), 1, id(100), true),
          true,
        );
      } finally {
        f.close();
      }
    },
  );
  await check(
    "completed Project never bypasses a pending successor Work owner",
    async () => {
      const f = await setup({ withWork: true, holdWork: true });
      await f.call();
      await f.workEntered.promise;
      await drain();
      const call = f.w.__workPublicationDiagnostic
        .snapshot()
        .calls.find((row) => row.operation === "getProject");
      assert.equal(call.workspace_published, true);
      assert.equal(f.auth.state.busy, true);
      const incomplete = report(f.w);
      assert.ok(incomplete.documents[0].publication.pending_at_retirement > 0);
      assert.equal(
        native.workOrdinaryConsumption(incomplete, 1, id(100), true),
        false,
      );
      f.workRelease.resolve();
      await drain();
      assert.equal(f.auth.state.busy, false);
      assert.equal(
        native.workOrdinaryConsumption(incomplete, 1, id(100), true),
        false,
      );
      f.close();
    },
  );
  await check(
    "production Vue owner; original Workspace Promise; typed archived publication",
    async () => {
      const f = await setup();
      await f.call();
      await drain();
      positive = report(f.w);
      assert.equal(
        native.workOrdinaryConsumption(positive, 1, id(100), true),
        true,
      );
      assert.equal(native.workOrdinaryConsumption(positive, 1, id(100)), false);
      assert.equal(f.requests(), 1);
      assert.ok(!JSON.stringify(positive).includes("PRIVATE_PROJECT_CANARY"));
      f.close();
    },
  );
  for (const hold of ["reader", "outer", "canonical"])
    await check(
      `actual ${hold} tail remains pending and cannot retire successfully`,
      async () => {
        const f = await setup({ hold, rename: hold === "canonical" }),
          promise = f.call();
        await f.entered.promise;
        await drain();
        assert.equal(
          f.w.__workPublicationDiagnostic.snapshot().calls[0]
            .workspace_returned,
          0,
        );
        if (hold !== "canonical") {
          assert.equal(f.auth.state.busy, true);
          await assert.rejects(
            f.auth.projects.get(id(10)),
            (e) => e.kind === "busy",
          );
          assert.equal(f.requests(), 1);
        }
        const before = report(f.w);
        assert.equal(
          native.workOrdinaryConsumption(before, 1, id(100), true),
          false,
        );
        f.release.resolve();
        await promise;
        await drain();
        assert.equal(
          native.workOrdinaryConsumption(report(f.w), 1, id(100), true),
          false,
        );
        f.close();
      },
    );
  for (const options of [
    { badOwner: true },
    { rename: true, canonicalReject: true },
    { hold: "outer", leave: true },
    { hold: "reader", abandon: true },
    { hold: "outer", deadline: true },
    { hold: "canonical", rename: true, navigate: true },
  ])
    await check(
      "actual Workspace rejects stale/failed completion " +
        Object.keys(options).join("/"),
      async () => {
        const f = await setup(options),
          promise = f.call();
        if (options.hold) {
          await f.entered.promise;
          if (options.leave) f.auth.leave();
          if (options.abandon) f.auth.projects.abandonRead();
          if (options.deadline) {
            const t = [...f.timers.values()].find((t) => t.ms === 30000);
            assert.ok(t);
            t.fn();
          }
          if (options.navigate) {
            f.workspace.afterNavigation("/");
            f.w.history.replaceState({}, "", "/");
          }
          f.release.resolve();
        }
        await promise;
        await drain();
        assert.equal(
          native.workOrdinaryConsumption(report(f.w), 1, id(100), true),
          false,
        );
        f.close();
      },
    );
  await check(
    "normal PW finished event requires one actual successful finished return",
    () => {
      const r = structuredClone(positive),
        p = r.requests[0];
      Object.assign(p, {
        failed_at: null,
        finished_event_at: 2,
        project_terminal: "finished",
        project_failed_count: 0,
        project_finished_count: 1,
      });
      assert.equal(native.workOrdinaryConsumption(r, 1, id(100), true), true);
      p.project_terminal = null;
      assert.equal(native.workOrdinaryConsumption(r, 1, id(100), true), false);
    },
  );
  for (const [name, mutate] of [
    ["missing public", (r) => (r.documents[0].publication.calls = [])],
    ["missing end", (r) => (r.documents[0].source = "sample")],
    [
      "expiry before finish",
      (r) => (r.documents[0].publication.retirement_reason = "expired"),
    ],
    [
      "retired with pending",
      (r) => (r.documents[0].publication.pending_at_retirement = 1),
    ],
    ["late close", (r) => (r.page_closed = true)],
    ["query", (r) => (r.requests[0].has_query = true)],
    ["method", (r) => (r.requests[0].method = "POST")],
    ["duplicate PW", (r) => r.requests.push({ ...r.requests[0], sequence: 2 })],
    [
      "duplicate native",
      (r) =>
        r.documents[0].native.requests.push({
          ...r.documents[0].native.requests[0],
          sequence: 2,
        }),
    ],
    [
      "wrong published owner",
      (r) => (r.documents[0].publication.calls[0].workspace_published = false),
    ],
    [
      "noncanonical",
      (r) => (r.documents[0].publication.calls[0].workspace_canonical = false),
    ],
    [
      "missing EOF",
      (r) => (r.documents[0].native.requests[0].read_done = false),
    ],
    [
      "CL mismatch",
      (r) =>
        (r.documents[0].native.requests[0].content_length_matches_eof = false),
    ],
    ["failed plus finished", (r) => (r.requests[0].finished_event_at = 3)],
    ["repeated failed", (r) => (r.requests[0].project_failed_count = 2)],
    [
      "other failure reason",
      (r) => (r.requests[0].project_failure_aborted = false),
    ],
    ["wrong root endpoint", (r) => (r.requests[0].path += "/commands/lookup")],
  ])
    await check("closed predicate rejects " + name, () => {
      const r = structuredClone(positive);
      mutate(r);
      assert.equal(native.workOrdinaryConsumption(r, 1, id(100), true), false);
    });

  // Real Node adapter functions; only PW event source and page evaluation are doubles.
  const ts = require(root + "/web/node_modules/typescript"),
    { EventEmitter } = require("node:events");
  function declaration(file, name) {
    const text = fs.readFileSync(root + "/" + file, "utf8"),
      ast = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true),
      fn = ast.statements.find(
        (n) => ts.isFunctionDeclaration(n) && n.name?.text === name,
      );
    assert.ok(fn);
    return ts.transpileModule(
      fn.getText(ast).replace(/^export /, "") + `;this.subject=${name};`,
      { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
    ).outputText;
  }
  async function adapter(projects = [id(10)], snapshot) {
    const page = new EventEmitter(),
      context = new EventEmitter(),
      timers = new Map();
    let seq = 0,
      last;
    page.context = () => context;
    page.addInitScript = async () => {};
    page.evaluate = async () =>
      snapshot ?? {
        native: positive.documents[0].native,
        publication: positive.documents[0].publication,
      };
    const scope = vm.createContext({
      Buffer,
      URL,
      performance,
      Date,
      workSessionBinding: async () => ({}),
      installWorkNativeDiagnostic() {},
      installWorkPublicationDiagnostic() {},
      writeFileSync: (_p, v) => {
        last = JSON.parse(v);
      },
      join: path.join,
      setTimeout: (fn, ms) => {
        const key = ++seq;
        timers.set(key, { fn, ms });
        return key;
      },
      clearTimeout: (key) => timers.delete(key),
      workOrdinaryConsumption: native.workOrdinaryConsumption,
    });
    vm.runInContext(
      declaration(
        "tests/account-captcha-web/e2e/project-work-planning.native.ts",
        "startWorkNativeDiagnostic",
      ),
      scope,
    );
    const d = await scope.subject(page, {
      projects,
      repository: root,
      evidence: "unused",
      classify: () => null,
      ordinaryCompletion: true,
      projectRefreshCompletion: true,
    });
    await drain();
    let finishedCalls = 0;
    const request = {
        url: () => `https://owned.invalid/api/v1/projects/${id(10)}`,
        method: () => "GET",
        failure: () => ({ errorText: "net::ERR_ABORTED" }),
      },
      response = {
        request: () => request,
        url: request.url,
        status: () => 200,
        headers: () => ({ "x-request-id": id(100) }),
        finished: async () => {
          finishedCalls++;
          return null;
        },
      };
    page.emit("request", request);
    page.emit("response", response);
    return {
      page,
      context,
      d,
      request,
      response,
      timers,
      finishedCalls: () => finishedCalls,
      data: () => last,
      async close() {
        await d.finish();
        assert.equal(page.eventNames().length, 0);
        assert.equal(context.eventNames().length, 0);
        assert.equal(timers.size, 0);
      },
    };
  }
  await check(
    "actual Request failure never calls permanently pending PW finished",
    async () => {
      const f = await adapter();
      f.response.finished = () => {
        throw Error("must not call finished on failed Request");
      };
      const wait = f.d.projectRefreshTerminal(f.response);
      f.page.emit("requestfailed", f.request);
      await wait;
      assert.equal(f.finishedCalls(), 0);
      await f.close();
      assert.equal(f.data().requests[0].project_terminal, "failed");
      assert.equal(
        f.data().documents[0].publication.calls[0].workspace_published,
        true,
      );
    },
  );
  await check(
    "actual Request finished event calls and joins original finished once",
    async () => {
      const f = await adapter();
      const wait = f.d.projectRefreshTerminal(f.response);
      f.page.emit("requestfinished", f.request);
      await wait;
      assert.equal(f.finishedCalls(), 1);
      await f.close();
      assert.equal(f.data().requests[0].project_terminal, "finished");
    },
  );
  for (const signal of [
    "close",
    "context",
    "finish",
    "flush",
    "deadline",
    "wrong-request",
    "duplicate-failed",
    "both-events",
    "other-failure",
  ])
    await check("actual terminal observer refuses " + signal, async () => {
      const f = await adapter(),
        wait = f.d.projectRefreshTerminal(f.response);
      const observed = assert.rejects(wait, /WORK_PROJECT_REFRESH_/);
      if (signal === "close") f.page.emit("close");
      else if (signal === "context") f.context.emit("close");
      else if (signal === "finish") await f.d.finish();
      else if (signal === "flush") await f.d.flush();
      else if (signal === "deadline") {
        const timer = [...f.timers.values()].find((t) => t.ms > 1000);
        assert.ok(timer);
        timer.fn();
      } else if (signal === "wrong-request") {
        f.page.emit("requestfailed", {});
        f.page.emit("close");
      } else {
        if (signal === "other-failure")
          f.request.failure = () => ({
            errorText: "net::ERR_CONTENT_LENGTH_MISMATCH",
          });
        f.page.emit("requestfailed", f.request);
        if (signal !== "other-failure")
          f.page.emit(
            signal === "both-events" ? "requestfinished" : "requestfailed",
            f.request,
          );
      }
      await observed;
      await f.close();
      assert.equal(f.finishedCalls(), 0);
    });
  await check(
    "late requestfinished cannot turn known failure into final acceptance",
    async () => {
      const f = await adapter();
      f.page.emit("requestfailed", f.request);
      await f.d.projectRefreshTerminal(f.response);
      f.page.emit("requestfinished", f.request);
      await f.close();
      assert.equal(
        native.workOrdinaryConsumption(f.data(), 1, id(100), true),
        false,
      );
    },
  );
  await check(
    "normal finished that resolves only after close is refused",
    async () => {
      const f = await adapter(),
        tail = defer();
      f.response.finished = () => tail.promise;
      f.page.emit("requestfinished", f.request);
      const wait = assert.rejects(
        f.d.projectRefreshTerminal(f.response),
        /WORK_PROJECT_REFRESH_/,
      );
      f.page.emit("close");
      tail.resolve(null);
      await wait;
      await f.close();
      assert.equal(f.data().requests[0].project_terminal, null);
    },
  );
  for (const bad of [null, "bytes", "owner", "version", "same-project"])
    await check(
      "final three original Project proofs " + (bad ?? "positive"),
      async () => {
        const projects = [id(10), id(20), id(30)],
          snapshot = {
            native: structuredClone(positive.documents[0].native),
            publication: structuredClone(positive.documents[0].publication),
          };
        const n = snapshot.native.requests[0],
          c = snapshot.publication.calls[0];
        snapshot.native.requests = projects.map((projectID, i) => ({
          ...n,
          sequence: i + 1,
          call_id: i + 1,
          request_id: id(100 + i),
          path: `/api/v1/projects/${bad === "same-project" ? projects[0] : projectID}`,
        }));
        snapshot.publication.calls = projects.map((projectID, i) => ({
          ...c,
          call_id: i + 1,
          native_sequence: i + 1,
          target_id: bad === "same-project" ? projects[0] : projectID,
          path: snapshot.native.requests[i].path,
        }));
        const f = await adapter(projects, snapshot);
        // adapter creates one original PW Request at construction; use that first.
        for (let i = 0; i < 3; i++) {
          const row = snapshot.native.requests[i],
            request =
              i === 0
                ? f.request
                : {
                    url: () => "https://owned.invalid" + row.path,
                    method: () => "GET",
                    failure: () => ({ errorText: "net::ERR_ABORTED" }),
                  },
            response =
              i === 0
                ? f.response
                : {
                    request: () => request,
                    url: request.url,
                    status: () => 200,
                    headers: () => ({ "x-request-id": id(100 + i) }),
                  };
          if (i) {
            f.page.emit("request", request);
            f.page.emit("response", response);
          }
          f.page.emit("requestfailed", request);
          await f.d.projectRefreshTerminal(response);
          f.d.recordProjectRefresh(request, {
            requestID: id(100 + i),
            bytes: n.bytes + (i === 0 && bad === "bytes" ? 1 : 0),
            ownerID: i === 0 && bad === "owner" ? id(3) : id(1),
            version: i === 0 && bad === "version" ? "9" : "2",
          });
        }
        assert.equal(f.d.projectRefreshesComplete(3), false);
        await f.close();
        assert.equal(f.d.projectRefreshesComplete(3), bad === null);
        assert.equal(f.d.projectRefreshesComplete(2), false);
      },
    );
  await check(
    "private dist marker resolves once from the actual locked app entry",
    async () => {
      const result = await publication.workSessionBinding(root, true);
      assert.equal(result.workspace_marker, "project-workspace");
    },
  );
  // Same original sidecar helper: formal schema + actual Project API; no HTTP.
  const tmp = fs.mkdtempSync(
    root +
      "/output/ai/work-owner-planning-ui/implementation/project-get-control-",
  );
  const helperSource = declaration(
      "tests/account-captcha-web/e2e/project-work-planning.helpers.ts",
      "projectRefreshBody",
    ),
    fixture = await setup();
  const helperScope = vm.createContext({
    expect: require(
      root + "/tests/account-captcha-web/node_modules/@playwright/test",
    ).expect,
    readdirSync: fs.readdirSync,
    readFileSync: fs.readFileSync,
    join: path.join,
    evidence: tmp,
    repository: root,
    createHash: crypto.createHash,
    spawnSync: require("node:child_process").spawnSync,
    createProjectOwnerAPI: fixture.A.createProjectOwnerAPI,
    URL,
    Response,
    AbortController,
  });
  vm.runInContext(helperSource, helperScope);
  const body = {
    ...project,
    lifecycle: "archived",
    version: "2",
    archived_at: at,
  };
  const response = {
    url: () => `https://owned.invalid/api/v1/projects/${id(10)}`,
    request: () => ({ method: () => "GET" }),
    status: () => 200,
    headerValue: async () => id(100),
  };
  function sidecar(value = body, mutate = () => {}) {
    for (const f of fs.readdirSync(tmp)) fs.unlinkSync(path.join(tmp, f));
    const raw = JSON.stringify(value),
      hash = crypto.createHash("sha256").update(raw).digest("hex"),
      meta = {
        request_id: id(100),
        endpoint: `/api/v1/projects/${id(10)}`,
        method: "GET",
        status: 200,
        source_run: "TestAccountProjectWorkPlanningWebOriginalRecovery",
        input_hash: "a".repeat(64),
        body_sha256: hash,
        body_file: `body-${hash}.json`,
        content_type: "application/json",
      };
    mutate(meta);
    fs.writeFileSync(path.join(tmp, `body-${hash}.json`), raw);
    fs.writeFileSync(path.join(tmp, "response-001.json"), JSON.stringify(meta));
  }
  try {
    await check(
      "same original Project sidecar formal schema and actual typed decoder",
      async () => {
        sidecar();
        const result = await helperScope.subject(response, id(10), id(1));
        assert.equal(result.project.lifecycle, "archived");
        assert.equal(result.proof.version, "2");
        assert.equal(result.proof.ownerID, id(1));
      },
    );
    for (const [name, value, mutate] of [
      ["wrong owner", { ...body, owner_user_id: id(3) }],
      ["wrong target", { ...body, id: id(11) }],
      ["extra schema field", { ...body, extra: true }],
      ["wrong route", body, (m) => (m.endpoint += "/tasks")],
      ["wrong source", body, (m) => (m.source_run = "another")],
      ["wrong body hash", body, (m) => (m.body_sha256 = "b".repeat(64))],
    ])
      await check("same-body helper refuses " + name, async () => {
        sidecar(value, mutate);
        await assert.rejects(helperScope.subject(response, id(10), id(1)));
      });
    await check("same-body helper refuses duplicate XID", async () => {
      sidecar();
      fs.copyFileSync(
        path.join(tmp, "response-001.json"),
        path.join(tmp, "response-002.json"),
      );
      await assert.rejects(helperScope.subject(response, id(10), id(1)));
    });
  } finally {
    fixture.close();
    fs.rmSync(tmp, { recursive: true });
  }
  await drain();
  assert.equal(unhandled.length, 0);
  console.log(
    "PASS Project completion controls",
    count,
    "unhandled",
    unhandled.length,
  );
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
