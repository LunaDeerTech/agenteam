import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount } from "@vue/test-utils";
import { h } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SessionResponse } from "../src/account/types";

const identity = (role: "admin" | "user" = "admin"): SessionResponse => ({
  user: {
    id: "019a2222-2222-7222-8222-222222222222",
    email: "system@example.test",
    username: "system-user",
    display_name: "System User",
    role,
    theme: "system",
    version: "7",
    initial_password_suggestion: false,
  },
  session: {
    id: "019a1111-1111-7111-8111-111111111111",
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "synthetic-system-context",
});
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      "Content-Type":
        status < 400 ? "application/json" : "application/problem+json",
    },
  });
const row = () => ({
  ...identity().user,
  created_at: "2026-10-16T12:00:00.000000Z",
});
const accepted = (value: unknown): value is { saved: true } =>
  !!value &&
  typeof value === "object" &&
  "saved" in value &&
  value.saved === true;
let disposeTheme: (() => void) | undefined;
let disposeView: (() => void) | undefined;
beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("VITE_ACCOUNT_ENABLED", "true");
  vi.stubGlobal("Blob", NodeBlob);
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }));
  localStorage.clear();
  disposeTheme = (await import("../src/composables/useTheme")).initTheme();
});
afterEach(() => {
  disposeView?.();
  disposeView = undefined;
  disposeTheme?.();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("system account access and directory", () => {
  it("encodes opaque cursors as one bounded query value and rejects a page exceeding the requested limit", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json({ items: [row()] }))
      .mockResolvedValueOnce(json({ items: Array.from({ length: 11 }, row) }));
    vi.stubGlobal("fetch", fetcher);
    await (await import("../src/account/session")).refreshSession();
    const { listUsers } = await import("../src/system/directory");
    const cursor = "signed+/cursor=&scope=other";
    await expect(listUsers(cursor)).resolves.toMatchObject({ items: [row()] });
    const url = new URL(
      String(fetcher.mock.calls[1]![0]),
      "https://fixture.test",
    );
    expect(url.pathname).toBe("/api/v1/system/users");
    expect([...url.searchParams.entries()]).toEqual([
      ["limit", "10"],
      ["cursor", cursor],
    ]);
    await expect(listUsers()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it("clears displayed system rows and retires pending system writes on 403 while keeping the account signed in", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json({ items: [row()] }))
      .mockResolvedValueOnce(
        json({ code: "FORBIDDEN", commit_state: "not_started" }, 403),
      )
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json({ items: [row()] }));
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const { listUsers } = await import("../src/system/directory");
    const { useCursorList } = await import("../src/system/useCursorList");
    const { systemCommand, useSystemAccess } =
      await import("../src/system/requests");
    const access = useSystemAccess();
    const view = mount({
      setup() {
        const list = useCursorList(listUsers);
        return () =>
          h("section", [
            h("output", list.items.value.map((item) => item.email).join(",")),
            h("button", { onClick: list.reload }, "Reload"),
          ]);
      },
    });
    disposeView = () => view.unmount();
    await vi.waitFor(() =>
      expect(view.get("output").text()).toBe("system@example.test"),
    );
    const pending = systemCommand(
      "/system/account-settings",
      { method: "PUT", body: "{}" },
      accepted,
    );
    await view.get("button").trigger("click");
    await vi.waitFor(() => expect(access.denied.value).toBe(true));
    expect(view.get("output").text()).toBe("");
    expect(session.useSession().status.value).toBe("authenticated");
    expect(pending.active).toBe(false);
    await expect(pending.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    expect(fetcher).toHaveBeenCalledTimes(3);
    await access.recheck();
    await vi.waitFor(() =>
      expect(view.get("output").text()).toBe("system@example.test"),
    );
    expect(pending.active).toBe(false);
    expect(access.allowed.value).toBe(true);
  });

  it("preserves an uncertain command during a same-identity session recheck but retires it after role loss", async () => {
    let finishSession!: (value: Response) => void;
    const rechecking = new Promise<Response>((resolve) => {
      finishSession = resolve;
    });
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockRejectedValueOnce(new TypeError("controlled response loss"))
      .mockReturnValueOnce(rechecking)
      .mockResolvedValueOnce(json({ saved: true }))
      .mockResolvedValueOnce(json(identity("user")));
    vi.stubGlobal("fetch", fetcher);
    const session = await import("../src/account/session");
    await session.refreshSession();
    const { systemCommand, useSystemAccess } =
      await import("../src/system/requests");
    const original = '{"version":"7","challenge_after_failures":"4"}';
    const command = systemCommand(
      "/system/account-settings",
      { method: "PUT", body: original },
      accepted,
    );
    await expect(command.run()).rejects.toMatchObject({
      commitState: "unknown",
    });
    const checking = session.refreshSession();
    await flushPromises();
    expect(session.useSession().status.value).toBe("checking");
    expect(command.active).toBe(true);
    finishSession(json(identity()));
    await checking;
    await expect(command.run()).resolves.toEqual({ saved: true });
    const writes = fetcher.mock.calls.filter(
      ([, init]) => (init as RequestInit).method === "PUT",
    );
    expect(writes).toHaveLength(2);
    for (const [, init] of writes) {
      expect((init as RequestInit).body).toBe(original);
      expect(
        new Headers((init as RequestInit).headers).get("Idempotency-Key"),
      ).toBe(command.key);
    }
    const waiting = systemCommand(
      "/system/account-settings",
      { method: "PUT", body: original },
      accepted,
    );
    await session.refreshSession();
    expect(session.useSession().status.value).toBe("authenticated");
    expect(useSystemAccess().allowed.value).toBe(false);
    expect(waiting.active).toBe(false);
    await expect(waiting.run()).rejects.toMatchObject({
      code: "STALE_REQUEST",
    });
    expect(fetcher).toHaveBeenCalledTimes(5);
  });
});
