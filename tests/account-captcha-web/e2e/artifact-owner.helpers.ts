import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
import { observeSystemRunnerWire } from "./system-runner-management.helpers";
const digest = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");
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
export function artifactMaterial() {
  const data = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "artifact-owner-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("catalog-and-bounded-read");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_ARTIFACT_OWNER_WEB_INPUT_HASH,
  );
  expect(data.rows).toHaveLength(52);
  expect(
    new Set(data.rows.map((row: any) => row.artifact_ref.artifact_id)).size,
  ).toBe(52);
  expect(data.rows[51]).toEqual(data.text_artifact);
  expect(data.first_text.length).toBe(8190);
  expect(data.text).toBe(data.first_text + data.second_text);
  expect(data.second_text.startsWith("🙂")).toBe(true);
  for (const item of data.rows) {
    expect(item.artifact_ref.project_id).toBe(data.project_id);
    expect(item.kind).toBe("generated");
    expect(Object.keys(item.object).sort()).toEqual([
      "byte_size",
      "media_type",
      "sha256",
    ]);
  }
  expect(data.other_artifact.artifact_ref.project_id).toBe(
    data.other_project_id,
  );
  expect(data.foreign_artifact.artifact_ref.project_id).toBe(
    data.foreign_project_id,
  );
  expect(
    new Set([data.project_id, data.other_project_id, data.foreign_project_id])
      .size,
  ).toBe(3);
  return data;
}

