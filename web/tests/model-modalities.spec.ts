import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { afterEach, beforeEach, expect, it } from "vitest";
import type { ModelInput } from "../src/system/model-catalog";
import {
  modelDraft,
  modelDraftInput,
  type ModelDraft,
} from "../src/system/model-draft";

let wrapper: VueWrapper | undefined;
let draft: ModelDraft;
let Fields: (typeof import("../src/components/system/ModelFormFields.vue"))["default"];
let CheckboxGroup: (typeof import("../src/components/ui/UiCheckboxGroup.vue"))["default"];

function mountFields(input?: ModelInput) {
  draft = reactive(modelDraft("openai-compatible", input));
  if (!input) {
    draft.name = "Capability Fixture";
    draft.providerModelId = "upstream-fixture";
    draft.type = "chat";
  }
  wrapper = mount(Fields, {
    props: { draft, protocol: "openai-compatible" },
    attachTo: document.body,
  });
}
function group(label: string) {
  const matches = wrapper!
    .findAll("fieldset")
    .filter((item) => item.find("legend").text() === label);
  expect(matches, `Exactly one group ${label}`).toHaveLength(1);
  return matches[0]!;
}
function label(text: string) {
  const matches = wrapper!
    .findAll("label")
    .filter((item) => item.text() === text);
  expect(matches, `Exactly one label ${text}`).toHaveLength(1);
  return matches[0]!;
}
const checkbox = (text: string) => label(text).get('input[type="checkbox"]');
function field(text: string) {
  return wrapper!.get(`input[id="${label(text).attributes("for")}"]`);
}
async function click(text: string) {
  const buttons = wrapper!
    .findAll("button")
    .filter((item) => (item.attributes("aria-label") ?? item.text()) === text);
  expect(buttons, `Exactly one button ${text}`).toHaveLength(1);
  expect(buttons[0]!.attributes("disabled")).toBeUndefined();
  await buttons[0]!.trigger("click");
}
const serialize = () => modelDraftInput(draft, "openai-compatible");

beforeEach(async () => {
  Fields = (await import("../src/components/system/ModelFormFields.vue"))
    .default;
  CheckboxGroup = (await import("../src/components/ui/UiCheckboxGroup.vue"))
    .default;
  mountFields();
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.replaceChildren();
});

it("keeps mandatory chat text while changing optional media and JSON Schema support", async () => {
  expect(serialize().capabilities).toMatchObject({
    input_modalities: ["text"],
    output_modalities: ["text"],
    structured_output_modes: ["text"],
    tool_calls: false,
    reasoning: false,
    reasoning_efforts: [],
    context_length: null,
    max_output: null,
  });
  expect(group("输入模态").text()).not.toContain("向量");
  expect((checkbox("文本").element as HTMLInputElement).disabled).toBe(true);
  await checkbox("图像").setValue(true);
  await checkbox("文件").setValue(true);
  await checkbox("图像").trigger("change");
  expect(serialize().capabilities.input_modalities).toEqual([
    "text",
    "image",
    "file",
  ]);
  await checkbox("图像").setValue(false);
  expect(serialize().capabilities.input_modalities).toEqual(["text", "file"]);
  await checkbox("支持结构化输出（JSON Schema）").setValue(true);
  expect(serialize().capabilities.structured_output_modes).toEqual([
    "text",
    "json_schema",
  ]);
  await checkbox("支持结构化输出（JSON Schema）").setValue(false);
  await checkbox("文件").setValue(false);
  expect(serialize().capabilities.input_modalities).toEqual(["text"]);
  expect(serialize().capabilities.output_modalities).toEqual(["text"]);
  expect(serialize().capabilities.structured_output_modes).toEqual(["text"]);
  expect(wrapper!.find("textarea").exists()).toBe(false);
  expect(serialize()).toMatchObject({
    parameters: {},
    request_overwrite: {},
    header_overwrite: {},
  });
});

