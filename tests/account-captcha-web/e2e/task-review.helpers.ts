import {
  expect,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

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
      const controls = [...document.querySelectorAll("button")].map((button) =>
        button.textContent?.trim(),
      );
      try {
        void send({
          seq: ++sequence,
          at: performance.timeOrigin + performance.now(),
          document_at: performance.timeOrigin,
          kind,
          ...fields,
          route: safeURL(location.href),
          visibility: document.visibilityState,
          logout_control: controls.includes("退出登录"),
          session_check_control: controls.includes("检查当前会话"),
        }).catch(() => {});
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
      signal?.addEventListener(
        "abort",
        () => record("signal-abort", { fetch: id, signal: signalID }),
        { once: true },
      );
      void result.then(
        (response) => {
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
        (error) =>
          record("fetch-rejected", { fetch: id, error_name: errorName(error) }),
      );
      return result;
    };
    const originalGetReader = ReadableStream.prototype.getReader;
    ReadableStream.prototype.getReader = function (...args) {
      const reader = Reflect.apply(originalGetReader, this, args);
      const id = streams.get(this);
      if (id !== undefined) {
        readers.set(reader, id);
        record("reader-acquired", { fetch: id });
        void reader.closed.then(
          () => record("reader-closed", { fetch: id }),
          (error: unknown) =>
            record("reader-closed-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return reader;
    };
    const originalRead = ReadableStreamDefaultReader.prototype.read;
    ReadableStreamDefaultReader.prototype.read = function (...args) {
      const result = Reflect.apply(originalRead, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        record("reader-read", { fetch: id });
        void result.then(
          (part: ReadableStreamReadResult<Uint8Array>) =>
            record("reader-read-resolved", {
              fetch: id,
              done: part.done,
              bytes: part.value?.byteLength ?? 0,
            }),
          (error: unknown) =>
            record("reader-read-rejected", {
              fetch: id,
              error_name: errorName(error),
            }),
        );
      }
      return result;
    };
    const readerCancel = ReadableStreamDefaultReader.prototype.cancel;
    ReadableStreamDefaultReader.prototype.cancel = function (...args) {
      const result = Reflect.apply(readerCancel, this, args);
      const id = readers.get(this);
      if (id !== undefined) {
        record("reader-cancel", { fetch: id });
        void result.then(
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
        record("stream-cancel", { fetch: id });
        void result.then(
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
    for (const kind of ["pageshow", "pagehide", "popstate", "visibilitychange"])
      window.addEventListener(kind, () => record(kind));
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
    entry.status = response.status();
    const id = response.headers()["x-request-id"];
    entry.requestID = id && /^[0-9a-f-]{36}$/.test(id) ? id : undefined;
    event("response", entry);
  };
  const closed = () => entries.forEach((entry) => end(entry.request, "closed"));
  page.on("request", onRequest);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  page.on("response", response);
  page.on("close", closed);
  await observeBrowserIO(page, (event) => {
    if (browserEvents.length < 4096) browserEvents.push(event);
    else browserEventsTruncated = true;
  });

  async function verify() {
    if (observerError) throw observerError;
    await expect
      .poll(() => entries.filter((entry) => !entry.terminal).length, {
        timeout: 5_000,
        message: "original API requests must actually finish or fail",
      })
      .toBe(0);
    if (observerError) throw observerError;
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
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
  }
  return { verify, receipt, originalLookup, agentOption, save };
}
