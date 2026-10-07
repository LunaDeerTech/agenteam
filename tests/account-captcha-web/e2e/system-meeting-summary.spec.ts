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
  summary?: { id: string; version: string; model: string | null };
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
  ids: Record<string, string>;
  selector_id: string;
  summary_id: string;
  providers: Provider[];
  models: Model[];
} => JSON.parse(readFileSync(join(directory, "summary-material.json"), "utf8"));
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "summary-result.json"),
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
    request = join(directory, "summary-ipc.json"),
    ack = join(directory, `summary-ack-${sequence}.json`);
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
        summary = path === "/api/v1/system/model-selection/meeting-summary",
        session = path === "/api/v1/session",
        lookup = path === "/api/v1/system/model-commands/lookup";
      const watched =
        (method === "GET" &&
          (providers ||
            providerID ||
            models ||
            modelID ||
            selector ||
            summary ||
            session)) ||
        (method === "PUT" && (selector || summary)) ||
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
              if (summary) {
                if (
                  closed(r, ["id", "version", "model"]) &&
                  id(r.id) &&
                  integer(r.version) &&
                  (r.model === null || id(r.model))
                )
                  fact.summary = {
                    id: r.id,
                    version: r.version,
                    model: r.model,
                  };
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
async function login(page: Page, who: "admin" | "member") {
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

const summaryEndpoint = endpoint + "/meeting-summary";
const section = (page: Page) => page.locator(".meeting-summary-settings");
const summaryButton = (page: Page, name: string) =>
  section(page).getByRole("button", { name, exact: true });
async function ready(page: Page) {
  await expect(button(page, "刷新配置")).toBeEnabled();
  await expect(summaryButton(page, "重读会议 Summary 配置")).toBeEnabled();
  await expect
    .poll(
      async () =>
        !!(await facts(page)).find(
          (f) =>
            f.path === summaryEndpoint &&
            f.method === "GET" &&
            f.ended &&
            !!f.summary,
        ),
    )
    .toBe(true);
}
async function setup(page: Page) {
  await page.addInitScript(observeNative);
  await page.goto("/system/model-selection");
  await login(page, "admin");
  await ready(page);
}
async function summaryCurrent(page: Page, after: number) {
  const f = await factAfter(
    page,
    after,
    (f) =>
      f.path === summaryEndpoint &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.summary,
  );
  await expect(summaryButton(page, "重读会议 Summary 配置")).toBeEnabled();
  return f.summary!;
}
async function summaryRefresh(page: Page) {
  const after = await seq(page);
  await summaryButton(page, "重读会议 Summary 配置").click();
  return summaryCurrent(page, after);
}
async function summaryOpen(page: Page) {
  await section(page)
    .getByRole("button", {
      name: /^(配置会议 Summary|编辑会议 Summary|会议 Summary 已保存)$/,
    })
    .click();
  await expect(
    section(page).getByRole("form", { name: "会议 Summary 编辑" }),
  ).toBeVisible();
}
async function summaryBrowse(
  page: Page,
  providerID = material().ids.summary_provider!,
) {
  await summaryButton(page, "选择会议 Summary Model").click();
  await expect(summaryButton(page, "重读 Providers 当前页")).toBeEnabled();
  await expect(section(page).locator(".choices > li").first()).toBeVisible();
  const row = section(page)
    .locator(".choices > li")
    .filter({
      has: page.locator("strong", { hasText: providerName(providerID) }),
    });
  for (let n = 0; n < 2 && (await row.count()) === 0; n++) {
    await summaryButton(page, "下一页 Providers").click();
    await expect(summaryButton(page, "重读 Providers 当前页")).toBeEnabled();
  }
  await row
    .getByRole("button", { name: "查看此 Provider 的 Models", exact: true })
    .click();
  await expect(
    section(page).getByText("Provider：" + providerName(providerID), {
      exact: false,
    }),
  ).toBeVisible();
  await expect(summaryButton(page, "重读 Models 当前页")).toBeEnabled();
}
async function summaryChoose(page: Page, target: string, changed = true) {
  await summaryBrowse(
    page,
    material().models.find((m) => m.id === target)!.provider_id,
  );
  const row = section(page)
    .locator(".choices > li")
    .filter({ has: page.locator("strong", { hasText: modelName(target) }) });
  for (let n = 0; n < 2 && (await row.count()) === 0; n++) {
    await summaryButton(page, "下一页 Models").click();
    await expect(summaryButton(page, "重读 Models 当前页")).toBeEnabled();
  }
  await row
    .getByRole("button", { name: "选择此会议 Summary Model", exact: true })
    .click();
  await expect(section(page).locator(".summary-picker")).toHaveCount(0);
  if (changed)
    await expect(summaryButton(page, "保存会议 Summary")).toBeEnabled();
}
async function summarySave(page: Page, read = true) {
  const after = await seq(page);
  await summaryButton(page, "保存会议 Summary").click();
  const f = await factAfter(
    page,
    after,
    (f) =>
      f.path === summaryEndpoint &&
      f.method === "PUT" &&
      f.ended &&
      !!f.receipt,
  );
  if (read) await summaryCurrent(page, f.sequence);
  return f.receipt!;
}
async function platformSave(page: Page) {
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  const f = await factAfter(
    page,
    after,
    (f) => f.path === endpoint && f.method === "PUT" && f.ended && !!f.receipt,
  );
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await ready(page);
  return f.receipt!;
}
async function dbFacts() {
  return (await ipc("facts")).facts as Record<
    "summary" | "platform",
    {
      id: string;
      version: string;
      model?: string | null;
      commands: number;
      audits: number;
      events: number;
      distinct_keys: number;
      references: { role: string; model: string; version: string }[];
      invocations: number;
      deliveries: number;
    }
  >;
}
function unique(value: Awaited<ReturnType<typeof dbFacts>>) {
  for (const s of Object.values(value)) {
    expect(s.commands).toBe(s.audits);
    expect(s.commands).toBe(s.distinct_keys);
    expect(s.events).toBeGreaterThanOrEqual(s.commands);
    expect(s.invocations).toBe(0);
    expect(s.deliveries).toBe(0);
  }
}
async function noOverlay(page: Page) {
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(
    await page
      .locator(".app-shell")
      .evaluate((el) => el instanceof HTMLElement && el.inert),
  ).toBe(false);
  expect(
    await page.evaluate(
      () =>
        document.activeElement !== document.body &&
        document.activeElement?.isConnected &&
        !document.activeElement?.closest("[inert]"),
    ),
  ).toBe(true);
}

test("[lifecycle] independent ordinary-chat Summary with retained disabled binding", async ({
  page,
}) => {
  const data = material();
  await setup(page);
  const initial = await dbFacts();
  expect(initial.summary).toMatchObject({
    version: "1",
    model: null,
    commands: 0,
  });
  expect(initial.platform.commands).toBe(0);
  await expect(
    section(page).getByRole("heading", {
      name: "尚未配置会议 Summary",
      exact: true,
    }),
  ).toBeVisible();
  await summaryOpen(page);
  await summaryChoose(page, data.ids.summary_0!);
  const plain = data.models.find((m) => m.id === data.ids.summary_0)!;
  expect(plain.input.capabilities.structured_output_modes).not.toContain(
    "json_schema",
  );
  const receipt = await summarySave(page);
  expect(receipt.resource_id).toBe(data.summary_id);
  expect(receipt.version).toBe("2");
  await expect(section(page)).toContainText(data.ids.summary_0!);
  await summaryOpen(page);
  expect(await summaryButton(page, "保存会议 Summary").isDisabled()).toBe(true);
  expect(
    await section(page)
      .getByRole("button", { name: /清空|不配置/ })
      .count(),
  ).toBe(0);
  await summaryButton(page, "取消会议 Summary 编辑").click();
  await openEditor(page);
  for (const purpose of ["embedding", "memory"] as const)
    await choose(page, purpose, data.ids[purpose]!);
  await platformSave(page);
  const configured = await dbFacts();
  expect(configured.summary).toEqual({
    ...initial.summary,
    version: "2",
    model: data.ids.summary_0,
    commands: 1,
    audits: 1,
    events: 1,
    distinct_keys: 1,
    references: [
      { role: "meeting_summary", model: data.ids.summary_0, version: "2" },
    ],
  });
  expect(configured.platform.commands).toBe(1);
  unique(configured);
  await ipc("disable-model", { id: data.ids.summary_0! });
  await summaryRefresh(page);
  await expect(section(page)).toContainText("Model 已停用");
  await expect(section(page)).toContainText(data.ids.summary_0!);
  expect((await dbFacts()).summary.version).toBe("2");
  result({
    null_v1: true,
    plain_chat: true,
    independent: true,
    disabled_reference: true,
    no_clear: true,
    no_invocation: true,
  });
});

test("[recovery] two accepted response losses keep independent original bodies and receipts", async ({
  page,
}) => {
  const data = material();
  await setup(page);
  const before = await dbFacts();
  await summaryOpen(page);
  await summaryChoose(page, data.ids.summary_1!);
  await ipc("arm-drop", { slot: "summary" });
  await summaryButton(page, "保存会议 Summary").click();
  await expect(summaryButton(page, "检查会议 Summary 原请求")).toBeEnabled();
  await openEditor(page);
  await clearOptional(page, "image");
  await ipc("arm-drop", { slot: "platform" });
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  await expect(
    top(page).getByRole("button", { name: "检查原请求", exact: true }),
  ).toBeEnabled();
  const accepted = await dbFacts();
  expect(accepted.summary.commands).toBe(before.summary.commands + 1);
  expect(accepted.platform.commands).toBe(before.platform.commands + 1);
  expect((await ipc("cross-key")).code).toBe("IDEMPOTENCY_KEY_REUSED");
  await top(page)
    .getByRole("button", { name: "检查原请求", exact: true })
    .click();
  await expect(
    top(page).getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  expect(await dbFacts()).toEqual(accepted);
  await top(page)
    .getByRole("button", { name: "重试原请求", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await ready(page);
  expect(await dbFacts()).toEqual(accepted);
  // A later formal replacement advances the selector; replay still returns the captured historical receipt.
  await ipc("delete", {
    id: data.ids.summary_1!,
    replacement: data.ids.summary_0!,
  });
  const replaced = await dbFacts();
  expect(BigInt(replaced.summary.version)).toBeGreaterThan(
    BigInt(accepted.summary.version),
  );
  await summaryButton(page, "检查会议 Summary 原请求").click();
  await expect(summaryButton(page, "重试会议 Summary 原请求")).toBeEnabled();
  expect(await dbFacts()).toEqual(replaced);
  await ipc("arm-get-fail", { slot: "summary" });
  await summaryButton(page, "重试会议 Summary 原请求").click();
  await expect(section(page)).toContainText("已保存，当前配置读取失败");
  expect(await dbFacts()).toEqual(replaced);
  for (const slot of ["summary", "platform"]) {
    const drop = await ipc("drop-facts", { slot });
    expect(drop.dropped).toBe(1);
    expect(drop.replayed).toBe(1);
    expect(drop.same_original).toBe(true);
  }
  await summaryRefresh(page);
  await summaryOpen(page);
  await summaryChoose(page, data.ids.summary_0!, false); // same current model is a no-op
  expect(await summaryButton(page, "保存会议 Summary").isDisabled()).toBe(true);
  await summaryButton(page, "取消会议 Summary 编辑").click();
  // Local Summary discard must preserve a separate, accepted-but-unobserved platform intent.
  await summaryOpen(page);
  await summaryBrowse(page);
  await summaryButton(page, "关闭会议 Summary 候选").click();
  await openEditor(page);
  await clearOptional(page, "reranker");
  await ipc("arm-drop", { slot: "platform" });
  await top(page)
    .getByRole("button", { name: "保存用途配置", exact: true })
    .click();
  await expect(
    top(page).getByRole("button", { name: "检查原请求", exact: true }),
  ).toBeEnabled();
  await top(page)
    .getByRole("button", { name: "放弃原请求", exact: true })
    .click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(
    section(page).getByRole("form", { name: "会议 Summary 编辑" }),
  ).toBeVisible();
  unique(await dbFacts());
  result({
    dual_intents: true,
    lookup_only: true,
    same_original: true,
    cross_key: true,
    historical: true,
    local_abandon: true,
    confirmed_read_failure: true,
    unique_facts: true,
  });
});

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

test("[authority] one aggregate navigation decision, actual Session continuity and current authorization", async ({
  page,
}) => {
  const data = material();
  await setup(page);
  const firstSession = await session(page);
  await summaryOpen(page);
  await summaryChoose(page, data.ids.summary_1!);
  await openEditor(page);
  await clearOptional(page, "image");
  await browse(page, "memory", data.ids.empty_provider!);
  await top(page)
    .getByRole("link", { name: "管理 Models", exact: true })
    .click();
  await expect(top(page)).toContainText("四项平台用途、会议 Summary");
  expect(await page.locator('[role="dialog"]').count()).toBe(2);
  await checkingConfirmation(page, true, "配置平台模型用途");
  await expect(section(page)).toContainText(
    "草稿 Model：" + modelName(data.ids.summary_1!),
  );
  await browse(page, "memory", data.ids.empty_provider!);
  await top(page)
    .getByRole("link", { name: "管理 Models", exact: true })
    .click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page).toHaveURL(/\/system\/models$/);
  await page
    .locator('nav[aria-label="系统设置"]')
    .getByRole("link", { name: "平台模型用途", exact: true })
    .click();
  await ready(page);
  await expect(section(page).locator("form")).toHaveCount(0);
  await noOverlay(page);
  // A real response held before forwarding keeps the shared Cookie owner; no sibling request is dispatched.
  await ipc("hold-get", { slot: "summary" });
  const heldAfter = await seq(page);
  await summaryButton(page, "重读会议 Summary 配置").click();
  await expect.poll(async () => (await ipc("hold-facts")).started).toBe(true);
  await expect(button(page, "刷新配置")).toBeDisabled();
  await expect(button(page, "退出登录")).toBeDisabled();
  expect(
    (await facts(page)).filter(
      (f) => f.sequence > heldAfter && f.path === endpoint,
    ),
  ).toHaveLength(0);
  await ipc("release-get");
  await summaryCurrent(page, heldAfter);
  await ipc("demote");
  const deniedAfter = await seq(page);
  await summaryButton(page, "重读会议 Summary 配置").click();
  await factAfter(
    page,
    deniedAfter,
    (f) =>
      f.path === summaryEndpoint &&
      f.status === 403 &&
      f.code === "FORBIDDEN" &&
      f.ended,
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await ipc("promote");
  await button(page, "重新检查权限").click();
  await ready(page);
  // A dirty Summary draft participates in the existing formal logout confirmation.
  await summaryOpen(page);
  await summaryChoose(page, data.ids.summary_1!);
  await button(page, "退出登录").click();
  await expect(top(page)).toContainText("会议 Summary");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await expect(section(page).locator("form")).toBeVisible();
  await button(page, "退出登录").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "admin");
  await page.goto("/system/model-selection");
  await ready(page);
  const nextSession = await session(page);
  expect(nextSession.session_id).not.toBe(firstSession.session_id);
  expect(nextSession.user_id).toBe(firstSession.user_id);
  await expect(section(page).locator("form")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /重试.*原请求/ })).toHaveCount(
    0,
  );
  await noOverlay(page);
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/model-selection");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  unique(await dbFacts());
  result({
    aggregate: true,
    checking: true,
    new_session: true,
    current403: true,
    logout: true,
    owner: true,
    focus: true,
    no_overlay: true,
    role_fact_preparation_only: true,
  });
});

test("[read] bounded real pages and incomplete-pair handling across eight layouts", async ({
  page,
}) => {
  const data = material();
  await setup(page);
  await summaryOpen(page);
  await summaryButton(page, "选择会议 Summary Model").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  const providerPage1 = (await facts(page))
    .filter((f) => f.providers)
    .at(-1)!.providers!;
  expect(providerPage1.next_cursor).not.toBeNull();
  await summaryButton(page, "下一页 Providers").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(
    data.providers.length - 25,
  );
  await summaryButton(page, "上一页 Providers").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  const providerRow = section(page)
    .locator(".choices > li")
    .filter({
      has: page.locator("strong", {
        hasText: providerName(data.ids.summary_provider!),
      }),
    });
  await providerRow
    .getByRole("button", { name: "查看此 Provider 的 Models", exact: true })
    .click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  const first = (await facts(page)).filter((f) => f.models).at(-1)!.models!;
  expect(first.next_cursor).not.toBeNull();
  expect(new Set(first.items.map((m) => m.id)).size).toBe(25);
  await summaryButton(page, "下一页 Models").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(2);
  const second = (await facts(page)).filter((f) => f.models).at(-1)!.models!;
  expect(second.next_cursor).toBeNull();
  expect(new Set([...first.items, ...second.items].map((m) => m.id)).size).toBe(
    27,
  );
  await summaryButton(page, "上一页 Models").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  let altered = false;
  await page.route("**/api/v1/system/models?*", async (route) => {
    const url = new URL(route.request().url());
    if (!altered && url.searchParams.has("cursor")) {
      altered = true;
      url.searchParams.set("cursor", "owned-summary-invalid-cursor");
      await route.continue({ url: url.toString() });
    } else await route.continue();
  });
  const badAfter = await seq(page);
  await summaryButton(page, "下一页 Models").click();
  await factAfter(
    page,
    badAfter,
    (f) => f.code === "CURSOR_INVALID" && f.ended,
  );
  await expect(section(page).locator(".choices > li")).toHaveCount(0);
  await page.unroute("**/api/v1/system/models?*");
  await summaryButton(page, "返回 Models 首页").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  await summaryButton(page, "返回会议 Summary Providers").click();
  await expect(section(page).locator(".choices > li")).toHaveCount(25);
  await section(page)
    .locator(".choices > li")
    .filter({
      has: page.locator("strong", {
        hasText: providerName(data.ids.empty_provider!),
      }),
    })
    .getByRole("button", { name: "查看此 Provider 的 Models", exact: true })
    .click();
  await expect(
    section(page).getByRole("heading", {
      name: "当前 Model 页为空",
      exact: true,
    }),
  ).toBeVisible();
  await summaryButton(page, "返回会议 Summary Providers").click();
  await ipc("hold-get", { id: data.ids.summary_provider! });
  await providerRow
    .getByRole("button", { name: "查看此 Provider 的 Models", exact: true })
    .click();
  await expect.poll(async () => (await ipc("hold-facts")).started).toBe(true);
  await summaryButton(page, "关闭会议 Summary 候选").click();
  await ipc("release-get");
  await expect(section(page).locator(".summary-picker")).toHaveCount(0);
  await summaryButton(page, "取消会议 Summary 编辑").click();
  await ipc("arm-get-fail", { id: data.ids.summary_provider! });
  await summaryRefresh(page);
  await expect(section(page)).toContainText(data.ids.summary_0!);
  await expect(section(page).locator(".current-summary dl")).toHaveCount(0);
  await summaryButton(page, "重读会议 Summary 详情").click();
  await expect(section(page).locator(".current-summary dl")).toHaveCount(1);
  await ipc("disable-provider", { id: data.ids.summary_provider! });
  await summaryRefresh(page);
  await expect(section(page)).toContainText("Provider 已停用");
  await ipc("enable-provider", { id: data.ids.summary_provider! });
  await summaryRefresh(page);
  const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
  expect(images).toBeTruthy();
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    for (const width of [390, 768, 1280, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await section(page).scrollIntoViewIfNeeded();
      expect(
        await section(page).evaluate(
          (el) =>
            document.documentElement.scrollWidth <= innerWidth + 1 &&
            el.scrollWidth <= el.clientWidth + 1,
        ),
      ).toBe(true);
      await page.screenshot({
        path: join(images!, `summary-${theme}-${width}.png`),
        fullPage: false,
      });
      layouts++;
    }
  }
  await summaryButton(page, "重读会议 Summary 配置").focus();
  await page.keyboard.press("Tab");
  expect(
    await page.evaluate(
      () =>
        document.activeElement?.closest(".meeting-summary-settings") !== null,
    ),
  ).toBe(true);
  await page.evaluate(() => {
    document.documentElement.style.zoom = "2";
  });
  expect(
    await section(page).evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
  ).toBe(true);
  await page.screenshot({
    path: join(images!, "summary-css-zoom2.png"),
    fullPage: false,
  });
  await page.evaluate(() => {
    document.documentElement.style.zoom = "";
  });
  unique(await dbFacts());
  result({
    pagination: true,
    providers: data.providers.length,
    models: data.models.filter(
      (m) => m.provider_id === data.ids.summary_provider,
    ).length,
    empty: true,
    bad_cursor: true,
    late_read: true,
    no_half_pair: true,
    disabled_reference: true,
    keyboard: true,
    no_overflow: true,
    layouts,
    css_zoom_only: true,
  });
});
