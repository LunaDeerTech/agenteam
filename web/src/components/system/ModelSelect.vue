<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { UiSearchSelect } from "../ui";
import {
  catalogError,
  listModels,
  modelDisplayName,
  modelTypeChoices,
  type CatalogModelOption,
} from "../../system/model-catalog";
import { useCursorList } from "../../system/useCursorList";
import { useSystemAccess } from "../../system/requests";
const props = defineProps<{
  label: string;
  modelValue?: string;
  selectedLabel?: string;
  disabled?: boolean;
  issue: (model: CatalogModelOption) => string | undefined;
}>();
const emit = defineEmits<{ select: [model: CatalogModelOption] }>();
const access = useSystemAccess();
const opened = ref(false);
const query = ref("");
const searched = ref("");
const waiting = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
const list = useCursorList(
  (cursor, current, limit) =>
    listModels(undefined, cursor, current, limit, searched.value),
  {
    enabled: () => opened.value && !props.disabled,
    immediate: false,
  },
);
const queryIssue = computed(() =>
  new TextEncoder().encode(query.value.trim()).length > 128 ||
  /[\u0000-\u001f\u007f-\u009f]/u.test(query.value)
    ? "搜索词需在 128 字节内，且不能包含控制字符。"
    : undefined,
);
const options = computed(() =>
  list.items.value.map((model) => ({
    value: model.id,
    label: modelDisplayName(model.provider.name, model.input.provider_model_id),
    description: modelTypeChoices.find(
      (choice) => choice.value === model.input.type,
    )?.label,
    disabledReason: props.issue(model),
  })),
);
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
  const model = list.items.value.find((item) => item.id === id);
  if (model && !props.issue(model)) emit("select", model);
}
watch(access.generation, () => {
  opened.value = false;
  cancelRead();
  query.value = searched.value = "";
});
onBeforeUnmount(cancelRead);
</script>
<template>
  <UiSearchSelect
    :key="access.generation.value"
    :label="label"
    :model-value="modelValue"
    :selected-label="selectedLabel"
    :options="options"
    :query="query"
    :disabled="disabled || !access.allowed.value"
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
  />
</template>
