import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { decodeWorkTask, workID, workText, type Task } from './work-review'
import type { WriteOptions } from './account'

export type TaskCreateDraft = Readonly<{
  sprint_id: string
  title: string
  description: string
  type: 'feature' | 'bug' | 'task' | 'spike' | 'chore'
  priority: 'critical' | 'high' | 'medium' | 'low'
  plan: string
}>
export type TaskCreateInput = Readonly<{
  request: TaskCreateDraft & Readonly<{ task_id: string }>
}>
export type TaskCreateReceipt = Readonly<{
  task: Task
  changed: true
  task_event_id: string
  event_ids: readonly string[]
}>
export type TaskCreateLookup =
  | Readonly<{ status: 'committed'; receipt: TaskCreateReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface WorkTaskPlanningAPI {
  create(project: string, input: TaskCreateInput, write: WriteOptions): Promise<TaskCreateReceipt>
  lookup(project: string, input: TaskCreateInput, write: WriteOptions): Promise<TaskCreateLookup>
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
function draft(value: unknown): TaskCreateDraft {
  const v = shape(value, ['sprint_id', 'title', 'description', 'type', 'priority', 'plan'])
  const title = workText(v.title, 1024, true)
  requireValue(['feature', 'bug', 'task', 'spike', 'chore'].includes(v.type as string))
  requireValue(['critical', 'high', 'medium', 'low'].includes(v.priority as string))
  return Object.freeze({
    sprint_id: workID(v.sprint_id),
    title,
    description: workText(v.description, 32768),
    type: v.type as TaskCreateDraft['type'],
    priority: v.priority as TaskCreateDraft['priority'],
    plan: workText(v.plan, 32768),
  })
}
export function captureTaskCreateDraft(value: unknown): TaskCreateDraft {
  return input(() => draft(value))
}
export function captureTaskCreateInput(value: unknown): TaskCreateInput {
  return input(() => {
    const v = shape(value, ['request']),
      r = shape(v.request, [
        'task_id',
        'sprint_id',
        'title',
        'description',
        'type',
        'priority',
        'plan',
      ]),
      { task_id, ...rest } = r
    return Object.freeze({ request: Object.freeze({ task_id: workID(task_id), ...draft(rest) }) })
  })
}
// Resource IDs are UUIDv7. Command keys remain owned separately by Session.
export function newTaskID(): string {
  return input(() => {
    const now = Date.now()
    requireValue(Number.isSafeInteger(now) && now >= 0 && now <= 0xffffffffffff)
    const bytes = new Uint8Array(16)
    globalThis.crypto.getRandomValues(bytes)
    let timestamp = BigInt(now)
    for (let n = 5; n >= 0; n--) {
      bytes[n] = Number(timestamp & 255n)
      timestamp >>= 8n
    }
    bytes[6] = (bytes[6]! & 15) | 0x70
    bytes[8] = (bytes[8]! & 63) | 0x80
    const hex = Array.from(bytes, (v) => v.toString(16).padStart(2, '0')).join('')
    return workID(
      `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`,
    )
  })
}
function receipt(value: unknown, project: string, original: TaskCreateInput): TaskCreateReceipt {
  const v = shape(value, ['task', 'changed', 'task_event_id', 'event_ids']),
    r = original.request,
    task = decodeWorkTask(v.task, project, r.task_id)
  requireValue(
    v.changed === true &&
      task.version === '1' &&
      task.state === 'backlog' &&
      task.assignee_agent_id === null,
  )
  requireValue(
    task.sprint_id === r.sprint_id &&
      task.title === r.title &&
      task.description === r.description &&
      task.type === r.type &&
      task.priority === r.priority &&
      task.plan === r.plan,
  )
  requireValue(Array.isArray(v.event_ids) && v.event_ids.length === 1)
  return Object.freeze({
    task,
    changed: true,
    task_event_id: workID(v.task_event_id),
    event_ids: Object.freeze(v.event_ids.map(workID)),
  })
}
export function createWorkTaskPlanningAPI(fetcher?: Fetch): WorkTaskPlanningAPI {
  const call = accountTransport(fetcher)
  return {
    create(project, value, write) {
      const original = captureTaskCreateInput(value)
      return call('taskCreate', (v) => receipt(v, project, original), {
        projectID: input(() => workID(project)),
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookup(project, value, write) {
      const original = captureTaskCreateInput(value)
      return call(
        'taskCreateLookup',
        (v): TaskCreateLookup => {
          const r = shape(v, ['status', 'receipt'])
          if (r.status === 'committed')
            return { status: 'committed', receipt: receipt(r.receipt, project, original) }
          requireValue(
            (r.status === 'in_progress' || r.status === 'not_observed') && r.receipt === null,
          )
          return { status: r.status as 'in_progress' | 'not_observed', receipt: null }
        },
        {
          projectID: input(() => workID(project)),
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.task.create', ...original },
        },
      )
    },
  }
}
