<script setup lang="ts">
import { UiBadge, UiButton, UiState } from '../ui'
import { taskStates, taskLabels, type TaskState } from '../../api/work-review'
import type { TaskColumn } from '../../composables/useProjectTasks'
defineProps<{
  columns: Record<TaskState, TaskColumn>
  busy: boolean
  name: (id: string | null) => string
}>()
defineEmits<{ select: [id: string]; more: [state: TaskState]; reload: [state: TaskState] }>()
const priorities = { critical: '紧急', high: '高', medium: '中', low: '低' }
</script>
<template>
  <div class="task-board" aria-label="任务看板" tabindex="0">
    <section
      v-for="status in taskStates"
      :key="status"
      class="task-column"
      :aria-label="taskLabels[status]"
    >
      <header>
        <h3>{{ taskLabels[status] }}</h3>
        <span aria-label="已读取任务数">{{ columns[status].items.length }}</span>
      </header>
      <UiState v-if="columns[status].phase === 'loading'" kind="loading" title="正在读取任务" />
      <UiState
        v-else-if="columns[status].phase === 'error'"
        kind="error"
        title="任务读取失败"
        :description="columns[status].message"
        ><UiButton :disabled="busy" @click="$emit('reload', status)">重读此列</UiButton></UiState
      >
      <p
        v-else-if="columns[status].phase === 'ready' && !columns[status].items.length"
        class="empty-column"
      >
        暂无任务
      </p>
      <button
        v-for="task in columns[status].items"
        :key="task.id"
        type="button"
        class="task-card"
        :aria-label="task.title"
        :disabled="busy"
        @click="$emit('select', task.id)"
      >
        <strong>{{ task.title }}</strong>
        <span class="card-meta"
          ><UiBadge>{{ task.type }}</UiBadge
          ><span>{{ priorities[task.priority] }}优先级</span></span
        >
        <span>{{ name(task.assignee_agent_id) }}</span>
        <span v-if="task.state === 'blocked'">任务受阻，请打开详情查看阻塞</span>
      </button>
      <UiButton v-if="columns[status].next" :disabled="busy" @click="$emit('more', status)"
        >更多{{ taskLabels[status] }}任务</UiButton
      >
    </section>
  </div>
</template>
<style scoped>
.task-board {
  display: flex;
  align-items: flex-start;
  gap: var(--content-gap);
  overflow-x: auto;
  min-width: 0;
  padding-bottom: var(--space);
}
.task-column {
  flex: 0 0 245px;
  min-width: 0;
  min-height: 240px;
  background: var(--surface-alt);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.task-column header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.task-column h3 {
  font-size: 1rem;
  margin: 0;
}
.task-column header span,
.empty-column {
  color: var(--muted);
  font-size: 0.875rem;
}
.task-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  text-align: left;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  padding: 14px;
  color: var(--text);
  overflow-wrap: anywhere;
  cursor: pointer;
  font: inherit;
}
.task-card:hover:not(:disabled) {
  border-color: var(--text);
}
.task-card:disabled {
  cursor: default;
}
.task-card > span {
  color: var(--muted);
  font-size: 0.875rem;
}
.card-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
