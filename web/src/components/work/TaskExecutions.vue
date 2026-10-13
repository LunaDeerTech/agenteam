<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { UiBadge, UiButton, UiState } from '../ui'
import type { TaskExecution } from '../../api/task-executions'
import type { TaskExecutions } from '../../composables/useTaskExecutions'
import TaskExecutionOutput from './TaskExecutionOutput.vue'
const props = defineProps<{ owner: TaskExecutions; agentName: (id: string | null) => string }>()
const section = ref<HTMLElement | null>(null)
const heading = ref<HTMLElement | null>(null)
const detailHeading = ref<HTMLElement | null>(null)
const openButton = ref<{ $el: HTMLButtonElement } | null>(null)
const labels = {
  created: '已创建',
  preparing: '准备中',
  running: '执行中',
  waiting: '等待中',
  succeeded: '执行成功',
  failed: '执行失败',
  cancelled: '已取消',
}
const reasons = {
  completed: '正常完成',
  model_failed: '模型调用失败',
  incomplete_response: '模型响应不完整',
  runtime_failed: '运行失败',
  cancelled: '执行已取消',
}
const terminal = (item: TaskExecution) => ['succeeded', 'failed', 'cancelled'].includes(item.status)
const status = (item: TaskExecution) =>
  item.cancel_requested_at && !terminal(item) ? '正在取消' : labels[item.status]
const tone = (item: TaskExecution): 'neutral' | 'success' | 'danger' | 'progress' | 'warning' =>
  item.cancel_requested_at && !terminal(item)
    ? 'warning'
    : item.status === 'succeeded'
      ? 'success'
      : item.status === 'failed'
        ? 'danger'
        : ['running', 'preparing'].includes(item.status)
          ? 'progress'
          : 'neutral'
