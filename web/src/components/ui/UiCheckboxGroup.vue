<script setup lang="ts">
import UiIcon from "./UiIcon.vue";

export interface CheckboxOption {
  value: string;
  label: string;
  icon?: string;
}
defineProps<{
  label: string;
  options: readonly CheckboxOption[];
  disabled?: boolean;
}>();
const model = defineModel<string[]>({ default: () => [] });
</script>

<template>
  <fieldset class="ui-checkbox-group" :disabled="disabled">
    <legend>{{ label }}</legend>
    <div class="ui-checkbox-options">
      <label
        v-for="option in options"
        :key="option.value"
        class="ui-checkbox-option"
        :class="{ 'is-selected': model.includes(option.value) }"
      >
        <input v-model="model" type="checkbox" :value="option.value" />
        <UiIcon v-if="option.icon" :name="option.icon" :size="14" />
        <span>{{ option.label }}</span>
      </label>
    </div>
  </fieldset>
</template>

<style scoped>
.ui-checkbox-group {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
  align-self: start;
}
.ui-checkbox-group legend {
  padding: 0;
  margin-bottom: 8px;
  font-size: 13px;
  font-weight: 500;
}
.ui-checkbox-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.ui-checkbox-option {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  min-height: var(--control-height);
  box-sizing: border-box;
  padding: 7px 10px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--surface-content);
  font-size: 13px;
  cursor: pointer;
}
.ui-checkbox-option.is-selected {
  background: var(--surface-active);
}
.ui-checkbox-option input {
  flex: 0 0 auto;
  margin: 0;
  accent-color: var(--accent);
}
.ui-checkbox-option .ui-icon {
  flex: 0 0 auto;
}
.ui-checkbox-option:has(input:disabled) {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
