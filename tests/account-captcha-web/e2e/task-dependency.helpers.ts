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
import { observeSystemRunnerWire } from "./system-runner-management.helpers";
import { knowledgeSessionBinding } from "./knowledge-owner-read.native";
const uuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const hash = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");
export type DependencyMaterial = {
  mode: "dependency-current-read";
  origin: string;
  cookie: string;
  project_id: string;
  other_project_id: string;
  initial: Record<string, any>;
  related: Record<string, any>;
  foreign: Record<string, any>;
  rows: Record<string, any>[];
  sprints: Record<string, any>[];
  milestone: Record<string, any>;
  route: string;
  input_hash: string;
  description: string;
  cycle_description: string;
  resolution: string;
};
export function dependencyMaterial(): DependencyMaterial {
  const data: DependencyMaterial = JSON.parse(
    readFileSync(
      join(
        process.env.AGENTEAM_AUTH_WEB_PRIVATE!,
        "task-dependency-material.json",
      ),
      "utf8",
    ),
  );
  expect(data.mode).toBe("dependency-current-read");
  expect(data.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN);
  expect(data.input_hash).toBe(
    process.env.AGENTEAM_TASK_DEPENDENCY_WEB_INPUT_HASH,
  );
  expect(data.input_hash).toMatch(/^[a-f0-9]{64}$/);
  for (const task of [data.initial, data.related, data.foreign]) {
    expect(task.id).toMatch(uuid);
    expect(task.version).toBe("1");
    expect(task.state).toBe("backlog");
    expect(task.assignee_agent_id).toBeNull();
  }
  expect(data.initial.project_id).toBe(data.project_id);
  expect(data.related.project_id).toBe(data.project_id);
  expect(data.foreign.project_id).toBe(data.other_project_id);
  expect(data.project_id).not.toBe(data.other_project_id);
  expect(data.initial.sprint_id).not.toBe(data.related.sprint_id);
  expect(data.rows).toHaveLength(53);
  expect(new Set(data.rows.map((t) => t.id)).size).toBe(53);
  expect(data.rows[52]).toEqual(data.related);
  expect(data.sprints[0]!.state).toBe("current");
  expect(data.sprints[1]!.state).toBe("planned");
  expect(data.route).toMatch(
    new RegExp(`^/[^/?#]+/[^/?#]+/tasks/${data.initial.id}$`),
  );
  return data;
}
// Observe the original public Session methods and application reader only.
// No clone, tee, synthetic fetch, response rewrite or alternate business API.
function installDependencyNative(config: {
  origin: string;
  project: string;
  source: string;
  related: string;
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
    bad: (error: any) => any = () => {
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
      throw Error("DEPENDENCY_NATIVE_DESCRIPTOR");
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
        throw Error("DEPENDENCY_NATIVE_CALL_BOUND");
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
      call.method =
        kind === "dependency.add" || kind === "dependency.resolve"
          ? "POST"
          : "GET";
      if (kind.startsWith("dependency.")) {
        if (args[0] !== config.project)
          throw Error("DEPENDENCY_NATIVE_PROJECT");
        if (kind === "dependency.candidates") {
          if (args.length !== 2) throw Error("DEPENDENCY_NATIVE_QUERY_ARITY");
          call.query = JSON.parse(JSON.stringify(args[1]));
        } else {
          if (
            ![config.source, config.related].includes(args[1]) ||
            args.length !== (kind === "dependency.add" ? 4 : 5)
          )
            throw Error("DEPENDENCY_NATIVE_WRITE_SCOPE");
          const taskBefore = args[kind === "dependency.add" ? 3 : 4];
          if (
            !taskBefore ||
            taskBefore.project_id !== config.project ||
            taskBefore.id !== args[1] ||
            taskBefore.version !== args[2]?.expected_version
          )
            throw Error("DEPENDENCY_NATIVE_ORIGINAL_TASK");
          call.taskBefore = JSON.parse(JSON.stringify(taskBefore));
          call.original = JSON.parse(JSON.stringify(args[2]));
          call.task = args[1];
          if (kind === "dependency.resolve" && args[3] !== config.related)
            throw Error("DEPENDENCY_NATIVE_RESOLVE_TARGET");
        }
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
            throw Error("DEPENDENCY_NATIVE_RESTORE_IDENTITY");
          if (call.method === "POST") {
            const p = auth.workTaskDependencies.progress;
            call.result_current =
              p?.phase === "confirmed" &&
              p.kind ===
                (kind === "dependency.add"
                  ? "dependency-add"
                  : "dependency-resolve") &&
              p.projectID === config.project &&
              p.taskID === call.task &&
              p.receipt === value &&
              JSON.stringify(canonical(value.task)) ===
                JSON.stringify(
                  canonical({
                    ...call.taskBefore,
                    version: String(BigInt(call.taskBefore.version) + 1n),
                    updated_at: value.task.updated_at,
                    manual_rank: value.task.manual_rank,
                  }),
                );
          }
          call.typed = JSON.parse(JSON.stringify(value));
          call.typed_digest = await typedDigest(value);
        },
        async (error) => {
          call.rejected = call.settled = true;
          call.current = current() && before === identity();
          call.identity = identity();
          call.not_busy = auth.state.busy === false;
          const p = auth.workTaskDependencies.progress;
          call.result_current =
            p?.phase === "rejected" &&
            p.kind === "dependency-add" &&
            p.projectID === config.project &&
            p.taskID === config.related;
          if (
            kind !== "dependency.add" ||
            call.task !== config.related ||
            error?.problem?.code !== "TASK_DEPENDENCY_CYCLE" ||
            error.problem.commit_state !== "not_committed" ||
            error.problem.status !== 409
          )
            throw Error("DEPENDENCY_NATIVE_UNEXPECTED_REJECTION");
          call.typed = JSON.parse(JSON.stringify(error.problem));
          call.typed_digest = await typedDigest(error.problem);
        },
      );
      return result;
    });
  }
  host.__taskDependencyNative = {
    bind(value: any) {
      if (auth || retired) throw Error("DEPENDENCY_NATIVE_DUPLICATE_BIND");
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
      const prefix = "/api/v1/projects/" + config.project;
      for (const method of [
        "milestones",
        "milestone",
        "sprints",
        "sprint",
        "tasks",
        "task",
        "blockers",
        "events",
        "agents",
        "agent",
      ]) {
        consumer(auth.workReview, method, "work." + method, (a) => {
          if (a[0] !== config.project)
            throw Error("DEPENDENCY_NATIVE_READ_SCOPE");
          if (["milestone", "sprint", "task", "agent"].includes(method))
            return prefix + "/" + method + "s/" + a[1];
          if (["events", "blockers"].includes(method))
            return prefix + "/tasks/" + a[1] + "/" + method;
          return prefix + "/" + method;
        });
      }
      consumer(
        auth.workTaskDependencies,
        "candidates",
        "dependency.candidates",
        () => prefix + "/tasks",
      );
      for (const method of ["add", "resolve"])
        consumer(
          auth.workTaskDependencies,
          method,
          "dependency." + method,
          (a) =>
            prefix +
            "/tasks/" +
            a[1] +
            "/blockers" +
            (method === "resolve" ? "/resolve" : ""),
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
        if (tails.size) throw Error("DEPENDENCY_NATIVE_ORIGINAL_TAIL_PENDING");
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
          !["GET", "POST"].includes(method) ||
          (method === "GET" &&
            (init?.body != null ||
              headers.has("idempotency-key") ||
              headers.has("x-csrf-token")))
        )
          throw Error("DEPENDENCY_NATIVE_READ_ONLY");
        const matches = calls.filter(
          (call) => !call.bound && call.path === url.pathname && !call.settled,
        );
        if (matches.length !== 1)
          throw Error("DEPENDENCY_NATIVE_ORIGINAL_CALL_IDENTITY");
        const call = matches[0];
        if (call.method !== method) throw Error("DEPENDENCY_NATIVE_METHOD");
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
          const actual = [...url.searchParams.entries()].sort(),
            expected = Object.entries(call.query)
              .map(([k, v]) => [k, String(v)])
              .sort();
          if (JSON.stringify(actual) !== JSON.stringify(expected))
            throw Error("DEPENDENCY_NATIVE_QUERY_DIFFERS");
          call.query_bound = row.query_bound = true;
        }
        if (method === "POST") {
          if (
            typeof init?.body !== "string" ||
            !headers.get("idempotency-key") ||
            !headers.get("x-csrf-token")
          )
            throw Error("DEPENDENCY_NATIVE_WRITE_ENVELOPE");
          const body = JSON.parse(init.body),
            original = call.original;
          let expected;
          if (call.kind === "dependency.add") {
            if (!/^[0-9a-f-]{36}$/.test(body.request?.blocker_id))
              throw Error("DEPENDENCY_NATIVE_BLOCKER_ID");
            expected = {
              ...original,
              request: {
                blocker_id: body.request.blocker_id,
                ...original.request,
              },
            };
          } else expected = original;
          if (
            JSON.stringify(canonical(body)) !==
            JSON.stringify(canonical(expected))
          )
            throw Error("DEPENDENCY_NATIVE_ORIGINAL_INPUT");
          call.request_bound = row.request_bound = true;
          track(digest(new TextEncoder().encode(init.body)), (value) => {
            row.request_digest = value;
            call.request_digest = value;
          });
          track(
            digest(new TextEncoder().encode(headers.get("idempotency-key")!)),
            (value) => {
              row.key_digest = value;
              call.key_digest = value;
            },
          );
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
            if (!response.body) throw Error("DEPENDENCY_NATIVE_BODY_MISSING");
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
                throw Error("DEPENDENCY_NATIVE_BODY_BOUND");
              row.chunks.push(part.value.slice());
              return;
            }
            if (row.eof) throw Error("DEPENDENCY_NATIVE_DUPLICATE_EOF");
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
              const call = calls.find((c) => c.id === row.call_id);
              if (row.status === 200) {
                const blocker = (item: any) => {
                  const { metadata, created_by, resolved_by, ...rest } = item;
                  return {
                    ...rest,
                    related_task_id:
                      item.type === "rely_on" ? metadata.related_task_id : null,
                  };
                };
                if (call.kind === "work.sprint") {
                  const { started_by, completed_by, ...rest } = value;
                  value = rest;
                }
                if (call.kind === "work.blockers")
                  value = { ...value, items: value.items.map(blocker) };
                if (
                  ["dependency.add", "dependency.resolve"].includes(call.kind)
                )
                  value = { ...value, blocker: blocker(value.blocker) };
              }
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
  write?: number;
};
export async function observeDependency(page: Page, data: DependencyMaterial) {
  const evidence = process.env.AGENTEAM_TASK_DEPENDENCY_WEB_EVIDENCE!,
    entries: Entry[] = [],
    proofs: any[] = [],
    keys = new Set<string>();
  const prefix = `/api/v1/projects/${data.project_id}`,
    binding = await knowledgeSessionBinding(
      resolve(process.cwd(), "../.."),
      process.env.AGENTEAM_TASK_DEPENDENCY_WEB_DIST!,
    );
  if (binding.entry === binding.asset)
    throw Error("DEPENDENCY_ENTRY_IS_SINGLETON");
  let failure: Error | undefined,
    writes = 0,
    blockerID = "",
    rejectedID = "",
    retired = false,
    completed = false,
    handlers = 0;
  const wire = await observeSystemRunnerWire(page, data.origin);
  const captureError = (fn: () => void) => {
    try {
      fn();
    } catch (error) {
      failure ??=
        error instanceof Error ? error : Error("DEPENDENCY_OBSERVER_FAILURE");
    }
  };
  function query(url: URL, values: Record<string, string>) {
    expect([...url.searchParams.entries()].sort()).toEqual(
      Object.entries(values).sort(),
    );
  }
  function classify(
    request: Request,
  ): { name: string; write?: number } | undefined {
    const url = new URL(request.url()),
      path = url.pathname,
      method = request.method();
    if (!path.startsWith("/api/")) return;
    expect(url.origin).toBe(data.origin);
    if (method === "POST") {
      expect(writes).toBeLessThan(3);
      query(url, {});
      const key = request.headers()["idempotency-key"]!;
      expect(key).toMatch(/^[A-Za-z0-9._:/-]{1,128}$/);
      expect(keys.has(key)).toBe(false);
      const body = request.postDataJSON(),
        write = writes;
      if (write < 2) {
        const target = write === 0 ? data.initial : data.related,
          related = write === 0 ? data.related : data.initial;
        expect(path).toBe(`${prefix}/tasks/${target.id}/blockers`);
        expect(body.request.blocker_id).toMatch(uuid);
        if (write === 0) blockerID = body.request.blocker_id;
        else {
          rejectedID = body.request.blocker_id;
          expect(rejectedID).not.toBe(blockerID);
        }
        expect(body).toEqual({
          expected_version: "1",
          request: {
            blocker_id: body.request.blocker_id,
            type: "rely_on",
            description:
              write === 0 ? data.description : data.cycle_description,
            metadata: { related_task_id: related.id },
          },
        });
      } else {
        expect(path).toBe(
          `${prefix}/tasks/${data.initial.id}/blockers/resolve`,
        );
        expect(body).toEqual({
          expected_version: "2",
          request: {
            blocker_id: blockerID,
            resolution_comment: data.resolution,
          },
        });
      }
      keys.add(key);
      writes++;
      return {
        name: write === 0 ? "add" : write === 1 ? "cycle" : "resolve-blocker",
        write,
      };
    }
    expect(method).toBe("GET");
    expect(request.postDataBuffer()).toBeNull();
    expect(request.headers()["idempotency-key"]).toBeUndefined();
    const globals: Record<string, string> = {
      "/api/v1/session": "session",
      "/api/v1/projects/resolve": "resolve",
      "/api/v1/projects": "projects",
      [prefix]: "project",
    };
    if (globals[path]) return { name: globals[path]! };
    if (!path.startsWith(prefix + "/"))
      throw Error("DEPENDENCY_FOREIGN_HTTP_SCOPE");
    const short = path.slice(prefix.length);
    if (["/milestones", "/agents"].includes(short)) {
      query(url, { limit: "50" });
      return { name: short.slice(1) };
    }
    if (short === `/milestones/${data.milestone.id}`) {
      query(url, {});
      return { name: "milestone" };
    }
    if (short === "/sprints") {
      query(url, { limit: "50", milestone_id: data.milestone.id });
      return { name: "sprints" };
    }
    if (data.sprints.some((s) => short === `/sprints/${s.id}`)) {
      query(url, {});
      return { name: "sprint" };
    }
    if (short === "/tasks") {
      if (url.searchParams.has("sprint_id")) {
        const sprint = url.searchParams.get("sprint_id")!,
          state = url.searchParams.get("state")!;
        expect([data.initial.sprint_id, data.related.sprint_id]).toContain(
          sprint,
        );
        expect([
          "backlog",
          "todo",
          "in_progress",
          "in_review",
          "blocked",
          "done",
          "cancelled",
        ]).toContain(state);
        query(url, { limit: "50", sprint_id: sprint, state });
        return { name: "tasks" };
      }
      const expected: Record<string, string> = { limit: "50" };
      if (url.searchParams.has("cursor")) {
        expect(url.searchParams.get("cursor")).not.toBe("");
        expected.cursor = url.searchParams.get("cursor")!;
      }
      if (url.searchParams.has("text")) {
        expect(url.searchParams.get("text")).toBe(data.initial.title);
        expected.text = data.initial.title;
      }
      query(url, expected);
      return { name: "candidates" };
    }
    for (const task of [data.initial, data.related]) {
      if (short === `/tasks/${task.id}`) {
        query(url, {});
        return { name: "task" };
      }
      if (short === `/tasks/${task.id}/blockers`) {
        query(url, { limit: "50", status: "all" });
        return { name: "blockers" };
      }
      if (short === `/tasks/${task.id}/events`) {
        query(url, { limit: "50", order: "desc" });
        return { name: "events" };
      }
    }
    throw Error("DEPENDENCY_UNDECLARED_ENDPOINT");
  }
  function onRequest(request: Request) {
    captureError(() => {
      const kind = classify(request);
      if (!kind) return;
      if (retired || entries.length >= 128)
        throw Error("DEPENDENCY_REQUEST_BOUND");
      entries.push({
        seq: entries.length + 1,
        request,
        ...kind,
        finished: 0,
        failed: 0,
        responses: 0,
        checked: false,
      });
    });
  }
  const entryFor = (request: Request) =>
    entries.find((e) => e.request === request);
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
      if (row.terminal) throw Error("DEPENDENCY_DUPLICATE_TERMINAL");
      row.terminal = "finished";
      row.finished++;
    });
  const onFailed = (request: Request) =>
    captureError(() => {
      const row = entryFor(request);
      if (!row) return;
      if (row.terminal) throw Error("DEPENDENCY_DUPLICATE_TERMINAL");
      row.terminal = "failed";
      row.failed++;
      row.failure = request.failure()?.errorText;
    });
  const onClose = () => {
    if (!retired) failure ??= Error("DEPENDENCY_PAGE_CLOSED_EARLY");
  };
  page.on("request", onRequest);
  page.on("response", onResponse);
  page.on("requestfinished", onFinished);
  page.on("requestfailed", onFailed);
  page.on("close", onClose);
  await page.addInitScript(installDependencyNative, {
    origin: data.origin,
    project: data.project_id,
    source: data.initial.id,
    related: data.related.id,
  });
  const entryURL = new URL(binding.entry, data.origin).href;
  const install = async (route: Route) => {
    handlers++;
    try {
      await page.evaluate(async (binding) => {
        const module = await import(/* @vite-ignore */ binding.asset),
          use = module[binding.exportName];
        if (typeof use !== "function")
          throw Error("DEPENDENCY_SINGLETON_EXPORT");
        const auth = use();
        if (use() !== auth) throw Error("DEPENDENCY_SINGLETON_IDENTITY");
        (window as any).__taskDependencyNative.bind(auth);
      }, binding);
      await route.continue();
    } catch (error) {
      failure ??=
        error instanceof Error ? error : Error("DEPENDENCY_BIND_FAILURE");
      await route.abort();
    } finally {
      handlers--;
    }
  };
  await page.route(entryURL, install);
  function projected(body: any, kind: string, status: number) {
    if (status !== 200) return body;
    if (kind === "session") return { user: body.user, session: body.session };
    if (kind === "sprint") {
      const { started_by, completed_by, ...rest } = body;
      return rest;
    }
    const blocker = (item: any) => {
      const { metadata, created_by, resolved_by, ...rest } = item;
      return {
        ...rest,
        related_task_id:
          item.type === "rely_on" ? metadata.related_task_id : null,
      };
    };
    if (kind === "blockers") return { ...body, items: body.items.map(blocker) };
    if (["add", "resolve-blocker"].includes(kind))
      return { ...body, blocker: blocker(body.blocker) };
    return body;
  }
  async function verify() {
    if (failure) throw failure;
    await expect
      .poll(() => entries.filter((e) => !e.terminal).length, { timeout: 5000 })
      .toBe(0);
    await expect
      .poll(
        () =>
          page.evaluate(
            () => (window as any).__taskDependencyNative.snapshot().pending,
          ),
        { timeout: 5000 },
      )
      .toBe(0);
    if (failure) throw failure;
    const original = await page.evaluate(() =>
      (window as any).__taskDependencyNative.snapshot(),
    );
    expect(original.failed).toBe(false);
    expect(original.current && original.not_busy).toBe(true);
    expect(page.isClosed()).toBe(false);
    expect(original.rows).toHaveLength(entries.length);
    expect(original.calls).toHaveLength(entries.length);
    expect(new Set(original.rows.map((n: any) => n.request_id)).size).toBe(
      entries.length,
    );
    for (const e of entries.filter((e) => !e.checked)) {
      const status = e.name === "cycle" ? 409 : 200,
        method = e.request.method(),
        url = new URL(e.request.url());
      expect(e.responses).toBe(1);
      expect(e.status).toBe(status);
      expect(e.rid).toMatch(uuid);
      const matches = original.rows.filter((n: any) => n.request_id === e.rid);
      expect(matches).toHaveLength(1);
      const n = matches[0];
      const calls = original.calls.filter((c: any) => c.id === n.call_id);
      expect(calls).toHaveLength(1);
      const c = calls[0];
      const go = JSON.parse(
        readFileSync(
          join(evidence, `task-dependency-${data.mode}-response-${e.rid}.json`),
          "utf8",
        ),
      );
      expect(go.input_hash).toBe(data.input_hash);
      expect(go.request_id).toBe(e.rid);
      expect(go.method).toBe(method);
      expect(go.status).toBe(status);
      expect(go.url_sha256).toBe(hash(url.pathname + url.search));
      expect(go.original_eof && go.original_closed && go.handler_returned).toBe(
        true,
      );
      expect(go.handler_return_source).toBe("app_http_request_log");
      expect(n.method).toBe(method);
      expect(n.path).toBe(url.pathname);
      expect(n.url_digest).toBe(go.url_sha256);
      expect(n.status).toBe(status);
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
      expect(c.method).toBe(method);
      expect(c.path).toBe(n.path);
      expect(c.settled && c.current && c.not_busy).toBe(true);
      expect(c.fulfilled).toBe(status === 200);
      expect(c.rejected).toBe(status === 409);
      expect(c.typed_digest).toMatch(/^[a-f0-9]{64}$/);
      expect(n.typed_digest).toBe(c.typed_digest);
      expect(c.kind).toBe(
        (
          {
            session: "restore",
            resolve: "project.resolve",
            projects: "project.list",
            project: "project.get",
            candidates: "dependency.candidates",
            add: "dependency.add",
            cycle: "dependency.add",
            "resolve-blocker": "dependency.resolve",
          } as Record<string, string>
        )[e.name] ?? "work." + e.name,
      );
      if (e.name === "candidates")
        expect(c.query_bound && n.query_bound).toBe(true);
      if (e.write !== undefined) {
        expect(c.result_current && c.request_bound && n.request_bound).toBe(
          true,
        );
        expect(c.request_digest).toBe(n.request_digest);
        expect(n.request_digest).toBe(go.request_body_sha256);
        expect(go.request_body_sha256).toBe(hash(e.request.postDataBuffer()!));
        expect(c.key_digest).toBe(n.key_digest);
        expect(n.key_digest).toBe(go.idempotency_key_sha256);
        expect(go.idempotency_key_sha256).toBe(
          hash(e.request.headers()["idempotency-key"]!),
        );
        expect(go.committed_verified).toBe(status === 200);
        expect(go.zero_commit_verified).toBe(status === 409);
      }
      const headers = await wire.headers({
        method,
        url: e.request.url(),
        status,
        requestID: e.rid!,
      });
      expect(headers.host_present && headers.host_matches).toBe(true);
      if (e.terminal === "failed") {
        expect(e.finished).toBe(0);
        expect(e.failed).toBe(1);
        expect(e.failure).toBe("net::ERR_ABORTED");
      } else {
        expect(e.finished).toBe(1);
        expect(e.failed).toBe(0);
        let timer: ReturnType<typeof setTimeout> | undefined;
        try {
          expect(
            await Promise.race([
              e.response!.finished(),
              new Promise<never>((_, reject) => {
                timer = setTimeout(
                  () => reject(Error("DEPENDENCY_FINISHED_TAIL")),
                  2000,
                );
              }),
            ]),
          ).toBeNull();
        } finally {
          if (timer) clearTimeout(timer);
        }
        const body = await e.response!.body();
        expect(body.length).toBe(n.bytes);
        expect(hash(body)).toBe(n.digest);
        expect(c.typed).toEqual(
          projected(JSON.parse(body.toString("utf8")), e.name, status),
        );
      }
      e.body = c.typed;
      e.checked = true;
      const { typed, query, original: originalInput, ...consumer } = c;
      proofs.push({
        seq: e.seq,
        request_id: e.rid,
        terminal: e.terminal,
        native: n,
        consumer,
        go,
        wire: headers,
      });
    }
  }
  function latest(name: string, task?: string) {
    const rows = entries.filter(
      (e) =>
        e.checked &&
        e.name === name &&
        (!task || new URL(e.request.url()).pathname.includes(`/tasks/${task}`)),
    );
    expect(rows.length).toBeGreaterThan(0);
    return rows.at(-1)!.body;
  }
  let added: any, resolved: any;
  return {
    verify,
    writes(count: number) {
      expect(writes).toBe(count);
    },
    candidates(pageNumber: 1 | 2 | "search") {
      const entry = entries
        .filter((e) => e.checked && e.name === "candidates")
        .at(-1)!;
      expect(entry).toBeTruthy();
      const url = new URL(entry.request.url());
      const rows =
        pageNumber === "search"
          ? data.rows.filter((t) => t.id === data.initial.id)
          : pageNumber === 1
            ? data.rows.slice(0, 50)
            : data.rows.slice(50);
      const current = resolved?.task ?? added?.task ?? data.initial;
      expect(entry.body.items).toEqual(
        rows.map((t) => {
          const value = t.id === data.initial.id ? current : t;
          const { description, plan, ...summary } = value;
          return summary;
        }),
      );
      if (pageNumber === 1) {
        expect(entry.body.next_cursor).toEqual(expect.any(String));
        expect(url.searchParams.has("cursor")).toBe(false);
      } else expect(entry.body.next_cursor).toBeUndefined();
      if (pageNumber === 2) {
        const previous = entries
          .filter(
            (e) =>
              e.checked &&
              e.name === "candidates" &&
              new URL(e.request.url()).searchParams.size === 1,
          )
          .at(-1)!;
        expect(url.searchParams.get("cursor")).toBe(previous.body.next_cursor);
      }
      if (pageNumber === "search")
        expect(url.searchParams.get("text")).toBe(data.initial.title);
      return entry.body;
    },
    receipt(stage: "added" | "resolved") {
      const result = latest(stage === "added" ? "add" : "resolve-blocker");
      expect(result.task).toEqual({
        ...data.initial,
        version: stage === "added" ? "2" : "3",
        updated_at: result.task.updated_at,
      });
      expect(result.task.updated_at >= data.initial.updated_at).toBe(true);
      expect(result.task_event_id).toMatch(uuid);
      expect(result.event_ids).toHaveLength(1);
      expect(result.event_ids[0]).toMatch(uuid);
      if (stage === "added") {
        expect(result.blocker).toEqual({
          id: blockerID,
          project_id: data.project_id,
          task_id: data.initial.id,
          type: "rely_on",
          description: data.description,
          related_task_id: data.related.id,
          created_at: result.task.updated_at,
          resolved_at: null,
          resolution_comment: null,
        });
        added = result;
      } else {
        expect(added).toBeTruthy();
        expect(result.blocker).toEqual({
          ...added.blocker,
          resolved_at: result.task.updated_at,
          resolution_comment: data.resolution,
        });
        expect(result.task_event_id).not.toBe(added.task_event_id);
        expect(result.event_ids[0]).not.toBe(added.event_ids[0]);
        resolved = result;
      }
      return result;
    },
    cycle() {
      expect(latest("cycle")).toMatchObject({
        code: "TASK_DEPENDENCY_CYCLE",
        status: 409,
        commit_state: "not_committed",
        instance: "/api/v1",
      });
      expect(writes).toBe(2);
    },
    current(task: "source" | "related") {
      const value =
        task === "related"
          ? data.related
          : (resolved?.task ?? added?.task ?? data.initial);
      expect(latest("task", value.id)).toEqual(value);
      const blocks =
        task === "related"
          ? []
          : resolved
            ? [resolved.blocker]
            : added
              ? [added.blocker]
              : [];
      expect(latest("blockers", value.id)).toEqual({ items: blocks });
      const events = latest("events", value.id);
      expect(events.next_cursor).toBeUndefined();
      expect(events.items).toHaveLength(
        task === "related" ? 1 : resolved ? 3 : added ? 2 : 1,
      );
      expect(events.items.at(-1).type).toBe("task_created");
      if (task === "source" && added) {
        const add = events.items.find((e: any) => e.type === "blocker_added");
        expect(add).toMatchObject({
          id: added.task_event_id,
          task_id: data.initial.id,
          task_version: "2",
          payload: { blocker_id: blockerID, blocker_type: "rely_on" },
        });
      }
      if (task === "source" && resolved) {
        const resolve = events.items.find(
          (e: any) => e.type === "blocker_resolved",
        );
        expect(resolve).toMatchObject({
          id: resolved.task_event_id,
          task_id: data.initial.id,
          task_version: "3",
          payload: {
            blocker_id: blockerID,
            blocker_type: "rely_on",
            resolution_comment: data.resolution,
          },
        });
      }
    },
    async finish() {
      await verify();
      expect(writes).toBe(3);
      expect(added && resolved).toBeTruthy();
      expect(entries.filter((e) => e.name === "cycle")).toHaveLength(1);
      wire.complete();
      completed = true;
    },
    async retire() {
      if (retired) return;
      try {
        await expect.poll(() => handlers).toBe(0);
        if (page.isClosed()) throw Error("DEPENDENCY_RETIRE_ORIGINAL_DOCUMENT");
        await expect
          .poll(() =>
            page.evaluate(
              () => (window as any).__taskDependencyNative.snapshot().pending,
            ),
          )
          .toBe(0);
        const result = await page.evaluate(() =>
          (window as any).__taskDependencyNative.finish(),
        );
        expect(result.retired).toBe(true);
        expect(result.failed).toBe(false);
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
        all_observed: entries.every((e) => e.checked),
        original_requests: entries.length,
        writes,
        committed: entries.filter(
          (e) => e.write !== undefined && e.status === 200,
        ).length,
        rejections: entries.filter(
          (e) => e.write !== undefined && e.status === 409,
        ).length,
        status,
        retired,
      };
      writeFileSync(
        join(evidence, "task-dependency-proof.json"),
        JSON.stringify(
          {
            ...result,
            requests: entries.map(({ request, response, body, ...e }) => e),
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
            "task-dependency-result.json",
          ),
          JSON.stringify(result),
          { mode: 0o600 },
        );
    },
  };
}
export async function dependencyScreenshot(page: Page, name: string) {
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
  const evidence = process.env.AGENTEAM_TASK_DEPENDENCY_WEB_EVIDENCE!;
  mkdirSync(evidence, { recursive: true, mode: 0o700 });
  await page.screenshot({
    path: join(evidence, `task-dependency-${name}.png`),
    fullPage: true,
    animations: "disabled",
  });
}
