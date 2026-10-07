import { test, expect, type Page } from "@playwright/test";
import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const endpoint = "/api/v1/system/audit";
type Credential = { email: string; password: string; user_id: string };
type AuditRecord = {
  audit_id: string;
  created_at: string;
  scope: "system";
  actor:
    | { kind: "human"; id: string }
    | { kind: "service"; service: string; cause_ref: string };
  action: string;
  outcome: string;
  resource: { kind: string; id?: string };
  metadata: Record<string, string | string[]>;
  associations: Record<string, string>;
  summary: string;
};
type AuditPage = { items: AuditRecord[]; next_cursor: string | null };
type Fact = {
  sequence: number;
  start: number;
  end?: number;
  method: string;
  path: string;
  query: Record<string, string>;
  status: number;
  ended: boolean;
  eof: boolean;
  bytes: number;
  mutationHeaders: boolean;
  bodyPresent: boolean;
  code?: string;
  page?: AuditPage;
  record?: AuditRecord;
  session?: { user_id: string; session_id: string; role: string };
};
type ObservedWindow = Window & {
  __auditFacts?: Fact[];
  __auditSequence?: number;
  __auditClock?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  media_type: string;
  object_id: string;
  missing_id: string;
} => JSON.parse(readFileSync(join(directory, "audit-material.json"), "utf8"));
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const heading = (page: Page, name: string) =>
  page.getByRole("heading", { name, exact: true });
const filter = (page: Page, name: string) =>
  page.locator(`#audit-filter-${name}`);
const nav = (page: Page) =>
  page.getByRole("navigation", { name: "系统设置", exact: true });
const facts = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__auditFacts ?? []);
const seq = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__auditSequence ?? 0);
const auditFacts = async (page: Page) =>
  (await facts(page)).filter(
    (f) =>
      f.path === endpoint ||
      /^\/api\/v1\/system\/audit\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        f.path,
      ),
  );
const same = (a: unknown, b: unknown) => {
  const canonical = (value: unknown) =>
    JSON.stringify(value, (_key, value) =>
      value && typeof value === "object" && !Array.isArray(value)
        ? Object.fromEntries(
            Object.entries(value).sort(([a], [b]) => a.localeCompare(b)),
          )
        : value,
    );
  return canonical(a) === canonical(b);
};
let ipcSequence = 0;
async function ipc(
  action: string,
  fields: Record<string, unknown> = {},
): Promise<Record<string, any>> {
  const sequence = ++ipcSequence,
    request = join(directory, "audit-ipc.json"),
    ack = join(directory, `audit-ack-${sequence}.json`);
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence, action, ...fields }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  await expect.poll(() => existsSync(ack), { timeout: 14000 }).toBe(true);
  const value = JSON.parse(readFileSync(ack, "utf8"));
  expect(value.ok === true && value.sequence === sequence).toBe(true);
  return value;
}
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "audit-result.json"),
    JSON.stringify({ completed: true, ...value }),
    { mode: 0o600 },
  );
}

