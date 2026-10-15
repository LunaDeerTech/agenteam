<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowReactive, shallowRef, watch } from 'vue'
import { AccountFailure } from '../../api/client'
import type { MeetingContribution, MeetingContributionExecution } from '../../api/meetings'
import { useSession } from '../../composables/useSession'
import { useProjectWorkspace } from '../../composables/useProjectWorkspace'
import { createMeetingExecutionApproval } from '../../composables/useMeetingExecutionApproval'
import type { MeetingSubscription } from '../../composables/meeting-realtime'
import { UiButton, UiState } from '../ui'
import MeetingExecutionApproval from './MeetingExecutionApproval.vue'

const props = defineProps<{
  projectID: string
  meetingID: string
  contribution: MeetingContribution
  mutable: boolean
  disabled: boolean
}>()
const emit = defineEmits<{ refresh: [] }>()
const auth = useSession(),
  workspace = useProjectWorkspace()
const open = ref(false)
const subscription = shallowRef<MeetingSubscription | null>(null)
const runtime = computed(() => {
  const value = subscription.value?.state.snapshot
  return value?.schema === 'meeting_execution_runtime_v1' ? value.content : null
})
const state = shallowReactive({
  phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
  execution: null as MeetingContributionExecution | null,
  message: '',
})
const observedStatus = computed(() => runtime.value?.status ?? state.execution?.status)
const observedWaiting = computed(() => {
  const waiting = runtime.value?.items.find((item) => item.type === 'waiting')
  return runtime.value
    ? waiting?.type === 'waiting'
      ? waiting.waiting.reason
      : null
    : state.execution?.waiting_reason
})
const key = computed(() => {
  const scope = workspace.currentReadContext.value
  if (!scope || scope.projectID !== props.projectID) return ''
  return [
    scope.identity.userID,
    scope.identity.sessionID,
    scope.identity.epoch,
    scope.generation,
    scope.readGeneration,
    props.projectID,
    props.meetingID,
    props.contribution.id,
    props.contribution.execution_id,
    props.contribution.generation,
    props.contribution.attempt,
  ].join('/')
})
const scope = computed(() =>
  key.value
    ? {
        projectID: props.projectID,
        meetingID: props.meetingID,
        contributionID: props.contribution.id,
        execution: state.execution,
        key: key.value,
        mutable: props.mutable,
      }
    : null,
)
const busy = computed(() => props.disabled || auth.state.busy || state.phase === 'loading')
let generation = 0,
  disposed = false,
  requestedVersion = '',
  timer: ReturnType<typeof setTimeout> | undefined
