<script setup lang="ts">
import UiIcon from './UiIcon.vue'
defineEmits<{ select: [] }>()
</script>
<template>
  <button type="button" class="ui-reference" @click="$emit('select')">
    <UiIcon name="file" /><slot />
  </button>
</template>
