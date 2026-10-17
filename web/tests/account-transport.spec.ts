import { Blob as NodeBlob } from "node:buffer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

const json = (value: unknown, status = 200, media = "application/json") =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": media },
  });
const valid = (value: unknown): value is { accepted: true } =>
  !!value &&
  typeof value === "object" &&
  "accepted" in value &&
  value.accepted === true;

beforeEach(() => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  // jsdom lacks the browser Blob streaming helpers. Node implements the same
  // standard body APIs here; actual cookies/headers remain browser-test scope.
  vi.stubGlobal("Blob", NodeBlob);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("account HTTP request ownership", () => {
  it("does not send the next request until the previous response body ends", async () => {
    const started = deferred();
    let body!: ReadableStreamDefaultController<Uint8Array>;
    const fetcher = vi
      .fn()
      .mockImplementationOnce(async () => {
        started.resolve();
        return new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              body = controller;
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      })
      .mockResolvedValueOnce(json({ accepted: true }));
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest } = await import("../src/account/transport");
    const first = accountRequest("/session", {}, valid);
    const next = accountRequest("/me", {}, valid);
    await started.promise;
    await Promise.resolve();
    expect(fetcher).toHaveBeenCalledTimes(1);
    body.enqueue(new TextEncoder().encode('{"accepted":true}'));
    await Promise.resolve();
    expect(fetcher).toHaveBeenCalledTimes(1);
    body.close();
    await expect(first).resolves.toEqual({ accepted: true });
    await expect(next).resolves.toEqual({ accepted: true });
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/session",
      "/api/v1/me",
    ]);
  });

  it("holds a command and its confirmation read ahead of an unrelated queued read", async () => {
    const started = deferred();
    const release = deferred();
    const fetcher = vi.fn(async (url: string) => {
      if (url.endsWith("/sessions/login")) {
        started.resolve();
        await release.promise;
      }
      return json({ accepted: true });
    });
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest, accountSequence } =
      await import("../src/account/transport");
    const login = accountSequence(async (send) => {
      await send(
        "/sessions/login",
        { method: "POST", key: "owned-key", body: "{}" },
        valid,
      );
      return send("/session", {}, valid);
    });
    await started.promise;
    const unrelated = accountRequest("/me", {}, valid);
    release.resolve();
    await Promise.all([login, unrelated]);
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/sessions/login",
      "/api/v1/session",
      "/api/v1/me",
    ]);
  });

  it("waits for oversized-body cancellation before releasing ownership and marks a sent mutation unknown", async () => {
    const cancelStarted = deferred();
    const cancelFinished = deferred();
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(new Uint8Array(1024 * 1024 + 1));
            },
            cancel() {
              cancelStarted.resolve();
              return cancelFinished.promise;
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(json({ accepted: true }));
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest } = await import("../src/account/transport");
    const sent = accountRequest(
      "/me",
      { method: "PATCH", body: "{}", key: "original-key" },
      valid,
    );
    const rejected = expect(sent).rejects.toMatchObject({
      commitState: "unknown",
    });
    const next = accountRequest("/session", {}, valid);
    await cancelStarted.promise;
    expect(fetcher).toHaveBeenCalledTimes(1);
    cancelFinished.resolve();
    await rejected;
    await expect(next).resolves.toEqual({ accepted: true });
  });

  it.each(["read", "mutation"] as const)(
    "rejects stale %s ownership before sending and after receiving a delayed body",
    async (mode) => {
      const started = deferred();
      const release = deferred();
      const fetcher = vi.fn(async () => {
        started.resolve();
        await release.promise;
        return json({ accepted: true });
      });
      vi.stubGlobal("fetch", fetcher);
      const { accountRequest } = await import("../src/account/transport");
      const options =
        mode === "mutation"
          ? { method: "PATCH" as const, key: "original-intent", body: "{}" }
          : {};
      await expect(
        accountRequest("/me", { ...options, current: () => false }, valid),
      ).rejects.toMatchObject({
        code: "STALE_REQUEST",
        commitState: "not_committed",
      });
      expect(fetcher).not.toHaveBeenCalled();
      let current = true;
      const reading = accountRequest(
        "/me",
        { ...options, current: () => current },
        valid,
      );
      const rejected = expect(reading).rejects.toMatchObject({
        code: "STALE_REQUEST",
        commitState: mode === "mutation" ? "unknown" : "not_committed",
      });
      await started.promise;
      current = false;
      release.resolve();
      await rejected;
    },
  );
});

