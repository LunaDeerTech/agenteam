import {
  expect,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { createHash } from "node:crypto";
import { join, resolve } from "node:path";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";

export type ReviewMaterial = {
  mode: "review-complete" | "lost-confirmation-lookup-and-session-revocation";
  cookie: string;
  project_id: string;
  task_id: string;
  sprint_id: string;
  title: string;
  version: string;
  worker_id: string;
  reviewer_id: string;
  route: string;
};
export function material(): ReviewMaterial {
  return JSON.parse(
    readFileSync(
      join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "task-review-material.json"),
      "utf8",
    ),
  );
}

export async function taskScreenshot(page: Page, name: string) {
  const directory = process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE!;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    )
    .toBe(true);
  // Only the owned Task screen is captured. Automatic failure screenshots,
  // traces, private Session material and account/password screens remain off.
  await page.screenshot({
    path: join(directory, `task-review-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}

type Tail = "finished" | "failed" | "closed";
type Entry = {
  seq: number;
  request: Request;
  name: string;
  status?: number;
  requestID?: string;
  failure?: string;
  responseCount: number;
  finishedCount: number;
  failedCount: number;
  terminal?: Tail;
  settle: (state: Tail) => void;
  done: Promise<Tail>;
  checked: boolean;
  body?: unknown;
};

// Preserve paths and the few non-secret query facts used by these reads. Never
// persist cursor values, credentials, headers, request bodies or response data.
function diagnosticURL(value: string): string {
  const url = new URL(value);
  const query = new URLSearchParams();
  for (const [key, item] of url.searchParams) {
    const safe =
      (key === "limit" && /^\d{1,3}$/.test(item)) ||
      (key === "state" &&
        /^(backlog|todo|in_progress|blocked|in_review|done|cancelled)$/.test(
          item,
        )) ||
      (["sprint_id", "milestone_id"].includes(key) &&
        /^[0-9a-f-]{36}$/.test(item));
    query.append(key, safe ? item : "<redacted>");
  }
  return `${url.origin}${url.pathname}${query.size ? `?${query}` : ""}`;
}

type BrowserDiagnostic = {
  seq: number;
  at: number;
  document_at: number;
  kind: string;
  fetch?: number;
  signal?: number;
  signal_source?: string;
  request_id?: string;
  method?: string;
  url?: string;
  status?: number;
  aborted?: boolean;
  done?: boolean;
  bytes?: number;
  error_name?: string;
  route: string;
  visibility: string;
  logout_control: boolean;
  session_check_control: boolean;
};

async function observeBrowserIO(
  page: Page,
  accept: (event: BrowserDiagnostic) => void,
) {
  await page.exposeBinding("__taskReviewDiagnostic", (_source, event) => {
    accept(event as BrowserDiagnostic);
  });
  await page.addInitScript(() => {
    // These observers return the original fetch/read/cancel promises and the
    // original Response/reader. They do not consume, clone or replace a body.
    const send = (
      window as unknown as {
        __taskReviewDiagnostic: (event: unknown) => Promise<void>;
      }
    ).__taskReviewDiagnostic;
    let sequence = 0;
    let nextFetch = 0;
    let nextSignal = 0;
    const rows = new Map<number, any>();
    const calls: any[] = [];
    const originalCalls = new WeakMap<Promise<any>, any>();
    const identities = new WeakMap<object, string>();
    const tails = new Set<Promise<void>>();
    const restorers: (() => void)[] = [];
    let retired = false,
      failed = false,
      auth: any;
    const host = window as any;
    const canonical = (value: any): any =>
      Array.isArray(value)
        ? value.map(canonical)
        : value !== null && typeof value === "object"
          ? Object.fromEntries(
              Object.keys(value)
                .sort()
                .map((key) => [key, canonical(value[key])]),
            )
          : value;
    const digest = async (bytes: Uint8Array) =>
      [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))]
        .map((v) => v.toString(16).padStart(2, "0"))
        .join("");
    const typedDigest = (value: unknown) =>
      digest(new TextEncoder().encode(JSON.stringify(canonical(value))));
    const identity = () => {
      const value = auth?.personalContext.identity;
      return value
        ? JSON.stringify([value.userID, value.sessionID, value.epoch])
        : "";
    };
    const current = () =>
      auth?.state.phase === "authenticated" &&
      auth?.personalContext.phase === "current";
    function track(
      promise: Promise<any>,
      good: (value: any) => any,
      bad: (error: unknown) => any,
    ) {
      const tail = promise
        .then(good, bad)
        .catch(() => {
          failed = true;
        })
        .then(() => {
          tails.delete(tail);
        });
      tails.add(tail);
    }
    function consumer(
      object: any,
      method: string,
      kind: string,
      path: (args: any[]) => string,
    ) {
      const original = object[method],
        descriptor = Object.getOwnPropertyDescriptor(object, method);
      if (typeof original !== "function" || !descriptor)
        throw Error("TASK_REVIEW_PUBLIC_CONSUMER_MISSING");
      const wrapper = function (this: any, ...args: any[]) {
        if (calls.length >= 192) failed = true;
        const before = identity();
        const call: any = {
          id: calls.length + 1,
          kind,
          path: path(args),
          bound: 0,
          fulfilled: false,
          settled: false,
          rejected: false,
          current: false,
          not_busy: false,
          typed_digest: null,
          typed: null,
        };
        calls.push(call);
        let result: Promise<any>;
        try {
          result = Reflect.apply(original, this, args);
        } catch (error) {
          call.rejected = call.settled = true;
          throw error;
        }
        if (originalCalls.has(result)) {
          calls.pop();
          return result;
        }
        originalCalls.set(result, call);
        track(
          result,
          async (value) => {
            call.fulfilled = call.settled = true;
            call.current =
              current() && (kind === "restore" || before === identity());
            identities.set(call, identity());
            call.not_busy = auth.state.busy === false;
            if (!value || typeof value !== "object") return;
            if (
              kind === "restore" &&
              (Object.keys(value).sort().join() !== "session,user" ||
                value.user.id !== auth.state.user?.id ||
                value.session.id !== auth.state.session?.id)
            )
              return;
            call.typed = JSON.parse(JSON.stringify(value));
            call.typed_digest = await typedDigest(value);
          },
          () => {
            call.rejected = call.settled = true;
          },
        );
        return result;
      };
      Object.defineProperty(object, method, { ...descriptor, value: wrapper });
      restorers.push(() => {
        if (object[method] !== wrapper) failed = true;
        Object.defineProperty(object, method, descriptor);
      });
    }
    host.__taskReviewProof = {
      consumers(value: any, project: string) {
        if (auth || retired) throw Error("TASK_REVIEW_DUPLICATE_CONSUMER");
        auth = value;
        consumer(auth, "restore", "restore", () => "/api/v1/session");
        for (const method of ["get", "list", "resolve"])
          consumer(
            auth.projects,
            method,
            "project." + method,
            (a) =>
              "/api/v1/projects" +
              (method === "get"
                ? "/" + a[0]
                : method === "resolve"
                  ? "/resolve"
                  : ""),
          );
        const paths: Record<string, string> = {
          milestones: "milestones",
          milestone: "milestones",
          sprints: "sprints",
          sprint: "sprints",
          tasks: "tasks",
          task: "tasks",
          blockers: "tasks",
          agents: "agents",
          agent: "agents",
        };
        for (const method of Object.keys(paths))
          consumer(auth.workReview, method, "work." + method, (a) => {
            if (a[0] !== project) throw Error("TASK_REVIEW_CONSUMER_SCOPE");
            const single = [
              "milestone",
              "sprint",
              "task",
              "agent",
              "blockers",
            ].includes(method);
            return (
              "/api/v1/projects/" +
              project +
              "/" +
              paths[method] +
              (single ? "/" + a[1] : "") +
              (method === "blockers" ? "/blockers" : "")
            );
          });
      },
      snapshot() {
        return {
          retired,
          failed,
          pending: tails.size,
          current: current(),
          not_busy: auth?.state.busy === false,
          rows: [...rows.values()],
          calls: calls.map((c) => ({
            ...c,
            current: c.current && current() && identities.get(c) === identity(),
          })),
        };
      },
      finish() {
        if (retired) return this.snapshot();
        if (tails.size)
          throw Error("TASK_REVIEW_ORIGINAL_OBSERVATIONS_PENDING");
        retired = true;
        for (const restore of restorers.reverse()) restore();
        return this.snapshot();
      },
    };
    const signals = new WeakMap<AbortSignal, number>();
    const streams = new WeakMap<ReadableStream, number>();
    const readers = new WeakMap<ReadableStreamDefaultReader, number>();
    const safeURL = (value: string) => {
      const url = new URL(value, location.href);
      // Browser observations need only the pathname. PW records the bounded
      // safe query projection separately for each original Request.
      return `${url.origin}${url.pathname}`;
    };
    const errorName = (error: unknown) =>
      error instanceof Error &&
      ["AbortError", "TypeError", "Error", "TimeoutError"].includes(error.name)
        ? error.name
        : "other";
    const record = (kind: string, fields: Record<string, unknown> = {}) => {
      if (sequence >= 2048) return;
      const controls = [...document.querySelectorAll("button")].map(
        (button) => {
          if (button.getAttribute("aria-hidden") === "true") return undefined;
          const label = button.getAttribute("aria-label")?.trim();
          if (label) return label;
          const visible = button.cloneNode(true) as HTMLElement;
          visible
            .querySelectorAll('[aria-hidden="true"]')
            .forEach((node) => node.remove());
          return visible.textContent?.replace(/\s+/g, " ").trim();
        },
      );
      try {
        track(
          send({
            seq: ++sequence,
            at: performance.timeOrigin + performance.now(),
            document_at: performance.timeOrigin,
            kind,
            ...fields,
            route: safeURL(location.href),
            visibility: document.visibilityState,
            logout_control: controls.includes("退出登录"),
            session_check_control: controls.includes("检查当前会话"),
          }),
          () => {},
          () => {},
        );
      } catch {
        // Closing the diagnostic channel must not alter the original call.
      }
    };
    const originalFetch = window.fetch;
    window.fetch = function (input, init) {
      const result = originalFetch.call(this, input, init);
      const rawURL = input instanceof Request ? input.url : String(input);
      try {
        if (!new URL(rawURL, location.href).pathname.startsWith("/api/"))
          return result;
      } catch {
        return result;
      }
      const id = ++nextFetch;
      if (id > 192) failed = true;
      const url = new URL(rawURL, location.href);
      const candidates = calls.filter(
        (call) =>
          !call.settled && call.bound === 0 && call.path === url.pathname,
      );
      const call = candidates.length === 1 ? candidates[0] : undefined;
      if (call) call.bound++;
      const row: any = {
        id,
        call_id: call?.id ?? null,
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        path: url.pathname,
        request_id: null,
        status: null,
        length: null,
        bytes: 0,
        digest: null,
        typed_digest: null,
        fetch_returned: false,
        fetch_rejected: false,
        readers: 0,
        reads: 0,
        read_returns: 0,
        eof: false,
        closed: false,
        release: 0,
        reader_cancel: 0,
        stream_cancel: 0,
        aborted: false,
        error: false,
      };
      rows.set(id, row);
      const signal =
        init?.signal ?? (input instanceof Request ? input.signal : undefined);
      if (signal && !signals.has(signal)) signals.set(signal, ++nextSignal);
      const signalID = signal ? signals.get(signal) : undefined;
      record("fetch-call", {
        fetch: id,
        signal: signalID,
        signal_source: init?.signal
          ? "init"
          : input instanceof Request
            ? "request"
            : "none",
        url: safeURL(rawURL),
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        aborted: signal?.aborted ?? false,
      });
      const abort = () => {
        row.aborted = true;
        record("signal-abort", { fetch: id, signal: signalID });
      };
      signal?.addEventListener("abort", abort, { once: true });
      if (signal)
        restorers.push(() => signal.removeEventListener("abort", abort));
      track(
        result,
        (response) => {
          row.fetch_returned = true;
          row.status = response.status;
          row.length = response.headers.get("content-length");
          row.request_id = response.headers.get("x-request-id");
          if (response.body) streams.set(response.body, id);
          const requestID = response.headers.get("x-request-id");
          record("fetch-resolved", {
            fetch: id,
            status: response.status,
            request_id:
              requestID && /^[0-9a-f-]{36}$/.test(requestID)
                ? requestID
                : undefined,
          });
        },
        (error) => {
          row.fetch_rejected = true;
          record("fetch-rejected", { fetch: id, error_name: errorName(error) });
        },
      );
      return result;
    };
    const originalGetReader = ReadableStream.prototype.getReader;
    ReadableStream.prototype.getReader = function (...args) {
      const reader = Reflect.apply(originalGetReader, this, args);
      const id = streams.get(this);
      if (id !== undefined) {
        readers.set(reader, id);
        rows.get(id).readers++;
        record("reader-acquired", { fetch: id });
        track(
          reader.closed,
          () => {
            rows.get(id).closed = true;
            record("reader-closed", { fetch: id });
          },
          (error: unknown) => {
            rows.get(id).error = true;
            record("reader-closed-rejected", {
              fetch: id,
              error_name: errorName(error),
            });
          },
        );
      }
      return reader;
    };
    const originalRead = ReadableStreamDefaultReader.prototype.read;
    const chunks = new WeakMap<ReadableStreamDefaultReader, Uint8Array[]>();
    ReadableStreamDefaultReader.prototype.read = function (...args) {
      const result = Reflect.apply(originalRead, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        const row = rows.get(id);
        row.reads++;
        record("reader-read", { fetch: id });
        const reader = this;
        track(
          result,
          async (part: ReadableStreamReadResult<Uint8Array>) => {
            row.read_returns++;
            record("reader-read-resolved", {
              fetch: id,
              done: part.done,
              bytes: part.value?.byteLength ?? 0,
            });
            if (!part.done) {
              row.bytes += part.value.byteLength;
              if (row.bytes > 2 << 20) {
                row.error = true;
                return;
              }
              chunks.set(reader, [
                ...(chunks.get(reader) ?? []),
                part.value.slice(),
              ]);
              return;
            }
            if (row.eof) {
              row.error = true;
              return;
            }
            row.eof = true;
            const body = new Uint8Array(row.bytes);
            let at = 0;
            for (const chunk of chunks.get(reader) ?? []) {
              body.set(chunk, at);
              at += chunk.length;
              chunk.fill(0);
            }
            chunks.delete(reader);
            try {
              row.digest = await digest(body);
              let parsed = JSON.parse(
                new TextDecoder("utf-8", { fatal: true }).decode(body),
              );
              if (row.path === "/api/v1/session" && row.status === 200)
                parsed = { user: parsed.user, session: parsed.session };
              row.typed_digest = await typedDigest(parsed);
            } catch {
              row.error = true;
            } finally {
              body.fill(0);
            }
          },
          (error: unknown) => {
            row.error = true;
            record("reader-read-rejected", {
              fetch: id,
              error_name: errorName(error),
            });
          },
        );
      }
      return result;
    };
    const readerCancel = ReadableStreamDefaultReader.prototype.cancel;
    ReadableStreamDefaultReader.prototype.cancel = function (...args) {
      const result = Reflect.apply(readerCancel, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        rows.get(id).reader_cancel++;
        record("reader-cancel", { fetch: id });
        track(
          result,
          () => record("reader-cancel-resolved", { fetch: id }),
          (error: unknown) =>
            record("reader-cancel-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return result;
    };
    const streamCancel = ReadableStream.prototype.cancel;
    ReadableStream.prototype.cancel = function (...args) {
      const result = Reflect.apply(streamCancel, this, args);
      const id = streams.get(this);
      if (id !== undefined) {
        rows.get(id).stream_cancel++;
        record("stream-cancel", { fetch: id });
        track(
          result,
          () => record("stream-cancel-resolved", { fetch: id }),
          (error: unknown) =>
            record("stream-cancel-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return result;
    };
    const originalRelease = ReadableStreamDefaultReader.prototype.releaseLock;
    ReadableStreamDefaultReader.prototype.releaseLock = function (...args) {
      const result = Reflect.apply(originalRelease, this, args);
      const id = readers.get(this);
      if (id !== undefined) rows.get(id).release++;
      return result;
    };
    for (const [object, key, original] of [
      [window, "fetch", originalFetch],
      [ReadableStream.prototype, "getReader", originalGetReader],
      [ReadableStreamDefaultReader.prototype, "read", originalRead],
      [ReadableStreamDefaultReader.prototype, "cancel", readerCancel],
      [ReadableStream.prototype, "cancel", streamCancel],
      [ReadableStreamDefaultReader.prototype, "releaseLock", originalRelease],
    ] as const) {
      const installed = (object as any)[key];
      restorers.push(() => {
        if ((object as any)[key] !== installed) failed = true;
        (object as any)[key] = original;
      });
    }
    for (const kind of [
      "pageshow",
      "pagehide",
      "popstate",
      "visibilitychange",
    ]) {
      const listener = () => record(kind);
      window.addEventListener(kind, listener);
      restorers.push(() => window.removeEventListener(kind, listener));
    }
    record("observer-installed");
  });
}

// This closed endpoint set includes every real Task/Agent/list/lookup read in
// the new page. No unmatched request falls back to an unbounded finished().
function endpoint(request: Request, data: ReviewMaterial): string | undefined {
  const { pathname } = new URL(request.url());
  if (!pathname.startsWith("/api/")) return undefined;
  const method = request.method();
  const global: Record<string, string> = {
    "GET /api/v1/session": "session",
    "GET /api/v1/me": "profile",
    "GET /api/v1/me/preferences": "preferences",
    "GET /api/v1/auth/bootstrap": "bootstrap",
    "POST /api/v1/sessions/logout": "logout",
    "GET /api/v1/projects/resolve": "resolve",
    "GET /api/v1/projects": "projects",
  };
  const direct = global[`${method} ${pathname}`];
  if (direct) return direct;
  const prefix = `/api/v1/projects/${data.project_id}`;
  if (method === "GET" && pathname === prefix) return "project";
  if (!pathname.startsWith(prefix + "/"))
    throw new Error("UNEXPECTED_API_SCOPE");
  const path = pathname.slice(prefix.length);
  const uuid =
    "[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
  const reads: [RegExp, string][] = [
    [/^\/milestones$/, "milestones"],
    [new RegExp(`^/milestones/${uuid}$`), "milestone"],
    [/^\/sprints$/, "sprints"],
    [new RegExp(`^/sprints/${uuid}$`), "sprint"],
    [/^\/tasks$/, "tasks"],
    [new RegExp(`^/tasks/${data.task_id}$`), "task"],
    [new RegExp(`^/tasks/${data.task_id}/blockers$`), "blockers"],
    [/^\/agents$/, "agents"],
    [new RegExp(`^/agents/(${data.worker_id}|${data.reviewer_id})$`), "agent"],
  ];
  if (method === "GET")
    for (const [pattern, name] of reads) if (pattern.test(path)) return name;
  if (method === "POST" && path === `/tasks/${data.task_id}/transfer`)
    return "transfer";
  if (method === "POST" && path === "/task-transition-commands/lookup")
    return "lookup";
  throw new Error("UNOBSERVED_TASK_REVIEW_ENDPOINT");
}

export async function observeReview(page: Page, data: ReviewMaterial) {
  const entries: Entry[] = [];
  const events: Record<string, unknown>[] = [];
  const browserEvents: BrowserDiagnostic[] = [];
  let browserEventsTruncated = false;
  let observerError: Error | undefined;
  let dropped = 0;
  const proofs: any[] = [];
  const retirements: any[] = [];
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_TASK_REVIEW_WEB_DIST!,
  );
  if (binding.entry === binding.asset)
    throw Error("TASK_REVIEW_ENTRY_IS_SINGLETON");
  const event = (kind: string, entry: Entry) => {
    if (events.length < 1024)
      events.push({
        kind,
        at: Date.now(),
        seq: entry.seq,
        name: entry.name,
        method: entry.request.method(),
        url: diagnosticURL(entry.request.url()),
        route: diagnosticURL(page.url()),
        status: entry.status,
        request_id: entry.requestID,
        failure: entry.failure,
      });
  };
  const end = (request: Request, state: Tail) => {
    const entry = entries.find((item) => item.request === request);
    if (entry) {
      if (state === "finished") entry.finishedCount++;
      if (state === "failed") entry.failedCount++;
      if (entry.terminal)
        observerError = new Error("DUPLICATE_ORIGINAL_REQUEST_TERMINAL");
    }
    if (entry && !entry.terminal) {
      entry.terminal = state;
      entry.failure = request
        .failure()
        ?.errorText.replaceAll(data.cookie, "<redacted>")
        .slice(0, 256);
      event(state, entry);
      entry.settle(state);
    }
  };
  const onRequest = (request: Request) => {
    try {
      const name = endpoint(request, data);
      if (!name) return;
      if (entries.length >= 192)
        throw new Error("TASK_REVIEW_REQUEST_BOUND_EXCEEDED");
      let settle!: (state: Tail) => void;
      const done = new Promise<Tail>((resolve) => {
        settle = resolve;
      });
      const entry = {
        seq: entries.length + 1,
        request,
        name,
        settle,
        done,
        checked: false,
        responseCount: 0,
        finishedCount: 0,
        failedCount: 0,
      };
      entries.push(entry);
      event("request", entry);
    } catch (error) {
      observerError =
        error instanceof Error ? error : new Error("API_OBSERVER_REJECTED");
    }
  };
  const finished = (request: Request) => end(request, "finished");
  const failed = (request: Request) => end(request, "failed");
  const response = (response: Response) => {
    const entry = entries.find((item) => item.request === response.request());
    if (!entry) return;
    entry.responseCount++;
    if (entry.responseCount !== 1)
      observerError = new Error("DUPLICATE_ORIGINAL_RESPONSE");
    entry.status = response.status();
    const id = response.headers()["x-request-id"];
    entry.requestID = id && /^[0-9a-f-]{36}$/.test(id) ? id : undefined;
    event("response", entry);
  };
  const closed = () =>
    entries
      .filter((entry) => !entry.terminal)
      .forEach((entry) => end(entry.request, "closed"));
  page.on("request", onRequest);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  page.on("response", response);
  page.on("close", closed);
  await observeBrowserIO(page, (event) => {
    if (browserEvents.length < 4096) browserEvents.push(event);
    else browserEventsTruncated = true;
  });
  const entryURL = new URL(binding.entry, process.env.AGENTEAM_AUTH_WEB_ORIGIN!)
    .href;
  let entryHandlers = 0;
  const install = async (route: import("@playwright/test").Route) => {
    entryHandlers++;
    try {
      await page.evaluate(
        async ({ binding, project }) => {
          const module = await import(/* @vite-ignore */ binding.asset);
          const useSession = module[binding.exportName];
          if (typeof useSession !== "function")
            throw Error("TASK_REVIEW_SINGLETON_EXPORT");
          const auth = useSession();
          if (useSession() !== auth)
            throw Error("TASK_REVIEW_SINGLETON_IDENTITY");
          (window as any).__taskReviewProof.consumers(auth, project);
        },
        { binding, project: data.project_id },
      );
      await route.continue();
    } catch {
      observerError = new Error("TASK_REVIEW_ORIGINAL_CONSUMER_INSTALL_FAILED");
      await route.abort();
    } finally {
      entryHandlers--;
    }
  };
  await page.route(entryURL, install);

  async function checkpoint() {
    await expect
      .poll(
        async () =>
          page.evaluate(
            () => (window as any).__taskReviewProof.snapshot().pending,
          ),
        {
          timeout: 5_000,
          message:
            "original reader and public-consumer observers must actually settle",
        },
      )
      .toBe(0);
    const proof = await page.evaluate(() =>
      (window as any).__taskReviewProof.snapshot(),
    );
    expect(proof.failed).toBe(false);
    expect(proof.pending).toBe(0);
    return proof;
  }

  function joint(entry: Entry, proof: any) {
    expect(entry.request.method()).toBe("GET");
    expect(entry.status).toBe(200);
    expect(entry.responseCount).toBe(1);
    expect(entry.finishedCount).toBe(entry.terminal === "finished" ? 1 : 0);
    expect(entry.failedCount).toBe(entry.terminal === "failed" ? 1 : 0);
    expect(page.isClosed()).toBe(false);
    expect(proof.current).toBe(true);
    expect(proof.not_busy).toBe(true);
    const matched = proof.rows.filter(
      (r: any) => r.request_id === entry.requestID,
    );
    expect(matched).toHaveLength(1);
    const n = matched[0];
    const consumers = proof.calls.filter((c: any) => c.id === n.call_id);
    expect(consumers).toHaveLength(1);
    const c = consumers[0];
    const expectedURL = new URL(entry.request.url());
    const record = JSON.parse(
      readFileSync(
        join(
          process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE!,
          `task-review-${data.mode}-response-${entry.requestID}.json`,
        ),
        "utf8",
      ),
    );
    expect(record.input_hash).toBe(
      process.env.AGENTEAM_TASK_REVIEW_WEB_INPUT_HASH,
    );
    expect(record.request_id).toBe(entry.requestID);
    expect(record.method).toBe("GET");
    expect(record.status).toBe(200);
    expect(record.url_sha256).toBe(
      createHash("sha256")
        .update(expectedURL.pathname + expectedURL.search)
        .digest("hex"),
    );
    expect(
      record.original_eof && record.original_closed && record.handler_returned,
    ).toBe(true);
    expect(n.path).toBe(expectedURL.pathname);
    expect(n.method).toBe("GET");
    expect(n.status).toBe(200);
    expect(n.fetch_returned).toBe(true);
    expect(n.fetch_rejected || n.error || n.aborted).toBe(false);
    expect(n.readers).toBe(1);
    expect(n.reads).toBeGreaterThan(0);
    expect(n.read_returns).toBe(n.reads);
    expect(n.eof && n.closed).toBe(true);
    expect(n.release).toBe(1);
    expect(n.reader_cancel).toBe(0);
    expect(n.stream_cancel).toBe(0);
    expect(n.length).toMatch(/^(0|[1-9][0-9]*)$/);
    expect(Number(n.length)).toBe(n.bytes);
    expect(record.content_length).toBe(n.bytes);
    expect(n.digest).toMatch(/^[0-9a-f]{64}$/);
    expect(record.body_sha256).toBe(n.digest);
    expect(c.bound).toBe(1);
    expect(c.path).toBe(n.path);
    expect(c.fulfilled && c.settled && c.current && c.not_busy).toBe(true);
    expect(c.rejected).toBe(false);
    expect(c.typed).not.toBeNull();
    expect(c.typed_digest).toMatch(/^[0-9a-f]{64}$/);
    expect(n.typed_digest).toBe(c.typed_digest);
    proofs.push({
      seq: entry.seq,
      request_id: entry.requestID,
      native: n,
      consumer: { ...c, typed: undefined },
      go: record,
    });
    return c.typed;
  }

  async function verify() {
    if (observerError) throw observerError;
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length, {
        timeout: 5_000,
        message: "original API requests must actually finish or fail",
      })
      .toBe(0);
    if (observerError) throw observerError;
    const proof = await checkpoint();
    for (const entry of entries) {
      if (entry.checked) continue;
      const state = await entry.done;
      const response = await entry.request.response();
      if (
        state === "failed" &&
        entry.name === "transfer" &&
        data.mode !== "review-complete" &&
        dropped === 0
      ) {
        // Go already validated this exact request's committed DB receipt before
        // physically truncating its original Content-Length response.
        expect(response?.status()).toBe(200);
        expect(entry.request.failure()).not.toBeNull();
        dropped++;
        entry.checked = true;
        continue;
      }
      if (
        state === "failed" &&
        entry.request.method() === "GET" &&
        response?.status() === 200 &&
        entry.failure === "net::ERR_ABORTED"
      ) {
        // Preserve the actual failed network terminal. Only independent
        // original-byte, native-reader and original-public-call proofs can
        // establish consumption; never create Response.finished() here.
        entry.body = joint(entry, proof);
        entry.checked = true;
        continue;
      }
      expect(state, `original ${entry.name} must not be silently aborted`).toBe(
        "finished",
      );
      expect(response).not.toBeNull();
      const r = response!;
      // Only genuine requestfinished permits waiting on Response.finished.
      // Keep an independent bounded observer deadline even for this branch.
      let timer: ReturnType<typeof setTimeout> | undefined;
      try {
        const terminal = await Promise.race([
          r.finished(),
          new Promise<never>((_, reject) => {
            timer = setTimeout(
              () => reject(new Error("FINISHED_RESPONSE_DID_NOT_SETTLE")),
              2_000,
            );
          }),
        ]);
        expect(terminal).toBeNull();
      } finally {
        if (timer) clearTimeout(timer);
      }
      const raw = await r.body();
      expect(raw.length).toBeLessThanOrEqual(2 << 20);
      if (r.status() === 204 && entry.name === "logout") {
        expect(raw.length).toBe(0);
        entry.checked = true;
        continue;
      }
      expect(r.headers()["content-length"]).toMatch(/^\d+$/);
      expect(Number(r.headers()["content-length"])).toBe(raw.length);
      const revoked = entry.name === "session" && r.status() === 401;
      expect(
        r.status() === 200 || revoked,
        `unexpected ${entry.name} status`,
      ).toBe(true);
      entry.body = JSON.parse(raw.toString("utf8"));
      expect(typeof entry.body).toBe("object");
      expect(entry.body).not.toBeNull();
      entry.checked = true;
    }
  }
  function receipt(
    name: "transfer" | "lookup",
    index: number,
  ): Record<string, any> {
    const values = entries.filter(
      (entry) => entry.name === name && entry.checked && entry.body,
    );
    const body = values[index]?.body as Record<string, any> | undefined;
    expect(
      body,
      "original completed response must be observed before publication assertions",
    ).toBeTruthy();
    return body!;
  }
  function originalLookup() {
    const transfers = entries.filter((entry) => entry.name === "transfer");
    const lookups = entries.filter((entry) => entry.name === "lookup");
    expect(transfers).toHaveLength(1);
    expect(lookups).toHaveLength(1);
    const sent = transfers[0]!.request.postDataJSON();
    expect(lookups[0]!.request.postDataJSON()).toEqual({
      command: "work.task.transfer",
      target_id: data.task_id,
      expected_version: sent.expected_version,
      request: sent.request,
    });
    expect(lookups[0]!.request.headers()["idempotency-key"]).toBe(
      transfers[0]!.request.headers()["idempotency-key"],
    );
    expect(dropped).toBe(1);
  }
  function agentOption(id: string): string {
    const pages = entries.filter(
      (entry) => entry.name === "agents" && entry.checked && entry.body,
    );
    for (const page of [...pages].reverse()) {
      const items = (
        page.body as {
          items: { id: string; name: string; display_name: string | null }[];
        }
      ).items;
      const agent = items.find((item) => item.id === id);
      if (agent) {
        const label = agent.display_name ?? agent.name;
        return label === agent.name ? agent.name : `${label} (${agent.name})`;
      }
    }
    throw new Error("CURRENT_AGENT_DIRECTORY_DID_NOT_INCLUDE_REQUIRED_AGENT");
  }
  function save(testStatus?: string) {
    const directory = process.env.AGENTEAM_TASK_REVIEW_WEB_EVIDENCE!;
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    writeFileSync(
      join(directory, `task-review-${data.mode}.json`),
      JSON.stringify(
        {
          case: data.mode,
          input_hash: process.env.AGENTEAM_TASK_REVIEW_WEB_INPUT_HASH,
          original_requests: entries.length,
          finished: entries.filter((e) => e.terminal === "finished").length,
          declared_receipt_cut: dropped,
          all_observed: entries.every((e) => e.checked),
          transitions: entries.filter((e) => e.name === "transfer").length,
          lookups: entries.filter((e) => e.name === "lookup").length,
          test_status: testStatus,
          route: diagnosticURL(page.url()),
          observer_error: observerError?.message,
          requests: entries.map((entry) => ({
            seq: entry.seq,
            name: entry.name,
            method: entry.request.method(),
            url: diagnosticURL(entry.request.url()),
            status: entry.status,
            request_id: entry.requestID,
            terminal: entry.terminal,
            failure: entry.failure,
            checked: entry.checked,
          })),
          request_events: events,
          browser_events: browserEvents,
          browser_events_truncated:
            browserEventsTruncated ||
            browserEvents.some((event) => event.seq === 2048),
          joint_proofs: proofs,
          observer_retirements: retirements,
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
  async function endDocument() {
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length, {
        timeout: 5_000,
      })
      .toBe(0);
    const snapshot = await checkpoint();
    const end = await page.evaluate(() =>
      (window as any).__taskReviewProof.finish(),
    );
    expect(end.retired).toBe(true);
    expect(end.failed).toBe(false);
    expect(end.pending).toBe(0);
    retirements.push({
      retired: end.retired,
      failed: end.failed,
      pending: end.pending,
      native_rows: snapshot.rows.length,
      calls: snapshot.calls.length,
    });
  }
  async function retire() {
    try {
      await endDocument();
    } finally {
      await page.unroute(entryURL, install);
      page.off("request", onRequest);
      page.off("requestfinished", finished);
      page.off("requestfailed", failed);
      page.off("response", response);
      page.off("close", closed);
    }
    expect(entryHandlers).toBe(0);
  }
  return {
    verify,
    receipt,
    originalLookup,
    agentOption,
    save,
    endDocument,
    retire,
  };
}
