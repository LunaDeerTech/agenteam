import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
type Credential = { email: string; password: string; user_id: string };
type Provider = {
  id: string;
  input: {
    name: string;
    protocol: string;
    base_url: string;
    enabled: boolean;
    credential_ref: string | null;
    options: object;
  };
  version: string;
  created_at: string;
  updated_at: string;
};
type Capabilities = {
  tool_calls: boolean;
  parallel_tool_calls: boolean;
  streaming: boolean;
  reasoning: boolean;
  input_modalities: string[];
  output_modalities: string[];
  reasoning_efforts: string[];
  structured_output_modes: string[];
  context_length: string | null;
  max_output: string | null;
};
type Model = {
  id: string;
  provider_id: string;
  input: {
    name: string;
    provider_model_id: string;
    type: string;
    enabled: boolean;
    parameters: object;
    request_overwrite: object;
    header_overwrite: object;
    capabilities: Capabilities;
  };
  version: string;
  created_at: string;
  updated_at: string;
};
type Selection = {
  id: string;
  version: string;
  configured: null | {
    embedding: string;
    memory: string;
    reranker: string | null;
    image: string | null;
  };
};
type Receipt = {
  kind: string;
  resource_id: string;
  version: string;
  affected_references: string;
};
type Fact = {
  sequence: number;
  path: string;
  method: string;
  status: number;
  bytes: number;
  ended: boolean;
  code?: string;
  commit_state?: "not_started" | "not_committed" | "committed" | "unknown";
  receipt?: Receipt;
  providers?: { items: Provider[]; next_cursor: string | null };
  models?: { items: Model[]; next_cursor: string | null };
  provider?: Provider;
  model?: Model;
  selection?: Selection;
  session?: { user_id: string; session_id: string; role: string };
  found?: boolean;
};
type ObservedWindow = Window & {
  __selectionFacts?: Fact[];
  __selectionSequence?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  second_admin: Credential;
  selector_id: string;
  initial: Selection;
  ids: Record<string, string>;
  providers: Provider[];
  models: Model[];
} =>
  JSON.parse(readFileSync(join(directory, "selection-material.json"), "utf8"));
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "selection-result.json"),
    JSON.stringify({ completed: true, ...value }),
    { mode: 0o600 },
  );
}
let ipcSequence = 0;
async function ipc(
  action: string,
  fields: Record<string, string> = {},
): Promise<Record<string, any>> {
  const sequence = ++ipcSequence,
    request = join(directory, "selection-ipc.json"),
    ack = join(directory, `selection-ack-${sequence}.json`);
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence, action, ...fields }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  await expect.poll(() => existsSync(ack), { timeout: 14_000 }).toBe(true);
  const value = JSON.parse(readFileSync(ack, "utf8"));
  expect(value.ok === true && value.sequence === sequence).toBe(true);
  return value;
}
// A side observation of the application's exact native promise/reader. It never
// clones/tees, re-fetches, consumes an additional read, or wraps/delays cancel.
// Only closed safe projections survive; raw bounded bytes are cleared at EOF.
function observeNative() {
  const scope = window as ObservedWindow;
  scope.__selectionFacts = [];
  scope.__selectionSequence = 0;
  const object = (v: unknown): Record<string, unknown> | null =>
    v !== null && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : null;
  const closed = (v: Record<string, unknown>, keys: string[]) =>
    Object.keys(v).length === keys.length &&
    keys.every((k) => Object.hasOwn(v, k));
  const id = (v: unknown): v is string =>
    typeof v === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      v,
    );
  const integer = (v: unknown, zero = false): v is string =>
    typeof v === "string" &&
    (zero ? /^(0|[1-9][0-9]{0,18})$/ : /^[1-9][0-9]{0,18}$/).test(v) &&
    BigInt(v) <= 9223372036854775807n;
  const string = (v: unknown, max: number): v is string =>
    typeof v === "string" &&
    !/[\u0000\uD800-\uDFFF]/u.test(v) &&
    new TextEncoder().encode(v).length <= max;
  const instant = (v: unknown): v is string => {
    if (
      typeof v !== "string" ||
      !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$/.test(v)
    )
      return false;
    const y = Number(v.slice(0, 4)),
      m = Number(v.slice(5, 7)),
      d = Number(v.slice(8, 10));
    const days = [
      31,
      y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28,
      31,
      30,
      31,
      30,
      31,
      31,
      30,
      31,
      30,
      31,
    ];
    return (
      m >= 1 &&
      m <= 12 &&
      d >= 1 &&
      d <= days[m - 1]! &&
      Number(v.slice(11, 13)) < 24 &&
      Number(v.slice(14, 16)) < 60 &&
      Number(v.slice(17, 19)) < 60
    );
  };
  const empty = (v: unknown) => {
    const o = object(v);
    return !!o && Object.keys(o).length === 0;
  };
  const strings = (v: unknown, allowed: string[]): v is string[] =>
    Array.isArray(v) &&
    v.every((x) => typeof x === "string" && allowed.includes(x)) &&
    new Set(v).size === v.length;
  const protocols = [
    "openai-chat-completions",
    "anthropic-messages",
    "openai-embeddings",
    "jina-rerank",
    "openai-images-generations",
  ];
  function provider(v: unknown): Provider | null {
    const r = object(v),
      i = object(r?.input);
    if (
      !r ||
      !i ||
      !closed(r, ["id", "input", "version", "created_at", "updated_at"]) ||
      !closed(i, [
        "name",
        "protocol",
        "base_url",
        "enabled",
        "credential_ref",
        "options",
      ]) ||
      !id(r.id) ||
      !integer(r.version) ||
      !instant(r.created_at) ||
      !instant(r.updated_at) ||
      r.updated_at < r.created_at ||
      !string(i.name, 512) ||
      Array.from(i.name).length < 1 ||
      Array.from(i.name).length > 128 ||
      typeof i.protocol !== "string" ||
      !protocols.includes(i.protocol) ||
      !string(i.base_url, 8192) ||
      typeof i.enabled !== "boolean" ||
      !(i.credential_ref === null || id(i.credential_ref)) ||
      !empty(i.options)
    )
      return null;
    return {
      id: r.id,
      input: {
        name: i.name,
        protocol: i.protocol,
        base_url: i.base_url,
        enabled: i.enabled,
        credential_ref: i.credential_ref,
        options: {},
      },
      version: r.version,
      created_at: r.created_at,
      updated_at: r.updated_at,
    };
  }
  function model(v: unknown): Model | null {
    const r = object(v),
      i = object(r?.input),
      c = object(i?.capabilities);
    if (
      !r ||
      !i ||
      !c ||
      !closed(r, [
        "id",
        "provider_id",
        "input",
        "version",
        "created_at",
        "updated_at",
      ]) ||
      !closed(i, [
        "name",
        "provider_model_id",
        "type",
        "enabled",
        "parameters",
        "request_overwrite",
        "header_overwrite",
        "capabilities",
      ]) ||
      !closed(c, [
        "tool_calls",
        "parallel_tool_calls",
        "streaming",
        "reasoning",
        "input_modalities",
        "output_modalities",
        "reasoning_efforts",
        "structured_output_modes",
        "context_length",
        "max_output",
      ]) ||
      !id(r.id) ||
      !id(r.provider_id) ||
      !integer(r.version) ||
      !instant(r.created_at) ||
      !instant(r.updated_at) ||
      r.updated_at < r.created_at ||
      !string(i.name, 512) ||
      Array.from(i.name).length < 1 ||
      Array.from(i.name).length > 128 ||
      !string(i.provider_model_id, 256) ||
      !i.provider_model_id.length ||
      typeof i.type !== "string" ||
      !["chat", "embedding", "reranker", "image_generation"].includes(i.type) ||
      typeof i.enabled !== "boolean" ||
      !empty(i.parameters) ||
      !empty(i.request_overwrite) ||
      !empty(i.header_overwrite) ||
      ![c.tool_calls, c.parallel_tool_calls, c.streaming, c.reasoning].every(
        (x) => typeof x === "boolean",
      ) ||
      !strings(c.input_modalities, ["text", "image", "file", "vector"]) ||
      !strings(c.output_modalities, ["text", "image", "file", "vector"]) ||
      !strings(c.reasoning_efforts, []) ||
      !strings(c.structured_output_modes, ["text", "json_schema"]) ||
      !(c.context_length === null || integer(c.context_length)) ||
      !(c.max_output === null || integer(c.max_output)) ||
      (c.context_length !== null &&
        c.max_output !== null &&
        BigInt(c.max_output) > BigInt(c.context_length)) ||
      (c.parallel_tool_calls && !c.tool_calls)
    )
      return null;
    return {
      id: r.id,
      provider_id: r.provider_id,
      input: {
        name: i.name,
        provider_model_id: i.provider_model_id,
        type: i.type,
        enabled: i.enabled,
        parameters: {},
        request_overwrite: {},
        header_overwrite: {},
        capabilities: {
          tool_calls: c.tool_calls as boolean,
          parallel_tool_calls: c.parallel_tool_calls as boolean,
          streaming: c.streaming as boolean,
          reasoning: c.reasoning as boolean,
          input_modalities: [...c.input_modalities],
          output_modalities: [...c.output_modalities],
          reasoning_efforts: [],
          structured_output_modes: [...c.structured_output_modes],
          context_length: c.context_length,
          max_output: c.max_output,
        },
      },
      version: r.version,
      created_at: r.created_at,
      updated_at: r.updated_at,
    };
  }
  function selection(value: unknown): Selection | null {
    const r = object(value);
    if (
      !r ||
      !closed(r, ["id", "version", "configured"]) ||
      !id(r.id) ||
      !integer(r.version)
    )
      return null;
    if (r.configured === null)
      return { id: r.id, version: r.version, configured: null };
    const c = object(r.configured);
    if (
      !c ||
      !closed(c, ["embedding", "memory", "reranker", "image"]) ||
      !id(c.embedding) ||
      !id(c.memory) ||
      !(c.reranker === null || id(c.reranker)) ||
      !(c.image === null || id(c.image))
    )
      return null;
    return {
      id: r.id,
      version: r.version,
      configured: {
        embedding: c.embedding,
        memory: c.memory,
        reranker: c.reranker,
        image: c.image,
      },
    };
  }
  function receipt(v: unknown): Receipt | null {
    const r = object(v);
    return r &&
      closed(r, ["kind", "resource_id", "version", "affected_references"]) &&
      typeof r.kind === "string" &&
      r.kind === "model.selection.update" &&
      id(r.resource_id) &&
      integer(r.version) &&
      integer(r.affected_references, true) &&
      r.affected_references === "0"
      ? {
          kind: r.kind,
          resource_id: r.resource_id,
          version: r.version,
          affected_references: r.affected_references,
        }
      : null;
  }
  const nativeFetch = window.fetch;
  window.fetch = function (this: unknown, ...args) {
    const pending = Reflect.apply(nativeFetch, this, args) as Promise<Response>;
    try {
      const [input, init] = args,
        request = input instanceof Request ? input : null,
        url = new URL(request ? request.url : String(input), location.href),
        path = url.pathname,
        method = init?.method ?? request?.method ?? "GET";
      const providers = path === "/api/v1/system/model-providers",
        providerID =
          path.startsWith("/api/v1/system/model-providers/") &&
          id(path.slice("/api/v1/system/model-providers/".length)),
        models = path === "/api/v1/system/models",
        modelID =
          path.startsWith("/api/v1/system/models/") &&
          id(path.slice("/api/v1/system/models/".length)),
        selector = path === "/api/v1/system/model-selection",
        session = path === "/api/v1/session",
        lookup = path === "/api/v1/system/model-commands/lookup";
      const watched =
        (method === "GET" &&
          (providers ||
            providerID ||
            models ||
            modelID ||
            selector ||
            session)) ||
        (method === "PUT" && selector) ||
        (method === "POST" && lookup);
      const allowed =
          method === "GET" && providers
            ? ["limit", "cursor"]
            : method === "GET" && models
              ? ["limit", "provider_id", "cursor"]
              : [],
        keys = [...url.searchParams.keys()];
      if (
        url.origin !== location.origin ||
        !watched ||
        keys.some((k) => !allowed.includes(k)) ||
        new Set(keys).size !== keys.length
      )
        return pending;
      const fact: Fact = {
        sequence: ++scope.__selectionSequence!,
        path: path + url.search,
        method,
        status: 0,
        bytes: 0,
        ended: false,
      };
      scope.__selectionFacts!.push(fact);
      void pending
        .then(
          (response) => {
            fact.status = response.status;
            const body = response.body;
            if (!body) {
              fact.ended = true;
              return;
            }
            const max =
              providers &&
              method === "GET" &&
              response.status === 200 &&
              response.headers
                .get("Content-Type")
                ?.split(";", 1)[0]
                ?.trim()
                .toLowerCase() === "application/json"
                ? 2097152
                : 600000;
            let text = "",
              collect = true;
            const decoder = new TextDecoder();
            function project() {
              if (!collect) return;
              const r = object(JSON.parse(text + decoder.decode()));
              if (!r) return;
              if (response.status !== 200) {
                if (
                  typeof r.code === "string" &&
                  [
                    "UNAUTHENTICATED",
                    "SESSION_REVOKED",
                    "CSRF_FAILED",
                    "FORBIDDEN",
                    "CURSOR_INVALID",
                    "VERSION_CONFLICT",
                    "INVALID_ARGUMENT",
                    "INVALID_STATE",
                    "NOT_FOUND",
                    "RESOURCE_BUSY",
                    "INTERNAL_ERROR",
                    "DEPENDENCY_UNBOUND",
                    "CAPABILITY_UNSUPPORTED",
                    "IDEMPOTENCY_KEY_REUSED",
                  ].includes(r.code)
                )
                  fact.code = r.code;
                if (
                  r.commit_state === "not_started" ||
                  r.commit_state === "not_committed" ||
                  r.commit_state === "committed" ||
                  r.commit_state === "unknown"
                )
                  fact.commit_state = r.commit_state;
                return;
              }
              if (session) {
                const u = object(r.user),
                  s = object(r.session);
                if (
                  id(u?.id) &&
                  id(s?.id) &&
                  (u?.role === "admin" || u?.role === "user")
                )
                  fact.session = {
                    user_id: u.id,
                    session_id: s.id,
                    role: u.role,
                  };
                return;
              }
              if (lookup) {
                if (!closed(r, ["found", "receipt"])) return;
                if (r.found === false && r.receipt === null) fact.found = false;
                else if (r.found === true) {
                  const value = receipt(r.receipt);
                  if (value) {
                    fact.found = true;
                    fact.receipt = value;
                  }
                }
                return;
              }
              if (method !== "GET") {
                const value = receipt(r);
                if (value) fact.receipt = value;
                return;
              }
              if (selector) {
                const value = selection(r);
                if (value) fact.selection = value;
                return;
              }
              if (providerID) {
                const value = provider(r);
                if (value) fact.provider = value;
                return;
              }
              if (modelID) {
                const value = model(r);
                if (value) fact.model = value;
                return;
              }
              if (
                !closed(r, ["items", "next_cursor"]) ||
                !Array.isArray(r.items) ||
                r.items.length > 25 ||
                !(
                  r.next_cursor === null ||
                  (string(r.next_cursor, 8192) &&
                    r.next_cursor.length > 0 &&
                    r.items.length === 25)
                )
              )
                return;
              const rows = providers
                ? r.items.map(provider)
                : r.items.map(model);
              if (rows.some((x) => x === null)) return;
              for (let n = 1; n < rows.length; n++) {
                const a = rows[n - 1]!,
                  b = rows[n]!;
                if (
                  a.created_at < b.created_at ||
                  (a.created_at === b.created_at && a.id <= b.id)
                )
                  return;
              }
              if (providers)
                fact.providers = {
                  items: rows as Provider[],
                  next_cursor: r.next_cursor,
                };
              else
                fact.models = {
                  items: rows as Model[],
                  next_cursor: r.next_cursor,
                };
            }
            try {
              const nativeGet = body.getReader;
              Object.defineProperty(body, "getReader", {
                configurable: true,
                writable: true,
                value: function (
                  this: ReadableStream<Uint8Array>,
                  ...getArgs: unknown[]
                ) {
                  const reader = Reflect.apply(
                    nativeGet,
                    this,
                    getArgs,
                  ) as ReadableStreamDefaultReader<Uint8Array>;
                  Object.defineProperty(body, "getReader", {
                    value: nativeGet,
                  });
                  const nativeRead = reader.read;
                  Object.defineProperty(reader, "read", {
                    configurable: true,
                    writable: true,
                    value: function (
                      this: ReadableStreamDefaultReader<Uint8Array>,
                      ...readArgs: unknown[]
                    ) {
                      const original = Reflect.apply(
                        nativeRead,
                        this,
                        readArgs,
                      ) as Promise<ReadableStreamReadResult<Uint8Array>>;
                      void original
                        .then(
                          ({ done, value }) => {
                            try {
                              if (value) {
                                fact.bytes += value.byteLength;
                                if (fact.bytes > max) {
                                  collect = false;
                                  text = "";
                                } else if (collect)
                                  text += decoder.decode(value, {
                                    stream: true,
                                  });
                              }
                              if (done) {
                                fact.ended = true;
                                try {
                                  project();
                                } finally {
                                  text = "";
                                  collect = false;
                                }
                              }
                            } catch {
                              text = "";
                              collect = false;
                            }
                          },
                          () => {
                            text = "";
                            collect = false;
                            fact.ended = true;
                          },
                        )
                        .catch(() => {
                          text = "";
                          collect = false;
                        });
                      return original;
                    },
                  });
                  return reader;
                },
              });
            } catch {
              text = "";
              collect = false;
            }
          },
          () => {
            fact.ended = true;
          },
        )
        .catch(() => {});
    } catch {
      /* Side observation cannot change fetch or native reader results. */
    }
    return pending;
  };
}
const endpoint = "/api/v1/system/model-selection";
const purposeLabels = {
  embedding: "Embedding",
  memory: "Memory",
  reranker: "Reranker",
  image: "Image Generation",
} as const;
type Purpose = keyof typeof purposeLabels;
const top = (page: Page) => page.getByRole("dialog").last();
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const title = (page: Page, name: string) =>
  page.getByRole("heading", { name, exact: true });
