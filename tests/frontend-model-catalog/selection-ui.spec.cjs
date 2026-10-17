const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  providerStats,
} = require("./owned-fixture.cjs");

function modelInput(name, type, structured = true, providerModelID) {
  return {
    name,
    provider_model_id:
      providerModelID || (type === "chat" ? "owned-chat" : "owned-embedding"),
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
    display: `${name}/${type === "chat" ? "owned-chat" : "owned-embedding"}`,
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
    .filter({ has: page.getByText(item.display, { exact: true }) })
    .getByRole("button", { name: "使用此模型", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
}

test("one summary resource saves once, displays provider model IDs and replaces its required reference", async ({
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
    input: modelInput(`No schema ${suffix}`, "chat", false, "no-schema"),
  });
  expect(incompatible.status).toBe(200);
  await page.goto(`${owned.origin}/system/model-selection`, {
    waitUntil: "domcontentloaded",
  });
  await expect(
    page.getByRole("heading", { name: "平台模型用途", exact: true }).first(),
  ).toBeVisible();
  await page.getByRole("button", { name: "选择总结模型", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "选择总结模型",
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
      .filter({ has: page.getByText(`${name}/no-schema`, { exact: true }) })
      .getByRole("button", { name: "使用此模型", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("listitem")
    .filter({ has: page.getByText(chat.display, { exact: true }) })
    .getByRole("button", { name: "使用此模型", exact: true })
    .click();
  await expect(dialog).toHaveCount(0);
  await pick(page, "向量模型", embedding);
  for (const label of ["向量模型", "总结模型", "重排模型", "图像模型"]) {
    await expect(
      page.getByRole("button", { name: `选择${label}`, exact: true }),
    ).toHaveCount(1);
  }
  await expect(
    page.getByRole("button", { name: "选择记忆模型", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "选择会议摘要模型", exact: true }),
  ).toHaveCount(0);
  const initialSelection = await api(page, "/system/model-selection");
  expect(initialSelection.status).toBe(200);
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
    expect(writes).toEqual(["/api/v1/system/model-selection"]);
    await expect(save).toBeDisabled();
  } finally {
    page.off("request", recordWrite);
  }
  const platform = await api(page, "/system/model-selection");
  expect(platform.status).toBe(200);
  expect(platform.value).toEqual({
    id: initialSelection.value.id,
    version: String(BigInt(initialSelection.value.version) + 1n),
    configured: {
      embedding: embedding.model,
      summary: chat.model,
      reranker: null,
      image: null,
    },
  });
  await page.reload({ waitUntil: "domcontentloaded" });
  await expect(page.getByText(chat.display, { exact: true })).toBeVisible();
  await expect(
    page.getByText(embedding.display, { exact: true }),
  ).toBeVisible();
  await expect(page.getByText(chat.modelName, { exact: true })).toHaveCount(0);

  const replacementName = `Replacement ${suffix}`;
  const replacement = await api(page, "/system/models", "POST", {
    provider_id: chat.provider,
    input: modelInput(replacementName, "chat", true, "replacement-chat"),
  });
  expect(replacement.status).toBe(200);
  await page.goto(`${owned.origin}/system/models/${chat.provider}`, {
    waitUntil: "domcontentloaded",
  });
  const trigger = page.getByRole("button", {
    name: `${chat.display}操作`,
    exact: true,
  });
  const openDelete = async () => {
    await trigger.click();
    await page.getByRole("menuitem", { name: "删除", exact: true }).click();
  };
  await openDelete();
  let deletion = page.getByRole("dialog", { name: "删除模型", exact: true });
  await expect(
    deletion.getByText("当前共有 1 处引用。", { exact: true }),
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
    .filter({
      has: page.getByText(`${name}/replacement-chat`, { exact: true }),
    })
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
  expect(finalPlatform.status).toBe(200);
  expect(
    finalPlatform.value.configured.summary === replacement.value.resource_id &&
      finalPlatform.value.configured.embedding === embedding.model,
  ).toBe(true);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
});
