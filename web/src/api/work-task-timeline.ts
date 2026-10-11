import { AccountFailure, accountTransport, shape, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import {
  taskStates,
  workID,
  workText,
  workVersion,
  type Page,
  type PageQuery,
  type Task,
  type TaskState,
} from './work-review'

export const taskTimelineTypes = [
  'task_created',
  'fields_updated',
  'blocker_added',
  'blocker_resolved',
  'state_changed',
  'assignee_changed',
  'comment',
] as const
export type TaskTimelineType = (typeof taskTimelineTypes)[number]
export type TaskTimelineQuery = PageQuery &
  Readonly<{ types?: readonly TaskTimelineType[]; order?: 'asc' | 'desc' }>
export type TaskTimelineActor =
  | Readonly<{ type: 'human'; user_id: string; source: 'task_domain' }>
  | Readonly<{ type: 'system'; service_name: 'scheduler'; source: 'scheduler'; cause_id: string }>
type Change<T> = Readonly<{ from: T; to: T }>
type Position = Readonly<{
  sprint_id: string
  state: 'backlog'
  priority: Task['priority']
  previous_id: string | null
  next_id: string | null
  order_generation: string
}>
const changedFields = ['description', 'manual_rank', 'plan', 'priority', 'title', 'type'] as const
type ChangedField = (typeof changedFields)[number]
type EventBody =
  | Readonly<{
      type: 'task_created'
      payload: Readonly<{
        initial_state: 'backlog'
        milestone_id: string
        sprint_id: string
        type: Task['type']
        priority: Task['priority']
      }>
    }>
  | Readonly<{
      type: 'fields_updated'
      payload: Readonly<{
        changed_fields: readonly ChangedField[]
        type_change: Change<Task['type']> | null
        priority_change: Change<Task['priority']> | null
        position: Position | null
      }>
    }>
  | Readonly<{
      type: 'state_changed'
      payload: Readonly<{
        from_state: TaskState
        to_state: TaskState
        reason_code:
          null | 'scheduler_claim' | 'scheduler_agent_busy_compensation' | 'scheduler_launch_failed'
      }>
    }>
  | Readonly<{
      type: 'assignee_changed'
      payload: Readonly<{ from_agent_id: string | null; to_agent_id: string }>
    }>
  | Readonly<{
      type: 'blocker_added'
      payload: Readonly<{
        blocker_id: string
        blocker_type: 'rely_on' | 'waiting_for_human' | 'technical'
        reason_code?: 'scheduler_launch_failed'
      }>
    }>
  | Readonly<{
      type: 'blocker_resolved'
      payload: Readonly<{
        blocker_id: string
        blocker_type: 'rely_on' | 'waiting_for_human' | 'technical'
        resolution_comment: string | null
      }>
    }>
  | Readonly<{ type: 'comment'; payload: Readonly<{ body: string }> }>
export type TaskTimelineEvent = Readonly<{
  id: string
  project_id: string
  task_id: string
  task_version: string
  actor: TaskTimelineActor
  operation_id: string
  correlation_id: string
  created_at: string
}> &
  EventBody
export interface WorkTaskTimelineAPI {
  list(
    project: string,
    task: string,
    query: TaskTimelineQuery,
    signal: AbortSignal,
  ): Promise<Page<TaskTimelineEvent>>
}
const priorities = ['critical', 'high', 'medium', 'low'] as const
const taskTypes = ['feature', 'bug', 'task', 'spike', 'chore'] as const
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
function choice<T extends string>(value: unknown, choices: readonly T[]): T {
  requireValue(typeof value === 'string' && choices.includes(value as T))
  return value as T
}
function nullableID(value: unknown) {
  return value === null ? null : workID(value)
}
function change<T extends string>(value: unknown, choices: readonly T[]): Change<T> | null {
  if (value === null) return null
  const v = shape(value, ['from', 'to'])
  const from = choice(v.from, choices),
    to = choice(v.to, choices)
  requireValue(from !== to)
  return Object.freeze({ from, to })
}
function position(value: unknown, task: string): Position | null {
  if (value === null) return null
  const v = shape(value, [
    'sprint_id',
    'state',
    'priority',
    'previous_id',
    'next_id',
    'order_generation',
  ])
  const result = {
    sprint_id: workID(v.sprint_id),
    state: choice(v.state, ['backlog'] as const),
    priority: choice(v.priority, priorities),
    previous_id: nullableID(v.previous_id),
    next_id: nullableID(v.next_id),
    order_generation: workVersion(v.order_generation),
  }
  requireValue(
    result.previous_id !== task &&
      result.next_id !== task &&
      (!result.previous_id || result.previous_id !== result.next_id),
  )
  return Object.freeze(result)
}
function actor(value: unknown, operation: string): TaskTimelineActor {
  requireValue(value && typeof value === 'object' && !Array.isArray(value))
  if ((value as Record<string, unknown>).type === 'human') {
    const v = shape(value, ['type', 'user_id', 'source'])
    requireValue(v.source === 'task_domain')
    return Object.freeze({ type: 'human', user_id: workID(v.user_id), source: 'task_domain' })
  }
  const v = shape(value, ['type', 'service_name', 'source', 'cause_id'])
  requireValue(
    v.type === 'system' &&
      v.service_name === 'scheduler' &&
      v.source === 'scheduler' &&
      workID(v.cause_id) === operation,
  )
  return Object.freeze({
    type: 'system',
    service_name: 'scheduler',
    source: 'scheduler',
    cause_id: operation,
  })
}
function body(
  type: TaskTimelineType,
  value: unknown,
  who: TaskTimelineActor,
  task: string,
): EventBody {
  const human = who.type === 'human'
  switch (type) {
    case 'task_created': {
      requireValue(human)
      const p = shape(value, ['initial_state', 'milestone_id', 'sprint_id', 'type', 'priority'])
      return {
        type,
        payload: Object.freeze({
          initial_state: choice(p.initial_state, ['backlog'] as const),
          milestone_id: workID(p.milestone_id),
          sprint_id: workID(p.sprint_id),
          type: choice(p.type, taskTypes),
          priority: choice(p.priority, priorities),
        }),
      }
    }
    case 'fields_updated': {
      requireValue(human)
      const p = shape(value, ['changed_fields', 'type_change', 'priority_change', 'position'])
      requireValue(
        Array.isArray(p.changed_fields) &&
          p.changed_fields.length > 0 &&
          p.changed_fields.length <= changedFields.length,
      )
      const fields = p.changed_fields.map((v) => choice(v, changedFields))
      requireValue(fields.every((v, n) => !n || fields[n - 1]! < v))
      const t = change(p.type_change, taskTypes),
        priority = change(p.priority_change, priorities),
        pos = position(p.position, task)
      requireValue(
        fields.includes('type') === (t !== null) &&
          fields.includes('priority') === (priority !== null),
      )
      if (fields.includes('manual_rank')) requireValue(fields.length === 1 && pos !== null)
      else if (priority)
        requireValue(pos !== null && pos.priority === priority.to && pos.next_id === null)
      else requireValue(pos === null)
      return {
        type,
        payload: Object.freeze({
          changed_fields: Object.freeze(fields),
          type_change: t,
          priority_change: priority,
          position: pos,
        }),
      }
    }
    case 'state_changed': {
      const p = shape(value, ['from_state', 'to_state', 'reason_code'])
      const from = choice(p.from_state, taskStates),
        to = choice(p.to_state, taskStates)
      let reason: Extract<EventBody, { type: 'state_changed' }>['payload']['reason_code'] = null
      if (human) {
        requireValue(p.reason_code === null)
        const edges: Partial<Record<TaskState, readonly TaskState[]>> = {
          backlog: ['todo', 'cancelled'],
          todo: ['blocked', 'cancelled'],
          in_progress: ['in_review', 'blocked', 'cancelled'],
          in_review: ['done', 'todo', 'blocked', 'cancelled'],
          blocked: ['todo', 'cancelled'],
        }
        requireValue(edges[from]?.includes(to))
      } else {
        reason = choice(p.reason_code, [
          'scheduler_claim',
          'scheduler_agent_busy_compensation',
          'scheduler_launch_failed',
        ] as const)
        requireValue(
          reason === 'scheduler_claim'
            ? from === 'todo' && to === 'in_progress'
            : reason === 'scheduler_agent_busy_compensation'
              ? from === 'in_progress' && to === 'todo'
              : ['in_progress', 'in_review'].includes(from) && to === 'blocked',
        )
      }
      return {
        type,
        payload: Object.freeze({ from_state: from, to_state: to, reason_code: reason }),
      }
    }
    case 'assignee_changed': {
      requireValue(human)
      const p = shape(value, ['from_agent_id', 'to_agent_id'])
      const from = nullableID(p.from_agent_id),
        to = workID(p.to_agent_id)
      requireValue(from !== to)
      return { type, payload: Object.freeze({ from_agent_id: from, to_agent_id: to }) }
    }
    case 'blocker_added': {
      const p = shape(
        value,
        human ? ['blocker_id', 'blocker_type'] : ['blocker_id', 'blocker_type', 'reason_code'],
      )
      if (!human) requireValue(p.reason_code === 'scheduler_launch_failed')
      return {
        type,
        payload: Object.freeze({
          blocker_id: workID(p.blocker_id),
          blocker_type: choice(
            p.blocker_type,
            human ? (['rely_on', 'waiting_for_human'] as const) : (['technical'] as const),
          ),
          ...(!human ? { reason_code: 'scheduler_launch_failed' as const } : {}),
        }),
      }
    }
    case 'blocker_resolved': {
      requireValue(human)
      const p = shape(value, ['blocker_id', 'blocker_type', 'resolution_comment'])
      const kind = choice(p.blocker_type, ['rely_on', 'waiting_for_human', 'technical'] as const)
      const comment =
        p.resolution_comment === null ? null : workText(p.resolution_comment, 1024, false, true)
      requireValue(kind !== 'technical' || comment === null)
      return {
        type,
        payload: Object.freeze({
          blocker_id: workID(p.blocker_id),
          blocker_type: kind,
          resolution_comment: comment,
        }),
      }
    }
    case 'comment': {
      requireValue(human)
      const p = shape(value, ['body'])
      return { type, payload: Object.freeze({ body: workText(p.body, 32768, false, true) }) }
    }
  }
}
// Match the server's JSON representation budget, including its HTML escaping.
function encodedBytes(value: unknown): number {
  return new TextEncoder().encode(
    JSON.stringify(value).replace(
      /[<>&\u2028\u2029]/g,
      (v) => `\\u${v.charCodeAt(0).toString(16).padStart(4, '0')}`,
    ),
  ).byteLength
}
function event(value: unknown, project: string, task: string): TaskTimelineEvent {
  const v = shape(value, [
    'id',
    'project_id',
    'task_id',
    'task_version',
    'type',
    'actor',
    'operation_id',
    'correlation_id',
    'payload',
    'created_at',
  ])
  const kind = choice(v.type, taskTimelineTypes),
    operation = workID(v.operation_id),
    who = actor(v.actor, operation)
  const version = workVersion(v.task_version)
  requireValue(
    workID(v.project_id) === project &&
      workID(v.task_id) === task &&
      workID(v.correlation_id) === operation &&
      (kind === 'task_created' ? version === '1' : BigInt(version) > 1n),
  )
  const result: TaskTimelineEvent = Object.freeze({
    id: workID(v.id),
    project_id: project,
    task_id: task,
    task_version: version,
    actor: who,
    operation_id: operation,
    correlation_id: operation,
    created_at: parseSystemInstant(v.created_at),
    ...body(kind, v.payload, who, task),
  })
  requireValue(encodedBytes(result) <= (kind === 'comment' ? 272 * 1024 : 16 * 1024))
  return result
}
export function compareTaskTimelineEvents(a: TaskTimelineEvent, b: TaskTimelineEvent): number {
  return a.created_at < b.created_at
    ? -1
    : a.created_at > b.created_at
      ? 1
      : a.id < b.id
        ? -1
        : a.id > b.id
          ? 1
          : 0
}
export function createWorkTaskTimelineAPI(fetcher?: Fetch): WorkTaskTimelineAPI {
  const call = accountTransport(fetcher)
  return {
    list(project, task, q, signal) {
      let query: Record<string, string>,
        types: readonly TaskTimelineType[] | undefined,
        order: 'asc' | 'desc'
      try {
        workID(project)
        workID(task)
        const v = shape(q, [], ['limit', 'cursor', 'types', 'order']),
          limit = Object.hasOwn(v, 'limit') ? v.limit : 50
        requireValue(
          typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 200,
        )
        order = Object.hasOwn(v, 'order') ? choice(v.order, ['asc', 'desc'] as const) : 'desc'
        if (Object.hasOwn(v, 'types')) {
          requireValue(
            Array.isArray(v.types) &&
              v.types.length > 0 &&
              v.types.length <= taskTimelineTypes.length,
          )
          types = v.types.map((t) => choice(t, taskTimelineTypes))
          requireValue(new Set(types).size === types.length)
        }
        query = {
          limit: String(limit),
          order,
          ...(Object.hasOwn(v, 'cursor') ? { cursor: workText(v.cursor, 8192, false, true) } : {}),
          ...(types ? { types: [...types].sort().join(',') } : {}),
        }
      } catch {
        throw new AccountFailure('invalid-input')
      }
      return call(
        'taskTimeline',
        (value) => {
          const v = shape(value, ['items'], ['next_cursor'])
          requireValue(Array.isArray(v.items) && v.items.length <= Number(query.limit))
          const items = v.items.map((raw) => event(raw, project, task)),
            seen = new Set<string>()
          for (let n = 0; n < items.length; n++) {
            const item = items[n]!
            requireValue(!seen.has(item.id) && (!types || types.includes(item.type)))
            seen.add(item.id)
            if (n)
              requireValue(
                order === 'desc'
                  ? compareTaskTimelineEvents(items[n - 1]!, item) > 0
                  : compareTaskTimelineEvents(items[n - 1]!, item) < 0,
              )
          }
          requireValue(encodedBytes(items) <= 4 * 1024 * 1024 - 16 * 1024)
          const next = Object.hasOwn(v, 'next_cursor')
            ? workText(v.next_cursor, 8192, false, true)
            : undefined
          requireValue(!next || (items.length > 0 && next !== query.cursor))
          return Object.freeze({
            items: Object.freeze(items),
            ...(next ? { next_cursor: next } : {}),
          })
        },
        { projectID: project, target: task, signal, workQuery: query },
      )
    },
  }
}
