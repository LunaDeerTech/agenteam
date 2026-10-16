import { Blob as NodeBlob } from "node:buffer";
import { createHash, webcrypto } from "node:crypto";
import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProfileResponse, SessionResponse } from "../src/account/types";

// Synthetic static PNGs; no account credentials, private images or API fixtures.
const images = [
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=",
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
].map((value) => Uint8Array.from(Buffer.from(value, "base64")));
const identity = (other = false): SessionResponse => ({
  user: {
    id: other
      ? "019a3333-3333-7333-8333-333333333333"
      : "019a2222-2222-7222-8222-222222222222",
    email: other ? "other@example.test" : "avatar@example.test",
    username: other ? "other-user" : "avatar-user",
    display_name: "Synthetic Avatar",
    role: "user",
    theme: "system",
    version: "7",
    initial_password_suggestion: false,
  },
  session: {
    id: other
      ? "019a5555-5555-7555-8555-555555555555"
      : "019a1111-1111-7111-8111-111111111111",
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "synthetic-session-context",
});
const profile = (index = 0, version = "7", other = false): ProfileResponse => ({
  user: { ...identity(other).user, version },
  avatar: {
    media_type: "image/png",
    byte_size: String(images[index]!.length),
    sha256: `sha256:${createHash("sha256").update(images[index]!).digest("hex")}`,
  },
});
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status < 400 ? "application/json" : "application/problem+json",
    },
  });
const imageResponse = (index = 0, body?: ReadableStream<Uint8Array>) =>
  new Response(body ?? images[index]!, {
    headers: {
      "Content-Type": "image/png",
      "Content-Length": profile(index).avatar!.byte_size,
      ETag: `"${profile(index).avatar!.sha256}"`,
    },
  });

