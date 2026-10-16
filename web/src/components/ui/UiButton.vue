<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: "default" | "primary" | "ghost" | "danger";
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    loading?: boolean;
  }>(),
  { variant: "default", type: "button" },
);
</script>

<template>
  <button
    :type="type"
    class="ui-button"
    :class="`ui-button--${variant}`"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
  >
    <span v-if="loading" class="ui-spinner" aria-hidden="true" />
    <slot />
  </button>
</template>
