const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
  providerStats,
} = require("./owned-fixture.cjs");

function input(providerModel, type = "chat") {
  return {
    name: `Owned management ${randomUUID().slice(0, 8)}`,
    provider_model_id: providerModel,
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
      structured_output_modes: type === "chat" ? ["json_schema"] : [],
      context_length: null,
      max_output: null,
    },
  };
}

test("owned management discovery and test reach real TLS provider once per command", async ({
  page,
  owned,
}) => {
  expect(process.env.NODE_EXTRA_CA_CERTS === owned.certificate_ca_file).toBe(
    true,
  );
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const capabilities = await api(page, "/system/model-capabilities");
  expect(capabilities.status).toBe(200);
  expect(
    capabilities.value.protocols.some(
      (item) =>
        item.protocol === "openai-compatible" && item.discovery && item.test,
    ),
  ).toBe(true);
  const credential = await api(page, "/system/model-credentials", "POST", {
    value: owned.provider_credential,
  });
  expect(credential.status).toBe(200);
  const provider = await api(page, "/system/model-providers", "POST", {
    input: {
      name: `Discovery provider ${randomUUID().slice(0, 8)}`,
      protocol: "openai-compatible",
      base_url: owned.provider_base_url,
      enabled: true,
      credential_ref: credential.value.credential_id,
      options: {},
    },
  });
  expect(provider.status).toBe(200);
  const providerID = provider.value.resource_id;
  const before = await providerStats(page, owned);
  const discoveryBody = { expected_version: provider.value.version };
  const discoveryKey = randomUUID();
  const path = `/system/model-providers/${providerID}/discovery`;
  const discovered = await api(page, path, "POST", discoveryBody, {
    key: discoveryKey,
  });
  expect(discovered.status).toBe(200);
  expect(
    discovered.value.outcome === "succeeded" &&
      discovered.value.reason === null,
  ).toBe(true);
  expect(
    discovered.value.items.some(
      (item) => item.provider_model_id === "owned-chat",
    ),
  ).toBe(true);
  expect(
    JSON.stringify(discovered.value).includes(owned.provider_credential),
  ).toBe(false);
  expect((await providerStats(page, owned)).discovery).toBe(
    before.discovery + 1,
  );
  const replay = await api(page, path, "POST", discoveryBody, {
    key: discoveryKey,
  });
  expect(replay.status).toBe(200);
  expect(
    JSON.stringify(replay.value) === JSON.stringify(discovered.value),
  ).toBe(true);
  expect((await providerStats(page, owned)).discovery).toBe(
    before.discovery + 1,
  );

  const model = await api(page, "/system/models", "POST", {
    provider_id: providerID,
    input: input("owned-chat"),
  });
  expect(model.status).toBe(200);
  const testPath = `/system/models/${model.value.resource_id}/test`;
  const testBody = {
    expected_version: model.value.version,
    provider_version: provider.value.version,
  };
  const testKey = randomUUID();
  const tested = await api(page, testPath, "POST", testBody, { key: testKey });
  expect(tested.status).toBe(200);
  expect(
    tested.value.outcome === "succeeded" && tested.value.reason === null,
  ).toBe(true);
  expect((await providerStats(page, owned)).chat).toBe(before.chat + 1);
  expect((await providerStats(page, owned)).authorized).toBe(
    before.authorized + 2,
  );
  const testReplay = await api(page, testPath, "POST", testBody, {
    key: testKey,
  });
  expect(testReplay.status).toBe(200);
  expect(
    JSON.stringify(testReplay.value) === JSON.stringify(tested.value),
  ).toBe(true);
  expect((await providerStats(page, owned)).chat).toBe(before.chat + 1);

  const rejected = await api(page, "/system/models", "POST", {
    provider_id: providerID,
    input: input("owned-rejected"),
  });
  expect(rejected.status).toBe(200);
  const failure = await api(
    page,
    `/system/models/${rejected.value.resource_id}/test`,
    "POST",
    {
      expected_version: rejected.value.version,
      provider_version: provider.value.version,
    },
  );
  expect(failure.status).toBe(200);
  expect(
    failure.value.outcome === "failed" &&
      failure.value.reason === "upstream_rejected",
  ).toBe(true);
  expect(
    JSON.stringify(failure.value).includes("owned bounded rejection"),
  ).toBe(false);
  expect((await providerStats(page, owned)).chat).toBe(before.chat + 2);

  // A real accepted response is lost at the browser boundary. Only an explicit
  // replay of the frozen key/body is used to recover; no second provider call.
  let dropped = false;
  const absolute = `${owned.origin}/api/v1${testPath}`;
  await page.route(absolute, async (route) => {
    try {
      const response = await route.fetch();
      if (response.status() !== 200) throw new Error();
      await response.body();
      dropped = true;
      await route.abort("failed");
    } catch {
      await route.abort("failed").catch(() => {});
      throw new Error("OWNED_MODEL_ACCEPTED_RESPONSE_LOSS_FAILED");
    }
  });
  const lostKey = randomUUID();
  let unknown = false;
  try {
    await api(page, testPath, "POST", testBody, { key: lostKey });
  } catch {
    unknown = true;
  }
  await page.unroute(absolute);
  expect(unknown && dropped).toBe(true);
  expect((await providerStats(page, owned)).chat).toBe(before.chat + 3);
  const recovered = await api(page, testPath, "POST", testBody, {
    key: lostKey,
  });
  expect(recovered.status).toBe(200);
  expect(recovered.value.outcome === "succeeded").toBe(true);
  expect((await providerStats(page, owned)).chat).toBe(before.chat + 3);
});
