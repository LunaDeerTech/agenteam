import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { observeUsageWire, type UsageRow } from "./project-usage.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";

const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const instant = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/;
const hash = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");
export const summaryFields = [
  "input",
  "output",
  "total",
  "cached_input",
  "cache_write",
  "reasoning",
] as const;
export const summaryCounts = [
  "confirmed_invocations",
  "dispatch_unknown",
  "succeeded",
  "failed",
  "cancelled",
  "unknown",
] as const;
export type ExecutionUsageMaterial = {
  mode: "execution-filter-current-read";
  origin: string;
  cookie: string;
  input_hash: string;
  project_id: string;
  execution_id: string;
  other_execution_id: string;
  other_project_id: string;
  foreign: UsageRow;
  items: UsageRow[];
  target_items: UsageRow[];
  target_summary: Record<string, any>[];
  all_summary: Record<string, any>[];
  route: string;
};
export function executionUsageMaterial(): ExecutionUsageMaterial {
  const data: ExecutionUsageMaterial = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "usage-execution-filter-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("execution-filter-current-read");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_INPUT_HASH,
  );
  expect(data.input_hash).toMatch(/^[0-9a-f]{64}$/);
  for (const id of [
    data.project_id,
    data.execution_id,
    data.other_execution_id,
    data.other_project_id,
  ])
    expect(id).toMatch(uuid);
  expect(data.execution_id).not.toBe(data.other_execution_id);
  expect(data.project_id).not.toBe(data.other_project_id);
  expect(data.items).toHaveLength(53);
  expect(data.target_items).toHaveLength(51);
  expect(new Set(data.items.map((row) => row.id)).size).toBe(53);
  expect(
    data.items.filter((row) => row.execution_id === data.execution_id),
  ).toEqual(data.target_items);
  expect(
    data.items.filter((row) => row.execution_id === data.other_execution_id),
  ).toHaveLength(1);
  expect(data.items.filter((row) => row.execution_id === null)).toHaveLength(1);
  expect(data.foreign.project_id).toBe(data.other_project_id);
  for (const row of data.items) {
    expect(row.project_id).toBe(data.project_id);
    expect(row.id).toMatch(uuid);
    expect(row.started_at).toMatch(instant);
    expect(row.id).not.toBe(data.foreign.id);
  }
  expect(
    data.target_summary.map((row) => row.summary.confirmed_invocations),
  ).toEqual(["25", "26"]);
  expect(
    data.all_summary.map((row) => row.summary.confirmed_invocations),
  ).toEqual(["27", "26"]);
  expect(data.target_summary.map((row) => row.summary.total.sum)).toEqual([
    "250",
    "260",
  ]);
  expect(data.all_summary.map((row) => row.summary.total.sum)).toEqual([
    "350",
    "260",
  ]);
  return data;
}

