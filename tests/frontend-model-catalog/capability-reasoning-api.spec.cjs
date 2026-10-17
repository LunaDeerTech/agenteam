const { randomUUID } = require("node:crypto");
const {
  test,
  expect,
  api,
  loginHTTP,
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
      reasoning: type === "chat",
      input_modalities: type === "chat" ? ["text", "image", "file"] : ["text"],
      output_modalities: [type === "chat" ? "text" : "vector"],
      reasoning_efforts:
        type === "chat" ? [{ name: "深入思考", value: "vendor-depth" }] : [],
      structured_output_modes: type === "chat" ? ["text", "json_schema"] : [],
      context_length: null,
      max_output: null,
      embedding:
        type === "embedding"
          ? { dimensions: "3", dimensions_mode: "fixed" }
          : null,
    },
  };
}

test("typed reasoning survives real API persistence and keeps capability gates", async ({
  page,
  owned,
}) => {
  expect(await loginHTTP(page, owned.credentials)).toBe(200);
  const before = await providerStats(page, owned);
  const credential = await api(page, "/system/model-credentials", "POST", {
    value: owned.provider_credential,
  });
  expect(credential.status).toBe(200);
  const provider = await api(page, "/system/model-providers", "POST", {
    input: {
      name: `Reasoning review ${randomUUID().slice(0, 8)}`,
      protocol: "openai-compatible",
      base_url: owned.provider_base_url,
      enabled: true,
      credential_ref: credential.value.credential_id,
      options: {},
    },
  });
  expect(provider.status).toBe(200);
  const providerID = provider.value.resource_id;
  const input = modelInput("Typed reasoning review");
  input.capabilities.reasoning_efforts.push({
    name: "思".repeat(64),
    value: "v".repeat(32),
  });
  const body = { provider_id: providerID, input };
  const key = randomUUID();
  const created = await api(page, "/system/models", "POST", body, { key });
  expect(created.status).toBe(200);
  const path = `/system/models/${created.value.resource_id}`;
  const first = await api(page, path);
  expect(first.status).toBe(200);
  expect(first.value.input.capabilities).toEqual(input.capabilities);
  const replay = await api(page, "/system/models", "POST", body, { key });
  expect(replay.status).toBe(200);
  expect(replay.value).toEqual(created.value);
  expect((await api(page, path)).value.version).toBe(first.value.version);
  expect(JSON.stringify(first.value).includes(owned.provider_credential)).toBe(
    false,
  );

  const invalid = [
    ["legacy string options", (c) => (c.reasoning_efforts = ["high"])],
    [
      "duplicate names",
      (c) =>
        (c.reasoning_efforts = [
          { name: "相同", value: "one" },
          { name: "相同", value: "two" },
        ]),
    ],
    [
      "duplicate values",
      (c) =>
        (c.reasoning_efforts = [
          { name: "一级", value: "same" },
          { name: "二级", value: "same" },
        ]),
    ],
    ["disabled reasoning with options", (c) => (c.reasoning = false)],
    [
      "control in display name",
      (c) => (c.reasoning_efforts[0].name = "bad\u0085name"),
    ],
    [
      "65 Unicode code points",
      (c) => (c.reasoning_efforts[0].name = "思".repeat(65)),
    ],
    [
      "untrimmed display name",
      (c) => (c.reasoning_efforts[0].name = " 深入思考 "),
    ],
    [
      "unsafe protocol value",
      (c) => (c.reasoning_efforts[0].value = "high value"),
    ],
    [
      "33 protocol bytes",
      (c) => (c.reasoning_efforts[0].value = "v".repeat(33)),
    ],
    [
      "33 declared options",
      (c) =>
        (c.reasoning_efforts = Array.from({ length: 33 }, (_, i) => ({
          name: `等级${i}`,
          value: `v${i}`,
        }))),
    ],
    [
      "undeclared option key",
      (c) => (c.reasoning_efforts[0].parameter = "free_override"),
    ],
  ];
  for (const [label, mutate] of invalid) {
    const bad = structuredClone(input);
    mutate(bad.capabilities);
    const rejected = await api(page, path, "PUT", {
      expected_version: first.value.version,
      input: bad,
    });
    expect(rejected.status, label).toBe(400);
  }
  expect((await api(page, path)).value).toEqual(first.value);

  const embeddingInput = modelInput("Typed embedding review", "embedding");
  const embedding = await api(page, "/system/models", "POST", {
    provider_id: providerID,
    input: embeddingInput,
  });
  expect(embedding.status).toBe(200);
  for (const [label, mutate] of [
    ["embedding reasoning", (c) => (c.reasoning = true)],
    [
      "embedding JSON Schema",
      (c) => (c.structured_output_modes = ["json_schema"]),
    ],
    ["embedding image input", (c) => (c.input_modalities = ["text", "image"])],
  ]) {
    const bad = structuredClone(embeddingInput);
    mutate(bad.capabilities);
    const rejected = await api(
      page,
      `/system/models/${embedding.value.resource_id}`,
      "PUT",
      {
        expected_version: embedding.value.version,
        input: bad,
      },
    );
    expect(rejected.status, label).toBe(422);
  }

  const plainInput = modelInput("Schema gate review");
  plainInput.capabilities.structured_output_modes = ["text"];
  plainInput.capabilities.reasoning_efforts = [];
  const plain = await api(page, "/system/models", "POST", {
    provider_id: providerID,
    input: plainInput,
  });
  expect(plain.status).toBe(200);
  const selection = await api(page, "/system/model-selection");
  expect(selection.status).toBe(200);
  const selectionBody = {
    id: selection.value.id,
    expected_version: selection.value.version,
    embedding: embedding.value.resource_id,
    summary: plain.value.resource_id,
    reranker: null,
    image: null,
  };
  expect(
    (await api(page, "/system/model-selection", "PUT", selectionBody)).status,
  ).toBe(422);
  expect((await api(page, "/system/model-selection")).value).toEqual(
    selection.value,
  );
  selectionBody.summary = created.value.resource_id;
  const selected = await api(
    page,
    "/system/model-selection",
    "PUT",
    selectionBody,
  );
  expect(selected.status).toBe(200);
  expect(
    (await api(page, "/system/model-selection")).value.configured.summary,
  ).toBe(created.value.resource_id);
  expect((await providerStats(page, owned)).requests).toBe(before.requests);

  // Management test has no effort selector. Its real success/one-call evidence
  // is separate from exact wire-field assertions in the Go snapshot/wire test.
  for (const model of [
    first.value,
    (await api(page, `/system/models/${plain.value.resource_id}`)).value,
  ]) {
    const result = await api(page, `/system/models/${model.id}/test`, "POST", {
      expected_version: model.version,
      provider_version: provider.value.version,
    });
    expect(result.status).toBe(200);
    expect(result.value.outcome).toBe("succeeded");
  }
  const after = await providerStats(page, owned);
  expect(after.requests).toBe(before.requests + 2);
  expect(after.chat).toBe(before.chat + 2);
  expect(after.authorized).toBe(before.authorized + 2);
});
