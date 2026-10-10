import type { Page, Request, Response as PWResponse } from "@playwright/test";
import {
  knowledgeSessionBinding,
  type KnowledgeBinding as SkillBinding,
} from "./knowledge-owner-read.native";

// These private observers preserve the original methods and Promise identities.
// They never create a Session, request, cloned body or replacement response.
export function installSkillNative(config: {
  project: string;
  skill: string;
  expiresAt: number;
}) {
  const host = window as any;
  if (host.__skillNative || Date.now() >= config.expiresAt)
    throw Error("SKILL_NATIVE_INSTALL");
  const originalFetch = window.fetch,
    detach: (() => void)[] = [],
    tails = new Set<Promise<void>>(),
    rows: any[] = [];
  const documentID = crypto.randomUUID();
  let retired = false,
    reason = "active",
    pendingAtRetirement: number | null = null,
    failed = false,
    overflow = false;
  const safe = (fn: () => void) => {
    try {
      fn();
    } catch {
      failed = true;
    }
  };
  const replace = (object: any, key: string, value: any) => {
    const descriptor = Object.getOwnPropertyDescriptor(object, key);
    Object.defineProperty(object, key, {
      value,
      writable: true,
      configurable: true,
    });
    detach.push(() => {
      if (object[key] !== value) {
        failed = true;
        return;
      }
      if (descriptor) Object.defineProperty(object, key, descriptor);
      else delete object[key];
    });
  };
  function observe(
    promise: Promise<any>,
    good: (value: any) => void,
    bad: () => void,
  ) {
    if (!promise || typeof promise.then !== "function") {
      failed = true;
      return;
    }
    const tail = promise
      .then(
        (v) => {
          if (!retired) safe(() => good(v));
        },
        () => {
          if (!retired) safe(bad);
        },
      )
      .catch(() => {
        failed = true;
      })
      .then(() => {
        tails.delete(tail);
      });
    tails.add(tail);
  }
  const prefix = `/api/v1/projects/${config.project}/skills`;
  const selected = (url: URL, method: string) =>
    url.origin === location.origin &&
    method === "GET" &&
    !url.search &&
    !url.hash &&
    [prefix, prefix + "/" + config.skill].includes(url.pathname);
  const wrapper: typeof fetch = function (this: any, input, init) {
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
    if (!selected(url, method) || retired)
      return Reflect.apply(originalFetch, this, [input, init]);
    if (rows.length >= 32) {
      overflow = true;
      return Reflect.apply(originalFetch, this, [input, init]);
    }
    const callID =
      host.__skillPublication?.bind(url.pathname + url.search) ?? null;
    const row: any = {
      sequence: rows.length + 1,
      document_id: documentID,
      call_id: callID,
      method,
      path: url.pathname,
      query: url.search.slice(1),
      request_id: null,
      status: null,
      content_length: null,
      bytes: 0,
      digest: null,
      fetch_returned: false,
      fetch_rejected: false,
      readers: 0,
      reads: 0,
      read_returns: 0,
      eof: false,
      read_rejected: false,
      reader_cancel: 0,
      reader_cancel_joined: 0,
      release: 0,
      outer_cancel: 0,
      outer_cancel_joined: 0,
      owner_error: false,
    };
    rows.push(row);
    let result: Promise<Response>;
    try {
      result = Reflect.apply(originalFetch, this, [input, init]);
    } catch (e) {
      row.fetch_rejected = true;
      throw e;
    }
    observe(
      result,
      (response) => {
        row.fetch_returned = true;
        row.status = response.status;
        row.request_id = response.headers.get("x-request-id");
        row.content_length = response.headers.get("content-length");
        const stream = response.body;
        if (!stream) {
          row.owner_error = true;
          return;
        }
        const getReader = stream.getReader,
          outerCancel = stream.cancel;
        replace(stream, "cancel", function (this: any, ...args: any[]) {
          if (!retired) row.outer_cancel++;
          const promise = Reflect.apply(
            outerCancel,
            this,
            args,
          ) as Promise<void>;
          observe(
            promise,
            () => {
              row.outer_cancel_joined++;
            },
            () => {
              row.owner_error = true;
            },
          );
          return promise;
        });
        replace(stream, "getReader", function (this: any, ...args: any[]) {
          const reader = Reflect.apply(
            getReader,
            this,
            args,
          ) as ReadableStreamDefaultReader<Uint8Array>;
          if (!retired) row.readers++;
          const read = reader.read,
            cancel = reader.cancel,
            release = reader.releaseLock;
          let chunks: Uint8Array[] = [];
          replace(reader, "read", function (this: any, ...args: any[]) {
            if (!retired) row.reads++;
            const promise = Reflect.apply(read, this, args) as Promise<
              ReadableStreamReadResult<Uint8Array>
            >;
            observe(
              promise,
              (value) => {
                row.read_returns++;
                if (value.done) {
                  if (row.eof) {
                    row.owner_error = true;
                    return;
                  }
                  row.eof = true;
                  const bytes = new Uint8Array(row.bytes);
                  let at = 0;
                  for (const chunk of chunks) {
                    bytes.set(chunk, at);
                    at += chunk.length;
                  }
                  chunks = [];
                  observe(
                    crypto.subtle.digest("SHA-256", bytes),
                    (digest) => {
                      row.digest = [...new Uint8Array(digest)]
                        .map((v) => v.toString(16).padStart(2, "0"))
                        .join("");
                    },
                    () => {
                      row.owner_error = true;
                    },
                  );
                } else if (value.value instanceof Uint8Array) {
                  row.bytes += value.value.byteLength;
                  const max = 64 << 10;
                  if (row.bytes > max) {
                    row.owner_error = true;
                    return;
                  }
                  chunks.push(value.value.slice());
                } else row.owner_error = true;
              },
              () => {
                row.read_rejected = true;
              },
            );
            return promise;
          });
          replace(reader, "cancel", function (this: any, ...args: any[]) {
            if (!retired) row.reader_cancel++;
            const promise = Reflect.apply(cancel, this, args) as Promise<void>;
            observe(
              promise,
              () => {
                row.reader_cancel_joined++;
              },
              () => {
                row.owner_error = true;
              },
            );
            return promise;
          });
          replace(reader, "releaseLock", function (this: any, ...args: any[]) {
            const result = Reflect.apply(release, this, args);
            if (!retired) row.release++;
            return result;
          });
          return reader;
        });
      },
      () => {
        row.fetch_rejected = true;
      },
    );
    return result;
  };
  replace(window, "fetch", wrapper);
  const seal = (why: string) => {
    if (retired) return;
    reason = why;
    pendingAtRetirement = tails.size;
    retired = true;
    clearTimeout(timer);
    for (const restore of detach.reverse()) safe(restore);
  };
  const timer = setTimeout(
    () => seal("expired"),
    Math.max(0, config.expiresAt - Date.now()),
  );
  host.__skillNative = {
    snapshot: () => ({
      document_id: documentID,
      retired,
      reason,
      pending: tails.size,
      pending_at_retirement: pendingAtRetirement,
      failed,
      overflow,
      rows: rows.map((v) => ({ ...v })),
    }),
    async finish() {
      seal("explicit");
      await Promise.all([...tails]);
      return this.snapshot();
    },
  };
}

