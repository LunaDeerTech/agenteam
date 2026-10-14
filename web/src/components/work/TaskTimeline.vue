<script setup lang="ts">
import { ref, watch } from 'vue'
import { UiButton, UiField, UiSelect, UiState } from '../ui'
import { taskLabels } from '../../api/work-review'
import type { TaskTimelineEvent } from '../../api/work-task-timeline'
import type { TaskTimelineState } from '../../composables/useProjectTasks'
const props = defineProps<{
  state: TaskTimelineState
  busy: boolean
  agentName: (id: string | null) => string
}>()
const emit = defineEmits<{
  load: [more: boolean]
  filter: [field: 'type' | 'order', value: string]
}>()
const heading = ref<HTMLElement | null>(null)
const filters = ref<HTMLElement | null>(null)
let requestedFocus: HTMLElement | null = null
function request(more: boolean, event: MouseEvent) {
  if (props.busy) return
  const button = event.currentTarget as HTMLElement
  requestedFocus = document.activeElement === button ? button : null
  emit('load', more)
}
function filter(field: 'type' | 'order', value: string, event?: MouseEvent) {
  if (props.busy || props.state[field] === value) return
  const target = event
    ? (event.currentTarget as HTMLElement)
    : filters.value?.querySelector<HTMLElement>(`[data-timeline-filter="${field}"]`)
  const active = document.activeElement
  requestedFocus =
    target &&
    (active === target ||
      active?.closest('[role="listbox"]')?.getAttribute('aria-label') ===
        target.getAttribute('aria-label'))
      ? target
      : null
  emit('filter', field, value)
}
watch(
  () => props.state.phase,
  (phase, previous) => {
    if (previous !== 'loading' || phase === 'loading' || !requestedFocus) return
    const target = requestedFocus
    requestedFocus = null
    if (document.activeElement !== document.body && document.activeElement !== target) return
    if (target.isConnected && !(target as HTMLButtonElement).disabled) target.focus()
    else heading.value?.focus()
  },
  { flush: 'post' },
)

