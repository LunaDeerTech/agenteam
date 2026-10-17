<script setup lang="ts">
const props = defineProps<{
  modelValue: boolean;
  label: string;
  disabled?: boolean;
  loading?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();
function toggle() {
  if (!props.disabled && !props.loading)
    emit("update:modelValue", !props.modelValue);
}
</script>

<template>
  <button
    type="button"
    role="switch"
    class="ui-switch"
    :class="{ 'is-checked': modelValue }"
    :aria-label="label"
    :aria-checked="modelValue"
    :aria-busy="loading || undefined"
    :aria-disabled="loading || undefined"
    :disabled="disabled"
    @click="toggle"
  >
    <span class="ui-switch-thumb" />
  </button>
</template>

<style scoped>
.ui-switch {
  flex: none;
  width: 34px;
  height: 20px;
  padding: 2px;
  border: 1px solid var(--border-strong);
  border-radius: 12px;
  background: var(--surface-hover);
  cursor: pointer;
}
.ui-switch.is-checked {
  background: var(--accent);
  border-color: var(--accent);
}
.ui-switch-thumb {
  display: block;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--text-secondary);
  transition: transform 120ms ease;
}
.is-checked .ui-switch-thumb {
  transform: translateX(14px);
  background: var(--accent-text);
}
.ui-switch:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 3px;
}
.ui-switch:disabled,
.ui-switch[aria-disabled="true"] {
  opacity: 0.5;
  cursor: not-allowed;
}
@media (prefers-reduced-motion: reduce) {
  .ui-switch-thumb {
    transition: none;
  }
}
</style>
