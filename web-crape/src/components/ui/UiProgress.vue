<script setup lang="ts">
withDefaults(defineProps<{ value: number; max?: number; label: string }>(), { max: 100 })
</script>
<template>
  <div class="ui-progress">
    <span>{{ label }}</span
    ><progress :value="Math.min(Math.max(value, 0), max)" :max="max" :aria-label="label" /><span
      class="meta"
      >{{ Math.round((Math.min(Math.max(value, 0), max) / max) * 100) }}%</span
    >
  </div>
</template>
