<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { UiButton, UiDrawer, UiState, UiSelect, UiField, UiBadge } from '../../components/ui'
import TaskBoard from '../../components/work/TaskBoard.vue'
import TaskReviewDetail from '../../components/work/TaskReviewDetail.vue'
import TaskCreateForm from '../../components/work/TaskCreateForm.vue'
import { useProjectTasks } from '../../composables/useProjectTasks'
const owner = useProjectTasks(),
  state = owner.state
const sidebar = ref(false)
const createButton = ref<{ $el: HTMLButtonElement } | null>(null)
async function restoreCreateFocus() {
  await nextTick()
  if (owner.visible.value && !state.task) createButton.value?.$el.focus()
}
const milestoneOptions = computed(() => {
  const rows = [...state.milestones]
  if (state.milestone && !rows.some((x) => x.id === state.milestone!.id))
    rows.unshift(state.milestone)
  return rows.map((m) => ({ value: m.id, label: m.title }))
})
const sprintLabels = { planned: '未启动', current: '当前', completed: '已完成' }
function selectSprint(id: string) {
  owner.selectSprint(id)
  sidebar.value = false
}
function returnToCreation() {
  const p = owner.creationPending.value
  if (p?.sprintID) selectSprint(p.sprintID)
}
</script>
<template>
  <section class="tasks-page" aria-label="项目任务">
    <header class="tasks-heading">
      <h1>任务</h1>
      <UiButton class="sidebar-trigger" :disabled="!owner.visible.value" @click="sidebar = true"
        >选择 Sprint</UiButton
      ><UiButton :disabled="owner.busy.value || !owner.visible.value" @click="owner.refresh()"
        >刷新任务看板</UiButton
      >
    </header>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认任务访问身份" />
    <template v-else>
      <p
        v-if="owner.pending.value && !owner.progress.value && !owner.creationProgress.value"
        role="status"
      >
        有一项任务操作结果待确认，请返回原 Sprint 或任务查询结果。
      </p>
      <UiButton
        v-if="
          owner.creationPending.value &&
          !owner.creationProgress.value &&
          owner.creationPending.value.sprintID
        "
        :disabled="owner.busy.value"
        @click="returnToCreation"
        >返回原创建 Sprint</UiButton
      >
      <p v-if="owner.creationProgress.value?.phase === 'submitting'" role="status">
        正在创建任务，请等待原请求结束。
      </p>
      <section
        v-if="owner.creationProgress.value?.phase === 'uncertain'"
        class="create-recovery"
        aria-label="创建结果恢复"
      >
        <p role="status">创建结果待确认。请查询原创建结果，不要重复创建。</p>
        <UiButton
          :disabled="owner.busy.value || !owner.creationProgress.value.contextValid"
          @click="owner.lookupCreation()"
          >查询原创建结果</UiButton
        >
        <p v-if="owner.creationProgress.value.observation === 'in_progress'" role="status">
          原创建仍在处理中，稍后可再次查询。
        </p>
        <p v-if="owner.creationProgress.value.observation === 'not_observed'" role="status">
          尚未观察到原创建结果，可再次查询。
        </p>
        <p v-if="owner.creation.message" role="alert">{{ owner.creation.message }}</p>
      </section>
      <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取任务工作区" />
      <UiState
        v-else-if="state.phase === 'error'"
        kind="error"
        title="任务工作区不可用"
        :description="state.message"
        ><UiButton :disabled="owner.busy.value" @click="owner.refresh()"
          >重新读取</UiButton
        ></UiState
      >
      <div v-else class="tasks-layout">
        <aside class="tasks-sidebar" :class="{ expanded: sidebar }" aria-label="Sprint 选择">
          <UiButton class="sidebar-close" @click="sidebar = false">关闭选择</UiButton>
          <UiField label="Milestone" v-slot="field"
            ><UiSelect
              :id="field.id"
              :model-value="state.milestone?.id ?? ''"
              label="Milestone"
              :options="milestoneOptions"
              :disabled="owner.busy.value"
              @update:model-value="owner.selectMilestone($event)"
          /></UiField>
          <UiButton
            v-if="state.milestoneNext"
            :disabled="owner.busy.value"
            @click="owner.loadMilestones(true)"
            >更多 Milestone</UiButton
          >
          <p v-if="!state.milestones.length">暂无 Milestone。</p>
          <nav aria-label="Sprint 列表">
            <button
              v-for="sprint in state.sprints"
              :key="sprint.id"
              type="button"
              :aria-current="state.sprint?.id === sprint.id ? 'page' : undefined"
              :disabled="owner.busy.value"
              @click="selectSprint(sprint.id)"
            >
              <span>{{ sprint.title }}</span
              ><UiBadge>{{ sprintLabels[sprint.state] }}</UiBadge>
            </button>
          </nav>
          <UiButton
            v-if="state.sprintNext"
            :disabled="owner.busy.value"
            @click="owner.loadSprints(true)"
            >更多 Sprint</UiButton
          >
        </aside>
        <main class="tasks-content">
          <template v-if="state.sprint"
            ><header class="sprint-heading">
              <h2>{{ state.sprint.title }}</h2>
              <UiBadge>{{ sprintLabels[state.sprint.state] }}</UiBadge>
              <UiButton
                v-if="!owner.creation.open"
                ref="createButton"
                :disabled="owner.busy.value || !owner.canCreate.value"
                @click="owner.openCreation()"
                >新建任务</UiButton
              >
            </header>
            <TaskCreateForm
              v-if="owner.creation.open && !owner.pending.value"
              :owner="owner"
              @closed="restoreCreateFocus" />
            <TaskBoard
              :columns="owner.columns"
              :busy="owner.busy.value"
              :name="owner.name"
              @select="owner.selectTask"
              @more="owner.loadColumn($event, true)"
              @reload="owner.loadColumn($event)"
          /></template>
          <UiState
            v-else
            kind="empty"
            title="请选择 Sprint"
            description="当前没有选中的 Sprint。请从左侧选择，不会自动启动所选 Sprint。"
          />
        </main>
      </div>
      <UiDrawer
        :open="!!state.task"
        title="任务详情"
        :close-on-outside="false"
        :close-on-escape="!owner.busy.value"
        @update:open="!$event && owner.closeTask()"
        ><TaskReviewDetail :owner="owner" /><template #footer
          ><UiButton :disabled="owner.busy.value" @click="owner.closeTask()"
            >关闭任务详情</UiButton
          ></template
        ></UiDrawer
      >
    </template>
  </section>
