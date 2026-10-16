<script setup lang="ts">
import UiIcon from "./UiIcon.vue";
withDefaults(
  defineProps<{
    label: string;
    icon: string;
    disabled?: boolean;
    active?: boolean;
  }>(),
  { disabled: false },
);
</script>

<template>
  <button
    type="button"
    class="ui-button ui-button--ghost ui-icon-button"
    :class="{ 'is-active': active }"
    :disabled="disabled"
    :aria-label="label"
    :title="label"
  >
    <UiIcon :name="icon" />
  </button>
</template>
