const {
  test,
  expect,
  fillPrivate,
  sessionStatus,
} = require("./owned-fixture.cjs");
const { newOwnedUser, login, api } = require("./journey-support.cjs");

async function logout(page) {
  await page.getByRole("button", { name: "账号", exact: true }).click();
  await page.getByRole("menuitem", { name: "退出登录", exact: true }).click();
  await page
    .getByRole("dialog", { name: "退出登录", exact: true })
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  expect(await sessionStatus(page)).toBe(401);
}

test("saved avatar is shared with the account menu while candidates stay private to the editor", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  // Admin is used only to invite a new disposable user. All profile/avatar
  // writes below belong to that new ordinary identity, never the shared admin.
  const user = await newOwnedUser(page, owned);
  await page.addInitScript(() => {
    const original = URL.revokeObjectURL.bind(URL);
    window.__avatarRevokedURLs = [];
    URL.revokeObjectURL = (url) => {
      window.__avatarRevokedURLs.push(String(url));
      original(url);
    };
  });
  try {
    await login(page, user.email, user.password);
    const trigger = page.getByRole("button", { name: "账号", exact: true });
    // The decorative image deliberately has empty alt; the button owns the
    // accessible name. Locate the image through its accessible parent.
    const menuImage = trigger.locator("img");
    const currentImage = page.getByRole("img", {
      name: "当前头像",
      exact: true,
    });
    await expect(menuImage).toHaveCount(0);
    const bytes = Buffer.from(
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
    await page.getByLabel("选择头像图片", { exact: true }).setInputFiles({
      name: "owned-menu-avatar.png",
      mimeType: "image/png",
      buffer: bytes,
    });
    await expect(
      page.getByRole("img", { name: "待上传头像预览", exact: true }),
    ).toBeVisible();
    await expect(menuImage).toHaveCount(0);
    expect((await api(page, "/me")).value.avatar).toBeNull();
    await page.getByRole("button", { name: "上传头像", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "头像已保存。" }),
    ).toBeVisible();
    await expect(currentImage).toBeVisible();
    await expect(menuImage).toBeVisible();
    const savedURL = await currentImage.getAttribute("src");
    expect(savedURL.startsWith("blob:")).toBe(true);
    await expect(menuImage).toHaveAttribute("src", savedURL);
    expect(
      await menuImage.evaluate(
        (image) => image.complete && image.naturalWidth > 0,
      ),
    ).toBe(true);

    // Unmount the editor through normal SPA navigation. Its saved Blob remains
    // available in the header, and the menu is the personal-settings entry.
    await page
      .getByRole("banner")
      .getByRole("link", { name: "项目", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "项目", exact: true }),
    ).toBeVisible();
    await expect(menuImage).toHaveAttribute("src", savedURL);
    await trigger.click();
    await page.getByRole("menuitem", { name: "个人设置", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "个人资料", exact: true }),
    ).toBeVisible();
    await expect(currentImage).toHaveAttribute("src", savedURL);
    await logout(page);
    expect(
      await page.evaluate(
        (url) => window.__avatarRevokedURLs.includes(url),
        savedURL,
      ),
    ).toBe(true);

    // A fresh document starts directly on another protected page. The top bar
    // must discover the persisted avatar without first mounting ProfilePage.
    await page.goto("/projects");
    await fillPrivate(page.getByLabel("邮箱", { exact: true }), user.email);
    await fillPrivate(page.getByLabel("密码", { exact: true }), user.password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "项目", exact: true }),
    ).toBeVisible();
    await expect(menuImage).toBeVisible();
    const reloadedURL = await menuImage.getAttribute("src");
    expect(reloadedURL.startsWith("blob:")).toBe(true);
    await trigger.click();
    await page.getByRole("menuitem", { name: "个人设置", exact: true }).click();
    await expect(currentImage).toHaveAttribute("src", reloadedURL);
    await page.getByRole("button", { name: "移除头像", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "头像已移除。" }),
    ).toBeVisible();
    await expect(currentImage).toHaveCount(0);
    await expect(menuImage).toHaveCount(0);
    expect(
      await page.evaluate(
        (url) => window.__avatarRevokedURLs.includes(url),
        reloadedURL,
      ),
    ).toBe(true);
    expect((await api(page, "/me")).value.avatar).toBeNull();
    await logout(page);
  } finally {
    // On a failed assertion retire only this context's session. Never emit the
    // browser request, response body, CSRF or Cookie in cleanup errors.
    try {
      await page.evaluate(async () => {
        const response = await fetch("/api/v1/session", { cache: "no-store" });
        if (response.status === 401) return;
        if (response.status !== 200) throw new Error();
        const session = await response.json();
        const logout = await fetch("/api/v1/sessions/logout", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": session.csrf_token,
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: "{}",
        });
        await logout.arrayBuffer();
        if (logout.status !== 204) throw new Error();
      });
    } catch {
      throw new Error("OWNED_AVATAR_SESSION_CLEANUP_FAILED");
    }
  }
});
