const { test, expect, fillPrivate } = require("./owned-fixture.cjs");
const { newOwnedUser } = require("./journey-support.cjs");
const {
  solvePublicRotation,
} = require("../account-captcha-web/e2e/public-solver.ts");

async function publicAngle(page) {
  await expect(
    page.getByRole("img", { name: "需要旋转的图片", exact: true }),
  ).toBeVisible();
  const pixels = await page.evaluate(async () => {
    const read = async (image) => {
      await image.decode();
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalWidth;
      canvas.height = image.naturalHeight;
      const painter = canvas.getContext("2d");
      painter.drawImage(image, 0, 0);
      return {
        width: canvas.width,
        height: canvas.height,
        rgba: Array.from(
          painter.getImageData(0, 0, canvas.width, canvas.height).data,
        ),
      };
    };
    const images = document.querySelectorAll(".challenge-images img");
    return { master: await read(images[0]), thumb: await read(images[1]) };
  });
  return (await page.evaluate(solvePublicRotation, pixels)).angle;
}

async function turnWithKeyboard(slider, angle) {
  await slider.focus();
  await slider.press("Home");
  for (let step = 0; step < angle; step++) await slider.press("ArrowRight");
  await expect(slider).toHaveValue(String(angle));
}

test("real login rejection remains correctable and public-image rotation verifies through keyboard with the original login key", async ({
  page,
  owned,
}) => {
  test.setTimeout(120000);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  const user = await newOwnedUser(page, owned);
  await page.goto("/login");
  let readRejection = false;
  for (let attempt = 0; attempt < 5; attempt++) {
    await fillPrivate(page.getByLabel("邮箱", { exact: true }), user.email);
    await fillPrivate(
      page.getByLabel("密码", { exact: true }),
      "Not-The-Correct-Password-99!",
    );
    const rejected = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/sessions/login") &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "登录", exact: true }).click();
    const response = await rejected;
    expect(response.status()).toBe(401);
    if (!readRejection) {
      // Decode only the non-secret problem fields. Never attach the full body.
      let problem;
      try {
        await response.finished();
        problem = await response.json();
      } catch {
        throw new Error("OWNED_LOGIN_PROBLEM_READ_FAILED");
      }
      expect(problem.code).toBe("UNAUTHENTICATED");
      expect(problem.commit_state).toBe("not_started");
      readRejection = true;
    }
    await page.waitForFunction(
      () =>
        document.querySelector(".rotate-challenge") ||
        [...document.querySelectorAll("button")].some(
          (button) => button.textContent.trim() === "登录" && !button.disabled,
        ),
    );
    await expect(
      page.getByRole("button", { name: "重试原请求", exact: true }),
    ).toHaveCount(0);
    if (await page.locator(".rotate-challenge").count()) {
      await page
        .getByRole("button", { name: "重新填写登录信息", exact: true })
        .click();
      break;
    }
    await expect(page.getByLabel("密码", { exact: true })).toBeEnabled();
  }
  await fillPrivate(page.getByLabel("邮箱", { exact: true }), user.email);
  await fillPrivate(page.getByLabel("密码", { exact: true }), user.password);
  const requested = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/sessions/login") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const challenged = await requested;
  const originalKey = challenged.request().headers()["idempotency-key"];
  await expect(
    page.getByRole("region", { name: "登录验证", exact: true }),
  ).toBeVisible();
  const slider = page.getByRole("slider");
  const angle = await publicAngle(page);
  await turnWithKeyboard(slider, (angle + 90) % 360);
  const refused = page.waitForResponse((response) =>
    response.url().endsWith("/api/v1/auth/challenges/verify"),
  );
  await page.getByRole("button", { name: "验证并登录", exact: true }).click();
  expect((await refused).status()).toBe(400);
  await expect(
    page.getByRole("button", { name: "换一张", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "换一张", exact: true }).click();
  await turnWithKeyboard(slider, await publicAngle(page));
  const verified = page.waitForResponse((response) =>
    response.url().endsWith("/api/v1/auth/challenges/verify"),
  );
  const loggedIn = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/sessions/login") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "验证并登录", exact: true }).click();
  expect((await verified).status()).toBe(200);
  const completed = await loggedIn;
  expect(completed.status()).toBe(200);
  expect(
    Boolean(originalKey) &&
      completed.request().headers()["idempotency-key"] === originalKey,
  ).toBe(true);
  await expect(
    page.getByRole("heading", { name: "首页", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
