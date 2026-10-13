import { computed, shallowReactive, watch, type Ref } from 'vue'
import { AccountFailure } from '../api/client'
import type { TaskExecutionOutput } from '../api/task-execution-output'
import {
  compareTaskExecutions,
  type TaskExecution,
  type TaskExecutionPage,
  type TaskExecutionQuery,
} from '../api/task-executions'

export type TaskExecutionScope = Readonly<{ projectID: string; taskID: string; key: string }>
type Phase = 'idle' | 'loading' | 'ready' | 'error'
type Reads = {
  list(project: string, task: string, query: TaskExecutionQuery): Promise<TaskExecutionPage>
  get(project: string, task: string, execution: string): Promise<TaskExecution>
  output(project: string, task: string, execution: string): Promise<TaskExecutionOutput>
}
function explanation(error: unknown) {
  const problem = error instanceof AccountFailure ? error.problem : undefined
  if (problem?.status === 401) return '当前会话已失效，请重新登录。'
  if (problem && [403, 404, 410].includes(problem.status))
    return '执行记录不可用或无权访问，请重新读取。'
  if (problem?.code === 'CURSOR_INVALID') return '执行记录分页已失效，请从头重新读取。'
  return '未取得完整执行记录，请在当前请求结束后重试。'
}

// Task navigation and Session own cancellation and the actual request tail. This
// view only retires observations; it must never abandon another Task read.
export function createTaskExecutions(
  scope: Readonly<Ref<TaskExecutionScope | null>>,
  busy: Readonly<Ref<boolean>>,
  reads: Reads,
) {
  const state = shallowReactive({
    open: false,
    phase: 'idle' as Phase,
    items: Object.freeze([]) as readonly TaskExecution[],
    next: null as string | null,
    message: '',
    retryMore: false,
    selectedID: '',
    detail: null as TaskExecution | null,
    detailPhase: 'idle' as Phase,
    detailMessage: '',
    output: null as TaskExecutionOutput | null,
    outputPhase: 'idle' as Phase,
    outputMessage: '',
    active: 0,
  })
  let generation = 0,
    disposed = false
  const visible = computed(() => !disposed && scope.value !== null)
  const blocked = computed(() => !visible.value || busy.value || state.active !== 0)
  function reset() {
    ++generation
    Object.assign(state, {
      open: false,
      phase: 'idle',
      items: Object.freeze([]),
      next: null,
      message: '',
      retryMore: false,
      selectedID: '',
      detail: null,
      detailPhase: 'idle',
      detailMessage: '',
      active: 0,
      output: null,
      outputPhase: 'idle',
      outputMessage: '',
    })
  }
  const stop = watch(() => scope.value?.key ?? '', reset, { flush: 'sync' })
  function current(captured: TaskExecutionScope, serial: number) {
    return !disposed && serial === generation && captured.key === scope.value?.key
  }
  function inaccessible(error: unknown) {
    return (
      error instanceof AccountFailure && [401, 403, 404, 410].includes(error.problem?.status ?? 0)
    )
  }
  function clearReads() {
    state.items = Object.freeze([])
    state.next = null
    state.detail = null
    state.selectedID = ''
    state.detailPhase = 'idle'
    state.detailMessage = ''
    clearOutput()
  }
  function clearOutput() {
    state.output = null
    state.outputPhase = 'idle'
    state.outputMessage = ''
  }
  async function load(more = false) {
    const captured = scope.value
    if (!captured || blocked.value || (more && !state.next)) return false
    const cursor = more ? state.next : null,
      old = more ? state.items : Object.freeze([])
    const serial = ++generation
    state.active = serial
    state.open = true
    state.phase = 'loading'
    state.message = ''
    state.retryMore = more
    state.selectedID = ''
    state.detail = null
    state.detailPhase = 'idle'
    state.detailMessage = ''
    clearOutput()
    if (!more) {
      state.items = Object.freeze([])
      state.next = null
    }
    try {
      const result = await reads.list(captured.projectID, captured.taskID, {
        limit: 50,
        ...(cursor ? { cursor } : {}),
      })
      if (!current(captured, serial)) return false
      const previous = old.at(-1)
      if (
        result.items.some(
          (item) =>
            item.project_id !== captured.projectID ||
            item.task_id !== captured.taskID ||
            old.some((row) => row.id === item.id),
        ) ||
        (previous &&
          result.items.length &&
          compareTaskExecutions(previous, result.items[0]!) >= 0) ||
        (result.next_cursor !== null && result.next_cursor === cursor)
      )
        throw new AccountFailure('invalid-response')
      state.items = Object.freeze([...old, ...result.items])
      state.next = result.next_cursor
      state.phase = 'ready'
      state.retryMore = false
      return true
    } catch (error) {
      if (!current(captured, serial)) return false
      if (inaccessible(error)) {
        clearReads()
        state.retryMore = false
      }
      if (error instanceof AccountFailure && error.problem?.code === 'CURSOR_INVALID') {
        state.next = null
        state.retryMore = false
      }
      state.phase = 'error'
      state.message = explanation(error)
      return false
    } finally {
      if (current(captured, serial)) state.active = 0
    }
  }
  async function select(id: string) {
    const captured = scope.value
    if (!captured || blocked.value || !state.items.some((item) => item.id === id)) return false
    const serial = ++generation
    state.active = serial
    state.selectedID = id
    state.detail = null
    state.detailPhase = 'loading'
    state.detailMessage = ''
    clearOutput()
    try {
      const detail = await reads.get(captured.projectID, captured.taskID, id)
      if (!current(captured, serial)) return false
      if (
        detail.project_id !== captured.projectID ||
        detail.task_id !== captured.taskID ||
        detail.id !== id
      )
        throw new AccountFailure('invalid-response')
      state.detail = detail
      state.detailPhase = 'ready'
      return true
    } catch (error) {
      if (!current(captured, serial)) return false
      if (inaccessible(error)) {
        clearReads()
        state.phase = 'error'
        state.retryMore = false
        state.message = explanation(error)
      } else {
        state.detailPhase = 'error'
        state.detailMessage = explanation(error)
      }
      return false
    } finally {
      if (current(captured, serial)) state.active = 0
    }
  }
  async function loadOutput() {
    const captured = scope.value, detail = state.detail
    if (!captured || blocked.value || !detail || state.detailPhase !== 'ready' || state.selectedID !== detail.id) return false
    const serial = ++generation
    state.active = serial
    clearOutput()
    state.outputPhase = 'loading'
    try {
      const result = await reads.output(captured.projectID, captured.taskID, detail.id)
      if (!current(captured, serial)) return false
      if (result.execution.project_id !== captured.projectID || result.execution.task_id !== captured.taskID ||
        result.execution.id !== detail.id || BigInt(result.execution.version) < BigInt(detail.version))
        throw new AccountFailure('invalid-response')
      // The canonical summary and final projection belong to one read; publish
      // them together, never combine new output with an older detail version.
      state.detail = result.execution
      state.output = result
      state.outputPhase = 'ready'
      return true
    } catch (error) {
      if (!current(captured, serial)) return false
      if (inaccessible(error)) {
        clearReads()
        state.phase = 'error'
        state.retryMore = false
        state.message = explanation(error)
      } else {
        state.outputPhase = 'error'
        state.outputMessage = '未取得完整最终输出，请在当前请求结束后重试。'
      }
      return false
    } finally {
      if (current(captured, serial)) state.active = 0
    }
  }
  function back() {
    if (blocked.value) return false
    state.selectedID = ''
    state.detail = null
    state.detailPhase = 'idle'
    state.detailMessage = ''
    clearOutput()
    return true
  }
  function close() {
    if (blocked.value) return false
    reset()
    return true
  }
  function dispose() {
    disposed = true
    stop()
    reset()
  }
  return { state, visible, blocked, load, select, loadOutput, back, close, dispose }
}
export type TaskExecutions = ReturnType<typeof createTaskExecutions>