// Observe the application's original native fetch and reader.read Promises.
// Return each original object/Promise unchanged, never clone/tee/re-read/cancel
// or await from this side branch. End is an observation, not owner-release proof.
function observeNative() {
  const scope = window as ObservedWindow;
  scope.__auditFacts = [];
  scope.__auditSequence = 0;
  scope.__auditClock = 0;
  const object = (v: unknown): Record<string, unknown> | null =>
    v !== null && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : null;
  const closed = (v: Record<string, unknown>, keys: string[]) =>
    Object.keys(v).length === keys.length &&
    keys.every((key) => Object.hasOwn(v, key));
  const id = (v: unknown): v is string =>
    typeof v === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      v,
    );
  const record = (v: unknown): v is AuditRecord => {
    const value = object(v);
    if (
      !value ||
      !closed(value, [
        "audit_id",
        "created_at",
        "scope",
        "actor",
        "action",
        "outcome",
        "resource",
        "metadata",
        "associations",
        "summary",
      ])
    )
      return false;
    const actor = object(value.actor),
      resource = object(value.resource),
      metadata = object(value.metadata),
      associations = object(value.associations);
    return (
      id(value.audit_id) &&
      typeof value.created_at === "string" &&
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value.created_at) &&
      value.scope === "system" &&
      !!actor &&
      ((actor.kind === "human" &&
        closed(actor, ["kind", "id"]) &&
        id(actor.id)) ||
        (actor.kind === "service" &&
          closed(actor, ["kind", "service", "cause_ref"]) &&
          typeof actor.service === "string" &&
          typeof actor.cause_ref === "string")) &&
      typeof value.action === "string" &&
      typeof value.outcome === "string" &&
      ["success", "denied", "failed", "unknown"].includes(value.outcome) &&
      !!resource &&
      typeof resource.kind === "string" &&
      (closed(resource, ["kind"]) ||
        (closed(resource, ["kind", "id"]) && id(resource.id))) &&
      !!metadata &&
      new TextEncoder().encode(JSON.stringify(metadata)).length <= 4096 &&
      Object.values(metadata).every(
        (x) =>
          typeof x === "string" ||
          (Array.isArray(x) && x.every((y) => typeof y === "string")),
      ) &&
      !!associations &&
      Object.entries(associations).every(
        ([key, value]) =>
          [
            "tool_id",
            "request_id",
            "runner_id",
            "correlation_id",
            "http_trace_id",
          ].includes(key) && id(value),
      ) &&
      typeof value.summary === "string" &&
      value.summary.length < 200
    );
  };
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
      const headers = new Headers(
        init?.headers ?? (input instanceof Request ? input.headers : undefined),
      );
      const fact: Fact = {
        sequence: ++scope.__auditSequence!,
        start: ++scope.__auditClock!,
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        path: url.pathname,
        query: Object.fromEntries(url.searchParams),
        status: 0,
        ended: false,
        eof: false,
        bytes: 0,
        mutationHeaders:
          headers.has("X-CSRF-Token") || headers.has("Idempotency-Key"),
        bodyPresent:
          init?.body != null ||
          (input instanceof Request && input.body !== null),
      };
      scope.__auditFacts!.push(fact);
      const audit =
        url.pathname === "/api/v1/system/audit" ||
        /^\/api\/v1\/system\/audit\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
          url.pathname,
        );
      const session = url.pathname === "/api/v1/session",
        smtp = url.pathname === "/api/v1/system/smtp";
      const finish = () => {
        if (!fact.ended) {
          fact.ended = true;
          fact.end = ++scope.__auditClock!;
        }
      };
      void original
        .then((response) => {
          fact.status = response.status;
          if (!audit && !session && !smtp) return;
          const body = response.body,
            media = response.headers
              .get("Content-Type")
              ?.split(";", 1)[0]
              ?.trim()
              .toLowerCase();
          if (
            !body ||
            media !==
              (response.status === 200
                ? "application/json"
                : "application/problem+json")
          ) {
            finish();
            return;
          }
          let text = "",
            collect = !smtp;
          const decoder = new TextDecoder("utf-8", { fatal: true });
          const bound = audit && response.status === 200 ? 1 << 20 : 600000;
          const project = () => {
            if (!collect) return;
            const value = object(JSON.parse(text + decoder.decode()));
            if (!value) return;
            if (response.status !== 200) {
              if (
                typeof value.code === "string" &&
                [
                  "NOT_FOUND",
                  "FORBIDDEN",
                  "SESSION_REVOKED",
                  "UNAUTHENTICATED",
                  "INVALID_ARGUMENT",
                  "CURSOR_INVALID",
                  "DEPENDENCY_UNAVAILABLE",
                ].includes(value.code)
              )
                fact.code = value.code;
            } else if (session) {
              const user = object(value.user),
                current = object(value.session);
              if (
                closed(value, ["user", "session", "csrf_token"]) &&
                user &&
                current &&
                id(user.id) &&
                id(current.id) &&
                (user.role === "admin" || user.role === "user")
              )
                fact.session = {
                  user_id: user.id,
                  session_id: current.id,
                  role: user.role,
                };
            } else if (audit) {
              if (url.pathname === "/api/v1/system/audit") {
                if (
                  !closed(value, ["items", "next_cursor"]) ||
                  !Array.isArray(value.items) ||
                  value.items.length > 200 ||
                  !value.items.every(record) ||
                  !(
                    value.next_cursor === null ||
                    (typeof value.next_cursor === "string" &&
                      /^[A-Za-z0-9_.-]+$/.test(value.next_cursor) &&
                      value.next_cursor.length <= 8192)
                  )
                )
                  return;
                const records = value.items as AuditRecord[],
                  seen = new Set<string>();
                if (
                  !records.every((row, n) => {
                    if (seen.has(row.audit_id)) return false;
                    seen.add(row.audit_id);
                    const previous = records[n - 1];
                    return (
                      !previous ||
                      previous.created_at > row.created_at ||
                      (previous.created_at === row.created_at &&
                        previous.audit_id > row.audit_id)
                    );
                  })
                )
                  return;
                fact.page = {
                  items: records,
                  next_cursor: value.next_cursor as string | null,
                };
              } else if (
                record(value) &&
                value.audit_id ===
                  url.pathname.slice(url.pathname.lastIndexOf("/") + 1)
              )
                fact.record = value;
            }
          };
          const nativeGet = body.getReader;
          Object.defineProperty(body, "getReader", {
            configurable: true,
            writable: true,
            value: function (
              this: ReadableStream<Uint8Array>,
              ...args: unknown[]
            ) {
              const reader = Reflect.apply(
                nativeGet,
                this,
                args,
              ) as ReadableStreamDefaultReader<Uint8Array>;
              try {
                Object.defineProperty(body, "getReader", { value: nativeGet });
                const nativeRead = reader.read;
                Object.defineProperty(reader, "read", {
                  configurable: true,
                  writable: true,
                  value: function (
                    this: ReadableStreamDefaultReader<Uint8Array>,
                    ...args: unknown[]
                  ) {
                    const original = Reflect.apply(
                      nativeRead,
                      this,
                      args,
                    ) as Promise<ReadableStreamReadResult<Uint8Array>>;
                    void original
                      .then(
                        ({ done, value }) => {
                          try {
                            if (value) {
                              fact.bytes += value.byteLength;
                              if (fact.bytes > bound) {
                                text = "";
                                collect = false;
                              } else if (collect)
                                text += decoder.decode(value, { stream: true });
                            }
                            if (done) {
                              fact.eof = true;
                              try {
                                project();
                              } finally {
                                text = "";
                                collect = false;
                                finish();
                              }
                            }
                          } catch {
                            text = "";
                            collect = false;
                            if (done) finish();
                          }
                        },
                        () => {
                          text = "";
                          collect = false;
                          finish();
                        },
                      )
                      .catch(() => {
                        text = "";
                        collect = false;
                      });
                    return original;
                  },
                });
              } catch {
                text = "";
                collect = false;
              }
              return reader;
            },
          });
        }, finish)
        .catch(() => undefined);
    } catch {
      /* Side observations never replace a native result. */
    }
    return original;
  };
}

