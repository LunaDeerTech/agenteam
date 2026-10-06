import { test, expect, type Page, type Locator } from "@playwright/test";
import { readFileSync, writeFileSync, renameSync, existsSync } from "node:fs";
import { join } from "node:path";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
type Credential = { email: string; password: string; user_id: string };
type Material = {
  admin: Credential;
  member: Credential;
  invitation_email: string;
  invitation_link: string;
  consumed_link: string;
  revoked_link: string;
  new_password: string;
};
const material = (): Material =>
  JSON.parse(readFileSync(join(directory, "entry-material.json"), "utf8"));
function result(value: Record<string, unknown>) {
  writeFileSync(
    join(directory, "entry-result.json"),
    JSON.stringify({ ...value, completed: true }),
    { mode: 0o600 },
  );
}
async function privateFill(input: Locator, value: string) {
  try {
    await input.fill(value);
  } catch {
    throw new Error("private entry field failed");
  }
}
async function privateLink(page: Page, value: string) {
  try {
    await page.goto(value);
  } catch {
    throw new Error("private entry navigation failed");
  }
}
async function identity(page: Page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (response.status !== 200)
      return { status: response.status, user_id: "", session_id: "" };
    const value = await response.json();
    return {
      status: 200,
      user_id: value.user.id as string,
      session_id: value.session.id as string,
    };
  });
}
async function enter(page: Page, credential: Credential) {
  await page.goto("/login");
  await expect(page.locator("#login-email")).toBeEditable();
  await page.locator("#login-email").fill(credential.email);
  await privateFill(page.locator("#login-password"), credential.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  const current = await identity(page);
  expect(current.status === 200 && current.user_id === credential.user_id).toBe(
    true,
  );
  return current;
}
async function responseFor(
  page: Page,
  path: string,
  status: number,
  action: () => Promise<unknown>,
) {
  const pending = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === path &&
      response.request().method() === "POST",
  );
  await action();
  const response = await pending;
  expect(response.status()).toBe(status);
  return response;
}
type JSONObservationResult =
  { ok: true; status: number; value: unknown } | { ok: false };
