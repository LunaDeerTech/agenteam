<script setup lang="ts">
import { UiButton, UiState } from '../ui'
import { taskStates, taskLabels, type TaskState } from '../../api/work-review'
import type { TaskColumn } from '../../composables/useProjectTasks'
defineProps<{
  columns: Record<TaskState, TaskColumn>
  stateFilter: TaskState | ''
  busy: boolean
  name: (id: string | null) => string
}>()
defineEmits<{ select: [id: string]; more: [state: TaskState]; reload: [state: TaskState] }>()
const types = { feature: '功能', bug: '缺陷', task: '任务', spike: '调研', chore: '维护' }
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
        <span v-if="!stateFilter || stateFilter === status" aria-label="已读取任务数">{{
          columns[status].items.length
        }}</span>
      </header>
      <div class="task-column-body">
        <p v-if="stateFilter && stateFilter !== status" class="empty-column">
          当前筛选未包含此状态
        </p>
        <template v-else>
          <UiState v-if="columns[status].phase === 'loading'" kind="loading" title="正在读取任务" />
          <UiState
            v-else-if="columns[status].phase === 'error'"
            kind="error"
            title="任务读取失败"
            :description="columns[status].message"
            ><UiButton :disabled="busy" @click="$emit('reload', status)"
              >重读此列</UiButton
            ></UiState
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
              ><span>{{ types[task.type] }}</span
              ><span>{{ priorities[task.priority] }}优先级</span></span
            >
            <span>{{ name(task.assignee_agent_id) }}</span>
            <span v-if="task.state === 'blocked'">任务受阻，请打开详情查看阻塞</span>
          </button>
          <UiButton v-if="columns[status].next" :disabled="busy" @click="$emit('more', status)"
            >更多{{ taskLabels[status] }}任务</UiButton
          >
        </template>
      </div>
    </section>
  </div>
</template>
<style scoped>
.task-board {
  display: flex;
  align-items: stretch;
  gap: var(--content-gap);
  overflow: auto;
  overscroll-behavior-x: contain;
  min-width: 0;
  min-height: 300px;
  padding: 2px 2px 12px;
}
.task-column {
  flex: 0 0 248px;
  min-width: 0;
  min-height: 240px;
  background: var(--surface-alt);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.task-column-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
  overflow: auto;
  padding: 2px 4px 4px;
}
.task-column header {
  padding: 0 4px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.task-column h3 {
  font-size: 0.875rem;
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
  gap: 8px;
  text-align: left;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  padding: 12px;
  flex: none;
  color: var(--text);
  overflow-wrap: anywhere;
  cursor: pointer;
  font: inherit;
}
.task-card:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 1px;
}
.task-card strong {
  font-size: 0.875rem;
  font-weight: 600;
  line-height: 1.5;
}
.task-card:hover:not(:disabled) {
  border-color: var(--muted);
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
