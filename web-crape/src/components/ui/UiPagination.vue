<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
const model = defineModel<number>({ default: 1 })
const props = defineProps<{ total: number; label?: string }>()
const pages = computed(() => {
  const n = Math.max(1, Math.floor(props.total))
  const result = new Set([1, n])
  for (let i = Math.max(1, model.value - 2); i <= Math.min(n, model.value + 2); i++) result.add(i)
  return [...result].sort((a, b) => a - b)
})
watchEffect(() => {
  model.value = Math.max(1, Math.min(model.value, Math.max(1, props.total)))
})
</script>
<template>
  <nav class="ui-row pagination" :aria-label="label || '分页'">
    <UiButton icon :disabled="model <= 1" aria-label="上一页" @click="model--"
      ><UiIcon name="arrow-left" /></UiButton
    ><template v-for="(page, index) in pages" :key="page"
      ><span v-if="index && page - pages[index - 1]! > 1" aria-hidden="true">…</span
      ><UiButton
        :aria-current="model === page ? 'page' : undefined"
        :aria-label="'第 ' + page + ' 页'"
        @click="model = page"
        >{{ page }}</UiButton
      ></template
    ><UiButton icon :disabled="model >= total" aria-label="下一页" @click="model++"
      ><UiIcon name="arrow-right"
    /></UiButton>
  </nav>
</template>
