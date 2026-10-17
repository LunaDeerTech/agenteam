import { mount, type VueWrapper } from "@vue/test-utils";
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
    draft.name = "Modality Fixture";
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
function checkbox(groupLabel: string, optionLabel: string) {
  const matches = group(groupLabel)
    .findAll("label")
    .filter((item) => item.text() === optionLabel);
  expect(
    matches,
    `Exactly one option ${optionLabel} in ${groupLabel}`,
  ).toHaveLength(1);
  return matches[0]!.get('input[type="checkbox"]');
}
function checkedLabels(groupLabel: string) {
  return group(groupLabel)
    .findAll("label")
    .filter(
      (item) =>
        (item.get('input[type="checkbox"]').element as HTMLInputElement)
          .checked,
    )
    .map((item) => item.text());
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

it("serializes independent modality choices without duplicates and respects a disabled checkbox group", async () => {
  await checkbox("输入模态", "文本").setValue(true);
  await checkbox("输入模态", "图像").setValue(true);
  await checkbox("输入模态", "文件").setValue(true);
  await checkbox("输入模态", "图像").setValue(false);
  await checkbox("输入模态", "文本").trigger("change");
  await checkbox("输出模态", "图像").setValue(true);
  await checkbox("输出模态", "向量").setValue(true);
  expect(serialize().capabilities.input_modalities).toEqual(["text", "file"]);
  expect(serialize().capabilities.output_modalities).toEqual([
    "image",
    "vector",
  ]);
  await checkbox("输出模态", "图像").setValue(false);
  expect(serialize().capabilities.input_modalities).toEqual(["text", "file"]);
  expect(serialize().capabilities.output_modalities).toEqual(["vector"]);

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
  (checkbox("已锁定模态", "图像").element as HTMLInputElement).click();
  expect(checkedLabels("已锁定模态")).toEqual(["文本"]);
  expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  await wrapper.setProps({ disabled: false });
  await checkbox("已锁定模态", "图像").setValue(true);
  await checkbox("已锁定模态", "图像").trigger("change");
  expect(checkedLabels("已锁定模态")).toEqual(["文本", "图像"]);
  expect(wrapper.emitted("update:modelValue")!.at(-1)).toEqual([
    ["text", "image"],
  ]);
});

it("restores checked modalities by rebuilding a draft from serialized input and remounting the form", async () => {
  await checkbox("输入模态", "图像").setValue(true);
  await checkbox("输入模态", "文件").setValue(true);
  await checkbox("输出模态", "文本").setValue(true);
  await checkbox("输出模态", "向量").setValue(true);
  const savedInput = serialize();
  wrapper!.unmount();
  mountFields(savedInput);
  expect(checkedLabels("输入模态")).toEqual(["图像", "文件"]);
  expect(checkedLabels("输出模态")).toEqual(["文本", "向量"]);
  expect(serialize()).toEqual(savedInput);
  await checkbox("输入模态", "图像").setValue(false);
  expect(serialize().capabilities.input_modalities).toEqual(["file"]);
  expect(serialize().capabilities.output_modalities).toEqual([
    "text",
    "vector",
  ]);
});

it("keeps required modalities empty until selected and rejects clearing either group", async () => {
  expect(checkedLabels("输入模态")).toEqual([]);
  expect(checkedLabels("输出模态")).toEqual([]);
  expect(() => serialize()).toThrow("请选择模型的输入和输出模态。");
  await checkbox("输入模态", "文本").setValue(true);
  await checkbox("输出模态", "文本").setValue(true);
  expect(serialize().capabilities).toMatchObject({
    input_modalities: ["text"],
    output_modalities: ["text"],
    tool_calls: false,
    parallel_tool_calls: false,
    reasoning: false,
    context_length: null,
    max_output: null,
  });
  await checkbox("输入模态", "文本").setValue(false);
  expect(() => serialize()).toThrow("请选择模型的输入和输出模态。");
  expect(checkedLabels("输入模态")).toEqual([]);
  expect(checkedLabels("输出模态")).toEqual(["文本"]);
  await checkbox("输入模态", "文本").setValue(true);
  await checkbox("输出模态", "文本").setValue(false);
  expect(() => serialize()).toThrow("请选择模型的输入和输出模态。");
  expect(checkedLabels("输入模态")).toEqual(["文本"]);
  expect(checkedLabels("输出模态")).toEqual([]);
});
