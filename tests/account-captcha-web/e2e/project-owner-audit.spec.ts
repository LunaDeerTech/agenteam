import { test, expect, type Page, type Locator } from "@playwright/test";
import { createHash } from "node:crypto";
import {
  existsSync,
  readFileSync,
  readdirSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import {
  createProjectAuditAPI,
  auditFilterActions,
  auditFilterResourceKinds,
  type AuditQuery,
  type ProjectAuditPage,
  type ProjectAuditRecord,
} from "../../../web/src/api/project-audit";
import { AccountFailure } from "../../../web/src/api/client";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const evidence = process.env.AGENTEAM_PROJECT_AUDIT_WEB_EVIDENCE!;
const dist = process.env.AGENTEAM_PROJECT_AUDIT_WEB_DIST!;
const repository = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN!;
if (![directory, evidence, dist].every((p) => p && isAbsolute(p)))
  throw new Error("OWNED_PROJECT_AUDIT_PATHS_REQUIRED");

type Credential = {
  email: string;
  password: string;
  user_id: string;
  username: string;
};
type ProjectLocator = {
  id: string;
  username: string;
  name: string;
  normalized_name: string;
  owner_user_id: string;
  initialized: boolean;
  lifecycle: string;
};
const projectKeys = [
  "main",
  "second",
  "other",
  "admin",
  "dotted",
  "archiving",
  "archived",
  "deleting",
  "pending",
] as const;
type ProjectKey = (typeof projectKeys)[number];
type Material = {
  admin: Credential;
  owner: Credential;
  other: Credential;
  projects: Record<ProjectKey, ProjectLocator>;
  missing_id: string;
  expected: {
    main_record_ids: string[];
    main_actions: string[];
    main_producer_families: string[];
  };
  system: null | {
    ids: Record<string, string>;
    names: Record<string, string>;
    selector_id: string;
    summary_id: string;
    selection: unknown;
    summary: unknown;
  };
};
const uuid = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    v,
  );
const object = (v: unknown): Record<string, unknown> => {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw new Error("CLOSED_OBJECT_REQUIRED");
  return v as Record<string, unknown>;
};
function exact(value: unknown, keys: readonly string[]) {
  const v = object(value);
  expect(Object.keys(v).sort()).toEqual([...keys].sort());
  return v;
}
function locator(value: unknown): ProjectLocator {
  const v = exact(value, [
    "id",
    "username",
    "name",
    "normalized_name",
    "owner_user_id",
    "initialized",
    "lifecycle",
  ]);
  expect(
    uuid(v.id) && uuid(v.owner_user_id) && typeof v.initialized === "boolean",
  ).toBe(true);
  expect(
    [v.username, v.name, v.normalized_name, v.lifecycle].every(
      (s) => typeof s === "string",
    ),
  ).toBe(true);
  return v as ProjectLocator;
}
function material(): Material {
  const value = JSON.parse(
    readFileSync(join(directory, "project-audit-material.json"), "utf8"),
  );
  exact(value, [
    "admin",
    "owner",
    "other",
    "projects",
    "missing_id",
    "expected",
    "system",
  ]);
  for (const who of ["admin", "owner", "other"] as const) {
    const c = exact(value[who], ["email", "password", "user_id", "username"]);
    // Diagnostics must never receive credentials, even on a failed assertion.
    expect(
      uuid(c.user_id) &&
        typeof c.email === "string" &&
        typeof c.password === "string" &&
        typeof c.username === "string",
    ).toBe(true);
  }
  exact(value.projects, projectKeys);
  for (const key of projectKeys) locator(value.projects[key]);
  exact(value.expected, [
    "main_record_ids",
    "main_actions",
    "main_producer_families",
  ]);
  expect(
    uuid(value.missing_id) && value.expected.main_record_ids.every(uuid),
  ).toBe(true);
  expect(value.expected.main_producer_families).toEqual([
    "project",
    "secret",
    "model",
  ]);
  if (value.system !== null)
    exact(value.system, [
      "ids",
      "names",
      "selector_id",
      "summary_id",
      "selection",
      "summary",
    ]);
  return value;
}

const countKeys = [
  "browser_list_gets",
  "browser_detail_gets",
  "setup_audit_gets",
  "control_audit_gets",
  "browser_project_mutations",
  "browser_command_lookups",
  "held",
  "joined",
  "cuts",
  "failures",
  "session_failures",
  "server_started",
  "server_finished",
] as const;
type Counts = Record<(typeof countKeys)[number], number>;
type IPC =
  | { action: "counts" | "release" | "session-fail" }
  | { action: "snapshot"; project: ProjectKey; ids: string[] }
  | {
      action: "arm-hold" | "arm-cut" | "arm-failure";
      project: ProjectKey;
      audit_id?: string;
    }
  | { action: "logout"; session_id: string }
  | { action: "rename-reuse"; project: "main" };
let ipcSequence = 0;
async function ipc(request: IPC): Promise<Record<string, unknown>> {
  const sequence = ++ipcSequence;
  expect(sequence <= 128).toBe(true);
  const file = join(directory, "project-audit-ipc.json"),
    ack = join(directory, `project-audit-ack-${sequence}.json`);
  writeFileSync(file + ".tmp", JSON.stringify({ sequence, ...request }), {
    mode: 0o600,
  });
  renameSync(file + ".tmp", file);
  await expect.poll(() => existsSync(ack), { timeout: 8_000 }).toBe(true);
  const envelope = exact(JSON.parse(readFileSync(ack, "utf8")), [
    "sequence",
    "result",
  ]);
  expect(envelope.sequence).toBe(sequence);
  const value = object(envelope.result);
  switch (request.action) {
    case "counts":
      exact(value, countKeys);
      expect(
        Object.values(value).every(
          (n) => Number.isSafeInteger(n) && Number(n) >= 0,
        ),
      ).toBe(true);
      break;
    case "snapshot":
      exact(value, ["records"]);
      expect(
        Array.isArray(value.records) &&
          value.records.length === request.ids.length,
      ).toBe(true);
      for (const row of value.records as unknown[])
        exact(row, [
          "audit_id",
          "created_at",
          "scope",
          "project_id",
          "actor",
          "action",
          "outcome",
          "resource",
          "metadata",
          "associations",
          "summary",
        ]);
      break;
    case "logout":
      exact(value, ["revoked", "logout_audit"]);
      expect(value.revoked === true && value.logout_audit === true).toBe(true);
      break;
    case "rename-reuse":
      exact(value, ["renamed", "replacement"]);
      locator(value.renamed);
      locator(value.replacement);
      break;
    default:
      exact(value, []);
  }
  return value;
}
const counts = async () => (await ipc({ action: "counts" })) as Counts;
async function joined(minimum: number) {
  let last: Counts | undefined;
  await expect
    .poll(
      async () => {
        last = await counts();
        return (
          last.joined === minimum &&
          last.held === minimum &&
          last.server_finished === last.server_started
        );
      },
      { timeout: 5_000 },
    )
    .toBe(true);
  return last!;
}

type Fact = {
  document: string;
  sequence: number;
  start: number;
  end?: number;
  method: string;
  path: string;
  query: string;
  status: number;
  media: string;
  requestID: string;
  contentLength: string;
  ended: boolean;
  eof: boolean;
  bytes: number;
  bodyPresent: boolean;
  mutationHeaders: boolean;
  chunks?: number[][];
  code?: string;
  session?: { user_id: string; session_id: string; role: string };
};
type Observed = Window & {
  __projectAuditFacts?: Fact[];
  __projectAuditObserve?: (fact: Fact) => Promise<void>;
};
const archive = new Map<string, Fact>();
const wireOverrides = new Map<
  string,
  { path: string; query: string; reason: string }
>();
const keyOf = (f: Fact) => `${f.document}:${f.sequence}`;
const isAudit = (f: Fact) =>
  /^\/api\/v1\/projects\/[^/]+\/audit(?:\/[^/]+)?$/.test(f.path);
const rawOf = (f: Fact) =>
  Buffer.concat((f.chunks ?? []).map((chunk) => Buffer.from(chunk)));
const bodyOf = <T>(f: Fact): T => JSON.parse(rawOf(f).toString("utf8")) as T;

