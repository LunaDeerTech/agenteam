import {
  test,
  expect,
  type Page,
  type Locator,
  type Response,
  type Request as BrowserRequest,
} from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";
const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
type Credential = { email: string; password: string; user_id: string };
type Delivery = {
  job_id: string;
  accepted_at: string;
  phase: string;
  attempts: string;
  version: string;
  channel: string | null;
  attempt_result: string | null;
  reason?: string;
};
type Invitation = {
  id: string;
  email: string;
  version: string;
  created_at: string;
  expires_at: string;
  latest_delivery: Delivery;
};
type InvitationPage = { items: Invitation[]; next_cursor?: string };
type NativeFact = {
  request: number;
  method: string;
  path: string;
  status: number;
  stage: "headers" | "done" | "invalid" | "overflow" | "read-failed";
  code?: string;
  receipt?: Record<string, string>;
  page?: InvitationPage;
  empty?: boolean;
};
type ObservedWindow = Window & {
  __invitationFacts?: NativeFact[];
  __invitationRequest?: number;
};
const material = (): {
  admin: Credential;
  member: Credential;
  expected: Invitation[];
} =>
  JSON.parse(
    readFileSync(join(directory, "invitations-material.json"), "utf8"),
  );
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "invitations-result.json"),
    JSON.stringify({ completed: true, ...value }),
    { mode: 0o600 },
  );
}
let sequence = 0;
async function ipc(action: string, fields: Record<string, string> = {}) {
  const next = ++sequence,
    request = join(directory, "invitations-ipc.json"),
    ack = join(directory, `invitations-ack-${next}.json`);
  writeFileSync(
    request + ".tmp",
    JSON.stringify({ sequence: next, action, ...fields }),
    { mode: 0o600 },
  );
  renameSync(request + ".tmp", request);
  await expect.poll(() => existsSync(ack), { timeout: 14_000 }).toBe(true);
  const value = JSON.parse(readFileSync(ack, "utf8"));
  expect(value.ok === true && value.sequence === next).toBe(true);
  return value as {
    items: Invitation[];
    single?: boolean;
    stable?: boolean;
    invalid?: boolean;
    redeemed?: boolean;
    commands?: number;
    intents?: number;
    dropped?: number;
    replayed?: number;
    armed?: boolean;
    failures?: number;
    advanced?: boolean;
    same_invitation?: boolean;
    joined?: boolean;
  };
}
async function lostReceipt(subject: Locator) {
  try {
    await expect(subject).toContainText("请求结果未确认");
  } catch (error) {
    const facts = await ipc("drop-facts");
    console.log("owned accepted-response control", JSON.stringify(facts));
    throw error;
  }
  const facts = await ipc("drop-facts");
  expect(
    facts.dropped === 1 &&
      facts.replayed === 0 &&
      facts.armed === false &&
      facts.commands === 1 &&
      facts.intents === 1,
  ).toBe(true);
}
// Observe the original native reader's result, returning each original Promise
// unchanged. No clone/tee, extra read or delayed abort; this is not a join test.
function observeNativeCodes() {
  const scope = window as ObservedWindow;
  scope.__invitationFacts = [];
  scope.__invitationRequest = 0;
  const uuid =
      "[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}",
    uuidValue = new RegExp(`^${uuid}$`),
    invitationPath = new RegExp(
      `^/api/v1/system/invitations/${uuid}/(resend|revoke)$`,
    ),
    deliveryPath = new RegExp(`^/api/v1/system/mail-jobs/${uuid}/retry$`);
  // Retain only the closed, bounded read DTO used by the existing wire/DB
  // comparison. This observes the application's original reader, never a copy.
  function readProjection(value: unknown): InvitationPage {
    const require = (condition: unknown) => {
      if (!condition) throw new Error("invalid closed read projection");
    };
    const shape = (
      value: unknown,
      required: string[],
      optional: string[] = [],
    ) => {
      require(value && typeof value === "object" && !Array.isArray(value));
      const row = value as Record<string, unknown>;
      require(
        required.every((key) => Object.hasOwn(row, key)) &&
          Object.keys(row).every(
            (key) => required.includes(key) || optional.includes(key),
          ),
      );
      return row;
    };
    const version = (value: unknown) =>
      typeof value === "string" &&
      /^[1-9][0-9]{0,18}$/.test(value) &&
      BigInt(value) <= 9223372036854775807n;
    const id = (value: unknown) =>
      typeof value === "string" && uuidValue.test(value);
    const instant = (value: unknown) => {
      if (
        typeof value !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(value)
      )
        return false;
      const year = Number(value.slice(0, 4)),
        month = Number(value.slice(5, 7)),
        day = Number(value.slice(8, 10)),
        leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0),
        days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
      return (
        month >= 1 &&
        month <= 12 &&
        day >= 1 &&
        day <= days[month - 1]! &&
        Number(value.slice(11, 13)) <= 23 &&
        Number(value.slice(14, 16)) <= 59 &&
        Number(value.slice(17, 19)) <= 59
      );
    };
    const email = (value: unknown) => {
      if (
        typeof value !== "string" ||
        value.length < 3 ||
        value.length > 254 ||
        !/^[\x21-\x7e]+$/.test(value) ||
        value !== value.toLowerCase()
      )
        return false;
      const parts = value.split("@"),
        atom =
          /^[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*$/;
      if (parts.length !== 2 || !atom.test(parts[0]!)) return false;
      const domain = parts[1]!;
      if (!(domain.startsWith("[") && domain.endsWith("]")))
        return atom.test(domain);
      const literal = domain.slice(1, -1),
        prefixed = literal.startsWith("ipv6:"),
        address = prefixed ? literal.slice(5) : literal;
      if (
        /^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(address) &&
        address.split(".").every((part) => Number(part) <= 255)
      )
        return true;
      if (!/^[0-9a-f:.]+$/.test(address) || !address.includes(":"))
        return false;
      try {
        const host = new URL(`http://[${address}]/`).hostname;
        return (
          prefixed || /^\[::ffff:[0-9a-f]{1,4}:[0-9a-f]{1,4}\]$/.test(host)
        );
      } catch {
        return false;
      }
    };
    const page = shape(value, ["items"], ["next_cursor"]);
    require(Array.isArray(page.items) && page.items.length <= 25);
    const ids = new Set<string>();
    let previous: Invitation | undefined;
    const items = (page.items as unknown[]).map((value) => {
      const row = shape(value, [
          "id",
          "email",
          "version",
          "created_at",
          "expires_at",
          "latest_delivery",
        ]),
        delivery = shape(
          row.latest_delivery,
          [
            "job_id",
            "accepted_at",
            "phase",
            "attempts",
            "version",
            "channel",
            "attempt_result",
          ],
          ["reason"],
        );
      require(
        id(row.id) &&
          email(row.email) &&
          version(row.version) &&
          instant(row.created_at) &&
          instant(row.expires_at),
      );
      require(
        id(delivery.job_id) &&
          instant(delivery.accepted_at) &&
          version(delivery.version) &&
          typeof delivery.attempts === "string" &&
          /^[0-6]$/.test(delivery.attempts),
      );
      require(
        [
          "enqueue_pending",
          "pending",
          "claimed",
          "sending",
          "retry_wait",
          "sent",
          "failed",
          "unknown",
          "cancelled",
        ].includes(delivery.phase as string),
      );
      require(
        delivery.channel === null ||
          delivery.channel === "smtp" ||
          delivery.channel === "backend_log",
      );
      require(
        delivery.attempt_result === null ||
          ["sent", "failed", "unknown", "cancelled"].includes(
            delivery.attempt_result as string,
          ),
      );
      if (Object.hasOwn(delivery, "reason"))
        require(
          [
            "sent",
            "token_invalid",
            "configuration_invalid",
            "policy_rejected",
            "network_failed",
            "smtp_rejected",
            "timeout",
            "cancelled",
            "unknown",
          ].includes(delivery.reason as string),
        );
      if (delivery.channel === null)
        require(
          delivery.attempt_result === null &&
            !["claimed", "sending", "retry_wait", "sent", "failed"].includes(
              delivery.phase as string,
            ),
        );
      else require(delivery.attempts !== "0");
      if (delivery.phase === "enqueue_pending")
        require(
          delivery.attempts === "0" &&
            delivery.version === "1" &&
            delivery.channel === null &&
            !Object.hasOwn(delivery, "reason"),
        );
      const item = row as unknown as Invitation;
      require(
        !ids.has(item.id) &&
          (!previous ||
            item.created_at < previous.created_at ||
            (item.created_at === previous.created_at && item.id < previous.id)),
      );
      ids.add(item.id);
      previous = item;
      return item;
    });
    if (Object.hasOwn(page, "next_cursor"))
      require(
        typeof page.next_cursor === "string" &&
          page.next_cursor.length >= 1 &&
          page.next_cursor.length <= 8192 &&
          items.length === 25,
      );
    return {
      items,
      ...(Object.hasOwn(page, "next_cursor")
        ? { next_cursor: page.next_cursor as string }
        : {}),
    };
  }
  const original = window.fetch;
  window.fetch = function (this: unknown, ...args) {
    const pending = Reflect.apply(
      original,
      this,
      args,
    ) as Promise<globalThis.Response>;
    // Any observation exception must leave the original fetch/reader untouched.
    try {
      const request = ++scope.__invitationRequest!,
        input = args[0],
        method = (
          args[1]?.method ?? (input instanceof Request ? input.method : "GET")
        ).toUpperCase(),
        url = new URL(
          input instanceof Request ? input.url : String(input),
          location.href,
        );
      if (
        url.origin !== location.origin ||
        !["GET", "POST"].includes(method) ||
        !(
          url.pathname === "/api/v1/system/invitations" ||
          invitationPath.test(url.pathname) ||
          deliveryPath.test(url.pathname)
        )
      )
        return pending;
      void pending
        .then((response) => {
          const fact: NativeFact = {
            request,
            method,
            path: url.pathname,
            status: response.status,
            stage: "headers",
          };
          scope.__invitationFacts!.push(fact);
          const receiptKeys =
            method === "POST" &&
            ((url.pathname === "/api/v1/system/invitations" &&
              response.status === 201) ||
              (invitationPath.test(url.pathname) &&
                url.pathname.endsWith("/resend") &&
                response.status === 202))
              ? ["id", "job_id", "version"]
              : method === "POST" &&
                  deliveryPath.test(url.pathname) &&
                  response.status === 202
                ? ["job_id", "version"]
                : undefined;
          const list =
            method === "GET" &&
            url.pathname === "/api/v1/system/invitations" &&
            response.status === 200;
          if (response.status === 204 && !response.body) {
            fact.empty = true;
            fact.stage = "done";
            return;
          }
          if (
            (!receiptKeys &&
              !list &&
              response.status < 400 &&
              response.status !== 204) ||
            !response.body
          )
            return;
          const stream = response.body,
            originalReader = stream.getReader;
          stream.getReader = function (
            this: ReadableStream<Uint8Array>,
            ...values: unknown[]
          ) {
            const reader = Reflect.apply(
              originalReader,
              this,
              values,
            ) as ReadableStreamDefaultReader<Uint8Array>;
            try {
              stream.getReader = originalReader;
              const nativeRead = reader.read;
              let bytes = new Uint8Array(600_000),
                size = 0,
                finished = false;
              const clear = () => {
                bytes.fill(0);
                bytes = new Uint8Array();
                finished = true;
              };
              reader.read = function (...readArgs: unknown[]) {
                const reading = Reflect.apply(
                  nativeRead,
                  this,
                  readArgs,
                ) as ReturnType<typeof nativeRead>;
                void reading
                  .then(
                    (chunk) => {
                      if (finished) return;
                      try {
                        if (!chunk.done) {
                          if (size + chunk.value.byteLength > bytes.length) {
                            fact.stage = "overflow";
                            clear();
                            return;
                          }
                          bytes.set(chunk.value, size);
                          size += chunk.value.byteLength;
                          return;
                        }
                        if (response.status === 204) {
                          fact.empty = size === 0;
                          fact.stage = "done";
                          clear();
                          return;
                        }
                        const body = JSON.parse(
                          new TextDecoder("utf-8", { fatal: true }).decode(
                            bytes.subarray(0, size),
                          ),
                        );
                        if (
                          !body ||
                          typeof body !== "object" ||
                          Array.isArray(body)
                        )
                          throw new Error("invalid safe projection");
                        if (
                          response.status >= 400 &&
                          typeof body.code === "string" &&
                          /^[A-Z][A-Z0-9_]{0,127}$/.test(body.code)
                        )
                          fact.code = body.code;
                        if (
                          receiptKeys &&
                          Object.keys(body).length === receiptKeys.length &&
                          receiptKeys.every(
                            (key) =>
                              Object.hasOwn(body, key) &&
                              typeof body[key] === "string",
                          ) &&
                          /^[1-9][0-9]{0,18}$/.test(body.version) &&
                          BigInt(body.version) <= 9223372036854775807n &&
                          uuidValue.test(body.job_id) &&
                          (!receiptKeys.includes("id") ||
                            uuidValue.test(body.id))
                        )
                          fact.receipt = Object.fromEntries(
                            receiptKeys.map((key) => [
                              key,
                              body[key] as string,
                            ]),
                          );
                        if (list) fact.page = readProjection(body);
                        fact.stage = "done";
                        clear();
                      } catch {
                        fact.stage = "invalid";
                        clear();
                      }
                    },
                    () => {
                      fact.stage = "read-failed";
                      clear();
                    },
                  )
                  .catch(() => undefined);
                return reading;
              };
            } catch {}
            return reader;
          } as typeof stream.getReader;
        })
        .catch(() => undefined);
    } catch {}
    return pending;
  };
}
test.beforeEach(async ({ context }) => {
  sequence = 0;
  await context.addInitScript(observeNativeCodes);
});
const button = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true });
const inviteButton = (page: Page) =>
  page.getByRole("button", { name: /^(邀请用户|邀请请求已接受)$/ });
