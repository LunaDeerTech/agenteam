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

test("one purpose form saves both resources and model deletion replaces required references", async ({
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
  await pick(page, "会议摘要模型", chat);
  for (const label of [
    "向量模型",
    "记忆模型",
    "重排模型",
    "图像模型",
    "会议摘要模型",
  ]) {
    await expect(
      page.getByRole("button", { name: `选择${label}`, exact: true }),
    ).toHaveCount(1);
  }
  const save = page.getByRole("button", { name: "保存", exact: true });
  await expect(save).toHaveCount(1);
  const writes = [];
  const recordWrite = (request) => {
    const path = new URL(request.url()).pathname;
    if (
      request.method() === "PUT" &&
      (path === "/api/v1/system/model-selection" ||
        path === "/api/v1/system/model-selection/meeting-summary")
    )
      writes.push(path);
  };
  page.on("request", recordWrite);
  try {
    await save.click();
    await expect(page.getByText("已保存。", { exact: true })).toBeVisible();
    expect(writes).toEqual([
      "/api/v1/system/model-selection",
      "/api/v1/system/model-selection/meeting-summary",
    ]);
    await expect(save).toBeDisabled();
  } finally {
    page.off("request", recordWrite);
  }
  const platform = await api(page, "/system/model-selection");
  expect(platform.status).toBe(200);
  expect(
    platform.value.configured.embedding === embedding.model &&
      platform.value.configured.memory === chat.model,
  ).toBe(true);

  const summary = await api(page, "/system/model-selection/meeting-summary");
  expect(summary.status).toBe(200);
  expect(summary.value.model === chat.model).toBe(true);
  await page.reload({ waitUntil: "domcontentloaded" });
  const selectedChat = page.getByText(chat.modelName, { exact: false });
  await expect(selectedChat).toHaveCount(2);
  await expect(selectedChat.nth(0)).toBeVisible();
  await expect(selectedChat.nth(1)).toBeVisible();

  const replacementName = `Replacement ${suffix}`;
  const replacement = await api(page, "/system/models", "POST", {
    provider_id: chat.provider,
    input: modelInput(replacementName, "chat"),
  });
  expect(replacement.status).toBe(200);
  await page.goto(`${owned.origin}/system/models/${chat.provider}`, {
    waitUntil: "domcontentloaded",
  });
  const trigger = page.getByRole("button", {
    name: `${chat.modelName}操作`,
    exact: true,
  });
  const openDelete = async () => {
    await trigger.click();
    await page.getByRole("menuitem", { name: "删除", exact: true }).click();
  };
  await openDelete();
  let deletion = page.getByRole("dialog", { name: "删除模型", exact: true });
  await expect(
    deletion.getByText("当前共有 2 处引用。", { exact: true }),
  ).toBeVisible();
  await expect(
    deletion.getByRole("button", { name: "确认删除", exact: true }),
  ).toBeDisabled();
  await deletion.getByRole("button", { name: "取消", exact: true }).click();
  await expect(deletion).toHaveCount(0);
  await expect(trigger).toBeFocused();
  expect((await api(page, `/system/models/${chat.model}`)).status).toBe(200);
  await openDelete();
  deletion = page.getByRole("dialog", { name: "删除模型", exact: true });
  await deletion
    .getByRole("button", { name: "选择替代模型", exact: true })
    .click();
  const replacing = page.getByRole("dialog", {
    name: "选择替代模型",
    exact: true,
  });
  await replacing
    .getByRole("listitem")
    .filter({ hasText: name })
    .getByRole("button", { name: "查看模型", exact: true })
    .click();
  await replacing
    .getByRole("listitem")
    .filter({ hasText: replacementName })
    .getByRole("button", { name: "选择", exact: true })
    .click();
  deletion = page.getByRole("dialog", { name: "删除模型", exact: true });
  await deletion.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(
    deletion.getByText("删除已完成。", { exact: true }),
  ).toBeVisible();
  await deletion.getByRole("button", { name: "完成", exact: true }).click();
  expect((await api(page, `/system/models/${chat.model}`)).status).toBe(404);
  const finalPlatform = await api(page, "/system/model-selection");
  const finalSummary = await api(
    page,
    "/system/model-selection/meeting-summary",
  );
  expect(
    finalPlatform.value.configured.memory === replacement.value.resource_id &&
      finalPlatform.value.configured.embedding === embedding.model &&
      finalSummary.value.model === replacement.value.resource_id,
  ).toBe(true);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
});
