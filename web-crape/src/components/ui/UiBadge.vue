<script setup lang="ts">
withDefaults(
  defineProps<{
    tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'accent' | 'progress' | 'review'
  }>(),
  { tone: 'neutral' },
)
</script>
<template>
  <span class="ui-badge" :class="tone"><slot /></span>
</template>
