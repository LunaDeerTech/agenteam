import { Blob as NodeBlob } from "node:buffer";
import { flushPromises, mount } from "@vue/test-utils";
import { h, watch } from "vue";
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
