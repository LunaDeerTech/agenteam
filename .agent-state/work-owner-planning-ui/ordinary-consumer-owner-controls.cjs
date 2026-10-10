// Offline actual-source controls: no server, socket, browser or real account.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const crypto = require("node:crypto");
const root = path.resolve(__dirname, "../..");
const { JSDOM } = require(root + "/web/node_modules/jsdom");
const pwRoot = root + "/tests/account-captcha-web/node_modules/playwright";
assert.equal(require(pwRoot + "/package.json").version, "1.56.1");
const { transformHook } = require(pwRoot + "/lib/transform/transform.js");
const id = (n) => `01900000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const project = id(10),
  target = id(11),
  at = "2026-10-09T10:00:00.000000Z";
const planningSpec = (slot = 1) => ({
  slot,
  kind: ["milestone", "sprint", "task"][Math.floor((slot - 1) / 2)],
  tail: slot % 2 === 0,
  projectID: project,
  targetID: target,
  expectedVersion: slot % 2 === 0 ? "2" : "1",
  peerID: id(14),
  milestoneID: slot > 2 ? id(12) : null,
  sprintID: slot > 4 ? id(13) : null,
});
const planningCommand = (spec = planningSpec()) => ({
  domain: spec.kind === "task" ? "task" : "structure",
  projectID: spec.projectID,
  command: `work.${spec.kind}.reorder`,
  targetID: spec.targetID,
  expected_version: spec.expectedVersion,
  request: {
    ...(spec.kind === "sprint" ? { milestone_id: spec.milestoneID } : {}),
    ...(!spec.tail ? { before_id: spec.peerID } : {}),
  },
});
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
const milestone = {
  id: target,
  project_id: project,
  title: "private-material-canary",
  description: "",
  manual_rank: "8".repeat(32),
  version: "1",
  created_at: at,
  updated_at: at,
};
const task = {
  ...milestone,
  milestone_id: id(12),
  sprint_id: id(13),
  type: "task",
  priority: "medium",
  state: "backlog",
  assignee_agent_id: null,
  plan: "",
  version: "2",
};
const sprint = {
  ...milestone,
  milestone_id: id(12),
  state: "planned",
  started_at: null,
  started_by: null,
  completed_at: null,
  completed_by: null,
};
const drain = async () => {
  for (let i = 0; i < 16; i++) await Promise.resolve();
  await new Promise((r) => setImmediate(r));
};
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
const failures = [];
process.on("unhandledRejection", (e) => failures.push(e));
function installer(file, name) {
  const source = fs.readFileSync(root + "/" + file, "utf8");
  const compiled = transformHook(source, root + "/" + file).code;
  const ctx = vm.createContext({
    exports: {},
    require: (id) => (id.startsWith("./") ? {} : require(id)),
  });
  vm.runInContext(compiled, ctx);
  return "(" + ctx.exports[name].toString() + ")";
}
(async () => {
  const { build } = await import(
    root + "/web/node_modules/vite/dist/node/index.js"
  );
  const entry =
    root + "/output/ai/work-owner-planning-ui/virtual-ordinary-owner.js";
  const built = await build({
    configFile: false,
    root,
    logLevel: "silent",
    plugins: [
      {
        name: "actual-work-consumer",
        enforce: "pre",
        resolveId: (id) =>
          id === entry ? "\0actual-work-consumer" : undefined,
        load: (id) =>
          id === "\0actual-work-consumer"
            ? `export {createSessionController} from '${root}/web/src/composables/useSession.ts';export {createAccountAPI} from '${root}/web/src/api/account.ts';export {createWorkPlanningAPI} from '${root}/web/src/api/work-planning.ts';export {createProjectOwnerAPI} from '${root}/web/src/api/project-owner.ts';export {createProjectWorkspace,projectWorkspaceKey} from '${root}/web/src/composables/useProjectWorkspace.ts';export {createProjectWorkPlanning} from '${root}/web/src/composables/useProjectWorkPlanning.ts';export {createApp,provide,h} from '${root}/web/node_modules/vue/dist/vue.runtime.esm-bundler.js';`
            : undefined,
      },
    ],
    build: {
      write: false,
      minify: false,
      lib: { entry, name: "ActualWork", formats: ["iife"] },
    },
  });
  const code = (Array.isArray(built) ? built : [built])
    .flatMap((x) => x.output)
    .find((x) => x.type === "chunk").code;
  const nativeCode = installer(
    "tests/account-captcha-web/e2e/project-work-planning.native.ts",
    "installWorkNativeDiagnostic",
  );
  const publicCode = installer(
    "tests/account-captcha-web/e2e/project-work-planning.publication.ts",
    "installWorkPublicationDiagnostic",
  );
  const judge = vm.runInNewContext(
    installer(
      "tests/account-captcha-web/e2e/project-work-planning.native.ts",
      "workOrdinaryConsumption",
    ),
  );
  let positiveReport, blockerReport, planningReport;
  function reportOf(e) {
    const requests = e.native.requests.map((n, i) => ({
      sequence: e.planning ? e.specs[i].slot : i + 1,
      request_id: n.request_id,
      method: n.method,
      path: n.path,
      status: n.status,
      declaration: null,
      original_replay_bound:
        e.originalReplayBound === true && n.request_id === id(100),
      replay_at_request_verified:
        e.originalReplayBound === true && n.request_id === id(100),
      original_replay_later_verified: false,
      replay_actual_verified: true,
      replay_headers_joined: true,
      replay_policy: e.replayPolicy,
      replay_request_at: 0,
      replay_verified_at: 1,
      replay_ended: true,
      replay_invalid: false,
      replay_lookup_request_id:
        e.replayPolicy === "historical-task" ? id(98) : null,
      replay_lookup_finished: e.replayPolicy === "historical-task",
      request_at: 0,
      response_at: 1,
      failed_at: n.request_id === id(98) ? null : 2,
      finished_event_at: n.request_id === id(98) ? 2 : null,
      ...(e.planning
        ? {
            planning_document_id: e.native.document_id,
            planning_reorder: {
              ...e.specs[i],
              bound: true,
              joined: true,
              invalid: false,
              ended: true,
              failedCount: 1,
              finishedCount: 0,
              aborted: true,
              finishedStarted: false,
              finishedNull: false,
            },
          }
        : {}),
    }));
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
        ...(e.planning ? { planning_policy: "planning-reorders" } : {}),
        requests,
        documents: [
          {
            source: "end",
            end_snapshot_observed: true,
            before_page_close: true,
            native: {
              ...e.native,
              requests: e.native.requests.map((n, i) => ({
                ...n,
                bound_original_request: true,
                bound_public_call: true,
                pw_sequence: e.planning ? e.specs[i].slot : i + 1,
                declaration: null,
                content_length_comparable: n.length_comparable_before_binding,
                content_length_matches_eof: n.length_matches_before_binding,
              })),
            },
            publication: e.public,
          },
        ],
      }),
    );
  }
  const accepts = (e) => {
    const report = reportOf(e),
      request = report.requests.find((r) => r.request_id === id(100));
    return (
      !!request &&
      judge(report, request.sequence, id(100), false, e.planning === true)
    );
  };
  const passed = [];
  async function check(name, fn) {
    if (process.env.WORK_PLANNING_ONLY === "1" && !name.startsWith("planning "))
      return;
    try {
      await fn();
    } catch (error) {
      console.error("CONTROL_FAILED", name);
      throw error;
    }
    passed.push(name);
  }
  async function setup(kind = "milestone", options = {}) {
    let plan = planningSpec(options.slot ?? 1);
    const specs = [],
      startPromises = new Set();
    const dom = new JSDOM("<body></body>", {
        url: "https://owned.invalid",
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
    let timerSequence = 0;
    const timers = new Map();
    w.setTimeout = (fn, ms) => {
      const id = ++timerSequence;
      timers.set(id, { fn, ms });
      return id;
    };
    w.clearTimeout = (id) => timers.delete(id);
    let requestCount = 0,
      preparing = false,
      lookupPreparing = false,
      originalMaterial,
      originalReplayBound = false;
    const historicalTaskReceipt = {
      task: { ...task, plan: "original" },
      changed: true,
      task_event_id: id(21),
      event_ids: [id(22)],
    };
    const successorHold = deferred();
    const readerHold = deferred(),
      streamHold = deferred(),
      readerEntered = deferred(),
      streamEntered = deferred();
    const endpoint =
      kind === "planning"
        ? `/api/v1/projects/${project}/${plan.kind}s/${target}/reorder`
        : kind === "lookup-blocker"
          ? `/api/v1/projects/${project}/tasks/${target}/blocker-commands/lookup`
          : ["structure", "lookup-task", "lookup-blocker"].includes(kind)
            ? `/api/v1/projects/${project}/${kind === "structure" ? "structure" : "task"}-commands/lookup`
            : `/api/v1/projects/${project}/${["milestone", "replay"].includes(kind) ? "milestones" : kind === "sprint" ? "sprints" : "tasks"}/${target}`;
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
      if (preparing) {
        originalMaterial = {
          url,
          method: init.method,
          body: init.body,
          key: new Headers(init.headers).get("idempotency-key"),
          csrf: new Headers(init.headers).get("x-csrf-token"),
        };
        return Promise.reject(Error("owned preparation transport failure"));
      }
      if (lookupPreparing) {
        assert.equal(
          url,
          `/api/v1/projects/${project}/${kind === "replay-task" ? "task" : "structure"}-commands/lookup`,
        );
        assert.equal(init.method, "POST");
        const value =
          kind === "replay-task"
            ? options.uncommittedHistory
              ? { status: "not_observed", receipt: null }
              : { status: "committed", receipt: historicalTaskReceipt }
            : { state: "not_observed", result: null };
        const raw = JSON.stringify(value);
        return Promise.resolve(
          new Response(raw, {
            status: 200,
            headers: {
              "Content-Type": "application/json",
              "Content-Length": String(Buffer.byteLength(raw)),
              "X-Request-ID": id(98),
            },
          }),
        );
      }
      assert.equal(url, endpoint);
      if (kind === "planning" && !options.skipMaterial) {
        const material = {
          body: init.body,
          key: new Headers(init.headers).get("idempotency-key"),
          csrf: new Headers(init.headers).get("x-csrf-token"),
          origin: w.location.origin,
        };
        if (options.wrongMaterial)
          material[options.wrongMaterial] = "wrong-private-material";
        w.__workPublicationDiagnostic.bindFirstRequest(plan, material);
      }
      if (kind === "replay" && init.method === "GET" && options.successorHeld)
        return successorHold.promise.then(
          () =>
            new Response(JSON.stringify(milestone), {
              status: 200,
              headers: {
                "Content-Type": "application/json",
                "X-Request-ID": id(101),
              },
            }),
        );
      if (["replay", "replay-task"].includes(kind)) {
        originalReplayBound =
          originalMaterial.url === url &&
          originalMaterial.method === init.method &&
          originalMaterial.body === init.body &&
          originalMaterial.key ===
            new Headers(init.headers).get("idempotency-key") &&
          originalMaterial.csrf ===
            new Headers(init.headers).get("x-csrf-token");
        assert.equal(originalReplayBound, true);
      }
      assert.equal(
        init.method,
        ["structure", "lookup-task", "lookup-blocker", "planning"].includes(
          kind,
        )
          ? "POST"
          : ["replay", "replay-task"].includes(kind)
            ? "PATCH"
            : "GET",
      );
      requestCount++;
      let value =
        kind === "milestone"
          ? milestone
          : kind === "sprint"
            ? sprint
            : kind === "task"
              ? task
              : kind === "structure"
                ? { state: "in_progress", result: null }
                : { status: "in_progress", receipt: null };
      if (kind === "replay")
        value = options.voidReceipt
          ? null
          : {
              command: "work.milestone.update",
              changed: true,
              milestone: { ...milestone, title: "original", version: "2" },
              sprint: null,
              event_id: id(20),
            };
      if (kind === "planning") {
        const entity = {
          ...(plan.kind === "milestone"
            ? milestone
            : plan.kind === "sprint"
              ? sprint
              : task),
          version: options.wrongVersion
            ? "4"
            : String(BigInt(plan.expectedVersion) + 1n),
          ...(plan.kind === "task"
            ? { priority: options.wrongPriority ? "medium" : "high" }
            : {}),
          ...(options.wrongTarget ? { id: id(15) } : {}),
          ...(options.wrongParent ? { milestone_id: id(16) } : {}),
        };
        value = options.voidReceipt
          ? null
          : plan.kind === "task"
            ? {
                task: entity,
                changed: !options.unchanged,
                task_event_id: id(20 + plan.slot),
                event_ids: [id(40 + plan.slot)],
              }
            : {
                command: `work.${plan.kind}.reorder`,
                changed: !options.unchanged,
                milestone: plan.kind === "milestone" ? entity : null,
                sprint: plan.kind === "sprint" ? entity : null,
                event_id: id(20 + plan.slot),
              };
      }
      if (kind === "replay-task") {
        value = options.voidReceipt
          ? null
          : {
              ...historicalTaskReceipt,
              task: {
                ...historicalTaskReceipt.task,
                ...(options.differentHistory
                  ? { plan: "newer-current-value" }
                  : {}),
                ...(options.wrongReplayTask ? { id: id(15) } : {}),
                ...(options.wrongReplayVersion ? { version: "3" } : {}),
              },
            };
      }
      if (kind === "lookup-blocker" && options.lookupStatus) {
        value = {
          status: options.lookupStatus,
          receipt:
            options.lookupStatus === "committed"
              ? {
                  task,
                  blocker: {
                    id: id(14),
                    project_id: project,
                    task_id: target,
                    type: "waiting_for_human",
                    description: "",
                    metadata: {},
                    created_at: at,
                    created_by: {
                      type: "human",
                      user_id: id(1),
                      source: "task_domain",
                    },
                    resolved_at: null,
                    resolved_by: null,
                    resolution_comment: null,
                  },
                  task_event_id: id(21),
                  event_ids: [id(22)],
                }
              : null,
        };
        if (options.wrongReceiptTask)
          value.receipt.task = { ...task, id: id(15) };
        if (options.wrongReceiptBlocker)
          value.receipt.blocker = { ...value.receipt.blocker, id: id(15) };
      }
      const bytes = options.badUTF8
        ? Uint8Array.of(255)
        : new TextEncoder().encode(
            options.badJSON
              ? "{"
              : JSON.stringify(
                  options.badSchema ? { ...value, title: 9 } : value,
                ),
          );
      const body = new ReadableStream({
        start(c) {
          c.enqueue(bytes);
          c.close();
        },
      });
      const get = body.getReader.bind(body),
        cancelStream = body.cancel.bind(body);
      body.getReader = (...args) => {
        const reader = get(...args),
          cancel = reader.cancel.bind(reader);
        reader.cancel = () => {
          readerEntered.resolve();
          return (
            options.readerHeld ? readerHold.promise : Promise.resolve()
          ).then(() => {
            if (options.readerRejected)
              throw Error("owned reader cancel failure");
            return cancel();
          });
        };
        return reader;
      };
      body.cancel = () => {
        streamEntered.resolve();
        return (
          options.streamHeld ? streamHold.promise : Promise.resolve()
        ).then(() => {
          if (options.streamRejected)
            throw Error("owned stream cancel failure");
          return cancelStream();
        });
      };
      return Promise.resolve(
        new Response(body, {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "Content-Length": String(bytes.length),
            "X-Request-ID": id(kind === "planning" ? 100 + plan.slot : 100),
          },
        }),
      );
    };
    vm.runInContext(code, ctx);
    const fetcher = (...args) => w.fetch(...args);
    const dependencies = Array(16).fill(undefined);
    dependencies[0] = w.ActualWork.createAccountAPI(fetcher);
    dependencies[15] = w.ActualWork.createWorkPlanningAPI(fetcher);
    const auth = w.ActualWork.createSessionController(...dependencies);
    await auth.restore();
    assert.equal(auth.state.phase, "authenticated");
    assert.equal(auth.state.busy, false);
    if (
      [
        "structure",
        "lookup-task",
        "lookup-blocker",
        "replay",
        "replay-task",
      ].includes(kind)
    ) {
      preparing = true;
      await auth.workPlanning
        .start(
          ["structure", "replay"].includes(kind)
            ? {
                domain: "structure",
                projectID: project,
                command: "work.milestone.update",
                targetID: target,
                expected_version: "1",
                request: { title: "original" },
              }
            : kind === "lookup-blocker"
              ? {
                  domain: "blocker",
                  projectID: project,
                  command: "work.task.blocker.add",
                  taskID: target,
                  expected_version: "1",
                  request: {
                    blocker_id: id(14),
                    type: "waiting_for_human",
                    description: "",
                    metadata: {},
                  },
                }
              : {
                  domain: "task",
                  projectID: project,
                  command: "work.task.update",
                  targetID: target,
                  expected_version: "1",
                  request: { plan: "original" },
                },
        )
        .catch(() => {});
      preparing = false;
      assert.equal(auth.workPlanning.progress.canLookup, true);
      if (kind === "replay") {
        lookupPreparing = true;
        await auth.workPlanning.checkOriginal();
        lookupPreparing = false;
        assert.equal(auth.workPlanning.progress.observation, "not_observed");
        assert.equal(auth.workPlanning.progress.canReplay, true);
      }
    }
    if (options.voidLookupFulfillment) {
      const original = auth.workPlanning.checkOriginal;
      auth.workPlanning.checkOriginal = (...args) =>
        original(...args).then(() => undefined);
    }
    if (options.voidFulfillment) {
      const method = kind === "planning" ? "start" : "retryOriginal";
      const original = auth.workPlanning[method];
      auth.workPlanning[method] = (...args) =>
        original(...args).then(() => undefined);
    }
    if (kind === "planning" && options.clonedFulfillment) {
      const original = auth.workPlanning.start;
      auth.workPlanning.start = (...args) =>
        original(...args).then((value) => structuredClone(value));
    }
    const actualPlanningTails = [];
    if (kind === "planning" && (options.oldPromise || options.oldReceipt)) {
      const original = auth.workPlanning.start;
      let oldPromise, oldReceipt;
      auth.workPlanning.start = (...args) => {
        const actual = original(...args);
        actualPlanningTails.push(
          actual.then(
            () => {},
            () => {},
          ),
        );
        const result =
          options.oldPromise && oldPromise
            ? oldPromise
            : options.oldReceipt && oldReceipt
              ? actual.then(() => oldReceipt)
              : actual;
        oldPromise ??= actual;
        void actual.then(
          (value) => {
            oldReceipt ??= value;
          },
          () => {},
        );
        return result;
      };
    }
    let originalStartPromise;
    if (kind === "planning") {
      const original = auth.workPlanning.start;
      auth.workPlanning.start = (...args) => {
        originalStartPromise = original(...args);
        return originalStartPromise;
      };
    }
    vm.runInContext(
      `this.installNative=${nativeCode};this.installPublic=${publicCode}`,
      ctx,
    );
    w.installNative({
      projects: [project],
      expiresAt: Date.now() + 45000,
      ...(kind === "planning" ? { planningPolicy: "planning-reorders" } : {}),
    });
    w.Function = function () {
      return async () => ({ singleton: () => auth });
    };
    assert.equal(
      await w.installPublic({
        binding: {
          entry: "/assets/main.js",
          asset: "/assets/session.js",
          export_name: "singleton",
        },
        expiresAt: Date.now() + 45000,
        ...(kind === "planning" ? { planningPolicy: "planning-reorders" } : {}),
      }),
      "installed",
    );
    if (kind === "planning" && !options.skipArm)
      assert.equal(
        w.__workPublicationDiagnostic.armPlanningReorder(plan),
        true,
      );
    if (kind === "replay-task") {
      lookupPreparing = true;
      await auth.workPlanning.checkOriginal();
      lookupPreparing = false;
      await drain();
    }
    if (["replay", "replay-task"].includes(kind) && !options.skipReplayArm) {
      const armed = w.__workPublicationDiagnostic.armReplay(
        kind === "replay-task" ? "historical-task" : "not-observed-milestone",
      );
      assert.equal(
        armed,
        !(options.uncommittedHistory || options.voidLookupFulfillment),
      );
    }
    let settled = false,
      outcome = "pending";
    const call = () => {
      const result =
        kind === "planning"
          ? auth.workPlanning.start({
              ...planningCommand(plan),
              ...options.command,
            })
          : ["replay", "replay-task"].includes(kind)
            ? auth.workPlanning.retryOriginal()
            : ["structure", "lookup-task", "lookup-blocker"].includes(kind)
              ? auth.workPlanning.checkOriginal()
              : auth.workPlanning[
                  kind === "milestone"
                    ? "getMilestone"
                    : kind === "sprint"
                      ? "getSprint"
                      : "getTask"
                ](project, target);
      if (kind === "planning") {
        assert.equal(
          startPromises.has(result),
          false,
          "each slot owns a new original Promise",
        );
        startPromises.add(result);
        specs.push({ ...plan });
        assert.equal(
          result,
          originalStartPromise,
          "public wrapper must return the exact original start Promise",
        );
      }
      void result.then(
        () => {
          settled = true;
          outcome = "fulfilled";
        },
        () => {
          settled = true;
          outcome = "rejected";
        },
      );
      return result;
    };
    const finish = () => ({
      planning: kind === "planning",
      specs: specs.map((v) => ({ ...v })),
      originalReplayBound,
      replayPolicy:
        kind === "replay-task" ? "historical-task" : "not-observed-milestone",
      public: w.__workPublicationDiagnostic.finish(),
      native: w.__workNativeDiagnostic.finish(),
    });
    return {
      auth,
      w,
      endpoint,
      call,
      plan: () => ({ ...plan }),
      joinActual: () => Promise.all(actualPlanningTails),
      expire: () => {
        w.Date.now = () => Date.now() + 46000;
      },
      end: () => w.__workPublicationDiagnostic.endPlanningReorder(plan.slot),
      next() {
        plan = planningSpec(plan.slot + 1);
        return w.__workPublicationDiagnostic.armPlanningReorder(plan);
      },
      readerEntered,
      streamEntered,
      releaseSuccessor: () => successorHold.resolve(),
      releaseReader: () => readerHold.resolve(),
      releaseStream: () => streamHold.resolve(),
      settled: () => settled,
      outcome: () => outcome,
      count: () => requestCount,
      timer() {
        const found = [...timers.values()].filter((t) => t.ms === 30000);
        assert.equal(found.length, 1);
        found[0].fn();
      },
      finish,
      async cleanup() {
        successorHold.resolve();
        readerHold.resolve();
        streamHold.resolve();
        auth.leave();
        await drain();
        finish();
        dom.window.close();
      },
    };
  }
  for (const kind of [
    "milestone",
    "sprint",
    "task",
    "structure",
    "lookup-task",
    "lookup-blocker",
    "replay",
  ])
    await check(
      "actual " +
        kind +
        " typed fulfillment follows real native and owner tails",
      async () => {
        const x = await setup(kind);
        try {
          await x.call();
          await drain();
          assert.equal(x.auth.state.busy, false);
          const e = x.finish(),
            n = e.native.requests[0],
            p = e.public.calls[0];
          assert.equal(x.count(), 1);
          assert.equal(p.fulfilled, 1);
          assert.equal(p.native_requests, 1);
          assert.equal(p.native_sequence, n.sequence);
          assert.equal(n.call_id, p.call_id);
          assert(n.eof_before_interruption && n.length_matches_before_binding);
          assert.equal(n.read_settled, n.read_calls);
          assert.equal(n.reader_cancel_settled, 1);
          assert.equal(n.stream_cancel_settled, 1);
          assert.equal(n.release_successes, 1);
          assert.equal(e.native.pending_at_retirement, 0);
          assert.equal(e.public.pending_at_retirement, 0);
          assert.equal(e.native.retirement_reason, "explicit");
          assert.equal(e.public.retirement_reason, "explicit");
          assert.equal(p.identity_current, true);
          assert.equal(p.not_busy, true);
          assert(!JSON.stringify(e).includes("private-material-canary"));
          assert.equal(accepts(e), true);
          if (kind === "milestone") positiveReport = reportOf(e);
          if (kind === "lookup-blocker") blockerReport = reportOf(e);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const kind of ["milestone", "structure", "lookup-blocker", "replay"])
    for (const held of ["reader", "stream"])
      await check(
        "actual " +
          kind +
          " " +
          held +
          " cancellation must join before normal visible fulfillment",
        async () => {
          const x = await setup(kind, { [held + "Held"]: true });
          try {
            const promise = x.call();
            await x[held + "Entered"].promise;
            await drain();
            assert.equal(x.settled(), false);
            assert.equal(x.auth.state.busy, true);
            if (kind === "lookup-blocker") {
              await assert.rejects(
                x.auth.workPlanning.getTask(project, target),
                (error) => error.kind === "busy",
              );
              assert.equal(x.count(), 1);
            }
            assert.equal(
              x.w.__workPublicationDiagnostic.snapshot().calls[0].fulfilled,
              0,
            );
            x[held === "reader" ? "releaseReader" : "releaseStream"]();
            await promise;
            await drain();
            assert.equal(x.outcome(), "fulfilled");
            assert.equal(x.auth.state.busy, false);
            const e = x.finish();
            assert.equal(e.public.calls[0].fulfilled, 1);
            assert.equal(e.native.pending_at_retirement, 0);
            assert.equal(e.public.pending_at_retirement, 0);
            assert.equal(accepts(e), true);
            assert.equal(x.count(), 1);
          } finally {
            await x.cleanup();
          }
        },
      );
  for (const kind of ["milestone", "lookup-blocker", "replay"])
    for (const ending of ["abandon", "timer", "identity"])
      await check(
        "actual early " +
          kind +
          " " +
          ending +
          " rejects visibly without releasing held original owner or late success",
        async () => {
          const x = await setup(kind, { streamHeld: true });
          try {
            const promise = x.call();
            await x.streamEntered.promise;
            await drain();
            if (ending === "abandon") {
              if (["lookup-blocker", "replay"].includes(kind))
                x.auth.workPlanning.abandon();
              else x.auth.workPlanning.abandonRead();
            } else if (ending === "timer") x.timer();
            else x.auth.leave();
            await promise.catch(() => {});
            await drain();
            assert.equal(x.outcome(), "rejected");
            assert.equal(x.auth.state.busy, true);
            assert.equal(
              x.w.__workPublicationDiagnostic.snapshot().calls[0].fulfilled,
              0,
            );
            x.releaseStream();
            await drain();
            assert.equal(x.auth.state.busy, false);
            const e = x.finish();
            assert.equal(e.public.calls[0].fulfilled, 0);
            assert.equal(e.public.calls[0].rejected, 1);
            assert.equal(e.native.requests[0].signal_aborted, true);
            assert.equal(accepts(e), false);
            if (ending === "identity")
              assert.equal(e.public.calls[0].identity_current, false);
            assert.equal(x.count(), 1);
          } finally {
            await x.cleanup();
          }
        },
      );
  for (const kind of ["milestone", "lookup-blocker"])
    for (const invalid of ["badJSON", "badUTF8", "badSchema"])
      await check(
        "actual malformed ordinary " +
          kind +
          " response with " +
          invalid +
          " cannot become typed completion",
        async () => {
          const x = await setup(kind, { [invalid]: true });
          try {
            await x.call().catch(() => {});
            await drain();
            const e = x.finish();
            assert.equal(
              e.native.requests[0].length_matches_before_binding,
              invalid !== "badUTF8",
            );
            assert.equal(e.public.calls[0].fulfilled, 0);
            assert.equal(e.public.calls[0].rejected, 1);
            assert.equal(x.auth.state.busy, false);
            assert.equal(accepts(e), false);
          } finally {
            await x.cleanup();
          }
        },
      );

  async function initializationControl(kind, fault = "none") {
    const selectedID = { milestone: id(12), sprint: id(13), task: target }[
      kind
    ];
    const route =
      "/owner/owned/tasks/explore" +
      (selectedID ? `/${kind}s/${selectedID}` : "");
    const dom = new JSDOM('<div id="app"></div>', {
      url: "https://owned.invalid" + route,
      runScripts: "outside-only",
    });
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
    const entered = deferred(),
      held = deferred();
    const wire = [],
      originals = [],
      returned = [],
      apiValues = [];
    let listPromise,
      listCount = 0,
      listSettled = false,
      listSignal;
    const ownProject = {
      id: project,
      owner_user_id: id(1),
      name: "owned",
      normalized_name: "owned",
      description: "",
      lifecycle: "active",
      version: "1",
      current_sprint_id: null,
      created_at: at,
      updated_at: at,
      archived_at: null,
    };
    const values = {
      milestone: { ...milestone, id: id(12) },
      sprint: { ...sprint, id: id(13) },
      task,
    };
    const reply = (value, sequence) => {
      const text = JSON.stringify(value);
      return new Response(text, {
        headers: {
          "Content-Type": "application/json",
          "Content-Length": String(Buffer.byteLength(text)),
          "X-Request-ID": id(sequence),
        },
      });
    };
    w.fetch = (url, init) => {
      if (url === "/api/v1/session") return Promise.resolve(reply(session, 90));
      if (
        !url.includes("/milestones") &&
        !url.includes("/sprints") &&
        !url.includes("/tasks")
      )
        return Promise.resolve(reply(ownProject, 91));
      wire.push(url);
      if (url === `/api/v1/projects/${project}/milestones?limit=50`) {
        listSignal = init.signal;
        entered.resolve();
        const { description, ...summary } = values.milestone;
        return held.promise.then(() => reply({ items: [summary] }, 92));
      }
      const selected = Object.entries(values).find(
        ([key, value]) =>
          url === `/api/v1/projects/${project}/${key}s/${value.id}`,
      );
      assert(selected, "only original formal initialization endpoints");
      return Promise.resolve(reply(selected[1], 100 + wire.length));
    };
    vm.runInContext(code, ctx);
    const A = w.ActualWork,
      deps = Array(16).fill(undefined);
    const fetcher = (...args) => w.fetch(...args);
    deps[0] = A.createAccountAPI(fetcher);
    deps[12] = A.createProjectOwnerAPI(fetcher);
    const workAPI = A.createWorkPlanningAPI(fetcher);
    for (const method of ["getMilestone", "getSprint", "getTask"]) {
      const actual = workAPI[method];
      workAPI[method] = (...args) => {
        const promise = actual(...args);
        void promise.then(
          (value) => apiValues.push(value),
          () => {},
        );
        return promise;
      };
    }
    deps[15] = workAPI;
    const auth = A.createSessionController(...deps);
    await auth.restore();
    let workspace;
    workspace = A.createProjectWorkspace(auth, async (path) => {
      w.history.replaceState({}, "", path);
      workspace.afterNavigation(path);
    });
    workspace.afterNavigation(route);
    await drain();
    assert.equal(workspace.detail.phase, "current");
    const app = A.createApp({
      setup() {
        A.provide(A.projectWorkspaceKey, workspace);
        return () => A.h("div", "actual workspace");
      },
    });
    app.mount(w.document.querySelector("#app"));
    const list = auth.workPlanning.listMilestones;
    auth.workPlanning.listMilestones = (...args) => {
      listCount++;
      listPromise = list(...args);
      void listPromise.then(
        () => {
          listSettled = true;
        },
        () => {
          listSettled = true;
        },
      );
      return listPromise;
    };
    for (const method of ["getMilestone", "getSprint", "getTask"]) {
      const actual = auth.workPlanning[method];
      auth.workPlanning[method] = (...args) => {
        const promise = actual(...args);
        originals.push({ method, promise, args });
        void promise.then(
          (value) => returned.push(value),
          () => {},
        );
        return promise;
      };
    }
    vm.runInContext(
      `this.installNative=${nativeCode};this.installPublic=${publicCode}`,
      ctx,
    );
    w.installNative({
      projects: [project],
      expiresAt: Date.now() + 45000,
      planningPolicy: "planning-reorders",
    });
    w.Function = function () {
      return async () => ({ singleton: () => auth });
    };
    const work = A.createProjectWorkPlanning(auth, workspace);
    try {
      work.afterNavigation(route);
      await entered.promise;
      await drain();
      assert(
        listPromise instanceof Promise ||
          typeof listPromise.then === "function",
      );
      assert.equal(listCount, 1);
      assert.equal(listSettled, false);
      assert.equal(originals.length, 0);
      assert.equal(auth.state.busy, true);
      // Re-entry while the same real owner holds the first list cannot prefetch details.
      work.afterNavigation(route);
      await drain();
      assert.equal(listCount, 1);
      assert.equal(originals.length, 0);
      if (fault === "already-started") {
        held.resolve();
        await listPromise;
        await drain();
      }
      const installed = await w.installPublic({
        binding: {
          entry: "/assets/main.js",
          asset: "/assets/session.js",
          export_name: "singleton",
          workspace_marker: "project-workspace",
        },
        expiresAt: Date.now() + 45000,
        planningPolicy: "planning-reorders",
        planningInitialization: { projectID: project, path: route },
      });
      if (fault === "already-started") {
        assert.equal(installed, "planning-context-unavailable");
        assert(originals.length > 0);
        return;
      }
      assert.equal(installed, "installed");
      const wrapped = new Map(),
        installedMethods = new Map();
      for (const method of ["getMilestone", "getSprint", "getTask"]) {
        const installedMethod = auth.workPlanning[method];
        installedMethods.set(method, installedMethod);
        auth.workPlanning[method] = (...args) => {
          const promise = installedMethod(...args);
          assert.equal(
            promise,
            originals.at(-1).promise,
            "wrapper returns same real Session Promise",
          );
          wrapped.set(promise, method);
          return promise;
        };
      }
      if (fault === "route")
        w.history.replaceState({}, "", "/owner/owned/tasks/explore");
      if (fault === "qualification") workspace.afterNavigation("/");
      if (fault === "timeout") {
        const ownerTimer = [...timers.values()].find((v) => v.ms === 30000);
        assert(ownerTimer);
        ownerTimer.fn();
      }
      if (fault === "closed") w.__workPublicationDiagnostic.finish();
      if (fault === "expired") {
        const expiry = [...timers.values()].find((v) => v.ms > 30000);
        assert(expiry);
        expiry.fn();
      }
      const accepted =
        w.__workPublicationDiagnostic.releasePlanningInitialization();
      assert.equal(
        accepted,
        fault === "none" || fault === "changed-after-release",
      );
      if (fault === "changed-after-release")
        w.history.replaceState({}, "", "/different");
      assert.equal(
        originals.length,
        0,
        "no detail entrance before releasing original list",
      );
      held.resolve();
      await listPromise.catch(() => {});
      await drain();
      await Promise.all(originals.map((row) => row.promise.catch(() => {})));
      await drain();
      assert.equal(listSettled, true);
      for (const [method, installed] of installedMethods)
        auth.workPlanning[method] = installed;
      if (fault === "timeout") assert.equal(listSignal.aborted, true);
      assert.equal(
        w.__workPublicationDiagnostic.planningInitializationComplete(),
        fault === "none",
      );
      if (fault === "none") {
        const expected = {
          explore: [],
          milestone: ["getMilestone"],
          sprint: ["getSprint", "getMilestone"],
          task: ["getTask", "getSprint", "getMilestone"],
        }[kind];
        assert.deepEqual(
          originals.map((v) => v.method),
          expected,
        );
        assert.equal(wrapped.size, expected.length);
        assert.equal(work.detail.phase, kind === "explore" ? "empty" : "ready");
        assert.equal(auth.state.busy, false);
        for (let i = 0; i < returned.length; i++)
          assert.equal(returned[i], apiValues[i]);
        const pub = w.__workPublicationDiagnostic.finish();
        const native = w.__workNativeDiagnostic.finish();
        assert.equal(pub.calls.length, expected.length);
        assert.equal(
          native.requests[0].call_id,
          null,
          "already-started list never retrobound",
        );
        assert.equal(pub.retirement_reason, "explicit");
        assert.equal(pub.pending_at_retirement, 0);
        assert.equal(pub.observer_failed, false);
        assert.equal(native.pending_at_retirement, 0);
        assert.equal(native.observer_failed, false);
        for (const call of pub.calls) {
          assert.equal(call.fulfilled, 1);
          assert.equal(call.result_kind, "typed-detail-returned");
          assert.equal(call.not_busy, true);
        }
      }
    } finally {
      held.resolve();
      await listPromise?.catch(() => {});
      await drain();
      await Promise.all(originals.map((row) => row.promise.catch(() => {})));
      work.dispose();
      workspace.dispose();
      w.__workPublicationDiagnostic?.finish();
      w.__workNativeDiagnostic?.finish();
      app.unmount();
      dom.window.close();
    }
  }
  for (const kind of ["explore", "milestone", "sprint", "task"])
    await check(
      "planning actual initial list precedes serial " + kind + " Session reads",
      () => initializationControl(kind),
    );
  for (const fault of [
    "already-started",
    "route",
    "qualification",
    "timeout",
    "closed",
    "expired",
    "changed-after-release",
  ])
    await check("planning actual initialization cannot bind " + fault, () =>
      initializationControl("task", fault),
    );

  // Predicate-only composition uses an explicit fourth empty document. The
  // real initializer/controller and Node document lifetime are controlled below.
  const fourthPlanningDocument = (parts) => {
    const doc = structuredClone(parts[2].documents[0]);
    doc.native.document_id = "01900000-0000-4000-8000-000000000004";
    doc.native.requests = [];
    doc.publication.calls = [];
    return doc;
  };
  const joinedPlanningReport = (parts) => {
    const documents = [
      ...parts.flatMap((p) => p.documents),
      fourthPlanningDocument(parts),
    ];
    return {
      ...parts[parts.length - 1],
      planning_joins_complete: true,
      planning_document_joins: documents.map((d) => ({
        document_id: d.native.document_id,
        joined: true,
        end_seen: true,
      })),
      planning_initializations: documents.map((d) => ({
        document_id: d.native.document_id,
        installed: true,
        released: true,
        route_joined: true,
        ready: true,
        invalid: false,
      })),
      requests: parts.flatMap((p) => p.requests),
      documents,
    };
  };
  const planningAccepts = (report) =>
    report.requests.length === 6 &&
    report.requests.every((r) =>
      judge(report, r.sequence, r.request_id, false, true),
    );
  const planningParts = [];
  for (const slot of [1, 3, 5])
    await check(
      "planning actual " +
        planningSpec(slot).kind +
        " before/tail original Promise and typed receipts",
      async () => {
        const x = await setup("planning", { slot });
        const receipts = [];
        try {
          for (let turn = 0; turn < 2; turn++) {
            const receipt = await x.call();
            await drain();
            assert.equal(receipt, x.auth.workPlanning.progress.receipt);
            assert(!receipts.includes(receipt));
            receipts.push(receipt);
            assert.equal(x.auth.state.busy, false);
            assert.equal(x.end(), true);
            if (!turn) assert.equal(x.next(), true);
          }
          const evidence = x.finish();
          assert.equal(evidence.public.calls.length, 2);
          assert.equal(evidence.native.retirement_reason, "explicit");
          assert.equal(evidence.public.retirement_reason, "explicit");
          assert.equal(evidence.native.pending_at_retirement, 0);
          assert.equal(evidence.public.pending_at_retirement, 0);
          for (const call of evidence.public.calls) {
            assert.equal(call.start_receipt_published, true);
            assert.equal(call.start_material_bound, true);
          }
          assert(!JSON.stringify(evidence).includes("private-material-canary"));
          planningParts.push(reportOf(evidence));
        } finally {
          await x.cleanup();
        }
      },
    );
  await check(
    "planning four independent documents retain six complete original consumers",
    () => {
      planningReport = joinedPlanningReport(planningParts);
      assert.equal(planningAccepts(planningReport), true);
      for (const row of planningReport.requests)
        assert.equal(
          judge(planningReport, row.sequence, row.request_id),
          false,
        );
      const normal = structuredClone(planningReport);
      for (const row of normal.requests) {
        row.failed_at = null;
        row.finished_event_at = 2;
        Object.assign(row.planning_reorder, {
          failedCount: 0,
          finishedCount: 1,
          finishedStarted: true,
          finishedNull: true,
        });
      }
      assert.equal(planningAccepts(normal), true);
      assert.deepEqual(
        planningReport.documents.map((d) =>
          d.publication.calls.map((c) => c.call_id),
        ),
        [[1, 2], [1, 2], [1, 2], []],
        "call IDs really reset across distinct documents",
      );
    },
  );
  for (const [issue, options] of [
    ...[
      "voidReceipt",
      "voidFulfillment",
      "clonedFulfillment",
      "badJSON",
      "badUTF8",
      "wrongVersion",
      "wrongTarget",
      "unchanged",
      "skipMaterial",
      "skipArm",
    ].map((name) => [name, { [name]: true }]),
    ...["key", "csrf", "origin", "body"].map((name) => [
      "wrong material " + name,
      { wrongMaterial: name },
    ]),
    ["wrong before", { command: { request: { before_id: id(15) } } }],
    ["wrong version argument", { command: { expected_version: "2" } }],
    ["Sprint wrong parent", { slot: 3, wrongParent: true }],
    ["Task wrong parent", { slot: 5, wrongParent: true }],
    ["Task wrong group", { slot: 5, wrongPriority: true }],
  ])
    await check(
      "planning actual typed input or receipt rejects " + issue,
      async () => {
        const x = await setup("planning", options);
        try {
          await x.call().catch(() => {});
          await drain();
          assert.equal(x.end(), false);
          const e = x.finish();
          assert.equal(
            e.public.calls.some(
              (c) =>
                c.start_receipt_published === true &&
                c.start_material_bound === true &&
                c.start_input_matches === true &&
                c.start_unique === true,
            ),
            false,
          );
          assert.equal(x.auth.state.busy, false);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const slot of [1, 3, 5])
    for (const tail of ["reader", "stream"])
      await check(
        "planning " +
          planningSpec(slot).kind +
          " held " +
          tail +
          " joins before first retirement",
        async () => {
          const x = await setup("planning", { slot, [tail + "Held"]: true });
          try {
            const promise = x.call();
            await x[tail + "Entered"].promise;
            await drain();
            assert.equal(x.settled(), false);
            assert.equal(x.auth.state.busy, true);
            assert.equal(
              x.w.__workPublicationDiagnostic.snapshot().calls[0]
                .start_receipt_published,
              false,
            );
            x[tail === "reader" ? "releaseReader" : "releaseStream"]();
            await promise;
            await drain();
            assert.equal(x.end(), true);
            const e = x.finish();
            assert.equal(e.public.calls[0].start_receipt_published, true);
            assert.equal(e.native.pending_at_retirement, 0);
            assert.equal(e.public.pending_at_retirement, 0);
          } finally {
            await x.cleanup();
          }
        },
      );
  for (const tail of ["reader", "stream"])
    await check(
      "planning first retirement pending " + tail + " cannot upgrade late",
      async () => {
        const x = await setup("planning", { [tail + "Held"]: true });
        try {
          const promise = x.call();
          await x[tail + "Entered"].promise;
          await drain();
          const early = x.finish();
          assert(early.public.pending_at_retirement > 0);
          assert(early.native.pending_at_retirement > 0);
          x[tail === "reader" ? "releaseReader" : "releaseStream"]();
          await promise;
          await drain();
          assert.equal(x.end(), false);
          const late = x.finish();
          assert(late.public.pending_at_retirement > 0);
          assert(late.native.pending_at_retirement > 0);
        } finally {
          await x.cleanup();
        }
      },
    );
  await check(
    "planning first wrong start occupies slot and cannot reselect",
    async () => {
      const x = await setup("planning", {
        command: { request: { before_id: id(15) } },
      });
      try {
        await x.call();
        await drain();
        await x.auth.workPlanning.start(planningCommand());
        await drain();
        assert.equal(x.end(), false);
        const e = x.finish();
        assert.equal(e.public.calls[0].start_unique, false);
        assert.equal(e.public.calls[0].start_input_matches, false);
      } finally {
        await x.cleanup();
      }
    },
  );
  for (const fault of ["oldPromise", "oldReceipt"])
    await check("planning second slot cannot reuse " + fault, async () => {
      const x = await setup("planning", { [fault]: true });
      try {
        await x.call();
        await drain();
        assert.equal(x.end(), true);
        assert.equal(x.next(), true);
        await x.auth.workPlanning.start(planningCommand(x.plan()));
        await x.joinActual();
        await drain();
        assert.equal(x.end(), false);
        const e = x.finish();
        assert.equal(e.public.calls[1].start_receipt_published, false);
      } finally {
        await x.cleanup();
      }
    });
  for (const slot of [1, 3, 5])
    await check(
      "planning " +
        planningSpec(slot).kind +
        " tail null differs from original omitted field",
      async () => {
        const x = await setup("planning", { slot });
        try {
          await x.call();
          await drain();
          assert.equal(x.end(), true);
          assert.equal(x.next(), true);
          const command = planningCommand(x.plan());
          command.request.before_id = null;
          await x.auth.workPlanning.start(command).catch(() => {});
          await drain();
          assert.equal(x.end(), false);
          const e = x.finish();
          assert.equal(e.public.calls[1].start_input_matches, false);
        } finally {
          await x.cleanup();
        }
      },
    );
  await check(
    "planning expiry while original outer held cannot accept late fulfillment",
    async () => {
      const x = await setup("planning", { streamHeld: true });
      try {
        const promise = x.call();
        await x.streamEntered.promise;
        x.expire();
        const early = x.finish();
        assert.notEqual(early.public.retirement_reason, "explicit");
        x.releaseStream();
        await promise;
        await drain();
        assert.equal(x.end(), false);
        assert.equal(x.finish().public.calls[0].start_receipt_published, false);
      } finally {
        await x.cleanup();
      }
    },
  );
  for (const [name, mutate] of [
    ["missing first document", (r) => r.documents.shift()],
    ["missing first join", (r) => r.planning_document_joins.shift()],
    ["prior failed join", (r) => (r.planning_document_joins[0].joined = false)],
    [
      "prior missing end",
      (r) => (r.planning_document_joins[0].end_seen = false),
    ],
    ["cumulative failure", (r) => (r.planning_joins_complete = false)],
    [
      "last document substitutes earlier",
      (r) =>
        (r.requests[0].planning_document_id =
          r.planning_document_joins[2].document_id),
    ],
    [
      "duplicate document",
      (r) =>
        (r.planning_document_joins[0].document_id =
          r.planning_document_joins[1].document_id),
    ],
    [
      "missing public reinstall",
      (r) => (r.documents[1].publication.calls = []),
    ],
    [
      "non explicit first retirement",
      (r) => (r.documents[0].publication.retirement_reason = "expired"),
    ],
    [
      "first pending at retirement",
      (r) => (r.documents[0].native.pending_at_retirement = 1),
    ],
    [
      "wrong call slot after reset",
      (r) => (r.documents[1].publication.calls[0].reorder_slot = 1),
    ],
    [
      "cross slot old XID",
      (r) => (r.requests[5].request_id = r.requests[0].request_id),
    ],
    ["missing sixth slot", (r) => r.requests.pop()],
    [
      "unjoined material",
      (r) => (r.requests[0].planning_reorder.joined = false),
    ],
    [
      "wrong receipt publication",
      (r) =>
        (r.documents[0].publication.calls[0].start_receipt_published = false),
    ],
    ["fourth document missing", (r) => r.documents.pop()],
    ["fourth original join missing", (r) => r.planning_document_joins.pop()],
    ["fourth public missing", (r) => (r.documents[3].publication = null)],
    [
      "first installation missing",
      (r) => (r.planning_initializations[0].installed = false),
    ],
    [
      "fourth route not joined",
      (r) => (r.planning_initializations[3].route_joined = false),
    ],
    [
      "initialization invalid",
      (r) => (r.planning_initializations[0].invalid = true),
    ],
    ["unknown policy", (r) => (r.planning_policy = "unknown")],
  ])
    await check("planning six conjunction rejects " + name, () => {
      const r = structuredClone(planningReport);
      mutate(r);
      assert.equal(planningAccepts(r), false);
    });
  for (const mode of [
    "complete",
    "missing-first-end",
    "first-pending",
    "first-non-explicit",
    "first-unjoined",
    "install-enum",
    "install-held",
    "release-false",
    "query",
    "wrong-first",
    "duplicate",
    "context-close",
    "continue-held",
    "unroute-held",
    "ready-false",
  ])
    await check(
      "planning actual Node four-document barrier and tails " + mode,
      async () => {
        const ts = require(root + "/web/node_modules/typescript");
        const source = fs.readFileSync(
          root +
            "/tests/account-captcha-web/e2e/project-work-planning.native.ts",
          "utf8",
        );
        const ast = ts.createSourceFile(
          "native.ts",
          source,
          ts.ScriptTarget.Latest,
          true,
        );
        const fn = ast.statements.find(
          (n) =>
            ts.isFunctionDeclaration(n) &&
            n.name?.text === "startWorkNativeDiagnostic",
        );
        const outputs = [],
          owned = [],
          held = deferred();
        const context = new (require("node:events").EventEmitter)();
        const page = new (require("node:events").EventEmitter)();
        const docs = [
          ...planningParts.map((p) => structuredClone(p.documents[0])),
          fourthPlanningDocument(planningParts),
        ];
        let documentIndex = 0,
          handler,
          match,
          continueCalls = 0,
          lateStarted = false,
          lateJoined = false;
        if (mode === "first-pending")
          docs[0].publication.pending_at_retirement = 1;
        if (mode === "first-non-explicit")
          docs[0].native.retirement_reason = "expired";
        page.context = () => context;
        page.addInitScript = async () => {};
        page.url = () => "https://owned.invalid/owner/owned/tasks/explore";
        page.route = async (m, h) => {
          match = m;
          handler = h;
        };
        page.unroute = async (m, h) => {
          assert.equal(m, match);
          assert.equal(h, handler);
          if (mode === "unroute-held") {
            lateStarted = true;
            await held.promise;
            lateJoined = true;
          }
          handler = null;
        };
        page.evaluate = async (f, arg) => {
          const d = docs[documentIndex],
            text = f.toString();
          if (arg?.planningInitialization) {
            if (mode === "install-held") {
              lateStarted = true;
              await held.promise;
              lateJoined = true;
            }
            if (mode === "context-close") context.emit("close");
            return mode === "install-enum" ? "already-installed" : "installed";
          }
          if (text.includes("releasePlanningInitialization"))
            return {
              accepted: mode !== "release-false",
              documentID: d.native.document_id,
            };
          if (text.includes("planningInitializationComplete"))
            return mode !== "ready-false";
          if (text.includes(".finish()") && documentIndex === 0) {
            if (mode === "missing-first-end")
              return { native: null, publication: d.publication };
            if (mode === "first-unjoined") {
              lateStarted = true;
              await held.promise;
              lateJoined = true;
            }
          }
          return { native: d.native, publication: d.publication };
        };
        const scope = vm.createContext({
          Map,
          Set,
          WeakMap,
          Promise,
          Error,
          Object,
          Array,
          URL,
          Date,
          Buffer,
          performance,
          setTimeout,
          clearTimeout,
          workSessionBinding: async () => ({}),
          installWorkNativeDiagnostic: () => {},
          installWorkPublicationDiagnostic: () => {},
          join: path.join,
          writeFileSync: (_name, value) => outputs.push(JSON.parse(value)),
        });
        vm.runInContext(
          ts.transpileModule(
            fn.getText(ast).replace(/^export /, "") +
              ";this.start=startWorkNativeDiagnostic;",
            { compilerOptions: { target: ts.ScriptTarget.ES2024 } },
          ).outputText,
          scope,
        );
        const diagnostic = await scope.start(page, {
          projects: [project],
          repository: root,
          evidence: "/controlled",
          classify: () => null,
          planningPolicy: "planning-reorders",
          ownPlanningTail(start) {
            const promise = Promise.resolve().then(start);
            owned.push(promise);
            return promise;
          },
        });
        let ready = false;
        const query = (suffix = "?limit=50") => ({
          method: () => "GET",
          postData: () => null,
          url: () =>
            `https://owned.invalid/api/v1/projects/${project}/milestones${suffix}`,
        });
        try {
          await drain();
          for (let i = 0; i < (mode === "complete" ? 4 : 1); i++) {
            documentIndex = i;
            await diagnostic.preparePlanningDocument(
              "/owner/owned/tasks/explore",
            );
            if (mode === "wrong-first")
              page.emit("request", query("/" + target));
            const q = query(mode === "query" ? "?limit=51" : "?limit=50");
            page.emit("request", q);
            const makeRoute = (request) => ({
              request: () => request,
              async continue(...args) {
                assert.equal(
                  args.length,
                  0,
                  "original continue never rewrites request",
                );
                continueCalls++;
                if (mode === "continue-held") {
                  lateStarted = true;
                  await held.promise;
                  lateJoined = true;
                }
              },
            });
            const routeTail = handler(makeRoute(q));
            await drain();
            if (mode === "duplicate") {
              const duplicate = query();
              page.emit("request", duplicate);
              await handler(makeRoute(duplicate));
            }
            if (mode !== "continue-held") await routeTail;
            try {
              await diagnostic.planningDocumentReady();
              ready = true;
            } catch (error) {
              assert.notEqual(mode, "complete");
              assert.equal(
                error.message,
                "WORK_PLANNING_INITIALIZATION_NOT_READY",
              );
            }
            if (mode === "complete") assert.equal(ready, true);
            if (
              mode.startsWith("first-") ||
              mode === "missing-first-end" ||
              mode === "complete"
            ) {
              if (i < 3)
                await diagnostic.flush().catch((error) => {
                  assert.notEqual(mode, "complete");
                  assert.equal(error.message, "WORK_REORDER_DOCUMENT_END");
                });
              else await diagnostic.finish();
            }
            if (mode !== "complete") {
              if (lateStarted) {
                assert.equal(lateJoined, false);
                held.resolve();
                await drain();
                assert.equal(lateJoined, true);
              }
              await routeTail;
              await diagnostic.finish();
              await assert.rejects(
                diagnostic.preparePlanningDocument(
                  "/owner/owned/tasks/explore",
                ),
                /INITIALIZATION_ARM/,
              );
            }
          }
          // Join every registered original operation, including timed-out operations
          // only after the controlled transport really settles them.
          await Promise.all(owned);
          const result = outputs.at(-1);
          assert.equal(result.planning_joins_complete, mode === "complete");
          if (mode === "complete") {
            assert.equal(result.planning_document_joins.length, 4);
            assert.equal(result.planning_initializations.length, 4);
            assert(
              result.planning_initializations.every(
                (v) => v.ready && v.route_joined,
              ),
            );
            assert.equal(continueCalls, 4);
          } else {
            if (mode === "context-close") assert.equal(continueCalls, 0);
            if (
              ["install-held", "continue-held", "unroute-held"].includes(mode)
            ) {
              assert(lateJoined);
              assert.equal(result.planning_initializations[0].ready, false);
            }
            if (mode === "first-unjoined")
              assert.equal(result.planning_document_joins[0].joined, false);
          }
        } finally {
          held.resolve();
          await Promise.allSettled(owned);
          await diagnostic.finish();
        }
      },
    );
  for (const issue of ["voidReceipt", "voidFulfillment", "badSchema"])
    await check("actual original replay rejects " + issue, async () => {
      const x = await setup("replay", { [issue]: true });
      try {
        await x.call().catch(() => {});
        await drain();
        const e = x.finish();
        assert.equal(e.public.calls[0].replay_receipt_published, false);
        assert.equal(accepts(e), false);
      } finally {
        await x.cleanup();
      }
    });
  await check(
    "actual original replay requires same request and published receipt",
    async () => {
      const x = await setup("replay");
      try {
        await x.call();
        await drain();
        const e = x.finish();
        assert.equal(e.public.calls[0].replay_receipt_published, true);
        assert.equal(accepts(e), true);
        const later = reportOf(e);
        Object.assign(later.requests[0], {
          original_replay_bound: false,
          replay_at_request_verified: false,
          original_replay_later_verified: true,
        });
        assert.equal(judge(later, 1, id(100)), true);
        for (const changes of [
          { original_replay_later_verified: false },
          { replay_actual_verified: false },
          { replay_headers_joined: false },
          { replay_at_request_verified: true },
          { replay_ended: false },
          { replay_invalid: true },
          { replay_verified_at: -1 },
          { replay_policy: "historical-task" },
        ]) {
          const bad = structuredClone(later);
          Object.assign(bad.requests[0], changes);
          assert.equal(judge(bad, 1, id(100)), false);
        }
        for (const field of [
          "replay_from_not_observed",
          "replay_receipt_published",
        ]) {
          const report = reportOf(e);
          report.documents[0].publication.calls[0][field] = false;
          assert.equal(judge(report, 1, id(100)), false);
        }
        e.originalReplayBound = false;
        assert.equal(accepts(e), false);
      } finally {
        await x.cleanup();
      }
    },
  );
  await check(
    "actual original replay cannot retire with a successor Work owner pending",
    async () => {
      const x = await setup("replay", { successorHeld: true });
      try {
        await x.call();
        await drain();
        const successor = x.auth.workPlanning
          .getMilestone(project, target)
          .catch(() => {});
        await drain();
        const e = x.finish();
        assert.equal(e.public.calls[0].replay_receipt_published, true);
        assert.equal(x.auth.state.busy, true);
        assert(e.public.pending_at_retirement > 0);
        assert.equal(accepts(e), false);
        x.releaseSuccessor();
        await successor;
        await drain();
        assert.equal(accepts(e), false);
      } finally {
        await x.cleanup();
      }
    },
  );

  for (const issue of [
    null,
    "uncommittedHistory",
    "voidLookupFulfillment",
    "differentHistory",
    "wrongReplayTask",
    "wrongReplayVersion",
    "voidFulfillment",
    "voidReceipt",
    "badJSON",
    "badUTF8",
  ]) {
    await check(
      "actual historical Task replay " +
        (issue ?? "joins original Lookup receipt and same replay publication"),
      async () => {
        const x = await setup("replay-task", issue ? { [issue]: true } : {});
        try {
          await x.call().catch(() => {});
          await drain();
          const e = x.finish();
          assert.equal(accepts(e), issue === null);
          if (!issue) {
            const report = reportOf(e),
              replay = report.requests.find((r) => r.request_id === id(100));
            for (const mutation of [
              (r) =>
                (r.requests.find(
                  (v) => v.request_id === id(100),
                ).replay_lookup_request_id = id(97)),
              (r) =>
                (r.documents[0].publication.calls[0].history_receipt_published = false),
              (r) =>
                (r.documents[0].native.requests[0].reader_cancel_settled = 0),
              (r) =>
                (r.documents[0].native.requests[0].stream_cancel_settled = 0),
              (r) =>
                (r.documents[0].publication.calls[1].history_lookup_call_id = 99),
              (r) => r.requests.push({ ...r.requests[0], sequence: 99 }),
            ]) {
              const bad = structuredClone(report);
              mutation(bad);
              assert.equal(judge(bad, replay.sequence, id(100)), false);
            }
          }
        } finally {
          await x.cleanup();
        }
      },
    );
  }
  for (const tail of ["reader", "stream"])
    await check(
      "actual historical Task replay waits original " + tail,
      async () => {
        const x = await setup("replay-task", { [tail + "Held"]: true });
        try {
          const original = x.call();
          await x[tail + "Entered"].promise;
          await drain();
          assert.equal(x.settled(), false);
          assert.equal(x.auth.state.busy, true);
          x[tail === "reader" ? "releaseReader" : "releaseStream"]();
          await original;
          await drain();
          assert.equal(accepts(x.finish()), true);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const kind of ["replay", "replay-task"])
    await check(
      "actual replay started before arm cannot acquire late publication proof: " +
        kind,
      async () => {
        const x = await setup(kind, { skipReplayArm: true, streamHeld: true });
        try {
          const promise = x.call();
          await x.streamEntered.promise;
          await drain();
          assert.equal(
            x.w.__workPublicationDiagnostic.armReplay(
              kind === "replay-task"
                ? "historical-task"
                : "not-observed-milestone",
            ),
            false,
          );
          x.releaseStream();
          await promise;
          await drain();
          assert.equal(accepts(x.finish()), false);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const ending of ["abandon", "identity"])
    await check(
      "actual historical Task replay early " +
        ending +
        " cannot upgrade after original outer tail",
      async () => {
        const x = await setup("replay-task", { streamHeld: true });
        try {
          const promise = x.call();
          await x.streamEntered.promise;
          await drain();
          if (ending === "abandon") x.auth.workPlanning.abandon();
          else x.auth.leave();
          await promise.catch(() => {});
          await drain();
          assert.equal(x.outcome(), "rejected");
          assert.equal(x.auth.state.busy, true);
          const evidence = x.finish();
          assert.equal(accepts(evidence), false);
          x.releaseStream();
          await drain();
          assert.equal(x.auth.state.busy, false);
          assert.equal(accepts(evidence), false);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const status of ["not_observed", "committed"])
    await check(
      "actual Blocker Lookup strict " +
        status +
        " response completes after owner tail",
      async () => {
        const x = await setup("lookup-blocker", { lookupStatus: status });
        try {
          const result = await x.call();
          await drain();
          assert.equal(result.domain, "blocker");
          assert.equal(result.value.status, status);
          assert.equal(x.auth.state.busy, false);
          const evidence = x.finish();
          assert.equal(evidence.public.calls[0].result_kind, status);
          assert.equal(accepts(evidence), true);
          assert.equal(x.count(), 1);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const issue of [
    "wrongReceiptTask",
    "wrongReceiptBlocker",
    "readerRejected",
    "streamRejected",
  ])
    await check(
      "actual Blocker Lookup rejects completion witness with " + issue,
      async () => {
        const x = await setup("lookup-blocker", {
          lookupStatus: "committed",
          [issue]: true,
        });
        try {
          await x.call().catch(() => {});
          await drain();
          const evidence = x.finish();
          assert.equal(x.auth.state.busy, false);
          if (issue.startsWith("wrongReceipt")) {
            assert.equal(evidence.public.calls[0].fulfilled, 0);
            assert.equal(evidence.public.calls[0].rejected, 1);
          } else {
            assert.equal(
              evidence.native.requests[0][
                issue === "readerRejected"
                  ? "reader_cancel_rejected"
                  : "stream_cancel_rejected"
              ],
              1,
            );
          }
          assert.equal(accepts(evidence), false);
          assert.equal(x.count(), 1);
        } finally {
          await x.cleanup();
        }
      },
    );
  for (const issue of ["wrong-target", "missing-public", "wrong-result"])
    await check("Blocker Lookup exact binding rejects " + issue, async () => {
      const report = structuredClone(blockerReport);
      const call = report.documents[0].publication.calls[0];
      if (issue === "wrong-target") call.target_id = id(15);
      if (issue === "missing-public")
        report.documents[0].publication.calls = [];
      if (issue === "wrong-result") call.result_kind = "other-returned";
      assert.equal(judge(report, 1, id(100)), false);
    });

  const negativeFields = [
    ["observation_finished", false],
    ["ordinary_finished_gate_unchanged", true],
    ["sample_joined", false],
    ["sample_join_unavailable", true],
    ["end_snapshot_observed", false],
    ["page_closed", true],
    ["context_closed", true],
    ["overflow", true],
    ["projection_rejected", 1],
    ["requests.0.sequence", 2],
    ["requests.0.request_id", id(101)],
    ["requests.0.status", 503],
    ["requests.0.declaration", "canceled-task-read"],
    ["requests.0.failed_at", null],
    ["requests.0.finished_event_at", 3],
    ["requests.0.method", "PATCH"],
    ["requests.0.path", `/api/v1/projects/${project}/milestones`],
    ["documents.0.source", "sample"],
    ["documents.0.end_snapshot_observed", false],
    ["documents.0.before_page_close", false],
    ...["native", "publication"].flatMap((o) =>
      [
        ["retired", false],
        ["retirement_reason", "expired"],
        ["pending_at_retirement", 1],
        ["pending_observations", 1],
        ["observer_failed", true],
        ["overflow", true],
      ].map(([key, value]) => [`documents.0.${o}.${key}`, value]),
    ),
    ...[
      ["bound_original_request", false],
      ["bound_public_call", false],
      ["pw_sequence", 2],
      ["declaration", "lost-task-update"],
      ["path", `/api/v1/projects/${project}/tasks/${target}`],
      ["status", 503],
      ["has_query", true],
      ["headers_seen", false],
      ["failure", "read-threw"],
      ["readers", 2],
      ["read_settled", 0],
      ["read_rejected", 1],
      ["read_done", false],
      ["eof_before_interruption", false],
      ["cancel_before_eof", true],
      ["signal_aborted_at_start", true],
      ["signal_aborted", true],
      ["abort_events", 1],
      ["content_length_present", false],
      ["content_length_valid", false],
      ["content_encoding_identity", false],
      ["content_length_comparable", false],
      ["content_length_matches_eof", false],
      ["content_length", 1],
      ["reader_cancel_settled", 0],
      ["reader_cancel_rejected", 1],
      ["stream_cancel_settled", 0],
      ["stream_cancel_rejected", 1],
      ["release_calls", 2],
      ["release_successes", 0],
      ["read_done_order", 9],
    ].map(([key, value]) => ["documents.0.native.requests.0." + key, value]),
    ...[
      ["native_requests", 2],
      ["native_sequence", 2],
      ["operation", "retryOriginal"],
      ["target_id", id(102)],
      ["entry_identity_matches", false],
      ["entry_not_busy", false],
      ["identity_current", false],
      ["authenticated", false],
      ["not_busy", false],
      ["fulfilled", 0],
      ["rejected", 1],
      ["synchronous_throws", 1],
      ["active", true],
      ["settled_at", null],
      ["result_kind", "other-returned"],
    ].map(([key, value]) => ["documents.0.publication.calls.0." + key, value]),
  ];
  for (const [field, value] of negativeFields)
    await check("completion conjunction rejects " + field, () => {
      const report = structuredClone(positiveReport),
        parts = field.split("."),
        key = parts.pop();
      let at = report;
      for (const part of parts) at = at[part];
      at[key] = value;
      assert.equal(judge(report, 1, id(100)), false);
    });
  for (const kind of ["pw", "document", "native", "public"])
    await check("completion rejects duplicate " + kind, () => {
      const report = structuredClone(positiveReport);
      if (kind === "pw")
        report.requests.push({ ...report.requests[0], sequence: 2 });
      else if (kind === "document")
        report.documents.push(structuredClone(report.documents[0]));
      else if (kind === "native")
        report.documents[0].native.requests.push({
          ...report.documents[0].native.requests[0],
          sequence: 2,
        });
      else
        report.documents[0].publication.calls.push({
          ...report.documents[0].publication.calls[0],
        });
      assert.equal(judge(report, 1, id(100)), false);
    });
  await check(
    "already-present DOM is independent from original typed completion",
    () => {
      const report = structuredClone(positiveReport),
        c = report.documents[0].publication.calls[0];
      c.entry_detail_target_present = true;
      c.detail_target_present = true;
      assert.equal(judge(report, 1, id(100)), true);
      c.fulfilled = 0;
      assert.equal(judge(report, 1, id(100)), false);
    },
  );
  await drain();
  assert.equal(failures.length, 0);
  console.log(
    JSON.stringify({
      passed: passed.length,
      controls: passed,
      unhandled: 0,
      actualSession: true,
      actualWorkAPI: true,
      actualTransport: true,
      browser: false,
      network: false,
    }),
  );
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