const draftRow = (page: Page, purpose: Purpose) =>
  top(page)
    .locator(".draft-purposes > li")
    .filter({
      has: page.getByRole("heading", {
        name: new RegExp("^" + purposeLabels[purpose] + " "),
      }),
    });
const savedRow = (page: Page, purpose: Purpose) =>
  page
    .locator(".current-references > li")
    .filter({ has: title(page, purposeLabels[purpose]) });
async function facts(page: Page) {
  return page.evaluate(() => (window as ObservedWindow).__selectionFacts ?? []);
}
async function seq(page: Page) {
  return page.evaluate(
    () => (window as ObservedWindow).__selectionSequence ?? 0,
  );
}
async function writes(page: Page) {
  return (await facts(page)).filter(
    (f) => f.path === endpoint && f.method === "PUT",
  );
}
async function factAfter(
  page: Page,
  after: number,
  predicate: (f: Fact) => boolean,
) {
  let found: Fact | undefined;
  await expect
    .poll(async () => {
      found = (await facts(page)).find(
        (f) => f.sequence > after && predicate(f),
      );
      return !!found;
    })
    .toBe(true);
  return found!;
}
async function session(page: Page) {
  let value: Fact["session"];
  await expect
    .poll(async () => {
      value = (await facts(page))
        .filter(
          (f) => f.path === "/api/v1/session" && f.status === 200 && f.ended,
        )
        .at(-1)?.session;
      return !!value;
    })
    .toBe(true);
  return value!;
}
async function privateFill(input: Locator, value: string) {
  try {
    await input.fill(value);
  } catch {
    throw new Error("private Selection login input failed");
  }
}
async function login(page: Page, who: "admin" | "member" | "second_admin") {
  const after = await seq(page),
    data = material()[who];
  await page.locator("#login-email").fill(data.email);
  await privateFill(page.locator("#login-password"), data.password);
  await button(page, "登录").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === data.user_id &&
      f.session.role === (who === "member" ? "user" : "admin"),
  );
  await expect(button(page, "退出登录")).toBeEnabled();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
