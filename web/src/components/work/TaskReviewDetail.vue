<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { UiButton, UiField, UiSelect, UiTextarea, UiState, UiBadge } from '../ui'
import { taskLabels } from '../../api/work-review'
import type { ProjectTasks } from '../../composables/useProjectTasks'
import TaskEditForm from './TaskEditForm.vue'
import TaskReorderForm from './TaskReorderForm.vue'
import TaskDependencyForm from './TaskDependencyForm.vue'
import TaskHumanBlockerForm from './TaskHumanBlockerForm.vue'
import TaskTimeline from './TaskTimeline.vue'
import TaskExecutions from './TaskExecutions.vue'
const props = defineProps<{ owner: ProjectTasks }>()
const detail = ref<HTMLElement | null>(null)
const task = computed(() => props.owner.state.task)
const addBlockerButton = ref<{ $el: HTMLButtonElement } | null>(null)
let blockerTrigger: HTMLButtonElement | null = null
let blockerTask = ''
function openBlocker(event: MouseEvent, id?: string) {
  blockerTrigger = event.currentTarget as HTMLButtonElement
  blockerTask = task.value?.id ?? ''
  props.owner.openHumanBlocker(id)
}
async function focusBlocker(saved = false) {
  await nextTick()
  if (!props.owner.visible.value || task.value?.id !== blockerTask || props.owner.pending.value) return
  const target = saved ? addBlockerButton.value?.$el : blockerTrigger
  if (target?.isConnected && !target.disabled) target.focus()
}
async function cancelBlocker() {
  if (await props.owner.cancelHumanBlocker()) await focusBlocker()
}
async function submitBlocker() {
  if (await props.owner.submitHumanBlocker()) await focusBlocker(true)
}
const refreshButton = ref<{ $el: HTMLButtonElement } | null>(null)
const addDependencyButton = ref<{ $el: HTMLButtonElement } | null>(null)
let dependencyTrigger: HTMLButtonElement | null = null
let dependencyTask = ''
async function openDependency(event: MouseEvent, id?: string) {
  dependencyTrigger = event.currentTarget as HTMLButtonElement
  dependencyTask = task.value?.id ?? ''
  await props.owner.openDependency(id)
  await nextTick()
  if (props.owner.dependency.open && !props.owner.busy.value && task.value?.id === dependencyTask)
    detail.value?.querySelector<HTMLElement>('form[aria-label="添加任务依赖"] input, form[aria-label="解除任务依赖"] textarea')?.focus()
}
async function focusDependency(saved = false) {
  await nextTick()
  if (!props.owner.visible.value || task.value?.id !== dependencyTask || props.owner.pending.value) return
  const target = saved ? addDependencyButton.value?.$el : dependencyTrigger
  if (target?.isConnected && !target.disabled) target.focus()
  else if (!props.owner.busy.value) refreshButton.value?.$el.focus()
}
async function cancelDependency() {
  if (await props.owner.cancelDependency()) await focusDependency()
}
async function submitDependency() {
  if (await props.owner.submitDependency()) await focusDependency(true)
}
async function lookup() {
  const original = props.owner.progress.value
  if (original?.kind.startsWith('blocker-')) blockerTask = original.taskID
  if (original?.kind.startsWith('dependency-')) dependencyTask = original.taskID
  if (original?.kind === 'reorder') reorderTask = original.taskID
  const confirmed = await props.owner.lookup()
  if (original?.kind === 'reorder' && confirmed) await focusTaskReorder()
  if (original?.kind.startsWith('blocker-') && confirmed) await focusBlocker(true)
  if (original?.kind.startsWith('dependency-') && confirmed) await focusDependency(true)
}
const editButton = ref<{ $el: HTMLButtonElement } | null>(null)
async function cancelEdit() {
  const target = task.value?.id
  await props.owner.cancelEdit()
  await nextTick()
  if (!props.owner.edit.open && props.owner.visible.value && task.value?.id === target)
    editButton.value?.$el.focus()
}
const reorderButton = ref<{ $el: HTMLButtonElement } | null>(null)
let reorderTask = ''
async function openTaskReorder() {
  reorderTask = task.value?.id ?? ''
  await props.owner.openTaskReorder()
}
async function focusTaskReorder() {
  await nextTick()
  if (!props.owner.visible.value || props.owner.busy.value || task.value?.id !== reorderTask || props.owner.pending.value) return
  const target = reorderButton.value?.$el
  if (target?.isConnected && !target.disabled) target.focus()
  else detail.value?.querySelector<HTMLElement>('h2')?.focus()
}
async function cancelTaskReorder() {
  if (props.owner.cancelTaskReorder()) await focusTaskReorder()
}
async function submitTaskReorder() {
  if (await props.owner.submitTaskReorder()) await focusTaskReorder()
}
const actionLabel = computed(() =>
  props.owner.draft.action === 'in_review'
    ? '提交评审'
    : props.owner.draft.action === 'todo'
      ? task.value?.state === 'backlog'
        ? '指派'
        : '退回修改'
      : '接受并完成',
)
const unresolved = computed(() => props.owner.state.blockers.filter((b) => b.resolved_at === null))
const canSubmit = computed(
  () =>
    !props.owner.busy.value &&
    !props.owner.readOnly.value &&
    !!props.owner.draft.action &&
    (task.value?.state === 'backlog' || !!props.owner.draft.comment.trim()) &&
    (props.owner.draft.action === 'done' || !!props.owner.draft.agentID) &&
    (props.owner.draft.action !== 'todo' ||
      (!unresolved.value.length &&
        props.owner.state.blockerPhase === 'ready' &&
        !props.owner.state.blockerNext)),
)
</script>
<template>
  <div v-if="task" ref="detail" class="task-detail">
    <h2 tabindex="-1">{{ task.title }}</h2>
    <dl class="task-facts">
      <div>
        <dt>状态</dt>
        <dd>{{ taskLabels[task.state] }}</dd>
      </div>
      <div>
        <dt>版本</dt>
        <dd>{{ task.version }}</dd>
      </div>
      <div>
        <dt>负责人</dt>
        <dd>{{ owner.name(task.assignee_agent_id) }}</dd>
      </div>
      <div>
        <dt>类型</dt>
        <dd>{{ task.type }}</dd>
      </div>
      <div>
        <dt>优先级</dt>
        <dd>{{ task.priority }}</dd>
      </div>
      <div>
        <dt>Milestone</dt>
        <dd>{{ owner.state.milestone?.title }}</dd>
      </div>
      <div>
        <dt>Sprint</dt>
        <dd>{{ owner.state.sprint?.title }}</dd>
      </div>
      <div>
        <dt>任务 ID</dt>
        <dd>{{ task.id }}</dd>
      </div>
      <div>
        <dt>更新时间</dt>
        <dd>
          <time :datetime="task.updated_at">{{ task.updated_at }}</time>
        </dd>
      </div>
    </dl>
    <section>
      <h3>任务描述</h3>
      <p class="task-copy">{{ task.description || '暂无描述' }}</p>
    </section>
    <section>
      <h3>执行计划</h3>
      <p class="task-copy">{{ task.plan || '暂无计划' }}</p>
    </section>
    <section>
      <h3>阻塞与依赖</h3>
      <UiButton
        v-if="task.state === 'backlog' && task.assignee_agent_id === null && !owner.pending.value"
        ref="addBlockerButton"
        :disabled="owner.busy.value || !owner.canManageHumanBlockers.value || owner.humanBlocker.open"
        @click="openBlocker($event)"
        >添加人工阻塞</UiButton
      >
      <UiButton
        v-if="task.state === 'backlog' && task.assignee_agent_id === null && !owner.pending.value"
        ref="addDependencyButton"
        :disabled="owner.busy.value || !owner.canManageDependencies.value || owner.dependency.open"
        @click="openDependency($event)"
        >添加任务依赖</UiButton
      >
      <UiState v-if="owner.state.blockerPhase === 'loading'" kind="loading" title="正在读取阻塞" />
      <template v-else-if="owner.state.blockerPhase === 'error'"
        ><p>未取得完整阻塞信息。</p>
        <UiButton :disabled="owner.busy.value" @click="owner.loadBlockers()"
          >重新读取阻塞</UiButton
        ></template
      >
      <p v-else-if="!owner.state.blockers.length">暂无阻塞记录。</p>
      <ul v-else class="blockers">
        <li v-for="b in owner.state.blockers" :key="b.id">
          <UiBadge>{{ b.resolved_at ? '已解除' : '未解除' }}</UiBadge>
          {{
            b.type === 'technical' ? '调度技术阻塞' : b.type === 'rely_on' ? '任务依赖' : '等待人工'
          }}
          <p>{{ b.description }}</p>
          <p v-if="b.related_task_id">关联任务：{{ b.related_task_id }}</p>
          <UiButton v-if="b.type === 'rely_on' && b.related_task_id" :disabled="owner.busy.value" @click="owner.viewDependency(b.id)">查看关联任务</UiButton>
          <UiButton
            v-if="b.type === 'rely_on' && b.resolved_at === null && task.state === 'backlog' && task.assignee_agent_id === null && !owner.pending.value"
            :disabled="owner.busy.value || !owner.canManageDependencies.value || owner.dependency.open"
            @click="openDependency($event, b.id)"
            >解除任务依赖</UiButton
          >
          <p v-if="b.resolution_comment">{{ b.resolution_comment }}</p>
          <UiButton
            v-if="b.type === 'waiting_for_human' && b.resolved_at === null && task.state === 'backlog' && task.assignee_agent_id === null && !owner.pending.value"
            :disabled="owner.busy.value || !owner.canManageHumanBlockers.value || owner.humanBlocker.open"
            @click="openBlocker($event, b.id)"
            >解除人工阻塞</UiButton
          >
        </li>
      </ul>
      <UiButton
        v-if="owner.state.blockerNext"
        :disabled="owner.busy.value"
        @click="owner.loadBlockers(true)"
        >更多阻塞记录</UiButton
      >
      <TaskDependencyForm v-if="owner.dependency.open && !owner.pending.value" :owner="owner" @cancel="cancelDependency" @submit="submitDependency" />
      <TaskHumanBlockerForm
        v-if="owner.humanBlocker.open && !owner.pending.value"
        :owner="owner"
        @cancel="cancelBlocker"
        @submit="submitBlocker"
      />
    </section>
    <p v-if="owner.progress.value?.phase === 'uncertain'" role="status">
      提交结果待确认。请查询原操作结果，不要重复提交。
    </p>
    <UiButton
      v-if="owner.progress.value?.phase === 'uncertain'"
      :disabled="owner.busy.value || !owner.progress.value.contextValid"
      @click="lookup"
      >查询原操作结果</UiButton
    >
    <p v-if="owner.progress.value?.observation === 'in_progress'" role="status">
      原操作仍在处理中，稍后可再次查询。
    </p>
    <p v-if="owner.progress.value?.observation === 'not_observed'" role="status">
      尚未观察到原操作结果，可再次查询。
    </p>
    <p v-if="owner.state.feedback" role="status">{{ owner.state.feedback }}</p>
    <p v-if="owner.state.message" role="alert">{{ owner.state.message }}</p>
    <UiButton ref="refreshButton" :disabled="owner.busy.value" @click="owner.refresh()">重新读取任务</UiButton>
    <TaskReorderForm
      v-if="owner.taskReorder.open && !owner.pending.value"
      :owner="owner"
      @cancel="cancelTaskReorder"
      @submit="submitTaskReorder"
    />
    <TaskEditForm
      v-if="owner.edit.open && !owner.pending.value"
      :owner="owner"
      @cancel="cancelEdit"
    />
    <div v-if="!owner.pending.value && !owner.edit.open && !owner.humanBlocker.open && !owner.dependency.open && !owner.taskReorder.open" class="review-actions">
      <UiButton
        v-if="task.state === 'backlog' && task.assignee_agent_id === null"
        ref="editButton"
        :disabled="owner.busy.value || !owner.canEdit.value || !!owner.draft.action"
        @click="owner.openEdit()"
        >编辑任务</UiButton
      >
      <UiButton
        v-if="task.state === 'backlog' && task.assignee_agent_id === null"
        ref="reorderButton"
        :disabled="owner.busy.value || !owner.canReorderTask.value"
        :state="owner.progress.value?.kind === 'reorder' && owner.progress.value.phase === 'confirmed' ? 'success' : 'idle'"
        success-label="顺序已确认"
        @click="openTaskReorder"
        >调整任务顺序</UiButton
      >
      <UiButton
        v-if="task.state === 'backlog'"
        :disabled="owner.busy.value || owner.readOnly.value"
        @click="owner.chooseAction('todo')"
        >指派并加入待执行</UiButton
      >
      <UiButton
        v-if="task.state === 'in_progress'"
        :disabled="owner.busy.value || owner.readOnly.value"
        @click="owner.chooseAction('in_review')"
        >提交评审</UiButton
      >
      <template v-if="task.state === 'in_review'"
        ><UiButton
          :disabled="owner.busy.value || owner.readOnly.value"
          @click="owner.chooseAction('done')"
          >接受并完成</UiButton
        ><UiButton
          :disabled="owner.busy.value || owner.readOnly.value"
          @click="owner.chooseAction('todo')"
          >退回修改</UiButton
        ></template
      >
    </div>
    <p v-if="['done', 'cancelled'].includes(task.state)" role="status">
      此任务已结束，保留只读信息。
    </p>
    <form
      v-if="owner.draft.action && !owner.pending.value && !owner.edit.open && !owner.humanBlocker.open && !owner.dependency.open && !owner.taskReorder.open"
      class="review-form"
      @submit.prevent="owner.submit()"
    >
      <h3>{{ actionLabel }}</h3>
      <UiField
        v-if="owner.draft.action !== 'done'"
        :label="owner.draft.action === 'in_review' ? '评审 Agent' : '执行 Agent'"
        required
        v-slot="field"
        ><UiSelect
          :id="field.id"
          v-model="owner.draft.agentID"
          :label="owner.draft.action === 'in_review' ? '评审 Agent' : '执行 Agent'"
          :options="owner.agentOptions.value"
          :disabled="owner.busy.value || owner.readOnly.value"
      /></UiField>
      <template v-if="owner.draft.action !== 'done'"
        ><p v-if="owner.state.agentMessage" role="alert">{{ owner.state.agentMessage }}</p>
        <UiButton
          v-if="owner.state.agentNext"
          type="button"
          :disabled="owner.busy.value"
          @click="owner.loadAgents(true)"
          >更多 Agent</UiButton
        ><UiButton
          v-if="owner.state.agentMessage"
          type="button"
          :disabled="owner.busy.value"
          @click="owner.loadAgents()"
          >重新读取 Agent 目录</UiButton
        ></template
      >
      <UiField
        :label="task.state === 'backlog' ? '指派说明' : '评审说明'"
        :required="task.state !== 'backlog'"
        hint="填写的说明将作为任务历史保留。"
        v-slot="field"
        ><UiTextarea
          :id="field.id"
          v-model="owner.draft.comment"
          :aria-describedby="field.describedby"
          :disabled="owner.busy.value || owner.readOnly.value"
          rows="5"
      /></UiField>
      <p v-if="owner.draft.action === 'todo' && (unresolved.length || owner.state.blockerNext)">
        提交前需要读取并确认全部阻塞已解除。
      </p>
      <UiButton type="submit" :disabled="!canSubmit">确认{{ actionLabel }}</UiButton>
    </form>
    <TaskExecutions :owner="owner.executions" :agent-name="owner.name" />
    <TaskTimeline
      :state="owner.timeline"
      :busy="owner.busy.value"
      :agent-name="owner.name"
      @load="owner.loadTimeline"
      @filter="owner.setTimelineFilter"
    />
  </div>
</template>
<style scoped>
.task-detail {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.task-detail h2,
.task-detail h3 {
  margin: 0;
  overflow-wrap: anywhere;
}
.task-detail h3 {
  font-size: 1rem;
}
.task-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
}
.task-facts dt {
  font-size: 0.875rem;
  color: var(--muted);
}
.task-facts dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.task-copy {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.review-actions {
  display: flex;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
.review-form {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--space);
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
.blockers {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 12px;
}
.blockers li {
  overflow-wrap: anywhere;
}
.blockers p {
  white-space: pre-wrap;
}
@media (max-width: 600px) {
  .task-facts {
    grid-template-columns: 1fr;
  }
}
</style>
