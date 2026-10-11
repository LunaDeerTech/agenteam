import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { decodeWorkTask, workID, workText, workVersion, type Task } from './work-review'
import type { WriteOptions } from './account'

export type TaskUpdateFields = Readonly<{
  title?: string
  description?: string
  type?: Task['type']
  priority?: Task['priority']
  plan?: string
}>
export type TaskUpdateInput = Readonly<{
  expected_version: string
  request: TaskUpdateFields
}>
export type TaskUpdateReceipt =
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
export type TaskUpdateLookup =
  | Readonly<{ status: 'committed'; receipt: TaskUpdateReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface WorkTaskEditAPI {
  update(
    project: string,
    task: string,
    input: TaskUpdateInput,
    write: WriteOptions,
  ): Promise<TaskUpdateReceipt>
  lookup(
    project: string,
    task: string,
    input: TaskUpdateInput,
    write: WriteOptions,
  ): Promise<TaskUpdateLookup>
}

const fields = ['title', 'description', 'type', 'priority', 'plan'] as const
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
function choice<T extends string>(value: unknown, choices: readonly T[]): T {
  requireValue(typeof value === 'string' && choices.includes(value as T))
  return value as T
}

// Field presence is part of the original command identity. Capture does not
// fill omitted values from a current Task or normalize explicit empty text.
export function captureTaskUpdateInput(value: unknown): TaskUpdateInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, [], [...fields])
    requireValue(Object.keys(r).length > 0)
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        ...(Object.hasOwn(r, 'title') ? { title: workText(r.title, 1024, true) } : {}),
        ...(Object.hasOwn(r, 'description') ? { description: workText(r.description, 32768) } : {}),
        ...(Object.hasOwn(r, 'type')
          ? { type: choice(r.type, ['feature', 'bug', 'task', 'spike', 'chore'] as const) }
          : {}),
        ...(Object.hasOwn(r, 'priority')
          ? { priority: choice(r.priority, ['critical', 'high', 'medium', 'low'] as const) }
          : {}),
        ...(Object.hasOwn(r, 'plan') ? { plan: workText(r.plan, 32768) } : {}),
      }),
    })
  })
}

function receipt(
  value: unknown,
  project: string,
  target: string,
  original: TaskUpdateInput,
): TaskUpdateReceipt {
  const v = shape(value, ['task', 'changed', 'task_event_id', 'event_ids'])
  const task = decodeWorkTask(v.task, project, target)
  requireValue(v.changed === true || v.changed === false)
  requireValue(task.state === 'backlog' && task.assignee_agent_id === null)
  requireValue(BigInt(task.version) === BigInt(original.expected_version) + (v.changed ? 1n : 0n))
  for (const key of fields) {
    if (Object.hasOwn(original.request, key)) requireValue(task[key] === original.request[key])
  }
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

export function createWorkTaskEditAPI(fetcher?: Fetch): WorkTaskEditAPI {
  const call = accountTransport(fetcher)
  return {
    update(project, task, value, write) {
      const original = captureTaskUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(task) }))
      return call('taskUpdate', (v) => receipt(v, project, task, original), {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookup(project, task, value, write) {
      const original = captureTaskUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(task) }))
      return call(
        'taskUpdateLookup',
        (v): TaskUpdateLookup => {
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
          body: { command: 'work.task.update', target_id: scope.target, ...original },
        },
      )
    },
  }
}
