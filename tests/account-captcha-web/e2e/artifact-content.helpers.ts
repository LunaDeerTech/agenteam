import {
  expect,
  type Page,
  type Request,
  type Response,
  type Route,
} from "@playwright/test";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
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
export type ContentInput = {
  name: string;
  description?: string;
  media_type: string;
  content: string;
};
export type ContentMaterial = {
  mode: string;
  origin: string;
  cookie: string;
  project_id: string;
  other_project_id: string;
  user_id: string;
  route: string;
  inputs: ContentInput[];
};
export function contentMaterial(): ContentMaterial {
  const data: ContentMaterial = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "artifact-content-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("create-and-recover-original");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.inputs).toHaveLength(4);
  expect(data.route).toBe("/artifact-owner/artifact-content/artifacts");
  return data;
}
// Each wrapper preserves the original Promise, callback and return value.
// Only the original application reader consumes the original response stream.
function installContentNative(config: { origin: string; projects: string[] }) {
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
    | "start"
    | "checkOriginal"
    | "retryOriginal"
    | "cancelOriginal"
    | "session.restore"
    | "project.resolve"
    | "project.get";
  let originalProject = "";
  function consumer(method: Consumer) {
    const command = [
      "start",
      "checkOriginal",
      "retryOriginal",
      "cancelOriginal",
    ].includes(method);
    const artifact = command || method === "list" || method === "read";
    const target = command
      ? auth.artifactCreation
      : artifact
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
      if (command) {
        if (method === "start") {
          if (args.length !== 2 || !config.projects.includes(args[0]))
            throw Error("ARTIFACT_NATIVE_SCOPE");
          originalProject = args[0];
        } else if (args.length !== 0 || !originalProject)
          throw Error("ARTIFACT_NATIVE_SCOPE");
        path =
          `/api/v1/projects/${originalProject}/artifacts/` +
          (method === "start" || method === "retryOriginal"
            ? "create-content"
            : "creation-commands/" +
              (method === "checkOriginal" ? "lookup" : "cancel"));
      } else if (artifact) {
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
            args[0]?.project_name !== "artifact-content")
        )
          throw Error("ARTIFACT_NATIVE_SCOPE");
        path =
          "/api/v1/projects/" +
          (method === "project.get" ? args[0] : "resolve");
        if (method === "project.resolve") {
          const query = new URLSearchParams();
          query.set("username", args[0].username);
          query.set("project_name", args[0].project_name);
          expectedQuery = query.toString();
        }
      }
      const call: any = {
        id: calls.length + 1,
        method,
        path,
        artifact,
        command,
        expected_query: expectedQuery,
        query:
          artifact && !command ? JSON.parse(JSON.stringify(args[1])) : null,
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
  host.__contentNative = {
    bind(value: any) {
      if (auth || retired) throw Error("ARTIFACT_DUPLICATE_BIND");
      auth = value;
      consumer("list");
      consumer("read");
      consumer("session.restore");
      consumer("start");
      consumer("checkOriginal");
      consumer("retryOriginal");
      consumer("cancelOriginal");
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
          !["GET", "POST"].includes(method) ||
          (method === "GET" &&
            (init?.body != null ||
              headers.has("x-csrf-token") ||
              headers.has("idempotency-key"))) ||
          (method === "POST" &&
            (typeof init?.body !== "string" ||
              !headers.has("x-csrf-token") ||
              !headers.has("idempotency-key"))) ||
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
        if (call.command !== (method === "POST"))
          throw Error("ARTIFACT_NATIVE_SCOPE");
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
  responses: number;
  finished: number;
  failed: number;
  failure: "none" | "net::ERR_ABORTED" | "other";
  checked: boolean;
  artifact: boolean;
  uri: string;
  body?: any;
};
export async function observeContent(page: Page, data: ContentMaterial) {
  const evidence = process.env.AGENTEAM_ARTIFACT_CONTENT_WEB_EVIDENCE!;
  const entries: Entry[] = [],
    proof: any[] = [],
    receipts: any[] = [];
  const keys = new Map<string, string>(),
    originalBodies = new Map<string, string>();
  const binding = await knowledgeSessionBinding(
    resolve(process.cwd(), "../.."),
    process.env.AGENTEAM_ARTIFACT_CONTENT_WEB_DIST!,
  );
  const wire = await observeSystemRunnerWire(page, data.origin);
  let failed = false,
    completed = false,
    retired = false,
    stage = "entry",
    handlers = 0;
  let nativeDiagnostic: any = { available: false };
  const request = (request: Request) => {
    try {
      const url = new URL(request.url());
      expect(url.origin).toBe(data.origin);
      if (!url.pathname.startsWith("/api/")) return;
      expect(entries.length).toBeLessThan(64);
      const artifact = url.pathname.startsWith(
        `/api/v1/projects/${data.project_id}/artifacts`,
      );
      if (!artifact) expect(request.method()).toBe("GET");
      if (request.method() === "POST") {
        expect(artifact).toBe(true);
        expect(url.search).toBe("");
        expect([
          "create-content",
          "creation-commands/lookup",
          "creation-commands/cancel",
        ]).toContain(url.pathname.split("/artifacts/")[1]);
        const body = request.postDataJSON(),
          input = data.inputs.find((value) => value.name === body.name);
        expect(input).toBeDefined();
        expect(body).toEqual(input);
        const raw = request.postData()!,
          key = request.headers()["idempotency-key"];
        expect(key).toMatch(/^[0-9a-f-]{36}$/);
        expect(request.headers()["x-csrf-token"]).toMatch(
          /^[A-Za-z0-9_-]{43}$/,
        );
        if (keys.has(body.name)) {
          expect(key).toBe(keys.get(body.name));
          expect(raw).toBe(originalBodies.get(body.name));
        } else {
          expect(url.pathname.endsWith("/create-content")).toBe(true);
          expect([...keys.values()]).not.toContain(key);
          keys.set(body.name, key);
          originalBodies.set(body.name, raw);
        }
      } else {
        expect(request.method()).toBe("GET");
        expect(request.postData()).toBeNull();
        expect(request.headers()["idempotency-key"]).toBeUndefined();
      }
      entries.push({
        request,
        responses: 0,
        finished: 0,
        failed: 0,
        failure: "none",
        checked: false,
        artifact,
        uri: url.pathname + url.search,
      });
    } catch {
      failed = true;
    }
  };
  const response = (response: Response) => {
    const e = entries.find((e) => e.request === response.request());
    if (e) {
      e.response = response;
      if (++e.responses !== 1) failed = true;
    }
  };
  const finished = (request: Request) => {
    const e = entries.find((e) => e.request === request);
    if (e) {
      if (++e.finished !== 1 || e.failed) failed = true;
    }
  };
  const requestFailed = (request: Request) => {
    const e = entries.find((e) => e.request === request);
    if (e) {
      e.failed++;
      e.failure =
        request.failure()?.errorText === "net::ERR_ABORTED"
          ? "net::ERR_ABORTED"
          : "other";
      if (e.failed !== 1 || e.finished || e.failure !== "net::ERR_ABORTED")
        failed = true;
    } else if (new URL(request.url()).pathname.startsWith("/api/"))
      failed = true;
  };
  page.on("request", request);
  page.on("response", response);
  page.on("requestfinished", finished);
  page.on("requestfailed", requestFailed);
  await page.addInitScript(installContentNative, {
    origin: data.origin,
    projects: [data.project_id, data.other_project_id],
  });
  const entryURL = new URL(binding.entry, data.origin).href;
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(async (binding) => {
        const mod = await import(/* @vite-ignore */ binding.asset),
          use = mod[binding.exportName];
        if (typeof use !== "function" || use() !== use())
          throw Error("CONTENT_SINGLETON");
        (window as any).__contentNative.bind(use());
      }, binding);
      await route.continue();
    } catch {
      failed = true;
      await route.abort();
    } finally {
      handlers--;
    }
  };
  await page.route(entryURL, install);
  function metadata(value: any, input: ContentInput) {
    expect(value.artifact_ref.project_id).toBe(data.project_id);
    for (const id of [
      value.artifact_ref.artifact_id,
      value.artifact_ref.file_id,
    ])
      expect(id).toMatch(/^[0-9a-f-]{36}$/);
    expect(value).toMatchObject({
      name: input.name,
      kind: "generated",
      created_by_user_id: data.user_id,
      version: "1",
    });
    expect(value.description ?? "").toBe(input.description ?? "");
    expect(value.created_at).toMatch(
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/,
    );
    expect(value.object).toEqual({
      media_type: input.media_type,
      byte_size: String(Buffer.byteLength(input.content)),
      sha256: "sha256:" + digest(input.content),
    });
    expect(
      Object.keys(value).every((k) =>
        [
          "artifact_ref",
          "name",
          "description",
          "kind",
          "created_by_user_id",
          "created_at",
          "version",
          "object",
        ].includes(k),
      ),
    ).toBe(true);
  }
  async function verify(expected: number) {
    expect(failed).toBe(false);
    await expect
      .poll(async () => {
        const n = await page.evaluate(() =>
          (window as any).__contentNative.snapshot(),
        );
        return (
          n.pending === 0 &&
          n.not_busy &&
          n.calls.every((c: any) => c.settled) &&
          n.rows.length === entries.length &&
          entries.every((e) => e.responses === 1 && e.finished + e.failed === 1)
        );
      })
      .toBe(true);
    const native = await page.evaluate(() =>
      (window as any).__contentNative.snapshot(),
    );
    expect(native.failed).toBe(false);
    expect(native.retired).toBe(false);
    expect(failed).toBe(false);
    expect(entries.filter((e) => e.artifact)).toHaveLength(expected);
    for (const e of entries.filter((e) => !e.checked)) {
      const r = e.response!,
        headers = await r.allHeaders(),
        rid = headers["x-request-id"];
      expect(native.rows.filter((n: any) => n.rid === rid)).toHaveLength(1);
      const n = native.rows.find((n: any) => n.rid === rid),
        call = native.calls.find((c: any) => c.id === n.call_id);
      expect(native.calls.filter((c: any) => c.id === n.call_id)).toHaveLength(
        1,
      );
      expect(call).toMatchObject({ bound: 1, settled: true });
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
      expect(n.path + (n.query ? "?" + n.query : "")).toBe(e.uri);
      expect(n.status).toBe(r.status());
      expect(n.bytes).toBe(Number(n.length));
      expect(n.bytes).toBe(Number(headers["content-length"]));
      expect(headers["cache-control"]).toBe("no-store");
      expect(n.digest).toMatch(/^[a-f0-9]{64}$/);
      if (e.finished) expect(await r.finished()).toBeNull();
      else expect(e.failure).toBe("net::ERR_ABORTED");
      const wireProof = await wire.headers({
        method: e.request.method(),
        url: e.request.url(),
        status: r.status(),
        requestID: rid,
      });
      let typed: any;
      if (!e.artifact) {
        expect(r.status()).toBe(200);
        expect(call).toMatchObject({
          fulfilled: true,
          rejected: false,
          current: true,
        });
        expect(["session.restore", "project.resolve", "project.get"]).toContain(
          call.method,
        );
        expect(call.typed_digest).toBe(n.wire_typed_digest);
        expect(n.wire_typed_digest).toMatch(/^[a-f0-9]{64}$/);
      } else {
        let body = n.artifact_value;
        if (e.finished) {
          const raw = await r.body();
          expect(raw.length).toBe(n.bytes);
          expect(digest(raw)).toBe(n.digest);
          expect(JSON.parse(raw.toString("utf8"))).toEqual(body);
        }
        const sidecar = JSON.parse(
          readFileSync(
            join(evidence, `artifact-content-response-${rid}.json`),
            "utf8",
          ),
        );
        expect(sidecar).toEqual({
          request_id: rid,
          method: e.request.method(),
          url_sha256: digest(e.uri),
          status: r.status(),
          content_length: n.bytes,
          body_sha256: n.digest,
          original_eof: true,
          original_closed: true,
          handler_returned: true,
          request_host_matches: true,
        });
        const url = new URL(e.request.url()),
          query = Object.fromEntries(url.searchParams);
        if (r.status() === 503) {
          const input = e.request.postDataJSON();
          expect(e.uri.endsWith("/create-content")).toBe(true);
          const pending = input.name === data.inputs[3].name;
          expect(body).toMatchObject({
            status: 503,
            request_id: rid,
            code: pending ? "DEPENDENCY_UNAVAILABLE" : "COMMIT_UNKNOWN",
            commit_state: pending ? "not_committed" : "unknown",
          });
          if (!pending) expect(body.retry_hint).toBe("lookup");
          expect(call).toMatchObject({ rejected: true, fulfilled: false });
          typed = body;
        } else {
          expect(r.status()).toBe(200);
          expect(call).toMatchObject({ fulfilled: true, rejected: false });
          if (e.request.method() === "POST") {
            const input = e.request.postDataJSON() as ContentInput;
            if (body.artifact) {
              metadata(body.artifact, input);
              if (body.state) expect(body.state).toBe("committed");
              expect(
                receipts.some(
                  (m) =>
                    m.artifact_ref.artifact_id ===
                    body.artifact.artifact_ref.artifact_id,
                ),
              ).toBe(false);
              receipts.push(body.artifact);
            } else if (e.uri.endsWith("/lookup")) {
              expect(body).toEqual({
                state:
                  input.name === data.inputs[2].name
                    ? "not_observed"
                    : "pending",
                artifact: null,
              });
            } else
              expect(body).toEqual({
                state: "cancelled",
                cleanup: "completed",
              });
            typed = body;
          } else if (call.method === "list") {
            expect(query).toEqual({ limit: "50" });
            expect(body).toEqual({ items: [...receipts].reverse() });
            typed = { ...body, next_cursor: null };
          } else {
            expect(query).toEqual({ offset: "0", limit: "8192" });
            const m = receipts.at(-1),
              input = data.inputs.find((v) => v.name === m.name)!;
            expect(e.uri.split("?")[0]).toBe(
              `/api/v1/projects/${data.project_id}/artifacts/${m.artifact_ref.artifact_id}/files/${m.artifact_ref.file_id}/content`,
            );
            expect(body).toMatchObject({
              artifact: m,
              projection: "text",
              text: input.content,
              offset: "0",
              next_offset: String(Buffer.byteLength(input.content)),
              truncated: false,
            });
            expect(body.invalid_utf8 ?? false).toBe(false);
            typed = {
              ...body,
              text: body.text ?? "",
              invalid_utf8: body.invalid_utf8 ?? false,
            };
          }
        }
        expect(call.typed_digest).toBe(
          digest(JSON.stringify(canonical(typed))),
        );
        e.body = body;
      }
      proof.push({
        request_id: rid,
        artifact: e.artifact,
        method: e.request.method(),
        url_sha256: digest(e.uri),
        status: r.status(),
        body_sha256: n.digest,
        typed_sha256: call.typed_digest,
        terminal: e.finished ? "finished" : "failed",
        failure: e.failure,
        original_eof: true,
        reader_released: true,
        wire: wireProof,
      });
      e.checked = true;
    }
  }
  return {
    stage(value: string) {
      stage = value;
    },
    verify,
    last() {
      return entries.filter((e) => e.artifact).at(-1)?.body;
    },
    async reached() {
      const marker = join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "reached-content",
      );
      await expect
        .poll(
          () => existsSync(marker) && readFileSync(marker, "utf8") === "held",
        )
        .toBe(true);
    },
    release() {
      writeFileSync(
        join(process.env.AGENTEAM_AUTH_WEB_PRIVATE!, "release-content"),
        "release",
        { mode: 0o600 },
      );
    },
    async complete() {
      await verify(16);
      expect(receipts).toHaveLength(3);
      expect(keys.size).toBe(4);
      expect(
        entries.filter((e) => e.artifact && e.request.method() === "POST"),
      ).toHaveLength(9);
      expect(
        entries.filter((e) => e.artifact && e.uri.endsWith("/cancel")),
      ).toHaveLength(1);
      await wire.complete();
      completed = true;
      stage = "complete";
    },
    async retire() {
      try {
        // Restoration preserves the original technical tail; a pending tail is
        // a failed test, never a manufactured completion or an extra wait.
        const n = await page.evaluate(() =>
          (window as any).__contentNative.finish(),
        );
        expect(n.pending).toBe(0);
        expect(n.failed).toBe(false);
        expect(n.retired).toBe(true);
      } finally {
        try {
          nativeDiagnostic = await page.evaluate(() => {
            const n = (window as any).__contentNative?.snapshot();
            return n
              ? {
                  available: true,
                  failed: n.failed,
                  retired: n.retired,
                  pending: n.pending,
                  not_busy: n.not_busy,
                  calls: n.calls.map((c: any) => ({
                    method: c.method,
                    bound: c.bound,
                    settled: c.settled,
                    fulfilled: c.fulfilled,
                    rejected: c.rejected,
                  })),
                  rows: n.rows.map((r: any) => ({
                    status: r.status,
                    eof: r.eof,
                    release: r.release,
                    bytes: r.bytes,
                    reads: r.reads,
                    read_returns: r.read_returns,
                    cancels: r.cancels,
                    failed: r.failed,
                    digest_present: typeof r.digest === "string",
                  })),
                }
              : { available: false };
          });
        } catch {
          nativeDiagnostic = { available: false };
        }
        try {
          await wire.close();
        } finally {
          await page.unroute(entryURL, install);
          page.off("request", request);
          page.off("response", response);
          page.off("requestfinished", finished);
          page.off("requestfailed", requestFailed);
          expect(handlers).toBe(0);
          retired = true;
        }
      }
    },
    save(status?: string, errors: readonly { stack?: string }[] = []) {
      writeFileSync(
        join(evidence, "artifact-content-browser-proof.json"),
        JSON.stringify(
          {
            stage,
            status,
            completed,
            retired,
            failed,
            native: nativeDiagnostic,
            original_requests: entries.filter((e) => e.artifact).length,
            events: entries.map((e) => ({
              url_sha256: digest(e.uri),
              status: e.response?.status(),
              responses: e.responses,
              finished: e.finished,
              failed: e.failed,
              failure: e.failure,
              checked: e.checked,
            })),
            proof,
            error_locations: errors.map(
              (e) =>
                e.stack?.match(
                  /artifact-content\.(?:helpers|spec)\.ts:[0-9]+:[0-9]+/,
                )?.[0] ?? "unavailable",
            ),
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
            "artifact-content-result.json",
          ),
          JSON.stringify({
            completed: true,
            original_requests: 16,
            observers_retired: true,
          }),
          { mode: 0o600 },
        );
    },
  };
}
export async function contentScreenshot(page: Page, name: string) {
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({
    path: join(
      process.env.AGENTEAM_ARTIFACT_CONTENT_WEB_EVIDENCE!,
      `artifact-content-${name}.png`,
    ),
    animations: "disabled",
  });
}
