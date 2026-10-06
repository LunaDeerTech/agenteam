import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const endpoint = "/api/v1/system/account-settings";
type Settings = {
  id: string;
  version: string;
  session_idle_seconds: string;
  session_absolute_seconds: string;
  password_reset_seconds: string;
  challenge_after_failures: string;
  lifetime_changes_apply_to: "newly_issued_sessions_and_tokens";
};
type Credential = { email: string; password: string; user_id: string };
type Fact = {
  sequence: number;
  path: string;
  method: string;
  status: number;
  bytes: number;
  ended: boolean;
  code?: string;
  commit_state?: string;
  settings?: Settings;
  session?: { user_id: string; session_id: string; role: string };
};
type ObservedWindow = Window & {
  __securityFacts?: Fact[];
  __securitySequence?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  initial: Settings;
} =>
  JSON.parse(
    readFileSync(join(directory, "account-security-material.json"), "utf8"),
  );
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "account-security-result.json"),
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
    request = join(directory, "account-security-ipc.json"),
    ack = join(directory, `account-security-ack-${sequence}.json`);
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
  scope.__securityFacts = [];
  scope.__securitySequence = 0;
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
    /^[1-9][0-9]{0,18}$/.test(value) &&
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
        sequence: ++scope.__securitySequence!,
        path: url.pathname,
        method:
          init?.method ?? (input instanceof Request ? input.method : "GET"),
        status: 0,
        bytes: 0,
        ended: false,
      };
      scope.__securityFacts!.push(fact);
      void original
        .then(
          (response) => {
            fact.status = response.status;
            if (!(
              fact.path === "/api/v1/session" ||
              fact.path === "/api/v1/system/account-settings"
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
                (response.status === 200
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
              if (response.status !== 200) {
                if (
                  typeof value.code === "string" &&
                  [
                    "UNAUTHENTICATED",
                    "SESSION_REVOKED",
                    "CSRF_FAILED",
                    "FORBIDDEN",
                    "VERSION_CONFLICT",
                    "INVALID_ARGUMENT",
                    "RESOURCE_BUSY",
                    "INTERNAL_ERROR",
                    "UNAVAILABLE",
                    "IDEMPOTENCY_KEY_REUSED",
                    "COMMIT_UNKNOWN",
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
              if (
                !closed(value, [
                  "id",
                  "version",
                  "session_idle_seconds",
                  "session_absolute_seconds",
                  "password_reset_seconds",
                  "challenge_after_failures",
                  "lifetime_changes_apply_to",
                ]) ||
                !id(value.id) ||
                !decimal(value.version, 1n, 9223372036854775807n) ||
                !decimal(value.session_idle_seconds, 900n, 2592000n) ||
                !decimal(value.session_absolute_seconds, 3600n, 7776000n) ||
                !decimal(value.password_reset_seconds, 300n, 7200n) ||
                !decimal(value.challenge_after_failures, 1n, 20n) ||
                BigInt(value.session_idle_seconds) >
                  BigInt(value.session_absolute_seconds) ||
                value.lifetime_changes_apply_to !==
                  "newly_issued_sessions_and_tokens"
              )
                return;
              fact.settings = {
                id: value.id,
                version: value.version,
                session_idle_seconds: value.session_idle_seconds,
                session_absolute_seconds: value.session_absolute_seconds,
                password_reset_seconds: value.password_reset_seconds,
                challenge_after_failures: value.challenge_after_failures,
                lifetime_changes_apply_to: value.lifetime_changes_apply_to,
              };
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
const form = (page: Page) =>
  page.getByRole("form", { name: "修改账号安全配置", exact: true });
const fields = [
  ["session_idle_seconds", "会话空闲期限（秒）"],
  ["session_absolute_seconds", "会话绝对期限（秒）"],
  ["password_reset_seconds", "密码重置有效期（秒）"],
  ["challenge_after_failures", "触发挑战的失败次数（次）"],
] as const;
const field = (page: Page, key: (typeof fields)[number][0]) =>
  page.getByRole("textbox", {
    name: fields.find(([name]) => name === key)![1],
    exact: true,
  });
const top = (page: Page) => page.getByRole("dialog");
const facts = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__securityFacts ?? []);
const seq = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__securitySequence ?? 0);
async function writes(page: Page) {
  return (await facts(page)).filter(
    (f) => f.path === endpoint && f.method === "PUT",
  );
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
async function login(page: Page, who: "admin" | "member" = "admin") {
  const data = material()[who],
    after = await seq(page);
  await page.locator("#login-email").fill(data.email);
  try {
    await page.locator("#login-password").fill(data.password);
  } catch {
    throw new Error("private Account security login input failed");
  }
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
async function setup(page: Page, who: "admin" | "member" = "admin") {
  await page.addInitScript(observeNative);
  await page.goto("/system/account-security");
  await login(page, who);
  if (who === "admin") await currentRead(page, 0);
}
async function currentRead(page: Page, after: number) {
  const read = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "GET" &&
      f.status === 200 &&
      f.ended &&
      !!f.settings,
  );
  await expect(button(page, "读取当前配置")).toBeEnabled();
  return read.settings!;
}
async function dbFacts() {
  return (await ipc("facts")).facts as {
    settings: Settings;
    commands: number;
    audits: number;
    original_commands: number;
    original_audits: number;
    invocations: number;
  };
}
async function matchesForm(page: Page, value: Settings) {
  await expect(form(page).locator("p code").first()).toHaveText(value.version);
  for (const [key] of fields)
    await expect(field(page, key)).toHaveValue(value[key]);
}
async function fill(page: Page, values: Partial<Settings>) {
  for (const [key] of fields)
    if (values[key] !== undefined) await field(page, key).fill(values[key]!);
}
async function save(page: Page) {
  const after = await seq(page);
  await button(page, "保存配置").click();
  const written = await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !!f.settings,
  );
  const current = await currentRead(page, written.sequence);
  await matchesForm(page, current);
  return written.settings!;
}
async function discard(page: Page) {
  await button(page, "取消修改").click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
}
async function refresh(page: Page) {
  const after = await seq(page);
  await button(page, "读取当前配置").click();
  return currentRead(page, after);
}
async function cutSave(page: Page) {
  await ipc("arm-drop");
  const after = await seq(page);
  await button(page, "保存配置").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !f.settings,
  );
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await expect(button(page, "检查当前会话与配置")).toBeEnabled();
}

test("[lifecycle] current settings, exact four-field save and new-only Session lifetimes", async ({
  page,
}) => {
  await setup(page);
  const initial = material().initial,
    baseline = await dbFacts(),
    original = await session(page);
  expect(await currentRead(page, 0)).toEqual(initial);
  await matchesForm(page, initial);
  await expect(button(page, "保存配置")).toBeDisabled();
  await ipc("capture-session", original);
  await fill(page, { password_reset_seconds: "3600" });
  await discard(page);
  await matchesForm(page, initial);
  const invalid: [(typeof fields)[number][0], string][] = [
    ["session_idle_seconds", "899"],
    ["session_idle_seconds", "2592001"],
    ["session_absolute_seconds", "3599"],
    ["session_absolute_seconds", "7776001"],
    ["password_reset_seconds", "299"],
    ["password_reset_seconds", "7201"],
    ["challenge_after_failures", "0"],
    ["challenge_after_failures", "21"],
    ["session_idle_seconds", "0900"],
    ["session_idle_seconds", " 900"],
  ];
  for (const [key, value] of invalid) {
    await field(page, key).fill(value);
    await expect(field(page, key)).toHaveValue(value);
    await expect(field(page, key)).toHaveAttribute("aria-invalid", "true");
    await expect(button(page, "保存配置")).toBeDisabled();
    await field(page, key).fill(initial[key]);
  }
  await fill(page, {
    session_idle_seconds: "7200",
    session_absolute_seconds: "3600",
  });
  await expect(button(page, "保存配置")).toBeDisabled();
  expect((await writes(page)).length).toBe(0);
  expect(await dbFacts()).toEqual(baseline);
  const desired = {
    session_idle_seconds: "1800",
    session_absolute_seconds: "7200",
    password_reset_seconds: "900",
    challenge_after_failures: "4",
  };
  await fill(page, desired);
  const saved = await save(page);
  expect(saved).toEqual({
    ...initial,
    ...desired,
    version: String(BigInt(initial.version) + 1n),
  });
  const after = await dbFacts();
  expect(after.settings).toEqual(saved);
  expect(after.commands - baseline.commands).toBe(1);
  expect(after.audits - baseline.audits).toBe(1);
  expect(await refresh(page)).toEqual(saved);
  expect((await writes(page)).length).toBe(1);
  const life = await ipc("lifetime-facts");
  expect(life.old_unchanged && life.new_matches && life.different_session).toBe(
    true,
  );
  await expect(
    page.getByText("期限修改只影响随后签发的 Session/token", { exact: false }),
  ).toBeVisible();
  expect(after.invocations).toBe(0);
  result({
    current_settings: true,
    four_fields: true,
    no_op: true,
    cancelled: true,
    ranges: true,
    persisted: true,
    old_session_unchanged: true,
    new_session_lifetimes: true,
  });
});

test("[concurrency] two formal administrator Sessions compete and explicitly adopt the latest baseline", async ({
  page,
  browser,
}) => {
  await setup(page);
  const baseline = await dbFacts(),
    otherContext = await browser.newContext({
      baseURL: process.env.AGENTEAM_AUTH_WEB_ORIGIN,
    });
  try {
    const other = await otherContext.newPage();
    await setup(other);
    expect(
      (await session(other)).session_id !== (await session(page)).session_id,
    ).toBe(true);
    await matchesForm(page, baseline.settings);
    await matchesForm(other, baseline.settings);
    const winner = {
      session_idle_seconds: "1800",
      session_absolute_seconds: "7200",
      password_reset_seconds: "900",
      challenge_after_failures: "4",
    };
    const loser = {
      session_idle_seconds: "3600",
      session_absolute_seconds: "14400",
      password_reset_seconds: "3600",
      challenge_after_failures: "6",
    };
    await fill(page, winner);
    await fill(other, loser);
    const attempts = await ipc("attempt-facts");
    await save(page);
    const accepted = await dbFacts(),
      after = await seq(other);
    await button(other, "保存配置").click();
    const rejected = await factAfter(
      other,
      after,
      (f) =>
        f.path === endpoint &&
        f.method === "PUT" &&
        f.status === 409 &&
        f.code === "VERSION_CONFLICT" &&
        f.ended,
    );
    expect(rejected.settings).toBeUndefined();
    expect(["not_started", "not_committed"]).toContain(rejected.commit_state);
    await expect(title(other, "需要核对最新配置")).toBeVisible();
    await matchesForm(other, { ...baseline.settings, ...loser });
    expect(await dbFacts()).toEqual(accepted);
    const attempted = await ipc("attempt-facts");
    expect(attempted.attempts - attempts.attempts).toBe(2);
    expect(attempted.distinct_keys - attempts.distinct_keys).toBe(2);
    expect(accepted.commands - baseline.commands).toBe(1);
    expect(accepted.audits - baseline.audits).toBe(1);
    const before = await seq(other);
    await button(other, "读取最新配置").click();
    expect(await currentRead(other, before)).toEqual(accepted.settings);
    await matchesForm(other, { ...baseline.settings, ...loser });
    await button(other, "采用最新值重新编辑").click();
    await expect(top(other)).toHaveAccessibleName("放弃草稿并采用最新值？");
    await top(other)
      .getByRole("button", { name: "继续编辑", exact: true })
      .click();
    await matchesForm(other, { ...baseline.settings, ...loser });
    await button(other, "采用最新值重新编辑").click();
    await top(other)
      .getByRole("button", { name: "放弃修改", exact: true })
      .click();
    await matchesForm(other, accepted.settings);
    expect((await writes(other)).length).toBe(1);
    await fill(other, loser);
    await save(other);
    const final = await dbFacts();
    expect(final.settings).toEqual({
      ...accepted.settings,
      ...loser,
      version: String(BigInt(accepted.settings.version) + 1n),
    });
    expect(final.commands - baseline.commands).toBe(2);
    expect(final.audits - baseline.audits).toBe(2);
  } finally {
    await otherContext.close();
  }
  result({
    two_sessions: true,
    same_version: true,
    real_conflict: true,
    draft_preserved: true,
    explicit_adoption: true,
    atomic_facts: true,
  });
});

test("[outcome] accepted response cut, current GET evidence and immutable historical replay", async ({
  page,
}) => {
  await setup(page);
  const baseline = await dbFacts();
  await fill(page, {
    session_idle_seconds: "1800",
    session_absolute_seconds: "7200",
    password_reset_seconds: "900",
    challenge_after_failures: "4",
  });
  await cutSave(page);
  const accepted = await dbFacts(),
    lost = await ipc("drop-facts");
  expect(lost.dropped === 1 && lost.replayed === 0).toBe(true);
  expect(accepted.commands - baseline.commands).toBe(1);
  expect(accepted.audits - baseline.audits).toBe(1);
  expect(accepted.original_commands).toBe(1);
  expect(accepted.original_audits).toBe(1);
  const newer = (await ipc("compete")).settings as Settings;
  expect(BigInt(newer.version)).toBe(BigInt(accepted.settings.version) + 1n);
  const after = await seq(page),
    count = (await writes(page)).length;
  await button(page, "检查当前会话与配置").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      !!f.session,
  );
  expect(await currentRead(page, after)).toEqual(newer);
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await expect(button(page, "重试原请求")).toBeEnabled();
  expect((await writes(page)).length).toBe(count);
  const retry = await seq(page);
  await button(page, "重试原请求").click();
  const historical = await factAfter(
    page,
    retry,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !!f.settings,
  );
  expect(historical.settings).toEqual(accepted.settings);
  expect(await currentRead(page, historical.sequence)).toEqual(newer);
  await matchesForm(page, newer);
  const replayed = await ipc("drop-facts"),
    final = await dbFacts();
  expect(
    replayed.dropped === 1 &&
      replayed.replayed === 1 &&
      replayed.same_original === true,
  ).toBe(true);
  expect(final.original_commands).toBe(1);
  expect(final.original_audits).toBe(1);
  expect(final.commands - baseline.commands).toBe(2);
  expect(final.audits - baseline.audits).toBe(2);
  await fill(page, { password_reset_seconds: "7200" });
  await ipc("arm-get-fail");
  const next = await seq(page);
  await button(page, "保存配置").click();
  const confirmed = await factAfter(
    page,
    next,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 200 &&
      f.ended &&
      !!f.settings,
  );
  await factAfter(
    page,
    confirmed.sequence,
    (f) =>
      f.path === endpoint && f.method === "GET" && f.status === 503 && f.ended,
  );
  await expect(title(page, "保存已确认，当前配置读取失败")).toBeVisible();
  await expect(form(page)).toHaveCount(0);
  const beforeRead = (await writes(page)).length;
  await refresh(page);
  expect((await writes(page)).length).toBe(beforeRead);
  await fill(page, { password_reset_seconds: "900" });
  await cutSave(page);
  const committed = await dbFacts();
  await button(page, "放弃本次操作").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(form(page)).toHaveCount(0);
  expect(await dbFacts()).toEqual(committed);
  await refresh(page);
  await matchesForm(page, committed.settings);
  expect((await facts(page)).some((f) => f.path.endsWith("/lookup"))).toBe(
    false,
  );
  expect((await ipc("failure-facts")).read_failures).toBe(1);
  result({
    accepted_cut: true,
    current_not_receipt: true,
    historical_result: true,
    same_original: true,
    unique_facts: true,
    confirmed_read_failure: true,
    abandon_no_rollback: true,
    no_lookup: true,
  });
});

async function promoteAndRestore(
  page: Page,
  identity: NonNullable<Fact["session"]>,
) {
  await ipc("promote", identity);
  const after = await seq(page);
  await button(page, "重新检查权限").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === "/api/v1/session" &&
      f.status === 200 &&
      f.ended &&
      f.session?.user_id === identity.user_id &&
      f.session.session_id === identity.session_id &&
      f.session.role === "admin",
  );
  await currentRead(page, after);
  await expect(button(page, "保存配置")).toBeDisabled();
  await expect(button(page, "重试原请求")).toHaveCount(0);
}
test("[authority] real current 403, denied historical replay, exact 401 and identity cleanup", async ({
  page,
}) => {
  await setup(page, "member");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(
    page.getByRole("navigation", { name: "系统设置", exact: true }),
  ).toHaveCount(0);
  await button(page, "退出登录").click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page);
  await page.goto("/system/account-security");
  await currentRead(page, 0);
  let original = await session(page),
    after = await seq(page);
  await ipc("demote", original);
  await button(page, "读取当前配置").click();
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
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await promoteAndRestore(page, original);
  await fill(page, { password_reset_seconds: "900" });
  const beforeRejected = await dbFacts();
  original = await session(page);
  await ipc("demote", original);
  after = await seq(page);
  await button(page, "保存配置").click();
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
  expect(await dbFacts()).toEqual(beforeRejected);
  await promoteAndRestore(page, original);
  await fill(page, { password_reset_seconds: "900" });
  await cutSave(page);
  after = await seq(page);
  await button(page, "检查当前会话与配置").click();
  await currentRead(page, after);
  await expect(button(page, "重试原请求")).toBeEnabled();
  original = await session(page);
  const accepted = await dbFacts();
  await ipc("demote", original);
  after = await seq(page);
  await button(page, "重试原请求").click();
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
  expect(await dbFacts()).toEqual(accepted);
  await promoteAndRestore(page, original);
  await fill(page, { password_reset_seconds: "3600" });
  await cutSave(page);
  original = await session(page);
  await ipc("revoke-session", original);
  after = await seq(page);
  await button(page, "检查当前会话与配置").click();
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
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await login(page);
  await page.goto("/system/account-security");
  await currentRead(page, 0);
  const fresh = await session(page);
  expect(
    fresh.user_id === original.user_id &&
      fresh.session_id !== original.session_id,
  ).toBe(true);
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(button(page, "保存配置")).toBeDisabled();
  await fill(page, { password_reset_seconds: "7200" });
  await cutSave(page);
  await button(page, "退出登录").click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/account-security");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  result({
    ordinary_denied: true,
    get_forbidden: true,
    put_forbidden: true,
    replay_forbidden: true,
    revoked: true,
    new_session: true,
    switched: true,
  });
});