async function factAfter(
  page: Page,
  after: number,
  predicate: (fact: Fact) => boolean,
) {
  let found: Fact | undefined;
  try {
    await expect
      .poll(async () => {
        found = (await facts(page)).find(
          (f) => f.sequence > after && predicate(f),
        );
        return !!found;
      })
      .toBe(true);
  } catch (error) {
    console.log(
      "safe Audit response facts",
      JSON.stringify(
        (await facts(page))
          .filter((f) => f.sequence > after)
          .slice(-12)
          .map((f) => ({
            method: f.method,
            audit:
              f.path === endpoint
                ? "list"
                : f.path.startsWith(endpoint + "/")
                  ? "detail"
                  : "other",
            status: f.status,
            ended: f.ended,
            eof: f.eof,
            code: f.code ?? null,
          })),
      ),
    );
    throw error;
  }
  return found!;
}
async function login(page: Page, who: "admin" | "member" = "admin") {
  const credential = material()[who],
    after = await seq(page);
  await page.locator("#login-email").fill(credential.email);
  try {
    await page.locator("#login-password").fill(credential.password);
  } catch {
    throw new Error("owned private login input unavailable");
  }
  await button(page, "登录").click();
  const current = await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.eof &&
      f.ended &&
      f.session?.user_id === credential.user_id &&
      f.session.role === (who === "admin" ? "admin" : "user"),
  );
  await expect(button(page, "退出登录")).toBeEnabled();
  return current.session!;
}
async function ready(page: Page, after = 0) {
  const value = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint && f.status === 200 && f.eof && f.ended && !!f.page,
  );
  await expect(button(page, "刷新第一页")).toBeEnabled();
  expect(!value.mutationHeaders && !value.bodyPresent).toBe(true);
  return value;
}
async function setup(page: Page) {
  await page.addInitScript(observeNative);
  await page.goto("/system/audit");
  await login(page);
  return ready(page);
}
async function refresh(page: Page) {
  const after = await seq(page);
  await button(page, "刷新第一页").click();
  return ready(page, after);
}
async function apply(page: Page) {
  const after = await seq(page);
  await button(page, "应用筛选").click();
  return ready(page, after);
}
async function reset(page: Page) {
  const after = await seq(page);
  await button(page, "重置筛选").click();
  return ready(page, after);
}
async function fillFilters(page: Page, values: Record<string, string>) {
  for (const [key, value] of Object.entries(values)) {
    if (["actor_kind", "action", "outcome", "resource_kind"].includes(key))
      await filter(page, key).selectOption(value);
    else await filter(page, key).fill(value);
  }
}
async function compareRows(page: Page, value: AuditPage) {
  const database = (
    await ipc("snapshot", { ids: value.items.map((row) => row.audit_id) })
  ).records;
  if (!same(value.items, database)) {
    // Only closed DTO paths and types may reach diagnostics, never values,
    // arbitrary incoming keys, Session material or whole metadata objects.
    const metadataKeys = [
      "version",
      "changed_fields",
      "lease_id",
      "consumer",
      "reason",
      "rotation_id",
      "count",
      "rule_count",
      "object_id",
      "initiator_kind",
      "initiator_id",
      "initiator_execution_id",
      "media_type",
      "byte_size",
      "sent_bytes",
      "phase",
      "delivery_id",
      "event_id",
      "handler_id",
      "from_state",
      "redrive_cycle",
      "reason_code",
      "user_id",
      "session_id",
      "attempt_id",
      "invitation_id",
      "reset_id",
      "job_id",
      "channel",
      "provider_id",
      "model_id",
      "replacement_id",
      "affected_count",
      "selection_id",
      "selector_kind",
    ];
    const paths = [
      ...[
        "audit_id",
        "created_at",
        "scope",
        "action",
        "outcome",
        "summary",
      ].map((key) => [key]),
      ...Object.entries({
        actor: ["kind", "id", "service", "cause_ref"],
        resource: ["kind", "id"],
        associations: [
          "tool_id",
          "request_id",
          "runner_id",
          "correlation_id",
          "http_trace_id",
        ],
        metadata: metadataKeys,
      }).flatMap(([parent, keys]) => [
        [parent],
        ...keys.map((key) => [parent, key]),
      ]),
    ];
    const missing = Symbol("missing");
    const at = (row: unknown, path: string[]) => {
      let current = row;
      for (const key of path) {
        if (
          !current ||
          typeof current !== "object" ||
          !Object.hasOwn(current, key)
        )
          return missing;
        current = (current as Record<string, unknown>)[key];
      }
      return current;
    };
    const kind = (value: unknown) =>
      value === missing
        ? "missing"
        : value === null
          ? "null"
          : Array.isArray(value)
            ? "array"
            : typeof value;
    const differences: Record<string, string | number | boolean>[] = [];
    const note = (
      index: number,
      path: string,
      observed: unknown,
      stored: unknown,
    ) => {
      if (differences.length < 20 && !same(observed, stored))
        differences.push({
          row_index: index,
          path,
          observed_type: kind(observed),
          stored_type: kind(stored),
          observed_missing: observed === missing,
          stored_missing: stored === missing,
          value_different: true,
        });
    };
    if (!Array.isArray(database)) note(-1, "items", value.items, database);
    else {
      if (value.items.length !== database.length)
        note(-1, "items", value.items, database);
      for (
        let index = 0;
        index < Math.max(value.items.length, database.length) &&
        differences.length < 20;
        index++
      ) {
        const observed: unknown = value.items[index] ?? missing;
        const stored: unknown = database[index] ?? missing;
        note(index, "record", observed, stored);
        for (const path of paths)
          note(index, path.join("."), at(observed, path), at(stored, path));
      }
    }
    console.log("audit-row-difference", JSON.stringify(differences));
  }
  expect(same(value.items, database)).toBe(true);
  if (!value.items.length) {
    await expect(heading(page, "没有匹配的审计记录")).toBeVisible();
    return;
  }
  const rows = await page.locator(".audit-table tbody tr").evaluateAll((rows) =>
    rows.map((row) => {
      const cells = [...row.querySelectorAll("td")];
      return {
        time: cells[0]?.querySelector("time")?.getAttribute("datetime"),
        actor: [...cells[1]!.querySelectorAll("span")].map(
          (n) => n.textContent,
        ),
        action: cells[2]?.textContent?.trim(),
        outcome: cells[3]?.textContent?.trim(),
        resource: [...cells[4]!.querySelectorAll("span")].map(
          (n) => n.textContent,
        ),
        associations: [...cells[5]!.querySelectorAll("dd")].map(
          (n) => n.textContent,
        ),
        detail: cells[6]?.querySelector("button")?.getAttribute("aria-label"),
      };
    }),
  );
  const outcomes: Record<string, string> = {
    success: "成功",
    denied: "拒绝",
    failed: "失败",
    unknown: "结果未知",
  };
  const valueItems = value.items;
  expect(
    rows.length === value.items.length &&
      rows.every((row, index) => {
        const value = valueItems[index]!;
        return (
          row.time === value.created_at &&
          same(
            row.actor,
            value.actor.kind === "human"
              ? ["human", value.actor.id]
              : ["service", value.actor.service, value.actor.cause_ref],
          ) &&
          row.action === value.action &&
          row.outcome === `${outcomes[value.outcome]}（${value.outcome}）` &&
          same(
            row.resource,
            value.resource.id
              ? [value.resource.kind, value.resource.id]
              : [value.resource.kind],
          ) &&
          same(
            row.associations,
            [
              "tool_id",
              "request_id",
              "runner_id",
              "correlation_id",
              "http_trace_id",
            ].flatMap((key) =>
              value.associations[key] ? [value.associations[key]] : [],
            ),
          ) &&
          row.detail === "查看详情 " + value.audit_id
        );
      }),
  ).toBe(true);
}
async function detail(page: Page, row: AuditRecord, index: number) {
  const after = await seq(page);
  await page
    .locator(".audit-table tbody tr")
    .nth(index)
    .getByRole("button")
    .click();
  const observation = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint + "/" + row.audit_id &&
      f.status === 200 &&
      f.eof &&
      f.ended &&
      !!f.record,
  );
  expect(same(observation.record, row)).toBe(true);
  expect(
    same((await ipc("snapshot", { ids: [row.audit_id] })).records, [row]),
  ).toBe(true);
  await expect(heading(page, "审计详情")).toBeFocused();
  const projected = await page
    .locator(".audit-observation .detail-fields")
    .evaluateAll((lists) =>
      lists.map((list) =>
        Object.fromEntries(
          [...list.querySelectorAll("dt")].map((dt) => [
            dt.textContent,
            dt.nextElementSibling?.textContent?.trim(),
          ]),
        ),
      ),
    );
  const fields = projected[0]!;
  expect(
    fields["审计 ID"] === row.audit_id &&
      fields["时间（UTC）"] === row.created_at &&
      fields["范围"] === "system" &&
      fields["动作"] === row.action &&
      fields["资源类型"] === row.resource.kind &&
      fields["摘要"] === row.summary,
  ).toBe(true);
  const labels: Record<string, string> = {
    version: "版本",
    user_id: "User ID",
    session_id: "Session ID",
    attempt_id: "Attempt ID",
    invitation_id: "Invitation ID",
    phase: "阶段",
    changed_fields: "变更字段",
    rule_count: "规则数量",
    object_id: "Object ID",
    media_type: "媒体类型",
    byte_size: "字节数",
    sent_bytes: "已传输字节数",
    initiator_id: "Initiator ID",
    initiator_kind: "发起者类型",
    reason: "原因",
  };
  expect(
    Object.keys(projected[1]!).length === Object.keys(row.metadata).length &&
      Object.entries(row.metadata).every(
        ([key, value]) =>
          labels[key] &&
          projected[1]![labels[key]!] ===
            (Array.isArray(value) ? value.join(", ") : value),
      ),
  ).toBe(true);
  expect(
    await page
      .locator(".system-audit pre,.system-audit textarea,.system-audit a")
      .count(),
  ).toBe(0);
  return observation;
}
async function zeroMutations(page: Page) {
  expect(
    (await facts(page)).some(
      (f) => f.path.startsWith("/api/v1/system/") && f.method !== "GET",
    ),
  ).toBe(false);
  expect(
    (await auditFacts(page)).every(
      (f) => f.method === "GET" && !f.bodyPresent && !f.mutationHeaders,
    ),
  ).toBe(true);
}