async function setup(
  page: Page,
  who: "admin" | "member" | "second_admin" = "admin",
  path = "/system/model-selection",
) {
  await page.addInitScript(observeNative);
  await page.goto(path);
  await login(page, who);
  await expect(page).toHaveURL(new RegExp(path + "$"));
  if (who !== "member" && path === "/system/model-selection")
    await currentRead(page, 0);
}
async function currentRead(page: Page, after: number, failedProvider?: string) {
  const observed = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.selection,
  );
  if (observed.selection!.configured)
    for (const target of new Set(
      Object.values(observed.selection!.configured).filter(
        (x): x is string => x !== null,
      ),
    )) {
      const detail = await factAfter(
        page,
        observed.sequence,
        (f) =>
          f.path === "/api/v1/system/models/" + target &&
          f.method === "GET" &&
          f.status === 200 &&
          f.ended &&
          !!f.model,
      );
      await factAfter(
        page,
        detail.sequence,
        (f) =>
          f.path ===
            "/api/v1/system/model-providers/" + detail.model!.provider_id &&
          f.method === "GET" &&
          (detail.model!.provider_id === failedProvider
            ? f.status === 503
            : f.status === 200 && f.ended && !!f.provider),
      );
    }
  await expect(button(page, "刷新配置")).toBeEnabled();
  return observed.selection!;
}
async function refresh(page: Page, failedProvider?: string) {
  const after = await seq(page);
  await button(page, "刷新配置").click();
  return currentRead(page, after, failedProvider);
}
async function openEditor(page: Page) {
  const open = page
    .locator(".selection-actions")
    .getByRole("button", { name: /^(编辑用途|配置用途|配置已保存)$/ });
  await expect(open).toBeEnabled();
  await open.click();
  await expect(top(page)).toHaveAccessibleName("配置平台模型用途");
}
const providerName = (target: string) =>
  material().providers.find((p) => p.id === target)!.input.name;
