import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const listPath = "/api/v1/system/mail-jobs/management";
const testPath = "/api/v1/system/smtp/test";
const jobPath = (id: string) => `/api/v1/system/mail-jobs/${id}/management`;
const retryPath = (id: string) => `/api/v1/system/mail-jobs/${id}/retry`;
type Job = {
  job_id: string;
  kind: string;
  phase: string;
  attempts: string;
  version: string;
  channel: string | null;
  created_at: string;
  attempt_channel: string | null;
  attempt_result: string | null;
  reason?: string;
};
type Fact = {
  sequence: number;
  path: string;
  query: string;
  method: string;
  status: number;
  bytes: number;
  ended: boolean;
  code?: string;
  commit_state?: string;
  session?: { user_id: string; session_id: string; role: string };
  configured?: boolean;
  job?: Job;
  items?: Job[];
  next_cursor?: string;
  receipt?: { job_id: string; version?: string };
};
type ObservedWindow = Window & {
  __smtpFacts?: Fact[];
  __smtpSequence?: number;
};
type Credential = { email: string; password: string; user_id: string };
const material = (): {
  admin: Credential;
  member: Credential;
  source_id: string;
} =>
  JSON.parse(
    readFileSync(join(directory, "smtp-delivery-material.json"), "utf8"),
  );
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "smtp-delivery-result.json"),
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
    request = join(directory, "smtp-delivery-ipc.json"),
    ack = join(directory, `smtp-delivery-ack-${sequence}.json`);
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
// Side observations of the application's original fetch/read Promises only.
// No clone, tee, second read or request; original cancel and abort are untouched.
// Ended below is a projection marker, never proof of Cookie-owner release.
function observeNative() {
  const scope = window as ObservedWindow;
  scope.__smtpFacts = [];
  scope.__smtpSequence = 0;
  const object = (value: unknown): Record<string, unknown> | null =>
    value !== null && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  const closed = (value: Record<string, unknown>, keys: string[]) =>
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key));
  const id = (value: unknown): value is string =>
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    );
  const decimal = (value: unknown, min: bigint, max: bigint): value is string =>
    typeof value === "string" &&
    /^(?:0|[1-9][0-9]{0,18})$/.test(value) &&
    BigInt(value) >= min &&
    BigInt(value) <= max;
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
        !(
          url.pathname === "/api/v1/session" ||
          url.pathname.startsWith("/api/v1/system/")
        )
      )
        return original;
      const fact: Fact = {
        sequence: ++scope.__smtpSequence!,
        path: url.pathname,
        query: url.search,
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        status: 0,
        bytes: 0,
        ended: false,
      };
      scope.__smtpFacts!.push(fact);
      void original
        .then(
          (response) => {
            fact.status = response.status;
            if (!(
              fact.path === "/api/v1/session" ||
              fact.path === "/api/v1/system/smtp" ||
              fact.path === "/api/v1/system/smtp/test" ||
              /^\/api\/v1\/system\/mail-jobs\/(?:management|[0-9a-f-]{36}\/(?:management|retry))$/.test(
                fact.path,
              )
            ))
              return;
            const body = response.body,
              media = response.headers
                .get("Content-Type")
                ?.split(";", 1)[0]
                ?.trim()
                .toLowerCase();
            if (
              !body ||
              media !==
                (response.status === 200 || response.status === 202
                  ? "application/json"
                  : "application/problem+json")
            ) {
              fact.ended = true;
              return;
            }
            let text = "",
              collect = true;
            const decoder = new TextDecoder();
            function project() {
              if (!collect) return;
              const value = object(JSON.parse(text + decoder.decode()));
              if (!value) return;
              if (response.status !== 200 && response.status !== 202) {
                if (
                  typeof value.code === "string" &&
                  [
                    "UNAUTHENTICATED",
                    "SESSION_REVOKED",
                    "CSRF_FAILED",
                    "FORBIDDEN",
                    "INVALID_ARGUMENT",
                    "VERSION_CONFLICT",
                    "INVALID_STATE",
                    "RATE_LIMITED",
                    "RESOURCE_DELETED",
                    "RESOURCE_BUSY",
                    "NOT_FOUND",
                    "COMMIT_UNKNOWN",
                    "IDEMPOTENCY_KEY_REUSED",
                    "DEPENDENCY_UNAVAILABLE",
                    "INTERNAL_ERROR",
                  ].includes(value.code)
                )
                  fact.code = value.code;
                if (
                  typeof value.commit_state === "string" &&
                  [
                    "not_started",
                    "not_committed",
                    "committed",
                    "unknown",
                  ].includes(value.commit_state)
                )
                  fact.commit_state = value.commit_state;
                return;
              }
              if (fact.path === "/api/v1/session") {
                const user = object(value.user),
                  session = object(value.session);
                if (
                  closed(value, ["user", "session", "csrf_token"]) &&
                  user &&
                  session &&
                  id(user.id) &&
                  id(session.id) &&
                  (user.role === "admin" || user.role === "user")
                )
                  fact.session = {
                    user_id: user.id,
                    session_id: session.id,
                    role: user.role,
                  };
                return;
              }
              if (fact.path === "/api/v1/system/smtp") {
                if (
                  fact.method === "GET" &&
                  closed(value, [
                    "id",
                    "version",
                    "configured",
                    "host",
                    "port",
                    "encryption",
                    "username",
                    "sender_email",
                    "sender_name",
                    "credential_present",
                    "auto_retry_count",
                    "retry_interval_seconds",
                  ]) &&
                  id(value.id) &&
                  decimal(value.version, 1n, 9223372036854775807n) &&
                  typeof value.configured === "boolean"
                )
                  fact.configured = value.configured;
                return;
              }
              if (fact.method === "POST") {
                const retry = fact.path.endsWith("/retry");
                if (
                  response.status === 202 &&
                  closed(value, retry ? ["job_id", "version"] : ["job_id"]) &&
                  id(value.job_id) &&
                  (!retry ||
                    (decimal(value.version, 1n, 9223372036854775807n) &&
                      value.job_id !== fact.path.split("/")[5]))
                )
                  fact.receipt = retry
                    ? { job_id: value.job_id, version: value.version as string }
                    : { job_id: value.job_id };
                return;
              }
              function instant(value: unknown): value is string {
                if (
                  typeof value !== "string" ||
                  !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value)
                )
                  return false;
                const date = new Date(value.slice(0, 23) + "Z");
                return (
                  Number.isFinite(date.getTime()) &&
                  date.toISOString() === value.slice(0, 23) + "Z"
                );
              }
              function job(raw: unknown): Job | undefined {
                const v = object(raw);
                if (!v) return;
                const required = [
                  "job_id",
                  "kind",
                  "phase",
                  "attempts",
                  "version",
                  "channel",
                  "created_at",
                  "attempt_channel",
                  "attempt_result",
                ];
                if (
                  !closed(
                    v,
                    Object.hasOwn(v, "reason")
                      ? [...required, "reason"]
                      : required,
                  ) ||
                  !id(v.job_id) ||
                  !["invitation", "password_reset", "test"].includes(
                    v.kind as string,
                  ) ||
                  ![
                    "enqueue_pending",
                    "pending",
                    "claimed",
                    "sending",
                    "retry_wait",
                    "sent",
                    "failed",
                    "unknown",
                    "cancelled",
                  ].includes(v.phase as string) ||
                  !decimal(v.attempts, 0n, 6n) ||
                  !decimal(v.version, 1n, 9223372036854775807n) ||
                  !instant(v.created_at) ||
                  ![null, "smtp", "backend_log"].includes(
                    v.channel as string | null,
                  ) ||
                  ![null, "smtp", "backend_log"].includes(
                    v.attempt_channel as string | null,
                  ) ||
                  ![null, "sent", "failed", "unknown", "cancelled"].includes(
                    v.attempt_result as string | null,
                  ) ||
                  (Object.hasOwn(v, "reason") &&
                    ![
                      "sent",
                      "token_invalid",
                      "configuration_invalid",
                      "policy_rejected",
                      "network_failed",
                      "smtp_rejected",
                      "timeout",
                      "cancelled",
                      "unknown",
                    ].includes(v.reason as string))
                )
                  return;
                if (v.attempt_channel === null) {
                  if (
                    v.attempt_result !== null ||
                    [
                      "claimed",
                      "sending",
                      "retry_wait",
                      "sent",
                      "failed",
                    ].includes(v.phase as string)
                  )
                    return;
                } else if (
                  v.attempt_channel !== v.channel ||
                  BigInt(v.attempts) < 1n
                )
                  return;
                if (
                  v.phase === "enqueue_pending" &&
                  (v.attempts !== "0" ||
                    v.version !== "1" ||
                    v.attempt_channel !== null ||
                    Object.hasOwn(v, "reason"))
                )
                  return;
                const output: Job = {
                  job_id: v.job_id,
                  kind: v.kind as string,
                  phase: v.phase as string,
                  attempts: v.attempts,
                  version: v.version,
                  channel: v.channel as string | null,
                  created_at: v.created_at,
                  attempt_channel: v.attempt_channel as string | null,
                  attempt_result: v.attempt_result as string | null,
                };
                if (Object.hasOwn(v, "reason"))
                  output.reason = v.reason as string;
                return output;
              }
              if (fact.path === "/api/v1/system/mail-jobs/management") {
                if (
                  !closed(
                    value,
                    Object.hasOwn(value, "next_cursor")
                      ? ["items", "next_cursor"]
                      : ["items"],
                  ) ||
                  !Array.isArray(value.items) ||
                  value.items.length > 25
                )
                  return;
                if (
                  Object.hasOwn(value, "next_cursor") &&
                  (typeof value.next_cursor !== "string" ||
                    !value.next_cursor ||
                    new TextEncoder().encode(value.next_cursor).byteLength >
                      8192 ||
                    value.items.length !== 25)
                )
                  return;
                const rows = value.items.map(job);
                if (rows.some((v) => !v)) return;
                const ids = new Set<string>();
                for (let i = 0; i < rows.length; i++) {
                  const v = rows[i]!;
                  if (ids.has(v.job_id)) return;
                  ids.add(v.job_id);
                  if (
                    i &&
                    !(
                      rows[i - 1]!.created_at > v.created_at ||
                      (rows[i - 1]!.created_at === v.created_at &&
                        rows[i - 1]!.job_id > v.job_id)
                    )
                  )
                    return;
                }
                fact.items = rows as Job[];
                if (Object.hasOwn(value, "next_cursor"))
                  fact.next_cursor = value.next_cursor as string;
              } else {
                const v = job(value);
                if (v && v.job_id === fact.path.split("/")[5]) fact.job = v;
              }
            }
            try {
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
                    Object.defineProperty(body, "getReader", {
                      value: nativeGet,
                    });
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
                                  if (fact.bytes > 600000) {
                                    text = "";
                                    collect = false;
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
                  } catch {
                    text = "";
                    collect = false;
                  }
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
        .catch(() => undefined);
    } catch {
      /* Observation failures cannot change the native result. */
    }
    return original;
  };
}

