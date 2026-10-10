import type { Page, Request, Response as PWResponse } from "@playwright/test";
import {
  knowledgeSessionBinding,
  type KnowledgeBinding,
} from "./knowledge-owner-read.native";

// One production singleton, no generated export, shadow controller, second
// fetch, cloned response or material-bearing request record.
export async function installSecretOwnerObservation(config: {
  binding: KnowledgeBinding;
  project: string;
  user: string;
  forbidden: string[];
  expiresAt: number;
}) {
  const host = window as any;
  if (host.__secretOwnerObservation) throw Error("SECRET_OBSERVER_DUPLICATE");
  const module = await import(/* @vite-ignore */ config.binding.asset);
  const useSession = module[config.binding.exportName],
    auth = useSession();
  if (
    useSession() !== auth ||
    !auth.secrets ||
    auth.state.phase !== "authenticated" ||
    auth.state.user?.id !== config.user ||
    auth.state.user?.role !== "user"
  )
    throw Error("SECRET_REAL_OWNER");
  const identity = () => {
    const value = auth.personalContext.identity;
    return value
      ? JSON.stringify([value.userID, value.sessionID, value.epoch])
      : "";
  };
  const originalIdentity = identity();
  if (!originalIdentity || auth.personalContext.phase !== "current")
    throw Error("SECRET_IDENTITY");
  const current = () =>
    identity() === originalIdentity &&
    auth.personalContext.phase === "current" &&
    auth.state.phase === "authenticated";
  const prefix = `/api/v1/projects/${config.project}/secret-variables`;
  const rows: any[] = [],
    tails = new Set<Promise<void>>(),
    restore: (() => void)[] = [];
  let retired = false,
    reason = "active",
    failed = false,
    first: any = null,
    finishPromise: Promise<any> | undefined;
  let target: string | null = null,
    patchKey: string | null = null;
  const safeJSON = (value: any) => {
    const text = JSON.stringify(value);
    if (
      typeof text !== "string" ||
      config.forbidden.some((s) => text.includes(s))
    ) {
      failed = true;
      return null;
    }
    return JSON.parse(text);
  };
  const replace = (object: any, key: string, value: any) => {
    const descriptor = Object.getOwnPropertyDescriptor(object, key);
    Object.defineProperty(object, key, {
      configurable: true,
      writable: true,
      value,
    });
    restore.push(() => {
      if (object[key] !== value) {
        failed = true;
        return;
      }
      if (descriptor) Object.defineProperty(object, key, descriptor);
      else delete object[key];
    });
  };
  const observe = (
    promise: Promise<any>,
    good: (v: any) => void,
    bad: () => void,
  ) => {
    if (!promise || typeof promise.then !== "function") {
      failed = true;
      return;
    }
    const tail = promise
      .then(
        (value) => {
          try {
            good(value);
          } catch {
            failed = true;
          }
        },
        () => {
          try {
            bad();
          } catch {
            failed = true;
          }
        },
      )
      .catch(() => {
        failed = true;
      })
      .then(() => {
        tails.delete(tail);
      });
    tails.add(tail);
  };
  const newRow = (method: string, args: any[]) => {
    let operation: string, path: string, verb: string;
    if (method === "execute") {
      const command = args[0];
      if (
        command?.projectID !== config.project ||
        !["create", "update", "delete"].includes(command.kind)
      )
        throw Error("SECRET_TARGET");
      operation = command.kind;
      verb =
        operation === "create"
          ? "POST"
          : operation === "update"
            ? "PATCH"
            : "DELETE";
      if (operation === "create") {
        if (target !== null) throw Error("SECRET_DUPLICATE_CREATE");
        target = command.request?.variable_id;
      }
      if (
        (operation !== "create" && command.targetID !== target) ||
        (operation !== "create" &&
          command.expectedVersion !== (operation === "update" ? "1" : "2"))
      )
        throw Error("SECRET_VERSION");
      path = operation === "create" ? prefix : prefix + "/" + target;
    } else if (method === "lookup") {
      operation = "lookup";
      verb = "POST";
      path = prefix + "/commands/lookup";
    } else {
      if (args[0] !== config.project) throw Error("SECRET_PROJECT");
      operation = method;
      verb = "GET";
      if (method === "get") {
        if (!target || args[1] !== target) throw Error("SECRET_TARGET");
        path = prefix + "/" + target;
      } else {
        if (
          method !== "list" ||
          args[1]?.limit !== 50 ||
          args[1]?.cursor !== undefined
        )
          throw Error("SECRET_LIST");
        path = prefix + "?limit=50";
      }
    }
    return {
      id: rows.length + 1,
      operation,
      method: verb,
      url: path,
      bound: 0,
      request_valid: false,
      settled: false,
      fulfilled: false,
      rejected: false,
      current: false,
      not_busy: false,
      typed: null,
      progress: null,
      xid: null,
      status: null,
      cl: null,
      fetch_returned: false,
      fetch_rejected: false,
      readers: 0,
      reads: 0,
      read_returns: 0,
      bytes: 0,
      eof: false,
      reader_cancel: 0,
      reader_cancel_joined: 0,
      release: 0,
      outer_cancel: 0,
      outer_cancel_joined: 0,
      io_error: false,
      body: null,
      digest: null,
    };
  };
  for (const method of ["list", "get", "execute", "lookup"]) {
    const original = auth.secrets[method];
    if (typeof original !== "function") throw Error("SECRET_PUBLIC_METHOD");
    replace(auth.secrets, method, function (this: any, ...args: any[]) {
      if (retired) {
        failed = true;
        return Reflect.apply(original, this, args);
      }
      let row: any;
      try {
        if (rows.length >= 64) throw Error("SECRET_BOUND");
        row = newRow(method, args);
        rows.push(row);
      } catch {
        failed = true;
        return Reflect.apply(original, this, args);
      }
      const promise = Reflect.apply(original, this, args) as Promise<any>;
      observe(
        promise,
        (value) => {
          row.settled = true;
          row.fulfilled = true;
          row.current = current();
          row.not_busy = auth.state.busy === false;
          row.typed = safeJSON(value);
          if (method === "execute" || method === "lookup") {
            const progress = auth.secrets.progress;
            row.progress = {
              phase: progress?.phase,
              observation: progress?.observation,
              receipt_same:
                progress?.receipt ===
                (method === "lookup" ? value?.receipt : value),
            };
          }
        },
        () => {
          row.settled = true;
          row.rejected = true;
          row.current = current();
          row.not_busy = auth.state.busy === false;
          row.progress = {
            phase: auth.secrets.progress?.phase,
            receipt_null: auth.secrets.progress?.receipt === null,
          };
        },
      );
      return promise; // Exactly the public method's original Promise.
    });
  }
  const originalFetch = window.fetch;
  replace(
    window,
    "fetch",
    function (this: any, input: RequestInfo | URL, init?: RequestInit) {
      const url = new URL(
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input.url,
        location.origin,
      );
      if (
        url.origin !== location.origin ||
        !(url.pathname === prefix || url.pathname.startsWith(prefix + "/"))
      )
        return Reflect.apply(originalFetch, this, [input, init]);
      const method =
        init?.method ?? (input instanceof Request ? input.method : "GET");
      const candidates = rows.filter(
        (r) =>
          !r.settled &&
          r.bound === 0 &&
          r.method === method &&
          r.url === url.pathname + url.search,
      );
      if (retired || candidates.length !== 1) {
        failed = true;
        return Reflect.apply(originalFetch, this, [input, init]);
      }
      const row = candidates[0];
      row.bound++;
      const headers = new Headers(
        init?.headers ?? (input instanceof Request ? input.headers : undefined),
      );
      const csrf = headers.get("x-csrf-token"),
        key = headers.get("idempotency-key");
      row.request_valid =
        init?.credentials === "same-origin" &&
        (method === "GET"
          ? csrf === null && key === null
          : !!csrf &&
            !!key &&
            headers.get("content-type") === "application/json");
      if (row.operation === "update") patchKey = key;
      if (row.operation === "lookup") {
        try {
          const body = JSON.parse(String(init?.body));
          row.request_valid &&=
            !!patchKey &&
            key === patchKey &&
            Object.keys(body).sort().join(",") ===
              "command,expected_version,target_id" &&
            body.command === "project.secret_variable.update" &&
            body.expected_version === "1" &&
            body.target_id === target;
        } catch {
          row.request_valid = false;
        }
      }
      const promise = Reflect.apply(originalFetch, this, [input, init]);
      observe(
        promise,
        (response: Response) => {
          row.fetch_returned = true;
          row.xid = response.headers.get("x-request-id");
          row.status = response.status;
          row.cl = response.headers.get("content-length");
          for (const [name, value] of response.headers)
            if (
              config.forbidden.some(
                (s) => name.includes(s) || value.includes(s),
              )
            ) {
              failed = true;
              row.io_error = true;
            }
          const stream = response.body;
          if (!stream) {
            row.io_error = true;
            return;
          }
          const getReader = stream.getReader,
            outerCancel = stream.cancel;
          replace(stream, "cancel", function (this: any, ...args: any[]) {
            row.outer_cancel++;
            const result = Reflect.apply(outerCancel, this, args);
            observe(
              result,
              () => {
                row.outer_cancel_joined++;
              },
              () => {
                row.io_error = true;
              },
            );
            return result;
          });
          replace(stream, "getReader", function (this: any, ...args: any[]) {
            const reader = Reflect.apply(
              getReader,
              this,
              args,
            ) as ReadableStreamDefaultReader<Uint8Array>;
            row.readers++;
            const read = reader.read,
              cancel = reader.cancel,
              release = reader.releaseLock;
            let chunks: Uint8Array[] = [];
            replace(reader, "read", function (this: any, ...args: any[]) {
              row.reads++;
              const result = Reflect.apply(read, this, args);
              observe(
                result,
                (value: ReadableStreamReadResult<Uint8Array>) => {
                  row.read_returns++;
                  if (value.done) {
                    if (row.eof) {
                      row.io_error = true;
                      return;
                    }
                    row.eof = true;
                    const data = new Uint8Array(row.bytes);
                    let offset = 0;
                    for (const chunk of chunks) {
                      data.set(chunk, offset);
                      offset += chunk.length;
                    }
                    chunks = [];
                    const text = new TextDecoder("utf-8", {
                      fatal: true,
                    }).decode(data);
                    if (config.forbidden.some((s) => text.includes(s))) {
                      row.io_error = true;
                      failed = true;
                      data.fill(0);
                      return;
                    }
                    row.body = JSON.parse(text);
                    observe(
                      crypto.subtle.digest("SHA-256", data),
                      (digest: ArrayBuffer) => {
                        row.digest = Array.from(new Uint8Array(digest), (v) =>
                          v.toString(16).padStart(2, "0"),
                        ).join("");
                      },
                      () => {
                        row.io_error = true;
                      },
                    );
                  } else if (value.value instanceof Uint8Array) {
                    row.bytes += value.value.byteLength;
                    if (
                      row.bytes >
                      (row.operation === "list" ? 5 : 1) * 1024 * 1024
                    ) {
                      row.io_error = true;
                      return;
                    }
                    chunks.push(value.value.slice());
                  } else row.io_error = true;
                },
                () => {
                  row.io_error = true;
                },
              );
              return result;
            });
            replace(reader, "cancel", function (this: any, ...args: any[]) {
              row.reader_cancel++;
              const result = Reflect.apply(cancel, this, args);
              observe(
                result,
                () => {
                  row.reader_cancel_joined++;
                },
                () => {
                  row.io_error = true;
                },
              );
              return result;
            });
            replace(
              reader,
              "releaseLock",
              function (this: any, ...args: any[]) {
                const result = Reflect.apply(release, this, args);
                row.release++;
                return result;
              },
            );
            return reader;
          });
        },
        () => {
          row.fetch_rejected = true;
        },
      );
      return promise;
    },
  );
  const snapshot = () => ({
    retired,
    reason,
    first,
    pending: tails.size,
    failed,
    current: current(),
    not_busy: auth.state.busy === false,
    rows: rows.map((r) => ({ ...r })),
  });
  const seal = (why: string) => {
    if (retired) return;
    first = Object.freeze({
      pending: tails.size,
      rows: rows.length,
      current: current(),
      not_busy: auth.state.busy === false,
    });
    retired = true;
    reason = why;
    clearTimeout(timer);
    for (const detach of restore.reverse()) {
      try {
        detach();
      } catch {
        failed = true;
      }
    }
    patchKey = null;
  };
  const timer = setTimeout(
    () => seal("expired"),
    Math.max(0, config.expiresAt - Date.now()),
  );
  host.__secretOwnerObservation = {
    snapshot,
    finish() {
      if (finishPromise) return finishPromise;
      seal("explicit");
      finishPromise = Promise.allSettled([...tails]).then(snapshot);
      return finishPromise;
    },
  };
  return true;
}

