import { Blob as NodeBlob } from "node:buffer";
import { mount } from "@vue/test-utils";
import { h } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogModel, Provider } from "../src/system/model-catalog";
const id = "019a2222-2222-7222-8222-222222222222";
const session = {
  user: {
    id,
    email: "test@example.invalid",
    username: "test",
    display_name: "Test",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: "019a1111-1111-7111-8111-111111111111",
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-18T12:00:00.000000Z",
    idle_expires_at: "2026-10-17T18:00:00.000000Z",
  },
  csrf_token: "synthetic",
};
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status < 400 ? "application/json" : "application/problem+json",
    },
  });
let cleanup: (() => void) | undefined;
beforeEach(() => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  localStorage.clear();
});
afterEach(() => {
  cleanup?.();
  cleanup = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});
it("keeps a found historical receipt uncertain until the original execute accepts the same body and key", async () => {
  const writes: { body: unknown; key: string | null }[] = [];
  const receipt = {
    kind: "provider.update",
    resource_id: id,
    version: "9007199254740994",
    affected_references: "0",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith("/session")) return json(session);
      if (url.endsWith("/model-commands/lookup"))
        return json({ found: true, receipt });
      writes.push({
        body: init.body,
        key: new Headers(init.headers).get("Idempotency-Key"),
      });
      if (writes.length === 1) throw new TypeError("lost response");
      return json(receipt);
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const { catalogOperation } = await import("../src/system/model-catalog");
  const { useCatalogCommand } = await import("../src/system/useCatalogCommand");
  let state!: ReturnType<typeof useCatalogCommand<unknown>>;
  const wrapper = mount({
    setup() {
      state = useCatalogCommand();
      return () => h("div");
    },
  });
  cleanup = () => wrapper.unmount();
  const done = vi.fn();
  const body = {
    expected_version: "9007199254740993",
    input: { name: "same original draft" },
  };
  await state.submit(
    () =>
      catalogOperation(
        `/system/model-providers/${id}`,
        "provider.update",
        body,
        "PUT",
        id,
      ),
    done,
  );
  expect(state.uncertain.value).toBe(true);
  await state.observe();
  expect(state.observation.value).toBe("found");
  expect(state.uncertain.value).toBe(true);
  expect(done).not.toHaveBeenCalled();
  expect(writes).toHaveLength(1);
  await state.retry();
  expect(done).toHaveBeenCalledExactlyOnceWith(receipt);
  expect(state.uncertain.value).toBe(false);
  expect(writes[1]).toEqual(writes[0]);
});
it("rejects a safe credential projection containing secret material and a receipt for a different version", async () => {
  const { isCredentialMetadata, isCredentialReceipt } =
    await import("../src/system/model-catalog");
  expect(
    isCredentialMetadata({
      credential_id: id,
      purpose: "model",
      version: "1",
      value: "must not publish",
    }),
  ).toBe(false);
  expect(
    isCredentialReceipt({
      credential_id: id,
      purpose: "model",
      version: 1,
      deleted: false,
    }),
  ).toBe(false);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("/session")
        ? json(session)
        : json({
            kind: "provider.update",
            resource_id: id,
            version: "3",
            affected_references: "0",
          }),
    ),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const { catalogOperation } = await import("../src/system/model-catalog");
  const operation = catalogOperation(
    `/system/model-providers/${id}`,
    "provider.update",
    { expected_version: "3", input: {} },
    "PUT",
    id,
  );
  await expect(operation.command.run()).rejects.toMatchObject({
    code: "INVALID_RESPONSE",
    commitState: "unknown",
  });
  operation.command.release();
});
it("limits provider filtering and expanded body budgets to exact model request paths", async () => {
  const fetcher = vi.fn(async (_url: string) => json({ accepted: true }));
  vi.stubGlobal("fetch", fetcher);
  const { accountRequest } = await import("../src/account/transport");
  await accountRequest("/system/models", {
    query: { provider_id: id, limit: 10 },
  });
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    `/api/v1/system/models?provider_id=${id}&limit=10`,
  );
  for (const path of ["/me", "/system/model-providers"])
    await expect(
      accountRequest(path, { query: { provider_id: id } }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  const body = JSON.stringify({ value: "x".repeat(20000) });
  await accountRequest("/system/model-credentials", { method: "POST", body });
  await expect(
    accountRequest("/me", { method: "PUT", body }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  await expect(
    accountRequest("/system/model-credentials", { method: "GET", body }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  await expect(
    accountRequest("/system/model-credentials/not-a-uuid", {
      method: "PUT",
      body,
    }),
  ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it("aborts only the bounded model operation and replays its original identity with a fresh signal", async () => {
  const requests: {
    key: string | null;
    body: unknown;
    signal: AbortSignal | null | undefined;
  }[] = [];
  let started!: () => void;
  const firstStarted = new Promise<void>((resolve) => (started = resolve));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      if (url.endsWith("/session")) return json(session);
      requests.push({
        key: new Headers(init.headers).get("Idempotency-Key"),
        body: init.body,
        signal: init.signal,
      });
      if (requests.length === 1) {
        started();
        return new Promise<Response>((_resolve, reject) =>
          init.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          ),
        );
      }
      return json({
        operation_id: "019a3333-3333-7333-8333-333333333333",
        provider_id: id,
        provider_version: "2",
        outcome: "unknown",
        reason: "cancelled",
        items: [],
        duration_ms: 100,
        created_at: "2026-10-17T01:00:00.000000Z",
      });
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const { discoveryCommand } = await import("../src/system/model-operations");
  const command = discoveryCommand({
    id,
    version: "2",
    input: {
      name: "Provider",
      protocol: "openai-chat-completions",
      base_url: "https://example.invalid",
      enabled: true,
      credential_ref: null,
      options: {},
    },
    created_at: "2026-10-17T01:00:00.000000Z",
    updated_at: "2026-10-17T01:00:00.000000Z",
  });
  const first = command.run();
  await firstStarted;
  command.cancel();
  await expect(first).rejects.toMatchObject({ commitState: "unknown" });
  await expect(command.run()).resolves.toMatchObject({
    outcome: "unknown",
    reason: "cancelled",
  });
  expect(requests[1]?.key).toBe(requests[0]?.key);
  expect(requests[1]?.body).toBe(requests[0]?.body);
  expect(requests[0]?.signal?.aborted).toBe(true);
  expect(requests[1]?.signal?.aborted).toBe(false);
  const { accountRequest } = await import("../src/account/transport");
  await expect(
    accountRequest("/sessions/login", {
      method: "POST",
      modelOperationSignal: () => new AbortController().signal,
    }),
  ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  expect(requests).toHaveLength(2);
});

describe("model operation budgets", () => {
  const modelId = "019a4444-4444-7444-8444-444444444444";
  const createdAt = "2026-10-17T01:00:00.000000Z";
  const provider: Provider = {
    id,
    version: "2",
    input: {
      name: "Budget Provider",
      protocol: "openai-compatible",
      base_url: "https://owned.invalid/v1",
      enabled: true,
      credential_ref: null,
      options: {},
    },
    created_at: createdAt,
    updated_at: createdAt,
  };
  const model = (type: "chat" | "image_generation"): CatalogModel => ({
    id: modelId,
    provider_id: id,
    version: "3",
    input: {
      notes: "Budget Model",
      provider_model_id: "budget-model",
      type,
      enabled: true,
      parameters: {},
      request_overwrite: {},
      header_overwrite: {},
      capabilities: {
        embedding: null,
        input_modalities: ["text"],
        output_modalities: [type === "chat" ? "text" : "image"],
        tool_calls: false,
        parallel_tool_calls: false,
        streaming: false,
        reasoning: false,
        reasoning_toggle_mode: null,
        reasoning_efforts: [],
        structured_output_modes: type === "chat" ? ["text"] : [],
        context_length: null,
        max_output: null,
      },
    },
    created_at: createdAt,
    updated_at: createdAt,
  });
  const result = (duration_ms: number) => ({
    operation_id: "019a3333-3333-7333-8333-333333333333",
    provider_id: id,
    provider_version: "2",
    outcome: "succeeded" as const,
    reason: null,
    duration_ms,
    created_at: createdAt,
  });
  const testResult = (duration: number, answer_text: string | null = null) => ({
    ...result(duration),
    model_id: modelId,
    model_version: "3",
    answer_text,
    answer_truncated: false,
  });
  type Request = {
    path: string;
    key: string | null;
    body: RequestInit["body"];
    signal: AbortSignal;
  };
  let requests: Request[];
  let respond: (request: Request) => Promise<Response>;
  let started: (() => void) | undefined;
  const nextRequest = () =>
    new Promise<void>((resolve) => {
      started = resolve;
    });
  let operations: typeof import("../src/system/model-operations");
  let accountRequest: (typeof import("../src/account/transport"))["accountRequest"];
  let owners: { release: () => void }[];

  beforeEach(async () => {
    requests = [];
    owners = [];
    started = undefined;
    respond = (request) =>
      new Promise<Response>((_resolve, reject) => {
        request.signal.addEventListener(
          "abort",
          () => reject(new DOMException("Aborted", "AbortError")),
          { once: true },
        );
      });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        if (url.endsWith("/session")) return json(session);
        const request = {
          path: url,
          key: new Headers(init.headers).get("Idempotency-Key"),
          body: init.body,
          signal: init.signal!,
        };
        requests.push(request);
        started?.();
        started = undefined;
        return respond(request);
      }),
    );
    await (await import("../src/account/session")).useSession().refresh();
    operations = await import("../src/system/model-operations");
    accountRequest = (await import("../src/account/transport")).accountRequest;
  });
  afterEach(() => {
    for (const owner of owners) owner.release();
    vi.useRealTimers();
  });

  it("waits 130 seconds for image tests and recovers with the same body and key after an unknown timeout", async () => {
    const command = operations.modelTestCommand(
      provider,
      model("image_generation"),
    );
    owners.push(command);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const requestStarted = nextRequest();
    let settled = false;
    const first = command.run().then(
      (value) => {
        settled = true;
        return value;
      },
      (error: unknown) => {
        settled = true;
        return error;
      },
    );
    await requestStarted;
    for (const elapsed of [30000, 95000, 4999]) {
      await vi.advanceTimersByTimeAsync(elapsed);
      expect(settled).toBe(false);
      expect(requests).toHaveLength(1);
      expect(requests[0]!.signal.aborted).toBe(false);
    }
    await vi.advanceTimersByTimeAsync(1);
    expect(await first).toMatchObject({
      code: "NETWORK_ERROR",
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    expect(command.active).toBe(true);
    expect(requests[0]!.signal.aborted).toBe(true);

    respond = async () => json(testResult(120000));
    await expect(command.run()).resolves.toMatchObject({
      outcome: "succeeded",
      duration_ms: 120000,
    });
    expect(command.confirmed).toBe(true);
    expect(requests).toHaveLength(2);
    expect(requests[1]!.path).toBe(`/api/v1/system/models/${modelId}/test`);
    expect(requests[1]!.key).toBe(requests[0]!.key);
    expect(requests[1]!.body).toBe(requests[0]!.body);
    expect(JSON.parse(String(requests[1]!.body))).toEqual({
      expected_version: "3",
      provider_version: "2",
    });
    expect(requests[1]!.signal).not.toBe(requests[0]!.signal);
    expect(requests[1]!.signal.aborted).toBe(false);
  });

  it("keeps non-image tests and discovery at 30 seconds and ordinary reads at 20 seconds", async () => {
    const test = operations.modelTestCommand(provider, model("chat"));
    const discovery = operations.discoveryCommand(provider);
    owners.push(test, discovery);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    for (const request of [
      { run: () => test.run(), budget: 30000, commitState: "unknown" },
      { run: () => discovery.run(), budget: 30000, commitState: "unknown" },
      {
        run: () => accountRequest("/me"),
        budget: 20000,
        commitState: "not_committed",
      },
    ]) {
      const requestStarted = nextRequest();
      let settled = false;
      const pending = request.run().then(
        (value) => {
          settled = true;
          return value;
        },
        (error: unknown) => {
          settled = true;
          return error;
        },
      );
      await requestStarted;
      await vi.advanceTimersByTimeAsync(request.budget - 1);
      expect(settled).toBe(false);
      expect(requests.at(-1)!.signal.aborted).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      expect(await pending).toMatchObject({
        code: "NETWORK_ERROR",
        commitState: request.commitState,
      });
      expect(requests.at(-1)!.signal.aborted).toBe(true);
    }
    expect(requests).toHaveLength(3);
  });

  it("bounds discovery receipts at 20 seconds and test receipts by their model type", async () => {
    expect(operations.isDiscoveryResult({ ...result(20000), items: [] })).toBe(
      true,
    );
    expect(operations.isDiscoveryResult({ ...result(20001), items: [] })).toBe(
      false,
    );
    expect(operations.isTestResult(testResult(120000))).toBe(true);
    expect(operations.isTestResult(testResult(120001))).toBe(false);
    respond = async () => json(testResult(20001, "Chat answer"));
    const command = operations.modelTestCommand(provider, model("chat"));
    owners.push(command);
    await expect(command.run()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    respond = async () => json(testResult(20000, "Chat answer"));
    await expect(command.run()).resolves.toMatchObject({ duration_ms: 20000 });
    expect(requests[1]!.key).toBe(requests[0]!.key);
  });

  it("rejects an extended budget outside the exact POST model test with a cancellation owner", async () => {
    const options = {
      method: "POST" as const,
      modelOperationSignal: () => new AbortController().signal,
      modelOperationTimeoutMs: 130000 as const,
    };
    for (const path of [
      "/me",
      `/system/model-providers/${id}/discovery`,
      "/system/models/not-a-uuid/test",
      `/system/models/${modelId}/test/extra`,
    ])
      await expect(accountRequest(path, options)).rejects.toMatchObject({
        code: "INVALID_REQUEST",
      });
    await expect(
      accountRequest(`/system/models/${modelId}/test`, {
        ...options,
        method: "GET",
      }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
    await expect(
      accountRequest(`/system/models/${modelId}/test`, {
        method: "POST",
        modelOperationTimeoutMs: 130000,
      }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
    expect(requests).toHaveLength(0);
  });
});

it("adds only mandatory chat text capabilities and rejects inconsistent explicit capabilities", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const draft = modelDraft("openai-compatible");
  draft.type = "chat";
  draft.notes = "Explicit";
  draft.providerModelId = "model-that-looks-capable";
  const input = modelDraftInput(draft, "openai-compatible");
  expect(input.capabilities.input_modalities).toEqual(["text"]);
  expect(input.capabilities.output_modalities).toEqual(["text"]);
  expect(input.capabilities.structured_output_modes).toEqual(["text"]);
  expect(input.capabilities.reasoning_efforts).toEqual([]);
  expect(input.capabilities.tool_calls).toBe(false);
  expect(input.capabilities.context_length).toBe(null);
  draft.parallelToolCalls = true;
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow("工具调用");
  draft.toolCalls = true;
  draft.contextLength = "200";
  draft.maxOutput = "201";
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow(
    "上下文长度",
  );
});

it("requires explicit bounded embedding dimensions and rejects missing or cross-type metadata", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const { isModelInput } = await import("../src/system/model-catalog");
  const draft = modelDraft("openai-compatible");
  draft.type = "embedding";
  draft.providerModelId = "explicit-dimensions";
  expect(draft.embeddingDimensions).toBe("");
  expect(draft.embeddingDimensionsMode).toBe("fixed");
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow("维度");
  for (const [dimensions, dimensions_mode] of [
    ["1", "fixed"],
    ["4096", "request"],
  ] as const) {
    draft.embeddingDimensions = dimensions;
    draft.embeddingDimensionsMode = dimensions_mode;
    const input = modelDraftInput(draft, "openai-compatible");
    expect(input.capabilities.embedding).toEqual({
      dimensions,
      dimensions_mode,
    });
    expect(isModelInput(input)).toBe(true);
    const restored = modelDraft("openai-compatible", input);
    expect(restored.embeddingDimensions).toBe(dimensions);
    expect(restored.embeddingDimensionsMode).toBe(dimensions_mode);
  }
  const input = modelDraftInput(draft, "openai-compatible");
  for (const dimensions of [
    "",
    "0",
    "4097",
    "01536",
    "1.5",
    " 1536",
    "1536 ",
  ]) {
    draft.embeddingDimensions = dimensions;
    expect(
      () => modelDraftInput(draft, "openai-compatible"),
      dimensions,
    ).toThrow("维度");
    expect(
      isModelInput({
        ...input,
        capabilities: {
          ...input.capabilities,
          embedding: { dimensions, dimensions_mode: "fixed" },
        },
      }),
      dimensions,
    ).toBe(false);
  }
  for (const embedding of [
    null,
    { dimensions: 1536, dimensions_mode: "fixed" },
    { dimensions: "1536" },
    { dimensions: "1536", dimensions_mode: "auto" },
  ])
    expect(
      isModelInput({
        ...input,
        capabilities: { ...input.capabilities, embedding },
      }),
    ).toBe(false);
  const { embedding: _embedding, ...missing } = input.capabilities;
  expect(isModelInput({ ...input, capabilities: missing })).toBe(false);
  for (const type of ["chat", "reranker", "image_generation"] as const) {
    draft.type = type;
    const other = modelDraftInput(draft, "openai-compatible");
    expect(other.capabilities.embedding).toBeNull();
    expect(isModelInput(other)).toBe(true);
    expect(
      isModelInput({
        ...other,
        capabilities: {
          ...other.capabilities,
          embedding: input.capabilities.embedding,
        },
      }),
    ).toBe(false);
    const { embedding: _unused, ...absent } = other.capabilities;
    expect(isModelInput({ ...other, capabilities: absent })).toBe(false);
  }
});

it("allows empty notes and preserves Unicode notes within the wire limits", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const { isModelInput } = await import("../src/system/model-catalog");
  const draft = modelDraft("openai-compatible");
  draft.type = "chat";
  draft.providerModelId = "an-upstream-id-is-not-a-note";
  const empty = modelDraftInput(draft, "openai-compatible");
  expect(empty.notes).toBe("");
  expect(empty).not.toHaveProperty("name");
  expect(isModelInput(empty)).toBe(true);
  expect(modelDraft("openai-compatible", empty).notes).toBe("");

  for (const notes of ["  一条备注\n保留原文  ", "🧠".repeat(2000)]) {
    draft.notes = notes;
    const input = modelDraftInput(draft, "openai-compatible");
    expect(input.notes).toBe(notes);
    expect(isModelInput(input)).toBe(true);
    expect(modelDraft("openai-compatible", input).notes).toBe(notes);
  }
  for (const notes of ["a".repeat(2001), "🧠".repeat(2001), "before\0after"]) {
    draft.notes = notes;
    expect(() => modelDraftInput(draft, "openai-compatible")).toThrow();
    expect(isModelInput({ ...empty, notes })).toBe(false);
  }
});

it("round-trips explicit reasoning names and wire values and rejects ambiguous or unsafe levels", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const { isReasoningEfforts } = await import("../src/system/model-catalog");
  const draft = modelDraft("openai-compatible");
  Object.assign(draft, {
    type: "chat",
    notes: "Reasoning Fixture",
    providerModelId: "reasoning-fixture",
    inputModalities: "text",
    reasoning: true,
  });
  const levels = [
    { name: "Deep", value: "HIGH" },
    { name: "deep", value: "high" },
    { name: "🧠".repeat(64), value: "vendor_fast.v2:budget-2" },
  ];
  draft.reasoningEfforts = levels;
  const input = modelDraftInput(draft, "openai-compatible");
  expect(input.capabilities.reasoning_efforts).toEqual(levels);
  expect(modelDraft("openai-compatible", input).reasoningEfforts).toEqual(
    levels,
  );

  const invalid = [
    [
      { name: "Quick", value: "low" },
      { name: "Quick", value: "high" },
    ],
    [
      { name: "Quick", value: "low" },
      { name: "Thorough", value: "low" },
    ],
    [{ name: "\u00a0Quick", value: "low" }],
    [{ name: "Quick\u009f", value: "low" }],
    [{ name: "🧠".repeat(65), value: "low" }],
    [{ name: "Quick", value: "extra effort" }],
    [{ name: "Quick", value: "x".repeat(33) }],
  ];
  for (const value of invalid) {
    expect(isReasoningEfforts(value)).toBe(false);
  }
  draft.reasoningEfforts = invalid[0]!;
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow();
  draft.reasoningEfforts = Array.from({ length: 32 }, (_, index) => ({
    name: `Level ${index}`,
    value: `level-${index}`,
  }));
  expect(
    modelDraftInput(draft, "openai-compatible").capabilities.reasoning_efforts,
  ).toHaveLength(32);
  draft.reasoningEfforts.push({ name: "Extra", value: "extra" });
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow();

  draft.reasoningEfforts = [];
  expect(
    modelDraftInput(draft, "openai-compatible").capabilities,
  ).toMatchObject({ reasoning: true, reasoning_efforts: [] });
  draft.reasoningEfforts = levels;
  draft.reasoning = false;
  expect(
    modelDraftInput(draft, "openai-compatible").capabilities,
  ).toMatchObject({ reasoning: false, reasoning_efforts: [] });
});

it("accepts the unified platform summary reference while rejecting removed platform roles", async () => {
  let ownerKind = "platform_selector";
  let role = "summary";
  let blocker: string | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/session")) return json(session);
      expect(url).toBe(`/api/v1/system/models/${id}/deletion-impact`);
      return json({
        model_id: id,
        version: "2",
        reference_count: "1",
        reference_groups: [{ owner_kind: ownerKind, role, count: "1" }],
        replacement_requirement: "required",
        delete_blocker: blocker,
      });
    }),
  );
  await (await import("../src/account/session")).useSession().refresh();
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const draft = modelDraft("openai-compatible");
  Object.assign(draft, {
    type: "chat",
    notes: "Summary",
    providerModelId: "summary-model",
    inputModalities: "text",
  });
  const model = {
    id,
    provider_id: id,
    version: "2",
    input: modelDraftInput(draft, "openai-compatible"),
    created_at: "2026-10-17T01:00:00.000000Z",
    updated_at: "2026-10-17T01:00:00.000000Z",
  };
  const { getDeletionImpact } = await import("../src/system/model-operations");
  await expect(getDeletionImpact(model)).resolves.toMatchObject({
    replacement_requirement: "required",
    delete_blocker: null,
    reference_groups: [
      { owner_kind: "platform_selector", role: "summary", count: "1" },
    ],
  });
  for (const removed of ["memory", "meeting_summary"]) {
    role = removed;
    await expect(getDeletionImpact(model)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  }
  ownerKind = "project_summary";
  role = "meeting_summary";
  blocker = "reference_adapter_unbound";
  await expect(getDeletionImpact(model)).resolves.toMatchObject({
    delete_blocker: "reference_adapter_unbound",
  });
});

it("confines global model search to the model list and rejects oversized or control-bearing queries", async () => {
  const fetcher = vi.fn(async (_url: string) => json({ accepted: true }));
  vi.stubGlobal("fetch", fetcher);
  const { accountRequest } = await import("../src/account/transport");
  await accountRequest("/system/models", {
    query: { q: "literal%_needle", limit: 10 },
  });
  const requested = new URL(
    fetcher.mock.calls[0]![0] as string,
    "http://localhost",
  );
  expect(requested.pathname).toBe("/api/v1/system/models");
  expect(requested.searchParams.get("q")).toBe("literal%_needle");
  for (const path of ["/me", "/system/model-providers"])
    await expect(
      accountRequest(path, { query: { q: "needle" } }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  for (const q of ["界".repeat(43), "bad\nquery"])
    await expect(
      accountRequest("/system/models", { query: { q } }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("limits model type filters to the model-list route and four valid types", async () => {
  const fetcher = vi.fn(async (_url: string) => json({ accepted: true }));
  vi.stubGlobal("fetch", fetcher);
  const { accountRequest } = await import("../src/account/transport");
  for (const type of [
    "chat",
    "embedding",
    "reranker",
    "image_generation",
  ] as const) {
    await accountRequest("/system/models", {
      query: { type, q: "needle", provider_id: id, cursor: "next", limit: 10 },
    });
    expect(
      new URL(
        fetcher.mock.calls.at(-1)![0],
        "http://localhost",
      ).searchParams.get("type"),
    ).toBe(type);
  }
  for (const path of ["/me", "/system/model-providers"])
    await expect(
      accountRequest(path, { query: { type: "chat" } }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  for (const type of ["", "image", "Chat", "unknown"])
    await expect(
      accountRequest("/system/models", { query: { type: type as "chat" } }),
    ).rejects.toMatchObject({ code: "INVALID_REQUEST" });
  expect(fetcher).toHaveBeenCalledTimes(4);
});