// Observe only the original native fetch and reader.read promises. Neither
// promise nor Response/reader is replaced; no clone, tee, extra read or cancel.
// Completion here is not Cookie-owner or server-handler completion evidence.
function observeNative() {
  const scope = window as Observed;
  scope.__projectAuditFacts = [];
  const documentID = crypto.randomUUID();
  let sequence = 0,
    clock = 0;
  const nativeFetch = window.fetch;
  window.fetch = function (...args: Parameters<typeof fetch>) {
    const original = Reflect.apply(nativeFetch, this, args) as ReturnType<
      typeof fetch
    >;
    try {
      const [input, init] = args,
        url = new URL(
          input instanceof Request ? input.url : String(input),
          location.origin,
        );
      if (
        url.origin !== location.origin ||
        !url.pathname.startsWith("/api/v1/")
      )
        return original;
      const audit =
          /^\/api\/v1\/projects\/[0-9a-f-]{36}\/audit(?:\/[0-9a-f-]{36})?$/.test(
            url.pathname,
          ),
        session = url.pathname === "/api/v1/session",
        headers = new Headers(
          init?.headers ??
            (input instanceof Request ? input.headers : undefined),
        );
      const fact: Fact = {
        document: documentID,
        sequence: ++sequence,
        start: ++clock,
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        path: url.pathname,
        query: audit ? url.search.slice(1) : "",
        status: 0,
        media: "",
        requestID: "",
        contentLength: "",
        ended: false,
        eof: false,
        bytes: 0,
        bodyPresent:
          init?.body != null ||
          (input instanceof Request && input.body !== null),
        mutationHeaders:
          headers.has("X-CSRF-Token") || headers.has("Idempotency-Key"),
      };
      const publish = () => {
        void scope.__projectAuditObserve?.(fact).catch(() => undefined);
      };
      const finish = () => {
        if (!fact.ended) {
          fact.ended = true;
          fact.end = ++clock;
          publish();
        }
      };
      scope.__projectAuditFacts!.push(fact);
      publish();
      void original
        .then((response) => {
          fact.status = response.status;
          if (audit) {
            fact.media = response.headers.get("Content-Type") ?? "";
            fact.requestID = response.headers.get("X-Request-ID") ?? "";
            fact.contentLength = response.headers.get("Content-Length") ?? "";
          }
          const body = response.body,
            media = response.headers
              .get("Content-Type")
              ?.split(";", 1)[0]
              ?.trim()
              .toLowerCase();
          if (!audit && !session) {
            finish();
            return;
          }
          if (
            !body ||
            !["application/json", "application/problem+json"].includes(
              media ?? "",
            )
          ) {
            finish();
            return;
          }
          const cap = audit && response.status === 200 ? 1 << 20 : 600000;
          let text = "",
            collect = true;
          const decoder = new TextDecoder("utf-8", { fatal: true });
          if (audit) fact.chunks = [];
          const nativeGet = body.getReader;
          Object.defineProperty(body, "getReader", {
            configurable: true,
            writable: true,
            value: function (
              this: ReadableStream<Uint8Array>,
              ...readerArgs: unknown[]
            ) {
              const reader = Reflect.apply(
                nativeGet,
                this,
                readerArgs,
              ) as ReadableStreamDefaultReader<Uint8Array>;
              Object.defineProperty(body, "getReader", { value: nativeGet });
              const nativeRead = reader.read;
              Object.defineProperty(reader, "read", {
                configurable: true,
                writable: true,
                value: function (
                  this: ReadableStreamDefaultReader<Uint8Array>,
                  ...readArgs: unknown[]
                ) {
                  const pending = Reflect.apply(
                    nativeRead,
                    this,
                    readArgs,
                  ) as Promise<ReadableStreamReadResult<Uint8Array>>;
                  void pending
                    .then(
                      ({ done, value }) => {
                        try {
                          if (value) {
                            fact.bytes += value.byteLength;
                            if (fact.bytes > cap) {
                              collect = false;
                              text = "";
                              delete fact.chunks;
                            } else if (collect) {
                              text += decoder.decode(value, { stream: true });
                              if (audit) fact.chunks!.push(Array.from(value));
                            }
                          }
                          if (done) {
                            fact.eof = true;
                            if (collect) {
                              const v = JSON.parse(text + decoder.decode());
                              if (
                                session &&
                                response.status === 200 &&
                                v &&
                                typeof v === "object"
                              ) {
                                const id = (s: unknown) =>
                                  typeof s === "string" &&
                                  /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
                                    s,
                                  );
                                if (
                                  id(v.user?.id) &&
                                  id(v.session?.id) &&
                                  ["admin", "user"].includes(v.user?.role)
                                )
                                  fact.session = {
                                    user_id: v.user.id,
                                    session_id: v.session.id,
                                    role: v.user.role,
                                  };
                              }
                              if (
                                audit &&
                                response.status !== 200 &&
                                typeof v?.code === "string" &&
                                [
                                  "NOT_FOUND",
                                  "FORBIDDEN",
                                  "UNAUTHENTICATED",
                                  "SESSION_REVOKED",
                                  "PROJECT_NOT_ACTIVE",
                                  "INVALID_STATE",
                                  "INVALID_ARGUMENT",
                                  "CURSOR_INVALID",
                                  "DEPENDENCY_UNAVAILABLE",
                                ].includes(v.code)
                              )
                                fact.code = v.code;
                            }
                          }
                        } catch {
                          collect = false;
                          delete fact.chunks;
                        } finally {
                          if (done) {
                            text = "";
                            collect = false;
                            finish();
                          }
                        }
                      },
                      () => {
                        text = "";
                        collect = false;
                        finish();
                      },
                    )
                    .catch(() => undefined);
                  return pending;
                },
              });
              return reader;
            },
          });
        }, finish)
        .catch(() => undefined);
    } catch {
      /* Observation does not substitute a native result. */
    }
    return original;
  };
}
async function observe(page: Page) {
  await page.exposeFunction("__projectAuditObserve", (fact: Fact) => {
    archive.set(keyOf(fact), fact);
  });
  await page.addInitScript(observeNative);
}
async function facts(page: Page) {
  const values = await page.evaluate(
    () => (window as Observed).__projectAuditFacts ?? [],
  );
  for (const value of values) archive.set(keyOf(value), value);
  return values;
}
const sequence = async (page: Page) =>
  (await facts(page)).at(-1)?.sequence ?? 0;
async function after(page: Page, start: number, match: (f: Fact) => boolean) {
  let found: Fact | undefined;
  await expect
    .poll(
      async () => {
        found = (await facts(page)).find((f) => f.sequence > start && match(f));
        return !!found;
      },
      { timeout: 5_000 },
    )
    .toBe(true);
  return found!;
}
const button = (scope: Page | Locator, name: string) =>
  scope.getByRole("button", { name, exact: true });
const heading = (page: Page, name: string) =>
  page.getByRole("heading", { name, exact: true });
const filter = (page: Page, key: string) =>
  page.locator(`#project-audit-filter-${key}`);
const detailRegion = (page: Page) =>
  page.getByRole("region", { name: "审计详情", exact: true });
const path = (p: ProjectLocator, leaf = "audit") =>
  `/${p.username}/${p.normalized_name}/settings/${leaf}`;
const endpoint = (p: ProjectLocator, id?: string) =>
  `/api/v1/projects/${p.id}/audit${id ? "/" + id : ""}`;
const auditCount = async (page: Page) =>
  (await facts(page)).filter(isAudit).length;

