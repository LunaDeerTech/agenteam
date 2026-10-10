import type { Page, Request, Response as PWResponse } from "@playwright/test";
import { createRequire } from "node:module";
import { basename, join } from "node:path";
import { readFile, readdir, writeFile } from "node:fs/promises";
import { writeFileSync } from "node:fs";

export type KnowledgeBinding = {
  entry: string;
  asset: string;
  exportName: string;
};

// Read-only selection of the one existing production singleton export. There
// is no rewritten asset, added export, or second Session/controller instance.
export async function knowledgeSessionBinding(
  root: string,
  dist: string,
): Promise<KnowledgeBinding> {
  const ts = createRequire(join(root, "web/package.json"))(
    "typescript",
  ) as typeof import("../../../web/node_modules/typescript");
  const parse = (code: string) =>
    ts.createSourceFile(
      "asset.js",
      code,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.JS,
    );
  const matches: { asset: string; exportName: string }[] = [];
  for (const file of (await readdir(join(dist, "assets")))
    .filter((v) => v.endsWith(".js"))
    .sort()) {
    const code = await readFile(join(dist, "assets", file), "utf8");
    if (!code.includes("getSession") || !code.includes("restore:")) continue;
    const ast = parse(code);
    const functions = new Map(
      ast.statements
        .filter(ts.isFunctionDeclaration)
        .filter((n) => n.name)
        .map((n) => [n.name!.text, n]),
    );
    for (const node of functions.values()) {
      const statements = node.body?.statements;
      if (
        node.parameters.length ||
        statements?.length !== 1 ||
        !ts.isReturnStatement(statements[0]!)
      )
        continue;
      const expression = statements[0]!.expression;
      if (
        !expression ||
        !ts.isBinaryExpression(expression) ||
        expression.operatorToken.kind !==
          ts.SyntaxKind.QuestionQuestionEqualsToken ||
        !ts.isIdentifier(expression.left) ||
        !ts.isCallExpression(expression.right) ||
        expression.right.arguments.length ||
        !ts.isIdentifier(expression.right.expression)
      )
        continue;
      const factory = functions.get(expression.right.expression.text),
        returns = factory?.body?.statements.filter(ts.isReturnStatement);
      if (
        returns?.length !== 1 ||
        !returns[0]!.expression ||
        !ts.isObjectLiteralExpression(returns[0]!.expression)
      )
        continue;
      const keys = returns[0]!.expression.properties.map((p) =>
        p.name?.getText(ast),
      );
      if (
        ![
          "state",
          "personalContext",
          "restore",
          "login",
          "logout",
          "leave",
          "restart",
          "projects",
          "knowledge",
        ].every((k) => keys.includes(k))
      )
        continue;
      const exports = ast.statements
        .filter(ts.isExportDeclaration)
        .flatMap((n) =>
          n.exportClause && ts.isNamedExports(n.exportClause)
            ? [...n.exportClause.elements]
            : [],
        )
        .filter((n) => (n.propertyName ?? n.name).text === node.name!.text);
      if (exports.length !== 1) throw Error("KNOWLEDGE_SINGLETON_EXPORT");
      matches.push({
        asset: "/assets/" + file,
        exportName: exports[0]!.name.text,
      });
    }
  }
  if (matches.length !== 1) throw Error("KNOWLEDGE_SINGLETON_UNIQUE");
  const html = await readFile(join(dist, "index.html"), "utf8");
  const scripts = [
    ...html.matchAll(/<script\b[^>]*\bsrc="(\/assets\/[^"/]+\.js)"[^>]*>/g),
  ];
  if (scripts.length !== 1) throw Error("KNOWLEDGE_APP_ENTRY");
  const entry = scripts[0]![1]!;
  const ast = parse(await readFile(join(dist, entry.slice(1)), "utf8"));
  const imports = ast.statements
    .filter(ts.isImportDeclaration)
    .map(
      (n) =>
        (
          n.moduleSpecifier as import("../../../web/node_modules/typescript").StringLiteral
        ).text,
    );
  if (
    imports.filter((p) => p === "./" + basename(matches[0]!.asset)).length !== 1
  )
    throw Error("KNOWLEDGE_SINGLETON_LOADED");
  return { ...matches[0]!, entry };
}

