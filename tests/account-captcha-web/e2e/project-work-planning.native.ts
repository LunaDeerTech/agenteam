import type { Page, Request as PWRequest } from "@playwright/test";
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
}) {
  const host = window as any;
  if (host.__workNativeDiagnostic || Date.now() >= config.expiresAt) return;
  const originalFetch = window.fetch;
  const uuid =
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const documentID = crypto.randomUUID();
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
    const selected =
      !retired &&
      url.origin === location.origin &&
      match &&
      uuid.test(match[1]!) &&
      config.projects.includes(match[1]!) &&
      (!match[3] || uuid.test(match[3]) || match[3] === "lookup") &&
      (!match[5] || uuid.test(match[5]) || match[5] === "lookup") &&
      ["GET", "POST", "PATCH", "DELETE"].includes(method);
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
            const result = streamCancel(...args);
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
          const read = reader.read.bind(reader),
            cancel = reader.cancel.bind(reader),
            release = reader.releaseLock.bind(reader);
          replace(reader, "read", (...args: unknown[]) => {
            if (!retired) row.read_calls++;
            const result = Reflect.apply(read, reader, args) as ReturnType<
              typeof read
            >;
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
              const result = cancel(...args);
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
          return reader;
        });
      },
      () => fail("fetch-rejected"),
    );
    return original;
  };
  const snapshot = () => ({
    document_id: documentID,
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
  const retire = () => {
    if (retired) return;
    retired = true;
    clearTimeout(timer);
    for (const stop of detach.splice(0)) safe(stop);
    if (window.fetch === wrapper) window.fetch = originalFetch;
    else observerFailed = true;
  };
  const timer = setTimeout(retire, Math.max(0, config.expiresAt - Date.now()));
  window.fetch = wrapper;
  host.__workNativeDiagnostic = {
    snapshot,
    finish() {
      retire();
      return snapshot();
    },
  };
}

export async function startWorkNativeDiagnostic(
  page: Page,
  config: {
    projects: string[];
    evidence: string;
    repository: string;
    classify: (request: PWRequest) => string | null;
  },
) {
  const expiresAt = Date.now() + 45_000;
  const binding = await workSessionBinding(config.repository);
  await page.addInitScript(installWorkNativeDiagnostic, {
    projects: config.projects,
    expiresAt,
  });
  const rows = new Map<PWRequest, any>(),
    documents = new Map<string, any>();
  let stopped = false,
    pending: Promise<void> | null = null,
    timer: ReturnType<typeof setTimeout> | undefined;
  let samples = 0,
    settled = 0,
    failed = 0,
    pageClosed = false,
    contextClosed = false,
    overflow = false;
  const start = performance.now();
  const at = () => performance.now() - start;
  const selected = (request: PWRequest) => {
    const url = new URL(request.url());
    return /^\/api\/v1\/projects\/[^/]+\/(milestones|sprints|tasks|structure-commands|task-commands)(?:\/|$)/.test(
      url.pathname,
    );
  };
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
      request_at: at(),
      response_at: null,
      failed_at: null,
      finished_event_at: null,
      status: 0,
    });
  };
  const responded = (response: any) => {
    const row = rows.get(response.request());
    if (!row || stopped) return;
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
    if (row && !stopped) row.failed_at = at();
  };
  const requestFinished = (request: PWRequest) => {
    const row = rows.get(request);
    if (row && !stopped) row.finished_event_at = at();
  };
  const pageClose = () => {
    pageClosed = true;
  };
  const contextClose = () => {
    contextClosed = true;
  };
  page.on("request", requested);
  page.on("response", responded);
  page.on("requestfailed", requestFailed);
  page.on("requestfinished", requestFinished);
  page.on("close", pageClose);
  page.context().on("close", contextClose);
  function publish(value: any, source: "sample" | "end") {
    const native = value?.native;
    if (
      !native ||
      typeof native.document_id !== "string" ||
      !Array.isArray(native.requests) ||
      native.requests.length > 256
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
      publication: value.publication ?? null,
    });
  }
  function sample() {
    if (stopped || pending) return;
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
          if (!stopped) publish(value, "sample");
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
        if (!stopped) timer = setTimeout(sample, 250);
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
    const requests = [...rows].map(([request, row]) => ({
      ...row,
      declaration: config.classify(request),
    }));
    const observations = [...documents.values()].map((doc) => ({
      ...doc,
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
          const bound = matches.length === 1 && globalMatches.length === 1;
          return {
            ...native,
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
    writeFileSync(
      join(config.evidence, "work-native-consumption-diagnostic.json"),
      JSON.stringify({
        diagnostic_only: true,
        ordinary_finished_gate_unchanged: true,
        samples,
        sample_settled: settled,
        sample_failed: failed,
        sample_joined: joined,
        sample_join_unavailable: !joined,
        end_snapshot_observed: endSeen,
        page_closed: pageClosed,
        context_closed: contextClosed,
        overflow,
        node_clock: "monotonic-observed-relative-to-install",
        requests,
        documents: observations,
      }),
      { mode: 0o600 },
    );
  }
  sample();
  return {
    async flush() {
      if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      if (!pending) sample();
      if (pending) await boundedJoin(pending);
      save(false, false);
    },
    async installPublication() {
      if (stopped) return;
      if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      if (pending && !(await boundedJoin(pending))) return;
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
      void observed
        .then(() => {
          if (pending === observed) pending = null;
          if (!stopped) timer = setTimeout(sample, 250);
        })
        .catch(() => {});
      await boundedJoin(observed);
    },
    async finish() {
      if (stopped) return;
      stopped = true;
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
