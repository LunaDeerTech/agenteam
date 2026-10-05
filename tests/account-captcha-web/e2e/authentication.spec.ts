import { test, expect, type Page } from "@playwright/test";
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { solvePublicRotation } from "./public-solver";
import { circularDistance, reachableDrag } from "./drag-geometry";

const directory = process.env.AGENTEAM_AUTH_WEB_PRIVATE!;
function credentials(): { email: string; password: string } {
  return JSON.parse(readFileSync(join(directory, "credentials.json"), "utf8"));
}
function result(value: Record<string, unknown>) {
  // Only safe IDs and booleans/counts leave the browser worker.
  writeFileSync(join(directory, "result.json"), JSON.stringify(value), {
    mode: 0o600,
  });
}
async function fillPassword(page: Page, value: string) {
  try {
    const password = page.locator("#login-password");
    await expect(password).toHaveAccessibleName("密码");
    await password.fill(value);
  } catch {
    throw new Error("private password entry failed");
  }
}
async function openLogin(page: Page) {
  await page.goto("/login");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeFocused();
  await expect(
    page.getByRole("textbox", { name: "邮箱", exact: true }),
  ).toBeEditable();
  await expect(page.locator("#login-password")).toHaveAccessibleName("密码");
  await expect(page.getByRole("navigation")).toHaveCount(0);
}
async function identity(page: Page) {
  return page.evaluate(async () => {
    const response = await fetch("/api/v1/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (response.status !== 200)
      return { user_id: "", session_id: "", csrfLength: 0 };
    const body = await response.json();
    return {
      user_id: body.user.id as string,
      session_id: body.session.id as string,
      csrfLength: body.csrf_token.length as number,
    };
  });
}
async function login(page: Page) {
  await openLogin(page);
  const material = credentials();
  await page
    .getByRole("textbox", { name: "邮箱", exact: true })
    .fill(material.email);
  await fillPassword(page, material.password);
  material.password = "";
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeFocused();
  const current = await identity(page);
  expect(current.csrfLength).toBe(43);
  expect(
    current.user_id.length === 36 && current.session_id.length === 36,
  ).toBe(true);
  return current;
}
async function noOverflow(page: Page) {
  const fits = await page.evaluate(
    () => document.documentElement.scrollWidth <= innerWidth,
  );
  if (!fits) {
    const geometry = await page.evaluate(() => ({
      viewport: { width: innerWidth, height: innerHeight },
      zoom: getComputedStyle(document.documentElement).zoom,
      elements: [
        ...document.querySelectorAll(
          "html,body,.app-shell,.system-nav,.brand,.account-actions,.account-name,.ui-button,.home-view,.app-content,.login-panel",
        ),
      ]
        .slice(0, 24)
        .map((element) => {
          const rect = element.getBoundingClientRect();
          return {
            tag: element.tagName,
            class: element.getAttribute("class") || "",
            rect: {
              x: rect.x,
              y: rect.y,
              width: rect.width,
              height: rect.height,
              right: rect.right,
            },
            client: element.clientWidth,
            scroll: element.scrollWidth,
          };
        }),
    }));
    // Geometry only: no DOM text, inputs, URLs or authentication material.
    console.info("overflow geometry", JSON.stringify(geometry));
  }
  expect(fits).toBe(true);
}
async function noStoredMaterials(page: Page) {
  expect(
    await page.evaluate(
      () => localStorage.length === 0 && sessionStorage.length === 0,
    ),
  ).toBe(true);
  expect(
    new URL(page.url()).search === "" && new URL(page.url()).hash === "",
  ).toBe(true);
}

test("[lifecycle] actual login, refresh, Session CSRF and logout in the production application", async ({
  page,
  context,
}) => {
  const current = await login(page);
  await expect(page.getByLabel("Dashboard")).toBeEmpty();
  expect(
    (await page.locator("main").textContent())?.includes("建议之后更换"),
  ).toBe(true);
  expect(
    (await context.cookies()).some(
      (c) =>
        c.name === "agenteam_local_session" &&
        c.httpOnly &&
        c.sameSite === "Lax" &&
        c.path === "/",
    ),
  ).toBe(true);
  const security = await page.evaluate(async () => {
    const bad = await fetch("/api/v1/sessions/logout", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
        "X-CSRF-Token": "x".repeat(43),
      },
      body: "{}",
    });
    return { status: bad.status, code: (await bad.json()).code };
  });
  expect(security).toEqual({ status: 403, code: "CSRF_FAILED" });
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  expect((await identity(page)).session_id === current.session_id).toBe(true);
  await page.getByRole("button", { name: "退出登录", exact: true }).focus();
  const logout = page.waitForResponse(
    (r) =>
      r.request().method() === "POST" &&
      new URL(r.url()).pathname === "/api/v1/sessions/logout",
  );
  await page.keyboard.press("Enter");
  expect((await logout).status()).toBe(204);
  await expect(page.locator("#login-password")).toHaveAccessibleName("密码");
  await expect(page.locator("#login-password")).toHaveValue("");
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  expect(
    (await context.cookies()).some((c) => c.name === "agenteam_local_session"),
  ).toBe(false);
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
  result({ ...current, logged_out: true });
});