async function login(page: Page, credential: Credential) {
  const start = await sequence(page);
  try {
    await page.locator("#login-email").fill(credential.email);
    await page.locator("#login-password").fill(credential.password);
  } catch {
    throw new Error("PRIVATE_PROJECT_AUDIT_LOGIN_INPUT_FAILED");
  }
  await button(page, "登录").click();
  const current = await after(
    page,
    start,
    (f) =>
      f.path === "/api/v1/session" &&
      f.eof &&
      f.session?.user_id === credential.user_id,
  );
  await expect(button(page, "退出登录")).toBeEnabled();
  return current.session!;
}
async function ready(page: Page, p: ProjectLocator, start = 0) {
  const fact = await after(
    page,
    start,
    (f) =>
      f.path === endpoint(p) &&
      f.status === 200 &&
      f.ended &&
      f.eof &&
      !!f.chunks,
  );
  await expect(heading(page, "项目审计")).toBeVisible();
  await expect(button(page, "重新读取")).toBeEnabled();
  expect(!fact.bodyPresent && !fact.mutationHeaders).toBe(true);
  return fact;
}
async function enter(
  page: Page,
  data: Material,
  who: "owner" | "admin" | "other" = "owner",
  key: ProjectKey = "main",
) {
  await observe(page);
  const response = await page.goto(path(data.projects[key]));
  expect(response?.status()).toBe(200);
  const session = await login(page, data[who]);
  return { session, fact: await ready(page, data.projects[key]) };
}
async function listAction(page: Page, p: ProjectLocator, name: string) {
  const start = await sequence(page);
  await button(page, name).click();
  return ready(page, p, start);
}
async function detail(page: Page, p: ProjectLocator, id: string) {
  const start = await sequence(page);
  await button(page, "查看详情 " + id).click();
  const fact = await after(
    page,
    start,
    (f) => f.path === endpoint(p, id) && f.status === 200 && f.ended && f.eof,
  );
  await expect(detailRegion(page)).toContainText(id);
  await expect(button(page, "重新读取详情")).toBeEnabled();
  expect(bodyOf<ProjectAuditRecord>(fact).audit_id).toBe(id);
  return fact;
}
async function back(page: Page, id?: string) {
  const before = await auditCount(page);
  await button(page, "返回列表").click();
  await expect(heading(page, "审计记录")).toBeVisible();
  expect(await auditCount(page)).toBe(before);
  if (id) await expect(button(page, "查看详情 " + id)).toBeFocused();
}
async function fillFilters(page: Page, values: Record<string, string>) {
  for (const [name, value] of Object.entries(values)) {
    if (["action", "outcome", "actor_kind", "resource_kind"].includes(name))
      await filter(page, name).selectOption(value);
    else await filter(page, name).fill(value);
  }
}
async function compare(page: ProjectAuditPage, key: ProjectKey) {
  const snapshot = await ipc({
    action: "snapshot",
    project: key,
    ids: page.items.map((r) => r.audit_id),
  });
  expect(page.items).toEqual(snapshot.records);
}
// A same-document browser history event exercises the installed router. This
// does not call a controller or bypass guards, and puts no filter/ID in the URL.
async function navigate(page: Page, target: string) {
  await page.evaluate((target) => {
    const old = history.state as { position?: number } | null;
    history.pushState(
      {
        ...old,
        back: location.pathname,
        current: target,
        forward: null,
        position: (old?.position ?? 0) + 1,
        replaced: false,
      },
      "",
      target,
    );
    dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
  }, target);
}

const schemaProgram = String.raw`
import calendar,hashlib,json,pathlib,re,sys
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
try:
 root=pathlib.Path(sys.argv[1]); evidence=pathlib.Path(sys.argv[2]); base=root/'api/openapi'
 docs={n:json.loads((base/n).read_bytes()) for n in ['common.json','project-audit.json']}
 registry=Registry().with_resources(((base/n).as_uri(),Resource.from_contents(d,default_specification=DRAFT202012)) for n,d in docs.items())
 checker=FormatChecker()
 @checker.checks('date-time')
 def formal_instant(v):
  # Same formal Gregorian-year-0000 checker as the accepted Audit HTTP
  # real-prep03/parse-real-body.py; schema still enforces canonical precision.
  if not isinstance(v,str):return True
  m=re.fullmatch(r'(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?Z',v)
  if not m:return False
  y,mo,d,h,mi,s=map(int,m.groups())
  return 1<=mo<=12 and 1<=d<=calendar.monthrange(y,mo)[1] and h<24 and mi<60 and s<60
 selected=json.loads((evidence/'same-body-input.json').read_bytes()); count=0
 assert selected and isinstance(selected,list)
 for selected_body in selected:
  name=selected_body['sidecar']; assert pathlib.Path(name).name==name
  m=json.loads((evidence/name).read_bytes()); raw=(evidence/m['body_file']).read_bytes()
  assert m['source']=='browser' and m['body_stage']=='complete_formal_upstream'
  assert m['transfer_kind'] in ['forwarded','hold'] and selected_body['browser_eof'] is True
  assert len(raw)==m['body_bytes']==selected_body['bytes']
  assert hashlib.sha256(raw).hexdigest()==m['body_sha256']==selected_body['sha256']
  assert m['body_file']=='body-'+m['body_sha256']+'.json'
  assert m['source_run'].startswith('TestAccountProjectOwnerAuditWeb') and len(m['input_hash'])==64
  route='/api/v1/projects/{project_id}/audit'+('/{id}' if m['audit_id'] else '')
  response=docs['project-audit.json']['paths'][route]['get']['responses'][str(m['status'])]
  media=m['content_type'].split(';')[0].strip().lower(); ref=response['content'][media]['schema']
  schema=dict(docs['project-audit.json']);schema['$id']=(base/'project-audit.json').as_uri();schema.update(ref)
  Draft202012Validator(schema,registry=registry,format_checker=checker).validate(json.loads(raw));count+=1
 print(count)
except Exception:
 print('SAFE_PROJECT_AUDIT_SAME_BODY_SCHEMA_FAILED',file=sys.stderr);sys.exit(1)
`;
type Sidecar = {
  sequence: number;
  source_run: string;
  input_hash: string;
  source: string;
  method: string;
  endpoint: string;
  query: string;
  status: number;
  content_type: string;
  content_length: string;
  request_id: string;
  project_id: string;
  audit_id: string;
  body_file: string;
  body_sha256: string;
  body_bytes: number;
  transfer_kind: string;
  body_stage: string;
};
async function verifyBodies(page: Page) {
  await facts(page);
  const actual = [...archive.values()].filter(isAudit),
    sidecars = readdirSync(evidence)
      .filter((name) => /^response-\d+\.json$/.test(name))
      .map((name) => ({
        name,
        meta: JSON.parse(readFileSync(join(evidence, name), "utf8")) as Sidecar,
      }));
  const matched: {
    sidecar: string;
    browser_eof: true;
    bytes: number;
    sha256: string;
    chunks: number[];
    controlled_wire: string | null;
  }[] = [];
  const classification: Record<string, number> = {
    incomplete: 0,
    controlled_failure: 0,
    success: 0,
    problem: 0,
  };
  for (const fact of actual) {
    expect(
      fact.method === "GET" && !fact.bodyPresent && !fact.mutationHeaders,
    ).toBe(true);
    if (!fact.ended || !fact.eof || !fact.chunks) {
      classification.incomplete!++;
      continue;
    }
    const wire = wireOverrides.get(keyOf(fact)) ?? {
      path: fact.path,
      query: fact.query,
      reason: null,
    };
    const found = sidecars.find(
      ({ meta }) =>
        meta.source === "browser" &&
        meta.request_id === fact.requestID &&
        meta.endpoint === wire.path &&
        meta.query === wire.query,
    );
    expect(!!found).toBe(true);
    const { name, meta } = found!;
    exact(meta, [
      "sequence",
      "source_run",
      "input_hash",
      "source",
      "method",
      "endpoint",
      "query",
      "status",
      "content_type",
      "content_length",
      "request_id",
      "project_id",
      "audit_id",
      "body_file",
      "body_sha256",
      "body_bytes",
      "transfer_kind",
      "body_stage",
    ]);
    if (meta.transfer_kind === "failure") {
      classification.controlled_failure!++;
      continue;
    }
    expect(["forwarded", "hold"].includes(meta.transfer_kind)).toBe(true);
    expect(
      meta.status === fact.status &&
        meta.content_type === fact.media &&
        fact.contentLength === String(meta.body_bytes),
    ).toBe(true);
    expect(
      meta.method === "GET" && meta.body_stage === "complete_formal_upstream",
    ).toBe(true);
    expect(meta.body_file).toBe(`body-${meta.body_sha256}.json`);
    const raw = readFileSync(join(evidence, meta.body_file)),
      received = rawOf(fact),
      sha = createHash("sha256").update(received).digest("hex");
    expect(
      raw.equals(received) &&
        received.byteLength === fact.bytes &&
        raw.byteLength === meta.body_bytes &&
        sha === meta.body_sha256,
    ).toBe(true);
    const api = createProjectAuditAPI(async (url, init) => {
      expect(url).toBe(meta.endpoint + (meta.query ? "?" + meta.query : ""));
      expect(
        init.method === "GET" &&
          !init.body &&
          !new Headers(init.headers).has("X-CSRF-Token") &&
          !new Headers(init.headers).has("Idempotency-Key"),
      ).toBe(true);
      let index = 0;
      const stream = new ReadableStream<Uint8Array>({
        pull(controller) {
          const chunk = fact.chunks![index++];
          if (chunk) controller.enqueue(Uint8Array.from(chunk));
          else controller.close();
        },
      });
      return new Response(stream, {
        status: meta.status,
        headers: {
          "Content-Type": meta.content_type,
          "X-Request-ID": meta.request_id,
          "Content-Length": fact.contentLength,
        },
      });
    });
    const signal = new AbortController().signal;
    const query = Object.fromEntries(
      new URLSearchParams(meta.query),
    ) as AuditQuery;
    let outcome: "success" | "problem";
    try {
      const value = meta.audit_id
        ? await api.get(meta.project_id, meta.audit_id, signal)
        : await api.list(meta.project_id, query, signal);
      expect(meta.status).toBe(200);
      expect(value).toEqual(JSON.parse(raw.toString("utf8")));
      outcome = "success";
    } catch (error) {
      expect(
        meta.status !== 200 &&
          error instanceof AccountFailure &&
          error.kind === "problem",
      ).toBe(true);
      outcome = "problem";
    }
    classification[outcome]!++;
    matched.push({
      sidecar: name,
      browser_eof: true,
      bytes: received.byteLength,
      sha256: sha,
      chunks: fact.chunks.map((c) => c.length),
      controlled_wire: wire.reason,
    });
  }
  expect(matched.length > 0).toBe(true);
  writeFileSync(
    join(evidence, "same-body-input.json"),
    JSON.stringify(matched),
    { mode: 0o600 },
  );
  const checked = spawnSync(
    "python3",
    ["-c", schemaProgram, repository, evidence],
    { encoding: "utf8", timeout: 6_000, maxBuffer: 4096 },
  );
  writeFileSync(
    join(evidence, "schema-validation.json"),
    JSON.stringify({
      status: checked.status,
      signal: checked.signal,
      stdout: checked.stdout,
      stderr: checked.stderr,
      failed_to_run: !!checked.error,
    }),
    { mode: 0o600 },
  );
  expect(checked.status === 0 && /^\d+\s*$/.test(checked.stdout)).toBe(true);
  expect(Number(checked.stdout)).toBe(matched.length);
  writeFileSync(
    join(evidence, "browser-observations.json"),
    JSON.stringify({
      classification,
      controlledWire: [...wireOverrides.values()],
      facts: actual.map(({ chunks, session: _session, ...fact }) => ({
        ...fact,
        chunks: chunks?.map((c) => c.length),
      })),
    }),
    { mode: 0o600 },
  );
  return {
    browser_audit_gets: actual.filter((f) => f.ended && f.eof).length,
    schema_bodies: matched.length,
    client_bodies: matched.length,
  };
}
const required = {
  read: [
    "complete_projection",
    "typed_families",
    "explicit_pagination",
    "all_filters",
    "valid_empty",
    "cursor_recovery",
    "same_body_schema",
    "same_body_client",
    "zero_mutations",
  ],
  authority: [
    "owner_and_admin_owned",
    "non_owner_hidden",
    "both_gets",
    "life_gates",
    "detail_missing",
    "cut_reread",
    "failure_reread",
    "list_cancel_join",
    "detail_cancel_join",
    "formal_logout",
    "late_isolated",
    "cross_project",
    "cross_domain",
    "zero_mutations",
  ],
  navigation: [
    "dotted_return",
    "raw_rejection",
    "settings_current",
    "default_general",
    "local_dirty_leave",
    "existing_draft_guards",
    "inline_focus",
    "checking_new_page",
    "drawer",
    "reduced_motion",
    "no_overflow",
    "no_debug",
    "zero_mutations",
  ],
} as const;
async function finish(
  page: Page,
  mode: keyof typeof required,
  checks: Record<string, true>,
  layouts = 0,
) {
  exact(checks, required[mode]);
  expect(Object.values(checks).every((v) => v === true)).toBe(true);
  const before = await counts(),
    c = await joined(before.held);
  expect(
    c.browser_project_mutations === 0 && c.browser_command_lookups === 0,
  ).toBe(true);
  expect(
    [...archive.values()].filter(
      (f) =>
        !["GET", "HEAD"].includes(f.method) &&
        /^\/api\/v1\/(projects|system)(?:\/|$)/.test(f.path),
    ),
  ).toHaveLength(0);
  const proof = await verifyBodies(page);
  expect(layouts).toBe(mode === "navigation" ? 8 : 0);
  writeFileSync(
    join(directory, "project-audit-result.json"),
    JSON.stringify({ completed: true, mode, checks, ...proof, layouts }),
    { mode: 0o600 },
  );
}