const labels = {
  created: '等待准备',
  preparing: '正在准备',
  running: '正在执行',
  waiting: '等待处理',
  succeeded: '执行成功',
  failed: '执行失败',
  cancelled: '已取消',
}
function clearTimer() {
  if (timer !== undefined) clearTimeout(timer)
  timer = undefined
}
function schedule() {
  clearTimer()
  if (
    auth.meetingRealtime ||
    !open.value ||
    disposed ||
    state.phase !== 'ready' ||
    !state.execution ||
    ['succeeded', 'failed', 'cancelled'].includes(state.execution.status)
  )
    return
  timer = setTimeout(() => {
    timer = undefined
    if (busy.value || (typeof document !== 'undefined' && document.hidden)) {
      schedule()
      return
    }
    void load()
  }, 2500)
}
async function load() {
  const captured = key.value,
    contribution = props.contribution
  if (!captured || busy.value || !contribution.execution_id) return
  const serial = ++generation
  clearTimer()
  state.phase = 'loading'
  state.message = ''
  try {
    const result = await auth.meetings.contributionExecution(
      props.projectID,
      props.meetingID,
      contribution.id,
    )
    if (disposed || key.value !== captured || serial !== generation) return
    if (
      result.id !== contribution.execution_id ||
      result.turn_id !== contribution.turn_id ||
      result.participant_id !== contribution.participant_id ||
      result.generation !== contribution.generation ||
      result.attempt !== contribution.attempt
    ) {
      state.execution = null
      subscription.value?.dispose()
      subscription.value = null
      state.phase = 'error'
      state.message = '发言已有新执行，请刷新会议后重新查看。'
      emit('refresh')
      return
    }
    state.execution = result
    state.phase = 'ready'
    if (!subscription.value && auth.meetingRealtime && open.value) {
      subscription.value = auth.meetingRealtime.subscribe({
        kind: 'meeting.execution',
        project_id: props.projectID,
        meeting_id: props.meetingID,
        contribution_id: contribution.id,
        generation: contribution.generation,
        attempt: contribution.attempt,
        execution_id: contribution.execution_id,
      })
    }
    schedule()
  } catch (error) {
    if (disposed || key.value !== captured || serial !== generation) return
    state.phase = 'error'
    state.message = '未能读取完整执行状态，请重新读取。'
    if (error instanceof AccountFailure && [401, 403, 404].includes(error.problem?.status ?? 0)) {
      state.execution = null
      subscription.value?.dispose()
      subscription.value = null
      state.message = '当前执行不可用或无权访问。'
      auth.executionApproval.forgetMeetingUnavailable(
        props.projectID,
        props.meetingID,
        contribution.id,
      )
    }
  }
}
const approval = createMeetingExecutionApproval(scope, busy, auth.executionApproval, async () => {
  await load()
  emit('refresh')
})
async function toggle() {
  open.value = !open.value
  if (open.value) await load()
  else {
    clearTimer()
    subscription.value?.dispose()
    subscription.value = null
    requestedVersion = ''
  }
}
function refresh() {
  subscription.value?.refresh()
  void load()
}
watch(
  [runtime, busy],
  ([value, blocked]) => {
    if (
      !value ||
      blocked ||
      !open.value ||
      value.execution_version === state.execution?.version ||
      requestedVersion === value.execution_version
    )
      return
    requestedVersion = value.execution_version
    // Approval controls still use the original authorized HTTP read; the live
    // projection only signals that its canonical version has changed.
    void load()
  },
  { flush: 'post' },
)
watch(
  key,
  () => {
    ++generation
    clearTimer()
    subscription.value?.dispose()
    subscription.value = null
    requestedVersion = ''
    state.execution = null
    state.phase = 'idle'
    state.message = ''
    open.value = false
  },
  { flush: 'sync' },
)
onScopeDispose(() => {
  disposed = true
  ++generation
  clearTimer()
  subscription.value?.dispose()
  approval.dispose()
})
</script>
<template>
  <div class="contribution-execution">
    <UiButton
      variant="ghost"
      :disabled="disabled || !key"
      :aria-expanded="open"
      @click="toggle()"
      >{{ open ? '收起执行详情' : '查看执行与审批' }}</UiButton
    >
    <section
      v-if="open"
      class="execution-details"
      aria-label="发言执行详情"
      :aria-busy="state.phase === 'loading'"
    >
      <UiState
        v-if="state.phase === 'loading' && !state.execution"
        kind="loading"
        title="正在读取执行"
      />
      <UiState
        v-if="state.phase === 'error'"
        kind="error"
        title="执行状态未确认"
        :description="state.message"
      />
      <template v-if="state.execution">
        <p role="status">
          {{
            observedStatus === 'waiting'
              ? observedWaiting === 'approval'
                ? '等待工具操作审批'
                : '等待用户决定'
              : observedStatus
                ? labels[observedStatus]
                : ''
          }}
        </p>
        <p
          v-if="
            state.execution.cancel_requested_at &&
            !['succeeded', 'failed', 'cancelled'].includes(observedStatus ?? '')
          "
          class="muted"
        >
          取消已请求，正在等待原执行停止。
        </p>
        <dl>
          <dt>执行 ID</dt>
          <dd>{{ state.execution.id }}</dd>
          <dt>回复版本 / 尝试</dt>
          <dd>{{ state.execution.generation }} / {{ state.execution.attempt }}</dd>
        </dl>
      </template>
      <section v-if="subscription" class="runtime-content" aria-label="执行过程">
        <p class="muted" role="status">
          {{
            subscription.state.phase === 'ready'
              ? '执行过程已更新'
              : subscription.state.phase === 'connecting'
                ? '正在连接执行过程'
                : subscription.state.phase === 'loading'
                  ? '正在读取执行过程'
                  : '更新已暂停，显示最近确认内容'
          }}
        </p>
        <template v-if="runtime">
          <ol
            v-if="runtime.items.some((item) => item.type === 'tool' || item.type === 'text')"
            class="runtime-items"
          >
            <template v-for="item in runtime.items" :key="item.id">
              <li v-if="item.type === 'tool'" class="runtime-item">
                <div class="runtime-tool-heading">
                  <strong>{{ item.tool.name }}</strong
                  ><span class="muted">{{
                    item.tool.result_available
                      ? item.tool.is_error
                        ? '处理失败'
                        : '已有结果'
                      : '等待结果'
                  }}</span>
                </div>
                <pre v-if="item.tool.result_available && item.tool.result_text">{{
                  item.tool.result_text
                }}</pre>
                <p v-if="item.tool.text_truncated" class="muted">结果较长，此处显示摘要。</p>
                <p v-if="item.tool.image_parts || item.tool.file_parts" class="muted">
                  {{ item.tool.image_parts }} 张图片 · {{ item.tool.file_parts }} 个文件
                </p>
              </li>
              <li v-else-if="item.type === 'text'" class="runtime-item">
                <pre>{{ item.text.content }}</pre>
              </li>
            </template>
          </ol>
          <p v-else class="muted">尚无工具结果或已提交文本。</p>
        </template>
        <UiButton
          v-if="subscription.state.phase === 'stale'"
          :disabled="busy"
          @click="subscription.refresh()"
          >重新连接执行过程</UiButton
        >
      </section>
      <UiButton :disabled="busy" @click="refresh()">刷新执行状态</UiButton>
      <MeetingExecutionApproval :owner="approval" />
    </section>
  </div>
</template>
<style scoped>
.contribution-execution {
  margin-top: 8px;
}
.runtime-content,
.runtime-items {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.runtime-items {
  list-style: none;
  padding: 0;
  margin: 0;
}
.runtime-item {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
}
.runtime-tool-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.runtime-item pre {
  font: inherit;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 8px 0 0;
  max-height: 24rem;
  overflow: auto;
}
.execution-details {
  display: grid;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
p,
dl {
  margin: 0;
}
dt,
.muted {
  color: var(--muted);
  font-size: 12px;
}
dd {
  margin: 4px 0 8px;
  overflow-wrap: anywhere;
}
</style>