</template>
<style scoped>
.tasks-page {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  padding: var(--space);
  min-width: 0;
}
.tasks-heading,
.sprint-heading {
  display: flex;
  align-items: center;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
.tasks-heading h1 {
  margin: 0;
  flex: 1;
}
.tasks-layout {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: var(--space);
  min-width: 0;
}
.tasks-sidebar {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
}
.tasks-sidebar nav {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.tasks-sidebar nav button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  color: var(--text);
  font: inherit;
  text-align: left;
  overflow-wrap: anywhere;
}
.tasks-sidebar nav button[aria-current='page'] {
  background: var(--nav-selected);
}
.tasks-sidebar nav button span {
  min-width: 0;
}
.tasks-content {
  min-width: 0;
}
.sprint-heading {
  margin-bottom: var(--space);
}
.sprint-heading h2 {
  margin: 0;
  font-size: 1.25rem;
  overflow-wrap: anywhere;
}
.sidebar-trigger,
.sidebar-close {
  display: none;
}
@media (max-width: 760px) {
  .tasks-layout {
    display: block;
  }
  .tasks-sidebar {
    display: none;
  }
  .tasks-sidebar.expanded {
    display: flex;
    position: fixed;
    inset: var(--nav-height) 16px 16px;
    z-index: 40;
    padding: var(--space);
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: var(--shadow);
    overflow: auto;
  }
  .sidebar-trigger,
  .sidebar-close {
    display: inline-flex;
  }
}
</style>
