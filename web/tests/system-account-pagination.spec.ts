import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount } from "@vue/test-utils";
import { h, ref, watch } from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { SessionResponse } from "../src/account/types";

const session: SessionResponse = {
  user: {
    id: "019a2222-2222-7222-8222-222222222222",
    email: "pagination@example.test",
    username: "pagination",
    display_name: "Pagination",
    role: "admin",
    theme: "system",
    version: "1",
    initial_password_suggestion: false,
  },
  session: {
    id: "019a1111-1111-7111-8111-111111111111",
    issued_at: "2026-10-16T12:00:00.000000Z",
    absolute_expires_at: "2026-10-17T12:00:00.000000Z",
    idle_expires_at: "2026-10-16T18:00:00.000000Z",
  },
  csrf_token: "synthetic-pagination-context",
};
const rows = (count: number, prefix = "page") =>
  Array.from({ length: count }, (_, index) => ({
    ...session.user,
    id: `019a2222-2222-7222-8222-${String(999999999999 - index)}`,
    email: `${prefix}-${index}@example.test`,
    created_at: "2026-10-16T12:00:00.000000Z",
  }));
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
let dispose: (() => void) | undefined;
let disposeTheme: (() => void) | undefined;
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
  dispose?.();
  dispose = undefined;
  disposeTheme?.();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

it("binds directory response limits to each selected page size", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(json(session));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const { listUsers } = await import("../src/system/directory");
  for (const size of [5, 20, 50]) {
    fetcher.mockResolvedValueOnce(json({ items: rows(size) }));
    await expect(listUsers(undefined, undefined, size)).resolves.toMatchObject({
      items: rows(size),
    });
    expect(
      new URL(
        String(fetcher.mock.calls.at(-1)![0]),
        "https://fixture.test",
      ).searchParams.get("limit"),
    ).toBe(String(size));
    fetcher.mockResolvedValueOnce(json({ items: rows(size + 1) }));
    await expect(listUsers(undefined, undefined, size)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  }
});

it("retires the old cursor on size change without publishing late rows or releasing the shared HTTP owner early", async () => {
  let body!: ReadableStreamDefaultController<Uint8Array>;
  const oldRows = rows(5, "late-old-size");
  const bytes = new TextEncoder().encode(JSON.stringify({ items: oldRows }));
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      body = controller;
    },
  });
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(session))
    .mockResolvedValueOnce(json({ items: rows(10), next_cursor: "cursor-ten" }))
    .mockResolvedValueOnce(json({ items: rows(5), next_cursor: "cursor-five" }))
    .mockResolvedValueOnce(
      new Response(stream, { headers: { "Content-Type": "application/json" } }),
    )
    .mockResolvedValueOnce(json({ items: rows(12, "new-size") }));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const { listUsers } = await import("../src/system/directory");
  const { useCursorList } = await import("../src/system/useCursorList");
  const published: string[] = [];
  const view = mount({
    setup() {
      const list = useCursorList(listUsers);
      watch(
        list.items,
        (value) => published.push(value.map((item) => item.email).join(",")),
        { flush: "sync" },
      );
      return () =>
        h("section", [
          h(
            "output",
            { "data-page": list.index.value, "data-size": list.pageSize.value },
            list.items.value.map((item) => item.email).join(","),
          ),
          h("button", { onClick: () => list.setPageSize(5) }, "Five"),
          h("button", { onClick: list.next }, "Next"),
          h("button", { onClick: () => list.setPageSize(20) }, "Twenty"),
        ]);
    },
  });
  dispose = () => view.unmount();
  await vi.waitFor(() => expect(view.get("output").text()).toContain("page-9"));
  await view.get("button:nth-of-type(1)").trigger("click");
  await vi.waitFor(() =>
    expect(view.get("output").attributes("data-size")).toBe("5"),
  );
  await flushPromises();
  await view.get("button:nth-of-type(2)").trigger("click");
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
  body.enqueue(bytes.slice(0, 8));
  await view.get("button:nth-of-type(3)").trigger("click");
  await flushPromises();
  expect(view.get("output").text()).toBe("");
  expect(fetcher).toHaveBeenCalledTimes(4);
  body.enqueue(bytes.slice(8));
  body.close();
  await vi.waitFor(() =>
    expect(view.get("output").text()).toContain("new-size-11"),
  );
  expect(published.some((value) => value.includes("late-old-size"))).toBe(
    false,
  );
  expect(view.get("output").attributes("data-page")).toBe("0");
  const finalURL = new URL(
    String(fetcher.mock.calls.at(-1)![0]),
    "https://fixture.test",
  );
  expect([...finalURL.searchParams.entries()]).toEqual([["limit", "20"]]);
});