export function secretOriginalCompleted(report: any): boolean {
  const canonical = (value: any): string =>
    JSON.stringify(value, (_key, v) =>
      v && typeof v === "object" && !Array.isArray(v)
        ? Object.fromEntries(
            Object.keys(v)
              .sort()
              .map((k) => [k, v[k]]),
          )
        : v,
    );
  const b = report?.browser;
  if (
    !b ||
    report.pw_failed ||
    !report.first?.ready ||
    report.pw_pending !== 0 ||
    b.failed ||
    !b.retired ||
    b.reason !== "explicit" ||
    b.first?.pending !== 0 ||
    b.pending !== 0 ||
    !b.first.current ||
    !b.first.not_busy ||
    !b.current ||
    !b.not_busy ||
    b.rows.length < 8 ||
    b.rows.length !== report.rows?.length
  )
    return false;
  const seen = new Set<string>();
  for (const row of b.rows) {
    const match = report.rows.filter((r: any) => r.xid === row.xid);
    if (!row.xid || seen.has(row.xid) || match.length !== 1) return false;
    seen.add(row.xid);
    const p = match[0];
    if (
      p.method !== row.method ||
      p.url !== row.url ||
      p.status !== row.status ||
      p.finished !== 1 ||
      p.failed !== 0 ||
      p.responses !== 1 ||
      p.finished_calls !== 1 ||
      !p.finished_null ||
      !p.joined ||
      !p.first_ready ||
      row.bound !== 1 ||
      !row.request_valid ||
      !row.fetch_returned ||
      row.fetch_rejected ||
      row.io_error ||
      !row.settled ||
      !row.current ||
      !row.not_busy ||
      row.outer_cancel !== 1 ||
      row.outer_cancel_joined !== 1
    )
      return false;
    if (row.operation === "update") {
      // A fixed 502 is a failed execution response, never completed metadata.
      if (
        row.status !== 502 ||
        !row.rejected ||
        row.fulfilled ||
        row.typed !== null ||
        row.readers !== 0 ||
        row.progress?.phase !== "uncertain" ||
        !row.progress.receipt_null
      )
        return false;
    } else {
      if (
        row.status !== 200 ||
        !row.fulfilled ||
        row.rejected ||
        !row.eof ||
        row.readers !== 1 ||
        row.reads < 1 ||
        row.reads !== row.read_returns ||
        row.reader_cancel !== 1 ||
        row.reader_cancel_joined !== 1 ||
        row.release !== 1 ||
        !/^[0-9]+$/.test(row.cl ?? "") ||
        row.bytes !== Number(row.cl) ||
        !/^[0-9a-f]{64}$/.test(row.digest ?? "") ||
        canonical(row.body) !== canonical(row.typed)
      )
        return false;
      if (
        ["create", "delete", "lookup"].includes(row.operation) &&
        (row.progress?.phase !== "confirmed" || !row.progress.receipt_same)
      )
        return false;
    }
  }
  for (const operation of ["create", "update", "delete", "lookup"])
    if (b.rows.filter((r: any) => r.operation === operation).length !== 1)
      return false;
  return true;
}