export async function installSkillPublication(config: {
  binding: SkillBinding;
  project: string;
  skill: string;
  user: string;
  expiresAt: number;
}) {
  const host = window as any;
  if (host.__skillPublication || Date.now() >= config.expiresAt)
    throw Error("SKILL_PUBLICATION_INSTALL");
  const module = await import(/* @vite-ignore */ config.binding.asset);
  const useSession = module[config.binding.exportName];
  if (typeof useSession !== "function") throw Error("SKILL_PUBLIC_SINGLETON");
  const auth = useSession();
  if (
    useSession() !== auth ||
    !auth.skills ||
    auth.state.phase !== "authenticated" ||
    auth.state.user?.id !== config.user ||
    auth.state.user?.role !== "user"
  )
    throw Error("SKILL_CURRENT_OWNER");
  const identity = () => {
    const value = auth.personalContext.identity;
    return value
      ? JSON.stringify([value.userID, value.sessionID, value.epoch])
      : "";
  };
  const initialIdentity = identity();
  if (!initialIdentity || auth.personalContext.phase !== "current")
    throw Error("SKILL_CURRENT_IDENTITY");
  const rows: any[] = [],
    tails = new Set<Promise<void>>(),
    detach: (() => void)[] = [];
  let retired = false,
    reason = "active",
    pendingAtRetirement: number | null = null,
    failed = false,
    overflow = false;
  const prefix = `/api/v1/projects/${config.project}/skills`;
  const target = (method: string, args: any[]) => {
    if (args[0] !== config.project) throw Error("SKILL_PROJECT_BINDING");
    if (method === "list" && args.length === 1) return prefix;
    if (method === "get" && args.length === 2 && args[1] === config.skill)
      return prefix + "/" + config.skill;
    throw Error("SKILL_METHOD_BINDING");
  };
  for (const method of ["list", "get"]) {
    const original = auth.skills[method],
      descriptor = Object.getOwnPropertyDescriptor(auth.skills, method);
    if (typeof original !== "function" || !descriptor)
      throw Error("SKILL_PUBLIC_METHOD");
    const wrapper = function (this: any, ...args: any[]) {
      if (retired) return Reflect.apply(original, this, args);
      if (rows.length >= 32) {
        overflow = true;
        return Reflect.apply(original, this, args);
      }
      let path: string;
      try {
        path = target(method, args);
      } catch {
        failed = true;
        return Reflect.apply(original, this, args);
      }
      const row: any = {
        id: rows.length + 1,
        method,
        url: path,
        bound: 0,
        fulfilled: false,
        rejected: false,
        settled: false,
        current: false,
        not_busy: false,
        typed: null,
      };
      rows.push(row);
      let promise: Promise<any>;
      try {
        promise = Reflect.apply(original, this, args);
      } catch (error) {
        row.rejected = true;
        row.settled = true;
        throw error;
      }
      if (!promise || typeof promise.then !== "function") {
        failed = true;
        return promise;
      }
      const tail = promise
        .then(
          (value) => {
            if (retired) return;
            row.fulfilled = true;
            row.settled = true;
            row.current =
              identity() === initialIdentity &&
              auth.personalContext.phase === "current" &&
              auth.state.phase === "authenticated";
            row.not_busy = auth.state.busy === false;
            // This is the real strict API result of the actual public method.
            // Void fulfillment is never publication; bytes are not re-consumed.
            if (
              !value ||
              typeof value !== "object" ||
              !Object.isFrozen(value) ||
              (method === "list" &&
                (!Object.isFrozen(value.items) ||
                  !value.items.every(Object.isFrozen)))
            ) {
              failed = true;
              return;
            }
            row.typed = JSON.parse(JSON.stringify(value));
          },
          () => {
            if (!retired) {
              row.rejected = true;
              row.settled = true;
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
      return promise;
    };
    Object.defineProperty(auth.skills, method, {
      ...descriptor,
      value: wrapper,
    });
    detach.push(() => {
      if (auth.skills[method] !== wrapper) {
        failed = true;
        return;
      }
      Object.defineProperty(auth.skills, method, descriptor);
    });
  }
  const seal = (why: string) => {
    if (retired) return;
    reason = why;
    pendingAtRetirement = tails.size;
    retired = true;
    clearTimeout(timer);
    for (const restore of detach.reverse()) {
      try {
        restore();
      } catch {
        failed = true;
      }
    }
  };
  const timer = setTimeout(
    () => seal("expired"),
    Math.max(0, config.expiresAt - Date.now()),
  );
  host.__skillPublication = {
    bind(url: string) {
      const candidates = rows.filter((row) => !row.settled && row.url === url);
      if (retired || candidates.length !== 1 || candidates[0].bound !== 0) {
        failed = true;
        return null;
      }
      candidates[0].bound++;
      return candidates[0].id;
    },
    snapshot: () => ({
      retired,
      reason,
      pending: tails.size,
      pending_at_retirement: pendingAtRetirement,
      failed,
      overflow,
      current:
        identity() === initialIdentity &&
        auth.personalContext.phase === "current" &&
        auth.state.phase === "authenticated",
      not_busy: auth.state.busy === false,
      rows: rows.map((v) => ({ ...v })),
    }),
    async finish() {
      seal("explicit");
      await Promise.all([...tails]);
      return this.snapshot();
    },
  };
  return true;
}

export function skillOriginalCompleted(
  native: any,
  publication: any,
  pw: any,
  sidecar: any,
) {
  const n = native?.rows?.find((v: any) => v.request_id === pw?.request_id);
  const p = publication?.rows?.find((v: any) => v.id === n?.call_id);
  const retired = (v: any) =>
    v?.retired === true &&
    v.reason === "explicit" &&
    v.pending_at_retirement === 0 &&
    v.pending === 0 &&
    v.failed === false &&
    v.overflow === false;
  return !!(
    retired(native) &&
    retired(publication) &&
    publication.current === true &&
    publication.not_busy === true &&
    n &&
    p &&
    native.rows.filter((v: any) => v.request_id === n.request_id).length ===
      1 &&
    publication.rows.filter((v: any) => v.id === n.call_id).length === 1 &&
    typeof n.request_id === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      n.request_id,
    ) &&
    pw.request_count === 1 &&
    pw.response_count === 1 &&
    pw.request_valid === true &&
    pw.events_valid === true &&
    pw.terminal_open === true &&
    pw.terminal_in_budget === true &&
    pw.tail_joined === true &&
    pw.terminal === "finished" &&
    pw.finished === true &&
    pw.finished_count === 1 &&
    pw.failed === false &&
    pw.failed_count === 0 &&
    pw.finished_calls === 1 &&
    pw.finished_null === true &&
    pw.method === "GET" &&
    pw.status === 200 &&
    pw.path === n.path &&
    pw.query === n.query &&
    sidecar.method === pw.method &&
    sidecar.request_id === n.request_id &&
    sidecar.path === n.path &&
    sidecar.query === n.query &&
    sidecar.status === n.status &&
    n.document_id === native.document_id &&
    n.method === "GET" &&
    n.status === 200 &&
    n.fetch_returned === true &&
    n.fetch_rejected === false &&
    n.readers === 1 &&
    n.reads === n.read_returns &&
    n.reads > 0 &&
    n.eof === true &&
    n.read_rejected === false &&
    n.reader_cancel === 1 &&
    n.reader_cancel_joined === 1 &&
    n.release === 1 &&
    n.outer_cancel === 1 &&
    n.outer_cancel_joined === 1 &&
    n.owner_error === false &&
    /^(0|[1-9][0-9]*)$/.test(n.content_length ?? "") &&
    Number(n.content_length) === n.bytes &&
    sidecar.content_length === n.bytes &&
    n.digest === sidecar.body_sha256 &&
    p.bound === 1 &&
    p.url === n.path + (n.query ? "?" + n.query : "") &&
    p.fulfilled === true &&
    p.rejected === false &&
    p.settled === true &&
    p.current === true &&
    p.not_busy === true &&
    p.typed &&
    typeof p.typed === "object"
  );
}

// A single original Request owns its event Promise and normal finished() call.
// Failure never starts finished(), and no native-consumption fallback exists.
export async function observeSkills(
  page: Page,
  config: {
    root: string;
    dist: string;
    project: string;
    skill: string;
    user: string;
    expiresAt: number;
  },
) {
  const prefix = `/api/v1/projects/${config.project}/skills`,
    origin = new URL(page.url()).origin;
  type Terminal = "pending" | "finished" | "failed" | "closed";
  type Slot = {
    row: any;
    terminal: Promise<Terminal>;
    resolve: (v: Terminal) => void;
  };
  const slots = new Map<Request, Slot>(),
    tails = new Set<Promise<void>>();
  let failed = false,
    closed = false,
    sealed = false,
    report: any;
  const selected = (request: Request) =>
    new URL(request.url()).pathname.startsWith(prefix);
  const valid = (request: Request) => {
    const u = new URL(request.url());
    return (
      u.origin === origin &&
      request.method() === "GET" &&
      !u.search &&
      !u.hash &&
      request.postData() === null &&
      [prefix, prefix + "/" + config.skill].includes(u.pathname)
    );
  };
  const invalidate = (row?: any) => {
    failed = true;
    if (row) row.events_valid = false;
  };
  const close = () => {
    failed = true;
    closed = true;
    sealed = true;
    clearTimeout(timer);
    for (const slot of slots.values()) {
      invalidate(slot.row);
      if (slot.row.terminal === "pending") {
        slot.row.terminal = "closed";
        slot.resolve("closed");
      }
    }
  };
  const timer = setTimeout(close, Math.max(0, config.expiresAt - Date.now()));
  const requested = (request: Request) => {
    if (!selected(request)) return;
    if (slots.has(request)) {
      slots.get(request)!.row.request_count++;
      invalidate(slots.get(request)!.row);
      return;
    }
    if (
      sealed ||
      closed ||
      page.isClosed() ||
      Date.now() >= config.expiresAt ||
      slots.size >= 32
    ) {
      invalidate();
      return;
    }
    const u = new URL(request.url());
    const row: any = {
      sequence: slots.size + 1,
      method: request.method(),
      path: u.pathname,
      query: u.search.slice(1),
      request_id: null,
      status: null,
      request_count: 1,
      response_count: 0,
      request_valid: valid(request),
      events_valid: true,
      terminal: "pending",
      terminal_open: false,
      terminal_in_budget: false,
      finished: false,
      finished_count: 0,
      failed: false,
      failed_count: 0,
      finished_calls: 0,
      finished_null: false,
      tail_joined: false,
      header_state: "not_entered",
    };
    let resolve!: (v: Terminal) => void;
    const terminal = new Promise<Terminal>((done) => (resolve = done));
    slots.set(request, { row, terminal, resolve });
    if (!row.request_valid) invalidate(row);
  };
  const terminal = (request: Request, kind: "finished" | "failed") => {
    const slot = slots.get(request);
    if (!slot) {
      if (selected(request)) invalidate();
      return;
    }
    const r = slot.row;
    r[kind] = true;
    r[kind + "_count"]++;
    if (
      r.terminal !== "pending" ||
      closed ||
      page.isClosed() ||
      Date.now() >= config.expiresAt
    ) {
      invalidate(r);
      return;
    }
    r.terminal_open = true;
    r.terminal_in_budget = true;
    r.terminal = kind;
    if (kind === "failed") invalidate(r);
    slot.resolve(kind);
  };
  const finished = (r: Request) => terminal(r, "finished"),
    rejected = (r: Request) => terminal(r, "failed");
  const response = (response: PWResponse) => {
    const slot = slots.get(response.request());
    if (!slot) {
      if (selected(response.request())) invalidate();
      return;
    }
    const r = slot.row;
    r.response_count++;
    if (r.response_count !== 1 || closed || Date.now() >= config.expiresAt) {
      invalidate(r);
      return;
    }
    const tail = (async () => {
      r.header_state = "entered";
      const xid = await response.headerValue("x-request-id");
      r.header_state = "returned";
      if (
        typeof xid !== "string" ||
        !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          xid,
        )
      ) {
        invalidate(r);
        return;
      }
      const ended = await slot.terminal;
      if (
        ended !== "finished" ||
        closed ||
        Date.now() >= config.expiresAt ||
        !r.events_valid
      ) {
        invalidate(r);
        return;
      }
      r.finished_calls++;
      r.finished_null = (await response.finished()) === null;
      if (
        !r.finished_null ||
        closed ||
        page.isClosed() ||
        Date.now() >= config.expiresAt
      ) {
        invalidate(r);
        return;
      }
      r.request_id = xid;
      r.status = response.status();
      if (r.status !== 200) invalidate(r);
    })()
      .catch(() => invalidate(r))
      .then(() => {
        r.tail_joined = true;
        tails.delete(tail);
      });
    tails.add(tail);
  };
  page.on("request", requested);
  page.on("requestfinished", finished);
  page.on("requestfailed", rejected);
  page.on("response", response);
  page.on("close", close);
  page.context().on("close", close);
  const detach = () => {
    page.off("request", requested);
    page.off("requestfinished", finished);
    page.off("requestfailed", rejected);
    page.off("response", response);
    page.off("close", close);
    page.context().off("close", close);
  };
  const snapshot = async () => ({
    pw_failed: failed,
    pw_pending: tails.size,
    requests: [...slots.values()].map((s) => ({ ...s.row })),
    ...(await page.evaluate(() => {
      const h = window as any;
      return {
        native: h.__skillNative?.snapshot() ?? null,
        publication: h.__skillPublication?.snapshot() ?? null,
      };
    })),
  });
  const abort = async () => {
    close();
    // Closing the actual page releases original protocol operations. Join them;
    // closing never counts as a successful response or observer retirement.
    if (!page.isClosed()) await page.close({ runBeforeUnload: false });
    await Promise.all([...tails]);
    detach();
    return {
      pw_failed: failed,
      pw_pending: tails.size,
      requests: [...slots.values()].map((s) => ({ ...s.row })),
    };
  };
  try {
    const binding = await knowledgeSessionBinding(config.root, config.dist);
    await page.evaluate(installSkillNative, {
      project: config.project,
      skill: config.skill,
      expiresAt: config.expiresAt,
    });
    if (
      (await page.evaluate(installSkillPublication, {
        binding,
        project: config.project,
        skill: config.skill,
        user: config.user,
        expiresAt: config.expiresAt,
      })) !== true
    )
      throw Error("SKILL_INSTALL");
  } catch (error) {
    await abort();
    throw error;
  }
  return {
    snapshot,
    abort,
    async idle() {
      const s = await snapshot();
      return (
        !s.pw_failed &&
        s.pw_pending === 0 &&
        slots.size > 0 &&
        [...slots.values()].every(
          (s) => s.row.terminal === "finished" && s.row.tail_joined,
        ) &&
        s.native?.pending === 0 &&
        s.publication?.pending === 0
      );
    },
    async finish() {
      if (report) return report;
      sealed = true;
      await Promise.all([
        ...tails,
        ...[...slots.values()].map((s) => s.terminal),
      ]);
      const result = await page.evaluate(async () => {
        const h = window as any;
        const n = h.__skillNative.finish(),
          p = h.__skillPublication.finish();
        return { native: await n, publication: await p };
      });
      if (closed || page.isClosed() || Date.now() >= config.expiresAt)
        invalidate();
      report = {
        ...result,
        pw_failed: failed,
        pw_pending: tails.size,
        requests: [...slots.values()].map((s) => ({ ...s.row })),
      };
      closed = true;
      clearTimeout(timer);
      detach();
      return report;
    },
  };
}
