import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'
import type { WriteOptions } from './account'

export const taskStates = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'blocked',
  'done',
  'cancelled',
] as const
export type TaskState = (typeof taskStates)[number]
export const taskLabels: Record<TaskState, string> = {
  backlog: '待规划',
  todo: '待执行',
  in_progress: '进行中',
  in_review: '评审中',
  blocked: '受阻',
  done: '已完成',
  cancelled: '已取消',
}
const priorities = ['critical', 'high', 'medium', 'low'] as const
const types = ['feature', 'bug', 'task', 'spike', 'chore'] as const
export type MilestoneSummary = Readonly<{
  id: string
  project_id: string
  title: string
  manual_rank: string
  version: string
  created_at: string
  updated_at: string
}>
export type Milestone = MilestoneSummary & Readonly<{ description: string }>
export type SprintSummary = MilestoneSummary &
  Readonly<{ milestone_id: string; state: 'planned' | 'current' | 'completed' }>
export type Sprint = SprintSummary &
  Readonly<{ description: string; started_at: string | null; completed_at: string | null }>
export type TaskSummary = MilestoneSummary &
  Readonly<{
    milestone_id: string
    sprint_id: string
    type: (typeof types)[number]
    priority: (typeof priorities)[number]
    state: TaskState
    assignee_agent_id: string | null
  }>
