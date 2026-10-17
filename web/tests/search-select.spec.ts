import {
  DOMWrapper,
  flushPromises,
  mount,
  type VueWrapper,
} from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import UiSearchSelect from "../src/components/ui/UiSearchSelect.vue";

let wrapper: VueWrapper | undefined;
const options = [
  { value: "a", label: "Alpha/model-a", description: "对话" },
  {
    value: "b",
    label: "Beta/model-b",
    disabledReason: "模型已停用",
  },
  { value: "c", label: "Charlie/model-c", description: "对话" },
];
function render(
  props: Partial<InstanceType<typeof UiSearchSelect>["$props"]> = {},
) {
  wrapper = mount(UiSearchSelect, {
    props: { label: "选择总结模型", options, ...props },
    attachTo: document.body,
  });
  return wrapper;
}
function body(selector: string) {
  const element = document.body.querySelector<HTMLElement>(selector);
  expect(element, `Rendered ${selector}`).not.toBeNull();
  return new DOMWrapper(element!);
}
async function open() {
  await wrapper!.get('button[aria-haspopup="dialog"]').trigger("click");
  await flushPromises();
  return body('input[role="combobox"]');
}
function action(text: string) {
  const buttons = body('[role="dialog"]')
    .findAll("button")
    .filter((item) => item.text() === text);
  expect(buttons).toHaveLength(1);
  return buttons[0]!;
}
beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.replaceChildren();
  vi.unstubAllGlobals();
});