async function fixedHeadingFact(page: Page, title: string) {
  const heading = page.getByRole("heading", { name: title, exact: true });
  if ((await heading.count()) !== 1) return { present: false };
  return heading.evaluate((element, title) => {
    const text = element.textContent ?? "",
      compact = text.replace(/\s/g, "");
    return {
      present: true,
      text:
        compact === title || compact === `!${title}`
          ? text
          : "unexpected fixed title",
      decorative_icon_hidden:
        element.querySelector("span")?.getAttribute("aria-hidden") === "true",
    };
  }, title);
}
const rowFor = (page: Page, email: string) =>
  page.locator("tbody tr").filter({
    has: page.locator("td[data-label='邮箱']").filter({ hasText: email }),
  });
async function privateFill(input: Locator, value: string) {
  try {
    await input.fill(value);
  } catch {
    throw new Error("private invitation credential entry failed");
  }
}
async function fillLogin(page: Page, credential: Credential) {
  await expect(page.locator("#login-email")).toBeEditable();
  await page.locator("#login-email").fill(credential.email);
  await privateFill(page.locator("#login-password"), credential.password);
  await button(page, "登录").click();
}
async function login(
  page: Page,
  credential: Credential,
  path = "/system/invitations",
  heading = "待注册邀请",
) {
  await page.goto(path);
  await fillLogin(page, credential);
  await expect(
    page.getByRole("heading", { name: heading, exact: true }),
  ).toBeVisible();
  if (heading === "待注册邀请")
    await expect(button(page, "刷新")).toBeEnabled();
}
async function read(page: Page, action: () => Promise<unknown>) {
  const after = await page.evaluate(
    () => (window as ObservedWindow).__invitationRequest ?? 0,
  );
  const pending = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/system/invitations" &&
      r.request().method() === "GET",
  );
  await action();
  const response = await pending;
  expect(response.status()).toBe(200);
  const observed = () =>
    page.evaluate(
      (after) =>
        (window as ObservedWindow).__invitationFacts?.filter(
          (fact) =>
            fact.request > after &&
            fact.method === "GET" &&
            fact.path === "/api/v1/system/invitations" &&
            fact.status === 200,
        ) ?? [],
      after,
    );
  await expect
    .poll(async () => {
      const facts = await observed();
      return (
        facts.length === 1 &&
        facts[0]!.stage === "done" &&
        facts[0]!.page !== undefined
      );
    })
    .toBe(true);
  const value = (await observed())[0]!.page!;
  await expect(button(page, "刷新")).toBeEnabled();
  return { value, url: new URL(response.url()) };
}
async function post(
  page: Page,
  suffix: string,
  action: () => Promise<unknown>,
  status: number,
) {
  const after = await page.evaluate(
    () => (window as ObservedWindow).__invitationRequest ?? 0,
  );
  const pending = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname.endsWith(suffix) &&
      r.request().method() === "POST",
  );
  await action();
  const response = await pending;
  expect(response.status()).toBe(status);
  const match = { after, path: new URL(response.url()).pathname, status };
  const observed = () =>
    page.evaluate(
      ({ after, path, status }) =>
        (window as ObservedWindow).__invitationFacts?.filter(
          (fact) =>
            fact.request > after &&
            fact.method === "POST" &&
            fact.path === path &&
            fact.status === status,
        ) ?? [],
      match,
    );
  await expect
    .poll(async () => {
      const facts = await observed();
      return (
        facts.length === 1 &&
        facts[0]!.stage === "done" &&
        (status === 204
          ? facts[0]!.empty === true
          : status >= 400
            ? facts[0]!.code !== undefined
            : facts[0]!.receipt !== undefined)
      );
    })
    .toBe(true);
  const fact = (await observed())[0]!;
  if (status === 204) {
    expect(fact.empty).toBe(true);
    return {} as Record<string, string>;
  }
  return status >= 400 ? { code: fact.code! } : fact.receipt!;
}
async function openCreate(page: Page, email: string) {
  await inviteButton(page).click();
  const dialog = page.getByRole("dialog", { name: "邀请用户", exact: true });
  await dialog.getByLabel("邮箱", { exact: false }).fill(email);
  return dialog;
}
async function create(page: Page, email: string) {
  const dialog = await openCreate(page, email);
  const receipt = await post(
    page,
    "/system/invitations",
    () => dialog.getByRole("button", { name: "发送邀请", exact: true }).click(),
    201,
  );
  await expect(dialog).toHaveCount(0);
  await expect(button(page, "刷新")).toBeEnabled();
  return receipt;
}
async function currentIdentity(page: Page) {
  return page.evaluate(async () => {
    const r = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (r.status !== 200)
      return { status: r.status, user_id: "", session_id: "" };
    const v = await r.json();
    return {
      status: r.status,
      user_id: v.user.id as string,
      session_id: v.session.id as string,
    };
  });
}
async function knownProblem(page: Page, response: Response, code: string) {
  let actual: string | undefined;
  try {
    actual = (await response.json()).code;
  } catch {}
  if (actual !== undefined) expect(actual).toBe(code);
  await expect
    .poll(() =>
      page.evaluate(
        ({ status, code }) =>
          (window as ObservedWindow).__invitationFacts?.some(
            (f) => f.status === status && f.code === code,
          ) ?? false,
        { status: response.status(), code },
      ),
    )
    .toBe(true);
}
function canonical(value: unknown): string {
  if (Array.isArray(value)) return "[" + value.map(canonical).join(",") + "]";
  if (value && typeof value === "object")
    return (
      "{" +
      Object.keys(value)
        .sort()
        .map(
          (k) =>
            JSON.stringify(k) +
            ":" +
            canonical((value as Record<string, unknown>)[k]),
        )
        .join(",") +
      "}"
    );
  return JSON.stringify(value);
}
async function matches(
  page: Page,
  wire: InvitationPage,
  expected: Invitation[],
) {
  expect(canonical(wire.items) === canonical(expected)).toBe(true);
  await expect(page.locator("tbody tr")).toHaveCount(expected.length);
  const display = await page.locator("tbody tr").evaluateAll((rows) =>
    rows.map((row) => ({
      fields: row.querySelectorAll("td").length,
      email: row.querySelector("td")?.textContent,
      times: [...row.querySelectorAll("time")].map((t) => [
        t.getAttribute("datetime"),
        t.getAttribute("aria-label"),
      ]),
      links: row.querySelectorAll("a").length,
    })),
  );
  expect(
    display.every(
      (r, i) =>
        r.fields === 5 &&
        r.email === expected[i]!.email &&
        r.links === 0 &&
        canonical(r.times) ===
          canonical(
            [
              expected[i]!.created_at,
              expected[i]!.expires_at,
              expected[i]!.latest_delivery.accepted_at,
            ].map((t) => [t, t]),
          ),
    ),
  ).toBe(true);
}

