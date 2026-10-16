const { test, expect, api, login } = require("./owned-fixture.cjs");
const endpoint = "/system/outbound-policy";

async function saveRules(page) {
  const responsePromise = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/v1${endpoint}` &&
      response.request().method() === "PUT",
  );
  await page.getByRole("button", { name: "保存规则", exact: true }).click();
  const response = await responsePromise;
  expect(response.status()).toBe(200);
  let receipt;
  try {
    receipt = await response.json();
  } catch {
    throw new Error("RULE_RECEIPT_UNREADABLE");
  }
  expect(
    Object.keys(receipt).sort().join() ===
      "audit_id,created_at,rule_count,version",
  ).toBe(true);
  await expect(
    page.getByRole("status").filter({ hasText: "访问规则已保存。" }),
  ).toBeVisible();
  return receipt;
}
function row(page, index) {
  return page.getByRole("group", { name: `规则 ${index + 1}`, exact: true });
}

test("owned rules persist CRUD and preserve a conflicting draft until explicit adoption", async ({
  page,
  owned,
}) => {
  await login(page, owned);
  const original = await api(page, endpoint);
  expect(original.status).toBe(200);
  const cidr = "10.241.216.0/24";
  if (
    original.value.rules.some((rule) => rule.cidr === cidr) ||
    original.value.rules.length > 250
  )
    throw new Error("OWNED_RULE_INPUT_COLLISION");
  await page.getByRole("button", { name: "添加规则", exact: true }).click();
  const added = row(page, original.value.rules.length);
  await added.getByLabel("CIDR", { exact: true }).fill(cidr);
  await added.getByLabel("端口", { exact: true }).fill("9443");
  const createdReceipt = await saveRules(page);
  const created = await api(page, endpoint);
  expect(
    created.value.version === createdReceipt.version &&
      createdReceipt.rule_count === String(original.value.rules.length + 1),
  ).toBe(true);
  expect(
    created.value.rules.some(
      (rule) =>
        rule.cidr === cidr &&
        JSON.stringify(rule.ports) === "[9443]" &&
        rule.allow_http === false,
    ),
  ).toBe(true);
  await page.reload();
  const index = created.value.rules.findIndex((rule) => rule.cidr === cidr);
  await expect(
    row(page, index).getByLabel("端口", { exact: true }),
  ).toHaveValue("9443");
  await row(page, index).getByLabel("端口", { exact: true }).fill("9443, 8080");
  await row(page, index).getByRole("checkbox", { name: "允许 HTTP" }).check();
  await saveRules(page);
  const edited = await api(page, endpoint);
  const editIndex = edited.value.rules.findIndex((rule) => rule.cidr === cidr);
  expect(
    JSON.stringify(edited.value.rules[editIndex].ports) === "[8080,9443]" &&
      edited.value.rules[editIndex].allow_http,
  ).toBe(true);

  // An independent real update advances the DB version. GET is an observation,
  // never permission to silently replace the user's stale draft.
  await row(page, editIndex).getByLabel("端口", { exact: true }).fill("9080");
  const remoteRules = edited.value.rules.map((rule) =>
    rule.cidr === cidr ? { ...rule, ports: [9081] } : rule,
  );
  expect(
    (
      await api(page, endpoint, "PUT", {
        expected_version: edited.value.version,
        rules: remoteRules,
      })
    ).status,
  ).toBe(200);
  const conflictResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === `/api/v1${endpoint}` &&
      response.request().method() === "PUT",
  );
  await page.getByRole("button", { name: "保存规则", exact: true }).click();
  expect((await conflictResponse).status()).toBe(409);
  await expect(page.getByRole("alert")).toContainText("你的草稿已保留");
  await expect(
    row(page, editIndex).getByLabel("端口", { exact: true }),
  ).toHaveValue("9080");
  await page.getByRole("button", { name: "读取最新配置", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "采用最新配置", exact: true }),
  ).toBeEnabled();
  await expect(
    row(page, editIndex).getByLabel("端口", { exact: true }),
  ).toHaveValue("9080");
  await page.getByRole("button", { name: "采用最新配置", exact: true }).click();
  await page
    .getByRole("dialog", { name: "放弃当前修改", exact: true })
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(
    row(page, editIndex).getByLabel("端口", { exact: true }),
  ).toHaveValue("9081");
  await page
    .getByRole("button", { name: `删除规则 ${editIndex + 1}`, exact: true })
    .click();
  // Final deletion confirmation selector follows the product owner's stable
  // public dialog contract; a candidate without it must not silently pass.
  await page
    .getByRole("dialog", { name: "删除访问规则", exact: true })
    .getByRole("button", { name: "删除规则", exact: true })
    .click();
  await saveRules(page);
  const removed = await api(page, endpoint);
  expect(
    removed.value.rules.length === original.value.rules.length &&
      !removed.value.rules.some((rule) => rule.cidr === cidr),
  ).toBe(true);
  await page.reload();
  await expect(
    page.getByRole("button", { name: "添加规则", exact: true }),
  ).toBeEnabled();
  expect(
    (await api(page, endpoint)).value.version === removed.value.version,
  ).toBe(true);
});
