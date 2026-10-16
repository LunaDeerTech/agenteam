import { Blob as NodeBlob } from "node:buffer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionResponse } from "../src/account/types";

const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
const failure = (code: string, status: number) =>
  json({ code, commit_state: "not_committed" }, status);
const bootstrap = () =>
  json({
    csrf_token: "browser-context",
    challenge_modes: [],
    delivery_channel: "smtp",
  });
const identity = (
  sessionId = "019a1111-1111-7111-8111-111111111111",
): SessionResponse => ({
  user: {
    id: "019a2222-2222-7222-8222-222222222222",
    email: "unit@example.test",
    username: "unit-user",
    display_name: "Unit User",
    role: "user",
    theme: "dark",
    version: "7",
    initial_password_suggestion: false,
  },
  session: {
    id: sessionId,
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "session-context",
});
const accepted = (value: unknown): value is { saved: true } =>
  !!value &&
  typeof value === "object" &&
  "saved" in value &&
  value.saved === true;

let disposeTheme: (() => void) | undefined;
beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  const changes = new EventTarget();
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: changes.addEventListener.bind(changes),
    removeEventListener: changes.removeEventListener.bind(changes),
  }));
  localStorage.clear();
  const theme = await import("../src/composables/useTheme");
  disposeTheme = theme.initTheme();
});
afterEach(() => {
  disposeTheme?.();
  disposeTheme = undefined;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("confirmed account commands", () => {
  it("clears login material at confirmation and only rereads the session after that read fails", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(bootstrap())
      .mockResolvedValueOnce(json({ ...identity(), next_path: "/" }))
      .mockResolvedValueOnce(failure("TEMPORARILY_UNAVAILABLE", 503))
      .mockResolvedValueOnce(json(identity()));
    vi.stubGlobal("fetch", fetcher);
    const { loginCommand, refreshSession, useSession } =
      await import("../src/account/session");
    const command = await loginCommand(
      "unit@example.test",
      "unit-only password",
    );
    const clearSecrets = vi.fn();
    await expect(command.run(clearSecrets)).rejects.toMatchObject({
      status: 503,
    });
    expect(clearSecrets).toHaveBeenCalledOnce();
    expect(command.confirmed).toBe(true);
    expect(useSession().status.value).toBe("unavailable");
    expect(useSession().user.value).toBeUndefined();
    await expect(command.run(clearSecrets)).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    await refreshSession();
    expect(useSession().status.value).toBe("authenticated");
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/auth/bootstrap",
      "/api/v1/sessions/login",
      "/api/v1/session",
      "/api/v1/session",
    ]);
  });

  it("does not confirm malformed login success or start a confirmation read", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(bootstrap())
      .mockResolvedValueOnce(
        json({ user: identity().user, session: {}, next_path: "/" }),
      );
    vi.stubGlobal("fetch", fetcher);
    const { loginCommand } = await import("../src/account/session");
    const command = await loginCommand(
      "unit@example.test",
      "unit-only password",
    );
    const confirmed = vi.fn();
    await expect(command.run(confirmed)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    expect(confirmed).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("never repeats a confirmed password change when replacement-session discovery fails", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json({ completed: true, next_path: "/" }))
      .mockRejectedValueOnce(new TypeError("controlled connection loss"))
      .mockResolvedValueOnce(
        json(identity("019a3333-3333-7333-8333-333333333333")),
      );
    vi.stubGlobal("fetch", fetcher);
    const { passwordCommand, refreshSession, sessionIdentity, useSession } =
      await import("../src/account/session");
    await refreshSession();
    const previous = sessionIdentity();
    const command = passwordCommand({
      version: "7",
      current_password: "old unit-only password",
      new_password: "new unit-only password",
      confirmation: "new unit-only password",
    });
    const clearSecrets = vi.fn();
    await expect(command.run(clearSecrets)).rejects.toMatchObject({
      code: "NETWORK_ERROR",
    });
    expect(command.confirmed).toBe(true);
    expect(clearSecrets).toHaveBeenCalledOnce();
    expect(useSession().status.value).toBe("unavailable");
    await refreshSession();
    expect(sessionIdentity().sessionId).not.toBe(previous.sessionId);
    await expect(command.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/session",
      "/api/v1/me/change-password",
      "/api/v1/session",
      "/api/v1/session",
    ]);
  });
});