const typeNames = { feature: '功能', bug: '缺陷', task: '任务', spike: '调研', chore: '维护' }
const priorities = { critical: '紧急', high: '高', medium: '中', low: '低' }
const fields = {
  description: '描述',
  manual_rank: '排序',
  plan: '执行计划',
  priority: '优先级',
  title: '标题',
  type: '类型',
}
const blockers = { rely_on: '任务依赖', waiting_for_human: '等待人工', technical: '技术阻塞' }
const reasons = {
  scheduler_claim: '调度器领取任务',
  scheduler_agent_busy_compensation: 'Agent 忙，任务返回待执行',
  scheduler_launch_failed: '执行启动失败',
}
const titles = {
  task_created: '创建任务',
  fields_updated: '更新任务',
  state_changed: '状态变更',
  assignee_changed: '负责人变更',
  blocker_added: '添加阻塞',
  blocker_resolved: '解除阻塞',
  comment: '评论',
}
const typeOptions = [
  { value: 'all', label: '全部事件' },
  ...Object.entries(titles).map(([value, label]) => ({ value, label })),
]
const orderOptions = [
  { value: 'desc', label: '由新到旧' },
  { value: 'asc', label: '由旧到新' },
]
function references(item: TaskTimelineEvent): ReadonlyArray<readonly [string, string]> {
  const rows: [string, string][] = [
    ['事件 ID', item.id],
    ['操作 ID', item.operation_id],
  ]
  if (item.actor.type === 'human') rows.push(['用户 ID', item.actor.user_id])
  if (item.type === 'assignee_changed') {
    if (item.payload.from_agent_id) rows.push(['原 Agent ID', item.payload.from_agent_id])
    rows.push(['新 Agent ID', item.payload.to_agent_id])
  }
  if (item.type === 'blocker_added' || item.type === 'blocker_resolved')
    rows.push(['阻塞 ID', item.payload.blocker_id])
  if (item.type === 'task_created')
    rows.push(['Milestone ID', item.payload.milestone_id], ['Sprint ID', item.payload.sprint_id])
  if (item.type === 'fields_updated' && item.payload.position) {
    rows.push(
      ['Sprint ID', item.payload.position.sprint_id],
      ['排序版本', item.payload.position.order_generation],
    )
    if (item.payload.position.previous_id)
      rows.push(['前一任务 ID', item.payload.position.previous_id])
    if (item.payload.position.next_id) rows.push(['后一任务 ID', item.payload.position.next_id])
  }
  return rows
}
</script>
<template>
  <section class="task-timeline" aria-label="任务历史" :aria-busy="state.phase === 'loading'">
    <div class="timeline-heading">
      <h3 ref="heading" tabindex="-1">任务历史</h3>
      <UiButton :disabled="busy" @click="request(false, $event)">重新读取历史</UiButton>
    </div>
    <div ref="filters" class="timeline-filters">
      <UiField class="timeline-filter" label="事件类型" v-slot="field">
        <UiSelect
          :id="field.id"
          label="历史事件类型"
          data-timeline-filter="type"
          :model-value="state.type"
          :options="typeOptions"
          :disabled="busy"
          @update:model-value="filter('type', $event)"
        />
      </UiField>
      <UiField class="timeline-filter" label="时间顺序" v-slot="field">
        <UiSelect
          :id="field.id"
          label="历史时间顺序"
          data-timeline-filter="order"
          :model-value="state.order"
          :options="orderOptions"
          :disabled="busy"
          @update:model-value="filter('order', $event)"
        />
      </UiField>
      <UiButton v-if="state.type !== 'all'" :disabled="busy" @click="filter('type', 'all', $event)"
        >清除事件筛选</UiButton
      >
    </div>
    <p class="timeline-hint">显示已读取的任务历史；重新读取可查看最新事件。</p>
    <UiState v-if="state.phase === 'loading'" kind="loading" title="正在读取任务历史" />
    <div v-if="state.phase === 'error'" role="alert">
      <p>{{ state.message }}</p>
      <UiButton :disabled="busy" @click="request(state.retryMore, $event)">重试读取历史</UiButton>
    </div>
    <p v-if="state.phase === 'ready' && !state.items.length">
      {{ state.type === 'all' ? '暂无任务历史。' : '没有此类型的任务历史。' }}
    </p>
    <ol v-if="state.items.length" class="timeline-events">
      <li v-for="item in state.items" :key="item.id" class="timeline-event">
        <div class="event-heading">
          <h4>{{ titles[item.type] }}</h4>
          <span>{{ item.actor.type === 'human' ? '用户' : '系统调度' }}</span>
        </div>
        <time :datetime="item.created_at">{{ item.created_at }}</time>
        <p v-if="item.type === 'comment'" class="event-text">{{ item.payload.body }}</p>
        <template v-else-if="item.type === 'task_created'">
          <p>
            {{ taskLabels[item.payload.initial_state] }} · {{ typeNames[item.payload.type] }} ·
            {{ priorities[item.payload.priority] }}优先级
          </p>
        </template>
        <template v-else-if="item.type === 'fields_updated'">
          <p>修改：{{ item.payload.changed_fields.map((key) => fields[key]).join('、') }}</p>
          <p v-if="item.payload.type_change">
            类型：{{ typeNames[item.payload.type_change.from] }} →
            {{ typeNames[item.payload.type_change.to] }}
          </p>
          <p v-if="item.payload.priority_change">
            优先级：{{ priorities[item.payload.priority_change.from] }} →
            {{ priorities[item.payload.priority_change.to] }}
          </p>
        </template>
        <template v-else-if="item.type === 'state_changed'">
          <p>{{ taskLabels[item.payload.from_state] }} → {{ taskLabels[item.payload.to_state] }}</p>
          <p v-if="item.payload.reason_code">{{ reasons[item.payload.reason_code] }}</p>
        </template>
        <p v-else-if="item.type === 'assignee_changed'">
          {{ props.agentName(item.payload.from_agent_id) }} →
          {{ props.agentName(item.payload.to_agent_id) }}
        </p>
        <template v-else-if="item.type === 'blocker_added' || item.type === 'blocker_resolved'">
          <p>{{ blockers[item.payload.blocker_type] }}</p>
          <p v-if="item.type === 'blocker_added' && item.payload.reason_code">
            {{ reasons[item.payload.reason_code] }}
          </p>
          <p
            v-if="item.type === 'blocker_resolved' && item.payload.resolution_comment"
            class="event-text"
          >
            {{ item.payload.resolution_comment }}
          </p>
        </template>
        <details>
          <summary>事件身份信息</summary>
          <dl>
            <div>
              <dt>任务版本</dt>
              <dd>{{ item.task_version }}</dd>
            </div>
            <div v-for="[label, value] in references(item)" :key="label">
              <dt>{{ label }}</dt>
              <dd>{{ value }}</dd>
            </div>
          </dl>
        </details>
      </li>
    </ol>
    <UiButton v-if="state.next" :disabled="busy" @click="request(true, $event)">{{
      state.order === 'desc' ? '加载较早历史' : '加载较新历史'
    }}</UiButton>
  </section>
</template>
<style scoped>
.task-timeline {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  min-width: 0;
}
.task-timeline > .ui-button {
  align-self: flex-start;
}
.timeline-heading,
.event-heading {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: var(--space);
}
.timeline-heading {
  justify-content: space-between;
}
.timeline-filters {
  display: flex;
  align-items: end;
  flex-wrap: wrap;
  gap: var(--space);
}
.timeline-filter {
  flex: 1 1 12rem;
  min-width: 0;
}
h3,
h4 {
  font-size: 1rem;
}
h3,
h4,
p {
  margin: 0;
  overflow-wrap: anywhere;
}
.timeline-hint,
time,
.event-heading span {
  color: var(--muted);
  font-size: 0.875rem;
}
time {
  display: block;
  overflow-wrap: anywhere;
}
.timeline-events {
  display: flex;
  flex-direction: column;
  gap: var(--space);
  list-style: none;
  margin: 0;
  padding: 0;
}
.timeline-event {
  display: flex;
  flex-direction: column;
  gap: calc(var(--space) / 2);
  min-width: 0;
  padding: 0 0 20px 16px;
  border-left: 2px solid var(--border);
  border-bottom: 1px solid var(--border);
}
.event-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
details {
  min-width: 0;
}
summary {
  cursor: pointer;
  width: fit-content;
}
summary:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 3px;
}
dl {
  display: grid;
  gap: calc(var(--space) / 2);
  margin-bottom: 0;
}
dl div {
  min-width: 0;
}
dt {
  color: var(--muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