// Passive wrappers return each original promise/result unchanged. Only the
// application's own reader sees the response stream: no clone, tee or fetch.
function installArtifactNative(config: { origin: string; projects: string[] }) {
  const host = window as any,
    rows: any[] = [],
    calls: any[] = [],
    restore: (() => void)[] = [],
    pending = new Set<Promise<void>>();
  let auth: any,
    failed = false,
    retired = false,
    failureReason = "";
  const knownReasons = new Set([
    "ARTIFACT_NATIVE_DESCRIPTOR",
    "ARTIFACT_NATIVE_BOUND",
    "ARTIFACT_NATIVE_SCOPE",
    "ARTIFACT_ORIGINAL_PROMISE",
    "ARTIFACT_UNEXPECTED_REJECTION",
    "ARTIFACT_DUPLICATE_BIND",
    "ARTIFACT_NATIVE_READONLY",
    "ARTIFACT_NATIVE_CALL_BIND",
    "ARTIFACT_NATIVE_BODY",
    "ARTIFACT_DUPLICATE_EOF",
    "ARTIFACT_CHUNK",
    "ARTIFACT_BODY_BOUND",
  ]);
  function observerFailed(error?: unknown) {
    failed = true;
    if (!failureReason)
      failureReason =
        error instanceof Error && knownReasons.has(error.message)
          ? error.message
          : "ARTIFACT_OBSERVER_ERROR";
  }
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
  const hash = async (bytes: Uint8Array<ArrayBuffer>) =>
    [...new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))]
      .map((value) => value.toString(16).padStart(2, "0"))
      .join("");
  function watch(
    promise: Promise<any>,
    good: (value: any) => any,
    bad: (error: any) => any = observerFailed,
  ) {
    const tail = promise
      .then(good, bad)
      .catch(observerFailed)
      .then(() => {
        pending.delete(tail);
      });
    pending.add(tail);
  }
  function replace(object: any, key: string, wrapper: any) {
    const descriptor = Object.getOwnPropertyDescriptor(object, key);
    if (!descriptor || typeof descriptor.value !== "function")
      throw Error("ARTIFACT_NATIVE_DESCRIPTOR");
    Object.defineProperty(object, key, { ...descriptor, value: wrapper });
    restore.push(() => {
      if (object[key] !== wrapper) observerFailed();
      Object.defineProperty(object, key, descriptor);
    });
  }
  type Consumer =
    | "list"
    | "read"
    | "session.restore"
    | "project.list"
    | "project.resolve"
    | "project.get";
  function consumer(method: Consumer) {
    const artifact = method === "list" || method === "read";
    const target = artifact
      ? auth.projectArtifacts
      : method === "session.restore"
        ? auth
        : auth.projects;
    const key = method.split(".").at(-1)!;
    const original = target[key];
    replace(target, key, function (this: any, ...args: any[]) {
      if (retired || calls.length >= 64) throw Error("ARTIFACT_NATIVE_BOUND");
      let path: string,
        expectedQuery = "";
      if (artifact) {
        const project = method === "list" ? args[0] : args[0].project_id;
        if (!config.projects.includes(project) || args.length !== 2)
          throw Error("ARTIFACT_NATIVE_SCOPE");
        path =
          `/api/v1/projects/${project}/artifacts` +
          (method === "read"
            ? `/${args[0].artifact_id}/files/${args[0].file_id}/content`
            : "");
      } else if (method === "session.restore") {
        if (args.length !== 0) throw Error("ARTIFACT_NATIVE_SCOPE");
        path = "/api/v1/session";
      } else {
        if (args.length !== 1) throw Error("ARTIFACT_NATIVE_SCOPE");
        if (method === "project.get" && !config.projects.includes(args[0]))
          throw Error("ARTIFACT_NATIVE_SCOPE");
        if (
          method === "project.resolve" &&
          (args[0]?.username !== "artifact-owner" ||
            !["artifact-browser", "artifact-browser-other"].includes(
              args[0]?.project_name,
            ))
        )
          throw Error("ARTIFACT_NATIVE_SCOPE");
        path =
          "/api/v1/projects" +
          (method === "project.get"
            ? `/${args[0]}`
            : method === "project.resolve"
              ? "/resolve"
              : "");
        const query = new URLSearchParams();
        if (method === "project.resolve") {
          query.set("username", args[0].username.toLowerCase());
          query.set("project_name", args[0].project_name.toLowerCase());
        } else if (method === "project.list") {
          query.set("limit", String(args[0].limit));
          if (args[0].lifecycle)
            query.set(
              "lifecycle",
              ["active", "archiving", "archived", "deleting"]
                .filter((value) => args[0].lifecycle.includes(value))
                .join(","),
            );
          if (args[0].cursor !== undefined) query.set("cursor", args[0].cursor);
        }
        expectedQuery = query.toString();
      }
      const call: any = {
        id: calls.length + 1,
        method,
        path,
        artifact,
        expected_query: expectedQuery,
        query: artifact ? JSON.parse(JSON.stringify(args[1])) : null,
        bound: 0,
        settled: false,
        fulfilled: false,
        rejected: false,
        current: false,
        typed_digest: null,
      };
      calls.push(call);
      const result = Reflect.apply(original, this, args);
      if (!(result instanceof Promise))
        throw Error("ARTIFACT_ORIGINAL_PROMISE");
      watch(
        result,
        async (value) => {
          call.settled = true;
          call.fulfilled = true;
          call.current =
            auth.state.phase === "authenticated" && auth.state.busy === false;
          if (!artifact) {
            if (!value || typeof value !== "object")
              throw Error("ARTIFACT_UNEXPECTED_REJECTION");
            if (
              method === "session.restore" &&
              (value.user?.id !== auth.state.user?.id ||
                value.session?.id !== auth.state.session?.id)
            )
              throw Error("ARTIFACT_NATIVE_SCOPE");
            if (
              (method === "project.resolve" || method === "project.get") &&
              (!config.projects.includes(value.id) ||
                value.owner_user_id !== auth.state.user?.id)
            )
              throw Error("ARTIFACT_NATIVE_SCOPE");
            if (
              method === "project.list" &&
              (!Array.isArray(value.items) ||
                value.items.some((v: any) => !config.projects.includes(v.id)))
            )
              throw Error("ARTIFACT_NATIVE_SCOPE");
          }
          call.typed_digest = await hash(
            new TextEncoder().encode(JSON.stringify(canonical(value))),
          );
        },
        async (error) => {
          call.settled = true;
          call.rejected = true;
          if (!error?.problem) throw Error("ARTIFACT_UNEXPECTED_REJECTION");
          call.typed_digest = await hash(
            new TextEncoder().encode(JSON.stringify(canonical(error.problem))),
          );
        },
      );
      return result;
    });
  }
  host.__artifactNative = {
    bind(value: any) {
      if (auth || retired) throw Error("ARTIFACT_DUPLICATE_BIND");
      auth = value;
      consumer("list");
      consumer("read");
      consumer("session.restore");
      consumer("project.list");
      consumer("project.resolve");
      consumer("project.get");
    },
    snapshot() {
      return {
        failed,
        failure_reason: failureReason,
        retired,
        pending: pending.size,
        not_busy: auth?.state.busy === false,
        rows: rows.map(({ chunks, ...row }) => row),
        calls: calls.map((call) => ({ ...call })),
      };
    },
    finish() {
      if (pending.size) throw Error("ARTIFACT_ORIGINAL_TAIL_PENDING");
      if (!retired) {
        for (const undo of restore.reverse()) undo();
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
          url.origin !== config.origin ||
          method !== "GET" ||
          init?.body != null ||
          headers.has("x-csrf-token") ||
          headers.has("idempotency-key") ||
          rows.length >= 64
        )
          throw Error("ARTIFACT_NATIVE_READONLY");
        const matches = calls.filter(
          (call) =>
            call.path === url.pathname &&
            (call.artifact || call.expected_query === url.search.slice(1)) &&
            !call.bound &&
            !call.settled,
        );
        if (matches.length !== 1) throw Error("ARTIFACT_NATIVE_CALL_BIND");
        const call = matches[0];
        call.bound++;
        const row: any = {
          id: rows.length + 1,
          call_id: call.id,
          artifact: call.artifact,
          path: url.pathname,
          query: url.search.slice(1),
          rid: null,
          status: null,
          length: null,
          fetch_returned: false,
          fetch_rejected: false,
          readers: 0,
          reads: 0,
          read_returns: 0,
          eof: false,
          release: 0,
          cancels: 0,
          cancel_returns: 0,
          bytes: 0,
          digest: null,
          wire_typed_digest: null,
          artifact_value: null,
          chunks: [],
          failed: false,
        };
        rows.push(row);
        watch(
          result,
          (response: globalThis.Response) => {
            row.fetch_returned = true;
            row.rid = response.headers.get("x-request-id");
            row.status = response.status;
            row.length = response.headers.get("content-length");
            const stream = response.body;
            if (!stream) throw Error("ARTIFACT_NATIVE_BODY");
            // Attach own methods to this exact stream/reader, preserving prototype
            // descriptors and restoring the original shape at retirement.
            function own(object: any, key: string, wrapper: any) {
              const before = Object.getOwnPropertyDescriptor(object, key);
              Object.defineProperty(object, key, {
                value: wrapper,
                writable: true,
                configurable: true,
              });
              restore.push(() => {
                if (object[key] !== wrapper) observerFailed();
                if (before) Object.defineProperty(object, key, before);
                else delete object[key];
              });
            }
            const getReader = stream.getReader,
              outerCancel = stream.cancel;
            own(stream, "cancel", function (this: any, ...args: any[]) {
              row.cancels++;
              const value = Reflect.apply(outerCancel, this, args);
              watch(value, () => {
                row.cancel_returns++;
              });
              return value;
            });
            own(stream, "getReader", function (this: any, ...args: any[]) {
              const reader = Reflect.apply(
                getReader,
                this,
                args,
              ) as ReadableStreamDefaultReader<Uint8Array>;
              row.readers++;
              const read = reader.read,
                cancel = reader.cancel,
                release = reader.releaseLock;
              own(reader, "read", function (this: any, ...args: any[]) {
                row.reads++;
                const value = Reflect.apply(read, this, args);
                watch(
                  value,
                  async (part: ReadableStreamReadResult<Uint8Array>) => {
                    row.read_returns++;
                    if (part.done) {
                      if (row.eof) throw Error("ARTIFACT_DUPLICATE_EOF");
                      row.eof = true;
                      const bytes = new Uint8Array(row.bytes);
                      let at = 0;
                      for (const chunk of row.chunks) {
                        bytes.set(chunk, at);
                        at += chunk.byteLength;
                      }
                      for (const chunk of row.chunks) chunk.fill(0);
                      row.chunks = [];
                      try {
                        row.digest = await hash(bytes);
                        let parsed = JSON.parse(
                          new TextDecoder("utf-8", { fatal: true }).decode(
                            bytes,
                          ),
                        );
                        if (row.artifact) row.artifact_value = parsed;
                        else {
                          // The Session wire contains CSRF material; only its public
                          // original restore result is projected, hashed, and discarded.
                          if (call.method === "session.restore")
                            parsed = {
                              user: parsed.user,
                              session: parsed.session,
                            };
                          row.wire_typed_digest = await hash(
                            new TextEncoder().encode(
                              JSON.stringify(canonical(parsed)),
                            ),
                          );
                        }
                      } finally {
                        bytes.fill(0);
                      }
                    } else {
                      if (!(part.value instanceof Uint8Array))
                        throw Error("ARTIFACT_CHUNK");
                      row.bytes += part.value.byteLength;
                      if (row.bytes > 8 << 20)
                        throw Error("ARTIFACT_BODY_BOUND");
                      row.chunks.push(part.value.slice());
                    }
                  },
                  () => {
                    row.failed = true;
                  },
                );
                return value;
              });
              own(reader, "cancel", function (this: any, ...args: any[]) {
                row.cancels++;
                const value = Reflect.apply(cancel, this, args);
                watch(value, () => {
                  row.cancel_returns++;
                });
                return value;
              });
              own(reader, "releaseLock", function (this: any, ...args: any[]) {
                const value = Reflect.apply(release, this, args);
                row.release++;
                return value;
              });
              return reader;
            });
          },
          () => {
            row.fetch_rejected = true;
          },
        );
      } catch (error) {
        observerFailed(error);
      }
      return result;
    },
  );
}

