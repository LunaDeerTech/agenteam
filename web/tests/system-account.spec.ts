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

describe("system settings command receipts", () => {
  it("keeps generic invitation commands after a committed Problem and distinguishes that feedback from an unknown outcome", async () => {
    const receipt = {
      id: "019a3333-3333-7333-8333-333333333333",
      job_id: "019a4444-4444-7444-8444-444444444444",
      version: "1",
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(
        json({ code: "UNAVAILABLE", commit_state: "committed" }, 503),
      )
      .mockResolvedValueOnce(json(receipt, 201))
      .mockResolvedValueOnce(
        json({ code: "UNAVAILABLE", commit_state: "not_started" }, 503),
      );
    vi.stubGlobal("fetch", fetcher);
    await (await import("../src/account/session")).refreshSession();
    const { createInvitation, listInvitations } =
      await import("../src/system/invitations");
    const { useCommand } = await import("../src/account/useCommand");
    const Feedback = (
      await import("../src/components/account/CommandFeedback.vue")
    ).default;
    const view = mount({
      setup() {
        const operation = useCommand<typeof receipt>();
        return () =>
          h("section", [
            h(
              "button",
              {
                onClick: () =>
                  operation.submit(
                    () => createInvitation("synthetic-recipient@example.test"),
                    async () => {
                      await listInvitations();
                    },
                  ),
              },
              "Invite",
            ),
            h(Feedback, {
              error: operation.error.value,
              uncertain: operation.uncertain.value,
              replayable: operation.replayable.value,
              busy: operation.busy.value,
              onRetry: operation.retry,
            }),
            h("output", String(operation.confirmed.value)),
          ]);
      },
    });
    disposeView = () => view.unmount();
    await view.get("button").trigger("click");
    await vi.waitFor(() =>
      expect(view.text()).toContain("操作已提交，回执尚未读回"),
    );
    expect(view.text()).not.toContain("原操作结果尚未确认");
    expect(view.get("output").text()).toBe("false");
    await view
      .findAll("button")
      .find((button) => button.text() === "重试原请求")!
      .trigger("click");
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    await flushPromises();
    expect(view.get("output").text()).toBe("true");
    expect(
      view.findAll("button").some((button) => button.text() === "重试原请求"),
    ).toBe(false);
    const writes = fetcher.mock.calls.filter(
      ([, init]) => (init as RequestInit).method === "POST",
    );
    expect(writes).toHaveLength(2);
    expect(writes[0]![0]).toBe("/api/v1/system/invitations");
    expect(writes[1]![0]).toBe(writes[0]![0]);
    expect((writes[1]![1] as RequestInit).body).toBe(
      (writes[0]![1] as RequestInit).body,
    );
    expect(
      new Headers((writes[1]![1] as RequestInit).headers).get(
        "Idempotency-Key",
      ),
    ).toBe(
      new Headers((writes[0]![1] as RequestInit).headers).get(
        "Idempotency-Key",
      ),
    );
  });

  it.each([
    { status: 404, state: "not_started", expected: "unknown" },
    { status: 503, state: "committed", expected: "committed" },
  ])(
    "retains an SMTP test intent after $status/$state without inventing a receipt",
    async ({ status, state, expected }) => {
      const jobId = "019a3333-3333-7333-8333-333333333333";
      const fetcher = vi
        .fn()
        .mockResolvedValueOnce(json(identity()))
        .mockResolvedValueOnce(
          json(
            {
              code: status === 404 ? "NOT_FOUND" : "UNAVAILABLE",
              commit_state: state,
            },
            status,
          ),
        )
        .mockResolvedValueOnce(json({ job_id: jobId }, 202));
      vi.stubGlobal("fetch", fetcher);
      await (await import("../src/account/session")).refreshSession();
      const { testSMTP } = await import("../src/system/account-settings");
      const command = testSMTP("synthetic-recipient@example.test");
      await expect(command.run()).rejects.toMatchObject({
        status,
        commitState: expected,
      });
      expect(command.confirmed).toBe(false);
      expect(command.active).toBe(true);
      await expect(command.run()).resolves.toEqual({ job_id: jobId });
      expect(command.confirmed).toBe(true);
      const writes = fetcher.mock.calls.slice(1);
      expect(writes).toHaveLength(2);
      for (const [url, init] of writes) {
        expect(url).toBe("/api/v1/system/smtp/test");
        expect((init as RequestInit).method).toBe("POST");
        expect((init as RequestInit).body).toBe(
          '{"recipient":"synthetic-recipient@example.test"}',
        );
        expect(
          new Headers((init as RequestInit).headers).get("Idempotency-Key"),
        ).toBe(command.key);
      }
    },
  );

  it("keeps an exact SMTP credential write private to the original intent and accepts a newer current view only with its matching receipt", async () => {
    const base = {
      id: "019a4444-4444-7444-8444-444444444444",
      version: "9007199254740992",
      configured: true,
      host: "mail.fixture.test",
      port: 2525,
      encryption: "starttls" as const,
      username: "fixture",
      sender_email: "sender@example.test",
      sender_name: "Original",
      credential_present: true,
      auto_retry_count: "0",
      retry_interval_seconds: "60",
    };
    const receipt = {
      applied_version: "9007199254740993",
      settings: {
        ...base,
        version: "9007199254740994",
        sender_name: "A later writer",
      },
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(
        json({
          ...receipt,
          settings: { ...receipt.settings, password: "forbidden-projection" },
        }),
      )
      .mockResolvedValueOnce(json(receipt));
    vi.stubGlobal("fetch", fetcher);
    await (await import("../src/account/session")).refreshSession();
    const { saveSMTPSettings, smtpDraft } =
      await import("../src/system/account-settings");
    const draft = {
      ...smtpDraft(base),
      credential_action: "replace" as const,
      password: "synthetic-only-smtp-secret",
      sender_name: "My draft",
    };
    const command = saveSMTPSettings(base, draft);
    draft.password = "different-draft";
    draft.sender_name = "Different name";
    base.version = "9007199254740995";
    await expect(command.run()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      commitState: "unknown",
    });
    expect(command.confirmed).toBe(false);
    await expect(command.run()).resolves.toEqual(receipt);
    const writes = fetcher.mock.calls
      .slice(1)
      .map(([, init]) => init as RequestInit);
    expect(writes).toHaveLength(2);
    expect(writes[0]!.body).toBe(writes[1]!.body);
    expect(JSON.parse(String(writes[0]!.body))).toMatchObject({
      version: "9007199254740992",
      password: "synthetic-only-smtp-secret",
      credential_action: "keep",
      sender_name: "My draft",
    });
    for (const write of writes)
      expect(new Headers(write.headers).get("Idempotency-Key")).toBe(
        command.key,
      );
  });

  it("does not repeat a confirmed SMTP test when reading the accepted job fails", async () => {
    const jobId = "019a3333-3333-7333-8333-333333333333";
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(json(identity()))
      .mockResolvedValueOnce(json({ job_id: jobId }, 202))
      .mockResolvedValueOnce(
        json({ code: "UNAVAILABLE", commit_state: "not_started" }, 503),
      );
    vi.stubGlobal("fetch", fetcher);
    await (await import("../src/account/session")).refreshSession();
    const { testSMTP, getSMTPJob, useSettingsCommand } =
      await import("../src/system/account-settings");
    const view = mount({
      setup() {
        const operation = useSettingsCommand<{ job_id: string }>();
        return () =>
          h("section", [
            h(
              "button",
              {
                onClick: () =>
                  operation.submit(
                    () => testSMTP("synthetic-recipient@example.test"),
                    async (receipt) => {
                      await getSMTPJob(receipt.job_id);
                    },
                  ),
              },
              "Test",
            ),
            h("button", { onClick: operation.retry }, "Retry original"),
            h(
              "output",
              `${operation.confirmed.value}:${operation.uncertain.value}:${operation.replayable.value}`,
            ),
          ]);
      },
    });
    disposeView = () => view.unmount();
    await view.get("button").trigger("click");
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
    await flushPromises();
    expect(view.get("output").text()).toBe("true:false:false");
    await view.findAll("button")[1]!.trigger("click");
    await flushPromises();
    expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/session",
      "/api/v1/system/smtp/test",
      `/api/v1/system/mail-jobs/${jobId}/management`,
    ]);
  });
});
