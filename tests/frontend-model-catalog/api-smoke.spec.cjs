const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  closeSession,
  providerStats,
} = require("./owned-fixture.cjs");

function modelInput(name, type = "chat") {
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
      output_modalities: [type === "embedding" ? "vector" : "text"],
      reasoning_efforts: [],
      structured_output_modes: type === "chat" ? ["json_schema"] : [],
      context_length: null,
      max_output: null,
    },
  };
}

// This first chain verifies the real HTTP/PG/Secret composition before the new
// Vue pages are ready. The separate UI journey must still exercise its controls.
test("owned production catalog CRUD, credential and selection wire", async ({
  page,
  owned,
}) => {
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const providerBefore = await providerStats(page, owned);
  const suffix = randomUUID().slice(0, 8);
  const credential = await api(page, "/system/model-credentials", "POST", {
    value: owned.provider_credential,
  });
  expect(credential.status).toBe(200);
  expect(
    credential.value.purpose === "model" && credential.value.deleted === false,
  ).toBe(true);
  const input = {
    name: `Owned provider ${suffix}`,
    protocol: "openai-chat-completions",
    base_url: owned.provider_base_url,
    enabled: true,
    credential_ref: credential.value.credential_id,
    options: {},
  };
  const key = randomUUID();
  const created = await api(
    page,
    "/system/model-providers",
    "POST",
    { input },
    { key },
  );
  expect(created.status).toBe(200);
  const replay = await api(
    page,
    "/system/model-providers",
    "POST",
    { input },
    { key },
  );
  expect(replay.status).toBe(200);
  expect(JSON.stringify(created.value) === JSON.stringify(replay.value)).toBe(
    true,
  );
  const providerPath = `/system/model-providers/${created.value.resource_id}`;
  const before = await api(page, providerPath);
  expect(before.status).toBe(200);
  expect(
    before.value.input.credential_ref === credential.value.credential_id,
  ).toBe(true);
  expect(JSON.stringify(before.value).includes(owned.provider_credential)).toBe(
    false,
  );
  const renamed = { ...input, name: `Renamed provider ${suffix}` };
  expect(
    (
      await api(page, providerPath, "PUT", {
        expected_version: before.value.version,
        input: renamed,
      })
    ).status,
  ).toBe(200);
  expect(
    (
      await api(page, providerPath, "PUT", {
        expected_version: before.value.version,
        input,
      })
    ).status,
  ).toBe(409);
  expect(
    (await api(page, providerPath)).value.input.name === renamed.name,
  ).toBe(true);

  const second = await api(page, "/system/model-providers", "POST", {
    input: {
      ...input,
      name: `Owned embedding ${suffix}`,
      protocol: "openai-embeddings",
    },
  });
  expect(second.status).toBe(200);
  const firstPage = await api(page, "/system/model-providers?limit=1");
  expect(firstPage.status).toBe(200);
  expect(
    firstPage.value.items.length === 1 &&
      typeof firstPage.value.next_cursor === "string",
  ).toBe(true);
  const nextPage = await api(
    page,
    `/system/model-providers?limit=1&cursor=${encodeURIComponent(firstPage.value.next_cursor)}`,
  );
  expect(nextPage.status).toBe(200);
  expect(firstPage.value.items[0].id !== nextPage.value.items[0].id).toBe(true);
  const models = [];
  for (const [provider, type, name] of [
    [created.value.resource_id, "chat", "First chat"],
    [created.value.resource_id, "chat", "Replacement chat"],
    [second.value.resource_id, "embedding", "Embedding"],
  ]) {
    const response = await api(page, "/system/models", "POST", {
      provider_id: provider,
      input: modelInput(`${name} ${suffix}`, type),
    });
    expect(response.status).toBe(200);
    models.push(response.value.resource_id);
  }
  const selection = await api(page, "/system/model-selection");
  expect(selection.status).toBe(200);
  expect(
    (
      await api(page, "/system/model-selection", "PUT", {
        id: selection.value.id,
        expected_version: selection.value.version,
        memory: models[0],
        embedding: models[2],
        reranker: null,
        image: null,
      })
    ).status,
  ).toBe(200);
  const impact = await api(page, `/system/models/${models[0]}/deletion-impact`);
  expect(impact.status).toBe(200);
  expect(
    impact.value.replacement_requirement === "required" &&
      impact.value.delete_blocker === null,
  ).toBe(true);
  expect(
    (
      await api(page, `/system/models/${models[0]}`, "DELETE", {
        expected_version: impact.value.version,
        replacement: null,
      })
    ).status,
  ).toBe(409);
  const deleted = await api(page, `/system/models/${models[0]}`, "DELETE", {
    expected_version: impact.value.version,
    replacement: models[1],
  });
  expect(deleted.status).toBe(200);
  expect(
    (await api(page, "/system/model-selection")).value.configured.memory ===
      models[1],
  ).toBe(true);

  const metadata = await api(
    page,
    `/system/model-credentials/${credential.value.credential_id}`,
  );
  expect(metadata.status).toBe(200);
  expect(
    Object.keys(metadata.value).sort().join() ===
      "credential_id,purpose,version",
  ).toBe(true);
  expect(
    JSON.stringify(metadata.value).includes(owned.provider_credential),
  ).toBe(false);
  const observed = await providerStats(page, owned);
  expect(observed.requests).toBe(providerBefore.requests);
});

test("owned model APIs enforce current role and CSRF without changing state", async ({
  page,
  owned,
}) => {
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await api(page, "/system/model-selection");
  expect(before.status).toBe(200);
  const denied = await api(
    page,
    "/system/model-providers",
    "POST",
    {
      input: {
        name: "Rejected CSRF",
        protocol: "openai-chat-completions",
        base_url: owned.provider_base_url,
        enabled: true,
        credential_ref: null,
        options: {},
      },
    },
    { omitCSRF: true },
  );
  expect(denied.status).toBe(403);
  expect(
    (await api(page, "/system/model-selection")).value.version ===
      before.value.version,
  ).toBe(true);
  await closeSession(page);
  expect(await loginHTTP(page, owned.regular_credentials)).toBe(200);
  const identity = await api(page, "/session");
  expect(identity.status).toBe(200);
  for (const path of [
    "/system/model-providers",
    "/system/models",
    "/system/model-selection",
  ])
    expect((await api(page, path)).status).toBe(403);
  const after = await api(page, "/session");
  expect(after.status).toBe(200);
  expect(
    after.value.user.id === identity.value.user.id &&
      after.value.session.id === identity.value.session.id,
  ).toBe(true);
});
