import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import UiDialog from "../src/components/ui/UiDialog.vue";

const wrappers: VueWrapper[] = [];
const originalShowModal = Object.getOwnPropertyDescriptor(
  HTMLDialogElement.prototype,
  "showModal",
);
const originalClose = Object.getOwnPropertyDescriptor(
  HTMLDialogElement.prototype,
  "close",
);
beforeEach(() => {
  // jsdom has no native top layer. Only model the public open property here;
  // browser tests prove focus trapping, inert background, and pointer dismissal.
  Object.defineProperties(HTMLDialogElement.prototype, {
    showModal: {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = true;
      },
    },
    close: {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = false;
      },
    },
  });
});
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount();
  for (const [name, descriptor] of [
    ["showModal", originalShowModal],
    ["close", originalClose],
  ] as const) {
    if (descriptor)
      Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
  document.body.replaceChildren();
  document.body.style.cssText = "";
  document.documentElement.style.cssText = "";
});

function createDialog(props: {
  open: boolean;
  title: string;
  closeOnEscape?: boolean;
}) {
  const wrapper = mount(UiDialog, {
    props,
    attachTo: document.body,
    slots: { default: "<button>继续</button>" },
  });
  wrappers.push(wrapper);
  return wrapper;
}

describe("controlled dialog", () => {
  it("reports cancellation without changing the owner value and restores focus when closed", async () => {
    const trigger = document.createElement("button");
    trigger.textContent = "打开";
    document.body.append(trigger);
    trigger.focus();
    const wrapper = createDialog({ open: false, title: "确认操作" });
    await wrapper.setProps({ open: true });
    await flushPromises();
    const dialog = document.querySelector("dialog")!;
    expect(dialog.open).toBe(true);
    dialog.dispatchEvent(new Event("cancel", { cancelable: true }));
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    expect(wrapper.emitted("close")).toEqual([["escape"]]);
    expect(dialog.open).toBe(true);
    await wrapper.setProps({ open: false });
    await flushPromises();
    expect(dialog.open).toBe(false);
    expect(document.activeElement).toBe(trigger);
  });

  it("honors disabled Escape while leaving the explicit close action usable", async () => {
    const wrapper = createDialog({
      open: true,
      title: "保留输入",
      closeOnEscape: false,
    });
    await flushPromises();
    document
      .querySelector("dialog")!
      .dispatchEvent(new Event("cancel", { cancelable: true }));
    expect(wrapper.emitted("update:open")).toBeUndefined();
    document
      .querySelector<HTMLButtonElement>('dialog button[aria-label="关闭"]')!
      .click();
    expect(wrapper.emitted("update:open")).toEqual([[false]]);
    expect(wrapper.emitted("close")).toEqual([["action"]]);
  });

  it("keeps scrolling locked until the last open dialog closes and restores the prior style", async () => {
    document.body.style.overflow = "scroll";
    const first = createDialog({ open: true, title: "第一层" });
    const second = createDialog({ open: true, title: "第二层" });
    await flushPromises();
    expect(document.body.style.overflow).toBe("hidden");
    await first.setProps({ open: false });
    await flushPromises();
    expect(document.body.style.overflow).toBe("hidden");
    second.unmount();
    wrappers.splice(wrappers.indexOf(second), 1);
    expect(document.body.style.overflow).toBe("scroll");
  });
});
