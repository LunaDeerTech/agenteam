<script setup lang="ts">
defineOptions({ inheritAttrs: false })
defineProps<{ invalid?: boolean }>()
const model = defineModel<string>({ default: '' })
</script>
<template>
  <textarea v-bind="$attrs" v-model="model" class="ui-input" :aria-invalid="invalid || undefined" />
</template>
