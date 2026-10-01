<script setup lang="ts">
defineOptions({ inheritAttrs: false })
const model = defineModel<boolean>({ default: false })
defineProps<{ disabled?: boolean; name?: string; value?: string }>()
</script>
<template>
  <label class="ui-choice" :class="{ disabled }"
    ><input
      v-bind="$attrs"
      v-model="model"
      type="checkbox"
      :disabled="disabled"
      :name="name"
      :value="value" /><span><slot /></span
  ></label>
</template>
