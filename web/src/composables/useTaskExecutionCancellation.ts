import { computed, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import type { TaskExecution } from '../api/task-executions'
import type { SessionController } from './useSession'

export type TaskExecutionCancellationScope = Readonly<{
  projectID: string
  taskID: string
  taskVersion: string
  execution: TaskExecution | null
  key: string
  mutable: boolean
}>
type Port = SessionController['executionCancel']
export function createTaskExecutionCancellation(
  scope: Readonly<Ref<TaskExecutionCancellationScope | null>>,
  busy: Readonly<Ref<boolean>>,
  port: Port | undefined,
  refresh: (project: string, task: string, execution: string, accepted: boolean) => Promise<void>,
) {
  const state = shallowReactive({ open: false, reason: '', message: '', active: false })
  let opened: {
      key: string
      executionID: string
      executionVersion: string
      taskVersion: string
    } | null = null,
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
      current.execution &&
      current.execution.id !== value.executionID &&
      !['submitting', 'uncertain'].includes(value.phase)
    )
      return null
    return value
  })
  const blocked = computed(() => disposed || busy.value || state.active || !scope.value || !port)
  const visible = computed(
    () =>
      !disposed &&
      !!port &&
      !!scope.value &&
      (!!progress.value ||
        (!!scope.value.execution &&
          !['succeeded', 'failed', 'cancelled'].includes(scope.value.execution.status))),
  )
  const canStart = computed(() => {
    const current = scope.value,
      execution = current?.execution
    return (
      !blocked.value &&
      !!current?.mutable &&
      !!execution &&
      !progress.value &&
      !execution.cancel_requested_at &&
      !['succeeded', 'failed', 'cancelled'].includes(execution.status)
    )
  })
  const stale = computed(() => {
    const value = scope.value
    return (
      !opened ||
      !value ||
      !value.execution ||
      opened.key !== value.key ||
      opened.executionID !== value.execution.id ||
      opened.executionVersion !== value.execution.version ||
      opened.taskVersion !== value.taskVersion
    )
  })
  const reasonValid = computed(
    () =>
      state.reason === '' ||
      (!/^\p{White_Space}*$/u.test(state.reason) &&
        !state.reason.includes('\0') &&
        new TextEncoder().encode(state.reason).length <= 1024 &&
        !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(
          state.reason,
        )),
  )
  const canSubmit = computed(
    () => state.open && canStart.value && !stale.value && reasonValid.value,
  )
  function reset() {
    state.open = false
    state.reason = ''
    state.message = ''
    opened = null
  }
  const stop = watch(() => scope.value?.key ?? '', reset, { flush: 'sync' })
  function current(key: string) {
    return !disposed && scope.value?.key === key
  }
  function open() {
    if (!canStart.value || !scope.value?.execution) return false
    const value = scope.value
    opened = {
      key: value.key,
      executionID: value.execution!.id,
      executionVersion: value.execution!.version,
      taskVersion: value.taskVersion,
    }
    state.open = true
    state.message = ''
    state.reason = ''
    return true
  }
  function keep() {
    if (blocked.value || progress.value?.phase === 'uncertain') return false
    reset()
    return true
  }
  function explanation(error: unknown) {
    if (
      error instanceof AccountFailure &&
      ['VERSION_CONFLICT', 'TASK_VERSION_CONFLICT'].includes(error.problem?.code ?? '')
    )
      return '任务或执行已变化，请重新读取后再决定。'
    if (error instanceof AccountFailure && error.problem?.code === 'INVALID_STATE')
      return '当前执行不再接受取消，请重新读取当前状态。'
    if (error instanceof AccountFailure && [401, 403, 404].includes(error.problem?.status ?? 0))
      return '当前会话或执行不可用，请重新读取。'
    return '取消结果尚未确认。结果未知时仅可核对原请求。'
  }
  async function submit() {
    const captured = scope.value,
      original = opened
    if (!captured || !original || !port || !canSubmit.value) return false
    state.active = true
    state.message = ''
    try {
      await port.start(captured.projectID, captured.taskID, original.executionID, {
        expected_version: original.executionVersion,
        expected_task_version: original.taskVersion,
        ...(state.reason === '' ? {} : { reason: state.reason }),
      })
      if (!current(captured.key)) return false
      state.open = false
      // The receipt is the accepted postimage. Re-read authoritative current
      // Task/blockers and Execution instead of synthesizing cancelled/done.
      await refresh(captured.projectID, captured.taskID, original.executionID, true)
      return true
    } catch (error) {
      if (current(captured.key)) state.message = explanation(error)
      return false
    } finally {
      state.active = false
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
    state.active = true
    state.message = ''
    try {
      const result = await port.checkOriginal()
      if (!current(captured.key)) return false
      if ('found' in result && !result.found) {
        state.message = '尚未查到原取消请求的提交记录，请继续核对原请求。'
        return false
      }
      state.open = false
      await refresh(captured.projectID, captured.taskID, original.executionID, true)
      return true
    } catch (error) {
      if (current(captured.key)) state.message = explanation(error)
      return false
    } finally {
      state.active = false
    }
  }
  async function reread() {
    const captured = scope.value,
      original = progress.value
    if (!captured || !port || blocked.value || original?.phase !== 'rejected') return false
    const execution = original.executionID
    try {
      port.editRejected()
      reset()
      state.active = true
      await refresh(captured.projectID, captured.taskID, execution, false)
      return true
    } catch (error) {
      if (current(captured.key)) state.message = explanation(error)
      return false
    } finally {
      state.active = false
    }
  }
  function dispose() {
    disposed = true
    stop()
    reset()
  }
  return {
    state,
    visible,
    blocked,
    progress,
    canStart,
    canSubmit,
    stale,
    reasonValid,
    open,
    keep,
    submit,
    lookup,
    reread,
    dispose,
  }
}
export type TaskExecutionCancellation = ReturnType<typeof createTaskExecutionCancellation>
