<script setup lang="ts">
withDefaults(defineProps<{ lines?: number }>(), { lines: 3 })
</script>
<template>
  <div class="ui-skeleton" aria-hidden="true"><span v-for="i in lines" :key="i" /></div>
</template>
