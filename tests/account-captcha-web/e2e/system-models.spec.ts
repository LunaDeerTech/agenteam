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
type Impact = {
  model_id: string;
  version: string;
  reference_count: string;
  reference_groups: { owner_kind: string; role: string; count: string }[];
  replacement_requirement: string;
  delete_blocker: string | null;
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
  impact?: Impact;
  session?: { user_id: string; session_id: string; role: string };
  found?: boolean;
};
type ObservedWindow = Window & {
  __modelFacts?: Fact[];
  __modelSequence?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  ids: Record<string, string>;
  providers: Provider[];
  models: Model[];
} => JSON.parse(readFileSync(join(directory, "models-material.json"), "utf8"));
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "models-result.json"),
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
    request = join(directory, "models-ipc.json"),
    ack = join(directory, `models-ack-${sequence}.json`);
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
  scope.__modelFacts = [];
  scope.__modelSequence = 0;
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
  const pairs = [
    "agent/agent_model",
    "agent/approval_model",
    "platform_selector/embedding",
    "platform_selector/image",
    "platform_selector/memory",
    "platform_selector/reranker",
    "project_summary/meeting_summary",
  ];
  function impact(v: unknown): Impact | null {
    const r = object(v);
    if (
      !r ||
      !closed(r, [
        "model_id",
        "version",
        "reference_count",
        "reference_groups",
        "replacement_requirement",
        "delete_blocker",
      ]) ||
      !id(r.model_id) ||
      !integer(r.version) ||
      !integer(r.reference_count, true) ||
      BigInt(r.reference_count) > 10000n ||
      !Array.isArray(r.reference_groups) ||
      r.reference_groups.length > 7
    )
      return null;
    const groups: Impact["reference_groups"] = [];
    let last = "",
      sum = 0n,
      required = false,
      unbound = false;
    for (const raw of r.reference_groups) {
      const g = object(raw);
      if (
        !g ||
        !closed(g, ["owner_kind", "role", "count"]) ||
        typeof g.owner_kind !== "string" ||
        typeof g.role !== "string" ||
        !integer(g.count)
      )
        return null;
      const key = g.owner_kind + "/" + g.role;
      if (!pairs.includes(key) || key <= last) return null;
      last = key;
      sum += BigInt(g.count);
      required ||= ![
        "platform_selector/image",
        "platform_selector/reranker",
      ].includes(key);
      unbound ||= g.owner_kind !== "platform_selector";
      groups.push({ owner_kind: g.owner_kind, role: g.role, count: g.count });
    }
    const requirement =
        sum === 0n ? "none" : required ? "required" : "optional",
      blocker = unbound ? "reference_adapter_unbound" : null;
    if (
      String(sum) !== r.reference_count ||
      r.replacement_requirement !== requirement ||
      r.delete_blocker !== blocker
    )
      return null;
    return {
      model_id: r.model_id,
      version: r.version,
      reference_count: r.reference_count,
      reference_groups: groups,
      replacement_requirement: requirement,
      delete_blocker: blocker,
    };
  }
  function receipt(v: unknown): Receipt | null {
    const r = object(v);
    return r &&
      closed(r, ["kind", "resource_id", "version", "affected_references"]) &&
      typeof r.kind === "string" &&
      ["model.create", "model.update", "model.delete"].includes(r.kind) &&
      id(r.resource_id) &&
      integer(r.version) &&
      integer(r.affected_references, true) &&
      (r.kind === "model.delete" || r.affected_references === "0")
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
        isImpact =
          /^\/api\/v1\/system\/models\/[^/]+\/deletion-impact$/.test(path) &&
          id(path.split("/")[5]),
        session = path === "/api/v1/session",
        lookup = path === "/api/v1/system/model-commands/lookup";
      const watched =
        (method === "GET" &&
          (providers ||
            providerID ||
            models ||
            modelID ||
            isImpact ||
            session)) ||
        (method === "POST" && (models || lookup)) ||
        ((method === "PUT" || method === "DELETE") && modelID);
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
        sequence: ++scope.__modelSequence!,
        path: path + url.search,
        method,
        status: 0,
        bytes: 0,
        ended: false,
      };
      scope.__modelFacts!.push(fact);
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
                if (typeof r.found === "boolean") fact.found = r.found;
                return;
              }
              if (method !== "GET") {
                const value = receipt(r);
                if (value) fact.receipt = value;
                return;
              }
              if (isImpact) {
                const value = impact(r);
                if (value) fact.impact = value;
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
const top = (page: Page) => page.getByRole("dialog").last();
const button = (page: Page, name: string) =>
  (name === "创建 Model"
    ? page.locator('[aria-label="Model 页面操作"]')
    : page
  ).getByRole("button", { name, exact: true });
const title = (page: Page, name: string) =>
  page.getByRole("heading", { name, exact: true });
async function facts(page: Page) {
  return page.evaluate(() => (window as ObservedWindow).__modelFacts ?? []);
}
async function seq(page: Page) {
  return page.evaluate(() => (window as ObservedWindow).__modelSequence ?? 0);
}
async function writes(page: Page) {
  return (await facts(page)).filter(
    (f) => f.method !== "GET" && !f.path.endsWith("/lookup"),
  );
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
async function factAfter(
  page: Page,
  after: number,
  predicate: (fact: Fact) => boolean,
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
  let found: Fact["session"];
  await expect
    .poll(async () => {
      found = (await facts(page))
        .filter((f) => f.status === 200 && f.ended && f.session)
        .at(-1)?.session;
      return !!found;
    })
    .toBe(true);
  return found!;
}
async function privateFill(input: Locator, value: string) {
  try {
    await input.fill(value);
  } catch {
    throw new Error("private Model login input failed");
  }
}
async function login(page: Page, who: "admin" | "member") {
  const after = await seq(page);
  await page.locator("#login-email").fill(material()[who].email);
  await privateFill(page.locator("#login-password"), material()[who].password);
  await button(page, "登录").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === material()[who].user_id &&
      f.session.role === (who === "admin" ? "admin" : "user"),
  );
  await expect(button(page, "退出登录")).toBeEnabled();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
async function setup(
  page: Page,
  who: "admin" | "member" = "admin",
  path = "/system/models",
) {
  await page.addInitScript(observeNative);
  await page.goto(path);
  await login(page, who);
  await expect(page).toHaveURL(new RegExp(path + "$"));
  if (who === "admin")
    await expect(
      title(page, path.endsWith("users") ? "用户" : "Models"),
    ).toBeVisible();
}
async function providerRow(page: Page, providerID: string) {
  const source = (await ipc("snapshot")).providers.find(
    (p: Provider) => p.id === providerID,
  ) as Provider;
  expect(!!source).toBe(true);
  return page
    .locator(".model-table tbody tr")
    .filter({ has: page.getByText(source.input.name, { exact: true }) });
}
async function selectProvider(
  page: Page,
  providerID = material().ids.target_provider!,
) {
  if (await button(page, "返回 Providers").count())
    await button(page, "返回 Providers").click();
  const row = await providerRow(page, providerID);
  await row.getByRole("button", { name: "选择 Provider", exact: true }).click();
  await expect(button(page, "创建 Model")).toBeEnabled();
  await expect(button(page, "刷新列表")).toBeEnabled();
}
async function selectModel(page: Page, modelID = material().ids.target!) {
  const row = (await ipc("snapshot")).models.find(
    (m: Model) => m.id === modelID,
  ) as Model;
  expect(!!row).toBe(true);
  const after = await seq(page);
  await page
    .locator(".model-table tbody tr")
    .filter({ has: page.getByText(row.input.name, { exact: true }) })
    .getByRole("button", { name: "查看 Model", exact: true })
    .click();
  const observed = await factAfter(
    page,
    after,
    (f) =>
      f.method === "GET" &&
      f.path === "/api/v1/system/models/" + modelID &&
      f.status === 200 &&
      f.ended &&
      !!f.model,
  );
  expect(canonical(observed.model) === canonical(row)).toBe(true);
  await expect(button(page, "编辑 Model")).toBeEnabled();
  return row;
}
async function openCreate(page: Page) {
  await button(page, "创建 Model").click();
  await expect(top(page)).toHaveAccessibleName("创建 Model");
}
async function fill(
  page: Page,
  name: string,
  kind = "chat",
  native = " native exact ",
) {
  const d = top(page);
  await d.getByRole("textbox", { name: "名称", exact: true }).fill(name);
  await d
    .getByRole("textbox", { name: "原生 Model ID", exact: true })
    .fill(native);
  await d.getByRole("button", { name: "启用状态", exact: true }).click();
  await page.getByRole("option", { name: "启用", exact: true }).click();
  await d
    .getByRole("group", { name: "输入模态", exact: true })
    .getByRole("checkbox", { name: "text", exact: true })
    .check();
  if (kind !== "reranker")
    await d
      .getByRole("group", { name: "输出模态", exact: true })
      .getByRole("checkbox", {
        name:
          kind === "embedding"
            ? "vector"
            : kind === "image_generation"
              ? "image"
              : "text",
        exact: true,
      })
      .check();
  if (kind === "chat")
    await d
      .getByRole("group", { name: "结构化输出声明", exact: true })
      .getByRole("checkbox", { name: "text", exact: true })
      .check();
}
async function perform(page: Page, name: string) {
  const after = await seq(page);
  await top(page).getByRole("button", { name, exact: true }).click();
  return after;
}
async function confirmed(page: Page, after: number, kind: string) {
  const fact = await factAfter(
    page,
    after,
    (f) => f.status === 200 && f.ended && f.receipt?.kind === kind,
  );
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(button(page, "刷新列表")).toBeEnabled();
  return fact.receipt!;
}
async function saved(page: Page, after: number, kind: string) {
  const receipt = await confirmed(page, after, kind);
  const observed = await factAfter(
    page,
    after,
    (f) => f.status === 200 && f.ended && f.model?.id === receipt.resource_id,
  );
  expect(observed.model!.version).toBe(receipt.version);
  return observed.model!;
}
async function edit(page: Page) {
  await button(page, "编辑 Model").click();
  await expect(top(page)).toHaveAccessibleName("编辑 Model");
}
async function removePreview(page: Page) {
  const after = await seq(page);
  await button(page, "删除 Model").click();
  await expect(top(page)).toHaveAccessibleName("删除 Model");
  const fact = await factAfter(
    page,
    after,
    (f) => f.status === 200 && f.ended && !!f.impact,
  );
  return fact.impact!;
}
async function discard(page: Page) {
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  if (
    await top(page)
      .getByRole("heading", { name: "放弃未保存修改？", exact: true })
      .count()
  )
    await top(page)
      .getByRole("button", { name: "放弃修改", exact: true })
      .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
}
async function dbFacts(id?: string) {
  return (await ipc("facts", id ? { id } : {})).facts;
}
async function candidateProvider(page: Page, id: string) {
  const source = (await ipc("snapshot")).providers.find(
    (p: Provider) => p.id === id,
  ) as Provider;
  if (
    await top(page)
      .getByRole("button", { name: "返回替代 Providers", exact: true })
      .count()
  )
    await top(page)
      .getByRole("button", { name: "返回替代 Providers", exact: true })
      .click();
  await top(page)
    .locator(".choices li")
    .filter({ hasText: source.input.name })
    .getByRole("button", { name: "查看此 Provider 的 Models", exact: true })
    .click();
  await expect(
    top(page).getByRole("heading", {
      name: "替代 Models：" + source.input.name,
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    top(page).getByRole("button", { name: "返回替代 Providers", exact: true }),
  ).toBeEnabled();
}
async function candidateRow(page: Page, id: string) {
  const row = (await ipc("snapshot")).models.find(
    (m: Model) => m.id === id,
  ) as Model;
  return top(page).locator(".choices li").filter({ hasText: row.input.name });
}
async function choose(page: Page, id: string) {
  await (
    await candidateRow(page, id)
  )
    .getByRole("button", { name: "选择此替代 Model", exact: true })
    .click();
  await expect(
    top(page).getByRole("button", { name: "确认删除 Model", exact: true }),
  ).toBeEnabled();
}
async function focusContained(page: Page) {
  for (const key of ["Tab", "Tab", "Shift+Tab", "Shift+Tab"]) {
    await page.keyboard.press(key);
    expect(
      await top(page).evaluate((n) => n.contains(document.activeElement)),
    ).toBe(true);
  }
}

test("[lifecycle] four legal types, safe details, configuration edits and unreferenced deletion", async ({
  page,
}) => {
  await setup(page);
  const start = await dbFacts();
  for (const [name, kind] of [
    ["chat", "chat"],
    ["anthropic", "chat"],
    ["embedding", "embedding"],
    ["reranker", "reranker"],
    ["image", "image_generation"],
  ]) {
    await selectProvider(page, material().ids[name + "_provider"]!);
    await openCreate(page);
    await fill(
      page,
      "Created " + name,
      kind,
      " native " + "界".repeat(70) + " ",
    );
    await expect(
      top(page).getByRole("textbox", { name: "Provider", exact: true }),
    ).toHaveAttribute("readonly", "");
    await expect(
      top(page).getByRole("textbox", { name: "类型", exact: true }),
    ).toHaveValue(kind!);
    await expect(
      top(page).getByRole("textbox", { name: "类型", exact: true }),
    ).toHaveAttribute("readonly", "");
    expect(await top(page).locator("textarea").count()).toBe(0);
    if (name === "chat") {
      const originalWrites = (await writes(page)).length;
      await top(page)
        .getByRole("textbox", { name: "上下文容量", exact: true })
        .fill("1");
      await top(page)
        .getByRole("textbox", { name: "最大输出容量", exact: true })
        .fill("2");
      await perform(page, "保存 Model 配置");
      await expect(top(page)).toContainText("输入不会自动改写");
      expect((await writes(page)).length).toBe(originalWrites);
      await expect(
        top(page).getByRole("button", { name: "保存 Model 配置", exact: true }),
      ).toBeFocused();
      await top(page)
        .getByRole("textbox", { name: "最大输出容量", exact: true })
        .fill("1");
    }
    let current = await saved(
      page,
      await perform(page, "保存 Model 配置"),
      "model.create",
    );
    expect(
      current.input.type === kind &&
        current.input.provider_model_id === " native " + "界".repeat(70) + " ",
    ).toBe(true);
    expect(current.version).toBe("1");
    await edit(page);
    await top(page)
      .getByRole("textbox", { name: "名称", exact: true })
      .fill(" Edited " + name + " ");
    await top(page)
      .getByRole("textbox", { name: "原生 Model ID", exact: true })
      .fill(" updated native ");
    await top(page)
      .getByRole("textbox", { name: "上下文容量", exact: true })
      .fill("9223372036854775807");
    await top(page)
      .getByRole("textbox", { name: "最大输出容量", exact: true })
      .fill("31");
    if (kind === "chat") {
      await top(page)
        .getByRole("checkbox", { name: "工具调用", exact: true })
        .check();
      await top(page)
        .getByRole("checkbox", {
          name: "并行工具调用（需要工具调用）",
          exact: true,
        })
        .check();
      await top(page)
        .getByRole("checkbox", { name: "流式输出", exact: true })
        .check();
    }
    current = await saved(
      page,
      await perform(page, "保存 Model 配置"),
      "model.update",
    );
    expect(
      current.version === "2" &&
        current.input.name === " Edited " + name + " " &&
        current.input.provider_model_id === " updated native " &&
        current.input.capabilities.context_length === "9223372036854775807",
    ).toBe(true);
    for (const enabled of [false, true]) {
      await edit(page);
      await top(page)
        .getByRole("button", { name: "启用状态", exact: true })
        .click();
      await page
        .getByRole("option", { name: enabled ? "启用" : "禁用", exact: true })
        .click();
      current = await saved(
        page,
        await perform(page, "保存 Model 配置"),
        "model.update",
      );
      expect(current.input.enabled).toBe(enabled);
    }
    const before = (await writes(page)).length;
    const impact = await removePreview(page);
    expect(
      impact.reference_count === "0" &&
        impact.replacement_requirement === "none",
    ).toBe(true);
    await discard(page);
    expect((await writes(page)).length).toBe(before);
    await removePreview(page);
    const receipt = await confirmed(
      page,
      await perform(page, "确认删除 Model"),
      "model.delete",
    );
    expect(
      receipt.resource_id === current.id &&
        receipt.version === "5" &&
        receipt.affected_references === "0",
    ).toBe(true);
    expect((await ipc("missing", { id: current.id })).missing).toBe(true);
    const state = await dbFacts(current.id);
    expect(
      !state.exists && state.delete_receipts === 1 && state.target_events === 5,
    ).toBe(true);
  }
  const end = await dbFacts();
  expect(
    end.models === start.models &&
      end.commands === start.commands + 25 &&
      end.audits === start.audits + 25 &&
      end.events === start.events + 25 &&
      end.invocations === 0 &&
      end.credentials === 0,
  ).toBe(true);
  result({
    four_types: true,
    edited: true,
    enabled: true,
    deleted: true,
    disabled_provider: true,
    no_invocation: true,
  });
});

test("[deletion] exact Impact, legal replacement, concurrent facts and the isolated unbound negative", async ({
  page,
}) => {
  await setup(page);
  const ids = material().ids;
  await selectProvider(page, ids.memory_provider!);
  await selectModel(page, ids.memory!);
  let impact = await removePreview(page);
  expect(
    impact.replacement_requirement === "required" &&
      impact.reference_count === "1",
  ).toBe(true);
  expect(
    await top(page)
      .getByRole("button", { name: "清空相应用途引用", exact: true })
      .count(),
  ).toBe(0);
  await candidateProvider(page, ids.memory_replacement_provider!);
  for (const key of ["no_schema", "disabled"]) {
    const row = await candidateRow(page, ids[key]!);
    await expect(
      row.getByRole("button", { name: "选择此替代 Model", exact: true }),
    ).toBeDisabled();
  }
  await candidateProvider(page, ids.memory_provider!);
  await expect(
    (await candidateRow(page, ids.memory!)).getByRole("button", {
      name: "选择此替代 Model",
      exact: true,
    }),
  ).toBeDisabled();
  await candidateProvider(page, ids.embedding_provider!);
  await expect(
    (await candidateRow(page, ids.embedding!)).getByRole("button", {
      name: "选择此替代 Model",
      exact: true,
    }),
  ).toBeDisabled();
  await candidateProvider(page, ids.disabled_provider!);
  await expect(
    (await candidateRow(page, ids.disabled_provider_model!)).getByRole(
      "button",
      { name: "选择此替代 Model", exact: true },
    ),
  ).toBeDisabled();
  await candidateProvider(page, ids.memory_replacement_provider!);
  await choose(page, ids.memory_replacement!);
  await ipc("disable", { id: ids.memory_replacement! });
  let baseline = await dbFacts(ids.memory!),
    count = (await writes(page)).length,
    after = await perform(page, "确认删除 Model");
  await factAfter(
    page,
    after,
    (f) =>
      f.status === 400 &&
      f.code === "INVALID_ARGUMENT" &&
      f.commit_state === "not_started",
  );
  await expect(top(page)).toContainText(
    "请检查原字段长度、Unicode、启用状态及能力声明。输入不会自动改写。",
  );
  expect((await writes(page)).length).toBe(count + 1);
  expect(canonical(await dbFacts(ids.memory!)) === canonical(baseline)).toBe(
    true,
  );
  await discard(page);
  await ipc("enable", { id: ids.memory_replacement! });
  // A new required reference is added by the accepted selector API after a zero preview.
  await selectProvider(page, ids.embedding_provider!);
  await selectModel(page, ids.late_reference!);
  impact = await removePreview(page);
  expect(impact.reference_count).toBe("0");
  await ipc("selection", { stage: "embedding", id: ids.late_reference! });
  baseline = await dbFacts(ids.late_reference!);
  count = (await writes(page)).length;
  after = await perform(page, "确认删除 Model");
  await factAfter(
    page,
    after,
    (f) => f.status === 409 && f.code === "INVALID_STATE",
  );
  expect(
    canonical(await dbFacts(ids.late_reference!)) === canonical(baseline),
  ).toBe(true);
  expect((await writes(page)).length).toBe(count + 1);
  await top(page)
    .getByRole("button", { name: "重新读取并核对删除预览", exact: true })
    .click();
  await expect(top(page)).toContainText("当前用途要求明确选择合法替代");
  expect((await writes(page)).length).toBe(count + 1);
  await candidateProvider(page, ids.embedding_replacement_provider!);
  await choose(page, ids.embedding_replacement!);
  let receipt = await confirmed(
    page,
    await perform(page, "确认删除 Model"),
    "model.delete",
  );
  expect(receipt.affected_references).toBe("1");
  await ipc("selection", { stage: "embedding", id: ids.embedding! });
  // A real Model update creates the version conflict. Neither request auto-rebases.
  await selectProvider(page, ids.target_provider!);
  await selectModel(page, ids.target!);
  await removePreview(page);
  await ipc("compete", { id: ids.target! });
  baseline = await dbFacts(ids.target!);
  count = (await writes(page)).length;
  after = await perform(page, "确认删除 Model");
  await factAfter(
    page,
    after,
    (f) => f.status === 409 && f.code === "VERSION_CONFLICT",
  );
  expect(canonical(await dbFacts(ids.target!)) === canonical(baseline)).toBe(
    true,
  );
  expect((await writes(page)).length).toBe(count + 1);
  await top(page)
    .getByRole("button", { name: "重新读取并核对删除预览", exact: true })
    .click();
  await expect(top(page)).toContainText("版本：2");
  expect((await writes(page)).length).toBe(count + 1);
  receipt = await confirmed(
    page,
    await perform(page, "确认删除 Model"),
    "model.delete",
  );
  expect(receipt.version).toBe("3");
  // UI emits no DELETE for unbound external references; HTTP refusal is separate evidence.
  await selectModel(page, ids.unbound!);
  count = (await writes(page)).length;
  impact = await removePreview(page);
  expect(impact.delete_blocker).toBe("reference_adapter_unbound");
  await expect(title(page, "当前引用替换能力尚未绑定")).toBeVisible();
  expect(
    await top(page)
      .getByRole("button", { name: "确认删除 Model", exact: true })
      .count(),
  ).toBe(0);
  expect((await writes(page)).length).toBe(count);
  await discard(page);
  const negative = await ipc("unbound-http", { id: ids.unbound! });
  expect(
    negative.status === 503 &&
      negative.code === "DEPENDENCY_UNBOUND" &&
      negative.unchanged,
  ).toBe(true);
  for (const role of ["embedding", "memory", "reranker", "image"]) {
    await selectProvider(page, ids[role + "_provider"]!);
    await selectModel(page, ids[role]!);
    impact = await removePreview(page);
    expect(impact.reference_count).toBe("1");
    expect(impact.replacement_requirement).toBe(
      role === "embedding" || role === "memory" ? "required" : "optional",
    );
    await expect(
      top(page).getByRole("button", { name: "确认删除 Model", exact: true }),
    ).toBeDisabled();
    const required = impact.replacement_requirement === "required";
    if (required) {
      await candidateProvider(page, ids[role + "_replacement_provider"]!);
      await choose(page, ids[role + "_replacement"]!);
    } else
      await top(page)
        .getByRole("button", { name: "清空相应用途引用", exact: true })
        .click();
    baseline = await dbFacts(ids[role]!);
    count = (await writes(page)).length;
    receipt = await confirmed(
      page,
      await perform(page, "确认删除 Model"),
      "model.delete",
    );
    expect(receipt.affected_references).toBe("1");
    const state = await dbFacts(ids[role]!);
    expect(
      !state.exists &&
        state.target_references === 0 &&
        state.delete_receipts === 1 &&
        state.commands === baseline.commands + 1 &&
        state.audits === baseline.audits + 1 &&
        state.events === baseline.events + 1 &&
        state.selection[role] ===
          (required ? ids[role + "_replacement"] : null),
    ).toBe(true);
    expect((await writes(page)).length).toBe(count + 1);
  }
  result({
    required: true,
    optional: true,
    candidate_rules: true,
    current_references: true,
    candidate_changed: true,
    version_conflict: true,
    unbound_ui: true,
    unbound_http: true,
    atomic: true,
  });
});

test("[outcome] accepted create/update/delete response loss preserves one original command and facts", async ({
  page,
}) => {
  await setup(page);
  await selectProvider(page);
  let current: Model | undefined;
  for (const stage of ["create", "update", "delete"] as const) {
    if (stage === "create") {
      await openCreate(page);
      await fill(page, "Recovered Model");
    } else if (stage === "update") {
      await edit(page);
      await top(page)
        .getByRole("textbox", { name: "名称", exact: true })
        .fill("Recovered updated Model");
    } else await removePreview(page);
    const baseline = await dbFacts();
    await ipc("arm-drop", {
      stage,
      ...(stage === "create" ? {} : { id: current!.id }),
    });
    await perform(
      page,
      stage === "delete" ? "确认删除 Model" : "保存 Model 配置",
    );
    await expect(title(page, "请求结果未确认")).toBeVisible();
    const lost = await ipc("drop-facts");
    expect(lost.dropped === 1 && lost.replayed === 0 && !lost.armed).toBe(true);
    const accepted = await dbFacts(lost.target);
    expect(
      accepted.commands === baseline.commands + 1 &&
        accepted.audits === baseline.audits + 1 &&
        accepted.events === baseline.events + 1,
    ).toBe(true);
    if (stage === "delete")
      expect((await ipc("missing", { id: lost.target })).missing).toBe(true);
    const before = (await writes(page)).length;
    await top(page)
      .getByRole("button", { name: "检查原请求", exact: true })
      .click();
    await expect(top(page)).toContainText("已观察到历史回执");
    expect((await writes(page)).length).toBe(before);
    expect((await ipc("drop-facts")).replayed).toBe(0);
    if (stage === "delete") {
      expect(
        (await facts(page)).some(
          (f) =>
            f.path === "/api/v1/system/models/" + lost.target &&
            f.status === 404 &&
            f.code === "NOT_FOUND",
        ),
      ).toBe(true);
      await expect(title(page, "删除影响尚未确认")).toBeVisible();
    }
    const after = await perform(page, "重试原请求"),
      receipt = await confirmed(page, after, "model." + stage);
    expect(receipt.resource_id).toBe(lost.target);
    const replay = await ipc("drop-facts");
    expect(
      replay.dropped === 1 &&
        replay.replayed === 1 &&
        replay.target === lost.target,
    ).toBe(true);
    expect(canonical(await dbFacts(lost.target)) === canonical(accepted)).toBe(
      true,
    );
    expect((await writes(page)).length).toBe(before + 1);
    if (stage !== "delete")
      current = (
        await factAfter(
          page,
          after,
          (f) =>
            f.status === 200 && f.ended && f.model?.id === receipt.resource_id,
        )
      ).model;
  }
  await openCreate(page);
  await fill(page, "Confirmed read failure");
  await ipc("arm-get-fail");
  await confirmed(page, await perform(page, "保存 Model 配置"), "model.create");
  await expect(title(page, "操作已确认，当前配置读取失败")).toBeVisible();
  const count = (await writes(page)).length;
  await button(page, "刷新列表").click();
  await expect(button(page, "编辑 Model")).toBeEnabled();
  expect((await writes(page)).length).toBe(count);
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  await openCreate(page);
  await expect(
    top(page).getByRole("button", { name: "保存 Model 配置", exact: true }),
  ).toHaveAccessibleName("保存 Model 配置");
  await fill(page, "Abandoned accepted Model");
  await ipc("arm-drop", { stage: "create" });
  await perform(page, "保存 Model 配置");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  const lost = await ipc("drop-facts"),
    accepted = await dbFacts(lost.target);
  await top(page)
    .getByRole("button", { name: "放弃本次操作", exact: true })
    .click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await button(page, "刷新列表").click();
  await expect(
    page.getByText("Abandoned accepted Model", { exact: true }),
  ).toBeVisible();
  expect(canonical(await dbFacts(lost.target)) === canonical(accepted)).toBe(
    true,
  );
  result({
    create_replayed: true,
    update_replayed: true,
    delete_replayed: true,
    historical_only: true,
    confirmed_read_failure: true,
    abandoned: true,
    unique_facts: true,
  });
});

test("[read] canonical bounded pages, explicit cursors and independent replacement browsing", async ({
  page,
}) => {
  await setup(page);
  const data = material();
  const providers = (await facts(page)).filter((f) => f.providers).at(-1)!;
  expect(providers.bytes > 600000 && providers.bytes <= 2097152).toBe(true);
  expect(
    canonical(providers.providers!.items) ===
      canonical(data.providers.slice(0, 25)),
  ).toBe(true);
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  expect((await facts(page)).filter((f) => f.providers).length).toBe(1);
  expect((await facts(page)).filter((f) => f.models).length).toBe(0);
  await page
    .locator('[aria-label="主 Provider 分页"]')
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(1);
  expect(
    canonical(
      (await facts(page)).filter((f) => f.providers).at(-1)!.providers!.items,
    ) === canonical(data.providers.slice(25)),
  ).toBe(true);
  await page
    .locator('[aria-label="主 Provider 分页"]')
    .getByRole("button", { name: "上一页", exact: true })
    .click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  await selectProvider(page);
  const expected = data.models.filter(
    (m) => m.provider_id === data.ids.target_provider,
  );
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  expect(
    canonical(
      (await facts(page)).filter((f) => f.models).at(-1)!.models!.items,
    ) === canonical(expected.slice(0, 25)),
  ).toBe(true);
  await page
    .locator('[aria-label="主 Model 分页"]')
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(1);
  expect(
    canonical(
      (await facts(page)).filter((f) => f.models).at(-1)!.models!.items,
    ) === canonical(expected.slice(25)),
  ).toBe(true);
  await page
    .locator('[aria-label="主 Model 分页"]')
    .getByRole("button", { name: "上一页", exact: true })
    .click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  await selectModel(page);
  await removePreview(page);
  await expect(top(page).locator(".choices li")).toHaveCount(25);
  await top(page)
    .locator('[aria-label="替代 Provider 分页"]')
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await expect(top(page).locator(".choices li")).toHaveCount(1);
  await candidateProvider(page, data.ids.embedding_provider!);
  await expect(
    top(page).getByRole("button", { name: "选择此替代 Model", exact: true }),
  ).toBeDisabled();
  await top(page)
    .getByRole("button", { name: "返回替代 Providers", exact: true })
    .click();
  await expect(top(page).locator(".choices li")).toHaveCount(25);
  await candidateProvider(page, data.ids.target_provider!);
  await expect(top(page).locator(".choices li")).toHaveCount(25);
  await top(page)
    .locator('[aria-label="替代 Model 分页"]')
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await expect(top(page).locator(".choices li")).toHaveCount(1);
  await discard(page);
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  await expect(
    page
      .locator('[aria-label="主 Model 分页"]')
      .getByRole("button", { name: "上一页", exact: true }),
  ).toBeDisabled();
  await selectProvider(page, data.ids.empty_provider!);
  await expect(title(page, "当前页没有 Model")).toBeVisible();
  await expect(
    page
      .locator('[aria-label="Model 页面操作"]')
      .getByRole("button", { name: "创建 Model", exact: true }),
  ).toBeEnabled();
  await selectProvider(page);
  let altered = false;
  await page.route("**/api/v1/system/models?*", async (route) => {
    const url = new URL(route.request().url());
    if (!altered && url.searchParams.has("cursor")) {
      altered = true;
      url.searchParams.set("cursor", "owned-invalid-cursor");
      await route.continue({ url: url.toString() });
    } else await route.continue();
  });
  const after = await seq(page);
  await page
    .locator('[aria-label="主 Model 分页"]')
    .getByRole("button", { name: "下一页", exact: true })
    .click();
  await factAfter(page, after, (f) => f.code === "CURSOR_INVALID");
  await expect(title(page, "Model 列表读取失败")).toBeVisible();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(0);
  expect(
    (await facts(page)).filter(
      (f) => f.sequence > after && f.path.startsWith("/api/v1/system/models?"),
    ).length,
  ).toBe(1);
  await page.unroute("**/api/v1/system/models?*");
  await button(page, "返回首页重新加载").click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  await ipc("arm-get-fail");
  await button(page, "刷新列表").click();
  await expect(title(page, "当前 Provider 读取失败")).toBeVisible();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(0);
  await button(page, "重新读取当前 Provider").click();
  await expect(page.locator(".model-table tbody tr")).toHaveCount(25);
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  expect((await writes(page)).length).toBe(0);
  result({
    canonical: true,
    independent_pages: true,
    empty: true,
    failure: true,
    bad_cursor: true,
    large_page: true,
    providers: data.providers.length,
    models: expected.length,
    large_bytes: providers.bytes,
  });
});

test("[authority] real CRUD/Impact/lookup authorization and exact Session invalidation", async ({
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
  await page.goto("/system/models");
  await expect(title(page, "Models")).toBeVisible();
  await selectProvider(page);
  await openCreate(page);
  await fill(page, "Authority draft");
  let original = await session(page);
  await ipc("demote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  let after = await perform(page, "保存 Model 配置");
  await factAfter(
    page,
    after,
    (f) => f.status === 403 && f.code === "FORBIDDEN" && f.method === "POST",
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  await button(page, "重新检查权限").click();
  await expect(title(page, "Models")).toBeVisible();
  await selectProvider(page);
  await selectModel(page);
  original = await session(page);
  let held = false;
  await page.route(
    "**/api/v1/system/models/*/deletion-impact",
    async (route) => {
      if (!held) {
        held = true;
        await ipc("demote", {
          user_id: original.user_id,
          session_id: original.session_id,
        });
      }
      await route.continue();
    },
  );
  after = await seq(page);
  await button(page, "删除 Model").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.status === 403 &&
      f.code === "FORBIDDEN" &&
      f.path.endsWith("/deletion-impact"),
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await page.unroute("**/api/v1/system/models/*/deletion-impact");
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  await button(page, "重新检查权限").click();
  await expect(title(page, "Models")).toBeVisible();
  await selectProvider(page);
  await openCreate(page);
  await fill(page, "Lookup authority draft");
  await ipc("arm-drop", { stage: "create" });
  await perform(page, "保存 Model 配置");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  original = await session(page);
  held = false;
  await page.route("**/api/v1/system/model-commands/lookup", async (route) => {
    if (!held) {
      held = true;
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
      f.status === 403 && f.code === "FORBIDDEN" && f.path.endsWith("/lookup"),
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await page.unroute("**/api/v1/system/model-commands/lookup");
  await ipc("promote", {
    user_id: original.user_id,
    session_id: original.session_id,
  });
  await button(page, "重新检查权限").click();
  await expect(title(page, "Models")).toBeVisible();
  await selectProvider(page);
  await openCreate(page);
  await fill(page, "Revoked draft");
  await ipc("arm-drop", { stage: "create" });
  await perform(page, "保存 Model 配置");
  await expect(title(page, "请求结果未确认")).toBeVisible();
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
      f.status === 401 &&
      f.code === "SESSION_REVOKED" &&
      f.path === "/api/v1/session",
  );
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  await login(page, "admin");
  await page.goto("/system/models");
  await expect(title(page, "Models")).toBeVisible();
  const fresh = await session(page);
  expect(
    fresh.user_id === original.user_id &&
      fresh.session_id !== original.session_id,
  ).toBe(true);
  await selectProvider(page);
  await openCreate(page);
  await expect(
    top(page).getByRole("textbox", { name: "名称", exact: true }),
  ).toHaveValue("");
  expect(
    await top(page)
      .getByRole("button", { name: "重试原请求", exact: true })
      .count(),
  ).toBe(0);
  await fill(page, "Explicit switch draft");
  await ipc("arm-drop", { stage: "create" });
  await perform(page, "保存 Model 配置");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/models");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(page.locator('[role="dialog"]')).toHaveCount(0);
  result({
    denied: true,
    write_forbidden: true,
    impact_forbidden: true,
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
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await focusContained(page);
  expect((await writes(page)).length).toBe(count);
}
async function layout(page: Page, width: number) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
  const faults = await page
    .locator(".model-table")
    .evaluate((region, width) => {
      const area = region.getBoundingClientRect(),
        faults: string[] = [];
      if (region.scrollWidth > region.clientWidth + 1)
        faults.push("table overflow");
      for (const row of region.querySelectorAll("tbody tr")) {
        const cells = [...row.querySelectorAll("td")];
        if (cells.length !== 6) faults.push("fields");
        for (const cell of cells) {
          const r = cell.getBoundingClientRect();
          if (
            r.left < area.left - 1 ||
            r.right > area.right + 1 ||
            r.width <= 0 ||
            getComputedStyle(cell).whiteSpace !== "normal"
          )
            faults.push("field bounds");
          if (
            width <= 700 &&
            getComputedStyle(cell, "::before").content === "none"
          )
            faults.push("stacked label");
        }
      }
      const caption = region.querySelector("caption")!;
      const style = getComputedStyle(caption);
      if (!(
        caption instanceof HTMLElement &&
        caption.offsetParent === region &&
        style.position === "absolute" &&
        style.width === "1px" &&
        style.overflow === "hidden" &&
        style.clipPath === "inset(50%)"
      ))
        faults.push("caption");
      return faults;
    }, width);
  expect(faults).toEqual([]);
}
test("[navigation] same-session cohost recovery, real focus and eight layouts", async ({
  page,
}) => {
  await setup(page, "admin", "/system/users");
  await expect(
    page
      .getByRole("navigation", { name: "系统设置", exact: true })
      .getByRole("link"),
  ).toHaveCount(5);
  for (const leaf of ["待注册邀请", "Providers", "Models"]) {
    await page.getByRole("link", { name: leaf, exact: true }).click();
    await expect(title(page, leaf)).toBeVisible();
  }
  await selectProvider(page);
  await openCreate(page);
  await fill(page, "Navigation draft");
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await checkingConfirmation(page, true, "创建 Model");
  await expect(
    top(page).getByRole("textbox", { name: "名称", exact: true }),
  ).toHaveValue("Navigation draft");
  await discard(page);
  await selectModel(page);
  await removePreview(page);
  await candidateProvider(page, material().ids.replacement_provider!);
  await choose(page, material().ids.replacement!);
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await checkingConfirmation(page, false, "删除 Model");
  await expect(top(page)).toContainText("已选择替代");
  await discard(page);
  await openCreate(page);
  await fill(page, "Uncertain navigation");
  await ipc("arm-drop", { stage: "create" });
  await perform(page, "保存 Model 配置");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await top(page).getByRole("button", { name: "取消", exact: true }).click();
  await checkingConfirmation(page, false, "创建 Model");
  await discard(page);
  await button(page, "刷新列表").click();
  await expect(button(page, "创建 Model")).toBeEnabled();
  await openCreate(page);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(button(page, "创建 Model")).toBeFocused();
  await openCreate(page);
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(button(page, "创建 Model")).toBeFocused();
  await page.getByRole("link", { name: "用户", exact: true }).click();
  await expect(title(page, "用户")).toBeVisible();
  await page.goBack();
  await expect(title(page, "Models")).toBeVisible();
  await selectProvider(page);
  await selectModel(page);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await layout(page, width);
      if (width === 390) {
        await openCreate(page);
        expect(
          await top(page).evaluate((n) => {
            const r = n.getBoundingClientRect();
            return r.left >= 0 && r.right <= innerWidth + 1;
          }),
        ).toBe(true);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        await expect(top(page).getByRole("link")).toHaveCount(5);
        await focusContained(page);
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(toggle).toBeFocused();
      }
      await expect(page.locator(".ui-overlay")).toHaveCount(0);
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
      await expect(title(page, "Models")).toBeInViewport();
      const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
      if (images)
        await page.screenshot({
          path: join(images, `models-${theme}-${width}.png`),
          fullPage: true,
        });
      layouts++;
    }
  }
  expect((await ipc("failure-facts")).session_failures).toBe(1);
  result({
    navigation: true,
    dirty: true,
    checking: true,
    selection: true,
    uncertain: true,
    dialogs: true,
    focus: true,
    layouts,
  });
});