test("[lifecycle] real create, raw errors, canonical literal, resend, revoke and redemption", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin);
  await expect(page.getByText("暂无待注册邀请", { exact: true })).toBeVisible();
  const dialog = await openCreate(page, " invalid@example.com ");
  const invalid = await post(
    page,
    "/system/invitations",
    () => dialog.getByRole("button", { name: "发送邀请", exact: true }).click(),
    400,
  );
  expect(invalid.code).toBe("INVALID_ARGUMENT");
  expect(
    await dialog.evaluate(
      (element) =>
        element.contains(document.activeElement) &&
        document.activeElement?.matches(
          "input:not(:disabled), button:not(:disabled)",
        ),
    ),
  ).toBe(true);
  await expect(dialog.getByLabel("邮箱", { exact: false })).toHaveValue(
    " invalid@example.com ",
  );
  await dialog.getByLabel("邮箱", { exact: false }).fill(data.member.email);
  const registered = await post(
    page,
    "/system/invitations",
    () => dialog.getByRole("button", { name: "发送邀请", exact: true }).click(),
    400,
  );
  expect(registered.code).toBe("INVALID_ARGUMENT");
  await dialog.getByLabel("邮箱", { exact: false }).fill("");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  const email = "invitations-ui-lifecycle@example.com",
    created = await create(page, email);
  await ipc("wait", { id: created.id!, phase: "sent" });
  await read(page, () => button(page, "刷新").click());
  const limited = await post(
    page,
    "/resend",
    () =>
      rowFor(page, email)
        .getByRole("button", { name: "重发邀请", exact: true })
        .click(),
    429,
  );
  expect(limited.code).toBe("RATE_LIMITED");
  await ipc("cooldown", { id: created.id! });
  const resent = await post(
    page,
    "/resend",
    () =>
      rowFor(page, email)
        .getByRole("button", { name: "重发邀请", exact: true })
        .click(),
    202,
  );
  expect(resent.id === created.id && resent.job_id !== created.job_id).toBe(
    true,
  );
  await ipc("wait", { id: created.id!, phase: "sent" });
  expect((await ipc("stable", { id: created.id! })).stable).toBe(true);
  await expect(rowFor(page, email)).toBeVisible();
  let revocations = 0;
  page.on("request", (r) => {
    if (r.method() === "POST" && new URL(r.url()).pathname.endsWith("/revoke"))
      ++revocations;
  });
  await rowFor(page, email)
    .getByRole("button", { name: "撤销邀请", exact: true })
    .click();
  const revoke = page.getByRole("dialog", { name: "撤销邀请？", exact: true });
  await expect(revoke).toContainText("链接将失效");
  await revoke.getByRole("button", { name: "取消", exact: true }).click();
  expect(revocations).toBe(0);
  await rowFor(page, email)
    .getByRole("button", { name: "撤销邀请", exact: true })
    .click();
  await post(page, "/revoke", () => button(page, "确认撤销").click(), 204);
  await expect(rowFor(page, email)).toHaveCount(0);
  expect((await ipc("revoked-link", { id: created.id!, email })).invalid).toBe(
    true,
  );
  const literal = await create(page, "a@[IPv6:2001:db8::1]");
  await expect(rowFor(page, "a@[ipv6:2001:db8::1]")).toBeVisible();
  await rowFor(page, "a@[ipv6:2001:db8::1]")
    .getByRole("button", { name: "撤销邀请", exact: true })
    .click();
  await post(page, "/revoke", () => button(page, "确认撤销").click(), 204);
  expect(typeof literal.id === "string").toBe(true);
  for (const mapped of ["a@[::ffff:192.0.2.1]", "a@[::ffff:c000:201]"]) {
    const accepted = await create(page, mapped);
    expect(typeof accepted.id === "string").toBe(true);
    await expect(rowFor(page, mapped)).toBeVisible();
    await rowFor(page, mapped)
      .getByRole("button", { name: "撤销邀请", exact: true })
      .click();
    await post(page, "/revoke", () => button(page, "确认撤销").click(), 204);
  }
  const redeemedEmail = "invitations-ui-redeemed@example.com",
    pending = await create(page, redeemedEmail);
  expect(
    (await ipc("redeem", { id: pending.id!, email: redeemedEmail })).redeemed,
  ).toBe(true);
  await read(page, () => button(page, "刷新").click());
  await expect(rowFor(page, redeemedEmail)).toHaveCount(0);
  await page
    .getByRole("navigation", { name: "系统设置", exact: true })
    .getByRole("link", { name: "用户", exact: true })
    .click();
  await expect(page.locator("tbody")).toContainText(redeemedEmail);
  result({
    created: true,
    literal: true,
    resend: true,
    limited: true,
    revoked: true,
    redeemed: true,
  });
});

