const { test, expect } = require("./owned-fixture.cjs");
const { newOwnedUser, login, api } = require("./journey-support.cjs");

function safeRoute(handler) {
  return async (route) => {
    try {
      await handler(route);
    } catch {
      await route.abort("failed").catch(() => {});
      // route.fetch errors otherwise include the complete private request headers.
      throw new Error("OWNED_ROUTE_FORWARDING_FAILED");
    }
  };
}

async function pngBytes(page) {
  return Buffer.from(
    await page.evaluate(() => {
      const canvas = document.createElement("canvas");
      canvas.width = 32;
      canvas.height = 32;
      const painter = canvas.getContext("2d");
      painter.fillStyle = "#476539";
      painter.fillRect(0, 0, 32, 32);
      return canvas.toDataURL("image/png").split(",")[1];
    }),
    "base64",
  );
}

test("real avatar rejects unsafe files and supports local preview, cancellation, upload and reload", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  // This focused case provisions a new ordinary account through real APIs and
  // accepted SMTP. Invitation UI itself is covered by account-journey.spec.cjs.
  const user = await newOwnedUser(page, owned);
  await login(page, user.email, user.password);

  await test.step("reject unsafe candidates without upload, preview and cancel a valid PNG", async () => {
    let uploads = 0;
    const observeUpload = (request) => {
      if (
        request.url().endsWith("/api/v1/me/avatar") &&
        request.method() === "PUT"
      )
        uploads++;
    };
    page.on("request", observeUpload);
    const file = page.getByLabel("选择头像图片", { exact: true });
    await file.setInputFiles({
      name: "rejected.svg",
      mimeType: "image/svg+xml",
      buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"/>'),
    });
    await expect(page.getByRole("alert")).toContainText(
      "请选择静态 JPEG、PNG 或 WebP 图片。",
    );
    expect(uploads).toBe(0);
    const bytes = await pngBytes(page);
    await file.setInputFiles({
      name: "owned.png",
      mimeType: "image/png",
      buffer: bytes,
    });
    await expect(
      page.getByRole("img", { name: "待上传头像预览", exact: true }),
    ).toBeVisible();
    expect(uploads).toBe(0);
    await page.getByRole("button", { name: "取消选择", exact: true }).click();
    await expect(
      page.getByRole("img", { name: "待上传头像预览", exact: true }),
    ).toHaveCount(0);
    expect((await api(page, "/me")).value.avatar).toBeNull();
    await file.setInputFiles({
      name: "owned.png",
      mimeType: "image/png",
      buffer: bytes,
    });
    await page.getByRole("button", { name: "上传头像", exact: true }).click();
    await expect(page.getByRole("status")).toContainText("头像已保存。");
    await expect(
      page.getByRole("img", { name: "当前头像", exact: true }),
    ).toBeVisible();
    const profile = await api(page, "/me");
    expect(profile.value.avatar.media_type).toBe("image/jpeg");
    expect(Number(profile.value.avatar.byte_size)).toBeGreaterThan(0);
    expect(uploads).toBe(1);
    await page.reload();
    await expect(
      page.getByRole("img", { name: "当前头像", exact: true }),
    ).toBeVisible();
    page.off("request", observeUpload);
  });
});

test("real confirmed-delete read recovery and explicit unknown profile replay", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  if (process.env.NODE_EXTRA_CA_CERTS !== owned.certificate_file)
    throw new Error("OWNED_NODE_CA_TRUST_REQUIRED");
  const user = await newOwnedUser(page, owned);
  await login(page, user.email, user.password);
  // Seed an avatar through the actual API; the separately passed browser case
  // owns candidate validation and upload UI. Only recovery is repeated here.
  const seeded = await page.evaluate(
    async (base64) => {
      const current = await (await fetch("/api/v1/session")).json();
      const profile = await (await fetch("/api/v1/me")).json();
      const response = await fetch("/api/v1/me/avatar", {
        method: "PUT",
        headers: {
          "Content-Type": "image/png",
          "If-Match": `"${profile.user.version}"`,
          "X-CSRF-Token": current.csrf_token,
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: Uint8Array.from(atob(base64), (character) =>
          character.charCodeAt(0),
        ),
      });
      await response.arrayBuffer();
      return response.status;
    },
    (await pngBytes(page)).toString("base64"),
  );
  expect(seeded).toBe(200);
  await page.reload();
  await expect(
    page.getByRole("img", { name: "当前头像", exact: true }),
  ).toBeVisible();

  await test.step("a confirmed delete followed by a failed profile read only offers a read retry", async () => {
    let deletes = 0;
    let failedRead = false;
    await page.route(
      "**/api/v1/me/avatar",
      safeRoute(async (route) => {
        if (route.request().method() === "DELETE") {
          deletes++;
          const response = await route.fetch();
          expect(response.status()).toBe(204);
          await response.body();
          failedRead = true;
          await route.fulfill({ response });
        } else await route.continue();
      }),
    );
    await page.route(
      "**/api/v1/me",
      safeRoute(async (route) => {
        if (route.request().method() === "GET" && failedRead) {
          failedRead = false;
          await route.abort("connectionfailed");
        } else await route.continue();
      }),
    );
    await page.getByRole("button", { name: "移除头像", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText(
      "头像已移除，当前资料尚未刷新。",
    );
    await expect(
      page.getByRole("img", { name: "当前头像", exact: true }),
    ).toHaveCount(0);
    await page
      .getByRole("button", { name: "重新读取资料", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "选择头像", exact: true }),
    ).toBeEnabled();
    expect((await api(page, "/me")).value.avatar).toBeNull();
    expect(deletes).toBe(1);
    await page.unroute("**/api/v1/me/avatar");
    await page.unroute("**/api/v1/me");
  });

  await test.step("after an actually committed profile response is lost, explicit replay uses the same command", async () => {
    const attempts = [];
    let committedVersion;
    await page.route(
      "**/api/v1/me",
      safeRoute(async (route) => {
        if (route.request().method() !== "PATCH") return route.continue();
        attempts.push({
          key: route.request().headers()["idempotency-key"],
          body: route.request().postData(),
        });
        const response = await route.fetch();
        expect(response.status()).toBe(200);
        const profile = await response.json();
        if (attempts.length === 1) {
          committedVersion = profile.user.version;
          await route.abort("connectionfailed");
        } else await route.fulfill({ response });
      }),
    );
    await page
      .getByLabel("显示名称", { exact: true })
      .fill("原请求恢复后的资料");
    await page.getByRole("button", { name: "保存资料", exact: true }).click();
    await expect(page.getByRole("alert")).toContainText("原操作结果尚未确认。");
    await expect(page.getByLabel("显示名称", { exact: true })).toBeDisabled();
    expect(attempts.length).toBe(1);
    await page.getByRole("button", { name: "重试原请求", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "个人资料已保存。" }),
    ).toBeVisible();
    expect(attempts.length).toBe(2);
    expect(
      attempts[0].key === attempts[1].key &&
        attempts[0].body === attempts[1].body,
    ).toBe(true);
    await page.unroute("**/api/v1/me");
    const profile = await api(page, "/me");
    expect(profile.value.user.version).toBe(committedVersion);
    expect(profile.value.user.display_name).toBe("原请求恢复后的资料");
  });
});