const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const title = (page: Page, name: string) =>
  page.getByRole("heading", { name, exact: true });
const panel = (page: Page) =>
  page.getByRole("region", { name: "SMTP 测试与任务", exact: true });
const top = (page: Page) => page.getByRole("dialog");
const recipient = (page: Page) =>
  page.getByRole("textbox", { name: "测试收件邮箱", exact: true });
const facts = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__smtpFacts ?? []);
const seq = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__smtpSequence ?? 0);
const writes = async (page: Page) =>
  (await facts(page)).filter(
    (f) =>
      f.method === "POST" && (f.path === testPath || f.path.endsWith("/retry")),
  );
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
async function login(page: Page, who: "admin" | "member" = "admin") {
  const v = material()[who],
    after = await seq(page);
  try {
    await page.locator("#login-email").fill(v.email);
    await page.locator("#login-password").fill(v.password);
  } catch {
    throw Error("private delivery login input failed");
  }
  await button(page, "登录").click();
  const s = await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === v.user_id &&
      f.session.role === (who === "member" ? "user" : "admin"),
  );
  await expect(button(page, "退出登录")).toBeEnabled();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
  return s.session!;
}
async function setup(page: Page) {
  await page.addInitScript(observeNative);
  await page.goto("/system/smtp");
  const identity = await login(page);
  await factAfter(
    page,
    0,
    (f) =>
      f.path === "/api/v1/system/smtp" &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      typeof f.configured === "boolean",
  );
  await expect(button(page, "读取当前配置")).toBeEnabled();
  expect(
    (await facts(page)).filter((f) =>
      f.path.startsWith("/api/v1/system/mail-jobs"),
    ).length,
  ).toBe(0);
  return identity;
}
async function switchDelivery(page: Page) {
  const before = await seq(page);
  await button(page, "测试与任务").click();
  await expect(panel(page)).toBeVisible();
  return factAfter(
    page,
    before,
    (f) =>
      f.path === listPath &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.items,
  );
}
async function refresh(page: Page) {
  const after = await seq(page);
  await button(page, "刷新任务列表").click();
  const f = await factAfter(
    page,
    after,
    (f) => f.path === listPath && f.status === 200 && f.ended && !!f.items,
  );
  await expect(button(page, "刷新任务列表")).toBeEnabled();
  return f;
}
async function detail(page: Page, id: string) {
  if (await button(page, "返回任务列表").count())
    await button(page, "返回任务列表").click();
  await refresh(page);
  const after = await seq(page);
  await button(page, "查看任务 " + id).click();
  const f = await factAfter(
    page,
    after,
    (f) => f.path === jobPath(id) && f.status === 200 && f.ended && !!f.job,
  );
  await expect(button(page, "重新读取任务详情")).toBeEnabled();
  return f.job!;
}
async function reread(page: Page, id: string) {
  const after = await seq(page);
  await button(page, "重新读取任务详情").click();
  const f = await factAfter(
    page,
    after,
    (f) => f.path === jobPath(id) && f.status === 200 && f.ended && !!f.job,
  );
  await expect(button(page, "重新读取任务详情")).toBeEnabled();
  return f.job!;
}
async function accepted(page: Page, after: number, path: string) {
  const f = await factAfter(
    page,
    after,
    (f) =>
      f.path === path &&
      f.method === "POST" &&
      f.status === 202 &&
      f.ended &&
      !!f.receipt,
  );
  await factAfter(
    page,
    f.sequence,
    (g) =>
      g.path === jobPath(f.receipt!.job_id) &&
      g.method === "GET" &&
      g.status !== 0,
  );
  await expect(button(page, "重新读取任务详情")).toBeEnabled();
  return f.receipt!;
}
async function requestTest(page: Page) {
  await recipient(page).fill("Owned.Test@example.test");
  const after = await seq(page);
  await button(page, "请求测试发送").click();
  return accepted(page, after, testPath);
}
async function applyRetry(page: Page, id: string) {
  await button(page, "申请新重试周期").click();
  await expect(top(page)).toHaveAccessibleName("申请新重试周期？");
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认申请新周期", exact: true })
    .click();
  return accepted(page, after, retryPath(id));
}
const kindLabel: Record<string, string> = {
  invitation: "邀请邮件",
  password_reset: "密码重置邮件",
  test: "测试邮件",
};
const phaseLabel: Record<string, string> = {
  enqueue_pending: "等待任务入队",
  pending: "等待投递",
  claimed: "已领取",
  sending: "正在投递",
  retry_wait: "等待自动重试",
  sent: "已报告接受",
  failed: "投递失败",
  unknown: "任务结果未知",
  cancelled: "已取消",
};
const actualLabel = (v: string | null) =>
  v === null ? "未记录" : v === "smtp" ? "SMTP" : "后台日志";
