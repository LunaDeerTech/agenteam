<script setup lang="ts">
import { computed } from 'vue'
import { UiButton, UiField, UiInput, UiState } from '../ui'
import type { KnowledgeTitleSearch } from '../../composables/useKnowledgeTitleSearch'

const props = defineProps<{ search: KnowledgeTitleSearch; selected: string | null }>()
const emit = defineEmits<{ select: [id: string] }>()
const draft = computed({
  get: () => props.search.state.draft,
  set: (value) => props.search.setDraft(value),
})
function select(id: string) {
  if (
    !props.search.disabled.value &&
    props.search.state.items.some((hit) => hit.document.id === id)
  )
    emit('select', id)
}
</script>

<template>
  <section class="title-search" aria-label="查找文档标题">
    <form role="search" aria-label="查找文档标题" @submit.prevent="search.search()">
      <UiField label="标题包含" :error="search.state.field" hint="查找整个项目的文档标题。">
        <template #default="field">
          <UiInput
            v-model="draft"
            :id="field.id"
            :invalid="field.invalid"
            :aria-describedby="field.describedby"
            :disabled="!search.visible.value"
            type="search"
            autocomplete="off"
          />
        </template>
      </UiField>
      <div class="search-actions">
        <UiButton type="submit" :disabled="search.disabled.value">查找标题</UiButton>
        <UiButton v-if="search.state.draft || search.state.query" @click="search.clear()"
          >清空查找</UiButton
        >
        <UiButton v-if="search.busy.value" @click="search.cancel()">停止查找</UiButton>
      </div>
    </form>
    <p v-if="search.state.message" role="status">{{ search.state.message }}</p>
    <UiState v-if="search.state.phase === 'loading'" kind="loading" title="正在查找文档标题" />
    <UiState v-else-if="search.state.phase === 'empty'" kind="empty" title="没有匹配的文档标题" />
    <template v-if="search.state.items.length">
      <p class="search-summary" role="status">已读取 {{ search.state.items.length }} 个匹配文档</p>
      <ul aria-label="标题查找结果">
        <li v-for="hit in search.state.items" :key="hit.document.id">
          <UiButton
            :disabled="search.disabled.value"
            :aria-current="selected === hit.document.id ? 'true' : undefined"
            @click="select(hit.document.id)"
            >{{ hit.document.title }}</UiButton
          >
          <p class="search-path">
            {{
              hit.ancestors.length
                ? hit.ancestors.map((item) => item.title).join(' / ')
                : '根层文档'
            }}
          </p>
        </li>
      </ul>
      <p v-if="search.state.phase === 'error'" class="search-summary">以上为此前完整读取的结果。</p>
      <UiButton
        v-if="search.state.nextCursor"
        :disabled="!search.canMore.value"
        @click="search.more()"
        >继续加载查找结果</UiButton
      >
    </template>
  </section>
</template>

<style scoped>
.title-search {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
.search-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
ul {
  display: grid;
  gap: var(--space-3);
  list-style: none;
  margin: 0;
  padding: 0;
}
li {
  min-width: 0;
}
li :deep(button) {
  max-width: 100%;
  text-align: start;
  white-space: normal;
  overflow-wrap: anywhere;
}
.search-path,
.search-summary {
  color: var(--muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.search-path {
  margin: var(--space-1) 0 0;
}
</style>
