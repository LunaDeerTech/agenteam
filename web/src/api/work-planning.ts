import { AccountFailure, accountTransport, shape, string, uuid7, type Fetch } from './client'
import { parseSystemInstant } from './system-account'

export type WorkVersion = string
export type WorkTaskType = 'feature' | 'bug' | 'task' | 'spike' | 'chore'
export type WorkTaskPriority = 'low' | 'medium' | 'high' | 'critical'
export type WorkTaskState =
  'backlog' | 'todo' | 'in_progress' | 'in_review' | 'blocked' | 'done' | 'cancelled'
export type WorkSprintState = 'planned' | 'current' | 'completed'
export type WorkActorHistory =
  | Readonly<{ kind: 'human'; user_id: string }>
  | Readonly<{ kind: 'agent_run'; project_id: string; agent_id: string; execution_id: string }>
  | Readonly<{
      kind: 'service'
      service_name: string
      cause_ref: string
      project_id: string | null
    }>
export type WorkTaskEventActor = Readonly<{ type: 'human'; user_id: string; source: 'task_domain' }>
export interface WorkMilestoneSummary {
  readonly id: string
  readonly project_id: string
  readonly title: string
  readonly manual_rank: string
  readonly version: WorkVersion
  readonly created_at: string
  readonly updated_at: string
}
export interface WorkMilestone extends WorkMilestoneSummary {
  readonly description: string
}
export interface WorkSprintSummary extends WorkMilestoneSummary {
  readonly milestone_id: string
  readonly state: WorkSprintState
}
export interface WorkSprint extends WorkSprintSummary {
  readonly description: string
  readonly started_at: string | null
  readonly started_by: WorkActorHistory | null
  readonly completed_at: string | null
  readonly completed_by: WorkActorHistory | null
}
export interface WorkTaskSummary extends WorkMilestoneSummary {
  readonly milestone_id: string
  readonly sprint_id: string
  readonly type: WorkTaskType
  readonly priority: WorkTaskPriority
  readonly state: WorkTaskState
  readonly assignee_agent_id: string | null
}
export interface WorkTask extends WorkTaskSummary {
  readonly description: string
  readonly plan: string
}
type BlockerMetadata =
  | Readonly<{ type: 'rely_on'; metadata: Readonly<{ related_task_id: string }> }>
  | Readonly<{ type: 'waiting_for_human'; metadata: Readonly<Record<string, never>> }>
export type WorkTaskBlocker = BlockerMetadata &
  Readonly<{
    id: string
    project_id: string
    task_id: string
    description: string
    created_at: string
    created_by: WorkTaskEventActor
    resolved_at: string | null
    resolved_by: WorkTaskEventActor | null
    resolution_comment: string | null
  }>
export type WorkPage<T> = Readonly<{ items: readonly T[]; next_cursor?: string }>
export type WorkMilestonePage = WorkPage<WorkMilestoneSummary>
export type WorkSprintPage = WorkPage<WorkSprintSummary>
export type WorkTaskPage = WorkPage<WorkTaskSummary>
export type WorkTaskBlockerPage = WorkPage<WorkTaskBlocker>
export type WorkMilestoneQuery = Readonly<{ limit?: number; cursor?: string }>
export type WorkSprintQuery = WorkMilestoneQuery & Readonly<{ milestone_id: string }>
export type WorkTaskQuery = WorkMilestoneQuery &
  Readonly<{
    milestone_id?: string
    sprint_id?: string
    state?: WorkTaskState
    priority?: WorkTaskPriority
    type?: WorkTaskType
    text?: string
    assignee_agent_id?: string | null
  }>
export type WorkTaskBlockerQuery = WorkMilestoneQuery &
  Readonly<{ status?: 'unresolved' | 'resolved' | 'all' }>
export type WorkMilestoneCreateRequest = Readonly<{
  milestone_id: string
  title: string
  description?: string
}>
export type WorkSprintCreateRequest = Readonly<{
  sprint_id: string
  milestone_id: string
  title: string
  description?: string
}>
export type WorkStructureUpdateRequest = Readonly<{ title?: string; description?: string }>
export type WorkReorderRequest = Readonly<{ before_id?: string }>
export type WorkSprintReorderRequest = WorkReorderRequest & Readonly<{ milestone_id: string }>
export type WorkTaskCreateRequest = Readonly<{
  task_id: string
  sprint_id: string
  title: string
  description?: string
  type: WorkTaskType
  priority: WorkTaskPriority
  plan?: string
}>
export type WorkTaskUpdateRequest = WorkStructureUpdateRequest &
  Readonly<{ type?: WorkTaskType; priority?: WorkTaskPriority; plan?: string }>
export type WorkBlockerAddRequest = BlockerMetadata &
  Readonly<{ blocker_id: string; description: string }>
