import type {
  Page,
  Request as PWRequest,
  Response as PWResponse,
} from "@playwright/test";
import { writeFileSync } from "node:fs";
import { join } from "node:path";
import {
  workSessionBinding,
  installWorkPublicationDiagnostic,
} from "./project-work-planning.publication";

// Self-contained init script. Derived from Model's diagnostic-only
// sessionDiagnostics; no Model body collector or acceptance rule is installed.
export function installWorkNativeDiagnostic(config: {
  projects: string[];
  expiresAt: number;
  projectRefreshCompletion?: boolean;
}) {
  const host = window as any;
  if (host.__workNativeDiagnostic || Date.now() >= config.expiresAt) return;
  const originalFetch = window.fetch;
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const documentID = crypto.randomUUID();
  let retirementReason: "active" | "explicit" | "expired" = "active";
  let pendingAtRetirement: number | null = null;
  let retired = false,
    overflow = false,
    observerFailed = false,
    pending = 0;
  const entries: any[] = [],
    detach: (() => void)[] = [];
  const safe = (work: () => void) => {
    try {
      work();
    } catch {
      observerFailed = true;
    }
  };
  const replace = (object: any, key: string, value: any) => {
    const descriptor = Object.getOwnPropertyDescriptor(object, key);
    Object.defineProperty(object, key, {
      value,
      configurable: true,
      writable: true,
    });
    detach.push(() => {
      if (object[key] !== value) {
        observerFailed = true;
        return;
      }
      if (descriptor) Object.defineProperty(object, key, descriptor);
      else delete object[key];
    });
  };
  function observe<T>(
    promise: Promise<T>,
    done: (value: T) => void,
    failed: () => void,
  ) {
    if (retired) return;
    pending++;
    void promise
      .then(
        (value) => {
          if (!retired) safe(() => done(value));
        },
        () => {
          if (!retired) safe(failed);
        },
      )
      .catch(() => {
        observerFailed = true;
      })
      .then(() => {
        pending--;
      });
  }
  const wrapper: typeof window.fetch = (input, init) => {
    let url: URL;
    try {
      url = new URL(
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url,
        location.origin,
      );
    } catch {
      return Reflect.apply(originalFetch, window, [input, init]);
    }
    const method =
      init?.method ?? (input instanceof Request ? input.method : "GET");
    const match =
      /^\/api\/v1\/projects\/([^/]+)\/(milestones|sprints|tasks|structure-commands|task-commands)(?:\/([^/]+))?(?:\/(blockers|blocker-commands)(?:\/([^/]+))?)?$/.exec(
        url.pathname,
      );
    const projectRoot =
      config.projectRefreshCompletion === true &&
      method === "GET" &&
      !url.search &&
      /^\/api\/v1\/projects\/[^/]+$/.test(url.pathname) &&
      uuid.test(url.pathname.split("/")[4]!) &&
      config.projects.includes(url.pathname.split("/")[4]!);
    const selected =
      !retired &&
      url.origin === location.origin &&
      (projectRoot ||
        (match &&
          uuid.test(match[1]!) &&
          config.projects.includes(match[1]!) &&
          (!match[3] || uuid.test(match[3]) || match[3] === "lookup") &&
          (!match[5] || uuid.test(match[5]) || match[5] === "lookup") &&
          ["GET", "POST", "PATCH", "DELETE"].includes(method)));
    if (!selected || entries.length >= 256) {
      if (selected) overflow = true;
      return Reflect.apply(originalFetch, window, [input, init]);
    }
    const row: any = {
      sequence: entries.length + 1,
      method,
      path: url.pathname,
      has_query: !!url.search,
      request_id: null,
      call_id: null,
      status: 0,
      headers_seen: false,
      readers: 0,
      read_calls: 0,
      read_settled: 0,
      read_rejected: 0,
      bytes: 0,
      reader_cancel_calls: 0,
      reader_cancel_settled: 0,
      reader_cancel_rejected: 0,
      stream_cancel_calls: 0,
      stream_cancel_settled: 0,
      stream_cancel_rejected: 0,
      release_calls: 0,
      release_successes: 0,
      abort_events: 0,
      read_done: false,
      cancel_before_eof: false,
      signal_aborted_at_start: false,
      headers_order: 0,
      read_done_order: 0,
      read_rejected_order: 0,
      abort_order: 0,
      reader_cancel_order: 0,
      stream_cancel_order: 0,
      release_order: 0,
      content_length: 0,
      content_length_present: false,
      content_length_valid: false,
      content_encoding_identity: false,
      failure: "none",
    };
    entries.push(row);
    safe(() => {
      row.call_id =
        host.__workPublicationDiagnostic?.bindNative(
          method,
          url.pathname,
          row.sequence,
        ) ?? null;
    });
    let order = 0;
    const mark = (key: string) => {
      if (!row[key]) row[key] = ++order;
    };
    const fail = (code: string) => {
      if (row.failure === "none") row.failure = code;
    };
    const signal =
      init?.signal ?? (input instanceof Request ? input.signal : null);
    row.signal_aborted_at_start = signal?.aborted === true;
    row.signal_aborted = row.signal_aborted_at_start;
    const abort = () => {
      if (!retired) {
        row.signal_aborted = true;
        row.abort_events++;
        mark("abort_order");
      }
    };
    signal?.addEventListener("abort", abort, { once: true });
    detach.push(() => signal?.removeEventListener("abort", abort));
    let original: Promise<Response>;
    try {
      original = Reflect.apply(originalFetch, window, [input, init]);
    } catch (error) {
      fail("fetch-threw");
      throw error;
    }
    observe(
      original,
      (response) => {
        row.headers_seen = true;
        row.status = response.status;
        mark("headers_order");
        const id = response.headers.get("X-Request-ID");
        row.request_id = id && uuid.test(id) ? id : null;
        const length = response.headers.get("Content-Length"),
          encoding = response.headers.get("Content-Encoding");
        row.content_length_present = length !== null;
        row.content_length_valid =
          length !== null &&
          /^[0-9]+$/.test(length) &&
          Number.isSafeInteger(Number(length));
        if (row.content_length_valid) row.content_length = Number(length);
        row.content_encoding_identity =
          encoding === null || encoding.trim().toLowerCase() === "identity";
        const stream = response.body;
        if (!stream) return;
        const streamCancel = stream.cancel.bind(stream),
          getReader = stream.getReader.bind(stream);
        replace(
          stream,
          "cancel",
          (...args: Parameters<typeof stream.cancel>) => {
            if (!retired) {
              row.stream_cancel_calls++;
              row.cancel_before_eof ||= !row.read_done;
              mark("stream_cancel_order");
            }
            let result: ReturnType<typeof streamCancel>;
            try {
              result = streamCancel(...args);
            } catch (error) {
              if (!retired) fail("stream-cancel-threw");
              throw error;
            }
            observe(
              result,
              () => {
                row.stream_cancel_settled++;
              },
              () => {
                row.stream_cancel_settled++;
                row.stream_cancel_rejected++;
                fail("stream-cancel-rejected");
              },
            );
            return result;
          },
        );
        replace(stream, "getReader", (...args: unknown[]) => {
          if (!retired) row.readers++;
          let reader: ReadableStreamDefaultReader<Uint8Array>;
          try {
            reader = Reflect.apply(getReader, stream, args);
          } catch (error) {
            if (!retired) fail("get-reader-threw");
            throw error;
          }
          safe(() => {
            const read = reader.read.bind(reader),
              cancel = reader.cancel.bind(reader),
              release = reader.releaseLock.bind(reader);
            replace(reader, "read", (...args: unknown[]) => {
              if (!retired) row.read_calls++;
              let result: ReturnType<typeof read>;
              try {
                result = Reflect.apply(read, reader, args);
              } catch (error) {
                if (!retired) fail("read-threw");
                throw error;
              }
              observe(
                result,
                (value) => {
                  row.read_settled++;
                  if (value.done) {
                    row.read_done = true;
                    mark("read_done_order");
                  } else row.bytes += value.value.byteLength;
                },
                () => {
                  row.read_settled++;
                  row.read_rejected++;
                  mark("read_rejected_order");
                  fail("read-rejected");
                },
              );
              return result;
            });
            replace(
              reader,
              "cancel",
              (...args: Parameters<typeof reader.cancel>) => {
                if (!retired) {
                  row.reader_cancel_calls++;
                  row.cancel_before_eof ||= !row.read_done;
                  mark("reader_cancel_order");
                }
                let result: ReturnType<typeof cancel>;
                try {
                  result = cancel(...args);
                } catch (error) {
                  if (!retired) fail("reader-cancel-threw");
                  throw error;
                }
                observe(
                  result,
                  () => {
                    row.reader_cancel_settled++;
                  },
                  () => {
                    row.reader_cancel_settled++;
                    row.reader_cancel_rejected++;
                    fail("reader-cancel-rejected");
                  },
                );
                return result;
              },
            );
            replace(reader, "releaseLock", () => {
              if (!retired) {
                row.release_calls++;
                mark("release_order");
              }
              try {
                release();
                if (!retired) row.release_successes++;
              } catch (error) {
                if (!retired) fail("release-threw");
                throw error;
              }
            });
          });
          return reader;
        });
      },
      () => fail("fetch-rejected"),
    );
    return original;
  };
  const snapshot = () => ({
    document_id: documentID,
    retirement_reason: retirementReason,
    pending_at_retirement: pendingAtRetirement,
    retired,
    overflow,
    observer_failed: observerFailed,
    pending_observations: pending,
    clock: "browser-local-operation-order",
    requests: entries.map((row) => {
      const eof =
        row.read_done &&
        !row.cancel_before_eof &&
        !row.signal_aborted_at_start &&
        (!row.abort_order || row.read_done_order < row.abort_order) &&
        (!row.read_rejected_order ||
          row.read_done_order < row.read_rejected_order);
      return {
        ...row,
        eof_before_interruption: eof,
        length_comparable_before_binding:
          row.content_length_valid && row.content_encoding_identity && eof,
        length_matches_before_binding:
          row.content_length_valid &&
          row.content_encoding_identity &&
          eof &&
          row.bytes === row.content_length,
      };
    }),
  });
  const retire = (reason: "explicit" | "expired") => {
    if (retired) return;
    retirementReason = Date.now() >= config.expiresAt ? "expired" : reason;
    pendingAtRetirement = pending;
    retired = true;
    clearTimeout(timer);
    for (const stop of detach.splice(0)) safe(stop);
    if (window.fetch === wrapper) window.fetch = originalFetch;
    else observerFailed = true;
  };
  const timer = setTimeout(
    () => retire("expired"),
    Math.max(0, config.expiresAt - Date.now()),
  );
  window.fetch = wrapper;
  host.__workNativeDiagnostic = {
    snapshot,
    finish() {
      retire("explicit");
      return snapshot();
    },
  };
}