it("preserves the successful cursor page during refresh failure and retries that page without discarding rows", async () => {
  let failRefresh!: (response: Response) => void;
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(session))
    .mockResolvedValueOnce(
      json({ items: rows(10), next_cursor: "cursor-page-two" }),
    )
    .mockResolvedValueOnce(json({ items: rows(3, "kept") }))
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          failRefresh = resolve;
        }),
    )
    .mockResolvedValueOnce(json({ items: rows(3, "updated") }));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const { listUsers } = await import("../src/system/directory");
  const { useCursorList } = await import("../src/system/useCursorList");
  const view = mount({
    setup() {
      const list = useCursorList(listUsers);
      return () =>
        h("section", [
          h(
            "output",
            {
              "data-page": list.index.value,
              "data-refreshing": list.refreshing.value,
              "data-loading": list.loading.value,
            },
            list.items.value.map((item) => item.email).join(","),
          ),
          list.error.value
            ? h("p", { role: "alert" }, "Read failed")
            : undefined,
          h("button", { onClick: list.next }, "Next"),
          h(
            "button",
            { onClick: () => list.refresh({ preserve: true }) },
            "Refresh",
          ),
          h(
            "button",
            { onClick: () => list.reload({ preserve: true }) },
            "Retry",
          ),
        ]);
    },
  });
  dispose = () => view.unmount();
  await vi.waitFor(() => expect(view.get("output").text()).toContain("page-9"));
  await view.get("button:nth-of-type(1)").trigger("click");
  await vi.waitFor(() => expect(view.get("output").text()).toContain("kept-2"));
  const kept = view.get("output").text();
  await view.get("button:nth-of-type(2)").trigger("click");
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
  expect(view.get("output").text()).toBe(kept);
  expect(view.get("output").attributes()).toMatchObject({
    "data-page": "1",
    "data-refreshing": "true",
    "data-loading": "false",
  });
  failRefresh(
    new Response(
      JSON.stringify({ code: "UNAVAILABLE", commit_state: "not_started" }),
      { status: 503, headers: { "Content-Type": "application/problem+json" } },
    ),
  );
  await vi.waitFor(() =>
    expect(view.find('[role="alert"]').exists()).toBe(true),
  );
  expect(view.get("output").text()).toBe(kept);
  expect(view.get("output").attributes("data-page")).toBe("1");
  await view.get("button:nth-of-type(3)").trigger("click");
  await vi.waitFor(() =>
    expect(view.get("output").text()).toContain("updated-2"),
  );
  expect(view.get("output").attributes("data-page")).toBe("1");
  expect(view.find('[role="alert"]').exists()).toBe(false);
  for (const call of fetcher.mock.calls.slice(2)) {
    const url = new URL(String(call[0]), "https://fixture.test");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      limit: "10",
      cursor: "cursor-page-two",
    });
  }
});

it("keeps an inactive list's current page and rejects its late refresh until explicitly reactivated", async () => {
  let body!: ReadableStreamDefaultController<Uint8Array>;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      body = controller;
    },
  });
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(session))
    .mockResolvedValueOnce(json({ items: rows(10), next_cursor: "cursor-two" }))
    .mockResolvedValueOnce(json({ items: rows(3, "kept") }))
    .mockResolvedValueOnce(
      new Response(stream, { headers: { "Content-Type": "application/json" } }),
    )
    .mockResolvedValueOnce(json({ items: rows(3, "reactivated") }));
  vi.stubGlobal("fetch", fetcher);
  await (await import("../src/account/session")).refreshSession();
  const { listUsers } = await import("../src/system/directory");
  const { useCursorList } = await import("../src/system/useCursorList");
  const enabled = ref(false);
  const published: string[] = [];
  const view = mount({
    setup() {
      const list = useCursorList(listUsers, {
        enabled: () => enabled.value,
        immediate: false,
      });
      watch(
        list.items,
        (value) => published.push(value.map((item) => item.email).join(",")),
        { flush: "sync" },
      );
      return () =>
        h("section", [
          h(
            "output",
            {
              "data-page": list.index.value,
              "data-pending": list.pending.value,
            },
            list.items.value.map((item) => item.email).join(","),
          ),
          list.error.value
            ? h("p", { role: "alert" }, "Read failed")
            : undefined,
          h("button", { onClick: () => list.first() }, "Read"),
          h("button", { onClick: list.next }, "Next"),
          h(
            "button",
            { onClick: () => list.refresh({ preserve: true }) },
            "Refresh",
          ),
        ]);
    },
  });
  dispose = () => view.unmount();
  await view.get("button:nth-of-type(1)").trigger("click");
  expect(fetcher).toHaveBeenCalledTimes(1);
  enabled.value = true;
  await flushPromises();
  expect(fetcher).toHaveBeenCalledTimes(1);
  await view.get("button:nth-of-type(1)").trigger("click");
  await vi.waitFor(() => expect(view.get("output").text()).toContain("page-9"));
  await view.get("button:nth-of-type(2)").trigger("click");
  await vi.waitFor(() => expect(view.get("output").text()).toContain("kept-2"));
  await view.get("button:nth-of-type(3)").trigger("click");
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
  enabled.value = false;
  await flushPromises();
  expect(view.get("output").text()).toContain("kept-2");
  expect(view.get("output").attributes()).toMatchObject({
    "data-page": "1",
    "data-pending": "false",
  });
  body.enqueue(
    new TextEncoder().encode(JSON.stringify({ items: rows(3, "retired") })),
  );
  body.close();
  await flushPromises();
  expect(published.some((value) => value.includes("retired"))).toBe(false);
  expect(view.find('[role="alert"]').exists()).toBe(false);
  enabled.value = true;
  await flushPromises();
  expect(fetcher).toHaveBeenCalledTimes(4);
  await view.get("button:nth-of-type(3)").trigger("click");
  await vi.waitFor(() =>
    expect(view.get("output").text()).toContain("reactivated-2"),
  );
  expect(view.get("output").attributes("data-page")).toBe("1");
  const url = new URL(
    String(fetcher.mock.calls.at(-1)![0]),
    "https://fixture.test",
  );
  expect(Object.fromEntries(url.searchParams)).toEqual({
    limit: "10",
    cursor: "cursor-two",
  });
});
