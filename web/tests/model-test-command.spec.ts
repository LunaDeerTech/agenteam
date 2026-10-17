import { Blob as NodeBlob } from "node:buffer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { CatalogModel, Provider } from "../src/system/model-catalog";
import type { ModelTestInput } from "../src/system/model-test-input";

const providerId = "019a2222-2222-7222-8222-222222222222";
const modelId = "019a4444-4444-7444-8444-444444444444";
const createdAt = "2026-10-17T01:00:00.000000Z";
const session = {
  user: {
    id: providerId,
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
const provider: Provider = {
  id: providerId,
  version: "2",
  input: {
    name: "Attachment Provider",
    protocol: "openai-compatible",
    base_url: "https://owned.invalid/v1",
    enabled: true,
    credential_ref: null,
    options: {},
  },
  created_at: createdAt,
  updated_at: createdAt,
};
const model = (type: "chat" | "image_generation" = "chat"): CatalogModel => ({
  id: modelId,
  provider_id: providerId,
  version: "3",
  input: {
    notes: "",
    provider_model_id: "attachment-model",
    type,
    enabled: true,
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
    capabilities: {
      input_modalities: type === "chat" ? ["text", "image", "file"] : ["text"],
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
const result = (duration_ms = 1, answer_text: string | null = null) => ({
  operation_id: "019a3333-3333-7333-8333-333333333333",
  provider_id: providerId,
  provider_version: "2",
  model_id: modelId,
  model_version: "3",
  outcome: "succeeded" as const,
  reason: null,
  duration_ms,
  created_at: createdAt,
  answer_text,
  answer_truncated: false,
});
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
type Request = {
  path: string;
  key: string | null;
  body: string;
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
let owners: { release: () => void }[];

beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  localStorage.clear();
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
        body: String(init.body ?? ""),
        signal: init.signal!,
      };
      requests.push(request);
      started?.();
      started = undefined;
      return respond(request);
    }),
  );
  // Finish imports and session preparation before controlling request timers.
  await (await import("../src/account/session")).useSession().refresh();
  operations = await import("../src/system/model-operations");
});
afterEach(() => {
  for (const owner of owners) owner.release();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("multimodal model test commands", () => {
  it("keeps attachments pending through 125 seconds and replays the frozen input after an unknown timeout", async () => {
    const input: ModelTestInput = {
      text: "Explain the attached note.",
      attachments: [
        { name: "note.txt", media_type: "text/plain", data_base64: "bm90ZQ==" },
      ],
    };
    const originalInput = structuredClone(input);
    const command = operations.modelTestCommand(provider, model(), input);
    owners.push(command);
    input.text = "a later draft";
    input.attachments[0]!.data_base64 = "bGF0ZXI=";
    input.attachments[0]!.name = "later.txt";
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const requestStarted = nextRequest();
    let settled = false;
    const pending = command.run().then(
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
    expect(await pending).toMatchObject({
      code: "NETWORK_ERROR",
      commitState: "unknown",
    });
    expect(command.active).toBe(true);
    expect(command.confirmed).toBe(false);
    expect(requests[0]!.signal.aborted).toBe(true);
    expect(JSON.parse(requests[0]!.body)).toEqual({
      expected_version: "3",
      provider_version: "2",
      input: originalInput,
    });

    input.attachments.length = 0;
    respond = async () => json(result(120000, "附件结论"));
    await expect(command.run()).resolves.toMatchObject({
      duration_ms: 120000,
      answer_text: "附件结论",
      answer_truncated: false,
    });
    expect(command.confirmed).toBe(true);
    expect(requests).toHaveLength(2);
    expect(requests[1]!.path).toBe(`/api/v1/system/models/${modelId}/test`);
    expect(requests[1]!.key).toBe(requests[0]!.key);
    expect(requests[1]!.body).toBe(requests[0]!.body);
    expect(requests[1]!.signal).not.toBe(requests[0]!.signal);
    expect(requests[1]!.signal.aborted).toBe(false);
  });

  it("keeps omitted and text-only Chat inputs at 30 seconds and rejects a receipt beyond 20 seconds", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    for (const input of [undefined, { text: "Only text", attachments: [] }]) {
      const command = operations.modelTestCommand(provider, model(), input);
      owners.push(command);
      const requestStarted = nextRequest();
      let settled = false;
      const pending = command.run().catch((error: unknown) => {
        settled = true;
        return error;
      });
      await requestStarted;
      await vi.advanceTimersByTimeAsync(29999);
      expect(settled).toBe(false);
      expect(requests.at(-1)!.signal.aborted).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      expect(await pending).toMatchObject({
        code: "NETWORK_ERROR",
        commitState: "unknown",
      });
      expect(JSON.parse(requests.at(-1)!.body)).toEqual({
        expected_version: "3",
        provider_version: "2",
        ...(input ? { input } : {}),
      });
    }
    const command = operations.modelTestCommand(provider, model(), {
      text: "Only text",
      attachments: [],
    });
    owners.push(command);
    respond = async () => json(result());
    await expect(command.run()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    respond = async () => json(result(20001, "too late"));
    await expect(command.run()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    respond = async () => json(result(20000, ""));
    await expect(command.run()).resolves.toMatchObject({
      duration_ms: 20000,
      answer_text: "",
    });
  });

  it("retains the image-generation budget and never accepts a text answer for a non-Chat model", async () => {
    const command = operations.modelTestCommand(
      provider,
      model("image_generation"),
    );
    owners.push(command);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const requestStarted = nextRequest();
    let settled = false;
    const pending = command.run().catch((error: unknown) => {
      settled = true;
      return error;
    });
    await requestStarted;
    await vi.advanceTimersByTimeAsync(129999);
    expect(settled).toBe(false);
    expect(requests[0]!.signal.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    expect(await pending).toMatchObject({
      code: "NETWORK_ERROR",
      commitState: "unknown",
    });
    respond = async () => json(result(120000, "not an image result"));
    await expect(command.run()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    respond = async () => json(result(120000));
    await expect(command.run()).resolves.toMatchObject({
      answer_text: null,
      answer_truncated: false,
    });
    expect(requests.map(({ key }) => key)).toEqual([
      command.key,
      command.key,
      command.key,
    ]);
    expect(new Set(requests.map(({ body }) => body)).size).toBe(1);
  });

  it("requires answer metadata and measures answer limits in UTF-8 bytes", () => {
    const answer = "🧠".repeat(2048);
    expect(
      operations.isTestResult({
        ...result(120000, answer),
        answer_truncated: true,
      }),
    ).toBe(true);
    expect(operations.isTestResult(result(1, answer + "a"))).toBe(false);
    const { answer_text: _text, ...missingText } = result();
    const { answer_truncated: _truncated, ...missingTruncated } = result();
    expect(operations.isTestResult(missingText)).toBe(false);
    expect(operations.isTestResult(missingTruncated)).toBe(false);
    expect(
      operations.isTestResult({ ...result(), answer_truncated: true }),
    ).toBe(false);
    const failed = { ...result(), outcome: "failed", reason: "network_failed" };
    expect(operations.isTestResult(failed)).toBe(true);
    expect(
      operations.isTestResult({ ...failed, answer_text: "unconfirmed answer" }),
    ).toBe(false);
  });

  it("rejects invalid explicit input before creating a request, including input for non-Chat models", () => {
    expect(() =>
      operations.modelTestCommand(provider, model(), {
        text: " \n",
        attachments: [],
      }),
    ).toThrow(expect.objectContaining({ code: "INVALID_ARGUMENT" }));
    expect(() =>
      operations.modelTestCommand(provider, model("image_generation"), {
        text: "generate",
        attachments: [],
      }),
    ).toThrow(expect.objectContaining({ code: "INVALID_ARGUMENT" }));
    expect(requests).toHaveLength(0);
  });
});