type JSONObservationWindow = Window & {
  __entryJSONObservation?: {
    result: Promise<JSONObservationResult>;
    calls: number;
    finish(): Promise<void>;
  };
};
function installJSONObservation(path: string) {
  const scope = window as JSONObservationWindow;
  if (scope.__entryJSONObservation)
    throw new Error("entry JSON observation already installed");
  const original = scope.fetch;
  let settle!: (value: JSONObservationResult) => void;
  let reading: Promise<void> | undefined;
  const observation = {
    result: new Promise<JSONObservationResult>((resolve) => {
      settle = resolve;
    }),
    calls: 0,
    async finish() {
      if (scope.fetch === wrapped) scope.fetch = original;
      try {
        if (reading) await reading;
        else settle({ ok: false });
      } finally {
        delete scope.__entryJSONObservation;
      }
    },
  };
  const wrapped: typeof fetch = function (this: unknown, ...args) {
    // Return the very same native promise/Response, without waiting for the
    // observer. Cloning tees the body: this instrumentation is only for the
    // request's small 202 DTO, never proof of an unobserved stream/owner tail.
    const pending = Reflect.apply(original, this, args) as Promise<Response>;
    let matches = false;
    try {
      const [input, init] = args;
      const url = new URL(
        input instanceof Request ? input.url : String(input),
        location.href,
      );
      const method =
        init?.method ?? (input instanceof Request ? input.method : "GET");
      matches =
        url.origin === location.origin &&
        url.pathname === path &&
        method.toUpperCase() === "POST";
    } catch {
      // Leave invalid/unrelated native fetch calls unchanged.
    }
    if (matches && ++observation.calls === 1) {
      reading = pending
        .then(async (response): Promise<JSONObservationResult> => {
          if (response.status !== 202)
            return { ok: true, status: response.status, value: null };
          const reader = response.clone().body?.getReader();
          if (!reader) throw new Error("entry JSON observation has no body");
          const decoder = new TextDecoder("utf-8", { fatal: true });
          let text = "",
            bytes = 0;
          try {
            for (;;) {
              const { done, value } = await reader.read();
              if (done) break;
              bytes += value.byteLength;
              // At most the production client's existing JSON byte limit.
              if (bytes > 600_000)
                throw new Error("entry JSON observation exceeds limit");
              text += decoder.decode(value, { stream: true });
            }
            text += decoder.decode();
            return {
              ok: true,
              status: response.status,
              value: JSON.parse(text) as unknown,
            };
          } finally {
            try {
              await reader.cancel();
            } finally {
              reader.releaseLock();
            }
          }
        })
        .then(settle, () => settle({ ok: false }));
    }
    return pending;
  };
  scope.__entryJSONObservation = observation;
  scope.fetch = wrapped;
}
async function jsonResponseFor(
  page: Page,
  path: string,
  status: number,
  action: () => Promise<unknown>,
) {
  await page.evaluate(installJSONObservation, path);
  try {
    const response = page.waitForResponse(
      (response) =>
        new URL(response.url()).origin === new URL(page.url()).origin &&
        new URL(response.url()).pathname === path &&
        response.request().method() === "POST",
    );
    const [, actual, observed] = await Promise.all([
      action(),
      response,
      page.evaluate(async () => {
        const observation = (window as JSONObservationWindow)
          .__entryJSONObservation!;
        return { result: await observation.result };
      }),
    ]);
    expect(actual.status()).toBe(status);
    expect(
      await page.evaluate(
        () => (window as JSONObservationWindow).__entryJSONObservation!.calls,
      ),
    ).toBe(1);
    expect(observed.result.ok).toBe(true);
    if (!observed.result.ok) throw new Error("entry JSON observation failed");
    expect(observed.result.status).toBe(status);
    const key = actual.request().headers()["idempotency-key"];
    expect(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
        key ?? "",
      ),
    ).toBe(true);
    resetRequestKey = key!;
    const value = observed.result.value;
    expect(
      value !== null && typeof value === "object" && !Array.isArray(value),
    ).toBe(true);
    return value as Record<string, unknown>;
  } finally {
    // Includes failed clicks/reads: restore fetch and join the clone's actual
    // read/cancel/release before leaving the page or destroying its context.
    await page.evaluate(async () => {
      await (window as JSONObservationWindow).__entryJSONObservation?.finish();
    });
  }
}
async function noCapability(page: Page, value: string) {
  const token = new URL(value).hash.slice(1);
  const safe = await page.evaluate((token) => {
    const states = [
      location.href,
      JSON.stringify(history.state),
      document.documentElement.outerHTML,
      JSON.stringify({ ...localStorage }),
      JSON.stringify({ ...sessionStorage }),
    ];
    return (
      states.every((value) => !value.includes(token)) && location.hash === ""
    );
  }, token);
  expect(safe).toBe(true);
}
async function renderedTheme(page: Page, theme: "light" | "dark") {
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
  expect(
    await page.evaluate(() => ({
      scheme: getComputedStyle(document.documentElement).colorScheme,
      background: getComputedStyle(document.body).backgroundColor,
    })),
  ).toEqual({
    scheme: theme,
    background: theme === "light" ? "rgb(244, 244, 245)" : "rgb(16, 17, 19)",
  });
}
let sequence = 0;
let resetRequestKey = "";
async function resetLink(email: string) {
  if (!resetRequestKey) throw new Error("owned reset request key missing");
  const current = ++sequence;
  // One deadline includes recovery's 10s tick, PG observation and restricted
  // log/IPC transfer. It never extends the original 45s browser case budget.
  const deadline = Date.now() + 20_000;
  const path = join(directory, "entry-ipc.json");
  writeFileSync(
    path + ".tmp",
    JSON.stringify({
      sequence: current,
      email,
      kind: "reset-link",
      command_key: resetRequestKey,
      deadline_ms: deadline,
    }),
    { mode: 0o600 },
  );
  renameSync(path + ".tmp", path);
  resetRequestKey = "";
  const ack = join(directory, `entry-link-${current}.json`);
  await expect
    .poll(() => existsSync(ack), {
      timeout: Math.max(1, deadline - Date.now()),
      intervals: [20, 50, 100],
    })
    .toBe(true);
  return (JSON.parse(readFileSync(ack, "utf8")) as { url: string }).url;
}
async function requestRecovery(page: Page, email: string) {
  await page.goto("/forgot-password");
  await expect(page.locator("#forgot-email")).toBeEditable();
  await page.locator("#forgot-email").fill(email);
  const value = await jsonResponseFor(
    page,
    "/api/v1/password-resets/request",
    202,
    () =>
      page.getByRole("button", { name: "申请恢复方式", exact: true }).click(),
  );
  expect(Object.keys(value).sort()).toEqual(["accepted", "delivery_channel"]);
  expect(
    value.accepted === true && value.delivery_channel === "backend_log",
  ).toBe(true);
  await expect(page.getByRole("alert")).toContainText("如果该邮箱可用");
  await expect(
    page.getByText("请有权限访问受限后端日志的人协助转交恢复方式。"),
  ).toBeVisible();
  return value;
}
async function fillInvitation(
  page: Page,
  values: Material,
  username = "entry-invited",
) {
  await expect(page.locator("#invitation-username")).toBeEditable();
  await expect(page.getByLabel("邀请邮箱（只读）")).toHaveText(
    values.invitation_email,
  );
  expect(await page.locator('input[name="email"]').count()).toBe(0);
  await page.locator("#invitation-username").fill(username);
  await page.locator("#invitation-display-name").fill("  Invited 中文  ");
  await privateFill(page.locator("#invitation-password"), values.new_password);
  await privateFill(
    page.locator("#invitation-confirmation"),
    values.new_password,
  );
}
async function reset(page: Page, value: string, password: string) {
  await privateLink(page, value);
  await expect(page.locator("#reset-password")).toBeEditable();
  await noCapability(page, value);
  expect(
    await page
      .getByText("public-entry-member@example.com", { exact: true })
      .count(),
  ).toBe(0);
  await privateFill(page.locator("#reset-password"), password);
  await privateFill(page.locator("#reset-confirmation"), password);
  await responseFor(page, "/api/v1/password-resets/complete", 204, () =>
    page.getByRole("button", { name: "重置密码", exact: true }).click(),
  );
  await expect(page.getByRole("alert")).toContainText("密码已重置");
  expect(await page.locator('input[type="password"]').count()).toBe(0);
}