const modelName = (target: string) =>
  material().models.find((m) => m.id === target)!.input.name;
async function browse(page: Page, purpose: Purpose, providerID?: string) {
  const after = await seq(page);
  await top(page)
    .getByRole("button", {
      name: "选择 " + purposeLabels[purpose],
      exact: true,
    })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.method === "GET" &&
      f.path.startsWith("/api/v1/system/model-providers?") &&
      f.status === 200 &&
      f.ended &&
      !!f.providers,
  );
  if (providerID) await browseProvider(page, providerID);
}
async function browseProvider(page: Page, target: string) {
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "浏览 " + providerName(target), exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.method === "GET" &&
      f.path.startsWith("/api/v1/system/models?") &&
      new URL(f.path, "http://owned.invalid").searchParams.get(
        "provider_id",
      ) === target &&
      f.status === 200 &&
      f.ended &&
      !!f.models,
  );
}
async function choose(page: Page, purpose: Purpose, target: string) {
  const record = material().models.find((m) => m.id === target)!;
  await browse(page, purpose, record.provider_id);
  await top(page)
    .getByRole("button", { name: "选择 " + record.input.name, exact: true })
    .click();
  await expect(top(page).locator(".candidate-picker")).toHaveCount(0);
  await expect(draftRow(page, purpose)).toContainText(target);
}
async function clearOptional(page: Page, purpose: "reranker" | "image") {
  await top(page)
    .getByRole("button", {
      name: "不配置 " + purposeLabels[purpose],
      exact: true,
    })
    .click();
}
async function save(page: Page, read = true) {
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  const confirmation = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !!f.receipt,
  );
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".system-model-selection")).toContainText(
    "平台模型用途配置已保存",
  );
  if (read) await currentRead(page, confirmation.sequence);
  return confirmation.receipt!;
}
async function discard(page: Page) {
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}
async function dbFacts() {
  return (await ipc("facts")).facts as {
    selection: Selection;
    references: { role: string; model_id: string; version: string }[];
    commands: number;
    audits: number;
    events: number;
    embedding_events: number;
    deliveries: number;
    invocations: number;
    distinct_keys: number;
    actors: number;
  };
}
function canonical(value: unknown): string {
  if (Array.isArray(value)) return "[" + value.map(canonical).join(",") + "]";
  if (value && typeof value === "object")
    return (
      "{" +
      Object.entries(value)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([k, v]) => JSON.stringify(k) + ":" + canonical(v))
        .join(",") +
      "}"
    );
  return JSON.stringify(value);
}
function referenceFacts(value: Awaited<ReturnType<typeof dbFacts>>) {
  const expected = value.selection.configured
    ? Object.entries(value.selection.configured)
        .filter(([, target]) => target !== null)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([role, model_id]) => ({
          role,
          model_id,
          version: value.selection.version,
        }))
    : [];
  expect(value.references).toEqual(expected);
  expect(value.commands).toBe(value.audits);
  expect(value.deliveries).toBe(0);
  expect(value.invocations).toBe(0);
}
async function focusContained(page: Page) {
  await expect
    .poll(() => top(page).evaluate((n) => n.contains(document.activeElement)))
    .toBe(true);
  for (const key of ["Tab", "Shift+Tab", "Shift+Tab", "Tab"]) {
    await page.keyboard.press(key);
    expect(
      await top(page).evaluate((n) => n.contains(document.activeElement)),
    ).toBe(true);
  }
}

test("[lifecycle] null/v1, four explicit purposes and one complete versioned configuration", async ({
  page,
}) => {
  await setup(page);
  const data = material(),
    before = await dbFacts();
  expect(before.selection).toEqual(data.initial);
  expect(before.selection.configured).toBeNull();
  expect(before.selection.version).toBe("1");
  await expect(title(page, "尚未配置平台模型用途")).toBeVisible();
  await openEditor(page);
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  await choose(page, "embedding", data.ids.embedding!);
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  await browse(page, "memory", data.ids.memory_provider!);
  await expect(
    top(page).getByRole("button", {
      name: "选择 " + modelName(data.ids.no_schema!),
      exact: true,
    }),
  ).toBeDisabled();
  await expect(
    top(page).getByRole("button", {
      name: "选择 " + modelName(data.ids.disabled!),
      exact: true,
    }),
  ).toBeDisabled();
  await top(page)
    .getByRole("button", {
      name: "选择 " + modelName(data.ids.memory!),
      exact: true,
    })
    .click();
  await choose(page, "reranker", data.ids.reranker!);
  await choose(page, "image", data.ids.image!);
  expect(
    await top(page)
      .getByRole("button", { name: /^不配置 (Embedding|Memory)$/ })
      .count(),
  ).toBe(0);
  const first = await save(page);
  expect(first.version).toBe("2");
  expect((await writes(page)).length).toBe(1);
  let current = await dbFacts();
  expect(current.selection.configured).toEqual({
    embedding: data.ids.embedding,
    memory: data.ids.memory,
    reranker: data.ids.reranker,
    image: data.ids.image,
  });
  referenceFacts(current);
  expect(current.commands - before.commands).toBe(1);
  expect(current.events - before.events).toBe(1);
  expect(current.embedding_events - before.embedding_events).toBe(1);
  await openEditor(page);
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  expect((await writes(page)).length).toBe(1);
  await openEditor(page);
  await clearOptional(page, "reranker");
  await discard(page);
  expect((await writes(page)).length).toBe(1);
  expect(canonical(await dbFacts())).toBe(canonical(current));
  await openEditor(page);
  await clearOptional(page, "reranker");
  await clearOptional(page, "image");
  const second = await save(page);
  expect(second.version).toBe("3");
  current = await dbFacts();
  expect(current.selection.configured).toEqual({
    embedding: data.ids.embedding,
    memory: data.ids.memory,
    reranker: null,
    image: null,
  });
  referenceFacts(current);
  expect(await refresh(page)).toEqual(current.selection);
  expect((await writes(page)).length).toBe(2);
  await expect(page.locator(".system-model-selection")).not.toContainText(
    "连接成功",
  );
  result({
    null_v1: true,
    four_purposes: true,
    optional_null: true,
    no_op: true,
    cancelled: true,
    read_back: true,
    no_invocation: true,
  });
});

