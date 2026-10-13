import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import type { WriteOptions } from './account'
import {
  decodeWorkBlocker,
  decodeWorkTask,
  workID,
  workText,
  workVersion,
  type Blocker,
  type Task,
} from './work-review'

export type HumanBlockerDraft = Readonly<{
  expected_version: string
  request: Readonly<{
    type: 'waiting_for_human'
    description: string
    metadata: Readonly<Record<string, never>>
  }>
}>
export type HumanBlockerAddInput = Readonly<{
  expected_version: string
  request: HumanBlockerDraft['request'] & Readonly<{ blocker_id: string }>
}>
export type HumanBlockerResolveInput = Readonly<{
  expected_version: string
  request: Readonly<{ blocker_id: string; resolution_comment: string | null }>
}>
export type HumanBlockerCommand = 'work.task.blocker.add' | 'work.task.blocker.resolve'
export type HumanBlockerReceipt = Readonly<{
  task: Task
  blocker: Blocker
  task_event_id: string
  event_ids: readonly string[]
}>
export type HumanBlockerLookup =
  | Readonly<{ status: 'committed'; receipt: HumanBlockerReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface WorkTaskBlockersAPI {
  add(
    project: string,
    task: string,
    input: HumanBlockerAddInput,
    write: WriteOptions,
  ): Promise<HumanBlockerReceipt>
  resolve(
    project: string,
    task: string,
    input: HumanBlockerResolveInput,
    write: WriteOptions,
  ): Promise<HumanBlockerReceipt>
  lookup(
    project: string,
    task: string,
    command: HumanBlockerCommand,
    input: HumanBlockerAddInput | HumanBlockerResolveInput,
    write: WriteOptions,
  ): Promise<HumanBlockerLookup>
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
export function captureHumanBlockerDraft(value: unknown): HumanBlockerDraft {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, ['type', 'description', 'metadata'])
    requireValue(r.type === 'waiting_for_human')
    shape(r.metadata, [])
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        type: 'waiting_for_human' as const,
        description: workText(r.description, 1024),
        metadata: Object.freeze({}),
      }),
    })
  })
}
export function captureHumanBlockerAdd(value: unknown): HumanBlockerAddInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, ['blocker_id', 'type', 'description', 'metadata'])
    const draft = captureHumanBlockerDraft({
      expected_version: v.expected_version,
      request: { type: r.type, description: r.description, metadata: r.metadata },
    })
    return Object.freeze({
      ...draft,
      request: Object.freeze({ blocker_id: workID(r.blocker_id), ...draft.request }),
    })
  })
}
export function captureHumanBlockerResolve(value: unknown): HumanBlockerResolveInput {
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
  original: HumanBlockerAddInput | HumanBlockerResolveInput,
): HumanBlockerReceipt {
  const v = shape(value, ['task', 'blocker', 'task_event_id', 'event_ids'])
  const task = decodeWorkTask(v.task, project, taskID)
  const blocker = decodeWorkBlocker(v.blocker, project, taskID)
  requireValue(
    task.state === 'backlog' &&
      task.assignee_agent_id === null &&
      BigInt(task.version) === BigInt(original.expected_version) + 1n,
  )
  requireValue(blocker.id === original.request.blocker_id && blocker.type === 'waiting_for_human')
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
export function createWorkTaskBlockersAPI(fetcher?: Fetch): WorkTaskBlockersAPI {
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
    add(project, task, value, write) {
      const original = captureHumanBlockerAdd(value)
      return call('taskBlockerAdd', (v) => receipt(v, project, task, original), {
        ...options(project, task, write),
        body: original,
      })
    },
    resolve(project, task, value, write) {
      const original = captureHumanBlockerResolve(value)
      return call('taskBlockerResolve', (v) => receipt(v, project, task, original), {
        ...options(project, task, write),
        body: original,
      })
    },
    lookup(project, task, command, value, write) {
      if (!['work.task.blocker.add', 'work.task.blocker.resolve'].includes(command))
        throw new AccountFailure('invalid-input')
      const original =
        command === 'work.task.blocker.add'
          ? captureHumanBlockerAdd(value)
          : captureHumanBlockerResolve(value)
      return call(
        'taskBlockerLookup',
        (value): HumanBlockerLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: receipt(v.receipt, project, task, original),
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