export type WorkBlockerResolveRequest = Readonly<{
  blocker_id: string
  resolution_comment: string | null
}>
export type WorkCreateBody<T> = Readonly<{ request: T }>
export type WorkUpdateBody<T> = Readonly<{ expected_version: WorkVersion; request: T }>
type Create<C extends string, R> = Readonly<{ command: C; request: R }>
type Update<C extends string, R> = Readonly<{
  command: C
  targetID: string
  expected_version: WorkVersion
  request: R
}>
export type WorkStructureCommand = Readonly<{ domain: 'structure'; projectID: string }> &
  (
    | Create<'work.milestone.create', WorkMilestoneCreateRequest>
    | Update<'work.milestone.update', WorkStructureUpdateRequest>
    | Update<'work.milestone.reorder', WorkReorderRequest>
    | Create<'work.sprint.create', WorkSprintCreateRequest>
    | Update<'work.sprint.update', WorkStructureUpdateRequest>
    | Update<'work.sprint.reorder', WorkSprintReorderRequest>
  )
export type WorkTaskCommand = Readonly<{ domain: 'task'; projectID: string }> &
  (
    | Create<'work.task.create', WorkTaskCreateRequest>
    | Update<'work.task.update', WorkTaskUpdateRequest>
    | Update<'work.task.reorder', WorkReorderRequest>
  )
export type WorkBlockerCommand = Readonly<{
  domain: 'blocker'
  projectID: string
  taskID: string
  expected_version: WorkVersion
}> &
  (
    | Create<'work.task.blocker.add', WorkBlockerAddRequest>
    | Create<'work.task.blocker.resolve', WorkBlockerResolveRequest>
  )
export type WorkPlanningCommand = WorkStructureCommand | WorkTaskCommand | WorkBlockerCommand
type LookupWire<T> = T extends { targetID: string }
  ? Omit<T, 'domain' | 'projectID' | 'targetID'> & Readonly<{ target_id: string }>
  : Omit<T, 'domain' | 'projectID' | 'taskID'>
export type WorkStructureLookupRequest = LookupWire<WorkStructureCommand>
export type WorkTaskLookupRequest = LookupWire<WorkTaskCommand>
export type WorkBlockerLookupRequest = LookupWire<WorkBlockerCommand>
export type WorkStructureMutation = Readonly<{
  command: WorkStructureCommand['command']
  changed: boolean
  milestone: WorkMilestone | null
  sprint: WorkSprint | null
  event_id: string | null
}>
export type WorkTaskMutation = Readonly<{
  task: WorkTask
  changed: boolean
  task_event_id: string | null
  event_ids: readonly string[]
}>
export type WorkTaskBlockerMutation = Readonly<{
  task: WorkTask
  blocker: WorkTaskBlocker
  task_event_id: string
  event_ids: readonly string[]
}>
export type WorkStructureLookup =
  | Readonly<{ state: 'committed'; result: WorkStructureMutation }>
  | Readonly<{ state: 'in_progress' | 'not_observed'; result: null }>
export type WorkTaskLookup =
  | Readonly<{ status: 'committed'; receipt: WorkTaskMutation }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export type WorkTaskBlockerLookup =
  | Readonly<{ status: 'committed'; receipt: WorkTaskBlockerMutation }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export type WorkPlanningReceipt =
  | Readonly<{ domain: 'structure'; value: WorkStructureMutation }>
  | Readonly<{ domain: 'task'; value: WorkTaskMutation }>
  | Readonly<{ domain: 'blocker'; value: WorkTaskBlockerMutation }>
export type WorkPlanningObservation =
  | Readonly<{ domain: 'structure'; value: WorkStructureLookup }>
  | Readonly<{ domain: 'task'; value: WorkTaskLookup }>
  | Readonly<{ domain: 'blocker'; value: WorkTaskBlockerLookup }>
