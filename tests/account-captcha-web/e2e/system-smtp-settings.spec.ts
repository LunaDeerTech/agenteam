import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
const endpoint = "/api/v1/system/smtp";
type Settings = {
  id: string;
  version: string;
  configured: boolean;
  host: string;
  port: number;
  encryption: string;
  username: string;
  sender_email: string;
  sender_name: string;
  credential_present: boolean;
  auto_retry_count: string;
  retry_interval_seconds: string;
};
type Receipt = { applied_version: string; settings: Settings };
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
  receipt?: Receipt;
  session?: { user_id: string; session_id: string; role: string };
};
type ObservedWindow = Window & {
  __smtpFacts?: Fact[];
  __smtpSequence?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  second: Credential;
  passwords: string[];
  baseline: DBFacts;
  initial: Settings;
} =>
  JSON.parse(
    readFileSync(join(directory, "smtp-settings-material.json"), "utf8"),
  );
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "smtp-settings-result.json"),
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
    request = join(directory, "smtp-settings-ipc.json"),
    ack = join(directory, `smtp-settings-ack-${sequence}.json`);
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
              fact.path === "/api/v1/system/smtp/unconfigure"
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
              const raw =
                fact.method === "GET" ? value : object(value.settings);
              if (
                !raw ||
                (fact.method !== "GET" &&
                  (!closed(value, ["applied_version", "settings"]) ||
                    !decimal(
                      value.applied_version,
                      1n,
                      9223372036854775807n,
                    ))) ||
                !closed(raw, [
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
                ]) ||
                !id(raw.id) ||
                !decimal(raw.version, 1n, 9223372036854775807n) ||
                typeof raw.configured !== "boolean" ||
                typeof raw.credential_present !== "boolean" ||
                ![
                  "host",
                  "encryption",
                  "username",
                  "sender_email",
                  "sender_name",
                ].every((k) => typeof raw[k] === "string") ||
                typeof raw.port !== "number" ||
                !Number.isInteger(raw.port) ||
                !decimal(raw.auto_retry_count, 0n, 5n) ||
                !decimal(raw.retry_interval_seconds, 10n, 3600n)
              )
                return;
              if (raw.configured) {
                if (
                  raw.port < 1 ||
                  raw.port > 65535 ||
                  !["tls", "starttls", "none"].includes(
                    raw.encryption as string,
                  ) ||
                  !raw.host ||
                  !raw.sender_email ||
                  (raw.username !== "") !== raw.credential_present
                )
                  return;
              } else if (
                raw.port !== 0 ||
                raw.credential_present ||
                [
                  "host",
                  "encryption",
                  "username",
                  "sender_email",
                  "sender_name",
                ].some((k) => raw[k] !== "")
              )
                return;
              const settings: Settings = {
                id: raw.id,
                version: raw.version,
                configured: raw.configured,
                host: raw.host as string,
                port: raw.port,
                encryption: raw.encryption as string,
                username: raw.username as string,
                sender_email: raw.sender_email as string,
                sender_name: raw.sender_name as string,
                credential_present: raw.credential_present,
                auto_retry_count: raw.auto_retry_count,
                retry_interval_seconds: raw.retry_interval_seconds,
              };
              if (fact.method === "GET") fact.settings = settings;
              else if (
                BigInt(settings.version) >=
                BigInt(value.applied_version as string)
              )
                fact.receipt = {
                  applied_version: value.applied_version as string,
                  settings,
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
  page.getByRole("form", { name: "修改 SMTP 配置", exact: true });
const top = (page: Page) => page.getByRole("dialog");
const fields = [
  ["host", "SMTP 主机"],
  ["port", "端口"],
  ["username", "认证用户名"],
  ["sender_email", "发件邮箱"],
  ["sender_name", "发件显示名"],
  ["auto_retry_count", "自动重试次数"],
  ["retry_interval_seconds", "重试间隔（秒）"],
] as const;
const field = (page: Page, key: (typeof fields)[number][0]) =>
  page.getByRole("textbox", {
    name: fields.find(([k]) => k === key)![1],
    exact: true,
  });
const password = (page: Page) =>
  page.getByLabel("新密码（仅写入）", { exact: true });
const facts = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__smtpFacts ?? []);
const seq = (page: Page) =>
  page.evaluate(() => (window as ObservedWindow).__smtpSequence ?? 0);
