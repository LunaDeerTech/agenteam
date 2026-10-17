const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  providerStats,
} = require("./owned-fixture.cjs");

test("real Vue saves named reasoning values and reloads the canonical capability state", async ({
  page,
  owned,
}) => {
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const suffix = randomUUID().slice(0, 8);
  const provider = await api(page, "/system/model-providers", "POST", {
    input: {
      name: `Reasoning UI ${suffix}`,
      protocol: "openai-compatible",
      base_url: owned.provider_base_url,
      enabled: true,
      credential_ref: null,
      options: {},
    },
  });
  expect(provider.status).toBe(200);
  const providerID = provider.value.resource_id;
  const modelID = `ui-reasoning-${suffix}`;
  const writes = [];
  page.on("request", (request) => {
    if (
      /^\/api\/v1\/system\/models(?:\/[0-9a-f-]+)?$/.test(
        new URL(request.url()).pathname,
      ) &&
      ["POST", "PUT"].includes(request.method())
    )
      writes.push(request.postDataJSON());
  });
  await page.goto(`${owned.origin}/system/models/${providerID}`, {
    waitUntil: "domcontentloaded",
  });
  await page.getByRole("button", { name: "添加模型", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "添加模型", exact: true });
  await dialog
    .getByLabel("模型名称", { exact: true })
    .fill(`Reasoning UI ${suffix}`);
  await dialog.getByLabel("模型 ID", { exact: true }).fill(modelID);
  await dialog
    .getByRole("checkbox", {
      name: "支持结构化输出（JSON Schema）",
      exact: true,
    })
    .check();
  await dialog.getByRole("checkbox", { name: "支持思考", exact: true }).check();
  await dialog.getByRole("button", { name: "添加等级", exact: true }).click();
  await dialog.getByLabel("等级名称 1", { exact: true }).fill(" 深入思考 ");
  await dialog.getByLabel("等级值 1", { exact: true }).fill("vendor-depth");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByText("模型已保存。", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();

  const listed = await api(page, `/system/models?provider_id=${providerID}`);
  expect(listed.status).toBe(200);
  const model = listed.value.items.find(
    (item) => item.input.provider_model_id === modelID,
  );
  expect(Boolean(model)).toBe(true);
  const path = `/system/models/${model.id}`;
  const saved = await api(page, path);
  expect(saved.status).toBe(200);
  expect(saved.value.input.capabilities.reasoning).toBe(true);
  expect(saved.value.input.capabilities.reasoning_efforts).toEqual([
    { name: "深入思考", value: "vendor-depth" },
  ]);
  expect(saved.value.input.capabilities.structured_output_modes).toEqual([
    "text",
    "json_schema",
  ]);
  expect(saved.value.input.capabilities.input_modalities).toEqual(["text"]);
  expect(saved.value.input.capabilities.output_modalities).toEqual(["text"]);
  expect(writes).toHaveLength(1);
  expect(writes[0].input.capabilities.reasoning_efforts).toEqual(
    saved.value.input.capabilities.reasoning_efforts,
  );

  await page.reload({ waitUntil: "domcontentloaded" });
  await page
    .getByRole("table", { name: "供应商模型", exact: true })
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name: modelID, exact: true }) })
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "编辑模型", exact: true });
  await expect(dialog.getByLabel("等级名称 1", { exact: true })).toHaveValue(
    "深入思考",
  );
  await expect(dialog.getByLabel("等级值 1", { exact: true })).toHaveValue(
    "vendor-depth",
  );
  await expect(
    dialog.getByRole("checkbox", {
      name: "支持结构化输出（JSON Schema）",
      exact: true,
    }),
  ).toBeChecked();
  await dialog
    .getByRole("checkbox", { name: "支持思考", exact: true })
    .uncheck();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByText("模型已保存。", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  const disabled = await api(page, path);
  expect(disabled.status).toBe(200);
  expect(disabled.value.version).toBe(String(BigInt(saved.value.version) + 1n));
  expect(disabled.value.input.capabilities.reasoning).toBe(false);
  expect(disabled.value.input.capabilities.reasoning_efforts).toEqual([]);
  expect(writes).toHaveLength(2);
  expect(writes[1].expected_version).toBe(saved.value.version);
  expect(writes[1].input.capabilities.reasoning_efforts).toEqual([]);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
});