export type WorkWriteOptions = Readonly<{ signal: AbortSignal; csrf: string; key: string }>
const types = ['feature', 'bug', 'task', 'spike', 'chore'] as const
const priorities = ['low', 'medium', 'high', 'critical'] as const
const states = [
  'backlog',
  'todo',
  'in_progress',
  'in_review',
  'blocked',
  'done',
  'cancelled',
] as const
const commands = [
  'work.milestone.create',
  'work.milestone.update',
  'work.milestone.reorder',
  'work.sprint.create',
  'work.sprint.update',
  'work.sprint.reorder',
  'work.task.create',
  'work.task.update',
  'work.task.reorder',
  'work.task.blocker.add',
  'work.task.blocker.resolve',
] as const
function requireValue(ok: unknown): asserts ok {
  if (!ok) throw new AccountFailure('invalid-response')
}
function freeze<T>(value: T): T {
  if (value && typeof value === 'object') {
    for (const item of Object.values(value)) freeze(item)
    Object.freeze(value)
  }
  return value
}
function input<T>(work: () => T): T {
  try {
    return freeze(work())
  } catch {
    throw new AccountFailure('invalid-input')
  }
}
function id(value: unknown): string {
  const v = string(value, 36, 36)
  requireValue(uuid7.test(v))
  return v
}
function version(value: unknown): string {
  const v = string(value, 1, 19)
  requireValue(/^[1-9][0-9]*$/.test(v) && BigInt(v) <= 9223372036854775807n)
  return v
}
function enumeration<T extends string>(value: unknown, values: readonly T[]): T {
  requireValue(typeof value === 'string' && values.includes(value as T))
  return value as T
}
function text(value: unknown, maximum: number, title = false, nonblank = false): string {
  const v = string(value, 0, maximum)
  requireValue(
    !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v) &&
      new TextEncoder().encode(v).byteLength <= maximum,
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
function cursor(value: unknown): string {
  const v = string(value, 1, 8192)
  requireValue(
    !v.includes('\0') &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(v) &&
      new TextEncoder().encode(v).byteLength <= 8192,
  )
  return v
}
export function newWorkPlanningID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  let time = BigInt(Date.now())
  if (time < 0n || time > 0xffffffffffffn) throw new AccountFailure('invalid-input')
  for (let i = 5; i >= 0; i--) {
    bytes[i] = Number(time & 255n)
    time >>= 8n
  }
  bytes[6] = (bytes[6]! & 15) | 0x70
  bytes[8] = (bytes[8]! & 63) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
export function captureWorkPlanningCommand(value: WorkPlanningCommand): WorkPlanningCommand {
  return input(() => {
    const command = enumeration(value.command, commands),
      domain = command.includes('.blocker.')
        ? 'blocker'
        : command.startsWith('work.task.')
          ? 'task'
          : 'structure'
    const create = command.endsWith('.create')
    const v = shape(value, [
      'domain',
      'projectID',
      'command',
      'request',
      ...(create ? [] : ['expected_version']),
      ...(domain === 'blocker' ? ['taskID'] : create ? [] : ['targetID']),
    ])
    requireValue(v.domain === domain)
    const fields = command.endsWith('.create')
      ? command.startsWith('work.milestone.')
        ? ['milestone_id', 'title']
        : command.startsWith('work.sprint.')
          ? ['sprint_id', 'milestone_id', 'title']
          : ['task_id', 'sprint_id', 'title', 'type', 'priority']
      : command.endsWith('.add')
        ? ['blocker_id', 'type', 'description', 'metadata']
        : command.endsWith('.resolve')
          ? ['blocker_id', 'resolution_comment']
          : command === 'work.sprint.reorder'
            ? ['milestone_id']
            : []
    const optional = create
      ? ['description', ...(domain === 'task' ? ['plan'] : [])]
      : command.endsWith('.update')
        ? ['title', 'description', ...(domain === 'task' ? ['type', 'priority', 'plan'] : [])]
        : command.endsWith('.reorder')
          ? ['before_id']
          : []
    const r = shape(v.request, fields, optional),
      request: Record<string, unknown> = {}
    requireValue(!command.endsWith('.update') || Object.keys(r).length > 0)
    for (const [key, item] of Object.entries(r)) {
      if (key.endsWith('_id')) request[key] = id(item)
      else if (key === 'title') request[key] = text(item, 1024, true)
      else if (key === 'description' || key === 'plan')
        request[key] = text(item, domain === 'blocker' ? 1024 : 32768)
      else if (key === 'resolution_comment')
        request[key] = item === null ? null : text(item, 1024, false, true)
      else if (key === 'type')
        request[key] = enumeration(
          item,
          domain === 'blocker' ? ['rely_on', 'waiting_for_human'] : types,
        )
      else if (key === 'priority') request[key] = enumeration(item, priorities)
      else if (key === 'metadata') {
        const m = shape(item, r.type === 'rely_on' ? ['related_task_id'] : [])
        request[key] = r.type === 'rely_on' ? { related_task_id: id(m.related_task_id) } : {}
      }
    }
    const result = {
      domain,
      projectID: id(v.projectID),
      command,
      ...(create ? {} : { expected_version: version(v.expected_version) }),
      ...(domain === 'blocker'
        ? { taskID: id(v.taskID) }
        : create
          ? {}
          : { targetID: id(v.targetID) }),
      request,
    }
    if (Object.hasOwn(request, 'before_id')) requireValue(request.before_id !== v.targetID)
    if (domain === 'blocker' && r.type === 'rely_on')
      requireValue((request.metadata as { related_task_id: string }).related_task_id !== v.taskID)
    return result as WorkPlanningCommand
  })
}
export function workPlanningTarget(command: WorkPlanningCommand): string {
  if (command.domain === 'blocker') return command.taskID
  if ('targetID' in command) return command.targetID
  if (command.command === 'work.milestone.create') return command.request.milestone_id
  if (command.command === 'work.sprint.create') return command.request.sprint_id
  return command.request.task_id
}
export function workPlanningBody(
  command: WorkPlanningCommand,
): WorkCreateBody<WorkPlanningCommand['request']> | WorkUpdateBody<WorkPlanningCommand['request']> {
  return freeze({
    ...('expected_version' in command ? { expected_version: command.expected_version } : {}),
    request: command.request,
  })
}
export function workPlanningLookup(command: WorkStructureCommand): WorkStructureLookupRequest
export function workPlanningLookup(command: WorkTaskCommand): WorkTaskLookupRequest
export function workPlanningLookup(command: WorkBlockerCommand): WorkBlockerLookupRequest
export function workPlanningLookup(command: WorkPlanningCommand) {
  return freeze({
    command: command.command,
    ...('targetID' in command ? { target_id: command.targetID } : {}),
    ...workPlanningBody(command),
  })
}
function captureLookup(
  domain: WorkPlanningCommand['domain'],
  projectID: string,
  value: unknown,
  taskID?: string,
): WorkPlanningCommand {
  return input(() => {
    const v = shape(value, ['command', 'request'], ['target_id', 'expected_version'])
    return captureWorkPlanningCommand({
      domain,
      projectID,
      command: v.command,
      request: v.request,
      ...(Object.hasOwn(v, 'expected_version') ? { expected_version: v.expected_version } : {}),
      ...(Object.hasOwn(v, 'target_id') ? { targetID: v.target_id } : {}),
      ...(domain === 'blocker' ? { taskID } : {}),
    } as WorkPlanningCommand)
  })
}
export function captureWorkPlanningQuery(
  kind: 'milestone' | 'sprint' | 'task' | 'blocker',
  value: WorkMilestoneQuery | WorkSprintQuery | WorkTaskQuery | WorkTaskBlockerQuery,
): Readonly<Record<string, string>> {
  return input(() => {
    const fields =
      kind === 'sprint'
        ? ['milestone_id']
        : kind === 'task'
          ? ['milestone_id', 'sprint_id', 'type', 'priority', 'state', 'text', 'assignee_agent_id']
          : kind === 'blocker'
            ? ['status']
            : []
    const v = shape(value, kind === 'sprint' ? ['milestone_id'] : [], [
        'limit',
        'cursor',
        ...fields,
      ]),
      result: Record<string, string> = {}
    for (const [key, item] of Object.entries(v)) {
      if (key === 'limit') {
        requireValue(typeof item === 'number' && Number.isInteger(item) && item >= 1 && item <= 200)
        result[key] = String(item)
      } else if (key === 'cursor') result[key] = cursor(item)
      else if (key.endsWith('_id'))
        result[key] = key === 'assignee_agent_id' && item === null ? 'null' : id(item)
      else if (key === 'text') result[key] = text(item, 1024, true)
      else
        result[key] = enumeration(
          item,
          key === 'type'
            ? types
            : key === 'priority'
              ? priorities
              : key === 'state'
                ? states
                : ['unresolved', 'resolved', 'all'],
        )
    }
    return result
  })
}
const baseKeys = ['id', 'project_id', 'title', 'manual_rank', 'version', 'created_at', 'updated_at']
function base(
  v: Record<string, unknown>,
  projectID: string,
  targetID?: string,
): WorkMilestoneSummary {
  const result = {
    id: id(v.id),
    project_id: id(v.project_id),
    title: text(v.title, 1024, true),
    manual_rank: string(v.manual_rank, 32, 32),
    version: version(v.version),
    created_at: parseSystemInstant(v.created_at),
    updated_at: parseSystemInstant(v.updated_at),
  }
  requireValue(
    result.project_id === projectID &&
      (!targetID || result.id === targetID) &&
      result.created_at <= result.updated_at &&
      /^[0-9a-f]{32}$/.test(result.manual_rank) &&
      !['0'.repeat(32), 'f'.repeat(32)].includes(result.manual_rank),
  )
  return result
}
function actor(value: unknown, projectID: string): WorkActorHistory {
  const kind = shape(
    value,
    ['kind'],
    ['user_id', 'project_id', 'agent_id', 'execution_id', 'service_name', 'cause_ref'],
  ).kind
  if (kind === 'human') {
    const v = shape(value, ['kind', 'user_id'])
    return { kind, user_id: id(v.user_id) }
  }
  if (kind === 'agent_run') {
    const v = shape(value, ['kind', 'project_id', 'agent_id', 'execution_id'])
    requireValue(id(v.project_id) === projectID)
    return {
      kind,
      project_id: projectID,
      agent_id: id(v.agent_id),
      execution_id: id(v.execution_id),
    }
  }
  const v = shape(value, ['kind', 'project_id', 'service_name', 'cause_ref'])
  requireValue(kind === 'service')
  const name = enumeration(v.service_name, [
    'secret',
    'secret-maintenance',
    'outbound',
    'object',
    'object-maintenance',
    'project-lifecycle',
    'project-initialization',
    'outbox-delivery',
    'account-bootstrap',
    'account-auth',
    'account-maintenance',
    'account-mail',
    'model-runtime',
  ])
  const cause = string(v.cause_ref, 36, 71)
  requireValue(uuid7.test(cause) || /^sha256:[0-9a-f]{64}$/.test(cause))
  requireValue(id(v.project_id) === projectID)
  return {
    kind: 'service',
    service_name: name,
    cause_ref: cause,
    project_id: projectID,
  }
}
function parseMilestone(
  value: unknown,
  projectID: string,
  targetID?: string,
  summary = false,
): WorkMilestone {
  const v = shape(value, [...baseKeys, ...(summary ? [] : ['description'])]),
    result = base(v, projectID, targetID)
  return freeze(
    summary ? result : { ...result, description: text(v.description, 32768) },
  ) as WorkMilestone
}
function parseSprint(
  value: unknown,
  projectID: string,
  targetID?: string,
  summary = false,
): WorkSprint {
  const v = shape(value, [
    ...baseKeys,
    'milestone_id',
    'state',
    ...(summary ? [] : ['description', 'started_at', 'started_by', 'completed_at', 'completed_by']),
  ])
  const result = {
    ...base(v, projectID, targetID),
    milestone_id: id(v.milestone_id),
    state: enumeration(v.state, ['planned', 'current', 'completed']),
  }
  if (summary) return freeze(result) as WorkSprint
  const started_at = v.started_at === null ? null : parseSystemInstant(v.started_at),
    completed_at = v.completed_at === null ? null : parseSystemInstant(v.completed_at)
  const started_by = v.started_by === null ? null : actor(v.started_by, projectID),
    completed_by = v.completed_by === null ? null : actor(v.completed_by, projectID)
  requireValue(
    result.state === 'planned'
      ? started_at === null && started_by === null && completed_at === null && completed_by === null
      : started_at !== null &&
          started_by !== null &&
          (result.state === 'current'
            ? completed_at === null && completed_by === null
            : completed_at !== null && completed_by !== null),
  )
  requireValue(
    (started_at === null || (result.created_at <= started_at && started_at <= result.updated_at)) &&
      (completed_at === null || (started_at! <= completed_at && completed_at <= result.updated_at)),
  )
  return freeze({
    ...result,
    description: text(v.description, 32768),
    started_at,
    started_by,
    completed_at,
    completed_by,
  })
}
function parseTask(
  value: unknown,
  projectID: string,
  targetID?: string,
  summary = false,
): WorkTask {
  const v = shape(value, [
    ...baseKeys,
    'milestone_id',
    'sprint_id',
    'type',
    'priority',
    'state',
    'assignee_agent_id',
    ...(summary ? [] : ['description', 'plan']),
  ])
  const result = {
    ...base(v, projectID, targetID),
    milestone_id: id(v.milestone_id),
    sprint_id: id(v.sprint_id),
    type: enumeration(v.type, types),
    priority: enumeration(v.priority, priorities),
    state: enumeration(v.state, states),
    assignee_agent_id: v.assignee_agent_id === null ? null : id(v.assignee_agent_id),
  }
  requireValue(
    !['todo', 'in_progress', 'in_review'].includes(result.state) ||
      result.assignee_agent_id !== null,
  )
  return freeze(
    summary
      ? result
      : { ...result, description: text(v.description, 32768), plan: text(v.plan, 32768) },
  ) as WorkTask
}
function taskActor(value: unknown): WorkTaskEventActor {
  const v = shape(value, ['type', 'user_id', 'source'])
  requireValue(v.type === 'human' && v.source === 'task_domain')
  return { type: 'human', user_id: id(v.user_id), source: 'task_domain' }
}
function parseBlocker(value: unknown, projectID: string, taskID: string): WorkTaskBlocker {
  const v = shape(value, [
    'id',
    'project_id',
    'task_id',
    'type',
    'description',
    'metadata',
    'created_at',
    'created_by',
    'resolved_at',
    'resolved_by',
    'resolution_comment',
  ])
  const type = enumeration(v.type, ['rely_on', 'waiting_for_human']),
    m = shape(v.metadata, type === 'rely_on' ? ['related_task_id'] : [])
  requireValue(id(v.project_id) === projectID && id(v.task_id) === taskID)
  const created_at = parseSystemInstant(v.created_at),
    resolved_at = v.resolved_at === null ? null : parseSystemInstant(v.resolved_at)
  const resolved_by = v.resolved_by === null ? null : taskActor(v.resolved_by),
    resolution_comment =
      v.resolution_comment === null ? null : text(v.resolution_comment, 1024, false, true)
  requireValue(
    resolved_at === null
      ? resolved_by === null && resolution_comment === null
      : resolved_by !== null && created_at <= resolved_at,
  )
  const metadata = type === 'rely_on' ? { related_task_id: id(m.related_task_id) } : {}
  requireValue(type !== 'rely_on' || metadata.related_task_id !== taskID)
  return freeze({
    id: id(v.id),
    project_id: projectID,
    task_id: taskID,
    type,
    metadata,
    description: text(v.description, 1024),
    created_at,
    created_by: taskActor(v.created_by),
    resolved_at,
    resolved_by,
    resolution_comment,
  }) as WorkTaskBlocker
}
function page<T extends { id: string }>(
  value: unknown,
  parse: (v: unknown) => T,
  limit: number,
  order: (item: T) => readonly (string | number)[],
): WorkPage<T> {
  const v = shape(value, ['items'], ['next_cursor'])
  requireValue(Array.isArray(v.items) && v.items.length <= limit)
  const items = v.items.map((item) => {
    requireValue(new TextEncoder().encode(JSON.stringify(item)).byteLength <= 16384)
    return parse(item)
  })
  requireValue(new Set(items.map((item) => item.id)).size === items.length)
  requireValue(!Object.hasOwn(v, 'next_cursor') || items.length === limit)
  for (let i = 1; i < items.length; i++) {
    const previous = order(items[i - 1]!),
      current = order(items[i]!)
    const different = previous.findIndex((key, index) => key !== current[index])
    requireValue(different >= 0 && previous[different]! < current[different]!)
  }
  return freeze({
    items,
    ...(Object.hasOwn(v, 'next_cursor') ? { next_cursor: cursor(v.next_cursor) } : {}),
  })
}
function matchFields(
  record: WorkMilestone | WorkSprint | WorkTask,
  command: WorkPlanningCommand,
  changed: boolean,
) {
  requireValue(record.id === workPlanningTarget(command))
  if ('expected_version' in command)
    requireValue(BigInt(record.version) === BigInt(command.expected_version) + (changed ? 1n : 0n))
  else requireValue(record.version === '1' && changed)
  if (command.command.endsWith('.reorder')) {
    if (command.command === 'work.sprint.reorder')
      requireValue((record as WorkSprint).milestone_id === command.request.milestone_id)
    return
  }
  const r = command.request as unknown as Record<string, unknown>
  for (const key of [
    'title',
    'description',
    'plan',
    'type',
    'priority',
    'sprint_id',
    'milestone_id',
  ])
    if (
      Object.hasOwn(r, key) &&
      !(command.command === 'work.milestone.create' && key === 'milestone_id') &&
      !(command.command === 'work.sprint.create' && key === 'sprint_id')
    )
      requireValue((record as unknown as Record<string, unknown>)[key] === r[key])
  if (command.command.endsWith('.create')) {
    requireValue(record.description === (r.description ?? ''))
    if (command.domain === 'task')
      requireValue(
        (record as WorkTask).plan === (r.plan ?? '') &&
          (record as WorkTask).state === 'backlog' &&
          (record as WorkTask).assignee_agent_id === null,
      )
    if (command.command === 'work.sprint.create')
      requireValue((record as WorkSprint).state === 'planned')
  }
}
export function parseWorkPlanningReceipt(
  value: unknown,
  command: WorkPlanningCommand,
): WorkPlanningReceipt {
  if (command.domain === 'structure') {
    const v = shape(value, ['command', 'changed', 'milestone', 'sprint', 'event_id'])
    requireValue(v.command === command.command && typeof v.changed === 'boolean')
    const milestone = command.command.startsWith('work.milestone.')
      ? parseMilestone(v.milestone, command.projectID)
      : null
    const sprint = milestone === null ? parseSprint(v.sprint, command.projectID) : null
    requireValue(milestone === null ? v.milestone === null : v.sprint === null)
    const event_id = v.event_id === null ? null : id(v.event_id)
    requireValue(v.changed ? event_id !== null : event_id === null)
    matchFields((milestone ?? sprint)!, command, v.changed)
    return freeze({
      domain: 'structure',
      value: { command: command.command, changed: v.changed, milestone, sprint, event_id },
    })
  }
  if (command.domain === 'task') {
    const v = shape(value, ['task', 'changed', 'task_event_id', 'event_ids'])
    requireValue(
      typeof v.changed === 'boolean' &&
        Array.isArray(v.event_ids) &&
        v.event_ids.length === (v.changed ? 1 : 0),
    )
    const task = parseTask(v.task, command.projectID),
      task_event_id = v.task_event_id === null ? null : id(v.task_event_id)
    requireValue(v.changed ? task_event_id !== null : task_event_id === null)
    matchFields(task, command, v.changed)
    requireValue(task.state === 'backlog' && task.assignee_agent_id === null)
    return freeze({
      domain: 'task',
      value: { task, changed: v.changed, task_event_id, event_ids: v.event_ids.map(id) },
    })
  }
  const v = shape(value, ['task', 'blocker', 'task_event_id', 'event_ids']),
    task = parseTask(v.task, command.projectID, command.taskID),
    blocker = parseBlocker(v.blocker, command.projectID, command.taskID)
  requireValue(
    Array.isArray(v.event_ids) &&
      v.event_ids.length === 1 &&
      task.state === 'backlog' &&
      task.assignee_agent_id === null &&
      BigInt(task.version) === BigInt(command.expected_version) + 1n &&
      blocker.id === command.request.blocker_id,
  )
  if (command.command === 'work.task.blocker.add') {
    requireValue(
      blocker.type === command.request.type &&
        blocker.description === command.request.description &&
        blocker.resolved_at === null &&
        task.updated_at === blocker.created_at,
    )
    if (blocker.type === 'rely_on' && command.request.type === 'rely_on')
      requireValue(blocker.metadata.related_task_id === command.request.metadata.related_task_id)
  } else
    requireValue(
      blocker.resolved_at !== null &&
        blocker.resolved_at === task.updated_at &&
        blocker.resolution_comment === command.request.resolution_comment,
    )
  return freeze({
    domain: 'blocker',
    value: { task, blocker, task_event_id: id(v.task_event_id), event_ids: v.event_ids.map(id) },
  })
}
export function parseWorkPlanningObservation(
  value: unknown,
  command: WorkPlanningCommand,
): WorkPlanningObservation {
  if (command.domain === 'structure') {
    const v = shape(value, ['state', 'result']),
      state = enumeration(v.state, ['committed', 'in_progress', 'not_observed'])
    if (state === 'committed') {
      const r = parseWorkPlanningReceipt(v.result, command)
      requireValue(r.domain === 'structure')
      return freeze({ domain: 'structure', value: { state, result: r.value } })
    }
    requireValue(v.result === null)
    return freeze({ domain: 'structure', value: { state, result: null } })
  }
  const v = shape(value, ['status', 'receipt']),
    status = enumeration(v.status, ['committed', 'in_progress', 'not_observed'])
  if (status === 'committed') {
    const r = parseWorkPlanningReceipt(v.receipt, command)
    if (r.domain === 'task') return freeze({ domain: 'task', value: { status, receipt: r.value } })
    requireValue(r.domain === 'blocker')
    return freeze({ domain: 'blocker', value: { status, receipt: r.value } })
  }
  requireValue(v.receipt === null)
  return command.domain === 'task'
    ? freeze({ domain: 'task', value: { status, receipt: null } })
    : freeze({ domain: 'blocker', value: { status, receipt: null } })
}

export function createWorkPlanningAPI(fetcher?: Fetch) {
  const request = accountTransport(fetcher)
  return {
    listMilestones(
      projectID: string,
      query: WorkMilestoneQuery,
      signal: AbortSignal,
    ): Promise<WorkMilestonePage> {
      const project = input(() => id(projectID)),
        captured = captureWorkPlanningQuery('milestone', query)
      return request(
        'workListMilestones',
        (value) =>
          page(
            value,
            (item) => {
              const result = parseMilestone(item, project, undefined, true)
              return result
            },
            Number(captured.limit ?? 50),
            (item) => [item.manual_rank, item.id],
          ),
        { signal, projectID: project, workQuery: captured },
      )
    },
    getMilestone(projectID: string, targetID: string, signal: AbortSignal): Promise<WorkMilestone> {
      const project = input(() => id(projectID)),
        target = input(() => id(targetID))
      return request('workGetMilestone', (value) => parseMilestone(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    createMilestone(
      projectID: string,
      body: WorkCreateBody<WorkMilestoneCreateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.milestone.create',
        ...body,
      })
      return request(
        'workCreateMilestone',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        { ...options, projectID: command.projectID, body: workPlanningBody(command) },
      )
    },
    updateMilestone(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkStructureUpdateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.milestone.update',
        targetID,
        ...body,
      })
      return request(
        'workUpdateMilestone',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    reorderMilestone(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkReorderRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.milestone.reorder',
        targetID,
        ...body,
      })
      return request(
        'workReorderMilestone',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    listSprints(
      projectID: string,
      query: WorkSprintQuery,
      signal: AbortSignal,
    ): Promise<WorkSprintPage> {
      const project = input(() => id(projectID)),
        captured = captureWorkPlanningQuery('sprint', query)
      return request(
        'workListSprints',
        (value) =>
          page(
            value,
            (item) => {
              const result = parseSprint(item, project, undefined, true)
              requireValue(result.milestone_id === captured.milestone_id)
              return result
            },
            Number(captured.limit ?? 50),
            (item) => [item.manual_rank, item.id],
          ),
        { signal, projectID: project, workQuery: captured },
      )
    },
    getSprint(projectID: string, targetID: string, signal: AbortSignal): Promise<WorkSprint> {
      const project = input(() => id(projectID)),
        target = input(() => id(targetID))
      return request('workGetSprint', (value) => parseSprint(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    createSprint(
      projectID: string,
      body: WorkCreateBody<WorkSprintCreateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.sprint.create',
        ...body,
      })
      return request(
        'workCreateSprint',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        { ...options, projectID: command.projectID, body: workPlanningBody(command) },
      )
    },
    updateSprint(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkStructureUpdateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.sprint.update',
        targetID,
        ...body,
      })
      return request(
        'workUpdateSprint',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    reorderSprint(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkSprintReorderRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkStructureMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'structure',
        projectID,
        command: 'work.sprint.reorder',
        targetID,
        ...body,
      })
      return request(
        'workReorderSprint',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    listTasks(projectID: string, query: WorkTaskQuery, signal: AbortSignal): Promise<WorkTaskPage> {
      const project = input(() => id(projectID)),
        captured = captureWorkPlanningQuery('task', query)
      return request(
        'workListTasks',
        (value) =>
          page(
            value,
            (item) => {
              const result = parseTask(item, project, undefined, true)
              for (const key of [
                'milestone_id',
                'sprint_id',
                'state',
                'type',
                'priority',
                'assignee_agent_id',
              ] as const)
                if (Object.hasOwn(captured, key))
                  requireValue(String(result[key]) === captured[key])
              return result
            },
            Number(captured.limit ?? 50),
            (item) => [
              item.sprint_id,
              states.indexOf(item.state),
              ['critical', 'high', 'medium', 'low'].indexOf(item.priority),
              item.manual_rank,
              item.id,
            ],
          ),
        { signal, projectID: project, workQuery: captured },
      )
    },
    getTask(projectID: string, targetID: string, signal: AbortSignal): Promise<WorkTask> {
      const project = input(() => id(projectID)),
        target = input(() => id(targetID))
      return request('workGetTask', (value) => parseTask(value, project, target), {
        signal,
        projectID: project,
        target,
      })
    },
    createTask(
      projectID: string,
      body: WorkCreateBody<WorkTaskCreateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkTaskMutation> {
      input(() => shape(body, ['request']))
      const command = captureWorkPlanningCommand({
        domain: 'task',
        projectID,
        command: 'work.task.create',
        ...body,
      })
      return request(
        'workCreateTask',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'task')
          return result.value
        },
        { ...options, projectID: command.projectID, body: workPlanningBody(command) },
      )
    },
    updateTask(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkTaskUpdateRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkTaskMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'task',
        projectID,
        command: 'work.task.update',
        targetID,
        ...body,
      })
      return request(
        'workUpdateTask',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'task')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    reorderTask(
      projectID: string,
      targetID: string,
      body: WorkUpdateBody<WorkReorderRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkTaskMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'task',
        projectID,
        command: 'work.task.reorder',
        targetID,
        ...body,
      })
      return request(
        'workReorderTask',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'task')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    listTaskBlockers(
      projectID: string,
      taskID: string,
      query: WorkTaskBlockerQuery,
      signal: AbortSignal,
    ): Promise<WorkTaskBlockerPage> {
      const project = input(() => id(projectID)),
        task = input(() => id(taskID)),
        captured = captureWorkPlanningQuery('blocker', query)
      return request(
        'workListTaskBlockers',
        (value) =>
          page(
            value,
            (item) => {
              const result = parseBlocker(item, project, task),
                status = captured.status ?? 'unresolved'
              requireValue(
                status === 'all' || (status === 'unresolved') === (result.resolved_at === null),
              )
              return result
            },
            Number(captured.limit ?? 50),
            (item) => [item.created_at, item.id],
          ),
        { signal, projectID: project, target: task, workQuery: captured },
      )
    },
    addTaskBlocker(
      projectID: string,
      taskID: string,
      body: WorkUpdateBody<WorkBlockerAddRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkTaskBlockerMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'blocker',
        projectID,
        taskID,
        command: 'work.task.blocker.add',
        ...body,
      })
      return request(
        'workAddTaskBlocker',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'blocker')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    resolveTaskBlocker(
      projectID: string,
      taskID: string,
      body: WorkUpdateBody<WorkBlockerResolveRequest>,
      options: WorkWriteOptions,
    ): Promise<WorkTaskBlockerMutation> {
      input(() => shape(body, ['expected_version', 'request']))
      const command = captureWorkPlanningCommand({
        domain: 'blocker',
        projectID,
        taskID,
        command: 'work.task.blocker.resolve',
        ...body,
      })
      return request(
        'workResolveTaskBlocker',
        (value) => {
          const result = parseWorkPlanningReceipt(value, command)
          requireValue(result.domain === 'blocker')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningBody(command),
        },
      )
    },
    lookupStructureCommand(
      projectID: string,
      body: WorkStructureLookupRequest,
      options: WorkWriteOptions,
    ): Promise<WorkStructureLookup> {
      const command = captureLookup('structure', projectID, body)
      requireValue(command.domain === 'structure')
      return request(
        'workLookupStructureCommand',
        (value) => {
          const result = parseWorkPlanningObservation(value, command)
          requireValue(result.domain === 'structure')
          return result.value
        },
        { ...options, projectID: command.projectID, body: workPlanningLookup(command) },
      )
    },
    lookupTaskCommand(
      projectID: string,
      body: WorkTaskLookupRequest,
      options: WorkWriteOptions,
    ): Promise<WorkTaskLookup> {
      const command = captureLookup('task', projectID, body)
      requireValue(command.domain === 'task')
      return request(
        'workLookupTaskCommand',
        (value) => {
          const result = parseWorkPlanningObservation(value, command)
          requireValue(result.domain === 'task')
          return result.value
        },
        { ...options, projectID: command.projectID, body: workPlanningLookup(command) },
      )
    },
    lookupTaskBlockerCommand(
      projectID: string,
      taskID: string,
      body: WorkBlockerLookupRequest,
      options: WorkWriteOptions,
    ): Promise<WorkTaskBlockerLookup> {
      const command = captureLookup('blocker', projectID, body, taskID)
      requireValue(command.domain === 'blocker')
      return request(
        'workLookupTaskBlockerCommand',
        (value) => {
          const result = parseWorkPlanningObservation(value, command)
          requireValue(result.domain === 'blocker')
          return result.value
        },
        {
          ...options,
          projectID: command.projectID,
          target: workPlanningTarget(command),
          body: workPlanningLookup(command),
        },
      )
    },
  }
}
export type WorkPlanningAPI = ReturnType<typeof createWorkPlanningAPI>
