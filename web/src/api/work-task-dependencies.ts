import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  decodeWorkBlocker,
  decodeWorkTask,
  decodeWorkTaskSummary,
  taskStates,
  taskPriorities,
  type TaskSummary,
  type Page,
  workID,
  workText,
  workVersion,
  type Blocker,
  type Task,
} from './work-review'

export type TaskDependencyDraft = Readonly<{
  expected_version: string
  request: Readonly<{
    type: 'rely_on'
    description: string
    metadata: Readonly<{ related_task_id: string }>
  }>
}>
export type TaskDependencyAddInput = Readonly<{
  expected_version: string
  request: TaskDependencyDraft['request'] & Readonly<{ blocker_id: string }>
}>
export type TaskDependencyResolveInput = Readonly<{
  expected_version: string
  request: Readonly<{ blocker_id: string; resolution_comment: string | null }>
}>
export type TaskDependencyCommand = 'work.task.blocker.add' | 'work.task.blocker.resolve'
export type TaskDependencyReceipt = Readonly<{
  task: Task
  blocker: Blocker
  task_event_id: string
  event_ids: readonly string[]
}>
export type TaskDependencyLookup =
  | Readonly<{ status: 'committed'; receipt: TaskDependencyReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export type TaskDependencyCandidatesQuery = Readonly<{ limit?: 50; cursor?: string; text?: string }>
export interface WorkTaskDependenciesAPI {
  candidates(
    project: string,
    query: TaskDependencyCandidatesQuery,
    signal: AbortSignal,
  ): Promise<Page<TaskSummary>>
  add(
    project: string,
    task: string,
    input: TaskDependencyAddInput,
    write: WriteOptions,
  ): Promise<TaskDependencyReceipt>
  resolve(
    project: string,
    task: string,
    input: TaskDependencyResolveInput,
    relatedTaskID: string,
    write: WriteOptions,
  ): Promise<TaskDependencyReceipt>
  lookup(
    project: string,
    task: string,
    command: TaskDependencyCommand,
    input: TaskDependencyAddInput | TaskDependencyResolveInput,
    relatedTaskID: string,
    write: WriteOptions,
  ): Promise<TaskDependencyLookup>
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
export function captureTaskDependencyDraft(value: unknown): TaskDependencyDraft {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, ['type', 'description', 'metadata'])
    requireValue(r.type === 'rely_on')
    const metadata = shape(r.metadata, ['related_task_id'])
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        type: 'rely_on' as const,
        description: workText(r.description, 1024),
        metadata: Object.freeze({ related_task_id: workID(metadata.related_task_id) }),
      }),
    })
  })
}
export function captureTaskDependencyAdd(value: unknown): TaskDependencyAddInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, ['blocker_id', 'type', 'description', 'metadata'])
    const draft = captureTaskDependencyDraft({
      expected_version: v.expected_version,
      request: { type: r.type, description: r.description, metadata: r.metadata },
    })
    return Object.freeze({
      ...draft,
      request: Object.freeze({ blocker_id: workID(r.blocker_id), ...draft.request }),
    })
  })
}
export function captureTaskDependencyResolve(value: unknown): TaskDependencyResolveInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, ['blocker_id', 'resolution_comment'])
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        blocker_id: workID(r.blocker_id),
        resolution_comment:
          r.resolution_comment === null ? null : workText(r.resolution_comment, 1024, false, true),
      }),
    })
  })
}
function receipt(
  value: unknown,
  project: string,
  taskID: string,
  original: TaskDependencyAddInput | TaskDependencyResolveInput,
  relatedTaskID: string,
): TaskDependencyReceipt {
  const v = shape(value, ['task', 'blocker', 'task_event_id', 'event_ids'])
  const task = decodeWorkTask(v.task, project, taskID)
  const blocker = decodeWorkBlocker(v.blocker, project, taskID)
  requireValue(
    !['done', 'cancelled'].includes(task.state) &&
      BigInt(task.version) === BigInt(original.expected_version) + 1n,
  )
  requireValue(
    blocker.id === original.request.blocker_id &&
      blocker.type === 'rely_on' &&
      blocker.related_task_id === relatedTaskID &&
      relatedTaskID !== taskID,
  )
  requireValue(task.updated_at === (blocker.resolved_at ?? blocker.created_at))
  if ('description' in original.request) {
    requireValue(
      blocker.description === original.request.description && blocker.resolved_at === null,
    )
  } else {
    requireValue(
      blocker.resolved_at !== null &&
        blocker.resolution_comment === original.request.resolution_comment,
    )
  }
  requireValue(Array.isArray(v.event_ids) && v.event_ids.length === 1)
  return Object.freeze({
    task,
    blocker,
    task_event_id: workID(v.task_event_id),
    event_ids: Object.freeze(v.event_ids.map(workID)),
  })
}
export function captureTaskDependencyQuery(value: unknown): TaskDependencyCandidatesQuery {
  return input(() => {
    const q = shape(value, [], ['limit', 'cursor', 'text'])
    requireValue(q.limit === undefined || q.limit === 50)
    const text = q.text === undefined ? undefined : workText(q.text, 1024, true)
    requireValue(text === undefined || [...text].length <= 256)
    const cursor = q.cursor === undefined ? undefined : workText(q.cursor, 8192)
    requireValue(cursor === undefined || cursor.length > 0)
    return Object.freeze({
      limit: 50,
      ...(text === undefined ? {} : { text }),
      ...(cursor === undefined ? {} : { cursor }),
    })
  })
}
export function compareDependencyCandidates(a: TaskSummary, b: TaskSummary): number {
  const compare = (x: string, y: string) => (x < y ? -1 : x > y ? 1 : 0)
  return (
    compare(a.sprint_id, b.sprint_id) ||
    taskStates.indexOf(a.state) - taskStates.indexOf(b.state) ||
    taskPriorities.indexOf(a.priority) - taskPriorities.indexOf(b.priority) ||
    compare(a.manual_rank, b.manual_rank) ||
    compare(a.id, b.id)
  )
}
export function createWorkTaskDependenciesAPI(fetcher?: Fetch): WorkTaskDependenciesAPI {
  const call = accountTransport(fetcher)
  function options(project: string, task: string, write: WriteOptions) {
    const scope = input(() => ({ projectID: workID(project), target: workID(task) }))
    return {
      ...scope,
      signal: write.signal ?? new AbortController().signal,
      csrf: write.csrfToken,
      key: write.key,
    }
  }
  return {
    candidates(project, query, signal) {
      const p = input(() => workID(project)),
        q = captureTaskDependencyQuery(query)
      return call(
        'dependencyTaskCandidates',
        (value) => {
          const v = shape(value, ['items'], ['next_cursor'])
          requireValue(Array.isArray(v.items) && v.items.length <= 50)
          const items = v.items.map((row) => decodeWorkTaskSummary(row, p))
          requireValue(new Set(items.map((row) => row.id)).size === items.length)
          for (let n = 1; n < items.length; n++)
            requireValue(compareDependencyCandidates(items[n - 1]!, items[n]!) < 0)
          const next = Object.hasOwn(v, 'next_cursor') ? workText(v.next_cursor, 8192) : undefined
          requireValue(
            next === undefined || (next.length > 0 && next !== q.cursor && items.length === 50),
          )
          return Object.freeze({
            items: Object.freeze(items),
            ...(next === undefined ? {} : { next_cursor: next }),
          })
        },
        { projectID: p, signal, workQuery: { ...q, limit: '50' } },
      )
    },
    add(project, task, value, write) {
      const original = captureTaskDependencyAdd(value)
      if (original.request.metadata.related_task_id === task)
        throw new AccountFailure('invalid-input')
      return call(
        'taskBlockerAdd',
        (v) => receipt(v, project, task, original, original.request.metadata.related_task_id),
        {
          ...options(project, task, write),
          body: original,
        },
      )
    },
    resolve(project, task, value, relatedTaskID, write) {
      const related = input(() => workID(relatedTaskID))
      const original = captureTaskDependencyResolve(value)
      return call('taskBlockerResolve', (v) => receipt(v, project, task, original, related), {
        ...options(project, task, write),
        body: original,
      })
    },
    lookup(project, task, command, value, relatedTaskID, write) {
      const related = input(() => workID(relatedTaskID))
      if (!['work.task.blocker.add', 'work.task.blocker.resolve'].includes(command))
        throw new AccountFailure('invalid-input')
      const original =
        command === 'work.task.blocker.add'
          ? captureTaskDependencyAdd(value)
          : captureTaskDependencyResolve(value)
      if (
        related === task ||
        ('metadata' in original.request && related !== original.request.metadata.related_task_id)
      )
        throw new AccountFailure('invalid-input')
      return call(
        'taskBlockerLookup',
        (value): TaskDependencyLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: receipt(v.receipt, project, task, original, related),
            })
          requireValue(
            (v.status === 'not_observed' || v.status === 'in_progress') && v.receipt === null,
          )
          return Object.freeze({
            status: v.status as 'not_observed' | 'in_progress',
            receipt: null,
          })
        },
        { ...options(project, task, write), body: { command, ...original } },
      )
    },
  }
}