async function focusContained(page: Page) {
  await expect(top(page)).toHaveCount(1);
  await expect
    .poll(() =>
      top(page).evaluate((dialog) => dialog.contains(document.activeElement)),
    )
    .toBe(true);
  for (let n = 0; n < 6; n++) {
    await page.keyboard.press("Tab");
    expect(
      await top(page).evaluate((dialog) =>
        dialog.contains(document.activeElement),
      ),
    ).toBe(true);
  }
  await page.keyboard.press("Shift+Tab");
  expect(
    await top(page).evaluate((dialog) =>
      dialog.contains(document.activeElement),
    ),
  ).toBe(true);
}
async function restoredConfirmation(
  page: Page,
  fail: boolean,
  decision: "continue" | "discard",
) {
  const original = await session(page),
    after = await seq(page),
    count = (await writes(page)).length;
  if (fail) await ipc("arm-session-fail");
  // A synthetic pageshow exercises the App listener; subsequent 200/EOF is real.
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
            unlocked: document.body.style.overflow !== "hidden",
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
  if (fail) {
    await expect(page.locator(".ui-overlay")).toHaveCount(0);
    await expect(button(page, "检查当前会话")).toBeEnabled();
    await button(page, "检查当前会话").click();
  }
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
  await expect(page.locator('[role="dialog"]')).toHaveCount(1);
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  expect(
    await top(page).evaluate(
      (node) =>
        !(node as HTMLElement).inert &&
        node.getAttribute("aria-hidden") !== "true",
    ),
  ).toBe(true);
  await focusContained(page);
  await top(page)
    .getByRole("button", {
      name: decision === "continue" ? "继续编辑" : "放弃修改",
      exact: true,
    })
    .click();
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  expect((await writes(page)).length).toBe(count);
  if (decision === "continue") {
    expect(
      await page.evaluate(
        () =>
          document.activeElement instanceof HTMLElement &&
          document.activeElement !== document.body &&
          document.activeElement.isConnected &&
          !document.activeElement.closest('[inert],[aria-hidden="true"]'),
      ),
    ).toBe(true);
    await expect(title(page, "账号安全")).toBeFocused();
    await page.keyboard.press("Tab");
    expect(
      await page.evaluate(
        () =>
          document.activeElement instanceof HTMLElement &&
          document.activeElement !== document.body &&
          !document.activeElement.closest('[inert],[aria-hidden="true"]'),
      ),
    ).toBe(true);
  }
}
async function layout(page: Page, width: number) {
  const problems = await page
    .locator(".system-account-security")
    .evaluate((region, width) => {
      const out: string[] = [],
        rect = region.getBoundingClientRect();
      if (
        document.documentElement.scrollWidth > innerWidth + 1 ||
        region.scrollWidth > region.clientWidth + 1
      )
        out.push("horizontal overflow");
      if (document.scrollingElement!.scrollHeight > innerHeight + 1)
        out.push("outer vertical overflow");
      if (rect.left < 0 || rect.right > innerWidth + 1) out.push("page bounds");
      const inputs = [
        ...region.querySelectorAll<HTMLInputElement>(
          'input[inputmode="numeric"]',
        ),
      ];
      if (inputs.length !== 4) out.push("four fields");
      for (const input of inputs) {
        const bound = input.getBoundingClientRect();
        if (
          bound.left < rect.left - 1 ||
          bound.right > rect.right + 1 ||
          bound.width < 80
        )
          out.push("field bounds");
        if (!input.labels?.length || !input.getAttribute("aria-describedby"))
          out.push("label and range");
      }
      for (const node of region.querySelectorAll<HTMLElement>("p,dd,.ui-field"))
        if (node.scrollWidth > node.clientWidth + 1) out.push("text overflow");
      if (
        width <= 680 &&
        inputs.length >= 2 &&
        Math.abs(
          inputs[0]!.getBoundingClientRect().left -
            inputs[1]!.getBoundingClientRect().left,
        ) > 1
      )
        out.push("narrow columns");
      return out;
    }, width);
  expect(problems).toEqual([]);
  for (const [key] of fields) {
    await field(page, key).scrollIntoViewIfNeeded();
    await expect(field(page, key)).toBeInViewport();
  }
  await button(page, "取消修改").scrollIntoViewIfNeeded();
  await expect(button(page, "取消修改")).toBeInViewport();
}
test("[navigation] six leaves, one restored inline confirmation and eight responsive layouts", async ({
  page,
}) => {
  await page.addInitScript(observeNative);
  await page.goto("/system/account-security");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(new URL(page.url()).searchParams.get("return")).toBe(
    "/system/account-security",
  );
  await login(page);
  await currentRead(page, 0);
  await page.goto("/system");
  await expect(page).toHaveURL(/\/system\/users$/);
  const nav = page.getByRole("navigation", { name: "系统设置", exact: true });
  await expect(nav.getByRole("link")).toHaveCount(8);
  await expect(nav.locator(".settings-group-toggle")).toHaveCount(3);
  const platform = nav.getByRole("button", { name: "平台配置", exact: true });
  await platform.focus();
  await page.keyboard.press("Space");
  await expect(platform).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("Space");
  await expect(platform).toHaveAttribute("aria-expanded", "true");
  let after = await seq(page);
  await nav.getByRole("link", { name: "账号安全", exact: true }).click();
  await currentRead(page, after);
  await expect(
    nav.getByRole("link", { name: "账号安全", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await fill(page, { password_reset_seconds: "3600" });
  const cancel = button(page, "取消修改");
  await cancel.click();
  await focusContained(page);
  await page.keyboard.press("Escape");
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(cancel).toBeFocused();
  await cancel.click();
  await page
    .locator(".ui-overlay")
    .last()
    .click({ position: { x: 3, y: 3 } });
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(cancel).toBeFocused();
  await nav.getByRole("link", { name: "用户", exact: true }).click();
  await restoredConfirmation(page, true, "continue");
  await expect(page).toHaveURL(/\/system\/account-security$/);
  await expect(field(page, "password_reset_seconds")).toHaveValue("3600");
  await nav.getByRole("link", { name: "用户", exact: true }).click();
  await restoredConfirmation(page, false, "discard");
  await expect(page).toHaveURL(/\/system\/users$/);
  after = await seq(page);
  await nav.getByRole("link", { name: "账号安全", exact: true }).click();
  await currentRead(page, after);
  await fill(page, { password_reset_seconds: "3600" });
  const back = page.goBack();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await back;
  await expect(page).toHaveURL(/\/system\/account-security$/);
  await cutSave(page);
  const accepted = await dbFacts();
  await nav.getByRole("link", { name: "用户", exact: true }).click();
  await restoredConfirmation(page, false, "continue");
  await expect(title(page, "请求结果未确认")).toBeVisible();
  expect(await dbFacts()).toEqual(accepted);
  await button(page, "放弃本次操作").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await refresh(page);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const original = await field(page, "session_idle_seconds").inputValue();
      await field(page, "session_idle_seconds").fill(
        " 000900000000000000000000 ",
      );
      await expect(field(page, "session_idle_seconds")).toHaveAttribute(
        "aria-invalid",
        "true",
      );
      await layout(page, width);
      await field(page, "session_idle_seconds").fill(original);
      await layout(page, width);
      if (width === 390) {
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        await expect(top(page).getByRole("link")).toHaveCount(8);
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
      await expect(title(page, "账号安全")).toBeInViewport();
      const images = process.env.AGENTEAM_AUTH_WEB_IMAGES;
      if (images)
        await page.screenshot({
          path: join(images, `account-security-${theme}-${width}.png`),
          fullPage: true,
        });
      layouts++;
    }
  }
  expect((await ipc("failure-facts")).session_failures).toBe(1);
  result({
    six_leaves: true,
    tenth_return: true,
    dirty: true,
    uncertain: true,
    checking: true,
    focus: true,
    drawer: true,
    no_overflow: true,
    layouts,
  });
});
