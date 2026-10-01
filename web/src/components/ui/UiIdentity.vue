<script setup lang="ts">
withDefaults(defineProps<{ color?: 1 | 2 | 3 | string }>(), { color: 1 })
</script>
<template>
  <span class="ui-identity"
    ><span
      class="agent-dot"
      :style="{ backgroundColor: typeof color === 'string' ? color : `var(--agent-${color})` }"
      aria-hidden="true" /><slot
  /></span>
</template>