test("[delivery] real restricted log and owned SMTP failure to manual retry success", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin);
  const smtp = data.expected.find(
      (r) => r.email === "invitations-ui-smtp@example.com",
    )!,
    logged = data.expected.find(
      (r) => r.latest_delivery.channel === "backend_log",
    )!;
  await expect(rowFor(page, logged.email)).toContainText("后台恢复日志");
  await expect(rowFor(page, smtp.email)).toContainText("SMTP");
  await expect(rowFor(page, smtp.email)).toContainText("投递失败");
  await ipc("cooldown", { id: smtp.id });
  await ipc("smtp-success");
  await rowFor(page, smtp.email)
    .getByRole("button", { name: "重试投递", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "重试投递？", exact: true }),
  ).toContainText("可能造成重复投递");
  const retry = await post(
    page,
    "/retry",
    () => button(page, "确认重试投递").click(),
    202,
  );
  expect(
    typeof retry.job_id === "string" &&
      retry.job_id !== smtp.latest_delivery.job_id,
  ).toBe(true);
  const expected = (await ipc("wait", { id: smtp.id, phase: "sent" })).items;
  const observation = await read(page, () => button(page, "刷新").click());
  await matches(page, observation.value, expected);
  expect(expected.find((r) => r.id === smtp.id)!.latest_delivery.channel).toBe(
    "smtp",
  );
  expect(
    expected.find((r) => r.id === logged.id)!.latest_delivery.channel,
  ).toBe("backend_log");
  expect((await ipc("stable", { id: smtp.id })).stable).toBe(true);
  await expect(rowFor(page, smtp.email)).toContainText("已完成投递");
  result({ channels: true, retried: true, new_job: true });
});