test("[read] actual Owner Project/Secret/Model audit, explicit filters and original response bytes", async ({
  page,
}) => {
  const data = material(),
    p = data.projects.main,
    checks: Record<string, true> = {};
  const entered = await enter(page, data),
    initial = bodyOf<ProjectAuditPage>(entered.fact);
  expect(initial.items.map((r) => r.audit_id)).toEqual(
    data.expected.main_record_ids,
  );
  expect(initial.items.map((r) => r.action)).toEqual(
    data.expected.main_actions,
  );
  expect(
    initial.items.every((r) => r.project_id === p.id && r.scope === "project"),
  ).toBe(true);
  await compare(initial, "main");
  checks.complete_projection = true;
  for (const family of ["project.", "secret.", "model."] as const) {
    const row = initial.items.find((r) =>
      family === "model."
        ? /^(model|provider)\./.test(r.action)
        : r.action.startsWith(family),
    );
    expect(!!row).toBe(true);
    const read = await detail(page, p, row!.audit_id);
    expect(bodyOf<ProjectAuditRecord>(read)).toEqual(row);
    await expect(detailRegion(page)).toContainText("结构化证据");
    await expect(detailRegion(page).locator("pre, a")).toHaveCount(0);
    await back(page, row!.audit_id);
  }
  checks.typed_families = true;
  const beforeDraft = await auditCount(page);
  await filter(page, "limit").fill("2");
  expect(await auditCount(page)).toBe(beforeDraft);
  let current = await listAction(page, p, "应用筛选"),
    first = bodyOf<ProjectAuditPage>(current);
  expect(first.items.length === 2 && first.next_cursor !== null).toBe(true);
  current = await listAction(page, p, "下一页");
  const second = bodyOf<ProjectAuditPage>(current);
  expect(new URLSearchParams(current.query).get("cursor")).toBe(
    first.next_cursor,
  );
  expect(
    second.items.length === 2 &&
      !second.items.some((r) =>
        first.items.some((v) => v.audit_id === r.audit_id),
      ),
  ).toBe(true);
  await compare(second, "main");
  current = await listAction(page, p, "上一页");
  expect(bodyOf<ProjectAuditPage>(current)).toEqual(first);
  expect(new URLSearchParams(current.query).has("cursor")).toBe(false);
  checks.explicit_pagination = true;
  // Corrupt only one real outgoing cursor. The formal server supplies its own
  // CURSOR_INVALID Problem; no response or successful Audit record is forged.
  let changed: { path: string; query: string; reason: string } | undefined;
  await page.route(origin + endpoint(p) + "?*", async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.has("cursor")) return route.continue();
    url.searchParams.set("cursor", "invalid_cursor");
    changed = {
      path: url.pathname,
      query: url.search.slice(1),
      reason: "one real outgoing cursor corrupted for CURSOR_INVALID recovery",
    };
    await route.continue({ url: url.href });
  });
  let start = await sequence(page);
  await button(page, "下一页").click();
  const invalid = await after(
    page,
    start,
    (f) => f.path === endpoint(p) && f.eof && f.code === "CURSOR_INVALID",
  );
  expect(invalid.status === 400 && !!changed).toBe(true);
  wireOverrides.set(keyOf(invalid), changed!);
  await page.unroute(origin + endpoint(p) + "?*");
  await expect(heading(page, "项目审计读取失败")).toBeVisible();
  await expect(
    page.getByRole("region", { name: "项目审计列表", exact: true }),
  ).toHaveCount(0);
  const failedCount = await auditCount(page);
  await expect(button(page, "返回第一页")).toBeEnabled();
  expect(await auditCount(page)).toBe(failedCount);
  current = await listAction(page, p, "返回第一页");
  expect(new URLSearchParams(current.query).has("cursor")).toBe(false);
  checks.cursor_recovery = true;
  await listAction(page, p, "重置筛选");
  expect(
    await filter(page, "action")
      .locator("option")
      .evaluateAll((nodes) => nodes.map((n) => (n as HTMLOptionElement).value)),
  ).toEqual(["", ...auditFilterActions]);
  expect(
    await filter(page, "resource_kind")
      .locator("option")
      .evaluateAll((nodes) => nodes.map((n) => (n as HTMLOptionElement).value)),
  ).toEqual(["", ...auditFilterResourceKinds]);
  const sample = initial.items.find(
    (r) => r.action === "project.update" && r.actor.kind === "human",
  )!;
  expect(!!sample && "id" in sample.resource).toBe(true);
  const values = {
    from: "0000-01-01T00:00:00.000000Z",
    to: "9999-12-31T23:59:59.999999Z",
    actor_kind: "human",
    actor_id: data.owner.user_id,
    action: sample.action,
    outcome: sample.outcome,
    resource_kind: sample.resource.kind,
    resource_id: "id" in sample.resource ? sample.resource.id : "",
    limit: "2",
  };
  const unchanged = await auditCount(page);
  await fillFilters(page, values);
  expect(await auditCount(page)).toBe(unchanged);
  current = await listAction(page, p, "应用筛选");
  expect(Object.fromEntries(new URLSearchParams(current.query))).toEqual(
    values,
  );
  expect(bodyOf<ProjectAuditPage>(current).items.length).toBe(2);
  await compare(bodyOf<ProjectAuditPage>(current), "main");
  for (const key of [
    "tool_id",
    "execution_id",
    "operation_id",
    "approval_id",
    "runner_id",
    "agent_id",
  ]) {
    await filter(page, key).fill(data.missing_id);
    current = await listAction(page, p, "应用筛选");
    expect(new URLSearchParams(current.query).get(key)).toBe(data.missing_id);
    expect(bodyOf<ProjectAuditPage>(current)).toEqual({
      items: [],
      next_cursor: null,
    });
    await filter(page, key).fill("");
  }
  checks.all_filters = true;
  await listAction(page, p, "重置筛选");
  await filter(page, "action").selectOption("account.login");
  current = await listAction(page, p, "应用筛选");
  expect(bodyOf<ProjectAuditPage>(current)).toEqual({
    items: [],
    next_cursor: null,
  });
  await expect(heading(page, "没有匹配的审计记录")).toBeVisible();
  start = await auditCount(page);
  await fillFilters(page, {
    actor_kind: "service",
    actor_id: data.owner.user_id,
  });
  await button(page, "应用筛选").click();
  await expect(filter(page, "actor_id")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await expect(filter(page, "actor_kind")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  await expect(filter(page, "actor_kind")).toBeFocused();
  expect(await auditCount(page)).toBe(start);
  checks.valid_empty = true;
  checks.same_body_schema = true;
  checks.same_body_client = true;
  checks.zero_mutations = true;
  // These final three claims are only published after finish's actual checks.
  await finish(page, "read", checks);
});