type Entry = {
  request: Request;
  response?: Response;
  finished: boolean;
  failed: boolean;
  responseCount: number;
  finishedCount: number;
  failedCount: number;
  failureKind: "not_observed" | "net::ERR_ABORTED" | "other" | "unavailable";
  checked: boolean;
  body?: any;
  uri: string;
  status?: number;
  rid?: string;
};
export async function observeArtifacts(
  page: Page,
  data: ReturnType<typeof artifactMaterial>,
) {
  const evidence = process.env.AGENTEAM_ARTIFACT_OWNER_WEB_EVIDENCE!,
    entries: Entry[] = [],
    auxiliary: Entry[] = [],
    proof: any[] = [],
    auxiliaryProof: any[] = [];
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_ARTIFACT_OWNER_WEB_DIST!,
  );
  const wire = await observeSystemRunnerWire(page, data.origin);
  let failure = false,
    retired = false,
    completed = false,
    handlers = 0,
    stage = "entry",
    failureStage = "";
  let nativeDiagnostic: Record<string, unknown> = {
    available: false,
    reason: "not_sampled",
  };
  const owned = [data.project_id, data.other_project_id];
  const selected = (request: Request) =>
    new URL(request.url()).pathname.includes("/artifacts");
  const request = (request: Request) => {
    try {
      const url = new URL(request.url());
      if (url.origin !== data.origin) throw Error("ARTIFACT_EXTERNAL_REQUEST");
      if (!url.pathname.startsWith("/api/")) return;
      if (request.method() !== "GET" || request.postData() !== null)
        throw Error("ARTIFACT_BROWSER_WRITE");
      if (!selected(request)) {
        auxiliary.push({
          request,
          finished: false,
          failed: false,
          responseCount: 0,
          finishedCount: 0,
          failedCount: 0,
          failureKind: "not_observed",
          checked: false,
          uri: url.pathname + url.search,
        });
        return;
      }
      expect(entries.length).toBeLessThan(64);
      expect(
        owned.some((p) =>
          url.pathname.startsWith(`/api/v1/projects/${p}/artifacts`),
        ),
      ).toBe(true);
      entries.push({
        request,
        finished: false,
        failed: false,
        responseCount: 0,
        finishedCount: 0,
        failedCount: 0,
        failureKind: "not_observed",
        checked: false,
        uri: url.pathname + url.search,
      });
    } catch {
      failure = true;
      failureStage ||= "request_observation";
    }
  };
  const response = (response: Response) => {
    const row = [...entries, ...auxiliary].find(
      (row) => row.request === response.request(),
    );
    if (row) {
      row.responseCount++;
      if (row.responseCount !== 1) {
        failure = true;
        failureStage ||= "duplicate_response";
      }
      row.response = response;
    }
  };
  const finished = (request: Request) => {
    const row = [...entries, ...auxiliary].find(
      (row) => row.request === request,
    );
    if (row) {
      row.finishedCount++;
      if (row.finishedCount !== 1 || row.failedCount !== 0) {
        failure = true;
        failureStage ||= "duplicate_terminal";
      }
      row.finished = true;
    }
  };
  const failed = (request: Request) => {
    if (new URL(request.url()).pathname.startsWith("/api/")) {
      const row = [...entries, ...auxiliary].find(
        (row) => row.request === request,
      );
      if (row) {
        row.failed = true;
        row.failedCount++;
        if (row.failedCount !== 1 || row.finishedCount !== 0)
          failureStage ||= "duplicate_terminal";
        // Only the actual event's exact closed enum survives. No raw browser
        // error is saved, and no cause is inferred from a fulfilled consumer.
        try {
          const originalFailure = request.failure();
          row.failureKind =
            originalFailure?.errorText === "net::ERR_ABORTED"
              ? "net::ERR_ABORTED"
              : originalFailure
                ? "other"
                : "unavailable";
        } catch {
          row.failureKind = "unavailable";
        }
      }
      if (
        !row ||
        row.failureKind !== "net::ERR_ABORTED" ||
        row.failedCount !== 1 ||
        row.finishedCount !== 0
      ) {
        failure = true;
        failureStage ||= "request_failed";
      }
    }
  };
  page.on("request", request);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", failed);
  await page.addInitScript(installArtifactNative, {
    origin: data.origin,
    projects: owned,
  });
  const entryURL = new URL(binding.entry, data.origin).href;
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(async (binding) => {
        const mod = await import(/* @vite-ignore */ binding.asset),
          use = mod[binding.exportName];
        if (typeof use !== "function" || use() !== use())
          throw Error("ARTIFACT_SINGLETON");
        (window as any).__artifactNative.bind(use());
      }, binding);
      await route.continue();
    } catch {
      failure = true;
      failureStage ||= "singleton_binding";
      await route.abort();
    } finally {
      handlers--;
    }
  };
  await page.route(entryURL, install);
  async function verify() {
    expect(failure).toBe(false);
    await expect
      .poll(async () => {
        const n = await page.evaluate(() =>
          (window as any).__artifactNative.snapshot(),
        );
        return (
          n.pending === 0 &&
          n.not_busy &&
          n.calls.every((c: any) => c.settled) &&
          [...entries, ...auxiliary].every(
            (e) =>
              e.response &&
              e.responseCount === 1 &&
              ((e.finished &&
                !e.failed &&
                e.finishedCount === 1 &&
                e.failedCount === 0) ||
                (!e.finished &&
                  e.failed &&
                  e.finishedCount === 0 &&
                  e.failedCount === 1 &&
                  e.failureKind === "net::ERR_ABORTED")),
          ) &&
          n.rows.length === entries.length + auxiliary.length
        );
      })
      .toBe(true);
    const native = await page.evaluate(() =>
      (window as any).__artifactNative.snapshot(),
    );
    expect(failure).toBe(false);
    expect(native.failed).toBe(false);
    expect(native.retired).toBe(false);
    function consumed(e: Entry, rid: string) {
      expect(page.isClosed()).toBe(false);
      expect(native.rows.filter((row: any) => row.rid === rid)).toHaveLength(1);
      const n = native.rows.find((row: any) => row.rid === rid);
      expect(
        native.calls.filter((row: any) => row.id === n.call_id),
      ).toHaveLength(1);
      const call = native.calls.find((row: any) => row.id === n.call_id);
      expect(call).toMatchObject({ bound: 1, settled: true });
      expect(n.path + (n.query ? "?" + n.query : "")).toBe(e.uri);
      expect(n.status).toBe(e.response!.status());
      expect(n).toMatchObject({
        fetch_returned: true,
        fetch_rejected: false,
        readers: 1,
        eof: true,
        release: 1,
        failed: false,
        cancels: 0,
        cancel_returns: 0,
      });
      expect(n.reads).toBeGreaterThan(0);
      expect(n.reads).toBe(n.read_returns);
      expect(n.length).toMatch(/^(0|[1-9][0-9]*)$/);
      expect(Number(n.length)).toBe(n.bytes);
      expect(n.digest).toMatch(/^[a-f0-9]{64}$/);
      return { n, call };
    }
    for (const e of auxiliary.filter((e) => !e.checked)) {
      const response = e.response!,
        headers = await response.allHeaders(),
        rid = headers["x-request-id"]!;
      expect(response.status()).toBe(200);
      const { n, call } = consumed(e, rid);
      expect(n.artifact).toBe(false);
      expect([
        "session.restore",
        "project.list",
        "project.resolve",
        "project.get",
      ]).toContain(call.method);
      expect(call).toMatchObject({
        fulfilled: true,
        rejected: false,
        current: true,
      });
      expect(n.wire_typed_digest).toMatch(/^[a-f0-9]{64}$/);
      expect(call.typed_digest).toBe(n.wire_typed_digest);
      expect(Number(headers["content-length"])).toBe(n.bytes);
      expect(headers["cache-control"]).toBe("no-store");
      if (e.finished) {
        expect(await response.finished()).toBeNull();
        // Auxiliary bodies can carry Session CSRF material. No second body
        // read or plaintext copy is needed: the original reader is witnessed.
      } else expect(e.failureKind).toBe("net::ERR_ABORTED");
      const wireProof = await wire.headers({
        method: "GET",
        url: e.request.url(),
        status: response.status(),
        requestID: rid,
      });
      auxiliaryProof.push({
        request_id: rid,
        url_sha256: digest(e.uri),
        status: response.status(),
        body_sha256: n.digest,
        typed_sha256: call.typed_digest,
        original_eof: true,
        reader_released: true,
        terminal: e.finished ? "finished" : "failed",
        failure_kind: e.failureKind,
        wire: wireProof,
      });
      e.checked = true;
    }
    for (const e of entries.filter((e) => !e.checked)) {
      const response = e.response!,
        headers = await response.allHeaders(),
        rid = headers["x-request-id"]!;
      const { n, call } = consumed(e, rid);
      expect(n.artifact).toBe(true);
      let body = n.artifact_value;
      if (e.finished) {
        expect(await response.finished()).toBeNull();
        const raw = await response.body();
        expect(raw.length).toBe(n.bytes);
        expect(digest(raw)).toBe(n.digest);
        body = JSON.parse(raw.toString("utf8"));
        expect(body).toEqual(n.artifact_value);
      } else expect(e.failureKind).toBe("net::ERR_ABORTED");
      expect(n).toBeDefined();
      expect(call).toBeDefined();
      expect(call.bound).toBe(1);
      expect(n.path + (n.query ? "?" + n.query : "")).toBe(e.uri);
      expect(n).toMatchObject({
        fetch_returned: true,
        fetch_rejected: false,
        readers: 1,
        eof: true,
        release: 1,
        failed: false,
      });
      expect(n.reads).toBe(n.read_returns);
      expect(n.cancels).toBe(n.cancel_returns);
      expect(Number(n.length)).toBe(n.bytes);
      expect(headers["cache-control"]).toBe("no-store");
      expect(Number(headers["content-length"])).toBe(n.bytes);
      const sidecar = JSON.parse(
        readFileSync(
          join(evidence, `artifact-owner-response-${rid}.json`),
          "utf8",
        ),
      );
      expect(sidecar).toMatchObject({
        input_hash: data.input_hash,
        request_id: rid,
        method: "GET",
        url_sha256: digest(e.uri),
        status: response.status(),
        content_length: n.bytes,
        body_sha256: n.digest,
        original_eof: true,
        original_closed: true,
        handler_returned: true,
        request_host_matches: true,
      });
      const wireProof = await wire.headers({
        method: "GET",
        url: e.request.url(),
        status: response.status(),
        requestID: rid,
      });
      const url = new URL(e.request.url()),
        query = Object.fromEntries(url.searchParams.entries());
      expect(Object.keys(query).length).toBe([...url.searchParams].length);
      let typed: any;
      if (response.status() === 404) {
        expect(body).toMatchObject({
          status: 404,
          code: "NOT_FOUND",
          instance: "/api/v1",
          commit_state: "not_committed",
          request_id: rid,
        });
        expect(call.rejected).toBe(true);
        typed = body;
      } else {
        expect(response.status()).toBe(200);
        expect(call.fulfilled).toBe(true);
        if (call.method === "list") {
          expect(query).toEqual(
            Object.fromEntries(
              Object.entries(call.query).map(([key, value]) => [
                key,
                String(value),
              ]),
            ),
          );
          const source = url.pathname.includes(data.other_project_id)
            ? [data.other_artifact]
            : data.rows;
          let expected = source.filter(
            (item: any) =>
              (!query.name_query || item.name.includes(query.name_query)) &&
              (!query.kind || item.kind === query.kind),
          );
          if (query.cursor) {
            const previous = entries
              .slice(0, entries.indexOf(e))
              .reverse()
              .find((item) => item.body?.next_cursor === query.cursor);
            expect(previous).toBeDefined();
            expect(previous!.body.items).toHaveLength(50);
            expected = expected.slice(50);
          }
          expect(body.items).toEqual(expected.slice(0, 50));
          if (expected.length > 50)
            expect(typeof body.next_cursor).toBe("string");
          else expect(body.next_cursor).toBeUndefined();
          expect(Object.keys(body).sort()).toEqual(
            body.next_cursor ? ["items", "next_cursor"] : ["items"],
          );
          typed = { ...body, next_cursor: body.next_cursor ?? null };
        } else {
          expect(query).toEqual({ offset: call.query.offset, limit: "8192" });
          const artifact = [...data.rows, data.other_artifact].find(
            (item: any) =>
              e.uri.startsWith(
                `/api/v1/projects/${item.artifact_ref.project_id}/artifacts/${item.artifact_ref.artifact_id}/files/${item.artifact_ref.file_id}/content?`,
              ),
          );
          expect(artifact).toBeDefined();
          expect(body.artifact).toEqual(artifact);
          if (
            artifact === data.text_artifact ||
            artifact.artifact_ref.artifact_id ===
              data.text_artifact.artifact_ref.artifact_id
          ) {
            const first = query.offset === "0";
            expect(["0", "8190"]).toContain(query.offset);
            expect(body).toMatchObject({
              projection: "text",
              text: first ? data.first_text : data.second_text,
              offset: first ? "0" : "8190",
              next_offset: first
                ? "8190"
                : String(Buffer.byteLength(data.text)),
              truncated: first,
            });
          } else if (
            artifact.artifact_ref.artifact_id ===
            data.image_artifact.artifact_ref.artifact_id
          )
            expect(body).toMatchObject({
              projection: "image_ref",
              offset: "0",
              next_offset: "0",
              truncated: false,
            });
          else if (
            artifact.artifact_ref.artifact_id ===
            data.file_artifact.artifact_ref.artifact_id
          )
            expect(body).toMatchObject({
              projection: "file_ref",
              offset: "0",
              next_offset: "0",
              truncated: false,
            });
          else
            expect(body).toMatchObject({
              projection: "text",
              text: "The other owned project remains isolated.\n",
              offset: "0",
              truncated: false,
            });
          expect(
            Object.keys(body).every((key) =>
              [
                "artifact",
                "projection",
                "text",
                "offset",
                "next_offset",
                "truncated",
                "invalid_utf8",
              ].includes(key),
            ),
          ).toBe(true);
          typed = {
            ...body,
            text: body.text ?? "",
            invalid_utf8: body.invalid_utf8 ?? false,
          };
        }
      }
      expect(call.typed_digest).toBe(digest(JSON.stringify(canonical(typed))));
      e.body = body;
      e.status = response.status();
      e.rid = rid;
      e.checked = true;
      proof.push({
        request_id: rid,
        url_sha256: digest(e.uri),
        status: e.status,
        body_sha256: n.digest,
        typed_sha256: call.typed_digest,
        original_eof: true,
        handler_returned: true,
        terminal: e.finished ? "finished" : "failed",
        failure_kind: e.failureKind,
        wire: wireProof,
      });
    }
  }
  return {
    stage(value: string) {
      stage = value;
    },
    verify,
    count() {
      return entries.length;
    },
    releaseLoading() {
      writeFileSync(
        join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "release-loading"),
        "release",
        { mode: 0o600 },
      );
    },
    last() {
      return entries.at(-1)?.body;
    },
    async complete() {
      await verify();
      expect(entries).toHaveLength(20);
      expect(entries.filter((e) => e.status === 404)).toHaveLength(1);
      expect(
        entries.filter((e) => e.status === 200 && e.uri.includes("/content?")),
      ).toHaveLength(9);
      expect(
        entries.filter((e) => e.status === 200 && !e.uri.includes("/content?")),
      ).toHaveLength(10);
      completed = true;
    },
    async retire() {
      try {
        if (retired) return;
        await verify();
        expect(handlers).toBe(0);
        const n = await page.evaluate(() =>
          (window as any).__artifactNative.finish(),
        );
        expect(n.pending).toBe(0);
        expect(n.retired).toBe(true);
        expect(n.failed).toBe(false);
        await page.unroute(entryURL, install);
        page.off("request", request);
        page.off("response", response);
        page.off("requestfinished", finished);
        page.off("requestfailed", failed);
        wire.complete();
        await wire.close();
        retired = true;
      } finally {
        // Snapshot only the original observer state while its document still
        // exists. This neither consumes a response nor issues a new request.
        try {
          nativeDiagnostic = await page.evaluate(() => {
            const owner = (window as any).__artifactNative;
            if (!owner) return { available: false, reason: "not_installed" };
            const n = owner.snapshot();
            const number = (value: unknown) =>
              typeof value === "number" && Number.isFinite(value)
                ? value
                : null;
            return {
              available: true,
              failed: n.failed === true,
              retired: n.retired === true,
              pending: number(n.pending),
              not_busy: n.not_busy === true,
              failure_reason:
                typeof n.failure_reason === "string" &&
                /^ARTIFACT_[A-Z_]+$/.test(n.failure_reason)
                  ? n.failure_reason
                  : "",
              calls: n.calls.slice(0, 64).map((c: any) => ({
                method: [
                  "list",
                  "read",
                  "session.restore",
                  "project.list",
                  "project.resolve",
                  "project.get",
                ].includes(c.method)
                  ? c.method
                  : "invalid",
                bound: number(c.bound),
                settled: c.settled === true,
                fulfilled: c.fulfilled === true,
                rejected: c.rejected === true,
                current: c.current === true,
                typed_digest_present: typeof c.typed_digest === "string",
              })),
              rows: n.rows.slice(0, 64).map((r: any) => ({
                status: number(r.status),
                request_id_present: typeof r.rid === "string",
                declared_length_present: typeof r.length === "string",
                fetch_returned: r.fetch_returned === true,
                fetch_rejected: r.fetch_rejected === true,
                readers: number(r.readers),
                reads: number(r.reads),
                read_returns: number(r.read_returns),
                eof: r.eof === true,
                release: number(r.release),
                cancels: number(r.cancels),
                cancel_returns: number(r.cancel_returns),
                bytes: number(r.bytes),
                digest_present: typeof r.digest === "string",
                artifact: r.artifact === true,
                wire_typed_digest_present:
                  typeof r.wire_typed_digest === "string",
                failed: r.failed === true,
              })),
            };
          });
        } catch {
          nativeDiagnostic = { available: false, reason: "snapshot_failed" };
        }
      }
    },
    save(status?: string, errors: readonly { stack?: string }[] = []) {
      writeFileSync(
        join(evidence, "artifact-owner-browser-proof.json"),
        JSON.stringify(
          {
            input_hash: data.input_hash,
            stage,
            status,
            completed,
            retired,
            failed: failure,
            first_failure_stage: failureStage,
            native: nativeDiagnostic,
            original_events: entries.map((e) => ({
              url_sha256: digest(e.uri),
              status: e.response?.status() ?? null,
              finished: e.finished,
              failed: e.failed,
              checked: e.checked,
              response_count: e.responseCount,
              finished_count: e.finishedCount,
              failed_count: e.failedCount,
              failure_kind: e.failureKind,
            })),
            auxiliary_events: {
              count: auxiliary.length,
              finished: auxiliary.filter((e) => e.finished).length,
              failed: auxiliary.filter((e) => e.failed).length,
              rows: auxiliary.slice(0, 64).map((e) => ({
                url_sha256: digest(e.uri),
                status: e.response?.status() ?? null,
                finished: e.finished,
                failed: e.failed,
                checked: e.checked,
                response_count: e.responseCount,
                finished_count: e.finishedCount,
                failed_count: e.failedCount,
                failure_kind: e.failureKind,
              })),
            },
            error_locations: errors.map(
              (error) =>
                error.stack?.match(
                  /artifact-owner\.(?:helpers|spec)\.ts:[0-9]+:[0-9]+/,
                )?.[0] ?? "unavailable",
            ),
            original_requests: entries.length,
            proof,
            auxiliary_proof: auxiliaryProof,
          },
          null,
          2,
        ),
        { mode: 0o600 },
      );
      if (completed && retired && status === "passed")
        writeFileSync(
          join(
            process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
            "artifact-owner-result.json",
          ),
          JSON.stringify({
            input_hash: data.input_hash,
            completed: true,
            original_requests: entries.length,
            typed_published: true,
            original_consumed: true,
            read_only: true,
            observers_retired: true,
          }),
          { mode: 0o600 },
        );
    },
  };
}
export async function artifactScreenshot(page: Page, name: string) {
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    )
    .toBe(true);
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
      process.env.AGENTEAM_ARTIFACT_OWNER_WEB_EVIDENCE!,
      `artifact-owner-${name}.png`,
    ),
    fullPage: false,
    animations: "disabled",
  });
}
