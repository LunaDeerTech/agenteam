<script setup lang="ts">
import { computed, ref } from 'vue'
import { UiButton, UiInput } from '../ui'
import type { MeetingSession } from '../../api/meetings'
const props = defineProps<{
  items: readonly MeetingSession[]
  selected: string | null
  disabled: boolean
}>()
defineEmits<{ select: [id: string | null] }>()
const filter = ref('')
const items = computed(() =>
  props.items.filter((m) =>
    (m.title || '新会议').toLocaleLowerCase().includes(filter.value.toLocaleLowerCase()),
  ),
)
</script>
<template>
  <div class="session-list">
    <div class="list-header">
      <h2>会议</h2>
      <UiButton :disabled="disabled" @click="$emit('select', null)">新会议</UiButton>
    </div>
    <UiInput v-model="filter" aria-label="筛选会议" placeholder="筛选会议…" />
    <nav aria-label="会议会话">
      <button
        v-for="m in items"
        :key="m.id"
        type="button"
        :disabled="disabled"
        :aria-current="selected === m.id ? 'page' : undefined"
        @click="$emit('select', m.id)"
      >
        <span class="title">{{ m.title || '新会议' }}</span
        ><span class="meta">{{
          m.deleting
            ? '正在删除'
            : m.status === 'archive'
              ? '已归档'
              : m.status === 'proposed'
                ? '待确认提案'
                : new Date(m.updated_at).toLocaleDateString('zh-CN')
        }}</span>
      </button>
    </nav>
    <p v-if="!items.length" class="empty">{{ filter ? '没有匹配的会议' : '还没有会议' }}</p>
  </div>
</template>
<style scoped>
.session-list {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
h2 {
  font-size: 14px;
  margin: 0;
}
nav {
  display: grid;
  gap: 3px;
}
nav button {
  border: 0;
  background: none;
  text-align: left;
  padding: 10px;
  border-radius: var(--radius);
  color: var(--text);
  min-width: 0;
  cursor: pointer;
}
nav button:hover,
nav button[aria-current='page'] {
  background: var(--nav-selected);
}
nav button:disabled {
  cursor: default;
  opacity: 0.65;
}
.title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
.meta {
  display: block;
  font-size: 11px;
  color: var(--muted);
  margin-top: 4px;
}
.empty {
  color: var(--muted);
  font-size: 13px;
}
</style>
