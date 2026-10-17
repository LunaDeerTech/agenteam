import { Blob as NodeBlob } from "node:buffer";
import { mount } from "@vue/test-utils";
import { h } from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
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
it("adds only mandatory chat text capabilities and rejects inconsistent explicit capabilities", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const draft = modelDraft("openai-compatible");
  draft.type = "chat";
  draft.name = "Explicit";
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

it("round-trips explicit reasoning names and wire values and rejects ambiguous or unsafe levels", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const { isReasoningEfforts } = await import("../src/system/model-catalog");
  const draft = modelDraft("openai-compatible");
  Object.assign(draft, {
    type: "chat",
    name: "Reasoning Fixture",
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
    name: "Summary",
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