// These two functions are self-contained browser programs. Instrumentation
// only observes the original calls; every wrapper preserves this and returns
// the identical original Promise/value. No clone, tee, body read or new fetch.
export function installKnowledgeNative(config: {
  project: string;
  documents: string[];
  expiresAt: number;
}) {
  const host = window as any;
  if (host.__knowledgeNative || Date.now() >= config.expiresAt)
    throw Error("KNOWLEDGE_NATIVE_INSTALL");
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
  const prefix = `/api/v1/projects/${config.project}/knowledge/documents`;
  const selected = (url: URL, method: string) =>
    url.origin === location.origin &&
    method === "GET" &&
    (url.pathname === prefix + "/children" ||
      config.documents.some((id) =>
        [
          prefix + "/" + id,
          prefix + "/" + id + "/ancestors",
          prefix + "/" + id + "/content",
        ].includes(url.pathname),
      ));
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
    if (rows.length >= 256) {
      overflow = true;
      return Reflect.apply(originalFetch, this, [input, init]);
    }
    const callID =
      host.__knowledgePublication?.bind(url.pathname + url.search) ?? null;
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
                  const max = row.path.endsWith("/content") ? 7 << 20 : 5 << 20;
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
  host.__knowledgeNative = {
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

export async function installKnowledgePublication(config: {
  binding: KnowledgeBinding;
  project: string;
  documents: string[];
  user: string;
  expiresAt: number;
}) {
  const host = window as any;
  if (host.__knowledgePublication || Date.now() >= config.expiresAt)
    throw Error("KNOWLEDGE_PUBLICATION_INSTALL");
  const module = await import(/* @vite-ignore */ config.binding.asset);
  const useSession = module[config.binding.exportName];
  if (typeof useSession !== "function")
    throw Error("KNOWLEDGE_PUBLIC_SINGLETON");
  const auth = useSession();
  if (
    useSession() !== auth ||
    !auth.knowledge ||
    auth.state.phase !== "authenticated" ||
    auth.state.user?.id !== config.user ||
    auth.state.user?.role !== "user"
  )
    throw Error("KNOWLEDGE_CURRENT_OWNER");
  const identity = () => {
    const value = auth.personalContext.identity;
    return value
      ? JSON.stringify([value.userID, value.sessionID, value.epoch])
      : "";
  };
  const initialIdentity = identity();
  if (!initialIdentity || auth.personalContext.phase !== "current")
    throw Error("KNOWLEDGE_CURRENT_IDENTITY");
  const rows: any[] = [],
    tails = new Set<Promise<void>>(),
    detach: (() => void)[] = [];
  let retired = false,
    reason = "active",
    pendingAtRetirement: number | null = null,
    failed = false,
    overflow = false;
  const prefix = `/api/v1/projects/${config.project}/knowledge/documents`;
  const target = (method: string, args: any[]) => {
    if (args[0] !== config.project) throw Error("KNOWLEDGE_PROJECT_BINDING");
    if (method === "children") {
      if (args[1] !== null && !config.documents.includes(args[1]))
        throw Error("KNOWLEDGE_PARENT_BINDING");
      const query = new URLSearchParams({
        parent_document_id: args[1] ?? "null",
        limit: String(args[2].limit),
      });
      if (args[2].cursor !== undefined) query.set("cursor", args[2].cursor);
      return prefix + "/children?" + query;
    }
    if (!config.documents.includes(args[1]))
      throw Error("KNOWLEDGE_DOCUMENT_BINDING");
    const path = prefix + "/" + args[1];
    if (method === "get") return path;
    if (method === "ancestors") return path + "/ancestors";
    if (method !== "readContent") throw Error("KNOWLEDGE_METHOD_BINDING");
    return (
      path +
      "/content?" +
      new URLSearchParams({
        byte_offset: args[2]?.byte_offset ?? "0",
        max_bytes: String(args[2]?.max_bytes ?? 65536),
      })
    );
  };
  for (const method of ["children", "get", "ancestors", "readContent"]) {
    const original = auth.knowledge[method],
      descriptor = Object.getOwnPropertyDescriptor(auth.knowledge, method);
    if (typeof original !== "function" || !descriptor)
      throw Error("KNOWLEDGE_PUBLIC_METHOD");
    const wrapper = function (this: any, ...args: any[]) {
      if (retired) return Reflect.apply(original, this, args);
      if (rows.length >= 256) {
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
            if (!value || typeof value !== "object") {
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
    Object.defineProperty(auth.knowledge, method, {
      ...descriptor,
      value: wrapper,
    });
    detach.push(() => {
      if (auth.knowledge[method] !== wrapper) {
        failed = true;
        return;
      }
      Object.defineProperty(auth.knowledge, method, descriptor);
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
  host.__knowledgePublication = {
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

// Pure conjunction shared by the real case and falsification controls. All
// body/typed equality checks are additional caller checks on the same rows.
export function knowledgeOriginalCompleted(
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
    ((pw.terminal === "finished" &&
      pw.finished === true &&
      pw.finished_count === 1 &&
      pw.failed === false &&
      pw.failed_count === 0 &&
      pw.finished_calls === 1 &&
      pw.finished_state === "null" &&
      pw.finished_null === true &&
      pw.failure_kind === "not_observed") ||
      (pw.terminal === "failed" &&
        pw.finished === false &&
        pw.finished_count === 0 &&
        pw.failed === true &&
        pw.failed_count === 1 &&
        pw.finished_calls === 0 &&
        pw.finished_state === "not_entered" &&
        pw.finished_null === false &&
        pw.failure_kind === "net::ERR_ABORTED")) &&
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

export type KnowledgeReadPhase =
  | "login"
  | "project-entry"
  | "tree-keyboard"
  | "first-content"
  | "utf8-next"
  | "parent-read"
  | "drawer"
  | "completion-button"
  | "observer-idle"
  | "observer-finish"
  | "pw-tails"
  | "page-observers"
  | "report-write"
  | "joint-proof"
  | "schema";
type KnowledgeReadEvent =
  | "request"
  | "requestfinished"
  | "requestfailed"
  | "response"
  | "terminal-return"
  | "header-enter"
  | "header-return"
  | "header-reject"
  | "finished-enter"
  | "finished-return"
  | "finished-reject"
  | "pw-tail-joined"
  | "page-close"
  | "context-close"
  | "idle-sample"
  | "idle-reject"
  | "finish-reject"
  | "cleanup-return"
  | "cleanup-reject"
  | "phase-enter"
  | "phase-complete";

// A synchronous, bounded sink owned by the existing Node observer. It never
// reads a body, header collection or error text and never supplies proof.
export function createKnowledgeReadDiagnostic(config: {
  evidence: string;
  inputHash: string;
  project: string;
  documents: string[];
}) {
  let sequence = 0,
    phase: KnowledgeReadPhase = "login",
    writeFailed = false,
    overflow = false,
    pageClosed = false,
    contextClosed = false;
  let first: any = null,
    lastSample: any = null;
  let pwPending: number | null = null;
  const completed: KnowledgeReadPhase[] = [],
    events: any[] = [],
    tailEvents: any[] = [];
  const rows = new Map<number, any>();
  const prefix = `/api/v1/projects/${config.project}/knowledge/documents`;
  const uuid = (value: unknown) =>
    typeof value === "string" &&
    /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(value);
  const project = (row: any) => {
    const children = row.path === prefix + "/children";
    const content = config.documents.some(
      (id) => row.path === `${prefix}/${id}/content`,
    );
    const item = config.documents.some((id) =>
      [`${prefix}/${id}`, `${prefix}/${id}/ancestors`].includes(row.path),
    );
    const pathOK = children || content || item;
    const queryOK = children
      ? ["null", ...config.documents].some(
          (id) => row.query === `parent_document_id=${id}&limit=50`,
        )
      : content
        ? /^(?:byte_offset=0|byte_offset=65535)&max_bytes=65536$/.test(
            row.query,
          )
        : item && row.query === "";
    return {
      sequence: Number.isSafeInteger(row.sequence) ? row.sequence : null,
      method: row.method === "GET" ? "GET" : "invalid",
      path: pathOK ? row.path : "invalid",
      query: queryOK ? row.query : "invalid",
      request_id: uuid(row.header_request_id ?? row.request_id)
        ? (row.header_request_id ?? row.request_id)
        : null,
      invalid_request_id:
        (row.header_request_id ?? row.request_id) !== null &&
        !uuid(row.header_request_id ?? row.request_id),
      status:
        Number.isInteger(row.response_status ?? row.status) &&
        (row.response_status ?? row.status) >= 100 &&
        (row.response_status ?? row.status) <= 599
          ? (row.response_status ?? row.status)
          : null,
      finished: row.finished === true,
      finished_null: row.finished_null === true,
      failed: row.failed === true,
      terminal: ["pending", "finished", "failed", "closed"].includes(
        row.terminal,
      )
        ? row.terminal
        : "invalid",
      terminal_open: row.terminal_open === true,
      terminal_in_budget: row.terminal_in_budget === true,
      request_valid: row.request_valid === true,
      events_valid: row.events_valid === true,
      request_count: Number.isSafeInteger(row.request_count)
        ? row.request_count
        : null,
      response_count: Number.isSafeInteger(row.response_count)
        ? row.response_count
        : null,
      finished_count: Number.isSafeInteger(row.finished_count)
        ? row.finished_count
        : null,
      failed_count: Number.isSafeInteger(row.failed_count)
        ? row.failed_count
        : null,
      finished_calls: Number.isSafeInteger(row.finished_calls)
        ? row.finished_calls
        : null,
      failure_kind: [
        "not_observed",
        "net::ERR_ABORTED",
        "other",
        "unavailable",
      ].includes(row.failure_kind)
        ? row.failure_kind
        : "invalid",
      header: ["not_entered", "entered", "returned", "rejected"].includes(
        row.header_state,
      )
        ? row.header_state
        : "not_entered",
      finished_await: [
        "not_entered",
        "entered",
        "null",
        "error",
        "rejected",
      ].includes(row.finished_state)
        ? row.finished_state
        : "not_entered",
      tail_joined: row.tail_joined === true,
    };
  };
  const state = () => ({
    input_hash: config.inputHash,
    phase,
    sequence,
    completed: [...completed],
    page_closed: pageClosed,
    context_closed: contextClosed,
    write_failed: writeFailed,
    overflow,
    pw_pending: pwPending,
    pw_rows: [...rows.values()],
    // A historical sample is never a claim about pending at failure time.
    last_observers: lastSample,
    first_failure: first,
    events,
    tail_events: tailEvents,
  });
  const persist = () => {
    try {
      const raw = JSON.stringify(state());
      writeFileSync(
        join(config.evidence, "knowledge-read-diagnostic.json"),
        raw,
        { mode: 0o600 },
      );
      if (first)
        writeFileSync(
          join(config.evidence, "knowledge-first-failure.json"),
          raw,
          { mode: 0o600 },
        );
    } catch {
      writeFailed = true;
    }
  };
  const event = (kind: KnowledgeReadEvent, row?: any, pending?: number) => {
    if (pending !== undefined) pwPending = pending;
    if (row) rows.set(row.sequence, project(row));
    if (kind === "page-close") pageClosed = true;
    if (kind === "context-close") contextClosed = true;
    if (events.length + tailEvents.length >= 256) overflow = true;
    else
      (first ? tailEvents : events).push({
        sequence: ++sequence,
        phase,
        kind,
        ...(row ? { request: project(row) } : {}),
      });
    persist();
  };
  return {
    enter(value: KnowledgeReadPhase) {
      phase = value;
      event("phase-enter");
    },
    complete(value: KnowledgeReadPhase) {
      if (!completed.includes(value)) completed.push(value);
      event("phase-complete");
    },
    event,
    sample(value: unknown) {
      // value was already allowlisted inside the original idle evaluate.
      lastSample = {
        observed_at_sequence: sequence + 1,
        observed_in_phase: phase,
        current_at_failure: "not_observed",
        value,
      };
      event("idle-sample");
    },
    firstFailure(pageIsClosed: boolean, reportPresent: boolean) {
      if (!first) {
        pageClosed ||= pageIsClosed;
        first = JSON.parse(
          JSON.stringify({
            phase,
            sequence: ++sequence,
            completed,
            page_closed: pageClosed,
            context_closed: contextClosed,
            observer_report: reportPresent,
            pw_pending: pwPending,
            pw_rows: [...rows.values()],
            last_observers: lastSample,
            write_failed: writeFailed,
            overflow,
          }),
        );
      }
      persist();
    },
    healthy: () => !writeFailed && !overflow && first === null,
    snapshot: () => JSON.parse(JSON.stringify(state())),
  };
}

export async function observeKnowledge(
  page: Page,
  config: {
    root: string;
    dist: string;
    project: string;
    documents: string[];
    user: string;
    evidence: string;
    inputHash: string;
    diagnostic: ReturnType<typeof createKnowledgeReadDiagnostic>;
  },
) {
  const expiresAt = Date.now() + 45_000;
  const diagnostic = config.diagnostic;
  const binding = await knowledgeSessionBinding(config.root, config.dist);
  type Terminal = "finished" | "failed" | "closed";
  type Slot = {
    row: any;
    // Created once at the original request event; both response and finish
    // consume this same Promise. No later URL match can replace its Request.
    terminal: Promise<Terminal>;
    resolve: (value: Terminal) => void;
    finished?: Promise<Error | null>;
  };
  const rows = new Map<Request, any>(),
    slots = new Map<Request, Slot>(),
    tails = new Set<Promise<void>>();
  let failed = false,
    closed = false;
  const prefix = `/api/v1/projects/${config.project}/knowledge/documents`;
  const selected = (request: Request) =>
    new URL(request.url()).pathname.startsWith(prefix);
  const validRequest = (request: Request, url: URL) => {
    if (request.method() !== "GET" || url.hash) return false;
    const query = url.search.slice(1);
    if (url.pathname === prefix + "/children")
      return ["null", ...config.documents].some(
        (id) => query === `parent_document_id=${id}&limit=50`,
      );
    return config.documents.some((id) =>
      url.pathname === `${prefix}/${id}/content`
        ? /^(?:byte_offset=0|byte_offset=65535)&max_bytes=65536$/.test(query)
        : [`${prefix}/${id}`, `${prefix}/${id}/ancestors`].includes(
            url.pathname,
          ) && query === "",
    );
  };
  const invalidate = (row?: any) => {
    failed = true;
    if (row) row.events_valid = false;
  };
  const settle = (slot: Slot, value: Terminal) => {
    if (slot.row.terminal !== "pending") return;
    slot.row.terminal = value;
    slot.resolve(value);
  };
  const close = () => {
    failed = true;
    closed = true;
    clearTimeout(expiry);
    for (const slot of slots.values()) {
      invalidate(slot.row);
      settle(slot, "closed");
    }
  };
  // This is the existing 45s observation deadline, not an extra wait budget.
  // Closing releases terminal waiters, never an already-started finished().
  const expiry = setTimeout(close, Math.max(0, expiresAt - Date.now()));
  const requested = (request: Request) => {
    if (!selected(request)) return;
    const previous = rows.get(request);
    if (previous) {
      previous.request_count++;
      invalidate(previous);
      diagnostic.event("request", previous, tails.size);
      return;
    }
    if (
      closed ||
      page.isClosed() ||
      Date.now() >= expiresAt ||
      rows.size >= 256
    ) {
      invalidate();
      return;
    }
    const url = new URL(request.url());
    const row = {
      sequence: rows.size + 1,
      method: request.method(),
      path: url.pathname,
      query: url.search.slice(1),
      request_id: null,
      status: null,
      request_count: 1,
      response_count: 0,
      request_valid: validRequest(request, url),
      events_valid: true,
      terminal: "pending",
      terminal_open: false,
      terminal_in_budget: false,
      finished: false,
      finished_count: 0,
      finished_calls: 0,
      finished_state: "not_entered",
      finished_null: false,
      failed: false,
      failed_count: 0,
      failure_kind: "not_observed",
    };
    let resolve!: (value: Terminal) => void;
    const terminal = new Promise<Terminal>((done) => (resolve = done));
    rows.set(request, row);
    slots.set(request, { row, terminal, resolve });
    if (!row.request_valid) invalidate(row);
    diagnostic.event("request", row, tails.size);
  };
  const terminalEvent = (request: Request, kind: "finished" | "failed") => {
    const slot = slots.get(request);
    if (!slot) {
      if (selected(request)) invalidate();
      return;
    }
    const row = slot.row;
    row[kind] = true;
    row[kind + "_count"]++;
    const open = !closed && !page.isClosed(),
      inBudget = Date.now() < expiresAt;
    if (row.terminal !== "pending" || !open || !inBudget) {
      invalidate(row);
      settle(slot, "closed");
    } else {
      row.terminal_open = open;
      row.terminal_in_budget = inBudget;
      if (kind === "failed") {
        // LIVE observation only. Never persist error text, or infer this enum
        // from read02's requestfailed event, a later GET or a rejected await.
        try {
          const failure = request.failure();
          row.failure_kind =
            failure?.errorText === "net::ERR_ABORTED"
              ? "net::ERR_ABORTED"
              : failure
                ? "other"
                : "unavailable";
        } catch {
          row.failure_kind = "unavailable";
        }
        if (row.failure_kind !== "net::ERR_ABORTED") invalidate(row);
      }
      settle(slot, kind);
    }
    diagnostic.event(
      kind === "finished" ? "requestfinished" : "requestfailed",
      row,
      tails.size,
    );
  };
  const finished = (request: Request) => terminalEvent(request, "finished");
  const rejected = (request: Request) => terminalEvent(request, "failed");
  const response = (response: PWResponse) => {
    const slot = slots.get(response.request());
    if (!slot) {
      if (selected(response.request())) invalidate();
      return;
    }
    const row = slot.row;
    row.response_count++;
    if (
      row.response_count !== 1 ||
      closed ||
      page.isClosed() ||
      Date.now() >= expiresAt
    ) {
      invalidate(row);
      diagnostic.event("response", row, tails.size);
      return;
    }
    row.response_status = response.status();
    const tail = (async () => {
      row.header_state = "entered";
      diagnostic.event("header-enter", row, tails.size);
      let id: string | null;
      try {
        id = await response.headerValue("x-request-id");
      } catch (error) {
        row.header_state = "rejected";
        diagnostic.event("header-reject", row, tails.size);
        throw error;
      }
      row.header_state = "returned";
      row.header_request_id =
        typeof id === "string" &&
        /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(id)
          ? id
          : "invalid";
      diagnostic.event("header-return", row, tails.size);
      if (row.header_request_id === "invalid") invalidate(row);
      const terminal = await slot.terminal;
      diagnostic.event("terminal-return", row, tails.size);
      if (
        closed ||
        page.isClosed() ||
        Date.now() >= expiresAt ||
        !row.events_valid ||
        terminal === "closed"
      ) {
        invalidate(row);
        return;
      }
      if (terminal === "finished") {
        if (row.finished_count !== 1 || row.failed_count !== 0) {
          invalidate(row);
          return;
        }
        // Only the normal terminal may create this Promise, at most once.
        // Once created its actual settlement is always joined, even on close.
        if (!slot.finished) {
          row.finished_calls++;
          row.finished_state = "entered";
          diagnostic.event("finished-enter", row, tails.size);
          slot.finished = response.finished();
        }
        let result: Error | null;
        try {
          result = await slot.finished;
        } catch (error) {
          row.finished_state = "rejected";
          diagnostic.event("finished-reject", row, tails.size);
          throw error;
        }
        row.finished_state = result === null ? "null" : "error";
        row.finished_null = result === null;
        diagnostic.event("finished-return", row, tails.size);
      } else if (
        row.failed_count !== 1 ||
        row.finished_count !== 0 ||
        row.failure_kind !== "net::ERR_ABORTED" ||
        slot.finished
      ) {
        invalidate(row);
        return;
      }
      if (closed || page.isClosed() || Date.now() >= expiresAt) {
        invalidate(row);
        return;
      }
      row.request_id = id;
      row.status = response.status();
    })()
      .catch(() => {
        invalidate(row);
      })
      .then(() => {
        tails.delete(tail);
        row.tail_joined = true;
        diagnostic.event("pw-tail-joined", row, tails.size);
      });
    tails.add(tail);
    diagnostic.event("response", row, tails.size);
  };
  const pageClose = () => {
    diagnostic.event("page-close", undefined, tails.size);
    close();
  };
  const contextClose = () => {
    diagnostic.event("context-close", undefined, tails.size);
    close();
  };
  page.on("request", requested);
  page.on("requestfinished", finished);
  page.on("requestfailed", rejected);
  page.on("response", response);
  page.on("close", pageClose);
  page.context().on("close", contextClose);
  const detach = () => {
    page.off("request", requested);
    page.off("requestfinished", finished);
    page.off("requestfailed", rejected);
    page.off("response", response);
    page.off("close", pageClose);
    page.context().off("close", contextClose);
  };
  // Installed in the current document after login, before its first Knowledge
  // navigation. It does not instrument unrelated Account material.
  try {
    await page.evaluate(installKnowledgeNative, {
      project: config.project,
      documents: config.documents,
      expiresAt,
    });
    await page.evaluate(installKnowledgePublication, {
      binding,
      project: config.project,
      documents: config.documents,
      user: config.user,
      expiresAt,
    });
  } catch (error) {
    close();
    detach();
    throw error;
  }
  let report: any;
  return {
    async finish() {
      if (report) return report;
      try {
        diagnostic.enter("pw-tails");
        await Promise.all([
          ...tails,
          ...[...slots.values()].map((slot) => slot.terminal),
        ]);
        diagnostic.complete("pw-tails");
        diagnostic.enter("page-observers");
        const value = await page.evaluate(async () => {
          const host = window as any;
          const native = host.__knowledgeNative.finish();
          const publication = host.__knowledgePublication.finish();
          return {
            native: await native,
            publication: await publication,
          };
        });
        diagnostic.complete("page-observers");
        closed = true;
        clearTimeout(expiry);
        detach();
        report = {
          ...value,
          pw_failed: failed || !diagnostic.healthy(),
          pw_pending: tails.size,
          requests: [...rows.values()],
          input_hash: config.inputHash,
        };
        diagnostic.enter("report-write");
        await writeFile(
          join(config.evidence, "knowledge-native-consumption.json"),
          JSON.stringify(report),
          { mode: 0o600 },
        );
        diagnostic.complete("report-write");
        return report;
      } catch (error) {
        diagnostic.event("finish-reject", undefined, tails.size);
        clearTimeout(expiry);
        close();
        throw error;
      }
    },
    async idle() {
      try {
        const sample = await page.evaluate(() => {
          const host = window as any;
          const native = host.__knowledgeNative.snapshot(),
            publication = host.__knowledgePublication.snapshot();
          const number = (v: unknown) =>
            typeof v === "number" && Number.isSafeInteger(v) && v >= 0
              ? v
              : null;
          const project = (v: any) => ({
            retired: v.retired === true,
            reason: ["active", "explicit", "expired"].includes(v.reason)
              ? v.reason
              : "invalid",
            pending: number(v.pending),
            pending_at_retirement: number(v.pending_at_retirement),
            failed: v.failed === true,
            overflow: v.overflow === true,
            current: typeof v.current === "boolean" ? v.current : null,
            not_busy: typeof v.not_busy === "boolean" ? v.not_busy : null,
            row_count: Array.isArray(v.rows) ? v.rows.length : null,
            rows: Array.isArray(v.rows)
              ? v.rows.slice(0, 256).map((r: any) => {
                  const result: Record<string, number | boolean | null> = {};
                  for (const key of [
                    "sequence",
                    "id",
                    "call_id",
                    "bytes",
                    "readers",
                    "reads",
                    "read_returns",
                    "reader_cancel",
                    "reader_cancel_joined",
                    "release",
                    "outer_cancel",
                    "outer_cancel_joined",
                    "bound",
                  ])
                    result[key] = number(r[key]);
                  for (const key of [
                    "fetch_returned",
                    "fetch_rejected",
                    "eof",
                    "read_rejected",
                    "owner_error",
                    "fulfilled",
                    "rejected",
                    "settled",
                    "current",
                    "not_busy",
                  ])
                    result[key] = typeof r[key] === "boolean" ? r[key] : null;
                  return result;
                })
              : [],
          });
          return {
            idle: native.pending === 0 && publication.pending === 0,
            observers: {
              native: project(native),
              publication: project(publication),
            },
          };
        });
        diagnostic.sample(sample.observers);
        return sample.idle;
      } catch (error) {
        diagnostic.event("idle-reject", undefined, tails.size);
        throw error;
      }
    },
  };
}
