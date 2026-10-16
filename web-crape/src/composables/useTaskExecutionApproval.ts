import { computed, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import type { ExecutionApproval } from '../api/execution-approval'
import type { TaskExecution } from '../api/task-executions'
import type { SessionController } from './useSession'

export type TaskExecutionApprovalScope = Readonly<{
  projectID: string
  taskID: string
  execution: TaskExecution | null
  key: string
  mutable: boolean
}>
export function createTaskExecutionApproval(
  scope: Readonly<Ref<TaskExecutionApprovalScope | null>>,
  busy: Readonly<Ref<boolean>>,
  port: SessionController['executionApproval'] | undefined,
  refresh: (project: string, task: string, execution: string, accepted: boolean) => Promise<void>,
) {
  const state = shallowReactive({
    phase: 'idle' as 'idle' | 'loading' | 'ready' | 'error',
    approval: null as ExecutionApproval | null,
    decision: null as 'approve' | 'reject' | null,
    message: '',
    active: 0,
  })
  let generation = 0,
    disposed = false
  const progress = computed(() => {
    const current = scope.value,
      value = port?.progress
    if (
      !current ||
      !value ||
      value.projectID !== current.projectID ||
      value.taskID !== current.taskID
    )
      return null
    if (
      !['submitting', 'uncertain'].includes(value.phase) &&
      current.execution &&
      (current.execution.id !== value.executionID ||
        (current.execution.waiting_reference &&
          current.execution.waiting_reference !== value.approvalID))
    )
      return null
    return value
  })
  const reference = computed(() => {
    const execution = scope.value?.execution
    return execution?.status === 'waiting' && execution.waiting_reason === 'approval'
      ? (execution.waiting_reference ?? null)
      : null
  })
  const visible = computed(
    () => !disposed && !!port && !!scope.value && (!!reference.value || !!progress.value),
  )
  const blocked = computed(() => !visible.value || busy.value || state.active !== 0)
  const canDecide = computed(() => {
    const current = scope.value,
      execution = current?.execution,
      approval = state.approval
    return (
      !blocked.value &&
      !!current?.mutable &&
      !progress.value &&
      state.phase === 'ready' &&
      !!execution &&
      execution.cancel_requested_at === null &&
      !!approval &&
      approval.status === 'pending' &&
      approval.id === reference.value &&
      approval.execution_id === execution.id &&
      approval.agent_id === execution.agent_id
    )
  })
  function reset() {
    ++generation
    Object.assign(state, { phase: 'idle', approval: null, decision: null, message: '', active: 0 })
  }
  const stop = watch(() => `${scope.value?.key ?? ''}/${reference.value ?? ''}`, reset, {
    flush: 'sync',
  })
  function current(key: string, serial: number) {
    return !disposed && scope.value?.key === key && serial === generation
  }
  function explanation(error: unknown) {
    if (
      error instanceof AccountFailure &&
      ['VERSION_CONFLICT', 'CONFIRMATION_STALE'].includes(error.problem?.code ?? '')
    )
      return '审批或执行已变化，请重新读取执行后再决定。'
    if (error instanceof AccountFailure && [401, 403, 404].includes(error.problem?.status ?? 0))
      return '当前审批不可用或无权访问，请重新读取。'
    return '尚未确认审批结果。结果未知时仅可核对原请求。'
  }
  async function load() {
    const captured = scope.value,
      approvalID = reference.value,
      execution = captured?.execution
    if (!captured || !execution || !approvalID || !port || blocked.value) return false
    const serial = ++generation
    state.active = serial
    state.phase = 'loading'
    state.approval = null
    state.decision = null
    state.message = ''
    try {
      const result = await port.get({
        projectID: captured.projectID,
        executionID: execution.id,
        agentID: execution.agent_id,
        approvalID,
      })
      if (!current(captured.key, serial) || reference.value !== approvalID) return false
      state.approval = result
      state.phase = 'ready'
      return true
    } catch (error) {
      if (current(captured.key, serial)) {
        state.phase = 'error'
        state.message = explanation(error)
      }
      return false
    } finally {
      if (current(captured.key, serial)) state.active = 0
    }
  }
  function choose(decision: 'approve' | 'reject') {
    if (!canDecide.value) return false
    state.decision = decision
    state.message = ''
    return true
  }
  function keep() {
    if (blocked.value) return false
    state.decision = null
    return true
  }
  async function submit() {
    const captured = scope.value,
      execution = captured?.execution,
      approval = state.approval,
      decision = state.decision
    if (!captured || !execution || !approval || !decision || !port || !canDecide.value) return false
    const serial = ++generation
    state.active = serial
    state.message = ''
    try {
      await port.start(captured.projectID, captured.taskID, execution, approval, decision)
      if (!current(captured.key, serial)) return false
      state.decision = null
      await refresh(captured.projectID, captured.taskID, execution.id, true)
      return true
    } catch (error) {
      if (current(captured.key, serial)) state.message = explanation(error)
      return false
    } finally {
      if (current(captured.key, serial)) state.active = 0
    }
  }
  async function lookup() {
    const captured = scope.value,
      original = progress.value
    if (
      !captured ||
      !port ||
      blocked.value ||
      original?.phase !== 'uncertain' ||
      !original.contextValid
    )
      return false
    const serial = ++generation
    state.active = serial
    state.message = ''
    try {
      const value = await port.checkOriginal()
      if (!current(captured.key, serial)) return false
      if ('status' in value && value.status === 'not_observed') {
        state.message = '尚未查到原审批决定的提交记录，请继续核对原请求。'
        return false
      }
      state.decision = null
      await refresh(captured.projectID, captured.taskID, original.executionID, true)
      return true
    } catch (error) {
      if (current(captured.key, serial)) state.message = explanation(error)
      return false
    } finally {
      if (current(captured.key, serial)) state.active = 0
    }
  }
  async function reread() {
    const captured = scope.value,
      original = progress.value
    if (!captured || !port || blocked.value || original?.phase !== 'rejected') return false
    try {
      port.editRejected()
      reset()
      await refresh(captured.projectID, captured.taskID, original.executionID, false)
      return true
    } catch (error) {
      if (!disposed && scope.value?.key === captured.key) state.message = explanation(error)
      return false
    }
  }
  function dispose() {
    disposed = true
    stop()
    reset()
  }
  return {
    state,
    progress,
    reference,
    visible,
    blocked,
    canDecide,
    load,
    choose,
    keep,
    submit,
    lookup,
    reread,
    dispose,
  }
}
export type TaskExecutionApproval = ReturnType<typeof createTaskExecutionApproval>