test("[outcome] actual accepted response loss, replay, abandon, read failure and version competition", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin);
  await ipc("arm-drop");
  let dialog = await openCreate(page, "invitations-ui-lost@example.com");
  await dialog.getByRole("button", { name: "发送邀请", exact: true }).click();
  await lostReceipt(dialog);
  await dialog
    .getByRole("button", { name: "检查当前状态", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "邀请用户", exact: true });
  await expect(
    dialog.getByRole("button", { name: "重试原请求", exact: true }),
  ).toBeEnabled();
  const accepted = await post(
    page,
    "/system/invitations",
    () =>
      dialog.getByRole("button", { name: "重试原请求", exact: true }).click(),
    201,
  );
  await expect(dialog).toHaveCount(0);
  expect((await ipc("replay-facts")).single).toBe(true);
  await expect(rowFor(page, "invitations-ui-lost@example.com")).toBeVisible();
  await ipc("arm-drop");
  dialog = await openCreate(page, "invitations-ui-abandoned@example.com");
  await dialog.getByRole("button", { name: "发送邀请", exact: true }).click();
  await lostReceipt(dialog);
  await dialog
    .getByRole("button", { name: "放弃本次操作", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "放弃本次操作？", exact: true }),
  ).toContainText("此前提交仍可能生效");
  await button(page, "放弃并清空").click();
  await expect(inviteButton(page)).toBeDisabled();
  await read(page, () => button(page, "刷新").click());
  await expect(inviteButton(page)).toBeEnabled();
  await ipc("arm-get-fail");
  const responses: { method: string; status: number }[] = [];
  const recordResponse = (response: Response) => {
    if (new URL(response.url()).pathname === "/api/v1/system/invitations")
      responses.push({
        method: response.request().method(),
        status: response.status(),
      });
  };
  page.on("response", recordResponse);
  const failedRead = page.waitForResponse(
    (response) =>
      response.request().method() === "GET" &&
      new URL(response.url()).pathname === "/api/v1/system/invitations",
  );
  try {
    await create(page, "invitations-ui-confirmed@example.com");
    const status = (await failedRead).status(),
      facts = await ipc("read-failure-facts");
    try {
      await expect(
        page.getByRole("heading", {
          name: "操作已确认，列表读取失败",
          exact: true,
        }),
      ).toBeVisible();
    } finally {
      console.log(
        "owned confirmed-read observation",
        JSON.stringify({
          status,
          failures: facts.failures,
          armed: facts.armed,
          responses,
          confirmed_failure: await page
            .getByRole("heading", {
              name: "操作已确认，列表读取失败",
              exact: true,
            })
            .isVisible(),
          ordinary_failure: await page
            .getByRole("heading", { name: "邀请列表读取失败", exact: true })
            .isVisible(),
          heading: await fixedHeadingFact(page, "操作已确认，列表读取失败"),
          accepted_message: await page
            .locator(".write-message")
            .filter({ hasText: "邀请请求已接受" })
            .isVisible(),
          dialogs: await page.getByRole("dialog").count(),
          read_retry: await button(page, "重新读取列表").isVisible(),
          busy: await button(page, "刷新").isDisabled(),
        }),
      );
    }
    expect(status).toBe(503);
    expect(facts.failures === 1 && facts.armed === false).toBe(true);
  } finally {
    page.off("response", recordResponse);
  }
  await expect(
    page.getByRole("button", { name: "重试原请求", exact: true }),
  ).toHaveCount(0);
  await read(page, () => button(page, "重新读取列表").click());
  const target = data.expected[0]!;
  expect(
    target.latest_delivery.phase === "sent" &&
      target.latest_delivery.channel === "backend_log" &&
      target.latest_delivery.attempt_result === "sent",
  ).toBe(true);
  const writes: boolean[] = [];
  const recordWrite = (request: BrowserRequest) => {
    const path = new URL(request.url()).pathname;
    if (
      request.method() !== "POST" ||
      !path.startsWith("/api/v1/system/mail-jobs/")
    )
      return;
    let original = false;
    try {
      const body = request.postDataJSON();
      original =
        path ===
          `/api/v1/system/mail-jobs/${target.latest_delivery.job_id}/retry` &&
        Object.keys(body).length === 1 &&
        body.version === target.latest_delivery.version;
    } catch {}
    writes.push(original);
  };
  page.on("request", recordWrite);
  try {
    await rowFor(page, target.email)
      .getByRole("button", { name: "重试投递", exact: true })
      .click();
    const retryDialog = page.getByRole("dialog", {
      name: "重试投递？",
      exact: true,
    });
    await expect(retryDialog).toContainText("可能造成重复投递");
    expect(writes.length).toBe(0);
    const competing = await ipc("compete", { id: target.id });
    expect(
      competing.advanced === true &&
        competing.same_invitation === true &&
        competing.joined === true,
    ).toBe(true);
    expect(writes.length).toBe(0);
    const conflict = await post(
      page,
      "/retry",
      () =>
        retryDialog
          .getByRole("button", { name: "确认重试投递", exact: true })
          .click(),
      409,
    );
    expect(conflict.code).toBe("VERSION_CONFLICT");
    await expect(retryDialog).toContainText("版本已变化");
    expect(writes).toEqual([true]);
    await expect(
      retryDialog.getByRole("button", { name: "重试原请求", exact: true }),
    ).toHaveCount(0);
    await retryDialog
      .getByRole("button", { name: "取消", exact: true })
      .click();
    const fresh = await read(page, () => button(page, "刷新").click());
    const latest = fresh.value.items.find((row) => row.id === target.id)!;
    expect(
      latest.email === target.email &&
        latest.latest_delivery.job_id !== target.latest_delivery.job_id,
    ).toBe(true);
    await rowFor(page, target.email)
      .getByRole("button", { name: "重试投递", exact: true })
      .click();
    await expect(retryDialog).toContainText("可能造成重复投递");
    await expect(
      retryDialog.getByRole("button", { name: "确认重试投递", exact: true }),
    ).toBeEnabled();
    expect(writes).toEqual([true]);
    await retryDialog
      .getByRole("button", { name: "取消", exact: true })
      .click();
    expect(writes).toEqual([true]);
  } finally {
    page.off("request", recordWrite);
  }
  expect(typeof accepted.id === "string").toBe(true);
  result({
    replayed: true,
    abandoned: true,
    confirmed_read_failure: true,
    conflict: true,
  });
});

