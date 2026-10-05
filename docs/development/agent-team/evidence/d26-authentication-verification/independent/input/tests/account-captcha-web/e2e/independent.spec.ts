import {
  test,
  expect,
  type Page,
  type Response,
  type Request as PWRequest,
} from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { solvePublicRotation } from "./public-solver";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
type CloneResult = {
  ok: boolean;
  status: number;
  request_id: string;
  bytes: number;
  body?: unknown;
  error?: string;
};
type ProbeWindow = Window & {
  __d26IndependentObservation: {
    arm(stage: string, method: string, path: string): void;
    take(stage: string): Promise<CloneResult>;
  };
};
const requestEvents = new WeakMap<
  PWRequest,
  { finished?: number; failed?: number }
>();
const armedNavigation = new Map<string, number>();
let navigationCount = 0;
const safeObservations: Record<string, unknown>[] = [];

async function installObservation(page: Page) {
  page.on("requestfinished", (request) => {
    requestEvents.set(request, {
      ...requestEvents.get(request),
      finished: performance.now(),
    });
  });
  page.on("requestfailed", (request) => {
    requestEvents.set(request, {
      ...requestEvents.get(request),
      failed: performance.now(),
    });
  });
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame()) navigationCount++;
  });
  armedNavigation.set("bootstrap", 0);
  await page.addInitScript(() => {
    const nativeFetch = window.fetch;
    const records = new Map<
      string,
      { promise: Promise<CloneResult>; resolve(value: CloneResult): void }
    >();
    const armed = new Map<
      string,
      { stage: string; method: string; path: string }
    >();
    const arm = (stage: string, method: string, path: string) => {
      if (
        armed.size >= 2 ||
        records.has(stage) ||
        [...armed.values()].some(
          (value) => value.method === method && value.path === path,
        )
      )
        throw new Error("INDEPENDENT_ARM_ALREADY_ACTIVE");
      let resolve!: (value: CloneResult) => void;
      const promise = new Promise<CloneResult>((done) => {
        resolve = done;
      });
      records.set(stage, { promise, resolve });
      armed.set(stage, { stage, method, path });
    };
    const capture = async (copy: globalThis.Response): Promise<CloneResult> => {
      const source = {
        status: copy.status,
        request_id: copy.headers.get("X-Request-ID") ?? "",
      };
      const reader = copy.body?.getReader();
      if (!reader) return { ok: false, ...source, bytes: 0, error: "no_body" };
      const decoder = new TextDecoder("utf-8", { fatal: true });
      let text = "",
        bytes = 0;
      try {
        for (;;) {
          const chunk = await reader.read();
          if (chunk.done) break;
          bytes += chunk.value.byteLength;
          if (bytes > 600_000)
            return { ok: false, ...source, bytes, error: "limit" };
          text += decoder.decode(chunk.value, { stream: true });
        }
        text += decoder.decode();
        const body: unknown = JSON.parse(text);
        text = "";
        return { ok: true, ...source, bytes, body };
      } catch {
        return { ok: false, ...source, bytes, error: "clone_read" };
      } finally {
        await reader.cancel().catch(() => undefined);
        reader.releaseLock();
      }
    };
    window.fetch = function (this: Window, ...args: Parameters<typeof fetch>) {
      const input = args[0],
        init = args[1];
      const url = new URL(
        input instanceof Request ? input.url : String(input),
        location.href,
      );
      const method =
        init?.method || (input instanceof Request ? input.method : "GET");
      const selected = [...armed.values()].find(
        (value) =>
          url.origin === location.origin &&
          url.pathname === value.path &&
          method.toUpperCase() === value.method,
      );
      if (selected) armed.delete(selected.stage);
      const actual = Reflect.apply(
        nativeFetch,
        this,
        args,
      ) as Promise<globalThis.Response>;
      if (!selected) return actual;
      const record = records.get(selected.stage)!;
      return actual.then(
        (value) => {
          try {
            // Test-only tee: same Response immediately returned; no await of this reader.
            void capture(value.clone()).then(record.resolve);
          } catch {
            record.resolve({
              ok: false,
              status: value.status,
              request_id: value.headers.get("X-Request-ID") ?? "",
              bytes: 0,
              error: "clone",
            });
          }
          return value;
        },
        (error) => {
          record.resolve({
            ok: false,
            status: 0,
            request_id: "",
            bytes: 0,
            error: "fetch",
          });
          throw error;
        },
      );
    };
    Object.defineProperty(window, "__d26IndependentObservation", {
      value: {
        arm,
        async take(stage: string) {
          const record = records.get(stage);
          if (!record) throw new Error("INDEPENDENT_STAGE_MISSING");
          try {
            return await record.promise;
          } finally {
            records.delete(stage);
          }
        },
      },
      configurable: true,
    });
    arm("bootstrap", "GET", "/api/v1/auth/bootstrap");
  });
}
async function armJSON(
  page: Page,
  stage: string,
  path: string,
  method = "POST",
) {
  armedNavigation.set(stage, navigationCount);
  await page.evaluate(
    ({ stage, path, method }) => {
      (window as unknown as ProbeWindow).__d26IndependentObservation.arm(
        stage,
        method,
        path,
      );
    },
    { stage, path, method },
  );
}
async function observedJSON(page: Page, stage: string, actual: Response) {
  // Exactly one original CDP body read is retained, with its outcome separately recorded.
  const cdp = await actual.json().then(
    (body) => ({ ok: true as const, body }),
    (error) => ({
      ok: false as const,
      known:
        String(error).includes("Network.getResponseBody") &&
        String(error).includes(
          "No data found for resource with given identifier",
        ),
    }),
  );
  await actual.finished();
  const event = requestEvents.get(actual.request());
  const failed =
    actual.request().failure() !== null || event?.failed !== undefined;
  const clone = await page.evaluate(
    async (stage) =>
      (window as unknown as ProbeWindow).__d26IndependentObservation.take(
        stage,
      ),
    stage,
  );
  const navigationDelta =
    navigationCount - (armedNavigation.get(stage) ?? navigationCount);
  const sameResponse =
    clone.request_id.length === 36 &&
    clone.request_id === actual.headers()["x-request-id"];
  const safe = {
    stage,
    status: actual.status(),
    cdp: cdp.ok
      ? "ok"
      : cdp.known
        ? "protocol_body_unavailable"
        : "other_error",
    request_finished: event?.finished !== undefined,
    request_failed: failed,
    finished_at_ms: event?.finished ?? null,
    failed_at_ms: event?.failed ?? null,
    navigation_delta: navigationDelta,
    clone_ok: clone.ok,
    clone_bytes: clone.bytes,
    same_response: sameResponse,
  };
  safeObservations.push(safe);
  console.info("independent passive observation", JSON.stringify(safe));
  expect(!failed && event?.finished !== undefined).toBe(true);
  expect(cdp.ok || cdp.known).toBe(true);
  expect(
    clone.ok &&
      clone.status === actual.status() &&
      clone.bytes <= 600_000 &&
      sameResponse,
  ).toBe(true);
  if (!["bootstrap", "login", "session"].includes(stage))
    expect(navigationDelta).toBe(0);
  if (cdp.ok)
    expect(JSON.stringify(cdp.body) === JSON.stringify(clone.body)).toBe(true);
  return clone.body as Record<string, any>;
}

