const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  providerStats,
} = require("./owned-fixture.cjs");

function modelInput(name, type, structured = true) {
  return {
    name,
    provider_model_id: type === "chat" ? "owned-chat" : "owned-embedding",
    type,
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      input_modalities: ["text"],
      output_modalities: [type === "chat" ? "text" : "vector"],
      reasoning_efforts: [],
      structured_output_modes:
        type === "chat" && structured ? ["json_schema"] : [],
      context_length: null,
      max_output: null,
    },
  };
}

async function seedModel(page, provider, name, type) {
  const model = await api(page, "/system/models", "POST", {
    provider_id: provider,
    input: modelInput(`${name} ${type}`, type),
  });
  expect(model.status).toBe(200);
  return {
    name,
    provider,
    model: model.value.resource_id,
    modelName: `${name} ${type}`,
  };
}

async function pick(page, label, item) {
  const trigger = page.getByRole("button", {
    name: `选择${label}`,
    exact: true,
  });
  await trigger.click();
  const dialog = page.getByRole("dialog", {
    name: `选择${label}`,
    exact: true,
  });
  await dialog
    .getByRole("listitem")
    .filter({ hasText: item.name })
    .getByRole("button", { name: "选择供应商", exact: true })
    .click();
  await dialog
    .getByRole("listitem")
    .filter({ hasText: item.modelName })
    .getByRole("button", { name: "使用此模型", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
}

test("real purpose picker filters incompatible models and persists platform and summary choices", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const suffix = randomUUID().slice(0, 8);
  // Only setup uses the production API. Selection and saving below use real UI.
  const name = `UI family ${suffix}`;
  const provider = await api(page, "/system/model-providers", "POST", {
    input: {
      name,
      protocol: "openai-compatible",
      base_url: owned.provider_base_url,
      enabled: true,
      credential_ref: null,
      options: {},
    },
  });
  expect(provider.status).toBe(200);
  const chat = await seedModel(page, provider.value.resource_id, name, "chat");
  const embedding = await seedModel(
    page,
    provider.value.resource_id,
    name,
    "embedding",
  );
  const incompatible = await api(page, "/system/models", "POST", {
    provider_id: chat.provider,
    input: modelInput(`No schema ${suffix}`, "chat", false),
  });
  expect(incompatible.status).toBe(200);
  await page.goto(`${owned.origin}/system/model-selection`, {
    waitUntil: "domcontentloaded",
  });
  await expect(
    page.getByRole("heading", { name: "平台模型用途", exact: true }).first(),
  ).toBeVisible();
  await page.getByRole("button", { name: "选择记忆模型", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "选择记忆模型",
    exact: true,
  });
  await dialog
    .getByRole("listitem")
    .filter({ hasText: chat.name })
    .getByRole("button", { name: "选择供应商", exact: true })
    .click();
  await expect(
    dialog
      .getByRole("listitem")
      .filter({ hasText: `No schema ${suffix}` })
      .getByRole("button", { name: "使用此模型", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("listitem")
    .filter({ hasText: chat.modelName })
    .getByRole("button", { name: "使用此模型", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await pick(page, "向量模型", embedding);
  await page
    .getByRole("button", { name: "保存平台模型用途", exact: true })
    .click();
  await expect(
    page.getByText("平台模型用途已保存。", { exact: true }),
  ).toBeVisible();
  const platform = await api(page, "/system/model-selection");
  expect(platform.status).toBe(200);
  expect(
    platform.value.configured.embedding === embedding.model &&
      platform.value.configured.memory === chat.model,
  ).toBe(true);

  await pick(page, "会议摘要模型", chat);
  await page
    .getByRole("button", { name: "保存会议摘要用途", exact: true })
    .click();
  await expect(
    page.getByText("会议摘要用途已保存。", { exact: true }),
  ).toBeVisible();
  const summary = await api(page, "/system/model-selection/meeting-summary");
  expect(summary.status).toBe(200);
  expect(summary.value.model === chat.model).toBe(true);
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(
    page
      .getByRole("region", { name: "平台模型用途", exact: true })
      .getByText(chat.modelName, { exact: false }),
  ).toBeVisible();
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
});
