const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  closeSession,
  providerStats,
} = require("./owned-fixture.cjs");

function modelInput(name, providerModelID) {
  return {
    name,
    provider_model_id: providerModelID,
    type: "chat",
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
      output_modalities: ["text"],
      reasoning_efforts: [],
      structured_output_modes: ["json_schema"],
      context_length: null,
      max_output: null,
      embedding: null,
    },
  };
}

test("owned global model search pages across suppliers without exposing provider secrets", async ({
  page,
  owned,
}) => {
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const marker = `picker-${randomUUID().slice(0, 8)}`;
  const credential = await api(page, "/system/model-credentials", "POST", {
    value: owned.provider_credential,
  });
  expect(credential.status).toBe(200);
  const expected = new Map();
  for (const suffix of ["a", "b"]) {
    const name = `${marker} supplier ${suffix}`;
    const created = await api(page, "/system/model-providers", "POST", {
      input: {
        name,
        protocol: "openai-compatible",
        base_url: owned.provider_base_url,
        enabled: true,
        credential_ref: credential.value.credential_id,
        options: {},
      },
    });
    expect(created.status).toBe(200);
    const model = await api(page, "/system/models", "POST", {
      provider_id: created.value.resource_id,
      input: modelInput(
        `Display ${suffix} ${marker}`,
        `remote-${suffix}-${marker}`,
      ),
    });
    expect(model.status).toBe(200);
    expected.set(model.value.resource_id, {
      id: created.value.resource_id,
      name,
      protocol: "openai-compatible",
      enabled: true,
      version: created.value.version,
    });
  }

  const query = `/system/models?limit=1&q=${encodeURIComponent(marker)}`;
  const first = await api(page, query);
  expect(first.status).toBe(200);
  expect(first.value.items.length).toBe(1);
  expect(typeof first.value.next_cursor).toBe("string");
  const cursor = encodeURIComponent(first.value.next_cursor);
  // Whitespace/case changes keep the same canonical query binding.
  const second = await api(
    page,
    `/system/models?limit=1&q=${encodeURIComponent(`  ${marker.toUpperCase()}  `)}&cursor=${cursor}`,
  );
  expect(second.status).toBe(200);
  expect(second.value.items.length).toBe(1);
  expect(second.value.next_cursor).toBeNull();
  const items = [...first.value.items, ...second.value.items];
  expect(new Set(items.map((item) => item.id)).size).toBe(2);
  expect(new Set(items.map((item) => item.provider_id)).size).toBe(2);
  for (const item of items) {
    expect(expected.has(item.id)).toBe(true);
    expect(item.provider).toEqual(expected.get(item.id));
    expect(item.provider.id === item.provider_id).toBe(true);
  }
  const encoded = JSON.stringify(items);
  for (const privateValue of [
    owned.provider_credential,
    credential.value.credential_id,
    owned.provider_base_url,
  ])
    expect(encoded.includes(privateValue)).toBe(false);

  const filtered = await api(
    page,
    `/system/models?q=${encodeURIComponent(`remote-a-${marker}`)}`,
  );
  expect(filtered.status).toBe(200);
  expect(filtered.value.items.length).toBe(1);
  expect(filtered.value.items[0].input.provider_model_id).toBe(
    `remote-a-${marker}`,
  );
  for (const path of [
    `${query}&provider_id=${items[0].provider_id}&cursor=${cursor}`,
    `/system/models?limit=1&q=${marker}-changed&cursor=${cursor}`,
    `/system/model-providers?limit=1&cursor=${cursor}`,
  ]) {
    const rejected = await api(page, path);
    expect(rejected.status).toBe(400);
    expect(rejected.value.code).toBe("CURSOR_INVALID");
  }
  expect((await providerStats(page, owned)).requests).toBe(before.requests);
  await closeSession(page);
  expect((await api(page, query)).status).toBe(401);
  expect(await loginHTTP(page, owned.regular_credentials)).toBe(200);
  const forbidden = await api(page, query);
  expect(forbidden.status).toBe(403);
  expect(forbidden.value.code).toBe("FORBIDDEN");
});