function response(page: Page, path: string, method = "POST") {
  return page.waitForResponse(
    (r) =>
      r.request().method() === method && new URL(r.url()).pathname === path,
  );
}
async function password(page: Page, value: string) {
  try {
    const input = page.locator("#login-password");
    await expect(input).toHaveAccessibleName("密码");
    await input.fill(value);
  } catch {
    throw new Error("private password entry failed");
  }
}
async function publicAngle(page: Page) {
  const images = await page.evaluate(async () => {
    const read = async (selector: string) => {
      const image = document.querySelector<HTMLImageElement>(selector)!;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const ctx = canvas.getContext("2d")!;
      ctx.drawImage(image, 0, 0);
      return {
        rgba: Array.from(
          ctx.getImageData(0, 0, canvas.width, canvas.height).data,
        ),
        width: canvas.width,
        height: canvas.height,
      };
    };
    return {
      master: await read(".gc-rotate-picture img"),
      thumb: await read(".gc-rotate-thumb-block img"),
    };
  });
  expect([
    images.master.width,
    images.master.height,
    images.thumb.width,
    images.thumb.height,
  ]).toEqual([220, 220, 160, 160]);
  return (await page.evaluate(solvePublicRotation, images)).angle;
}
async function keyboardVerify(page: Page, angle: number): Promise<Response> {
  const slider = page.getByRole("slider", { name: "旋转角度", exact: true });
  await expect(slider).toBeFocused();
  await slider.press("Home");
  for (let i = 0; i < angle; ++i) await slider.press("ArrowRight");
  await expect(slider).toHaveValue(String(angle));
  const pending = response(page, "/api/v1/auth/challenges/verify");
  await slider.press("Enter");
  return pending;
}