export type Task = TaskSummary & Readonly<{ description: string; plan: string }>
export type Blocker = Readonly<{
  id: string
  project_id: string
  task_id: string
  type: 'rely_on' | 'waiting_for_human' | 'technical'
  description: string
  related_task_id: string | null
  created_at: string
  resolved_at: string | null
  resolution_comment: string | null
}>
export type Page<T> = Readonly<{ items: readonly T[]; next_cursor?: string }>
export type PageQuery = Readonly<{ limit?: number; cursor?: string }>
export type TaskQuery = PageQuery & Readonly<{ sprint_id: string; state: TaskState }>
export type ReviewInput = Readonly<{
  expected_version: string
  request: Readonly<{
    target_state: 'in_review' | 'done' | 'todo'
    comment: string
    assignee_agent_id?: string
  }>
}>
export type ReadyInput = Readonly<{
  expected_version: string
  request: Readonly<{
    target_state: 'todo'
    assignee_agent_id: string
    comment?: string
  }>
}>
export type ReviewReceipt = Readonly<{
  task: Task
  task_event_ids: readonly string[]
  event_ids: readonly string[]
}>
export type ReviewLookup =
  | Readonly<{ status: 'committed'; receipt: ReviewReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export interface WorkReviewAPI {
  milestones(
    project: string,
    query: PageQuery,
    signal: AbortSignal,
  ): Promise<Page<MilestoneSummary>>
  milestone(project: string, target: string, signal: AbortSignal): Promise<Milestone>
  sprints(
    project: string,
    milestone: string,
    query: PageQuery,
    signal: AbortSignal,
  ): Promise<Page<SprintSummary>>
  sprint(project: string, target: string, signal: AbortSignal): Promise<Sprint>
  tasks(project: string, query: TaskQuery, signal: AbortSignal): Promise<Page<TaskSummary>>
  task(project: string, target: string, signal: AbortSignal): Promise<Task>
  blockers(
    project: string,
    task: string,
    query: PageQuery,
    signal: AbortSignal,
  ): Promise<Page<Blocker>>
  transfer(
    project: string,
    task: string,
    input: ReviewInput,
    write: WriteOptions,
  ): Promise<ReviewReceipt>
  lookup(
    project: string,
    task: string,
    input: ReviewInput,
    write: WriteOptions,
  ): Promise<ReviewLookup>
  ready(
    project: string,
    task: string,
    input: ReadyInput,
    write: WriteOptions,
  ): Promise<ReviewReceipt>
  lookupReady(
    project: string,
    task: string,
    input: ReadyInput,
    write: WriteOptions,
  ): Promise<ReviewLookup>
}
function requireValue(value: unknown): asserts value {
  if (!value) throw new AccountFailure('invalid-response')
}
export function workID(value: unknown): string {
  const v = string(value, 36, 36)
  requireValue(uuid7.test(v))
  return v
}
export function workVersion(value: unknown): string {
  const v = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(v) && BigInt(v) <= 9223372036854775807n)
  return v
}
export function workText(value: unknown, max: number, title = false, nonblank = false): string {
  const v = string(value, 0, max)
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v) &&
      new TextEncoder().encode(v).byteLength <= max,
  )
  requireValue(
    !(
      title
        ? /[\u0000-\u001f\u007f-\u009f]/u
        : /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/u
    ).test(v),
  )
  if (title || nonblank) requireValue(!/^\p{White_Space}*$/u.test(v))
  if (title) requireValue([...v].length <= 256)
  return v
}
function enumeration<T extends string>(value: unknown, choices: readonly T[]): T {
  requireValue(typeof value === 'string' && choices.includes(value as T))
  return value as T
}
function input<T>(fn: () => T): T {
  try {
    return fn()
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function freeze<T>(value: T): T {
  if (value && typeof value === 'object') {
    for (const item of Object.values(value)) freeze(item)
    Object.freeze(value)
  }
  return value
}
const keys = ['id', 'project_id', 'title', 'manual_rank', 'version', 'created_at', 'updated_at']
const taskKeys = [
  ...keys,
  'milestone_id',
  'sprint_id',
  'type',
  'priority',
  'state',
  'assignee_agent_id',
]
function base(v: Record<string, unknown>, project: string, target?: string): MilestoneSummary {
  const result = {
    id: workID(v.id),
    project_id: workID(v.project_id),
    title: workText(v.title, 1024, true),
    manual_rank: string(v.manual_rank, 32, 32),
    version: workVersion(v.version),
    created_at: parseSystemInstant(v.created_at),
    updated_at: parseSystemInstant(v.updated_at),
  }
  requireValue(
    result.project_id === project &&
      (!target || result.id === target) &&
      result.created_at <= result.updated_at &&
      /^[0-9a-f]{32}$/.test(result.manual_rank) &&
      !['0'.repeat(32), 'f'.repeat(32)].includes(result.manual_rank),
  )
  return result
}
function parseTask(
  value: unknown,
  project: string,
  target?: string,
  summary = false,
): Task | TaskSummary {
  const v = shape(value, summary ? taskKeys : [...taskKeys, 'description', 'plan'])
  const result: TaskSummary = {
    ...base(v, project, target),
    milestone_id: workID(v.milestone_id),
    sprint_id: workID(v.sprint_id),
    type: enumeration(v.type, types),
    priority: enumeration(v.priority, priorities),
    state: enumeration(v.state, taskStates),
    assignee_agent_id: v.assignee_agent_id === null ? null : workID(v.assignee_agent_id),
  }
  requireValue(
    !['todo', 'in_progress', 'in_review'].includes(result.state) ||
      result.assignee_agent_id !== null,
  )
  return freeze(
    summary
      ? result
      : { ...result, description: workText(v.description, 32768), plan: workText(v.plan, 32768) },
  )
}
function human(value: unknown) {
  const v = shape(value, ['type', 'user_id', 'source'])
  requireValue(v.type === 'human' && v.source === 'task_domain')
  workID(v.user_id)
}
function historyActor(value: unknown, project: string) {
  const kind = shape(
    value,
    ['kind'],
    ['user_id', 'project_id', 'agent_id', 'execution_id', 'service_name', 'cause_ref'],
  ).kind
  if (kind === 'human') {
    const v = shape(value, ['kind', 'user_id'])
    workID(v.user_id)
    return
  }
  if (kind === 'agent_run') {
    const v = shape(value, ['kind', 'project_id', 'agent_id', 'execution_id'])
    requireValue(workID(v.project_id) === project)
    workID(v.agent_id)
    workID(v.execution_id)
    return
  }
  const v = shape(value, ['kind', 'project_id', 'service_name', 'cause_ref'])
  requireValue(kind === 'service' && (v.project_id === null || workID(v.project_id) === project))
  workText(v.service_name, 128, true)
  workText(v.cause_ref, 256, true)
}
function parseSprint(
  value: unknown,
  project: string,
  target?: string,
  summary = false,
): Sprint | SprintSummary {
  const v = shape(value, [
    ...keys,
    'milestone_id',
    'state',
    ...(summary ? [] : ['description', 'started_at', 'started_by', 'completed_at', 'completed_by']),
  ])
  const result: SprintSummary = {
    ...base(v, project, target),
    milestone_id: workID(v.milestone_id),
    state: enumeration(v.state, ['planned', 'current', 'completed']),
  }
  if (summary) return freeze(result)
  const started = v.started_at === null ? null : parseSystemInstant(v.started_at),
    completed = v.completed_at === null ? null : parseSystemInstant(v.completed_at)
  requireValue(
    (started === null) === (v.started_by === null) &&
      (completed === null) === (v.completed_by === null),
  )
  requireValue(
    result.state === 'planned'
      ? started === null && completed === null
      : result.state === 'current'
        ? started !== null && completed === null
        : started !== null && completed !== null,
  )
  if (started) {
    requireValue(result.created_at <= started && started <= result.updated_at)
    historyActor(v.started_by, project)
  }
  if (completed) {
    requireValue(started! <= completed && completed <= result.updated_at)
    historyActor(v.completed_by, project)
  }
  return freeze({
    ...result,
    description: workText(v.description, 32768),
    started_at: started,
    completed_at: completed,
  })
}
function parseBlocker(value: unknown, project: string, task: string): Blocker {
  const v = shape(value, [
    'id',
    'project_id',
    'task_id',
    'type',
    'metadata',
    'description',
    'created_at',
    'created_by',
    'resolved_at',
    'resolved_by',
    'resolution_comment',
  ])
  const type = enumeration(v.type, ['rely_on', 'waiting_for_human', 'technical']),
    created = parseSystemInstant(v.created_at),
    resolved = v.resolved_at === null ? null : parseSystemInstant(v.resolved_at)
  requireValue(
    workID(v.project_id) === project &&
      workID(v.task_id) === task &&
      (resolved === null) === (v.resolved_by === null) &&
      (resolved === null ? v.resolution_comment === null : created <= resolved),
  )
  let related: string | null = null
  if (type === 'technical') {
    const m = shape(v.metadata, ['code', 'source', 'reference_id']),
      a = shape(v.created_by, ['type', 'service_name', 'source', 'cause_id'])
    requireValue(
      m.code === 'scheduler_launch_failed' &&
        m.source === 'scheduler_dispatch' &&
        workID(m.reference_id) === workID(a.cause_id) &&
        a.type === 'system' &&
        a.service_name === 'scheduler' &&
        a.source === 'scheduler',
    )
  } else {
    const m = shape(v.metadata, type === 'rely_on' ? ['related_task_id'] : [])
    if (type === 'rely_on') {
      related = workID(m.related_task_id)
      requireValue(related !== task)
    }
    human(v.created_by)
  }
  if (resolved !== null) human(v.resolved_by)
  return freeze({
    id: workID(v.id),
    project_id: project,
    task_id: task,
    type,
    description: workText(v.description, 1024),
    related_task_id: related,
    created_at: created,
    resolved_at: resolved,
    resolution_comment:
      v.resolution_comment === null ? null : workText(v.resolution_comment, 1024, false, true),
  })
}
export function captureReviewInput(value: unknown): ReviewInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request']),
      r = shape(v.request, ['target_state', 'comment'], ['assignee_agent_id'])
    const target = enumeration(r.target_state, ['in_review', 'todo', 'done'])
    requireValue(target === 'done' || Object.hasOwn(r, 'assignee_agent_id'))
    return freeze({
      expected_version: workVersion(v.expected_version),
      request: {
        target_state: target,
        comment: workText(r.comment, 32768, false, true),
        ...(Object.hasOwn(r, 'assignee_agent_id')
          ? { assignee_agent_id: workID(r.assignee_agent_id) }
          : {}),
      },
    })
  })
}
export function captureReadyInput(value: unknown): ReadyInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request']),
      r = shape(v.request, ['target_state', 'assignee_agent_id'], ['comment'])
    requireValue(r.target_state === 'todo')
    return freeze({
      expected_version: workVersion(v.expected_version),
      request: {
        target_state: 'todo' as const,
        assignee_agent_id: workID(r.assignee_agent_id),
        ...(Object.hasOwn(r, 'comment')
          ? { comment: workText(r.comment, 32768, false, true) }
          : {}),
      },
    })
  })
}
// Reuse the same complete Task decoder for other Work receipts.
export function decodeWorkTask(value: unknown, project: string, task: string): Task {
  return parseTask(value, project, task) as Task
}
function query(value: PageQuery): Readonly<Record<string, string>> {
  return input(() => {
    const q = shape(value, [], ['limit', 'cursor']),
      limit = q.limit ?? 50
    requireValue(typeof limit === 'number' && Number.isInteger(limit) && limit >= 1 && limit <= 200)
    const result: Record<string, string> = { limit: String(limit) }
    if (Object.hasOwn(q, 'cursor')) {
      const v = workText(q.cursor, 8192)
      requireValue(v.length > 0)
      result.cursor = v
    }
    return freeze(result)
  })
}
function page<T extends { id: string }>(
  value: unknown,
  q: Readonly<Record<string, string>>,
  parse: (v: unknown) => T,
  compare?: (a: T, b: T) => number,
): Page<T> {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= Number(q.limit))
  const seen = new Set<string>()
  const items = v.items.map((item) => {
    const row = parse(item)
    requireValue(!seen.has(row.id))
    seen.add(row.id)
    return row
  })
  if (compare)
    for (let n = 1; n < items.length; n++) requireValue(compare(items[n - 1]!, items[n]!) < 0)
  let next: string | undefined
  if (Object.hasOwn(v, 'next_cursor')) {
    next = workText(v.next_cursor, 8192)
    requireValue(next.length > 0 && items.length === Number(q.limit) && next !== q.cursor)
  }
  return freeze({ items, ...(next ? { next_cursor: next } : {}) })
}
const rank = (a: MilestoneSummary, b: MilestoneSummary) =>
  a.manual_rank < b.manual_rank ? -1 : a.manual_rank > b.manual_rank ? 1 : a.id.localeCompare(b.id)