const resultLabel = (v: string | null) =>
  v === null
    ? "尚无已报告终局结果"
    : (
        {
          sent: "已报告接受",
          failed: "失败",
          unknown: "结果未知",
          cancelled: "已取消",
        } as Record<string, string>
      )[v];
function sameJob(a: Job, b: Job) {
  return (
    [
      "job_id",
      "kind",
      "phase",
      "attempts",
      "version",
      "created_at",
      "channel",
      "attempt_channel",
      "attempt_result",
      "reason",
    ].every((k) => a[k as keyof Job] === b[k as keyof Job]) &&
    Object.keys(a).length === Object.keys(b).length
  );
}
async function listMatches(page: Page, f: Fact, expected: Job[]) {
  expect(
    !!f.items &&
      f.items.length === expected.length &&
      f.items.every((j, i) => sameJob(j, expected[i]!)),
  ).toBe(true);
  const rows = page.locator(".smtp-delivery tbody tr");
  await expect(rows).toHaveCount(expected.length);
  const text = await rows.evaluateAll((nodes) =>
    nodes.map((n) =>
      Array.from(n.querySelectorAll("td")).map(
        (c) => c.textContent?.trim() ?? "",
      ),
    ),
  );
  expect(
    text.every((cells, i) => {
      const v = expected[i]!;
      return (
        cells.length === 6 &&
        cells[0] === kindLabel[v.kind] + v.job_id &&
        cells[1] === phaseLabel[v.phase] &&
        cells[2] ===
          actualLabel(v.attempt_channel) + resultLabel(v.attempt_result) &&
        cells[3] === `尝试 ${v.attempts} 次版本 ${v.version}` &&
        cells[4] === v.created_at &&
        cells[5].includes("查看详情")
      );
    }),
  ).toBe(true);
}
async function detailMatches(page: Page, j: Job) {
  const snapshot = await ipc("snapshot");
  if (j.phase === "cancelled" && j.attempts === "0")
    expect(snapshot.cancelled_unattempted).toBe(true);
  const db = (snapshot.jobs as Job[]).find((v) => v.job_id === j.job_id);
  expect(!!db && sameJob(j, db)).toBe(true);
  const values = await page
    .locator(".job-values > div")
    .evaluateAll((nodes) =>
      Object.fromEntries(
        nodes.map((n) => [
          n.querySelector("dt")?.textContent?.trim(),
          n.querySelector("dd")?.textContent?.trim(),
        ]),
      ),
    );
  expect(
    values["任务类型"] === kindLabel[j.kind] &&
      values["当前阶段"] === phaseLabel[j.phase] &&
      values["尝试次数"] === j.attempts &&
      values["当前版本"] === j.version &&
      values["实际当前尝试渠道"] === actualLabel(j.attempt_channel) &&
      values["实际当前尝试结果"] === resultLabel(j.attempt_result) &&
      values["创建时间"] === j.created_at &&
      values["兼容渠道摘要"] ===
        actualLabel(j.channel) + "；不证明已尝试或下次使用渠道。",
  ).toBe(true);
  expect(
    await panel(page)
      .locator('a[href*="invite"],a[href*="reset-password"]')
      .count(),
  ).toBe(0);
}
async function settle(page: Page, id: string, phase: string) {
  await ipc("wait", { id, phase });
  const j = await reread(page, id);
  expect(
    j.phase === phase &&
      j.attempt_channel === "smtp" &&
      j.attempt_result === phase,
  ).toBe(true);
  await detailMatches(page, j);
  return j;
}
async function expectProblem(
  page: Page,
  after: number,
  path: string,
  status: number,
  code: string,
) {
  return factAfter(
    page,
    after,
    (f) =>
      f.path === path &&
      f.method === "POST" &&
      f.status === status &&
      f.ended &&
      f.code === code &&
      (f.commit_state === "not_started" || f.commit_state === "not_committed"),
  );
}
async function uniqueCut() {
  const f = (await ipc("cut-facts")).facts;
  expect(
    f.commands === 1 &&
      f.intents === 1 &&
      f.events === 1 &&
      f.audits === 1 &&
      f.dropped === 1 &&
      f.replays === 1 &&
      f.dispatched === 2 &&
      f.same_body &&
      f.same_csrf &&
      f.same_method_path,
  ).toBe(true);
  return f as { job_id: string };
}

