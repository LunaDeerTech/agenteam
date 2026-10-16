import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { decodeWorkTask, workID, workVersion, type Task } from './work-review'
import type { WriteOptions } from './account'

export type TaskReorderPosition = Readonly<{ before_id?: string }>
export type TaskReorderInput = Readonly<{
  expected_version: string
  request: TaskReorderPosition
}>
export type TaskReorderReceipt =
  | Readonly<{
      task: Task
      changed: true
      task_event_id: string
      event_ids: readonly string[]
    }>
  | Readonly<{
      task: Task
      changed: false
      task_event_id: null
      event_ids: readonly string[]
    }>
export type TaskReorderLookup =
  | Readonly<{ status: 'committed'; receipt: TaskReorderReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface WorkTaskReorderAPI {
  reorder(
    project: string,
    task: string,
    input: TaskReorderInput,
    write: WriteOptions,
  ): Promise<TaskReorderReceipt>
  lookup(
    project: string,
    task: string,
    input: TaskReorderInput,
    write: WriteOptions,
  ): Promise<TaskReorderLookup>
}

function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function input<T>(fn: () => T): T {
  try {
    return fn()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}

// An omitted anchor means the group tail. Null and caller-supplied ranks are invalid.
export function captureTaskReorderInput(value: unknown, target?: string): TaskReorderInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, [], ['before_id'])
    const before = Object.hasOwn(r, 'before_id') ? workID(r.before_id) : undefined
    requireValue(before === undefined || before !== target)
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze(before === undefined ? {} : { before_id: before }),
    })
  })
}

function receipt(
  value: unknown,
  project: string,
  target: string,
  original: TaskReorderInput,
): TaskReorderReceipt {
  const v = shape(value, ['task', 'changed', 'task_event_id', 'event_ids'])
  const task = decodeWorkTask(v.task, project, target)
  requireValue(v.changed === true || v.changed === false)
  requireValue(task.state === 'backlog' && task.assignee_agent_id === null)
  requireValue(BigInt(task.version) === BigInt(original.expected_version) + (v.changed ? 1n : 0n))
  requireValue(Array.isArray(v.event_ids))
  if (v.changed) {
    requireValue(v.event_ids.length === 1)
    return Object.freeze({
      task,
      changed: true,
      task_event_id: workID(v.task_event_id),
      event_ids: Object.freeze(v.event_ids.map(workID)),
    })
  }
  requireValue(v.task_event_id === null && v.event_ids.length === 0)
  return Object.freeze({
    task,
    changed: false,
    task_event_id: null,
    event_ids: Object.freeze([]),
  })
}

export function createWorkTaskReorderAPI(fetcher?: Fetch): WorkTaskReorderAPI {
  const call = accountTransport(fetcher)
  return {
    reorder(project, task, value, write) {
      const original = captureTaskReorderInput(value, task)
      const scope = input(() => ({ projectID: workID(project), target: workID(task) }))
      return call('taskReorder', (v) => receipt(v, project, task, original), {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookup(project, task, value, write) {
      const original = captureTaskReorderInput(value, task)
      const scope = input(() => ({ projectID: workID(project), target: workID(task) }))
      return call(
        'taskReorderLookup',
        (v): TaskReorderLookup => {
          const r = shape(v, ['status', 'receipt'])
          if (r.status === 'committed') {
            return Object.freeze({
              status: 'committed',
              receipt: receipt(r.receipt, project, task, original),
            })
          }
          requireValue(
            (r.status === 'in_progress' || r.status === 'not_observed') && r.receipt === null,
          )
          return Object.freeze({
            status: r.status as 'in_progress' | 'not_observed',
            receipt: null,
          })
        },
        {
          projectID: scope.projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.task.reorder', target_id: scope.target, ...original },
        },
      )
    },
  }
}