const writes = async (page: Page) =>
  (await facts(page)).filter(
    (f) =>
      (f.path === endpoint || f.path === endpoint + "/unconfigure") &&
      f.method !== "GET",
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
async function login(page: Page, who: "admin" | "member" | "second" = "admin") {
  const data = material()[who],
    after = await seq(page);
  await page.locator("#login-email").fill(data.email);
  try {
    await page.locator("#login-password").fill(data.password);
  } catch {
    throw new Error("private SMTP login input failed");
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
  await page.goto("/system/smtp");
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
type DBFacts = {
  settings: Settings;
  commands: number;
  audits: number;
  original_commands: number;
  original_audits: number;
  reference_count: number;
  current_reference_valid: boolean;
  same_reference: boolean;
  previous_cleanup_registered: boolean;
  delivery_unchanged: boolean;
  forbidden_calls: number;
};
async function dbFacts() {
  const v = (await ipc("facts")).facts as DBFacts;
  expect(
    v.delivery_unchanged &&
      v.forbidden_calls === 0 &&
      v.current_reference_valid,
  ).toBe(true);
  return v;
}
const settingsEqual = (a: Settings, b: Settings) =>
  Object.keys(a).length === 12 &&
  Object.keys(b).length === 12 &&
  Object.keys(a).every(
    (k) => a[k as keyof Settings] === b[k as keyof Settings],
  );
async function matchesForm(page: Page, value: Settings) {
  await expect(form(page).locator("p code").first()).toHaveText(value.version);
  for (const [key] of fields) {
    if (
      !value.configured &&
      !["auto_retry_count", "retry_interval_seconds"].includes(key)
    )
      continue;
    await expect(field(page, key)).toHaveValue(String(value[key]));
  }
}
async function fill(
  page: Page,
  values: Partial<Record<(typeof fields)[number][0], string | number>>,
) {
  for (const [key] of fields)
    if (values[key] !== undefined)
      await field(page, key).fill(String(values[key]));
}
async function setPassword(page: Page, index: number) {
  try {
    await password(page).fill(material().passwords[index]!);
  } catch {
    throw new Error("private SMTP input failed");
  }
  expect(
    (await password(page).inputValue()) === material().passwords[index],
  ).toBe(true);
}
async function encryption(page: Page, value: "tls" | "starttls" | "none") {
  await button(page, "加密方式").click();
  const label = {
    tls: "TLS（tls）",
    starttls: "STARTTLS（starttls）",
    none: "无加密（none）",
  }[value];
  await page.getByRole("option", { name: label, exact: true }).click();
}
async function saved(page: Page, after: number, method: "PUT" | "POST") {
  const write = await factAfter(
    page,
    after,
    (f) =>
      (f.path === endpoint || f.path === endpoint + "/unconfigure") &&
      f.method === method &&
      f.status === 200 &&
      f.ended &&
      !!f.receipt,
  );
  await expect(button(page, "读取当前配置")).toBeEnabled();
  await matchesForm(page, write.receipt!.settings);
  const db = await dbFacts();
  expect(settingsEqual(write.receipt!.settings, db.settings)).toBe(true);
  return { ...write.receipt!, evidence: db };
}
async function save(page: Page, kind: "configure" | "policy" = "configure") {
  const after = await seq(page);
  await button(
    page,
    kind === "configure" ? "保存 SMTP 配置" : "保存重试策略",
  ).click();
  return saved(page, after, kind === "configure" ? "PUT" : "POST");
}
async function refresh(page: Page) {
  const after = await seq(page);
  await button(page, "读取当前配置").click();
  return currentRead(page, after);
}
async function discard(page: Page) {
  await button(page, "取消修改").click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
}
async function configure(page: Page) {
  await button(page, "配置 SMTP").click();
  await fill(page, {
    host: "mail.example.com",
    port: "587",
    sender_email: "sender@example.com",
  });
  await encryption(page, "starttls");
}
async function disable(page: Page) {
  await button(page, "停用并清除配置").click();
  await expect(top(page)).toHaveAccessibleName("停用并清除 SMTP 配置？");
  const after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认停用并清除", exact: true })
    .click();
  return saved(page, after, "POST");
}
async function noPrivateExposure(page: Page) {
  const values = material().passwords;
  const clean = await page.evaluate((values) => {
    const readable =
      document.body.innerText +
      document.documentElement.outerHTML +
      JSON.stringify(Object.entries(localStorage)) +
      JSON.stringify(Object.entries(sessionStorage));
    return values.every((v) => !readable.includes(v));
  }, values);
  expect(clean).toBe(true);
  expect(
    (await facts(page)).some((f) => /lookup|smtp\/test|mail-jobs/.test(f.path)),
  ).toBe(false);
}
async function cutSave(
  page: Page,
  kind: "configure" | "disable" = "configure",
) {
  await ipc("arm-drop");
  if (kind === "disable") {
    await button(page, "停用并清除配置").click();
    await top(page)
      .getByRole("button", { name: "确认停用并清除", exact: true })
      .click();
  } else await button(page, "保存 SMTP 配置").click();
  await expect(title(page, "请求结果未确认")).toBeVisible();
  await expect(button(page, "检查当前会话与配置")).toBeEnabled();
  const drop = await ipc("drop-facts");
  expect(drop.dropped === 1 && drop.armed === false && !!drop.result).toBe(
    true,
  );
  return drop.result as Receipt;
}
async function checkOriginal(page: Page) {
  const after = await seq(page);
  await button(page, "检查当前会话与配置").click();
  const current = await currentRead(page, after);
  await expect(title(page, "请求结果未确认")).toBeVisible();
  return current;
}
async function retry(page: Page, method: "PUT" | "POST") {
  const after = await seq(page);
  await button(page, "重试原请求").click();
  const receipt = await saved(page, after, method);
  const drop = await ipc("drop-facts");
  expect(drop.replayed === 1 && drop.same_original === true).toBe(true);
  return receipt;
}
async function restored(page: Page, count: number, fail = true) {
  const original = await session(page),
    after = await seq(page),
    before = (await writes(page)).length;
  // This node is intentionally inert below the already-open confirmation.
  const oldHeading = await page
    .locator(".system-smtp-settings > h1")
    .elementHandle();
  if (fail) await ipc("arm-session-fail");
  // This deliberately synthetic pageshow observes the existing App restoration listener.
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
  await expect(page.locator('[role="dialog"]')).toHaveCount(count);
  await expect(top(page)).toHaveCount(count ? 1 : 0);
  if (count) {
    expect(
      await top(page).evaluate(
        (node) =>
          !(node as HTMLElement).inert &&
          node.getAttribute("aria-hidden") !== "true",
      ),
    ).toBe(true);
    await expect
      .poll(() =>
        top(page).evaluate((node) => node.contains(document.activeElement)),
      )
      .toBe(true);
  }
  expect(await oldHeading!.evaluate((node) => node.isConnected)).toBe(false);
  await oldHeading!.dispose();
  expect((await writes(page)).length).toBe(before);
}
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

test("[lifecycle] three explicit transports, unconfigured policy and saved disable policy", async ({
  page,
}) => {
  await setup(page);
  const initial = material().initial;
  expect(
    initial.configured === false &&
      settingsEqual((await dbFacts()).settings, initial),
  ).toBe(true);
  await expect(button(page, "保存重试策略")).toBeDisabled();
  expect((await writes(page)).length).toBe(0);
  for (const value of ["-1", "6", "00", " 1"]) {
    await field(page, "auto_retry_count").fill(value);
    await expect(field(page, "auto_retry_count")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    await expect(button(page, "保存重试策略")).toBeDisabled();
  }
  await discard(page);
  expect((await writes(page)).length).toBe(0);
  await fill(page, { auto_retry_count: "0", retry_interval_seconds: "3600" });
  const policy = await save(page, "policy");
  expect(
    policy.settings.configured === false &&
      policy.settings.auto_retry_count === "0",
  ).toBe(true);
  await button(page, "配置 SMTP").click();
  await expect(field(page, "host")).toHaveValue("");
  await expect(field(page, "port")).toHaveValue("");
  await expect(button(page, "加密方式")).toContainText("请选择");
  await discard(page);
  await configure(page);
  await field(page, "port").fill("0655");
  await expect(button(page, "保存 SMTP 配置")).toBeDisabled();
  await field(page, "port").fill("65535");
  await fill(page, {
    sender_name: " Sender ",
    auto_retry_count: "5",
    retry_interval_seconds: "10",
  });
  for (const mode of ["tls", "starttls", "none"] as const) {
    await encryption(page, mode);
    const receipt = await save(page);
    expect(
      receipt.settings.encryption === mode &&
        !receipt.settings.credential_present &&
        receipt.settings.username === "",
    ).toBe(true);
    await refresh(page);
  }
  const count = (await writes(page)).length;
  await expect(button(page, "保存 SMTP 配置")).toBeDisabled();
  await fill(page, { sender_name: "cancelled" });
  await discard(page);
  expect((await writes(page)).length).toBe(count);
  await fill(page, { auto_retry_count: "1" });
  await button(page, "停用并清除配置").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(top(page)).toContainText("保留自动重试 5 次");
  await top(page)
    .getByRole("button", { name: "取消停用", exact: true })
    .click();
  expect((await writes(page)).length).toBe(count);
  const unconfigured = await disable(page);
  expect(
    unconfigured.settings.configured === false &&
      unconfigured.settings.auto_retry_count === "5" &&
      unconfigured.settings.retry_interval_seconds === "10",
  ).toBe(true);
  await refresh(page);
  await noPrivateExposure(page);
  result({
    strict_current: true,
    policy: true,
    three_modes: true,
    no_op: true,
    validation: true,
    cancel: true,
    persisted: true,
    disabled_saved_policy: true,
    no_delivery: true,
  });
});

test("[credential] private new/keep/replace/remove material and exact Secret references", async ({
  page,
}) => {
  await setup(page);
  const initial = await dbFacts();
  await fill(page, { username: "owned-auth" });
  await expect(button(page, "保存 SMTP 配置")).toBeDisabled();
  await setPassword(page, 0);
  await button(page, "取消修改").click();
  await restored(page, 1);
  await expect(password(page)).toHaveValue("");
  await expect(
    page.getByText("已保留新密码输入；重挂后不会回显。", { exact: false }),
  ).toBeVisible();
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await fallback(page);
  await save(page);
  let db = await dbFacts();
  expect(
    db.settings.credential_present &&
      db.reference_count === 1 &&
      db.commands === initial.commands + 1,
  ).toBe(true);
  await noPrivateExposure(page);
  await fill(page, { sender_name: "keep existing" });
  await password(page).fill("");
  await save(page);
  db = await dbFacts();
  expect(
    db.same_reference &&
      db.settings.credential_present &&
      db.reference_count === 1,
  ).toBe(true);
  await setPassword(page, 1);
  await cutSave(page);
  await expect(password(page)).toBeDisabled();
  await expect(password(page)).toHaveValue("");
  await expect(button(page, "清除新密码输入")).toBeDisabled();
  await checkOriginal(page);
  const replaced = await retry(page, "PUT");
  db = replaced.evidence;
  expect(
    db.settings.credential_present &&
      !db.same_reference &&
      db.previous_cleanup_registered &&
      db.reference_count === 1,
  ).toBe(true);
  await page
    .getByRole("checkbox", { name: "移除凭据并清空用户名", exact: true })
    .check();
  await expect(field(page, "username")).toHaveValue("");
  await expect(password(page)).toBeDisabled();
  const removed = await save(page);
  db = removed.evidence;
  expect(
    !db.settings.credential_present &&
      db.settings.username === "" &&
      db.previous_cleanup_registered &&
      db.reference_count === 0,
  ).toBe(true);
  await setPassword(page, 0);
  await expect(button(page, "保存 SMTP 配置")).toBeDisabled();
  const count = (await writes(page)).length;
  await button(page, "清除新密码输入").click();
  expect((await writes(page)).length).toBe(count);
  await fill(page, { username: "new-auth" });
  await setPassword(page, 1);
  await save(page);
  const disabled = await disable(page);
  db = disabled.evidence;
  expect(
    !db.settings.configured &&
      !db.settings.credential_present &&
      db.reference_count === 0 &&
      db.previous_cleanup_registered,
  ).toBe(true);
  await noPrivateExposure(page);
  result({
    new_material: true,
    empty_keep: true,
    replacement: true,
    remove: true,
    mismatch: true,
    disabled_reference: true,
    private_remount: true,
    private_safety: true,
  });
});

test("[concurrency] two real administrators, original version conflict and explicit adoption", async ({
  page,
}) => {
  await setup(page);
  const before = await dbFacts(),
    baseVersion = before.settings.version;
  expect(material().admin.user_id !== material().second.user_id).toBe(true);
  await fill(page, { username: "browser-auth", sender_name: "browser draft" });
  await setPassword(page, 0);
  const competing = (await ipc("compete-configure")).result as Receipt;
  expect(competing.applied_version === String(BigInt(baseVersion) + 1n)).toBe(
    true,
  );
  const accepted = await dbFacts(),
    after = await seq(page);
  await button(page, "保存 SMTP 配置").click();
  await factAfter(
    page,
    after,
    (f) =>
      f.path === endpoint &&
      f.method === "PUT" &&
      f.status === 409 &&
      f.code === "VERSION_CONFLICT" &&
      f.ended,
  );
  await expect(title(page, "需要核对最新配置")).toBeVisible();
  await expect(field(page, "sender_name")).toHaveValue("browser draft");
  await expect(form(page).locator("p code").first()).toHaveText(baseVersion);
  await expect(
    page.getByText("已保留新密码输入；重挂后不会回显。", { exact: false }),
  ).toBeVisible();
  let db = await dbFacts();
  expect(
    settingsEqual(db.settings, accepted.settings) &&
      db.commands === accepted.commands &&
      db.audits === accepted.audits &&
      db.reference_count === accepted.reference_count,
  ).toBe(true);
  const readAfter = await seq(page);
  await button(page, "读取最新配置").click();
  await currentRead(page, readAfter);
  await expect(field(page, "sender_name")).toHaveValue("browser draft");
  await expect(form(page).locator("p code").first()).toHaveText(baseVersion);
  await button(page, "采用最新值重新编辑").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(form(page).locator("p code").first()).toHaveText(
    competing.settings.version,
  );
  await expect(password(page)).toHaveValue("");
  await expect(
    page.getByText("已保留新密码输入；重挂后不会回显。", { exact: false }),
  ).toHaveCount(0);
  await fill(page, { sender_name: "explicitly reviewed" });
  await setPassword(page, 0);
  await save(page);
  db = await dbFacts();
  expect(
    db.commands === accepted.commands + 1 &&
      db.audits === accepted.audits + 1 &&
      db.reference_count === 1,
  ).toBe(true);
  await noPrivateExposure(page);
  result({
    two_admins: true,
    real_conflict: true,
    draft_and_material: true,
    explicit_adoption: true,
    atomic_facts: true,
  });
});

test("[outcome] accepted lost PUT and POST replay with original material and opposite current settings", async ({
  page,
}) => {
  await setup(page);
  await fill(page, { username: "original-auth" });
  await setPassword(page, 0);
  const lost = await cutSave(page);
  const accepted = await dbFacts();
  expect(
    accepted.original_commands === 1 && accepted.original_audits === 1,
  ).toBe(true);
  const later = (await ipc("compete-disable")).result as Receipt;
  expect(!later.settings.configured).toBe(true);
  const count = (await writes(page)).length;
  const observed = await checkOriginal(page);
  expect(
    settingsEqual(observed, later.settings) &&
      (await writes(page)).length === count,
  ).toBe(true);
  const recovered = await retry(page, "PUT");
  expect(
    recovered.applied_version === lost.applied_version &&
      settingsEqual(recovered.settings, later.settings),
  ).toBe(true);
  await expect(
    page.getByText("原操作已确认，当前配置已变化。", { exact: true }),
  ).toBeVisible();
  let db = await dbFacts();
  expect(
    db.original_commands === 1 &&
      db.original_audits === 1 &&
      !db.settings.configured,
  ).toBe(true);
  await configure(page);
  await save(page);
  const lostPost = await cutSave(page, "disable");
  const newer = (await ipc("compete-configure")).result as Receipt;
  await checkOriginal(page);
  const replayedPost = await retry(page, "POST");
  expect(
    replayedPost.applied_version === lostPost.applied_version &&
      settingsEqual(replayedPost.settings, newer.settings) &&
      replayedPost.settings.configured,
  ).toBe(true);
  db = await dbFacts();
  expect(db.original_commands === 1 && db.original_audits === 1).toBe(true);
  await ipc("arm-get-fail");
  const before = (await writes(page)).length;
  await button(page, "读取当前配置").click();
  await expect(title(page, "原操作已确认，当前配置读取失败")).toBeVisible();
  expect((await writes(page)).length === before).toBe(true);
  await refresh(page);
  await fill(page, { sender_name: "accepted but abandoned" });
  const abandoned = await cutSave(page);
  await button(page, "放弃本次操作").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await refresh(page);
  db = await dbFacts();
  expect(settingsEqual(db.settings, abandoned.settings)).toBe(true);
  await noPrivateExposure(page);
  result({
    accepted_put_cut: true,
    opposite_current: true,
    exact_original: true,
    historical_put: true,
    accepted_post_cut: true,
    historical_post: true,
    unique_facts: true,
    read_failure: true,
    abandon_no_rollback: true,
    no_lookup: true,
  });
});

test("[authority] real role and Session revocation isolate both configuration and original recovery", async ({
  page,
}) => {
  await setup(page, "member");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await button(page, "退出登录").click();
  await login(page);
  await page.goto("/system/smtp");
  await currentRead(page, 0);
  async function restoreAdministrator(original: NonNullable<Fact["session"]>) {
    await ipc("promote", original);
    const after = await seq(page);
    await page.evaluate(() => window.dispatchEvent(new Event("pageshow")));
    await factAfter(
      page,
      after,
      (f) =>
        f.path === "/api/v1/session" &&
        f.status === 200 &&
        f.ended &&
        f.session?.role === "admin",
    );
    await currentRead(page, after);
  }
  let original = await session(page);
  await ipc("demote", original);
  let after = await seq(page);
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
  await expect(form(page)).toHaveCount(0);
  await restoreAdministrator(original);
  await fill(page, { username: "write-rejected" });
  await setPassword(page, 0);
  original = await session(page);
  await ipc("demote", original);
  after = await seq(page);
  await button(page, "保存 SMTP 配置").click();
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
  await restoreAdministrator(original);
  await expect(password(page)).toHaveValue("");
  await fill(page, { username: "replay-rejected" });
  await setPassword(page, 0);
  await cutSave(page);
  await checkOriginal(page);
  original = await session(page);
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
  await restoreAdministrator(original);
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(password(page)).toHaveValue("");
  await fill(page, { sender_name: "revoked recovery" });
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
  await page.goto("/system/smtp");
  await currentRead(page, 0);
  const fresh = await session(page);
  expect(
    fresh.user_id === original.user_id &&
      fresh.session_id !== original.session_id,
  ).toBe(true);
  await expect(button(page, "重试原请求")).toHaveCount(0);
  await expect(password(page)).toHaveValue("");
  await fill(page, { username: "leaving" });
  await setPassword(page, 1);
  await button(page, "退出登录").click();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page).toHaveURL(/\/login$/);
  await login(page, "member");
  await page.goto("/system/smtp");
  await expect(title(page, "无权访问系统设置")).toBeVisible();
  expect(
    (await facts(page)).some((f) => f.path.startsWith("/api/v1/system/")),
  ).toBe(false);
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await noPrivateExposure(page);
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

async function layout(page: Page, width: number) {
  const problems = await page
    .locator(".system-smtp-settings")
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
          'form input:not([type="checkbox"])',
        ),
      ];
      if (inputs.length !== 8) out.push("eight fields");
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
  await password(page).scrollIntoViewIfNeeded();
  await expect(password(page)).toBeInViewport();
  await button(page, "取消修改").scrollIntoViewIfNeeded();
  await expect(button(page, "取消修改")).toBeInViewport();
}
test("[navigation] eight leaves, two restored confirmations and eight responsive layouts", async ({
  page,
}) => {
  await page.addInitScript(observeNative);
  await page.goto("/system/smtp");
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(new URL(page.url()).searchParams.get("return")).toBe("/system/smtp");
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
  await nav.getByRole("link", { name: "SMTP", exact: true }).click();
  await currentRead(page, after);
  await expect(
    nav.getByRole("link", { name: "SMTP", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await fill(page, { sender_name: "retained draft" });
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
  await restored(page, 1);
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await fallback(page);
  await expect(field(page, "sender_name")).toHaveValue("retained draft");
  await expect(page).toHaveURL(/\/system\/smtp$/);
  await nav.getByRole("link", { name: "用户", exact: true }).click();
  await restored(page, 1, false);
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(page).toHaveURL(/\/system\/users$/);
  after = await seq(page);
  await nav.getByRole("link", { name: "SMTP", exact: true }).click();
  await currentRead(page, after);
  const stop = button(page, "停用并清除配置");
  const focusObservation = await stop.evaluateHandle((element) => {
    const trigger = element as HTMLButtonElement;
    const heading = document.querySelector(".system-smtp-settings > h1");
    let phase = "before-open";
    const events: Record<string, string | boolean | null>[] = [];
    const record = (event: string) => {
      if (events.length >= 32) return;
      events.push({
        phase,
        event,
        active_tag: document.activeElement?.tagName ?? null,
        active_trigger: document.activeElement === trigger,
        active_heading: document.activeElement === heading,
        active_body: document.activeElement === document.body,
        trigger_connected: trigger.isConnected,
        trigger_disabled: trigger.disabled,
      });
    };
    const focus = () => record("focusin");
    const blur = () => record("trigger-blur");
    const disabled = new MutationObserver(() => record("disabled-change"));
    document.addEventListener("focusin", focus, true);
    trigger.addEventListener("blur", blur);
    disabled.observe(trigger, {
      attributes: true,
      attributeFilter: ["disabled"],
    });
    record("installed");
    return {
      mark(value: string) {
        phase = value;
        record("stage");
      },
      finish() {
        record("finished");
        document.removeEventListener("focusin", focus, true);
        trigger.removeEventListener("blur", blur);
        disabled.disconnect();
        return events;
      },
    };
  });
  try {
    await stop.click();
    await focusContained(page);
    await focusObservation.evaluate((observation) =>
      observation.mark("before-close"),
    );
    await page.keyboard.press("Escape");
    await expect(page.locator(".ui-overlay")).toHaveCount(0);
    await expect(stop).toBeFocused();
  } finally {
    const safe = await focusObservation.evaluate((observation) =>
      observation.finish(),
    );
    console.log("SMTP_DISABLE_FOCUS_SAFE", JSON.stringify(safe));
    await focusObservation.dispose();
  }
  await stop.click();
  // Browser history remains available while the page behind a modal is inert.
  const layeredBack = page.goBack();
  await expect(page.locator('[role="dialog"]')).toHaveCount(2);
  await restored(page, 2);
  await focusContained(page);
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await layeredBack;
  await expect(page.locator('[role="dialog"]')).toHaveCount(1);
  await expect(top(page)).toHaveAccessibleName("停用并清除 SMTP 配置？");
  await focusContained(page);
  await top(page)
    .getByRole("button", { name: "取消停用", exact: true })
    .click();
  await fallback(page);
  await stop.click();
  await restored(page, 1, false);
  after = await seq(page);
  await top(page)
    .getByRole("button", { name: "确认停用并清除", exact: true })
    .click();
  await saved(page, after, "POST");
  await configure(page);
  await save(page);
  await fill(page, { sender_name: "browser back draft" });
  const back = page.goBack();
  await expect(top(page)).toHaveAccessibleName("放弃未保存修改？");
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await back;
  await expect(page).toHaveURL(/\/system\/smtp$/);
  await cutSave(page);
  const accepted = await dbFacts();
  await nav.getByRole("link", { name: "用户", exact: true }).click();
  await restored(page, 1, false);
  await top(page)
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await fallback(page);
  await expect(title(page, "请求结果未确认")).toBeVisible();
  let db = await dbFacts();
  expect(
    settingsEqual(db.settings, accepted.settings) &&
      db.commands === accepted.commands &&
      db.audits === accepted.audits,
  ).toBe(true);
  await button(page, "放弃本次操作").click();
  await top(page)
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await refresh(page);
  // Long legal ordinary values are rendered only after a real save; no private material.
  await fill(page, {
    host: "a".repeat(63) + "." + "b".repeat(63) + ".example.com",
    sender_email: "a".repeat(64) + "@" + "b".repeat(63) + ".example.com",
    sender_name: "长".repeat(80),
  });
  await save(page);
  let layouts = 0;
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const original = await field(page, "port").inputValue();
      await field(page, "port").fill(" 000655350000000000000000 ");
      await expect(field(page, "port")).toHaveAttribute("aria-invalid", "true");
      await layout(page, width);
      await field(page, "port").fill(original);
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
      await expect(password(page)).toHaveValue("");
      await noPrivateExposure(page);
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
          path: join(images, `smtp-settings-${theme}-${width}.png`),
          fullPage: true,
        });
      layouts++;
    }
  }
  expect((await ipc("failure-facts")).session_failures).toBe(2);
  result({
    seven_leaves: true,
    eleventh_return: true,
    dirty: true,
    uncertain: true,
    two_confirmations: true,
    checking: true,
    fallback: true,
    native_focus: true,
    drawer: true,
    no_overflow: true,
    layouts,
  });
});