async function solve(page: Page) {
  const images = await page.evaluate(async () => {
    const read = async (selector: string) => {
      const image = document.querySelector<HTMLImageElement>(selector)!;
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const context = canvas.getContext("2d")!;
      context.drawImage(image, 0, 0);
      return {
        rgba: Array.from(
          context.getImageData(0, 0, canvas.width, canvas.height).data,
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
for (const mode of ["desktop", "keyboard"])
  test(`[${mode}] real rotate verification and one-time login consumption`, async ({
    page,
  }) => {
    await page.setViewportSize({
      width: mode === "desktop" ? 1024 : 390,
      height: 900,
    });
    await page.emulateMedia({
      colorScheme: mode === "desktop" ? "light" : "dark",
      reducedMotion: "reduce",
    });
    await openLogin(page);
    const material = credentials();
    await page
      .getByRole("textbox", { name: "邮箱", exact: true })
      .fill(material.email);
    await fillPassword(page, "Wrong-Authentication-Password-93!");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("邮箱或密码不正确");
    await fillPassword(page, material.password);
    material.password = "";
    const challenged = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/api/v1/sessions/login",
    );
    await page.getByRole("button", { name: "登录", exact: true }).click();
    const first = await challenged;
    expect((await first.json()).code).toBe("CHALLENGE_REQUIRED");
    const originalKey = first.request().headers()["idempotency-key"];
    const originalBody = first.request().postDataJSON();
    await page
      .getByRole("button", { name: "开始旋转验证", exact: true })
      .click();
    await expect(page.locator(".gc-rotate-picture img")).toBeVisible();
    const reduced = await page.evaluate(() => {
      const controls = [
        ...document.querySelectorAll(".rotate-challenge, .rotate-challenge *"),
      ];
      return {
        matched: matchMedia("(prefers-reduced-motion: reduce)").matches,
        count: controls.length,
        disabled: controls.every((element) => {
          const style = getComputedStyle(element);
          return (
            [style.transitionDuration, style.animationDuration].every((value) =>
              value
                .split(",")
                .every((duration) => Number.parseFloat(duration) === 0),
            ) &&
            style.animationName
              .split(",")
              .every((name) => name.trim() === "none")
          );
        }),
      };
    });
    expect(reduced.matched && reduced.count > 20 && reduced.disabled).toBe(
      true,
    );
    await expect(
      page.getByRole("slider", { name: "旋转角度", exact: true }),
    ).toBeFocused();
    const angle = await solve(page);
    const verification = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/api/v1/auth/challenges/verify",
    );
    let expectedAngle = angle;
    if (mode === "desktop") {
      const handle = await page.locator(".gc-drag-block").boundingBox();
      const geometry = await page
        .locator(".gc-drag-slide-bar")
        .evaluate((element) => {
          const block = element.querySelector<HTMLElement>(".gc-drag-block")!;
          return {
            bar: (element as HTMLElement).offsetWidth,
            handle: block.offsetWidth,
            left: block.offsetLeft,
          };
        });
      expect(handle !== null && geometry.left === 0).toBe(true);
      const target = reachableDrag(angle, geometry.bar - geometry.handle);
      expect(circularDistance(angle, target.angle)).toBeLessThanOrEqual(1);
      expectedAngle = target.angle;
      const x = Math.round(handle!.x + handle!.width / 2),
        y = Math.round(handle!.y + handle!.height / 2);
      await page.mouse.move(x, y);
      await page.mouse.down();
      await page.mouse.move(x + target.pixel, y, { steps: 18 });
      await page.mouse.up();
    } else {
      const slider = page.getByRole("slider", {
        name: "旋转角度",
        exact: true,
      });
      await slider.press("Home");
      for (let i = 0; i < angle; ++i) await slider.press("ArrowRight");
      await expect(slider).toHaveValue(String(angle));
      await slider.press("Enter");
    }
    const response = await verification;
    expect(response.status()).toBe(200);
    const submitted = response.request().postDataJSON();
    expect(submitted.proof.angle).toBe(expectedAngle);
    expect(submitted.login_key === originalKey).toBe(true);
    let pass = (await response.json()).pass as string;
    expect(typeof pass === "string" && pass.length === 80).toBe(true);
    await expect(
      page.getByRole("button", { name: "登录", exact: true }),
    ).toBeFocused();
    const completed = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        new URL(r.url()).pathname === "/api/v1/sessions/login",
    );
    await page.keyboard.press("Enter");
    const final = await completed;
    expect(final.status()).toBe(200);
    const finalBody = final.request().postDataJSON();
    const exactPass = finalBody.challenge_pass === pass;
    const exactKey =
      final.request().headers()["idempotency-key"] === originalKey &&
      finalBody.email === originalBody.email &&
      finalBody.password === originalBody.password;
    expect(exactPass && exactKey).toBe(true);
    pass = "";
    originalBody.password = "";
    finalBody.password = "";
    finalBody.challenge_pass = "";
    await expect(
      page.getByRole("heading", { name: "首页", exact: true }),
    ).toBeVisible();
    await noOverflow(page);
    await noStoredMaterials(page);
    result({
      ...(await identity(page)),
      challenge_id: submitted.challenge_id,
      verified: true,
      exact_pass: exactPass,
      exact_key: exactKey,
    });
  });

test("[revocation] real password change revokes the other browser and hides old contents", async ({
  page,
  browser,
}) => {
  const original = await login(page);
  const other = await browser.newContext({
    baseURL: process.env.AGENTEAM_AUTH_WEB_ORIGIN,
  });
  try {
    const peer = await other.newPage();
    await login(peer);
    const material = credentials();
    const status = await peer.evaluate(
      async ({ password }) => {
        const session = await (await fetch("/api/v1/session")).json();
        const profile = await (await fetch("/api/v1/me")).json();
        const next = "Changed-Authentication-Password-84!";
        const response = await fetch("/api/v1/me/change-password", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
            "X-CSRF-Token": session.csrf_token,
          },
          body: JSON.stringify({
            version: profile.user.version,
            current_password: password,
            new_password: next,
            confirmation: next,
          }),
        });
        return response.status;
      },
      { password: material.password },
    );
    material.password = "";
    expect(status).toBe(200);
    const unavailable = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/v1/session" && r.status() === 401,
    );
    await page.reload();
    expect((await unavailable).status()).toBe(401);
    await expect(
      page.getByRole("heading", { name: "登录", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel("Dashboard")).toHaveCount(0);
    expect(
      (await page.context().cookies()).some(
        (c) => c.name === "agenteam_local_session",
      ),
    ).toBe(false);
    result({ ...original, unavailable: true });
  } finally {
    await other.close();
  }
});

test("[expiry] exact owned database expiry is observed through the real Session route", async ({
  page,
}) => {
  const original = await login(page);
  writeFileSync(
    join(directory, "expire.json"),
    JSON.stringify({ session_id: original.session_id }),
    { mode: 0o600 },
  );
  await expect
    .poll(() => existsSync(join(directory, "expire-ack.json")), {
      timeout: 10_000,
    })
    .toBe(true);
  const unavailable = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/api/v1/session" && r.status() === 401,
  );
  await page.reload();
  expect((await unavailable).status()).toBe(401);
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  expect(
    (await page.context().cookies()).some(
      (c) => c.name === "agenteam_local_session",
    ),
  ).toBe(false);
  result({ ...original, unavailable: true });
});

test("[layouts] formal dist, real saved themes, keyboard, narrow widths and safe fallback", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await openLogin(page);
  await page
    .getByRole("textbox", { name: "邮箱", exact: true })
    .fill("long-authentication-email-for-layout-check@owned-example.invalid");
  await page.getByRole("textbox", { name: "邮箱", exact: true }).focus();
  await page.keyboard.press("Tab");
  await expect(page.locator("#login-password")).toBeFocused();
  await noOverflow(page);
  const current = await login(page);
  const renamed = await page.evaluate(async () => {
    const session = await (await fetch("/api/v1/session")).json();
    const response = await fetch("/api/v1/me", {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
        "X-CSRF-Token": session.csrf_token,
      },
      body: JSON.stringify({
        version: session.user.version,
        display_name: "很长的实际显示名称".repeat(8),
      }),
    });
    return response.status;
  });
  expect(renamed).toBe(200);
  let count = 0;
  for (const theme of ["light", "dark"]) {
    const saved = await page.evaluate(async (theme) => {
      const session = await (await fetch("/api/v1/session")).json();
      const response = await fetch("/api/v1/me/preferences", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
          "X-CSRF-Token": session.csrf_token,
        },
        body: JSON.stringify({ version: session.user.version, theme }),
      });
      return response.status;
    }, theme);
    expect(saved).toBe(200);
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "首页", exact: true }),
    ).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
    await expect(page.locator(".account-name")).toHaveText(
      "很长的实际显示名称".repeat(8),
    );
    await page.emulateMedia({
      reducedMotion: "reduce",
      colorScheme: theme === "light" ? "dark" : "light",
    });
    for (const width of [1440, 1024, 834, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await noOverflow(page);
      await expect(
        page.getByRole("button", { name: "退出登录", exact: true }),
      ).toBeVisible();
      if (
        process.env.AGENTEAM_AUTH_WEB_IMAGES &&
        (width === 1440 || width === 390)
      ) {
        await expect(page.locator("#login-password")).toHaveCount(0);
        await page.screenshot({
          path: join(
            process.env.AGENTEAM_AUTH_WEB_IMAGES,
            `${theme}-${width}.png`,
          ),
        });
      }
      count++;
    }
  }
  // CSS zoom exercises a 200% content scale without changing the device scale
  // factor (which alone is not a layout zoom).
  await page.evaluate(() => {
    document.documentElement.style.zoom = "2";
  });
  await noOverflow(page);
  await page.getByRole("button", { name: "退出登录", exact: true }).focus();
  await expect(
    page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeFocused();
  await page.evaluate(() => {
    document.documentElement.style.zoom = "";
  });
  await noStoredMaterials(page);
  const responses = await page.evaluate(async () => {
    const api = await fetch("/api/v1/not-a-real-authentication-route");
    const asset = await fetch("/assets/not-a-real-authentication-file.js");
    const scripts = [...document.scripts].map((s) => s.src).filter(Boolean);
    const bodies = await Promise.all(
      scripts.map(async (src) => (await fetch(src)).text()),
    );
    return {
      apiStatus: api.status,
      apiHTML: (api.headers.get("content-type") ?? "").includes("text/html"),
      assetStatus: asset.status,
      assetIndex: (await asset.text()).includes('<div id="app">'),
      noDebug: bodies.every(
        (s) => !s.includes("DebugView") && !s.includes("debugFixtures"),
      ),
    };
  });
  expect(responses).toEqual({
    apiStatus: 404,
    apiHTML: false,
    assetStatus: 404,
    assetIndex: false,
    noDebug: true,
  });
  await page.goto("/debug");
  await expect(
    page.getByRole("heading", { name: "未找到页面", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Dashboard")).toHaveCount(0);
  result({ ...current, layouts: count });
});