// Explicit browser probes cover a formal HTTP gate even when the Owner page
// blocks the child before it can fetch. They are labelled in the evidence and
// are not counted as automatic page requests. They still use real native EOF.
const probeKinds = new Map<string, string>();
async function probe(page: Page, target: string, label: string) {
  const start = await sequence(page);
  await page.evaluate(async (target) => {
    const response = await fetch(target, {
      method: "GET",
      credentials: "same-origin",
      redirect: "error",
      cache: "no-store",
    });
    const reader = response.body!.getReader();
    let bytes = 0;
    try {
      for (;;) {
        const value = await reader.read();
        if (value.value) bytes += value.value.byteLength;
        if (bytes > 1 << 20) throw new Error("OWNED_AUDIT_PROBE_CAP");
        if (value.done) break;
      }
    } finally {
      reader.releaseLock();
    }
  }, target);
  const url = new URL(target, origin),
    fact = await after(
      page,
      start,
      (f) =>
        f.path === url.pathname &&
        f.query === url.search.slice(1) &&
        f.ended &&
        f.eof,
    );
  probeKinds.set(keyOf(fact), label);
  return fact;
}
async function deniedBoth(
  page: Page,
  p: ProjectLocator,
  auditID: string,
  status: number,
  codes: string[],
) {
  for (const target of [endpoint(p) + "?limit=2", endpoint(p, auditID)]) {
    const f = await probe(
      page,
      target,
      "explicit formal gate probe after Owner page blocked child access",
    );
    expect(f.status === status && codes.includes(f.code ?? "")).toBe(true);
    expect(
      !Object.hasOwn(bodyOf<object>(f), "items") &&
        !Object.hasOwn(bodyOf<object>(f), "audit_id"),
    ).toBe(true);
  }
}
async function noChild(page: Page, p: ProjectLocator) {
  await expect(button(page, "重新读取项目")).toBeEnabled();
  await expect(heading(page, "项目审计")).toHaveCount(0);
  await expect(
    page.getByRole("form", { name: "项目审计筛选", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("navigation", { name: "项目导航", exact: true }),
  ).toHaveCount(0);
  expect(
    (await facts(page)).filter(
      (f) => f.path === endpoint(p) || f.path.startsWith(endpoint(p) + "/"),
    ),
  ).toHaveLength(0);
}
async function hold(
  page: Page,
  key: ProjectKey,
  p: ProjectLocator,
  id?: string,
) {
  const before = await counts(),
    start = await sequence(page);
  await ipc({
    action: "arm-hold",
    project: key,
    ...(id ? { audit_id: id } : {}),
  });
  await button(page, id ? "查看详情 " + id : "重新读取").click();
  await expect.poll(async () => (await counts()).held).toBe(before.held + 1);
  await expect(button(page, "取消读取")).toBeEnabled();
  expect(
    (await facts(page)).some(
      (f) => f.sequence > start && f.path === endpoint(p, id) && f.eof,
    ),
  ).toBe(false);
  return { start, held: before.held + 1 };
}
async function explicitDetailRetry(page: Page, p: ProjectLocator, id: string) {
  const start = await sequence(page);
  await button(page, "重新读取详情").click();
  const fact = await after(
    page,
    start,
    (f) => f.path === endpoint(p, id) && f.status === 200 && f.eof && f.ended,
  );
  await expect(detailRegion(page)).toContainText(id);
  await expect(button(page, "重新读取详情")).toBeEnabled();
  return fact;
}

test("[authority] current Owner gates, actual cancelled tails and explicit recovery", async ({
  page,
  browser,
}) => {
  const data = material(),
    checks: Record<string, true> = {};
  let p = data.projects.main;
  const entered = await enter(page, data),
    initial = bodyOf<ProjectAuditPage>(entered.fact),
    row = initial.items[0]!;
  await detail(page, p, row.audit_id);
  await back(page, row.audit_id);
  // A real missing target produces the formal 404. This one target substitution
  // is a declared HTTP control, not a fabricated list item or forged response.
  const missingTarget = endpoint(p, data.missing_id);
  await page.route(origin + endpoint(p, row.audit_id), (route) =>
    route.continue({ url: origin + missingTarget }),
  );
  let start = await sequence(page);
  await button(page, "查看详情 " + row.audit_id).click();
  const missing = await after(
    page,
    start,
    (f) =>
      f.path === endpoint(p, row.audit_id) && f.eof && f.code === "NOT_FOUND",
  );
  expect(missing.status).toBe(404);
  wireOverrides.set(keyOf(missing), {
    path: missingTarget,
    query: "",
    reason: "one real detail GET directed to a missing Audit ID",
  });
  await page.unroute(origin + endpoint(p, row.audit_id));
  await expect(heading(page, "记录不存在或不可访问")).toBeVisible();
  await expect(detailRegion(page).locator(".detail-fields")).toHaveCount(0);
  await explicitDetailRetry(page, p, row.audit_id);
  await back(page, row.audit_id);
  checks.detail_missing = true;

  for (const kind of ["arm-cut", "arm-failure"] as const) {
    await ipc({ action: kind, project: "main" });
    start = await sequence(page);
    await button(page, "重新读取").click();
    await expect(heading(page, "项目审计读取失败")).toBeVisible();
    await expect(
      page.getByRole("region", { name: "项目审计列表", exact: true }),
    ).toHaveCount(0);
    await expect(button(page, "重试读取")).toBeEnabled();
    const count = await auditCount(page),
      failed = (await facts(page)).find(
        (f) => f.sequence > start && f.path === endpoint(p),
      )!;
    expect(!!failed && !failed.eof).toBe(true);
    expect(await auditCount(page)).toBe(count);
    await listAction(page, p, "重试读取");
    await ipc({ action: kind, project: "main", audit_id: row.audit_id });
    await button(page, "查看详情 " + row.audit_id).click();
    await expect(heading(page, "审计详情读取失败")).toBeVisible();
    await expect(detailRegion(page).locator(".detail-fields")).toHaveCount(0);
    await expect(button(page, "重新读取详情")).toBeEnabled();
    await explicitDetailRetry(page, p, row.audit_id);
    await back(page, row.audit_id);
  }
  checks.cut_reread = true;
  checks.failure_reread = true;
  for (const detailID of [undefined, row.audit_id]) {
    const held = await hold(page, "main", p, detailID);
    await button(page, "取消读取").click();
    // release is idempotent completion permission, never a join receipt.
    await ipc({ action: "release" });
    await joined(held.held);
    await expect(
      heading(page, detailID ? "审计详情读取失败" : "项目审计读取失败"),
    ).toBeVisible();
    const count = await auditCount(page);
    await expect(
      button(page, detailID ? "重新读取详情" : "重试读取"),
    ).toBeEnabled();
    expect(await auditCount(page)).toBe(count);
    if (detailID) {
      await explicitDetailRetry(page, p, detailID);
      await back(page, detailID);
    } else await listAction(page, p, "重试读取");
  }
  checks.list_cancel_join = true;
  checks.detail_cancel_join = true;

  // Actual same-document parameter navigation retires the old page and its
  // hold before the newly bound Project is permitted its one default read.
  await filter(page, "tool_id").fill(data.missing_id);
  const crossing = await hold(page, "main", p);
  await navigate(page, path(data.projects.second));
  const second = await ready(page, data.projects.second, crossing.start);
  await ipc({ action: "release" });
  await joined(crossing.held);
  await expect(filter(page, "tool_id")).toHaveValue("");
  await expect(filter(page, "limit")).toHaveValue("50");
  expect(
    bodyOf<ProjectAuditPage>(second).items.every(
      (r) => r.project_id === data.projects.second.id,
    ),
  ).toBe(true);
  await expect(page.locator(".project-audit")).not.toContainText(row.audit_id);
  start = await sequence(page);
  await navigate(page, path(p));
  await ready(page, p, start);
  const crossDomain = await hold(page, "main", p);
  await navigate(page, "/settings/profile");
  await expect(heading(page, "基本资料")).toBeVisible();
  await ipc({ action: "release" });
  await joined(crossDomain.held);
  const all = await facts(page),
    old = all.find(
      (f) => f.sequence > crossDomain.start && f.path === endpoint(p),
    )!,
    profile = all.find(
      (f) => f.sequence > old.sequence && f.path === "/api/v1/me",
    )!;
  expect(!!old.end && !!profile && profile.start > old.end).toBe(true);
  await expect(heading(page, "项目审计")).toHaveCount(0);
  checks.cross_domain = true;

  for (const key of ["archiving", "archived"] as const) {
    await page.goto(path(data.projects[key]));
    const fact = await ready(page, data.projects[key]),
      records = bodyOf<ProjectAuditPage>(fact).items;
    expect(
      records.length > 0 &&
        records.every((r) => r.project_id === data.projects[key].id),
    ).toBe(true);
    await detail(page, data.projects[key], records[0]!.audit_id);
    await expect(
      page.getByRole("form", { name: "项目基本信息", exact: true }),
    ).toHaveCount(0);
  }
  for (const key of ["deleting", "pending"] as const) {
    await page.goto(path(data.projects[key]));
    await noChild(page, data.projects[key]);
    await deniedBoth(page, data.projects[key], data.missing_id, 409, [
      "PROJECT_NOT_ACTIVE",
    ]);
  }
  checks.life_gates = true;
  await page.goto(path(data.projects.other));
  await noChild(page, data.projects.other);
  await deniedBoth(page, data.projects.other, data.missing_id, 404, [
    "NOT_FOUND",
  ]);
  const adminContext = await browser.newContext({ baseURL: origin });
  try {
    const adminPage = await adminContext.newPage(),
      admin = await enter(adminPage, data, "admin", "admin"),
      adminRows = bodyOf<ProjectAuditPage>(admin.fact).items;
    expect(adminRows.length > 0).toBe(true);
    await detail(adminPage, data.projects.admin, adminRows[0]!.audit_id);
    await adminPage.goto(path(p));
    await noChild(adminPage, p);
    await deniedBoth(adminPage, p, row.audit_id, 404, ["NOT_FOUND"]);
    await facts(adminPage);
  } finally {
    await adminContext.close();
  }
  checks.owner_and_admin_owned = true;
  checks.non_owner_hidden = true;

  await page.goto(path(p));
  await ready(page, p);
  await filter(page, "tool_id").fill(data.missing_id);
  const renamed = await ipc({ action: "rename-reuse", project: "main" }),
    replacement = locator(renamed.replacement),
    newName = locator(renamed.renamed);
  expect(
    newName.id === p.id &&
      replacement.id !== p.id &&
      replacement.normalized_name === p.normalized_name,
  ).toBe(true);
  start = await sequence(page);
  await navigate(page, path(data.projects.second));
  await ready(page, data.projects.second, start);
  start = await sequence(page);
  await navigate(page, path(replacement));
  const reused = await ready(page, replacement, start);
  expect(
    bodyOf<ProjectAuditPage>(reused).items.every(
      (r) => r.project_id === replacement.id,
    ),
  ).toBe(true);
  await expect(filter(page, "tool_id")).toHaveValue("");
  await expect(filter(page, "limit")).toHaveValue("50");
  await expect(page.locator(".project-audit")).not.toContainText(row.audit_id);
  start = await sequence(page);
  await navigate(page, path(newName));
  await ready(page, newName, start);
  p = newName;
  checks.cross_project = true;

  // A real committed Logout revokes this exact browser Session. The pending
  // earlier read is retired; only an explicit new read can observe the 401.
  const logoutHold = await hold(page, "main", p, row.audit_id);
  await ipc({ action: "logout", session_id: entered.session.session_id });
  await button(page, "取消读取").click();
  await ipc({ action: "release" });
  await joined(logoutHold.held);
  start = await sequence(page);
  await expect(button(page, "重新读取详情")).toBeEnabled();
  await button(page, "重新读取详情").click();
  const denied = await after(
    page,
    start,
    (f) => f.path === endpoint(p, row.audit_id) && f.status === 401 && f.eof,
  );
  expect(
    ["SESSION_REVOKED", "UNAUTHENTICATED"].includes(denied.code ?? ""),
  ).toBe(true);
  await expect(heading(page, "会话尚未确认")).toBeVisible();
  await expect(page.locator(".project-audit")).toHaveCount(0);
  await expect(
    page.getByRole("navigation", { name: "项目导航", exact: true }),
  ).toHaveCount(0);
  await expect(button(page, "检查当前会话")).toBeEnabled();
  await button(page, "检查当前会话").click();
  await expect(page.locator("#login-email")).toBeVisible();
  await deniedBoth(page, p, row.audit_id, 401, [
    "SESSION_REVOKED",
    "UNAUTHENTICATED",
  ]);
  checks.formal_logout = true;
  checks.late_isolated = true;
  checks.both_gets = true;
  checks.zero_mutations = true;
  writeFileSync(
    join(evidence, "explicit-browser-probes.json"),
    JSON.stringify([...probeKinds]),
    { mode: 0o600 },
  );
  await finish(page, "authority", checks);
});

async function settingsCurrent(
  page: Page,
  p: ProjectLocator,
  leaf: "general" | "audit",
) {
  const project = page.getByRole("navigation", {
      name: "项目导航",
      exact: true,
    }),
    settings = project.getByRole("link", { name: "项目设置", exact: true }),
    menu = page.getByRole("navigation", { name: "项目设置", exact: true });
  await expect(settings).toHaveAttribute("href", path(p, "general"));
  await expect(settings).toHaveAttribute("aria-current", "page");
  await expect(
    menu.getByRole("link", {
      name: leaf === "audit" ? "项目审计" : "基本信息",
      exact: true,
    }),
  ).toHaveAttribute("aria-current", "page");
  await expect(
    menu.getByRole("link", {
      name: leaf === "audit" ? "基本信息" : "项目审计",
      exact: true,
    }),
  ).not.toHaveAttribute("aria-current", "page");
  expect(
    await menu.locator(".settings-group-toggle").allTextContents(),
  ).toEqual(["项目资料", "安全记录", "模型与 Provider"]);
}
async function noOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    viewport: innerWidth,
    width: document.documentElement.clientWidth,
    root: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(
    dimensions.root <= dimensions.width + 1 &&
      dimensions.body <= dimensions.width + 1,
  ).toBe(true);
  return dimensions;
}
async function focusInside(page: Page) {
  expect(
    await page.evaluate(
      () =>
        !!document
          .querySelector('[role="dialog"]')
          ?.contains(document.activeElement),
    ),
  ).toBe(true);
}
async function draftsStillGuarded(page: Page, data: Material) {
  expect(data.system !== null).toBe(true);
  let start = await sequence(page);
  await navigate(page, "/system/model-selection");
  await expect(page).toHaveURL(/\/system\/model-selection$/);
  const summary = page.locator(".meeting-summary-settings");
  await expect(button(summary, "重读会议 Summary 配置")).toBeEnabled();
  await button(summary, "编辑会议 Summary").click();
  await button(summary, "选择会议 Summary Model").click();
  const provider = summary.locator(".choices > li").filter({
    has: page.locator("strong", { hasText: "Owner draft memory Provider" }),
  });
  await button(provider, "查看此 Provider 的 Models").click();
  const replacement = summary.locator(".choices > li").filter({
    has: page.locator("strong", { hasText: data.system!.names.summary_1! }),
  });
  await button(replacement, "选择此会议 Summary Model").click();
  await button(page, "编辑用途").click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toHaveAccessibleName("配置平台模型用途");
  await button(dialog, "不配置 Image Generation").click();
  const auditReads = await auditCount(page);
  await page.evaluate(() => history.back());
  await expect(dialog).toContainText("四项平台用途、会议 Summary");
  await button(dialog, "继续编辑").click();
  await expect(dialog).toHaveAccessibleName("配置平台模型用途");
  await expect(page.locator(".summary-editor")).toContainText(
    "草稿 Model：" + data.system!.names.summary_1!,
  );
  await expect(page.locator(".draft-purposes > li").last()).toContainText(
    "不配置",
  );
  expect(await auditCount(page)).toBe(auditReads);
  await page.evaluate(() => history.back());
  await expect(dialog).toContainText("四项平台用途、会议 Summary");
  start = await sequence(page);
  await button(dialog, "放弃修改").click();
  await ready(page, data.projects.admin, start);
  await expect(dialog).toHaveCount(0);
}
async function productionAssets(page: Page) {
  const assetFiles: string[] = [];
  const walk = (directory: string, relative = "") => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const file = relative ? relative + "/" + entry.name : entry.name;
      if (entry.isDirectory()) walk(join(directory, entry.name), file);
      else if (entry.isFile()) assetFiles.push(file);
      else throw new Error("OWNED_PRODUCTION_ASSET_NOT_REGULAR");
    }
  };
  walk(dist);
  expect(
    assetFiles.includes("index.html") &&
      assetFiles.some((name) => /^assets\/.*\.js$/.test(name)),
  ).toBe(true);
  const hashes = assetFiles.sort().map((name) => {
    const bytes = readFileSync(join(dist, name));
    if (/\.(js|css|html)$/.test(name))
      expect(
        !/DebugView|views\/debug|debug-demo/.test(
          name + bytes.toString("utf8"),
        ),
      ).toBe(true);
    return {
      name,
      bytes: bytes.length,
      sha256: createHash("sha256").update(bytes).digest("hex"),
    };
  });
  await expect(page.getByRole("link", { name: /Debug/i })).toHaveCount(0);
  const publicURLs = await page
    .locator(
      'script[src^="/assets/"],link[href^="/assets/"],link[rel="icon"],img[src^="/assets/brand/"]',
    )
    .evaluateAll((nodes) =>
      nodes.map((node) =>
        node instanceof HTMLScriptElement || node instanceof HTMLImageElement
          ? node.src
          : (node as HTMLLinkElement).href,
      ),
    );
  expect(publicURLs.length > 0).toBe(true);
  const observed = [];
  for (const value of [...new Set(publicURLs)]) {
    const url = new URL(value);
    expect(url.origin === origin && !url.search && !url.hash).toBe(true);
    const name = url.pathname.slice(1),
      expected = hashes.find((entry) => entry.name === name);
    expect(!!expected).toBe(true);
    const response = await page.request.get(url.href),
      raw = await response.body();
    expect(
      response.status() === 200 &&
        createHash("sha256").update(raw).digest("hex") === expected!.sha256,
    ).toBe(true);
    observed.push({ url: url.pathname, sha256: expected!.sha256 });
  }
  expect(
    (await page.request.get("/assets/project-audit-missing.js")).status(),
  ).toBe(404);
  await page.goto("/debug");
  await expect(heading(page, "未找到页面")).toBeVisible();
  await expect(page.getByRole("link", { name: /Debug/i })).toHaveCount(0);
  writeFileSync(
    join(evidence, "production-assets.json"),
    JSON.stringify({
      dist,
      files: hashes,
      public: observed,
      debug_route:
        "actual frontend NotFound; private fallback HTTP status is not production hosting acceptance",
    }),
    { mode: 0o600 },
  );
}

