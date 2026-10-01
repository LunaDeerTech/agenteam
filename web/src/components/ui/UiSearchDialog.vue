<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import UiDialog from './UiDialog.vue'
import UiInput from './UiInput.vue'
import { optionKeys } from '../../composables/useOptionKeys'
const open = defineModel<boolean>('open', { default: false })
const props = defineProps<{ title: string; items: { id: string; label: string }[] }>()
const emit = defineEmits<{ select: [id: string] }>()
const query = ref('')
const id = useId()
const results = computed(() =>
  props.items.filter((i) =>
    i.label.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase()),
  ),
)
function select(value: string) {
  open.value = false
  emit('select', value)
}
function keys(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    ;(e.currentTarget as HTMLElement).parentElement
      ?.querySelector<HTMLButtonElement>('.search-results button')
      ?.focus()
  }
  if (e.key === 'Enter' && results.value[0]) select(results.value[0].id)
}
</script>
<template>
  <UiDialog v-model:open="open" :title="title"
    ><label :for="id">搜索内容</label
    ><UiInput :id="id" v-model="query" data-autofocus placeholder="输入关键词…" @keydown="keys" />
    <div class="search-results" aria-label="搜索结果" @keydown="optionKeys">
      <button
        v-for="item in results"
        :key="item.id"
        type="button"
        class="menu-item"
        @click="select(item.id)"
      >
        {{ item.label }}
      </button>
      <p v-if="!results.length" role="status">没有匹配的内容</p>
    </div></UiDialog
  >
</template>
