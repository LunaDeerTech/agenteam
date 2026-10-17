import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { reactive } from "vue";
import { afterEach, beforeEach, expect, it } from "vitest";
import { isModelInput, type ModelInput } from "../src/system/model-catalog";
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
    draft.notes = "Capability Fixture";
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
function button(text: string) {
  const buttons = wrapper!
    .findAll("button")
    .filter((item) => (item.attributes("aria-label") ?? item.text()) === text);
  expect(buttons, `Exactly one button ${text}`).toHaveLength(1);
  return buttons[0]!;
}
async function click(text: string) {
  const target = button(text);
  expect(target.attributes("disabled")).toBeUndefined();
  (target.element as HTMLButtonElement).focus();
  await target.trigger("click");
}
async function selectOption(labelText: string, text: string) {
  const type = wrapper!.get(`[id="${label(labelText).attributes("for")}"]`);
  expect(type.attributes("disabled")).toBeUndefined();
  await type.trigger("keydown", { key: "ArrowDown" });
  await flushPromises();
  expect(type.attributes("aria-expanded")).toBe("true");
  const listbox = document.getElementById(type.attributes("aria-controls")!);
  expect(listbox?.getAttribute("role")).toBe("listbox");
  const option = [
    ...listbox!.querySelectorAll<HTMLElement>('[role="option"]'),
  ].find((item) => item.textContent?.trim() === text);
  expect(option, `Available model type ${text}`).toBeTruthy();
  option!.click();
  await flushPromises();
  expect(type.text()).toBe(text);
}
const selectType = (text: string) => selectOption("模型类型", text);
async function fillChatSettings() {
  await checkbox("图像").setValue(true);
  await checkbox("工具调用").setValue(true);
  await checkbox("并行工具调用").setValue(true);
  await checkbox("流式输出").setValue(true);
  await checkbox("支持思考").setValue(true);
  await checkbox("支持结构化输出（JSON Schema）").setValue(true);
  await click("添加思考等级");
  await field("等级名称 1").setValue("深入");
  await field("等级值 1").setValue("high");
  await wrapper!.get("summary").trigger("click");
  await field("上下文窗口（tokens，可选）").setValue("128000");
  await field("最大输出（tokens，可选）").setValue("4096");
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
    reasoning_toggle_mode: null,
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
  const notes = label("备注（可选）").get("textarea");
  await notes.setValue("");
  expect(serialize().notes).toBe("");
  expect(wrapper!.findAll("textarea")).toHaveLength(1);
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
  await click("添加思考等级");
  expect(document.activeElement).toBe(field("等级名称 1").element);
  expect(field("等级名称 1").attributes("placeholder")).toBe("名称");
  expect(field("等级值 1").attributes("placeholder")).toBe("值");
  await field("等级名称 1").setValue("快速");
  await field("等级值 1").setValue("low");
  await click("添加思考等级");
  expect(document.activeElement).toBe(field("等级名称 2").element);
  await field("等级名称 2").setValue("深入");
  await field("等级值 2").setValue("vendor.high:2");
  await click("删除等级 1");
  expect(document.activeElement).toBe(field("等级名称 1").element);
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
  await click("删除等级 1");
  expect(serialize().capabilities.reasoning_efforts).toEqual([]);
  expect(document.activeElement).toBe(button("添加思考等级").element);
  await click("添加思考等级");
  await field("等级名称 1").setValue("深入");
  await field("等级值 1").setValue("vendor.high:2");
  await checkbox("支持思考").setValue(false);
  expect(wrapper!.text()).not.toContain("添加思考等级");
  const disabledInput = serialize();
  expect(disabledInput.capabilities).toMatchObject({
    reasoning: false,
    reasoning_toggle_mode: null,
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

it("keeps toggle mapping only for level-free chat reasoning and clears it when hidden", async () => {
  const hasMapping = () =>
    wrapper!.findAll("label").some((item) => item.text() === "思考开关格式");
  expect(hasMapping()).toBe(false);
  await checkbox("支持思考").setValue(true);
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await selectOption("思考开关格式", "thinking.type");
  expect(serialize().capabilities.reasoning_toggle_mode).toBe("thinking_type");
  const saved = serialize();
  wrapper!.unmount();
  mountFields(saved);
  expect(
    wrapper!.get(`[id="${label("思考开关格式").attributes("for")}"]`).text(),
  ).toBe("thinking.type");

  await click("添加思考等级");
  expect(hasMapping()).toBe(false);
  await field("等级名称 1").setValue("深入");
  await field("等级值 1").setValue("high");
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await click("删除等级 1");
  expect(hasMapping()).toBe(true);
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await selectOption("思考开关格式", "enable_thinking");
  expect(serialize().capabilities.reasoning_toggle_mode).toBe(
    "enable_thinking",
  );
  await checkbox("支持思考").setValue(false);
  expect(hasMapping()).toBe(false);
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await checkbox("支持思考").setValue(true);
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await selectOption("思考开关格式", "enable_thinking");
  await selectType("嵌入（Embedding）");
  expect(hasMapping()).toBe(false);
  await field("向量维度").setValue("1536");
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
  await selectType("对话（Chat）");
  expect(hasMapping()).toBe(true);
  expect(serialize().capabilities.reasoning_toggle_mode).toBeNull();
});

it("switches new models to fixed embedding capabilities without leaking chat settings", async () => {
  await fillChatSettings();
  await selectType("嵌入（Embedding）");
  const requestDimensions = () =>
    wrapper!.get('[role="switch"][aria-label="请求指定维度"]');
  expect((field("向量维度").element as HTMLInputElement).value).toBe("");
  expect(requestDimensions().attributes("aria-checked")).toBe("false");
  expect(() => serialize()).toThrow("维度");
  await field("向量维度").setValue("1536");
  expect(serialize().capabilities.embedding).toEqual({
    dimensions: "1536",
    dimensions_mode: "fixed",
  });
  await requestDimensions().trigger("click");
  for (const text of [
    "图像",
    "文件",
    "工具调用",
    "并行工具调用",
    "流式输出",
    "支持思考",
    "支持结构化输出（JSON Schema）",
    "最大输出（tokens，可选）",
  ])
    expect(wrapper!.findAll("label").some((item) => item.text() === text)).toBe(
      false,
    );
  const input = serialize();
  expect(input.type).toBe("embedding");
  expect(input.capabilities).toEqual({
    embedding: { dimensions: "1536", dimensions_mode: "request" },
    input_modalities: ["text"],
    output_modalities: ["vector"],
    tool_calls: false,
    parallel_tool_calls: false,
    streaming: false,
    reasoning: false,
    reasoning_toggle_mode: null,
    reasoning_efforts: [],
    structured_output_modes: [],
    context_length: "128000",
    max_output: null,
  });
  wrapper!.unmount();
  mountFields(input);
  expect((field("向量维度").element as HTMLInputElement).value).toBe("1536");
  expect(requestDimensions().attributes("aria-checked")).toBe("true");
  expect(serialize()).toEqual(input);
  await selectType("对话（Chat）");
  expect(
    wrapper!.findAll("label").some((item) => item.text() === "向量维度"),
  ).toBe(false);
  expect(serialize().capabilities.embedding).toBeNull();
  await selectType("嵌入（Embedding）");
  expect((field("向量维度").element as HTMLInputElement).value).toBe("");
  expect(requestDimensions().attributes("aria-checked")).toBe("false");
  expect(() => serialize()).toThrow("维度");
});

it.each([
  {
    type: "reranker",
    label: "重排（ReRank）",
    inputLabel: "查询与文档",
    outputLabel: "排序结果",
    output: [],
  },
  {
    type: "image_generation",
    label: "图像生成（Image Generation）",
    inputLabel: "文本",
    outputLabel: "图像",
    output: ["image"],
  },
])(
  "switches a configured chat draft to $type and restores only that type's capabilities",
  async ({ type, label: typeLabel, inputLabel, outputLabel, output }) => {
    await fillChatSettings();
    await selectType(typeLabel);
    for (const text of [
      "图像",
      "文件",
      "工具调用",
      "并行工具调用",
      "流式输出",
      "支持思考",
      "支持结构化输出（JSON Schema）",
      "上下文窗口（tokens，可选）",
      "最大输出（tokens，可选）",
    ])
      expect(
        wrapper!.findAll("label").some((item) => item.text() === text),
      ).toBe(false);
    expect(wrapper!.find("details").exists()).toBe(false);
    expect(wrapper!.text()).toContain(inputLabel);
    expect(wrapper!.text()).toContain(outputLabel);

    const input = serialize();
    expect(input.type).toBe(type);
    expect(input.capabilities).toEqual({
      embedding: null,
      input_modalities: ["text"],
      output_modalities: output,
      tool_calls: false,
      parallel_tool_calls: false,
      streaming: false,
      reasoning: false,
      reasoning_toggle_mode: null,
      reasoning_efforts: [],
      structured_output_modes: [],
      context_length: null,
      max_output: null,
    });
    expect(isModelInput(input)).toBe(true);
    wrapper!.unmount();
    mountFields(input);
    expect(wrapper!.get('[role="combobox"]').text()).toBe(typeLabel);
    expect(wrapper!.text()).toContain(inputLabel);
    expect(wrapper!.text()).toContain(outputLabel);
    expect(wrapper!.find("details").exists()).toBe(false);
    expect(serialize()).toEqual(input);
  },
);

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
