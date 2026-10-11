import { AccountFailure, accountTransport, shape, string, type Fetch } from './client'
import type { WriteOptions } from './account'
import { workID, workText, workVersion } from './work-review'
import { parseSystemInstant } from './system-account'

export type SprintStartInput = Readonly<{
  expected_version: string
  request: Readonly<Record<string, never>>
}>
export type StartedSprint = Readonly<{
  id: string
  project_id: string
  milestone_id: string
  title: string
  description: string
  manual_rank: string
  version: string
  created_at: string
  updated_at: string
  state: 'current'
  started_at: string
  started_by: Readonly<{ kind: 'human'; user_id: string }>
  completed_at: null
  completed_by: null
}>
export type SprintStartProject = Readonly<{
  id: string
  owner_user_id: string
  name: string
  normalized_name: string
  description: string
  lifecycle: 'active'
  version: string
  current_sprint_id: string
  created_at: string
  updated_at: string
}>
export type SprintStartReceipt = Readonly<{
  sprint: StartedSprint
  project: SprintStartProject
  event_id: string
}>
export type SprintStartLookup =
  | Readonly<{ status: 'committed'; receipt: SprintStartReceipt }>
  | Readonly<{ status: 'in_progress' | 'not_observed'; receipt: null }>
export type SprintCreateDraft = Readonly<{
  milestone_id: string
  title: string
  description?: string
}>
export type SprintCreateInput = Readonly<{
  request: SprintCreateDraft & Readonly<{ sprint_id: string }>
}>
export type CreatedSprint = Readonly<
  Omit<StartedSprint, 'state' | 'started_at' | 'started_by'> & {
    state: 'planned'
    started_at: null
    started_by: null
  }
>
export type SprintCreateReceipt = Readonly<{
  command: 'work.sprint.create'
  changed: true
  milestone: null
  sprint: CreatedSprint
  event_id: string
}>
export type SprintCreateLookup =
  | Readonly<{ state: 'committed'; result: SprintCreateReceipt }>
  | Readonly<{ state: 'in_progress' | 'not_observed'; result: null }>
