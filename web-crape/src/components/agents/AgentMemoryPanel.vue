<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { UiButton, UiDialog, UiState } from '../ui'
import { useAgentMemory } from '../../composables/useAgentMemory'
import type { MemoryRevision } from '../../api/agent-memory'
const props = defineProps<{ agentId: string | null }>()
const emit = defineEmits<{ close: [] }>()
const owner = useAgentMemory(() => props.agentId),
  state = owner.state
const heading = ref<HTMLElement | null>(null)
const labels = { fact: '事实', experience: '经验', procedure: '流程', observation: '观察' }
const statuses = { active: '当前', superseded: '已替代', deleted: '已删除' }
function focus() {
  heading.value?.focus()
}
defineExpose({ focus })
async function remove(lookup = false) {
  if (await owner.remove(lookup)) {
    await nextTick()
    focus()
  }
}
function confirm(memory: MemoryRevision) {
  if (owner.canDelete.value) state.confirmation = memory
}
onMounted(() => {
  void owner.load()
})
</script>
<template>
  <section class="agent-memory" aria-label="Agent 记忆">
    <header class="memory-heading">
      <h3 ref="heading" tabindex="-1">记忆</h3>
      <UiButton :disabled="owner.busy.value" @click="emit('close')">关闭记忆</UiButton>
    </header>
    <p>此 Agent 显式保留的经验与流程可在后续执行中按需检索。</p>
    <div class="memory-actions">
      <UiButton :disabled="owner.busy.value || !owner.scope.value" @click="owner.load()"
        >刷新记忆</UiButton
      ><span v-if="state.feedback" role="status">{{ state.feedback }}</span>
    </div>
    <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取记忆" />
    <p v-if="state.message" role="alert">{{ state.message }}</p>
    <section v-if="owner.pending.value?.phase === 'uncertain'" aria-label="删除结果恢复">
      <p>删除结果待确认。可查询原操作结果。</p>
      <UiButton
        :disabled="owner.busy.value || !owner.pending.value.contextValid"
        @click="remove(true)"
        >查询原删除结果</UiButton
      >
    </section>
    <p v-if="state.phase === 'ready' && !state.records.length">此 Agent 暂无当前记忆。</p>
    <ul class="memory-list">
      <li v-for="record in state.records" :key="record.current.memory_id">
        <div class="memory-meta">
          <strong>{{ labels[record.current.type] }}</strong
          ><span>修订 {{ record.current.revision }}</span
          ><time :datetime="record.updated_at">{{
            new Date(record.updated_at).toLocaleString()
          }}</time>
        </div>
        <p class="memory-text">{{ record.current.text }}</p>
        <p v-if="record.current.tags.length" class="memory-tags">
          {{ record.current.tags.join(' · ') }}
        </p>
        <div class="memory-actions">
          <UiButton :disabled="owner.busy.value" @click="owner.history(record.current)"
            >查看历史</UiButton
          ><UiButton :disabled="!owner.canDelete.value" @click="confirm(record.current)"
            >删除记忆</UiButton
          >
        </div>
      </li>
    </ul>
    <UiButton v-if="state.next" :disabled="owner.busy.value" @click="owner.load(true)"
      >加载更多记忆</UiButton
    >
    <section v-if="state.selected" class="memory-history" aria-label="记忆历史">
      <h4>修订历史</h4>
      <UiState v-if="state.historyLoading" kind="loading" title="正在读取历史" />
      <p v-if="state.historyMessage" role="alert">{{ state.historyMessage }}</p>
      <UiButton
        v-if="state.historyMessage"
        :disabled="owner.busy.value"
        @click="owner.history(state.selected)"
        >重新读取历史</UiButton
      >
      <ol class="memory-list">
        <li v-for="revision in state.revisions" :key="revision.revision">
          <strong>修订 {{ revision.revision }} · {{ statuses[revision.status] }}</strong>
          <p class="memory-text">{{ revision.text }}</p>
          <details>
            <summary>来源与时间</summary>
            <dl>
              <dt>来源执行</dt>
              <dd>{{ revision.provenance.execution_id }}</dd>
              <dt>保留时间</dt>
              <dd>{{ new Date(revision.created_at).toLocaleString() }}</dd>
            </dl>
          </details>
        </li>
      </ol>
      <UiButton
        v-if="state.historyNext"
        :disabled="owner.busy.value"
        @click="owner.history(state.selected, true)"
        >更多修订</UiButton
      >
    </section>
    <UiDialog
      :open="!!state.confirmation"
      title="删除这条记忆？"
      :fallback-focus="heading"
      :close-on-outside="false"
      :close-on-escape="!owner.busy.value"
      @update:open="!$event && !owner.busy.value && (state.confirmation = null)"
    >
      <p class="memory-text">{{ state.confirmation?.text }}</p>
      <p>删除后，Agent 后续检索不再返回这条记忆；修订历史保留供查看。</p>
      <template #footer
        ><UiButton :disabled="owner.busy.value" @click="state.confirmation = null"
          >保留记忆</UiButton
        ><UiButton variant="danger" :disabled="!owner.canDelete.value" @click="remove()"
          >确认删除</UiButton
        ></template
      >
    </UiDialog>
  </section>
</template>
<style scoped>
.agent-memory {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
  padding-block: var(--space);
}
.memory-heading,
.memory-actions,
.memory-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space);
}
.memory-heading {
  justify-content: space-between;
}
.memory-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.memory-list > li {
  padding-block: calc(var(--space) * 1.5);
  border-bottom: 1px solid var(--border);
}
.memory-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.memory-meta,
.memory-tags {
  color: var(--muted);
  font-size: 0.9em;
}
.memory-meta strong {
  color: var(--text);
}
.memory-history {
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
dd {
  margin-inline-start: 0;
  overflow-wrap: anywhere;
}
</style>