// A dedicated closed, read-only observer. Shared Usage profiles keep rejecting
// execution_id. Every wrapper returns the identical original Promise/value;
// only the application's original reader is observed, with no clone/tee/fetch.
function installExecutionNative(config: {
  origin: string;
  project: string;
  execution: string;
}) {
  const host = window as any,
    calls: any[] = [],
    rows: any[] = [],
    tails = new Set<Promise<void>>(),
    restore: (() => void)[] = [];
  const streams = new WeakMap<ReadableStream, any>(),
    readers = new WeakMap<ReadableStreamDefaultReader, any>(),
    promises = new WeakMap<Promise<any>, any>();
  let auth: any,
    failed = false,
    retired = false;
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
      .map((n) => n.toString(16).padStart(2, "0"))
      .join("");
  const typedDigest = (value: any) =>
    digest(new TextEncoder().encode(JSON.stringify(canonical(value))));
  const identity = () => {
    const i = auth?.personalContext.identity;
    return i ? JSON.stringify([i.userID, i.sessionID, i.epoch]) : "";
  };
  const current = () =>
    auth?.state.phase === "authenticated" &&
    auth?.personalContext.phase === "current";
  function track(
    promise: Promise<any>,
    good: (value: any) => any,
    bad = () => {
      failed = true;
    },
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
  function replace(object: any, key: string, wrapper: any) {
    const descriptor = Object.getOwnPropertyDescriptor(object, key);
    if (!descriptor || typeof descriptor.value !== "function")
      throw Error("EXECUTION_NATIVE_DESCRIPTOR");
    Object.defineProperty(object, key, { ...descriptor, value: wrapper });
    restore.push(() => {
      if (object[key] !== wrapper) failed = true;
      Object.defineProperty(object, key, descriptor);
    });
  }
  function consumer(
    object: any,
    method: string,
    kind: string,
    path: (args: any[]) => string,
  ) {
    const original = object[method];
    replace(object, method, function (this: any, ...args: any[]) {
      if (retired || calls.length >= 128)
        throw Error("EXECUTION_NATIVE_CALL_BOUND");
      const before = identity(),
        call: any = {
          id: calls.length + 1,
          kind,
          path: path(args),
          bound: 0,
          fulfilled: false,
          rejected: false,
          settled: false,
          typed: null,
        };
      if (kind.startsWith("usage.")) {
        const q = args[1],
          fields =
            kind === "usage.list" ? ["from", "to", "limit"] : ["from", "to"];
        if (q && Object.hasOwn(q, "cursor")) fields.push("cursor");
        if (q && Object.hasOwn(q, "execution_id")) fields.push("execution_id");
        if (
          args.length !== 2 ||
          args[0] !== config.project ||
          !q ||
          Object.keys(q).sort().join() !== fields.sort().join() ||
          (kind === "usage.list" && q.limit !== 50) ||
          (kind === "usage.daily" && Object.hasOwn(q, "cursor")) ||
          typeof q.from !== "string" ||
          typeof q.to !== "string" ||
          !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(q.from) ||
          !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(q.to) ||
          q.from >= q.to ||
          (Object.hasOwn(q, "execution_id") &&
            q.execution_id !== config.execution) ||
          (Object.hasOwn(q, "cursor") &&
            (typeof q.cursor !== "string" || !q.cursor.length))
        )
          throw Error("EXECUTION_NATIVE_FACADE_QUERY");
        call.query = JSON.parse(JSON.stringify(q));
      }
      calls.push(call);
      const result = Reflect.apply(original, this, args);
      if (promises.has(result)) {
        calls.pop();
        return result;
      }
      promises.set(result, call);
      track(
        result,
        async (value) => {
          call.fulfilled = call.settled = true;
          call.current =
            current() && (kind === "restore" || before === identity());
          call.identity = identity();
          call.not_busy = auth.state.busy === false;
          if (
            kind === "restore" &&
            (!value ||
              Object.keys(value).sort().join() !== "session,user" ||
              value.user?.id !== auth.state.user?.id ||
              value.session?.id !== auth.state.session?.id)
          )
            throw Error("EXECUTION_NATIVE_RESTORE_IDENTITY");
          call.typed = JSON.parse(JSON.stringify(value));
          call.typed_digest = await typedDigest(value);
        },
        () => {
          call.rejected = call.settled = true;
        },
      );
      return result;
    });
  }
  host.__executionUsageNative = {
    bind(value: any) {
      if (auth || retired) throw Error("EXECUTION_NATIVE_DUPLICATE_BIND");
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
      for (const method of ["list", "daily"])
        consumer(
          auth.projectUsage,
          method,
          "usage." + method,
          () =>
            "/api/v1/projects/" +
            config.project +
            "/model-usage" +
            (method === "daily" ? "/summary" : ""),
        );
    },
    snapshot() {
      return {
        retired,
        failed,
        pending: tails.size,
        current: current(),
        not_busy: auth?.state.busy === false,
        rows: rows.map(({ chunks, ...row }) => row),
        calls: calls.map(({ identity: captured, ...call }) => ({
          ...call,
          current: call.current && current() && captured === identity(),
        })),
      };
    },
    finish() {
      if (!retired) {
        if (tails.size) throw Error("EXECUTION_NATIVE_ORIGINAL_TAIL_PENDING");
        for (const fn of restore.reverse()) fn();
        retired = true;
      }
      return this.snapshot();
    },
  };
  const originalFetch = window.fetch;
  replace(
    window,
    "fetch",
    function (this: any, input: RequestInfo | URL, init?: RequestInit) {
      const result = Reflect.apply(originalFetch, this, [input, init]);
      try {
        const url = new URL(
          input instanceof Request ? input.url : String(input),
          location.href,
        );
        if (!url.pathname.startsWith("/api/")) return result;
        const method =
            init?.method ?? (input instanceof Request ? input.method : "GET"),
          headers = new Headers(
            init?.headers ??
              (input instanceof Request ? input.headers : undefined),
          );
        if (
          retired ||
          rows.length >= 128 ||
          url.origin !== config.origin ||
          method !== "GET" ||
          init?.body != null ||
          headers.has("idempotency-key") ||
          headers.has("x-csrf-token")
        )
          throw Error("EXECUTION_NATIVE_READ_ONLY");
        const matches = calls.filter(
          (call) => !call.bound && call.path === url.pathname && !call.settled,
        );
        if (matches.length !== 1)
          throw Error("EXECUTION_NATIVE_ORIGINAL_CALL_IDENTITY");
        const call = matches[0];
        call.bound++;
        const row: any = {
          id: rows.length + 1,
          call_id: call.id,
          method,
          path: url.pathname,
          request_id: null,
          status: null,
          fetch_returned: false,
          fetch_rejected: false,
          readers: 0,
          reads: 0,
          read_returns: 0,
          bytes: 0,
          eof: false,
          closed: false,
          release: 0,
          reader_cancel: 0,
          stream_cancel: 0,
          aborted: false,
          error: false,
          chunks: [],
        };
        rows.push(row);
        track(
          digest(new TextEncoder().encode(url.pathname + url.search)),
          (value) => {
            row.url_digest = value;
          },
        );
        if (call.query) {
          const q =
            call.kind === "usage.daily"
              ? { ...call.query, limit: 100, group_by: "day" }
              : call.query;
          const actual = [...url.searchParams.entries()].sort(),
            expected = Object.entries(q)
              .map(([k, v]) => [k, String(v)])
              .sort();
          if (JSON.stringify(actual) !== JSON.stringify(expected))
            throw Error("EXECUTION_NATIVE_ORIGINAL_QUERY_DIFFERS");
          call.query_bound = row.query_bound = true;
        }
        const signal =
          init?.signal ?? (input instanceof Request ? input.signal : undefined);
        const abort = () => {
          row.aborted = true;
        };
        signal?.addEventListener("abort", abort, { once: true });
        restore.push(() => signal?.removeEventListener("abort", abort));
        track(
          result,
          (response) => {
            row.fetch_returned = true;
            row.status = response.status;
            row.length = response.headers.get("content-length");
            row.request_id = response.headers.get("x-request-id");
            if (!response.body) throw Error("EXECUTION_NATIVE_BODY_MISSING");
            streams.set(response.body, row);
          },
          () => {
            row.fetch_rejected = true;
          },
        );
      } catch {
        failed = true;
      }
      return result;
    },
  );
  const originalGetReader = ReadableStream.prototype.getReader;
  replace(
    ReadableStream.prototype,
    "getReader",
    function (this: ReadableStream, ...args: any[]) {
      const reader = Reflect.apply(originalGetReader, this, args),
        row = streams.get(this);
      if (row) {
        row.readers++;
        readers.set(reader, row);
        track(
          reader.closed,
          () => {
            row.closed = true;
          },
          () => {
            row.error = true;
          },
        );
      }
      return reader;
    },
  );
  const originalRead = ReadableStreamDefaultReader.prototype.read;
  replace(
    ReadableStreamDefaultReader.prototype,
    "read",
    function (this: ReadableStreamDefaultReader, ...args: any[]) {
      const result = Reflect.apply(originalRead, this, args),
        row = readers.get(this);
      if (row) {
        row.reads++;
        track(
          result,
          async (part) => {
            row.read_returns++;
            if (!part.done) {
              row.bytes += part.value.byteLength;
              if (row.bytes > 1 << 20)
                throw Error("EXECUTION_NATIVE_BODY_BOUND");
              row.chunks.push(part.value.slice());
              return;
            }
            if (row.eof) throw Error("EXECUTION_NATIVE_DUPLICATE_EOF");
            row.eof = true;
            const body = new Uint8Array(row.bytes);
            let offset = 0;
            for (const chunk of row.chunks) {
              body.set(chunk, offset);
              offset += chunk.length;
              chunk.fill(0);
            }
            row.chunks = [];
            try {
              row.digest = await digest(body);
              let value = JSON.parse(
                new TextDecoder("utf-8", { fatal: true }).decode(body),
              );
              if (row.path === "/api/v1/session")
                value = { user: value.user, session: value.session };
              row.typed_digest = await typedDigest(value);
            } finally {
              body.fill(0);
            }
          },
          () => {
            row.error = true;
          },
        );
      }
      return result;
    },
  );
  for (const [object, key, map, counter] of [
    [ReadableStreamDefaultReader.prototype, "cancel", readers, "reader_cancel"],
    [ReadableStream.prototype, "cancel", streams, "stream_cancel"],
    [ReadableStreamDefaultReader.prototype, "releaseLock", readers, "release"],
  ] as const) {
    const original = (object as any)[key];
    replace(object, key, function (this: any, ...args: any[]) {
      const result = Reflect.apply(original, this, args);
      const row = (map as WeakMap<any, any>).get(this);
      if (row) {
        row[counter]++;
        if (key === "cancel")
          track(
            result,
            () => {},
            () => {
              row.error = true;
            },
          );
      }
      return result;
    });
  }
}

type Entry = {
  seq: number;
  request: Request;
  response?: Response;
  status?: number;
  rid?: string;
  terminal?: "finished" | "failed";
  failure?: string;
  finished: number;
  failed: number;
  responses: number;
  checked: boolean;
  name: string;
  body?: any;
  index?: number;
};
export async function observeExecutionUsage(
  page: Page,
  data: ExecutionUsageMaterial,
) {
  const evidence = process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_EVIDENCE!,
    entries: Entry[] = [],
    proofs: any[] = [];
  const prefix = `/api/v1/projects/${data.project_id}`,
    binding = await knowledgeSessionBinding(
      resolve(process.cwd(), "../.."),
      process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_DIST!,
    );
  if (binding.entry === binding.asset)
    throw Error("EXECUTION_USAGE_ENTRY_IS_SINGLETON");
  let failure: Error | undefined,
    count = 0,
    verified = 0,
    cursor: string | null = null,
    window: { from: string; to: string } | undefined,
    retired = false,
    completed = false,
    handlers = 0;
  const daily = (i: number) => [4, 9, 10].includes(i),
    filtered = (i: number) => i >= 2 && i <= 9,
    continuation = (i: number) => [1, 3, 6, 12].includes(i);
  const wire = await observeUsageWire(page, data.origin);
  const captureError = (fn: () => void) => {
    try {
      fn();
    } catch (error) {
      failure ??=
        error instanceof Error
          ? error
          : Error("EXECUTION_USAGE_OBSERVER_FAILURE");
    }
  };
  function onRequest(request: Request) {
    captureError(() => {
      const url = new URL(request.url());
      if (!url.pathname.startsWith("/api/")) return;
      if (retired || entries.length >= 128)
        throw Error("EXECUTION_USAGE_REQUEST_BOUND");
      expect(url.origin).toBe(data.origin);
      expect(request.method()).toBe("GET");
      expect(request.postDataBuffer()).toBeNull();
      expect(request.headers()["idempotency-key"]).toBeUndefined();
      expect(request.headers()["x-csrf-token"]).toBeUndefined();
      const globals: Record<string, string> = {
        "/api/v1/session": "session",
        "/api/v1/projects/resolve": "resolve",
        "/api/v1/projects": "projects",
        [prefix]: "project",
      };
      let name = globals[url.pathname],
        index: number | undefined;
      if (
        url.pathname === prefix + "/model-usage" ||
        url.pathname === prefix + "/model-usage/summary"
      ) {
        index = count;
        expect(verified).toBe(count);
        expect(count).toBeLessThan(13);
        name = daily(index) ? "summary" : "usage";
        expect(url.pathname).toBe(
          prefix + "/model-usage" + (daily(index) ? "/summary" : ""),
        );
        const from = url.searchParams.get("from")!,
          to = url.searchParams.get("to")!;
        expect(from).toMatch(instant);
        expect(to).toMatch(instant);
        expect(Date.parse(to) - Date.parse(from)).toBe(30 * 86400000);
        if (index === 0) window = { from, to };
        else if (index === 8) {
          expect(from > window!.from && to > window!.to).toBe(true);
          window = { from, to };
        } else expect({ from, to }).toEqual(window);
        const expected: Record<string, string> = {
          from,
          to,
          limit: daily(index) ? "100" : "50",
          ...(daily(index) ? { group_by: "day" } : {}),
          ...(filtered(index) ? { execution_id: data.execution_id } : {}),
        };
        if (continuation(index)) {
          expect(cursor).not.toBeNull();
          expected.cursor = cursor!;
        }
        expect([...url.searchParams.entries()].sort()).toEqual(
          Object.entries(expected).sort(),
        );
        count++;
      }
      if (!name) throw Error("EXECUTION_USAGE_UNDECLARED_ENDPOINT");
      entries.push({
        seq: entries.length + 1,
        request,
        name,
        index,
        finished: 0,
        failed: 0,
        responses: 0,
        checked: false,
      });
    });
  }
  const entryFor = (request: Request) =>
    entries.find((row) => row.request === request);
  const onResponse = (response: Response) =>
    captureError(() => {
      const row = entryFor(response.request());
      if (!row) return;
      row.responses++;
      row.response = response;
      row.status = response.status();
      row.rid = response.headers()["x-request-id"];
    });
  const onFinished = (request: Request) =>
    captureError(() => {
      const row = entryFor(request);
      if (!row) return;
      if (row.terminal) throw Error("EXECUTION_USAGE_DUPLICATE_TERMINAL");
      row.terminal = "finished";
      row.finished++;
    });
  const onFailed = (request: Request) =>
    captureError(() => {
      const row = entryFor(request);
      if (!row) return;
      if (row.terminal) throw Error("EXECUTION_USAGE_DUPLICATE_TERMINAL");
      row.terminal = "failed";
      row.failed++;
      row.failure = request.failure()?.errorText;
    });
  const onClose = () => {
    if (!retired) failure ??= Error("EXECUTION_USAGE_PAGE_CLOSED_EARLY");
  };
  page.on("request", onRequest);
  page.on("response", onResponse);
  page.on("requestfinished", onFinished);
  page.on("requestfailed", onFailed);
  page.on("close", onClose);
  await page.addInitScript(installExecutionNative, {
    origin: data.origin,
    project: data.project_id,
    execution: data.execution_id,
  });
  const entryURL = new URL(binding.entry, data.origin).href;
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(async (binding) => {
        const module = await import(/* @vite-ignore */ binding.asset);
        const use = module[binding.exportName];
        if (typeof use !== "function")
          throw Error("EXECUTION_USAGE_SINGLETON_EXPORT");
        const auth = use();
        if (use() !== auth) throw Error("EXECUTION_USAGE_SINGLETON_IDENTITY");
        (window as any).__executionUsageNative.bind(auth);
      }, binding);
      await route.continue();
    } catch (error) {
      failure ??=
        error instanceof Error ? error : Error("EXECUTION_USAGE_BIND_FAILURE");
      await route.abort();
    } finally {
      handlers--;
    }
  };
  await page.route(entryURL, install);
  async function verify() {
    if (failure) throw failure;
    await expect
      .poll(() => entries.filter((row) => !row.terminal).length, {
        timeout: 5000,
      })
      .toBe(0);
    await expect
      .poll(
        () =>
          page.evaluate(
            () => (window as any).__executionUsageNative.snapshot().pending,
          ),
        { timeout: 5000 },
      )
      .toBe(0);
    if (failure) throw failure;
    const original = await page.evaluate(() =>
      (window as any).__executionUsageNative.snapshot(),
    );
    expect(original.failed).toBe(false);
    expect(original.current && original.not_busy).toBe(true);
    expect(page.isClosed()).toBe(false);
    expect(original.rows).toHaveLength(entries.length);
    expect(original.calls).toHaveLength(entries.length);
    expect(new Set(original.rows.map((row: any) => row.request_id)).size).toBe(
      entries.length,
    );
    for (const entry of entries.filter((row) => !row.checked)) {
      expect(entry.responses).toBe(1);
      expect(entry.status).toBe(200);
      expect(entry.rid).toMatch(uuid);
      const matches = original.rows.filter(
        (row: any) => row.request_id === entry.rid,
      );
      expect(matches).toHaveLength(1);
      const n = matches[0];
      const calls = original.calls.filter((call: any) => call.id === n.call_id);
      expect(calls).toHaveLength(1);
      const c = calls[0];
      const url = new URL(entry.request.url());
      const go = JSON.parse(
        readFileSync(
          join(
            evidence,
            `usage-execution-filter-${data.mode}-response-${entry.rid}.json`,
          ),
          "utf8",
        ),
      );
      expect(go.input_hash).toBe(data.input_hash);
      expect(go.request_id).toBe(entry.rid);
      expect(go.method).toBe("GET");
      expect(go.status).toBe(200);
      expect(go.url_sha256).toBe(hash(url.pathname + url.search));
      expect(
        go.original_eof &&
          go.original_closed &&
          go.handler_returned &&
          go.browser_ingress_host_matches &&
          go.downstream_host_matches,
      ).toBe(true);
      expect(go.handler_return_source).toBe("app_http_request_log");
      expect(n.method).toBe("GET");
      expect(n.path).toBe(url.pathname);
      expect(n.url_digest).toBe(go.url_sha256);
      expect(n.status).toBe(200);
      expect(n.fetch_returned && n.eof && n.closed).toBe(true);
      expect(n.fetch_rejected || n.aborted || n.error).toBe(false);
      expect(n.readers).toBe(1);
      expect(n.reads).toBeGreaterThan(0);
      expect(n.read_returns).toBe(n.reads);
      expect(n.release).toBe(1);
      expect(n.reader_cancel + n.stream_cancel).toBe(0);
      expect(n.length).toMatch(/^[1-9][0-9]*$/);
      expect(Number(n.length)).toBe(n.bytes);
      expect(go.content_length).toBe(n.bytes);
      expect(go.body_sha256).toBe(n.digest);
      expect(c.bound).toBe(1);
      expect(c.path).toBe(n.path);
      expect(c.fulfilled && c.settled && c.current && c.not_busy).toBe(true);
      expect(c.rejected).toBe(false);
      expect(c.typed_digest).toMatch(/^[0-9a-f]{64}$/);
      expect(n.typed_digest).toBe(c.typed_digest);
      expect(c.kind).toBe(
        {
          session: "restore",
          resolve: "project.resolve",
          projects: "project.list",
          project: "project.get",
          usage: "usage.list",
          summary: "usage.daily",
        }[entry.name],
      );
      if (entry.index !== undefined)
        expect(c.query_bound && n.query_bound).toBe(true);
      const headers = await wire.headers({
        method: "GET",
        url: entry.request.url(),
        status: 200,
        requestID: entry.rid!,
      });
      expect(headers.host_present && headers.host_matches).toBe(true);
      if (entry.terminal === "failed") {
        expect(entry.finished).toBe(0);
        expect(entry.failed).toBe(1);
        expect(entry.failure).toBe("net::ERR_ABORTED");
      } else {
        expect(entry.finished).toBe(1);
        expect(entry.failed).toBe(0);
        let timer: ReturnType<typeof setTimeout> | undefined;
        try {
          expect(
            await Promise.race([
              entry.response!.finished(),
              new Promise<never>((_, reject) => {
                timer = setTimeout(
                  () => reject(Error("EXECUTION_USAGE_FINISHED_TAIL")),
                  2000,
                );
              }),
            ]),
          ).toBeNull();
        } finally {
          if (timer) clearTimeout(timer);
        }
        const body = await entry.response!.body();
        expect(body.length).toBe(n.bytes);
        expect(hash(body)).toBe(n.digest);
        let parsed = JSON.parse(body.toString("utf8"));
        if (entry.name === "session")
          parsed = { user: parsed.user, session: parsed.session };
        expect(c.typed).toEqual(parsed);
      }
      entry.body = c.typed;
      entry.checked = true;
      const { typed, query, ...consumer } = c;
      proofs.push({
        seq: entry.seq,
        request_id: entry.rid,
        terminal: entry.terminal,
        native: n,
        consumer,
        go,
        wire: headers,
      });
    }
  }
  return {
    verify,
    read(index: number) {
      expect(verified).toBe(index);
      expect(count).toBe(index + 1);
      const entry = entries.find((row) => row.index === index);
      expect(entry?.checked).toBe(true);
      const body = entry!.body;
      if (daily(index)) {
        expect(Object.keys(body).sort()).toEqual([
          "as_of",
          "items",
          "next_cursor",
        ]);
        expect(body.next_cursor).toBeNull();
        expect(body.as_of).toMatch(instant);
        const expected = filtered(index)
          ? data.target_summary
          : data.all_summary;
        expect(body.items).toEqual(
          expected.map((row) => ({
            ...row,
            summary: { ...row.summary, as_of: body.as_of },
          })),
        );
      } else {
        expect(Object.keys(body).sort()).toEqual(["items", "next_cursor"]);
        const rows = filtered(index) ? data.target_items : data.items;
        expect(body.items).toEqual(
          continuation(index) ? rows.slice(50) : rows.slice(0, 50),
        );
        if (continuation(index)) expect(body.next_cursor).toBeNull();
        else {
          expect(typeof body.next_cursor).toBe("string");
          expect(body.next_cursor.length).toBeGreaterThan(0);
        }
        cursor = body.next_cursor;
      }
      verified++;
      return body;
    },
    async finish() {
      await verify();
      expect(verified).toBe(13);
      expect(count).toBe(13);
      wire.complete();
      completed = true;
    },
    async retire() {
      if (retired) return;
      try {
        await expect.poll(() => handlers).toBe(0);
        if (!page.isClosed()) {
          await expect
            .poll(() =>
              page.evaluate(
                () => (window as any).__executionUsageNative.snapshot().pending,
              ),
            )
            .toBe(0);
          const result = await page.evaluate(() =>
            (window as any).__executionUsageNative.finish(),
          );
          expect(result.retired).toBe(true);
          expect(result.failed).toBe(false);
        } else throw Error("EXECUTION_USAGE_RETIRE_ORIGINAL_DOCUMENT");
        await page.unroute(entryURL, install);
        wire.complete();
      } finally {
        await wire.close();
        page.off("request", onRequest);
        page.off("response", onResponse);
        page.off("requestfinished", onFinished);
        page.off("requestfailed", onFailed);
        page.off("close", onClose);
        retired = true;
      }
    },
    save(status?: string) {
      mkdirSync(evidence, { recursive: true, mode: 0o700 });
      const result = {
        input_hash: data.input_hash,
        completed,
        all_observed: entries.every((row) => row.checked),
        original_requests: entries.length,
        reads: count,
        lists: entries.filter((row) => row.name === "usage").length,
        summaries: entries.filter((row) => row.name === "summary").length,
        writes: entries.filter((row) => row.request.method() !== "GET").length,
        status,
        retired,
      };
      writeFileSync(
        join(evidence, "usage-execution-filter-proof.json"),
        JSON.stringify(
          {
            ...result,
            requests: entries.map(({ request, response, body, ...row }) => row),
            joint_proofs: proofs,
            wire: wire.snapshot(),
          },
          null,
          2,
        ) + "\n",
        { mode: 0o600 },
      );
      if (completed && retired && status === "passed")
        writeFileSync(
          join(
            process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
            "usage-execution-filter-result.json",
          ),
          JSON.stringify(result),
          { mode: 0o600 },
        );
    },
  };
}

export async function executionUsageScreenshot(page: Page, name: string) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
  expect(
    await page.evaluate(
      () => matchMedia("(prefers-reduced-motion: reduce)").matches,
    ),
  ).toBe(true);
  const brand = page.locator(".app-brand-image:visible");
  await expect(brand).toHaveCount(1);
  await expect
    .poll(() =>
      brand.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_USAGE_EXECUTION_FILTER_WEB_EVIDENCE!,
      `usage-execution-filter-${name}.png`,
    ),
    animations: "disabled",
  });
}