test("[read] formal 26-intent pages and canonical current projection", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin, "/login", "首页");
  const first = await read(page, () => page.goto("/system/invitations"));
  await matches(page, first.value, data.expected.slice(0, 25));
  expect([...first.url.searchParams]).toEqual([["limit", "25"]]);
  const next = await read(page, () => button(page, "下一页").click());
  expect(next.url.searchParams.get("cursor") === first.value.next_cursor).toBe(
    true,
  );
  await matches(page, next.value, data.expected.slice(25));
  const previous = await read(page, () => button(page, "上一页").click());
  expect(previous.url.searchParams.get("cursor")).toBeNull();
  await matches(page, previous.value, data.expected.slice(0, 25));
  const refresh = await read(page, () => button(page, "刷新").click());
  await matches(page, refresh.value, data.expected.slice(0, 25));
  const invalid = await page.request.get(
    "/api/v1/system/invitations?limit=25&cursor=invalid",
  );
  expect(invalid.status()).toBe(400);
  const invalidBody = await invalid.json();
  expect(
    invalidBody.code === "CURSOR_INVALID" &&
      !Object.hasOwn(invalidBody, "items"),
  ).toBe(true);
  await ipc("expire-all");
  await read(page, () => button(page, "刷新").click());
  await expect(page.getByText("暂无待注册邀请", { exact: true })).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        localStorage.length === 0 &&
        sessionStorage.length === 0 &&
        location.search === "" &&
        location.hash === "",
    ),
  ).toBe(true);
  result({
    canonical: true,
    pages: 4,
    rows: 26,
    expired: true,
    bad_cursor: true,
  });
});