describe("account HTTP response contract", () => {
  it.each([
    ["not_started", "not_started", "not_started"],
    ["missing state", undefined, "unknown"],
    ["invalid state", "maybe_committed", "unknown"],
  ])(
    "preserves the recovery meaning of a login rejection with %s",
    async (_name, state, expected) => {
      const fetcher = vi.fn().mockResolvedValue(
        json(
          {
            code: "UNAUTHENTICATED",
            ...(state === undefined ? {} : { commit_state: state }),
          },
          401,
          "application/problem+json",
        ),
      );
      vi.stubGlobal("fetch", fetcher);
      const { accountRequest } = await import("../src/account/transport");
      await expect(
        accountRequest("/sessions/login", {
          method: "POST",
          key: "original-login-intent",
          body: "{}",
        }),
      ).rejects.toMatchObject({
        code: "UNAUTHENTICATED",
        status: 401,
        commitState: expected,
      });
      expect(fetcher).toHaveBeenCalledOnce();
    },
  );

  it.each([
    [
      "HTML success",
      () =>
        new Response("<html>not an account response</html>", {
          headers: { "Content-Type": "text/html" },
        }),
    ],
    [
      "malformed JSON",
      () =>
        new Response("{", { headers: { "Content-Type": "application/json" } }),
    ],
    ["wrong success status", () => json({ accepted: true }, 201)],
    ["wrong success shape", () => json({ accepted: false })],
  ])("does not confirm a mutation with %s", async (_name, response) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response()));
    const { accountRequest } = await import("../src/account/transport");
    await expect(
      accountRequest(
        "/me/preferences",
        { method: "PUT", key: "same-intent", body: "{}" },
        valid,
      ),
    ).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
  });

  it("uses same-origin, no-store and redirect rejection, and handles 204 without JSON", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest } = await import("../src/account/transport");
    await expect(
      accountRequest("/sessions/logout", {
        method: "POST",
        key: "logout-intent",
        csrf: "owned-csrf",
        body: "{}",
        status: 204,
      }),
    ).resolves.toBeUndefined();
    const options = fetcher.mock.calls[0]![1] as RequestInit;
    expect(options.credentials).toBe("same-origin");
    expect(options.cache).toBe("no-store");
    expect(options.redirect).toBe("error");
    expect(new Headers(options.headers).get("X-CSRF-Token")).toBe("owned-csrf");
    expect(new Headers(options.headers).get("Idempotency-Key")).toBe(
      "logout-intent",
    );
  });

  it("preserves safe problem facts and never displays raw server text", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        json(
          {
            code: "VERSION_CONFLICT",
            commit_state: "not_committed",
            field_errors: [{ path: "/username", code: "TAKEN" }],
            detail: "PRIVATE_SERVER_SENTINEL",
          },
          409,
          "application/problem+json",
        ),
      ),
    );
    const { accountRequest, accountErrorMessage } =
      await import("../src/account/transport");
    let failure: unknown;
    try {
      await accountRequest(
        "/me",
        { method: "PATCH", key: "intent", body: "{}" },
        valid,
      );
    } catch (error) {
      failure = error;
    }
    expect(failure).toMatchObject({
      code: "VERSION_CONFLICT",
      status: 409,
      commitState: "not_committed",
      fields: [{ path: "/username", code: "TAKEN" }],
    });
    expect(accountErrorMessage(failure)).not.toContain(
      "PRIVATE_SERVER_SENTINEL",
    );
  });
});

describe("multimodal model-test request budgets", () => {
  const modelId = "019a4444-4444-7444-8444-444444444444";
  const testPath = `/system/models/${modelId}/test`;
  const mib = 1024 * 1024;
  const bodyWithBytes = (bytes: number) => {
    const empty = JSON.stringify({ text: "" });
    return JSON.stringify({ text: "a".repeat(bytes - empty.length) });
  };

  it("counts the entire UTF-8 JSON envelope at the 16 MiB model-test boundary", async () => {
    const fetcher = vi.fn(async (_url: string, _init?: RequestInit) =>
      json({ accepted: true }),
    );
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest } = await import("../src/account/transport");
    const envelope = {
      expected_version: "3",
      provider_version: "2",
      input: {
        text: "🧠",
        attachments: [
          {
            name: "sample.pdf",
            media_type: "application/pdf",
            data_base64: "",
          },
        ],
      },
    };
    const available =
      16 * mib - new TextEncoder().encode(JSON.stringify(envelope)).length;
    envelope.input.text += "a".repeat(available % 4);
    envelope.input.attachments[0]!.data_base64 = "A".repeat(
      available - (available % 4),
    );
    // Attachment content limits belong to the input validator. This fixture
    // isolates the transport budget for the complete encoded JSON envelope.
    const boundary = JSON.stringify(envelope);
    expect(new TextEncoder().encode(boundary)).toHaveLength(16 * mib);
    await expect(
      accountRequest(testPath, { method: "POST", body: boundary }, valid),
    ).resolves.toEqual({ accepted: true });
    expect(fetcher.mock.calls[0]?.[0]).toBe(`/api/v1${testPath}`);
    envelope.input.text += "a";
    await expect(
      accountRequest(
        testPath,
        { method: "POST", body: JSON.stringify(envelope) },
        valid,
      ),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("keeps ordinary and catalog budgets and denies the large exception on other paths or methods", async () => {
    const fetcher = vi.fn(async () => json({ accepted: true }));
    vi.stubGlobal("fetch", fetcher);
    const { accountRequest } = await import("../src/account/transport");
    await accountRequest(
      "/me",
      { method: "PATCH", body: bodyWithBytes(16384) },
      valid,
    );
    const aboveOrdinary = bodyWithBytes(16385);
    for (const [path, method] of [
      ["/me", "PATCH"],
      [testPath, "GET"],
      [testPath, "PUT"],
      ["/system/models/not-a-uuid/test", "POST"],
      [`${testPath}/extra`, "POST"],
      [`/system/model-providers/${modelId}/discovery`, "POST"],
    ] as const)
      await expect(
        accountRequest(path, { method, body: aboveOrdinary }, valid),
      ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    await accountRequest(
      `/system/models/${modelId}`,
      { method: "PUT", body: bodyWithBytes(mib) },
      valid,
    );
    await expect(
      accountRequest(
        `/system/models/${modelId}`,
        { method: "PUT", body: bodyWithBytes(mib + 1) },
        valid,
      ),
    ).rejects.toMatchObject({ code: "INVALID_ARGUMENT" });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
