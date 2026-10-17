const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  fillPrivate,
  select,
  providerStats,
} = require("./owned-fixture.cjs");

async function rowAction(page, modelID, action) {
  const row = page
    .getByRole("table", { name: "供应商模型", exact: true })
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name: modelID, exact: true }) });
  await row.getByRole("button", { name: action, exact: true }).click();
}

async function setCapabilities(scope, type) {
  await scope.getByLabel("输入模态", { exact: true }).fill("text");
  await scope
    .getByLabel("输出模态", { exact: true })
    .fill(type === "embedding" ? "vector" : "text");
  if (type === "chat")
    await scope.getByLabel("结构化输出", { exact: true }).fill("json_schema");
}

async function testModel(page, name, message) {
  await rowAction(page, name, "测试");
  const dialog = page.getByRole("dialog", { name: "测试模型", exact: true });
  await dialog.getByRole("button", { name: "开始测试", exact: true }).click();
  await expect(dialog.getByText(message, { exact: true })).toBeVisible();
  await dialog
    .getByRole("button", { name: "关闭", exact: true })
    .filter({ hasText: /^关闭$/ })
    .click();
  await expect(dialog).toHaveCount(0);
}

test("one real supplier configures credentials and mixed models, discovers, imports and tests with explicit recovery", async ({
  page,
  owned,
}) => {
  test.setTimeout(120000);
  expect(process.env.NODE_EXTRA_CA_CERTS === owned.certificate_ca_file).toBe(
    true,
  );
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const name = `UI supplier ${randomUUID().slice(0, 8)}`;
  await page.goto(`${owned.origin}/system/models`, {
    waitUntil: "domcontentloaded",
  });
  await page.getByRole("button", { name: "添加供应商", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "添加供应商", exact: true });
  await dialog.getByLabel("供应商名称", { exact: true }).fill(name);
  await dialog
    .getByLabel("API 端点", { exact: true })
    .fill(owned.provider_base_url);
  await fillPrivate(
    dialog.getByLabel("API 凭据（可选）", { exact: true }),
    owned.provider_credential,
  );
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  // Creation now closes its dialog after canonical readback and opens the
  // inline configuration tab. Credential persistence is still checked below.
  await expect(dialog).toHaveCount(0);
  await expect(page).toHaveURL(/\/system\/models\/[0-9a-f-]+\?tab=config$/);
  await expect(
    page.getByRole("tab", { name: "配置", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
  const providerID = new URL(page.url()).pathname.split("/").at(-1);
  const provider = await api(page, `/system/model-providers/${providerID}`);
  expect(provider.status).toBe(200);
  expect(
    provider.value.input.protocol === "openai-compatible" &&
      !!provider.value.input.credential_ref,
  ).toBe(true);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);

  await page.getByRole("tab", { name: "模型", exact: true }).click();
  await expect(
    page.getByRole("tab", { name: "模型", exact: true }),
  ).toHaveAttribute("aria-selected", "true");

  // Manual creation and editing use the actual fields, with bounded failure later.
  await page.getByRole("button", { name: "添加模型", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "添加模型", exact: true });
  await dialog.getByLabel("模型名称", { exact: true }).fill("Owned rejected");
  await dialog.getByLabel("模型 ID", { exact: true }).fill("owned-rejected");
  await select(page, "模型类型", "对话");
  await setCapabilities(dialog, "chat");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByText("模型已保存。", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await rowAction(page, "owned-rejected", "编辑");
  dialog = page.getByRole("dialog", { name: "编辑模型", exact: true });
  await dialog
    .getByLabel("模型名称", { exact: true })
    .fill("Owned rejected renamed");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByText("模型已保存。", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();

  await page.getByRole("button", { name: "更多", exact: true }).click();
  await page.getByRole("menuitem", { name: "发现模型", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "发现并导入模型", exact: true });
  await dialog.getByRole("button", { name: "开始发现", exact: true }).click();
  await dialog
    .getByRole("checkbox", { name: `${name}/owned-chat`, exact: true })
    .check();
  await dialog
    .getByRole("checkbox", { name: `${name}/owned-embedding`, exact: true })
    .check();
  await dialog
    .getByRole("button", { name: "配置所选模型", exact: true })
    .click();
  for (const [id, type, label] of [
    ["owned-chat", "chat", "对话"],
    ["owned-embedding", "embedding", "向量"],
  ]) {
    const item = dialog
      .locator("details")
      .filter({ has: page.locator("summary", { hasText: id }) });
    if (!(await item.evaluate((element) => element.open)))
      await item.locator(":scope > summary").click();
    await item.getByRole("combobox", { name: "模型类型", exact: true }).click();
    await page.getByRole("option", { name: label, exact: true }).click();
    await setCapabilities(item, type);
  }
  // Hold delivery of the first *real committed* import response. Stopping the
  // batch must preserve that item and prevent dispatch of the second item.
  const createPath = `${owned.origin}/api/v1/system/models`;
  let releaseImport;
  const importGate = new Promise((resolve) => {
    releaseImport = resolve;
  });
  let firstImported = false;
  let importCalls = 0;
  await page.route(createPath, async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    importCalls++;
    try {
      const response = await route.fetch();
      await response.body();
      if (importCalls === 1) {
        firstImported = response.status() === 200;
        await importGate;
      }
      await route.fulfill({ response });
    } catch {
      await route.abort("failed").catch(() => {});
      throw new Error("OWNED_IMPORT_BOUNDARY_FORWARDING_FAILED");
    }
  });
  try {
    await dialog
      .getByRole("button", { name: "导入所选模型", exact: true })
      .click();
    await expect.poll(() => firstImported).toBe(true);
    await dialog
      .getByRole("button", { name: "停止后续导入", exact: true })
      .click();
    releaseImport();
    await expect(
      dialog.getByText("已导入 1 / 2", { exact: true }),
    ).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "继续未完成项", exact: true }),
    ).toBeVisible();
    expect(importCalls).toBe(1);
    const partial = await api(
      page,
      `/system/models?provider_id=${providerID}&limit=10`,
    );
    expect(partial.status).toBe(200);
    expect(
      partial.value.items.filter((item) =>
        ["owned-chat", "owned-embedding"].includes(
          item.input.provider_model_id,
        ),
      ).length,
    ).toBe(1);
    await dialog
      .getByRole("button", { name: "继续未完成项", exact: true })
      .click();
    await expect(
      dialog.getByText("已导入 2 / 2", { exact: true }),
    ).toBeVisible();
    expect(importCalls).toBe(2);
  } finally {
    releaseImport();
    await page.unroute(createPath);
  }
  await expect(dialog.getByText("已导入 2 / 2", { exact: true })).toBeVisible();
  await dialog
    .getByRole("button", { name: "关闭", exact: true })
    .filter({ hasText: /^关闭$/ })
    .click();
  const models = await api(
    page,
    `/system/models?provider_id=${providerID}&limit=10`,
  );
  expect(models.status).toBe(200);
  const chat = models.value.items.find(
    (item) => item.input.provider_model_id === "owned-chat",
  );
  const embedding = models.value.items.find(
    (item) => item.input.provider_model_id === "owned-embedding",
  );
  expect(
    chat?.input.type === "chat" && embedding?.input.type === "embedding",
  ).toBe(true);
  expect((await providerStats(page, owned)).discovery).toBe(
    before.discovery + 1,
  );
  await testModel(page, "owned-chat", "本次测试成功。");
  await testModel(page, "owned-embedding", "本次测试成功。");
  await testModel(page, "owned-rejected", "供应商拒绝了请求。");
  let observed = await providerStats(page, owned);
  expect(
    observed.chat === before.chat + 2 &&
      observed.embedding === before.embedding + 1,
  ).toBe(true);

  // The real request completes, then only its browser delivery is lost. The UI
  // must retain its original command and replay it without another provider hit.
  const path = `${owned.origin}/api/v1/system/models/${chat.id}/test`;
  let dropped = false;
  await page.route(path, async (route) => {
    try {
      const response = await route.fetch();
      await response.body();
      dropped = response.status() === 200;
      await route.abort("failed");
    } catch {
      await route.abort("failed").catch(() => {});
      throw new Error("OWNED_UI_ACCEPTED_RESPONSE_LOSS_FAILED");
    }
  });
  await rowAction(page, "owned-chat", "测试");
  dialog = page.getByRole("dialog", { name: "测试模型", exact: true });
  await dialog.getByRole("button", { name: "开始测试", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "恢复原操作", exact: true }),
  ).toBeVisible();
  await page.unroute(path);
  expect(dropped).toBe(true);
  observed = await providerStats(page, owned);
  expect(observed.chat).toBe(before.chat + 3);
  await dialog.getByRole("button", { name: "恢复原操作", exact: true }).click();
  await expect(
    dialog.getByText("本次测试成功。", { exact: true }),
  ).toBeVisible();
  expect((await providerStats(page, owned)).chat).toBe(observed.chat);
  await dialog
    .getByRole("button", { name: "关闭", exact: true })
    .filter({ hasText: /^关闭$/ })
    .click();
});