test("[authority] real 403 and 401 retire drafts and same-account/new-session or explicit account changes retire originals", async ({
  page,
  context,
}) => {
  const data = material();
  let invitationRequests = 0;
  page.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/api/v1/system/invitations"))
      ++invitationRequests;
  });
  await login(page, data.member, "/system/invitations", "无权访问系统设置");
  expect(invitationRequests).toBe(0);
  await expect(
    page.getByRole("link", { name: "系统设置", exact: true }),
  ).toHaveCount(0);
  await page.goto("/login?switch=1&return=/system/invitations");
  await button(page, "退出当前账号后登录").click();
  await fillLogin(page, data.admin);
  await expect(button(page, "刷新")).toBeEnabled();
  const identity = await currentIdentity(page);
  expect(
    identity.status === 200 && identity.user_id === data.admin.user_id,
  ).toBe(true);
  await openCreate(page, "invitations-ui-denied@example.com");
  await ipc("demote", {
    user_id: identity.user_id,
    session_id: identity.session_id,
  });
  const denied = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/system/invitations" &&
      r.request().method() === "POST",
  );
  await button(page, "发送邀请").click();
  const response = await denied;
  expect(response.status()).toBe(403);
  await knownProblem(page, response, "FORBIDDEN");
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await ipc("promote", {
    user_id: identity.user_id,
    session_id: identity.session_id,
  });
  await button(page, "重新检查权限").click();
  await expect(button(page, "刷新")).toBeEnabled();
  await ipc("arm-drop");
  let draft = await openCreate(page, "invitations-ui-old-session@example.com");
  await draft.getByRole("button", { name: "发送邀请", exact: true }).click();
  await lostReceipt(draft);
  const other = await context.newPage();
  await other.goto("/login?switch=1&return=/system/invitations");
  await button(other, "退出当前账号后登录").click();
  await fillLogin(other, data.admin);
  await expect(button(other, "刷新")).toBeEnabled();
  const fresh = await currentIdentity(other);
  expect(
    fresh.user_id === identity.user_id &&
      fresh.session_id !== identity.session_id,
  ).toBe(true);
  await other.close();
  await page.bringToFront();
  await page.evaluate(() =>
    window.dispatchEvent(new PageTransitionEvent("pageshow")),
  );
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "重试原请求", exact: true }),
  ).toHaveCount(0);
  await expect(button(page, "刷新")).toBeEnabled();
  draft = await openCreate(page, "invitations-ui-revoked@example.com");
  await ipc("revoke-session", {
    user_id: fresh.user_id,
    session_id: fresh.session_id,
  });
  const revoked = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/system/invitations" &&
      r.request().method() === "POST",
  );
  await draft.getByRole("button", { name: "发送邀请", exact: true }).click();
  const gone = await revoked;
  expect(gone.status()).toBe(401);
  await knownProblem(page, gone, "SESSION_REVOKED");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await button(page, "检查当前会话").click();
  await expect(page.locator("#login-email")).toBeEditable();
  await fillLogin(page, data.member);
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  await page.goto("/system/invitations");
  await expect(
    page.getByRole("heading", { name: "无权访问系统设置", exact: true }),
  ).toBeVisible();
  result({ denied: true, forbidden: true, revoked: true, switched: true });
});