test("[read] three kinds, actual channels and continuous strict pages", async ({
  page,
}) => {
  await setup(page);
  const empty = await switchDelivery(page);
  expect(empty.items?.length === 0 && empty.next_cursor === undefined).toBe(
    true,
  );
  await expect(title(page, "当前页没有投递任务")).toBeVisible();
  await expect(button(page, "请求测试发送")).toBeDisabled();
  const expected = (await ipc("seed-read")).jobs as Job[];
  expect(expected.length).toBe(26);
  const first = await refresh(page);
  await listMatches(page, first, expected.slice(0, 25));
  expect(typeof first.next_cursor === "string" && !!first.next_cursor).toBe(
    true,
  );
  let after = await seq(page);
  await button(page, "下一页").click();
  const second = await factAfter(
    page,
    after,
    (f) => f.path === listPath && f.status === 200 && f.ended && !!f.items,
  );
  await listMatches(page, second, expected.slice(25));
  expect(second.next_cursor === undefined).toBe(true);
  await expect(button(page, "下一页")).toBeDisabled();
  const all = [...first.items!, ...second.items!];
  expect(
    new Set(all.map((j) => j.job_id)).size === 26 &&
      new Set(all.map((j) => j.kind)).size === 3 &&
      all.some((j) => j.attempt_channel === "smtp") &&
      all.some((j) => j.attempt_channel === "backend_log"),
  ).toBe(true);
  after = await seq(page);
  await button(page, "上一页").click();
  const again = await factAfter(
    page,
    after,
    (f) => f.path === listPath && f.status === 200 && f.ended && !!f.items,
  );
  await listMatches(page, again, expected.slice(0, 25));
  const cancelled = all.find((j) => j.phase === "cancelled")!;
  expect(
    !!cancelled &&
      cancelled.attempt_channel === null &&
      cancelled.attempt_result === null &&
      cancelled.attempts === "0",
  ).toBe(true);
  const j = await detail(page, cancelled.job_id);
  await detailMatches(page, j);
  await expect(button(page, "申请新重试周期")).toBeEnabled();
  await button(page, "返回任务列表").click();
  await listMatches(page, again, expected.slice(0, 25));
  result({
    empty: true,
    three_kinds: true,
    two_channels: true,
    strict_dto: true,
    continuous_pages: true,
    exact_detail: true,
    cancelled_unattempted: true,
    compatibility_separate: true,
  });
});