it("adds and removes explicit reasoning levels, restores saved input, and never invents a default level", async () => {
  await checkbox("支持思考").setValue(true);
  expect(serialize().capabilities).toMatchObject({
    reasoning: true,
    reasoning_efforts: [],
  });
  await click("添加等级");
  await field("等级名称 1").setValue("快速");
  await field("等级值 1").setValue("low");
  await click("添加等级");
  await field("等级名称 2").setValue("深入");
  await field("等级值 2").setValue("vendor.high:2");
  await click("删除等级 1");
  expect((field("等级名称 1").element as HTMLInputElement).value).toBe("深入");
  await checkbox("支持结构化输出（JSON Schema）").setValue(true);
  const savedInput = serialize();
  expect(savedInput.capabilities.reasoning_efforts).toEqual([
    { name: "深入", value: "vendor.high:2" },
  ]);
  wrapper!.unmount();
  mountFields(savedInput);
  expect((checkbox("支持思考").element as HTMLInputElement).checked).toBe(true);
  expect(
    (checkbox("支持结构化输出（JSON Schema）").element as HTMLInputElement)
      .checked,
  ).toBe(true);
  expect((field("等级名称 1").element as HTMLInputElement).value).toBe("深入");
  expect((field("等级值 1").element as HTMLInputElement).value).toBe(
    "vendor.high:2",
  );
  expect(serialize()).toEqual(savedInput);
  await checkbox("支持思考").setValue(false);
  expect(wrapper!.text()).not.toContain("添加等级");
  const disabledInput = serialize();
  expect(disabledInput.capabilities).toMatchObject({
    reasoning: false,
    reasoning_efforts: [],
  });
  wrapper!.unmount();
  mountFields(disabledInput);
  await checkbox("支持思考").setValue(true);
  expect(serialize().capabilities).toMatchObject({
    reasoning: true,
    reasoning_efforts: [],
  });
});

it("switches new models to fixed embedding capabilities without leaking chat settings", async () => {
  await checkbox("图像").setValue(true);
  await checkbox("工具调用").setValue(true);
  await checkbox("并行工具调用").setValue(true);
  await checkbox("流式输出").setValue(true);
  await checkbox("支持思考").setValue(true);
  await checkbox("支持结构化输出（JSON Schema）").setValue(true);
  await click("添加等级");
  await field("等级名称 1").setValue("深入");
  await field("等级值 1").setValue("high");
  await wrapper!.get("summary").trigger("click");
  await field("上下文长度（可选）").setValue("128000");
  await field("最大输出（可选）").setValue("4096");
  const type = wrapper!.get('[role="combobox"]');
  expect(type.attributes("disabled")).toBeUndefined();
  await type.trigger("keydown", { key: "End" });
  await flushPromises();
  await type.trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(type.text()).toBe("向量");
  for (const text of [
    "图像",
    "文件",
    "工具调用",
    "并行工具调用",
    "流式输出",
    "支持思考",
    "支持结构化输出（JSON Schema）",
    "最大输出（可选）",
  ])
    expect(wrapper!.findAll("label").some((item) => item.text() === text)).toBe(
      false,
    );
  const input = serialize();
  expect(input.type).toBe("embedding");
  expect(input.capabilities).toEqual({
    input_modalities: ["text"],
    output_modalities: ["vector"],
    tool_calls: false,
    parallel_tool_calls: false,
    streaming: false,
    reasoning: false,
    reasoning_efforts: [],
    structured_output_modes: [],
    context_length: "128000",
    max_output: null,
  });
  wrapper!.unmount();
  mountFields(input);
  expect(serialize()).toEqual(input);
});

it("uses a native disabled fieldset and does not duplicate selected values on repeated changes", async () => {
  wrapper!.unmount();
  wrapper = mount(CheckboxGroup, {
    props: {
      label: "已锁定模态",
      options: [
        { value: "text", label: "文本" },
        { value: "image", label: "图像" },
      ],
      modelValue: ["text"],
      disabled: true,
      "onUpdate:modelValue": (value: string[]) => {
        void wrapper!.setProps({ modelValue: value });
      },
    },
    attachTo: document.body,
  });
  expect((group("已锁定模态").element as HTMLFieldSetElement).disabled).toBe(
    true,
  );
  (checkbox("图像").element as HTMLInputElement).click();
  expect((checkbox("图像").element as HTMLInputElement).checked).toBe(false);
  expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  await wrapper.setProps({ disabled: false });
  await checkbox("图像").setValue(true);
  await checkbox("图像").trigger("change");
  expect((checkbox("文本").element as HTMLInputElement).checked).toBe(true);
  expect((checkbox("图像").element as HTMLInputElement).checked).toBe(true);
  expect(wrapper.emitted("update:modelValue")!.at(-1)).toEqual([
    ["text", "image"],
  ]);
});
