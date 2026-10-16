import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import { afterEach, describe, expect, it } from "vitest";
import UiMenu from "../src/components/ui/UiMenu.vue";

let wrapper: VueWrapper | undefined;
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.replaceChildren();
});

const items = [
  { id: "light", label: "浅色", checked: false },
  { id: "unavailable", label: "不可用", disabled: true },
  { id: "dark", label: "深色", checked: true },
];

function menuItem(label: string) {
  const element = [
    ...document.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]'),
  ].find((item) => item.textContent?.trim() === label);
  if (!element) throw new Error(`Missing menu item: ${label}`);
  return element;
}

function key(element: Element, value: string) {
  element.dispatchEvent(
    new KeyboardEvent("keydown", {
      key: value,
      bubbles: true,
      cancelable: true,
    }),
  );
}

describe("shared menu public behavior", () => {
  it("navigates enabled choices and emits the selected identity once", async () => {
    wrapper = mount(UiMenu, {
      props: { label: "外观", items },
      attachTo: document.body,
    });
    const trigger = wrapper.get("button");
    await trigger.trigger("keydown", { key: "ArrowDown" });
    await flushPromises();
    expect(trigger.attributes("aria-expanded")).toBe("true");
    expect(menuItem("深色").getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(menuItem("浅色"));
    key(menuItem("浅色"), "ArrowDown");
    expect(document.activeElement).toBe(menuItem("深色"));
    menuItem("不可用").click();
    expect(wrapper.emitted("select")).toBeUndefined();
    menuItem("深色").click();
    await flushPromises();
    expect(wrapper.emitted("select")).toEqual([["dark"]]);
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(trigger.element);
  });

  it("cancels with Escape and lets an outside control keep focus", async () => {
    const outside = document.createElement("button");
    outside.textContent = "下一操作";
    document.body.append(outside);
    wrapper = mount(UiMenu, {
      props: { label: "操作", items },
      attachTo: document.body,
    });
    const trigger = wrapper.get("button");
    await trigger.trigger("click");
    await flushPromises();
    key(menuItem("浅色"), "Escape");
    await flushPromises();
    expect(document.activeElement).toBe(trigger.element);
    expect(wrapper.emitted("select")).toBeUndefined();
    await trigger.trigger("click");
    await flushPromises();
    outside.focus();
    await flushPromises();
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(outside);
  });

  it("does not open a disabled trigger and closes when it becomes disabled", async () => {
    wrapper = mount(UiMenu, {
      props: { label: "操作", items, disabled: true },
      attachTo: document.body,
    });
    const trigger = wrapper.get("button");
    (trigger.element as HTMLButtonElement).click();
    await flushPromises();
    expect(document.querySelector('[role="menu"]')).toBeNull();
    await wrapper.setProps({ disabled: false });
    await trigger.trigger("click");
    await flushPromises();
    expect(document.querySelector('[role="menu"]')).not.toBeNull();
    await wrapper.setProps({ disabled: true });
    await flushPromises();
    expect(document.querySelector('[role="menu"]')).toBeNull();
    expect(wrapper.emitted("select")).toBeUndefined();
  });
});
