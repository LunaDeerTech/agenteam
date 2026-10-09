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
  let positiveReport, blockerReport;
  function reportOf(e) {
    const n = e.native.requests[0];
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
      requests: [
        {
          sequence: 1,
          request_id: n.request_id,
          method: n.method,
          path: n.path,
          status: n.status,
          declaration: null,
          request_at: 0,
          response_at: 1,
          failed_at: 2,
          finished_event_at: null,
        },
      ],
      documents: [
        {
          source: "end",
          end_snapshot_observed: true,
          before_page_close: true,
          native: {
            ...e.native,
            requests: [
              {
                ...n,
                bound_original_request: true,
                bound_public_call: true,
                pw_sequence: 1,
                declaration: null,
                content_length_comparable: n.length_comparable_before_binding,
                content_length_matches_eof: n.length_matches_before_binding,
              },
            ],
          },
          publication: e.public,
        },
      ],
    };
    return JSON.parse(JSON.stringify(report));
  }
  const accepts = (e) => judge(reportOf(e), 1, id(100));
  const passed = [];
  async function check(name, fn) {
    await fn();
    passed.push(name);
  }
  async function setup(kind = "milestone", options = {}) {
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
      preparing = false;
    const readerHold = deferred(),
      streamHold = deferred(),
      readerEntered = deferred(),
      streamEntered = deferred();
    const endpoint =
      kind === "lookup-blocker"
        ? `/api/v1/projects/${project}/tasks/${target}/blocker-commands/lookup`
        : ["structure", "lookup-task", "lookup-blocker"].includes(kind)
          ? `/api/v1/projects/${project}/${kind === "structure" ? "structure" : "task"}-commands/lookup`
          : `/api/v1/projects/${project}/${kind === "milestone" ? "milestones" : kind === "sprint" ? "sprints" : "tasks"}/${target}`;
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
      if (preparing)
        return Promise.reject(Error("owned preparation transport failure"));
      assert.equal(url, endpoint);
      assert.equal(
        init.method,
        ["structure", "lookup-task", "lookup-blocker"].includes(kind)
          ? "POST"
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
            "X-Request-ID": id(100),
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
    if (["structure", "lookup-task", "lookup-blocker"].includes(kind)) {
      preparing = true;
      await auth.workPlanning
        .start(
          kind === "structure"
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
    }
    vm.runInContext(
      `this.installNative=${nativeCode};this.installPublic=${publicCode}`,
      ctx,
    );
    w.installNative({ projects: [project], expiresAt: Date.now() + 45000 });
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
      }),
      "installed",
    );
    let settled = false,
      outcome = "pending";
    const call = () => {
      const result = ["structure", "lookup-task", "lookup-blocker"].includes(
        kind,
      )
        ? auth.workPlanning.checkOriginal()
        : auth.workPlanning[
            kind === "milestone"
              ? "getMilestone"
              : kind === "sprint"
                ? "getSprint"
                : "getTask"
          ](project, target);
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
      public: w.__workPublicationDiagnostic.finish(),
      native: w.__workNativeDiagnostic.finish(),
    });
    return {
      auth,
      w,
      endpoint,
      call,
      readerEntered,
      streamEntered,
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
  for (const kind of ["milestone", "structure", "lookup-blocker"])
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
  for (const kind of ["milestone", "lookup-blocker"])
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
              if (kind === "lookup-blocker") x.auth.workPlanning.abandon();
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