describe("account command ownership and recovery", () => {
  it("keeps the original key, body, version and CSRF for an explicit unknown-result retry", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockRejectedValueOnce(new TypeError("controlled connection loss"))
      .mockResolvedValueOnce(json({ saved: true }));
    vi.stubGlobal("fetch", fetcher);
    const { refreshSession, sessionCommand } =
      await import("../src/account/session");
    await refreshSession();
    const original = JSON.stringify({ version: "7", theme: "light" });
    const options = { method: "PUT" as const, body: original };
    const command = sessionCommand("/me/preferences", options, accepted);
    options.body = JSON.stringify({ version: "8", theme: "dark" });
    await expect(command.run()).rejects.toMatchObject({
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    expect(command.active).toBe(true);
    expect(fetcher).toHaveBeenCalledTimes(2);
    await expect(command.run()).resolves.toEqual({ saved: true });
    const attempts = fetcher.mock.calls
      .slice(1)
      .map(([, init]) => init as RequestInit);
    expect(attempts).toHaveLength(2);
    for (const attempt of attempts) {
      expect(attempt.body).toBe(original);
      expect(new Headers(attempt.headers).get("Idempotency-Key")).toBe(
        command.key,
      );
      expect(new Headers(attempt.headers).get("X-CSRF-Token")).toBe(
        "session-context",
      );
    }
    expect(command.confirmed).toBe(true);
  });

  it("retires authenticated commands after CSRF rejection even when the same identity is reloaded", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(failure("CSRF_FAILED", 403))
      .mockResolvedValueOnce(
        json({ ...identity(), csrf_token: "refreshed-context" }),
      );
    vi.stubGlobal("fetch", fetcher);
    const { refreshSession, sessionCommand, useSession } =
      await import("../src/account/session");
    await refreshSession();
    const command = sessionCommand(
      "/me",
      { method: "PATCH", body: '{"version":"7","display_name":"draft"}' },
      accepted,
    );
    await expect(command.run()).rejects.toMatchObject({ code: "CSRF_FAILED" });
    expect(command.active).toBe(false);
    expect(useSession().status.value).toBe("unavailable");
    await refreshSession();
    expect(useSession().status.value).toBe("authenticated");
    expect(command.active).toBe(false);
    await expect(command.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("retires all browser-context commands on CSRF rejection and requires fresh bootstrap", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(bootstrap())
      .mockResolvedValueOnce(failure("CSRF_FAILED", 403))
      .mockResolvedValueOnce(bootstrap());
    vi.stubGlobal("fetch", fetcher);
    const { browserCommand } = await import("../src/account/session");
    const first = await browserCommand(
      "/password-resets/request",
      { method: "POST", body: "{}" },
      accepted,
    );
    const waiting = await browserCommand(
      "/password-resets/request",
      { method: "POST", body: "{}" },
      accepted,
    );
    await expect(first.run()).rejects.toMatchObject({ code: "CSRF_FAILED" });
    expect(first.active).toBe(false);
    expect(waiting.active).toBe(false);
    await expect(waiting.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    const fresh = await browserCommand(
      "/password-resets/request",
      { method: "POST", body: "{}" },
      accepted,
    );
    expect(fresh.active).toBe(true);
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/auth/bootstrap",
      "/api/v1/password-resets/request",
      "/api/v1/auth/bootstrap",
    ]);
  });

  it("preserves identity on ordinary forbidden responses but clears identity, theme and pending authority on 401", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(failure("FORBIDDEN", 403))
      .mockResolvedValueOnce(failure("UNAUTHENTICATED", 401));
    vi.stubGlobal("fetch", fetcher);
    const { authenticatedRequest, refreshSession, sessionCommand, useSession } =
      await import("../src/account/session");
    await refreshSession();
    const waiting = sessionCommand("/me", { method: "PATCH", body: "{}" });
    await expect(authenticatedRequest("/me")).rejects.toMatchObject({
      status: 403,
    });
    expect(useSession().status.value).toBe("authenticated");
    expect(waiting.active).toBe(true);
    expect(document.documentElement.dataset.theme).toBe("dark");
    await expect(authenticatedRequest("/me")).rejects.toMatchObject({
      status: 401,
    });
    expect(useSession().status.value).toBe("anonymous");
    expect(useSession().hasIdentity.value).toBe(false);
    expect(useSession().user.value).toBeUndefined();
    expect(waiting.active).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("light");
    await expect(waiting.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    expect(fetcher).toHaveBeenCalledTimes(3);
  });
});