export interface WorkSprintLifecycleAPI {
  create(
    project: string,
    input: SprintCreateInput,
    write: WriteOptions,
  ): Promise<SprintCreateReceipt>
  lookupCreate(
    project: string,
    input: SprintCreateInput,
    write: WriteOptions,
  ): Promise<SprintCreateLookup>
  start(
    project: string,
    sprint: string,
    input: SprintStartInput,
    write: WriteOptions,
  ): Promise<SprintStartReceipt>
  lookup(
    project: string,
    sprint: string,
    input: SprintStartInput,
    write: WriteOptions,
  ): Promise<SprintStartLookup>
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
export function captureSprintStartInput(value: unknown): SprintStartInput {
  return input(() => {
    const v = shape(value, ['expected_version', 'request'])
    shape(v.request, [])
    return Object.freeze({
      expected_version: workVersion(v.expected_version),
      request: Object.freeze({}),
    })
  })
}
function receipt(
  value: unknown,
  projectID: string,
  sprintID: string,
  original: SprintStartInput,
): SprintStartReceipt {
  const v = shape(value, ['sprint', 'project', 'event_id'])
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
  const p = shape(v.project, [
    'id',
    'owner_user_id',
    'name',
    'normalized_name',
    'description',
    'lifecycle',
    'version',
    'current_sprint_id',
    'created_at',
    'updated_at',
  ])
  const actor = shape(s.started_by, ['kind', 'user_id'])
  const owner = workID(p.owner_user_id)
  const created = parseSystemInstant(s.created_at),
    updated = parseSystemInstant(s.updated_at),
    started = parseSystemInstant(s.started_at)
  const projectCreated = parseSystemInstant(p.created_at),
    projectUpdated = parseSystemInstant(p.updated_at)
  const rank = string(s.manual_rank, 32, 32),
    sprintVersion = workVersion(s.version),
    projectVersion = workVersion(p.version)
  const name = string(p.name, 1, 64),
    description = string(p.description, 0, 8192)
  requireValue(
    /^[A-Za-z0-9._-]+$/.test(name) &&
      name !== '.' &&
      name !== '..' &&
      p.normalized_name === name.toLowerCase(),
  )
  requireValue(
    !/[\u0000-\u0008\u000b-\u001f\u007f]/u.test(description) &&
      !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(
        description,
      ) &&
      new TextEncoder().encode(description).byteLength <= 8192,
  )
  requireValue(
    workID(s.id) === sprintID &&
      workID(s.project_id) === projectID &&
      workID(p.id) === projectID &&
      workID(p.current_sprint_id) === sprintID,
  )
  requireValue(
    s.state === 'current' &&
      p.lifecycle === 'active' &&
      actor.kind === 'human' &&
      workID(actor.user_id) === owner &&
      s.completed_at === null &&
      s.completed_by === null,
  )
  requireValue(
    BigInt(sprintVersion) === BigInt(original.expected_version) + 1n && BigInt(projectVersion) > 1n,
  )
  requireValue(
    created <= started &&
      started <= updated &&
      projectCreated <= projectUpdated &&
      updated === projectUpdated,
  )
  requireValue(/^[0-9a-f]{32}$/.test(rank) && rank !== '0'.repeat(32) && rank !== 'f'.repeat(32))
  return Object.freeze({
    sprint: Object.freeze({
      id: sprintID,
      project_id: projectID,
      milestone_id: workID(s.milestone_id),
      title: workText(s.title, 1024, true),
      description: workText(s.description, 32768),
      manual_rank: rank,
      version: sprintVersion,
      created_at: created,
      updated_at: updated,
      state: 'current',
      started_at: started,
      started_by: Object.freeze({ kind: 'human', user_id: owner }),
      completed_at: null,
      completed_by: null,
    }),
    project: Object.freeze({
      id: projectID,
      owner_user_id: owner,
      name,
      normalized_name: name.toLowerCase(),
      description,
      lifecycle: 'active',
      version: projectVersion,
      current_sprint_id: sprintID,
      created_at: projectCreated,
      updated_at: projectUpdated,
    }),
    event_id: workID(v.event_id),
  })
}
export function captureSprintCreateDraft(value: unknown): SprintCreateDraft {
  return input(() => {
    const v = shape(value, ['milestone_id', 'title'], ['description'])
    return Object.freeze({
      milestone_id: workID(v.milestone_id),
      title: workText(v.title, 1024, true),
      ...(Object.hasOwn(v, 'description') ? { description: workText(v.description, 32768) } : {}),
    })
  })
}
export function captureSprintCreateInput(value: unknown): SprintCreateInput {
  return input(() => {
    const v = shape(value, ['request'])
    const r = shape(v.request, ['sprint_id', 'milestone_id', 'title'], ['description'])
    const { sprint_id, ...draft } = r
    return Object.freeze({
      request: Object.freeze({ sprint_id: workID(sprint_id), ...captureSprintCreateDraft(draft) }),
    })
  })
}
function createReceipt(
  value: unknown,
  project: string,
  original: SprintCreateInput,
): SprintCreateReceipt {
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
    r = original.request
  requireValue(v.command === 'work.sprint.create' && v.changed === true && v.milestone === null)
  requireValue(
    workID(s.id) === r.sprint_id &&
      workID(s.project_id) === project &&
      workID(s.milestone_id) === r.milestone_id,
  )
  requireValue(
    s.title === r.title &&
      s.description === (r.description ?? '') &&
      workVersion(s.version) === '1',
  )
  requireValue(
    s.state === 'planned' &&
      s.started_at === null &&
      s.started_by === null &&
      s.completed_at === null &&
      s.completed_by === null &&
      created === updated,
  )
  requireValue(/^[0-9a-f]{32}$/.test(rank) && rank !== '0'.repeat(32) && rank !== 'f'.repeat(32))
  return Object.freeze({
    command: 'work.sprint.create',
    changed: true,
    milestone: null,
    event_id: workID(v.event_id),
    sprint: Object.freeze({
      id: r.sprint_id,
      project_id: project,
      milestone_id: r.milestone_id,
      title: r.title,
      description: r.description ?? '',
      manual_rank: rank,
      version: '1',
      created_at: created,
      updated_at: updated,
      state: 'planned',
      started_at: null,
      started_by: null,
      completed_at: null,
      completed_by: null,
    }),
  })
}
export function createWorkSprintLifecycleAPI(fetcher?: Fetch): WorkSprintLifecycleAPI {
  const call = accountTransport(fetcher)
  return {
    create(project, value, write) {
      const original = captureSprintCreateInput(value)
      const projectID = input(() => workID(project))
      return call('sprintCreate', (value) => createReceipt(value, projectID, original), {
        projectID,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookupCreate(project, value, write) {
      const original = captureSprintCreateInput(value)
      const projectID = input(() => workID(project))
      return call(
        'sprintCreateLookup',
        (value): SprintCreateLookup => {
          const v = shape(value, ['state', 'result'])
          if (v.state === 'committed')
            return Object.freeze({
              state: 'committed',
              result: createReceipt(v.result, projectID, original),
            })
          requireValue(
            (v.state === 'in_progress' || v.state === 'not_observed') && v.result === null,
          )
          return Object.freeze({ state: v.state as 'in_progress' | 'not_observed', result: null })
        },
        {
          projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.sprint.create', ...original },
        },
      )
    },
    start(project, sprint, value, write) {
      const original = captureSprintStartInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(sprint) }))
      return call('sprintStart', (v) => receipt(v, project, sprint, original), {
        ...scope,
        signal: write.signal ?? new AbortController().signal,
        csrf: write.csrfToken,
        key: write.key,
        body: original,
      })
    },
    lookup(project, sprint, value, write) {
      const original = captureSprintStartInput(value)
      const scope = input(() => ({ projectID: workID(project), target: workID(sprint) }))
      return call(
        'sprintStartLookup',
        (value): SprintStartLookup => {
          const v = shape(value, ['status', 'receipt'])
          if (v.status === 'committed')
            return Object.freeze({
              status: 'committed',
              receipt: receipt(v.receipt, project, sprint, original),
            })
          requireValue(
            (v.status === 'in_progress' || v.status === 'not_observed') && v.receipt === null,
          )
          return Object.freeze({
            status: v.status as 'in_progress' | 'not_observed',
            receipt: null,
          })
        },
        {
          projectID: scope.projectID,
          signal: write.signal ?? new AbortController().signal,
          csrf: write.csrfToken,
          key: write.key,
          body: { command: 'work.sprint.start', target_id: scope.target, ...original },
        },
      )
    },
  }
}