async function load(more: boolean, event: MouseEvent) {
  const trigger = event.currentTarget as HTMLButtonElement,
    focused = document.activeElement === trigger
  await props.owner.load(more)
  await nextTick()
  if (
    !focused ||
    !props.owner.visible.value ||
    (document.activeElement !== document.body && document.activeElement !== trigger)
  )
    return
  if (trigger.isConnected && !trigger.disabled) trigger.focus()
  else heading.value?.focus()
}
async function select(id: string) {
  const pending = props.owner.select(id)
  await nextTick()
  if (props.owner.state.selectedID === id) detailHeading.value?.focus()
  await pending
  await nextTick()
  if (
    props.owner.visible.value &&
    !props.owner.state.selectedID &&
    document.activeElement === document.body
  )
    heading.value?.focus()
}
async function refresh(event: MouseEvent) {
  const trigger = event.currentTarget as HTMLButtonElement,
    focused = document.activeElement === trigger
  await props.owner.select(props.owner.state.selectedID)
  await nextTick()
  if (
    !focused ||
    !props.owner.visible.value ||
    (document.activeElement !== document.body && document.activeElement !== trigger)
  )
    return
  if (trigger.isConnected && !trigger.disabled) trigger.focus()
  else if (props.owner.state.selectedID) detailHeading.value?.focus()
  else heading.value?.focus()
}
async function loadOutput(event: MouseEvent) {
  const trigger = event.currentTarget as HTMLButtonElement, focused = document.activeElement === trigger
  await props.owner.loadOutput()
  await nextTick()
  if (!focused || !props.owner.visible.value || (document.activeElement !== document.body && document.activeElement !== trigger)) return
  if (trigger.isConnected && !trigger.disabled) trigger.focus()
  else if (props.owner.state.selectedID) detailHeading.value?.focus()
  else heading.value?.focus()
}
async function back() {
  const id = props.owner.state.selectedID
  if (!props.owner.back()) return
  await nextTick()
  section.value?.querySelector<HTMLButtonElement>(`[data-execution-id="${id}"]`)?.focus()
}
async function close() {
  if (!props.owner.close()) return
  await nextTick()
  openButton.value?.$el.focus()
}
</script>
<template>
  <section
    v-if="owner.visible.value"
    ref="section"
    class="task-executions"
    aria-label="执行记录"
    :aria-busy="owner.state.active !== 0"
  >
    <div class="execution-heading">
      <h3 ref="heading" tabindex="-1">执行记录</h3>
      <UiButton
        v-if="!owner.state.open"
        ref="openButton"
        :disabled="owner.blocked.value"
        @click="load(false, $event)"
        >查看执行记录</UiButton
      >
      <UiButton v-else :disabled="owner.blocked.value" @click="close">收起执行记录</UiButton>
    </div>
    <template v-if="owner.state.open">
      <p class="execution-note">执行状态独立于任务状态。</p>
      <div v-if="owner.state.selectedID" class="execution-detail">
        <div class="execution-heading">
          <h4 ref="detailHeading" tabindex="-1">执行详情</h4>
          <div class="execution-actions">
            <UiButton :disabled="owner.blocked.value" @click="back">返回执行列表</UiButton>
            <UiButton :disabled="owner.blocked.value" @click="refresh">刷新执行详情</UiButton>
          </div>
        </div>
        <UiState
          v-if="owner.state.detailPhase === 'loading'"
          kind="loading"
          title="正在读取执行详情"
        />
        <UiState
          v-else-if="owner.state.detailPhase === 'error'"
          kind="error"
          title="未能读取执行详情"
          :description="owner.state.detailMessage"
        >
          <UiButton :disabled="owner.blocked.value" @click="refresh">重试执行详情</UiButton>
        </UiState>
        <dl v-else-if="owner.state.detail" class="execution-facts">
          <div>
            <dt>执行状态</dt>
            <dd>
              <UiBadge :tone="tone(owner.state.detail)">{{ status(owner.state.detail) }}</UiBadge>
            </dd>
          </div>
          <div>
            <dt>用途</dt>
            <dd>{{ owner.state.detail.purpose === 'task/work' ? '任务执行' : '任务评审' }}</dd>
          </div>
          <div>
            <dt>执行 Agent</dt>
            <dd>{{ agentName(owner.state.detail.agent_id) }}</dd>
          </div>
          <div>
            <dt>Agent ID</dt>
            <dd>{{ owner.state.detail.agent_id }}</dd>
          </div>
          <div>
            <dt>执行 ID</dt>
            <dd>{{ owner.state.detail.id }}</dd>
          </div>
          <div>
            <dt>执行版本</dt>
            <dd>{{ owner.state.detail.version }}</dd>
          </div>
          <div>
            <dt>任务 ID</dt>
            <dd>{{ owner.state.detail.task_id }}</dd>
          </div>
          <div>
            <dt>项目 ID</dt>
            <dd>{{ owner.state.detail.project_id }}</dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>
              <time :datetime="owner.state.detail.created_at">{{
                owner.state.detail.created_at
              }}</time>
            </dd>
          </div>
          <div>
            <dt>开始时间</dt>
            <dd>
              <time
                v-if="owner.state.detail.started_at"
                :datetime="owner.state.detail.started_at"
                >{{ owner.state.detail.started_at }}</time
              ><span v-else>未记录</span>
            </dd>
          </div>
          <div>
            <dt>结束时间</dt>
            <dd>
              <time
                v-if="owner.state.detail.completed_at"
                :datetime="owner.state.detail.completed_at"
                >{{ owner.state.detail.completed_at }}</time
              ><span v-else>尚未结束</span>
            </dd>
          </div>
          <div>
            <dt>取消请求时间</dt>
            <dd>
              <time
                v-if="owner.state.detail.cancel_requested_at"
                :datetime="owner.state.detail.cancel_requested_at"
                >{{ owner.state.detail.cancel_requested_at }}</time
              ><span v-else>无取消请求</span>
            </dd>
          </div>
          <div>
            <dt>结束原因</dt>
            <dd>{{ owner.state.detail.reason ? reasons[owner.state.detail.reason] : '未提供' }}</dd>
          </div>
          <div>
            <dt>执行快照 ID</dt>
            <dd>{{ owner.state.detail.snapshot_id ?? '未生成' }}</dd>
          </div>
        </dl>
        <TaskExecutionOutput v-if="owner.state.detail && owner.state.detailPhase === 'ready'" :owner="owner" @load="loadOutput" />
      </div>
      <template v-else>
        <UiButton :disabled="owner.blocked.value" @click="load(false, $event)"
          >刷新执行列表</UiButton
        >
        <UiState v-if="owner.state.phase === 'loading'" kind="loading" title="正在读取执行记录" />
        <UiState
          v-else-if="owner.state.phase === 'error'"
          kind="error"
          title="未能读取执行记录"
          :description="owner.state.message"
        >
          <UiButton :disabled="owner.blocked.value" @click="load(owner.state.retryMore, $event)"
            >重试执行列表</UiButton
          >
        </UiState>
        <UiState
          v-else-if="!owner.state.items.length"
          kind="empty"
          title="暂无执行记录"
          description="此任务尚无可查看的执行记录。"
        />
        <ol v-if="owner.state.items.length" class="execution-list">
          <li v-for="item in owner.state.items" :key="item.id">
            <div class="execution-heading">
              <UiButton
                :data-execution-id="item.id"
                :disabled="owner.blocked.value"
                @click="select(item.id)"
                >{{ item.purpose === 'task/work' ? '任务执行' : '任务评审' }} ·
                {{ item.id }}</UiButton
              >
              <UiBadge :tone="tone(item)">{{ status(item) }}</UiBadge>
            </div>
            <p>
              {{ agentName(item.agent_id) }} ·
              <time :datetime="item.created_at">{{ item.created_at }}</time>
            </p>
          </li>
        </ol>
        <UiButton
          v-if="owner.state.next && owner.state.phase !== 'error'"
          :disabled="owner.blocked.value"
          @click="load(true, $event)"
          >加载较早执行</UiButton
        >
      </template>
    </template>
  </section>
</template>
<style scoped>
.task-executions,
.execution-detail {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  min-width: 0;
}
.task-executions {
  border-top: 1px solid var(--border);
  padding-top: var(--space);
}
.execution-heading,
.execution-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--content-gap);
  flex-wrap: wrap;
}
.execution-heading h3,
.execution-heading h4,
.execution-note {
  margin: 0;
}
.execution-note,
.execution-list p,
.execution-facts dt {
  color: var(--muted);
  font-size: 0.875rem;
}
.execution-list {
  display: flex;
  flex-direction: column;
  gap: var(--content-gap);
  list-style: none;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.execution-list li {
  border-bottom: 1px solid var(--border);
  padding-bottom: var(--content-gap);
  min-width: 0;
}
.execution-list .ui-button {
  height: auto;
  white-space: normal;
  text-align: left;
  overflow-wrap: anywhere;
  min-width: 0;
}
.execution-list p {
  margin: var(--content-gap) 0 0;
  overflow-wrap: anywhere;
}
.execution-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space);
  margin: 0;
}
.execution-facts dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .execution-facts {
    grid-template-columns: 1fr;
  }
}
</style>
