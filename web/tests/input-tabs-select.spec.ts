import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, describe, expect, it } from "vitest";
import { UiInput, UiSelect, UiTabs } from "../src/components/ui";

let wrapper: VueWrapper | undefined;
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.replaceChildren();
});

describe("shared field contracts", () => {
  it("connects the input label and descriptions, forwards native attributes, and emits edits", async () => {
    wrapper = mount(UiInput, {
      props: {
        label: "名称",
        hint: "填写名称",
        error: "名称不可用",
        modelValue: "原值",
      },
      attrs: {
        name: "displayName",
        maxlength: 20,
        autocomplete: "off",
        "aria-describedby": "external-help",
      },
    });
    const input = wrapper.get("input");
    expect(wrapper.get("label").attributes("for")).toBe(input.attributes("id"));
    const ids = input.attributes("aria-describedby")!.split(" ");
    expect(ids[0]).toBe("external-help");
    expect(wrapper.get(`[id="${ids[1]}"]`).text()).toBe("填写名称");
    expect(wrapper.get(`[id="${ids[2]}"]`).text()).toBe("名称不可用");
    expect(input.attributes("aria-invalid")).toBe("true");
    expect(input.attributes("name")).toBe("displayName");
    expect(input.attributes("maxlength")).toBe("20");
    expect(input.attributes("autocomplete")).toBe("off");
    await input.setValue("新名称");
    expect(wrapper.emitted("update:modelValue")).toEqual([["新名称"]]);
    await wrapper.setProps({
      modelValue: "外部更新",
      error: undefined,
      disabled: true,
    });
    expect((input.element as HTMLInputElement).value).toBe("外部更新");
    expect(input.attributes("aria-invalid")).toBeUndefined();
    expect((input.element as HTMLInputElement).disabled).toBe(true);
  });

  it("selects only enabled options through the keyboard and follows external model updates", async () => {
    wrapper = mount(UiSelect, {
      props: {
        label: "选择项",
        modelValue: "a",
        options: [
          { value: "a", label: "Alpha" },
          { value: "b", label: "Beta", disabled: true },
          { value: "c", label: "Charlie" },
        ],
      },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[role="combobox"]');
    await trigger.trigger("keydown", { key: "ArrowDown" });
    await flushPromises();
    await trigger.trigger("keydown", { key: "ArrowDown" });
    const activeId = trigger.attributes("aria-activedescendant")!;
    expect(document.getElementById(activeId)?.textContent?.trim()).toBe(
      "Charlie",
    );
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["c"]]);
    expect(trigger.attributes("aria-expanded")).toBe("false");
    await wrapper.setProps({ modelValue: "c" });
    expect(trigger.text()).toBe("Charlie");
    await trigger.trigger("click");
    await flushPromises();
    await wrapper.setProps({ disabled: true });
    await flushPromises();
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect((trigger.element as HTMLButtonElement).disabled).toBe(true);
  });

  it("handles an empty select without emitting an invented choice", async () => {
    wrapper = mount(UiSelect, {
      props: { label: "空选项", options: [] },
      attachTo: document.body,
    });
    const trigger = wrapper.get('[role="combobox"]');
    await trigger.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(document.querySelector('[role="listbox"]')?.textContent).toContain(
      "无可选项",
    );
    await trigger.trigger("keydown", { key: "Enter" });
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
    await trigger.trigger("keydown", { key: "Escape" });
    expect(trigger.attributes("aria-expanded")).toBe("false");
  });
});

describe("tabs accessible selection", () => {
  it("skips disabled tabs, emits the selected value and associates the panel with its active tab", async () => {
    wrapper = mount(UiTabs, {
      props: {
        label: "内容分组",
        modelValue: "first",
        items: [
          { value: "first", label: "第一项" },
          { value: "disabled", label: "不可用", disabled: true },
          { value: "last", label: "最后一项" },
        ],
      },
      slots: { default: ({ value }: { value: string }) => `当前 ${value}` },
      attachTo: document.body,
    });
    const tabs = wrapper.findAll('[role="tab"]');
    await tabs[0]!.trigger("keydown", { key: "ArrowRight" });
    expect(wrapper.emitted("update:modelValue")).toEqual([["last"]]);
    await wrapper.setProps({ modelValue: "last" });
    expect(tabs[2]!.attributes("aria-selected")).toBe("true");
    expect(tabs[0]!.attributes("tabindex")).toBe("-1");
    const panel = wrapper.get('[role="tabpanel"]');
    expect(panel.text()).toBe("当前 last");
    expect(panel.attributes("aria-labelledby")).toBe(tabs[2]!.attributes("id"));
    expect(tabs[2]!.attributes("aria-controls")).toBe(panel.attributes("id"));
    await tabs[2]!.trigger("keydown", { key: "Home" });
    expect(wrapper.emitted("update:modelValue")?.at(-1)).toEqual(["first"]);
  });
});
