import type { Page, Request, Response } from "@playwright/test";

export const emptyNativeDiagnostic = () => ({
  binding: "unobserved",
  sample_count: 0,
  sample_settled: 0,
  sample_failed: 0,
  sample_joined: false,
  hooks_retired: false,
  eof_before_interruption: false,
  length_comparable: false,
  length_matches: false,
  facts: null as Record<string, unknown> | null,
});

// Diagnostic contract adapted from Model sessionDiagnostics, blob at 2b3beb81.
// Installed in the original page; never reads, clones or cancels a body itself.
export function installVariableNativeDiagnostic() {
  const host = window as any;
  if (host.__variableNativeDiagnostic) return;
  const originalFetch = window.fetch;
  const records: any[] = [],
    restorers: (() => void)[] = [];
  let retired = false,
    hooksRetired = false;
  const documentID = crypto.randomUUID();
  const uuid =
    "[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
  const selected = new RegExp(
    `^/api/v1/projects/${uuid}/variables(?:/${uuid}|/commands/lookup)?$`,
  );
  function retire() {
    if (retired) return;
    retired = true;
    clearTimeout(timer);
    hooksRetired = true;
    for (const restore of restorers.reverse()) {
      try {
        restore();
      } catch {
        hooksRetired = false;
      }
    }
    if (window.fetch === fetcher) {
      try {
        window.fetch = originalFetch;
      } catch {
        hooksRetired = false;
      }
    }
  }
  function fetcher(
    this: unknown,
    ...args: Parameters<typeof fetch>
  ): ReturnType<typeof fetch> {
    const pending = Reflect.apply(originalFetch, this, args) as ReturnType<
      typeof fetch
    >;
    if (retired) return pending;
    try {
      const [input, init] = args;
      const url = new URL(
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url,
        location.origin,
      );
      const method =
        init?.method ?? (input instanceof Request ? input.method : "GET");
      if (
        retired ||
        records.length >= 256 ||
        url.origin !== location.origin ||
        !selected.test(url.pathname) ||
        !["GET", "POST", "PATCH", "DELETE"].includes(method)
      )
        return pending;
      const facts = {
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
        read_done: false,
        cancel_before_eof: false,
        signal_aborted_at_start: false,
        failure: "none",
      };
      const record = {
        method,
        path: url.pathname,
        query: url.search,
        request_id: "",
        status: 0,
        facts,
      };
      records.push(record);
      let sequence = 0;
      const mark = (
        key:
          | "headers_order"
          | "read_done_order"
          | "read_rejected_order"
          | "abort_order"
          | "reader_cancel_order"
          | "stream_cancel_order"
          | "release_order",
      ) => {
        if (!facts[key]) facts[key] = ++sequence;
      };
      const failed = (reason: string) => {
        if (facts.failure === "none") facts.failure = reason;
      };
      const signal =
        init?.signal ?? (input instanceof Request ? input.signal : null);
      facts.signal_aborted_at_start = signal?.aborted === true;
      const abort = () => {
        if (!retired) {
          facts.abort_events++;
          mark("abort_order");
        }
      };
      signal?.addEventListener("abort", abort, { once: true });
      restorers.push(() => signal?.removeEventListener("abort", abort));
      function observe<T>(
        promise: Promise<T>,
        fulfilled: (value: T) => void,
        rejected: () => void,
      ) {
        void promise
          .then(
            (value) => {
              if (!retired) fulfilled(value);
            },
            () => {
              if (!retired) rejected();
            },
          )
          .catch(() => {
            if (!retired) facts.failure = "observer-error";
          });
      }
      function wrap(object: any, key: string, replacement: any) {
        try {
          const descriptor = Object.getOwnPropertyDescriptor(object, key);
          Object.defineProperty(object, key, {
            configurable: true,
            writable: true,
            value: replacement,
          });
          restorers.push(() => {
            if (object[key] !== replacement) return;
            if (descriptor) Object.defineProperty(object, key, descriptor);
            else delete object[key];
          });
        } catch {
          // Installation is diagnostic-only. Preserve an already successful
          // original getReader(), even if its reader cannot be instrumented.
          facts.failure = "observer-error";
        }
      }
      observe(
        pending,
        (response) => {
          record.request_id = response.headers.get("X-Request-ID") ?? "";
          record.status = response.status;
          mark("headers_order");
          const length = response.headers.get("Content-Length"),
            encoding = response.headers.get("Content-Encoding");
          facts.content_length_present = length !== null;
          facts.content_length_valid =
            length !== null &&
            /^[0-9]+$/.test(length) &&
            Number.isSafeInteger(Number(length));
          if (facts.content_length_valid) facts.content_length = Number(length);
          facts.content_encoding_identity =
            encoding === null || encoding.trim().toLowerCase() === "identity";
          const stream = response.body;
          if (!stream) return;
          const streamCancel = stream.cancel,
            getReader = stream.getReader;
          wrap(
            stream,
            "cancel",
            function (
              this: typeof stream,
              ...values: Parameters<typeof stream.cancel>
            ) {
              if (retired) return Reflect.apply(streamCancel, this, values);
              facts.stream_cancel_calls++;
              facts.cancel_before_eof ||= !facts.read_done;
              mark("stream_cancel_order");
              let result: ReturnType<typeof stream.cancel>;
              try {
                result = Reflect.apply(streamCancel, this, values);
              } catch (error) {
                failed("stream-cancel-threw");
                throw error;
              }
              observe(
                result,
                () => {
                  facts.stream_cancel_settled++;
                },
                () => {
                  facts.stream_cancel_settled++;
                  facts.stream_cancel_rejected++;
                  failed("stream-cancel-rejected");
                },
              );
              return result;
            },
          );
          wrap(
            stream,
            "getReader",
            function (this: typeof stream, ...values: unknown[]) {
              if (retired) return Reflect.apply(getReader, this, values);
              facts.readers++;
              let reader: ReadableStreamDefaultReader<Uint8Array>;
              try {
                reader = Reflect.apply(getReader, this, values);
              } catch (error) {
                failed("get-reader-threw");
                throw error;
              }
              let read: typeof reader.read,
                cancel: typeof reader.cancel,
                release: typeof reader.releaseLock;
              try {
                read = reader.read;
                cancel = reader.cancel;
                release = reader.releaseLock;
              } catch {
                facts.failure = "observer-error";
                return reader;
              }
              wrap(
                reader,
                "read",
                function (this: typeof reader, ...values: unknown[]) {
                  if (retired) return Reflect.apply(read, this, values);
                  facts.read_calls++;
                  let result: ReturnType<typeof read>;
                  try {
                    result = Reflect.apply(read, this, values);
                  } catch (error) {
                    failed("read-threw");
                    mark("read_rejected_order");
                    throw error;
                  }
                  observe(
                    result,
                    (value) => {
                      facts.read_settled++;
                      if (value.done) {
                        facts.read_done = true;
                        mark("read_done_order");
                      } else facts.bytes += value.value.byteLength;
                    },
                    () => {
                      facts.read_settled++;
                      facts.read_rejected++;
                      mark("read_rejected_order");
                      failed("read-rejected");
                    },
                  );
                  return result;
                },
              );
              wrap(
                reader,
                "cancel",
                function (
                  this: typeof reader,
                  ...values: Parameters<typeof cancel>
                ) {
                  if (retired) return Reflect.apply(cancel, this, values);
                  facts.reader_cancel_calls++;
                  facts.cancel_before_eof ||= !facts.read_done;
                  mark("reader_cancel_order");
                  let result: ReturnType<typeof cancel>;
                  try {
                    result = Reflect.apply(cancel, this, values);
                  } catch (error) {
                    failed("reader-cancel-threw");
                    throw error;
                  }
                  observe(
                    result,
                    () => {
                      facts.reader_cancel_settled++;
                    },
                    () => {
                      facts.reader_cancel_settled++;
                      facts.reader_cancel_rejected++;
                      failed("reader-cancel-rejected");
                    },
                  );
                  return result;
                },
              );
              wrap(reader, "releaseLock", function (this: typeof reader) {
                if (retired) return Reflect.apply(release, this, []);
                facts.release_calls++;
                mark("release_order");
                try {
                  const result = Reflect.apply(release, this, []);
                  facts.release_successes++;
                  return result;
                } catch (error) {
                  failed("release-threw");
                  throw error;
                }
              });
              return reader;
            },
          );
        },
        () => failed("fetch-rejected"),
      );
    } catch {
      /* A diagnostic setup failure cannot change the original fetch. */
    }
    return pending;
  }
  const timer = setTimeout(retire, 45_000);
  window.fetch = fetcher;
  host.__variableNativeDiagnostic = {
    snapshot(end: boolean) {
      if (end) retire();
      return {
        document: documentID,
        retired: hooksRetired,
        records: records.map((record) => ({
          ...record,
          facts: { ...record.facts },
        })),
      };
    },
  };
}

