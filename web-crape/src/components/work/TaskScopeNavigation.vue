<script setup lang="ts">
import { computed } from 'vue'
import { UiBadge, UiButton, UiField, UiSelect } from '../ui'
import type { ProjectTasks } from '../../composables/useProjectTasks'

const props = defineProps<{ owner: ProjectTasks }>()
defineEmits<{ select: [id: string] }>()
const milestoneOptions = computed(() => {
  const rows = [...props.owner.state.milestones]
  const selected = props.owner.state.milestone
  if (selected && !rows.some((row) => row.id === selected.id)) rows.unshift(selected)
  return rows.map((row) => ({ value: row.id, label: row.title }))
})
const sprintLabels = { planned: '未启动', current: '当前', completed: '已完成' }
</script>

<template>
  <div class="task-scope-navigation">
    <UiField v-slot="field" label="Milestone">
      <UiSelect
        :id="field.id"
        :model-value="owner.state.milestone?.id ?? ''"
        label="Milestone"
        :options="milestoneOptions"
        :disabled="owner.busy.value"
        @update:model-value="owner.selectMilestone($event)"
      />
    </UiField>
    <UiButton
      v-if="owner.state.milestoneNext"
      :disabled="owner.busy.value"
      @click="owner.loadMilestones(true)"
      >更多 Milestone</UiButton
    >
    <p v-if="!owner.state.milestones.length" class="scope-empty">暂无 Milestone。</p>
    <div class="scope-heading">
      <h2>Sprint</h2>
      <span>选择查看任务</span>
    </div>
    <nav aria-label="Sprint 列表">
      <button
        v-for="sprint in owner.state.sprints"
        :key="sprint.id"
        type="button"
        :aria-current="owner.state.sprint?.id === sprint.id ? 'page' : undefined"
        :disabled="owner.busy.value"
        @click="$emit('select', sprint.id)"
      >
        <span class="sprint-name">{{ sprint.title }}</span>
        <UiBadge>{{ sprintLabels[sprint.state] }}</UiBadge>
      </button>
    </nav>
    <p v-if="owner.state.milestone && !owner.state.sprints.length" class="scope-empty">
      此 Milestone 暂无 Sprint。
    </p>
    <UiButton
      v-if="owner.state.sprintNext"
      :disabled="owner.busy.value"
      @click="owner.loadSprints(true)"
      >更多 Sprint</UiButton
    >
  </div>
</template>

<style scoped>
.task-scope-navigation {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--content-gap);
  min-width: 0;
}
.task-scope-navigation > .ui-button {
  align-self: flex-start;
}
.scope-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  margin-top: 12px;
}
.scope-heading h2 {
  margin: 0;
  font-size: 0.875rem;
}
.scope-heading span,
.scope-empty {
  margin: 0;
  color: var(--muted);
  font-size: 0.8125rem;
}
nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
nav button {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  min-height: var(--row-height);
  padding: 10px 12px;
  border: 1px solid transparent;
  border-radius: var(--radius);
  background: transparent;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
nav button:hover:not(:disabled) {
  background: var(--surface-alt);
}
nav button[aria-current='page'] {
  background: var(--nav-selected);
  border-color: var(--border);
}
nav button:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: -2px;
}
nav button:disabled {
  cursor: default;
}
.sprint-name {
  min-width: 0;
  overflow-wrap: anywhere;
}
@media (max-width: 900px) {
  nav button {
    min-height: 44px;
  }
}
</style>