test("[read] bounded saved pairs, formal pages, exact failures and independent late choices", async ({
  page,
}) => {
  await setup(page);
  const data = material(),
    initial = await dbFacts();
  const initialFacts = await facts(page),
    detailFacts = initialFacts.filter((f) =>
      /^\/api\/v1\/system\/(models|model-providers)\/[^/]+$/.test(f.path),
    );
  expect(detailFacts.filter((f) => f.path.includes("/models/")).length).toBe(4);
  expect(
    detailFacts.filter((f) => f.path.includes("/model-providers/")).length,
  ).toBe(4);
  expect(new Set(detailFacts.map((f) => f.path)).size).toBe(8);
  expect(initialFacts.filter((f) => f.path.includes("?")).length).toBe(0);
  await openEditor(page);
  await browse(page, "memory");
  const providerPage = () =>
      top(page).locator('[aria-label="当前 Provider 候选页"] > li'),
    modelPage = () =>
      top(page).locator('[aria-label="当前 Model 候选页"] > li');
  const pagination = (kind: "Provider" | "Model") =>
    top(page).getByRole("navigation", {
      name: kind + " 候选分页",
      exact: true,
    });
  await expect(providerPage()).toHaveCount(25);
  let latest = (await facts(page)).filter((f) => f.providers).at(-1)!;
  expect(canonical(latest.providers!.items)).toBe(
    canonical(data.providers.slice(0, 25)),
  );
  let after = await seq(page);
  await pagination("Provider")
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  latest = await factAfter(page, after, (f) => !!f.providers && f.ended);
  expect(canonical(latest.providers!.items)).toBe(
    canonical(data.providers.slice(25)),
  );
  await expect(providerPage()).toHaveCount(data.providers.length - 25);
  await pagination("Provider")
    .getByRole("button", { name: "上一页", exact: true })
    .click();
  await expect(providerPage()).toHaveCount(25);
  await browseProvider(page, data.ids.memory_provider!);
  await expect(modelPage()).toHaveCount(25);
  const expected = data.models.filter(
    (m) => m.provider_id === data.ids.memory_provider,
  );
  latest = (await facts(page)).filter((f) => f.models).at(-1)!;
  expect(canonical(latest.models!.items)).toBe(
    canonical(expected.slice(0, 25)),
  );
  await pagination("Model")
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await expect(modelPage()).toHaveCount(1);
  await pagination("Model")
    .getByRole("button", { name: "上一页", exact: true })
    .click();
  await expect(modelPage()).toHaveCount(25);
  let altered = false;
  await page.route("**/api/v1/system/models?*", async (route) => {
    const url = new URL(route.request().url());
    if (!altered && url.searchParams.has("cursor")) {
      altered = true;
      url.searchParams.set("cursor", "owned-invalid-cursor");
      await route.continue({ url: url.toString() });
    } else await route.continue();
  });
  after = await seq(page);
  await pagination("Model")
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await factAfter(page, after, (f) => f.code === "CURSOR_INVALID" && f.ended);
  await expect(modelPage()).toHaveCount(0);
  expect(
    (await facts(page)).filter(
      (f) => f.sequence > after && f.path.startsWith("/api/v1/system/models?"),
    ).length,
  ).toBe(1);
  await page.unroute("**/api/v1/system/models?*");
  await top(page)
    .getByRole("button", { name: "返回候选首页", exact: true })
    .click();
  await expect(modelPage()).toHaveCount(25);
  await top(page)
    .getByRole("button", { name: "返回 Provider 列表", exact: true })
    .click();
  await expect(providerPage()).toHaveCount(25);
  await browseProvider(page, data.ids.empty_provider!);
  await expect(title(page, "当前 Provider 的这一页没有 Model")).toBeVisible();
  for (const purpose of ["embedding", "reranker", "image"] as const) {
    await browse(page, purpose, data.ids[purpose + "_provider"]!);
    await expect(modelPage()).toHaveCount(1);
    await expect(
      pagination("Model").getByRole("button", { name: "上一页", exact: true }),
    ).toBeDisabled();
  }
  await browse(page, "memory");
  await ipc("hold-get", { id: data.ids.memory_provider! });
  await top(page)
    .getByRole("button", {
      name: "浏览 " + providerName(data.ids.memory_provider!),
      exact: true,
    })
    .click();
  await expect.poll(async () => (await ipc("hold-facts")).started).toBe(true);
  after = await seq(page);
  await browse(page, "image");
  await ipc("release-get");
  await expect.poll(async () => (await ipc("hold-facts")).finished).toBe(true);
  await expect(title(page, "选择用途：Image Generation")).toBeVisible();
  await expect(modelPage()).toHaveCount(0);
  expect(
    (await facts(page)).filter(
      (f) => f.sequence > after && f.path.startsWith("/api/v1/system/models?"),
    ).length,
  ).toBe(0);
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await ipc("arm-get-fail", { id: data.ids.embedding_provider! });
  after = await seq(page);
  const failed = await refresh(page, data.ids.embedding_provider!);
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  expect(failed).toEqual(initial.selection);
  await expect(savedRow(page, "embedding")).toContainText(data.ids.embedding!);
  await expect(savedRow(page, "embedding")).toContainText(
    "读取失败，尚未获得完整的当前详情",
  );
  await expect(savedRow(page, "embedding")).not.toContainText(
    modelName(data.ids.embedding!),
  );
  await expect(title(page, "尚未配置平台模型用途")).toHaveCount(0);
  const bounded = (await facts(page)).filter(
    (f) =>
      f.sequence > after &&
      /^\/api\/v1\/system\/(models|model-providers)\//.test(f.path),
  );
  expect(bounded.length).toBe(8);
  await openEditor(page);
  await clearOptional(page, "reranker");
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  expect((await writes(page)).length).toBe(0);
  await top(page)
    .getByRole("button", { name: "重读 Embedding", exact: true })
    .click();
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeEnabled();
  await discard(page);
  for (const [action, key, purpose, reason] of [
    ["disable-model", "embedding", "embedding", "Model 已停用"],
    ["remove-schema", "memory", "memory", "Memory 需要声明 json_schema 能力"],
    ["disable-provider", "image_provider", "image", "Provider 已停用"],
  ] as const) {
    await ipc(action, { id: data.ids[key]! });
    await refresh(page);
    await expect(savedRow(page, purpose)).toContainText(reason);
    await expect(savedRow(page, purpose)).toContainText(data.ids[purpose]!);
  }
  await openEditor(page);
  await clearOptional(page, "reranker");
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  await discard(page);
  expect((await writes(page)).length).toBe(0);
  const current = await dbFacts();
  expect(current.selection).toEqual(initial.selection);
  referenceFacts(current);
  result({
    four_plus_four: true,
    canonical_pages: true,
    independent_pages: true,
    empty: true,
    bad_cursor: true,
    late_read: true,
    no_half_pair: true,
    invalid_bindings: true,
    providers: data.providers.length,
    models: expected.length,
  });
});
async function rejected(page: Page, code: string, status: number) {
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  const failure = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.code === code &&
      f.status === status &&
      f.ended,
  );
  expect(failure.receipt).toBeUndefined();
  await expect(
    top(page).getByRole("button", { name: "读取最新配置并核对", exact: true }),
  ).toBeEnabled();
  return failure;
}

