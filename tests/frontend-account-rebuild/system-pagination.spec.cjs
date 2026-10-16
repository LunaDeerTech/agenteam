const { test, expect, loginSystem } = require("./system-owned.cjs");
const { api } = require("./journey-support.cjs");

test("real directory page size resets the cursor and reads only the selected bound", async ({
  page,
  owned,
}) => {
  await loginSystem(page, owned);
  const directory = page.getByRole("list", { name: "用户目录", exact: true });
  const pagination = page.getByRole("navigation", {
    name: "用户分页",
    exact: true,
  });
  const selector = page.getByRole("combobox", {
    name: "每页条数",
    exact: true,
  });
  await expect(directory.getByRole("listitem")).toHaveCount(10);
  const requested = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/system/users")
      requested.push({
        limit: url.searchParams.get("limit"),
        cursor: url.searchParams.has("cursor"),
      });
  });
  const select = async (size) => {
    await selector.click();
    await page.getByRole("option", { name: `${size} 条`, exact: true }).click();
    await expect(pagination).toContainText("第 1 页");
    await expect(selector).toBeEnabled();
  };
  await select(5);
  await expect(directory.getByRole("listitem")).toHaveCount(5);
  expect(requested.at(-1)).toEqual({ limit: "5", cursor: false });
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await expect(pagination).toContainText("第 2 页");
  expect(requested.at(-1)).toEqual({ limit: "5", cursor: true });
  for (const size of [20, 50]) {
    const expected = await api(page, `/system/users?limit=${size}`);
    expect(expected.status).toBe(200);
    const beforeSelection = requested.length;
    await select(size);
    await expect.poll(() => requested.length).toBe(beforeSelection + 1);
    await expect(directory.getByRole("listitem")).toHaveCount(
      expected.value.items.length,
    );
    expect(requested.at(-1)).toEqual({ limit: String(size), cursor: false });
    await expect(
      page.getByRole("button", { name: "上一页", exact: true }),
    ).toBeDisabled();
    for (const item of expected.value.items)
      await expect(
        directory.getByText(item.email, { exact: true }),
      ).toBeVisible();
  }
});