describe("searchable model selection control", () => {
  it("keeps the selected label when the current model is absent from search results or another page", async () => {
    const view = render({
      modelValue: "saved-model",
      selectedLabel: "Saved Provider/saved-model",
    });
    const trigger = view.get('button[aria-haspopup="dialog"]');
    expect(trigger.text()).toBe("Saved Provider/saved-model");
    expect(trigger.attributes("aria-label")).toContain("选择总结模型");
    await open();
    await view.setProps({ options: [], query: "missing", pageIndex: 2 });
    expect(trigger.text()).toBe("Saved Provider/saved-model");
    expect(body('[role="dialog"]').text()).toContain("没有匹配的模型");
    expect(body('[role="dialog"]').text()).toContain("第 3 页");
    expect(view.emitted("update:modelValue")).toBeUndefined();
  });

  it("explains disabled choices and skips them during keyboard selection", async () => {
    const view = render({ modelValue: "a", selectedLabel: "Alpha/model-a" });
    const search = await open();
    const disabled = body('[role="option"][aria-disabled="true"]');
    expect(disabled.text()).toContain("模型已停用");
    expect(
      document.getElementById(disabled.attributes("aria-describedby")!)
        ?.textContent,
    ).toBe("模型已停用");
    await disabled.trigger("click");
    expect(view.emitted("update:modelValue")).toBeUndefined();
    await search.trigger("keydown", { key: "ArrowDown" });
    const activeId = search.attributes("aria-activedescendant")!;
    expect(document.getElementById(activeId)?.textContent).toContain(
      "Charlie/model-c",
    );
    await search.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(view.emitted("update:modelValue")).toEqual([["c"]]);
    expect(view.emitted("open-change")).toEqual([[true], [false]]);
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(
      view.get('button[aria-haspopup="dialog"]').element,
    );
  });

  it("emits search edits for the owner to fetch and follows returned query and options", async () => {
    const view = render();
    const search = await open();
    await search.setValue("another provider/model");
    expect(view.emitted("update:query")).toEqual([["another provider/model"]]);
    expect(view.emitted("update:modelValue")).toBeUndefined();
    await view.setProps({
      query: "another provider/model",
      options: [{ value: "remote", label: "Another Provider/model" }],
    });
    expect((search.element as HTMLInputElement).value).toBe(
      "another provider/model",
    );
    await body('[role="option"]').trigger("click");
    expect(view.emitted("update:modelValue")).toEqual([["remote"]]);
  });

  it("distinguishes loading, failure with retry, and a completed empty result", async () => {
    const view = render({ options: [], loading: true });
    const search = await open();
    expect(body('[role="status"]').text()).toBe("正在读取模型…");
    expect(body('[role="dialog"]').text()).not.toContain("没有匹配的模型");
    await search.trigger("keydown", { key: "Enter" });
    expect(view.emitted("update:modelValue")).toBeUndefined();
    await view.setProps({ loading: false, error: "读取失败，请稍后重试" });
    expect(body('[role="alert"]').text()).toContain("读取失败，请稍后重试");
    expect(body('[role="dialog"]').text()).not.toContain("没有匹配的模型");
    await action("重新读取").trigger("click");
    expect(view.emitted("retry")).toEqual([[]]);
    await view.setProps({ error: undefined });
    expect(body('[role="status"]').text()).toContain("没有匹配的模型");
    expect(document.querySelector('[role="alert"]')).toBeNull();
  });

  it("keeps retry focus in search when results arrive and respects a later move outside", async () => {
    const view = render({
      options: [],
      error: "读取失败，请稍后重试",
      modelValue: "saved-model",
      selectedLabel: "Saved Provider/saved-model",
    });
    const search = await open();
    const retry = action("重新读取");
    (retry.element as HTMLButtonElement).focus();
    expect(document.activeElement).toBe(retry.element);
    await retry.trigger("click");
    expect(view.emitted("retry")).toEqual([[]]);
    expect(document.activeElement).toBe(search.element);
    await view.setProps({ loading: true, error: undefined });
    expect(body('[role="status"]').text()).toBe("正在读取模型…");
    expect(document.activeElement).toBe(search.element);
    await view.setProps({
      loading: false,
      options: [options[2]!],
      pageIndex: 1,
      hasPrevious: true,
    });
    await flushPromises();
    expect(body('[role="dialog"]').text()).toContain("第 2 页");
    expect(body('[role="option"]').text()).toContain("Charlie/model-c");
    expect(document.activeElement).toBe(search.element);
    expect(view.emitted("update:modelValue")).toBeUndefined();

    await view.setProps({ options: [], error: "读取失败，请稍后重试" });
    const nextRetry = action("重新读取");
    (nextRetry.element as HTMLButtonElement).focus();
    await nextRetry.trigger("click");
    expect(view.emitted("retry")).toEqual([[], []]);
    await view.setProps({ loading: true, error: undefined });
    const outside = document.createElement("button");
    outside.textContent = "其他操作";
    document.body.append(outside);
    outside.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    outside.focus();
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    await view.setProps({
      loading: false,
      options: [options[0]!],
      pageIndex: 2,
    });
    await flushPromises();
    expect(document.activeElement).toBe(outside);
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(
      view.get('button[aria-haspopup="dialog"]').attributes("aria-expanded"),
    ).toBe("false");
    expect(view.emitted("open-change")).toEqual([[true], [false]]);
    expect(view.emitted("update:modelValue")).toBeUndefined();
  });

  it("emits page navigation without replacing the current selection and disables unavailable pages", async () => {
    const view = render({ hasNext: true });
    const search = await open();
    expect(action("上一页").attributes("disabled")).toBeDefined();
    await action("下一页").trigger("click");
    expect(view.emitted("next")).toEqual([[]]);
    expect(document.activeElement).toBe(search.element);
    await view.setProps({ pageIndex: 1, hasPrevious: true, hasNext: false });
    expect(body('[role="dialog"]').text()).toContain("第 2 页");
    expect(action("下一页").attributes("disabled")).toBeDefined();
    await action("上一页").trigger("click");
    expect(view.emitted("previous")).toEqual([[]]);
    await view.setProps({ loading: true, hasNext: true });
    expect(action("上一页").attributes("disabled")).toBeDefined();
    expect(action("下一页").attributes("disabled")).toBeDefined();
    expect(view.emitted("update:modelValue")).toBeUndefined();
  });

  it("opens with the keyboard and restores focus on Escape without changing the selection", async () => {
    const view = render();
    const trigger = view.get('button[aria-haspopup="dialog"]');
    (trigger.element as HTMLButtonElement).focus();
    await trigger.trigger("keydown", { key: "ArrowDown" });
    await flushPromises();
    const search = body('input[role="combobox"]');
    expect(document.activeElement).toBe(search.element);
    await search.trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(trigger.attributes("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(trigger.element);
    expect(view.emitted("update:modelValue")).toBeUndefined();
  });

  it("closes for an outside pointer without reclaiming focus and closes when disabled", async () => {
    const view = render();
    await open();
    const outside = document.createElement("button");
    outside.textContent = "其他操作";
    document.body.append(outside);
    outside.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    outside.focus();
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(outside);
    expect(view.emitted("open-change")).toEqual([[true], [false]]);
    await open();
    await view.setProps({ disabled: true });
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(
      view.get('button[aria-haspopup="dialog"]').attributes("disabled"),
    ).toBeDefined();
    expect(view.emitted("update:modelValue")).toBeUndefined();
  });
});