test("[invitation] formal invitation and explicit login", async ({ page }) => {
  const values = material();
  await privateLink(page, values.invitation_link);
  await noCapability(page, values.invitation_link);
  await fillInvitation(page, values, "entry-existing");
  await responseFor(page, "/api/v1/invitations/redeem", 400, () =>
    page.getByRole("button", { name: "创建账号", exact: true }).click(),
  );
  await expect(page.locator("#invitation-username")).toHaveAttribute(
    "aria-invalid",
    "true",
  );
  expect(
    await page
      .locator("#invitation-password")
      .evaluate(
        (input, expected) => (input as HTMLInputElement).value === expected,
        values.new_password,
      ),
  ).toBe(true);
  await page.locator("#invitation-username").fill("entry-invited");
  await responseFor(page, "/api/v1/invitations/redeem", 201, () =>
    page.locator("#invitation-confirmation").press("Enter"),
  );
  await expect(page.getByRole("alert")).toContainText("账号已创建");
  expect(await page.locator('input[type="password"]').count()).toBe(0);
  expect((await identity(page)).status).toBe(401);
  await page.getByRole("link", { name: "返回登录", exact: true }).click();
  await expect(page.locator("#login-email")).toBeEditable();
  await page.locator("#login-email").fill(values.invitation_email);
  await privateFill(page.locator("#login-password"), values.new_password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  const current = await identity(page);
  expect(current.status).toBe(200);
  result({ user_id: current.user_id, session_id: current.session_id });
});

test("[reset] recovery revokes two real Sessions and accepts only the new password", async ({
  page,
  browser,
}) => {
  const values = material();
  const other = await browser.newContext({
    baseURL: process.env.AGENTEAM_AUTH_WEB_ORIGIN,
  });
  const otherPage = await other.newPage();
  try {
    const first = await enter(page, values.member),
      second = await enter(otherPage, values.member);
    await requestRecovery(page, values.member.email);
    const link = await resetLink(values.member.email);
    await reset(page, link, values.new_password);
    expect((await identity(page)).status).toBe(401);
    expect((await identity(otherPage)).status).toBe(401);
    await page.getByRole("link", { name: "返回登录", exact: true }).click();
    await expect(page.locator("#login-email")).toBeEditable();
    await page.locator("#login-email").fill(values.member.email);
    await privateFill(page.locator("#login-password"), values.member.password);
    await responseFor(page, "/api/v1/sessions/login", 401, () =>
      page.getByRole("button", { name: "登录", exact: true }).click(),
    );
    await expect(page.getByRole("alert")).toContainText("邮箱或密码不正确");
    await privateFill(page.locator("#login-password"), values.new_password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "首页", exact: true }),
    ).toBeVisible();
    const current = await identity(page);
    result({
      user_id: current.user_id,
      session_id: current.session_id,
      old_session_ids: [first.session_id, second.session_id],
    });
  } finally {
    await other.close();
  }
});