test("[concurrency] two formal administrators and current reference transactions", async ({
  page,
  browser,
}) => {
  await setup(page);
  const data = material(),
    baseline = await dbFacts();
  const otherContext = await browser.newContext({
    baseURL: process.env.AGENTEAM_AUTH_WEB_ORIGIN,
  });
  try {
    const other = await otherContext.newPage();
    await setup(other, "second_admin");
    await openEditor(page);
    await openEditor(other);
    await expect(top(page)).toContainText(
      "核对版本：" + baseline.selection.version,
    );
    await expect(top(other)).toContainText(
      "核对版本：" + baseline.selection.version,
    );
    await clearOptional(page, "reranker");
    await clearOptional(other, "image");
    const attempts = await ipc("attempt-facts");
    await save(page);
    const winner = await dbFacts();
    await rejected(other, "VERSION_CONFLICT", 409);
    await expect(draftRow(other, "image")).toContainText("不配置");
    expect(canonical(await dbFacts())).toBe(canonical(winner));
    const after = await ipc("attempt-facts");
    expect(after.attempts - attempts.attempts).toBe(2);
    expect(after.distinct_keys - attempts.distinct_keys).toBe(2);
    expect(winner.commands - baseline.commands).toBe(1);
    expect(winner.audits - baseline.audits).toBe(1);
    expect(winner.events - baseline.events).toBe(1);
    referenceFacts(winner);
  } finally {
    await otherContext.close();
  }
  for (const item of [
    {
      action: "disable-model",
      restore: "enable-model",
      key: "memory_replacement",
      target: "memory_replacement",
      purpose: "memory",
      code: "INVALID_ARGUMENT",
      status: 400,
    },
    {
      action: "remove-schema",
      restore: "restore-schema",
      key: "memory_replacement",
      target: "memory_replacement",
      purpose: "memory",
      code: "CAPABILITY_UNSUPPORTED",
      status: 422,
    },
    {
      action: "disable-provider",
      restore: "enable-provider",
      key: "memory_replacement_provider",
      target: "memory_replacement",
      purpose: "memory",
      code: "INVALID_STATE",
      status: 409,
    },
    {
      action: "delete",
      restore: null,
      key: "doomed",
      target: "doomed",
      purpose: "embedding",
      code: "NOT_FOUND",
      status: 404,
    },
  ] as const) {
    await openEditor(page);
    await choose(page, item.purpose, data.ids[item.target]!);
    await expect(
      top(page).getByRole("button", { name: "保存用途配置", exact: true }),
    ).toBeEnabled();
    await ipc(item.action, { id: data.ids[item.key]! });
    const before = await dbFacts();
    await rejected(page, item.code, item.status);
    await expect(draftRow(page, item.purpose)).toContainText(
      data.ids[item.target]!,
    );
    expect(canonical(await dbFacts())).toBe(canonical(before));
    await discard(page);
    if (item.restore) await ipc(item.restore, { id: data.ids[item.key]! });
    await refresh(page);
  }
  for (const [purpose, replacement] of [
    ["embedding", data.ids.embedding_replacement],
    ["image", undefined],
  ] as const) {
    await openEditor(page);
    await choose(page, "memory", data.ids.memory_replacement!);
    const old = await dbFacts();
    await ipc("delete", {
      id: data.ids[purpose]!,
      ...(replacement ? { replacement } : {}),
    });
    const changed = await dbFacts();
    expect(changed.selection.version).toBe(
      String(BigInt(old.selection.version) + 1n),
    );
    expect(changed.selection.configured![purpose]).toBe(replacement ?? null);
    referenceFacts(changed);
    expect(changed.commands).toBe(old.commands);
    expect(changed.audits).toBe(old.audits);
    expect(changed.events - old.events).toBe(1);
    await rejected(page, "VERSION_CONFLICT", 409);
    expect(canonical(await dbFacts())).toBe(canonical(changed));
    await discard(page);
    expect(await refresh(page)).toEqual(changed.selection);
  }
  result({
    two_admins: true,
    same_version: true,
    atomic_rejection: true,
    required_replacement: true,
    optional_clear: true,
    old_version_rejected: true,
  });
});

async function cutSave(page: Page) {
  await ipc("arm-drop");
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !f.receipt,
  );
  await expect(top(page)).toContainText("保存结果未确认");
  await expect(
    top(page).getByRole("button", { name: "检查原请求", exact: true }),
  ).toBeEnabled();
}

test("[outcome] accepted loss, historical lookup and explicit original replay after formal deletion", async ({
  page,
}) => {
  await setup(page);
  const data = material(),
    baseline = await dbFacts();
  await openEditor(page);
  await choose(page, "memory", data.ids.memory_replacement!);
  await cutSave(page);
  const lost = await ipc("drop-facts"),
    accepted = await dbFacts();
  expect(lost.dropped).toBe(1);
  expect(lost.replayed).toBe(0);
  expect(accepted.commands - baseline.commands).toBe(1);
  expect(accepted.audits - baseline.audits).toBe(1);
  expect(accepted.events - baseline.events).toBe(1);
  referenceFacts(accepted);
  await ipc("delete", {
    id: data.ids.memory_replacement!,
    replacement: data.ids.memory!,
  });
  expect(
    (await ipc("missing", { id: data.ids.memory_replacement! })).missing,
  ).toBe(true);
  const advanced = await dbFacts();
  expect(advanced.selection.version).toBe(
    String(BigInt(accepted.selection.version) + 1n),
  );
  expect(advanced.selection.configured!.memory).toBe(data.ids.memory);
  let after = await seq(page);
  await top(page)
    .getByRole("button", { name: "检查原请求", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path.endsWith("/model-commands/lookup") &&
      f.status === 200 &&
      f.ended &&
      f.found === true,
  );
  await expect(top(page)).toContainText("找到记录");
  await expect(top(page)).toContainText("不会代替原请求的执行回执");
  expect((await writes(page)).length).toBe(1);
  expect((await ipc("drop-facts")).replayed).toBe(0);
  expect(canonical(await dbFacts())).toBe(canonical(advanced));
  await expect(
    top(page).getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  await ipc("arm-get-fail");
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "重试原请求", exact: true })
    .click();
  const replay = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !!f.receipt,
  );
  expect(replay.receipt).toEqual(lost.receipt);
  await factAfter(
    page,
    replay.sequence,
    (f) => f.path === endpoint && f.method === "GET" && f.status === 503,
  );
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".system-model-selection")).toContainText(
    "已保存，当前配置读取失败",
  );
  await expect(button(page, "重新读取配置")).toBeEnabled();
  const replayFacts = await ipc("drop-facts");
  expect(replayFacts.replayed).toBe(1);
  expect(replayFacts.same_original).toBe(true);
  expect(canonical(await dbFacts())).toBe(canonical(advanced));
  after = await seq(page);
  await button(page, "重新读取配置").click();
  expect(await currentRead(page, after)).toEqual(advanced.selection);
  expect((await writes(page)).length).toBe(2);
  await openEditor(page);
  await clearOptional(page, "image");
  await cutSave(page);
  const abandoned = await dbFacts();
  await top(page)
    .getByRole("button", { name: "放弃原请求", exact: true })
    .click();
  await expect(top(page)).toHaveAccessibleName("放弃本次请求的追踪？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(canonical(await dbFacts())).toBe(canonical(abandoned));
  expect((await ipc("drop-facts")).replayed).toBe(0);
  expect(await refresh(page)).toEqual(abandoned.selection);
  referenceFacts(await dbFacts());
  result({
    accepted_cut: true,
    lookup_only: true,
    old_404: true,
    same_original: true,
    unique_facts: true,
    confirmed_read_failure: true,
    abandoned: true,
  });
});