test("[test] real SMTP outcomes and explicitly reviewed retry decisions", async ({
  page,
}) => {
  await setup(page);
  await switchDelivery(page);
  const sent = await requestTest(page);
  await settle(page, sent.job_id, "sent");
  await ipc("configure", { endpoint: "failed" });
  const failed = await requestTest(page);
  await settle(page, failed.job_id, "failed");
  await ipc("configure", { endpoint: "unknown" });
  const unknown = await requestTest(page);
  await settle(page, unknown.job_id, "unknown");
  await expect(title(page, "请求结果未确认")).toHaveCount(0);
  await expect(title(page, "测试请求已接受")).toBeVisible();
  await ipc("configure", { endpoint: "success" });
  const next = await applyRetry(page, unknown.job_id);
  expect(next.job_id !== unknown.job_id && BigInt(next.version!) >= 1n).toBe(
    true,
  );
  await settle(page, next.job_id, "sent");
  const source = material().source_id;
  const original = await detail(page, source);
  await button(page, "申请新重试周期").click();
  const before = (await writes(page)).length;
  const competing = (await ipc("compete", { id: source })).receipt as {
    job_id: string;
    version: string;
  };
  await ipc("wait", { id: competing.job_id, phase: "sent" });
  let after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认申请新周期", exact: true })
    .click();
  await expectProblem(page, after, retryPath(source), 409, "VERSION_CONFLICT");
  await expect(
    page.getByText(
      "任务版本冲突。请明确重新读取、核对当前任务后重新确认申请，不自动更新原版本。",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(button(page, "申请新重试周期")).toBeDisabled();
  expect((await writes(page)).length === before + 1).toBe(true);
  const observed = await reread(page, source);
  expect(BigInt(observed.version) > BigInt(original.version)).toBe(true);
  await button(page, "申请新重试周期").click();
  expect((await writes(page)).length === before + 1).toBe(true);
  await top(page)
    .getByRole("button", { name: "取消申请", exact: true })
    .click();
  await ipc("configure", { endpoint: "held" });
  const held = (await ipc("compete", { id: source })).receipt as {
    job_id: string;
    version: string;
  };
  await ipc("wait-held");
  await reread(page, source);
  await button(page, "申请新重试周期").click();
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认申请新周期", exact: true })
    .click();
  await expectProblem(page, after, retryPath(source), 409, "RESOURCE_BUSY");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await button(page, "放弃追踪本次操作").click();
  await top(page)
    .getByRole("button", { name: "放弃草稿和追踪", exact: true })
    .click();
  await ipc("release");
  await ipc("wait", { id: held.job_id, phase: "sent" });
  await reread(page, source);
  await ipc("revoke-source");
  await reread(page, source);
  await button(page, "申请新重试周期").click();
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认申请新周期", exact: true })
    .click();
  await expectProblem(page, after, retryPath(source), 410, "RESOURCE_DELETED");
  await expect(
    page.getByText(
      "原业务材料已失效。请重新读取任务并核对；没有自动申请新周期。",
      { exact: true },
    ),
  ).toBeVisible();
  result({
    accepted: true,
    smtp_success: true,
    smtp_failed: true,
    smtp_unknown: true,
    new_cycle: true,
    version_conflict: true,
    root_busy: true,
    material_invalid: true,
    explicit_review: true,
  });
});

test("[outcome] original accepted requests survive lost receipts and changed current facts", async ({
  page,
}) => {
  await setup(page);
  await switchDelivery(page);
  await ipc("arm-drop");
  await recipient(page).fill("Original.Test@example.test");
  let after = await seq(page);
  await button(page, "请求测试发送").click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  const lost = (await facts(page)).find(
    (f) => f.sequence > after && f.path === testPath && f.status === 202,
  );
  expect(!!lost && !lost.receipt).toBe(true);
  await ipc("wait-cut", { phase: "sent" });
  await ipc("unconfigure");
  after = await seq(page);
  await button(page, "检查当前状态").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      !!f.session,
  );
  await factAfter(
    page,
    after,
    (f) => f.path === listPath && f.status === 200 && f.ended && !!f.items,
  );
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await expect(title(page, "测试请求已接受")).toHaveCount(0);
  await expect(button(page, "重试原请求")).toBeEnabled();
  after = await seq(page);
  await button(page, "重试原请求").click();
  const replayTest = await accepted(page, after, testPath);
  expect(replayTest.job_id === (await uniqueCut()).job_id).toBe(true);
  const source = material().source_id;
  await detail(page, source);
  await ipc("arm-drop", { id: source });
  await button(page, "申请新重试周期").click();
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认申请新周期", exact: true })
    .click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await ipc("wait-cut", { phase: "sent" });
  await ipc("revoke-source");
  after = await seq(page);
  await button(page, "检查当前状态").click();
  await factAfter(
    page,
    after,
    (f) => f.path === jobPath(source) && f.status === 200 && f.ended && !!f.job,
  );
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await expect(button(page, "重试原请求")).toBeEnabled();
  after = await seq(page);
  await button(page, "重试原请求").click();
  const replayRetry = await accepted(page, after, retryPath(source));
  expect(
    replayRetry.job_id === (await uniqueCut()).job_id &&
      BigInt(replayRetry.version!) > 1n,
  ).toBe(true);
  await ipc("configure", { endpoint: "success" });
  await button(page, "配置").click();
  await expect(button(page, "读取当前配置")).toBeEnabled();
  after = await seq(page);
  await button(page, "读取当前配置").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/system/smtp" &&
      f.status === 200 &&
      f.ended &&
      f.configured === true,
  );
  await button(page, "测试与任务").click();
  await ipc("arm-get-fail");
  await recipient(page).fill("after.confirmation@example.test");
  const prior = (await writes(page)).length;
  after = await seq(page);
  await button(page, "请求测试发送").click();
  const confirmed = await accepted(page, after, testPath);
  await expect(title(page, "请求已确认，任务详情读取失败")).toBeVisible();
  await expect(title(page, "测试请求已接受")).toBeVisible();
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  await expect(title(page, "请求结果未确认")).toHaveCount(0);
  await ipc("wait", { id: confirmed.job_id, phase: "sent" });
  await reread(page, confirmed.job_id);
  expect((await writes(page)).length === prior + 1).toBe(true);
  result({
    test_cut: true,
    retry_cut: true,
    exact_original: true,
    unique_transactions: true,
    current_disabled: true,
    source_changed: true,
    new_version: true,
    read_not_receipt: true,
    read_failure: true,
  });
});
async function session(page: Page) {
  const f = (await facts(page))
    .filter(
      (f) =>
        f.path === "/api/v1/session" &&
        f.status === 200 &&
        f.ended &&
        !!f.session,
    )
    .at(-1);
  expect(!!f?.session).toBe(true);
  return f!.session!;
}
async function restoreAdministrator(
  page: Page,
  identity: NonNullable<Fact["session"]>,
) {
  await ipc("promote", identity);
  const after = await seq(page);
  await page.evaluate(() => window.dispatchEvent(new Event("pageshow")));
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.role === "admin" &&
      f.session.session_id === identity.session_id,
  );
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/system/smtp" &&
      f.status === 200 &&
      f.ended &&
      typeof f.configured === "boolean",
  );
  await switchDelivery(page);
}
async function forbidden(
  page: Page,
  after: number,
  path: string,
  method: string,
) {
  await factAfter(
    page,
    after,
    (f) =>
      f.path === path &&
      f.method === method &&
      f.status === 403 &&
      f.ended &&
      f.code === "FORBIDDEN",
  );
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  await expect(panel(page)).toHaveCount(0);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
}
test("[authority] exact current authority and revoked identities destroy recovery eligibility", async ({
  page,
}) => {
  let identity = await setup(page);
  await switchDelivery(page);
  await ipc("demote", identity);
  let after = await seq(page);
  await button(page, "刷新任务列表").click();
  await forbidden(page, after, listPath, "GET");
  await restoreAdministrator(page, identity);
  await recipient(page).fill("refused.new@example.test");
  const dbBefore = ((await ipc("snapshot")).jobs as Job[]).length;
  identity = await session(page);
  await ipc("demote", identity);
  after = await seq(page);
  await button(page, "请求测试发送").click();
  await forbidden(page, after, testPath, "POST");
  expect(((await ipc("snapshot")).jobs as Job[]).length === dbBefore).toBe(
    true,
  );
  await restoreAdministrator(page, identity);
  await expect(recipient(page)).toHaveValue("");
  await ipc("arm-drop");
  await recipient(page).fill("refused.replay@example.test");
  await button(page, "请求测试发送").click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await ipc("wait-cut", { phase: "sent" });
  await button(page, "检查当前状态").click();
  await expect(button(page, "重试原请求")).toBeEnabled();
  identity = await session(page);
  await ipc("demote", identity);
  after = await seq(page);
  await button(page, "重试原请求").click();
  await forbidden(page, after, testPath, "POST");
  await restoreAdministrator(page, identity);
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(recipient(page)).toHaveValue("");
  await ipc("arm-drop");
  await recipient(page).fill("revoked.original@example.test");
  await button(page, "请求测试发送").click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await ipc("wait-cut", { phase: "sent" });
  identity = await session(page);
  await ipc("revoke-session", identity);
  after = await seq(page);
  await button(page, "检查当前状态").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 401 &&
      f.ended &&
      f.code === "SESSION_REVOKED",
  );
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  const renewed = await login(page);
  expect(
    renewed.user_id === identity.user_id &&
      renewed.session_id !== identity.session_id,
  ).toBe(true);
  await page.goto("/system/smtp");
  await factAfter(
    page,
    0,
    (f) =>
      f.path === "/api/v1/system/smtp" &&
      f.status === 200 &&
      f.ended &&
      typeof f.configured === "boolean",
  );
  await switchDelivery(page);
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(recipient(page)).toHaveValue("");
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/smtp");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  result({
    get_forbidden: true,
    write_forbidden: true,
    replay_forbidden: true,
    revoked: true,
    ordinary_denied: true,
    new_session: true,
    private_cleanup: true,
  });
});
async function focusContained(page: Page) {
  await expect(top(page)).toHaveCount(1);
  await expect
    .poll(() => top(page).evaluate((d) => d.contains(document.activeElement)))
    .toBe(true);
  for (let n = 0; n < 6; n++) {
    await page.keyboard.press("Tab");
    expect(
      await top(page).evaluate((d) => d.contains(document.activeElement)),
    ).toBe(true);
  }
  await page.keyboard.press("Shift+Tab");
  expect(
    await top(page).evaluate((d) => d.contains(document.activeElement)),
  ).toBe(true);
}
async function fallback(page: Page) {
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(title(page, "SMTP")).toBeFocused();
  await page.keyboard.press("Tab");
  expect(
    await page.evaluate(
      () =>
        document.activeElement instanceof HTMLElement &&
        document.activeElement !== document.body &&
        document.activeElement.isConnected &&
        !document.activeElement.closest('[inert],[aria-hidden="true"]'),
    ),
  ).toBe(true);
}
async function restored(page: Page, count: number) {
  const original = await session(page),
    after = await seq(page),
    before = (await writes(page)).length;
  const oldHeading = await page
    .locator(".system-smtp-settings > h1")
    .elementHandle();
  await ipc("arm-session-fail");
  // Synthetic pageshow exercises the production listener, not browser BFCache.
  const checking = await page.evaluate(async () => {
    let observer: MutationObserver | undefined;
    try {
      return await new Promise<{
        zero: boolean;
        unlocked: boolean;
        operative: boolean;
      }>((resolve) => {
        observer = new MutationObserver(() => {
          const check = document.querySelector<HTMLElement>(".session-check");
          if (!check) return;
          resolve({
            zero: document.querySelectorAll(".ui-overlay").length === 0,
            unlocked:
              document.body.style.overflow !== "hidden" &&
              document.documentElement.style.overflow !== "hidden",
            operative: check.closest("[inert]") === null,
          });
        });
        observer.observe(document.body, { childList: true, subtree: true });
        window.dispatchEvent(new Event("pageshow"));
      });
    } finally {
      observer?.disconnect();
    }
  });
  expect(checking).toEqual({ zero: true, unlocked: true, operative: true });
  await expect(button(page, "检查当前会话")).toBeEnabled();
  await button(page, "检查当前会话").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === original.user_id &&
      f.session.session_id === original.session_id &&
      f.session.role === original.role,
  );
  await expect(page.locator('[role="dialog"]')).toHaveCount(count);
  await expect(top(page)).toHaveCount(1);
  if (count === 2)
    expect(
      await page
        .locator('[role="dialog"]')
        .first()
        .evaluate(
          (node) =>
            (node as HTMLElement).inert &&
            node.getAttribute("aria-hidden") === "true",
        ),
    ).toBe(true);
  expect(
    await top(page).evaluate(
      (node) =>
        !(node as HTMLElement).inert &&
        node.getAttribute("aria-hidden") !== "true",
    ),
  ).toBe(true);
  await focusContained(page);
  expect(await oldHeading!.evaluate((node) => node.isConnected)).toBe(false);
  await oldHeading!.dispose();
  expect((await writes(page)).length === before).toBe(true);
}
async function deliveryLayout(page: Page, width: number) {
  const problems = await panel(page).evaluate((region, width) => {
    const out: string[] = [],
      rect = region.getBoundingClientRect(),
      rows = [...region.querySelectorAll("tbody tr")];
    if (
      document.documentElement.scrollWidth > innerWidth + 1 ||
      region.scrollWidth > region.clientWidth + 1
    )
      out.push("horizontal overflow");
    if (document.scrollingElement!.scrollHeight > innerHeight + 1)
      out.push("outer vertical overflow");
    if (rect.left < 0 || rect.right > innerWidth + 1) out.push("region bounds");
    if (!rows.length) out.push("no real row");
    for (const row of rows) {
      const cells = [...row.querySelectorAll<HTMLElement>("td")];
      if (cells.length !== 6) out.push("six fields");
      for (const cell of cells) {
        const r = cell.getBoundingClientRect();
        if (
          r.left < rect.left - 1 ||
          r.right > rect.right + 1 ||
          cell.scrollWidth > cell.clientWidth + 1
        )
          out.push("field bounds");
        if (width <= 680 && getComputedStyle(cell).display !== "grid")
          out.push("narrow field layout");
      }
    }
    const caption = region.querySelector("caption")!.getBoundingClientRect();
    if (caption.width > 2 || caption.height > 2)
      out.push("caption not visually hidden");
    const main = document.querySelector<HTMLElement>("main.app-content")!;
    if (main.scrollHeight < main.clientHeight) out.push("main scroll geometry");
    return out;
  }, width);
  expect(problems).toEqual([]);
  await recipient(page).scrollIntoViewIfNeeded();
  await expect(recipient(page)).toBeInViewport();
  for (const cell of await page
    .locator(".smtp-delivery tbody tr")
    .first()
    .locator("td")
    .all()) {
    await cell.scrollIntoViewIfNeeded();
    await expect(cell).toBeInViewport();
  }
  await button(page, "下一页").scrollIntoViewIfNeeded();
  await expect(button(page, "下一页")).toBeInViewport();
}
test("[navigation] active section ownership, two native confirmations and eight layouts", async ({
  page,
}) => {
  await setup(page);
  expect((await writes(page)).length).toBe(0);
  await page.goto("/system/users");
  const nav = page.getByRole("navigation", { name: "系统设置", exact: true });
  await expect(nav.getByRole("link")).toHaveCount(10);
  await expect(nav.locator(".settings-group-toggle")).toHaveCount(4);
  let after = await seq(page);
  await nav.getByRole("link", { name: "SMTP", exact: true }).click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/system/smtp" &&
      f.status === 200 &&
      f.ended &&
      f.configured === true,
  );
  expect(
    (await facts(page)).filter((f) =>
      f.path.startsWith("/api/v1/system/mail-jobs"),
    ).length,
  ).toBe(0);
  await page
    .getByRole("textbox", { name: "发件显示名", exact: true })
    .fill("configuration draft");
  const switcher = button(page, "测试与任务");
  await switcher.click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await focusContained(page);
  await page.keyboard.press("Escape");
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(switcher).toBeFocused();
  await switcher.click();
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(switcher).toBeFocused();
  await switcher.click();
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await factAfter(
    page,
    after,
    (f) => f.path === listPath && f.status === 200 && f.ended && !!f.items,
  );
  await expect(
    page.getByRole("form", { name: "修改 SMTP 配置", exact: true }),
  ).toHaveCount(0);
  await recipient(page).fill("retained.draft@example.test");
  await button(page, "配置").click();
  await expect(top(page)).toHaveAccessibleName("放弃投递草稿或原请求追踪？");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await expect(button(page, "配置")).toBeFocused();
  await expect(recipient(page)).toHaveValue("retained.draft@example.test");
  await detail(page, material().source_id);
  await button(page, "申请新重试周期").click();
  await expect(top(page)).toHaveAccessibleName("放弃收件草稿并申请重试？");
  await top(page)
    .getByRole("button", { name: "放弃草稿和追踪", exact: true })
    .click();
  await expect(top(page)).toHaveAccessibleName("申请新重试周期？");
  const layeredBack = page.goBack();
  await expect(page.locator('[role="dialog"]')).toHaveCount(2);
  await restored(page, 2);
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await layeredBack;
  await expect(page.locator('[role="dialog"]')).toHaveCount(1);
  await expect(top(page)).toHaveAccessibleName("申请新重试周期？");
  await focusContained(page);
  await top(page)
    .getByRole("button", { name: "取消申请", exact: true })
    .click();
  await fallback(page);
  await expect(recipient(page)).toHaveValue("");
  // A read cancelled by checking is retired; the user explicitly re-reads it.
  await reread(page, material().source_id);
  const retry = button(page, "申请新重试周期");
  await retry.click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(retry).toBeFocused();
  await retry.click();
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(retry).toBeFocused();
  await ipc("arm-drop");
  await recipient(page).fill("navigation.original@example.test");
  await button(page, "请求测试发送").click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await ipc("wait-cut", { phase: "sent" });
  const posted = (await writes(page)).length;
  await button(page, "配置").click();
  await expect(top(page)).toHaveAccessibleName("放弃投递草稿或原请求追踪？");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  expect((await writes(page)).length === posted).toBe(true);
  await button(page, "配置").click();
  await top(page)
    .getByRole("button", { name: "放弃草稿和追踪", exact: true })
    .click();
  await expect(panel(page)).toHaveCount(0);
  await expect(button(page, "读取当前配置")).toBeEnabled();
  await button(page, "测试与任务").click();
  await expect(panel(page)).toBeVisible();
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(recipient(page)).toHaveValue("");
  await expect(
    page.getByRole("region", { name: "投递任务详情", exact: true }),
  ).toBeVisible();
  await expect(button(page, "刷新任务列表")).toHaveCount(0);
  await button(page, "返回任务列表").click();
  await refresh(page);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 768, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await deliveryLayout(page, width);
      if (width === 390) {
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        await expect(top(page).getByRole("link")).toHaveCount(10);
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
      await expect(page.locator(".ui-overlay")).toHaveCount(0);
      await expect(recipient(page)).toHaveValue("");
      expect(await page.locator('input[type="password"]').count()).toBe(0);
      expect(
        await page.evaluate(
          () =>
            !Object.keys(localStorage).some((k) =>
              /smtp|recipient|intent|csrf/i.test(k),
            ) &&
            !Object.keys(sessionStorage).some((k) =>
              /smtp|recipient|intent|csrf/i.test(k),
            ) &&
            !location.search,
        ),
      ).toBe(true);
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
      await expect(title(page, "SMTP")).toBeInViewport();
      const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
      if (images)
        await page.screenshot({
          path: join(images, `smtp-delivery-${theme}-${width}.png`),
          fullPage: true,
        });
      layouts++;
    }
  }
  expect((await ipc("failure-facts")).session_failures).toBe(1);
  result({
    default_no_delivery: true,
    two_sections: true,
    dirty: true,
    two_confirmations: true,
    checking: true,
    same_session: true,
    focus: true,
    escape: true,
    pointer: true,
    tab: true,
    drawer: true,
    no_overflow: true,
    safe_capture: true,
    layouts,
  });
});
