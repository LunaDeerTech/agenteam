import { mount, flushPromises } from "@vue/test-utils";
import { h } from "vue";
import { expect, it, vi } from "vitest";
import { useCommand } from "../src/account/useCommand";
import { AccountError } from "../src/account/transport";
import type { AccountCommand } from "../src/account/session";

it("does not let an abandoned verification's late failure replace a new command state", async () => {
  let operation!: ReturnType<typeof useCommand<string>>;
  const view = mount({
    setup() {
      operation = useCommand<string>();
      return () =>
        h("output", `${operation.busy.value}:${operation.uncertain.value}`);
    },
  });
  let rejectOld!: (reason: unknown) => void;
  let finishNew!: (value: string) => void;
  const oldResult = new Promise<string>((_resolve, reject) => {
    rejectOld = reject;
  });
  const newResult = new Promise<string>((resolve) => {
    finishNew = resolve;
  });
  const oldCommand: AccountCommand<string> = {
    key: "old",
    active: true,
    confirmed: false,
    run: () => oldResult,
    release: vi.fn(),
  };
  let confirmed = false;
  const newCommand: AccountCommand<string> = {
    key: "new",
    active: true,
    get confirmed() {
      return confirmed;
    },
    run: async (onConfirmed) => {
      const value = await newResult;
      confirmed = true;
      onConfirmed?.();
      return value;
    },
    release: vi.fn(),
  };
  const received = vi.fn();
  try {
    const old = operation.submit(() => oldCommand);
    await flushPromises();
    operation.abandon();
    const next = operation.submit(() => newCommand, received);
    await flushPromises();
    rejectOld(new AccountError("NETWORK_ERROR", 0, "unknown"));
    await old;
    expect(operation.busy.value).toBe(true);
    expect(operation.uncertain.value).toBe(false);
    expect(operation.error.value).toBeUndefined();
    expect(oldCommand.release).toHaveBeenCalledOnce();
    finishNew("new verification");
    await next;
    expect(received).toHaveBeenCalledWith("new verification");
    expect(operation.confirmed.value).toBe(true);
    expect(operation.busy.value).toBe(false);
  } finally {
    view.unmount();
  }
});