test("[authority] current formal Session and administrator checks on GET, PUT and lookup", async ({
  page,
}) => {
  await setup(page, "member");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "admin");
  await page.goto("/system/model-selection");
  await currentRead(page, 0);
  let original = await session(page);
  await ipc("demote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  let after = await seq(page);
  await button(page, "刷新配置").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "GET" &&
      f.status === 403 &&
      f.code === "FORBIDDEN" &&
      f.ended,
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(page.locator(".current-references")).toHaveCount(0);
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  after = await seq(page);
  await button(page, "重新检查权限").click();
  await currentRead(page, after);
  await openEditor(page);
  await clearOptional(page, "image");
  original = await session(page);
  await ipc("demote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  const before = await dbFacts();
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 403 &&
      f.code === "FORBIDDEN" &&
      f.ended,
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  expect(canonical(await dbFacts())).toBe(canonical(before));
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  after = await seq(page);
  await button(page, "重新检查权限").click();
  await currentRead(page, after);
  await openEditor(page);
  await clearOptional(page, "image");
  await cutSave(page);
  original = await session(page);
  let intercepted = false;
  await page.route("**/api/v1/system/model-commands/lookup", async (route) => {
    if (!intercepted) {
      intercepted = true;
      await ipc("demote", {
        user_id: original.user_id,
        session_id: original.session_id,
      });
    }
    await route.continue();
  });
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "检查原请求", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path.endsWith("/model-commands/lookup") &&
      f.status === 403 &&
      f.code === "FORBIDDEN" &&
      f.ended,
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  await page.unroute("**/api/v1/system/model-commands/lookup");
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  after = await seq(page);
  await button(page, "重新检查权限").click();
  await currentRead(page, after);
  await openEditor(page);
  await clearOptional(page, "reranker");
  await cutSave(page);
  original = await session(page);
  await ipc("revoke-session", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "检查原请求", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 401 &&
      f.code === "SESSION_REVOKED" &&
      f.ended,
  );
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  await login(page, "admin");
  await page.goto("/system/model-selection");
  await currentRead(page, 0);
  const fresh = await session(page);
  expect(
    fresh.user_id === original.user_id &&
      fresh.session_id !== original.session_id,
  ).toBe(true);
  await openEditor(page);
  await expect(
    top(page).getByRole("button", { name: "重试原请求", exact: true }),
  ).toHaveCount(0);
  await expect(
    top(page).getByRole("button", { name: "保存用途配置", exact: true }),
  ).toBeDisabled();
  await choose(page, "embedding", material().ids.embedding_replacement!);
  await cutSave(page);
  await discard(page);
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/model-selection");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  result({
    denied: true,
    get_forbidden: true,
    put_forbidden: true,
    lookup_forbidden: true,
    revoked: true,
    new_session: true,
    switched: true,
  });
});

async function checkingConfirmation(
  page: Page,
  fail: boolean,
  business: string,
  decision: "continue" | "discard" = "continue",
  heldProvider?: string,
) {
  const before = await session(page),
    after = await seq(page),
    count = (await writes(page)).length;
  if (fail) await ipc("arm-session-fail");
  // This is an explicit synthetic pageshow; all subsequent Session 200s and EOFs
  // are the genuine backend, with only one owned bounded 503 when requested.
  const checking = await page.evaluate(async () => {
    let observer: MutationObserver | undefined;
    try {
      return await new Promise<{ zero: boolean; unlocked: boolean }>(
        (resolve) => {
          observer = new MutationObserver(() => {
            if (!document.querySelector(".session-check")) return;
            resolve({
              zero: document.querySelectorAll('[role="dialog"]').length === 0,
              unlocked: document.body.style.overflow !== "hidden",
            });
          });
          observer.observe(document.body, { childList: true, subtree: true });
          window.dispatchEvent(new Event("pageshow"));
        },
      );
    } finally {
      observer?.disconnect();
    }
  });
  expect(checking).toEqual({ zero: true, unlocked: true });
  if (fail) {
    await expect(page.locator('[role="dialog"]')).toHaveCount(0);
    const restore = button(page, "检查当前会话");
    await expect(restore).toBeEnabled();
    expect(await restore.evaluate((n) => n.closest("[inert]") === null)).toBe(
      true,
    );
    await restore.click();
  }
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === before.user_id &&
      f.session.session_id === before.session_id &&
      f.session.role === before.role,
  );
  const physical = page.locator('[role="dialog"]');
  await expect(physical).toHaveCount(2);
  await expect(page.getByRole("dialog")).toHaveCount(1);
  expect(
    await physical.evaluateAll((nodes, business) => {
      const [lower, upper] = nodes as HTMLElement[];
      return (
        lower?.inert &&
        lower.getAttribute("aria-hidden") === "true" &&
        lower.querySelector("h2")?.textContent === business &&
        upper?.inert === false &&
        upper.getAttribute("aria-hidden") !== "true" &&
        upper.querySelector("h2")?.textContent === "放弃未保存修改？"
      );
    }, business),
  ).toBe(true);
  if (heldProvider) {
    await expect.poll(async () => (await ipc("hold-facts")).started).toBe(true);
    await factAfter(
      page,
      after,
      (f) =>
        f.path === "/api/v1/system/model-providers/" + heldProvider &&
        f.method === "GET" &&
        !f.ended,
    );
    await expect(
      page.locator(".current-references > li").nth(0).locator("dl"),
    ).toHaveCount(1);
    await expect(page.locator(".current-references > li").nth(1)).toContainText(
      "正在读取 Model 与 Provider 详情",
    );
    // A hold that already expired is not evidence of cancelling a pending read.
    expect(await ipc("hold-facts")).toMatchObject({
      started: true,
      finished: false,
    });
  }
  await top(page)
    .getByRole("button", {
      name: decision === "continue" ? "继续编辑" : "放弃修改",
      exact: true,
    })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(
    decision === "continue" ? 1 : 0,
  );
  if (decision === "continue") await focusContained(page);
  expect((await writes(page)).length).toBe(count);
  return after;
}

async function rereadSavedReference(page: Page, purpose: Purpose) {
  const after = await seq(page),
    target = material().ids[purpose]!;
  await savedRow(page, purpose)
    .getByRole("button", {
      name: "重读 " + purposeLabels[purpose] + " 详情",
      exact: true,
    })
    .click();
  const model = await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/system/models/" + target &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.model,
  );
  await factAfter(
    page,
    model.sequence,
    (f) =>
      f.path === "/api/v1/system/model-providers/" + model.model!.provider_id &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.provider,
  );
  await expect(savedRow(page, purpose).locator("dl")).toHaveCount(1);
  await expect(savedRow(page, purpose)).not.toContainText("读取已取消");
  await expect(button(page, "刷新配置")).toBeEnabled();
}
async function layout(page: Page, width: number) {
  const facts = await page
    .locator(".system-model-selection")
    .evaluate((region, width) => {
      const problems: string[] = [],
        area = region.getBoundingClientRect();
      if (
        document.documentElement.scrollWidth > innerWidth + 1 ||
        region.scrollWidth > region.clientWidth + 1
      )
        problems.push("horizontal overflow");
      if (document.scrollingElement!.scrollHeight > innerHeight + 1)
        problems.push("outer vertical overflow");
      if (area.left < 0 || area.right > innerWidth + 1)
        problems.push("main bounds");
      const cards = [
        ...region.querySelectorAll<HTMLElement>(".current-references > li"),
      ];
      if (cards.length !== 4) problems.push("four purposes");
      for (const card of cards) {
        if (card.scrollWidth > card.clientWidth + 1)
          problems.push("card overflow");
        for (const value of card.querySelectorAll("dd,code")) {
          if (getComputedStyle(value).whiteSpace === "nowrap")
            problems.push("unwrapped detail");
        }
      }
      if (
        width <= 760 &&
        cards[0] &&
        cards[1] &&
        Math.abs(
          cards[0].getBoundingClientRect().left -
            cards[1].getBoundingClientRect().left,
        ) > 1
      )
        problems.push("narrow columns");
      return problems;
    }, width);
  expect(facts).toEqual([]);
}

