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

async function seedModel(
  page,
  provider,
  providerName,
  type,
  remoteID,
  structured = true,
) {
  const modelName = `Custom ${remoteID}`;
  const model = await api(page, "/system/models", "POST", {
    provider_id: provider,
    input: modelInput(modelName, type, structured, remoteID),
  });
  expect(model.status).toBe(200);
  return {
    provider,
    model: model.value.resource_id,
    modelName,
    remoteID,
    display: `${providerName}/${remoteID}`,
  };
}

function option(page, panel, display) {
  return panel.getByRole("option").filter({
    has: page.getByText(display, { exact: true }),
  });
}

function modelResponse(page, query, paged = false) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      response.request().method() === "GET" &&
      url.pathname === "/api/v1/system/models" &&
      url.searchParams.get("q") === query &&
      !url.searchParams.has("provider_id") &&
      url.searchParams.has("cursor") === paged
    );
  });
}

async function search(page, panel, query) {
  const pending = modelResponse(page, query);
  await panel
    .getByRole("combobox", { name: "搜索供应商或模型", exact: true })
    .fill(query);
  const response = await pending;
  expect(response.status()).toBe(200);
}

async function openPicker(page, label) {
  const trigger = page.getByRole("button", {
    name: new RegExp(`^${label}(?:，当前 .*)?$`),
  });
  await trigger.click();
  const panel = page.getByRole("dialog", { name: label, exact: true });
  await expect(panel).toBeVisible();
  return { trigger, panel };
}

