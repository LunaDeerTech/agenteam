import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { workID, workText, workVersion, type Sprint } from './work-review'
import { parseSystemInstant } from './system-account'

export type SprintUpdateInput = Readonly<{
  expected_version: string
  request: Readonly<{ title?: string; description?: string }>
}>
type SprintActor =
  | Readonly<{ kind: 'human'; user_id: string }>
  | Readonly<{ kind: 'agent_run'; project_id: string; agent_id: string; execution_id: string }>
  | Readonly<{
      kind: 'service'
      project_id: string
      service_name: string
      cause_ref: string
    }>
export type UpdatedSprint = Sprint &
  Readonly<{
    state: 'planned' | 'current'
    started_by: SprintActor | null
    completed_at: null
    completed_by: null
  }>
export type SprintUpdateReceipt = Readonly<{
  command: 'work.sprint.update'
  changed: boolean
  milestone: null
  sprint: UpdatedSprint
  event_id: string | null
}>
export type SprintUpdateLookup =
  | Readonly<{ state: 'committed'; result: SprintUpdateReceipt }>
  | Readonly<{ state: 'in_progress' | 'not_observed'; result: null }>
export interface WorkSprintPlanningAPI {
  update(
    project: string,
    target: string,
    input: SprintUpdateInput,
    write: WriteOptions,
  ): Promise<SprintUpdateReceipt>
  lookupUpdate(
    project: string,
    target: string,
    input: SprintUpdateInput,
    write: WriteOptions,
  ): Promise<SprintUpdateLookup>
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
export function captureSprintUpdateInput(value: unknown): SprintUpdateInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    const r = shape(v.request, [], ['title', 'description'])
    requireValue(Object.keys(r).length > 0)
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({
        ...(Object.hasOwn(r, 'title') ? { title: workText(r.title, 1024, true) } : {}),
        ...(Object.hasOwn(r, 'description') ? { description: workText(r.description, 32768) } : {}),
      }),
    })
  })
}
function historyActor(value: unknown, project: string): SprintActor {
  const kind = shape(
    value,
    ['kind'],
    ['user_id', 'project_id', 'agent_id', 'execution_id', 'service_name', 'cause_ref'],
  ).kind
  if (kind === 'human') {
    const v = shape(value, ['kind', 'user_id'])
    return Object.freeze({ kind, user_id: workID(v.user_id) })
  }
  if (kind === 'agent_run') {
    const v = shape(value, ['kind', 'project_id', 'agent_id', 'execution_id'])
    requireValue(workID(v.project_id) === project)
    return Object.freeze({
      kind,
      project_id: project,
      agent_id: workID(v.agent_id),
      execution_id: workID(v.execution_id),
    })
  }
  const v = shape(value, ['kind', 'project_id', 'service_name', 'cause_ref'])
  requireValue(kind === 'service' && workID(v.project_id) === project)
  const service_name = string(v.service_name, 1, 128),
    cause_ref = string(v.cause_ref, 1, 256)
  requireValue(
    [
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
      'runner-identity',
      'scheduler',
    ].includes(service_name),
  )
  if (!/^sha256:[0-9a-f]{64}$/.test(cause_ref)) workID(cause_ref)
  return Object.freeze({
    kind,
    project_id: project,
    service_name,
    cause_ref,
  })
}
function receipt(
  value: unknown,
  project: string,
  target: string,
  original: SprintUpdateInput,
): SprintUpdateReceipt {
  const v = shape(value, ['command', 'changed', 'milestone', 'sprint', 'event_id'])
  const s = shape(v.sprint, [
    'id',
    'project_id',
    'milestone_id',
    'title',
    'description',
    'manual_rank',
    'version',
    'created_at',
    'updated_at',
    'state',
    'started_at',
    'started_by',
    'completed_at',
    'completed_by',
  ])
  const created = parseSystemInstant(s.created_at),
    updated = parseSystemInstant(s.updated_at)
  const rank = string(s.manual_rank, 32, 32),
    version = workVersion(s.version)
  const title = workText(s.title, 1024, true),
    description = workText(s.description, 32768)
  requireValue(v.command === 'work.sprint.update' && v.milestone === null)
  requireValue(v.changed === true || v.changed === false)
  requireValue(workID(s.id) === target && workID(s.project_id) === project && created <= updated)
  requireValue(/^[0-9a-f]{32}$/.test(rank) && rank !== '0'.repeat(32) && rank !== 'f'.repeat(32))
  requireValue(BigInt(version) === BigInt(original.expected_version) + (v.changed ? 1n : 0n))
  if (Object.hasOwn(original.request, 'title')) requireValue(title === original.request.title)
  if (Object.hasOwn(original.request, 'description'))
    requireValue(description === original.request.description)
  if (!v.changed) requireValue(v.event_id === null)
  // This is the immutable result of the original update, not a current lifecycle read.
  requireValue(
    (s.state === 'planned' || s.state === 'current') &&
      s.completed_at === null &&
      s.completed_by === null,
  )
  const started_at = s.started_at === null ? null : parseSystemInstant(s.started_at)
  const started_by = s.started_by === null ? null : historyActor(s.started_by, project)
  requireValue(
    s.state === 'planned'
      ? started_at === null && started_by === null
      : started_at !== null && started_by !== null,
  )
  if (started_at !== null) requireValue(created <= started_at && started_at <= updated)
  return Object.freeze({
    command: 'work.sprint.update',
    changed: v.changed,
    milestone: null,
    event_id: v.changed ? workID(v.event_id) : null,
    sprint: Object.freeze({
      id: target,
      project_id: project,
      milestone_id: workID(s.milestone_id),
      title,
      description,
      manual_rank: rank,
      version,
      created_at: created,
      updated_at: updated,
      state: s.state,
      started_at,
      started_by,
      completed_at: null,
      completed_by: null,
    }),
  })
}
export function createWorkSprintPlanningAPI(fetcher?: Fetch): WorkSprintPlanningAPI {
  const call = accountTransport(fetcher)
  return {
    update(project, target, value, write) {
      const original = captureSprintUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(target) }))
      return call('sprintUpdate', (v) => receipt(v, project, target, original), {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookupUpdate(project, target, value, write) {
      const original = captureSprintUpdateInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(target) }))
      return call(
        'sprintUpdateLookup',
        (value): SprintUpdateLookup => {
          const v = shape(value, ['state', 'result'])
          if (v.state === 'committed')
            return Object.freeze({
              state: 'committed',
              result: receipt(v.result, project, target, original),
            })
          requireValue(
            (v.state === 'in_progress' || v.state === 'not_observed') && v.result === null,
          )
          return Object.freeze({ state: v.state as 'in_progress' | 'not_observed', result: null })
        },
        {
          projectID: scope.projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.sprint.update', target_id: scope.target, ...original },
        },
      )
    },
  }
}