// This is only the native/public half of the recovery completion method.
// The caller still proves original PW events and the unchanged same-body,
// schema/client, UI, SQL and resource tails. No persisted report is read back.
export function workOrdinaryConsumption(
  report: any,
  sequence: number,
  requestID: string,
  projectRefresh = false,
): boolean {
  function completed(
    sequence: number,
    requestID: string,
    historyLookup?: { finished: boolean },
  ): boolean {
    const uuid =
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    const count = (n: unknown) =>
      typeof n === "number" && Number.isSafeInteger(n) && n > 0;
    const retired = (observer: any) =>
      observer?.retired === true &&
      observer.retirement_reason === "explicit" &&
      observer.pending_at_retirement === 0 &&
      observer.pending_observations === 0 &&
      observer.observer_failed === false &&
      observer.overflow === false;
    if (
      !report ||
      report.observation_finished !== true ||
      report.ordinary_finished_gate_unchanged !== false ||
      report.sample_joined !== true ||
      report.sample_join_unavailable !== false ||
      report.end_snapshot_observed !== true ||
      report.page_closed !== false ||
      report.context_closed !== false ||
      report.overflow !== false ||
      report.projection_rejected !== 0 ||
      !count(sequence) ||
      !uuid.test(requestID) ||
      !Array.isArray(report.requests) ||
      !Array.isArray(report.documents)
    )
      return false;
    const requests = report.requests.filter(
      (r: any) => r.sequence === sequence || r.request_id === requestID,
    );
    if (requests.length !== 1) return false;
    const pw = requests[0];
    if (
      pw.sequence !== sequence ||
      pw.request_id !== requestID ||
      pw.status !== 200 ||
      pw.declaration !== null ||
      !(
        ((projectRefresh || historyLookup?.finished === true) &&
          pw.failed_at === null &&
          Number.isFinite(pw.finished_event_at) &&
          (historyLookup?.finished === true ||
            (pw.project_terminal === "finished" &&
              pw.project_failed_count === 0 &&
              pw.project_finished_count === 1))) ||
        (pw.failed_at !== null &&
          Number.isFinite(pw.failed_at) &&
          pw.finished_event_at === null &&
          (!projectRefresh ||
            (pw.project_terminal === "failed" &&
              pw.project_failure_aborted === true &&
              pw.project_failed_count === 1 &&
              pw.project_finished_count === 0)))
      ) ||
      !Number.isFinite(pw.request_at) ||
      !Number.isFinite(pw.response_at) ||
      pw.request_at > pw.response_at ||
      pw.response_at > (pw.failed_at ?? pw.finished_event_at)
    )
      return false;
    const parts = typeof pw.path === "string" ? pw.path.split("/") : [];
    const project =
      projectRefresh &&
      parts.length === 5 &&
      pw.method === "GET" &&
      pw.has_query === false;
    const detail =
      parts.length === 7 &&
      pw.method === "GET" &&
      ["milestones", "sprints", "tasks"].includes(parts[5]) &&
      uuid.test(parts[6]);
    const lookup =
      parts.length === 7 &&
      pw.method === "POST" &&
      ["structure-commands", "task-commands"].includes(parts[5]) &&
      parts[6] === "lookup";
    const blockerLookup =
      parts.length === 9 &&
      pw.method === "POST" &&
      parts[5] === "tasks" &&
      uuid.test(parts[6]) &&
      parts[7] === "blocker-commands" &&
      parts[8] === "lookup";
    const originalReplay =
      parts.length === 7 &&
      pw.method === "PATCH" &&
      ["milestones", "tasks"].includes(parts[5]) &&
      uuid.test(parts[6]) &&
      (pw.original_replay_bound === true ||
        pw.original_replay_later_verified === true) &&
      pw.replay_at_request_verified === pw.original_replay_bound &&
      pw.replay_ended === true &&
      pw.replay_invalid === false &&
      Number.isFinite(pw.replay_request_at) &&
      Number.isFinite(pw.replay_verified_at) &&
      pw.replay_request_at <= pw.replay_verified_at &&
      pw.replay_policy ===
        (parts[5] === "tasks" ? "historical-task" : "not-observed-milestone");
    if (
      parts.slice(0, 4).join("/") !== "/api/v1/projects" ||
      !uuid.test(parts[4]) ||
      !(historyLookup
        ? lookup && parts[5] === "task-commands"
        : projectRefresh
          ? project
          : detail || lookup || blockerLookup || originalReplay)
    )
      return false;
    const matches = report.documents.flatMap((doc: any) =>
        (doc.native?.requests ?? [])
          .filter((n: any) => n.request_id === requestID)
          .map((native: any) => ({ doc, native })),
      ),
      pair = matches[0];
    if (matches.length !== 1 || !pair) return false;
    const { doc, native: n } = pair;
    if (
      doc.source !== "end" ||
      doc.end_snapshot_observed !== true ||
      doc.before_page_close !== true ||
      !retired(doc.native) ||
      !retired(doc.publication) ||
      n.bound_original_request !== true ||
      n.bound_public_call !== true ||
      n.pw_sequence !== sequence ||
      n.declaration !== null ||
      n.method !== pw.method ||
      n.path !== pw.path ||
      n.status !== 200 ||
      n.has_query !== false ||
      !count(n.sequence) ||
      !count(n.call_id) ||
      n.headers_seen !== true ||
      n.failure !== "none" ||
      n.readers !== 1 ||
      !count(n.read_calls) ||
      n.read_settled !== n.read_calls ||
      n.read_rejected !== 0 ||
      n.read_done !== true ||
      n.eof_before_interruption !== true ||
      n.cancel_before_eof !== false ||
      n.signal_aborted_at_start !== false ||
      n.signal_aborted !== false ||
      n.abort_events !== 0 ||
      n.abort_order !== 0 ||
      n.read_rejected_order !== 0 ||
      n.content_length_present !== true ||
      n.content_length_valid !== true ||
      n.content_encoding_identity !== true ||
      n.content_length_comparable !== true ||
      n.content_length_matches_eof !== true ||
      !count(n.bytes) ||
      n.bytes !== n.content_length ||
      n.reader_cancel_calls !== 1 ||
      n.reader_cancel_settled !== 1 ||
      n.reader_cancel_rejected !== 0 ||
      n.stream_cancel_calls !== 1 ||
      n.stream_cancel_settled !== 1 ||
      n.stream_cancel_rejected !== 0 ||
      n.release_calls !== 1 ||
      n.release_successes !== 1 ||
      ![
        n.headers_order,
        n.read_done_order,
        n.reader_cancel_order,
        n.release_order,
        n.stream_cancel_order,
      ].every(count) ||
      !(
        n.headers_order < n.read_done_order &&
        n.read_done_order < n.reader_cancel_order &&
        n.reader_cancel_order < n.release_order &&
        n.release_order < n.stream_cancel_order
      )
    )
      return false;
    const calls = doc.publication.calls.filter(
        (c: any) => c.call_id === n.call_id,
      ),
      call = calls[0];
    if (
      calls.length !== 1 ||
      doc.native.requests.filter((v: any) => v.call_id === n.call_id).length !==
        1 ||
      !call ||
      call.method !== pw.method ||
      call.path !== pw.path ||
      call.native_requests !== 1 ||
      call.native_sequence !== n.sequence ||
      !uuid.test(call.target_id) ||
      call.entry_identity_matches !== true ||
      call.entry_not_busy !== true ||
      call.identity_current !== true ||
      call.authenticated !== true ||
      call.not_busy !== true ||
      call.fulfilled !== 1 ||
      call.rejected !== 0 ||
      call.synchronous_throws !== 0 ||
      call.active !== false ||
      !Number.isFinite(call.call_at) ||
      !Number.isFinite(call.settled_at) ||
      !Number.isFinite(call.sample_at) ||
      call.call_at > call.settled_at ||
      call.settled_at > call.sample_at
    )
      return false;
    if (project)
      return (
        call.operation === "getProject" &&
        call.target_id === parts[4] &&
        call.result_kind === "typed-detail-returned" &&
        call.result_target_matches === true &&
        uuid.test(call.result_owner_id) &&
        call.result_lifecycle === "archived" &&
        typeof call.result_version === "string" &&
        /^[1-9][0-9]{0,18}$/.test(call.result_version) &&
        call.workspace_returned === 1 &&
        call.workspace_rejected === 0 &&
        call.workspace_published === true &&
        call.workspace_canonical === true &&
        Number.isFinite(call.workspace_settled_at) &&
        call.settled_at <= call.workspace_settled_at &&
        call.workspace_settled_at <= call.sample_at
      );
    if (historyLookup)
      return (
        call.operation === "checkOriginal" &&
        call.result_kind === "committed" &&
        call.history_receipt_published === true
      );
    if (originalReplay) {
      if (
        call.operation !== "retryOriginal" ||
        call.target_id !== parts[6] ||
        call.result_kind !== "typed-receipt-returned" ||
        call.replay_receipt_published !== true
      )
        return false;
      if (pw.replay_policy === "not-observed-milestone")
        return call.replay_from_not_observed === true;
      if (
        call.replay_from_history !== true ||
        !count(call.history_lookup_call_id) ||
        !uuid.test(pw.replay_lookup_request_id)
      )
        return false;
      const anchors = doc.native.requests.filter(
        (v: any) =>
          v.call_id === call.history_lookup_call_id &&
          v.request_id === pw.replay_lookup_request_id &&
          v.method === "POST" &&
          v.path === `/api/v1/projects/${parts[4]}/task-commands/lookup`,
      );
      const anchor = anchors[0],
        historicalCalls = doc.publication.calls.filter(
          (v: any) => v.call_id === call.history_lookup_call_id,
        );
      return (
        anchors.length === 1 &&
        historicalCalls.length === 1 &&
        historicalCalls[0].target_id === parts[6] &&
        historicalCalls[0].settled_at <= call.call_at &&
        completed(anchor.pw_sequence, pw.replay_lookup_request_id, {
          finished: pw.replay_lookup_finished === true,
        })
      );
    }
    return detail
      ? call.operation ===
          (
            {
              milestones: "getMilestone",
              sprints: "getSprint",
              tasks: "getTask",
            } as Record<string, string>
          )[parts[5]] &&
          call.target_id === parts[6] &&
          call.result_kind === "typed-detail-returned"
      : call.operation === "checkOriginal" &&
          (!blockerLookup || call.target_id === parts[6]) &&
          ["committed", "in_progress", "not_observed"].includes(
            call.result_kind,
          );
  }
  return completed(sequence, requestID);
}