test("real failed proof focus, fresh same-intent login, and Session-owned logout", async ({
  page,
  context,
}) => {
  await installObservation(page);
  await page.setViewportSize({ width: 390, height: 900 });
  const bootstrap = response(page, "/api/v1/auth/bootstrap", "GET");
  await page.goto("/login");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeFocused();
  const anonymous = await bootstrap;
  expect(anonymous.status()).toBe(200);
  let anonymousCSRF = (await observedJSON(page, "bootstrap", anonymous))
    .csrf_token as string;
  expect(typeof anonymousCSRF === "string" && anonymousCSRF.length === 43).toBe(
    true,
  );
  const material = JSON.parse(
    readFileSync(join(directory, "credentials.json"), "utf8"),
  ) as { email: string; password: string };
  await page
    .getByRole("textbox", { name: "邮箱", exact: true })
    .fill(material.email);
  await password(page, "Wrong-Independent-Password-93!");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("邮箱或密码不正确");
  await password(page, material.password);
  material.password = "";
  await armJSON(page, "required", "/api/v1/sessions/login");
  const required = response(page, "/api/v1/sessions/login");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const original = await required;
  expect(original.status()).toBe(401);
  expect((await observedJSON(page, "required", original)).code).toBe(
    "CHALLENGE_REQUIRED",
  );
  await expect(
    page.getByRole("button", { name: "开始旋转验证", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeDisabled();
  const originalKey = original.request().headers()["idempotency-key"];
  const originalBody = original.request().postDataJSON();
  expect(typeof originalKey === "string" && originalKey.length === 36).toBe(
    true,
  );
  const create = page.getByRole("button", {
    name: "开始旋转验证",
    exact: true,
  });
  await armJSON(page, "first_create", "/api/v1/auth/challenges");
  const firstCreate = response(page, "/api/v1/auth/challenges");
  await create.click();
  const firstQuestion = await firstCreate;
  expect(firstQuestion.status()).toBe(201);
  const failedID = (await observedJSON(page, "first_create", firstQuestion))
    .id as string;
  expect(firstQuestion.request().postDataJSON().login_key === originalKey).toBe(
    true,
  );
  await expect(page.locator(".gc-rotate-picture img")).toBeVisible();
  const wrongAngle = ((await publicAngle(page)) + 180) % 360;
  // Observe the native disabled blur; no focus/blur override, delay or API patch.
  await page.evaluate(() => {
    const slider = document.querySelector<HTMLInputElement>(
      'input[type="range"]',
    )!;
    slider.addEventListener(
      "blur",
      () => {
        (
          window as unknown as {
            independentBlur: { disabled: boolean; body: boolean };
          }
        ).independentBlur = {
          disabled: slider.disabled,
          body: document.activeElement === document.body,
        };
      },
      { once: true },
    );
  });
  await armJSON(page, "wrong_verify", "/api/v1/auth/challenges/verify");
  const rejected = await keyboardVerify(page, wrongAngle);
  // Fixed D07 HTTP contract maps CHALLENGE_INVALID to 400, not the old plan typo 403.
  expect(rejected.status()).toBe(400);
  expect((await observedJSON(page, "wrong_verify", rejected)).code).toBe(
    "CHALLENGE_INVALID",
  );
  const wrong = rejected.request().postDataJSON();
  expect(
    wrong.challenge_id === failedID &&
      wrong.login_key === originalKey &&
      wrong.email === originalBody.email &&
      wrong.proof.angle === wrongAngle,
  ).toBe(true);
  await expect(
    page.getByRole("slider", { name: "旋转角度", exact: true }),
  ).toHaveCount(0);
  await expect(create).toBeFocused();
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeDisabled();
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  const blur = await page.evaluate(
    () =>
      (
        window as unknown as {
          independentBlur: { disabled: boolean; body: boolean };
        }
      ).independentBlur,
  );
  expect(blur?.disabled === true && blur?.body === true).toBe(true);

  await armJSON(page, "next_create", "/api/v1/auth/challenges");
  const nextCreate = response(page, "/api/v1/auth/challenges");
  await page.keyboard.press("Enter"); // The actual failed-proof handoff owns focus.
  const nextQuestion = await nextCreate;
  expect(nextQuestion.status()).toBe(201);
  const consumedID = (await observedJSON(page, "next_create", nextQuestion))
    .id as string;
  const nextBody = nextQuestion.request().postDataJSON();
  expect(
    consumedID !== failedID &&
      nextBody.login_key === originalKey &&
      nextBody.email === originalBody.email,
  ).toBe(true);
  await expect(page.locator(".gc-rotate-picture img")).toBeVisible();
  const correctAngle = await publicAngle(page);
  await armJSON(page, "correct_verify", "/api/v1/auth/challenges/verify");
  const verified = await keyboardVerify(page, correctAngle);
  expect(verified.status()).toBe(200);
  const proof = verified.request().postDataJSON();
  expect(
    proof.challenge_id === consumedID &&
      proof.login_key === originalKey &&
      proof.email === originalBody.email &&
      proof.proof.angle === correctAngle,
  ).toBe(true);
  let pass = (await observedJSON(page, "correct_verify", verified))
    .pass as string;
  expect(typeof pass === "string" && pass.length === 80).toBe(true);
  await expect(
    page.getByRole("button", { name: "登录", exact: true }),
  ).toBeFocused();
  await armJSON(page, "login", "/api/v1/sessions/login");
  await armJSON(page, "session", "/api/v1/session", "GET");
  const login = response(page, "/api/v1/sessions/login");
  const confirmation = response(page, "/api/v1/session", "GET");
  await page.keyboard.press("Enter");
  const completed = await login;
  expect(completed.status()).toBe(200);
  const finalBody = completed.request().postDataJSON();
  const exactIntent =
    completed.request().headers()["idempotency-key"] === originalKey &&
    finalBody.email === originalBody.email &&
    finalBody.password === originalBody.password &&
    Object.keys(finalBody).sort().join(",") === "challenge_pass,email,password";
  const exactPass = finalBody.challenge_pass === pass;
  expect(exactIntent && exactPass).toBe(true);
  pass = "";
  originalBody.password = "";
  finalBody.password = "";
  finalBody.challenge_pass = "";
  const loginResult = await observedJSON(page, "login", completed);
  const sessionResponse = await confirmation;
  expect(sessionResponse.status()).toBe(200);
  const session = await observedJSON(page, "session", sessionResponse);
  const sameIdentity =
    session.user.id === loginResult.user.id &&
    session.session.id === loginResult.session.id;
  expect(
    sameIdentity &&
      typeof session.csrf_token === "string" &&
      session.csrf_token.length === 43 &&
      session.csrf_token !== anonymousCSRF,
  ).toBe(true);
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  expect(
    (await context.cookies()).some((c) => c.name === "agenteam_local_session"),
  ).toBe(true);
  const badCSRF = await page.evaluate(async (token) => {
    const rejected = await fetch("/api/v1/sessions/logout", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
        "X-CSRF-Token": token,
      },
      body: "{}",
    });
    return { status: rejected.status, code: (await rejected.json()).code };
  }, anonymousCSRF);
  anonymousCSRF = "";
  expect(badCSRF).toEqual({ status: 403, code: "CSRF_FAILED" });
  const stillCurrent = await page.evaluate(async () => {
    const r = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (r.status !== 200) return { ok: false, id: "" };
    return { ok: true, id: (await r.json()).session.id as string };
  });
  expect(stillCurrent.ok && stillCurrent.id === session.session.id).toBe(true);
  const logout = response(page, "/api/v1/sessions/logout");
  const logoutButton = page.getByRole("button", {
    name: "退出登录",
    exact: true,
  });
  await logoutButton.focus();
  await page.keyboard.press("Enter");
  const loggedOut = await logout;
  expect(loggedOut.status()).toBe(204);
  const csrfMatched =
    loggedOut.request().headers()["x-csrf-token"] === session.csrf_token;
  expect(csrfMatched).toBe(true);
  session.csrf_token = "";
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await expect(page.locator("#login-password")).toHaveAccessibleName("密码");
  await expect(page.locator("#login-password")).toHaveValue("");
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  expect(
    (await context.cookies()).some((c) => c.name === "agenteam_local_session"),
  ).toBe(false);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
  writeFileSync(
    join(directory, "independent-result.json"),
    JSON.stringify({
      user_id: session.user.id,
      session_id: session.session.id,
      failed_challenge_id: failedID,
      consumed_challenge_id: consumedID,
      failed_proof: true,
      failed_focus: true,
      native_blur: true,
      exact_intent: exactIntent,
      exact_pass: exactPass,
      identity_confirmed: sameIdentity,
      anonymous_rejected: true,
      session_csrf_matched: csrfMatched,
      logged_out: true,
    }),
    { mode: 0o600 },
  );
});