async function layout(page: Page, stacked: boolean) {
  const faults = await page
    .locator(".invitation-list")
    .evaluate((region, stacked) => {
      const rect = region.getBoundingClientRect(),
        rows = [...region.querySelectorAll("tbody tr")];
      const faults: string[] = [];
      if (
        document.documentElement.scrollWidth > innerWidth + 1 ||
        region.scrollWidth > region.clientWidth + 1
      )
        faults.push("overflow");
      for (const row of rows) {
        const cells = [...row.querySelectorAll("td")];
        if (cells.length !== 5) faults.push("fields");
        for (const cell of cells) {
          const r = cell.getBoundingClientRect();
          if (
            r.left < rect.left - 1 ||
            r.right > rect.right + 1 ||
            r.width <= 0 ||
            getComputedStyle(cell).whiteSpace !== "normal"
          )
            faults.push("cell");
          if (stacked && getComputedStyle(cell, "::before").content === "none")
            faults.push("label");
        }
      }
      return faults;
    }, stacked);
  expect(faults).toEqual([]);
}
test("[navigation] dirty navigation, same Session check, dialogs, drawer and eight production layouts", async ({
  page,
}) => {
  const data = material();
  await login(page, data.admin);
  await expect(page).toHaveURL(/\/system\/invitations$/);
  await page
    .getByRole("link", { name: "系统设置", exact: true })
    .first()
    .click();
  await expect(
    page.getByRole("heading", { name: "用户", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "系统设置", exact: true })
    .getByRole("link", { name: "待注册邀请", exact: true })
    .click();
  await expect(button(page, "刷新")).toBeEnabled();
  const trigger = inviteButton(page);
  await trigger.focus();
  await page.keyboard.press("Enter");
  let dialog = page.getByRole("dialog", { name: "邀请用户", exact: true });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await page.locator(".ui-overlay").click({ position: { x: 1, y: 1 } });
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  dialog = await openCreate(page, "invitations-ui-draft@example.com");
  const before = await currentIdentity(page);
  const confirmation = page.getByRole("dialog", {
    name: "放弃未保存修改？",
    exact: true,
  });
  let invitationPosts = 0;
  page.on("request", (request) => {
    if (
      request.method() === "POST" &&
      new URL(request.url()).pathname.startsWith("/api/v1/system/")
    )
      ++invitationPosts;
  });
  for (const failFirst of [false, true]) {
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(confirmation).toBeVisible();
    if (failFirst) await ipc("arm-session-fail");
    const session = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/v1/session" &&
        r.request().method() === "GET",
    );
    // A synthetic pageshow enters the production App listener. Only the one
    // explicitly armed Session response is 503; recovery uses the real backend.
    const checking = await page.evaluate(async () => {
      let observer: MutationObserver | undefined, timer: number | undefined;
      try {
        return await new Promise<{
          dialogs: number;
          inert: boolean;
          locked: boolean;
        }>((resolve, reject) => {
          observer = new MutationObserver(() => {
            if (!document.querySelector(".session-check")) return;
            resolve({
              dialogs: [
                ...document.querySelectorAll<HTMLElement>('[role="dialog"]'),
              ].filter(
                (element) =>
                  !element.inert &&
                  element.getAttribute("aria-hidden") !== "true",
              ).length,
              inert: document.getElementById("app")!.inert,
              locked: document.body.style.overflow === "hidden",
            });
          });
          observer.observe(document.body, { childList: true, subtree: true });
          timer = window.setTimeout(
            () => reject(new Error("production checking DOM was not observed")),
            5_000,
          );
          window.dispatchEvent(new PageTransitionEvent("pageshow"));
        });
      } finally {
        observer?.disconnect();
        window.clearTimeout(timer);
      }
    });
    expect(checking).toEqual({ dialogs: 0, inert: false, locked: false });
    expect((await session).status()).toBe(failFirst ? 503 : 200);
    if (failFirst) {
      await expect(
        page.getByRole("heading", { name: "会话尚未确认", exact: true }),
      ).toBeVisible();
      console.log(
        "owned failed-Session heading",
        JSON.stringify(await fixedHeadingFact(page, "会话尚未确认")),
      );
      await expect(page.getByRole("dialog")).toHaveCount(0);
      const retry = page.waitForResponse(
        (r) =>
          new URL(r.url()).pathname === "/api/v1/session" &&
          r.request().method() === "GET",
      );
      await button(page, "检查当前会话").click();
      expect((await retry).status()).toBe(200);
      const facts = await ipc("session-failure-facts");
      expect(facts.failures === 1 && facts.armed === false).toBe(true);
    }
    await expect(confirmation).toBeVisible();
    expect(
      await confirmation.evaluate((element) => {
        const overlays = document.querySelectorAll(".ui-overlay");
        return (
          element.closest(".ui-overlay") === overlays[overlays.length - 1] &&
          element instanceof HTMLElement &&
          !element.inert &&
          !element.hasAttribute("aria-hidden") &&
          element.contains(document.activeElement)
        );
      }),
    ).toBe(true);
    await expect(
      page
        .locator(".system-invitations .invitation-actions")
        .first()
        .locator("button")
        .nth(1),
    ).toBeEnabled();
    const after = await currentIdentity(page);
    expect(
      before.session_id === after.session_id &&
        before.user_id === after.user_id,
    ).toBe(true);
    expect(invitationPosts).toBe(0);
    await confirmation
      .getByRole("button", { name: "继续编辑", exact: true })
      .click();
    dialog = page.getByRole("dialog", { name: "邀请用户", exact: true });
    await expect(dialog.getByLabel("邮箱", { exact: false })).toHaveValue(
      "invitations-ui-draft@example.com",
    );
    // Do not focus anything here: the accepted shared layer must recover into
    // the remaining business modal after its original trigger was unmounted.
    expect(
      await dialog.evaluate((element) =>
        element.contains(document.activeElement),
      ),
    ).toBe(true);
    for (const key of [
      "Tab",
      "Tab",
      "Tab",
      "Tab",
      "Shift+Tab",
      "Shift+Tab",
      "Shift+Tab",
      "Shift+Tab",
    ]) {
      await page.keyboard.press(key);
      expect(
        await dialog.evaluate((element) =>
          element.contains(document.activeElement),
        ),
      ).toBe(true);
    }
  }
  const cancel = dialog.getByRole("button", { name: "取消", exact: true });
  await cancel.click();
  await expect(confirmation).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(cancel).toBeFocused();
  await cancel.click();
  await button(page, "放弃修改").click();
  await expect(dialog).toHaveCount(0);
  const target = data.expected[0]!;
  await ipc("cooldown", { id: target.id });
  await ipc("arm-drop", { id: target.id });
  await rowFor(page, target.email)
    .getByRole("button", { name: "重发邀请", exact: true })
    .click();
  await lostReceipt(
    page.getByRole("heading", { name: "请求结果未确认", exact: true }),
  );
  let checks = 0;
  page.on("request", (r) => {
    if (new URL(r.url()).pathname === "/api/v1/session") ++checks;
  });
  await page
    .getByRole("navigation", { name: "系统设置", exact: true })
    .getByRole("link", { name: "用户", exact: true })
    .click();
  await expect(confirmation).toBeVisible();
  expect(checks).toBe(0);
  await button(page, "继续编辑").click();
  await page
    .locator(".account-actions")
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  await expect(confirmation).toBeVisible();
  expect(checks).toBe(0);
  await button(page, "继续编辑").click();
  await button(page, "放弃本次操作").click();
  await button(page, "放弃并清空").click();
  await read(page, () => button(page, "刷新").click());
  await rowFor(page, target.email)
    .getByRole("button", { name: "撤销邀请", exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    rowFor(page, target.email).getByRole("button", {
      name: "撤销邀请",
      exact: true,
    }),
  ).toBeFocused();
  let layouts = 0;
  for (const theme of ["light", "dark"] as const)
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      await layout(page, width <= 760);
      if (width === 390) {
        const toggle = button(page, "系统设置栏目");
        await toggle.click();
        const drawer = page.getByRole("dialog", {
          name: "系统设置栏目",
          exact: true,
        });
        await expect(drawer.getByRole("link")).toHaveCount(6);
        for (let n = 0; n < 6; n++) {
          await page.keyboard.press("Tab");
          expect(
            await drawer.evaluate((e) => e.contains(document.activeElement)),
          ).toBe(true);
        }
        await page.keyboard.press("Escape");
        await expect(drawer).toHaveCount(0);
        await expect(toggle).toBeFocused();
        await toggle.click();
        await page.locator(".ui-overlay").click({ position: { x: 1, y: 1 } });
        await expect(drawer).toHaveCount(0);
        await expect(toggle).toBeFocused();
        await toggle.click();
        await drawer
          .getByRole("link", { name: "待注册邀请", exact: true })
          .click();
        await expect(drawer).toHaveCount(0);
      }
      // The accessible drawer closes before its leave transition removes the
      // overlay. Capture the actual page only after the overlay is detached.
      await expect(page.locator(".ui-overlay")).toHaveCount(0);
      if (process.env.AGENTEAM_AUTH_WEB_IMAGES)
        await page.screenshot({
          path: join(
            process.env.AGENTEAM_AUTH_WEB_IMAGES,
            `system-invitations-${theme}-${width}.png`,
          ),
        });
      ++layouts;
    }
  await page
    .locator(".account-actions")
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  await expect(page.locator("#login-email")).toBeEditable();
  result({
    navigation: true,
    dirty: true,
    checking: true,
    dialogs: true,
    layouts,
  });
});