function receipt(
  value: unknown,
  project: string,
  task: string,
  command: ReviewInput | ReadyInput,
): ReviewReceipt {
  const v = shape(value, ['task', 'task_event_ids', 'event_ids']),
    result = parseTask(v.task, project, task) as Task
  requireValue(
    BigInt(result.version) === BigInt(command.expected_version) + 1n &&
      result.state === command.request.target_state &&
      (command.request.assignee_agent_id === undefined ||
        result.assignee_agent_id === command.request.assignee_agent_id),
  )
  requireValue(
    Array.isArray(v.task_event_ids) &&
      v.task_event_ids.length >= 1 &&
      v.task_event_ids.length <= 35 &&
      Array.isArray(v.event_ids) &&
      v.event_ids.length === 1,
  )
  const history = v.task_event_ids.map(workID),
    events = v.event_ids.map(workID)
  requireValue(history.every((id, n) => n === 0 || history[n - 1]! < id))
  return freeze({ task: result, task_event_ids: history, event_ids: events })
}
export function createWorkReviewAPI(fetcher?: Fetch): WorkReviewAPI {
  const call = accountTransport(fetcher)
  const ids = (project: string, target?: string) =>
    input(() => ({ projectID: workID(project), ...(target ? { target: workID(target) } : {}) }))
  function readyCall(
    project: string,
    task: string,
    value: ReadyInput,
    write: WriteOptions,
    lookup: boolean,
  ) {
    const captured = captureReadyInput(value)
    return call(
      lookup ? 'reviewLookup' : 'reviewTransfer',
      (v): ReviewReceipt | ReviewLookup => {
        if (!lookup) return receipt(v, project, task, captured)
        const r = shape(v, ['status', 'receipt']),
          status = enumeration(r.status, ['committed', 'in_progress', 'not_observed'])
        if (status === 'committed')
          return { status, receipt: receipt(r.receipt, project, task, captured) }
        requireValue(r.receipt === null)
        return { status, receipt: null }
      },
      {
        ...ids(project, lookup ? undefined : task),
        signal: write.signal ?? new AbortController().signal,
        body: lookup
          ? { command: 'work.task.transfer', target_id: input(() => workID(task)), ...captured }
          : captured,
        csrf: write.csrfToken,
        key: write.key,
      },
    )
  }
  return {
    ready: (p, t, value, write) => readyCall(p, t, value, write, false) as Promise<ReviewReceipt>,
    lookupReady: (p, t, value, write) =>
      readyCall(p, t, value, write, true) as Promise<ReviewLookup>,
    milestones(project, q, signal) {
      const captured = query(q)
      return call(
        'reviewMilestones',
        (v) => page(v, captured, (item) => base(shape(item, keys), project), rank),
        { ...ids(project), signal, workQuery: captured },
      )
    },
    milestone(project, target, signal) {
      return call(
        'reviewMilestone',
        (v) => {
          const r = shape(v, [...keys, 'description'])
          return freeze({
            ...base(r, project, target),
            description: workText(r.description, 32768),
          })
        },
        { ...ids(project, target), signal },
      )
    },
    sprints(project, milestone, q, signal) {
      const captured = { ...query(q), milestone_id: input(() => workID(milestone)) }
      return call(
        'reviewSprints',
        (v) =>
          page(
            v,
            captured,
            (item) => {
              const r = parseSprint(item, project, undefined, true)
              requireValue(r.milestone_id === milestone)
              return r
            },
            rank,
          ),
        { ...ids(project), signal, workQuery: captured },
      )
    },
    sprint(project, target, signal) {
      return call('reviewSprint', (v) => parseSprint(v, project, target) as Sprint, {
        ...ids(project, target),
        signal,
      })
    },
    tasks(project, q, signal) {
      const captured = input(() => {
        const v = shape(q, ['sprint_id', 'state'], ['limit', 'cursor'])
        return {
          ...query({
            ...(v.limit === undefined ? {} : { limit: v.limit as number }),
            ...(v.cursor === undefined ? {} : { cursor: v.cursor as string }),
          }),
          sprint_id: workID(v.sprint_id),
          state: enumeration(v.state, taskStates),
        }
      })
      return call(
        'reviewTasks',
        (v) =>
          page(
            v,
            captured,
            (item) => {
              const r = parseTask(item, project, undefined, true) as TaskSummary
              requireValue(r.sprint_id === captured.sprint_id && r.state === captured.state)
              return r
            },
            (a, b) => priorities.indexOf(a.priority) - priorities.indexOf(b.priority) || rank(a, b),
          ),
        { ...ids(project), signal, workQuery: captured },
      )
    },
    task(project, target, signal) {
      return call('reviewTask', (v) => parseTask(v, project, target) as Task, {
        ...ids(project, target),
        signal,
      })
    },
    blockers(project, task, q, signal) {
      const captured = { ...query(q), status: 'all' }
      return call(
        'reviewBlockers',
        (v) => page(v, captured, (item) => parseBlocker(item, project, task)),
        { ...ids(project, task), signal, workQuery: captured },
      )
    },
    transfer(project, task, value, write) {
      const captured = captureReviewInput(value)
      return call('reviewTransfer', (v) => receipt(v, project, task, captured), {
        ...ids(project, task),
        signal: write.signal ?? new AbortController().signal,
        body: captured,
        csrf: write.csrfToken,
        key: write.key,
      })
    },
    lookup(project, task, value, write) {
      const captured = captureReviewInput(value)
      return call(
        'reviewLookup',
        (v) => {
          const r = shape(v, ['status', 'receipt'])
          const status = enumeration(r.status, ['committed', 'in_progress', 'not_observed'])
          if (status === 'committed')
            return { status, receipt: receipt(r.receipt, project, task, captured) }
          requireValue(r.receipt === null)
          return { status, receipt: null }
        },
        {
          ...ids(project),
          signal: write.signal ?? new AbortController().signal,
          body: {
            command: 'work.task.transfer',
            target_id: input(() => workID(task)),
            ...captured,
          },
          csrf: write.csrfToken,
          key: write.key,
        },
      )
    },
  }
}