test("[identity] real other identity and explicit account switching", async ({
  page,
}) => {
  const values = material();
  let logouts = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/sessions/logout")
      logouts++;
  });
  await enter(page, values.admin);
  await privateLink(page, values.invitation_link);
  await expect(page.getByRole("alert")).toContainText("当前账号不能兑换此邀请");
  await noCapability(page, values.invitation_link);
  expect((await identity(page)).user_id).toBe(values.admin.user_id);
  expect(logouts).toBe(0);
  await page.getByRole("link", { name: "切换账号", exact: true }).click();
  await expect(page.getByText("当前账号：", { exact: false })).toBeVisible();
  expect(logouts).toBe(0);
  await page
    .getByRole("button", { name: "退出当前账号后登录", exact: true })
    .click();
  await expect(page.locator("#login-email")).toBeEditable();
  expect(logouts).toBe(1);
  await privateLink(page, values.invitation_link);
  await fillInvitation(page, values);
  await responseFor(page, "/api/v1/invitations/redeem", 201, () =>
    page.getByRole("button", { name: "创建账号", exact: true }).click(),
  );
  await expect(page.getByRole("alert")).toContainText("账号已创建");
  const currentA = await enter(page, values.admin);
  await requestRecovery(page, values.member.email);
  const link = await resetLink(values.member.email);
  await reset(page, link, values.new_password);
  const after = await identity(page);
  expect(after).toEqual(currentA);
  await page.getByRole("link", { name: "返回登录", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "退出当前账号后登录", exact: true }),
  ).toBeVisible();
  expect(logouts).toBe(1);
  // Build real History entries with product links, then exercise settings dirty
  // confirmation when the browser returns across the public domain boundary.
  await page.goto("/forgot-password");
  await expect(page.locator("#forgot-email")).toBeEditable();
  await page.getByRole("link", { name: "返回登录", exact: true }).click();
  await page.getByRole("link", { name: "返回当前账号", exact: true }).click();
  await page.locator('a[href="/settings/profile"]').click();
  await expect(page.locator("#profile-display-name")).toBeEditable();
  await page.locator("#profile-display-name").fill("Unsaved entry draft");
  await page.evaluate(() => history.go(-3));
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "继续编辑", exact: true }).click();
  await expect(page.locator("#profile-display-name")).toHaveValue(
    "Unsaved entry draft",
  );
  await page.evaluate(() => history.go(-3));
  await page.getByRole("button", { name: "放弃修改", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "找回密码", exact: true }),
  ).toBeVisible();
  result({
    user_id: after.user_id,
    session_id: after.session_id,
    identity_preserved: true,
  });
});

