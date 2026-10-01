<script setup lang="ts">
defineOptions({ inheritAttrs: false })
const model = defineModel<boolean>({ default: false })
defineProps<{ disabled?: boolean; name?: string }>()
</script>
<template>
  <label class="ui-choice" :class="{ disabled }"
    ><input
      v-bind="$attrs"
      v-model="model"
      role="switch"
      type="checkbox"
      :disabled="disabled"
      :name="name" /><span><slot :checked="model" /></span
  ></label>
</template>
