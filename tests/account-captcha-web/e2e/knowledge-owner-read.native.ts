import type { Page, Request, Response as PWResponse } from "@playwright/test";
import { createRequire } from "node:module";
import { basename, join } from "node:path";
import { readFile, readdir, writeFile } from "node:fs/promises";

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
    pw.finished === true &&
    pw.finished_null === true &&
    pw.failed === false &&
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
  },
) {
  const expiresAt = Date.now() + 45_000;
  const binding = await knowledgeSessionBinding(config.root, config.dist);
  const rows = new Map<Request, any>(),
    tails = new Set<Promise<void>>();
  let failed = false,
    closed = false;
  const selected = (request: Request) =>
    new URL(request.url()).pathname.startsWith(
      `/api/v1/projects/${config.project}/knowledge/documents`,
    );
  const requested = (request: Request) => {
    if (!selected(request)) return;
    if (closed || rows.size >= 256) {
      failed = true;
      return;
    }
    const url = new URL(request.url());
    rows.set(request, {
      sequence: rows.size + 1,
      method: request.method(),
      path: url.pathname,
      query: url.search.slice(1),
      request_id: null,
      status: null,
      finished: false,
      finished_null: false,
      failed: false,
    });
  };
  const finished = (request: Request) => {
    const row = rows.get(request);
    if (row && !closed) row.finished = true;
  };
  const rejected = (request: Request) => {
    const row = rows.get(request);
    if (row && !closed) row.failed = true;
  };
  const response = (response: PWResponse) => {
    const row = rows.get(response.request());
    if (!row) return;
    const tail = (async () => {
      const id = await response.headerValue("x-request-id");
      const terminal = await response.finished();
      if (closed) {
        failed = true;
        return;
      }
      row.request_id = id;
      row.status = response.status();
      row.finished_null = terminal === null;
    })()
      .catch(() => {
        failed = true;
      })
      .then(() => {
        tails.delete(tail);
      });
    tails.add(tail);
  };
  const close = () => {
    failed = true;
    closed = true;
  };
  page.on("request", requested);
  page.on("requestfinished", finished);
  page.on("requestfailed", rejected);
  page.on("response", response);
  page.on("close", close);
  page.context().on("close", close);
  // Installed in the current document after login, before its first Knowledge
  // navigation. It does not instrument unrelated Account material.
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
  let report: any;
  return {
    async finish() {
      if (report) return report;
      await Promise.all([...tails]);
      const value = await page.evaluate(async () => {
        const host = window as any;
        const native = host.__knowledgeNative.finish();
        const publication = host.__knowledgePublication.finish();
        return {
          native: await native,
          publication: await publication,
        };
      });
      closed = true;
      page.off("request", requested);
      page.off("requestfinished", finished);
      page.off("requestfailed", rejected);
      page.off("response", response);
      page.off("close", close);
      page.context().off("close", close);
      report = {
        ...value,
        pw_failed: failed,
        pw_pending: tails.size,
        requests: [...rows.values()],
        input_hash: config.inputHash,
      };
      await writeFile(
        join(config.evidence, "knowledge-native-consumption.json"),
        JSON.stringify(report),
        { mode: 0o600 },
      );
      return report;
    },
    async idle() {
      return page.evaluate(() => {
        const host = window as any;
        return (
          host.__knowledgeNative.snapshot().pending === 0 &&
          host.__knowledgePublication.snapshot().pending === 0
        );
      });
    },
  };
}