test("[read] formal complete Audit projections, typed families and fourteen explicit filters", async ({
  page,
}) => {
  let current = await setup(page);
  await expect(
    page.locator(".audit-filters input,.audit-filters select"),
  ).toHaveCount(15);
  await expect(filter(page, "action").locator("option")).toHaveCount(54);
  await expect(filter(page, "resource_kind").locator("option")).toHaveCount(26);
  await compareRows(page, current.page!);
  const profileRows = current.page!.items.filter(
    (r) =>
      r.action === "account.profile.update" &&
      r.resource.id === material().admin.user_id,
  );
  const policyRow = current.page!.items.find(
    (r) => r.action === "outbound.policy.update",
  );
  const mimeRow = current.page!.items.find(
    (r) =>
      r.resource.id === material().object_id &&
      r.metadata.media_type === material().media_type,
  );
  expect(profileRows.length === 4 && !!policyRow && !!mimeRow).toBe(true);
  for (const row of [profileRows[0]!, policyRow!, mimeRow!]) {
    await detail(page, row, current.page!.items.indexOf(row));
    await button(page, "返回列表").click();
    expect(
      await page
        .locator(".audit-table tbody tr")
        .nth(current.page!.items.indexOf(row))
        .getByRole("button")
        .evaluate((node) => document.activeElement === node),
    ).toBe(true);
  }
  const ordered = [...profileRows].sort((a, b) =>
    a.created_at < b.created_at
      ? -1
      : a.created_at > b.created_at
        ? 1
        : a.audit_id.localeCompare(b.audit_id),
  );
  const from = ordered[0]!.created_at,
    last = ordered.at(-1)!.created_at;
  // The UTC next second safely contains all four microsecond instants.
  const to = new Date(Date.parse(last) + 1000)
    .toISOString()
    .replace(/\.\d{3}Z$/, ".000000Z");
  const beforeDraft = (await auditFacts(page)).length;
  await fillFilters(page, {
    from,
    to,
    actor_kind: "human",
    actor_id: material().admin.user_id,
    action: "account.profile.update",
    outcome: "success",
    resource_kind: "user",
    resource_id: material().admin.user_id,
    limit: "2",
  });
  expect((await auditFacts(page)).length).toBe(beforeDraft);
  current = await apply(page);
  expect(
    same(current.query, {
      from,
      to,
      actor_kind: "human",
      actor_id: material().admin.user_id,
      action: "account.profile.update",
      outcome: "success",
      resource_kind: "user",
      resource_id: material().admin.user_id,
      limit: "2",
    }),
  ).toBe(true);
  expect(
    current.page!.items.length === 2 && current.page!.next_cursor !== null,
  ).toBe(true);
  await compareRows(page, current.page!);
  const first = current.page!,
    after = await seq(page);
  await button(page, "下一页").click();
  current = await ready(page, after);
  expect(
    current.query.cursor === first.next_cursor &&
      current.page!.items.length === 2 &&
      !current.page!.items.some((r) =>
        first.items.some((v) => r.audit_id === v.audit_id),
      ),
  ).toBe(true);
  await compareRows(page, current.page!);
  const afterPrevious = await seq(page);
  await button(page, "上一页").click();
  current = await ready(page, afterPrevious);
  expect(same(current.page, first)).toBe(true);
  current = await refresh(page);
  expect(
    !Object.hasOwn(current.query, "cursor") && same(current.page, first),
  ).toBe(true);
  for (const key of [
    "tool_id",
    "execution_id",
    "operation_id",
    "approval_id",
    "runner_id",
    "agent_id",
  ]) {
    await filter(page, key).fill(material().missing_id);
    current = await apply(page);
    expect(
      current.query[key] === material().missing_id &&
        current.page!.items.length === 0 &&
        current.page!.next_cursor === null,
    ).toBe(true);
    await compareRows(page, current.page!);
    await filter(page, key).fill("");
  }
  await reset(page);
  await filter(page, "action").selectOption("project.create.accepted");
  current = await apply(page);
  expect(
    current.query.action === "project.create.accepted" &&
      current.page!.items.length === 0,
  ).toBe(true);
  const beforeInvalid = (await auditFacts(page)).length;
  await filter(page, "actor_kind").selectOption("service");
  await filter(page, "actor_id").fill(material().admin.user_id);
  await button(page, "应用筛选").click();
  await expect(filter(page, "actor_id")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect((await auditFacts(page)).length).toBe(beforeInvalid);
  await reset(page);
  await zeroMutations(page);
  result({
    complete_projection: true,
    typed_families: true,
    formal_mime: true,
    explicit_pagination: true,
    all_filters: true,
    valid_empty: true,
    zero_mutations: true,
  });
});

async function waitHeld(previous: number) {
  await expect
    .poll(async () => (await ipc("counts")).held === previous + 1)
    .toBe(true);
}
async function waitJoined() {
  await expect
    .poll(async () => {
      const counts = await ipc("counts");
      return (
        counts.held === counts.joined &&
        counts.server_started === counts.server_finished
      );
    })
    .toBe(true);
}
async function cancelRead(
  page: Page,
  target: string,
  action: () => Promise<unknown>,
  detailMode: boolean,
) {
  const counts = await ipc("counts"),
    after = await seq(page);
  await ipc("arm-hold", { path: target });
  try {
    await action();
    await waitHeld(counts.held);
    await expect(
      button(page, detailMode ? "重新读取详情" : "刷新第一页"),
    ).toBeDisabled();
    await button(page, "取消读取").click();
    await expect(
      heading(page, detailMode ? "审计详情读取失败" : "系统审计读取失败"),
    ).toBeVisible();
    const ended = await factAfter(
      page,
      after,
      (f) => f.path === target && f.ended && !f.eof,
    );
    await waitJoined();
    await expect(
      button(page, detailMode ? "重新读取详情" : "重试读取"),
    ).toBeEnabled();
    expect(
      (await auditFacts(page)).filter((f) => f.sequence > after).length,
    ).toBe(1);
    expect(
      (await auditFacts(page)).filter((f) => f.sequence > ended.sequence)
        .length,
    ).toBe(0);
    const retryAfter = await seq(page);
    await button(page, detailMode ? "重新读取详情" : "重试读取").click();
    const reread = await factAfter(
      page,
      retryAfter,
      (f) =>
        f.path === target &&
        f.status === 200 &&
        f.eof &&
        f.ended &&
        !!(detailMode ? f.record : f.page),
    );
    expect(ended.end !== undefined && reread.start > ended.end).toBe(true);
    return reread;
  } finally {
    await ipc("release");
    await waitJoined();
  }
}

test("[authority] formal permissions, exact Logout, cancelled native reads and cross-domain isolation", async ({
  page,
  browser,
}) => {
  let current = await setup(page);
  const row = current.page!.items.find(
    (r) => r.action === "account.profile.update",
  )!;
  expect(!!row).toBe(true);
  const permission = await ipc("permissions", { ids: [row.audit_id] });
  expect(
    permission.ordinary_both_forbidden === true &&
      permission.detail_missing === true,
  ).toBe(true);
  const ordinaryContext = await browser.newContext({
    baseURL: new URL(page.url()).origin,
  });
  try {
    const ordinary = await ordinaryContext.newPage();
    await ordinary.addInitScript(observeNative);
    await ordinary.goto("/system/audit");
    await login(ordinary, "member");
    await expect(heading(ordinary, "需要系统管理员权限")).toBeVisible();
    expect((await auditFacts(ordinary)).length).toBe(0);
    await button(ordinary, "退出登录").click();
    await expect(ordinary.locator("#login-email")).toBeVisible();
  } finally {
    await ordinaryContext.close();
  }

  // Both controls use real, fully-read service responses; a cut produces a
  // native incomplete body. No synthetic success/Problem is substituted.
  let after = await seq(page);
  await ipc("arm-cut", { path: endpoint });
  await button(page, "刷新第一页").click();
  const cut = await factAfter(
    page,
    after,
    (f) => f.path === endpoint && f.status === 200 && f.ended && !f.eof,
  );
  expect(!cut.page).toBe(true);
  await expect(heading(page, "系统审计读取失败")).toBeVisible();
  expect(await page.locator(".audit-table tbody tr").count()).toBe(0);
  after = await seq(page);
  await button(page, "重试读取").click();
  current = await ready(page, after);
  await compareRows(page, current.page!);
  current = await cancelRead(
    page,
    endpoint,
    () => button(page, "刷新第一页").click(),
    false,
  );
  const index = current.page!.items.findIndex(
    (r) => r.audit_id === row.audit_id,
  );
  expect(index >= 0).toBe(true);
  await cancelRead(
    page,
    endpoint + "/" + row.audit_id,
    () =>
      page
        .locator(".audit-table tbody tr")
        .nth(index)
        .getByRole("button")
        .click(),
    true,
  );
  await expect(heading(page, "审计详情")).toBeFocused();
  await button(page, "返回列表").click();
  expect(
    await page
      .locator(".audit-table tbody tr")
      .nth(index)
      .getByRole("button")
      .evaluate((node) => document.activeElement === node),
  ).toBe(true);

  const counts = await ipc("counts");
  after = await seq(page);
  await ipc("arm-hold", { path: endpoint });
  try {
    await button(page, "刷新第一页").click();
    await waitHeld(counts.held);
    await nav(page).getByRole("link", { name: "SMTP", exact: true }).click();
    const cancelled = await factAfter(
      page,
      after,
      (f) => f.path === endpoint && f.ended && !f.eof,
    );
    const smtp = await factAfter(
      page,
      after,
      (f) =>
        f.path === "/api/v1/system/smtp" &&
        f.status === 200 &&
        f.ended &&
        f.eof,
    );
    expect(cancelled.end !== undefined && smtp.start > cancelled.end).toBe(
      true,
    );
    await waitJoined();
    await expect(heading(page, "SMTP")).toBeVisible();
    const jobs = "/api/v1/system/mail-jobs";
    expect(
      (await facts(page)).some(
        (f) =>
          f.sequence > after &&
          (f.path === jobs ||
            f.path.startsWith(jobs + "/") ||
            f.path === "/api/v1/system/smtp/test"),
      ),
    ).toBe(false);
  } finally {
    await ipc("release");
    await waitJoined();
  }
  after = await seq(page);
  await nav(page).getByRole("link", { name: "系统审计", exact: true }).click();
  current = await ready(page, after);
  const currentSession = (await facts(page))
    .filter((f) => f.session)
    .at(-1)!.session!;
  const before = await ipc("counts");
  after = await seq(page);
  await ipc("arm-hold", { path: endpoint });
  try {
    await button(page, "刷新第一页").click();
    await waitHeld(before.held);
    const revoked = await ipc("logout", {
      session_id: currentSession.session_id,
    });
    expect(revoked.revoked === true && revoked.logout_audit === true).toBe(
      true,
    );
    await nav(page).getByRole("link", { name: "用户", exact: true }).click();
    await expect(page.locator("#login-email")).toBeVisible();
    await expect(page).toHaveURL(/\/login(?:\?|$)/);
    const loginURL = new URL(page.url());
    expect(loginURL.pathname).toBe("/login");
    expect(loginURL.hash).toBe("");
    const loginQuery = [...loginURL.searchParams.entries()];
    const memberTarget = loginQuery.length === 0 ? "/" : "/system/users";
    if (loginQuery.length !== 0)
      expect(loginQuery).toEqual([["return", "/system/users"]]);
    await factAfter(
      page,
      after,
      (f) =>
        f.path === "/api/v1/session" &&
        f.status === 401 &&
        f.ended &&
        f.code === "SESSION_REVOKED",
    );
    await waitJoined();
    await ipc("release");
    expect(
      await page.locator(".system-audit,.audit-table,.detail-fields").count(),
    ).toBe(0);
    expect(
      (await auditFacts(page)).filter((f) => f.sequence > after).length,
    ).toBe(1);
    const memberSession = await login(page, "member");
    expect(
      memberSession.user_id === material().member.user_id &&
        memberSession.user_id !== currentSession.user_id &&
        memberSession.session_id !== currentSession.session_id,
    ).toBe(true);
    await expect(page).toHaveURL(
      (url) =>
        url.pathname === memberTarget && url.search === "" && url.hash === "",
    );
    if (memberTarget === "/") {
      await expect(heading(page, "首页")).toBeVisible();
      await expect(
        page
          .getByRole("navigation", { name: "系统导航", exact: true })
          .getByRole("link", { name: "系统设置", exact: true }),
      ).toHaveCount(0);
    } else {
      await expect(heading(page, "需要系统管理员权限")).toBeVisible();
    }
    expect(
      await page.locator(".system-audit,.audit-table,.detail-fields").count(),
    ).toBe(0);
    expect(
      (await auditFacts(page)).filter((f) => f.sequence > after).length,
    ).toBe(1);
  } finally {
    await ipc("release");
    await waitJoined();
  }
  await zeroMutations(page);
  result({
    ordinary_forbidden: true,
    both_gets: true,
    detail_missing: true,
    cut_reread: true,
    list_cancel_join: true,
    detail_cancel_join: true,
    cross_domain: true,
    formal_logout: true,
    late_isolated: true,
    zero_mutations: true,
  });
});

async function pageshowRecovery(page: Page) {
  const session = (await facts(page)).filter((f) => f.session).at(-1)!.session!;
  await filter(page, "tool_id").fill(material().missing_id);
  await ipc("session-fail");
  const after = await seq(page);
  // Install before dispatch and always disconnect; observe actual checking DOM,
  // not a timer. This is synthetic pageshow, not a BFCache navigation claim.
  const checking = await page.evaluate(async () => {
    let observer: MutationObserver | undefined;
    try {
      return await new Promise<{ cleared: boolean; unlocked: boolean }>(
        (resolve) => {
          observer = new MutationObserver(() => {
            if (!document.querySelector(".session-check")) return;
            resolve({
              cleared: !document.querySelector(
                ".system-audit,.audit-filters,.audit-table,.detail-fields",
              ),
              unlocked:
                document.body.style.overflow !== "hidden" &&
                !document.querySelector(".ui-overlay"),
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
  expect(checking.cleared && checking.unlocked).toBe(true);
  await expect(button(page, "检查当前会话")).toBeEnabled();
  expect(await page.locator(".system-audit").count()).toBe(0);
  expect(
    (await auditFacts(page)).filter((f) => f.sequence > after).length,
  ).toBe(0);
  await button(page, "检查当前会话").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.eof &&
      f.ended &&
      same(f.session, session),
  );
  const current = await ready(page, after);
  expect(same(current.query, { limit: "50" })).toBe(true);
  expect(
    (await auditFacts(page)).filter((f) => f.sequence > after).length,
  ).toBe(1);
  expect(await filter(page, "tool_id").inputValue()).toBe("");
  expect((await ipc("counts")).session_failures).toBe(1);
  return current;
}
async function focusContained(page: Page) {
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  for (let n = 0; n < 12; n++) {
    await page.keyboard.press(n % 2 ? "Shift+Tab" : "Tab");
    expect(
      await dialog.evaluate((node) => node.contains(document.activeElement)),
    ).toBe(true);
  }
}
async function layout(page: Page, detailMode: boolean, width: number) {
  const { safe, diagnostic } = await page.locator(".system-audit").evaluate(
    (region, { detailMode, width }) => {
      const tag = (node: Element) =>
        [
          "TD",
          "DD",
          "P",
          "DIV",
          "LABEL",
          "FIELDSET",
          "LEGEND",
          "INPUT",
          "SELECT",
        ].includes(node.tagName)
          ? node.tagName
          : "OTHER";
      const bound = region.getBoundingClientRect();
      if (
        document.documentElement.scrollWidth > innerWidth + 1 ||
        bound.left < 0 ||
        bound.right > innerWidth + 1 ||
        region.scrollWidth > region.clientWidth + 1
      )
        return {
          safe: false,
          diagnostic: {
            category: "region",
            viewport_width: innerWidth,
            document_scroll_width: document.documentElement.scrollWidth,
            left: bound.left,
            right: bound.right,
            scroll_width: region.scrollWidth,
            client_width: region.clientWidth,
          },
        };
      let nodeIndex = 0;
      for (const node of region.querySelectorAll<HTMLElement>(
        "td,dd,p,.ui-field,legend",
      )) {
        if (node.scrollWidth > node.clientWidth + 1)
          return {
            safe: false,
            diagnostic: {
              category: "content-overflow",
              index: nodeIndex,
              tag: tag(node),
              scroll_width: node.scrollWidth,
              client_width: node.clientWidth,
            },
          };
        nodeIndex++;
      }
      if (detailMode) {
        const safe =
          !!region.querySelector(".detail-fields") &&
          !region.querySelector("table");
        return {
          safe,
          diagnostic: safe
            ? null
            : {
                category: "detail-structure",
                detail_present: !!region.querySelector(".detail-fields"),
                table_present: !!region.querySelector("table"),
              },
        };
      }
      const table = region.querySelector("table"),
        caption = region.querySelector("caption");
      if (
        !table ||
        !caption ||
        table.querySelectorAll("thead th").length !== 7 ||
        table.scrollWidth > table.clientWidth + 1 ||
        caption.getBoundingClientRect().width > 2 ||
        caption.getBoundingClientRect().height > 2
      )
        return {
          safe: false,
          diagnostic: {
            category: "table-structure",
            table_present: !!table,
            caption_present: !!caption,
            header_count: table?.querySelectorAll("thead th").length ?? 0,
            scroll_width: table?.scrollWidth ?? 0,
            client_width: table?.clientWidth ?? 0,
            caption_width: caption?.getBoundingClientRect().width ?? 0,
            caption_height: caption?.getBoundingClientRect().height ?? 0,
          },
        };
      if (
        width <= 760 &&
        [...table.querySelectorAll("td")].some(
          (cell) => getComputedStyle(cell).display !== "block",
        )
      ) {
        const cells = [...table.querySelectorAll("td")];
        const index = cells.findIndex(
          (cell) => getComputedStyle(cell).display !== "block",
        );
        return {
          safe: false,
          diagnostic: {
            category: "mobile-cell-display",
            index,
            tag: "TD",
            scroll_width: cells[index]?.scrollWidth ?? 0,
            client_width: cells[index]?.clientWidth ?? 0,
          },
        };
      }
      let inputIndex = 0;
      for (const input of region.querySelectorAll<HTMLInputElement>(
        "input,select",
      )) {
        if (
          !input.labels?.length ||
          input.getBoundingClientRect().width < 50 ||
          input.getBoundingClientRect().right > bound.right + 1
        )
          return {
            safe: false,
            diagnostic: {
              category: "control",
              index: inputIndex,
              tag: tag(input),
              label_count: input.labels?.length ?? 0,
              width: input.getBoundingClientRect().width,
              right: input.getBoundingClientRect().right,
              region_right: bound.right,
            },
          };
        inputIndex++;
      }
      return { safe: true, diagnostic: null };
    },
    { detailMode, width },
  );
  if (!safe)
    console.log(
      "audit-layout-safe-diagnostic",
      JSON.stringify({ width, detailMode, diagnostic }),
    );
  expect(safe).toBe(true);
  if (detailMode) {
    await page.locator(".detail-fields dd").last().scrollIntoViewIfNeeded();
    await expect(page.locator(".detail-fields dd").last()).toBeInViewport();
    await button(page, "返回列表").scrollIntoViewIfNeeded();
    await expect(button(page, "返回列表")).toBeInViewport();
  } else {
    for (const cell of await page
      .locator(".audit-table tbody tr")
      .first()
      .locator("td")
      .all()) {
      await cell.scrollIntoViewIfNeeded();
      await expect(cell).toBeInViewport();
    }
  }
}

test("[navigation] ninth leaf, thirteenth return, page lifetimes and eight real layouts", async ({
  page,
}) => {
  await page.addInitScript(observeNative);
  await page.goto("/system/audit");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(new URL(page.url()).searchParams.get("return")).toBe("/system/audit");
  await login(page);
  let current = await ready(page);
  await page.goto("/system");
  await expect(page).toHaveURL(/\/system\/users$/);
  await expect(nav(page).getByRole("link")).toHaveCount(9);
  await expect(nav(page).locator(".settings-group-toggle")).toHaveCount(4);
  expect(
    await nav(page)
      .getByRole("link")
      .evaluateAll((links) => links.map((link) => link.getAttribute("href"))),
  ).toEqual([
    "/system/users",
    "/system/invitations",
    "/system/providers",
    "/system/models",
    "/system/model-selection",
    "/system/audit",
    "/system/account-security",
    "/system/smtp",
    "/system/outbound-policy",
  ]);
  const group = nav(page).getByRole("button", { name: "审计", exact: true });
  await group.focus();
  await page.keyboard.press("Space");
  await expect(group).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("Space");
  await expect(group).toHaveAttribute("aria-expanded", "true");
  let after = await seq(page);
  await nav(page).getByRole("link", { name: "系统审计", exact: true }).click();
  current = await ready(page, after);
  await expect(
    nav(page).getByRole("link", { name: "系统审计", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await filter(page, "tool_id").fill(material().missing_id);
  await nav(page).getByRole("link", { name: "用户", exact: true }).click();
  await expect(page).toHaveURL(/\/system\/users$/);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  after = await seq(page);
  await nav(page).getByRole("link", { name: "系统审计", exact: true }).click();
  current = await ready(page, after);
  expect(await filter(page, "tool_id").inputValue()).toBe("");
  current = await pageshowRecovery(page);
  const mime = current.page!.items.find(
    (r) =>
      r.resource.id === material().object_id &&
      r.metadata.media_type === material().media_type,
  );
  expect(!!mime).toBe(true);
  // Keep exactly the formal Object record and its associated long IDs visible.
  await fillFilters(page, {
    resource_kind: "stored_object",
    resource_id: material().object_id,
    limit: "2",
  });
  current = await apply(page);
  const index = current.page!.items.findIndex(
    (row) => row.audit_id === mime!.audit_id,
  );
  expect(index >= 0).toBe(true);
  await compareRows(page, current.page!);
  await detail(page, mime!, index);
  await expect(page).toHaveURL(/\/system\/audit$/);
  await button(page, "返回列表").click();
  expect(
    await page
      .locator(".audit-table tbody tr")
      .nth(index)
      .getByRole("button")
      .evaluate((node) => document.activeElement === node),
  ).toBe(true);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const before = (await auditFacts(page)).length;
      await filter(page, "limit").fill("01");
      await button(page, "应用筛选").click();
      await expect(filter(page, "limit")).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await expect(filter(page, "limit")).toBeFocused();
      expect((await auditFacts(page)).length).toBe(before);
      await layout(page, false, width);
      await filter(page, "limit").fill("2");
      current = await apply(page);
      await detail(page, current.page!.items[index]!, index);
      await layout(page, true, width);
      if (theme === "light") {
        await button(page, "返回列表").click();
        await layout(page, false, width);
      }
      if (width === 390) {
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        await expect(page.getByRole("dialog").getByRole("link")).toHaveCount(9);
        await focusContained(page);
        await page.keyboard.press("Escape");
        await expect(page.locator(".ui-overlay")).toHaveCount(0);
        await expect(toggle).toBeFocused();
        await toggle.click();
        await page
          .locator(".ui-overlay")
          .last()
          .click({ position: { x: width - 3, y: 3 } });
        await expect(page.locator(".ui-overlay")).toHaveCount(0);
        await expect(toggle).toBeFocused();
      }
      await expect(
        page.locator(".ui-overlay,input[type=password],textarea"),
      ).toHaveCount(0);
      await heading(
        page,
        theme === "light" ? "审计记录" : "审计详情",
      ).scrollIntoViewIfNeeded();
      await page.screenshot({
        path: join(
          process.env.AGENTEAM_AUTH_WEB_IMAGES!,
          `audit-${theme}-${width}.png`,
        ),
        fullPage: false,
      });
      layouts++;
      if (theme === "dark") await button(page, "返回列表").click();
    }
  }
  await zeroMutations(page);
  result({
    nine_leaves: true,
    thirteenth_return: true,
    local_dirty_leave: true,
    inline_focus: true,
    checking_new_page: true,
    drawer: true,
    no_overflow: true,
    formal_mime: true,
    zero_mutations: true,
    layouts,
  });
});
