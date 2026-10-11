<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { UiButton, UiDrawer, UiState, UiSelect, UiField, UiBadge } from '../../components/ui'
import TaskBoard from '../../components/work/TaskBoard.vue'
import TaskReviewDetail from '../../components/work/TaskReviewDetail.vue'
import TaskCreateForm from '../../components/work/TaskCreateForm.vue'
import MilestoneCreateForm from '../../components/work/MilestoneCreateForm.vue'
import MilestoneEditForm from '../../components/work/MilestoneEditForm.vue'
import SprintEditForm from '../../components/work/SprintEditForm.vue'
import SprintCreateForm from '../../components/work/SprintCreateForm.vue'
import { useProjectTasks } from '../../composables/useProjectTasks'
const owner = useProjectTasks(),
  state = owner.state
const sidebar = ref(false)
const createButton = ref<{ $el: HTMLButtonElement } | null>(null)
const startButton = ref<{ $el: HTMLButtonElement } | null>(null)
const startConfirmButton = ref<{ $el: HTMLButtonElement } | null>(null)
const sprintHeading = ref<HTMLHeadingElement | null>(null)
const sprintCreateButton = ref<{ $el: HTMLButtonElement } | null>(null)
const milestoneCreateButton = ref<{ $el: HTMLButtonElement } | null>(null)
const milestoneHeading = ref<HTMLHeadingElement | null>(null)
const milestoneEditButton = ref<{ $el: HTMLButtonElement } | null>(null)
async function cancelMilestoneEdit() {
  const project = owner.milestoneEdit.projectID,
    target = owner.milestoneEdit.milestoneID
  owner.cancelMilestoneEdit()
  await nextTick()
  if (
    !owner.milestoneEdit.open &&
    owner.visible.value &&
    owner.projectID.value === project &&
    state.milestone?.id === target &&
    !state.task
  )
    milestoneEditButton.value?.$el.focus()
}
async function saveMilestoneEdit(action: 'save' | 'lookup' | 'read' = 'save') {
  const project = owner.projectID.value,
    target = state.milestone?.id
  if (action === 'lookup') await owner.lookupMilestoneEdit()
  else if (action === 'read') await owner.rereadMilestoneEdit()
  else await owner.submitMilestoneEdit()
  await nextTick()
  if (
    owner.visible.value &&
    owner.projectID.value === project &&
    state.milestone?.id === target &&
    !state.task &&
    !owner.milestoneEdit.open &&
    !owner.milestoneEdit.needsRead &&
    owner.milestoneEditProgress.value?.phase === 'confirmed'
  )
    milestoneHeading.value?.focus()
}
const sprintEditButton = ref<{ $el: HTMLButtonElement } | null>(null)
async function cancelSprintEdit() {
  const project = owner.sprintEdit.projectID,
    target = owner.sprintEdit.sprintID
  owner.cancelSprintEdit()
  await nextTick()
  if (
    !owner.sprintEdit.open &&
    owner.visible.value &&
    owner.projectID.value === project &&
    state.sprint?.id === target &&
    !state.task
  ) {
    const button = sprintEditButton.value?.$el
    if (button && !button.disabled) button.focus()
    else sprintHeading.value?.focus()
  }
}
async function saveSprintEdit(action: 'save' | 'lookup' | 'read' = 'save') {
  const project = owner.projectID.value,
    target = state.sprint?.id
  if (action === 'lookup') await owner.lookupSprintEdit()
  else if (action === 'read') await owner.rereadSprintEdit()
  else await owner.submitSprintEdit()
  await nextTick()
  if (
    owner.visible.value &&
    owner.projectID.value === project &&
    state.sprint?.id === target &&
    !state.task &&
    !owner.sprintEdit.open &&
    !owner.sprintEdit.needsRead &&
    owner.sprintEditProgress.value?.phase === 'confirmed'
  )
    sprintHeading.value?.focus()
}
watch(
  () => owner.milestoneCreationFocus.value,
  async (id) => {
    if (!id) return
    await nextTick()
    if (owner.milestoneCreationFocus.value !== id) return
    const heading = milestoneHeading.value
    if (!heading?.isConnected) return
    heading.focus()
    if (heading.ownerDocument.activeElement === heading) owner.consumeMilestoneCreationFocus(id)
  },
  { immediate: true, flush: 'post' },
)
async function cancelMilestoneCreation() {
  const project = owner.milestoneCreation.projectID
  owner.cancelMilestoneCreation()
  await nextTick()
  if (
    !owner.milestoneCreation.open &&
    owner.visible.value &&
    owner.canCreateMilestone.value &&
    owner.projectID.value === project
  )
    milestoneCreateButton.value?.$el.focus()
}
watch(
  () => owner.sprintCreationFocus.value,
  async (id) => {
    if (!id) return
    await nextTick()
    if (owner.sprintCreationFocus.value !== id) return
    const heading = sprintHeading.value
    if (!heading?.isConnected) return
    heading.focus()
    if (heading.ownerDocument.activeElement === heading) owner.consumeSprintCreationFocus(id)
  },
  { immediate: true, flush: 'post' },
)
async function cancelSprintCreation() {
  const project = owner.sprintCreation.projectID
  owner.cancelSprintCreation()
  await nextTick()
  if (
    !owner.sprintCreation.open &&
    owner.visible.value &&
    !state.task &&
    state.milestones.some((m) => m.project_id === project)
  )
    sprintCreateButton.value?.$el.focus()
}
async function submitSprintCreation(action: 'create' | 'lookup' | 'read' = 'create') {
  if (action === 'lookup') await owner.lookupSprintCreation()
  else if (action === 'read') await owner.rereadConfirmedCreation()
  else await owner.submitSprintCreation()
}
async function openSprintStart() {
  owner.openSprintStart()
  await nextTick()
  if (owner.sprintStart.open && owner.visible.value) startConfirmButton.value?.$el.focus()
}
async function cancelSprintStart() {
  const sprint = state.sprint?.id
  owner.cancelSprintStart()
  await nextTick()
  if (!owner.sprintStart.open && owner.visible.value && state.sprint?.id === sprint)
    startButton.value?.$el.focus()
}
async function submitSprintStart(lookup = false) {
  const sprint = state.sprint?.id
  if (lookup) await owner.lookupSprintStart()
  else await owner.submitSprintStart()
  await nextTick()
  if (
    owner.visible.value &&
    state.sprint?.id === sprint &&
    state.sprint?.state === 'current' &&
    !owner.sprintStart.needsRead &&
    owner.sprintProgress.value?.phase === 'confirmed'
  )
    sprintHeading.value?.focus()
}
async function rereadSprintStart(confirmed = false) {
  const sprint = state.sprint?.id
  if (confirmed) await owner.rereadConfirmedSprint()
  else await owner.rereadSprintStart()
  await nextTick()
  if (owner.visible.value && state.sprint?.id === sprint && !owner.sprintStart.needsRead)
    sprintHeading.value?.focus()
}
function returnToStartingSprint() {
  const p = owner.sprintPendingHere.value
  if (p) selectSprint(p.sprintID)
}
async function cancelCreation() {
  const sprintID = state.sprint?.id
  await owner.cancelCreation()
  await nextTick()
  if (!owner.creation.open && owner.visible.value && !state.task && state.sprint?.id === sprintID)
    createButton.value?.$el.focus()
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
    <section
      v-if="owner.sprintReadRecovery.value"
      class="sprint-start-recovery"
      aria-label="启动后状态读取"
    >
      <p role="status">{{ owner.sprintStart.message }}</p>
      <UiButton :disabled="owner.busy.value" @click="rereadSprintStart()"
        >重新读取启动条件与当前状态</UiButton
      >
    </section>
    <UiState v-if="!owner.visible.value" kind="loading" title="正在确认任务访问身份" />
    <template v-else>
      <section v-if="owner.milestoneEditProgress.value" aria-label="Milestone 保存状态">
        <p v-if="owner.milestoneEditProgress.value.phase === 'submitting'" role="status">
          正在保存 Milestone。
        </p>
        <template v-if="owner.milestoneEditProgress.value.phase === 'uncertain'">
          <p role="status">Milestone 保存结果尚未确认。请查证原保存，勿再次提交。</p>
          <p v-if="owner.milestoneEditProgress.value.observation === 'not_observed'" role="status">
            尚未观察到原保存结果，可再次查询。
          </p>
          <p v-if="owner.milestoneEditProgress.value.observation === 'in_progress'" role="status">
            原保存仍在处理中，可稍后再次查询。
          </p>
          <UiButton
            v-if="
              state.milestone?.id === owner.milestoneEditProgress.value.milestoneID && !state.task
            "
            :disabled="owner.busy.value || !owner.milestoneEditProgress.value.contextValid"
            @click="saveMilestoneEdit('lookup')"
            >查询原 Milestone 保存结果</UiButton
          >
          <UiButton
            v-else
            :disabled="owner.busy.value || !owner.milestoneEditProgress.value.contextValid"
            @click="owner.selectMilestone(owner.milestoneEditProgress.value.milestoneID)"
            >返回待确认 Milestone</UiButton
          >
        </template>
        <template
          v-if="
            owner.milestoneEditProgress.value.phase === 'confirmed' &&
            (owner.milestoneEdit.needsRead ||
              state.milestone?.id !== owner.milestoneEditProgress.value.milestoneID)
          "
        >
          <p role="status">原 Milestone 保存已确认，当前内容需要重新读取。</p>
          <UiButton
            v-if="
              state.milestone?.id === owner.milestoneEditProgress.value.milestoneID && !state.task
            "
            :disabled="owner.busy.value || !owner.milestoneEditProgress.value.contextValid"
            @click="saveMilestoneEdit('read')"
            >读取已确认的 Milestone</UiButton
          >
          <UiButton
            v-else
            :disabled="owner.busy.value || !owner.milestoneEditProgress.value.contextValid"
            @click="owner.selectMilestone(owner.milestoneEditProgress.value.milestoneID)"
            >返回已确认 Milestone</UiButton
          >
        </template>
      </section>
      <section v-if="owner.sprintEditProgress.value" aria-label="Sprint 保存状态">
        <p v-if="owner.sprintEditProgress.value.phase === 'submitting'" role="status">
          正在保存 Sprint。
        </p>
        <template v-if="owner.sprintEditProgress.value.phase === 'uncertain'">
          <p role="status">Sprint 保存结果尚未确认。请查证原保存，勿再次提交。</p>
          <p v-if="owner.sprintEditProgress.value.observation === 'not_observed'" role="status">
            尚未观察到原保存结果，可再次查询。
          </p>
          <p v-if="owner.sprintEditProgress.value.observation === 'in_progress'" role="status">
            原保存仍在处理中，可稍后再次查询。
          </p>
          <UiButton
            v-if="state.sprint?.id === owner.sprintEditProgress.value.sprintID && !state.task"
            :disabled="owner.busy.value || !owner.sprintEditProgress.value.contextValid"
            @click="saveSprintEdit('lookup')"
            >查询原 Sprint 保存结果</UiButton
          >
          <UiButton
            v-else
            :disabled="owner.busy.value || !owner.sprintEditProgress.value.contextValid"
            @click="owner.selectSprint(owner.sprintEditProgress.value.sprintID)"
            >返回待确认 Sprint</UiButton
          >
        </template>
        <template
          v-if="
            owner.sprintEditProgress.value.phase === 'confirmed' &&
            (owner.sprintEdit.needsRead ||
              state.sprint?.id !== owner.sprintEditProgress.value.sprintID)
          "
        >
          <p role="status">原 Sprint 保存已确认，当前内容需要重新读取。</p>
          <UiButton
            v-if="state.sprint?.id === owner.sprintEditProgress.value.sprintID && !state.task"
            :disabled="owner.busy.value || !owner.sprintEditProgress.value.contextValid"
            @click="saveSprintEdit('read')"
            >读取已确认的 Sprint</UiButton
          >
          <UiButton
            v-else
            :disabled="owner.busy.value || !owner.sprintEditProgress.value.contextValid"
            @click="owner.selectSprint(owner.sprintEditProgress.value.sprintID)"
            >返回已确认 Sprint</UiButton
          >
        </template>
      </section>
      <p v-if="owner.sprintEditPending.value && !owner.sprintEditProgress.value" role="status">
        有一项 Sprint 保存结果待确认，请返回原 Project 查询结果。
      </p>
      <p v-if="owner.milestonePending.value && !owner.milestoneProgress.value" role="status">
        有一项 Milestone 创建结果待确认，请返回原 Project 查询结果。
      </p>
      <section
        v-if="owner.milestoneProgress.value?.phase === 'uncertain'"
        aria-label="Milestone 创建结果恢复"
      >
        <p role="status">Milestone 创建结果待确认。请查询原创建结果，不要重复创建。</p>
        <UiButton
          :disabled="
            owner.busy.value || !owner.milestoneProgress.value.contextValid || !!state.task
          "
          @click="owner.lookupMilestoneCreation()"
          >查询原 Milestone 创建结果</UiButton
        >
        <p v-if="owner.milestoneProgress.value.observation === 'not_observed'" role="status">
          尚未观察到原创建结果，可再次查询。
        </p>
        <p v-if="owner.milestoneProgress.value.observation === 'in_progress'" role="status">
          原创建仍在处理中，稍后可再次查询。
        </p>
      </section>
      <p v-if="owner.milestoneProgress.value?.phase === 'submitting'" role="status">
        正在创建 Milestone，请等待原请求结束。
      </p>
      <p v-if="owner.milestoneCreation.message && !owner.milestoneCreation.open" role="status">
        {{ owner.milestoneCreation.message }}
      </p>
      <UiButton
        v-if="
          owner.milestoneProgress.value?.phase === 'confirmed' &&
          (owner.milestoneCreation.needsRead ||
            state.milestone?.id !== owner.milestoneProgress.value.milestoneID)
        "
        :disabled="owner.busy.value || !owner.milestoneProgress.value.contextValid || !!state.task"
        @click="owner.rereadConfirmedMilestone()"
        >读取已创建的 Milestone</UiButton
      >
      <p v-if="owner.sprintCreatePending.value && !owner.sprintCreateProgress.value" role="status">
        有一项 Sprint 创建结果待确认，请返回原 Project 查询结果。
      </p>
      <section
        v-if="owner.sprintCreateProgress.value?.phase === 'uncertain'"
        class="sprint-create-recovery"
        aria-label="Sprint 创建结果恢复"
      >
        <p role="status">Sprint 创建结果待确认。请查询原创建结果，不要重复创建。</p>
        <UiButton
          :disabled="
            owner.busy.value || !owner.sprintCreateProgress.value.contextValid || !!state.task
          "
          @click="submitSprintCreation('lookup')"
          >查询原 Sprint 创建结果</UiButton
        >
        <p v-if="owner.sprintCreateProgress.value.observation === 'not_observed'" role="status">
          尚未观察到原创建结果，可再次查询。
        </p>
        <p v-if="owner.sprintCreateProgress.value.observation === 'in_progress'" role="status">
          原创建仍在处理中，稍后可再次查询。
        </p>
      </section>
      <p v-if="owner.sprintCreateProgress.value?.phase === 'submitting'" role="status">
        正在创建 Sprint，请等待原请求结束。
      </p>
      <p v-if="owner.sprintCreation.message && !owner.sprintCreation.open" role="status">
        {{ owner.sprintCreation.message }}
      </p>
      <UiButton
        v-if="
          owner.sprintCreateProgress.value?.phase === 'confirmed' &&
          (owner.sprintCreation.needsRead ||
            state.sprint?.id !== owner.sprintCreateProgress.value.sprintID)
        "
        :disabled="
          owner.busy.value || !owner.sprintCreateProgress.value.contextValid || !!state.task
        "
        @click="submitSprintCreation('read')"
        >读取已创建的 Sprint</UiButton
      >
      <p v-if="owner.sprintPending.value && !owner.sprintProgress.value" role="status">
        有一项 Sprint 启动结果待确认，请返回原 Project 和 Sprint 查询结果。
      </p>
      <UiButton
        v-if="owner.sprintPendingHere.value && !owner.sprintProgress.value"
        :disabled="owner.busy.value"
        @click="returnToStartingSprint"
        >返回原启动 Sprint</UiButton
      >
      <p v-if="owner.sprintProgress.value?.phase === 'submitting'" role="status">
        正在启动 Sprint，请等待原请求结束。
      </p>
      <section
        v-if="owner.sprintProgress.value?.phase === 'uncertain'"
        class="sprint-start-recovery"
        aria-label="启动结果恢复"
      >
        <p role="status">启动结果待确认。请查询原启动结果，不要重复启动。</p>
        <UiButton
          :disabled="owner.busy.value || !owner.sprintProgress.value.contextValid"
          @click="submitSprintStart(true)"
          >查询原启动结果</UiButton
        >
        <p v-if="owner.sprintProgress.value.observation === 'in_progress'" role="status">
          原启动仍在处理中，稍后可再次查询。
        </p>
        <p v-if="owner.sprintProgress.value.observation === 'not_observed'" role="status">
          尚未观察到原启动结果，可再次查询。
        </p>
      </section>
      <p
        v-if="
          owner.sprintStart.message && !owner.sprintReadRecovery.value && !owner.sprintPending.value
        "
        role="status"
      >
        {{ owner.sprintStart.message }}
      </p>
      <UiButton
        v-if="owner.sprintProgress.value?.phase === 'confirmed' && !owner.sprintStart.message"
        :disabled="owner.busy.value || !owner.sprintProgress.value.contextValid"
        @click="rereadSprintStart(true)"
        >读取已确认启动的当前状态</UiButton
      >
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
          <UiButton
            v-if="!state.task && !owner.milestoneCreation.open && !owner.milestonePending.value"
            ref="milestoneCreateButton"
            :disabled="owner.busy.value || !owner.canCreateMilestone.value"
            @click="owner.openMilestoneCreation()"
            >新建 Milestone</UiButton
          >
          <MilestoneCreateForm
            v-if="owner.milestoneCreation.open && !owner.milestonePending.value"
            :owner="owner"
            @cancel="cancelMilestoneCreation"
            @submit="owner.submitMilestoneCreation()"
          />
          <section
            v-if="state.milestone && !state.task"
            class="selected-milestone"
            aria-label="所选 Milestone"
          >
            <h2 ref="milestoneHeading" tabindex="-1">{{ state.milestone.title }}</h2>
            <p class="milestone-description">{{ state.milestone.description || '暂无描述。' }}</p>
            <UiButton
              v-if="!owner.milestoneEdit.open && !owner.milestoneEditPending.value"
              ref="milestoneEditButton"
              :disabled="owner.busy.value || !owner.canEditMilestone.value"
              @click="owner.openMilestoneEdit()"
              >编辑 Milestone</UiButton
            >
            <MilestoneEditForm
              v-if="owner.milestoneEdit.open && !owner.milestoneEditPending.value"
              :owner="owner"
              @cancel="cancelMilestoneEdit"
              @submit="saveMilestoneEdit()"
            />
            <p v-if="!owner.milestoneEdit.open && owner.milestoneEdit.message" role="status">
              {{ owner.milestoneEdit.message }}
            </p>
          </section>
          <UiButton
            v-if="!state.task && !owner.sprintCreation.open && !owner.sprintCreatePending.value"
            ref="sprintCreateButton"
            :disabled="owner.busy.value || !owner.canCreateSprint.value"
            @click="owner.openSprintCreation()"
            >新建 Sprint</UiButton
          >
          <p v-if="!state.task && !state.milestones.length">
            暂无 Milestone，暂时无法创建 Sprint。
          </p>
          <SprintCreateForm
            v-if="owner.sprintCreation.open && !owner.sprintCreatePending.value"
            :owner="owner"
            @cancel="cancelSprintCreation"
            @submit="submitSprintCreation()"
          />
          <template v-if="state.sprint"
            ><header class="sprint-heading">
              <h2 ref="sprintHeading" tabindex="-1">{{ state.sprint.title }}</h2>
              <UiBadge>{{ sprintLabels[state.sprint.state] }}</UiBadge>
              <UiButton
                v-if="!state.task && !owner.sprintEdit.open && !owner.sprintEditPending.value"
                ref="sprintEditButton"
                :disabled="owner.busy.value || !owner.canEditSprint.value"
                @click="owner.openSprintEdit()"
                >编辑 Sprint</UiButton
              >
              <UiButton
                v-if="
                  state.sprint.state === 'planned' &&
                  !owner.sprintStart.open &&
                  !owner.sprintPending.value
                "
                ref="startButton"
                :disabled="owner.busy.value || !owner.canStartSprint.value"
                @click="openSprintStart"
                >启动 Sprint</UiButton
              >
              <UiButton
                v-if="!owner.creation.open"
                ref="createButton"
                :disabled="owner.busy.value || !owner.canCreate.value"
                @click="owner.openCreation()"
                >新建任务</UiButton
              >
            </header>
            <p v-if="!state.task" class="sprint-description">
              {{ state.sprint.description || '暂无描述。' }}
            </p>
            <SprintEditForm
              v-if="!state.task && owner.sprintEdit.open && !owner.sprintEditPending.value"
              :owner="owner"
              @cancel="cancelSprintEdit"
              @submit="saveSprintEdit()" />
            <p
              v-if="!state.task && !owner.sprintEdit.open && owner.sprintEdit.message"
              role="status"
            >
              {{ owner.sprintEdit.message }}
            </p>
            <p v-if="!state.task && state.sprint.state === 'completed'">
              已完成的 Sprint 不可修改标题或描述。
            </p>
            <p
              v-if="
                state.sprint.state === 'planned' &&
                !owner.canStartSprint.value &&
                !owner.sprintStart.open &&
                !owner.sprintPending.value &&
                !owner.sprintStart.needsRead
              "
            >
              启动需要当前 Project 尚无正在进行的 Sprint，并完成当前操作。
            </p>
            <p class="scheduler-note">
              启动 Sprint 不会改变 Scheduler 的启用状态；已暂停的调度保持暂停。
            </p>
            <form
              v-if="owner.sprintStart.open && !owner.sprintPending.value"
              class="sprint-start-form"
              aria-label="启动 Sprint 确认"
              @submit.prevent="submitSprintStart()"
            >
              <p>
                确认将“{{ state.sprint.title }}”设为当前 Sprint？此操作不会改变 Scheduler
                的启用状态。
              </p>
              <p v-if="owner.sprintStartStale.value" role="alert">
                Sprint 已变化，请取消后重新读取并核对。
              </p>
              <UiButton
                ref="startConfirmButton"
                type="submit"
                variant="primary"
                :disabled="
                  owner.busy.value || !owner.canStartSprint.value || owner.sprintStartStale.value
                "
                >确认启动 Sprint</UiButton
              >
              <UiButton :disabled="owner.busy.value" @click="cancelSprintStart">取消启动</UiButton>
            </form>
            <TaskCreateForm
              v-if="owner.creation.open && !owner.pending.value"
              :owner="owner"
              @cancel="cancelCreation" />
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
.milestone-description,
.sprint-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
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
.sprint-start-form,
.sprint-start-recovery {
  padding: var(--space);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  overflow-wrap: anywhere;
}
.sprint-start-form button + button {
  margin-left: var(--content-gap);
}
.scheduler-note {
  color: var(--muted);
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
.selected-milestone h2,
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
