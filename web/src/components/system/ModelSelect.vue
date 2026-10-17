<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { UiSearchSelect } from "../ui";
import {
  catalogError,
  listModels,
  modelDisplayName,
  modelTypeChoices,
  modelTypes,
  modelReasoningIssue,
  type CatalogModelOption,
  type ReasoningChoice,
  type ModelType,
} from "../../system/model-catalog";
import { useCursorList } from "../../system/useCursorList";
import { useSystemAccess } from "../../system/requests";
const props = defineProps<{
  label: string;
  requiredKind: ModelType;
  modelValue?: string;
  selectedLabel?: string;
  disabled?: boolean;
  allowReasoning?: boolean;
  reasoning?: ReasoningChoice;
  issue: (model: CatalogModelOption) => string | undefined;
}>();
const emit = defineEmits<{
  select: [model: CatalogModelOption, reasoning?: ReasoningChoice];
}>();
const id = useId();
const access = useSystemAccess();
const validKind = computed(() => modelTypes.includes(props.requiredKind));
const opened = ref(false);
const query = ref("");
const searched = ref("");
const waiting = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
const list = useCursorList(
  (cursor, current, limit) =>
    listModels(
      undefined,
      cursor,
      current,
      limit,
      searched.value,
      props.requiredKind,
    ),
  {
    enabled: () => opened.value && !props.disabled && validKind.value,
    immediate: false,
  },
);
const queryIssue = computed(() =>
  new TextEncoder().encode(query.value.trim()).length > 128 ||
  /[\u0000-\u001f\u007f-\u009f]/u.test(query.value)
    ? "搜索词需在 128 字节内，且不能包含控制字符。"
    : undefined,
);
function capacityTag(label: string, value: string | null) {
  if (!value) return [];
  const count = BigInt(value);
  let display = value;
  if (count >= 1000n) {
    const tenths = count / 100n;
    display = `${tenths / 10n}${tenths % 10n ? `.${tenths % 10n}` : ""}K`;
  }
  return [{ label: display, title: `${label} ${value} tokens` }];
}
function modelTags(model: CatalogModelOption) {
  const input = model.input;
  const capabilities = input.capabilities;
  const tags: { label: string; title?: string }[] = [
    {
      label: modelTypeChoices.find((choice) => choice.value === input.type)!
        .label,
    },
  ];
  if (input.type === "chat") {
    tags.push(...capacityTag("上下文", capabilities.context_length));
    if (capabilities.input_modalities.includes("image"))
      tags.push({ label: "图片", title: "图片输入" });
    if (capabilities.input_modalities.includes("file"))
      tags.push({ label: "文件", title: "文件输入" });
  } else if (input.type === "embedding") {
    if (capabilities.embedding)
      tags.push({ label: `${capabilities.embedding.dimensions} 维` });
    tags.push(...capacityTag("输入", capabilities.context_length));
  } else if (
    input.type === "image_generation" &&
    capabilities.input_modalities.includes("text") &&
    capabilities.output_modalities.includes("image")
  )
    tags.push({ label: "文生图" });
  return tags;
}
const candidates = computed(() =>
  list.items.value.filter((model) => model.input.type === props.requiredKind),
);
const emptyText = computed(
  () =>
    `没有匹配的${modelTypeChoices.find((choice) => choice.value === props.requiredKind)?.label ?? "此类型"}模型。`,
);
const options = computed(() =>
  candidates.value.map((model) => ({
    value: model.id,
    label: modelDisplayName(model.provider.name, model.input.provider_model_id),
    tags: modelTags(model),
    disabledReason: props.issue(model),
    details:
      !!model.input.notes.trim() ||
      !!(props.allowReasoning && model.input.capabilities.reasoning),
  })),
);
function candidate(value: string) {
  return candidates.value.find((model) => model.id === value);
}
function currentReasoning(value: string): ReasoningChoice {
  return value === props.modelValue && props.reasoning
    ? props.reasoning
    : { mode: "default" };
}
function reasoningOptions(
  value: string,
): { label: string; choice: ReasoningChoice }[] {
  const c = candidate(value)?.input.capabilities;
  if (!c?.reasoning) return [];
  const result: { label: string; choice: ReasoningChoice }[] = [
    { label: "供应商默认", choice: { mode: "default" } },
  ];
  if (c.reasoning_efforts.length)
    return [
      ...result,
      ...c.reasoning_efforts.map((effort) => ({
        label: effort.name,
        choice: { mode: "effort" as const, value: effort.value },
      })),
    ];
  return c.reasoning_toggle_mode
    ? [
        ...result,
        { label: "开启思考", choice: { mode: "on" } },
        { label: "关闭思考", choice: { mode: "off" } },
      ]
    : result;
}
function checked(value: string, choice: ReasoningChoice) {
  const current = currentReasoning(value);
  return (
    current.mode === choice.mode &&
    (current.mode !== "effort" ||
      (choice.mode === "effort" && current.value === choice.value))
  );
}
function chooseReasoning(value: string, choice: ReasoningChoice) {
  const model = candidate(value);
  if (
    props.allowReasoning &&
    model &&
    !props.disabled &&
    !props.issue(model) &&
    !modelReasoningIssue(model, choice)
  )
    emit("select", model, choice);
}
function cancelRead() {
  clearTimeout(timer);
  timer = undefined;
  waiting.value = false;
  list.reset();
}
function search(value: string) {
  query.value = value;
  cancelRead();
  if (queryIssue.value || !opened.value) return;
  waiting.value = true;
  timer = setTimeout(() => {
    waiting.value = false;
    searched.value = query.value.trim().toLowerCase();
    void list.first();
  }, 250);
}
function openChanged(value: boolean) {
  opened.value = value;
  cancelRead();
  query.value = searched.value = "";
  if (value) void list.first();
}
function select(id: string) {
  const model = candidate(id);
  if (model && !props.disabled && !props.issue(model)) emit("select", model);
}
watch(
  () => props.requiredKind,
  () => {
    opened.value = false;
    cancelRead();
    query.value = searched.value = "";
  },
  { flush: "sync" },
);
watch(access.generation, () => {
  opened.value = false;
  cancelRead();
  query.value = searched.value = "";
});
onBeforeUnmount(cancelRead);
</script>
<template>
  <UiSearchSelect
    :key="`${access.generation.value}:${requiredKind}`"
    :label="label"
    :model-value="modelValue"
    :selected-label="selectedLabel"
    :options="options"
    :empty-text="emptyText"
    rich
    :query="query"
    :disabled="disabled || !access.allowed.value || !validKind"
    :loading="list.pending.value || waiting"
    :error="
      queryIssue ||
      (list.error.value ? catalogError(list.error.value) : undefined)
    "
    :page-index="list.index.value"
    :has-previous="list.index.value > 0"
    :has-next="!!list.nextCursor.value"
    @open-change="openChanged"
    @update:query="search"
    @update:model-value="select"
    @previous="list.previous"
    @next="list.next"
    @retry="!queryIssue && list.reload()"
  >
    <template #details="{ option }">
      <p
        v-if="candidate(option.value)?.input.notes.trim()"
        class="model-picker-notes"
      >
        {{ candidate(option.value)?.input.notes }}
      </p>
      <fieldset
        v-if="
          allowReasoning &&
          candidate(option.value)?.input.capabilities.reasoning
        "
        class="model-picker-reasoning"
        :disabled="disabled || !!option.disabledReason"
      >
        <legend class="sr-only">思考策略</legend>
        <label
          v-for="(item, index) in reasoningOptions(option.value)"
          :key="index"
        >
          <input
            type="radio"
            :name="`${id}-${option.value}-reasoning`"
            :checked="checked(option.value, item.choice)"
            @change="chooseReasoning(option.value, item.choice)"
          />{{ item.label }}
        </label>
        <p
          v-if="
            !candidate(option.value)?.input.capabilities.reasoning_efforts
              .length &&
            !candidate(option.value)?.input.capabilities.reasoning_toggle_mode
          "
          class="model-picker-hint"
        >
          此模型尚未配置思考开关格式，按供应商默认策略调用。
        </p>
      </fieldset>
    </template>
  </UiSearchSelect>
</template>

<style scoped>
.model-picker-notes {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 13px;
  color: var(--text-secondary);
}
.model-picker-reasoning {
  margin: 0;
  padding: 0;
  border: 0;
  display: grid;
  gap: 4px;
  min-width: 0;
}
.model-picker-reasoning label {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 36px;
  font-size: 13px;
  overflow-wrap: anywhere;
  cursor: pointer;
}
.model-picker-reasoning input {
  flex: none;
}
.model-picker-hint {
  margin: 4px 0 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.5;
}
@media (pointer: coarse) {
  .model-picker-reasoning label {
    min-height: 44px;
  }
}
</style>