test("[navigation] exact dotted routes, shared draft guards, focus and eight layouts", async ({
  page,
  browser,
}) => {
  const data = material(),
    p = data.projects.main,
    dotted = data.projects.dotted,
    checks: Record<string, true> = {};
  await page.setViewportSize({ width: 1440, height: 900 });
  await observe(page);
  const direct = `/${dotted.username.toUpperCase()}/${dotted.name.toUpperCase()}/settings/audit`,
    response = await page.goto(direct);
  expect(response?.status()).toBe(200);
  await expect(page.locator("#login-email")).toBeVisible();
  expect(new URL(page.url()).searchParams.get("return")).toBe(path(dotted));
  const session = await login(page, data.owner);
  await ready(page, dotted);
  await expect(page).toHaveURL(origin + path(dotted));
  const initial = await facts(page),
    resolved = initial.find((f) => f.path === "/api/v1/projects/resolve")!,
    read = initial.find((f) => f.path === `/api/v1/projects/${dotted.id}`)!,
    audit = initial.find((f) => f.path === endpoint(dotted))!;
  expect(
    !!resolved &&
      !!read &&
      resolved.start < read.start &&
      read.start < audit.start,
  ).toBe(true);
  const directAgain = await page.goto(direct);
  expect(directAgain?.status()).toBe(200);
  await ready(page, dotted);
  checks.dotted_return = true;
  await settingsCurrent(page, dotted, "audit");
  await page.goto(`/${dotted.username}/${dotted.normalized_name}/settings`);
  await expect(
    page.getByRole("form", { name: "项目基本信息", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(origin + path(dotted, "general"));
  await settingsCurrent(page, dotted, "general");
  checks.default_general = true;
  await page
    .getByRole("navigation", { name: "项目设置", exact: true })
    .getByRole("link", { name: "项目审计", exact: true })
    .click();
  await ready(page, dotted);
  await settingsCurrent(page, dotted, "audit");
  checks.settings_current = true;
  for (const raw of [
    path(p) + "?cursor=x",
    path(p) + "#fragment",
    path(p) + "/record",
    `/${p.username}/${p.normalized_name}/settings/%61udit`,
    path(p, "general") + "?cursor=x",
    `/${p.username}/${p.normalized_name}/settings?cursor=x`,
  ]) {
    await page.goto(raw);
    await expect(heading(page, "未找到页面")).toBeVisible();
    expect(
      (await facts(page)).filter((f) => f.path.startsWith("/api/v1/projects")),
    ).toHaveLength(0);
    await expect(
      page.getByRole("navigation", { name: "项目导航", exact: true }),
    ).toHaveCount(0);
  }
  checks.raw_rejection = true;
  await page.goto(path(p));
  let current = await ready(page, p),
    row = bodyOf<ProjectAuditPage>(current).items[0]!;
  await filter(page, "tool_id").fill(data.missing_id);
  await page
    .getByRole("navigation", { name: "项目设置", exact: true })
    .getByRole("link", { name: "基本信息", exact: true })
    .click();
  await expect(
    page.getByRole("form", { name: "项目基本信息", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page
    .getByRole("textbox", { name: "项目描述", exact: true })
    .fill("Owner draft before Audit navigation");
  const auditLink = page
    .getByRole("navigation", { name: "项目设置", exact: true })
    .getByRole("link", { name: "项目审计", exact: true });
  await auditLink.click();
  await expect(page.getByRole("dialog")).toHaveAccessibleName("放弃项目修改？");
  await button(page.getByRole("dialog"), "继续编辑").click();
  await expect(
    page.getByRole("textbox", { name: "项目描述", exact: true }),
  ).toHaveValue("Owner draft before Audit navigation");
  await expect(heading(page, "项目审计")).toHaveCount(0);
  await button(page, "取消修改").click();
  let start = await sequence(page);
  await auditLink.click();
  current = await ready(page, p, start);
  await expect(filter(page, "tool_id")).toHaveValue("");
  checks.local_dirty_leave = true;
  const adminContext = await browser.newContext({
    baseURL: origin,
    viewport: { width: 1440, height: 900 },
  });
  try {
    const adminPage = await adminContext.newPage();
    await enter(adminPage, data, "admin", "admin");
    await draftsStillGuarded(adminPage, data);
    await facts(adminPage);
  } finally {
    await adminContext.close();
  }
  checks.existing_draft_guards = true;
  row = bodyOf<ProjectAuditPage>(current).items[0]!;
  const detailButton = button(page, "查看详情 " + row.audit_id);
  await detailButton.focus();
  start = await sequence(page);
  await page.keyboard.press("Enter");
  await after(
    page,
    start,
    (f) => f.path === endpoint(p, row.audit_id) && f.status === 200 && f.eof,
  );
  await expect(heading(page, "审计详情")).toBeFocused();
  await expect(detailRegion(page)).toContainText(row.audit_id);
  await back(page, row.audit_id);
  checks.inline_focus = true;
  await filter(page, "action").selectOption("project.update");
  current = await listAction(page, p, "应用筛选");
  row = bodyOf<ProjectAuditPage>(current).items[0]!;
  await detail(page, p, row.audit_id);
  await expect(button(page, "重新读取详情")).toBeEnabled();
  const oldAuditCount = await auditCount(page);
  await ipc({ action: "session-fail" });
  // The production pageshow listener is invoked with an idle owner. Busy-tail
  // replacement is a separate controlled factory case, not claimed here.
  await page.evaluate(() => dispatchEvent(new PageTransitionEvent("pageshow")));
  await expect(heading(page, "会话尚未确认")).toBeVisible();
  await expect(page.locator(".project-audit")).toHaveCount(0);
  expect(await auditCount(page)).toBe(oldAuditCount);
  start = await sequence(page);
  await button(page, "检查当前会话").click();
  current = await ready(page, p, start);
  await expect(filter(page, "action")).toHaveValue("");
  await expect(filter(page, "limit")).toHaveValue("50");
  const restored = (await facts(page)).find(
    (f) => f.sequence > start && f.path === "/api/v1/session" && f.session,
  );
  expect(restored?.session?.session_id).toBe(session.session_id);
  expect(
    (await facts(page)).filter((f) => f.sequence > start && isAudit(f)),
  ).toHaveLength(1);
  checks.checking_new_page = true;

  await filter(page, "limit").fill("2");
  current = await listAction(page, p, "应用筛选");
  row =
    bodyOf<ProjectAuditPage>(current).items.find(
      (r) => r.actor.kind === "human",
    ) ?? bodyOf<ProjectAuditPage>(current).items[0]!;
  const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
  if (!images || !isAbsolute(images))
    throw new Error("OWNED_PROJECT_AUDIT_IMAGES_REQUIRED");
  const layouts = [];
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    for (const width of [390, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      if (width === 390) {
        const trigger = button(page, "项目设置栏目");
        await trigger.focus();
        await page.keyboard.press("Enter");
        const dialog = page.getByRole("dialog", {
          name: "项目设置栏目",
          exact: true,
        });
        await expect(dialog).toBeVisible();
        await focusInside(page);
        const close = button(dialog, "关闭");
        await close.focus();
        await page.keyboard.press("Shift+Tab");
        await focusInside(page);
        await page.keyboard.press("Tab");
        await focusInside(page);
        const group = button(dialog, "安全记录"),
          controlled = await group.getAttribute("aria-controls");
        expect(!!controlled).toBe(true);
        await expect(group).toHaveAttribute("aria-expanded", "true");
        await group.click();
        await expect(group).toHaveAttribute("aria-expanded", "false");
        await expect(page.locator(`[id="${controlled}"]`)).toHaveCount(0);
        await group.click();
        await expect(group).toHaveAttribute("aria-expanded", "true");
        expect(
          await dialog.evaluate((node) =>
            [...node.querySelectorAll<HTMLElement>("*")].every((el) => {
              const style = getComputedStyle(el);
              return [
                ...style.animationDuration.split(","),
                ...style.transitionDuration.split(","),
              ].every((duration) => parseFloat(duration) <= 0.00001);
            }),
          ),
        ).toBe(true);
        await page.keyboard.press("Escape");
        await expect(dialog).toHaveCount(0);
        await expect(trigger).toBeFocused();
        await trigger.click();
        await page.locator(".ui-overlay").click({ position: { x: 1, y: 1 } });
        await expect(dialog).toHaveCount(0);
        await expect(trigger).toBeFocused();
        checks.drawer = true;
        checks.reduced_motion = true;
      }
      const inline = width === 390 || width === 1024;
      if (inline) await detail(page, p, row.audit_id);
      else
        await fillFilters(page, {
          from: "0000-01-01T00:00:00.000000Z",
          to: "9999-12-31T23:59:59.999999Z",
          tool_id: data.missing_id,
        });
      const dimensions = await noOverflow(page);
      await page.evaluate(() => scrollTo(0, 0));
      const image = join(images, `project-audit-${theme}-${width}.png`);
      await page.screenshot({ path: image, fullPage: true });
      layouts.push({
        theme,
        width,
        height: 900,
        fullPage: true,
        view: inline
          ? "inline detail"
          : "unapplied safe filters and original records",
        dimensions,
        path: image,
        sha256: createHash("sha256").update(readFileSync(image)).digest("hex"),
      });
      if (inline) await back(page, row.audit_id);
    }
  }
  checks.no_overflow = true;
  writeFileSync(
    join(evidence, "layouts.json"),
    JSON.stringify({
      layouts,
      visual_review:
        "pending independent human/model inspection of these exact eight images; file creation is not visual acceptance",
    }),
    { mode: 0o600 },
  );
  await productionAssets(page);
  checks.no_debug = true;
  checks.zero_mutations = true;
  await finish(page, "navigation", checks, layouts.length);
});
