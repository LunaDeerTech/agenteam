<script setup lang="ts">
import { UiSelect } from "../ui";
import { isPageSize, pageSizeChoices } from "../../system/pagination";

defineProps<{ modelValue: number; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: number] }>();
function update(value: string) {
  const size = Number(value);
  if (isPageSize(size)) emit("update:modelValue", size);
}
</script>

<template>
  <div class="page-size-select">
    <UiSelect
      :model-value="String(modelValue)"
      label="每页条数"
      :options="pageSizeChoices"
      :disabled="disabled"
      @update:model-value="update"
    />
  </div>
</template>

<style scoped>
.page-size-select {
  position: relative;
  flex: none;
  width: 88px;
}
.page-size-select :deep(.ui-field) {
  gap: 0;
}
.page-size-select :deep(.ui-field-label) {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