let disposeTheme: (() => void) | undefined;
let createURL: ReturnType<typeof vi.fn>;
let revokeURL: ReturnType<typeof vi.fn>;
beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("crypto", webcrypto);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  let sequence = 0;
  createURL = vi.fn(() => `blob:synthetic-${++sequence}`);
  revokeURL = vi.fn();
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = createURL;
      static revokeObjectURL = revokeURL;
    },
  );
  localStorage.clear();
  disposeTheme = (await import("../src/composables/useTheme")).initTheme();
});
afterEach(() => {
  disposeTheme?.();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("saved account avatar ownership", () => {
  it("loads once for multiple consumers, holds the shared HTTP owner through the image body and reuses unchanged metadata", async () => {
    let body!: ReadableStreamDefaultController<Uint8Array>;
    const stream = new ReadableStream<Uint8Array>({
      start: (value) => {
        body = value;
      },
    });
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json(profile()))
      .mockResolvedValueOnce(imageResponse(0, stream))
      .mockResolvedValueOnce(json(profile()));
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const avatar = await import("../src/account/avatar");
    const header = avatar.useAccountAvatar();
    const editor = avatar.useAccountAvatar();
    const refresh = avatar.refreshAccountAvatar();
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
    expect(header.loading.value).toBe(true);
    const queued = session.authenticatedRequest("/me");
    await flushPromises();
    expect(fetcher).toHaveBeenCalledTimes(3);
    body.enqueue(images[0]!);
    body.close();
    await Promise.all([refresh, queued]);
    expect(header.url.value).toBe("blob:synthetic-1");
    expect(editor.url.value).toBe(header.url.value);
    expect(header.loading.value).toBe(false);
    await avatar.acceptAvatarProfile(
      profile(0, "8"),
      session.sessionIdentity(),
    );
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/session",
      "/api/v1/me",
      "/api/v1/me/avatar",
      "/api/v1/me",
    ]);
    expect(createURL).toHaveBeenCalledOnce();
    expect(revokeURL).not.toHaveBeenCalled();
  });

  it("releases replaced images and never restores a removed version when the confirmation reread fails", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json(profile()))
      .mockResolvedValueOnce(imageResponse())
      .mockResolvedValueOnce(imageResponse(1))
      .mockRejectedValueOnce(new TypeError("controlled read loss"));
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const avatar = await import("../src/account/avatar");
    await avatar.refreshAccountAvatar();
    const shared = avatar.useAccountAvatar();
    const owner = session.sessionIdentity();
    const original = shared.url.value;
    await avatar.acceptAvatarProfile(profile(1, "8"), owner);
    expect(shared.url.value).toBe("blob:synthetic-2");
    expect(revokeURL).toHaveBeenCalledWith(original);
    avatar.rejectAvatarImage(original);
    expect(shared.url.value).toBe("blob:synthetic-2");
    avatar.confirmAvatarRemoval("9", owner);
    expect(shared.url.value).toBe("");
    expect(revokeURL).toHaveBeenCalledWith("blob:synthetic-2");
    await avatar.refreshAccountAvatar();
    expect(shared.error.value).toMatchObject({ code: "NETWORK_ERROR" });
    await avatar.acceptAvatarProfile(profile(1, "8"), owner);
    await avatar.acceptAvatarProfile(profile(1, "9"), owner);
    expect(shared.url.value).toBe("");
    expect(createURL).toHaveBeenCalledTimes(2);
    expect(fetcher).toHaveBeenCalledTimes(5);
  });

  it("clears on identity loss and revokes a late old-identity Blob without replacing the new user's image", async () => {
    let finishDigest!: (value: ArrayBuffer) => void;
    const heldDigest = new Promise<ArrayBuffer>((resolve) => {
      finishDigest = resolve;
    });
    const digest = vi
      .fn()
      .mockImplementationOnce((algorithm: string, bytes: ArrayBuffer) =>
        webcrypto.subtle.digest(algorithm, bytes),
      )
      .mockReturnValueOnce(heldDigest)
      .mockImplementation((algorithm: string, bytes: ArrayBuffer) =>
        webcrypto.subtle.digest(algorithm, bytes),
      );
    vi.stubGlobal("crypto", { subtle: { digest } });
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json(profile()))
      .mockResolvedValueOnce(imageResponse())
      .mockResolvedValueOnce(imageResponse(1))
      .mockResolvedValueOnce(
        json({ code: "UNAUTHENTICATED", commit_state: "not_started" }, 401),
      )
      .mockResolvedValueOnce(json(identity(true)))
      .mockResolvedValueOnce(json(profile(0, "7", true)))
      .mockResolvedValueOnce(imageResponse());
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const avatar = await import("../src/account/avatar");
    await avatar.refreshAccountAvatar();
    const shared = avatar.useAccountAvatar();
    const oldOwner = session.sessionIdentity();
    const delayed = avatar.acceptAvatarProfile(profile(1, "8"), oldOwner);
    await vi.waitFor(() => expect(digest).toHaveBeenCalledTimes(2));
    await expect(session.authenticatedRequest("/me")).rejects.toMatchObject({
      status: 401,
    });
    expect(shared.url.value).toBe("");
    expect(revokeURL).toHaveBeenCalledWith("blob:synthetic-1");
    await session.refreshSession();
    await avatar.refreshAccountAvatar();
    expect(shared.url.value).toBe("blob:synthetic-2");
    finishDigest(await webcrypto.subtle.digest("SHA-256", images[1]!));
    await delayed;
    await avatar.acceptAvatarProfile(profile(1, "99"), oldOwner);
    expect(shared.url.value).toBe("blob:synthetic-2");
    expect(revokeURL).toHaveBeenCalledWith("blob:synthetic-3");
    expect(shared.error.value).toBeUndefined();
  });

  it("exposes a failed image read for retry and never publishes mismatched content", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json(profile()))
      .mockResolvedValueOnce(imageResponse(1))
      .mockResolvedValueOnce(json(profile()))
      .mockResolvedValueOnce(imageResponse());
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const avatar = await import("../src/account/avatar");
    await avatar.refreshAccountAvatar();
    const shared = avatar.useAccountAvatar();
    expect(shared.url.value).toBe("");
    expect(shared.error.value).toMatchObject({ code: "INVALID_RESPONSE" });
    expect(createURL).not.toHaveBeenCalled();
    await avatar.refreshAccountAvatar();
    expect(shared.url.value).toBe("blob:synthetic-1");
    expect(shared.error.value).toBeUndefined();
    avatar.rejectAvatarImage(shared.url.value);
    expect(shared.url.value).toBe("");
    expect(revokeURL).toHaveBeenCalledWith("blob:synthetic-1");
    expect(shared.error.value).toMatchObject({ code: "INVALID_RESPONSE" });
  });
});