test("[privacy] generic response and actual production layouts", async ({
  page,
}) => {
  const values = material();
  const known = await requestRecovery(page, values.member.email);
  const link = await resetLink(values.member.email);
  await page.locator("#forgot-email").fill("public-entry-unknown@example.com");
  const unknown = await jsonResponseFor(
    page,
    "/api/v1/password-resets/request",
    202,
    () => page.getByRole("button", { name: "再次申请", exact: true }).click(),
  );
  expect(unknown).toEqual(known);
  for (const unavailable of [values.consumed_link, values.revoked_link]) {
    await privateLink(page, unavailable);
    await expect(page.getByRole("alert")).toContainText("邀请链接不可用");
    await noCapability(page, unavailable);
  }
  let layouts = 0;
  const publicPages = [
    [values.invitation_link, "接受邀请"],
    [process.env.AGENTEAM_AUTH_WEB_ORIGIN + "/forgot-password", "找回密码"],
    [link, "重置密码"],
  ];
  for (const [url, title] of publicPages) {
    await privateLink(page, url!);
    await expect(
      page.getByRole("heading", { name: title!, exact: true }),
    ).toBeVisible();
    if (title === "接受邀请")
      await expect(page.locator("#invitation-username")).toBeEditable();
    if (title === "重置密码")
      await expect(page.locator("#reset-password")).toBeEditable();
    for (const colorScheme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      await renderedTheme(page, colorScheme);
      for (const width of [1440, 1024, 834, 390]) {
        await page.setViewportSize({ width, height: 1000 });
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        ).toBe(true);
        layouts++;
      }
    }
    const action = page.locator(".ui-button").first();
    await page.emulateMedia({ reducedMotion: "no-preference" });
    expect(
      await action.evaluate((element) =>
        getComputedStyle(element)
          .transitionDuration.split(",")
          .some((value) => parseFloat(value) > 0),
      ),
    ).toBe(true);
    await page.emulateMedia({ reducedMotion: "reduce" });
    expect(
      await action.evaluate((element) =>
        getComputedStyle(element)
          .transitionDuration.split(",")
          .every((value) => parseFloat(value) === 0),
      ),
    ).toBe(true);
    expect(
      await page
        .locator(".ui-spinner")
        .first()
        .evaluate((element) => getComputedStyle(element).animationName),
    ).toBe("none");
    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.evaluate(() => {
      document.documentElement.style.zoom = "";
    });
    await page.keyboard.press("Tab");
    expect(
      await page.evaluate(() => document.activeElement !== document.body),
    ).toBe(true);
    if (url!.includes("#")) await noCapability(page, url!);
  }
  // Save preferences through the actual settings page, then let each public
  // page restore the real Session. The opposing OS preference must not win.
  await enter(page, values.admin);
  for (const [label, theme, opposing] of [
    ["浅色", "light", "dark"],
    ["深色", "dark", "light"],
  ] as const) {
    await page.goto("/settings/appearance");
    await page.getByRole("radio", { name: label, exact: true }).check();
    const saved = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/api/v1/me/preferences" &&
        response.request().method() === "PUT",
    );
    await page.getByRole("button", { name: "保存主题", exact: true }).click();
    expect((await saved).status()).toBe(200);
    await expect(page.getByText("主题已保存。", { exact: true })).toBeVisible();
    await page.emulateMedia({ colorScheme: opposing });
    for (const [url, title] of publicPages) {
      await privateLink(page, url!);
      await expect(
        page.getByRole("heading", { name: title!, exact: true }),
      ).toBeVisible();
      await renderedTheme(page, theme);
      if (url!.includes("#")) await noCapability(page, url!);
    }
  }
  await page.goto("/debug");
  await expect(
    page.getByRole("heading", { name: "未找到页面", exact: true }),
  ).toBeVisible();
  expect((await page.request.get("/api/v1/entry-unknown")).status()).toBe(404);
  expect((await page.request.get("/assets/entry-absent.js")).status()).toBe(
    404,
  );
  result({ generic_response: true, layouts });
});