export async function startWorkNativeDiagnostic(
  page: Page,
  config: {
    projects: string[];
    evidence: string;
    repository: string;
    classify: (request: PWRequest) => string | null;
    isOriginalReplay?: (request: PWRequest) => boolean;
    replayEvidence?: (request: PWRequest) => {
      policy: string;
      at_request_verified: boolean;
      later_verified: boolean;
      request_at: number | null;
      verified_at: number | null;
      ended: boolean;
      invalid: boolean;
      reason: string;
      lookup_request_id: string | null;
      lookup_finished: boolean;
    } | null;
    ordinaryCompletion?: boolean;
    projectRefreshCompletion?: boolean;
  },
) {
  const expiresAt = Date.now() + 45_000;
  const binding = await workSessionBinding(
    config.repository,
    config.projectRefreshCompletion === true,
  );
  await page.addInitScript(installWorkNativeDiagnostic, {
    projects: config.projects,
    expiresAt,
    projectRefreshCompletion: config.projectRefreshCompletion,
  });
  const rows = new Map<PWRequest, any>(),
    documents = new Map<string, any>();
  const projectWaiters = new Map<PWRequest, () => void>();
  const projectProofs = new Map<
    PWRequest,
    { requestID: string; bytes: number; ownerID: string; version: string }
  >();
  const closeProjectWaiters = () => {
    for (const resolve of projectWaiters.values()) resolve();
    projectWaiters.clear();
  };
  let finalReport: any = null;
  let stopped = false,
    paused = false,
    pending: Promise<void> | null = null,
    timer: ReturnType<typeof setTimeout> | undefined;
  let samples = 0,
    settled = 0,
    failed = 0,
    pageClosed = false,
    contextClosed = false,
    overflow = false,
    projectionRejected = 0;
  const start = performance.now();
  const at = () => performance.now() - start;
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const safePath = (path: unknown) => {
    if (typeof path !== "string") return false;
    if (
      config.projectRefreshCompletion === true &&
      /^\/api\/v1\/projects\/[^/]+$/.test(path)
    )
      return (
        uuid.test(path.split("/")[4]!) &&
        config.projects.includes(path.split("/")[4]!)
      );
    const match =
      /^\/api\/v1\/projects\/([^/]+)\/(milestones|sprints|tasks|structure-commands|task-commands)(?:\/([^/]+))?(?:\/(blockers|blocker-commands)(?:\/([^/]+))?)?$/.exec(
        path,
      );
    return (
      !!match &&
      uuid.test(match[1]!) &&
      config.projects.includes(match[1]!) &&
      (!match[3] || uuid.test(match[3]) || match[3] === "lookup") &&
      (!match[5] || uuid.test(match[5]) || match[5] === "lookup")
    );
  };
  const selected = (request: PWRequest) =>
    safePath(new URL(request.url()).pathname) &&
    ["GET", "POST", "PATCH", "DELETE"].includes(request.method());
  const requested = (request: PWRequest) => {
    if (stopped || !selected(request)) return;
    if (rows.size >= 256) {
      overflow = true;
      return;
    }
    rows.set(request, {
      sequence: rows.size + 1,
      method: request.method(),
      path: new URL(request.url()).pathname,
      request_id: null,
      original_replay_bound: config.isOriginalReplay?.(request) === true,
      request_at: at(),
      response_at: null,
      failed_at: null,
      finished_event_at: null,
      status: 0,
      ...(config.projectRefreshCompletion &&
      new URL(request.url()).pathname.split("/").length === 5
        ? {
            has_query: !!new URL(request.url()).search,
            project_failed_count: 0,
            project_finished_count: 0,
            project_terminal: null,
            project_failure_aborted: false,
          }
        : {}),
    });
  };
  const responded = (response: any) => {
    const row = rows.get(response.request());
    if (!row || stopped || pageClosed || contextClosed) return;
    const id = response.headers()["x-request-id"];
    row.request_id =
      typeof id === "string" &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        id,
      )
        ? id
        : null;
    row.status = response.status();
    row.response_at = at();
  };
  const requestFailed = (request: PWRequest) => {
    const row = rows.get(request);
    if (row && !stopped && !pageClosed && !contextClosed) {
      row.failed_at = at();
      if (row.project_failed_count !== undefined) {
        row.project_failed_count++;
        row.project_failure_aborted =
          request.failure()?.errorText === "net::ERR_ABORTED";
        projectWaiters.get(request)?.();
      }
    }
  };
  const requestFinished = (request: PWRequest) => {
    const row = rows.get(request);
    if (row && !stopped && !pageClosed && !contextClosed) {
      row.finished_event_at = at();
      if (row.project_finished_count !== undefined) {
        row.project_finished_count++;
        projectWaiters.get(request)?.();
      }
    }
  };
  const pageClose = () => {
    pageClosed = true;
    closeProjectWaiters();
  };
  const contextClose = () => {
    contextClosed = true;
    closeProjectWaiters();
  };
  page.on("request", requested);
  page.on("response", responded);
  page.on("requestfailed", requestFailed);
  page.on("requestfinished", requestFinished);
  page.on("close", pageClose);
  page.context().on("close", contextClose);
  // Copy only this observer's closed scalar vocabulary; never persist page objects.
  const scalar = (row: any, nums: string, bools: string) => {
    const result: any = {};
    for (const key of nums.split(" ")) {
      if (row[key] === undefined || row[key] === null) continue;
      if (
        typeof row[key] !== "number" ||
        !Number.isFinite(row[key]) ||
        row[key] < 0 ||
        row[key] > Number.MAX_SAFE_INTEGER
      )
        throw Error("number");
      result[key] = row[key];
    }
    for (const key of bools.split(" ")) {
      if (row[key] === undefined) continue;
      if (typeof row[key] !== "boolean") throw Error("boolean");
      result[key] = row[key];
    }
    return result;
  };
  const projectRow = (row: any, publication = false) => {
    if (
      !safePath(row.path) ||
      !["GET", "POST", "PATCH", "DELETE"].includes(row.method)
    )
      throw Error("path");
    const result = scalar(
      row,
      publication
        ? "history_lookup_call_id call_id call_at fulfilled rejected synchronous_throws native_requests native_sequence settled_at workspace_returned workspace_rejected workspace_settled_at detail_observed_after_fulfilled_at confirmed_observed_after_fulfilled_at sample_at"
        : "sequence call_id status readers read_calls read_settled read_rejected bytes reader_cancel_calls reader_cancel_settled reader_cancel_rejected stream_cancel_calls stream_cancel_settled stream_cancel_rejected release_calls release_successes abort_events headers_order read_done_order read_rejected_order abort_order reader_cancel_order stream_cancel_order release_order content_length",
      publication
        ? "history_receipt_published replay_from_history replay_from_not_observed replay_receipt_published workspace_published workspace_canonical result_target_matches entry_identity_matches entry_not_busy active identity_current authenticated not_busy detail_target_present entry_detail_target_present recovery_confirmed entry_recovery_confirmed recovery_uncertain replay_available"
        : "has_query headers_seen read_done cancel_before_eof signal_aborted_at_start signal_aborted content_length_present content_length_valid content_encoding_identity eof_before_interruption length_comparable_before_binding length_matches_before_binding",
    );
    result.method = row.method;
    result.path = row.path;
    if (publication) {
      if (
        !uuid.test(row.target_id) ||
        ![
          ...(config.projectRefreshCompletion ? ["getProject"] : []),
          "getMilestone",
          "getTask",
          "getSprint",
          "checkOriginal",
          "retryOriginal",
        ].includes(row.operation) ||
        ![
          "unobserved",
          "typed-detail-returned",
          "typed-receipt-returned",
          "committed",
          "in_progress",
          "not_observed",
          "other-returned",
        ].includes(row.result_kind)
      )
        throw Error("publication");
      result.target_id = row.target_id;
      result.operation = row.operation;
      result.result_kind = row.result_kind;
      if (row.operation === "getProject") {
        for (const key of [
          "result_owner_id",
          "result_lifecycle",
          "result_version",
        ]) {
          const value = row[key];
          if (value === undefined || value === null) continue;
          if (
            typeof value !== "string" ||
            !(key === "result_owner_id"
              ? uuid.test(value)
              : key === "result_lifecycle"
                ? ["active", "archiving", "archived"].includes(value)
                : /^[1-9][0-9]{0,18}$/.test(value))
          )
            throw Error("project-result");
          result[key] = value;
        }
      }
    } else {
      if (row.request_id !== null && !uuid.test(row.request_id))
        throw Error("request_id");
      if (
        ![
          "none",
          "fetch-threw",
          "fetch-rejected",
          "get-reader-threw",
          "read-threw",
          "read-rejected",
          "stream-cancel-threw",
          "stream-cancel-rejected",
          "reader-cancel-threw",
          "reader-cancel-rejected",
          "release-threw",
        ].includes(row.failure)
      )
        throw Error("failure");
      result.request_id = row.request_id;
      result.failure = row.failure;
    }
    return result;
  };
  const retirement = (reason: unknown) => {
    if (
      typeof reason !== "string" ||
      !["active", "explicit", "expired", "failed"].includes(reason)
    )
      throw Error("retirement");
    return reason;
  };
  function publish(value: any, source: "sample" | "end") {
    let native: any,
      publication: any = null;
    try {
      const raw = value?.native;
      if (!raw) return;
      if (
        !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          raw.document_id,
        ) ||
        !Array.isArray(raw.requests) ||
        raw.requests.length > 256
      )
        throw Error("document");
      native = {
        ...scalar(
          raw,
          "pending_observations pending_at_retirement",
          "retired overflow observer_failed",
        ),
        retirement_reason: retirement(raw.retirement_reason),
        document_id: raw.document_id,
        requests: raw.requests.map((row: any) => projectRow(row)),
      };
      if (value.publication) {
        const raw = value.publication;
        if (!Array.isArray(raw.calls) || raw.calls.length > 256)
          throw Error("calls");
        publication = {
          ...scalar(
            raw,
            "pending_observations pending_at_retirement",
            "retired overflow observer_failed",
          ),
          retirement_reason: retirement(raw.retirement_reason),
          calls: raw.calls.map((row: any) => projectRow(row, true)),
        };
      }
    } catch {
      projectionRejected++;
      return;
    }
    if (
      documents.get(native.document_id)?.source === "end" &&
      source === "sample"
    )
      return;
    if (!documents.has(native.document_id) && documents.size >= 8) {
      overflow = true;
      return;
    }
    documents.set(native.document_id, {
      source,
      observed_at: at(),
      before_page_close: !pageClosed && !contextClosed,
      native,
      publication,
    });
  }
  function sample() {
    if (stopped || paused || pending) return;
    samples++;
    const work = page.evaluate(() => ({
      native: (window as any).__workNativeDiagnostic?.snapshot() ?? null,
      publication:
        (window as any).__workPublicationDiagnostic?.snapshot() ?? null,
    }));
    const observed = work
      .then(
        (value) => {
          settled++;
          if (!stopped && !pageClosed && !contextClosed)
            publish(value, "sample");
        },
        () => {
          settled++;
          failed++;
        },
      )
      .catch(() => {
        failed++;
      });
    pending = observed;
    void observed
      .then(() => {
        if (pending === observed) pending = null;
        if (!stopped && !paused) timer = setTimeout(sample, 250);
      })
      .catch(() => {});
  }
  const boundedJoin = async (work: Promise<unknown>) => {
    let deadline: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        work.then(
          () => true,
          () => false,
        ),
        new Promise<false>((resolve) => {
          deadline = setTimeout(() => resolve(false), 250);
        }),
      ]);
    } finally {
      if (deadline) clearTimeout(deadline);
    }
  };
  function save(joined: boolean, endSeen: boolean) {
    const requests = [...rows].map(([request, row]) => {
      const replay = config.replayEvidence?.(request);
      return {
        ...row,
        ...(replay
          ? {
              replay_policy: replay.policy,
              replay_at_request_verified: replay.at_request_verified,
              original_replay_later_verified: replay.later_verified,
              replay_request_at: replay.request_at,
              replay_verified_at: replay.verified_at,
              replay_ended: replay.ended,
              replay_invalid: replay.invalid,
              replay_reason: replay.reason,
              replay_lookup_request_id: replay.lookup_request_id,
              replay_lookup_finished: replay.lookup_finished,
            }
          : {}),
        declaration: [
          "unforwarded-milestone-update",
          "lost-milestone-update",
          "lost-task-update",
          "lost-blocker-add",
          "canceled-task-read",
        ].includes(config.classify(request) ?? "")
          ? config.classify(request)
          : null,
      };
    });
    const observations = [...documents.values()].map((doc) => ({
      ...doc,
      end_snapshot_observed: doc.source === "end",
      native: {
        ...doc.native,
        requests: doc.native.requests.map((native: any) => {
          const matches = requests.filter(
            (row) =>
              native.request_id &&
              row.request_id === native.request_id &&
              row.method === native.method &&
              row.path === native.path &&
              row.status === native.status,
          );
          const globalMatches = [...documents.values()]
            .flatMap((d) => d.native.requests)
            .filter(
              (n: any) =>
                native.request_id && n.request_id === native.request_id,
            );
          const bound =
            matches.length === 1 &&
            globalMatches.length === 1 &&
            requests.filter(
              (row) =>
                native.request_id && row.request_id === native.request_id,
            ).length === 1;
          const calls = (doc.publication?.calls ?? []).filter(
            (call: any) =>
              call.call_id === native.call_id &&
              call.native_requests === 1 &&
              call.native_sequence === native.sequence &&
              call.method === native.method &&
              call.path === native.path &&
              call.entry_identity_matches === true,
          );
          const callBound =
            calls.length === 1 &&
            doc.native.requests.filter(
              (row: any) => row.call_id === native.call_id,
            ).length === 1;
          return {
            ...native,
            bound_public_call: bound && callBound,
            bound_original_request: bound,
            pw_sequence: bound ? matches[0]!.sequence : null,
            declaration: bound ? matches[0]!.declaration : null,
            content_length_comparable:
              bound && native.length_comparable_before_binding === true,
            content_length_matches_eof:
              bound && native.length_matches_before_binding === true,
          };
        }),
      },
    }));
    const report = {
      diagnostic_only: !config.ordinaryCompletion,
      ordinary_finished_gate_unchanged: !config.ordinaryCompletion,
      observation_finished: stopped,
      samples,
      sample_settled: settled,
      sample_failed: failed,
      sample_joined: joined,
      sample_join_unavailable: !joined,
      end_snapshot_observed: endSeen,
      page_closed: pageClosed,
      context_closed: contextClosed,
      overflow,
      projection_rejected: projectionRejected,
      node_clock: "monotonic-observed-relative-to-install",
      requests,
      documents: observations,
    };
    let json = JSON.stringify(report);
    if (Buffer.byteLength(json) > 2 * 1024 * 1024)
      json = JSON.stringify({
        ...report,
        overflow: true,
        evidence_omitted: "byte-limit",
        requests: [],
        documents: [],
      });
    finalReport = JSON.parse(json);
    writeFileSync(
      join(config.evidence, "work-native-consumption-diagnostic.json"),
      json,
      { mode: 0o600 },
    );
  }
  sample();
  return {
    async armOriginalReplay(
      policy: "not-observed-milestone" | "historical-task",
    ) {
      if (
        stopped ||
        pageClosed ||
        contextClosed ||
        Date.now() >= expiresAt ||
        !(await page.evaluate(
          (value) =>
            (window as any).__workPublicationDiagnostic?.armReplay(value) ===
            true,
          policy,
        ))
      )
        throw Error("WORK_PUBLIC_REPLAY_ARM_REJECTED");
    },
    async projectRefreshTerminal(response: PWResponse) {
      const request = response.request(),
        row = rows.get(request),
        url = new URL(response.url());
      if (
        !config.projectRefreshCompletion ||
        stopped ||
        pageClosed ||
        contextClosed ||
        !row ||
        request.method() !== "GET" ||
        url.search ||
        url.pathname.split("/").length !== 5 ||
        row.project_terminal !== null ||
        response.status() !== 200 ||
        projectWaiters.has(request)
      )
        throw Error("WORK_PROJECT_REFRESH_TERMINAL");
      let deadline: ReturnType<typeof setTimeout> | undefined;
      try {
        if (row.failed_at === null && row.finished_event_at === null)
          await new Promise<void>((resolve) => {
            projectWaiters.set(request, resolve);
            deadline = setTimeout(resolve, Math.max(0, expiresAt - Date.now()));
          });
        if (stopped || pageClosed || contextClosed || Date.now() >= expiresAt)
          throw Error("WORK_PROJECT_REFRESH_CLOSED");
        if (
          row.project_failed_count === 1 &&
          row.project_finished_count === 0 &&
          row.project_failure_aborted === true
        )
          row.project_terminal = "failed";
        else if (
          row.project_failed_count === 0 &&
          row.project_finished_count === 1 &&
          (await response.finished()) === null &&
          !stopped &&
          !pageClosed &&
          !contextClosed &&
          Date.now() < expiresAt
        )
          row.project_terminal = "finished";
        else throw Error("WORK_PROJECT_REFRESH_EVENTS");
      } finally {
        if (deadline) clearTimeout(deadline);
        projectWaiters.delete(request);
      }
    },
    recordProjectRefresh(
      request: PWRequest,
      proof: {
        requestID: string;
        bytes: number;
        ownerID: string;
        version: string;
      },
    ) {
      const row = rows.get(request);
      if (
        !config.projectRefreshCompletion ||
        stopped ||
        !row?.project_terminal ||
        projectProofs.has(request) ||
        row.request_id !== proof.requestID ||
        !uuid.test(proof.ownerID) ||
        !Number.isSafeInteger(proof.bytes) ||
        proof.bytes <= 0
      )
        throw Error("WORK_PROJECT_REFRESH_PROOF");
      projectProofs.set(request, { ...proof });
    },
    projectRefreshesComplete(expected: number) {
      return (
        config.projectRefreshCompletion === true &&
        stopped &&
        expected === 3 &&
        projectProofs.size === expected &&
        new Set(
          [...projectProofs.keys()].map(
            (request) => new URL(request.url()).pathname,
          ),
        ).size === expected &&
        [...projectProofs].every(([request, proof]) => {
          const row = rows.get(request);
          if (
            !row ||
            !workOrdinaryConsumption(
              finalReport,
              row.sequence,
              proof.requestID,
              true,
            )
          )
            return false;
          const matches = finalReport.documents.flatMap((doc: any) =>
            doc.native.requests
              .filter((n: any) => n.request_id === proof.requestID)
              .map((n: any) => ({ doc, n })),
          );
          if (matches.length !== 1) return false;
          const { doc, n } = matches[0],
            call = doc.publication.calls.find(
              (c: any) => c.call_id === n.call_id,
            );
          return (
            n.bytes === proof.bytes &&
            call.result_owner_id === proof.ownerID &&
            call.result_version === proof.version
          );
        })
      );
    },
    consumed(request: PWRequest, requestID: string) {
      const row = rows.get(request);
      return (
        config.ordinaryCompletion === true &&
        stopped &&
        !!row &&
        row.request_id === requestID &&
        workOrdinaryConsumption(finalReport, row.sequence, requestID)
      );
    },
    async flush() {
      closeProjectWaiters();
      paused = true;
      if (timer) clearTimeout(timer);
      let joined = !pending,
        endSeen = false;
      try {
        if (pending) joined = await boundedJoin(pending);
        if (joined && !pageClosed && !contextClosed) {
          let active = true;
          const work = page
            .evaluate(() => ({
              publication:
                (window as any).__workPublicationDiagnostic?.finish() ?? null,
              native: (window as any).__workNativeDiagnostic?.finish() ?? null,
            }))
            .then(
              (value) => {
                if (active && !pageClosed && !contextClosed) {
                  publish(value, "end");
                  endSeen = !!value.native;
                }
              },
              () => {},
            );
          pending = work;
          void work
            .finally(() => {
              if (pending === work) pending = null;
            })
            .catch(() => {});
          joined = await boundedJoin(work);
          active = false;
        }
      } finally {
        save(joined, endSeen);
        paused = false;
        if (!stopped) timer = setTimeout(sample, 250);
      }
    },
    async installPublication() {
      if (stopped) return;
      paused = true;
      if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      if (pending && !(await boundedJoin(pending))) {
        paused = false;
        return;
      }
      if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      const work = page.evaluate(installWorkPublicationDiagnostic, {
        binding,
        expiresAt,
      });
      const observed = work
        .then(
          () => {},
          () => {
            failed++;
          },
        )
        .catch(() => {
          failed++;
        });
      pending = observed;
      paused = false;
      void observed
        .then(() => {
          if (pending === observed) pending = null;
          if (!stopped && !paused) timer = setTimeout(sample, 250);
        })
        .catch(() => {});
      await boundedJoin(observed);
    },
    async finish() {
      if (stopped) return;
      stopped = true;
      closeProjectWaiters();
      if (timer) clearTimeout(timer);
      let joined = !pending,
        endSeen = false;
      try {
        if (pending) joined = await boundedJoin(pending);
        if (joined && !pageClosed && !contextClosed) {
          const work = page.evaluate(() => ({
            publication:
              (window as any).__workPublicationDiagnostic?.finish() ?? null,
            native: (window as any).__workNativeDiagnostic?.finish() ?? null,
          }));
          let endActive = true;
          const observed = work
            .then(
              (value) => {
                if (endActive && !pageClosed && !contextClosed) {
                  publish(value, "end");
                  endSeen = !!value.native;
                }
              },
              () => {},
            )
            .catch(() => {});
          joined = await boundedJoin(observed);
          endActive = false;
        }
      } finally {
        page.off("request", requested);
        page.off("response", responded);
        page.off("requestfailed", requestFailed);
        page.off("requestfinished", requestFinished);
        page.off("close", pageClose);
        page.context().off("close", contextClose);
        save(joined, endSeen);
      }
    },
  };
}
