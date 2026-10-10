// Independent offline method controls. Real Session/API/native/public source;
// DOM and PW wire events are explicit controlled halves, never browser evidence.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const crypto = require("node:crypto");
const root = path.resolve(__dirname, "../..");
const ts = require(root + "/web/node_modules/typescript");
const { JSDOM } = require(root + "/web/node_modules/jsdom");
const id = (n) => `01900000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const sourceRun = "TestIndependentProjectWorkPlanningWebRecovery";
const at = "2026-10-10T00:00:00.000000Z";
const policies = [
  "independent-blocker-tree-tasks",
  "independent-task-tree-sprints",
  "independent-history-milestone",
];
const seeds = Object.fromEntries(
  ["blocker", "task", "structure"].map((name, i) => [
    name,
    {
      project_id: id(10 + i * 10),
      milestone_id: id(11 + i * 10),
      sprint_id: id(12 + i * 10),
      task_id: id(13 + i * 10),
    },
  ]),
);
const unhandled = [];
process.on("unhandledRejection", (error) => unhandled.push(error));
const deferred = () => {
  let resolve;
  const promise = new Promise((yes) => (resolve = yes));
  return { promise, resolve };
};
const drain = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise((resolve) => setImmediate(resolve));
};
function declaration(file, name) {
  const source = ts.createSourceFile(
    file,
    fs.readFileSync(root + "/" + file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
  const fn = source.statements.find(
    (node) => ts.isFunctionDeclaration(node) && node.name?.text === name,
  );
  assert.ok(fn, name);
  return (
    ts.transpileModule(fn.getText(source).replace(/^export /, ""), {
      compilerOptions: { target: ts.ScriptTarget.ES2022 },
    }).outputText + `\nthis.${name}=${name};`
  );
}
const helperFile =
  "tests/account-captcha-web/e2e/project-work-planning.helpers.ts";
const nativeFile =
  "tests/account-captcha-web/e2e/project-work-planning.native.ts";
const publicFile =
  "tests/account-captcha-web/e2e/project-work-planning.publication.ts";
const scope = vm.createContext({
  URL,
  Date,
  Promise,
  Error,
  Set,
  Map,
  Object,
  JSON,
  uuid7:
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
  safeWorkFailure: (r) =>
    r.failure()?.errorText === "net::ERR_ABORTED" ? "aborted" : "other",
});
vm.runInContext(
  declaration(helperFile, "workIndependentRecovery") +
    declaration(helperFile, "workOrdinaryCompletionEvents") +
    declaration(nativeFile, "workOrdinaryConsumption"),
  scope,
);
const passed = [];
async function check(name, run) {
  await run();
  passed.push(name);
}
(async () => {
  const { build } = await import(
    root + "/web/node_modules/vite/dist/node/index.js"
  );
  const entry =
    root + "/output/ai/work-owner-ui-independent/virtual-consumers.js";
  const built = await build({
    configFile: false,
    root,
    logLevel: "silent",
    plugins: [
      {
        name: "independent-actual-session",
        resolveId: (id) =>
          id === entry ? "\0independent-actual-session" : undefined,
        load: (id) =>
          id === "\0independent-actual-session"
            ? `export {createSessionController} from '${root}/web/src/composables/useSession.ts';export {createAccountAPI} from '${root}/web/src/api/account.ts';export {createWorkPlanningAPI} from '${root}/web/src/api/work-planning.ts';`
            : undefined,
      },
    ],
    build: {
      write: false,
      minify: false,
      lib: { entry, name: "ActualWork", formats: ["iife"] },
    },
  });
  const bundle = (Array.isArray(built) ? built : [built])
    .flatMap((part) => part.output)
    .find((part) => part.type === "chunk").code;
  async function setup(policy, options = {}) {
    const history = policy === policies[2],
      tasks = policy === policies[0];
    const seed = seeds[history ? "structure" : tasks ? "blocker" : "task"];
    const dom = new JSDOM(
      '<body><div id="app"></div><div class="desktop-tree"></div></body>',
      {
        url: "https://owned.invalid/owner/project/tasks/explore",
        runScripts: "outside-only",
      },
    );
    const w = dom.window,
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
    let timerID = 0;
    const timers = new Map();
    w.setTimeout = (fn, ms) => {
      timers.set(++timerID, { fn, ms });
      return timerID;
    };
    w.clearTimeout = (id) => timers.delete(id);
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
    const base = {
      id: seed.milestone_id,
      project_id: seed.project_id,
      title: "independent private value",
      description: "",
      manual_rank: "8".repeat(32),
      version: "1",
      created_at: at,
      updated_at: at,
    };
    const item = tasks
      ? {
          ...base,
          id: seed.task_id,
          milestone_id: seed.milestone_id,
          sprint_id: seed.sprint_id,
          type: "task",
          priority: "medium",
          state: "backlog",
          assignee_agent_id: null,
          plan: "",
        }
      : {
          ...base,
          id: seed.sprint_id,
          milestone_id: seed.milestone_id,
          state: "planned",
          started_at: null,
          started_by: null,
          completed_at: null,
          completed_by: null,
        };
    for (const key of [
      "description",
      "plan",
      "started_at",
      "started_by",
      "completed_at",
      "completed_by",
    ])
      delete item[key];
    const receipt = {
      command: "work.milestone.update",
      changed: true,
      milestone: { ...base, title: "original", version: "2" },
      sprint: null,
      event_id: id(500),
    };
    let ledger,
      preparing = false,
      serial = 0,
      rawCount = 0,
      lastPromise,
      originalPromise,
      successorOnly = false,
      lookupCount = 0;
    const requests = [],
      tails = [],
      headerHeld = deferred(),
      compareHeld = deferred(),
      readerHeld = deferred(),
      outerHeld = deferred();
    const readerEntered = deferred(),
      outerEntered = deferred();
    const method = history
      ? "retryOriginal"
      : tasks
        ? "listTasks"
        : "listSprints";
    w.fetch = (url, init) => {
      if (url === "/api/v1/session")
        return Promise.resolve(
          new Response(JSON.stringify(session), {
            status: 200,
            headers: {
              "Content-Type": "application/json",
              "X-Request-ID": id(99),
            },
          }),
        );
      const full = new URL(url, w.location.origin),
        head = new Headers(init?.headers),
        requestID = id(
          1000 + Number(seed.project_id.slice(-12)) * 10 + ++serial,
        );
      const actual = {};
      for (const [k, v] of head) actual[k] = v;
      if (options.duplicateQuery && !history)
        full.searchParams.append("limit", "50");
      if (options.wrongBody && !history) init = { ...init, body: "{}" };
      if (init.method !== "GET") actual.origin = full.origin;
      const wireBody =
        options.replayBody && history && !preparing && init.method === "PATCH"
          ? JSON.stringify({ ...JSON.parse(init.body), ...options.replayBody })
          : init.body;
      const request = {
        url: () => full.href,
        method: () => init.method,
        postData: () => wireBody ?? null,
        postDataJSON: () => JSON.parse(wireBody),
        failure: () => ({
          errorText: options.failureReason ?? "net::ERR_ABORTED",
        }),
        allHeaders: async () => {
          rawCount++;
          if (options.headersHeld) await headerHeld.promise;
          return { ...actual, ...options.rawOverride };
        },
      };
      requests.push({ request, requestID, status: 200 });
      ledger?.request(request, preparing ? "lost-milestone-update" : null);
      if (preparing) {
        requests.at(-1).declaration = "lost-milestone-update";
        return Promise.reject(Error("owned original loss"));
      }
      ledger?.response(request, requestID);
      let value = history
        ? full.pathname.endsWith("/lookup")
          ? ++lookupCount > 1 && options.latestInProgress
            ? { state: "in_progress", result: null }
            : { state: "committed", result: receipt }
          : receipt
        : { items: [item] };
      if (options.badPage && !history)
        value = { items: [{ ...item, project_id: id(999) }] };
      if (options.badReceipt && history && !full.pathname.endsWith("/lookup"))
        value = {
          ...receipt,
          milestone: { ...receipt.milestone, version: "3" },
        };
      const bytes = new TextEncoder().encode(JSON.stringify(value));
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(bytes);
          controller.close();
        },
      });
      const get = stream.getReader.bind(stream),
        cancel = stream.cancel.bind(stream);
      stream.getReader = (...args) => {
        const reader = get(...args),
          release = reader.cancel.bind(reader);
        reader.cancel = () => {
          if (!history || successorOnly) readerEntered.resolve();
          return (
            options.readerHeld && (!history || successorOnly)
              ? readerHeld.promise
              : Promise.resolve()
          ).then(() => release());
        };
        return reader;
      };
      stream.cancel = () => {
        if (!history || successorOnly) outerEntered.resolve();
        return (
          options.outerHeld && (!history || successorOnly)
            ? outerHeld.promise
            : Promise.resolve()
        ).then(() => cancel());
      };
      return Promise.resolve(
        new Response(stream, {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "Content-Length": String(bytes.length),
            "X-Request-ID": requestID,
          },
        }),
      );
    };
    vm.runInContext(bundle, ctx);
    const fetcher = (...args) => w.fetch(...args),
      deps = Array(16).fill(undefined);
    deps[0] = w.ActualWork.createAccountAPI(fetcher);
    deps[15] = w.ActualWork.createWorkPlanningAPI(fetcher);
    const auth = w.ActualWork.createSessionController(...deps);
    await auth.restore();
    const workspace = {
      currentReadContext: {
        value: {
          projectID: seed.project_id,
          identity: { ...auth.personalContext.identity },
          generation: 1,
          readGeneration: 1,
        },
      },
      detail: { project: {} },
      blocked: { value: false },
      readCurrent: () => Promise.resolve(),
    };
    const app = w.document.querySelector("#app"),
      instance = {
        parent: null,
        provides: { [Symbol("project-workspace")]: workspace },
      };
    instance.root = instance;
    app._vnode = { component: instance };
    app.__vue_app__ = { _container: app };
    let oldPage;
    if (options.oldPromise) {
      oldPage = await auth.workPlanning[method](
        seed.project_id,
        tasks
          ? {
              sprint_id: seed.sprint_id,
              state: "backlog",
              assignee_agent_id: null,
              limit: 50,
            }
          : { milestone_id: seed.milestone_id, limit: 50 },
      );
      requests.length = 0;
      serial = 0;
    }
    const original = auth.workPlanning[method];
    auth.workPlanning[method] = function (...args) {
      originalPromise = Reflect.apply(original, this, args);
      return options.oldPromise
        ? Promise.resolve(oldPage)
        : options.voidResult
          ? originalPromise.then(() => undefined)
          : originalPromise;
    };
    vm.runInContext(
      declaration(nativeFile, "installWorkNativeDiagnostic") +
        declaration(publicFile, "installWorkPublicationDiagnostic"),
      ctx,
    );
    w.installWorkNativeDiagnostic({
      projects: [seed.project_id],
      expiresAt: Date.now() + 45000,
      independentRecovery: sourceRun,
    });
    w.Function = function () {
      return async () => ({ singleton: () => auth });
    };
    assert.equal(
      await w.installWorkPublicationDiagnostic({
        binding: {
          entry: "/assets/main.js",
          asset: "/assets/session.js",
          export_name: "singleton",
          workspace_marker: "project-workspace",
        },
        expiresAt: Date.now() + 45000,
        independentRecovery: sourceRun,
      }),
      "installed",
    );
    const invoke = async (action, value) => {
      if (options.compareHeld && action === "bind") await compareHeld.promise;
      return w.__workPublicationDiagnostic.independentAction(action, value);
    };
    ledger =
      options.sharedLedger ??
      scope.workIndependentRecovery(
        sourceRun,
        seeds,
        (tail) => tails.push(tail),
        invoke,
      );
    if (!options.sharedLedger)
      await ledger.arm(policy, history ? "1" : undefined);
    const call = (query = options.query) => {
      successorOnly = true;
      lastPromise = history
        ? auth.workPlanning.retryOriginal()
        : auth.workPlanning[method](
            seed.project_id,
            tasks
              ? {
                  sprint_id: seed.sprint_id,
                  state: "backlog",
                  assignee_agent_id: null,
                  limit: 50,
                  ...query,
                }
              : { milestone_id: seed.milestone_id, limit: 50, ...query },
          );
      if (!options.voidResult && !options.oldPromise)
        assert.equal(
          lastPromise,
          originalPromise,
          "wrapper returns original Promise",
        );
      void lastPromise.catch(() => {});
      return lastPromise;
    };
    const prepare = async () => {
      preparing = true;
      await auth.workPlanning
        .start({
          domain: "structure",
          command: "work.milestone.update",
          projectID: seed.project_id,
          targetID: seed.milestone_id,
          expected_version: "1",
          request: { title: "original" },
        })
        .catch(() => {});
      preparing = false;
      const result = await auth.workPlanning.checkOriginal();
      await drain();
      assert.equal(result.value.state, "committed");
      assert.equal(
        auth.workPlanning.progress.receipt.value,
        result.value.result,
      );
      await auth.workPlanning.checkOriginal();
      await drain();
      if (options.latestWrong) {
        const old = requests.at(-1).request;
        const wrong = {
          ...old,
          postData: () =>
            JSON.stringify({
              ...JSON.parse(old.postData()),
              target_id: id(999),
            }),
          allHeaders: () => old.allHeaders(),
        };
        ledger.request(wrong, null);
        ledger.response(wrong, id(999));
      }
      await ledger.armHistory();
    };
    const publish = async () => {
      if (!history && !options.oldUI) {
        const row = w.document.createElement("div");
        row.setAttribute("role", "treeitem");
        row.setAttribute(
          "data-tree-id",
          (tasks ? "task:" : "sprint:") +
            (tasks ? seed.task_id : seed.sprint_id),
        );
        row.getClientRects = () => [{}];
        w.document.querySelector(".desktop-tree").append(row);
      }
      await drain();
      return ledger.published(policy);
    };
    const finish = async (normal = false) => {
      for (const { request, declaration } of requests)
        if (!declaration && ledger.selected(request)) {
          if (normal) {
            ledger.finished(request);
            ledger.finishedResult(request, null);
          } else ledger.failed(request);
        }
      await drain();
      const publication = w.__workPublicationDiagnostic.finish(),
        native = w.__workNativeDiagnostic.finish();
      const pw = requests.map(
        ({ request, requestID, status, declaration }, i) => ({
          sequence: i + 1,
          method: request.method(),
          path: new URL(request.url()).pathname,
          request_id: requestID,
          status,
          declaration: declaration ?? null,
          request_at: 1,
          response_at: 2,
          failed_at: normal ? null : 3,
          finished_event_at: normal ? 3 : null,
          independent_recovery: ledger.evidence(request),
        }),
      );
      const projected = native.requests.map((row) => {
        const index = pw.findIndex(
          (value) => value.request_id === row.request_id,
        );
        return {
          ...row,
          bound_original_request: index >= 0,
          bound_public_call: row.call_id !== null,
          pw_sequence: index + 1,
          declaration: pw[index]?.declaration ?? null,
          content_length_comparable: row.length_comparable_before_binding,
          content_length_matches_eof: row.length_matches_before_binding,
        };
      });
      const report = {
        observation_finished: true,
        ordinary_finished_gate_unchanged: false,
        sample_joined: true,
        sample_join_unavailable: false,
        end_snapshot_observed: true,
        page_closed: false,
        context_closed: false,
        overflow: false,
        projection_rejected: 0,
        independent_recovery: sourceRun,
        requests: pw,
        documents: [
          {
            source: "end",
            end_snapshot_observed: true,
            before_page_close: true,
            native: { ...native, requests: projected },
            publication,
          },
        ],
      };
      const selected = pw.find((row) => row.independent_recovery);
      return {
        accepted:
          !!selected &&
          scope.workOrdinaryConsumption(
            report,
            selected.sequence,
            selected.request_id,
            false,
            true,
          ),
        report,
        selected,
      };
    };
    return {
      auth,
      w,
      ledger,
      invoke,
      call,
      prepare,
      publish,
      finish,
      requests,
      tails,
      readerEntered,
      outerEntered,
      rawCount: () => rawCount,
      release() {
        headerHeld.resolve();
        compareHeld.resolve();
        readerHeld.resolve();
        outerHeld.resolve();
      },
      async cleanup() {
        this.release();
        ledger.close();
        await Promise.all(tails);
        auth.leave();
        await originalPromise?.catch(() => {});
        await drain();
        w.__workPublicationDiagnostic.finish();
        w.__workNativeDiagnostic.finish();
        dom.window.close();
      },
    };
  }
  for (const policy of policies)
    for (const normal of [true, false])
      await check(
        `${policy}: ${normal ? "finished" : "failed-complete"} original consumer`,
        async () => {
          const e = await setup(policy);
          try {
            if (policy === policies[2]) await e.prepare();
            await e.call();
            await e.publish();
            const result = await e.finish(normal);
            assert.equal(result.accepted, true);
            assert.equal(
              scope.workOrdinaryConsumption(
                result.report,
                result.selected.sequence,
                result.selected.request_id,
              ),
              false,
              "default scope rejects new route",
            );
            assert.equal(e.rawCount(), policy === policies[2] ? 4 : 1);
          } finally {
            await e.cleanup();
          }
        },
      );
  for (const policy of policies.slice(0, 2))
    for (const options of [
      { query: { cursor: "opaque" } },
      { query: { limit: 49 } },
      { query: { text: "filter" } },
      { badPage: true },
      { voidResult: true },
      { oldPromise: true },
      { oldUI: true },
      { duplicateQuery: true },
      { wrongBody: true },
    ])
      await check(
        `${policy}: malformed input/result/publication rejected ${Object.keys(options)[0]}`,
        async () => {
          const e = await setup(policy, options);
          try {
            await e.call().catch(() => {});
            await e.publish().catch(() => {});
            assert.equal((await e.finish()).accepted, false);
          } finally {
            await e.cleanup();
          }
        },
      );
  for (const policy of policies)
    for (const held of ["reader", "outer"])
      await check(
        `${policy}: held ${held} cannot retire or upgrade late`,
        async () => {
          const e = await setup(policy, { [held + "Held"]: true });
          try {
            if (policy === policies[2]) await e.prepare();
            const promise = e.call();
            await e[held + "Entered"].promise;
            const before = await e.finish();
            assert.equal(before.accepted, false);
            e.release();
            await promise.catch(() => {});
            await e.publish().catch(() => {});
            assert.equal((await e.finish()).accepted, false);
          } finally {
            await e.cleanup();
          }
        },
      );
  for (const field of ["key", "csrf", "origin"])
    await check(`history actual ${field} mismatch rejected`, async () => {
      const header = {
        key: "idempotency-key",
        csrf: "x-csrf-token",
        origin: "origin",
      }[field];
      const e = await setup(policies[2], {
        rawOverride: { [header]: "wrong" },
      });
      try {
        await assert.rejects(e.prepare());
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    });
  for (const held of ["headers", "compare"])
    await check(
      `owned ${held} tail seals without late acceptance`,
      async () => {
        const e = await setup(policies[0], { [held + "Held"]: true });
        try {
          await e.call();
          e.ledger.close();
          assert.equal(e.ledger.verify(), false);
          e.release();
          await Promise.all(e.tails);
          assert.equal(e.ledger.verify(), false);
          assert.equal((await e.finish()).accepted, false);
        } finally {
          await e.cleanup();
        }
      },
    );

  await check("history wrong latest Lookup cannot fall back", async () => {
    const e = await setup(policies[2], { latestWrong: true });
    try {
      await assert.rejects(e.prepare());
      assert.equal((await e.finish()).accepted, false);
    } finally {
      await e.cleanup();
    }
  });
  for (const policy of policies.slice(0, 2))
    await check(`${policy}: first wrong call occupies slot`, async () => {
      const e = await setup(policy, { query: { limit: 49 } });
      try {
        await e.call().catch(() => {});
        await e.call({}).catch(() => {});
        await e.publish().catch(() => {});
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    });
  for (const policy of policies)
    await check(
      `${policy}: identity loss rejects original publication`,
      async () => {
        const e = await setup(policy, { readerHeld: true });
        try {
          if (policy === policies[2]) await e.prepare();
          const original = e.call();
          await e.readerEntered.promise;
          e.auth.leave();
          e.release();
          await original.catch(() => {});
          await e.publish().catch(() => {});
          assert.equal((await e.finish()).accepted, false);
        } finally {
          await e.cleanup();
        }
      },
    );
  await check(
    "history wrong typed replay cannot replace published Lookup",
    async () => {
      const e = await setup(policies[2], { badReceipt: true });
      try {
        await e.prepare();
        await e.call().catch(() => {});
        await e.publish().catch(() => {});
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    },
  );
  await check("sealed header admission rejects a new candidate", async () => {
    const e = await setup(policies[0]);
    try {
      await e.call();
      await e.publish();
      e.ledger.seal();
      const original = e.requests[0].request;
      const raw = e.rawCount();
      e.ledger.request({ ...original }, null);
      await Promise.all(e.tails);
      assert.equal(e.rawCount(), raw);
      assert.equal(e.ledger.verify(), false);
    } finally {
      await e.cleanup();
    }
  });

  await check(
    "latest real Structure in_progress cannot reuse older committed pointer",
    async () => {
      const e = await setup(policies[2], { latestInProgress: true });
      try {
        await assert.rejects(e.prepare());
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    },
  );
  for (const replayBody of [
    { expected_version: "2" },
    { request: { title: "wrong-original" } },
  ])
    await check("history wrong original replay body rejects", async () => {
      const e = await setup(policies[2], { replayBody });
      try {
        await e.prepare();
        await e.call();
        await e.publish().catch(() => {});
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    });
  await check(
    "history second mutation invalidates first captured replay",
    async () => {
      const e = await setup(policies[2]);
      try {
        await e.prepare();
        await e.call();
        const first = e.requests.at(-1).request;
        e.ledger.request({ ...first }, null);
        await e.publish().catch(() => {});
        assert.equal((await e.finish()).accepted, false);
      } finally {
        await e.cleanup();
      }
    },
  );
  await check("duplicate response identity invalidates method", async () => {
    const e = await setup(policies[0]);
    try {
      await e.call();
      await e.publish();
      const row = e.requests[0];
      e.ledger.response(row.request, row.requestID);
      assert.equal(e.ledger.verify(), false);
    } finally {
      await e.cleanup();
    }
  });

  await check(
    "three original documents retain one complete ledger and joined operations",
    async () => {
      let active;
      const tails = [],
        fixtures = [];
      const ledger = scope.workIndependentRecovery(
        sourceRun,
        seeds,
        (tail) => tails.push(tail),
        (action, value) => active.invoke(action, value),
      );
      try {
        for (const policy of policies) {
          active = await setup(policy, { sharedLedger: ledger });
          fixtures.push(active);
          await ledger.arm(policy, policy === policies[2] ? "1" : undefined);
          if (policy === policies[2]) await active.prepare();
          await active.call();
          await active.publish();
          assert.equal((await active.finish()).accepted, true);
        }
        ledger.seal();
        await Promise.all(tails);
        assert.equal(ledger.verify(), true);
        const request = fixtures[0].requests[0].request,
          count = fixtures[0].rawCount();
        ledger.request({ ...request }, null);
        await Promise.all(tails);
        assert.equal(fixtures[0].rawCount(), count);
        assert.equal(
          ledger.verify(),
          false,
          "new admission after seal is monotonic failure",
        );
      } finally {
        ledger.close();
        for (const fixture of fixtures) await fixture.cleanup();
        await Promise.all(tails);
      }
    },
  );
  await check("unknown source and unknown policy reject", async () => {
    assert.throws(() =>
      scope.workIndependentRecovery(
        "author",
        seeds,
        () => {},
        async () => true,
      ),
    );
    const e = await setup(policies[0]);
    try {
      await assert.rejects(e.ledger.arm("unknown"));
      assert.equal(
        e.w.__workPublicationDiagnostic.independentAction("unknown", {}),
        false,
      );
    } finally {
      await e.cleanup();
    }
  });
  await drain();
  assert.equal(unhandled.length, 0);
  console.log(
    JSON.stringify({ controls: passed.length, passed, unhandled: 0 }),
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