test("[navigation] five leaves, restored cohost focus and eight real layouts", async ({
  page,
}) => {
  await page.addInitScript(observeNative);
  await page.goto("/system/model-selection");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(new URL(page.url()).searchParams.get("return")).toBe(
    "/system/model-selection",
  );
  await login(page, "admin");
  await expect(page).toHaveURL(/\/system\/model-selection$/);
  await currentRead(page, 0);
  await page.goto("/system");
  await expect(page).toHaveURL(/\/system\/users$/);
  await expect(title(page, "用户")).toBeVisible();
  await expect(
    page
      .getByRole("navigation", { name: "系统设置", exact: true })
      .getByRole("link"),
  ).toHaveCount(6);
  for (const leaf of ["待注册邀请", "Providers", "Models"]) {
    await page
      .getByRole("navigation", { name: "系统设置", exact: true })
      .getByRole("link", { name: leaf, exact: true })
      .click();
    await expect(title(page, leaf)).toBeVisible();
  }
  let after = await seq(page);
  await page
    .getByRole("navigation", { name: "系统设置", exact: true })
    .getByRole("link", { name: "平台模型用途", exact: true })
    .click();
  await currentRead(page, after);
  await openEditor(page);
  await clearOptional(page, "reranker");
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  const continuedRead = await checkingConfirmation(
    page,
    true,
    "配置平台模型用途",
  );
  // The continue decision lets its valid batch finish; the next round cancels
  // an independently held batch instead of relying on response timing.
  await currentRead(page, continuedRead);
  await expect(draftRow(page, "reranker")).toContainText("不配置");
  await page.keyboard.press("Escape");
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await page.keyboard.press("Escape");
  await expect(top(page)).toHaveAccessibleName("配置平台模型用途");
  await focusContained(page);
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  const beforeCancellation = await dbFacts();
  await ipc("hold-get", { id: material().ids.memory_provider! });
  let cancelledAfter: number;
  try {
    cancelledAfter = await checkingConfirmation(
      page,
      false,
      "配置平台模型用途",
      "discard",
      material().ids.memory_provider!,
    );
    await expect(savedRow(page, "embedding").locator("dl")).toHaveCount(1);
    for (const purpose of ["memory", "reranker", "image"] as const) {
      await expect(savedRow(page, purpose)).toContainText(
        material().ids[purpose]!,
      );
      await expect(savedRow(page, purpose)).toContainText("读取已取消");
      await expect(
        savedRow(page, purpose).locator('[role="status"]'),
      ).toHaveCount(0);
    }
  } finally {
    await ipc("release-get");
  }
  // The server hold ending is separate from the application's native tail.
  // The pure stream tests prove cancel joining; here retain the real observer
  // completion and enabled owner-gated control before starting explicit reads.
  await expect.poll(async () => (await ipc("hold-facts")).finished).toBe(true);
  await factAfter(
    page,
    cancelledAfter,
    (f) =>
      f.path ===
        "/api/v1/system/model-providers/" + material().ids.memory_provider! &&
      f.method === "GET" &&
      f.ended,
  );
  await expect(button(page, "刷新配置")).toBeEnabled();
  const cancelledReads = (await facts(page)).filter(
    (f) =>
      f.sequence > cancelledAfter &&
      /^\/api\/v1\/system\/(models|model-providers)\//.test(f.path),
  );
  expect(cancelledReads.map((f) => f.path)).toEqual([
    "/api/v1/system/models/" + material().ids.embedding!,
    "/api/v1/system/model-providers/" + material().ids.embedding_provider!,
    "/api/v1/system/models/" + material().ids.memory!,
    "/api/v1/system/model-providers/" + material().ids.memory_provider!,
  ]);
  expect(canonical(await dbFacts())).toBe(canonical(beforeCancellation));
  expect((await writes(page)).length).toBe(0);
  for (const purpose of ["memory", "reranker", "image"] as const)
    await rereadSavedReference(page, purpose);
  expect(canonical(await dbFacts())).toBe(canonical(beforeCancellation));
  expect((await writes(page)).length).toBe(0);
  await openEditor(page);
  await clearOptional(page, "image");
  const back = page.goBack();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await back;
  await expect(page).toHaveURL(/\/system\/model-selection$/);
  await focusContained(page);
  await discard(page);
  await openEditor(page);
  await clearOptional(page, "image");
  await cutSave(page);
  const accepted = await dbFacts();
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await checkingConfirmation(page, false, "配置平台模型用途");
  await expect(top(page)).toContainText("保存结果未确认");
  expect(canonical(await dbFacts())).toBe(canonical(accepted));
  await discard(page);
  await refresh(page);
  await openEditor(page);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page
      .locator(".selection-actions")
      .getByRole("button", { name: "编辑用途", exact: true }),
  ).toBeFocused();
  await openEditor(page);
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page
      .locator(".selection-actions")
      .getByRole("button", { name: "编辑用途", exact: true }),
  ).toBeFocused();
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await layout(page, width);
      if (width === 390) {
        await openEditor(page);
        await focusContained(page);
        expect(
          await top(page).evaluate((n) => {
            const r = n.getBoundingClientRect();
            return (
              r.left >= 0 &&
              r.right <= innerWidth + 1 &&
              n.scrollWidth <= n.clientWidth + 1
            );
          }),
        ).toBe(true);
        await browse(page, "memory", material().ids.memory_provider!);
        expect(
          await top(page).evaluate((n) => n.scrollWidth <= n.clientWidth + 1),
        ).toBe(true);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        await expect(top(page).getByRole("link")).toHaveCount(6);
        await focusContained(page);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(toggle).toBeFocused();
        await toggle.click();
        await page
          .locator(".ui-overlay")
          .last()
          .click({ position: { x: width - 3, y: 3 } });
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(toggle).toBeFocused();
      }
      await expect(page.locator(".ui-overlay")).toHaveCount(0);
      await expect(page.locator('input[type="password"],textarea')).toHaveCount(
        0,
      );
      await page.evaluate(() => {
        window.scrollTo(0, 0);
        document.querySelector<HTMLElement>("main.app-content")!.scrollTop = 0;
      });
      await expect
        .poll(() =>
          page.evaluate(() => ({
            main: document.querySelector<HTMLElement>("main.app-content")!
              .scrollTop,
            document: document.scrollingElement!.scrollTop,
          })),
        )
        .toEqual({ main: 0, document: 0 });
      await expect(title(page, "平台模型用途")).toBeInViewport();
      const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
      if (images)
        await page.screenshot({
          path: join(images, `selection-${theme}-${width}.png`),
          fullPage: true,
        });
      layouts++;
    }
  }
  expect((await ipc("failure-facts")).session_failures).toBe(1);
  result({
    five_leaves: true,
    ninth_return: true,
    dirty: true,
    uncertain: true,
    checking: true,
    focus: true,
    dialogs: true,
    no_overflow: true,
    layouts,
  });
});