test("anchored global picker searches across pages and suppliers, drafts once and replaces a summary reference", async ({
  page,
  owned,
}) => {
  test.setTimeout(90000);
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const marker = `ui-picker-${randomUUID().slice(0, 8)}`;
  const providers = [];
  for (const suffix of ["a", "b"]) {
    const name = `${marker}-${suffix}`;
    const created = await api(page, "/system/model-providers", "POST", {
      input: {
        name,
        protocol: "openai-compatible",
        base_url: owned.provider_base_url,
        enabled: true,
        credential_ref: null,
        options: {},
      },
    });
    expect(created.status).toBe(200);
    providers.push({ id: created.value.resource_id, name });
  }
  const [a, b] = providers;
  // Thirteen models exceed the real UI's limit 10. The target is deliberately
  // oldest, so neither first-page client filtering nor a provider-only list can pass.
  const chat = await seedModel(page, b.id, b.name, "chat", `target-${marker}`);
  const replacement = await seedModel(
    page,
    a.id,
    a.name,
    "chat",
    `replacement-${marker}`,
  );
  const embedding = await seedModel(
    page,
    a.id,
    a.name,
    "embedding",
    `embedding-${marker}`,
  );
  const incompatible = await seedModel(
    page,
    a.id,
    a.name,
    "chat",
    `no-schema-${marker}`,
    false,
  );
  for (let index = 0; index < 9; index += 1)
    await seedModel(page, a.id, a.name, "chat", `filler-${index}-${marker}`);
  const initialSelection = await api(page, "/system/model-selection");
  expect(initialSelection.status).toBe(200);
  const writes = [];
  const recordWrite = (request) => {
    const path = new URL(request.url()).pathname;
    if (
      request.method() === "PUT" &&
      path.startsWith("/api/v1/system/model-selection")
    )
      writes.push(path);
  };
  page.on("request", recordWrite);
  await page.goto(`${owned.origin}/system/model-selection`, {
    waitUntil: "domcontentloaded",
  });
  await expect(
    page.getByRole("heading", { name: "平台模型用途", exact: true }).first(),
  ).toBeVisible();
  const { trigger, panel } = await openPicker(page, "选择总结模型（Chat）");
  await expect(panel).not.toHaveAttribute("aria-modal", "true");
  await expect(panel.getByRole("option")).toHaveCount(10);
  await expect(option(page, panel, chat.display)).toHaveCount(0);
  await expect(option(page, panel, incompatible.display)).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  const triggerBox = await trigger.boundingBox();
  const panelBox = await panel.boundingBox();
  expect(triggerBox !== null && panelBox !== null).toBe(true);
  expect(panelBox.y >= triggerBox.y + triggerBox.height - 1).toBe(true);
  await search(page, panel, chat.remoteID);
  await expect(panel.getByRole("option")).toHaveCount(1);
  await expect(option(page, panel, chat.display)).toBeVisible();
  await search(page, panel, marker);
  await expect(panel.getByRole("option")).toHaveCount(10);
  await expect(option(page, panel, chat.display)).toHaveCount(0);
  await expect(
    panel.getByRole("button", { name: "下一页", exact: true }),
  ).toBeEnabled();
  const nextResponse = modelResponse(page, marker, true);
  await panel.getByRole("button", { name: "下一页", exact: true }).click();
  const response = await nextResponse;
  expect(response.status()).toBe(200);
  // The UI's parsed result and exact-cursor read below establish application
  // completion. Playwright's body/finished bookkeeping is not a business gate.
  const nextURL = new URL(response.url());
  expect(!!nextURL.searchParams.get("cursor")).toBe(true);
  // Re-read exactly the cursor used by the UI through the page's normal fetch,
  // not CDP's response-body cache. This is read-only and does not invoke a model.
  const second = await api(
    page,
    `${nextURL.pathname.replace(/^\/api\/v1/, "")}${nextURL.search}`,
  );
  expect(second.status).toBe(200);
  expect(second.value.items.length).toBe(3);
  expect(second.value.items.some((item) => item.id === chat.model)).toBe(true);
  expect(second.value.next_cursor).toBeNull();
  await expect(panel.getByRole("option")).toHaveCount(3);
  await expect(option(page, panel, chat.display)).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "下一页", exact: true }),
  ).toBeDisabled();
  await expect(
    panel.getByRole("button", { name: "上一页", exact: true }),
  ).toBeEnabled();
  await option(page, panel, chat.display).click();
  await expect(panel).toHaveCount(0);
  await expect(trigger).toBeFocused();
  expect(writes).toEqual([]);
  expect((await api(page, "/system/model-selection")).value).toEqual(
    initialSelection.value,
  );
  const vector = await openPicker(page, "选择向量模型（Embedding）");
  await search(page, vector.panel, embedding.remoteID);
  await expect(vector.panel.getByRole("option")).toHaveCount(1);
  await option(page, vector.panel, embedding.display).click();
  await expect(vector.panel).toHaveCount(0);
  for (const label of [
    "向量模型（Embedding）",
    "总结模型（Chat）",
    "重排模型（Rerank）",
    "图像生成模型（Image Generation）",
  ])
    await expect(
      page.getByRole("button", {
        name: new RegExp(`^选择${label}(?:，当前 .*)?$`),
      }),
    ).toHaveCount(1);
  expect(writes).toEqual([]);
  const save = page.getByRole("button", { name: "保存", exact: true });
  await expect(save).toHaveCount(1);
  await save.click();
  await expect(page.getByText("已保存。", { exact: true })).toBeVisible();
  expect(writes).toEqual(["/api/v1/system/model-selection"]);
  await expect(save).toBeDisabled();
  page.off("request", recordWrite);
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

  await page.goto(`${owned.origin}/system/models/${chat.provider}`, {
    waitUntil: "domcontentloaded",
  });
  const row = page
    .getByRole("table", { name: "供应商模型", exact: true })
    .getByRole("row")
    .filter({
      has: page.getByRole("cell", { name: chat.remoteID, exact: true }),
    });
  const editor = page.getByRole("dialog", { name: "编辑模型", exact: true });
  const deleteTrigger = editor.getByRole("button", {
    name: "删除模型",
    exact: true,
  });
  const openDelete = async () => {
    if (!(await editor.isVisible()))
      await row.getByRole("button", { name: "编辑", exact: true }).click();
    await deleteTrigger.click();
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
  await expect(deleteTrigger).toBeFocused();
  expect((await api(page, `/system/models/${chat.model}`)).status).toBe(200);
  await openDelete();
  deletion = page.getByRole("dialog", { name: "删除模型", exact: true });
  const replacing = await openPicker(page, "选择替代模型");
  await search(page, replacing.panel, replacement.remoteID);
  await expect(replacing.panel.getByRole("option")).toHaveCount(1);
  await option(page, replacing.panel, replacement.display).click();
  await expect(replacing.panel).toHaveCount(0);
  expect((await api(page, `/system/models/${chat.model}`)).status).toBe(200);
  await deletion.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(
    deletion.getByText("删除已完成。", { exact: true }),
  ).toBeVisible();
  await deletion.getByRole("button", { name: "完成", exact: true }).click();
  expect((await api(page, `/system/models/${chat.model}`)).status).toBe(404);
  const finalPlatform = await api(page, "/system/model-selection");
  expect(finalPlatform.status).toBe(200);
  expect(finalPlatform.value.configured.summary).toBe(replacement.model);
  expect(finalPlatform.value.configured.embedding).toBe(embedding.model);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
});
