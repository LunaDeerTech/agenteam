import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import { workID, workVersion } from './work-review'

export const executionStatuses = [
  'created',
  'preparing',
  'running',
  'waiting',
  'succeeded',
  'failed',
  'cancelled',
] as const
export const executionReasons = [
  'completed',
  'model_failed',
  'incomplete_response',
  'runtime_failed',
  'cancelled',
] as const
export type TaskExecution = Readonly<{
  id: string
  project_id: string
  task_id: string
  agent_id: string
  purpose: 'task/work' | 'task/review'
  status: (typeof executionStatuses)[number]
  version: string
  created_at: string
  started_at: string | null
  completed_at: string | null
  cancel_requested_at: string | null
  snapshot_id: string | null
  reason: (typeof executionReasons)[number] | null
}>
export type TaskExecutionQuery = Readonly<{ limit?: number; cursor?: string }>
export type TaskExecutionPage = Readonly<{
  items: readonly TaskExecution[]
  next_cursor: string | null
}>
export interface TaskExecutionsAPI {
  list(
    project: string,
    task: string,
    query: TaskExecutionQuery,
    signal: AbortSignal,
  ): Promise<TaskExecutionPage>
  get(project: string, task: string, execution: string, signal: AbortSignal): Promise<TaskExecution>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function choice<T extends string>(value: unknown, values: readonly T[]): T {
  requireValue(typeof value === 'string' && values.includes(value as T))
  return value as T
}
function cursor(value: unknown): string {
  const result = string(value, 1, 8192)
  requireValue(
    new TextEncoder().encode(result).byteLength <= 8192 &&
      !/[\u0000-\u001f\u007f-\u009f]/u.test(result) &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result),
  )
  return result
}
export function captureTaskExecutionQuery(
  value: unknown,
): Readonly<{ limit: number; cursor?: string }> {
  try {
    const q = shape(value, [], ['limit', 'cursor']),
      limit = Object.hasOwn(q, 'limit') ? q.limit : 50
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 100)
    return Object.freeze({
      limit,
      ...(Object.hasOwn(q, 'cursor') ? { cursor: cursor(q.cursor) } : {}),
    })
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function inputID(value: unknown): string {
  try {
    return workID(value)
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
export function compareTaskExecutions(a: TaskExecution, b: TaskExecution): number {
  return a.created_at > b.created_at
    ? -1
    : a.created_at < b.created_at
      ? 1
      : a.id > b.id
        ? -1
        : a.id < b.id
          ? 1
          : 0
}
export function parseTaskExecution(
  value: unknown,
  project: string,
  task: string,
  execution?: string,
): TaskExecution {
  const v = shape(value, [
    'id',
    'project_id',
    'task_id',
    'agent_id',
    'purpose',
    'status',
    'version',
    'created_at',
    'started_at',
    'completed_at',
    'cancel_requested_at',
    'snapshot_id',
    'reason',
  ])
  const nullableTime = (value: unknown) => (value === null ? null : parseSystemInstant(value))
  const result: TaskExecution = {
    id: workID(v.id),
    project_id: workID(v.project_id),
    task_id: workID(v.task_id),
    agent_id: workID(v.agent_id),
    purpose: choice(v.purpose, ['task/work', 'task/review']),
    status: choice(v.status, executionStatuses),
    version: workVersion(v.version),
    created_at: parseSystemInstant(v.created_at),
    started_at: nullableTime(v.started_at),
    completed_at: nullableTime(v.completed_at),
    cancel_requested_at: nullableTime(v.cancel_requested_at),
    snapshot_id: v.snapshot_id === null ? null : workID(v.snapshot_id),
    reason: v.reason === null ? null : choice(v.reason, executionReasons),
  }
  const terminal = ['succeeded', 'failed', 'cancelled'].includes(result.status)
  requireValue(
    result.project_id === project &&
      result.task_id === task &&
      (execution === undefined || result.id === execution) &&
      terminal === (result.completed_at !== null) &&
      [result.started_at, result.completed_at, result.cancel_requested_at].every(
        (at) => at === null || at >= result.created_at,
      ) &&
      (result.started_at === null ||
        result.completed_at === null ||
        result.completed_at >= result.started_at) &&
      (result.reason === null ||
        (result.status === 'succeeded'
          ? result.reason === 'completed'
          : result.status === 'cancelled'
            ? result.reason === 'cancelled'
            : result.status === 'failed' &&
              ['model_failed', 'incomplete_response', 'runtime_failed'].includes(result.reason))),
  )
  return Object.freeze(result)
}
export function createTaskExecutionsAPI(fetcher?: Fetch): TaskExecutionsAPI {
  const request = accountTransport(fetcher)
  return {
    async list(projectID, taskID, query, signal) {
      const project = inputID(projectID),
        task = inputID(taskID),
        q = captureTaskExecutionQuery(query)
      return request(
        'listTaskExecutions',
        (value) => {
          const page = shape(value, ['items', 'next_cursor'])
          requireValue(Array.isArray(page.items) && page.items.length <= q.limit)
          const items = page.items.map((item) => parseTaskExecution(item, project, task))
          requireValue(
            new Set(items.map((item) => item.id)).size === items.length &&
              items.every(
                (item, index) => index === 0 || compareTaskExecutions(items[index - 1]!, item) < 0,
              ),
          )
          const next = page.next_cursor === null ? null : cursor(page.next_cursor)
          requireValue(next === null || (items.length === q.limit && next !== q.cursor))
          return Object.freeze({ items: Object.freeze(items), next_cursor: next })
        },
        {
          signal,
          projectID: project,
          target: task,
          workQuery: {
            limit: String(q.limit),
            ...(q.cursor === undefined ? {} : { cursor: q.cursor }),
          },
        },
      )
    },
    async get(projectID, taskID, executionID, signal) {
      const project = inputID(projectID),
        task = inputID(taskID),
        execution = inputID(executionID)
      return request(
        'getTaskExecution',
        (value) =>
          parseTaskExecution(shape(value, ['execution']).execution, project, task, execution),
        { signal, projectID: project, target: task, executionID: execution },
      )
    },
  }
}
