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
it("rejects guessed or internally inconsistent capabilities and preserves explicit unknown capacities", async () => {
  const { modelDraft, modelDraftInput } =
    await import("../src/system/model-draft");
  const draft = modelDraft("openai-compatible");
  draft.type = "chat";
  draft.name = "Explicit";
  draft.providerModelId = "model-that-looks-capable";
  expect(() => modelDraftInput(draft, "openai-compatible")).toThrow(
    "输入和输出",
  );
  draft.inputModalities = draft.outputModalities = "text";
  const input = modelDraftInput(draft, "openai-compatible");
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
