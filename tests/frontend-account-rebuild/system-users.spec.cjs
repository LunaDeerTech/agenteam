const {
  test,
  expect,
  loginSystem,
  closeSystemSession,
} = require("./system-owned.cjs");
const { api, newOwnedUser } = require("./journey-support.cjs");

test("real administrator directory uses bounded cursor pages and keeps registered users read-only", async ({
  page,
  owned,
}) => {
  test.setTimeout(180000);
  await loginSystem(page, owned);
  await expect(
    page.getByRole("heading", { name: "用户", exact: true }),
  ).toBeVisible();
  const directory = page.getByRole("list", { name: "用户目录", exact: true });
  await expect(directory).toBeVisible();
  // Reuse an already seeded isolated runtime after a focused rerun. Populate
  // only when one page is insufficient, using real invitation/SMTP/redeem APIs.
  let first = await api(page, "/system/users?limit=10");
  expect(first.status).toBe(200);
  if (!first.value.next_cursor) {
    const needed = Math.max(0, 11 - first.value.items.length);
    await closeSystemSession(page);
    for (let index = 0; index < needed; index++)
      await newOwnedUser(page, owned);
    await loginSystem(page, owned);
    first = await api(page, "/system/users?limit=10");
  }
  expect(first.status).toBe(200);
  expect(first.value.items).toHaveLength(10);
  expect(typeof first.value.next_cursor).toBe("string");
  await expect(directory.getByRole("listitem")).toHaveCount(10);
  for (const user of first.value.items)
    await expect(
      directory.getByText(user.email, { exact: true }),
    ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "上一页", exact: true }),
  ).toBeDisabled();

  const second = await api(
    page,
    `/system/users?limit=10&cursor=${encodeURIComponent(first.value.next_cursor)}`,
  );
  expect(second.status).toBe(200);
  expect(second.value.items.length).toBeGreaterThan(0);
  expect(second.value.items.length).toBeLessThanOrEqual(10);
  const requested = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname === "/api/v1/system/users" &&
      url.searchParams.get("cursor") === first.value.next_cursor
    );
  });
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  const request = await requested;
  expect([...new URL(request.url()).searchParams.keys()].sort()).toEqual([
    "cursor",
    "limit",
  ]);
  expect(new URL(request.url()).searchParams.get("limit")).toBe("10");
  await expect(
    page.getByRole("navigation", { name: "用户分页", exact: true }),
  ).toContainText("第 2 页");
  await expect(directory.getByRole("listitem")).toHaveCount(
    second.value.items.length,
  );
  for (const user of second.value.items)
    await expect(
      directory.getByText(user.email, { exact: true }),
    ).toBeVisible();
  expect(
    second.value.items.some((item) =>
      first.value.items.some((earlier) => item.id === earlier.id),
    ),
  ).toBe(false);
  await expect(directory.getByRole("button")).toHaveCount(0);
  await expect(directory.getByRole("textbox")).toHaveCount(0);

  await page.getByRole("button", { name: "上一页", exact: true }).click();
  await expect(
    page.getByRole("navigation", { name: "用户分页", exact: true }),
  ).toContainText("第 1 页");
  for (const user of first.value.items)
    await expect(
      directory.getByText(user.email, { exact: true }),
    ).toBeVisible();
});