export function nativeConsumption(page: Page) {
  type Observed = {
    epoch: number;
    method: string;
    path: string;
    query: string;
    response?: Response;
    responses: number;
  };
  const observed = new Map<Request, Observed>(),
    samples = new Map<number, any>();
  let epoch = 0,
    stopped = false,
    stopping = false,
    pending: Promise<void> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined,
    count = 0,
    settled = 0,
    failed = 0,
    joined = false;
  const navigation = (frame: unknown) => {
    if (frame === page.mainFrame()) epoch++;
  };
  page.on("framenavigated", navigation);
  function launch(end = false) {
    if (stopped || pending) return pending;
    const startedEpoch = epoch;
    count++;
    let work: Promise<any>;
    try {
      work = page.evaluate(
        (end) =>
          (window as any).__variableNativeDiagnostic?.snapshot(end) ?? null,
        end,
      );
    } catch {
      settled++;
      failed++;
      if (!stopping)
        timer = setTimeout(() => {
          timer = undefined;
          launch();
        }, 250);
      return Promise.resolve();
    }
    const branch = work
      .then(
        (value) => {
          settled++;
          if (stopped || startedEpoch !== epoch || !value) return;
          const previous = samples.get(epoch);
          if (previous && previous.document !== value.document) {
            samples.delete(epoch);
            return;
          }
          samples.set(epoch, value);
          if (samples.size > 32) samples.delete(samples.keys().next().value!);
        },
        () => {
          settled++;
          failed++;
        },
      )
      .catch(() => {
        failed++;
      });
    pending = branch;
    void branch
      .then(() => {
        if (pending === branch) pending = undefined;
        if (!stopping && !stopped)
          timer = setTimeout(() => {
            timer = undefined;
            launch();
          }, 250);
      })
      .catch(() => {});
    return branch;
  }
  launch();
  let stopPromise: Promise<void> | undefined;
  // Capture and retire this actual document before the authority test reloads.
  // Keep the Node sampler alive so the next document has its own observations.
  async function endDocument() {
    if (stopping || stopped) return false;
    if (timer) clearTimeout(timer);
    const before = epoch;
    const deadline = Date.now() + 250;
    const bound = async (work: Promise<void> | undefined) => {
      if (!work) return true;
      let timeout: ReturnType<typeof setTimeout> | undefined;
      try {
        return await Promise.race([
          work.then(() => true),
          new Promise<false>((resolve) => {
            timeout = setTimeout(
              () => resolve(false),
              Math.max(0, deadline - Date.now()),
            );
          }),
        ]);
      } finally {
        if (timeout) clearTimeout(timeout);
      }
    };
    const first = await bound(pending);
    if (
      !first ||
      Date.now() >= deadline ||
      before !== epoch ||
      stopping ||
      stopped
    )
      return false;
    // The preceding branch may have scheduled a timer; preserve single flight.
    if (timer) clearTimeout(timer);
    const ended = await bound(launch(true));
    return (
      ended &&
      Date.now() < deadline &&
      !stopping &&
      !stopped &&
      before === epoch &&
      samples.get(before)?.retired === true
    );
  }
  function stop() {
    if (stopPromise) return stopPromise;
    stopping = true;
    if (timer) clearTimeout(timer);
    stopPromise = (async () => {
      const deadline = Date.now() + 250;
      async function bounded(work: Promise<void> | undefined) {
        if (!work) return true;
        let timeout: ReturnType<typeof setTimeout> | undefined;
        try {
          return await Promise.race([
            work.then(() => true),
            new Promise<false>((resolve) => {
              timeout = setTimeout(
                () => resolve(false),
                Math.max(0, deadline - Date.now()),
              );
            }),
          ]);
        } finally {
          if (timeout) clearTimeout(timeout);
        }
      }
      const first = await bounded(pending);
      if (first && Date.now() < deadline) joined = await bounded(launch(true));
      stopped = true;
      page.off("framenavigated", navigation);
    })();
    return stopPromise;
  }
  return {
    request(request: Request) {
      if (stopped) return;
      const url = new URL(request.url());
      if (observed.size < 4096)
        observed.set(request, {
          epoch,
          method: request.method(),
          path: url.pathname,
          query: url.search,
          responses: 0,
        });
    },
    response(response: Response) {
      if (stopped) return;
      const row = observed.get(response.request());
      if (row) {
        row.response = response;
        row.responses++;
      }
    },
    stop,
    endDocument,
    snapshot(request?: Request) {
      const result = {
        ...emptyNativeDiagnostic(),
        sample_count: count,
        sample_settled: settled,
        sample_failed: failed,
        sample_joined: joined,
      };
      const original = request ? observed.get(request) : undefined;
      if (!original?.response) return result;
      const snapshot = samples.get(original.epoch);
      if (!snapshot) return { ...result, binding: "document-unavailable" };
      result.hooks_retired = snapshot.retired === true;
      const id = original.response.headers()["x-request-id"] ?? "";
      if (
        !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          id,
        )
      )
        return { ...result, binding: "missing-id" };
      const pw = [...observed.values()].filter(
        (r) =>
          r.epoch === original.epoch &&
          r.response?.headers()["x-request-id"] === id,
      );
      const native = snapshot.records.filter((r: any) => r.request_id === id);
      if (pw.length !== 1 || original.responses !== 1 || native.length !== 1)
        return { ...result, binding: "ambiguous" };
      const matched = native[0];
      if (
        matched.method !== original.method ||
        matched.path !== original.path ||
        matched.query !== original.query ||
        matched.status !== original.response.status()
      )
        return { ...result, binding: "mismatch" };
      const f = matched.facts;
      const eof =
        f.failure !== "observer-error" &&
        f.read_done &&
        !f.cancel_before_eof &&
        !f.signal_aborted_at_start &&
        f.read_done_order > 0 &&
        [
          f.abort_order,
          f.read_rejected_order,
          f.reader_cancel_order,
          f.stream_cancel_order,
        ].every((n: number) => n === 0 || f.read_done_order < n);
      const comparable =
        eof && f.content_length_valid && f.content_encoding_identity;
      return {
        ...result,
        binding: "bound",
        facts: f,
        eof_before_interruption: eof,
        length_comparable: comparable,
        length_matches: comparable && f.content_length === f.bytes,
      };
    },
  };
}