// Closed diagnostic projection only. It never participates in acceptance and
// never returns request/response values, headers, identities or material.
export function secretOwnerDiagnostic(report: any) {
  const b = report?.browser,
    node = Array.isArray(report?.rows) ? report.rows.slice(0, 64) : [],
    browser = Array.isArray(b?.rows) ? b.rows.slice(0, 64) : [];
  const canonical = (value: any): string =>
    JSON.stringify(value, (_key, v) =>
      v && typeof v === "object" && !Array.isArray(v)
        ? Object.fromEntries(
            Object.keys(v)
              .sort()
              .map((k) => [k, v[k]]),
          )
        : v,
    );
  const known = ["list", "get", "create", "update", "lookup", "delete"];
  return Object.freeze({
    node_ready: report?.node_ready === true,
    node_failed: report?.pw_failed === true,
    node_pending: Number.isSafeInteger(report?.pw_pending)
      ? report.pw_pending
      : -1,
    node_rows: node.length,
    first_present: !!report?.first,
    first_ready: report?.first?.ready === true,
    browser_present: !!b,
    browser_failed: b?.failed === true,
    browser_pending: Number.isSafeInteger(b?.pending) ? b.pending : -1,
    browser_rows: browser.length,
    current: b?.current === true,
    not_busy: b?.not_busy === true,
    retired: b?.retired === true,
    reason: ["active", "explicit", "expired"].includes(b?.reason)
      ? b.reason
      : "unavailable",
    first_pending: Number.isSafeInteger(b?.first?.pending)
      ? b.first.pending
      : -1,
    first_current: b?.first?.current === true,
    first_not_busy: b?.first?.not_busy === true,
    pw: Object.freeze(
      node.map((p: any, index: number) =>
        Object.freeze({
          index,
          finished_one: p.finished === 1,
          failed_zero: p.failed === 0,
          responses_one: p.responses === 1,
          finished_calls_one: p.finished_calls === 1,
          finished_null: p.finished_null === true,
          joined: p.joined === true,
          xid_present: typeof p.xid === "string" && p.xid.length > 0,
          xid_unique:
            !!p.xid && node.filter((r: any) => r.xid === p.xid).length === 1,
          first_ready: p.first_ready === true,
        }),
      ),
    ),
    consumer: Object.freeze(
      browser.map((row: any, index: number) => {
        const matched = node.filter((p: any) => p.xid === row.xid),
          p = matched[0];
        return Object.freeze({
          index,
          operation: known.includes(row.operation) ? row.operation : "unknown",
          binding:
            !!row.xid &&
            matched.length === 1 &&
            p.method === row.method &&
            p.url === row.url &&
            p.status === row.status &&
            row.bound === 1 &&
            row.request_valid === true,
          fetch:
            row.fetch_returned === true && !row.fetch_rejected && !row.io_error,
          publication:
            row.settled === true &&
            row.current === true &&
            row.not_busy === true,
          outer: row.outer_cancel === 1 && row.outer_cancel_joined === 1,
          reader:
            row.operation === "update"
              ? row.readers === 0
              : row.eof === true &&
                row.readers === 1 &&
                row.reads >= 1 &&
                row.reads === row.read_returns &&
                row.reader_cancel === 1 &&
                row.reader_cancel_joined === 1 &&
                row.release === 1,
          representation:
            row.operation === "update"
              ? row.status === 502 &&
                row.rejected === true &&
                !row.fulfilled &&
                row.typed === null
              : row.status === 200 &&
                row.fulfilled === true &&
                !row.rejected &&
                /^[0-9]+$/.test(row.cl ?? "") &&
                row.bytes === Number(row.cl) &&
                /^[0-9a-f]{64}$/.test(row.digest ?? "") &&
                canonical(row.body) === canonical(row.typed),
          progress:
            row.operation === "update"
              ? row.progress?.phase === "uncertain" &&
                row.progress?.receipt_null === true
              : !["create", "delete", "lookup"].includes(row.operation) ||
                (row.progress?.phase === "confirmed" &&
                  row.progress?.receipt_same === true),
        });
      }),
    ),
  });
}

export async function observeSecretOwner(
  page: Page,
  config: {
    root: string;
    dist: string;
    project: string;
    user: string;
    forbidden: string[];
    expiresAt: number;
  },
) {
  const binding = await knowledgeSessionBinding(config.root, config.dist);
  await page.evaluate(installSecretOwnerObservation, {
    binding,
    project: config.project,
    user: config.user,
    forbidden: config.forbidden,
    expiresAt: config.expiresAt,
  });
  const prefix = `/api/v1/projects/${config.project}/secret-variables`;
  const rows: any[] = [],
    slots = new Map<Request, any>(),
    tails = new Set<Promise<void>>();
  let sealed = false,
    closed = false,
    failed = false,
    first: any = null,
    lastReady: ReturnType<typeof secretOwnerDiagnostic> | null = null,
    finishPromise: Promise<any> | undefined,
    pageFinish: Promise<any> | undefined;
  const selected = (request: Request) => {
    const u = new URL(request.url());
    return (
      u.origin === new URL(page.url()).origin &&
      (u.pathname === prefix || u.pathname.startsWith(prefix + "/"))
    );
  };
  const requested = (request: Request) => {
    if (!selected(request)) return;
    if (
      sealed ||
      closed ||
      Date.now() >= config.expiresAt ||
      rows.length >= 64
    ) {
      failed = true;
      return;
    }
    const u = new URL(request.url()),
      row = {
        method: request.method(),
        url: u.pathname + u.search,
        xid: null as string | null,
        status: 0,
        finished: 0,
        failed: 0,
        responses: 0,
        finished_calls: 0,
        finished_null: false,
        joined: false,
        first_ready: false,
      };
    rows.push(row);
    slots.set(request, row);
  };
  const finished = (request: Request) => {
    const row = slots.get(request);
    if (row) {
      row.finished++;
      if (sealed || closed || row.finished !== 1 || row.failed !== 0)
        failed = true;
    } else if (selected(request)) failed = true;
  };
  const rejected = (request: Request) => {
    const row = slots.get(request);
    if (row) row.failed++;
    if (row || selected(request)) failed = true;
  };
  const response = (value: PWResponse) => {
    const row = slots.get(value.request());
    if (!row) {
      if (selected(value.request())) failed = true;
      return;
    }
    row.responses++;
    if (
      sealed ||
      closed ||
      row.responses !== 1 ||
      Date.now() >= config.expiresAt
    ) {
      failed = true;
      return;
    }
    const tail = (async () => {
      row.status = value.status();
      row.xid = await value.headerValue("x-request-id");
      row.finished_calls++;
      const result = await value.finished();
      row.finished_null = result === null;
      if (
        !row.finished_null ||
        !row.xid ||
        closed ||
        Date.now() >= config.expiresAt
      )
        failed = true;
    })()
      .catch(() => {
        failed = true;
      })
      .then(() => {
        row.joined = true;
        tails.delete(tail);
      });
    tails.add(tail);
  };
  const close = () => {
    closed = true;
    failed = true;
  };
  page.on("request", requested);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", rejected);
  page.on("close", close);
  page.context().on("close", close);
  const detach = () => {
    page.off("request", requested);
    page.off("response", response);
    page.off("requestfinished", finished);
    page.off("requestfailed", rejected);
    page.off("close", close);
    page.context().off("close", close);
  };
  const ready = () =>
    !closed &&
    !failed &&
    tails.size === 0 &&
    rows.length > 0 &&
    rows.every(
      (r) =>
        r.joined &&
        r.finished === 1 &&
        r.failed === 0 &&
        r.responses === 1 &&
        r.finished_null,
    );
  return {
    async snapshot() {
      return page.evaluate(() =>
        (window as any).__secretOwnerObservation.snapshot(),
      );
    },
    async ready() {
      const node = ready();
      const nodeSample = {
        node_ready: node,
        pw_failed: failed,
        pw_pending: tails.size,
        first,
        rows: rows.map((r) => ({ ...r })),
      };
      const b = await page.evaluate(() =>
        (window as any).__secretOwnerObservation.snapshot(),
      );
      lastReady = secretOwnerDiagnostic({ ...nodeSample, browser: b });
      return (
        node &&
        !b.failed &&
        b.pending === 0 &&
        b.not_busy &&
        b.current &&
        b.rows.length === rows.length
      );
    },
    diagnostics(terminal?: any) {
      return terminal ? secretOwnerDiagnostic(terminal) : lastReady;
    },
    finish() {
      if (finishPromise) return finishPromise;
      const eligible = ready() && Date.now() < config.expiresAt;
      first = Object.freeze({
        ready: eligible,
        pending: tails.size,
        rows: rows.length,
      });
      sealed = true;
      for (const row of rows)
        row.first_ready =
          eligible && row.joined && row.finished === 1 && row.failed === 0;
      // Dispatch the browser's synchronous seal immediately, before awaiting
      // any header/finished tail. Late tails cannot change this first record.
      pageFinish = page.evaluate(() =>
        (window as any).__secretOwnerObservation.finish(),
      );
      finishPromise = (async () => {
        const settled = await Promise.allSettled([...tails, pageFinish!]);
        const last = settled[settled.length - 1];
        if (!last || last.status !== "fulfilled") failed = true;
        detach();
        return {
          first,
          pw_failed: failed || !eligible,
          pw_pending: tails.size,
          rows: rows.map((r) => ({ ...r })),
          browser: last?.status === "fulfilled" ? last.value : null,
        };
      })();
      return finishPromise;
    },
    async abort() {
      try {
        await this.finish();
      } catch {
        failed = true;
      } finally {
        detach();
      }
    },
  };
}
